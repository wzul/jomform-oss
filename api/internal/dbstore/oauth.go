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

	"github.com/wzul/jomform/api/internal/statemachine"
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
	ConsumedAt    *time.Time
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

// Facts renders the row's state-bearing columns for the ONE derivation
// (statemachine.TokenStateOf). Every reader goes through this — the /mcp
// authenticator, the REST resolver, the connected-apps and connected-agents
// lists, and the refresh arm of the token endpoint — so a screen and an endpoint
// cannot disagree about whether a pair is still usable (card t_df243a6e).
//
// last_used_at is not one of them: it records that the credential was used,
// never whether it may be.
func (t *OAuthToken) Facts() statemachine.TokenFacts {
	return statemachine.TokenFacts{
		HasRefresh:       t.RefreshHash != "",
		AccessExpiresAt:  t.AccessExpiresAt,
		RefreshExpiresAt: t.RefreshExpiresAt,
		RevokedAt:        t.RevokedAt,
	}
}

// StateAt derives the pair's state at `now`. Reads take an explicit clock so a
// test can place the row on either side of a deadline without sleeping.
func (t *OAuthToken) StateAt(now time.Time) statemachine.TokenState {
	return statemachine.TokenStateOf(t.Facts(), now)
}

// Facts renders the authorization code's state-bearing columns for the ONE
// derivation (statemachine.AuthCodeStateOf). The code exchange (ConsumeAuthCode)
// is the only reader a code has, and it asks the redeem cell of the table.
func (c *OAuthAuthCode) Facts() statemachine.AuthCodeFacts {
	return statemachine.AuthCodeFacts{ExpiresAt: c.ExpiresAt, ConsumedAt: c.ConsumedAt}
}

// tokenErrFor is the ONE place a state-machine answer becomes a token store
// error. The two error values are unchanged by card t_df243a6e — only their
// source moved, from two hand-written SQL predicates to the table in
// statemachine — and they are the pair the callers already map to 401 and to
// the token endpoint's invalid_grant.
func tokenErrFor(reason statemachine.TokenReason) error {
	switch reason {
	case statemachine.TokenReasonNone:
		return nil
	case statemachine.TokenReasonInvalidGrant:
		return ErrRefreshInvalid
	default:
		return ErrAccessTokenInvalid
	}
}

// authCodeErrFor is the code-exchange counterpart. One frozen error covers every
// refusal — unknown, spent and expired are indistinguishable on the wire
// (RFC 6749 §5.2 answers invalid_grant for all three).
func authCodeErrFor(reason statemachine.AuthCodeReason) error {
	if reason == statemachine.AuthCodeReasonNone {
		return nil
	}
	return ErrAuthCodeInvalid
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
		 VALUES ($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8)`,
		codeHash, c.ClientID, c.UserID, c.AccountID, c.RedirectURI, c.CodeChallenge, c.Scope, c.ExpiresAt)
	return err
}

// ConsumeAuthCode atomically redeems a single-use authorization code. Returns
// the code payload (for PKCE verification + token issue) or an error.
//
// WHETHER THE CODE MAY BE REDEEMED IS THE TABLE'S ANSWER (card t_df243a6e), not
// a SQL predicate only this call site could see: the row is locked, its state
// comes from statemachine.AuthCodeStateOf and the redeem cell of the table
// decides. The UPDATE keeps `consumed_at IS NULL AND expires_at > now()` as the
// belt: it is the atomic compare-and-swap that makes the code single-use under
// concurrency (a second consumer blocks on the lock, then finds the row
// redeemed), and it is the DATABASE's clock that bounds the window. The error
// value is frozen — ErrAuthCodeInvalid, whatever the reason.
func (s *OAuthStore) ConsumeAuthCode(ctx context.Context, codeHash string) (*OAuthAuthCode, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	c := &OAuthAuthCode{}
	err = tx.QueryRow(ctx,
		`SELECT client_id, user_id, COALESCE(account_id::text,''), redirect_uri, code_challenge, scope,
		        expires_at, consumed_at
		 FROM oauth_auth_codes WHERE code_hash = $1 FOR UPDATE`,
		codeHash,
	).Scan(&c.ClientID, &c.UserID, &c.AccountID, &c.RedirectURI, &c.CodeChallenge, &c.Scope,
		&c.ExpiresAt, &c.ConsumedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAuthCodeInvalid
	}
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if step := statemachine.AuthCodeRedeemStep(c.Facts(), now); !step.Allowed {
		return nil, authCodeErrFor(step.Answer)
	}
	tag, err := tx.Exec(ctx,
		`UPDATE oauth_auth_codes SET consumed_at = now()
		 WHERE code_hash = $1 AND consumed_at IS NULL AND expires_at > now()`,
		codeHash)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() != 1 {
		// Belt on top of the lock: never report a redemption that did not land.
		return nil, ErrAuthCodeInvalid
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	c.ConsumedAt = &now
	return c, nil
}

// CreateToken inserts a new opaque access(+refresh) token pair. Only hashes
// are stored. A nil RefreshHash means a non-rotatable access token. account_id
// may be "" (NULL) — issue #307 binds the token to the user, not an account.
func (s *OAuthStore) CreateToken(ctx context.Context, t *OAuthToken) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO oauth_tokens (token_hash, refresh_hash, client_id, user_id, account_id, scope,
		                       access_expires_at, refresh_expires_at)
		 VALUES ($1,$2,$3,$4,NULLIF($5,'')::uuid,$6,$7,$8)`,
		t.TokenHash, t.RefreshHash, t.ClientID, t.UserID, t.AccountID, t.Scope,
		t.AccessExpiresAt, t.RefreshExpiresAt)
	return err
}

// AccessTokenByHash resolves a current access token to the account it was issued
// for — used to authenticate MCP /mcp and /v1/rest calls.
//
// WHETHER IT IS CURRENT IS THE TABLE'S ANSWER (card t_df243a6e): the row is read
// by its unique hash and statemachine.TokenAccessStep decides, instead of a
// `revoked_at IS NULL AND access_expires_at > now()` predicate living in this
// query where no other reader could see it. The error value is frozen —
// ErrAccessTokenInvalid for every refusal, because distinguishing them would
// tell a caller which half of a credential they got wrong.
func (s *OAuthStore) AccessTokenByHash(ctx context.Context, tokenHash string) (*OAuthToken, error) {
	t := &OAuthToken{TokenHash: tokenHash}
	err := s.pool.QueryRow(ctx,
		`SELECT id, client_id, user_id, COALESCE(account_id::text,''), scope,
		        COALESCE(refresh_hash,''), access_expires_at, refresh_expires_at, revoked_at
		 FROM oauth_tokens
		 WHERE token_hash = $1`,
		tokenHash,
	).Scan(&t.ID, &t.ClientID, &t.UserID, &t.AccountID, &t.Scope, &t.RefreshHash,
		&t.AccessExpiresAt, &t.RefreshExpiresAt, &t.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAccessTokenInvalid
	}
	if err != nil {
		return nil, err
	}
	if step := statemachine.TokenAccessStep(t.Facts(), time.Now().UTC()); !step.Allowed {
		return nil, tokenErrFor(step.Answer)
	}
	return t, nil
}

// RefreshGrant atomically rotates an OAuth token pair on grant_type=refresh_token.
// It locks the existing row by refresh_hash, revokes it (single-use refresh),
// and inserts a fresh pair carrying the same client/user/account/scope.
//
// WHETHER THE REFRESH TOKEN MAY BE REDEEMED IS THE TABLE'S ANSWER (card
// t_df243a6e). The row is locked by refresh_hash and statemachine.TokenRefreshStep
// decides — the same call that refuses the token endpoint's answer — instead of
// a `revoked_at IS NULL AND (refresh_expires_at IS NULL OR ... > now())`
// predicate here that the connected-apps list then read the other way round.
// The lock is what makes the rotation single-use: a second consumer blocks,
// re-reads the committed row and finds it revoked. The error value is frozen —
// ErrRefreshInvalid.
func (s *OAuthStore) RefreshGrant(ctx context.Context, refreshHash, newAccessHash, newRefreshHash string, accessExpires, refreshExpires time.Time) (*OAuthToken, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	old := &OAuthToken{}
	err = tx.QueryRow(ctx,
		`SELECT id, client_id, user_id, COALESCE(account_id::text,''), scope,
		        COALESCE(refresh_hash,''), access_expires_at, refresh_expires_at, revoked_at
		 FROM oauth_tokens
		 WHERE refresh_hash = $1
		 FOR UPDATE`,
		refreshHash,
	).Scan(&old.ID, &old.ClientID, &old.UserID, &old.AccountID, &old.Scope, &old.RefreshHash,
		&old.AccessExpiresAt, &old.RefreshExpiresAt, &old.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRefreshInvalid
	}
	if err != nil {
		return nil, err
	}
	if step := statemachine.TokenRefreshStep(old.Facts(), time.Now().UTC()); !step.Allowed {
		return nil, tokenErrFor(step.Answer)
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
		 VALUES ($1,$2,$3,$4,NULLIF($5,'')::uuid,$6,$7,$8)`,
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
//
// The `revoked_at IS NULL` here is NOT a re-derivation of the pair's state: no
// reader's answer depends on it. It is the idempotency guard of a single
// statement, and the wire is 200 either way (RFC 7009 §2.2), so there is nothing
// for the machine to say. Card t_df243a6e moved every predicate that decides an
// ANSWER; the ones that are a compare-and-swap stay where they are, in the one
// statement that is atomic.
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

// UserToken is one connected-agent (OAuth client) the user has an active
// token for, surfaced in the user-level Profile → MCP tab (issue #307).
type UserToken struct {
	ClientID   string
	ClientName string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// ConnectedAgentsByUser lists the distinct OAuth clients the user holds an
// access token for that can authenticate RIGHT NOW — the "connected agents"
// shown in the Profile page (issue #307, user-level).
//
// "Right now" is statemachine.TokenStatusOf's AccessUsable (card t_df243a6e),
// the same derivation the /mcp authenticator uses, instead of a second
// `revoked_at IS NULL AND access_expires_at > now()` predicate in this query:
// the list can then never show a client whose calls are refused, nor hide one
// whose calls work. The aggregation is done here because the state is no longer
// a SQL expression; rows arrive newest-first, so a client's first appearance
// carries the MAX(created_at) the old GROUP BY selected.
func (s *OAuthStore) ConnectedAgentsByUser(ctx context.Context, userID string) ([]UserToken, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.client_id, COALESCE(c.client_name, t.client_id),
		       t.created_at, t.last_used_at,
		       COALESCE(t.refresh_hash,''), t.access_expires_at, t.refresh_expires_at, t.revoked_at
		FROM oauth_tokens t
		LEFT JOIN oauth_clients c ON c.client_id = t.client_id
		WHERE t.user_id = $1
		ORDER BY t.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now().UTC()
	var out []UserToken
	index := make(map[string]int)
	for rows.Next() {
		var (
			clientID, clientName, refreshHash string
			createdAt, accessExpires          time.Time
			lastUsedAt, refreshExpires        *time.Time
			revokedAt                         *time.Time
		)
		if err := rows.Scan(&clientID, &clientName, &createdAt, &lastUsedAt,
			&refreshHash, &accessExpires, &refreshExpires, &revokedAt); err != nil {
			return nil, err
		}
		status := statemachine.TokenStatusOf(statemachine.TokenFacts{
			HasRefresh:       refreshHash != "",
			AccessExpiresAt:  accessExpires,
			RefreshExpiresAt: refreshExpires,
			RevokedAt:        revokedAt,
		}, now)
		if !status.AccessUsable {
			continue
		}
		if i, seen := index[clientID]; seen {
			// MAX(last_used_at): a NULL in SQL means "never used", so a NULL
			// never outranks a timestamp.
			if lastUsedAt != nil && (out[i].LastUsedAt == nil || lastUsedAt.After(*out[i].LastUsedAt)) {
				out[i].LastUsedAt = lastUsedAt
			}
			continue
		}
		index[clientID] = len(out)
		out = append(out, UserToken{
			ClientID: clientID, ClientName: clientName,
			CreatedAt: createdAt, LastUsedAt: lastUsedAt,
		})
	}
	return out, rows.Err()
}

// RevokeAllByUserClient revokes every active token for a given client_id +
// user — the "Disconnect" action on a connected agent in the Profile MCP tab
// (issue #307).
func (s *OAuthStore) RevokeAllByUserClient(ctx context.Context, userID, clientID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE oauth_tokens SET revoked_at = now()
		 WHERE user_id = $1 AND client_id = $2 AND revoked_at IS NULL`, userID, clientID)
	return err
}
