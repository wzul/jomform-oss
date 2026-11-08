package formfields

// Card t_9e875bca — the builder's conditional logic. The rule lives in this
// package because this is where the submit path re-evaluates it (ValidateFields
// and VisibleAnswers), while the public renderers emit the same rule as the
// block's data-condition attribute and the browser script mirrors
// Condition.Met op for op. Before this card the rule existed in the renderer
// only: no page carried the attribute, and a form whose required field was
// hidden rejected every submission.

import (
	"strings"
	"testing"
)

// TestConditionMet pins the one evaluator: the same table the browser script
// mirrors, so a page and the server cannot disagree about a block's visibility.
func TestConditionMet(t *testing.T) {
	cases := []struct {
		name    string
		cond    *Condition
		answers map[string]string
		want    bool
	}{
		{"nil condition always matches", nil, map[string]string{}, true},
		{"unknown op matches (never hides a field)", &Condition{FieldID: "a", Op: "?"}, map[string]string{"a": "x"}, true},
		{"empty op matches", &Condition{FieldID: "a"}, map[string]string{"a": "x"}, true},

		{"equals, same value", &Condition{FieldID: "a", Op: "equals", Value: "Ali"}, map[string]string{"a": "Ali"}, true},
		{"equals is case-insensitive", &Condition{FieldID: "a", Op: "equals", Value: "ali"}, map[string]string{"a": "ALI"}, true},
		{"equals trims the answer", &Condition{FieldID: "a", Op: "equals", Value: "Ali"}, map[string]string{"a": " Ali "}, true},
		{"equals, different value", &Condition{FieldID: "a", Op: "equals", Value: "Ali"}, map[string]string{"a": "Abu"}, false},
		{"equals with no value means filled", &Condition{FieldID: "a", Op: "equals", Value: ""}, map[string]string{"a": "x"}, true},
		{"equals with no value and a blank answer", &Condition{FieldID: "a", Op: "equals", Value: ""}, map[string]string{"a": "  "}, false},

		{"not_equals", &Condition{FieldID: "a", Op: "not_equals", Value: "Ali"}, map[string]string{"a": "Abu"}, true},
		{"not_equals, same value", &Condition{FieldID: "a", Op: "not_equals", Value: "Ali"}, map[string]string{"a": "ali"}, false},
		{"not_equals with no value means blank", &Condition{FieldID: "a", Op: "not_equals", Value: ""}, map[string]string{"a": ""}, true},

		{"contains", &Condition{FieldID: "a", Op: "contains", Value: "li"}, map[string]string{"a": "Ali"}, true},
		{"contains is case-insensitive", &Condition{FieldID: "a", Op: "contains", Value: "LI"}, map[string]string{"a": "ali"}, true},
		{"contains, absent", &Condition{FieldID: "a", Op: "contains", Value: "zz"}, map[string]string{"a": "Ali"}, false},

		{"gt", &Condition{FieldID: "q", Op: "gt", Value: "2"}, map[string]string{"q": "3"}, true},
		{"gt, equal is not greater", &Condition{FieldID: "q", Op: "gt", Value: "3"}, map[string]string{"q": "3"}, false},
		{"lt", &Condition{FieldID: "q", Op: "lt", Value: "3"}, map[string]string{"q": "2"}, true},
		{"not_empty", &Condition{FieldID: "a", Op: "not_empty"}, map[string]string{"a": "x"}, true},
		{"not_empty, blank", &Condition{FieldID: "a", Op: "not_empty"}, map[string]string{"a": " "}, false},
		{"a missing answer is empty", &Condition{FieldID: "a", Op: "not_empty"}, map[string]string{}, false},
	}
	for _, c := range cases {
		if got := c.cond.Met(c.answers, intAnswers(c.answers)); got != c.want {
			t.Errorf("%s: Met = %v, want %v", c.name, got, c.want)
		}
	}
}

// conditionalConfig is a stored form config: a plain name block and a required
// email shown only while that name block has an answer. The builder offers the
// conditional-logic panel for exactly these collectable types (fieldTypes in
// web/src/app/builder/page.tsx), so the contract below covers every block a
// rule can be attached to.
func conditionalConfig() map[string]any {
	return map[string]any{
		"blocks": []any{
			map[string]any{"id": "t1", "type": "name", "value": "Nama"},
			map[string]any{
				"id": "t2", "type": "email", "value": "Emel", "required": true,
				"condition": map[string]any{"field_id": "t1", "op": "not_empty", "value": ""},
			},
		},
	}
}

// The contract carries each block's rule, which is what makes the two functions
// below possible at all.
func TestContractCarriesBlockConditions(t *testing.T) {
	regs := Contract(conditionalConfig())
	if len(regs) != 2 {
		t.Fatalf("Contract = %+v, want 2 fields", regs)
	}
	if regs[0].Condition != nil {
		t.Errorf("the unconditional block carries a condition: %+v", regs[0].Condition)
	}
	c := regs[1].Condition
	if c == nil || c.FieldID != "t1" || c.Op != "not_empty" || c.Value != "" {
		t.Errorf("the conditional block's rule is missing or wrong: %+v", c)
	}
}

// A required field whose rule is false for THIS submission is not required — and
// the moment the rule holds it is enforced again (the neutralising arm).
func TestValidateFieldsSkipsAHiddenField(t *testing.T) {
	regs := Contract(conditionalConfig())

	// t1 blank → t2 is hidden → the submission is accepted without it.
	if err := ValidateFields(regs, map[string]string{"t1": ""}); err != nil {
		t.Errorf("a hidden required field was enforced: %v", err)
	}
	// t1 filled → t2 is visible → required again, naming the merchant's label.
	err := ValidateFields(regs, map[string]string{"t1": "Ali"})
	if err == nil || !strings.Contains(err.Error(), "Emel is required") {
		t.Errorf("a visible required field was not enforced: %v", err)
	}
	// Filled → accepted, and the value is validated as before the change.
	if err := ValidateFields(regs, map[string]string{"t1": "Ali", "t2": "ali@example.test"}); err != nil {
		t.Errorf("a valid visible submission was rejected: %v", err)
	}
	if err := ValidateFields(regs, map[string]string{"t1": "Ali", "t2": "not-an-email"}); err == nil {
		t.Error("the email check stopped running for a visible field")
	}
	// A hidden field's value is not type-checked either: the buyer never typed
	// it, and VisibleAnswers drops it before it is stored.
	if err := ValidateFields(regs, map[string]string{"t1": "", "t2": "not-an-email"}); err != nil {
		t.Errorf("a hidden field's value was type-checked: %v", err)
	}
}

// VisibleAnswers is the rule applied to what is RECORDED: a hidden block's
// answer is dropped, a visible one is kept, and the input map is not mutated.
func TestVisibleAnswersDropsHiddenFields(t *testing.T) {
	regs := Contract(conditionalConfig())

	answers := map[string]string{"t1": "", "t2": "sneak@example.test", "extra": "kept"}
	visible, hidden := VisibleAnswers(regs, answers)
	if _, ok := visible["t2"]; ok {
		t.Errorf("the hidden block's answer survived: %v", visible)
	}
	if !hidden["t2"] || len(hidden) != 1 {
		t.Errorf("hidden = %v, want exactly t2", hidden)
	}
	if visible["t1"] != "" || visible["extra"] != "kept" {
		t.Errorf("visible answers were mangled: %v", visible)
	}
	if answers["t2"] != "sneak@example.test" {
		t.Errorf("VisibleAnswers mutated its input: %v", answers)
	}

	// The rule holds → nothing is hidden and every answer is kept.
	visible, hidden = VisibleAnswers(regs, map[string]string{"t1": "Ali", "t2": "ali@example.test"})
	if len(hidden) != 0 {
		t.Errorf("hidden = %v, want none", hidden)
	}
	if visible["t2"] != "ali@example.test" {
		t.Errorf("a visible answer was dropped: %v", visible)
	}
}
