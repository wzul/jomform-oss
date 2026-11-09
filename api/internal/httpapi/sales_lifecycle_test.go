package httpapi

// The sales lifecycle table wired into the REST door.
//
// The machine's own tests (internal/lifecycle) prove the TABLE. These prove the
// WIRING: that the handlers consult it rather than accepting whatever state a
// caller sends, and — the money half — that an illegal capture/release is
// refused BEFORE the CHIP gateway is called.
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
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/lifecycle"
	"github.com/wzul/jomform/api/internal/payments"
)

// TestLifecycleRefusalsCarryTheirMachineCodeOnTheWire pins the code path that
// makes the machine's stable code reach a client: the typed error names it, and
// the door's code lookup reads it off the message instead of keeping a second
// table that could drift.
func TestLifecycleRefusalsCarryTheirMachineCodeOnTheWire(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{&lifecycle.IllegalTransitionError{Machine: "sales", From: "cancelled", Event: "confirm"},
			lifecycle.CodeIllegalTransition},
		{&lifecycle.TransitionRefusedError{Machine: "sales", From: "pending", Event: "capture", Reason: "not an authorization awaiting capture"},
			lifecycle.CodeTransitionRefused},
	}
	for _, c := range cases {
		if got := codeFor(http.StatusBadRequest, c.err.Error()); got != c.want {
			t.Errorf("codeFor(%q) = %q, want %q", c.err.Error(), got, c.want)
		}
	}
	// An ordinary message is untouched: this must not swallow the class code.
	if got := codeFor(http.StatusBadRequest, "some other refusal"); got != ClassBadRequest {
		t.Errorf("an unrelated message answered %q, want the class code %q", got, ClassBadRequest)
	}
}

type salesLifecycleFixture struct {
	pool      *pgxpool.Pool
	h         *RESTHandlers
	key       string
	accountID string
	// gatewayHits counts every request the stub CHIP API received. An illegal
	// transition must leave it at zero.
	gatewayHits *int64
}

func seedSalesLifecycleFixture(t *testing.T) salesLifecycleFixture {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)

	h := newBundleHandlers(pool)
	h.Events = dbstore.NewEventStore(pool)

	var hits int64
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "pur_stub", "status": chip.StatusPaid})
	})
	gateway := httptest.NewServer(mux)
	t.Cleanup(gateway.Close)

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	h.Pay = payments.New(chip.New(gateway.URL), dbstore.NewAccountStore(pool), h.Sales,
		dbstore.NewSubscriptionStore(pool), key, "http://localhost:8080")

	return salesLifecycleFixture{
		pool: pool, h: h, key: bundleAPIKey(t, ctx, pool, accountID),
		accountID: accountID, gatewayHits: &hits,
	}
}

// newSale inserts a sale in the given state and returns its ref code.
func (f salesLifecycleFixture) newSale(t *testing.T, status string, skipCapture bool) string {
	t.Helper()
	ref := "JF-LC-" + uuid.NewString()[:8]
	sale := &dbstore.Sale{
		AccountID: f.accountID, RefCode: ref,
		Client:      dbstore.SaleClient{FullName: "Ali", Email: "ali@example.test"},
		Items:       []dbstore.SaleItem{{ProductID: "p-lc", Name: "Tee", PriceSen: 5000, Qty: 1}},
		SubtotalSen: 5000, TotalSen: 5000,
		SaleType: dbstore.SaleTypeOneTime, PaymentMethod: "chip", Status: status,
		SkipCapture: skipCapture,
	}
	if err := f.h.Sales.Create(context.Background(), sale); err != nil {
		t.Fatalf("create sale: %v", err)
	}
	return ref
}

func (f salesLifecycleFixture) post(t *testing.T, path string) (int, map[string]any) {
	t.Helper()
	mux := http.NewServeMux()
	f.h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, path, nil)
	req.Header.Set("Authorization", "Bearer "+f.key)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

func (f salesLifecycleFixture) statusOf(t *testing.T, ref string) string {
	t.Helper()
	var status string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT status FROM sales WHERE ref_code=$1`, ref).Scan(&status); err != nil {
		t.Fatalf("read sale status: %v", err)
	}
	return status
}

// TestSalesConfirmFollowsTheLifecycleTable pins both halves: the two real
// confirmation paths still work (bank transfer from pending, CHIP payment from
// paid), and the states the old unguarded endpoint used to accept — failed and
// cancelled — are refused with the stable machine code, leaving the row alone.
func TestSalesConfirmFollowsTheLifecycleTable(t *testing.T) {
	f := seedSalesLifecycleFixture(t)

	allowed := []string{dbstore.SalePending, dbstore.SalePaid, dbstore.SaleConfirmed}
	for _, status := range allowed {
		ref := f.newSale(t, status, false)
		code, body := f.post(t, "/v1/rest/sales/"+ref+"/confirm")
		if code != http.StatusOK {
			t.Errorf("confirm of a %s sale = %d (body %v), want 200", status, code, body)
		}
		if got := f.statusOf(t, ref); got != dbstore.SaleConfirmed {
			t.Errorf("confirm of a %s sale left status %q, want confirmed", status, got)
		}
	}

	refused := []string{dbstore.SaleFailed, dbstore.SaleCancelled, dbstore.SaleRefunded}
	for _, status := range refused {
		ref := f.newSale(t, status, false)
		code, body := f.post(t, "/v1/rest/sales/"+ref+"/confirm")
		if code != http.StatusBadRequest {
			t.Errorf("confirm of a %s sale = %d (body %v), want 400", status, code, body)
		}
		msg, _ := body["error"].(string)
		if !strings.Contains(msg, "lifecycle.illegalTransition") {
			t.Errorf("confirm of a %s sale = %q, want the stable lifecycle.illegalTransition code", status, msg)
		}
		if code, _ := body["code"].(string); code != "lifecycle.illegalTransition" {
			t.Errorf("confirm of a %s sale carried code %q, want lifecycle.illegalTransition on the wire", status, code)
		}
		if got := f.statusOf(t, ref); got != status {
			t.Errorf("a refused confirm changed the %s sale to %q", status, got)
		}
	}
}

// TestSalesCaptureAndReleaseNeverReachTheGatewayWhenIllegal is the money half:
// a sale that is not an uncaptured authorization must be refused locally, so a
// stray click cannot capture or void a payment at CHIP.
func TestSalesCaptureAndReleaseNeverReachTheGatewayWhenIllegal(t *testing.T) {
	f := seedSalesLifecycleFixture(t)

	for _, action := range []string{"release", "capture"} {
		// A pending sale that is NOT a skip_capture authorization: the table
		// defines both events for pending, and the guard is what refuses them.
		ref := f.newSale(t, dbstore.SalePending, false)
		code, body := f.post(t, "/v1/rest/sales/"+ref+"/"+action)
		if code != http.StatusBadRequest {
			t.Errorf("%s of a non-authorization = %d (body %v), want 400", action, code, body)
		}
		msg, _ := body["error"].(string)
		if !strings.Contains(msg, "not an authorization awaiting capture") {
			t.Errorf("%s refusal = %q, want the awaiting-capture sentence", action, msg)
		}
		if got := f.statusOf(t, ref); got != dbstore.SalePending {
			t.Errorf("a refused %s changed the sale to %q", action, got)
		}
	}
	if n := atomic.LoadInt64(f.gatewayHits); n != 0 {
		t.Errorf("CHIP gateway calls = %d, want 0 — an illegal transition must be refused before the gateway", n)
	}

	// A sale whose status does not accept the event at all is refused too, with
	// the missing-edge code rather than the guard's.
	ref := f.newSale(t, dbstore.SaleCancelled, true)
	code, body := f.post(t, "/v1/rest/sales/"+ref+"/capture")
	if code != http.StatusBadRequest {
		t.Errorf("capture of a cancelled sale = %d (body %v), want 400", code, body)
	}
	if msg, _ := body["error"].(string); !strings.Contains(msg, "lifecycle.illegalTransition") {
		t.Errorf("capture of a cancelled sale = %q, want lifecycle.illegalTransition", msg)
	}
	if n := atomic.LoadInt64(f.gatewayHits); n != 0 {
		t.Errorf("CHIP gateway calls = %d after a cancelled-sale capture, want 0", n)
	}
}
