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

// Field is the strict field-registration contract (issue #254). JomForm
// validates it and renders the inputs; the agent's HTML must match the
// registered FieldID or the submission won't record correctly.
type Field struct {
	FieldID    string `json:"field_id"`
	Type       string `json:"type"`
	Label      string `json:"label"`
	Required   bool   `json:"required"`
	Validation any    `json:"validation,omitempty"` // string pattern or object
	ProductID  string `json:"product_id,omitempty"`
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

// Validate re-validates submitted field values against the form's registered
// field contract (issue #254). Agent JS is client-side only and bypassable, so
// the server re-validates every field. Returns an error describing the first
// violation. A form with no registered contract is not enforced.
func Validate(config map[string]any, fields map[string]string) error {
	regs := FromConfig(config)
	if len(regs) == 0 {
		return nil // no contract registered — nothing to enforce
	}
	for _, r := range regs {
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
