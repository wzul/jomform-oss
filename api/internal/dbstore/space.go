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

// Create makes a new space (slug unique).
func (s *SpaceStore) Create(ctx context.Context, ownerUserID, name, slug string) (*Space, error) {
	sp := &Space{Owner: ownerUserID, Name: name, Slug: slug}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO spaces (owner_user_id, name, slug) VALUES ($1,$2,$3)
		 RETURNING id, owner_user_id, name, slug`,
		ownerUserID, name, slug).Scan(&sp.ID, &sp.Owner, &sp.Name, &sp.Slug)
	return sp, err
}

// SpaceSettings is the storefront configuration for one space.
type SpaceSettings struct {
	SpaceID    string `json:"space_id"`
	FooterText string `json:"footer_text"`
	CompanyReg string `json:"company_reg"`
	PrivacyURL string `json:"privacy_url"`
	TermsURL   string `json:"terms_url"`
	MenuJSON   string `json:"menu_json"`
}

// Settings reads (creating default row when missing) the storefront config.
func (s *SpaceStore) Settings(ctx context.Context, spaceID string) (*SpaceSettings, error) {
	st := &SpaceSettings{SpaceID: spaceID, MenuJSON: "[]"}
	err := s.pool.QueryRow(ctx,
		`SELECT footer_text, company_reg, privacy_url, terms_url, menu_json::text FROM space_settings WHERE space_id=$1`,
		spaceID).Scan(&st.FooterText, &st.CompanyReg, &st.PrivacyURL, &st.TermsURL, &st.MenuJSON)
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
		`INSERT INTO space_settings (space_id, footer_text, company_reg, privacy_url, terms_url, menu_json)
		 VALUES ($1,$2,$3,$4,$5,$6::jsonb)
		 ON CONFLICT (space_id) DO UPDATE SET
		   footer_text=EXCLUDED.footer_text, company_reg=EXCLUDED.company_reg,
		   privacy_url=EXCLUDED.privacy_url, terms_url=EXCLUDED.terms_url, menu_json=EXCLUDED.menu_json`,
		st.SpaceID, st.FooterText, st.CompanyReg, st.PrivacyURL, st.TermsURL, st.MenuJSON)
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
