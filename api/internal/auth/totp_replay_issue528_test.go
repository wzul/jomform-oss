package auth

// Issue #528: ValidateTOTPSecret was stateless, so the same code verified
// repeatedly for its whole drift window. ValidateTOTPSecretStep now returns the
// matched time-step so a caller can consume it and make the code single-use.
//
// These tests pin the step-reporting contract the handler layer relies on.
//
// Each test here PINS the package clock (pinTOTPClock). That is not decoration:
// every one of them derives a code from one timeNow() read and lets
// ValidateTOTPSecretStep take its own, and a 30s period boundary falling between
// the two reads moves the validator's base one step relative to the codes, so the
// ±1 window is applied to a code built for a different step. That is how
// TestIssue528StepToleranceUnchanged came to report "code from step +2 was
// accepted; the drift window widened" on a contended host (card t_6bf6c940) —
// nothing had widened; the clock had moved between the reads.

import (
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// totpPinnedNow is the instant these tests pin the clock to. It is deliberately
// one second BEFORE a 30s period boundary — 1577836799 = 30*52594560 - 1, i.e.
// 2019-12-31T23:59:59Z — the phase at which the old, unpinned test split across a
// boundary. It is also years away from the live wall clock on purpose: if the pin
// is ever dropped, the codes generated below sit in a step the live validator is
// nowhere near and the ±1 case fails outright rather than flaking on whatever
// phase the wall clock happened to be at.
var totpPinnedNow = time.Unix(1577836799, 0)

// fixedStepCode generates the code for secret at a given absolute time.
func fixedStepCode(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	code, err := totp.GenerateCodeCustom(secret, at, totp.ValidateOpts{
		Period:    totpPeriod,
		Skew:      0,
		Digits:    6,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	return code
}

// TestIssue528ValidateReportsMatchedStep is the core contract: a valid code
// returns the step it matched, so the caller has something to consume.
func TestIssue528ValidateReportsMatchedStep(t *testing.T) {
	secret, _, err := GenerateTOTPSecret("replay@example.test")
	if err != nil {
		t.Fatalf("generate secret: %v", err)
	}
	// Pin before the first read: the code below is built from this test's read
	// and the window is decided by ValidateTOTPSecretStep's own read.
	pinTOTPClock(t, totpPinnedNow)

	now := timeNow()
	code := fixedStepCode(t, secret, now)
	wantStep := now.Unix() / totpPeriod

	step, ok := ValidateTOTPSecretStep(secret, code)
	if !ok {
		t.Fatalf("issue #528: ValidateTOTPSecretStep rejected a valid current code")
	}
	if step != wantStep {
		t.Fatalf("matched step = %d, want %d", step, wantStep)
	}

	// An empty secret/code must not report a step.
	if _, ok := ValidateTOTPSecretStep("", code); ok {
		t.Fatalf("empty secret accepted")
	}
	if _, ok := ValidateTOTPSecretStep(secret, ""); ok {
		t.Fatalf("empty code accepted")
	}
	if _, ok := ValidateTOTPSecretStep(secret, "000000"); ok && "000000" != code {
		// "000000" is a 1-in-a-million fluke; only fail if it is not the real code.
		t.Fatalf("invalid code accepted")
	}
}

// TestIssue528SameCodeValidatesRepeatedlyWithoutConsumption documents the BUG
// being fixed rather than asserting the old behaviour is good: the validator
// itself is deliberately stateless (that is the library's contract), which is
// exactly why the caller MUST consume the step. It proves that validation alone
// is not single-use — the property comes from ConsumeTOTPStep in the store,
// asserted in dbstore's TestIssue528ConsumeTOTPStepIsSingleUse.
func TestIssue528SameCodeValidatesRepeatedlyWithoutConsumption(t *testing.T) {
	secret, _, err := GenerateTOTPSecret("replay2@example.test")
	if err != nil {
		t.Fatalf("generate secret: %v", err)
	}
	// Both validations below read the clock; pin it so the two reads and the
	// code's step cannot be separated by a period boundary.
	pinTOTPClock(t, totpPinnedNow)
	code := fixedStepCode(t, secret, timeNow())

	first, ok1 := ValidateTOTPSecretStep(secret, code)
	second, ok2 := ValidateTOTPSecretStep(secret, code)
	if !ok1 || !ok2 {
		t.Fatalf("expected the code to validate (this is the stateless validator)")
	}
	if first != second {
		t.Fatalf("the same code reported different steps (%d vs %d)", first, second)
	}
	// The point: the validator says yes twice, so single-use MUST be enforced by
	// consuming the reported step. If a future change ever makes validation
	// itself stateful, this test tells us the contract moved.
}

// TestIssue528StepToleranceUnchanged pins that the drift window is still the
// current step ±1 — the fix must not narrow or widen accepted codes.
//
// The clock is PINNED and the phase is swept across the period because of how
// this test used to fail. It read the wall clock to build the step-(base±1) and
// step-(base+2) codes, and ValidateTOTPSecretStep (totp.go:52, `now := timeNow()`)
// read it AGAIN to decide the window. A boundary between the two reads makes the
// validator's base one step later than the base the codes were built from, so the
// "two steps away" code is only one step away from the validator's now, the ±1
// window accepts it, and the failure blames the window ("the drift window
// widened") when nothing widened. The margin is not microseconds either: under a
// full `go test ./... -race` on a contended host a goroutine can be descheduled
// for tens of milliseconds between those two reads.
//
// One pinned instant for both the codes and the validation means no boundary can
// fall between them, and sweeping the phase (including the second before a
// boundary, the position that used to break) asserts the window is the same
// wherever in the period the clock sits. The ±1 set itself is untouched.
func TestIssue528StepToleranceUnchanged(t *testing.T) {
	secret, _, err := GenerateTOTPSecret("window@example.test")
	if err != nil {
		t.Fatalf("generate secret: %v", err)
	}
	pinnedBase := totpPinnedNow.Unix() / totpPeriod
	for _, phase := range []int64{0, 1, totpPeriod / 2, totpPeriod - 1} {
		now := time.Unix(pinnedBase*totpPeriod+phase, 0)
		pinTOTPClock(t, now)
		base := now.Unix() / totpPeriod

		// Codes one step either side must be accepted (Skew: 1 behaviour preserved).
		for _, delta := range []int64{-1, 0, 1} {
			at := time.Unix((base+delta)*totpPeriod, 0)
			code := fixedStepCode(t, secret, at)
			if _, ok := ValidateTOTPSecretStep(secret, code); !ok {
				t.Fatalf("%ds into the period: code from step %+d was rejected; the ±1 drift window changed", phase, delta)
			}
		}
		// A code two steps away must NOT be accepted.
		at := time.Unix((base+2)*totpPeriod, 0)
		far := fixedStepCode(t, secret, at)
		if _, ok := ValidateTOTPSecretStep(secret, far); ok {
			t.Fatalf("%ds into the period: code from step +2 was accepted; the drift window widened", phase)
		}
	}
}
