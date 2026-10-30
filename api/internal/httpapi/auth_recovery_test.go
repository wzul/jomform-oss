// Integration tests for issues #72 (2FA email backup code) + #73 (passwordless
// magic link). Exercise the real handlers against real Postgres + Valkey. These
// are skipped unless JOMFORM_TEST_DATABASE_URL + JOMFORM_TEST_VALKEY_URL are
// set, so CI (which sets neither) stays green with unit-level tests only.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/auth"
)

// registerVerified2FA creates a verified user with 2FA fully enabled and
// returns the user's email. Reuses the standard TOTP enable path.
func registerVerified2FA(t *testing.T, h *AuthHandlers) string {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := "2fa-" + randSuffix() + "@example.test"
	rec := postJSON(mux, "/v1/auth/register", map[string]any{
		"email": email, "password": "correct-horse-battery-staple",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("register status = %d body=%s", rec.Code, rec.Body.String())
	}
	tok, err := auth.SignVerifyToken(testVerifyKey, email)
	if err != nil {
		t.Fatalf("sign verify token: %v", err)
	}
	if rec := postJSON(mux, "/v1/auth/verify?token="+tok, nil, ""); rec.Code != http.StatusOK {
		t.Fatalf("verify status = %d", rec.Code)
	}

	// Set up + enable 2FA (needs a session).
	lrec := postJSON(mux, "/v1/auth/login", map[string]any{
		"email": email, "password": "correct-horse-battery-staple",
	}, "")
	if lrec.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", lrec.Code, lrec.Body.String())
	}
	var cookie string
	for _, c := range lrec.Result().Cookies() {
		if c.Name == "jf_session" {
			cookie = c.Name + "=" + c.Value
		}
	}
	if cookie == "" {
		t.Fatal("login did not set session cookie")
	}
	sr := httptest.NewRequest(http.MethodPost, "/v1/auth/2fa/setup", strings.NewReader("{}"))
	sr.Header.Set("Content-Type", "application/json")
	sr.Header.Set("Cookie", cookie)
	srec := httptest.NewRecorder()
	mux.ServeHTTP(srec, sr)
	if srec.Code != http.StatusOK {
		t.Fatalf("2fa setup status = %d body=%s", srec.Code, srec.Body.String())
	}
	var out map[string]string
	_ = json.Unmarshal(srec.Body.Bytes(), &out)
	code := totpCode(t, out["secret"])
	er := httptest.NewRequest(http.MethodPost, "/v1/auth/2fa/enable", strings.NewReader(`{"code":"`+code+`"}`))
	er.Header.Set("Content-Type", "application/json")
	er.Header.Set("Cookie", cookie)
	erec := httptest.NewRecorder()
	mux.ServeHTTP(erec, er)
	if erec.Code != http.StatusOK {
		t.Fatalf("2fa enable status = %d body=%s", erec.Code, erec.Body.String())
	}
	return email
}

var codeRe = regexp.MustCompile(`(?m)^\s*(\d{6})\s*$`)
var linkRe = regexp.MustCompile(`magic-link\?token=([0-9a-f]{64})`)

// TestEmail2FARequiresPendingFlagProvesPassword: email-code endpoint refuses
// (still 200-generic, nothing sent) when called WITHOUT a prior correct-password
// totp_required. This proves the pending gate blocks blind enumeration.
func TestEmail2FARequiresPendingFlagProvesPassword(t *testing.T) {
	h, _, mailer := authTestFixture(t)
	email := registerVerified2FA(t, h)
	_ = mailer.wasCalled() // ignore register/enable sends

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	// One address for this test (never a literal — see testIP): the login
	// breaker's per-IP counter is keyed on it in a Valkey DB shared by every
	// checkout, and only addresses inside this process's own /24 are reclaimed.
	ip := testIP(t)
	// Attempt email-code WITHOUT ever doing the login challenge.
	rec := postJSON(mux, "/v1/auth/2fa/email-code", map[string]any{"email": email}, ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("email-code status = %d body=%s (want generic 200)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "dihantar") {
		t.Fatalf("unexpected generic body: %s", rec.Body.String())
	}
	// No 6-digit code should have been emailed by the LAST send. We check the
	// last mailer body does not contain our 6-digit pattern.
	if codeRe.MatchString(mailer.lastBody()) {
		t.Error("email-code endpoint issued a code without a pending flag (enumeration leak)")
	}
}

// TestEmail2FABackupCodeEndToEndSingleUse: correct-password totp_required arms
// the pending flag → request email-code sends a 6-digit code → login with the
// emailed code succeeds → the same code cannot be reused.
//
// Both counters this test touches are per-IP (`jf:loginfail:ip:` and
// `jf:2fa_pending:`), which live in the shared test Valkey; the whole flow runs
// on one `testIP(t)` address so the arm and the login it enables are seen as the
// same client and the fixture can reclaim the counters afterwards.
func TestEmail2FABackupCodeEndToEndSingleUse(t *testing.T) {
	h, _, mailer := authTestFixture(t)
	email := registerVerified2FA(t, h)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// One address for this test (see testIP) — never a literal, so the per-IP
	// counter it writes stays inside this process's own reclaimed /24.
	ip := testIP(t)

	// Step 1: correct-password-only login → totp_required (arms pending flag).
	rec := postJSON(mux, "/v1/auth/login", map[string]any{
		"email": email, "password": "correct-horse-battery-staple",
	}, ip)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("password-only login status = %d, want 401", rec.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["next_step"] != "totp_required" {
		t.Fatalf("expected totp_required, got %v", body)
	}

	// Step 2: request the emailed backup code from the SAME IP.
	rec = postJSON(mux, "/v1/auth/2fa/email-code", map[string]any{"email": email}, ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("email-code status = %d body=%s", rec.Code, rec.Body.String())
	}
	last := mailer.lastBody()
	var code string
	m := codeRe.FindStringSubmatch(last)
	if len(m) < 2 {
		t.Fatalf("no 6-digit code found in email body: %q", last)
	}
	code = m[1]

	// Step 3: login with the emailed code succeeds (email-OTP accepted in
	// totp_code field).
	rec = postJSON(mux, "/v1/auth/login", map[string]any{
		"email": email, "password": "correct-horse-battery-staple", "totp_code": code,
	}, ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("login with emailed code = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	// Step 4: the code is single-use — reuse must be rejected again at the 2FA
	// barrier the next time 2FA is challenged.
	rec = postJSON(mux, "/v1/auth/login", map[string]any{
		"email": email, "password": "correct-horse-battery-staple", "totp_code": code,
	}, ip)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("reused emailed code = %d, want 401 (single-use violated)", rec.Code)
	}
}

// TestEmail2FARateLimit: > 3 email-code requests/hour from one IP → 429.
func TestEmail2FARateLimit(t *testing.T) {
	h, _, _ := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	// One address for this test: the 3/hour bucket is per-IP and the 4th
	// request must be the one over the limit. A literal would put the counter
	// outside this process's reclaimed /24 (see testIP).
	ip := testIP(t)
	for i := 0; i < 3; i++ {
		rec := postJSON(mux, "/v1/auth/2fa/email-code", map[string]any{"email": "x@example.test"}, ip)
		if rec.Code != http.StatusOK {
			t.Fatalf("email-code request %d status = %d, want 200 (generic)", i+1, rec.Code)
		}
	}
	rec := postJSON(mux, "/v1/auth/2fa/email-code", map[string]any{"email": "x@example.test"}, ip)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("4th email-code request status = %d, want 429", rec.Code)
	}
}

// TestMagicLinkEndToEndSingleUse: request → token in email → verify starts a
// session → re-verify fails (single-use).
func TestMagicLinkEndToEndSingleUse(t *testing.T) {
	ip := testIP(t)
	h, _, mailer := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := "magic-" + randSuffix() + "@example.test"
	rec := postJSON(mux, "/v1/auth/register", map[string]any{
		"email": email, "password": "correct-horse-battery-staple",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("register status = %d", rec.Code)
	}
	tok, _ := auth.SignVerifyToken(testVerifyKey, email)
	vreq := httptest.NewRequest(http.MethodGet, "/v1/auth/verify?token="+tok, nil)
	vrec := httptest.NewRecorder()
	mux.ServeHTTP(vrec, vreq)
	if vrec.Code != http.StatusOK {
		t.Fatalf("verify status = %d", vrec.Code)
	}

	// Request the magic link.
	rec = postJSON(mux, "/v1/auth/magic-link/request", map[string]any{"email": email}, ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("magic-link request status = %d body=%s", rec.Code, rec.Body.String())
	}
	last := mailer.lastBody()
	mt := linkRe.FindStringSubmatch(last)
	if len(mt) < 2 {
		t.Fatalf("no magic link token in email body: %q", last)
	}
	token := mt[1]

	// Verify → a session cookie must be issued.
	rec = postJSON(mux, "/v1/auth/magic-link/verify", map[string]any{"token": token}, ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("magic-link verify status = %d body=%s", rec.Code, rec.Body.String())
	}
	gotCookie := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == "jf_session" && c.Value != "" {
			gotCookie = true
		}
	}
	if !gotCookie {
		t.Fatal("magic-link verify did not issue a session cookie")
	}

	// The token is single-use — a second verify must fail.
	rec = postJSON(mux, "/v1/auth/magic-link/verify", map[string]any{"token": token}, ip)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("reused magic-link token status = %d, want 400 (single-use violated)", rec.Code)
	}
}

// TestMagicLinkTTL: after the 15-min TTL expires the token is unusable.
// We assert the store TTL constants (the authoritative expiry source) and that
// an unknown/expired token is rejected.
func TestMagicLinkTTL(t *testing.T) {
	ip := testIP(t)
	if auth.MagicLinkTTL != 15*time.Minute {
		t.Fatalf("MagicLinkTTL = %v, want 15m", auth.MagicLinkTTL)
	}
	if auth.Email2FACodeTTL != 10*time.Minute {
		t.Fatalf("Email2FACodeTTL = %v, want 10m", auth.Email2FACodeTTL)
	}
	if auth.Email2FAPendingTTL != 5*time.Minute {
		t.Fatalf("Email2FAPendingTTL = %v, want 5m", auth.Email2FAPendingTTL)
	}

	h, _, _ := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	rec := postJSON(mux, "/v1/auth/magic-link/verify", map[string]any{"token": "0000000000000000000000000000000000000000000000000000000000000000"}, ip)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expired/unknown magic token status = %d, want 400", rec.Code)
	}
}

// TestMagicLinkHybridSignup: a NEW email gets a signup magic link that, on
// click, creates the account (verified) + issues a session cookie (issue #278).
func TestMagicLinkHybridSignup(t *testing.T) {
	ip := testIP(t)
	h, users, mailer := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := "hybrid-" + randSuffix() + "@example.test"
	// The email must not exist yet.
	if u, _ := users.ByEmail(context.Background(), email); u != nil {
		t.Fatalf("email %s already exists", email)
	}

	// Request a magic link for the new email → a link must be emailed.
	rec := postJSON(mux, "/v1/auth/magic-link/request", map[string]any{"email": email}, ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("magic-link request status = %d body=%s", rec.Code, rec.Body.String())
	}
	last := mailer.lastBody()
	mt := linkRe.FindStringSubmatch(last)
	if len(mt) < 2 {
		t.Fatalf("no magic link token in email body: %q", last)
	}
	token := mt[1]

	// Verify → account is created (verified) + a session cookie is issued.
	rec = postJSON(mux, "/v1/auth/magic-link/verify", map[string]any{"token": token}, ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("magic-link verify status = %d body=%s", rec.Code, rec.Body.String())
	}
	gotCookie := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == "jf_session" && c.Value != "" {
			gotCookie = true
		}
	}
	if !gotCookie {
		t.Fatal("magic-link signup verify did not issue a session cookie")
	}
	user, err := users.ByEmail(context.Background(), email)
	if err != nil || user == nil {
		t.Fatalf("account not created after signup magic link: err=%v", err)
	}
	if !user.Verified {
		t.Fatal("signup magic-link account should be verified (email confirmed by click)")
	}

	// The token is single-use — a second verify must fail.
	rec = postJSON(mux, "/v1/auth/magic-link/verify", map[string]any{"token": token}, ip)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("reused signup magic-link token status = %d, want 400 (single-use violated)", rec.Code)
	}
}

// TestMagicLinkVerifiesExistingUnverifiedAccount (card t_39c5ddad): clicking a
// mailed magic link is proof of ownership of the address — the same proof
// GET /v1/auth/verify accepts — so it must verify an EXISTING account that a
// password signup left unverified. Before this fix the click issued a session
// and left verified=false, so that account's password login answered
// 403 email.unverified forever, while the magic link kept working.
//
// The test asserts the gate is shut BEFORE the click (so it can only pass by
// lifting it), that the click flips verified on the SAME row without creating a
// second one or a second default account, and that the password login then
// succeeds.
func TestMagicLinkVerifiesExistingUnverifiedAccount(t *testing.T) {
	ip := testIP(t)
	h, users, mailer := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	ctx := context.Background()
	pool := bundleTestPool(t)
	const password = "correct-horse-battery-staple"

	count := func(q string, args ...any) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, q, args...).Scan(&n); err != nil {
			t.Fatalf("count %q: %v", q, err)
		}
		return n
	}

	// An existing account in exactly the state a password signup leaves behind
	// (verified=false, issue #42) — NOT a new address, which would take the
	// brand-new-account path that already worked.
	email := "unverified-" + randSuffix() + "@example.test"
	rec := postJSON(mux, "/v1/auth/register", map[string]any{
		"email": email, "password": password,
	}, ip)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register status = %d body=%s", rec.Code, rec.Body.String())
	}
	user, err := users.ByEmail(ctx, email)
	if err != nil || user == nil {
		t.Fatalf("load the registered account: err=%v user=%v", err, user)
	}
	if user.Verified {
		t.Fatal("fixture is wrong: a password signup must start verified=false, " +
			"otherwise this test proves nothing about the unverified path")
	}
	accountsBefore := count(`SELECT count(*) FROM accounts WHERE owner_user_id=$1`, user.ID)

	// The gate this card is about, asserted BEFORE the click: the credentials
	// are correct, only the missing verification blocks the login.
	rec = postJSON(mux, "/v1/auth/login", map[string]any{"email": email, "password": password}, ip)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"code":"email.unverified"`) {
		t.Fatalf("password login before the magic-link click = %d body=%s, want 403 email.unverified — "+
			"without that precondition this test could pass on a tree where the unverified gate never fired",
			rec.Code, rec.Body.String())
	}

	// Request the link. An existing UNVERIFIED account is not on the
	// existing-verified-user branch, so the mail it gets is the sign-up variant
	// (measured on prod, t_89743ea3) — pinned by content, not by a copied
	// string: the body must be the signup rendering of exactly this link.
	rec = postJSON(mux, "/v1/auth/magic-link/request", map[string]any{"email": email}, ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("magic-link request status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := mailer.lastBody()
	mt := linkRe.FindStringSubmatch(body)
	if len(mt) < 2 {
		t.Fatalf("no magic link token in email body: %q", body)
	}
	token := mt[1]
	link := strings.TrimRight(h.BaseURL, "/") + "/magic-link?token=" + token
	if _, wantBody := magicLinkEmailText(link, true, ""); body != wantBody {
		t.Fatalf("magic-link mail for an existing unverified account is not the sign-up variant.\n got: %q\nwant: %q", body, wantBody)
	}

	// Click it → a session must be issued.
	rec = postJSON(mux, "/v1/auth/magic-link/verify", map[string]any{"token": token}, ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("magic-link verify status = %d body=%s", rec.Code, rec.Body.String())
	}
	gotCookie := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == "jf_session" && c.Value != "" {
			gotCookie = true
		}
	}
	if !gotCookie {
		t.Fatal("magic-link verify did not issue a session cookie")
	}

	// (a) The click verified the account …
	after, err := users.ByEmail(ctx, email)
	if err != nil || after == nil {
		t.Fatalf("re-read the account after the click: err=%v user=%v", err, after)
	}
	if !after.Verified {
		t.Fatal("clicking the magic link signed the user in but left verified=false — " +
			"the click proved ownership of the address, so the account must be verified " +
			"(card t_39c5ddad)")
	}
	// … on the SAME row: no duplicate user, no re-created account.
	if after.ID != user.ID || !after.CreatedAt.Equal(user.CreatedAt) {
		t.Fatalf("the click replaced the account: id %s→%s, created_at %s→%s",
			user.ID, after.ID, user.CreatedAt, after.CreatedAt)
	}
	if n := count(`SELECT count(*) FROM users WHERE email=$1`, email); n != 1 {
		t.Fatalf("users rows for %s = %d, want 1 (the click must not create a duplicate)", email, n)
	}
	// The ErrEmailTaken branch must not auto-provision a second default account.
	if n := count(`SELECT count(*) FROM accounts WHERE owner_user_id=$1`, user.ID); n != accountsBefore {
		t.Fatalf("accounts for the existing user = %d, want %d (the existing-account path must not provision another)", n, accountsBefore)
	}

	// (b) The password path is now open — pre-fix this is the 403 above.
	rec = postJSON(mux, "/v1/auth/login", map[string]any{"email": email, "password": password}, ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("password login after the magic-link click = %d body=%s, want 200 — "+
			"the verified flip did not lift the password-login gate", rec.Code, rec.Body.String())
	}
	loginCookie := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == "jf_session" && c.Value != "" {
			loginCookie = true
		}
	}
	if !loginCookie {
		t.Fatal("password login after the magic-link click issued no session cookie")
	}
}

// TestMagicLinkHybridSignupHoneypot: a bot that fills the hidden honeypot
// field on the magic-link request is silently discarded — no link is emailed
// and no account is created (issue #278 anti-spam).
func TestMagicLinkHybridSignupHoneypot(t *testing.T) {
	ip := testIP(t)
	h, users, mailer := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := "honey-" + randSuffix() + "@example.test"
	rec := postJSON(mux, "/v1/auth/magic-link/request", map[string]any{
		"email": email, "website": "https://spam.example",
	}, ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("magic-link request status = %d body=%s (want generic 200)", rec.Code, rec.Body.String())
	}
	if linkRe.MatchString(mailer.lastBody()) {
		t.Fatal("honeypot magic-link request still emailed a link (anti-spam bypass)")
	}
	if u, _ := users.ByEmail(context.Background(), email); u != nil {
		t.Fatal("honeypot magic-link request created an account (anti-spam bypass)")
	}
}

// TestEmail2FAStoreUnit exercises the store's consume semantics directly over
// Valkey (single-use, correct-mismatch behaviour).
func TestEmail2FAStoreUnit(t *testing.T) {
	sess := authTestValkey(t)
	ctx := context.Background()
	store := sess.Email2FA()
	userID := "u-" + randSuffix()

	code, err := auth.Generate6Digit()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(code) != 6 {
		t.Fatalf("code %q not 6 digits", code)
	}
	if err := store.Store(ctx, userID, code); err != nil {
		t.Fatalf("store: %v", err)
	}
	// Wrong code does not consume.
	if err := store.Consume(ctx, userID, "000000"); err == nil {
		t.Fatal("wrong code consumed (should be rejected, code left intact)")
	}
	// Correct code consumes.
	if err := store.Consume(ctx, userID, code); err != nil {
		t.Fatalf("consume: %v", err)
	}
	// Single-use: replay fails.
	if err := store.Consume(ctx, userID, code); err == nil {
		t.Fatal("reused emailed code should be rejected (single-use)")
	}
}
