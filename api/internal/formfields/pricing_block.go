package formfields

// The UNPRICED-PRICING-BLOCK rule — the write-side half of "an add-on or
// shipping block that cannot charge anything must not be storable" (card
// t_3d11f98d).
//
// THE DEFECT. An option/shipping block's price is only ever read from the
// config, and the builder writes it on the CHOICE (`choices[].price_sen`) —
// never on the block. So a block that carries no price anywhere is served to
// the buyer as a free add-on and charged 0 sen, which is what
// `generate_form` (formgen) used to produce: its prompt asked for `choices`
// with a label and a value and never mentioned money. The merchant asked their
// agent for "extra sambal, RM5", the page showed no price, and the sale
// recorded 0.
//
// TWO SHAPES ARE REFUSED, both of them ambiguous in the same way — the config
// cannot say what a pick costs:
//
//  1. no price at all (no block-level `price_sen` and not one priced choice);
//  2. a block-level `price_sen` beside choices that carry none. The renderer
//     prints a per-choice price and the submit path charges the selected
//     CHOICE (httpapi/lookupProduct), so that block price is dead config: the
//     merchant believes they set a fee and the buyer is charged nothing.
//
// A choice with an EXPLICIT 0 is fine — that is a free choice written down on
// purpose (the builder's own default choice is `price_sen: 0`), and the page
// shows it without a "+RM…" exactly as it will be charged.
//
// WHERE THE RULE IS APPLIED. Beside `UnsupportedBlock` at every write path that
// stores a caller-supplied form/template config — REST forms create/update,
// REST own-template create/update, MCP upsert_form create/update, MCP
// create_template/update_template — so the builder, the REST API, MCP and the
// AI generator cannot disagree about what a storable pricing block is.

import (
	"encoding/json"
	"fmt"
	"strings"
)

// pricingBlockTypes are the block types whose value is money. Both are priced
// from the config at submit time (httpapi/lookupProduct) and neither is a
// product reference, so the products table never supplies their price.
var pricingBlockTypes = map[string]bool{"option": true, "shipping": true}

// PricingContract derives the ORDER route's pricing-answer rule: one Field per
// option/shipping block, carrying the block's `required` flag and the key its
// <select> submits under.
//
// WHY THIS IS NOT PART OF Contract. Contract is shared by three surfaces and
// only one of them paints a pricing control:
//
//   - the form's own order route (httpapi/order.go) — the payment renderer
//     emits the option/shipping <select>, so the merchant's `required` there is
//     a real requirement the server must also enforce (card t_f9e2f855: the
//     page refused the empty choice and a direct POST still created the sale);
//   - the menu checkout (httpapi/menu.go menuExtraContract) — renders the
//     attached order form's INPUT fields, never a pricing block, so enforcing
//     one there would refuse an order for a control the checkout never paints;
//   - the response surface (httpapi/public.go renderResponseForm) — drops
//     `product`, `quantity`, `option`, `shipping` and `total` behind an
//     explicit case for the same reason.
//
// So the pricing blocks stay out of collectableTypes and the callers that
// render them opt in. That keeps the renderer/contract parity rule true
// (httpapi/contract_renderer_parity_test.go asserts pricing is NOT in the
// derived field contract) and keeps the two surfaces that never show a choice
// from growing a requirement nobody can answer.
//
// THE KEY. Both renderers tag the select with data-id="{{.ProductID}}" and the
// page submits {ProductID: answer}, so that is the key validated here. The
// builder writes product_id = the block's own id; a config that never set it
// falls back to the block's own submit key (SubmitKeys — the block id, or the
// positional "_block_<i>" of a legacy id-less block), which is the same value
// the renderer's block wrapper carries.
//
// The entries are only as strong as the flag: ValidateFields enforces nothing
// on a non-required entry whose answer is empty, which is exactly the
// neutralising case — the same form with `required` flipped false accepts the
// payload unchanged.
//
// THE RULE. The entry carries the block's conditional-logic rule as well. The
// payment renderer wraps every block in it, pricing blocks included, and disables
// a hidden block's controls, so a buyer never sees — and never posts — a choice
// whose rule is false. ValidateFields applies the same rule to a contract field,
// so a hidden pricing block is not required and a visible one still is. Card
// t_d20bad94: without this the route refused an order for a choice the page had
// hidden, the same failure t_9e875bca fixed for contract fields.
func PricingContract(config map[string]any) []Field {
	bs := blocksOf(config)
	if len(bs) == 0 {
		return nil
	}
	rawIDs := make([]string, len(bs))
	for i := range bs {
		rawIDs[i] = bs[i].ID
	}
	keys := SubmitKeys(rawIDs)
	var out []Field
	seen := map[string]bool{}
	for i, b := range bs {
		if !pricingBlockTypes[b.Type] {
			continue
		}
		id := strings.TrimSpace(b.ProductID)
		if id == "" {
			id = keys[i]
		}
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		label := strings.TrimSpace(b.Value)
		if label == "" {
			label = id
		}
		out = append(out, Field{
			FieldID:  id,
			Type:     b.Type,
			Label:    label,
			Required: b.Required,
			// The block's own conditional rule travels with the entry. The
			// payment renderer wraps EVERY block in it — pricing blocks
			// included, `data-condition` (render_blocks.go) — and its script
			// hides AND disables the controls of a block whose rule is false,
			// so a hidden choice is never posted and its `required` never runs.
			// ValidateFields already skips a field whose rule is false (the
			// path a contract field takes), so carrying it here is the whole
			// fix: a hidden pricing block is not required, a visible one still
			// is. Without it the route demanded an answer to a control the
			// buyer was never shown (card t_d20bad94).
			Condition: b.Condition,
		})
	}
	return out
}

// UnpricedPricingBlock returns the merchant-facing rejection for the first
// option/shipping block whose price the config cannot resolve, or "" when every
// pricing block is priced. A config with no blocks key, a non-array blocks (the
// config-shape rule's refusal) or a non-block entry yields "" — this rule
// reports only what it recognises.
func UnpricedPricingBlock(config map[string]any) string {
	if config == nil {
		return ""
	}
	raw, ok := config["blocks"]
	if !ok || raw == nil {
		return ""
	}
	body, err := json.Marshal(raw)
	if err != nil {
		return ""
	}
	var blocks []struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Value    string `json:"value"`
		PriceSen *int64 `json:"price_sen"`
		Choices  []struct {
			Label    string `json:"label"`
			Value    string `json:"value"`
			PriceSen *int64 `json:"price_sen"`
		} `json:"choices"`
	}
	if json.Unmarshal(body, &blocks) != nil {
		return ""
	}
	for _, b := range blocks {
		typ := strings.TrimSpace(b.Type)
		if !pricingBlockTypes[typ] {
			continue
		}
		priced := false
		for _, c := range b.Choices {
			if c.PriceSen != nil {
				priced = true
				break
			}
		}
		label := strings.TrimSpace(b.Value)
		if label == "" {
			label = strings.TrimSpace(b.ID)
		}
		switch {
		case !priced && b.PriceSen == nil:
			return fmt.Sprintf("blok %s %q tiada harga: tetapkan harga pada setiap pilihan (0 untuk percuma) sebelum menyimpan", typ, label)
		case !priced && len(b.Choices) > 0:
			return fmt.Sprintf("blok %s %q menyimpan harga blok yang tidak digunakan kerana pilihannya tiada harga: letak harga pada setiap pilihan (0 untuk percuma) sebelum menyimpan", typ, label)
		}
	}
	return ""
}
