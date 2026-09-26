// Auth recovery handlers for issues #72 + #73:
//   - 2FA email one-time backup code (issue #72): a 6-digit code emailed during
//     login as an alternative to the authenticator TOTP. Gated on a short
//     pending flag armed only by a correct-password `totp_required` response,
//     single-use, suppression/breaker aware, and never grants trust-device.
//   - Passwordless magic link (issue #73): a single-use 15-min link that starts
//     a session directly for a verified user.
package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
	emailpkg "github.com/wzul/jomform/api/internal/email"
	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/emailtext"
	"github.com/wzul/jomform/api/internal/httpapi/middleware"
)

// ─── 2FA email backup code (issue #72) ──────────────────────────────────────

// email2FA returns the Email2FA store, or nil when Valkey is absent.
func (a *AuthHandlers) email2FA() *auth.Email2FA {
	if a.Sessions == nil {
		return nil
	}
	return a.Sessions.Email2FA()
}

// handleEmail2FACode: POST /v1/auth/2fa/email-code {email}
// Send a 6-digit one-time backup code to the account's inbox. Always returns a
// generic 200 (no enumeration). A code is only minted when BOTH:
//   - the account exists and is verified, AND
//   - a live `jf:2fa_pending:{ip}:{email}` flag exists — armed exclusively by a
//     correct-password login that returned `totp_required`. This proves the
//     requestor knows the password, so a blind attacker who only knows an email
//     can never harvest backup codes.
//
// The endpoint itself is per-IP rate-limited to 3/hour (see RegisterRoutes).
func (a *AuthHandlers) handleEmail2FACode(w http.ResponseWriter, r *http.Request) {
	ip := middleware.ClientIP(r)
	var req struct {
		Email string `json:"email"`
	}
	if err := decodeJSON(r, &req); err != nil {
		a.replyEmail2FACodeOK(w)
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		a.replyEmail2FACodeOK(w)
		return
	}

	// The pending flag is the gate. Without it we never send (generic 200).
	e2fa := a.email2FA()
	pending := false
	if e2fa != nil {
		p, err := e2fa.Pending(r.Context(), ip, email)
		if err != nil {
			slog.Warn("email2fa_pending_error", "err", err, "ip", ip)
		}
		pending = p
	}
	if !pending {
		a.replyEmail2FACodeOK(w)
		return
	}
	// Re-confirm the account is real + verified before emailing a code.
	user, err := a.Users.ByEmail(r.Context(), email)
	if err != nil || user == nil || !user.Verified || user.TOTPSecret == "" {
		a.replyEmail2FACodeOK(w)
		return
	}

	code, err := auth.Generate6Digit()
	if err != nil {
		slog.Error("email2fa_generate", "err", err)
		a.replyEmail2FACodeOK(w)
		return
	}
	if e2fa == nil {
		a.replyEmail2FACodeOK(w) // no store wired → cannot issue (bare test)
		return
	}
	if err := e2fa.Store(r.Context(), user.ID, code); err != nil {
		slog.Error("email2fa_store", "err", err, "user", user.ID)
		a.replyEmail2FACodeOK(w)
		return
	}
	if err := a.sendEmail2FACode(r.Context(), user.Email, code, user.Lang); err != nil {
		slog.Warn("email2fa_send_failed", "err", err, "user", user.ID)
		// Code stored but email didn't send; it expires in 10 min. Keep the
		// stored code so a retry from the login page succeeds when SES recovers.
	}
	// Consume the pending flag regardless — the user had one window to request.
	_ = e2fa.ConsumePending(r.Context(), ip, email)
	a.replyEmail2FACodeOK(w)
}

// replyEmail2FACodeOK emits the uniform, enumeration-safe 200 body.
func (a *AuthHandlers) replyEmail2FACodeOK(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"message": "Jika akaun anda memenuhi syarat, kod 6-angka telah dihantar ke emel anda.",
	})
}

// sendEmail2FACode delivers the 6-digit backup code via the reputaion-checked
// SES sender (suppression-aware + breaker-gated, mirroring #56). Issue #199:
// when an EmailQueue is wired the email is enqueued and sent by a background
// worker; otherwise it is sent synchronously.
func (a *AuthHandlers) sendEmail2FACode(ctx context.Context, email, code, lang string) error {
	if mailerUnavailable(a.VerifyMailer) {
		return errors.New("email code mailer not configured")
	}
	if a.Suppressions != nil {
		if sup, err := emailpkg.ShouldSuppress(ctx, a.Suppressions, email); err != nil {
			// Fail open (see (*email.Sender).suppressed): the pre-send choke
			// point still guards the send, and a store blip must not stop a
			// user from receiving their 2FA fallback code.
			slog.Error("email_suppression_check_error", "to", obfuscateEmail(email), "err", err)
		} else if sup {
			slog.Warn("email2fa_suppressed_skipped", "email", obfuscateEmail(email))
			recordSESSuppression("email2fa")
			return nil // suppressed: never resend, not a hard error
		}
	}
	msg := emailtext.Text(lang, emailtext.Email2FACode, map[string]string{"code": code})
	// Issue #199: background path — enqueue and return immediately.
	if a.EmailQueue != nil {
		if err := a.EmailQueue.Enqueue(ctx, emailqueue.Job{
			Type: "email2fa", To: email,
			Subject: msg.Subject, Body: msg.Body, Lang: msg.Lang,
		}); err != nil {
			return err
		}
		return nil
	}
	if err := a.VerifyMailer.Send(email, msg.Subject, msg.Body, msg.Lang); err != nil {
		return err
	}
	recordSESSend("email2fa")
	if a.Breaker != nil {
		if err := a.Breaker.RecordSend(ctx); err != nil {
			slog.Warn("breaker_record_send_email2fa", "err", err)
		}
	}
	return nil
}

// send2FAActiveEmail confirms on enable (issue #72) that 2FA is active and
// explains the email-code fallback trade-off: a backup code is a convenience
// recovery path that bypasses the authenticator, so it is more convenient but
// marginally weaker than TOTP alone. Issue #199: enqueued when a queue is wired.
func (a *AuthHandlers) send2FAActiveEmail(ctx context.Context, email, lang string) error {
	if mailerUnavailable(a.VerifyMailer) {
		return errors.New("2fa active mailer not configured")
	}
	if a.Suppressions != nil {
		if sup, err := emailpkg.ShouldSuppress(ctx, a.Suppressions, email); err != nil {
			// Fail open (see (*email.Sender).suppressed): the pre-send choke
			// point still guards the send.
			slog.Error("email_suppression_check_error", "to", obfuscateEmail(email), "err", err)
		} else if sup {
			slog.Warn("2fa_active_suppressed_skipped", "email", obfuscateEmail(email))
			recordSESSuppression("2fa_active")
			return nil
		}
	}
	msg := emailtext.Text(lang, emailtext.TwoFAActivated, nil)
	// Issue #199: background path — enqueue and return immediately.
	if a.EmailQueue != nil {
		if err := a.EmailQueue.Enqueue(ctx, emailqueue.Job{
			Type: "2fa_active", To: email,
			Subject: msg.Subject, Body: msg.Body, Lang: msg.Lang,
		}); err != nil {
			return err
		}
		return nil
	}
	if err := a.VerifyMailer.Send(email, msg.Subject, msg.Body, msg.Lang); err != nil {
		return err
	}
	recordSESSend("2fa_active")
	if a.Breaker != nil {
		if err := a.Breaker.RecordSend(ctx); err != nil {
			slog.Warn("breaker_record_send_2fa_active", "err", err)
		}
	}
	return nil
}

// ─── magic link (issue #73) ─────────────────────────────────────────────────

// handleMagicLinkRequest: POST /v1/auth/magic-link/request {email}
// Always 200-generic. Hybrid passwordless (issue #278): one email input serves
// BOTH login and signup.
//   - Existing verified user → mints a single-use hashed 15-min token bound to
//     the userID and emails a passwordless sign-in link (unchanged, #73).
//   - New email → mints a single-use hashed 15-min token bound to the email
//     and emails a sign-up link that, on click, creates the account
//     (verified=true, since the address was confirmed by clicking) + logs in.
//
// Tight per-IP rate limit in RegisterRoutes. Honeypot (issue #42) applies to
// the signup path so a bot that fills the hidden field is silently discarded.
func (a *AuthHandlers) handleMagicLinkRequest(w http.ResponseWriter, r *http.Request) {
	ip := middleware.ClientIP(r)
	var req struct {
		Email string `json:"email"`
		// Honeypot (issue #42): a hidden form field bots fill, humans don't.
		// When populated the request is silently accepted but discarded.
		Website string `json:"website"`
		// Lang (issue #206): the user's chosen locale, carried on the signup
		// magic-link email so the confirmation page renders in their language.
		Lang string `json:"lang,omitempty"`
	}
	if err := decodeJSON(r, &req); err != nil {
		a.replyMagicLinkRequestOK(w)
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		a.replyMagicLinkRequestOK(w)
		return
	}
	if a.Sessions == nil || mailerUnavailable(a.VerifyMailer) {
		a.replyMagicLinkRequestOK(w) // nothing wired (bare test) — generic ok
		return
	}

	user, err := a.Users.ByEmail(r.Context(), email)
	if err == nil && user != nil && user.Verified {
		// Existing verified account → login magic link (unchanged, #73).
		token, err := a.Sessions.MagicLink().Generate(r.Context(), user.ID)
		if err != nil {
			slog.Error("magiclink_generate", "err", err, "user", user.ID)
			a.replyMagicLinkRequestOK(w)
			return
		}
		if err := a.sendMagicLinkEmail(r.Context(), user.Email, token, false, ""); err != nil {
			slog.Warn("magiclink_email_send_failed", "err", err, "user", user.ID)
			// Token exists but the email didn't send; it expires in 15 min.
		}
		a.replyMagicLinkRequestOK(w)
		return
	}

	// New email (or unverified) → signup magic link (issue #278). The account
	// is created on click, so the same anti-spam as password signup applies:
	// honeypot discard + the per-IP rate limit already on this route.
	if req.Website != "" {
		slog.Warn("magiclink_signup_honeypot_caught", "ip", ip)
		a.replyMagicLinkRequestOK(w)
		return
	}
	if !auth.ValidEmail(email) {
		a.replyMagicLinkRequestOK(w)
		return
	}
	token, err := a.Sessions.MagicLink().GenerateForEmail(r.Context(), email)
	if err != nil {
		slog.Error("magiclink_signup_generate", "err", err, "email", obfuscateEmail(email))
		a.replyMagicLinkRequestOK(w)
		return
	}
	if err := a.sendMagicLinkEmail(r.Context(), email, token, true, req.Lang); err != nil {
		slog.Warn("magiclink_signup_email_send_failed", "err", err, "email", obfuscateEmail(email))
		// Token exists but the email didn't send; it expires in 15 min.
	}
	a.replyMagicLinkRequestOK(w)
}

func (a *AuthHandlers) replyMagicLinkRequestOK(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"message": "Jika emel tersebut berdaftar dan disahkan, pautan log masuk telah dihantar ke peti masuk anda.",
	})
}

// sendMagicLinkEmail delivers the sign-in link (BASE_URL/magic-link?token=…).
// Issue #199: enqueued when a queue is wired; otherwise sent synchronously.
// Issue #278: when signup is true the email is the hybrid sign-up variant
// (account is created on click) and lang localizes the body.
func (a *AuthHandlers) sendMagicLinkEmail(ctx context.Context, email, token string, signup bool, lang string) error {
	if mailerUnavailable(a.VerifyMailer) {
		return errors.New("magic link mailer not configured")
	}
	if a.Suppressions != nil {
		if sup, err := emailpkg.ShouldSuppress(ctx, a.Suppressions, email); err != nil {
			// Fail open (see (*email.Sender).suppressed): the pre-send choke
			// point still guards the send.
			slog.Error("email_suppression_check_error", "to", obfuscateEmail(email), "err", err)
		} else if sup {
			slog.Warn("magiclink_suppressed_skipped", "email", obfuscateEmail(email))
			recordSESSuppression("magic_link")
			return nil
		}
	}
	link := strings.TrimRight(a.BaseURL, "/") + "/magic-link?token=" + token
	subject, body := magicLinkEmailText(link, signup, lang)
	// Issue #199: background path — enqueue and return immediately. Lang rides
	// with the rendered text so the worker brands the HTML part in the same
	// language (language consistency).
	if a.EmailQueue != nil {
		if err := a.EmailQueue.Enqueue(ctx, emailqueue.Job{
			Type: "magic_link", To: email,
			Subject: subject, Body: body, Lang: lang,
		}); err != nil {
			return err
		}
		return nil
	}
	if err := a.VerifyMailer.Send(email, subject, body, lang); err != nil {
		return err
	}
	recordSESSend("magic_link")
	if a.Breaker != nil {
		if err := a.Breaker.RecordSend(ctx); err != nil {
			slog.Warn("breaker_record_send_magiclink", "err", err)
		}
	}
	return nil
}

// magicLinkEmailText returns the localized subject + body for a magic-link
// email. signup=true renders the hybrid sign-up variant (account created on
// click); otherwise the login variant. lang selects one of the 5 supported
// locales, defaulting to English (issue #278 i18n).
//
// The strings live in the shared transactional dictionary
// (api/internal/emailtext) — the ONE place that decides what a JomForm email
// says in each language. They were duplicated here as a `switch lang` until the
// language-consistency change; the signature is unchanged so every caller and
// guard that knows this function keeps working.
func magicLinkEmailText(link string, signup bool, lang string) (subject, body string) {
	kind := emailtext.MagicLinkLogin
	if signup {
		kind = emailtext.MagicLinkSignup
	}
	msg := emailtext.Text(lang, kind, map[string]string{"link": link})
	return msg.Subject, msg.Body
}

// handleMagicLinkVerify: POST /v1/auth/magic-link/verify {token}
// Atomically consumes the single-use token and starts a session. Hybrid
// (issue #278): a login token signs in the existing user; a signup token
// creates the account (verified=true, since the email was confirmed by
// clicking the link) + auto-provisions the default account + signs in.
func (a *AuthHandlers) handleMagicLinkVerify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Token == "" {
		// Own wording for THIS surface (issue #6177 follow-up) — see
		// CodeMagicTokenMissing in errcode.go.
		writeErr(w, http.StatusBadRequest, "token log masuk diperlukan")
		return
	}
	if a.Sessions == nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	kind, val, err := a.Sessions.MagicLink().ConsumeAny(r.Context(), req.Token)
	if err != nil {
		if errors.Is(err, auth.ErrMagicLinkInvalid) {
			writeErr(w, http.StatusBadRequest, "pautan log masuk tidak sah atau telah tamat tempoh")
			return
		}
		slog.Error("magiclink_consume", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	var user *dbstore.User
	switch kind {
	case auth.MagicLinkUser:
		// Login token → existing user.
		user, err = a.Users.ByID(r.Context(), val)
		if err != nil || user == nil {
			writeErr(w, http.StatusBadRequest, "akaun tidak dijumpai")
			return
		}
	case auth.MagicLinkEmail:
		// Signup token → create the account (verified=true, the email was
		// confirmed by clicking the link) + auto-provision the default
		// account, mirroring handleRegister (issue #278).
		user, err = a.createMagicLinkUser(r.Context(), val)
		if err != nil {
			slog.Error("magiclink_signup_create", "err", err, "email", obfuscateEmail(val))
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
	default:
		writeErr(w, http.StatusBadRequest, "pautan log masuk tidak sah atau telah tamat tempoh")
		return
	}

	// Start a short, default (non-remembered) session. No trust-device cookie
	// is ever issued via magic link. Existing sessions are left intact.
	// Issue #458: the session is marked magic=true — the login method is
	// observable and sensitive actions require a fresh TOTP step-up when the
	// user also has 2FA enabled.
	if err := a.startSession(w, r, user.ID, false, true); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Successful login resets the failure counters (issue #345) so a user who
	// was locked out on the password path isn't re-locked by stale history
	// after signing in via a magic link. Fail-open (log only, never block).
	if a.LoginLimiter != nil {
		if err := a.LoginLimiter.Reset(r.Context(), middleware.ClientIP(r), user.Email); err != nil {
			slog.Warn("loginfail_reset_error_magiclink", "err", err, "ip", middleware.ClientIP(r))
		}
	}
	slog.Info("magiclink_login", "user", user.ID, "kind", kind)
	// Issue #92/#458: audit the login with the magic flag set, so the
	// passwordless path is distinguishable from a password login.
	a.recordAuthEvent(r, a.firstAccountID(r, user.ID), user.ID, "auth.login", map[string]any{
		"method": "magic_link", "totp": user.TOTPEnabled, "magic": true,
	})
	// Issue #164: include the user's default workspace so the frontend can skip
	// the workspace-picker when a default is set.
	defWorkspace := ""
	if a.Users != nil {
		if ds, err := a.Users.DefaultWorkspace(r.Context(), user.ID); err == nil {
			defWorkspace = ds
		}
	}
	writeJSON(w, http.StatusOK, meResponse{
		ID: user.ID, Email: user.Email, TOTPEnabled: user.TOTPEnabled, CreatedAt: user.CreatedAt.UTC().Format(time.RFC3339),
		Phone: user.Phone, DefaultWorkspaceID: defWorkspace,
		// Issue #458: this response is the magic-link verify path, so the
		// session is always magic here.
		MagicSession: true,
	})
}

// createMagicLinkUser creates a verified account for a signup magic-link email
// (issue #278). The email was confirmed by clicking the link, so the account
// is created verified=true and may log in immediately. Auto-provisions the
// default account (mirroring handleRegister). Returns the created user.
func (a *AuthHandlers) createMagicLinkUser(ctx context.Context, email string) (*dbstore.User, error) {
	// A race is possible if the same email is clicked twice; the unique
	// constraint makes the second create fail, so re-fetch the existing user.
	user, err := a.Users.Create(ctx, email, "")
	if err != nil {
		if errors.Is(err, dbstore.ErrEmailTaken) {
			existing, e2 := a.Users.ByEmail(ctx, email)
			if e2 == nil && existing != nil {
				return existing, nil
			}
		}
		return nil, err
	}
	// Mark verified — the address was confirmed by clicking the link.
	if err := a.Users.SetVerified(ctx, user.ID); err != nil {
		return nil, err
	}
	// Auto-provision the default account so new users can build immediately.
	if a.Accounts != nil {
		acc := &dbstore.Account{
			OwnerUserID: user.ID,
			Name:        strings.Split(email, "@")[0] + " Store",
			Slug:        slugify(strings.Split(email, "@")[0], user.ID),
		}
		if err := a.Accounts.Create(ctx, acc); err != nil {
			// Slug collision: append random suffix and retry once.
			acc.Slug = acc.Slug + "-" + shortRand()
			if err2 := a.Accounts.Create(ctx, acc); err2 != nil {
				slog.Error("magiclink_default_account_create", "err", err2, "user", user.ID)
			}
		}
	}
	return user, nil
}
