// Rendered proof of the feedback notification emails, and the assertions that
// keep the proof honest.
//
// The three defects on this card were all in a string a person READS: a missing
// Reply-To, a link to the wrong page, and Malay copy for a non-Malay reader.
// Unit tests on the individual helpers prove each piece; this file renders the
// whole message through the SHIPPED PATH and asserts the facts a reader would
// notice, so a regression in how the pieces COMPOSE cannot hide behind green
// helper tests.
//
// WHAT "THE SHIPPED PATH" MEANS HERE, AND WHY IT MATTERS. The message a reader
// receives is assembled in two places: the handler renders the subject and the
// plain-text body (emailtext.Text for the copy, feedbackThreadLink for the link),
// and the SES sender renders the branded HTML shell around that body at send
// time. An earlier version of this file called a language-aware wrapper directly
// to produce the HTML — a function the product never called on this path — so its
// per-language <html lang>/footer assertions passed over dead code while the live
// message carried lang="en" over a Malay body. That is the bug this file exists
// to prevent recurring, so it now drives the REAL chain instead:
//
//	feedbackMailJob (what the handler enqueues)
//	  → EmailDispatcher.Send (the background worker's routing)
//	    → pay.Receipts.Sender.Send(job.To, job.Subject, job.Body, job.Lang)
//	      → the SESv2 SendEmail request (read back out of the captured request)
//
// The assertions run on the HTML in the request SES was actually asked to send,
// so losing the language anywhere along that chain turns them red.
//
// It is also the evidence generator for the card: with JOMFORM_EMAIL_PROOF_DIR set
// it writes the subject, the plain-text body and the fully branded HTML for a
// given reader language, so a human can open the message and read it. The
// assertions run either way — no database, no queue, no network.
package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/wzul/jomform/api/internal/email"
	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/emailtext"
	"github.com/wzul/jomform/api/internal/payments"
)

const (
	proofBaseURL = "https://jomform.com"
	proofSubject = "Checkout button does nothing"
	proofExcerpt = "Thanks for reporting this - fixed in release 1.4.2, please reload the page."
	proofThread  = "5e01c110-d9e4-412d-bfd5-f9c4e240a75b"
	// proofFiler is the address the notification goes to. Asserted against the
	// captured request, so a test cannot pass against a message addressed
	// somewhere else.
	proofFiler = "filer@jomform.test"
)

// sentMessage is one message the shipped path handed to SES: everything a reader
// would see, read back out of the real SESv2 request.
type sentMessage struct {
	To      string
	Subject string
	// Body is the plain-text alternative; HTML is the branded shell around it.
	Body string
	HTML string
	// Lang is the shell's declared language, read out of the HTML SES received.
	Lang string
}

// capturingSender returns an *email.Sender pointed at a local SESv2 endpoint, and
// a function returning the raw JSON body of the last request SES received.
//
// Nothing inside the app is faked: the dispatcher runs for real, the sender
// builds a real request through the SDK, and this only records the JSON the SDK
// serialised — the bytes a real SES deployment would receive.
func capturingSender(t *testing.T) (*email.Sender, func() string) {
	t.Helper()
	var mu sync.Mutex
	var last []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		last = body
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"MessageId":"fake-ses-message-id"}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("AWS_ENDPOINT_URL_SESV2", srv.URL)

	sender := email.New(email.SMTPConfig{
		From: "noreply@jomform.com", Region: "ap-southeast-1",
		Username: "AKIAEXAMPLE", Password: "secretexample",
	})
	return sender, func() string {
		mu.Lock()
		defer mu.Unlock()
		return string(last)
	}
}

// simpleMessageOf decodes the Simple-path content of a captured SESv2 SendEmail
// request into everything a reader would see.
func simpleMessageOf(t *testing.T, raw string) sentMessage {
	t.Helper()
	var got struct {
		Destination struct {
			ToAddresses []string `json:"ToAddresses"`
		} `json:"Destination"`
		Content struct {
			Simple struct {
				Subject struct {
					Data string `json:"Data"`
				} `json:"Subject"`
				Body struct {
					Text struct {
						Data string `json:"Data"`
					} `json:"Text"`
					Html struct {
						Data string `json:"Data"`
					} `json:"Html"`
				} `json:"Body"`
			} `json:"Simple"`
		} `json:"Content"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode the SESv2 request SES received: %v\nbody: %s", err, raw)
	}
	to := ""
	if len(got.Destination.ToAddresses) == 1 {
		to = got.Destination.ToAddresses[0]
	}
	html := got.Content.Simple.Body.Html.Data
	return sentMessage{
		To:      to,
		Subject: got.Content.Simple.Subject.Data,
		Body:    got.Content.Simple.Body.Text.Data,
		HTML:    html,
		Lang:    declaredLang(html),
	}
}

// declaredLang reads the lang attribute out of a rendered shell. It returns ""
// when the shell carries none, which no assertion should accept.
func declaredLang(html string) string {
	const marker = `<html lang="`
	i := strings.Index(html, marker)
	if i < 0 {
		return ""
	}
	rest := html[i+len(marker):]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// deliverThroughTheShippedPath runs one job through the real dispatcher and the
// real sender, and returns the message SES was asked to deliver.
func deliverThroughTheShippedPath(t *testing.T, job emailqueue.Job) sentMessage {
	t.Helper()
	sender, lastRaw := capturingSender(t)
	d := &EmailDispatcher{Pay: &payments.Service{
		Receipts: &payments.Receipts{Sender: sender},
	}}
	if err := d.Send(context.Background(), job); err != nil {
		t.Fatalf("the shipped path failed to deliver %q to %s: %v", job.Subject, job.To, err)
	}
	return simpleMessageOf(t, lastRaw())
}

// filerReplyJob is exactly what the handler enqueues for a filer reply: the
// localised subject and body (rendered from the shared dictionary), the thread
// link, and the reader's language.
func filerReplyJob(lang string) emailqueue.Job {
	text := emailtext.Text(lang, emailtext.FeedbackReply, map[string]string{
		"subject": proofSubject,
		"excerpt": proofExcerpt,
		"link":    feedbackThreadLink(proofBaseURL, proofThread),
	})
	return feedbackMailJob(proofFiler, text.Subject, text.Body, text.Lang)
}

// operatorNotifyJob is what the handler enqueues when a thread is filed: the
// platform team's alert, rendered in the FILER's language (the thread's words are
// being quoted back to the team, which is the language transport that matters
// here).
func operatorNotifyJob(lang string) emailqueue.Job {
	text := emailtext.Text(lang, emailtext.FeedbackNew, map[string]string{
		"from":    proofFiler,
		"status":  "open",
		"subject": proofSubject,
		"excerpt": proofExcerpt,
		"link":    feedbackAdminLink(proofBaseURL, proofThread),
	})
	return feedbackMailJob(proofFiler, text.Subject, text.Body, text.Lang)
}

// TestFeedbackEmailRendersForTheFiler pins the composed message: the subject, the
// body AND the branded shell all agree on the reader's language, and the link
// names the thread — asserted on the SESv2 request the sender produced, which is
// the message a reader receives.
func TestFeedbackEmailRendersForTheFiler(t *testing.T) {
	cases := []struct {
		lang          string
		wantSubject   string
		wantBodyBit   string
		wantFooterBit string
	}{
		{"en", "New reply on your feedback", "We have replied to your feedback.", "Made in Malaysia"},
		// The Malay strings are the PRE-FIX literals: they must survive for a
		// Malay reader, which is what makes the English case meaningful.
		{"ms", "Balasan baharu pada maklum balas anda", "Kami telah membalas maklum balas anda.", "Dibuat di Malaysia"},
		{"zh", "您收到的反馈有新回复", "我们已回复您的反馈。", "马来西亚制造"},
		{"ta", "உங்கள் கருத்துக்கு புதிய பதில்", "உங்கள் கருத்துக்கு நாங்கள் பதிலளித்துள்ளோம்.", "மலேசியாவில்"},
		{"ar", "رد جديد على ملاحظاتك", "لقد قمنا بالرد على ملاحظاتك.", "صُنع في ماليزيا"},
		{"", "New reply on your feedback", "We have replied to your feedback.", "Made in Malaysia"},
	}
	wantLink := proofBaseURL + "/feedback?thread=" + proofThread
	for _, c := range cases {
		sent := deliverThroughTheShippedPath(t, filerReplyJob(c.lang))

		// ANTI-VACUITY: the request must be the one addressed to the filer —
		// otherwise these assertions could pass against a message that went
		// nowhere.
		if sent.To != proofFiler {
			t.Fatalf("lang=%q: the captured request was not addressed to the filer: %q", c.lang, sent.To)
		}
		if sent.Subject != c.wantSubject {
			t.Errorf("lang=%q subject = %q, want %q", c.lang, sent.Subject, c.wantSubject)
		}
		if !strings.Contains(sent.Body, c.wantBodyBit) {
			t.Errorf("lang=%q body is missing %q:\n%s", c.lang, c.wantBodyBit, sent.Body)
		}
		// The whole message must carry the thread link, and the link must be the
		// SAME string in the body and reachable in the HTML (a link the client
		// cannot turn into an anchor is not a link).
		if !strings.Contains(sent.Body, wantLink) {
			t.Errorf("lang=%q body does not link the thread (%s):\n%s", c.lang, wantLink, sent.Body)
		}
		if !strings.Contains(sent.HTML, wantLink) {
			t.Errorf("lang=%q branded HTML does not carry the thread link", c.lang)
		}
		// THE DEFECT: the shell's footer must be in the language the subject and
		// the body were written in. This reads the HTML the SENDER produced, so
		// it fails if the language stops reaching the sender at all.
		if !strings.Contains(sent.HTML, c.wantFooterBit) {
			t.Errorf("lang=%q the shell SES was asked to deliver is not in the message's language: "+
				"its footer is missing %q. A %q body under a shell that declares another language is "+
				"the defect this card reworks.\nhtml head:\n%s",
				c.lang, c.wantFooterBit, c.lang, headOf(sent.HTML))
		}
		// The shell must not declare a language the body is not in.
		wantLang := c.lang
		if wantLang == "" {
			wantLang = "en"
		}
		if sent.Lang != wantLang {
			t.Errorf("lang=%q shell declares lang=%q, want %q:\n%s",
				c.lang, sent.Lang, wantLang, headOf(sent.HTML))
		}
		if c.lang != "ms" && strings.Contains(sent.HTML, "Hak cipta terpelihara") {
			t.Errorf("lang=%q message carries the Malay footer copyright line", c.lang)
		}
	}
}

// TestFeedbackEmailRendersForTheOperator pins the operator-side message: the copy
// follows the FILER's language (the filer's own words are quoted back to the
// team), and the link names the FILED THREAD.
//
// The pre-fix defect here was Malay to everyone; the language rework QA asked for
// was a language-agnostic operator message, which the shared dictionary does not
// express (it renders every kind in the reader's language). So the assertion is
// the transport property that matters: whatever the operator receives, the copy
// is in one locale and the shell declares exactly that locale.
func TestFeedbackEmailRendersForTheOperator(t *testing.T) {
	link := feedbackAdminLink(proofBaseURL, proofThread)
	if link != proofBaseURL+"/admin?thread="+proofThread {
		t.Fatalf("operator link = %q, want the admin deep link", link)
	}
	sent := deliverThroughTheShippedPath(t, operatorNotifyJob(""))
	for _, want := range []string{
		"New feedback received",
		proofFiler,
		link,
		proofSubject,
	} {
		if !strings.Contains(sent.Subject+"\n"+sent.Body, want) {
			t.Errorf("operator message is missing %q:\nsubject: %s\n%s", want, sent.Subject, sent.Body)
		}
	}
	// The pre-fix defect was Malay here too.
	if strings.Contains(sent.Subject+sent.Body, "Maklum balas baharu diterima") {
		t.Errorf("operator notification carries the pre-fix Malay subject line:\n%s", sent.Subject)
	}
	if sent.Lang != "en" {
		t.Errorf("operator shell must be English, got lang=%q:\n%s", sent.Lang, headOf(sent.HTML))
	}
}

// TestFeedbackEmailProofGoesThroughTheShippedPath is the bite guard for this
// file: it fails if the proof starts asserting on a rendering the product does
// not produce.
//
// The earlier version of this file rendered the shell by calling the shell's own
// language-aware entry point directly, so every per-language assertion passed
// while the live message carried lang="en" — the assertions were green over dead
// code. A test can only be as good as the path it reads, so this pins the path:
// the HTML asserted above must come out of the shipped chain, and it must DIFFER
// between locales. If the language stopped reaching the sender, every locale
// would render the same English shell and this fails.
func TestFeedbackEmailProofGoesThroughTheShippedPath(t *testing.T) {
	msSent := deliverThroughTheShippedPath(t, filerReplyJob("ms"))
	enSent := deliverThroughTheShippedPath(t, filerReplyJob("en"))

	// The request must carry an HTML part at all: an empty one would make every
	// shell assertion in this file vacuous.
	if msSent.HTML == "" {
		t.Fatal("the shipped path sent no HTML part, so the shell assertions would be vacuous")
	}
	// The same Malay body must produce a DIFFERENT shell for a Malay reader and
	// an English one. This is what a direct call to the wrapper could not
	// establish: it proves the language survives the whole chain.
	if msSent.HTML == enSent.HTML {
		t.Fatal("a Malay reader and an English reader received byte-identical shells — the reader's " +
			"language is not reaching the branded HTML on the shipped path")
	}
	if !strings.Contains(msSent.HTML, `<html lang="ms"`) {
		t.Errorf("the Malay message's shell does not declare Malay:\n%s", headOf(msSent.HTML))
	}
	if strings.Contains(msSent.HTML, `<html lang="en"`) {
		t.Errorf("the Malay message's shell declares English:\n%s", headOf(msSent.HTML))
	}
}

// TestReceiptJobWithAttachmentCarriesTheLanguageOnTheRawPath covers the OTHER
// send path through the dispatcher. The feedback notification has no attachment,
// so it takes the Simple path — but the dispatcher also routes receipt jobs with
// a PDF through the Raw/MIME path, and a language that only reaches one of the
// two paths is the same class of bug this card fixes (the Reply-To fix had to be
// made on both for exactly this reason).
//
// The job here carries a language no receipt sets today; the point is that the
// dispatcher forwards whatever language the job has, so the moment a caller
// knows its reader's language the shell follows it on either path.
func TestReceiptJobWithAttachmentCarriesTheLanguageOnTheRawPath(t *testing.T) {
	sender, lastRaw := capturingSender(t)
	d := &EmailDispatcher{Pay: &payments.Service{
		Receipts: &payments.Receipts{Sender: sender},
	}}
	if err := d.Send(context.Background(), emailqueue.Job{
		Type: "receipt", To: proofFiler,
		Subject: "Balasan baharu pada maklum balas anda",
		Body:    "Kami telah membalas maklum balas anda.",
		Lang:    "ms",
		PDFFilename: "resit.pdf", PDFData: []byte("PDFDATA"),
	}); err != nil {
		t.Fatalf("dispatcher refused a receipt job with an attachment: %v", err)
	}

	html := unquotedPrintable(rawMessageOf(t, lastRaw()))
	if !strings.Contains(html, `<html lang="ms"`) {
		t.Errorf("the Raw/MIME path rendered the shell in another language:\n%s", headOf(html))
	}
	if !strings.Contains(html, "Dibuat di Malaysia") {
		t.Errorf("the Raw/MIME path's shell footer is not Malay:\n%s", headOf(html))
	}
}

// rawMessageOf extracts and decodes Content.Raw.Data from a captured SESv2
// SendEmail request body.
func rawMessageOf(t *testing.T, raw string) string {
	t.Helper()
	var sent struct {
		Content struct {
			Raw struct {
				Data string `json:"Data"`
			} `json:"Raw"`
		} `json:"Content"`
	}
	if err := json.Unmarshal([]byte(raw), &sent); err != nil {
		t.Fatalf("decode the captured SESv2 request: %v\nbody: %s", err, raw)
	}
	if sent.Content.Raw.Data == "" {
		t.Fatalf("the captured request carried no raw message: %s", raw)
	}
	dec, err := base64.StdEncoding.DecodeString(sent.Content.Raw.Data)
	if err != nil {
		t.Fatalf("decode the raw message: %v", err)
	}
	return string(dec)
}

// unquotedPrintable reverses the quoted-printable encoding the raw MIME's HTML
// part uses, so a test can read the shell as text. Without this the non-ASCII
// footer bytes would be =XX on the wire and an assertion on them would fail for
// the wrong reason (or, worse, pass on an ASCII-only English shell).
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

// TestGenerateFeedbackEmailProof writes the rendered messages to disk when
// JOMFORM_EMAIL_PROOF_DIR is set, so the card's evidence is the message itself
// rather than a description of it. Its assertions above run regardless.
//
// The HTML written here is the HTML from the SESv2 request, so the file a human
// opens is the message the reader would get.
func TestGenerateFeedbackEmailProof(t *testing.T) {
	dir := os.Getenv("JOMFORM_EMAIL_PROOF_DIR")
	if dir == "" {
		t.Skip("JOMFORM_EMAIL_PROOF_DIR not set; the assertions in TestFeedbackEmailRenders* still ran")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir proof dir: %v", err)
	}

	writeProof := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	for _, lang := range []string{"en", "ms", "zh", "ta", "ar"} {
		sent := deliverThroughTheShippedPath(t, filerReplyJob(lang))
		writeProof("filer-reply-"+lang+".txt",
			"Subject: "+sent.Subject+"\nTo: "+sent.To+"\nReply-To: <SES_REPLY_TO>\n\n"+sent.Body)
		writeProof("filer-reply-"+lang+".html", sent.HTML)
	}
	// The unset-language case is the common one: users.lang is NULL until the
	// user picks, and it must render English (this is the defect).
	unsetSent := deliverThroughTheShippedPath(t, filerReplyJob(""))
	writeProof("filer-reply-UNSET.html", unsetSent.HTML)

	adminSent := deliverThroughTheShippedPath(t, operatorNotifyJob(""))
	writeProof("admin-notify-en.html", adminSent.HTML)
	writeProof("admin-notify-en.txt",
		"Subject: "+adminSent.Subject+"\nTo: "+adminSent.To+"\nReply-To: <SES_REPLY_TO>\n\n"+adminSent.Body)

	files, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		t.Fatalf("glob proof dir: %v", err)
	}
	t.Logf("wrote %d proof file(s) to %s", len(files), dir)
}

// headOf is the first few lines of a rendered message, for failure output that
// shows the shell's declaration without dumping the whole template.
func headOf(html string) string {
	lines := strings.SplitN(html, "\n", 4)
	if len(lines) > 3 {
		lines = lines[:3]
	}
	return strings.Join(lines, "\n")
}
