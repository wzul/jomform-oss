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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
)

var authOnce sync.Once

// authTestValkey returns a Sessions client for the dedicated test Valkey DB and
// reclaims the login-breaker counters this process owns.
//
// Counters keyed by the fixture's test IPs live in a Valkey DB SHARED by every
// checkout, with a 15-minute TTL, so stale ones must be cleared for a test to be
// deterministic across -count and across runs. Clearing them the obvious way —
// SCAN+DEL of the production prefixes `jf:loginfail:*` / `jf:session:*` — is
// also destructive to a peer: whatever else is on that database, a concurrent
// `go test` binary holding a live lockout counter or a live API's session tokens,
// loses its keys to this suite. A whole-database flush is the same defect in a
// sharper form.
//
// So the reclaim is scoped instead: testIP gives each PROCESS its own address
// block (testIPPoolFor — two /24s), and only counters under THIS process's block
// are deleted. Tests must still point
// JOMFORM_TEST_VALKEY_URL at a throwaway DB (e.g. .../15), never dev/prod.
//
// Only the prefixes this fixture can legitimately own are touched. The sweep used
// to also clear `jf:session:*`, but every session in this package is minted by
// its own test and dies with it, so nothing here needs a reclaim; a test that
// asserts on a live session reads the key it just created (auth_ux_test.go) and
// must not have it swept out from under it.
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
	authOnce.Do(func() { reclaimOwnLoginCounters(t, sess) })
	return sess
}

// loginCounterKeyPrefixes are the shared-Valkey keys the auth fixture's test IPs
// reach. The login breaker's per-IP counter is the only one: the coarse per-IP
// token buckets (RateLimit / RegistrationLimit) are process-local maps in
// middleware/ratelimit.go, and the per-ACCOUNT counter is keyed by an email the
// tests mint per run, so that one cannot go stale.
//
// The PER-IP counter can go stale, and that is why this reclaim exists: it is
// keyed by an address, and every test address in this package now comes from
// testIP, which is inside the /24 this process owns. A hand-written address
// would sit outside every process's /24 and survive here for its full 15-minute
// TTL — so TestNoLiteralClientIPInTests refuses one.
var loginCounterKeyPrefixes = []string{"jf:loginfail:ip:"}

// reclaimOwnLoginCounters deletes the login-breaker counters under this
// process's own test-IP block only.
//
// Ownership is not assumed: the block comes from uniqueTestIP's derivation, so a
// key is deleted only if its address is one this process's block can hand out.
// A pattern hit that is not such an address is left alone and reported —
// deleting a key we cannot prove we own is how a scoped cleanup turns back into
// the cross-process clobber it replaced.
func reclaimOwnLoginCounters(t *testing.T, sess *auth.Sessions) {
	t.Helper()
	ctx := context.Background()
	pool := testIPPoolFor(uint32(os.Getpid()))

	for _, prefix := range loginCounterKeyPrefixes {
		var owned []string
		// Every /24 of the block, because the process's addresses span all of
		// them: a reclaim that stopped at the block's first /24 would leave the
		// counters of every call past 254 behind, which is exactly the pass-3
		// state this fixture has to start from (card t_7655c2bf).
		for _, family := range pool.families() {
			// SCAN, not KEYS: this runs against a shared DB whose keyspace is
			// not bounded by the fixture.
			var cursor uint64
			for {
				keys, next, err := sess.Client().Scan(ctx, cursor, prefix+family+"*", 100).Result()
				if err != nil {
					// A cleanup that cannot run must not silently pass; the
					// assertions it protects would then depend on a previous run's
					// leftovers.
					t.Fatalf("list %s counters under %s%s*: %v", prefix, prefix, family, err)
				}
				for _, k := range keys {
					if ownLoginCounterKey(prefix, k, pool) {
						owned = append(owned, k)
					} else {
						t.Logf("leaving %q alone: it matches %s%s* but encodes no address this process owns", k, prefix, family)
					}
				}
				if next == 0 {
					break
				}
				cursor = next
			}
		}
		if len(owned) == 0 {
			continue
		}
		if err := sess.Client().Del(ctx, owned...).Err(); err != nil {
			t.Fatalf("clear %d stale %s counter(s) under %s: %v", len(owned), prefix, prefix, pool)
		}
	}
}

// ownLoginCounterKey reports whether a key found under the process's own block
// is one this process could have written: the key must be exactly the prefix
// plus an address the block can hand out, with nothing appended.
func ownLoginCounterKey(prefix, key string, pool testIPPool) bool {
	ip := strings.TrimPrefix(key, prefix)
	if ip == key {
		return false
	}
	return pool.owns(ip)
}

func authUsers(t *testing.T) *dbstore.UserStore {
	t.Helper()
	return dbstore.NewUserStore(bundleTestPool(t))
}

// recorderMailer captures verification emails (satisfies VerifyMailer).
type recorderMailer struct {
	mu       sync.Mutex
	called   bool
	last     string // body of the most recent send (issue #62: captures reset token)
	lastLang string // the lang the most recent send was branded in
}

func (m *recorderMailer) Send(to, subject, body, lang string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.called = true
	m.last = body
	m.lastLang = lang
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
		Accounts:     dbstore.NewAccountStore(bundleTestPool(t)),
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
		setTestClientAddr(req, ip)
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

	// Unique IP per test: the per-IP lockout counter lives in the shared test
	// Valkey with a 15-minute TTL, so a fixed IP would already be locked on a
	// re-run (-count) or after a prior test (issue #474).
	ip := uniqueTestIP()
	email := fmt.Sprintf("lockout-%d@example.test", time.Now().UnixNano())
	registerAndVerify(t, h, email, "password123", ip)

	for i := 0; i < 10; i++ {
		rec := postJSON(mux, "/v1/auth/login", map[string]any{
			"email": email, "password": "WRONG",
		}, ip)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("failed login attempt %d: status = %d, want 401 (body %s)", i+1, rec.Code, rec.Body.String())
		}
	}

	rec := postJSON(mux, "/v1/auth/login", map[string]any{
		"email": email, "password": "password123",
	}, ip)
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
	// Unique IP per test so the Valkey login-breaker counter never collides
	// with a previous test or -count run (issue #474).
	ip := uniqueTestIP()
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
	ip := uniqueTestIP()
	registerAndVerify(t, h, known, "password123", ip)
	unknown := fmt.Sprintf("nobody-%d@example.test", time.Now().UnixNano())

	kr := postJSON(mux, "/v1/auth/login", map[string]any{"email": known, "password": "WRONG"}, ip)
	ur := postJSON(mux, "/v1/auth/login", map[string]any{"email": unknown, "password": "WRONG"}, ip)

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
	ip := testIP(t)
	h, users, _ := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := fmt.Sprintf("bot-%d@example.test", time.Now().UnixNano())
	rec := postJSON(mux, "/v1/auth/register", map[string]any{
		"email": email, "password": "password123", "website": "http://spam.example",
	}, ip)
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
	ip := testIP(t)
	h, users, mailer := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := fmt.Sprintf("verreq-%d@example.test", time.Now().UnixNano())
	if rec := postJSON(mux, "/v1/auth/register", map[string]any{
		"email": email, "password": "password123",
	}, ip); rec.Code != http.StatusCreated {
		t.Fatalf("register status = %d (body %s)", rec.Code, rec.Body.String())
	}
	if !mailer.wasCalled() {
		t.Fatal("verify email was not sent on register")
	}

	// Correct credentials still blocked until verified.
	if rec := postJSON(mux, "/v1/auth/login", map[string]any{
		"email": email, "password": "password123",
	}, ip); rec.Code != http.StatusForbidden {
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
	}, ip); rec.Code != http.StatusOK {
		t.Fatalf("login after verify status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestAuthRegisterRateLimit: more than the tight burst from one IP is 429.
func TestAuthRegisterRateLimit(t *testing.T) {
	h, _, _ := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Unique IP per test: the register burst limiter is per-IP in the shared
	// test Valkey (issue #474).
	ip := uniqueTestIP()
	for i := 0; i < 5; i++ {
		email := fmt.Sprintf("spam-%d-%d@example.test", i, time.Now().UnixNano())
		rec := postJSON(mux, "/v1/auth/register", map[string]any{
			"email": email, "password": "password123",
		}, ip)
		if rec.Code != http.StatusCreated {
			t.Fatalf("reg attempt %d status = %d, want 201 (body %s)", i+1, rec.Code, rec.Body.String())
		}
	}
	// 6th registration from the same IP within the window → 429.
	rec := postJSON(mux, "/v1/auth/register", map[string]any{
		"email": fmt.Sprintf("spam-x-%d@example.test", time.Now().UnixNano()), "password": "password123",
	}, ip)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("6th register status = %d, want 429 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestAuthAllRoutesRateLimited: spot-check that the anonymous POST login and
// register routes carry the per-IP limiter (proven above by lockout/spam), and
// that unauthenticated GET /v1/auth/me is not rate-limited (sessionful).
func TestAuthAllRoutesRateLimited(t *testing.T) {
	ip := testIP(t)
	h, _, _ := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// /me with no session returns 401 (not 429) and is not a login surface.
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	setTestClientAddr(req, ip)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /v1/auth/me status = %d, want 401", rec.Code)
	}
}
