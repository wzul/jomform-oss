// MCP tools: form listing + retrieval (agents can browse & manage forms).
package mcpsrv

import (
	"context"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/formfields"
)

type formOut struct {
	ID     string `json:"id"`
	Slug   string `json:"slug"`
	Title  string `json:"title"`
	Status string `json:"status"`
	// IsPrivate reports whether the form is password-protected (issue #176).
	// The password itself is never returned.
	IsPrivate bool `json:"is_private"`
	// Mode is the form's collection mode — "payment" or "response".
	// An agent must be able to tell an order form from an RSVP form before
	// offering the merchant a sales action.
	Mode string `json:"mode"`
	URL  string `json:"url,omitempty"`
}

type formDetailOut struct {
	ID     string `json:"id"`
	Slug   string `json:"slug"`
	Title  string `json:"title"`
	Status string `json:"status"`
	// IsPrivate reports whether the form is password-protected (issue #176).
	IsPrivate bool `json:"is_private"`
	// Mode is the form's collection mode — "payment" or "response".
	Mode   string         `json:"mode"`
	URL    string         `json:"url,omitempty"`
	Config map[string]any `json:"config,omitempty"`
	// Issue #254: custom styling/theme + registered input fields (MCP parity).
	Theme            map[string]any     `json:"theme,omitempty"`
	RegisteredFields []formfields.Field `json:"registered_fields,omitempty"`
}

func (s *Server) listForms(
	ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn,
) (*mcp.CallToolResult, []formOut, error) {
	accs, err := s.accountScopes(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Forms == nil {
		return nil, nil, errors.New("forms tidak tersedia")
	}
	// Issue #307: a user-level agent lists forms across all of the user's
	// accessible workspaces (ownership enforced per store query).
	out := make([]formOut, 0)
	for _, acc := range accs {
		forms, err := s.deps.Forms.ListByAccount(ctx, acc)
		if err != nil {
			return nil, nil, err
		}
		// Issue #77: public links are workspace-scoped ({workspace}.jomform.com/{slug}),
		// never the legacy /f/{slug} form. Issue #83: a verified custom domain
		// takes precedence over the workspace subdomain.
		for _, f := range forms {
			u := ""
			if f.Status == dbstore.FormStatusPublished || f.Status == dbstore.FormStatusUnlisted {
				u = s.formPublicURLForAccount(ctx, acc, f.Slug)
			}
			out = append(out, formOut{
				ID: f.ID, Slug: f.Slug, Title: f.Title, Status: f.Status,
				IsPrivate: f.Status == dbstore.FormStatusPrivate, URL: u,
				Mode: dbstore.FormMode(f),
			})
		}
	}
	return nil, out, nil
}

func (s *Server) getForm(
	ctx context.Context, _ *mcp.CallToolRequest, in formRefIn,
) (*mcp.CallToolResult, formDetailOut, error) {
	if s.deps.Forms == nil {
		return nil, formDetailOut{}, errors.New("forms tidak tersedia")
	}
	// Issue #307: the form may live in any of the user's workspaces; resolve the
	// owning account (RBAC via resolveFormAccount scope).
	acc, err := s.resolveFormAccount(ctx, in.FormID)
	if err != nil {
		return nil, formDetailOut{}, err
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
				Theme: f.Theme, RegisteredFields: formfields.FromConfig(f.Config),
				Mode: dbstore.FormMode(f),
			}
			if f.Status == dbstore.FormStatusPublished || f.Status == dbstore.FormStatusUnlisted {
				out.URL = s.formPublicURLForAccount(ctx, acc, f.Slug)
			}
			return nil, out, nil
		}
	}
	return nil, formDetailOut{}, errors.New("borang tidak dijumpai")
}

// workspaceSlugFor resolves the workspace slug an account's forms live under, for
// building workspace-scoped public URLs ("" when the account has no linked workspace).
func (s *Server) workspaceSlugFor(ctx context.Context, accountID string) string {
	if s.deps.Workspaces == nil {
		return ""
	}
	slug, err := s.deps.Workspaces.WorkspaceSlugByAccount(ctx, accountID)
	if err != nil {
		return ""
	}
	return slug
}

// formPublicURL builds the canonical workspace-scoped public URL for a form
// (issue #77). Returns "" when the workspace slug is unknown.
func formPublicURL(workspaceSlug, slug string) string {
	if workspaceSlug == "" {
		return ""
	}
	return "https://" + workspaceSlug + ".jomform.com/" + slug
}

// formPublicURLForAccount builds the canonical public URL for a form,
// preferring a verified custom domain over the workspace subdomain (issue #83).
// Returns "" when neither is resolvable.
func (s *Server) formPublicURLForAccount(ctx context.Context, accountID, slug string) string {
	if s.deps.Accounts != nil {
		if acc, aerr := s.deps.Accounts.ByID(ctx, accountID); aerr == nil && acc.CustomDomain != "" && acc.CustomDomainVerified {
			return "https://" + strings.TrimRight(acc.CustomDomain, "/") + "/" + slug
		}
	}
	return formPublicURL(s.workspaceSlugFor(ctx, accountID), slug)
}
