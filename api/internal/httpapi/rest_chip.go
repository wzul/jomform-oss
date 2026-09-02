// REST: CHIP credentials management (store encrypted, status-only readback).
package httpapi

import (
	"net/http"
	"strings"

	"github.com/wzul/jomform/api/internal/cryptoutil"
)

// setChipCreds saves Brand ID + Secret Key (AES-256-GCM encrypted at rest).
// Secret is write-only: never echoed back.
func (h *RESTHandlers) setChipCreds(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	var in struct {
		BrandID         string   `json:"brand_id"`
		SecretKey       string   `json:"secret_key"`
		Mode            string   `json:"mode"` // optional label; auto-derived from secret prefix
		EmailFallback   string   `json:"email_fallback"`
		MethodWhitelist []string `json:"payment_method_whitelist"`
	}
	if err := decodeJSON(r, &in); err != nil || in.BrandID == "" || in.SecretKey == "" {
		writeErr(w, http.StatusBadRequest, "brand_id dan secret_key wajib")
		return
	}

	// Mode semantics follow CHIP WooCommerce: one API endpoint; test vs live is
	// decided by WHICH secret key the merchant pastes (sk_test_… vs sk_live_…).
	// Derive the label automatically; in.Mode is only a fallback.
	mode := strings.ToLower(in.Mode)
	if strings.HasPrefix(strings.ToLower(in.SecretKey), "sk_live") {
		mode = "live"
	} else if strings.HasPrefix(strings.ToLower(in.SecretKey), "sk_test") {
		mode = "test"
	}
	if mode != "live" && mode != "test" {
		mode = "test"
	}

	// Validate against CHIP before storing (Woo parity) — save is REJECTED
	// with the CHIP error when the pair is wrong, so the ✓ badge is truthful.
	if h.ChipProbe != nil {
		if verr := h.ChipProbe.ValidateCredentials(r.Context(), in.SecretKey); verr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":    "kredensial tidak sah — semak Brand ID / Secret Key",
				"verified": false,
			})
			return
		}
	}

	enc, err := cryptoutil.Encrypt(h.SecretsKey, []byte(in.SecretKey))
	if err != nil {
		respondErr(w, err)
		return
	}
	if err := h.Accounts.SetChipCredentials(r.Context(), actx.Account.ID, in.BrandID, enc, mode, in.EmailFallback, in.MethodWhitelist); err != nil {
		respondErr(w, err)
		return
	}
	// Issue #92: audit CHIP credential change.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "settings.chip_updated", map[string]any{"mode": mode})
	respond(w, http.StatusOK, map[string]string{"status": "configured", "mode": mode})
}

// chipStatus reports stored credentials. Secret never leaves the vault,
// but Brand ID, mode and a masked tail ARE shown (like Woo's settings page).
func (h *RESTHandlers) chipStatus(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	resp, err := h.chipStatusData(r.Context(), actx.Account.ID)
	if err != nil {
		respondErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// deleteChipCreds removes stored CHIP credentials.
func (h *RESTHandlers) deleteChipCreds(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if err := h.Accounts.ClearChipCredentials(r.Context(), actx.Account.ID); err != nil {
		respondErr(w, err)
		return
	}
	// Issue #92: audit CHIP credential removal.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "settings.chip_cleared", nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}
