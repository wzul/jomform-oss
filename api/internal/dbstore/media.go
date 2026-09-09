package dbstore

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MediaSource distinguishes merchant uploads from customer file uploads.
// Customer uploads are a dependency (issue #180) — the column exists so the
// Media page can filter by source once they land.
const (
	MediaSourceMerchant = "merchant"
	MediaSourceCustomer = "customer"
)

// Media models one uploaded object in the media bucket (issue #180). It
// carries the metadata the WordPress-style Media page needs: name, mime,
// size, date, and the form/source it belongs to for filtering.
type Media struct {
	ID        string
	AccountID string
	FormID    string // "" when not tied to a form
	Key       string
	URL       string
	Mime      string
	Size      int64
	Source    string
	// ScanStatus gates whether a code asset may be served (issue #361).
	// 'pending' (default) / 'scanning' / 'needs_review' / 'quarantined' keep
	// serving blocked (404); 'ok' is the ONLY status that allows a code asset
	// to be served. Set by the background scan worker (issue #364).
	ScanStatus string
	// ScanReason records WHY a code asset was flagged / quarantined (the
	// deterministic marker(s) or the AI judge's verdict). Empty for images /
	// passing assets. Set by the background scan worker (issue #364).
	ScanReason string
	CreatedAt  time.Time
	DeletedAt  *time.Time
}

// MediaStore wraps pgx for the media table.
type MediaStore struct{ pool *pgxpool.Pool }

// NewMediaStore builds a MediaStore.
func NewMediaStore(pool *pgxpool.Pool) *MediaStore { return &MediaStore{pool: pool} }

// Create inserts a media row and returns its id + created_at.
func (s *MediaStore) Create(ctx context.Context, m *Media) error {
	if m.Source == "" {
		m.Source = MediaSourceMerchant
	}
	return s.pool.QueryRow(ctx,
		`INSERT INTO media (account_id, form_id, key, url, mime, size, source, scan_status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, 'pending')
		 RETURNING id, created_at`,
		m.AccountID, nullString(m.FormID), m.Key, m.URL, m.Mime, m.Size, m.Source,
	).Scan(&m.ID, &m.CreatedAt)
}

// ListPage returns one page of non-deleted media for an account, newest
// first, optionally filtered by form and source, plus the total count for the
// same filter (for paging). Ownership is enforced inside the query.
func (s *MediaStore) ListPage(ctx context.Context, accountID, formID, source string, page, limit int) ([]*Media, int64, error) {
	if accountID == "" {
		return []*Media{}, 0, nil
	}
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	offset := (page - 1) * limit

	where := "account_id = $1 AND deleted_at IS NULL"
	args := []any{accountID}
	if formID != "" {
		args = append(args, formID)
		where += " AND form_id = $" + itoa(len(args))
	}
	if source != "" {
		args = append(args, source)
		where += " AND source = $" + itoa(len(args))
	}

	var total int64
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM media WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, account_id, form_id, key, url, mime, size, source, scan_status, scan_reason, created_at, deleted_at
		 FROM media WHERE `+where+` ORDER BY created_at DESC LIMIT $`+itoa(len(args)+1)+` OFFSET $`+itoa(len(args)+2),
		append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*Media{}
	for rows.Next() {
		m, err := scanMedia(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, m)
	}
	return out, total, rows.Err()
}

// ByIDAccount fetches a non-deleted media row by id, scoped to the account.
// Returns nil when not found or soft-deleted.
func (s *MediaStore) ByIDAccount(ctx context.Context, accountID, id string) (*Media, error) {
	m, err := scanMedia(s.pool.QueryRow(ctx,
		`SELECT id, account_id, form_id, key, url, mime, size, source, scan_status, scan_reason, created_at, deleted_at
		 FROM media WHERE id = $1 AND account_id = $2 AND deleted_at IS NULL`, id, accountID))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return m, err
}

// SoftDelete marks a media row deleted (soft-delete) and returns the object
// key so the caller can remove it from MinIO. Returns (key, true) when the
// row existed and was not already deleted; ("", false) when not found.
func (s *MediaStore) SoftDelete(ctx context.Context, accountID, id string) (string, bool, error) {
	var key string
	err := s.pool.QueryRow(ctx,
		`UPDATE media SET deleted_at = now()
		 WHERE id = $1 AND account_id = $2 AND deleted_at IS NULL
		 RETURNING key`, id, accountID).Scan(&key)
	if err == pgx.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return key, true, nil
}

// TotalBytes returns the total stored bytes for an account across all
// non-deleted media rows (issue #384). This is the per-space storage usage the
// 1GB quota is enforced against. Soft-deleted rows are excluded, so deleting a
// media object releases its bytes back into the quota.
func (s *MediaStore) TotalBytes(ctx context.Context, accountID string) (int64, error) {
	if accountID == "" {
		return 0, nil
	}
	var total int64
	if err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(size), 0) FROM media WHERE account_id = $1 AND deleted_at IS NULL`,
		accountID).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

// MarkScanning sets a media row's scan_status to 'scanning' (issue #364) so
// ops can see a scan is in flight. idempotent; no-op when the row is gone.
func (s *MediaStore) MarkScanning(ctx context.Context, accountID, id, reason string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE media SET scan_status = 'scanning', scan_reason = $3
		 WHERE id = $1 AND account_id = $2 AND deleted_at IS NULL`, id, accountID, reason)
	return err
}

// SetScanStatus persists a scan result (issue #364) on a media row, scoped to
// the owning account. No rows updated (foreign/gone id) is not an error, so
// the scan worker's retry ladder doesn't spin on a deleted asset.
func (s *MediaStore) SetScanStatus(ctx context.Context, accountID, id, status, reason string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE media SET scan_status = $3, scan_reason = $4
		 WHERE id = $1 AND account_id = $2 AND deleted_at IS NULL`,
		id, accountID, status, reason)
	return err
}

// ByKey fetches a non-deleted media row by object key, scoped to the account.
// Returns nil when not found or soft-deleted. Used by the tenant-scoped asset
// serving route to look up the scan status for an object key before serving.
func (s *MediaStore) ByKey(ctx context.Context, accountID, key string) (*Media, error) {
	m, err := scanMedia(s.pool.QueryRow(ctx,
		`SELECT id, account_id, form_id, key, url, mime, size, source, scan_status, scan_reason, created_at, deleted_at
		 FROM media WHERE key = $1 AND account_id = $2 AND deleted_at IS NULL`, key, accountID))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return m, err
}

// scanStatusFromKey reports whether a stored asset at `key` may be served.
// Serving is gated on scan_status: only 'ok' (scanned + benign) is allowed.
// 'pending'/'scanning'/'needs_review'/'quarantined' are all fail-closed.
// A missing row (e.g. legacy media predating the media table, or images that
// were uploaded before scanning existed) — treat as allow-listed, because
// images must keep working; the gate is for code assets keyed by mime.
func (s *MediaStore) scanStatusFromKey(ctx context.Context, accountID, key string) string {
	m, err := s.ByKey(ctx, accountID, key)
	if err != nil {
		return StatusUnknownScan
	}
	if m == nil {
		// Legacy object with no media row — can't prove it was scanned. For
		// images this is normal and must keep serving; code assets without a
		// row are a server-side gap (every uploadAsset records a row). We fall
		// back to scanning the key for a code extension and fail-closed there.
		return ""
	}
	return m.ScanStatus
}

// StatusUnknownScan marks "no media row found" from scanStatusFromKey.
const StatusUnknownScan = "unknown"

func scanMedia(row pgx.Row) (*Media, error) {
	m := &Media{}
	var formID *string
	var deletedAt *time.Time
	err := row.Scan(&m.ID, &m.AccountID, &formID, &m.Key, &m.URL, &m.Mime, &m.Size, &m.Source, &m.ScanStatus, &m.ScanReason, &m.CreatedAt, &deletedAt)
	if formID != nil {
		m.FormID = *formID
	}
	m.DeletedAt = deletedAt
	return m, err
}

// nullString returns a *string for a possibly-empty value (NULL in SQL).
func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
