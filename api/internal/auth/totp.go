// TOTP two-factor via pquerna/otp.
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

const totpPeriod = 30 // seconds; standard

// GenerateTOTPSecret creates a new base32 TOTP secret for the account label.
func GenerateTOTPSecret(accountEmail string) (secret string, url string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "JomForm",
		AccountName: accountEmail,
		Period:      totpPeriod,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return "", "", fmt.Errorf("generate totp: %w", err)
	}
	return key.Secret(), key.URL(), nil
}

// ValidateTOTPSecret checks a 6-digit code against the stored secret,
// tolerating one period of clock drift.
func ValidateTOTPSecret(secret, code string) bool {
	_, ok := ValidateTOTPSecretStep(secret, code)
	return ok
}

// ValidateTOTPSecretStep validates a code AND returns the time-step it matched,
// so the caller can enforce single-use (issue #528).
//
// ValidateTOTPSecret alone is stateless: totp.ValidateCustom accepts any code
// within the current step ± Skew, so the SAME code verifies repeatedly for its
// whole ~90s window. Callers that gate access on a code must use this function
// and then consume the returned step, otherwise a leaked code (mid-flow
// screenshot, over-shoulder, a proxy log) can be replayed.
//
// ok is false when the secret/code is empty or the code does not match any
// tolerated step; step is only meaningful when ok is true.
func ValidateTOTPSecretStep(secret, code string) (step int64, ok bool) {
	if secret == "" || code == "" {
		return 0, false
	}
	now := timeNow()
	// Match across the same tolerance the library uses (Skew: 1 → the current
	// 30s window plus one either side), and report WHICH step matched so the
	// caller can record it. Iterating explicitly (rather than calling
	// ValidateCustom once) is what makes the matched step available; the
	// per-step comparison is identical to the library's.
	base := now.Unix() / totpPeriod
	for _, s := range []int64{base - 1, base, base + 1} {
		ok, err := totp.ValidateCustom(code, secret, time.Unix(s*totpPeriod, 0), totp.ValidateOpts{
			Period:    totpPeriod,
			Skew:      0, // the window is applied by the loop above
			Digits:    otp.DigitsSix,
			Algorithm: otp.AlgorithmSHA1,
		})
		if err == nil && ok {
			return s, true
		}
	}
	return 0, false
}

var errNoSecret = errors.New("totp secret not set")

var _ = errNoSecret
