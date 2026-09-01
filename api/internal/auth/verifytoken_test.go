package auth

import (
	"strings"
	"testing"
	"time"
)

func TestVerifyTokenRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	email := "wan@jomform.com"

	tok, err := SignVerifyToken(key, email)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if tok == "" {
		t.Fatal("empty token")
	}

	got, err := VerifyToken(key, tok)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !strings.EqualFold(got, email) {
		t.Errorf("verified email = %q, want %q", got, email)
	}
}

func TestVerifyTokenBadKeyLength(t *testing.T) {
	if _, err := SignVerifyToken(make([]byte, 16), "a@b.com"); err == nil {
		t.Error("SignVerifyToken accepted a 16-byte key")
	}
	if _, err := VerifyToken(make([]byte, 16), "x"); err == nil {
		t.Error("VerifyToken accepted a 16-byte key")
	}
}

func TestVerifyTokenTamperRejected(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(0x40 + i)
	}
	tok, _ := SignVerifyToken(key, "a@b.com")

	// Flip one character in the payload portion (before the final '|').
	for i := 2; i < len(tok)-1; i++ {
		c := tok[i]
		if c >= 'A' && c <= 'Z' {
			c -= 'A'
			if c == 'A' {
				c = 'B'
			}
			break
		}
	}
	if _, err := VerifyToken(key, tok[:len(tok)-5]+"AAAAA"); err == nil {
		t.Error("tampered token accepted")
	}
}

func TestVerifyTokenWrongKeyRejected(t *testing.T) {
	keyA := make([]byte, 32)
	keyB := make([]byte, 32)
	for i := range keyB {
		keyB[i] = 0xFF
	}
	tok, _ := SignVerifyToken(keyA, "a@b.com")
	if _, err := VerifyToken(keyB, tok); err == nil {
		t.Error("token signed with another key accepted")
	}
}

func TestVerifyTokenExpired(t *testing.T) {
	key := make([]byte, 32)
	tok, _ := SignVerifyToken(key, "a@b.com")

	// Freeze the clock 25h into the future so the 24h token is expired.
	old := timeNow
	timeNow = func() time.Time { return old().Add(25 * time.Hour) }
	defer func() { timeNow = old }()

	if _, err := VerifyToken(key, tok); err == nil {
		t.Error("expired token accepted")
	}
}

func TestVerifyTokenMalformed(t *testing.T) {
	key := make([]byte, 32)
	for _, tok := range []string{"", "not-base64-!!", "ab", "a|b"} {
		if _, err := VerifyToken(key, tok); err == nil {
			t.Errorf("malformed token %q accepted", tok)
		}
	}
}
