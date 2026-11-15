package statemachine

// Matrix tests for the device-authorisation state machine (card t_c6ca84a7).
//
// WHY A MATRIX AND NOT A HANDFUL OF SCENARIOS. The bug this card removes was a
// DISAGREEMENT between readers, and a hand-written suite only ever covers the
// arms somebody remembered to write: every device-code test in the tree before
// this file walked pending → approved → consumed and stopped. The two arms that
// actually diverged — expired-but-not-consumed and approved-but-not-exchanged —
// had no test at all, so the consent card could call an expired code "approved"
// for a year and nothing reddened.
//
// Every test below walks the FULL product of DeviceStates() × DeviceEvents().
// A cell added without a decision, a transition made legal by accident, or a
// terminal state that quietly reopens fails here.

import (
	"testing"
	"time"
)

// at is a fixed clock, so a "remaining seconds" assertion cannot flake.
var at = time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

// ptr is the nullable-timestamp helper.
func ptr(t time.Time) *time.Time { return &t }

func TestEveryStateAndEventHasATableCell(t *testing.T) {
	// The table is a map, so a missing cell would fall through to the refusal
	// default. That default is safe for the TOKEN ENDPOINT (a missing cell can
	// only be too strict), but it would silently make a legal event
	// unavailable — an approval button that does nothing. Walk the product and
	// require every cell to be spelled out.
	if got, want := len(deviceTable), len(DeviceStates()); got != want {
		t.Fatalf("table has %d state rows, want %d (%v)", got, want, DeviceStates())
	}
	for _, state := range DeviceStates() {
		row, ok := deviceTable[state]
		if !ok {
			t.Fatalf("no row for state %q", state)
		}
		if got, want := len(row), len(DeviceEvents()); got != want {
			t.Fatalf("state %q has %d event cells, want %d", state, got, len(DeviceEvents()))
		}
		for _, event := range DeviceEvents() {
			step, ok := row[event]
			if !ok {
				t.Fatalf("state %q is missing an event cell for %q", state, event)
			}
			// A refused step must name WHY. "Not allowed and I will not say"
			// is the bare 400 this card exists to replace.
			if !step.Allowed && step.Answer == DeviceReasonNone {
				t.Fatalf("state %q event %q is refused with no reason", state, event)
			}
			// An allowed step must not carry a refusal reason, except the
			// documented poll-on-pending, whose reason IS the answer.
			if step.Allowed && step.Answer != DeviceReasonNone &&
				!(state == DevicePending && event == DeviceEventPoll) {
				t.Fatalf("state %q event %q is allowed but answers %q", state, event, step.Answer)
			}
			// Next must be a real state.
			if _, ok := deviceTable[step.Next]; !ok {
				t.Fatalf("state %q event %q leads to unknown state %q", state, event, step.Next)
			}
		}
	}
}

// TestNoNewlyLegalTransition freezes the allowed set of every state. This is the
// "additive, behaviour-freeze" clause of the card: the table may explain the old
// behaviour, it may not grant one transition more.
func TestNoNewlyLegalTransition(t *testing.T) {
	want := map[DeviceState][]DeviceEvent{
		// A pending code can be polled, consented to, refused and expire.
		DevicePending: {DeviceEventPoll, DeviceEventApprove, DeviceEventDeny, DeviceEventExpire},
		// An approved one can be polled (status only), exchanged once, and
		// expire; it can never be approved or denied again.
		DeviceApproved: {DeviceEventPoll, DeviceEventExchange, DeviceEventExpire},
		// Terminal: nothing at all.
		DeviceDenied:   {},
		DeviceConsumed: {},
		DeviceExpired:  {},
	}
	for _, state := range DeviceStates() {
		got := DeviceAvailableEvents(state)
		wantEvents, ok := want[state]
		if !ok {
			t.Fatalf("state %q is not in the frozen expectation map", state)
		}
		if len(got) != len(wantEvents) {
			t.Fatalf("state %q allows %v, want %v", state, got, wantEvents)
		}
		for i := range wantEvents {
			if got[i] != wantEvents[i] {
				t.Fatalf("state %q allows %v, want %v (order included)", state, got, wantEvents)
			}
		}
		// The names helper is what the wire carries; it must agree exactly.
		names := DeviceAvailableEventNames(state)
		if len(names) != len(wantEvents) {
			t.Fatalf("state %q wire names %v, want %d entries", state, names, len(wantEvents))
		}
		if names == nil {
			t.Fatalf("state %q wire names is nil; a terminal state must be an empty array", state)
		}
	}
}

// TestTerminalStatesRefuseEverything is the half a hand-written suite forgets:
// denied / consumed / expired are dead, and no event may revive them. Each
// refusal must carry its own reason, so a caller can tell "already used" from
// "the person said no" from "the clock ran out".
func TestTerminalStatesRefuseEverything(t *testing.T) {
	wantReason := map[DeviceState]DeviceReason{
		DeviceDenied:   DeviceReasonAccessDenied,
		DeviceConsumed: DeviceReasonInvalidGrant,
		DeviceExpired:  DeviceReasonExpiredToken,
	}
	for state, reason := range wantReason {
		for _, event := range DeviceEvents() {
			step := DeviceTransition(state, event)
			if step.Allowed {
				t.Fatalf("terminal state %q allowed event %q", state, event)
			}
			if step.Answer != reason {
				t.Fatalf("state %q event %q answers %q, want %q", state, event, step.Answer, reason)
			}
			if step.Next != state {
				t.Fatalf("state %q event %q moves to %q; terminal states are terminal", state, event, step.Next)
			}
		}
	}
}

// TestDerivationPrecedence pins the order of DeviceStateOf, including the two
// arms no test in the tree covered: a code that expired before it was consumed,
// and one that was approved but never exchanged.
func TestDerivationPrecedence(t *testing.T) {
	live := at.Add(5 * time.Minute)  // expires in the future
	dead := at.Add(-5 * time.Minute) // already past
	approved := at.Add(-2 * time.Minute)
	denied := at.Add(-2 * time.Minute)
	consumed := at.Add(-1 * time.Minute)

	cases := []struct {
		what string
		f    DeviceFacts
		want DeviceState
	}{
		{"fresh row", DeviceFacts{ExpiresAt: live}, DevicePending},
		{"polled but undecided stays pending", DeviceFacts{ExpiresAt: live}, DevicePending},
		{"approved, not exchanged", DeviceFacts{ExpiresAt: live, ApprovedAt: &approved}, DeviceApproved},
		{"denied", DeviceFacts{ExpiresAt: live, DeniedAt: &denied}, DeviceDenied},
		{"consumed", DeviceFacts{ExpiresAt: live, ApprovedAt: &approved, ConsumedAt: &consumed}, DeviceConsumed},

		// ARM 1 — expired-but-not-consumed: the clock ran out with nobody
		// deciding. The token endpoint answers expired_token; the card must not
		// offer Approve.
		{"expired, never decided", DeviceFacts{ExpiresAt: dead}, DeviceExpired},
		// ARM 2 — approved-but-not-exchanged, past the deadline. Approval does
		// NOT outrank expiry: the endpoint answers expired_token for this row,
		// so the card must say expired too (it used to say "approved").
		{"approved, never collected, now expired", DeviceFacts{ExpiresAt: dead, ApprovedAt: &approved}, DeviceExpired},
		// Precedence inside the terminal group.
		{"denied outranks the clock", DeviceFacts{ExpiresAt: dead, DeniedAt: &denied}, DeviceDenied},
		{"consumed outranks everything", DeviceFacts{ExpiresAt: dead, ApprovedAt: &approved, DeniedAt: &denied, ConsumedAt: &consumed}, DeviceConsumed},
	}
	for _, tc := range cases {
		if got := DeviceStateOf(tc.f, at); got != tc.want {
			t.Errorf("%s: state = %q, want %q", tc.what, got, tc.want)
		}
	}
}

// TestExpiryBoundary: expires_at is the deadline itself, not the last live
// instant (so now == expires_at is expired, matching the token endpoint's
// `!now.Before(expires_at)`).
func TestExpiryBoundary(t *testing.T) {
	deadline := at.Add(2 * time.Second)
	for _, tc := range []struct {
		now  time.Time
		want DeviceState
	}{
		{at.Add(time.Second), DevicePending},
		{deadline, DeviceExpired},
		{deadline.Add(time.Nanosecond), DeviceExpired},
	} {
		if got := DeviceStateOf(DeviceFacts{ExpiresAt: deadline}, tc.now); got != tc.want {
			t.Fatalf("at %v (deadline %v): state = %q, want %q", tc.now, deadline, got, tc.want)
		}
	}
}

// TestTokenEventSelection pins which event POST /oauth/token is in each state.
// Approved is the only state that exchanges; everything else polls, which is
// what makes a spent code answer invalid_grant and a denied one access_denied
// exactly as before.
func TestTokenEventSelection(t *testing.T) {
	want := map[DeviceState]DeviceEvent{
		DevicePending:  DeviceEventPoll,
		DeviceApproved: DeviceEventExchange,
		DeviceDenied:   DeviceEventPoll,
		DeviceConsumed: DeviceEventPoll,
		DeviceExpired:  DeviceEventPoll,
	}
	for _, state := range DeviceStates() {
		if got := DeviceTokenEvent(state); got != want[state] {
			t.Errorf("state %q: token event = %q, want %q", state, got, want[state])
		}
	}
}

// TestTokenEndpointAnswers walks the exact RFC 8628 §3.5 answers the token
// endpoint gives, state by state, through the table the endpoint now uses.
func TestTokenEndpointAnswers(t *testing.T) {
	approved := at
	denied := at
	consumed := at
	cases := []struct {
		what       string
		f          DeviceFacts
		wantAllow  bool
		wantAnswer DeviceReason
	}{
		{"pending polls", DeviceFacts{ExpiresAt: at.Add(time.Minute)}, true, DeviceReasonAuthorizationPending},
		{"approved exchanges", DeviceFacts{ExpiresAt: at.Add(time.Minute), ApprovedAt: &approved}, true, DeviceReasonNone},
		{"denied", DeviceFacts{ExpiresAt: at.Add(time.Minute), DeniedAt: &denied}, false, DeviceReasonAccessDenied},
		{"consumed", DeviceFacts{ExpiresAt: at.Add(time.Minute), ApprovedAt: &approved, ConsumedAt: &consumed}, false, DeviceReasonInvalidGrant},
		{"expired", DeviceFacts{ExpiresAt: at.Add(-time.Minute)}, false, DeviceReasonExpiredToken},
		{"approved then expired", DeviceFacts{ExpiresAt: at.Add(-time.Minute), ApprovedAt: &approved}, false, DeviceReasonExpiredToken},
	}
	for _, tc := range cases {
		state := DeviceStateOf(tc.f, at)
		step := DeviceTransition(state, DeviceTokenEvent(state))
		if step.Allowed != tc.wantAllow || step.Answer != tc.wantAnswer {
			t.Errorf("%s: allowed=%v answer=%q, want allowed=%v answer=%q",
				tc.what, step.Allowed, step.Answer, tc.wantAllow, tc.wantAnswer)
		}
	}
}

// TestStatusAnswersUsability is the AX contract: the answer to "is this code
// still usable" carries the state and the seconds left, so a client is never
// left with a bare 400.
func TestStatusAnswersUsability(t *testing.T) {
	approved := at
	cases := []struct {
		what           string
		f              DeviceFacts
		wantState      DeviceState
		wantUsable     bool
		wantRemaining  int
		wantNoEventsAt bool
	}{
		{"pending, 90s left", DeviceFacts{ExpiresAt: at.Add(90 * time.Second)}, DevicePending, true, 90, false},
		{"approved, 30s left", DeviceFacts{ExpiresAt: at.Add(30 * time.Second), ApprovedAt: &approved}, DeviceApproved, true, 30, false},
		// A consumed code still has wall-clock time left: "remaining seconds"
		// and "usable" are different facts and the answer says both.
		{"consumed, 60s left", DeviceFacts{ExpiresAt: at.Add(60 * time.Second), ConsumedAt: &approved}, DeviceConsumed, false, 60, true},
		{"expired", DeviceFacts{ExpiresAt: at.Add(-time.Second)}, DeviceExpired, false, 0, true},
	}
	for _, tc := range cases {
		st := DeviceStatusOf(tc.f, at)
		if st.State != tc.wantState || st.Usable != tc.wantUsable || st.RemainingSeconds != tc.wantRemaining {
			t.Errorf("%s: status = {%q usable=%v remaining=%d}, want {%q usable=%v remaining=%d}",
				tc.what, st.State, st.Usable, st.RemainingSeconds, tc.wantState, tc.wantUsable, tc.wantRemaining)
		}
		if got := len(st.AvailableEvents) == 0; got != tc.wantNoEventsAt {
			t.Errorf("%s: no available events = %v, want %v (%v)", tc.what, got, tc.wantNoEventsAt, st.AvailableEvents)
		}
		if !st.ExpiresAt.Equal(tc.f.ExpiresAt) {
			t.Errorf("%s: expires_at not carried through", tc.what)
		}
	}
}

// TestWireNamesAreFrozen: the consent payload has spelled consumed as "used"
// since the device grant shipped, and the deployed dashboard branches on those
// literals. The alias must not drift silently.
func TestWireNamesAreFrozen(t *testing.T) {
	want := map[DeviceState]string{
		DevicePending: "pending", DeviceApproved: "approved", DeviceDenied: "denied",
		DeviceConsumed: "used", DeviceExpired: "expired",
	}
	for _, state := range DeviceStates() {
		if got := state.DeviceWireName(); got != want[state] {
			t.Fatalf("wire name of %q = %q, want %q", state, got, want[state])
		}
	}
}

// TestUnknownStateOrEventIsRefusedNotAllowed: the zero-value default of
// DeviceStep is a refusal, and DeviceTransition must not hand back a permissive
// step for something it does not know.
func TestUnknownStateOrEventIsRefusedNotAllowed(t *testing.T) {
	if step := DeviceTransition(DeviceState("nonsense"), DeviceEventPoll); step.Allowed {
		t.Fatalf("unknown state was allowed: %+v", step)
	}
	if step := DeviceTransition(DevicePending, DeviceEvent("nonsense")); step.Allowed {
		t.Fatalf("unknown event was allowed: %+v", step)
	}
}
