package httpapi

// The code a person TYPES must resolve to the authorisation that was mailed to
// them (card t_4f3ab16e).
//
// THE DEFECT this half guards: the dashboard's consent card is RFC 8628's
// verification endpoint, but its code input only existed once `?user_code=` was
// already in the URL — so the typed-code path was never exercised by anything,
// and the shapes a person actually produces ("kx9mx5bs", " KX9M-X5BS ",
// "KX9MX5BS") were never resolved against a real row either. The web half
// (web/src/app/profile/page.tsx + src/lib/deviceCode.ts) now always renders the
// field; this half proves the SERVER accepts what that field will send.
//
// The server owns the rule (normalizeUserCode: upper-case, strip `-`/space/tab,
// require 8 characters, re-insert the canonical dash before hashing). This test
// drives the REAL handler against a REAL authorisation row, so "the shapes
// resolve to the same row" is a statement about the SQL lookup, not about a
// string function: every shape below must return the same `user_code`, the same
// client and the same state, and the approve write must land on that same row.
//
// Integration test: needs JOMFORM_TEST_DATABASE_URL and JOMFORM_TEST_VALKEY_URL
// (newDeviceFlowEnv skips when they are unset).
import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/oauthmeta"
)

func TestTypedDeviceCodeShapesResolveToTheMailedRow(t *testing.T) {
	env := newDeviceFlowEnv(t)
	clientID := env.registerDeviceClient(t, "typed-code-shapes")
	start := env.startDevice(t, clientID, oauthmeta.ScopeRead, env.email)
	_ = env.mail.wait(t)

	bare := strings.ReplaceAll(start.UserCode, "-", "")
	if len(bare) != deviceUserCodeLen {
		t.Fatalf("the issued user code %q is not the documented shape", start.UserCode)
	}

	// What a person produces: the mailed form, the lower-cased form a phone
	// keyboard gives, a copy-paste with the surrounding spaces, no dash at all,
	// and a space where the dash was.
	shapes := []string{
		start.UserCode,
		strings.ToLower(start.UserCode),
		"  " + strings.ToLower(bare) + "  ",
		strings.ToUpper(bare),
		strings.ReplaceAll(start.UserCode, "-", " "),
		"\t" + start.UserCode + "\t",
	}
	for _, shape := range shapes {
		t.Run(shape, func(t *testing.T) {
			resp, raw := env.do(t, env.authed(t, "GET",
				"/v1/auth/connected-apps/device?user_code="+url.QueryEscape(shape), nil))
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("shape %q was refused (%d): %s — a person retyping the code they were shown must not be told it is invalid", shape, resp.StatusCode, raw)
			}
			var out deviceConsentOut
			if err := json.Unmarshal(raw, &out); err != nil {
				t.Fatalf("decode %s: %v", raw, err)
			}
			if out.UserCode != start.UserCode {
				t.Fatalf("shape %q resolved to %q, want the mailed %q", shape, out.UserCode, start.UserCode)
			}
			if out.State != "pending" || out.Client != "typed-code-shapes" {
				t.Fatalf("shape %q resolved to another authorisation: %+v", shape, out)
			}
		})
	}

	// The decision path normalises too — otherwise the card could SHOW a
	// resolution and then fail to act on it, which is the worse half of the bug.
	resp, raw := env.do(t, env.authed(t, "POST", "/v1/auth/connected-apps/device",
		strings.NewReader(`{"user_code":"`+strings.ToLower(bare)+`","decision":"approve"}`)))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("approve with a lower-case, dashless code = %d body=%s", resp.StatusCode, raw)
	}

	// One row changed state, and it is the one every shape resolved to.
	resp, raw = env.do(t, env.authed(t, "GET",
		"/v1/auth/connected-apps/device?user_code="+url.QueryEscape(" "+start.UserCode+" "), nil))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("re-read after approve = %d body=%s", resp.StatusCode, raw)
	}
	var after deviceConsentOut
	if err := json.Unmarshal(raw, &after); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	if after.State != "approved" {
		t.Fatalf("state after approve = %q, want approved (the shapes did not all reach one row)", after.State)
	}

	// Rejecting what is genuinely unknown is still a rejection: this is the only
	// honest answer to give, and the page renders it as "not valid or expired".
	resp, _ = env.do(t, env.authed(t, "GET", "/v1/auth/connected-apps/device?user_code=ZZZZ-ZZZZ", nil))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("an unknown code = %d, want 404", resp.StatusCode)
	}
}

// TestExpiredDeviceCodeIsReportedAsExpiredNotAsUnknown pins the state the card's
// "this code has expired — ask your agent for a new one" copy depends on. If the
// store filtered expired rows out, an expired code would come back as 404
// "not valid" and the page could never tell the two apart — the reader would be
// told to get a new code without being told WHY the one they have stopped
// working.
func TestExpiredDeviceCodeIsReportedAsExpiredNotAsUnknown(t *testing.T) {
	env := newDeviceFlowEnv(t)
	clientID := env.registerDeviceClient(t, "expired-code-shape")
	start := env.startDevice(t, clientID, oauthmeta.ScopeRead, env.email)
	_ = env.mail.wait(t)

	if _, err := env.pool.Exec(context.Background(),
		`UPDATE oauth_device_codes SET expires_at = now() - interval '1 minute' WHERE user_code_hash = $1`,
		hashToken(start.UserCode)); err != nil {
		t.Fatalf("expire the row: %v", err)
	}

	resp, raw := env.do(t, env.authed(t, "GET",
		"/v1/auth/connected-apps/device?user_code="+url.QueryEscape(start.UserCode), nil))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("an expired code = %d body=%s, want 200 with state=expired", resp.StatusCode, raw)
	}
	var out deviceConsentOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	if out.State != "expired" {
		t.Fatalf("state = %q, want expired", out.State)
	}
}
