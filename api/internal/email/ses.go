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
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
)

// ErrSuppressed is returned when the recipient is on the suppression list and
// the message was therefore NOT handed to SES. It is a *skip*, not a failure:
// every caller must map it to a logged no-op — never an error response, never a
// queue retry, never a dead letter.
var ErrSuppressed = errors.New("email suppressed")

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
	// Suppressor, when non-nil, is consulted before EVERY SES send. This is
	// the single pre-send choke point: every outbound message in the repo —
	// buyer receipts, merchant notifications, per-form notifications and all
	// auth/invite mail, whether sent synchronously or by the background email
	// worker — reaches SES through SendReceipt/SendReceiptWithAttachment below,
	// so guarding them covers present and future callers alike. Call-site
	// checks remain as an extra layer but are no longer load-bearing.
	//
	// nil → no check at all (bare-mode tests, or a deployment with SES but no
	// database).
	Suppressor Suppressor
}

// New builds a sender. The client is created lazily on first send so a missing
// region/credentials doesn't fail construction.
func New(cfg SMTPConfig) *Sender {
	if cfg.Region == "" {
		cfg.Region = "ap-southeast-1"
	}
	return &Sender{cfg: cfg, from: cfg.From}
}

// NewWithSuppressor builds a sender whose every send is checked against the
// suppression list first.
func NewWithSuppressor(cfg SMTPConfig, s Suppressor) *Sender {
	snd := New(cfg)
	snd.Suppressor = s
	return snd
}

// suppressed reports whether this send must be skipped because the recipient is
// on the suppression list. It is the pre-send choke point guard.
//
//   - A nil Suppressor means "no check configured" and never blocks a send
//     (bare-mode tests, SES-without-DB).
//   - The check runs on context.Background(): the SES call in this file already
//     does, and threading a request ctx down would force a change to the
//     VerifyMailer interface and every recorder mailer for no gain. The lookup
//     is a single indexed primary-key hit.
//   - A store error FAILS OPEN: it is logged as email_suppression_check_error
//     and the send proceeds. A fail-closed rule would strand a paying buyer's
//     receipt and a registering user's verify link on a database blip, and the
//     suppression list is a reputation guard, not a correctness gate.
//   - A hit is logged as one structured email_suppressed_skip line, so an
//     operator can always tell why a message never left.
func (s *Sender) suppressed(to, subject string) bool {
	if s.Suppressor == nil {
		return false
	}
	sup, err := ShouldSuppress(context.Background(), s.Suppressor, to)
	if err != nil {
		// Fail open, loudly. Never silently swallow a real suppression: the
		// error path is always logged, and only an infrastructure error lands
		// here.
		slog.Error("email_suppression_check_error", "to", obfuscate(to), "err", err)
		return false
	}
	if sup {
		slog.Warn("email_suppressed_skip", "to", obfuscate(to), "subject", subject)
		return true
	}
	return false
}

// obfuscate masks the local part of an address for logs (e.g.
// "wan***@jomform.com") so a skip line is greppable without writing full PII
// into the log. Local to this package: the email package must not import the
// HTTP layer that owns the equivalent helper.
func obfuscate(addr string) string {
	addr = strings.TrimSpace(addr)
	at := strings.Index(addr, "@")
	if at <= 1 {
		return "***"
	}
	local, domain := addr[:at], addr[at:]
	if len(local) <= 3 {
		return local[:1] + "***" + domain
	}
	return local[:3] + "***" + domain
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
	// The pre-send choke point. Checked before the SES client is even
	// constructed so a suppressed address costs one indexed lookup and never
	// opens a connection.
	if s.suppressed(to, subject) {
		return "", ErrSuppressed
	}
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
	// Same pre-send choke point as SendReceipt — the second and last
	// sesv2.SendEmail call in the repo.
	if s.suppressed(to, subject) {
		return "", ErrSuppressed
	}
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
