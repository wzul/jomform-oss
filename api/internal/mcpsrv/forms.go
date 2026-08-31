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
	out := make([]formOut, 0, len(forms))
	for _, f := range forms {
		u := ""
		if f.Status == "published" {
			u = "https://jomform.com/f/" + f.Slug
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
	for _, f := range forms {
		if f.ID == in.FormID {
			out := formDetailOut{
				ID: f.ID, Slug: f.Slug, Title: f.Title, Status: f.Status, Config: f.Config,
			}
			if f.Status == "published" {
				out.URL = "https://jomform.com/f/" + f.Slug
			}
			return nil, out, nil
		}
	}
	return nil, formDetailOut{}, errors.New("borang tidak dijumpai")
}
