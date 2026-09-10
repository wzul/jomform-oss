package httpapi

import (
	"encoding/json"
	"testing"
	"time"
)

// TestRealtimeHubPublishScoped verifies the hub fans an event out to the
// matching account's subscribers and never leaks to another account (issue
// #447 — no cross-tenant leak).
func TestRealtimeHubPublishScoped(t *testing.T) {
	h := NewRealtimeHub()
	accA := "account-a"
	accB := "account-b"

	chA := h.Subscribe(accA)
	defer h.Unsubscribe(accA, chA)
	chB := h.Subscribe(accB)
	defer h.Unsubscribe(accB, chB)

	h.Publish(accA, "sale.paid", map[string]any{"ref_code": "ABC", "amount_sen": 1000})

	select {
	case msg := <-chA:
		var ev struct {
			Event string         `json:"event"`
			Data  map[string]any `json:"data"`
		}
		if err := json.Unmarshal(msg, &ev); err != nil {
			t.Fatalf("bad event json: %v", err)
		}
		if ev.Event != "sale.paid" {
			t.Fatalf("event = %q, want sale.paid", ev.Event)
		}
		if ev.Data["ref_code"] != "ABC" {
			t.Fatalf("ref_code = %v, want ABC", ev.Data["ref_code"])
		}
	case <-time.After(time.Second):
		t.Fatal("account A subscriber did not receive the event")
	}

	// Account B must NOT receive account A's event (no cross-tenant leak).
	select {
	case msg := <-chB:
		t.Fatalf("account B leaked account A's event: %s", msg)
	case <-time.After(50 * time.Millisecond):
		// expected — no leak
	}
}

// TestRealtimeHubSlowConsumerDropped verifies a full subscriber buffer is
// dropped (closed) rather than blocking the publisher, so one stuck client
// can never stall the payment/submit flow.
func TestRealtimeHubSlowConsumerDropped(t *testing.T) {
	h := NewRealtimeHub()
	ch := h.Subscribe("acc")
	defer h.Unsubscribe("acc", ch)

	// Fill the buffer (capacity 16) so the next publish hits the default path.
	for i := 0; i < 16; i++ {
		h.Publish("acc", "sale.paid", map[string]any{"n": i})
	}
	// This publish must not block; the slow consumer is dropped.
	done := make(chan struct{})
	go func() {
		h.Publish("acc", "sale.paid", map[string]any{"n": 99})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a full subscriber buffer")
	}
}
