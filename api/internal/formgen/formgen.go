// Package formgen turns a natural-language prompt into a validated JomForm
// form config (issue #256). The LLM (deepseek-v4.1-flash:1017 via the internal
// llm client) drafts a JSON form config; this package parses and validates it
// against the known builder block types so the output is safe to load into
// the builder. The LLM output is treated as untrusted: unknown block types,
// bad field_ids and any price tampering are rejected. Prices for product
// blocks are never taken from the LLM — they resolve from the product DB
// (server authority), matching the #254/#255 field-registration contract.
package formgen

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// BlockType is the union of every block the builder can place (mirrors
// web/src/app/builder/page.tsx). The LLM may only emit these types.
type BlockType string

// ValidBlockTypes is the allowlist of block types the AI builder accepts.
// It matches the builder palette (issue #173) plus the registered-field
// contract types (issue #254). Unknown types are rejected.
//
// Card t_c28b45ea: `file` is NOT in this map and NOT in the builder palette,
// because nothing in this product can serve the block. No renderer can collect
// it (the response page and the payment page both print the merchant's label
// plus a note instead of a picker) and no write path will store it —
// `formfields.UnsupportedBlock` refuses a config carrying `file`, because every
// upload endpoint sits behind an authenticated account and the public mux has no
// upload route, so an anonymous buyer has no way to deposit bytes. A generator
// that still offered the type would hand an MCP agent a block that is refused
// two calls later, at `upsert_form`: the same third-surface defect `coupon` had
// (card t_4975927e, formgen/coupon_withdrawn_test.go). A legacy stored form that
// already carries a `file` block is untouched by this — it still renders its
// label plus the note on both public renderers.
var ValidBlockTypes = map[BlockType]bool{
	// Standard
	"text": true, "textarea": true, "number": true, "select": true,
	"radio": true, "checkbox": true, "name": true, "email": true,
	"phone": true, "date": true, "time": true, "website": true,
	"section": true, "html": true,
	// Advanced
	"address": true, "consent": true, "hidden": true,
	"list": true, "repeater": true, "image-choice": true, "captcha": true,
	// Pricing
	"product": true, "quantity": true, "option": true, "shipping": true,
	"total": true,
	// Legacy / decorative
	"image": true, "divider": true,
}

// fieldIDUnsafe matches any character that is not safe in a block id.
var fieldIDUnsafe = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// Choice is one option of a select/radio/checkbox/image-choice/option/shipping
// block. price_sen is only meaningful for pricing blocks (option/shipping);
// product prices never come from the LLM.
//
// Card t_3d11f98d: price_sen is a POINTER because "no price was given" and "the
// price is 0 (free)" are different answers for a pricing block — the first is a
// config that cannot say what it charges and is refused, the second is a
// deliberate free choice (the builder writes `price_sen: 0` on its default
// choice). With a plain int64 both decoded to 0 and the check could not run.
type Choice struct {
	Label    string `json:"label"`
	Value    string `json:"value"`
	PriceSen *int64 `json:"price_sen,omitempty"`
}

// Block is one field/block in the generated form config. It mirrors the
// builder Block shape and the render_blocks schema (snake_case JSON).
type Block struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Value     string `json:"value,omitempty"`
	Src       string `json:"src,omitempty"`
	ProductID string `json:"product_id,omitempty"`
	Required  bool   `json:"required,omitempty"`
	// Issue #173: per-field settings.
	Placeholder  string   `json:"placeholder,omitempty"`
	Description  string   `json:"description,omitempty"`
	CSSClass     string   `json:"css_class,omitempty"`
	MaxLength    int      `json:"max_length,omitempty"`
	DefaultValue string   `json:"default_value,omitempty"`
	Min          string   `json:"min,omitempty"`
	Max          string   `json:"max,omitempty"`
	Step         string   `json:"step,omitempty"`
	Format       string   `json:"format,omitempty"`
	Pattern      string   `json:"pattern,omitempty"`
	RequiredMsg  string   `json:"required_message,omitempty"`
	PriceSen     int64    `json:"price_sen,omitempty"`
	Quantity     *bool    `json:"quantity,omitempty"`
	Choices      []Choice `json:"choices,omitempty"`
}

// Theme is the optional custom styling/branding the LLM may suggest. It maps
// to the form's Theme map (issue #254). Layout/spacing are allowlisted.
type Theme struct {
	Primary          string `json:"primary,omitempty"`
	Background       string `json:"background,omitempty"`
	Text             string `json:"text,omitempty"`
	CardBackground   string `json:"card_background,omitempty"`
	CardBorderRadius string `json:"card_border_radius,omitempty"`
	FontFamily       string `json:"font_family,omitempty"`
	LogoURL          string `json:"logo_url,omitempty"`
	Layout           string `json:"layout,omitempty"`
	Spacing          string `json:"spacing,omitempty"`
	CustomCSS        string `json:"custom_css,omitempty"`
	// Issue #388: power-user custom JS (owner-gated + scanned at save).
	CustomJS string `json:"custom_js,omitempty"`
}

// Config is the generated form config returned to the builder for review.
// The user reviews/edits it before saving as a new form.
type Config struct {
	Title  string  `json:"title"`
	Blocks []Block `json:"blocks"`
	Theme  *Theme  `json:"theme,omitempty"`
}

// Parse validates raw LLM output (a JSON form config) against the known
// block types and returns a normalized Config. The LLM output is untrusted:
// unknown block types, bad ids and any price tampering are rejected. Product
// blocks are stripped of any LLM-supplied price (server authority).
func Parse(raw string) (*Config, error) {
	raw = strings.TrimSpace(raw)
	// The model sometimes wraps JSON in a code fence — strip it.
	raw = stripFence(raw)
	if raw == "" {
		return nil, fmt.Errorf("penjanaan kosong")
	}
	var cfg Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("output LLM bukan JSON sah: %w", err)
	}
	cfg.Title = strings.TrimSpace(cfg.Title)
	if cfg.Title == "" {
		return nil, fmt.Errorf("title wajib dalam output LLM")
	}
	if len(cfg.Blocks) == 0 {
		return nil, fmt.Errorf("tiada blok dalam output LLM")
	}
	seen := map[string]bool{}
	for i := range cfg.Blocks {
		b := &cfg.Blocks[i]
		b.ID = strings.TrimSpace(b.ID)
		b.Type = strings.TrimSpace(b.Type)
		b.Value = strings.TrimSpace(b.Value)
		if b.ID == "" {
			return nil, fmt.Errorf("blok #%d: id wajib", i+1)
		}
		if fieldIDUnsafe.MatchString(b.ID) {
			return nil, fmt.Errorf("blok #%d: id tidak sah (hanya a-z, 0-9, _ , -)", i+1)
		}
		if seen[b.ID] {
			return nil, fmt.Errorf("blok #%d: id %q didaftar dua kali", i+1, b.ID)
		}
		seen[b.ID] = true
		if b.Type == "" {
			return nil, fmt.Errorf("blok #%d: type wajib", i+1)
		}
		if !ValidBlockTypes[BlockType(b.Type)] {
			return nil, fmt.Errorf("blok #%d: type %q tidak sah", i+1, b.Type)
		}
		if b.Value == "" {
			b.Value = defaultLabel(b.Type)
		}
		// Product prices never come from the LLM — the server resolves them
		// from the product DB. Strip any LLM-supplied price.
		if b.Type == "product" {
			b.PriceSen = 0
		}
		// Normalize choices: value defaults to label when absent.
		for ci := range b.Choices {
			c := &b.Choices[ci]
			c.Label = strings.TrimSpace(c.Label)
			if c.Label == "" {
				return nil, fmt.Errorf("blok #%d: pilihan tanpa label", i+1)
			}
			if c.Value == "" {
				c.Value = c.Label
			}
		}
		// Card t_3d11f98d: a pricing block's price lives on the CHOICE the buyer
		// picks — that is what the renderer prints and what /order charges
		// (httpapi/lookupProduct). The model used to be asked for choices with
		// no price at all, so "extra sambal RM5" came back as an add-on the page
		// showed free and the sale recorded at 0 sen.
		//
		// A block-level price the model did supply is pushed down onto every
		// choice that has none (the same money the merchant asked for, now in
		// the place the page and the submit path read it from), and the
		// block-level field is cleared: two levels for one price is the
		// ambiguity formfields.UnpricedPricingBlock refuses at save time.
		if b.Type == "option" || b.Type == "shipping" {
			if len(b.Choices) == 0 {
				return nil, fmt.Errorf("blok #%d (%s): beri sekurang-kurangnya satu pilihan berharga", i+1, b.Type)
			}
			for ci := range b.Choices {
				if b.Choices[ci].PriceSen == nil && b.PriceSen != 0 {
					p := b.PriceSen
					b.Choices[ci].PriceSen = &p
				}
				if b.Choices[ci].PriceSen == nil {
					return nil, fmt.Errorf("blok #%d (%s): pilihan %q tiada price_sen — letak harga dalam sen (0 untuk percuma)", i+1, b.Type, b.Choices[ci].Label)
				}
			}
			b.PriceSen = 0
		}
	}
	// Validate theme allowlists.
	if cfg.Theme != nil {
		if cfg.Theme.Layout != "" && cfg.Theme.Layout != "centered" && cfg.Theme.Layout != "full_width" && cfg.Theme.Layout != "split" {
			return nil, fmt.Errorf("layout mesti centered|full_width|split")
		}
		if cfg.Theme.Spacing != "" && cfg.Theme.Spacing != "compact" && cfg.Theme.Spacing != "comfortable" && cfg.Theme.Spacing != "spacious" {
			return nil, fmt.Errorf("spacing mesti compact|comfortable|spacious")
		}
	}
	return &cfg, nil
}

// defaultLabel returns a sensible default label for a block type when the LLM
// omitted one.
func defaultLabel(t string) string {
	switch t {
	case "name":
		return "Full name"
	case "email":
		return "Email"
	case "phone":
		return "Phone number"
	case "textarea":
		return "Message"
	case "select":
		return "Select"
	case "radio":
		return "Choose one"
	case "checkbox":
		return "Checkbox"
	case "date":
		return "Date"
	case "time":
		return "Time"
	case "number":
		return "Number"
	case "website":
		return "Website"
	case "address":
		return "Address"
	case "consent":
		return "I agree"
	case "section":
		return "Section"
	case "product":
		return "Product"
	case "quantity":
		return "Quantity"
	case "option":
		return "Option"
	case "shipping":
		return "Shipping"
	case "total":
		return "Total"
	case "captcha":
		return "Captcha"
	default:
		return strings.Title(t)
	}
}

// stripFence removes a ```json ... ``` code fence if present.
func stripFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
		if i := strings.LastIndex(s, "```"); i >= 0 {
			s = s[:i]
		}
	}
	return strings.TrimSpace(s)
}

// Prompt builds the structured prompt that asks the LLM to draft a form
// config for the merchant's natural-language request. The prompt constrains
// the model to the known block types and the JSON shape Parse expects.
func Prompt(userPrompt string) string {
	var sb strings.Builder
	sb.WriteString("You are a form-builder assistant for JomForm. Turn the user's request into a JSON form config.\n")
	sb.WriteString("Reply with ONLY a JSON object (no markdown, no preamble) matching this shape:\n")
	sb.WriteString(`{"title": "<form title>", "blocks": [{"id": "<unique a-z0-9_- id>", "type": "<block type>", "value": "<field label>", "required": true|false, "placeholder": "<optional>", "choices": [{"label": "...", "value": "...", "price_sen": <integer sen, option/shipping only>}]}], "theme": {"primary": "#hex", "background": "#hex", "text": "#hex", "card_background": "#hex", "card_border_radius": "16px", "font_family": "system-ui, sans-serif", "layout": "centered|full_width|split", "spacing": "compact|comfortable|spacious"}}` + "\n")
	sb.WriteString("Allowed block types (use only these): text, textarea, number, select, radio, checkbox, name, email, phone, date, time, website, section, html, address, consent, hidden, list, repeater, image-choice, captcha, product, quantity, option, shipping, total, image, divider.\n")
	sb.WriteString("Rules:\n")
	sb.WriteString("- Each block needs a unique id (a-z, 0-9, _ , -) and a human label in `value`.\n")
	sb.WriteString("- For select/radio/checkbox/image-choice/option/shipping blocks, provide `choices` (label + value). An option/shipping choice's price is MONEY THE BUYER PAYS: put `price_sen` (integer sen — RM5.00 is 500, free is 0) on EVERY choice, taken from the user's request. Never emit an option/shipping block whose choices have no price.\n")
	sb.WriteString("- Mark name/email/phone and any obviously-required field as required: true.\n")
	sb.WriteString("- Do NOT invent prices. For product blocks, omit price entirely (the server resolves it).\n")
	sb.WriteString("- The theme is optional; if the user mentions a colour/theme, suggest matching hex values.\n")
	sb.WriteString("- Keep the form focused on what the user asked for. 3-10 blocks is typical.\n\n")
	sb.WriteString("User request: " + userPrompt + "\n")
	return sb.String()
}
