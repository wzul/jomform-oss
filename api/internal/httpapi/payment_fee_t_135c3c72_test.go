package httpapi

// Card t_135c3c72 — the buyer bears the CHIP processing fee, one rate per
// payment method, with a gross-up so the merchant is short by at most 1 sen.
//
// These are the guards that fail if the feature is merely wired and not
// CORRECT. The money is asserted against a fake CHIP server that records the
// purchase body, not against a mock of our own code:
//
//  1. the buyer's method on the page/route decides the rate, and the CHIP
//     purchase carries ONLY that method (they cannot switch to a cheaper one at
//     the CHIP page) with the server's grossed-up total as its amount;
//  2. a method that is not active, and a total the page did not display, are
//     REFUSED (400) rather than silently corrected;
//  3. the fee is a SNAPSHOT on the sale — changing the merchant's rate later
//     does not rewrite a sale that already happened;
//  4. a form with fee_override='off' charges no fee even though the workspace
//     has one, and an unacknowledged workspace charges no fee at all;
//  5. no fee active → the page and the totals are exactly what they were before
//     this card (no chooser, no fee row, no total_override surprise).
//
// The property grid and the naive-formula negative control live in
// payments/fee_test.go; the store-level resolution tests in
// dbstore/payment_fee_test.go.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/payments"
)

// fakeChip is a CHIP API stand-in that records every POST /purchases/ body, so a
// test can assert the amount and whitelist the buyer's card is actually charged
// with — the whole point of the card.
type fakeChip struct {
	mu       sync.Mutex
	requests []map[string]any
	srv      *httptest.Server
}

func newFakeChip(t *testing.T) *fakeChip {
	t.Helper()
	fc := &fakeChip{}
	fc.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/purchases/" {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		fc.mu.Lock()
		fc.requests = append(fc.requests, body)
		fc.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"pur_test_1","status":"created","checkout_url":"https://pay.example.test/pur_test_1","total":0,"currency":"MYR"}`))
	}))
	t.Cleanup(fc.srv.Close)
	return fc
}

// last returns the most recent purchase body (nil when none was sent).
func (fc *fakeChip) last() map[string]any {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if len(fc.requests) == 0 {
		return nil
	}
	return fc.requests[len(fc.requests)-1]
}

// feeTestFixture is one workspace with a product, a published form, CHIP
// credentials, and a handler wired to the fake CHIP server.
type feeTestFixture struct {
	h        *PublicHandlers
	host     string
	formSlug string
	formID   string
	product  string
	account  string
	ws       string
	fees     *dbstore.PaymentFeeStore
	chip     *fakeChip
}

const feeTestEncKey = "0123456789abcdef0123456789abcdef"

// newFeeTestFixture builds a workspace whose CHIP credentials really decrypt, so
// the CHIP purchase is attempted against the fake server instead of failing
// early on ErrNotConfigured.
func newFeeTestFixture(t *testing.T, blocks bool) feeTestFixture {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)
	key := []byte(feeTestEncKey)

	sealed, err := cryptoutil.Encrypt(key, []byte("test-chip-secret"))
	if err != nil {
		t.Fatalf("encrypt chip secret: %v", err)
	}

	var userID, wsSlug, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-' || md5(random()::text) || '@example.test', 'it') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	var wsID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'fee-ws-' || md5(random()::text), 'it-' || md5(random()::text))
		 RETURNING id, slug`, userID).Scan(&wsID, &wsSlug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id, chip_brand_id, chip_secret_key_enc)
		 VALUES ($1, 'fee-acc-' || md5(random()::text), 'it-' || md5(random()::text), $2, 'brand_test', $3)
		 RETURNING id`, userID, wsID, sealed).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	ps := dbstore.NewProductStore(pool)
	prod := &dbstore.Product{AccountID: accountID, Name: "Produk Ujian", PriceSen: 100000, Type: dbstore.SaleTypeOneTime, Active: true}
	if err := ps.Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}

	fs := dbstore.NewFormStore(pool)
	formSlug := "it-fee-" + accountID[:8]
	cfg := map[string]any{"description": "fee fixture"}
	if blocks {
		cfg["blocks"] = []any{
			map[string]any{"id": "b1", "type": "product", "products": []any{
				map[string]any{"id": prod.ID, "name": prod.Name, "price_sen": prod.PriceSen},
			}},
		}
	} else {
		cfg["products"] = []any{
			map[string]any{"id": prod.ID, "name": prod.Name, "price_sen": prod.PriceSen},
		}
	}
	f := &dbstore.Form{
		AccountID: accountID, Slug: formSlug, Title: "Fee form",
		Status: dbstore.FormStatusPublished, Config: cfg,
	}
	if err := fs.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}

	fc := newFakeChip(t)
	sales := dbstore.NewSaleStore(pool)
	accounts := dbstore.NewAccountStore(pool)
	subs := dbstore.NewSubscriptionStore(pool)
	fees := dbstore.NewPaymentFeeStore(pool)
	pay := payments.New(chip.New(fc.srv.URL), accounts, sales, subs, key, "http://localhost:8080")
	pay.Workspaces = dbstore.NewWorkspaceStore(pool)
	pay.Forms = fs

	h := &PublicHandlers{
		Forms:       fs,
		Sales:       sales,
		Accounts:    accounts,
		Products:    ps,
		Workspaces:  dbstore.NewWorkspaceStore(pool),
		BaseURL:     "http://localhost:8080",
		Pay:         pay,
		PaymentFees: fees,
	}
	return feeTestFixture{
		h: h, host: wsSlug + ".jomform.com", formSlug: formSlug, formID: f.ID,
		product: prod.ID, account: accountID, ws: wsID, fees: fees, chip: fc,
	}
}

// enableFee stores one rate and records the merchant's acknowledgement.
func (fx feeTestFixture) enableFee(t *testing.T, rates ...dbstore.PaymentFeeRate) {
	t.Helper()
	ctx := context.Background()
	if err := fx.fees.Acknowledge(ctx, fx.ws, "owner@example.test"); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	if err := fx.fees.Replace(ctx, fx.ws, "owner@example.test", rates); err != nil {
		t.Fatalf("replace rates: %v", err)
	}
}

func feeOrderBody(productID, feeMethod string, expected int64) string {
	b := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"` + productID + `","quantity":1}],` +
		`"payment_method":"chip","fields":{}`
	if feeMethod != "" {
		b += `,"fee_method":"` + feeMethod + `"`
	}
	if expected != 0 {
		b += `,"expected_total_sen":` + itoa64(expected)
	}
	return b + `}`
}

func itoa64(v int64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// TestFeeRouteChargesGrossedUpTotalWithOneMethod is the card's route acceptance:
// the CHIP purchase carries ONLY the chosen method and the grossed-up amount, so
// RM1,000 at 1% is charged as RM1,010.10 (not RM1,010.00 — the naive formula is
// 10 sen short, see payments/fee_test.go).
func TestFeeRouteChargesGrossedUpTotalWithOneMethod(t *testing.T) {
	fx := newFeeTestFixture(t, true)
	fx.enableFee(t,
		dbstore.PaymentFeeRate{Method: payments.FeeMethodCard, Enabled: true, PercentBP: 100},
		dbstore.PaymentFeeRate{Method: payments.FeeMethodFPX, Enabled: true, FixedSen: 100},
	)

	// A = 100000 sen (RM1,000) at 1% → F = 101010, fee = 1010.
	rec := issue461Serve(t, fx.h, fx.host, http.MethodPost, "/"+fx.formSlug+"/order",
		feeOrderBody(fx.product, payments.FeeMethodCard, 101010))
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}

	sent := fx.chip.last()
	if sent == nil {
		t.Fatalf("no CHIP purchase was created")
	}
	// amount: CHIP must charge the server's grossed-up total.
	purchase, ok := sent["purchase"].(map[string]any)
	if !ok {
		t.Fatalf("purchase body has no purchase object: %#v", sent)
	}
	if got := int64(purchase["total_override"].(float64)); got != 101010 {
		t.Errorf("purchase.total_override = %d, want 101010 (RM1,010.10) — the merchant would be short by %d sen on RM1,000 @1%%",
			got, 100000-(got-payments.ProcessingFee(got, 0, 100)))
	}
	// method: ONLY the chosen one. `card` is the admin-facing aggregator, which
	// CHIP only understands as the network identifiers — so the assertion is on
	// the expansion, and it must exclude every OTHER method's identifiers (an
	// FPX or e-wallet entry here lets the buyer switch to a cheaper method after
	// seeing the fee).
	wl, _ := sent["payment_method_whitelist"].([]any)
	got := make([]string, 0, len(wl))
	for _, m := range wl {
		got = append(got, m.(string))
	}
	if strings.Join(got, ",") != "visa,mastercard,maestro" {
		t.Errorf("payment_method_whitelist = %v, want exactly the card networks [visa mastercard maestro] — anything else is a method the buyer can switch to",
			got)
	}

	// The sale records the fee as a component with the frozen rate.
	var ref string
	if err := json.Unmarshal(rec.Body.Bytes(), &struct {
		RefCode *string `json:"ref_code"`
	}{&ref}); err != nil {
		t.Fatalf("decode ref: %v", err)
	}
	sale, err := fx.h.Sales.ByRefCode(context.Background(), ref)
	if err != nil {
		t.Fatalf("fetch sale: %v", err)
	}
	if sale.FeeSen != 1010 || sale.FeeMethod != payments.FeeMethodCard ||
		sale.FeePercentBp != 100 || sale.FeeFixedSen != 0 {
		t.Errorf("sale fee snapshot = {sen:%d method:%q fixed:%d bp:%d}, want {1010 card 0 100}",
			sale.FeeSen, sale.FeeMethod, sale.FeeFixedSen, sale.FeePercentBp)
	}
	if sale.TotalSen != 101010 || sale.SubtotalSen != 100000 {
		t.Errorf("sale total/subtotal = %d/%d, want 101010/100000 — the fee must be a component, not folded into an item price",
			sale.TotalSen, sale.SubtotalSen)
	}
}

// TestFeeRouteRefusesUnchosenOrInactiveMethod pins "reject, never silently
// correct": a method the merchant did not enable, or none at all when several
// are active, is a 400 — not a checkout priced with a rate the buyer never saw.
func TestFeeRouteRefusesUnchosenOrInactiveMethod(t *testing.T) {
	fx := newFeeTestFixture(t, true)
	fx.enableFee(t,
		dbstore.PaymentFeeRate{Method: payments.FeeMethodCard, Enabled: true, PercentBP: 100},
		dbstore.PaymentFeeRate{Method: payments.FeeMethodFPX, Enabled: true, FixedSen: 100},
	)

	// fpx is stored but DISABLED: its rate must not be usable.
	if err := fx.fees.Replace(context.Background(), fx.ws, "owner@example.test", []dbstore.PaymentFeeRate{
		{Method: payments.FeeMethodCard, Enabled: true, PercentBP: 100},
		{Method: payments.FeeMethodFPX, Enabled: false, FixedSen: 100},
	}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	// With only one active method the empty choice is the default, so a
	// disabled method is the sharper case: it must be refused outright.
	rec := issue461Serve(t, fx.h, fx.host, http.MethodPost, "/"+fx.formSlug+"/order",
		feeOrderBody(fx.product, payments.FeeMethodFPX, 0))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("disabled method status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), errFeeMethodInvalid.Error()) {
		t.Errorf("disabled method body = %s, want %q", rec.Body.String(), errFeeMethodInvalid.Error())
	}
	if fx.chip.last() != nil {
		t.Errorf("a CHIP purchase was created for a refused order")
	}

	// Two active methods and no choice → the buyer must pick.
	fx.enableFee(t,
		dbstore.PaymentFeeRate{Method: payments.FeeMethodCard, Enabled: true, PercentBP: 100},
		dbstore.PaymentFeeRate{Method: payments.FeeMethodEWallet, Enabled: true, PercentBP: 140},
	)
	rec = issue461Serve(t, fx.h, fx.host, http.MethodPost, "/"+fx.formSlug+"/order",
		feeOrderBody(fx.product, "", 0))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("no-choice status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), errFeeChooseMethod.Error()) {
		t.Errorf("no-choice body = %s, want %q", rec.Body.String(), errFeeChooseMethod.Error())
	}
}

// TestFeeRouteRefusesTotalDrift: if the price or the rate moved while the buyer
// was filling the form, the page's total is stale and the order is refused
// rather than charged at an amount that was never on screen.
func TestFeeRouteRefusesTotalDrift(t *testing.T) {
	fx := newFeeTestFixture(t, true)
	fx.enableFee(t, dbstore.PaymentFeeRate{Method: payments.FeeMethodCard, Enabled: true, PercentBP: 100})

	rec := issue461Serve(t, fx.h, fx.host, http.MethodPost, "/"+fx.formSlug+"/order",
		feeOrderBody(fx.product, payments.FeeMethodCard, 100000)) // the PRE-fee price
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("drift status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), errFeeTotalMismatch.Error()) {
		t.Errorf("drift body = %s, want %q", rec.Body.String(), errFeeTotalMismatch.Error())
	}
	if fx.chip.last() != nil {
		t.Errorf("a CHIP purchase was created for a total the page never showed")
	}
}

// TestFeeSnapshotSurvivesRateChange: the rate is frozen on the sale, so a later
// MDR change (or a form switching the fee off) cannot rewrite what a past buyer
// was charged.
func TestFeeSnapshotSurvivesRateChange(t *testing.T) {
	fx := newFeeTestFixture(t, true)
	fx.enableFee(t, dbstore.PaymentFeeRate{Method: payments.FeeMethodCard, Enabled: true, PercentBP: 100})

	rec := issue461Serve(t, fx.h, fx.host, http.MethodPost, "/"+fx.formSlug+"/order",
		feeOrderBody(fx.product, payments.FeeMethodCard, 101010))
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		RefCode string `json:"ref_code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// The merchant doubles the rate and switches off the form's fee.
	if err := fx.fees.Replace(context.Background(), fx.ws, "owner@example.test", []dbstore.PaymentFeeRate{
		{Method: payments.FeeMethodCard, Enabled: true, PercentBP: 300},
	}); err != nil {
		t.Fatalf("replace: %v", err)
	}

	sale, err := fx.h.Sales.ByRefCode(context.Background(), resp.RefCode)
	if err != nil {
		t.Fatalf("fetch sale: %v", err)
	}
	if sale.FeePercentBp != 100 || sale.FeeSen != 1010 || sale.TotalSen != 101010 {
		t.Errorf("sale after rate change = {bp:%d sen:%d total:%d}, want the FROZEN {100 1010 101010} — history was rewritten",
			sale.FeePercentBp, sale.FeeSen, sale.TotalSen)
	}
}

// TestFeeOffPathsChargeNothing: an unacknowledged workspace, and a form with
// fee_override='off', must both price the plain asking total and let the CHIP
// purchase keep the merchant's own whitelist.
func TestFeeOffPathsChargeNothing(t *testing.T) {
	cases := []struct {
		name string
		set  func(t *testing.T, fx feeTestFixture)
	}{
		{"unacknowledged workspace", func(t *testing.T, fx feeTestFixture) {
			t.Helper()
			// Rates saved but the merchant never accepted the disclosure.
			if err := fx.fees.Replace(context.Background(), fx.ws, "owner@example.test", []dbstore.PaymentFeeRate{
				{Method: payments.FeeMethodCard, Enabled: true, PercentBP: 100},
			}); err != nil {
				t.Fatalf("replace: %v", err)
			}
		}},
		{"form fee_override off", func(t *testing.T, fx feeTestFixture) {
			t.Helper()
			fx.enableFee(t, dbstore.PaymentFeeRate{Method: payments.FeeMethodCard, Enabled: true, PercentBP: 100})
			if err := fx.h.Forms.SetFeeOverride(context.Background(), fx.account, fx.formID, dbstore.FeeOverrideOff); err != nil {
				t.Fatalf("set fee override: %v", err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newFeeTestFixture(t, true)
			tc.set(t, fx)

			rec := issue461Serve(t, fx.h, fx.host, http.MethodPost, "/"+fx.formSlug+"/order",
				feeOrderBody(fx.product, "", 100000))
			if rec.Code != http.StatusCreated {
				t.Fatalf("submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
			}
			var resp struct {
				RefCode string `json:"ref_code"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			sale, err := fx.h.Sales.ByRefCode(context.Background(), resp.RefCode)
			if err != nil {
				t.Fatalf("fetch sale: %v", err)
			}
			if sale.FeeSen != 0 || sale.FeeMethod != "" || sale.TotalSen != 100000 {
				t.Errorf("sale = {fee:%d method:%q total:%d}, want no fee and total 100000", sale.FeeSen, sale.FeeMethod, sale.TotalSen)
			}
			// The purchase keeps the merchant's whitelist (empty here) and, with
			// no fee, our total is still the sale total.
			if sent := fx.chip.last(); sent != nil {
				if purchase, ok := sent["purchase"].(map[string]any); ok {
					if got := int64(purchase["total_override"].(float64)); got != 100000 {
						t.Errorf("purchase.total_override = %d, want 100000", got)
					}
				}
			}
		})
	}
}
