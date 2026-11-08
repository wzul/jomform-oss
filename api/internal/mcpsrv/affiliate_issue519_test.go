package mcpsrv

// Issue #519: the MCP surface of the affiliate commission bug.
//
// set_affiliate_commission authorised in.WorkspaceID and then wrote
// in.AffiliateID, with nothing tying the two together — so a caller with
// admin/manage in workspace B could change workspace A's affiliate commission
// rate by passing A's affiliate_id. The REST twin is covered in
// httpapi/rest_affiliate_issue519_test.go.
//
// The test drives the tool handler directly (the same typed input the protocol
// delivers), seeds two independent tenants, and asserts the cross-tenant write
// is refused AND leaves workspace A's row unchanged, while the caller's own
// workspace still works.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL); skipped
// when unset.

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// issue519MCPState is two tenants plus a server whose caller is tenant B's
// owner (admin of B, no access to A).
type issue519MCPState struct {
	srv        *Server
	ctx        context.Context // bound to user B
	affStore   *dbstore.AffiliateStore
	wsA, wsB   string
	affA, affB *dbstore.Affiliate
}

func issue519MCPFixture(t *testing.T) issue519MCPState {
	t.Helper()
	if strings.TrimSpace(os.Getenv("JOMFORM_TEST_DATABASE_URL")) == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping affiliate MCP cross-tenant test")
	}
	ctx := context.Background()
	pool := testdb.Open(t)

	newUser := func() string {
		var id string
		if err := pool.QueryRow(ctx,
			`INSERT INTO users (email, password_hash) VALUES ('i519m-' || md5(random()::text) || '@example.test', 'it')
			 RETURNING id`).Scan(&id); err != nil {
			t.Fatalf("create user: %v", err)
		}
		return id
	}
	userA, userB := newUser(), newUser()

	sp := dbstore.NewWorkspaceStore(pool)
	wsA, err := sp.Create(ctx, userA, "I519 MCP A", "i519m-a-"+uuid.NewString()[:8])
	if err != nil {
		t.Fatalf("create workspace A: %v", err)
	}
	wsB, err := sp.Create(ctx, userB, "I519 MCP B", "i519m-b-"+uuid.NewString()[:8])
	if err != nil {
		t.Fatalf("create workspace B: %v", err)
	}

	affStore := dbstore.NewAffiliateStore(pool)
	affA := &dbstore.Affiliate{ID: uuid.NewString(), WorkspaceID: wsA.ID, Code: "MA", Name: "Tenant A", CommissionPct: 10}
	affB := &dbstore.Affiliate{ID: uuid.NewString(), WorkspaceID: wsB.ID, Code: "MB", Name: "Tenant B", CommissionPct: 10}
	if err := affStore.Create(ctx, affA); err != nil {
		t.Fatalf("create affiliate A: %v", err)
	}
	if err := affStore.Create(ctx, affB); err != nil {
		t.Fatalf("create affiliate B: %v", err)
	}

	srv := &Server{deps: &Deps{
		Accounts:   dbstore.NewAccountStore(pool),
		Workspaces: sp,
		Affiliates: affStore,
		Sales:      dbstore.NewSaleStore(pool),
	}}
	// OAuth-shaped identity: bound to user B (issue #307).
	callCtx := WithAuthIdentity(ctx, &AuthIdentity{UserID: userB, Scopes: []string{"readwrite"}})
	return issue519MCPState{srv: srv, ctx: callCtx, affStore: affStore, wsA: wsA.ID, wsB: wsB.ID, affA: affA, affB: affB}
}

// commission519 re-reads the persisted commission rate.
func (st issue519MCPState) commission519(t *testing.T, id string) float64 {
	t.Helper()
	got, err := st.affStore.ByID(context.Background(), id)
	if err != nil || got == nil {
		t.Fatalf("reload affiliate %s: %v", id, err)
	}
	return got.CommissionPct
}

// TestIssue519MCPCrossTenantCommissionRefused: the caller administers B; A's
// affiliate must be refused and untouched.
func TestIssue519MCPCrossTenantCommissionRefused(t *testing.T) {
	st := issue519MCPFixture(t)

	before := st.commission519(t, st.affA.ID)

	_, _, err := st.srv.setAffiliateCommission(st.ctx, nil, setAffiliateCommissionIn{
		WorkspaceID: st.wsB, AffiliateID: st.affA.ID, CommissionPct: 99,
	})
	if err == nil {
		t.Fatal("cross-tenant set_affiliate_commission was ACCEPTED — the IDOR is back")
	}
	if !strings.Contains(err.Error(), "affiliate not found") {
		t.Errorf("refusal error = %v, want the not-found message (no info leak)", err)
	}
	if after := st.commission519(t, st.affA.ID); after != before {
		t.Fatalf("workspace A affiliate commission changed %v -> %v by a workspace B caller", before, after)
	}

	// An id that does not exist must produce the same refusal, so the caller
	// cannot distinguish "another tenant's id" from "no such id".
	_, _, errMiss := st.srv.setAffiliateCommission(st.ctx, nil, setAffiliateCommissionIn{
		WorkspaceID: st.wsB, AffiliateID: uuid.NewString(), CommissionPct: 99,
	})
	if errMiss == nil || errMiss.Error() != err.Error() {
		t.Errorf("unknown id error = %v, want the same refusal as the cross-tenant id (%v)", errMiss, err)
	}
}

// TestIssue519MCPOwnWorkspaceCommissionStillWorks: the happy path is intact.
func TestIssue519MCPOwnWorkspaceCommissionStillWorks(t *testing.T) {
	st := issue519MCPFixture(t)

	if _, _, err := st.srv.setAffiliateCommission(st.ctx, nil, setAffiliateCommissionIn{
		WorkspaceID: st.wsB, AffiliateID: st.affB.ID, CommissionPct: 25,
	}); err != nil {
		t.Fatalf("own-workspace set_affiliate_commission refused: %v", err)
	}
	if got := st.commission519(t, st.affB.ID); got != 25 {
		t.Fatalf("own-workspace commission = %v, want 25", got)
	}
	if got := st.commission519(t, st.affA.ID); got != 10 {
		t.Fatalf("workspace A commission = %v, want 10 (untouched)", got)
	}
}

// TestIssue519MCPGetAffiliateSalesStaysScoped pins the sibling's behaviour the
// fix mirrors, so a future refactor cannot quietly unship the comparison.
func TestIssue519MCPGetAffiliateSalesStaysScoped(t *testing.T) {
	st := issue519MCPFixture(t)

	_, _, err := st.srv.getAffiliateSales(st.ctx, nil, getAffiliateSalesIn{
		WorkspaceID: st.wsB, AffiliateID: st.affA.ID,
	})
	if err == nil {
		t.Fatal("get_affiliate_sales accepted another workspace's affiliate id")
	}
	if !strings.Contains(err.Error(), "affiliate not found") {
		t.Errorf("get_affiliate_sales refusal = %v, want not-found", err)
	}
}
