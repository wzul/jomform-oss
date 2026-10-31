package httpapi

// Card t_4975927e — the `coupon` block promised a discount nothing applied.
//
// THE DEFECT, as measured on the tree this card was opened on:
//   * blockTmpl painted a real text input
//     (`<input type="text" name="coupon" class="coupon" data-id="…">`) for a
//     block the palette offered under "Pricing" (blk.couponHint = "Discount
//     code"), and the submit JS collected every `[data-id]` element — so the
//     typed code really was transmitted and stored as an unlabelled field;
//   * NOTHING on the submit/order path ever read it: order.go charges
//     `payments.SumItems(subtotal, 0, 0)`, there is no coupon table and no
//     server-side validation. The buyer typed a code, got no error, and paid
//     full price.
//
// THE FIX (option A of the card): the block is withdrawn from the palette, from
// formgen's allowlist and from the renderer. A form that stored one BEFORE the
// withdrawal must still be served honestly — the same decision `file` got
// (card t_4086b8f0): the merchant's label plus a note saying the discount is not
// applied, and NEVER an input, because an input is the promise that something
// will be applied.
//
// WHAT THESE ASSERTIONS PIN, per renderer:
//   1. no coupon control and no `[data-id]` element for the block — so the
//      submit JS cannot collect a code that would go nowhere;
//   2. the block is not dropped silently — its label and the note are on the
//      page (the class rule `file` established);
//   3. the neighbouring real field is untouched, so the withdrawal is scoped to
//      the coupon block and not to the template.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const couponNoteEN = "Discount codes are not applied on this form."

// couponConfig is a payment form with one stored coupon block and one real input
// beside it, so a template-wide change is distinguishable from a targeted one.
func couponConfig() map[string]any {
	return map[string]any{"blocks": []any{
		map[string]any{"id": "cp1", "type": "coupon", "value": "Kod diskaun"},
		map[string]any{"id": "tx1", "type": "text", "value": "Nama"},
	}}
}

func TestPaymentFormStoredCouponBlockIsNotAnInput(t *testing.T) {
	h := &PublicHandlers{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://demo.jomform.com/pay", nil)
	if !h.renderBlocks(rec, req, themedForm("pay", "#5e6ad2", couponConfig())) {
		t.Fatal("renderBlocks declined a config that carries blocks")
	}
	body := rec.Body.String()

	// 1. The lie is gone: no coupon control, and no element the submit JS's
	//    `[data-id]` collector would turn into a submitted field.
	if strings.Contains(body, `name="coupon"`) {
		t.Errorf("the payment page still offers a coupon input — the buyer can type a code nothing applies")
	}
	if strings.Contains(body, `class="coupon`) {
		t.Errorf("the payment page still paints the coupon class, so a coupon control is back in some form")
	}
	if in := firstInputNamed(body, "cp1"); in != "" {
		t.Errorf("a stored coupon block rendered an input the submit JS would collect: %s", in)
	}
	if strings.Contains(body, `data-id="cp1"`) {
		t.Errorf("the coupon block still carries data-id, so the submit JS records a code that is never applied")
	}

	// 2. Not dropped silently: the merchant's own label and the honest note.
	if !strings.Contains(body, "Kod diskaun") {
		t.Errorf("the stored coupon block's label is missing — the block was dropped silently")
	}
	if !strings.Contains(body, couponNoteEN) {
		t.Errorf("the page does not say the discount is not applied (page: %s)", body)
	}

	// 3. The withdrawal is scoped to the coupon block: the text field beside it
	//    still works. Without this a template that rendered nothing at all would
	//    pass 1 and 2's first half.
	if in := firstInputNamed(body, "tx1"); in == "" {
		t.Errorf("the neighbouring text block lost its input, so the withdrawal hit the whole template")
	}
}

func TestResponseFormStoredCouponBlockIsNotDroppedSilently(t *testing.T) {
	cfg := map[string]any{"mode": "response", "blocks": []any{
		map[string]any{"id": "cp1", "type": "coupon", "value": "Kod diskaun"},
		map[string]any{"id": "tx1", "type": "text", "value": "Nama"},
	}}
	body := renderResponsePage(t, "legacy-coupon", cfg)

	if !strings.Contains(body, "Kod diskaun") {
		t.Errorf("a stored coupon block's own label is missing from the response page — the block was dropped silently")
	}
	if !strings.Contains(body, couponNoteEN) {
		t.Errorf("the response page does not say the discount is not applied (page: %s)", body)
	}
	if strings.Contains(body, `name="coupon"`) || firstInputNamed(body, "cp1") != "" {
		t.Errorf("a stored coupon block rendered a control on the response page, where there is nothing to apply")
	}
	if in := firstInputNamed(body, "tx1"); in == "" {
		t.Errorf("the neighbouring text block lost its input, so the withdrawal hit the whole template")
	}
}

// TestPublicFormRouteServesAStoredCouponBlockHonestly is the end-to-end half of
// card t_4975927e, through the real route (publicFixture + publicServe) rather
// than the template function: it proves the withdrawal reaches the page a buyer
// is served, with the language the form declares, after a real DB load.
//
// The stored config is written by SQL on purpose — it is the state a form saved
// BEFORE the withdrawal is in, and no write path can produce it any more.
//
// Card t_bfdcafbc: the seeded block list carries one buyable product block as
// well, because a payment form whose blocks hold nothing a buyer can add to the
// cart is now served the submit-less "not accepting orders" page — which is not
// the page this test is about.
func TestPublicFormRouteServesAStoredCouponBlockHonestly(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	h, workspaceSlug, formSlug := publicFixture(t)

	var formID string
	if err := pool.QueryRow(ctx, `SELECT id FROM forms WHERE slug = $1`, formSlug).Scan(&formID); err != nil {
		t.Fatalf("lookup form id: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE forms SET config = config
		|| '{"lang":"en"}'::jsonb
		|| '{"blocks":[{"id":"cp1","type":"coupon","value":"Kod diskaun"},{"id":"tx1","type":"text","value":"Nama"},{"id":"it_prod","type":"product","product_id":"it_prod_marker","value":"Marker item","price_sen":1000}]}'::jsonb
		WHERE id = $1`, formID); err != nil {
		t.Fatalf("store the legacy coupon config: %v", err)
	}

	rec := publicServe(t, h, workspaceSlug+".jomform.com", "/"+formSlug)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /%s status = %d, want 200 (body %s)", formSlug, rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	if strings.Contains(body, `name="coupon"`) || firstInputNamed(body, "cp1") != "" {
		t.Errorf("the served page still offers a coupon control: the buyer can type a code nothing applies")
	}
	if !strings.Contains(body, "Kod diskaun") || !strings.Contains(body, couponNoteEN) {
		t.Errorf("the served page does not carry the block's label and the not-applied note")
	}
	if in := firstInputNamed(body, "tx1"); in == "" {
		t.Errorf("the neighbouring text block lost its input on the served page")
	}
}

// The note is a SYSTEM string, so it follows the form's language like every
// other rendered label — a Malay form must not serve an English sentence
// (issue #57's rule). Two locales are enough to catch a hardcoded string.
func TestStoredCouponNoteFollowsTheFormLanguage(t *testing.T) {
	for _, tc := range []struct {
		lang string
		want string
	}{
		{"en", "Discount codes are not applied on this form."},
		{"ms", "Kod diskaun tidak digunakan pada borang ini."},
	} {
		h := &PublicHandlers{}
		cfg := map[string]any{"lang": tc.lang, "blocks": []any{
			map[string]any{"id": "cp1", "type": "coupon", "value": "Kod diskaun"},
		}}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://demo.jomform.com/pay", nil)
		if !h.renderBlocks(rec, req, themedForm("pay", "#5e6ad2", cfg)) {
			t.Fatalf("%s: renderBlocks declined a config that carries blocks", tc.lang)
		}
		if body := rec.Body.String(); !strings.Contains(body, tc.want) {
			t.Errorf("lang %s: the coupon note is not the %s string (want %q)", tc.lang, tc.lang, tc.want)
		}
	}
}
