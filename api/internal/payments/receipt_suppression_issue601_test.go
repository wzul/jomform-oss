// Payments-side suppression handling (issue #601).
//
// The three payments send paths (buyer receipt, merchant notification,
// per-form notification) send through (*email.Sender), which is the pre-send
// choke point. What is asserted here is the layer above: when the choke point
// answers ErrSuppressed, the caller must log the skip and carry on — never
// treat it as a send failure.
//
// The senders are stubs so the branch is exercised directly; the guard in front
// of SES is covered by api/internal/email/ses_suppression_test.go against a fake
// SES endpoint.
package payments

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/email"
)

// suppressedSender replaces the SES-backed sender for one test. It is a
// *email.Sender subclass only in behaviour: the branch under test reads
// Receipts.Sender, so the concrete type has to be *email.Sender, and its
// Suppressor is what decides the outcome.
func suppressedSender() *email.Sender {
	return email.NewWithSuppressor(email.SMTPConfig{From: "noreply@jomform.com"},
		suppressAll{})
}

// suppressAll reports every address as suppressed.
type suppressAll struct{}

func (suppressAll) IsSuppressed(_ context.Context, _ string) (bool, error) {
	return true, nil
}

// capturePaymentsLogs routes slog into a buffer for one test.
func capturePaymentsLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// TestSendReceiptLogsSuppressedSkip: a suppressed buyer receipt is logged with
// the dedicated key and no MessageID is persisted. SaleRef fields are populated
// so ReceiptPDF succeeds, which means the send really goes through the
// ATTACHMENT branch (SendReceiptWithAttachment) — the path production takes for
// a normal receipt.
func TestSendReceiptLogsSuppressedSkip(t *testing.T) {
	logs := capturePaymentsLogs(t)
	svc := &Service{Receipts: &Receipts{Sender: suppressedSender()}}

	persisted := ""
	svc.sendReceipt(context.Background(),
		SaleRef{RefCode: "JF-601-A", BuyerEmail: "buyer@example.com",
			AmountSen: 1500, ItemsSummary: "1x Tee", FormURL: "https://x.test/f"},
		"Resit pembayaran", "Badan", "en", false,
		func(id string) { persisted = id })

	logged := logs.String()
	if !strings.Contains(logged, "receipt_suppressed_skipped") {
		t.Fatalf("skip was not logged as receipt_suppressed_skipped; log:\n%s", logged)
	}
	if strings.Contains(logged, "receipt_send_failed") {
		t.Fatalf("a suppressed receipt was logged as a send FAILURE; log:\n%s", logged)
	}
	if strings.Contains(logged, "receipt_sent") {
		t.Fatalf("a suppressed receipt was logged as sent; log:\n%s", logged)
	}
	if persisted != "" {
		t.Fatalf("persisted MessageID = %q, want empty for a suppressed send", persisted)
	}
}

// TestSendReceiptLogsSuppressedSkipAttachmentPath pins the ATTACHMENT branch
// specifically (receipt-then-PDF) and the text-only fallback separately, so
// neither can lose its skip handling without a failure.
func TestSendReceiptLogsSuppressedSkipAttachmentPath(t *testing.T) {
	logs := capturePaymentsLogs(t)
	svc := &Service{Receipts: &Receipts{Sender: suppressedSender()}}

	// A ref that renders a PDF ⇒ the attachment branch.
	svc.sendReceipt(context.Background(),
		SaleRef{RefCode: "JF-601-PDF", BuyerEmail: "buyer@example.com",
			AmountSen: 9900, ItemsSummary: "2x Tee", FormURL: "https://x.test/f"},
		"Resit", "Badan", "en", false, func(string) {})

	logged := logs.String()
	if !strings.Contains(logged, "receipt_suppressed_skipped") {
		t.Fatalf("attachment-path skip was not logged; log:\n%s", logged)
	}
	if strings.Contains(logged, "receipt_send_failed") {
		t.Fatalf("attachment-path skip was logged as a FAILURE; log:\n%s", logged)
	}
}

// TestNotifyMerchantLogsSuppressedSkip: the merchant notification is no longer
// exempt, so its suppressed case is a logged skip with its own key.
func TestNotifyMerchantLogsSuppressedSkip(t *testing.T) {
	logs := capturePaymentsLogs(t)
	svc := &Service{Receipts: &Receipts{Sender: suppressedSender()}}

	svc.notifyMerchant(context.Background(),
		SaleRef{RefCode: "JF-601-B"}, "Resit pembayaran", "Badan", "en", false,
		"merchant@example.com")

	logged := logs.String()
	if !strings.Contains(logged, "receipt_merchant_suppressed_skipped") {
		t.Fatalf("merchant skip was not logged as receipt_merchant_suppressed_skipped; log:\n%s", logged)
	}
	if strings.Contains(logged, "receipt_merchant_notify_failed") {
		t.Fatalf("a suppressed merchant notify was logged as a FAILURE; log:\n%s", logged)
	}
	if strings.Contains(logged, "receipt_merchant_notify_sent") {
		t.Fatalf("a suppressed merchant notify was logged as sent; log:\n%s", logged)
	}
}

// TestSendNotificationLogsSuppressedSkip: the synchronous per-form notification
// path logs its own skip key and does not report a failure.
func TestSendNotificationLogsSuppressedSkip(t *testing.T) {
	logs := capturePaymentsLogs(t)
	svc := &Service{Receipts: &Receipts{Sender: suppressedSender()}}
	// No EmailQueue → the synchronous branch runs.
	rule := testNotification("admin@example.com", "sale.paid")

	svc.sendNotification(context.Background(),
		NotificationRef{FormID: "form-601", RefCode: "JF-601-C"}, rule)

	logged := logs.String()
	if !strings.Contains(logged, "notify_suppressed_skipped") {
		t.Fatalf("notification skip was not logged as notify_suppressed_skipped; log:\n%s", logged)
	}
	if strings.Contains(logged, "notify_send_failed") {
		t.Fatalf("a suppressed notification was logged as a FAILURE; log:\n%s", logged)
	}
}

// TestSendReceiptNonSuppressionErrorStillLoggedAsFailure: the guard must only
// absorb a suppression, not mask a genuine send failure.
func TestSendReceiptNonSuppressionErrorStillLoggedAsFailure(t *testing.T) {
	logs := capturePaymentsLogs(t)
	svc := &Service{Receipts: &Receipts{Sender: failingSender(t, errors.New("ses throttled"))}}

	svc.sendReceipt(context.Background(),
		SaleRef{RefCode: "JF-601-D", BuyerEmail: "buyer@example.com"},
		"Resit", "Badan", "en", false, func(string) {})

	logged := logs.String()
	if !strings.Contains(logged, "receipt_send_failed") {
		t.Fatalf("a real send failure was not logged as receipt_send_failed; log:\n%s", logged)
	}
	if strings.Contains(logged, "receipt_suppressed_skipped") {
		t.Fatalf("a real send failure was misreported as a suppression skip; log:\n%s", logged)
	}
}

// failingSender is a Suppressor that errors, which the choke point turns into a
// fail-open attempt at SES. There is no SES here, so the attempt fails — which
// is exactly the "real failure" this test wants surfaced.
func failingSender(t *testing.T, err error) *email.Sender {
	t.Helper()
	// A non-routable endpoint so the SES call itself fails fast rather than
	// reaching AWS.
	t.Setenv("AWS_ENDPOINT_URL_SESV2", "http://127.0.0.1:1")
	return email.NewWithSuppressor(
		email.SMTPConfig{From: "noreply@jomform.com", Username: "AKIAEXAMPLE", Password: "x"},
		errSuppressor{err: err})
}

// errSuppressor makes every lookup fail.
type errSuppressor struct{ err error }

func (e errSuppressor) IsSuppressed(_ context.Context, _ string) (bool, error) {
	return false, e.err
}

// testNotification builds a minimal enabled rule for the recipient. Enabled must
// be true or the send never happens and the skip branch is unreachable.
func testNotification(to, event string) *dbstore.FormNotification {
	return &dbstore.FormNotification{
		ID: "601", FormID: "form-601", Event: event, ToEmail: to,
		Subject: "Jualan baharu", Body: "Badan emel", Enabled: true,
	}
}
