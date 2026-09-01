// Email confirmation (issue #42): new accounts are verified=false until they
// confirm their address via a signed link. This file wires the /v1/auth/verify
// endpoint and the register-time email send.
package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/httpapi/middleware"
)

// sendVerifyEmail mints a signed 24h verification token and emails the link to
// the just-registered user. The mailer being nil (bare-mode tests, or SES
// unconfigured) yields a clear error so the caller can log but not crash.
func (a *AuthHandlers) sendVerifyEmail(ctx context.Context, userID, email string) error {
	if a.VerifyMailer == nil {
		return fmt.Errorf("verify mailer not configured")
	}
	if len(a.VerifyKey) != 32 {
		return fmt.Errorf("verify key not configured")
	}
	token, err := auth.SignVerifyToken(a.VerifyKey, email)
	if err != nil {
		return err
	}
	link := strings.TrimRight(a.BaseURL, "/") + "/v1/auth/verify?token=" + token
	body := "Sila sahkan alamat emel anda untuk mengaktifkan akaun JomForm anda.\n\n" +
		"Tap pautan berikut (sah selama 24 jam):\n\n" +
		link + "\n\n" +
		"Jika anda tidak mendaftar akaun JomForm, anda boleh abaikan emel ini.\n"
	return a.VerifyMailer.Send(email, "Sahkan emel JomForm anda", body)
}

// handleVerify confirms a user's email from a signed token (?token=...).
// GET renders a small confirmation page; POST returns JSON. Both accept the
// same token so an app can complete verification after a fetch.
func (a *AuthHandlers) handleVerify(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		writeErr(w, http.StatusBadRequest, "token diperlukan")
		return
	}
	if len(a.VerifyKey) != 32 {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	email, err := auth.VerifyToken(a.VerifyKey, token)
	if err != nil {
		slog.Warn("verify_token_invalid", "err", err, "ip", middleware.ClientIP(r))
		writeErr(w, http.StatusBadRequest, "pautan pengesahan tidak sah atau telah tamat tempoh")
		return
	}
	user, uerr := a.Users.ByEmail(r.Context(), email)
	if uerr != nil || user == nil {
		writeErr(w, http.StatusBadRequest, "pautan pengesahan tidak sah")
		return
	}
	if user.Verified {
		// Idempotent: already verified.
		if r.Method == http.MethodGet {
			writeVerifyPage(w, "Emel anda telah disahkan! Anda kini boleh log masuk.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "verified", "email": email})
		return
	}
	if err := a.Users.SetVerified(r.Context(), user.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	slog.Info("email_verified", "user", user.ID)
	if r.Method == http.MethodGet {
		writeVerifyPage(w, "Emel anda telah disahkan! Anda kini boleh log masuk.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "verified", "email": email})
}

// writeVerifyPage renders a minimal self-contained confirmation page for GET
// (no JS, no assets) so a click straight from an email client works anywhere.
func writeVerifyPage(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<!doctype html><html><body style="font-family:system-ui,sans-serif;background:#0b1020;color:#e6e9ef;display:flex;align-items:center;justify-content:center;height:100vh;margin:0">
<div style="text-align:center;max-width:480px;padding:32px">
<h1 style="color:#5e6ad2">JomForm</h1>
<p style="font-size:18px">%s</p>
<p style="font-size:14px;color:#9aa3b2">Tekan <a href="/api/auth/login" style="color:#5e6ad2">here</a> untuk log masuk.</p>
</div></body></html>`, htmlEsc(msg))
}

// htmlEsc escapes text for injection-safe HTML rendering.
func htmlEsc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;")
	return r.Replace(s)
}

// obfuscateEmail masks an address for logs while keeping it greppable, e.g.
// "wan***@jomform.com" — logs the per-IP register attempt but not the full PII
// (issue #42 operational guardrail).
func obfuscateEmail(email string) string {
	at := strings.Index(email, "@")
	if at <= 1 {
		return "***"
	}
	local := email[:at]
	domain := email[at:]
	if len(local) <= 3 {
		return local[:1] + "***" + domain
	}
	return local[:3] + "***" + domain
}
