// Pre-send suppression choke point tests.
//
// These cover the guard added to (*email.Sender).SendReceipt /
// SendReceiptWithAttachment — the two and only sesv2.SendEmail calls in the
// repo, and therefore the single point every outbound message passes through.
//
// The oracle for "was the message handed to SES?" is a fake SESv2 endpoint
// (httptest) reached by pointing AWS_ENDPOINT_URL_SESV2 at it: the SDK reads
// that variable when it builds the client, so a counted HTTP request proves the
// send left this package, and zero requests proves it did not. That is a real
// observation of the send, not a mock of the guard.
package email

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeSES starts a local SESv2 endpoint that accepts every SendEmail and counts
// the requests. It is wired into the SDK through AWS_ENDPOINT_URL_SESV2.
func fakeSES(t *testing.T) *atomic.Int64 {
	t.Helper()
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"MessageId":"fake-ses-message-id"}`))
	}))
	t.Cleanup(srv.Close)
	// The SDK's resolveBaseEndpoint honours this (see aws-sdk-go-v2/service/
	// sesv2/endpoints.go), so the client built in client() talks to the fake.
	t.Setenv("AWS_ENDPOINT_URL_SESV2", srv.URL)
	return &calls
}

// testSender builds a Sender that will talk to the fake SES endpoint.
func testSender(s Suppressor) *Sender {
	return NewWithSuppressor(SMTPConfig{
		From: "noreply@jomform.com", Region: "ap-southeast-1",
		// Static credentials so the client does not try the instance metadata
		// service (which would hang/slow the test).
		Username: "AKIAEXAMPLE", Password: "secretexample",
	}, s)
}

// staticSuppressor answers IsSuppressed from a fixed verdict.
type staticSuppressor struct {
	suppressed bool
	err        error
	calls      int
	lastAsk    string
}

func (s *staticSuppressor) IsSuppressed(_ context.Context, email string) (bool, error) {
	s.calls++
	s.lastAsk = email
	return s.suppressed, s.err
}

// TestSendReceiptSkipsSuppressedRecipient: a suppressed address is never handed
// to SES, and the caller gets the non-fatal ErrSuppressed sentinel.
func TestSendReceiptSkipsSuppressedRecipient(t *testing.T) {
	calls := fakeSES(t)
	sup := &staticSuppressor{suppressed: true}
	s := testSender(sup)

	id, err := s.SendReceipt("buyer@example.com", "Resit pembayaran", "Badan", "")
	if !errors.Is(err, ErrSuppressed) {
		t.Fatalf("SendReceipt err = %v, want ErrSuppressed", err)
	}
	if id != "" {
		t.Errorf("MessageID = %q, want empty for a skipped send", id)
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("SES received %d requests, want 0 — a suppressed address must never reach SES", n)
	}
	if sup.calls != 1 {
		t.Errorf("suppression lookups = %d, want 1", sup.calls)
	}
}

// TestSendReceiptWithAttachmentSkipsSuppressedRecipient: the attachment path is
// the second sesv2.SendEmail call and must be guarded identically.
func TestSendReceiptWithAttachmentSkipsSuppressedRecipient(t *testing.T) {
	calls := fakeSES(t)
	s := testSender(&staticSuppressor{suppressed: true})

	id, err := s.SendReceiptWithAttachment("buyer@example.com", "Resit", "Badan", "",
		"resit.pdf", []byte("%PDF-1.4"))
	if !errors.Is(err, ErrSuppressed) {
		t.Fatalf("SendReceiptWithAttachment err = %v, want ErrSuppressed", err)
	}
	if id != "" {
		t.Errorf("MessageID = %q, want empty for a skipped send", id)
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("SES received %d requests, want 0 (attachment path)", n)
	}
}

// TestSendReceiptUnsuppressedStillSends is the regression guard: the choke point
// must not block ordinary mail.
func TestSendReceiptUnsuppressedStillSends(t *testing.T) {
	calls := fakeSES(t)
	sup := &staticSuppressor{suppressed: false}
	s := testSender(sup)

	id, err := s.SendReceipt("buyer@example.com", "Resit pembayaran", "Badan", "")
	if err != nil {
		t.Fatalf("SendReceipt err = %v, want nil", err)
	}
	if id != "fake-ses-message-id" {
		t.Errorf("MessageID = %q, want the SES message id", id)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("SES received %d requests, want 1", n)
	}
	if sup.calls != 1 {
		t.Errorf("suppression lookups = %d, want 1", sup.calls)
	}
}

// TestSendReceiptWithAttachmentUnsuppressedStillSends mirrors the above for the
// raw-MIME path.
func TestSendReceiptWithAttachmentUnsuppressedStillSends(t *testing.T) {
	calls := fakeSES(t)
	s := testSender(&staticSuppressor{suppressed: false})

	if _, err := s.SendReceiptWithAttachment("buyer@example.com", "Resit", "Badan", "",
		"resit.pdf", []byte("%PDF-1.4")); err != nil {
		t.Fatalf("SendReceiptWithAttachment err = %v, want nil", err)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("SES received %d requests, want 1", n)
	}
}

// TestSendReceiptSuppressionLookupIsNormalised: the guard asks about the
// lower-cased, trimmed address, so " Buyer@Example.COM " and
// "buyer@example.com" are one address on the list.
func TestSendReceiptSuppressionLookupIsNormalised(t *testing.T) {
	calls := fakeSES(t)
	sup := &staticSuppressor{suppressed: true}
	s := testSender(sup)

	if _, err := s.SendReceipt("  Buyer@Example.COM  ", "Resit", "Badan", ""); !errors.Is(err, ErrSuppressed) {
		t.Fatalf("err = %v, want ErrSuppressed for a mixed-case + padded address", err)
	}
	if sup.lastAsk != "buyer@example.com" {
		t.Fatalf("lookup asked about %q, want the normalised address", sup.lastAsk)
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("SES received %d requests, want 0", n)
	}
}

// TestSendReceiptNilSuppressorSendsWithNoLookup: a Sender built without a
// suppression store (bare-mode tests, SES configured without a database) must
// behave exactly as before the choke point existed.
func TestSendReceiptNilSuppressorSendsWithNoLookup(t *testing.T) {
	calls := fakeSES(t)
	s := testSender(nil)
	if s.Suppressor != nil {
		t.Fatal("test setup: expected a nil Suppressor")
	}
	if _, err := s.SendReceipt("buyer@example.com", "Resit", "Badan", ""); err != nil {
		t.Fatalf("SendReceipt err = %v, want nil", err)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("SES received %d requests, want 1", n)
	}
}

// TestSendReceiptSuppressionStoreErrorFailsOpen: an infrastructure error in the
// lookup must NOT stop revenue-critical mail, and must be logged loudly.
func TestSendReceiptSuppressionStoreErrorFailsOpen(t *testing.T) {
	calls := fakeSES(t)
	sup := &staticSuppressor{err: errors.New("connection refused")}

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	s := testSender(sup)
	if _, err := s.SendReceipt("buyer@example.com", "Resit", "Badan", ""); err != nil {
		t.Fatalf("SendReceipt err = %v, want nil (fail open on a store error)", err)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("SES received %d requests, want 1 (fail open must still send)", n)
	}
	logged := buf.String()
	if !strings.Contains(logged, "email_suppression_check_error") {
		t.Fatalf("store error was not logged as email_suppression_check_error; log:\n%s", logged)
	}
	if !strings.Contains(logged, "connection refused") {
		t.Fatalf("the underlying error was swallowed; log:\n%s", logged)
	}
}

// TestSendReceiptSuppressedSkipIsLogged: the skip is an audit trail — one
// structured line naming the recipient (obfuscated) and the subject — so an
// operator can always tell why a message never left.
func TestSendReceiptSuppressedSkipIsLogged(t *testing.T) {
	fakeSES(t)

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	s := testSender(&staticSuppressor{suppressed: true})
	_, _ = s.SendReceipt("wan@jomform.com", "Resit pembayaran", "Badan", "")

	logged := buf.String()
	if !strings.Contains(logged, "email_suppressed_skip") {
		t.Fatalf("skip was not logged as email_suppressed_skip; log:\n%s", logged)
	}
	if !strings.Contains(logged, "w***@jomform.com") {
		t.Fatalf("skip log must name the recipient, obfuscated; log:\n%s", logged)
	}
	if !strings.Contains(logged, "Resit pembayaran") {
		t.Fatalf("skip log must carry the subject; log:\n%s", logged)
	}
	// The full local part must not be written to the log.
	if strings.Contains(logged, "wan@jomform.com") {
		t.Fatalf("skip log leaked the full address; log:\n%s", logged)
	}
}

// TestObfuscateMasksLocalPart pins the log-safety helper the skip line uses.
func TestObfuscateMasksLocalPart(t *testing.T) {
	cases := map[string]string{
		"wan@jomform.com":        "w***@jomform.com",
		"owner00@x.test":   "own***@x.test",
		"ab@x.test":              "a***@x.test",
		"a@x.test":               "***",
		"no-at-sign":             "***",
		"  spaced@jomform.com  ": "spa***@jomform.com",
	}
	for in, want := range cases {
		if got := obfuscate(in); got != want {
			t.Errorf("obfuscate(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSendDelegatesThroughTheGuardedPath: Send/SendWithAttachment are thin
// wrappers over the two guarded methods, so they inherit the guard. Pinned
// explicitly because a future edit could re-implement them against the SDK and
// silently bypass the choke point.
func TestSendDelegatesThroughTheGuardedPath(t *testing.T) {
	calls := fakeSES(t)
	s := testSender(&staticSuppressor{suppressed: true})

	if err := s.Send("buyer@example.com", "Resit", "Badan", ""); !errors.Is(err, ErrSuppressed) {
		t.Fatalf("Send err = %v, want ErrSuppressed", err)
	}
	if err := s.SendWithAttachment("buyer@example.com", "Resit", "Badan", "",
		"resit.pdf", []byte("%PDF-1.4")); !errors.Is(err, ErrSuppressed) {
		t.Fatalf("SendWithAttachment err = %v, want ErrSuppressed", err)
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("SES received %d requests, want 0 via Send/SendWithAttachment", n)
	}
}
