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
// The table above covers the guard SITES that a send function owns. One site is
// shaped differently and needed its own test, because no send call can reach it:
// the OUTER guard on the magic-link request (auth_recovery.go:233,
// `a.Sessions == nil || mailerUnavailable(a.VerifyMailer)`) is the only combined
// one in the package, and the handler it guards is the only entry point that
// needs a live Sessions store AND a typed-nil mailer at once. See
// TestUnconfiguredSESMagicLinkRequestStopsAtTheOuterGuard.
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

	"github.com/wzul/jomform/api/internal/auth"
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

// TestUnconfiguredSESMagicLinkRequestStopsAtTheOuterGuard pins the OUTER guard on
// handleMagicLinkRequest (auth_recovery.go:233), the one combined site:
//
//	if a.Sessions == nil || mailerUnavailable(a.VerifyMailer) {
//		a.replyMagicLinkRequestOK(w) // nothing wired (bare test) — generic ok
//		return
//	}
//
// WHY IT NEEDS ITS OWN TEST. Every other mailerUnavailable site sits inside a
// send function, so a test that calls the sender reaches it. This one guards a
// REQUEST HANDLER, and the handler is defended twice: the inner guard in
// sendMagicLinkEmail (auth_recovery.go:292) answers the same request with
// "magic link mailer not configured". So reverting the outer guard to the
// pre-#672 `a.VerifyMailer == nil` still compiles AND leaves the whole package
// green — nothing else in it ever asks this handler for a typed-nil mailer, and
// the response is a generic 200 on BOTH sides. That is why asserting "the
// response is 200" would prove nothing here: the status cannot tell the two
// trees apart.
//
// WHAT THE HOLE ACTUALLY COSTS, and therefore what this test reads instead:
//
//	(a) a magic-link token is minted for a deployment whose SES is
//	    unconfigured. The guard's stated intent is to stop BEFORE touching the
//	    session store at all, so with the guard gone the existing-verified-user
//	    branch (:239) calls a.Sessions.MagicLink().Generate(...) and writes a
//	    needless token row that expires unused 15 minutes later;
//	(b) the send that follows fails, so the handler logs
//	    magiclink_email_send_failed and still replies 200.
//
// Both are observable, so both are asserted. Neither is a panic or a wrong
// status, which is exactly why the mutation survived: there is nothing louder
// to notice.
//
// THE FIXTURE NEEDS BOTH HALVES: a LIVE Sessions store and a typed-nil
// *email.Sender. A bare nil mailer is a nil INTERFACE — the pre-#672 `== nil`
// check handled that correctly, so the test would pass against the broken code
// — and a nil Sessions makes the guard's FIRST clause fire, which proves nothing
// about the mailer clause this test exists for.
//
// The fixture is also the PRODUCTION shape in the other direction: `makeAuthMailer`
// in cmd/jomform-api/main.go:130 returns a nil INTERFACE when SES is absent, and
// this test's typed nil is the shape that reached the interfaces before #672 —
// keeping it here is what makes the guard hold for either wiring.
func TestUnconfiguredSESMagicLinkRequestStopsAtTheOuterGuard(t *testing.T) {
	sess := authTestValkey(t) // live store: the guard's first clause must not be the one that fires
	users := authUsers(t)
	ctx := context.Background()

	// The exact production wiring when SES is unconfigured: a typed nil.
	var sender *email.Sender
	h := &AuthHandlers{
		Sessions:     sess,
		Users:        users,
		VerifyKey:    make([]byte, 32),
		BaseURL:      "http://localhost",
		VerifyMailer: sender,
	}
	if h.VerifyMailer == nil {
		t.Fatal("test fixture is wrong: a nil *email.Sender in the interface must NOT be a nil " +
			"interface — that is the whole bug, so if this is nil the test is vacuous")
	}
	if h.Sessions == nil {
		t.Fatal("test fixture is wrong: Sessions must be live, or the guard's first clause fires and " +
			"the test proves nothing about the mailer clause")
	}

	// A real VERIFIED user, so the handler takes the existing-verified-user
	// branch (:239) — the one that mints a token. An unknown email would take
	// the signup branch (:267) and reveal the same hole by a different call,
	// but that branch also requires a valid email and is not the case the card
	// measured.
	email := "mgouter-" + randSuffix() + "@example.test"
	if _, err := users.Create(ctx, email, "unused-in-this-test"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	user, err := users.ByEmail(ctx, email)
	if err != nil || user == nil {
		t.Fatalf("load the account just created: err=%v user=%v", err, user)
	}
	if err := users.SetVerified(ctx, user.ID); err != nil {
		t.Fatalf("verify the account: %v", err)
	}

	// Tokens already stored for THIS user. The test database is shared with
	// every concurrent run, so the count is scoped by the token's stored VALUE
	// (this user's id) rather than by the jf:magic: prefix alone — a prefix-wide
	// count would also see a peer run's tokens and could fail the correct code.
	own := func() []string { return magicLinkKeysFor(t, sess, ctx, user.ID) }
	if before := own(); len(before) != 0 {
		t.Fatalf("fixture is wrong: %d magic-link token(s) already stored for the fresh user %s (%v)",
			len(before), user.ID, before)
	}
	t.Cleanup(func() {
		if keys := magicLinkKeysFor(t, sess, ctx, user.ID); len(keys) > 0 {
			if err := sess.Client().Del(ctx, keys...).Err(); err != nil {
				t.Errorf("cleanup: delete own magic-link keys: %v", err)
			}
		}
	})

	logs := captureSlog(t)

	// Drive the REAL registered route (auth.go:100), not the bare method, so the
	// request path is production-shaped. The route carries a 1/hour per-IP
	// limiter; a fresh mux has a fresh bucket and a unique IP is used, so this
	// single request is not throttled (and -count runs stay independent).
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	rec := postJSON(mux, "/v1/auth/magic-link/request",
		map[string]any{"email": email}, uniqueTestIP())

	// The status is the SAME on both trees — asserted only so that a change to
	// the generic reply is noticed here, never as the guard's evidence.
	if rec.Code != http.StatusOK {
		t.Fatalf("magic-link request status = %d, want the generic 200; body=%s",
			rec.Code, rec.Body.String())
	}

	if keys := own(); len(keys) != 0 {
		t.Fatalf("the magic-link request minted %d token(s) (%v) for a deployment whose SES is "+
			"unconfigured — the OUTER guard at auth_recovery.go:233 "+
			"(`a.Sessions == nil || mailerUnavailable(a.VerifyMailer)`) must return BEFORE the session "+
			"store is touched. A token was written, which means the guard did not stop at the typed-nil "+
			"mailer (e.g. it was reverted to the pre-#672 `a.VerifyMailer == nil`, which is false for a "+
			"nil *email.Sender held in the interface). The inner guard in sendMagicLinkEmail only "+
			"answers the request AFTER the write.", len(keys), keys)
	}
	if out := logs(); strings.Contains(out, "magiclink_email_send_failed") {
		t.Fatalf("the magic-link request reached the send and logged magiclink_email_send_failed for "+
			"an unconfigured deployment — the OUTER guard at auth_recovery.go:233 must skip the send "+
			"entirely (the inner guard's failure is what this line reports). Captured log:\n%s", out)
	}
}

// magicLinkKeysFor returns the live `jf:magic:*` keys whose stored value is
// exactly userID (the login token's value is the user id; a signup token's is
// "email:<addr>", so it never matches).
//
// Scoping by the stored VALUE rather than by prefix alone is what keeps the
// assertion honest on a shared test Valkey: every checkout points at the same
// database, so a bare `jf:magic:*` count also sees the tokens a concurrent run's
// magic-link tests minted and could report a token this test did not write.
func magicLinkKeysFor(t *testing.T, sess *auth.Sessions, ctx context.Context, userID string) []string {
	t.Helper()
	rdb := sess.Client()
	var found []string
	var cursor uint64
	for {
		keys, next, err := rdb.Scan(ctx, cursor, auth.MagicLinkKeyPrefix+"*", 100).Result()
		if err != nil {
			t.Fatalf("scan %s*: %v", auth.MagicLinkKeyPrefix, err)
		}
		for _, k := range keys {
			if v, err := rdb.Get(ctx, k).Result(); err == nil && v == userID {
				found = append(found, k)
			}
		}
		if next == 0 {
			return found
		}
		cursor = next
	}
}
