package statemachine

// Matrix tests for the API-key state machine (card t_df243a6e).

import (
	"testing"
	"time"
)

// apiKeyExpectation is this file's own copy of the matrix.
var apiKeyExpectation = map[APIKeyState]map[APIKeyEvent]APIKeyStep{
	APIKeyActive: {
		APIKeyEventAuthenticate: {Allowed: true, Next: APIKeyActive},
		APIKeyEventList:         {Allowed: true, Next: APIKeyActive},
		APIKeyEventRevoke:       {Allowed: true, Next: APIKeyRevoked},
	},
	APIKeyRevoked: {
		APIKeyEventAuthenticate: {Next: APIKeyRevoked, Answer: APIKeyReasonInvalidKey},
		APIKeyEventList:         {Next: APIKeyRevoked},
		APIKeyEventRevoke:       {Next: APIKeyRevoked},
	},
}

// TestAPIKeyTransitionMatrixIsTheWholeProduct walks every (state, event) cell.
func TestAPIKeyTransitionMatrixIsTheWholeProduct(t *testing.T) {
	states := APIKeyStates()
	events := APIKeyEvents()
	if len(states) != 2 || len(events) != 3 {
		t.Fatalf("state machine shape changed: %d states x %d events", len(states), len(events))
	}
	for _, state := range states {
		row, ok := apiKeyExpectation[state]
		if !ok {
			t.Fatalf("state %q has no expectation row — the table was extended without this test", state)
		}
		for _, event := range events {
			want, ok := row[event]
			if !ok {
				t.Fatalf("state %q has no expectation for event %q", state, event)
			}
			if got := APIKeyTransition(state, event); got != want {
				t.Errorf("APIKeyTransition(%q, %q) = %+v, want %+v", state, event, got, want)
			}
		}
	}
}

// TestAPIKeyStateAndStatus pins the two states and the two readers' answers, so a
// key that a lookup refuses can never still be listed.
func TestAPIKeyStateAndStatus(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	revoked := now.Add(-time.Hour)

	active := APIKeyFacts{}
	if got := APIKeyStateOf(active, now); got != APIKeyActive {
		t.Fatalf("state = %q, want active", got)
	}
	status := APIKeyStatusOf(active, now)
	if !status.Authenticatable || !status.Listed || len(status.AvailableEvents) == 0 {
		t.Fatalf("active status = %+v, want authenticatable and listed", status)
	}

	gone := APIKeyFacts{RevokedAt: &revoked}
	if got := APIKeyStateOf(gone, now); got != APIKeyRevoked {
		t.Fatalf("state = %q, want revoked", got)
	}
	status = APIKeyStatusOf(gone, now)
	if status.Authenticatable {
		t.Fatal("a revoked key must not authenticate")
	}
	if status.Listed {
		t.Fatal("a revoked key must not be listed")
	}
	if step := APIKeyAuthenticateStep(gone, now); step.Answer != APIKeyReasonInvalidKey {
		t.Fatalf("answer = %q, want invalid_key", step.Answer)
	}
	if APIKeyStatusOf(gone, now).AvailableEvents == nil {
		t.Fatal("AvailableEvents must never be nil")
	}
}

// TestAPIKeyTransitionRefusesAnUnknownStateOrEvent keeps the default fail-closed.
func TestAPIKeyTransitionRefusesAnUnknownStateOrEvent(t *testing.T) {
	for _, step := range []APIKeyStep{
		APIKeyTransition(APIKeyState("nope"), APIKeyEventAuthenticate),
		APIKeyTransition(APIKeyActive, APIKeyEvent("nope")),
	} {
		if step.Allowed || step.Answer != APIKeyReasonInvalidKey {
			t.Fatalf("an unknown cell must answer invalid_key, got %+v", step)
		}
	}
}
