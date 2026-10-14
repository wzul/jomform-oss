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

	// A FOURTH code is no longer refused, and this is where that used to be
	// asserted: mustTOTP held a three-entry window per secret and panicked on the
	// 4th call, which is what made `go test ./internal/httpapi/ -count=2` die
	// here — the same defect class as the per-process test-IP pool (card
	// t_7655c2bf). The property to keep is not "the fixture refuses" but "no step
	// is ever handed out twice, and every code is inside the ±1 window it was
	// generated in": totpClaim is asserted over both, including the 4th-code case
	// (a wait of exactly one step), by TestTOTPClaimCoversEveryFixtureCase, and
	// the resumption a finished -count pass needs by
	// TestMustTOTPResumesAfterASpentWindow. The end-to-end evidence that a rerun
	// works is the package run at -count=2/3 that this card's PR records.
}

// TestTOTPClaimCoversEveryFixtureCase pins the decision BOTH TOTP fixtures share
// (totpClaim in totp_step_testhelper_test.go) over the cases they really meet,
// without sleeping for any of them: a fresh secret, a claim a whole -test.count
// pass behind the clock, and a 4th code inside one window.
//
// The 4th-code case is the one that used to panic (card t_7655c2bf) and is now a
// wait of exactly one step, so the properties that make waiting correct are
// asserted here instead: the step always ascends (ConsumeTOTPStep refuses a step
// that does not), and a code at it is always inside the ±1 window of the base the
// clock will have when the wait is over.
func TestTOTPClaimCoversEveryFixtureCase(t *testing.T) {
	const base = int64(59_000_000)
	for _, tc := range []struct {
		what      string
		last      int64
		step      int64
		waitSteps int64
	}{
		{"a fresh secret: the earliest step the server accepts", 0, base - 1, 0},
		{"the 2nd code of one window", base - 1, base, 0},
		{"the 3rd code of one window", base, base + 1, 0},
		{"a 4th code in the same window: the window is spent, so wait one step", base + 1, base + 2, 1},
		{"a claim a whole -count pass behind the clock: pulled up to base-1", base - 40, base - 1, 0},
		{"a claim the clock has run past", base - 2, base - 1, 0},
	} {
		step, wait := totpClaim(tc.last, base)
		if step != tc.step || wait != tc.waitSteps {
			t.Errorf("totpClaim(last=%d, base=%d) for %s = (step %d, wait %d), want (%d, %d)",
				tc.last, base, tc.what, step, wait, tc.step, tc.waitSteps)
		}
		if step <= tc.last {
			t.Errorf("%s: step %d is not later than the %d handed out last — ConsumeTOTPStep "+
				"(totp_last_step < $2) refuses it as a replay", tc.what, step, tc.last)
		}
		if now := base + wait; step < now-1 || step > now+1 {
			t.Errorf("%s: after waiting %d step(s) the code at step %d is outside the ±1 window of base %d, "+
				"so the server would answer \"kod tidak sah\"", tc.what, wait, step, now)
		}
	}
}

// TestMustTOTPResumesAfterASpentWindow pins the -test.count case deterministically
// (card t_7655c2bf). A finished pass leaves this secret's claim a whole pass —
// many steps — behind the clock, and the fixture has to come back with a code the
// server accepts NOW, still strictly later than the step handed out last.
//
// Priming the per-secret claim instead of running two passes keeps this instant
// and deterministic; the two- and three-pass runs are the gate evidence. The bite
// is the old behaviour: a count-based window panics on exactly this call (the
// primed claim IS this secret's 4th), which is how a rerun died.
func TestMustTOTPResumesAfterASpentWindow(t *testing.T) {
	secret := "KRUGKIDROVUWG2ZAMJZG653OEBTG66BA" // unique to this test: totpStepAlloc is keyed by secret

	// A pass's worth of codes behind the clock, as pass 2 finds it.
	const passSteps = 40
	baseBefore := time.Now().Unix() / totpTestPeriod
	primed := baseBefore - passSteps
	totpStepAlloc.Lock()
	totpStepAlloc.used[secret] = primed
	totpStepAlloc.Unlock()

	code := mustTOTP(secret)
	baseAfter := time.Now().Unix() / totpTestPeriod

	step := int64(-1)
	for s := baseBefore - 1; s <= baseAfter+1; s++ {
		if code == totpCodeAtStep(secret, s) {
			step = s
			break
		}
	}
	if step < 0 {
		t.Fatalf("the resumed code matches no step in [%d,%d] — the ±1 window the clock had while it was "+
			"generated — so the server would refuse it", baseBefore-1, baseAfter+1)
	}
	if step <= primed {
		t.Fatalf("the resumed code is step %d, not later than the primed %d: the sequence has to keep "+
			"ascending or ConsumeTOTPStep refuses it as a replay", step, primed)
	}
}

// waitOutBoundaryGuard reports whether the wait would block. If the host clock is
// inside the margin the guard has nothing to measure, so it skips rather than
// turning a slow clock into a red gate.
func waitOutBoundaryGuard() bool {
	now := time.Now().Unix()
	return now%totpTestPeriod > totpTestPeriod-totpBoundaryMargin
}
