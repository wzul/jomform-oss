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

// SpaceSettings is the storefront configuration for one space.
type SpaceSettings struct {
	SpaceID     string `json:"space_id"`
	FooterText  string `json:"footer_text"`
	CompanyReg  string `json:"company_reg"`
	PrivacyURL  string `json:"privacy_url"`
	TermsURL    string `json:"terms_url"`
	MenuJSON    string `json:"menu_json"`
	DefaultLang string `json:"default_lang"`
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
		`SELECT footer_text, company_reg, privacy_url, terms_url, menu_json::text, default_lang
		   FROM space_settings WHERE space_id=$1`,
		spaceID).Scan(&st.FooterText, &st.CompanyReg, &st.PrivacyURL, &st.TermsURL, &st.MenuJSON, &st.DefaultLang)
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
		`INSERT INTO space_settings (space_id, footer_text, company_reg, privacy_url, terms_url, menu_json, default_lang)
		 VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7)
		 ON CONFLICT (space_id) DO UPDATE SET
		   footer_text=EXCLUDED.footer_text, company_reg=EXCLUDED.company_reg,
		   privacy_url=EXCLUDED.privacy_url, terms_url=EXCLUDED.terms_url, menu_json=EXCLUDED.menu_json,
		   default_lang=EXCLUDED.default_lang`,
		st.SpaceID, st.FooterText, st.CompanyReg, st.PrivacyURL, st.TermsURL, st.MenuJSON, st.SpaceLang())
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

// AccountIDsBySlugForOwner returns the account ids linked to the space with
// the given slug, but only when that space is owned by the given user.
// owned is false when the slug is unknown or not owned by the user —
// the caller must not fall back to unscoped data in that case. An owned
// space with no linked accounts yields (true, nil).
func (s *SpaceStore) AccountIDsBySlugForOwner(ctx context.Context, slug, ownerUserID string) (owned bool, accountIDs []string, err error) {
	rows, err := s.pool.Query(ctx,
		`SELECT a.id FROM spaces sp
		 LEFT JOIN accounts a ON a.space_id = sp.id
		 WHERE sp.slug = $1 AND sp.owner_user_id = $2`,
		slug, ownerUserID)
	if err != nil {
		return false, nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id *string
		if err := rows.Scan(&id); err != nil {
			return false, nil, err
		}
		owned = true
		if id != nil && *id != "" {
			out = append(out, *id)
		}
	}
	return owned, out, rows.Err()
}
