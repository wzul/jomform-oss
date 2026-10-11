package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
