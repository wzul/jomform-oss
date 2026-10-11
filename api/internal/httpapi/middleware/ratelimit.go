// Simple in-memory rate limiter: token bucket per key with periodic GC.
package middleware

import (
	"net/http"
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
		return true
	}
	elapsed := now.Sub(b.lastFill).Seconds()
	b.tokens += elapsed * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.lastFill = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
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
func RateLimitWith(rate, burst float64) func(http.Handler) http.Handler {
	limiter := NewRateLimiter(rate, burst)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.Allow(ClientIP(r)) {
				w.Header().Set("Retry-After", "30")
				http.Error(w, "Terlalu banyak percubaan. Cuba lagi sebentar. (429)", http.StatusTooManyRequests)
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
