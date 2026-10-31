package httpapi

// Card t_b6e75ebb: a pricing block stored WITHOUT `product_id` must still be
// buyable. The write paths accept such a config (the unpriced-block rule has
// nothing to say about it, and a zone/courier-sourced shipping block is exempt
// from that rule by design), so the renderer is where the two halves of the
// contract have to agree: the submit key of an option/shipping block IS its
// product_id, and the submit path already falls back to the block's own id
// (httpapi/shipping_blocks.go: shippingBlocksWhere, and formfields.PricingContract
// keys its required entry the same way).
//
// THE DEFECT this file pins: the renderer copied `product_id` verbatim into
// `data-id`, so an agent-authored config that omitted it painted
// `<select class="ship" data-id="">`, the page's own submit handler posted
// `items:[{product_id:""}]` / `fields:{"": …}`, and every order from the
// published page answered 400 "produk tidak sah: " — a published merchant form
// that could not take money.
//
// These are integration tests (a real Postgres, real zone rows) and are skipped
// unless JOMFORM_TEST_DATABASE_URL is set.

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// pricingSelectDataIDs reads the data-id of each pricing <select> out of a
// rendered form page — the SAME attribute the page's submit handler keys items
// and fields by, so these assertions are the buyer's contract, not the markup's.
func pricingSelectDataIDs(body string) map[string]string {
	re := regexp.MustCompile(`<select class="(opt|ship)[^"]*" data-id="([^"]*)"`)
	out := map[string]string{}
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		out[m[1]] = m[2]
	}
	return out
}

// zoneOptionValue returns the value of the rendered shipping option whose label
// carries name (e.g. "West Malaysia") — the zone id the buyer's browser posts.
func zoneOptionValue(t *testing.T, body, name string) string {
	t.Helper()
	re := regexp.MustCompile(`(?s)<select class="ship[^"]*"[^>]*>(.*?)</select>`)
	m := re.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no shipping select in the rendered page")
	}
	opt := regexp.MustCompile(`value="([^"]+)"[^>]*>[^<]*` + regexp.QuoteMeta(name))
	if om := opt.FindStringSubmatch(m[1]); om != nil {
		return om[1]
	}
	t.Fatalf("no %q option in the rendered shipping select: %s", name, m[1])
	return ""
}

// productIDLessForm publishes a form whose option and shipping blocks carry NO
// `product_id` — the shape the REST and MCP write paths accept verbatim.
func productIDLessForm(t *testing.T, h *PublicHandlers, accountID, setID, slug, shipSource string) {
	t.Helper()
	ship := map[string]any{
		"id": "ship1", "type": "shipping", "value": "Penghantaran",
		"source": shipSource, "zone_set_id": setID,
		"zone_state_field": "b_state", "on_no_match": "reject",
	}
	if shipSource == "courier" {
		ship["courier_provider"] = "lalamove"
	}
	if err := h.Forms.Create(context.Background(), &dbstore.Form{
		AccountID: accountID, Slug: slug, Title: "No product_id", Status: "published",
		Config: map[string]any{
			"mode": "payment",
			"blocks": []any{
				map[string]any{"id": "item1", "type": "option", "value": "Item", "required": true,
					"choices": []any{map[string]any{"label": "One", "value": "one", "price_sen": 1000}}},
				map[string]any{"id": "b_state", "type": "text", "value": "Negeri", "required": true},
				ship,
			},
		},
	}); err != nil {
		t.Fatalf("create product_id-less form %q: %v", slug, err)
	}
}

// The buyer's page must key BOTH pricing controls by the block's own id — the
// value the submit path and the required-field contract already use.
func TestPricingBlockWithoutProductIDRendersItsBlockID(t *testing.T) {
	h, ws, _, setID, accountID, _ := zoneShippingFixture(t, "reject")
	host := ws + ".jomform.com"

	for _, src := range []string{"zones", "courier"} {
		slug := "it-nopid-" + src + "-" + accountID[:8]
		productIDLessForm(t, h, accountID, setID, slug, src)

		code, body := getForm(t, h, host, slug)
		if code != http.StatusOK {
			t.Fatalf("%s render = %d, want 200", src, code)
		}
		ids := pricingSelectDataIDs(body)
		if ids["opt"] != "item1" {
			t.Errorf("%s: option block rendered data-id=%q, want the block id \"item1\"", src, ids["opt"])
		}
		if ids["ship"] != "ship1" {
			t.Errorf("%s: shipping block rendered data-id=%q, want the block id \"ship1\"", src, ids["ship"])
		}
		// An empty pricing id is exactly what made the page post
		// product_id:"" and the server answer 400 "produk tidak sah: ".
		if strings.Contains(body, `class="opt" data-id=""`) || strings.Contains(body, `class="ship" data-id=""`) {
			t.Errorf("%s: a pricing control still carries an empty data-id", src)
		}
	}
}

// The card's "fixed means": the form's OWN page, submitted exactly as the
// browser submits it (data-id read back out of the markup), takes an order whose
// shipping_sen is the zone rate — never 400 "produk tidak sah".
func TestPricingBlockWithoutProductIDTakesAnOrder(t *testing.T) {
	h, ws, _, setID, accountID, _ := zoneShippingFixture(t, "reject")
	slug := "it-nopid-order-" + accountID[:8]
	productIDLessForm(t, h, accountID, setID, slug, "zones")
	host := ws + ".jomform.com"

	code, body := getForm(t, h, host, slug)
	if code != http.StatusOK {
		t.Fatalf("render = %d, want 200", code)
	}
	ids := pricingSelectDataIDs(body)
	zoneID := zoneOptionValue(t, body, "West Malaysia")

	// The page's own submit body: one item per answered pricing control, and
	// every answer keyed by that control's data-id.
	payload := map[string]any{
		"client": map[string]any{"full_name": "Ali", "email": "ali@example.test"},
		"items": []any{
			map[string]any{"product_id": ids["opt"], "quantity": 1},
			map[string]any{"product_id": ids["ship"], "quantity": 1},
		},
		"fields": map[string]string{
			"b_state":   "Selangor",
			ids["opt"]:  "one",
			ids["ship"]: zoneID,
		},
		"payment_method": "bank_direct",
	}
	raw, _ := json.Marshal(payload)

	rec := zoneOrderPost(t, h, host, slug, string(raw))
	if rec.Code != http.StatusCreated {
		t.Fatalf("order = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		RefCode string `json:"ref_code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	sale, err := h.Sales.ByRefCode(context.Background(), resp.RefCode)
	if err != nil {
		t.Fatalf("fetch sale: %v", err)
	}
	if sale.SubtotalSen != 1000 {
		t.Errorf("subtotal_sen = %d, want 1000", sale.SubtotalSen)
	}
	if sale.ShippingSen != 800 {
		t.Errorf("shipping_sen = %d, want 800 (the West Malaysia zone rate)", sale.ShippingSen)
	}
	if sale.TotalSen != 1800 {
		t.Errorf("total_sen = %d, want 1800", sale.TotalSen)
	}
	if len(sale.Items) != 1 || sale.Items[0].ProductID != ids["opt"] {
		t.Errorf("line items = %+v, want the option block %q only (the shipping item is the fee)", sale.Items, ids["opt"])
	}
}
