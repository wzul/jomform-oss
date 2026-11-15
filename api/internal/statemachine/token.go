// Package statemachine derives an aggregate's state from the nullable timestamp
// columns it is spread across — ONCE, for every reader (card t_df243a6e).
//
// WHY THIS PACKAGE EXISTS. The auth tables have no status column. A token pair's
// life is spread over revoked_at, access_expires_at and refresh_expires_at; an
// authorization code's over consumed_at and expires_at; an API key's over
// revoked_at. Every reader used to derive the answer again in its own SQL
// predicate or Go switch — the connected-apps list, the connected-agents list,
// the /mcp authenticator, the refresh path, the code exchange — and two of them
// had already drifted apart: an oauth_tokens row whose refresh credential has no
// deadline was listed as dead by ConnectedAppsByUser and accepted by
// RefreshGrant, so the dashboard could hide a connection that still refreshes.
//
// THE RULE. One reader per fact:
//
//	TokenStateOf / TokenStatusOf           a token pair
//	AuthCodeStateOf / AuthCodeStatusOf     a single-use authorization code
//	APIKeyStateOf / APIKeyStatusOf         an API key
//
// and, beside each, one spelled-out (state × event) table answering "what does
// this event do in this state". A reader that needs "may I still use this"
// asks a StatusOf; a writer that needs "may I apply this event" asks the
// transition. Both read the same table, so a screen and an endpoint cannot
// disagree again.
//
// BEHAVIOUR FREEZE (card t_df243a6e): the store errors that carry these answers
// (dbstore.ErrAccessTokenInvalid, ErrRefreshInvalid, ErrAuthCodeInvalid,
// ErrAPIKeyInvalid) and the HTTP statuses they map to are unchanged. Only where
// the answer is computed moved.
package statemachine

import "time"

// TokenState is the state of one OAuth access+refresh token pair.
type TokenState string

const (
	// TokenLive: not revoked and the ACCESS token is still inside its own
	// deadline. The pair authenticates requests as it stands. The refresh
	// credential may already be dead — that is the "refresh expired but the
	// access token still works" arm, and it is deliberately not a refusal of
	// the access token (see TokenRefreshStep for the other half).
	TokenLive TokenState = "live"
	// TokenRefreshable: not revoked, the access token has lapsed, and a refresh
	// credential exists whose deadline has not passed. The pair can no longer
	// authenticate a request, but it can still be rotated into one that can, so
	// the connection is alive and stays listed.
	TokenRefreshable TokenState = "refreshable"
	// TokenExpired: not revoked, and nothing is left — the access token lapsed
	// and there is either no refresh credential or its deadline passed too.
	TokenExpired TokenState = "expired"
	// TokenRevoked: revoked_at is set. Both credentials are dead whatever the
	// clocks say.
	TokenRevoked TokenState = "revoked"
)

// TokenFacts is a token row's state-bearing columns.
//
// HasRefresh reports whether the row carries a refresh credential at all
// (refresh_hash IS NOT NULL). A non-rotatable access token has none, and its
// access deadline is then the only thing keeping it alive.
//
// A NIL RefreshExpiresAt MEANS "NO DEADLINE", never "expired". The refresh path
// has always accepted such a row (`refresh_expires_at IS NULL OR ... > now()`),
// and that is the only reading under which a row written before the column
// existed stays refreshable. The connected-apps query used to read the same
// column the other way round and call the pair dead, which is the drift this
// card removes; the derivation follows the refresh path, because that is the
// arm whose job it is to know.
//
// last_used_at is deliberately absent, for the same reason the device-code
// table leaves out last_polled_at: it records that the credential was USED,
// never that anything about it was decided.
type TokenFacts struct {
	HasRefresh       bool
	AccessExpiresAt  time.Time
	RefreshExpiresAt *time.Time
	RevokedAt        *time.Time
}

// tokenRefreshUsable reports whether a refresh credential exists and its
// deadline has not passed. A nil deadline is unbounded (see TokenFacts).
func tokenRefreshUsable(f TokenFacts, now time.Time) bool {
	if !f.HasRefresh {
		return false
	}
	return f.RefreshExpiresAt == nil || now.Before(*f.RefreshExpiresAt)
}

// TokenStateOf is THE derivation. Every reader — the /mcp authenticator, the
// REST key resolver, the connected-apps list, the connected-agents list and the
// token endpoint's refresh arm — calls this and nothing else.
//
// PRECEDENCE, and why it is this order:
//
//  1. revoked     — a revocation outranks every clock: a revoked pair whose
//     access token has not expired yet is still dead.
//  2. live        — the access token is inside its deadline, so requests
//     authenticate right now.
//  3. refreshable — the access token has lapsed but the pair can still be
//     rotated, so the connection is not over.
//  4. expired     — nothing can be done with the row.
func TokenStateOf(f TokenFacts, now time.Time) TokenState {
	switch {
	case f.RevokedAt != nil:
		return TokenRevoked
	case now.Before(f.AccessExpiresAt):
		return TokenLive
	case tokenRefreshUsable(f, now):
		return TokenRefreshable
	default:
		return TokenExpired
	}
}

// TokenEvent is something that happens TO a token pair. These are the real
// events of the flow, not the HTTP routes that carry them: GET /mcp and
// /v1/rest/* both authenticate with the access token, and POST /oauth/token is
// a refresh.
type TokenEvent string

const (
	// TokenEventUseAccess: a request authenticates with the access token.
	TokenEventUseAccess TokenEvent = "use_access"
	// TokenEventRefresh: the refresh token is redeemed at the token endpoint.
	// Rotation is single-use — the row being refreshed is revoked as it is
	// replaced.
	TokenEventRefresh TokenEvent = "refresh"
	// TokenEventList: the pair appears in a dashboard inventory (connected
	// apps / connected agents). Not an error surface: a refused list means the
	// row is omitted, so its answer is always TokenReasonNone.
	TokenEventList TokenEvent = "list"
	// TokenEventRevoke: the credential is revoked (RFC 7009, the dashboard's
	// Disconnect/Revoke, or "revoke all").
	TokenEventRevoke TokenEvent = "revoke"
	// TokenEventExpire: a deadline passes. Nobody calls it; the derivation
	// applies it, and the table says where it leaves each state.
	TokenEventExpire TokenEvent = "expire"
)

// TokenReason is the answer the caller gets. The two OAuth vocabularies are
// used verbatim: `invalid_token` is what a resource server says (RFC 6750, and
// what the /mcp and REST resolvers answer, 401), `invalid_grant` is what the
// token endpoint says (RFC 6749 §5.2, 400).
type TokenReason string

const (
	// TokenReasonNone: nothing to say — the event was applied, or the refusal
	// is a silent one (a row that is simply not listed, an idempotent revoke).
	TokenReasonNone TokenReason = ""
	// TokenReasonInvalidToken: the access token cannot authenticate this
	// request (revoked, lapsed, or a pair with nothing left).
	TokenReasonInvalidToken TokenReason = "invalid_token"
	// TokenReasonInvalidGrant: the refresh token cannot be redeemed.
	TokenReasonInvalidGrant TokenReason = "invalid_grant"
)

// TokenStep is what the table says about one (state, event) cell.
type TokenStep struct {
	// Allowed reports whether the event may be applied in this state.
	Allowed bool
	// Next is the state after the event.
	Next TokenState
	// Answer is what the caller is told: for a refused step the reason, for an
	// allowed one TokenReasonNone unless the event itself has something to say.
	Answer TokenReason
}

// tokenTable is the whole matrix: four states × five events, every cell spelled
// out. It is a map rather than a switch so TokenTransitionAll — and the matrix
// test — can walk it, and so a state added without its row is a caught hole
// instead of a silent default.
var tokenTable = map[TokenState]map[TokenEvent]TokenStep{
	TokenLive: {
		TokenEventUseAccess: {Allowed: true, Next: TokenLive},
		// Rotation is a revocation of the row it replaces: the new pair is
		// inserted in the same transaction.
		TokenEventRefresh: {Allowed: true, Next: TokenRevoked},
		TokenEventList:    {Allowed: true, Next: TokenLive},
		TokenEventRevoke:  {Allowed: true, Next: TokenRevoked},
		// The ACCESS deadline passing. The pair is only dead if no refresh
		// credential survives; TokenStateOf re-derives that from
		// refresh_expires_at, so this cell names where the clock takes the row,
		// not a promise that it stops there.
		TokenEventExpire: {Allowed: true, Next: TokenRefreshable},
	},
	TokenRefreshable: {
		// The access token has lapsed: this is the arm a hand-written check
		// forgets to keep separate from a revocation, and it is why the answer
		// is invalid_token rather than "gone".
		TokenEventUseAccess: {Next: TokenRefreshable, Answer: TokenReasonInvalidToken},
		TokenEventRefresh:   {Allowed: true, Next: TokenRevoked},
		TokenEventList:      {Allowed: true, Next: TokenRefreshable},
		TokenEventRevoke:    {Allowed: true, Next: TokenRevoked},
		// The REFRESH deadline passing: nothing is left.
		TokenEventExpire: {Allowed: true, Next: TokenExpired},
	},
	TokenExpired: {
		TokenEventUseAccess: {Next: TokenExpired, Answer: TokenReasonInvalidToken},
		TokenEventRefresh:   {Next: TokenExpired, Answer: TokenReasonInvalidGrant},
		TokenEventList:      {Next: TokenExpired},
		// A dead pair can still be revoked; it costs nothing and closes the row.
		TokenEventRevoke: {Allowed: true, Next: TokenRevoked},
		// Already there.
		TokenEventExpire: {Next: TokenExpired},
	},
	TokenRevoked: {
		TokenEventUseAccess: {Next: TokenRevoked, Answer: TokenReasonInvalidToken},
		TokenEventRefresh:   {Next: TokenRevoked, Answer: TokenReasonInvalidGrant},
		TokenEventList:      {Next: TokenRevoked},
		// RFC 7009 §2.2: revoking an already-revoked token is a successful
		// no-op, not an error. The wire answers 200 either way, which is why
		// this cell refuses the write and says nothing.
		TokenEventRevoke: {Next: TokenRevoked},
		TokenEventExpire: {Next: TokenRevoked},
	},
}

// TokenStates lists every state, in the life of a pair.
func TokenStates() []TokenState {
	return []TokenState{TokenLive, TokenRefreshable, TokenExpired, TokenRevoked}
}

// TokenEvents lists every event, in the order a reader should show them.
func TokenEvents() []TokenEvent {
	return []TokenEvent{
		TokenEventUseAccess, TokenEventRefresh, TokenEventList,
		TokenEventRevoke, TokenEventExpire,
	}
}

// TokenTransition asks the table. An unknown state or event is a programming
// error, not a runtime condition, and answers a refusal (never a permissive
// zero value) so a typo can only ever be too strict.
func TokenTransition(state TokenState, event TokenEvent) TokenStep {
	if row, ok := tokenTable[state]; ok {
		if step, ok := row[event]; ok {
			return step
		}
	}
	return TokenStep{Next: state, Answer: TokenReasonInvalidToken}
}

// TokenAccessStep is "may this row authenticate a request", which is the
// use_access cell of the table for the row's derived state.
func TokenAccessStep(f TokenFacts, now time.Time) TokenStep {
	return TokenTransition(TokenStateOf(f, now), TokenEventUseAccess)
}

// TokenRefreshStep is "may this refresh token be redeemed", and it carries the
// credential fact the table cannot see: a refresh credential that does not exist
// has no event at all, and one whose deadline has passed is refused even when
// the state is live.
//
// WHY THIS IS NOT JUST TokenTransition(state, refresh): state live + deadline
// passed is a real, common row (the access token outlives nothing, the refresh
// token's 30 days run out first) and the token endpoint has always answered
// invalid_grant for it. Readers still derive the STATE once; this one refusal
// remembers the fact that decides it.
func TokenRefreshStep(f TokenFacts, now time.Time) TokenStep {
	state := TokenStateOf(f, now)
	if !tokenRefreshUsable(f, now) {
		return TokenStep{Next: state, Answer: TokenReasonInvalidGrant}
	}
	return TokenTransition(state, TokenEventRefresh)
}

// TokenAvailableEvents is every event legal in this state, derived from the
// same table. The slice is never nil.
func TokenAvailableEvents(state TokenState) []TokenEvent {
	out := make([]TokenEvent, 0, len(tokenTable))
	for _, event := range TokenEvents() {
		if TokenTransition(state, event).Allowed {
			out = append(out, event)
		}
	}
	return out
}

// TokenAvailableEventNames is TokenAvailableEvents as the wire names.
func TokenAvailableEventNames(state TokenState) []string {
	events := TokenAvailableEvents(state)
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, string(e))
	}
	return out
}

// TokenRemainingAccessSeconds is the access token's life left, floored at zero.
// It answers the clock question on its own: the remaining time and the state are
// two different facts (a revoked pair can still have an hour on the clock).
func TokenRemainingAccessSeconds(f TokenFacts, now time.Time) int {
	left := f.AccessExpiresAt.Sub(now)
	if left <= 0 {
		return 0
	}
	return int(left / time.Second)
}

// TokenStatus is the whole answer in one call: the state and the three "may I"
// questions the readers actually ask, all three read off the one table.
type TokenStatus struct {
	State TokenState
	// AccessUsable: a request may authenticate with the access token right now.
	AccessUsable bool
	// RefreshUsable: the refresh token may still be redeemed, in this state and
	// given this credential's own deadline.
	RefreshUsable bool
	// Listed: the pair belongs in a dashboard inventory. A live access token or
	// a usable refresh credential is enough — a pair that merely needs a refresh
	// is a working connection, not a dead one.
	Listed           bool
	AccessExpiresAt  time.Time
	RefreshExpiresAt *time.Time
	// RemainingAccessSeconds is the access token's life left, floored at zero.
	RemainingAccessSeconds int
	// AvailableEvents is every event the table still allows.
	AvailableEvents []TokenEvent
}

// TokenStatusOf is TokenStatus from the one derivation — including the three
// booleans, which are read off the table rather than hand-written, so a reader
// cannot offer a capability the transition would refuse.
func TokenStatusOf(f TokenFacts, now time.Time) TokenStatus {
	state := TokenStateOf(f, now)
	return TokenStatus{
		State:                  state,
		AccessUsable:           TokenTransition(state, TokenEventUseAccess).Allowed,
		RefreshUsable:          TokenRefreshStep(f, now).Allowed,
		Listed:                 TokenTransition(state, TokenEventList).Allowed,
		AccessExpiresAt:        f.AccessExpiresAt,
		RefreshExpiresAt:       f.RefreshExpiresAt,
		RemainingAccessSeconds: TokenRemainingAccessSeconds(f, now),
		AvailableEvents:        TokenAvailableEvents(state),
	}
}
