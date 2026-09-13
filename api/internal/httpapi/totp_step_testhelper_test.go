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

// totpStepAlloc tracks, per secret, how many codes have already been handed out,
// so each call returns a code from a strictly later time-step.
var totpStepAlloc = struct {
	sync.Mutex
	used map[string]int64
}{used: map[string]int64{}}

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
