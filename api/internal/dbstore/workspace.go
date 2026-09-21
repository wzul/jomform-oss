// Workspace: multi-tenant storefront layer. Each user may own many workspaces;
// forms, products, sales and settings hang off a workspace.
package dbstore

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Workspace models the workspaces table.
type Workspace struct {
	ID    string
	Name  string
	Slug  string
	Owner string
}

// WorkspaceStore wraps workspaces.
type WorkspaceStore struct{ pool *pgxpool.Pool }

func NewWorkspaceStore(pool *pgxpool.Pool) *WorkspaceStore { return &WorkspaceStore{pool: pool} }

const workspaceSelect = `SELECT id, owner_user_id, name, slug FROM workspaces`

func scanWorkspace(row rowScanner) (*Workspace, error) {
	s := &Workspace{}
	err := row.Scan(&s.ID, &s.Owner, &s.Name, &s.Slug)
	return s, err
}

// ByOwner lists workspaces owned by a user.
func (s *WorkspaceStore) ByOwner(ctx context.Context, userID string) ([]*Workspace, error) {
	rows, err := s.pool.Query(ctx, workspaceSelect+" WHERE owner_user_id=$1 ORDER BY created_at", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Workspace
	for rows.Next() {
		sp, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

// BySlug resolves a workspace by its URL slug.
func (s *WorkspaceStore) BySlug(ctx context.Context, slug string) (*Workspace, error) {
	return scanWorkspace(s.pool.QueryRow(ctx, workspaceSelect+" WHERE slug=$1", slug))
}

// ByID fetches one workspace.
func (s *WorkspaceStore) ByID(ctx context.Context, id string) (*Workspace, error) {
	return scanWorkspace(s.pool.QueryRow(ctx, workspaceSelect+" WHERE id=$1", id))
}

// Create makes a new workspace (slug unique) AND a fresh, empty account linked
// to it (issue #158). Each workspace owns its own account so a new workspace starts
// with a clean slate: no CHIP creds, no products, no sales, no domains, no
// webhooks. CHIP credentials are never shared across workspaces. The account
// slug is derived from the workspace slug (unique-verified by the caller) plus a
// short id suffix to avoid colliding with the workspace's own slug.
func (s *WorkspaceStore) Create(ctx context.Context, ownerUserID, name, slug string) (*Workspace, error) {
	sp := &Workspace{Owner: ownerUserID, Name: name, Slug: slug}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug) VALUES ($1,$2,$3)
		 RETURNING id, owner_user_id, name, slug`,
		ownerUserID, name, slug).Scan(&sp.ID, &sp.Owner, &sp.Name, &sp.Slug)
	if err != nil {
		return sp, err
	}
	// Fresh account owned by the workspace. Empty CHIP creds, no products/sales.
	// The account slug is unique-verified by the caller (workspace slug) plus a
	// short id suffix; a collision is extremely unlikely and the DB enforces
	// uniqueness as a backstop.
	accSlug := slug + "-" + sp.ID[:8]
	_, aerr := s.pool.Exec(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1,$2,$3,$4)`,
		ownerUserID, name, accSlug, sp.ID)
	if aerr != nil {
		return sp, aerr
	}
	return sp, nil
}

// WorkspaceAccess carries the account set + the caller's role for a workspace the
// user can access (owner or member). Role is one of RoleReadOnly/RoleManage/
// RoleAdmin (issue #162). Accounts holds every account linked to the workspace.
type WorkspaceAccess struct {
	WorkspaceID string
	Name        string
	Slug        string
	Role        string
	Accounts    []string
}

// AccessBySlug resolves the workspace's account ids + the caller's role when the
// user is the owner or a member of the workspace. ok=false when the workspace is
// unknown or the user has no access (issue #162 — members, not just owners).
func (s *WorkspaceStore) AccessBySlug(ctx context.Context, slug, userID string) (WorkspaceAccess, bool, error) {
	return s.access(ctx, `WHERE sp.slug = $1`, slug, userID)
}

// AccessByID resolves the workspace's account ids + the caller's role when the
// user is the owner or a member of the workspace.
func (s *WorkspaceStore) AccessByID(ctx context.Context, workspaceID, userID string) (WorkspaceAccess, bool, error) {
	return s.access(ctx, `WHERE sp.id = $1`, workspaceID, userID)
}

// access is the shared owner-or-member workspace resolution. The owner is always
// treated as an admin (the migration backfills owners as admin members, and
// new workspaces add the owner as a member at creation), so a workspace created
// before the backfill still resolves correctly.
func (s *WorkspaceStore) access(ctx context.Context, where string, key, userID string) (WorkspaceAccess, bool, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT sp.id, sp.name, sp.slug, sp.owner_user_id, a.id, COALESCE(m.role, '')
		FROM workspaces sp
		LEFT JOIN accounts a ON a.workspace_id = sp.id
		LEFT JOIN workspace_members m ON m.workspace_id = sp.id AND m.user_id = $2
		`+where, key, userID)
	if err != nil {
		return WorkspaceAccess{}, false, err
	}
	defer rows.Close()
	var out WorkspaceAccess
	found := false
	for rows.Next() {
		var owner, accID, role string
		if err := rows.Scan(&out.WorkspaceID, &out.Name, &out.Slug, &owner, &accID, &role); err != nil {
			return WorkspaceAccess{}, false, err
		}
		found = true
		if owner == userID {
			role = RoleAdmin
		}
		if role == "" {
			// Workspace exists but the user is neither owner nor a member.
			return WorkspaceAccess{}, false, nil
		}
		out.Role = role
		if accID != "" {
			out.Accounts = append(out.Accounts, accID)
		}
	}
	if err := rows.Err(); err != nil {
		return WorkspaceAccess{}, false, err
	}
	return out, found, nil
}

// WorkspaceBlocker is one category of content that prevents a workspace from
// being deleted, with the count that blocked it. The delete path returns the
// non-zero categories so the caller can list exactly what must be cleared
// instead of a bare "not empty".
type WorkspaceBlocker struct {
	// Kind is the machine-readable category: "forms", "products", "sales",
	// "domains", "webhooks", "api_keys" or "subscriptions".
	Kind string `json:"kind"`
	// Count is how many rows of that kind the workspace still owns.
	Count int64 `json:"count"`
	// Label is the human-readable category name shown in the refusal.
	Label string `json:"label"`
}

// WorkspaceDeleteError is returned by Delete when the workspace cannot be
// removed. Two shapes, both actionable:
//
//	reason "last_workspace" — the owner would be left with zero workspaces. A
//	   user must always have at least one, so this is the guard Wan asked for: it
//	   protects the workspace they started with without hard-coding which one
//	   that is.
//	reason "not_empty"     — the workspace still owns content. Blockers lists
//	   each category and its count, so the caller can clear them (trash + purge a
//	   form, archive a product, …) instead of guessing.
type WorkspaceDeleteError struct {
	Reason   string
	Blockers []WorkspaceBlocker
}

func (e *WorkspaceDeleteError) Error() string {
	if e.Reason == "last_workspace" {
		return "sekurang-kurangnya satu workspace diperlukan"
	}
	return "workspace tidak kosong"
}

// ErrWorkspaceNotEmpty reports whether err is the "still owns content" refusal,
// and returns the blocking categories.
func ErrWorkspaceNotEmpty(err error) ([]WorkspaceBlocker, bool) {
	var de *WorkspaceDeleteError
	if errors.As(err, &de) && de.Reason == "not_empty" {
		return de.Blockers, true
	}
	return nil, false
}

// IsLastWorkspaceError reports whether err is the "you would be left with no
// workspace" refusal.
func IsLastWorkspaceError(err error) bool {
	var de *WorkspaceDeleteError
	return errors.As(err, &de) && de.Reason == "last_workspace"
}

// emptyCheckSQL is the definition of "an empty workspace", in ONE place so the
// refusal message and the delete path can never disagree about it. A workspace
// is empty when ALL of these are zero:
//
//	forms         — INCLUDING trashed ones. A form in the trash is still a row
//	                in `forms`; it must be purged (trash → delete permanently)
//	                before the workspace can go. This is why the refusal message
//	                names the trash path.
//	products      — LIVE products only (active). The card asks that products
//	                block, and they do — but ONLY the ones the merchant is still
//	                selling. There is deliberately no product DELETE in the
//	                store (archiving is "dulang tanpa padam data"), so counting
//	                archived products too would make this guard UNSATISFIABLE and
//	                lock the merchant into a workspace they can never empty. An
//	                archived product is already withdrawn from sale; it is
//	                removed with the workspace below, exactly like the account.
//	                (A subscription is the one thing that could keep an archived
//	                product alive — and every subscription, of any status,
//	                blocks above, so that interaction is refused first.)
//	sales         — the trading history. Never destroyed by a workspace delete.
//	subscriptions — EVERY status, not just active. The FK is
//	                `subscriptions.account_id → accounts ON DELETE RESTRICT`
//	                (and `product_id → products RESTRICT`), so a CANCELLED
//	                subscription still blocks the account and product deletes.
//	                Counting only active/past_due would let the guard pass and
//	                then fail the DELETE with SQLSTATE 23503 — a raw 500 on a
//	                path the merchant was told was clear. Billing history joins
//	                sales as something this call never destroys.
//	domains, webhooks, api_keys — merchant-facing configuration. Domains and
//	                webhooks CASCADE at the DB level and live API keys CASCADE
//	                too, so these are a POLICY choice (the card asks for them):
//	                the merchant is told to clear configuration rather than have
//	                it vanish silently. Revoked keys are not counted — they are
//	                already dead — but they are still deleted below, because a
//	                revoked row keeps its workspace_id and that FK RESTRICTs.
//
// The account holding CHIP credentials is removed WITH the workspace, never
// shared, so it is not a blocker — it is the thing that must go.
//
// WHY THIS IS ONE ROW OF FILTERED COUNTS, NOT A UNION OF PER-CATEGORY ROWS.
// The repo has a single definition of "the merged record read" (sales +
// submissions), and a guard (httpapi TestMergedReadGoesThroughTheOneBuilder)
// refuses an explicit union of the two tables outside dbstore/records.go — a
// union in a second file is how that record set grew a second definition (the
// guard matches the SQL keyword, comments included, so this note names it only
// in prose). This query is not a record read; it is a per-category blocker
// census. Returning ONE row of seven counters keeps the shape impossible to
// mistake for the merged read, counts every category in a single scan of
// `forms`, and leaves each category's filter exactly as it was.
const emptyCheckSQL = `
	SELECT
	  COUNT(*) FILTER (WHERE TRUE) AS forms,
	  (SELECT COUNT(*) FROM products WHERE account_id = ANY($1) AND active) AS products,
	  (SELECT COUNT(*) FROM sales WHERE account_id = ANY($1)) AS sales,
	  (SELECT COUNT(*) FROM subscriptions WHERE account_id = ANY($1)) AS subscriptions,
	  (SELECT COUNT(*) FROM account_domains WHERE account_id = ANY($1)) AS domains,
	  (SELECT COUNT(*) FROM outbound_webhooks WHERE account_id = ANY($1) AND active) AS webhooks,
	  (SELECT COUNT(*) FROM api_keys
	    WHERE account_id = ANY($1) AND revoked_at IS NULL
	      AND id <> COALESCE($2::uuid, '00000000-0000-0000-0000-000000000000'::uuid)) AS api_keys
	FROM forms WHERE account_id = ANY($1)`

// emptyCheckArgs builds the parameter list for emptyCheckSQL: the account ids,
// and the API key (if any) that is EXEMPT from the live-key blocker.
//
// WHY THE EXEMPTION EXISTS. The live-key blocker stops a workspace being
// destroyed while credentials still point at it. But the delete revokes every
// key on the account anyway, so no live key can survive it — and without the
// exemption the check is self-defeating: whoever deletes the workspace is
// authenticated by a key, that key is live, so every key-authenticated caller
// (i.e. every MCP caller) would be refused. That is a lock, not a guard. Exactly
// ONE key is ignored, and only when it is the very key making the call.
func emptyCheckArgs(accIDs []string, exemptKeyID string) []any {
	return []any{accIDs, nullableKeyID(exemptKeyID)}
}

// nullableKeyID turns an empty key id into a nil, so the COALESCE in
// emptyCheckSQL handles "no key on the call" (a session caller) without a
// second statement.
func nullableKeyID(id string) any {
	if id == "" {
		return nil
	}
	return id
}

// blockerLabels names each blocker for the refusal message.
var blockerLabels = map[string]string{
	"forms":         "borang (termasuk yang dalam tong sampah)",
	"products":      "produk",
	"sales":         "jualan",
	"subscriptions": "langganan (sejarah bil)",
	"domains":       "domain tersuai",
	"webhooks":      "webhook",
	"api_keys":      "kunci API",
}

// CountEmpty reports what still blocks the workspace from being deleted — the
// non-zero categories only, ordered as in emptyCheckSQL. An empty slice means
// the workspace may be deleted.
//
// This is a READ, deliberately separate from Delete: the handler calls it first
// so a refusal can list every blocking category with its count, instead of
// discovering the first obstacle and stopping.
func (s *WorkspaceStore) CountEmpty(ctx context.Context, workspaceID, exemptKeyID string) (int64, []WorkspaceBlocker, error) {
	accs, err := s.accountIDs(ctx, workspaceID)
	if err != nil {
		return 0, nil, err
	}
	var forms, products, sales, subs, domains, webhooks, keys int64
	err = s.pool.QueryRow(ctx, emptyCheckSQL, emptyCheckArgs(accs, exemptKeyID)...).
		Scan(&forms, &products, &sales, &subs, &domains, &webhooks, &keys)
	if err != nil {
		return 0, nil, err
	}
	return int64(len(accs)), blockersFrom(forms, products, sales, subs, domains, webhooks, keys), nil
}

// blockersFrom turns the seven counters into the non-zero blocker list, in the
// order emptyCheckSQL returns them. ONE place, so the delete path and the read
// path can never disagree about which categories blocked or how they are named.
func blockersFrom(forms, products, sales, subs, domains, webhooks, keys int64) []WorkspaceBlocker {
	var out []WorkspaceBlocker
	for _, c := range []struct {
		kind  string
		count int64
	}{
		{"forms", forms}, {"products", products}, {"sales", sales},
		{"subscriptions", subs}, {"domains", domains},
		{"webhooks", webhooks}, {"api_keys", keys},
	} {
		if c.count > 0 {
			out = append(out, WorkspaceBlocker{Kind: c.kind, Count: c.count, Label: blockerLabels[c.kind]})
		}
	}
	return out
}

// accountIDs returns every account id linked to a workspace.
func (s *WorkspaceStore) accountIDs(ctx context.Context, workspaceID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, workspaceID)
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

// Delete removes a workspace and everything that belongs to it, refusing when
// either guard says no.
//
// exemptKeyID is the id of the API key authenticating the call, or "" for a
// session caller. See emptyCheckArgs for why exactly one live key may be
// ignored by the emptiness guard.
//
// WHY THIS IS NOT ONE `DELETE FROM workspaces`. The foreign keys on the live
// schema make that statement FAIL (SQLSTATE 23503) on any workspace holding an
// account — accounts.workspace_id, api_keys.workspace_id, forms.workspace_id,
// products.workspace_id and sales.workspace_id are all NO ACTION (RESTRICT),
// while only the seven child tables (workspace_settings, workspace_members,
// workspace_invites, workspace_legal_pages, workspace_email_settings,
// affiliates) cascade. Every workspace MUST own an account (migration 000024),
// so the naive delete works on literally nothing.
//
// The order below is what makes the cascade reach the workspace: the RESTRICT
// rows are removed deepest-first (sales → their subscription/charge children
// cascade; then forms, products, api_keys; then the accounts), and only then the
// workspaces row, whose own children fall away by CASCADE.
//
// The accounts go WITH the workspace: an account holds CHIP credentials, and
// the repo invariant is that CHIP is never shared between workspaces, so an
// account orphaned by a workspace delete would be a credential nobody owns.
//
// Guards (both checked inside the transaction, so nothing is deleted when
// either refuses):
//
//	last_workspace — the owner would be left with zero workspaces. The rule is
//	   "a user always has at least one", expressed as a COUNT rather than by
//	   pinning the workspace they started with, so it protects whichever
//	   workspace is last without a "is this the original one" flag.
//	not_empty      — CountEmpty reports blockers; the workspace is untouched.
//
// Returns the number of rows removed per table for the audit trail.
func (s *WorkspaceStore) Delete(ctx context.Context, workspaceID, ownerUserID, exemptKeyID string) (map[string]int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock the workspace row: two concurrent deletes of the same workspace
	// would otherwise both pass the guards and the second would fail late with a
	// raw FK error.
	var owner string
	if err := tx.QueryRow(ctx,
		`SELECT owner_user_id FROM workspaces WHERE id=$1 FOR UPDATE`,
		workspaceID).Scan(&owner); isNoSuchRow(err) {
		return nil, ErrWorkspaceNotFound
	} else if err != nil {
		return nil, err
	}

	// Guard 1 — at least one workspace must remain. Counted on the owner's
	// membership (owned OR member), which is exactly what the switcher lists, so
	// a user who is a member of someone else's workspace still has one to land
	// on and may delete their own last one.
	var remaining int64
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM workspaces sp
		 WHERE sp.id <> $1
		   AND (sp.owner_user_id = $2
		        OR EXISTS (SELECT 1 FROM workspace_members m
		                    WHERE m.workspace_id = sp.id AND m.user_id = $2))`,
		workspaceID, ownerUserID).Scan(&remaining); err != nil {
		return nil, err
	}
	if remaining == 0 {
		return nil, &WorkspaceDeleteError{Reason: "last_workspace"}
	}

	// Guard 2 — the workspace must be empty, using the ONE definition.
	var accIDs []string
	{
		rows, qerr := tx.Query(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, workspaceID)
		if qerr != nil {
			return nil, qerr
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			accIDs = append(accIDs, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	if len(accIDs) > 0 {
		var forms, products, sales, subs, domains, webhooks, keys int64
		qerr := tx.QueryRow(ctx, emptyCheckSQL, emptyCheckArgs(accIDs, exemptKeyID)...).
			Scan(&forms, &products, &sales, &subs, &domains, &webhooks, &keys)
		if qerr != nil {
			return nil, qerr
		}
		if blockers := blockersFrom(forms, products, sales, subs, domains, webhooks, keys); len(blockers) > 0 {
			return nil, &WorkspaceDeleteError{Reason: "not_empty", Blockers: blockers}
		}
	}

	deleted := map[string]int64{}
	// Step 1: trading history first — sales/subscriptions RESTRICT their
	// account, and subscription_charges CASCADE off their subscription. Only
	// counts of zero can reach here (guard 2), but the statement is written as
	// a delete so a race that inserts a sale between the check and here cannot
	// leave the account behind.
	for _, step := range []struct {
		table string
		sql   string
	}{
		{"sales", `DELETE FROM sales WHERE account_id = ANY($1)`},
		{"forms", `DELETE FROM forms WHERE account_id = ANY($1)`},
		{"products", `DELETE FROM products WHERE account_id = ANY($1)`},
		{"api_keys", `DELETE FROM api_keys WHERE account_id = ANY($1)`},
	} {
		tag, derr := tx.Exec(ctx, step.sql, accIDs)
		if derr != nil {
			return nil, derr
		}
		deleted[step.table] = tag.RowsAffected()
	}
	// Step 2: the accounts (they hold the CHIP credentials; never orphaned).
	if len(accIDs) > 0 {
		tag, derr := tx.Exec(ctx, `DELETE FROM accounts WHERE workspace_id=$1`, workspaceID)
		if derr != nil {
			return nil, derr
		}
		deleted["accounts"] = tag.RowsAffected()
	}
	// Step 3: the workspace itself. workspace_settings / workspace_members /
	// workspace_invites / workspace_legal_pages / workspace_email_settings /
	// affiliates go with it by CASCADE. users.default_workspace_id is SET NULL
	// by its FK, so no dangling default is left behind.
	tag, derr := tx.Exec(ctx, `DELETE FROM workspaces WHERE id=$1`, workspaceID)
	if derr != nil {
		return nil, derr
	}
	deleted["workspaces"] = tag.RowsAffected()
	if deleted["workspaces"] == 0 {
		return nil, ErrWorkspaceNotFound
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return deleted, nil
}

// ErrWorkspaceNotFound means the workspace id named no row.
var ErrWorkspaceNotFound = errors.New("workspace not found")

// ByMember lists workspaces the user owns OR is a member of, with the caller's
// role in each (issue #162). Used by the workspace switcher so members see the
// workspaces they belong to, not just the ones they own.
func (s *WorkspaceStore) ByMember(ctx context.Context, userID string) ([]WorkspaceAccess, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT sp.id, sp.name, sp.slug, sp.owner_user_id, COALESCE(m.role, ''), a.id
		FROM workspaces sp
		LEFT JOIN workspace_members m ON m.workspace_id = sp.id AND m.user_id = $1
		LEFT JOIN accounts a ON a.workspace_id = sp.id
		WHERE sp.owner_user_id = $1 OR m.user_id = $1
		ORDER BY sp.created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkspaceAccess
	seen := map[string]int{}
	for rows.Next() {
		var a WorkspaceAccess
		var owner, role, accID string
		if err := rows.Scan(&a.WorkspaceID, &a.Name, &a.Slug, &owner, &role, &accID); err != nil {
			return nil, err
		}
		if owner == userID {
			role = RoleAdmin
		}
		a.Role = role
		if i, ok := seen[a.WorkspaceID]; ok {
			if accID != "" {
				out[i].Accounts = append(out[i].Accounts, accID)
			}
			continue
		}
		seen[a.WorkspaceID] = len(out)
		if accID != "" {
			a.Accounts = append(a.Accounts, accID)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// LandingPage is the customizable workspace landing-page design (issue #360). The
// storefront root ({workspace}.jomform.com and custom domains) renders a styled
// landing page from this config when Enabled. When no config is set (or
// Enabled=false) handleWorkspaceRoot falls back to the auto-generated bare listing.
type LandingPage struct {
	Enabled bool `json:"enabled"`
	// Hero is the main heading / hero title.
	Hero string `json:"hero"`
	// Subtitle is the hero subheading.
	Subtitle string `json:"subtitle"`
	// Body is the landing-page body text (plain paragraphs).
	Body string `json:"body"`
	// ImageURL is an optional banner/hero image.
	ImageURL string `json:"image_url"`
	// PrimaryColor is the theme accent color (hex, e.g. "#7c3aed").
	PrimaryColor string `json:"primary_color"`
	// BackgroundColor is the page background (hex). Empty = default.
	BackgroundColor string `json:"background_color"`
	// LogoURL is an optional brand logo image.
	LogoURL string `json:"logo_url"`
	// CTAs are buttons linking to published forms (label + form slug).
	CTAs []LandingCTA `json:"ctas"`
	// Forms are published forms embedded inline on the landing page (issue
	// #362). Each embed renders the full JomForm form (fields + validation +
	// submission) same-origin via an iframe to /{slug}, so RSVP-style forms
	// submit straight into the form's submissions/sales exactly as a direct
	// visit would. Stored in the landing_page_json blob.
	Forms []LandingEmbed `json:"forms"`
	// Issue #438 shipped a per-workspace restaurant menu here (LandingPage.Menu,
	// rendered at the workspace ROOT). Issue #497 moved the menu onto the FORM
	// (forms.config.menu) because the workspace-level shape hijacked the whole
	// storefront, sold every account product, and allowed only one menu per
	// workspace. The field is retained ONLY so a stray/legacy
	// landing_page_json.menu is parsed and safely ignored — nothing reads it.
	//
	// Deprecated: use MenuFromConfig(form.Config) / WithMenu instead.
	LegacyMenu *MenuPage `json:"menu,omitempty"`
}

// MenuPage, MenuCategory, IsBestSeller, BestSellerProductIDs and the
// config helpers now live in form_menu.go — issue #497 moved the restaurant
// menu onto the FORM. See MenuFromConfig / WithMenu.

// LandingCTA is a landing-page call-to-action button linking to a public form.
type LandingCTA struct {
	Label string `json:"label"`
	// FormSlug is the published form's slug within the workspace; rendered as a
	// relative link that resolves on the workspace subdomain / custom domain.
	FormSlug string `json:"form_slug"`
}

// LandingEmbed is a published form rendered inline on the landing page (issue
// #362). FormSlug must resolve to a published form in the same workspace; the
// server only renders embed blocks whose slug matches a published form, so a
// draft/private/unknown slug is never shown.
type LandingEmbed struct {
	// Label is an optional heading shown above the embedded form.
	Label string `json:"label"`
	// FormSlug is the published form to embed (e.g. "rsvp").
	FormSlug string `json:"form_slug"`
}

// WorkspaceSettings is the storefront configuration for one workspace.
type WorkspaceSettings struct {
	WorkspaceID string `json:"workspace_id"`
	FooterText  string `json:"footer_text"`
	CompanyReg  string `json:"company_reg"`
	PrivacyURL  string `json:"privacy_url"`
	TermsURL    string `json:"terms_url"`
	// Issue #183: optional public-form footer fields.
	RefundURL      string `json:"refund_url"`
	ContactPhone   string `json:"contact_phone"`
	ContactAddress string `json:"contact_address"`
	BusinessName   string `json:"business_name"`
	MenuJSON       string `json:"menu_json"`
	DefaultLang    string `json:"default_lang"`
	// Currency is the workspace's settlement/display currency (issue #185).
	// Default MYR. The workspace's account(s) inherit it for CHIP purchases; new
	// amounts are stored in this currency's minor units. Historical sales keep
	// their original currency (never auto-converted).
	Currency string `json:"currency"`
	// Indexing is the per-workspace SEO toggle (issue #276). When true (default)
	// the workspace's public storefront + forms are eligible for search-engine /
	// AI indexing on the jomform.com apex and on custom domains. When false
	// the workspace opts out of indexing entirely. Public forms on jomform.com
	// subdomains are always noindex regardless of this flag (privacy).
	Indexing bool `json:"indexing"`
	// LandingPage is the customizable landing-page config (issue #360).
	// Stored as landing_page_json on workspace_settings. When nil/empty the
	// storefront root falls back to the auto-generated bare listing.
	LandingPage *LandingPage `json:"landing_page,omitempty"`
	// SoundEnabled is the per-workspace realtime notification sound toggle
	// (issue #447). When true, the merchant's dashboard plays an audio beep
	// when a new sale/response arrives over the SSE stream. Default false
	// (muted) so no merchant is surprised by sound.
	SoundEnabled bool `json:"sound_enabled"`
	// SpamJudgeEnabled is the per-workspace AI spam-judge toggle.
	// When true (the default) the background judge (issue #245) is enqueued for
	// every public submission/sale. When false NO judge job is enqueued at all
	// (the gate is on the enqueue, not in the worker, so no AI work is spent),
	// and is_spam is therefore never set — the workspace's /spam queue stays
	// empty. True by default so enabling the feature changes no existing
	// workspace's behaviour.
	SpamJudgeEnabled bool `json:"spam_judge_enabled"`
}

// WorkspaceLang returns the storefront default language code, or a fallback when
// unset. Callers must use this so a not-yet-migrated or legacy row resolves
// to 'en' (maintainer decision 2026-09-01), never "".
func (st *WorkspaceSettings) WorkspaceLang() string {
	switch st.DefaultLang {
	case "en", "ms", "zh", "ta", "ar":
		return st.DefaultLang
	default:
		return "en"
	}
}

// WorkspaceCurrency returns the workspace's currency code, or MYR when unset/unknown.
// Callers must use this so a not-yet-migrated or legacy row resolves to MYR
// (issue #185), never "".
func (st *WorkspaceSettings) WorkspaceCurrency() string {
	if IsSupportedCurrency(st.Currency) {
		return st.Currency
	}
	return "MYR"
}

// Settings reads (creating default row when missing) the storefront config.
// The fallback literal mirrors the migration defaults so a workspace whose row
// does not exist yet behaves exactly like a migrated one: indexing on and the
// AI spam judge on (issue #276 / this change).
func (s *WorkspaceStore) Settings(ctx context.Context, workspaceID string) (*WorkspaceSettings, error) {
	var lpJSON string
	st := &WorkspaceSettings{WorkspaceID: workspaceID, MenuJSON: "[]", DefaultLang: "en", Currency: "MYR", Indexing: true, SpamJudgeEnabled: true}
	err := s.pool.QueryRow(ctx,
		`SELECT footer_text, company_reg, privacy_url, terms_url, refund_url, contact_phone, contact_address, business_name, menu_json::text, default_lang, currency, indexing, COALESCE(landing_page_json::text, '{}'), sound_enabled, spam_judge_enabled
		   FROM workspace_settings WHERE workspace_id=$1`,
		workspaceID).Scan(&st.FooterText, &st.CompanyReg, &st.PrivacyURL, &st.TermsURL, &st.RefundURL, &st.ContactPhone, &st.ContactAddress, &st.BusinessName, &st.MenuJSON, &st.DefaultLang, &st.Currency, &st.Indexing, &lpJSON, &st.SoundEnabled, &st.SpamJudgeEnabled)
	if err != nil {
		// default row
		_, ierr := s.pool.Exec(ctx,
			`INSERT INTO workspace_settings (workspace_id) VALUES ($1) ON CONFLICT DO NOTHING`, workspaceID)
		if ierr != nil {
			return nil, ierr
		}
		lpJSON = "{}"
	}
	// Parse the landing-page config; a missing/bad payload is treated as "no
	// landing page" so the storefront keeps falling back to the bare listing.
	if lpJSON != "" && lpJSON != "{}" {
		var lp LandingPage
		if jerr := json.Unmarshal([]byte(lpJSON), &lp); jerr == nil {
			st.LandingPage = &lp
		}
	}
	return st, nil
}

// SaveSettings upserts the storefront config.
func (s *WorkspaceStore) SaveSettings(ctx context.Context, st *WorkspaceSettings) error {
	var lpJSON string
	if st.LandingPage != nil {
		b, err := json.Marshal(st.LandingPage)
		if err != nil {
			return err
		}
		lpJSON = string(b)
	} else {
		lpJSON = "{}"
	}
	if lpJSON == "" || lpJSON == "null" {
		lpJSON = "{}"
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO workspace_settings (workspace_id, footer_text, company_reg, privacy_url, terms_url, refund_url, contact_phone, contact_address, business_name, menu_json, default_lang, currency, indexing, landing_page_json, sound_enabled, spam_judge_enabled)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12,$13,$14::jsonb,$15,$16)
		 ON CONFLICT (workspace_id) DO UPDATE SET
		   footer_text=EXCLUDED.footer_text, company_reg=EXCLUDED.company_reg,
		   privacy_url=EXCLUDED.privacy_url, terms_url=EXCLUDED.terms_url,
		   refund_url=EXCLUDED.refund_url, contact_phone=EXCLUDED.contact_phone,
		   contact_address=EXCLUDED.contact_address, business_name=EXCLUDED.business_name,
		   menu_json=EXCLUDED.menu_json,
		   default_lang=EXCLUDED.default_lang, currency=EXCLUDED.currency,
		   indexing=EXCLUDED.indexing,
		   landing_page_json=EXCLUDED.landing_page_json,
		   sound_enabled=EXCLUDED.sound_enabled,
		   spam_judge_enabled=EXCLUDED.spam_judge_enabled`,
		st.WorkspaceID, st.FooterText, st.CompanyReg, st.PrivacyURL, st.TermsURL, st.RefundURL, st.ContactPhone, st.ContactAddress, st.BusinessName, st.MenuJSON, st.WorkspaceLang(), st.WorkspaceCurrency(), st.Indexing, lpJSON, st.SoundEnabled, st.SpamJudgeEnabled)
	return err
}

// WorkspaceCurrencyByAccount resolves the currency of the workspace that owns the
// given account ("" when the account has no linked workspace). Used by the
// payments path to pick the CHIP purchase currency from the workspace (issue
// #185) instead of the account-level default.
func (s *WorkspaceStore) WorkspaceCurrencyByAccount(ctx context.Context, accountID string) (string, error) {
	var cur string
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(ss.currency, 'MYR')
		   FROM accounts a
		   LEFT JOIN workspace_settings ss ON ss.workspace_id = a.workspace_id
		  WHERE a.id=$1`, accountID).Scan(&cur)
	if err != nil {
		return "", err
	}
	return cur, nil
}

// IndexingByAccount resolves whether the workspace owning the given account has
// indexing enabled (issue #276). Defaults to true when the account has no
// linked workspace (legacy) or the setting is unset. Used by the public form
// renderer to decide whether a custom-domain form may be indexed.
func (s *WorkspaceStore) IndexingByAccount(ctx context.Context, accountID string) (bool, error) {
	var idx bool
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(ss.indexing, TRUE)
		   FROM accounts a
		   LEFT JOIN workspace_settings ss ON ss.workspace_id = a.workspace_id
		  WHERE a.id=$1`, accountID).Scan(&idx)
	if err != nil {
		return true, err
	}
	return idx, nil
}

// SpamJudgeEnabledByAccount resolves whether the workspace owning the given
// account has the AI spam judge enabled. Defaults to true when the
// account has no linked workspace (legacy) or the setting is unset — the same
// "on unless explicitly turned off" contract the migration default gives a
// migrated row. Used by the public submit path to decide whether to enqueue a
// spam-judge job AT ALL (the gate is on the enqueue, so a disabled judge costs
// no AI work).
//
// The bool returned alongside the error is the FAIL DIRECTION, and it is true
// on error: the judge exists to remove work, so a database hiccup must not
// silently stop spam protection for every workspace, and running the judge is
// the pre-existing behaviour — an error therefore cannot change behaviour. The
// caller logs the error; it must not fail the submission on it.
func (s *WorkspaceStore) SpamJudgeEnabledByAccount(ctx context.Context, accountID string) (bool, error) {
	var on bool
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(ss.spam_judge_enabled, TRUE)
		   FROM accounts a
		   LEFT JOIN workspace_settings ss ON ss.workspace_id = a.workspace_id
		  WHERE a.id=$1`, accountID).Scan(&on)
	if err != nil {
		return true, err
	}
	return on, nil
}

// IndexedWorkspaceURLs returns the custom-domain URLs of workspaces that have indexing
// enabled (issue #276). Only verified custom domains count; the jomform.com
// apex is handled by the web app. Used to build sitemap.xml.
func (s *WorkspaceStore) IndexedWorkspaceURLs(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT ad.domain
		   FROM workspace_settings ss
		   JOIN accounts a ON a.workspace_id = ss.workspace_id
		   JOIN account_domains ad ON ad.account_id = a.id
		  WHERE ss.indexing = TRUE AND ad.verified = TRUE
		  ORDER BY ad.domain`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// IDBySlug resolves a workspace id from its slug ("" when unknown).
func (s *WorkspaceStore) IDBySlug(ctx context.Context, slug string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `SELECT id FROM workspaces WHERE slug=$1`, slug).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, nil
}

// WorkspaceIDByAccount resolves the workspace id a form's account belongs to ("" when
// the account has no linked workspace). Rendering falls back to 'en' in that case.
func (s *WorkspaceStore) WorkspaceIDByAccount(ctx context.Context, accountID string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx,
		`SELECT a.workspace_id FROM accounts a WHERE a.id=$1`, accountID).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, nil
}

// RoleByAccount returns the role a user holds in the workspace that owns the given
// account ("" when the account has no workspace or the user is not a member).
// The account owner is always treated as an admin (issue #162).
func (s *WorkspaceStore) RoleByAccount(ctx context.Context, accountID, userID string) (string, error) {
	var owner, role string
	err := s.pool.QueryRow(ctx, `
		SELECT a.owner_user_id, COALESCE(m.role, '')
		FROM accounts a
		LEFT JOIN workspace_members m ON m.workspace_id = a.workspace_id AND m.user_id = $2
		WHERE a.id = $1`, accountID, userID).Scan(&owner, &role)
	if err != nil {
		return "", err
	}
	if owner == userID {
		return RoleAdmin, nil
	}
	return role, nil
}

// PublicHostSlugByAccount resolves the subdomain a form's account is publicly
// reachable under, for building {slug}.jomform.com/{form-slug} URLs.
//
// It resolves the way the PUBLIC REQUEST PATH resolves, which is the invariant
// that matters: form serving (FormStore.ByWorkspaceSlug, reached from
// accountSubdomain + handleWorkspaceRootForm) accepts EITHER the account's own
// slug (accounts.slug) OR its workspace's slug (workspaces.slug) — it tries the
// account slug first and falls back to the workspace join. A URL builder that
// resolved ONLY through the workspace join therefore disagreed with the server
// that serves the URL, and answered "" whenever accounts.workspace_id was NULL.
//
// accounts.workspace_id is NULL for every account created through signup
// (AccountStore.Create does not set it; only WorkspaceStore.Create does), so
// that empty answer was the norm, not an edge case: it produced an empty href on
// the dashboard's "View" action, an empty url on MCP list_forms, and a receipt
// email/PDF whose form link fell back to the apex thank-you page.
//
// So: prefer the workspace slug (a merchant who named their workspace keeps the
// pretty host), and fall back to the account slug — the host the serving path
// resolves second — instead of degrading to "". Both branches yield a host the
// public server actually serves; "" is now returned only when the account row
// itself cannot be read.
//
// NOT for legal-page hosts ({slug}.jomform.com/privacy|terms|refund): those are
// resolved by workspaces.IDBySlug alone (handlePublicLegalPage), so a legal URL
// must keep using WorkspaceSlugByAccount — see rewriteHostedLegalURL, which
// leaves a stored legal URL untouched when no workspace slug resolves rather
// than rewriting it to a host that would 404.
func (s *WorkspaceStore) PublicHostSlugByAccount(ctx context.Context, accountID string) (string, error) {
	var workspaceSlug, accountSlug string
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(sp.slug, ''), a.slug
		   FROM accounts a
		   LEFT JOIN workspaces sp ON sp.id = a.workspace_id
		  WHERE a.id=$1`, accountID).Scan(&workspaceSlug, &accountSlug)
	if err != nil {
		return "", err
	}
	if workspaceSlug != "" {
		return workspaceSlug, nil
	}
	return accountSlug, nil
}

// WorkspaceSlugByAccount resolves the workspace slug a form's account belongs to
// ("" when the account has no linked workspace). Used to build the canonical
// workspace-scoped public URL {workspace}.jomform.com/{slug} (issue #77).
func (s *WorkspaceStore) WorkspaceSlugByAccount(ctx context.Context, accountID string) (string, error) {
	var slug string
	err := s.pool.QueryRow(ctx,
		`SELECT sp.slug FROM accounts a JOIN workspaces sp ON sp.id = a.workspace_id
		  WHERE a.id=$1`, accountID).Scan(&slug)
	if err != nil {
		return "", err
	}
	return slug, nil
}
