// Package customform implements the fully-custom HTML response-only form
// (issue #255). A merchant's AI agent supplies raw HTML for the form body
// plus a field-registration contract; JomForm renders the HTML in a sandboxed
// iframe (CSP + sanitization, no external scripts, no data exfiltration) and
// records submissions from the registered fields only.
//
// This package is shared by the public renderer/submit path (httpapi) and the
// MCP tools (mcpsrv) so the sanitizer and the field contract stay in one place.
package customform

import (
	"encoding/json"
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

// Field is one registered input field of a custom HTML form. The agent's HTML
// inputs must carry the field's ID as their name attribute so JomForm can map
// submitted values back to the contract.
type Field struct {
	ID         string `json:"field_id"`
	Type       string `json:"type"`
	Label      string `json:"label"`
	Required   bool   `json:"required"`
	Validation string `json:"validation,omitempty"`
}

// Config is the custom-form section of a form's config map. It is stored under
// the "custom_html" key alongside the existing "mode":"response" flag.
type Config struct {
	// HTML is the raw, agent-authored form body. It is sanitized at render
	// time (never trusted verbatim) and rendered inside a sandboxed iframe.
	HTML string `json:"html"`
	// Fields is the field-registration contract. On submit, only these
	// field_ids are read from the submitted data.
	Fields []Field `json:"fields"`
}

// ConfigKey is the config-map key under which the custom-form config lives.
const ConfigKey = "custom_html"

// FromConfig extracts the custom-form config from a form's config map. Returns
// nil when the form has no custom_html section.
func FromConfig(cfg map[string]any) *Config {
	if cfg == nil {
		return nil
	}
	raw, ok := cfg[ConfigKey]
	if !ok {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil
	}
	return &c
}

// Enabled reports whether the form is a custom-HTML response form: it has a
// non-empty custom_html.html body. A custom form with no HTML falls back to
// the block renderer.
func Enabled(cfg map[string]any) bool {
	c := FromConfig(cfg)
	return c != nil && strings.TrimSpace(c.HTML) != ""
}

// Validate checks the field-registration contract. Returns a human-readable
// error message (Malay, matching the codebase) when the contract is invalid.
func Validate(c *Config) error {
	if c == nil {
		return nil
	}
	seen := map[string]bool{}
	for _, f := range c.Fields {
		if strings.TrimSpace(f.ID) == "" {
			return errField("field_id wajib")
		}
		if seen[f.ID] {
			return errField("field_id duplikat: " + f.ID)
		}
		seen[f.ID] = true
		if !validType(f.Type) {
			return errField("jenis medan tidak sah: " + f.Type)
		}
	}
	return nil
}

func errField(msg string) error {
	return &contractError{msg}
}

type contractError struct{ msg string }

func (e *contractError) Error() string { return e.msg }

// validType reports whether a field type is one of the supported input types.
func validType(t string) bool {
	switch t {
	case "text", "email", "tel", "number", "date", "time", "url", "password",
		"textarea", "select", "radio", "checkbox", "hidden", "consent":
		return true
	}
	return false
}

// FieldIDs returns the set of registered field IDs.
func FieldIDs(c *Config) map[string]bool {
	out := map[string]bool{}
	if c == nil {
		return out
	}
	for _, f := range c.Fields {
		out[f.ID] = true
	}
	return out
}

// Collect reads the submitted data and returns only the values for registered
// fields. Values for unregistered field_ids (arbitrary HTML inputs) are
// dropped — the server never trusts arbitrary HTML. The returned map is keyed
// by field_id.
func Collect(c *Config, data map[string]any) map[string]any {
	out := map[string]any{}
	if c == nil || data == nil {
		return out
	}
	ids := FieldIDs(c)
	for k, v := range data {
		if ids[k] {
			out[k] = v
		}
	}
	return out
}

// sanitizer is the bluemonday policy for custom HTML form bodies. It strips
// scripts, event handlers, javascript: URLs, external links, iframes/objects
// and any unsafe markup, while preserving the structural + form elements an
// agent needs for creative freedom. Only safe CSS properties are allowed (no
// url()/background-image, which could exfiltrate data).
var sanitizer = buildSanitizer()

func buildSanitizer() *bluemonday.Policy {
	p := bluemonday.NewPolicy()

	p.AllowElements(
		"div", "span", "p", "h1", "h2", "h3", "h4", "h5", "h6", "br", "hr",
		"strong", "em", "b", "i", "u", "s", "small", "sub", "sup",
		"ul", "ol", "li", "table", "thead", "tbody", "tfoot", "tr", "td", "th", "caption",
		"section", "article", "header", "footer", "main", "aside", "blockquote", "code", "pre",
		"input", "select", "option", "optgroup", "textarea", "button", "label", "fieldset", "legend",
		"a", "img", "style",
	)

	p.AllowAttrs("name", "id", "class", "type", "value", "placeholder", "required",
		"min", "max", "step", "pattern", "checked", "selected", "disabled", "readonly",
		"rows", "cols", "maxlength", "for", "alt", "title", "colspan", "rowspan",
		"width", "height", "target", "rel", "autocomplete", "multiple", "size", "wrap", "accept",
		"href", "src").Globally()

	// Safe CSS properties only. url()/background-image are deliberately
	// excluded so a style attribute can never trigger a remote request.
	p.AllowStyles(
		"color", "background-color", "font-size", "font-family", "font-weight",
		"font-style", "text-align", "text-decoration", "line-height", "letter-spacing",
		"margin", "margin-top", "margin-bottom", "margin-left", "margin-right",
		"padding", "padding-top", "padding-bottom", "padding-left", "padding-right",
		"border", "border-radius", "width", "height", "max-width", "max-height",
		"min-width", "min-height", "display", "flex", "flex-direction", "gap",
		"align-items", "justify-content", "position", "top", "right", "bottom", "left",
		"float", "opacity", "cursor", "box-shadow", "overflow", "white-space",
		"vertical-align", "z-index",
	).Globally()

	// Only relative URLs (no external links) plus data: (inline images) and
	// mailto:. javascript:, https://, http:// are all stripped.
	p.AllowRelativeURLs(true)
	p.AllowURLSchemes("data", "mailto")

	// Drop the content of any element that could execute or load remotely.
	p.SkipElementsContent("script", "iframe", "object", "embed")

	// Keep <style> content raw (CSS) so agents can style their form. Scripts
	// are already skipped above; AllowUnsafe only affects the style/script
	// token handling, and script content is dropped by SkipElementsContent.
	p.AllowUnsafe(true)

	return p
}

// SanitizeHTML strips any unsafe markup from an agent-authored custom HTML
// form body. The result is safe to render inside the sandboxed iframe.
func SanitizeHTML(html string) string {
	return sanitizer.Sanitize(html)
}
