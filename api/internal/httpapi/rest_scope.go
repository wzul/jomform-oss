// Shared helpers for dashboard list endpoints: ?page/&limit pagination and
// the optional ?workspace=<slug> scope (workspace must belong to the session user).
package httpapi

import (
	"net/http"
	"strconv"

	"github.com/wzul/jomform/api/internal/dbstore"
)

const (
	defaultPageLimit = 20
	maxPageLimit     = 100
)

// scopeResult carries the account set a list endpoint should filter by.
// When workspace is unset (or unusable), Accounts holds just the resolved
// account — the pre-existing behaviour.
type scopeResult struct {
	Accounts   []string
	Workspace      string // slug that was applied ("" when no workspace scope)
	WorkspaceKnown bool   // true when the workspace exists but owns zero accounts
	Role       string // caller's role in the workspace ("" when unscoped/API-key)
}

// errWorkspaceForbidden is signalled when a workspace slug is given that does not
// belong to the session user (or does not exist).
type workspaceForbiddenError struct{ slug string }

func (e *workspaceForbiddenError) Error() string { return "workspace tidak dijumpai: " + e.slug }

// resolveAccountScope computes the account-id set for a dashboard list call.
// With ?workspace=<slug> it verifies the workspace belongs to the session user (owner
// OR member, issue #162) and returns every account linked to it plus the
// caller's role; otherwise it falls back to the single account resolved by
// authed(). API-key callers have no session user, so a workspace scope is
// rejected there.
func (h *RESTHandlers) resolveAccountScope(r *http.Request, actx *AccountCtx) (*scopeResult, error) {
	slug := r.URL.Query().Get("workspace")
	if slug == "" {
		return &scopeResult{Accounts: []string{actx.Account.ID}}, nil
	}
	userID := h.sessionOwnerID(r)
	if userID == "" {
		return nil, &workspaceForbiddenError{slug: slug}
	}
	acc, ok, err := h.Workspaces.AccessBySlug(r.Context(), slug, userID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, &workspaceForbiddenError{slug: slug}
	}
	return &scopeResult{Accounts: acc.Accounts, Workspace: slug, WorkspaceKnown: true, Role: acc.Role}, nil
}

// scopeOr403 resolves the account scope and writes a 403 when the requested
// workspace does not belong to the session user. Returns ok=false when handled.
func (h *RESTHandlers) scopeOr403(w http.ResponseWriter, r *http.Request, actx *AccountCtx) (*scopeResult, bool) {
	scope, err := h.resolveAccountScope(r, actx)
	if err != nil {
		if _, bad := err.(*workspaceForbiddenError); bad {
			writeErr(w, http.StatusForbidden, "workspace tidak dijumpai")
			return nil, false
		}
		respondErr(w, r, err)
		return nil, false
	}
	return scope, true
}

// roleAtLeast reports whether the caller's role in the scoped workspace meets the
// required minimum. admin > manage > read_only. An unscoped request (no
// ?workspace=) has no role and is treated as the owner's full access (legacy
// single-account callers / API keys), so it always passes.
func roleAtLeast(role, required string) bool {
	if role == "" {
		return true // unscoped / API-key: owner-equivalent
	}
	rank := map[string]int{dbstore.RoleReadOnly: 1, dbstore.RoleManage: 2, dbstore.RoleAdmin: 3}
	return rank[role] >= rank[required]
}

// requireWriteRole enforces the manage-or-admin gate on a write endpoint. It
// resolves the ?workspace= scope (so a member's role is known) and writes a 403
// when the caller is a read_only member. Returns the scope (nil when handled).
func (h *RESTHandlers) requireWriteRole(w http.ResponseWriter, r *http.Request, actx *AccountCtx) (*scopeResult, bool) {
	scope, ok := h.scopeOr403(w, r, actx)
	if !ok {
		return nil, false
	}
	if !roleAtLeast(scope.Role, dbstore.RoleManage) {
		writeErr(w, http.StatusForbidden, "perlu peranan manage atau admin")
		return nil, false
	}
	return scope, true
}

// requireAdminRole enforces the admin-only gate (CHIP credentials + member
// management). Writes a 403 when the caller is not an admin of the scoped
// workspace. Returns the scope (nil when handled).
func (h *RESTHandlers) requireAdminRole(w http.ResponseWriter, r *http.Request, actx *AccountCtx) (*scopeResult, bool) {
	scope, ok := h.scopeOr403(w, r, actx)
	if !ok {
		return nil, false
	}
	if !roleAtLeast(scope.Role, dbstore.RoleAdmin) {
		writeErr(w, http.StatusForbidden, "perlu admin workspace")
		return nil, false
	}
	return scope, true
}

// parsePageLimit reads ?page= and ?limit= (1-based page, positive limit).
// Missing or invalid values fall back to defaults; limit is capped.
func parsePageLimit(r *http.Request) (page, limit int) {
	page, limit = 1, defaultPageLimit
	if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && v > 0 {
		page = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = v
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}
	return page, limit
}
