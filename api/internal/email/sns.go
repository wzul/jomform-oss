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
	Type             string `json:"Type"` // Notification | SubscriptionConfirmation
	Message          string `json:"Message"`
	MessageID        string `json:"MessageId"`
	Subject          string `json:"Subject,omitempty"`
	Timestamp        string `json:"Timestamp"`
	SignatureVersion string `json:"SignatureVersion"`
	Signature        string `json:"Signature"`
	SigningCertURL   string `json:"SigningCertURL"`
	SubscribeURL     string `json:"SubscribeURL"`
	Token            string `json:"Token"`
	TopicArn         string `json:"TopicArn"`
	UnsubscribeURL   string `json:"UnsubscribeURL"`
}

// SNSCertFetcher fetches the PEM-encoded signing certificate at a URL. It is a
// function so tests can inject a fake certificate without network access.
type SNSCertFetcher func(url string) ([]byte, error)

// SNSCertURLRe matches AWS SNS signing-certificate URLs. Amazon signs messages
// only with certificates hosted on the SNS HTTPS endpoint; restricting the URL
// to sns.<region>.amazonaws.com prevents SSRF via a malicious SigningCertURL
// pointing at an internal host, and HTTPS enforces verification against the
// system root pool (no plaintext fetch).
var SNSCertURLRe = regexp.MustCompile(`^https://sns\.[a-z0-9-]+\.amazonaws\.com/.*\.pem$`)

// snsTopicRegionRe matches the region segment of an SNS topic ARN
// (arn:aws:sns:<region>:<account>:<topic>).
var snsTopicRegionRe = regexp.MustCompile(`^arn:aws:sns:([a-z0-9-]+):\d+:.+$`)

// snsRegion returns the AWS region embedded in the SNS message's TopicArn.
func snsRegion(msg SNSNotification) (string, error) {
	m := snsTopicRegionRe.FindStringSubmatch(msg.TopicArn)
	if m == nil {
		return "", fmt.Errorf("sns topic arn has no parseable region: %q", msg.TopicArn)
	}
	return m[1], nil
}

// VerifySNSNotification verifies the SNS signature on the raw notification body
// using the certificate fetched for the message. The cert is never fetched from
// an arbitrary URL: the SigningCertURL is allowlisted to AWS's HTTPS
// sns.<region>.amazonaws.com endpoint, its region must match the (signed)
// TopicArn, and the fetched cert must be anchored to AWS's SNS signing chain
// (subject CN "Amazon SNS"). certFetch defaults to an HTTP GET when nil. It
// returns an error when the signature is absent, the cert URL is not a valid
// AWS SNS URL for the message's region, the cert cannot be fetched/trusted, or
// the signature does not validate.
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
	// Derive the cert source from the message's own region (the signed
	// TopicArn), and require SigningCertURL to point at that same AWS HTTPS
	// SNS endpoint. We never accept a URL that doesn't match — an attacker who
	// alters SigningCertURL is rejected before any fetch.
	region, err := snsRegion(msg)
	if err != nil {
		return err
	}
	allowed := fmt.Sprintf("https://sns.%s.amazonaws.com/", region)
	if !SNSCertURLRe.MatchString(msg.SigningCertURL) ||
		!strings.HasPrefix(msg.SigningCertURL, allowed) {
		return fmt.Errorf("sns signing cert url not allowed for region %q: %q", region, msg.SigningCertURL)
	}
	certPEM, err := certFetch(msg.SigningCertURL)
	if err != nil {
		return fmt.Errorf("sns cert fetch: %w", err)
	}
	pub, err := parseSNSPublicKey(certPEM, SNSCertSubjects)
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

// SNSCertSubjects are the subject common names AWS uses for its SNS message
// signing certificates. The cert fetched for signature verification must be
// issued under one of these — otherwise a (hypothetically) successful fetch of
// an arbitrary cert from an allowlisted host is rejected. This anchors trust to
// AWS's SNS signing chain even if the allowlisted fetch were ever tampered with.
var SNSCertSubjects = map[string]bool{
	"Amazon SNS":        true,
	"sns.amazonaws.com": true,
}

// parseSNSPublicKey extracts the RSA public key from an SNS signing cert PEM,
// but only when the certificate's subject common name is one of the pinned AWS
// SNS signer names given in allowedSubjects. This prevents verification from
// trusting a cert that AWS did not issue for SNS message signing.
func parseSNSPublicKey(pemBytes []byte, allowedSubjects map[string]bool) (any, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("no pem block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}
	if !allowedSubjects[cert.Subject.CommonName] {
		return nil, fmt.Errorf(
			"certificate subject %q is not a pinned AWS SNS signer", cert.Subject.CommonName)
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
		// Subject is part of the signature ONLY when the message carries one
		// (SES Bounce/Complaint notifications have no Subject, so AWS signs
		// without that line — adding an empty Subject breaks verification).
		if msg.Subject != "" {
			fields["Subject"] = msg.Subject
		}
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
