package httpapi

// Card t_2612f421 — a PUBLISHED response-mode form whose config carries an
// empty `blocks` array was served the legacy PAYMENT layout.
//
// Measured on prod (qa-t-203b3a93.jomform.com/qa-t-203b3a93-rsvp, and
// wan.jomform.com/untitled-template, both `config->>'mode' = "response"` with
// `jsonb_array_length(config->'blocks') = 0`): the page carried the literal
// `alert('Please select at least one product')` and a "Proceed to Payment"
// button. A response form has no products by design, so that button's only
// reachable outcome was the alert — no request was ever sent and the visitor
// could not submit.
//
// The cause is the dispatch, not the renderer's content: `renderResponseForm`
// refused a zero-block config (it returned false, "caller falls back to the
// legacy layout") and `renderBlocks` also declines a config with no blocks, so
// `serveForm` ended at `renderLegacy` → `formTmpl` — the payment template,
// whose submit handler is gated on a ticked product.
//
// These tests drive the REAL dispatch (`serveForm`), not a renderer by hand.
// That distinction is the point: `renderResponseForm` alone stays green while
// `serveForm` still falls through to the legacy layout, which is exactly the
// defect. They also pin the two neighbours the card must not disturb: a
// response form WITH blocks keeps the response layout, and a legacy payment
// form (products, no blocks) keeps its product-required guard.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// zeroBlockResponseForm is the stored shape of the two published prod forms:
// mode=response and an empty blocks array, and nothing else.
func zeroBlockResponseForm(slug string) *dbstore.Form {
	return &dbstore.Form{
		ID: "f_zero_block", AccountID: "acc_zero_block", Slug: slug,
		Title: "QA RSVP", Status: dbstore.FormStatusPublished,
		Config: map[string]any{
			"mode":   "response",
			"blocks": []any{},
		},
	}
}

// serveFormBody drives the public dispatch for a resolved form and returns the
// rendered page (plus the recorder, for status assertions).
func serveFormBody(t *testing.T, f *dbstore.Form) (string, *httptest.ResponseRecorder) {
	t.Helper()
	h := &PublicHandlers{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://demo.jomform.com/"+f.Slug, nil)
	h.serveForm(rec, req, f)
	return rec.Body.String(), rec
}

// The defect: the served page is the legacy payment template. Its two tells are
// the payment CTA label and the product-required guard that makes the page a
// dead end; a response form must carry neither, and must carry the response
// submit contract instead.
func TestZeroBlockResponseFormIsServedTheResponseLayout(t *testing.T) {
	body, rec := serveFormBody(t, zeroBlockResponseForm("qa-t-2612f421-rsvp"))

	if rec.Code != http.StatusOK {
		t.Fatalf("serveForm status = %d, want 200 (body: %s)", rec.Code, body)
	}
	for _, banned := range []struct{ what, marker string }{
		{"the legacy PAYMENT CTA (Ls.Submit)", "Proceed to Payment"},
		{"the legacy product-required alert (Ls.NoProductAlert)", "Please select at least one product"},
		{"the legacy submit handler's product gate", "items.length"},
	} {
		if strings.Contains(body, banned.marker) {
			t.Errorf("zero-block response form was served the payment layout: page carries %s (%q)", banned.what, banned.marker)
		}
	}
	// The response layout's own marker: the submit button label and the
	// submission handler that posts {client, fields} with no product list.
	if !strings.Contains(body, ">Submit<") {
		t.Errorf("page does not carry the response submit button (Ls.SubmitResponse); page: %s", body)
	}
	if !strings.Contains(body, "location.pathname+'/order'") {
		t.Errorf("page does not carry the response submit handler's POST target")
	}
	if !strings.Contains(body, "d.status==='submitted'") {
		t.Errorf("page does not carry the response thank-you branch (submitted status)")
	}
}

// Control 1 (card acceptance 4): a response form WITH blocks is unchanged — it
// still renders the response layout, never the payment template.
func TestBlockResponseFormKeepsTheResponseLayout(t *testing.T) {
	f := &dbstore.Form{
		ID: "f_with_blocks", AccountID: "acc_with_blocks", Slug: "event-registration",
		Title: "Event registration", Status: dbstore.FormStatusPublished,
		Config: map[string]any{
			"mode": "response",
			"blocks": []any{
				map[string]any{"id": "n1", "type": "name", "value": "Full name", "required": true},
				map[string]any{"id": "e1", "type": "email", "value": "Email", "required": true},
			},
		},
	}
	body, _ := serveFormBody(t, f)

	for _, banned := range []string{"Proceed to Payment", "Please select at least one product", "items.length"} {
		if strings.Contains(body, banned) {
			t.Errorf("block response form fell to the payment layout: page carries %q", banned)
		}
	}
	if !strings.Contains(body, `data-jf-client="email"`) {
		t.Errorf("the email block is not rendered as a response field; page: %s", body)
	}
	if !strings.Contains(body, ">Submit<") {
		t.Errorf("page does not carry the response submit button")
	}
}

// Control 2 (card acceptance 4): the payment path keeps its product-required
// guard for a form that DOES have products. This is the layout the fix must
// keep off a response form — and it must keep serving payment forms that have
// a product list, or the fix would have traded one dead end for another.
func TestLegacyPaymentFormKeepsProductRequiredGuard(t *testing.T) {
	f := &dbstore.Form{
		ID: "f_legacy_pay", AccountID: "acc_legacy_pay", Slug: "new-form-paid",
		Title: "Paid form", Status: dbstore.FormStatusPublished,
		Config: map[string]any{
			"description": "Order something",
			"products": []any{
				map[string]any{"id": "p1", "name": "Item", "price_sen": 2500, "active": true},
			},
		},
	}
	body, _ := serveFormBody(t, f)

	if !strings.Contains(body, "Please select at least one product") {
		t.Errorf("a legacy payment form lost its product-required guard")
	}
	if !strings.Contains(body, "Proceed to Payment") {
		t.Errorf("a legacy payment form lost its payment CTA label")
	}
	if !strings.Contains(body, `class="p"`) {
		t.Errorf("a legacy payment form with one product rendered no product control")
	}
}
