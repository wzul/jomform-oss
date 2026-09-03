// REST endpoints for response-only form submissions (issue #171).
package httpapi

import (
	"net/http"
	"time"
)

// restSubmission is the REST JSON shape of one response-only submission.
type restSubmission struct {
	ID        string         `json:"id"`
	FormID    string         `json:"form_id"`
	Data      map[string]any `json:"data"`
	CreatedAt string         `json:"created_at"`
}

// listSubmissions handles GET /v1/rest/forms/{id}/submissions — account-scoped,
// paginated, newest first. Returns { items, page, limit, total }.
func (h *RESTHandlers) listSubmissions(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	scope, ok := h.scopeOr403(w, r, actx)
	if !ok {
		return
	}
	formID := r.PathValue("id")
	page, limit := parsePageLimit(r)
	subs, total, err := h.Submissions.ListByAccountsFormPage(r.Context(), scope.Accounts, formID, page, limit)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	out := make([]restSubmission, 0, len(subs))
	for _, s := range subs {
		out = append(out, restSubmission{
			ID: s.ID, FormID: s.FormID, Data: s.Data,
			CreatedAt: s.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	respond(w, http.StatusOK, map[string]any{
		"items": out, "page": page, "limit": limit, "total": total,
	})
}

// countSubmissions handles GET /v1/rest/forms/{id}/submissions/count —
// account-scoped submission count for one form.
func (h *RESTHandlers) countSubmissions(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	scope, ok := h.scopeOr403(w, r, actx)
	if !ok {
		return
	}
	formID := r.PathValue("id")
	total, err := h.Submissions.CountByAccountsForm(r.Context(), scope.Accounts, formID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	respond(w, http.StatusOK, map[string]int64{"count": total})
}
