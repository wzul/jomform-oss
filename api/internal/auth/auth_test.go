package auth

import (
	"strings"
	"testing"
	"time"
)

func TestTOTPGenerateAndValidate(t *testing.T) {
	secret, url, err := GenerateTOTPSecret("wan@jomform.com")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if secret == "" {
		t.Fatal("empty secret")
	}
	if !strings.Contains(url, "otpauth://totp/") || !strings.Contains(url, "JomForm") {
		t.Errorf("unexpected otpauth url: %s", url)
	}

	// Generate a code with the same algorithm at current time.
	code, err := currentCode(secret)
	if err != nil {
		t.Fatalf("code gen: %v", err)
	}
	if !ValidateTOTPSecret(secret, code) {
		t.Fatalf("valid code rejected: %s", code)
	}
	if ValidateTOTPSecret(secret, "000000") == true && code != "000000" {
		// not a hard failure ( astronomically unlikely ), but flag
		t.Log("000000 unexpectedly valid")
	}
	if ValidateTOTPSecret(secret, "1234567") {
		t.Fatal("7-digit code accepted")
	}
	if ValidateTOTPSecret("", code) {
		t.Fatal("empty secret accepted")
	}
}

func TestTOTPStaleCodeRejected(t *testing.T) {
	secret, _, _ := GenerateTOTPSecret("wan@jomform.com")
	// Freeze validation 10 minutes in the past so the current code won't match.
	old := time.Now().Add(-10 * time.Minute)
	timeNow = func() time.Time { return old }
	defer func() { timeNow = time.Now }()

	code, _ := currentCodeAt(secret, time.Now())
	if ValidateTOTPSecret(secret, code) {
		t.Fatal("code from current time accepted while clock is 10min behind")
	}
}

func TestGenerateAPIKey(t *testing.T) {
	raw, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.HasPrefix(raw, "jfk_") {
		t.Errorf("raw key missing prefix: %s", raw)
	}
	if len(hash) != 64 {
		t.Errorf("hash length = %d, want 64 (sha256 hex)", len(hash))
	}
	if HashAPIKey(raw) != hash {
		t.Error("hash mismatch on re-hash")
	}
	raw2, hash2, _ := GenerateAPIKey()
	if raw == raw2 || hash == hash2 {
		t.Fatal("API keys not unique")
	}
}
