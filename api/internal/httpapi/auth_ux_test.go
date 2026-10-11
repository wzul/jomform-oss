// Issue #62 + #63 integration tests:
//   - remember-me: unchecked → 24h server TTL + browser-session cookie;
//     checked → 14d.
//   - forgot/reset password: single-use token, old sessions die via pw_reset_at.
//   - trusted device: valid jf_trust cookie skips TOTP; cleared/new device still
//     challenged; pw_reset invalidates trust.
//
// These need a real Postgres (JOMFORM_TEST_DATABASE_URL) + Valkey
// (JOMFORM_TEST_VALKEY_URL) and are skipped in CI (which sets neither).
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/auth"
)

// requireIntegration skips the test when DB or Valkey are unavailable.
func requireIntegration(t *testing.T) (*AuthHandlers, *recorderMailer) {
	t.Helper()
	h, _, mailer := authTestFixture(t) // skips when env vars unset
	return h, mailer
}

// loginOnce performs a full password login, applying TOTP code / remember /
// trust cookie as given, and returns the recorder (for reading Set-Cookie).
//
// The client address is the test's own (testIP) rather than httptest's default
// RemoteAddr ("192.0.2.1"): that default is a literal outside every process's
// reclaimed /24, so a login this helper completes with a WRONG credential would
// leave a jf:loginfail:ip:192.0.2.1 counter that no run can ever clear.
func loginOnce(t *testing.T, h *AuthHandlers, email, password, totpCode string, totpRemember bool, trustValue string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	body := map[string]any{
		"email": email, "password": password,
	}
	if totpCode != "" {
		body["totp_code"] = totpCode
	}
	if totpRemember {
		body["totp_remember"] = true
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(string(mustJSON(body))))
	req.Header.Set("Content-Type", "application/json")
	setTestClientAddr(req, testIP(t))
	if trustValue != "" {
		req.AddCookie(&http.Cookie{Name: trustCookie, Value: trustValue})
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// cookieValueFrom extracts a Set-Cookie value for name.
func cookieValueFrom(rec *httptest.ResponseRecorder, name string) string {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

// postJSONCookies posts JSON with an extra session cookie to a bare mux.
func postJSONCookies(mux *http.ServeMux, path string, body map[string]any, ip, session string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(mustJSON(body))))
	req.Header.Set("Content-Type", "application/json")
	setTestClientAddr(req, ip)
	if session != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: session})
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestAuthRememberMeUncheckedShortSession: default (unchecked) login yields a
// browser-session cookie and a 24h server TTL, not the 14-day persistent one.
func TestAuthRememberMeUncheckedShortSession(t *testing.T) {
	ip := testIP(t)
	h, _ := requireIntegration(t)
	email := fmt.Sprintf("rem-no-%d@example.test", time.Now().UnixNano())
	registerAndVerify(t, h, email, "password123", ip)

	rec := loginOnce(t, h, email, "password123", "", false, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", rec.Code, rec.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no session cookie set")
	}
	// Issue #70: Go net/http emits Max-Age=0 for MaxAge<0 (expire immediately →
	// browser discards the cookie → unticked login "succeeds" but the user is
	// dropped back to the landing page). A true browser-session cookie must
	// OMIT the Max-Age attribute, which Go does only for MaxAge == 0.
	if cookie.MaxAge != 0 {
		t.Errorf("unchecked remember cookie MaxAge = %d, want 0 (omits Max-Age → browser session)", cookie.MaxAge)
	}
	if hdr := rec.Header().Get("Set-Cookie"); strings.Contains(hdr, "Max-Age") {
		t.Errorf("unchecked remember Set-Cookie must not contain Max-Age, got: %s", hdr)
	}
	// Server-side: the session key must not live long. Read its TTL.
	ttl := requireSessionTTL(t, h, cookie.Value)
	if ttl > 25*time.Hour {
		t.Errorf("unchecked session TTL = %v, want <= ~24h", ttl)
	}
}

// TestAuthRememberMeCheckedLongSession: checked login yields a 14-day cookie.
func TestAuthRememberMeCheckedLongSession(t *testing.T) {
	ip := testIP(t)
	h, _ := requireIntegration(t)
	email := fmt.Sprintf("rem-yes-%d@example.test", time.Now().UnixNano())
	registerAndVerify(t, h, email, "password123", ip)

	body := map[string]any{"email": email, "password": "password123", "remember": true}
	rec := postJSON(loginMux(h), "/v1/auth/login", body, ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", rec.Code, rec.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no session cookie set")
	}
	if cookie.MaxAge != int(rememberLongTTL.Seconds()) {
		t.Errorf("checked remember cookie MaxAge = %d, want %d", cookie.MaxAge, int(rememberLongTTL.Seconds()))
	}
	ttl := requireSessionTTL(t, h, cookie.Value)
	if ttl < 13*24*time.Hour {
		t.Errorf("checked session TTL = %v, want ~14d", ttl)
	}
}

// requireSessionTTL returns the remaining TTL of a session token server-side.
func requireSessionTTL(t *testing.T, h *AuthHandlers, token string) time.Duration {
	t.Helper()
	// The Session store doesn't expose TTL directly; derive it from issued +
	// configured TTL is wrong (unchecked=24h). Instead read the live Valkey TTL
	// via the shared client.
	ttl, err := h.Sessions.Client().TTL(context.Background(), "jf:session:"+token).Result()
	if err != nil {
		t.Fatalf("read session TTL: %v", err)
	}
	return ttl
}

// TestAuthForgotResetKillsOldSessions: after reset-password, a session issued
// before the reset is rejected (pw_reset_at), and the token is single-use.
func TestAuthForgotResetKillsOldSessions(t *testing.T) {
	ip := testIP(t)
	h, mailer := requireIntegration(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := fmt.Sprintf("reset-%d@example.test", time.Now().UnixNano())
	registerAndVerify(t, h, email, "password123", ip)

	// Establish a pre-reset session.
	login := postJSON(mux, "/v1/auth/login", map[string]any{"email": email, "password": "password123"}, ip)
	oldCookie := cookieValueFrom(login, sessionCookie)
	if oldCookie == "" {
		t.Fatal("no pre-reset session cookie")
	}
	// Old session works now.
	me := authMeGet(t, h, oldCookie)
	if me.Code != http.StatusOK {
		t.Fatalf("old session before reset status = %d", me.Code)
	}

	// Request a reset link.
	forgot := postJSON(mux, "/v1/auth/forgot-password", map[string]any{"email": email}, ip)
	if forgot.Code != http.StatusOK {
		t.Fatalf("forgot-password status = %d body=%s", forgot.Code, forgot.Body.String())
	}
	if !mailer.wasCalled() {
		t.Fatal("reset email not sent")
	}
	// Capture the raw token from the sent body (recorderMailer currently just
	// records a bool; extend it to store body).
	token := requireLastResetToken(t, mailer)
	if token == "" {
		t.Fatal("no reset token captured from email")
	}

	// Consume once → success.
	resp := postJSON(mux, "/v1/auth/reset-password", map[string]any{"token": token, "new_password": "newpassword123"}, ip)
	if resp.Code != http.StatusOK {
		t.Fatalf("reset-password status = %d body=%s", resp.Code, resp.Body.String())
	}
	// Same token again → consumed (single-use).
	resp2 := postJSON(mux, "/v1/auth/reset-password", map[string]any{"token": token, "new_password": "anotherpass123"}, ip)
	if resp2.Code != http.StatusBadRequest {
		t.Fatalf("reused reset token status = %d, want 400", resp2.Code)
	}

	// Old pre-reset session is now dead (pw_reset_at invalidation).
	if me := authMeGet(t, h, oldCookie); me.Code != http.StatusUnauthorized {
		t.Fatalf("old session after reset status = %d, want 401", me.Code)
	}
	// New password works.
	if rec := postJSON(mux, "/v1/auth/login", map[string]any{"email": email, "password": "newpassword123"}, ip); rec.Code != http.StatusOK {
		t.Fatalf("login with new password status = %d body=%s", rec.Code, rec.Body.String())
	}
}

// requireLastResetToken extracts the reset token from the most recent email.
// recorderMailer's Send stores the body so a test can capture the token.
func requireLastResetToken(t *testing.T, m *recorderMailer) string {
	t.Helper()
	body := m.lastBody()
	if body == "" {
		return ""
	}
	// The link is ".../reset-password?token=<hex>".
	idx := strings.Index(body, "reset-password?token=")
	if idx < 0 {
		return ""
	}
	rest := body[idx+len("reset-password?token="):]
	rest = strings.TrimSpace(rest)
	// token is 64 hex chars.
	if len(rest) >= 64 {
		return rest[:64]
	}
	return ""
}

func authMeGet(t *testing.T, h *AuthHandlers, session string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	req.Header.Set("Cookie", sessionCookie+"="+session)
	rec := httptest.NewRecorder()
	h.handleMe(rec, req)
	return rec
}

// TestAuthTrustedDeviceSkipsTOTP: with a valid jf_trust cookie, a 2FA user
// logs in with just the password (no TOTP challenge); a fresh/cleared device
// is still challenged. Trust is minted only on a completed 2FA login.
func TestAuthTrustedDeviceSkipsTOTP(t *testing.T) {
	ip := testIP(t)
	h, _ := requireIntegration(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := fmt.Sprintf("trust-%d@example.test", time.Now().UnixNano())
	registerAndVerify(t, h, email, "password123", ip)

	// Enable 2FA: login (establish session), setup, generate code, enable.
	sessCookie := cookieValueFrom(postJSON(mux, "/v1/auth/login", map[string]any{"email": email, "password": "password123"}, ip), sessionCookie)
	if sessCookie == "" {
		t.Fatal("no session cookie for 2FA setup")
	}
	uid := requireUserID(t, h, email)
	setup := postJSONCookies(mux, "/v1/auth/2fa/setup", map[string]any{}, ip, sessCookie)
	var s map[string]string
	_ = json.Unmarshal(setup.Body.Bytes(), &s)
	secret := s["secret"]
	if secret == "" {
		t.Fatalf("2FA setup no secret (status %d body %s)", setup.Code, setup.Body.String())
	}
	if rec := postJSONCookies(mux, "/v1/auth/2fa/enable", map[string]any{"code": mustTOTP(secret)}, ip, sessCookie); rec.Code != http.StatusOK {
		t.Fatalf("enable 2FA status = %d body=%s", rec.Code, rec.Body.String())
	}

	// Untrusted device => TOTP challenged.
	challenge := loginOnce(t, h, email, "password123", "", false, "")
	if challenge.Code != http.StatusUnauthorized {
		t.Fatalf("untrusted login status = %d, want 401 (challenge); body=%s", challenge.Code, challenge.Body.String())
	}
	var cr meResponse
	_ = json.Unmarshal(challenge.Body.Bytes(), &cr)
	if cr.NextStep == nil || *cr.NextStep != "totp_required" {
		t.Fatalf("expected next_step=totp_required, got %+v", cr.NextStep)
	}

	// Complete the 2FA login WITH totp_remember=true → trust cookie issued.
	ok := loginOnce(t, h, email, "password123", mustTOTP(secret), true, "")
	if ok.Code != http.StatusOK {
		t.Fatalf("trusted 2FA login status = %d body=%s", ok.Code, ok.Body.String())
	}
	trustToken := cookieValueFrom(ok, trustCookie)
	if trustToken == "" {
		t.Fatal("no trust cookie issued on totp_remember login")
	}

	// Now a password-only login on the SAME device (with trust cookie) skips TOTP.
	skip := loginOnce(t, h, email, "password123", "", false, trustToken)
	if skip.Code != http.StatusOK {
		t.Fatalf("trusted password-only login status = %d, want 200; body=%s", skip.Code, skip.Body.String())
	}

	// An invalid trust token still challenges.
	fake := loginOnce(t, h, email, "password123", "", false, "deadbeef")
	if fake.Code != http.StatusUnauthorized {
		t.Fatalf("invalid-trust login status = %d, want 401 (challenge)", fake.Code)
	}

	// Trust is invalidated on password reset.
	_resetTrust(t, h, email, uid)
	after := loginOnce(t, h, email, "password123", "", false, trustToken)
	if after.Code != http.StatusUnauthorized {
		t.Fatalf("post-reset trusted login status = %d, want 401 (challenge)", after.Code)
	}
}

// requireUserID resolves a user id by email.
func requireUserID(t *testing.T, h *AuthHandlers, email string) string {
	t.Helper()
	u, err := h.Users.ByEmail(context.Background(), email)
	if err != nil || u == nil {
		t.Fatalf("resolve user: %v", err)
	}
	return u.ID
}

// mustTOTP returns a current TOTP code for a secret, failing the test.
//
// Issue #528: a TOTP code is now single-use, so this returns a code from a
// strictly LATER time-step on each call for the same secret — modelling a real
// authenticator rather than repeating one code. Callers that verify several
// times (enable, then each login) therefore keep working, and a test can no
// longer accidentally assert "the same code works twice", which is the bug #528
// fixed.
//
// THE STEP ORDER IS FIXED AT ASCENDING, and that is a constraint, not a
// preference: `ConsumeTOTPStep` advances `users.totp_last_step` with the
// predicate `totp_last_step < $2` (api/internal/dbstore/user.go:143), so a code
// whose step is not greater than the last consumed one is REFUSED as a replay.
// Handing out {base, base+1, base-1} — longest-lived first, which looks strictly
// better in isolation — makes the second login fail 401 `login.invalid`
// (`trust_devices_issue454_test.go`), measured.
//
// THE FLAKE IS THEREFORE FIXED BY WAITING OUT THE BOUNDARY, NOT BY REORDERING
// (card t_e74defb7). The first code handed to a fresh secret has always been
// `base-1`, whose remaining life is 0..1 steps: generate it microseconds before a
// 30s boundary and validate microseconds after, and the server's step has
// advanced, `base-1` has left the ±1 window, and the request gets
// `{"error":"kod tidak sah"}`. That is the measured red — on an unrelated card,
// 0.55s in:
//
//	--- FAIL: TestMagicFlagSurvives2FAEnableRotation (0.55s)
//	    auth_stepup_issue458_test.go:552: enable 2FA status = 401 body={"error":"kod tidak sah"}
//
// and it is why the test passes 12/12 in isolation and reds occasionally inside a
// package run. Waiting until the clock is at least `totpBoundaryMargin` seconds
// into the current step before generating the FIRST code makes all three offsets
// (base-1, base, base+1) live at least that long, so no generation can race the
// boundary. It costs the wait once per secret — usually nothing, since a step is
// 30s — and the flake it removes used to cost a whole package run.
func mustTOTP(secret string) string {
	totpStepAlloc.Lock()
	first := totpStepAlloc.used[secret] == 0
	totpStepAlloc.Unlock()

	if first {
		waitOutStepBoundary()
	}

	totpStepAlloc.Lock()
	base := time.Now().Unix() / totpTestPeriod
	// Ascending, and it must stay ascending: see the ConsumeTOTPStep note above.
	window := [3]int64{base - 1, base, base + 1}
	idx := totpStepAlloc.used[secret]
	totpStepAlloc.used[secret] = idx + 1
	totpStepAlloc.Unlock()

	if int(idx) >= len(window) {
		panic("mustTOTP: exhausted the ±1 drift window for this secret; " +
			"a real authenticator cannot supply another valid code yet")
	}
	return totpCodeAtStep(secret, window[idx])
}

// totpBoundaryMargin is how far into a step the FIRST code of a secret may be
// generated. It must be comfortably less than the period (30s) — it delays a
// test only when it happens to start just before a boundary.
const totpBoundaryMargin = 3

// waitOutStepBoundary blocks until the current TOTP step has at least
// totpBoundaryMargin seconds left in it, so a code generated now cannot be
// validated after the step has advanced. It is a no-op in the common case.
func waitOutStepBoundary() {
	for {
		now := time.Now().Unix()
		if now%totpTestPeriod <= totpTestPeriod-totpBoundaryMargin {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// _resetTrust forces a password reset to invalidate the user's trust cookies.
func _resetTrust(t *testing.T, h *AuthHandlers, email, uid string) {
	t.Helper()
	hash, err := auth.HashPassword("freshpassword123")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Users.ResetPassword(context.Background(), uid, hash); err != nil {
		t.Fatal(err)
	}
	if h.Sessions != nil {
		_ = h.Sessions.TrustedDevices().RevokeAll(context.Background(), uid)
	}
}

// TestAuth2FAEnableInheritsRememberLong: a "remember me" login that enables 2FA
// must keep a 14-day persistent session post-rotation. Issue #69 hardcoded the
// fresh post-2FA session to remember=false, silently downgrading persistence.
func TestAuth2FAEnableInheritsRememberLong(t *testing.T) {
	ip := testIP(t)
	h, _ := requireIntegration(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := fmt.Sprintf("2fa-lng-%d@example.test", time.Now().UnixNano())
	registerAndVerify(t, h, email, "password123", ip)

	// Establish a 14-day (remember) session.
	login := postJSON(mux, "/v1/auth/login", map[string]any{"email": email, "password": "password123", "remember": true}, ip)
	origCookie := cookieValueFrom(login, sessionCookie)
	if origCookie == "" {
		t.Fatal("no session cookie on remembered login")
	}
	if !h.Sessions.Remembered(context.Background(), origCookie) {
		t.Fatal("expected original remembered session to report remember=true")
	}

	// Enable 2FA from that session.
	secret := requirePendingSecret(t, h, mux, email, origCookie)
	enable := postJSONCookies(mux, "/v1/auth/2fa/enable", map[string]any{"code": mustTOTP(secret)}, ip, origCookie)
	if enable.Code != http.StatusOK {
		t.Fatalf("enable 2FA status = %d body=%s", enable.Code, enable.Body.String())
	}
	rotated := cookieValueFrom(enable, sessionCookie)
	if rotated == "" || rotated == origCookie {
		t.Fatalf("expected a fresh session cookie post-2FA, got %q", rotated)
	}
	if !h.Sessions.Remembered(context.Background(), rotated) {
		t.Error("post-2FA rotated session must INHERIT remember=true (14d), got remember=false")
	}
	var rc *http.Cookie
	for _, c := range enable.Result().Cookies() {
		if c.Name == sessionCookie {
			rc = c
		}
	}
	if rc == nil || rc.MaxAge != int(rememberLongTTL.Seconds()) {
		t.Errorf("post-2FA rotated cookie MaxAge = %v, want %d (14d)", rc.MaxAge, int(rememberLongTTL.Seconds()))
	}
	ttl := requireSessionTTL(t, h, rotated)
	if ttl < 13*24*time.Hour {
		t.Errorf("post-2FA rotated TTL = %v, want ~14d", ttl)
	}
}

// TestAuth2FAEnableInheritsRememberShort: an UNCHECKED login (24h browser
// session) enabling 2FA must stay short — the rotation never upgrades
// persistence, it only preserves it.
func TestAuth2FAEnableInheritsRememberShort(t *testing.T) {
	ip := testIP(t)
	h, _ := requireIntegration(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := fmt.Sprintf("2fa-sht-%d@example.test", time.Now().UnixNano())
	registerAndVerify(t, h, email, "password123", ip)

	login := postJSON(mux, "/v1/auth/login", map[string]any{"email": email, "password": "password123"}, ip)
	origCookie := cookieValueFrom(login, sessionCookie)
	if origCookie == "" || h.Sessions.Remembered(context.Background(), origCookie) {
		t.Fatal("expected an unchecked session (remember=false)")
	}

	secret := requirePendingSecret(t, h, mux, email, origCookie)
	enable := postJSONCookies(mux, "/v1/auth/2fa/enable", map[string]any{"code": mustTOTP(secret)}, ip, origCookie)
	if enable.Code != http.StatusOK {
		t.Fatalf("enable 2FA status = %d body=%s", enable.Code, enable.Body.String())
	}
	rotated := cookieValueFrom(enable, sessionCookie)
	if h.Sessions.Remembered(context.Background(), rotated) {
		t.Error("post-2FA rotated session must inherit remember=false (stay short), got remember=true")
	}
	var rc *http.Cookie
	for _, c := range enable.Result().Cookies() {
		if c.Name == sessionCookie {
			rc = c
		}
	}
	if rc == nil || rc.MaxAge != 0 {
		t.Errorf("post-2FA short rotated cookie MaxAge = %v, want 0 (browser session)", rc.MaxAge)
	}
	ttl := requireSessionTTL(t, h, rotated)
	if ttl > 25*time.Hour {
		t.Errorf("post-2FA short rotated TTL = %v, want <= ~24h", ttl)
	}
}

// requirePendingSecret drives 2FA setup for a logged-in session and returns the
// pending secret so the test can generate a valid enable code.
func requirePendingSecret(t *testing.T, h *AuthHandlers, mux *http.ServeMux, email, session string) string {
	ip := testIP(t)
	t.Helper()
	setup := postJSONCookies(mux, "/v1/auth/2fa/setup", map[string]any{}, ip, session)
	var s map[string]string
	_ = json.Unmarshal(setup.Body.Bytes(), &s)
	if s["secret"] == "" {
		t.Fatalf("2FA setup no secret (status %d body %s)", setup.Code, setup.Body.String())
	}
	return s["secret"]
}
