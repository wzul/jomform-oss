package dbstore

// Issue #528: TOTP replay protection. auth.ValidateTOTPSecret was stateless —
// totp.ValidateCustom accepts any code in the current 30s step ±1, so the SAME
// code verified repeatedly for its whole ~90s window. users.totp_last_step lets
// validation accept a step only once.
//
// These tests cover the storage-layer half of the fix: the atomic
// "advance only forward" consume. The handler half (all three call sites go
// through one shared helper) is covered in httpapi.
//
// Integration test; skipped unless JOMFORM_TEST_DATABASE_URL is set.

import (
	"context"
	"testing"
)

// TestIssue528ConsumeTOTPStepIsSingleUse is the core guard: the same step
// cannot be consumed twice.
func TestIssue528ConsumeTOTPStepIsSingleUse(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	us := NewUserStore(p)
	acc := uniqueAccount(t, ctx, p) // ensures the pool/fixtures are ready
	_ = acc

	u, err := us.Create(ctx, "totp528-"+randSuffix()+"@example.test", "x")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	step := int64(59_000_123)

	// First consume of a step must succeed.
	ok, err := us.ConsumeTOTPStep(ctx, u.ID, step)
	if err != nil {
		t.Fatalf("first consume: %v", err)
	}
	if !ok {
		t.Fatalf("issue #528: the first consume of step %d returned false — a valid code would be rejected", step)
	}

	// The SAME step must now be refused (this is the replay).
	ok, err = us.ConsumeTOTPStep(ctx, u.ID, step)
	if err != nil {
		t.Fatalf("replay consume: %v", err)
	}
	if ok {
		t.Fatalf("issue #528: consuming step %d twice returned true — the same TOTP code is replayable", step)
	}

	// An OLDER step must also be refused (a code from a previous window).
	ok, err = us.ConsumeTOTPStep(ctx, u.ID, step-1)
	if err != nil {
		t.Fatalf("older-step consume: %v", err)
	}
	if ok {
		t.Fatalf("issue #528: consuming an older step (%d) returned true — a code from a previous window is replayable", step-1)
	}

	// A LATER step must still succeed, so the guard does not break normal use.
	ok, err = us.ConsumeTOTPStep(ctx, u.ID, step+1)
	if err != nil {
		t.Fatalf("later-step consume: %v", err)
	}
	if !ok {
		t.Fatalf("consuming a later step (%d) returned false — a legitimate subsequent login would be blocked", step+1)
	}
}

// TestIssue528ConsumeTOTPStepConcurrentSameCodeSettleOnce proves the predicate
// is atomic: two concurrent submissions of the same code must not both pass.
// The guard is in the UPDATE's WHERE clause, so the database serializes them and
// exactly one gets RowsAffected()==1.
func TestIssue528ConsumeTOTPStepConcurrentSameCodeSettleOnce(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	us := NewUserStore(p)

	u, err := us.Create(ctx, "totp528c-"+randSuffix()+"@example.test", "x")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	step := int64(59_111_222)

	type res struct {
		ok  bool
		err error
	}
	results := make(chan res, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			ok, err := us.ConsumeTOTPStep(ctx, u.ID, step)
			results <- res{ok, err}
		}()
	}
	close(start)

	succeeded := 0
	for i := 0; i < 2; i++ {
		r := <-results
		if r.err != nil {
			t.Fatalf("concurrent consume: %v", r.err)
		}
		if r.ok {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("issue #528: %d of 2 concurrent consumes of the SAME code succeeded, want exactly 1", succeeded)
	}
}

// TestIssue528ConsumeTOTPStepPersistsAcrossReload confirms the consumed step is
// durable state on the user row (not just an in-process guard), so a restart or
// a different API instance still sees the code as used.
func TestIssue528ConsumeTOTPStepPersistsAcrossReload(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	us := NewUserStore(p)

	u, err := us.Create(ctx, "totp528p-"+randSuffix()+"@example.test", "x")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	// A fresh user has consumed nothing.
	if u.TOTPLastStep != 0 {
		t.Fatalf("new user totp_last_step = %d, want 0", u.TOTPLastStep)
	}

	step := int64(59_222_333)
	if _, err := us.ConsumeTOTPStep(ctx, u.ID, step); err != nil {
		t.Fatalf("consume: %v", err)
	}

	// Reload through the normal select (this also proves the column is present
	// in userSelect and every scan site stays column-aligned).
	got, err := us.ByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.TOTPLastStep != step {
		t.Fatalf("reloaded totp_last_step = %d, want %d (column not selected/scanned?)", got.TOTPLastStep, step)
	}

	// And ByEmail must agree, so a login-path load sees the consumed step too.
	byEmail, err := us.ByEmail(ctx, got.Email)
	if err != nil {
		t.Fatalf("ByEmail: %v", err)
	}
	if byEmail.TOTPLastStep != step {
		t.Fatalf("ByEmail totp_last_step = %d, want %d", byEmail.TOTPLastStep, step)
	}
}
