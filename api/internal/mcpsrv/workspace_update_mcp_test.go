package mcpsrv

// update_workspace over the REAL MCP transport.
//
// The store behaviour is proven in dbstore's workspace_rename_test.go and the
// REST route in httpapi's workspace_rename_rest_test.go. THIS file proves what
// the MCP surface adds and what an agent must experience:
//
//   - the tool is actually REGISTERED and callable (a tool nobody registered
//     cannot be tested at all, and a hand-written handler unit test would pass
//     with the tool invisible to every client);
//   - the CONFIRMATION gate on a slug change (the revoke_api_key precedent Wan
//     asked for): without confirm=true the call is REFUSED as a result an agent
//     can read, not a transport error;
//   - OWNERSHIP: a manage/admin MEMBER may rename the name but not the slug;
//   - the write-SCOPE gate (issue #645): a readonly key cannot rename at all;
//   - the answer states that the old URL keeps working, because that is the whole
//     reason a rename is safe to do.
//
// Every call goes through HTTPHandler → authenticate → the tool handler with a
// real API key, exactly as a configured MCP client does.
//
// Skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// updateWSSlug returns a workspace's current slug.
func updateWSSlug(t *testing.T, pool *pgxpool.Pool, id string) string {
	t.Helper()
	w, err := dbstore.NewWorkspaceStore(pool).ByID(context.Background(), id)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	return w.Slug
}

// TestMCPUpdateWorkspaceIsRegisteredAndRenamesTheName proves the tool exists on
// the wire and that the LOW-RISK half needs no confirmation: a name is a label,
// so an agent may fix a typo without a prompt.
func TestMCPUpdateWorkspaceIsRegisteredAndRenamesTheName(t *testing.T) {
	requireTestDB(t)
	pool := testdb.Open(t)
	_, wsIDs, _, rawKey := wsMCPUserWorkspaces(t, pool, "updname", 1)
	httpSrv := wsMCPFixture(t, pool)

	out := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "update_workspace",
		map[string]any{"workspace_id": wsIDs[0], "name": "Kedai Bunga Baru"}))
	if out["status"] != "updated" {
		t.Fatalf("update_workspace (name) returned %v, want status=updated — if the tool were not "+
			"registered this call could not have reached a handler at all", out)
	}
	if out["name"] != "Kedai Bunga Baru" {
		t.Errorf("name = %v, want the new name", out["name"])
	}
	if out["slug_changed"] != false || out["name_changed"] != true {
		t.Errorf("flags = %v, want name_changed=true slug_changed=false", out)
	}
	// The row, not just the answer.
	got, err := dbstore.NewWorkspaceStore(pool).ByID(context.Background(), wsIDs[0])
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Name != "Kedai Bunga Baru" {
		t.Errorf("row name = %q, want the new name — the tool reported updated without writing", got.Name)
	}
}

// TestMCPUpdateWorkspaceSlugRequiresConfirm is the confirmation gate Wan asked
// for. An agent renaming a workspace's PUBLIC identity by inference is the thing
// this prevents, so the refusal must be actionable: it has to say what moves and
// how to proceed.
func TestMCPUpdateWorkspaceSlugRequiresConfirm(t *testing.T) {
	requireTestDB(t)
	pool := testdb.Open(t)
	_, wsIDs, _, rawKey := wsMCPUserWorkspaces(t, pool, "updconfirm", 1)
	httpSrv := wsMCPFixture(t, pool)
	before := updateWSSlug(t, pool, wsIDs[0])

	out := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "update_workspace",
		map[string]any{"workspace_id": wsIDs[0], "slug": "mcp-unconfirmed-move"}))
	if out["status"] != "refused" {
		t.Fatalf("update_workspace with a slug and NO confirm returned %v, want status=refused", out)
	}
	msg := strings.ToLower(str(out["error"]))
	if !strings.Contains(msg, "confirm") {
		t.Errorf("the refusal does not tell the agent how to proceed: %v", out["error"])
	}
	if !strings.Contains(msg, "alamat awam") && !strings.Contains(msg, "public") {
		t.Errorf("the refusal does not explain that the PUBLIC address is what moves: %v", out["error"])
	}
	if updateWSSlug(t, pool, wsIDs[0]) != before {
		t.Error("the slug moved despite the confirmation being refused")
	}

	// WITH confirm=true it goes through, and the answer states the promise.
	done := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "update_workspace",
		map[string]any{"workspace_id": wsIDs[0], "slug": "mcp-confirmed-move", "confirm": true}))
	if done["status"] != "updated" {
		t.Fatalf("update_workspace with confirm=true returned %v, want status=updated", done)
	}
	if done["previous_slug"] != before || done["slug"] != "mcp-confirmed-move" {
		t.Errorf("answer = %v, want previous_slug=%q slug=mcp-confirmed-move", done, before)
	}
	if done["previous_url_redirects"] != true || done["previous_url"] != "https://"+before+".jomform.com" {
		t.Errorf("the answer does not promise the old address keeps working: %v — an agent must be able "+
			"to tell the merchant their printed QR stands still resolve", done)
	}
	if updateWSSlug(t, pool, wsIDs[0]) != "mcp-confirmed-move" {
		t.Error("the slug did not move despite confirm=true and status=updated")
	}
}

// TestMCPUpdateWorkspaceSlugRefusedForNonOwner pins the ownership rule over the
// protocol: a manage/admin MEMBER may rename the label but not the public
// address. Both halves are asserted, so the rule cannot be "fixed" by refusing
// the member everything.
func TestMCPUpdateWorkspaceSlugRefusedForNonOwner(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	ownerID, wsIDs, _, _ := wsMCPUserWorkspaces(t, pool, "updown", 2)
	httpSrv := wsMCPFixture(t, pool)

	// The member: their own workspace (so they have an account for the API key)
	// plus manage rights on the target.
	sp := dbstore.NewWorkspaceStore(pool)
	var memberID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ('updmember-' || md5(random()::text) || '@example.test', 'it') RETURNING id`,
	).Scan(&memberID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	memberWS, err := sp.Create(ctx, memberID, "Member WS", "updmember-"+memberID[:8])
	if err != nil {
		t.Fatalf("create member workspace: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1,$2,'manage')`,
		wsIDs[0], memberID); err != nil {
		t.Fatalf("add manage member: %v", err)
	}
	var memberAcc string
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, memberWS.ID).Scan(&memberAcc); err != nil {
		t.Fatalf("read member account: %v", err)
	}
	memberKey := "jf_updmember_" + memberID[:8] + "0000000000"
	if _, err := dbstore.NewAPIKeyStore(pool).Create(ctx, memberAcc, auth.HashAPIKey(memberKey), "updmember",
		[]string{dbstore.ScopeReadWrite}); err != nil {
		t.Fatalf("create member key: %v", err)
	}

	before := updateWSSlug(t, pool, wsIDs[0])
	out := succeeded(t, mcpCall(t, httpSrv.URL, memberKey, "update_workspace",
		map[string]any{"workspace_id": wsIDs[0], "slug": "member-tried-this", "confirm": true}))
	if out["status"] != "refused" {
		t.Fatalf("a manage MEMBER moved the public slug over MCP: %v, want status=refused", out)
	}
	if updateWSSlug(t, pool, wsIDs[0]) != before {
		t.Error("the refused slug change moved the slug anyway")
	}

	// The NAME is theirs to change.
	okName := succeeded(t, mcpCall(t, httpSrv.URL, memberKey, "update_workspace",
		map[string]any{"workspace_id": wsIDs[0], "name": "Renamed By Member"}))
	if okName["status"] != "updated" {
		t.Errorf("a manage member could not rename the NAME over MCP: %v — the label is not the public identity",
			okName)
	}

	// The OWNER can move the slug. The control that the refusal above is about
	// OWNERSHIP and not a broken tool: the owner's own key, on the same
	// workspace, with the same call.
	ownerKey := wsMCPKeyFor(t, pool, ownerID, wsIDs[1])
	ownerOut := succeeded(t, mcpCall(t, httpSrv.URL, ownerKey, "update_workspace",
		map[string]any{"workspace_id": wsIDs[0], "slug": "mcp-owner-moved-it", "confirm": true}))
	if ownerOut["status"] != "updated" {
		t.Fatalf("the OWNER could not move the slug over MCP: %v", ownerOut)
	}
	if updateWSSlug(t, pool, wsIDs[0]) != "mcp-owner-moved-it" {
		t.Errorf("the owner's rename reported updated but the slug is %q", updateWSSlug(t, pool, wsIDs[0]))
	}
}

// wsMCPKeyFor mints a readwrite key on keyWorkspaceID's account, for an EXISTING
// user.
//
// The key lives on a different workspace on purpose: deleting or renaming a
// workspace revokes/does not touch keys by account, and keeping the caller's
// credential off the workspace under test means a later step in a test can never
// remove its own access and turn a real failure into a 401.
func wsMCPKeyFor(t *testing.T, pool *pgxpool.Pool, userID, keyWorkspaceID string) string {
	t.Helper()
	ctx := context.Background()
	var accID string
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, keyWorkspaceID).Scan(&accID); err != nil {
		t.Fatalf("read account for key workspace: %v", err)
	}
	raw := "jf_updowner_" + userID[:8] + "0000000000"
	if _, err := dbstore.NewAPIKeyStore(pool).Create(ctx, accID, auth.HashAPIKey(raw), "updown-owner",
		[]string{dbstore.ScopeReadWrite}); err != nil {
		t.Fatalf("create owner key: %v", err)
	}
	return raw
}

// TestMCPUpdateWorkspaceRefusedForReadonlyKey is the scope axis (issue #645) on
// the new tool: a readonly key must not rename anything, while a read control
// proves the key is valid so the refusal is about the scope.
func TestMCPUpdateWorkspaceRefusedForReadonlyKey(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	_, wsIDs, _, _ := wsMCPUserWorkspaces(t, pool, "updro", 1)

	// A readonly key on the workspace's own account.
	var accID string
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, wsIDs[0]).Scan(&accID); err != nil {
		t.Fatalf("read account: %v", err)
	}
	roKey := "jf_updro_" + wsIDs[0][:8] + "0000000000"
	if _, err := dbstore.NewAPIKeyStore(pool).Create(ctx, accID, auth.HashAPIKey(roKey), "updro",
		[]string{dbstore.ScopeReadonly}); err != nil {
		t.Fatalf("create readonly key: %v", err)
	}
	httpSrv := wsMCPFixture(t, pool)

	// Read control: the key authenticates and may read.
	if out := succeeded(t, mcpCall(t, httpSrv.URL, roKey, "list_workspaces", map[string]any{})); out == nil {
		t.Fatal("list_workspaces returned nothing — the readonly key would be unusable, making the assertion vacuous")
	}

	for _, args := range []map[string]any{
		{"workspace_id": wsIDs[0], "name": "readonly tried"},
		{"workspace_id": wsIDs[0], "slug": "readonly-tried", "confirm": true},
	} {
		res := mcpCall(t, httpSrv.URL, roKey, "update_workspace", args)
		if !res.IsError {
			t.Errorf("a READONLY key was allowed to rename via update_workspace(%v): %s", args,
				toolResultText(t, res))
			continue
		}
		if !strings.Contains(strings.ToLower(toolResultText(t, res)), "scope") {
			t.Errorf("refused, but not for the scope reason: %s", toolResultText(t, res))
		}
	}
	// Nothing moved.
	got, err := dbstore.NewWorkspaceStore(pool).ByID(ctx, wsIDs[0])
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Name != "WS updro" {
		t.Errorf("a refused readonly rename changed the name: %+v", got)
	}
}
