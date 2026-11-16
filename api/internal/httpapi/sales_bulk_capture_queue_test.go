package httpapi

// THE BULK-CAPTURE ROUTE CHARGES WHAT THE PER-SALE ROUTE CHARGES.
//
// The card: a sale CONFIRMED before its authorization was captured could be
// charged one sale at a time — `POST /v1/rest/sales/{ref}/capture` and MCP
// `capture_sale` both pass the guard — while `POST /v1/rest/sales/bulk-capture`
// with `refs="all"` never offered it, because its queue carried a hand-written
// status list that predated the lifecycle table. The authorization then expired
// and the merchant was never charged for money they believed was covered.
//
// The fix is in the queue (dbstore.ListAwaitingCapture now offers exactly the
// states the table declares a `capture` edge from; pinned by
// TestCaptureQueueOffersExactlyWhatTheTableAllows in internal/lifecycle). These
// tests are the END-TO-END half: they drive the real REST route against a stub
// CHIP API, so the claim being checked is "CHIP was actually asked to charge this
// sale", counted on real requests, not read out of a comment.
//
// Both entry points the merchant can reach are covered:
//
//   - `{"refs":"all"}` — what an agent's capture_all_pending_sales uses, and the
//     queue path that was broken;
//   - `{"refs":["JF-…"]}` — what the Sales page's own "Capture all pending"
//     button sends (it lists the rows it believes are capturable).
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL); skipped
// without it.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/payments"
)

// bulkCaptureFixture is one account with CHIP credentials, REST handlers wired to
// a stub CHIP capture endpoint, and an API key.
type bulkCaptureFixture struct {
	pool       *pgxpool.Pool
	h          *RESTHandlers
	key        string
	accountID  string
	captureHit *int64
}

func seedBulkCaptureFixture(t *testing.T) bulkCaptureFixture {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)

	h := newBundleHandlers(pool)
	h.Events = dbstore.NewEventStore(pool)

	// The same 32-byte key the fixture's payments service decrypts with.
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	sealed, err := cryptoutil.Encrypt(key, []byte("sk_test_bulk_capture"))
	if err != nil {
		t.Fatalf("encrypt chip secret: %v", err)
	}
	if err := dbstore.NewAccountStore(pool).SetChipCredentials(ctx, accountID, "br_bulk", sealed, "test", "", nil, true); err != nil {
		t.Fatalf("set chip creds: %v", err)
	}

	var hits int64
	mux := http.NewServeMux()
	mux.HandleFunc("POST /purchases/{id}/capture/", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": r.PathValue("id"), "status": chip.StatusPaid})
	})
	stub := httptest.NewServer(mux)
	t.Cleanup(stub.Close)

	h.Pay = payments.New(chip.New(stub.URL), dbstore.NewAccountStore(pool), h.Sales,
		dbstore.NewSubscriptionStore(pool), key, "http://localhost:8080")

	return bulkCaptureFixture{
		pool: pool, h: h, key: bundleAPIKey(t, ctx, pool, accountID),
		accountID: accountID, captureHit: &hits,
	}
}

// newSale inserts one sale in the given state and returns its ref code and id.
// `hold` attaches a CHIP purchase id, i.e. an authorization that can be captured.
func (f bulkCaptureFixture) newSale(t *testing.T, status string, skipCapture, hold bool) (string, string) {
	t.Helper()
	ctx := context.Background()
	ref := "JF-BC-" + uuid.NewString()[:8]
	sale := &dbstore.Sale{
		AccountID: f.accountID, RefCode: ref,
		Client:      dbstore.SaleClient{FullName: "Ali", Email: "ali@example.test"},
		Items:       []dbstore.SaleItem{{ProductID: "p-bc", Name: "Tee", PriceSen: 5000, Qty: 1}},
		SubtotalSen: 5000, TotalSen: 5000,
		SaleType: dbstore.SaleTypeOneTime, PaymentMethod: "chip", Status: status,
		SkipCapture: skipCapture,
	}
	if err := f.h.Sales.Create(ctx, sale); err != nil {
		t.Fatalf("create the %s sale: %v", status, err)
	}
	if hold {
		if err := f.h.Sales.AttachChipPurchase(ctx, sale.ID, "pur_bc_"+sale.ID[:8], "https://gate.example/checkout/pur_bc"); err != nil {
			t.Fatalf("attach purchase: %v", err)
		}
	}
	return ref, sale.ID
}

// bulkCapture POSTs the route the way the page and an agent do.
func (f bulkCaptureFixture) bulkCapture(t *testing.T, body string) (int, map[string]any) {
	t.Helper()
	mux := http.NewServeMux()
	f.h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/v1/rest/sales/bulk-capture", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+f.key)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("bulk-capture returned a non-JSON body (%d): %s", rec.Code, rec.Body.String())
	}
	return rec.Code, out
}

// TestBulkCaptureAllChargesAConfirmedAuthorization is the regression: a sale the
// merchant confirmed while its authorization was still uncaptured IS captured by
// "Capture all", and a row that was already captured is not charged again.
func TestBulkCaptureAllChargesAConfirmedAuthorization(t *testing.T) {
	f := seedBulkCaptureFixture(t)
	ctx := context.Background()

	// The row the card is about: confirmed BEFORE its authorization was captured.
	confirmedRef, confirmedID := f.newSale(t, dbstore.SaleConfirmed, true, true)
	// The control: an authorization that was already captured must not be charged
	// a second time by the batch.
	_, capturedID := f.newSale(t, dbstore.SalePaid, true, true)
	if err := f.h.Sales.MarkCaptured(ctx, capturedID); err != nil {
		t.Fatalf("MarkCaptured on the control sale: %v", err)
	}
	// And an ordinary (non-skip_capture) sale is never a hold.
	f.newSale(t, dbstore.SaleConfirmed, false, true)

	code, body := f.bulkCapture(t, `{"refs":"all"}`)
	if code != http.StatusOK {
		t.Fatalf("POST bulk-capture refs=all = %d, want 200: %v", code, body)
	}
	if got := body["requested"]; got != float64(1) {
		t.Errorf("requested = %v, want 1 (exactly the confirmed uncaptured authorization): %v", got, body)
	}
	if got := body["captured"]; got != float64(1) {
		t.Errorf("captured = %v, want 1: %v", got, body)
	}
	if got := body["failed"]; got != float64(0) {
		t.Errorf("failed = %v, want 0: %v", got, body)
	}
	results, _ := body["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results = %v, want one row (the confirmed authorization)", results)
	}
	one, _ := results[0].(map[string]any)
	if one == nil || one["ref_code"] != confirmedRef || one["ok"] != true {
		t.Fatalf("results[0] = %v, want {ref_code:%s ok:true}", results[0], confirmedRef)
	}
	if n := atomic.LoadInt64(f.captureHit); n != 1 {
		t.Errorf("CHIP capture requests = %d, want 1 — an already-captured row must never reach the gateway", n)
	}

	// The sale is charged: status is the table's capture target, the hold is
	// stamped, and the money is marked captured.
	sale, err := f.h.Sales.ByID(ctx, confirmedID)
	if err != nil {
		t.Fatalf("re-read the captured sale: %v", err)
	}
	if sale.Status != dbstore.SalePaid {
		t.Errorf("status after bulk capture = %q, want %q", sale.Status, dbstore.SalePaid)
	}
	if sale.CapturedAt == nil || !sale.Authorized {
		t.Errorf("after bulk capture: captured_at=%v authorized=%v, want both stamped", sale.CapturedAt, sale.Authorized)
	}

	// A second run must find nothing: the queue is drained, and a re-run cannot
	// charge the same authorization twice.
	code, body = f.bulkCapture(t, `{"refs":"all"}`)
	if code != http.StatusOK {
		t.Fatalf("second POST bulk-capture = %d, want 200: %v", code, body)
	}
	if got := body["requested"]; got != float64(0) {
		t.Errorf("second run requested = %v, want 0: %v", got, body)
	}
	if n := atomic.LoadInt64(f.captureHit); n != 1 {
		t.Errorf("CHIP capture requests = %d after a re-run, want 1", n)
	}
}

// TestBulkCaptureByRefChargesAConfirmedAuthorization covers the other door: the
// Sales page's button sends the refs it believes are capturable, and that path
// never had a status filter — the guard decides. It is pinned here so the two
// doors cannot drift apart again in the other direction (the page now lists rows
// from the server's own `available_events`, which is this guard's answer).
func TestBulkCaptureByRefChargesAConfirmedAuthorization(t *testing.T) {
	f := seedBulkCaptureFixture(t)
	ctx := context.Background()

	confirmedRef, confirmedID := f.newSale(t, dbstore.SaleConfirmed, true, true)
	// A per-ref request for a row that is NOT an authorization is a per-row
	// refusal, not a failure of the batch: the sentence the merchant reads.
	ordinaryRef, _ := f.newSale(t, dbstore.SalePending, false, true)

	code, body := f.bulkCapture(t, `{"refs":["`+confirmedRef+`","`+ordinaryRef+`"]}`)
	if code != http.StatusOK {
		t.Fatalf("POST bulk-capture with refs = %d, want 200: %v", code, body)
	}
	if got := body["requested"]; got != float64(2) {
		t.Fatalf("requested = %v, want 2: %v", got, body)
	}
	if got := body["captured"]; got != float64(1) {
		t.Errorf("captured = %v, want 1 (the confirmed authorization): %v", got, body)
	}
	if got := body["failed"]; got != float64(1) {
		t.Errorf("failed = %v, want 1 (the ordinary sale is refused per row): %v", got, body)
	}
	results, _ := body["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("results = %v, want two rows", results)
	}
	var refused string
	for _, r := range results {
		row, _ := r.(map[string]any)
		if row == nil {
			continue
		}
		if row["ref_code"] == ordinaryRef {
			refused, _ = row["error"].(string)
		}
	}
	if !strings.Contains(refused, "not an authorization awaiting capture") {
		t.Errorf("per-row refusal = %q, want the awaiting-capture sentence the results panel shows", refused)
	}

	sale, err := f.h.Sales.ByID(ctx, confirmedID)
	if err != nil {
		t.Fatalf("re-read the captured sale: %v", err)
	}
	if sale.Status != dbstore.SalePaid || sale.CapturedAt == nil {
		t.Errorf("after a refs capture: status=%q captured_at=%v, want paid + stamped", sale.Status, sale.CapturedAt)
	}
	if n := atomic.LoadInt64(f.captureHit); n != 1 {
		t.Errorf("CHIP capture requests = %d, want 1", n)
	}
}
