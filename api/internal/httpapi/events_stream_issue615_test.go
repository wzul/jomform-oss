// Issue #615: the SSE stream (GET /v1/rest/events/stream) answered 500 on every
// authenticated request, because the logging middleware's wrapper structurally
// dropped http.Flusher (an embedded INTERFACE promotes only the methods that
// interface declares). The handler's `w.(http.Flusher)` assertion then failed
// before it ever subscribed — so the dashboard's realtime notifications never
// received an event.
//
// The regression test that catches this has to mount the FULL middleware chain
// and speak over a real network connection: a bare httptest.ResponseRecorder
// satisfies http.Flusher by itself, so a handler-only test passes both before
// and after the fix and proves nothing. These tests go through Server.Handler()
// (RequestID → Logger → recoverer → Secure) over a real listener, which is
// exactly the shape production serves.
//
// Integration: needs JOMFORM_TEST_DATABASE_URL (scratch schema) and
// JOMFORM_TEST_VALKEY_URL; skips otherwise.
package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/config"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// eventsStreamFixture wires RESTHandlers + a realtime hub behind the full server
// middleware chain, and returns the live base URL plus an API key for the seeded
// account.
func eventsStreamFixture(t *testing.T) (baseURL, rawKey, accountID string, hub *RealtimeHub) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ = bundleFixture(t, ctx, pool)

	h := newBundleHandlers(pool)
	hub = NewRealtimeHub()
	h.Realtime = hub

	srv := NewServer(&config.Config{
		Port:        "0",
		DatabaseURL: "postgres://localhost/jomform_test",
		AppBaseURL:  "http://localhost:8080",
		Env:         "dev",
	}, testLogger(), &Deps{REST: h})

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	return ts.URL, bundleAPIKey(t, ctx, pool, accountID), accountID, hub
}

// TestEventsStreamIsNot500ThroughTheFullMiddlewareChain is the regression test
// the issue asks for: it must return 200 with Content-Type: text/event-stream
// and the ": connected" comment, over the real chain. Pre-fix this answers 500
// (the wrapper hid http.Flusher) — see the bite proof in the PR.
func TestEventsStreamIsNot500ThroughTheFullMiddlewareChain(t *testing.T) {
	baseURL, rawKey, _, _ := eventsStreamFixture(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/rest/events/stream", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+rawKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET events/stream: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/rest/events/stream = %d, want 200 with Content-Type text/event-stream "+
			"(a 500 here means the middleware chain still hides http.Flusher — issue #615)", resp.StatusCode)
	}
	// The header must be on the COMMITTED response: a handler that flushes
	// before setting it answers 200 with an empty Content-Type.
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	// The initial comment must actually reach the client (flushed, not held).
	br := bufio.NewReader(resp.Body)
	line, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("read stream opening: %v", err)
	}
	if strings.TrimSpace(line) != ": connected" {
		t.Fatalf("first stream line = %q, want %q", strings.TrimSpace(line), ": connected")
	}
}

// TestEventsStreamDeliversAPublishedEvent: a 200 that streams nothing is not a
// fix. A published event for the caller's account must arrive on the live
// stream, and an event for a DIFFERENT account must not (no cross-tenant leak).
func TestEventsStreamDeliversAPublishedEvent(t *testing.T) {
	baseURL, rawKey, accountID, hub := eventsStreamFixture(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/rest/events/stream", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+rawKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET events/stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// Drain the opening comment so the next read is the event frame.
	br := bufio.NewReader(resp.Body)

	// A foreign account's event must NOT arrive on this stream. Publish it
	// before ours so it would be the FIRST frame if the stream leaked.
	hub.Publish("some-other-account", "sale.paid", map[string]any{"ref_code": "LEAK"})

	frames := make(chan string, 8)
	go func() {
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "data: ") {
				frames <- strings.TrimPrefix(line, "data: ")
			}
		}
	}()

	// Republish until the first frame lands: Subscribe happens inside the
	// handler, so a single publish can race the subscription. Our own event is
	// the only one published to this account; the foreign one above is the leak
	// detector and would arrive first if the scoping were broken.
	var raw string
	deadline := time.Now().Add(5 * time.Second)
loop:
	for {
		hub.Publish(accountID, "response.submitted", map[string]any{"form_id": "f-1"})
		select {
		case raw = <-frames:
			break loop
		case <-time.After(25 * time.Millisecond):
			if time.Now().After(deadline) {
				t.Fatal("no event arrived on the stream within 5s — a 200 that streams nothing is not a fix")
			}
		}
	}

	var ev struct {
		Event string         `json:"event"`
		Data  map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		t.Fatalf("bad event frame %q: %v", raw, err)
	}
	if ev.Event != "response.submitted" {
		t.Fatalf("first streamed frame = %q, want response.submitted (a sale.paid frame for "+
			"another account is a cross-tenant leak)", ev.Event)
	}
	if ev.Data["form_id"] != "f-1" {
		t.Fatalf("data = %v, want form_id f-1", ev.Data)
	}
}

// TestEventsStreamRefusesForeignWorkspaceScope: the account that receives events
// is the RESOLVED one. An API key cannot pass ?workspace= (no session user), so
// the stream must fall back to the key's own account rather than 403 or leak.
func TestEventsStreamRefusesForeignWorkspaceScope(t *testing.T) {
	baseURL, rawKey, _, _ := eventsStreamFixture(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		baseURL+"/v1/rest/events/stream?workspace=no-such-workspace", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+rawKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET events/stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (keyed callers are the legacy unscoped path)", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
}

// TestEventsStreamUnauthenticatedIs401: the stream is account-scoped, so it must
// never open for an anonymous caller.
func TestEventsStreamUnauthenticatedIs401(t *testing.T) {
	baseURL, _, _, _ := eventsStreamFixture(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/rest/events/stream", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET events/stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous GET events/stream = %d, want 401", resp.StatusCode)
	}
}

// TestEventsStream503WhenHubUnwired keeps the honest-negative case: without a
// hub the endpoint says so rather than opening a dead stream.
func TestEventsStream503WhenHubUnwired(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)
	h := newBundleHandlers(pool)
	h.Realtime = nil

	raw := bundleAPIKey(t, ctx, pool, accountID)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/v1/rest/events/stream", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 when the realtime hub is unwired (body %s)", rec.Code, rec.Body.String())
	}
}

// TestSeedAccountExists keeps the fixture honest: the account the API key
// belongs to must be the one the stream resolves.
func TestSeedAccountExists(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)
	if _, err := dbstore.NewAccountStore(pool).ByID(ctx, accountID); err != nil {
		t.Fatalf("seeded account not readable: %v", err)
	}
}
