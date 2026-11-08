package formfields

// Card t_3d11f98d — the unpriced-pricing-block rule (see pricing_block.go).
//
// A unit of the shared rule, so REST, MCP and the template write paths cannot
// drift (they all call this one function).

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUnpricedPricingBlockRefusesBlocksThatCannotPriceAPick(t *testing.T) {
	cases := map[string]map[string]any{
		"option, choices with no price and no block price": {"blocks": []any{
			map[string]any{"id": "opt", "type": "option", "value": "Tambahan",
				"choices": []any{
					map[string]any{"label": "Kad", "value": "card"},
					map[string]any{"label": "Tiada", "value": "none"},
				}},
		}},
		"shipping with no choices and no price": {"blocks": []any{
			map[string]any{"id": "ship", "type": "shipping", "value": "Penghantaran"},
		}},
		"block price beside choices that carry none": {"blocks": []any{
			map[string]any{"id": "opt", "type": "option", "value": "Tambahan", "price_sen": 500,
				"choices": []any{
					map[string]any{"label": "Kad", "value": "card"},
				}},
		}},
	}
	for name, cfg := range cases {
		if msg := UnpricedPricingBlock(cfg); msg == "" {
			t.Errorf("%s: no violation reported — the block cannot price a pick", name)
		}
	}
}

// TestUnpricedPricingBlockAcceptsEveryPricedShape pins the other direction: the
// rule must not refuse a legitimate add-on, including a DELIBERATELY free one.
func TestUnpricedPricingBlockAcceptsEveryPricedShape(t *testing.T) {
	cases := map[string]map[string]any{
		// What the builder writes: per-choice prices, no block-level price.
		"builder shape": {"blocks": []any{
			map[string]any{"id": "opt", "type": "option", "product_id": "opt", "value": "Tambahan",
				"choices": []any{
					map[string]any{"label": "Kad", "value": "card", "price_sen": 500},
					map[string]any{"label": "Tiada", "value": "none", "price_sen": 0},
				}},
		}},
		// A free choice written down on purpose (the builder's default choice).
		"explicit zero only": {"blocks": []any{
			map[string]any{"id": "opt", "type": "option", "value": "Tambahan",
				"choices": []any{map[string]any{"label": "Percuma", "value": "free", "price_sen": 0}}},
		}},
		// The legacy/hand shape the issue #461 test uses: a block-level price.
		"block price, no choices": {"blocks": []any{
			map[string]any{"id": "opt-1", "type": "option", "value": "Extra sambal", "price_sen": 500},
		}},
		"block price and priced choices": {"blocks": []any{
			map[string]any{"id": "opt", "type": "option", "price_sen": 500,
				"choices": []any{map[string]any{"label": "Kad", "value": "card", "price_sen": 500}}},
		}},
		// Non-pricing blocks are not this rule's business.
		"product block without a price": {"blocks": []any{
			map[string]any{"id": "p1", "type": "product", "product_id": "prod-1", "value": "Nasi Lemak"},
		}},
		"select with unpriced choices": {"blocks": []any{
			map[string]any{"id": "s1", "type": "select", "value": "Saiz",
				"choices": []any{map[string]any{"label": "M", "value": "m"}}},
		}},
	}
	for name, cfg := range cases {
		if msg := UnpricedPricingBlock(cfg); msg != "" {
			t.Errorf("%s: refused a legitimate config: %s", name, msg)
		}
	}
}

// TestUnpricedPricingBlockIgnoresWhatItDoesNotOwn: shape problems belong to
// urlguard.CheckConfigShape, and the rule reports only what it recognises.
func TestUnpricedPricingBlockIgnoresWhatItDoesNotOwn(t *testing.T) {
	for name, cfg := range map[string]map[string]any{
		"nil config":     nil,
		"no blocks key":  {"mode": "payment"},
		"null blocks":    {"blocks": nil},
		"object blocks":  {"blocks": map[string]any{"oops": true}},
		"string blocks":  {"blocks": "oops"},
		"empty array":    {"blocks": []any{}},
		"non-object":     {"blocks": []any{"oops", 42}},
		"unknown option": {"blocks": []any{map[string]any{"id": "o1", "type": "optionx", "value": "X"}}},
	} {
		if msg := UnpricedPricingBlock(cfg); msg != "" {
			t.Errorf("%s: UnpricedPricingBlock = %q, want no violation", name, msg)
		}
	}
}

// TestUnpricedPricingBlockExemptsServerPricedShippingBlocks: a shipping block
// whose rate SOURCE is resolved by the server has no config price to check. Its
// fee is the zone set's rate (a courier block quotes at checkout and falls back
// to that same set), the renderer paints the set's zones instead of the block's
// choices, and the submit path drops the block's own line item — so the block's
// `price_sen`/`choices` are dead config, and demanding one refused every
// API/MCP author who wrote the sourced block correctly (card t_3d5b3a38).
func TestUnpricedPricingBlockExemptsServerPricedShippingBlocks(t *testing.T) {
	cases := map[string]map[string]any{
		// The card's own repro: courier source, no price, no choices.
		"courier source, no price, no choices": {"blocks": []any{
			map[string]any{"id": "ship1", "type": "shipping", "value": "Postage",
				"product_id": "ship1", "required": true,
				"source": "courier", "courier_provider": "lalamove",
				"zone_set_id": "set-1", "zone_state_field": "b_state"},
		}},
		"zones source, no price, no choices": {"blocks": []any{
			map[string]any{"id": "ship1", "type": "shipping", "value": "Postage",
				"source": "zones", "zone_set_id": "set-1", "zone_state_field": "b_state"},
		}},
		// A sourced block the builder seeded with its default priced choice:
		// the choice is dead config too, so neither arm may fire.
		"sourced block still carrying the builder's priced choice": {"blocks": []any{
			map[string]any{"id": "ship1", "type": "shipping", "value": "Postage",
				"source": "courier", "zone_set_id": "set-1",
				"choices": []any{map[string]any{"label": "Postage", "value": "1", "price_sen": 0}}},
		}},
		// A sourced block whose dead choices carry no price (the second arm's
		// shape) — still nothing to charge from the config.
		"sourced block, block price beside unpriced choices": {"blocks": []any{
			map[string]any{"id": "ship1", "type": "shipping", "value": "Postage", "price_sen": 500,
				"source": "zones", "zone_set_id": "set-1",
				"choices": []any{map[string]any{"label": "A", "value": "a"}}},
		}},
		// The submit path TRIMS `source` (httpapi.shippingBlocksWhere), so a
		// padded value is resolved from the zone set and must be exempt too.
		"padded source": {"blocks": []any{
			map[string]any{"id": "ship1", "type": "shipping", "value": "Postage",
				"source": " courier ", "zone_set_id": "set-1"},
		}},
		// An option block is never sourced: the exemption is shipping-only.
		"option beside a sourced shipping block": {"blocks": []any{
			map[string]any{"id": "ship1", "type": "shipping", "value": "Postage", "source": "zones"},
			map[string]any{"id": "opt1", "type": "option", "value": "Tambahan",
				"choices": []any{map[string]any{"label": "Kad", "value": "card", "price_sen": 500}}},
		}},
	}
	for name, cfg := range cases {
		if msg := UnpricedPricingBlock(cfg); msg != "" {
			t.Errorf("%s: refused a server-priced shipping block: %s", name, msg)
		}
	}
}

// TestUnpricedPricingBlockStillRefusesAStaticShippingBlock pins the other
// direction of the same exemption: only a source the SUBMIT path actually
// resolves may skip the rule. Everything else is charged from the config at
// submit, so an unpriced one is still the t_3d11f98d defect.
func TestUnpricedPricingBlockStillRefusesAStaticShippingBlock(t *testing.T) {
	cases := map[string]map[string]any{
		"no source at all": {"blocks": []any{
			map[string]any{"id": "ship1", "type": "shipping", "value": "Postage"},
		}},
		"source static": {"blocks": []any{
			map[string]any{"id": "ship1", "type": "shipping", "value": "Postage", "source": "static"},
		}},
		"source static with unpriced choices": {"blocks": []any{
			map[string]any{"id": "ship1", "type": "shipping", "value": "Postage", "source": "static",
				"choices": []any{map[string]any{"label": "Pos", "value": "pos"}}},
		}},
		// shippingBlocksWhere compares case-SENSITIVELY, so this block is
		// charged from its own choices at submit — it must stay inside the rule
		// or it is storable with no price and charged 0 sen.
		"wrongly-cased source": {"blocks": []any{
			map[string]any{"id": "ship1", "type": "shipping", "value": "Postage", "source": "Courier"},
		}},
		"unknown source": {"blocks": []any{
			map[string]any{"id": "ship1", "type": "shipping", "value": "Postage", "source": "zones2"},
		}},
		// The exemption is for shipping blocks only.
		"option with a source": {"blocks": []any{
			map[string]any{"id": "opt1", "type": "option", "value": "Tambahan", "source": "zones"},
		}},
	}
	for name, cfg := range cases {
		if msg := UnpricedPricingBlock(cfg); msg == "" {
			t.Errorf("%s: accepted a block the submit path prices from its own config", name)
		}
	}
}

// TestUnpricedPricingBlockReadsAStoredConfig: the same rule is asked about a
// config decoded from JSONB, so the answer must not depend on how it was built.
func TestUnpricedPricingBlockReadsAStoredConfig(t *testing.T) {
	const stored = `{"blocks":[{"id":"b-opt","type":"option","value":"Tambahan","product_id":"b-opt","choices":[{"label":"Kad","value":"card"}]}]}`
	var cfg map[string]any
	if err := json.Unmarshal([]byte(stored), &cfg); err != nil {
		t.Fatalf("decode the stored config: %v", err)
	}
	msg := UnpricedPricingBlock(cfg)
	if msg == "" {
		t.Fatal("a JSON-decoded unpriced option block was accepted")
	}
	// The message has to tell the merchant WHICH block and WHAT to do.
	for _, want := range []string{"option", "Tambahan", "price", "choice"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not contain %q", msg, want)
		}
	}
	// Surrounding whitespace in the type is not a way around the rule.
	if UnpricedPricingBlock(map[string]any{"blocks": []any{
		map[string]any{"id": "o1", "type": " option ", "value": "X"},
	}}) == "" {
		t.Error("a padded type slipped past the rule")
	}
}
