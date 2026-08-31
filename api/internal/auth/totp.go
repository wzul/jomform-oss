// TOTP two-factor via pquerna/otp.
package auth

import (
	"errors"
	"fmt"

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

// ValidateTOTP checks a 6-digit code against the stored secret,
// tolerating one period of clock drift.
func ValidateTOTPSecret(secret, code string) bool {
	if secret == "" || code == "" {
		return false
	}
	ok, err := totp.ValidateCustom(code, secret, timeNow(), totp.ValidateOpts{
		Period:    totpPeriod,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	return err == nil && ok
}

var errNoSecret = errors.New("totp secret not set")

var _ = errNoSecret
