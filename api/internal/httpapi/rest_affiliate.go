// Affiliate REST handlers (issue #444). A merchant creates affiliate links
// (?aff=CODE) for each affiliate; sales driven by an affiliate are attributed
// to that affiliate and commission is computed on paid sales.
package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// listAffiliates GET /v1/workspaces/{id}/affiliates — list a workspace's affiliates.
func (h *RESTHandlers) listAffiliates(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	s, _, ok := h.workspaceAccessByID(w, r)
	if !ok {
		return
	}
	affs, err := h.Affiliates.ByWorkspace(r.Context(), s.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ralat")
		return
	}
	writeJSON(w, http.StatusOK, affs)
}

// createAffiliate POST /v1/workspaces/{id}/affiliates — create an affiliate link.
func (h *RESTHandlers) createAffiliate(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	s, role, ok := h.workspaceAccessByID(w, r)
	if !ok {
		return
	}
	if role != "admin" && role != "manage" {
		writeErr(w, http.StatusForbidden, "peranan tidak mencukupi")
		return
	}
	var in struct {
		Code          string  `json:"code"`
		Name          string  `json:"name"`
		Email         string  `json:"email"`
		CommissionPct float64 `json:"commission_pct"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak sah")
		return
	}
	code := strings.TrimSpace(in.Code)
	if code == "" {
		writeErr(w, http.StatusBadRequest, "code diperlukan")
		return
	}
	// Reject a duplicate code in this workspace.
	if existing, _ := h.Affiliates.ByCode(r.Context(), s.ID, code); existing != nil {
		writeErr(w, http.StatusConflict, "code sudah wujud")
		return
	}
	a := &dbstore.Affiliate{
		ID:            uuid.NewString(),
		WorkspaceID:   s.ID,
		Code:          code,
		Name:          strings.TrimSpace(in.Name),
		Email:         strings.TrimSpace(in.Email),
		CommissionPct: in.CommissionPct,
	}
	if err := h.Affiliates.Create(r.Context(), a); err != nil {
		writeErr(w, http.StatusInternalServerError, "ralat")
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

// setAffiliateCommission PUT /v1/workspaces/{id}/affiliates/{affid}/commission.
func (h *RESTHandlers) setAffiliateCommission(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	_, role, ok := h.workspaceAccessByID(w, r)
	if !ok {
		return
	}
	if role != "admin" && role != "manage" {
		writeErr(w, http.StatusForbidden, "peranan tidak mencukupi")
		return
	}
	var in struct {
		CommissionPct float64 `json:"commission_pct"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak sah")
		return
	}
	if err := h.Affiliates.SetCommission(r.Context(), r.PathValue("affid"), in.CommissionPct); err != nil {
		writeErr(w, http.StatusInternalServerError, "ralat")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// getAffiliateSales GET /v1/workspaces/{id}/affiliates/{affid}/sales — sales
// attributed to an affiliate (read-only, scoped to the workspace).
func (h *RESTHandlers) getAffiliateSales(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	s, _, ok := h.workspaceAccessByID(w, r)
	if !ok {
		return
	}
	aff, err := h.Affiliates.ByID(r.Context(), r.PathValue("affid"))
	if err != nil || aff == nil || aff.WorkspaceID != s.ID {
		writeErr(w, http.StatusNotFound, "affiliate tidak dijumpai")
		return
	}
	sales, err := h.Sales.ByAffiliateCode(r.Context(), s.ID, aff.Code)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ralat")
		return
	}
	writeJSON(w, http.StatusOK, sales)
}
