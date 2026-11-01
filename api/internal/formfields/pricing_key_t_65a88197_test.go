package formfields

// Card t_65a88197 — the ONE derivation every reader of a pricing block's key
// shares: PricingKey. The block's own `product_id` when it carries one, else
// its submit key (SubmitKeys — the block id, or the positional "_block_<i>" of
// a legacy block that carries no id).
//
// The renderer paints it as `data-id`, the page posts it as
// items[].product_id and fields{}, PricingContract validates `required` under
// it, httpapi.shippingBlocksWhere drops a server-priced block's item under it
// and httpapi.lookupProduct charges it. These arms pin the derivation itself,
// so the two server halves cannot drift from the renderer's rule without one of
// them failing here.

import "testing"

func TestPricingKey(t *testing.T) {
	cases := []struct {
		name      string
		productID string
		submitKey string
		want      string
		why       string
	}{
		{"declared product_id", "opt-key", "o1", "opt-key",
			"the builder and the renderer key a pricing block by product_id"},
		{"no product_id falls back to the submit key", "", "_block_1", "_block_1",
			"an id-less legacy block is keyed positionally by SubmitKeys"},
		{"whitespace product_id falls back too", "   ", "o1", "o1",
			"a padded-only product_id is absent, and every server half trims it"},
		{"both present: product_id wins and is trimmed", "  opt-key  ", "o1", "opt-key",
			"the page paints the trimmed value, so the charge must key the trimmed value"},
	}
	for _, c := range cases {
		if got := PricingKey(c.productID, c.submitKey); got != c.want {
			t.Errorf("%s: PricingKey(%q, %q) = %q, want %q — %s",
				c.name, c.productID, c.submitKey, got, c.want, c.why)
		}
	}
}

// TestPricingContractKeysAnIdlessPricingBlock: the required answer of a pricing
// block with no id at all is enforced under the SAME positional key the page
// posts, which is what makes lookupProduct's key (this one) chargeable at all.
func TestPricingContractKeysAnIdlessPricingBlock(t *testing.T) {
	cfg := map[string]any{"blocks": []any{
		map[string]any{
			"type": "option", "value": "Item", "required": true,
			"choices": []any{map[string]any{"label": "One", "value": "one", "price_sen": 1000}},
		},
		map[string]any{"id": "b_state", "type": "text", "value": "Negeri"},
		map[string]any{"type": "shipping", "value": "Penghantaran", "required": true,
			"source": "zones", "zone_set_id": "set-1"},
	}}
	got := PricingContract(cfg)
	if len(got) != 2 {
		t.Fatalf("PricingContract = %+v, want the option and shipping blocks", got)
	}
	if got[0].FieldID != "_block_0" {
		t.Errorf("option entry id = %q, want \"_block_0\" (the positional key the page submits)", got[0].FieldID)
	}
	if got[1].FieldID != "_block_2" {
		t.Errorf("shipping entry id = %q, want \"_block_2\" (its index in the blocks array)", got[1].FieldID)
	}

	// Enforced under that key, and only under that key: the page's own answer
	// satisfies the requirement, and the empty submission is refused naming the
	// merchant's label.
	if err := ValidateFields(got, map[string]string{"_block_0": "one", "_block_2": "zone-1"}); err != nil {
		t.Errorf("ValidateFields(page's own keys) = %v, want nil", err)
	}
	if err := ValidateFields(got, nil); err == nil {
		t.Error("ValidateFields(no answers) = nil, want a refusal for the two required id-less pricing blocks")
	}
}
