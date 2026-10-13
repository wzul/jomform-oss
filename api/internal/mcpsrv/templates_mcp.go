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

	"github.com/wzul/jomform/api/internal/dbstore"
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
		return nil, nil, errors.New("forms tidak tersedia")
	}
	forms, err := s.deps.Forms.ListTemplates(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]templateOut, 0, len(forms))
	for _, f := range forms {
		desc, cat, mode := "", "", ""
		if c, ok := f.Config["category"].(string); ok {
			cat = c
		}
		if m, ok := f.Config["mode"].(string); ok {
			mode = m
		}
		if blocks, ok := f.Config["blocks"].([]any); ok {
			for _, b := range blocks {
				if bm, ok := b.(map[string]any); ok && bm["type"] == "text" {
					if s2, ok := bm["value"].(string); ok {
						desc = s2
						break
					}
				}
			}
		}
		out = append(out, templateOut{
			ID: f.ID, Slug: f.Slug, Title: f.Title,
			Description: desc, Category: cat, Mode: mode, Theme: f.Theme,
		})
	}
	return nil, newListOut(out), nil
}

type useTemplateIn struct {
	Slug       string `json:"slug,omitempty" jsonschema:"Slug template (dari list_templates)"`
	TemplateID string `json:"template_id,omitempty" jsonschema:"ID template anda sendiri (dari list_own_templates) — alternatif kepada slug"`
	Name       string `json:"name,omitempty" jsonschema:"Nama borang baharu (optional; lalai = tajuk template)"`
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
		return nil, nil, errors.New("forms tidak tersedia")
	}
	if in.Slug == "" && in.TemplateID == "" {
		return nil, nil, errors.New("slug atau template_id wajib")
	}
	var tpl *dbstore.Form
	if in.TemplateID != "" {
		tpl, err = s.deps.Forms.TemplateByIDForAccount(ctx, acc, in.TemplateID)
	} else {
		tpl, err = s.deps.Forms.TemplateBySlug(ctx, in.Slug)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("template tidak dijumpai")
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
		return nil, nil, errors.New("forms tidak tersedia")
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
	FormID string         `json:"form_id,omitempty" jsonschema:"ID borang sedia ada untuk disimpan sebagai template (dari list_forms)"`
	Title  string         `json:"title,omitempty" jsonschema:"Tajuk template (wajib bila tiada form_id)"`
	Config map[string]any `json:"config,omitempty" jsonschema:"Block config template (pilihan; guna bila tiada form_id)"`
	Theme  map[string]any `json:"theme,omitempty" jsonschema:"Gaya/branding template (pilihan)"`
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
		return nil, nil, errors.New("forms tidak tersedia")
	}
	title := strings.TrimSpace(in.Title)
	config, theme := in.Config, in.Theme
	if in.FormID != "" {
		src, gerr := s.deps.Forms.GetByID(ctx, acc, in.FormID)
		if gerr != nil {
			return nil, nil, errors.New("borang tidak dijumpai")
		}
		config, theme = src.Config, src.Theme
		if title == "" {
			title = src.Title
		}
	}
	if title == "" {
		return nil, nil, errors.New("title atau form_id wajib")
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
	TemplateID string         `json:"template_id" jsonschema:"ID template anda (dari list_own_templates)"`
	Title      string         `json:"title,omitempty" jsonschema:"Tajuk baharu (optional)"`
	Config     map[string]any `json:"config,omitempty" jsonschema:"Block config baharu (optional; omit = tak berubah)"`
	Theme      map[string]any `json:"theme,omitempty" jsonschema:"Gaya/branding baharu (optional; omit = tak berubah)"`
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
		return nil, nil, errors.New("forms tidak tersedia")
	}
	if in.TemplateID == "" {
		return nil, nil, errors.New("template_id wajib")
	}
	existing, gerr := s.deps.Forms.TemplateByIDForAccount(ctx, acc, in.TemplateID)
	if gerr != nil {
		return nil, nil, errors.New("template tidak dijumpai")
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = existing.Title
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
	if err := s.deps.Forms.UpdateTemplate(ctx, acc, in.TemplateID, title, in.Config, in.Theme); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "updated", "template_id": in.TemplateID}, nil
}

type templateIDIn struct {
	TemplateID string `json:"template_id" jsonschema:"ID template anda (dari list_own_templates)"`
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
		return nil, nil, errors.New("forms tidak tersedia")
	}
	if in.TemplateID == "" {
		return nil, nil, errors.New("template_id wajib")
	}
	if err := s.deps.Forms.SoftDeleteTemplate(ctx, acc, in.TemplateID); err != nil {
		return nil, nil, errors.New("template tidak dijumpai")
	}
	return nil, map[string]string{"status": "deleted", "template_id": in.TemplateID}, nil
}
