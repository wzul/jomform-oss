// Realtime notifications (issue #447): an in-memory pub/sub hub + SSE stream.
//
// A merchant can keep a dashboard page open (e.g. a kitchen/order screen) and
// get an instant alert when a new sale or response arrives — no manual
// refresh. The hub is account-scoped: every subscriber is bound to the
// account resolved from the authenticated request, and Publish only fans out
// to that account's subscribers, so there is no cross-tenant leak.
//
// The stream is one-way server→client (SSE). If bidirectional messaging is
// ever needed, a websocket can be layered on the same hub later.
package httpapi

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// RealtimeHub is an in-memory pub/sub hub keyed by account ID. It is safe for
// concurrent use. Subscribers are buffered channels; a slow consumer that
// fills its buffer is dropped (its channel is closed) so one stuck client
// can never block the payment/submit flow.
type RealtimeHub struct {
	mu   sync.Mutex
	subs map[string]map[chan []byte]struct{}
}

// NewRealtimeHub builds an empty hub.
func NewRealtimeHub() *RealtimeHub {
	return &RealtimeHub{subs: map[string]map[chan []byte]struct{}{}}
}

// Subscribe registers a new subscriber for an account and returns its channel.
// The caller must call Unsubscribe when the connection closes.
func (h *RealtimeHub) Subscribe(accountID string) chan []byte {
	ch := make(chan []byte, 16)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[accountID] == nil {
		h.subs[accountID] = map[chan []byte]struct{}{}
	}
	h.subs[accountID][ch] = struct{}{}
	return ch
}

// Unsubscribe removes a subscriber for an account.
func (h *RealtimeHub) Unsubscribe(accountID string, ch chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set, ok := h.subs[accountID]; ok {
		if _, exists := set[ch]; exists {
			delete(set, ch)
			close(ch)
		}
		if len(set) == 0 {
			delete(h.subs, accountID)
		}
	}
}

// Publish fans an event out to every subscriber of an account. It never
// blocks: a full subscriber buffer is dropped (closed) so a slow client
// cannot stall the caller. accountID is the account that owns the event.
func (h *RealtimeHub) Publish(accountID, event string, payload map[string]any) {
	if h == nil {
		return
	}
	msg, err := json.Marshal(map[string]any{
		"event":     event,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data":      payload,
	})
	if err != nil {
		slog.Warn("realtime_marshal", "err", err)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	set := h.subs[accountID]
	for ch := range set {
		select {
		case ch <- msg:
		default:
			// Slow consumer — drop it so it can't block the flow.
			delete(set, ch)
			close(ch)
		}
	}
	if len(set) == 0 {
		delete(h.subs, accountID)
	}
}

// handleEventsStream: GET /v1/rest/events/stream — an authenticated SSE stream
// that pushes sale.paid, sale.failed, response.submitted, subscription.charged
// (and sale.refunded) events to the merchant's connected clients. Scoped to
// the resolved account (no cross-tenant leak). Sends a periodic keepalive
// comment so proxies/browsers don't time the connection out.
func (h *RESTHandlers) handleEventsStream(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if h.Realtime == nil {
		writeErr(w, http.StatusServiceUnavailable, "realtime tidak tersedia")
		return
	}
	// Issue #615: do NOT assert the writer directly for http.Flusher. The
	// logging middleware wraps the response, and a wrapped writer does not
	// necessarily expose the optional interfaces of the writer underneath —
	// that assertion failed on every production request and this endpoint
	// answered 500. http.NewResponseController unwraps through such layers
	// (via Unwrap(), which the middleware implements) and reports a proper
	// error if the chain genuinely cannot flush.
	rc := http.NewResponseController(w)

	// Account-scoped: the resolved account (from the session/API key) is the
	// only tenant this stream may receive events for.
	accountID := actx.Account.ID

	// Headers BEFORE the flush probe below: a flush commits the status line and
	// the headers set at that moment, so probing first would answer 200 with no
	// Content-Type and every later Header().Set would be a no-op. On a chain
	// that cannot flush nothing has been committed yet, so the 500 below still
	// goes out with its own Content-Type.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	if err := rc.Flush(); err != nil {
		writeErr(w, http.StatusInternalServerError, "streaming tidak disokong")
		return
	}
	// The flush above already committed the 200 and the headers, so there is no
	// WriteHeader here: calling it would make net/http log "superfluous
	// response.WriteHeader call" on every SSE connection.

	// Issue #615 (second half): the server sets WriteTimeout: 60s. That deadline
	// is armed per connection and would tear this stream down 60 seconds in —
	// "fixed" would mean "streams for a minute", and the dashboard would go
	// silently dead while the client still shows a connected EventSource. Clear
	// it for this connection. A writer that cannot set deadlines is not fatal
	// (ErrNotSupported): the stream still works, it just inherits the timeout.
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		slog.Debug("events_stream_no_write_deadline", "err", err)
	}

	// Send the initial comment so the client knows the stream is open.
	fmt.Fprintf(w, ": connected\n\n")
	rc.Flush()

	ch := h.Realtime.Subscribe(accountID)
	defer h.Realtime.Unsubscribe(accountID, ch)

	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			rc.Flush()
		case msg, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			rc.Flush()
		}
	}
}
