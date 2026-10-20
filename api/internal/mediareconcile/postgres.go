package mediareconcile

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGStore is the read/write side of the reconciliation: the accounts counter,
// the media rows, and the application surfaces that can name an object uuid.
type PGStore struct {
	pool *pgxpool.Pool
}

// Options configures a PGStore.
type Options struct {
	DSN        string
	SearchPath string // appended to the connection's search_path (tests use a scratch schema)
	ReadOnly   bool
}

// NewPGStore opens a pool. readOnly sets Postgres's own
// default_transaction_read_only on EVERY pooled connection, so a report run
// cannot write even if a later code path tries to — the report mode is enforced
// by the database, not by convention.
func NewPGStore(ctx context.Context, dsn string, readOnly bool) (*PGStore, error) {
	return NewPGStoreWithOptions(ctx, Options{DSN: dsn, ReadOnly: readOnly})
}

// NewPGStoreWithOptions is NewPGStore with an explicit search_path, which the
// integration tests use to reach their scratch schema.
func NewPGStoreWithOptions(ctx context.Context, opt Options) (*PGStore, error) {
	cfg, err := pgxpool.ParseConfig(opt.DSN)
	if err != nil {
		return nil, fmt.Errorf("db config: %w", err)
	}
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	if opt.SearchPath != "" {
		cfg.ConnConfig.RuntimeParams["search_path"] = opt.SearchPath
	}
	if opt.ReadOnly {
		cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db connect: %w", err)
	}
	return &PGStore{pool: pool}, nil
}

// NewPGStoreFromPool wraps an existing pool (tests).
func NewPGStoreFromPool(pool *pgxpool.Pool) *PGStore { return &PGStore{pool: pool} }

// Close releases the pool.
func (s *PGStore) Close() { s.pool.Close() }

// Accounts returns every account that could own media, with its quota counter.
func (s *PGStore) Accounts(ctx context.Context) ([]Account, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id::text, name, slug, COALESCE(media_used_bytes, 0) FROM accounts ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("accounts: %w", err)
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.ID, &a.Name, &a.Slug, &a.Counter); err != nil {
			return nil, fmt.Errorf("accounts scan: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// MediaRows returns every media row, live AND trashed. Trashed rows are included
// deliberately: they still own their bytes (quota is released only by purge), so
// a trashed row is not "no row" and must never be treated as orphaned.
func (s *PGStore) MediaRows(ctx context.Context) ([]Row, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id::text, account_id::text, key, url, mime, size, created_at, deleted_at
		   FROM media ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("media rows: %w", err)
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var r Row
		if err := rows.Scan(&r.ID, &r.AccountID, &r.Key, &r.URL, &r.Mime, &r.Size, &r.CreatedAt, &r.DeletedAt); err != nil {
			return nil, fmt.Errorf("media rows scan: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// referenceSurface is one place application data can name a media object. The
// SQL takes a text[] of base uuids and returns (uuid, record id) for every
// record whose text form contains one.
type referenceSurface struct {
	name string
	sql  string
}

// referenceSurfaces is the complete list of surfaces this tool checks. Adding a
// new place the product stores a media URL means adding it HERE — the
// classification ("deletable") is only as safe as this list is complete, so it
// lives in one place and the report names every surface it scanned.
var referenceSurfaces = []referenceSurface{
	{"users.avatar_url", `SELECT k.uuid, u.id::text FROM unnest($1::text[]) AS k(uuid)
	    JOIN users u ON u.avatar_url LIKE '%'||k.uuid||'%'`},
	{"forms.config", `SELECT k.uuid, f.id::text FROM unnest($1::text[]) AS k(uuid)
	    JOIN forms f ON f.config::text LIKE '%'||k.uuid||'%'`},
	{"forms.theme", `SELECT k.uuid, f.id::text FROM unnest($1::text[]) AS k(uuid)
	    JOIN forms f ON f.theme::text LIKE '%'||k.uuid||'%'`},
	{"forms.logo_url", `SELECT k.uuid, f.id::text FROM unnest($1::text[]) AS k(uuid)
	    JOIN forms f ON f.logo_url LIKE '%'||k.uuid||'%'`},
	{"products.images", `SELECT k.uuid, p.id::text FROM unnest($1::text[]) AS k(uuid)
	    JOIN products p ON p.images::text LIKE '%'||k.uuid||'%'`},
	{"product_variations.images", `SELECT k.uuid, v.id::text FROM unnest($1::text[]) AS k(uuid)
	    JOIN product_variations v ON v.images::text LIKE '%'||k.uuid||'%'`},
	{"workspace_settings.landing_page_json", `SELECT k.uuid, s.workspace_id::text FROM unnest($1::text[]) AS k(uuid)
	    JOIN workspace_settings s ON s.landing_page_json::text LIKE '%'||k.uuid||'%'`},
	{"workspace_settings.menu_json", `SELECT k.uuid, s.workspace_id::text FROM unnest($1::text[]) AS k(uuid)
	    JOIN workspace_settings s ON s.menu_json::text LIKE '%'||k.uuid||'%'`},
	{"workspace_legal_pages.content", `SELECT k.uuid, l.workspace_id::text FROM unnest($1::text[]) AS k(uuid)
	    JOIN workspace_legal_pages l ON l.content LIKE '%'||k.uuid||'%'`},
}

// Surfaces names every surface References scans, for the report.
func Surfaces() []string {
	out := make([]string, 0, len(referenceSurfaces))
	for _, s := range referenceSurfaces {
		out = append(out, s.name)
	}
	return out
}

// References scans every surface for the given base uuids and returns the hits
// keyed by uuid. An error is fatal to the run: an unscanned surface would make a
// group look unreferenced, which is the one mistake that can authorise a delete.
func (s *PGStore) References(ctx context.Context, uuids []string) (map[string][]RefHit, error) {
	out := map[string][]RefHit{}
	if len(uuids) == 0 {
		return out, nil
	}
	for _, surf := range referenceSurfaces {
		rows, err := s.pool.Query(ctx, surf.sql, uuids)
		if err != nil {
			return nil, fmt.Errorf("references %s: %w", surf.name, err)
		}
		for rows.Next() {
			var uuid, recordID string
			if err := rows.Scan(&uuid, &recordID); err != nil {
				rows.Close()
				return nil, fmt.Errorf("references %s scan: %w", surf.name, err)
			}
			out[uuid] = append(out[uuid], RefHit{Surface: surf.name, RecordID: recordID})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, fmt.Errorf("references %s: %w", surf.name, err)
		}
	}
	return out, nil
}

// CreateMediaRow inserts an adopted row. The caller has already established that
// the key has no row; the size recorded is the GROUP's byte total, because that
// is what the bytes in the bucket actually are (pre-#545 rows recorded only the
// base object).
func (s *PGStore) CreateMediaRow(ctx context.Context, r Row) (Row, error) {
	err := s.pool.QueryRow(ctx,
		`INSERT INTO media (account_id, key, url, mime, size, source, scan_status)
		 VALUES ($1, $2, $3, $4, $5, 'merchant', 'pending')
		 RETURNING id::text, created_at`,
		r.AccountID, r.Key, r.URL, r.Mime, r.Size).Scan(&r.ID, &r.CreatedAt)
	if err != nil {
		return r, fmt.Errorf("create media row %s: %w", r.Key, err)
	}
	return r, nil
}

// SetMediaUsedBytes sets one account's counter and returns the value it held
// before. The row is locked FOR UPDATE so a concurrent upload's reservation
// cannot be lost between the read and the write.
func (s *PGStore) SetMediaUsedBytes(ctx context.Context, accountID string, n int64) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var before int64
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(media_used_bytes, 0) FROM accounts WHERE id = $1 FOR UPDATE`,
		accountID).Scan(&before); err != nil {
		return 0, fmt.Errorf("read counter %s: %w", accountID, err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE accounts SET media_used_bytes = $2 WHERE id = $1`, accountID, n); err != nil {
		return 0, fmt.Errorf("set counter %s: %w", accountID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return before, nil
}

// ReadOnlySession reports whether this store's connections are read-only, so
// the report can state the mode it actually ran under rather than the flag it
// was asked for.
func (s *PGStore) ReadOnlySession(ctx context.Context) (bool, error) {
	var v string
	if err := s.pool.QueryRow(ctx, `SHOW default_transaction_read_only`).Scan(&v); err != nil {
		return false, err
	}
	return strings.EqualFold(v, "on"), nil
}

// AssertReadOnly proves the server really is refusing writes on this store's
// connections, instead of trusting that the DSN parameter was applied. It issues
// an UPDATE that can affect no rows (`WHERE false`), inside a transaction that is
// always rolled back, and requires Postgres to reject it with SQLSTATE 25006
// (read_only_sql_transaction). If the write is accepted the store is NOT
// read-only and the caller must abort before doing any "report" work.
func (s *PGStore) AssertReadOnly(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("read-only probe: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx,
		`UPDATE accounts SET media_used_bytes = media_used_bytes WHERE false`); err == nil {
		return fmt.Errorf("read-only probe: database accepted a write while report mode is on")
	} else {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "25006" {
			return nil
		}
		return fmt.Errorf("read-only probe: unexpected error %v", err)
	}
}
