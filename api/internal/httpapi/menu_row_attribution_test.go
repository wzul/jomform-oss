package httpapi

// The ROW path of the restaurant menu: which dish a chosen variant is recorded
// against.
//
// A `.varwrap` (the variant <select> under an item) must resolve ITS OWN item.
// The #665 change handler did it the other way round: it took
// `wrap.parentNode.querySelector('.card[data-id],.row[data-id]')`, and because
// menuRow rendered its <button> and its .varwrap as direct siblings inside the
// category's <div class="rows"> (menuCard had a <div class="item"> wrapper,
// menuRow did not) that parentNode WAS the category container. `querySelector`
// then returned the FIRST row of the category, so a variant picked on any later
// row was recorded against the first dish — a basket line that can never be
// checked out (the order endpoint answers 400 "variasi tidak sah") and, when the
// two dishes' variants happen to share a price, a silently mis-priced sale.
//
// These tests drive the page's OWN script against the document the template
// renders, so they exercise the shipped code rather than a re-statement of it,
// and they assert on OUTCOMES (which product the basket line carries, which
// selector got flagged) — never on the shape of the fix, so any correct way of
// resolving the host passes.

import (
	"testing"

	"golang.org/x/net/html"
)

// menuTwoRowsFixture builds the data for one category holding two VARIABLE
// dishes, with the dish the buyer picks a variant on SECOND — the order the
// defect needs. Both are variable, so a selector resolving "the first variable
// item in this category" still answers a real product: that is what made the
// wrong attribution silent rather than an error.
//
// Both dishes use the same base price (780) and the same variant price, which
// is the dangerous shape — the basket looks plausible and only the recorded
// product id is wrong.
func menuTwoRowsFixture(t *testing.T) workspaceMenuData {
	t.Helper()
	d := menuVariationFixture(t)

	stock := 5
	h := &PublicHandlers{}
	mk := func(id, name, variationID, variantName string) menuProductView {
		ls := formLangs["en"]
		v := h.menuProductView(id, name, "", 780, "", false, ls.MenuBestBadge, ls.MenuAdd)
		v.Price = "7.80"
		v.Variations = []variationView{
			{ID: variationID, Name: variantName, PriceSen: 780, Stock: &stock, Price: "7.80"},
		}
		v.Variable = true
		v.SelectLabel = ls.SelectVariation
		v.InStockLabel = ls.InStock
		v.PickLabel = ls.SelectVariationAlert
		return v
	}
	first := mk("drink-id", "Teh Tarik", "drink-var", "Biasa")
	second := mk("dish-id", "Nasi Lemak Ayam", "dish-var", "Biasa")

	d.Categories = []menuCategoryView{{Name: "Makanan", Products: []menuProductView{first, second}}}
	d.BestSellers = nil
	d.HasItems = true
	return d
}

// Every dish on the row path must render inside its own item wrapper, with its
// own selector — without that there is no element a selector could resolve "its
// own" dish to, and no correct answer is reachable at all.
func TestMenuRowEachDishHasItsOwnHost(t *testing.T) {
	d, body := newMenuDOMPage(t, menuTwoRowsFixture(t))

	for _, tc := range []struct{ id, variant string }{
		{"drink-id", "drink-var"},
		{"dish-id", "dish-var"},
	} {
		sel := d.selectorFor(tc.id)
		if sel == nil {
			t.Fatalf("no variant selector for row %s\nbody:\n%s", tc.id, body)
		}
		item := itemOf(sel)
		if item == nil {
			t.Fatalf("row %s: its selector is not inside a per-item wrapper, so the selector cannot resolve the row that owns it", tc.id)
		}
		host := hostIn(item, ".card[data-id],.row[data-id]")
		if host == nil {
			t.Fatalf("row %s: no add button inside its own wrapper", tc.id)
		}
		if got := menuAttr(host, "data-id"); got != tc.id {
			t.Errorf("row %s: its wrapper hosts data-id=%q — a variant picked here would be attributed to another dish", tc.id, got)
		}
		if got := menuAttr(host, "class"); got != "row" && !menuHasClass(host, "row") {
			t.Errorf("row %s: host is not a list row (%q)", tc.id, got)
		}
	}

	// The rows must be separate elements in one category, in the configured
	// order — the defect is ordering-dependent.
	seen := d.rowIDsInCategory()
	if len(seen) != 2 || seen[0] != "drink-id" || seen[1] != "dish-id" {
		t.Fatalf("the category's rows are %v, want [drink-id dish-id] in that order", seen)
	}
}

// THE DEFECT: a variant chosen on the SECOND row's own selector must be
// recorded against that row's dish, and the basket line it produces must be the
// payload the checkout posts.
//
// The order endpoint answers an empty object here: no checkout_url and no error,
// so the page takes its failure path and keeps the basket, exactly as it does on
// a rejected line.
func TestMenuRowVariantIsAttributedToItsOwnDish(t *testing.T) {
	d, body := newMenuDOMPage(t, menuTwoRowsFixture(t))

	sel := d.selectorFor("dish-id")
	if sel == nil {
		t.Fatalf("the second row has no selector\nbody:\n%s", body)
	}
	d.chooseVariant(sel, "dish-var")
	d.dispatch("change", sel)

	cart := d.cart()
	if len(cart) != 1 {
		t.Fatalf("basket after choosing a variant on the second row = %v, want exactly one line", cart)
	}
	if got := cart[0]["id"]; got != "dish-id" {
		t.Errorf("basket line id = %v, want dish-id (the dish whose OWN selector fired) — it was attributed to another dish", got)
	}
	if got := cart[0]["vid"]; got != "dish-var" {
		t.Errorf("basket line variation_id = %v, want dish-var", got)
	}
	if got := cart[0]["name"]; got != "Nasi Lemak Ayam — Biasa" {
		t.Errorf("basket line name = %v, want 'Nasi Lemak Ayam — Biasa'", got)
	}

	// The checkout must post the same dish. A basket line the order endpoint
	// rejects with 400 is the buyer-visible half of this defect, so it is
	// asserted here as well as in the DB-backed integration test.
	if posted := d.postedItems(); len(posted) != 1 || posted[0] != "dish-id|dish-var" {
		t.Errorf("checkout posted items = %v, want [dish-id|dish-var]", posted)
	}
}

// The control from the QA report: with the variable dish FIRST, the same input
// records the correct dish. Kept so a fix that only works in one order cannot
// pass — and so a later regression in the ordering-independent case is caught.
func TestMenuRowVariantFirstRowStillRecordsItsOwnDish(t *testing.T) {
	data := menuTwoRowsFixture(t)
	// Swap, so the dish picked on is the first row.
	data.Categories[0].Products[0], data.Categories[0].Products[1] =
		data.Categories[0].Products[1], data.Categories[0].Products[0]

	d, body := newMenuDOMPage(t, data)
	sel := d.selectorFor("dish-id")
	if sel == nil {
		t.Fatalf("the variable row has no selector\nbody:\n%s", body)
	}
	d.chooseVariant(sel, "dish-var")
	d.dispatch("change", sel)

	cart := d.cart()
	if len(cart) != 1 || cart[0]["id"] != "dish-id" || cart[0]["vid"] != "dish-var" {
		t.Fatalf("basket = %v, want one line for dish-id/dish-var", cart)
	}
}

// A no-choice tap on a variable row must flag ITS OWN selector — the .missing
// border and the localised hint belong under the dish the buyer tapped — and
// nothing may be added.
//
// Two shapes are asserted. (a) Distinct dishes in one category: the tap flags
// the tapped row's selector and reveals only that row's hint. (b) The same dish
// rendered twice on the page (a best-seller card AND its category row): the tap
// on the ROW must flag the ROW's selector, not the first element on the page
// carrying that data-id — which is what the page-wide lookup did.
func TestMenuRowNoChoiceTapFlagsItsOwnSelector(t *testing.T) {
	d, body := newMenuDOMPage(t, menuTwoRowsFixture(t))

	for _, tc := range []struct{ id, other string }{
		{"drink-id", "dish-id"},
		{"dish-id", "drink-id"},
	} {
		if d.buttonFor(tc.id) == nil {
			t.Fatalf("no add button for %s\nbody:\n%s", tc.id, body)
		}
		d.dispatch("click", d.buttonFor(tc.id))

		own := d.selectorFor(tc.id)
		other := d.selectorFor(tc.other)
		if !menuHasClass(own, "missing") {
			t.Errorf("tapping the %s row did not flag its own selector", tc.id)
		}
		if menuHasClass(other, "missing") {
			t.Errorf("tapping the %s row flagged the OTHER dish's selector", tc.id)
		}
		// The hint lives in the tapped selector's own .varwrap (that is where
		// the page looks for it); the other dish's must stay hidden.
		if hint := hintOf(own); hint == nil || menuHasAttr(hint, "hidden") {
			t.Errorf("tapping the %s row did not reveal its own pick hint", tc.id)
		}
		if hint := hintOf(other); hint != nil && !menuHasAttr(hint, "hidden") {
			t.Errorf("tapping the %s row revealed the OTHER dish's pick hint", tc.id)
		}
		if got := len(d.cart()); got != 0 {
			t.Fatalf("a no-choice tap on the %s row added %d basket line(s); it must add none", tc.id, got)
		}
		// Reset through the page's OWN controls, so the in-memory basket and
		// localStorage stay in step for the next iteration: choosing a variant
		// clears the flag (and legitimately adds a line), then Clear empties it.
		d.chooseVariant(own, variantOf(tc.id))
		d.dispatch("change", own)
		if got := len(d.cart()); got != 1 {
			t.Fatalf("choosing a variant on the %s row left %d basket line(s), want 1", tc.id, got)
		}
		d.dispatch("click", d.byID["menuClear"])
		if got := len(d.cart()); got != 0 {
			t.Fatalf("Clear left %d basket line(s)", got)
		}
		if menuHasClass(own, "missing") {
			t.Errorf("the %s row's selector is still flagged after a variant was chosen", tc.id)
		}
	}
}

// The duplicate-render tap: one dish shown as both a card and a row, so there
// are two elements carrying its data-id. Tapping the ROW must flag the row's own
// selector and reveal the row's own hint.
func TestMenuRowDuplicateRenderTapFlagsTheTappedRender(t *testing.T) {
	data := menuTwoRowsFixture(t)
	data.BestSellers = []menuProductView{data.Categories[0].Products[1]} // Nasi Lemak: card + row
	d, body := newMenuDOMPage(t, data)

	sels := d.selectorsFor("dish-id")
	if len(sels) != 2 {
		t.Fatalf("the duplicated dish rendered %d selectors, want 2 (card + row)\nbody:\n%s", len(sels), body)
	}
	card, row := sels[0], sels[1]

	// The row sits under its own wrapper while the card keeps its own: tapping
	// the row must not reach the card.
	d.dispatch("click", d.rowButtonFor("dish-id"))
	if !menuHasClass(row, "missing") {
		t.Error("tapping the duplicated dish's ROW did not flag the row's own selector")
	}
	if menuHasClass(card, "missing") {
		t.Error("tapping the duplicated dish's ROW flagged the CARD's selector — the tap is attributed to the wrong render")
	}
	if hint := hintOf(row); hint == nil || menuHasAttr(hint, "hidden") {
		t.Error("tapping the ROW did not reveal the under-the-row hint")
	}
	if hint := hintOf(card); hint != nil && !menuHasAttr(hint, "hidden") {
		t.Error("tapping the ROW revealed the CARD's hint")
	}
	if got := len(d.cart()); got != 0 {
		t.Fatalf("a no-choice tap added %d basket line(s); it must add none", got)
	}
}

// hintOf returns the pick hint the page shows when the tapped item has no
// variant chosen: the .mvarstock inside the selector's own container.
func hintOf(sel *html.Node) *html.Node {
	if sel == nil {
		return nil
	}
	for n := sel; n != nil; n = n.Parent {
		if n.Type == html.ElementNode && menuHasClass(n, "mvarwrap") {
			return hostIn(n, ".mvarstock")
		}
	}
	if sel.Parent == nil {
		return nil
	}
	return hostIn(sel.Parent, ".mvarstock")
}

// A dish rendered twice on one page (a best-seller card AND its category row)
// must not cross-attribute either. This is the duplicate-render shape the
// page-wide data-id lookup mis-targeted, and the basket keys the two lines
// apart by variation.
func TestMenuRowDuplicateRenderDoesNotCrossAttribute(t *testing.T) {
	data := menuTwoRowsFixture(t)
	// The second dish also becomes a best-seller card.
	data.BestSellers = []menuProductView{data.Categories[0].Products[1]}

	d, body := newMenuDOMPage(t, data)
	sels := d.selectorsFor("dish-id")
	if len(sels) != 2 {
		t.Fatalf("the duplicated dish rendered %d selectors, want 2 (card + row)\nbody:\n%s", len(sels), body)
	}
	// The ROW's selector is the second render; pick on it.
	rowSel := sels[1]
	d.chooseVariant(rowSel, "dish-var")
	d.dispatch("change", rowSel)

	cart := d.cart()
	if len(cart) != 1 {
		t.Fatalf("basket = %v, want one line", cart)
	}
	if cart[0]["id"] != "dish-id" || cart[0]["vid"] != "dish-var" {
		t.Errorf("basket line = %v, want the duplicated dish's own product + variant", cart[0])
	}

	// …and picking on the CARD's selector records the same dish at the same
	// variant: one basket line, topped up to 2, not two lines and not a line
	// for a different product.
	d.chooseVariant(sels[0], "dish-var")
	d.dispatch("change", sels[0])
	cart = d.cart()
	if len(cart) != 1 {
		t.Fatalf("basket after picking on both renders = %v, want one line", cart)
	}
	if got := cart[0]["qty"]; got != int64(2) {
		t.Errorf("basket qty after picking the same dish+variant twice = %v, want 2", got)
	}
	if cart[0]["id"] != "dish-id" || cart[0]["vid"] != "dish-var" {
		t.Errorf("basket line = %v, want the duplicated dish's own product + variant", cart[0])
	}
}

func variantOf(productID string) string {
	if productID == "drink-id" {
		return "drink-var"
	}
	return "dish-var"
}
