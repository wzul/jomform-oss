// Shared helpers for dashboard list endpoints: ?page/&limit pagination and
// the optional ?space=<slug> scope (space must belong to the session user).
package httpapi

import (
	"net/http"
	"strconv"
)

const (
	defaultPageLimit = 20
	maxPageLimit     = 100
)

// scopeResult carries the account set a list endpoint should filter by.
// When space is unset (or unusable), Accounts holds just the resolved
// account — the pre-existing behaviour.
type scopeResult struct {
	Accounts   []string
	Space      string // slug that was applied ("" when no space scope)
	SpaceKnown bool   // true when the space exists but owns zero accounts
}

// errSpaceForbidden is signalled when a space slug is given that does not
// belong to the session user (or does not exist).
type spaceForbiddenError struct{ slug string }

func (e *spaceForbiddenError) Error() string { return "space tidak dijumpai: " + e.slug }

// resolveAccountScope computes the account-id set for a dashboard list call.
// With ?space=<slug> it verifies the space belongs to the session user and
// returns every account linked to it; otherwise it falls back to the single
// account resolved by authed(). API-key callers have no session user, so a
// space scope is rejected there.
func (h *RESTHandlers) resolveAccountScope(r *http.Request, actx *AccountCtx) (*scopeResult, error) {
	slug := r.URL.Query().Get("space")
	if slug == "" {
		return &scopeResult{Accounts: []string{actx.Account.ID}}, nil
	}
	userID := h.sessionOwnerID(r)
	if userID == "" {
		return nil, &spaceForbiddenError{slug: slug}
	}
	owned, ids, err := h.Spaces.AccountIDsBySlugForOwner(r.Context(), slug, userID)
	if err != nil {
		return nil, err
	}
	if !owned {
		return nil, &spaceForbiddenError{slug: slug}
	}
	return &scopeResult{Accounts: ids, Space: slug, SpaceKnown: true}, nil
}

// scopeOr403 resolves the account scope and writes a 403 when the requested
// space does not belong to the session user. Returns ok=false when handled.
func (h *RESTHandlers) scopeOr403(w http.ResponseWriter, r *http.Request, actx *AccountCtx) (*scopeResult, bool) {
	scope, err := h.resolveAccountScope(r, actx)
	if err != nil {
		if _, bad := err.(*spaceForbiddenError); bad {
			writeErr(w, http.StatusForbidden, "space tidak dijumpai")
			return nil, false
		}
		respondErr(w, err)
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
