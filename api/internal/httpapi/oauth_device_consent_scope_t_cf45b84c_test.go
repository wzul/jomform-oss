package httpapi

// The access level a PERSON picks on the RFC 8628 consent card — end to end
// (card t_cf45b84c).
//
// THE DEFECT. The consent card rendered the client's requested scope as text and
// offered only Approve / Deny, so the grant was whatever the harness had asked
// for: an agent that requested `read` stayed read-only and had to be re-granted
// the first time it needed to write, and the only route to change it afterwards
// (Connected apps → scope) ROTATES the credential and forces the harness to
// re-authenticate. Wan: "default scope tu jgn read only. bagi semua supaya user
// tak perlu grant banyak kali."
//
// WHAT THIS FILE PROVES, over HTTP against the real store and the real token
// endpoint:
//
//	1. an explicit `scope` on the approval mints a token carrying EXACTLY that
//	   scope — read→readwrite (the widening the card exists for) and
//	   readwrite→read (the cautious direction), both in one call;
//	2. the choice is stored on the DEVICE-CODE row and on the CONNECTED-APP
//	   grant (the oauth_tokens row the harness's bearer token belongs to);
//	3. an omitted `scope` grants the client's requested scope — the behaviour
//	   every dashboard before this card produced, unchanged;
//	4. a scope outside what this deployment can issue is REFUSED with a machine
//	   code and the authorisation is left pending (not burned, not clamped);
//	5. the card's GET advertises the two choices from the server.
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

// clearDeviceRateWindow moves every existing device-authorisation row out of the
// per-IP hourly window.
//
// WHY IT IS NEEDED. deviceRateLimitPerHour is 3 and it counts ROWS BY IP across
// the whole schema, not per user — and every test in this package reaches the
// listener from 127.0.0.1. This file starts more authorisations than that on
// purpose (one per scope case). Backdating `created_at` rather than deleting the
// rows keeps them available to any assertion that reads a row back, and the
// package's tests run sequentially, so no live authorisation is disturbed.
// (Measured: without this, the fourth POST answers 429 and the failure looks
// like a scope bug.)
func (e *deviceFlowEnv) clearDeviceRateWindow(t *testing.T) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(),
		`UPDATE oauth_device_codes SET created_at = now() - interval '2 hours'`); err != nil {
		t.Fatalf("age device-code rows out of the rate window: %v", err)
	}
}

// approve posts the dashboard's consent decision. `body` is the JSON object, so
// a test can send the legacy shape (no scope) or an explicit one.
func (e *deviceFlowEnv) approve(t *testing.T, body map[string]any) (*http.Response, []byte) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal consent body: %v", err)
	}
	return e.do(t, e.authed(t, "POST", "/v1/auth/connected-apps/device", strings.NewReader(string(raw))))
}

// consentCard fetches the GET half the card renders from.
func (e *deviceFlowEnv) consentCard(t *testing.T, userCode string) deviceConsentOut {
	t.Helper()
	resp, raw := e.do(t, e.authed(t, "GET", "/v1/auth/connected-apps/device?user_code="+url.QueryEscape(userCode), nil))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent card = %d body=%s", resp.StatusCode, raw)
	}
	var out deviceConsentOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode consent card %s: %v", raw, err)
	}
	return out
}

// storedDeviceScope reads the scope actually PERSISTED on the device-code row.
func (e *deviceFlowEnv) storedDeviceScope(t *testing.T, userCode string) []string {
	t.Helper()
	var scope []string
	if err := e.pool.QueryRow(context.Background(),
		`SELECT scope FROM oauth_device_codes WHERE user_code_hash = $1`,
		hashToken(userCode)).Scan(&scope); err != nil {
		t.Fatalf("read stored device scope: %v", err)
	}
	return scope
}

// TestDeviceConsentGrantsTheScopeThePersonPicked is the card's acceptance, case
// by case: each row starts a real device authorisation asking for one scope,
// approves it with an explicit or omitted `scope`, then polls the token endpoint
// and asserts the credential the harness actually receives.
func TestDeviceConsentGrantsTheScopeThePersonPicked(t *testing.T) {
	cases := []struct {
		name      string
		requested string
		// sent is the `scope` field on the approval. nil = the field is OMITTED,
		// which is the legacy dashboard's body.
		sent   any
		want   string
		why    string
		legacy bool
	}{
		{
			name: "omitted scope grants the requested readwrite", requested: oauthmeta.ScopeReadWrite,
			sent: nil, want: oauthmeta.ScopeReadWrite, legacy: true,
			why: "an older dashboard sends only {user_code, decision} and must behave exactly as before",
		},
		{
			name: "omitted scope grants the requested read", requested: oauthmeta.ScopeRead,
			sent: nil, want: oauthmeta.ScopeRead, legacy: true,
			why: "the same legacy body, the other request: the default is the REQUEST, not a constant",
		},
		{
			name: "read requested, readwrite approved", requested: oauthmeta.ScopeRead,
			sent: oauthmeta.ScopeReadWrite, want: oauthmeta.ScopeReadWrite,
			why: "the widening Wan asked for: the agent never has to be re-granted later",
		},
		{
			name: "readwrite requested, read approved", requested: oauthmeta.ScopeReadWrite,
			sent: oauthmeta.ScopeRead, want: oauthmeta.ScopeRead,
			why: "a cautious user can hand out less than the harness asked for, in the same click",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newDeviceFlowEnv(t)
			clientID := env.registerDeviceClient(t, "consent-scope-e2e")
			env.clearDeviceRateWindow(t)

			start := env.startDevice(t, clientID, tc.requested, env.email)
			// The card renders the server's own list, so it can never offer a
			// value this deployment would then refuse.
			card := env.consentCard(t, start.UserCode)
			if strings.Join(card.Scopes, " ") != tc.requested {
				t.Fatalf("card scopes = %v, want the request %q", card.Scopes, tc.requested)
			}
			if strings.Join(card.Choices, " ") != strings.Join(oauthmeta.ScopesSupported(), " ") {
				t.Fatalf("card choices = %v, want the server's %v", card.Choices, oauthmeta.ScopesSupported())
			}

			body := map[string]any{"user_code": start.UserCode, "decision": "approve"}
			if tc.sent != nil {
				body["scope"] = tc.sent
			}
			resp, raw := env.approve(t, body)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("approve = %d body=%s", resp.StatusCode, raw)
			}
			if got := strings.Join(env.storedDeviceScope(t, start.UserCode), " "); got != tc.want {
				t.Fatalf("device-code row scope = %q, want %q (%s)", got, tc.want, tc.why)
			}

			code, tok := env.pollToken(t, clientID, start.DeviceCode)
			if code != http.StatusOK {
				t.Fatalf("token exchange = %d %v", code, tok)
			}
			if tok["scope"] != tc.want {
				t.Fatalf("token response scope = %v, want %q (%s)", tok["scope"], tc.want, tc.why)
			}
			access, _ := tok["access_token"].(string)
			if access == "" {
				t.Fatalf("no access token in %v", tok)
			}
			// THE CONNECTED-APP GRANT: the bearer token the harness now holds
			// must carry the chosen scope, or the ladder at the MCP layer would
			// refuse (or allow) the wrong calls.
			grant, err := env.store.AccessTokenByHash(context.Background(), hashToken(access))
			if err != nil {
				t.Fatalf("read the minted grant: %v", err)
			}
			if strings.Join(grant.Scope, " ") != tc.want {
				t.Fatalf("stored grant scope = %v, want %q", grant.Scope, tc.want)
			}

			// The audit trail separates the REQUEST from the GRANT: at this card
			// they can differ in either direction, and an operator reading the
			// events needs to see that a person widened (or narrowed) it.
			var props string
			if err := env.pool.QueryRow(context.Background(),
				`SELECT properties::text FROM events
				 WHERE account_id = $1 AND event_name = 'oauth.device_approved'
				 ORDER BY id DESC LIMIT 1`, env.acctID).Scan(&props); err != nil {
				t.Fatalf("read oauth.device_approved audit event: %v", err)
			}
			for _, want := range []string{`"scope": "` + tc.want + `"`, `"requested": "` + tc.requested + `"`} {
				if !strings.Contains(props, want) {
					t.Fatalf("audit event %s missing %s", props, want)
				}
			}
		})
	}
}

// TestDeviceConsentRefusesAScopeItCannotIssue pins the refusal half: the value
// must be rejected with a MACHINE CODE, and the authorisation must be left
// APPROVABLE — a person who picked from a stale tab should be able to pick again,
// not start over with a new code.
//
// Documented decision (the card asks for it explicitly): a scope the client did
// NOT request but the server supports is ACCEPTED — the case above
// ("read requested, readwrite approved") asserts that behaviour. Only values this
// deployment cannot issue at all are refused.
func TestDeviceConsentRefusesAScopeItCannotIssue(t *testing.T) {
	env := newDeviceFlowEnv(t)
	clientID := env.registerDeviceClient(t, "consent-refuse-e2e")
	env.clearDeviceRateWindow(t)
	start := env.startDevice(t, clientID, oauthmeta.ScopeRead, env.email)

	bad := []struct {
		what  string
		scope any
	}{
		{"an unsupported scope", "admin"},
		{"a scope in the wrong case", "READWRITE"},
		{"a comma-joined pair", "read,readwrite"},
		{"a space-joined pair", "read readwrite"},
		{"a prefix of a real scope", "readw"},
	}
	for _, tc := range bad {
		t.Run(tc.what, func(t *testing.T) {
			resp, raw := env.approve(t, map[string]any{
				"user_code": start.UserCode, "decision": "approve", "scope": tc.scope,
			})
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("%s: approve = %d body=%s, want 400", tc.what, resp.StatusCode, raw)
			}
			var body map[string]string
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatalf("decode refusal %s: %v", raw, err)
			}
			if body["code"] != CodeDeviceScopeInvalid {
				t.Fatalf("%s: code = %q, want %q (a class code would print the page's "+
					"\"this code is not valid\" for a value the person can simply change)",
					tc.what, body["code"], CodeDeviceScopeInvalid)
			}
			// NOT CLAMPED and NOT BURNED: the row is still pending and still says
			// what the client asked for.
			if got := strings.Join(env.storedDeviceScope(t, start.UserCode), " "); got != oauthmeta.ScopeRead {
				t.Fatalf("%s: a refused approval rewrote the row's scope to %q", tc.what, got)
			}
			if card := env.consentCard(t, start.UserCode); card.State != "pending" {
				t.Fatalf("%s: state after a refused approval = %q, want pending", tc.what, card.State)
			}
		})
	}

	// The refusal above consumed none of the authorisation: the person picks a
	// valid level and it works, first time.
	resp, raw := env.approve(t, map[string]any{
		"user_code": start.UserCode, "decision": "approve", "scope": oauthmeta.ScopeReadWrite,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("approve after refusals = %d body=%s", resp.StatusCode, raw)
	}
	code, tok := env.pollToken(t, clientID, start.DeviceCode)
	if code != http.StatusOK || tok["scope"] != oauthmeta.ScopeReadWrite {
		t.Fatalf("token after a refused-then-valid approval = %d %v", code, tok)
	}
}

// TestDeviceConsentScopeChoiceReachesTheMCPLayer is the acceptance sentence the
// card is written around, at the far end: a harness that asked for `read` but was
// approved as `readwrite` can WRITE with the credential it picked up, without any
// second consent. The unit above proves the row and the response; this proves the
// thing the user actually wanted — the agent stops asking to be re-granted.
func TestDeviceConsentScopeChoiceReachesTheMCPLayer(t *testing.T) {
	env := newDeviceFlowEnv(t)
	clientID := env.registerDeviceClient(t, "consent-mcp-e2e")
	env.clearDeviceRateWindow(t)
	start := env.startDevice(t, clientID, oauthmeta.ScopeRead, env.email)

	if resp, raw := env.approve(t, map[string]any{
		"user_code": start.UserCode, "decision": "approve", "scope": oauthmeta.ScopeReadWrite,
	}); resp.StatusCode != http.StatusOK {
		t.Fatalf("approve = %d body=%s", resp.StatusCode, raw)
	}
	code, tok := env.pollToken(t, clientID, start.DeviceCode)
	if code != http.StatusOK {
		t.Fatalf("token exchange = %d %v", code, tok)
	}
	access, _ := tok["access_token"].(string)

	// A write tool runs on that credential. This is the whole point: the harness
	// asked for read and was widened once, at the consent card, by the person.
	text, isErr := mcpText(t, env.http.URL, access, "update_profile_language", map[string]any{"lang": "ar"})
	if isErr {
		t.Fatalf("a readwrite grant minted from a `read` request was refused a write: %q", text)
	}
	var lang string
	if err := env.pool.QueryRow(context.Background(),
		`SELECT COALESCE(lang,'') FROM users WHERE id = $1`, env.userID).Scan(&lang); err != nil {
		t.Fatalf("read users.lang: %v", err)
	}
	if lang != "ar" {
		t.Fatalf("users.lang = %q after an approved write, want ar", lang)
	}

	// And the cautious direction is a real limit, not a label: the same flow
	// approved as `read` cannot write.
	env.clearDeviceRateWindow(t)
	start2 := env.startDevice(t, clientID, oauthmeta.ScopeReadWrite, env.email)
	if resp, raw := env.approve(t, map[string]any{
		"user_code": start2.UserCode, "decision": "approve", "scope": oauthmeta.ScopeRead,
	}); resp.StatusCode != http.StatusOK {
		t.Fatalf("approve (narrowed) = %d body=%s", resp.StatusCode, raw)
	}
	code, tok2 := env.pollToken(t, clientID, start2.DeviceCode)
	if code != http.StatusOK {
		t.Fatalf("token exchange (narrowed) = %d %v", code, tok2)
	}
	narrowed, _ := tok2["access_token"].(string)
	if out, werr := mcpText(t, env.http.URL, narrowed, "update_profile_language",
		map[string]any{"lang": "ms"}); !werr {
		t.Fatalf("a credential narrowed to read was allowed to write: %q", out)
	}
}
