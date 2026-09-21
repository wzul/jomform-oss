// Auth UX handlers for issues #62 + #63:
//   - Password recovery (forgot/reset) with single-use, hashed, 30-min tokens.
//   - Trusted-device (jf_trust) status and forget endpoints.
//
// Plus the shared session-resolution guard that rejects sessions older than a
// password reset (pw_reset_at), used by every session-consuming caller.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
	emailpkg "github.com/wzul/jomform/api/internal/email"
	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/httpapi/middleware"
)

// ─── password recovery (issue #62) ───────────────────────────────────────────

// handleForgotPassword: POST /v1/auth/forgot-password
// Always returns 200 with a generic message. When the email belongs to a real
// AND verified user, mints a single-use reset token (hashed in Valkey, 30 min)
// and emails the reset link via the same SES sender as verification emails, so
// the #56 suppression/breaker that gates verification emails gates reset
// emails too (the mail-bomb vector is identical).
func (a *AuthHandlers) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	// Malformed body still yields the same generic success (no enumeration).
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.replyForgotPasswordOK(w)
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		a.replyForgotPasswordOK(w)
		return
	}

	user, err := a.Users.ByEmail(r.Context(), email)
	if err != nil || user == nil || !user.Verified {
		// No account, or unverified: still 200 (no enumeration). A real
		// password-reset link must never go to an unverified address.
		a.replyForgotPasswordOK(w)
		return
	}

	token, err := a.Sessions.PasswordReset().Generate(r.Context(), user.ID)
	if err != nil {
		slog.Error("pwreset_generate", "err", err, "user", user.ID)
		a.replyForgotPasswordOK(w) // respond generically; user can retry
		return
	}
	if err := a.sendPasswordResetEmail(r.Context(), user.Email, token); err != nil {
		slog.Warn("pwreset_email_send_failed", "err", err, "user", user.ID)
		// Token exists but the email didn't send; it expires in 30 min.
	}
	a.replyForgotPasswordOK(w)
}

// replyForgotPasswordOK emits the uniform, enumeration-safe 200 body.
func (a *AuthHandlers) replyForgotPasswordOK(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"message": "Jika emel tersebut berdaftar dan disahkan, pautan set semula kata laluan telah dihantar ke peti masuk anda.",
	})
}

// sendPasswordResetEmail delivers the reset link (BASE_URL/reset-password?token=…).
// Issue #199: when an EmailQueue is wired the email is enqueued and sent by a
// background worker; otherwise it is sent synchronously.
//
// The suppression check here is an early exit that avoids minting a link for an
// address we will never mail; the load-bearing guard is the pre-send choke point
// at (*email.Sender).SendReceipt, which also covers the queued path. A store
// error fails open (the send proceeds) — see (*email.Sender).suppressed.
func (a *AuthHandlers) sendPasswordResetEmail(ctx context.Context, email, token string) error {
	if mailerUnavailable(a.VerifyMailer) {
		return errors.New("reset mailer not configured")
	}
	if a.Suppressions != nil {
		sup, err := emailpkg.ShouldSuppress(ctx, a.Suppressions, email)
		if err != nil {
			slog.Error("email_suppression_check_error", "to", obfuscateEmail(email), "err", err)
		} else if sup {
			slog.Warn("pwreset_email_suppressed_skipped", "email", obfuscateEmail(email))
			recordSESSuppression("password_reset")
			return nil // suppressed: don't send, don't surface as a hard error
		}
	}
	link := strings.TrimRight(a.BaseURL, "/") + "/reset-password?token=" + token
	body := "Anda telah meminta untuk set semula kata laluan JomForm anda.\n\n" +
		"Tap pautan berikut untuk menetapkan kata laluan baharu (sah selama 30 minit):\n\n" +
		link + "\n\n" +
		"Jika anda tidak meminta ini, anda boleh abaikan emel ini — kata laluan anda tidak berubah.\n"
	// Issue #199: background path — enqueue and return immediately.
	if a.EmailQueue != nil {
		if err := a.EmailQueue.Enqueue(ctx, emailqueue.Job{
			Type: "password_reset", To: email,
			Subject: "Set semula kata laluan JomForm anda", Body: body,
		}); err != nil {
			return err
		}
		return nil
	}
	if err := a.VerifyMailer.Send(email, "Set semula kata laluan JomForm anda", body); err != nil {
		return err
	}
	recordSESSend("password_reset")
	return nil
}

// handleResetPassword: POST /v1/auth/reset-password {token,new_password}
// Atomically consumes the single-use token, validates password policy (>=10),
// updates the hash, stamps pw_reset_at (killing all prior sessions) and
// invalidates every trust cookie. Responds 200 → UI redirects to login.
// Does NOT start a session (user must log in again — simple + auditable).
func (a *AuthHandlers) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" {
		writeErr(w, http.StatusBadRequest, "token diperlukan")
		return
	}
	if len(req.NewPassword) < 10 {
		writeErr(w, http.StatusBadRequest, "kata laluan baharu mesti sekurang-kurangnya 10 aksara")
		return
	}

	userID, err := a.Sessions.PasswordReset().Consume(r.Context(), req.Token)
	if err != nil {
		if errors.Is(err, auth.ErrResetTokenInvalid) {
			writeErr(w, http.StatusBadRequest, "pautan set semula tidak sah atau telah tamat tempoh")
			return
		}
		slog.Error("pwreset_consume", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	user, err := a.Users.ByID(r.Context(), userID)
	if err != nil || user == nil {
		writeErr(w, http.StatusBadRequest, "akaun tidak dijumpai")
		return
	}

	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := a.Users.ResetPassword(r.Context(), userID, hash); err != nil {
		slog.Error("pwreset_apply", "err", err, "user", userID)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Invalidate every trusted device (issue #63) — a reset is a credential
	// change and must force TOTP again everywhere.
	if a.Sessions != nil {
		_ = a.Sessions.TrustedDevices().RevokeAll(r.Context(), userID)
	}
	slog.Info("password_reset", "user", userID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "password_reset"})
}

// ─── trusted devices (issue #63) ─────────────────────────────────────────────

// trustStore returns the TrustedDevices store, or nil when Valkey is absent.
func (a *AuthHandlers) trustStore() *auth.TrustedDevices {
	if a.Sessions == nil {
		return nil
	}
	return a.Sessions.TrustedDevices()
}

// trustCookieValue returns the raw jf_trust cookie value ("" when absent).
func (a *AuthHandlers) trustCookieValue(r *http.Request) string {
	c, err := r.Cookie(trustCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

// trustMatchesUser reports whether a valid jf_trust cookie maps to userID.
func (a *AuthHandlers) trustMatchesUser(r *http.Request, userID string) bool {
	ts := a.trustStore()
	if ts == nil {
		return false
	}
	return ts.UserIDFor(r.Context(), a.trustCookieValue(r)) == userID
}

// issueTrustCookie mints a jf_trust cookie + Valkey record for userID. The
// requesting device's user-agent and IP are recorded so Profile → Security can
// list the device (issue #454).
func (a *AuthHandlers) issueTrustCookie(w http.ResponseWriter, r *http.Request, userID string) {
	ts := a.trustStore()
	if ts == nil {
		return
	}
	token, err := ts.IssueDevice(r.Context(), userID, r.UserAgent(), middleware.ClientIP(r))
	if err != nil {
		slog.Error("trust_issue", "err", err, "user", userID)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     trustCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   !isDev,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(auth.TrustTTL.Seconds()),
	})
}

// clearTrustCookie expires the jf_trust cookie on the client.
func clearTrustCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     trustCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   !isDev,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// handleTrustStatus: GET /v1/auth/trust-status
// Reports whether the current trust cookie is valid for the logged-in user and
// an approximate expiry (now + remaining window). Profile → Security line.
type trustStatusResponse struct {
	Trusted bool   `json:"trusted"`
	Until   string `json:"until,omitempty"` // RFC3339, when trusted
}

func (a *AuthHandlers) handleTrustStatus(w http.ResponseWriter, r *http.Request) {
	user := a.currentUser(r)
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "tidak log masuk")
		return
	}
	ts := a.trustStore()
	token := a.trustCookieValue(r)
	trusted := ts != nil && ts.UserIDFor(r.Context(), token) == user.ID
	out := trustStatusResponse{Trusted: trusted}
	if trusted {
		exp := ts.Expiry(r.Context(), token)
		if !exp.IsZero() {
			out.Until = exp.Format(time.RFC3339)
		} else {
			out.Until = time.Now().UTC().Add(auth.TrustTTL).Format(time.RFC3339)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleTrustForget: POST /v1/auth/trust/forget — forget THIS device.
func (a *AuthHandlers) handleTrustForget(w http.ResponseWriter, r *http.Request) {
	user := a.currentUser(r)
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "tidak log masuk")
		return
	}
	if ts := a.trustStore(); ts != nil {
		_ = ts.Revoke(r.Context(), user.ID, a.trustCookieValue(r))
	}
	clearTrustCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "forgotten"})
}

// handleTrustForgetAll: POST /v1/auth/trust/forget-all — revoke every trusted
// device. Also clears the current cookie.
func (a *AuthHandlers) handleTrustForgetAll(w http.ResponseWriter, r *http.Request) {
	user := a.currentUser(r)
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "tidak log masuk")
		return
	}
	if ts := a.trustStore(); ts != nil {
		if err := ts.RevokeAll(r.Context(), user.ID); err != nil {
			slog.Error("trust_revoke_all", "err", err, "user", user.ID)
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	clearTrustCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked_all"})
}

// ─── trusted devices list + revoke (issue #454) ─────────────────────────────
//
// #63 could only forget the CURRENT device. #454 adds the management list:
// GET /v1/auth/trust-devices lists every trusted device (label, IP, first
// trusted, expiry), DELETE /v1/auth/trust/{id} revokes one by its opaque id,
// and DELETE /v1/auth/trust revokes all ("log out everywhere"). Revocation is
// immediate — the trust key is deleted, so the very next login on that device
// is challenged for TOTP again.

// trustDeviceOut is one row in the trusted-devices list.
type trustDeviceOut struct {
	ID        string `json:"id"`
	Label     string `json:"label,omitempty"`
	IP        string `json:"ip,omitempty"`
	FirstSeen string `json:"first_seen,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
	Current   bool   `json:"current,omitempty"`
}

// handleTrustDevices: GET /v1/auth/trust-devices — list the logged-in user's
// trusted devices for the Profile → Security management list.
func (a *AuthHandlers) handleTrustDevices(w http.ResponseWriter, r *http.Request) {
	user := a.currentUser(r)
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "tidak log masuk")
		return
	}
	ts := a.trustStore()
	if ts == nil {
		writeJSON(w, http.StatusOK, []trustDeviceOut{})
		return
	}
	devices, err := ts.List(r.Context(), user.ID, ts.HashFor(a.trustCookieValue(r)))
	if err != nil {
		slog.Error("trust_list", "err", err, "user", user.ID)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]trustDeviceOut, 0, len(devices))
	for _, d := range devices {
		out = append(out, trustDeviceOut{
			ID: d.ID, Label: d.Label, IP: d.IP,
			FirstSeen: d.FirstSeen, ExpiresAt: d.ExpiresAt, Current: d.Current,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleTrustRevokeOne: DELETE /v1/auth/trust/{id} — revoke one trusted device
// by its opaque id. If it was the current device, its cookie is cleared too.
// Idempotent: an unknown/already-revoked id still returns 200.
func (a *AuthHandlers) handleTrustRevokeOne(w http.ResponseWriter, r *http.Request) {
	user := a.currentUser(r)
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "tidak log masuk")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeErr(w, http.StatusBadRequest, "id peranti diperlukan")
		return
	}
	ts := a.trustStore()
	if ts == nil {
		writeErr(w, http.StatusServiceUnavailable, "perkhidmatan tidak tersedia")
		return
	}
	// RevokeHash only acts on a hash present in THIS user's trust set, so one
	// user can never revoke another user's device.
	if err := ts.RevokeHash(r.Context(), user.ID, id); err != nil {
		slog.Error("trust_revoke_one", "err", err, "user", user.ID)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if id == ts.HashFor(a.trustCookieValue(r)) {
		clearTrustCookie(w)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked", "id": id})
}

// handleTrustRevokeAll: DELETE /v1/auth/trust — "log out everywhere": revoke
// every trusted device for the user. Equivalent to forget-all but on the
// management endpoint's RESTful verb.
func (a *AuthHandlers) handleTrustRevokeAll(w http.ResponseWriter, r *http.Request) {
	user := a.currentUser(r)
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "tidak log masuk")
		return
	}
	if ts := a.trustStore(); ts != nil {
		if err := ts.RevokeAll(r.Context(), user.ID); err != nil {
			slog.Error("trust_revoke_all", "err", err, "user", user.ID)
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	clearTrustCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked_all"})
}

// ─── session resolution: pw_reset_at invalidation (issue #62) ───────────────

// sessionUser resolves a session cookie to its user, honoring pw_reset_at: if
// the user reset their password after the session was issued, the session is
// treated as invalid (nil). This kills every pre-reset session on all devices.
func (a *AuthHandlers) sessionUser(r *http.Request) *dbstore.User {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil
	}
	userID, issued, err := a.Sessions.GetCreated(r.Context(), c.Value)
	if err != nil {
		return nil
	}
	user, err := a.Users.ByID(r.Context(), userID)
	if err != nil {
		return nil
	}
	if user.PwResetAt != nil && !auth.ValidAfter(issued, *user.PwResetAt) {
		// Session predates the reset → dead. Destroy the stale cookie mapping
		// so it isn't carried around.
		_ = a.Sessions.Destroy(r.Context(), c.Value)
		return nil
	}
	return user
}
