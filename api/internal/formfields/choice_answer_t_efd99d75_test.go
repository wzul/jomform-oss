package formfields

// Card t_efd99d75 — a choice-bearing field accepted any string.
//
// `image-choice` is a collectable type (collectableTypes), so an attached
// form's image-choice block reaches the menu checkout's derived contract with
// its `choices`. ValidateFields had arms for email/website/number and an
// optional pattern, but nothing for a field that OFFERS choices: a hand-written
// POST could answer `select`, `radio` or `image-choice` with a string the
// merchant never listed and it was recorded as the buyer's answer. On the menu
// checkout the block was additionally painted as a free-text box, so the buyer
// could not answer it with a choice at all (that renderer half is pinned in
// httpapi/menu_choice_fields_t_efd99d75_test.go).
//
// The rule these tests pin: a field that declared choices accepts exactly the
// declared values. A contract that declares none is not membership-checked —
// registered_fields may carry a select without choices, and a contract with
// nothing to compare against must not refuse a legitimate answer.

import (
	"strings"
	"testing"
)

// choiceContract derives a one-field contract the way the menu checkout does —
// from builder blocks — so the fixture cannot drift from the production writer.
func choiceContract(t *testing.T, typ string) []Field {
	t.Helper()
	block := map[string]any{"id": "f1", "type": typ, "value": "Pilih " + typ, "required": true}
	switch typ {
	case "select", "radio", "image-choice":
		block["choices"] = []any{
			map[string]any{"label": "Kecil", "value": "k"},
			map[string]any{"label": "Besar", "value": "b"},
		}
	}
	regs := Contract(map[string]any{"blocks": []any{block}})
	if len(regs) != 1 || regs[0].FieldID != "f1" {
		t.Fatalf("fixture defect: Contract(%s) = %+v, want the one block f1", typ, regs)
	}
	return regs
}

// Every value the merchant declared is accepted; a value outside the declared
// set is refused, naming the field.
func TestChoiceFieldsAcceptOnlyDeclaredValues(t *testing.T) {
	for _, typ := range []string{"select", "radio", "image-choice"} {
		t.Run(typ, func(t *testing.T) {
			regs := choiceContract(t, typ)
			if len(regs[0].Choices) != 2 {
				t.Fatalf("%s: derived contract lost the merchant's choices: %+v", typ, regs[0])
			}
			for _, good := range []string{"k", "b"} {
				if err := ValidateFields(regs, map[string]string{"f1": good}); err != nil {
					t.Errorf("%s: declared choice %q was refused: %v", typ, good, err)
				}
			}
			for _, bad := range []string{"anything at all", "K", "c"} {
				err := ValidateFields(regs, map[string]string{"f1": bad})
				if err == nil {
					t.Fatalf("%s: answer %q is not one of the merchant's choices but was accepted", typ, bad)
				}
				// Assert the message, not merely "an error": the required-blank
				// arm produces an error too, and only this text can tell the
				// membership refusal apart from it.
				if !strings.Contains(err.Error(), "pilihan tidak sah: Pilih "+typ) {
					t.Errorf("%s: answer %q = %v, want the membership refusal for %q", typ, bad, err, "Pilih "+typ)
				}
			}
			// The required arm keeps its own message: a blank answer is
			// "missing", not "not offered".
			err := ValidateFields(regs, nil)
			if err == nil || !strings.Contains(err.Error(), "medan wajib tidak diisi: Pilih "+typ) {
				t.Errorf("%s: blank required answer = %v, want the required refusal", typ, err)
			}
		})
	}
}

// The acceptance direction: a contract that declares NO choices has nothing to
// compare against, so membership must not refuse a legitimate answer. This is
// the shape a registered_fields contract (issue #254) can have, and a guard that
// refused it would break every such form.
func TestChoiceFieldWithoutDeclaredChoicesIsNotMembershipChecked(t *testing.T) {
	regs := []Field{{FieldID: "s1", Type: "select", Label: "Tahap pedas"}}
	if err := ValidateFields(regs, map[string]string{"s1": "apa-apa"}); err != nil {
		t.Errorf("a select that declares no choices refused an answer: %v", err)
	}
}

// Whitespace around a declared value is not a different choice — the emptiness
// check above trims for the same reason, and a browser that submits `" k "`
// (a copy-pasted answer, or a control with padding) still answered `k`.
func TestChoiceFieldTrimsTheAnswerBeforeComparing(t *testing.T) {
	regs := choiceContract(t, "image-choice")
	if err := ValidateFields(regs, map[string]string{"f1": "  b \n"}); err != nil {
		t.Errorf("a padded declared choice was refused: %v", err)
	}
}
