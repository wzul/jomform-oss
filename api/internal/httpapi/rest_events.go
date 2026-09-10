// REST: event-tracking read endpoints (issue #92).
//
// GET /v1/rest/events?account_id=&event_name=&from=&to=&page=&limit=
//   Account-scoped, paginated audit/behaviour log for the dashboard.
// GET /v1/rest/events/count?event_name=&from=&to=
//   Simple count for behaviour analytics.
// GET /v1/rest/events/breakdown?from=&to=
//   Event counts grouped by event_name (behaviour analytics).
//
// All are read-only and account-scoped via the resolved account (or the
// ?workspace= scope). The Events store is nil in bare-mode tests → 503.
package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/httpapi/middleware"
)

// restEvent is the dashboard JSON shape for one events row.
type restEvent struct {
	ID         int64          `json:"id"`
	EventName  string         `json:"event_name"`
	Properties map[string]any `json:"properties"`
	UserID     string         `json:"user_id,omitempty"`
	IP         string         `json:"ip,omitempty"`
	UserAgent  string         `json:"user_agent,omitempty"`
	CreatedAt  string         `json:"created_at"`
}

// eventFilterFromRequest parses the shared ?event_name=&from=&to= query params
// into an EventFilter. from/to are RFC3339; invalid values are ignored.
func eventFilterFromRequest(r *http.Request, accounts []string) dbstore.EventFilter {
	f := dbstore.EventFilter{AccountIDs: accounts, EventName: r.URL.Query().Get("event_name")}
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.From = &t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.To = &t
		}
	}
	return f
}

// recordEvent appends an audit/behaviour event for the resolved account,
// capturing the caller's IP and user-agent. Best-effort: a nil Events store or
// a write error is logged, never fatal to the primary action.
func (h *RESTHandlers) recordEvent(r *http.Request, actx *AccountCtx, userID, eventName string, props map[string]any) {
	if h.Events == nil {
		return
	}
	if err := h.Events.Record(r.Context(), actx.Account.ID, userID, eventName, props,
		middleware.ClientIP(r), r.UserAgent()); err != nil {
		slog.Error("event_record", "err", err, "event", eventName, "account", actx.Account.ID)
	}
}

// listEvents: GET /v1/rest/events — paginated, filterable audit log.
func (h *RESTHandlers) listEvents(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if h.Events == nil {
		writeErr(w, http.StatusServiceUnavailable, "event store tidak dikonfigurasi")
		return
	}
	scope, ok := h.scopeOr403(w, r, actx)
	if !ok {
		return
	}
	page, limit := parsePageLimit(r)
	f := eventFilterFromRequest(r, scope.Accounts)
	f.Page, f.Limit = page, limit
	events, total, err := h.Events.List(r.Context(), f)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	out := make([]restEvent, 0, len(events))
	for _, e := range events {
		out = append(out, restEvent{
			ID: e.ID, EventName: e.EventName, Properties: e.Properties,
			UserID: e.UserID, IP: e.IP, UserAgent: e.UserAgent,
			CreatedAt: e.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	respond(w, http.StatusOK, map[string]any{
		"items": out, "page": page, "limit": limit, "total": total,
	})
}

// countEvents: GET /v1/rest/events/count — behaviour-analytics count.
func (h *RESTHandlers) countEvents(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if h.Events == nil {
		writeErr(w, http.StatusServiceUnavailable, "event store tidak dikonfigurasi")
		return
	}
	scope, ok := h.scopeOr403(w, r, actx)
	if !ok {
		return
	}
	f := eventFilterFromRequest(r, scope.Accounts)
	n, err := h.Events.Count(r.Context(), f)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	respond(w, http.StatusOK, map[string]int64{"count": n})
}

// breakdownEvents: GET /v1/rest/events/breakdown — counts by event_name.
func (h *RESTHandlers) breakdownEvents(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if h.Events == nil {
		writeErr(w, http.StatusServiceUnavailable, "event store tidak dikonfigurasi")
		return
	}
	scope, ok := h.scopeOr403(w, r, actx)
	if !ok {
		return
	}
	f := eventFilterFromRequest(r, scope.Accounts)
	rows, err := h.Events.Breakdown(r.Context(), f)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	respond(w, http.StatusOK, rows)
}
