// Package email — SESv2 API email sender.
//
// Switched from SES SMTP to the SESv2 SendEmail API (2026-09-01): the account's
// SMTP endpoint rejects every derived credential with 535 (account-level issue,
// only the SES console can mint working SMTP creds), while the SESv2 API send
// works with plain IAM keys. So we authenticate via SigV4 with the IAM access
// key/secret that were already in the env (SES_SMTP_USER = access key id,
// SES_SMTP_PASS = secret access key) — no new credentials required.
package email

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
)

// SMTPConfig holds SES credentials. Despite the name (kept for compatibility),
// these are now used for the SESv2 API, not SMTP:
//   - Username = IAM access key id (AKIA…)
//   - Password = IAM secret access key
//   - Host     = unused (kept for config compatibility)
//   - Port     = unused
//   - From     = verified sender, e.g. noreply@jomform.com
type SMTPConfig struct {
	Host     string
	Port     string
	Username string
	Password string
	From     string
	Region   string // AWS region for SESv2 (default ap-southeast-1)
}

// Sender delivers transactional email via the SESv2 SendEmail API.
type Sender struct {
	cfg  SMTPConfig
	from string
	cli  *sesv2.Client
}

// New builds a sender. The client is created lazily on first send so a missing
// region/credentials doesn't fail construction.
func New(cfg SMTPConfig) *Sender {
	if cfg.Region == "" {
		cfg.Region = "ap-southeast-1"
	}
	return &Sender{cfg: cfg, from: cfg.From}
}

// client returns a lazily-initialised SESv2 client using static IAM creds.
func (s *Sender) client() (*sesv2.Client, error) {
	if s.cli != nil {
		return s.cli, nil
	}
	optFns := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(s.cfg.Region),
	}
	if s.cfg.Username != "" && s.cfg.Password != "" {
		optFns = append(optFns, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(s.cfg.Username, s.cfg.Password, ""),
		))
	}
	cfg, err := awsconfig.LoadDefaultConfig(context.Background(), optFns...)
	if err != nil {
		return nil, fmt.Errorf("ses config: %w", err)
	}
	s.cli = sesv2.NewFromConfig(cfg)
	return s.cli, nil
}

// Send delivers a plain-text email via SESv2 SendEmail.
func (s *Sender) Send(to, subject, body string) error {
	_, err := s.SendReceipt(to, subject, body)
	return err
}

// SendReceipt delivers a branded multipart email via SESv2 SendEmail and
// returns the SES MessageID (issue #106). The MessageID is the correlation key
// the SNS endpoint uses to tie an Open/Click/Delivered event back to the sale
// that sent the receipt. An empty MessageID (SES accepted without a message id)
// is not an error.
//
// Issue #339: the email is sent as multipart — a plain-text part (fallback for
// clients without HTML) plus a branded HTML part (JomForm logo, colors, layout).
func (s *Sender) SendReceipt(to, subject, body string) (string, error) {
	cli, err := s.client()
	if err != nil {
		return "", err
	}
	out, err := cli.SendEmail(context.Background(), &sesv2.SendEmailInput{
		FromEmailAddress: aws.String(s.from),
		Destination: &types.Destination{
			ToAddresses: []string{to},
		},
		Content: &types.EmailContent{
			Simple: &types.Message{
				Subject: &types.Content{Data: aws.String(subject)},
				Body: &types.Body{
					Text: &types.Content{Data: aws.String(body)},
					Html: &types.Content{Data: aws.String(BrandedHTML(subject, body))},
				},
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("ses send: %w", err)
	}
	if out.MessageId == nil {
		return "", nil
	}
	return *out.MessageId, nil
}

// SendWithAttachment delivers a multipart/mixed email with a base64 attachment
// (PDF receipts) via SESv2 SendEmail using a raw MIME message.
func (s *Sender) SendWithAttachment(to, subject, body string, filename string, data []byte) error {
	_, err := s.SendReceiptWithAttachment(to, subject, body, filename, data)
	return err
}

// SendReceiptWithAttachment delivers a multipart/mixed email with a base64
// attachment (PDF receipts) via SESv2 SendEmail using a raw MIME message, and
// returns the SES MessageID (issue #106) for open/click correlation.
func (s *Sender) SendReceiptWithAttachment(to, subject, body string, filename string, data []byte) (string, error) {
	cli, err := s.client()
	if err != nil {
		return "", err
	}
	raw := buildMultipart(s.from, to, subject, body, filename, data)
	out, err := cli.SendEmail(context.Background(), &sesv2.SendEmailInput{
		FromEmailAddress: aws.String(s.from),
		Destination: &types.Destination{
			ToAddresses: []string{to},
		},
		Content: &types.EmailContent{
			Raw: &types.RawMessage{
				Data: raw,
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("ses send attachment: %w", err)
	}
	if out.MessageId == nil {
		return "", nil
	}
	return *out.MessageId, nil
}

// buildMultipart assembles a multipart/mixed MIME message with a base64
// attachment, matching the previous SMTP attachment format. The body is a
// nested multipart/alternative carrying both the plain-text fallback and the
// branded HTML part (issue #339).
func buildMultipart(from, to, subject, body, filename string, data []byte) []byte {
	boundary := "jf-boundary-7f3a9c1b"
	altBoundary := "jf-alt-4d2e8f0a"
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: multipart/mixed; boundary=\"" + boundary + "\"\r\n\r\n")

	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: multipart/alternative; boundary=\"" + altBoundary + "\"\r\n\r\n")

	b.WriteString("--" + altBoundary + "\r\n")
	b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n\r\n")
	b.WriteString(body + "\r\n")

	b.WriteString("--" + altBoundary + "\r\n")
	b.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n")
	b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
	b.WriteString(quotedPrintable(BrandedHTML(subject, body)) + "\r\n")

	b.WriteString("--" + altBoundary + "--\r\n")

	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: application/pdf; name=\"" + filename + "\"\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n")
	b.WriteString("Content-Disposition: attachment; filename=\"" + filename + "\"\r\n\r\n")

	enc := base64.StdEncoding.EncodeToString(data)
	for i := 0; i < len(enc); i += 76 {
		end := i + 76
		if end > len(enc) {
			end = len(enc)
		}
		b.WriteString(enc[i:end] + "\r\n")
	}
	b.WriteString("--" + boundary + "--\r\n")
	return []byte(b.String())
}

// quotedPrintable encodes a string for the quoted-printable transfer encoding
// so long HTML lines (which email clients choke on) are soft-wrapped at 76
// chars. Non-ASCII bytes are encoded as =XX so the HTML stays 7-bit safe.
func quotedPrintable(s string) string {
	var b strings.Builder
	line := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		var enc string
		if c == '=' {
			enc = "=3D"
		} else if c >= 33 && c <= 126 && c != '=' {
			enc = string(c)
		} else {
			enc = "=" + strings.ToUpper(fmt.Sprintf("%02X", c))
		}
		if line+len(enc) > 75 {
			b.WriteString("=\r\n")
			line = 0
		}
		b.WriteString(enc)
		line += len(enc)
	}
	return b.String()
}