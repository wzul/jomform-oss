// MCP tools: form listing + retrieval (agents can browse & manage forms).
package mcpsrv

import (
	"context"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
)

type formOut struct {
	ID     string `json:"id"`
	Slug   string `json:"slug"`
	Title  string `json:"title"`
	Status string `json:"status"`
	// IsPrivate reports whether the form is password-protected (issue #176).
	// The password itself is never returned.
	IsPrivate bool   `json:"is_private"`
	URL       string `json:"url,omitempty"`
}

type formDetailOut struct {
	ID     string         `json:"id"`
	Slug   string         `json:"slug"`
	Title  string         `json:"title"`
	Status string         `json:"status"`
	// IsPrivate reports whether the form is password-protected (issue #176).
	IsPrivate bool           `json:"is_private"`
	URL       string         `json:"url,omitempty"`
	Config    map[string]any `json:"config,omitempty"`
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
	// never the legacy /f/{slug} form. Issue #83: a verified custom domain
	// takes precedence over the space subdomain.
	out := make([]formOut, 0, len(forms))
	for _, f := range forms {
		u := ""
		if f.Status == dbstore.FormStatusPublished {
			u = s.formPublicURLForAccount(ctx, acc, f.Slug)
		}
		out = append(out, formOut{
			ID: f.ID, Slug: f.Slug, Title: f.Title, Status: f.Status,
			IsPrivate: f.Status == dbstore.FormStatusPrivate, URL: u,
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
				ID: f.ID, Slug: f.Slug, Title: f.Title, Status: f.Status,
				IsPrivate: f.Status == dbstore.FormStatusPrivate, Config: f.Config,
			}
			if f.Status == dbstore.FormStatusPublished {
				out.URL = s.formPublicURLForAccount(ctx, acc, f.Slug)
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

// formPublicURLForAccount builds the canonical public URL for a form,
// preferring a verified custom domain over the space subdomain (issue #83).
// Returns "" when neither is resolvable.
func (s *Server) formPublicURLForAccount(ctx context.Context, accountID, slug string) string {
	if s.deps.Accounts != nil {
		if acc, aerr := s.deps.Accounts.ByID(ctx, accountID); aerr == nil && acc.CustomDomain != "" && acc.CustomDomainVerified {
			return "https://" + strings.TrimRight(acc.CustomDomain, "/") + "/" + slug
		}
	}
	return formPublicURL(s.spaceSlugFor(ctx, accountID), slug)
}
