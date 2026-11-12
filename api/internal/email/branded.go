// Branded HTML email template (issue #339).
//
// Every transactional email JomForm sends (receipts, merchant notifications,
// verification, magic link, password reset, 2FA backup, social-connect, invite,
// feedback) is wrapped in this shared corporate layout: the JomForm logo
// wordmark, brand colors, and a consistent header/footer. The plain-text body is
// preserved as the multipart fallback so clients without HTML still work.
//
// The template is deliberately email-client-safe: inline CSS only, table-based
// layout, no external images (the logo is a styled text wordmark), and no
// JavaScript. It renders fine in Gmail, Outlook, Apple Mail, and mobile clients.
//
// # Language (language consistency)
//
// BrandedHTML takes the language the message was RENDERED IN and declares it on
// the document: `<html lang="…">`, plus `dir="rtl"` for Arabic — the same rule
// the public form renderer already applies (httpapi/menu.go). Before this the
// wrapper emitted `lang="ms"` unconditionally, so a Chinese or English email
// still told the client it was Malay. The footer sentences come from the shared
// transactional dictionary too, so nothing inside the shell contradicts the
// declared language. An unset/unknown code resolves to English, never Malay.
//
// The language travels with the MESSAGE, not with the wrapper's caller: the
// feedback notifications carry the filer's `users.lang` in emailqueue.Job.Lang
// and hand it to the sender, so a Malay message goes out with `<html lang="ms">`
// and the Malay footer. An earlier shape had a second, language-agnostic entry
// point that hard-wired "" into this function, which made the per-language
// shell unreachable on the shipped send path; there is deliberately one entry
// point now, and one place for the language to come from.
package email

import (
	"html"
	"strings"

	"github.com/wzul/jomform/api/internal/emailtext"
)

// Brand colors used across the JomForm app.
const (
	brandIndigo = "#5e6ad2"
	brandDeep   = "#4338ca"
	// codeSurface is the tinted block a device code is printed on (card
	// t_4f3ab16e). It is the shell's page background, so the block reads as a
	// panel rather than as another line of prose.
	codeSurface = "#f4f5fb"
)

// ── Line markers: the one exception to "the body is plain text" ─────────────
//
// A transactional body is PLAIN TEXT that the dictionary owns (one string per
// locale, api/internal/emailtext/catalog.go) and it is sent twice: as the
// multipart text part, and escaped into the body cell of this branded shell. That
// is the right shape for a receipt and the wrong shape for a security code the
// reader has to FIND and RETYPE. The device-authorisation mail was the proof:
// "Your code: KX9M-X5BS" was one line inside the paragraph block, and a reader
// scanning for a big code did not see one (card t_4f3ab16e, the second half of
// Wan's report: "dalam email tak dinyatakan code pun").
//
// So a body may carry two LINE-level markers, and this file is the only place
// that decides what they look like:
//
//	[[code:VALUE]]         a large, monospaced, centred block on a tinted panel
//	[[button:LABEL|URL]]   a button, with the URL printed underneath it
//
// This is not a general mark-up. Only a line whose whole content is one marker is
// expanded, every value is HTML-escaped, an unrecognised or malformed marker is
// left as text (never half-rendered), and a body without markers renders
// byte-for-byte as it did before — TestBrandedHTML pins that, because every other
// transactional email goes through this function.
//
// The plain-text part gets the same lines rendered back to their plain meaning by
// plainTextBody ("  VALUE" and "LABEL: URL"), so no marker ever reaches an inbox
// and a client that shows only text still sees the code on a line of its own.
const (
	markerOpen  = "[["
	markerClose = "]]"
)

// markerArg returns the argument of `[[name:ARG]]` when line is exactly that
// marker, and ok=false for anything else (including a line that merely starts
// with one — a half-expanded marker is a bug, not a rendering decision).
func markerArg(line, name string) (string, bool) {
	prefix := markerOpen + name + ":"
	if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, markerClose) {
		return "", false
	}
	arg := strings.TrimSpace(line[len(prefix) : len(line)-len(markerClose)])
	if arg == "" || strings.Contains(arg, markerOpen) {
		return "", false
	}
	return arg, true
}

// buttonParts parses `[[button:LABEL|URL]]`. ok is false for any other line; a
// marker that parses but carries no usable http(s) URL also reports false, so the
// caller can degrade to plain text rather than emit a dead control.
func buttonParts(line string) (label, url string, ok bool) {
	inner, ok := markerArg(line, "button")
	if !ok {
		return "", "", false
	}
	i := strings.IndexByte(inner, '|')
	if i <= 0 || i == len(inner)-1 {
		return "", "", false
	}
	label = strings.TrimSpace(inner[:i])
	url = strings.TrimSpace(inner[i+1:])
	if label == "" || !(strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://")) {
		return "", "", false
	}
	return label, url, true
}

// codeBlockHTML is the block the reader cannot miss: >=22px, monospaced,
// letter-spaced, centred, on the tinted surface, and in a table so email clients
// that ignore `div` styling still stack it as its own row.
func codeBlockHTML(code string) string {
	return `<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="margin:6px 0 12px 0;">` +
		`<tr><td align="center" style="background-color:` + codeSurface + `;border:1px solid #e5e7eb;border-radius:10px;padding:20px 12px;">` +
		`<span style="font-family:'Courier New',Courier,monospace;font-size:28px;font-weight:700;letter-spacing:6px;color:#111827;">` +
		html.EscapeString(code) +
		`</span></td></tr></table>`
}

// buttonHTML renders the action as a button and prints the URL underneath it, so
// a client that strips the link (or a reader who wants to check where it goes)
// still has the destination. No prose is emitted here: this package holds no
// copy, so anything a reader must READ comes from the dictionary.
func buttonHTML(label, url string) string {
	esc := html.EscapeString(url)
	return `<div style="margin:0 0 12px 0;">` +
		`<a href="` + esc + `" style="display:inline-block;background-color:` + brandIndigo + `;color:#ffffff;` +
		`font-size:16px;font-weight:700;line-height:1;text-decoration:none;padding:14px 28px;border-radius:8px;">` +
		html.EscapeString(label) +
		`</a>` +
		`<div style="margin-top:8px;font-size:12px;line-height:1.5;color:#6b7280;word-break:break-all;">` + esc + `</div>` +
		`</div>`
}

// renderBodyHTML escapes the body line by line and expands the two markers.
func renderBodyHTML(body string) string {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	var b strings.Builder
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if code, ok := markerArg(trimmed, "code"); ok {
			b.WriteString(codeBlockHTML(code))
			continue
		}
		if label, url, ok := buttonParts(trimmed); ok {
			b.WriteString(buttonHTML(label, url))
			continue
		}
		b.WriteString(html.EscapeString(line))
		if i < len(lines)-1 {
			b.WriteString("<br/>")
		}
	}
	return b.String()
}

// plainTextBody is the multipart fallback: the same body with the markers reduced
// to what they mean in plain text, so the code stays on a line of its own and the
// link is still an address the reader can copy.
func plainTextBody(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if code, ok := markerArg(trimmed, "code"); ok {
			lines[i] = "  " + code
			continue
		}
		if label, url, ok := buttonParts(trimmed); ok {
			lines[i] = label + ": " + url
		}
	}
	return strings.Join(lines, "\n")
}

// BrandedHTML wraps a plain-text email body in the shared JomForm corporate
// HTML layout. The body is HTML-escaped and newlines are converted to <br> so
// any plain-text content (receipts, notifications, auth links) renders safely
// and predictably inside the branded shell.
//
// lang is the language the message was rendered in (emailtext.NormalizeLang
// pins it to one of the five supported locales, defaulting to English). The
// footer sentences come from the same dictionary, so the shell never contradicts
// the language it declares.
func BrandedHTML(lang, title, body string) string {
	l := emailtext.NormalizeLang(lang)
	escaped := renderBodyHTML(body)
	dir := ""
	if emailtext.IsRTL(l) {
		dir = ` dir="rtl"`
	}

	return `<!DOCTYPE html>
<html lang="` + l + `"` + dir + ` xmlns="http://www.w3.org/1999/xhtml">
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
              <div>` + html.EscapeString(emailtext.Phrase(l, emailtext.PhraseMadeIn)) + `</div>
              <div style="padding-top:4px;">` + html.EscapeString(emailtext.Phrase(l, emailtext.PhraseDisclaimer)) + `</div>
              <div style="padding-top:4px;">` + html.EscapeString(emailtext.Phrase(l, emailtext.PhraseCopyright)) + `</div>
            </td>
          </tr>
        </table>
      </td>
    </tr>
  </table>
</body>
</html>`
}
