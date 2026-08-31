package auth

import "testing"

func TestHashVerifyRoundTrip(t *testing.T) {
	hash, err := HashPassword("kata-laluan-sangat-selamat-123")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	ok, err := VerifyPassword("kata-laluan-sangat-selamat-123", hash)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !ok {
		t.Fatal("correct password rejected")
	}
}

func TestVerifyWrongPassword(t *testing.T) {
	hash, _ := HashPassword("betul-betul-betul")
	ok, err := VerifyPassword("salah-salah-salah", hash)
	if err != nil {
		t.Fatalf("verify should not error on mismatch: %v", err)
	}
	if ok {
		t.Fatal("wrong password accepted")
	}
}

func TestHashUniqueSalts(t *testing.T) {
	a, _ := HashPassword("same-password")
	b, _ := HashPassword("same-password")
	if a == b {
		t.Fatal("identical hashes — salt not random")
	}
}

func TestVerifyMalformedHash(t *testing.T) {
	if _, err := VerifyPassword("x", "not-a-hash"); err == nil {
		t.Fatal("expected error for malformed hash")
	}
	if _, err := VerifyPassword("x", "$bcrypt$v=1$aaa$bbb"); err == nil {
		t.Fatal("expected error for non-argon2id hash")
	}
}

func TestValidEmail(t *testing.T) {
	cases := map[string]bool{
		"wan@jomform.com":        true,
		"user+tag@sub.domain.my": true,
		"nope":                   false,
		"no-at-sign":             false,
		"@missing-local.com":     false,
	}
	for email, want := range cases {
		if got := ValidEmail(email); got != want {
			t.Errorf("ValidEmail(%q) = %v, want %v", email, got, want)
		}
	}
}
