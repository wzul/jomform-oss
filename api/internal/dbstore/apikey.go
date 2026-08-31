package dbstore

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// APIKey models the api_keys table.
type APIKey struct {
	ID         string
	AccountID  string
	KeyHash    string
	Name       string
	Scopes     []string
	LastUsedAt *time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
}

// ErrAPIKeyInvalid returned when a key hash has no active row.
var ErrAPIKeyInvalid = errors.New("api key invalid")

// APIKeyStore wraps pgx for api_keys.
type APIKeyStore struct{ pool *pgxpool.Pool }

// NewAPIKeyStore builds the store.
func NewAPIKeyStore(pool *pgxpool.Pool) *APIKeyStore { return &APIKeyStore{pool: pool} }

// LookupByKeyHash resolves an API key by its sha256 hash. Returns
// ErrAPIKeyInvalid when missing/revoked.
func (s *APIKeyStore) LookupByKeyHash(ctx context.Context, hash string) (*APIKey, error) {
	k := &APIKey{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, account_id, key_hash, name, scopes, last_used_at, revoked_at, created_at
		 FROM api_keys WHERE key_hash = $1 AND revoked_at IS NULL`, hash,
	).Scan(&k.ID, &k.AccountID, &k.KeyHash, &k.Name, &k.Scopes, &k.LastUsedAt, &k.RevokedAt, &k.CreatedAt)
	if err != nil {
		return nil, ErrAPIKeyInvalid
	}
	return k, nil
}

// Create stores a new API key (hash only).
func (s *APIKeyStore) Create(ctx context.Context, accountID, keyHash, name string, scopes []string) (*APIKey, error) {
	k := &APIKey{}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO api_keys (account_id, key_hash, name, scopes) VALUES ($1,$2,$3,$4)
		 RETURNING id, created_at`,
		accountID, keyHash, name, scopes,
	).Scan(&k.ID, &k.CreatedAt)
	if err != nil {
		return nil, err
	}
	k.AccountID, k.KeyHash, k.Name, k.Scopes = accountID, keyHash, name, scopes
	return k, nil
}

// Revoke marks a key revoked.
func (s *APIKeyStore) Revoke(ctx context.Context, accountID, keyID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE api_keys SET revoked_at=now() WHERE id=$2 AND account_id=$1`, accountID, keyID)
	return err
}

// Touch updates last_used_at (fire-and-forget).
func (s *APIKeyStore) Touch(ctx context.Context, keyID string) {
	_, _ = s.pool.Exec(ctx, `UPDATE api_keys SET last_used_at=now() WHERE id=$1`, keyID)
}

// ScopeHas reports whether the key holds the scope.
func ScopeHas(scopes []string, want string) bool {
	for _, s := range scopes {
		if s == want || s == "admin" {
			return true
		}
	}
	return false
}
