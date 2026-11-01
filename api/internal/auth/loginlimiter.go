package auth

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Login hardening constants (issue #42).
//
//   - Each failed login bumps ONE counter, keyed on the source ADDRESS: the
//     lockout is per IP, and that is the only key this package reads.
//   - From FailDelayThreshold (5) failures Status reports delayed=true, and the
//     login gate makes that attempt wait LoginFailDelay BEFORE the password
//     check (it does not sleep in failLogin, so the attempt that crosses the
//     threshold is not the one that pays). The response is unchanged — same
//     uniform 401, no Retry-After — so the penalty is invisible in the
//     bytes and cannot become an enumeration oracle. The 5..9 failure band
//     belongs to this in-handler delay: the coarse per-IP token bucket in
//     front of the route (middleware.RateLimit, 1 req/s, burst 10) only
//     answers 429 + a Retry-After derived from that bucket to a client that
//     exceeds it, and a
//     sequential client paced by this delay stays under that bucket, so it
//     reaches the address lockout below instead of a contradictory backoff.
//   - After FailLockoutThreshold (10) failures in the 15-minute window the
//     login is refused outright with 429 + Retry-After until the window
//     slides clear.
//   - A successful login resets the counter.
//   - The counter is kept for a fixed 15-minute window (TTL on the key), so the
//     "within 15min" acceptance criterion is enforced by expiry.
//
// There is deliberately NO per-account counter. One existed until it was removed
// as dead code: it was written under `jf:loginfail:user:<email>` and read by
// nothing, so it bounded nothing while these constants advertised a per-account
// protection that did not exist. Enforcing one is a change of lockout SEMANTICS
// and not a bug fix — the account-keyed counter is only ever written for an
// email that EXISTS (unknown emails deliberately share the per-IP counter), so a
// 429 keyed on it answers "does this account exist?" to an attacker who spends
// one attempt per address, and issue #42 requires the auth responses to stay
// uniform. It would also hand anyone a way to lock a known user out of their own
// account for the window. So attempts against one account spread over many
// addresses are bounded by the per-IP window alone; that is a stated trade, not
// an oversight.
const (
	LoginFailWindow        = 15 * time.Minute
	FailDelayThreshold     = 5
	FailLockoutThreshold   = 10
	LoginLockoutRetryAfter = "900" // seconds — tells the client when the window clears
	LoginLockoutMessage    = "Terlalu banyak percubaan. Sila cuba lagi dalam 15 minit."

	// LoginFailDelay is the wall-clock penalty the login gate spends before the
	// password check on every attempt that arrives while the source address
	// already holds FailDelayThreshold failures. It is FLAT, not a doubling
	// ladder: the band it covers is only five attempts wide (the tenth failure
	// locks the address out), so a ladder would multiply connection-holding
	// time for a legitimate user who mistyped without buying a proportional
	// bound. What the delay does buy: the 5..9 band costs ~0.5s of wall clock
	// per attempt instead of nothing, and (see the constants comment) it paces
	// a sequential client under the coarse 1 req/s token bucket, so the client
	// is answered by the address lockout (429 + 900) rather than by the coarse
	// layer's 429 + 30.
	LoginFailDelay = 500 * time.Millisecond

	loginfailIPKey string = "jf:loginfail:ip:"
)

// LoginLimiter tracks failed login attempts per source address in Valkey, which
// is what lets the login gate refuse a brute-forcer outright once the window's
// failures cross FailLockoutThreshold. Nothing here keys on the email: a known
// and an unknown email take the same code path and the same 401 (see the
// constants above).
type LoginLimiter struct {
	rdb *goredis.Client
}

// NewLoginLimiter wires the limiter over the same Valkey client that backs
// sessions (go-redis only understands redis://, so a valkey:// prefix is
// rewritten just as NewSessions does).
func NewLoginLimiter(rdb *goredis.Client) *LoginLimiter { return &LoginLimiter{rdb: rdb} }

func loginIPKey(ip string) string { return loginfailIPKey + ip }

// RecordFailure bumps the per-IP failure counter, sliding the 15-minute window
// on each hit.
func (l *LoginLimiter) RecordFailure(ctx context.Context, ip string) error {
	pipe := l.rdb.TxPipeline()
	pipe.Incr(ctx, loginIPKey(ip))
	pipe.Expire(ctx, loginIPKey(ip), LoginFailWindow)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("loginfail record: %w", err)
	}
	return nil
}

// Reset clears the IP's counter (called on login success).
func (l *LoginLimiter) Reset(ctx context.Context, ip string) error {
	if err := l.rdb.Del(ctx, loginIPKey(ip)).Err(); err != nil {
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

// Status reports whether the caller's source address is hard-locked (>= 10
// failures) or should be delayed (>= 5 failures). locked=true means the caller
// must reject with 429 + Retry-After and NOT run the password check.
// delayed=true means the caller must spend LoginFailDelay before the password
// check and then answer exactly as it would have otherwise (the uniform 401) —
// it is a pacing signal, not a refusal, and it carries no header of its own.
//
// It is keyed on the ADDRESS alone: there is no per-account counter to consult
// (see the constants above for why). count is that address's failure count.
func (l *LoginLimiter) Status(ctx context.Context, ip string) (locked, delayed bool, count int64, err error) {
	count, err = l.IPCount(ctx, ip)
	if err != nil {
		return false, false, 0, err
	}
	if count >= FailLockoutThreshold {
		return true, true, count, nil
	}
	if count >= FailDelayThreshold {
		return false, true, count, nil
	}
	return false, false, count, nil
}
