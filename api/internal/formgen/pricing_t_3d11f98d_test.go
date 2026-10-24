package formgen

// Card t_3d11f98d — generate_form must ask for (and keep) the money on an
// option/shipping block.
//
// The prompt used to request `choices` as label + value only, so an AI-made
// add-on reached the builder with no price anywhere: the buyer saw it free and
// the sale recorded 0 sen. These pin the prompt's instruction and the parse-side
// rules that keep the generated config inside what the save paths accept
// (formfields.UnpricedPricingBlock).

import (
	"strings"
	"testing"
)

func TestParseRejectsAnOptionBlockWithNoPrice(t *testing.T) {
	raw := `{"title":"T","blocks":[{"id":"opt","type":"option","value":"Tambahan",
		"choices":[{"label":"Kad","value":"card"}]}]}`
	_, err := Parse(raw)
	if err == nil {
		t.Fatal("an option block with no price anywhere was accepted — it would be charged 0 sen")
	}
	if !strings.Contains(err.Error(), "price_sen") {
		t.Errorf("error = %q, want it to name the missing field", err)
	}
}

func TestParsePushesABlockPriceOntoTheChoices(t *testing.T) {
	// The model supplied one price for the whole block. The page prints (and
	// /order charges) the CHOICE price, so the money has to move there — and
	// the block-level field is cleared so the config has one source of price.
	raw := `{"title":"T","blocks":[{"id":"ship","type":"shipping","value":"Penghantaran","price_sen":800,
		"choices":[{"label":"Standard","value":"std"},{"label":"Express","value":"exp"}]}]}`
	cfg, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	b := cfg.Blocks[0]
	if b.PriceSen != 0 {
		t.Errorf("block-level price_sen = %d, want it cleared (the choice is the one source)", b.PriceSen)
	}
	for i, c := range b.Choices {
		if c.PriceSen == nil {
			t.Fatalf("choice %d (%q) has no price after the push-down", i, c.Label)
		}
		if *c.PriceSen != 800 {
			t.Errorf("choice %d (%q) price = %d, want 800", i, c.Label, *c.PriceSen)
		}
	}
}

func TestParseKeepsAnExplicitFreeChoice(t *testing.T) {
	// An explicit 0 is a free choice written on purpose and must survive as 0
	// (not be treated as "no price").
	raw := `{"title":"T","blocks":[{"id":"opt","type":"option","value":"Tambahan",
		"choices":[{"label":"Tiada","value":"none","price_sen":0},{"label":"Kad","value":"card","price_sen":500}]}]}`
	cfg, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if p := cfg.Blocks[0].Choices[0].PriceSen; p == nil || *p != 0 {
		t.Errorf("free choice price = %v, want an explicit 0", p)
	}
	if p := cfg.Blocks[0].Choices[1].PriceSen; p == nil || *p != 500 {
		t.Errorf("priced choice price = %v, want 500", p)
	}
}

func TestParseRejectsAPricingBlockWithNoChoices(t *testing.T) {
	raw := `{"title":"T","blocks":[{"id":"opt","type":"option","value":"Tambahan"}]}`
	if _, err := Parse(raw); err == nil {
		t.Fatal("a pricing block with nothing to pick was accepted")
	}
}

// TestPromptAsksForTheChoicePrice: the prompt is the only thing that makes the
// model emit the field, so its instruction is asserted, not just the parser.
func TestPromptAsksForTheChoicePrice(t *testing.T) {
	p := Prompt("form with a RM5 extra sambal add-on and RM8 shipping")
	if !strings.Contains(p, "price_sen") {
		t.Error("prompt never mentions price_sen, so the model cannot price an add-on")
	}
	if !strings.Contains(p, "option/shipping") {
		t.Error("prompt does not say which block types carry a price")
	}
	// The product rule (server authority) must survive.
	if !strings.Contains(p, "Do NOT invent prices") {
		t.Error("prompt lost the product-price rule")
	}
}
