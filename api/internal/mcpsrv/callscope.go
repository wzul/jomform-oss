// Per-call workspace scope for MCP tools (issue #307).
//
// A connected OAuth agent is bound to the USER, not to a single account. Each
// MCP tool call may optionally carry a `workspace_id` (or `account_id`) argument
// naming which of the user's workspaces to operate on. This file extracts that
// optional argument from the raw tool Arguments (the typed input structs are
// left untouched — JSON schema allows additional properties by default) and
// stashes a resolved scope on the request so `accountAndScope` /
// `requireWriteRole` / `requireAdminRole` / `workspaceOwner` honour it and never
// exceed the user's role in the chosen workspace.
package mcpsrv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// scopeCtxKey is the context key for the per-call resolved scope.
type scopeCtxKey struct{}

// CallScope is the resolved, per-call target: the account set the tool may
// act on (every account of the user's chosen/accessible workspaces) plus the
// caller's role within them.
type CallScope struct {
	// Accounts holds every account the caller may operate on for this call.
	// With an explicit workspace_id it is that workspace's accounts; otherwise it is
	// every account across the user's accessible workspaces (issue #307).
	Accounts []string
	// WorkspaceID is the explicitly requested workspace ("" when none).
	WorkspaceID string
	// Role is the caller's role in the requested/implied workspace ("" when the
	// caller is the owner / unscoped / API-key — owners always have admin).
	Role string
	// ExplicitAccountID is non-empty when the call named account_id directly.
	ExplicitAccountID string
}

// withCallScope stores the resolved per-call scope on ctx.
func withCallScope(ctx context.Context, s *CallScope) context.Context {
	return context.WithValue(ctx, scopeCtxKey{}, s)
}

// callScopeFrom returns the per-call scope, or nil when none was set.
func callScopeFrom(ctx context.Context) *CallScope {
	if s, ok := ctx.Value(scopeCtxKey{}).(*CallScope); ok {
		return s
	}
	return nil
}

// scopeFromArgs reads the optional `workspace_id` / `account_id` fields out of the
// raw MCP tool arguments (json.RawMessage). Both are reserved helper fields;
// they are never part of the typed tool input. Returns "" when absent.
func scopeFromArgs(raw json.RawMessage) (workspaceID, accountID string) {
	if len(raw) == 0 {
		return "", ""
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", ""
	}
	if v, ok := m["workspace_id"].(string); ok {
		workspaceID = v
	}
	if v, ok := m["account_id"].(string); ok {
		accountID = v
	}
	return workspaceID, accountID
}

// resolveScope resolves the per-call scope from the raw arguments against the
// authenticated principal. It verifies a named workspace/account belongs to the
// user (owner or member, issue #162). Unscoped calls resolve to every account
// across the user's accessible workspaces (issue #307). Returns an error when the
// named scope is inaccessible.
func (s *Server) resolveScope(ctx context.Context, raw json.RawMessage) (*CallScope, error) {
	workspaceID, accountID := scopeFromArgs(raw)
	if accountID != "" && workspaceID != "" {
		return nil, errors.New("hanya satu daripada workspace_id atau account_id dibenarkan")
	}
	userID := s.userIDFor(ctx)
	if userID == "" {
		return nil, errors.New("token tiada atau tidak sah")
	}

	// API-key callers have no bound user (id.UserID == ""). Issue #307 scopes
	// OAuth agents to all the user's workspaces; an API key must stay bound to the
	// single account it authenticates (legacy behaviour — do not widen it).
	if id := IdentityFromContext(ctx); id != nil && id.UserID == "" {
		if id.AccountID != "" {
			return &CallScope{Accounts: []string{id.AccountID}, ExplicitAccountID: id.AccountID}, nil
		}
		return &CallScope{Accounts: []string{}}, nil
	}

	if accountID != "" {
		ok, err := s.accountAccessible(ctx, userID, accountID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errors.New("akaun tidak dijumpai")
		}
		return &CallScope{Accounts: []string{accountID}, ExplicitAccountID: accountID}, nil
	}

	if workspaceID != "" {
		if s.deps.Workspaces == nil {
			return nil, errors.New("workspaces tidak tersedia")
		}
		acc, ok, err := s.deps.Workspaces.AccessByID(ctx, workspaceID, userID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errors.New("workspace tidak dijumpai")
		}
		return &CallScope{Accounts: acc.Accounts, WorkspaceID: workspaceID, Role: acc.Role}, nil
	}

	// Unscoped: every account across the user's accessible workspaces.
	accounts, err := s.userAccessibleAccounts(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &CallScope{Accounts: accounts}, nil
}

// userIDFor resolves the authenticated user id: the identity's UserID when it
// is bound (OAuth tokens, issue #307) or the account owner for API-key callers.
func (s *Server) userIDFor(ctx context.Context) string {
	id := IdentityFromContext(ctx)
	if id == nil {
		return ""
	}
	if id.UserID != "" {
		return id.UserID
	}
	if id.AccountID == "" || s.deps == nil || s.deps.Accounts == nil {
		return ""
	}
	a, err := s.deps.Accounts.ByID(ctx, id.AccountID)
	if err != nil {
		return ""
	}
	return a.OwnerUserID
}

// accountScopes returns the full account set this call may operate on
// (issue #307): every account across the user's accessible workspaces, or the
// explicitly scoped subset when a per-call scope is present. Scope write gate
// (if write) is also checked.
func (s *Server) accountScopes(ctx context.Context, write bool) ([]string, error) {
	id := IdentityFromContext(ctx)
	if id == nil {
		return nil, errors.New("token tiada atau tidak sah")
	}
	if write && !dbstore.ScopeHas(id.Scopes, "readwrite") {
		return nil, errors.New("scope tidak mencukupi (perlu readwrite)")
	}
	if cs := callScopeFrom(ctx); cs != nil && len(cs.Accounts) > 0 {
		return cs.Accounts, nil
	}
	if id.UserID == "" {
		// API-key: single account.
		return []string{id.AccountID}, nil
	}
	return s.userAccessibleAccounts(ctx, id.UserID)
}

// ownedOrScopedAccounts returns every account the call may act on: explicitly
// scoped set, or all the user's accessible accounts (issue #307). For
// API-key callers this is just their own account.
func (s *Server) ownedOrScopedAccounts(ctx context.Context) ([]string, error) {
	if cs := callScopeFrom(ctx); cs != nil && len(cs.Accounts) > 0 {
		return cs.Accounts, nil
	}
	return s.accountScopes(ctx, false)
}

// isStoredNotFound reports whether err is a store "absent in this account"
// sentinel (not owned here) rather than a real failure. Both entity sentinels
// belong here: resolveEntityAccount walks the caller's accounts, so a
// not-owned answer for one account must let the walk continue to the next
// (ErrFormNotFound and ErrProductNotFound are the two lookups it drives).
func isStoredNotFound(err error) bool {
	return errors.Is(err, dbstore.ErrFormNotFound) ||
		errors.Is(err, dbstore.ErrProductNotFound) ||
		errors.Is(err, dbstore.ErrAuthCodeInvalid) ||
		errors.Is(err, dbstore.ErrAccessTokenInvalid) ||
		errors.Is(err, dbstore.ErrRefreshInvalid)
}

// errNotOwned is returned by owningAccount when an entity is not owned by any
// accessible account.
var errNotOwned = errors.New("entiti tidak dijumpai")

// resolveEntityAccount finds the account (within the call's scope) that owns
// an entity, using a lookup that returns nil when the account owns it and a
// store "not found" error otherwise. It returns the owning account id or
// errNotOwned.
func (s *Server) resolveEntityAccount(ctx context.Context, lookup func(acc string) error) (string, error) {
	accs, err := s.ownedOrScopedAccounts(ctx)
	if err != nil {
		return "", err
	}
	for _, acc := range accs {
		if err := lookup(acc); err == nil {
			return acc, nil
		} else if !isStoredNotFound(err) {
			return "", err
		}
	}
	return "", errNotOwned
}

// resolveFormAccount returns the account owning formID within the call's scope.
func (s *Server) resolveFormAccount(ctx context.Context, formID string) (string, error) {
	if s.deps.Forms == nil {
		return "", errors.New("forms tidak tersedia")
	}
	return s.resolveEntityAccount(ctx, func(acc string) error {
		_, err := s.deps.Forms.GetByID(ctx, acc, formID)
		return err
	})
}

// resolveProductAccount returns the account owning productID within the
// call's scope.
func (s *Server) resolveProductAccount(ctx context.Context, productID string) (string, error) {
	if s.deps.Products == nil {
		return "", errors.New("products tidak tersedia")
	}
	return s.resolveEntityAccount(ctx, func(acc string) error {
		if _, err := s.deps.Products.Get(ctx, acc, productID); err != nil {
			return err
		}
		return nil
	})
}

// requireFormWrite returns the account owning formID after enforcing the
// manage-or-admin gate for that form's workspace (issue #162/#307). This replaces
// the old single-account requireWriteRole for per-form write tools.
func (s *Server) requireFormWrite(ctx context.Context, formID string) (string, error) {
	acc, err := s.resolveFormAccount(ctx, formID)
	if err != nil {
		return "", err
	}
	if err := s.requireWriteRoleIn(ctx, acc); err != nil {
		return "", err
	}
	return acc, nil
}

// resolveTrashedFormAccount is resolveFormAccount's counterpart for a form in the
// TRASH. It must be separate: resolveFormAccount's lookup is Forms.GetByID, whose
// select filters `deleted_at IS NULL`, so a trashed form is invisible to it and
// every trash operation would fail as "not owned" — including restore, whose
// entire purpose is reaching a trashed row.
func (s *Server) resolveTrashedFormAccount(ctx context.Context, formID string) (string, error) {
	if s.deps.Forms == nil {
		return "", errors.New("forms tidak tersedia")
	}
	return s.resolveEntityAccount(ctx, func(acc string) error {
		_, err := s.deps.Forms.GetTrashed(ctx, acc, formID)
		return err
	})
}

// requireTrashedFormWrite is the gate for restore / purge / trash reads: it
// resolves the owning account through the TRASH-aware lookup and then enforces
// both write axes (API-key scope + manage-or-admin role in that workspace).
//
// The SCOPE axis is checked FIRST, before the entity lookup. requireWriteRoleIn
// also checks it, but only after the resolve — and the resolve's failure mode
// ("entiti tidak dijumpai") differs from a refusal, so a readonly key could tell
// a real form id from a fake one by the wording of the error. Naming the gate
// before any lookup removes that probe. Same reason the REST purge handler
// checks the write role before reading the row.
func (s *Server) requireTrashedFormWrite(ctx context.Context, formID string) (string, error) {
	if err := s.requireWriteScope(ctx); err != nil {
		return "", err
	}
	acc, err := s.resolveTrashedFormAccount(ctx, formID)
	if err != nil {
		return "", err
	}
	if err := s.requireWriteRoleIn(ctx, acc); err != nil {
		return "", err
	}
	return acc, nil
}

// requireProductWrite returns the account owning productID after enforcing the
// manage-or-admin gate for that product's workspace.
func (s *Server) requireProductWrite(ctx context.Context, productID string) (string, error) {
	acc, err := s.resolveProductAccount(ctx, productID)
	if err != nil {
		return "", err
	}
	if err := s.requireWriteRoleIn(ctx, acc); err != nil {
		return "", err
	}
	return acc, nil
}

// resolveMediaAccount returns the account owning mediaID within the call's
// scope, through the LIVE media read (ByIDAccount filters `deleted_at IS NULL`).
func (s *Server) resolveMediaAccount(ctx context.Context, mediaID string) (string, error) {
	if s.deps.Media == nil {
		return "", errors.New("media tidak tersedia")
	}
	return s.resolveEntityAccount(ctx, func(acc string) error {
		_, err := s.deps.Media.ByIDAccount(ctx, acc, mediaID)
		return err
	})
}

// requireMediaWrite is the gate for trashing a LIVE media row: resolve the
// owning account through the live read, then enforce both write axes.
func (s *Server) requireMediaWrite(ctx context.Context, mediaID string) (string, error) {
	acc, err := s.resolveMediaAccount(ctx, mediaID)
	if err != nil {
		return "", err
	}
	if err := s.requireWriteRoleIn(ctx, acc); err != nil {
		return "", err
	}
	return acc, nil
}

// requireTrashedMediaWrite is the gate for restore / purge on a TRASHED media
// row. It mirrors requireTrashedFormWrite / requireTrashedProductWrite: the
// SCOPE axis runs BEFORE the entity lookup, so a readonly key cannot tell a real
// media id from a fake one by which error it gets, and the lookup is the
// bin-scoped GetTrashed (the live read cannot see a trashed row at all).
func (s *Server) requireTrashedMediaWrite(ctx context.Context, mediaID string) (string, error) {
	if err := s.requireWriteScope(ctx); err != nil {
		return "", err
	}
	if s.deps.Media == nil {
		return "", errors.New("media tidak tersedia")
	}
	acc, err := s.resolveEntityAccount(ctx, func(acc string) error {
		row, err := s.deps.Media.GetTrashed(ctx, acc, mediaID)
		if err != nil {
			return err
		}
		if row == nil {
			return errNotOwned
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if err := s.requireWriteRoleIn(ctx, acc); err != nil {
		return "", err
	}
	return acc, nil
}

// resolveSaleAccount returns the account within the call's scope that owns the
// given ref_code, or errNotOwned.
func (s *Server) resolveSaleAccount(ctx context.Context, refCode string) (string, error) {
	if s.deps.Sales == nil {
		return "", errors.New("sales tidak tersedia")
	}
	return s.resolveEntityAccount(ctx, func(acc string) error {
		sale, err := s.deps.Sales.ByRefCode(ctx, refCode)
		if err != nil {
			// ByRefCode is global; treat any error / missing as not-owned.
			return errNotOwned
		}
		if sale == nil || sale.AccountID != acc {
			return errNotOwned
		}
		return nil
	})
}

// requireSaleWrite returns the account owning a sale after enforcing the
// manage-or-admin gate for its workspace.
func (s *Server) requireSaleWrite(ctx context.Context, refCode string) (string, error) {
	acc, err := s.resolveSaleAccount(ctx, refCode)
	if err != nil {
		return "", err
	}
	if err := s.requireWriteRoleIn(ctx, acc); err != nil {
		return "", err
	}
	return acc, nil
}

// resolveWebhookAccount returns the account within scope owning a webhook id.
func (s *Server) resolveWebhookAccount(ctx context.Context, webhookID string) (string, error) {
	if s.deps.Webhooks == nil {
		return "", errors.New("webhooks tidak tersedia")
	}
	return s.resolveEntityAccount(ctx, func(acc string) error {
		hooks, err := s.deps.Webhooks.ListByAccount(ctx, acc, "")
		if err != nil {
			return err
		}
		for _, h := range hooks {
			if h.ID == webhookID {
				return nil
			}
		}
		return errNotOwned
	})
}

// subscriptionByEmail returns the account within scope owning the active
// subscription for email.
func (s *Server) subscriptionByEmail(ctx context.Context, email string) (*dbstore.Subscription, string, error) {
	if s.deps.Subs == nil {
		return nil, "", errors.New("subscriptions tidak tersedia")
	}
	accs, err := s.ownedOrScopedAccounts(ctx)
	if err != nil {
		return nil, "", err
	}
	for _, acc := range accs {
		sub, err := s.deps.Subs.ByEmail(ctx, acc, email)
		if err == nil && sub != nil {
			return sub, acc, nil
		}
	}
	return nil, "", errNotOwned
}

// roleForAccount resolves the caller's role in the workspace owning the given
// account (issue #162). The account owner is always admin; a member's role
// comes from workspace_members. "" = owner/legacy (full access).
func (s *Server) roleForAccount(ctx context.Context, accountID string) (string, error) {
	userID := s.userIDFor(ctx)
	if userID == "" {
		return "", nil
	}
	if s.deps == nil || s.deps.Workspaces == nil {
		return "", nil
	}
	return s.deps.Workspaces.RoleByAccount(ctx, accountID, userID)
}

// requireWriteRoleIn enforces BOTH write gates for the workspace owning
// accountID:
//
//   - the API-key WRITE SCOPE (requireWriteScope): a readonly key is refused
//     (issue #645 — the axis the role-only gate dropped).
//   - the manage-or-admin ROLE there (issue #162): a read_only member's agent
//     cannot write.
//
// The scope check runs first because it is credential-wide: there is no reason
// to walk the account's workspaces for a caller whose key cannot write at all.
func (s *Server) requireWriteRoleIn(ctx context.Context, accountID string) error {
	if err := s.requireWriteScope(ctx); err != nil {
		return err
	}
	role, err := s.roleForAccount(ctx, accountID)
	if err != nil {
		return fmt.Errorf("db: %w", err)
	}
	if role != "" && role != dbstore.RoleManage && role != dbstore.RoleAdmin {
		return errors.New("perlu peranan manage atau admin dalam workspace ini")
	}
	return nil
}

// requireAdminRoleIn enforces the admin-only gate for the workspace owning
// accountID (CHIP credentials + member management), on BOTH axes: the API-key
// write scope and the admin role.
func (s *Server) requireAdminRoleIn(ctx context.Context, accountID string) error {
	if err := s.requireWriteScope(ctx); err != nil {
		return err
	}
	role, err := s.roleForAccount(ctx, accountID)
	if err != nil {
		return fmt.Errorf("db: %w", err)
	}
	if role != "" && role != dbstore.RoleAdmin {
		return errors.New("perlu admin workspace")
	}
	return nil
}

// accountAccessible reports whether the user owns or is a member of a workspace
// that owns the given account (issue #162).
func (s *Server) accountAccessible(ctx context.Context, userID, accountID string) (bool, error) {
	accs, err := s.deps.Accounts.ByOwner(ctx, userID)
	if err != nil {
		return false, err
	}
	for _, a := range accs {
		if a.ID == accountID {
			return true, nil
		}
	}
	if s.deps.Workspaces != nil {
		if members, err := s.deps.Workspaces.ByMember(ctx, userID); err == nil {
			for _, m := range members {
				for _, id := range m.Accounts {
					if id == accountID {
						return true, nil
					}
				}
			}
		}
	}
	return false, nil
}

// userAccessibleAccounts returns every account across the user's accessible
// workspaces (owner + member, issue #162/#307). Without multi-workspace support it
// falls back to the user's owned accounts.
func (s *Server) userAccessibleAccounts(ctx context.Context, userID string) ([]string, error) {
	if s.deps == nil || s.deps.Workspaces == nil || s.deps.Accounts == nil {
		if s.deps == nil || s.deps.Accounts == nil {
			return []string{}, nil
		}
		accs, err := s.deps.Accounts.ByOwner(ctx, userID)
		if err != nil {
			return nil, err
		}
		ids := make([]string, 0, len(accs))
		for _, a := range accs {
			ids = append(ids, a.ID)
		}
		return ids, nil
	}
	workspaces, err := s.deps.Workspaces.ByMember(ctx, userID)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	ids := make([]string, 0, len(workspaces))
	for _, sp := range workspaces {
		for _, id := range sp.Accounts {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	// Backstop: guarantee owned accounts are included.
	owned, err := s.deps.Accounts.ByOwner(ctx, userID)
	if err == nil {
		for _, a := range owned {
			if !seen[a.ID] {
				seen[a.ID] = true
				ids = append(ids, a.ID)
			}
		}
	}
	return ids, nil
}
