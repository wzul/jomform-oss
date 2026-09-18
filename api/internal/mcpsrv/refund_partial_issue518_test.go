package mcpsrv

// Issue #518: MCP parity for partial-refund accounting (AGENTS.md "MCP FEATURE
// PARITY" + "DOCUMENTATION PARITY").
//
// The dashboard refund surface (POST /v1/rest/sales/{ref}/refund) gained
// partial-refund bookkeeping and an over-refund guard; per the parity rule the
// MCP tool must behave identically. These tests pin:
//
//   - refund_sale records the refunded amount and reports the balance instead
//     of silently marking the sale fully refunded;
//   - an over-refund is rejected by the tool (the balance error surfaces to the
//     agent) BEFORE any CHIP call;
//   - list_sales / get_sales_summary expose the partial state.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/payments"
	"github.com/wzul/jomform/api/internal/testdb"
)

// issue518MCPState is a seeded account + MCP server + a paid sale, with
// payments wired to a stub CHIP API.
type issue518MCPState struct {
	srv        *Server
	ctx        context.Context
	accountID  string
	saleID     string
	refCode    string
	pool       *pgxpool.Pool
	refundHits *int64
}

func issue518MCPFixture(t *testing.T, totalSen int64) issue518MCPState {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('i518-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	var workspaceID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'i518-ws-' || md5(random()::text), 'i518-' || md5(random()::text))
		 RETURNING id`, userID).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1, 'i518-acc-' || md5(random()::text), 'i518-' || md5(random()::text), $2)
		 RETURNING id`, userID, workspaceID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	sealed, err := cryptoutil.Encrypt(key, []byte("sk_test_i518_secret"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := dbstore.NewAccountStore(pool).SetChipCredentials(ctx, accountID, "br_i518", sealed, "test", "", nil, true); err != nil {
		t.Fatalf("set chip creds: %v", err)
	}

	var hits int64
	mux := http.NewServeMux()
	mux.HandleFunc("POST /purchases/{id}/refund/", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": r.PathValue("id"), "status": chip.StatusRefunded})
	})
	chipSrv := httptest.NewServer(mux)
	t.Cleanup(chipSrv.Close)

	deps := &Deps{
		Accounts:   dbstore.NewAccountStore(pool),
		Sales:      dbstore.NewSaleStore(pool),
		Forms:      dbstore.NewFormStore(pool),
		Workspaces: dbstore.NewWorkspaceStore(pool),
	}
	srv := &Server{
		deps: deps,
		pay:  payments.New(chip.New(chipSrv.URL), deps.Accounts, deps.Sales, dbstore.NewSubscriptionStore(pool), key, "http://localhost:8080"),
	}

	ref := "JF-MCP-518-" + accountID[:8]
	sale := &dbstore.Sale{
		AccountID: accountID, RefCode: ref,
		Client:      dbstore.SaleClient{FullName: "Ali", Email: "ali@example.test"},
		Items:       []dbstore.SaleItem{{ProductID: "p518", Name: "Tee", PriceSen: totalSen, Qty: 1}},
		SubtotalSen: totalSen, TotalSen: totalSen,
		SaleType: dbstore.SaleTypeOneTime, PaymentMethod: "chip", Status: dbstore.SalePaid,
	}
	if err := deps.Sales.Create(ctx, sale); err != nil {
		t.Fatalf("create sale: %v", err)
	}
	if err := deps.Sales.AttachChipPurchase(ctx, sale.ID, "pur_mcp518_"+accountID[:8], "https://gate.example/co/pur_mcp518"); err != nil {
		t.Fatalf("attach purchase: %v", err)
	}

	callCtx := WithAuthIdentity(ctx, &AuthIdentity{AccountID: accountID, Scopes: []string{"readwrite"}})
	return issue518MCPState{
		srv: srv, ctx: callCtx, accountID: accountID, saleID: sale.ID,
		refCode: ref, pool: pool, refundHits: &hits,
	}
}

// TestIssue518MCPRefundPartialRecordsAmountAndStaysPartial: refund_sale must
// record the amount and report the balance, not claim a full refund.
func TestIssue518MCPRefundPartialRecordsAmountAndStaysPartial(t *testing.T) {
	st := issue518MCPFixture(t, 5000)
	amount := int64(1000)

	_, out, err := st.srv.refundSaleMCP(st.ctx, nil, saleRefIn{RefCode: st.refCode, AmountSen: &amount})
	if err != nil {
		t.Fatalf("refund_sale (partial): %v", err)
	}
	res := map[string]any(out)
	if res["refunded_sen"] != int64(1000) {
		t.Errorf("refund_sale refunded_sen = %v, want 1000", res["refunded_sen"])
	}
	if res["outstanding_sen"] != int64(4000) {
		t.Errorf("refund_sale outstanding_sen = %v, want 4000", res["outstanding_sen"])
	}
	if res["fully_refunded"] != false {
		t.Errorf("refund_sale fully_refunded = %v, want false for a partial refund", res["fully_refunded"])
	}
	if res["refund_status"] != dbstore.RefundPartial {
		t.Errorf("refund_sale refund_status = %v, want %q", res["refund_status"], dbstore.RefundPartial)
	}
	sale, err := st.srv.deps.Sales.ByID(st.ctx, st.saleID)
	if err != nil {
		t.Fatalf("reload sale: %v", err)
	}
	if sale.RefundedSen != 1000 {
		t.Errorf("sale.refunded_sen = %d, want 1000", sale.RefundedSen)
	}
	if sale.Status == dbstore.SaleRefunded {
		t.Errorf("sale status = %q after a partial refund — must stay partially refunded", sale.Status)
	}

	// list_sales must show the partial state.
	_, listed, err := st.srv.listSales(st.ctx, nil, listSalesIn{})
	if err != nil {
		t.Fatalf("list_sales: %v", err)
	}
	if listed == nil {
		t.Fatal("list_sales returned no output")
	}
	rows := listed.Items
	var found *saleOut
	for i := range rows {
		if rows[i].RefCode == st.refCode {
			found = &rows[i]
		}
	}
	if found == nil {
		t.Fatalf("list_sales did not return the sale")
	}
	if found.RefundedSen != 1000 || found.RefundStatus != dbstore.RefundPartial {
		t.Errorf("list_sales refunded_sen=%d refund_status=%q, want 1000 / %q", found.RefundedSen, found.RefundStatus, dbstore.RefundPartial)
	}

	// get_sales_summary must show the refunded total.
	_, sum, err := st.srv.salesSummary(st.ctx, nil, emptyIn{})
	if err != nil {
		t.Fatalf("get_sales_summary: %v", err)
	}
	if sum.RefundedSen != 1000 {
		t.Errorf("get_sales_summary refunded_sen = %d, want 1000", sum.RefundedSen)
	}
}

// TestIssue518MCPOverRefundRejectedBeforeChip: the tool must surface the
// balance error and never reach CHIP.
func TestIssue518MCPOverRefundRejectedBeforeChip(t *testing.T) {
	st := issue518MCPFixture(t, 5000)
	first := int64(4000)
	if _, _, err := st.srv.refundSaleMCP(st.ctx, nil, saleRefIn{RefCode: st.refCode, AmountSen: &first}); err != nil {
		t.Fatalf("first refund: %v", err)
	}
	before := atomic.LoadInt64(st.refundHits)

	over := int64(2000)
	_, _, err := st.srv.refundSaleMCP(st.ctx, nil, saleRefIn{RefCode: st.refCode, AmountSen: &over})
	if err == nil {
		t.Fatalf("refund_sale ACCEPTED an over-refund (4000 + 2000 > 5000)")
	}
	if !strings.Contains(err.Error(), "baki") {
		t.Errorf("over-refund error = %q, want it to name the remaining balance so the agent can act", err)
	}
	if after := atomic.LoadInt64(st.refundHits); after != before {
		t.Errorf("CHIP refund calls = %d (was %d) — the tool must reject an over-refund before calling CHIP", after, before)
	}
	sale, _ := st.srv.deps.Sales.ByID(st.ctx, st.saleID)
	if sale.RefundedSen != 4000 {
		t.Errorf("sale.refunded_sen = %d, want 4000", sale.RefundedSen)
	}
}

// TestIssue518MCPNonPositiveAmountRejected preserves the existing > 0 check.
func TestIssue518MCPNonPositiveAmountRejected(t *testing.T) {
	st := issue518MCPFixture(t, 5000)
	for _, amount := range []int64{0, -100} {
		a := amount
		if _, _, err := st.srv.refundSaleMCP(st.ctx, nil, saleRefIn{RefCode: st.refCode, AmountSen: &a}); err == nil {
			t.Errorf("refund_sale accepted amount_sen = %d", amount)
		}
	}
	if n := atomic.LoadInt64(st.refundHits); n != 0 {
		t.Errorf("CHIP refund calls = %d, want 0", n)
	}
}
