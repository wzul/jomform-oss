// RFC 8628 device authorisation (card t_9a6cc85f) and the "Connected apps"
// token inventory.
//
// The device grant exists so a headless AI harness can onboard WITHOUT a
// browser: the client asks for a device_code, the AUTHORIZATION SERVER mails a
// user_code to the account's registered address, and the harness polls
// /oauth/token until a person approved it. That makes the email the consent
// screen, so everything the consent covers (client, scope, requester) is
// recorded on the row the mail is built from.
//
// Both secrets are stored as SHA-256 hashes, exactly like the authorization
// codes and tokens beside them: a dump of this table must not yield a session.
package dbstore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// DeviceAuthorisation errors. The token endpoint maps them onto the RFC 8628
// §3.5 error codes, so they are deliberately named after the states they
// describe rather than after the HTTP surface.
var (
	// ErrDeviceCodeInvalid: unknown, already consumed, or wrong principal.
	ErrDeviceCodeInvalid = errors.New("device code is invalid or already used")
	// ErrDevicePending: not approved (or denied) yet.
	ErrDevicePending = errors.New("authorization_pending")
	// ErrDeviceDenied: the person said no.
	ErrDeviceDenied = errors.New("access_denied")
	// ErrDeviceExpired: past expires_at.
	ErrDeviceExpired = errors.New("expired_token")
	// ErrDeviceSlowDown: polled faster than interval_s; the interval has been
	// penalised by 5s for the rest of the authorisation.
	ErrDeviceSlowDown = errors.New("slow_down")
	// ErrOAuthTokenMissing: no live token row with that id for this user.
	ErrOAuthTokenMissing = errors.New("oauth token not found")
)

// OAuthDeviceCode models one in-flight device authorisation (hashes stored).
type OAuthDeviceCode struct {
	DeviceHash   string
	UserCodeHash string
	ClientID     string
	UserID       string
	AccountID    string
	Scope        []string
	IP           string
	UserAgent    string
	ExpiresAt    time.Time
	IntervalS    int
	LastPolledAt *time.Time
	ApprovedAt   *time.Time
	DeniedAt     *time.Time
	ConsumedAt   *time.Time
	CreatedAt    time.Time
}

// CreateDeviceCode persists a new device authorisation. The row is created
// BEFORE any mail is sent (see httpapi/oauth_device.go): the mail carries the
// user_code of a row that already exists, so a failed send leaves an
// authorisation nobody can approve rather than a code that resolves to nothing.
//
// It also prunes rows whose 10-minute life ended more than a day ago. That is a
// best-effort DELETE on an indexed time column — this table is written once per
// onboarding attempt and must not grow forever.
func (s *OAuthStore) CreateDeviceCode(ctx context.Context, d *OAuthDeviceCode) error {
	if _, err := s.pool.Exec(ctx,
		`DELETE FROM oauth_device_codes WHERE expires_at < now() - interval '1 day'`); err != nil {
		// Pruning is housekeeping; never fail an authorisation on it.
		_ = err
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO oauth_device_codes
		 (device_hash, user_code_hash, client_id, user_id, account_id, scope, ip, user_agent,
		  expires_at, interval_s)
		 VALUES ($1,$2,$3,NULLIF($4,'')::uuid,NULLIF($5,'')::uuid,$6,$7,$8,$9,$10)`,
		d.DeviceHash, d.UserCodeHash, d.ClientID, d.UserID, d.AccountID, d.Scope,
		d.IP, d.UserAgent, d.ExpiresAt, d.IntervalS)
	return err
}

// CountDeviceCodesByIP returns how many authorisations this address started
// since `since` — the per-IP half of the hourly rate limit that keeps the
// email path from becoming a mail bomb.
func (s *OAuthStore) CountDeviceCodesByIP(ctx context.Context, ip string, since time.Time) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM oauth_device_codes WHERE ip = $1 AND created_at >= $2`,
		ip, since).Scan(&n)
	return n, err
}

// CountDeviceCodesByUser returns how many authorisations this account started
// since `since` — the per-account half of the hourly rate limit.
func (s *OAuthStore) CountDeviceCodesByUser(ctx context.Context, userID string, since time.Time) (int, error) {
	if userID == "" {
		return 0, nil
	}
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM oauth_device_codes WHERE user_id = $1 AND created_at >= $2`,
		userID, since).Scan(&n)
	return n, err
}

// deviceSelectColumnList is the ONE column list every device-code read scans.
// Keeping it in a constant next to the struct means a migration that adds a
// column has exactly one place to update (the same rule the *Select constants
// follow for the account/form/product tables).
const deviceSelectColumnList = `device_hash, user_code_hash, client_id,
	COALESCE(user_id::text,''), COALESCE(account_id::text,''), scope, ip, user_agent,
	expires_at, interval_s, last_polled_at, approved_at, denied_at, consumed_at, created_at`

func scanDeviceCode(row pgx.Row) (*OAuthDeviceCode, error) {
	d := &OAuthDeviceCode{}
	err := row.Scan(&d.DeviceHash, &d.UserCodeHash, &d.ClientID, &d.UserID, &d.AccountID,
		&d.Scope, &d.IP, &d.UserAgent, &d.ExpiresAt, &d.IntervalS,
		&d.LastPolledAt, &d.ApprovedAt, &d.DeniedAt, &d.ConsumedAt, &d.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrDeviceCodeInvalid
	}
	if err != nil {
		return nil, err
	}
	return d, nil
}

// DeviceCodeByUserCode resolves the authorisation a person was mailed, for the
// dashboard's approve/deny card. It is a plain read: whether the row is still
// approvable is decided by ApproveDeviceCode/DenyDeviceCode, which lock it.
func (s *OAuthStore) DeviceCodeByUserCode(ctx context.Context, userCodeHash string) (*OAuthDeviceCode, error) {
	return scanDeviceCode(s.pool.QueryRow(ctx,
		`SELECT `+deviceSelectColumnList+` FROM oauth_device_codes WHERE user_code_hash = $1`,
		userCodeHash))
}

// ApproveDeviceCode records consent for a device authorisation after the
// person confirmed it on jomform.com (the email's link lands there).
//
// The person must be the one the mail was sent to: when the row already names a
// user, only that user may approve it. An unbound row (no login_hint, or a
// login_hint that matched nobody) is bound to the approving session here —
// that is the RFC 8628 "type the code at the verification URI" path.
//
// `scope` may only NARROW the requested set. Widening is refused by the caller
// AND re-checked here, because this is the last write before a token exists.
func (s *OAuthStore) ApproveDeviceCode(ctx context.Context, userCodeHash, userID, accountID string, scope []string) (*OAuthDeviceCode, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	d, err := scanDeviceCode(tx.QueryRow(ctx,
		`SELECT `+deviceSelectColumnList+` FROM oauth_device_codes
		 WHERE user_code_hash = $1 FOR UPDATE`, userCodeHash))
	if err != nil {
		return nil, err
	}
	switch {
	case d.ConsumedAt != nil, d.ApprovedAt != nil:
		return nil, ErrDeviceCodeInvalid
	case d.DeniedAt != nil:
		return nil, ErrDeviceDenied
	case !time.Now().Before(d.ExpiresAt):
		return nil, ErrDeviceExpired
	case d.UserID != "" && d.UserID != userID:
		// Someone else's authorisation: indistinguishable from unknown.
		return nil, ErrDeviceCodeInvalid
	case !scopeSubset(scope, d.Scope):
		return nil, ErrDeviceCodeInvalid
	}
	if _, err := tx.Exec(ctx,
		`UPDATE oauth_device_codes
		 SET approved_at = now(), user_id = $2, account_id = NULLIF($3,'')::uuid, scope = $4
		 WHERE user_code_hash = $1`,
		userCodeHash, userID, accountID, scope); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	d.UserID, d.AccountID, d.Scope = userID, accountID, scope
	now := time.Now().UTC()
	d.ApprovedAt = &now
	return d, nil
}

// DenyDeviceCode records a refusal. The token endpoint then answers
// access_denied (RFC 8628 §3.5) instead of polling forever.
func (s *OAuthStore) DenyDeviceCode(ctx context.Context, userCodeHash, userID string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE oauth_device_codes SET denied_at = now()
		 WHERE user_code_hash = $1 AND consumed_at IS NULL AND approved_at IS NULL
		   AND denied_at IS NULL AND expires_at > now()
		   AND (user_id IS NULL OR user_id::text = $2)`,
		userCodeHash, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrDeviceCodeInvalid
	}
	return nil
}

// PollDeviceCode is the token endpoint's side of RFC 8628 §3.4/§3.5. It runs
// under a row lock so the states cannot race each other, and it enforces every
// rule the RFC gives the server, in the RFC's order:
//
//	unknown/consumed → ErrDeviceCodeInvalid
//	denied           → ErrDeviceDenied   (access_denied)
//	expired          → ErrDeviceExpired  (expired_token)
//	polled too fast  → ErrDeviceSlowDown (slow_down, +5s penalty, persisted)
//	not approved yet → ErrDevicePending  (authorization_pending)
//	approved         → CONSUMED HERE, row returned for token minting
//
// Consumption is part of the same locked transaction, so two polls of one
// approved code cannot both mint a token: the second finds consumed_at set.
func (s *OAuthStore) PollDeviceCode(ctx context.Context, deviceHash, clientID string) (*OAuthDeviceCode, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	d, err := scanDeviceCode(tx.QueryRow(ctx,
		`SELECT `+deviceSelectColumnList+` FROM oauth_device_codes
		 WHERE device_hash = $1 FOR UPDATE`, deviceHash))
	if err != nil {
		return nil, err
	}
	if d.ConsumedAt != nil {
		return nil, ErrDeviceCodeInvalid
	}
	// RFC 8628 §3.4: the token request repeats client_id. A mismatch must not
	// burn the code, so it is checked before anything is written.
	if clientID != "" && d.ClientID != clientID {
		return nil, ErrDeviceCodeInvalid
	}
	if d.DeniedAt != nil {
		return nil, ErrDeviceDenied
	}
	now := time.Now().UTC()
	if !now.Before(d.ExpiresAt) {
		return nil, ErrDeviceExpired
	}
	if d.LastPolledAt != nil {
		interval := time.Duration(d.IntervalS) * time.Second
		if now.Sub(*d.LastPolledAt) < interval {
			// RFC 8628 §3.5: answer slow_down AND add 5 seconds to the
			// interval for this and all subsequent polls.
			if _, err := tx.Exec(ctx,
				`UPDATE oauth_device_codes SET interval_s = interval_s + 5, last_polled_at = now()
				 WHERE device_hash = $1`, deviceHash); err != nil {
				return nil, err
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
			return nil, ErrDeviceSlowDown
		}
	}
	if d.ApprovedAt == nil {
		if _, err := tx.Exec(ctx,
			`UPDATE oauth_device_codes SET last_polled_at = now() WHERE device_hash = $1`,
			deviceHash); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		d.LastPolledAt = &now
		return nil, ErrDevicePending
	}
	// Approved: consume exactly once, inside the same locked transaction.
	tag, err := tx.Exec(ctx,
		`UPDATE oauth_device_codes SET consumed_at = now()
		 WHERE device_hash = $1 AND consumed_at IS NULL`, deviceHash)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() != 1 {
		return nil, ErrDeviceCodeInvalid
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	d.ConsumedAt = &now
	d.LastPolledAt = &now
	return d, nil
}

// scopeSubset reports whether every scope in `want` is present in `have`.
// Used to guarantee an approval can only narrow the client's request.
func scopeSubset(want, have []string) bool {
	set := make(map[string]bool, len(have))
	for _, s := range have {
		set[s] = true
	}
	for _, s := range want {
		if !set[s] {
			return false
		}
	}
	return true
}

// ─── Connected apps ──────────────────────────────────────────────────────────

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
// A pair is live while it is not revoked and either its access token or its
// refresh token can still be used; an access token that merely expired an hour
// ago belongs to a connection that still works, so it stays listed.
func (s *OAuthStore) ConnectedAppsByUser(ctx context.Context, userID string) ([]ConnectedApp, error) {
	rows, err := s.pool.Query(ctx, `
SELECT t.id::text, t.client_id, COALESCE(c.client_name, t.client_id), t.scope,
       t.created_at, t.last_used_at, t.access_expires_at, t.refresh_expires_at
FROM oauth_tokens t
LEFT JOIN oauth_clients c ON c.client_id = t.client_id
WHERE t.user_id = $1 AND t.revoked_at IS NULL
  AND (t.access_expires_at > now() OR (t.refresh_expires_at IS NOT NULL AND t.refresh_expires_at > now()))
ORDER BY t.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConnectedApp
	for rows.Next() {
		var a ConnectedApp
		if err := rows.Scan(&a.ID, &a.ClientID, &a.ClientName, &a.Scope, &a.CreatedAt,
			&a.LastUsedAt, &a.AccessExpiresAt, &a.RefreshExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// RevokeTokenByIDForUser revokes ONE token pair the user owns. Access and
// refresh live on the same row, so revoking it kills both in a single UPDATE —
// which is what the Connected-apps "Revoke" button promises.
//
// The user_id predicate is the authorization: an id belonging to somebody else
// matches nothing and reads back as ErrOAuthTokenMissing.
func (s *OAuthStore) RevokeTokenByIDForUser(ctx context.Context, userID, id string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE oauth_tokens SET revoked_at = now()
		 WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrOAuthTokenMissing
	}
	return nil
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
func (s *OAuthStore) RotateTokenScope(ctx context.Context, userID, id string, newScope []string, newAccessHash, newRefreshHash string, accessExpires, refreshExpires time.Time) (*OAuthToken, []string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	old := &OAuthToken{}
	err = tx.QueryRow(ctx,
		`SELECT client_id, user_id, COALESCE(account_id::text,''), scope
		 FROM oauth_tokens
		 WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL
		 FOR UPDATE`, id, userID,
	).Scan(&old.ClientID, &old.UserID, &old.AccountID, &old.Scope)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrOAuthTokenMissing
	}
	if err != nil {
		return nil, nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE oauth_tokens SET revoked_at = now() WHERE id = $1`, id); err != nil {
		return nil, nil, err
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

// ScopeString joins a scope set the way RFC 6749 §3.3 spells it, for audit
// payloads. Local to this package because every other caller already has the
// slice it wants to print.
func ScopeString(scope []string) string { return strings.Join(scope, " ") }
