package httpapi

// Card t_7d4c20c8 — a PUBLISHED PAYMENT-mode form with no block and no product
// was served the legacy product-checkbox layout with an EMPTY product list, so
// the page's only submit handler could never pass its own guard:
//
//	if (!items.length) { alert('Please select at least one product'); return; }
//
// There was nothing to tick (`class="p"` count 0), so the button's only
// reachable outcome was the alert: no request, no checkout, under a "Proceed to
// Payment" label. Measured live on prod (membe.jomform.com/new-form, read-only:
// HTTP 200, alert present, 0 product checkboxes, "Proceed to Payment" present).
//
// Decision pinned here: what such a form serves is the "not accepting orders"
// message page — the same standalone, submit-less page the schedule/entry-limit
// restrictions already use, in the buyer's language. It does NOT become a
// collect-only response form (that would silently change what the merchant's
// "payment" mode means) and the write path is not refused here (the six forms
// already published in that shape would keep serving the dead end until
// somebody edited them; the renderer is what a visitor actually hits).
//
// These tests drive the REAL dispatch (`serveForm`), not a renderer by hand: the
// defect is the dispatch choosing the legacy layout at all, so a test that calls
// renderLegacy directly measures the thing that was never wrong.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// emptyPaymentForm is the stored shape of the live specimen: mode=payment (the
// default — no `mode` key at all), an empty `blocks` array and no product.
func emptyPaymentForm(slug string, cfg map[string]any) *dbstore.Form {
	if cfg == nil {
		cfg = map[string]any{"description": "New form", "products": []any{}}
	}
	return &dbstore.Form{
		ID: "f_empty_payment", AccountID: "acc_empty_payment", Slug: slug,
		Title: "New form", Status: dbstore.FormStatusPublished, Config: cfg,
	}
}

// The defect: the served page is a checkout whose submit handler can only
// alert. The page must carry neither the payment CTA nor its product gate, and
// must carry the honest message instead — with no submit control at all.
func TestPublishedPaymentFormWithNoProductsIsNotServedACheckout(t *testing.T) {
	cases := map[string]map[string]any{
		// The live specimen: an empty products array.
		"empty products array": {"description": "New form", "products": []any{}},
		// A never-configured form: the key is simply absent.
		"no products key": {"description": "New form"},
		// An empty block list alongside it, spelled out: still nothing to tick.
		"empty blocks and products": {"blocks": []any{}, "products": []any{}},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			body, rec := serveFormBody(t, emptyPaymentForm("new-form", cfg))

			if rec.Code != http.StatusOK {
				t.Fatalf("serveForm status = %d, want 200 (body: %s)", rec.Code, body)
			}
			for _, banned := range []struct{ what, marker string }{
				{"the legacy payment CTA", "Proceed to Payment"},
				{"the product-required alert the button could only reach", "Please select at least one product"},
				{"the legacy submit handler's product gate", "items.length"},
				{"a product checkbox", `class="p"`},
				{"the legacy total row", `id="total"`},
			} {
				if strings.Contains(body, banned.marker) {
					t.Errorf("a payment form with nothing to sell was served the checkout: page carries %s (%q)", banned.what, banned.marker)
				}
			}
			// No submit control of any kind: this is the acceptance criterion,
			// not just "a different layout".
			for _, banned := range []struct{ what, marker string }{
				{"a form element", "<form"},
				{"a button", "<button"},
			} {
				if strings.Contains(body, banned.marker) {
					t.Errorf("the message page still carries %s (%q): the buyer is handed a control that cannot do anything", banned.what, banned.marker)
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
// a merchant workspace set to Malay must not get an English sentence.
func TestNoProductsMessageFollowsTheFormLanguage(t *testing.T) {
	f := emptyPaymentForm("borang-baharu", map[string]any{"lang": "ms", "products": []any{}})
	body, _ := serveFormBody(t, f)

	if !strings.Contains(body, formLangs["ms"].NoProductsMessage) {
		t.Errorf("a Malay form was not served the Malay message; page: %s", body)
	}
	if strings.Contains(body, formLangs["en"].NoProductsMessage) {
		t.Errorf("a Malay form was served the English message; page: %s", body)
	}
	if !strings.Contains(body, `lang="ms"`) {
		t.Errorf("the message page does not declare the form's language; page: %s", body)
	}
}

// Control 1 (card acceptance 4): a payment form that DOES have a product keeps
// the legacy checkout AND its product-required guard. The fix must not trade
// one dead end for another by hiding a working payment form.
func TestPaymentFormWithAProductKeepsTheCheckoutAndItsGuard(t *testing.T) {
	f := emptyPaymentForm("paid-form", map[string]any{
		"description": "Order something",
		"products": []any{
			map[string]any{"id": "p1", "name": "Item", "price_sen": 2500, "active": true},
		},
	})
	body, rec := serveFormBody(t, f)

	if rec.Code != http.StatusOK {
		t.Fatalf("serveForm status = %d, want 200", rec.Code)
	}
	for _, want := range []struct{ what, marker string }{
		{"the payment CTA", "Proceed to Payment"},
		{"the product-required guard", "Please select at least one product"},
		{"the product checkbox", `class="p"`},
	} {
		if !strings.Contains(body, want.marker) {
			t.Errorf("a payment form with one product lost %s (%q); page: %s", want.what, want.marker, body)
		}
	}
	if strings.Contains(body, formLangs["en"].NoProductsMessage) {
		t.Errorf("a payment form with one product was served the \"not accepting orders\" page")
	}
}

// Control 2: the guard mirrors the layout's own row loop. A snapshot with no id
// paints no checkbox in renderLegacy, so it must not be counted as "there is
// something to buy" — otherwise the dead end would come back for exactly this
// shape (the config the builder writes before a product is attached).
func TestPaymentFormWithNoTickableSnapshotIsNotServedACheckout(t *testing.T) {
	f := emptyPaymentForm("idless-product", map[string]any{
		"products": []any{map[string]any{"name": "Item", "price_sen": 100}},
	})
	body, _ := serveFormBody(t, f)

	if strings.Contains(body, "Proceed to Payment") || strings.Contains(body, `class="p"`) {
		t.Errorf("a payment form whose only product snapshot has no id was served a checkout it cannot submit; page: %s", body)
	}
	if !strings.Contains(body, formLangs["en"].NoProductsMessage) {
		t.Errorf("page does not carry the \"not accepting orders\" message; page: %s", body)
	}
}

// Control 3: a payment form WITH blocks is handled by renderBlocks before this
// guard ever runs, so it keeps its block layout. The new state must not become a
// catch-all for payment forms.
func TestBlockPaymentFormIsUnaffectedByTheNoProductsGuard(t *testing.T) {
	f := emptyPaymentForm("block-paid-form", map[string]any{
		"blocks": []any{
			map[string]any{"id": "p1", "type": "product", "product_id": "prod_1", "value": "Item", "price_sen": 2500},
		},
	})
	body, rec := serveFormBody(t, f)

	if rec.Code != http.StatusOK {
		t.Fatalf("serveForm status = %d, want 200", rec.Code)
	}
	if strings.Contains(body, formLangs["en"].NoProductsMessage) {
		t.Errorf("a payment form with a block was served the \"not accepting orders\" page; page: %s", body)
	}
	if !strings.Contains(body, `data-id="prod_1"`) {
		t.Errorf("the block payment form's product block was not rendered; page: %s", body)
	}
}

// The message page must not be reachable for a RESPONSE form either: those are
// dispatched by their own branch before this guard (t_2612f421). This is the
// boundary between the two cards, pinned so a later edit to the dispatch order
// cannot swallow response forms into the "not accepting orders" page.
func TestResponseFormIsNotServedTheNoProductsPage(t *testing.T) {
	f := &dbstore.Form{
		ID: "f_resp", AccountID: "acc_resp", Slug: "rsvp",
		Title: "RSVP", Status: dbstore.FormStatusPublished,
		Config: map[string]any{"mode": "response", "blocks": []any{}},
	}
	body, _ := serveFormBody(t, f)

	if strings.Contains(body, formLangs["en"].NoProductsMessage) {
		t.Errorf("a zero-block response form was served the payment \"not accepting orders\" page; page: %s", body)
	}
	if !strings.Contains(body, ">Submit<") {
		t.Errorf("a zero-block response form lost its response submit button; page: %s", body)
	}
}

// Guards the helper the dispatch relies on, so the guard cannot drift into
// "there is a products key" instead of "there is a row to tick".
func TestLegacyHasProducts(t *testing.T) {
	if legacyHasProducts(nil) {
		t.Error("nil form reported a product")
	}
	if legacyHasProducts(emptyPaymentForm("x", map[string]any{"products": []any{}})) {
		t.Error("an empty products array reported a product")
	}
	if legacyHasProducts(emptyPaymentForm("x", map[string]any{"products": []any{map[string]any{"name": "no id", "price_sen": 100}}})) {
		t.Error("a snapshot without an id reported a product: renderLegacy paints no checkbox for it")
	}
	if !legacyHasProducts(emptyPaymentForm("x", map[string]any{"products": []any{map[string]any{"id": "p1", "name": "Item"}}})) {
		t.Error("a snapshot with an id reported no product: renderLegacy paints a checkbox for it")
	}
}
