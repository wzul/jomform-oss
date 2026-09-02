package email

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

// signSNSNotification builds a signed SNS Notification envelope + its signer
// certificate, returning (rawBody, sign, canonicalToSign). The certificate is
// NOT PEM-wrapped in the returned body's SigningCertURL — the fetcher is what
// supplies the PEM bytes, so the test injects them directly.
func makeSNSSigner(t *testing.T, key *rsa.PrivateKey, certPEM []byte, msg SNSNotification) ([]byte, []byte) {
	t.Helper()
	// canonical string to sign (must match VerifySNSNotification)
	canon := snsCanonicalString(msg)
	digest := sha1.Sum([]byte(canon))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA1, digest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	msg.Signature = base64.StdEncoding.EncodeToString(sig)
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw, sig
}

// fakeCertFetcher returns canned certPEM for any SigningCertURL (still must
// match the SNSCertURLRe allowlist).
func fakeCertFetcher(pemBytes []byte) SNSCertFetcher {
	return func(url string) ([]byte, error) { return pemBytes, nil }
}

func TestVerifySNSNotificationValidSignature(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	certDER, err := x509.CreateCertificate(rand.Reader,
		&x509.Certificate{
			SerialNumber: big.NewInt(1),
			Subject:      pkix.Name{CommonName: "sns"},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(time.Hour),
		},
		&x509.Certificate{},
		&key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	msg := SNSNotification{
		Type:             "Notification",
		MessageID:        "msg-1",
		Timestamp:        time.Now().UTC().Format(time.RFC3339),
		SignatureVersion: "1",
		SigningCertURL:   "https://sns.us-east-1.amazonaws.com/SimpleNotificationService-abc.pem",
		TopicArn:         "arn:aws:sns:us-east-1:123:ses-notif",
		Message:          `{"eventType":"Bounce","bounce":{"bounceType":"Permanent","bouncedRecipients":[{"emailAddress":"x@example.com"}]},"mail":{"destination":["x@example.com"]}}`,
	}
	raw, _ := makeSNSSigner(t, key, certPEM, msg)

	if err := VerifySNSNotification(raw, fakeCertFetcher(certPEM)); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
}

func TestVerifySNSNotificationRejectsBadSignature(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	certDER, err := x509.CreateCertificate(rand.Reader,
		&x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "sns"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)},
		&x509.Certificate{}, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	msg := SNSNotification{
		Type: "Notification", MessageID: "m2", Timestamp: "2026-01-01T00:00:00Z",
		SignatureVersion: "1", SigningCertURL: "https://sns.us-east-1.amazonaws.com/SimpleNotificationService-x.pem",
		TopicArn: "arn:aws:sns:us-east-1:1:t", Message: "spoofed",
	}
	raw, _ := makeSNSSigner(t, key, certPEM, msg)
	// tamper: alter the message so the signature no longer matches
	var env SNSNotification
	_ = json.Unmarshal(raw, &env)
	env.Message = "tampered"
	tampered, _ := json.Marshal(env)

	if err := VerifySNSNotification(tampered, fakeCertFetcher(certPEM)); err == nil {
		t.Fatal("tampered message accepted — signature must fail")
	}
}

func TestVerifySNSNotificationRejectsUntrustedCertURL(t *testing.T) {
	msg := SNSNotification{
		Type: "Notification", MessageID: "m3", Timestamp: "2026-01-01T00:00:00Z",
		SignatureVersion: "1", SigningCertURL: "http://evil.example.com/cert.pem", // not AWS allowlisted
		TopicArn: "arn:aws:sns:us-east-1:1:t", Message: "x",
		Signature: "AAAA",
	}
	raw, _ := json.Marshal(msg)
	if err := VerifySNSNotification(raw, fakeCertFetcher([]byte("PEM"))); err == nil {
		t.Fatal("untrusted cert URL accepted — SSRF guard must reject it")
	}
}

func TestVerifySNSNotificationRejectsMissingSignature(t *testing.T) {
	msg := SNSNotification{Type: "Notification", MessageID: "m4", Timestamp: "2026-01-01T00:00:00Z"}
	raw, _ := json.Marshal(msg)
	if err := VerifySNSNotification(raw, nil); err == nil {
		t.Fatal("missing signature accepted — SNS verification must fail")
	}
}
