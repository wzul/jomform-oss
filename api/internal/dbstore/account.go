package dbstore

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Account models the accounts table.
type Account struct {
	ID               string
	OwnerUserID      string
	Name             string
	Slug             string
	CustomDomain     string
	Timezone         string
	Currency         string
	ChipBrandID      string
	ChipSecretKeyEnc string
	ChipMode         string
	BNMRounding      bool
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// AccountStore wraps pgx for account operations.
type AccountStore struct{ pool *pgxpool.Pool }

// NewAccountStore builds an AccountStore.
func NewAccountStore(pool *pgxpool.Pool) *AccountStore { return &AccountStore{pool: pool} }

// ByID fetches an account by UUID string.
func (s *AccountStore) ByID(ctx context.Context, id string) (*Account, error) {
	a := &Account{}
	err := s.pool.QueryRow(ctx, accountSelect+" WHERE id = $1", id).
		Scan(&a.ID, &a.OwnerUserID, &a.Name, &a.Slug, &a.CustomDomain, &a.Timezone,
			&a.Currency, &a.ChipBrandID, &a.ChipSecretKeyEnc, &a.ChipMode, &a.BNMRounding,
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
		if err := rows.Scan(&a.ID, &a.OwnerUserID, &a.Name, &a.Slug, &a.CustomDomain, &a.Timezone,
			&a.Currency, &a.ChipBrandID, &a.ChipSecretKeyEnc, &a.ChipMode, &a.BNMRounding,
			&a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetChipCredentials stores Brand ID and the AES-GCM encrypted secret key.
func (s *AccountStore) SetChipCredentials(ctx context.Context, accountID, brandID, secretKeyEnc, mode string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE accounts SET chip_brand_id=$2, chip_secret_key_enc=$3, chip_mode=$4, updated_at=now() WHERE id=$1`,
		accountID, brandID, secretKeyEnc, mode)
	return err
}

// ChipCredentials clears stored CHIP credentials.
func (s *AccountStore) ClearChipCredentials(ctx context.Context, accountID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE accounts SET chip_brand_id=NULL, chip_secret_key_enc=NULL, chip_mode='test', updated_at=now() WHERE id=$1`,
		accountID)
	return err
}

const accountSelect = `SELECT id, owner_user_id, name, slug, COALESCE(custom_domain,''), timezone, currency,
	COALESCE(chip_brand_id,''), COALESCE(chip_secret_key_enc,''), chip_mode, bnm_rounding, created_at, updated_at
	FROM accounts`
