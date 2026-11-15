package httpapi

// The device code's state is derived ONCE, and every reader agrees (card
// t_c6ca84a7).
//
// THE DEFECT THIS PINS. oauth_device_codes has no status column: pending /
// approved / denied / consumed / expired come from last_polled_at, approved_at,
// denied_at, consumed_at and expires_at. Four readers derived them separately and
// disagreed. The sharpest disagreement was the "approved but never collected"
// arm: the consent card asked "approved?" BEFORE "expired?", so a code approved
// ten minutes ago rendered as a live card with Approve and Deny on it — while the
// token endpoint answered expired_token for the very same row. No test covered
// that arm, because a hand-written suite walks pending → approved → consumed and
// stops.
//
// WHAT IS ASSERTED, per arm:
//
//	the consent card's state        (a person's screen)
//	the token endpoint's state      (an agent's channel)
//	that the two are the SAME       (the one-derivation property)
//	the RFC error string            (UNCHANGED — behaviour freeze)
//	usable / remaining_seconds      (the agent-readable answer, not a bare 400)
//
// WHY THE REAL SERVER. Every arm needs a row that the four timestamps put in a
// particular state, and the property is that the HTTP surface agrees with the
// table. The rows are seeded directly in Postgres so each arm is exact instead of
// "sleep ten minutes", and the endpoints are driven over HTTP so what is asserted
// is the wire, not a helper.
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
	"time"

	"github.com/wzul/jomform/api/internal/oauthmeta"
)

// deviceArm is one row's state-bearing columns: the deadline plus the three
// nullable decision timestamps, exactly as statemachine.DeviceFacts reads them.
type deviceArm struct {
	name     string
	expires  time.Duration // relative to now
	approved time.Duration // 0 = NULL
	denied   time.Duration // 0 = NULL
	consumed time.Duration // 0 = NULL

	wantState      string // the wire spelling of the derived state
	wantUsable     bool
	wantRemaining  bool // remaining_seconds > 0
	wantDecideable bool // may the consent card draw Approve/Deny
	// wantTokenError is the RFC 8628 §3.5 `error` the token endpoint answers.
	// Empty means "the exchange succeeds".
	wantTokenError string
}

// theDeviceArms is the matrix, including the two arms a hand-written suite
// forgets: expired-but-never-consumed, and approved-but-never-exchanged (both
// before and after the deadline).
func theDeviceArms() []deviceArm {
	return []deviceArm{
		{
			name:      "pending, nobody has decided",
			expires:   10 * time.Minute,
			wantState: "pending", wantUsable: true, wantRemaining: true, wantDecideable: true,
			wantTokenError: "authorization_pending",
		},
		{
			// APPROVED-BUT-NOT-EXCHANGED, still inside the deadline: collectable
			// exactly once, and NOT a decision any more.
			name:      "approved, harness has not collected it",
			expires:   10 * time.Minute,
			approved:  -time.Minute,
			wantState: "approved", wantUsable: true, wantRemaining: true, wantDecideable: false,
			wantTokenError: "", // the exchange succeeds
		},
		{
			// THE ARM THE CARD EXISTS FOR: approved, never collected, deadline
			// passed. The card used to say "approved" with live buttons; the
			// token endpoint has always answered expired_token.
			name:      "approved, never collected, now expired",
			expires:   -time.Minute,
			approved:  -2 * time.Minute,
			wantState: "expired", wantUsable: false, wantRemaining: false, wantDecideable: false,
			wantTokenError: "expired_token",
		},
		{
			name:      "expired, never decided",
			expires:   -time.Minute,
			wantState: "expired", wantUsable: false, wantRemaining: false, wantDecideable: false,
			wantTokenError: "expired_token",
		},
		{
			name:    "denied",
			expires: 10 * time.Minute, denied: -time.Minute,
			wantState: "denied", wantUsable: false, wantRemaining: true, wantDecideable: false,
			wantTokenError: "access_denied",
		},
		{
			// Consumed: single use. The clock still has time on it, which is why
			// "remaining seconds" and "usable" are two different facts.
			name:     "consumed",
			expires:  10 * time.Minute,
			approved: -2 * time.Minute, consumed: -time.Minute,
			wantState: "used", wantUsable: false, wantRemaining: true, wantDecideable: false,
			wantTokenError: "invalid_grant",
		},
	}
}

// seedDeviceRow writes one authorisation with exactly the timestamps an arm
// names, bound to the env's user so the consent card may render it.
func (e *deviceFlowEnv) seedDeviceRow(t *testing.T, clientID, deviceCode, userCode string, arm deviceArm) {
	t.Helper()
	now := time.Now().UTC()
	nullable := func(d time.Duration) any {
		if d == 0 {
			return nil
		}
		return now.Add(d)
	}
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO oauth_device_codes
		   (device_hash, user_code_hash, client_id, user_id, scope, ip, user_agent,
		    expires_at, interval_s, approved_at, denied_at, consumed_at, created_at)
		 VALUES ($1,$2,$3,$4,$5,'203.0.113.99','state-matrix',$6,5,$7,$8,$9,now())`,
		hashToken(deviceCode), hashToken(userCode), clientID, e.userID, []string{oauthmeta.ScopeRead},
		now.Add(arm.expires), nullable(arm.approved), nullable(arm.denied), nullable(arm.consumed),
	); err != nil {
		t.Fatalf("seed the %q row: %v", arm.name, err)
	}
}

// consentCard GETs the card the email's link lands on.
func (e *deviceFlowEnv) consentCardOf(t *testing.T, userCode string) deviceConsentOut {
	t.Helper()
	resp, raw := e.do(t, e.authed(t, "GET",
		"/v1/auth/connected-apps/device?user_code="+url.QueryEscape(userCode), nil))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent card = %d body=%s, want 200", resp.StatusCode, raw)
	}
	var out deviceConsentOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode consent card %s: %v", raw, err)
	}
	return out
}

// TestDeviceStateIsDerivedOnceForEveryReader walks the matrix and requires the
// card and the token endpoint to give the SAME answer about the same row.
func TestDeviceStateIsDerivedOnceForEveryReader(t *testing.T) {
	env := newDeviceFlowEnv(t)
	clientID := env.registerDeviceClient(t, "state-matrix")

	for i, arm := range theDeviceArms() {
		t.Run(arm.name, func(t *testing.T) {
			// Distinct codes per arm so a stale row can never satisfy a lookup.
			deviceCode := "device-state-" + time.Now().Format("150405.000000000") + "-" + string(rune('a'+i))
			userCode := "BCDF-GHJ" + string(rune('A'+i))

			env.seedDeviceRow(t, clientID, deviceCode, userCode, arm)

			// (1) A PERSON'S SCREEN.
			card := env.consentCardOf(t, userCode)
			if card.State != arm.wantState {
				t.Fatalf("consent card state = %q, want %q (the card must agree with the token endpoint)", card.State, arm.wantState)
			}
			if card.AvailableEvents == nil {
				t.Fatalf("consent card available_events is null; a terminal state must be an empty array")
			}
			decideable := false
			for _, ev := range card.AvailableEvents {
				if ev == "approve" {
					decideable = true
				}
			}
			if decideable != arm.wantDecideable {
				t.Fatalf("consent card available_events = %v for state %q, so approve-is-offered = %v, want %v",
					card.AvailableEvents, card.State, decideable, arm.wantDecideable)
			}
			// remaining_seconds is a DIFFERENT fact from the state: a consumed
			// code is not usable and still has time on the clock.
			if got := card.RemainingSeconds > 0; got != arm.wantRemaining {
				t.Fatalf("consent card remaining_seconds = %d (nonzero=%v), want nonzero=%v for state %q",
					card.RemainingSeconds, got, arm.wantRemaining, card.State)
			}

			// (2) AN AGENT'S CHANNEL. The RFC error string is frozen...
			status, body := env.pollToken(t, clientID, deviceCode)
			gotErr, _ := body["error"].(string)
			if gotErr != arm.wantTokenError {
				t.Fatalf("token endpoint = %d %q, want error %q (body %v)", status, gotErr, arm.wantTokenError, body)
			}
			// ...and the state it reports is the SAME derivation the card used.
			if arm.wantTokenError == "" {
				// The approved arm minted a token; the row is now consumed, so
				// there is no state to compare and nothing left to say.
				return
			}
			gotState, _ := body["state"].(string)
			if gotState != arm.wantState {
				t.Fatalf("token endpoint state = %q, want %q — the card said %q and the two readers must agree (card t_c6ca84a7)",
					gotState, arm.wantState, card.State)
			}
			if gotUsable, _ := body["usable"].(bool); gotUsable != arm.wantUsable {
				t.Fatalf("token endpoint usable = %v, want %v for state %q", gotUsable, arm.wantUsable, arm.wantState)
			}
			if _, ok := body["remaining_seconds"]; !ok {
				t.Fatalf("token endpoint answered without remaining_seconds: %v", body)
			}
			events, ok := body["available_events"].([]any)
			if !ok {
				t.Fatalf("token endpoint answered without available_events: %v", body)
			}
			if len(events) != len(card.AvailableEvents) {
				t.Fatalf("available_events differ between the card (%v) and the token endpoint (%v)",
					card.AvailableEvents, events)
			}
		})
	}
}

// TestUnknownDeviceCodeSaysNothing is the other half: the extra members are
// about a code the caller can prove it holds. An unknown device_code (or a wrong
// client_id) must stay a bare invalid_grant with no state, no remaining seconds
// and no event list — otherwise the additions would become an oracle.
func TestUnknownDeviceCodeSaysNothing(t *testing.T) {
	env := newDeviceFlowEnv(t)
	clientID := env.registerDeviceClient(t, "state-unknown")

	status, body := env.pollToken(t, clientID, "no-such-device-code")
	if status != http.StatusBadRequest || body["error"] != "invalid_grant" {
		t.Fatalf("unknown device_code = %d %v, want 400 invalid_grant", status, body)
	}
	for _, k := range []string{"state", "usable", "remaining_seconds", "available_events"} {
		if _, ok := body[k]; ok {
			t.Fatalf("unknown device_code answered %q=%v; a code nobody can produce must not be described", k, body[k])
		}
	}
}

// TestDenyAndApproveObeyTheSameTable pins the freeze on the DASHBOARD writes.
// Deny used to be one conditional UPDATE with its own copy of "may this row be
// denied"; it now asks the same table as everything else. The wire must not
// notice: a dead row is 404 in every case, and the pending path still works.
func TestDenyAndApproveObeyTheSameTable(t *testing.T) {
	env := newDeviceFlowEnv(t)
	clientID := env.registerDeviceClient(t, "state-decide")

	decide := func(t *testing.T, userCode, decision string) (int, string) {
		t.Helper()
		resp, raw := env.do(t, env.authed(t, "POST", "/v1/auth/connected-apps/device",
			strings.NewReader(`{"user_code":"`+userCode+`","decision":"`+decision+`"}`)))
		return resp.StatusCode, string(raw)
	}

	t.Run("a pending row can be denied, then cannot be denied again", func(t *testing.T) {
		env.seedDeviceRow(t, clientID, "deny-device-1", "BCDF-GHJM", deviceArm{expires: 10 * time.Minute})
		if code, body := decide(t, "BCDF-GHJM", "deny"); code != http.StatusOK {
			t.Fatalf("deny = %d %s, want 200", code, body)
		}
		if code, body := decide(t, "BCDF-GHJM", "deny"); code != http.StatusNotFound {
			t.Fatalf("second deny = %d %s, want 404 (frozen)", code, body)
		}
		// ...and the approval that tries to rescue a denial is refused, too.
		if code, body := decide(t, "BCDF-GHJM", "approve"); code != http.StatusConflict {
			t.Fatalf("approve after deny = %d %s, want 409 (frozen)", code, body)
		}
	})

	t.Run("an expired row cannot be denied", func(t *testing.T) {
		env.seedDeviceRow(t, clientID, "deny-device-2", "BCDF-GHJN", deviceArm{expires: -time.Minute})
		if code, body := decide(t, "BCDF-GHJN", "deny"); code != http.StatusNotFound {
			t.Fatalf("deny on an expired row = %d %s, want 404 (frozen)", code, body)
		}
		// The row must be untouched: a refusal that wrote denied_at would turn
		// "expired" into "denied" for every later reader.
		var denied *time.Time
		if err := env.pool.QueryRow(context.Background(),
			`SELECT denied_at FROM oauth_device_codes WHERE user_code_hash = $1`,
			hashToken("BCDF-GHJN")).Scan(&denied); err != nil {
			t.Fatalf("read the expired row: %v", err)
		}
		if denied != nil {
			t.Fatalf("a refused deny stamped denied_at = %v on an expired row", denied)
		}
	})

	t.Run("an approved row cannot be denied", func(t *testing.T) {
		env.seedDeviceRow(t, clientID, "deny-device-3", "BCDF-GHJP",
			deviceArm{expires: 10 * time.Minute, approved: -time.Minute})
		if code, body := decide(t, "BCDF-GHJP", "deny"); code != http.StatusNotFound {
			t.Fatalf("deny on an approved row = %d %s, want 404 (frozen)", code, body)
		}
	})
}
