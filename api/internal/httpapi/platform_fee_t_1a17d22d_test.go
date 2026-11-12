package httpapi

// Card t_1a17d22d — the platform fee as its own line in `purchase.products`.
//
// The buyer's typed amount must not change, the CHIP purchase must carry TWO
// product lines (the item, then "Platform fee"), and the charged total must be
// exactly item + fee. A workspace that stores a PERCENTAGE keeps the legacy
// gross-up for one release, so nothing is silently charged differently by this
// change (requirement 6) — that is asserted here too, at the route that does the
// pricing.
//
// The money is asserted against the recorded CHIP purchase body, not against a
// mock of our own code, so a mis-wired call site cannot pass.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/payments"
)

// platformOrderBody is feeOrderBody with a buyer-chosen METHOD (the pay picker
// posts pay_method, the legacy page posts fee_method).
func platformOrderBody(productID, payMethod string, expected int64) string {
	b := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"` + productID + `","quantity":1}],` +
		`"payment_method":"chip","fields":{}`
	if payMethod != "" {
		b += `,"pay_method":"` + payMethod + `"`
	}
	if expected != 0 {
		b += `,"expected_total_sen":` + itoa64(expected)
	}
	return b + `}`
}

// purchaseLines reads the products array the fake CHIP server received.
func purchaseLines(t *testing.T, sent map[string]any) []struct {
	Name  string
	Price int64
} {
	t.Helper()
	purchase, ok := sent["purchase"].(map[string]any)
	if !ok {
		t.Fatalf("purchase body has no purchase object: %#v", sent)
	}
	raw, _ := purchase["products"].([]any)
	out := make([]struct {
		Name  string
		Price int64
	}, 0, len(raw))
	for _, p := range raw {
		m, ok := p.(map[string]any)
		if !ok {
			t.Fatalf("product line is not an object: %#v", p)
		}
		name, _ := m["name"].(string)
		price, _ := m["price"].(float64)
		out = append(out, struct {
			Name  string
			Price int64
		}{name, int64(price)})
	}
	return out
}

// TestPlatformFeePurchaseCarriesTwoLinesAndLeavesTheBuyersAmount is the card's
// acceptance test: item 100000 + platform fee 100 → two product lines, total
// 100100, and the buyer's typed amount untouched.
func TestPlatformFeePurchaseCarriesTwoLinesAndLeavesTheBuyersAmount(t *testing.T) {
	fx := newFeeTestFixture(t, true)
	fx.enableFee(t, dbstore.PaymentFeeRate{
		Method: payments.FeeMethodFPX, Enabled: true, PlatformFeeSen: 100,
	})

	rec := issue461Serve(t, fx.h, fx.host, http.MethodPost, "/"+fx.formSlug+"/order",
		platformOrderBody(fx.product, payments.FeeMethodFPX, 100100))
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}

	sent := fx.chip.last()
	if sent == nil {
		t.Fatalf("no CHIP purchase was created")
	}
	lines := purchaseLines(t, sent)
	if len(lines) != 2 {
		t.Fatalf("purchase.products = %#v, want TWO lines (the item and the platform fee)", lines)
	}
	if lines[0].Name != "Produk Ujian" || lines[0].Price != 100000 {
		t.Errorf("line 0 = %q/%d, want the buyer's item at the price they typed (100000)", lines[0].Name, lines[0].Price)
	}
	if lines[1].Name != "Platform fee" || lines[1].Price != 100 {
		t.Errorf("line 1 = %q/%d, want \"Platform fee\"/100 — the charge is JomForm's own line, not a grossed-up total", lines[1].Name, lines[1].Price)
	}
	purchase, _ := sent["purchase"].(map[string]any)
	if got := int64(purchase["total_override"].(float64)); got != 100100 {
		t.Errorf("purchase.total_override = %d, want 100100 (item 100000 + platform fee 100)", got)
	}
	// The purchase is pinned to the identifier whose fee was charged.
	wl, _ := sent["payment_method_whitelist"].([]any)
	if len(wl) != 1 || wl[0].(string) != payments.FeeMethodFPX {
		t.Errorf("payment_method_whitelist = %v, want exactly [fpx]", wl)
	}

	// The sale snapshot: both amounts, the model, and the money invariant.
	var resp struct {
		RefCode string `json:"ref_code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode ref: %v", err)
	}
	sale, err := fx.h.Sales.ByRefCode(context.Background(), resp.RefCode)
	if err != nil {
		t.Fatalf("fetch sale: %v", err)
	}
	if sale.SubtotalSen != 100000 {
		t.Errorf("sale.subtotal_sen = %d, want the buyer's typed amount 100000 unchanged", sale.SubtotalSen)
	}
	if sale.TotalSen != 100100 || sale.FeeSen != 100 || sale.PlatformFeeSen != 100 {
		t.Errorf("sale = {total:%d fee:%d platform:%d}, want {100100 100 100}",
			sale.TotalSen, sale.FeeSen, sale.PlatformFeeSen)
	}
	if sale.FeeModel != dbstore.FeeModelPlatform || sale.FeeIdentifier != payments.FeeMethodFPX {
		t.Errorf("sale model/identifier = %q/%q, want platform/fpx", sale.FeeModel, sale.FeeIdentifier)
	}
}

// TestPlatformFeeFPXVariantsCarryDifferentAmounts: B2C and B2B1 are separate CHIP
// identifiers with their own amounts, and the purchase is pinned to the one the
// buyer chose.
func TestPlatformFeeFPXVariantsCarryDifferentAmounts(t *testing.T) {
	fx := newFeeTestFixture(t, true)
	fx.enableFee(t,
		dbstore.PaymentFeeRate{Method: payments.FeeMethodFPX, Enabled: true, PlatformFeeSen: 100},
		dbstore.PaymentFeeRate{Method: payments.FeeMethodFPXB2B1, Enabled: true, PlatformFeeSen: 200},
	)

	rec := issue461Serve(t, fx.h, fx.host, http.MethodPost, "/"+fx.formSlug+"/order",
		platformOrderBody(fx.product, payments.FeeMethodFPXB2B1, 100200))
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	lines := purchaseLines(t, fx.chip.last())
	if len(lines) != 2 || lines[1].Name != "Platform fee" || lines[1].Price != 200 {
		t.Fatalf("purchase.products = %#v, want the item plus a 200-sen platform fee for B2B1", lines)
	}
	sent := fx.chip.last()
	wl, _ := sent["payment_method_whitelist"].([]any)
	if len(wl) != 1 || wl[0].(string) != payments.FeeMethodFPXB2B1 {
		t.Errorf("payment_method_whitelist = %v, want exactly [fpx_b2b1] — the buyer chose the corporate variant", wl)
	}
}

// TestLegacyPercentageWorkspaceKeepsItsCharge is the migration rule at the route:
// a workspace that stores a PERCENTAGE is charged exactly what it was charged
// before this change (RM1,000 at 1% → RM1,010.10), records the legacy model, and
// reports no platform amount.
func TestLegacyPercentageWorkspaceKeepsItsCharge(t *testing.T) {
	fx := newFeeTestFixture(t, true)
	fx.enableFee(t, dbstore.PaymentFeeRate{
		Method: payments.FeeMethodCard, Enabled: true, PercentBP: 100,
	})

	rec := issue461Serve(t, fx.h, fx.host, http.MethodPost, "/"+fx.formSlug+"/order",
		platformOrderBody(fx.product, payments.FeeMethodCard, 101010))
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		RefCode string `json:"ref_code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode ref: %v", err)
	}
	sale, err := fx.h.Sales.ByRefCode(context.Background(), resp.RefCode)
	if err != nil {
		t.Fatalf("fetch sale: %v", err)
	}
	if sale.TotalSen != 101010 || sale.FeeSen != 1010 {
		t.Errorf("legacy sale total/fee = %d/%d, want 101010/1010 — the gross-up is unchanged for one release",
			sale.TotalSen, sale.FeeSen)
	}
	if sale.FeeModel != dbstore.FeeModelLegacyGrossUp || sale.PlatformFeeSen != 0 {
		t.Errorf("legacy sale model/platform = %q/%d, want legacy_grossup/0", sale.FeeModel, sale.PlatformFeeSen)
	}
	if sale.FeePercentBp != 100 {
		t.Errorf("legacy sale lost its frozen percentage: bp=%d, want 100", sale.FeePercentBp)
	}
	// The buyer's path still calls the line the platform fee, and the amount is
	// the grossed-up component (the frozen snapshot is what reports it).
	lines := purchaseLines(t, fx.chip.last())
	if len(lines) != 2 || lines[1].Name != "Platform fee" || lines[1].Price != 1010 {
		t.Errorf("legacy purchase.products = %#v, want the item plus a \"Platform fee\" line of 1010", lines)
	}
	for _, l := range lines {
		if strings.Contains(strings.ToLower(l.Name), "processing fee") {
			t.Errorf("buyer-facing product line still says %q — the old wording is gone from the buyer path", l.Name)
		}
	}
}

// TestFixedOnlyRateStopsMovingTheBuyersAmount: a FIXED-only row is the same
// arithmetic as a flat platform fee (gross-up of a constant leaves A + fixed), so
// it moves to the platform model — and the amount the buyer pays does not change.
func TestFixedOnlyRateStopsMovingTheBuyersAmount(t *testing.T) {
	fx := newFeeTestFixture(t, true)
	fx.enableFee(t, dbstore.PaymentFeeRate{
		Method: payments.FeeMethodEWallet, Enabled: true, FixedSen: 100,
	})

	rec := issue461Serve(t, fx.h, fx.host, http.MethodPost, "/"+fx.formSlug+"/order",
		platformOrderBody(fx.product, payments.FeeMethodEWallet, 100100))
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		RefCode string `json:"ref_code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode ref: %v", err)
	}
	sale, err := fx.h.Sales.ByRefCode(context.Background(), resp.RefCode)
	if err != nil {
		t.Fatalf("fetch sale: %v", err)
	}
	if sale.TotalSen != 100100 || sale.PlatformFeeSen != 100 || sale.FeeModel != dbstore.FeeModelPlatform {
		t.Errorf("fixed-only sale = {total:%d platform:%d model:%q}, want {100100 100 platform}",
			sale.TotalSen, sale.PlatformFeeSen, sale.FeeModel)
	}
	if sale.FeePercentBp != 0 {
		t.Errorf("fixed-only sale kept a percentage (%d): a flat amount is not a rate", sale.FeePercentBp)
	}
}

// TestPlatformFeeOnABankDirectOrderChargesNothing: the fee is a CHIP purchase
// line; a bank-direct order has no gateway page to carry it, so the buyer pays
// the plain total.
func TestPlatformFeeOnABankDirectOrderChargesNothing(t *testing.T) {
	fx := newFeeTestFixture(t, true)
	fx.enableFee(t, dbstore.PaymentFeeRate{
		Method: payments.FeeMethodFPX, Enabled: true, PlatformFeeSen: 100,
	})

	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"` + fx.product + `","quantity":1}],` +
		`"payment_method":"bank_direct","fields":{}}`
	rec := issue461Serve(t, fx.h, fx.host, http.MethodPost, "/"+fx.formSlug+"/order", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("bank-direct submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		RefCode string `json:"ref_code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode ref: %v", err)
	}
	sale, err := fx.h.Sales.ByRefCode(context.Background(), resp.RefCode)
	if err != nil {
		t.Fatalf("fetch sale: %v", err)
	}
	if sale.TotalSen != 100000 || sale.FeeSen != 0 || sale.PlatformFeeSen != 0 {
		t.Errorf("bank-direct sale = {total:%d fee:%d platform:%d}, want the plain 100000 with no charge",
			sale.TotalSen, sale.FeeSen, sale.PlatformFeeSen)
	}
}
