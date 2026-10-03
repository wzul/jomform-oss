package httpapi

// Issue #492 (part 2 of 3): restaurant-menu cart + checkout UI.
//
// These are pure template/helper tests (no DB) pinning the acceptance
// criteria:
//   - a menu card/row is an add-to-cart control carrying the product id and
//     its integer-sen price — it is NOT an <a href> to a form (the menu→form
//     link is gone);
//   - the page ships a sticky basket bar, a basket sheet with qty stepper /
//     remove / clear, and a checkout form that collects name + email;
//   - the cart persists in localStorage keyed by workspace;
//   - the checkout posts to the workspace order endpoint (POST /menu-order,
//     #491) with items[{product_id,quantity}], client{full_name,email},
//     table_number and affiliate_code, then redirects to the CHIP URL;
//   - ?table=N and ?aff=CODE reach the page and are echoed by the client;
//   - every new string exists in all 5 locales;
//   - regular (non-menu) forms are untouched: the menu template is the only
//     place the cart lives.

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

// menu492Fixture builds a one-item menu on a workspace with ?table=N and
// ?aff=CODE set.
func menu492Fixture(t *testing.T) workspaceMenuData {
	t.Helper()
	d := menu485Fixture(t)
	d.Aff = "AFF9"
	d.RenderTS = 1700000000
	return d
}

func renderMenu492(t *testing.T, data workspaceMenuData) string {
	t.Helper()
	var buf bytes.Buffer
	if err := menuPageTmpl.Execute(&buf, data); err != nil {
		t.Fatalf("menuPageTmpl.Execute: %v", err)
	}
	return buf.String()
}

// A menu card/row is an add-to-cart button (no navigation to a form) and
// carries the product id + integer-sen price the basket needs.
func TestIssue492ItemIsAddToCartNotFormLink(t *testing.T) {
	body := renderMenu492(t, menu492Fixture(t))

	if strings.Contains(body, `href="/order`) || strings.Contains(body, `<a class="card"`) || strings.Contains(body, `<a class="row"`) {
		t.Errorf("menu item still links into a form\nbody:\n%s", body)
	}
	for _, want := range []string{
		`<button class="card" type="button" data-id="p1"`,
		`<button class="row" type="button" data-id="p2"`,
		`data-price="780"`,  // integer sen, not a formatted string
		`data-add="Add"`,    // localised add affordance
		`aria-label="Add: `, // accessible name
		`class="plus"`,      // visible + affordance
		`class="qtybadge"`,  // on-item quantity badge
	} {
		if !strings.Contains(body, want) {
			t.Errorf("menu item missing %q", want)
		}
	}
}

// The sticky bar, basket sheet and checkout controls are all present, and the
// checkout collects name + email.
func TestIssue492BasketBarSheetAndCheckoutPresent(t *testing.T) {
	body := renderMenu492(t, menu492Fixture(t))
	for _, want := range []string{
		`id="menuBar"`,             // sticky basket bar
		`id="menuBarCount"`,        // count
		`id="menuBarTotal"`,        // total
		`id="menuSheet"`,           // basket sheet
		`id="menuLines"`,           // line items
		`id="menuSubtotal"`,        // running subtotal
		`id="menuClear"`,           // clear cart
		`id="menuCheckout"`,        // checkout form
		`name="full_name"`,         // mandatory name
		`name="email"`,             // mandatory email
		`id="menuPay"`,             // pay button
		`id="menuErr"`,             // submit error surface
		".stepper {",               // qty stepper styling
		"step.className='stepper'", // qty stepper is built per line
		`id="menuBar" hidden`,      // hidden until the cart is non-empty
	} {
		if !strings.Contains(body, want) {
			t.Errorf("menu page missing %q", want)
		}
	}
}

// The client script persists the cart per workspace, posts to /menu-order with
// the right payload, carries table + affiliate, redirects to CHIP, and keeps
// the basket on failure.
func TestIssue492CheckoutScriptWiring(t *testing.T) {
	body := renderMenu492(t, menu492Fixture(t))
	for _, want := range []string{
		"localStorage",                                             // persists across reload
		"'jf.menu.cart.v2.'+slug",                                  // keyed by FORM slug (#497)
		"fetch('/'+encodeURIComponent(slug)+'/order'",              // the form's own order route (#497)
		"product_id:it.id,variation_id:it.vid||'',quantity:it.qty", // items[{product_id,variation_id,quantity}]
		"client:{full_name:name,email:email}",                      // client{full_name,email}
		"table_number:table||''",                                   // table carried (#489)
		"affiliate_code:aff||''",                                   // affiliate carried (#444)
		"d.checkout_url||d.next",                                   // redirect to CHIP
		"location.href=d.checkout_url||d.next",
		"new URLSearchParams(location.search)", // ?table=/?aff= read client-side
		"qs.get('table')",
		"qs.get('aff')",
		"localStorage.removeItem(KEY)", // basket cleared only on success
	} {
		if !strings.Contains(body, want) {
			t.Errorf("checkout script missing %q", want)
		}
	}
	// A menu cart line carries the variant the buyer chose, so a variable
	// product is not silently flattened to its base price at checkout.
	if !strings.Contains(body, "variation_id:it.vid") {
		t.Error("checkout script does not send the chosen variation_id")
	}
}

// The checkout requires BOTH name and email and blocks submit when either is
// empty, and keeps the basket on a failed submit.
func TestIssue492CheckoutRequiresNameAndEmail(t *testing.T) {
	body := renderMenu492(t, menu492Fixture(t))
	if !strings.Contains(body, "if(!name||!email)") {
		t.Errorf("checkout does not block submit when name or email is empty\nbody:\n%s", body)
	}
	// The mandatory-field message is surfaced (localised).
	if !strings.Contains(body, `data-lbl-required="Please enter your name and email."`) {
		t.Error("checkout missing the localised required-fields message")
	}
	// On failure the button is restored and the basket is NOT cleared.
	if !strings.Contains(body, "payBtn.disabled=false") {
		t.Error("checkout does not re-enable the pay button after a failure")
	}
}

// ?table=N and ?aff=CODE are echoed into the page (server side) and shipped to
// the client; the no-JS filter round-trip keeps them too.
func TestIssue492TableAndAffiliateCarried(t *testing.T) {
	body := renderMenu492(t, menu492Fixture(t))
	for _, want := range []string{
		`data-table="8"`,
		`data-aff="AFF9"`,
		`<input type="hidden" name="table" value="8">`,
		`<input type="hidden" name="aff" value="AFF9">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("menu page missing %q", want)
		}
	}
}

// The render timestamp is embedded so the cart checkout submits the same
// anti-spam timestamp every public form does.
func TestIssue492RenderTimestampEmbedded(t *testing.T) {
	body := renderMenu492(t, menu492Fixture(t))
	if !strings.Contains(body, `data-ts="1700000000"`) {
		t.Error("menu page did not embed the render timestamp for the anti-spam check")
	}
	if !strings.Contains(body, "jf_ts:String(ts)") {
		t.Error("checkout does not submit the anti-spam timestamp field")
	}
}

// Every locale carries the full cart/checkout dictionary — no blank label can
// leak, and a non-English locale really localises the cart.
func TestIssue492CartStringsInFiveLocales(t *testing.T) {
	fields := map[string]func(langStrings) string{
		"MenuAdd":         func(l langStrings) string { return l.MenuAdd },
		"MenuBasket":      func(l langStrings) string { return l.MenuBasket },
		"MenuItems":       func(l langStrings) string { return l.MenuItems },
		"MenuViewBasket":  func(l langStrings) string { return l.MenuViewBasket },
		"MenuSubtotal":    func(l langStrings) string { return l.MenuSubtotal },
		"MenuCheckout":    func(l langStrings) string { return l.MenuCheckout },
		"MenuClear":       func(l langStrings) string { return l.MenuClear },
		"MenuBasketEmpty": func(l langStrings) string { return l.MenuBasketEmpty },
		"MenuName":        func(l langStrings) string { return l.MenuName },
		"MenuRemove":      func(l langStrings) string { return l.MenuRemove },
		"MenuClose":       func(l langStrings) string { return l.MenuClose },
		"MenuInc":         func(l langStrings) string { return l.MenuInc },
		"MenuDec":         func(l langStrings) string { return l.MenuDec },
		"MenuSubmitting":  func(l langStrings) string { return l.MenuSubmitting },
		"MenuSubmitError": func(l langStrings) string { return l.MenuSubmitError },
		"MenuRequired":    func(l langStrings) string { return l.MenuRequired },
		// The variation selector a variable menu product renders, and the
		// prompt a card tap raises when no variant is chosen yet.
		"SelectVariation":      func(l langStrings) string { return l.SelectVariation },
		"InStock":              func(l langStrings) string { return l.InStock },
		"SelectVariationAlert": func(l langStrings) string { return l.SelectVariationAlert },
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
	if got := langStringsForCode("ms").MenuViewBasket; got != "Lihat bakul" {
		t.Errorf("ms MenuViewBasket = %q, want Lihat bakul", got)
	}
	// An Arabic menu page localises the basket sheet and stays RTL.
	d := menu492Fixture(t)
	d.Ls = formLangs["ar"]
	body := renderMenu492(t, d)
	if !strings.Contains(body, `dir="rtl"`) {
		t.Error("ar menu page did not set dir=rtl")
	}
	if !strings.Contains(body, "عرض السلة") {
		t.Error("ar menu page did not localise the basket call to action")
	}
}

// The menu page is the ONLY place the cart lives: the shared public form
// submit still posts to the form-scoped order route and never to /menu-order,
// so regular (RSVP/wedding) forms are completely unaffected.
func TestIssue492RegularFormsUnaffected(t *testing.T) {
	body := renderMenu492(t, menu492Fixture(t))
	// The cart script is scoped to #menuSheet; a page without that element
	// exits before doing anything.
	if !strings.Contains(body, "var sheet=document.getElementById('menuSheet');\n  if(!sheet) return;") {
		t.Error("cart script is not guarded on the menu sheet element")
	}
	// No regular (non-menu) form renderer may reference the cart or the
	// workspace order route — the form path keeps posting to /{slug}/order.
	for name, tmpl := range map[string]*template.Template{
		"blockTmpl":      blockTmpl,
		"responseTmpl":   responseTmpl,
		"formTmpl":       formTmpl,
		"customFormTmpl": customFormTmpl,
	} {
		src := tmpl.Tree.Root.String()
		if strings.Contains(src, "/menu-order") || strings.Contains(src, "jf.menu.cart") {
			t.Errorf("%s leaked the menu cart (regular forms must be untouched)", name)
		}
	}
}
