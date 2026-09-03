// Per-form notification email settings REST endpoints (issue #177).
package httpapi

import (
	"net/http"
)

// getFormEmailSettings: GET /v1/rest/forms/{id}/email-settings
func (h *RESTHandlers) getFormEmailSettings(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	if _, err := h.Forms.GetByID(r.Context(), actx.Account.ID, r.PathValue("id")); err != nil {
		respondErr(w, r, err)
		return
	}
	st, err := h.Forms.FormEmailSettings(r.Context(), r.PathValue("id"))
	if err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// saveFormEmailSettings: PUT /v1/rest/forms/{id}/email-settings
func (h *RESTHandlers) saveFormEmailSettings(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	var in struct {
		ReceiptSubject      string `json:"receipt_subject"`
		ReceiptBody         string `json:"receipt_body"`
		ReceiptEnabled      *bool  `json:"receipt_enabled"`
		NotifyMerchant      *bool  `json:"notify_merchant"`
		MerchantNotifyEmail string `json:"merchant_notify_email"`
		HTML                *bool  `json:"html"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
		return
	}
	if _, err := h.Forms.GetByID(r.Context(), actx.Account.ID, r.PathValue("id")); err != nil {
		respondErr(w, r, err)
		return
	}
	// Read-modify-write: omitted fields keep their current value.
	st, err := h.Forms.FormEmailSettings(r.Context(), r.PathValue("id"))
	if err != nil {
		respondErr(w, r, err)
		return
	}
	st.ReceiptSubject = in.ReceiptSubject
	st.ReceiptBody = in.ReceiptBody
	st.MerchantNotifyEmail = in.MerchantNotifyEmail
	if in.ReceiptEnabled != nil {
		st.ReceiptEnabled = *in.ReceiptEnabled
	}
	if in.NotifyMerchant != nil {
		st.NotifyMerchant = *in.NotifyMerchant
	}
	if in.HTML != nil {
		st.HTML = *in.HTML
	}
	if err := h.Forms.SaveFormEmailSettings(r.Context(), st); err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}
