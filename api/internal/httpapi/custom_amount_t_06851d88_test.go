package httpapi

// Card t_06851d88 — pay-what-you-want (custom_amount) products.
//
// The buyer's typed amount is a CLAIM. These tests are written against the HTTP
// surface (not the helper functions) because that is the boundary the claim
// crosses: every case below posts what a hand-made request could post and
// asserts what the server charged, refused, or recorded.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/payments"
)

type customAmountFixture struct {
	fx      feeTestFixture
	slug    string
	product *dbstore.Product
}

// newCustomAmountFixture builds one published form holding one custom_amount
// product (min RM5.00, max RM500.00, suggested RM10.00). It reuses the fee
// fixture's workspace/account so the order path is identical to a real form's.
func newCustomAmountFixture(t *testing.T) customAmountFixture {
	t.Helper()
	fx := newFeeTestFixture(t, true)
	ctx := context.Background()

	minSen, maxSen, suggested := int64(500), int64(50000), int64(1000)
	prod := &dbstore.Product{
		AccountID: fx.account, Name: "Derma Masjid", PriceSen: suggested,
		Type: dbstore.SaleTypeCustomAmount, Active: true,
		MinPriceSen: minSen, MaxPriceSen: &maxSen, SuggestedPriceSen: &suggested,
	}
	if err := fx.h.Products.Create(ctx, prod); err != nil {
		t.Fatalf("create custom_amount product: %v", err)
	}

	slug := "it-ca-" + fx.account[:8]
	cfg := map[string]any{
		"lang": "en",
		"blocks": []any{
			map[string]any{"id": "b1", "type": "product", "value": prod.Name, "product_id": prod.ID},
		},
		"products": []any{
			map[string]any{"id": prod.ID, "name": prod.Name, "price_sen": prod.PriceSen},
		},
	}
	f := &dbstore.Form{
		AccountID: fx.account, Slug: slug, Title: "Derma",
		Status: dbstore.FormStatusPublished, Config: cfg,
	}
	if err := fx.h.Forms.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}
	return customAmountFixture{fx: fx, slug: slug, product: prod}
}

// orderBody posts what the rendered page posts. amount is raw JSON so the tests
// can send 100.5 and -100, not only integers.
func (cf customAmountFixture) orderBody(amount string, qty int64) string {
	b := `{"client":{"full_name":"Penderma","email":"derma@example.test"},` +
		`"items":[{"product_id":"` + cf.product.ID + `","quantity":` + strconv.FormatInt(qty, 10)
	if amount != "" {
		b += `,"amount_sen":` + amount
	}
	return b + `}],"payment_method":"chip","fields":{}}`
}

func (cf customAmountFixture) post(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	return issue461Serve(t, cf.fx.h, cf.fx.host, http.MethodPost, "/"+cf.slug+"/order", body)
}

// TestCustomAmountPageRendersTheAmountInput pins the buyer UI wiring: the page
// must show a number input bound to the product, seeded from the suggestion,
// bounded by the product's limits, and the inline script must reprice on input
// (without a reload) and submit the typed amount.
func TestCustomAmountPageRendersTheAmountInput(t *testing.T) {
	cf := newCustomAmountFixture(t)
	rec := issue461Serve(t, cf.fx.h, cf.fx.host, http.MethodGet, "/"+cf.slug, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("render status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	for _, want := range []string{
		`class="pa"`,
		`data-id="` + cf.product.ID + `"`,
		`min="5.00"`,
		`max="500.00"`,
		`value="10.00"`,
		`step="0.01"`,
		// The repaint-on-type wiring: the amount inputs are collected, each one
		// repaints the fee/total on input, and the typed amount is sent as the
		// line price. Asserted as the exact assignment (not just the listener
		// idiom) — the quantity input uses the same idiom, so a looser string
		// would pass even with the amount wiring deleted.
		`var amounts=document.querySelectorAll('input.pa')`,
		`amounts.forEach(function(el){el.addEventListener('input',upd)});`,
		`item.amount_sen=amt`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered page is missing %q — the amount input or its repricing wiring is not wired", want)
		}
	}
	// A custom product has no quantity selector: the typed amount IS the gift.
	if strings.Contains(body, `class="pqrow"`) {
		t.Error("rendered page still shows a quantity selector for a custom_amount product")
	}
}

// TestCustomAmountSubmitIsPricedByTheServer covers the happy path and the two
// things the client cannot dictate: the line price is the typed amount, and the
// quantity is 1 no matter what was sent.
func TestCustomAmountSubmitIsPricedByTheServer(t *testing.T) {
	cf := newCustomAmountFixture(t)
	rec := issue461Serve(t, cf.fx.h, cf.fx.host, http.MethodPost, "/"+cf.slug+"/order",
		cf.orderBody("12345", 9)) // 9 is a claim the server must ignore
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 201/200: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		RefCode string `json:"ref_code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	sale, err := cf.fx.h.Sales.ByRefCode(context.Background(), out.RefCode)
	if err != nil {
		t.Fatalf("reload sale: %v", err)
	}
	if sale.SubtotalSen != 12345 {
		t.Errorf("subtotal_sen = %d, want the typed 12345 (the product's price_sen is only a display fallback)", sale.SubtotalSen)
	}
	if len(sale.Items) != 1 || sale.Items[0].PriceSen != 12345 || sale.Items[0].Qty != 1 {
		t.Errorf("items = %+v, want one line at 12345 x 1", sale.Items)
	}
	if sale.SaleType != dbstore.SaleTypeCustomAmount {
		t.Errorf("sale_type = %q, want %q", sale.SaleType, dbstore.SaleTypeCustomAmount)
	}
}

// TestCustomAmountRefusals is the limit rule at the wire boundary. Every case
// must be a 400 carrying the English sentence AND the localized message — a
// silent fallback to the product price, or a 200 with the wrong total, is the
// bug this card exists to prevent.
func TestCustomAmountRefusals(t *testing.T) {
	cases := []struct {
		name     string
		amount   string
		wantCode string
		wantWord string
	}{
		{"missing amount", "", "order.amountRequired", "Please enter an amount"},
		{"below the product minimum", "499", "order.amountTooLow", "at least RM5.00"},
		{"zero", "0", "order.amountTooLow", ""},
		{"negative", "-100", "order.amountTooLow", ""},
		{"above the product maximum", "50001", "order.amountTooHigh", "must not exceed RM500.00"},
		{"not a whole sen", "100.5", "order.amountInvalid", "whole number of sen"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cf := newCustomAmountFixture(t) // fresh form: the anti-spam limiter is per form
			rec := cf.post(t, cf.orderBody(tc.amount, 1))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 — body: %s", rec.Code, rec.Body.String())
			}
			var got struct {
				Code    string `json:"code"`
				Error   string `json:"error"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode refusal: %v (%s)", err, rec.Body.String())
			}
			if got.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", got.Code, tc.wantCode)
			}
			if got.Error == "" {
				t.Error("refusal carries no English `error` sentence")
			}
			if got.Message == "" {
				t.Error("refusal carries no localized `message`")
			}
			if tc.wantWord != "" && !strings.Contains(got.Error, tc.wantWord) {
				t.Errorf("error = %q, want it to mention %q", got.Error, tc.wantWord)
			}
		})
	}
}

// TestCustomAmountDoesNotPinOtherLinesQuantity is the mixed-cart guard: the
// quantity pin belongs to the custom line ONLY. A donation plus three shirts
// must still charge for three shirts.
func TestCustomAmountDoesNotPinOtherLinesQuantity(t *testing.T) {
	cf := newCustomAmountFixture(t)
	ctx := context.Background()

	fixed := &dbstore.Product{
		AccountID: cf.fx.account, Name: "Baju", PriceSen: 2500,
		Type: dbstore.SaleTypeOneTime, Active: true,
	}
	if err := cf.fx.h.Products.Create(ctx, fixed); err != nil {
		t.Fatalf("create fixed product: %v", err)
	}

	body := `{"client":{"full_name":"Penderma","email":"derma@example.test"},"items":[` +
		`{"product_id":"` + cf.product.ID + `","quantity":1,"amount_sen":2000},` +
		`{"product_id":"` + fixed.ID + `","quantity":3}],` +
		`"payment_method":"chip","fields":{}}`
	rec := issue461Serve(t, cf.fx.h, cf.fx.host, http.MethodPost, "/"+cf.slug+"/order", body)
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 2xx: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		RefCode string `json:"ref_code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	sale, err := cf.fx.h.Sales.ByRefCode(ctx, out.RefCode)
	if err != nil {
		t.Fatalf("reload sale: %v", err)
	}
	// 2000 (the gift) + 3 x 2500 (the shirts) = 9500.
	if sale.SubtotalSen != 9500 {
		t.Errorf("subtotal_sen = %d, want 9500 — a custom line must not force the other lines to qty 1", sale.SubtotalSen)
	}
	var fixedQty int64
	for _, it := range sale.Items {
		if it.ProductID == fixed.ID {
			fixedQty = it.Qty
		}
	}
	if fixedQty != 3 {
		t.Errorf("fixed item qty = %d, want 3", fixedQty)
	}
}

// TestCustomAmountFeeAndSnapshotFreeze pins rule 3 and rule 4 together: the fee
// is grossed up from the TYPED amount (not the product price), and everything
// the sale needs is snapshotted, so changing the merchant's rate afterwards
// cannot rewrite an old gift or its receipt.
func TestCustomAmountFeeAndSnapshotFreeze(t *testing.T) {
	cf := newCustomAmountFixture(t)
	ctx := context.Background()

	// Buyer pays the 1% fee on a RM100.00 gift.
	const rateBP, fixedSen = int64(100), int64(0)
	cf.fx.enableFee(t, dbstore.PaymentFeeRate{Method: payments.FeeMethodCard, Enabled: true, FixedSen: fixedSen, PercentBP: rateBP})
	wantTotal, _ := payments.GrossUp(10000, fixedSen, rateBP)

	rec := issue461Serve(t, cf.fx.h, cf.fx.host, http.MethodPost, "/"+cf.slug+"/order",
		strings.Replace(cf.orderBody("10000", 1), `"payment_method":"chip"`,
			`"payment_method":"chip","fee_method":"`+payments.FeeMethodCard+`"`, 1))
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 201/200: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		RefCode string `json:"ref_code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	before, err := cf.fx.h.Sales.ByRefCode(ctx, out.RefCode)
	if err != nil {
		t.Fatalf("reload sale: %v", err)
	}
	if before.SubtotalSen != 10000 {
		t.Errorf("subtotal_sen = %d, want 10000 (the typed amount)", before.SubtotalSen)
	}
	if before.TotalSen != wantTotal {
		t.Errorf("total_sen = %d, want the grossed-up %d", before.TotalSen, wantTotal)
	}
	if before.FeeSen != wantTotal-10000 || before.FeePercentBp != rateBP {
		t.Errorf("fee_sen=%d fee_percent_bp=%d, want %d / %d",
			before.FeeSen, before.FeePercentBp, wantTotal-10000, rateBP)
	}
	if len(before.Items) != 1 || before.Items[0].PriceSen != 10000 {
		t.Errorf("snapshotted items = %+v, want one line at 10000", before.Items)
	}

	// The merchant changes the rate. The recorded gift must not move.
	cf.fx.enableFee(t, dbstore.PaymentFeeRate{Method: payments.FeeMethodCard, Enabled: true, FixedSen: 200, PercentBP: 300})
	after, err := cf.fx.h.Sales.ByRefCode(ctx, out.RefCode)
	if err != nil {
		t.Fatalf("reload sale after rate change: %v", err)
	}
	if after.SubtotalSen != before.SubtotalSen || after.TotalSen != before.TotalSen ||
		after.FeeSen != before.FeeSen || after.FeePercentBp != before.FeePercentBp ||
		after.FeeFixedSen != before.FeeFixedSen {
		t.Errorf("sale changed when the rate did:\n before %+v\n after  %+v", before, after)
	}
	if len(after.Items) != 1 || after.Items[0].PriceSen != 10000 {
		t.Errorf("snapshot items changed: %+v", after.Items)
	}
}
