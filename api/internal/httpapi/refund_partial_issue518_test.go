package httpapi

// Issue #518 — partial-refund accounting (money correctness).
//
// Before the fix, a PARTIAL refund marked the sale fully `refunded` and
// recorded nothing about how much was actually returned:
//
//   - `payments.RefundSale` passed the amount to CHIP but tracked nothing;
//   - BOTH callers (REST refundSale, MCP refund_sale) unconditionally flipped
//     the sale to the terminal `refunded` status, ignoring the amount;
//   - the `sales` table had nowhere to record the refunded amount, so there was
//     no outstanding balance and no over-refund guard.
//
// Consequence for amount_sen = 1000 on a RM 50.00 sale: CHIP returns RM 10
// (correct money), the sale reads as FULLY refunded on every surface
// (list/report/CSV/MCP), and the remaining RM 40 can never be refunded through
// JomForm — the paid/confirmed guard rejects it. Merchants then refund the rest
// outside JomForm, which is how a genuine over-refund happens.
//
// These are the end-to-end regression tests for the fix, driven over the real
// REST refund route against a stub CHIP API so the "rejected BEFORE CHIP is
// called" requirement is asserted on an actual request count, not on a
// comment. Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL);
// skipped without it.
//
// NOTE: the recorded amount is read with raw SQL on purpose. That keeps the
// failure mode of the pre-fix code honest: this file compiles against the
// un-fixed tree (no Go symbol depends on the fix) and fails at runtime with
// `column "refunded_sen" does not exist` plus the status assertions below.

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
)

// issue518Fixture is one account with CHIP credentials, a PAID sale with a CHIP
// purchase id, REST handlers wired to a stub CHIP API, and an API key.
type issue518Fixture struct {
	pool       *pgxpool.Pool
	h          *RESTHandlers
	key        string
	saleID     string
	refCode    string
	refundHits *int64
}

// seedIssue518Fixture builds the fixture. totalSen is the sale's total; the
// stub CHIP API records every refund request in refundHits.
func seedIssue518Fixture(t *testing.T, totalSen int64) issue518Fixture {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)

	accountID, _ := bundleFixture(t, ctx, pool)

	h := newBundleHandlers(pool)
	h.Events = dbstore.NewEventStore(pool)

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	sealed, err := cryptoutil.Encrypt(key, []byte("sk_test_issue518_secret"))
	if err != nil {
		t.Fatalf("encrypt chip secret: %v", err)
	}
	if err := dbstore.NewAccountStore(pool).SetChipCredentials(ctx, accountID, "br_518", sealed, "test", "", nil, true); err != nil {
		t.Fatalf("set chip creds: %v", err)
	}

	var refundHits int64
	mux := http.NewServeMux()
	mux.HandleFunc("POST /purchases/{id}/refund/", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&refundHits, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": r.PathValue("id"), "status": chip.StatusRefunded})
	})
	chipSrv := httptest.NewServer(mux)
	t.Cleanup(chipSrv.Close)

	h.Pay = payments.New(chip.New(chipSrv.URL), dbstore.NewAccountStore(pool), h.Sales,
		dbstore.NewSubscriptionStore(pool), key, "http://localhost:8080")

	ref := "JF-518-" + accountID[:8]
	sale := &dbstore.Sale{
		AccountID: accountID, RefCode: ref,
		Client:      dbstore.SaleClient{FullName: "Ali", Email: "ali@example.test"},
		Items:       []dbstore.SaleItem{{ProductID: "p518", Name: "Tee", PriceSen: totalSen, Qty: 1}},
		SubtotalSen: totalSen, TotalSen: totalSen,
		SaleType: dbstore.SaleTypeOneTime, PaymentMethod: "chip", Status: dbstore.SalePaid,
	}
	if err := h.Sales.Create(ctx, sale); err != nil {
		t.Fatalf("create sale: %v", err)
	}
	if err := h.Sales.AttachChipPurchase(ctx, sale.ID, "pur_518_"+accountID[:8], "https://gate.example/checkout/pur_518"); err != nil {
		t.Fatalf("attach purchase: %v", err)
	}

	raw := bundleAPIKey(t, ctx, pool, accountID)
	return issue518Fixture{
		pool: pool, h: h, key: raw, saleID: sale.ID, refCode: ref, refundHits: &refundHits,
	}
}

// refund posts to the REST refund route and returns the status code + body.
func (f issue518Fixture) refund(t *testing.T, body string) (int, map[string]any) {
	t.Helper()
	mux := http.NewServeMux()
	f.h.RegisterRoutes(mux)
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(http.MethodPost, "/v1/rest/sales/"+f.refCode+"/refund", nil)
	} else {
		req = httptest.NewRequest(http.MethodPost, "/v1/rest/sales/"+f.refCode+"/refund", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+f.key)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// refundedSen reads the recorded refunded amount straight from the sales row.
// A missing column is a hard failure with an explicit message: recording the
// refunded amount IS the fix (issue #518).
func (f issue518Fixture) refundedSen(t *testing.T) int64 {
	t.Helper()
	var sen int64
	if err := f.pool.QueryRow(context.Background(),
		`SELECT refunded_sen FROM sales WHERE id=$1`, f.saleID).Scan(&sen); err != nil {
		t.Fatalf("read sales.refunded_sen: %v — the refunded amount must be recorded on the sale (issue #518)", err)
	}
	return sen
}

// status reads the sale's current status.
func (f issue518Fixture) status(t *testing.T) string {
	t.Helper()
	sale, err := f.h.Sales.ByID(context.Background(), f.saleID)
	if err != nil {
		t.Fatalf("reload sale: %v", err)
	}
	return sale.Status
}

// TestIssue518PartialRefundRecordsAmountAndStaysPartial is the core defect: a
// partial refund must record the amount and must NOT read as fully refunded.
func TestIssue518PartialRefundRecordsAmountAndStaysPartial(t *testing.T) {
	f := seedIssue518Fixture(t, 5000) // RM 50.00

	code, body := f.refund(t, `{"amount_sen":1000}`)
	if code != http.StatusOK {
		t.Fatalf("partial refund status = %d, want 200 (body %v)", code, body)
	}
	if got := f.refundedSen(t); got != 1000 {
		t.Errorf("sales.refunded_sen = %d, want 1000 (a partial refund must record what was returned)", got)
	}
	// Status: see the PR for the choice — a partially refunded sale keeps its
	// paid/confirmed status (the money only reads as refunded when the full
	// total is returned), and the read surfaces expose the partial state.
	if got := f.status(t); got == dbstore.SaleRefunded {
		t.Errorf("sale status = %q after a PARTIAL refund (1000 of 5000) — the sale must not read as fully refunded", got)
	}
	if n := atomic.LoadInt64(f.refundHits); n != 1 {
		t.Errorf("CHIP refund calls = %d, want 1", n)
	}

	// The read surface must expose both the recorded amount and the partial state.
	var row struct {
		RefundedSen  int64  `json:"refunded_sen"`
		RefundStatus string `json:"refund_status"`
	}
	code, raw := issue518ListSale(t, f)
	if code != http.StatusOK {
		t.Fatalf("list sales status = %d", code)
	}
	if err := json.Unmarshal(raw, &row); err != nil {
		t.Fatalf("decode list row: %v (%s)", err, raw)
	}
	if row.RefundedSen != 1000 {
		t.Errorf("list_sales refunded_sen = %d, want 1000 — the partial refund must be visible", row.RefundedSen)
	}
	if row.RefundStatus != "partial" {
		t.Errorf("list_sales refund_status = %q, want %q", row.RefundStatus, "partial")
	}
}

// issue518ListSale fetches the single sale from GET /v1/rest/sales and returns
// its first row (the shape the Sales page and CSV consumers read).
func issue518ListSale(t *testing.T, f issue518Fixture) (int, []byte) {
	t.Helper()
	mux := http.NewServeMux()
	f.h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/v1/rest/sales", nil)
	req.Header.Set("Authorization", "Bearer "+f.key)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var rows []json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil || len(rows) == 0 {
		t.Fatalf("list sales: %d rows (%s) err=%v", len(rows), rec.Body.String(), err)
	}
	return rec.Code, rows[0]
}

// TestIssue518PartialThenRemainderCompletesRefundExactlyOnce refunds RM 10 of
// RM 50, then the remaining RM 40. Only the second call may mark the sale
// fully refunded, and the recorded total must be exactly the sale total.
func TestIssue518PartialThenRemainderCompletesRefundExactlyOnce(t *testing.T) {
	f := seedIssue518Fixture(t, 5000)

	if code, body := f.refund(t, `{"amount_sen":1000}`); code != http.StatusOK {
		t.Fatalf("first partial refund status = %d (body %v)", code, body)
	}
	if got := f.status(t); got == dbstore.SaleRefunded {
		t.Fatalf("sale fully refunded after a partial refund")
	}

	if code, body := f.refund(t, `{"amount_sen":4000}`); code != http.StatusOK {
		t.Fatalf("remainder refund status = %d, want 200 (body %v)", code, body)
	}
	if got := f.refundedSen(t); got != 5000 {
		t.Errorf("sales.refunded_sen = %d, want 5000 after refunding the remainder", got)
	}
	if got := f.status(t); got != dbstore.SaleRefunded {
		t.Errorf("sale status = %q, want %q once the full total has been refunded", got, dbstore.SaleRefunded)
	}
	if n := atomic.LoadInt64(f.refundHits); n != 2 {
		t.Errorf("CHIP refund calls = %d, want 2 (one per refund)", n)
	}

	// A third attempt must be rejected by the refundable-state guard: the sale
	// is fully refunded, so nothing is left to return.
	if code, _ := f.refund(t, `{"amount_sen":100}`); code != http.StatusBadRequest {
		t.Errorf("refund after full refund status = %d, want 400", code)
	}
	if n := atomic.LoadInt64(f.refundHits); n != 2 {
		t.Errorf("CHIP refund calls = %d, want 2 — no CHIP call may follow a full refund", n)
	}
}

// TestIssue518OverRefundRejectedBeforeChip is the missing over-refund guard:
// refunded_sen + amount_sen > total_sen must be rejected BEFORE CHIP is called.
func TestIssue518OverRefundRejectedBeforeChip(t *testing.T) {
	f := seedIssue518Fixture(t, 5000)

	if code, body := f.refund(t, `{"amount_sen":4000}`); code != http.StatusOK {
		t.Fatalf("first refund status = %d (body %v)", code, body)
	}
	before := atomic.LoadInt64(f.refundHits)

	code, body := f.refund(t, `{"amount_sen":2000}`) // 4000 + 2000 > 5000
	if code != http.StatusBadRequest {
		t.Fatalf("over-refund status = %d, want 400 (body %v)", code, body)
	}
	if after := atomic.LoadInt64(f.refundHits); after != before {
		t.Errorf("CHIP refund calls = %d (was %d) — an over-refund must be rejected BEFORE calling CHIP", after, before)
	}
	if got := f.refundedSen(t); got != 4000 {
		t.Errorf("sales.refunded_sen = %d, want 4000 (a rejected refund must not be recorded)", got)
	}
	// The remaining balance is still refundable — the guard must not consume it.
	if code, body := f.refund(t, `{"amount_sen":1000}`); code != http.StatusOK {
		t.Fatalf("remainder refund after rejection status = %d, want 200 (body %v)", code, body)
	}
	if got := f.refundedSen(t); got != 5000 {
		t.Errorf("sales.refunded_sen = %d, want 5000", got)
	}
}

// TestIssue518NonPositiveAmountStillRejected preserves the pre-existing
// validation (0 / negative amounts never reach CHIP).
func TestIssue518NonPositiveAmountStillRejected(t *testing.T) {
	f := seedIssue518Fixture(t, 5000)
	for _, body := range []string{`{"amount_sen":0}`, `{"amount_sen":-500}`} {
		code, resp := f.refund(t, body)
		if code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (body %v)", body, code, resp)
		}
	}
	if n := atomic.LoadInt64(f.refundHits); n != 0 {
		t.Errorf("CHIP refund calls = %d, want 0 for non-positive amounts", n)
	}
	if got := f.refundedSen(t); got != 0 {
		t.Errorf("sales.refunded_sen = %d, want 0", got)
	}
}

// TestIssue518NonRefundableSaleStillRejected preserves the paid/confirmed
// guard: a pending sale cannot be refunded.
func TestIssue518NonRefundableSaleStillRejected(t *testing.T) {
	f := seedIssue518Fixture(t, 5000)
	if err := f.h.Sales.SetStatus(context.Background(), f.saleID, dbstore.SalePending); err != nil {
		t.Fatalf("reset status: %v", err)
	}
	code, resp := f.refund(t, `{"amount_sen":1000}`)
	if code != http.StatusBadRequest {
		t.Errorf("refund of a pending sale status = %d, want 400 (body %v)", code, resp)
	}
	if n := atomic.LoadInt64(f.refundHits); n != 0 {
		t.Errorf("CHIP refund calls = %d, want 0 — a non-refundable sale must be rejected before CHIP", n)
	}
	if got := f.refundedSen(t); got != 0 {
		t.Errorf("sales.refunded_sen = %d, want 0", got)
	}
}

// TestIssue518RefundAuditEventKeepsAmountAndRunningTotal preserves the #407
// audit event (`sale.refunded`) and extends it with the running refunded total.
func TestIssue518RefundAuditEventKeepsAmountAndRunningTotal(t *testing.T) {
	f := seedIssue518Fixture(t, 5000)

	if code, body := f.refund(t, `{"amount_sen":1000}`); code != http.StatusOK {
		t.Fatalf("partial refund status = %d (body %v)", code, body)
	}

	ctx := context.Background()
	var propsRaw []byte
	if err := f.pool.QueryRow(ctx,
		`SELECT properties FROM events WHERE event_name='sale.refunded' AND account_id=(SELECT account_id FROM sales WHERE id=$1) ORDER BY id DESC LIMIT 1`,
		f.saleID).Scan(&propsRaw); err != nil {
		t.Fatalf("read sale.refunded audit event: %v (issue #407 regression)", err)
	}
	var props map[string]any
	if err := json.Unmarshal(propsRaw, &props); err != nil {
		t.Fatalf("decode props: %v", err)
	}
	if props["ref_code"] != f.refCode {
		t.Errorf("audit props ref_code = %v, want %q", props["ref_code"], f.refCode)
	}
	if got, _ := props["amount_sen"].(float64); got != 1000 {
		t.Errorf("audit props amount_sen = %v, want 1000 (issue #407)", props["amount_sen"])
	}
	if got, _ := props["refunded_sen"].(float64); got != 1000 {
		t.Errorf("audit props refunded_sen = %v, want the running refunded total 1000", props["refunded_sen"])
	}
	if got, _ := props["fully_refunded"].(bool); got {
		t.Errorf("audit props fully_refunded = true after a PARTIAL refund")
	}
}
