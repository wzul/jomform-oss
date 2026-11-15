package statemachine

// Matrix tests for the authorization-code state machine (card t_df243a6e).

import "testing"

import "time"

// authCodeExpectation is this file's own copy of the matrix.
var authCodeExpectation = map[AuthCodeState]map[AuthCodeEvent]AuthCodeStep{
	AuthCodeIssued: {
		AuthCodeEventRedeem: {Allowed: true, Next: AuthCodeConsumed},
		AuthCodeEventExpire: {Allowed: true, Next: AuthCodeExpired},
	},
	AuthCodeConsumed: {
		AuthCodeEventRedeem: {Next: AuthCodeConsumed, Answer: AuthCodeReasonInvalidGrant},
		AuthCodeEventExpire: {Next: AuthCodeConsumed},
	},
	AuthCodeExpired: {
		AuthCodeEventRedeem: {Next: AuthCodeExpired, Answer: AuthCodeReasonInvalidGrant},
		AuthCodeEventExpire: {Next: AuthCodeExpired},
	},
}

// TestAuthCodeTransitionMatrixIsTheWholeProduct walks every (state, event) cell.
func TestAuthCodeTransitionMatrixIsTheWholeProduct(t *testing.T) {
	states := AuthCodeStates()
	events := AuthCodeEvents()
	if len(states) != 3 || len(events) != 2 {
		t.Fatalf("state machine shape changed: %d states x %d events", len(states), len(events))
	}
	for _, state := range states {
		row, ok := authCodeExpectation[state]
		if !ok {
			t.Fatalf("state %q has no expectation row — the table was extended without this test", state)
		}
		for _, event := range events {
			want, ok := row[event]
			if !ok {
				t.Fatalf("state %q has no expectation for event %q", state, event)
			}
			if got := AuthCodeTransition(state, event); got != want {
				t.Errorf("AuthCodeTransition(%q, %q) = %+v, want %+v", state, event, got, want)
			}
		}
	}
}

// TestAuthCodeStateOfProduct is the derivation over the full fact product.
func TestAuthCodeStateOfProduct(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Minute)
	future := now.Add(time.Minute)
	consumed := now.Add(-time.Second)

	cases := []struct {
		name       string
		expiresAt  time.Time
		consumedAt *time.Time
		want       AuthCodeState
	}{
		{"issued", future, nil, AuthCodeIssued},
		{"expired", past, nil, AuthCodeExpired},
		{"consumed", future, &consumed, AuthCodeConsumed},
		// Single use is absolute: a spent code does not become "expired" later.
		{"consumed long after the deadline", past, &consumed, AuthCodeConsumed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := AuthCodeStateOf(AuthCodeFacts{ExpiresAt: c.expiresAt, ConsumedAt: c.consumedAt}, now)
			if got != c.want {
				t.Fatalf("state = %q, want %q", got, c.want)
			}
		})
	}
}

// TestAuthCodeRedeemArm pins the answer each state gives to a redemption, and
// that the window is reported separately from the state.
func TestAuthCodeRedeemArm(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Minute)
	future := now.Add(10 * time.Minute)
	consumed := now.Add(-time.Second)

	issued := AuthCodeFacts{ExpiresAt: future}
	if step := AuthCodeRedeemStep(issued, now); !step.Allowed || step.Next != AuthCodeConsumed {
		t.Fatalf("an issued code must redeem into consumed, got %+v", step)
	}
	status := AuthCodeStatusOf(issued, now)
	if !status.Redeemable || status.State != AuthCodeIssued {
		t.Fatalf("status = %+v, want redeemable issued", status)
	}
	if status.RemainingSeconds != 600 {
		t.Fatalf("remaining = %d, want 600", status.RemainingSeconds)
	}
	if status.AvailableEvents == nil {
		t.Fatal("AvailableEvents must never be nil")
	}

	for name, facts := range map[string]AuthCodeFacts{
		"expired":  {ExpiresAt: past},
		"consumed": {ExpiresAt: future, ConsumedAt: &consumed},
	} {
		step := AuthCodeRedeemStep(facts, now)
		if step.Allowed {
			t.Errorf("%s code must not redeem", name)
		}
		if step.Answer != AuthCodeReasonInvalidGrant {
			t.Errorf("%s code answered %q, want invalid_grant", name, step.Answer)
		}
		if AuthCodeStatusOf(facts, now).Redeemable {
			t.Errorf("%s status must not be redeemable", name)
		}
	}

	if got := AuthCodeRemainingSeconds(AuthCodeFacts{ExpiresAt: past}, now); got != 0 {
		t.Fatalf("remaining past the deadline = %d, want 0", got)
	}
}

// TestAuthCodeTransitionRefusesAnUnknownStateOrEvent keeps the default fail-closed.
func TestAuthCodeTransitionRefusesAnUnknownStateOrEvent(t *testing.T) {
	for _, step := range []AuthCodeStep{
		AuthCodeTransition(AuthCodeState("nope"), AuthCodeEventRedeem),
		AuthCodeTransition(AuthCodeIssued, AuthCodeEvent("nope")),
	} {
		if step.Allowed || step.Answer != AuthCodeReasonInvalidGrant {
			t.Fatalf("an unknown cell must answer invalid_grant, got %+v", step)
		}
	}
}
