package httpapi

// Card t_bfdcafbc — the BLOCK payment layout was the same dead end card
// t_7d4c20c8 removed for the block-less shape.
//
// renderBlocks returns true for any config with a non-empty `blocks` array and
// paints the payment template, whose submit handler refuses to post unless it
// collected a line item:
//
//	if (!items.length) { alert('Please select at least one product'); return; }
//
// `items` is filled ONLY from the controls that template paints: a product
// block's checkbox (`class="p"`, data-id = the block's product_id), an option
// block's select (`class="opt"`) and a shipping block's select (`class="ship"`).
// The t_7d4c20c8 guard ran AFTER renderBlocks, so it never saw a block form. A
// PAYMENT form whose blocks paint none of those — which is exactly what the
// builder writes when a merchant publishes its DEFAULT form
// (`blocks:[name,email,phone]`, `products:[]`; measured live: HTTP 200, a
// "Proceed to Payment" button, alert-only submit, zero /order requests, zero
// submissions, zero sales) — therefore took the buyer's typing and discarded it.
//
// Decision pinned here: the question is asked BEFORE the block renderer, with a
// predicate that mirrors what that renderer PAINTS
// (blockLayoutPaintsNoLineItemControl). What the buyer gets is the existing
// submit-less "not accepting orders" page, in their language — not a collect-only
// response form (that would silently change what payment mode means).
//
// The predicate is deliberately NOT dbstore.PaymentFormWithoutProducts: that is
// the WRITE-path rule, and its "config product snapshot with an id" arm is true
// for the legacy layout (one row per snapshot) but false for the block layout,
// which paints controls from `blocks` only. Measured for this card: a form with
// `blocks:[name,email,phone]` and `products:[{id:…}]` but no product block
// renders "Proceed to Payment" with zero product/option/shipping controls — a
// dead end the snapshot arm would have let through. That shape is reachable from
// the builder (it saves the workspace catalogue into `products` on every save),
// so it is one of the cases below, and
// TestBlockLineItemPredicateMatchesTheRenderedPage keeps the predicate honest
// against the page renderBlocks actually paints.
//
// These tests drive the REAL dispatch (`serveForm`), not renderBlocks by hand:
// the defect is the dispatch ORDER, so a test that calls the renderer directly
// measures the thing that was never wrong.

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// blockPaymentForm is a published payment-mode form carrying `blocks` — the
// shape renderBlocks takes over. `mode` is absent or payment in every case here
// because payment is the default mode.
func blockPaymentForm(slug string, cfg map[string]any) *dbstore.Form {
	return &dbstore.Form{
		ID: "f_block_payment", AccountID: "acc_block_payment", Slug: slug,
		Title: "New form", Status: dbstore.FormStatusPublished, Config: cfg,
	}
}

// nameEmailPhone is the builder's default block set — no product block, no
// pricing block, nothing a buyer could be charged for.
func nameEmailPhone() []any {
	return []any{
		map[string]any{"id": "b_name", "type": "name", "value": "Full name", "required": true},
		map[string]any{"id": "b_email", "type": "email", "value": "Email", "required": true},
		map[string]any{"id": "b_phone", "type": "phone", "value": "Phone number"},
	}
}

// blockPaymentLayoutMarkers are the tells of the BLOCK payment template
// (render_blocks.go: form id="of" + `<button type="submit" form="of">` with
// Ls.Submit + the shared `if (!items.length)` gate). They are specific to it:
// the response block template also carries id="of", but its CTA is
// Ls.SubmitResponse ("Submit") and it has no product gate.
var blockPaymentLayoutMarkers = []struct{ what, marker string }{
	{"the block payment CTA (Ls.Submit)", "Proceed to Payment"},
	{"the block submit handler's line-item gate", "if(!items.length)"},
	{"the product-required alert the gate can only reach", "Please select at least one product"},
	{"the product checkbox control", `class="p"`},
	{"the total row", `id="total"`},
}

// The defect: a published payment form whose blocks carry nothing a buyer can
// add to the cart is served a checkout whose only reachable outcome is the
// product alert. It must be served the submit-less message page instead, with no
// request path at all.
func TestBlockPaymentFormWithNothingSellableIsNotServedADeadCheckout(t *testing.T) {
	cases := map[string]map[string]any{
		// The builder's own default publish: the live specimen of this card.
		"builder default: name/email/phone beside products:[]": {
			"description": "New form goes here",
			"products":    []any{},
			"blocks":      nameEmailPhone(),
		},
		// Same shape, products key simply absent (a never-configured form).
		"builder blocks with no products key": {
			"blocks": nameEmailPhone(),
		},
		// A product block the builder stored before a product was attached:
		// it paints a checkbox with an EMPTY data-id, so the cart it posts is
		// refused (`produk tidak sah: `) — the block-side twin of the idless
		// snapshot pinned in the t_7d4c20c8 tests.
		"product block without a product_id": {
			"blocks": []any{
				map[string]any{"id": "b_name", "type": "name", "value": "Full name"},
				map[string]any{"id": "b_prod", "type": "product", "value": "Item", "price_sen": 2500},
			},
		},
		// An option block with no choice rows: the select carries only its
		// empty placeholder, so nothing can ever be posted from it.
		"option block with no choices": {
			"blocks": []any{
				map[string]any{"id": "b_name", "type": "name", "value": "Full name"},
				map[string]any{"id": "b_opt", "type": "option", "value": "Extras", "price_sen": 0},
			},
		},
		// Choices that all have an empty value: the page's submit handler skips
		// them (`if (o && o.value)`), so the select is a dead control too.
		"option block whose choices carry no value": {
			"blocks": []any{
				map[string]any{"id": "b_name", "type": "name", "value": "Full name"},
				map[string]any{"id": "b_opt", "type": "option", "value": "Extras", "choices": []any{
					map[string]any{"label": "Yes", "price_sen": 500},
				}},
			},
		},
		// A static shipping block with no rate rows: placeholder only.
		"static shipping block with no choices": {
			"blocks": []any{
				map[string]any{"id": "b_name", "type": "name", "value": "Full name"},
				map[string]any{"id": "b_ship", "type": "shipping", "value": "Postage", "price_sen": 0},
			},
		},
		// A config product snapshot with an id beside the blocks — the shape
		// the builder writes for a name/email-only form in a workspace that has
		// a catalogue. renderBlocks paints NO control from the snapshot, so this
		// page is a dead checkout too (see the header comment).
		"snapshot with an id but no product block": {
			"products": []any{map[string]any{"id": "prod_9", "name": "Item", "price_sen": 1000}},
			"blocks":   nameEmailPhone(),
		},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			body, rec := serveFormBody(t, blockPaymentForm("builder-default", cfg))

			if rec.Code != http.StatusOK {
				t.Fatalf("serveForm status = %d, want 200 (body: %s)", rec.Code, body)
			}
			for _, banned := range blockPaymentLayoutMarkers {
				if strings.Contains(body, banned.marker) {
					t.Errorf("a block payment form with nothing sellable was served the checkout: page carries %s (%q); page: %s",
						banned.what, banned.marker, body)
				}
			}
			// No submit control of any kind. This is the acceptance criterion:
			// not "a different layout" but "nothing the buyer can press".
			for _, banned := range []struct{ what, marker string }{
				{"a form element", "<form"},
				{"a button", "<button"},
			} {
				if strings.Contains(body, banned.marker) {
					t.Errorf("the message page still carries %s (%q): the buyer is handed a control that cannot do anything",
						banned.what, banned.marker)
				}
			}
			if !strings.Contains(body, formLangs["en"].NoProductsMessage) {
				t.Errorf("page does not tell the buyer the form is not accepting orders; page: %s", body)
			}
			if !strings.Contains(body, "<h1>New form</h1>") {
				t.Errorf("page does not carry the form title; page: %s", body)
			}
		})
	}
}

// The message page follows the buyer's language like every other public page —
// this arm proves the block form is served the SAME localized page as the
// block-less one, not an English fallback copy.
func TestBlockNoProductsPageFollowsTheFormLanguage(t *testing.T) {
	f := blockPaymentForm("borang-baharu", map[string]any{
		"lang":     "ms",
		"products": []any{},
		"blocks":   nameEmailPhone(),
	})
	body, _ := serveFormBody(t, f)

	if !strings.Contains(body, formLangs["ms"].NoProductsMessage) {
		t.Errorf("a Malay block form was not served the Malay message; page: %s", body)
	}
	if strings.Contains(body, formLangs["en"].NoProductsMessage) {
		t.Errorf("a Malay block form was served the English message; page: %s", body)
	}
	if !strings.Contains(body, `lang="ms"`) {
		t.Errorf("the message page does not declare the form's language; page: %s", body)
	}
}

// Control 1: every way a block payment form CAN be paid for keeps the block
// checkout untouched — the control sources that template paints. The guard must
// not trade one dead end for another by hiding a working payment form.
func TestSellableBlockPaymentFormKeepsItsBlockCheckout(t *testing.T) {
	cases := map[string]map[string]any{
		"product block with a product_id": {
			"blocks": []any{
				map[string]any{"id": "b_name", "type": "name", "value": "Full name"},
				map[string]any{"id": "b_prod", "type": "product", "product_id": "prod_1", "value": "Item", "price_sen": 2500},
			},
		},
		"option block whose choice can be picked": {
			"blocks": []any{
				map[string]any{"id": "b_name", "type": "name", "value": "Full name"},
				map[string]any{"id": "b_opt", "type": "option", "value": "Gift wrap", "choices": []any{
					map[string]any{"label": "Yes", "value": "yes", "price_sen": 500},
				}},
			},
		},
		"static shipping block with a rate": {
			"blocks": []any{
				map[string]any{"id": "b_name", "type": "name", "value": "Full name"},
				map[string]any{"id": "b_ship", "type": "shipping", "value": "Postage", "choices": []any{
					map[string]any{"label": "Peninsular", "value": "pen", "price_sen": 800},
				}},
			},
		},
		// Zone-sourced shipping is its own shape with its own server-side guard
		// (t_9a013e57): the guard must not swallow it.
		"zone-sourced shipping block": {
			"blocks": []any{
				map[string]any{"id": "b_name", "type": "name", "value": "Full name"},
				map[string]any{"id": "b_ship", "type": "shipping", "value": "Delivery", "source": "zones", "zone_set_id": "zs_1"},
			},
		},
		// A product block is enough on its own even with a snapshot beside it.
		"product block beside a config snapshot": {
			"products": []any{map[string]any{"id": "prod_9", "name": "Item", "price_sen": 1000}},
			"blocks": []any{
				map[string]any{"id": "b_name", "type": "name", "value": "Full name"},
				map[string]any{"id": "b_prod", "type": "product", "product_id": "prod_9", "value": "Item", "price_sen": 1000},
			},
		},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			body, rec := serveFormBody(t, blockPaymentForm("block-paid-form", cfg))

			if rec.Code != http.StatusOK {
				t.Fatalf("serveForm status = %d, want 200 (body: %s)", rec.Code, body)
			}
			if strings.Contains(body, formLangs["en"].NoProductsMessage) {
				t.Errorf("a block payment form with something to sell was served the \"not accepting orders\" page; page: %s", body)
			}
			for _, want := range []struct{ what, marker string }{
				{"the block payment form element", `id="of"`},
				{"the block payment CTA", "Proceed to Payment"},
				{"the buyer's own block inputs", `name="full_name"`},
			} {
				if !strings.Contains(body, want.marker) {
					t.Errorf("a sellable block payment form lost %s (%q); page: %s", want.what, want.marker, body)
				}
			}
		})
	}
}

// Control 2: a RESPONSE form with the same nothing-sellable blocks keeps its
// collect-only layout (t_2612f421). Its branch is dispatched before this guard,
// and the guard's own predicate is mode-aware — pinning both so a later edit to
// either cannot swallow response forms into the payment message page.
func TestResponseBlockFormWithNothingSellableKeepsItsResponseLayout(t *testing.T) {
	f := blockPaymentForm("rsvp", map[string]any{
		"mode":     "response",
		"products": []any{},
		"blocks":   nameEmailPhone(),
	})
	body, rec := serveFormBody(t, f)

	if rec.Code != http.StatusOK {
		t.Fatalf("serveForm status = %d, want 200 (body: %s)", rec.Code, body)
	}
	if strings.Contains(body, formLangs["en"].NoProductsMessage) {
		t.Errorf("a response form was served the payment \"not accepting orders\" page; page: %s", body)
	}
	for _, want := range []struct{ what, marker string }{
		{"the response submit control", ">Submit<"},
		{"the form's own blocks", `data-jf-client="full_name"`},
	} {
		if !strings.Contains(body, want.marker) {
			t.Errorf("a response form lost %s (%q); page: %s", want.what, want.marker, body)
		}
	}
	// The payment template's gate must not be on a response page at all.
	for _, banned := range blockPaymentLayoutMarkers[:3] {
		if strings.Contains(body, banned.marker) {
			t.Errorf("a response form was served the payment layout: page carries %s (%q)", banned.what, banned.marker)
		}
	}
}

// Control 3 (#497): an enabled per-form menu is its own storefront — the form
// URL renders the menu page even though the form carries no product block. Both
// the menu dispatch (which returns before this guard) and the predicate's
// payment-mode scope keep it out of the message page.
func TestMenuFormWithNothingSellableKeepsItsMenuPage(t *testing.T) {
	f := blockPaymentForm("menu-lunch", map[string]any{
		"products": []any{},
		"blocks":   nameEmailPhone(),
		"menu":     map[string]any{"enabled": true, "categories": []any{}},
	})
	body, rec := serveFormBody(t, f)

	if rec.Code != http.StatusOK {
		t.Fatalf("serveForm status = %d, want 200 (body: %s)", rec.Code, body)
	}
	if strings.Contains(body, formLangs["en"].NoProductsMessage) {
		t.Errorf("an enabled-menu form was served the payment \"not accepting orders\" page; page: %s", body)
	}
	if !strings.Contains(body, `id="menuSheet"`) {
		t.Errorf("an enabled-menu form did not render its menu page; page: %s", body)
	}
	if strings.Contains(body, "Proceed to Payment") {
		t.Error("an enabled-menu form was served the block payment checkout instead of its menu")
	}
}

// productCheckbox and pricingChoice match the two controls the block payment
// template's submit handler collects line items from: a product checkbox with a
// NON-EMPTY data-id, and a selectable option/shipping choice (painted as
// `<option value="V" data-price="P">`, which is how they are told apart from the
// plain `notes` selects that carry no data-price).
var (
	productCheckbox = regexp.MustCompile(`class="p" data-id="[^"]+"`)
	pricingChoice   = regexp.MustCompile(`<option value="[^"]+" data-price="`)
)

// The predicate exists because two different renderer questions were being
// answered by one rule. This test is the anti-drift guard the card asks for: for
// every block shape, what blockLayoutPaintsNoLineItemControl CLAIMS must equal
// what renderBlocks actually PAINTS. If a later template change adds or drops a
// buyable control, one side moves and this test reddens instead of the fix
// silently rotting.
func TestBlockLineItemPredicateMatchesTheRenderedPage(t *testing.T) {
	cases := map[string]map[string]any{
		"builder default": {
			"products": []any{},
			"blocks":   nameEmailPhone(),
		},
		"product block with an id": {
			"blocks": []any{map[string]any{"id": "b1", "type": "product", "product_id": "prod_1", "value": "Item", "price_sen": 2500}},
		},
		"product block without an id": {
			"blocks": []any{map[string]any{"id": "b1", "type": "product", "value": "Item", "price_sen": 2500}},
		},
		"option block with a priced choice": {
			"blocks": []any{map[string]any{"id": "b1", "type": "option", "value": "Add-on", "choices": []any{
				map[string]any{"label": "Yes", "value": "yes", "price_sen": 500},
			}}},
		},
		"option block with no choices": {
			"blocks": []any{map[string]any{"id": "b1", "type": "option", "value": "Add-on"}},
		},
		"option block whose choice has no value": {
			"blocks": []any{map[string]any{"id": "b1", "type": "option", "value": "Add-on", "choices": []any{
				map[string]any{"label": "Yes", "price_sen": 500},
			}}},
		},
		"shipping block with a rate": {
			"blocks": []any{map[string]any{"id": "b1", "type": "shipping", "value": "Postage", "choices": []any{
				map[string]any{"label": "Peninsular", "value": "pen", "price_sen": 800},
			}}},
		},
		"shipping block without rates": {
			"blocks": []any{map[string]any{"id": "b1", "type": "shipping", "value": "Postage"}},
		},
		"snapshot with an id and no product block": {
			"products": []any{map[string]any{"id": "prod_9", "name": "Item", "price_sen": 1000}},
			"blocks":   nameEmailPhone(),
		},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			f := blockPaymentForm("predicate", cfg)
			h := &PublicHandlers{}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "http://demo.jomform.com/predicate", nil)
			if !h.renderBlocks(rec, req, f) {
				t.Fatalf("renderBlocks declined the config — this shape is no longer the block layout, so the predicate is measuring nothing")
			}
			page := rec.Body.String()
			pageHasControl := productCheckbox.MatchString(page) || pricingChoice.MatchString(page)
			claimsNoControl := blockLayoutPaintsNoLineItemControl(f)

			if claimsNoControl == pageHasControl {
				t.Fatalf("predicate and page disagree: blockLayoutPaintsNoLineItemControl=%v but the rendered page has a line-item control=%v ("+
					"the predicate and the template have drifted apart); page: %s", claimsNoControl, pageHasControl, page)
			}
			if claimsNoControl && !strings.Contains(page, "Proceed to Payment") {
				t.Errorf("premise broken: the page this case calls a dead end is not even the payment layout; page: %s", page)
			}
		})
	}
}
