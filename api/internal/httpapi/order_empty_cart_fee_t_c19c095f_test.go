package httpapi

// Card t_c19c095f — an EMPTY cart must not be priced like a free order.
//
// THE DEFECT (measured on prod, v1.23.0): a payment page whose active fee method
// carries a FIXED charge painted that fixed part as payable before the buyer had
// chosen anything. The page's own script runs `upd()` once at load; with no item
// ticked `base = 0`, and the gross-up mirror (like payments.GrossUp, where a
// zero asking amount deliberately returns the fixed fee for a genuinely
// zero-priced order) returned the fixed charge — so a fresh page read
//
//	#fee-sub RM0.00   #fee-amt RM0.50   #total RM0.50
//
// with nothing selected. The display is the whole harm (the client refuses to
// post an empty cart and the server refuses it with 400), but "Total RM0.50" on
// an empty page is a price the buyer never agreed to.
//
// WHAT THESE PROVE: the served page's OWN script is run in the goja+cascadia
// harness against the real markup, and the fee/total elements are read out of
// the DOM — first untouched (must be RM0.00), then after ticking the item (must
// gross up exactly as before). Both Go order layouts are covered, because both
// post to the same /order route and both priced the fixed fee on an empty cart.
//
// The over-fix is guarded too: a genuinely ZERO-PRICED selection still shows the
// fixed fee, because that is what the server charges for it
// (payments.GrossUp(0, fixed, bp) = fixed by design). Gating on `base > 0`
// alone would hide a real charge from the buyer, which is the worse defect.

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/andybalholm/cascadia"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/payments"
)

// feeRowText reads one fee-breakdown element out of the driven document.
func feeRowText(t *testing.T, d *menuDOM, id string) string {
	t.Helper()
	n := d.byID[id]
	if n == nil {
		t.Fatalf("the served page carries no #%s element", id)
	}
	return strings.TrimSpace(nodeText(n))
}

// tickProduct checks the page's only product checkbox the way a click does: the
// attribute the shim's `checked` getter reads is set, then the page's own change
// listener is fired.
func tickProduct(t *testing.T, d *menuDOM) {
	t.Helper()
	box := cascadia.Query(d.doc, cascadia.MustCompile("input.p"))
	if box == nil {
		t.Fatal("the served page carries no product checkbox (.p)")
	}
	setMenuAttr(box, "checked", "")
	d.dispatch("change", box)
}

// feeBlockForm publishes a payment form in fx's account whose blocks carry ONE
// priced product block — the shape the builder writes for a priced item, and the
// one the block renderer paints a `.p` checkbox from. The shared feeTestFixture
// stores `products` inside its block, which paints no line-item control, so it
// is served the submit-less "not accepting orders" page.
func feeBlockForm(t *testing.T, fx feeTestFixture) string {
	t.Helper()
	slug := "it-fee-empty-" + fx.account[:8]
	cfg := map[string]any{
		"description": "empty cart probe",
		"blocks": []any{map[string]any{
			"id": "b1", "type": "product", "product_id": fx.product,
			"value": "Produk Ujian", "price_sen": 100000,
		}},
	}
	f := &dbstore.Form{
		AccountID: fx.account, Slug: slug, Title: "Fee form",
		Status: dbstore.FormStatusPublished, Config: cfg,
	}
	if err := fx.h.Forms.Create(context.Background(), f); err != nil {
		t.Fatalf("create block form: %v", err)
	}
	return slug
}

// feeFixedPage renders the published payment form `slug` of fx and returns the
// page a buyer receives.
func feeFixedPage(t *testing.T, fx feeTestFixture, slug string) string {
	t.Helper()
	rec := issue461Serve(t, fx.h, fx.host, http.MethodGet, "/"+slug, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET form page = %d (body %.400s)", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// TestEmptyCartPaintsNoFeeOrTotalOnBlockLayout is the card's measurement in the
// block layout: FPX fixed RM0.50 is the only active method (so it is
// preselected by design) and nothing is ticked.
func TestEmptyCartPaintsNoFeeOrTotalOnBlockLayout(t *testing.T) {
	fx := newFeeTestFixture(t, true)
	fx.enableFee(t, dbstore.PaymentFeeRate{Method: payments.FeeMethodFPX, Enabled: true, FixedSen: 50})
	page := feeFixedPage(t, fx, feeBlockForm(t, fx))

	// The fee IS active on this page — otherwise the reading below proves nothing.
	if !strings.Contains(page, `class="feee"`) {
		t.Fatalf("the served page carries no fee chooser, so the empty-cart reading is vacuous:\n%.600s", page)
	}
	if !strings.Contains(page, `data-fixed="50"`) {
		t.Fatalf("the served page does not carry the fixed RM0.50 rate:\n%.600s", page)
	}

	d := newPageDOM(t, page)
	d.runPageScript(page, "itemsSubtotal")

	for _, id := range []string{"fee-sub", "fee-amt", "total"} {
		if got := feeRowText(t, d, id); got != "RM0.00" {
			t.Errorf("with nothing selected, #%s = %q, want %q. A cart that is merely NOT YET FILLED is not a "+
				"zero-priced order: painting the method's fixed charge as a total tells the buyer they owe money "+
				"before they have chosen anything.", id, got, "RM0.00")
		}
	}

	// And the gross-up is unchanged once there IS something in the cart:
	// RM1,000.00 + FPX RM0.50 = RM1,000.50.
	tickProduct(t, d)
	if got := feeRowText(t, d, "fee-sub"); got != "RM1000.00" {
		t.Errorf("after ticking the RM1,000 item, #fee-sub = %q, want %q", got, "RM1000.00")
	}
	if got := feeRowText(t, d, "fee-amt"); got != "RM0.50" {
		t.Errorf("after ticking the RM1,000 item, #fee-amt = %q, want %q (FPX fixed RM0.50)", got, "RM0.50")
	}
	if got := feeRowText(t, d, "total"); got != "RM1000.50" {
		t.Errorf("after ticking the RM1,000 item, #total = %q, want %q", got, "RM1000.50")
	}
}

// TestEmptyCartPaintsNoFeeOrTotalOnLegacyLayout is the same reading on the
// LAST-RESORT layout (a payment form with no blocks), which ships its own copy
// of the pricing script and had the same load-time call.
func TestEmptyCartPaintsNoFeeOrTotalOnLegacyLayout(t *testing.T) {
	fx := newFeeTestFixture(t, false)
	fx.enableFee(t, dbstore.PaymentFeeRate{Method: payments.FeeMethodFPX, Enabled: true, FixedSen: 50})
	page := feeFixedPage(t, fx, fx.formSlug)

	if !strings.Contains(page, `class="feee"`) || !strings.Contains(page, `data-fixed="50"`) {
		t.Fatalf("the legacy page does not carry the fixed RM0.50 chooser:\n%.600s", page)
	}

	d := newPageDOM(t, page)
	// Marker unique to the legacy layout's script (the block layout wires its
	// listeners with `function(b)`; this one uses an arrow).
	d.runPageScript(page, "boxes.forEach(b =>")

	for _, id := range []string{"fee-sub", "fee-amt", "total"} {
		if got := feeRowText(t, d, id); got != "RM0.00" {
			t.Errorf("last-resort layout: with nothing selected, #%s = %q, want %q", id, got, "RM0.00")
		}
	}

	tickProduct(t, d)
	if got := feeRowText(t, d, "total"); got != "RM1000.50" {
		t.Errorf("last-resort layout: after ticking the RM1,000 item, #total = %q, want %q", got, "RM1000.50")
	}
	if got := feeRowText(t, d, "fee-amt"); got != "RM0.50" {
		t.Errorf("last-resort layout: after ticking the RM1,000 item, #fee-amt = %q, want %q", got, "RM0.50")
	}
}

// TestEmptyCartPercentageOnlyAlreadyReadsZero: the control the card measured.
// A percentage-only method already read RM0.00 on an empty cart, and it must
// keep doing so (and still gross up once an item is chosen) — this is what
// tells a reader the fix did not disturb the rate-percentage path.
func TestEmptyCartPercentageOnlyAlreadyReadsZero(t *testing.T) {
	fx := newFeeTestFixture(t, true)
	fx.enableFee(t, dbstore.PaymentFeeRate{Method: payments.FeeMethodCard, Enabled: true, PercentBP: 100})
	page := feeFixedPage(t, fx, feeBlockForm(t, fx))

	d := newPageDOM(t, page)
	d.runPageScript(page, "itemsSubtotal")

	for _, id := range []string{"fee-sub", "fee-amt", "total"} {
		if got := feeRowText(t, d, id); got != "RM0.00" {
			t.Errorf("percentage-only page: with nothing selected, #%s = %q, want RM0.00", id, got)
		}
	}

	tickProduct(t, d)
	// RM1,000.00 at 1% grosses up to RM1,010.10 (fee RM10.10), not RM1,010.00.
	if got := feeRowText(t, d, "total"); got != "RM1010.10" {
		t.Errorf("percentage-only page: after ticking the RM1,000 item, #total = %q, want RM1010.10", got)
	}
	if got := feeRowText(t, d, "fee-amt"); got != "RM10.10" {
		t.Errorf("percentage-only page: after ticking the RM1,000 item, #fee-amt = %q, want RM10.10", got)
	}
}

// TestZeroPricedSelectionStillPaysTheFixedFee stops the obvious over-fix: the
// fee is suppressed on an EMPTY cart, not on a cheap one. A genuinely zero-priced
// selection is grossed up by the server (payments.GrossUp(0, fixed, 0) = the
// fixed charge, deliberate), so the page must show that charge or the buyer is
// surprised at CHIP.
func TestZeroPricedSelectionStillPaysTheFixedFee(t *testing.T) {
	fx := newFeeTestFixture(t, true)
	fx.enableFee(t, dbstore.PaymentFeeRate{Method: payments.FeeMethodFPX, Enabled: true, FixedSen: 50})

	// The block layout reads product prices LIVE, so a zero price on the row is
	// the price the page renders.
	if _, err := bundleTestPool(t).Exec(context.Background(),
		`UPDATE products SET price_sen = 0 WHERE id = $1`, fx.product); err != nil {
		t.Fatalf("zero the product price: %v", err)
	}

	page := feeFixedPage(t, fx, feeBlockForm(t, fx))
	d := newPageDOM(t, page)
	d.runPageScript(page, "itemsSubtotal")

	tickProduct(t, d)
	if got := feeRowText(t, d, "fee-sub"); got != "RM0.00" {
		t.Errorf("free item selected: #fee-sub = %q, want RM0.00", got)
	}
	if got := feeRowText(t, d, "fee-amt"); got != "RM0.50" {
		t.Errorf("free item selected: #fee-amt = %q, want RM0.50 — the server charges the fixed fee on a "+
			"zero-priced order, so hiding it here would be a surprise charge at CHIP", got)
	}
	if got := feeRowText(t, d, "total"); got != "RM0.50" {
		t.Errorf("free item selected: #total = %q, want RM0.50", got)
	}
}
