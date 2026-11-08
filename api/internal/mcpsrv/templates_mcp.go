// MCP tools: template gallery parity (issue #174). Agents can browse the
// system template library and clone a template into a new draft, mirroring
// the dashboard template picker (GET /v1/templates + POST /v1/templates/use).
package mcpsrv

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/customform"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/formfields"
	"github.com/wzul/jomform/api/internal/urlguard"
)

type templateOut struct {
	ID          string         `json:"id"`
	Slug        string         `json:"slug"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Category    string         `json:"category,omitempty"`
	Mode        string         `json:"mode,omitempty"`
	Theme       map[string]any `json:"theme,omitempty"`
}

// listTemplatesMCP returns the published system template gallery (no auth —
// same as the REST listTemplates endpoint).
func (s *Server) listTemplatesMCP(
	ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn,
) (*mcp.CallToolResult, *listOutput[templateOut], error) {
	if s.deps.Forms == nil {
		return nil, nil, errors.New("forms not available")
	}
	forms, err := s.deps.Forms.ListTemplates(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]templateOut, 0, len(forms))
	for _, f := range forms {
		desc, mode := "", ""
		// Category is TRIMMED, the same way the publish gate compares it and
		// the REST gallery returns it, so an agent and the dashboard see one
		// value: the picker paints it as the card's category chip and keys its
		// icon off the exact string (t_b58fd4fc).
		cat := formfields.Category(f.Config)
		if m, ok := f.Config["mode"].(string); ok {
			mode = m
		}
		// Description = the form's intro line (card t_7fb80479): the first
		// section/html block's value, falling back to the first text block's
		// value for a row written while `text` still meant prose. Never a
		// field's label — `text` is an input now, and its value is the label.
		desc = formfields.Description(f.Config)
		out = append(out, templateOut{
			ID: f.ID, Slug: f.Slug, Title: f.Title,
			Description: desc, Category: cat, Mode: mode, Theme: f.Theme,
		})
	}
	return nil, newListOut(out), nil
}

type useTemplateIn struct {
	Slug       string `json:"slug,omitempty" jsonschema:"Template slug (from list_templates)"`
	TemplateID string `json:"template_id,omitempty" jsonschema:"Your own template ID (from list_own_templates) — an alternative to slug"`
	Name       string `json:"name,omitempty" jsonschema:"New form name (optional; default = template title)"`
}

// refuseBlockedCustomJS answers the same sentence (`custom_js ditolak:
// <reason>`) the form write paths (createForm/updateForm, set_form_theme) and
// the REST template routes answer, and reports whether it refused.
//
// Card t_ef3b0573: a template's theme is cloned verbatim onto a real form by
// use_template, and since v1.21.283 the block-rendered buyer page serves
// theme.custom_js as live code. Every sibling guard here is already justified
// by that clone; custom_js was the one guard missing, so a value the form
// paths refuse was storable on a template over MCP and then became live code
// on a buyer's page. One rule, one wording, every door.
//
// A nil theme means "leave unchanged" on update and "nothing supplied" on
// create, so it is not a refusal; a non-string custom_js is not JS and is left
// to the renderer's type handling, exactly as on the form paths.
func refuseBlockedCustomJS(theme map[string]any) error {
	if theme == nil {
		return nil
	}
	js, ok := theme["custom_js"].(string)
	if !ok {
		return nil
	}
	if reason := customform.ScanCustomJS(js); reason != "" {
		return errors.New(customform.RejectedMessage(reason))
	}
	return nil
}

// useTemplateMCP clones a template config/theme into a new draft for the
// caller (write role required), mirroring POST /v1/templates/use. Accepts
// either a gallery slug (system library) or template_id (an own template,
// issue #457 — owner-scoped).
func (s *Server) useTemplateMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in useTemplateIn,
) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.requireWriteRole(ctx)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Forms == nil {
		return nil, nil, errors.New("forms not available")
	}
	if in.Slug == "" && in.TemplateID == "" {
		return nil, nil, errors.New("slug or template_id is required")
	}
	// Card t_7546eda5: this clone MINTS a real form row (draft, and publishable —
	// served at /f/<slug> like any other), so it is the second MCP door into the
	// orphan-form defect: with no workspace linked, the forms_sync_workspace_id
	// trigger (000045) copies NULL onto the row and the cloned form is then 404
	// at every address, forever, with no error here. Refused with the REST
	// form-create refusal (`workspace.required`) BEFORE the template is resolved:
	// the account's state cannot carry the call, so the answer must not depend on
	// which template was named.
	if err := s.requireLiveWorkspace(ctx, acc); err != nil {
		return nil, nil, err
	}
	var tpl *dbstore.Form
	if in.TemplateID != "" {
		tpl, err = s.deps.Forms.TemplateByIDForAccount(ctx, acc, in.TemplateID)
	} else {
		tpl, err = s.deps.Forms.TemplateBySlug(ctx, in.Slug)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("template not found")
	}
	// Card t_ef3b0573: the clone below copies tpl.Theme verbatim onto a new
	// form, whose buyer page serves theme.custom_js as live code. The write
	// paths now refuse a blocked value, but a row that predates that guard (or
	// was written by any other door) would still become live code here, so the
	// clone door refuses it too.
	if err := refuseBlockedCustomJS(tpl.Theme); err != nil {
		return nil, nil, err
	}
	title := in.Name
	if title == "" {
		title = tpl.Title
	}
	f := &dbstore.Form{
		AccountID:       acc,
		Slug:            s.uniqueFormSlug(ctx, acc, tpl.Title),
		Title:           title,
		Config:          tpl.Config,
		Theme:           tpl.Theme,
		Status:          "draft",
		AllowEmbed:      true,
		LogoURL:         tpl.LogoURL,
		RedirectSuccess: tpl.RedirectSuccess,
		RedirectFailure: tpl.RedirectFailure,
		FailureMessage:  tpl.FailureMessage,
	}
	if err := s.deps.Forms.Create(ctx, f); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	// Issue #193: a cloned form gets the default notification for its own mode
	// (issue #597).
	if s.deps.Notifications != nil {
		_ = s.deps.Notifications.EnsureDefault(ctx, f)
	}
	return nil, map[string]string{"status": "created", "form_id": f.ID, "slug": f.Slug, "title": f.Title}, nil
}

// ── Issue #457: merchant-managed templates (MCP parity) ─────────────────────
//
// Parity for the dashboard Templates view: list the caller's OWN templates,
// save an existing form (or a fresh config) as a reusable template, edit it,
// delete it, and clone it into a new draft. Every operation is account-scoped,
// so an agent can never read, edit, use or delete another workspace's
// template. System gallery tools (list_templates / use_template with a slug)
// stay unchanged.

type ownTemplateOut struct {
	ID        string         `json:"id"`
	Slug      string         `json:"slug"`
	Title     string         `json:"title"`
	UpdatedAt string         `json:"updated_at,omitempty"`
	Config    map[string]any `json:"config,omitempty"`
	Theme     map[string]any `json:"theme,omitempty"`
}

// listOwnTemplatesMCP returns the account's own templates (issue #457).
func (s *Server) listOwnTemplatesMCP(
	ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn,
) (*mcp.CallToolResult, *listOutput[ownTemplateOut], error) {
	acc, err := s.accountAndScope(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Forms == nil {
		return nil, nil, errors.New("forms not available")
	}
	rows, err := s.deps.Forms.ListTemplatesByAccount(ctx, acc)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]ownTemplateOut, 0, len(rows))
	for _, f := range rows {
		out = append(out, ownTemplateOut{
			ID: f.ID, Slug: f.Slug, Title: f.Title,
			UpdatedAt: f.UpdatedAt.UTC().Format(time.RFC3339),
			Config:    f.Config, Theme: f.Theme,
		})
	}
	return nil, newListOut(out), nil
}

type createTemplateIn struct {
	FormID string         `json:"form_id,omitempty" jsonschema:"Existing form ID to save as a template (from list_forms)"`
	Title  string         `json:"title,omitempty" jsonschema:"Template title (required when there is no form_id)"`
	Config map[string]any `json:"config,omitempty" jsonschema:"Template block config (optional; use when there is no form_id)"`
	Theme  map[string]any `json:"theme,omitempty" jsonschema:"Template style/branding (optional)"`
}

// createTemplateMCP saves an existing form (form_id) — or a fresh config — as
// a reusable template owned by the caller (issue #457).
func (s *Server) createTemplateMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in createTemplateIn,
) (*mcp.CallToolResult, map[string]any, error) {
	acc, err := s.requireWriteRole(ctx)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Forms == nil {
		return nil, nil, errors.New("forms not available")
	}
	title := strings.TrimSpace(in.Title)
	config, theme := in.Config, in.Theme
	if in.FormID != "" {
		src, gerr := s.deps.Forms.GetByID(ctx, acc, in.FormID)
		if gerr != nil {
			return nil, nil, errors.New("form not found")
		}
		config, theme = src.Config, src.Theme
		if title == "" {
			title = src.Title
		}
	}
	if title == "" {
		return nil, nil, errors.New("title or form_id is required")
	}
	// Card t_ef3b0573: a template's theme is cloned onto a real form by
	// use_template and the buyer page serves theme.custom_js verbatim, so a
	// value the form write paths refuse ("custom_js ditolak: <reason>") must
	// not be storable here — otherwise this tool is a bypass around that
	// refusal. Same sentence, same rule as createForm/updateForm and the REST
	// template routes.
	if err := refuseBlockedCustomJS(in.Theme); err != nil {
		return nil, nil, err
	}
	// Issue #501: a template's config/theme is cloned onto a real form by
	// use_template, so a non-https image URL stored here would reach a
	// buyer's page as #ZgotmplZ. Reject it with the ONE shared scheme rule.
	if field, _ := urlguard.CheckTheme(theme); field != "" {
		return nil, nil, errors.New(urlguard.InvalidMessage("theme." + field))
	}
	if field, _ := urlguard.CheckConfig(config); field != "" {
		return nil, nil, errors.New(urlguard.InvalidMessage("config." + field))
	}
	// Card t_8fcb546d: the MCP template write path stores config verbatim, so it
	// refuses a present-but-not-array `config.blocks` exactly as the REST
	// template routes do — the shape makes the gallery-metadata predicate raise
	// SQLSTATE 22023 for every id in a bulk publish.
	if field, want := urlguard.CheckConfigShape(config); field != "" {
		return nil, nil, errors.New(urlguard.InvalidShapeMessage("config."+field, want))
	}
	// Card t_4086b8f0: a template is cloned onto a real form by use_template, so
	// a block type no renderer can honour is refused here too.
	if bt := formfields.UnsupportedBlock(config); bt != "" {
		return nil, nil, errors.New(formfields.UnsupportedBlockMessage(bt))
	}
	// Card t_3d11f98d: and so is an option/shipping block with no resolvable
	// price — cloned onto a form it would be a free add-on the merchant
	// believes is priced.
	if msg := formfields.UnpricedPricingBlock(config); msg != "" {
		return nil, nil, errors.New(msg)
	}
	// Card t_73a5db6e: and so is an unsatisfiable custom-form contract — it
	// would reach a buyer's page through use_template.
	if err := customform.ValidateAndNormalizeConfig(config); err != nil {
		return nil, nil, err
	}
	f := &dbstore.Form{
		AccountID: acc,
		Slug:      s.uniqueFormSlug(ctx, acc, "tpl-"+title),
		Title:     title,
		Config:    orEmptyMap(config),
		Theme:     orEmptyMap(theme),
	}
	if err := s.deps.Forms.CreateTemplate(ctx, f); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]any{
		"status": "created", "template_id": f.ID, "slug": f.Slug, "title": f.Title,
	}, nil
}

type updateTemplateIn struct {
	TemplateID string         `json:"template_id" jsonschema:"Your template ID (from list_own_templates)"`
	Title      string         `json:"title,omitempty" jsonschema:"New title (optional)"`
	Config     map[string]any `json:"config,omitempty" jsonschema:"New block config (optional; omit = unchanged)"`
	Theme      map[string]any `json:"theme,omitempty" jsonschema:"New style/branding (optional; omit = unchanged)"`
}

// updateTemplateMCP edits one of the caller's own templates (issue #457).
func (s *Server) updateTemplateMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in updateTemplateIn,
) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.requireWriteRole(ctx)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Forms == nil {
		return nil, nil, errors.New("forms not available")
	}
	if in.TemplateID == "" {
		return nil, nil, errors.New("template_id is required")
	}
	existing, gerr := s.deps.Forms.TemplateByIDForAccount(ctx, acc, in.TemplateID)
	if gerr != nil {
		return nil, nil, errors.New("template not found")
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = existing.Title
	}
	// Card t_ef3b0573: same refusal as create_template — a template's theme is
	// cloned onto a real form by use_template, so an update must not store a
	// blocked custom_js (nil theme = leave unchanged, and is skipped).
	if err := refuseBlockedCustomJS(in.Theme); err != nil {
		return nil, nil, err
	}
	// Issue #501: same shared scheme rule as create_template (nil = leave
	// unchanged, so only an explicitly supplied map is validated).
	if field, _ := urlguard.CheckTheme(in.Theme); field != "" {
		return nil, nil, errors.New(urlguard.InvalidMessage("theme." + field))
	}
	if field, _ := urlguard.CheckConfig(in.Config); field != "" {
		return nil, nil, errors.New(urlguard.InvalidMessage("config." + field))
	}
	// Card t_8fcb546d: same present-but-not-array `config.blocks` refusal as
	// create_template.
	if field, want := urlguard.CheckConfigShape(in.Config); field != "" {
		return nil, nil, errors.New(urlguard.InvalidShapeMessage("config."+field, want))
	}
	// Card t_4086b8f0: same unsupported-block refusal as create_template.
	if bt := formfields.UnsupportedBlock(in.Config); bt != "" {
		return nil, nil, errors.New(formfields.UnsupportedBlockMessage(bt))
	}
	// Card t_3d11f98d: same unresolved-pricing refusal as create_template.
	if msg := formfields.UnpricedPricingBlock(in.Config); msg != "" {
		return nil, nil, errors.New(msg)
	}
	// Card t_73a5db6e: same unsatisfiable-custom-form refusal as
	// create_template (nil config = leave unchanged, and is skipped).
	if err := customform.ValidateAndNormalizeConfig(in.Config); err != nil {
		return nil, nil, err
	}
	if err := s.deps.Forms.UpdateTemplate(ctx, acc, in.TemplateID, title, in.Config, in.Theme); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "updated", "template_id": in.TemplateID}, nil
}

type templateIDIn struct {
	TemplateID string `json:"template_id" jsonschema:"Your template ID (from list_own_templates)"`
}

// deleteTemplateMCP soft-deletes one of the caller's own templates (#457).
func (s *Server) deleteTemplateMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in templateIDIn,
) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.requireWriteRole(ctx)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Forms == nil {
		return nil, nil, errors.New("forms not available")
	}
	if in.TemplateID == "" {
		return nil, nil, errors.New("template_id is required")
	}
	if err := s.deps.Forms.SoftDeleteTemplate(ctx, acc, in.TemplateID); err != nil {
		return nil, nil, errors.New("template not found")
	}
	return nil, map[string]string{"status": "deleted", "template_id": in.TemplateID}, nil
}
