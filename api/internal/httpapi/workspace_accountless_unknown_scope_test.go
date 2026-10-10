// The SECOND route to the workspace-delete audit attribution decision point: an
// UNKNOWN `?workspace=<slug>`.
//
// The scoped route is already pinned (t_0bb441ac /
// TestRESTWorkspaceDeleteEventScopedToAStrangersWorkspaceStaysOwned): the caller
// members a stranger's workspace and scopes to its slug, so
// resolveSessionAccount resolves the STRANGER's account directly. This file
// covers the other way that account gets resolved — a scope slug that names
// nothing at all, which sends resolveSessionAccount down its fallback walk:
//
//	Accounts.ByOwner(caller)  -> empty (this caller owns no account)
//	Workspaces.ByMember(caller) -> first candidate = the stranger's older
//	                               workspace, i.e. the STRANGER's account
//
// That walk was UNREACHABLE before this card: the caller's own workspace has no
// `accounts` row, so ByMember's strict `a.id` scan hit NULL, returned
// `(nil, "cannot scan NULL into *string")`, and resolveSessionAccount read the
// error as "no workspaces" — 401 for a signed-in user. With ByMember nullable
// and access() still strict the route becomes a 403 instead (workspaceAccessByID
// -> AccessByID on the account-less workspace). Both scans are fixed together;
// this test is what drives the end-to-end shape and would fail on either half.
//
// The expected end state is the tenancy guard's: the resolved account belongs to
// the stranger, who the caller does not OWN, and the caller has no live account
// of their own, so the event is DELIBERATELY SKIPPED and says so — the stranger's
// own audit read must show total=0.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

func TestRESTWorkspaceDeleteUnknownScopeWithAccountlessWorkspaceStaysOwned(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST workspace test in -short mode")
	}
	logs := wsEventLogs(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	sess := authTestValkey(t)
	h := trashRESTFixture(t, pool)
	h.Sessions = sess
	h.Users = dbstore.NewUserStore(pool)
	h.Events = dbstore.NewEventStore(pool)
	sp := dbstore.NewWorkspaceStore(pool)

	// The stranger's workspace is created FIRST, so it heads the list
	// Workspaces.ByMember returns (ORDER BY sp.created_at) — the fallback walk
	// looks there first.
	strangerWS, strangerUser := wsStrangerWorkspaceOwned(t, pool, "noacc-unknown")
	var strangerAcc string
	if err := pool.QueryRow(ctx,
		`SELECT id FROM accounts WHERE workspace_id=$1`, strangerWS.ID).Scan(&strangerAcc); err != nil {
		t.Fatalf("read stranger account: %v", err)
	}
	userID := mkWsUser(t, pool, "ws-noacc-unknown")
	wsJoinAsMember(t, pool, strangerWS.ID, userID)

	// The caller's OWN workspace — the one it deletes — carries NO account. It is
	// inserted directly because WorkspaceStore.Create always links one, and the
	// account-less row is the whole point of the fixture.
	var doomedID, doomedSlug string
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'Caller account-less', 'noacc-caller-' || substr(md5(random()::text),1,8))
		 RETURNING id, slug`, userID).Scan(&doomedID, &doomedSlug); err != nil {
		t.Fatalf("create account-less workspace: %v", err)
	}
	// Preconditions, asserted rather than assumed. (1) the caller owns NO account
	// at all, so the fallback's Accounts.ByOwner leg yields nothing and ByMember
	// is what resolves the account; (2) that walk's FIRST candidate is the
	// stranger's account, so the guard has something to refuse.
	//
	// These are reported with Errorf rather than Fatalf on purpose: the failing
	// query IS this defect, and aborting here would hide the request-level
	// outcome (401 pre-fix, 403 with only this half fixed) that the assertions
	// below exist to show.
	var mine int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM accounts WHERE owner_user_id=$1`, userID).Scan(&mine); err != nil {
		t.Fatalf("count the caller's accounts: %v", err)
	}
	if mine != 0 {
		t.Fatalf("fixture is not the defect shape: the caller owns %d account(s), so the "+
			"ByMember fallback would never be the resolver", mine)
	}
	walk, walkErr := sp.ByMember(ctx, userID)
	switch {
	case walkErr != nil:
		t.Errorf("premise: ByMember(caller) err = %v — the fallback walk this route needs "+
			"cannot even be walked (this is the defect, not the fixture)", walkErr)
	case len(walk) == 0 || len(walk[0].Accounts) == 0 || walk[0].Accounts[0] != strangerAcc:
		t.Errorf("premise: ByMember()[0].Accounts = %v, want the stranger's account %s first — "+
			"the fallback would not resolve a stranger's account on this fixture", walk, strangerAcc)
	}

	// The scope slug names nothing: that is what routes the request to the
	// fallback walk instead of resolving a workspace directly.
	unknownSlug := "noacc-missing-" + userID[:8]
	if _, ok, err := sp.AccessBySlug(ctx, unknownSlug, userID); err != nil || ok {
		t.Fatalf("fixture is not the defect shape: scope slug %q resolves (ok=%v err=%v)",
			unknownSlug, ok, err)
	}

	token, err := sess.Create(ctx, userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// The UI's own delete, with an unknown workspace scope on it.
	rec := sessionReq(t, h, token, http.MethodDelete,
		"/v1/workspaces/"+doomedID+"?workspace="+unknownSlug,
		`{"confirm_slug":"`+doomedSlug+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /v1/workspaces/{id}?workspace=<unknown> = %d (%s) — with both NULL "+
			"scans fixed the account-less workspace is listable and accessible, so this request "+
			"must reach the tenancy guard and be answered, not 401/403 on the way to it",
			rec.Code, rec.Body.String())
	}
	// The delete must actually have happened, or nothing below proves anything.
	var binned int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM workspaces WHERE id=$1 AND deleted_at IS NOT NULL`,
		doomedID).Scan(&binned); err != nil {
		t.Fatalf("count workspaces: %v", err)
	}
	if binned != 1 {
		t.Fatal("the workspace was not binned, so this test proves nothing")
	}
	// The stranger's audit log must be untouched: the resolved account came off
	// the ByMember walk, and it is not the caller's to file into.
	if n, props := wsDeleteEvents(t, strangerAcc); n != 0 {
		t.Errorf("the caller's workspace.deleted event was written into the STRANGER's account %s "+
			"(n=%d, properties=%v) — resolved through the unknown-slug fallback and accepted on "+
			"reachability instead of ownership", strangerAcc, n, props)
	}
	// Nor anywhere else: the caller has no live account of its own.
	var anywhere int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM events WHERE event_name='workspace.deleted' AND properties->>'workspace_id'=$1`,
		doomedID).Scan(&anywhere); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if anywhere != 0 {
		t.Errorf("workspace.deleted rows for the deleted workspace = %d, want 0 — the caller has "+
			"no account of its own, so the event must be skipped, not filed against another tenant",
			anywhere)
	}
	// The user-visible consequence, driven through the real endpoint: the
	// stranger reads their OWN audit log and finds nothing of the caller's.
	victimToken, err := sess.Create(ctx, strangerUser)
	if err != nil {
		t.Fatalf("create stranger session: %v", err)
	}
	read := sessionReq(t, h, victimToken, http.MethodGet,
		"/v1/rest/events?workspace="+strangerWS.Slug+"&event_name=workspace.deleted", "")
	if read.Code != http.StatusOK {
		t.Fatalf("the stranger's GET /v1/rest/events = %d (%s)", read.Code, read.Body.String())
	}
	var out struct {
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(read.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode the stranger's events listing %s: %v", read.Body.String(), err)
	}
	if out.Total != 0 {
		t.Errorf("the STRANGER's own audit read reports total=%d workspace.deleted row(s), want 0 — "+
			"the stranger reads the caller's delete out of their own audit log (%s)",
			out.Total, read.Body.String())
	}
	// The skip must be a stated decision, not a swallowed FK failure.
	if !strings.Contains(logs.String(), "workspace_deleted_event_skipped") {
		t.Errorf("the deliberate skip was not logged, so a swallowed failure would look the same; "+
			"audit log was:\n%s", logs.String())
	}
	assertNoFKEventRefusal(t, logs)
}
