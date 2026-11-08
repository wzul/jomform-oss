package testdb

// Guards for the asynchronous OnPaid fan-out rule (webhook_fanout.go).
//
// These pin the three halves of the rule that are easy to get wrong:
//
//   - a fire that is DELAYED (the state a starved scheduler leaves the goroutine
//     in — the false red this card was filed from) must PASS, and
//   - a fire that never happens must still FAIL, with the message the failing
//     gate printed. Waiting must not become tolerance.
//   - the settle window WebhookSettle must stay long enough to SEE a second
//     fire, and the double-fire guard must drive the shipped constant rather
//     than a window of its own — otherwise zeroing the constant silently turns
//     the "exactly 1" arm of all 12 integration assertions into a one-shot read.

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

// secondFireDelay is how late the second fan-out of the double-fire guard
// arrives. It is a LITERAL on purpose, never `WebhookSettle/4`: the sabotage this
// guard must survive is `WebhookSettle := 0`, and a fire derived from the
// constant would shrink WITH the window, so a zeroed window could still catch its
// own zeroed fire and the mutation would pass.
const secondFireDelay = 100 * time.Millisecond

// TestWebhookSettleStaysLongEnoughToSeeASecondFire pins the magnitude of the
// exported constant the 12 integration assertions pass
// (payments: callback_lock_test.go, callback_claim_issue517_test.go,
// table_number_issue489_test.go; httpapi: payments_issue517_test.go). None of
// them re-derives the window, and the settle is the ONLY thing that can see a
// second fan-out for the same sale — so a window at or below the gap between two
// fires turns every "want exactly 1" into the one-shot read this rule exists to
// replace. This is the arm that fails DETERMINISTICALLY when the constant is
// zeroed; the behavioural arm below fails too, but it depends on scheduling.
func TestWebhookSettleStaysLongEnoughToSeeASecondFire(t *testing.T) {
	// Twice the gap between the two fires: the second fire has to land with at
	// least as much slack again inside the window, or a host that delays that
	// goroutine past the re-read is indistinguishable from "no second fire".
	if min := 2 * secondFireDelay; WebhookSettle < min {
		t.Fatalf("WebhookSettle = %v, want at least %v (twice the %v gap between two fires): "+
			"a settle window this short cannot see a second fire, so every WaitExact %q "+
			"in the payments and httpapi suites becomes a one-shot read",
			WebhookSettle, min, secondFireDelay, "want exactly 1")
	}
}

// TestWaitExactCatchesASecondFireInsideTheSettleWindow pins that "exactly 1"
// still means exactly one: the settle window is what sees a double-fire, which
// the one-shot read the tests used to do could not.
//
// The settle argument is the SHIPPED constant, not a hand-picked window, because
// the 12 integration call sites pass exactly this value: a guard that supplied
// its own 800ms would stay green with WebhookSettle zeroed, which would delete
// the double-fire arm from every one of them without a red test.
func TestWaitExactCatchesASecondFireInsideTheSettleWindow(t *testing.T) {
	c := NewWebhookCounter()
	sink := &fatalSink{}

	fireAfter(10*time.Millisecond, c, "sale.paid")
	fireAfter(secondFireDelay, c, "sale.paid")

	c.WaitExact(sink, 1, WebhookSettle, "webhooks after redelivery")

	const want = "webhooks after redelivery = 2, want exactly 1"
	if sink.msg != want {
		t.Fatalf("double-fire verdict = %q, want %q (WebhookSettle = %v must hold the wait "+
			"open long enough to see the fire at +%v)",
			sink.msg, want, WebhookSettle, secondFireDelay)
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
