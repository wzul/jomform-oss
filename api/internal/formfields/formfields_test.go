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
		{"missing field_id", []Field{{Type: "name", Label: "Nama"}}, "field_id wajib"},
		{"bad field_id", []Field{{FieldID: "a b", Type: "name", Label: "Nama"}}, "field_id tidak sah"},
		{"duplicate field_id", []Field{{FieldID: "x", Type: "name", Label: "A"}, {FieldID: "x", Type: "email", Label: "B"}}, "didaftar dua kali"},
		{"missing type", []Field{{FieldID: "x", Label: "A"}}, "type wajib"},
		{"unknown type", []Field{{FieldID: "x", Type: "hacker", Label: "A"}}, "type \"hacker\" tidak sah"},
		{"missing label", []Field{{FieldID: "x", Type: "name"}}, "label wajib"},
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
	if err == nil || !strings.Contains(err.Error(), "medan wajib tidak diisi: Nama") {
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
