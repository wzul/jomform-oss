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

	"github.com/wzul/jomform/api/internal/customform"
	"github.com/wzul/jomform/api/internal/formfields"
	"github.com/wzul/jomform/api/internal/urlguard"
)

// ── set_form_theme (issue #254) ────────────────────────────────────────────
//
// The agent sets custom styling/branding freely. JomForm stores it in the
// form's Theme map and the renderer applies it while keeping the JomForm-owned
// structure (inputs, submit, payment). Prices and the payment flow are never
// part of the theme — the server remains the price authority.

// setFormThemeIn is the input for set_form_theme.
type setFormThemeIn struct {
	FormID string `json:"form_id" jsonschema:"Form ID"`
	// Theme keys are all optional; omitted keys keep their current value.
	// Pass an explicit empty string to clear a key.
	Primary          string `json:"primary,omitempty" jsonschema:"Primary color (buttons/accent), e.g. #5e6ad2"`
	Background       string `json:"background,omitempty" jsonschema:"Page background color, e.g. #f7f8f8"`
	Text             string `json:"text,omitempty" jsonschema:"Text color, e.g. #17181c"`
	CardBackground   string `json:"card_background,omitempty" jsonschema:"Form card background colour, e.g. #ffffff"`
	CardBorderRadius string `json:"card_border_radius,omitempty" jsonschema:"Card corner radius, e.g. 16px"`
	FontFamily       string `json:"font_family,omitempty" jsonschema:"Font stack, e.g. 'Inter, system-ui, sans-serif'"`
	LogoURL          string `json:"logo_url,omitempty" jsonschema:"Logo URL (branding)"`
	Layout           string `json:"layout,omitempty" jsonschema:"Layout: centered (default) | full_width | split"`
	Spacing          string `json:"spacing,omitempty" jsonschema:"Spacing: compact | comfortable (default) | spacious"`
	// Issue #471: product image size + layout on the buyer's form.
	ProductImageSize   string `json:"product_image_size,omitempty" jsonschema:"Product image size: small (52px, default) | medium (96px) | large (160px) | hidden. Use medium/large for restaurant menus; hidden hides the image."`
	ProductImageLayout string `json:"product_image_layout,omitempty" jsonschema:"Product image layout: thumbnail-left (default, image to the left of the name) | image-top (menu card: full image above the name)."`
	// CustomCSS is arbitrary CSS injected into the rendered page. It is
	// HTML-escaped so a </style> cannot break out of the style block. The
	// agent controls look-and-feel only — never prices or payment logic.
	CustomCSS string `json:"custom_css,omitempty" jsonschema:"Custom CSS (free-form) — use for styling/branding/layout"`
	// CustomJS is arbitrary JS injected into the rendered page (issue #388).
	// Owner-gated and scanned with the deterministic code-asset scan (#364) so
	// obviously malicious JS is never served on the buyer's page.
	CustomJS string `json:"custom_js,omitempty" jsonschema:"Custom JS (user power) — use for form behaviour. Owner-controlled + scanned."`
}

// setFormTheme stores a form's custom styling/theme (issue #254). The theme
// is applied by the renderer while JomForm keeps authority over field types,
// prices and the payment flow.
func (s *Server) setFormTheme(
	ctx context.Context, _ *mcp.CallToolRequest, in setFormThemeIn,
) (*mcp.CallToolResult, map[string]string, error) {
	if in.FormID == "" {
		return nil, nil, errors.New("form_id is required")
	}
	// Issue #307: resolve + RBAC-gate the owning account across the user's workspaces.
	acc, err := s.requireFormWrite(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	f, err := s.deps.Forms.GetByID(ctx, acc, in.FormID)
	if err != nil {
		return nil, nil, errors.New("form not found")
	}
	theme := orEmptyMap(f.Theme)
	// Layout/spacing are validated against a small allowlist so the renderer
	// can map them to known CSS without trusting arbitrary strings.
	if in.Layout != "" {
		if in.Layout != "centered" && in.Layout != "full_width" && in.Layout != "split" {
			return nil, nil, errors.New("layout must be centered|full_width|split")
		}
		theme["layout"] = in.Layout
	}
	if in.Spacing != "" {
		if in.Spacing != "compact" && in.Spacing != "comfortable" && in.Spacing != "spacious" {
			return nil, nil, errors.New("spacing must be compact|comfortable|spacious")
		}
		theme["spacing"] = in.Spacing
	}
	// Issue #471: product image size + layout. Validated against the same
	// allowlist the renderer uses (empty = unset → clear the key, restoring the
	// default small / thumbnail-left look).
	if in.ProductImageSize != "" && !validProductImageSize(in.ProductImageSize) {
		return nil, nil, errors.New("product_image_size must be small|medium|large|hidden")
	}
	if in.ProductImageLayout != "" && !validProductImageLayout(in.ProductImageLayout) {
		return nil, nil, errors.New("product_image_layout must be thumbnail-left|image-top")
	}
	setThemeStr(theme, "product_image_size", in.ProductImageSize)
	setThemeStr(theme, "product_image_layout", in.ProductImageLayout)
	setThemeStr(theme, "primary", in.Primary)
	setThemeStr(theme, "background", in.Background)
	setThemeStr(theme, "text", in.Text)
	setThemeStr(theme, "card_background", in.CardBackground)
	setThemeStr(theme, "card_border_radius", in.CardBorderRadius)
	setThemeStr(theme, "font_family", in.FontFamily)
	// Issue #501: the theme's logo_url is rendered into a buyer-facing
	// `<img src>` (render_blocks.go / render_custom.go / menu.go). It is
	// caller input, so a non-https scheme is REJECTED here — never trusted
	// by marking it template.URL, which would let an arbitrary scheme into
	// the buyer's page. ONE shared rule: internal/urlguard.
	// Empty means "clear the key" (as before) and stays allowed.
	if !urlguard.Valid(in.LogoURL) {
		return nil, nil, errors.New(urlguard.InvalidMessage("logo_url"))
	}
	setThemeStr(theme, "logo_url", in.LogoURL)
	setThemeStr(theme, "custom_css", in.CustomCSS)
	// Issue #388: power-user custom JS. Owner-gated (requireFormWrite above)
	// and scanned with the deterministic code-asset scan (#364) so obviously
	// malicious JS is never served on the buyer's page.
	if in.CustomJS != "" {
		if reason := customform.ScanCustomJS(in.CustomJS); reason != "" {
			return nil, nil, errors.New(customform.RejectedMessage(reason))
		}
	}
	setThemeStr(theme, "custom_js", in.CustomJS)
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

// ── product image size + layout (issue #471) ──────────────────────────────
//
// The renderer (httpapi/product_image_issue471.go) owns the canonical
// allowlists; these mirrors keep MCP validation independent of the httpapi
// package (the dependency runs httpapi -> mcpsrv, not the reverse) while
// staying in lockstep. An empty value is allowed and means "unset" (clear the
// theme key, restoring the default small / thumbnail-left look).
func validProductImageSize(v string) bool {
	switch v {
	case "", "small", "medium", "large", "hidden":
		return true
	}
	return false
}

func validProductImageLayout(v string) bool {
	switch v {
	case "", "thumbnail-left", "image-top":
		return true
	}
	return false
}

// ── register_form_fields (issue #254) ─────────────────────────────────────
//
// The agent registers each input field via a strict contract. JomForm
// validates the contract, renders the inputs, and re-validates all fields
// server-side on submit (agent JS is client-side only, bypassable).

// registerFormFieldsIn is the input for register_form_fields.
type registerFormFieldsIn struct {
	FormID string             `json:"form_id" jsonschema:"Form ID"`
	Fields []formfields.Field `json:"fields" jsonschema:"List of registered input fields: {field_id, type, label, required, validation?, product_id?}. Replaces all existing fields."`
}

// registerFormFields validates and stores a form's registered input fields
// (issue #254). The contract is strict: field_id, type and label are
// required; type must be a known input type; product_id is optional (for
// pricing fields, the price still resolves from the product DB server-side).
func (s *Server) registerFormFields(
	ctx context.Context, _ *mcp.CallToolRequest, in registerFormFieldsIn,
) (*mcp.CallToolResult, map[string]string, error) {
	if in.FormID == "" {
		return nil, nil, errors.New("form_id is required")
	}
	// Issue #307: resolve + RBAC-gate the owning account across the user's workspaces.
	acc, err := s.requireFormWrite(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	f, err := s.deps.Forms.GetByID(ctx, acc, in.FormID)
	if err != nil {
		return nil, nil, errors.New("form not found")
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
