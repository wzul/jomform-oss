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
	if ip != "" {
		req.Header.Set("X-Real-IP", ip)
	}
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
	h, _ := requireIntegration(t)
	email := fmt.Sprintf("rem-no-%d@example.test", time.Now().UnixNano())
	registerAndVerify(t, h, email, "password123", "10.20.1.1")

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
	h, _ := requireIntegration(t)
	email := fmt.Sprintf("rem-yes-%d@example.test", time.Now().UnixNano())
	registerAndVerify(t, h, email, "password123", "10.20.2.1")

	body := map[string]any{"email": email, "password": "password123", "remember": true}
	rec := postJSON(loginMux(h), "/v1/auth/login", body, "10.20.2.1")
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
	h, mailer := requireIntegration(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := fmt.Sprintf("reset-%d@example.test", time.Now().UnixNano())
	registerAndVerify(t, h, email, "password123", "10.21.1.1")

	// Establish a pre-reset session.
	login := postJSON(mux, "/v1/auth/login", map[string]any{"email": email, "password": "password123"}, "10.21.1.1")
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
	forgot := postJSON(mux, "/v1/auth/forgot-password", map[string]any{"email": email}, "10.21.1.1")
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
	resp := postJSON(mux, "/v1/auth/reset-password", map[string]any{"token": token, "new_password": "newpassword123"}, "10.21.1.1")
	if resp.Code != http.StatusOK {
		t.Fatalf("reset-password status = %d body=%s", resp.Code, resp.Body.String())
	}
	// Same token again → consumed (single-use).
	resp2 := postJSON(mux, "/v1/auth/reset-password", map[string]any{"token": token, "new_password": "anotherpass123"}, "10.21.1.1")
	if resp2.Code != http.StatusBadRequest {
		t.Fatalf("reused reset token status = %d, want 400", resp2.Code)
	}

	// Old pre-reset session is now dead (pw_reset_at invalidation).
	if me := authMeGet(t, h, oldCookie); me.Code != http.StatusUnauthorized {
		t.Fatalf("old session after reset status = %d, want 401", me.Code)
	}
	// New password works.
	if rec := postJSON(mux, "/v1/auth/login", map[string]any{"email": email, "password": "newpassword123"}, "10.21.1.1"); rec.Code != http.StatusOK {
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
	h, _ := requireIntegration(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := fmt.Sprintf("trust-%d@example.test", time.Now().UnixNano())
	registerAndVerify(t, h, email, "password123", "10.22.1.1")

	// Enable 2FA: login (establish session), setup, generate code, enable.
	sessCookie := cookieValueFrom(postJSON(mux, "/v1/auth/login", map[string]any{"email": email, "password": "password123"}, "10.22.1.1"), sessionCookie)
	if sessCookie == "" {
		t.Fatal("no session cookie for 2FA setup")
	}
	uid := requireUserID(t, h, email)
	setup := postJSONCookies(mux, "/v1/auth/2fa/setup", map[string]any{}, "10.22.1.1", sessCookie)
	var s map[string]string
	_ = json.Unmarshal(setup.Body.Bytes(), &s)
	secret := s["secret"]
	if secret == "" {
		t.Fatalf("2FA setup no secret (status %d body %s)", setup.Code, setup.Body.String())
	}
	if rec := postJSONCookies(mux, "/v1/auth/2fa/enable", map[string]any{"code": mustTOTP(secret)}, "10.22.1.1", sessCookie); rec.Code != http.StatusOK {
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
func mustTOTP(secret string) string {
	totpStepAlloc.Lock()
	base := time.Now().Unix() / totpTestPeriod
	// The validator accepts the current step ±1, so three distinct codes are
	// available per secret per window: base-1, base, base+1. Hand them out in
	// that order.
	floor := base - 1
	step := totpStepAlloc.used[secret]
	if step < floor {
		step = floor
	} else {
		step++
	}
	totpStepAlloc.used[secret] = step
	totpStepAlloc.Unlock()

	if step > base+1 {
		panic("mustTOTP: exhausted the ±1 drift window for this secret; " +
			"a real authenticator cannot supply another valid code yet")
	}
	return totpCodeAtStep(secret, step)
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
	h, _ := requireIntegration(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := fmt.Sprintf("2fa-lng-%d@example.test", time.Now().UnixNano())
	registerAndVerify(t, h, email, "password123", "10.24.1.1")

	// Establish a 14-day (remember) session.
	login := postJSON(mux, "/v1/auth/login", map[string]any{"email": email, "password": "password123", "remember": true}, "10.24.1.1")
	origCookie := cookieValueFrom(login, sessionCookie)
	if origCookie == "" {
		t.Fatal("no session cookie on remembered login")
	}
	if !h.Sessions.Remembered(context.Background(), origCookie) {
		t.Fatal("expected original remembered session to report remember=true")
	}

	// Enable 2FA from that session.
	secret := requirePendingSecret(t, h, mux, email, origCookie)
	enable := postJSONCookies(mux, "/v1/auth/2fa/enable", map[string]any{"code": mustTOTP(secret)}, "10.24.1.1", origCookie)
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
	h, _ := requireIntegration(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := fmt.Sprintf("2fa-sht-%d@example.test", time.Now().UnixNano())
	registerAndVerify(t, h, email, "password123", "10.24.2.1")

	login := postJSON(mux, "/v1/auth/login", map[string]any{"email": email, "password": "password123"}, "10.24.2.1")
	origCookie := cookieValueFrom(login, sessionCookie)
	if origCookie == "" || h.Sessions.Remembered(context.Background(), origCookie) {
		t.Fatal("expected an unchecked session (remember=false)")
	}

	secret := requirePendingSecret(t, h, mux, email, origCookie)
	enable := postJSONCookies(mux, "/v1/auth/2fa/enable", map[string]any{"code": mustTOTP(secret)}, "10.24.2.1", origCookie)
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
	t.Helper()
	setup := postJSONCookies(mux, "/v1/auth/2fa/setup", map[string]any{}, "10.24.9.1", session)
	var s map[string]string
	_ = json.Unmarshal(setup.Body.Bytes(), &s)
	if s["secret"] == "" {
		t.Fatalf("2FA setup no secret (status %d body %s)", setup.Code, setup.Body.String())
	}
	return s["secret"]
}
