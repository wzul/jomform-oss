package httpapi

// Card t_addad3bd — a PAYMENT form's `blockTmpl` arms must honour the
// merchant's `required` flag, and the invariant this file guards is the one the
// defect broke: a block the page MARKS required must have a control the browser
// actually enforces.
//
// The measured defect: the `option` and `shipping` arms painted the localised
// "(required)" suffix on the label while their `<select>` carried no `required`
// attribute, so the served bytes read
//
//	<label class="lbl">LBL-option (required)</label><select class="opt" data-id="p1">
//
// and the buyer could submit with the choice box empty. The browser runs
// constraint validation BEFORE the submit event, so the missing attribute (not
// any `form.submit()` call) is the whole mechanism — measured in a real browser
// on the arm markup: without `required` the form is valid and the submit fires
// with the select empty; with it, validity.valueMissing is true, `invalid`
// fires, and no submit is issued.
//
// The assertions are per type over the palette authority (formgen.
// ValidBlockTypes), and they read the CONTROL TAG rather than the page — a bare
// strings.Contains(body, "required") stays green while a whole arm is missing
// the attribute, which is exactly how this defect survived.
//
//   - every palette type has an explicit decision here (a new type in the
//     registry with no row fails, so an unguarded arm cannot be added quietly);
//   - a type whose arm paints a control must paint it (an arm removal names
//     itself);
//   - required=true ⇒ that control carries `required` (arms that mirror the
//     flag), and a type whose control is not browser-enforced never carries the
//     "(required)" claim on its label;
//   - required=false ⇒ the flag does not leak onto the control (the
//     neutralising arm: the assertions hinge on the flag, not on the markup);
//   - the two withdrawn types the renderer still serves (coupon, file) paint no
//     control and claim nothing.

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/formgen"
)

// requiredMode is how a payment arm relates the control to the block's
// `required` flag.
const (
	// modeHonours: the control carries `required` exactly when the block does.
	modeHonours = "honours"
	// modeAlways: the control is unconditionally required, flag or no flag (the
	// email arm — a buyer address is never optional on an order).
	modeAlways = "always"
	// modeNone: the arm must never emit it.
	modeNone = "none"
)

// paymentArm is the payment renderer's per-type contract: the tag the buyer
// answers with, and what it must do with the block's `required` flag.
type paymentArm struct {
	// control is a regexp for the tag the buyer answers with. Empty for a type
	// that paints no control at all.
	control string
	mode    string
	// why records the decision for the arms that do not honour the flag.
	why string
}

// paymentArms is the whole palette (formgen.ValidBlockTypes), type by type.
// Every block id is "b1" and every product id "p1" — see paymentBlockConfig.
var paymentArms = map[string]paymentArm{
	// Standard: the buyer types/picks an answer, so the merchant's flag is
	// enforceable and every one of these already honoured it.
	"text":     {control: `<input type="text" name="b1" data-id="b1"[^>]*>`, mode: modeHonours},
	"textarea": {control: `<textarea name="notes" data-id="b1"[^>]*>`, mode: modeHonours},
	"number":   {control: `<input type="number" name="notes" data-id="b1"[^>]*>`, mode: modeHonours},
	"select":   {control: `<select name="notes" data-id="b1"[^>]*>`, mode: modeHonours},
	// Card t_5d625806: the two radio-drawing arms name their radios after the
	// BLOCK id, not a shared literal — `name` is what makes a radio button
	// group, so one shared name would merge two questions into one group.
	"radio":    {control: `<input type="radio" name="b1" data-id="b1"[^>]*>`, mode: modeHonours},
	"checkbox": {control: `<input type="checkbox" class="extra[^"]*" data-id="b1"[^>]*>`, mode: modeHonours},
	"name":     {control: `<input type="text" name="full_name" data-id="b1"[^>]*>`, mode: modeHonours},
	// The email arm emits `required` unconditionally: an order that cannot reach
	// the buyer is not an order. The flag does not turn it off.
	"email":   {control: `<input type="email" name="email" data-id="b1"[^>]*>`, mode: modeAlways},
	"phone":   {control: `<input type="tel" name="phone" data-id="b1"[^>]*>`, mode: modeHonours},
	"date":    {control: `<input type="date" name="notes" data-id="b1"[^>]*>`, mode: modeHonours},
	"time":    {control: `<input type="time" name="notes" data-id="b1"[^>]*>`, mode: modeHonours},
	"website": {control: `<input type="url" name="notes" data-id="b1"[^>]*>`, mode: modeHonours},
	// Prose. A section/html block is display copy, not a question — it paints no
	// control, so it can neither be required nor claim to be.
	"section": {mode: modeNone, why: "prose: display copy, no control to answer"},
	"html":    {mode: modeNone, why: "prose: display copy, no control to answer"},

	// Advanced.
	"address":      {control: `<textarea name="notes" data-id="b1"[^>]*>`, mode: modeHonours},
	"consent":      {control: `<input type="checkbox" class="extra[^"]*" data-id="b1"[^>]*>`, mode: modeHonours},
	"hidden":       {control: `<input type="hidden" name="notes" data-id="b1"[^>]*>`, mode: modeNone, why: "hidden inputs are barred from constraint validation and the buyer never sees it"},
	"list":         {control: `<textarea name="notes" data-id="b1"[^>]*>`, mode: modeHonours},
	"repeater":     {control: `<textarea name="notes" data-id="b1"[^>]*>`, mode: modeHonours},
	"image-choice": {control: `<input type="radio" name="b1" data-id="b1"[^>]*>`, mode: modeHonours},
	"captcha":      {control: `<input type="text" name="notes" data-id="b1"[^>]*>`, mode: modeHonours},

	// Pricing. `option` and `shipping` DO honour the flag (this card's fix):
	// they paint the "(required)" suffix, so the browser has to enforce it.
	"option":   {control: `<select class="opt[^"]*" data-id="p1"[^>]*>`, mode: modeHonours},
	"shipping": {control: `<select class="ship[^"]*" data-id="p1"[^>]*>`, mode: modeHonours},
	// The product block's checkbox is one product's buy-me switch, not a question
	// the merchant can make mandatory: the panel offers no Required toggle for
	// pricing types, and "at least one item" is already the submit JS's
	// NoProductAlert. Marking the checkbox `required` would force THAT product.
	"product": {control: `<input type="checkbox" class="p" data-id="p1"[^>]*>`, mode: modeNone, why: "pricing: a buy-me switch, and 'at least one item' is the JS NoProductAlert rule"},
	// Quantity is per-product, defaults to 1 and is never empty.
	"quantity": {control: `<input type="number" class="qty"[^>]*>`, mode: modeNone, why: "pricing: per-product, defaults to 1, never empty"},
	// The order total is computed from the other blocks.
	"total": {control: `<div class="total[^>]*>`, mode: modeNone, why: "display: the total the renderer computes, not an answer"},

	// Decorative.
	"image":   {mode: modeNone, why: "decorative: no control"},
	"divider": {mode: modeNone, why: "decorative: no control"},
}

// paymentBlockConfig is a payment-mode config carrying exactly one block of
// `typ`, labelled distinctly and — for the pricing arms — keyed to product "p1"
// with one priced choice.
func paymentBlockConfig(typ string, required bool) map[string]any {
	return map[string]any{
		"mode":     "payment",
		"products": []any{},
		"blocks": []any{map[string]any{
			"id": "b1", "type": typ, "value": "LBL-" + typ, "required": required,
			"product_id": "p1", "css_class": "jf-wide",
			"choices": []any{
				map[string]any{"label": "Choice A", "value": "a", "price_sen": 100},
			},
		}},
	}
}

// controlTags returns every tag in body matching re.
func controlTags(body, re string) []string {
	if re == "" {
		return nil
	}
	return regexp.MustCompile(re).FindAllString(body, -1)
}

// tagHasRequired reports whether the tag carries `required` as its own
// attribute. Fields() is used so a value that merely contains the word (a
// placeholder, a class, the oninvalid hook) cannot satisfy it.
func tagHasRequired(tag string) bool {
	for _, f := range strings.Fields(strings.TrimSuffix(tag, ">")) {
		if f == "required" {
			return true
		}
	}
	return false
}

// TestPaymentFormRequiredReachesTheControl is the card's claim, per palette
// type: a block the merchant marked required is enforced by the browser, and no
// block is labelled "(required)" without it. Reverting the two pricing arms (or
// deleting any other arm) reds the named type.
func TestPaymentFormRequiredReachesTheControl(t *testing.T) {
	for typ := range formgen.ValidBlockTypes {
		arm, ok := paymentArms[string(typ)]
		if !ok {
			t.Errorf("palette type %q has no decision in paymentArms — the payment renderer may paint no control for it at all", typ)
			continue
		}
		for _, required := range []bool{true, false} {
			body := renderPaymentPage(t, fmt.Sprintf("pf-%s-%v", typ, required), paymentBlockConfig(string(typ), required))

			// The arm must paint the control it is supposed to paint: a dropped
			// arm fails here by name instead of passing on an empty page.
			var tag string
			if arm.control != "" {
				tags := controlTags(body, arm.control)
				if len(tags) == 0 {
					t.Errorf("%s (required=%v): no control matching %s on the page\npage: %s", typ, required, arm.control, body)
					continue
				}
				tag = tags[0]
			}
			enforced := tag != "" && tagHasRequired(tag)

			// The fixture carries exactly ONE block, so a " (required)" mark
			// anywhere on the page belongs to it — that is the claim the buyer
			// reads, and it must match what the browser enforces.
			claims := strings.Contains(body, " (required)")

			switch arm.mode {
			case modeHonours:
				if required && !enforced {
					t.Errorf("block type %q is marked required on the page but its control carries no `required`, so the browser lets the empty answer through:\n  control: %s", typ, tag)
				}
				if !required && enforced {
					t.Errorf("block type %q is NOT required but its control carries `required`:\n  control: %s", typ, tag)
				}
				// The invariant the defect broke, in the direction that loses a
				// merchant's mark: the page must never CLAIM required for a block
				// the browser will not enforce. (An arm that enforces without
				// painting the suffix — checkbox, consent — is the other
				// direction and is not this card's class.)
				if !required && claims {
					t.Errorf("block type %q is NOT required but the page marks it (required):\n  control: %s", typ, tag)
				}
			case modeAlways:
				if !enforced {
					t.Errorf("block type %q must always be enforced, but its control carries no `required`:\n  control: %s", typ, tag)
				}
			case modeNone:
				if enforced {
					t.Errorf("block type %q must never carry `required` (%s) but its control does:\n  control: %s", typ, arm.why, tag)
				}
				// The invariant the defect broke: a page that marks a block
				// required must have a control the browser enforces. An arm with
				// nothing to answer (or one that deliberately ignores the flag)
				// may not claim it.
				if claims {
					t.Errorf("block type %q is marked (required) on the page but paints no enforceable control (%s):\n  control: %s", typ, arm.why, tag)
				}
			}
		}
	}
}

// TestPaymentFormRequiredMessageReachesThePricingControls keeps the two fixed
// arms in step with every sibling arm: the merchant's per-field message must
// ride on the control as the browser's own validation text, not be dropped.
func TestPaymentFormRequiredMessageReachesThePricingControls(t *testing.T) {
	for _, typ := range []string{"option", "shipping"} {
		body := renderPaymentPage(t, "pf-msg-"+typ, map[string]any{
			"mode":     "payment",
			"products": []any{},
			"blocks": []any{map[string]any{
				"id": "b1", "type": typ, "value": "LBL-" + typ, "required": true,
				"product_id": "p1", "required_message": "Pilih dahulu",
				"choices": []any{map[string]any{"label": "Choice A", "value": "a"}},
			}},
		})
		tags := controlTags(body, paymentArms[typ].control)
		if len(tags) == 0 {
			t.Fatalf("%s: no control on the page: %s", typ, body)
		}
		tag := tags[0]
		if !tagHasRequired(tag) {
			t.Errorf("%s: the control is not required, so the message can never show:\n  control: %s", typ, tag)
		}
		for _, want := range []string{`oninvalid="this.setCustomValidity('Pilih dahulu')"`, `oninput="this.setCustomValidity('')"`} {
			if !strings.Contains(tag, want) {
				t.Errorf("%s: the control is missing %s\n  control: %s", typ, want, tag)
			}
		}
	}
}

// TestPaymentFormWithdrawnBlocksClaimNoRequired pins item 2 of the card rather
// than leaving it implicit: `coupon` and `file` are no longer offered by the
// builder palette or the generator (formgen/coupon_withdrawn_test.go,
// formgen/file_block_withdrawn_test.go), but a form stored before the
// withdrawal still renders. On the payment surface each paints the merchant's
// label plus a note and NO control, so neither may claim to be required — there
// is nothing for the buyer to fill in.
func TestPaymentFormWithdrawnBlocksClaimNoRequired(t *testing.T) {
	for _, typ := range []string{"coupon", "file"} {
		body := renderPaymentPage(t, "pf-legacy-"+typ, map[string]any{
			"mode":     "payment",
			"products": []any{},
			"blocks": []any{map[string]any{
				"id": "b1", "type": typ, "value": "LBL-" + typ, "required": true,
			}},
		})
		if !strings.Contains(body, "LBL-"+typ) {
			t.Errorf("%s: a stored block's own label is missing from the page — it was dropped silently", typ)
		}
		if strings.Contains(body, "LBL-"+typ+" (required)") {
			t.Errorf("%s: the page claims the block is required, but it paints no control", typ)
		}
		if strings.Contains(body, `data-id="b1"`) {
			t.Errorf("%s: the page painted a control for a block whose answer goes nowhere", typ)
		}
	}
}
