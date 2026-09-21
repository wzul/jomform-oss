// MCP tools: form listing + retrieval (agents can browse & manage forms).
package mcpsrv

import (
	"context"
	"errors"
	"strings"
	"time"

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
	// created_at/updated_at are RFC3339 strings, matching every other MCP
	// timestamp shape (admin_mcp.go, events_mcp.go, …). They let an agent tell
	// two same-titled forms apart, and let a read → write → re-read cycle show
	// that the write actually landed.
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

func (s *Server) listForms(
	ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn,
) (*mcp.CallToolResult, *listOutput[formOut], error) {
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
	// Wrapped in listOutput so the tool advertises an OBJECT outputSchema —
	// a bare slice makes a schema-strict client reject the WHOLE tools/list
	// result and take all tools down (see output_schema_test.go).
	return nil, newListOut(out), nil
}

func (s *Server) getForm(
	ctx context.Context, _ *mcp.CallToolRequest, in formRefIn,
) (*mcp.CallToolResult, *formDetailOut, error) {
	if s.deps.Forms == nil {
		return nil, nil, errors.New("forms tidak tersedia")
	}
	if in.FormID == "" {
		return nil, nil, errors.New("form_id wajib")
	}
	// Issue #307: the form may live in any of the user's workspaces; resolve the
	// owning account (RBAC via resolveFormAccount scope).
	acc, err := s.resolveFormAccount(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	// One form by id: the store query enforces ownership and does not depend on
	// the form being present in a list read.
	f, err := s.deps.Forms.GetByID(ctx, acc, in.FormID)
	if err != nil {
		return nil, nil, errors.New("borang tidak dijumpai")
	}
	out := &formDetailOut{
		ID: f.ID, Slug: f.Slug, Title: f.Title, Status: f.Status,
		IsPrivate: f.Status == dbstore.FormStatusPrivate, Config: f.Config,
		Theme: f.Theme, RegisteredFields: formfields.FromConfig(f.Config),
		Mode:      dbstore.FormMode(f),
		CreatedAt: f.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: f.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if f.Status == dbstore.FormStatusPublished || f.Status == dbstore.FormStatusUnlisted {
		out.URL = s.formPublicURLForAccount(ctx, acc, f.Slug)
	}
	return nil, out, nil
}

// ── Tong sampah (trash): list / restore / purge ──────────────────────────────
//
// Parity for the REST trash routes (GET /v1/rest/forms/trash, POST
// /v1/rest/forms/{id}/restore, DELETE /v1/rest/forms/{id}/purge).

// trashedFormOut is one row of the trash listing.
type trashedFormOut struct {
	ID    string `json:"id"`
	Slug  string `json:"slug"`
	Title string `json:"title"`
	// DeletedAt is when the form entered the trash (RFC3339).
	DeletedAt string `json:"deleted_at"`
	// SalesCount is how many sales reference the form. Non-zero means purge
	// refuses (409): sales.form_id is ON DELETE SET NULL, so the purge would
	// silently orphan those sales.
	SalesCount int64 `json:"sales_count"`
	// CanPurge is the pre-computed answer, so a caller does not have to derive it
	// from sales_count.
	CanPurge bool `json:"can_purge"`
}

// listDeletedForms: trash listing across the caller's accessible accounts.
func (s *Server) listDeletedForms(
	ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn,
) (*mcp.CallToolResult, *listOutput[trashedFormOut], error) {
	if s.deps.Forms == nil {
		return nil, nil, errors.New("forms tidak tersedia")
	}
	accs, err := s.accountScopes(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	out := make([]trashedFormOut, 0)
	for _, acc := range accs {
		forms, err := s.deps.Forms.ListTrashed(ctx, acc)
		if err != nil {
			return nil, nil, err
		}
		if len(forms) == 0 {
			continue
		}
		counts, err := s.deps.Forms.SalesCountsByForm(ctx, acc)
		if err != nil {
			return nil, nil, err
		}
		for _, f := range forms {
			n := counts[f.ID]
			row := trashedFormOut{
				ID: f.ID, Slug: f.Slug, Title: f.Title,
				SalesCount: n, CanPurge: n == 0,
			}
			if f.DeletedAt != nil {
				row.DeletedAt = f.DeletedAt.UTC().Format(time.RFC3339)
			}
			out = append(out, row)
		}
	}
	return nil, newListOut(out), nil
}

// restoreFormMCP brings one trashed form back. No confirmation: it destroys
// nothing, and trashing it again undoes it.
//
// The slug-collision refusal is surfaced as data, not a silent rename: the
// per-workspace unique index covers live rows only, so a new form may have taken
// the slug while this one sat in the trash. Restoring would violate the index
// (SQLSTATE 23505 → dbstore.ErrFormSlugTaken), and renaming behind the caller's
// back would silently move a public URL they were told about.
func (s *Server) restoreFormMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in formRefIn,
) (*mcp.CallToolResult, map[string]any, error) {
	acc, err := s.requireTrashedFormWrite(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	if err := s.deps.Forms.Restore(ctx, acc, in.FormID); err != nil {
		if errors.Is(err, dbstore.ErrFormSlugTaken) {
			return nil, map[string]any{
				"status": "refused",
				"error": "slug borang ini telah diambil borang lain semasa ia dalam tong sampah — " +
					"tukar slug borang itu dahulu, kemudian pulihkan semula. Tiada apa-apa dinamakan semula secara automatik.",
				"form_id": in.FormID,
			}, nil
		}
		return nil, nil, err
	}
	return nil, map[string]any{"status": "restored", "form_id": in.FormID}, nil
}

// purgeFormMCP permanently destroys a TRASHED form.
//
// Two gates, because this cannot be undone:
//
//	confirm_slug — the caller must type the form's slug (same level as
//	  revoke_api_key's confirm=true). A destructive, irreversible call should
//	  require the caller to name what it is destroying.
//	sales        — a form that has ever been sold is REFUSED, not purged.
//	  sales.form_id is ON DELETE SET NULL, so the delete would succeed and
//	  silently orphan every sale that references it. Sales are history.
func (s *Server) purgeFormMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in purgeFormIn,
) (*mcp.CallToolResult, map[string]any, error) {
	acc, err := s.requireTrashedFormWrite(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	f, err := s.deps.Forms.GetTrashed(ctx, acc, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(in.ConfirmSlug) != f.Slug {
		return nil, map[string]any{
			"status": "refused",
			"error": "taip slug borang untuk sahkan pemadaman kekal (borang: " + f.Slug + "). " +
				"Pemadaman kekal tidak boleh dibatalkan.",
			"form_id": in.FormID, "slug": f.Slug,
		}, nil
	}
	if err := s.deps.Forms.Purge(ctx, acc, in.FormID); err != nil {
		if errors.Is(err, dbstore.ErrFormHasSales) {
			n, _ := s.deps.Forms.CountSalesByForm(ctx, acc, in.FormID)
			return nil, map[string]any{
				"status": "refused",
				"error": "borang ini mempunyai jualan — sejarah jualan tidak boleh dimusnahkan " +
					"dengan memadam borang. Ia kekal dalam tong sampah.",
				"form_id": in.FormID, "sales_count": n,
			}, nil
		}
		return nil, nil, err
	}
	return nil, map[string]any{
		"status": "purged", "form_id": in.FormID, "slug": f.Slug, "title": f.Title,
	}, nil
}

// publicHostSlugFor resolves the subdomain host an account's forms are publicly
// served under ("", only when the account row cannot be read). Delegates to the
// shared resolver; see WorkspaceStore.PublicHostSlugByAccount.
func (s *Server) publicHostSlugFor(ctx context.Context, accountID string) string {
	if s.deps.Workspaces == nil {
		return ""
	}
	slug, err := s.deps.Workspaces.PublicHostSlugByAccount(ctx, accountID)
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
	return formPublicURL(s.publicHostSlugFor(ctx, accountID), slug)
}
