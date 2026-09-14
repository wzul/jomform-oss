package dbstore

// Issue #545: the storage quota must be enforced ATOMICALLY.
//
// The upload handlers used to read the used total, compare against the cap, and
// then store — a check-then-act with nothing serialising the read against the
// write, so concurrent uploads all passed against the same snapshot and
// collectively exceeded the cap (measured 1.39x with 5 concurrent 100MB uploads).
//
// These tests drive the real store layer against the real database. Each FAILS
// against the pre-fix code (which had no ReserveQuota at all).

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// TestIssue545ReserveQuotaIsAtomic is the core guard: N concurrent reservations
// must not collectively exceed the cap.
func TestIssue545ReserveQuotaIsAtomic(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ms := NewMediaStore(p)
	acc := uniqueAccount(t, ctx, p)

	const cap = MaxWorkspaceBytesFromStore
	each := int64(100 << 20) // 100MB
	// Start at cap minus one upload, so exactly ONE reservation can succeed.
	start := cap - each
	if err := setUsedBytes(ctx, t, p, acc, start); err != nil {
		t.Fatalf("seed used bytes: %v", err)
	}

	const concurrent = 8
	var wg sync.WaitGroup
	results := make([]error, concurrent)
	barrier := make(chan struct{})
	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-barrier // release all at once to maximise interleaving
			results[i] = ms.ReserveQuota(ctx, acc, each)
		}(i)
	}
	close(barrier)
	wg.Wait()

	granted := 0
	for i, err := range results {
		switch {
		case err == nil:
			granted++
		case errors.Is(err, ErrQuotaExceeded):
			// expected for the losers
		default:
			t.Fatalf("goroutine %d: unexpected error: %v", i, err)
		}
	}
	if granted != 1 {
		t.Fatalf("issue #545: %d of %d concurrent reservations succeeded, want exactly 1 — "+
			"the cap must hold under concurrency", granted, concurrent)
	}

	used, err := ms.UsedBytes(ctx, acc)
	if err != nil {
		t.Fatalf("used bytes: %v", err)
	}
	if used > cap {
		t.Fatalf("issue #545: used bytes %d exceed the cap %d", used, cap)
	}
	if used != start+each {
		t.Fatalf("used = %d, want %d (exactly one reservation granted)", used, start+each)
	}
}

// TestIssue545ReserveQuotaRefusesOverCap proves a single over-cap reservation is
// refused outright.
func TestIssue545ReserveQuotaRefusesOverCap(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ms := NewMediaStore(p)
	acc := uniqueAccount(t, ctx, p)

	if err := setUsedBytes(ctx, t, p, acc, MaxWorkspaceBytesFromStore-10); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := ms.ReserveQuota(ctx, acc, 100); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("issue #545: reserving 100 bytes above the cap returned %v, want ErrQuotaExceeded", err)
	}
	// Exactly to the cap is allowed (the cap is inclusive).
	if err := ms.ReserveQuota(ctx, acc, 10); err != nil {
		t.Fatalf("reserving exactly to the cap should succeed, got %v", err)
	}
	// One byte more must fail.
	if err := ms.ReserveQuota(ctx, acc, 1); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("reserving past the cap returned %v, want ErrQuotaExceeded", err)
	}
}

// TestIssue545ReleaseQuotaFreesBytes proves a failed upload does not leak quota.
func TestIssue545ReleaseQuotaFreesBytes(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ms := NewMediaStore(p)
	acc := uniqueAccount(t, ctx, p)

	if err := setUsedBytes(ctx, t, p, acc, 0); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := ms.ReserveQuota(ctx, acc, 5<<20); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if used, _ := ms.UsedBytes(ctx, acc); used != 5<<20 {
		t.Fatalf("used after reserve = %d, want %d", used, 5<<20)
	}
	if err := ms.ReleaseQuota(ctx, acc, 5<<20); err != nil {
		t.Fatalf("release: %v", err)
	}
	if used, _ := ms.UsedBytes(ctx, acc); used != 0 {
		t.Fatalf("used after release = %d, want 0", used)
	}
	// Release clamps at zero (never negative).
	if err := ms.ReleaseQuota(ctx, acc, 1<<30); err != nil {
		t.Fatalf("release over: %v", err)
	}
	if used, _ := ms.UsedBytes(ctx, acc); used != 0 {
		t.Fatalf("used after over-release = %d, want 0 (clamped)", used)
	}
}

// TestIssue545SettleAccountsForRealBytes proves the settle step records the true
// total (base + variants), which is the gap that made ~64% of real bytes
// invisible to the quota.
func TestIssue545SettleAccountsForRealBytes(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ms := NewMediaStore(p)
	acc := uniqueAccount(t, ctx, p)

	base := int64(1 << 20)
	variants := int64(700 << 10)
	if err := ms.ReserveQuota(ctx, acc, base); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if err := ms.SettleQuotaBytes(ctx, acc, base, base+variants); err != nil {
		t.Fatalf("settle: %v", err)
	}
	used, err := ms.UsedBytes(ctx, acc)
	if err != nil {
		t.Fatalf("used: %v", err)
	}
	if used != base+variants {
		t.Fatalf("issue #545: used = %d, want %d — variant bytes must be counted", used, base+variants)
	}
	// Settling downward (fewer bytes than reserved) refunds the difference.
	if err := ms.SettleQuotaBytes(ctx, acc, base+variants, base); err != nil {
		t.Fatalf("settle down: %v", err)
	}
	if used, _ := ms.UsedBytes(ctx, acc); used != base {
		t.Fatalf("used after downward settle = %d, want %d", used, base)
	}
}

// TestIssue545ReconcileRepairsDrift proves the counter can be rebuilt from the
// media rows, which is the repair path for a crash between reserve and store.
func TestIssue545ReconcileRepairsDrift(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ms := NewMediaStore(p)
	acc := uniqueAccount(t, ctx, p)

	// Real stored rows totalling 300 bytes.
	for _, size := range []int64{100, 200} {
		m := &Media{
			AccountID: acc, Key: "u/" + acc + "/" + randSuffix() + ".png",
			URL: "https://media.test/x.png", Mime: "image/png",
			Size: size, Source: MediaSourceMerchant,
		}
		if err := ms.Create(ctx, m); err != nil {
			t.Fatalf("create media: %v", err)
		}
	}
	// Counter drifted high (e.g. a leaked reservation).
	if err := setUsedBytes(ctx, t, p, acc, 999999); err != nil {
		t.Fatalf("seed drift: %v", err)
	}
	before, after, err := ms.ReconcileQuotaBytes(ctx, acc)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if before != 999999 {
		t.Fatalf("before = %d, want 999999", before)
	}
	if after != 300 {
		t.Fatalf("issue #545: after = %d, want 300 (sum of the media rows)", after)
	}
}

// TestIssue545CapConstantMatchesObjstore pins the duplicated constant so the
// store-side guard can never drift from the shared objstore cap.
func TestIssue545CapConstantMatchesObjstore(t *testing.T) {
	if MaxWorkspaceBytesFromStore != objstoreMaxWorkspaceBytesForTest {
		t.Fatalf("issue #545: dbstore cap %d != objstore cap %d — the duplicated constant has drifted",
			MaxWorkspaceBytesFromStore, objstoreMaxWorkspaceBytesForTest)
	}
}
