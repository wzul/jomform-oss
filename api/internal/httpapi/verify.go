// Email confirmation (issue #42): new accounts are verified=false until they
// confirm their address via a signed link. This file wires the /v1/auth/verify
// endpoint and the register-time email send.
package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/wzul/jomform/api/internal/auth"
	emailpkg "github.com/wzul/jomform/api/internal/email"
	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/httpapi/middleware"
)

// sendVerifyEmail mints a signed 24h verification token and emails the link to
// the just-registered user. The mailer being nil (bare-mode tests, or SES
// unconfigured) yields a clear error so the caller can log but not crash.
//
// Issue #56: before emailing, the suppression list is consulted — a
// suppressed address never receives another verification send. On a
// successful send the verification breaker's send counter is bumped.
//
// Issue #206: the lang (the user's chosen locale from the signup form) is
// carried on the verify link as a ?lang= param so the confirmation page can
// render in the user's language.
// Issue #199: when an EmailQueue is wired, the fully-rendered email is enqueued
// and sent by a background worker — the register handler returns immediately.
// When the queue is nil (bare-mode tests) the send happens synchronously.
func (a *AuthHandlers) sendVerifyEmail(ctx context.Context, userID, email, lang string) error {
	if a.VerifyMailer == nil {
		return fmt.Errorf("verify mailer not configured")
	}
	if len(a.VerifyKey) != 32 {
		return fmt.Errorf("verify key not configured")
	}
	if a.Suppressions != nil {
		sup, err := emailpkg.ShouldSuppress(ctx, a.Suppressions, email)
		if err != nil {
			return fmt.Errorf("suppression check: %w", err)
		}
		if sup {
			slog.Warn("verify_email_suppressed_skipped", "user", userID)
			recordSESSuppression("verify")
			return nil // suppressed: don't resend, don't surface as a hard error
		}
	}
	token, err := auth.SignVerifyToken(a.VerifyKey, email)
	if err != nil {
		return err
	}
	link := strings.TrimRight(a.BaseURL, "/") + "/v1/auth/verify?token=" + token
	if lang != "" {
		link += "&lang=" + url.QueryEscape(lang)
	}
	body := "Sila sahkan alamat emel anda untuk mengaktifkan akaun JomForm anda.\n\n" +
		"Tap pautan berikut (sah selama 24 jam):\n\n" +
		link + "\n\n" +
		"Jika anda tidak mendaftar akaun JomForm, anda boleh abaikan emel ini.\n"
	// Issue #199: background path — enqueue and return immediately.
	if a.EmailQueue != nil {
		if err := a.EmailQueue.Enqueue(ctx, emailqueue.Job{
			Type: "verify", To: email,
			Subject: "Sahkan emel JomForm anda", Body: body,
			AccountID: userID,
		}); err != nil {
			return err
		}
		return nil
	}
	if err := a.VerifyMailer.Send(email, "Sahkan emel JomForm anda", body); err != nil {
		return err
	}
	recordSESSend("verify")
	if a.Breaker != nil {
		if err := a.Breaker.RecordSend(ctx); err != nil {
			slog.Warn("breaker_record_send", "err", err)
		}
	}
	return nil
}

// handleVerify confirms a user's email from a signed token (?token=...).
// GET renders a small confirmation page; POST returns JSON. Both accept the
// same token so an app can complete verification after a fetch.
//
// Issue #206: the confirmation page is language-aware. The locale is resolved
// from the ?lang= param (carried from the signup form's language choice), then
// the Accept-Language header, defaulting to English when unknown.
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
	lang := detectVerifyLang(r)
	if user.Verified {
		// Idempotent: already verified.
		if r.Method == http.MethodGet {
			writeVerifyPage(w, lang, a.loginURL())
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
		writeVerifyPage(w, lang, a.loginURL())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "verified", "email": email})
}

// supportedVerifyLangs are the 5 locales the verify page can render in.
var supportedVerifyLangs = map[string]bool{"en": true, "ms": true, "zh": true, "ta": true, "ar": true}

// detectVerifyLang resolves the user's locale for the verify page. It prefers
// the ?lang= param (carried from the signup form's language choice), then the
// Accept-Language header, and defaults to English when neither yields a
// supported locale (issue #206).
func detectVerifyLang(r *http.Request) string {
	if l := r.URL.Query().Get("lang"); supportedVerifyLangs[l] {
		return l
	}
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		code := strings.TrimSpace(part)
		if i := strings.IndexByte(code, ';'); i >= 0 {
			code = code[:i]
		}
		code = strings.ToLower(strings.TrimSpace(code))
		if code == "" {
			continue
		}
		// Match the primary subtag (e.g. "zh-CN" → "zh", "ms-MY" → "ms").
		base := code
		if i := strings.IndexByte(code, '-'); i >= 0 {
			base = code[:i]
		}
		if supportedVerifyLangs[base] {
			return base
		}
	}
	return "en"
}

// loginURL returns the web login page URL. The verify page is served from the
// API host, so the "log in" link must point at the app's login route (issue
// #189) rather than the POST-only /api/auth/login endpoint, which returns 405
// on a browser GET. Falls back to a relative /login when no app base is set.
func (a *AuthHandlers) loginURL() string {
	if a.BaseURL == "" {
		return "/login"
	}
	return strings.TrimRight(a.BaseURL, "/") + "/login"
}

// verifyPageText holds the localized message + login-link sentence for each of
// the 5 supported locales (issue #206). The login link is split around the
// anchor so the sentence reads naturally in each language.
var verifyPageText = map[string]struct{ msg, before, link, after string }{
	"en": {"Your email has been verified! You can now log in.", "Click ", "here", " to log in."},
	"ms": {"Emel anda telah disahkan! Anda kini boleh log masuk.", "Klik ", "di sini", " untuk log masuk."},
	"zh": {"您的邮箱已验证！您现在可以登录。", "点击", "此处", "登录。"},
	"ta": {"உங்கள் மின்னஞ்சல் சரிபார்க்கப்பட்டது! இப்போது நீங்கள் உள்நுழையலாம்.", "உள்நுழைய ", "இங்கே", " கிளிக் செய்யவும்."},
	"ar": {"تم التحقق من بريدك الإلكتروني! يمكنك الآن تسجيل الدخول.", "انقر ", "هنا", " لتسجيل الدخول."},
}

// writeVerifyPage renders a minimal self-contained confirmation page for GET
// (no JS, no assets) so a click straight from an email client works anywhere.
// The message + login link render in the user's language (issue #206); an
// unknown lang falls back to English.
func writeVerifyPage(w http.ResponseWriter, lang, loginURL string) {
	text, ok := verifyPageText[lang]
	if !ok {
		text = verifyPageText["en"]
		lang = "en"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<!doctype html><html lang="%s"><body style="font-family:system-ui,sans-serif;background:#0b1020;color:#e6e9ef;display:flex;align-items:center;justify-content:center;height:100vh;margin:0">
<div style="text-align:center;max-width:480px;padding:32px">
<h1 style="color:#5e6ad2">JomForm</h1>
<p style="font-size:18px">%s</p>
<p style="font-size:14px;color:#9aa3b2">%s<a href="%s" style="color:#5e6ad2">%s</a>%s</p>
</div></body></html>`, htmlEsc(lang), htmlEsc(text.msg), htmlEsc(text.before), htmlEsc(loginURL), htmlEsc(text.link), htmlEsc(text.after))
}

// htmlEsc escapes text for injection-safe HTML rendering.
func htmlEsc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;")
	return r.Replace(s)
}

// jsSingleQuoted escapes a value for interpolation inside a SINGLE-QUOTED
// JavaScript string literal in an inline <script> (issue #537).
//
// HTML escaping is not sufficient for that context: `'` would terminate the
// literal and let the remainder run as code. Anything that is not a safe
// identifier character is emitted as a \uXXXX escape, so the result can be
// placed between single quotes without terminating or escaping the literal.
// This also neutralises `</script>` (the `<` becomes \u003c), which would
// otherwise end the script block early.
//
// Only used for values the page interpolates into JS; HTML text uses htmlEsc.
func jsSingleQuoted(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.', r == '/':
			b.WriteRune(r)
		default:
			// \uXXXX keeps the literal intact for every other rune.
			if r <= 0xFFFF {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				// Supplementary plane: encode as a surrogate pair.
				r -= 0x10000
				fmt.Fprintf(&b, `\u%04x\u%04x`, 0xD800+(r>>10), 0xDC00+(r&0x3FF))
			}
		}
	}
	return b.String()
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
