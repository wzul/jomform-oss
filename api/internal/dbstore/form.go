package dbstore

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Form models the forms table.
type Form struct {
	ID        string
	AccountID string
	Slug      string
	Title     string
	Config    map[string]any
	Theme     map[string]any
	Status    string // draft | published
	IsMain    bool

	RedirectSuccess string `json:"redirect_success,omitempty"`
	RedirectFailure string `json:"redirect_failure,omitempty"`
	AllowEmbed      bool   `json:"allow_embed"`
	LogoURL         string `json:"logo_url,omitempty"`
	IsTemplate      bool   `json:"is_template"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// ErrFormNotFound indicates a missing form.
var ErrFormNotFound = errors.New("form not found")

// FormStore wraps pgx for forms.
type FormStore struct{ pool *pgxpool.Pool }

// NewFormStore builds a FormStore.
func NewFormStore(pool *pgxpool.Pool) *FormStore { return &FormStore{pool: pool} }

// formSelect builds every non-trash form read: deleted forms are
// invisible everywhere except the explicit trash query.
const formSelect = `SELECT id, account_id, slug, title, config, theme, status, is_main, redirect_success, redirect_failure, allow_embed, logo_url, is_template, created_at, updated_at
	FROM forms WHERE deleted_at IS NULL`

// Create inserts a form. Nil config/theme maps are coerced to {} so a
// missing key can never violate the NOT NULL constraints (SQLSTATE 23502).
func (s *FormStore) Create(ctx context.Context, f *Form) error {
	if f.Config == nil {
		f.Config = map[string]any{}
	}
	if f.Theme == nil {
		f.Theme = map[string]any{}
	}
	return s.pool.QueryRow(ctx,
		`INSERT INTO forms (account_id, slug, title, config, theme, status, redirect_success, redirect_failure, allow_embed, logo_url, is_template)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		 RETURNING id, created_at, updated_at`,
		f.AccountID, f.Slug, f.Title, f.Config, f.Theme, f.Status,
		f.RedirectSuccess, f.RedirectFailure, f.AllowEmbed, f.LogoURL, f.IsTemplate,
	).Scan(&f.ID, &f.CreatedAt, &f.UpdatedAt)
}

// ListByAccount lists all non-deleted forms for an account.
func (s *FormStore) ListByAccount(ctx context.Context, accountID string) ([]*Form, error) {
	rows, err := s.pool.Query(ctx, formSelect+" AND account_id=$1 ORDER BY created_at DESC", accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Form
	for rows.Next() {
		f, serr := s.scanOne(rows)
		if serr != nil {
			return nil, serr
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetByID fetches one non-deleted form scoped to account.
func (s *FormStore) GetByID(ctx context.Context, accountID, id string) (*Form, error) {
	return s.scanOne(s.pool.QueryRow(ctx,
		formSelect+" AND id=$2 AND account_id=$1", accountID, id))
}

// Update mutates title/config/theme (soft-deleted forms are immutable).
// A nil map means "leave unchanged": the SQL COALESCEs nil config/theme over
// the stored values, so partial payloads (e.g. PATCH without a theme key,
// MCP upsert_form without config) can never write NULL into a NOT NULL
// jsonb column (SQLSTATE 23502). Callers that want to clear a map send {}.
func (s *FormStore) Update(ctx context.Context, accountID, id, title string, config, theme map[string]any) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE forms SET title=$3, config=COALESCE($4, config), theme=COALESCE($5, theme), updated_at=now()
		 WHERE id=$2 AND account_id=$1 AND deleted_at IS NULL`,
		accountID, id, title, config, theme)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrFormNotFound
	}
	return nil
}

// SetStatus publishes/unpublishes (soft-deleted forms keep their status).
func (s *FormStore) SetStatus(ctx context.Context, accountID, id, status string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE forms SET status=$3, updated_at=now() WHERE id=$2 AND account_id=$1 AND deleted_at IS NULL`,
		accountID, id, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrFormNotFound
	}
	return nil
}

// rowScanner matches pgx.Row for form scanning.
type rowScanner interface {
	Scan(dest ...any) error
}

func (s *FormStore) scanOne(row rowScanner) (*Form, error) {
	f := &Form{}
	err := row.Scan(&f.ID, &f.AccountID, &f.Slug, &f.Title, &f.Config, &f.Theme,
		&f.Status, &f.IsMain, &f.RedirectSuccess, &f.RedirectFailure,
		&f.AllowEmbed, &f.LogoURL, &f.IsTemplate, &f.CreatedAt, &f.UpdatedAt)
	return f, err
}

// PublishedByAccountSlug resolves a published form via account subdomain +
// form slug (account slug == subdomain). Soft-deleted forms are invisible.
func (s *FormStore) PublishedByAccountSlug(ctx context.Context, accountSlug, formSlug string) (*Form, error) {
	f := &Form{}
	err := s.pool.QueryRow(ctx, "SELECT f.id, f.account_id, f.slug, f.title, f.config, f.theme, "+
		"f.status, f.is_main, f.redirect_success, f.redirect_failure, f.allow_embed, f.logo_url, f.is_template, "+
		"f.created_at, f.updated_at "+
		"FROM forms f JOIN accounts a ON a.id = f.account_id "+
		"WHERE a.slug = $1 AND f.slug = $2 AND f.status = 'published' AND f.deleted_at IS NULL",
		accountSlug, formSlug).Scan(&f.ID, &f.AccountID, &f.Slug, &f.Title, &f.Config, &f.Theme,
		&f.Status, &f.IsMain, &f.RedirectSuccess, &f.RedirectFailure,
		&f.AllowEmbed, &f.LogoURL, &f.IsTemplate, &f.CreatedAt, &f.UpdatedAt)
	return f, err
}

// SetTracking stores per-form head tracking (sanitised by caller).
func (s *FormStore) SetTracking(ctx context.Context, accountID, formID, markup string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE forms SET tracking_head=$3, updated_at=now() WHERE id=$2 AND account_id=$1`,
		accountID, formID, markup)
	return err
}

// UpdateSettings changes redirect/embed fields on a form.
func (s *FormStore) UpdateSettings(ctx context.Context, accountID, id, redirectSuccess, redirectFailure string, allowEmbed bool, logoURL string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE forms SET redirect_success=$3, redirect_failure=$4, allow_embed=$5, logo_url=$6, updated_at=now()
		 WHERE id=$2 AND account_id=$1`,
		accountID, id, redirectSuccess, redirectFailure, allowEmbed, logoURL)
	return err
}

// ListTemplates returns published template forms for the gallery.
func (s *FormStore) ListTemplates(ctx context.Context) ([]*Form, error) {
	rows, err := s.pool.Query(ctx,
		formSelect+` AND is_template AND status='published' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Form
	for rows.Next() {
		f, err := s.scanOne(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// TemplateBySlug fetches one non-deleted template.
func (s *FormStore) TemplateBySlug(ctx context.Context, slug string) (*Form, error) {
	f, err := s.scanOne(s.pool.QueryRow(ctx, formSelect+` AND is_template AND slug=$1`, slug))
	if err != nil {
		return nil, err
	}
	return f, nil
}

// PublishedBySpaceSlug resolves a published form via space subdomain
// (e.g. wanzulhosting.jomform.com/form-baru — no /f/ prefix).
func (s *FormStore) PublishedBySpaceSlug(ctx context.Context, spaceSlug, formSlug string) (*Form, error) {
	f, err := s.PublishedByAccountSlug(ctx, spaceSlug, formSlug)
	if err == nil {
		return f, nil
	}
	// space slug may differ from account slug — join spaces
	f2 := &Form{}
	err = s.pool.QueryRow(ctx,
		`SELECT f.id, f.account_id, f.slug, f.title, f.config, f.theme, f.status, f.is_main,
		        f.redirect_success, f.redirect_failure, f.allow_embed, f.logo_url, f.is_template,
		        f.created_at, f.updated_at
		 FROM forms f
		 JOIN accounts a ON a.id = f.account_id
		 JOIN spaces sp ON sp.id = a.space_id
		 WHERE sp.slug = $1 AND f.slug = $2 AND f.status = 'published' AND f.deleted_at IS NULL`,
		spaceSlug, formSlug).Scan(
		&f2.ID, &f2.AccountID, &f2.Slug, &f2.Title, &f2.Config, &f2.Theme,
		&f2.Status, &f2.IsMain, &f2.RedirectSuccess, &f2.RedirectFailure,
		&f2.AllowEmbed, &f2.LogoURL, &f2.IsTemplate, &f2.CreatedAt, &f2.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return f2, nil
}

// ListPublishedBySpace lists published forms belonging to a space (by slug).
func (s *FormStore) ListPublishedBySpace(ctx context.Context, spaceSlug string) ([]*Form, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT f.id, f.account_id, f.slug, f.title, f.config, f.theme, f.status, f.is_main,
		        f.redirect_success, f.redirect_failure, f.allow_embed, f.logo_url, f.is_template,
		        f.created_at, f.updated_at
		 FROM forms f
		 JOIN accounts a ON a.id = f.account_id
		 JOIN spaces sp ON sp.id = a.space_id
		 WHERE sp.slug = $1 AND f.status = 'published' AND f.deleted_at IS NULL
		 ORDER BY f.created_at`, spaceSlug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Form
	for rows.Next() {
		f, err := s.scanOne(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// SoftDelete marks a form deleted (trash). Soft-deleted forms disappear
// from every normal read; Restore brings them back.
func (s *FormStore) SoftDelete(ctx context.Context, accountID, formID string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE forms SET deleted_at=now(), updated_at=now()
		 WHERE id=$2 AND account_id=$1 AND deleted_at IS NULL`,
		accountID, formID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrFormNotFound
	}
	return nil
}

// Restore clears deleted_at on a trashed form.
func (s *FormStore) Restore(ctx context.Context, accountID, formID string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE forms SET deleted_at=NULL, updated_at=now()
		 WHERE id=$2 AND account_id=$1 AND deleted_at IS NOT NULL`,
		accountID, formID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrFormNotFound
	}
	return nil
}

// ListTrashed lists soft-deleted forms for an account (explicit trash read —
// the only query in the store that may return deleted rows).
func (s *FormStore) ListTrashed(ctx context.Context, accountID string) ([]*Form, error) {
	rows, err := s.pool.Query(ctx,
		formSelect+" AND deleted_at IS NOT NULL AND account_id=$1 ORDER BY deleted_at DESC", accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Form
	for rows.Next() {
		f, serr := s.scanOne(rows)
		if serr != nil {
			return nil, serr
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// SoftDeleteMany soft-deletes many forms at once; returns how many of the ids
// belong to the account and were still live (deleted_at IS NULL).
func (s *FormStore) SoftDeleteMany(ctx context.Context, accountID string, formIDs []string) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE forms SET deleted_at=now(), updated_at=now()
		 WHERE account_id=$1 AND deleted_at IS NULL AND id = ANY($2)`,
		accountID, formIDs)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// SetStatusMany publishes/unpublishes many forms at once; returns how many
// rows were updated (account-scoped, live forms only).
func (s *FormStore) SetStatusMany(ctx context.Context, accountID string, formIDs []string, status string) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE forms SET status=$3, updated_at=now()
		 WHERE account_id=$1 AND deleted_at IS NULL AND id = ANY($2)`,
		accountID, formIDs, status)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
