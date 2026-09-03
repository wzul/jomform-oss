// Per-form confirmation REST endpoints (issue #179).
package httpapi

import (
	"net/http"
	"strings"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// getFormConfirmations: GET /v1/rest/forms/{id}/confirmations
func (h *RESTHandlers) getFormConfirmations(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, err := h.Forms.GetByID(r.Context(), actx.Account.ID, r.PathValue("id")); err != nil {
		respondErr(w, r, err)
		return
	}
	confs, err := h.Forms.Confirmations(r.Context(), r.PathValue("id"))
	if err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"confirmations": confs})
}

// saveFormConfirmations: PUT /v1/rest/forms/{id}/confirmations
// Replaces the form's confirmations wholesale. The default confirmation is
// always preserved server-side (cannot be deleted).
func (h *RESTHandlers) saveFormConfirmations(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	var in struct {
		Confirmations []*dbstore.Confirmation `json:"confirmations"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
		return
	}
	if _, err := h.Forms.GetByID(r.Context(), actx.Account.ID, r.PathValue("id")); err != nil {
		respondErr(w, r, err)
		return
	}
	// Validate: names non-empty, redirect confirmations need a https URL,
	// message confirmations may carry HTML (sanitized on render).
	for _, c := range in.Confirmations {
		if strings.TrimSpace(c.Name) == "" {
			writeErr(w, http.StatusBadRequest, "nama pengesahan wajib")
			return
		}
		if c.Type != dbstore.ConfirmationTypeMessage && c.Type != dbstore.ConfirmationTypeRedirect {
			writeErr(w, http.StatusBadRequest, "jenis pengesahan tidak sah")
			return
		}
		if c.Type == dbstore.ConfirmationTypeRedirect {
			if c.RedirectURL == "" || !strings.HasPrefix(c.RedirectURL, "https://") {
				writeErr(w, http.StatusBadRequest, "URL pengesahan mesti https://")
				return
			}
		}
	}
	if err := h.Forms.SaveConfirmations(r.Context(), r.PathValue("id"), in.Confirmations); err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}
