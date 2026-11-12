package httpapi

// Card t_c7e193bb — the reconciliation this assertion protects.
//
// #1710 (card t_1d0198e3) landed "FPX B2C and FPX B2B1 are two buyer choices", so
// the picker builds one row per CHIP identifier. #1711 adds a PER-IDENTIFIER
// platform amount. The two models touch at exactly this point, and the failure
// they can produce together is silent: both FPX rows look right, both are shown,
// but the corporate row prices the buyer with the personal amount (or with no
// amount at all) because the charge was looked up by a "group" key instead of by
// the identifier the row stands for.
//
// So this asserts, on the REAL rendered page and the REAL CHIP request body:
//
//  1. two FPX rows are rendered, keyed `fpx` and `fpx_b2b1`;
//  2. the `fpx_b2b1` row carries the B2B1 platform amount (200) and the `fpx` row
//     the B2C one (100) — neither inherits the other, from the page's own data
//     attributes;
//  3. choosing the corporate row puts a 200-sen "Platform fee" line in the CHIP
//     purchase and pins the purchase to `fpx_b2b1`;
//  4. the sale records the amount and the identifier that were actually shown.

import (
	"context"
	"net/http"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/payments"
)

// TestB2B1PickerRowCarriesItsOwnPlatformAmount is the cross-model assertion: with
// #1710's two-choice picker in place, an `fpx_b2b1` row still carries its own
// platform amount, not the B2C one.
func TestB2B1PickerRowCarriesItsOwnPlatformAmount(t *testing.T) {
	ctx := context.Background()
	fx := newPickFixtureWithAvailability(t, func() map[string]any {
		return map[string]any{"id": "pur_plat_b2b1", "status": "created",
			"checkout_url": "https://pay.example.test/plat_b2b1", "total": 100200, "currency": "MYR"}
	}, bothFPX)
	fx.setWhitelist(t, "fpx", "fpx_b2b1")
	// A fee is only charged once the merchant accepted the pass-on disclosure —
	// the same two steps the Settings page performs.
	if err := fx.fees.Acknowledge(ctx, fx.ws, "test@example.test"); err != nil {
		t.Fatalf("acknowledge the pass-on disclosure: %v", err)
	}
	if err := fx.fees.Replace(ctx, fx.ws, "test@example.test", []dbstore.PaymentFeeRate{
		{Method: payments.FeeMethodFPX, Enabled: true, PlatformFeeSen: 100},
		{Method: payments.FeeMethodFPXB2B1, Enabled: true, PlatformFeeSen: 200},
	}); err != nil {
		t.Fatalf("store the two FPX platform amounts: %v", err)
	}

	// 1 + 2: the page's own numbers, per row.
	rows := fx.renderPicker(t)
	byValue := map[string]map[string]string{}
	for _, r := range rows {
		byValue[r["value"]] = r
	}
	if _, ok := byValue[payments.FeeMethodFPX]; !ok {
		t.Fatalf("picker rows = %+v, want an fpx (B2C) row", rows)
	}
	if _, ok := byValue[payments.FeeMethodFPXB2B1]; !ok {
		t.Fatalf("picker rows = %+v, want an fpx_b2b1 (B2B1) row", rows)
	}
	if got := byValue[payments.FeeMethodFPX]["data-platform"]; got != "100" {
		t.Errorf("FPX B2C row data-platform = %q, want 100 (its own RM1.00)", got)
	}
	if got := byValue[payments.FeeMethodFPXB2B1]["data-platform"]; got != "200" {
		t.Errorf("FPX B2B1 row data-platform = %q, want 200 (its own RM2.00, not the B2C 100)", got)
	}
	// The legacy attributes must not smuggle the other amount onto the row: the
	// page prices the chosen row from data-platform and falls back to
	// data-fixed/data-bp for a legacy rate.
	if got := byValue[payments.FeeMethodFPXB2B1]["data-fixed"]; got != "0" {
		t.Errorf("FPX B2B1 row data-fixed = %q, want 0 — a platform amount is not a legacy fixed rate", got)
	}

	// 3: the charge and the pin, on the request CHIP actually received.
	rec := fx.order(t, payments.FeeMethodFPXB2B1)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	sent := fx.chip.last()
	if sent == nil {
		t.Fatalf("no CHIP purchase was created")
	}
	lines := purchaseLines(t, sent)
	if len(lines) != 2 || lines[1].Name != payments.PlatformFeeLineName || lines[1].Price != 200 {
		t.Fatalf("purchase.products = %#v, want the item plus a 200-sen %q line for the B2B1 choice",
			lines, payments.PlatformFeeLineName)
	}
	if lines[0].Price != 100000 {
		t.Errorf("item line = %d, want the buyer's 100000 untouched by the platform fee", lines[0].Price)
	}
	if got := sentWhitelist(t, sent); len(got) != 1 || got[0] != payments.FeeMethodFPXB2B1 {
		t.Errorf("payment_method_whitelist = %v, want exactly [fpx_b2b1]", got)
	}

	// 4: the sale snapshot agrees with what the buyer was shown.
	var platformFee, total int64
	var identifier, model string
	if err := bundleTestPool(t).QueryRow(ctx,
		`SELECT platform_fee_sen, fee_identifier, fee_model, total_sen FROM sales
		 WHERE account_id=$1 ORDER BY created_at DESC LIMIT 1`, fx.account).
		Scan(&platformFee, &identifier, &model, &total); err != nil {
		t.Fatalf("read the sale: %v", err)
	}
	if platformFee != 200 || total != 100200 {
		t.Errorf("sale platform fee/total = %d/%d, want 200/100200 (the B2B1 amount)", platformFee, total)
	}
	if identifier != payments.FeeMethodFPXB2B1 || model != dbstore.FeeModelPlatform {
		t.Errorf("sale identifier/model = %q/%q, want fpx_b2b1/platform", identifier, model)
	}
}
