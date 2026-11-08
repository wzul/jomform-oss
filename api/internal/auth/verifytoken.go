package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Email verification tokens (issue #42).
//
// A token is a URL-safe base64 blob of "<email>|<expiryUnix>|<hmac>". The HMAC
// is computed over email+expiry with the AES-GCM signing key (the same
// ENCRYPTION_KEY bytes used for at-rest secrets, sourced by the caller via
// cryptoutil). No state is stored in Valkey: verification is stateless and the
// link expires by timestamp.
const VerifyTokenTTL = 24 * time.Hour

var (
	ErrVerifyTokenExpired = errors.New("the link has expired")
	ErrVerifyTokenInvalid = errors.New("the link is invalid")
)

// SignVerifyToken mints a signed, expiring verification token for an email.
// key must be 32 bytes (the same bytes used for AES-GCM at rest).
func SignVerifyToken(key []byte, email string) (string, error) {
	if len(key) != 32 {
		return "", errors.New("verify key must be 32 bytes")
	}
	expiry := timeNow().Add(VerifyTokenTTL).Unix()
	payload := email + "|" + fmt.Sprintf("%d", expiry)
	mac := hmacSHA256(key, payload)
	raw := payload + "|" + mac
	return base64.RawURLEncoding.EncodeToString([]byte(raw)), nil
}

// VerifyToken validates a token and returns the verified email, or an error if
// the token is malformed, its HMAC does not match, or it has expired.
func VerifyToken(key []byte, token string) (string, error) {
	if len(key) != 32 {
		return "", errors.New("verify key must be 32 bytes")
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", ErrVerifyTokenInvalid
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 {
		return "", ErrVerifyTokenInvalid
	}
	email, expiryStr, mac := parts[0], parts[1], parts[2]
	if email == "" {
		return "", ErrVerifyTokenInvalid
	}
	payload := email + "|" + expiryStr
	if !hmac.Equal([]byte(mac), []byte(hmacSHA256(key, payload))) {
		return "", ErrVerifyTokenInvalid
	}
	expiry, err := parseUnix(expiryStr)
	if err != nil {
		return "", ErrVerifyTokenInvalid
	}
	if timeNow().After(time.Unix(expiry, 0)) {
		return "", ErrVerifyTokenExpired
	}
	return email, nil
}

func hmacSHA256(key []byte, data string) string {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

func parseUnix(s string) (int64, error) {
	var n int64
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, err
	}
	return n, nil
}
