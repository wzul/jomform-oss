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
		BrandID   string `json:"brand_id"`
		SecretKey string `json:"secret_key"`
		// Mode is ACCEPTED AND IGNORED (card t_9efc5ed7). It used to be the
		// merchant's Test/Live choice; the control is gone because the label
		// changes no payment behaviour (ChipAPIBaseUrl picks the endpoint, the
		// key decides what CHIP does). The field stays in the struct so an
		// older client that still sends it is not refused — the value written
		// is the stored one, never the caller's (dbstore.StoredChipMode).
		Mode            string   `json:"mode"`
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
	// Card t_9efc5ed7: the stored mode and verified state are still read,
	// because the column survives a save (nothing in the request decides it)
	// and a save that only edits the fallback email must not reset the ✓.
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

	// Card t_9efc5ed7: the Test/Live choice is GONE. The label never chose the
	// API endpoint (ChipAPIBaseUrl does), never blocked anything and never
	// changed a payment, so asking for it was a question whose answer had no
	// reader. What is stored is what the account already had (default "test"
	// for a row that has none); a `mode` in the request is accepted for
	// compatibility and ignored, never rejected.
	mode := dbstore.StoredChipMode(existingMode)

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

// testChipCreds runs the SAME live CHIP credential check the save path runs,
// without storing anything (card t_9efc5ed7 — the card's "Test connection"
// action). A pasted secret_key is the one tested; an empty body tests the
// stored key, which is what the button does when the merchant has not opened
// the Replace field.
func (h *RESTHandlers) testChipCreds(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireAdminRole(w, r, actx); !ok {
		return
	}
	var in struct {
		SecretKey string `json:"secret_key"`
	}
	// An absent/empty body is not an error: it means "test what is stored".
	_ = decodeJSON(r, &in)
	if h.ChipProbe == nil {
		writeErr(w, http.StatusServiceUnavailable, "the CHIP connection check is not configured on this server")
		return
	}
	secret := in.SecretKey
	if secret == "" {
		acc, err := h.Accounts.ByID(r.Context(), actx.Account.ID)
		if err != nil {
			respondErr(w, r, err)
			return
		}
		if acc.ChipSecretKeyEnc == "" {
			writeErr(w, http.StatusBadRequest, "secret_key is required")
			return
		}
		plain, derr := cryptoutil.Decrypt(h.SecretsKey, acc.ChipSecretKeyEnc)
		if derr != nil {
			respondErr(w, r, derr)
			return
		}
		secret = string(plain)
	}
	if err := h.ChipProbe.ValidateCredentials(r.Context(), secret); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":    "invalid credentials — check the Brand ID / Secret Key",
			"code":     CodeChipCredentialsBad,
			"verified": false,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "verified": true})
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
