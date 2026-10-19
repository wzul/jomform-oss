// Email backup-code (issue #72) and magic-link (issue #73) token stores.
//
// Both share the PasswordReset design: a raw secret travels to the user's
// inbox, only a digest is stored in Valkey, and it is single-use (atomic
// GetDel on consume) so a leaked key can never be replayed. The DIGEST differs
// between the two, and the reason is the size of the secret: a magic-link token
// is 256 bits of `crypto/rand`, so a plain SHA-256 is one-way by construction,
// while the emailed 2FA backup code is six digits — a bare hash of it is
// invertible by enumeration, so it is keyed with a server-side secret instead
// (see the Email2FA section below).
package auth

import (
	"context"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// ─── 2FA email backup code (issue #72) ──────────────────────────────────────
//
// A six-digit one-time code emailed to the account during login is accepted as
// an alternative to the authenticator TOTP when the app is unavailable. It is:
//   - gated on a short pending flag (`jf:2fa_pending:{ip}:{email}`) that is
//     only armed when a *correct-password* login returned `totp_required`, so
//     a blind attacker who knows the email can't harvest codes;
//   - stored under `jf:2faemail:{userID}` as an HMAC-SHA256 keyed with a
//     server-side secret derived from ENCRYPTION_KEY (10 min TTL);
//   - single-use — verified against the stored digest and then consumed via an
//     atomic GetDel so it can never be replayed.
//
// The digest is KEYED, not merely hashed. The code space is exactly 1,000,000
// values (Generate6Digit), so an unsalted SHA-256 of it is not one-way in any
// useful sense: whoever can read the keyspace — a dump, a snapshot/AOF file,
// MONITOR — enumerates ("%06d" % n) for n < 1e6 and the stored hex names the
// live code, offline, in well under a second. That hands out the SECOND FACTOR
// for a password they already have, for the whole Email2FACodeTTL window. With
// HMAC the reader also needs the server key, which lives in the process
// environment and never in Valkey. (The session re-keying in session.go is the
// same threat model one credential to the right; there a bare SHA-256 is
// enough because a session token is 256 bits.)
//
// There is deliberately NO fallback that accepts a pre-keying `sha256(code)`
// value: keeping one would keep the inversion alive for every code still in
// flight. Codes are re-issuable and the value lives 10 minutes, so a deploy
// costs a user at most one re-request — unlike a session, which lives 14 days
// and therefore earns its migration path.
const (
	Email2FACodeKeyPrefix = "jf:2faemail:" // + {userID}
	Email2FACodeTTL       = 10 * time.Minute

	Email2FAPendingPrefix = "jf:2fa_pending:" // + {ip}:{email}
	Email2FAPendingTTL    = 5 * time.Minute

	// emailCodeKeyInfo pins the subkey's purpose. HKDF's info string is what
	// separates this key from the other uses of the same ENCRYPTION_KEY bytes
	// (AES-GCM at rest, email-verification link signing), so one master secret
	// never signs two different things with the same key.
	emailCodeKeyInfo = "jomform/email2fa-code/v1"
)

// EmailCodeKey derives the HMAC key that authenticates the emailed 2FA backup
// code from the AES-GCM ENCRYPTION_KEY bytes (32 bytes, decoded from the
// 64-char hex env var). It returns an error rather than a zero value when the
// master key is absent or the wrong size, because a keyless digest is exactly
// the defect this replaces.
func EmailCodeKey(encryptionKey []byte) ([]byte, error) {
	if len(encryptionKey) != 32 {
		return nil, errors.New("email 2fa code key: ENCRYPTION_KEY must be 32 bytes")
	}
	return hkdf.Key(sha256.New, encryptionKey, nil, emailCodeKeyInfo, 32)
}

func email2FaCodeKey(userID string) string { return Email2FACodeKeyPrefix + userID }
func email2FaPendingKey(ip, email string) string {
	return Email2FAPendingPrefix + ip + ":" + strings.ToLower(email)
}

// emailCodeDigest binds the code to the server key AND to the account it was
// issued to: hex(HMAC-SHA256(k, "v1|" + userID + "|" + code)). Binding the
// userID means a digest lifted from one user's key and replayed onto another
// user's key (a keyspace WRITE, not just a read) does not verify.
func emailCodeDigest(key []byte, userID, code string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("v1|"))
	mac.Write([]byte(userID))
	mac.Write([]byte("|"))
	mac.Write([]byte(code))
	return hex.EncodeToString(mac.Sum(nil))
}

// Email2FA exposes the email-backup-code store sharing the session client and
// its server-side code key.
func (s *Sessions) Email2FA() *Email2FA { return &Email2FA{rdb: s.rdb, codeKey: s.emailCodeKey} }

type Email2FA struct {
	rdb *goredis.Client
	// codeKey is the HMAC key from EmailCodeKey. Empty means the deployment was
	// wired without ENCRYPTION_KEY: the store then refuses to arm a code rather
	// than falling back to a hash anyone can invert.
	codeKey []byte
}

// ErrEmail2FAKeyMissing is returned by Store when the store has no server-side
// code key. It is a wiring error, not a user error: outside dev ENCRYPTION_KEY
// is required, so this can only fire on a dev instance that never set one.
var ErrEmail2FAKeyMissing = errors.New("email 2fa code store: no server key configured")

// Generate6Digit returns a cryptographically-random 000000-999999 code.
func Generate6Digit() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", fmt.Errorf("email2fa generate: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// Store arms a code for userID for Email2FACodeTTL. A later store for the same
// user replaces the previous (unconsumed) code.
func (e *Email2FA) Store(ctx context.Context, userID, code string) error {
	if len(e.codeKey) == 0 {
		return ErrEmail2FAKeyMissing
	}
	return e.rdb.Set(ctx, email2FaCodeKey(userID), emailCodeDigest(e.codeKey, userID, code), Email2FACodeTTL).Err()
}

// ErrEmail2FAInvalid is returned when the supplied code does not match any
// live emailed code for the user (absent, expired, already consumed, or wrong).
var ErrEmail2FAInvalid = errors.New("email 2fa code invalid")

// Consume verifies code against the stored digest and, on match, atomically
// consumes it via GetDel so it can only ever be used once. A wrong code does
// not consume (a typo doesn't burn the valid code). A store with no code key
// can never match anything, so it fails closed with ErrEmail2FAInvalid.
func (e *Email2FA) Consume(ctx context.Context, userID, code string) error {
	if code == "" || len(e.codeKey) == 0 {
		return ErrEmail2FAInvalid
	}
	key := email2FaCodeKey(userID)
	stored, err := e.rdb.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return ErrEmail2FAInvalid
		}
		return err
	}
	// Constant-time compare: the value is a MAC, and hmac.Equal exists so the
	// comparison cannot be turned into an oracle.
	if !hmac.Equal([]byte(stored), []byte(emailCodeDigest(e.codeKey, userID, code))) {
		// Mismatch — leave the stored code intact so a typo doesn't burn it.
		return ErrEmail2FAInvalid
	}
	// Match → atomically delete so the code is single-use. Under a concurrent
	// double-submit the second GetDel returns Nil and fails the compare below.
	consumed, err := e.rdb.GetDel(ctx, key).Result()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return ErrEmail2FAInvalid
		}
		return err
	}
	if consumed != stored {
		return ErrEmail2FAInvalid
	}
	return nil
}

// ArmPending grants email-code access to an ip+email pair (called when a
// correct-password login returned totp_required). The flag expires after 5 min.
func (e *Email2FA) ArmPending(ctx context.Context, ip, email string) error {
	return e.rdb.Set(ctx, email2FaPendingKey(ip, email), "1", Email2FAPendingTTL).Err()
}

// Pending reports whether the ip+email pair has a live pending flag.
func (e *Email2FA) Pending(ctx context.Context, ip, email string) (bool, error) {
	_, err := e.rdb.Get(ctx, email2FaPendingKey(ip, email)).Result()
	if err == nil {
		return true, nil
	}
	if errors.Is(err, goredis.Nil) {
		return false, nil
	}
	return false, err
}

// ConsumePending clears the pending flag (called after a code is issued).
func (e *Email2FA) ConsumePending(ctx context.Context, ip, email string) error {
	return e.rdb.Del(ctx, email2FaPendingKey(ip, email)).Err()
}

// ─── passwordless magic link (issue #73) ────────────────────────────────────
//
// A 256-bit token emailed to a verified user; only its SHA-256 hash is stored
// under `jf:magic:{hash}` → userID, 15 min TTL, single-use (atomic GetDel).
const (
	MagicLinkKeyPrefix = "jf:magic:"
	MagicLinkTTL       = 15 * time.Minute
)

func magicLinkKey(hash string) string { return MagicLinkKeyPrefix + hash }

// MagicLink exposes the magic-link store sharing the session client.
func (s *Sessions) MagicLink() *MagicLink { return &MagicLink{rdb: s.rdb} }

type MagicLink struct {
	rdb *goredis.Client
}

// Generate mints a fresh 32-byte token, stores its SHA-256 → userID and
// returns the raw token (the only copy of the usable secret).
func (m *MagicLink) Generate(ctx context.Context, userID string) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	if err := m.rdb.Set(ctx, magicLinkKey(hashOf(token)), userID, MagicLinkTTL).Err(); err != nil {
		return "", fmt.Errorf("magiclink set: %w", err)
	}
	return token, nil
}

// ErrMagicLinkInvalid is returned when the token is absent, expired, or already
// consumed.
var ErrMagicLinkInvalid = errors.New("magic link token invalid")

// GenerateForEmail mints a signup magic link bound to an email that does not
// yet have an account (issue #278 hybrid passwordless). The stored value is
// the email itself (prefixed) rather than a userID, so on click the verify
// handler can create the account. Same single-use 15-min token as login.
func (m *MagicLink) GenerateForEmail(ctx context.Context, email string) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	if err := m.rdb.Set(ctx, magicLinkKey(hashOf(token)), "email:"+strings.ToLower(email), MagicLinkTTL).Err(); err != nil {
		return "", fmt.Errorf("magiclink signup set: %w", err)
	}
	return token, nil
}

// ConsumeEmail atomically invalidates a signup magic-link token and returns
// the email it was issued to. Returns ErrMagicLinkInvalid when the token is
// absent, expired, already consumed, or was a login (userID) token.
func (m *MagicLink) ConsumeEmail(ctx context.Context, token string) (string, error) {
	if token == "" {
		return "", ErrMagicLinkInvalid
	}
	val, err := m.rdb.GetDel(ctx, magicLinkKey(hashOf(token))).Result()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return "", ErrMagicLinkInvalid
		}
		return "", err
	}
	if !strings.HasPrefix(val, "email:") {
		return "", ErrMagicLinkInvalid
	}
	email := strings.TrimPrefix(val, "email:")
	if email == "" {
		return "", ErrMagicLinkInvalid
	}
	return email, nil
}

// Consume atomically invalidates the token (GetDel) and returns the userID it
// was issued to. A single-use token can never be replayed.
func (m *MagicLink) Consume(ctx context.Context, token string) (string, error) {
	if token == "" {
		return "", ErrMagicLinkInvalid
	}
	userID, err := m.rdb.GetDel(ctx, magicLinkKey(hashOf(token))).Result()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return "", ErrMagicLinkInvalid
		}
		return "", err
	}
	if userID == "" {
		return "", ErrMagicLinkInvalid
	}
	return userID, nil
}

// MagicLinkKind discriminates a consumed magic-link token.
type MagicLinkKind int

const (
	// MagicLinkUser is a login token bound to an existing userID.
	MagicLinkUser MagicLinkKind = iota
	// MagicLinkEmail is a signup token bound to an email (issue #278).
	MagicLinkEmail
)

// ConsumeAny atomically invalidates the token (single GetDel) and reports
// whether it was a login token (kind=MagicLinkUser, value=userID) or a signup
// token (kind=MagicLinkEmail, value=email). Single-use is preserved: the token
// is consumed exactly once regardless of kind.
func (m *MagicLink) ConsumeAny(ctx context.Context, token string) (MagicLinkKind, string, error) {
	if token == "" {
		return 0, "", ErrMagicLinkInvalid
	}
	val, err := m.rdb.GetDel(ctx, magicLinkKey(hashOf(token))).Result()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return 0, "", ErrMagicLinkInvalid
		}
		return 0, "", err
	}
	if val == "" {
		return 0, "", ErrMagicLinkInvalid
	}
	if strings.HasPrefix(val, "email:") {
		email := strings.TrimPrefix(val, "email:")
		if email == "" {
			return 0, "", ErrMagicLinkInvalid
		}
		return MagicLinkEmail, email, nil
	}
	return MagicLinkUser, val, nil
}
