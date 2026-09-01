// Package email — SES SMTP email sender.
package email

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
	"strings"
)

// smtpNewClient wraps smtp.NewClient for testability.
var smtpNewClient = smtp.NewClient

// SMTPConfig holds SES SMTP relay settings.
type SMTPConfig struct {
	Host     string // e.g. email-smtp.ap-southeast-1.amazonaws.com
	Port     string // 587 (STARTTLS) or 465 (implicit TLS)
	Username string // SES SMTP access key (AKIA…)
	Password string // SES SMTP secret key
	From     string // verified sender, e.g. noreply@jomform.com
}

// Sender delivers transactional email via SES SMTP with TLS.
type Sender struct {
	cfg SMTPConfig
}

// New builds a sender.
func New(cfg SMTPConfig) *Sender { return &Sender{cfg: cfg} }

// Send delivers a plain-text email.
func (s *Sender) Send(to, subject, body string) error {
	host := hostOf(s.cfg.Host)
	addr := fmt.Sprintf("%s:%s", s.cfg.Host, s.cfg.Port)

	headers := map[string]string{
		"From":         s.cfg.From,
		"To":           to,
		"Subject":      subject,
		"MIME-Version": "1.0",
		"Content-Type": `text/plain; charset="utf-8"`,
	}
	msg := buildMessage(headers, body)

	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		return fmt.Errorf("ses dial: %w", err)
	}
	defer conn.Close()

	c, err := smtpNewClient(conn, host)
	if err != nil {
		return fmt.Errorf("ses client: %w", err)
	}
	defer c.Quit()

	return s.authAndSend(c, to, msg)
}

// authAndSend performs SMTP AUTH then the MAIL/RCPT/DATA sequence. Both SES
// entry points share it (Send and SendWithAttachment) — without AUTH, SES
// rejects DATA with "530 Authentication Required", which previously surfaced
// only as a receipt warning in the logs.
func (s *Sender) authAndSend(c *smtp.Client, to string, msg []byte) error {
	if s.cfg.Username != "" && s.cfg.Password != "" {
		host := hostOf(s.cfg.Host)
		if ok, _ := c.Extension("AUTH"); ok {
			if err := c.Auth(smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, host)); err != nil {
				return fmt.Errorf("smtp auth: %w", err)
			}
		} else {
			return fmt.Errorf("smtp server does not advertise AUTH (host %s)", host)
		}
	}
	if err := c.Mail(s.cfg.From); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("rcpt to: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	return w.Close()
}

func buildMessage(headers map[string]string, body string) []byte {
	var b strings.Builder
	for _, k := range []string{"From", "To", "Subject", "MIME-Version", "Content-Type"} {
		if v := headers[k]; v != "" {
			b.WriteString(k + ": " + v + "\r\n")
		}
	}
	b.WriteString("\r\n")
	return append([]byte(b.String()), []byte(body)...)
}

func hostOf(endpoint string) string {
	// strip optional scheme
	if i := strings.Index(endpoint, "://"); i >= 0 {
		return endpoint[i+3:]
	}
	return endpoint
}