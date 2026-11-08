// MCP parity tools: workspaces (multi-tenant storefronts) + per-workspace settings.
package mcpsrv

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/installurl"
	"github.com/wzul/jomform/api/internal/urlguard"
)

// ── Workspace context resolution ─────────────────────────────────────────────────

// workspacesStore returns the workspace store, erroring when not wired.
func (s *Server) workspacesStore() (*dbstore.WorkspaceStore, error) {
	if s.deps.Workspaces == nil {
		return nil, errors.New("workspaces not available")
	}
	return s.deps.Workspaces, nil
}

// workspaceOwner resolves the user behind the MCP caller — the same identity the
// dashboard's sessionOwnerID resolves. Issue #307: OAuth tokens are bound to
// the user directly; API-key auth resolves the account's owner.
func (s *Server) workspaceOwner(ctx context.Context) (string, error) {
	id := IdentityFromContext(ctx)
	if id == nil {
		return "", fmt.Errorf("token missing or invalid")
	}
	if id.UserID != "" {
		return id.UserID, nil
	}
	a, err := s.deps.Accounts.ByID(ctx, id.AccountID)
	if err != nil {
		return "", fmt.Errorf("db: %w", err)
	}
	return a.OwnerUserID, nil
}

// accessibleWorkspace fetches a workspace by ID and verifies the caller is the owner
// or a member (issue #162). Returns the workspace + the caller's role.
func (s *Server) accessibleWorkspace(ctx context.Context, workspaceID string) (*dbstore.Workspace, string, error) {
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, "", err
	}
	owner, err := s.workspaceOwner(ctx)
	if err != nil {
		return nil, "", err
	}
	workspace, err := sp.ByID(ctx, workspaceID)
	if err != nil || workspace == nil {
		return nil, "", errors.New("workspace not found")
	}
	acc, ok, err := sp.AccessByID(ctx, workspaceID, owner)
	if err != nil || !ok {
		return nil, "", errors.New("workspace not found")
	}
	return workspace, acc.Role, nil
}

// requireWorkspaceWriteRole is the ONE gate for a per-workspace write tool. It
// enforces both independent axes:
//
//   - the API-key WRITE SCOPE (requireWriteScope): a readonly key is refused.
//     Issue #645 — the axis the role-only gate dropped.
//   - the manage-or-admin ROLE in the named workspace (issue #162).
//
// It replaces the `_, role, err := s.accessibleWorkspace(...)` + inline role
// comparison that six write handlers used to repeat: that copy-paste is how the
// scope half went missing from all of them at once, so the check lives here now
// and each handler names it.
func (s *Server) requireWorkspaceWriteRole(ctx context.Context, workspaceID string) (*dbstore.Workspace, error) {
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, err
	}
	workspace, role, err := s.accessibleWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if role != "" && role != dbstore.RoleManage && role != dbstore.RoleAdmin {
		return nil, errors.New(RoleManageRequiredLeadIn)
	}
	return workspace, nil
}

// ── list_workspaces ──────────────────────────────────────────────────────────────

// workspaceURL is the MCP side of the ONE rule (card t_48a93084): the address a
// workspace is published at, decided by the deployment flag. Same package
// (internal/installurl) the REST payloads call, so the two transports cannot
// print different addresses for the same workspace.
// cnameHost is the MCP side of installurl.CNAMEHost (card t_48a93084): an
// agent that relays "CNAME → custom.jomform.com" on a self-hosted install is
// telling the merchant to point their domain at a server they do not use.
func (s *Server) cnameHost() string {
	if s.deps == nil {
		return installurl.CNAMEHost("", false)
	}
	if host := installurl.CNAMEHost(s.deps.AppBaseURL, s.deps.SelfHosted); host != "" {
		return host
	}
	return "your installation's origin"
}

func (s *Server) workspaceURL(slug string) string {
	if s.deps == nil {
		return installurl.Workspace("", false, slug)
	}
	return installurl.Workspace(s.deps.AppBaseURL, s.deps.SelfHosted, slug)
}

type workspaceOut struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
	URL  string `json:"url"`
	Role string `json:"role,omitempty"`
}

func (s *Server) listWorkspaces(
	ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn,
) (*mcp.CallToolResult, *listOutput[workspaceOut], error) {
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	owner, err := s.workspaceOwner(ctx)
	if err != nil {
		return nil, nil, err
	}
	workspaces, err := sp.ByMember(ctx, owner)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]workspaceOut, 0, len(workspaces))
	for _, x := range workspaces {
		out = append(out, workspaceOut{
			ID: x.WorkspaceID, Name: x.Name, Slug: x.Slug,
			URL:  s.workspaceURL(x.Slug),
			Role: x.Role,
		})
	}
	return nil, newListOut(out), nil
}

// ── create_workspace ─────────────────────────────────────────────────────────────

type createWorkspaceIn struct {
	Name string `json:"name" jsonschema:"Workspace / storefront name"`
	Slug string `json:"slug" jsonschema:"Unique URL slug — 3-40 characters, a-z 0-9 dash only"`
}

// validateWorkspaceSlug delegates to the ONE shared rule (dbstore) so the MCP
// create path, the REST create/rename path and the rename path cannot disagree.
// It used to be a second copy of the rule pasted here — reserved-subdomain leg
// and all — which is exactly the drift the repo forbids for a rule shared by
// more than one write path (see urlguard: "ONE shared rule; never hand-roll
// another copy"). The reserved list still lives in ONE place (internal/urlguard);
// it is consumed by dbstore.ValidateWorkspaceSlug, which is now the rule.
func validateWorkspaceSlug(slug string) (string, error) {
	return dbstore.ValidateWorkspaceSlug(slug)
}

func (s *Server) createWorkspace(
	ctx context.Context, _ *mcp.CallToolRequest, in createWorkspaceIn,
) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: creating a workspace is a write. It has no target workspace to
	// take a role from yet, so the API-key write scope is the gate.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	owner, err := s.workspaceOwner(ctx)
	if err != nil {
		return nil, nil, err
	}
	// ONE shared name rule (dbstore.ValidateWorkspaceName), so the MCP create
	// path gives the SAME verdict as the REST create path and the rename path
	// for the same name. Checked BEFORE the emptiness test for the same reason
	// REST does: the rule returns "" alongside its error, so a too-long name
	// would otherwise be reported as a MISSING one.
	name, nerr := dbstore.ValidateWorkspaceName(in.Name)
	if nerr != nil {
		return nil, nil, nerr
	}
	if name == "" || strings.TrimSpace(in.Slug) == "" {
		return nil, nil, errors.New("name and slug are required")
	}
	slug, verr := validateWorkspaceSlug(in.Slug)
	if verr != nil {
		return nil, nil, verr
	}
	// The slug pre-check must read the NOT-FOUND case as "free", not as "taken".
	// dbstore.scanWorkspace returns a non-nil *Workspace even when the query
	// matched no row (it returns the struct it was filling, alongside
	// pgx.ErrNoRows), so testing `existing != nil` was ALWAYS true and
	// create_workspace answered "slug is already taken" for every slug — the tool
	// could never create anything. Treat only a real row as taken; any other
	// error is left to the INSERT below, which has the unique-constraint backstop.
	if existing, berr := sp.BySlug(ctx, slug); berr == nil && existing != nil {
		return nil, nil, errors.New("slug is already taken")
	} else if berr != nil && !errors.Is(berr, pgx.ErrNoRows) {
		return nil, nil, fmt.Errorf("db: %w", berr)
	}
	// Ask-first onboarding parity with the REST create path (httpapi
	// rest_workspaces.go): a caller creating their FIRST workspace reuses the
	// account signup already made instead of minting a second one. Workspaces.Create
	// always inserts a fresh account (issue #158), so without this the first
	// workspace created through MCP would leave the user owning two accounts —
	// one of them workspace-less and unreachable. Bounded exactly like REST: only
	// when the caller has no workspace yet, and only for an account that carries
	// no CONTENT (a legacy account holding forms/products/sales is a
	// data-migration decision, not a side effect of creating a workspace).
	//
	// The caller's own API key is NOT such content: an /mcp caller always has
	// one (auth resolves the Bearer through the api_keys table), so treating it
	// as a boundary made this branch unreachable on the one transport that needs
	// it and minted a second account with the credential stranded. See
	// dbstore.WorkspaceStore.SoleEmptyUnscopedAccount.
	var workspace *dbstore.Workspace
	if prior, _ := sp.ByMember(ctx, owner); len(prior) == 0 {
		if accID, aerr := sp.SoleEmptyUnscopedAccount(ctx, owner); aerr == nil && accID != "" {
			workspace, err = sp.CreateForAccount(ctx, owner, strings.TrimSpace(in.Name), slug, accID)
		}
	}
	if workspace == nil && err == nil {
		workspace, err = sp.Create(ctx, owner, name, slug)
	}
	if err != nil {
		// unique-constraint backstop (race between the pre-check and INSERT),
		// and the ALIAS reservation: a slug a workspace has renamed away from
		// still belongs to it (its printed links resolve there).
		var pgErr *pgconn.PgError
		if errors.Is(err, dbstore.ErrWorkspaceSlugTaken) ||
			(errors.As(err, &pgErr) && pgErr.Code == "23505") {
			return nil, nil, errors.New("slug is already taken")
		}
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{
		"status": "created", "id": workspace.ID, "name": workspace.Name, "slug": workspace.Slug,
		"url": s.workspaceURL(workspace.Slug),
	}, nil
}

// ── set_default_workspace (issue #164) ──────────────────────────────────────────

type setDefaultWorkspaceIn struct {
	WorkspaceID string `json:"workspace_id" jsonschema:"Workspace ID (from list_workspaces). Empty = remove the default."`
}

// setDefaultWorkspace sets (or clears, when workspace_id is empty) the user's default
// workspace — the workspace they land on after login, skipping the workspace-picker. The
// workspace must belong to the user (owner or member).
func (s *Server) setDefaultWorkspace(
	ctx context.Context, _ *mcp.CallToolRequest, in setDefaultWorkspaceIn,
) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: this is a user-level preference write (the user row, not the
	// account), so the API-key write scope is the gate.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	if s.deps.Users == nil {
		return nil, nil, errors.New("users not available")
	}
	owner, err := s.workspaceOwner(ctx)
	if err != nil {
		return nil, nil, err
	}
	if in.WorkspaceID == "" {
		if err := s.deps.Users.ClearDefaultWorkspace(ctx, owner); err != nil {
			return nil, nil, fmt.Errorf("db: %w", err)
		}
		return nil, map[string]string{"status": "cleared"}, nil
	}
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	workspace, err := sp.ByID(ctx, in.WorkspaceID)
	if err != nil || workspace == nil {
		return nil, nil, errors.New("workspace not found")
	}
	// The workspace must belong to the user (owner or member, issue #162).
	if _, ok, aerr := sp.AccessByID(ctx, in.WorkspaceID, owner); aerr != nil || !ok {
		return nil, nil, errors.New("workspace not found")
	}
	if err := s.deps.Users.SetDefaultWorkspace(ctx, owner, in.WorkspaceID); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "saved", "workspace_id": in.WorkspaceID}, nil
}

// mcpExemptKeyID returns the id of the API key authenticating this MCP call, or
// "" for a non-key identity (OAuth token). It is passed to the workspace
// emptiness guard as the ONE key exempt from the live-key blocker — see
// dbstore.emptyCheckArgs for why that exemption is required rather than
// convenient.
func mcpExemptKeyID(ctx context.Context) string {
	id := IdentityFromContext(ctx)
	if id == nil || id.APIKey == nil {
		return ""
	}
	return id.APIKey.ID
}

// ── delete_workspace / check_workspace_deletion ─────────────────────────────────
//
// Parity for DELETE /v1/workspaces/{id} and GET /v1/workspaces/{id}/deletion-check.
//
// OWNER ONLY. An admin member manages a workspace but does not own its public
// identity, its trading history, or the CHIP account holding its credentials;
// they leave through remove_workspace_member. The MCP path enforces the same
// rule as REST rather than relying on the manage-or-admin role gate, because
// "admin member" would otherwise be enough to destroy someone else's workspace.

// deleteWorkspaceIn confirms a permanent workspace delete.
//
// The confirmation is the workspace SLUG, not a boolean — the same level as
// revoke_api_key's confirm=true. This destroys the workspace, everything under
// it, and its CHIP account, and cannot be undone.
type deleteWorkspaceIn struct {
	WorkspaceID string `json:"workspace_id" jsonschema:"Workspace ID (from list_workspaces)"`
	ConfirmSlug string `json:"confirm_slug,omitempty" jsonschema:"The workspace's own slug, to confirm deletion. Wrong/absent = rejected."`
}

// deleteWorkspaceMCP permanently deletes a workspace the caller OWNS.
//
// It is not a thin wrapper over the store call: both guards the store enforces
// are surfaced as named refusals here, so an agent gets a reason it can act on
// (which categories block, or "you need at least one workspace") instead of an
// opaque failure.
func (s *Server) deleteWorkspaceMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in deleteWorkspaceIn,
) (*mcp.CallToolResult, map[string]any, error) {
	// Issue #645: the scope axis. Checked first so a readonly key cannot probe.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	owner, err := s.workspaceOwner(ctx)
	if err != nil {
		return nil, nil, err
	}
	workspace, err := sp.ByID(ctx, in.WorkspaceID)
	if err != nil || workspace == nil {
		return nil, nil, errors.New("workspace not found")
	}
	// Ownership, not membership: the caller must be the OWNER. A member who is
	// an admin is refused with the same message as REST.
	if workspace.Owner != owner {
		return nil, nil, errors.New("only the workspace owner can delete it")
	}
	// Confirmation: the caller must name the workspace it is destroying.
	if strings.TrimSpace(in.ConfirmSlug) != workspace.Slug {
		return nil, map[string]any{
			"status": "refused",
			"error": "type the workspace slug to confirm the deletion: " + workspace.Slug +
				". A workspace deletion cannot be undone.",
			"workspace_id": workspace.ID, "slug": workspace.Slug,
		}, nil
	}
	deleted, err := sp.Delete(ctx, workspace.ID, owner, mcpExemptKeyID(ctx))
	if err != nil {
		var de *dbstore.WorkspaceDeleteError
		if errors.As(err, &de) {
			if de.Reason == "last_workspace" {
				return nil, map[string]any{
					"status": "refused",
					"error": "you need at least one workspace — create a new workspace first " +
						"before deleting the last one.",
					"workspace_id": workspace.ID, "last_workspace": true,
				}, nil
			}
			return nil, map[string]any{
				"status":       "refused",
				"error":        "only the workspace owner can delete it",
				"workspace_id": workspace.ID,
			}, nil
		}
		if errors.Is(err, dbstore.ErrWorkspaceNotFound) {
			return nil, nil, errors.New("workspace not found")
		}
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]any{
		"status": "trashed", "workspace_id": workspace.ID, "slug": workspace.Slug,
		"name": workspace.Name, "deleted": deleted,
		"note": "workspace moved to the TRASH — ALL data stays (accounts, forms, products, sales, " +
			"API keys). Nothing is destroyed. The owner can restore it from Settings → " +
			"Workspace → Trash. PERMANENT deletion only happens through purge_workspace, and chat can never suggest it — it is an owner-only MCP tool.",
	}, nil
}

// checkWorkspaceDeletionMCP is the READ half of delete_workspace: it reports the
// SAME emptiness definition the delete enforces, plus whether this is the
// caller's last workspace, so an agent can state what must be cleared instead of
// discovering it by attempting the delete.
func (s *Server) checkWorkspaceDeletionMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in workspaceRefIn,
) (*mcp.CallToolResult, map[string]any, error) {
	workspace, role, err := s.accessibleWorkspace(ctx, in.WorkspaceID)
	if err != nil {
		return nil, nil, err
	}
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	owner, err := s.workspaceOwner(ctx)
	if err != nil {
		return nil, nil, err
	}
	_, blockers, err := sp.CountEmpty(ctx, workspace.ID, mcpExemptKeyID(ctx))
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]map[string]any, 0, len(blockers))
	for _, b := range blockers {
		out = append(out, map[string]any{"kind": b.Kind, "count": b.Count, "label": b.Label})
	}
	listed, err := sp.ByMember(ctx, owner)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	last := true
	for _, w := range listed {
		if w.WorkspaceID != workspace.ID {
			last = false
			break
		}
	}
	isOwner := workspace.Owner == owner
	// can_delete is OWNERSHIP + last-workspace ONLY.
	//
	// The content gate is gone from the write path (Wan's decision: a full
	// workspace goes to the bin, so there is nothing to protect), and this read
	// half must agree with it or the UI would disable a button for a reason the
	// API no longer enforces. `blockers` and `empty` are still reported: they are
	// what the dashboard shows BEFORE a purge, where content does matter.
	return nil, map[string]any{
		"workspace_id": workspace.ID, "name": workspace.Name, "slug": workspace.Slug,
		"role": role, "is_owner": isOwner,
		"last_workspace": last, "empty": len(out) == 0,
		"blockers":   out,
		"can_delete": isOwner && !last,
	}, nil
}

// ── list_trashed_workspaces / restore_workspace / purge_workspace ──────────────
//
// Parity for GET /v1/workspaces/trash, POST /v1/workspaces/{id}/restore and
// DELETE /v1/workspaces/{id}/purge — the bin half of the workspace lifecycle.
//
// All three are OWNER ONLY, for the same reason delete_workspace is: an admin
// member manages a workspace but does not own it, so they must not be able to
// bring it back or destroy it for good. Each names a refusal the caller can act
// on rather than surfacing a store error.

// purgeWorkspaceIn confirms a PERMANENT workspace delete.
//
// The confirmation is the workspace SLUG, matching delete_workspace and
// revoke_api_key: a call that cannot be undone should make the caller name what
// it is destroying.
type purgeWorkspaceIn struct {
	WorkspaceID string `json:"workspace_id" jsonschema:"Workspace ID in the trash (from list_trashed_workspaces)"`
	ConfirmSlug string `json:"confirm_slug,omitempty" jsonschema:"The workspace's own slug, to confirm the permanent delete. Wrong/absent = rejected."`
}

// listTrashedWorkspacesMCP returns the caller's OWN trashed workspaces.
//
// Owned only: only the owner may restore or purge, so listing a workspace the
// caller merely MEMBERS would offer a row they cannot act on.
func (s *Server) listTrashedWorkspacesMCP(
	ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn,
) (*mcp.CallToolResult, map[string]any, error) {
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	owner, err := s.workspaceOwner(ctx)
	if err != nil {
		return nil, nil, err
	}
	list, err := sp.ListTrashed(ctx, owner)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]map[string]any, 0, len(list))
	for _, w := range list {
		row := map[string]any{"workspace_id": w.ID, "name": w.Name, "slug": w.Slug}
		if w.DeletedAt != nil {
			row["deleted_at"] = w.DeletedAt.UTC().Format(time.RFC3339)
		}
		out = append(out, row)
	}
	return nil, map[string]any{"items": out, "total": len(out)}, nil
}

// restoreWorkspaceMCP brings a trashed workspace back.
func (s *Server) restoreWorkspaceMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in workspaceRefIn,
) (*mcp.CallToolResult, map[string]any, error) {
	// Issue #645: the scope axis. Checked first so a readonly key cannot probe.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	owner, err := s.workspaceOwner(ctx)
	if err != nil {
		return nil, nil, err
	}
	// Read through the TRASH reader: the live ByID deliberately hides binned
	// rows, so using it here would report "not found" for exactly the row this
	// tool exists to bring back.
	workspace, err := sp.TrashedByID(ctx, in.WorkspaceID)
	if err != nil || workspace == nil {
		return nil, nil, errors.New("workspace not found in the Trash")
	}
	if workspace.Owner != owner {
		return nil, map[string]any{
			"status": "refused", "error": "only the workspace owner can restore it",
			"workspace_id": workspace.ID,
		}, nil
	}
	if err := sp.Restore(ctx, workspace.ID, owner); err != nil {
		if errors.Is(err, dbstore.ErrWorkspaceSlugTaken) {
			return nil, map[string]any{
				"status": "refused",
				"error": "slug '" + workspace.Slug + "' was taken by another workspace while it was in the " +
					"Trash — change that workspace's slug first, then restore again. " +
					"Nothing is renamed automatically.",
				"workspace_id": workspace.ID, "slug": workspace.Slug, "slug_taken": true,
			}, nil
		}
		if errors.Is(err, dbstore.ErrWorkspaceNotFound) {
			return nil, nil, errors.New("workspace not found in the Trash")
		}
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]any{
		"status": "restored", "workspace_id": workspace.ID, "slug": workspace.Slug,
		"name": workspace.Name,
		"note": "workspace restored — its accounts, forms, products and sales are back as they were " +
			"(no row was touched while it was in the Trash).",
	}, nil
}

// purgeWorkspaceMCP permanently destroys a TRASHED workspace.
//
// It refuses a LIVE workspace: permanent destruction is only ever reachable on a
// row the user has already put in the bin, which is what makes the two-step —
// move it, look at it, then destroy it — the only path to data loss.
func (s *Server) purgeWorkspaceMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in purgeWorkspaceIn,
) (*mcp.CallToolResult, map[string]any, error) {
	// Issue #645: the scope axis. Checked first so a readonly key cannot probe.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	owner, err := s.workspaceOwner(ctx)
	if err != nil {
		return nil, nil, err
	}
	workspace, err := sp.TrashedByID(ctx, in.WorkspaceID)
	if err != nil || workspace == nil {
		// Deliberately the same refusal for "no such workspace" and "still
		// live": purging a live row is not a thing, and saying so precisely
		// would tell a caller whether a workspace they cannot touch exists.
		return nil, nil, errors.New("workspace not found in the Trash — " +
			"send it to the Trash first with delete_workspace")
	}
	if workspace.Owner != owner {
		return nil, map[string]any{
			"status": "refused", "error": "only the workspace owner can permanently delete it",
			"workspace_id": workspace.ID,
		}, nil
	}
	if strings.TrimSpace(in.ConfirmSlug) != workspace.Slug {
		return nil, map[string]any{
			"status": "refused",
			"error": "type the workspace slug to confirm the permanent deletion: " + workspace.Slug +
				". A permanent delete cannot be undone.",
			"workspace_id": workspace.ID, "slug": workspace.Slug,
		}, nil
	}
	deleted, err := sp.Purge(ctx, workspace.ID, owner)
	if err != nil {
		if errors.Is(err, dbstore.ErrWorkspaceHasSales) {
			return nil, map[string]any{
				"status": "refused",
				"error": "this workspace has sales, so it cannot be permanently deleted — " +
					"sales are never destroyed or orphaned. Leave it in the Trash " +
					"(restore it when needed) or settle its sales first.",
				"workspace_id": workspace.ID, "has_sales": true,
			}, nil
		}
		if errors.Is(err, dbstore.ErrWorkspaceNotFound) {
			return nil, nil, errors.New("workspace not found in the Trash")
		}
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]any{
		"status": "purged", "workspace_id": workspace.ID, "slug": workspace.Slug,
		"name": workspace.Name, "deleted": deleted,
		"note": "workspace PERMANENTLY deleted. Cannot be undone. The CHIP account that ALREADY EXISTED " +
			"before this workspace (the signup account used when it was created) is not " +
			"destroyed — it is released back as an account without a workspace; see " +
			"deleted.accounts_unlinked.",
	}, nil
}

// ── update_workspace ─────────────────────────────────────────────────────────
//
// MCP parity for PATCH /v1/workspaces/{id}: rename a workspace's name and/or its
// public slug.
//
// WHY THE SLUG NEEDS confirm=true (the revoke_api_key precedent, Wan's ask). The
// slug is the tenant's PUBLIC identity — the host on printed QR stands, in
// iframe snippets, in affiliate links. A rename is not destructive (the old slug
// is kept as an alias and keeps redirecting), but it moves every address the
// merchant has already handed out, and an agent should never do that because it
// inferred it. Renaming the NAME needs no confirmation: it is a label.
type updateWorkspaceIn struct {
	WorkspaceID string `json:"workspace_id" jsonschema:"Workspace ID (from list_workspaces)"`
	Name        string `json:"name,omitempty" jsonschema:"New workspace name (empty = keep). The name is an internal/dashboard label."`
	Slug        string `json:"slug,omitempty" jsonschema:"New slug/public address (empty = keep): 3-40 characters a-z 0-9 dash, not the suffix 'jomform'. It becomes https://<slug>.jomform.com. Only the workspace OWNER can change it, and it requires confirm=true. The OLD slug is kept as an alias and keeps working (permanent redirect to the new slug), so links/QRs already shared do not break."`
	Confirm     bool   `json:"confirm,omitempty" jsonschema:"Set true to confirm the SLUG change. Required when a slug is provided."`
}

func (s *Server) updateWorkspace(
	ctx context.Context, _ *mcp.CallToolRequest, in updateWorkspaceIn,
) (*mcp.CallToolResult, map[string]any, error) {
	// Issue #645 + issue #162: the scope axis and the manage-or-admin role. This
	// is a named per-workspace write gate, exactly like the settings tools.
	workspace, err := s.requireWorkspaceWriteRole(ctx, in.WorkspaceID)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(in.Name) == "" && strings.TrimSpace(in.Slug) == "" {
		return nil, nil, errors.New("name or slug is required")
	}
	owner, err := s.workspaceOwner(ctx)
	if err != nil {
		return nil, nil, err
	}
	isOwner := workspace.Owner == owner
	if strings.TrimSpace(in.Slug) != "" {
		// Confirmation first (before any validation), because the point of the
		// gate is that an agent names what it is moving.
		if !in.Confirm {
			return nil, map[string]any{
				"status":       "refused",
				"error":        "changing the slug moves the workspace's PUBLIC address (https://" + workspace.Slug + ".jomform.com) — every link, iframe and QR already in the wild moves to the new address. Set confirm=true to confirm. The old address keeps working as an alias (permanent redirect).",
				"workspace_id": workspace.ID, "slug": workspace.Slug,
			}, nil
		}
		// Ownership, not mere membership: the same rule REST enforces. Stated
		// here as its own refusal so an agent is told why, rather than getting a
		// message about roles it does hold.
		if !isOwner {
			return nil, map[string]any{
				"status":       "refused",
				"error":        "only the workspace OWNER can change the slug — the slug is the workspace's public address (links, iframe, QR). The name alone can be changed by manage/admin members.",
				"workspace_id": workspace.ID, "slug": workspace.Slug, "is_owner": false,
			}, nil
		}
	}

	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	res, err := sp.Rename(ctx, dbstore.WorkspaceRenameInput{
		WorkspaceID:   workspace.ID,
		Name:          in.Name,
		Slug:          in.Slug,
		CanChangeSlug: isOwner,
	})
	if err != nil {
		switch {
		case errors.Is(err, dbstore.ErrWorkspaceSlugTaken):
			return nil, nil, errors.New("slug is already taken — that slug belongs to another workspace (live, or reserved as an old alias)")
		case errors.Is(err, dbstore.ErrWorkspaceSlugNotOwned):
			return nil, nil, errors.New("only the workspace owner can change the slug")
		case errors.Is(err, dbstore.ErrWorkspaceNameLength):
			return nil, nil, err
		case errors.Is(err, dbstore.ErrWorkspaceSlugLength),
			errors.Is(err, dbstore.ErrWorkspaceSlugChars),
			errors.Is(err, dbstore.ErrWorkspaceSlugReserved):
			return nil, nil, err
		case errors.Is(err, dbstore.ErrWorkspaceSlugReservedSubdomain):
			// The reserved LIST — a POLICY refusal about a well-formed name JomForm
			// owns, not the caller's malformed input. Two things follow from that:
			//
			//  1. It must NOT fall through to `fmt.Errorf("db: %w", err)` below.
			//     Before this arm existed the refusal was an anonymous error that
			//     matched no case, so the agent read `db: "<label>" is reserved for
			//     JomForm: …` — indistinguishable from a storage fault, for input
			//     the agent can simply change (t_758bb125).
			//  2. It answers as DATA, carrying the same machine code REST sends,
			//     because the remedy is an instruction ("choose a different
			//     address") and #694's rule is that the caller decides its sentence
			//     from the code rather than from prose. The shape legs above stay
			//     transport errors: their remedy is to retype the value, and no
			//     caller needs a token to act on "slug 3-40 characters".
			return nil, map[string]any{
				"status":       "refused",
				"error":        err.Error(),
				"code":         urlguard.ReservedSubdomainCode,
				"workspace_id": workspace.ID,
				"slug":         workspace.Slug,
			}, nil
		case errors.Is(err, dbstore.ErrWorkspaceNoChange):
			return nil, nil, errors.New("no change — the name and slug are the same as now")
		case errors.Is(err, dbstore.ErrWorkspaceNotFound):
			return nil, nil, errors.New("workspace not found")
		}
		return nil, nil, fmt.Errorf("db: %w", err)
	}

	url := s.workspaceURL(res.Workspace.Slug)
	out := map[string]any{
		"status": "updated", "workspace_id": res.Workspace.ID,
		"name": res.Workspace.Name, "slug": res.Workspace.Slug,
		"url":          url,
		"name_changed": res.NameChanged, "slug_changed": res.SlugChanged,
		"previous_slug": res.OldSlug,
	}
	// The promise the merchant needs to hear: the addresses already handed out
	// still work. Stated explicitly so an agent relays it.
	//
	// Only when the address actually MOVED. One self-hosted origin serves every
	// workspace (card t_48a93084), so a slug rename leaves the address alone —
	// telling an agent that a previous_url redirects, and printing the same
	// string twice, would be false.
	if prev := s.workspaceURL(res.OldSlug); res.SlugChanged && prev != "" && prev != url {
		out["previous_url"] = prev
		out["previous_url_redirects"] = true
	}
	return nil, out, nil
}

// ── update_profile_phone (issue #181) ───────────────────────────────────────

type updateProfilePhoneIn struct {
	Phone string `json:"phone" jsonschema:"Optional phone number (empty = remove). Loose format: digits, +, spaces, 8-15 digits."`
}

// updateProfilePhone sets (or clears, when phone is empty) the account owner's
// optional phone number — MCP parity for the dashboard Profile → Identity tab.
func (s *Server) updateProfilePhone(
	ctx context.Context, _ *mcp.CallToolRequest, in updateProfilePhoneIn,
) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: the profile writes edit the USER row (no account role applies),
	// so the API-key write scope is the gate.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	if s.deps.Users == nil {
		return nil, nil, errors.New("users not available")
	}
	owner, err := s.workspaceOwner(ctx)
	if err != nil {
		return nil, nil, err
	}
	phone := strings.TrimSpace(in.Phone)
	if phone != "" {
		digits := 0
		for _, c := range phone {
			switch {
			case c >= '0' && c <= '9':
				digits++
			case c == '+' || c == ' ' || c == '-' || c == '(' || c == ')':
			default:
				return nil, nil, errors.New("invalid phone number")
			}
		}
		if digits < 8 || digits > 15 {
			return nil, nil, errors.New("invalid phone number")
		}
	}
	if err := s.deps.Users.UpdatePhone(ctx, owner, phone); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "saved", "phone": phone}, nil
}

// ── update_profile_timezone (issue #191) ───────────────────────────────────

type updateProfileTimezoneIn struct {
	Timezone string `json:"timezone" jsonschema:"Optional IANA timezone (empty = Auto, follow the browser/PC). Example: Asia/Kuala_Lumpur, UTC."`
}

// updateProfileTimezone sets (or clears, when timezone is empty) the account
// owner's preferred IANA timezone — MCP parity for the dashboard Profile →
// Identity tab. Empty = Auto (follow the browser/PC default).
func (s *Server) updateProfileTimezone(
	ctx context.Context, _ *mcp.CallToolRequest, in updateProfileTimezoneIn,
) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: user-row write — API-key write scope is the gate.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	if s.deps.Users == nil {
		return nil, nil, errors.New("users not available")
	}
	owner, err := s.workspaceOwner(ctx)
	if err != nil {
		return nil, nil, err
	}
	tz := strings.TrimSpace(in.Timezone)
	if tz != "" {
		if _, lerr := time.LoadLocation(tz); lerr != nil {
			return nil, nil, errors.New("invalid timezone")
		}
	}
	if err := s.deps.Users.UpdateTimezone(ctx, owner, tz); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "saved", "timezone": tz}, nil
}

// ── update_profile_language (language placement) ──────────────────────────

type updateProfileLanguageIn struct {
	Lang string `json:"lang" jsonschema:"Your preferred dashboard UI language: en, ms, zh, ta or ar. Empty = remove the setting (falls back to the browser, then English). This is the DASHBOARD language only — the public form language is set per-form/per-workspace."`
}

// updateProfileLanguage sets (or clears, when lang is empty) the account owner's
// preferred DASHBOARD UI language — MCP parity for the dashboard Profile →
// Identity language picker. This is per-PROFILE (per-user): the setting follows
// the person into every browser. It is deliberately NOT the workspace's
// buyer-facing default_lang, which is set via set_workspace_settings.
func (s *Server) updateProfileLanguage(
	ctx context.Context, _ *mcp.CallToolRequest, in updateProfileLanguageIn,
) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: this edits the USER row (the caller's own profile), not the
	// account, so no workspace role applies — the API-key write scope is the
	// gate, exactly as for the neighbouring profile writes.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	if s.deps.Users == nil {
		return nil, nil, errors.New("users not available")
	}
	owner, err := s.workspaceOwner(ctx)
	if err != nil {
		return nil, nil, err
	}
	lang := strings.TrimSpace(in.Lang)
	if !dbstore.ValidProfileLang(lang) {
		return nil, nil, errors.New("language not supported (en, ms, zh, ta, ar)")
	}
	if err := s.deps.Users.UpdateLang(ctx, owner, lang); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "saved", "lang": lang}, nil
}

// ── get_workspace_settings ───────────────────────────────────────────────────────

type workspaceRefIn struct {
	WorkspaceID string `json:"workspace_id" jsonschema:"Workspace ID (from list_workspaces)"`
}

func (s *Server) getWorkspaceSettings(
	ctx context.Context, _ *mcp.CallToolRequest, in workspaceRefIn,
) (*mcp.CallToolResult, *dbstore.WorkspaceSettings, error) {
	if _, _, err := s.accessibleWorkspace(ctx, in.WorkspaceID); err != nil {
		return nil, nil, err
	}
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	st, err := sp.Settings(ctx, in.WorkspaceID)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, st, nil
}

// ── set_workspace_settings ───────────────────────────────────────────────────────

type setWorkspaceSettingsIn struct {
	WorkspaceID    string  `json:"workspace_id" jsonschema:"Workspace ID (from list_workspaces)"`
	FooterText     *string `json:"footer_text,omitempty" jsonschema:"Storefront footer text (omit = keep, \"\" = empty it)"`
	CompanyReg     *string `json:"company_reg,omitempty" jsonschema:"Company registration number (omit = keep)"`
	PrivacyURL     *string `json:"privacy_url,omitempty" jsonschema:"Privacy policy URL — must be https:// (omit = keep)"`
	TermsURL       *string `json:"terms_url,omitempty" jsonschema:"Terms & conditions URL — must be https:// (omit = keep)"`
	RefundURL      *string `json:"refund_url,omitempty" jsonschema:"Refund policy URL — must be https:// (omit = keep)"`
	ContactPhone   *string `json:"contact_phone,omitempty" jsonschema:"Business phone number for the footer (omit = keep)"`
	ContactAddress *string `json:"contact_address,omitempty" jsonschema:"Business address for the footer (omit = keep)"`
	BusinessName   *string `json:"business_name,omitempty" jsonschema:"Business name for the footer (omit = keep)"`
	MenuJSON       *string `json:"menu_json,omitempty" jsonschema:"Navigation menu JSON, e.g. [{\"label\":\"Home\",\"url\":\"/\"}] (omit = keep)"`
	// Issue #276: per-workspace SEO indexing toggle. Omitted (nil) = keep current.
	Indexing *bool `json:"indexing,omitempty" jsonschema:"Allow search engines/AI to index this workspace's storefront (omit = keep)"`
	// Issue #447: per-workspace realtime notification sound toggle. Omitted
	// (nil) = keep current; explicit true/false sets it.
	SoundEnabled *bool `json:"sound_enabled,omitempty" jsonschema:"Play a beep sound on the dashboard when a new sale/response arrives (omit = keep)"`
	// Per-workspace AI spam-judge toggle. Omitted (nil) = keep
	// current; explicit true/false sets it. Default true.
	SpamJudgeEnabled *bool `json:"spam_judge_enabled,omitempty" jsonschema:"Enable AI spam detection (clef decision model) for this workspace — true = on (default), false = OFF: no AI job is sent at all and is_spam is no longer set, so /spam stays empty (omit = keep)"`
	// Language placement: the workspace's buyer-facing default language, used by
	// PUBLIC forms when the form itself sets no config.lang (form-level wins).
	// Omitted (nil) = keep current. This is the STOREFRONT language, not the
	// dashboard UI language (that one is per-profile, update_profile_language).
	DefaultLang *string `json:"default_lang,omitempty" jsonschema:"Default language of PUBLIC forms for this workspace when the form does not set its own language: en, ms, zh, ta or ar. This is the buyer's language, not your dashboard language (omit = keep)."`
}

func (s *Server) setWorkspaceSettings(
	ctx context.Context, _ *mcp.CallToolRequest, in setWorkspaceSettingsIn,
) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: write gate = API-key write scope AND manage-or-admin role.
	if _, err := s.requireWorkspaceWriteRole(ctx, in.WorkspaceID); err != nil {
		return nil, nil, err
	}
	// URL fields are https-only (issue #501): ONE shared rule from
	// internal/urlguard, the same validator the REST handlers import — so the
	// policy cannot drift between the two write layers again.
	// A nil (omitted) field keeps its stored value and is never rejected.
	for _, f := range []struct {
		name string
		u    *string
	}{
		{"privacy_url", in.PrivacyURL},
		{"terms_url", in.TermsURL},
		{"refund_url", in.RefundURL},
	} {
		if f.u != nil && !urlguard.Valid(*f.u) {
			return nil, nil, errors.New(urlguard.InvalidMessage(f.name))
		}
	}
	if in.MenuJSON != nil && *in.MenuJSON != "" && !jsonValid(*in.MenuJSON) {
		return nil, nil, errors.New("menu_json is not valid JSON")
	}
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	// Read-modify-write: omit a field = keep its current value.
	st, err := sp.Settings(ctx, in.WorkspaceID)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	if in.FooterText != nil {
		st.FooterText = *in.FooterText
	}
	if in.CompanyReg != nil {
		st.CompanyReg = *in.CompanyReg
	}
	if in.PrivacyURL != nil {
		st.PrivacyURL = *in.PrivacyURL
	}
	if in.TermsURL != nil {
		st.TermsURL = *in.TermsURL
	}
	if in.RefundURL != nil {
		st.RefundURL = *in.RefundURL
	}
	if in.ContactPhone != nil {
		st.ContactPhone = *in.ContactPhone
	}
	if in.ContactAddress != nil {
		st.ContactAddress = *in.ContactAddress
	}
	if in.BusinessName != nil {
		st.BusinessName = *in.BusinessName
	}
	if in.MenuJSON != nil {
		st.MenuJSON = *in.MenuJSON
	}
	if in.Indexing != nil {
		st.Indexing = *in.Indexing
	}
	if in.SoundEnabled != nil {
		st.SoundEnabled = *in.SoundEnabled
	}
	if in.SpamJudgeEnabled != nil {
		st.SpamJudgeEnabled = *in.SpamJudgeEnabled
	}
	// Language placement: the workspace's buyer-facing default language. An
	// unsupported code is rejected rather than silently stored, so a typo cannot
	// leave the storefront on an unrenderable language.
	if in.DefaultLang != nil {
		if !dbstore.ValidProfileLang(*in.DefaultLang) || strings.TrimSpace(*in.DefaultLang) == "" {
			return nil, nil, errors.New("language not supported (en, ms, zh, ta, ar)")
		}
		st.DefaultLang = strings.TrimSpace(*in.DefaultLang)
	}
	if strings.TrimSpace(st.MenuJSON) == "" {
		st.MenuJSON = "[]"
	}
	if err := sp.SaveSettings(ctx, st); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "saved", "workspace_id": in.WorkspaceID}, nil
}

// ── get_workspace_landing_page / set_workspace_landing_page (issue #360) ─────────────

type workspaceLandingRefIn struct {
	WorkspaceID string `json:"workspace_id" jsonschema:"Workspace ID (from list_workspaces)"`
}

func (s *Server) getWorkspaceLandingPage(
	ctx context.Context, _ *mcp.CallToolRequest, in workspaceLandingRefIn,
) (*mcp.CallToolResult, *dbstore.LandingPage, error) {
	if _, _, err := s.accessibleWorkspace(ctx, in.WorkspaceID); err != nil {
		return nil, nil, err
	}
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	st, err := sp.Settings(ctx, in.WorkspaceID)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	if st.LandingPage == nil {
		st.LandingPage = &dbstore.LandingPage{}
	}
	return nil, st.LandingPage, nil
}

type setWorkspaceLandingPageIn struct {
	WorkspaceID     string                 `json:"workspace_id" jsonschema:"Workspace ID (from list_workspaces)"`
	Enabled         bool                   `json:"enabled" jsonschema:"true = show the landing page; false = fall back to the forms list"`
	Hero            string                 `json:"hero,omitempty" jsonschema:"Main heading (hero)"`
	Subtitle        string                 `json:"subtitle,omitempty" jsonschema:"Sub-heading under the hero"`
	Body            string                 `json:"body,omitempty" jsonschema:"Landing page body text"`
	ImageURL        string                 `json:"image_url,omitempty" jsonschema:"Banner image URL (must be https://)"`
	PrimaryColor    string                 `json:"primary_color,omitempty" jsonschema:"Theme accent color, e.g. #7c3aed"`
	BackgroundColor string                 `json:"background_color,omitempty" jsonschema:"Background color, e.g. #ffffff"`
	LogoURL         string                 `json:"logo_url,omitempty" jsonschema:"Brand logo URL (must be https://)"`
	CTAs            []dbstore.LandingCTA   `json:"ctas,omitempty" jsonschema:"CTA button links to a published form, e.g. [{\"label\":\"RSVP\",\"form_slug\":\"rsvp\"}]"`
	Forms           []dbstore.LandingEmbed `json:"forms,omitempty" jsonschema:"Published form is displayed inline in the landing page, e.g. [{\"label\":\"RSVP\",\"form_slug\":\"rsvp\"}]"`
}

func (s *Server) setWorkspaceLandingPage(
	ctx context.Context, _ *mcp.CallToolRequest, in setWorkspaceLandingPageIn,
) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: write gate = API-key write scope AND manage-or-admin role.
	if _, err := s.requireWorkspaceWriteRole(ctx, in.WorkspaceID); err != nil {
		return nil, nil, err
	}
	// Media URLs are https-only (issue #501), through the ONE shared rule in
	// internal/urlguard (same validator as the REST landing-page path). An
	// empty value means "no image" and stays allowed.
	for _, f := range []struct{ name, u string }{
		{"image_url", in.ImageURL},
		{"logo_url", in.LogoURL},
	} {
		if !urlguard.Valid(f.u) {
			return nil, nil, errors.New(urlguard.InvalidMessage(f.name))
		}
	}
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	st, err := sp.Settings(ctx, in.WorkspaceID)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	lp := &dbstore.LandingPage{
		Enabled:         in.Enabled,
		Hero:            in.Hero,
		Subtitle:        in.Subtitle,
		Body:            in.Body,
		ImageURL:        in.ImageURL,
		PrimaryColor:    in.PrimaryColor,
		BackgroundColor: in.BackgroundColor,
		LogoURL:         in.LogoURL,
		CTAs:            in.CTAs,
		Forms:           in.Forms,
	}
	if lp.CTAs == nil {
		lp.CTAs = []dbstore.LandingCTA{}
	}
	if lp.Forms == nil {
		lp.Forms = []dbstore.LandingEmbed{}
	}
	st.LandingPage = lp
	if err := sp.SaveSettings(ctx, st); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "saved", "workspace_id": in.WorkspaceID}, nil
}
