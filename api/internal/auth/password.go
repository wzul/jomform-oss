// Package auth implements password hashing (argon2id), TOTP two-factor, and
// API key generation for JomForm.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters — OWASP-recommended baseline for interactive login.
const (
	ArgonTime    = 1
	ArgonMemory  = 64 * 1024 // KiB
	ArgonThreads = 2
	ArgonKeyLen  = 32
	ArgonSaltLen = 16
)

var errInvalidHash = errors.New("invalid argon2 hash encoding")

// HashPassword derives an argon2id PHC-encoded string for password.
func HashPassword(password string) (string, error) {
	salt := make([]byte, ArgonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("read salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, ArgonTime, ArgonMemory, ArgonThreads, ArgonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, ArgonMemory, ArgonTime, ArgonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword reports whether password matches an argon2id PHC string.
// Encoded form: $argon2id$v=19$m=65536,t=1,p=2$<salt>$<key> (5 parts after
// trimming the leading '$'). Comparison is constant-time.
func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(strings.TrimPrefix(encoded, "$"), "$")
	if len(parts) != 5 || parts[0] != "argon2id" {
		return false, errInvalidHash
	}
	var (
		version int
		m       uint32
		t       uint32
		p       uint8
	)
	if _, err := fmt.Sscanf(parts[1], "v=%d", &version); err != nil {
		return false, errInvalidHash
	}
	if _, err := fmt.Sscanf(parts[2], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, errInvalidHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false, errInvalidHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, errInvalidHash
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// ValidEmail performs a pragmatic RFC 5322 address parse.
func ValidEmail(email string) bool {
	addr, err := mail.ParseAddress(email)
	return err == nil && addr.Address == email && strings.Contains(email, "@")
}

// DummyPasswordHash is a fixed, valid argon2id hash with the same cost
// parameters as real user hashes. Login verifies an unknown email against it
// so the CPU cost — and therefore response timing — is indistinguishable from
// a real (wrong-password) check, defeating user-enumeration via timing
// (issue #42). It is constant time to derive, so it is safe to share.
var DummyPasswordHash string

func init() {
	// Hash a throwaway string once at package init with the same parameters
	// used for real registration hashes.
	if h, err := HashPassword("not-a-real-password"); err == nil {
		DummyPasswordHash = h
	} else {
		// Argon2 hashing is deterministic and self-contained — failure here is
		// effectively impossible; fall back to an empty string and let
		// VerifyPassword return an error (treated as failed login) rather than
		// crash. Login still works for real users.
		DummyPasswordHash = ""
	}
}
