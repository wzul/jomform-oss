package mcpsrv

// purge_test_accounts — gate and default-shape guard (card t_6ae7f586).
//
// The store's behaviour (marker-only sweeping, the sales/subscription refusal,
// the protected account, dry-run/idempotency) is pinned against a real Postgres
// in api/internal/dbstore/user_test_purge_test.go. What this file pins is the
// part that lives in the tool layer and must fail closed:
//
//   - the tool is REGISTERED (a feature with no tool breaks MCP parity);
//   - a non-super-admin is REFUSED, for both the dry run and the destructive
//     form — a missing allowlist must never expose a platform-wide delete;
//   - the DEFAULT is a dry run. `confirm` absent means "report, delete nothing";
//     only an explicit confirm=true may turn it destructive. That is the
//     difference between an operator inspecting the backlog and an operator
//     sweeping production because they forgot a flag.

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestPurgeTestAccountsIsRegistered(t *testing.T) {
	srv := &Server{
		mcp:  mcp.NewServer(&mcp.Implementation{Name: "jomform", Version: "test"}, nil),
		deps: &Deps{},
	}
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

	var found *mcp.Tool
	cursor := ""
	for {
		res, err := clientSession.ListTools(context.Background(), &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			t.Fatalf("ListTools: %v", err)
		}
		for _, tl := range res.Tools {
			if tl.Name == "purge_test_accounts" {
				found = tl
			}
		}
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	if found == nil {
		t.Fatal("purge_test_accounts is not registered — a QA lane would have no way to remove a test account")
	}
	if found.Description == "" {
		t.Error("purge_test_accounts has an empty description; a client's model cannot know what it does or that it is a dry run by default")
	}
}

func TestPurgeTestAccountsFailsClosedAndDefaultsToDryRun(t *testing.T) {
	srv := &Server{
		mcp:  mcp.NewServer(&mcp.Implementation{Name: "jomform", Version: "test"}, nil),
		deps: &Deps{},
	}
	ctx := context.Background()
	req := &mcp.CallToolRequest{}

	// No super-admin allowlist configured: both forms must be refused.
	if _, _, err := srv.purgeTestAccounts(ctx, req, purgeTestAccountsIn{}); err == nil {
		t.Error("purge_test_accounts (dry run) should refuse a non-super-admin")
	}
	if _, _, err := srv.purgeTestAccounts(ctx, req, purgeTestAccountsIn{Confirm: true}); err == nil {
		t.Error("purge_test_accounts (confirm=true) should refuse a non-super-admin — a missing allowlist must " +
			"never expose a platform-wide delete")
	}

	// The DEFAULT is a dry run; only an explicit confirm turns it destructive.
	// Asserted on the returned payload even on the refusal path, because the
	// handler decides the mode before the gate and that decision is what keeps a
	// forgotten flag from sweeping production.
	_, out, _ := srv.purgeTestAccounts(ctx, req, purgeTestAccountsIn{})
	if !out.DryRun {
		t.Error("purge_test_accounts without confirm reported dry_run=false — the default must be a DRY RUN")
	}
	_, out, _ = srv.purgeTestAccounts(ctx, req, purgeTestAccountsIn{Confirm: true})
	if out.DryRun {
		t.Error("purge_test_accounts with confirm=true still reported dry_run=true")
	}
}
