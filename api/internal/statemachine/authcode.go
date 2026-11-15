package statemachine

import "time"

// AuthCodeState is the state of one OAuth authorization code (RFC 6749 §4.1).
//
// The code has no status column: consumed_at records the single use and
// expires_at bounds the ten-minute window, so "issued", "consumed" and
// "expired" are all read off those two columns.
type AuthCodeState string

const (
	// AuthCodeIssued: minted by /oauth/authorize, not redeemed, still inside
	// its deadline. Redeemable exactly once.
	AuthCodeIssued AuthCodeState = "issued"
	// AuthCodeConsumed: redeemed. Terminal — single use is absolute, and it
	// stays consumed even after the clock runs out.
	AuthCodeConsumed AuthCodeState = "consumed"
	// AuthCodeExpired: past expires_at and never redeemed. Terminal.
	AuthCodeExpired AuthCodeState = "expired"
)

// AuthCodeFacts is an authorization code's state-bearing columns.
type AuthCodeFacts struct {
	ExpiresAt  time.Time
	ConsumedAt *time.Time
}

// AuthCodeStateOf is THE derivation:
//
//  1. consumed — single use is absolute; a spent code is spent.
//  2. expired  — past its deadline, whatever else is set.
//  3. issued   — the default.
func AuthCodeStateOf(f AuthCodeFacts, now time.Time) AuthCodeState {
	switch {
	case f.ConsumedAt != nil:
		return AuthCodeConsumed
	case !now.Before(f.ExpiresAt):
		return AuthCodeExpired
	default:
		return AuthCodeIssued
	}
}

// AuthCodeEvent is something that happens TO an authorization code.
type AuthCodeEvent string

const (
	// AuthCodeEventRedeem: POST /oauth/token with grant_type=authorization_code.
	// This is the single-use step.
	AuthCodeEventRedeem AuthCodeEvent = "redeem"
	// AuthCodeEventExpire: the clock passes expires_at. Nobody calls it; the
	// derivation applies it.
	AuthCodeEventExpire AuthCodeEvent = "expire"
)

// AuthCodeReason is the answer the caller gets. RFC 6749 §5.2 gives one code
// for every refusal here — an unknown, a spent and an expired code are the same
// `invalid_grant` on the wire, and the store has always answered one frozen
// error (ErrAuthCodeInvalid), so there is nothing finer to say.
type AuthCodeReason string

const (
	// AuthCodeReasonNone: the event was applied.
	AuthCodeReasonNone AuthCodeReason = ""
	// AuthCodeReasonInvalidGrant: unknown, already redeemed, or past its
	// deadline.
	AuthCodeReasonInvalidGrant AuthCodeReason = "invalid_grant"
)

// AuthCodeStep is what the table says about one (state, event) cell.
type AuthCodeStep struct {
	Allowed bool
	Next    AuthCodeState
	Answer  AuthCodeReason
}

// authCodeTable is the whole matrix: three states × two events, every cell
// spelled out. The expire event never answers anybody — there is no caller when
// a deadline passes — so its Answer is empty in every state and Allowed says
// whether the clock actually moves the row.
var authCodeTable = map[AuthCodeState]map[AuthCodeEvent]AuthCodeStep{
	AuthCodeIssued: {
		AuthCodeEventRedeem: {Allowed: true, Next: AuthCodeConsumed},
		AuthCodeEventExpire: {Allowed: true, Next: AuthCodeExpired},
	},
	AuthCodeConsumed: {
		// Single use is absolute: a second redemption is refused, and the row
		// does not become "expired" later — it is consumed.
		AuthCodeEventRedeem: {Next: AuthCodeConsumed, Answer: AuthCodeReasonInvalidGrant},
		AuthCodeEventExpire: {Next: AuthCodeConsumed},
	},
	AuthCodeExpired: {
		AuthCodeEventRedeem: {Next: AuthCodeExpired, Answer: AuthCodeReasonInvalidGrant},
		// Already there.
		AuthCodeEventExpire: {Next: AuthCodeExpired},
	},
}

// AuthCodeStates lists every state, in the life of a code.
func AuthCodeStates() []AuthCodeState {
	return []AuthCodeState{AuthCodeIssued, AuthCodeConsumed, AuthCodeExpired}
}

// AuthCodeEvents lists every event.
func AuthCodeEvents() []AuthCodeEvent {
	return []AuthCodeEvent{AuthCodeEventRedeem, AuthCodeEventExpire}
}

// AuthCodeTransition asks the table. An unknown state or event answers a
// refusal, never a permissive zero value.
func AuthCodeTransition(state AuthCodeState, event AuthCodeEvent) AuthCodeStep {
	if row, ok := authCodeTable[state]; ok {
		if step, ok := row[event]; ok {
			return step
		}
	}
	return AuthCodeStep{Next: state, Answer: AuthCodeReasonInvalidGrant}
}

// AuthCodeRedeemStep is "may this code be exchanged", the redeem cell of the
// table for the code's derived state.
func AuthCodeRedeemStep(f AuthCodeFacts, now time.Time) AuthCodeStep {
	return AuthCodeTransition(AuthCodeStateOf(f, now), AuthCodeEventRedeem)
}

// AuthCodeAvailableEvents is every event legal in this state. The slice is
// never nil.
func AuthCodeAvailableEvents(state AuthCodeState) []AuthCodeEvent {
	out := make([]AuthCodeEvent, 0, len(authCodeTable))
	for _, event := range AuthCodeEvents() {
		if AuthCodeTransition(state, event).Allowed {
			out = append(out, event)
		}
	}
	return out
}

// AuthCodeRemainingSeconds is the code's window left, floored at zero.
func AuthCodeRemainingSeconds(f AuthCodeFacts, now time.Time) int {
	left := f.ExpiresAt.Sub(now)
	if left <= 0 {
		return 0
	}
	return int(left / time.Second)
}

// AuthCodeStatus is the whole answer in one call.
type AuthCodeStatus struct {
	State AuthCodeState
	// Redeemable: the code may still be exchanged for a token pair.
	Redeemable       bool
	ExpiresAt        time.Time
	RemainingSeconds int
	AvailableEvents  []AuthCodeEvent
}

// AuthCodeStatusOf is AuthCodeStatus from the one derivation.
func AuthCodeStatusOf(f AuthCodeFacts, now time.Time) AuthCodeStatus {
	state := AuthCodeStateOf(f, now)
	return AuthCodeStatus{
		State:            state,
		Redeemable:       AuthCodeTransition(state, AuthCodeEventRedeem).Allowed,
		ExpiresAt:        f.ExpiresAt,
		RemainingSeconds: AuthCodeRemainingSeconds(f, now),
		AvailableEvents:  AuthCodeAvailableEvents(state),
	}
}
