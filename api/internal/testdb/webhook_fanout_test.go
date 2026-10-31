package testdb

// Guards for the asynchronous OnPaid fan-out rule (webhook_fanout.go).
//
// These pin the two halves of the rule that are easy to get wrong in opposite
// directions:
//
//   - a fire that is DELAYED (the state a starved scheduler leaves the goroutine
//     in — the false red this card was filed from) must PASS, and
//   - a fire that never happens must still FAIL, with the message the failing
//     gate printed. Waiting must not become tolerance.

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// fatalSink records what WaitExact would have failed the test with, instead of
// failing the run. The rule it pins is "a swallowed webhook must fail", so the
// test asserting that cannot be the thing that goes red.
type fatalSink struct {
	msg  string
	msgs []string
}

func (f *fatalSink) Helper() {}

func (f *fatalSink) Fatalf(format string, args ...any) {
	f.msg = fmt.Sprintf(format, args...)
	f.msgs = append(f.msgs, f.msg)
}

// fireAfter calls the counter's OnPaid from a goroutine after d — the shape a
// fan-out has when the host is too busy to schedule it promptly.
func fireAfter(d time.Duration, c *WebhookCounter, event string) {
	go func() {
		time.Sleep(d)
		c.OnPaid(context.Background(), "acc_1", event, map[string]any{"event": event})
	}()
}

// TestWaitExactHoldsThroughADelayedFanOut is the regression guard: TWO harness
// instances are alive at once and both fan-outs are late, so the assertion must
// survive the ordering the failing gate hit (sale committed, fan-out still
// queued) without seeing the other instance's call.
func TestWaitExactHoldsThroughADelayedFanOut(t *testing.T) {
	first := NewWebhookCounter()
	second := NewWebhookCounter()

	fireAfter(200*time.Millisecond, first, "sale.paid")
	fireAfter(300*time.Millisecond, second, "sale.paid")

	first.WaitExact(t, 1, WebhookSettle, "webhooks on the first harness")
	second.WaitExact(t, 1, WebhookSettle, "webhooks on the second harness")

	// The point of two instances: a wait that "found" a fire belonging to the
	// other harness would be the isolation failure this guards against.
	if got := first.Calls(); len(got) != 1 || got[0].AccountID != "acc_1" {
		t.Fatalf("first harness recorded %d call(s) %+v, want exactly its own", len(got), got)
	}
	if got := second.Calls(); len(got) != 1 {
		t.Fatalf("second harness recorded %d call(s), want exactly 1", len(got))
	}
}

// TestWaitExactStillFailsWhenTheWebhookIsSwallowed keeps the card's constraint:
// the assertion may wait, but a webhook that never fires still fails — with the
// message the gate printed, so the failure stays recognisable.
func TestWaitExactStillFailsWhenTheWebhookIsSwallowed(t *testing.T) {
	c := NewWebhookCounter()
	sink := &fatalSink{}

	start := time.Now()
	c.WaitExact(sink, 1, 50*time.Millisecond, "webhooks after redelivery")
	elapsed := time.Since(start)

	if len(sink.msgs) == 0 {
		t.Fatal("a swallowed webhook PASSED the wait; the assertion tolerates a missing fire")
	}
	const want = "webhooks after redelivery = 0, want exactly 1"
	if sink.msg != want {
		t.Fatalf("failure message = %q, want %q", sink.msg, want)
	}
	// It must fail by TIMEOUT, not by returning before the fire could have
	// arrived — otherwise the guard is waiting for nothing.
	if elapsed < WebhookFireTimeout {
		t.Fatalf("failed after %v, before the %v deadline: the wait gave up early", elapsed, WebhookFireTimeout)
	}
}

// TestWaitExactCatchesASecondFireInsideTheSettleWindow pins that "exactly 1"
// still means exactly one: the settle window is what sees a double-fire, which
// the one-shot read the tests used to do could not.
func TestWaitExactCatchesASecondFireInsideTheSettleWindow(t *testing.T) {
	c := NewWebhookCounter()
	sink := &fatalSink{}

	fireAfter(10*time.Millisecond, c, "sale.paid")
	fireAfter(100*time.Millisecond, c, "sale.paid")

	c.WaitExact(sink, 1, 800*time.Millisecond, "webhooks after redelivery")

	const want = "webhooks after redelivery = 2, want exactly 1"
	if sink.msg != want {
		t.Fatalf("double-fire verdict = %q, want %q", sink.msg, want)
	}
}

// TestAwaitReportsAnOverrunImmediately keeps a double-fire from costing the
// whole timeout before it is reported: a count past want never returns to it.
func TestAwaitReportsAnOverrunImmediately(t *testing.T) {
	c := NewWebhookCounter()
	c.OnPaid(context.Background(), "acc_1", "sale.paid", nil)
	c.OnPaid(context.Background(), "acc_1", "sale.paid", nil)

	start := time.Now()
	got, ok := c.Await(1, WebhookFireTimeout)
	elapsed := time.Since(start)

	if ok || got != 2 {
		t.Fatalf("Await(1) = (%d, %v), want (2, false)", got, ok)
	}
	if elapsed > time.Second {
		t.Fatalf("Await waited %v on a count that had already overrun, want an immediate verdict", elapsed)
	}
}
