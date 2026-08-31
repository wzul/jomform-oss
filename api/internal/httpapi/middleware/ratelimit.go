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

// RateLimit limits per client IP: rate 1/sec, burst 10 (spam guard).
func RateLimit(next http.Handler) http.Handler {
	limiter := NewRateLimiter(1, 10)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.Header.Get("X-Real-IP")
		if ip == "" {
			ip = r.Header.Get("CF-Connecting-IP")
		}
		if ip == "" {
			ip = r.RemoteAddr
		}
		if !limiter.Allow(ip) {
			w.Header().Set("Retry-After", "30")
			http.Error(w, "Terlalu banyak percubaan. Cuba lagi sebentar. (429)", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
