// Per-form notification rules REST endpoints (issue #193).
package httpapi

import (
	"net/http"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// listFormNotifications: GET /v1/rest/forms/{id}/notifications
func (h *RESTHandlers) listFormNotifications(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	if _, err := h.Forms.GetByID(r.Context(), actx.Account.ID, r.PathValue("id")); err != nil {
		respondErr(w, r, err)
		return
	}
	if h.Notifications == nil {
		writeJSON(w, http.StatusOK, []*dbstore.FormNotification{})
		return
	}
	list, err := h.Notifications.ListByForm(r.Context(), r.PathValue("id"))
	if err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// createFormNotification: POST /v1/rest/forms/{id}/notifications
func (h *RESTHandlers) createFormNotification(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	var in struct {
		Name      string                         `json:"name"`
		Event     string                         `json:"event"`
		ToEmail   string                         `json:"to_email"`
		Subject   string                         `json:"subject"`
		Body      string                         `json:"body"`
		Condition *dbstore.NotificationCondition `json:"condition"`
		Enabled   *bool                          `json:"enabled"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
		return
	}
	if _, err := h.Forms.GetByID(r.Context(), actx.Account.ID, r.PathValue("id")); err != nil {
		respondErr(w, r, err)
		return
	}
	if !validNotificationEvent(in.Event) {
		writeErr(w, http.StatusBadRequest, "event tidak sah")
		return
	}
	if h.Notifications == nil {
		writeErr(w, http.StatusBadRequest, "notifications tidak tersedia")
		return
	}
	n := &dbstore.FormNotification{
		FormID: r.PathValue("id"), Name: in.Name, Event: in.Event,
		ToEmail: in.ToEmail, Subject: in.Subject, Body: in.Body, Condition: in.Condition,
		Enabled: true,
	}
	if in.Enabled != nil {
		n.Enabled = *in.Enabled
	}
	if err := h.Notifications.Create(r.Context(), n); err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, n)
}

// updateFormNotification: PUT /v1/rest/forms/{id}/notifications/{nid}
func (h *RESTHandlers) updateFormNotification(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	var in struct {
		Name      string                         `json:"name"`
		Event     string                         `json:"event"`
		ToEmail   string                         `json:"to_email"`
		Subject   string                         `json:"subject"`
		Body      string                         `json:"body"`
		Condition *dbstore.NotificationCondition `json:"condition"`
		Enabled   *bool                          `json:"enabled"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
		return
	}
	if _, err := h.Forms.GetByID(r.Context(), actx.Account.ID, r.PathValue("id")); err != nil {
		respondErr(w, r, err)
		return
	}
	if !validNotificationEvent(in.Event) {
		writeErr(w, http.StatusBadRequest, "event tidak sah")
		return
	}
	if h.Notifications == nil {
		writeErr(w, http.StatusBadRequest, "notifications tidak tersedia")
		return
	}
	n := &dbstore.FormNotification{
		Name: in.Name, Event: in.Event, ToEmail: in.ToEmail,
		Subject: in.Subject, Body: in.Body, Condition: in.Condition, Enabled: true,
	}
	if in.Enabled != nil {
		n.Enabled = *in.Enabled
	}
	if err := h.Notifications.Update(r.Context(), r.PathValue("id"), r.PathValue("nid"), n); err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// deleteFormNotification: DELETE /v1/rest/forms/{id}/notifications/{nid}
func (h *RESTHandlers) deleteFormNotification(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	if _, err := h.Forms.GetByID(r.Context(), actx.Account.ID, r.PathValue("id")); err != nil {
		respondErr(w, r, err)
		return
	}
	if h.Notifications == nil {
		writeErr(w, http.StatusBadRequest, "notifications tidak tersedia")
		return
	}
	if err := h.Notifications.Delete(r.Context(), r.PathValue("id"), r.PathValue("nid")); err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// validNotificationEvent reports whether an event name is a known notification
// event.
func validNotificationEvent(e string) bool {
	switch e {
	case dbstore.NotifyEventSalePaid, dbstore.NotifyEventSaleFailed,
		dbstore.NotifyEventResponseSubmitted, dbstore.NotifyEventSubscriptionCharged:
		return true
	}
	return false
}
