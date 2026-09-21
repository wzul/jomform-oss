package httpapi

// Restaurant-menu cart: product VARIATIONS.
//
// The menu cart used to ignore variations entirely: menuProductView had no
// Variations field, the card markup had no selector, and the checkout payload
// shipped only {product_id,quantity} — so every menu order for a variable
// product priced from the product's BASE price and decremented the
// PRODUCT-level stock (order.go's else branch), while the buyer's chosen size
// was neither charged nor recorded. See menuVariationIntegrationFixture for
// the DB-backed proof against the real order core.
//
// These are pure template/helper tests (no DB): they pin that
//
//   - a product WITH variations renders a <select class="v"> on its menu card
//     and row, each option carrying its own integer-sen price, sold-out
//     options disabled;
//   - the card itself does not silently add at the base price (it carries the
//     localised prompt a variable item needs);
//   - the checkout payload sends variation_id;
//   - a product WITHOUT variations is byte-for-byte the #492 behaviour: no
//     selector, no prompt, and the base price still shipped;
//   - every new user-facing string exists in all 5 locales.

import (
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// menuVariationFixture builds the #485/#492 menu with one VARIABLE product in the
// best-seller cards and one variable product in a category row, plus one simple
// product as the control. The variation list is attached directly (the DB
// resolution is exercised by the integration test below).
//
// The labels are resolved at build time because that is what the page does:
// the item sub-templates receive the item view as their data and cannot reach
// the page-level $.Ls, so menuProductView/withVariations resolve them in Go.
// A test that swaps d.Ls afterwards therefore does NOT retranslate the items —
// build the fixture for the language you want to assert.
func menuVariationFixture(t *testing.T) workspaceMenuData {
	t.Helper()
	return menuVariationFixtureFor(t, formLangs["en"])
}

func menuVariationFixtureFor(t *testing.T, ls langStrings) workspaceMenuData {
	t.Helper()
	d := menu492Fixture(t)
	d.Ls = ls

	stockFive := 5
	stockZero := 0
	vars := []variationView{
		{ID: "var-m", Name: "Size M", PriceSen: 1200, Stock: &stockFive, Price: "12.00"},
		{ID: "var-l", Name: "Size L", PriceSen: 1300, Stock: &stockZero, SoldOut: true, Price: "13.00"},
	}

	h := &PublicHandlers{}
	variable := h.menuProductView("p1", "Nasi Lemak Ayam", "", 780, "", true, ls.MenuBestBadge, ls.MenuAdd)
	variable.Price = "7.80"
	variable.Variations = vars
	variable.Variable = true
	variable.SelectLabel = ls.SelectVariation
	variable.InStockLabel = ls.InStock
	variable.PickLabel = ls.SelectVariationAlert

	rowVar := h.menuProductView("p2", "Teh Tarik", "Panas", 350, "", false, ls.MenuBestBadge, ls.MenuAdd)
	rowVar.Price = "3.50"
	rowVar.Variations = vars
	rowVar.Variable = true
	rowVar.SelectLabel = ls.SelectVariation
	rowVar.InStockLabel = ls.InStock
	rowVar.PickLabel = ls.SelectVariationAlert

	simple := h.menuProductView("p3", "Nasi Lemak Kosong", "Tanpa lauk", 380, "", false, ls.MenuBestBadge, ls.MenuAdd)
	simple.Price = "3.80"

	d.BestSellers = []menuProductView{variable}
	d.Categories = []menuCategoryView{{Name: "Minuman", Products: []menuProductView{rowVar, simple}}}
	d.HasItems = true
	return d
}

// A variable menu product renders a variant selector with each variant's own
// price, and a sold-out variant is disabled — the same convention the ordinary
// form block renderer uses (render_blocks.go).
func TestMenuVariationMenuCardRendersVariationSelector(t *testing.T) {
	body := renderMenu492(t, menuVariationFixture(t))

	if !strings.Contains(body, `class="v"`) {
		t.Errorf("variable menu item has no variation selector\nbody:\n%s", body)
	}
	// The selector is bound to the product id, exactly like the form's.
	if !strings.Contains(body, `<select class="v" data-id="p1"`) {
		t.Error("card selector is not bound to the product id")
	}
	if !strings.Contains(body, `<select class="v" data-id="p2"`) {
		t.Error("row selector is not bound to the product id")
	}
	// Each option carries its own integer-sen price and its name.
	for _, want := range []string{
		`value="var-m" data-vname="Size M" data-price="1200"`,
		`value="var-l" data-vname="Size L" data-price="1300" disabled`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("variation option missing %q\nbody:\n%s", want, body)
		}
	}
	// The per-variant price is shown next to the option (display only; the
	// server re-reads the price).
	if !strings.Contains(body, "Size M — RM 12.00") {
		t.Error("option does not show the variant's own price")
	}
	// The selector is a sibling of the button, never inside it: a <select>
	// nested in a <button> is invalid HTML and the browser drops it.
	cardEnd := strings.Index(body, "</button>")
	selStart := strings.Index(body, `<select class="v" data-id="p1"`)
	if cardEnd < 0 || selStart < 0 || selStart < cardEnd {
		t.Error("the variation selector is not rendered after its button")
	}
}

// The card tap on a variable item cannot decide a variant, so the card carries
// the localised prompt and the script refuses to add without a chosen variant.
func TestMenuVariationVariableItemAsksForAVariant(t *testing.T) {
	body := renderMenu492(t, menuVariationFixture(t))

	// The prompt rides on the card so the script can show it (the item
	// sub-template cannot reach the page-level $.Ls).
	if !strings.Contains(body, `data-pick="Please choose a variation for each product"`) {
		t.Errorf("variable card does not carry the localised pick prompt\nbody:\n%s", body)
	}
	// A simple item must NOT carry it — otherwise the control is meaningless.
	r := renderMenu492(t, menuVariationFixture(t))
	idx := strings.Index(r, "Nasi Lemak Kosong")
	rowStart := strings.LastIndex(r[:idx], `<button class="row"`)
	gt := strings.Index(r[rowStart:], ">")
	simpleTag := r[rowStart : rowStart+gt+1]
	if strings.Contains(simpleTag, "data-pick") {
		t.Errorf("a simple item carries the variation prompt: %s", simpleTag)
	}

	// The script must gate the add on the selector's value. The gate is
	// asserted as an ANCHORED adjacency (the lookup of the TAPPED item's own
	// selector, then the branch that refuses without a choice) — asserting the
	// inner line alone would still pass if the lookup were removed and the
	// selector never consulted, which is the failure mode this guard exists to
	// prevent.
	for _, want := range []string{
		"var sel=varOf(el);\n    if(sel){",
		"if(!sel.value){ flagPick(sel); return; }",
		"sel.classList.add('missing')",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("cart script does not gate the add on a chosen variant (%q missing)", want)
		}
	}
	// The lookup must exist and resolve the TAPPED item's own selector in the
	// live DOM — a stub that returns null would silently restore the base-price
	// add for a variable item, and a page-wide data-id lookup would attribute
	// the variant to the first element carrying that id instead of the item
	// that was tapped. The assertion is on that CONTRACT (the element is what
	// gets resolved, and no page-wide id query survives) rather than on the
	// exact expression, so any correct way of finding the item's own selector
	// passes.
	if !strings.Contains(body, "varOf(el)") {
		t.Error("the script does not resolve the variation selector from the element that was tapped")
	}
	if strings.Contains(body, `querySelectorAll('select.v[data-id="`) {
		t.Error("the script resolves a variation selector by a page-wide data-id query, which returns the first element carrying that id instead of the tapped item's own")
	}
	// The missing-choice hint is rendered hidden and has the localised text.
	if !strings.Contains(body, `class="varstock" data-hint="Please choose a variation for each product" hidden`) {
		t.Error("the missing-choice hint is not rendered (hidden) for a variable item")
	}
}

// The chosen variant — not the product's base price — is what the basket holds
// and what the checkout payload carries.
func TestMenuVariationCheckoutPayloadCarriesVariationID(t *testing.T) {
	body := renderMenu492(t, menuVariationFixture(t))

	if !strings.Contains(body, "product_id:it.id,variation_id:it.vid||'',quantity:it.qty") {
		t.Errorf("checkout payload does not send variation_id\nbody:\n%s", body)
	}
	// The basket is keyed by product + variation, so two sizes of one dish are
	// two lines with their own prices.
	if !strings.Contains(body, "function keyOf(id,vid){ return vid?id+'::'+vid:id; }") {
		t.Error("basket lines are not keyed by product + variation")
	}
	// The line is priced from the chosen option, and named with the variant.
	for _, want := range []string{
		"o.getAttribute('data-price'),sel.value,o.getAttribute('data-vname')",
		"name:vname?(name+' — '+vname):name",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("basket line does not take the variant price/name (%q missing)", want)
		}
	}
}

// The control: an ordinary (no-variation) menu product is unchanged — no
// selector, no prompt, and the base price still shipped. This is the
// TestIssue492RegularFormsUnaffected precedent for the variant change.
func TestMenuVariationSimpleMenuProductUnchanged(t *testing.T) {
	// A simple menu product renders NO variation markup. (The `varwrap` CSS
	// rule is always in the stylesheet, so the check is on rendered markup —
	// a selector, an option, or the prompt attribute — not on the class name.)
	body := renderMenu492(t, menu492Fixture(t))
	for _, bad := range []string{`<select class="v"`, "data-pick=", "data-vname=", `class="varstock"`} {
		if strings.Contains(body, bad) {
			t.Errorf("a simple menu product rendered variation markup (%q)\nbody:\n%s", bad, body)
		}
	}
	// The #492 add path is still intact: the card carries its own id + price.
	for _, want := range []string{`data-price="780"`, `data-add="Add"`} {
		if !strings.Contains(body, want) {
			t.Errorf("simple item lost its #492 add contract (%q missing)", want)
		}
	}
	// ...and the plain add call is still present for a non-variable item.
	if !strings.Contains(body, "addItem(id,el.getAttribute('data-name'),el.getAttribute('data-price'));") {
		t.Error("the simple-item add call was removed by the variation change")
	}
}

// withVariations is a no-op when the store is absent (a menu page must still
// render) and leaves the item simple when the product has no variations.
func TestMenuVariationWithVariationsNilSafe(t *testing.T) {
	h := &PublicHandlers{}
	v := h.menuProductView("p1", "Nasi Lemak", "", 780, "", false, "Best", "Add")
	got := h.withVariations(nil, v, formLangs["en"])
	if got.Variable || len(got.Variations) != 0 {
		t.Errorf("nil variation store made the item variable: %+v", got)
	}
	// The labels are resolved even without a store, so the template's data is
	// never blank when the item does turn out to be variable.
	if got.SelectLabel == "" || got.PickLabel == "" || got.InStockLabel == "" {
		t.Errorf("variation labels not resolved: %+v", got)
	}
}

// variationViews flags a sold-out variant and formats its price, and is nil for
// an empty list (so a simple product never gains a selector).
func TestMenuVariationVariationViews(t *testing.T) {
	if got := variationViews(nil); got != nil {
		t.Errorf("variationViews(nil) = %+v, want nil", got)
	}
	zero := 0
	unlimited := (*int)(nil)
	views := variationViews([]*dbstore.ProductVariation{
		{ID: "a", Name: "Small", PriceSen: 1250, Stock: &zero},
		{ID: "b", Name: "Large", PriceSen: 1500, Stock: unlimited},
	})
	if len(views) != 2 {
		t.Fatalf("variationViews = %d, want 2", len(views))
	}
	if !views[0].SoldOut {
		t.Error("a zero-stock variation is not flagged sold out")
	}
	if views[1].SoldOut {
		t.Error("an unlimited-stock variation must never be sold out")
	}
	if views[0].Price != "12.50" || views[1].Price != "15.00" {
		t.Errorf("variant display prices = %q/%q, want 12.50/15.00", views[0].Price, views[1].Price)
	}
	if views[0].PriceSen != 1250 {
		t.Errorf("integer-sen price lost: %d", views[0].PriceSen)
	}
}

// The variant strings the menu page uses are the SAME keys the form path uses,
// and every locale carries them — so a variable menu item can never render a
// blank label, and the two surfaces cannot drift apart.
func TestMenuVariationVariationStringsInFiveLocales(t *testing.T) {
	fields := map[string]func(langStrings) string{
		"SelectVariation":      func(l langStrings) string { return l.SelectVariation },
		"SelectVariationAlert": func(l langStrings) string { return l.SelectVariationAlert },
		"InStock":              func(l langStrings) string { return l.InStock },
	}
	if len(formLangs) != 5 {
		t.Fatalf("formLangs has %d entries, want 5", len(formLangs))
	}
	for code, ls := range formLangs {
		for name, get := range fields {
			if get(ls) == "" {
				t.Errorf("lang %q has empty %s", code, name)
			}
		}
	}
	if got := langStringsForCode("ms").SelectVariation; got != "Pilih variasi" {
		t.Errorf("ms SelectVariation = %q, want Pilih variasi", got)
	}
}

// An Arabic menu page localises the variation selector it renders, and an
// unknown language falls back to English (never a blank option).
func TestMenuVariationVariationSelectorLocalisedOnTheMenuPage(t *testing.T) {
	body := renderMenu492(t, menuVariationFixtureFor(t, formLangs["ar"]))
	if !strings.Contains(body, `dir="rtl"`) {
		t.Error("ar menu page did not set dir=rtl")
	}
	if !strings.Contains(body, formLangs["ar"].SelectVariation) {
		t.Errorf("ar menu page did not localise the variation placeholder\nbody:\n%s", body)
	}
	if !strings.Contains(body, formLangs["ar"].SelectVariationAlert) {
		t.Error("ar menu page did not localise the pick prompt")
	}
	if got := langStringsForCode("unknown").SelectVariation; got != "Select an option" {
		t.Errorf("unknown lang did not fall back to en: %q", got)
	}
}
