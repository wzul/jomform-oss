// MCP tools: MCP-driven form creation (issue #254). A merchant's AI agent can
// build a form with creative styling freedom (CSS, theme, branding, layout)
// via set_form_theme, and register each input field via a strict contract via
// register_form_fields. JomForm keeps authority over field types, pricing
// (lookupProduct) and the payment flow — the agent controls look-and-feel and
// field registration, never prices or payment logic.
package mcpsrv

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/formfields"
)

// ── set_form_theme (issue #254) ────────────────────────────────────────────
//
// The agent sets custom styling/branding freely. JomForm stores it in the
// form's Theme map and the renderer applies it while keeping the JomForm-owned
// structure (inputs, submit, payment). Prices and the payment flow are never
// part of the theme — the server remains the price authority.

// setFormThemeIn is the input for set_form_theme.
type setFormThemeIn struct {
	FormID string `json:"form_id" jsonschema:"ID borang"`
	// Theme keys are all optional; omitted keys keep their current value.
	// Pass an explicit empty string to clear a key.
	Primary          string `json:"primary,omitempty" jsonschema:"Warna utama (butang/aksen), cth #5e6ad2"`
	Background       string `json:"background,omitempty" jsonschema:"Warna latar halaman, cth #f7f8f8"`
	Text             string `json:"text,omitempty" jsonschema:"Warna teks, cth #17181c"`
	CardBackground   string `json:"card_background,omitempty" jsonschema:"Warna latar kad borang, cth #ffffff"`
	CardBorderRadius string `json:"card_border_radius,omitempty" jsonschema:"Jejari sudut kad, cth 16px"`
	FontFamily       string `json:"font_family,omitempty" jsonschema:"Stack fon, cth 'Inter, system-ui, sans-serif'"`
	LogoURL          string `json:"logo_url,omitempty" jsonschema:"URL logo (branding)"`
	Layout           string `json:"layout,omitempty" jsonschema:"Susun atur: centered (lalai) | full_width | split"`
	Spacing          string `json:"spacing,omitempty" jsonschema:"Jarak: compact | comfortable (lalai) | spacious"`
	// CustomCSS is arbitrary CSS injected into the rendered page. It is
	// HTML-escaped so a </style> cannot break out of the style block. The
	// agent controls look-and-feel only — never prices or payment logic.
	CustomCSS string `json:"custom_css,omitempty" jsonschema:"CSS tersuai (bebas) — guna untuk gaya/branding/layout"`
}

// setFormTheme stores a form's custom styling/theme (issue #254). The theme
// is applied by the renderer while JomForm keeps authority over field types,
// prices and the payment flow.
func (s *Server) setFormTheme(
	ctx context.Context, _ *mcp.CallToolRequest, in setFormThemeIn,
) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.requireWriteRole(ctx)
	if err != nil {
		return nil, nil, err
	}
	if in.FormID == "" {
		return nil, nil, errors.New("form_id wajib")
	}
	f, err := s.deps.Forms.GetByID(ctx, acc, in.FormID)
	if err != nil {
		return nil, nil, errors.New("borang tidak dijumpai")
	}
	theme := orEmptyMap(f.Theme)
	// Layout/spacing are validated against a small allowlist so the renderer
	// can map them to known CSS without trusting arbitrary strings.
	if in.Layout != "" {
		if in.Layout != "centered" && in.Layout != "full_width" && in.Layout != "split" {
			return nil, nil, errors.New("layout mesti centered|full_width|split")
		}
		theme["layout"] = in.Layout
	}
	if in.Spacing != "" {
		if in.Spacing != "compact" && in.Spacing != "comfortable" && in.Spacing != "spacious" {
			return nil, nil, errors.New("spacing mesti compact|comfortable|spacious")
		}
		theme["spacing"] = in.Spacing
	}
	setThemeStr(theme, "primary", in.Primary)
	setThemeStr(theme, "background", in.Background)
	setThemeStr(theme, "text", in.Text)
	setThemeStr(theme, "card_background", in.CardBackground)
	setThemeStr(theme, "card_border_radius", in.CardBorderRadius)
	setThemeStr(theme, "font_family", in.FontFamily)
	setThemeStr(theme, "logo_url", in.LogoURL)
	setThemeStr(theme, "custom_css", in.CustomCSS)
	if err := s.deps.Forms.Update(ctx, acc, in.FormID, f.Title, nil, theme); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "ok", "form_id": in.FormID}, nil
}

// setThemeStr sets a theme key, or clears it when the value is empty.
func setThemeStr(theme map[string]any, key, val string) {
	if val == "" {
		delete(theme, key)
		return
	}
	theme[key] = val
}

// ── register_form_fields (issue #254) ─────────────────────────────────────
//
// The agent registers each input field via a strict contract. JomForm
// validates the contract, renders the inputs, and re-validates all fields
// server-side on submit (agent JS is client-side only, bypassable).

// registerFormFieldsIn is the input for register_form_fields.
type registerFormFieldsIn struct {
	FormID string              `json:"form_id" jsonschema:"ID borang"`
	Fields []formfields.Field  `json:"fields" jsonschema:"Senarai medan input berdaftar: {field_id, type, label, required, validation?, product_id?}. Ganti semua medan sedia ada."`
}

// registerFormFields validates and stores a form's registered input fields
// (issue #254). The contract is strict: field_id, type and label are
// required; type must be a known input type; product_id is optional (for
// pricing fields, the price still resolves from the product DB server-side).
func (s *Server) registerFormFields(
	ctx context.Context, _ *mcp.CallToolRequest, in registerFormFieldsIn,
) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.requireWriteRole(ctx)
	if err != nil {
		return nil, nil, err
	}
	if in.FormID == "" {
		return nil, nil, errors.New("form_id wajib")
	}
	f, err := s.deps.Forms.GetByID(ctx, acc, in.FormID)
	if err != nil {
		return nil, nil, errors.New("borang tidak dijumpai")
	}
	// Validate the contract before persisting anything.
	if err := formfields.ValidateContract(in.Fields); err != nil {
		return nil, nil, err
	}
	cfg := orEmptyMap(f.Config)
	cfg["registered_fields"] = in.Fields
	if err := s.deps.Forms.Update(ctx, acc, in.FormID, f.Title, cfg, nil); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "ok", "form_id": in.FormID, "fields": fmt.Sprintf("%d", len(in.Fields))}, nil
}
