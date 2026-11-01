package httpapi

// The SELF-HOSTED verification path: a deployment with no email sender must be
// able to confirm its first account, and it must say so in its own log rather
// than creating an account nobody can ever log into.
//
// WHY THIS IS A TEST AND NOT A DOC SENTENCE. Registration creates the user
// unverified and starts no session (auth.go handleRegister: "No session is
// started: the account must be email-verified before first login"), and the
// ONLY copy of the confirmation link is the message sendVerifyEmail delivers.
// The sender is the AWS SESv2 API, not plain SMTP, so a self-hosted box with no
// AWS credentials mails nothing at all — the self-hoster's first signup was a
// dead account. JOMFORM_LOG_VERIFY_LINK=1 is the way out, and the two ways it
// could be wrong are both silent:
//
//   - the switch is on but nothing is logged (the operator follows the
//     documented `docker compose logs api` step and finds no link), or
//   - the switch leaks into a deployment that HAS a mailer, putting an
//     account-activation token in the logs of a working install.
//
// Both directions are pinned below, plus the "no switch, no mailer" baseline
// (still a recorded failure, never a silent success) and the fact that the
// logged link is the SAME link the mailer would have been handed.

import (
	"bytes"
	"context"
	"log/slog"
	"net/url"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/auth"
)

// noMailer is a VerifyMailer that records what it was asked to send. A NIL
// VerifyMailer field is the production shape (main.go leaves it a nil interface
// when SES is unconfigured — see makeAuthMailer), and mailerUnavailable reads
// that through niliface. The recording variant is the control arm: it proves the
// switch does NOT divert a deployment that can actually send.
type recordingMailer struct {
	sent []string
}

func (m *recordingMailer) Send(to, subject, body, lang string) error {
	m.sent = append(m.sent, to+"|"+subject+"|"+body)
	return nil
}

func captureWarnLog(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	return &buf, func() { slog.SetDefault(prev) }
}

func selfHostAuth(logVerifyLink bool) *AuthHandlers {
	return &AuthHandlers{
		BaseURL:       "https://forms.example.test",
		VerifyKey:     bytes.Repeat([]byte{0x2a}, 32),
		VerifyMailer:  nil, // SES unconfigured, exactly as main.go leaves it
		LogVerifyLink: logVerifyLink,
	}
}

func TestVerifyLinkIsLoggedWhenNoMailerAndSwitchOn(t *testing.T) {
	buf, restore := captureWarnLog(t)
	defer restore()

	h := selfHostAuth(true)
	if err := h.sendVerifyEmail(context.Background(), "user-1", "first@example.test", "ms"); err != nil {
		t.Fatalf("sendVerifyEmail with the switch on returned %v, want nil "+
			"(an error here is logged as verify_email_send_failed and the self-hoster is "+
			"left with no way to confirm the account)", err)
	}

	out := buf.String()
	if !strings.Contains(out, "verify_link_logged_instead_of_emailed") {
		t.Fatalf("no verify_link_logged_instead_of_emailed line in the log; got:\n%s", out)
	}
	if !strings.Contains(out, "/v1/auth/verify?token=") {
		t.Fatalf("the logged line carries no confirmation link; got:\n%s", out)
	}
	// The page the link opens renders in the language the signup was made in,
	// so the ?lang= the EMAIL path adds must be on the logged link too.
	if !strings.Contains(out, "lang=ms") {
		t.Fatalf("the logged link lost the signup locale (?lang=ms); got:\n%s", out)
	}
	// Non-vacuity on the base URL: the link must be built from THIS deployment's
	// APP_BASE_URL, because the confirmation page is served here and nowhere
	// else. A link to the wrong host is indistinguishable from no link.
	if !strings.Contains(out, "https://forms.example.test/v1/auth/verify?token=") {
		t.Fatalf("the logged link is not built from APP_BASE_URL; got:\n%s", out)
	}
}

// The control: a deployment that CAN send never logs a link, even with the
// switch on. Otherwise the switch would be a permanent back door into the logs
// of every install that turned it on once.
func TestVerifyLinkIsNotLoggedWhenAMailerExists(t *testing.T) {
	buf, restore := captureWarnLog(t)
	defer restore()

	mailer := &recordingMailer{}
	h := selfHostAuth(true)
	h.VerifyMailer = mailer

	if err := h.sendVerifyEmail(context.Background(), "user-1", "first@example.test", "en"); err != nil {
		t.Fatalf("sendVerifyEmail returned %v, want nil", err)
	}
	if len(mailer.sent) != 1 {
		t.Fatalf("mailer got %d message(s), want 1 — the switch diverted a sendable deployment", len(mailer.sent))
	}
	if strings.Contains(buf.String(), "/v1/auth/verify?token=") {
		t.Fatalf("a verification link reached the log although a mailer is configured:\n%s", buf.String())
	}
}

// The baseline: switch off + no mailer = the pre-existing behaviour. A recorded
// failure the caller logs, never a silent nil that would look like a send.
func TestVerifyLinkNeedsTheSwitchWhenThereIsNoMailer(t *testing.T) {
	buf, restore := captureWarnLog(t)
	defer restore()

	h := selfHostAuth(false)
	err := h.sendVerifyEmail(context.Background(), "user-1", "first@example.test", "en")
	if err == nil {
		t.Fatalf("sendVerifyEmail succeeded with no mailer and no switch — the default " +
			"must still report that nothing was sent")
	}
	if strings.Contains(buf.String(), "/v1/auth/verify?token=") {
		t.Fatalf("a verification link was logged without JOMFORM_LOG_VERIFY_LINK:\n%s", buf.String())
	}
}

// The logged link must be a link the SERVER will accept, and it must carry the
// same locale the email path does. Two builders (one for "email it", one for
// "log it") is how a deployment ends up logging a link that cannot verify: the
// token is signed against ENCRYPTION_KEY, so a token minted from a different key
// or a truncated URL answers "link not valid" and the self-hoster is stuck
// again, with a link that looks right in the log.
//
// The comparison is on VERIFICATION, not on string equality: the token embeds a
// per-second expiry, so the logged and mailed arms legitimately mint different
// tokens a second apart — asserting the two URLs are byte-identical would flake
// on a second boundary and prove nothing about the property that matters.
func TestLoggedVerifyLinkIsOneTheServerAccepts(t *testing.T) {
	logBuf, restore := captureWarnLog(t)
	logged := selfHostAuth(true)
	if err := logged.sendVerifyEmail(context.Background(), "user-1", "same@example.test", "ta"); err != nil {
		restore()
		t.Fatalf("sendVerifyEmail on the no-mailer path: %v", err)
	}
	restore()

	link := extractVerifyLink(t, logBuf.String())
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("the logged link does not parse (%q): %v", link, err)
	}
	if u.Path != "/v1/auth/verify" {
		t.Fatalf("the logged link points at %q, want the /v1/auth/verify handler (handleVerify)", u.Path)
	}
	email, err := auth.VerifyToken(logged.VerifyKey, u.Query().Get("token"))
	if err != nil {
		t.Fatalf("the logged token does not verify against the deployment's own key: %v", err)
	}
	if email != "same@example.test" {
		t.Fatalf("the logged token verifies to %q, want the address that registered", email)
	}
	if lang := u.Query().Get("lang"); lang != "ta" {
		t.Fatalf("the logged link carries lang=%q, want the signup locale ta "+
			"(the confirmation page would render in the wrong language)", lang)
	}
}

// extractVerifyLink pulls the `link=` field out of the structured log line, so
// the comparison below is between the two URLs and not between two log formats.
// slog's TextHandler QUOTES a value that carries `?`/`&` (measured: the logged
// field came back as `link="https://…&lang=ta"`), so the quotes come off here.
func extractVerifyLink(t *testing.T, logLine string) string {
	t.Helper()
	for _, f := range strings.Fields(logLine) {
		if v, ok := strings.CutPrefix(f, "link="); ok {
			return strings.Trim(v, "\"")
		}
	}
	t.Fatalf("no link= field in the log line:\n%s", logLine)
	return ""
}
