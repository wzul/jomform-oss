package httpapi

// t_c7515d28 — a MENU checkout showed a total that EXCLUDED the buyer-borne
// CHIP processing fee while the shared order core ADDED it to sales.total_sen
// for the method the buyer picked, and posted no expected_total_sen, so the
// server's "the price shown must be the price paid" guard could never fire.
//
// WHY THIS MATTERS: on the block checkout the same workspace and the same rate
// painted "Processing fee — FPX RM1.00" and "Total RM11.00" before the Pay
// button. On the menu the picker row already carried the rate
// (data-fixed="100" data-bp="0") but no script read it: the sheet's total, the
// sticky bar and the button all said the cart subtotal, while the order core
// grossed that subtotal up. A buyer was charged more than the button said.
//
// WHAT THESE PROVE (the page's OWN script is run in the goja+cascadia harness
// against the markup the template actually served, then the body it would POST
// is decoded):
//
//  1. with a stored rate the sheet shows the fee line and a Total, and the
//     sticky bar + Pay button carry that same grossed-up total;
//  2. switching the method re-prices all three on the spot (each row carries
//     its own rate, so a stale total is a wrong total);
//  3. the order body echoes the displayed total as expected_total_sen;
//  4. with no stored rate the page is unchanged (one subtotal line, no fee) —
//     and still echoes the amount it displayed;
//  5. the route honours that echo on a real sale: the page's body is accepted at
//     the grossed total, and the SAME body is refused (400) once the merchant
//     changes the rate under it — instead of charging the difference.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/payments"
)

// menuFeeData is the menu page of a workspace whose picker offers two methods
// with DIFFERENT stored rates, the shape measured on prod (FPX RM1.00, FPX B2B1
// RM2.00). FeeRows is derived by the same helper the handler calls, so a
// handler that stops setting it fails the markup assertions below.
func menuFeeData(t *testing.T) workspaceMenuData {
	t.Helper()
	d := menu492Fixture(t)
	d.PayChoices = []payMethodChoiceView{
		{Key: payments.PayMethodFPX, Label: "FPX", FixedSen: 100, RateLabel: "FPX RM 1.00", Selected: true},
		{Key: payments.PayMethodFPXB2B1, Label: "FPX B2B1", FixedSen: 200, RateLabel: "FPX B2B1 RM 2.00"},
		// A percentage-only row is the arm where the naive "base + base × rate"
		// is 1 sen short, so the mirror's walk must be exercised here too.
		{Key: payments.PayMethodCard, Label: "Card", PercentBP: 200, RateLabel: "Card 2.00%"},
	}
	d.FeeRows = menuFeeShown(d.PayChoices)
	return d
}

// chooseMenuPay checks one picker row the way a click does: a radio group holds
// at most one checked control, so the others are cleared, the wanted one is set,
// and the page's own change listener fires.
func chooseMenuPay(t *testing.T, d *menuDOM, value string) {
	t.Helper()
	rows := cascadia.QueryAll(d.doc, cascadia.MustCompile(`input[name=pay_method]`))
	if len(rows) == 0 {
		t.Fatal("the served menu page carries no payment-method rows (input[name=pay_method])")
	}
	var want *html.Node
	for _, n := range rows {
		removeMenuAttr(n, "checked")
		if menuAttr(n, "value") == value {
			want = n
		}
	}
	if want == nil {
		t.Fatalf("the served picker has no row with value %q", value)
	}
	setMenuAttr(want, "checked", "")
	d.dispatch("change", want)
}

// menuSheetRow reads one line of the basket sheet / bar out of the driven page.
func menuSheetRow(t *testing.T, d *menuDOM, id string) string {
	t.Helper()
	return feeRowText(t, d, id)
}

// TestMenuSheetDisclosesTheFeeForTheChosenMethod is the card's measurement, on
// the page the template serves: one RM7.80 dish, FPX (fixed RM1.00) preselected.
// Before the fix every number below was "RM 7.80" and the body carried no
// expected_total_sen at all.
func TestMenuSheetDisclosesTheFeeForTheChosenMethod(t *testing.T) {
	data := menuFeeData(t)
	dom, body := newMenuDOMPage(t, data)

	// The page must carry the breakdown and the rate it prices with — otherwise
	// the readings below come from a page that could never show a fee.
	for _, want := range []string{
		`id="menuFeeRow"`, `id="menuFeeAmt"`, `id="menuFeeRate"`, `id="menuTotal"`,
		`data-fixed="100"`, `data-fixed="200"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("served menu page missing %q — the fee cannot be disclosed without it", want)
		}
	}

	// One dish in the basket, the way a tap does it.
	dom.dispatch("click", dom.buttonFor("p1"))
	if n := len(dom.cart()); n != 1 {
		t.Fatalf("adding the dish put %d line(s) in the basket, want 1", n)
	}

	// FPX is preselected: fee RM1.00, so every amount the buyer can act on is
	// RM8.80 — not the RM7.80 subtotal.
	if got := menuSheetRow(t, dom, "menuSubtotal"); got != "RM 7.80" {
		t.Errorf("#menuSubtotal = %q, want %q (the dishes, before the fee)", got, "RM 7.80")
	}
	if got := menuSheetRow(t, dom, "menuFeeAmt"); got != "RM 1.00" {
		t.Errorf("#menuFeeAmt = %q, want %q (FPX fixed rate)", got, "RM 1.00")
	}
	if got := menuSheetRow(t, dom, "menuFeeRate"); got != "— FPX" {
		t.Errorf("#menuFeeRate = %q, want %q — the buyer must be told which method's rate this is", got, "— FPX")
	}
	if got := menuSheetRow(t, dom, "menuTotal"); got != "RM 8.80" {
		t.Errorf("#menuTotal = %q, want %q (RM7.80 + FPX RM1.00)", got, "RM 8.80")
	}
	if got := menuSheetRow(t, dom, "menuBarTotal"); got != "RM 8.80" {
		t.Errorf("#menuBarTotal = %q, want %q — the sticky bar must not promise less than the charge", got, "RM 8.80")
	}
	if got := strings.TrimSpace(nodeText(dom.byID["menuPayLabel"])); got != "Checkout RM 8.80" {
		t.Errorf("#menuPayLabel = %q, want %q", got, "Checkout RM 8.80")
	}
	if menuHasAttr(dom.byID["menuFeeRow"], "hidden") {
		t.Error("#menuFeeRow is hidden with a non-empty basket — the buyer never sees the fee")
	}

	// Switching method re-prices on the spot: FPX B2B1 costs RM2.00.
	chooseMenuPay(t, dom, payments.PayMethodFPXB2B1)
	if got := menuSheetRow(t, dom, "menuFeeAmt"); got != "RM 2.00" {
		t.Errorf("after choosing FPX B2B1, #menuFeeAmt = %q, want %q", got, "RM 2.00")
	}
	if got := menuSheetRow(t, dom, "menuFeeRate"); got != "— FPX B2B1" {
		t.Errorf("after choosing FPX B2B1, #menuFeeRate = %q, want %q", got, "— FPX B2B1")
	}
	if got := menuSheetRow(t, dom, "menuTotal"); got != "RM 9.80" {
		t.Errorf("after choosing FPX B2B1, #menuTotal = %q, want %q (RM7.80 + RM2.00)", got, "RM 9.80")
	}
	if got := menuSheetRow(t, dom, "menuBarTotal"); got != "RM 9.80" {
		t.Errorf("after choosing FPX B2B1, #menuBarTotal = %q, want %q", got, "RM 9.80")
	}
	if got := strings.TrimSpace(nodeText(dom.byID["menuPayLabel"])); got != "Checkout RM 9.80" {
		t.Errorf("after choosing FPX B2B1, #menuPayLabel = %q, want %q", got, "Checkout RM 9.80")
	}

	// A percentage-only rate exercises the gross-up itself: 2.00% of RM7.80 is
	// RM0.156, so the charge is RM7.96 (fee RM0.16) and not "RM7.80 + RM0.16" from
	// a naive multiply. The numbers match payments.GrossUp to the sen.
	chooseMenuPay(t, dom, payments.PayMethodCard)
	if got := menuSheetRow(t, dom, "menuFeeAmt"); got != "RM 0.16" {
		t.Errorf("after choosing Card 2.00%%, #menuFeeAmt = %q, want %q", got, "RM 0.16")
	}
	if got := menuSheetRow(t, dom, "menuTotal"); got != "RM 7.96" {
		t.Errorf("after choosing Card 2.00%%, #menuTotal = %q, want %q", got, "RM 7.96")
	}
	if got := strings.TrimSpace(nodeText(dom.byID["menuPayLabel"])); got != "Checkout RM 7.96" {
		t.Errorf("after choosing Card 2.00%%, #menuPayLabel = %q, want %q", got, "Checkout RM 7.96")
	}

	// The order body must echo exactly what the button said, and the method it
	// said it with — that is what lets the server refuse a changed price or rate.
	posted := dom.postCheckout()
	var order struct {
		PayMethod        string `json:"pay_method"`
		ExpectedTotalSen int64  `json:"expected_total_sen"`
	}
	if err := json.Unmarshal([]byte(posted), &order); err != nil {
		t.Fatalf("decode the posted order body %q: %v", posted, err)
	}
	if order.PayMethod != payments.PayMethodCard {
		t.Errorf("posted pay_method = %q, want %q (the row the buyer last ticked)", order.PayMethod, payments.PayMethodCard)
	}
	if displayed := menuSheetRow(t, dom, "menuTotal"); displayed != "RM 7.96" {
		t.Fatalf("#menuTotal = %q at submit time, want %q", displayed, "RM 7.96")
	}
	if order.ExpectedTotalSen != 796 {
		t.Errorf("posted expected_total_sen = %d, want 796 — the displayed, fee-bearing total (body %s). "+
			"Without it the server's mismatch guard is skipped and a changed rate is charged silently.",
			order.ExpectedTotalSen, posted)
	}
}

// TestMenuSheetWithoutARateIsUnchanged is the control: no stored rate means no
// fee, so the sheet keeps its single subtotal line and every amount stays the
// cart total. The body still echoes that total (it is the only price the buyer
// ever saw).
func TestMenuSheetWithoutARateIsUnchanged(t *testing.T) {
	dom, body := newMenuDOMPage(t, menu492Fixture(t))

	if strings.Contains(body, `id="menuFeeRow"`) {
		t.Error("a menu with no stored rate renders a processing-fee line — there is no fee to disclose")
	}

	dom.dispatch("click", dom.buttonFor("p1"))
	if n := len(dom.cart()); n != 1 {
		t.Fatalf("adding the dish put %d line(s) in the basket, want 1", n)
	}
	if got := menuSheetRow(t, dom, "menuSubtotal"); got != "RM 7.80" {
		t.Errorf("#menuSubtotal = %q, want %q", got, "RM 7.80")
	}
	if got := menuSheetRow(t, dom, "menuBarTotal"); got != "RM 7.80" {
		t.Errorf("#menuBarTotal = %q, want %q (no rate, no fee)", got, "RM 7.80")
	}
	if got := strings.TrimSpace(nodeText(dom.byID["menuPayLabel"])); got != "Checkout RM 7.80" {
		t.Errorf("#menuPayLabel = %q, want %q", got, "Checkout RM 7.80")
	}

	var order struct {
		ExpectedTotalSen int64 `json:"expected_total_sen"`
	}
	if err := json.Unmarshal([]byte(dom.postCheckout()), &order); err != nil {
		t.Fatalf("decode the posted order body: %v", err)
	}
	if order.ExpectedTotalSen != 780 {
		t.Errorf("posted expected_total_sen = %d, want 780", order.ExpectedTotalSen)
	}
}

// TestMenuCheckoutRefusesAChangedRate is the server half on a real database and
// a real CHIP stand-in: the body the menu page now posts is accepted at the
// grossed total, and the SAME body is refused after the merchant changes the
// rate — no sale, no CHIP purchase, and the buyer is told the total moved.
func TestMenuCheckoutRefusesAChangedRate(t *testing.T) {
	fx := newPickFixtureWithAvailability(t, func() map[string]any {
		return map[string]any{"id": "pur_menu_fee", "checkout_url": "https://pay.example.test/checkout/menu"}
	}, []string{"fpx", "fpx_b2b1"})
	fx.setWhitelist(t, payments.PayMethodFPX, payments.PayMethodFPXB2B1)

	// Turn the pick fixture's form into a MENU form: its product block already
	// references the live product, so the page renders one dish and the sheet.
	ctx := context.Background()
	pool := bundleTestPool(t)
	var formID, title string
	var cfg map[string]any
	if err := pool.QueryRow(ctx,
		`SELECT id, title, config FROM forms WHERE slug = $1`, fx.formSlug).Scan(&formID, &title, &cfg); err != nil {
		t.Fatalf("read the fixture form: %v", err)
	}
	cfg["menu"] = map[string]any{"enabled": true, "categories": []any{}}
	if err := fx.h.Forms.Update(ctx, fx.account, formID, title, cfg, nil); err != nil {
		t.Fatalf("write the menu config: %v", err)
	}
	menuEnableFee(t, fx, dbstore.PaymentFeeRate{Method: payments.FeeMethodFPX, Enabled: true, FixedSen: 100})

	page := issue461Serve(t, fx.h, fx.host, http.MethodGet, "/"+fx.formSlug, "")
	if page.Code != http.StatusOK {
		t.Fatalf("GET menu page = %d (body %.400s)", page.Code, page.Body.String())
	}
	for _, want := range []string{`id="menuFeeRow"`, `id="menuTotal"`, `data-fixed="100"`, `expected_total_sen:jfTotal`} {
		if !strings.Contains(page.Body.String(), want) {
			t.Fatalf("the served menu page does not carry %q, so it cannot disclose (or echo) the fee:\n%.800s",
				want, page.Body.String())
		}
	}

	// The exact body the page now posts: RM1,000.00 + FPX RM1.00 = RM1,001.00.
	posted := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"` + fx.product + `","variation_id":"","quantity":1}],` +
		`"payment_method":"chip","pay_method":"fpx","expected_total_sen":100100,"fields":{}}`

	rec := issue461Serve(t, fx.h, fx.host, http.MethodPost, "/"+fx.formSlug+"/order", posted)
	if rec.Code != http.StatusCreated {
		t.Fatalf("order at the displayed total = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		RefCode string `json:"ref_code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode the order response: %v", err)
	}
	sale, err := fx.h.Sales.ByRefCode(ctx, resp.RefCode)
	if err != nil {
		t.Fatalf("read the created sale: %v", err)
	}
	if sale.TotalSen != 100100 || sale.FeeSen != 100 || sale.FeeMethod != payments.FeeMethodFPX {
		t.Errorf("sale total/fee = %d/%d (%q), want 100100/100 (%q) — the page and the charge agree",
			sale.TotalSen, sale.FeeSen, sale.FeeMethod, payments.FeeMethodFPX)
	}

	// The merchant raises the rate while the buyer's basket sits open. The page
	// still says RM1,001.00, so that is what is posted — and it must be refused
	// rather than charged at the new price.
	menuEnableFee(t, fx, dbstore.PaymentFeeRate{Method: payments.FeeMethodFPX, Enabled: true, FixedSen: 300})
	bought := fx.chip.count()
	salesBefore := menuFormSaleCount(t, formID)

	rec = issue461Serve(t, fx.h, fx.host, http.MethodPost, "/"+fx.formSlug+"/order", posted)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("stale-total order = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), errFeeTotalMismatch.Error()) {
		t.Errorf("stale-total body = %s, want %q", rec.Body.String(), errFeeTotalMismatch.Error())
	}
	if got := menuFormSaleCount(t, formID); got != salesBefore {
		t.Errorf("a refused order still created a sale (%d -> %d)", salesBefore, got)
	}
	if got := fx.chip.count(); got != bought {
		t.Errorf("a refused order still created a CHIP purchase (%d -> %d)", bought, got)
	}
}

// menuEnableFee stores the workspace's rates and records the merchant's
// acknowledgement (the settings gate the store enforces).
func menuEnableFee(t *testing.T, fx pickFixture, rates ...dbstore.PaymentFeeRate) {
	t.Helper()
	ctx := context.Background()
	if err := fx.fees.Acknowledge(ctx, fx.ws, "owner@example.test"); err != nil {
		t.Fatalf("acknowledge the fee: %v", err)
	}
	if err := fx.fees.Replace(ctx, fx.ws, "owner@example.test", rates); err != nil {
		t.Fatalf("store the rates: %v", err)
	}
}

func menuFormSaleCount(t *testing.T, formID string) int {
	t.Helper()
	var n int
	if err := bundleTestPool(t).QueryRow(context.Background(),
		`SELECT count(*) FROM sales WHERE form_id = $1`, formID).Scan(&n); err != nil {
		t.Fatalf("count sales of the menu form: %v", err)
	}
	return n
}
