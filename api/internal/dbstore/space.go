// Space: multi-tenant storefront layer. Each user may own many spaces;
// forms, products, sales and settings hang off a space.
package dbstore

import (
	"context"

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

// SpaceSettings is the storefront configuration for one space.
type SpaceSettings struct {
	SpaceID        string `json:"space_id"`
	FooterText     string `json:"footer_text"`
	CompanyReg     string `json:"company_reg"`
	PrivacyURL     string `json:"privacy_url"`
	TermsURL       string `json:"terms_url"`
	// Issue #183: optional public-form footer fields.
	RefundURL      string `json:"refund_url"`
	ContactPhone   string `json:"contact_phone"`
	ContactAddress string `json:"contact_address"`
	BusinessName   string `json:"business_name"`
	MenuJSON       string `json:"menu_json"`
	DefaultLang    string `json:"default_lang"`
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

// Settings reads (creating default row when missing) the storefront config.
func (s *SpaceStore) Settings(ctx context.Context, spaceID string) (*SpaceSettings, error) {
	st := &SpaceSettings{SpaceID: spaceID, MenuJSON: "[]", DefaultLang: "en"}
	err := s.pool.QueryRow(ctx,
		`SELECT footer_text, company_reg, privacy_url, terms_url, refund_url, contact_phone, contact_address, business_name, menu_json::text, default_lang
		   FROM space_settings WHERE space_id=$1`,
		spaceID).Scan(&st.FooterText, &st.CompanyReg, &st.PrivacyURL, &st.TermsURL, &st.RefundURL, &st.ContactPhone, &st.ContactAddress, &st.BusinessName, &st.MenuJSON, &st.DefaultLang)
	if err != nil {
		// default row
		_, ierr := s.pool.Exec(ctx,
			`INSERT INTO space_settings (space_id) VALUES ($1) ON CONFLICT DO NOTHING`, spaceID)
		if ierr != nil {
			return nil, ierr
		}
	}
	return st, nil
}

// SaveSettings upserts the storefront config.
func (s *SpaceStore) SaveSettings(ctx context.Context, st *SpaceSettings) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO space_settings (space_id, footer_text, company_reg, privacy_url, terms_url, refund_url, contact_phone, contact_address, business_name, menu_json, default_lang)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11)
		 ON CONFLICT (space_id) DO UPDATE SET
		   footer_text=EXCLUDED.footer_text, company_reg=EXCLUDED.company_reg,
		   privacy_url=EXCLUDED.privacy_url, terms_url=EXCLUDED.terms_url,
		   refund_url=EXCLUDED.refund_url, contact_phone=EXCLUDED.contact_phone,
		   contact_address=EXCLUDED.contact_address, business_name=EXCLUDED.business_name,
		   menu_json=EXCLUDED.menu_json,
		   default_lang=EXCLUDED.default_lang`,
		st.SpaceID, st.FooterText, st.CompanyReg, st.PrivacyURL, st.TermsURL, st.RefundURL, st.ContactPhone, st.ContactAddress, st.BusinessName, st.MenuJSON, st.SpaceLang())
	return err
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
