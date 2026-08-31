package cryptoutil

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	plaintext := []byte(`{"brand_id":"x","secret_key":"super-secret"}`)

	sealed, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	out, err := Decrypt(key, sealed)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(out, plaintext) {
		t.Fatalf("round trip mismatch: %q", out)
	}
}

func TestDecryptWrongKey(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	rand.Read(key1)
	rand.Read(key2)

	sealed, _ := Encrypt(key1, []byte("secret"))
	if _, err := Decrypt(key2, sealed); err == nil {
		t.Fatal("expected error decrypting with wrong key")
	}
}

func TestKeySizeEnforced(t *testing.T) {
	if _, err := Encrypt([]byte("short"), []byte("x")); err == nil {
		t.Fatal("expected error for non-32-byte key")
	}
	if _, err := Decrypt([]byte{}, hex.EncodeToString([]byte("ab"))); err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestNonceUnique(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	a, _ := Encrypt(key, []byte("same"))
	b, _ := Encrypt(key, []byte("same"))
	if a == b {
		t.Fatal("nonce reuse: identical ciphertexts for identical plaintext")
	}
}