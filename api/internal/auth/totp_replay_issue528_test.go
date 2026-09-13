package auth

// Issue #528: ValidateTOTPSecret was stateless, so the same code verified
// repeatedly for its whole drift window. ValidateTOTPSecretStep now returns the
// matched time-step so a caller can consume it and make the code single-use.
//
// These tests pin the step-reporting contract the handler layer relies on.

import (
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

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
func TestIssue528StepToleranceUnchanged(t *testing.T) {
	secret, _, err := GenerateTOTPSecret("window@example.test")
	if err != nil {
		t.Fatalf("generate secret: %v", err)
	}
	now := timeNow()
	base := now.Unix() / totpPeriod

	// Codes one step either side must be accepted (Skew: 1 behaviour preserved).
	for _, delta := range []int64{-1, 0, 1} {
		at := time.Unix((base+delta)*totpPeriod, 0)
		code := fixedStepCode(t, secret, at)
		if _, ok := ValidateTOTPSecretStep(secret, code); !ok {
			t.Fatalf("code from step %+d was rejected; the ±1 drift window changed", delta)
		}
	}
	// A code two steps away must NOT be accepted.
	at := time.Unix((base+2)*totpPeriod, 0)
	far := fixedStepCode(t, secret, at)
	if _, ok := ValidateTOTPSecretStep(secret, far); ok {
		t.Fatalf("code from step +2 was accepted; the drift window widened")
	}
}
