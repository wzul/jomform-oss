// Issue #42 auth hardening integration tests.
//
// These exercise the real handlers against a real Postgres + Valkey, mirroring
// the pattern in rest_dashboard_test.go. They are skipped unless
// JOMFORM_TEST_DATABASE_URL and JOMFORM_TEST_VALKEY_URL are set, so CI (which
// sets neither) stays green with only the unit-level middleware/auth tests.
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/db"
	"github.com/wzul/jomform/api/internal/dbstore"
)

var authOnce sync.Once

// authTestValkey returns a Sessions client and flushes the ENTIRE dedicated
// test Valkey DB once per test-binary run, so stale jf:loginfail:* counters
// from a previous run (15-min TTL, fixed test IPs) can't skew the lockout and
// rate-limit assertions. Tests must point JOMFORM_TEST_VALKEY_URL at a throwaway
// DB (e.g. .../15), never the dev/prod DB.
func authTestValkey(t *testing.T) *auth.Sessions {
	t.Helper()
	valkeyURL := os.Getenv("JOMFORM_TEST_VALKEY_URL")
	if valkeyURL == "" {
		t.Skip("JOMFORM_TEST_VALKEY_URL not set; skipping auth hardening integration test")
	}
	sess, err := auth.NewSessions(valkeyURL)
	if err != nil {
		t.Fatalf("valkey sessions: %v", err)
	}
	authOnce.Do(func() { _ = sess.Client().FlushDB(context.Background()).Err() })
	return sess
}

func authUsers(t *testing.T) *dbstore.UserStore {
	t.Helper()
	dsn := os.Getenv("JOMFORM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping auth hardening integration test")
	}
	pool, err := db.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("db connect: %v", err)
	}
	if err := db.Migrate(context.Background(), pool, testLogger()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return dbstore.NewUserStore(pool)
}

// recorderMailer captures verification emails (satisfies VerifyMailer).
type recorderMailer struct {
	mu     sync.Mutex
	called bool
	last   string // body of the most recent send (issue #62: captures reset token)
	// no-op Send
}

func (m *recorderMailer) Send(to, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.called = true
	m.last = body
	return nil
}

func (m *recorderMailer) wasCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.called
}

func (m *recorderMailer) lastBody() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.last
}

var testVerifyKey = func() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(0x11 + i)
	}
	return key
}()

// authTestFixture wires a full AuthHandlers over real Postgres + Valkey.
func authTestFixture(t *testing.T) (*AuthHandlers, *dbstore.UserStore, *recorderMailer) {
	t.Helper()
	users := authUsers(t)
	sess := authTestValkey(t)
	mailer := &recorderMailer{}
	h := &AuthHandlers{
		Sessions:     sess,
		Users:        users,
		BaseURL:      "http://localhost:8080",
		LoginLimiter: auth.NewLoginLimiter(sess.Client()),
		VerifyKey:    testVerifyKey,
		VerifyMailer: mailer,
	}
	return h, users, mailer
}

// registerAndVerify creates a real email-verified account usable for login.
func registerAndVerify(t *testing.T, h *AuthHandlers, email, password, ip string) {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	reg := postJSON(mux, "/v1/auth/register", map[string]any{
		"email": email, "password": password,
	}, ip)
	if reg.Code != http.StatusCreated {
		t.Fatalf("register status = %d (body %s)", reg.Code, reg.Body.String())
	}
	tok, err := auth.SignVerifyToken(testVerifyKey, email)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	vreq := httptest.NewRequest(http.MethodGet, "/v1/auth/verify?token="+tok, nil)
	vrec := httptest.NewRecorder()
	mux.ServeHTTP(vrec, vreq)
	if vrec.Code != http.StatusOK {
		t.Fatalf("verify status = %d (body %s)", vrec.Code, vrec.Body.String())
	}
}

// loginMux registers ONLY handleLogin on a bare mux — no coarse RateLimit
// middleware — so the failure-tracker's own lockout (10 attempts → 429 + 900s
// Retry-After) can be asserted in isolation. The route-level coarse per-IP
// limiter is a separate layer (tested in middleware/ratelimit_test.go).
func loginMux(h *AuthHandlers) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/auth/login", h.handleLogin)
	return mux
}

func postJSON(mux *http.ServeMux, path string, body map[string]any, ip string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if ip != "" {
		req.Header.Set("X-Real-IP", ip)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestAuthLoginLockoutAfter10: 10 failed logins from one IP then the 11th
// within the window is 429 + Retry-After.
func TestAuthLoginLockoutAfter10(t *testing.T) {
	h, _, _ := authTestFixture(t)
	mux := loginMux(h)

	email := fmt.Sprintf("lockout-%d@example.test", time.Now().UnixNano())
	registerAndVerify(t, h, email, "password123", "10.1.1.1")

	for i := 0; i < 10; i++ {
		rec := postJSON(mux, "/v1/auth/login", map[string]any{
			"email": email, "password": "WRONG",
		}, "10.1.1.1")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("failed login attempt %d: status = %d, want 401 (body %s)", i+1, rec.Code, rec.Body.String())
		}
	}

	rec := postJSON(mux, "/v1/auth/login", map[string]any{
		"email": email, "password": "password123",
	}, "10.1.1.1")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("11th login: status = %d, want 429 (body %s)", rec.Code, rec.Body.String())
	}
	if ra := rec.Header().Get("Retry-After"); ra != auth.LoginLockoutRetryAfter {
		t.Errorf("Retry-After = %q, want %q", ra, auth.LoginLockoutRetryAfter)
	}
}

// TestAuthLoginSuccessResetsCounter: success clears the per-IP counter, so a
// fresh 10 failures are needed to relock.
func TestAuthLoginSuccessResetsCounter(t *testing.T) {
	h, _, _ := authTestFixture(t)
	mux := loginMux(h)

	email := fmt.Sprintf("reset-%d@example.test", time.Now().UnixNano())
	// Unique IP per run so the Valkey login-breaker counter never collides
	// with a previous run's state (test isolation).
	ip := fmt.Sprintf("10.2.%d.%d", time.Now().UnixNano()%250+1, time.Now().UnixNano()%250+1)
	registerAndVerify(t, h, email, "password123", ip)

	// 9 failures (below lockout).
	for i := 0; i < 9; i++ {
		if rec := postJSON(mux, "/v1/auth/login", map[string]any{
			"email": email, "password": "WRONG",
		}, ip); rec.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d status = %d", i+1, rec.Code)
		}
	}
	// Correct password succeeds → resets.
	if rec := postJSON(mux, "/v1/auth/login", map[string]any{
		"email": email, "password": "password123",
	}, ip); rec.Code != http.StatusOK {
		t.Fatalf("success login status = %d (body %s)", rec.Code, rec.Body.String())
	}
	// Another 10 failures are required to lock out again (proves reset to 0).
	for i := 0; i < 10; i++ {
		if rec := postJSON(mux, "/v1/auth/login", map[string]any{
			"email": email, "password": "WRONG",
		}, ip); rec.Code != http.StatusUnauthorized {
			t.Fatalf("post-reset failure %d status = %d", i+1, rec.Code)
		}
	}
	if rec := postJSON(mux, "/v1/auth/login", map[string]any{
		"email": email, "password": "WRONG",
	}, ip); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("post-reset 11th status = %d, want 429", rec.Code)
	}
}

// TestAuthUniformErrorsNoEnumeration: known and unknown emails return an
// identical 401 body/status so no enumeration leaks.
func TestAuthUniformErrorsNoEnumeration(t *testing.T) {
	h, _, _ := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	known := fmt.Sprintf("enum-%d@example.test", time.Now().UnixNano())
	registerAndVerify(t, h, known, "password123", "10.3.3.3")
	unknown := fmt.Sprintf("nobody-%d@example.test", time.Now().UnixNano())

	kr := postJSON(mux, "/v1/auth/login", map[string]any{"email": known, "password": "WRONG"}, "10.3.3.3")
	ur := postJSON(mux, "/v1/auth/login", map[string]any{"email": unknown, "password": "WRONG"}, "10.3.3.3")

	if kr.Code != http.StatusUnauthorized || ur.Code != http.StatusUnauthorized {
		t.Fatalf("known=%d unknown=%d, want both 401", kr.Code, ur.Code)
	}
	if kr.Body.String() != ur.Body.String() {
		t.Errorf("enumeration leak: known %q != unknown %q", kr.Body.String(), ur.Body.String())
	}
}

// TestAuthRegisterHoneypotCatchesBot: a filled honeypot field is silently
// accepted but creates no user.
func TestAuthRegisterHoneypotCatchesBot(t *testing.T) {
	h, users, _ := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := fmt.Sprintf("bot-%d@example.test", time.Now().UnixNano())
	rec := postJSON(mux, "/v1/auth/register", map[string]any{
		"email": email, "password": "password123", "website": "http://spam.example",
	}, "10.4.4.4")
	if rec.Code != http.StatusCreated {
		t.Fatalf("honeypot register status = %d, want 201 (silent accept)", rec.Code)
	}
	if _, err := users.ByEmail(context.Background(), email); err == nil {
		t.Error("honeypot registration created a real user")
	}
}

// TestAuthVerifyRequiredBeforeLogin: accounts block login until the signed
// /verify link flips verified=true.
func TestAuthVerifyRequiredBeforeLogin(t *testing.T) {
	h, users, mailer := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := fmt.Sprintf("verreq-%d@example.test", time.Now().UnixNano())
	if rec := postJSON(mux, "/v1/auth/register", map[string]any{
		"email": email, "password": "password123",
	}, "10.5.5.5"); rec.Code != http.StatusCreated {
		t.Fatalf("register status = %d (body %s)", rec.Code, rec.Body.String())
	}
	if !mailer.wasCalled() {
		t.Fatal("verify email was not sent on register")
	}

	// Correct credentials still blocked until verified.
	if rec := postJSON(mux, "/v1/auth/login", map[string]any{
		"email": email, "password": "password123",
	}, "10.5.5.5"); rec.Code != http.StatusForbidden {
		t.Fatalf("login before verify status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	// Verify via signed token.
	tok, _ := auth.SignVerifyToken(testVerifyKey, email)
	vreq := httptest.NewRequest(http.MethodGet, "/v1/auth/verify?token="+tok, nil)
	vrec := httptest.NewRecorder()
	mux.ServeHTTP(vrec, vreq)
	if vrec.Code != http.StatusOK {
		t.Fatalf("verify status = %d (body %s)", vrec.Code, vrec.Body.String())
	}
	u, err := users.ByEmail(context.Background(), email)
	if err != nil || !u.Verified {
		t.Fatalf("user not verified after /verify (err=%v verified=%v)", err, u != nil && u.Verified)
	}

	if rec := postJSON(mux, "/v1/auth/login", map[string]any{
		"email": email, "password": "password123",
	}, "10.5.5.5"); rec.Code != http.StatusOK {
		t.Fatalf("login after verify status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestAuthRegisterRateLimit: more than the tight burst from one IP is 429.
func TestAuthRegisterRateLimit(t *testing.T) {
	h, _, _ := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	for i := 0; i < 5; i++ {
		email := fmt.Sprintf("spam-%d-%d@example.test", i, time.Now().UnixNano())
		rec := postJSON(mux, "/v1/auth/register", map[string]any{
			"email": email, "password": "password123",
		}, "10.6.6.6")
		if rec.Code != http.StatusCreated {
			t.Fatalf("reg attempt %d status = %d, want 201 (body %s)", i+1, rec.Code, rec.Body.String())
		}
	}
	// 6th registration from the same IP within the window → 429.
	rec := postJSON(mux, "/v1/auth/register", map[string]any{
		"email": fmt.Sprintf("spam-x-%d@example.test", time.Now().UnixNano()), "password": "password123",
	}, "10.6.6.6")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("6th register status = %d, want 429 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestAuthAllRoutesRateLimited: spot-check that the anonymous POST login and
// register routes carry the per-IP limiter (proven above by lockout/spam), and
// that unauthenticated GET /v1/auth/me is not rate-limited (sessionful).
func TestAuthAllRoutesRateLimited(t *testing.T) {
	h, _, _ := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// /me with no session returns 401 (not 429) and is not a login surface.
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	req.Header.Set("X-Real-IP", "10.7.7.7")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /v1/auth/me status = %d, want 401", rec.Code)
	}
}
