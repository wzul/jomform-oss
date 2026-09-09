// Form display settings: redirects, embed toggle, logo.
package httpapi

import (
	"net/http"
	"strings"
)

func (h *RESTHandlers) putFormSettings(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	var in struct {
		RedirectSuccess string `json:"redirect_success"`
		RedirectFailure string `json:"redirect_failure"`
		AllowEmbed      *bool  `json:"allow_embed"`
		LogoURL         string `json:"logo_url"`
		// Issue #393: per-form payment-failed message (HTML, sanitized on
		// render). Empty = fall back to the buyer-facing default.
		FailureMessage string `json:"failure_message"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
		return
	}
	for _, u := range []string{in.RedirectSuccess, in.RedirectFailure, in.LogoURL} {
		if u != "" && !strings.HasPrefix(u, "https://") {
			writeErr(w, http.StatusBadRequest, "URL mesti https://")
			return
		}
	}
	embed := true
	if in.AllowEmbed != nil {
		embed = *in.AllowEmbed
	}
	if err := h.Forms.UpdateSettings(r.Context(), actx.Account.ID, r.PathValue("id"),
		in.RedirectSuccess, in.RedirectFailure, embed, in.LogoURL, in.FailureMessage); err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// getWebhookKey moved to signkey.go
