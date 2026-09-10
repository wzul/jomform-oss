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
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming tidak disokong")
		return
	}

	// Account-scoped: the resolved account (from the session/API key) is the
	// only tenant this stream may receive events for.
	accountID := actx.Account.ID

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	// Send the initial comment so the client knows the stream is open.
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

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
			flusher.Flush()
		case msg, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}
