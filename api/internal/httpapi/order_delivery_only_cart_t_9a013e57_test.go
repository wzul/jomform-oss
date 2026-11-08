package httpapi

// Card t_9a013e57: a DELIVERY-ONLY form answered 500 instead of a sentence.
//
// A form whose only pricing block is a zone/courier-sourced shipping block is
// published on purpose (`publish?acknowledge=1`, dbstore.PublishNeedsAck): the
// page posts that block's own item so the fee can be resolved from the buyer's
// address, and submitOrder drops it because a server-resolved fee is not a line
// item (httpapi/order.go, feeItemIDs). When it is the ONLY item the cart
// resolves to zero line items and sales.items NOT NULL answered
// `500 {"code":"internal"}` on SQLSTATE 23502 — the buyer saw "internal error"
// for a form the product's own publish flow produced.
//
// The fix refuses the order in words, before the subscription/shipping
// resolution (so a doomed order never makes a live courier quotation and never
// records a sale): "There are no products to pay for". A delivery-only form has
// nothing to sell; charging postage on an empty cart is not a sale.
//
// WHAT THESE PIN, and why each exists:
//
//   - TestDeliveryOnlyZoneFormIsRefusedNotA500: the card itself — one shipped
//     item, no product, zone source. The response must be the refusal sentence,
//     never the internal 500, and no sale may be recorded.
//   - TestDeliveryOnlyCourierFormIsRefusedNotA500: the same shape from the
//     OTHER managed source; the drop is source-agnostic, so the refusal must be
//     too.
//   - TestStaticShippingOnlyFormStillOrders: the regression direction — a
//     STATIC shipping block IS a line item, so a form that sells only that must
//     keep creating an order. The guard is about a cart with no line items, not
//     about shipping blocks.
//
// Skipped unless JOMFORM_TEST_DATABASE_URL is set (forms, sales and the managed
// zone set are real rows).

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// deliveryOnlyRefusal is the buyer-facing sentence the route answers with. Kept
// as a constant here so the test reads the production wording exactly.
// The refusal is English on the wire and the form's language in `message`
// (card t_874021eb): this fixture's form sets no lang, so both read English.
const deliveryOnlyRefusal = "There are no products to pay for"

// deliveryOnlyForm creates a PUBLISHED form in accountID whose builder config
// carries exactly `blocks` — no "products" key at all, which is the shape
// `POST /v1/rest/forms/{id}/publish?acknowledge=1` exists to allow.
func deliveryOnlyForm(t *testing.T, h *PublicHandlers, accountID, slug string, blocks []any) {
	t.Helper()
	if err := h.Forms.Create(context.Background(), &dbstore.Form{
		AccountID: accountID, Slug: slug, Title: "Delivery only", Status: "published",
		Config: map[string]any{"blocks": blocks},
	}); err != nil {
		t.Fatalf("create delivery-only form: %v", err)
	}
}

// deliveryOnlyBody is the payload the buyer's page sends: the shipping block's
// own item and nothing else, plus the answers its controls carry.
func deliveryOnlyBody(fields map[string]string) string {
	body := map[string]any{
		"client":         map[string]any{"full_name": "Ali", "email": "ali@example.test"},
		"items":          []any{map[string]any{"product_id": "ship1", "quantity": 1}},
		"payment_method": "bank_direct",
		"fields":         fields,
	}
	raw, _ := json.Marshal(body)
	return string(raw)
}

// zoneIDsByName reads the account's managed zone set so a test can post the
// zone id the buyer's page actually offers in the shipping block's <select>
// (a made-up id is refused by the resolver as "zon penghantaran tidak sah"
// before the cart ever reaches Create, which would hide the defect).
func zoneIDsByName(t *testing.T, h *PublicHandlers, accountID string) map[string]string {
	t.Helper()
	sets, err := h.Shipping.Sets(context.Background(), []string{accountID})
	if err != nil || len(sets) != 1 {
		t.Fatalf("read zone sets: %v (sets=%d)", err, len(sets))
	}
	out := map[string]string{}
	for _, z := range sets[0].Zones {
		out[z.Name] = z.ID
	}
	return out
}

// assertRefusedNot500 checks the wire: 400, the refusal sentence, and no sale.
func assertRefusedNot500(t *testing.T, h *PublicHandlers, accountID string, code int, body string) {
	t.Helper()
	if code != http.StatusBadRequest {
		t.Fatalf("order = %d, want 400 (%q) — the delivery-only cart must be refused, not answered with an internal error (body %s)",
			code, deliveryOnlyRefusal, body)
	}
	var got struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode refusal body %s: %v", body, err)
	}
	if got.Error != deliveryOnlyRefusal {
		t.Errorf("refusal = %q, want %q (body %s)", got.Error, deliveryOnlyRefusal, body)
	}
	if got.Code == "internal" {
		t.Errorf("refusal body still carries code=internal: %s", body)
	}
	if !strings.Contains(body, deliveryOnlyRefusal) {
		t.Errorf("body does not carry the buyer-facing sentence %q: %s", deliveryOnlyRefusal, body)
	}
	sales, err := h.Sales.ListByAccount(context.Background(), accountID, 10)
	if err != nil {
		t.Fatalf("list sales: %v", err)
	}
	if len(sales) != 0 {
		t.Errorf("%d sale(s) recorded for a refused delivery-only order", len(sales))
	}
}

// The card's own shape: a zone-sourced shipping block is the only pricing
// block, so the posted cart is entirely a fee. Before the fix this was
// `500 {"code":"internal","error":"internal error"}` from sales.items NOT NULL.
func TestDeliveryOnlyZoneFormIsRefusedNotA500(t *testing.T) {
	h, ws, _, setID, accountID, _ := zoneShippingFixture(t, "reject")
	host := ws + ".jomform.com"
	slug := "it-deliv-zone-" + accountID[:8]
	deliveryOnlyForm(t, h, accountID, slug, []any{
		map[string]any{"id": "b_state", "type": "select", "value": "Negeri", "required": true,
			"choices": []any{
				map[string]any{"label": "Peninsular", "value": "peninsular"},
				map[string]any{"label": "Sabah", "value": "sabah"},
			}},
		map[string]any{"id": "ship1", "type": "shipping", "value": "Postage",
			"product_id": "ship1", "required": true, "source": "zones",
			"zone_set_id": setID, "zone_state_field": "b_state"},
	})

	rec := zoneOrderPost(t, h, host, slug, deliveryOnlyBody(map[string]string{
		"b_state": "sabah", "ship1": zoneIDsByName(t, h, accountID)["East Malaysia"],
	}))
	assertRefusedNot500(t, h, accountID, rec.Code, rec.Body.String())
}

// The same cart from the OTHER managed source. feeItemIDs is built from both
// specs, so the refusal cannot be zone-only.
func TestDeliveryOnlyCourierFormIsRefusedNotA500(t *testing.T) {
	h, ws, _, setID, accountID, _ := zoneShippingFixture(t, "reject")
	host := ws + ".jomform.com"
	slug := "it-deliv-courier-" + accountID[:8]
	deliveryOnlyForm(t, h, accountID, slug, []any{
		map[string]any{"id": "b_state", "type": "text", "value": "State", "required": true},
		map[string]any{"id": "ship1", "type": "shipping", "value": "Postage",
			"product_id": "ship1", "required": true, "source": "courier",
			"courier_provider": "lalamove", "zone_set_id": setID,
			"zone_state_field": "b_state"},
	})

	rec := zoneOrderPost(t, h, host, slug, deliveryOnlyBody(map[string]string{
		"b_state": "Sabah", "ship1": zoneIDsByName(t, h, accountID)["East Malaysia"],
	}))
	assertRefusedNot500(t, h, accountID, rec.Code, rec.Body.String())
}

// The regression direction: a STATIC shipping block has no rate source, so its
// own item IS a line item and a form that sells only that still takes an order.
// The new guard must not swallow it.
func TestStaticShippingOnlyFormStillOrders(t *testing.T) {
	h, ws, _, _, accountID, _ := zoneShippingFixture(t, "reject")
	host := ws + ".jomform.com"
	slug := "it-deliv-static-" + accountID[:8]
	deliveryOnlyForm(t, h, accountID, slug, []any{
		map[string]any{"id": "ship1", "type": "shipping", "value": "Postage",
			"product_id": "ship1", "required": true,
			"choices": []any{map[string]any{"label": "Standard", "value": "std", "price_sen": 1500}}},
	})

	sale := saleFor(t, h, host, slug, deliveryOnlyBody(map[string]string{"ship1": "std"}))
	if len(sale.Items) != 1 {
		t.Fatalf("%d line items, want 1 (a static shipping block IS a line item)", len(sale.Items))
	}
	if it := sale.Items[0]; it.ProductID != "ship1" || it.PriceSen != 1500 {
		t.Errorf("line item = %+v, want ship1/1500", it)
	}
	if sale.SubtotalSen != 1500 || sale.ShippingSen != 0 || sale.TotalSen != 1500 {
		t.Errorf("subtotal/shipping/total = %d/%d/%d, want 1500/0/1500 (a static block's price is the subtotal)",
			sale.SubtotalSen, sale.ShippingSen, sale.TotalSen)
	}
}
