package mcpsrv

// Card t_3d5b3a38 — the MCP half: `upsert_form` stores a form config verbatim,
// so an agent authoring a zone- or courier-sourced shipping block hit the same
// refusal REST did ("blok shipping … tiada harga") unless it also carried a
// block price the renderer and the submit path never read.
//
// The rule lives in formfields.UnpricedPricingBlock and is now exempt for a
// shipping block whose `source` the server resolves. This test pins the TOOL's
// behaviour, because a correct rule that a call site forgets to reach protects
// nothing — both halves of upsert_form (create and update) are exercised.
//
// Assertions read the DATABASE, not the tool's reply: the defect class is a
// write that is refused/claimed without the config landing.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL); skipped
// when unset.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// sourcedShippingCfgMCP is the shape an agent writes for a sourced block: the
// source and the zone set it points at, and NO `price_sen` and NO `choices`.
func sourcedShippingCfgMCP(source string) map[string]any {
	ship := map[string]any{
		"id": "ship1", "type": "shipping", "value": "Postage",
		"product_id": "ship1", "required": true,
		"source": source, "zone_set_id": "set-1", "zone_state_field": "b_state",
	}
	if source == "courier" {
		ship["courier_provider"] = "lalamove"
	}
	return map[string]any{"blocks": []any{
		map[string]any{"id": "b-state", "type": "select", "value": "Negeri",
			"choices": []any{map[string]any{"label": "Peninsular", "value": "peninsular"}}},
		ship,
	}}
}

// storedShippingSourceMCP reads the shipping block's source out of the stored
// config, or "" when there is no such block.
func storedShippingSourceMCP(t *testing.T, st issue501MCPState, formID string) string {
	t.Helper()
	var raw []byte
	if err := st.pool.QueryRow(context.Background(),
		`SELECT config FROM forms WHERE id=$1 AND account_id=$2`, formID, st.accountID).Scan(&raw); err != nil {
		t.Fatalf("read stored config for %s: %v", formID, err)
	}
	var cfg struct {
		Blocks []struct {
			Type     string `json:"type"`
			Source   string `json:"source"`
			PriceSen *int64 `json:"price_sen"`
			Choices  []any  `json:"choices"`
		} `json:"blocks"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decode stored config: %v (%s)", err, raw)
	}
	for _, b := range cfg.Blocks {
		if b.Type != "shipping" {
			continue
		}
		if b.PriceSen != nil {
			t.Errorf("stored sourced shipping block carries price_sen = %d — a sourced block needs no block price", *b.PriceSen)
		}
		if len(b.Choices) != 0 {
			t.Errorf("stored sourced shipping block carries %d choices, want none", len(b.Choices))
		}
		return b.Source
	}
	return ""
}

// TestMCPUpsertFormAcceptsASourcedShippingBlockWithNoPrice: both halves of the
// tool accept the sourced block with no price.
func TestMCPUpsertFormAcceptsASourcedShippingBlockWithNoPrice(t *testing.T) {
	st := issue501MCPFixture(t)

	for _, source := range []string{"courier", "zones"} {
		_, out, err := st.srv.upsertForm(st.ctx, nil, upsertFormIn{
			Title:  "Sourced shipping " + source,
			Config: sourcedShippingCfgMCP(source),
		})
		if err != nil {
			t.Fatalf("upsert_form create with a %s-sourced shipping block and no price: %v.\n"+
				"The block's fee comes from the zone set / courier quote, so its own price is dead config.", source, err)
		}
		formID := out["form_id"]
		if formID == "" {
			t.Fatalf("upsert_form create returned no form_id: %v", out)
		}
		if got := storedShippingSourceMCP(t, st, formID); got != source {
			t.Fatalf("stored shipping source after create = %q, want %q", got, source)
		}
	}

	// The update arm: a form that exists must be able to take the sourced block.
	formID := st.seedForm(t, "i-src-ship-"+randHex(4))
	if _, _, err := st.srv.upsertForm(st.ctx, nil, upsertFormIn{
		FormID: formID, Title: "Sourced shipping update",
		Config: sourcedShippingCfgMCP("courier"),
	}); err != nil {
		t.Fatalf("upsert_form update with a courier-sourced shipping block and no price: %v", err)
	}
	if got := storedShippingSourceMCP(t, st, formID); got != "courier" {
		t.Fatalf("stored shipping source after update = %q, want \"courier\"", got)
	}
}

// TestMCPUpsertFormStillRefusesUnpricedStaticAndOptionBlocks pins that the
// exemption did not open the t_3d11f98d hole on the agent's write path: a block
// the submit path prices from its own config is still refused when unpriced.
func TestMCPUpsertFormStillRefusesUnpricedStaticAndOptionBlocks(t *testing.T) {
	st := issue501MCPFixture(t)

	cases := map[string]map[string]any{
		"static shipping block, no price": {"blocks": []any{
			map[string]any{"id": "ship1", "type": "shipping", "value": "Postage", "product_id": "ship1"},
		}},
		"option block, unpriced choice": {"blocks": []any{
			map[string]any{"id": "opt1", "type": "option", "value": "Tambahan", "product_id": "opt1",
				"choices": []any{map[string]any{"label": "Kad", "value": "card"}}},
		}},
	}
	for name, cfg := range cases {
		_, out, err := st.srv.upsertForm(st.ctx, nil, upsertFormIn{Title: "Unpriced " + name, Config: cfg})
		if err == nil {
			t.Fatalf("%s: upsert_form ACCEPTED an unpriced block the submit path charges from its own config (%v)", name, out)
		}
		if !strings.Contains(err.Error(), "price") {
			t.Errorf("%s: refusal does not name the missing price: %v", name, err)
		}
	}
}
