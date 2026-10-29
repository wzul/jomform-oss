package httpapi

// Card t_fda24726 — the menu checkout rendered an attached form's `list` and
// `repeater` blocks as a single-line text input, while BOTH public renderers
// paint them as a multi-line <textarea rows="3">.
//
// Measured 2026-10-27 while fixing t_efd99d75: menuPageTmpl's "menuField" table
// had arms for select / radio / image-choice / checkbox / consent / textarea /
// address / number / date / time / website / hidden / phone and then an
// `{{else}}` fallback painting `<input type="text">`. `list` and `repeater` are
// collectable types (they reach the checkout's derived contract), but they had
// no arm, so they landed in that fallback. The answer WAS still collected and
// validated — this is a renderer-parity/UX defect, not data loss: a buyer's
// several-line list or repeated rows were typed into one line.
//
// Pinned here:
//   - the checkout's control for list / repeater is a <textarea rows="3">
//     carrying the block's placeholder and its required flag, inside the
//     merchant's <label>;
//   - the page's own collection script (jfMenuExtraFields) submits the typed
//     multi-line answer under the block id, and the control is inside the
//     checkout form that script reads;
//   - the server accepts that multi-line answer and still refuses a blank
//     required one, so the new control did not change what the order route
//     stores.
//
// The class guard lives next door: contract_renderer_parity_test.go's
// TestMenuCheckoutPaintsTheSameControlKindAsThePublicRenderers checks "textarea
// on both public renderers ⇒ textarea in the checkout" for every palette type,
// so the next arm that drifts this way is caught without a new card.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/formfields"
)

// listRepeaterConfig is the block shape the card measured: a required `list`
// with a placeholder, and an optional `repeater` with one, run through the same
// derivation the menu page runs.
func listRepeaterConfig() map[string]any {
	return map[string]any{"mode": "response", "blocks": []any{
		map[string]any{
			"id": "l1", "type": "list", "value": "Senarai", "required": true,
			"placeholder": "Satu item setiap baris",
		},
		map[string]any{
			"id": "rp1", "type": "repeater", "value": "Repeater",
			"placeholder": "Baris",
		},
	}}
}

func listRepeaterMenuFixture(t *testing.T) (workspaceMenuData, []formfields.Field) {
	t.Helper()
	contract := menuExtraContract(listRepeaterConfig())
	if len(contract) != 2 || contract[0].Type != "list" || contract[1].Type != "repeater" {
		t.Fatalf("fixture defect: menuExtraContract = %+v, want the list + repeater blocks", contract)
	}
	d := menu492Fixture(t)
	d.ExtraFields = menuExtraFields(contract, formLangs["en"])
	return d, contract
}

// The checkout paints both types as the multi-line control the public
// renderers paint — not the fallback single-line input.
func TestMenuCheckoutPaintsListAndRepeaterAsTextareas(t *testing.T) {
	d, _ := listRepeaterMenuFixture(t)
	body := renderMenu492(t, d)

	for _, want := range []struct {
		id, label, placeholder string
		required               bool
	}{
		{"l1", "Senarai", "Satu item setiap baris", true},
		{"rp1", "Repeater", "Baris", false},
	} {
		tag := controlKeyedBy(body, want.id)
		if tag == "" {
			t.Fatalf("the checkout renders no control for block %s", want.id)
		}
		if !strings.HasPrefix(tag, "<textarea") {
			t.Errorf("the checkout's control for block %s is %s, want a <textarea> — both public renderers "+
				"paint this type as <textarea rows=\"3\">", want.id, tag)
			continue
		}
		for _, attr := range []string{
			`name="` + want.id + `"`,
			`rows="3"`,
			`placeholder="` + want.placeholder + `"`,
		} {
			if !strings.Contains(tag, attr) {
				t.Errorf("the checkout's textarea for block %s is missing %s: %s", want.id, attr, tag)
			}
		}
		if got := strings.Contains(tag, "required"); got != want.required {
			t.Errorf("block %s required=%v but its control has required=%v: %s", want.id, want.required, got, tag)
		}
		if !strings.Contains(body, want.label) {
			t.Errorf("the checkout does not carry the merchant's label %q for block %s", want.label, want.id)
		}
		// The defect itself: the fallback single-line input must be gone.
		if strings.Contains(body, `<input type="text" name="`+want.id+`"`) {
			t.Errorf("block %s still renders as the fallback single-line text input", want.id)
		}
	}
}

// Structural check through the page's own document, not the template source:
// each control is a TEXTAREA with rows=3, sits inside its merchant label, and is
// a descendant of the checkout form the collection script reads.
func TestMenuCheckoutListAndRepeaterDOM(t *testing.T) {
	d, _ := listRepeaterMenuFixture(t)
	dom, _ := newMenuDOMPage(t, d)

	for _, want := range []struct {
		id, label string
		required  bool
	}{
		{"l1", "Senarai (required)", true},
		{"rp1", "Repeater", false},
	} {
		expr := `(() => {
			var el = document.querySelector('[name=` + want.id + `]');
			if (!el) return JSON.stringify({missing: true});
			var l = el.closest('label');
			return JSON.stringify({
				tag: el.tagName,
				rows: el.getAttribute('rows'),
				placeholder: el.getAttribute('placeholder'),
				name: el.name,
				required: el.hasAttribute('required'),
				inCheckoutForm: !!el.closest('#menuCheckout'),
				label: l ? l.textContent.trim() : null
			});
		})()`
		v, err := dom.vm.RunString(expr)
		if err != nil {
			t.Fatalf("read block %s's control from the served page: %v", want.id, err)
		}
		var got struct {
			Missing        bool   `json:"missing"`
			Tag            string `json:"tag"`
			Rows           string `json:"rows"`
			Placeholder    string `json:"placeholder"`
			Name           string `json:"name"`
			Required       bool   `json:"required"`
			InCheckoutForm bool   `json:"inCheckoutForm"`
			Label          string `json:"label"`
		}
		if err := json.Unmarshal([]byte(v.String()), &got); err != nil {
			t.Fatalf("decode block %s's control = %s: %v", want.id, v.String(), err)
		}
		if got.Missing {
			t.Fatalf("the served page has no control named %s", want.id)
		}
		if got.Tag != "TEXTAREA" {
			t.Errorf("block %s is a <%s> in the served page, want TEXTAREA", want.id, got.Tag)
		}
		if got.Rows != "3" {
			t.Errorf("block %s's textarea rows = %q, want 3 (the public renderers' height)", want.id, got.Rows)
		}
		if got.Placeholder == "" {
			t.Errorf("block %s's textarea lost the merchant's placeholder", want.id)
		}
		if got.Name != want.id {
			t.Errorf("block %s's textarea is named %q — the script submits under the block id", want.id, got.Name)
		}
		if got.Required != want.required {
			t.Errorf("block %s required=%v in the served page, want %v", want.id, got.Required, want.required)
		}
		if !got.InCheckoutForm {
			t.Errorf("block %s's control is outside #menuCheckout, so the collection script never reads it", want.id)
		}
		if got.Label != want.label {
			t.Errorf("block %s's control label = %q, want %q", want.id, got.Label, want.label)
		}
	}
}

// The page's own script is what turns the typed answer into the `fields{}`
// payload the order route validates: a multi-line answer must arrive under the
// block id with its line breaks intact.
func TestMenuCheckoutCollectsAMultiLineListAnswer(t *testing.T) {
	d, _ := listRepeaterMenuFixture(t)
	dom, _ := newMenuDOMPage(t, d)

	const answer = "Nasi lemak\nSambal\nTelur"
	script := `(() => {
		var el = document.querySelector('[name=l1]');
		if (!el) throw new Error('no control for the list block');
		el.value = ` + jsString(t, answer) + `;
		return JSON.stringify(jfMenuExtraFields());
	})()`
	v, err := dom.vm.RunString(script)
	if err != nil {
		t.Fatalf("type a multi-line answer and run the page's jfMenuExtraFields(): %v", err)
	}
	var fields map[string]string
	if err := json.Unmarshal([]byte(v.String()), &fields); err != nil {
		t.Fatalf("decode jfMenuExtraFields() = %q: %v", v.String(), err)
	}
	if fields["l1"] != answer {
		t.Errorf("the checkout submitted fields[l1] = %q, want the typed multi-line answer %q (payload %q)",
			fields["l1"], answer, v.String())
	}
}

// Server side: a multi-line answer is accepted (the control change did not alter
// what the order route stores), and the required arm still refuses a blank one.
func TestListAndRepeaterAcceptAMultiLineAnswer(t *testing.T) {
	_, contract := listRepeaterMenuFixture(t)

	answers := map[string]string{
		"l1":  "Nasi lemak\nSambal\nTelur",
		"rp1": "baris satu\nbaris dua",
	}
	if err := formfields.ValidateFields(contract, answers); err != nil {
		t.Errorf("a multi-line list/repeater answer was refused: %v", err)
	}
	if err := formfields.ValidateFields(contract, map[string]string{"rp1": "x"}); err == nil ||
		!strings.Contains(err.Error(), "Senarai") {
		t.Errorf("a blank required list answer = %v, want a refusal naming the merchant's label", err)
	}
}

// jsString renders a Go string as a JS string literal for the goja harness.
func jsString(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("encode JS string %q: %v", s, err)
	}
	return string(b)
}
