// Form restrictions REST endpoints (issue #207): schedule + entry limit.
package httpapi

import (
	"net/http"
	"time"
)

// putFormRestrictions saves a form's schedule (start/end datetime) and entry
// limit, plus the per-state messages. Nullable fields clear the restriction.
func (h *RESTHandlers) putFormRestrictions(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	var in struct {
		StartAt        *time.Time `json:"start_at"`
		EndAt          *time.Time `json:"end_at"`
		EntryLimit     *int       `json:"entry_limit"`
		PendingMessage string     `json:"pending_message"`
		ExpiredMessage string     `json:"expired_message"`
		LimitMessage   string     `json:"limit_message"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid payload")
		return
	}
	// A schedule window must not be inverted (start after end).
	if in.StartAt != nil && in.EndAt != nil && in.EndAt.Before(*in.StartAt) {
		writeErr(w, http.StatusBadRequest, "the end date must be after the start date")
		return
	}
	if in.EntryLimit != nil && *in.EntryLimit < 0 {
		writeErr(w, http.StatusBadRequest, "invalid entry limit")
		return
	}
	if err := h.Forms.UpdateRestrictions(r.Context(), actx.Account.ID, r.PathValue("id"),
		in.StartAt, in.EndAt, in.EntryLimit, in.PendingMessage, in.ExpiredMessage, in.LimitMessage); err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}
