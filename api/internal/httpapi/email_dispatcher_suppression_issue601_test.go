// Dispatcher suppression tests (issue #601).
//
// The email worker is how production actually sends: with Valkey up, mail is
// enqueued and delivered by EmailDispatcher.Send. A suppression hit must be a
// *handled* job — logged, and returned as nil — never a retry and never a
// dead-letter entry, because a retry can never succeed.
//
// The senders are stubs here on purpose: the real guard lives in the email
// package (covered by ses_suppression_test.go against a fake SES endpoint), so
// what these tests pin is the dispatcher's treatment of the sentinel it gets
// back from a sender.
package httpapi

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/email"
	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/payments"
)

// suppressedMailer is a VerifyMailer that always reports the recipient as
// suppressed, exactly as (*email.Sender).Send does.
type suppressedMailer struct {
	calls int
}

func (m *suppressedMailer) Send(to, subject, body, lang string) error {
	m.calls++
	return email.ErrSuppressed
}

// capturingLogs routes the default slog logger into a buffer for one test.
func capturingLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// TestEmailDispatcherSuppressedAuthJobReturnsNil: an auth job whose recipient is
// suppressed must return nil (no retry, no dead letter) and log the skip as
// emailq_skip_suppressed.
func TestEmailDispatcherSuppressedAuthJobReturnsNil(t *testing.T) {
	logs := capturingLogs(t)
	mailer := &suppressedMailer{}
	d := &EmailDispatcher{AuthMailer: mailer}

	err := d.Send(context.Background(), emailqueue.Job{
		Type: "verify", To: "bounced@example.com", Subject: "Sahkan emel", Body: "…",
	})
	if err != nil {
		t.Fatalf("Send(verify) err = %v, want nil — a suppressed job must not be retried", err)
	}
	if mailer.calls != 1 {
		t.Fatalf("mailer calls = %d, want 1 (the dispatcher must still attempt the send "+
			"and let the sender's guard decide)", mailer.calls)
	}
	logged := logs.String()
	if !strings.Contains(logged, "emailq_skip_suppressed") {
		t.Fatalf("skip was not logged as emailq_skip_suppressed; log:\n%s", logged)
	}
	if !strings.Contains(logged, "verify") || !strings.Contains(logged, "bou***@example.com") {
		t.Fatalf("skip log must carry the job type and the obfuscated recipient; log:\n%s", logged)
	}
	// The full address must not be written to the log.
	if strings.Contains(logged, "bounced@example.com") {
		t.Fatalf("skip log leaked the full recipient address; log:\n%s", logged)
	}
	// A suppressed job must NOT be reported as sent.
	if strings.Contains(logged, "emailq_auth_sent") {
		t.Fatalf("a suppressed job was logged as sent; log:\n%s", logged)
	}
}

// TestEmailDispatcherNonSuppressionErrorStillRetries: the guard must not
// swallow real failures — an ordinary send error keeps its retry semantics.
func TestEmailDispatcherNonSuppressionErrorStillRetries(t *testing.T) {
	capturingLogs(t)
	want := errors.New("ses throttled")
	d := &EmailDispatcher{AuthMailer: failingMailer{err: want}}

	err := d.Send(context.Background(), emailqueue.Job{
		Type: "verify", To: "ok@example.com", Subject: "s", Body: "b",
	})
	if !errors.Is(err, want) {
		t.Fatalf("Send err = %v, want the underlying send error (must still retry)", err)
	}
}

// failingMailer always fails with a non-suppression error.
type failingMailer struct{ err error }

func (m failingMailer) Send(to, subject, body, lang string) error { return m.err }

// TestEmailDispatcherSuppressedReceiptJobReturnsNil: the receipt path (the one
// carrying a sale_ref and a PDF) behaves the same way.
func TestEmailDispatcherSuppressedReceiptJobReturnsNil(t *testing.T) {
	logs := capturingLogs(t)
	sender := email.NewWithSuppressor(email.SMTPConfig{From: "noreply@jomform.com"},
		&alwaysSuppressed{})
	d := &EmailDispatcher{Pay: &payments.Service{
		Receipts: &payments.Receipts{Sender: sender},
	}}

	err := d.Send(context.Background(), emailqueue.Job{
		Type: "receipt", To: "buyer@example.com", Subject: "Resit", Body: "…",
		SaleRef: "JF-TEST-1", PDFFilename: "resit.pdf", PDFData: []byte("%PDF-1.4"),
	})
	if err != nil {
		t.Fatalf("Send(receipt) err = %v, want nil — a suppressed receipt must not retry", err)
	}
	if !strings.Contains(logs.String(), "emailq_skip_suppressed") {
		t.Fatalf("receipt skip was not logged; log:\n%s", logs.String())
	}
}

// alwaysSuppressed is a Suppressor that reports every address as suppressed.
type alwaysSuppressed struct{}

func (alwaysSuppressed) IsSuppressed(_ context.Context, _ string) (bool, error) {
	return true, nil
}

// TestEmailDispatcherSuppressedMerchantNotifyReturnsNil: the merchant
// notification is no longer exempt, so its suppressed case is a handled skip.
func TestEmailDispatcherSuppressedMerchantNotifyReturnsNil(t *testing.T) {
	logs := capturingLogs(t)
	sender := email.NewWithSuppressor(email.SMTPConfig{From: "noreply@jomform.com"},
		&alwaysSuppressed{})
	d := &EmailDispatcher{Pay: &payments.Service{
		Receipts: &payments.Receipts{Sender: sender},
	}}

	err := d.Send(context.Background(), emailqueue.Job{
		Type: "merchant_notify", To: "merchant@example.com", Subject: "Jualan baharu",
		Body: "…", SaleRef: "JF-TEST-2",
	})
	if err != nil {
		t.Fatalf("Send(merchant_notify) err = %v, want nil", err)
	}
	if !strings.Contains(logs.String(), "emailq_skip_suppressed") {
		t.Fatalf("merchant skip was not logged; log:\n%s", logs.String())
	}
}

// TestSuppressionSkipOnlyMatchesTheSentinel pins the branch itself: only
// email.ErrSuppressed (wrapped or not) is treated as a skip.
func TestSuppressionSkipOnlyMatchesTheSentinel(t *testing.T) {
	capturingLogs(t)
	d := &EmailDispatcher{}
	job := emailqueue.Job{Type: "verify", To: "x@example.com"}

	if d.suppressionSkip(job, errors.New("ses throttled")) {
		t.Error("an ordinary send error was treated as a suppression skip")
	}
	if d.suppressionSkip(job, nil) {
		t.Error("nil was treated as a suppression skip")
	}
	if !d.suppressionSkip(job, email.ErrSuppressed) {
		t.Error("the sentinel was not recognised as a suppression skip")
	}
	if !d.suppressionSkip(job, &wrapErr{email.ErrSuppressed}) {
		t.Error("a wrapped sentinel was not recognised as a suppression skip")
	}
}

// wrapErr wraps an error the way fmt.Errorf %w would.
type wrapErr struct{ inner error }

func (w *wrapErr) Error() string { return "wrapped: " + w.inner.Error() }
func (w *wrapErr) Unwrap() error { return w.inner }
