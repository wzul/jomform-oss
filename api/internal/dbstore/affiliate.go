// Affiliate: affiliate marketing (issue #444). A merchant creates affiliate
// links (?aff=CODE) for each affiliate; sales driven by an affiliate are
// attributed to that affiliate and commission is computed on paid sales.
package dbstore

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrAffiliateNotFound is returned when a workspace-scoped affiliate operation
// matches no row (issue #519) — either the id does not exist or it belongs to
// another workspace. Callers must treat both the same way (a not-found
// response) so the difference never leaks.
var ErrAffiliateNotFound = errors.New("affiliate not found")

// Affiliate models the affiliates table.
type Affiliate struct {
	ID            string
	WorkspaceID   string
	Code          string
	Name          string
	Email         string
	CommissionPct float64
}

// AffiliateStore wraps affiliates.
type AffiliateStore struct{ pool *pgxpool.Pool }

func NewAffiliateStore(pool *pgxpool.Pool) *AffiliateStore { return &AffiliateStore{pool: pool} }

const affiliateSelect = `SELECT id, workspace_id, code, name, email, commission_pct FROM affiliates`

func scanAffiliate(row rowScanner) (*Affiliate, error) {
	a := &Affiliate{}
	err := row.Scan(&a.ID, &a.WorkspaceID, &a.Code, &a.Name, &a.Email, &a.CommissionPct)
	return a, err
}

// Create inserts a new affiliate.
func (s *AffiliateStore) Create(ctx context.Context, a *Affiliate) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO affiliates (id, workspace_id, code, name, email, commission_pct)
		 VALUES ($1,$2,$3,$4,$5,$6)`,
		a.ID, a.WorkspaceID, a.Code, a.Name, a.Email, a.CommissionPct)
	return err
}

// ByWorkspace lists affiliates for a workspace.
func (s *AffiliateStore) ByWorkspace(ctx context.Context, workspaceID string) ([]*Affiliate, error) {
	rows, err := s.pool.Query(ctx, affiliateSelect+" WHERE workspace_id=$1 ORDER BY created_at", workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Affiliate
	for rows.Next() {
		a, err := scanAffiliate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ByCode resolves an affiliate by workspace + code (for attribution).
func (s *AffiliateStore) ByCode(ctx context.Context, workspaceID, code string) (*Affiliate, error) {
	row := s.pool.QueryRow(ctx, affiliateSelect+" WHERE workspace_id=$1 AND code=$2", workspaceID, code)
	a, err := scanAffiliate(row)
	if err != nil {
		return nil, err
	}
	return a, nil
}

// ByID resolves an affiliate by id.
func (s *AffiliateStore) ByID(ctx context.Context, id string) (*Affiliate, error) {
	row := s.pool.QueryRow(ctx, affiliateSelect+" WHERE id=$1", id)
	a, err := scanAffiliate(row)
	if err != nil {
		return nil, err
	}
	return a, nil
}

// SetCommission updates an affiliate's commission rate, scoped to its owning
// workspace (issue #519).
//
// The workspace predicate is part of the UPDATE itself, not a handler-side
// convention: an affiliate id that does not belong to workspaceID updates zero
// rows and reports ErrAffiliateNotFound, so a caller can never move another
// workspace's commission rate by passing its affiliate id. Callers must supply
// the workspace they were authorised against.
func (s *AffiliateStore) SetCommission(ctx context.Context, workspaceID, id string, pct float64) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE affiliates SET commission_pct=$3 WHERE id=$2 AND workspace_id=$1`,
		workspaceID, id, pct)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAffiliateNotFound
	}
	return nil
}
