// OAuth 2.1 stores: persisted DCR clients, single-use authorization codes and
// opaque access/refresh tokens for MCP clients (issue #121). All secret
// material (codes + tokens) is stored only as SHA-256 hashes, mirroring the
// existing API-key pattern.
package dbstore

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OAuthClient models a persisted Dynamic Client Registration record.
type OAuthClient struct {
	ClientID                string
	Name                    string
	RedirectURIs            []string
	TokenEndpointAuthMethod string
	GrantTypes              []string
	CreatedAt               time.Time
}

// OAuthAuthCode models a single-use authorization code (hash stored).
type OAuthAuthCode struct {
	ClientID      string
	UserID        string
	AccountID     string
	RedirectURI   string
	CodeChallenge string // S256 base64url of the code_verifier
	Scope         []string
	ExpiresAt     time.Time
}

// OAuthToken models an opaque access+refresh token pair (hashes stored).
type OAuthToken struct {
	ID               string
	TokenHash        string
	RefreshHash      string
	ClientID         string
	UserID           string
	AccountID        string
	Scope            []string
	AccessExpiresAt  time.Time
	RefreshExpiresAt *time.Time
	RevokedAt        *time.Time
}

// Errors returned by the OAuth store.
var (
	ErrOAuthClientMissing = errors.New("oauth client not found")
	ErrAuthCodeInvalid    = errors.New("authorization code invalid, expired or already used")
	ErrRefreshInvalid     = errors.New("refresh token invalid or revoked")
	ErrAccessTokenInvalid = errors.New("access token invalid or revoked")
)

// OAuthStore wraps pgx for OAuth client/code/token operations.
type OAuthStore struct{ pool *pgxpool.Pool }

// NewOAuthStore builds an OAuthStore.
func NewOAuthStore(pool *pgxpool.Pool) *OAuthStore { return &OAuthStore{pool: pool} }

// Touch records a token use (fire-and-forget).
func (s *OAuthStore) Touch(ctx context.Context, tokenHash string) {
	_, _ = s.pool.Exec(ctx,
		`UPDATE oauth_tokens SET last_used_at = now() WHERE token_hash = $1`, tokenHash)
}

// CreateClient persists a Dynamic Client Registration record.
func (s *OAuthStore) CreateClient(ctx context.Context, c *OAuthClient) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO oauth_clients (client_id, client_name, redirect_uris, token_endpoint_auth_method, grant_types)
		 VALUES ($1,$2,$3,$4,$5)`,
		c.ClientID, c.Name, c.RedirectURIs, c.TokenEndpointAuthMethod, c.GrantTypes)
	return err
}

// ClientByID fetches a registered client by its client_id.
func (s *OAuthStore) ClientByID(ctx context.Context, clientID string) (*OAuthClient, error) {
	c := &OAuthClient{ClientID: clientID}
	err := s.pool.QueryRow(ctx,
		`SELECT client_name, redirect_uris, token_endpoint_auth_method, grant_types, created_at
		 FROM oauth_clients WHERE client_id = $1`, clientID,
	).Scan(&c.Name, &c.RedirectURIs, &c.TokenEndpointAuthMethod, &c.GrantTypes, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOAuthClientMissing
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

// StoreAuthCode persists a single-use authorization code (hashed).
func (s *OAuthStore) StoreAuthCode(ctx context.Context, codeHash string, c *OAuthAuthCode) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO oauth_auth_codes
		 (code_hash, client_id, user_id, account_id, redirect_uri, code_challenge, scope, expires_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		codeHash, c.ClientID, c.UserID, c.AccountID, c.RedirectURI, c.CodeChallenge, c.Scope, c.ExpiresAt)
	return err
}

// ConsumeAuthCode atomically redeems a single-use authorization code. The
// code_hash is the unique PK, so a concurrent second consumer races the
// UPDATE ... WHERE consumed_at IS NULL and gets ErrAuthCodeInvalid. Returns
// the code payload (for PKCE verification + token issue) or an error.
func (s *OAuthStore) ConsumeAuthCode(ctx context.Context, codeHash string) (*OAuthAuthCode, error) {
	c := &OAuthAuthCode{}
	err := s.pool.QueryRow(ctx,
		`UPDATE oauth_auth_codes SET consumed_at = now()
		 WHERE code_hash = $1 AND consumed_at IS NULL AND expires_at > now()
		 RETURNING client_id, user_id, account_id, redirect_uri, code_challenge, scope, expires_at`,
		codeHash,
	).Scan(&c.ClientID, &c.UserID, &c.AccountID, &c.RedirectURI, &c.CodeChallenge, &c.Scope, &c.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAuthCodeInvalid
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

// CreateToken inserts a new opaque access(+refresh) token pair. Only hashes
// are stored. A nil RefreshHash means a non-rotatable access token.
func (s *OAuthStore) CreateToken(ctx context.Context, t *OAuthToken) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO oauth_tokens (token_hash, refresh_hash, client_id, user_id, account_id, scope,
		                       access_expires_at, refresh_expires_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		t.TokenHash, t.RefreshHash, t.ClientID, t.UserID, t.AccountID, t.Scope,
		t.AccessExpiresAt, t.RefreshExpiresAt)
	return err
}

// AccessTokenByHash resolves a current (non-revoked, unexpired) access token
// to the account it was issued for — used to authenticate MCP /mcp calls.
func (s *OAuthStore) AccessTokenByHash(ctx context.Context, tokenHash string) (*OAuthToken, error) {
	t := &OAuthToken{TokenHash: tokenHash}
	err := s.pool.QueryRow(ctx,
		`SELECT id, client_id, user_id, account_id, scope, access_expires_at, refresh_expires_at, revoked_at
		 FROM oauth_tokens
		 WHERE token_hash = $1 AND revoked_at IS NULL AND access_expires_at > now()`,
		tokenHash,
	).Scan(&t.ID, &t.ClientID, &t.UserID, &t.AccountID, &t.Scope, &t.AccessExpiresAt, &t.RefreshExpiresAt, &t.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAccessTokenInvalid
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

// RefreshGrant atomically rotates an OAuth token pair on grant_type=refresh_token.
// It locks the existing row by refresh_hash, revokes it (single-use refresh),
// and inserts a fresh pair carrying the same client/user/account/scope.
func (s *OAuthStore) RefreshGrant(ctx context.Context, refreshHash, newAccessHash, newRefreshHash string, accessExpires, refreshExpires time.Time) (*OAuthToken, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	old := &OAuthToken{}
	err = tx.QueryRow(ctx,
		`SELECT id, client_id, user_id, account_id, scope
		 FROM oauth_tokens
		 WHERE refresh_hash = $1 AND revoked_at IS NULL
		   AND (refresh_expires_at IS NULL OR refresh_expires_at > now())
		 FOR UPDATE`,
		refreshHash,
	).Scan(&old.ID, &old.ClientID, &old.UserID, &old.AccountID, &old.Scope)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRefreshInvalid
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE oauth_tokens SET revoked_at = now() WHERE id = $1`, old.ID); err != nil {
		return nil, err
	}
	nt := &OAuthToken{
		TokenHash: newAccessHash, ClientID: old.ClientID, UserID: old.UserID,
		AccountID: old.AccountID, Scope: old.Scope,
		AccessExpiresAt: accessExpires, RefreshExpiresAt: &refreshExpires,
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO oauth_tokens (token_hash, refresh_hash, client_id, user_id, account_id, scope,
		                      access_expires_at, refresh_expires_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		nt.TokenHash, newRefreshHash, nt.ClientID, nt.UserID, nt.AccountID, nt.Scope,
		nt.AccessExpiresAt, nt.RefreshExpiresAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return nt, nil
}

// RevokeAccessToken revokes a specific access token (RFC 7009). A nil return
// means no active row matched (already revoked — idempotent, still OK).
func (s *OAuthStore) RevokeAccessToken(ctx context.Context, tokenHash string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE oauth_tokens SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL`, tokenHash)
	return err
}

// RevokeByRefreshHash revokes the whole token pair identified by a raw refresh
// token's hash (RFC 7009 with token_type_hint=refresh_token). Revoking the
// refresh token also kills the paired access token.
func (s *OAuthStore) RevokeByRefreshHash(ctx context.Context, refreshHash string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE oauth_tokens SET revoked_at = now() WHERE refresh_hash = $1 AND revoked_at IS NULL`, refreshHash)
	return err
}

// RevokeAllByAccount revokes every token for an account (settings "Revoke all"
// / logout). Used to kill all OAuth access after the merchant revokes.
func (s *OAuthStore) RevokeAllByAccount(ctx context.Context, accountID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE oauth_tokens SET revoked_at = now() WHERE account_id = $1 AND revoked_at IS NULL`, accountID)
	return err
}
