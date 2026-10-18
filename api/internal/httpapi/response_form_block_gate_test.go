package httpapi

// Card t_ec5fdf4b — the response page loses content silently. Two guards.
//
// (1) THE BLOCK GATE. renderResponseForm's switch decides what the buyer's page
// carries, and a type missing from it is dropped BEFORE the template ever sees
// the block: the merchant placed it, the page says nothing, and nothing errors
// — the shape measured for `text` (card t_99c3a08b) and for the static group
// and `file` (card t_4086b8f0). The table in
// TestResponseFormBlockGateCoversTheWholePalette is driven by
// formgen.ValidBlockTypes, the API's own record of the palette, and is checked
// in BOTH directions: a NEW palette type reds a row NAMING ITSELF until its
// treatment is decided, and a treatment whose type has left the palette reds
// too. Block types WITHDRAWN from the palette (`file`, `coupon`) are therefore
// in neither direction — a form stored before a withdrawal is pinned by that
// withdrawal's own renderer test (response_form_palette_blocks_test.go for the
// stored `file` block, coupon_block_withdrawn_test.go for the stored `coupon`
// block), and neither may be painted as an input on any renderer.
//
// (2) THE REQUIRED ATTRIBUTE. Every control the page emits for a block the
// merchant marked required must carry the browser's `required`. Constraint
// validation runs BEFORE the submit event, so `required` on the control is the
// only thing standing between an empty answer and a stored submission on this
// page. The `textarea` arm was the one arm that emitted maxlength but never
// `required` while every sibling arm did; measured in a real browser against
// the served page, an empty field's checkValidity() was true, no `invalid`
// event fired, and the submit handler POSTed {fields:{ta1:""}} — the required
// field was submitted empty. With `required` on the arm: checkValidity() false,
// `invalid` fires, no POST. (The card named `form.submit()` as the mechanism;
// no such call exists anywhere in api/ — measured with `git log -S` over every
// revision — so the mechanism was the missing attribute and the effect is the
// one the card describes.)

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/formgen"
)

// paletteTypes is formgen.ValidBlockTypes — the API's own record of the builder
// palette — in a stable order.
func paletteTypes() []string {
	out := make([]string, 0, len(formgen.ValidBlockTypes))
	for t := range formgen.ValidBlockTypes {
		out = append(out, string(t))
	}
	sort.Strings(out)
	return out
}

// paletteTypeConfig is a response-mode config with ONE block per palette type,
// each id and value derived from its type so a row can name itself.
func paletteTypeConfig(required bool) map[string]any {
	blocks := make([]any, 0, len(formgen.ValidBlockTypes))
	for _, t := range paletteTypes() {
		blocks = append(blocks, map[string]any{
			"id": "bk-" + t, "type": t, "value": "V-" + t, "required": required,
			"src":        "https://cdn.example/" + t + ".png",
			"product_id": "pr1", "price_sen": 500, "quantity": true,
			"choices": []any{map[string]any{"label": "C1", "value": "c1"}},
		})
	}
	return map[string]any{"mode": "response", "blocks": blocks, "products": []any{}}
}

// controlNamed returns the opening tag of the FIRST input/select/textarea whose
// name is id, or "" when the page carries no control by that name.
func controlNamed(body, id string) string {
	re := regexp.MustCompile(`(?s)<(?:input|select|textarea)[^>]*\sname="` + regexp.QuoteMeta(id) + `"[^>]*>`)
	return re.FindString(body)
}

// TestResponseFormBlockGateCoversTheWholePalette drives the REAL call site
// (renderResponseForm) with one block of every palette type and pins, per type,
// what the served page must do with it. The table must cover the palette
// exactly: a NEW palette type reds this test until its treatment is decided, and
// a type whose arm is deleted reds its own row.
func TestResponseFormBlockGateCoversTheWholePalette(t *testing.T) {
	body := renderResponsePage(t, "gate", paletteTypeConfig(false))

	// treatment is how the response renderer must handle a placed block.
	const (
		asInput   = "input"        // a control named by the block id, collected on submit
		asStatic  = "static"       // painted content, never collected
		asPricing = "payment-only" // deliberately not painted on a response page
	)
	table := map[string]string{
		// Inputs the responder fills in.
		"text": asInput, "name": asInput, "email": asInput, "phone": asInput,
		"textarea": asInput, "number": asInput, "select": asInput, "radio": asInput,
		"checkbox": asInput, "date": asInput, "time": asInput, "website": asInput,
		"address": asInput, "consent": asInput, "hidden": asInput, "list": asInput,
		"repeater": asInput, "image-choice": asInput, "captcha": asInput,
		// Static content, painted as the payment renderer paints it.
		"section": asStatic, "html": asStatic, "image": asStatic, "divider": asStatic,
		// Pricing: meaningless without the payment step.
		"product": asPricing, "quantity": asPricing, "option": asPricing,
		"shipping": asPricing, "total": asPricing,
	}

	// Both directions: no type without a decision, no decision without a type.
	for _, ty := range paletteTypes() {
		if _, ok := table[ty]; !ok {
			t.Errorf("palette type %q has no declared treatment in the response block gate — decide it (serve it, or drop it on purpose and say so)", ty)
		}
	}
	for ty := range table {
		if !formgen.ValidBlockTypes[formgen.BlockType(ty)] {
			t.Errorf("the table names %q, which is not a palette type", ty)
		}
	}

	for _, ty := range paletteTypes() {
		id := "bk-" + ty
		in := controlNamed(body, id)
		switch table[ty] {
		case asInput:
			if in == "" {
				t.Errorf("%s block: no control named %q on the served response page — the merchant placed it and the buyer cannot answer it", ty, id)
				continue
			}
			if ty == "hidden" {
				continue // a hidden input carries no label (its arm has none)
			}
			if !strings.Contains(body, "V-"+ty) && !strings.Contains(body, "<label class=\"lbl\">Email") {
				// `email` paints the localized label, not the block's value.
				t.Errorf("%s block: the control is there but nothing names it for the buyer (page has no %q)", ty, "V-"+ty)
			}
		case asStatic:
			switch ty {
			case "image":
				if !strings.Contains(body, `src="https://cdn.example/image.png"`) {
					t.Errorf("image block: the validated https src is not on the page")
				}
			case "divider":
				if !strings.Contains(body, "<hr>") {
					t.Errorf("divider block: no <hr> on the page")
				}
			default:
				if !strings.Contains(body, "V-"+ty) {
					t.Errorf("%s block: the merchant's text is not on the page", ty)
				}
			}
			if in != "" {
				t.Errorf("%s block: a STATIC block rendered a control (%s) — it collects an answer it has no meaning for", ty, in)
			}
		case asPricing:
			if in != "" || strings.Contains(body, "V-"+ty) {
				t.Errorf("%s block: a payment-only pricing block reached the response page (%s)", ty, in)
			}
		}
	}
}

// TestResponseFormRequiredControlsCarryTheAttribute is the required-check
// guard: a block the merchant marked required must give the browser the means
// to enforce it. The browser runs constraint validation BEFORE the submit
// event, so `required` on the control is the only thing standing between an
// empty answer and a stored submission on this page — measured against the
// served bytes: without it checkValidity() was true and the handler POSTed the
// empty value. This is the guard the card's `textarea` defect fails: that arm
// was the one arm emitting maxlength but never required.
func TestResponseFormRequiredControlsCarryTheAttribute(t *testing.T) {
	body := renderResponsePage(t, "reqd", paletteTypeConfig(true))

	for _, ty := range paletteTypes() {
		switch ty {
		case "hidden":
			// The browser bars a hidden control from constraint validation, so
			// `required` on it would be a lie in the other direction.
			if in := controlNamed(body, "bk-"+ty); !strings.Contains(in, `type="hidden"`) {
				t.Errorf("hidden block: expected a hidden input, got %s", in)
			}
			if in := controlNamed(body, "bk-"+ty); strings.Contains(in, "required") {
				t.Errorf("hidden block: `required` on a hidden input cannot be enforced (%s)", in)
			}
			continue
		case "email":
			// The email arm is required unconditionally (a form that collects
			// an email address cannot store a submission without one).
		case "section", "html", "image", "divider", "product", "quantity", "option", "shipping", "total":
			continue // static / payment-only — no answer to require
		}
		in := controlNamed(body, "bk-"+ty)
		if in == "" {
			t.Errorf("%s block: no control on the page, so its required check is gone with it", ty)
			continue
		}
		if !strings.Contains(in, "required") {
			t.Errorf("%s block: the merchant marked it required and the control carries no `required` — an empty answer is submitted (%s)", ty, in)
		}
	}
}

// Guard the guard: controlNamed must find the control it is asked for, and
// must not match a lookalike id (the failure mode that would make the two
// guards above pass on a page that renders nothing).
func TestControlNamedMatchesOnlyItsOwnControl(t *testing.T) {
	page := fmt.Sprintf(`<input type="text" name="cpx"><input type="text" name="cpx1" required>`)
	if got := controlNamed(page, "cpx"); got != `<input type="text" name="cpx">` {
		t.Errorf("controlNamed(cpx) = %q, want only the exact match", got)
	}
	if got := controlNamed(page, "cpx1"); got != `<input type="text" name="cpx1" required>` {
		t.Errorf("controlNamed(cpx1) = %q", got)
	}
	if got := controlNamed(page, "nope"); got != "" {
		t.Errorf("controlNamed(nope) = %q, want empty", got)
	}
	// A page that carries no control at all — the withdrawn-coupon shape: the
	// block's label and a note, no input — must read as empty here, so the two
	// guards above can never pass on a bare label.
	if got := controlNamed(`<label class="lbl">Coupon</label><div class="desc">Discount codes are not applied on this form.</div>`, "cp1"); got != "" {
		t.Errorf("controlNamed(cp1) on a label-only page = %q, want empty", got)
	}
}
