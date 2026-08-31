// Analytics endpoints — per-form & per-product rollups.
package httpapi

import "net/http"

func (h *RESTHandlers) formAnalytics(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	stats, err := h.Sales.FormAnalytics(r.Context(), actx.Account.ID)
	if err != nil {
		respondErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (h *RESTHandlers) productAnalytics(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	stats, err := h.Sales.ProductAnalytics(r.Context(), actx.Account.ID)
	if err != nil {
		respondErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}
