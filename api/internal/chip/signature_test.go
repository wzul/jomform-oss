package chip

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"testing"
)

func testKeypair(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	return key, pemStr
}

func signBody(t *testing.T, priv *rsa.PrivateKey, body []byte) string {
	t.Helper()
	digest := sha256.Sum256(body)
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(sig)
}

func TestVerifySignatureValid(t *testing.T) {
	priv, pemStr := testKeypair(t)
	pub, err := ParsePublicKey(pemStr)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	body := []byte(`{"id":"pur_123","event_type":"purchase.paid","status":"paid"}`)
	sig := signBody(t, priv, body)

	if err := VerifySignature(pub, body, sig); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
}

func TestVerifySignatureTampered(t *testing.T) {
	priv, pemStr := testKeypair(t)
	pub, _ := ParsePublicKey(pemStr)
	body := []byte(`{"id":"pur_123","event_type":"purchase.paid"}`)
	sig := signBody(t, priv, body)

	tampered := []byte(`{"id":"pur_999","event_type":"purchase.paid"}`)
	if err := VerifySignature(pub, tampered, sig); err == nil {
		t.Fatal("tampered body accepted")
	}
}

func TestVerifySignatureGarbage(t *testing.T) {
	_, pemStr := testKeypair(t)
	pub, _ := ParsePublicKey(pemStr)
	if err := VerifySignature(pub, []byte("body"), "not-base64!!!"); err == nil {
		t.Fatal("garbage signature accepted")
	}
	if err := VerifySignature(pub, []byte("body"), base64.StdEncoding.EncodeToString([]byte("small"))); err == nil {
		t.Fatal("wrong-length signature accepted")
	}
}

func TestParsePublicKeyInvalidPEM(t *testing.T) {
	if _, err := ParsePublicKey("no pem here"); err == nil {
		t.Fatal("expected error for non-PEM input")
	}
}
