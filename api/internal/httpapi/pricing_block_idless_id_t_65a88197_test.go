package httpapi

// Card t_65a88197 — a pricing block with NO `id` at all:
// `{"type":"option","value":"Item","required":true,"choices":[{"label":"One",
// "value":"one","price_sen":1000}]}`.
//
// The write doors accept it (urlguard.CheckConfigShape only requires `blocks` to
// be an array) and, since t_b6e75ebb, the published page posts its POSITIONAL
// submit key — formfields.SubmitKeys, "_block_<i>" for a block that carries no
// id — as items[].product_id. The charge side did not agree: lookupProduct's
// block arm matched `b.ID == productID` only, and an id-less block's b.ID is
// "", so the order route answered 400 `produk tidak sah: _block_0` and recorded
// no sale. A storable, published form that cannot take money.
//
// THE FIX IS ONE KEY, FOUR READERS. formfields.PricingKey (the block's
// product_id, else its submit key) is what the renderer paints as data-id, what
// PricingContract validates `required` under and what shippingBlocksWhere drops
// a server-priced block's item under. It is now also what lookupProduct charges
// and what the renderer derives unconditionally, so the page and the charge
// cannot disagree for a block with no id — nor for a product_id that is only
// whitespace, which used to paint data-id=" " while every server half trimmed
// it.
//
// These arms are the card's own repro (the in-package lookupProduct line), the
// fee-side identity for an id-less sourced shipping block, and the end-to-end
// shape: a form created through the REST door with id-less pricing blocks,
// published through the REST door, then ordered with the page's own body.
//
// Integration arms need a real Postgres and are skipped when
// JOMFORM_TEST_DATABASE_URL is unset.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/shipping"
)

// TestLookupProductChargesAnIdlessPricingBlock is the card's repro line. Before
// the fix this answered name="" price=0 — which order.go turns into 400
// "produk tidak sah: _block_0" — and the explicit-0 answer below is what proves
// the block is now RESOLVED rather than merely tolerated.
func TestLookupProductChargesAnIdlessPricingBlock(t *testing.T) {
	h := &PublicHandlers{}
	idless := &dbstore.Form{Config: map[string]any{"blocks": []any{
		map[string]any{
			"type": "option", "value": "Item", "required": true,
			"choices": []any{map[string]any{"label": "One", "value": "one", "price_sen": 1000}},
		},
	}}}

	name, price, err := h.lookupProduct(context.Background(), "", idless, "_block_0", "one")
	if err != nil {
		t.Fatalf("lookupProduct(id-less block, _block_0, \"one\") = %v, want the choice priced: "+
			"the page posts the block's positional submit key, so the charge side has to key it the same way", err)
	}
	if name != "Item" || price != 1000 {
		t.Fatalf("lookupProduct(id-less block, _block_0, \"one\") = (%q, %d), want (\"Item\", 1000). "+
			"(\"\", 0) is the carded defect: order.go refuses it with 400 produk tidak sah: _block_0", name, price)
	}

	// The fix keys the block; it does not make any answer priceable. A value
	// that names no choice of the block is still refused.
	if _, _, err := h.lookupProduct(context.Background(), "", idless, "_block_0", "two"); err == nil {
		t.Fatal("lookupProduct accepted an answer naming no choice of the id-less block — a forged value must not be priced")
	}

	// Control: the same block WITH an id is unchanged — it resolves under its id
	// (the builder's shape) and under a product_id the config declares.
	withID := &dbstore.Form{Config: map[string]any{"blocks": []any{
		map[string]any{
			"id": "item1", "type": "option", "value": "Item",
			"choices": []any{map[string]any{"label": "One", "value": "one", "price_sen": 1000}},
		},
	}}}
	if name, price, err := h.lookupProduct(context.Background(), "", withID, "item1", "one"); err != nil || name != "Item" || price != 1000 {
		t.Fatalf("control: lookupProduct(with id \"item1\") = (%q, %d, %v), want (\"Item\", 1000, nil)", name, price, err)
	}
	withProductID := &dbstore.Form{Config: map[string]any{"blocks": []any{
		map[string]any{
			"id": "blk", "product_id": "opt-key", "type": "option", "value": "Item",
			"choices": []any{map[string]any{"label": "One", "value": "one", "price_sen": 1000}},
		},
	}}}
	if name, price, err := h.lookupProduct(context.Background(), "", withProductID, "opt-key", "one"); err != nil || name != "Item" || price != 1000 {
		t.Fatalf("control: lookupProduct(product_id \"opt-key\") = (%q, %d, %v), want (\"Item\", 1000, nil) — "+
			"product_id is the key the renderer paints, so it has to be the key that charges", name, price, err)
	}
}

// TestShippingBlocksGiveAnIdlessSourcedBlockItsSubmitKey is the fee side. A
// zone/courier-sourced shipping block was skipped outright when its id was
// empty (`if id == "" { continue }`), so nothing dropped its submitted item:
// the order tried to price "_block_1" as a product and answered 400 instead of
// resolving the fee from the buyer's address.
func TestShippingBlocksGiveAnIdlessSourcedBlockItsSubmitKey(t *testing.T) {
	cfg := map[string]any{"blocks": []any{
		map[string]any{"type": "text", "value": "Negeri"},
		map[string]any{"type": "shipping", "value": "Postage", "source": "zones", "zone_set_id": "set-1"},
	}}

	specs := zoneShippingBlocks(cfg)
	if len(specs) != 1 {
		t.Fatalf("zoneShippingBlocks = %+v, want the id-less sourced block", specs)
	}
	if specs[0].ID != "_block_1" || specs[0].ItemID != "_block_1" {
		t.Fatalf("id-less sourced block keyed ID=%q ItemID=%q, want both \"_block_1\" (its positional submit key)",
			specs[0].ID, specs[0].ItemID)
	}
	if !shippingFeeItemIDs(specs, nil)["_block_1"] {
		t.Fatal("the id-less sourced block's item is not in feeItemIDs, so the order would try to PRICE it instead of dropping it for the resolved fee")
	}

	// Control: a block with an id is unchanged.
	withID := map[string]any{"blocks": []any{
		map[string]any{"id": "ship1", "type": "shipping", "value": "Postage", "source": "zones", "zone_set_id": "set-1"},
	}}
	if specs := zoneShippingBlocks(withID); len(specs) != 1 || specs[0].ID != "ship1" || specs[0].ItemID != "ship1" {
		t.Fatalf("control: zoneShippingBlocks(with id) = %+v, want ID/ItemID \"ship1\"", specs)
	}

	// A STATIC id-less shipping block keeps its own item (that is where its fee
	// is charged from), and it must not be picked up as a server-resolved fee.
	staticCfg := map[string]any{"blocks": []any{
		map[string]any{"type": "shipping", "value": "Postage", "choices": []any{
			map[string]any{"label": "Standard", "value": "std", "price_sen": 800},
		}},
	}}
	if specs := zoneShippingBlocks(staticCfg); len(specs) != 0 {
		t.Fatalf("a STATIC id-less shipping block was read as server-priced: %+v", specs)
	}
}

// TestWhitespaceProductIDRendersTheBlockKey pins the renderer hunk's other
// consequence, because the derivation is now unconditional: a product_id that is
// only WHITESPACE is ABSENT (every server half trims it — ShippingBlocksWhere and
// PricingContract always did), so the page must paint the block's own key. Before
// the fix it painted data-id=" ", the page posted " " as items[].product_id, and
// the server priced a key it had trimmed away.
func TestWhitespaceProductIDRendersTheBlockKey(t *testing.T) {
	fx := newRenameFixture(t, "it-noi-pad-"+randHex(4))
	slug := "it-pad-" + randHex(4)

	code, raw := tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/forms",
		modeReqBody(t, map[string]any{"title": "Padded product_id", "slug": slug, "config": map[string]any{
			"mode": "payment",
			"blocks": []any{
				map[string]any{"id": "item1", "product_id": " ", "type": "option", "value": "Item",
					"choices": []any{map[string]any{"label": "One", "value": "one", "price_sen": 1000}}},
			},
		}}))
	if code != http.StatusCreated {
		t.Fatalf("create = %d, want 201 (body %s)", code, raw)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &created); err != nil || created.ID == "" {
		t.Fatalf("decode create response: %v (%s)", err, raw)
	}
	if code, raw = tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/forms/"+created.ID+"/publish?acknowledge=1", ""); code != http.StatusOK {
		t.Fatalf("publish = %d, want 200 (body %s)", code, raw)
	}

	pub := &PublicHandlers{
		Forms:             dbstore.NewFormStore(fx.pool),
		Sales:             dbstore.NewSaleStore(fx.pool),
		Accounts:          dbstore.NewAccountStore(fx.pool),
		Products:          dbstore.NewProductStore(fx.pool),
		ProductVariations: dbstore.NewProductVariationStore(fx.pool),
		Workspaces:        dbstore.NewWorkspaceStore(fx.pool),
		BaseURL:           "http://localhost:8080",
	}
	code, body := getForm(t, pub, fx.host, slug)
	if code != http.StatusOK {
		t.Fatalf("render = %d, want 200", code)
	}
	if got := pricingSelectDataIDs(body)["opt"]; got != "item1" {
		t.Fatalf("option block rendered data-id=%q, want %q — a whitespace-only product_id is absent, "+
			"and the block's own key is what every server half already validates and charges", got, "item1")
	}
}

// TestIdlessPricingBlockStoredThroughRESTTakesAnOrder is the card's acceptance
// arm, end to end and through the door the card names: POST /v1/rest/forms with
// an option block that has NO id and a zone-sourced shipping block that has NO
// id, POST .../publish, then submit the page's own body (every data-id read
// back out of the rendered markup) and read the SALE, never the reply.
func TestIdlessPricingBlockStoredThroughRESTTakesAnOrder(t *testing.T) {
	fx := newRenameFixture(t, "it-noid-"+randHex(4))

	// The managed zone set the shipping block points at (the same shape
	// zoneShippingFixture seeds): West Malaysia RM8.00.
	shipStore := dbstore.NewShippingStore(fx.pool)
	if err := shipStore.Replace(fx.ctx, fx.accountID, []shipping.ZoneSet{{
		Name: "Malaysia", Default: true,
		Zones: []shipping.Zone{
			{Name: "West Malaysia", States: []string{"peninsular"},
				Rates: []shipping.Rate{{Label: "Standard", PriceSen: 800}}},
		},
	}}); err != nil {
		t.Fatalf("seed shipping config: %v", err)
	}
	sets, err := shipStore.Sets(fx.ctx, []string{fx.accountID})
	if err != nil || len(sets) != 1 {
		t.Fatalf("read back shipping config: %v (sets=%d)", err, len(sets))
	}
	setID := sets[0].ID

	slug := "it-noid-order-" + randHex(4)
	// The config the card describes: an option block with no `id` (a priced
	// choice, so the unpriced-block rule has nothing to say), a real state
	// block, and a zone-sourced shipping block with no `id`.
	cfg := map[string]any{
		"mode": "payment",
		"blocks": []any{
			map[string]any{"type": "option", "value": "Item", "required": true,
				"choices": []any{map[string]any{"label": "One", "value": "one", "price_sen": 1000}}},
			map[string]any{"id": "b_state", "type": "text", "value": "Negeri", "required": true},
			map[string]any{"type": "shipping", "value": "Penghantaran", "required": true,
				"source": "zones", "zone_set_id": setID,
				"zone_state_field": "b_state", "on_no_match": "reject"},
		},
	}
	code, raw := tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/forms",
		modeReqBody(t, map[string]any{"title": "Id-less pricing blocks", "slug": slug, "config": cfg}))
	if code != http.StatusCreated {
		t.Fatalf("POST /v1/rest/forms with id-less pricing blocks = %d, want 201 (body %s)\n"+
			"The card's acceptance is either a 400 naming the block or a chargeable form; this must not be a silent 5xx.", code, raw)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &created); err != nil || created.ID == "" {
		t.Fatalf("decode create response: %v (%s)", err, raw)
	}

	// The door stored the config it was handed — no id was invented for the
	// pricing blocks (if it were, the charge would be keyed differently from a
	// legacy row and this test would not cover the cards' shape).
	var stored []byte
	if err := fx.pool.QueryRow(fx.ctx, `SELECT config FROM forms WHERE id=$1 AND account_id=$2`,
		created.ID, fx.accountID).Scan(&stored); err != nil {
		t.Fatalf("read stored config: %v", err)
	}
	var storedCfg struct {
		Blocks []struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"blocks"`
	}
	if err := json.Unmarshal(stored, &storedCfg); err != nil {
		t.Fatalf("decode stored config: %v (%s)", err, stored)
	}
	if len(storedCfg.Blocks) != 3 || storedCfg.Blocks[0].ID != "" || storedCfg.Blocks[2].ID != "" {
		t.Fatalf("stored blocks = %+v, want the option and shipping blocks to carry NO id (stored verbatim)", storedCfg.Blocks)
	}

	code, raw = tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/forms/"+created.ID+"/publish?acknowledge=1", "")
	if code != http.StatusOK {
		t.Fatalf("publish = %d, want 200 (body %s)", code, raw)
	}

	pub := &PublicHandlers{
		Forms:             dbstore.NewFormStore(fx.pool),
		Sales:             dbstore.NewSaleStore(fx.pool),
		Accounts:          dbstore.NewAccountStore(fx.pool),
		Products:          dbstore.NewProductStore(fx.pool),
		ProductVariations: dbstore.NewProductVariationStore(fx.pool),
		Workspaces:        dbstore.NewWorkspaceStore(fx.pool),
		Shipping:          shipStore,
		BaseURL:           "http://localhost:8080",
	}
	code, body := getForm(t, pub, fx.host, slug)
	if code != http.StatusOK {
		t.Fatalf("render = %d, want 200", code)
	}
	ids := pricingSelectDataIDs(body)
	if ids["opt"] != "_block_0" {
		t.Fatalf("option block rendered data-id=%q, want %q — the page keys the item by this attribute", ids["opt"], "_block_0")
	}
	if ids["ship"] != "_block_2" {
		t.Fatalf("shipping block rendered data-id=%q, want %q", ids["ship"], "_block_2")
	}
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
	rawBody, _ := json.Marshal(payload)

	rec := zoneOrderPost(t, pub, fx.host, slug, string(rawBody))
	if rec.Code != http.StatusCreated {
		t.Fatalf("order = %d, want 201 (body %s)\n"+
			"400 produk tidak sah: _block_0 is the carded defect: the page posts the block's positional key and the charge side refused it.",
			rec.Code, rec.Body.String())
	}
	var resp struct {
		RefCode string `json:"ref_code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	sale, err := pub.Sales.ByRefCode(fx.ctx, resp.RefCode)
	if err != nil {
		t.Fatalf("fetch sale: %v", err)
	}
	if sale.SubtotalSen != 1000 {
		t.Errorf("subtotal_sen = %d, want 1000 (the option block's choice price)", sale.SubtotalSen)
	}
	if sale.ShippingSen != 800 {
		t.Errorf("shipping_sen = %d, want 800 (the West Malaysia zone rate resolved for the id-less sourced block)", sale.ShippingSen)
	}
	if sale.TotalSen != 1800 {
		t.Errorf("total_sen = %d, want 1800", sale.TotalSen)
	}
	if len(sale.Items) != 1 || sale.Items[0].ProductID != ids["opt"] || sale.Items[0].PriceSen != 1000 {
		t.Errorf("line items = %+v, want the option block %q at 1000 sen only (the shipping item is the fee)", sale.Items, ids["opt"])
	}
}
