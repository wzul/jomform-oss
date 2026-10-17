// Builder conditional logic: show/hide blocks based on earlier answers.
//
// The rule itself lives in formfields (Condition.Met) — ONE evaluator for the
// three places that must agree about whether a block was visible: the public
// renderer (which emits the block's data-condition attribute), the browser
// script that hides the block, and the submit path (the contract's `required`
// check plus the answers that are stored). Before card t_9e875bca the attribute
// was never emitted and the script had nothing to act on, so a merchant's
// conditional logic did nothing on any public form.
package httpapi

import (
	"encoding/json"

	"github.com/wzul/jomform/api/internal/formfields"
)

// Condition describes when a block should be VISIBLE in the public form.
//
// It is an ALIAS of formfields.Condition, not a copy: formfields owns the rule
// (Met) because that is the package the submit path validates against, and the
// CHIP-feed router and the confirmation picker share it from here.
type Condition = formfields.Condition

// conditionMet evaluates one condition against submitted answers.
// answers maps block-id → submitted string; nums maps block-id → numeric.
// A nil condition matches (the default CHIP feed, the default confirmation).
func conditionMet(cond *Condition, answers map[string]string, nums map[string]int64) bool {
	return cond.Met(answers, nums)
}

// marshalCondition serializes a condition for data-condition attributes.
func marshalCondition(c *Condition) string {
	if c == nil {
		return ""
	}
	b, _ := json.Marshal(c)
	return string(b)
}
