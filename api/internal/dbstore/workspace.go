// Workspace: multi-tenant storefront layer. Each user may own many workspaces;
// forms, products, sales and settings hang off a workspace.
package dbstore

import (
	"context"
	"encoding/json"

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
	WorkspaceID  string
	Name     string
	Slug     string
	Role     string
	Accounts []string
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
}

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
	WorkspaceID    string `json:"workspace_id"`
	FooterText string `json:"footer_text"`
	CompanyReg string `json:"company_reg"`
	PrivacyURL string `json:"privacy_url"`
	TermsURL   string `json:"terms_url"`
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
func (s *WorkspaceStore) Settings(ctx context.Context, workspaceID string) (*WorkspaceSettings, error) {
	var lpJSON string
	st := &WorkspaceSettings{WorkspaceID: workspaceID, MenuJSON: "[]", DefaultLang: "en", Currency: "MYR", Indexing: true}
	err := s.pool.QueryRow(ctx,
		`SELECT footer_text, company_reg, privacy_url, terms_url, refund_url, contact_phone, contact_address, business_name, menu_json::text, default_lang, currency, indexing, COALESCE(landing_page_json::text, '{}')
		   FROM workspace_settings WHERE workspace_id=$1`,
		workspaceID).Scan(&st.FooterText, &st.CompanyReg, &st.PrivacyURL, &st.TermsURL, &st.RefundURL, &st.ContactPhone, &st.ContactAddress, &st.BusinessName, &st.MenuJSON, &st.DefaultLang, &st.Currency, &st.Indexing, &lpJSON)
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
		`INSERT INTO workspace_settings (workspace_id, footer_text, company_reg, privacy_url, terms_url, refund_url, contact_phone, contact_address, business_name, menu_json, default_lang, currency, indexing, landing_page_json)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12,$13,$14::jsonb)
		 ON CONFLICT (workspace_id) DO UPDATE SET
		   footer_text=EXCLUDED.footer_text, company_reg=EXCLUDED.company_reg,
		   privacy_url=EXCLUDED.privacy_url, terms_url=EXCLUDED.terms_url,
		   refund_url=EXCLUDED.refund_url, contact_phone=EXCLUDED.contact_phone,
		   contact_address=EXCLUDED.contact_address, business_name=EXCLUDED.business_name,
		   menu_json=EXCLUDED.menu_json,
		   default_lang=EXCLUDED.default_lang, currency=EXCLUDED.currency,
		   indexing=EXCLUDED.indexing,
		   landing_page_json=EXCLUDED.landing_page_json`,
		st.WorkspaceID, st.FooterText, st.CompanyReg, st.PrivacyURL, st.TermsURL, st.RefundURL, st.ContactPhone, st.ContactAddress, st.BusinessName, st.MenuJSON, st.WorkspaceLang(), st.WorkspaceCurrency(), st.Indexing, lpJSON)
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
