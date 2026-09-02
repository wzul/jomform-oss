// Package auth — session management backed by Valkey.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const SessionTTL = 14 * 24 * time.Hour

// Sessions stores browser sessions in Valkey.
type Sessions struct {
	rdb *goredis.Client
}

// NewSessions wires a Valkey client. go-redis only accepts redis:// /
// rediss:// URL schemes, so a valkey:// prefix (RESP-compatible, same wire
// protocol) is transparently rewritten before parsing.
func NewSessions(valkeyURL string) (*Sessions, error) {
	normalized := strings.TrimPrefix(valkeyURL, "valkey://")
	normalized = "redis://" + normalized
	opts, err := goredis.ParseURL(normalized)
	if err != nil {
		return nil, fmt.Errorf("parse valkey url: %w", err)
	}
	return &Sessions{rdb: goredis.NewClient(opts)}, nil
}

// sessionKey namespaces session tokens in the keyspace.
func sessionKey(token string) string { return "jf:session:" + token }

// value encodes the stored userID together with the session's issued-at
// timestamp (unix ns) and the remember flag (issue #69). Formats over time:
//   - legacy v1: bare userID (no separator)
//   - v2:        userID:issuedNs
//   - v3 (now):  userID:issuedNs:remember  (remember "1"/"0")
//
// The remember flag is persisted so a session-rotating action (2FA enable per
// issue #69) can inherit the original session's persistence choice instead of
// silently downgrading a "remember me" login to a 24h browser-session cookie.
const valueSep = ":"

func encodeValue(userID string, issued time.Time, remember bool) string {
	flag := "0"
	if remember {
		flag = "1"
	}
	return userID + valueSep + strconv.FormatInt(issued.UnixNano(), 10) + valueSep + flag
}

// parseValue returns the userID, issuance time, and remember flag. It tolerates
// every historical value format. A value carrying a zero issued time (bare or
// malformed) means "issued unknown" and defaults remember to false (the safe,
// short-session default).
func parseValue(v string) (userID string, issued time.Time, remember bool) {
	parts := strings.Split(v, valueSep)
	if len(parts) > 0 {
		userID = parts[0]
	}
	if len(parts) >= 2 {
		if ns, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
			issued = time.Unix(0, ns).UTC()
		}
	}
	if len(parts) >= 3 && parts[2] == "1" {
		remember = true
	}
	return userID, issued, remember
}

// Create stores userID under a fresh opaque token with the default SessionTTL
// (14d) and returns it.
func (s *Sessions) Create(ctx context.Context, userID string) (string, error) {
	return s.CreateWithTTL(ctx, userID, SessionTTL)
}

// CreateWithTTL stores userID under a fresh opaque token with the given TTL
// and returns the token. TTL drives both the Valkey expiry and (caller-side)
// the cookie MaxAge, so remember-me works end to end (issue #62). Remember is
// stored into the session value so rotation can inherit it (issue #69).
func (s *Sessions) CreateWithTTL(ctx context.Context, userID string, ttl time.Duration) (string, error) {
	return s.CreateRememberedWithTTL(ctx, userID, ttl, false)
}

// CreateRememberedWithTTL stores userID under a fresh opaque token with the
// given TTL and remember flag. Same contract as CreateWithTTL plus persisting
// remember so a later session rotation can preserve the persistence choice.
func (s *Sessions) CreateRememberedWithTTL(ctx context.Context, userID string, ttl time.Duration, remember bool) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	if err := s.rdb.Set(ctx, sessionKey(token), encodeValue(userID, time.Now().UTC(), remember), ttl).Err(); err != nil {
		return "", fmt.Errorf("session create: %w", err)
	}
	return token, nil
}

// Get resolves a session token to its userID.
func (s *Sessions) Get(ctx context.Context, token string) (string, error) {
	v, err := s.rdb.Get(ctx, sessionKey(token)).Result()
	if err != nil {
		return "", err
	}
	uid, _, _ := parseValue(v)
	return uid, nil
}

// GetCreated resolves a session token to its userID and issuance time. It is
// used by session resolution to reject sessions older than a password reset.
func (s *Sessions) GetCreated(ctx context.Context, token string) (string, time.Time, error) {
	v, err := s.rdb.Get(ctx, sessionKey(token)).Result()
	if err != nil {
		return "", time.Time{}, err
	}
	uid, issued, _ := parseValue(v)
	return uid, issued, nil
}

// Remembered resolves a session token to its persisted remember flag. A
// missing/legacy session is treated as remember=false (24h/browser-session) —
// the safe default. Used by session-rotating handlers (2FA enable, issue #69)
// to inherit the original login's persistence choice.
func (s *Sessions) Remembered(ctx context.Context, token string) bool {
	if token == "" {
		return false
	}
	v, err := s.rdb.Get(ctx, sessionKey(token)).Result()
	if err != nil {
		return false
	}
	_, _, remember := parseValue(v)
	return remember
}

// ValidAfter reports whether the session's issuance time is not before cut —
// used to reject sessions created before a pw_reset_at timestamp. A session
// with an unknown (zero) issuance time is treated as valid only when cut is
// zero (the user has never reset), so existing sessions are never locked out.
func ValidAfter(issued time.Time, cut time.Time) bool {
	if cut.IsZero() {
		return true
	}
	if issued.IsZero() {
		// Legacy (pre-upgrade) session value carries no timestamp. Only reach
		// here when the user has reset (cut != 0); such a session predates the
		// reset by construction, so reject it conservatively.
		return false
	}
	return issued.After(cut) || issued.Equal(cut)
}

// Destroy removes a session (logout).
func (s *Sessions) Destroy(ctx context.Context, token string) error {
	return s.rdb.Del(ctx, sessionKey(token)).Err()
}

// Ping verifies the Valkey connection for readiness probes.
func (s *Sessions) Ping(ctx context.Context) error {
	return s.rdb.Ping(ctx).Err()
}

// Client exposes the underlying Valkey client so the login-failure limiter can
// reuse the same connection pool (issue #42).
func (s *Sessions) Client() *goredis.Client { return s.rdb }

// Close releases the connection pool.
func (s *Sessions) Close() error { return s.rdb.Close() }

// PendingTOTP stores a generated-but-not-yet-enabled 2FA secret in Valkey.
// The secret only becomes active (DB) after the user confirms a valid code,
// so an abandoned setup never arms the login challenge.
const (
	PendingTOTPKeyPrefix = "jf:totp_pending:"
	PendingTOTPTTL       = 10 * time.Minute
)

func pendingTOTPKey(userID string) string { return PendingTOTPKeyPrefix + userID }

// PendingTOTP exposes a pending-2FA store sharing the session client.
func (s *Sessions) PendingTOTP() *PendingTOTP { return &PendingTOTP{rdb: s.rdb} }

type PendingTOTP struct {
	rdb *goredis.Client
}

// Set stores the pending secret with a short TTL.
func (p *PendingTOTP) Set(ctx context.Context, userID, secret string) error {
	return p.rdb.Set(ctx, pendingTOTPKey(userID), secret, PendingTOTPTTL).Err()
}

// Get returns the pending secret (redis.Nil when none or expired).
func (p *PendingTOTP) Get(ctx context.Context, userID string) (string, error) {
	return p.rdb.Get(ctx, pendingTOTPKey(userID)).Result()
}

// Delete removes the pending secret (used after successful enable).
func (p *PendingTOTP) Delete(ctx context.Context, userID string) error {
	return p.rdb.Del(ctx, pendingTOTPKey(userID)).Err()
}

// ─── shared helpers ──────────────────────────────────────────────────────────

// randomToken returns a hex-encoded 32-byte (256-bit) random token.
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// hashOf returns the hex SHA-256 of a token (tokens stored only hashed, so a
// Valkey leak never exposes usable credential/password-reset tokens).
func hashOf(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// ─── password-reset tokens (issue #62) ──────────────────────────────────────
//
// The reset link ships the raw 256-bit token to the user's inbox; only its
// SHA-256 is stored in Valkey, TTL 30 minutes, single-use (DEL on consume).

const (
	PasswordResetKeyPrefix = "jf:pwreset:"
	PasswordResetTTL       = 30 * time.Minute
)

func passwordResetKey(hash string) string { return PasswordResetKeyPrefix + hash }

// PasswordReset exposes a single-use, hashed, 30-min password-reset token
// store backed by the shared Valkey client.
func (s *Sessions) PasswordReset() *PasswordReset { return &PasswordReset{rdb: s.rdb} }

type PasswordReset struct {
	rdb *goredis.Client
}

// Generate mints a fresh raw token, stores its hash→userID mapping and returns
// the raw token (the only copy of the usable secret).
func (p *PasswordReset) Generate(ctx context.Context, userID string) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	if err := p.rdb.Set(ctx, passwordResetKey(hashOf(token)), userID, PasswordResetTTL).Err(); err != nil {
		return "", fmt.Errorf("pwreset set: %w", err)
	}
	return token, nil
}

// ErrResetTokenInvalid is returned when the token is absent, expired, or
// already consumed.
var ErrResetTokenInvalid = errors.New("password reset token invalid")

// Consume atomically invalidates the token and returns the userID it was
// issued to. A single-use token can never be replayed.
func (p *PasswordReset) Consume(ctx context.Context, token string) (string, error) {
	if token == "" {
		return "", ErrResetTokenInvalid
	}
	key := passwordResetKey(hashOf(token))
	// GET before DEL so we only delete when the mapping actually existed
	// (redis handles this atomically under a script, but the token hash is a
	// unique key so GET→DEL is race-safe here: a second consumer would GET
	// redis.Nil after the first DEL).
	userID, err := p.rdb.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return "", ErrResetTokenInvalid
		}
		return "", err
	}
	if err := p.rdb.Del(ctx, key).Err(); err != nil {
		return "", err
	}
	return userID, nil
}

// ─── trusted devices (issue #63) ────────────────────────────────────────────
//
// jf_trust is a single-purpose device cookie that waives TOTP on subsequent
// logins (password still required every time). Raw token lives in the cookie;
// only its SHA-256 is stored. A per-user set key makes invalidation trivial.

const (
	TrustKeyPrefix   = "jf:trust:"
	TrustSetKeyPrefix = "jf:trustset:"
	TrustTTL         = 30 * 24 * time.Hour
)

func trustKey(hash string) string           { return TrustKeyPrefix + hash }
func trustSetKey(userID string) string      { return TrustSetKeyPrefix + userID }

// TrustedDevices exposes the trust-cookie store sharing the session client.
func (s *Sessions) TrustedDevices() *TrustedDevices { return &TrustedDevices{rdb: s.rdb} }

type TrustedDevices struct {
	rdb *goredis.Client
}

// Issue mints a fresh trust token, stores hash→userID (TTL 30d), adds it to
// the user's trust-set, and returns the raw token for the cookie.
func (t *TrustedDevices) Issue(ctx context.Context, userID string) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	h := hashOf(token)
	pipe := t.rdb.TxPipeline()
	pipe.Set(ctx, trustKey(h), userID, TrustTTL)
	pipe.SAdd(ctx, trustSetKey(userID), h)
	pipe.Expire(ctx, trustSetKey(userID), TrustTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return "", fmt.Errorf("trust issue: %w", err)
	}
	return token, nil
}

// UserIDFor returns the userID a raw trust token maps to, or "" if invalid.
func (t *TrustedDevices) UserIDFor(ctx context.Context, token string) string {
	if token == "" {
		return ""
	}
	userID, err := t.rdb.Get(ctx, trustKey(hashOf(token))).Result()
	if err != nil {
		return ""
	}
	return userID
}

// Expiry returns the absolute expiry time of a trust token, or zero time if
// the token is unknown/expired. Used by the Profile → Security line to show an
// exact "trusted until {date}".
func (t *TrustedDevices) Expiry(ctx context.Context, token string) time.Time {
	if token == "" {
		return time.Time{}
	}
	ttl, err := t.rdb.TTL(ctx, trustKey(hashOf(token))).Result()
	if err != nil || ttl <= 0 {
		return time.Time{}
	}
	return time.Now().UTC().Add(ttl)
}

// Revoke deletes the trust entry (by raw token) and removes it from the
// user's set — used by "forget this device".
func (t *TrustedDevices) Revoke(ctx context.Context, userID, token string) error {
	h := hashOf(token)
	pipe := t.rdb.TxPipeline()
	pipe.Del(ctx, trustKey(h))
	pipe.SRem(ctx, trustSetKey(userID), h)
	_, err := pipe.Exec(ctx)
	return err
}

// RevokeAll deletes every trust entry for a user — called on password reset
// and 2FA disable.
func (t *TrustedDevices) RevokeAll(ctx context.Context, userID string) error {
	members, err := t.rdb.SMembers(ctx, trustSetKey(userID)).Result()
	if err != nil {
		return err
	}
	if len(members) == 0 {
		return t.rdb.Del(ctx, trustSetKey(userID)).Err()
	}
	keys := make([]string, 0, len(members)+1)
	for _, h := range members {
		keys = append(keys, trustKey(h))
	}
	keys = append(keys, trustSetKey(userID))
	return t.rdb.Del(ctx, keys...).Err()
}