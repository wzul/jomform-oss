// Affiliate REST handlers (issue #444). A merchant creates affiliate links
// (?aff=CODE) for each affiliate; sales driven by an affiliate are attributed
// to that affiliate and commission is computed on paid sales.
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// affiliateView is the REST shape of an affiliate.
//
// The keys are snake_case and referral_url is derived, exactly like the MCP
// tool's affiliateOut (mcpsrv/affiliate_mcp.go). The dashboard reads
// code / name / commission_pct / referral_url (settings/page.tsx,
// `AffiliateRow`); the handler used to marshal the dbstore struct straight out,
// whose keys are PascalCase and which has no referral_url at all — so every row
// on Settings → Affiliates rendered as an empty "%" and the referral link, the
// whole point of the tab, was never shown.
type affiliateView struct {
	ID            string  `json:"id"`
	WorkspaceID   string  `json:"workspace_id"`
	Code          string  `json:"code"`
	Name          string  `json:"name"`
	Email         string  `json:"email"`
	CommissionPct float64 `json:"commission_pct"`
	ReferralURL   string  `json:"referral_url"`
}

// affiliateViewOf builds the view + the link an affiliate hands out. The host is
// the workspace's own storefront, so the code travels with whatever page the
// merchant sent the affiliate to.
func affiliateViewOf(slug string, a *dbstore.Affiliate) affiliateView {
	return affiliateView{
		ID:            a.ID,
		WorkspaceID:   a.WorkspaceID,
		Code:          a.Code,
		Name:          a.Name,
		Email:         a.Email,
		CommissionPct: a.CommissionPct,
		ReferralURL:   "https://" + slug + ".jomform.com/?aff=" + a.Code,
	}
}

// listAffiliates GET /v1/workspaces/{id}/affiliates — list a workspace's affiliates.
func (h *RESTHandlers) listAffiliates(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	s, _, ok := h.workspaceAccessByID(w, r)
	if !ok {
		return
	}
	affs, err := h.Affiliates.ByWorkspace(r.Context(), s.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "error")
		return
	}
	out := make([]affiliateView, 0, len(affs))
	for _, a := range affs {
		out = append(out, affiliateViewOf(s.Slug, a))
	}
	writeJSON(w, http.StatusOK, out)
}

// createAffiliate POST /v1/workspaces/{id}/affiliates — create an affiliate link.
func (h *RESTHandlers) createAffiliate(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	s, role, ok := h.workspaceAccessByID(w, r)
	if !ok {
		return
	}
	if role != "admin" && role != "manage" {
		writeErr(w, http.StatusForbidden, "insufficient role")
		return
	}
	var in struct {
		Code          string  `json:"code"`
		Name          string  `json:"name"`
		Email         string  `json:"email"`
		CommissionPct float64 `json:"commission_pct"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	code := strings.TrimSpace(in.Code)
	if code == "" {
		writeErr(w, http.StatusBadRequest, "code is required")
		return
	}
	// Reject a duplicate code in this workspace.
	if existing, _ := h.Affiliates.ByCode(r.Context(), s.ID, code); existing != nil {
		writeErr(w, http.StatusConflict, "code already exists")
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
		writeErr(w, http.StatusInternalServerError, "error")
		return
	}
	writeJSON(w, http.StatusCreated, affiliateViewOf(s.Slug, a))
}

// setAffiliateCommission PUT /v1/workspaces/{id}/affiliates/{affid}/commission.
//
// Issue #519: the affiliate id in the path is checked against the workspace the
// caller was authorised against before anything is written — the same rule
// getAffiliateSales applies. A mismatch is a 404, identical to an unknown id in
// one's own workspace, so the caller cannot probe for ids in other tenants.
func (h *RESTHandlers) setAffiliateCommission(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	s, role, ok := h.workspaceAccessByID(w, r)
	if !ok {
		return
	}
	if role != "admin" && role != "manage" {
		writeErr(w, http.StatusForbidden, "insufficient role")
		return
	}
	var in struct {
		CommissionPct float64 `json:"commission_pct"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	affid := r.PathValue("affid")
	// Scope check (issue #519): the affiliate must belong to the authorised
	// workspace. 404 — never 403 — so no other tenant's ids are enumerable.
	aff, err := h.Affiliates.ByID(r.Context(), affid)
	if err != nil || aff == nil || aff.WorkspaceID != s.ID {
		writeErr(w, http.StatusNotFound, "affiliate not found")
		return
	}
	// Scoped store write (issue #519): the UPDATE itself is bound to the
	// authorised workspace, so a future caller cannot reintroduce the bug.
	if err := h.Affiliates.SetCommission(r.Context(), s.ID, affid, in.CommissionPct); err != nil {
		if errors.Is(err, dbstore.ErrAffiliateNotFound) {
			writeErr(w, http.StatusNotFound, "affiliate not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "error")
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
		writeErr(w, http.StatusNotFound, "affiliate not found")
		return
	}
	sales, err := h.Sales.ByAffiliateCode(r.Context(), s.ID, aff.Code)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "error")
		return
	}
	writeJSON(w, http.StatusOK, sales)
}
