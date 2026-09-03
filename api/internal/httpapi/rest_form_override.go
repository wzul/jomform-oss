// Per-form CHIP override endpoints (migration 000005).
package httpapi

import (
	"net/http"

	"github.com/wzul/jomform/api/internal/cryptoutil"
)

func (h *RESTHandlers) setFormChipOverride(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireAdminRole(w, r, actx); !ok {
		return
	}
	var in struct {
		BrandID         string   `json:"brand_id"`
		SecretKey       string   `json:"secret_key"`
		EmailFallback   string   `json:"email_fallback"`
		MethodWhitelist []string `json:"payment_method_whitelist"`
	}
	if err := decodeJSON(r, &in); err != nil || in.BrandID == "" || in.SecretKey == "" {
		writeErr(w, http.StatusBadRequest, "brand_id dan secret_key wajib")
		return
	}
	enc, err := cryptoutil.Encrypt(h.SecretsKey, []byte(in.SecretKey))
	if err != nil {
		respondErr(w, r, err)
		return
	}
	if err := h.Forms.SetChipOverride(r.Context(), actx.Account.ID, r.PathValue("id"),
		in.BrandID, enc, in.EmailFallback, in.MethodWhitelist); err != nil {
		respondErr(w, r, err)
		return
	}
	respond(w, http.StatusOK, map[string]string{"status": "override_set"})
}

func (h *RESTHandlers) clearFormChipOverride(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireAdminRole(w, r, actx); !ok {
		return
	}
	if err := h.Forms.ClearChipOverride(r.Context(), actx.Account.ID, r.PathValue("id")); err != nil {
		respondErr(w, r, err)
		return
	}
	respond(w, http.StatusOK, map[string]string{"status": "override_cleared"})
}
