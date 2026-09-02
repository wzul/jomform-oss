// Tracking scripts settings — global + per-form.
package httpapi

import (
	"net/http"
)

func (h *RESTHandlers) putTracking(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	var in struct {
		Head string `json:"head"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
		return
	}
	if err := h.Accounts.SetTracking(r.Context(), actx.Account.ID, sanitizeTracking(in.Head)); err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (h *RESTHandlers) getTracking(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	v, err := h.Accounts.Tracking(r.Context(), actx.Account.ID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"head": v})
}

func (h *RESTHandlers) putFormTracking(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	var in struct {
		Head string `json:"head"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
		return
	}
	if err := h.Forms.SetTracking(r.Context(), actx.Account.ID, r.PathValue("id"), sanitizeTracking(in.Head)); err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}
