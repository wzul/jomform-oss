// Multi-domain support (migration 000007): each account may claim many
// custom domains; routing is manual CNAME → Verify → auto Traefik route.
package dbstore

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AccountDomain models the account_domains table.
type AccountDomain struct {
	ID         string
	AccountID  string
	Domain     string
	Provider   string // cname
	Verified   bool
	VerifiedAt *time.Time
	// AutoCNAME records whether the CNAME was auto-created via the linked
	// Cloudflare account (issue #322): "created" | "failed" | "" (manual).
	AutoCNAME string
	CreatedAt time.Time
}

// DomainStore wraps account_domains.
type DomainStore struct{ pool *pgxpool.Pool }

// NewDomainStore builds one.
func NewDomainStore(pool *pgxpool.Pool) *DomainStore { return &DomainStore{pool: pool} }

// AddDomain inserts a claim (error on duplicate domain).
func (s *DomainStore) AddDomain(ctx context.Context, accountID, domain, provider string) (*AccountDomain, error) {
	d := &AccountDomain{AccountID: accountID, Domain: domain, Provider: provider}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO account_domains (account_id, domain, provider) VALUES ($1,$2,$3)
		 RETURNING id, created_at`, accountID, domain, provider).Scan(&d.ID, &d.CreatedAt)
	return d, err
}

// SetAutoCNAME records the auto-CNAME attempt result for a domain (issue #322).
func (s *DomainStore) SetAutoCNAME(ctx context.Context, accountID, domain, status string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE account_domains SET auto_cname=$3 WHERE domain=$1 AND account_id=$2`,
		domain, accountID, status)
	return err
}

// GetDomain fetches a claim by uuid + account.
func (s *DomainStore) GetDomain(ctx context.Context, accountID, id string) (*AccountDomain, error) {
	d := &AccountDomain{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, account_id, domain, provider, verified, verified_at, auto_cname, created_at
		 FROM account_domains WHERE id=$1 AND account_id=$2`, id, accountID).
		Scan(&d.ID, &d.AccountID, &d.Domain, &d.Provider, &d.Verified, &d.VerifiedAt, &d.AutoCNAME, &d.CreatedAt)
	return d, err
}

// ListDomains lists all domains claimed by an account.
func (s *DomainStore) ListDomains(ctx context.Context, accountID string) ([]*AccountDomain, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, account_id, domain, provider, verified, verified_at, auto_cname, created_at
		 FROM account_domains WHERE account_id=$1 ORDER BY created_at`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AccountDomain
	for rows.Next() {
		d := &AccountDomain{}
		if err := rows.Scan(&d.ID, &d.AccountID, &d.Domain, &d.Provider, &d.Verified,
			&d.VerifiedAt, &d.AutoCNAME, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ByDomain maps any claimed domain (verified) to its account — used by the
// public renderer for host-based resolution.
func (s *DomainStore) ByDomain(ctx context.Context, domain string) (*AccountDomain, error) {
	d := &AccountDomain{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, account_id, domain, provider, verified FROM account_domains
		 WHERE domain = $1 AND verified`, strings.ToLower(domain)).
		Scan(&d.ID, &d.AccountID, &d.Domain, &d.Provider, &d.Verified)
	return d, err
}

// MarkVerified stamps verification for a domain row.
func (s *DomainStore) MarkVerified(ctx context.Context, accountID, domain string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE account_domains SET verified=true, verified_at=now() WHERE domain=$1 AND account_id=$2`,
		domain, accountID)
	return err
}

// RemoveDomain deletes a claim.
func (s *DomainStore) RemoveDomain(ctx context.Context, accountID, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM account_domains WHERE id=$2 AND account_id=$1`, accountID, id)
	return err
}

// GetByID fetches one row by id+account.
func (s *DomainStore) GetByID(ctx context.Context, accountID, id string) (*AccountDomain, error) {
	d := &AccountDomain{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, account_id, domain, provider, verified FROM account_domains
		 WHERE id=$1 AND account_id=$2`, id, accountID).
		Scan(&d.ID, &d.AccountID, &d.Domain, &d.Provider, &d.Verified)
	return d, err
}
