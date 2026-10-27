package httpapi

// Card t_0176fb17 — a payment form may carry MORE THAN ONE shipping block, and
// the buyer's page read exactly one of them.
//
// THE DEFECT (measured on the public page, pre-existing on main): the page's own
// script resolved `document.querySelector('select.ship')` — singular. Everything
// downstream of that one element was therefore single-block:
//
//   - `shipPrice()` returned only that block's fee, so the on-page Total counted
//     one shipping charge while the server charges every block (a static block's
//     fee is a line item inside the subtotal; a zone-sourced block's fee is added
//     to sales.shipping_sen by resolveZoneShipping, which loops EVERY zone spec);
//   - only that one select got a `change` listener, so choosing the second block
//     left the Total stale as well;
//   - the submit payload pushed only that one select, so a second STATIC shipping
//     block's line item was never posted and the server silently charged less
//     than the page showed.
//
// WHAT THESE PROVE:
//
//   - TestPaymentPageSumsEveryShippingBlock (no database): the served page is
//     run in the goja+cascadia harness, both shipping selects are driven, and the
//     Total must be product + BOTH fees, with both block items in the posted
//     body. This is the bite test — with the singular querySelector it reads
//     RM15.00 and posts one shipping item.
//   - TestTwoShippingBlocksChargedTotalMatchesPageTotal (real Postgres): the same
//     shape (one static block + one zone-sourced block) end to end — the Total
//     the page's own script paints must equal sales.total_sen the order route
//     records, and the static block's fee must be inside subtotal while the zone
//     block's is inside shipping_sen. This settles the "is it display-only or a
//     displayed-vs-charged mismatch" question the card left open.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/shipping"
)

// twoShippingBlocksConfig is a payment form with TWO independent static shipping
// blocks — the cheapest shape the defect needs and the one that proves the money
// half: each static block is its own line item, so a dropped item is an
// undercharge, not just a cosmetic total.
func twoShippingBlocksConfig() map[string]any {
	ship := func(id, label string, choices [][3]any) map[string]any {
		cs := make([]any, 0, len(choices))
		for _, c := range choices {
			cs = append(cs, map[string]any{"label": c[0], "value": c[1], "price_sen": c[2]})
		}
		return map[string]any{
			"id": id, "type": "shipping", "value": label,
			"product_id": id, "choices": cs,
		}
	}
	return map[string]any{
		"mode": "payment",
		"blocks": []any{
			ship("ship1", "Postage", [][3]any{
				{"Standard", "std", 500},
				{"Express", "exp", 1500},
			}),
			ship("ship2", "Delivery", [][3]any{
				{"Same day", "sd", 800},
			}),
		},
		"products": []any{},
	}
}

// chooseSelectOption marks an option as the buyer's choice the way clicking it
// does in a browser: the option becomes `selected` and the select takes its
// value. The change event is left to the caller so the test fires it explicitly.
func chooseSelectOption(t *testing.T, d *menuDOM, sel *html.Node, value string) {
	t.Helper()
	found := false
	for c := sel.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode || c.Data != "option" {
			continue
		}
		if menuAttr(c, "value") == value {
			found = true
			setMenuAttr(c, "selected", "")
		} else {
			removeMenuAttr(c, "selected")
		}
	}
	if !found {
		t.Fatalf("<select class=ship data-id=%q> does not offer option %q", menuAttr(sel, "data-id"), value)
	}
	setMenuAttr(sel, "value", value)
}

// shipSelects returns every shipping control the served page carries, in
// document order.
func shipSelects(d *menuDOM) []*html.Node {
	return cascadia.QueryAll(d.doc, cascadia.MustCompile("select.ship"))
}

// postedItemIDs decodes the product_ids of the order body the page posted.
func postedItemIDs(t *testing.T, posted string) []string {
	t.Helper()
	var body struct {
		Items []struct {
			ProductID string `json:"product_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(posted), &body); err != nil {
		t.Fatalf("the page posted unparsable JSON: %v (%s)", err, posted)
	}
	out := make([]string, 0, len(body.Items))
	for _, it := range body.Items {
		out = append(out, it.ProductID)
	}
	return out
}

// TestPaymentPageSumsEveryShippingBlock is the bite test: two static shipping
// blocks, both driven, and BOTH must reach the Total and the posted items.
func TestPaymentPageSumsEveryShippingBlock(t *testing.T) {
	body := renderPaymentPage(t, "pf-two-ship", twoShippingBlocksConfig())

	d := newPageDOM(t, body)
	d.orderResponse = map[string]any{"checkout_url": "https://pay.example.test/x"}
	d.runPageScript(body, "live('[name=full_name]')")

	ships := shipSelects(d)
	if len(ships) != 2 {
		t.Fatalf("the served page carries %d shipping controls, want 2 — the form has two shipping blocks", len(ships))
	}

	chooseSelectOption(t, d, ships[0], "std") // RM5.00
	d.dispatch("change", ships[0])
	chooseSelectOption(t, d, ships[1], "sd") // RM8.00
	d.dispatch("change", ships[1])

	// RM5.00 postage + RM8.00 delivery.
	if got := d.formTotal(); got != "RM13.00" {
		t.Errorf("the page's Total = %q, want %q (ship1 500 + ship2 800). The page "+
			"sums the shipping blocks it finds; one block is not the form's shipping charge.",
			got, "RM13.00")
	}

	d.dispatch("submit", d.byID["of"])
	posted := d.storage["__posted_body"]
	if strings.TrimSpace(posted) == "" {
		t.Fatalf("the page posted no order body (requests: %v)", d.fetches)
	}
	got := postedItemIDs(t, posted)
	for _, want := range []string{"ship1", "ship2"} {
		seen := false
		for _, id := range got {
			if id == want {
				seen = true
			}
		}
		if !seen {
			t.Errorf("the posted items %v omit %q. A static shipping block is charged as ITS OWN line "+
				"item, so a dropped control is an undercharge: the server only charges the blocks the "+
				"page actually posted.", got, want)
		}
	}
}

// twoBlockShippingFixture seeds a workspace/account/product, one managed zone
// set (West Malaysia RM8.00, free over RM150.00), and a published payment form
// carrying a product block, a state block, an email block, a STATIC shipping
// block (Standard RM5.00) and a ZONE-SOURCED shipping block — the shape the
// card's measurement used.
func twoBlockShippingFixture(t *testing.T) (h *PublicHandlers, host, slug string) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)

	var userID, workspaceID, workspaceSlug, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-twoship-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-twoship-ws-' || md5(random()::text), 'it-twoship-' || md5(random()::text))
		 RETURNING id, slug`, userID).Scan(&workspaceID, &workspaceSlug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1, 'it-twoship-acc-' || md5(random()::text), 'it-twoship-' || md5(random()::text), $2)
		 RETURNING id`, userID, workspaceID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	prod := &dbstore.Product{AccountID: accountID, Name: "Widget", PriceSen: 1000,
		Type: dbstore.SaleTypeOneTime, Active: true}
	if err := dbstore.NewProductStore(pool).Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}

	shipStore := dbstore.NewShippingStore(pool)
	if err := shipStore.Replace(ctx, accountID, []shipping.ZoneSet{{
		Name: "Malaysia", Default: true,
		Zones: []shipping.Zone{
			{Name: "West Malaysia", States: []string{"peninsular"},
				Rates: []shipping.Rate{{Label: "Standard", PriceSen: 800, FreeOverSen: 15000}}},
		},
	}}); err != nil {
		t.Fatalf("seed shipping config: %v", err)
	}
	all, err := shipStore.Sets(ctx, []string{accountID})
	if err != nil || len(all) != 1 {
		t.Fatalf("read back shipping config: %v (sets=%d)", err, len(all))
	}

	h = &PublicHandlers{
		Forms:             dbstore.NewFormStore(pool),
		Sales:             dbstore.NewSaleStore(pool),
		Accounts:          dbstore.NewAccountStore(pool),
		Products:          dbstore.NewProductStore(pool),
		ProductVariations: dbstore.NewProductVariationStore(pool),
		Workspaces:        dbstore.NewWorkspaceStore(pool),
		Shipping:          shipStore,
		BaseURL:           "http://localhost:8080",
	}
	host = workspaceSlug + ".jomform.com"
	slug = "it-twoship-form-" + accountID[:8]
	if err := h.Forms.Create(ctx, &dbstore.Form{
		AccountID: accountID, Slug: slug, Title: "Two shipping blocks", Status: "published",
		Config: map[string]any{
			"blocks": []any{
				map[string]any{"id": "p1", "type": "product", "product_id": prod.ID, "value": "Widget"},
				map[string]any{"id": "em1", "type": "email", "value": "Emel", "required": true},
				map[string]any{"id": "b_state", "type": "text", "value": "State"},
				map[string]any{"id": "ship1", "type": "shipping", "value": "Postage",
					"product_id": "ship1",
					"choices": []any{
						map[string]any{"label": "Standard", "value": "std", "price_sen": 500},
					}},
				map[string]any{"id": "ship2", "type": "shipping", "value": "Delivery",
					"product_id": "ship2", "source": "zones", "zone_set_id": all[0].ID,
					"zone_state_field": "b_state"},
			},
			"products": []any{map[string]any{"id": prod.ID, "name": "Widget", "price_sen": 1000}},
		},
	}); err != nil {
		t.Fatalf("create form: %v", err)
	}
	return h, host, slug
}

// TestTwoShippingBlocksChargedTotalMatchesPageTotal closes the card's open
// question end to end: the Total the buyer's page paints for a two-block form is
// the total the order route actually records.
func TestTwoShippingBlocksChargedTotalMatchesPageTotal(t *testing.T) {
	h, host, slug := twoBlockShippingFixture(t)

	code, body := getForm(t, h, host, slug)
	if code != http.StatusOK {
		t.Fatalf("render = %d, want 200 (body %.400s)", code, body)
	}

	d := newPageDOM(t, body)
	d.orderResponse = map[string]any{"checkout_url": "https://pay.example.test/x"}
	d.runPageScript(body, "live('[name=full_name]')")

	ships := shipSelects(d)
	if len(ships) != 2 {
		t.Fatalf("the served page carries %d shipping controls, want 2 (static + zone-sourced)", len(ships))
	}
	box := d.find(func(n *html.Node) bool { return n.Data == "input" && menuHasClass(n, "p") })
	if box == nil {
		t.Fatal("the served page has no product checkbox")
	}
	setMenuAttr(box, "checked", "")
	d.dispatch("change", box)

	// The buyer's identity and address, as the page's own controls carry them.
	setMenuAttr(identityControl(t, d, "em1"), "value", "ali@example.test")
	setMenuAttr(identityControl(t, d, "b_state"), "value", "Selangor")

	chooseSelectOption(t, d, ships[0], "std") // static: RM5.00
	d.dispatch("change", ships[0])

	// The zone-sourced block offers the zone SET's zones; pick West Malaysia
	// (RM8.00) by its label, the way a buyer reads it.
	var west *html.Node
	for c := ships[1].FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "option" &&
			strings.Contains(nodeText(c), "West Malaysia") {
			west = c
		}
	}
	if west == nil {
		t.Fatalf("the zone-sourced shipping block does not offer West Malaysia: %.300s", nodeText(ships[1]))
	}
	chooseSelectOption(t, d, ships[1], menuAttr(west, "value"))
	d.dispatch("change", ships[1])

	// Widget RM10.00 + postage RM5.00 + West Malaysia delivery RM8.00.
	const wantTotal = "RM23.00"
	if got := d.formTotal(); got != wantTotal {
		t.Fatalf("the page's Total = %q, want %q — with one shipping control read, the second block's "+
			"fee never reaches the buyer's total", got, wantTotal)
	}

	d.dispatch("submit", d.byID["of"])
	posted := d.storage["__posted_body"]
	if strings.TrimSpace(posted) == "" {
		t.Fatalf("the page posted no order body (requests: %v)", d.fetches)
	}

	// The page embeds an anti-spam render timestamp; a scripted submit is
	// instantaneous, so drop it from the replayed body. Nothing about shipping
	// depends on it (the server skips the time check when it is absent).
	var payload map[string]any
	if err := json.Unmarshal([]byte(posted), &payload); err != nil {
		t.Fatalf("the page posted unparsable JSON: %v (%s)", err, posted)
	}
	// The page always asks for the CHIP gateway; this test is about the amount,
	// not the provider, and no payments service is wired here. bank_direct
	// records the sale on the same code path (every line-item and shipping rule
	// above runs before the method branch) and returns its ref_code.
	payload["payment_method"] = "bank_direct"
	if fields, ok := payload["fields"].(map[string]any); ok {
		delete(fields, antiSpamRenderField)
	}
	replay, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("re-encode the page's own body: %v", err)
	}

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/"+slug+"/order", bytes.NewReader(replay))
	req.Host = host
	req.RemoteAddr = uniqueTestIP() + ":1234"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("the order the page posted was rejected: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		RefCode string `json:"ref_code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode order response: %v", err)
	}
	sale, err := h.Sales.ByRefCode(context.Background(), resp.RefCode)
	if err != nil {
		t.Fatalf("fetch sale: %v", err)
	}

	// A static block's fee is inside the subtotal (its own line item); a
	// zone-sourced block's fee is shipping_sen. Both are part of the total.
	if sale.SubtotalSen != 1500 {
		t.Errorf("subtotal_sen = %d, want 1500 (product 1000 + the static block's 500 line item)", sale.SubtotalSen)
	}
	if sale.ShippingSen != 800 {
		t.Errorf("shipping_sen = %d, want 800 (the zone-sourced block's West Malaysia rate)", sale.ShippingSen)
	}
	if sale.TotalSen != 2300 {
		t.Errorf("total_sen = %d, want 2300", sale.TotalSen)
	}
	if got, want := d.formTotal(), "RM"+senLabel(sale.TotalSen)[2:]; got != want {
		t.Errorf("the buyer's page showed %q but the order was recorded at %q — a displayed-vs-charged "+
			"mismatch", got, want)
	}
}
