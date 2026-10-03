package httpapi

// The three gaps the row-attribution fix (t_3a593996) left behind in the menu
// page's inline cart script. Each guard below drives the page's OWN script (the
// same goja + cascadia harness the row tests use) and goes RED when its
// behaviour is removed — see api/scripts/biteproof_menu_variant_gaps.py.
//
//  1. flagPick/clearPick must resolve the pick hint through the selector's own
//     CONTAINER, not through parentNode. Parent-node happens to work only
//     because the template renders .mvarstock as a direct sibling of the
//     <select>; anything inserted between the two made the hint silently
//     vanish — the same "reach out of your own container" class of bug the tap
//     and change paths already had (menu.go varOf closest('.item')).
//
//  2. A variant pick must re-check sold-out. The page is JS-driven and stays
//     open: `disabled` is rendered at page load, so a pick made after a stock
//     change (or a browser-restored selection on reload) would land an
//     unbuyable line in the basket and only fail at checkout with 409
//     "stok habis".
//
//  3. The menu cart script must not match the ordinary form renderer's
//     selectors. Both surfaces render `select.v` + `.varwrap` + `.varstock`
//     and both listen on the document, and the menu's products ARE the
//     attached form's product blocks — so on a shared document (embed /
//     landing shell) each script drove the other's selectors.
//
// Scope, stated plainly: the shim is not a browser (see menu_dom_test.go). The
// live-page check covers what it cannot.

import (
	"bytes"
	"html/template"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// wrapMenuSelectorAndHint inserts an extra element between one product's
// variant <select> and its hint, leaving .mvarwrap as their nearest common
// ancestor. That is the future markup change a parentNode lookup cannot
// survive, and it keeps the page otherwise identical to the served one.
func wrapMenuSelectorAndHint(t *testing.T, body, productID string) string {
	t.Helper()
	open := `<div class="mvarwrap"><select class="mvar" data-id="` + productID + `"`
	if n := strings.Count(body, open); n != 1 {
		t.Fatalf("reshape: the selector for %s occurs %d times in the served page, want exactly 1", productID, n)
	}
	idx := strings.Index(body, open)
	selStart := idx + len(`<div class="mvarwrap">`)

	closeIdx := strings.Index(body[selStart:], "</select>")
	if closeIdx < 0 {
		t.Fatalf("reshape: no </select> for %s", productID)
	}
	closeIdx += selStart
	hintStart := closeIdx + len("</select>")
	if !strings.HasPrefix(body[hintStart:], `<div class="mvarstock"`) {
		t.Fatalf("reshape: the element right after %s's </select> is not its hint, so this page cannot be reshaped this way: %.60q",
			productID, body[hintStart:])
	}
	// The extra element closes BEFORE the hint: the hint stays a child of
	// .mvarwrap, a sibling of the wrapper holding the selector — so a
	// parentNode lookup from the selector reaches the wrapper, not the hint.
	return body[:selStart] + `<div class="extra">` + body[selStart:hintStart] + `</div>` + body[hintStart:]
}

// reshapeMenuHTML inserts an extra wrapper element around a substring of the
// served page.
func reshapeMenuHTML(t *testing.T, body, needle, open, close string) string {
	t.Helper()
	idx := strings.Index(body, needle)
	if idx < 0 {
		t.Fatalf("reshape: %q not found in the served page", needle)
	}
	if strings.Count(body, needle) != 1 {
		t.Fatalf("reshape: %q occurs %d times, want exactly 1", needle, strings.Count(body, needle))
	}
	return body[:idx] + open + needle + close + body[idx+len(needle):]
}

// ---------------------------------------------------------------------------
// GAP 1 — the hint lookup must not depend on parentNode.
// ---------------------------------------------------------------------------

// THE DEFECT: on a page where the item's selector and its hint are separated by
// a wrapper (perfectly valid markup — a styling <span>, an extra div, any
// future refactor), a no-choice tap must STILL reveal the item's own hint. With
// the lookup done through parentNode the hint is one level further down, so it
// resolves to nothing: the tap flags the selector red but shows the buyer no
// reason.
func TestMenuVariantHintSurvivesAnExtraWrapper(t *testing.T) {
	// Wrap the selector in an extra element, so the hint (still a sibling of
	// the selector inside .mvarwrap) is NOT reachable through parentNode.
	body := wrapMenuSelectorAndHint(t, renderMenu492(t, menuTwoRowsFixture(t)), "dish-id")

	d, _ := newMenuDOMPageFrom(t, body)
	sel := d.selectorFor("dish-id")
	if sel == nil {
		t.Fatal("the reshaped page lost the dish's variant selector")
	}

	// The hint is still reachable from the selector through its own container
	// — so a page that resolves it that way can find it.
	if h := d.hintFor(sel); h == nil {
		t.Fatal("hintFor could not reach the hint through the selector's container — the reshape is wrong, not the page")
	}

	d.dispatch("click", d.buttonFor("dish-id"))

	if !menuHasClass(sel, "missing") {
		t.Error("the no-choice tap did not flag the tapped item's selector")
	}
	hint := d.hintFor(sel)
	if hint == nil {
		t.Fatal("the reshaped page has no hint for the tapped item")
	}
	if menuHasAttr(hint, "hidden") {
		t.Error("the tap on the item did not reveal its own pick hint — the hint lookup missed it (parentNode reaches only the wrapper inserted between selector and hint)")
	}
	if got := strings.TrimSpace(nodeText(hint)); got == "" {
		t.Error("the revealed hint is empty, so it explains nothing to the buyer")
	}
	if got := len(d.cart()); got != 0 {
		t.Fatalf("a no-choice tap added %d basket line(s); it must add none", got)
	}
}

// The control for the guard above: on the UNCHANGED template the hint is also
// revealed — so a fix that only works on reshaped markup cannot pass, and a
// regression on the shipped shape is caught too.
func TestMenuVariantHintRevealedOnTheShippedShape(t *testing.T) {
	d, body := newMenuDOMPage(t, menuTwoRowsFixture(t))
	sel := d.selectorFor("dish-id")
	if sel == nil {
		t.Fatalf("the second row has no selector\nbody:\n%s", body)
	}
	d.dispatch("click", d.buttonFor("dish-id"))
	hint := d.hintFor(sel)
	if hint == nil || menuHasAttr(hint, "hidden") {
		t.Fatal("tapping a variable item did not reveal its pick hint on the shipped markup")
	}
	if !menuHasClass(sel, "missing") {
		t.Error("the tap did not flag the selector")
	}
}

// clearPick must reach the hint the same way: after a valid choice the hint is
// hidden again even when the shape changed. Without this, a wrapper would leave
// a stale "choose a variation" message under a line that was already added.
func TestMenuVariantHintClearsThroughTheContainer(t *testing.T) {
	body := wrapMenuSelectorAndHint(t, renderMenu492(t, menuTwoRowsFixture(t)), "dish-id")

	d, _ := newMenuDOMPageFrom(t, body)
	sel := d.selectorFor("dish-id")
	if sel == nil {
		t.Fatal("the reshaped page lost the dish's variant selector")
	}
	// Flag it first, the way a no-choice tap does.
	d.dispatch("click", d.buttonFor("dish-id"))
	hint := d.hintFor(sel)
	if hint == nil || menuHasAttr(hint, "hidden") {
		t.Fatal("precondition failed: the tap did not reveal the hint")
	}

	d.chooseVariant(sel, "dish-var")
	d.dispatch("change", sel)

	if got := len(d.cart()); got != 1 {
		t.Fatalf("choosing a variant left %d basket line(s), want 1", got)
	}
	hint = d.hintFor(sel)
	if hint != nil && !menuHasAttr(hint, "hidden") {
		t.Error("the pick hint stayed visible after a variant was chosen — the clear lookup missed it")
	}
	if menuHasClass(sel, "missing") {
		t.Error("the selector is still flagged after a variant was chosen")
	}
	// The hidden hint must still carry the rendered "choose a variation"
	// prompt, not whatever a previous refusal wrote into it: a later no-choice
	// tap reuses this element, and its text is what the buyer reads.
	if hint != nil {
		if want := menuAttr(hint, "data-hint"); strings.TrimSpace(nodeText(hint)) != strings.TrimSpace(want) {
			t.Errorf("the cleared hint carries %q, want the rendered prompt %q — a later no-choice tap would show a stale message",
				nodeText(hint), want)
		}
	}
}

// ---------------------------------------------------------------------------
// GAP 2 — a pick on a sold-out variant must not land in the basket.
// ---------------------------------------------------------------------------

// THE DEFECT: `disabled` is the no-JS guard, but this page is JS-driven and
// stays open. A variant that sells out after the page rendered still fires
// change when picked (or when the browser restores it on reload), and the line
// went into the basket — surfacing only at checkout as 409 "stok habis".
//
// The option is disabled in the SERVED markup here, which is exactly the state
// a stale open page holds: the attribute was correct at render time and the
// buyer picks it anyway.
func TestMenuSoldOutVariantPickIsRefused(t *testing.T) {
	data := menuTwoRowsFixture(t)
	// Make the second dish's only variant sold out, the way the server renders
	// a variant whose stock has reached zero.
	zero := 0
	data.Categories[0].Products[1].Variations[0].Stock = &zero
	data.Categories[0].Products[1].Variations[0].SoldOut = true

	d, body := newMenuDOMPage(t, data)
	sel := d.selectorFor("dish-id")
	if sel == nil {
		t.Fatalf("the sold-out dish has no selector\nbody:\n%s", body)
	}
	opts := optionNodes(sel)
	if len(opts) != 2 {
		t.Fatalf("selector offers %d options, want the placeholder + 1 variant", len(opts))
	}
	variantOpt := opts[1]
	if !menuHasAttr(variantOpt, "disabled") {
		t.Fatalf("precondition failed: the sold-out variant is not rendered disabled\nbody:\n%s", body)
	}

	// The buyer picks it anyway — the browser selects a disabled option only
	// under programmatic/restored input, which is the real scenario on a page
	// that has been open across a stock change.
	d.chooseVariant(sel, "dish-var")
	d.dispatch("change", sel)

	if got := len(d.cart()); got != 0 {
		t.Fatalf("picking a sold-out variant added %d basket line(s) — the basket holds a line the order endpoint refuses with 409 stok habis\nbasket: %v", got, d.cart())
	}
	if !menuHasClass(sel, "missing") {
		t.Error("the refused pick did not flag the selector, so the buyer got no feedback")
	}
	hint := d.hintFor(sel)
	if hint == nil {
		t.Fatal("the sold-out item has no hint to explain the refusal")
	}
	if menuHasAttr(hint, "hidden") {
		t.Error("the refusal left the hint hidden, so nothing tells the buyer why the pick was refused")
	}
	if got := strings.TrimSpace(nodeText(hint)); got == "" {
		t.Error("the refusal hint is empty")
	}
}

// The control: an IN-STOCK pick on the same fixture still adds, so the guard
// above cannot be satisfied by refusing every pick.
func TestMenuInStockVariantPickStillAdds(t *testing.T) {
	d, body := newMenuDOMPage(t, menuTwoRowsFixture(t))
	sel := d.selectorFor("dish-id")
	if sel == nil {
		t.Fatalf("the dish has no selector\nbody:\n%s", body)
	}
	d.chooseVariant(sel, "dish-var")
	d.dispatch("change", sel)

	cart := d.cart()
	if len(cart) != 1 {
		t.Fatalf("an in-stock pick added %d basket line(s), want 1 — the sold-out guard refuses buyable variants", len(cart))
	}
	if cart[0]["id"] != "dish-id" || cart[0]["vid"] != "dish-var" {
		t.Errorf("basket line = %v, want dish-id/dish-var", cart[0])
	}
	if menuHasClass(sel, "missing") {
		t.Error("an accepted pick left the selector flagged")
	}
}

// A tap on the CARD of a sold-out item (the same selector, reached through the
// other path) must be refused too — the guard belongs to both add paths, not
// only the change handler.
func TestMenuSoldOutVariantCardTapIsRefused(t *testing.T) {
	data := menuTwoRowsFixture(t)
	zero := 0
	data.Categories[0].Products[1].Variations[0].Stock = &zero
	data.Categories[0].Products[1].Variations[0].SoldOut = true

	d, _ := newMenuDOMPage(t, data)
	sel := d.selectorFor("dish-id")
	if sel == nil {
		t.Fatal("the sold-out dish has no selector")
	}
	d.chooseVariant(sel, "dish-var")
	// No change event: the buyer picked programmatically (restored selection)
	// and taps the card to add.
	d.dispatch("click", d.buttonFor("dish-id"))

	if got := len(d.cart()); got != 0 {
		t.Fatalf("tapping the card of a sold-out variant added %d basket line(s), want 0", got)
	}
}

// ---------------------------------------------------------------------------
// GAP 3 — the menu script must not see (or drive) the form renderer's
// selectors, and vice versa.
// ---------------------------------------------------------------------------

// THE DEFECT: the menu page and the ordinary block form both rendered
// `select.v` + `.varwrap` and both registered a document-level `change`
// listener keyed on that class. On a document carrying both surfaces — the menu
// markup embedded in an embed/landing shell that also renders the form, which
// is the realistic case because the menu's products ARE that form's product
// blocks, so the same data-id appears on both — each script answered the
// other's selectors.
//
// The form renderer's own price lookup is the sharpest observable: it is a
// document-wide `select.v[data-id=...]` query, so the MENU's selector answered
// it and the form's running total followed the menu's variant price.
//
// The two fixtures deliberately share one product id and carry DIFFERENT
// variant prices, so "which selector answered" is readable off the total.
func TestMenuCartDoesNotDriveTheFormRenderersTotal(t *testing.T) {
	menuBody, formBody := sharedDocumentSurfaces(t)
	menuVariantPrice, formProductPrice := int64(5000), int64(1000)

	d := newPageDOM(t, menuBody+formBody)
	d.runPageScript(menuBody, "menuSheet")
	d.runPageScript(formBody, "getElementById('of')")

	// Check the form's product box and let the form's own script price it.
	box := d.find(func(n *html.Node) bool {
		return n.Data == "input" && menuHasClass(n, "p") && menuAttr(n, "data-id") == sharedProductID
	})
	if box == nil {
		t.Fatal("precondition failed: the serving fixture is missing something the assertion below needs — " + "the block form's product checkbox is not on the combined document")
	}
	setMenuAttr(box, "checked", "")
	d.dispatch("change", box)
	if got := d.formTotal(); got != "RM10.00" {
		t.Fatalf("precondition failed: the form's total after checking its %d-sen product = %q, want RM10.00", formProductPrice, got)
	}

	// Now pick a variant on the MENU's own selector. That is the menu's
	// business, and nothing the form's script should react to.
	sel := d.selectorFor(sharedProductID)
	if sel == nil {
		t.Fatal("precondition failed: the serving fixture is missing something the assertion below needs — " + "the menu's variant selector is not on the combined document")
	}
	d.chooseVariant(sel, sharedMenuVariantID)
	d.dispatch("change", sel)

	// The menu cart did its job…
	cart := d.cart()
	if len(cart) != 1 || cart[0]["id"] != sharedProductID || cart[0]["vid"] != sharedMenuVariantID {
		t.Fatalf("the menu's own pick was not recorded: basket = %v", cart)
	}
	// …and the form's total did NOT follow the menu's %d-sen variant.
	if got := d.formTotal(); got != "RM10.00" {
		t.Errorf("the form renderer's total = %q after a pick on the MENU's selector, want RM10.00 (its own %d-sen product) — the form's script matched a selector the menu rendered, so it priced the form from the menu's variant (%d sen)",
			got, formProductPrice, menuVariantPrice)
	}
}

// The other half of the same collision: the form's submit resolves its
// variation the same document-wide way, so with a shared class it posted the
// MENU's chosen variant id into the form's order — a line the buyer never
// chose on that form.
func TestFormSubmitIsNotDrivenByTheMenusSelector(t *testing.T) {
	menuBody, formBody := sharedDocumentSurfaces(t)

	d := newPageDOM(t, menuBody+formBody)
	d.runPageScript(menuBody, "menuSheet")
	d.runPageScript(formBody, "getElementById('of')")
	// A successful order answers with a checkout URL (the block script's own
	// success path), so the submit under test is the one that POSTs.
	d.orderResponse = map[string]any{"checkout_url": "https://example.test/pay"}

	// Both surfaces have a variant chosen, so neither legitimately blocks.
	menuSel := d.selectorFor(sharedProductID)
	if menuSel == nil {
		t.Fatal("precondition failed: the serving fixture is missing something the assertion below needs — " + "the menu's variant selector is not on the combined document")
	}
	d.chooseVariant(menuSel, sharedMenuVariantID)
	d.dispatch("change", menuSel)

	formSel := d.find(func(n *html.Node) bool {
		return n.Data == "select" && menuHasClass(n, "v") && menuAttr(n, "data-id") == sharedProductID
	})
	if formSel == nil {
		t.Fatal("precondition failed: the serving fixture is missing something the assertion below needs — " + "the block form's variation selector is not on the combined document")
	}
	d.chooseVariant(formSel, sharedFormVariantID)

	box := d.find(func(n *html.Node) bool {
		return n.Data == "input" && menuHasClass(n, "p") && menuAttr(n, "data-id") == sharedProductID
	})
	if box == nil {
		t.Fatal("precondition failed: the serving fixture is missing something the assertion below needs — " + "the block form's product checkbox is not on the combined document")
	}
	setMenuAttr(box, "checked", "")

	body := d.submitBlockForm()
	pid, vid := postedItem(body)
	if pid != sharedProductID {
		t.Fatalf("the form's order carries product %q, want %q\nbody: %s", pid, sharedProductID, body)
	}
	if vid != sharedFormVariantID {
		t.Errorf("the form's order carries variation_id %q, want the FORM's own %q — the form's script resolved the variation through the document-wide selector and picked up the MENU's choice\nbody: %s",
			vid, sharedFormVariantID, body)
	}
}

// The structural half of the contract, asserted directly: neither surface may
// emit the other's selector class. This is what makes the two tests above pass
// by construction rather than by listener ordering.
func TestMenuAndFormSelectorContractsAreDisjoint(t *testing.T) {
	menuBody, formBody := sharedDocumentSurfaces(t)

	for _, bad := range []string{`<select class="v" `, `<select class="v">`, `class="varwrap"`, `class="varstock"`} {
		if strings.Contains(menuBody, bad) {
			t.Errorf("the menu page emits the ordinary form renderer's selector markup (%q)", bad)
		}
	}
	for _, bad := range []string{`<select class="mvar"`, `class="mvarwrap"`, `class="mvarstock"`} {
		if strings.Contains(formBody, bad) {
			t.Errorf("the form renderer emits the menu's own selector markup (%q)", bad)
		}
	}
	// …and the menu's own contract is the one it matches on.
	for _, want := range []string{`<select class="mvar" data-id="` + sharedProductID + `"`, `class="mvarwrap"`, `class="mvarstock"`} {
		if !strings.Contains(menuBody, want) {
			t.Errorf("the menu page is missing its own selector contract (%q)", want)
		}
	}
	// The script matches the menu's class, never the form renderer's.
	if strings.Contains(menuBody, `classList.contains('v')`) {
		t.Error("the menu cart script still filters change events on the form renderer's bare v class")
	}
}

// The menu side of the direction: a pick on the FORM renderer's selector must
// leave the menu's basket, its own selectors and its own hints untouched.
//
// This one is the sharpest bite of the three. The form renderer's selector sits
// inside the form's own product container, and the menu's change handler
// resolves its host from `.item` (absent there) with a fallback to
// previousElementSibling — which IS the form's checkbox. So with a shared
// selector class the menu cart answered the form's selector directly and put
// the form's product in the MENU's basket.
func TestMenuBasketIgnoresTheFormRenderersSelectors(t *testing.T) {
	menuBody, formBody := sharedDocumentSurfaces(t)

	d := newPageDOM(t, menuBody+formBody)
	d.runPageScript(menuBody, "menuSheet")
	d.runPageScript(formBody, "getElementById('of')")

	formSel := d.find(func(n *html.Node) bool {
		return n.Data == "select" && menuHasClass(n, "v") && menuAttr(n, "data-id") == sharedProductID
	})
	if formSel == nil {
		t.Fatal("precondition failed: the serving fixture is missing something the assertion below needs — " + "the block form's variation selector is not on the combined document")
	}
	d.chooseVariant(formSel, sharedFormVariantID)
	d.dispatch("change", formSel)

	if got := len(d.cart()); got != 0 {
		t.Fatalf("a pick on the FORM renderer's selector added %d line(s) to the MENU basket — the menu cart answered a selector class it does not own\nbasket: %v", got, d.cart())
	}
	for _, sel := range d.selectorsFor(sharedProductID) {
		if menuHasClass(sel, "missing") {
			t.Error("a pick on the FORM renderer's selector flagged the MENU's own selector")
		}
		if h := d.hintFor(sel); h != nil && !menuHasAttr(h, "hidden") {
			t.Error("a pick on the FORM renderer's selector revealed the MENU's own pick hint")
		}
	}
}

// The converse direction: the FORM renderer's listener must not answer a pick
// on the MENU's selector. The form script recalculates its total on any change,
// and its price lookup is document-wide, so with a shared selector class a menu
// pick re-priced the form from the menu's variant.
//
// (The menu's own selector resolves no host in the form's container, so the
// form's script has nothing to add here; the observable is its TOTAL.)
func TestFormRendererIgnoresAMenuPick(t *testing.T) {
	menuBody, formBody := sharedDocumentSurfaces(t)

	d := newPageDOM(t, menuBody+formBody)
	d.runPageScript(menuBody, "menuSheet")
	d.runPageScript(formBody, "getElementById('of')")

	box := d.find(func(n *html.Node) bool {
		return n.Data == "input" && menuHasClass(n, "p") && menuAttr(n, "data-id") == sharedProductID
	})
	if box == nil {
		t.Fatal("precondition failed: the serving fixture is missing something the assertion below needs — " + "the block form's product checkbox is not on the combined document")
	}
	setMenuAttr(box, "checked", "")
	d.dispatch("change", box)
	if got := d.formTotal(); got != "RM10.00" {
		t.Fatalf("precondition failed: the form's total = %q, want RM10.00", got)
	}

	sel := d.selectorFor(sharedProductID)
	if sel == nil {
		t.Fatal("precondition failed: the serving fixture is missing something the assertion below needs — " + "the menu's variant selector is not on the combined document")
	}
	d.chooseVariant(sel, sharedMenuVariantID)
	d.dispatch("change", sel)

	if got := d.formTotal(); got != "RM10.00" {
		t.Errorf("the form renderer's total = %q after a pick on the MENU's selector — it answered a change event for a selector the menu rendered", got)
	}
	// The menu's own pick still has to work, so the guard above cannot be
	// satisfied by breaking the menu.
	if got := len(d.cart()); got != 1 {
		t.Errorf("the menu's own pick recorded %d basket line(s), want 1", got)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// The two surfaces of a shared document: the restaurant menu and the ordinary
// block form that renders the same product (the shape a landing/embed shell
// produces). Both carry sharedProductID, with deliberately different variant
// prices so the tests can tell which selector answered.
const (
	sharedProductID     = "shared-dish"
	sharedMenuVariantID = "menu-size"
	sharedFormVariantID = "form-size"
)

func sharedDocumentSurfaces(t *testing.T) (menuBody, formBody string) {
	t.Helper()
	data := menuTwoRowsFixture(t)
	// One variable dish with the shared id, at a price the form does not use.
	stock := 5
	ls := formLangs["en"]
	dish := data.Categories[0].Products[1]
	dish.ID = sharedProductID
	dish.Variations = []variationView{
		{ID: sharedMenuVariantID, Name: "Menu Size", PriceSen: 5000, Stock: &stock, Price: "50.00"},
	}
	dish.PriceSen = 5000
	dish.Price = "50.00"
	dish.Variable = true
	dish.SelectLabel = ls.SelectVariation
	dish.InStockLabel = ls.InStock
	dish.PickLabel = ls.SelectVariationAlert
	data.Categories[0].Products = []menuProductView{dish}
	data.BestSellers = nil
	menuBody = renderMenu492(t, data)

	formBody = renderBlockFormPage(t, sharedProductID, 1000, []variationView{
		{ID: sharedFormVariantID, Name: "Form Size", PriceSen: 1000, Stock: &stock, Price: "10.00"},
	})
	return menuBody, formBody
}

// The hidden hint must still carry the rendered "choose a variation" prompt,
// not whatever a previous refusal wrote into it: a later no-choice tap reuses
// this element, and its text is what the buyer reads.
//
// The sequence is the one a real buyer produces: tap with no choice (prompt),
// pick the sold-out option (the refusal writes "Sold out" into that same hint),
// pick a buyable one (hint hidden), then tap with no choice again — and the
// hint must be the RENDERED prompt, never the leftover "Sold out".
func TestMenuHintTextIsNotLeftStaleByARefusal(t *testing.T) {
	data := menuTwoRowsFixture(t)
	zero := 0
	data.Categories[0].Products[1].Variations = append(
		data.Categories[0].Products[1].Variations,
		variationView{ID: "dish-sold", Name: "Habis", PriceSen: 900, Stock: &zero, SoldOut: true, Price: "9.00"},
	)
	d, body := newMenuDOMPage(t, data)
	sel := d.selectorFor("dish-id")
	if sel == nil {
		t.Fatalf("the dish has no selector\nbody:\n%s", body)
	}
	prompt := menuAttr(d.hintFor(sel), "data-hint")
	if prompt == "" {
		t.Fatal("the served hint carries no rendered prompt")
	}

	// (1) The refusal writes its own message into the hint.
	d.chooseVariant(sel, "dish-sold")
	d.dispatch("change", sel)
	if got := len(d.cart()); got != 0 {
		t.Fatalf("picking the sold-out variant added %d basket line(s), want 0", got)
	}
	if got := strings.TrimSpace(nodeText(d.hintFor(sel))); got == prompt {
		t.Fatalf("the refusal left the rendered prompt in place, so this guard cannot tell a stale message from a correct one")
	}

	// (2) A buyable pick clears the flag AND the hint it overrode must go back
	// to carrying the rendered prompt. The element is only hidden here, not
	// discarded: the next no-choice tap reuses this same node (flagPick writes
	// into it), and a hint still holding a refusal's text is read by whatever
	// reveals it — a screen reader walking the group, or the next caller that
	// unhides it.
	d.chooseVariant(sel, "dish-var")
	d.dispatch("change", sel)
	if got := len(d.cart()); got != 1 {
		t.Fatalf("the buyable pick recorded %d basket line(s), want 1", got)
	}
	cleared := d.hintFor(sel)
	if cleared == nil {
		t.Fatal("the served item has no hint")
	}
	if !menuHasAttr(cleared, "hidden") {
		t.Error("the hint stayed visible after a buyable pick was recorded")
	}
	if got := strings.TrimSpace(nodeText(cleared)); got != strings.TrimSpace(prompt) {
		t.Errorf("the cleared hint still carries %q, want the rendered prompt %q — the refusal's message outlived the refusal", got, prompt)
	}

	// (3) Back to no selection (the state a reloaded/stale page holds), then
	// the tap must show the rendered prompt again.
	for c := sel.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "option" {
			removeMenuAttr(c, "selected")
		}
	}
	setMenuAttr(sel, "value", "")
	d.dispatch("click", d.buttonFor("dish-id"))
	hint := d.hintFor(sel)
	if hint == nil || menuHasAttr(hint, "hidden") {
		t.Fatal("the no-choice tap after a valid pick did not reveal the hint")
	}
	if got := strings.TrimSpace(nodeText(hint)); got != strings.TrimSpace(prompt) {
		t.Errorf("the hint shows %q after a refusal was followed by a valid pick, want the rendered prompt %q — the buyer is told the wrong reason", got, prompt)
	}
	// …and the flag is back, so the buyer can see which item is unresolved.
	if !menuHasClass(sel, "missing") {
		t.Error("the no-choice tap did not flag the selector")
	}
}

// ---------------------------------------------------------------------------
// Every locale must carry the "sold out" message the refusal path shows. A
// blank one renders an empty hint, which is the same silent failure the guard
// above exists to prevent — so it is asserted directly rather than left to a
// completeness test that does not list this field.
func TestMenuSoldOutLabelInFiveLocales(t *testing.T) {
	if len(formLangs) != 5 {
		t.Fatalf("formLangs has %d entries, want 5", len(formLangs))
	}
	for code, ls := range formLangs {
		if strings.TrimSpace(ls.MenuSoldOut) == "" {
			t.Errorf("lang %q has an empty MenuSoldOut, so a refused sold-out pick shows nothing", code)
		}
	}
	if got := formLangs["en"].MenuSoldOut; got != "Sold out" {
		t.Errorf("en MenuSoldOut = %q, want Sold out", got)
	}
	// The refusal path must actually carry it into the page: the item's own
	// hint is what the buyer reads.
	if _, body := newMenuDOMPage(t, menuTwoRowsFixture(t)); !strings.Contains(body, `data-lbl-soldout="`+formLangs["en"].MenuSoldOut+`"`) {
		t.Error("the served menu page does not carry the sold-out message for the refusal path")
	}
}

// helpers
// ---------------------------------------------------------------------------

// renderBlockFormPage renders the ordinary block-form page (render_blocks.go's
// blockTmpl) with one product block. This is the surface whose selector
// contract used to collide with the menu's, so a shared-document test needs the
// REAL served markup and the REAL served script, not a re-statement of them.
//
// The block view type is local to renderBlocksForm, so it is re-declared here
// in the same shape the template expects (the technique variation_render_test.go
// uses for the same template).
func renderBlockFormPage(t *testing.T, productID string, priceSen int64, vars []variationView) string {
	t.Helper()
	type block struct {
		ID               string
		Type, Value, Src string
		ProductID        string
		PriceSen         int64
		Required         bool
		HasPrice         bool
		HasQuantity      bool
		CondJSON         string
		Image            string
		ImgSize          string
		ImgTop           bool
		BaseImage        string
		Variations       []variationView
		Style            string
	}
	var buf bytes.Buffer
	err := blockTmpl.Execute(&buf, map[string]any{
		"Slug": "demo", "Title": "Demo",
		"Blocks": []block{
			// The buyer fields the block script reads for client{}.
			{ID: "your-name", Type: "name", Value: "Name", Required: true},
			{ID: "your-email", Type: "email", Value: "Email", Required: true},
			{ID: "your-phone", Type: "phone", Value: "Phone"},
			{
				Type: "product", ProductID: productID, Value: "Shared Dish",
				PriceSen: priceSen, HasPrice: true, Variations: vars,
			},
		},
		"TrackHead": "", "ThemePrimary": "", "ThemeBg": "", "LogoURL": "",
		"Lang": "en", "Ls": formLangs["en"],
		"Footer":   footerInfo{},
		"AntiSpam": template.HTML(antiSpamRenderFields()),
	})
	if err != nil {
		t.Fatalf("blockTmpl.Execute: %v", err)
	}
	return buf.String()
}

// optionNodes returns a selector's <option> children in document order.
func optionNodes(sel *html.Node) []*html.Node {
	var out []*html.Node
	for c := sel.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "option" {
			out = append(out, c)
		}
	}
	return out
}

// nodeText is the concatenated text directly under a node.
func nodeText(n *html.Node) string {
	if n == nil {
		return ""
	}
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			sb.WriteString(c.Data)
		}
	}
	return sb.String()
}
