package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wzul/jomform/api/internal/config"
)

func newTestServer() *Server {
	return NewServer(&config.Config{
		Port:        "0",
		DatabaseURL: "postgres://localhost/jomform_test",
		AppBaseURL:  "http://localhost:8080",
		Env:         "dev",
	}, testLogger(), nil)
}

func TestHealthz(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("content-type = %q", got)
	}
	if body := rec.Body.String(); len(body) == 0 || !containsAll(body, `"status"`, `"ok"`) {
		t.Errorf("body = %q", body)
	}
}

func TestVersion(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest("GET", "/version", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !containsAll(rec.Body.String(), `"version"`) {
		t.Errorf("body missing version field: %q", rec.Body.String())
	}
}

func TestReadyzDegradedWhenDepsMissing(t *testing.T) {
	srv := newTestServer() // deps == nil: database & valkey not wired
	req := httptest.NewRequest("GET", "/readyz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	// Without wired deps the service is honest: 503 + degraded.
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 when deps missing", rec.Code)
	}
	body := rec.Body.String()
	if !containsAll(body, `"database"`, `"valkey"`, `"degraded"`) {
		t.Errorf("body missing expected fields: %s", body)
	}
}

func TestRequestIDHeaderSet(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Header().Get("X-Request-ID") == "" {
		t.Fatal("X-Request-ID header not set")
	}
}

func TestPanicRecovered(t *testing.T) {
	// A handler that panics must come back as 500, not crash the server.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /boom", func(w http.ResponseWriter, _ *http.Request) { panic("boom") })
	srv := newTestServer()

	// Wrap through the same pipeline by calling recoverer indirectly: instead
	// register panicking route via Handler? Handler is fixed; test recoverer
	// directly with a panic handler.
	h := recoverer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { panic("boom") }))
	req := httptest.NewRequest("GET", "/boom", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if srv == nil {
		t.Fatal("server construction should not fail")
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !stringContains(s, sub) {
			return false
		}
	}
	return true
}

func stringContains(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
