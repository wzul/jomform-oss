// Branded HTML email template (issue #339).
//
// Every transactional email JomForm sends (receipts, merchant notifications,
// verification, magic link, password reset, 2FA backup, social-connect, invite)
// is wrapped in this shared corporate layout: the JomForm logo wordmark, brand
// colors, and a consistent header/footer. The plain-text body is preserved as
// the multipart fallback so clients without HTML still work.
//
// The template is deliberately email-client-safe: inline CSS only, table-based
// layout, no external images (the logo is a styled text wordmark), and no
// JavaScript. It renders fine in Gmail, Outlook, Apple Mail, and mobile clients.
package email

import (
	"html"
	"strings"
)

// Brand colors used across the JomForm app.
const (
	brandIndigo = "#5e6ad2"
	brandDeep   = "#4338ca"
)

// BrandedHTML wraps a plain-text email body in the shared JomForm corporate
// HTML layout. The body is HTML-escaped and newlines are converted to <br> so
// any plain-text content (receipts, notifications, auth links) renders safely
// and predictably inside the branded shell.
func BrandedHTML(title, body string) string {
	escaped := html.EscapeString(body)
	escaped = strings.ReplaceAll(escaped, "\r\n", "\n")
	escaped = strings.ReplaceAll(escaped, "\n", "<br/>")

	return `<!DOCTYPE html>
<html lang="ms" xmlns="http://www.w3.org/1999/xhtml">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1.0"/>
<meta http-equiv="X-UA-Compatible" content="IE=edge"/>
<title>` + html.EscapeString(title) + `</title>
</head>
<body style="margin:0;padding:0;background-color:#f4f5fb;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;">
  <table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="background-color:#f4f5fb;padding:24px 12px;">
    <tr>
      <td align="center">
        <table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="max-width:600px;width:100%;">
          <!-- Header: logo wordmark -->
          <tr>
            <td align="center" style="padding:0 0 20px 0;">
              <table role="presentation" cellpadding="0" cellspacing="0" border="0">
                <tr>
                  <td style="font-size:26px;font-weight:800;letter-spacing:-0.5px;color:` + brandDeep + `;">Jom<span style="color:` + brandIndigo + `;">Form</span></td>
                </tr>
              </table>
            </td>
          </tr>
          <!-- Card -->
          <tr>
            <td style="background-color:#ffffff;border-radius:12px;border:1px solid #e5e7eb;padding:32px 28px;">
              <table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0">
                <tr>
                  <td style="padding:0 0 16px 0;border-bottom:3px solid ` + brandIndigo + `;">
                    <span style="font-size:20px;font-weight:700;color:#1f2937;">` + html.EscapeString(title) + `</span>
                  </td>
                </tr>
                <tr>
                  <td style="padding:20px 0 0 0;font-size:15px;line-height:1.6;color:#374151;">
                    ` + escaped + `
                  </td>
                </tr>
              </table>
            </td>
          </tr>
          <!-- Footer -->
          <tr>
            <td align="center" style="padding:24px 0 0 0;font-size:12px;line-height:1.5;color:#6b7280;">
              <div>JomForm — Made in Malaysia.</div>
              <div style="padding-top:4px;">Jika anda tidak meminta emel ini, anda boleh abaikan sahaja.</div>
              <div style="padding-top:4px;">&copy; JomForm. Hak cipta terpelihara.</div>
            </td>
          </tr>
        </table>
      </td>
    </tr>
  </table>
</body>
</html>`
}
