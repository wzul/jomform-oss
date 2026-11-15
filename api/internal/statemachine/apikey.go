package statemachine

import "time"

// APIKeyState is the state of one api_keys row.
//
// An API key has no deadline at all: the only state-bearing column is
// revoked_at, so the machine is two states and the whole of it is "has the row
// been revoked". It lives here anyway, because the readers must ask the same
// question the same way — a key that a lookup refuses and a key that a list
// still shows is the same class of disagreement the token pair had.
type APIKeyState string

const (
	// APIKeyActive: never revoked; the key authenticates.
	APIKeyActive APIKeyState = "active"
	// APIKeyRevoked: revoked_at is set. Terminal — a key is never un-revoked,
	// and a revocation never expires.
	APIKeyRevoked APIKeyState = "revoked"
)

// APIKeyFacts is an api_keys row's state-bearing columns. last_used_at is not
// one of them: it records that the key was used, never whether it may be.
type APIKeyFacts struct {
	RevokedAt *time.Time
}

// APIKeyStateOf is THE derivation. API keys carry no deadline, so the clock
// parameter exists only to keep the three StateOf functions callable the same
// way.
func APIKeyStateOf(f APIKeyFacts, _ time.Time) APIKeyState {
	if f.RevokedAt != nil {
		return APIKeyRevoked
	}
	return APIKeyActive
}

// APIKeyEvent is something that happens TO an API key.
type APIKeyEvent string

const (
	// APIKeyEventAuthenticate: the key is presented as a Bearer credential on
	// /v1/rest/* or /mcp.
	APIKeyEventAuthenticate APIKeyEvent = "authenticate"
	// APIKeyEventList: the key appears in GET /v1/rest/keys or list_api_keys.
	APIKeyEventList APIKeyEvent = "list"
	// APIKeyEventRevoke: DELETE /v1/rest/keys/{id} or revoke_api_key.
	APIKeyEventRevoke APIKeyEvent = "revoke"
)

// APIKeyReason is the answer the caller gets. The wire is one frozen answer
// (dbstore.ErrAPIKeyInvalid → 401 "authentication required"), so a refusal never
// says which kind it was.
type APIKeyReason string

const (
	// APIKeyReasonNone: the event was applied, or the refusal is silent (a
	// revoked key is simply not listed; a repeat revoke is a 200 no-op).
	APIKeyReasonNone APIKeyReason = ""
	// APIKeyReasonInvalidKey: the key may not authenticate.
	APIKeyReasonInvalidKey APIKeyReason = "invalid_key"
)

// APIKeyStep is what the table says about one (state, event) cell.
type APIKeyStep struct {
	Allowed bool
	Next    APIKeyState
	Answer  APIKeyReason
}

// apiKeyTable is the whole matrix: two states × three events, every cell
// spelled out.
var apiKeyTable = map[APIKeyState]map[APIKeyEvent]APIKeyStep{
	APIKeyActive: {
		APIKeyEventAuthenticate: {Allowed: true, Next: APIKeyActive},
		APIKeyEventList:         {Allowed: true, Next: APIKeyActive},
		APIKeyEventRevoke:       {Allowed: true, Next: APIKeyRevoked},
	},
	APIKeyRevoked: {
		APIKeyEventAuthenticate: {Next: APIKeyRevoked, Answer: APIKeyReasonInvalidKey},
		// A revoked key is not part of the account's inventory.
		APIKeyEventList: {Next: APIKeyRevoked},
		// Revoking twice is a successful no-op: the UPDATE matches id+account
		// and does not filter on revoked_at, so a client retry stays a 200
		// (see dbstore.APIKeyStore.Revoke).
		APIKeyEventRevoke: {Next: APIKeyRevoked},
	},
}

// APIKeyStates lists every state.
func APIKeyStates() []APIKeyState {
	return []APIKeyState{APIKeyActive, APIKeyRevoked}
}

// APIKeyEvents lists every event.
func APIKeyEvents() []APIKeyEvent {
	return []APIKeyEvent{APIKeyEventAuthenticate, APIKeyEventList, APIKeyEventRevoke}
}

// APIKeyTransition asks the table. An unknown state or event answers a
// refusal, never a permissive zero value.
func APIKeyTransition(state APIKeyState, event APIKeyEvent) APIKeyStep {
	if row, ok := apiKeyTable[state]; ok {
		if step, ok := row[event]; ok {
			return step
		}
	}
	return APIKeyStep{Next: state, Answer: APIKeyReasonInvalidKey}
}

// APIKeyAuthenticateStep is "may this key authenticate a request", the
// authenticate cell of the table for the key's derived state.
func APIKeyAuthenticateStep(f APIKeyFacts, now time.Time) APIKeyStep {
	return APIKeyTransition(APIKeyStateOf(f, now), APIKeyEventAuthenticate)
}

// APIKeyAvailableEvents is every event legal in this state. The slice is never
// nil.
func APIKeyAvailableEvents(state APIKeyState) []APIKeyEvent {
	out := make([]APIKeyEvent, 0, len(apiKeyTable))
	for _, event := range APIKeyEvents() {
		if APIKeyTransition(state, event).Allowed {
			out = append(out, event)
		}
	}
	return out
}

// APIKeyStatus is the whole answer in one call.
type APIKeyStatus struct {
	State APIKeyState
	// Authenticatable: the key may authenticate a request right now.
	Authenticatable bool
	// Listed: the key belongs in the account's key inventory.
	Listed          bool
	AvailableEvents []APIKeyEvent
}

// APIKeyStatusOf is APIKeyStatus from the one derivation.
func APIKeyStatusOf(f APIKeyFacts, now time.Time) APIKeyStatus {
	state := APIKeyStateOf(f, now)
	return APIKeyStatus{
		State:           state,
		Authenticatable: APIKeyTransition(state, APIKeyEventAuthenticate).Allowed,
		Listed:          APIKeyTransition(state, APIKeyEventList).Allowed,
		AvailableEvents: APIKeyAvailableEvents(state),
	}
}
