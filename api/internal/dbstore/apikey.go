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

// ListByAccount lists active (non-revoked) keys for the account.
func (s *APIKeyStore) ListByAccount(ctx context.Context, accountID string) ([]*APIKey, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, account_id, key_hash, name, scopes, last_used_at, revoked_at, created_at
		 FROM api_keys WHERE account_id=$1 AND revoked_at IS NULL ORDER BY created_at DESC`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*APIKey
	for rows.Next() {
		k := &APIKey{}
		if err := rows.Scan(&k.ID, &k.AccountID, &k.KeyHash, &k.Name, &k.Scopes, &k.LastUsedAt, &k.RevokedAt, &k.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// Revoke marks a key revoked. Account-scoped.
//
// A miss is a not-found: an id naming no row this account owns — never existed,
// another account's key — and an id that cannot be a UUID at all (Postgres
// SQLSTATE 22P02) both return ErrAPIKeyInvalid, so DELETE /v1/rest/keys/{id}
// answers 404. Before this, the malformed id escaped as a raw driver error into
// a 500, and a missing/foreign id answered 200 "revoked" while revoking
// nothing — telling the caller an access path was closed when it was not.
//
// Revoking a key the caller owns is idempotent: the UPDATE matches on id +
// account and does not filter on revoked_at, so a repeat (or a client retry)
// still affects the row and answers 200 rather than a confusing 404 for a key
// that is, in fact, revoked (see isNoSuchRow).
func (s *APIKeyStore) Revoke(ctx context.Context, accountID, keyID string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE api_keys SET revoked_at=now() WHERE id=$2 AND account_id=$1`,
		accountID, keyID)
	if isNoSuchRow(err) {
		return ErrAPIKeyInvalid
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAPIKeyInvalid
	}
	return nil
}

// Touch updates last_used_at (fire-and-forget).
func (s *APIKeyStore) Touch(ctx context.Context, keyID string) {
	_, _ = s.pool.Exec(ctx, `UPDATE api_keys SET last_used_at=now() WHERE id=$1`, keyID)
}

// API-key scopes. A key carries ONE of these (prod data also holds "admin",
// which is a superset). Scope and workspace ROLE are independent axes:
//   - scope  = what this credential may do at all (readonly vs readwrite)
//   - role   = what this person may do in a given workspace (read_only vs manage
//     vs admin, issue #162)
//
// A write must satisfy BOTH. The RBAC work in 5dafe3a replaced the per-tool
// scope test with a role-only gate and silently dropped the scope half, which
// let a readonly key delete forms (issue #645) — so the scope names live here,
// next to the test, and the enforcement lives in one named function per layer
// (mcpsrv.requireWriteScope, httpapi.requireWriteScope).
const (
	// ScopeReadonly permits read-only calls.
	ScopeReadonly = "readonly"
	// ScopeReadWrite permits read + write calls.
	ScopeReadWrite = "readwrite"
	// ScopeAdmin is the legacy/prod-admin scope; ScopeHas treats it as a
	// superset of every other scope so existing keys keep working.
	ScopeAdmin = "admin"
)

// ScopeHas reports whether the key holds the scope.
func ScopeHas(scopes []string, want string) bool {
	for _, s := range scopes {
		if s == want || s == ScopeAdmin {
			return true
		}
	}
	return false
}

// ScopeHasWrite reports whether a key's scopes permit a mutating call. It is the
// ONE test every write path uses; a readonly key is refused, an admin-scoped key
// (prod legacy) passes.
func ScopeHasWrite(scopes []string) bool {
	return ScopeHas(scopes, ScopeReadWrite)
}
