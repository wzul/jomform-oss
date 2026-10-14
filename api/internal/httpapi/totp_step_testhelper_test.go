package httpapi

// Issue #528 helper.
//
// Before #528, TOTP validation was stateless: the same 6-digit code verified
// repeatedly for its whole ~90s window. Tests relied on that — they generated
// ONE code with mustTOTP(secret) and reused it for the 2FA-enable confirmation
// and then for each subsequent login.
//
// #528 correctly makes a code single-use (a replay is refused), so a test that
// drives several verifications must supply a DIFFERENT code each time, exactly
// as a real user's authenticator app does. mustTOTP now hands out successive
// time-steps for a given secret instead of repeating one code, which keeps every
// existing call site correct and makes the tests model real behaviour.
//
// The validator tolerates the current 30s step ±1, so three codes are available
// per secret per window. A test needing a fourth inside one window is asking for
// something a real authenticator cannot supply, so it fails loudly.

import (
	"sync"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// totpTestPeriod mirrors auth's 30s period (the library default).
const totpTestPeriod = 30

// totpStepAlloc tracks, per secret, the last time-step a code was handed out for
// (0 when none has been), so each call returns a code from a strictly later step
// — the property `ConsumeTOTPStep` enforces (`totp_last_step < $2`). It is a
// STEP, not a count: both helpers that draw from it (mustTOTP in auth_ux_test.go
// and totpCode in auth_totp_test.go) have to agree on that, because the window a
// code is valid in moves with the clock, and a count cannot tell whether a
// secret's codes are behind it (a later -test.count pass) or ahead of it (a 4th
// code inside one window).
var totpStepAlloc = struct {
	sync.Mutex
	used map[string]int64
}{used: map[string]int64{}}

// totpClaim is the pure decision behind the TOTP fixtures: given the last step
// handed out for a secret (0 when none has been) and the clock's current step,
// it returns the step to hand out next and how many step boundaries to wait for
// before a code at that step is inside the server's ±1 window (0 = generate now).
//
// Two rules, and they are the whole design:
//
//   - The step MUST ascend: one step after the last one handed out is the
//     earliest choice (`ConsumeTOTPStep` refuses anything else). The earliest
//     step the server still accepts is base-1, and the LATER of the two
//     satisfies both — for a fresh secret, and for the first call of a later
//     -test.count pass (whose claim is a whole pass, i.e. many steps, behind the
//     clock), that is base-1.
//   - A code is valid only inside {base-1, base, base+1}: a claim ahead of that
//     window cannot be generated yet, so the caller waits (waitSteps) — which is
//     what a real authenticator does, and the reason a 4th code for one secret
//     is now a wait rather than the panic this fixture used to raise (card
//     t_7655c2bf).
//
// Pure, so every case the fixture meets is pinned by a table instead of by
// sleeping through it: TestTOTPClaimCoversEveryFixtureCase.
func totpClaim(last, base int64) (step, waitSteps int64) {
	step = last + 1
	if step < base-1 {
		step = base - 1
	}
	if step > base+1 {
		waitSteps = step - 1 - base
	}
	return step, waitSteps
}

// totpCodeAtStep generates the code for `secret` at an absolute time-step.
func totpCodeAtStep(secret string, step int64) string {
	code, err := totp.GenerateCodeCustom(secret, time.Unix(step*totpTestPeriod, 0), totp.ValidateOpts{
		Period:    totpTestPeriod,
		Skew:      0,
		Digits:    6,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		panic("totp: " + err.Error())
	}
	return code
}

// distinctStepCode returns a code for `secret` at `steps` periods from now.
// Offsets of -1, 0 and +1 are all inside the validator's drift window. Use when
// a specific window position matters; otherwise prefer mustTOTP.
func distinctStepCode(t *testing.T, secret string, steps int) string {
	t.Helper()
	base := time.Now().Unix() / totpTestPeriod
	return totpCodeAtStep(secret, base+int64(steps))
}
