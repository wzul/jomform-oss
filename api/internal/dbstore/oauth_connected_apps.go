// The "Connected apps" token inventory: one row per LIVE oauth_tokens pair.
//
// Moved out of oauth_device.go with card t_df243a6e so the token-pair files are
// not the device-flow files: the two aggregates share an OAuthStore but nothing
// else, and a change to one should never be a merge conflict in the other.
package dbstore

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/wzul/jomform/api/internal/statemachine"
)

// ConnectedApp is one live OAuth token pair as the dashboard lists it: which
// client holds it, what it may do, when it was granted and last used.
type ConnectedApp struct {
	ID               string
	ClientID         string
	ClientName       string
	Scope            []string
	CreatedAt        time.Time
	LastUsedAt       *time.Time
	AccessExpiresAt  time.Time
	RefreshExpiresAt *time.Time
}

// ConnectedAppsByUser lists every LIVE token pair the user holds — one row per
// pair, not per client. That difference matters: the scope ladder rotates a
// pair when it changes, and the user has to be able to see (and revoke) the
// credential that actually exists.
//
// "LIVE" IS THE TABLE'S ANSWER (card t_df243a6e): statemachine.TokenStatusOf's
// Listed — a live access token OR a usable refresh credential. The query used to
// decide it inline (`refresh_expires_at IS NOT NULL AND ... > now()`) and so read
// a refresh credential with no deadline as dead, while RefreshGrant read the same
// row as refreshable: the dashboard hid a connection that still worked. Both
// readers now ask the one table.
func (s *OAuthStore) ConnectedAppsByUser(ctx context.Context, userID string) ([]ConnectedApp, error) {
	rows, err := s.pool.Query(ctx, `
SELECT t.id::text, t.client_id, COALESCE(c.client_name, t.client_id), t.scope,
       t.created_at, t.last_used_at, t.access_expires_at, t.refresh_expires_at, t.revoked_at,
       COALESCE(t.refresh_hash,'')
FROM oauth_tokens t
LEFT JOIN oauth_clients c ON c.client_id = t.client_id
WHERE t.user_id = $1
ORDER BY t.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now().UTC()
	var out []ConnectedApp
	for rows.Next() {
		var (
			a           ConnectedApp
			refreshHash string
			revokedAt   *time.Time
		)
		if err := rows.Scan(&a.ID, &a.ClientID, &a.ClientName, &a.Scope, &a.CreatedAt,
			&a.LastUsedAt, &a.AccessExpiresAt, &a.RefreshExpiresAt, &revokedAt,
			&refreshHash); err != nil {
			return nil, err
		}
		status := statemachine.TokenStatusOf(statemachine.TokenFacts{
			HasRefresh:       refreshHash != "",
			AccessExpiresAt:  a.AccessExpiresAt,
			RefreshExpiresAt: a.RefreshExpiresAt,
			RevokedAt:        revokedAt,
		}, now)
		if !status.Listed {
			continue
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// RotateTokenScope replaces a live token pair with a new one carrying a
// DIFFERENT scope, and returns the scope it replaced (for the audit event).
//
// This is the only way a scope ever changes for an existing client. It is done
// as a rotation, not an edit, because a leaked refresh token must stop working
// the moment the user believes the credential changed: the old row is revoked
// in the same transaction that inserts the new one, so there is no window in
// which both are live.
//
// It cannot WIDEN anything by itself — the caller decides the new scope and is
// responsible for having obtained fresh consent (dashboard confirmation for an
// upgrade; the scope ladder refuses a widen through the token endpoint).
//
// WHETHER THE PAIR MAY BE ROTATED IS THE TABLE'S ANSWER (card t_df243a6e): the
// row is locked, then the revoke cell of statemachine.TokenTransition decides —
// allowed in every state except revoked, which is exactly the `revoked_at IS
// NULL` this query used to carry, now asked in one place. A pair that is already
// revoked is not the caller's to rotate any more, and it reads back as the
// frozen ErrOAuthTokenMissing (404).
func (s *OAuthStore) RotateTokenScope(ctx context.Context, userID, id string, newScope []string, newAccessHash, newRefreshHash string, accessExpires, refreshExpires time.Time) (*OAuthToken, []string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	old := &OAuthToken{}
	err = tx.QueryRow(ctx,
		`SELECT client_id, user_id, COALESCE(account_id::text,''), scope,
		        COALESCE(refresh_hash,''), access_expires_at, refresh_expires_at, revoked_at
		 FROM oauth_tokens
		 WHERE id = $1 AND user_id = $2
		 FOR UPDATE`, id, userID,
	).Scan(&old.ClientID, &old.UserID, &old.AccountID, &old.Scope, &old.RefreshHash,
		&old.AccessExpiresAt, &old.RefreshExpiresAt, &old.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrOAuthTokenMissing
	}
	if err != nil {
		return nil, nil, err
	}
	if step := statemachine.TokenTransition(old.StateAt(time.Now().UTC()), statemachine.TokenEventRevoke); !step.Allowed {
		return nil, nil, ErrOAuthTokenMissing
	}
	tag, err := tx.Exec(ctx,
		`UPDATE oauth_tokens SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, id)
	if err != nil {
		return nil, nil, err
	}
	if tag.RowsAffected() != 1 {
		// Belt on top of the lock: never report a rotation whose revocation did
		// not land.
		return nil, nil, ErrOAuthTokenMissing
	}
	nt := &OAuthToken{
		TokenHash: newAccessHash, RefreshHash: newRefreshHash,
		ClientID: old.ClientID, UserID: old.UserID, AccountID: old.AccountID,
		Scope: newScope, AccessExpiresAt: accessExpires, RefreshExpiresAt: &refreshExpires,
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO oauth_tokens (token_hash, refresh_hash, client_id, user_id, account_id, scope,
		                       access_expires_at, refresh_expires_at)
		 VALUES ($1,$2,$3,$4,NULLIF($5,'')::uuid,$6,$7,$8)
		 RETURNING id::text`,
		nt.TokenHash, nt.RefreshHash, nt.ClientID, nt.UserID, nt.AccountID, nt.Scope,
		nt.AccessExpiresAt, nt.RefreshExpiresAt).Scan(&nt.ID); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	return nt, old.Scope, nil
}
