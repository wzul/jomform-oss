// MCP tools: template gallery parity (issue #174). Agents can browse the
// system template library and clone a template into a new draft, mirroring
// the dashboard template picker (GET /v1/templates + POST /v1/templates/use).
package mcpsrv

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// shortToken returns n random bytes hex-encoded, for unique cloned slugs.
func shortToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "x"
	}
	return hex.EncodeToString(b)
}

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
) (*mcp.CallToolResult, []templateOut, error) {
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
	return nil, out, nil
}

type useTemplateIn struct {
	Slug string `json:"slug" jsonschema:"Slug template (dari list_templates)"`
	Name string `json:"name,omitempty" jsonschema:"Nama borang baharu (optional; lalai = tajuk template)"`
}

// useTemplateMCP clones a template config/theme into a new draft for the
// caller (write role required), mirroring POST /v1/templates/use.
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
	if in.Slug == "" {
		return nil, nil, errors.New("slug wajib")
	}
	tpl, err := s.deps.Forms.TemplateBySlug(ctx, in.Slug)
	if err != nil {
		return nil, nil, fmt.Errorf("template tidak dijumpai")
	}
	title := in.Name
	if title == "" {
		title = tpl.Title
	}
	f := &dbstore.Form{
		AccountID:       acc,
		Slug:            tpl.Slug + "-" + shortToken(3),
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
	return nil, map[string]string{"status": "created", "form_id": f.ID, "slug": f.Slug, "title": f.Title}, nil
}
