package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// okHandler always returns 200 with a marker body.
func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

// TestRegistrationLimitRejectsAfterBurst: the registration bucket is tight
// (burst 5, ~1/hour refill). More than the burst from one IP is throttled.
func TestRegistrationLimitRejectsAfterBurst(t *testing.T) {
	lim := RegistrationLimit(okHandler())
	req := httptest.NewRequest("POST", "/v1/auth/register", nil)
	req.RemoteAddr = "1.2.3.4:5678"

	// Exactly burst (5) succeed.
	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		lim.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("attempt %d: status = %d, want 200 (burst not exceeded)", i+1, rec.Code)
		}
	}
	// 6th is rejected 429.
	rec := httptest.NewRecorder()
	lim.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("6th attempt: status = %d, want 429", rec.Code)
	}
	if ra := rec.Header().Get("Retry-After"); ra == "" {
		t.Error("429 response missing Retry-After header")
	}
}

// TestRateLimitAllowsUnderBurstAndRejectsOver: the coarse per-IP auth bucket
// (burst 10, 1/sec) admits a burst then throttles.
func TestRateLimitAllowsUnderBurstAndRejectsOver(t *testing.T) {
	lim := RateLimit(okHandler())
	req := httptest.NewRequest("POST", "/v1/auth/login", nil)
	req.RemoteAddr = "5.6.7.8:9"

	for i := 0; i < 10; i++ {
		rec := httptest.NewRecorder()
		lim.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("attempt %d: status = %d, want 200", i+1, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	lim.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("11th attempt: status = %d, want 429", rec.Code)
	}
}

// TestRateLimitDistinctIPsDoNotShareBuckets: each IP has its own bucket, so
// two IPs each get the full burst.
func TestRateLimitDistinctIPsDoNotShareBuckets(t *testing.T) {
	lim := RateLimit(okHandler())
	a := httptest.NewRequest("POST", "/", nil)
	a.RemoteAddr = "10.0.0.1:1"
	b := httptest.NewRequest("POST", "/", nil)
	b.RemoteAddr = "10.0.0.2:1"

	for i := 0; i < 10; i++ {
		for _, req := range []*http.Request{a, b} {
			rec := httptest.NewRecorder()
			lim.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("IP %s burst %d: status = %d", req.RemoteAddr, i+1, rec.Code)
			}
		}
	}
}

// TestClientIP now lives in clientip_test.go: the resolution rule (and why a
// caller cannot choose its own key) is the subject of card t_ac16d273.

// ---------------------------------------------------------------------------
// Idle-gap behaviour of the token bucket (card t_19d8fd3b)
//
// The sweep in Allow used to drop any bucket untouched for 10 minutes and
// re-create it at FULL burst, so the sustained rate of every RateLimitWith route
// was really `burst` requests per ~10 minutes instead of the documented rate
// (measured on prod: 4 magic-link admissions in ONE bucket inside 11 minutes,
// where the 1/hour guard left only ~0.18 tokens). These tests age a bucket past
// the window through the middleware.timeNow seam (clock.go) and assert the
// guard still bites.
// ---------------------------------------------------------------------------

// fakeClock swaps middleware.timeNow for the duration of one test, so a bucket
// can be aged past the idle/GC window without sleeping. Tests in this package do
// not run parallel, so the package-level clock is safe here.
type fakeClock struct{ t time.Time }

func newFakeClock(t *testing.T, start time.Time) *fakeClock {
	t.Helper()
	c := &fakeClock{t: start}
	old := timeNow
	timeNow = func() time.Time { return c.t }
	t.Cleanup(func() { timeNow = old })
	return c
}

func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// TestMagicLinkBucketDoesNotResetAfterIdleGap walks the measured prod shape:
// the magic-link route (auth.go:100) is burst 2, 1/hour. Two requests spend the
// burst; after the 643 s idle gap the documented semantics leave
// 643 * (1/3600) ≈ 0.18 tokens, so the next request — and the one after it —
// must be 429. Before the fix the bucket was evicted at the 10-minute mark and
// re-created full, which is exactly the 200/200 prod measured at 14:46:31.
func TestMagicLinkBucketDoesNotResetAfterIdleGap(t *testing.T) {
	clk := newFakeClock(t, time.Date(2026, 10, 11, 14, 35, 30, 0, time.UTC))
	lim := RateLimitWith(1.0/3600.0, 2)(okHandler())
	req := httptest.NewRequest("POST", "/v1/auth/magic-link/request", nil)
	req.RemoteAddr = "203.0.113.7:443"

	post := func() int {
		rec := httptest.NewRecorder()
		lim.ServeHTTP(rec, req)
		return rec.Code
	}

	if got := post(); got != http.StatusOK { // 14:35:30
		t.Fatalf("1st request: status = %d, want 200 (burst 2 not spent)", got)
	}
	if got := post(); got != http.StatusOK { // 14:35:48
		t.Fatalf("2nd request: status = %d, want 200 (burst 2 not spent)", got)
	}
	if got := post(); got != http.StatusTooManyRequests {
		t.Fatalf("3rd request with the burst spent: status = %d, want 429", got)
	}

	clk.advance(643 * time.Second) // 14:46:31, the measured idle gap
	if got := post(); got != http.StatusTooManyRequests {
		t.Errorf("after a 643 s idle gap: status = %d, want 429 — tokens at that instant are 0 + 643/3600 ≈ 0.18, so the 1/hour guard must bite; a 200 means the bucket was evicted and re-created at full burst", got)
	}
	if got := post(); got != http.StatusTooManyRequests {
		t.Errorf("2nd request after the idle gap: status = %d, want 429 (still ≈0.18 tokens, far below 1)", got)
	}
}

// TestRegistrationLimitSustainsOnePerHourAfterBurst pins the intended sustained
// rate of RegistrationLimit (burst 5, then 1/hour), including across idle gaps
// long enough to trip the sweep.
func TestRegistrationLimitSustainsOnePerHourAfterBurst(t *testing.T) {
	clk := newFakeClock(t, time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC))
	lim := RegistrationLimit(okHandler())
	req := httptest.NewRequest("POST", "/v1/auth/register", nil)
	req.RemoteAddr = "198.51.100.9:51234"

	post := func() int {
		rec := httptest.NewRecorder()
		lim.ServeHTTP(rec, req)
		return rec.Code
	}

	for i := 0; i < 5; i++ {
		if got := post(); got != http.StatusOK {
			t.Fatalf("burst request %d: status = %d, want 200", i+1, got)
		}
	}
	if got := post(); got != http.StatusTooManyRequests {
		t.Fatalf("6th request with the burst spent: status = %d, want 429", got)
	}

	// 10.5 min idle (over the sweep window): ~0.175 tokens, so still 429.
	clk.advance(10*time.Minute + 30*time.Second)
	if got := post(); got != http.StatusTooManyRequests {
		t.Errorf("request after a 10.5 min idle gap: status = %d, want 429 (≈0.175 tokens refilled)", got)
	}

	// A second past the hour: 1 token refilled → exactly one more is admitted,
	// and the next one immediately after is refused again.
	clk.advance(time.Hour - 10*time.Minute - 30*time.Second + time.Second)
	if got := post(); got != http.StatusOK {
		t.Errorf("request at burst+1h (+1s): status = %d, want 200 (1/hour refill)", got)
	}
	if got := post(); got != http.StatusTooManyRequests {
		t.Errorf("second request at burst+1h: status = %d, want 429 (the hourly token was just spent)", got)
	}

	// One more hour, one more admission.
	clk.advance(time.Hour)
	if got := post(); got != http.StatusOK {
		t.Errorf("request at burst+2h: status = %d, want 200 (1/hour refill)", got)
	}
	if got := post(); got != http.StatusTooManyRequests {
		t.Errorf("second request at burst+2h: status = %d, want 429", got)
	}
}

// TestRateLimiterKeepsLiveBucketsAndStillEvictsRefilledOnes is the memory-bound
// half: the sweep must keep a bucket that still carries unrefilled state (or the
// client gets a fresh full burst) and must still drop one that has refilled to
// capacity (it is then indistinguishable from a new bucket, so the map cannot
// grow without bound).
func TestRateLimiterKeepsLiveBucketsAndStillEvictsRefilledOnes(t *testing.T) {
	clk := newFakeClock(t, time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC))
	l := NewRateLimiter(1.0/3600.0, 5) // registration-shaped bucket

	for i := 0; i < 5; i++ {
		if !l.Allow("A") {
			t.Fatalf("burst request %d for A refused", i+1)
		}
	}
	if l.Allow("A") {
		t.Fatal("6th request for A admitted with the burst spent")
	}

	// Idle past the sweep window, but A is far from refilled: live state.
	clk.advance(643 * time.Second)
	l.Allow("B") // any call may run the sweep
	if _, ok := l.buckets["A"]; !ok {
		t.Error("A was evicted after 643 s idle with only ≈0.18 of 5 tokens refilled: the sweep must keep any bucket whose tokens have not refilled to capacity, otherwise the client gets a fresh full burst (card t_19d8fd3b)")
	}

	// Long enough for A to be back at capacity: dropping it changes no decision,
	// so the memory bound must still hold.
	clk.advance(5 * time.Hour)
	l.Allow("B")
	if _, ok := l.buckets["A"]; ok {
		t.Error("A is still in the map after 5 h of idle (tokens back at capacity): fully-refilled buckets must be evicted or the map grows without bound")
	}
	if _, ok := l.buckets["B"]; !ok {
		t.Error("B (the key that just made a request) is missing from the map")
	}
}
