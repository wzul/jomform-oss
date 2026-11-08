package mcpsrv

// In-app feedback MCP tools: registration + the credential-scope guard.
//
// WHY THIS FILE EXISTS. An adversarial review of the feedback feature found a
// real privilege-conflation hole, and these tests are what keep it closed.
//
// The hole: `userIDFor` resolves an API-KEY caller to its ACCOUNT OWNER's user
// id, because an API key carries no user of its own (see server.go
// AuthIdentity.UserID — "empty for API-key auth"). That is the right convention
// for account-scoped tools. A feedback thread is not account-scoped data — it
// belongs to the PERSON who filed it — so applying the fallback here silently
// converted an account-scoped credential into a person-scoped one. Concretely: a
// `manage` member of a victim's workspace can mint an API key on the victim's
// account (POST /v1/rest/keys is gated only on manage/admin), and that key would
// have read and written the victim's private conversations with JomForm. The
// super-admin branch had the same shape: `isSuperAdmin` judges an API-key caller
// by its account owner's email, so a key on a super-admin's account would have
// opened the entire triage queue.
//
// The guard: `feedbackScopeFor` refuses any caller with no USER identity. These
// tests assert that refusal on every one of the four tools, and — the part that
// makes it a guard rather than a restatement — assert that a user-bound identity
// (OAuth) is NOT refused, so the fix cannot be "make everything fail".

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// newFeedbackMCP builds a Server with the feedback deps unset, so any call that
// gets PAST the scope guard fails with "feedback system not configured"
// rather than panicking. That distinction is what lets these tests tell
// "refused for the right reason" from "never reached the check".
func newFeedbackMCP() *Server {
	return &Server{
		mcp:  mcp.NewServer(&mcp.Implementation{Name: "jomform", Version: "test"}, nil),
		deps: &Deps{},
	}
}

// TestFeedbackToolsRegistered verifies the four tools are discoverable over the
// MCP protocol (a handler that is never registered is invisible to a client, and
// the parity guard reads tools/list — not the source).
func TestFeedbackToolsRegistered(t *testing.T) {
	srv := newFeedbackMCP()
	srv.registerTools()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	cTransport, sTransport := mcp.NewInMemoryTransports()
	serverSession, err := srv.mcp.Connect(context.Background(), sTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	defer serverSession.Close()
	clientSession, err := client.Connect(context.Background(), cTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	defer clientSession.Close()

	res, err := clientSession.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	got := map[string]bool{}
	for _, tl := range res.Tools {
		got[tl.Name] = true
	}
	for _, name := range []string{
		"list_feedback", "get_feedback_thread", "reply_feedback", "set_feedback_status",
	} {
		if !got[name] {
			t.Errorf("feedback tool %q not registered by registerTools — the agent cannot see it", name)
		}
	}
}

// TestFeedbackToolsRefuseAnAPIKeyIdentity is the regression guard for the
// privilege-conflation hole: a caller with NO user identity (an API-key
// principal: AccountID set, UserID empty) must never obtain a feedback scope,
// whichever tool it calls and whatever the super-admin allowlist says.
//
// The super-admin variant is the sharper half: with the allowlist open, a scope
// built from the account owner would return the ADMIN (all-threads) scope and
// expose every user's thread.
func TestFeedbackToolsRefuseAnAPIKeyIdentity(t *testing.T) {
	// The crisp guard, asserted on the decision function itself: no user
	// identity ⇒ no scope. This is the line a regression would have to cross,
	// and it fails closed rather than falling back to the account owner.
	apiKeyIdentity := &AuthIdentity{AccountID: "acct-1", UserID: "", Scopes: []string{"readwrite"}}
	srvAdminOpen := newFeedbackMCP()
	srvAdminOpen.deps.SuperAdmin = func(string) bool { return true }
	// Even with the allowlist open, an API-key identity must not get the admin
	// scope (isSuperAdmin judges such a caller by its account owner's email).
	for _, srv := range []*Server{newFeedbackMCP(), srvAdminOpen} {
		if sc, err := srv.feedbackScopeFor(WithAuthIdentity(context.Background(), apiKeyIdentity)); err == nil {
			t.Errorf("feedbackScopeFor returned scope %+v for an API-key identity, want an error — an API "+
				"key carries no user, so mapping it to its account owner's private feedback is exactly the "+
				"leak this refuses", sc)
		}
	}
	// A hand-built zero scope must also be refused by the store, so the guard is
	// not the only thing standing between a mistake and an unfiltered read.
	if _, _, err := dbstore.NewFeedbackThreadStore(nil).ListFilteredPage(
		context.Background(), dbstore.FeedbackScope{}, "", 1, 20); err == nil {
		t.Error("the store accepted an empty FeedbackScope — an unfiltered scope must fail closed")
	}

	req := &mcp.CallToolRequest{}
	refused := func(err error) bool {
		// Refused means "an authorization decision stopped it", NOT "the store
		// is missing" — the distinction is what proves the guard ran first.
		return err != nil && !strings.Contains(err.Error(), "tidak dikonfigurasi")
	}

	for _, tc := range []struct {
		name            string
		accountID       string
		superAdminAllow bool
	}{
		{"account-bound with no allowlist", "acct-1", false},
		{"account-bound whose owner is a super-admin", "acct-1", true},
		{"no identity at all", "", true},
	} {
		srv := newFeedbackMCP()
		if tc.superAdminAllow {
			srv.deps.SuperAdmin = func(string) bool { return true }
		}
		ctx := WithAuthIdentity(context.Background(),
			&AuthIdentity{AccountID: tc.accountID, UserID: "", Scopes: []string{"readwrite"}})

		if _, _, err := srv.listFeedback(ctx, req, listFeedbackIn{}); !refused(err) {
			t.Errorf("list_feedback (%s): err = %v — an API-key caller must be refused before the store, "+
				"never mapped to its account owner's threads", tc.name, err)
		}
		if _, _, err := srv.getFeedbackThread(ctx, req, getFeedbackThreadIn{ThreadID: "any"}); !refused(err) {
			t.Errorf("get_feedback_thread (%s): err = %v, want a refusal", tc.name, err)
		}
		if _, _, err := srv.replyFeedback(ctx, req,
			replyFeedbackIn{ThreadID: "any", Body: "satu mesej yang cukup panjang"}); !refused(err) {
			t.Errorf("reply_feedback (%s): err = %v — a key must not write into another person's thread",
				tc.name, err)
		}
		// set_feedback_status is super-admin gated as well, so an API-key caller
		// may be stopped by either guard; what matters is that it is refused and
		// never reaches the store with an admin scope.
		if _, _, err := srv.setFeedbackStatus(ctx, req,
			setFeedbackStatusIn{ThreadID: "any", Status: "resolved"}); !refused(err) {
			t.Errorf("set_feedback_status (%s): err = %v — a key whose account owner is a super-admin must "+
				"not open the triage queue", tc.name, err)
		}
	}
}

// TestFeedbackToolsAcceptAUserBoundIdentity is the OTHER half of the guard, and
// it is what keeps the fix honest.
//
// A refusal test alone can be satisfied by breaking the feature: "make every
// call error" would pass it. So this asserts that a user-bound identity (what an
// OAuth token produces) is NOT refused for the scope reason — it proceeds to the
// store and fails only because the store is unconfigured here.
//
// Without this, deleting the whole feedback feature would leave the refusal
// tests green. With it, the refusal has to be specific to the credential, not to
// the request.
func TestFeedbackToolsAcceptAUserBoundIdentity(t *testing.T) {
	srv := newFeedbackMCP()
	ctx := WithAuthIdentity(context.Background(),
		&AuthIdentity{AccountID: "acct-1", UserID: "user-1", Scopes: []string{"readwrite"}})
	req := &mcp.CallToolRequest{}

	_, _, err := srv.listFeedback(ctx, req, listFeedbackIn{})
	if err == nil {
		t.Fatal("list_feedback succeeded with an unconfigured store")
	}
	if strings.Contains(err.Error(), "an API key cannot access it") {
		t.Errorf("list_feedback refused a USER-BOUND identity: %v — the API-key refusal must key on the "+
			"missing user, not disable the tool for everyone (a tool that always errors passes the "+
			"refusal test while delivering no feature)", err)
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Errorf("list_feedback err = %v, want it to reach the store check (store is nil here)", err)
	}

	// And the same identity is accepted as an author for a reply.
	_, _, err = srv.replyFeedback(ctx, req,
		replyFeedbackIn{ThreadID: "any", Body: "satu mesej yang cukup panjang"})
	if err != nil && strings.Contains(err.Error(), "tidak boleh mengaksesnya") {
		t.Errorf("reply_feedback refused a user-bound identity: %v", err)
	}
}
