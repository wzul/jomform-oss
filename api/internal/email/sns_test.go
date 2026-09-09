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

func makeTestCert(t *testing.T) ([]byte, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	certDER, err := x509.CreateCertificate(rand.Reader,
		&x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Amazon SNS"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)},
		&x509.Certificate{}, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	return certPEM, key
}

// makeTestCertWithCN is like makeTestCert but with a caller-chosen subject
// common name, so tests can exercise subject pinning.
func makeTestCertWithCN(t *testing.T, cn string) ([]byte, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	certDER, err := x509.CreateCertificate(rand.Reader,
		&x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: cn}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)},
		&x509.Certificate{}, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	return certPEM, key
}

// A Notification WITHOUT a Subject (the SES Bounce/Complaint case): the
// canonical string must NOT include an empty Subject line, or verification
// fails (issue #76).
func TestVerifySNSNotificationNoSubject(t *testing.T) {
	certPEM, key := makeTestCert(t)
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
		t.Fatalf("no-subject notification rejected: %v", err)
	}
}

// A Notification WITH a Subject: the Subject line must be part of the
// canonical string (issue #76 — only include it when present).
func TestVerifySNSNotificationWithSubject(t *testing.T) {
	certPEM, key := makeTestCert(t)
	msg := SNSNotification{
		Type: "Notification", MessageID: "m-subj", Subject: "hello",
		Timestamp: time.Now().UTC().Format(time.RFC3339), SignatureVersion: "1",
		SigningCertURL: "https://sns.us-east-1.amazonaws.com/SimpleNotificationService-subj.pem",
		TopicArn:       "arn:aws:sns:us-east-1:1:t", Message: "x",
	}
	raw, _ := makeSNSSigner(t, key, certPEM, msg)
	if err := VerifySNSNotification(raw, fakeCertFetcher(certPEM)); err != nil {
		t.Fatalf("subject-bearing notification rejected: %v", err)
	}
}

func TestVerifySNSNotificationRejectsBadSignature(t *testing.T) {
	certPEM, key := makeTestCert(t)
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

// The SigningCertURL is allowlisted but points at a different region than the
// (signed) TopicArn — attacker switched regions to point at another AWS SNS
// endpoint. Verification must reject it; the cert source is pinned to the
// message's own region.
func TestVerifySNSNotificationRejectsRegionMismatch(t *testing.T) {
	certPEM, key := makeTestCert(t)
	msg := SNSNotification{
		Type: "Notification", MessageID: "m-reg", Timestamp: "2026-01-01T00:00:00Z",
		SignatureVersion: "1",
		SigningCertURL:   "https://sns.eu-west-1.amazonaws.com/SimpleNotificationService-x.pem", // wrong region
		TopicArn:         "arn:aws:sns:us-east-1:1:t",
		Message:          "x",
	}
	raw, _ := makeSNSSigner(t, key, certPEM, msg)
	if err := VerifySNSNotification(raw, fakeCertFetcher(certPEM)); err == nil {
		t.Fatal("region-mismatched cert URL accepted — must reject")
	}
}

// The fetched certificate is a valid RSA cert but its subject is not an AWS SNS
// signer — even though the URL is allowlisted, the cert must be rejected.
func TestVerifySNSNotificationRejectsUntrustedCertSubject(t *testing.T) {
	certPEM, key := makeTestCertWithCN(t, "evil.corp.local")
	msg := SNSNotification{
		Type: "Notification", MessageID: "m-subj-reject", Timestamp: "2026-01-01T00:00:00Z",
		SignatureVersion: "1",
		SigningCertURL:   "https://sns.us-east-1.amazonaws.com/SimpleNotificationService-evil.pem",
		TopicArn:         "arn:aws:sns:us-east-1:1:t",
		Message:          "x",
	}
	raw, _ := makeSNSSigner(t, key, certPEM, msg)
	if err := VerifySNSNotification(raw, fakeCertFetcher(certPEM)); err == nil {
		t.Fatal("non-AWS subject cert accepted — subject pinning must reject it")
	}
}

func TestVerifySNSNotificationRejectsMissingSignature(t *testing.T) {
	msg := SNSNotification{Type: "Notification", MessageID: "m4", Timestamp: "2026-01-01T00:00:00Z"}
	raw, _ := json.Marshal(msg)
	if err := VerifySNSNotification(raw, nil); err == nil {
		t.Fatal("missing signature accepted — SNS verification must fail")
	}
}
