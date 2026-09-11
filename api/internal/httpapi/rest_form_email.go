// Per-form notification email settings REST endpoints (issue #177).
//
// Issue #453: the per-form settings are an OVERRIDE of the workspace email
// settings (#107), mirroring the #57 language-override pattern. Resolution
// order is per-form -> workspace -> default. GET returns the raw override, the
// workspace settings it inherits from, and the effective (resolved) values so
// the UI can render a "use workspace default" affordance. PUT accepts
// tri-state toggles (null = inherit). DELETE clears every override.
package httpapi

import (
	"net/http"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// formEmailSettingsView is the GET response: the raw override plus the
// inherited workspace settings and the resolved effective values.
type formEmailSettingsView struct {
	*dbstore.FormEmailSettings
	// Workspace is the settings the form inherits from (nil when the form's
	// account has no workspace).
	Workspace *dbstore.EmailSettings `json:"workspace,omitempty"`
	// Effective is the resolved settings actually used when sending
	// (per-form -> workspace -> default).
	Effective *dbstore.FormEmailSettings `json:"effective"`
}

// getFormEmailSettings: GET /v1/rest/forms/{id}/email-settings
func (h *RESTHandlers) getFormEmailSettings(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	formID := r.PathValue("id")
	form, err := h.Forms.GetByID(r.Context(), actx.Account.ID, formID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	st, err := h.Forms.FormEmailSettings(r.Context(), formID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	ws := h.formWorkspaceEmailSettings(r, form)
	writeJSON(w, http.StatusOK, formEmailSettingsView{
		FormEmailSettings: st,
		Workspace:         ws,
		Effective:         dbstore.ResolveFormEmailSettings(st, ws),
	})
}

// formWorkspaceEmailSettings resolves the workspace email settings a form
// inherits from (nil when unresolvable).
func (h *RESTHandlers) formWorkspaceEmailSettings(r *http.Request, form *dbstore.Form) *dbstore.EmailSettings {
	if h.Workspaces == nil || form == nil {
		return nil
	}
	wsID, err := h.Workspaces.WorkspaceIDByAccount(r.Context(), form.AccountID)
	if err != nil || wsID == "" {
		return nil
	}
	st, err := h.Workspaces.EmailSettings(r.Context(), wsID)
	if err != nil {
		return nil
	}
	return st
}

// saveFormEmailSettings: PUT /v1/rest/forms/{id}/email-settings
//
// The boolean toggles are tri-state: the *_override fields accept true/false
// (explicit per-form override) or null (inherit the workspace setting). The
// legacy receipt_enabled / notify_merchant / html names are still accepted and
// treated as an explicit override, so existing clients keep working.
func (h *RESTHandlers) saveFormEmailSettings(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	var in struct {
		ReceiptSubject      *string `json:"receipt_subject"`
		ReceiptBody         *string `json:"receipt_body"`
		ReceiptEnabled      *bool   `json:"receipt_enabled"`
		NotifyMerchant      *bool   `json:"notify_merchant"`
		MerchantNotifyEmail *string `json:"merchant_notify_email"`
		HTML                *bool   `json:"html"`

		// Issue #453 tri-state overrides (null = use workspace default).
		ReceiptEnabledOverride *bool `json:"receipt_enabled_override"`
		NotifyMerchantOverride *bool `json:"notify_merchant_override"`
		HTMLOverride           *bool `json:"html_override"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
		return
	}
	formID := r.PathValue("id")
	if _, err := h.Forms.GetByID(r.Context(), actx.Account.ID, formID); err != nil {
		respondErr(w, r, err)
		return
	}
	// Read-modify-write: omitted fields keep their current value.
	st, err := h.Forms.FormEmailSettings(r.Context(), formID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	if in.ReceiptSubject != nil {
		st.ReceiptSubject = *in.ReceiptSubject
	}
	if in.ReceiptBody != nil {
		st.ReceiptBody = *in.ReceiptBody
	}
	if in.MerchantNotifyEmail != nil {
		st.MerchantNotifyEmail = *in.MerchantNotifyEmail
	}
	// Explicit override fields take precedence over the legacy aliases.
	if in.ReceiptEnabledOverride != nil {
		st.ReceiptEnabledOverride = in.ReceiptEnabledOverride
	} else if in.ReceiptEnabled != nil {
		st.ReceiptEnabledOverride = in.ReceiptEnabled
	}
	if in.NotifyMerchantOverride != nil {
		st.NotifyMerchantOverride = in.NotifyMerchantOverride
	} else if in.NotifyMerchant != nil {
		st.NotifyMerchantOverride = in.NotifyMerchant
	}
	if in.HTMLOverride != nil {
		st.HTMLOverride = in.HTMLOverride
	} else if in.HTML != nil {
		st.HTMLOverride = in.HTML
	}
	// Keep the legacy mirror columns consistent with the override so the row
	// remains readable by any older reader.
	st.ReceiptEnabled = st.ReceiptEnabledOverride != nil && *st.ReceiptEnabledOverride
	st.NotifyMerchant = st.NotifyMerchantOverride != nil && *st.NotifyMerchantOverride
	st.HTML = st.HTMLOverride != nil && *st.HTMLOverride
	if err := h.Forms.SaveFormEmailSettings(r.Context(), st); err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// clearFormEmailOverrides: DELETE /v1/rest/forms/{id}/email-settings
//
// Resets every per-form override so the form inherits the workspace email
// settings again (issue #453).
func (h *RESTHandlers) clearFormEmailOverrides(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	formID := r.PathValue("id")
	if _, err := h.Forms.GetByID(r.Context(), actx.Account.ID, formID); err != nil {
		respondErr(w, r, err)
		return
	}
	if err := h.Forms.ClearFormEmailOverrides(r.Context(), formID); err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}
