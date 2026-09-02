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
	emailpkg "github.com/wzul/jomform/api/internal/email"
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
	if err := a.sendEmail2FACode(r.Context(), user.Email, code); err != nil {
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
// SES sender (suppression-aware + breaker-gated, mirroring #56).
func (a *AuthHandlers) sendEmail2FACode(ctx context.Context, email, code string) error {
	if a.VerifyMailer == nil {
		return errors.New("email code mailer not configured")
	}
	if a.Suppressions != nil {
		if sup, err := emailpkg.ShouldSuppress(ctx, a.Suppressions, email); err != nil {
			return err
		} else if sup {
			slog.Warn("email2fa_suppressed_skipped", "email", obfuscateEmail(email))
			return nil // suppressed: never resend, not a hard error
		}
	}
	body := "Kod sandaran sekali-pakai untuk log masuk 2FA JomForm anda:\n\n" +
		"\t" + code + "\n\n" +
		"Kod ini sah selama 10 minit dan hanya boleh digunakan sekali. Ia menggantikan kod dari aplikasi pengesah anda.\n\n" +
		"Jika anda tidak cuba log masuk, anda boleh abaikan emel ini.\n"
	if err := a.VerifyMailer.Send(email, "Kod sandaran 2FA JomForm anda", body); err != nil {
		return err
	}
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
// marginally weaker than TOTP alone.
func (a *AuthHandlers) send2FAActiveEmail(ctx context.Context, email string) error {
	if a.VerifyMailer == nil {
		return errors.New("2fa active mailer not configured")
	}
	if a.Suppressions != nil {
		if sup, err := emailpkg.ShouldSuppress(ctx, a.Suppressions, email); err != nil {
			return err
		} else if sup {
			slog.Warn("2fa_active_suppressed_skipped", "email", obfuscateEmail(email))
			return nil
		}
	}
	body := "Pengesahan dua faktor (2FA) kini AKTIF pada akaun JomForm anda. ✓\n\n" +
		"Pada log masuk seterusnya, anda perlu mengesahkan kod 6-angka daripada aplikasi pengesah anda.\n\n" +
		"Jika anda kehilangan akses kepada aplikasi pengesah, anda boleh meminta kod sandaran sekali-pakai\n" +
		"dihantar ke emel ini melalui pautan \"Hantar kod melalui emel\" pada langkah 2FA. Nota: kod emel\n" +
		"adalah laluan pemulihan yang lebih mudah tetapi lebih lemah sedikit berbanding TOTP sahaja — layan\n" +
		"peti masuk emel anda dengan selamat.\n"
	if err := a.VerifyMailer.Send(email, "2FA kini aktif pada akaun JomForm anda", body); err != nil {
		return err
	}
	if a.Breaker != nil {
		if err := a.Breaker.RecordSend(ctx); err != nil {
			slog.Warn("breaker_record_send_2fa_active", "err", err)
		}
	}
	return nil
}

// ─── magic link (issue #73) ─────────────────────────────────────────────────

// handleMagicLinkRequest: POST /v1/auth/magic-link/request {email}
// Always 200-generic. When the email belongs to a verified user, mints a
// single-use hashed 15-min token and emails a passwordless sign-in link via the
// #56 reputation-checked sender. Tight per-IP rate limit in RegisterRoutes.
func (a *AuthHandlers) handleMagicLinkRequest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
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

	user, err := a.Users.ByEmail(r.Context(), email)
	if err != nil || user == nil || !user.Verified {
		// No account or unverified: still 200 (no enumeration). A magic link
		// must never be sent to an unverified address.
		a.replyMagicLinkRequestOK(w)
		return
	}
	if a.Sessions == nil || a.VerifyMailer == nil {
		a.replyMagicLinkRequestOK(w) // nothing wired (bare test) — generic ok
		return
	}

	token, err := a.Sessions.MagicLink().Generate(r.Context(), user.ID)
	if err != nil {
		slog.Error("magiclink_generate", "err", err, "user", user.ID)
		a.replyMagicLinkRequestOK(w)
		return
	}
	if err := a.sendMagicLinkEmail(r.Context(), user.Email, token); err != nil {
		slog.Warn("magiclink_email_send_failed", "err", err, "user", user.ID)
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
func (a *AuthHandlers) sendMagicLinkEmail(ctx context.Context, email, token string) error {
	if a.VerifyMailer == nil {
		return errors.New("magic link mailer not configured")
	}
	if a.Suppressions != nil {
		if sup, err := emailpkg.ShouldSuppress(ctx, a.Suppressions, email); err != nil {
			return err
		} else if sup {
			slog.Warn("magiclink_suppressed_skipped", "email", obfuscateEmail(email))
			return nil
		}
	}
	link := strings.TrimRight(a.BaseURL, "/") + "/magic-link?token=" + token
	body := "Log masuk tanpa kata laluan ke JomForm anda:\n\n" +
		link + "\n\n" +
		"Pautan ini sah selama 15 minit dan hanya boleh digunakan sekali.\n\n" +
		"Jika anda tidak meminta ini, anda boleh abaikan emel ini — akaun anda selamat.\n"
	if err := a.VerifyMailer.Send(email, "Pautan log masuk JomForm anda", body); err != nil {
		return err
	}
	if a.Breaker != nil {
		if err := a.Breaker.RecordSend(ctx); err != nil {
			slog.Warn("breaker_record_send_magiclink", "err", err)
		}
	}
	return nil
}

// handleMagicLinkVerify: POST /v1/auth/magic-link/verify {token}
// Atomically consumes the single-use token and starts a session for its owner.
func (a *AuthHandlers) handleMagicLinkVerify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Token == "" {
		writeErr(w, http.StatusBadRequest, "token diperlukan")
		return
	}
	if a.Sessions == nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	userID, err := a.Sessions.MagicLink().Consume(r.Context(), req.Token)
	if err != nil {
		if errors.Is(err, auth.ErrMagicLinkInvalid) {
			writeErr(w, http.StatusBadRequest, "pautan log masuk tidak sah atau telah tamat tempoh")
			return
		}
		slog.Error("magiclink_consume", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	user, err := a.Users.ByID(r.Context(), userID)
	if err != nil || user == nil {
		writeErr(w, http.StatusBadRequest, "akaun tidak dijumpai")
		return
	}
	// Start a short, default (non-remembered) session. No trust-device cookie
	// is ever issued via magic link. Existing sessions are left intact.
	if err := a.startSession(w, r, user.ID, user.Email, user.TOTPEnabled, false); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	slog.Info("magiclink_login", "user", user.ID)
	writeJSON(w, http.StatusOK, meResponse{
		ID: user.ID, Email: user.Email, TOTPEnabled: user.TOTPEnabled, CreatedAt: user.CreatedAt.UTC().Format(time.RFC3339),
	})
}
