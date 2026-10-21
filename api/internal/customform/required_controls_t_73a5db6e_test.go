package customform

// Card t_73a5db6e: a registered `required:true` field with no matching control
// in the HTML is not refused at save time, so every submit answers 400.
//
// The server treats the registered fields as the submit contract
// (ValidationContract → formfields.ValidateFields: a required field must carry a
// non-empty value), but the buyer's page can only produce a value for a control
// it can find — render_custom.go's bridge posts doc.querySelectorAll('[name]')
// from inside the sandboxed iframe. A form saved with
// fields:[{field_id:"nama", type:"text", required:true}] and no name="nama"
// control is therefore PERMANENTLY unsubmittable: every POST answers
// 400 `medan wajib tidak diisi: nama`, and the only client-visible symptom is
// the page's own alert(). Nothing refused the mismatch at write time, so it
// could be stored (customform.Validate checked only that field_id was
// non-empty/unique and the type supported).
//
// The rule below is exactly the bridge's: a name attribute only helps when the
// element also has a string `value` the bridge can read (el.name + el.value).
// The check runs over SanitizeHTML's output because that is the bytes the
// renderer serves.

import (
	"strings"
	"testing"
)

// controlNames covers the elements a name attribute can actually help on, and
// the ones it cannot — the pair matters, because a guard that accepted any
// element carrying name="…" would let the unsatisfiable contract through on a
// `<div name>`.
func TestRequiredControlsRuleMatchesWhatTheBridgeCanRead(t *testing.T) {
	required := []Field{{ID: "nama", Type: "text", Label: "Nama", Required: true}}

	cases := []struct {
		name string
		html string
		sat  bool // the contract is satisfiable (Validate must accept it)
		why  string
	}{
		{
			name: "plain input",
			html: `<div><label>Nama</label><input type="text" name="nama" required></div>`,
			sat:  true,
		},
		{
			name: "select",
			html: `<select name="nama"><option value="a">A</option></select>`,
			sat:  true,
		},
		{
			name: "textarea",
			html: `<textarea name="nama"></textarea>`,
			sat:  true,
		},
		{
			name: "checkbox and radio post their own key",
			html: `<input type="checkbox" name="nama"><input type="radio" name="nama" value="x">`,
			sat:  true,
		},
		{
			name: "button with a value",
			html: `<button type="submit" name="nama" value="yes">Hantar</button>`,
			sat:  true,
		},
		{
			name: "attribute values are HTML-decoded before matching",
			html: `<input type="text" name="na&#109;a">`,
			sat:  true,
			why:  "&#109; is 'm' — the DOM hands the bridge the decoded value",
		},
		{
			// The reported defect, in its plainest form.
			name: "registered field with no control at all",
			html: `<div><label>Nama</label><input type="text" name="other"></div>`,
			sat:  false,
		},
		{
			name: "empty HTML is not a custom form at all",
			html: "   ",
			sat:  true,
			why:  "empty html clears the custom form and falls back to the block renderer",
		},
		{
			name: "name on a div cannot produce a value",
			html: `<div name="nama">Nama</div>`,
			sat:  false,
			why: "querySelectorAll('[name]') selects it but el.name/el.value are undefined, " +
				"so the key the server validates never appears",
		},
		{
			name: "name on an anchor cannot produce a value",
			html: `<a name="nama">Nama</a>`,
			sat:  false,
		},
		{
			name: "button without a value posts an empty string",
			html: `<button type="submit" name="nama">Hantar</button>`,
			sat:  false,
			why:  "a button's default value is \"\", so the required check would still fail",
		},
		{
			name: "case must match the registered id",
			html: `<input type="text" name="Nama">`,
			sat:  false,
			why:  "the server matches the posted key against field_id verbatim",
		},
		{
			name: "the sanitizer strips the element carrying the name",
			html: `<iframe name="nama"></iframe><p>hi</p>`,
			sat:  false,
			why:  "iframe is skipped before render, so the served page has no such control",
		},
		{
			// The case that separates "parse the raw html" from "parse what the
			// renderer serves": object content is parsed as markup, so the raw
			// body DOES hold this input — and the sanitizer drops the subtree,
			// so the buyer's page never has it.
			name: "a control inside an element the sanitizer skips entirely",
			html: `<object data="x"><input type="text" name="nama"></object>`,
			sat:  false,
			why:  "SkipElementsContent(object) removes the subtree from the served html",
		},
		{
			name: "markup inside a script is not a control",
			html: `<script>var s = '<input name="nama">';</script><p>hi</p>`,
			sat:  false,
			why:  "script content is dropped by the sanitizer and never parsed as markup",
		},
		{
			name: "an unquoted name attribute still counts",
			html: `<input type=text name=nama>`,
			sat:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cc := &Config{HTML: tc.html, Fields: required}
			err := Validate(cc)
			if tc.sat {
				if err != nil {
					t.Fatalf("satisfiable contract refused: %v (%s)", err, tc.why)
				}
				return
			}
			if err == nil {
				t.Fatalf("unsatisfiable contract ACCEPTED for %q (%s) — this form could be saved "+
					"and every submit would answer 400 `medan wajib tidak diisi: Nama`", tc.html, tc.why)
			}
			if msg := err.Error(); !strings.Contains(msg, "medan wajib tiada dalam HTML") ||
				!strings.Contains(msg, `name="nama"`) {
				t.Fatalf("refusal message = %q, want it to name the missing control (name=\"nama\") "+
					"so the caller knows what to add", msg)
			}
		})
	}
}

// A `required:false` field needs no control: the buyer simply cannot fill it in,
// and the submit still succeeds. Refusing that would break the legitimate
// "registered but not on this page" shape an in-progress draft can have.
func TestOptionalFieldWithoutControlIsAccepted(t *testing.T) {
	cc := &Config{
		HTML: `<input type="text" name="nama">`,
		Fields: []Field{
			{ID: "nama", Type: "text", Required: true},
			{ID: "catatan", Type: "textarea"}, // optional, absent from the HTML
		},
	}
	if err := Validate(cc); err != nil {
		t.Fatalf("optional field absent from the HTML was refused: %v", err)
	}
}

// Every missing required field is named, not just the first: an agent fixing the
// form should learn the whole list in one round trip.
func TestRefusalNamesEveryMissingRequiredControl(t *testing.T) {
	cc := &Config{
		HTML: `<input type="text" name="emel">`,
		Fields: []Field{
			{ID: "nama", Type: "text", Required: true},
			{ID: "emel", Type: "email", Required: true},
			{ID: "umur", Type: "number", Required: true},
		},
	}
	err := Validate(cc)
	if err == nil {
		t.Fatal("contract with two missing required controls was accepted")
	}
	msg := err.Error()
	for _, id := range []string{`name="nama"`, `name="umur"`} {
		if !strings.Contains(msg, id) {
			t.Errorf("refusal message %q does not name the missing control %s", msg, id)
		}
	}
	if strings.Contains(msg, `name="emel"`) {
		t.Errorf("refusal message %q names a control that IS in the HTML (emel)", msg)
	}
}

// ValidateConfig is the entry point the REST/MCP write paths call with the raw
// config map (a custom_html section that arrives as JSON, not as a Config).
func TestValidateConfigOverRawMap(t *testing.T) {
	bad := map[string]any{
		"mode": "response",
		ConfigKey: map[string]any{
			"html":   `<div><p>tiada input</p></div>`,
			"fields": []any{map[string]any{"field_id": "nama", "type": "text", "required": true}},
		},
	}
	if err := ValidateConfig(bad); err == nil {
		t.Fatal("config map with an unsatisfiable custom_html contract was accepted")
	}

	good := map[string]any{
		"mode": "response",
		ConfigKey: map[string]any{
			"html":   `<div><input type="text" name="nama"></div>`,
			"fields": []any{map[string]any{"field_id": "nama", "type": "text", "required": true}},
		},
	}
	if err := ValidateConfig(good); err != nil {
		t.Fatalf("satisfiable config map refused: %v", err)
	}

	// No custom_html section, or no config at all: nothing to check (a block
	// form keeps working, and a PATCH with no config means "leave unchanged").
	if err := ValidateConfig(map[string]any{"blocks": []any{}}); err != nil {
		t.Errorf("config without custom_html refused: %v", err)
	}
	if err := ValidateConfig(nil); err != nil {
		t.Errorf("nil config refused: %v", err)
	}

	// A custom_html the package cannot decode is refused instead of skipped:
	// it is not renderable as a custom form, yet it looks configured.
	if err := ValidateConfig(map[string]any{ConfigKey: "bukan objek"}); err == nil {
		t.Error("undecodable custom_html accepted — Enabled() would silently fall back to the block renderer")
	}
}
