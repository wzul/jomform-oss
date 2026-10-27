package httpapi

// Shipping by zone, end to end against a real Postgres: the managed rate source
// (a workspace's zone sets + rate rows) resolved from the buyer's submitted
// address at submit time, landing in sales.shipping_sen and sales.total_sen.
//
// WHAT THESE PROVE, and why each one exists:
//
//   - TestShippingZoneOrderChargesByAddress: the card's headline — the SAME cart
//     from a Peninsular address and an East Malaysian address is charged
//     DIFFERENT fees, and sales.shipping_sen reflects each one (with the fee OUT
//     of the line items, so subtotal + shipping == total).
//   - TestShippingZoneOrderNoMatchIsRefused: an address no zone covers is a 400
//     NAMING the refusal, and no sale is recorded — never a silent 0.
//   - TestShippingZoneOrderFallbackZoneChargesTheFallbackRate: on_no_match=default
//     charges the fallback zone's rate.
//   - TestShippingZoneOrderFreeShippingThreshold: crossing the threshold charges
//     0 AND records free_over_sen + free=true on the sale's shipping_detail.
//   - TestShippingZoneOrderRejectsAZoneThatDisagreesWithTheAddress: the address
//     is the price authority; a disagreeing choice is refused rather than
//     silently re-priced behind the buyer's back.
//   - TestStaticShippingBlockIsUnchanged: the REGRESSION rule — a block with no
//     `source` (every form published before this feature) still resolves its own
//     priced choice as a LINE ITEM, with shipping_sen untouched at 0.
//   - TestCourierSourceIsRefusedNotSilentlyFree: the phase-2 seam is a refusal.
//   - TestShippingZoneBlockRendersTheZoneSetFromTheDatabase: the buyer's page
//     carries the zone options, their prices and the free-over note.
//
// Skipped unless JOMFORM_TEST_DATABASE_URL is set (the configuration is real rows).

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/shipping"
)

// zoneShippingFixture seeds a user + workspace + account + one product, writes a
// managed zone set (Peninsular RM8 free over RM150 / East RM15 / fallback RM50),
// and publishes a form carrying a product block, a state block and a
// zone-sourced shipping block. Returns the handlers, the workspace slug, the
// form slug, the set id, the account id and the product id.
func zoneShippingFixture(t *testing.T, onNoMatch string) (
	h *PublicHandlers, workspaceSlug, formSlug, setID, accountID, prodID string,
) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)

	var userID, workspaceID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-ship-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-ship-ws-' || md5(random()::text), 'it-ship-' || md5(random()::text))
		 RETURNING id, slug`, userID).Scan(&workspaceID, &workspaceSlug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1, 'it-ship-acc-' || md5(random()::text), 'it-ship-' || md5(random()::text), $2)
		 RETURNING id`, userID, workspaceID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	// A flat 1000-sen product, so the free-shipping threshold is reachable by
	// ordering two of them.
	prod := &dbstore.Product{
		AccountID: accountID, Name: "Widget", PriceSen: 1000,
		Type: dbstore.SaleTypeOneTime, Active: true,
	}
	if err := dbstore.NewProductStore(pool).Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	prodID = prod.ID

	// The managed configuration. Prices are integer sen.
	shipStore := dbstore.NewShippingStore(pool)
	sets := []shipping.ZoneSet{{
		Name: "Malaysia", Default: true,
		Zones: []shipping.Zone{
			{Name: "West Malaysia", States: []string{"peninsular"},
				Rates: []shipping.Rate{{Label: "Standard", PriceSen: 800, FreeOverSen: 15000}}},
			{Name: "East Malaysia", States: []string{"east_malaysia"},
				Rates: []shipping.Rate{{Label: "Standard", PriceSen: 1500}}},
			{Name: "Elsewhere", Fallback: true,
				Rates: []shipping.Rate{{Label: "Standard", PriceSen: 5000}}},
		},
	}}
	if err := shipStore.Replace(ctx, accountID, sets); err != nil {
		t.Fatalf("seed shipping config: %v", err)
	}
	all, err := shipStore.Sets(ctx, []string{accountID})
	if err != nil || len(all) != 1 {
		t.Fatalf("read back shipping config: %v (sets=%d)", err, len(all))
	}
	setID = all[0].ID

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

	formSlug = "it-ship-form-" + accountID[:8]
	if err := h.Forms.Create(ctx, &dbstore.Form{
		AccountID: accountID, Slug: formSlug, Title: "Shipping form", Status: "published",
		Config: map[string]any{
			"blocks": []any{
				map[string]any{"id": "b_state", "type": "text", "value": "State"},
				map[string]any{"id": "ship1", "type": "shipping", "value": "Postage",
					"product_id": "ship1", "source": "zones", "zone_set_id": setID,
					"zone_state_field": "b_state", "on_no_match": onNoMatch},
			},
			"products": []any{map[string]any{"id": prodID, "name": "Widget", "price_sen": 1000}},
		},
	}); err != nil {
		t.Fatalf("create form: %v", err)
	}
	return h, workspaceSlug, formSlug, setID, accountID, prodID
}

// zoneOrderBody builds the submit payload a buyer's browser sends: the cart, the
// mapped state block's value and (optionally) the zone the buyer picked.
func zoneOrderBody(prodID, state, chosenZoneID string, qty int) string {
	fields := map[string]string{"b_state": state}
	if chosenZoneID != "" {
		fields["ship1"] = chosenZoneID
	}
	body := map[string]any{
		"client":         map[string]any{"full_name": "Ali", "email": "ali@example.test"},
		"items":          []any{map[string]any{"product_id": prodID, "quantity": qty}, map[string]any{"product_id": "ship1", "quantity": 1}},
		"payment_method": "bank_direct",
		"fields":         fields,
	}
	raw, _ := json.Marshal(body)
	return string(raw)
}

// zoneOrderPost posts to the form's own order route.
func zoneOrderPost(t *testing.T, h *PublicHandlers, host, slug, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/"+slug+"/order", bytes.NewBufferString(body))
	req.Host = host
	req.RemoteAddr = uniqueTestIP() + ":1234"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// saleFor posts an order and returns the recorded sale.
func saleFor(t *testing.T, h *PublicHandlers, host, slug, body string) *dbstore.Sale {
	t.Helper()
	rec := zoneOrderPost(t, h, host, slug, body)
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
	return sale
}

// The card's headline: the same cart, two addresses, two fees — and the fee is
// in shipping_sen, NOT in the line items (so subtotal + shipping == total).
func TestShippingZoneOrderChargesByAddress(t *testing.T) {
	h, ws, slug, setID, _, prodID := zoneShippingFixture(t, "reject")
	host := ws + ".jomform.com"

	west := saleFor(t, h, host, slug, zoneOrderBody(prodID, "Selangor", "", 1))
	east := saleFor(t, h, host, slug, zoneOrderBody(prodID, "Sabah", "", 1))

	if west.ShippingSen != 800 {
		t.Errorf("Peninsular shipping_sen = %d, want 800", west.ShippingSen)
	}
	if east.ShippingSen != 1500 {
		t.Errorf("East Malaysian shipping_sen = %d, want 1500", east.ShippingSen)
	}
	if west.ShippingSen == east.ShippingSen {
		t.Fatal("both addresses were charged the same fee — the zone lookup did not resolve per address")
	}
	for _, tc := range []struct {
		name string
		sale *dbstore.Sale
		want int64
	}{{"west", west, 800}, {"east", east, 1500}} {
		if got := tc.sale.SubtotalSen + tc.sale.ShippingSen - tc.sale.DiscountSen; got != tc.sale.TotalSen {
			t.Errorf("%s: subtotal+shipping-discount = %d, total_sen = %d", tc.name, got, tc.sale.TotalSen)
		}
		if tc.sale.TotalSen != 1000+tc.want {
			t.Errorf("%s: total_sen = %d, want %d", tc.name, tc.sale.TotalSen, 1000+tc.want)
		}
		// The shipping fee is NOT a line item.
		if len(tc.sale.Items) != 1 {
			t.Errorf("%s: %d line items, want 1 (the fee must not be a line item)", tc.name, len(tc.sale.Items))
		}
		for _, it := range tc.sale.Items {
			if it.ProductID == "ship1" {
				t.Errorf("%s: the zone shipping block's item was recorded as a line item", tc.name)
			}
		}
		if tc.sale.SubtotalSen != 1000 {
			t.Errorf("%s: subtotal_sen = %d, want 1000 (items only)", tc.name, tc.sale.SubtotalSen)
		}
	}

	// The sale RECORDS how the fee was reached, so the merchant can see the zone
	// and the rule instead of inferring it from a number.
	var detail struct {
		Blocks []struct {
			BlockID   string `json:"block_id"`
			Source    string `json:"source"`
			ZoneID    string `json:"zone_id"`
			ZoneName  string `json:"zone_name"`
			PriceSen  int64  `json:"price_sen"`
			MatchedBy string `json:"matched_by"`
		} `json:"blocks"`
	}
	if err := json.Unmarshal(west.ShippingDetail, &detail); err != nil {
		t.Fatalf("decode shipping_detail %s: %v", west.ShippingDetail, err)
	}
	if len(detail.Blocks) != 1 {
		t.Fatalf("shipping_detail blocks = %d, want 1 (%s)", len(detail.Blocks), west.ShippingDetail)
	}
	b := detail.Blocks[0]
	if b.BlockID != "ship1" || b.Source != "zones" || b.PriceSen != 800 || b.MatchedBy != "state" {
		t.Errorf("shipping_detail block = %+v, want ship1/zones/800/state", b)
	}
	if b.ZoneName != "West Malaysia" || b.ZoneID == "" {
		t.Errorf("shipping_detail did not name the zone: %+v", b)
	}
	if b.ZoneID == "" {
		t.Errorf("set %s was not recorded on the sale", setID)
	}
	// The east sale names the other zone (one reading, two outcomes).
	var eastDetail struct {
		Blocks []struct {
			ZoneName string `json:"zone_name"`
		} `json:"blocks"`
	}
	if err := json.Unmarshal(east.ShippingDetail, &eastDetail); err != nil {
		t.Fatalf("decode east shipping_detail: %v", err)
	}
	if len(eastDetail.Blocks) != 1 || eastDetail.Blocks[0].ZoneName != "East Malaysia" {
		t.Errorf("east shipping_detail = %s, want zone East Malaysia", east.ShippingDetail)
	}
}

// An address no zone covers is REFUSED, with the resolver's own sentence, and no
// sale exists afterwards. This is the "never a silent 0" rule on the wire.
func TestShippingZoneOrderNoMatchIsRefused(t *testing.T) {
	h, ws, slug, _, accountID, prodID := zoneShippingFixture(t, "reject")
	host := ws + ".jomform.com"

	rec := zoneOrderPost(t, h, host, slug, zoneOrderBody(prodID, "Antarctica", "", 1))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unmatched address = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), shipping.ErrNoZone.Error()) {
		t.Errorf("refusal body should name the reason (%q), got %s", shipping.ErrNoZone.Error(), rec.Body.String())
	}
	// No sale was recorded for the refused order.
	sales, err := h.Sales.ListByAccount(context.Background(), accountID, 10)
	if err != nil {
		t.Fatalf("list sales: %v", err)
	}
	if len(sales) != 0 {
		t.Errorf("%d sale(s) recorded for a refused order", len(sales))
	}
}

// on_no_match=default charges the fallback zone's rate rather than refusing.
func TestShippingZoneOrderFallbackZoneChargesTheFallbackRate(t *testing.T) {
	h, ws, slug, _, _, prodID := zoneShippingFixture(t, "default")
	sale := saleFor(t, h, ws+".jomform.com", slug, zoneOrderBody(prodID, "Antarctica", "", 1))
	if sale.ShippingSen != 5000 {
		t.Errorf("fallback shipping_sen = %d, want 5000", sale.ShippingSen)
	}
	if sale.TotalSen != 6000 {
		t.Errorf("total_sen = %d, want 6000", sale.TotalSen)
	}
	if !strings.Contains(string(sale.ShippingDetail), `"matched_by": "fallback"`) {
		t.Errorf("shipping_detail should record the fallback rule, got %s", sale.ShippingDetail)
	}
}

// Crossing the free-shipping threshold charges 0 and records WHY; below it the
// full rate applies to the same cart.
func TestShippingZoneOrderFreeShippingThreshold(t *testing.T) {
	h, ws, slug, _, _, prodID := zoneShippingFixture(t, "reject")
	host := ws + ".jomform.com"

	// 2 x 1000 = 2000 sen, below the 15000 threshold.
	below := saleFor(t, h, host, slug, zoneOrderBody(prodID, "Selangor", "", 2))
	if below.ShippingSen != 800 {
		t.Errorf("below the threshold: shipping_sen = %d, want 800", below.ShippingSen)
	}
	// 15 x 1000 = 15000 sen, AT the threshold.
	at := saleFor(t, h, host, slug, zoneOrderBody(prodID, "Selangor", "", 15))
	if at.ShippingSen != 0 {
		t.Errorf("at the threshold: shipping_sen = %d, want 0", at.ShippingSen)
	}
	if at.TotalSen != at.SubtotalSen {
		t.Errorf("free shipping: total_sen = %d, subtotal = %d (should be equal)", at.TotalSen, at.SubtotalSen)
	}
	if !strings.Contains(string(at.ShippingDetail), `"free": true`) ||
		!strings.Contains(string(at.ShippingDetail), `"free_over_sen": 15000`) {
		t.Errorf("the free-shipping crossing was not recorded: %s", at.ShippingDetail)
	}
	if !strings.Contains(string(at.ShippingDetail), `"list_price_sen": 800`) {
		t.Errorf("the waived price should stay visible on the sale: %s", at.ShippingDetail)
	}
}

// The address is the price authority: a zone choice that disagrees with it is
// REFUSED (400), not silently re-priced.
func TestShippingZoneOrderRejectsAZoneThatDisagreesWithTheAddress(t *testing.T) {
	h, ws, slug, _, accountID, prodID := zoneShippingFixture(t, "reject")
	host := ws + ".jomform.com"
	ctx := context.Background()

	// Resolve the zones so a real (but WRONG) zone id is submitted.
	sets, err := h.Shipping.Sets(ctx, []string{accountID})
	if err != nil || len(sets) != 1 {
		t.Fatalf("read zones: %v", err)
	}
	var east string
	for _, z := range sets[0].Zones {
		if z.Name == "East Malaysia" {
			east = z.ID
		}
	}
	if east == "" {
		t.Fatal("fixture has no East Malaysia zone")
	}
	rec := zoneOrderPost(t, h, host, slug, zoneOrderBody(prodID, "Selangor", east, 1))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("mismatched zone = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), shipping.ErrZoneMismatch.Error()) {
		t.Errorf("refusal should name the mismatch, got %s", rec.Body.String())
	}
	// The agreeing choice is accepted and charged the address's zone.
	west := ""
	for _, z := range sets[0].Zones {
		if z.Name == "West Malaysia" {
			west = z.ID
		}
	}
	sale := saleFor(t, h, host, slug, zoneOrderBody(prodID, "Selangor", west, 1))
	if sale.ShippingSen != 800 {
		t.Errorf("agreeing choice: shipping_sen = %d, want 800", sale.ShippingSen)
	}
}

// The REGRESSION rule of the card: a shipping block with NO `source` (every form
// published before this feature) keeps resolving its own priced choice list as a
// LINE ITEM, with sales.shipping_sen untouched at 0 — byte-for-byte the old
// shape.
func TestStaticShippingBlockIsUnchanged(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)

	var userID, workspaceID, workspaceSlug, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-static-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-static-ws-' || md5(random()::text), 'it-static-' || md5(random()::text))
		 RETURNING id, slug`, userID).Scan(&workspaceID, &workspaceSlug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1, 'it-static-acc-' || md5(random()::text), 'it-static-' || md5(random()::text), $2)
		 RETURNING id`, userID, workspaceID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	prod := &dbstore.Product{AccountID: accountID, Name: "Widget", PriceSen: 1000,
		Type: dbstore.SaleTypeOneTime, Active: true}
	if err := dbstore.NewProductStore(pool).Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}

	h := &PublicHandlers{
		Forms:             dbstore.NewFormStore(pool),
		Sales:             dbstore.NewSaleStore(pool),
		Accounts:          dbstore.NewAccountStore(pool),
		Products:          dbstore.NewProductStore(pool),
		ProductVariations: dbstore.NewProductVariationStore(pool),
		Workspaces:        dbstore.NewWorkspaceStore(pool),
		// Deliberately NIL: a static block must not need the shipping store at
		// all — that is the strongest statement that the old path is untouched.
		Shipping: nil,
		BaseURL:  "http://localhost:8080",
	}

	// The pre-existing shape: a priced choice list, no `source`, and the
	// block-level price_sen the server resolves the line item from (issue #173).
	slug := "it-static-form-" + accountID[:8]
	if err := h.Forms.Create(ctx, &dbstore.Form{
		AccountID: accountID, Slug: slug, Title: "Static shipping", Status: "published",
		Config: map[string]any{
			"blocks": []any{
				map[string]any{"id": "b_state", "type": "text", "value": "State"},
				map[string]any{"id": "ship1", "type": "shipping", "value": "Postage",
					"product_id": "ship1", "price_sen": 950,
					"choices": []any{
						map[string]any{"label": "West Malaysia", "value": "wm", "price_sen": 800},
						map[string]any{"label": "East Malaysia", "value": "em", "price_sen": 1500},
					}},
			},
			"products": []any{map[string]any{"id": prod.ID, "name": "Widget", "price_sen": 1000}},
		},
	}); err != nil {
		t.Fatalf("create form: %v", err)
	}

	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"` + prod.ID + `","quantity":1},` +
		`{"product_id":"ship1","quantity":1}],"payment_method":"bank_direct",` +
		`"fields":{"b_state":"Selangor"}}`
	sale := saleFor(t, h, workspaceSlug+".jomform.com", slug, body)
	if sale.ShippingSen != 0 {
		t.Errorf("a static block must not set shipping_sen (got %d)", sale.ShippingSen)
	}
	if sale.SubtotalSen != 1950 {
		t.Errorf("subtotal_sen = %d, want 1950 (product 1000 + the block's own 950 line item)", sale.SubtotalSen)
	}
	if sale.TotalSen != 1950 {
		t.Errorf("total_sen = %d, want 1950 (subtotal + shipping(0) - discount(0))", sale.TotalSen)
	}
	if len(sale.Items) != 2 {
		t.Fatalf("line items = %d, want 2 (the static block IS a line item)", len(sale.Items))
	}
	var shipItem *dbstore.SaleItem
	for i := range sale.Items {
		if sale.Items[i].ProductID == "ship1" {
			shipItem = &sale.Items[i]
		}
	}
	if shipItem == nil {
		t.Fatal("the static shipping block's line item disappeared")
	}
	if shipItem.PriceSen != 950 || shipItem.Name != "Postage" {
		t.Errorf("static shipping item = %+v, want the block's own snapshot (Postage/950)", *shipItem)
	}
	// shipping_detail stays the empty object for a sale that resolved no
	// managed rate — the pre-existing shape, not null.
	if strings.TrimSpace(string(sale.ShippingDetail)) != "{}" {
		t.Errorf("static shipping_detail = %q, want {}", string(sale.ShippingDetail))
	}
}

// The phase-2 seam is a refusal, not a quiet free delivery.
func TestCourierSourceIsRefusedNotSilentlyFree(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	var userID, workspaceID, workspaceSlug, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-courier-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-courier-ws-' || md5(random()::text), 'it-courier-' || md5(random()::text))
		 RETURNING id, slug`, userID).Scan(&workspaceID, &workspaceSlug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1, 'it-courier-acc-' || md5(random()::text), 'it-courier-' || md5(random()::text), $2)
		 RETURNING id`, userID, workspaceID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	prod := &dbstore.Product{AccountID: accountID, Name: "Widget", PriceSen: 1000,
		Type: dbstore.SaleTypeOneTime, Active: true}
	if err := dbstore.NewProductStore(pool).Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	h := &PublicHandlers{
		Forms: dbstore.NewFormStore(pool), Sales: dbstore.NewSaleStore(pool),
		Accounts: dbstore.NewAccountStore(pool), Products: dbstore.NewProductStore(pool),
		ProductVariations: dbstore.NewProductVariationStore(pool),
		Workspaces:        dbstore.NewWorkspaceStore(pool),
		Shipping:          dbstore.NewShippingStore(pool),
	}
	slug := "it-courier-form-" + accountID[:8]
	if err := h.Forms.Create(ctx, &dbstore.Form{
		AccountID: accountID, Slug: slug, Title: "Courier shipping", Status: "published",
		Config: map[string]any{
			"blocks": []any{
				map[string]any{"id": "ship1", "type": "shipping", "value": "Postage",
					"product_id": "ship1", "source": "courier"},
			},
			"products": []any{map[string]any{"id": prod.ID, "name": "Widget", "price_sen": 1000}},
		},
	}); err != nil {
		t.Fatalf("create form: %v", err)
	}
	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"` + prod.ID + `","quantity":1}],"payment_method":"bank_direct"}`
	rec := zoneOrderPost(t, h, workspaceSlug+".jomform.com", slug, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("courier source = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), shipping.ErrNotSupported.Error()) {
		t.Errorf("courier refusal should say the source is not supported yet, got %s", rec.Body.String())
	}
}

// The buyer's page carries the managed zones: their names, prices and the
// free-over note, in a select the browser requires.
func TestShippingZoneBlockRendersTheZoneSetFromTheDatabase(t *testing.T) {
	h, ws, slug, _, _, _ := zoneShippingFixture(t, "reject")

	code, body := getForm(t, h, ws+".jomform.com", slug)
	if code != http.StatusOK {
		t.Fatalf("render = %d, want 200", code)
	}
	for _, want := range []string{
		`<select class="ship" data-id="ship1" required>`,
		"West Malaysia",
		"East Malaysia",
		"RM8.00",
		"RM15.00",
		`data-price="800"`,
		`data-price="1500"`,
		`data-free-over="15000"`,
		"Free over RM150.00",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("zone-sourced shipping select missing %q", want)
		}
	}
	// The fallback zone is NOT offered when the block rejects unmatched addresses.
	if strings.Contains(body, "Elsewhere") {
		t.Error("the fallback zone was offered as a buyer choice under on_no_match=reject")
	}
}
