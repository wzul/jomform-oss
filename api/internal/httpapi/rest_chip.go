// REST: CHIP credentials management (store encrypted, status-only readback).
package httpapi

import (
	"net/http"

	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// setChipCreds saves Brand ID + Secret Key (AES-256-GCM encrypted at rest).
// Secret is write-only: never echoed back.
func (h *RESTHandlers) setChipCreds(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireAdminRole(w, r, actx); !ok {
		return
	}
	var in struct {
		BrandID         string   `json:"brand_id"`
		SecretKey       string   `json:"secret_key"`
		Mode            string   `json:"mode"` // "test"|"live": the merchant's choice; empty keeps the stored mode
		EmailFallback   string   `json:"email_fallback"`
		MethodWhitelist []string `json:"payment_method_whitelist"`
	}
	if err := decodeJSON(r, &in); err != nil || in.BrandID == "" {
		writeErr(w, http.StatusBadRequest, "brand_id is required")
		return
	}

	// Issue #327: the secret is write-only and never echoed back. A blank
	// secret_key keeps the existing stored key (so the merchant can edit
	// other fields without re-entering the secret); a non-empty value
	// replaces it. Brand ID is always required.
	//
	// Card t_8f38dc34: the stored mode and verified state are read on BOTH
	// paths now, because an omitted mode keeps what the account already says
	// (dbstore.ResolveChipMode) — a save that only edits the fallback email
	// must not relabel live credentials as test.
	secretKey := in.SecretKey
	secretEnc := ""
	existingMode := ""
	existingVerified := false
	if acc, err := h.Accounts.ByID(r.Context(), actx.Account.ID); err == nil {
		existingMode = acc.ChipMode
		existingVerified = acc.ChipVerified
		if secretKey == "" {
			secretEnc = acc.ChipSecretKeyEnc
		}
	}
	if secretKey != "" {
		enc, err := cryptoutil.Encrypt(h.SecretsKey, []byte(secretKey))
		if err != nil {
			respondErr(w, r, err)
			return
		}
		secretEnc = enc
	}
	if secretEnc == "" {
		writeErr(w, http.StatusBadRequest, "secret_key is required")
		return
	}

	// Card t_8f38dc34: the mode is the merchant's CHOICE, never a reading of
	// the secret key. CHIP issues a separate key per mode in the merchant
	// portal and the key is an opaque bearer token with no published prefix,
	// so there is nothing to infer from it. dbstore.ResolveChipMode is the ONE
	// rule this door shares with the MCP door: an explicit choice wins, an
	// omitted mode keeps the stored one, and a request that names neither
	// test nor live is rejected rather than coerced.
	mode, modeOK := dbstore.ResolveChipMode(in.Mode, existingMode)
	if !modeOK {
		writeErr(w, http.StatusBadRequest, `mode must be "test" or "live"`)
		return
	}

	// Validate against CHIP before storing (Woo parity) — save is REJECTED
	// with the CHIP error when the pair is wrong, so the ✓ badge is truthful.
	// verified records whether a live CHIP API check passed (issue #325).
	// When the secret is unchanged (blank), skip re-validation — the stored
	// key already passed at save time.
	verified := false
	if h.ChipProbe != nil && secretKey != "" {
		if verr := h.ChipProbe.ValidateCredentials(r.Context(), secretKey); verr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":    "invalid credentials — check the Brand ID / Secret Key",
				"code":     CodeChipCredentialsBad,
				"verified": false,
			})
			return
		}
		verified = true
	} else if secretKey == "" {
		// Secret unchanged — keep the previously recorded verified state.
		verified = existingVerified
	}

	if err := h.Accounts.SetChipCredentials(r.Context(), actx.Account.ID, in.BrandID, secretEnc, mode, in.EmailFallback, in.MethodWhitelist, verified); err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #92: audit CHIP credential change.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "settings.chip_updated", map[string]any{"mode": mode, "verified": verified})
	respond(w, http.StatusOK, map[string]any{"status": "configured", "mode": mode, "verified": verified})
}

// chipStatus reports stored credentials. Secret never leaves the vault,
// but Brand ID, mode and a masked tail ARE shown (like Woo's settings page).
func (h *RESTHandlers) chipStatus(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	resp, err := h.chipStatusData(r.Context(), actx.Account.ID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// deleteChipCreds removes stored CHIP credentials.
func (h *RESTHandlers) deleteChipCreds(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireAdminRole(w, r, actx); !ok {
		return
	}
	if err := h.Accounts.ClearChipCredentials(r.Context(), actx.Account.ID); err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #92: audit CHIP credential removal.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "settings.chip_cleared", nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}
