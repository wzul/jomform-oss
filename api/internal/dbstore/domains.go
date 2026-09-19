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
	// TLS (issue #397): per-domain TLS configuration.
	//   TLSMode   "auto" (default) | "custom"
	//   TLSStatus "pending" | "active" | "error" | "custom" | "" (not verified)
	//   TLSError  human reason when TLSStatus == "error"
	//   TLSCertEnc / TLSKeyEnc  AES-GCM sealed PEM (custom mode only)
	TLSMode    string
	TLSStatus  string
	TLSError   string
	TLSCertEnc string
	TLSKeyEnc  string
	// TLSEmail is the per-domain ACME/Let's Encrypt contact email (issue
	// #398). Empty = fall back to the account email, then the global
	// ACME_EMAIL env. Defaults to the account email on verify.
	TLSEmail  string
	CreatedAt time.Time
}

// domainSelect is the shared column list for an AccountDomain read, so the
// two full-row reads (GetDomain, ListDomains) cannot drift apart, and it is
// registered with the migration/scan-drift guard in
// migration_scan_drift_test.go so a future account_domains column must land
// here, in the struct, and in both scan sites in the same change.
//
// The five columns added by migrations 000038 (auto_cname), 000043
// (tls_error, tls_cert_enc, tls_key_enc) and 000044 (tls_email) are nullable
// and have NO default, so they read as NULL for any row created through the
// production write path — AddDomain inserts only
// (account_id, domain, provider), and SetTLSEmail / SetTLSMode /
// SetCustomCert actively write NULL back. A NULL can never scan into the
// plain `string` fields below, so every such row made the whole read fail:
// "can't scan into dest[6] (col: auto_cname): cannot scan NULL into *string".
// COALESCE(col, empty-string) keeps the string fields meaningful and the scan
// column-aligned; the repo uses the same convention for the nullable accounts
// columns (see accountSelect).
const domainSelect = `SELECT id, account_id, domain, provider, verified, verified_at,
	        COALESCE(auto_cname,''), tls_mode, tls_status, COALESCE(tls_error,''),
	        COALESCE(tls_cert_enc,''), COALESCE(tls_key_enc,''), COALESCE(tls_email,''), created_at
	 FROM account_domains`

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
		domainSelect+` WHERE id=$1 AND account_id=$2`, id, accountID).
		Scan(&d.ID, &d.AccountID, &d.Domain, &d.Provider, &d.Verified, &d.VerifiedAt,
			&d.AutoCNAME, &d.TLSMode, &d.TLSStatus, &d.TLSError,
			&d.TLSCertEnc, &d.TLSKeyEnc, &d.TLSEmail, &d.CreatedAt)
	return d, err
}

// ListDomains lists all domains claimed by an account.
func (s *DomainStore) ListDomains(ctx context.Context, accountID string) ([]*AccountDomain, error) {
	rows, err := s.pool.Query(ctx,
		domainSelect+` WHERE account_id=$1 ORDER BY created_at`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AccountDomain
	for rows.Next() {
		d := &AccountDomain{}
		if err := rows.Scan(&d.ID, &d.AccountID, &d.Domain, &d.Provider, &d.Verified,
			&d.VerifiedAt, &d.AutoCNAME, &d.TLSMode, &d.TLSStatus, &d.TLSError,
			&d.TLSCertEnc, &d.TLSKeyEnc, &d.TLSEmail, &d.CreatedAt); err != nil {
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

// SetTLSMode sets the per-domain TLS mode (issue #397): "auto" (default) or
// "custom". Switching to "auto" clears any stored custom cert + error so the
// origin provisions a Let's Encrypt cert for the verified domain.
func (s *DomainStore) SetTLSMode(ctx context.Context, accountID, domain, mode string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE account_domains SET tls_mode=$3,
		        tls_cert_enc=NULL, tls_key_enc=NULL, tls_error=NULL
		 WHERE domain=$1 AND account_id=$2`, domain, accountID, mode)
	return err
}

// SetTLSStatus records the TLS provisioning status for a domain (issue #397):
// "pending" | "active" | "error" | "custom". A non-empty reason is stored
// when status == "error".
func (s *DomainStore) SetTLSStatus(ctx context.Context, accountID, domain, status, reason string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE account_domains SET tls_status=$3, tls_error=$4
		 WHERE domain=$1 AND account_id=$2`, domain, accountID, status, reason)
	return err
}

// SetTLSEmail sets the per-domain ACME/Let's Encrypt contact email (issue
// #398). An empty email clears it, so the domain falls back to the account
// email, then the global ACME_EMAIL env.
func (s *DomainStore) SetTLSEmail(ctx context.Context, accountID, domain, email string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE account_domains SET tls_email=NULLIF($3,'') WHERE domain=$1 AND account_id=$2`,
		domain, accountID, email)
	return err
}

// SetCustomCert stores the merchant-supplied cert + key (AES-GCM sealed PEM)
// for a domain and marks it custom mode + custom status (issue #397).
func (s *DomainStore) SetCustomCert(ctx context.Context, accountID, domain, certEnc, keyEnc string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE account_domains SET tls_mode='custom', tls_status='custom',
		        tls_cert_enc=$3, tls_key_enc=$4, tls_error=NULL
		 WHERE domain=$1 AND account_id=$2`, domain, accountID, certEnc, keyEnc)
	return err
}

// CustomCert returns the sealed custom cert + key for a domain (issue #397).
// Returns empty strings when the domain has no custom cert.
func (s *DomainStore) CustomCert(ctx context.Context, accountID, domain string) (certEnc, keyEnc string, err error) {
	err = s.pool.QueryRow(ctx,
		`SELECT COALESCE(tls_cert_enc,''), COALESCE(tls_key_enc,'')
		 FROM account_domains WHERE domain=$1 AND account_id=$2`, domain, accountID).
		Scan(&certEnc, &keyEnc)
	return certEnc, keyEnc, err
}
