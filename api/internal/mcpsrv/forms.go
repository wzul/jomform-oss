// MCP tools: form listing + retrieval (agents can browse & manage forms).
package mcpsrv

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type formOut struct {
	ID     string `json:"id"`
	Slug   string `json:"slug"`
	Title  string `json:"title"`
	Status string `json:"status"`
	URL    string `json:"url,omitempty"`
}

type formDetailOut struct {
	ID     string         `json:"id"`
	Slug   string         `json:"slug"`
	Title  string         `json:"title"`
	Status string         `json:"status"`
	URL    string         `json:"url,omitempty"`
	Config map[string]any `json:"config,omitempty"`
}

func (s *Server) listForms(
	ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn,
) (*mcp.CallToolResult, []formOut, error) {
	acc, err := s.accountAndScope(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Forms == nil {
		return nil, nil, errors.New("forms tidak tersedia")
	}
	forms, err := s.deps.Forms.ListByAccount(ctx, acc)
	if err != nil {
		return nil, nil, err
	}
	// Issue #77: public links are space-scoped ({space}.jomform.com/{slug}),
	// never the legacy /f/{slug} form.
	spaceSlug := s.spaceSlugFor(ctx, acc)
	out := make([]formOut, 0, len(forms))
	for _, f := range forms {
		u := ""
		if f.Status == "published" {
			u = formPublicURL(spaceSlug, f.Slug)
		}
		out = append(out, formOut{
			ID: f.ID, Slug: f.Slug, Title: f.Title, Status: f.Status, URL: u,
		})
	}
	return nil, out, nil
}

func (s *Server) getForm(
	ctx context.Context, _ *mcp.CallToolRequest, in formRefIn,
) (*mcp.CallToolResult, formDetailOut, error) {
	acc, err := s.accountAndScope(ctx, false)
	if err != nil {
		return nil, formDetailOut{}, err
	}
	if s.deps.Forms == nil {
		return nil, formDetailOut{}, errors.New("forms tidak tersedia")
	}
	forms, err := s.deps.Forms.ListByAccount(ctx, acc)
	if err != nil {
		return nil, formDetailOut{}, err
	}
	spaceSlug := s.spaceSlugFor(ctx, acc)
	for _, f := range forms {
		if f.ID == in.FormID {
			out := formDetailOut{
				ID: f.ID, Slug: f.Slug, Title: f.Title, Status: f.Status, Config: f.Config,
			}
			if f.Status == "published" {
				out.URL = formPublicURL(spaceSlug, f.Slug)
			}
			return nil, out, nil
		}
	}
	return nil, formDetailOut{}, errors.New("borang tidak dijumpai")
}

// spaceSlugFor resolves the space slug an account's forms live under, for
// building space-scoped public URLs ("" when the account has no linked space).
func (s *Server) spaceSlugFor(ctx context.Context, accountID string) string {
	if s.deps.Spaces == nil {
		return ""
	}
	slug, err := s.deps.Spaces.SpaceSlugByAccount(ctx, accountID)
	if err != nil {
		return ""
	}
	return slug
}

// formPublicURL builds the canonical space-scoped public URL for a form
// (issue #77). Returns "" when the space slug is unknown.
func formPublicURL(spaceSlug, slug string) string {
	if spaceSlug == "" {
		return ""
	}
	return "https://" + spaceSlug + ".jomform.com/" + slug
}
