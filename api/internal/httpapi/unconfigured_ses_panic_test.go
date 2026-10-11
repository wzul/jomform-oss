// Regression test for the unconfigured-SES process kill.
//
// What was reported, and reproduced against the built binary: with SES_SMTP_HOST
// unset, POST /v1/auth/register returned 201 and the queued verify email then
// ran (*email.Sender).SendReceipt on a NIL receiver — the panic was on the
// email queue worker goroutine, which recovers nothing, so the whole
// jomform-api process died and every later request got connection-refused.
//
// The mechanism is Go's typed nil: cmd/jomform-api held `var mailer
// *email.Sender` (nil when SES is unconfigured) and assigned it into the
// VerifyMailer INTERFACE field. A nil *T inside an interface is not a nil
// interface, so `d.AuthMailer == nil` was false and the guard's intent ("nil →
// those job types are dropped") never ran for the auth branch.
//
// The test below pins the contract for BOTH shapes of "no mailer", on the ASYNC
// path (the dispatcher, where the reported panic ran) and on the SYNC path (the
// auth handlers, which had the identical typed-nil problem), plus the
// nil-receiver backstop in the email package that keeps a future wiring mistake
// from taking the process down again.
package httpapi

import (
	"context"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/email"
	"github.com/wzul/jomform/api/internal/emailqueue"
)

// authJobTypes is every type (*EmailDispatcher).Send routes to sendAuth. The
// list is duplicated from the switch's case label on purpose: this test is about
// the nil-mailer branch, not about routing (email_dispatcher_jobtype_test.go
// owns the routing contract, and fails if the two drift).
var authJobTypes = []string{"verify", "magic_link", "password_reset", "email2fa", "2fa_active", "invite", "connect"}

// TestEmailDispatcherTypedNilAuthMailerIsDropped: the reported crash shape. A
// nil *email.Sender assigned into the AuthMailer interface must be treated
// exactly like a nil interface (job dropped + logged), NOT called.
//
// Pre-fix this test panicked with a SIGSEGV from the nil receiver, i.e. the test
// binary itself died, which is what the production process did.
func TestEmailDispatcherTypedNilAuthMailerIsDropped(t *testing.T) {
	for _, jobType := range authJobTypes {
		t.Run(jobType, func(t *testing.T) {
			logs := capturingLogs(t)
			// The exact production wiring when SES is unconfigured: a typed
			// nil *email.Sender inside the interface.
			var sender *email.Sender
			d := &EmailDispatcher{AuthMailer: sender}

			if !mailerUnavailable(d.AuthMailer) {
				t.Fatalf("mailerUnavailable(nil *email.Sender) = false, want true — " +
					"the guard must unwrap a typed nil or the call runs on a nil receiver")
			}

			err := d.Send(context.Background(), emailqueue.Job{
				Type: jobType, To: "user@example.test", Subject: "S", Body: "B",
			})
			if err != nil {
				t.Fatalf("Send(%s) with an unconfigured mailer err = %v, want nil "+
					"(the job must be dropped, not retried)", jobType, err)
			}
			if !strings.Contains(logs.String(), "emailq_auth_no_mailer") {
				t.Fatalf("the dropped %s job was not logged as emailq_auth_no_mailer; log:\n%s",
					jobType, logs.String())
			}
		})
	}
}

// TestEmailDispatcherNilInterfaceAuthMailerStillDropped: the plain nil interface
// keeps working — the fix must not move the guard off the documented behaviour.
func TestEmailDispatcherNilInterfaceAuthMailerStillDropped(t *testing.T) {
	logs := capturingLogs(t)
	d := &EmailDispatcher{}

	if err := d.Send(context.Background(), emailqueue.Job{
		Type: "verify", To: "user@example.test", Subject: "S", Body: "B",
	}); err != nil {
		t.Fatalf("Send(verify) without a mailer err = %v, want nil", err)
	}
	if !strings.Contains(logs.String(), "emailq_auth_no_mailer") {
		t.Fatalf("a missing mailer was not logged; log:\n%s", logs.String())
	}
}

// TestEmailDispatcherConfiguredMailerStillSends: the guard must not swallow a
// live mailer. A non-nil mailer is called exactly once, and never reported as
// "no mailer" — that is the failure mode a too-broad nil check would introduce
// (verification mail silently going nowhere with a green suite).
func TestEmailDispatcherConfiguredMailerStillSends(t *testing.T) {
	logs := capturingLogs(t)
	mailer := &recorderMailer{}
	d := &EmailDispatcher{AuthMailer: mailer}

	if err := d.Send(context.Background(), emailqueue.Job{
		Type: "verify", To: "user@example.test", Subject: "S", Body: "B",
	}); err != nil {
		t.Fatalf("Send(verify) err = %v, want nil", err)
	}
	if !mailer.wasCalled() {
		t.Fatal("a configured mailer was never called — verification mail would silently vanish")
	}
	if strings.Contains(logs.String(), "emailq_auth_no_mailer") {
		t.Fatalf("a configured mailer was reported as missing; log:\n%s", logs.String())
	}
}

// TestSenderNilReceiverReturnsErrorInsteadOfPanicking: the last-resort backstop
// in the email package. Even if some future wiring assigns a nil *email.Sender
// into an interface again, a send must return an error rather than dereference
// the receiver — the panic ran on the queue worker goroutine and the process
// could not survive it.
func TestSenderNilReceiverReturnsErrorInsteadOfPanicking(t *testing.T) {
	var s *email.Sender
	// The language the branded HTML part declares. Its value is irrelevant to
	// this test: the point is that the nil receiver is reported before any of
	// the arguments is looked at, so no send is attempted at all.
	const lang = "en"

	if err := s.Send("user@example.test", "S", "B", lang); err == nil {
		t.Fatal("Send on a nil *email.Sender returned nil, want an error — " +
			"a silent nil would hide an unconfigured deployment")
	}
	if _, err := s.SendReceipt("user@example.test", "S", "B", lang); err == nil {
		t.Fatal("SendReceipt on a nil *email.Sender returned nil, want an error")
	}
	if err := s.SendWithAttachment("user@example.test", "S", "B", lang, "f.pdf", []byte("x")); err == nil {
		t.Fatal("SendWithAttachment on a nil *email.Sender returned nil, want an error")
	}
	if _, err := s.SendReceiptWithAttachment("user@example.test", "S", "B", lang, "f.pdf", []byte("x")); err == nil {
		t.Fatal("SendReceiptWithAttachment on a nil *email.Sender returned nil, want an error")
	}
}

// TestAuthHandlersTypedNilMailerReportsNotConfigured: the SYNCHRONOUS path. When
// no EmailQueue is wired the auth handlers send inline, and they guarded with
// the same typed-nil-defeated `a.VerifyMailer == nil` the dispatcher used. Every
// one of these must answer "not configured" instead of calling into a nil
// receiver.
func TestAuthHandlersTypedNilMailerReportsNotConfigured(t *testing.T) {
	var sender *email.Sender // SES unconfigured
	ctx := context.Background()

	// A 32-byte key so sendVerifyEmail gets PAST its key check and actually
	// reaches the mailer guard — assert the guard's own error text, not merely
	// "some error", or the test would pass on the wrong branch.
	key := make([]byte, 32)

	a := &AuthHandlers{VerifyMailer: sender, VerifyKey: key, BaseURL: "http://localhost"}

	verifyErr := a.sendVerifyEmail(ctx, "u1", "user@example.test", "en")
	if verifyErr == nil || !strings.Contains(verifyErr.Error(), "verify mailer not configured") {
		t.Fatalf("sendVerifyEmail with a typed-nil mailer err = %v, want the "+
			"\"verify mailer not configured\" guard (a nil receiver would instead "+
			"have panicked the process)", verifyErr)
	}

	if err := a.sendPasswordResetEmail(ctx, "user@example.test", "tok", "en"); err == nil {
		t.Fatal("sendPasswordResetEmail with a typed-nil mailer returned nil, want an error")
	}
	if err := a.sendConnectEmail(ctx, "user@example.test", "http://x/y", "en"); err == nil {
		t.Fatal("sendConnectEmail with a typed-nil mailer returned nil, want an error")
	}
	if err := a.sendMagicLinkEmail(ctx, "user@example.test", "tok", false, "en", ""); err == nil {
		t.Fatal("sendMagicLinkEmail with a typed-nil mailer returned nil, want an error")
	}
	if err := a.sendEmail2FACode(ctx, "user@example.test", "123456", "en"); err == nil {
		t.Fatal("sendEmail2FACode with a typed-nil mailer returned nil, want an error")
	}
	if err := a.send2FAActiveEmail(ctx, "user@example.test", "en"); err == nil {
		t.Fatal("send2FAActiveEmail with a typed-nil mailer returned nil, want an error")
	}
}
