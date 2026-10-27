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
// asserts on a live session reads the key it just created (auth_ux_test.go, via
// Sessions.TTL) and must not have it swept out from under it.
func authTestValkey(t *testing.T) *auth.Sessions {
	t.Helper()
	valkeyURL := os.Getenv("JOMFORM_TEST_VALKEY_URL")
	if valkeyURL == "" {
		t.Skip("JOMFORM_TEST_VALKEY_URL not set; skipping auth hardening integration test")
	}
	sess, err := auth.NewSessions(valkeyURL, auth.WithEmailCodeKey(testEmailCodeKey(t)))
	if err != nil {
		t.Fatalf("valkey sessions: %v", err)
	}
	authOnce.Do(func() { reclaimOwnLoginCounters(t, sess) })
	return sess
}

// loginCounterKeyPrefixes are the shared-Valkey keys the auth fixture's test IPs
// reach. The login breaker's per-IP counter is the only one: the coarse per-IP
// token buckets (RateLimit / RegistrationLimit) are process-local maps in
// middleware/ratelimit.go, and the breaker keys nothing on an email at all (the
// lockout is per source address — see auth/loginlimiter.go), so the email-keyed
// counters that would outlive a run do not exist.
//
// The PER-IP counter can go stale, and that is why this reclaim exists: it is
// keyed by an address, and every test address in this package now comes from
// testIP, which is inside the block this process owns. A hand-written address
// would sit outside every process's block and survive here for its full
// 15-minute TTL — so TestNoLiteralClientIPInTests refuses one.
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

// testEmailCodeKey derives the server-side key the emailed 2FA backup code is
// authenticated with (card t_a3f3aca5), from the same 32-byte test secret the
// verification links use. Without it the Email2FA store fails closed by design,
// so the fixture has to wire it for the end-to-end 2FA flow to run at all.
func testEmailCodeKey(t *testing.T) []byte {
	t.Helper()
	key, err := auth.EmailCodeKey(testVerifyKey)
	if err != nil {
		t.Fatalf("derive email code key: %v", err)
	}
	return key
}

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

// TestAuthLoginDelayAppliedInTheFiveToNineBand pins the friction half of issue
// #42 section A.2: the login gate consumes LoginLimiter.Status's `delayed` flag.
//
// The bite is exact, not a stopwatch: the seam records the duration the gate
// applied and on which attempts, so an implementation that ignores the flag
// records NOTHING here (that was the state before this test: `locked, _, _, err
// := ... Status(...)` discarded it and every failure in the band was answered
// at argon2 speed), and one that delays the wrong attempts (the first five, the
// 429, or a per-account-keyed variant) fails the count. The uniform 401 body and
// the absence of Retry-After are asserted on every attempt in the band: the
// penalty must cost wall clock and nothing else, or it becomes the enumeration /
// contradictory-backoff signal the per-address gate exists to avoid.
func TestAuthLoginDelayAppliedInTheFiveToNineBand(t *testing.T) {
	h, _, _ := authTestFixture(t)
	mux := loginMux(h)
	ip := uniqueTestIP()
	email := fmt.Sprintf("delay-%s@example.test", runKey())
	registerAndVerify(t, h, email, "password123", ip)

	var mu sync.Mutex
	var delays []time.Duration
	h.loginDelaySleep = func(_ context.Context, d time.Duration) {
		mu.Lock()
		delays = append(delays, d)
		mu.Unlock()
	}
	recorded := func() []time.Duration {
		mu.Lock()
		defer mu.Unlock()
		return append([]time.Duration(nil), delays...)
	}

	// Ten failures: attempts 1..5 arrive with a count of 0..4 (no delay),
	// attempts 6..10 arrive with 5..9 (delayed), and the 11th is refused.
	var bodies []string
	for i := 1; i <= auth.FailLockoutThreshold; i++ {
		rec := postJSON(mux, "/v1/auth/login", map[string]any{
			"email": email, "password": "WRONG",
		}, ip)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d: status = %d, want 401 (body %s)", i, rec.Code, rec.Body.String())
		}
		if ra := rec.Header().Get("Retry-After"); ra != "" {
			t.Errorf("failure %d carries Retry-After %q: the delay is a pacing penalty on a normal 401, not a refusal", i, ra)
		}
		bodies = append(bodies, rec.Body.String())
	}
	for i, b := range bodies {
		if b != bodies[0] {
			t.Errorf("failure %d body = %q, want the uniform %q — the delay must not change the response", i+1, b, bodies[0])
		}
	}

	got := recorded()
	wantDelayed := auth.FailLockoutThreshold - auth.FailDelayThreshold // attempts 6..10
	if len(got) != wantDelayed {
		t.Fatalf("gate applied the delay on %d of %d failures in the 5..9 band, want %d — attempts 1..%d arrive below auth.FailDelayThreshold and the 429 must not sleep (recorded %v)",
			len(got), auth.FailLockoutThreshold, wantDelayed, auth.FailDelayThreshold, got)
	}
	for i, d := range got {
		if d != auth.LoginFailDelay {
			t.Errorf("delayed attempt %d waited %v, want %v", i+1, d, auth.LoginFailDelay)
		}
	}

	// The lockout is a refusal, not a wait: the 11th attempt answers at once.
	rec := postJSON(mux, "/v1/auth/login", map[string]any{
		"email": email, "password": "password123",
	}, ip)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("11th login: status = %d, want 429 (body %s)", rec.Code, rec.Body.String())
	}
	if ra := rec.Header().Get("Retry-After"); ra != auth.LoginLockoutRetryAfter {
		t.Errorf("11th login Retry-After = %q, want %q", ra, auth.LoginLockoutRetryAfter)
	}
	if extra := len(recorded()); extra != len(got) {
		t.Errorf("the locked-out 429 also slept (%d delays recorded, want %d): the delay paces the 5..9 band, the lockout refuses", extra, len(got))
	}
}

// TestSleepCtxWaitsThenHonoursCancellation covers the production half of the
// delay that the seam above deliberately does not exercise: the default sleep
// really spends wall clock, and a client that hangs up releases the handler
// instead of pinning a goroutine for the whole penalty.
func TestSleepCtxWaitsThenHonoursCancellation(t *testing.T) {
	const d = 60 * time.Millisecond

	start := time.Now()
	sleepCtx(context.Background(), d)
	if elapsed := time.Since(start); elapsed < d {
		t.Errorf("sleepCtx(%v) returned after %v — the real delay is shorter than the constant it stands for", d, elapsed)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start = time.Now()
	sleepCtx(ctx, 30*time.Second)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("sleepCtx ignored cancellation and waited %v", elapsed)
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

// TestAuthFailedLoginsAreCountedPerAddressNotPerAccount pins the lockout's scope,
// both directions of it.
//
// The breaker counts failures per SOURCE ADDRESS and nothing else. It used to
// also write a per-account counter under `jf:loginfail:user:<email>` that no code
// ever read: ten failures for one account produced a key sitting at 10 while the
// account stayed wide open (measured on prod), and the file's own comment
// advertised the per-account protection all the same. This test is what says the
// counter is gone: the burst below is ten failures for ONE verified account,
// spread one per address, and afterwards
//
//	(a) no `jf:loginfail:user:` key exists for that account at all,
//	(b) each of the ten addresses carries exactly its own count of 1 (which is
//	    also what makes the burst genuinely spread — if the requests ever stopped
//	    presenting the address, every failure would land on httptest's default
//	    192.0.2.1 and this half would fail), and
//	(c) the CORRECT password for that account still logs in from an eleventh,
//	    untouched address: one account's attacker cannot lock the owner out.
//
// (c) is the tripwire for the other resolution of the same mismatch: ENFORCING a
// per-account lockout is a change of lockout semantics (it needs a 429 that
// answers "does this account exist?" to an attacker spending one attempt per
// address, and it hands anyone a way to lock a known user out of their account),
// so it must not arrive as a silent edit — it arrives as this test going red, and
// whoever changes it has to argue for it in the same change. See
// auth/loginlimiter.go for the reasoning.
func TestAuthFailedLoginsAreCountedPerAddressNotPerAccount(t *testing.T) {
	h, _, _ := authTestFixture(t)
	mux := loginMux(h)
	rdb := h.Sessions.Client()
	ctx := context.Background()

	email := fmt.Sprintf("perip-%s@example.test", runKey())
	// Eleven addresses: ten for the burst and one kept completely clean, so (c)
	// shows the account is open rather than showing a fresh address is.
	ips := make([]string, 0, 11)
	for i := 0; i < 11; i++ {
		ips = append(ips, uniqueTestIP())
	}
	registerAndVerify(t, h, email, "password123", ips[0])

	accountKey := "jf:loginfail:user:" + email
	t.Cleanup(func() {
		keys := append([]string{accountKey}, keysForIPs(ips)...)
		if err := rdb.Del(context.Background(), keys...).Err(); err != nil {
			t.Logf("cleanup: delete %v: %v", keys, err)
		}
	})

	for i := 0; i < 10; i++ {
		rec := postJSON(mux, "/v1/auth/login", map[string]any{
			"email": email, "password": "WRONG",
		}, ips[i])
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d from %s: status = %d, want 401 (body %s)", i+1, ips[i], rec.Code, rec.Body.String())
		}
	}

	// (a) The account counter must not exist. The exact key and a SCAN for any
	// case variant of it, since the prefix lower-cased its email.
	if n, err := rdb.Exists(ctx, accountKey).Result(); err != nil || n != 0 {
		t.Errorf("a per-account failure counter was written: %q exists=%d err=%v — the breaker must key on the address alone", accountKey, n, err)
	}
	var cursor uint64
	var accountHits []string
	for {
		keys, next, err := rdb.Scan(ctx, cursor, "jf:loginfail:user:*"+email, 100).Result()
		if err != nil {
			t.Fatalf("scan per-account counters: %v", err)
		}
		accountHits = append(accountHits, keys...)
		if next == 0 {
			break
		}
		cursor = next
	}
	if len(accountHits) > 0 {
		t.Errorf("per-account failure counter(s) written under the account prefix: %v — the login breaker must count per source address only", accountHits)
	}

	// (b) Each address carries its own single failure: the ten attempts were
	// spread, so none of them can explain a lockout of the account.
	for i, ip := range ips[:10] {
		got, err := rdb.Get(ctx, "jf:loginfail:ip:"+ip).Result()
		if err != nil || got != "1" {
			t.Errorf("address %d (%s): counter = %q err=%v, want \"1\" — the ten failures must be one per address, below the delay and lockout thresholds", i+1, ip, got, err)
		}
	}

	// (c) The account is NOT locked: its owner can still log in, from an address
	// that has never failed.
	rec := postJSON(mux, "/v1/auth/login", map[string]any{
		"email": email, "password": "password123",
	}, ips[10])
	if rec.Code != http.StatusOK {
		t.Fatalf("correct password from an unused address: status = %d, want 200 — ten failures for one account must not lock that account (body %s)", rec.Code, rec.Body.String())
	}
}

// keysForIPs maps addresses to the login breaker's counter keys.
func keysForIPs(ips []string) []string {
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		out = append(out, "jf:loginfail:ip:"+ip)
	}
	return out
}
