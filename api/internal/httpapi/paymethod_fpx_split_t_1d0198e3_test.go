package httpapi

// Card t_1d0198e3 — FPX B2C and FPX B2B1 are two buyer choices at checkout.
//
// WHAT IS PINNED HERE, on the REAL rendered page and the REAL CHIP request body:
//
//  1. the picker renders TWO FPX radios, each with its own value and its own
//     description — the buyer answers B2C-or-B2B1 on JomForm, once;
//  2. choosing B2B1 narrows the purchase to `fpx_b2b1` ALONE and lands on CHIP
//     with ?preferred=fpx_b2b1 (and choosing B2C does the mirror image), so CHIP
//     cannot offer the other product;
//  3. a row is offered only when CHIP's /payment_methods/ lists it: a brand
//     enabled for B2C only renders no B2B1 row, and posting B2B1 anyway is a 400
//     with no purchase created;
//  4. a listing CHIP could not answer does NOT hide rows (a CHIP outage must not
//     empty the picker);
//  5. an existing whitelist of ["fpx"] keeps working and stays FPX B2C — it never
//     silently becomes B2B1 (the migration rule);
//  6. the two rows can carry DIFFERENT fee rates (RM1 vs RM2), and the fee stored
//     on the sale follows the row the buyer picked.
//
// BITE PROOF for the group mapping: fold `fpx_b2b1` back into the FPX group in
// api/internal/payments/paymethod.go and tests 1, 2 and 6 go red (one FPX row, a
// whitelist holding both identifiers, and one shared fee line).

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/payments"
)

// payMethodRadios returns the (value, label, description) of every picker row on
// a rendered page, in document order. The picker's own radio markup is
// `<input type="radio" name="pay_method" class="paym" value="…" … data-name="…"
// data-fixed="…" data-bp="…">` followed by the label/description spans
// (render_blocks.go / menu.go), so the match is anchored on the name attribute
// rather than on the value — the page has other `value="fpx"`-looking text.
func payMethodRadios(t *testing.T, body string) []map[string]string {
	t.Helper()
	re := regexp.MustCompile(`(?s)<input type="radio" name="pay_method"[^>]*>`)
	var out []map[string]string
	for _, input := range re.FindAllString(body, -1) {
		row := map[string]string{"html": input}
		for _, attr := range []string{"value", "data-name", "data-platform", "data-fixed", "data-bp"} {
			if m := regexp.MustCompile(attr + `="([^"]*)"`).FindStringSubmatch(input); m != nil {
				row[attr] = m[1]
			}
		}
		out = append(out, row)
	}
	return out
}

// renderPicker renders the fixture form's public page and returns the picker rows.
func (fx pickFixture) renderPicker(t *testing.T) []map[string]string {
	t.Helper()
	rec := issue461Serve(t, fx.h, fx.host, http.MethodGet, "/"+fx.formSlug, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("render status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	return payMethodRadios(t, rec.Body.String())
}

// bothFPX is a CHIP listing that carries both FPX products.
var bothFPX = []string{"fpx", "fpx_b2b1"}

// TestPickerRendersFPXAsTwoRows: a buyer sees "FPX" and "FPX B2B1" as separate
// choices, each with its own description, on the page they actually get.
func TestPickerRendersFPXAsTwoRows(t *testing.T) {
	fx := newPickFixtureWithAvailability(t, func() map[string]any {
		return map[string]any{"id": "pur_two", "status": "created",
			"checkout_url": "https://pay.example.test/two", "total": 100000, "currency": "MYR"}
	}, bothFPX)
	fx.setWhitelist(t, "fpx", "fpx_b2b1")

	rows := fx.renderPicker(t)
	var values []string
	for _, r := range rows {
		values = append(values, r["value"])
	}
	if strings.Join(values, ",") != "fpx,fpx_b2b1" {
		t.Fatalf("picker rows = %v, want exactly [fpx fpx_b2b1] (two FPX products, B2C then B2B1)", values)
	}
	if rows[0]["data-name"] != "FPX" {
		t.Fatalf("first row is labelled %q, want FPX", rows[0]["data-name"])
	}
	if rows[1]["data-name"] != "FPX B2B1" {
		t.Fatalf("second row is labelled %q, want FPX B2B1", rows[1]["data-name"])
	}
	// Each row EXPLAINS which product it is: two FPX rows with the same sentence
	// would be a picker a buyer cannot answer (asserted on the served page in
	// TestFPXRowLabelsAndDescriptionsReachThePage).
	if !strings.Contains(rows[1]["html"], "fpx_b2b1") {
		t.Fatalf("second row posts %q, want fpx_b2b1", rows[1]["html"])
	}
}

// TestB2B1ChoiceNarrowsToB2B1Alone: choosing the corporate row sends CHIP a
// whitelist holding only `fpx_b2b1` and pre-selects it on the checkout URL.
func TestB2B1ChoiceNarrowsToB2B1Alone(t *testing.T) {
	fx := newPickFixtureWithAvailability(t, func() map[string]any {
		return map[string]any{"id": "pur_b2b1", "status": "created",
			"checkout_url": "https://pay.example.test/pur_b2b1?lang=en", "total": 100000, "currency": "MYR"}
	}, bothFPX)
	fx.setWhitelist(t, "fpx", "fpx_b2b1")

	rec := fx.order(t, payments.PayMethodFPXB2B1)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if got := strings.Join(sentWhitelist(t, fx.chip.last()), ","); got != "fpx_b2b1" {
		t.Fatalf("payment_method_whitelist = %q, want only fpx_b2b1 — anything else lets CHIP offer the other FPX product", got)
	}
	want := "https://pay.example.test/pur_b2b1?lang=en&preferred=fpx_b2b1"
	if got := checkoutURL(t, rec); got != want {
		t.Fatalf("checkout_url = %q, want %q", got, want)
	}
}

// TestB2CChoiceNarrowsToB2CAlone is the mirror image, so "the buyer picks one"
// is true in both directions.
func TestB2CChoiceNarrowsToB2CAlone(t *testing.T) {
	fx := newPickFixtureWithAvailability(t, func() map[string]any {
		return map[string]any{"id": "pur_b2c", "status": "created",
			"checkout_url": "https://pay.example.test/pur_b2c", "total": 100000, "currency": "MYR"}
	}, bothFPX)
	fx.setWhitelist(t, "fpx", "fpx_b2b1")

	rec := fx.order(t, payments.PayMethodFPX)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if got := strings.Join(sentWhitelist(t, fx.chip.last()), ","); got != "fpx" {
		t.Fatalf("payment_method_whitelist = %q, want only fpx", got)
	}
	if got := checkoutURL(t, rec); got != "https://pay.example.test/pur_b2c?preferred=fpx" {
		t.Fatalf("checkout_url = %q, want ?preferred=fpx", got)
	}
}

// TestPickerHidesB2B1WhenChipDoesNotListIt: requirement 3 — a brand enabled for
// B2C only must not see a B2B1 row, even when the workspace's own whitelist holds
// both and even though the page render has no cart amount.
func TestPickerHidesB2B1WhenChipDoesNotListIt(t *testing.T) {
	fx := newPickFixtureWithAvailability(t, func() map[string]any {
		return map[string]any{"id": "pur_b2c_only", "status": "created",
			"checkout_url": "https://pay.example.test/b2c_only", "total": 100000, "currency": "MYR"}
	}, []string{"fpx"})
	fx.setWhitelist(t, "fpx", "fpx_b2b1")

	rows := fx.renderPicker(t)
	if len(rows) != 1 || rows[0]["value"] != "fpx" {
		t.Fatalf("picker rows = %+v, want only the FPX B2C row — CHIP does not list fpx_b2b1 for this brand", rows)
	}
	// The listing was really asked for, with CHIP's recommended probe amount
	// (RM10) rather than nothing: a missing amount hides FPX from the response.
	calls, amounts := fx.chip.listCalls()
	if calls == 0 {
		t.Fatalf("the picker never asked CHIP for its method list")
	}
	if amounts[0] != "1000" {
		t.Fatalf("listing probe amount = %q, want 1000 (RM10, CHIP's recommended probe)", amounts[0])
	}

	// And the door is closed too: a crafted request for the hidden row is refused
	// before any purchase exists.
	rec := fx.order(t, payments.PayMethodFPXB2B1)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("posting b2b1 status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if n := fx.chip.count(); n != 0 {
		t.Fatalf("%d CHIP purchase(s) created for a product the brand does not list", n)
	}
}

// TestPickerKeepsBothRowsWhenChipCannotBeAsked: an outage is not evidence. The
// picker falls back to the whitelist rather than emptying the buyer's choices.
func TestPickerKeepsBothRowsWhenChipCannotBeAsked(t *testing.T) {
	fx := newPickFixture(t, func() map[string]any {
		return map[string]any{"id": "pur_outage", "status": "created",
			"checkout_url": "https://pay.example.test/outage", "total": 100000, "currency": "MYR"}
	})
	fx.setWhitelist(t, "fpx", "fpx_b2b1")

	rows := fx.renderPicker(t)
	var values []string
	for _, r := range rows {
		values = append(values, r["value"])
	}
	if strings.Join(values, ",") != "fpx,fpx_b2b1" {
		t.Fatalf("picker rows on a CHIP failure = %v, want both FPX rows (fail open)", values)
	}
}

// TestPickerAsksChipOncePerWindow: the listing is cached, so a page render does
// not pay a CHIP round trip for every visitor (and a slow CHIP cannot slow every
// checkout).
func TestPickerAsksChipOncePerWindow(t *testing.T) {
	fx := newPickFixtureWithAvailability(t, func() map[string]any {
		return map[string]any{"id": "pur_cache", "status": "created",
			"checkout_url": "https://pay.example.test/cache", "total": 100000, "currency": "MYR"}
	}, bothFPX)
	fx.setWhitelist(t, "fpx", "fpx_b2b1")

	fx.renderPicker(t)
	fx.renderPicker(t)
	if calls, _ := fx.chip.listCalls(); calls != 1 {
		t.Fatalf("CHIP /payment_methods/ was called %d times for two renders, want 1 (cached)", calls)
	}
}

// TestExistingFPXWhitelistStaysB2COnly is the migration rule: a workspace whose
// stored whitelist is ["fpx"] keeps working, shows the personal product, and does
// NOT silently gain (or become) FPX B2B1.
func TestExistingFPXWhitelistStaysB2COnly(t *testing.T) {
	fx := newPickFixtureWithAvailability(t, func() map[string]any {
		return map[string]any{"id": "pur_legacy", "status": "created",
			"checkout_url": "https://pay.example.test/legacy", "total": 100000, "currency": "MYR"}
	}, bothFPX)
	fx.setWhitelist(t, "fpx")

	rows := fx.renderPicker(t)
	if len(rows) != 1 || rows[0]["value"] != "fpx" || rows[0]["data-name"] != "FPX" {
		t.Fatalf("picker rows for a stored [\"fpx\"] = %+v, want the FPX B2C row only", rows)
	}
	rec := fx.order(t, payments.PayMethodFPXB2B1)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a stored [\"fpx\"] workspace must refuse a B2B1 choice, got %d (body %s)", rec.Code, rec.Body.String())
	}
	if n := fx.chip.count(); n != 0 {
		t.Fatalf("%d CHIP purchase(s) created for a refused choice", n)
	}
}

// TestTwoFPXRowsCarryDifferentFeeRates: the fee table stores one rate per
// product, the rows carry their own rate to the page, and the sale records the
// rate of the row the buyer picked (RM1 for B2C, RM2 for B2B1).
func TestTwoFPXRowsCarryDifferentFeeRates(t *testing.T) {
	ctx := context.Background()
	fx := newPickFixtureWithAvailability(t, func() map[string]any {
		return map[string]any{"id": "pur_fees", "status": "created",
			"checkout_url": "https://pay.example.test/fees", "total": 100200, "currency": "MYR"}
	}, bothFPX)
	fx.setWhitelist(t, "fpx", "fpx_b2b1")
	// A fee only becomes active once the merchant has accepted the pass-on
	// disclosure (the acknowledgement gate ForForm reads), so record it — the
	// same two steps the Settings page performs.
	if err := fx.fees.Acknowledge(ctx, fx.ws, "test@example.test"); err != nil {
		t.Fatalf("acknowledge the pass-on disclosure: %v", err)
	}
	if err := fx.fees.Replace(ctx, fx.ws, "test@example.test", []dbstore.PaymentFeeRate{
		{Method: "fpx", Enabled: true, FixedSen: 100},
		{Method: "fpx_b2b1", Enabled: true, FixedSen: 200},
	}); err != nil {
		t.Fatalf("store the two FPX rates: %v", err)
	}

	rows := fx.renderPicker(t)
	byValue := map[string]map[string]string{}
	for _, r := range rows {
		byValue[r["value"]] = r
	}
	if got := byValue["fpx"]["data-fixed"]; got != "100" {
		t.Fatalf("FPX B2C row data-fixed = %q, want 100 (RM1.00)", got)
	}
	if got := byValue["fpx_b2b1"]["data-fixed"]; got != "200" {
		t.Fatalf("FPX B2B1 row data-fixed = %q, want 200 (RM2.00)", got)
	}

	// Paying with the corporate row charges the corporate rate. The asking price
	// is 100000 sen, so the grossed-up total is 100000 + its own 200 sen fee.
	rec := fx.order(t, payments.PayMethodFPXB2B1)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var feeSen, totalSen int64
	var feeMethod string
	if err := bundleTestPool(t).QueryRow(ctx,
		`SELECT fee_sen, fee_method, total_sen FROM sales WHERE account_id=$1 ORDER BY created_at DESC LIMIT 1`,
		fx.account).Scan(&feeSen, &feeMethod, &totalSen); err != nil {
		t.Fatalf("read the sale: %v", err)
	}
	if feeMethod != "fpx_b2b1" {
		t.Fatalf("sale fee_method = %q, want fpx_b2b1 — the B2B1 rate is a different key from the B2C one", feeMethod)
	}
	if feeSen != 200 || totalSen != 100200 {
		t.Fatalf("sale fee/total = %d/%d, want 200/100200 (RM2.00 on RM1,000.00)", feeSen, totalSen)
	}
}

// TestFPXRowDescriptionsAreTranslated guards requirement 5's i18n half on the Go
// side: every locale carries a description for BOTH FPX rows, and the two
// descriptions differ (a corporate payer must be able to tell them apart).
func TestFPXRowDescriptionsAreTranslated(t *testing.T) {
	for code, ls := range formLangs {
		if strings.TrimSpace(ls.PayFpxDesc) == "" {
			t.Errorf("locale %q: PayFpxDesc is empty", code)
		}
		if strings.TrimSpace(ls.PayFpxB2B1Desc) == "" {
			t.Errorf("locale %q: PayFpxB2B1Desc is empty — the FPX B2B1 row would render a blank second line", code)
		}
		if ls.PayFpxDesc == ls.PayFpxB2B1Desc {
			t.Errorf("locale %q: both FPX rows carry the same description (%q)", code, ls.PayFpxDesc)
		}
	}
}

// TestFPXRowLabelsAndDescriptionsReachThePage is the rendered half of the i18n
// requirement: the descriptions in the dictionary are what the buyer reads under
// each row.
func TestFPXRowLabelsAndDescriptionsReachThePage(t *testing.T) {
	fx := newPickFixtureWithAvailability(t, func() map[string]any {
		return map[string]any{"id": "pur_i18n", "status": "created",
			"checkout_url": "https://pay.example.test/i18n", "total": 100000, "currency": "MYR"}
	}, bothFPX)
	fx.setWhitelist(t, "fpx", "fpx_b2b1")

	rec := issue461Serve(t, fx.h, fx.host, http.MethodGet, "/"+fx.formSlug, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("render status = %d", rec.Code)
	}
	body := rec.Body.String()
	en := formLangs["en"]
	for _, want := range []string{en.PayFpxDesc, en.PayFpxB2B1Desc} {
		if !strings.Contains(body, want) {
			t.Fatalf("the served page does not carry %q — the row's second line is missing or the dictionary is not wired", want)
		}
	}
	// The B2B1 row must not be described with the personal banks list: an
	// English page where both rows read the same is the regression.
	if strings.Count(body, en.PayFpxDesc) < 1 {
		t.Fatalf("personal FPX description missing")
	}
	var seen []string
	for _, m := range regexp.MustCompile(`<span class="payrowdesc">([^<]*)</span>`).FindAllStringSubmatch(body, -1) {
		seen = append(seen, m[1])
	}
	if len(seen) < 2 || seen[0] == seen[1] {
		t.Fatalf("picker descriptions = %v, want two different lines (personal vs corporate)", seen)
	}
}
