package dbstore

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Form statuses (issue #176): draft | private (password-protected) |
// published | unlisted. An unlisted form is live (served at its direct
// /{slug} URL, submissions work) but excluded from the storefront listing.
const (
	FormStatusDraft     = "draft"
	FormStatusPrivate   = "private"
	FormStatusPublished = "published"
	FormStatusUnlisted  = "unlisted"
)

// Form models the forms table.
type Form struct {
	ID        string
	AccountID string
	Slug      string
	Title     string
	Config    map[string]any
	Theme     map[string]any
	Status    string // draft | private | published
	IsMain    bool

	RedirectSuccess string `json:"redirect_success,omitempty"`
	RedirectFailure string `json:"redirect_failure,omitempty"`
	AllowEmbed      bool   `json:"allow_embed"`
	LogoURL         string `json:"logo_url,omitempty"`
	IsTemplate      bool   `json:"is_template"`

	// Issue #393: per-form payment-failed message. When a sale fails, the
	// thank-you page shows this (HTML-sanitized) instead of the success
	// confirmation. Empty = fall back to the buyer-facing default.
	FailureMessage string `json:"failure_message,omitempty"`

	// Issue #207: form restrictions (Gravity-Forms-style). All nullable so an
	// unconfigured form behaves exactly as before.
	StartAt        *time.Time `json:"start_at,omitempty"` // schedule window opens
	EndAt          *time.Time `json:"end_at,omitempty"`   // schedule window closes
	EntryLimit     *int       `json:"entry_limit,omitempty"`
	PendingMessage *string    `json:"pending_message,omitempty"` // before start_at
	ExpiredMessage *string    `json:"expired_message,omitempty"` // after end_at
	LimitMessage   *string    `json:"limit_message,omitempty"`   // entry limit reached

	CreatedAt time.Time
	UpdatedAt time.Time
}

// ErrFormNotFound indicates a missing form.
var ErrFormNotFound = errors.New("form not found")

// validFormStatus reports whether status is one of the accepted form statuses
// (draft | private | published | unlisted).
func validFormStatus(status string) bool {
	switch status {
	case FormStatusDraft, FormStatusPrivate, FormStatusPublished, FormStatusUnlisted:
		return true
	}
	return false
}

// FormStore wraps pgx for forms.
type FormStore struct{ pool *pgxpool.Pool }

// NewFormStore builds a FormStore.
func NewFormStore(pool *pgxpool.Pool) *FormStore { return &FormStore{pool: pool} }

// formSelect builds every non-trash form read: deleted forms are
// invisible everywhere except the explicit trash query.
const formSelect = `SELECT id, account_id, slug, title, config, theme, status, is_main, redirect_success, redirect_failure, allow_embed, logo_url, is_template, failure_message, start_at, end_at, entry_limit, pending_message, expired_message, limit_message, created_at, updated_at
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

// ListByAccount lists all non-deleted forms for an account. Templates
// (is_template) are excluded: they are managed separately (issue #457) and
// must never appear in the merchant's Forms list.
func (s *FormStore) ListByAccount(ctx context.Context, accountID string) ([]*Form, error) {
	rows, err := s.pool.Query(ctx, formSelect+" AND account_id=$1 AND NOT is_template ORDER BY created_at DESC", accountID)
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

// SetStatus sets a form's status (draft | private | published | unlisted).
// Soft-deleted forms keep their status. When moving to 'private' the caller
// must have set an access password first (SetAccessPassword) — a private form
// with no password would be permanently locked.
func (s *FormStore) SetStatus(ctx context.Context, accountID, id, status string) error {
	if !validFormStatus(status) {
		return errors.New("status mesti draft|private|published|unlisted")
	}
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

// SetStatusWithPassword sets a form's status AND its access password in one
// statement (issue #176). Used when publishing a private form so the password
// and status can never drift apart. passwordHash is an argon2id PHC string;
// pass "" to clear the password (e.g. when leaving private status).
func (s *FormStore) SetStatusWithPassword(ctx context.Context, accountID, id, status, passwordHash string) error {
	if !validFormStatus(status) {
		return errors.New("status mesti draft|private|published|unlisted")
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE forms SET status=$3, access_password=$4, updated_at=now()
		 WHERE id=$2 AND account_id=$1 AND deleted_at IS NULL`,
		accountID, id, status, passwordHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrFormNotFound
	}
	return nil
}

// SetAccessPassword stores (or clears, when hash is "") the argon2id PHC hash
// of a private form's access password. The plaintext is never stored.
func (s *FormStore) SetAccessPassword(ctx context.Context, accountID, id, passwordHash string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE forms SET access_password=$3, updated_at=now()
		 WHERE id=$2 AND account_id=$1 AND deleted_at IS NULL`,
		accountID, id, passwordHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrFormNotFound
	}
	return nil
}

// AccessPassword returns the stored argon2id PHC hash of a form's access
// password ("" when none is set). Read via a dedicated accessor so the hash
// never leaks through the main formSelect / REST responses.
func (s *FormStore) AccessPassword(ctx context.Context, accountID, id string) (string, error) {
	var hash string
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(access_password,'') FROM forms
		 WHERE id=$2 AND account_id=$1 AND deleted_at IS NULL`,
		accountID, id).Scan(&hash)
	if err != nil {
		return "", err
	}
	return hash, nil
}

// SlugExists reports whether a live (non-deleted) form in the account's workspace
// already claims the given slug. Uniqueness is per-workspace (#77); the account
// maps to exactly one workspace, so scoping by account is equivalent.
func (s *FormStore) SlugExists(ctx context.Context, accountID, slug string) (bool, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM forms
		 WHERE account_id=$1 AND slug=$2 AND deleted_at IS NULL`,
		accountID, slug).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// rowScanner matches pgx.Row for form scanning.
type rowScanner interface {
	Scan(dest ...any) error
}

func (s *FormStore) scanOne(row rowScanner) (*Form, error) {
	f := &Form{}
	err := row.Scan(&f.ID, &f.AccountID, &f.Slug, &f.Title, &f.Config, &f.Theme,
		&f.Status, &f.IsMain, &f.RedirectSuccess, &f.RedirectFailure,
		&f.AllowEmbed, &f.LogoURL, &f.IsTemplate, &f.FailureMessage, &f.StartAt, &f.EndAt,
		&f.EntryLimit, &f.PendingMessage, &f.ExpiredMessage, &f.LimitMessage,
		&f.CreatedAt, &f.UpdatedAt)
	return f, err
}

// PublishedByAccountSlug resolves a published form via account subdomain +
// form slug (account slug == subdomain). Soft-deleted forms are invisible.
func (s *FormStore) PublishedByAccountSlug(ctx context.Context, accountSlug, formSlug string) (*Form, error) {
	f := &Form{}
	err := s.pool.QueryRow(ctx, "SELECT f.id, f.account_id, f.slug, f.title, f.config, f.theme, "+
		"f.status, f.is_main, f.redirect_success, f.redirect_failure, f.allow_embed, f.logo_url, f.is_template, f.failure_message, "+
		"f.start_at, f.end_at, f.entry_limit, f.pending_message, f.expired_message, f.limit_message, "+
		"f.created_at, f.updated_at "+
		"FROM forms f JOIN accounts a ON a.id = f.account_id "+
		"WHERE a.slug = $1 AND f.slug = $2 AND f.status = 'published' AND f.deleted_at IS NULL",
		accountSlug, formSlug).Scan(&f.ID, &f.AccountID, &f.Slug, &f.Title, &f.Config, &f.Theme,
		&f.Status, &f.IsMain, &f.RedirectSuccess, &f.RedirectFailure,
		&f.AllowEmbed, &f.LogoURL, &f.IsTemplate, &f.FailureMessage, &f.StartAt, &f.EndAt,
		&f.EntryLimit, &f.PendingMessage, &f.ExpiredMessage, &f.LimitMessage,
		&f.CreatedAt, &f.UpdatedAt)
	return f, err
}

// SetTracking stores per-form head tracking (sanitised by caller).
func (s *FormStore) SetTracking(ctx context.Context, accountID, formID, markup string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE forms SET tracking_head=$3, updated_at=now() WHERE id=$2 AND account_id=$1`,
		accountID, formID, markup)
	return err
}

// UpdateSettings changes redirect/embed/failure-message fields on a form.
func (s *FormStore) UpdateSettings(ctx context.Context, accountID, id, redirectSuccess, redirectFailure string, allowEmbed bool, logoURL, failureMessage string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE forms SET redirect_success=$3, redirect_failure=$4, allow_embed=$5, logo_url=$6, failure_message=$7, updated_at=now()
		 WHERE id=$2 AND account_id=$1`,
		accountID, id, redirectSuccess, redirectFailure, allowEmbed, logoURL, failureMessage)
	return err
}

// SetFailureMessage stores a form's payment-failed message (issue #393).
// Empty clears it so the renderer falls back to the buyer-facing default.
func (s *FormStore) SetFailureMessage(ctx context.Context, accountID, id, message string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE forms SET failure_message=$3, updated_at=now() WHERE id=$2 AND account_id=$1`,
		accountID, id, message)
	return err
}

// UpdateRestrictions sets a form's schedule + entry limit (issue #207).
// startAt/endAt are nil to clear the schedule window; entryLimit is nil to
// clear the limit. Empty message strings clear the per-state override so the
// renderer falls back to the buyer-facing default for the form's language.
func (s *FormStore) UpdateRestrictions(ctx context.Context, accountID, id string, startAt, endAt *time.Time, entryLimit *int, pendingMessage, expiredMessage, limitMessage string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE forms SET start_at=$3, end_at=$4, entry_limit=$5,
		        pending_message=$6, expired_message=$7, limit_message=$8, updated_at=now()
		 WHERE id=$2 AND account_id=$1 AND deleted_at IS NULL`,
		accountID, id, startAt, endAt, entryLimit, pendingMessage, expiredMessage, limitMessage)
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

// TemplateBySlug fetches one published (gallery) template. Published status
// keeps this lookup deterministic: merchant-owned templates are drafts
// (issue #457) and are therefore never resolved by slug here — callers that
// want an own template pass its id to TemplateByIDForAccount.
func (s *FormStore) TemplateBySlug(ctx context.Context, slug string) (*Form, error) {
	f, err := s.scanOne(s.pool.QueryRow(ctx, formSelect+` AND is_template AND status='published' AND slug=$1`, slug))
	if err != nil {
		return nil, err
	}
	return f, nil
}

// ── Issue #457: merchant-managed templates ──────────────────────────────
//
// Merchant templates reuse the existing forms.is_template mechanism rather
// than a parallel table: an own template is a forms row with
// is_template=true, status='draft' and account_id = the owning account.
// Because the row is a draft it is invisible to the public gallery
// (ListTemplates filters status='published'), to the storefront
// (ListPublishedByAccountSlug filters status='published') and to the Forms
// list (ListByAccount filters NOT is_template) — every read is
// owner-scoped, so templates stay inside their tenant.

// ListTemplatesByAccount returns the account's own templates, newest first.
func (s *FormStore) ListTemplatesByAccount(ctx context.Context, accountID string) ([]*Form, error) {
	rows, err := s.pool.Query(ctx,
		formSelect+` AND account_id=$1 AND is_template ORDER BY updated_at DESC`, accountID)
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

// TemplateByIDForAccount fetches one of the account's own templates by id.
// A non-template form id or another account's template is not found.
func (s *FormStore) TemplateByIDForAccount(ctx context.Context, accountID, id string) (*Form, error) {
	f, err := s.scanOne(s.pool.QueryRow(ctx,
		formSelect+` AND id=$2 AND account_id=$1 AND is_template`, accountID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrFormNotFound
	}
	return f, err
}

// CreateTemplate inserts an owner-scoped template. is_template is forced true
// and status is forced to draft so the row can never be served publicly.
func (s *FormStore) CreateTemplate(ctx context.Context, f *Form) error {
	f.IsTemplate = true
	f.Status = FormStatusDraft
	return s.Create(ctx, f)
}

// UpdateTemplate mutates title/config/theme of the account's own template.
// Nil maps mean "leave unchanged" (same COALESCE contract as Update).
func (s *FormStore) UpdateTemplate(ctx context.Context, accountID, id, title string, config, theme map[string]any) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE forms SET title=$3, config=COALESCE($4, config), theme=COALESCE($5, theme), updated_at=now()
		 WHERE id=$2 AND account_id=$1 AND is_template AND deleted_at IS NULL`,
		accountID, id, title, config, theme)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrFormNotFound
	}
	return nil
}

// SoftDeleteTemplate soft-deletes one of the account's own templates.
func (s *FormStore) SoftDeleteTemplate(ctx context.Context, accountID, id string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE forms SET deleted_at=now(), updated_at=now()
		 WHERE id=$2 AND account_id=$1 AND is_template AND deleted_at IS NULL`,
		accountID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrFormNotFound
	}
	return nil
}

// PublishedByWorkspaceSlug resolves a published form via workspace subdomain
// (e.g. wanzulhosting.jomform.com/form-baru — no /f/ prefix).
func (s *FormStore) PublishedByWorkspaceSlug(ctx context.Context, workspaceSlug, formSlug string) (*Form, error) {
	f, err := s.PublishedByAccountSlug(ctx, workspaceSlug, formSlug)
	if err == nil {
		return f, nil
	}
	// workspace slug may differ from account slug — join workspaces
	f2 := &Form{}
	err = s.pool.QueryRow(ctx,
		`SELECT f.id, f.account_id, f.slug, f.title, f.config, f.theme, f.status, f.is_main,
		        f.redirect_success, f.redirect_failure, f.allow_embed, f.logo_url, f.is_template, f.failure_message,
		        f.start_at, f.end_at, f.entry_limit, f.pending_message, f.expired_message, f.limit_message,
		        f.created_at, f.updated_at
		 FROM forms f
		 JOIN accounts a ON a.id = f.account_id
		 JOIN workspaces sp ON sp.id = a.workspace_id
		 WHERE sp.slug = $1 AND f.slug = $2 AND f.status = 'published' AND f.deleted_at IS NULL`,
		workspaceSlug, formSlug).Scan(
		&f2.ID, &f2.AccountID, &f2.Slug, &f2.Title, &f2.Config, &f2.Theme,
		&f2.Status, &f2.IsMain, &f2.RedirectSuccess, &f2.RedirectFailure,
		&f2.AllowEmbed, &f2.LogoURL, &f2.IsTemplate, &f2.FailureMessage, &f2.StartAt, &f2.EndAt,
		&f2.EntryLimit, &f2.PendingMessage, &f2.ExpiredMessage, &f2.LimitMessage,
		&f2.CreatedAt, &f2.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return f2, nil
}

// ByWorkspaceSlug resolves a live (non-deleted) form by workspace subdomain + slug
// regardless of status (issue #176). Used by the public renderer so a private
// form can show its password gate; the caller enforces the status rules.
func (s *FormStore) ByWorkspaceSlug(ctx context.Context, workspaceSlug, formSlug string) (*Form, error) {
	f, err := s.ByAccountSlug(ctx, workspaceSlug, formSlug)
	if err == nil {
		return f, nil
	}
	f2 := &Form{}
	err = s.pool.QueryRow(ctx,
		`SELECT f.id, f.account_id, f.slug, f.title, f.config, f.theme, f.status, f.is_main,
		        f.redirect_success, f.redirect_failure, f.allow_embed, f.logo_url, f.is_template, f.failure_message,
		        f.start_at, f.end_at, f.entry_limit, f.pending_message, f.expired_message, f.limit_message,
		        f.created_at, f.updated_at
		 FROM forms f
		 JOIN accounts a ON a.id = f.account_id
		 JOIN workspaces sp ON sp.id = a.workspace_id
		 WHERE sp.slug = $1 AND f.slug = $2 AND f.deleted_at IS NULL`,
		workspaceSlug, formSlug).Scan(
		&f2.ID, &f2.AccountID, &f2.Slug, &f2.Title, &f2.Config, &f2.Theme,
		&f2.Status, &f2.IsMain, &f2.RedirectSuccess, &f2.RedirectFailure,
		&f2.AllowEmbed, &f2.LogoURL, &f2.IsTemplate, &f2.FailureMessage, &f2.StartAt, &f2.EndAt,
		&f2.EntryLimit, &f2.PendingMessage, &f2.ExpiredMessage, &f2.LimitMessage,
		&f2.CreatedAt, &f2.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return f2, nil
}

// ByAccountSlug resolves a live (non-deleted) form by account slug + form slug
// regardless of status (issue #176). See ByWorkspaceSlug.
func (s *FormStore) ByAccountSlug(ctx context.Context, accountSlug, formSlug string) (*Form, error) {
	f := &Form{}
	err := s.pool.QueryRow(ctx, "SELECT f.id, f.account_id, f.slug, f.title, f.config, f.theme, "+
		"f.status, f.is_main, f.redirect_success, f.redirect_failure, f.allow_embed, f.logo_url, f.is_template, f.failure_message, "+
		"f.start_at, f.end_at, f.entry_limit, f.pending_message, f.expired_message, f.limit_message, "+
		"f.created_at, f.updated_at "+
		"FROM forms f JOIN accounts a ON a.id = f.account_id "+
		"WHERE a.slug = $1 AND f.slug = $2 AND f.deleted_at IS NULL",
		accountSlug, formSlug).Scan(&f.ID, &f.AccountID, &f.Slug, &f.Title, &f.Config, &f.Theme,
		&f.Status, &f.IsMain, &f.RedirectSuccess, &f.RedirectFailure,
		&f.AllowEmbed, &f.LogoURL, &f.IsTemplate, &f.FailureMessage, &f.StartAt, &f.EndAt,
		&f.EntryLimit, &f.PendingMessage, &f.ExpiredMessage, &f.LimitMessage,
		&f.CreatedAt, &f.UpdatedAt)
	return f, err
}

// ListPublishedByWorkspace lists published forms for a workspace (by its URL slug).
func (s *FormStore) ListPublishedByWorkspace(ctx context.Context, workspaceSlug string) ([]*Form, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT f.id, f.account_id, f.slug, f.title, f.config, f.theme, f.status, f.is_main,
		        f.redirect_success, f.redirect_failure, f.allow_embed, f.logo_url, f.is_template, f.failure_message,
		        f.start_at, f.end_at, f.entry_limit, f.pending_message, f.expired_message, f.limit_message,
		        f.created_at, f.updated_at
		 FROM forms f
		 JOIN accounts a ON a.id = f.account_id
		 JOIN workspaces sp ON sp.id = a.workspace_id
		 WHERE sp.slug = $1 AND f.status = 'published' AND f.deleted_at IS NULL
		 ORDER BY f.created_at`, workspaceSlug)
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

// ListPublishedByAccountSlug lists published forms for an account (by its URL
// slug). Used by the sitemap handler (issue #276) to list a custom-domain
// workspace's indexable forms.
func (s *FormStore) ListPublishedByAccountSlug(ctx context.Context, accountSlug string) ([]*Form, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT f.id, f.account_id, f.slug, f.title, f.config, f.theme, f.status, f.is_main,
		        f.redirect_success, f.redirect_failure, f.allow_embed, f.logo_url, f.is_template, f.failure_message,
		        f.start_at, f.end_at, f.entry_limit, f.pending_message, f.expired_message, f.limit_message,
		        f.created_at, f.updated_at
		 FROM forms f
		 JOIN accounts a ON a.id = f.account_id
		 WHERE a.slug = $1 AND f.status = 'published' AND f.deleted_at IS NULL
		 ORDER BY f.created_at`, accountSlug)
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
// the only query in the store that may return deleted rows). It must NOT reuse
// formSelect, which already filters `deleted_at IS NULL`; appending
// `deleted_at IS NOT NULL` to it would make the query self-contradictory and
// the trash list would always be empty (issue #98 regression).
func (s *FormStore) ListTrashed(ctx context.Context, accountID string) ([]*Form, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, account_id, slug, title, config, theme, status, is_main, redirect_success, redirect_failure, allow_embed, logo_url, is_template, failure_message, start_at, end_at, entry_limit, pending_message, expired_message, limit_message, created_at, updated_at
		 FROM forms WHERE deleted_at IS NOT NULL AND account_id=$1 ORDER BY deleted_at DESC`, accountID)
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
