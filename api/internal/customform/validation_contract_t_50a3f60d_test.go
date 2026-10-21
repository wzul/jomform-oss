package customform

// Card t_50a3f60d: the registered custom-field contract must become the
// server-side validation contract the public submit handler enforces. This
// pins the mapping (type vocabulary + label fallback) so a field registered as
// `email` cannot silently lose its format check on the way to
// formfields.ValidateFields.

import (
	"testing"

	"github.com/wzul/jomform/api/internal/formfields"
)

func TestValidationContractMapsRegisteredFields(t *testing.T) {
	c := &Config{Fields: []Field{
		{ID: "name", Type: "text", Label: "Name", Required: true},
		{ID: "email", Type: "email", Label: "Email", Required: true},
		{ID: "tel", Type: "tel", Label: "Phone"},
		{ID: "site", Type: "url", Label: "Website"},
		{ID: "qty", Type: "number", Label: "Quantity"},
		{ID: "feedback", Type: "textarea"},
	}}
	got := ValidationContract(c)
	if len(got) != len(c.Fields) {
		t.Fatalf("contract has %d fields, want %d: %+v", len(got), len(c.Fields), got)
	}
	wantType := map[string]string{
		"name": "text", "email": "email", "tel": "phone",
		"site": "website", "qty": "number", "feedback": "textarea",
	}
	for _, f := range got {
		if f.Type != wantType[f.FieldID] {
			t.Errorf("field %s type = %q, want %q", f.FieldID, f.Type, wantType[f.FieldID])
		}
	}
	if got[0].Label != "Name" || !got[0].Required || !got[1].Required {
		t.Errorf("required/label not carried: %+v", got[:2])
	}
	// No label registered: the message must still name something findable.
	if got[5].Label != "feedback" {
		t.Errorf("label fallback = %q, want the field id", got[5].Label)
	}
}

func TestValidationContractEnforcesRequiredAndTypes(t *testing.T) {
	regs := ValidationContract(&Config{Fields: []Field{
		{ID: "name", Type: "text", Label: "Name", Required: true},
		{ID: "email", Type: "email", Label: "Email", Required: true},
		{ID: "qty", Type: "number", Label: "Quantity"},
		{ID: "site", Type: "url", Label: "Website"},
	}})

	if err := formfields.ValidateFields(regs, map[string]string{"email": "a@b.test"}); err == nil {
		t.Fatal("missing required field passed validation")
	} else if err.Error() != "medan wajib tidak diisi: Name" {
		t.Errorf("error = %q, want it to name Name", err.Error())
	}
	if err := formfields.ValidateFields(regs, map[string]string{"name": "Ali", "email": "nope"}); err == nil {
		t.Error("non-email passed a type=email field")
	}
	if err := formfields.ValidateFields(regs, map[string]string{"name": "Ali", "email": "a@b.test", "qty": "x"}); err == nil {
		t.Error("non-number passed a type=number field")
	}
	if err := formfields.ValidateFields(regs, map[string]string{"name": "Ali", "email": "a@b.test", "site": "jomform.com"}); err == nil {
		t.Error("scheme-less value passed a type=url field")
	}
	if err := formfields.ValidateFields(regs, map[string]string{
		"name": "Ali", "email": "a@b.test", "qty": "2", "site": "https://jomform.com",
	}); err != nil {
		t.Errorf("valid answers rejected: %v", err)
	}
}

func TestValidationContractEmpty(t *testing.T) {
	if got := ValidationContract(nil); got != nil {
		t.Errorf("nil config produced %+v", got)
	}
	if got := ValidationContract(&Config{HTML: "<p>x</p>"}); got != nil {
		t.Errorf("config with no registered fields produced %+v", got)
	}
}
