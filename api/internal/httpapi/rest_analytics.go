// Analytics endpoints — per-form & per-product rollups.
package httpapi

import "net/http"

func (h *RESTHandlers) formAnalytics(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	scope, ok := h.scopeOr403(w, r, actx)
	if !ok {
		return
	}
	stats, err := h.Sales.FormAnalyticsAccounts(r.Context(), scope.Accounts)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (h *RESTHandlers) productAnalytics(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	scope, ok := h.scopeOr403(w, r, actx)
	if !ok {
		return
	}
	stats, err := h.Sales.ProductAnalyticsAccounts(r.Context(), scope.Accounts)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}
