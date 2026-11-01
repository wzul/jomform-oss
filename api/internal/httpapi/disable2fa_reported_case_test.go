// Regression test for the reported defect: disabling 2FA from /profile did
// nothing for a user who typed the 6-digit code their authenticator showed.
//
// WHAT WAS BROKEN
//
//	The Profile → Security control prompted with the label "Enter a current 2FA
//	code to disable:" and sent the answer as {"password": "<the code>"}. The
//	backend had (since #458) required a fresh `totp_code` on a magic-link
//	session for a 2FA user and otherwise verified `password` against the
//	account hash — so a magic-link user's code was checked as a password and
//	rejected with 401, for ever, with 2FA still enabled. Every 401 in the
//	reported window was that rejection.
//
// WHAT THIS PINS
//  1. A magic-link session + 2FA + a current code in `totp_code` → 200, the
//     code actually disables 2FA (users.totp_enabled false, secret cleared),
//     every trusted device is revoked and the trust cookie is cleared.
//  2. The code is STILL the second factor: without one the same session is
//     challenged (401 + next_step), and the account password is NOT accepted
//     as a substitute on a magic session (it never proved a factor, and a
//     magic-link signup has a random password the user has never seen).
//  3. #528 is intact — the code is single-use: replaying the same code in a
//     second disable attempt is refused.
//  4. The non-magic 2FA path is unchanged: the account password disables (the
//     REST/MCP shape), and with no proof at all it is refused.
//
// Needs a real Postgres (JOMFORM_TEST_DATABASE_URL) + Valkey
// (JOMFORM_TEST_VALKEY_URL); skipped otherwise.
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// disable2FAResult reads the user's 2FA columns straight from the database, so
// the assertion is about what actually happened rather than about the handler's
// own response body.
func twoFAStateFromDB(t *testing.T, userID string) (secret string, enabled bool) {
	t.Helper()
	pool := bundleTestPool(t)
	if err := pool.QueryRow(context.Background(),
		`SELECT COALESCE(totp_secret,''), totp_enabled FROM users WHERE id=$1`, userID).
		Scan(&secret, &enabled); err != nil {
		t.Fatalf("read 2FA state for %s: %v", userID, err)
	}
	return secret, enabled
}

// TestDisable2FAWithCurrentCodeOnMagicSession is the reported case, end to end
// through the real mux: magic-link session + 2FA + a current code.
func TestDisable2FAWithCurrentCodeOnMagicSession(t *testing.T) {
	ip := testIP(t)
	h, _, mailer := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := fmt.Sprintf("disable2fa-magic-%d@%s", time.Now().UnixNano(), testMailDomain)
	registerAndVerify(t, h, email, "password123", ip)

	// Establish a non-magic session and enable 2FA on it.
	pw := postJSON(mux, "/v1/auth/login", map[string]any{"email": email, "password": "password123"}, ip)
	baseSession := sessionTokenValue(pw)
	if baseSession == "" {
		t.Fatal("no session cookie on password login")
	}
	secret := enable2FAFor(t, h, mux, baseSession)
	uid := requireUserIDByEmail(t, h.Users, email)

	// A trusted device exists before the disable, so the revocation assertion
	// below is not vacuous.
	trustToken, err := h.Sessions.TrustedDevices().IssueDevice(context.Background(), uid, "test-ua", "10.40.9.9")
	if err != nil {
		t.Fatalf("issue trusted device: %v", err)
	}
	if h.Sessions.TrustedDevices().UserIDFor(context.Background(), trustToken) != uid {
		t.Fatal("trusted device was not issued — the revocation check would be vacuous")
	}

	// Magic-link session for the same (2FA) user — the session shape Wan had.
	_ = postJSON(mux, "/v1/auth/magic-link/request", map[string]any{"email": email}, ip)
	token := magicLinkTokenFromMailer(t, mailer)
	mv := postJSON(mux, "/v1/auth/magic-link/verify", map[string]any{"token": token}, ip)
	if mv.Code != http.StatusOK {
		t.Fatalf("magic verify status = %d body=%s", mv.Code, mv.Body.String())
	}
	magicSession := sessionTokenValue(mv)
	if !h.Sessions.Magic(context.Background(), magicSession) {
		t.Fatal("expected a magic session")
	}

	// The user types the code from their authenticator app and submits it.
	code := mustTOTP(secret)
	rec := postJSONCookies(mux, "/v1/auth/2fa/disable",
		map[string]any{"totp_code": code}, ip, magicSession)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable 2FA with a current code = %d, want 200 (this is the reported defect); body=%s",
			rec.Code, rec.Body.String())
	}

	// 2FA is really off — read the database, not the response body.
	dbSecret, dbEnabled := twoFAStateFromDB(t, uid)
	if dbEnabled || dbSecret != "" {
		t.Fatalf("after disable: enabled=%v secret=%q, want false/\"\"", dbEnabled, dbSecret)
	}

	// Every trusted device is gone and the trust cookie was cleared.
	if got := h.Sessions.TrustedDevices().UserIDFor(context.Background(), trustToken); got != "" {
		t.Errorf("trusted device still valid after disable (user %q) — it would still waive the removed 2FA step", got)
	}
	// The trust cookie must actually be cleared. An empty Value alone is not
	// evidence (an ABSENT cookie reads the same), so require the Set-Cookie
	// that clears it (MaxAge < 0 → the browser drops it).
	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == trustCookie && c.Value == "" && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Errorf("disable did not send a clearing Set-Cookie for %s (headers: %v)", trustCookie, rec.Header().Values("Set-Cookie"))
	}
}

// TestDisable2FAStillNeedsASecondFactorOnMagicSession: the fix must not have
// loosened #458. A magic + 2FA session without a code is challenged, and the
// account password is NOT a substitute on that session.
func TestDisable2FAStillNeedsASecondFactorOnMagicSession(t *testing.T) {
	ip := testIP(t)
	h, _, mailer := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := fmt.Sprintf("disable2fa-guard-%d@%s", time.Now().UnixNano(), testMailDomain)
	registerAndVerify(t, h, email, "password123", ip)

	pw := postJSON(mux, "/v1/auth/login", map[string]any{"email": email, "password": "password123"}, ip)
	baseSession := sessionTokenValue(pw)
	_ = enable2FAFor(t, h, mux, baseSession)
	uid := requireUserIDByEmail(t, h.Users, email)

	_ = postJSON(mux, "/v1/auth/magic-link/request", map[string]any{"email": email}, ip)
	token := magicLinkTokenFromMailer(t, mailer)
	mv := postJSON(mux, "/v1/auth/magic-link/verify", map[string]any{"token": token}, ip)
	magicSession := sessionTokenValue(mv)

	// (a) No proof at all → the #458 challenge.
	none := postJSONCookies(mux, "/v1/auth/2fa/disable", map[string]any{}, ip, magicSession)
	if none.Code != http.StatusUnauthorized {
		t.Fatalf("disable with no proof = %d, want 401; body=%s", none.Code, none.Body.String())
	}
	var ch map[string]any
	_ = json.Unmarshal(none.Body.Bytes(), &ch)
	if ch["next_step"] != stepUpNextStep {
		t.Errorf("next_step = %v, want %q (the client needs the marker to prompt)", ch["next_step"], stepUpNextStep)
	}

	// (b) The CORRECT account password is still not enough on a magic session:
	// it proves nothing that the stolen inbox did not already prove.
	pwd := postJSONCookies(mux, "/v1/auth/2fa/disable",
		map[string]any{"password": "password123"}, ip, magicSession)
	if pwd.Code != http.StatusUnauthorized {
		t.Fatalf("disable with the account password on a magic session = %d, want 401; body=%s",
			pwd.Code, pwd.Body.String())
	}
	if _, enabled := twoFAStateFromDB(t, uid); !enabled {
		t.Fatal("2FA was disabled by a password alone on a magic session — the #458 gate is broken")
	}

	// (c) A wrong code is refused too, and changes nothing.
	bad := postJSONCookies(mux, "/v1/auth/2fa/disable",
		map[string]any{"totp_code": "000000"}, ip, magicSession)
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("disable with a wrong code = %d, want 401; body=%s", bad.Code, bad.Body.String())
	}
	if _, enabled := twoFAStateFromDB(t, uid); !enabled {
		t.Fatal("a wrong code disabled 2FA")
	}
}

// TestDisable2FACodeIsSingleUse: #528 must survive this path — the code that
// disabled 2FA cannot be replayed once 2FA is armed again.
func TestDisable2FACodeIsSingleUse(t *testing.T) {
	ip := testIP(t)
	h, _, _ := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := fmt.Sprintf("disable2fa-replay-%d@example.test", time.Now().UnixNano())
	registerAndVerify(t, h, email, "password123", ip)

	pw := postJSON(mux, "/v1/auth/login", map[string]any{"email": email, "password": "password123"}, ip)
	session := sessionTokenValue(pw)
	secret := enable2FAFor(t, h, mux, session)
	uid := requireUserIDByEmail(t, h.Users, email)

	code := mustTOTP(secret)
	first := postJSONCookies(mux, "/v1/auth/2fa/disable", map[string]any{"totp_code": code}, ip, session)
	if first.Code != http.StatusOK {
		t.Fatalf("first disable = %d, want 200; body=%s", first.Code, first.Body.String())
	}

	// Re-arm 2FA with the SAME secret so the replay below is refused by the
	// single-use guard and not merely by a rotated secret. The write goes
	// through the store directly on purpose: no production path re-arms the
	// secret it just cleared (setup always mints a new one), so the state this
	// assertion needs is unreachable from a request — enabling it again via the
	// endpoint would change the secret and make the test pass for the wrong
	// reason.
	if err := h.Users.EnableTOTP(context.Background(), uid, secret); err != nil {
		t.Fatalf("re-arm 2FA: %v", err)
	}
	if _, enabled := twoFAStateFromDB(t, uid); !enabled {
		t.Fatal("2FA was not re-armed — the replay assertion would be vacuous")
	}

	// Replay the SAME code: it must be refused, and 2FA must stay on.
	replay := postJSONCookies(mux, "/v1/auth/2fa/disable", map[string]any{"totp_code": code}, ip, session)
	if replay.Code != http.StatusUnauthorized {
		t.Fatalf("replaying the consumed code = %d, want 401 (single-use, issue #528); body=%s",
			replay.Code, replay.Body.String())
	}
	if _, enabled := twoFAStateFromDB(t, uid); !enabled {
		t.Fatal("a replayed code disabled 2FA — issue #528 is broken on this path")
	}
}

// TestDisable2FAPasswordPathUnchanged: a non-magic 2FA session still disables
// with the account password (the REST/MCP contract), and with neither a code
// nor a wrong password it is refused.
func TestDisable2FAPasswordPathUnchanged(t *testing.T) {
	ip := testIP(t)
	h, _, _ := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := fmt.Sprintf("disable2fa-pw-%d@example.test", time.Now().UnixNano())
	registerAndVerify(t, h, email, "password123", ip)

	pw := postJSON(mux, "/v1/auth/login", map[string]any{"email": email, "password": "password123"}, ip)
	session := sessionTokenValue(pw)
	if session == "" {
		t.Fatal("no session cookie on password login")
	}
	_ = enable2FAFor(t, h, mux, session)
	uid := requireUserIDByEmail(t, h.Users, email)

	// Wrong password → 401, nothing changes.
	bad := postJSONCookies(mux, "/v1/auth/2fa/disable", map[string]any{"password": "WRONG"}, ip, session)
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("disable with a wrong password = %d, want 401; body=%s", bad.Code, bad.Body.String())
	}
	if _, enabled := twoFAStateFromDB(t, uid); !enabled {
		t.Fatal("a wrong password disabled 2FA")
	}

	// Correct password → 200.
	ok := postJSONCookies(mux, "/v1/auth/2fa/disable", map[string]any{"password": "password123"}, ip, session)
	if ok.Code != http.StatusOK {
		t.Fatalf("disable with the account password = %d, want 200 (unchanged non-magic path); body=%s",
			ok.Code, ok.Body.String())
	}
	if _, enabled := twoFAStateFromDB(t, uid); enabled {
		t.Fatal("2FA still enabled after a successful disable")
	}
}
