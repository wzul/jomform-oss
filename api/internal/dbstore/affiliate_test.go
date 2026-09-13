package dbstore

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// Issue #444: affiliate marketing — create + list + commission.
func TestAffiliateCRUD(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	store := NewAffiliateStore(pool)

	// workspaces.owner_user_id is a FK to users(id) — create the owner first.
	var ownerID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, 'it') RETURNING id`,
		"aff-test-"+uuid.NewString()[:8]+"@example.test").Scan(&ownerID); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	// Create a workspace to own the affiliate.
	ws := &Workspace{ID: uuid.NewString(), Name: "Aff Test", Slug: "aff-test-" + uuid.NewString()[:8], Owner: ownerID}
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

	if err := store.SetCommission(ctx, ws.ID, a.ID, 15); err != nil {
		t.Fatalf("set commission: %v", err)
	}
	got2, _ := store.ByID(ctx, a.ID)
	if got2.CommissionPct != 15 {
		t.Fatalf("commission after set = %v, want 15", got2.CommissionPct)
	}

	// Issue #519: the store rejects a write scoped to the WRONG workspace, so
	// the guard cannot be bypassed by a future caller that forgets its own
	// handler-side check. The row must be left untouched.
	otherWS := &Workspace{ID: uuid.NewString(), Name: "Other", Slug: "aff-other-" + uuid.NewString()[:8], Owner: ownerID}
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id, owner_user_id, name, slug) VALUES ($1,$2,$3,$4)`, otherWS.ID, otherWS.Owner, otherWS.Name, otherWS.Slug); err != nil {
		t.Fatalf("insert other workspace: %v", err)
	}
	if err := store.SetCommission(ctx, otherWS.ID, a.ID, 99); !errors.Is(err, ErrAffiliateNotFound) {
		t.Fatalf("cross-workspace SetCommission err = %v, want ErrAffiliateNotFound", err)
	}
	if got3, _ := store.ByID(ctx, a.ID); got3.CommissionPct != 15 {
		t.Fatalf("cross-workspace SetCommission modified the row: %v, want 15", got3.CommissionPct)
	}

	list, err := store.ByWorkspace(ctx, ws.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("ByWorkspace: %v len=%d", err, len(list))
	}
}
