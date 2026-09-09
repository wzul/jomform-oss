// Space: multi-tenant storefront layer. Each user may own many spaces;
// forms, products, sales and settings hang off a space.
package dbstore

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Space models the spaces table.
type Space struct {
	ID    string
	Name  string
	Slug  string
	Owner string
}

// SpaceStore wraps spaces.
type SpaceStore struct{ pool *pgxpool.Pool }

func NewSpaceStore(pool *pgxpool.Pool) *SpaceStore { return &SpaceStore{pool: pool} }

const spaceSelect = `SELECT id, owner_user_id, name, slug FROM spaces`

func scanSpace(row rowScanner) (*Space, error) {
	s := &Space{}
	err := row.Scan(&s.ID, &s.Owner, &s.Name, &s.Slug)
	return s, err
}

// ByOwner lists spaces owned by a user.
func (s *SpaceStore) ByOwner(ctx context.Context, userID string) ([]*Space, error) {
	rows, err := s.pool.Query(ctx, spaceSelect+" WHERE owner_user_id=$1 ORDER BY created_at", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Space
	for rows.Next() {
		sp, err := scanSpace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

// BySlug resolves a space by its URL slug.
func (s *SpaceStore) BySlug(ctx context.Context, slug string) (*Space, error) {
	return scanSpace(s.pool.QueryRow(ctx, spaceSelect+" WHERE slug=$1", slug))
}

// ByID fetches one space.
func (s *SpaceStore) ByID(ctx context.Context, id string) (*Space, error) {
	return scanSpace(s.pool.QueryRow(ctx, spaceSelect+" WHERE id=$1", id))
}

// Create makes a new space (slug unique) AND a fresh, empty account linked
// to it (issue #158). Each space owns its own account so a new space starts
// with a clean slate: no CHIP creds, no products, no sales, no domains, no
// webhooks. CHIP credentials are never shared across spaces. The account
// slug is derived from the space slug (unique-verified by the caller) plus a
// short id suffix to avoid colliding with the space's own slug.
func (s *SpaceStore) Create(ctx context.Context, ownerUserID, name, slug string) (*Space, error) {
	sp := &Space{Owner: ownerUserID, Name: name, Slug: slug}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO spaces (owner_user_id, name, slug) VALUES ($1,$2,$3)
		 RETURNING id, owner_user_id, name, slug`,
		ownerUserID, name, slug).Scan(&sp.ID, &sp.Owner, &sp.Name, &sp.Slug)
	if err != nil {
		return sp, err
	}
	// Fresh account owned by the space. Empty CHIP creds, no products/sales.
	// The account slug is unique-verified by the caller (space slug) plus a
	// short id suffix; a collision is extremely unlikely and the DB enforces
	// uniqueness as a backstop.
	accSlug := slug + "-" + sp.ID[:8]
	_, aerr := s.pool.Exec(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, space_id)
		 VALUES ($1,$2,$3,$4)`,
		ownerUserID, name, accSlug, sp.ID)
	if aerr != nil {
		return sp, aerr
	}
	return sp, nil
}

// SpaceAccess carries the account set + the caller's role for a space the
// user can access (owner or member). Role is one of RoleReadOnly/RoleManage/
// RoleAdmin (issue #162). Accounts holds every account linked to the space.
type SpaceAccess struct {
	SpaceID  string
	Name     string
	Slug     string
	Role     string
	Accounts []string
}

// AccessBySlug resolves the space's account ids + the caller's role when the
// user is the owner or a member of the space. ok=false when the space is
// unknown or the user has no access (issue #162 — members, not just owners).
func (s *SpaceStore) AccessBySlug(ctx context.Context, slug, userID string) (SpaceAccess, bool, error) {
	return s.access(ctx, `WHERE sp.slug = $1`, slug, userID)
}

// AccessByID resolves the space's account ids + the caller's role when the
// user is the owner or a member of the space.
func (s *SpaceStore) AccessByID(ctx context.Context, spaceID, userID string) (SpaceAccess, bool, error) {
	return s.access(ctx, `WHERE sp.id = $1`, spaceID, userID)
}

// access is the shared owner-or-member space resolution. The owner is always
// treated as an admin (the migration backfills owners as admin members, and
// new spaces add the owner as a member at creation), so a space created
// before the backfill still resolves correctly.
func (s *SpaceStore) access(ctx context.Context, where string, key, userID string) (SpaceAccess, bool, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT sp.id, sp.name, sp.slug, sp.owner_user_id, a.id, COALESCE(m.role, '')
		FROM spaces sp
		LEFT JOIN accounts a ON a.space_id = sp.id
		LEFT JOIN space_members m ON m.space_id = sp.id AND m.user_id = $2
		`+where, key, userID)
	if err != nil {
		return SpaceAccess{}, false, err
	}
	defer rows.Close()
	var out SpaceAccess
	found := false
	for rows.Next() {
		var owner, accID, role string
		if err := rows.Scan(&out.SpaceID, &out.Name, &out.Slug, &owner, &accID, &role); err != nil {
			return SpaceAccess{}, false, err
		}
		found = true
		if owner == userID {
			role = RoleAdmin
		}
		if role == "" {
			// Space exists but the user is neither owner nor a member.
			return SpaceAccess{}, false, nil
		}
		out.Role = role
		if accID != "" {
			out.Accounts = append(out.Accounts, accID)
		}
	}
	if err := rows.Err(); err != nil {
		return SpaceAccess{}, false, err
	}
	return out, found, nil
}

// ByMember lists spaces the user owns OR is a member of, with the caller's
// role in each (issue #162). Used by the space switcher so members see the
// spaces they belong to, not just the ones they own.
func (s *SpaceStore) ByMember(ctx context.Context, userID string) ([]SpaceAccess, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT sp.id, sp.name, sp.slug, sp.owner_user_id, COALESCE(m.role, ''), a.id
		FROM spaces sp
		LEFT JOIN space_members m ON m.space_id = sp.id AND m.user_id = $1
		LEFT JOIN accounts a ON a.space_id = sp.id
		WHERE sp.owner_user_id = $1 OR m.user_id = $1
		ORDER BY sp.created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SpaceAccess
	seen := map[string]int{}
	for rows.Next() {
		var a SpaceAccess
		var owner, role, accID string
		if err := rows.Scan(&a.SpaceID, &a.Name, &a.Slug, &owner, &role, &accID); err != nil {
			return nil, err
		}
		if owner == userID {
			role = RoleAdmin
		}
		a.Role = role
		if i, ok := seen[a.SpaceID]; ok {
			if accID != "" {
				out[i].Accounts = append(out[i].Accounts, accID)
			}
			continue
		}
		seen[a.SpaceID] = len(out)
		if accID != "" {
			a.Accounts = append(a.Accounts, accID)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// LandingPage is the customizable space landing-page design (issue #360). The
// storefront root ({space}.jomform.com and custom domains) renders a styled
// landing page from this config when Enabled. When no config is set (or
// Enabled=false) handleSpaceRoot falls back to the auto-generated bare listing.
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
	// FormSlug is the published form's slug within the space; rendered as a
	// relative link that resolves on the space subdomain / custom domain.
	FormSlug string `json:"form_slug"`
}

// LandingEmbed is a published form rendered inline on the landing page (issue
// #362). FormSlug must resolve to a published form in the same space; the
// server only renders embed blocks whose slug matches a published form, so a
// draft/private/unknown slug is never shown.
type LandingEmbed struct {
	// Label is an optional heading shown above the embedded form.
	Label string `json:"label"`
	// FormSlug is the published form to embed (e.g. "rsvp").
	FormSlug string `json:"form_slug"`
}

// SpaceSettings is the storefront configuration for one space.
type SpaceSettings struct {
	SpaceID    string `json:"space_id"`
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
	// Currency is the space's settlement/display currency (issue #185).
	// Default MYR. The space's account(s) inherit it for CHIP purchases; new
	// amounts are stored in this currency's minor units. Historical sales keep
	// their original currency (never auto-converted).
	Currency string `json:"currency"`
	// Indexing is the per-space SEO toggle (issue #276). When true (default)
	// the space's public storefront + forms are eligible for search-engine /
	// AI indexing on the jomform.com apex and on custom domains. When false
	// the space opts out of indexing entirely. Public forms on jomform.com
	// subdomains are always noindex regardless of this flag (privacy).
	Indexing bool `json:"indexing"`
	// LandingPage is the customizable landing-page config (issue #360).
	// Stored as landing_page_json on space_settings. When nil/empty the
	// storefront root falls back to the auto-generated bare listing.
	LandingPage *LandingPage `json:"landing_page,omitempty"`
}

// SpaceLang returns the storefront default language code, or a fallback when
// unset. Callers must use this so a not-yet-migrated or legacy row resolves
// to 'en' (maintainer decision 2026-09-01), never "".
func (st *SpaceSettings) SpaceLang() string {
	switch st.DefaultLang {
	case "en", "ms", "zh", "ta", "ar":
		return st.DefaultLang
	default:
		return "en"
	}
}

// SpaceCurrency returns the space's currency code, or MYR when unset/unknown.
// Callers must use this so a not-yet-migrated or legacy row resolves to MYR
// (issue #185), never "".
func (st *SpaceSettings) SpaceCurrency() string {
	if IsSupportedCurrency(st.Currency) {
		return st.Currency
	}
	return "MYR"
}

// Settings reads (creating default row when missing) the storefront config.
func (s *SpaceStore) Settings(ctx context.Context, spaceID string) (*SpaceSettings, error) {
	var lpJSON string
	st := &SpaceSettings{SpaceID: spaceID, MenuJSON: "[]", DefaultLang: "en", Currency: "MYR", Indexing: true}
	err := s.pool.QueryRow(ctx,
		`SELECT footer_text, company_reg, privacy_url, terms_url, refund_url, contact_phone, contact_address, business_name, menu_json::text, default_lang, currency, indexing, COALESCE(landing_page_json::text, '{}')
		   FROM space_settings WHERE space_id=$1`,
		spaceID).Scan(&st.FooterText, &st.CompanyReg, &st.PrivacyURL, &st.TermsURL, &st.RefundURL, &st.ContactPhone, &st.ContactAddress, &st.BusinessName, &st.MenuJSON, &st.DefaultLang, &st.Currency, &st.Indexing, &lpJSON)
	if err != nil {
		// default row
		_, ierr := s.pool.Exec(ctx,
			`INSERT INTO space_settings (space_id) VALUES ($1) ON CONFLICT DO NOTHING`, spaceID)
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
func (s *SpaceStore) SaveSettings(ctx context.Context, st *SpaceSettings) error {
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
		`INSERT INTO space_settings (space_id, footer_text, company_reg, privacy_url, terms_url, refund_url, contact_phone, contact_address, business_name, menu_json, default_lang, currency, indexing, landing_page_json)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12,$13,$14::jsonb)
		 ON CONFLICT (space_id) DO UPDATE SET
		   footer_text=EXCLUDED.footer_text, company_reg=EXCLUDED.company_reg,
		   privacy_url=EXCLUDED.privacy_url, terms_url=EXCLUDED.terms_url,
		   refund_url=EXCLUDED.refund_url, contact_phone=EXCLUDED.contact_phone,
		   contact_address=EXCLUDED.contact_address, business_name=EXCLUDED.business_name,
		   menu_json=EXCLUDED.menu_json,
		   default_lang=EXCLUDED.default_lang, currency=EXCLUDED.currency,
		   indexing=EXCLUDED.indexing,
		   landing_page_json=EXCLUDED.landing_page_json`,
		st.SpaceID, st.FooterText, st.CompanyReg, st.PrivacyURL, st.TermsURL, st.RefundURL, st.ContactPhone, st.ContactAddress, st.BusinessName, st.MenuJSON, st.SpaceLang(), st.SpaceCurrency(), st.Indexing, lpJSON)
	return err
}

// SpaceCurrencyByAccount resolves the currency of the space that owns the
// given account ("" when the account has no linked space). Used by the
// payments path to pick the CHIP purchase currency from the space (issue
// #185) instead of the account-level default.
func (s *SpaceStore) SpaceCurrencyByAccount(ctx context.Context, accountID string) (string, error) {
	var cur string
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(ss.currency, 'MYR')
		   FROM accounts a
		   LEFT JOIN space_settings ss ON ss.space_id = a.space_id
		  WHERE a.id=$1`, accountID).Scan(&cur)
	if err != nil {
		return "", err
	}
	return cur, nil
}

// IndexingByAccount resolves whether the space owning the given account has
// indexing enabled (issue #276). Defaults to true when the account has no
// linked space (legacy) or the setting is unset. Used by the public form
// renderer to decide whether a custom-domain form may be indexed.
func (s *SpaceStore) IndexingByAccount(ctx context.Context, accountID string) (bool, error) {
	var idx bool
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(ss.indexing, TRUE)
		   FROM accounts a
		   LEFT JOIN space_settings ss ON ss.space_id = a.space_id
		  WHERE a.id=$1`, accountID).Scan(&idx)
	if err != nil {
		return true, err
	}
	return idx, nil
}

// IndexedSpaceURLs returns the custom-domain URLs of spaces that have indexing
// enabled (issue #276). Only verified custom domains count; the jomform.com
// apex is handled by the web app. Used to build sitemap.xml.
func (s *SpaceStore) IndexedSpaceURLs(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT ad.domain
		   FROM space_settings ss
		   JOIN accounts a ON a.space_id = ss.space_id
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

// IDBySlug resolves a space id from its slug ("" when unknown).
func (s *SpaceStore) IDBySlug(ctx context.Context, slug string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `SELECT id FROM spaces WHERE slug=$1`, slug).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, nil
}

// SpaceIDByAccount resolves the space id a form's account belongs to ("" when
// the account has no linked space). Rendering falls back to 'en' in that case.
func (s *SpaceStore) SpaceIDByAccount(ctx context.Context, accountID string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx,
		`SELECT a.space_id FROM accounts a WHERE a.id=$1`, accountID).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, nil
}

// RoleByAccount returns the role a user holds in the space that owns the given
// account ("" when the account has no space or the user is not a member).
// The account owner is always treated as an admin (issue #162).
func (s *SpaceStore) RoleByAccount(ctx context.Context, accountID, userID string) (string, error) {
	var owner, role string
	err := s.pool.QueryRow(ctx, `
		SELECT a.owner_user_id, COALESCE(m.role, '')
		FROM accounts a
		LEFT JOIN space_members m ON m.space_id = a.space_id AND m.user_id = $2
		WHERE a.id = $1`, accountID, userID).Scan(&owner, &role)
	if err != nil {
		return "", err
	}
	if owner == userID {
		return RoleAdmin, nil
	}
	return role, nil
}

// SpaceSlugByAccount resolves the space slug a form's account belongs to
// ("" when the account has no linked space). Used to build the canonical
// space-scoped public URL {space}.jomform.com/{slug} (issue #77).
func (s *SpaceStore) SpaceSlugByAccount(ctx context.Context, accountID string) (string, error) {
	var slug string
	err := s.pool.QueryRow(ctx,
		`SELECT sp.slug FROM accounts a JOIN spaces sp ON sp.id = a.space_id
		  WHERE a.id=$1`, accountID).Scan(&slug)
	if err != nil {
		return "", err
	}
	return slug, nil
}
