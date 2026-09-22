// Typed-nil mailer guard: one behaviour test per file that the transactional-
// email language work and #672 rewrote on the same lines.
//
// WHY THIS FILE EXISTS
//
//	#672 fixed a process-killing SIGSEGV. cmd/jomform-api held `var mailer
//	*email.Sender` (nil when SES is unconfigured) and assigned it into the
//	VerifyMailer INTERFACE field. A nil *T inside an interface is not a nil
//	interface, so every downstream `field == nil` check was FALSE and the queued
//	verify email ran (*email.Sender).SendReceipt on a nil receiver — on the
//	email-queue worker goroutine, which recovers nothing. POST /v1/auth/register
//	answered 201 and the process then died.
//
//	The language work rewrites those same lines to add a `lang` parameter and
//	render through the shared emailtext dictionary. A rebase that resolves such
//	a file to one side drops the other, and resolving to the language side
//	leaves #672's guard DEFINED BUT UNCALLED — which `go build ./...` cannot see.
//
// So the guard is pinned by BEHAVIOUR, not by the symbol. A test asserting that
// `mailerUnavailable` still exists would have PASSED on exactly the broken
// resolution this file guards against: the function was still there, with zero
// callers. Every test below drives a real entry point with the production shape
// — a nil *T stored in the interface, never a bare nil — and requires that the
// mailer is not reached.
//
// Each test asserts its own fixture FIRST. A bare `nil` fixture is a nil
// INTERFACE, which the old `== nil` guard handled correctly, so it would pass
// against the broken code and prove nothing.
//
// Coverage of the six files that conflict:
//
//	auth_recovery.go, auth_ux.go, social.go      → TestUnconfiguredSESAuthHandlersDoNotCallTheMailer
//	rest_feedback.go                             → TestUnconfiguredSESFeedbackNotifyDoesNotCallTheMailer
//	rest_workspace_members.go                    → TestUnconfiguredSESInviteReportsNoEmailError
//	email/ses.go (the leaf nil-receiver backstop) → TestSenderNilReceiverReturnsErrorInsteadOfPanicking
//	                                               in unconfigured_ses_panic_test.go
//
// The two tests that follow the guard's happy path (a live mailer must still be
// called) are what keep it from being "fixed" by making it too broad: a guard
// that skips a configured deployment drops real mail with a green suite.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/email"
)

// detectorMailer is a VerifyMailer that DEREFERENCES its receiver, so being
// called through a nil pointer is a panic. Holding a nil *detectorMailer in the
// VerifyMailer field is the production shape twice over: it is a nil *T inside a
// non-nil interface (what defeats `== nil`), and it fails the moment a call is
// attempted on it (what the pre-#672 *email.Sender did before email/ses.go grew
// its nil-receiver backstop).
//
// It exists because a send on the send paths that return no error to a caller —
// a best-effort notification — is otherwise unobservable: the only difference
// between "skipped" and "attempted and failed" is the call itself.
type detectorMailer struct{ calls int }

func (m *detectorMailer) Send(to, subject, body, lang string) error {
	m.calls++
	return nil
}

// TestUnconfiguredSESAuthHandlersDoNotCallTheMailer drives every auth/invite
// send entry point whose typed-nil guard sits on a line the language work
// rewrites. With SES unconfigured each must stop at the guard and say so, rather
// than reach a nil receiver.
//
// The assertion is the guard's own error text, not merely "some error": a send
// attempted on the nil receiver returns email.ErrNotConfigured ("email sender
// not configured"), which is non-nil, so a test asserting only `err != nil`
// would pass on the broken tree.
func TestUnconfiguredSESAuthHandlersDoNotCallTheMailer(t *testing.T) {
	// The exact production wiring when SES is unconfigured: a typed nil.
	var sender *email.Sender
	a := &AuthHandlers{
		VerifyMailer: sender,
		// A 32-byte key so sendVerifyEmail gets past its key check and the
		// failure we see is the mailer guard, not a different branch.
		VerifyKey: make([]byte, 32),
		BaseURL:   "http://localhost",
	}

	if a.VerifyMailer == nil {
		t.Fatal("test fixture is wrong: a nil *email.Sender in the interface must NOT be a nil " +
			"interface — that is the whole bug, so if this is nil the test is vacuous")
	}

	ctx := context.Background()
	cases := []struct {
		name string // the file it lives in, so a failure names the file to grep
		call func() error
		want string // the guard's own message
	}{
		{"verify.go", func() error { return a.sendVerifyEmail(ctx, "u1", "user@example.test", "en") },
			"verify mailer not configured"},
		{"auth_ux.go", func() error { return a.sendPasswordResetEmail(ctx, "user@example.test", "tok", "en") },
			"reset mailer not configured"},
		{"auth_recovery.go/magic_link", func() error {
			return a.sendMagicLinkEmail(ctx, "user@example.test", "tok", false, "en")
		}, "magic link mailer not configured"},
		{"auth_recovery.go/email2fa", func() error {
			return a.sendEmail2FACode(ctx, "user@example.test", "123456", "en")
		}, "email code mailer not configured"},
		{"auth_recovery.go/2fa_active", func() error {
			return a.send2FAActiveEmail(ctx, "user@example.test", "en")
		}, "2fa active mailer not configured"},
		{"social.go", func() error { return a.sendConnectEmail(ctx, "user@example.test", "http://x/y", "en") },
			"connect mailer not configured"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.call()
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("%s with an unconfigured (typed-nil) mailer err = %v, want the %q guard — "+
					"anything else means the call did not stop at the typed-nil check", c.name, err, c.want)
			}
		})
	}
}

// TestUnconfiguredSESGuardStillSendsWithAConfiguredMailer is the other direction,
// and it is not decoration: the cheap way to make the tests above green is a
// guard that reports every mailer as unavailable, which silently stops all
// transactional mail from an unconfigured-LOOKING deployment that is in fact
// configured.
func TestUnconfiguredSESGuardStillSendsWithAConfiguredMailer(t *testing.T) {
	mailer := &recorderMailer{}
	a := &AuthHandlers{
		VerifyMailer:     mailer,
		VerifyKey:        make([]byte, 32),
		BaseURL:          "http://localhost",
		Users:            nil, // not reached
		Suppressions:     nil,
		WorkspaceMembers: nil,
		LoginLimiter:     nil,
	}

	if err := a.sendPasswordResetEmail(context.Background(), "user@example.test", "tok", "en"); err != nil {
		t.Fatalf("sendPasswordResetEmail with a configured mailer err = %v, want nil", err)
	}
	if !mailer.wasCalled() {
		t.Fatal("a configured mailer was never called — the guard is too broad and would silently " +
			"stop a working deployment from sending")
	}
	if got := mailer.lastLang; got != "en" {
		t.Fatalf("configured send lang = %q, want %q — the guard must not disturb the language argument",
			got, "en")
	}
}

// TestUnconfiguredSESFeedbackNotifyDoesNotCallTheMailer pins the guard inside
// rest_feedback.go. A feedback notification returns no error to a caller (it is
// best-effort), so the only observable difference between "skipped by the guard"
// and "attempted on a nil receiver" is whether the send happens at all — which
// the detector mailer turns into a panic.
func TestUnconfiguredSESFeedbackNotifyDoesNotCallTheMailer(t *testing.T) {
	var nilMailer *detectorMailer // the typed-nil shape
	h := &RESTHandlers{Mailer: nilMailer}

	if h.Mailer == nil {
		t.Fatal("test fixture is wrong: a nil *detectorMailer in the interface must NOT be a nil " +
			"interface — a nil interface would not exercise the typed-nil path")
	}

	attempted := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				attempted = true
			}
		}()
		h.sendFeedbackMail("feedback@example.test", "Subjek", "Badan", "en")
	}()
	if attempted {
		t.Fatal("sendFeedbackMail reached the mailer on an unconfigured deployment — " +
			"the typed-nil guard is missing from rest_feedback.go")
	}
}

// TestUnconfiguredSESFeedbackNotifyStillSendsWithAConfiguredMailer keeps the
// feedback path's guard honest in the other direction: a wired mailer is used.
func TestUnconfiguredSESFeedbackNotifyStillSendsWithAConfiguredMailer(t *testing.T) {
	mailer := &detectorMailer{}
	h := &RESTHandlers{Mailer: mailer}

	h.sendFeedbackMail("feedback@example.test", "Subjek", "Badan", "en")

	if mailer.calls != 1 {
		t.Fatalf("sendFeedbackMail calls on a configured mailer = %d, want 1", mailer.calls)
	}

	// An empty recipient is the other leg of the same method: nothing is sent
	// and nothing is enqueued, configured mailer or not.
	h.sendFeedbackMail("", "Subjek", "Badan", "en")
	if mailer.calls != 1 {
		t.Fatalf("sendFeedbackMail with an empty recipient made %d call(s), want the original 1 — "+
			"an empty To must return before any send", mailer.calls)
	}
}

// TestUnconfiguredSESInviteReportsNoEmailError pins the guard inside
// rest_workspace_members.go through the real endpoint. The invite ROW is
// recorded either way and the response is always 201, so the observable
// difference is the `email_error` key: with the guard the unavailable mailer is
// skipped exactly like the nil-interface case was, and without it the send is
// attempted on the nil receiver and its failure is reported on the invite.
//
// Skipped unless JOMFORM_TEST_DATABASE_URL + JOMFORM_TEST_VALKEY_URL are set,
// like the neighbouring RBAC/invite tests.
func TestUnconfiguredSESInviteReportsNoEmailError(t *testing.T) {
	pool := bundleTestPool(t)
	ctx := context.Background()
	h := rbacHandlers(t, pool)
	h.EmailQueue = nil // synchronous path: Mailer is the send path

	var sender *email.Sender // SES unconfigured
	h.Mailer = sender
	if h.Mailer == nil {
		t.Fatal("test fixture is wrong: a nil *email.Sender in the interface must NOT be a nil " +
			"interface — that is the whole bug, so if this is nil the test is vacuous")
	}

	wsID, wsSlug, memberID, _ := rbacFixture(t, ctx, pool, dbstore.RoleAdmin)
	token := rbacSessionCookie(t, h, memberID)

	prev := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	t.Cleanup(func() { slog.SetDefault(prev) })

	// A NEW address, so the invite takes the pending-invite + email branch
	// rather than the "existing user → added as a member" one, which sends
	// nothing.
	invitee := "invite-unconfigured-" + randSuffix() + "@example.com"
	rec := rbacDo(t, h, http.MethodPost,
		"/v1/workspaces/"+wsID+"/invites?workspace="+wsSlug, token,
		map[string]any{"email": invitee, "role": dbstore.RoleReadOnly})

	if rec.Code != http.StatusCreated {
		t.Fatalf("invite status = %d, want 201 (the invite row is recorded regardless of the mailer); body=%s",
			rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "email_error") {
		t.Fatalf("invite body = %s, want NO email_error — with an unconfigured mailer the send must be "+
			"skipped by the typed-nil guard, never attempted on the nil receiver", rec.Body.String())
	}
}
