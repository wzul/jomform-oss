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
	"golang.org/x/net/html"

	"github.com/wzul/jomform/api/internal/formfields"
)

// Field is one registered input field of a custom HTML form. The agent's HTML
// inputs must carry the field's ID as their name attribute so JomForm can map
// submitted values back to the contract.
type Field struct {
	ID string `json:"field_id"`
	// Type is the value the merchant's HTML puts in the input's `type`
	// attribute. Supported: text, email, tel, number, date, time, url,
	// password, textarea, select, radio, checkbox, hidden, consent.
	//
	// Server-side enforcement (see ValidationContract) follows the browser:
	// `required` is refused when the value is missing or blank, `email` uses
	// the same address grammar the browser accepts, `number` must parse, and
	// `url` accepts any absolute URL the browser's own `input[type=url]`
	// accepts — `https://…`, `ftp://…`, `mailto:…` — not http(s) only. The one
	// place the server is stricter: a script-bearing scheme (javascript:,
	// data:, vbscript:) is refused, because the answer is stored and read
	// outside the form. See formfields.ValidWebsiteURL for the exact rule.
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
//
// Card t_73a5db6e: it also refuses an UNSATISFIABLE contract — a `required:true`
// field whose controls do not exist in the HTML. The buyer's page can only
// produce a value for an input it can find (the iframe bridge posts
// `doc.querySelectorAll('[name]')`, render_custom.go), so such a form is
// unsubmittable: every POST answers 400 `medan wajib tidak diisi: <label>` and
// the buyer can do nothing about it. Refusing the write is the only place the
// merchant can still act, and it is why the check runs over the SANITIZED HTML
// (what the renderer will actually serve), not the raw body.
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
	if missing := MissingRequiredControls(c); len(missing) > 0 {
		quoted := make([]string, 0, len(missing))
		for _, id := range missing {
			quoted = append(quoted, `name="`+id+`"`)
		}
		return errField("medan wajib tiada dalam HTML (" + strings.Join(quoted, ", ") + ")")
	}
	return nil
}

// ValidateConfig runs the same checks over the custom-form section of a form or
// template config map, when it carries one. Every surface that stores a config
// verbatim (REST form create/update, MCP upsert_form, the template writers whose
// config is later cloned onto a real form) calls this beside its existing
// urlguard/formfields config checks, so no write path can persist an
// unsatisfiable custom-form contract.
//
// A `custom_html` key the package cannot even decode is refused rather than
// skipped: such a config is not renderable as a custom form (FromConfig returns
// nil, Enabled is false) yet it looks configured to whoever stored it.
func ValidateConfig(cfg map[string]any) error {
	if cfg == nil {
		return nil
	}
	if _, ok := cfg[ConfigKey]; !ok {
		return nil
	}
	c := FromConfig(cfg)
	if c == nil {
		return errField("custom_html tidak sah")
	}
	return Validate(c)
}

// MissingRequiredControls returns the registered `required:true` field ids that
// no control in the SANITIZED HTML can supply — i.e. the ones that make the
// form permanently unsubmittable. Empty (or whitespace-only) HTML returns nil:
// that state clears the custom form and falls back to the block renderer.
func MissingRequiredControls(c *Config) []string {
	if c == nil || strings.TrimSpace(c.HTML) == "" {
		return nil
	}
	required := make([]string, 0, len(c.Fields))
	for _, f := range c.Fields {
		if id := strings.TrimSpace(f.ID); f.Required && id != "" {
			required = append(required, id)
		}
	}
	if len(required) == 0 {
		return nil
	}
	names := controlNames(SanitizeHTML(c.HTML))
	missing := make([]string, 0, len(required))
	for _, id := range required {
		if !names[id] {
			missing = append(missing, id)
		}
	}
	return missing
}

// controlNames returns the `name` values of the controls in a sanitized custom
// form body that the iframe bridge can read a value from. The bridge walks
// `doc.querySelectorAll('[name]')` and takes `el.name` / `el.value`, so a name
// attribute only helps when the element has BOTH: input, select and textarea
// reflect it and carry a string value. A `<div name="x">` selects but yields
// `undefined` for both, so the key the server validates against never appears —
// counting it would let exactly the unsatisfiable contract this guard exists to
// refuse through.
//
// A button reflects `name` and `value` too, but its default value is "", so it
// only counts when it carries a non-empty value attribute.
func controlNames(safeHTML string) map[string]bool {
	names := map[string]bool{}
	// srcdoc — what the renderer feeds the sandboxed iframe — is parsed as a
	// document, so parse one here as well rather than a fragment.
	doc, err := html.Parse(strings.NewReader(safeHTML))
	if err != nil {
		return names
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			name, hasName := nodeAttr(n, "name")
			if hasName {
				switch n.Data {
				case "input", "select", "textarea":
					names[name] = true
				case "button":
					if v, ok := nodeAttr(n, "value"); ok && strings.TrimSpace(v) != "" {
						names[name] = true
					}
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return names
}

// nodeAttr returns an element's attribute value by name. Namespaced attributes
// are ignored: the parser stores `xlink:name` etc. under a namespace, and no
// form control's name is set that way.
func nodeAttr(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == key && a.Namespace == "" {
			return a.Val, true
		}
	}
	return "", false
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

// ValidationContract converts a custom-HTML form's registered field contract
// into the shared server-side validation contract (formfields.Field), so
// handleResponseSubmit enforces the same rule the buyer's browser enforces.
//
// Card t_50a3f60d: the public submit path validated against
// formfields.Contract(config), which reads config.registered_fields and then
// the form's builder blocks. A custom-HTML form has no builder blocks, so the
// derived contract was empty and ValidateFields had nothing to check: a direct
// POST that omitted a `required:true` registered field (or sent an empty
// `fields` map, or a non-email into a type=email field) was stored with a 201.
// The registered fields ARE the contract of a custom form — this is where that
// fact is expressed, and it is enforced before the submission row is created.
//
// Only `required` and the input types with a cheap deterministic check (email,
// number, url) are carried: those are the constraints the renderer's HTML
// attributes assert, i.e. the browser's own checks. They are NOT a superset of
// them — the server refuses strictly more in one place, and it is deliberate:
// a `url` value whose scheme executes (javascript:, data:, vbscript:) is
// refused although `<input type=url>` accepts it, because the answer is stored
// and later read outside the form. formfields.ValidWebsiteURL owns that rule.
// The contract's Validation string is deliberately NOT carried as a regex — it
// is a merchant-facing hint, not a pattern the page applies, so enforcing it
// here could refuse an answer the live page accepts.
func ValidationContract(c *Config) []formfields.Field {
	if c == nil || len(c.Fields) == 0 {
		return nil
	}
	out := make([]formfields.Field, 0, len(c.Fields))
	for _, f := range c.Fields {
		id := strings.TrimSpace(f.ID)
		if id == "" {
			continue
		}
		label := strings.TrimSpace(f.Label)
		if label == "" {
			// The message must name something the buyer/merchant can find in
			// the form; the field id is the fallback the renderer uses too.
			label = id
		}
		out = append(out, formfields.Field{
			FieldID:  id,
			Type:     validationType(f.Type),
			Label:    label,
			Required: f.Required,
		})
	}
	return out
}

// validationType maps a custom-form input type to the type name
// formfields.ValidateFields understands. The custom form's own vocabulary
// (validType) is the browser's `type` attribute; formfields uses the builder's
// block names, so `tel` / `url` map onto the block names for the same check and
// anything without a server-side check reads as plain text.
//
// `url` → the builder's `website` check, which is formfields.ValidWebsiteURL:
// the browser's own absolute-URL rule (ftp://, mailto: and a custom scheme with
// a host all pass), not http(s) only. That is the point of the mapping — a
// buyer's `<input type=url>` and the server must agree on what a URL is.
func validationType(t string) string {
	switch t {
	case "email", "number", "date", "time", "textarea", "select", "radio",
		"checkbox", "hidden", "consent", "text":
		return t
	case "tel":
		return "phone"
	case "url":
		return "website"
	}
	// password and any unknown type: required-only, no format check.
	return "text"
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

// styleSanitizer is a bluemonday policy used to sanitize a per-field inline
// style attribute (issue #388). It allows only the same safe CSS properties
// as the customform sanitizer (no url()/background-image, which could trigger
// a remote request), so a power user can style a field without introducing an
// XSS or data-exfiltration vector.
var styleSanitizer = buildStyleSanitizer()

func buildStyleSanitizer() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	// Allow the wrapper element + the style attribute so bluemonday can parse
	// and sanitize the inline style value.
	p.AllowElements("div")
	p.AllowAttrs("style").Globally()
	// Same safe CSS allowlist as the customform sanitizer (customform.go).
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
	return p
}

// SanitizeStyle sanitizes a per-field inline style attribute value (issue
// #388). Only safe CSS properties are kept; url()/background-image and any
// other property that could trigger a remote request or script execution are
// stripped. The result is safe to emit as an element's style attribute.
func SanitizeStyle(style string) string {
	if strings.TrimSpace(style) == "" {
		return ""
	}
	// Wrap in a div so bluemonday can parse the style attribute, then extract
	// the sanitized value back out.
	out := styleSanitizer.Sanitize(`<div style="` + style + `"></div>`)
	// Extract style="..." from the sanitized wrapper.
	start := strings.Index(out, `style="`)
	if start < 0 {
		return ""
	}
	start += len(`style="`)
	end := strings.Index(out[start:], `"`)
	if end < 0 {
		return ""
	}
	return out[start : start+end]
}

// ScanCustomJS runs a focused deterministic scan over a power user's custom
// form JS (issue #388). It returns a non-empty reason when the JS contains an
// active code-execution / obfuscation / data-exfiltration marker that should
// block it from being served on the buyer's page.
//
// Unlike the full assetgate scan (#364) — which is tuned for standalone
// uploaded JS assets and hard-blocks the ubiquitous `function(` token — this
// scan is tuned for form-behaviour JS, where `function(e){...}` event handlers
// are the norm. It blocks only the genuinely dangerous patterns: dynamic code
// execution, document.write, javascript: URIs, obfuscation and external
// script/beacon loads. Legitimate form validation / analytics the owner opted
// into pass.
func ScanCustomJS(js string) string {
	lower := strings.ToLower(js)
	reasons := []string{}
	// Hard block: active code execution the browser would run.
	for _, m := range []string{
		"eval(", "eval (", "new function(", "new function (",
		"document.write(", "document.writeln(", "constructor.constructor",
		"constructor[\"constructor\"]", "constructor['constructor']",
		"javascript:", "expression(", "-moz-binding", "behavior:url",
	} {
		if strings.Contains(lower, m) {
			reasons = append(reasons, "active code execution marker: "+m)
		}
	}
	// Suspicious: obfuscation / data-exfiltration / external script loads.
	for _, m := range []string{
		"atob(", "btoa(", "sendbeacon(", "navigator.sendbeacon(", "xmlhttprequest",
		"data:text/html", "data:text/javascript", "data:text/js",
		"<script src=\"http", "<script src='http", "<script src=\"//",
		"__proto__",
	} {
		if strings.Contains(lower, m) {
			reasons = append(reasons, "obfuscation/beacon marker: "+m)
		}
	}
	// External http(s) URL in custom JS is the data-exfiltration / script-load
	// vector. A form's custom JS should not need to reach an external host
	// (that is what tracking_head is for), so block it.
	if strings.Contains(lower, "http://") || strings.Contains(lower, "https://") {
		reasons = append(reasons, "reference to external URL from custom JS")
	}
	if len(reasons) == 0 {
		return ""
	}
	return strings.Join(reasons, "; ")
}
