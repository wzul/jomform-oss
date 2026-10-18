package httpapi

// Card t_4a466bc5 — `formfields.collectableTypes` is the allowlist behind
// `formfields.Contract`, which is what the menu checkout renders and validates
// an attached form with (`menuExtraContract` → `menuExtraFields` /
// `formfields.ValidateFields`, issue #493). Its doc comment used to claim it
// "mirrors the renderer's input set" — a claim nothing checked. It drifted:
// `text` was a real `<input type="text">` on both served pages while being
// absent from the allowlist, so a merchant's placed text block never reached
// the checkout and nothing enforced it (fixed in the allowlist by #1287, card
// t_7fb80479 — that fix is the instance; this file is the class).
//
// These tests are the machine check the comment now points at: they drive the
// REAL renderers (blockTmpl for a payment form, responseTmpl for a response
// form) and the REAL menu checkout template with one block of every palette
// type, and assert that "this block paints an answer control" and "this block
// is in the derived contract" are the same statement for every type. A future
// renderer arm added without the allowlist (or an allowlist entry whose arm is
// removed) now fails the build instead of shipping a field the merchant cannot
// see.
//
// Why not derive the allowlist from the renderers at runtime: the "renderer
// input set" only exists as Go template source — there is no machine-readable
// declaration to read, so a production-time derivation would mean parsing
// template text on a payment path. A checked hand-maintained list keeps one
// readable declaration and one failure mode: this test.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/formfields"
	"github.com/wzul/jomform/api/internal/formgen"
)

// parityPricingTypes are the palette blocks that paint controls but are NOT
// answers: their controls are keyed by the PRODUCT id and priced through
// payments.SumItems, so they are deliberately outside the field contract (see
// collectableTypes' comment). They are excluded from the response-fixture only
// because a response form serves no pricing blocks.
var parityPricingTypes = map[string]bool{
	"product": true, "quantity": true, "option": true, "shipping": true, "total": true,
}

var parityControlTags = regexp.MustCompile(`<(?:input|select|textarea)\b[^>]*>`)

// controlKeyedBy returns the first control tag on the page whose name or
// data-id is the block id — i.e. the control the page would submit an answer
// under. "" means the block paints no answer control at all.
//
// Keying on the BLOCK id is what separates an answer from a pricing control: a
// product/option/shipping control carries the product id instead.
func controlKeyedBy(body, id string) string {
	keyed := regexp.MustCompile(`(?:name|data-id)="` + regexp.QuoteMeta(id) + `"`)
	for _, tag := range parityControlTags.FindAllString(body, -1) {
		if keyed.MatchString(tag) {
			return tag
		}
	}
	return ""
}

// parityBlocks builds one block of EVERY type formgen.ValidBlockTypes offers —
// the palette, not a hand-picked list, so a type added there is swept
// automatically — and returns the config plus type → block id.
func parityBlocks(t *testing.T) (map[string]any, map[string]string) {
	t.Helper()
	types := make([]string, 0, len(formgen.ValidBlockTypes))
	for typ := range formgen.ValidBlockTypes {
		types = append(types, string(typ))
	}
	sort.Strings(types)

	blocks := make([]any, 0, len(types))
	idOf := make(map[string]string, len(types))
	for i, typ := range types {
		id := fmt.Sprintf("b%02d", i)
		idOf[typ] = id
		b := map[string]any{"id": id, "type": typ, "value": "Label " + typ}
		switch typ {
		case "select", "radio", "checkbox", "image-choice", "option", "shipping":
			b["choices"] = []any{map[string]any{"label": "A", "value": "a"}}
		case "image":
			b["src"] = "https://cdn.example/p.png"
		}
		blocks = append(blocks, b)
	}
	return map[string]any{"mode": "payment", "blocks": blocks, "products": []any{}}, idOf
}

// responseParityConfig is the same fixture with the pricing blocks removed: a
// response form serves no products, and the contract must agree with BOTH
// renderers, whichever mode the form is in.
func responseParityConfig(cfg map[string]any) map[string]any {
	blocks := []any{}
	for _, raw := range cfg["blocks"].([]any) {
		b := raw.(map[string]any)
		if parityPricingTypes[b["type"].(string)] {
			continue
		}
		blocks = append(blocks, b)
	}
	return map[string]any{"mode": "response", "blocks": blocks, "products": []any{}}
}

// TestDerivedContractMirrorsTheRendererInputSet is the guard this card exists
// for: for every palette type, "the served pages paint an answer control keyed
// by the block id" and "formfields.Contract lists the block" must be the same
// answer — on the payment renderer AND on the response renderer.
func TestDerivedContractMirrorsTheRendererInputSet(t *testing.T) {
	cfg, idOf := parityBlocks(t)
	payment := renderPaymentPage(t, "parity-payment", cfg)
	response := renderResponsePage(t, "parity-response", responseParityConfig(cfg))

	inContract := map[string]bool{}
	for _, f := range formfields.Contract(cfg) {
		inContract[f.Type] = true
	}

	types := make([]string, 0, len(idOf))
	for typ := range idOf {
		types = append(types, typ)
	}
	sort.Strings(types)

	for _, typ := range types {
		id := idOf[typ]
		payTag := controlKeyedBy(payment, id)
		respTag := controlKeyedBy(response, id)
		painted := payTag != "" && respTag != ""

		if (payTag != "") != (respTag != "") {
			t.Errorf("%q is an input on one renderer and not the other: payment=%q response=%q — "+
				"the two public pages must not disagree about the same block", typ, payTag, respTag)
		}
		if painted != inContract[typ] {
			t.Errorf("%q: the served pages paint its answer control=%v while formfields.Contract lists it=%v "+
				"(payment=%q). A block that collects a value but is not in the contract is a field the menu "+
				"checkout never renders and never validates; a contract entry with no control asks the buyer "+
				"for something they cannot see.", typ, painted, inContract[typ], payTag)
		}
	}

	// The fixture is worth nothing if it silently lost the types this card is
	// about: pin the two that drifted (t_7fb80479: `text`) and the pricing
	// exception, so a shrinking palette is visible too.
	for _, typ := range []string{"text", "name", "address"} {
		if !inContract[typ] {
			t.Fatalf("fixture defect: %q is not in the derived contract at all", typ)
		}
	}
	for typ := range parityPricingTypes {
		if inContract[typ] {
			t.Errorf("pricing block %q is in the field contract — pricing is charged through the product "+
				"blocks, never validated as an answer", typ)
		}
	}
}

// TestMenuCheckoutRendersEveryDerivedContractField is the menu end of the same
// statement: every field the contract carries must reach the checkout page as a
// control the checkout script submits under that field id. This is the join that
// was missing — the contract test and the checkout-template test each existed,
// but nothing drove an attached form's CONFIG through both.
func TestMenuCheckoutRendersEveryDerivedContractField(t *testing.T) {
	cfg, _ := parityBlocks(t)
	contract := menuExtraContract(cfg)
	if len(contract) == 0 {
		t.Fatalf("the attached form produced no checkout contract: %+v", cfg["blocks"])
	}

	d := menu492Fixture(t)
	d.ExtraFields = menuExtraFields(contract, formLangs["en"])
	body := renderMenu492(t, d)

	for _, f := range contract {
		if controlKeyedBy(body, f.FieldID) == "" {
			t.Errorf("field %q (%s) is in the checkout contract but the checkout renders no control for it — "+
				"the buyer is never asked, and ValidateFields still enforces it", f.FieldID, f.Type)
		}
		// A `hidden` block is deliberately painted as an input with no visible
		// label — its value is set by the page, not typed by the buyer.
		if f.Type != "hidden" && !strings.Contains(body, f.Label) {
			t.Errorf("field %q is rendered without the merchant's label %q", f.FieldID, f.Label)
		}
	}
	// name/email are always collected as the buyer fields — a form block for
	// them must not add a second, unrendered required field.
	for _, f := range contract {
		if f.Type == "name" || f.Type == "email" {
			t.Errorf("menuExtraContract kept a %q block (%s): the checkout collects those as full_name/email", f.Type, f.FieldID)
		}
	}
}

// TestMenuCheckoutEnforcesARequiredTextBlock is the card's measured case, end
// to end: a form carrying a required `text` block, attached to a menu, must both
// show the field on the checkout and refuse an order that leaves it blank. This
// is what the missing allowlist entry took away.
func TestMenuCheckoutEnforcesARequiredTextBlock(t *testing.T) {
	cfg := map[string]any{"mode": "response", "blocks": []any{
		map[string]any{"id": "tx1", "type": "text", "value": "Nama syarikat", "required": true},
		map[string]any{"id": "nm1", "type": "name", "value": "Nama penuh", "required": true},
	}}
	contract := menuExtraContract(cfg)
	if len(contract) != 1 || contract[0].FieldID != "tx1" || !contract[0].Required {
		t.Fatalf("menuExtraContract = %+v, want just the required text block tx1", contract)
	}

	d := menu492Fixture(t)
	d.ExtraFields = menuExtraFields(contract, formLangs["en"])
	body := renderMenu492(t, d)
	tag := controlKeyedBy(body, "tx1")
	if tag == "" {
		t.Fatalf("the checkout renders no control for the required text block")
	}
	if !strings.Contains(tag, `required`) {
		t.Errorf("the checkout's control for the required text block is not marked required: %s", tag)
	}
	if !strings.Contains(body, "Nama syarikat") {
		t.Errorf("the checkout does not carry the merchant's label for the text block")
	}

	// Blank answer → refused, naming the merchant's label (the direct-POST path:
	// the client-side attribute is bypassable).
	if err := formfields.ValidateFields(contract, map[string]string{"full_name": "Ali"}); err == nil ||
		!strings.Contains(err.Error(), "Nama syarikat") {
		t.Errorf("a blank required text answer = %v, want a refusal naming the merchant's label", err)
	}
	if err := formfields.ValidateFields(contract, map[string]string{"full_name": "Ali", "tx1": "JomForm Sdn Bhd"}); err != nil {
		t.Errorf("a filled required text answer was refused: %v", err)
	}
}
