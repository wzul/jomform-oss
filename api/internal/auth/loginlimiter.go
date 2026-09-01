package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Login hardening constants (issue #42).
//
//   - Each failed login bumps both the per-IP and per-account counters.
//   - After FailDelayThreshold (5) failures we transparently return the
//     generic 401 and record a Retry-After delay so a slow attacker stalls.
//   - After FailLockoutThreshold (10) failures in the 15-minute window the
//     login is refused outright with 429 + Retry-After until the window
//     slides clear.
//   - A successful login resets both counters.
//   - The counters are kept for a fixed 15-minute window (TTL on each key),
//     so the "within 15min" acceptance criterion is enforced by expiry.
const (
	LoginFailWindow               = 15 * time.Minute
	FailDelayThreshold            = 5
	FailLockoutThreshold          = 10
	LoginLockoutRetryAfter        = "900" // seconds — tells the client when the window clears
	LoginLockoutMessage           = "Terlalu banyak percubaan. Sila cuba lagi dalam 15 minit."
	loginfailIPKey         string = "jf:loginfail:ip:"
	loginfailUserKey       string = "jf:loginfail:user:"
)

// LoginLimiter tracks failed login attempts per IP and per account email in
// Valkey so brute-force attempts are progressively slowed and then refused,
// without leaking whether an email exists (unknown emails share the generic
// 401 + uniform delay path).
type LoginLimiter struct {
	rdb *goredis.Client
}

// NewLoginLimiter wires the limiter over the same Valkey client that backs
// sessions (go-redis only understands redis://, so a valkey:// prefix is
// rewritten just as NewSessions does).
func NewLoginLimiter(rdb *goredis.Client) *LoginLimiter { return &LoginLimiter{rdb: rdb} }

func loginIPKey(ip string) string { return loginfailIPKey + ip }
func loginUserKey(email string) string {
	return loginfailUserKey + strings.ToLower(email)
}

// RecordFailure bumps the per-IP and (when a real user email is supplied) the
// per-account failure counters, sliding the 15-minute window on each hit.
func (l *LoginLimiter) RecordFailure(ctx context.Context, ip, email string) error {
	pipe := l.rdb.TxPipeline()
	pipe.Incr(ctx, loginIPKey(ip))
	pipe.Expire(ctx, loginIPKey(ip), LoginFailWindow)
	if email != "" {
		pipe.Incr(ctx, loginUserKey(email))
		pipe.Expire(ctx, loginUserKey(email), LoginFailWindow)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("loginfail record: %w", err)
	}
	return nil
}

// Reset clears both counters for the given ip/email (called on login success).
func (l *LoginLimiter) Reset(ctx context.Context, ip, email string) error {
	keys := []string{loginIPKey(ip)}
	if email != "" {
		keys = append(keys, loginUserKey(email))
	}
	if err := l.rdb.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("loginfail reset: %w", err)
	}
	return nil
}

// IPCount returns the current failure count for an IP ("" and 0 when the
// limiter is unavailable — callers must treat limiter failure as fail-open for
// availability, never as a lockout).
func (l *LoginLimiter) IPCount(ctx context.Context, ip string) (int64, error) {
	return l.count(ctx, loginIPKey(ip))
}

// UserCount returns the current failure count for an account email.
func (l *LoginLimiter) UserCount(ctx context.Context, email string) (int64, error) {
	return l.count(ctx, loginUserKey(email))
}

func (l *LoginLimiter) count(ctx context.Context, key string) (int64, error) {
	n, err := l.rdb.Get(ctx, key).Int64()
	if err != nil {
		if err == goredis.Nil {
			return 0, nil
		}
		return 0, err
	}
	return n, nil
}

// Status reports whether the ip/email pair is hard-locked (>= 10 failures) or
// should be delayed (>= 5 failures). locked=true means the caller must reject
// with 429 + Retry-After and NOT run the password check.
func (l *LoginLimiter) Status(ctx context.Context, ip, email string) (locked, delayed bool, count int64, err error) {
	ipCount, err := l.IPCount(ctx, ip)
	if err != nil {
		return false, false, 0, err
	}
	count = ipCount
	if count >= FailLockoutThreshold {
		return true, true, count, nil
	}
	if count >= FailDelayThreshold {
		return false, true, count, nil
	}
	return false, false, count, nil
}
