// Package testdb — the shared rule for asserting on the payments OnPaid
// fan-out, which is ASYNCHRONOUS.
//
// WHY THIS FILE EXISTS (card t_76e7d45d)
//
// payments.Service does not call its OnPaid hook inline; it hands it to a
// goroutine (api/internal/payments/purchases.go):
//
//	go s.OnPaid(ctx, cur.AccountID, event, ...)
//
// so NEITHER of the two signals a test usually waits for means the fan-out has
// run yet:
//
//   - the callback handler has returned 200, and
//   - the sale row reads `paid` (it is committed by the transaction the hook is
//     spawned from, not after it)
//
// A fixture that counts OnPaid calls immediately after the settle is therefore
// reading a race. Most of the time the goroutine has already run — the settle's
// own database round trip parks the asserting goroutine and the runtime picks
// the fan-out up — which is exactly what makes the assertion look sound. On a
// loaded host it can still be queued, and the assertion then reports 0 webhooks
// for a webhook that did fire:
//
//	--- FAIL: TestChipCallbackFailedAttemptIsReprocessedOnRedelivery (2.20s)
//	    payments_issue517_test.go:200: webhooks after redelivery = 0, want exactly 1
//
// measured while a sibling lane's `go test ./... -race` had every core busy.
// The same bytes passed twice in isolation. That is a false red: the sale was
// paid and the fan-out ran, just after the read.
//
// The rule is therefore: assert on the fan-out by WAITING for it, never by
// reading the counter once. Waiting is not tolerance — a swallowed webhook
// still fails, because the wait has a deadline and nothing ever arrives. What
// it removes is the ORDER dependence: "did the goroutine get scheduled before
// this line ran" stops being part of the test.
//
// It lives in testdb because both integration suites need one copy of that
// rule (the payments package and httpapi), and because the counter must not be
// a per-file invention: a package that re-derives the wait differently is how
// one suite goes back to a bare read.
package testdb

import (
	"context"
	"sync"
	"time"
)

// WebhookFireTimeout bounds how long WaitExact waits for the fan-out. It is
// generous on purpose: the fan-out is one goroutine doing an atomic append, so
// a healthy fire is sub-millisecond — anything slower than this is a fire that
// is not coming, not a slow one. The budget is what makes a swallowed webhook
// a FAILURE instead of a hang.
const WebhookFireTimeout = 5 * time.Second

// WebhookSettle is the quiet period WaitExact holds after the expected count is
// reached, before it re-reads. It is what makes "exactly" mean exactly: a
// SECOND fire from the same request is spawned at the same instant as the
// first, so a double-fire lands inside this window — the one-shot read the
// tests used to do could not see it at all. Bounded at 500ms so the handful of
// assertions per test do not add up to a slow suite.
const WebhookSettle = 500 * time.Millisecond

// WebhookCall is one OnPaid invocation, recorded verbatim.
type WebhookCall struct {
	AccountID string
	Event     string
	Payload   map[string]any
}

// WebhookCounter records the payments OnPaid fan-out for a test. Wire it
// straight into the service:
//
//	h := &payments.Service{..., OnPaid: counter.OnPaid}
//
// and assert with WaitExact (or Await, when the test wants to judge the
// timeout itself). It is safe for concurrent use: the fan-out runs on its own
// goroutine while the test reads.
type WebhookCounter struct {
	mu    sync.Mutex
	calls []WebhookCall
}

// NewWebhookCounter returns an empty recorder.
func NewWebhookCounter() *WebhookCounter { return &WebhookCounter{} }

// OnPaid is the payments.Service.OnPaid hook: `OnPaid: counter.OnPaid`.
func (c *WebhookCounter) OnPaid(_ context.Context, accountID, event string, payload map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, WebhookCall{AccountID: accountID, Event: event, Payload: payload})
}

// Count is how many fan-out calls have been RECORDED so far. It is a snapshot,
// not a settlement: use Await or WaitExact to assert on it, because a count
// that is still 0 may simply be a goroutine that has not run yet.
func (c *WebhookCounter) Count() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return int64(len(c.calls))
}

// Calls returns a copy of the calls recorded so far, in order. The copy is what
// keeps a caller reading a payload from racing the fan-out's append.
func (c *WebhookCounter) Calls() []WebhookCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]WebhookCall(nil), c.calls...)
}

// Await blocks until EXACTLY want calls have been recorded, and reports the
// count it last saw plus whether it reached want.
//
// It returns as soon as the count reaches want, and — importantly — also as
// soon as the count OVERRUNS want: a count that has passed want never comes
// back down, so waiting for the deadline would only make a double-fire take
// timeout seconds to report. ok is false in both the overrun and the timeout
// case, and the returned count is what the caller should print.
func (c *WebhookCounter) Await(want int64, timeout time.Duration) (int64, bool) {
	deadline := time.Now().Add(timeout)
	for {
		got := c.Count()
		if got >= want {
			return got, got == want
		}
		if time.Now().After(deadline) {
			return got, false
		}
		// Short poll: the fan-out is one goroutine with no I/O, so 2ms is
		// below the noise floor of the assertion and keeps a delayed fire
		// from costing a scheduler quantum to notice.
		time.Sleep(2 * time.Millisecond)
	}
}

// fataler is the slice of *testing.T WaitExact needs. It is an interface so the
// guard tests can drive the failure path with a sink that RECORDS the message
// instead of failing the run — a test that asserts "a swallowed webhook must
// still fail" cannot itself be allowed to fail.
type fataler interface {
	Helper()
	Fatalf(format string, args ...any)
}

// WaitExact waits for the fan-out to reach want calls, then holds it there for
// settle and fails the test if the count moved. `what` names the assertion and
// is the first half of the failure message, so the call site keeps its own
// wording:
//
//	c.webhooks.WaitExact(t, 1, testdb.WebhookSettle, "webhooks after redelivery")
//	→ "webhooks after redelivery = 0, want exactly 1"
//
// want==0 is a supported and meaningful case (a failed delivery must fire
// nothing): the wait returns immediately and the settle window is what proves
// nothing fired late.
func (c *WebhookCounter) WaitExact(t fataler, want int64, settle time.Duration, what string) {
	t.Helper()
	if got, ok := c.Await(want, WebhookFireTimeout); !ok {
		t.Fatalf("%s = %d, want exactly %d", what, got, want)
	}
	time.Sleep(settle)
	if got := c.Count(); got != want {
		t.Fatalf("%s = %d, want exactly %d", what, got, want)
	}
}
