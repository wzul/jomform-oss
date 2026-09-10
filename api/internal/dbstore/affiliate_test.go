package dbstore

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// Issue #444: affiliate marketing — create + list + commission.
func TestAffiliateCRUD(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	store := NewAffiliateStore(pool)

	// Create a workspace to own the affiliate.
	ws := &Workspace{ID: uuid.NewString(), Name: "Aff Test", Slug: "aff-test-" + uuid.NewString()[:8], Owner: uuid.NewString()}
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id, owner_user_id, name, slug) VALUES ($1,$2,$3,$4)`, ws.ID, ws.Owner, ws.Name, ws.Slug); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}

	a := &Affiliate{ID: uuid.NewString(), WorkspaceID: ws.ID, Code: "ALI", Name: "Ali", Email: "ali@example.com", CommissionPct: 10}
	if err := store.Create(ctx, a); err != nil {
		t.Fatalf("create affiliate: %v", err)
	}

	got, err := store.ByCode(ctx, ws.ID, "ALI")
	if err != nil || got == nil {
		t.Fatalf("ByCode: %v", err)
	}
	if got.CommissionPct != 10 {
		t.Fatalf("commission = %v, want 10", got.CommissionPct)
	}

	if err := store.SetCommission(ctx, a.ID, 15); err != nil {
		t.Fatalf("set commission: %v", err)
	}
	got2, _ := store.ByID(ctx, a.ID)
	if got2.CommissionPct != 15 {
		t.Fatalf("commission after set = %v, want 15", got2.CommissionPct)
	}

	list, err := store.ByWorkspace(ctx, ws.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("ByWorkspace: %v len=%d", err, len(list))
	}
}
