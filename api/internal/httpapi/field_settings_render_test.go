package httpapi

// Card t_23289ce3: the builder offers per-field settings that NO renderer ever
// emitted. The settings are stored in `forms.config.blocks[]` (the builder's
// panel writes them, MCP carries them, the menu checkout reads some of them),
// but both public renderers — blockTmpl (payment/order) and responseTmpl
// (response) — ignored them, so a merchant filled in "max length 20", hit
// Save, and the published page took 500 characters.
//
// These tests drive the REAL call sites (renderBlocks / renderResponseForm),
// never the templates by hand: a template-level test stays green while the
// switch or the view struct drops the setting before the template is reached —
// which is exactly the shape of the defect.
//
// Every assertion below is on the EMITTED ATTRIBUTE, not on the struct: the
// value being carried into blockView/fieldView proves nothing about the HTML
// the buyer is served.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// perFieldBlocks is the stored config the builder produces when a merchant
// fills in every per-field setting the panel offers: placeholder, description,
// CSS class, inline style, max length, default value, min/max/step, validation
// pattern, required message, choices and the date/time format.
func perFieldBlocks() []any {
	return []any{
		map[string]any{
			"id": "t1", "type": "text", "value": "Short answer",
			"placeholder": "Type here", "max_length": 20, "default_value": "Ali",
			"pattern": "[A-Za-z ]{1,20}", "description": "Helper text under the field",
			"css_class": "jf-wide", "style": "color:#111827",
		},
		map[string]any{
			"id": "a1", "type": "textarea", "value": "Notes",
			"placeholder": "More detail", "max_length": 300, "default_value": "None",
			"description": "Long answer", "css_class": "jf-wide",
			"required": true, "required_message": "Please write something",
		},
		map[string]any{
			"id": "n1", "type": "name", "value": "Your name",
			"required": true, "required_message": "Tell us your name",
			"description": "As printed on your IC",
		},
		map[string]any{
			"id": "e1", "type": "email", "value": "Email",
			"placeholder": "you@example.com", "pattern": "[^@]+@[^@]+",
		},
		map[string]any{
			"id": "p1", "type": "phone", "value": "Phone",
			"placeholder": "0123456789", "pattern": "[0-9]{9,11}",
		},
		map[string]any{
			"id": "w1", "type": "website", "value": "Website",
			"placeholder": "https://example.com", "pattern": "https://.*",
		},
		map[string]any{
			"id": "m1", "type": "number", "value": "Qty",
			"placeholder": "1", "min": "1", "max": "10", "step": "1",
		},
		map[string]any{
			"id": "d1", "type": "date", "value": "Preferred slot",
			"format": "datetime", "css_class": "jf-when",
		},
		map[string]any{
			"id": "c1", "type": "select", "value": "Colour", "required": true,
			"required_message": "Pick a colour",
			"choices": []any{
				map[string]any{"label": "Red", "value": "red"},
				map[string]any{"label": "Blue", "value": "blue"},
			},
		},
		map[string]any{
			"id": "g1", "type": "consent", "value": "I agree", "required": true,
			"required_message": "Please tick to continue",
		},
	}
}

// renderPaymentPage renders a payment-mode form through the handler that serves
// it and returns the page a buyer receives.
func renderPaymentPage(t *testing.T, slug string, cfg map[string]any) string {
	t.Helper()
	h := &PublicHandlers{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://demo.jomform.com/"+slug, nil)
	if !h.renderBlocks(rec, req, themedForm(slug, "#5e6ad2", cfg)) {
		t.Fatalf("renderBlocks declined a payment form with blocks: %v", cfg["blocks"])
	}
	return rec.Body.String()
}

// The text block is the field the card's measurement names ("he sets max length
// 20 … and the field accepts 500 characters"). Its arm must carry every setting
// the panel offers for it.
func TestPaymentPageRendersTextFieldSettings(t *testing.T) {
	body := renderPaymentPage(t, "pf-payment", map[string]any{
		"mode": "payment", "blocks": perFieldBlocks(), "products": []any{},
	})

	// The whole control, attribute for attribute: a substring check per
	// attribute would stay green if two of them ever collided.
	want := `<input type="text" name="t1" data-id="t1" class="jf-wide" placeholder="Type here" maxlength="20" value="Ali" pattern="[A-Za-z ]{1,20}">`
	if !strings.Contains(body, want) {
		t.Errorf("the text block's input is missing settings the panel offers.\nwant: %s", want)
	}
	// The label is the block's own value and the inline style is sanitized onto
	// it (issue #388) — the pre-fix arm rendered the value as a paragraph.
	if !strings.Contains(body, `<label class="lbl" style="color: #111827">Short answer</label>`) {
		t.Errorf("the text block did not render a label from its value:\n%.400s", body)
	}
	// The description is helper text UNDER the field (bld.descriptionPh).
	if !strings.Contains(body, `<div class="desc">Helper text under the field</div>`) {
		t.Error("the text block's description was not rendered")
	}
}

// Every field type the panel offers these settings for must emit them.
func TestPaymentPageRendersPerFieldSettings(t *testing.T) {
	body := renderPaymentPage(t, "pf-payment2", map[string]any{
		"mode": "payment", "blocks": perFieldBlocks(), "products": []any{},
	})

	for _, tc := range []struct{ what, want string }{
		{"textarea placeholder", `placeholder="More detail"`},
		{"textarea max length", `maxlength="300"`},
		{"textarea default value", `>None</textarea>`},
		{"textarea class", `class="jf-wide"`},
		{"textarea description", `<div class="desc">Long answer</div>`},
		{"email placeholder", `placeholder="you@example.com"`},
		{"email pattern", `pattern="[^@]&#43;@[^@]&#43;"`}, // html/template escapes `+` as `&#43;`
		{"phone placeholder", `placeholder="0123456789"`},
		{"phone pattern", `pattern="[0-9]{9,11}"`},
		{"website placeholder", `placeholder="https://example.com"`},
		{"website pattern", `pattern="https://.*"`},
		{"number placeholder", `placeholder="1"`},
		{"number min/max/step", `min="1" max="10" step="1"`},
		{"name description", `<div class="desc">As printed on your IC</div>`},
		{"date class", `class="jf-when"`},
	} {
		if !strings.Contains(body, tc.want) {
			t.Errorf("%s: %q is not in the served page", tc.what, tc.want)
		}
	}

	// required_message reaches the buyer's own validation: the browser shows the
	// merchant's words instead of "Please fill out this field".
	for _, tc := range []struct{ what, want string }{
		{"textarea required message", `oninvalid="this.setCustomValidity('Please write something')"`},
		{"name required message", `oninvalid="this.setCustomValidity('Tell us your name')"`},
		{"select required message", `oninvalid="this.setCustomValidity('Pick a colour')"`},
		{"consent required message", `oninvalid="this.setCustomValidity('Please tick to continue')"`},
		{"required stays enforced", `required`},
	} {
		if !strings.Contains(body, tc.want) {
			t.Errorf("%s: %q is not in the served page", tc.what, tc.want)
		}
	}
	// The hook must not leave a field permanently invalid: any input clears it.
	if !strings.Contains(body, `oninput="this.setCustomValidity('')"`) {
		t.Error("the required-message hook never clears itself — a corrected field would stay invalid")
	}
}

// The date/time format picker chooses the control the buyer gets.
func TestPaymentPageHonoursDateFormat(t *testing.T) {
	body := renderPaymentPage(t, "pf-date", map[string]any{
		"mode": "payment", "products": []any{},
		"blocks": []any{
			map[string]any{"id": "d1", "type": "date", "value": "Day", "format": "datetime"},
			map[string]any{"id": "d2", "type": "date", "value": "Day"},
			map[string]any{"id": "t9", "type": "time", "value": "Hour", "format": "date"},
			map[string]any{"id": "t8", "type": "time", "value": "Hour"},
		},
	})
	for _, tc := range []struct{ what, want string }{
		{"date + Date & time", `<input type="datetime-local" name="notes" data-id="d1"`},
		{"date + default", `<input type="date" name="notes" data-id="d2"`},
		{"time + Date", `<input type="date" name="notes" data-id="t9"`},
		{"time + default", `<input type="time" name="notes" data-id="t8"`},
	} {
		if !strings.Contains(body, tc.want) {
			t.Errorf("%s: %q is not in the served page", tc.what, tc.want)
		}
	}
}

// The response renderer serves the same stored config; the settings must land
// there too (the two renderers disagreeing about a block is the defect class
// this card comes from).
func TestResponsePageRendersPerFieldSettings(t *testing.T) {
	body := renderResponsePage(t, "pf-response", map[string]any{
		"mode": "response", "blocks": perFieldBlocks(), "products": []any{},
	})

	for _, tc := range []struct{ what, want string }{
		{"text placeholder", `placeholder="Type here"`},
		{"text max length", `maxlength="20"`},
		{"text default value", `value="Ali"`},
		{"text pattern", `pattern="[A-Za-z ]{1,20}"`},
		{"text class", `class="jf-wide"`},
		{"text description", `<div class="desc">Helper text under the field</div>`},
		{"textarea placeholder", `placeholder="More detail"`},
		{"textarea max length", `maxlength="300"`},
		{"textarea required", `required`},
		{"textarea required message", `oninvalid="this.setCustomValidity('Please write something')"`},
		{"email pattern", `pattern="[^@]&#43;@[^@]&#43;"`},
		{"phone pattern", `pattern="[0-9]{9,11}"`},
		{"website placeholder", `placeholder="https://example.com"`},
		{"number placeholder", `placeholder="1"`},
		{"number min/max/step", `min="1" max="10" step="1"`},
		{"name description", `<div class="desc">As printed on your IC</div>`},
		{"select required message", `oninvalid="this.setCustomValidity('Pick a colour')"`},
		{"consent required message", `oninvalid="this.setCustomValidity('Please tick to continue')"`},
		{"date + Date & time", `<input type="datetime-local" name="d1"`},
	} {
		if !strings.Contains(body, tc.want) {
			t.Errorf("%s: %q is not in the served page", tc.what, tc.want)
		}
	}
	// The description is helper text under the FIELD, never a second input.
	if n := strings.Count(body, `<div class="desc">`); n != 3 {
		t.Errorf(`class="desc" appears %d times, want 3 (one per description)`, n)
	}
}

// The card is explicit: the settings that already worked must keep working —
// required, choices, min/max/step and default value were never the defect, and
// a fix that quietly dropped one of them would be a regression.
func TestPaymentPageKeepsTheSettingsThatAlreadyWorked(t *testing.T) {
	body := renderPaymentPage(t, "pf-keep", map[string]any{
		"mode": "payment", "products": []any{},
		"blocks": []any{
			map[string]any{"id": "s1", "type": "select", "value": "Colour", "required": true,
				"choices": []any{
					map[string]any{"label": "Red", "value": "red"},
					map[string]any{"label": "Blue", "value": "blue"},
				}},
			map[string]any{"id": "q1", "type": "quantity", "value": "Qty", "default_value": "2"},
			map[string]any{"id": "m1", "type": "number", "value": "N", "min": "2", "max": "8", "step": "2"},
			map[string]any{"id": "h1", "type": "hidden", "value": "utm", "default_value": "news"},
		},
	})
	for _, tc := range []struct{ what, want string }{
		{"select keeps required", `required`},
		{"select keeps its choices", `<option value="red">Red</option>`},
		{"select keeps the second choice", `<option value="blue">Blue</option>`},
		{"quantity keeps its default", `class="qty" min="1" value="2"`},
		{"number keeps min/max/step", `min="2" max="8" step="2"`},
		{"hidden keeps its default", `<input type="hidden" name="notes" data-id="h1" value="news"`},
	} {
		if !strings.Contains(body, tc.want) {
			t.Errorf("%s: %q is not in the served page", tc.what, tc.want)
		}
	}
}

// Every value here is merchant-supplied text that lands inside an HTML
// attribute or an element. Nothing may break out of it — the page is served to
// anonymous buyers on the merchant's own domain.
func TestPerFieldSettingsCannotBreakOutOfTheirMarkup(t *testing.T) {
	cfg := map[string]any{
		"mode": "payment", "products": []any{},
		"blocks": []any{map[string]any{
			"id": "x1", "type": "text", "value": "Label",
			"css_class":   `jf-x" onmouseover="alert(1)`,
			"description": "<script>alert(1)</script>",
			"placeholder": `"><img src=x onerror=alert(1)>`,
			"pattern":     `"><svg onload=alert(1)>`,
			"required":    true, "required_message": `it's "quoted" </script>`,
		}},
	}
	for _, page := range []struct {
		what string
		body string
	}{
		{"payment", renderPaymentPage(t, "pf-escape", cfg)},
		{"response", renderResponsePage(t, "pf-escape-r", map[string]any{
			"mode": "response", "blocks": cfg["blocks"].([]any), "products": []any{},
		})},
	} {
		// Raw markup, or a raw quote closing the attribute the value sits in.
		// (`onerror=alert(1)` DOES appear in the served bytes — the placeholder
		// attribute carries it as text inside `&lt;img …&gt;`, which is inert;
		// what must never appear is the raw `<`, or a `"` that closes the value.)
		for _, bad := range []string{
			"<img src=x", "<svg onload", "<script>alert(1)</script>",
			`onmouseover="alert(1)`, `jf-x" onmouseover`,
		} {
			if strings.Contains(page.body, bad) {
				t.Errorf("%s page carries an unescaped %q — a merchant value broke out of its attribute", page.what, bad)
			}
		}
		// …and the values must still REACH the page, escaped rather than
		// dropped: a silent omission would pass every check above.
		for _, good := range []string{
			`class="jf-x&#34; onmouseover=&#34;alert(1)"`,
			`placeholder="&#34;&gt;&lt;img src=x onerror=alert(1)&gt;"`,
			`<div class="desc">&lt;script&gt;alert(1)&lt;/script&gt;</div>`,
		} {
			if !strings.Contains(page.body, good) {
				t.Errorf("%s page: %q is missing — the merchant's value was dropped, not escaped", page.what, good)
			}
		}
		// The custom validity hook is a JS string inside an attribute: the
		// merchant's apostrophe must be JS-escaped (`\u0027`), because an HTML
		// entity (`&#39;`) is decoded back to `'` BEFORE the JS runs and would
		// close the string.
		if !strings.Contains(page.body, `oninvalid="this.setCustomValidity('it\u0027s \u0022quoted\u0022 \u003c\/script\u003e')"`) {
			t.Errorf("%s page: the required message is not JS-string escaped", page.what)
		}
	}
}
