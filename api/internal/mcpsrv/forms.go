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
	"github.com/wzul/jomform/api/internal/lifecycle"
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
	// AcceptingOrders is the derived buyer state (card t_3d5a8d32): the same
	// answer the public page and the dashboard list give, so an agent reading a
	// 200 does not have to guess whether the form's page can be submitted.
	// Reason explains a false; Code is the stable machine token.
	AcceptingOrders bool   `json:"accepting_orders"`
	Reason          string `json:"reason,omitempty"`
	Code            string `json:"code,omitempty"`
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

	// ── What this form can do (card t_3d5a8d32) ─────────────────────────────
	//
	// AcceptingOrders/Reason/Code are lifecycle.FormOrders' answer — the ONE
	// derived buyer state, the same one the dashboard list and the public page
	// ask. State is the row's lifecycle state (draft/private/published/
	// unlisted/template_*/trashed), and AvailableEvents is what the form
	// lifecycle table says the row accepts right now, each with enabled and,
	// when a guard refuses it, the English reason.
	AcceptingOrders bool   `json:"accepting_orders"`
	Reason          string `json:"reason,omitempty"`
	Code            string `json:"code,omitempty"`
	// NoProductBlockID names the product block to fix when the refusal is about
	// a specific block.
	NoProductBlockID string                     `json:"no_product_block_id,omitempty"`
	State            string                     `json:"state"`
	AvailableEvents  []lifecycle.AvailableEvent `json:"available_events"`
}

// formOrderEnv assembles the facts lifecycle.FormOrders needs that the row
// cannot carry: the account's product ids (the purged-product arm) and, only
// when the form HAS an entry limit, the recorded order/submission count.
func (s *Server) formOrderEnv(ctx context.Context, f *dbstore.Form, liveProductIDs map[string]bool) lifecycle.FormOrderEnv {
	env := lifecycle.FormOrderEnv{LiveProductIDs: liveProductIDs, Entries: -1, Now: time.Now()}
	if f.EntryLimit != nil && *f.EntryLimit > 0 {
		env.Entries = -1
		accounts := []string{f.AccountID}
		if dbstore.FormMode(f) == dbstore.FormModeResponse {
			if s.deps.Submissions != nil {
				if n, err := s.deps.Submissions.CountByAccountsForm(ctx, accounts, f.ID); err == nil {
					env.Entries = n
				}
			}
		} else if s.deps.Sales != nil {
			if n, err := s.deps.Sales.CountByAccountsForm(ctx, accounts, f.ID); err == nil {
				env.Entries = n
			}
		}
	}
	return env
}

// formOrder is the MCP-side single derivation of a form's buyer state. Every
// form read on this surface uses it, so a tool cannot answer differently from
// the dashboard or the page.
func (s *Server) formOrder(ctx context.Context, f *dbstore.Form) lifecycle.FormOrderState {
	return lifecycle.FormOrders(f, s.formOrderEnv(ctx, f, s.productIDsForAccount(ctx, f.AccountID)))
}

// availableFormEvents is the row's lifecycle actions, never nil (a nil slice
// marshals to null, which a schema-strict client would have to special-case).
func availableFormEvents(f *dbstore.Form) []lifecycle.AvailableEvent {
	out := lifecycle.FormsMachine.Available(f)
	if out == nil {
		return []lifecycle.AvailableEvent{}
	}
	return out
}

func (s *Server) listForms(
	ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn,
) (*mcp.CallToolResult, *listOutput[formOut], error) {
	accs, err := s.accountScopes(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Forms == nil {
		return nil, nil, errors.New("forms not available")
	}
	// Issue #307: a user-level agent lists forms across all of the user's
	// accessible workspaces (ownership enforced per store query).
	out := make([]formOut, 0)
	for _, acc := range accs {
		forms, err := s.deps.Forms.ListByAccount(ctx, acc)
		if err != nil {
			return nil, nil, err
		}
		// ONE product-id read per account — the dead-form arm needs it, and
		// reading it per row would make the list N+1.
		live := s.productIDsForAccount(ctx, acc)
		// Issue #77: public links are workspace-scoped ({workspace}.jomform.com/{slug}),
		// never the legacy /f/{slug} form. Issue #83: a verified custom domain
		// takes precedence over the workspace subdomain.
		for _, f := range forms {
			u := ""
			if f.Status == dbstore.FormStatusPublished || f.Status == dbstore.FormStatusUnlisted {
				u = s.formPublicURLForAccount(ctx, acc, f.Slug)
			}
			order := lifecycle.FormOrders(f, s.formOrderEnv(ctx, f, live))
			out = append(out, formOut{
				ID: f.ID, Slug: f.Slug, Title: f.Title, Status: f.Status,
				IsPrivate: f.Status == dbstore.FormStatusPrivate, URL: u,
				Mode:            dbstore.FormMode(f),
				AcceptingOrders: order.Accepting,
				Reason:          order.Reason,
				Code:            order.Code,
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
		return nil, nil, errors.New("forms not available")
	}
	if in.FormID == "" {
		return nil, nil, errors.New("form_id is required")
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
		return nil, nil, errors.New("form not found")
	}
	order := s.formOrder(ctx, f)
	out := &formDetailOut{
		ID: f.ID, Slug: f.Slug, Title: f.Title, Status: f.Status,
		IsPrivate: f.Status == dbstore.FormStatusPrivate, Config: f.Config,
		Theme: f.Theme, RegisteredFields: formfields.FromConfig(f.Config),
		Mode:      dbstore.FormMode(f),
		CreatedAt: f.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: f.UpdatedAt.UTC().Format(time.RFC3339),
		// Card t_3d5a8d32: what a caller can actually DO with this form, and
		// whether a buyer could submit it right now — so an agent never has to
		// infer either from a 200.
		AcceptingOrders:  order.Accepting,
		Reason:           order.Reason,
		Code:             order.Code,
		NoProductBlockID: order.BlockID,
		State:            f.LifecycleState(),
		AvailableEvents:  availableFormEvents(f),
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
		return nil, nil, errors.New("forms not available")
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
				"error": "this form's slug was taken by another form while it was in the Trash — " +
					"change that form's slug first, then restore again. Nothing is renamed automatically.",
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
			"error": "type the form slug to confirm the permanent deletion (form: " + f.Slug + "). " +
				"Permanent deletion cannot be undone.",
			"form_id": in.FormID, "slug": f.Slug,
		}, nil
	}
	if err := s.deps.Forms.Purge(ctx, acc, in.FormID); err != nil {
		if errors.Is(err, dbstore.ErrFormHasSales) {
			n, _ := s.deps.Forms.CountSalesByForm(ctx, acc, in.FormID)
			return nil, map[string]any{
				"status": "refused",
				"error": "this form has sales — sales history cannot be destroyed " +
					"by deleting the form. It stays in the Trash.",
				"form_id": in.FormID, "sales_count": n,
			}, nil
		}
		return nil, nil, err
	}
	// The row is gone, so the origin can no longer serve this form's card at all;
	// the edge may still hold the copy a crawler fetched while it was live. Asked
	// for unconditionally: a prefix that matches nothing costs nothing, and this is
	// the one call that destroys the form for good.
	edge := s.purgeLeftPublicArtefacts(ctx, acc, []string{f.Slug})
	return nil, withEdgeFields(map[string]any{
		"status": "purged", "form_id": in.FormID, "slug": f.Slug, "title": f.Title,
	}, edge), nil
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

// formPublicURLForAccount builds the canonical public URL for a form,
// preferring a verified custom domain over the workspace subdomain (issue #83).
// Returns "" when neither is resolvable.
func (s *Server) formPublicURLForAccount(ctx context.Context, accountID, slug string) string {
	host := s.publicHostForID(ctx, accountID)
	if host == "" {
		return ""
	}
	return "https://" + host + "/" + slug
}

// publicHostForID returns the host an account's public pages are served from: a
// VERIFIED custom domain first, then {workspace-slug}.jomform.com — the repo's
// one public-URL priority (see httpapi.PublicHandlers.formURLForAccount). ""
// when neither resolves.
//
// media_mcp.go uses it for the URL a CODE ASSET is advertised on: /media serves
// a code asset only when the request Host resolves to the key's own account
// (issue #379), so the canonical media host 404s the very URL the library used
// to hand out — see objstore.MerchantURL.
func (s *Server) publicHostForID(ctx context.Context, accountID string) string {
	if accountID == "" {
		return ""
	}
	if s.deps.Accounts != nil {
		if acc, aerr := s.deps.Accounts.ByID(ctx, accountID); aerr == nil && acc != nil &&
			acc.CustomDomain != "" && acc.CustomDomainVerified {
			return strings.TrimRight(acc.CustomDomain, "/")
		}
	}
	if slug := s.publicHostSlugFor(ctx, accountID); slug != "" {
		return slug + ".jomform.com"
	}
	return ""
}
