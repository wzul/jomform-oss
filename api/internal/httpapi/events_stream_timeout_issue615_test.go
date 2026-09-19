// Issue #615 follow-up: the server sets WriteTimeout: 60s (api/cmd/jomform-api/main.go).
// A long-lived SSE stream must not be cut off by it, otherwise "fixed" means
// "streams for 60 seconds". These tests pin the stream's survival past a
// deliberately small WriteTimeout, and they are the ones that would fail if the
// deadline were left in place.
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
)

// eventsStreamWithWriteTimeout wires the full chain behind a server whose
// WriteTimeout is SHORTER than the observation window, so a stream that does not
// clear its write deadline is provably killed mid-flight.
func eventsStreamWithWriteTimeout(t *testing.T, writeTimeout time.Duration) (baseURL, rawKey, accountID string, hub *RealtimeHub) {
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

	ts := httptest.NewUnstartedServer(srv.Handler())
	ts.Config.WriteTimeout = writeTimeout
	ts.Start()
	t.Cleanup(ts.Close)

	return ts.URL, bundleAPIKey(t, ctx, pool, accountID), accountID, hub
}

// TestEventsStreamSurvivesTheWriteTimeout opens the stream, waits well past the
// server's WriteTimeout WITHOUT any traffic in between (the keepalive is 25s, so
// nothing would be written), then publishes an event and requires it to arrive.
//
// Without clearing the per-connection write deadline the connection is torn down
// at the timeout, the publish is lost, and this fails — the merchant's dashboard
// goes dead after the timeout with no error on the page.
func TestEventsStreamSurvivesTheWriteTimeout(t *testing.T) {
	const timeout = 1500 * time.Millisecond
	baseURL, rawKey, accountID, hub := eventsStreamWithWriteTimeout(t, timeout)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
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
	br := bufio.NewReader(resp.Body)

	frames := make(chan string, 8)
	go func() {
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				close(frames)
				return
			}
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "data: ") {
				frames <- strings.TrimPrefix(line, "data: ")
			}
		}
	}()

	// Idle past the WriteTimeout: no bytes flow, so only an explicit deadline
	// clear keeps the connection alive.
	time.Sleep(timeout + 1200*time.Millisecond)

	deadline := time.Now().Add(5 * time.Second)
	for {
		hub.Publish(accountID, "sale.paid", map[string]any{"ref_code": "AFTER-TIMEOUT"})
		select {
		case raw, ok := <-frames:
			if !ok {
				t.Fatalf("the stream was closed after %s idle — the server's WriteTimeout still "+
					"kills long-lived streams, so a connected dashboard goes silently dead", timeout)
			}
			var ev struct {
				Event string         `json:"event"`
				Data  map[string]any `json:"data"`
			}
			if err := json.Unmarshal([]byte(raw), &ev); err != nil {
				t.Fatalf("bad frame %q: %v", raw, err)
			}
			if ev.Data["ref_code"] != "AFTER-TIMEOUT" {
				t.Fatalf("data = %v, want the post-timeout event", ev.Data)
			}
			return
		case <-time.After(30 * time.Millisecond):
			if time.Now().After(deadline) {
				t.Fatalf("no event after idling past the %s WriteTimeout — the stream is dead "+
					"although the client still believes it is connected", timeout)
			}
		}
	}
}
