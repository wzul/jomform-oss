package formfields

import (
	"strings"
	"testing"
)

func TestValidateContract(t *testing.T) {
	ok := []Field{
		{FieldID: "full_name", Type: "name", Label: "Nama", Required: true},
		{FieldID: "email", Type: "email", Label: "Emel"},
		{FieldID: "qty", Type: "number", Label: "Kuantiti", Validation: "^[0-9]+$"},
	}
	if err := ValidateContract(ok); err != nil {
		t.Fatalf("valid contract rejected: %v", err)
	}

	cases := []struct {
		name string
		f    []Field
		want string
	}{
		{"missing field_id", []Field{{Type: "name", Label: "Nama"}}, "field_id is required"},
		{"bad field_id", []Field{{FieldID: "a b", Type: "name", Label: "Nama"}}, "invalid field_id"},
		{"duplicate field_id", []Field{{FieldID: "x", Type: "name", Label: "A"}, {FieldID: "x", Type: "email", Label: "B"}}, "is registered twice"},
		{"missing type", []Field{{FieldID: "x", Label: "A"}}, "type is required"},
		{"unknown type", []Field{{FieldID: "x", Type: "hacker", Label: "A"}}, `invalid type "hacker"`},
		{"missing label", []Field{{FieldID: "x", Type: "name"}}, "label is required"},
	}
	for _, c := range cases {
		err := ValidateContract(c.f)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want contains %q", c.name, err, c.want)
		}
	}
}

func TestValidate(t *testing.T) {
	config := map[string]any{
		"registered_fields": []Field{
			{FieldID: "full_name", Type: "name", Label: "Nama", Required: true},
			{FieldID: "email", Type: "email", Label: "Emel", Required: true},
			{FieldID: "qty", Type: "number", Label: "Kuantiti", Validation: "^[0-9]+$"},
		},
	}

	// No contract → not enforced.
	if err := Validate(nil, nil); err != nil {
		t.Fatalf("nil config should not be enforced: %v", err)
	}

	// Valid submission passes.
	if err := Validate(config, map[string]string{"full_name": "Ali", "email": "ali@example.com", "qty": "3"}); err != nil {
		t.Fatalf("valid submission rejected: %v", err)
	}

	// Missing required field.
	if err := Validate(config, map[string]string{"email": "ali@example.com"}); err == nil {
		t.Fatal("missing required field should fail")
	}

	// Invalid email.
	if err := Validate(config, map[string]string{"full_name": "Ali", "email": "not-an-email"}); err == nil {
		t.Fatal("invalid email should fail")
	}

	// Invalid number.
	if err := Validate(config, map[string]string{"full_name": "Ali", "email": "ali@example.com", "qty": "abc"}); err == nil {
		t.Fatal("invalid number should fail")
	}

	// Validation pattern mismatch.
	if err := Validate(config, map[string]string{"full_name": "Ali", "email": "ali@example.com", "qty": "12a"}); err == nil {
		t.Fatal("pattern mismatch should fail")
	}
}

// Card t_589dc006: Validate derives the contract from the form's builder blocks
// when the config carries no registered_fields — the same derivation the menu
// checkout validates against — so a required block is enforced on a form's own
// submit route. An explicit registered_fields contract still wins.
func TestValidateFallsBackToBuilderBlocks(t *testing.T) {
	blocks := map[string]any{
		"blocks": []any{
			map[string]any{"id": "nm", "type": "name", "value": "Nama", "required": true},
			map[string]any{"id": "note", "type": "textarea", "value": "Nota"},
		},
	}
	// Missing required block → rejected, naming the merchant's own label.
	err := Validate(blocks, map[string]string{"note": "x"})
	if err == nil || !strings.Contains(err.Error(), "Nama is required") {
		t.Errorf("missing required block: got %v, want the required-field error", err)
	}
	// A blank required value is missing too.
	if err := Validate(blocks, map[string]string{"nm": "   "}); err == nil {
		t.Error("blank required value was accepted")
	}
	// Filled → accepted.
	if err := Validate(blocks, map[string]string{"nm": "Ali", "note": "x"}); err != nil {
		t.Errorf("valid submission rejected: %v", err)
	}
	// Not required → nothing to enforce (the pre-fix acceptance).
	if err := Validate(map[string]any{
		"blocks": []any{map[string]any{"id": "nm", "type": "name", "value": "Nama"}},
	}, map[string]string{}); err != nil {
		t.Errorf("optional block enforced: %v", err)
	}
	// registered_fields still wins over the blocks.
	if err := Validate(map[string]any{
		"blocks":            []any{map[string]any{"id": "nm", "type": "name", "value": "Nama", "required": true}},
		"registered_fields": []any{map[string]any{"field_id": "other", "type": "textarea", "label": "Lain"}},
	}, map[string]string{}); err != nil {
		t.Errorf("registered_fields did not win over the blocks: %v", err)
	}
	// No contract at all → still nothing enforced.
	if err := Validate(map[string]any{}, map[string]string{}); err != nil {
		t.Errorf("empty config enforced something: %v", err)
	}
}

func TestFromConfig(t *testing.T) {
	config := map[string]any{
		"registered_fields": []Field{{FieldID: "a", Type: "name", Label: "A"}},
	}
	got := FromConfig(config)
	if len(got) != 1 || got[0].FieldID != "a" {
		t.Fatalf("FromConfig = %+v, want 1 field 'a'", got)
	}
	if FromConfig(nil) != nil {
		t.Fatal("FromConfig(nil) should be nil")
	}
}

// ── Card t_7fb80479: `text` has ONE meaning — a single-line text INPUT ──────
//
// The block was ambiguous: the builder's palette offers it in the Standard
// group with placeholder / max length / default value / pattern, and both
// public renderers paint `<input type="text">`, while the seeded template
// library, the form-description derivation and the two type allowlists here
// still read it as an intro paragraph. The renderers won (three merged cards:
// t_99c3a08b, t_23289ce3, t_4086b8f0); these tests are the half of the
// reconciliation that lives in this package — the field-type contract.
//
// Removing `text` from either allowlist below reds one of them. A separate
// call-site test (httpapi/text_block_meaning_test.go) drives the two template
// derivations, and web/src/lib/formDescription.test.ts covers the builder's.

// The agent contract accepts `text`: it is the palette's only short-answer
// field, and an agent could not register it at all before this card.
func TestTextIsARegisterableInputType(t *testing.T) {
	if !ValidTypes["text"] {
		t.Fatal("ValidTypes excludes `text` — an agent cannot register the palette's only short-answer field")
	}
	if err := ValidateContract([]Field{{FieldID: "company", Type: "text", Label: "Syarikat"}}); err != nil {
		t.Errorf("a `text` field was rejected: %v", err)
	}
	// The display blocks stay OUT of the field contract: they carry copy, not
	// an answer. Prose is `section`/`html`, never a field type.
	for _, prose := range []string{"section", "html", "divider", "image"} {
		if ValidTypes[prose] {
			t.Errorf("ValidTypes accepts %q — a display block is not a field type", prose)
		}
	}
}

// A `text` block is part of the derived contract, so its answer is validated
// and recorded like every other input. Before this card it was dropped from
// the contract: `required` on a text field was an HTML attribute a direct POST
// could ignore, and Validate had nothing to check.
func TestTextBlockIsPartOfTheDerivedContract(t *testing.T) {
	cfg := map[string]any{"blocks": []any{
		map[string]any{"id": "intro", "type": "section", "value": "Selamat datang"},
		map[string]any{"id": "company", "type": "text", "value": "Syarikat", "required": true,
			"placeholder": "Sdn Bhd", "default_value": "Acme"},
	}}
	got := Contract(cfg)
	if len(got) != 1 {
		t.Fatalf("Contract returned %d fields, want 1 — the text block (%+v)", len(got), got)
	}
	f := got[0]
	if f.FieldID != "company" || f.Type != "text" || !f.Required ||
		f.Label != "Syarikat" || f.Placeholder != "Sdn Bhd" || f.DefaultValue != "Acme" {
		t.Errorf("text field = %+v, want the block's id/type/label + the merchant's settings", f)
	}

	// Empty submission → refused, naming the merchant's own label.
	err := Validate(cfg, map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "Syarikat is required") {
		t.Errorf("empty submission = %v, want the required-field error naming the label", err)
	}
	// A blank (whitespace-only) answer is missing too.
	if err := Validate(cfg, map[string]string{"company": "   "}); err == nil {
		t.Error("a blank required text answer was accepted")
	}
	// Filled → accepted.
	if err := Validate(cfg, map[string]string{"company": "Acme Sdn Bhd"}); err != nil {
		t.Errorf("valid submission rejected: %v", err)
	}
	// An agent-registered text field carries its validation pattern into the
	// server-side check, like every other registered field.
	reg := map[string]any{"registered_fields": []any{map[string]any{
		"field_id": "ic", "type": "text", "label": "IC", "validation": "^[0-9]{6}$"}}}
	if err := ValidateContract([]Field{{FieldID: "ic", Type: "text", Label: "IC", Validation: "^[0-9]{6}$"}}); err != nil {
		t.Fatalf("a registered `text` field with a pattern was rejected: %v", err)
	}
	if err := Validate(reg, map[string]string{"ic": "12ab34"}); err == nil {
		t.Error("a text answer breaking the registered pattern was accepted")
	}
	if err := Validate(reg, map[string]string{"ic": "123456"}); err != nil {
		t.Errorf("a text answer matching the registered pattern was rejected: %v", err)
	}
}

// Description reads the form's PROSE — its intro line — never a field's label.
func TestDescriptionIsTheIntroLineNotAFieldLabel(t *testing.T) {
	cases := []struct {
		name string
		cfg  map[string]any
		want string
	}{
		{"section intro wins over a text field, wherever it sits", map[string]any{"blocks": []any{
			map[string]any{"id": "nm", "type": "text", "value": "Nama"},
			map[string]any{"id": "in1", "type": "section", "value": "Get in touch — we usually reply within a day."},
		}}, "Get in touch — we usually reply within a day."},
		{"html counts as prose", map[string]any{"blocks": []any{
			map[string]any{"id": "h1", "type": "html", "value": "We value your feedback."},
			map[string]any{"id": "nm", "type": "text", "value": "Nama"},
		}}, "We value your feedback."},
		{"an empty display block is skipped", map[string]any{"blocks": []any{
			map[string]any{"id": "s1", "type": "section", "value": ""},
			map[string]any{"id": "s2", "type": "section", "value": "Book your slot."},
		}}, "Book your slot."},
		{"old content model: the intro is still in a text block", map[string]any{"blocks": []any{
			map[string]any{"id": "b1", "type": "text", "value": "Order from our menu — pay online when you submit."},
			map[string]any{"id": "nm", "type": "name", "value": "Full name"},
		}}, "Order from our menu — pay online when you submit."},
		{"fields only → no description", map[string]any{"blocks": []any{
			map[string]any{"id": "nm", "type": "name", "value": "Full name"},
			map[string]any{"id": "em", "type": "email", "value": "Email"},
		}}, ""},
		{"no blocks at all", map[string]any{}, ""},
		{"blank values are not a description", map[string]any{"blocks": []any{
			map[string]any{"id": "b1", "type": "text", "value": "   "},
			map[string]any{"id": "s1", "type": "section", "value": "  "},
		}}, ""},
	}
	for _, c := range cases {
		if got := Description(c.cfg); got != c.want {
			t.Errorf("%s: Description = %q, want %q", c.name, got, c.want)
		}
	}
	if got := Description(nil); got != "" {
		t.Errorf("Description(nil) = %q, want empty", got)
	}
	// The merchant's own sentence is copied verbatim (no truncation, no HTML
	// unescaping — the gallery JSON-encodes it).
	long := strings.Repeat("x", 400)
	if got := Description(map[string]any{"blocks": []any{map[string]any{"type": "section", "value": long}}}); got != long {
		t.Errorf("Description truncated a %d-char intro to %d chars", len(long), len(got))
	}
}
