// The buyer's receipt is ONE language: the branded shell SES was asked to send
// declares the same language as the body inside it, and its footer is written in
// that language too.
//
// WHERE THIS COMES FROM (card t_0e662927). The receipt path used to hand the
// sender no language at all, so the shell came out English (`<html lang="en">`
// with the English footer) while the body inside it was rendered in the buyer's
// language — for an uncustomised workspace, the Malay `defaultReceiptBody`. The
// defect is exactly the one the branded shell was built to remove, on a path the
// shell's own tests could not see: it is a MISMATCH between two languages, not a
// wrong string, so a unit test of either half passes while the message a buyer
// receives is wrong.
//
// The resolution now is one lookup, and it is the buyer axis the receipt already
// uses for its copy — form.config.lang ?? the workspace's default_lang ?? en
// (dbstore.EmailLang, receipt.go). The language travels WITH the message:
//
//	SendSaleReceipt(ref)                     the real entry point
//	  -> dbstore.EmailLang(form, workspace)  the ONE lookup
//	    -> emailqueue.Job{Lang: lang}        the message carries it
//	      -> EmailDispatcher.Send            the background worker's routing
//	        -> Sender.SendReceiptWithAttachment(job.Lang)
//	          -> the SESv2 SendEmail request SES was asked to send
//
// This file drives that whole chain for real — a real Valkey-backed queue, the
// real dispatcher, the real SDK request — and reads the language back out of the
// SESv2 request. It deliberately does NOT call the shell helper: an earlier proof
// on this defect asserted over a wrapper the product never called, and stayed
// green while the shipped message carried `lang="en"` over a Malay body. The
// assertions below are on the bytes a real SES deployment would receive, and
// TestReceiptShellProofGoesThroughTheShippedPath fails if the language stops
// reaching that request.
//
// The capturingSender / rawMessageOf / unquotedPrintable / declaredLang helpers
// live in feedback_email_proof_evidence_test.go, the same package: one SESv2
// capture harness, not a second copy of it.
package httpapi

import (
	"context"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/payments"
	"github.com/wzul/jomform/api/internal/testdb"
)

// receiptBuyer is the address the receipt is sent to. Asserted against the
// captured message, so no case can pass against a receipt that went elsewhere.
const receiptBuyer = "buyer@jomform.test"

// receiptShellCase is one resolution outcome: the buyer-axis inputs, the
// language they resolve to, and the copy that language produces. The four
// expectations are HARDCODED literals on purpose — using the dictionary as the
// oracle would let the dictionary and the shell drift together and still pass.
type receiptShellCase struct {
	name        string
	formLang    string // form.config.lang
	workspace   string // workspace default_lang
	wantLang    string // what the shell must declare
	wantSubject string
	wantBody    string // the body's first line — the language the buyer reads
	wantFooter  string // the shell's "made in" line, in the same language
}

var receiptShellCases = []receiptShellCase{
	// The case this card names: a Malay receipt must carry a Malay shell and the
	// Malay footer, not an English one.
	{
		name: "form is Malay", formLang: "ms", workspace: "",
		wantLang: "ms", wantSubject: "Resit pembayaran",
		wantBody:   "Terima kasih atas pembayaran anda!",
		wantFooter: "JomForm — Dibuat di Malaysia.",
	},
	// The other half of the buyer axis: the workspace's own default decides when
	// the form has no language of its own.
	{
		name: "workspace default is Malay", formLang: "", workspace: "ms",
		wantLang: "ms", wantSubject: "Resit pembayaran",
		wantBody:   "Terima kasih atas pembayaran anda!",
		wantFooter: "JomForm — Dibuat di Malaysia.",
	},
	// The form override wins, and the shell must not follow the workspace away
	// from the body it wraps.
	{
		name: "English form in a Malay workspace", formLang: "en", workspace: "ms",
		wantLang: "en", wantSubject: "Payment receipt",
		wantBody:   "Thank you for your payment!",
		wantFooter: "JomForm — Made in Malaysia.",
	},
	{
		name: "form is Chinese", formLang: "zh", workspace: "",
		wantLang: "zh", wantSubject: "付款收据",
		wantBody:   "感谢您的付款！",
		wantFooter: "JomForm — 马来西亚制造。",
	},
	{
		name: "form is Tamil", formLang: "ta", workspace: "",
		wantLang: "ta", wantSubject: "கட்டண ரசீது",
		wantBody:   "உங்கள் கட்டணத்திற்கு நன்றி!",
		wantFooter: "JomForm — மலேசியாவில் உருவாக்கப்பட்டது.",
	},
	// Arabic is the RTL locale: the shell's direction must follow it.
	{
		name: "form is Arabic", formLang: "ar", workspace: "",
		wantLang: "ar", wantSubject: "إيصال الدفع",
		wantBody:   "شكراً لك على دفعتك!",
		wantFooter: "JomForm — صُنع في ماليزيا.",
	},
	// The uncustomised workspace: nothing set anywhere. It must be English end to
	// end — this is the case the card was filed about, where the body was Malay
	// while the shell declared English. There is no Malay default left to ship.
	{
		name: "nothing set (uncustomised workspace)", formLang: "", workspace: "",
		wantLang: "en", wantSubject: "Payment receipt",
		wantBody:   "Thank you for your payment!",
		wantFooter: "JomForm — Made in Malaysia.",
	},
	// An unsupported code is not a language: it resolves to English, never to
	// Malay, and the shell must follow it there.
	{
		name: "unsupported form and workspace codes", formLang: "fr", workspace: "de",
		wantLang: "en", wantSubject: "Payment receipt",
		wantBody:   "Thank you for your payment!",
		wantFooter: "JomForm — Made in Malaysia.",
	},
}

// receiptRef is the sale as the payment callback hands it to the receipt sender.
// Only the buyer-axis language inputs and the money fields matter here; the ref
// code is asserted back out of the sent message, which is what makes a message
// produced by anything other than this entry point detectable.
func receiptRef(c receiptShellCase) payments.SaleRef {
	return payments.SaleRef{
		RefCode: "JF-SHELL1", BuyerEmail: receiptBuyer, BuyerName: "Siti Aminah",
		AmountSen: 2500, ItemsSummary: "1x Tiket", FormURL: "https://acme.jomform.com/tiket",
		FormLang: c.formLang, WorkspaceLang: c.workspace,
	}
}

// receiptThroughTheQueue drives the SHIPPED production chain for one case:
// SendSaleReceipt (which renders the copy and enqueues with the language it
// rendered it in) -> a real Valkey queue -> the real EmailDispatcher -> the real
// SESv2 request. It returns the queued job and the raw MIME SES was asked to
// send.
func receiptThroughTheQueue(t *testing.T, c receiptShellCase) (emailqueue.Job, string) {
	t.Helper()
	ctx := context.Background()
	rdb, prefix := testdb.Valkey(t)
	q := emailqueue.NewQueueWithPrefix(rdb, prefix)
	sender, lastRaw := capturingSender(t)
	svc := &payments.Service{
		Receipts:   &payments.Receipts{Sender: sender},
		EmailQueue: q,
	}

	svc.SendSaleReceipt(ctx, receiptRef(c))

	job, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("the receipt path enqueued nothing to send: %v", err)
	}
	if job.Type != "receipt" {
		t.Fatalf("the receipt path enqueued a %q job, not a receipt", job.Type)
	}
	if job.To != receiptBuyer {
		t.Fatalf("the queued receipt is addressed to %q, want %q", job.To, receiptBuyer)
	}
	if len(job.PDFData) == 0 {
		t.Errorf("the queued receipt carries no PDF, so the attachment branch production takes is not being exercised")
	}

	d := &EmailDispatcher{Pay: svc}
	if err := d.Send(ctx, job); err != nil {
		t.Fatalf("the worker's dispatcher refused the queued receipt: %v", err)
	}
	return job, rawMessageOf(t, lastRaw())
}

// TestReceiptShellFollowsItsBodyThroughTheQueue pins the composed message on the
// shipped path: for every resolution outcome the subject, the body and the
// branded shell agree on ONE language, read out of the SESv2 request SES was
// asked to send.
func TestReceiptShellFollowsItsBodyThroughTheQueue(t *testing.T) {
	for _, c := range receiptShellCases {
		t.Run(c.name, func(t *testing.T) {
			job, mime := receiptThroughTheQueue(t, c)

			// The hop the defect lived on: the queued job must declare the
			// language the body was rendered in. Asserted separately from the
			// shell so a failure says WHICH half lost the language.
			if job.Lang != c.wantLang {
				t.Errorf("the queued receipt declares lang=%q, want %q — the language that rendered "+
					"the body must travel with the message", job.Lang, c.wantLang)
			}
			// The job's own copy is the same language as the shell about to wrap it.
			if !strings.Contains(job.Subject, c.wantSubject) {
				t.Errorf("queued subject = %q, want one containing %q", job.Subject, c.wantSubject)
			}
			if !strings.Contains(job.Body, c.wantBody) {
				t.Errorf("queued body does not contain the %s copy %q:\n%s", c.wantLang, c.wantBody, job.Body)
			}

			assertReceiptShellMatchesItsBody(t, mime, c)
		})
	}
}

// TestReceiptShellFollowsItsBodyWhenTheQueueIsNotWired is the same proof for the
// synchronous branch (a bare deployment, or an install with SES but no queue):
// SendSaleReceipt sends directly, and the shell must still follow the body. The
// Reply-To fix on this repo had to be made on both send paths for exactly this
// reason — a fix that covers one path is not a fix.
func TestReceiptShellFollowsItsBodyWhenTheQueueIsNotWired(t *testing.T) {
	for _, c := range receiptShellCases {
		t.Run(c.name, func(t *testing.T) {
			sender, lastRaw := capturingSender(t)
			svc := &payments.Service{Receipts: &payments.Receipts{Sender: sender}}

			svc.SendSaleReceipt(context.Background(), receiptRef(c))

			assertReceiptShellMatchesItsBody(t, rawMessageOf(t, lastRaw()), c)
		})
	}
}

// assertReceiptShellMatchesItsBody reads one delivered receipt out of the raw
// MIME SES received and asserts the property the card is about: the shell
// declares the language of the body inside it, its footer is written in that
// language, and no other language leaks in.
func assertReceiptShellMatchesItsBody(t *testing.T, mime string, c receiptShellCase) {
	t.Helper()

	// ANTI-VACUITY: the message must be the one addressed to the buyer, and it
	// must be the receipt this entry point rendered. A hand-built shell, or a
	// message that went nowhere, cannot satisfy these.
	if !strings.Contains(mime, "To: "+receiptBuyer) {
		t.Fatalf("the captured SES message is not addressed to the buyer:\n%s", headOf(mime))
	}
	if !strings.Contains(mime, "JF-SHELL1") {
		t.Fatalf("the captured message carries no reference code, so it is not a rendered receipt:\n%s",
			headOf(mime))
	}
	if !strings.Contains(mime, "application/pdf") {
		t.Fatalf("the captured message has no receipt PDF part, so it did not come from the "+
			"receipt's own render+attach chain:\n%s", headOf(mime))
	}

	subject := mimeSubject(t, mime)
	text := mimePart(t, mime, `text/plain; charset="utf-8"`)
	html := mimePart(t, mime, `text/html; charset="utf-8"`)

	// The buyer's copy, in the case's language.
	if !strings.Contains(subject, c.wantSubject) {
		t.Errorf("subject = %q, want one containing %q (lang %s)", subject, c.wantSubject, c.wantLang)
	}
	if !strings.Contains(text, c.wantBody) {
		t.Errorf("the plain-text body is not the %s copy %q:\n%s", c.wantLang, c.wantBody, text)
	}

	// THE DEFECT: the shell SES was asked to send must be the language of the
	// body it wraps, and it must wrap THAT body.
	if !strings.Contains(html, c.wantBody) {
		t.Errorf("the branded shell does not enclose the %s body it declares (%q is missing):\n%s",
			c.wantLang, c.wantBody, headOf(html))
	}
	if got := declaredLang(html); got != c.wantLang {
		t.Errorf("the delivered shell declares lang=%q, want %q — a %s body under a shell that "+
			"declares another language is the defect this card fixes.\n%s",
			got, c.wantLang, c.wantLang, headOf(html))
	}
	if !strings.Contains(html, c.wantFooter) {
		t.Errorf("the shell's footer is not the %s line %q, so the shell contradicts its own "+
			"declaration:\n%s", c.wantLang, c.wantFooter, htmlFooterOf(t, html))
	}
	// Nothing Malay may survive outside a Malay message — the pre-fix default
	// template is the thing that used to leak into every language.
	if c.wantLang != "ms" {
		if strings.Contains(html, "Dibuat di Malaysia") {
			t.Errorf("a %s receipt carries the Malay footer line", c.wantLang)
		}
		if strings.Contains(text, "Terima kasih") {
			t.Errorf("a %s receipt carries the Malay body (the pre-fix default template):\n%s",
				c.wantLang, text)
		}
	}
	// Arabic is the RTL locale, and only Arabic is.
	if rtl := strings.Contains(html, `dir="rtl"`); rtl != (c.wantLang == "ar") {
		t.Errorf("dir=\"rtl\" present = %v for lang %q; only Arabic may declare it", rtl, c.wantLang)
	}
}

// TestReceiptShellProofGoesThroughTheShippedPath is the bite guard for this
// file: it fails if these assertions start reading a rendering the product does
// not produce, and it fails if the language stops reaching the request.
//
// An earlier proof on this defect called the shell's own language-aware entry
// point directly, so every per-language assertion passed over a path the product
// never took. The oracle here is that two differently-resolved receipts must
// produce two DIFFERENT shells on the SHIPPED chain (the queue, the dispatcher,
// the SESv2 request) — which a direct call to the helper could never establish.
func TestReceiptShellProofGoesThroughTheShippedPath(t *testing.T) {
	var ms, en receiptShellCase
	for _, c := range receiptShellCases {
		switch c.wantLang {
		case "ms":
			if ms.wantLang == "" {
				ms = c
			}
		case "en":
			if en.wantLang == "" {
				en = c
			}
		}
	}
	_, msMIME := receiptThroughTheQueue(t, ms)
	_, enMIME := receiptThroughTheQueue(t, en)

	msHTML := mimePart(t, msMIME, `text/html; charset="utf-8"`)
	enHTML := mimePart(t, enMIME, `text/html; charset="utf-8"`)
	if msHTML == "" {
		t.Fatal("the shipped path sent no branded HTML part, so every shell assertion in this file would be vacuous")
	}
	if msHTML == enHTML {
		t.Fatal("a Malay receipt and an English receipt produced byte-identical shells — the language " +
			"is not reaching the branded HTML on the shipped path")
	}
	if !strings.Contains(msHTML, `<html lang="ms"`) {
		t.Errorf("the Malay receipt's shell does not declare Malay:\n%s", headOf(msHTML))
	}
	if strings.Contains(msHTML, `<html lang="en"`) {
		t.Errorf("the Malay receipt's shell declares English:\n%s", headOf(msHTML))
	}
	if !strings.Contains(enHTML, `JomForm — Made in Malaysia.`) {
		t.Errorf("the English receipt's shell footer is not English:\n%s", headOf(enHTML))
	}
}

// mimeSubject reads the Subject header of the raw message SES was asked to send.
// Only the header block is searched, so a Subject-looking line inside the body
// cannot be mistaken for it.
func mimeSubject(t *testing.T, mime string) string {
	t.Helper()
	headers := mime
	if i := strings.Index(mime, "\r\n\r\n"); i >= 0 {
		headers = mime[:i]
	}
	const marker = "Subject: "
	i := strings.Index(headers, marker)
	if i < 0 {
		t.Fatalf("the message SES was asked to send carries no Subject header:\n%s", headOf(mime))
	}
	rest := headers[i+len(marker):]
	if j := strings.Index(rest, "\r\n"); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

// mimePart returns the decoded body of one part of the multipart/mixed message
// the attachment path hands to SES, identified by its Content-Type. The HTML part
// is quoted-printable, so it is decoded here: without that, a non-ASCII footer
// would be =XX on the wire and an assertion on it would fail for the wrong reason.
func mimePart(t *testing.T, mime, contentType string) string {
	t.Helper()
	marker := "Content-Type: " + contentType
	i := strings.Index(mime, marker)
	if i < 0 {
		t.Fatalf("the message SES was asked to send has no %s part:\n%s", contentType, headOf(mime))
	}
	rest := mime[i+len(marker):]
	j := strings.Index(rest, "\r\n\r\n")
	if j < 0 {
		t.Fatalf("the %s part has no body separator:\n%s", contentType, headOf(mime))
	}
	body := rest[j+4:]
	if k := strings.Index(body, "\r\n--"); k >= 0 {
		body = body[:k]
	}
	if strings.Contains(contentType, "text/html") {
		return unquotedPrintable(body)
	}
	return body
}

// htmlFooterOf is the tail of a rendered shell, where the footer lines live —
// failure output that shows the footer without dumping the whole template.
func htmlFooterOf(t *testing.T, html string) string {
	t.Helper()
	i := strings.Index(html, `padding:24px 0 0 0;font-size:12px`)
	if i < 0 {
		return headOf(html)
	}
	tail := html[i:]
	if len(tail) > 600 {
		tail = tail[:600]
	}
	return tail
}
