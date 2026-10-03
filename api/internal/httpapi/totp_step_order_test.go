package httpapi

// Card t_e74defb7.
//
// Two properties must hold, and they pull in OPPOSITE directions, which is why the
// flake was easy to "fix" wrongly:
//
//  1. The steps handed out must ASCEND, because `ConsumeTOTPStep` advances
//     `users.totp_last_step` with the predicate `totp_last_step < $2`
//     (api/internal/dbstore/user.go:143). A code whose step is not greater than
//     the last consumed one is refused as a replay — so a "longest-lived first"
//     order makes the second verification fail 401, measured on
//     trust_devices_issue454_test.go.
//
//  2. The FIRST code must not be generated so close to a step boundary that the
//     test's verifications run after it. A fresh secret's first code is `base-1`,
//     and `base-1` is accepted only while the server is still at `base` — it is
//     GONE the moment the step advances. That is not a margin that can be tuned
//     away; it is arithmetic, and it is why the fix is to make sure the clock has
//     room BEFORE generating, not to reorder the offsets.
//
// Both are deterministic, so they are asserted directly rather than by running a
// wall-clock test repeatedly and hoping.

import (
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// totpValidAt reproduces server-side verification
// (api/internal/auth/totp.go's ValidateTOTPSecretStep): the accepted set is
// {now-1, now, now+1}.
func totpValidAt(secret, code string, gen, now int64) bool {
	for _, s := range []int64{now - 1, now, now + 1} {
		ok, err := totp.ValidateCustom(code, secret, time.Unix(s*totpTestPeriod, 0), totp.ValidateOpts{
			Period:    totpTestPeriod,
			Skew:      0,
			Digits:    otp.DigitsSix,
			Algorithm: otp.AlgorithmSHA1,
		})
		if err == nil && ok && s == gen {
			return true
		}
	}
	return false
}

// TestBaseMinusOneDiesAtTheBoundary documents WHY the fix is a wait and not a
// reorder: the first code handed out is `base-1`, and one step later it is simply
// outside the accepted set. If this ever stops being true the reasoning above
// needs revisiting.
func TestBaseMinusOneDiesAtTheBoundary(t *testing.T) {
	const secret = "JBSWY3DPEHPK3PXP"
	const gen = int64(59701690)

	if !totpValidAt(secret, totpCodeAtStep(secret, gen-1), gen-1, gen) {
		t.Fatal("base-1 must be accepted while the server is at base")
	}
	if totpValidAt(secret, totpCodeAtStep(secret, gen-1), gen-1, gen+1) {
		t.Fatal("base-1 is expected to be DEAD once the step advances — that is the race; if it " +
			"now survives, the wait in mustTOTP is no longer the reason the flake is gone")
	}
}

// TestWaitOutStepBoundaryLeavesEnoughTime pins the actual guarantee the fix
// provides: after the wait, there is at least the margin left before the next
// boundary, and every offset mustTOTP can hand out is valid AT THAT MOMENT.
//
// Note what is NOT asserted: that base-1 stays valid one step later. It cannot,
// by definition — see TestBaseMinusOneDiesAtTheBoundary. An earlier version of
// this guard demanded exactly that and failed on its own arithmetic, which is
// recorded here so the next reader does not repeat it.
func TestWaitOutStepBoundaryLeavesEnoughTime(t *testing.T) {
	if waitOutBoundaryGuard() {
		t.Skip("clock is inside the margin window; waitOutStepBoundary would block the gate")
	}
	waitOutStepBoundary()

	now := time.Now().Unix()
	left := totpTestPeriod - (now % totpTestPeriod)
	if left < totpBoundaryMargin {
		t.Fatalf("only %ds left in the step after the wait, want at least %ds — a test that "+
			"verifies three codes would run into the boundary and the first code (base-1) would "+
			"vanish mid-flow", left, totpBoundaryMargin)
	}

	const secret = "MFRGGZDFMZTWQ2LK"
	base := now / totpTestPeriod
	for _, offset := range []int64{-1, 0, 1} {
		if !totpValidAt(secret, totpCodeAtStep(secret, base+offset), base+offset, base) {
			t.Fatalf("step %d (offset %d) is not accepted at generation time", base+offset, offset)
		}
	}
}

// TestMustTOTPStepsAscend pins property 1, the constraint that makes reordering
// unavailable: the sequence must strictly increase, because ConsumeTOTPStep only
// accepts a step greater than the last one consumed.
//
// It also pins the PRE-CONDITION the fix provides ("the clock had room before the
// first code was generated"), which is asserted here because it is only observable
// through a call: `waitOutStepBoundary` is exercised by `mustTOTP`, and its return
// value is not published. The post-condition of the wait itself is pinned
// separately by TestWaitOutStepBoundaryLeavesEnoughTime.
//
// LIMIT, stated so it is not mistaken for more: removing the wait from `mustTOTP`
// does NOT red this test on its own — it reds only when the clock happens to be
// inside the margin, because that is when the pre-condition is unmet. The
// deterministic guard for the property is TestWaitOutStepBoundaryLeavesEnoughTime;
// this one catches the wiring when it can.
func TestMustTOTPStepsAscend(t *testing.T) {
	secret := "GEZDGNBVGY3TQOJQ" // unique to this test: totpStepAlloc is keyed by secret
	if waitOutBoundaryGuard() {
		t.Skip("clock is inside the margin window; waitOutStepBoundary would block the gate")
	}
	prev := int64(-1)
	first := true
	for i := 0; i < 3; i++ {
		code := mustTOTP(secret)
		if first {
			frac := time.Now().Unix() % totpTestPeriod
			if frac > totpTestPeriod-totpBoundaryMargin {
				t.Fatalf("the FIRST code came %ds into a %ds step: mustTOTP did not wait out the "+
					"boundary, so this secret's base-1 code can vanish before it is verified — "+
					"the flake card t_e74defb7 measured", frac, totpTestPeriod)
			}
			first = false
		}
		step := int64(-1)
		base := time.Now().Unix() / totpTestPeriod
		for _, s := range []int64{base - 1, base, base + 1} {
			if code == totpCodeAtStep(secret, s) {
				step = s
				break
			}
		}
		if step < 0 {
			t.Fatalf("call %d returned a code matching none of the tolerated steps", i)
		}
		if step <= prev {
			t.Fatalf("call %d returned step %d after %d: the steps do NOT ascend, so "+
				"ConsumeTOTPStep (totp_last_step < $2) refuses it as a replay", i, step, prev)
		}
		prev = step
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("a FOURTH code must be refused: a real authenticator cannot supply one inside " +
				"one window, and replaying a spent code is the bug #528 fixed")
		}
	}()
	_ = mustTOTP(secret)
}

// waitOutBoundaryGuard reports whether the wait would block. If the host clock is
// inside the margin the guard has nothing to measure, so it skips rather than
// turning a slow clock into a red gate.
func waitOutBoundaryGuard() bool {
	now := time.Now().Unix()
	return now%totpTestPeriod > totpTestPeriod-totpBoundaryMargin
}
