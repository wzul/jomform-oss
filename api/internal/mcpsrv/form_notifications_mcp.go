// MCP parity tools: per-form notification rules (issue #193).
package mcpsrv

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// notificationOut mirrors dbstore.FormNotification for the MCP response.
type notificationOut struct {
	ID        string                         `json:"id"`
	FormID    string                         `json:"form_id"`
	Name      string                         `json:"name"`
	Event     string                         `json:"event"`
	ToEmail   string                         `json:"to_email"`
	Subject   string                         `json:"subject"`
	Body      string                         `json:"body"`
	Condition *dbstore.NotificationCondition `json:"condition,omitempty"`
	Enabled   bool                           `json:"enabled"`
	IsDefault bool                           `json:"is_default"`
}

func toNotificationOut(n *dbstore.FormNotification) notificationOut {
	return notificationOut{
		ID: n.ID, FormID: n.FormID, Name: n.Name, Event: n.Event,
		ToEmail: n.ToEmail, Subject: n.Subject, Body: n.Body,
		Condition: n.Condition, Enabled: n.Enabled, IsDefault: n.IsDefault,
	}
}

// getFormNotificationsIn is the input for get_form_notifications.
type getFormNotificationsIn struct {
	FormID string `json:"form_id" jsonschema:"ID borang"`
}

// getFormNotifications lists a form's notification rules (issue #193).
func (s *Server) getFormNotifications(
	ctx context.Context, _ *mcp.CallToolRequest, in getFormNotificationsIn,
) (*mcp.CallToolResult, *listOutput[notificationOut], error) {
	if s.deps.Forms == nil || s.deps.Notifications == nil {
		return nil, nil, errors.New("notifications tidak tersedia")
	}
	acc, err := s.resolveFormAccount(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	if _, err := s.deps.Forms.GetByID(ctx, acc, in.FormID); err != nil {
		return nil, nil, errors.New("borang tidak dijumpai")
	}
	list, err := s.deps.Notifications.ListByForm(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	out := make([]notificationOut, 0, len(list))
	for _, n := range list {
		out = append(out, toNotificationOut(n))
	}
	return nil, newListOut(out), nil
}

// notificationIn is one notification rule in set_form_notifications.
type notificationIn struct {
	ID        string                         `json:"id,omitempty" jsonschema:"ID sedia ada (kosong = baharu)"`
	Name      string                         `json:"name" jsonschema:"Nama notifikasi"`
	Event     string                         `json:"event" jsonschema:"sale.paid|sale.failed|response.submitted|subscription.charged"`
	ToEmail   string                         `json:"to_email" jsonschema:"Emel penerima, {admin_email}, atau {field_id}"`
	Subject   string                         `json:"subject" jsonschema:"Subjek (placeholder {{ref_code}}, {{amount}}, {{field_id}})"`
	Body      string                         `json:"body" jsonschema:"Badan (placeholder {{ref_code}}, {{amount}}, {{field_id}})"`
	Condition *dbstore.NotificationCondition `json:"condition,omitempty" jsonschema:"Logik bersyarat {field_id, op, value} | null"`
	Enabled   *bool                          `json:"enabled,omitempty" jsonschema:"Aktif (lalai true)"`
}

// setFormNotificationsIn replaces a form's notification rules wholesale.
type setFormNotificationsIn struct {
	FormID        string           `json:"form_id" jsonschema:"ID borang"`
	Notifications []notificationIn `json:"notifications" jsonschema:"Senarai penuh peraturan notifikasi (ganti semua)"`
}

// setFormNotifications replaces a form's notification rules (issue #193).
// The full list is written atomically: existing rules not present are deleted,
// rules with an id are updated, and rules without an id are created. A form
// with an empty list sends no email.
func (s *Server) setFormNotifications(
	ctx context.Context, _ *mcp.CallToolRequest, in setFormNotificationsIn,
) (*mcp.CallToolResult, map[string]string, error) {
	if s.deps.Forms == nil || s.deps.Notifications == nil {
		return nil, nil, errors.New("notifications tidak tersedia")
	}
	if in.FormID == "" {
		return nil, nil, errors.New("form_id wajib")
	}
	acc, err := s.requireFormWrite(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	if _, err := s.deps.Forms.GetByID(ctx, acc, in.FormID); err != nil {
		return nil, nil, errors.New("borang tidak dijumpai")
	}
	// Validate events up front.
	for _, n := range in.Notifications {
		if !validNotificationEvent(n.Event) {
			return nil, nil, errors.New("event tidak sah: " + n.Event)
		}
	}
	// Read the current rules to know which to delete.
	existing, err := s.deps.Notifications.ListByForm(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	keep := map[string]bool{}
	for _, n := range in.Notifications {
		enabled := true
		if n.Enabled != nil {
			enabled = *n.Enabled
		}
		rec := &dbstore.FormNotification{
			FormID: in.FormID, Name: n.Name, Event: n.Event, ToEmail: n.ToEmail,
			Subject: n.Subject, Body: n.Body, Condition: n.Condition, Enabled: enabled,
		}
		if n.ID == "" {
			if err := s.deps.Notifications.Create(ctx, rec); err != nil {
				return nil, nil, err
			}
		} else {
			keep[n.ID] = true
			if err := s.deps.Notifications.Update(ctx, in.FormID, n.ID, rec); err != nil {
				return nil, nil, err
			}
		}
	}
	// Delete any existing rule not in the new list.
	for _, e := range existing {
		if !keep[e.ID] {
			if err := s.deps.Notifications.Delete(ctx, in.FormID, e.ID); err != nil {
				return nil, nil, err
			}
		}
	}
	return nil, map[string]string{"status": "saved", "form_id": in.FormID, "count": itoa(len(in.Notifications))}, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// validNotificationEvent reports whether an event name is a known notification
// event (mirrors the httpapi helper).
func validNotificationEvent(e string) bool {
	switch e {
	case dbstore.NotifyEventSalePaid, dbstore.NotifyEventSaleFailed,
		dbstore.NotifyEventResponseSubmitted, dbstore.NotifyEventSubscriptionCharged:
		return true
	}
	return false
}
