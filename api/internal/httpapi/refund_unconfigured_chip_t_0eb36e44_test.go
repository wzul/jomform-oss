package httpapi

// Card t_0eb36e44: the REST door's answer for a refund / capture / release on an
// account with NO CHIP keys.
//
// Before the fix the body was 400 {"code":"request.badRequest","error":"ciphertext
// too short"} — cryptoutil's message for decrypting the empty stored key, which
// tells the merchant nothing and leaks an implementation detail. It now answers
// 400 with the ErrNotConfigured refusal (code payment.notConfigured), and the
// other case — a stored key that will not decrypt — answers a masked 500
// (code internal), because that one is a server-side fault, not a bad request.
//
// Driven over the real REST routes with an API key, against a stub CHIP API, so
// "the gateway was never called" is an assertion on a request count.
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

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/payments"
)

// t0eb36RESTFixture is RESTHandlers + a payments Service over one account that
// has no usable CHIP key, with a stub CHIP API that counts lifecycle calls.
type t0eb36RESTFixture struct {
	h        *RESTHandlers
	key      string
	account  string
	saleID   string
	refCode  string
	chipHits *int64
}

// t0eb36RESTSeed builds the fixture. corruptKey=false leaves the account's CHIP
// columns empty (a fresh signup); true stores a well-formed secret sealed under
// a DIFFERENT key, which is what a rotated ENCRYPTION_KEY leaves behind.
func t0eb36RESTSeed(t *testing.T, corruptKey, skipCapture bool) t0eb36RESTFixture {
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
	if corruptKey {
		other := make([]byte, 32)
		for i := range other {
			other[i] = byte(255 - i)
		}
		sealed, err := cryptoutil.Encrypt(other, []byte("sk_live_sealed_under_old_key"))
		if err != nil {
			t.Fatalf("seal under old key: %v", err)
		}
		if err := dbstore.NewAccountStore(pool).SetChipCredentials(ctx, accountID, "br_t0eb36", sealed, "test", "", nil, true); err != nil {
			t.Fatalf("set chip creds: %v", err)
		}
	}

	var hits int64
	mux := http.NewServeMux()
	record := func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": r.PathValue("id"), "status": chip.StatusRefunded})
	}
	mux.HandleFunc("POST /purchases/{id}/capture/", record)
	mux.HandleFunc("POST /purchases/{id}/release/", record)
	mux.HandleFunc("POST /purchases/{id}/refund/", record)
	chipSrv := httptest.NewServer(mux)
	t.Cleanup(chipSrv.Close)

	h.Pay = payments.New(chip.New(chipSrv.URL), dbstore.NewAccountStore(pool), h.Sales,
		dbstore.NewSubscriptionStore(pool), key, "http://localhost:8080")

	ref := "JF-T0EB36-" + accountID[:8]
	sale := &dbstore.Sale{
		AccountID: accountID, RefCode: ref,
		Client:      dbstore.SaleClient{FullName: "Ali", Email: "ali@example.test"},
		Items:       []dbstore.SaleItem{{ProductID: "p-t0eb36", Name: "Tee", PriceSen: 5000, Qty: 1}},
		SubtotalSen: 5000, TotalSen: 5000,
		SaleType: dbstore.SaleTypeOneTime, PaymentMethod: "chip", Status: dbstore.SalePaid,
		SkipCapture: skipCapture,
	}
	if err := h.Sales.Create(ctx, sale); err != nil {
		t.Fatalf("create sale: %v", err)
	}
	if err := h.Sales.AttachChipPurchase(ctx, sale.ID, "pur_t0eb36_"+accountID[:8], "https://gate.example/checkout/pur_t0eb36"); err != nil {
		t.Fatalf("attach purchase: %v", err)
	}

	return t0eb36RESTFixture{
		h: h, key: bundleAPIKey(t, ctx, pool, accountID), account: accountID,
		saleID: sale.ID, refCode: ref, chipHits: &hits,
	}
}

// post drives one REST route with the fixture's API key and decodes the error
// envelope (the only shape these routes answer on failure).
func (f t0eb36RESTFixture) post(t *testing.T, path string) (int, map[string]any, string) {
	t.Helper()
	mux := http.NewServeMux()
	f.h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, path, nil)
	req.Header.Set("Authorization", "Bearer "+f.key)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body, rec.Body.String()
}

// TestRESTRefundWithoutChipKeysAnswersNotConfigured is the card's repro: a PAID
// sale with a CHIP purchase id on an account with empty CHIP columns.
func TestRESTRefundWithoutChipKeysAnswersNotConfigured(t *testing.T) {
	f := t0eb36RESTSeed(t, false, false)

	code, body, raw := f.post(t, "/v1/rest/sales/"+f.refCode+"/refund")
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", code, raw)
	}
	if got := body["error"]; got != paymentNotConfiguredText {
		t.Errorf("error = %q; want the not-configured sentence %q", got, paymentNotConfiguredText)
	}
	// The refusal's own code, not the generic bad-request class: the dashboard
	// translates by code, so this is what lets it name the cause.
	if got := body["code"]; got != CodePaymentNotConfigured {
		t.Errorf("code = %q; want %q", got, CodePaymentNotConfigured)
	}
	if strings.Contains(raw, "ciphertext") {
		t.Errorf("body leaks the crypto detail: %s", raw)
	}
	if got := atomic.LoadInt64(f.chipHits); got != 0 {
		t.Errorf("CHIP refund called %d time(s); an unconfigured account must never reach the gateway", got)
	}
	f.assertSaleUntouched(t)
}

// TestRESTCaptureAndReleaseWithoutChipKeysAnswerNotConfigured covers the other
// two doors through the same function: they answer the same refusal, because
// the failure is the account's configuration and not the operation.
func TestRESTCaptureAndReleaseWithoutChipKeysAnswerNotConfigured(t *testing.T) {
	for _, route := range []string{"capture", "release"} {
		t.Run(route, func(t *testing.T) {
			f := t0eb36RESTSeed(t, false, true) // an authorization awaiting capture
			code, body, raw := f.post(t, "/v1/rest/sales/"+f.refCode+"/"+route)
			if code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", code, raw)
			}
			if got := body["error"]; got != paymentNotConfiguredText {
				t.Errorf("error = %q; want the not-configured sentence %q", got, paymentNotConfiguredText)
			}
			if got := body["code"]; got != CodePaymentNotConfigured {
				t.Errorf("code = %q; want %q", got, CodePaymentNotConfigured)
			}
			if strings.Contains(raw, "ciphertext") {
				t.Errorf("body leaks the crypto detail: %s", raw)
			}
			if got := atomic.LoadInt64(f.chipHits); got != 0 {
				t.Errorf("CHIP %s called %d time(s)", route, got)
			}
		})
	}
}

// TestRESTRefundWithUnreadableChipKeyAnswersMasked500 pins the decision this
// card had to make: a key that IS stored but will not decrypt is a server-side
// fault, so it answers the generic masked 500 — not a 400 that blames the
// request, and never the crypto text.
func TestRESTRefundWithUnreadableChipKeyAnswersMasked500(t *testing.T) {
	f := t0eb36RESTSeed(t, true, false)

	code, body, raw := f.post(t, "/v1/rest/sales/"+f.refCode+"/refund")
	if code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body %s)", code, raw)
	}
	if got := body["error"]; got != "internal error" {
		t.Errorf("error = %q; want the masked %q", got, "internal error")
	}
	if got := body["code"]; got != ClassInternal {
		t.Errorf("code = %q; want %q", got, ClassInternal)
	}
	for _, leak := range []string{"ciphertext", "message authentication failed", "decrypt"} {
		if strings.Contains(raw, leak) {
			t.Errorf("body leaks %q: %s", leak, raw)
		}
	}
	if got := atomic.LoadInt64(f.chipHits); got != 0 {
		t.Errorf("CHIP refund called %d time(s) without a usable key", got)
	}
	f.assertSaleUntouched(t)
}

// assertSaleUntouched checks the failed refund/capture recorded nothing: the
// money state must be exactly as seeded.
func (f t0eb36RESTFixture) assertSaleUntouched(t *testing.T) {
	t.Helper()
	pool := bundleTestPool(t)
	var refunded int64
	var status string
	if err := pool.QueryRow(context.Background(),
		`SELECT refunded_sen, status FROM sales WHERE id=$1`, f.saleID).Scan(&refunded, &status); err != nil {
		t.Fatalf("reload sale: %v", err)
	}
	if refunded != 0 {
		t.Errorf("refunded_sen = %d; a refused refund must record nothing", refunded)
	}
	if status != dbstore.SalePaid {
		t.Errorf("status = %q; a refused refund/capture must not move the sale", status)
	}
}
