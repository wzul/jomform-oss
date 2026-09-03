// Currency rates endpoint (issue #185): exposes the BNM daily-cached
// exchange rates for display/conversion of new amounts. Historical sales keep
// their original currency — this endpoint never retroactively converts.
package httpapi

import (
	"net/http"
)

// getCurrencyRates: GET /v1/rest/currency/rates — returns the cached BNM
// exchange rates (Ringgit per unit of foreign currency) plus the cache
// freshness. When the currency store is not wired (bare-mode tests) or the
// fetch fails, returns an empty list rather than erroring the dashboard.
func (h *RESTHandlers) getCurrencyRates(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if h.Currency == nil {
		writeJSON(w, http.StatusOK, map[string]any{"rates": []any{}, "cached": false})
		return
	}
	rates, cached, err := h.Currency.Rates(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"rates": []any{}, "cached": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rates": rates, "cached": cached})
}
