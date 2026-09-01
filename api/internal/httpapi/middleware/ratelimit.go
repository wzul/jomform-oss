// Simple in-memory rate limiter: token bucket per key with periodic GC.
package middleware

import (
	"net"
	"net/http"
	"sync"
	"time"
)

type bucket struct {
	tokens   float64
	lastFill time.Time
}

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
		lastGC:  time.Now(),
	}
}

// Allow consumes one token for key; false = over the limit.
func (l *RateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()

	// lazy GC every minute
	if now.Sub(l.lastGC) > time.Minute {
		for k, b := range l.buckets {
			if now.Sub(b.lastFill) > 10*time.Minute {
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

// ClientIP returns the best client IP from the request: CF-Connecting-IP when
// present (set by the Cloudflare edge, cannot be spoofed behind it), else
// X-Real-IP (client-suppliable, only used for direct/internal traffic), else
// the socket peer. Shared by rate limiting and login-failure keying.
func ClientIP(r *http.Request) string {
	ip := r.Header.Get("CF-Connecting-IP")
	if ip == "" {
		ip = r.Header.Get("X-Real-IP")
	}
	if ip == "" {
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			ip = host
		} else {
			ip = r.RemoteAddr
		}
	}
	return ip
}

// RateLimit limits per client IP: rate 1/sec, burst 10 (spam guard).
func RateLimit(next http.Handler) http.Handler {
	return RateLimitWith(1, 10)(next)
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
