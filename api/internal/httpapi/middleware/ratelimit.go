// Simple in-memory rate limiter: token bucket per key with periodic GC.
package middleware

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type bucket struct {
	tokens   float64
	lastFill time.Time
}

// Sweep cadence and idle threshold for RateLimiter's lazy GC.
//
// A bucket is only evicted once it is BOTH idle for gcIdle AND refilled to
// capacity: at that point it is indistinguishable from a bucket that was never
// created, so dropping it cannot change any decision. Evicting a bucket that
// still holds unrefilled tokens would hand its client a fresh full burst —
// that was card t_19d8fd3b (every RateLimitWith route really admitted `burst`
// per ~10 min idle, e.g. 4 magic-link admissions in one bucket inside 11 min
// where the 1/hour guard left ~0.18 tokens).
//
// Consequence for the memory bound: an entry now lives until it refills, i.e. at
// most burst/rate seconds after it was last touched (10 s for RateLimit's 1/s
// bucket, ~2 h for the magic-link bucket, 5 h for registration's), instead of a
// flat 10 minutes. That is the price of not forgetting live token state, and it
// is bounded — the map holds only the keys that are still being throttled.
const (
	gcEvery = time.Minute
	gcIdle  = 10 * time.Minute
)

// RateLimiter limits each key to `rate` refills per second, capacity `burst`.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64
	burst   float64
	lastGC  time.Time
}

// NewRateLimiter builds a limiter (rate tokens/sec, burst max tokens).
func NewRateLimiter(rate, burst float64) *RateLimiter {
	return &RateLimiter{
		buckets: map[string]*bucket{},
		rate:    rate,
		burst:   burst,
		lastGC:  timeNow(),
	}
}

// Allow consumes one token for key; false = over the limit.
func (l *RateLimiter) Allow(key string) bool {
	ok, _ := l.AllowRetry(key)
	return ok
}

// AllowRetry consumes one token for key. A refusal also reports how long THAT
// caller must wait before the bucket holds a token for it again.
//
// The wait is read from the bucket's own deficit, never from a constant: one
// number cannot be true for the buckets RateLimitWith is handed (1/s here,
// 1/hour on magic-link, 3/hour on the emailed 2FA code). A constant is what
// card t_5bed7cab removed — the middleware answered a flat "30" while the
// drained /login bucket refilled in about one second, so the sentence the
// dashboard renders from that header (lib/rateLimitErr.ts) overstated the wait
// ~30x; on the hourly buckets the same constant UNDERstated it ~120x, which
// sends the caller straight back into a refusal.
func (l *RateLimiter) AllowRetry(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := timeNow()

	// Lazy sweep every gcEvery. Only buckets that have refilled to capacity are
	// dropped: see the gcEvery/gcIdle comment for why discarding unrefilled
	// tokens is a defect, not housekeeping.
	if now.Sub(l.lastGC) > gcEvery {
		for k, b := range l.buckets {
			idle := now.Sub(b.lastFill)
			if idle <= gcIdle {
				continue
			}
			// b.tokens is the count as of b.lastFill; project the refill to now
			// before deciding (the same arithmetic the consume path below applies).
			if b.tokens+idle.Seconds()*l.rate >= l.burst-1e-9 {
				delete(l.buckets, k)
			}
		}
		l.lastGC = now
	}

	b, ok := l.buckets[key]
	if !ok {
		l.buckets[key] = &bucket{tokens: l.burst - 1, lastFill: now}
		return true, 0
	}
	elapsed := now.Sub(b.lastFill).Seconds()
	b.tokens += elapsed * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.lastFill = now
	if b.tokens < 1 {
		return false, l.waitForToken(b.tokens)
	}
	b.tokens--
	return true, 0
}

// retryAfterNever is what a bucket that never refills (rate <= 0) answers: a
// day, the largest window a Retry-After can sensibly carry. No production call
// site passes a non-positive rate; this exists so a misconfiguration answers a
// parseable number instead of +Inf/NaN.
const retryAfterNever = 24 * time.Hour

// waitForToken is how long a bucket currently holding `tokens` (0 <= tokens < 1)
// needs to reach the one token a request costs.
//
// ROUNDED UP, never down. Retry-After is a promise the client acts on, so a
// value one second short lands the retry back inside the refusal. The 1e-9 nudge
// keeps exact cases exact in float64 — 1/(1.0/3600.0) is 3599.9999999999995, and
// a legitimate 3600.0000000001 must not be rounded up to 3601 for an hour-long
// bucket. A deficit below the nudge (a caller refused a hair before refill)
// yields 0 and is floored to the one-second minimum.
func (l *RateLimiter) waitForToken(tokens float64) time.Duration {
	if l.rate <= 0 {
		return retryAfterNever
	}
	secs := math.Ceil((1-tokens)/l.rate - 1e-9)
	if secs < 1 {
		secs = 1
	}
	return time.Duration(secs) * time.Second
}

// RateLimitBurst is the per-IP CAPACITY of RateLimit's bucket. Exported so a test
// that must DRAIN a bucket before measuring throttle behaviour does not duplicate
// the number (card t_80c62c1c); see RateLimit for what the pair means.
const RateLimitBurst = 10

// RateLimitRate is RateLimit's refill, in tokens per second.
const RateLimitRate = 1

// RateLimit limits per client IP (middleware.ClientIP — see clientip.go for how
// that address is resolved and why a caller cannot choose it): rate 1/sec,
// burst 10 (spam guard).
func RateLimit(next http.Handler) http.Handler {
	return RateLimitWith(RateLimitRate, RateLimitBurst)(next)
}

// RateLimitWith limits each client IP with the given token-bucket parameters
// (rate tokens/sec, burst max tokens). 429 + Retry-After when over the limit.
//
// Retry-After is DERIVED from the refusing client's own bucket (card
// t_5bed7cab) and is therefore different per route: `ceil((1 - tokens) / rate)`
// seconds, minimum 1, i.e. the time that bucket needs to hold one more token.
// It was the constant "30", which was true for no call site — RateLimit refills
// a token per second, so a drained client was re-admitted in about one while
// the header promised thirty (measured on prod for the /login passkey button,
// which renders the server's number verbatim through lib/rateLimitErr.ts), and
// the hourly buckets had the opposite error. A client that obeys a value that
// is too SHORT is refused again immediately; one that obeys a value that is too
// LONG is throttled for no reason.
//
// The 1/s rate is deliberately NOT relaxed to make "30" true: RateLimit also
// wraps the public order/unlock routes, where "burst 10 then 2/minute" would
// throttle a merchant's customers on a shared connection during a launch.
func RateLimitWith(rate, burst float64) func(http.Handler) http.Handler {
	limiter := NewRateLimiter(rate, burst)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			allowed, retryAfter := limiter.AllowRetry(ClientIP(r))
			if !allowed {
				w.Header().Set("Retry-After", strconv.FormatInt(int64(retryAfter/time.Second), 10))
				http.Error(w, "Too many attempts. Try again shortly. (429)", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RegistrationLimit is a deliberately tight per-IP guard for the open
// registration endpoint: burst 5 with a 1/hour refill — an attacker can create
// at most a handful of accounts per hour from one IP before being throttled,
// while a normal user never notices.
func RegistrationLimit(next http.Handler) http.Handler {
	// 1 token/hour, burst 5: ~5 regs/IP instantly, then 1/hour.
	return RateLimitWith(1.0/3600.0, 5)(next)
}
