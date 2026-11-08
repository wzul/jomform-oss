package httpapi

// t_aa6e3596 — a variant that goes sold out while the menu page is OPEN.
//
// THE DEFECT. The menu page's sold-out re-check read the option's RENDER-TIME
// `disabled` attribute. That attribute is written when the page is rendered, and
// the menu page stays open — that is how a buyer uses it. So when a dish sells
// out while the page is being read the attribute is simply wrong: the pick was
// accepted, the line landed in the basket, and the buyer only found out at
// checkout, where the order core answers 409 "stok habis".
//
// THE FIX. `unbuyable` still consults the rendered attribute (it is a real guard,
// and it is all a page with no JS has), but it ALSO consults the server's live
// answer, fetched from GET /menu-stock/{slug} before every add and again when
// the page opens. The rendered attribute stays the fallback, so a page whose
// read fails keeps precisely the guard it had.
//
// WHAT IS PINNED HERE
//  1. The shape the card describes: a page whose SERVED markup says "in stock"
//     (so the attribute cannot refuse) and whose live read says sold out must
//     REFUSE the pick; the control — same markup, payload says in stock — must
//     still ADD.
//  2. The page must actually ASK. Asserting only the outcome cannot tell "asked
//     and refused" from "refused for some other reason", and the previous,
//     attribute-only implementation would have satisfied that weaker shape.
//  3. A basket line added before the dish sold out must not survive a reload:
//     the basket outlives the page and nothing re-read the rendered markup for
//     it.
//  4. Checkout must re-read once more, so a line that goes sold out while the
//     basket sits there is refused client-side with the dish named, instead of
//     meeting the server's 409.
//  5. The route behind the read is gated like the page it serves, and is never
//     cached.
//
// BITE PROOF: api/scripts/biteproof_menu_variant_gaps.py mutates the re-check
// and asserts the guards below go RED.
//
// Scope, stated plainly: this is the goja/cascadia DOM harness, not a browser
// (see menu_dom_test.go). The live-page check on this card covers what a harness
// cannot.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// liveStockPayload builds one live-stock row, the shape handleMenuStock serves.
func liveStockPayload(productID, variationID string, soldOut bool) map[string]any {
	return map[string]any{"product_id": productID, "variation_id": variationID, "sold_out": soldOut}
}

// menuStaleVariantPage serves the two-dish fixture in the state this defect
// needs: the dish's variant was IN STOCK when the page was rendered, so the
// served markup offers it enabled — the attribute cannot refuse it — and the
// page is left open across the stock change.
func menuStaleVariantPage(t *testing.T) (*menuDOM, string) {
	t.Helper()
	d, body := newMenuDOMPage(t, menuTwoRowsFixture(t))
	sel := d.selectorFor("dish-id")
	if sel == nil {
		t.Fatalf("the dish has no selector\nbody:\n%s", body)
	}
	opts := optionNodes(sel)
	if len(opts) != 2 {
		t.Fatalf("the selector offers %d options, want the placeholder + 1 variant", len(opts))
	}
	if menuHasAttr(opts[1], "disabled") {
		t.Fatal("precondition failed: the served markup already renders the variant sold out, so " +
			"this page cannot reproduce the stale render-time attribute")
	}
	return d, body
}

// THE DEFECT. A variant that sold out after the page rendered is still enabled
// in the DOM, so the disabled-attribute re-check cannot see it; the pick must
// still be refused, on the live answer.
func TestMenuLiveStockRefusesAVariantThatSoldOutWhileThePageWasOpen(t *testing.T) {
	d, body := menuStaleVariantPage(t)
	sel := d.selectorFor("dish-id")
	if sel == nil {
		t.Fatalf("the dish has no selector\nbody:\n%s", body)
	}
	// The buyer picked this variant while it was in stock; the server now says
	// it is sold out. The option is NOT disabled in the DOM — that is the point.
	d.stockResponse = []map[string]any{liveStockPayload("dish-id", "dish-var", true)}

	d.chooseVariant(sel, "dish-var")
	d.dispatch("change", sel)
	d.flushTimers()

	if got := len(d.cart()); got != 0 {
		t.Fatalf("picking a variant that went sold out while the page was open added %d basket "+
			"line(s) — the basket holds a line the order endpoint refuses with 409 is out of stock\nbasket: %v",
			got, d.cart())
	}
	// The page must have ASKED: without the read the outcome above could come
	// from any other refusal, and the attribute-only version passed that shape.
	if d.stockReads == 0 {
		t.Error("the page refused the pick without reading the live stock — the refusal cannot be " +
			"about the stock change, and the rendered attribute is the only thing it can have consulted")
	}
	if !menuHasClass(sel, "missing") {
		t.Error("the refused pick did not flag the selector, so the buyer got no feedback")
	}
	hint := d.hintFor(sel)
	if hint == nil || menuHasAttr(hint, "hidden") {
		t.Fatal("the refusal left the hint hidden, so nothing tells the buyer why the pick was refused")
	}
	if got := strings.TrimSpace(nodeText(hint)); got != formLangs["en"].MenuSoldOut {
		t.Errorf("the refusal hint reads %q, want the localised sold-out message %q",
			got, formLangs["en"].MenuSoldOut)
	}
}

// The control: the SAME stale page, with the server still reporting the variant
// in stock, must still add. Without this, a fix that simply refuses every pick
// on a stale page would pass the guard above.
func TestMenuLiveStockInStockPickStillAdds(t *testing.T) {
	d, body := menuStaleVariantPage(t)
	sel := d.selectorFor("dish-id")
	if sel == nil {
		t.Fatalf("the dish has no selector\nbody:\n%s", body)
	}
	d.stockResponse = []map[string]any{liveStockPayload("dish-id", "dish-var", false)}

	d.chooseVariant(sel, "dish-var")
	d.dispatch("change", sel)
	d.flushTimers()

	cart := d.cart()
	if len(cart) != 1 {
		t.Fatalf("an in-stock pick on a stale page added %d basket line(s), want 1 — the live "+
			"re-check refuses buyable variants\nbasket: %v", len(cart), cart)
	}
	if cart[0]["vid"] != "dish-var" {
		t.Errorf("the added line carries variation %v, want dish-var", cart[0]["vid"])
	}
	if d.stockReads == 0 {
		t.Error("the page added without reading the live stock, so the guard above proves nothing")
	}
}

// The card tap is the other add path, and a stale page can reach it with the
// variant already selected (the buyer picked before the dish sold out, then taps
// the dish again to top it up). It must refuse on the same live answer.
func TestMenuLiveStockRefusesACardTapOnAStaleSoldOutVariant(t *testing.T) {
	d, body := menuStaleVariantPage(t)
	sel := d.selectorFor("dish-id")
	if sel == nil {
		t.Fatalf("the dish has no selector\nbody:\n%s", body)
	}
	// The choice is already there (as a restored/programmatic selection is), so
	// the tap does not prompt — it tries to add.
	d.chooseVariant(sel, "dish-var")
	d.stockResponse = []map[string]any{liveStockPayload("dish-id", "dish-var", true)}

	d.dispatch("click", d.buttonFor("dish-id"))
	d.flushTimers()

	if got := len(d.cart()); got != 0 {
		t.Fatalf("tapping the dish of a variant that went sold out added %d basket line(s), want 0", got)
	}
	if !menuHasClass(sel, "missing") {
		t.Error("the refused card tap did not flag the selector")
	}
}

// The basket OUTLIVES the page. A line added while the dish was in stock is
// still in localStorage when the buyer opens the page again, and nothing
// consults the rendered markup for it — so the page must re-check the basket it
// just restored, drop the line, and say so.
func TestMenuReloadDropsABasketLineThatWentSoldOut(t *testing.T) {
	d, body := menuStaleVariantPage(t)
	sel := d.selectorFor("dish-id")
	if sel == nil {
		t.Fatalf("the dish has no selector\nbody:\n%s", body)
	}

	// The buyer adds the variant while it is in stock…
	d.stockResponse = []map[string]any{liveStockPayload("dish-id", "dish-var", false)}
	d.chooseVariant(sel, "dish-var")
	d.dispatch("change", sel)
	d.flushTimers()
	if got := len(d.cart()); got != 1 {
		t.Fatalf("precondition failed: the in-stock add recorded %d line(s), want 1", got)
	}

	// …the dish sells out, and the buyer reloads: the same localStorage basket,
	// a freshly served page, and a live answer that has changed.
	d2 := reloadMenuDOM(t, body, d.storage, []map[string]any{liveStockPayload("dish-id", "dish-var", true)})

	if got := len(d2.cart()); got != 0 {
		t.Fatalf("the reloaded page kept %d basket line(s) for a dish that sold out — the buyer "+
			"checks out with it and only meets the 409\nbasket: %v", got, d2.cart())
	}
	if d2.stockReads == 0 {
		t.Error("the reloaded page did not read the live stock, so it cannot have known the dish sold out")
	}
	// The basket changed without the buyer touching it, so they are told.
	errEl := d2.byID["menuErr"]
	if errEl == nil {
		t.Fatal("the served page has no error element to carry the notice")
	}
	if menuHasAttr(errEl, "hidden") {
		t.Error("the dropped basket line was announced nowhere — the buyer's basket changed silently")
	}
	got := strings.TrimSpace(nodeText(errEl))
	if !strings.Contains(got, formLangs["en"].MenuStockChanged) {
		t.Errorf("the notice reads %q, want the localised message %q", got, formLangs["en"].MenuStockChanged)
	}
	if !strings.Contains(got, "Nasi Lemak Ayam") {
		t.Errorf("the notice does not name the dropped dish (%q), so the buyer cannot tell which line went", got)
	}
	if sheet := d2.byID["menuSheet"]; sheet != nil && menuHasAttr(sheet, "hidden") {
		t.Error("the basket sheet stayed closed, so a notice written into it is never seen")
	}
}

// The control for the reload guard: a basket line whose dish is STILL in stock
// survives the reload untouched. Without this, a reload that simply emptied the
// basket would pass.
func TestMenuReloadKeepsAnInStockBasketLine(t *testing.T) {
	d, body := menuStaleVariantPage(t)
	sel := d.selectorFor("dish-id")
	if sel == nil {
		t.Fatalf("the dish has no selector\nbody:\n%s", body)
	}
	d.stockResponse = []map[string]any{liveStockPayload("dish-id", "dish-var", false)}
	d.chooseVariant(sel, "dish-var")
	d.dispatch("change", sel)
	d.flushTimers()
	if got := len(d.cart()); got != 1 {
		t.Fatalf("precondition failed: the in-stock add recorded %d line(s), want 1", got)
	}

	d2 := reloadMenuDOM(t, body, d.storage, []map[string]any{liveStockPayload("dish-id", "dish-var", false)})

	cart := d2.cart()
	if len(cart) != 1 {
		t.Fatalf("the reloaded page kept %d basket line(s) for an in-stock dish, want 1 — the "+
			"reload guard throws away a basket the buyer can still buy\nbasket: %v", len(cart), cart)
	}
	if cart[0]["vid"] != "dish-var" {
		t.Errorf("the surviving line carries variation %v, want dish-var", cart[0]["vid"])
	}
}

// Checkout is the last moment the page can catch a line that went sold out while
// the basket sat there. It must re-read and refuse client-side, naming the dish,
// instead of POSTing a payload the server answers 409 for.
func TestMenuCheckoutRefusesRatherThanPostingASoldOutLine(t *testing.T) {
	d, body := menuStaleVariantPage(t)
	sel := d.selectorFor("dish-id")
	if sel == nil {
		t.Fatalf("the dish has no selector\nbody:\n%s", body)
	}
	d.stockResponse = []map[string]any{liveStockPayload("dish-id", "dish-var", false)}
	d.chooseVariant(sel, "dish-var")
	d.dispatch("change", sel)
	d.flushTimers()
	if got := len(d.cart()); got != 1 {
		t.Fatalf("precondition failed: the in-stock add recorded %d line(s), want 1", got)
	}
	// The dish sells out while the basket sits open…
	d.stockResponse = []map[string]any{liveStockPayload("dish-id", "dish-var", true)}

	// …and the buyer pays.
	before := len(d.fetches)
	setMenuAttr(d.byID["menuName"], "value", "Ali")
	setMenuAttr(d.byID["menuEmail"], "value", "ali@example.test")
	d.dispatch("submit", d.byID["menuCheckout"])
	d.flushTimers()

	for _, u := range d.fetches[before:] {
		if strings.Contains(u, "/order") {
			t.Errorf("the checkout POSTed %s for a line the server will refuse with 409 — the page "+
				"held the live answer that says so", u)
		}
	}
	errEl := d.byID["menuErr"]
	if errEl == nil || menuHasAttr(errEl, "hidden") {
		t.Fatal("the refused checkout told the buyer nothing")
	}
	got := strings.TrimSpace(nodeText(errEl))
	if !strings.Contains(got, formLangs["en"].MenuStockChanged) {
		t.Errorf("the refusal reads %q, want the localised message %q", got, formLangs["en"].MenuStockChanged)
	}
	if !strings.Contains(got, "Nasi Lemak Ayam") {
		t.Errorf("the refusal does not name the dish (%q), so the buyer cannot tell which line went", got)
	}
	// The basket is retained: the buyer can remove the line and retry.
	if n := len(d.cart()); n != 1 {
		t.Errorf("the refused checkout left %d basket line(s), want the basket retained (1)", n)
	}
}

// The control for the checkout guard: with the dish still in stock the checkout
// must actually POST, so a guard that blocks every checkout cannot pass.
func TestMenuCheckoutStillPostsWhenEverythingIsInStock(t *testing.T) {
	d, body := menuStaleVariantPage(t)
	sel := d.selectorFor("dish-id")
	if sel == nil {
		t.Fatalf("the dish has no selector\nbody:\n%s", body)
	}
	d.stockResponse = []map[string]any{liveStockPayload("dish-id", "dish-var", false)}
	d.chooseVariant(sel, "dish-var")
	d.dispatch("change", sel)
	d.flushTimers()

	before := len(d.fetches)
	setMenuAttr(d.byID["menuName"], "value", "Ali")
	setMenuAttr(d.byID["menuEmail"], "value", "ali@example.test")
	d.dispatch("submit", d.byID["menuCheckout"])
	d.flushTimers()

	posted := false
	for _, u := range d.fetches[before:] {
		if strings.Contains(u, "/order") {
			posted = true
		}
	}
	if !posted {
		t.Fatalf("the checkout did not POST for an all-in-stock basket (requests: %v, error: %q)",
			d.fetches[before:], menuAttr(d.byID["menuErr"], "data-x"))
	}
	if body, ok := d.storage["__posted_body"]; !ok || !strings.Contains(body, `"variation_id":"dish-var"`) {
		t.Errorf("the posted body does not carry the chosen variation: %q", body)
	}
}

// A page whose live-stock read answers NOTHING must keep exactly the guard it
// had: the rendered attribute. Nothing may be added for a variant the server
// rendered disabled, and nothing may be refused just because the read was empty.
func TestMenuStalePageFallsBackToTheRenderedAttributeWhenTheReadIsEmpty(t *testing.T) {
	data := menuTwoRowsFixture(t)
	zero := 0
	data.Categories[0].Products[1].Variations[0].Stock = &zero
	data.Categories[0].Products[1].Variations[0].SoldOut = true
	d, body := newMenuDOMPage(t, data)
	sel := d.selectorFor("dish-id")
	if sel == nil {
		t.Fatalf("the dish has no selector\nbody:\n%s", body)
	}
	// No live-stock payload: the shim answers an empty list, which is the state
	// a failed or blocked read leaves the page in.
	d.chooseVariant(sel, "dish-var")
	d.dispatch("change", sel)
	d.flushTimers()

	if got := len(d.cart()); got != 0 {
		t.Fatalf("a rendered-disabled variant was added when the live read answered nothing (%d lines) "+
			"— the rendered attribute is the fallback, not something the read replaces", got)
	}

	// And the other direction: an in-stock rendered variant is still addable
	// with no live answer, so an empty read cannot freeze the menu.
	d2, body2 := menuStaleVariantPage(t)
	sel2 := d2.selectorFor("dish-id")
	if sel2 == nil {
		t.Fatalf("the dish has no selector\nbody:\n%s", body2)
	}
	d2.chooseVariant(sel2, "dish-var")
	d2.dispatch("change", sel2)
	d2.flushTimers()
	if got := len(d2.cart()); got != 1 {
		t.Fatalf("an in-stock pick was refused because the live read answered nothing (%d lines, want 1) "+
			"— an empty read must leave the menu usable", got)
	}
}

// The two option states must disagree in exactly the way the fix relies on: a
// variant the server rendered IN STOCK is not disabled, so nothing but the live
// answer can refuse it. This is the precondition every guard above depends on,
// asserted rather than assumed.
func TestMenuStaleVariantIsNotRenderedDisabled(t *testing.T) {
	d, body := menuStaleVariantPage(t)
	sel := d.selectorFor("dish-id")
	if sel == nil {
		t.Fatalf("the dish has no selector\nbody:\n%s", body)
	}
	if menuHasAttr(optionNodes(sel)[1], "disabled") {
		t.Error("the in-stock variant is rendered disabled, so the stale-attribute case cannot be " +
			"reproduced and the guards above would pass without the live read")
	}
}

// ─── the route behind the read ───────────────────────────────────────────────

// t_00b6a9cb — the SIMPLE (non-variant) dish tap.
//
// THE DEFECT. The card/row tap had two branches: a variable item went through
// addIfBuyable (attribute + live read), and a simple item called addItem
// directly with the served attributes. A sold-out simple dish is served the
// SAME button as an in-stock one — there is no disabled attribute and no
// sold-out marker to fall back on (the only reason a variant has one is that
// the selector can carry it) — so the tap put a line the order core answers 409
// for into the basket, and the buyer only found out at checkout. The live-stock
// read already publishes the product's own row with variation_id:""
// (menuLiveStock), and soldOutLive(id,'') already keys on it, so the guard is
// the same one the variable path uses; it simply was not reached.
//
// WHAT IS PINNED HERE
//  1. A simple dish whose live read says sold out must REFUSE the tap, and the
//     page must be seen to ASK (the read count must grow on the tap — the
//     page-open read is not evidence about the tap).
//  2. A refused tap must SAY so: a simple item has no selector to flag, so the
//     refusal uses the sheet's error line, naming the dish.
//  3. The controls: the same tap on an in-stock answer must still add, and an
//     EMPTY read must still add (the render-time state is the fallback, so a
//     failed read cannot freeze the menu).
//  4. The fixture's precondition is asserted, not assumed: the sold-out simple
//     dish carries nothing in the served markup that could refuse it.

// menuSimpleDishPage serves the menu fixture and returns it with the id of its
// SIMPLE dish — the one with no selector, which is the shape this defect is
// about. The fixture's simple dish is asserted to be simple here, so a later
// change that gives it variations cannot silently turn these guards into
// variable-dish guards.
func menuSimpleDishPage(t *testing.T) (*menuDOM, string) {
	t.Helper()
	d, body := newMenuDOMPage(t, menuVariationFixture(t))
	if sel := d.selectorFor("p3"); sel != nil {
		t.Fatalf("the fixture's simple dish now has a variant selector, so it cannot reproduce the "+
			"simple-dish shape\nbody:\n%s", body)
	}
	btn := d.buttonFor("p3")
	if btn == nil {
		t.Fatalf("the fixture renders no add button for its simple dish\nbody:\n%s", body)
	}
	// The served markup offers nothing to refuse it with: this is the whole
	// reason the tap had to be taught to ask. A `disabled` here would mean the
	// guard below could pass without any live read.
	if menuHasAttr(btn, "disabled") {
		t.Fatal("precondition failed: the simple dish is served disabled, so the tap could be " +
			"refused from the markup alone and these guards would prove nothing")
	}
	// A menu with no variant selector at all, so varOf() answers null: the only
	// way into the simple branch. selectorFor resolves exactly what varOf does
	// (select.mvar inside the item), so a non-nil answer here would mean the tap
	// could never reach the branch these guards exist for.
	if sels := d.selectorsFor("p3"); len(sels) != 0 {
		t.Fatalf("precondition failed: the simple dish carries %d selector(s), so the tap cannot "+
			"reach the simple branch", len(sels))
	}
	return d, body
}

// THE DEFECT. A simple dish that the server's live read reports sold out must
// not be added by the tap — and the page must actually have asked, because
// "no line was added" alone cannot tell a refusal from a tap that never landed.
func TestMenuLiveStockRefusesASoldOutSimpleDishTap(t *testing.T) {
	d, _ := menuSimpleDishPage(t)
	// The buyer read the page while the dish was in stock; it is sold out now.
	// Nothing in the served markup says so — that is the defect.
	d.stockResponse = []map[string]any{liveStockPayload("p3", "", true)}

	before := d.stockReads
	d.dispatch("click", d.buttonFor("p3"))
	d.flushTimers()

	if got := len(d.cart()); got != 0 {
		t.Fatalf("tapping a sold-out simple dish added %d basket line(s) — the basket holds a line "+
			"the order endpoint refuses with 409 is out of stock\nbasket: %v", got, d.cart())
	}
	// The page must have asked ON THE TAP. The page-open read (loadLiveStock on
	// startup) is not evidence about the tap: the served markup is byte-for-byte
	// the same for a sold-out and an in-stock simple dish, so without a read
	// here the refusal could not be about the stock at all.
	if d.stockReads <= before {
		t.Errorf("the tap refused without reading the live stock (%d read(s) before, %d after) — the "+
			"refusal cannot be about the stock change, and the served markup is identical either way",
			before, d.stockReads)
	}
	// A simple item has no selector to flag, so the refusal must use the page's
	// other announcement surface — and name the dish, or the buyer cannot tell
	// what the tap did.
	errEl := d.byID["menuErr"]
	if errEl == nil {
		t.Fatal("the served page has no error element to carry the refusal")
	}
	if menuHasAttr(errEl, "hidden") {
		t.Error("the refused tap announced nothing — the buyer taps a dish and nothing happens")
	}
	got := strings.TrimSpace(nodeText(errEl))
	if !strings.Contains(got, formLangs["en"].MenuSoldOut) {
		t.Errorf("the refusal reads %q, want the localised sold-out message %q", got, formLangs["en"].MenuSoldOut)
	}
	if !strings.Contains(got, "Nasi Lemak Kosong") {
		t.Errorf("the refusal does not name the dish (%q), so the buyer cannot tell which one could not be added", got)
	}
	// The sheet carries the message, so it must be open for it to be read.
	if sheet := d.byID["menuSheet"]; sheet != nil && menuHasAttr(sheet, "hidden") {
		t.Error("the basket sheet stayed closed, so a refusal written into it is never seen")
	}
}

// The control: the SAME tap with the server reporting the dish in stock must
// still add. Without this, a fix that refuses every simple tap would pass.
func TestMenuLiveStockInStockSimpleDishTapStillAdds(t *testing.T) {
	d, _ := menuSimpleDishPage(t)
	d.stockResponse = []map[string]any{liveStockPayload("p3", "", false)}

	before := d.stockReads
	d.dispatch("click", d.buttonFor("p3"))
	d.flushTimers()

	cart := d.cart()
	if len(cart) != 1 {
		t.Fatalf("an in-stock simple tap added %d basket line(s), want 1 — the live re-check refuses "+
			"buyable dishes\nbasket: %v", len(cart), cart)
	}
	if cart[0]["id"] != "p3" {
		t.Errorf("the added line carries product %v, want p3", cart[0]["id"])
	}
	// A simple line stays keyed on the product alone (no variation), which is
	// what the order payload and dropSoldOutLines both key on.
	if got, _ := cart[0]["vid"].(string); got != "" {
		t.Errorf("the simple line carries variation %q, want none", got)
	}
	if d.stockReads <= before {
		t.Error("the tap added without reading the live stock, so the guard above proves nothing")
	}
	errEl := d.byID["menuErr"]
	if errEl != nil && !menuHasAttr(errEl, "hidden") {
		t.Errorf("a successful add wrote a refusal message: %q", strings.TrimSpace(nodeText(errEl)))
	}
}

// The second control: a read that answers NOTHING must leave the menu usable.
// The rendered state is the fallback (there is no other guard for a simple
// dish), so an empty or failed read must not freeze every simple dish.
func TestMenuSimpleDishTapStillAddsWhenTheLiveReadIsEmpty(t *testing.T) {
	d, _ := menuSimpleDishPage(t)
	// No live-stock payload at all: the state a failed or blocked read leaves.
	d.stockResponse = nil

	d.dispatch("click", d.buttonFor("p3"))
	d.flushTimers()

	if got := len(d.cart()); got != 1 {
		t.Fatalf("a simple tap with no live answer added %d basket line(s), want 1 — an empty read "+
			"must leave the menu usable\nbasket: %v", got, d.cart())
	}
}

// A basket line for a SIMPLE dish must not survive a reload once the dish sold
// out: the basket outlives the page, and nothing re-reads the rendered markup
// for it. The payload carries the product's own row with variation_id:"", so
// the same re-check drops it — asserted rather than assumed, because this is
// the other half of the simple shape's exposure.
func TestMenuReloadDropsASimpleBasketLineThatWentSoldOut(t *testing.T) {
	d, _ := menuSimpleDishPage(t)
	d.stockResponse = []map[string]any{liveStockPayload("p3", "", false)}
	d.dispatch("click", d.buttonFor("p3"))
	d.flushTimers()
	if got := len(d.cart()); got != 1 {
		t.Fatalf("precondition failed: the in-stock add recorded %d line(s), want 1", got)
	}

	d2 := reloadMenuDOM(t, mustMenuBody(t, menuVariationFixture(t)), d.storage,
		[]map[string]any{liveStockPayload("p3", "", true)})

	if got := len(d2.cart()); got != 0 {
		t.Fatalf("the reloaded page kept %d basket line(s) for a simple dish that sold out — the buyer "+
			"checks out with it and only meets the 409\nbasket: %v", got, d2.cart())
	}
	errEl := d2.byID["menuErr"]
	if errEl == nil || menuHasAttr(errEl, "hidden") {
		t.Fatal("the dropped basket line was announced nowhere — the buyer's basket changed silently")
	}
	if got := strings.TrimSpace(nodeText(errEl)); !strings.Contains(got, "Nasi Lemak Kosong") {
		t.Errorf("the notice does not name the dropped dish (%q)", got)
	}
}

// mustMenuBody re-renders the fixture's served markup for a reload, so the test
// drives the server's own markup rather than a re-statement of it.
func mustMenuBody(t *testing.T, data workspaceMenuData) string {
	t.Helper()
	return renderMenu492(t, data)
}

// The live-stock read must be gated like the page it serves, and it must not
// answer for a form that is not a menu. Driven through the REAL route, so the
// registration, the gate and the payload shape are all covered.
func TestMenuStockRouteIsGatedLikeTheMenuPage(t *testing.T) {
	h, workspaceSlug, accountID, menuSlug, _, limitedVariationID := menuVariationIntegrationFixture(t)
	host := workspaceSlug + ".jomform.com"

	code, body := menuStockGet(t, h, host, menuSlug)
	if code != http.StatusOK {
		t.Fatalf("GET /menu-stock/{slug} = %d, want 200 (body %s)", code, body)
	}
	// The limited variant is in stock in the fixture, so it is reported buyable
	// — the control against an endpoint that simply answers "sold out".
	if !strings.Contains(body, `"variation_id":"`+limitedVariationID+`","sold_out":false`) {
		t.Errorf("the in-stock variant is not reported buyable: %s", body)
	}
	// Nothing here may be cached: a cached answer is the stale attribute again.
	rec := menuStockGetRec(t, h, host, menuSlug)
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Errorf("Cache-Control = %q, want no-store — a cached live-stock answer is the stale "+
			"render-time attribute this endpoint exists to replace", got)
	}
	// Prove the read is LIVE: sell the variant out and ask again. Without this,
	// an endpoint returning the rendered state would satisfy the shape above.
	if err := h.ProductVariations.DecrementStock(t.Context(), limitedVariationID, 1); err != nil {
		t.Fatalf("decrement variant stock: %v", err)
	}
	_, body2 := menuStockGet(t, h, host, menuSlug)
	if !strings.Contains(body2, `"variation_id":"`+limitedVariationID+`","sold_out":true`) {
		t.Errorf("a variant that just sold out is still reported buyable: %s", body2)
	}

	// A form that is NOT a menu is refused rather than answered with an empty
	// list, so this cannot become a stock reader for every form.
	plain := &dbstore.Form{
		AccountID: accountID, Slug: "it-plain-" + accountID[:8], Title: "RSVP", Status: "published",
		Config: map[string]any{},
	}
	if err := h.Forms.Create(t.Context(), plain); err != nil {
		t.Fatalf("create non-menu form: %v", err)
	}
	if code, body := menuStockGet(t, h, host, plain.Slug); code != http.StatusNotFound {
		t.Errorf("GET /menu-stock/{slug} for a non-menu form = %d, want 404 (body %s)", code, body)
	}
	// A form slug that does not belong to the Host's workspace cannot be read
	// through this route.
	if code, _ := menuStockGet(t, h, "someone-else.jomform.com", menuSlug); code != http.StatusNotFound {
		t.Errorf("GET /menu-stock/{slug} on a foreign workspace host = %d, want 404", code)
	}
	// An unknown slug is not found.
	if code, _ := menuStockGet(t, h, host, "no-such-form"); code != http.StatusNotFound {
		t.Errorf("GET /menu-stock/{slug} for an unknown form = %d, want 404", code)
	}
}

// A DRAFT form is never publicly served, so its stock is not public either.
func TestMenuStockRouteRefusesADraftForm(t *testing.T) {
	h, workspaceSlug, accountID, _, prodID, _ := menuVariationIntegrationFixture(t)
	host := workspaceSlug + ".jomform.com"

	f := createMenuForm(t, h, accountID, "it-draft-"+accountID[:8], "Draft menu", []string{prodID})
	if err := h.Forms.SetStatus(t.Context(), accountID, f.ID, dbstore.FormStatusDraft); err != nil {
		t.Fatalf("set draft: %v", err)
	}
	if code, body := menuStockGet(t, h, host, f.Slug); code != http.StatusNotFound {
		t.Errorf("GET /menu-stock/{slug} for a draft menu = %d, want 404 (body %s)", code, body)
	}
}

// menuStockGet drives the real route and returns the status + body.
func menuStockGet(t *testing.T, h *PublicHandlers, host, slug string) (int, string) {
	t.Helper()
	rec := menuStockGetRec(t, h, host, slug)
	return rec.Code, rec.Body.String()
}

// menuStockGetRec drives the real GET /menu-stock/{slug} route through the
// public mux, so the guard covers the registration and not just the handler.
func menuStockGetRec(t *testing.T, h *PublicHandlers, host, slug string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/menu-stock/"+slug, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// reloadMenuDOM builds a fresh page from the SAME served markup — the way a
// browser reload does — and seeds it with the localStorage the previous page
// left behind. The live-stock payload is installed BEFORE the script runs,
// because the page asks on open.
//
// The document is re-parsed rather than reused, so the new page's attributes are
// the server's render-time values again: that is what makes the reload case the
// stale-attribute case.
func reloadMenuDOM(t *testing.T, body string, storage map[string]string, stock []map[string]any) *menuDOM {
	t.Helper()
	d := newMenuDOM(t, body)
	for k, v := range storage {
		d.storage[k] = v
	}
	d.stockResponse = stock
	d.run(menuPageScript(t, body))
	d.flushTimers()
	return d
}
