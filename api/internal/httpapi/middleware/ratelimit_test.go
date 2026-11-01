package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
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

// ---------------------------------------------------------------------------
// Retry-After tells the truth about the bucket that refused (card t_5bed7cab)
//
// The header used to be the constant "30" for every RateLimitWith caller, while
// the buckets behind it refill at 1/s (RateLimit), 1/hour (magic-link) and
// 3/hour (the emailed 2FA code): true for none of them. A value that is too
// SHORT sends the client straight back into a refusal, and a value that is too
// LONG throttles it for nothing — and the number is user-facing, because /login
// renders it verbatim as "Too many attempts — try again in N seconds"
// (web/src/lib/rateLimitErr.ts). These tests read the header off a real 429 and
// then SPEND the window it claims.
// ---------------------------------------------------------------------------

// post serves one request through lim and returns the recorder.
func post(lim http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	lim.ServeHTTP(rec, req)
	return rec
}

// retryAfterSeconds reads a 429's Retry-After as whole seconds, failing on a
// missing or unparseable header (the shape a client cannot obey).
func retryAfterSeconds(t *testing.T, rec *httptest.ResponseRecorder) int {
	t.Helper()
	raw := rec.Header().Get("Retry-After")
	if raw == "" {
		t.Fatalf("429 carries no Retry-After (body %q)", rec.Body.String())
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		t.Fatalf("Retry-After = %q, want a whole number of seconds >= 1", raw)
	}
	return n
}

// postUntilRefusal posts until the bucket refuses and returns that answer's
// Retry-After in seconds.
func postUntilRefusal(t *testing.T, lim http.Handler, req *http.Request) int {
	t.Helper()
	for i := 0; i < 500; i++ {
		if rec := post(lim, req); rec.Code == http.StatusTooManyRequests {
			return retryAfterSeconds(t, rec)
		}
	}
	t.Fatalf("the bucket never refused after 500 requests")
	return 0
}

// TestRateLimitRetryAfterIsTheRealWaitOnTheLoginBucket is the measured prod
// shape from the card: the real /login limiter (RateLimit, burst 10, 1/s),
// drained, and the number a visitor would be shown then spent in wall-clock
// time. Before the fix the header said 30 while the bucket was empty for about
// one second, and /login's passkey button printed "try again in 30 seconds".
func TestRateLimitRetryAfterIsTheRealWaitOnTheLoginBucket(t *testing.T) {
	lim := RateLimit(okHandler())
	req := httptest.NewRequest("POST", "/v1/auth/login", nil)
	req.RemoteAddr = "203.0.113.44:41234"

	secs := postUntilRefusal(t, lim, req)
	if secs > 2 {
		t.Fatalf("Retry-After = %ds on a bucket that refills %g token(s)/s: the bucket is empty for about %gs, so the header overstates the wait (this was the constant 30)",
			secs, float64(RateLimitRate), 1/float64(RateLimitRate))
	}
	// n-1 seconds: n is 1 for this bucket, so n-1 is "right now" — the retry must
	// still be REFUSED, i.e. the header is not overstated.
	if rec := post(lim, req); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("retry %ds before the promised window: status = %d, want 429 (the header must not be overstated)", secs-1, rec.Code)
	}
	// The promise, spent. time.Sleep never wakes early, so the bucket holds at
	// least the one token the request costs at this instant.
	time.Sleep(time.Duration(secs) * time.Second)
	if rec := post(lim, req); rec.Code != http.StatusOK {
		t.Fatalf("retry after exactly the %ds the header promised: status = %d, want 200 — the header promised a window the limiter does not honour", secs, rec.Code)
	}
}

// TestRetryAfterIsReadFromTheBucketNotFixedAtThirtySeconds pins the derivation
// on a bucket where n-1 and n are a whole second apart, and where the answer
// must SHRINK as the window is spent (a constant cannot do that).
func TestRetryAfterIsReadFromTheBucketNotFixedAtThirtySeconds(t *testing.T) {
	clk := newFakeClock(t, time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC))
	// burst 2, one token per 30 s: the "30 seconds" window the constant was
	// presumably chosen for, applied to a bucket that really is that slow.
	lim := RateLimitWith(1.0/30.0, 2)(okHandler())
	req := httptest.NewRequest("POST", "/v1/rest/uploads/image", nil)
	req.RemoteAddr = "198.51.100.77:5000"

	for i := 0; i < 2; i++ {
		if rec := post(lim, req); rec.Code != http.StatusOK {
			t.Fatalf("burst request %d: status = %d, want 200", i+1, rec.Code)
		}
	}
	rec := post(lim, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request with the burst spent: status = %d, want 429", rec.Code)
	}
	if secs := retryAfterSeconds(t, rec); secs != 30 {
		t.Fatalf("Retry-After = %ds, want 30 (the bucket needs (1-0)/(1/30) = 30 s to hold one token)", secs)
	}

	// n-1: one second before the promise is spent the bucket holds 29/30 of a
	// token, so the retry must be refused.
	clk.advance(29 * time.Second)
	rec = post(lim, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("retry %ds before the promised window: status = %d, want 429 — a header that lets the client back in early is understated", 30-1, rec.Code)
	}
	// ...and this answer names the one second that is actually left, which is the
	// proof the number comes from the deficit rather than from a constant.
	if secs := retryAfterSeconds(t, rec); secs != 1 {
		t.Errorf("Retry-After at 29 s into a 30 s window = %ds, want 1 — the header must shrink with the deficit", secs)
	}

	// n seconds after the refusal: admitted.
	clk.advance(1 * time.Second)
	if rec := post(lim, req); rec.Code != http.StatusOK {
		t.Fatalf("retry after exactly the 30 s the header promised: status = %d, want 200", rec.Code)
	}
}

// TestRetryAfterOnAnHourlyBucketNamesTheHour is the other half of the same
// defect: RegistrationLimit refills one token per hour (burst 5), so the
// constant's "30" UNDERSTATED the wait ~120x and walked the caller back into a
// refusal. The header now names the bucket's own hour — no route keeps a number
// that was chosen for a different route.
func TestRetryAfterOnAnHourlyBucketNamesTheHour(t *testing.T) {
	newFakeClock(t, time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC))
	lim := RegistrationLimit(okHandler())
	req := httptest.NewRequest("POST", "/v1/auth/register", nil)
	req.RemoteAddr = "198.51.100.9:51234"

	for i := 0; i < 5; i++ {
		if rec := post(lim, req); rec.Code != http.StatusOK {
			t.Fatalf("burst request %d: status = %d, want 200", i+1, rec.Code)
		}
	}
	rec := post(lim, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("6th request with the burst spent: status = %d, want 429", rec.Code)
	}
	if secs := retryAfterSeconds(t, rec); secs != 3600 {
		t.Errorf("Retry-After = %ds on a 1/hour bucket, want 3600", secs)
	}
}

// TestWaitForTokenRoundsUpAndFloors pins the arithmetic under the header: round
// UP (never understate the wait), floor at one second (a header of 0 tells a
// client to retry immediately, into the same refusal), and answer a parseable
// window for a bucket that can never refill.
func TestWaitForTokenRoundsUpAndFloors(t *testing.T) {
	cases := []struct {
		tokens, rate float64
		want         time.Duration
	}{
		{0, 1, time.Second},                       // the drained /login bucket: one second, not thirty
		{0.4, 1, time.Second},                     // ceil(0.6)
		{0, 1.0 / 30.0, 30 * time.Second},         // a 30 s window
		{0, 1.0 / 3600.0, 3600 * time.Second},     // the hourly buckets
		{0.9999999999, 1.0 / 3600.0, time.Second}, // floored, never 0
		{0, 0, retryAfterNever},                   // a bucket that never refills
	}
	for _, tc := range cases {
		l := &RateLimiter{rate: tc.rate}
		if got := l.waitForToken(tc.tokens); got != tc.want {
			t.Errorf("waitForToken(tokens=%g, rate=%g) = %v, want %v", tc.tokens, tc.rate, got, tc.want)
		}
	}
}
