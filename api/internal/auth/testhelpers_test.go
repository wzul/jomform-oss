package auth

import (
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// currentCode generates a TOTP code for secret at timeNow().
func currentCode(secret string) (string, error) {
	return currentCodeAt(secret, timeNow())
}

// currentCodeAt generates a code at an explicit time.
func currentCodeAt(secret string, at time.Time) (string, error) {
	return totp.GenerateCodeCustom(secret, at, totp.ValidateOpts{
		Period:    totpPeriod,
		Skew:      0,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
}

var _ = testing.Short
