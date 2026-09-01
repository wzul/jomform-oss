// Attachment-capable multipart email (PDF receipts).
package email

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"strings"
)

// SendWithAttachment delivers multipart/mixed with a base64 attachment.
func (s *Sender) SendWithAttachment(to, subject, body string, filename string, data []byte) error {
	host := hostOf(s.cfg.Host)
	addr := fmt.Sprintf("%s:%s", s.cfg.Host, s.cfg.Port)

	boundary := "jf-boundary-7f3a9c1b"
	var b strings.Builder
	b.WriteString("From: " + s.cfg.From + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: multipart/mixed; boundary=\"" + boundary + "\"\r\n\r\n")

	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n\r\n")
	b.WriteString(body + "\r\n")

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

	// Same authenticated path as Send — see authAndSend in ses.go.
	return s.authAndSend(c, to, []byte(b.String()))
}