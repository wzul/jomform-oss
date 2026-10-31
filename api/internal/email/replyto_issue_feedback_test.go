// Reply-To on every outbound message, and a branded shell that speaks the
// message's language.
//
// THE DEFECT THIS FILE PINS (a): the raw MIME builder wrote only From/To/Subject,
// and the Simple path set no ReplyToAddresses, so a filer who hit Reply in their
// mail client sent the answer to `From` — noreply@jomform.com, an address with no
// reader. The reply was lost silently and the thread looked abandoned.
//
// WHY THE RAW BUILDER IS THE ORACLE. buildMultipart is testable directly and is
// the function that assembles the MIME, so asserting on its bytes proves the
// header is in the message that would go to SES. The Simple path cannot be read
// back from the SDK call without a fake endpoint, so it is asserted through the
// request the SDK actually serialises: fakeSESCapture records the JSON body SESv2
// received, and the test reads ReplyToAddresses out of it. That is an observation
// of the sent request, not a mock of the header.
//
// The per-language shell assertions run on BOTH SES send paths, because a
// language carried by only one of them is the same defect class as a Reply-To
// written by only one of them.
//
// The footer strings asserted below come from the shared transactional dictionary
// (internal/emailtext) — the ONE place the repo's email copy lives — so the
// expectations are read out of the dictionary rather than copied beside it.
package email

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/wzul/jomform/api/internal/emailtext"
)

// fakeSESCapture is fakeSES that also records the last request body, so a test
// can assert on what SES was actually asked to send.
func fakeSESCapture(t *testing.T) *captureSES {
	t.Helper()
	c := &captureSES{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.calls++
		c.last = body
		c.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"MessageId":"fake-ses-message-id"}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("AWS_ENDPOINT_URL_SESV2", srv.URL)
	return c
}

type captureSES struct {
	mu    sync.Mutex
	calls int
	last  []byte
}

func (c *captureSES) body() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return string(c.last)
}

func (c *captureSES) n() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// TestBuildMultipartCarriesReplyTo proves the raw/MIME path writes the header,
// and that an empty list writes NO header rather than an empty one.
func TestBuildMultipartCarriesReplyTo(t *testing.T) {
	msg := string(buildMultipart(
		"noreply@jomform.com", "filer@example.test",
		[]string{"support@jomform.com"},
		"New reply to your feedback", "Badan", "", "resit.pdf", []byte("PDFDATA"),
	))
	if !strings.Contains(msg, "Reply-To: support@jomform.com\r\n") {
		t.Fatalf("the MIME must carry a Reply-To header line.\ngot:\n%s", firstLines(msg, 8))
	}
	// A Reply-To must sit in the HEADER block, before the first blank line —
	// a header written after the body is just text the client will show.
	head := msg
	if i := strings.Index(msg, "\r\n\r\n"); i >= 0 {
		head = msg[:i]
	}
	if !strings.Contains(head, "Reply-To: support@jomform.com") {
		t.Fatalf("Reply-To appears outside the header block:\n%s", head)
	}

	// No addresses → no header. `Reply-To: ` with nothing after it is not a
	// header a mail client can act on, so it must be omitted entirely.
	bare := string(buildMultipart("noreply@jomform.com", "a@b.c", nil, "s", "b", "", "f.pdf", []byte("x")))
	if strings.Contains(bare, "Reply-To") {
		t.Fatalf("an unconfigured sender must write NO Reply-To header, got:\n%s", firstLines(bare, 8))
	}
}

// TestSenderReplyToIsCleanedAndDeduped pins the sender-side list: blank entries
// are dropped (SES rejects an empty address) and the value reaches both paths.
func TestSenderReplyToIsCleanedAndDeduped(t *testing.T) {
	s := New(SMTPConfig{
		From: "noreply@jomform.com", Region: "ap-southeast-1",
		ReplyTo: []string{"  ", "support@jomform.com", ""},
	})
	got := s.replyTo()
	if len(got) != 1 || got[0] != "support@jomform.com" {
		t.Fatalf("replyTo() = %#v, want exactly [support@jomform.com]", got)
	}
	empty := New(SMTPConfig{From: "noreply@jomform.com"}).replyTo()
	if len(empty) != 0 {
		t.Fatalf("a sender with no Reply-To configured returned %#v", empty)
	}
}

// TestSendReceiptSendsReplyToToSES is the Simple-path half: the request SESv2
// actually receives must name the Reply-To address.
func TestSendReceiptSendsReplyToToSES(t *testing.T) {
	c := fakeSESCapture(t)
	s := New(SMTPConfig{
		From: "noreply@jomform.com", Region: "ap-southeast-1",
		Username: "AKIAEXAMPLE", Password: "secretexample",
		ReplyTo: []string{"support@jomform.com"},
	})
	if _, err := s.SendReceipt("filer@example.test", "New reply to your feedback", "Badan", ""); err != nil {
		t.Fatalf("SendReceipt: %v", err)
	}
	if c.n() != 1 {
		t.Fatalf("expected exactly 1 SES call, got %d", c.n())
	}
	var sent struct {
		ReplyToAddresses []string `json:"ReplyToAddresses"`
		Destination      struct {
			ToAddresses []string `json:"ToAddresses"`
		} `json:"Destination"`
	}
	if err := json.Unmarshal([]byte(c.body()), &sent); err != nil {
		t.Fatalf("decode the SESv2 request SES received: %v\nbody: %s", err, c.body())
	}
	if len(sent.ReplyToAddresses) != 1 || sent.ReplyToAddresses[0] != "support@jomform.com" {
		t.Fatalf("the Simple path sent ReplyToAddresses=%#v; a reply would go to the From address "+
			"(noreply@jomform.com) where nothing reads it.\nrequest: %s",
			sent.ReplyToAddresses, c.body())
	}
	// ANTI-VACUITY: the same request must genuinely be the one addressed to the
	// filer — otherwise this test passes against a request that went nowhere.
	if len(sent.Destination.ToAddresses) != 1 || sent.Destination.ToAddresses[0] != "filer@example.test" {
		t.Fatalf("the captured request was not addressed to the filer: %#v", sent.Destination.ToAddresses)
	}
}

// TestSendReceiptWithAttachmentCarriesReplyTo is the Raw-path end-to-end check:
// the MIME handed to SES carries the header.
func TestSendReceiptWithAttachmentCarriesReplyTo(t *testing.T) {
	c := fakeSESCapture(t)
	s := New(SMTPConfig{
		From: "noreply@jomform.com", Region: "ap-southeast-1",
		Username: "AKIAEXAMPLE", Password: "secretexample",
		ReplyTo: []string{"support@jomform.com"},
	})
	if _, err := s.SendReceiptWithAttachment("filer@example.test", "Resit", "Badan", "",
		"resit.pdf", []byte("PDFDATA")); err != nil {
		t.Fatalf("SendReceiptWithAttachment: %v", err)
	}
	// The raw message is base64 inside the JSON, so decode out of the envelope
	// rather than searching for the literal header text.
	raw := rawMessageOf(t, c.body())
	if !strings.Contains(raw, "Reply-To: support@jomform.com") {
		t.Fatalf("the raw/MIME path did not carry Reply-To.\nraw:\n%s", firstLines(raw, 10))
	}
}

// rawMessageOf extracts and decodes Content.Raw.Data from a captured SESv2
// SendEmail request body.
func rawMessageOf(t *testing.T, body string) string {
	t.Helper()
	var sent struct {
		Content struct {
			Raw struct {
				Data string `json:"Data"`
			} `json:"Raw"`
		} `json:"Content"`
	}
	if err := json.Unmarshal([]byte(body), &sent); err != nil {
		t.Fatalf("decode SESv2 request: %v\nbody: %s", err, body)
	}
	if sent.Content.Raw.Data == "" {
		t.Fatalf("the request carried no raw message (content was not Raw): %s", body)
	}
	// The SDK base64-encodes the blob; the captured body carries it as a string.
	dec, err := base64.StdEncoding.DecodeString(sent.Content.Raw.Data)
	if err != nil {
		t.Fatalf("decode raw message: %v", err)
	}
	return string(dec)
}

// TestBrandedHTMLFollowsTheMessage pins the shell's lang attribute and footer:
// the defect was a hardcoded lang="ms" over every message, whatever language the
// message itself was written in.
//
// The footer expectation is read from the shared dictionary, so this test cannot
// pass by agreeing with a private copy of the footer while the messages go out
// with another one.
func TestBrandedHTMLFollowsTheMessage(t *testing.T) {
	footerMS := emailtext.Phrase(emailtext.Ms, emailtext.PhraseCopyright)
	cases := []struct{ lang, wantAttr, wantFooter string }{
		{"en", `<html lang="en"`, "Made in Malaysia"},
		{"ms", `<html lang="ms"`, "Dibuat di Malaysia"},
		{"zh", `<html lang="zh"`, "马来西亚制造"},
		{"ta", `<html lang="ta"`, "மலேசியாவில்"},
		{"ar", `<html lang="ar"`, "صُنع في ماليزيا"},
		// Unset is the common case (users.lang is NULL until the user chooses)
		// and must be English, not Malay.
		{"", `<html lang="en"`, "Made in Malaysia"},
		// An unknown value must not be interpolated into the attribute.
		{"xx\"><script>", `<html lang="en"`, "Made in Malaysia"},
	}
	for _, c := range cases {
		got := BrandedHTML(c.lang, "Subject", "Body")
		if !strings.Contains(got, c.wantAttr) {
			t.Errorf("lang=%q: shell must contain %q\ngot: %s", c.lang, c.wantAttr, firstLines(got, 3))
		}
		if !strings.Contains(got, c.wantFooter) {
			t.Errorf("lang=%q: footer must contain %q\ngot: %s", c.lang, c.wantFooter, firstLines(got, 3))
		}
	}
	// The Malay shell's footer must be the dictionary's Malay copyright, not a
	// second translation living in this package.
	if !strings.Contains(BrandedHTML("ms", "S", "B"), footerMS) {
		t.Errorf("the Malay shell's copyright line is not the dictionary's %q", footerMS)
	}
	// The unset case must NOT claim Malay anywhere in the shell — this is the
	// regression the card names ("<html lang=\"ms\">" over an English body).
	if strings.Contains(BrandedHTML("", "Subject", "Body"), `lang="ms"`) {
		t.Error("an unset language still produced a Malay shell")
	}
	// Arabic is the one RTL locale: direction must follow it.
	if !strings.Contains(BrandedHTML("ar", "S", "B"), `dir="rtl"`) {
		t.Error("arabic shell must be dir=rtl")
	}
	if strings.Contains(BrandedHTML("en", "S", "B"), `dir="rtl"`) {
		t.Error("english shell must not be dir=rtl")
	}
	// An empty language keeps behaving exactly as before: the English shell.
	if !strings.Contains(BrandedHTML("", "S", "B"), `<html lang="en"`) {
		t.Error("an empty language must render the English shell")
	}
}

// TestShippedSendPathsCarryTheReadersLanguageToTheShell is the shipped-path
// half of the shell's language, and the test that would have caught the defect
// this reworks.
//
// The helper test above proves BrandedHTML obeys a language it is HANDED. It
// cannot prove the product ever hands it one — and in an earlier shape it did
// not: both SES send paths called a language-agnostic wrapper that passed "", so
// every message, whatever language its subject and body were written in, got the
// English shell. The per-locale footer was unreachable on the only path that
// reaches a reader.
//
// So this asserts on what SES was actually asked to send, for both paths: Simple
// (SendReceipt) and Raw/MIME (SendReceiptWithAttachment). A missing language
// parameter anywhere in that chain makes the message declare English over Malay
// copy and this goes red.
func TestShippedSendPathsCarryTheReadersLanguageToTheShell(t *testing.T) {
	const (
		subject = "Balasan baharu pada maklum balas anda"
		body    = "Kami telah membalas maklum balas anda."
	)
	// The assertions are on the message the reader receives, so they are the
	// facts a reader would notice: the declared language and the footer lines.
	assertMalayShell := func(t *testing.T, path, html string) {
		t.Helper()
		for what, want := range map[string]string{
			"<html lang> declaration": `<html lang="ms"`,
			"footer line 1":           "JomForm — Dibuat di Malaysia.",
			"footer sentence":         emailtext.Phrase(emailtext.Ms, emailtext.PhraseDisclaimer),
			"footer copyright":        emailtext.Phrase(emailtext.Ms, emailtext.PhraseCopyright),
		} {
			if !strings.Contains(html, want) {
				t.Errorf("%s path: the message's %s is not Malay, so a Malay body went out under a shell "+
					"that claims another language.\nwant %q in:\n%s", path, what, want, firstLines(html, 3))
			}
		}
		// ANTI-VACUITY: the English footer must be absent, or "contains Malay"
		// would also pass on a shell carrying both. The English string is read
		// from the same dictionary, so this cannot agree with a private copy.
		if en := emailtext.Phrase(emailtext.En, emailtext.PhraseCopyright); strings.Contains(html, en) {
			t.Errorf("%s path: the English footer %q is still present alongside the Malay one:\n%s",
				path, en, firstLines(html, 3))
		}
	}

	t.Run("simple", func(t *testing.T) {
		c := fakeSESCapture(t)
		s := New(SMTPConfig{
			From: "noreply@jomform.com", Region: "ap-southeast-1",
			Username: "AKIAEXAMPLE", Password: "secretexample",
		})
		if _, err := s.SendReceipt("ms@jomform.test", subject, body, "ms"); err != nil {
			t.Fatalf("SendReceipt: %v", err)
		}
		var sent struct {
			Destination struct {
				ToAddresses []string `json:"ToAddresses"`
			} `json:"Destination"`
			Content struct {
				Simple struct {
					Body struct {
						Html struct {
							Data string `json:"Data"`
						} `json:"Html"`
					} `json:"Body"`
				} `json:"Simple"`
			} `json:"Content"`
		}
		if err := json.Unmarshal([]byte(c.body()), &sent); err != nil {
			t.Fatalf("decode the SESv2 request SES received: %v\nbody: %s", err, c.body())
		}
		if len(sent.Destination.ToAddresses) != 1 || sent.Destination.ToAddresses[0] != "ms@jomform.test" {
			t.Fatalf("the captured request was not addressed to the Malay filer: %#v", sent.Destination.ToAddresses)
		}
		assertMalayShell(t, "simple", sent.Content.Simple.Body.Html.Data)
	})

	t.Run("raw-mime", func(t *testing.T) {
		c := fakeSESCapture(t)
		s := New(SMTPConfig{
			From: "noreply@jomform.com", Region: "ap-southeast-1",
			Username: "AKIAEXAMPLE", Password: "secretexample",
		})
		if _, err := s.SendReceiptWithAttachment("ms@jomform.test", subject, body, "ms",
			"resit.pdf", []byte("PDFDATA")); err != nil {
			t.Fatalf("SendReceiptWithAttachment: %v", err)
		}
		// The HTML part is quoted-printable inside the raw MIME, so the
		// assertions run on the decoded message: the footer's non-ASCII bytes
		// are =XX-encoded in the wire form.
		assertMalayShell(t, "raw-mime", unquotedPrintable(rawMessageOf(t, c.body())))
	})
}

// unquotedPrintable reverses quotedPrintable so a test can read the HTML part
// of a raw MIME message as text. Non-ASCII is what matters here (the Malay and
// Chinese/Tamil/Arabic footers), so an incomplete decoder would produce
// mojibake and fail the assertions for the wrong reason.
func unquotedPrintable(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '=' {
			b.WriteByte(c)
			continue
		}
		// A soft line break: "=" followed by CRLF (or a bare LF).
		if i+1 < len(s) && s[i+1] == '\r' {
			i++
			if i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
			continue
		}
		if i+1 < len(s) && s[i+1] == '\n' {
			i++
			continue
		}
		if i+2 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
				b.WriteByte(byte(v))
				i += 2
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

// firstLines trims a message for failure output so a full MIME dump does not
// bury the assertion.
func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
