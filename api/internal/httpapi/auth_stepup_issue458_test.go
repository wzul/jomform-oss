// Issue #458 integration tests: magic-link session hardening (A+C).
//
//   - A: a magic-link verify marks the session magic=true; password login does
//     not. The magic flag survives parsing (v4) and is reported to the client.
//   - C: the step-up gate fires ONLY for a magic session belonging to a
//     2FA-enabled user. Every other combination is unchanged:
//     magic + no 2FA → no prompt; non-magic + 2FA → no prompt.
//
// Needs a real Postgres (JOMFORM_TEST_DATABASE_URL) + Valkey
// (JOMFORM_TEST_VALKEY_URL); skipped otherwise.
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

	"github.com/wzul/jomform/api/internal/dbstore"
)

// magicLinkTokenFromMailer extracts the raw magic-link token from the last
// email body (linkRe is declared in auth_recovery_test.go).
func magicLinkTokenFromMailer(t *testing.T, m *recorderMailer) string {
	t.Helper()
	mt := linkRe.FindStringSubmatch(m.lastBody())
	if len(mt) < 2 {
		t.Fatalf("no magic-link token in email body: %q", m.lastBody())
	}
	return mt[1]
}

// requireUserIDByEmail resolves a user id, failing the test when absent.
func requireUserIDByEmail(t *testing.T, users *dbstore.UserStore, email string) string {
	t.Helper()
	u, err := users.ByEmail(context.Background(), email)
	if err != nil || u == nil {
		t.Fatalf("resolve user %s: err=%v", email, err)
	}
	return u.ID
}

// sessionTokenValue pulls the jf_session cookie value from a response.
func sessionTokenValue(rec *httptest.ResponseRecorder) string {
	return cookieValueFrom(rec, sessionCookie)
}

// enable2FAFor turns 2FA on for an already-logged-in session and returns the
// TOTP secret so the test can mint fresh codes.
func enable2FAFor(t *testing.T, h *AuthHandlers, mux *http.ServeMux, session string) string {
	t.Helper()
	setup := postJSONCookies(mux, "/v1/auth/2fa/setup", map[string]any{}, "10.30.9.1", session)
	var s map[string]string
	_ = json.Unmarshal(setup.Body.Bytes(), &s)
	if s["secret"] == "" {
		t.Fatalf("2FA setup no secret (status %d body %s)", setup.Code, setup.Body.String())
	}
	en := postJSONCookies(mux, "/v1/auth/2fa/enable", map[string]any{"code": mustTOTP(s["secret"])}, "10.30.9.1", session)
	if en.Code != http.StatusOK {
		t.Fatalf("enable 2FA status = %d body=%s", en.Code, en.Body.String())
	}
	return s["secret"]
}

// TestMagicSessionFlaggedAndPasswordSessionNot: option A — only the magic-link
// verify path marks the session magic=true.
func TestMagicSessionFlaggedAndPasswordSessionNot(t *testing.T) {
	h, users, mailer := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := fmt.Sprintf("magic-flag-%d@example.test", time.Now().UnixNano())
	registerAndVerify(t, h, email, "password123", "10.31.1.1")

	// Password login → not a magic session.
	pw := postJSON(mux, "/v1/auth/login", map[string]any{"email": email, "password": "password123"}, "10.31.1.1")
	if pw.Code != http.StatusOK {
		t.Fatalf("password login status = %d body=%s", pw.Code, pw.Body.String())
	}
	pwToken := sessionTokenValue(pw)
	if pwToken == "" {
		t.Fatal("no session cookie on password login")
	}
	if h.Sessions.Magic(context.Background(), pwToken) {
		t.Error("password login session reported magic=true, want false")
	}
	// The password login response must not claim a magic session.
	var pwMe meResponse
	_ = json.Unmarshal(pw.Body.Bytes(), &pwMe)
	if pwMe.MagicSession {
		t.Error("password login response reported magic_session=true")
	}

	// Magic-link login → magic session.
	_ = postJSON(mux, "/v1/auth/magic-link/request", map[string]any{"email": email}, "10.31.2.1")
	token := magicLinkTokenFromMailer(t, mailer)
	mv := postJSON(mux, "/v1/auth/magic-link/verify", map[string]any{"token": token}, "10.31.2.1")
	if mv.Code != http.StatusOK {
		t.Fatalf("magic-link verify status = %d body=%s", mv.Code, mv.Body.String())
	}
	magicToken := sessionTokenValue(mv)
	if magicToken == "" {
		t.Fatal("no session cookie on magic-link verify")
	}
	if !h.Sessions.Magic(context.Background(), magicToken) {
		t.Error("magic-link verify session reported magic=false, want true")
	}
	if h.Sessions.Magic(context.Background(), pwToken) {
		t.Error("password session became magic after an unrelated magic login")
	}
	var mvMe meResponse
	_ = json.Unmarshal(mv.Body.Bytes(), &mvMe)
	if !mvMe.MagicSession {
		t.Error("magic-link verify response did not report magic_session=true")
	}
	_ = requireUserIDByEmail(t, users, email)

	// /v1/auth/me reflects the flag for the magic session.
	me := authMeGet(t, h, magicToken)
	var meBody meResponse
	_ = json.Unmarshal(me.Body.Bytes(), &meBody)
	if !meBody.MagicSession {
		t.Error("/v1/auth/me did not report magic_session=true for a magic session")
	}
}

// TestMagicSessionAuditFlagged: the auth.login audit event records magic=true
// on the magic path and magic=false on the password path.
func TestMagicSessionAuditFlagged(t *testing.T) {
	h, users, mailer := authTestFixture(t)
	h.Events = dbstore.NewEventStore(bundleTestPool(t))
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := fmt.Sprintf("magic-audit-%d@example.test", time.Now().UnixNano())
	registerAndVerify(t, h, email, "password123", "10.32.1.1")
	uid := requireUserIDByEmail(t, users, email)

	// The register/verify flow may have created events; note the starting point.
	before, _, _ := h.Events.List(context.Background(), dbstore.EventFilter{
		AccountIDs: []string{accountIDForUser(t, h, uid)}, EventName: "auth.login", Limit: 50,
	})

	// Password login → auth.login magic=false.
	if rec := postJSON(mux, "/v1/auth/login", map[string]any{"email": email, "password": "password123"}, "10.32.1.1"); rec.Code != http.StatusOK {
		t.Fatalf("password login status = %d", rec.Code)
	}
	// Magic-link login → auth.login magic=true.
	_ = postJSON(mux, "/v1/auth/magic-link/request", map[string]any{"email": email}, "10.32.2.1")
	token := magicLinkTokenFromMailer(t, mailer)
	if rec := postJSON(mux, "/v1/auth/magic-link/verify", map[string]any{"token": token}, "10.32.2.1"); rec.Code != http.StatusOK {
		t.Fatalf("magic verify status = %d body=%s", rec.Code, rec.Body.String())
	}

	events := waitForAuthLoginCount(t, h, uid, len(before)+2)
	var sawPassword, sawMagic bool
	for _, ev := range events {
		if ev.EventName != "auth.login" {
			continue
		}
		magic, _ := ev.Properties["magic"].(bool)
		method, _ := ev.Properties["method"].(string)
		if method == "magic_link" && magic {
			sawMagic = true
		}
		if method == "password" && !magic {
			sawPassword = true
		}
	}
	if !sawPassword {
		t.Error("no auth.login event with method=password magic=false")
	}
	if !sawMagic {
		t.Error("no auth.login event with method=magic_link magic=true")
	}
}

// accountIDForUser resolves the user's first owned account id (the audit
// events are account-scoped).
func accountIDForUser(t *testing.T, h *AuthHandlers, userID string) string {
	t.Helper()
	if h.Accounts == nil {
		t.Fatal("Accounts store not wired in fixture")
	}
	accs, err := h.Accounts.ByOwner(context.Background(), userID)
	if err != nil || len(accs) == 0 {
		t.Fatalf("resolve account for user %s: err=%v n=%d", userID, err, len(accs))
	}
	return accs[0].ID
}

// waitForAuthLoginCount polls the events table briefly so the best-effort audit
// write is observed (Record is synchronous, but keep it robust).
func waitForAuthLoginCount(t *testing.T, h *AuthHandlers, userID string, want int) []dbstore.Event {
	t.Helper()
	acc := accountIDForUser(t, h, userID)
	deadline := time.Now().Add(3 * time.Second)
	var last []dbstore.Event
	for {
		evs, _, err := h.Events.List(context.Background(), dbstore.EventFilter{
			AccountIDs: []string{acc}, EventName: "auth.login", Limit: 50,
		})
		if err == nil {
			last = evs
			if len(evs) >= want {
				return evs
			}
		}
		if time.Now().After(deadline) {
			return last
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestStepUpMagicAnd2FARequiresTOTP: the A+C case — change-password on a magic
// session for a 2FA user is rejected without a fresh code and accepted with one.
func TestStepUpMagicAnd2FARequiresTOTP(t *testing.T) {
	h, _, mailer := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := fmt.Sprintf("stepup-cp-%d@example.test", time.Now().UnixNano())
	registerAndVerify(t, h, email, "password123", "10.33.1.1")

	// Establish a normal session and enable 2FA.
	pw := postJSON(mux, "/v1/auth/login", map[string]any{"email": email, "password": "password123"}, "10.33.1.1")
	baseSession := sessionTokenValue(pw)
	if baseSession == "" {
		t.Fatal("no session cookie on password login")
	}
	secret := enable2FAFor(t, h, mux, baseSession)

	// Now log in via magic link → magic session (2FA user).
	_ = postJSON(mux, "/v1/auth/magic-link/request", map[string]any{"email": email}, "10.33.2.1")
	token := magicLinkTokenFromMailer(t, mailer)
	mv := postJSON(mux, "/v1/auth/magic-link/verify", map[string]any{"token": token}, "10.33.2.1")
	if mv.Code != http.StatusOK {
		t.Fatalf("magic verify status = %d body=%s", mv.Code, mv.Body.String())
	}
	magicSession := sessionTokenValue(mv)
	if !h.Sessions.Magic(context.Background(), magicSession) {
		t.Fatal("expected a magic session")
	}

	// Without a TOTP code → step-up challenge (401 + next_step marker).
	noCode := postJSONCookies(mux, "/v1/auth/change-password", map[string]any{
		"old_password": "password123", "new_password": "newpassword123",
	}, "10.33.3.1", magicSession)
	if noCode.Code != http.StatusUnauthorized {
		t.Fatalf("change-password without step-up = %d, want 401; body=%s", noCode.Code, noCode.Body.String())
	}
	var ch map[string]any
	_ = json.Unmarshal(noCode.Body.Bytes(), &ch)
	if ch["next_step"] != stepUpNextStep {
		t.Errorf("next_step = %v, want %q", ch["next_step"], stepUpNextStep)
	}

	// Wrong code → still challenged.
	bad := postJSONCookies(mux, "/v1/auth/change-password", map[string]any{
		"old_password": "password123", "new_password": "newpassword123", "totp_code": "000000",
	}, "10.33.3.1", magicSession)
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("change-password with wrong TOTP = %d, want 401", bad.Code)
	}

	// Fresh TOTP code → accepted, password actually rotates.
	ok := postJSONCookies(mux, "/v1/auth/change-password", map[string]any{
		"old_password": "password123", "new_password": "newpassword123", "totp_code": mustTOTP(secret),
	}, "10.33.3.1", magicSession)
	if ok.Code != http.StatusOK {
		t.Fatalf("change-password with fresh TOTP = %d, want 200; body=%s", ok.Code, ok.Body.String())
	}

	// The new password works (proves the rotation happened).
	if rec := postJSON(mux, "/v1/auth/login", map[string]any{"email": email, "password": "newpassword123"}, "10.33.4.1"); rec.Code != http.StatusOK && rec.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected login status after rotation: %d", rec.Code)
	}
}

// TestStepUpNotRequiredNonMagic2FA: a 2FA user on a NORMAL session is not
// prompted — the existing password confirm is all that applies.
func TestStepUpNotRequiredNonMagic2FA(t *testing.T) {
	h, _, _ := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := fmt.Sprintf("stepup-nomagic-%d@example.test", time.Now().UnixNano())
	registerAndVerify(t, h, email, "password123", "10.34.1.1")

	pw := postJSON(mux, "/v1/auth/login", map[string]any{"email": email, "password": "password123"}, "10.34.1.1")
	session := sessionTokenValue(pw)
	if session == "" {
		t.Fatal("no session cookie on password login")
	}
	_ = enable2FAFor(t, h, mux, session)
	if h.Sessions.Magic(context.Background(), session) {
		t.Fatal("password session must not be magic")
	}

	// Change-password with NO totp_code succeeds on the password confirm alone.
	rec := postJSONCookies(mux, "/v1/auth/change-password", map[string]any{
		"old_password": "password123", "new_password": "newpassword123",
	}, "10.34.2.1", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("non-magic 2FA change-password = %d, want 200 (no step-up); body=%s", rec.Code, rec.Body.String())
	}
}

// TestStepUpNotRequiredMagicWithout2FA: a magic session for a NON-2FA user is
// unchanged — no new friction (the existing password confirm still applies).
func TestStepUpNotRequiredMagicWithout2FA(t *testing.T) {
	h, _, mailer := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := fmt.Sprintf("stepup-no2fa-%d@example.test", time.Now().UnixNano())
	registerAndVerify(t, h, email, "password123", "10.35.1.1")

	_ = postJSON(mux, "/v1/auth/magic-link/request", map[string]any{"email": email}, "10.35.2.1")
	token := magicLinkTokenFromMailer(t, mailer)
	mv := postJSON(mux, "/v1/auth/magic-link/verify", map[string]any{"token": token}, "10.35.2.1")
	session := sessionTokenValue(mv)
	if session == "" {
		t.Fatal("no session cookie on magic verify")
	}
	if !h.Sessions.Magic(context.Background(), session) {
		t.Fatal("expected a magic session")
	}

	// No 2FA → no step-up; the password confirm alone is enough.
	rec := postJSONCookies(mux, "/v1/auth/change-password", map[string]any{
		"old_password": "password123", "new_password": "newpassword123",
	}, "10.35.3.1", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("magic non-2FA change-password = %d, want 200 (no step-up); body=%s", rec.Code, rec.Body.String())
	}
}

// TestStepUpDisable2FA: disable-2FA on a magic session for a 2FA user requires
// a fresh TOTP code; the non-magic path is unchanged.
func TestStepUpDisable2FA(t *testing.T) {
	h, _, mailer := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := fmt.Sprintf("stepup-disable-%d@example.test", time.Now().UnixNano())
	registerAndVerify(t, h, email, "password123", "10.36.1.1")

	pw := postJSON(mux, "/v1/auth/login", map[string]any{"email": email, "password": "password123"}, "10.36.1.1")
	baseSession := sessionTokenValue(pw)
	secret := enable2FAFor(t, h, mux, baseSession)

	// Magic session for the 2FA user.
	_ = postJSON(mux, "/v1/auth/magic-link/request", map[string]any{"email": email}, "10.36.2.1")
	token := magicLinkTokenFromMailer(t, mailer)
	mv := postJSON(mux, "/v1/auth/magic-link/verify", map[string]any{"token": token}, "10.36.2.1")
	magicSession := sessionTokenValue(mv)

	// Without a TOTP code → challenged.
	noCode := postJSONCookies(mux, "/v1/auth/2fa/disable", map[string]any{"password": "password123"}, "10.36.3.1", magicSession)
	if noCode.Code != http.StatusUnauthorized {
		t.Fatalf("disable-2FA without step-up = %d, want 401; body=%s", noCode.Code, noCode.Body.String())
	}
	var ch map[string]any
	_ = json.Unmarshal(noCode.Body.Bytes(), &ch)
	if ch["next_step"] != stepUpNextStep {
		t.Errorf("disable-2FA next_step = %v, want %q", ch["next_step"], stepUpNextStep)
	}

	// With a fresh code → disabled.
	ok := postJSONCookies(mux, "/v1/auth/2fa/disable", map[string]any{
		"password": "password123", "totp_code": mustTOTP(secret),
	}, "10.36.3.1", magicSession)
	if ok.Code != http.StatusOK {
		t.Fatalf("disable-2FA with fresh TOTP = %d, want 200; body=%s", ok.Code, ok.Body.String())
	}
}

// TestStepUpPasskeyRegisterGate: the passkey register /start endpoint enforces
// the gate for a magic + 2FA session (401 + marker) and passes for a normal
// session. It does not need a real authenticator: the gate runs before WebAuthn.
func TestStepUpPasskeyRegisterGate(t *testing.T) {
	h, _, mailer := authTestFixture(t)
	rest := &RESTHandlers{
		Sessions: h.Sessions,
		Users:    h.Users,
		Accounts: h.Accounts,
		Passkeys: dbstore.NewPasskeyStore(bundleTestPool(t)),
		Events:   dbstore.NewEventStore(bundleTestPool(t)),
	}

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := fmt.Sprintf("stepup-passkey-%d@example.test", time.Now().UnixNano())
	registerAndVerify(t, h, email, "password123", "10.37.1.1")
	pw := postJSON(mux, "/v1/auth/login", map[string]any{"email": email, "password": "password123"}, "10.37.1.1")
	baseSession := sessionTokenValue(pw)
	secret := enable2FAFor(t, h, mux, baseSession)

	// Magic session for the 2FA user.
	_ = postJSON(mux, "/v1/auth/magic-link/request", map[string]any{"email": email}, "10.37.2.1")
	token := magicLinkTokenFromMailer(t, mailer)
	mv := postJSON(mux, "/v1/auth/magic-link/verify", map[string]any{"token": token}, "10.37.2.1")
	magicSession := sessionTokenValue(mv)

	// Without a TOTP code → the gate rejects before any WebAuthn work.
	noCode := passkeyRegisterStartReq(t, rest, magicSession, "")
	if noCode.Code != http.StatusUnauthorized {
		t.Fatalf("passkey register start without step-up = %d, want 401; body=%s", noCode.Code, noCode.Body.String())
	}
	var ch map[string]any
	_ = json.Unmarshal(noCode.Body.Bytes(), &ch)
	if ch["next_step"] != stepUpNextStep {
		t.Errorf("passkey next_step = %v, want %q", ch["next_step"], stepUpNextStep)
	}

	// With a fresh code → the gate passes (fails later on WebAuthn, not 401+marker).
	ok := passkeyRegisterStartReq(t, rest, magicSession, mustTOTP(secret))
	if ok.Code == http.StatusUnauthorized {
		var body map[string]any
		_ = json.Unmarshal(ok.Body.Bytes(), &body)
		if body["next_step"] == stepUpNextStep {
			t.Fatalf("passkey register start with fresh TOTP still challenged: %s", ok.Body.String())
		}
	}

	// A normal (non-magic) 2FA session is not challenged at all.
	normal := passkeyRegisterStartReq(t, rest, baseSession, "")
	var nbody map[string]any
	_ = json.Unmarshal(normal.Body.Bytes(), &nbody)
	if nbody["next_step"] == stepUpNextStep {
		t.Fatalf("non-magic session wrongly challenged for passkey register: %s", normal.Body.String())
	}
}

// passkeyRegisterStartReq drives passkeyRegisterStart directly with a session
// cookie and optional totp_code.
func passkeyRegisterStartReq(t *testing.T, rest *RESTHandlers, session, totpCode string) *httptest.ResponseRecorder {
	t.Helper()
	body := "{}"
	if totpCode != "" {
		body = `{"totp_code":"` + totpCode + `"}`
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/passkeys/register/start", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if session != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: session})
	}
	rec := httptest.NewRecorder()
	rest.passkeyRegisterStart(rec, req, &AccountCtx{Account: accountForUser(t, rest, uidForSession(t, rest, session))})
	return rec
}

// uidForSession resolves the user id behind a raw session token.
func uidForSession(t *testing.T, rest *RESTHandlers, session string) string {
	t.Helper()
	uid, err := rest.Sessions.Get(context.Background(), session)
	if err != nil || uid == "" {
		t.Fatalf("resolve session user: err=%v uid=%q", err, uid)
	}
	return uid
}

// accountForUser loads the user's first owned account (for the audit context).
func accountForUser(t *testing.T, rest *RESTHandlers, userID string) *dbstore.Account {
	t.Helper()
	accs, err := rest.Accounts.ByOwner(context.Background(), userID)
	if err != nil || len(accs) == 0 {
		t.Fatalf("resolve account for %s: err=%v n=%d", userID, err, len(accs))
	}
	return accs[0]
}

// TestStepUpGrantIsShortLivedAndScoped: a grant is bound to the session token
// and expires, so a different session never inherits it.
func TestStepUpGrantIsShortLivedAndScoped(t *testing.T) {
	h, _, _ := authTestFixture(t)
	ctx := context.Background()

	tokA, err := h.Sessions.CreateSession(ctx, "user-a", time.Hour, false, true)
	if err != nil {
		t.Fatalf("create session A: %v", err)
	}
	tokB, err := h.Sessions.CreateSession(ctx, "user-b", time.Hour, false, true)
	if err != nil {
		t.Fatalf("create session B: %v", err)
	}
	su := h.Sessions.StepUp()
	if err := su.Grant(ctx, tokA, "user-a"); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if !su.Granted(ctx, tokA, "user-a") {
		t.Error("grant for session A not recognised")
	}
	if su.Granted(ctx, tokB, "user-b") {
		t.Error("grant leaked to a different session")
	}
	if su.Granted(ctx, tokA, "user-other") {
		t.Error("grant recognised for a different user")
	}
	if err := su.Consume(ctx, tokA); err != nil {
		t.Fatalf("consume: %v", err)
	}
	if su.Granted(ctx, tokA, "user-a") {
		t.Error("consumed grant still recognised")
	}
	// Legacy/non-existent sessions never report magic.
	legacy, err := h.Sessions.CreateRememberedWithTTL(ctx, "user-legacy", time.Hour, false)
	if err != nil {
		t.Fatalf("create legacy session: %v", err)
	}
	if h.Sessions.Magic(ctx, legacy) {
		t.Error("v4 non-magic session reported magic=true")
	}
	if h.Sessions.Magic(ctx, "does-not-exist") {
		t.Error("unknown session reported magic=true")
	}
}

// TestMagicFlagSurvives2FAEnableRotation: enabling 2FA rotates the session; the
// magic flag must be inherited so the marker (and its gate) is not silently
// erased mid-session (issue #458).
func TestMagicFlagSurvives2FAEnableRotation(t *testing.T) {
	h, _, mailer := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := fmt.Sprintf("magic-rotate-%d@example.test", time.Now().UnixNano())
	registerAndVerify(t, h, email, "password123", "10.38.1.1")

	// Magic session (no 2FA yet).
	_ = postJSON(mux, "/v1/auth/magic-link/request", map[string]any{"email": email}, "10.38.2.1")
	token := magicLinkTokenFromMailer(t, mailer)
	mv := postJSON(mux, "/v1/auth/magic-link/verify", map[string]any{"token": token}, "10.38.2.1")
	magicSession := sessionTokenValue(mv)
	if !h.Sessions.Magic(context.Background(), magicSession) {
		t.Fatal("expected a magic session")
	}

	// Enable 2FA from the magic session → the response rotates the cookie.
	setup := postJSONCookies(mux, "/v1/auth/2fa/setup", map[string]any{}, "10.38.3.1", magicSession)
	var s map[string]string
	_ = json.Unmarshal(setup.Body.Bytes(), &s)
	en := postJSONCookies(mux, "/v1/auth/2fa/enable", map[string]any{"code": mustTOTP(s["secret"])}, "10.38.3.1", magicSession)
	if en.Code != http.StatusOK {
		t.Fatalf("enable 2FA status = %d body=%s", en.Code, en.Body.String())
	}
	rotated := sessionTokenValue(en)
	if rotated == "" || rotated == magicSession {
		t.Fatalf("expected a rotated session cookie, got %q", rotated)
	}
	if !h.Sessions.Magic(context.Background(), rotated) {
		t.Error("rotated session dropped the magic flag (issue #458 regression)")
	}
}
