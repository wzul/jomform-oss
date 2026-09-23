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

// maxRecordPageLimit is the row cap of a merged /sales page. It matches
// maxPageLimit so a mixed list is not silently capped lower than the sales-only
// list it replaces.
const maxRecordPageLimit = maxPageLimit

// scopeResult carries the account set a list endpoint should filter by.
// When workspace is unset (or unusable), Accounts holds just the resolved
// account — the pre-existing behaviour.
type scopeResult struct {
	Accounts       []string
	Workspace      string // slug that was applied ("" when no workspace scope)
	WorkspaceKnown bool   // true when the workspace exists but owns zero accounts
	Role           string // caller's role in the workspace ("" when unscoped/API-key)
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

// workspaceScopeResolution reports how a request's ?workspace= scope resolved for
// a session caller. It exists so the "the scope could not be resolved" answer is
// one shared value rather than a per-endpoint guess.
type workspaceScopeResolution int

const (
	// scopeResolved: the request is scoped and the workspace belongs to the
	// caller (or the request is unscoped and there is nothing to resolve).
	scopeResolved workspaceScopeResolution = iota
	// scopeUnresolved: the request carries ?workspace=<slug> and the slug is
	// unknown or not the caller's. Answered with the same 403 and the same
	// "workspace tidak dijumpai" body resolveAccountScope already gives — every
	// dashboard page already handles that pair — and NOT with a fallback to the
	// caller's first account, which is what would make a foreign slug read
	// somebody else's data.
	scopeUnresolved
	// scopeAmbiguous: the request is UNSCOPED and the session user can reach more
	// than one workspace. There is no rule by which "no scope" means one of them,
	// so a caller that cannot tolerate the guess must refuse rather than take the
	// first account (see resolveSessionAccount).
	//
	// It is refused as a 400, not a 403, and deliberately so: this condition is a
	// request that is MISSING the parameter the endpoint requires (the same class
	// as the chat's other malformed-input 400s), not a permission denial. The
	// distinction is load-bearing on the client: web/src/lib/wsfetch.ts treats
	// 401/403 as "the session is gone" and redirects to /login, so a 403 here
	// would bounce a signed-in, multi-workspace user to the login page for a
	// request they never made by hand. A 400 renders the page's own message and
	// leaves the session alone.
	scopeAmbiguous
)

// workspaceScopeResolutionFor classifies the caller's ?workspace= scope for the
// endpoints that must not silently fall back to the first owned account. It is
// deliberately narrow: only a SESSION caller with a real ambiguity to resolve is
// affected, so no existing correct call is perturbed.
//
// It answers, in order:
//   - a slug was sent and it belongs to the caller → resolved (the same
//     workspace resolveSessionAccount would have picked),
//   - a slug was sent and it does not → unresolved (403, never a fallback),
//   - no slug and the caller has at most one accessible workspace → resolved,
//     because there is nothing to guess: the unscoped path and the scoped path
//     name the same account,
//   - no slug and SEVERAL accessible workspaces → ambiguous: the unscoped fallback
//     would pick Accounts.ByOwner(...)[0] and that is a guess.
//
// The accessible set is ByMember (owner OR member), not ByOwner: a member reaches
// workspaces they own nothing in, so counting owned accounts would serve the guess
// to exactly the people the membership feature exists for.
//
// A MISSING workspace store (bare-mode tests, an unwired deployment) is treated as
// "no workspaces", i.e. resolved: with nothing to resolve FROM, refusing would
// break every session call in a deployment that never had the feature.
func (h *RESTHandlers) workspaceScopeResolutionFor(r *http.Request, userID string) workspaceScopeResolution {
	if h == nil || userID == "" {
		return scopeResolved
	}
	slug := r.URL.Query().Get("workspace")
	if slug != "" {
		if h.Workspaces == nil {
			// No way to resolve a slug: treat it as unresolvable rather than
			// reading the caller's first account under it, which is exactly the
			// confusion this helper exists to stop.
			return scopeUnresolved
		}
		_, ok, err := h.Workspaces.AccessBySlug(r.Context(), slug, userID)
		if err != nil || !ok {
			return scopeUnresolved
		}
		return scopeResolved
	}
	if h.Workspaces == nil {
		return scopeResolved
	}
	accessible, err := h.Workspaces.ByMember(r.Context(), userID)
	if err != nil {
		// A listing failure is NOT proof of ambiguity, and refusing on it would
		// turn a database blip into a 403 for a single-workspace merchant.
		return scopeResolved
	}
	if len(accessible) > 1 {
		return scopeAmbiguous
	}
	return scopeResolved
}

// requireResolvedWorkspaceScope is the gate for the endpoints that may not fall
// back to the first owned account — currently the chat lane (see
// chatWorkspaceScopeOr403 in rest_chat.go for the argument). It writes the refusal
// and returns false when the scope cannot be resolved, and is silent (true) when it
// can.
//
// The two refusals are deliberately different, and the difference is the CLIENT's,
// not the server's taste:
//
//   - a slug the caller cannot access is a 403 with the same "workspace tidak
//     dijumpai" body resolveAccountScope already uses, so a page that handles a
//     foreign workspace already handles this one;
//   - a MISSING scope on a session that could have sent one is a 400, because
//     web/src/lib/wsfetch.ts defines 401/403 as "the session is no longer valid"
//     and redirects to /login. A 403 there would log a signed-in merchant out of
//     the page they are reading; a 400 is rendered as the page's own message.
func (h *RESTHandlers) requireResolvedWorkspaceScope(w http.ResponseWriter, r *http.Request, actx *AccountCtx) bool {
	// API-key callers have no session and no workspace list; their account comes
	// from the key. The unscoped fallback is the documented API-key path and is
	// not a guess about a person's selection.
	if actx == nil || actx.ViaAPIKey {
		return true
	}
	switch h.workspaceScopeResolutionFor(r, h.sessionOwnerID(r)) {
	case scopeUnresolved:
		writeErr(w, http.StatusForbidden, "workspace tidak dijumpai")
		return false
	case scopeAmbiguous:
		writeErr(w, http.StatusBadRequest,
			"skop workspace diperlukan — pilih workspace dahulu")
		return false
	}
	return true
}

// requireWriteScope enforces the API-key WRITE SCOPE on a mutating REST
// endpoint. Scope and workspace role are INDEPENDENT axes and a write must
// satisfy both:
//
//	scope = what this credential may do at all (readonly vs readwrite)
//	role  = what this person may do in the ?workspace= scope (issue #162)
//
// This is the REST half of the issue #645 regression. requireWriteRole used to
// test only scope.Role, and resolve() never read key.Scopes at all, so a readonly
// key could write every account endpoint (delete forms, archive products, cancel
// subscriptions, mint itself a readwrite key). A session-authenticated caller
// has no key and no scope restriction — its write gate is its role.
//
// It is a separately named function on purpose: folding the scope test back into
// requireWriteRole is exactly what 5dafe3a did to the MCP layer, and a named
// gate makes dropping it a visible diff line rather than a changed boolean.
func (h *RESTHandlers) requireWriteScope(w http.ResponseWriter, actx *AccountCtx) bool {
	if actx == nil || !actx.ViaAPIKey {
		return true // session caller: role is the gate
	}
	if !dbstore.ScopeHasWrite(actx.KeyScopes) {
		writeErr(w, http.StatusForbidden, "scope tidak mencukupi (perlu readwrite)")
		return false
	}
	return true
}

// requireWriteRole enforces BOTH axes of a write: the API-key scope (via
// requireWriteScope) and the manage-or-admin role in the ?workspace= scope. It
// resolves the ?workspace= scope (so a member's role is known) and writes a 403
// when the caller is a read_only member or holds a readonly key. Returns the
// scope (nil when handled).
func (h *RESTHandlers) requireWriteRole(w http.ResponseWriter, r *http.Request, actx *AccountCtx) (*scopeResult, bool) {
	if !h.requireWriteScope(w, actx) {
		return nil, false
	}
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
// management), on BOTH axes: the API-key write scope and the admin role. Writes
// a 403 when the caller is not an admin of the scoped workspace, or holds a
// readonly key (a readonly key is refused before the role is even consulted —
// an admin scope, not a workspace role, is what a key carries).
func (h *RESTHandlers) requireAdminRole(w http.ResponseWriter, r *http.Request, actx *AccountCtx) (*scopeResult, bool) {
	if !h.requireWriteScope(w, actx) {
		return nil, false
	}
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
