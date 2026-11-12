package email

// The device-consent mail must make its code IMPOSSIBLE TO MISS (card
// t_4f3ab16e, the second half of Wan's report: "dalam email tak dinyatakan code
// pun").
//
// WHAT WAS WRONG, MEASURED. The template did carry the code — `api/internal/emailtext/catalog.go`
// DeviceAuthorization has `{code}` in all five locales, `oauth_device.go`
// deliverDeviceCode passes `"code": userCode`, and branded.go escaped it into
// the shell. But it arrived as one more line of the paragraph block
// ("Your code: KX9M-X5BS"), i.e. the same weight as "Access : read-only". A
// reader told by their harness "enter this code at jomform.com" scans the mail
// for a CODE and finds a sentence, so the mail reads as if it said nothing.
//
// WHAT IS PINNED HERE, and why each assertion is the defect rather than a style
// preference:
//
//	1. the rendered HTML carries the ACTUAL code for a fixture — not the
//	   `{code}` placeholder. A template that loses the var, or a renderer that
//	   escapes the block away, is red here;
//	2. it is in a block of its own: >=22px, monospaced, letter-spaced, centred,
//	   on the tinted panel. That is the "large enough to retype across a room"
//	   property the report is about;
//	3. the block sits ABOVE the button, so the eye meets the code before the
//	   action — the way a bank's OTP mail reads;
//	4. the link is a BUTTON, and the URL is printed under it, so a client that
//	   strips links still shows where the button goes;
//	5. the plain-text part (the multipart fallback) has no marker syntax in it
//	   and still shows the code on a line of its own;
//	6. all five locales, and `en` in English.
//
// The markers are `[[code:…]]` / `[[button:…]]`, expanded by the one renderer in
// branded.go; the dictionary stays plain text (see the DeviceAuthorization entry),
// which is why the same body can be sent twice without two copies drifting.
import (
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/emailtext"
)

// The fixture the operator's report is written in: a real-shaped code and a real
// verification_uri_complete.
const (
	fixtureCode = "KX9M-X5BS"
	fixtureLink = "https://jomform.com/profile?tab=mcp&user_code=KX9M-X5BS"
)

func deviceMailFor(t *testing.T, lang string) emailtext.Message {
	t.Helper()
	return emailtext.Text(lang, emailtext.DeviceAuthorization, map[string]string{
		"client": "Claude Desktop",
		"scope":  "read-only — read your forms, products and sales",
		"where":  "Kuala Lumpur, MY",
		"ip":     "203.0.113.9",
		"time":   "2026-11-12 03:04 UTC",
		"code":   fixtureCode,
		"link":   fixtureLink,
	})
}

// TestDeviceMailCarriesTheCodeAsABlockOfItsOwn is the card's evidence: the code
// the harness is waiting for is rendered, at a size a person cannot read past, in
// every locale.
func TestDeviceMailCarriesTheCodeAsABlockOfItsOwn(t *testing.T) {
	for _, lang := range emailtext.Langs {
		t.Run(lang, func(t *testing.T) {
			msg := deviceMailFor(t, lang)
			if !strings.Contains(msg.Body, "[[code:") {
				t.Fatalf("%s: the dictionary body lost its code marker — the code is a line of prose again:\n%s", lang, msg.Body)
			}
			html := BrandedHTML(msg.Lang, msg.Subject, msg.Body)

			// 1. the ACTUAL code, not the placeholder.
			if !strings.Contains(html, fixtureCode) {
				t.Fatalf("%s: the rendered mail does not contain the code %q. A reader cannot retype what is not there.\n%s", lang, fixtureCode, html)
			}
			if strings.Contains(html, "{code}") {
				t.Fatalf("%s: the `{code}` placeholder survived into the rendered mail", lang)
			}
			// 2. a block of its own: the code is inside the 28px monospaced span,
			//    on the tinted panel, centred.
			block := `<span style="font-family:'Courier New',Courier,monospace;font-size:28px;font-weight:700;letter-spacing:6px;color:#111827;">` + fixtureCode + `</span>`
			if !strings.Contains(html, block) {
				t.Fatalf("%s: the code is not rendered in its own large monospaced block.\nwant substring: %s", lang, block)
			}
			if !strings.Contains(html, `align="center" style="background-color:`+codeSurface) {
				t.Fatalf("%s: the code block is not centred on the tinted surface", lang)
			}
			// 3. the code comes BEFORE the action.
			codeAt := strings.Index(html, block)
			buttonAt := strings.Index(html, `href="`+strings.ReplaceAll(fixtureLink, "&", "&amp;")+`"`)
			if buttonAt < 0 {
				t.Fatalf("%s: the verification link is not in the mail as an href", lang)
			}
			if codeAt > buttonAt {
				t.Fatalf("%s: the button is rendered above the code — the eye meets the action before the code it needs", lang)
			}
			// 4. the URL is printed under the button, so a client that strips the
			//    link still shows where it goes.
			if strings.Count(html, strings.ReplaceAll(fixtureLink, "&", "&amp;")) < 2 {
				t.Fatalf("%s: the verification URL appears only as a button target; a text-only client would show a label with no destination", lang)
			}
			// 5. no marker syntax leaks, in either part.
			if strings.Contains(html, markerOpen) || strings.Contains(html, markerClose) {
				t.Fatalf("%s: raw marker syntax reached the HTML part", lang)
			}
			textPart := plainTextBody(msg.Body)
			if strings.Contains(textPart, markerOpen) || strings.Contains(textPart, markerClose) {
				t.Fatalf("%s: raw marker syntax reached the plain-text part:\n%s", lang, textPart)
			}
			if !strings.Contains(textPart, "\n  "+fixtureCode+"\n") {
				t.Fatalf("%s: the code is not on a line of its own in the text part (a text-only client must still see it):\n%s", lang, textPart)
			}
			if !strings.Contains(textPart, fixtureLink) {
				t.Fatalf("%s: the text part does not carry the verification URL", lang)
			}
			// 6. the phishing defence the card says to keep.
			if !strings.Contains(textPart, fixtureCode) {
				t.Fatalf("%s: text part lost the code", lang)
			}
		})
	}
}

// TestDeviceMailEnglishReadsAsEnglish: `en` is the documented default and the
// locale the board's own measurements are taken in, so a body that fell back to
// Malay would otherwise satisfy every "the code is there" assertion above.
func TestDeviceMailEnglishReadsAsEnglish(t *testing.T) {
	msg := deviceMailFor(t, "")
	if msg.Lang != emailtext.En {
		t.Fatalf("an unset language resolved to %q, want en", msg.Lang)
	}
	html := BrandedHTML(msg.Lang, msg.Subject, msg.Body)
	for _, want := range []string{
		fixtureCode,
		"Enter it on jomform.com, or just open the button below.",
		"do not share this code",
		"Approve or deny",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the English mail is missing %q", want)
		}
	}
	if strings.Contains(html, "Kod anda") {
		t.Error("the English mail carries the Malay code label")
	}
}

// TestBrandedHTMLStillTreatsAMarkerlessBodyAsPlainText is the other half of the
// contract: every OTHER transactional email goes through the same renderer, so a
// body with no markers has to render exactly as it did before (escaped, newlines
// as <br/>). Without this, the marker reader could start mangling receipts and
// nothing would say so.
func TestBrandedHTMLStillTreatsAMarkerlessBodyAsPlainText(t *testing.T) {
	html := BrandedHTML("en", "Subject", "Rujukan : <ABC123>\nJumlah  : RM 10.00")
	if !strings.Contains(html, "Rujukan : &lt;ABC123&gt;<br/>Jumlah  : RM 10.00") {
		t.Fatalf("a markerless body no longer renders as escaped plain text:\n%s", html)
	}
	// A malformed marker is left as text rather than half-rendered.
	html = BrandedHTML("en", "S", "[[code:]]\n[[button:no-url]]")
	if strings.Contains(html, `font-size:28px`) || strings.Contains(html, "<a href") {
		t.Fatalf("a malformed marker was rendered as a block:\n%s", html)
	}
	if !strings.Contains(html, "[[code:]]") {
		t.Fatalf("a malformed marker was swallowed instead of shown:\n%s", html)
	}
}
