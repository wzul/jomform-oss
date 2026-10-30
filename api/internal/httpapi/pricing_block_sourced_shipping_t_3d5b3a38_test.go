package httpapi

// Card t_3d5b3a38 — a shipping block whose rate SOURCE is resolved by the server
// (zones / courier) must be storable with NO block-level price.
//
// THE DEFECT. formfields.UnpricedPricingBlock treated every `shipping` block as
// a priced block, so the shape an API/MCP author writes for a zone- or
// courier-sourced block — `source` + `zone_set_id` and nothing else — was
// refused with "blok shipping … tiada harga". The stored block price is DEAD
// config for a sourced block: the renderer paints the zone set's zones and the
// submit path drops the block's own line item, so the refusal demanded exactly
// the unread price the rule's own second arm calls untrustworthy. Only API/MCP
// authors ever hit it — the builder seeds its block with a priced choice, which
// satisfied the rule by accident.
//
// WHY THESE ARE REST TESTS, NOT DBSTORE FIXTURES. The shipped courier tests
// (shipping_courier_order_test.go) write the form row through
// dbstore.FormStore.Create, which is BELOW the write guard, so they never
// exercised the refusal at all. These go through POST/PATCH /v1/rest/forms —
// the route the card's repro used.
//
// Both directions are pinned: the sourced shape saves, and an unpriced STATIC
// shipping block / option block is still refused (that is the t_3d11f98d defect
// and it must not be collateral damage of this exemption).
//
// Integration tests; skipped unless JOMFORM_TEST_DATABASE_URL is set.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// seedZoneSet stores one zone set for the fixture's account through the REST
// shipping route and returns its id, so the form's block points at a REAL set —
// the block is then exactly what an API author would save, not a dangling id.
func seedZoneSet(t *testing.T, fx renameFixture) string {
	t.Helper()
	code, raw := tplReq(t, fx.h, fx.key, http.MethodPut, "/v1/rest/shipping",
		`{"sets":[{"name":"Malaysia","zones":[`+
			`{"name":"West Malaysia","states":["peninsular"],"rates":[{"price_sen":800}]},`+
			`{"name":"East Malaysia","states":["sabah","sarawak"],"rates":[{"price_sen":1500}]}]}]}`)
	if code != http.StatusOK {
		t.Fatalf("seed PUT /v1/rest/shipping = %d, want 200 (body %s)", code, raw)
	}
	var out struct {
		Sets []struct {
			ID string `json:"id"`
		} `json:"sets"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Sets) != 1 || out.Sets[0].ID == "" {
		t.Fatalf("decode seed PUT: %v (%s)", err, raw)
	}
	return out.Sets[0].ID
}

// sourcedShippingConfig is the card's repro shape with the block pointing at a
// real zone set: no `price_sen` on the block and no `choices` anywhere. The
// state select is there because the zone set resolves the buyer's address from
// it (zone_state_field), which is what makes the block renderable and chargeable
// with no config price.
func sourcedShippingConfig(source, setID string) map[string]any {
	ship := map[string]any{
		"id": "ship1", "type": "shipping", "value": "Postage",
		"product_id": "ship1", "required": true,
		"source": source, "zone_set_id": setID, "zone_state_field": "b_state",
	}
	if source == "courier" {
		ship["courier_provider"] = "lalamove"
	}
	return map[string]any{"blocks": []any{
		map[string]any{"id": "b-state", "type": "select", "value": "Negeri",
			"choices": []any{
				map[string]any{"label": "Peninsular", "value": "peninsular"},
				map[string]any{"label": "Sabah", "value": "sabah"},
			}},
		ship,
	}}
}

func shippingFormBody(t *testing.T, slug, title string, cfg map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"title": title, "slug": slug, "config": cfg})
	if err != nil {
		t.Fatalf("marshal form body: %v", err)
	}
	return string(raw)
}

// TestCreateFormAcceptsASourcedShippingBlockWithNoPrice is the card's primary
// acceptance item: POST /v1/rest/forms accepts the sourced block with no price
// and no choices, for BOTH server-resolved sources, and what is STORED really
// carries no price (the assertion is the row, not the response).
func TestCreateFormAcceptsASourcedShippingBlockWithNoPrice(t *testing.T) {
	fx := newRenameFixture(t, "it-sourced-"+randHex(4))
	setID := seedZoneSet(t, fx)

	for _, source := range []string{"courier", "zones"} {
		slug := "it-sourced-" + source + "-" + randHex(4)
		body := shippingFormBody(t, slug, "Sourced shipping "+source,
			sourcedShippingConfig(source, setID))
		code, raw := tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/forms", body)
		if code != http.StatusCreated {
			t.Fatalf("POST /v1/rest/forms with a %s-sourced shipping block and no price = %d, want 201 (body %s).\n"+
				"The block's fee comes from the zone set / courier quote, so its own price is dead config.",
				source, code, raw)
		}
		if n := liveFormCount(t, fx, slug); n != 1 {
			t.Fatalf("%d live forms on %q after an accepted create, want 1", n, slug)
		}
		block := storedShippingBlock(t, fx, slug)
		if got, _ := block["source"].(string); got != source {
			t.Errorf("stored source = %q, want %q", got, source)
		}
		if got, _ := block["zone_set_id"].(string); got != setID {
			t.Errorf("stored zone_set_id = %q, want %q", got, setID)
		}
		if v, ok := block["price_sen"]; ok {
			t.Errorf("stored block carries price_sen = %v — the point of the card is that a sourced "+
				"block needs no block price at all", v)
		}
		if v, ok := block["choices"]; ok {
			t.Errorf("stored block carries choices = %v, want none", v)
		}
	}
}

// TestUpdateFormAcceptsASourcedShippingBlockWithNoPrice is the second write
// path: a PATCH that rewrites the config of an existing form to a sourced block
// with no price must not be a way to trip the same refusal (and must actually
// write, so the guard cannot pass by dropping the change).
func TestUpdateFormAcceptsASourcedShippingBlockWithNoPrice(t *testing.T) {
	fx := newRenameFixture(t, "it-sourced-upd-"+randHex(4))
	setID := seedZoneSet(t, fx)
	slug := "it-sourced-upd-" + randHex(4)

	// Seed: the zone source.
	code, raw := tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/forms",
		shippingFormBody(t, slug, "Sourced update", sourcedShippingConfig("zones", setID)))
	if code != http.StatusCreated {
		t.Fatalf("seed create = %d, want 201 (body %s)", code, raw)
	}
	var created map[string]any
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("decode create response: %v (%s)", err, raw)
	}
	formID, _ := created["id"].(string)
	if formID == "" {
		t.Fatalf("create response carries no id: %s", raw)
	}

	// PATCH: switch the block to the courier source, still with no price.
	patch, err := json.Marshal(map[string]any{
		"config": sourcedShippingConfig("courier", setID),
	})
	if err != nil {
		t.Fatalf("marshal patch body: %v", err)
	}
	code, raw = tplReq(t, fx.h, fx.key, http.MethodPatch, "/v1/rest/forms/"+formID, string(patch))
	if code != http.StatusOK {
		t.Fatalf("PATCH to a courier-sourced block with no price = %d, want 200 (body %s)", code, raw)
	}
	block := storedShippingBlock(t, fx, slug)
	if got, _ := block["source"].(string); got != "courier" {
		t.Fatalf("stored source after the PATCH = %q, want \"courier\" — the update was refused or dropped", got)
	}
	if v, ok := block["price_sen"]; ok {
		t.Errorf("stored block carries price_sen = %v after the PATCH, want none", v)
	}
}

// TestCreateFormStillRefusesAnUnpricedStaticShippingBlockAndOption pins the
// direction the exemption must NOT touch: a block the submit path prices from
// its own config (no source / source=static), and every option block, still
// cannot be stored without a price.
func TestCreateFormStillRefusesAnUnpricedStaticShippingBlockAndOption(t *testing.T) {
	fx := newRenameFixture(t, "it-unpriced-"+randHex(4))
	setID := seedZoneSet(t, fx)

	cases := map[string]map[string]any{
		"static shipping block (no source, no price)": {"blocks": []any{
			map[string]any{"id": "ship1", "type": "shipping", "value": "Postage",
				"product_id": "ship1"},
		}},
		"source=static shipping block (no price)": {"blocks": []any{
			map[string]any{"id": "ship1", "type": "shipping", "value": "Postage",
				"product_id": "ship1", "source": "static", "zone_set_id": setID},
		}},
		"option block (unpriced choice, no block price)": {"blocks": []any{
			map[string]any{"id": "opt1", "type": "option", "value": "Tambahan",
				"product_id": "opt1",
				"choices":    []any{map[string]any{"label": "Kad", "value": "card"}}},
		}},
	}
	for name, cfg := range cases {
		slug := "it-unpriced-" + randHex(4)
		code, raw := tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/forms",
			shippingFormBody(t, slug, "Unpriced "+name, cfg))
		if code != http.StatusBadRequest {
			t.Fatalf("%s: POST = %d, want 400 (body %s).\n"+
				"The sourced-shipping exemption must not open up an unpriced static shipping block "+
				"or an option block — those are charged from their own config.", name, code, raw)
		}
		if !strings.Contains(string(raw), "harga") {
			t.Errorf("%s: 400 body = %s, want it to name the missing price", name, raw)
		}
		if n := liveFormCount(t, fx, slug); n != 0 {
			t.Errorf("%s: %d live forms on %q after a refused create, want 0", name, n, slug)
		}
	}
}

// storedShippingBlock reads the shipping block straight out of the stored
// config — a response that claimed success is not evidence that the config
// landed.
func storedShippingBlock(t *testing.T, fx renameFixture, slug string) map[string]any {
	t.Helper()
	cfg := formConfig(t, fx, slug)
	blocks, _ := cfg["blocks"].([]any)
	for _, raw := range blocks {
		b, _ := raw.(map[string]any)
		if typ, _ := b["type"].(string); typ == "shipping" {
			return b
		}
	}
	t.Fatalf("stored config for %q carries no shipping block: %v", slug, cfg["blocks"])
	return nil
}
