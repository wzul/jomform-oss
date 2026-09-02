// Email backup-code (issue #72) and magic-link (issue #73) token stores.
//
// Both share the PasswordReset design: a raw secret travels to the user's
// inbox, only its SHA-256 is stored in Valkey, and it is single-use (atomic
// GetDel on consume) so a leaked key can never be replayed.
package auth

import (
	"context"
	"crypto/rand"
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
//   - stored hashed under `jf:2faemail:{userID}` (SHA-256, 10 min TTL);
//   - single-use — verified against the stored hash and then consumed via an
//     atomic GetDel so it can never be replayed.
const (
	Email2FACodeKeyPrefix = "jf:2faemail:" // + {userID}
	Email2FACodeTTL       = 10 * time.Minute

	Email2FAPendingPrefix = "jf:2fa_pending:" // + {ip}:{email}
	Email2FAPendingTTL    = 5 * time.Minute
)

func email2FaCodeKey(userID string) string { return Email2FACodeKeyPrefix + userID }
func email2FaPendingKey(ip, email string) string {
	return Email2FAPendingPrefix + ip + ":" + strings.ToLower(email)
}

// Email2FA exposes the email-backup-code store sharing the session client.
func (s *Sessions) Email2FA() *Email2FA { return &Email2FA{rdb: s.rdb} }

type Email2FA struct {
	rdb *goredis.Client
}

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
	return e.rdb.Set(ctx, email2FaCodeKey(userID), hashOf(code), Email2FACodeTTL).Err()
}

// ErrEmail2FAInvalid is returned when the supplied code does not match any
// live emailed code for the user (absent, expired, already consumed, or wrong).
var ErrEmail2FAInvalid = errors.New("email 2fa code invalid")

// Consume verifies code against the stored hash and, on match, atomically
// consumes it via GetDel so it can only ever be used once. A wrong code does
// not consume (a typo doesn't burn the valid code).
func (e *Email2FA) Consume(ctx context.Context, userID, code string) error {
	if code == "" {
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
	if stored != hashOf(code) {
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
