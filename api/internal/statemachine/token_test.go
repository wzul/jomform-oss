package statemachine

// Matrix tests for the token-pair state machine (card t_df243a6e).
//
// WHAT THESE PIN. The derivation is the ONE place a token pair's state is
// decided, so the matrix is asserted twice on purpose: once as a (state × event)
// table of its own, written out independently in this file, and once as the
// product of the FACTS that produce each state. A cell changed in token.go makes
// the first test fail; a precedence rule changed in TokenStateOf makes the second
// and third fail.
//
// The arms a hand-written suite forgets are named tests, not comments:
//
//	TestTokenRevokedOutranksAnUnexpiredAccessToken   revoked-but-unexpired
//	TestTokenRefreshDeadlineIsSeparateFromTheState   refresh-expired, access live
//	TestTokenRefreshCredentialIsRequired             no refresh credential at all
//	TestTokenNilRefreshDeadlineIsUnbounded           legacy row, no deadline

import (
	"reflect"
	"testing"
	"time"
)

// tokenExpectation is this file's own copy of the matrix. It is deliberately a
// second spelling: if token.go's table and this disagree, the test fails.
var tokenExpectation = map[TokenState]map[TokenEvent]TokenStep{
	TokenLive: {
		TokenEventUseAccess: {Allowed: true, Next: TokenLive},
		TokenEventRefresh:   {Allowed: true, Next: TokenRevoked},
		TokenEventList:      {Allowed: true, Next: TokenLive},
		TokenEventRevoke:    {Allowed: true, Next: TokenRevoked},
		TokenEventExpire:    {Allowed: true, Next: TokenRefreshable},
	},
	TokenRefreshable: {
		TokenEventUseAccess: {Next: TokenRefreshable, Answer: TokenReasonInvalidToken},
		TokenEventRefresh:   {Allowed: true, Next: TokenRevoked},
		TokenEventList:      {Allowed: true, Next: TokenRefreshable},
		TokenEventRevoke:    {Allowed: true, Next: TokenRevoked},
		TokenEventExpire:    {Allowed: true, Next: TokenExpired},
	},
	TokenExpired: {
		TokenEventUseAccess: {Next: TokenExpired, Answer: TokenReasonInvalidToken},
		TokenEventRefresh:   {Next: TokenExpired, Answer: TokenReasonInvalidGrant},
		TokenEventList:      {Next: TokenExpired},
		TokenEventRevoke:    {Allowed: true, Next: TokenRevoked},
		TokenEventExpire:    {Next: TokenExpired},
	},
	TokenRevoked: {
		TokenEventUseAccess: {Next: TokenRevoked, Answer: TokenReasonInvalidToken},
		TokenEventRefresh:   {Next: TokenRevoked, Answer: TokenReasonInvalidGrant},
		TokenEventList:      {Next: TokenRevoked},
		TokenEventRevoke:    {Next: TokenRevoked},
		TokenEventExpire:    {Next: TokenRevoked},
	},
}

// TestTokenTransitionMatrixIsTheWholeProduct walks every (state, event) cell and
// compares it with this file's independent copy — including the cells that must
// be a REFUSAL, so a permissive zero value cannot slip in.
func TestTokenTransitionMatrixIsTheWholeProduct(t *testing.T) {
	states := TokenStates()
	events := TokenEvents()
	if len(states) != 4 || len(events) != 5 {
		t.Fatalf("state machine shape changed: %d states x %d events", len(states), len(events))
	}
	if len(tokenExpectation) != len(states) {
		t.Fatalf("expectation table has %d states, machine has %d", len(tokenExpectation), len(states))
	}
	if len(TokenAvailableEvents(TokenLive)) == 0 {
		t.Fatal("TokenAvailableEvents must never be empty for a live pair")
	}
	for _, state := range states {
		row, ok := tokenExpectation[state]
		if !ok {
			t.Fatalf("state %q has no expectation row — the table was extended without this test", state)
		}
		if len(row) != len(events) {
			t.Fatalf("state %q expects %d cells, machine has %d events", state, len(row), len(events))
		}
		for _, event := range events {
			want, ok := row[event]
			if !ok {
				t.Fatalf("state %q has no expectation for event %q", state, event)
			}
			got := TokenTransition(state, event)
			if got != want {
				t.Errorf("TokenTransition(%q, %q) = %+v, want %+v", state, event, got, want)
			}
		}
	}
	// Every event must be reachable from at least one state, or the table has a
	// column nobody can ever use.
	for _, event := range events {
		reachable := false
		for _, state := range states {
			if TokenTransition(state, event).Allowed {
				reachable = true
				break
			}
		}
		if !reachable {
			t.Errorf("event %q is not allowed in any state", event)
		}
	}
}

// TestTokenStateOfProduct is the derivation over the full fact product:
// revoked × refresh credential × refresh deadline × access deadline.
func TestTokenStateOfProduct(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Minute)
	future := now.Add(time.Minute)
	revoked := now.Add(-time.Hour)

	type deadline int
	const (
		none deadline = iota
		unexpired
		expired
	)

	cases := []struct {
		name       string
		revokedAt  *time.Time
		hasRefresh bool
		refresh    deadline
		access     deadline
		want       TokenState
	}{
		// ── not revoked ──────────────────────────────────────────────────────
		{"live, access unexpired, refresh unexpired", nil, true, unexpired, unexpired, TokenLive},
		{"live, access unexpired, refresh expired", nil, true, expired, unexpired, TokenLive},
		{"live, access unexpired, refresh with no deadline", nil, true, none, unexpired, TokenLive},
		{"live, access unexpired, no refresh credential", nil, false, none, unexpired, TokenLive},
		{"refreshable, access expired, refresh unexpired", nil, true, unexpired, expired, TokenRefreshable},
		{"refreshable, access expired, refresh with no deadline", nil, true, none, expired, TokenRefreshable},
		{"expired, access expired, refresh expired", nil, true, expired, expired, TokenExpired},
		{"expired, access expired, no refresh credential", nil, false, none, expired, TokenExpired},
		{"expired, access expired, deadline ignored without a credential", nil, false, unexpired, expired, TokenExpired},
		// ── revoked outranks every clock ─────────────────────────────────────
		{"revoked with an unexpired access token", &revoked, true, unexpired, unexpired, TokenRevoked},
		{"revoked with an unexpired refresh token", &revoked, true, unexpired, expired, TokenRevoked},
		{"revoked with everything alive", &revoked, true, none, unexpired, TokenRevoked},
		{"revoked with nothing left", &revoked, false, none, expired, TokenRevoked},
	}

	deadlinePtr := func(d deadline) *time.Time {
		switch d {
		case unexpired:
			return &future
		case expired:
			return &past
		default:
			return nil
		}
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			facts := TokenFacts{
				HasRefresh:       c.hasRefresh,
				AccessExpiresAt:  map[deadline]time.Time{unexpired: future, expired: past}[c.access],
				RefreshExpiresAt: deadlinePtr(c.refresh),
				RevokedAt:        c.revokedAt,
			}
			if got := TokenStateOf(facts, now); got != c.want {
				t.Fatalf("TokenStateOf = %q, want %q (facts %+v)", got, c.want, facts)
			}
		})
	}
}

// TestTokenRevokedOutranksAnUnexpiredAccessToken is the arm a hand-written check
// written as "access_expires_at > now()" gets wrong.
func TestTokenRevokedOutranksAnUnexpiredAccessToken(t *testing.T) {
	now := time.Now().UTC()
	revoked := now.Add(-time.Second)
	facts := TokenFacts{
		HasRefresh:       true,
		AccessExpiresAt:  now.Add(time.Hour),
		RefreshExpiresAt: func() *time.Time { t := now.Add(24 * time.Hour); return &t }(),
		RevokedAt:        &revoked,
	}
	if got := TokenStateOf(facts, now); got != TokenRevoked {
		t.Fatalf("state = %q, want revoked", got)
	}
	step := TokenAccessStep(facts, now)
	if step.Allowed {
		t.Fatal("a revoked pair with an unexpired access token must not authenticate")
	}
	if step.Answer != TokenReasonInvalidToken {
		t.Fatalf("answer = %q, want invalid_token", step.Answer)
	}
}

// TestTokenRefreshDeadlineIsSeparateFromTheState pins the second forgotten arm:
// the refresh credential's own deadline is a FACT, not a state. A live pair whose
// refresh token has expired still authenticates (the access token is fine) and
// can no longer be refreshed.
func TestTokenRefreshDeadlineIsSeparateFromTheState(t *testing.T) {
	now := time.Now().UTC()
	refreshPast := now.Add(-time.Minute)
	facts := TokenFacts{
		HasRefresh:       true,
		AccessExpiresAt:  now.Add(time.Hour),
		RefreshExpiresAt: &refreshPast,
	}
	if got := TokenStateOf(facts, now); got != TokenLive {
		t.Fatalf("state = %q, want live (the access token is unexpired)", got)
	}
	if !TokenAccessStep(facts, now).Allowed {
		t.Fatal("a live access token must authenticate even when the refresh token expired")
	}
	step := TokenRefreshStep(facts, now)
	if step.Allowed {
		t.Fatal("a refresh credential past its deadline must not be redeemed")
	}
	if step.Answer != TokenReasonInvalidGrant {
		t.Fatalf("answer = %q, want invalid_grant", step.Answer)
	}
	status := TokenStatusOf(facts, now)
	if !status.AccessUsable || status.RefreshUsable {
		t.Fatalf("status = %+v, want access usable and refresh not usable", status)
	}
}

// TestTokenRefreshCredentialIsRequired pins that a pair with no refresh token at
// all can never be refreshed, even while its access token is live.
func TestTokenRefreshCredentialIsRequired(t *testing.T) {
	now := time.Now().UTC()
	facts := TokenFacts{AccessExpiresAt: now.Add(time.Hour)}
	if step := TokenRefreshStep(facts, now); step.Allowed {
		t.Fatal("a pair with no refresh credential must not be refreshable")
	}
	if facts.HasRefresh {
		t.Fatal("zero-value facts must report no refresh credential")
	}
}

// TestTokenNilRefreshDeadlineIsUnbounded pins the legacy-row reading: a refresh
// credential with no deadline is refreshable, which is what RefreshGrant has
// always accepted and what the connected-apps query used to deny.
func TestTokenNilRefreshDeadlineIsUnbounded(t *testing.T) {
	now := time.Now().UTC()
	facts := TokenFacts{
		HasRefresh:      true,
		AccessExpiresAt: now.Add(-time.Minute),
	}
	if got := TokenStateOf(facts, now); got != TokenRefreshable {
		t.Fatalf("state = %q, want refreshable (an unbounded refresh credential)", got)
	}
	if !TokenRefreshStep(facts, now).Allowed {
		t.Fatal("a refresh credential with no deadline must be redeemable")
	}
	if !TokenStatusOf(facts, now).Listed {
		t.Fatal("a pair that can still be refreshed must stay listed")
	}
}

// TestTokenStatusIsTheTable pins that the three booleans are read off the table,
// not written by hand: a status that offers a capability the transition refuses
// is the disagreement this card removes.
func TestTokenStatusIsTheTable(t *testing.T) {
	now := time.Now().UTC()
	past := now.Add(-time.Minute)
	future := now.Add(time.Hour)
	revoked := now.Add(-time.Hour)

	cases := []struct {
		name    string
		facts   TokenFacts
		state   TokenState
		access  bool
		refresh bool
		listed  bool
	}{
		{"live", TokenFacts{HasRefresh: true, AccessExpiresAt: future, RefreshExpiresAt: &future}, TokenLive, true, true, true},
		{"live, refresh expired", TokenFacts{HasRefresh: true, AccessExpiresAt: future, RefreshExpiresAt: &past}, TokenLive, true, false, true},
		{"live, no refresh", TokenFacts{AccessExpiresAt: future}, TokenLive, true, false, true},
		{"refreshable", TokenFacts{HasRefresh: true, AccessExpiresAt: past, RefreshExpiresAt: &future}, TokenRefreshable, false, true, true},
		{"expired", TokenFacts{HasRefresh: true, AccessExpiresAt: past, RefreshExpiresAt: &past}, TokenExpired, false, false, false},
		{"expired, no refresh", TokenFacts{AccessExpiresAt: past}, TokenExpired, false, false, false},
		{"revoked, unexpired access", TokenFacts{HasRefresh: true, AccessExpiresAt: future, RefreshExpiresAt: &future, RevokedAt: &revoked}, TokenRevoked, false, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status := TokenStatusOf(c.facts, now)
			if status.State != c.state {
				t.Fatalf("state = %q, want %q", status.State, c.state)
			}
			if status.AccessUsable != c.access {
				t.Errorf("AccessUsable = %v, want %v", status.AccessUsable, c.access)
			}
			if status.RefreshUsable != c.refresh {
				t.Errorf("RefreshUsable = %v, want %v", status.RefreshUsable, c.refresh)
			}
			if status.Listed != c.listed {
				t.Errorf("Listed = %v, want %v", status.Listed, c.listed)
			}
			// The booleans must be the same answer the table gives, so a reader
			// cannot offer something the writer would refuse.
			if status.AccessUsable != TokenTransition(status.State, TokenEventUseAccess).Allowed {
				t.Error("AccessUsable disagrees with the use_access cell")
			}
			if status.Listed != TokenTransition(status.State, TokenEventList).Allowed {
				t.Error("Listed disagrees with the list cell")
			}
			if status.AvailableEvents == nil {
				t.Error("AvailableEvents must never be nil")
			}
			for _, want := range []TokenEvent{TokenEventUseAccess, TokenEventRefresh, TokenEventList, TokenEventRevoke} {
				var got bool
				for _, e := range status.AvailableEvents {
					if e == want {
						got = true
					}
				}
				// A listed pair allows the revoke; a dead one does not.
				if want == TokenEventRevoke && got != (status.State != TokenRevoked) {
					t.Errorf("revoke availability = %v in state %q", got, status.State)
				}
			}
		})
	}
}

// TestTokenRemainingSecondsFloorsAtZero pins the clock answer, which is a
// different fact from the state (a revoked pair keeps its remaining time).
func TestTokenRemainingSecondsFloorsAtZero(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	facts := TokenFacts{AccessExpiresAt: now.Add(90 * time.Second)}
	if got := TokenRemainingAccessSeconds(facts, now); got != 90 {
		t.Fatalf("remaining = %d, want 90", got)
	}
	revoked := now.Add(-time.Minute)
	facts.RevokedAt = &revoked
	if got := TokenRemainingAccessSeconds(facts, now); got != 90 {
		t.Fatalf("remaining after revocation = %d, want 90 (the revocation does not shorten the clock)", got)
	}
	facts.AccessExpiresAt = now.Add(-time.Second)
	if got := TokenRemainingAccessSeconds(facts, now); got != 0 {
		t.Fatalf("remaining past the deadline = %d, want 0", got)
	}
}

// TestTokenTransitionRefusesAnUnknownStateOrEvent pins the fail-closed default: a
// typo in a state or event name can only ever be too strict.
func TestTokenTransitionRefusesAnUnknownStateOrEvent(t *testing.T) {
	for _, step := range []TokenStep{
		TokenTransition(TokenState("nope"), TokenEventUseAccess),
		TokenTransition(TokenLive, TokenEvent("nope")),
		TokenTransition(TokenState(""), TokenEvent("")),
	} {
		if step.Allowed {
			t.Fatalf("an unknown cell must be a refusal, got %+v", step)
		}
		if step.Answer != TokenReasonInvalidToken {
			t.Fatalf("an unknown cell must answer invalid_token, got %q", step.Answer)
		}
	}
}

// TestTokenAvailableEventNamesAreTheWireNames keeps the JSON helper honest.
func TestTokenAvailableEventNamesAreTheWireNames(t *testing.T) {
	names := TokenAvailableEventNames(TokenLive)
	want := []string{"use_access", "refresh", "list", "revoke", "expire"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("live event names = %v, want %v", names, want)
	}
	if len(TokenAvailableEventNames(TokenRevoked)) != 0 {
		t.Fatal("a revoked pair must offer no events")
	}
}
