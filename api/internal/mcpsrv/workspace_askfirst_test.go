package mcpsrv

// Ask-first onboarding (Wan's decision, 2026-09-20) reached the MCP surface
// too: the dashboard asks a workspace-less user to create their first
// workspace, and agents create workspaces through the create_workspace tool. If
// the MCP path minted a SECOND account while the REST path reused the signup
// account, the same user would be onboarded two different ways depending on
// how they created it — and the MCP route would leave every new user owning an
// extra workspace-less account that nothing can reach.
//
// THIS FILE DRIVES THE REAL TRANSPORT, WITH THE CREDENTIAL EVERY MCP CALLER
// HOLDS. An earlier version of this test fabricated the caller with
// WithAuthIdentity(...) and never inserted an `api_keys` row — a shape no real
// /mcp request can have, since auth resolves the Bearer through that table
// (http.go → Keys.LookupByKeyHash). That fabricated shape is exactly why the
// defect the test was supposed to catch stayed undetected:
// SoleEmptyUnscopedAccount excluded accounts holding keys, so inserting the row
// the shipped caller always has turned this test RED. Every call below now goes
// HTTPHandler → authenticate → the tool handler with a real API key, and the
// assertion is on the ACCOUNTS TABLE, not on the handler's return value.
//
// Skipped without JOMFORM_TEST_DATABASE_URL.
import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

func TestMCPCreateWorkspaceReusesTheSignupAccount(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)

	// The post-signup state: a user with exactly one account, no workspace.
	accountID, userID := scopeTestAccount(t, pool, "mcpaskfirst")
	keys := dbstore.NewAPIKeyStore(pool)
	// ...plus the credential MCP requires. Without this row the caller could not
	// authenticate at all, and without the fix this row also pushed the account
	// out of the reuse path.
	rawKey := scopeTestKey(t, ctx, keys, accountID, "askfirst-key", []string{dbstore.ScopeReadWrite})

	srv := New(&Deps{
		Workspaces: dbstore.NewWorkspaceStore(pool),
		Accounts:   dbstore.NewAccountStore(pool),
		Keys:       keys,
	}, nil, nil, nil)
	httpSrv := httptest.NewServer(srv.HTTPHandler())
	defer httpSrv.Close()

	slug := "mcpaf-" + accountID[:8]
	out := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "create_workspace", map[string]any{
		"name": "Ask First", "slug": slug,
	}))
	if out["slug"] != slug {
		t.Fatalf("create_workspace slug = %v, want %q", out["slug"], slug)
	}

	// THE ASSERTION THAT MATTERS: exactly one account, and it is the signup one.
	var total int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM accounts WHERE owner_user_id=$1`, userID).Scan(&total); err != nil {
		t.Fatalf("count accounts: %v", err)
	}
	if total != 1 {
		t.Fatalf("MCP create_workspace left the user owning %d accounts, want 1 — it minted a "+
			"second account instead of reusing the signup one, so the MCP onboarding path and "+
			"the dashboard's ask-first path disagree", total)
	}
	var linked string
	var wsID *string
	if err := pool.QueryRow(ctx,
		`SELECT id, workspace_id FROM accounts WHERE owner_user_id=$1`, userID).Scan(&linked, &wsID); err != nil {
		t.Fatalf("read account: %v", err)
	}
	if linked != accountID {
		t.Fatalf("surviving account = %q, want the signup account %q", linked, accountID)
	}
	if wsID == nil {
		t.Fatal("the signup account was left workspace-less — the created workspace links no account")
	}
	if ws := out["id"]; ws != nil && wsID != nil && ws.(string) != *wsID {
		t.Fatalf("the signup account was linked to workspace %q, but create_workspace returned %v", *wsID, ws)
	}

	// The caller's own credential must travel WITH the account. A key left on an
	// account outside the workspace is invisible to the dashboard's key list
	// (GET /v1/rest/keys?workspace=<slug> filters by the workspace's accounts),
	// so the merchant could neither see nor revoke the key their agent holds.
	var keyWorkspace *string
	if err := pool.QueryRow(ctx,
		`SELECT workspace_id FROM api_keys WHERE account_id=$1`, accountID).Scan(&keyWorkspace); err != nil {
		t.Fatalf("read key back: %v", err)
	}
	if keyWorkspace == nil || (wsID != nil && *keyWorkspace != *wsID) {
		t.Fatalf("the agent's API key points at workspace %v, want the created workspace %v — "+
			"the credential is stranded where the dashboard cannot list or revoke it",
			keyWorkspace, *wsID)
	}
}
