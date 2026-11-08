package dbstore

// admin_list_workspaces' product_count (mcpsrv/admin_mcp.go:66) must report the
// products a workspace actually holds. It was read from products.workspace_id,
// a column the application never writes — ProductStore.Create's INSERT omits
// it and no trigger maintains it — so it answered 0 for EVERY workspace.
//
// These cases pin the workspace's product set to the products of ITS ACCOUNTS,
// which is the ownership the rest of the store already uses (every product read
// is by account_id / a set of account ids, and WorkspaceStore.emptyCheckSQL
// counts a workspace's products the same way). The fixture is built from the
// production writers — WorkspaceStore.Create / CreateForAccount and
// ProductStore.Create — so it cannot inherit the assumption under test.

import (
	"context"
	"testing"
)

func TestAdminWorkspaceProductCountCountsTheWorkspacesProducts(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewWorkspaceStore(p)
	ps := NewProductStore(p)

	// productCounts reads the super-admin list ONCE and returns slug → count, so
	// each case asserts the number the admin tool prints for THAT workspace,
	// never merely "some workspace is non-zero".
	productCounts := func() map[string]int64 {
		t.Helper()
		rows, err := sp.ListAllWithCounts(ctx)
		if err != nil {
			t.Fatalf("ListAllWithCounts: %v", err)
		}
		out := make(map[string]int64, len(rows))
		for _, r := range rows {
			out[r.Slug] = r.ProductCount
		}
		return out
	}

	// createWorkspace uses the ordinary writer (workspace + a fresh account
	// already linked to it) and returns the workspace plus that account's id.
	createWorkspace := func(name string) (*Workspace, string) {
		t.Helper()
		w, err := sp.Create(ctx, testOwnerUser(t, ctx, p), name, "it-pcount-"+randSuffix())
		if err != nil {
			t.Fatalf("create workspace %q: %v", name, err)
		}
		var acc string
		if err := p.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, w.ID).Scan(&acc); err != nil {
			t.Fatalf("read the account of workspace %q: %v", name, err)
		}
		return w, acc
	}

	// (1) THE REPORTED DEFECT. The merchant's account is already in its
	// workspace when the product is created — the ordinary path. Nothing writes
	// products.workspace_id here, so the old count answered 0.
	sold, soldAcc := createWorkspace("with a product")
	if err := ps.Create(ctx, &Product{
		AccountID: soldAcc, Name: "e-book", PriceSen: 1500, Type: "one_time",
	}); err != nil {
		t.Fatalf("create product: %v", err)
	}

	// (2) A workspace with no products must stay at 0: the number is per
	// workspace, not a platform total.
	empty, _ := createWorkspace("without products")

	got := productCounts()
	if got[sold.Slug] != 1 {
		t.Errorf("workspace %q product_count = %d, want 1 — a product created for an "+
			"account in this workspace must be counted", sold.Slug, got[sold.Slug])
	}
	if got[empty.Slug] != 0 {
		t.Errorf("workspace %q product_count = %d, want 0 — the count must not include "+
			"another workspace's products", empty.Slug, got[empty.Slug])
	}

	// (3) A product created BEFORE its account has a workspace still belongs to
	// the workspace once the account is linked: the count follows the account,
	// so the link is enough and no second write to the product is needed.
	user := testOwnerUser(t, ctx, p)
	var lateAcc string
	if err := p.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 VALUES ($1, 'ask first', 'it-pcount-' || md5(random()::text))
		 RETURNING id`, user).Scan(&lateAcc); err != nil {
		t.Fatalf("create the unscoped account: %v", err)
	}
	if err := ps.Create(ctx, &Product{
		AccountID: lateAcc, Name: "created before the link", PriceSen: 900, Type: "one_time",
	}); err != nil {
		t.Fatalf("create product before the link: %v", err)
	}
	late, err := sp.CreateForAccount(ctx, user, "ask first", "it-pcount-"+randSuffix(), lateAcc)
	if err != nil {
		t.Fatalf("CreateForAccount: %v", err)
	}
	if got := productCounts(); got[late.Slug] != 1 {
		t.Errorf("workspace %q product_count = %d, want 1 — a product that predates the "+
			"link belongs to the workspace through its account", late.Slug, got[late.Slug])
	}
}
