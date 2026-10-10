package mcpsrv

// The MCP twin of the REST `workspace.deleted` defect.
//
// `mcp.tool_call` history rows are attributed to the account a call resolved
// against, and `events.account_id` is `REFERENCES accounts(id) ON DELETE
// CASCADE`. delete_workspace destroys that very account, so the history write
// for a delete_workspace call was refused by Postgres (SQLSTATE 23503) — and
// because recordMCPEvent is best-effort that refusal was swallowed to a log
// line. The tool still reported success to the agent; the call simply vanished
// from Settings → MCP history.
//
// This test drives the REAL tool handler with a user-bound (OAuth-shaped)
// identity whose call scope names the workspace being deleted, then asserts the
// history row exists and is attributed to an account that survived. It fails on
// the pre-fix code with the FK error in the log and zero rows.
//
// Skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// mcpDeleteEventFixture builds a server whose deps include the events store,
// plus a user with two empty workspaces. It returns the server, the call
// context (user-bound identity, like an OAuth token) and the ids involved.
//
// The call scope is resolved the way addTool does it, so the test covers the
// real path rather than a hand-built CallScope.
func mcpDeleteEventFixture(t *testing.T, tag string) (
	srv *Server, callCtx context.Context, userID, doomedAccID, survivorAccID, doomedID, doomedSlug string,
) {
	srv, callCtx, userID, doomedAccID, survivorAccID, doomedID, doomedSlug, _ =
		mcpDeleteEventFixtureWithStranger(t, tag)
	return srv, callCtx, userID, doomedAccID, survivorAccID, doomedID, doomedSlug
}

// mcpDeleteEventFixtureWithStranger is mcpDeleteEventFixture plus the account of a
// STRANGER's workspace the caller is only a MEMBER of. That account is the
// candidate class the surviving-account fallback must never use, and it is
// created BEFORE the caller's own workspaces so it heads the list
// Workspaces.ByMember returns (ORDER BY sp.created_at) — exactly where the
// pre-rework fallback looked first.
func mcpDeleteEventFixtureWithStranger(t *testing.T, tag string) (
	srv *Server, callCtx context.Context, userID, doomedAccID, survivorAccID, doomedID, doomedSlug, strangerAccID string,
) {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Open(t)
	sp := dbstore.NewWorkspaceStore(pool)

	_, strangerUserID := scopeTestAccount(t, pool, "mcpev-stranger-"+tag)
	strangerWS, err := sp.Create(ctx, strangerUserID, "Stranger "+tag, "mcpev-stranger-"+tag+"-"+strangerUserID[:6])
	if err != nil {
		t.Fatalf("create stranger workspace: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, strangerWS.ID).Scan(&strangerAccID); err != nil {
		t.Fatalf("read stranger account: %v", err)
	}

	// The caller gets a bare user row, NOT scopeTestAccount: that helper also
	// inserts a standalone account, which would be an extra account of the
	// caller's own and would make "which surviving account was chosen" ambiguous
	// (Accounts.ByOwner is ordered by created_at, so the stray one would win).
	var userID2 string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ('mcpev-`+tag+`-' || md5(random()::text) || '@example.test', 'it') RETURNING id`,
	).Scan(&userID2); err != nil {
		t.Fatalf("create caller user: %v", err)
	}
	userID = userID2
	doomed, err := sp.Create(ctx, userID, "MCP Event One", "mcpev-"+tag+"-one-"+userID[:6])
	if err != nil {
		t.Fatalf("create workspace 1: %v", err)
	}
	survivor, err := sp.Create(ctx, userID, "MCP Event Two", "mcpev-"+tag+"-two-"+userID[:6])
	if err != nil {
		t.Fatalf("create workspace 2: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, doomed.ID).Scan(&doomedAccID); err != nil {
		t.Fatalf("read doomed account: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, survivor.ID).Scan(&survivorAccID); err != nil {
		t.Fatalf("read surviving account: %v", err)
	}
	// Membership — the ONLY relationship that makes the stranger's account
	// reachable to this caller, and never an ownership relation.
	if err := dbstore.NewWorkspaceMemberStore(pool).
		AddMember(ctx, strangerWS.ID, userID, dbstore.RoleReadOnly); err != nil {
		t.Fatalf("join stranger workspace: %v", err)
	}

	srv = New(&Deps{
		Forms:      formsFor(pool),
		Accounts:   dbstore.NewAccountStore(pool),
		Keys:       dbstore.NewAPIKeyStore(pool),
		Workspaces: sp,
		Products:   dbstore.NewProductStore(pool),
		Sales:      dbstore.NewSaleStore(pool),
		Subs:       dbstore.NewSubscriptionStore(pool),
		Users:      dbstore.NewUserStore(pool),
		Events:     dbstore.NewEventStore(pool),
	}, nil, nil, nil)

	// A user-bound identity with no account_id is exactly what an OAuth bearer
	// produces (server.go AuthIdentity.UserID), so resolveScope takes the
	// unscoped branch and returns the caller's accounts across workspaces.
	callCtx = WithAuthIdentity(ctx, &AuthIdentity{UserID: userID, Scopes: []string{"readwrite"}})
	scope, err := srv.resolveScope(callCtx, []byte(`{"workspace_id":"`+doomed.ID+`"}`))
	if err != nil {
		t.Fatalf("resolveScope: %v", err)
	}
	// Precondition: the call scope names the doomed account. If that stops being
	// true the test has stopped reproducing the defect, so say so rather than
	// passing quietly.
	if len(scope.Accounts) != 1 || scope.Accounts[0] != doomedAccID {
		t.Fatalf("call scope = %v, want exactly the doomed account %s", scope.Accounts, doomedAccID)
	}
	// Precondition: the stranger's account is the first candidate ByMember would
	// offer — the defect shape. If ordering changes, fail loudly here.
	mine, err := sp.ByMember(ctx, userID)
	if err != nil {
		t.Fatalf("ByMember: %v", err)
	}
	if len(mine) == 0 || len(mine[0].Accounts) == 0 || mine[0].Accounts[0] != strangerAccID {
		t.Fatalf("fixture is not the defect shape: ByMember()[0].Accounts = %v, want the stranger's account %s first",
			mine, strangerAccID)
	}
	callCtx = withCallScope(callCtx, scope)
	return srv, callCtx, userID, doomedAccID, survivorAccID, doomed.ID, doomed.Slug, strangerAccID
}

// TestMCPDeleteWorkspaceHistoryEventLands is the regression test: the
// delete_workspace call must leave its mcp.tool_call history row, and the row
// must point at an account that still exists.
func TestMCPDeleteWorkspaceHistoryEventLands(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	srv, callCtx, userID, doomedAccID, survivorAccID, doomedID, doomedSlug := mcpDeleteEventFixture(t, "land")

	_, out, err := srv.deleteWorkspaceMCP(callCtx, nil, deleteWorkspaceIn{
		WorkspaceID: doomedID, ConfirmSlug: doomedSlug,
	})
	if err != nil {
		t.Fatalf("delete_workspace: %v", err)
	}
	if out["status"] != "trashed" {
		t.Fatalf("delete_workspace returned %v, want status=trashed", out)
	}
	// The workspace really went to the BIN — otherwise this test proves nothing.
	// Under the trash the equivalent pre-condition is `deleted_at IS NOT NULL`
	// rather than "the row is gone", and the mcp.tool_call history row must
	// still land on an account that exists (the bin destroys nothing).
	var exists int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM workspaces WHERE id=$1 AND deleted_at IS NOT NULL`, doomedID).Scan(&exists); err != nil {
		t.Fatalf("count workspaces: %v", err)
	}
	if exists != 1 {
		t.Fatal("the workspace was not trashed, so this test proves nothing")
	}
	// The scope account SURVIVES the move — which is exactly why the history
	// write can be attributed to it, and why the old FK defect is unreachable
	// on this route now.
	if acc, err := dbstore.NewAccountStore(pool).ByID(ctx, doomedAccID); err != nil || acc == nil {
		t.Fatalf("the binned workspace's account %s is gone (err=%v); the trash must keep every "+
			"row, and that surviving row is what the history event is attributed to", doomedAccID, err)
	}

	// addTool records the history row AFTER the handler returns.
	srv.recordMCPEvent(callCtx, "delete_workspace", false)

	var n int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM events WHERE event_name='mcp.tool_call'
		   AND user_id=$1 AND properties->>'tool'='delete_workspace'`, userID).Scan(&n); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if n != 1 {
		t.Fatalf("mcp.tool_call rows for delete_workspace = %d, want 1 — the call must be "+
			"recorded in Settings → MCP history", n)
	}
	var accountID string
	if err := pool.QueryRow(ctx,
		`SELECT account_id FROM events WHERE event_name='mcp.tool_call'
		   AND user_id=$1 AND properties->>'tool'='delete_workspace'`, userID).Scan(&accountID); err != nil {
		t.Fatalf("read event account_id: %v", err)
	}
	// The row is attributed to the caller's OTHER live account, not to the binned
	// one. Both rows still exist under the trash, so "it exists" is not the test:
	// the history is read through the account scope, and an event parked inside
	// the workspace that was just put away would be invisible to the caller — the
	// bin's version of the swallowed-FK defect.
	if accountID == doomedAccID {
		t.Errorf("the history row is attributed to the BINNED workspace's account %s — that row "+
			"exists but is parked in the trash, so the caller's own history would not show the call", accountID)
	}
	if accountID != survivorAccID {
		t.Errorf("the history row is attributed to %s, want the caller's live account %s", accountID, survivorAccID)
	}
	// And the account it names must be live.
	var live bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM accounts WHERE id=$1)`, accountID).Scan(&live); err != nil {
		t.Fatalf("check account: %v", err)
	}
	if !live {
		t.Errorf("the history row's account %s does not exist", accountID)
	}
	// The properties still name the tool and the agent-facing account id.
	var propsAccount string
	if err := pool.QueryRow(ctx,
		`SELECT properties->>'account_id' FROM events WHERE event_name='mcp.tool_call'
		   AND user_id=$1 AND properties->>'tool'='delete_workspace'`, userID).Scan(&propsAccount); err != nil {
		t.Fatalf("read properties: %v", err)
	}
	if propsAccount != accountID {
		t.Errorf("properties.account_id = %s but the row's account_id = %s — they must agree",
			propsAccount, accountID)
	}
}

// TestMCPHistoryEventStillLandsForOrdinaryCalls is the control: an ordinary tool
// call (one that deletes nothing) must still be attributed to its own scope
// account, exactly as before. Without this, "always fall back to another
// account" would pass the test above while breaking history attribution.
func TestMCPHistoryEventStillLandsForOrdinaryCalls(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	srv, callCtx, userID, doomedAccID, _, _, _ := mcpDeleteEventFixture(t, "control")

	// No delete: the scoped account survives, so it must be used as-is.
	srv.recordMCPEvent(callCtx, "list_workspaces", false)

	var accountID string
	if err := pool.QueryRow(ctx,
		`SELECT account_id FROM events WHERE event_name='mcp.tool_call'
		   AND user_id=$1 AND properties->>'tool'='list_workspaces'`, userID).Scan(&accountID); err != nil {
		t.Fatalf("history row missing for an ordinary call: %v", err)
	}
	if accountID != doomedAccID {
		t.Errorf("an ordinary call was attributed to %s, want its own scope account %s "+
			"(the surviving-account fallback must not apply when nothing was deleted)",
			accountID, doomedAccID)
	}
}

// TestMCPDeleteWorkspaceHistoryEventIsRecordedOnce pins the count: exactly one
// history row per call. A fix that recorded before AND after the delete would
// pass the single-row assertion above and fail here.
func TestMCPDeleteWorkspaceHistoryEventIsRecordedOnce(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	srv, callCtx, userID, _, _, doomedID, doomedSlug := mcpDeleteEventFixture(t, "once")

	for i := 0; i < 2; i++ {
		_, out, err := srv.deleteWorkspaceMCP(callCtx, nil, deleteWorkspaceIn{
			WorkspaceID: doomedID, ConfirmSlug: doomedSlug,
		})
		if i == 0 && (err != nil || out["status"] != "trashed") {
			t.Fatalf("first delete_workspace: err=%v out=%v", err, out)
		}
		if i == 1 && err == nil {
			t.Fatalf("the second delete_workspace of the same workspace should have failed, got %v", out)
		}
		srv.recordMCPEvent(callCtx, "delete_workspace", err != nil)
	}
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM events WHERE event_name='mcp.tool_call'
		   AND user_id=$1 AND properties->>'tool'='delete_workspace'`, userID).Scan(&n); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if n != 2 {
		t.Errorf("mcp.tool_call rows after two calls = %d, want exactly 2 — one per call, "+
			"no double-recording", n)
	}
}

// TestMCPDeleteWorkspaceHistoryNeverLandsOnAStrangerAccount is the MCP twin of the
// cross-tenant mis-attribution.
//
// The caller owns workspaces AND is a plain MEMBER of a stranger's older one,
// created first so its account heads the list ByMember returns — exactly the
// candidate a fallback would reach for.
//
// Under the OLD hard delete the scope account was destroyed, so the
// surviving-account fallback ran on every delete_workspace call, and it must not
// reach into a workspace the caller only BELONGS to: mcp.tool_call rows are
// account-scoped, so the stranger would read the caller's delete out of their
// own history.
//
// The bin removes the trigger — the scope account survives, so no fallback runs
// at all. This test still pins the cross-tenant property (the stranger's history
// stays empty) AND the stronger fact that the row is attributed to the call's
// own scope account rather than to some other live account the fallback might
// have picked.
func TestMCPDeleteWorkspaceHistoryNeverLandsOnAStrangerAccount(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	srv, callCtx, _, doomedAccID, survivorAccID, doomedID, doomedSlug, strangerAccID :=
		mcpDeleteEventFixtureWithStranger(t, "xtenant")

	_, out, err := srv.deleteWorkspaceMCP(callCtx, nil, deleteWorkspaceIn{
		WorkspaceID: doomedID, ConfirmSlug: doomedSlug,
	})
	if err != nil || out["status"] != "trashed" {
		t.Fatalf("delete_workspace: err=%v out=%v", err, out)
	}
	var exists int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM workspaces WHERE id=$1 AND deleted_at IS NOT NULL`, doomedID).Scan(&exists); err != nil {
		t.Fatalf("count workspaces: %v", err)
	}
	if exists != 1 {
		t.Fatal("the workspace was not trashed, so this test proves nothing")
	}
	// The scope account SURVIVES the bin, so attribution does not have to go
	// looking for a substitute — the stranger is never a candidate.
	if acc, err := dbstore.NewAccountStore(pool).ByID(ctx, doomedAccID); err != nil || acc == nil {
		t.Fatalf("the binned workspace's account %s is gone (err=%v); under the trash it must "+
			"survive, which is what keeps attribution off the stranger's account", doomedAccID, err)
	}
	srv.recordMCPEvent(callCtx, "delete_workspace", false)

	var inStranger, inSurvivor int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM events WHERE event_name='mcp.tool_call'
		   AND account_id=$1 AND properties->>'tool'='delete_workspace'`, strangerAccID).
		Scan(&inStranger); err != nil {
		t.Fatalf("count stranger history: %v", err)
	}
	if inStranger != 0 {
		t.Errorf("the caller's delete_workspace history row landed on the STRANGER's account %s (%d row(s)) "+
			"— the caller only MEMBERS that workspace, so attribution leaked across tenants", strangerAccID, inStranger)
	}
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM events WHERE event_name='mcp.tool_call'
		   AND account_id=$1 AND properties->>'tool'='delete_workspace'`, survivorAccID).
		Scan(&inSurvivor); err != nil {
		t.Fatalf("count survivor history: %v", err)
	}
	if inSurvivor != 1 {
		t.Errorf("history rows on the caller's OWN live account %s = %d, want 1 — the call must be "+
			"recorded on a live account of the caller's, never on the binned one and never on a "+
			"stranger's", survivorAccID, inSurvivor)
	}
}

// mcpRescopeToStrangerAccount re-resolves the fixture's call scope as if the
// agent had passed `account_id=<the stranger's account>` — the EXACT argument
// shape the card measured. resolveScope accepts it (accountAccessible admits
// owner OR member), which is the whole point: the scope names an account the
// caller does not own.
//
// It asserts the shape rather than assuming it: the resolution must name the
// stranger's account and the caller must not own it. If either stops holding the
// test has stopped reproducing the defect, so it fails loudly here instead of
// passing quietly.
func mcpRescopeToStrangerAccount(
	t *testing.T, srv *Server, callCtx context.Context, pool *pgxpool.Pool, userID, strangerAccID string,
) context.Context {
	t.Helper()
	scope, err := srv.resolveScope(callCtx, []byte(`{"account_id":"`+strangerAccID+`"}`))
	if err != nil {
		t.Fatalf("resolveScope(account_id=%s) = %v — the caller only MEMBERS that workspace, and "+
			"accountAccessible admits a member, so the resolution must succeed; without it the "+
			"fixture is not the defect shape", strangerAccID, err)
	}
	if len(scope.Accounts) != 1 || scope.Accounts[0] != strangerAccID {
		t.Fatalf("call scope = %v, want exactly the stranger's account %s", scope.Accounts, strangerAccID)
	}
	acc, err := dbstore.NewAccountStore(pool).ByID(context.Background(), strangerAccID)
	if err != nil || acc == nil {
		t.Fatalf("read stranger account %s: %v", strangerAccID, err)
	}
	if acc.OwnerUserID == userID {
		t.Fatalf("the caller OWNS the stranger's account %s; the fixture is not cross-tenant", strangerAccID)
	}
	return withCallScope(callCtx, scope)
}

// mcpBinnedAndRecorded drives the delete of the caller's OWN workspace, asserts
// the workspace really went to the bin (otherwise the test proves nothing), then
// records the history row the way addTool does after the handler returns.
func mcpBinnedAndRecorded(
	t *testing.T, srv *Server, callCtx context.Context, pool *pgxpool.Pool, doomedID, doomedSlug string,
) {
	t.Helper()
	ctx := context.Background()
	if _, out, err := srv.deleteWorkspaceMCP(callCtx, nil, deleteWorkspaceIn{
		WorkspaceID: doomedID, ConfirmSlug: doomedSlug,
	}); err != nil || out["status"] != "trashed" {
		t.Fatalf("delete_workspace: err=%v out=%v", err, out)
	}
	var binned int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM workspaces WHERE id=$1 AND deleted_at IS NOT NULL`, doomedID).Scan(&binned); err != nil {
		t.Fatalf("count workspaces: %v", err)
	}
	if binned != 1 {
		t.Fatal("the workspace was not trashed, so this test proves nothing")
	}
	srv.recordMCPEvent(callCtx, "delete_workspace", false)
}

// mcpHistoryRead reads mcp.tool_call history through the ACCOUNT scope — the
// same filter an account-scoped history read uses — and returns the total.
func mcpHistoryRead(t *testing.T, pool *pgxpool.Pool, accountID string) int64 {
	t.Helper()
	_, total, err := dbstore.NewEventStore(pool).List(context.Background(), dbstore.EventFilter{
		AccountIDs: []string{accountID}, EventName: "mcp.tool_call", Page: 1, Limit: 10,
	})
	if err != nil {
		t.Fatalf("read mcp.tool_call history for account %s: %v", accountID, err)
	}
	return total
}

// TestMCPDeleteWorkspaceHistoryScopedToAStrangerAccountStaysOwned is the MCP
// twin of TestRESTWorkspaceDeleteEventScopedToAStrangersWorkspaceStaysOwned: the
// RESOLVED-account half of the cross-tenant mis-attribution.
//
// Shape: the caller OWNS two workspaces and is a plain MEMBER of a stranger's
// older one; the call is scoped by `account_id=<the stranger's account>`, which
// resolveScope accepts because accountAccessible admits a member. The call then
// deletes the caller's OWN workspace, so nothing of the stranger's is touched —
// the stranger's account enters through the call scope alone. It survives the
// call, so an existence-only guard accepts it as the attribution target and the
// caller's own delete_workspace row lands in the STRANGER's account-scoped
// history: the stranger reads the caller's tool calls, the caller reads nothing.
//
// Tenancy, not existence, has to decide. The resolved scope account may be used
// only when the caller OWNS it; otherwise the fallback (the caller's own
// surviving account) applies and the stranger's history stays empty.
func TestMCPDeleteWorkspaceHistoryScopedToAStrangerAccountStaysOwned(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	srv, callCtx, userID, _, survivorAccID, doomedID, doomedSlug, strangerAccID :=
		mcpDeleteEventFixtureWithStranger(t, "accscope")

	callCtx = mcpRescopeToStrangerAccount(t, srv, callCtx, pool, userID, strangerAccID)
	mcpBinnedAndRecorded(t, srv, callCtx, pool, doomedID, doomedSlug)

	// The stranger's account-scoped history — what the stranger's own Activity
	// view reads — must be untouched.
	if n := mcpHistoryRead(t, pool, strangerAccID); n != 0 {
		t.Errorf("the caller's delete_workspace history row landed on the STRANGER's account %s "+
			"(%d row(s)) — the caller only MEMBERS that workspace, so a scope that resolved to it "+
			"is REACHABILITY, not a tenancy to file events in", strangerAccID, n)
	}
	// The faithful home is the caller's own surviving account.
	if n := mcpHistoryRead(t, pool, survivorAccID); n != 1 {
		t.Errorf("mcp.tool_call rows on the caller's OWN surviving account %s = %d, want 1 — the "+
			"caller still owns a live account, so that is where its history belongs", survivorAccID, n)
	}
	// Exactly one row for the call in total: never filed twice, never filed
	// anywhere other than the caller's own surviving account.
	var rows int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM events WHERE event_name='mcp.tool_call' AND user_id=$1`, userID).
		Scan(&rows); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if rows != 1 {
		t.Errorf("mcp.tool_call rows for the caller = %d, want exactly 1 — one per call", rows)
	}
	var accountID, propsAccount string
	if err := pool.QueryRow(ctx,
		`SELECT account_id, properties->>'account_id' FROM events WHERE event_name='mcp.tool_call'
		   AND user_id=$1`, userID).Scan(&accountID, &propsAccount); err != nil {
		t.Fatalf("read event account: %v", err)
	}
	if accountID != survivorAccID {
		t.Errorf("the history row is attributed to %s, want the caller's own surviving account %s",
			accountID, survivorAccID)
	}
	if propsAccount != accountID {
		t.Errorf("properties.account_id = %s but the row's account_id = %s — they must agree",
			propsAccount, accountID)
	}
}

// TestMCPUnscopedHistoryEventNeverLandsOnAMemberStrangerAccount covers the same
// branch through the scope route that needs NO explicit argument at all.
//
// An unscoped call resolves to every account across the user's accessible
// workspaces — Workspaces.ByMember, which is owned OR JOINED, ordered by
// workspace created_at. The fixture's stranger workspace is the oldest, so
// cs.Accounts[0] is the stranger's account for a plain, unscoped tool call: the
// default shape every connected agent uses. It survives the call, so an
// existence-only guard files the caller's own delete_workspace history in the
// stranger's account without the caller ever naming it.
//
// Ownership is the decider here too, so the row lands on the caller's own
// surviving account and the stranger's history stays empty.
func TestMCPUnscopedHistoryEventNeverLandsOnAMemberStrangerAccount(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	srv, callCtx, userID, _, survivorAccID, doomedID, doomedSlug, strangerAccID :=
		mcpDeleteEventFixtureWithStranger(t, "unscoped")

	// No workspace_id / account_id: the unscoped resolution.
	scope, err := srv.resolveScope(callCtx, []byte(`{}`))
	if err != nil {
		t.Fatalf("resolveScope(unscoped): %v", err)
	}
	// Precondition, asserted rather than assumed: the unscoped scope's FIRST
	// account is the stranger's. If ByMember ordering ever changes, this test
	// stops reproducing the defect, so fail loudly here.
	if len(scope.Accounts) == 0 || scope.Accounts[0] != strangerAccID {
		t.Fatalf("unscoped call scope = %v, want the stranger's account %s first (ByMember orders "+
			"by workspace created_at and the stranger's workspace is the oldest)", scope.Accounts, strangerAccID)
	}
	if acc, err := dbstore.NewAccountStore(pool).ByID(ctx, strangerAccID); err != nil || acc == nil {
		t.Fatalf("read stranger account %s: %v", strangerAccID, err)
	} else if acc.OwnerUserID == userID {
		t.Fatalf("the caller OWNS the stranger's account %s; the fixture is not cross-tenant", strangerAccID)
	}
	callCtx = withCallScope(callCtx, scope)

	mcpBinnedAndRecorded(t, srv, callCtx, pool, doomedID, doomedSlug)

	if n := mcpHistoryRead(t, pool, strangerAccID); n != 0 {
		t.Errorf("an UNSCOPED call's delete_workspace history row landed on the STRANGER's account %s "+
			"(%d row(s)) — the unscoped scope is ordered by workspace age, so a workspace the caller "+
			"merely MEMBERS can head it", strangerAccID, n)
	}
	if n := mcpHistoryRead(t, pool, survivorAccID); n != 1 {
		t.Errorf("mcp.tool_call rows on the caller's OWN surviving account %s = %d, want 1",
			survivorAccID, n)
	}
}
