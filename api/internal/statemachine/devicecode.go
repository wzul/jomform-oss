// Package statemachine derives an aggregate's state from the nullable timestamp
// columns it is spread across — ONCE, for every reader (card t_c6ca84a7).
//
// WHY THIS PACKAGE EXISTS. oauth_device_codes has no status column. Its five
// states — pending, approved, denied, consumed, expired — are derived from
// last_polled_at, approved_at, denied_at, consumed_at and expires_at, and every
// reader used to derive them again in its own switch: the token endpoint, the
// consent card, the approve/deny store methods, and the handler test's fake.
// They disagreed. The consent card asked "approved?" BEFORE "expired?", so an
// approved code whose ten minutes had run out rendered as "approved" — with
// Approve and Deny buttons under it — while the token endpoint answered
// expired_token for the very same row, and the card could also paint a red
// "not valid" banner above a live consent card (issue #1737).
//
// THE RULE. One reader per fact:
//
//	DeviceStateOf      the state, from the row's four nullable timestamps
//	DeviceTransition   what one event does in one state (the table below)
//	DeviceStatusOf     the state + the seconds left, for the agent-facing answer
//
// Nothing else in the tree may re-derive a device code's state. A reader that
// needs "can this still be used" asks DeviceStatusOf; a writer that needs "may
// I apply this event" asks DeviceTransition. Both read the same table, so the
// screen and the token endpoint cannot disagree again.
package statemachine

import "time"

// DeviceState is the state of one RFC 8628 device authorisation.
type DeviceState string

const (
	// DevicePending: created, nobody has decided, still inside expires_at.
	DevicePending DeviceState = "pending"
	// DeviceApproved: a person consented; the harness has not collected the
	// token yet. Exchangeable exactly once.
	DeviceApproved DeviceState = "approved"
	// DeviceDenied: a person refused. Terminal.
	DeviceDenied DeviceState = "denied"
	// DeviceConsumed: the token has been minted. Terminal (single use).
	DeviceConsumed DeviceState = "consumed"
	// DeviceExpired: past expires_at. Terminal — a token endpoint answers
	// expired_token for an expired row no matter what else is set on it.
	DeviceExpired DeviceState = "expired"
)

// DeviceWireName is the spelling of a state on the wire.
//
// "consumed" is the DATABASE column's word and the state machine's word; the
// consent payload has called it "used" since card t_9a6cc85f and the deployed
// dashboard branches on that literal. The alias lives here, in the one file
// that owns the vocabulary, so the rename can never be done in half the tree —
// the wire vocabulary is unchanged by this card.
func (s DeviceState) DeviceWireName() string {
	if s == DeviceConsumed {
		return "used"
	}
	return string(s)
}

// DeviceFacts is a device row's state-bearing columns: the deadline plus the
// three nullable decision timestamps. last_polled_at is deliberately absent —
// it records that a client asked, never that anything was decided.
type DeviceFacts struct {
	ExpiresAt  time.Time
	ApprovedAt *time.Time
	DeniedAt   *time.Time
	ConsumedAt *time.Time
}

// DeviceStateOf is THE derivation. Every reader — token endpoint, consent card,
// connected-apps/MCP surface, tests — calls this and nothing else.
//
// PRECEDENCE, and why it is this order:
//
//  1. consumed  — single use is absolute; a spent code is spent.
//  2. denied    — a refusal outranks the clock: RFC 8628 §3.5 makes access_denied
//     the answer to a denied authorisation even after it expires, and the token
//     endpoint has always answered in this order.
//  3. expired   — BEFORE approved, on purpose. expires_at bounds the whole
//     authorisation, so an approval the harness never collected cannot keep a
//     dead code looking alive. The token endpoint already answers
//     expired_token for such a row; deriving "approved" on the consent card was
//     the disagreement this card removes.
//  4. approved  — consented, collectable exactly once.
//  5. pending   — the default, including a row that was polled but not decided.
func DeviceStateOf(f DeviceFacts, now time.Time) DeviceState {
	switch {
	case f.ConsumedAt != nil:
		return DeviceConsumed
	case f.DeniedAt != nil:
		return DeviceDenied
	case !now.Before(f.ExpiresAt):
		return DeviceExpired
	case f.ApprovedAt != nil:
		return DeviceApproved
	default:
		return DevicePending
	}
}

// DeviceEvent is something that happens TO a device authorisation. These are the
// real events of the flow, not the HTTP routes that carry them: POST /oauth/token
// is a poll while nothing is approved and an exchange once it is.
type DeviceEvent string

const (
	// DeviceEventPoll: the client asks whether the person has decided yet. It
	// records last_polled_at and, while pending, answers authorization_pending
	// (RFC 8628 §3.5).
	DeviceEventPoll DeviceEvent = "poll"
	// DeviceEventApprove: the person consents, on the consent card or the
	// emailed verification link.
	DeviceEventApprove DeviceEvent = "approve"
	// DeviceEventDeny: the person refuses.
	DeviceEventDeny DeviceEvent = "deny"
	// DeviceEventExchange: the approved code is turned into a token pair. This
	// is the single-use step.
	DeviceEventExchange DeviceEvent = "exchange"
	// DeviceEventExpire: the clock passes expires_at. Nobody calls it; the
	// derivation applies it, and the table says what it does to each state.
	DeviceEventExpire DeviceEvent = "expire"
)

// DeviceReason is the answer the caller gets. The RFC 8628 §3.5 vocabulary is
// used verbatim so the token endpoint's `error` field can be this value, and
// the same strings serve the non-RFC callers (approve/deny from the dashboard)
// as a machine-readable reason instead of prose.
type DeviceReason string

const (
	// DeviceReasonNone: nothing to say — the event was applied and the caller
	// proceeds.
	DeviceReasonNone DeviceReason = ""
	// DeviceReasonAuthorizationPending: not decided yet; keep polling.
	DeviceReasonAuthorizationPending DeviceReason = "authorization_pending"
	// DeviceReasonAccessDenied: the person said no.
	DeviceReasonAccessDenied DeviceReason = "access_denied"
	// DeviceReasonExpiredToken: past expires_at; start over.
	DeviceReasonExpiredToken DeviceReason = "expired_token"
	// DeviceReasonInvalidGrant: unknown, already spent, or a replay of a step
	// that can happen once.
	DeviceReasonInvalidGrant DeviceReason = "invalid_grant"
)

// DeviceStep is what the table says about one (state, event) cell.
type DeviceStep struct {
	// Allowed reports whether the event may be applied in this state. An
	// allowed step writes the column the event is about.
	Allowed bool
	// Next is the state after the event.
	Next DeviceState
	// Answer is what the caller is told. For a refused step it is the reason;
	// for an allowed one it is DeviceReasonNone, except poll-on-pending, where
	// the poll IS legal (it records last_polled_at) and the harness must still
	// be told to keep polling.
	Answer DeviceReason
}

// deviceTable is the whole matrix: five states × five events, every cell spelled
// out. It is a map rather than a switch so DeviceTransitionAll — and the matrix
// test — can walk it, and so a state added without its row is a caught hole
// (devicecode_test.go walks the full product and fails on a missing cell)
// instead of a silent default.
var deviceTable = map[DeviceState]map[DeviceEvent]DeviceStep{
	DevicePending: {
		DeviceEventPoll:     {Allowed: true, Next: DevicePending, Answer: DeviceReasonAuthorizationPending},
		DeviceEventApprove:  {Allowed: true, Next: DeviceApproved},
		DeviceEventDeny:     {Allowed: true, Next: DeviceDenied},
		DeviceEventExchange: {Next: DevicePending, Answer: DeviceReasonAuthorizationPending},
		DeviceEventExpire:   {Allowed: true, Next: DeviceExpired},
	},
	DeviceApproved: {
		// A status poll of an approved code is legal and changes nothing: the
		// token endpoint does not take this arm (it exchanges instead), but a
		// reader asking "anything new?" is not doing something wrong.
		DeviceEventPoll: {Allowed: true, Next: DeviceApproved},
		// Consent happens once.
		DeviceEventApprove:  {Next: DeviceApproved, Answer: DeviceReasonInvalidGrant},
		DeviceEventDeny:     {Next: DeviceApproved, Answer: DeviceReasonInvalidGrant},
		DeviceEventExchange: {Allowed: true, Next: DeviceConsumed},
		DeviceEventExpire:   {Allowed: true, Next: DeviceExpired},
	},
	DeviceDenied: {
		DeviceEventPoll:     {Next: DeviceDenied, Answer: DeviceReasonAccessDenied},
		DeviceEventApprove:  {Next: DeviceDenied, Answer: DeviceReasonAccessDenied},
		DeviceEventDeny:     {Next: DeviceDenied, Answer: DeviceReasonAccessDenied},
		DeviceEventExchange: {Next: DeviceDenied, Answer: DeviceReasonAccessDenied},
		DeviceEventExpire:   {Next: DeviceDenied, Answer: DeviceReasonAccessDenied},
	},
	DeviceConsumed: {
		DeviceEventPoll:     {Next: DeviceConsumed, Answer: DeviceReasonInvalidGrant},
		DeviceEventApprove:  {Next: DeviceConsumed, Answer: DeviceReasonInvalidGrant},
		DeviceEventDeny:     {Next: DeviceConsumed, Answer: DeviceReasonInvalidGrant},
		DeviceEventExchange: {Next: DeviceConsumed, Answer: DeviceReasonInvalidGrant},
		DeviceEventExpire:   {Next: DeviceConsumed, Answer: DeviceReasonInvalidGrant},
	},
	DeviceExpired: {
		DeviceEventPoll:     {Next: DeviceExpired, Answer: DeviceReasonExpiredToken},
		DeviceEventApprove:  {Next: DeviceExpired, Answer: DeviceReasonExpiredToken},
		DeviceEventDeny:     {Next: DeviceExpired, Answer: DeviceReasonExpiredToken},
		DeviceEventExchange: {Next: DeviceExpired, Answer: DeviceReasonExpiredToken},
		DeviceEventExpire:   {Next: DeviceExpired, Answer: DeviceReasonExpiredToken},
	},
}

// DeviceStates lists every state, in the flow's order.
func DeviceStates() []DeviceState {
	return []DeviceState{DevicePending, DeviceApproved, DeviceDenied, DeviceConsumed, DeviceExpired}
}

// DeviceEvents lists every event, in the order a reader should show them.
func DeviceEvents() []DeviceEvent {
	return []DeviceEvent{DeviceEventPoll, DeviceEventApprove, DeviceEventDeny, DeviceEventExchange, DeviceEventExpire}
}

// DeviceTransition asks the table. An unknown state or event is a programming
// error, not a runtime condition, and answers a refusal (never a permissive
// zero value) so a typo can only ever be too strict.
func DeviceTransition(state DeviceState, event DeviceEvent) DeviceStep {
	if row, ok := deviceTable[state]; ok {
		if step, ok := row[event]; ok {
			return step
		}
	}
	return DeviceStep{Next: state, Answer: DeviceReasonInvalidGrant}
}

// DeviceApproveStep is what a CONSENT ATTEMPT does, and it carries the flow's
// frozen precedence on top of the table: a row that already carries approved_at
// (or consumed_at) answers invalid_grant even when its deadline has ALSO passed.
//
// WHY THIS IS NOT JUST DeviceTransition(state, approve): the dashboard has always
// checked "already approved?" before it looked at the clock, so re-approving a
// code approved ten minutes ago is a 404 "not valid" today. The derived state for
// that row is expired — correctly, the token endpoint answers expired_token — but
// card t_c6ca84a7 freezes machine codes, so this one refusal keeps its old answer
// instead of turning into 410. Readers still derive the STATE once; only the
// dashboard's re-approve refusal remembers what it used to say.
func DeviceApproveStep(f DeviceFacts, now time.Time) DeviceStep {
	if f.ApprovedAt != nil || f.ConsumedAt != nil {
		return DeviceStep{Next: DeviceStateOf(f, now), Answer: DeviceReasonInvalidGrant}
	}
	return DeviceTransition(DeviceStateOf(f, now), DeviceEventApprove)
}

// DeviceTokenEvent is the event a POST /oauth/token step IS in this state: a
// POLL while nothing is approved (it records last_polled_at and answers
// authorization_pending), an EXCHANGE once the person has approved it. Every
// other state answers a refusal either way, so the poll is used there.
//
// This is the ONE place the two halves of §3.4/§3.5 are joined; the endpoint
// does not decide for itself which event it is running.
func DeviceTokenEvent(state DeviceState) DeviceEvent {
	if state == DeviceApproved {
		return DeviceEventExchange
	}
	return DeviceEventPoll
}

// DeviceAvailableEvents is every event legal in this state, derived from the
// same table (a reader cannot offer a button the table would refuse). The slice
// is never nil: a terminal state answers an empty array, which is what the
// consent payload puts on the wire.
func DeviceAvailableEvents(state DeviceState) []DeviceEvent {
	out := make([]DeviceEvent, 0, len(deviceTable))
	for _, event := range DeviceEvents() {
		if DeviceTransition(state, event).Allowed {
			out = append(out, event)
		}
	}
	return out
}

// DeviceAvailableEventNames is DeviceAvailableEvents as the wire names, for a
// JSON payload.
func DeviceAvailableEventNames(state DeviceState) []string {
	events := DeviceAvailableEvents(state)
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, string(e))
	}
	return out
}

// DeviceRemainingSeconds is the authorisation's life left, floored at zero. It
// answers the clock question on its own: the remaining time and the state are
// two different facts (a consumed code still has time left on the clock, and a
// denial does not shorten it).
func DeviceRemainingSeconds(f DeviceFacts, now time.Time) int {
	left := f.ExpiresAt.Sub(now)
	if left <= 0 {
		return 0
	}
	return int(left / time.Second)
}

// DeviceStatus is the agent-readable answer to "is this code still usable": the
// state, what may still happen to it, and the seconds left.
type DeviceStatus struct {
	State DeviceState
	// Usable reports whether the flow can still complete: pending (waiting for
	// a person) or approved (waiting for the exchange). A denied, consumed or
	// expired code is dead even if the clock still has time on it.
	Usable           bool
	RemainingSeconds int
	ExpiresAt        time.Time
	// AvailableEvents is every event the table still allows — empty for a
	// terminal state, which is what makes "nothing can happen" visible instead
	// of inferred from a 400.
	AvailableEvents []DeviceEvent
}

// DeviceStatusOf is the whole answer in one call, from the one derivation.
func DeviceStatusOf(f DeviceFacts, now time.Time) DeviceStatus {
	state := DeviceStateOf(f, now)
	return DeviceStatus{
		State:            state,
		Usable:           state == DevicePending || state == DeviceApproved,
		RemainingSeconds: DeviceRemainingSeconds(f, now),
		ExpiresAt:        f.ExpiresAt,
		AvailableEvents:  DeviceAvailableEvents(state),
	}
}
