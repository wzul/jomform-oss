package dbstore

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"time"

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

	// DeletedAt is set only by the trash reads (ListTrashed / GetTrashed). The
	// normal reads never populate it: a nil value is how a caller tells a live
	// form from a trashed one without a second query.
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// ErrFormNotFound indicates a missing form.
var ErrFormNotFound = errors.New("form not found")

// ErrSlugTaken indicates a slug write was refused because another live form in
// the same workspace already serves the requested slug. Uniqueness is per
// workspace (one workspace owns its accounts), so this is the store's answer to
// a collision — never a silent overwrite of either form's URL.
//
// TWO writers reach it, and they must answer the same thing: FormStore.Create
// (an explicit slug that is already taken) and FormStore.RenameSlug (a rename
// onto a taken slug). The store maps SQLSTATE 23505 on the per-workspace unique
// index (forms_workspace_slug_key) to this sentinel at both call sites, so the
// API can answer 409 form.slugInUse for the one user-visible situation however
// it was reached. Before that mapping existed on the create path, the same
// collision answered 500 `internal` with no code — the caller could not tell
// "slug taken" from "the server broke" (t_c9c7b104).
var ErrSlugTaken = errors.New("form slug already taken")

// ErrInvalidSlug indicates a requested slug is not a usable URL segment.
var ErrInvalidSlug = errors.New("form slug must be 1-40 characters of a-z, 0-9 or -")

// formSlugMaxLen is the cap the create path's generator already applies
// (uniqueFormSlug truncates a title-derived base to 40). A rename accepts the
// same bound so it can never produce a slug the create path would not have.
const formSlugMaxLen = 40

// validFormSlugChars matches any byte that may not appear in a form slug.
var validFormSlugChars = regexp.MustCompile(`^[a-z0-9-]+$`)

// ValidFormSlug reports whether s is a usable form slug: 1-40 characters of
// a-z, 0-9 and '-'. ONE rule, applied by FormStore.RenameSlug, so every write
// path that can move a form's URL (REST PATCH /forms/{id}, MCP upsert_form)
// enforces the same shape and a future third caller cannot bypass it by
// forgetting its own copy.
//
// Deliberately NOT applied to the create path's verbatim slug in this change:
// the scope of this work is that a slug must be editable, unique, and that a
// rename must not break references to it. Rewiring create is a separate
// decision.
func ValidFormSlug(s string) bool {
	return len(s) > 0 && len(s) <= formSlugMaxLen && validFormSlugChars.MatchString(s)
}

// ErrTemplateGalleryMetadata indicates a template is being published into the
// gallery without the two fields every gallery card renders (issue #594): a
// category and a text block whose value is the description line. The gallery
// endpoint reads config.category and the FIRST text block, so a template
// published without them shows up in the New-form picker as a bare title.
var ErrTemplateGalleryMetadata = errors.New("template needs a category and a description text block")

// ErrFormHasSales refuses a permanent purge of a form that was ever sold. A
// purged form row is gone for good, and sales.form_id is ON DELETE SET NULL, so
// the purge would not fail — it would silently detach the sale from the form it
// records (the sale becomes an orphan pointing at nothing). History is never
// destroyed by deleting the thing that produced it, so the purge is refused and
// the caller must decide what to do with the sales first.
var ErrFormHasSales = errors.New("form has sales")

// ErrFormSlugTaken refuses a restore whose slug was re-claimed by another live
// form in the same workspace. The per-workspace unique index covers live rows
// only (forms_workspace_slug_key, WHERE deleted_at IS NULL), so a trashed form
// releases its slug — a new form may then take it. Clearing deleted_at would
// violate the index (SQLSTATE 23505), which without this mapping escaped as a
// 500. Two live forms can never share a slug, so the restore is refused by name
// rather than silently renaming the row into a URL nobody was told about.
var ErrFormSlugTaken = errors.New("slug borang itu telah diambil borang lain")

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
type FormStore struct {
	pool *pgxpool.Pool
	// notifications is the store ReconcileDefault lives on. Update calls it
	// after a config write, because a config write is the ONLY way a form's
	// mode moves (see ReconcileDefault). It is wired by NewFormStore so no
	// caller has to remember — a caller that could forget is exactly how the
	// mode-change defect survived #597.
	notifications *FormNotificationStore
}

// NewFormStore builds a FormStore.
func NewFormStore(pool *pgxpool.Pool) *FormStore {
	return &FormStore{pool: pool, notifications: NewFormNotificationStore(pool)}
}

// formSelect builds every non-trash form read: deleted forms are
// invisible everywhere except the explicit trash query. deleted_at is part of
// the column list because every form read scans into a Form, and the
// migration/scan-drift guard pins a select's column count to that struct's
// field count — so Form.DeletedAt has to be selectable here. It is always NULL
// on these reads (the WHERE filters to live rows), which is exactly how a
// caller tells a live form from a trashed one.
const formSelect = `SELECT id, account_id, slug, title, config, theme, status, is_main, redirect_success, redirect_failure, allow_embed, logo_url, is_template, failure_message, start_at, end_at, entry_limit, pending_message, expired_message, limit_message, created_at, updated_at, deleted_at
	FROM forms WHERE deleted_at IS NULL`

// Create inserts a form. Nil config/theme maps are coerced to {} so a
// missing key can never violate the NOT NULL constraints (SQLSTATE 23502).
//
// A slug another live form of the workspace already serves is ErrSlugTaken,
// never the raw driver error. The per-workspace unique index
// (forms_workspace_slug_key, partial on deleted_at IS NULL) is the authority on
// the collision — a pre-check races any concurrent writer — so the mapping
// happens at the error level, exactly as Restore and RenameSlug do it.
//
// Without it the INSERT's SQLSTATE 23505 escaped the store, respondErr's
// default branch logged error_class=internal and answered 500 `internal error`,
// and a REST API-key caller or an MCP upsert_form with an explicit slug could
// not tell "that slug is taken" from "the server broke". The rename path onto
// the SAME taken slug already answered 409 form.slugInUse, so one user-visible
// situation had two answers and the 500 was the one hiding the reason. Mapping
// here (the one insert path behind FormStore.Create) is inherited by every
// caller — REST createForm, the two template-use routes, MCP upsert_form's
// create path and MCP template clone — rather than repeated at each of them.
func (s *FormStore) Create(ctx context.Context, f *Form) error {
	if f.Config == nil {
		f.Config = map[string]any{}
	}
	if f.Theme == nil {
		f.Theme = map[string]any{}
	}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO forms (account_id, slug, title, config, theme, status, redirect_success, redirect_failure, allow_embed, logo_url, is_template)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		 RETURNING id, created_at, updated_at`,
		f.AccountID, f.Slug, f.Title, f.Config, f.Theme, f.Status,
		f.RedirectSuccess, f.RedirectFailure, f.AllowEmbed, f.LogoURL, f.IsTemplate,
	).Scan(&f.ID, &f.CreatedAt, &f.UpdatedAt)
	if isUniqueViolation(err) {
		// The only unique index this INSERT can violate is the per-workspace
		// slug index (forms_workspace_slug_key): forms' global UNIQUE on slug
		// was dropped by migration 000019, and nothing else on the table is
		// unique beyond its primary key.
		return ErrSlugTaken
	}
	return err
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

// ListByAccounts lists every non-deleted form across a SET of accounts.
// Templates (is_template) are excluded, matching ListByAccount. Ownership is
// enforced inside the query.
//
// The multi-account read exists for the /sales record decision (records.go):
// an unfiltered read has to know which forms in the account scope are
// response-mode forms, and resolving that per account would be N queries and
// would silently miss a form when the caller's scope spans accounts (the same
// reason GetByIDInAccounts exists).
func (s *FormStore) ListByAccounts(ctx context.Context, accountIDs []string) ([]*Form, error) {
	if len(accountIDs) == 0 {
		return []*Form{}, nil
	}
	rows, err := s.pool.Query(ctx, formSelect+" AND account_id = ANY($1) AND NOT is_template ORDER BY created_at DESC", accountIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Form{}
	for rows.Next() {
		f, serr := s.scanOne(rows)
		if serr != nil {
			return nil, serr
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetByID fetches one non-deleted form scoped to account. Every way an id can
// fail to name a form the caller may see — no such row, another account's
// form, a gallery template owned by the system account, an id that is not a
// UUID at all — is ErrFormNotFound. See isNoSuchRow for why the raw driver
// error must not escape: respondErr only maps the store sentinels, so an
// unmapped error became HTTP 500 "internal error" and an error_class=internal
// counter on a plain not-found lookup, and the MCP call scope (which walks the
// caller's accounts and needs "not owned here" to continue) could not tell a
// miss from a failure.
func (s *FormStore) GetByID(ctx context.Context, accountID, id string) (*Form, error) {
	f, err := s.scanOne(s.pool.QueryRow(ctx,
		formSelect+" AND id=$2 AND account_id=$1", accountID, id))
	if isNoSuchRow(err) {
		return nil, ErrFormNotFound
	}
	return f, err
}

// GetByIDInAccounts fetches one non-deleted form by id across a SET of accounts,
// enforcing ownership inside the query. The multi-account read surfaces (e.g.
// the workspace-scoped sales list) resolve the account set once and then need
// the form behind a single-account-only GetByID — resolving it per account would
// be N queries and would silently miss the form when the caller's scope spans
// accounts. Same select/scan as every other form read.
func (s *FormStore) GetByIDInAccounts(ctx context.Context, accountIDs []string, id string) (*Form, error) {
	if len(accountIDs) == 0 || id == "" {
		return nil, ErrFormNotFound
	}
	return s.scanOne(s.pool.QueryRow(ctx,
		formSelect+" AND id=$2 AND account_id = ANY($1)", accountIDs, id))
}

// Update mutates title/config/theme (soft-deleted forms are immutable).
// A nil map means "leave unchanged": the SQL COALESCEs nil config/theme over
// the stored values, so partial payloads (e.g. PATCH without a theme key,
// MCP upsert_form without config) can never write NULL into a NOT NULL
// jsonb column (SQLSTATE 23502). Callers that want to clear a map send {}.
//
// A slug is NOT part of this statement on purpose: a rename is its own atomic
// operation (RenameSlug) because it must also leave the temporary 301 behind
// for the old URL. Keeping it out means every existing caller of Update keeps
// its exact contract and no call site needed a new argument.
//
// A config write is the ONLY way a form's mode moves, so this is also where the
// form's default notification is reconciled onto the mode it now has: a form
// switched to response mode keeps the sale.paid rule it was created with, and
// that rule can never fire — the merchant is never told anyone responded
// (issue #597's defect, surviving on the mode-change path; see
// FormNotificationStore.ReconcileDefault for the scope of what it may touch).
// It runs AFTER the write, against the stored row, so the mode it reads is the
// mode the form now has rather than the one the caller intended.
//
// The reconcile is best-effort on purpose: it must never fail a save that
// already landed. A failure to re-point a notification is a missed email, not
// a lost form edit, and making the write answer an error it cannot undo would
// be the worse outcome.
func (s *FormStore) Update(ctx context.Context, accountID, id, title string, config, theme map[string]any) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE forms SET title=$3, config=COALESCE($4, config), theme=COALESCE($5, theme), updated_at=now()
		 WHERE id=$2 AND account_id=$1 AND deleted_at IS NULL`,
		accountID, id, title, config, theme)
	if isNoSuchRow(err) {
		return ErrFormNotFound
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrFormNotFound
	}
	// Only a config write can have moved the mode; a title-only save has
	// nothing to reconcile and skips the extra read entirely.
	if config == nil || s.notifications == nil {
		return nil
	}
	if f, gerr := s.GetByID(ctx, accountID, id); gerr == nil {
		if _, rerr := s.notifications.ReconcileDefault(ctx, f); rerr != nil {
			// Best-effort, but never SILENT: a reconcile that stops working
			// would leave the merchant un-notified with nothing anywhere to
			// say so (the shape that let the original defect live through five
			// releases). The save has already landed and cannot be unwound, so
			// the failure is recorded rather than returned.
			slog.Warn("form_notification_reconcile_failed", "err", rerr,
				"form_id", id, "account", accountID)
		}
	} else {
		slog.Warn("form_notification_reconcile_skip", "err", gerr,
			"form_id", id, "account", accountID)
	}
	return nil
}

// formSlugRedirectTTL bounds how long a rename's temporary 301 lives. Wan's
// contract is "301 sementara": long enough that a printed QR stand or a shared
// link keeps working, short enough that it is a migration aid rather than a
// permanent alias. After it expires the old URL 404s like any unknown slug.
const formSlugRedirectTTL = 90 * 24 * time.Hour

// RenameSlug changes a form's public URL slug and leaves a temporary 301 from
// the old one. Returns the previous slug ("" when the slug was already the
// value passed in, i.e. nothing changed).
//
// Contract, in the order the checks run:
//
//  1. The id must name a live form of the account, else ErrFormNotFound.
//  2. The new slug must not be claimed by another live form of the same
//     account — SlugExists is the create path's own rule (#77/#176), so a
//     rename can never produce a collision the create path would have refused.
//     A collision is ErrSlugTaken (HTTP 409), never a silent write: the
//     carded defect was precisely a write that reported success and changed
//     nothing.
//  3. The slug write and the redirect row happen in ONE transaction. A rename
//     that moved the form but left no redirect would orphan every URL already
//     handed out (a printed QR, an order_form_slug reference), which is the
//     second half of the card.
//
// The redirect row is keyed (account_id, old_slug) and UPSERTed, so a later
// rename onto the same old slug REPLACES the target instead of adding a second
// answer. It is only ever consulted as a FALLBACK (see ByAccountSlug), so it
// can never shadow a live form that later claims the slug — the claimant wins
// by being found first, which is exactly Wan's rule.
func (s *FormStore) RenameSlug(ctx context.Context, accountID, id, newSlug string) (string, error) {
	newSlug = strings.ToLower(strings.TrimSpace(newSlug))
	if !ValidFormSlug(newSlug) {
		return "", ErrInvalidSlug
	}
	var oldSlug string
	err := s.pool.QueryRow(ctx,
		`SELECT slug FROM forms WHERE id=$2 AND account_id=$1 AND deleted_at IS NULL`,
		accountID, id).Scan(&oldSlug)
	if isNoSuchRow(err) {
		return "", ErrFormNotFound
	}
	if err != nil {
		return "", err
	}
	if oldSlug == newSlug {
		// Nothing to do: the caller asked for the slug the form already has.
		// No redirect row is written, because the old URL is still the live
		// URL and a redirect onto itself would be a loop.
		return "", nil
	}
	// Another live form in the same account already serves this slug.
	taken, terr := s.SlugExistsNotID(ctx, accountID, newSlug, id)
	if terr != nil {
		return "", terr
	}
	if taken {
		return "", ErrSlugTaken
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx,
		`UPDATE forms SET slug=$3, updated_at=now()
		  WHERE id=$2 AND account_id=$1 AND deleted_at IS NULL`,
		accountID, id, newSlug)
	if err != nil {
		if isUniqueViolation(err) {
			// SlugExists is scoped per ACCOUNT, while the unique index is
			// (workspace_id, slug) — a workspace can hold more than one
			// account (migration 000024), so a sibling account in the same
			// workspace can hold the slug and the pre-check above cannot see
			// it. The index is the authority; report it as the same conflict
			// rather than a 500.
			return "", ErrSlugTaken
		}
		if isNoSuchRow(err) {
			return "", ErrFormNotFound
		}
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", ErrFormNotFound
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO form_slug_redirects (account_id, old_slug, form_id, expires_at)
		 VALUES ($1, $2, $3, now() + $4::interval)
		 ON CONFLICT (account_id, old_slug)
		 DO UPDATE SET form_id = EXCLUDED.form_id, created_at = now(), expires_at = EXCLUDED.expires_at`,
		accountID, oldSlug, id, formSlugRedirectTTL.String()); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return oldSlug, nil
}

// SlugExistsNotID reports whether another live form (id <> exceptID) in the
// account's workspace already claims the slug. SlugExists is the create-time
// rule; a rename has to ask the same question while excluding the form being
// renamed, which would otherwise report itself as a collision.
func (s *FormStore) SlugExistsNotID(ctx context.Context, accountID, slug, exceptID string) (bool, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM forms
		 WHERE account_id=$1 AND slug=$2 AND deleted_at IS NULL
		   AND id <> COALESCE(NULLIF($3,'')::uuid, '00000000-0000-0000-0000-000000000000'::uuid)`,
		accountID, slug, exceptID).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// isUniqueViolation lives in notfound.go, beside isNoSuchRow: main grew it as a
// shared helper after this branch's base, and the two copies this rebase briefly
// carried were identical. One definition, so a future change to the 23505
// handling cannot apply to one call site and not the other.

// galleryMetadataMissingSQL is the predicate that reports a template row as
// unfit for the gallery: no config.category, or no text block carrying a value
// (issue #594). The gallery endpoint and the picker read exactly these two
// things, so a row failing it would render as a bare title.
const galleryMetadataMissingSQL = `
	COALESCE(config->>'category', '') = ''
	OR NOT EXISTS (
	     SELECT 1 FROM jsonb_array_elements(COALESCE(config->'blocks', '[]'::jsonb)) b
	      WHERE b->>'type' = 'text' AND COALESCE(b->>'value', '') <> ''
	   )`

// setStatusSQL performs a status change and, in the same round trip, reports
// whether the row exists and whether the gallery-metadata rule blocks it
// (issue #594). Publishing a template with neither a category nor a description
// text block is refused rather than silently producing a card with no category
// and no description — the defect this issue is about.
//
// The UPDATE is written with a CTE rather than a WHERE clause so the caller can
// tell "no such form" (0 rows matched) apart from "refused" (matched but
// blocked): a WHERE clause would collapse both into 0 rows affected and the
// caller would report "not found" for a row it was looking straight at.
const setStatusSQL = `
	WITH target AS (
	  SELECT id, is_template, config FROM forms
	   WHERE id = $2 AND account_id = $1 AND deleted_at IS NULL
	),
	blocked AS (
	  SELECT id FROM target
	   WHERE is_template AND $3 = 'published' AND (` + galleryMetadataMissingSQL + `)
	),
	updated AS (
	  UPDATE forms SET status = $3, updated_at = now()
	   WHERE id IN (SELECT id FROM target)
	     AND id NOT IN (SELECT id FROM blocked)
	   RETURNING id
	)
	SELECT (SELECT count(*) FROM target), (SELECT count(*) FROM blocked)`

// setStatusWithPasswordSQL is setStatusSQL with the access password written in
// the same statement (issue #176: the password and the status must never drift
// apart). Same existence/blocked reporting as setStatusSQL.
const setStatusWithPasswordSQL = `
	WITH target AS (
	  SELECT id, is_template, config FROM forms
	   WHERE id = $2 AND account_id = $1 AND deleted_at IS NULL
	),
	blocked AS (
	  SELECT id FROM target
	   WHERE is_template AND $3 = 'published' AND (` + galleryMetadataMissingSQL + `)
	),
	updated AS (
	  UPDATE forms SET status = $3, access_password = $4, updated_at = now()
	   WHERE id IN (SELECT id FROM target)
	     AND id NOT IN (SELECT id FROM blocked)
	   RETURNING id
	)
	SELECT (SELECT count(*) FROM target), (SELECT count(*) FROM blocked)`

// classifyStatusWrite turns the two counts into the store's error contract.
func classifyStatusWrite(matched, blocked int64) error {
	switch {
	case matched == 0:
		return ErrFormNotFound
	case blocked > 0:
		return ErrTemplateGalleryMetadata
	}
	return nil
}

// SetStatus sets a form's status (draft | private | published | unlisted).
// Soft-deleted forms keep their status. When moving to 'private' the caller
// must have set an access password first (SetAccessPassword) — a private form
// with no password would be permanently locked.
//
// Publishing a template (is_template) requires a category and a text block
// (issue #594); without them it returns ErrTemplateGalleryMetadata instead of
// putting an unlabelled card in the gallery.
func (s *FormStore) SetStatus(ctx context.Context, accountID, id, status string) error {
	if !validFormStatus(status) {
		return errors.New("status mesti draft|private|published|unlisted")
	}
	var matched, blocked int64
	if err := s.pool.QueryRow(ctx, setStatusSQL, accountID, id, status).Scan(&matched, &blocked); err != nil {
		if isNoSuchRow(err) {
			return ErrFormNotFound
		}
		return err
	}
	return classifyStatusWrite(matched, blocked)
}

// SetStatusWithPassword sets a form's status AND its access password in one
// statement (issue #176). Used when publishing a private form so the password
// and status can never drift apart. passwordHash is an argon2id PHC string;
// pass "" to clear the password (e.g. when leaving private status). The
// gallery-metadata rule of SetStatus applies unchanged (issue #594).
func (s *FormStore) SetStatusWithPassword(ctx context.Context, accountID, id, status, passwordHash string) error {
	if !validFormStatus(status) {
		return errors.New("status mesti draft|private|published|unlisted")
	}
	var matched, blocked int64
	if err := s.pool.QueryRow(ctx, setStatusWithPasswordSQL,
		accountID, id, status, passwordHash).Scan(&matched, &blocked); err != nil {
		if isNoSuchRow(err) {
			return ErrFormNotFound
		}
		return err
	}
	return classifyStatusWrite(matched, blocked)
}

// SetAccessPassword stores (or clears, when hash is "") the argon2id PHC hash
// of a private form's access password. The plaintext is never stored.
func (s *FormStore) SetAccessPassword(ctx context.Context, accountID, id, passwordHash string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE forms SET access_password=$3, updated_at=now()
		 WHERE id=$2 AND account_id=$1 AND deleted_at IS NULL`,
		accountID, id, passwordHash)
	if isNoSuchRow(err) {
		return ErrFormNotFound
	}
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
// already claims the given slug.
//
// Uniqueness is per-WORKSPACE (#77), and this query is scoped by ACCOUNT. They are
// NOT equivalent, and the comment here used to claim they were ("the account maps
// to exactly one workspace, so scoping by account is equivalent"). They are equal
// only while every account HAS a workspace: the unique index is
// `forms (workspace_id, slug) WHERE deleted_at IS NULL` (000019_slug_scoping), and
// `accounts.workspace_id` is nullable — so a workspace-free account writes forms
// with `workspace_id IS NULL`, and NULL never equals NULL in an index, which means
// such an account can create the SAME slug twice. This query still reports the
// first one (it filters by account_id), so it is the stricter answer, not an
// equivalent one — a caller relying on "equivalent" would be wrong about which
// rows the index actually protects. The behaviour is pinned by
// TestFormCreateSlugIndexIsWorkspaceScoped (form_create_slug_test.go), which is the
// control that disproves the old claim.
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

// redirectFormSelect builds the redirect fallback's read: the live form a
// rename's temporary 301 points at. Column list and order match formSelect so
// FormStore.scanOne scans it — the same full-struct scan as every other form
// read, so a future migration cannot drift this one copy out of alignment.
//
// A redirect is only ever a FALLBACK: every public lookup must first ask
// whether a live form serves the slug, and come here only when none does. That
// ordering is the whole of Wan's rule — a form that later CLAIMS the old slug
// wins, because it is found first, and this table can never shadow it. Nothing
// is reserved and no cleanup job is needed for the supersede case.
//
// expires_at > now() bounds the redirect: it is a temporary migration aid, so
// an expired row is invisible and the old URL 404s like any unknown slug.
const redirectFormColumns = `f.id, f.account_id, f.slug, f.title, f.config, f.theme, f.status, f.is_main,
	        f.redirect_success, f.redirect_failure, f.allow_embed, f.logo_url, f.is_template, f.failure_message,
	        f.start_at, f.end_at, f.entry_limit, f.pending_message, f.expired_message, f.limit_message,
	        f.created_at, f.updated_at, f.deleted_at`

// RedirectedFormByAccountSlug resolves a rename's temporary 301 on an account
// slug host (a verified custom domain, or an account with no linked workspace):
// oldSlug is no longer a live form, but a rename left a live form behind it.
// Returns ErrFormNotFound when nothing (or nothing live) matches — the same
// answer an unknown slug gets.
func (s *FormStore) RedirectedFormByAccountSlug(ctx context.Context, accountSlug, oldSlug string) (*Form, error) {
	f, err := s.scanOne(s.pool.QueryRow(ctx,
		`SELECT `+redirectFormColumns+`
		 FROM form_slug_redirects r
		 JOIN forms f ON f.id = r.form_id AND f.deleted_at IS NULL
		 JOIN accounts a ON a.id = r.account_id
		 WHERE a.slug = $1 AND r.old_slug = $2 AND r.expires_at > now()`,
		accountSlug, oldSlug))
	if isNoSuchRow(err) {
		return nil, ErrFormNotFound
	}
	return f, err
}

// RedirectedFormByWorkspaceSlug resolves a rename's temporary 301 on a WORKSPACE
// slug host ({workspace}.jomform.com). Same contract as
// RedirectedFormByAccountSlug; the workspace slug is a different namespace from
// the account slug, so it joins workspaces rather than accounts.
func (s *FormStore) RedirectedFormByWorkspaceSlug(ctx context.Context, workspaceSlug, oldSlug string) (*Form, error) {
	f, err := s.scanOne(s.pool.QueryRow(ctx,
		`SELECT `+redirectFormColumns+`
		 FROM form_slug_redirects r
		 JOIN forms f ON f.id = r.form_id AND f.deleted_at IS NULL
		 JOIN accounts a ON a.id = r.account_id
		 JOIN workspaces sp ON sp.id = a.workspace_id
		 WHERE sp.slug = $1 AND r.old_slug = $2 AND r.expires_at > now()`,
		workspaceSlug, oldSlug))
	if isNoSuchRow(err) {
		return nil, ErrFormNotFound
	}
	return f, err
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
		&f.CreatedAt, &f.UpdatedAt, &f.DeletedAt)
	return f, err
}

// PublishedByAccountSlug resolves a published form by the owning ACCOUNT's slug
// plus the form slug. Soft-deleted forms are invisible.
//
// This is the CUSTOM-DOMAIN resolver (issue #236/#297): a verified custom domain
// belongs to an account, so the public renderer maps the request host to the
// account slug and looks the form up here. It is NOT a subdomain resolver —
// a {workspace}.jomform.com host resolves through PublishedByWorkspaceSlug.
func (s *FormStore) PublishedByAccountSlug(ctx context.Context, accountSlug, formSlug string) (*Form, error) {
	f := &Form{}
	err := s.pool.QueryRow(ctx, "SELECT f.id, f.account_id, f.slug, f.title, f.config, f.theme, "+
		"f.status, f.is_main, f.redirect_success, f.redirect_failure, f.allow_embed, f.logo_url, f.is_template, f.failure_message, "+
		"f.start_at, f.end_at, f.entry_limit, f.pending_message, f.expired_message, f.limit_message, "+
		"f.created_at, f.updated_at, f.deleted_at "+
		"FROM forms f JOIN accounts a ON a.id = f.account_id "+
		"WHERE a.slug = $1 AND f.slug = $2 AND f.status = 'published' AND f.deleted_at IS NULL",
		accountSlug, formSlug).Scan(&f.ID, &f.AccountID, &f.Slug, &f.Title, &f.Config, &f.Theme,
		&f.Status, &f.IsMain, &f.RedirectSuccess, &f.RedirectFailure,
		&f.AllowEmbed, &f.LogoURL, &f.IsTemplate, &f.FailureMessage, &f.StartAt, &f.EndAt,
		&f.EntryLimit, &f.PendingMessage, &f.ExpiredMessage, &f.LimitMessage,
		&f.CreatedAt, &f.UpdatedAt, &f.DeletedAt)
	return f, err
}

// SetTracking stores a form's custom head markup. A miss — no such form,
// another account's, or an id that cannot be a UUID at all — is ErrFormNotFound
// (see isNoSuchRow).
func (s *FormStore) SetTracking(ctx context.Context, accountID, formID, markup string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE forms SET tracking_head=$3, updated_at=now() WHERE id=$2 AND account_id=$1`,
		accountID, formID, markup)
	if isNoSuchRow(err) {
		return ErrFormNotFound
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrFormNotFound
	}
	return nil
}

// UpdateSettings changes redirect/embed/failure-message fields on a form. A
// miss is ErrFormNotFound (see isNoSuchRow).
func (s *FormStore) UpdateSettings(ctx context.Context, accountID, id, redirectSuccess, redirectFailure string, allowEmbed bool, logoURL, failureMessage string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE forms SET redirect_success=$3, redirect_failure=$4, allow_embed=$5, logo_url=$6, failure_message=$7, updated_at=now()
		 WHERE id=$2 AND account_id=$1`,
		accountID, id, redirectSuccess, redirectFailure, allowEmbed, logoURL, failureMessage)
	if isNoSuchRow(err) {
		return ErrFormNotFound
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrFormNotFound
	}
	return nil
}

// SetFailureMessage stores a form's payment-failed message (issue #393).
// Empty clears it so the renderer falls back to the buyer-facing default.
// A miss is ErrFormNotFound (see isNoSuchRow).
func (s *FormStore) SetFailureMessage(ctx context.Context, accountID, id, message string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE forms SET failure_message=$3, updated_at=now() WHERE id=$2 AND account_id=$1`,
		accountID, id, message)
	if isNoSuchRow(err) {
		return ErrFormNotFound
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrFormNotFound
	}
	return nil
}

// UpdateRestrictions sets a form's schedule + entry limit (issue #207).
// startAt/endAt are nil to clear the schedule window; entryLimit is nil to
// clear the limit. Empty message strings clear the per-state override so the
// renderer falls back to the buyer-facing default for the form's language.
func (s *FormStore) UpdateRestrictions(ctx context.Context, accountID, id string, startAt, endAt *time.Time, entryLimit *int, pendingMessage, expiredMessage, limitMessage string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE forms SET start_at=$3, end_at=$4, entry_limit=$5,
		        pending_message=$6, expired_message=$7, limit_message=$8, updated_at=now()
		 WHERE id=$2 AND account_id=$1 AND deleted_at IS NULL`,
		accountID, id, startAt, endAt, entryLimit, pendingMessage, expiredMessage, limitMessage)
	if isNoSuchRow(err) {
		return ErrFormNotFound
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrFormNotFound
	}
	return nil
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
//
// Issue #589: "own template" must mean NOT ALSO a published gallery template.
// The two template queries used to overlap — a published template owned by a
// merchant account satisfied both ListTemplates and ListTemplatesByAccount — so
// such a row was returned twice and the picker (which merges both lists) carded
// it twice. The gallery is the single source for published templates, so the
// own-templates query excludes them and the two lists are now disjoint by
// construction: a row is either a gallery template or an own template.

// ListTemplatesByAccount returns the account's own (non-gallery) templates,
// newest first. Templates the public gallery already serves are excluded
// (issue #589) so this list can never overlap ListTemplates.
func (s *FormStore) ListTemplatesByAccount(ctx context.Context, accountID string) ([]*Form, error) {
	rows, err := s.pool.Query(ctx,
		formSelect+` AND account_id=$1 AND is_template
		 AND NOT (status=$2) ORDER BY updated_at DESC`, accountID, FormStatusPublished)
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
// A non-template form id or another account's template is not found. So is an
// id that is not a UUID at all: Postgres raises SQLSTATE 22P02 for an id that
// can never name a row, which is bad input rather than a server fault, and
// respondErr only maps the store sentinels — the raw driver error would become
// a 500 with an error_class=internal counter. isNoSuchRow recognises both
// shapes at the error level.
func (s *FormStore) TemplateByIDForAccount(ctx context.Context, accountID, id string) (*Form, error) {
	f, err := s.scanOne(s.pool.QueryRow(ctx,
		formSelect+` AND id=$2 AND account_id=$1 AND is_template`, accountID, id))
	if isNoSuchRow(err) {
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
// A miss is a not-found like TemplateByIDForAccount: an id naming no row
// (never existed, another account's, or not a template) and an id that cannot
// be a UUID at all (SQLSTATE 22P02) both map to ErrFormNotFound, so the delete
// route answers 404 rather than leaking a driver error into a 500.
func (s *FormStore) SoftDeleteTemplate(ctx context.Context, accountID, id string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE forms SET deleted_at=now(), updated_at=now()
		 WHERE id=$2 AND account_id=$1 AND is_template AND deleted_at IS NULL`,
		accountID, id)
	if isNoSuchRow(err) {
		return ErrFormNotFound
	}
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
//
// The host is a WORKSPACE slug and nothing else. This used to try the account
// slug first and fall back to the workspace join, which made every workspace
// serve on two hostnames: the one the dashboard shows
// ({workspace}.jomform.com) and the synthetic account slug
// (<workspace>-<8 hex of the account id>) that no surface ever displays. Wan's
// decision: a workspace is served on its workspace subdomain, and nothing else.
// Both slugs are different namespaces — accounts.slug is still what a VERIFIED
// CUSTOM DOMAIN resolves through (PublishedByAccountSlug), which is why that
// helper remains.
func (s *FormStore) PublishedByWorkspaceSlug(ctx context.Context, workspaceSlug, formSlug string) (*Form, error) {
	f2 := &Form{}
	err := s.pool.QueryRow(ctx,
		`SELECT f.id, f.account_id, f.slug, f.title, f.config, f.theme, f.status, f.is_main,
		        f.redirect_success, f.redirect_failure, f.allow_embed, f.logo_url, f.is_template, f.failure_message,
		        f.start_at, f.end_at, f.entry_limit, f.pending_message, f.expired_message, f.limit_message,
		        f.created_at, f.updated_at, f.deleted_at
 FROM forms f
		 JOIN accounts a ON a.id = f.account_id
		 JOIN workspaces sp ON sp.id = a.workspace_id AND sp.deleted_at IS NULL
		 WHERE sp.slug = $1 AND f.slug = $2 AND f.status = 'published' AND f.deleted_at IS NULL`,
		workspaceSlug, formSlug).Scan(
		&f2.ID, &f2.AccountID, &f2.Slug, &f2.Title, &f2.Config, &f2.Theme,
		&f2.Status, &f2.IsMain, &f2.RedirectSuccess, &f2.RedirectFailure,
		&f2.AllowEmbed, &f2.LogoURL, &f2.IsTemplate, &f2.FailureMessage, &f2.StartAt, &f2.EndAt,
		&f2.EntryLimit, &f2.PendingMessage, &f2.ExpiredMessage, &f2.LimitMessage,
		&f2.CreatedAt, &f2.UpdatedAt, &f2.DeletedAt)
	if err != nil {
		return nil, err
	}
	return f2, nil
}

// ByWorkspaceSlug resolves a live (non-deleted) form by workspace subdomain + slug
// regardless of status (issue #176). Used by the public renderer so a private
// form can show its password gate; the caller enforces the status rules.
//
// As with PublishedByWorkspaceSlug, the host is the WORKSPACE slug only: a host
// that names no workspace resolves nothing, and every caller answers 404 (see
// accountSubdomain, which already returns "" for the apex and for the reserved
// infrastructure names). An account's own slug is not a host a form is served
// on — it is the namespace a verified custom domain resolves through.
func (s *FormStore) ByWorkspaceSlug(ctx context.Context, workspaceSlug, formSlug string) (*Form, error) {
	f2 := &Form{}
	err := s.pool.QueryRow(ctx,
		`SELECT f.id, f.account_id, f.slug, f.title, f.config, f.theme, f.status, f.is_main,
		        f.redirect_success, f.redirect_failure, f.allow_embed, f.logo_url, f.is_template, f.failure_message,
		        f.start_at, f.end_at, f.entry_limit, f.pending_message, f.expired_message, f.limit_message,
		        f.created_at, f.updated_at, f.deleted_at
		 FROM forms f
		 JOIN accounts a ON a.id = f.account_id
		 JOIN workspaces sp ON sp.id = a.workspace_id AND sp.deleted_at IS NULL
		 WHERE sp.slug = $1 AND f.slug = $2 AND f.deleted_at IS NULL`,
		workspaceSlug, formSlug).Scan(
		&f2.ID, &f2.AccountID, &f2.Slug, &f2.Title, &f2.Config, &f2.Theme,
		&f2.Status, &f2.IsMain, &f2.RedirectSuccess, &f2.RedirectFailure,
		&f2.AllowEmbed, &f2.LogoURL, &f2.IsTemplate, &f2.FailureMessage, &f2.StartAt, &f2.EndAt,
		&f2.EntryLimit, &f2.PendingMessage, &f2.ExpiredMessage, &f2.LimitMessage,
		&f2.CreatedAt, &f2.UpdatedAt, &f2.DeletedAt)
	if err != nil {
		return nil, err
	}
	return f2, nil
}

// ByAccountSlug resolves a live (non-deleted) form by account slug + form slug
// regardless of status (issue #176). See PublishedByAccountSlug — this is the
// custom-domain resolver, not a subdomain one.
func (s *FormStore) ByAccountSlug(ctx context.Context, accountSlug, formSlug string) (*Form, error) {
	f := &Form{}
	err := s.pool.QueryRow(ctx, "SELECT f.id, f.account_id, f.slug, f.title, f.config, f.theme, "+
		"f.status, f.is_main, f.redirect_success, f.redirect_failure, f.allow_embed, f.logo_url, f.is_template, f.failure_message, "+
		"f.start_at, f.end_at, f.entry_limit, f.pending_message, f.expired_message, f.limit_message, "+
		"f.created_at, f.updated_at, f.deleted_at "+
		"FROM forms f JOIN accounts a ON a.id = f.account_id "+
		"WHERE a.slug = $1 AND f.slug = $2 AND f.deleted_at IS NULL",
		accountSlug, formSlug).Scan(&f.ID, &f.AccountID, &f.Slug, &f.Title, &f.Config, &f.Theme,
		&f.Status, &f.IsMain, &f.RedirectSuccess, &f.RedirectFailure,
		&f.AllowEmbed, &f.LogoURL, &f.IsTemplate, &f.FailureMessage, &f.StartAt, &f.EndAt,
		&f.EntryLimit, &f.PendingMessage, &f.ExpiredMessage, &f.LimitMessage,
		&f.CreatedAt, &f.UpdatedAt, &f.DeletedAt)
	return f, err
}

// ListPublishedByWorkspace lists published forms for a workspace (by its URL slug).
func (s *FormStore) ListPublishedByWorkspace(ctx context.Context, workspaceSlug string) ([]*Form, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT f.id, f.account_id, f.slug, f.title, f.config, f.theme, f.status, f.is_main,
		        f.redirect_success, f.redirect_failure, f.allow_embed, f.logo_url, f.is_template, f.failure_message,
		        f.start_at, f.end_at, f.entry_limit, f.pending_message, f.expired_message, f.limit_message,
		        f.created_at, f.updated_at, f.deleted_at
		 FROM forms f
		 JOIN accounts a ON a.id = f.account_id
		 JOIN workspaces sp ON sp.id = a.workspace_id AND sp.deleted_at IS NULL
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
		        f.created_at, f.updated_at, f.deleted_at
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
	if isNoSuchRow(err) {
		return ErrFormNotFound
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrFormNotFound
	}
	return nil
}

// Restore clears deleted_at on a trashed form.
//
// A restore whose slug was re-claimed while the form sat in the trash is
// refused with ErrFormSlugTaken rather than failing as an opaque 500: the
// per-workspace unique index only covers LIVE rows, so trashing releases the
// slug and a new form may take it, at which point clearing deleted_at violates
// the index (SQLSTATE 23505). Nothing is renamed behind the merchant's back —
// the caller is told which obstacle to clear.
func (s *FormStore) Restore(ctx context.Context, accountID, formID string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE forms SET deleted_at=NULL, updated_at=now()
		 WHERE id=$2 AND account_id=$1 AND deleted_at IS NOT NULL`,
		accountID, formID)
	// A malformed id never reaches RowsAffected: Postgres refuses to CAST it at
	// all and raises SQLSTATE 22P02 out of Exec. Without this arm the raw
	// PgError is returned, `respondErr` has no case for it, and the caller gets
	// a 500 for what is really "there is no such form" — measured on main:
	//   POST /v1/rest/forms/not-a-uuid/restore -> 500 {"code":"internal"}
	//   ERROR internal_error err="ERROR: invalid input syntax for type uuid:
	//     \"not-a-uuid\" (SQLSTATE 22P02)" rest_handlers.go:1258
	// while the sibling reads answer 404 for the same shape (the workspace trash
	// reads via PR 859, and this file's own GetTrashed). SoftDelete above carries
	// the same arm; this function was the one copy left without it.
	// A non-uuid id is not a client error to explain either — it cannot name a
	// row, so it is "not found", the same answer an unknown uuid gets.
	if isNoSuchRow(err) {
		return ErrFormNotFound
	}
	if isUniqueViolation(err) {
		return ErrFormSlugTaken
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrFormNotFound
	}
	return nil
}

// Purge permanently deletes ONE trashed form (and, through the FKs already on
// the table, its submissions, notifications, confirmations, per-form email
// settings and form-scoped webhooks — all ON DELETE CASCADE).
//
// Two guards, because a purge cannot be undone:
//
//  1. Only a TRASHED form may be purged (`deleted_at IS NOT NULL`), so the only
//     purge path is trash → purge and a live form cannot be destroyed by a
//     single mistyped call. A live form is ErrFormNotFound here, exactly as an
//     unknown id is.
//  2. A form that has EVER received a sale is refused outright with
//     ErrFormHasSales. `sales.form_id` is ON DELETE SET NULL, so the delete
//     itself would succeed and silently detach every sale from the form it
//     records. Sales are history: they are never destroyed or orphaned by the
//     deletion of the form that produced them.
func (s *FormStore) Purge(ctx context.Context, accountID, formID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock the row so a concurrent restore cannot flip it live between the
	// trashed check and the delete.
	var slug string
	if err := tx.QueryRow(ctx,
		`SELECT slug FROM forms
		  WHERE id=$2 AND account_id=$1 AND deleted_at IS NOT NULL
		  FOR UPDATE`,
		accountID, formID).Scan(&slug); isNoSuchRow(err) {
		return ErrFormNotFound
	} else if err != nil {
		return err
	}

	var sales int64
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM sales WHERE account_id=$1 AND form_id=$2`,
		accountID, formID).Scan(&sales); err != nil {
		return err
	}
	if sales > 0 {
		return ErrFormHasSales
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM forms WHERE id=$2 AND account_id=$1 AND deleted_at IS NOT NULL`,
		accountID, formID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CountSalesByForm reports how many sales reference a form (account-scoped).
// Used by the purge guard so the refusal can state the number.
func (s *FormStore) CountSalesByForm(ctx context.Context, accountID, formID string) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM sales WHERE account_id=$1 AND form_id=$2`,
		accountID, formID).Scan(&n)
	return n, err
}

// trashedFormSelect builds the two trash reads (ListTrashed / GetTrashed).
//
// It is a SEPARATE constant from formSelect, and its filter is the OPPOSITE
// one, on purpose: appending `deleted_at IS NOT NULL` to formSelect would make
// that query self-contradictory and every trash read would silently return
// nothing (the issue #98 regression class). The columns are formSelect's plus
// deleted_at, which is what the trash UI shows and what the purge confirmation
// needs to know.
const trashedFormSelect = `SELECT id, account_id, slug, title, config, theme, status, is_main, redirect_success, redirect_failure, allow_embed, logo_url, is_template, failure_message, start_at, end_at, entry_limit, pending_message, expired_message, limit_message, created_at, updated_at, deleted_at
	FROM forms WHERE deleted_at IS NOT NULL`

// scanTrashed scans a trashedFormSelect row (formSelect's columns + deleted_at).
func (s *FormStore) scanTrashed(row rowScanner) (*Form, error) {
	f := &Form{}
	err := row.Scan(&f.ID, &f.AccountID, &f.Slug, &f.Title, &f.Config, &f.Theme,
		&f.Status, &f.IsMain, &f.RedirectSuccess, &f.RedirectFailure,
		&f.AllowEmbed, &f.LogoURL, &f.IsTemplate, &f.FailureMessage, &f.StartAt, &f.EndAt,
		&f.EntryLimit, &f.PendingMessage, &f.ExpiredMessage, &f.LimitMessage,
		&f.CreatedAt, &f.UpdatedAt, &f.DeletedAt)
	return f, err
}

// ListTrashed lists soft-deleted forms for an account (explicit trash read —
// the only query in the store that may return deleted rows). It must NOT reuse
// formSelect, which already filters `deleted_at IS NULL`; appending
// `deleted_at IS NOT NULL` to it would make the query self-contradictory and
// the trash list would always be empty (issue #98 regression).
func (s *FormStore) ListTrashed(ctx context.Context, accountID string) ([]*Form, error) {
	rows, err := s.pool.Query(ctx, trashedFormSelect+` AND account_id=$1 ORDER BY deleted_at DESC`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Form
	for rows.Next() {
		f, serr := s.scanTrashed(rows)
		if serr != nil {
			return nil, serr
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetTrashed reads ONE trashed form, account-scoped. A live form, another
// account's form, or an unknown id is ErrFormNotFound — so this cannot be used
// as a side door to read or act on a live form, and it is the read the purge
// confirmation compares its typed slug against.
func (s *FormStore) GetTrashed(ctx context.Context, accountID, formID string) (*Form, error) {
	f, err := s.scanTrashed(s.pool.QueryRow(ctx,
		trashedFormSelect+` AND id=$2 AND account_id=$1`, accountID, formID))
	if isNoSuchRow(err) {
		return nil, ErrFormNotFound
	}
	if err != nil {
		return nil, err
	}
	return f, nil
}

// SalesCountsByForm returns, for every form of an account that has at least one
// sale, how many sales reference it — in ONE query, so the trash list can show
// the count and decide `can_purge` per row instead of N+1 queries.
//
// The map is keyed by form_id and omits forms with zero sales (a missing key IS
// zero).
func (s *FormStore) SalesCountsByForm(ctx context.Context, accountID string) (map[string]int64, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT form_id, COUNT(*) FROM sales
		  WHERE account_id=$1 AND form_id IS NOT NULL GROUP BY form_id`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var id string
		var n int64
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
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

// BlockedGalleryTemplates returns the ids in formIDs that a publish would skip
// because they are templates without a category or a description text block
// (issue #594). The bulk form action uses it to tell the caller which ids were
// left behind, so a skip is reported instead of looking like a silent no-op.
func (s *FormStore) BlockedGalleryTemplates(ctx context.Context, accountID string, formIDs []string) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id FROM forms
		  WHERE account_id=$1 AND deleted_at IS NULL AND id = ANY($2)
		    AND is_template AND (`+galleryMetadataMissingSQL+`)`,
		accountID, formIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SetStatusMany publishes/unpublishes many forms at once; returns how many
// rows were updated (account-scoped, live forms only).
//
// Publishing skips any template missing its gallery metadata rather than
// failing the whole batch (issue #594): the other ids in a bulk action are
// ordinary forms and must still be honoured. Rows skipped this way are counted
// by TemplateStatusBlocked, which the handler reports back to the caller.
func (s *FormStore) SetStatusMany(ctx context.Context, accountID string, formIDs []string, status string) (int64, error) {
	if !validFormStatus(status) {
		return 0, errors.New("status mesti draft|private|published|unlisted")
	}
	// galleryMetadataMissingSQL refers to `forms` columns via bare names, so
	// the UPDATE's table must stay named `forms` (no alias).
	if status == FormStatusPublished {
		tag, err := s.pool.Exec(ctx,
			`UPDATE forms SET status=$3, updated_at=now()
			 WHERE account_id=$1 AND deleted_at IS NULL AND id = ANY($2)
			   AND NOT (is_template AND (`+galleryMetadataMissingSQL+`))`,
			accountID, formIDs, status)
		if err != nil {
			return 0, err
		}
		return tag.RowsAffected(), nil
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE forms SET status=$3, updated_at=now()
		 WHERE account_id=$1 AND deleted_at IS NULL AND id = ANY($2)`,
		accountID, formIDs, status)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
