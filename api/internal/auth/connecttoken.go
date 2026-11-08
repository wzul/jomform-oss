// Social "connect" email-ownership tokens (issue #302).
//
// First-time social login: when a Google (etc.) email matches an existing
// JomForm account but the identity has no prior link, we must NOT silently
// auto-link — that would let an attacker's OAuth attach to a victim's account
// just because the emails match. Instead the user is sent through an explicit
// "connect" step where they must prove ownership of the email address.
//
// This token is the proof: a URL-safe base64 blob of
// "<provider>|<uid>|<email>|<expiryUnix>|<hmac>". The HMAC is computed over
// provider+uid+email+expiry with the same AES-GCM signing key (ENCRYPTION_KEY
// bytes) used by the email verification tokens. Stateless — no Valkey state —
// and expires by timestamp.
package auth

import (
	"crypto/hmac"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ConnectTokenTTL is how long a connect link stays valid.
const ConnectTokenTTL = 24 * time.Hour

var (
	// ErrConnectTokenExpired is returned when the link has passed its TTL.
	ErrConnectTokenExpired = errors.New("the connect link has expired")
	// ErrConnectTokenInvalid is returned for a malformed or forged link.
	ErrConnectTokenInvalid = errors.New("the connect link is invalid")
)

// SignConnectToken mints a signed, expiring email-ownership token for a social
// connect. key must be 32 bytes (same bytes as the AES-GCM at-rest key).
func SignConnectToken(key []byte, provider, uid, email string) (string, error) {
	if len(key) != 32 {
		return "", errors.New("connect token key must be 32 bytes")
	}
	expiry := timeNow().Add(ConnectTokenTTL).Unix()
	payload := provider + "|" + uid + "|" + email + "|" + fmt.Sprintf("%d", expiry)
	mac := hmacSHA256(key, payload)
	raw := payload + "|" + mac
	return base64.RawURLEncoding.EncodeToString([]byte(raw)), nil
}

// VerifyConnectToken validates a connect token and returns the provider, the
// provider UID and the verified email, or an error if the token is malformed,
// its HMAC does not match, or it has expired.
func VerifyConnectToken(key []byte, token string) (provider, uid, email string, err error) {
	if len(key) != 32 {
		return "", "", "", errors.New("connect token key must be 32 bytes")
	}
	raw, derr := base64.RawURLEncoding.DecodeString(token)
	if derr != nil {
		return "", "", "", ErrConnectTokenInvalid
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 5 {
		return "", "", "", ErrConnectTokenInvalid
	}
	provider, uid, email, expiryStr, mac := parts[0], parts[1], parts[2], parts[3], parts[4]
	if provider == "" || uid == "" || email == "" {
		return "", "", "", ErrConnectTokenInvalid
	}
	payload := provider + "|" + uid + "|" + email + "|" + expiryStr
	if !hmac.Equal([]byte(mac), []byte(hmacSHA256(key, payload))) {
		return "", "", "", ErrConnectTokenInvalid
	}
	expiry, perr := parseUnix(expiryStr)
	if perr != nil {
		return "", "", "", ErrConnectTokenInvalid
	}
	if timeNow().After(time.Unix(expiry, 0)) {
		return "", "", "", ErrConnectTokenExpired
	}
	return provider, uid, email, nil
}
