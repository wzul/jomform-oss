package auth

import (
	"testing"
	"time"
)

// TestConnectTokenRoundTrip verifies a signed connect token validates and
// returns the provider, uid and email it was minted for (issue #302).
func TestConnectTokenRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	tok, err := SignConnectToken(key, "google", "gid-123", "alice@example.com")
	if err != nil {
		t.Fatalf("SignConnectToken: %v", err)
	}
	provider, uid, email, err := VerifyConnectToken(key, tok)
	if err != nil {
		t.Fatalf("VerifyConnectToken: %v", err)
	}
	if provider != "google" || uid != "gid-123" || email != "alice@example.com" {
		t.Fatalf("roundtrip mismatch: provider=%q uid=%q email=%q", provider, uid, email)
	}
}

// TestConnectTokenWrongKey ensures a token signed with a different key is
// rejected — an attacker cannot forge a connect link without the signing key.
func TestConnectTokenWrongKey(t *testing.T) {
	key := make([]byte, 32)
	key[0] = 1
	tok, _ := SignConnectToken(key, "google", "uid", "a@b.com")
	bad := make([]byte, 32)
	bad[0] = 2
	if _, _, _, err := VerifyConnectToken(bad, tok); err == nil {
		t.Fatal("VerifyConnectToken accepted a token signed with a different key")
	}
}

// TestConnectTokenShortKey rejects a 16-byte key (mirrors verifytoken).
func TestConnectTokenShortKey(t *testing.T) {
	if _, err := SignConnectToken(make([]byte, 16), "google", "uid", "a@b.com"); err == nil {
		t.Fatal("SignConnectToken accepted a 16-byte key")
	}
}

// TestConnectTokenExpired confirms an expired token is rejected.
func TestConnectTokenExpired(t *testing.T) {
	key := make([]byte, 32)
	tok, err := SignConnectToken(key, "google", "uid", "a@b.com")
	if err != nil {
		t.Fatalf("SignConnectToken: %v", err)
	}
	// Rewinding the clock makes the just-minted token read as expired.
	timeNow = func() time.Time { return time.Now().Add(ConnectTokenTTL + time.Hour) }
	defer func() { timeNow = time.Now }()
	if _, _, _, err := VerifyConnectToken(key, tok); err == nil {
		t.Fatal("VerifyConnectToken accepted an expired token")
	}
}
