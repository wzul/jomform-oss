// SNS message signature verification (issue #56). SES publishes Bounce and
// Complaint notifications to an SNS topic, which POSTs an HTTPS notification to
// our /v1/internal/ses/notifications endpoint. Every SNS message is signed and
// carries the signing certificate URL; this verifies the signature so
// unauthenticated, spoofed notifications cannot write to the suppression list.
package email

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

// SNSNotification is the envelope every SNS HTTPS message carries.
type SNSNotification struct {
	Type               string `json:"Type"` // Notification | SubscriptionConfirmation
	Message            string `json:"Message"`
	MessageID          string `json:"MessageId"`
	Timestamp          string `json:"Timestamp"`
	SignatureVersion   string `json:"SignatureVersion"`
	Signature          string `json:"Signature"`
	SigningCertURL     string `json:"SigningCertURL"`
	SubscribeURL       string `json:"SubscribeURL"`
	Token              string `json:"Token"`
	TopicArn           string `json:"TopicArn"`
	UnsubscribeURL     string `json:"UnsubscribeURL"`
}

// SNSCertFetcher fetches the PEM-encoded signing certificate at a URL. It is a
// function so tests can inject a fake certificate without network access.
type SNSCertFetcher func(url string) ([]byte, error)

// SNSCertURLRe matches AWS SNS signing-certificate URLs. Amazon signs messages
// only with certificates hosted on AWS S3; restricting the URL prevents SSRF
// via a malicious SigningCertURL pointing at an internal host.
var SNSCertURLRe = regexp.MustCompile(`^https://sns\.[a-z0-9-]+\.amazonaws\.com/.*\.pem$`)

// VerifySNSNotification verifies the SNS signature on the raw notification body
// using the certificate fetched from SigningCertURL. certFetch defaults to an
// HTTP GET when nil. It returns an error when the signature is absent, the cert
// URL is not a valid AWS SNS URL, the cert cannot be fetched/parsed, or the
// signature does not validate.
func VerifySNSNotification(raw []byte, certFetch SNSCertFetcher) error {
	if certFetch == nil {
		certFetch = func(url string) ([]byte, error) {
			resp, err := http.Get(url)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("cert fetch status %d", resp.StatusCode)
			}
			buf := make([]byte, 0, 4096)
			tmp := make([]byte, 4096)
			total := 0
			for {
				n, rerr := resp.Body.Read(tmp)
				total += n
				buf = append(buf, tmp[:n]...)
				if rerr != nil {
					break
				}
				if total > 1<<16 {
					return nil, fmt.Errorf("cert too large")
				}
			}
			return buf, nil
		}
	}

	var msg SNSNotification
	if err := json.Unmarshal(raw, &msg); err != nil {
		return fmt.Errorf("sns parse: %w", err)
	}
	if msg.Signature == "" || msg.SigningCertURL == "" {
		return fmt.Errorf("sns message missing signature fields")
	}
	if msg.SignatureVersion != "1" {
		return fmt.Errorf("unsupported sns signature version %q", msg.SignatureVersion)
	}
	// Restrict cert URLs to AWS-hosted SNS certs (SSRF guard).
	if !SNSCertURLRe.MatchString(msg.SigningCertURL) {
		return fmt.Errorf("sns signing cert url not allowed: %q", msg.SigningCertURL)
	}
	certPEM, err := certFetch(msg.SigningCertURL)
	if err != nil {
		return fmt.Errorf("sns cert fetch: %w", err)
	}
	pub, err := parseSNSPublicKey(certPEM)
	if err != nil {
		return fmt.Errorf("sns cert parse: %w", err)
	}

	toSign := snsCanonicalString(msg)
	sigBytes, err := base64.StdEncoding.DecodeString(msg.Signature)
	if err != nil {
		return fmt.Errorf("sns signature decode: %w", err)
	}
	if err := verifyRSA(pub, []byte(toSign), sigBytes); err != nil {
		return fmt.Errorf("sns signature verify: %w", err)
	}
	return nil
}

// parseSNSPublicKey extracts the RSA public key from an SNS signing cert PEM.
func parseSNSPublicKey(pemBytes []byte) (any, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("no pem block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}
	return cert.PublicKey, nil
}

// snsCanonicalString builds the SNS signature string. For a Notification it is
// the sorted MessageType + fields the message itself is signed over, joined
// with newlines, exactly as AWS documents.
func snsCanonicalString(msg SNSNotification) string {
	fields := make(map[string]string)
	switch msg.Type {
	case "Notification":
		fields["Message"] = msg.Message
		fields["MessageId"] = msg.MessageID
		fields["Subject"] = "" // Subject is not part of a Notification signature
		fields["Timestamp"] = msg.Timestamp
		fields["TopicArn"] = msg.TopicArn
		fields["Type"] = msg.Type
	case "SubscriptionConfirmation":
		fields["Message"] = msg.Message
		fields["MessageId"] = msg.MessageID
		fields["SubscribeURL"] = msg.SubscribeURL
		fields["Timestamp"] = msg.Timestamp
		fields["Token"] = msg.Token
		fields["TopicArn"] = msg.TopicArn
		fields["Type"] = msg.Type
	default:
		return ""
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		v := fields[k]
		if k == "Subject" {
			v = ""
		}
		b.WriteString(k)
		b.WriteString("\n")
		b.WriteString(v)
		b.WriteString("\n")
	}
	return b.String()
}

// verifyRSA verifies an RSASSA-PKCS1-v1_5 SHA-1 signature (the only scheme
// AWS SNS uses for signature version 1). pub is an *rsa.PublicKey.
func verifyRSA(pub any, signed, sig []byte) error {
	key, ok := pub.(*rsa.PublicKey)
	if !ok {
		return fmt.Errorf("expected rsa public key, got %T", pub)
	}
	digest := sha1.Sum(signed) //nolint:gosec // SNS signature v1 uses SHA-1 by design
	return rsa.VerifyPKCS1v15(key, crypto.SHA1, digest[:], sig)
}
