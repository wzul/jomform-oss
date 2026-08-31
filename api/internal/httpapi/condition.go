// Builder conditional logic: show/hide blocks based on earlier answers.
// The public renderer evaluates conditions server-side: hidden blocks are
// skipped, their answers excluded, and their prices not counted.
package httpapi

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Condition describes when a block should be VISIBLE in the public form.
type Condition struct {
	FieldID string `json:"field_id"`
	Op      string `json:"op"` // equals | not_equals | contains | gt | lt | not_empty
	Value   string `json:"value"`
}

// conditionMet evaluates one condition against submitted answers.
// answers maps block-id → submitted string; nums maps block-id → numeric.
func conditionMet(cond *Condition, answers map[string]string, nums map[string]int64) bool {
	if cond == nil {
		return true
	}
	v := strings.TrimSpace(answers[cond.FieldID])
	switch cond.Op {
	case "equals":
		if cond.Value == "" {
			return v != ""
		}
		return strings.EqualFold(v, strings.TrimSpace(cond.Value))
	case "not_equals":
		c := *cond
		c.Op = "equals"
		return !conditionMet(&c, answers, nums)
	case "contains":
		return strings.Contains(strings.ToLower(v), strings.ToLower(strings.TrimSpace(cond.Value)))
	case "gt":
		return nums[cond.FieldID] > parseNum(cond.Value)
	case "lt":
		return nums[cond.FieldID] < parseNum(cond.Value)
	case "not_empty":
		return v != ""
	}
	return true
}

func parseNum(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

// marshalCondition serializes a condition for data-condition attributes.
func marshalCondition(c *Condition) string {
	if c == nil {
		return ""
	}
	b, _ := json.Marshal(c)
	return string(b)
}
