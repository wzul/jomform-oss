package chip

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
)

// parsePKIX parses SubjectPublicKeyInfo (what CHIP returns in PEM).
func parsePKIX(der []byte) (*rsa.PublicKey, error) {
	key, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		// Try PKCS#1 as a fallback.
		if k, err2 := x509.ParsePKCS1PublicKey(der); err2 == nil {
			return k, nil
		}
		return nil, err
	}
	rsaKey, ok := key.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("chip: public key is not RSA")
	}
	return rsaKey, nil
}

// VerifySignature validates CHIP's callback/webhook signature:
// X-Signature is base64(RSA PKCS#1 v1.5 (RSASSA-PKCS1-v1_5)) over the
// SHA256 digest of the raw request body.
func VerifySignature(publicKey *rsa.PublicKey, body []byte, signatureB64 string) error {
	sig, err := base64.StdEncoding.DecodeString(signatureB64)
	if err != nil {
		return fmt.Errorf("chip: decode signature: %w", err)
	}
	digest := sha256.Sum256(body)
	if err := rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], sig); err != nil {
		return fmt.Errorf("chip: signature verification failed: %w", err)
	}
	return nil
}
