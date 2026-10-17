// Package formfields holds the strict field-registration contract for
// MCP-driven form creation (issue #254). A merchant's AI agent registers each
// input field via MCP; JomForm validates the contract, renders the inputs, and
// re-validates all fields server-side on submit (agent JS is client-side only
// and bypassable). The contract lives here so both the MCP layer (mcpsrv) and
// the public submit handler (httpapi) share one definition — no drift.
package formfields

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/wzul/jomform/api/internal/auth"
)

// Choice is one option of a select/radio/checkbox/image-choice field. It is
// only populated when a contract is derived from a form's builder blocks
// (Contract) so the menu checkout can render the merchant's own choices;
// registered_fields may also carry choices.
type Choice struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Field is the strict field-registration contract (issue #254). JomForm
// validates it and renders the inputs; the agent's HTML must match the
// registered FieldID or the submission won't record correctly.
//
// Issue #493: Placeholder / DefaultValue / Choices are optional render hints
// carried through from the form's builder blocks so the restaurant-menu
// checkout can render an attached form's fields with the merchant's own
// labels, placeholders and choices. They never affect validation.
type Field struct {
	FieldID    string `json:"field_id"`
	Type       string `json:"type"`
	Label      string `json:"label"`
	Required   bool   `json:"required"`
	Validation any    `json:"validation,omitempty"` // string pattern or object
	ProductID  string `json:"product_id,omitempty"`
	// Issue #493: render hints (menu checkout extra fields).
	Placeholder  string   `json:"placeholder,omitempty"`
	DefaultValue string   `json:"default_value,omitempty"`
	Choices      []Choice `json:"choices,omitempty"`
	// Condition is the block's conditional-logic rule, when the contract was
	// derived from builder blocks (Contract). A field whose condition is false
	// for a submission is NOT part of it: not required, not validated, and its
	// answer is dropped (VisibleAnswers) — the buyer never saw the field.
	Condition *Condition `json:"condition,omitempty"`
}

// Condition is a block's conditional-logic rule (the builder's "conditional
// logic" panel): the block is shown only while the answer to FieldID satisfies
// Op. It is ONE definition, shared by the public renderer (which emits it as
// the block's `data-condition` attribute), the browser script that hides the
// block, and this package's submit-side re-evaluation — so the page and the
// server cannot disagree about whether a block was visible.
type Condition struct {
	FieldID string `json:"field_id"`
	Op      string `json:"op"` // equals | not_equals | contains | gt | lt | not_empty
	Value   string `json:"value"`
}

// Met reports whether the condition holds for the given answers. answers maps
// field id → submitted value; nums maps field id → that value's numeric form
// for gt/lt (a nil map is allowed and reads as 0 — that is what the callers
// without numbers, e.g. the CHIP feed router, have always passed).
//
// An unknown (or empty) Op matches: a rule the server cannot parse must never
// hide a field the buyer could see.
func (c *Condition) Met(answers map[string]string, nums map[string]int64) bool {
	if c == nil {
		return true
	}
	v := strings.TrimSpace(answers[c.FieldID])
	switch c.Op {
	case "equals":
		if c.Value == "" {
			return v != ""
		}
		return strings.EqualFold(v, strings.TrimSpace(c.Value))
	case "not_equals":
		cc := *c
		cc.Op = "equals"
		return !cc.Met(answers, nums)
	case "contains":
		return strings.Contains(strings.ToLower(v), strings.ToLower(strings.TrimSpace(c.Value)))
	case "gt":
		return nums[c.FieldID] > parseNum(c.Value)
	case "lt":
		return nums[c.FieldID] < parseNum(c.Value)
	case "not_empty":
		return v != ""
	}
	return true
}

// parseNum reads the numeric side of a gt/lt condition. A value that is not an
// integer reads as 0, which is the historical behaviour of the comparison.
func parseNum(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

// intAnswers is the numeric view of the answers that gt/lt compare against.
func intAnswers(answers map[string]string) map[string]int64 {
	nums := make(map[string]int64, len(answers))
	for k, v := range answers {
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			nums[k] = n
		}
	}
	return nums
}

// ValidTypes is the allowlist of input field types JomForm renders and
// records. The agent cannot invent a type — JomForm keeps authority over
// field types (issue #254).
var ValidTypes = map[string]bool{
	"name": true, "email": true, "phone": true, "textarea": true,
	"number": true, "select": true, "radio": true, "checkbox": true,
	"date": true, "time": true, "website": true, "address": true,
	"consent": true, "hidden": true, "list": true, "repeater": true,
	"image-choice": true, "captcha": true,
}

// fieldIDUnsafe matches any character that is not safe in a field_id.
var fieldIDUnsafe = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// ValidateContract validates a field-registration contract. Returns an error
// describing the first violation. field_id, type and label are required; type
// must be a known input type; field_ids must be unique and URL-safe.
func ValidateContract(fields []Field) error {
	seen := map[string]bool{}
	for i := range fields {
		fl := &fields[i]
		fl.FieldID = strings.TrimSpace(fl.FieldID)
		fl.Label = strings.TrimSpace(fl.Label)
		if fl.FieldID == "" {
			return fmt.Errorf("medan #%d: field_id wajib", i+1)
		}
		if fieldIDUnsafe.MatchString(fl.FieldID) {
			return fmt.Errorf("medan #%d: field_id tidak sah (hanya a-z, 0-9, _ , -)", i+1)
		}
		if seen[fl.FieldID] {
			return fmt.Errorf("medan #%d: field_id %q didaftar dua kali", i+1, fl.FieldID)
		}
		seen[fl.FieldID] = true
		if fl.Type == "" {
			return fmt.Errorf("medan #%d: type wajib", i+1)
		}
		if !ValidTypes[fl.Type] {
			return fmt.Errorf("medan #%d: type %q tidak sah", i+1, fl.Type)
		}
		if fl.Label == "" {
			return fmt.Errorf("medan #%d: label wajib", i+1)
		}
	}
	return nil
}

// FromConfig reads a form's registered input fields (issue #254) from its
// config JSON. Returns nil when none are registered.
func FromConfig(config map[string]any) []Field {
	if config == nil {
		return nil
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return nil
	}
	var cfg struct {
		RegisteredFields []Field `json:"registered_fields"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return nil
	}
	return cfg.RegisteredFields
}

// Validate re-validates submitted field values against the form's field
// contract (issue #254). Agent JS is client-side only and bypassable, so the
// server re-validates every field. Returns an error describing the first
// violation. A form with no contract is not enforced.
//
// The contract is derived with Contract — i.e. the SAME rule the menu checkout
// validates against: an explicit registered_fields contract (issue #254) wins,
// otherwise it comes from the form's own builder blocks (#493). Deriving from
// the blocks here is the point (card t_589dc006): this used to read
// registered_fields alone, so a form whose required markers live in
// config.blocks[].required — the shape every builder-made form has — had
// NOTHING enforced on its own submit route. A direct POST of {} then stored an
// empty submission and fired the merchant's notification email.
func Validate(config map[string]any, fields map[string]string) error {
	return ValidateFields(Contract(config), fields)
}

// ValidateFields re-validates submitted values against an explicit field
// contract (issue #493). The restaurant-menu checkout validates against the
// contract derived from the attached form's builder blocks, so the same
// enforcement applies without a registered_fields entry.
func ValidateFields(regs []Field, fields map[string]string) error {
	if len(regs) == 0 {
		return nil // no contract registered — nothing to enforce
	}
	nums := intAnswers(fields)
	for _, r := range regs {
		// Card t_9e875bca: a block whose conditional-logic rule is false for
		// THIS submission was hidden on the page the buyer filled in, so its own
		// `required` marker (and any pattern/type check) does not apply — the
		// buyer could not have filled it. Without this, a form with a required
		// conditional field rejected every submission made while the field was
		// hidden. The value itself is dropped by VisibleAnswers before it is
		// stored, so there is nothing left to type-check either.
		if r.Condition != nil && !r.Condition.Met(fields, nums) {
			continue
		}
		val, present := fields[r.FieldID]
		if r.Required {
			if !present || strings.TrimSpace(val) == "" {
				return fmt.Errorf("medan wajib tidak diisi: %s", r.Label)
			}
		}
		if !present || strings.TrimSpace(val) == "" {
			continue
		}
		switch r.Type {
		case "email":
			if !auth.ValidEmail(val) {
				return fmt.Errorf("emel tidak sah: %s", r.Label)
			}
		case "website":
			if !strings.HasPrefix(val, "http://") && !strings.HasPrefix(val, "https://") {
				return fmt.Errorf("URL tidak sah: %s", r.Label)
			}
		case "number":
			if _, err := strconv.ParseFloat(val, 64); err != nil {
				return fmt.Errorf("nombor tidak sah: %s", r.Label)
			}
		}
		// Optional per-field validation pattern (string form).
		if pat, ok := r.Validation.(string); ok && pat != "" {
			re, err := regexp.Compile(pat)
			if err == nil && !re.MatchString(val) {
				return fmt.Errorf("nilai tidak sah: %s", r.Label)
			}
		}
	}
	return nil
}

// collectableTypes is the allowlist of builder-block types that collect a
// value (mirrors the renderer's input set). Decorative/pricing blocks
// (text, image, divider, product, option, shipping, total) are never
// part of a field contract.
var collectableTypes = map[string]bool{
	"name": true, "email": true, "phone": true, "textarea": true,
	"number": true, "select": true, "radio": true, "checkbox": true,
	"date": true, "time": true, "website": true, "address": true,
	"consent": true, "hidden": true, "list": true, "repeater": true,
	"image-choice": true, "captcha": true,
}

// block is the subset of a builder block the contract derivation reads.
type block struct {
	ID           string     `json:"id"`
	Type         string     `json:"type"`
	Value        string     `json:"value"`
	Required     bool       `json:"required"`
	Condition    *Condition `json:"condition"`
	Placeholder  string     `json:"placeholder"`
	DefaultValue string     `json:"default_value"`
	Choices      []struct {
		Label string `json:"label"`
		Value string `json:"value"`
	} `json:"choices"`
}

// Contract derives the input-field contract from a form's builder blocks
// (issue #493). The merchant configures extra menu-checkout fields in the
// normal form builder; the checkout renders exactly those blocks and the
// server validates them with ValidateFields. A form that carries an explicit
// registered_fields contract (issue #254) wins — that contract is the
// merchant/agent's declared shape. Returns nil when the form has neither.
//
// The block id is the field id, matching how the block renderer submits
// ({fields: {block_id: value}}), so a form's own submit path and the menu
// checkout record the same keys.
func Contract(config map[string]any) []Field {
	if regs := FromConfig(config); len(regs) > 0 {
		return regs
	}
	if config == nil {
		return nil
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return nil
	}
	var cfg struct {
		Blocks []block `json:"blocks"`
	}
	if json.Unmarshal(raw, &cfg) != nil || len(cfg.Blocks) == 0 {
		return nil
	}
	var out []Field
	seen := map[string]bool{}
	for _, b := range cfg.Blocks {
		id := strings.TrimSpace(b.ID)
		if id == "" || !collectableTypes[b.Type] || seen[id] {
			continue
		}
		seen[id] = true
		label := strings.TrimSpace(b.Value)
		if label == "" {
			label = id
		}
		f := Field{
			FieldID:      id,
			Type:         b.Type,
			Label:        label,
			Required:     b.Required,
			Placeholder:  b.Placeholder,
			DefaultValue: b.DefaultValue,
			Condition:    b.Condition,
		}
		for _, c := range b.Choices {
			f.Choices = append(f.Choices, Choice{Label: c.Label, Value: c.Value})
		}
		out = append(out, f)
	}
	return out
}

// VisibleAnswers returns the answers to RECORD for a submission: the submitted
// answers minus every field whose conditional-logic rule is false for them.
// The second result is the set of hidden field ids, for a caller that has to
// prune a differently-typed map (the response route carries map[string]any).
//
// The contract is the right unit: the builder offers the conditional-logic panel
// for exactly the collectable types (fieldTypes in web/src/app/builder/page.tsx
// — name, email, phone, textarea, number, select, radio, checkbox, date, time,
// website, address, consent, hidden, list, repeater, image-choice, captcha), and
// those are the contract's types. A hand-written config that attached a rule to
// some other block type would have that rule honoured on the page and ignored
// here — the same asymmetry a rule on a block the renderer drops already has.
//
// Why the server drops the answers at all: the public renderers hide a
// conditional block whose rule is false and disable its controls, so the buyer
// never sees it and its answer never leaves the page. A client is not
// authoritative — a direct POST can name any field — so the same rule is applied
// here to what is stored (and to what the confirmation, notification and
// CHIP-feed logic downstream read). Conditions are evaluated against the
// SUBMITTED answers in one pass, so a field that is itself hidden contributes no
// answer and cannot keep a dependent block visible.
func VisibleAnswers(regs []Field, answers map[string]string) (map[string]string, map[string]bool) {
	visible := make(map[string]string, len(answers))
	for k, v := range answers {
		visible[k] = v
	}
	nums := intAnswers(answers)
	var hidden map[string]bool
	for i := range regs {
		r := &regs[i]
		if r.Condition == nil || r.Condition.Met(answers, nums) {
			continue
		}
		if hidden == nil {
			hidden = make(map[string]bool, 1)
		}
		hidden[r.FieldID] = true
		delete(visible, r.FieldID)
	}
	return visible, hidden
}
