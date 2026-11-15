package httpapi

// The platform-admin refusal at the CONSENT step (card t_c94bb976).
//
// THE PROMISE THIS PINS. /docs/mcp/connect tells an operator that a
// platform-admin account cannot use the device grant: no code arrives, and
// Approve answers 403 ("not valid or has expired"). Both halves are real, and
// each is enforced in a different place:
//
//	mail step      oauth_device.go:187-192   — never mail the code
//	               pinned by TestDeviceAuthorizationNeverMailsSuperAdmin
//	               (oauth_device_test.go, in-memory fake)
//	consent step   oauth_device.go postDeviceConsent — refuse the approval even
//	               for a code somebody typed by hand
//	               pinned HERE, on the DB-backed server
//
// Only the first half had a test. The second could be deleted and the whole
// suite stayed green while the public page kept promising "Approve answers
// 403" — which is what this file closes.
//
// WHY THE DB-BACKED SERVER AND NOT THE IN-MEMORY FAKE. The refusal runs before
// the decision switch, so the interesting state is not the response alone: the
// authorisation row has already been created UNBOUND (nothing was mailed, so no
// user_id was recorded) and the consent card has to keep rendering it as
// pending — a person typing the code by hand is the documented fallback. A fake
// store cannot show that the row a real deployment writes is the row this
// refusal is handed.
//
// The class itself comes from cfg.SuperAdminEmails (newDeviceFlowEnvWith), i.e.
// the same config → config.IsSuperAdmin → handler path production uses, not a
// handler field pinned by hand.
//
// Integration test: needs JOMFORM_TEST_DATABASE_URL and JOMFORM_TEST_VALKEY_URL;
// skipped when either is unset.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/oauthmeta"
)

// TestDeviceApproveRefusedForPlatformAdmin: the consent step refuses a
// platform-admin account, with the machine code the dashboard reads, and the
// authorisation it refuses stays unapproved and untokened.
func TestDeviceApproveRefusedForPlatformAdmin(t *testing.T) {
	env := newDeviceFlowEnvWith(t, deviceFlowEnvOpts{platformAdmin: true})
	ctx := context.Background()
	clientID := env.registerDeviceClient(t, "claude-desktop-admin")

	// The same 200 an ordinary account gets: the refusal must not turn the
	// open endpoint into an enumeration oracle.
	start := env.startDevice(t, clientID, oauthmeta.ScopeRead, env.email)
	if start.UserCode == "" || start.DeviceCode == "" {
		t.Fatalf("device_authorization returned no code pair: %+v", start)
	}

	// The row exists and is UNBOUND. That is what "no code arrives" means in the
	// database, and it is why the consent card still renders for a typed code.
	var boundUser, approved string
	if err := env.pool.QueryRow(ctx,
		`SELECT COALESCE(user_id::text, ''), COALESCE(approved_at::text, '')
		   FROM oauth_device_codes WHERE user_code_hash = $1`,
		hashToken(start.UserCode)).Scan(&boundUser, &approved); err != nil {
		t.Fatalf("read the device row: %v", err)
	}
	if boundUser != "" {
		t.Fatalf("the device row is bound to user %q — a platform admin was mailed a code", boundUser)
	}
	if approved != "" {
		t.Fatalf("the device row was already approved: %s", approved)
	}

	// The consent card renders it as pending, so the dashboard's "type the code"
	// path is real rather than a 404 for these accounts.
	resp, raw := env.do(t, env.authed(t, "GET",
		"/v1/auth/connected-apps/device?user_code="+url.QueryEscape(start.UserCode), nil))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent card = %d body=%s, want 200 (the row exists)", resp.StatusCode, raw)
	}
	var consent deviceConsentOut
	if err := json.Unmarshal(raw, &consent); err != nil {
		t.Fatalf("decode consent card %s: %v", raw, err)
	}
	if consent.State != "pending" || consent.Client != "claude-desktop-admin" {
		t.Fatalf("consent card = %+v, want pending / claude-desktop-admin", consent)
	}

	// THE GUARD: approve answers 403, and says so with the machine code the
	// dashboard switches on. 403 alone is not enough — a 401/404 would also be
	// "not 200", and the public page names the refusal exactly.
	resp, raw = env.do(t, env.authed(t, "POST", "/v1/auth/connected-apps/device",
		strings.NewReader(`{"user_code":"`+start.UserCode+`","decision":"approve"}`)))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("approve = %d body=%s, want 403", resp.StatusCode, raw)
	}
	var refused struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal(raw, &refused); err != nil {
		t.Fatalf("decode refusal %s: %v", raw, err)
	}
	if refused.Code != "request.forbidden" {
		t.Fatalf("refusal code = %q, want request.forbidden (body=%s)", refused.Code, raw)
	}
	for _, want := range []string{"platform-admin accounts", "browser flow"} {
		if !strings.Contains(refused.Error, want) {
			t.Fatalf("refusal prose %q does not name %q", refused.Error, want)
		}
	}

	// Nothing was approved and nothing was issued: the harness keeps polling a
	// code that will expire, which is exactly what the docs page warns about.
	if err := env.pool.QueryRow(ctx,
		`SELECT COALESCE(approved_at::text, '') FROM oauth_device_codes WHERE user_code_hash = $1`,
		hashToken(start.UserCode)).Scan(&approved); err != nil {
		t.Fatalf("re-read the device row: %v", err)
	}
	if approved != "" {
		t.Fatalf("a refused approval still wrote approved_at = %s", approved)
	}
	code, poll := env.pollToken(t, clientID, start.DeviceCode)
	if code != http.StatusBadRequest || poll["error"] != "authorization_pending" {
		t.Fatalf("poll after the refusal = %d %v, want 400 authorization_pending", code, poll)
	}

	// And no token exists for the account: the refusal is the last point before
	// one would, so a bug that wrote the token and still answered 403 would be
	// invisible in the response.
	var tokens int
	if err := env.pool.QueryRow(ctx,
		`SELECT count(*) FROM oauth_tokens WHERE user_id = $1`, env.userID).Scan(&tokens); err != nil {
		t.Fatalf("count tokens: %v", err)
	}
	if tokens != 0 {
		t.Fatalf("a refused approval left %d token(s) behind", tokens)
	}
}
