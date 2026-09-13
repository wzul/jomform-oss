package dbstore

// Issue #534: a due subscription must be claimed before it is charged, so that a
// second sweep tick (or a concurrent sweep, or a second instance) cannot charge
// the customer's card again while the first charge is still pending.
//
// Issue #535: FailedAttempts and ChargeCount silently returned 0 on a query
// error. FailedAttempts referenced a nonexistent column (created_at instead of
// charged_at), so the retry ladder never escalated; ChargeCount failing to 0
// would let a fixed-billing subscription charge past its Nth charge.
//
// The guards below bite: each assertion fails against the pre-fix code.

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestIssue534ClaimDueIsSingleWinner is the core double-charge guard: two
// concurrent claims for the same due subscription must yield exactly one winner.
func TestIssue534ClaimDueIsSingleWinner(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ss := NewSubscriptionStore(p)
	acc := uniqueAccount(t, ctx, p)
	subID := newDueSubscriptionFixture(t, ctx, p, acc, "claim@example.test")

	const claimants = 4
	var wg sync.WaitGroup
	wins := make([]bool, claimants)
	errs := make([]error, claimants)
	start := make(chan struct{})
	for i := 0; i < claimants; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start // fire together, so the claims genuinely race
			won, err := ss.ClaimDue(ctx, subID, time.Hour)
			wins[i], errs[i] = won, err
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("claimant %d: %v", i, err)
		}
	}
	n := 0
	for _, w := range wins {
		if w {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("issue #534: %d of %d concurrent ClaimDue calls won, want exactly 1 — "+
			"more than one winner means more than one charge for the same period", n, claimants)
	}
}

// TestIssue534ClaimRefusedWhileChargePending proves the second half of the guard:
// even a single sequential claim must be refused while a charge is outstanding.
func TestIssue534ClaimRefusedWhileChargePending(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ss := NewSubscriptionStore(p)
	acc := uniqueAccount(t, ctx, p)
	subID := newDueSubscriptionFixture(t, ctx, p, acc, "pending@example.test")

	// A charge is recorded as pending (money already in flight).
	if err := ss.AddCharge(ctx, subID, "pur_pending_534", 1500, ChargePending, 1); err != nil {
		t.Fatalf("add pending charge: %v", err)
	}
	// Make the subscription due again, isolating the pending-charge guard from
	// the lease (which would otherwise also refuse the claim).
	if err := ss.ScheduleNextCharge(ctx, subID, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("make due: %v", err)
	}

	won, err := ss.ClaimDue(ctx, subID, time.Hour)
	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if won {
		t.Fatal("issue #534: ClaimDue won while a charge is still pending — the next " +
			"sweep tick would charge the customer's card a second time for the same period")
	}
}

// TestIssue534ClaimAdvancesNextCharge proves the claim acts as a lease and takes
// the subscription out of the due window, so a sequential re-scan cannot pick it
// up again while the charge is in flight.
func TestIssue534ClaimAdvancesNextCharge(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ss := NewSubscriptionStore(p)
	acc := uniqueAccount(t, ctx, p)
	subID := newDueSubscriptionFixture(t, ctx, p, acc, "lease@example.test")

	won, err := ss.ClaimDue(ctx, subID, time.Hour)
	if err != nil || !won {
		t.Fatalf("first ClaimDue = (%v, %v), want (true, nil)", won, err)
	}

	// The sweep's own query must no longer see it as due.
	due, err := ss.DueActive(ctx, time.Now().Add(time.Minute), 200)
	if err != nil {
		t.Fatalf("due scan: %v", err)
	}
	for _, d := range due {
		if d.ID == subID {
			t.Fatal("issue #534: a claimed subscription is still returned by the due scan — " +
				"the next sweep would charge it again while the first charge is pending")
		}
	}

	// And the lease expires, so a crashed attempt is retried rather than lost.
	var next time.Time
	if err := p.QueryRow(ctx, `SELECT next_charge_at FROM subscriptions WHERE id=$1`, subID).Scan(&next); err != nil {
		t.Fatalf("read next_charge_at: %v", err)
	}
	if !next.After(time.Now()) {
		t.Fatalf("claimed next_charge_at = %s, want a future lease expiry", next)
	}
}

// TestIssue534ClaimRefusedWhenNotDue confirms the claim cannot be used to charge
// a subscription that is not due (e.g. a stale object passed by a caller).
func TestIssue534ClaimRefusedWhenNotDue(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ss := NewSubscriptionStore(p)
	acc := uniqueAccount(t, ctx, p)
	subID := newDueSubscriptionFixture(t, ctx, p, acc, "notdue@example.test")

	if err := ss.ScheduleNextCharge(ctx, subID, time.Now().Add(24*time.Hour)); err != nil {
		t.Fatalf("push next charge out: %v", err)
	}
	won, err := ss.ClaimDue(ctx, subID, time.Hour)
	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if won {
		t.Fatal("issue #534: ClaimDue won for a subscription that is not due")
	}
}

// TestIssue535FailedAttemptsCountsFailures is the retry-ladder guard.
func TestIssue535FailedAttemptsCountsFailures(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ss := NewSubscriptionStore(p)
	acc := uniqueAccount(t, ctx, p)
	subID := newDueSubscriptionFixture(t, ctx, p, acc, "attempts@example.test")

	if err := ss.AddCharge(ctx, subID, "", 1500, ChargeFailed, 1); err != nil {
		t.Fatalf("add failed 1: %v", err)
	}
	if err := ss.AddCharge(ctx, subID, "", 1500, ChargeFailed, 2); err != nil {
		t.Fatalf("add failed 2: %v", err)
	}
	got, err := ss.FailedAttempts(ctx, subID)
	if err != nil {
		t.Fatalf("FailedAttempts errored (it must query charged_at, which exists): %v", err)
	}
	if got != 2 {
		t.Fatalf("issue #535: FailedAttempts = %d after two failures, want 2 — with 0 the "+
			"attempt counter never reaches maxChargeAttempts, so past_due / card-update / "+
			"auto-cancel stay unreachable and a dead card retries forever", got)
	}
}

// TestIssue535FailedAttemptsResetsAfterPaid pins the intended window: failures
// are counted only since the last successful charge.
func TestIssue535FailedAttemptsResetsAfterPaid(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ss := NewSubscriptionStore(p)
	acc := uniqueAccount(t, ctx, p)
	subID := newDueSubscriptionFixture(t, ctx, p, acc, "reset@example.test")

	if err := ss.AddCharge(ctx, subID, "pur_535a", 1500, ChargeFailed, 1); err != nil {
		t.Fatalf("add failed: %v", err)
	}
	if err := ss.AddCharge(ctx, subID, "pur_535b", 1500, ChargePending, 1); err != nil {
		t.Fatalf("add pending: %v", err)
	}
	if err := ss.MarkChargePaid(ctx, "pur_535b"); err != nil {
		t.Fatalf("mark paid: %v", err)
	}
	time.Sleep(20 * time.Millisecond) // ensure the paid row is strictly later
	if err := ss.AddCharge(ctx, subID, "pur_535c", 1500, ChargeFailed, 1); err != nil {
		t.Fatalf("add failed after paid: %v", err)
	}

	got, err := ss.FailedAttempts(ctx, subID)
	if err != nil {
		t.Fatalf("FailedAttempts: %v", err)
	}
	if got != 1 {
		t.Fatalf("FailedAttempts after a paid charge then one failure = %d, want 1", got)
	}
}

// TestIssue535ChargeCountReportsPaid proves the billing-count source no longer
// fails to 0, which would let a fixed-billing subscription over-charge.
func TestIssue535ChargeCountReportsPaid(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ss := NewSubscriptionStore(p)
	acc := uniqueAccount(t, ctx, p)
	subID := newDueSubscriptionFixture(t, ctx, p, acc, "chargecount@example.test")

	if err := ss.AddCharge(ctx, subID, "pur_cnt_a", 1500, ChargePending, 1); err != nil {
		t.Fatalf("add pending a: %v", err)
	}
	if err := ss.AddCharge(ctx, subID, "pur_cnt_b", 1500, ChargePending, 1); err != nil {
		t.Fatalf("add pending b: %v", err)
	}
	if err := ss.MarkChargePaid(ctx, "pur_cnt_a"); err != nil {
		t.Fatalf("mark paid a: %v", err)
	}
	if err := ss.MarkChargePaid(ctx, "pur_cnt_b"); err != nil {
		t.Fatalf("mark paid b: %v", err)
	}

	got, err := ss.ChargeCount(ctx, subID)
	if err != nil {
		t.Fatalf("ChargeCount: %v", err)
	}
	if got != 2 {
		t.Fatalf("issue #535: ChargeCount = %d after two paid charges, want 2 — a silent 0 "+
			"reads as 'nothing consumed yet', so a fixed-billing subscription would be "+
			"charged past its Nth charge instead of completing", got)
	}
}
