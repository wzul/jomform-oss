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
	if out["status"] != "deleted" {
		t.Fatalf("delete_workspace returned %v, want status=deleted", out)
	}
	// The delete really happened (otherwise this test proves nothing).
	var exists int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM workspaces WHERE id=$1`, doomedID).Scan(&exists); err != nil {
		t.Fatalf("count workspaces: %v", err)
	}
	if exists != 0 {
		t.Fatal("the workspace was not deleted, so this test proves nothing")
	}
	// The account the scope named is gone — the reason the pre-fix write failed.
	if acc, err := dbstore.NewAccountStore(pool).ByID(ctx, doomedAccID); err == nil && acc != nil {
		t.Errorf("the doomed account %s still exists; the fixture did not reproduce the defect shape", doomedAccID)
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
		t.Fatalf("mcp.tool_call rows for delete_workspace = %d, want 1 — the history write "+
			"is attributed to the account the call just destroyed, so "+
			"events_account_id_fkey rejects it with SQLSTATE 23503 and recordMCPEvent's "+
			"best-effort path swallows it", n)
	}
	var accountID string
	if err := pool.QueryRow(ctx,
		`SELECT account_id FROM events WHERE event_name='mcp.tool_call'
		   AND user_id=$1 AND properties->>'tool'='delete_workspace'`, userID).Scan(&accountID); err != nil {
		t.Fatalf("read event account_id: %v", err)
	}
	if accountID == doomedAccID {
		t.Errorf("the history row is attributed to the DELETED account %s — that is the FK violation", accountID)
	}
	if accountID != survivorAccID {
		t.Errorf("the history row is attributed to %s, want the surviving account %s", accountID, survivorAccID)
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
		if i == 0 && (err != nil || out["status"] != "deleted") {
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
// The caller owns workspaces AND is a plain MEMBER of a stranger's older one.
// When the call's own scope account is destroyed by delete_workspace, the
// surviving-account fallback must not reach into a workspace the caller only
// belongs to: mcp.tool_call rows are account-scoped, so the stranger would read
// the caller's delete out of their own history while the caller's history showed
// nothing.
//
// Two assertions, because they fail differently: the stranger's history must be
// empty, AND the caller's own surviving account must carry the row. With the
// rework's owned-only fallback the second holds directly; a fallback that merely
// skipped would pass the first and fail the second, which is the point.
func TestMCPDeleteWorkspaceHistoryNeverLandsOnAStrangerAccount(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	srv, callCtx, _, doomedAccID, survivorAccID, doomedID, doomedSlug, strangerAccID :=
		mcpDeleteEventFixtureWithStranger(t, "xtenant")

	_, out, err := srv.deleteWorkspaceMCP(callCtx, nil, deleteWorkspaceIn{
		WorkspaceID: doomedID, ConfirmSlug: doomedSlug,
	})
	if err != nil || out["status"] != "deleted" {
		t.Fatalf("delete_workspace: err=%v out=%v", err, out)
	}
	var exists int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM workspaces WHERE id=$1`, doomedID).Scan(&exists); err != nil {
		t.Fatalf("count workspaces: %v", err)
	}
	if exists != 0 {
		t.Fatal("the workspace was not deleted, so this test proves nothing")
	}
	// The scope account is gone: the fallback is what attribution exercises.
	if acc, err := dbstore.NewAccountStore(pool).ByID(ctx, doomedAccID); err == nil && acc != nil {
		t.Errorf("the doomed account %s still exists; the fixture did not reproduce the defect shape", doomedAccID)
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
		t.Errorf("history rows on the caller's OWN surviving account %s = %d, want 1 — the caller still "+
			"owns a live account, so the call must be recorded there", survivorAccID, inSurvivor)
	}
}
