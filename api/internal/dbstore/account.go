package dbstore

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Account models the accounts table.
type Account struct {
	ID                     string
	OwnerUserID            string
	Name                   string
	Slug                   string
	CustomDomain           string
	CustomDomainVerified   bool
	CustomDomainVerifiedAt *time.Time
	Timezone               string
	Currency               string
	ChipEmailFallback      string
	ChipMethodWhitelist    []string
	EinvoiceTIN            string
	EinvoiceMSIC           string
	ChipBrandID            string
	ChipSecretKeyEnc       string
	ChipMode               string
	BNMRounding            bool
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

// AccountStore wraps pgx for account operations.
type AccountStore struct{ pool *pgxpool.Pool }

// NewAccountStore builds an AccountStore.
func NewAccountStore(pool *pgxpool.Pool) *AccountStore { return &AccountStore{pool: pool} }

// ByID fetches an account by UUID string.
func (s *AccountStore) ByID(ctx context.Context, id string) (*Account, error) {
	a := &Account{}
	err := s.pool.QueryRow(ctx, accountSelect+" WHERE id = $1", id).
		Scan(&a.ID, &a.OwnerUserID, &a.Name, &a.Slug,
			&a.Timezone, &a.Currency,
			&a.ChipBrandID, &a.ChipSecretKeyEnc, &a.ChipMode, &a.BNMRounding,
			&a.ChipEmailFallback, &a.ChipMethodWhitelist,
			&a.CustomDomain, &a.CustomDomainVerified, &a.CustomDomainVerifiedAt,
			&a.EinvoiceTIN, &a.EinvoiceMSIC,
			&a.CreatedAt, &a.UpdatedAt)
	return a, err
}

// ByOwner lists accounts owned by a user.
func (s *AccountStore) ByOwner(ctx context.Context, userID string) ([]*Account, error) {
	rows, err := s.pool.Query(ctx, accountSelect+" WHERE owner_user_id = $1 ORDER BY created_at", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Account
	for rows.Next() {
		a := &Account{}
		if err := rows.Scan(&a.ID, &a.OwnerUserID, &a.Name, &a.Slug,
			&a.Timezone, &a.Currency,
			&a.ChipBrandID, &a.ChipSecretKeyEnc, &a.ChipMode, &a.BNMRounding,
			&a.ChipEmailFallback, &a.ChipMethodWhitelist,
			&a.CustomDomain, &a.CustomDomainVerified, &a.CustomDomainVerifiedAt,
			&a.EinvoiceTIN, &a.EinvoiceMSIC,
			&a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetChipCredentials stores Brand ID and the AES-GCM encrypted secret key.
func (s *AccountStore) SetChipCredentials(ctx context.Context, accountID, brandID, secretKeyEnc, mode string, emailFallback string, whitelist []string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE accounts SET chip_brand_id=$2, chip_secret_key_enc=$3, chip_mode=$4,
		   chip_email_fallback=NULLIF($5,'')::citext, chip_method_whitelist=$6, updated_at=now()
		 WHERE id=$1`,
		accountID, brandID, secretKeyEnc, mode, emailFallback, whitelist)
	return err
}

// ChipCredentials clears stored CHIP credentials.
func (s *AccountStore) ClearChipCredentials(ctx context.Context, accountID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE accounts SET chip_brand_id=NULL, chip_secret_key_enc=NULL, chip_mode='test', updated_at=now() WHERE id=$1`,
		accountID)
	return err
}

const accountSelect = `SELECT id, owner_user_id, name, slug, timezone, currency,
	COALESCE(chip_brand_id,''), COALESCE(chip_secret_key_enc,''), chip_mode, bnm_rounding,
	COALESCE(chip_email_fallback::text,''), COALESCE(chip_method_whitelist, '{}'),
	COALESCE(custom_domain,''), custom_domain_verified, custom_domain_verified_at,
	COALESCE(einvoice_tin,''), COALESCE(einvoice_msic,''),
	created_at, updated_at
	FROM accounts`

// SlugByCustomDomain maps a verified custom domain to its account slug.
func (s *AccountStore) SlugByCustomDomain(ctx context.Context, domain string) (string, error) {
	var slug string
	err := s.pool.QueryRow(ctx,
		`SELECT slug FROM accounts WHERE lower(custom_domain) = $1`, domain).Scan(&slug)
	return slug, err
}

// SetCustomDomain claims a custom domain for this account.
func (s *AccountStore) SetCustomDomain(ctx context.Context, accountID, domain string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE accounts SET custom_domain=$2, custom_domain_verified=false, updated_at=now() WHERE id=$1`,
		accountID, domain)
	return err
}

// MarkCustomDomainVerified stamps verification time.
func (s *AccountStore) MarkCustomDomainVerified(ctx context.Context, accountID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE accounts SET custom_domain_verified=true, custom_domain_verified_at=now() WHERE id=$1`,
		accountID)
	return err
}

// ClearCustomDomain removes the custom-domain claim.
func (s *AccountStore) ClearCustomDomain(ctx context.Context, accountID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE accounts SET custom_domain=NULL, custom_domain_verified=false,
		   custom_domain_verified_at=NULL, updated_at=now() WHERE id=$1`, accountID)
	return err
}

// SetEinvoice stores LHDN e-invoicing identifiers.
func (s *AccountStore) SetEinvoice(ctx context.Context, accountID, tin, msic string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE accounts SET einvoice_tin=NULLIF($2,''), einvoice_msic=NULLIF($3,''), updated_at=now() WHERE id=$1`,
		accountID, tin, msic)
	return err
}

// Einvoice reads LHDN identifiers (may be empty strings).
func (s *AccountStore) Einvoice(ctx context.Context, accountID string) (tin, msic string, err error) {
	err = s.pool.QueryRow(ctx,
		`SELECT COALESCE(einvoice_tin,''), COALESCE(einvoice_msic,'') FROM accounts WHERE id=$1`,
		accountID).Scan(&tin, &msic)
	return
}

// SetTracking stores the global head tracking markup (sanitised by caller).
func (s *AccountStore) SetTracking(ctx context.Context, accountID, head string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE accounts SET tracking_head=$2, updated_at=now() WHERE id=$1`, accountID, head)
	return err
}

// Tracking reads the global tracking markup.
func (s *AccountStore) Tracking(ctx context.Context, accountID string) (string, error) {
	var v string
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(tracking_head,'') FROM accounts WHERE id=$1`, accountID).Scan(&v)
	return v, err
}

// SigningKeys returns the account's webhook Ed25519 keys (base64).
func (s *AccountStore) SigningKeys(ctx context.Context, accountID string) (string, string, error) {
	var pub, priv string
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(wh_sign_pub,''), COALESCE(wh_sign_priv,'') FROM accounts WHERE id=$1`,
		accountID).Scan(&pub, &priv)
	return pub, priv, err
}

// SaveSigningKeys persists the generated keypair (priv stays server-side).
func (s *AccountStore) SaveSigningKeys(ctx context.Context, accountID, pubB64, privB64 string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE accounts SET wh_sign_pub=$2, wh_sign_priv=$3, updated_at=now() WHERE id=$1`,
		accountID, pubB64, privB64)
	return err
}

// SaveCFToken stores the sealed Cloudflare OAuth token blob for the account.
// The blob is a sealed JSON {"access","refresh"} produced by the caller;
// an empty string unlinks. (Issue #22 — restored after #15 Option B.)
func (s *AccountStore) SaveCFToken(ctx context.Context, accountID, sealed string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE accounts SET cf_token_enc=NULLIF($2,''), updated_at=now() WHERE id=$1`,
		accountID, sealed)
	return err
}

// CFToken returns the sealed Cloudflare token ("" when not linked).
func (s *AccountStore) CFToken(ctx context.Context, accountID string) (string, error) {
	var sealed string
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(cf_token_enc,'') FROM accounts WHERE id=$1`, accountID).Scan(&sealed)
	return sealed, err
}
