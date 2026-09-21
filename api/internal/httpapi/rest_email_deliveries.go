// REST: email delivery status view (issue #194).
//
// GET /v1/rest/email-deliveries?form_id=&sale_ref=&event_type=&page=&limit=
//
//	Account-scoped, paginated list of email delivery events (delivered /
//	bounce / complaint / open / click) with email, event type, timestamp,
//	sale ref and detail. Filterable by form (via the sale's form_id), sale
//	ref, and event type. Read-only.
//
// The store is nil in bare-mode tests → 503.
package httpapi

import (
	"net/http"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// restEmailDelivery is the dashboard JSON shape for one email_delivery_events
// row.
type restEmailDelivery struct {
	ID         int64          `json:"id"`
	Email      string         `json:"email"`
	EventType  string         `json:"event_type"`
	BounceType string         `json:"bounce_type,omitempty"`
	SaleRef    string         `json:"sale_ref,omitempty"`
	FormID     string         `json:"form_id,omitempty"`
	Detail     map[string]any `json:"detail"`
	CreatedAt  string         `json:"created_at"`
}

// listEmailDeliveries: GET /v1/rest/email-deliveries — paginated, filterable
// delivery-event log for the merchant's accounts.
func (h *RESTHandlers) listEmailDeliveries(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if h.EmailDeliveryEvents == nil {
		writeErr(w, http.StatusServiceUnavailable, "store penghantaran emel tidak dikonfigurasi")
		return
	}
	scope, ok := h.scopeOr403(w, r, actx)
	if !ok {
		return
	}
	page, limit := parsePageLimit(r)
	f := dbstore.DeliveryFilter{
		AccountIDs: scope.Accounts,
		FormID:     r.URL.Query().Get("form_id"),
		SaleRef:    r.URL.Query().Get("sale_ref"),
		EventType:  r.URL.Query().Get("event_type"),
		Page:       page,
		Limit:      limit,
	}
	events, total, err := h.EmailDeliveryEvents.ListByFilter(r.Context(), f)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	out := make([]restEmailDelivery, 0, len(events))
	for _, e := range events {
		out = append(out, restEmailDelivery{
			ID: e.ID, Email: e.Email, EventType: e.EventType,
			BounceType: e.BounceType, SaleRef: e.SaleRef, FormID: e.FormID, Detail: e.Detail,
			CreatedAt: e.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	respond(w, http.StatusOK, map[string]any{
		"items": out, "page": page, "limit": limit, "total": total,
	})
}
