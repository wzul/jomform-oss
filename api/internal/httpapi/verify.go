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
	"github.com/wzul/jomform/api/internal/emailtext"
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
	if mailerUnavailable(a.VerifyMailer) {
		return a.logVerifyLinkWhenNoMailer(userID, email, lang)
	}
	if len(a.VerifyKey) != 32 {
		return fmt.Errorf("verify key not configured")
	}
	if a.Suppressions != nil {
		sup, err := emailpkg.ShouldSuppress(ctx, a.Suppressions, email)
		if err != nil {
			// Fail open (see (*email.Sender).suppressed): a store blip must not
			// strand a registering user's verify link. The pre-send choke point
			// repeats this check, so the send is still guarded there.
			slog.Error("email_suppression_check_error", "to", obfuscateEmail(email), "err", err)
		} else if sup {
			slog.Warn("verify_email_suppressed_skipped", "user", userID)
			recordSESSuppression("verify")
			return nil // suppressed: don't resend, don't surface as a hard error
		}
	}
	link, l, err := a.verifyLink(email, lang)
	if err != nil {
		return err
	}
	msg := emailtext.Text(l, emailtext.VerifyEmail, map[string]string{"link": link})
	// Issue #199: background path — enqueue and return immediately.
	if a.EmailQueue != nil {
		if err := a.EmailQueue.Enqueue(ctx, emailqueue.Job{
			Type: "verify", To: email,
			Subject: msg.Subject, Body: msg.Body, Lang: msg.Lang,
			AccountID: userID,
		}); err != nil {
			return err
		}
		return nil
	}
	if err := a.VerifyMailer.Send(email, msg.Subject, msg.Body, msg.Lang); err != nil {
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

// verifyLink mints the confirmation URL for an address, and returns the locale
// the message is (or would be) written in.
//
// ONE builder for both delivery paths — the mailed link and the one a
// self-hosted deployment logs (logVerifyLinkWhenNoMailer) — so the two can
// never drift apart in the token, the base URL or the ?lang= param.
//
// Issue #206: the lang rides on the link as ?lang= so the confirmation page
// (writeVerifyPage) renders in the language the EMAIL was written in. It is the
// resolved code, not the raw argument, so the page and the message can never
// disagree about which language the user was addressed in.
func (a *AuthHandlers) verifyLink(email, lang string) (string, string, error) {
	if len(a.VerifyKey) != 32 {
		return "", "", fmt.Errorf("verify key not configured")
	}
	token, err := auth.SignVerifyToken(a.VerifyKey, email)
	if err != nil {
		return "", "", err
	}
	l := emailtext.NormalizeLang(lang)
	link := strings.TrimRight(a.BaseURL, "/") + "/v1/auth/verify?token=" + token +
		"&lang=" + url.QueryEscape(l)
	return link, l, nil
}

// logVerifyLinkWhenNoMailer is the SELF-HOSTED delivery path: the deployment has
// no email sender at all (the sender is AWS SES — SES_SMTP_HOST/SES_FROM — so a
// box without AWS credentials cannot mail anything), and an account that is
// created unverified can never log in, because the only copy of the
// confirmation link is a message that was never sent.
//
// With JOMFORM_LOG_VERIFY_LINK=1 the link is written to the API log instead, so
// `docker compose logs api` is enough to finish the first signup. Deliberately
// opt-in: a link in the logs is an account-activation token readable by anyone
// who can read them, which is why it is only reachable when no mailer exists
// (a deployment with working email never takes this branch) and why a
// deployment without the switch keeps the old behaviour — a recorded failure,
// never a silent no-op.
func (a *AuthHandlers) logVerifyLinkWhenNoMailer(userID, email, lang string) error {
	if !a.LogVerifyLink {
		return fmt.Errorf("verify mailer not configured")
	}
	link, _, err := a.verifyLink(email, lang)
	if err != nil {
		return err
	}
	slog.Warn("verify_link_logged_instead_of_emailed",
		"user", userID,
		"to", obfuscateEmail(email),
		"link", link,
		"why", "no email sender is configured and JOMFORM_LOG_VERIFY_LINK is on: this link is the only copy, open it to confirm the account")
	return nil
}

// handleVerify confirms a user's email from a signed token (?token=...).
// GET renders a small confirmation page; POST returns JSON. Both accept the
// same token so an app can complete verification after a fetch.
//
// Issue #206: the confirmation page is language-aware. The locale is resolved
// from the ?lang= param (carried from the signup form's language choice), then
// the Accept-Language header, defaulting to English when unknown.
//
// The GET/JSON split holds on the FAILURE paths too (see verifyFail): a GET
// renders a localized page for every link problem, because the caller is a
// human who clicked an emailed link in a browser, not an API client.
func (a *AuthHandlers) handleVerify(w http.ResponseWriter, r *http.Request) {
	// Resolved up front so a GET failure can render in the caller's language
	// too — the failures below used to answer before the locale was read.
	lang := detectVerifyLang(r)
	token := r.URL.Query().Get("token")
	if token == "" {
		// Own wording for THIS surface (issue #6177 follow-up) — see
		// CodeVerifyTokenMissing in errcode.go.
		a.verifyFail(w, r, lang, http.StatusBadRequest, verifyFailMissing, "verification token is required")
		return
	}
	if len(a.VerifyKey) != 32 {
		// Deliberately NOT a page, even on GET (the one failure class that
		// keeps the JSON envelope for both methods): the deployment cannot
		// mint a verification link at all, so a page telling the user to
		// "request a new link" would promise something this server cannot do.
		// It is a misconfiguration signal, unreachable in a correctly
		// configured deployment (main.go derives VerifyKey from
		// ENCRYPTION_KEY), and the JSON envelope is what says so.
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	email, err := auth.VerifyToken(a.VerifyKey, token)
	if err != nil {
		slog.Warn("verify_token_invalid", "err", err, "ip", middleware.ClientIP(r))
		a.verifyFail(w, r, lang, http.StatusBadRequest, verifyFailExpired, "the verification link is invalid or has expired")
		return
	}
	user, uerr := a.Users.ByEmail(r.Context(), email)
	if uerr != nil || user == nil {
		a.verifyFail(w, r, lang, http.StatusBadRequest, verifyFailUnknown, "invalid verification link")
		return
	}
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
	// THE SUCCESS PAGE CARRIES ITS OWN DIRECTION (card t_75ce0ac6). The dashboard
	// sets `document.documentElement.dir` from the language, but this page is the
	// ONE surface a user reaches straight from an email client — no dashboard JS
	// runs here — so an Arabic confirmation rendered RTL prose inside an LTR
	// document. The FAILURE page already set it, so the two siblings disagreed
	// about direction; both now use verifyDir.
	fmt.Fprintf(w, `<!doctype html><html lang="%s" dir="%s"><body style="font-family:system-ui,sans-serif;background:#0b1020;color:#e6e9ef;display:flex;align-items:center;justify-content:center;height:100vh;margin:0">
<div style="text-align:center;max-width:480px;padding:32px">
<h1 style="color:#5e6ad2">JomForm</h1>
<p style="font-size:18px">%s</p>
<p style="font-size:14px;color:#9aa3b2">%s<a href="%s" style="color:#5e6ad2">%s</a>%s</p>
</div></body></html>`, htmlEsc(lang), htmlEsc(verifyDir(lang)), htmlEsc(text.msg), htmlEsc(text.before), htmlEsc(loginURL), htmlEsc(text.link), htmlEsc(text.after))
}

// verifyFailClass names WHICH link failure the caller hit, so the GET page can
// say the same thing the JSON envelope says for that failure. An explicit
// parameter rather than a lookup keyed on the wire message: a reworded literal
// would silently fall back to a generic sentence, and the wire prose is
// deliberately per-surface here (issue #6177).
type verifyFailClass int

const (
	// verifyFailExpired — the token did not verify: forged, tampered, or past
	// its 24h TTL. The common real-world case (a stale link in an inbox).
	verifyFailExpired verifyFailClass = iota
	// verifyFailUnknown — the token verified but its account is gone (deleted
	// between the send and the click).
	verifyFailUnknown
	// verifyFailMissing — no ?token= at all (a hand-typed or mangled URL).
	verifyFailMissing
)

// verifyFailCopy is the localized failure page's copy: one sentence per
// failure class, plus the shared "what to do next" hint and the login-link
// label. The hint is the whole point of the page — a human who clicked a stale
// link must be told to get a fresh one, which a machine code cannot say.
type verifyFailCopy struct {
	expired string
	unknown string
	missing string
	hint    string
	action  string
}

// verifyFailText holds that copy for each of the 5 locales the verify surface
// renders in (issue #206). An unknown locale falls back to English.
var verifyFailText = map[string]verifyFailCopy{
	"en": {
		expired: "This verification link is invalid or has expired.",
		unknown: "This verification link is no longer valid.",
		missing: "This confirmation link is missing its token.",
		hint:    "Open the newest verification email we sent you, or sign in to request a new link.",
		action:  "Sign in",
	},
	"ms": {
		expired: "Pautan pengesahan ini tidak sah atau telah tamat tempoh.",
		unknown: "Pautan pengesahan ini tidak lagi sah.",
		missing: "Pautan pengesahan ini tiada tokennya.",
		hint:    "Buka emel pengesahan terbaharu yang kami hantar, atau log masuk untuk minta pautan baharu.",
		action:  "Log masuk",
	},
	"zh": {
		expired: "此验证链接无效或已过期。",
		unknown: "此验证链接已不再有效。",
		missing: "此确认链接缺少令牌。",
		hint:    "请打开我们发送的最新验证邮件，或登录以获取新链接。",
		action:  "登录",
	},
	"ta": {
		expired: "இந்த சரிபார்ப்பு இணைப்பு தவறானது அல்லது காலாவதியானது.",
		unknown: "இந்த சரிபார்ப்பு இணைப்பு இனி செல்லுபடியாகாது.",
		missing: "இந்த உறுதிப்படுத்தல் இணைப்பில் டோக்கன் இல்லை.",
		hint:    "நாங்கள் அனுப்பிய சமீபத்திய சரிபார்ப்பு மின்னஞ்சலைத் திறக்கவும், அல்லது புதிய இணைப்பைக் கோர உள்நுழையவும்.",
		action:  "உள்நுழை",
	},
	"ar": {
		expired: "رابط التحقق هذا غير صالح أو منتهي الصلاحية.",
		unknown: "رابط التحقق هذا لم يعد صالحاً.",
		missing: "رابط التأكيد هذا تفتقد رمزه.",
		hint:    "افتح أحدث بريد تحقق أرسلناه إليك، أو سجّل الدخول لتطلب رابطاً جديداً.",
		action:  "تسجيل الدخول",
	},
}

// verifyFailSentence picks the sentence for one failure class.
func (c verifyFailCopy) sentence(class verifyFailClass) string {
	switch class {
	case verifyFailUnknown:
		return c.unknown
	case verifyFailMissing:
		return c.missing
	default:
		return c.expired
	}
}

// verifyFail answers a verification failure. A GET — a human who clicked the
// emailed link in a browser — renders a self-contained, localized page that
// tells them to get a new link; every other method keeps the machine JSON
// envelope for app/API callers.
//
// WHY THIS EXISTS. The success path rendered a page and every failure path
// answered `application/json`, so an expired link — the ordinary case for an
// emailed link — showed the raw envelope in the browser, in Malay prose, to a
// user whose dashboard language could be any of the five. The method split was
// only ever implemented on the happy path.
func (a *AuthHandlers) verifyFail(w http.ResponseWriter, r *http.Request, lang string, status int, class verifyFailClass, wireMsg string) {
	if r.Method != http.MethodGet {
		writeErr(w, status, wireMsg)
		return
	}
	writeVerifyFailPage(w, lang, a.loginURL(), class)
}

// verifyDir returns the text direction for a locale, for BOTH verify pages. The
// rule itself is formLang.go's isRTL (Arabic is the only RTL locale in the five
// the platform ships); this page spells it explicitly as "ltr"/"rtl" because
// its <html> tag has always carried the attribute rather than omitting it.
func verifyDir(lang string) string {
	if isRTL(lang) {
		return "rtl"
	}
	return "ltr"
}

// writeVerifyFailPage renders the GET failure page: no JS, no assets, the same
// self-contained shape as writeVerifyPage so a click straight from an email
// client works anywhere. The status stays the JSON path's status (400) — the
// page is what makes the 400 legible, not a 200 that would lie about the link.
//
// The body is deliberately kept well over 512 bytes: a browser (Chrome and
// Chromium-derived ones; measured threshold, webhint's no-friendly-error-pages
// hint: 512 bytes for 400/404/500/…, 256 for 403/405/410) REPLACES a 4xx body
// that is shorter than that with its own generic error page. A "helpful" page
// that trips that rule would be discarded, and the user would see the browser's
// error instead — the very failure this change removes.
func writeVerifyFailPage(w http.ResponseWriter, lang, loginURL string, class verifyFailClass) {
	text, ok := verifyFailText[lang]
	if !ok {
		text = verifyFailText["en"]
		lang = "en"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprintf(w, `<!doctype html><html lang="%s" dir="%s"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>JomForm</title></head>
<body style="font-family:system-ui,sans-serif;background:#0b1020;color:#e6e9ef;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0">
<div style="text-align:center;max-width:480px;padding:32px">
<h1 style="color:#5e6ad2">JomForm</h1>
<p style="font-size:18px">%s</p>
<p style="font-size:14px;color:#9aa3b2">%s</p>
<p style="font-size:14px;color:#9aa3b2"><a href="%s" style="color:#5e6ad2">%s</a></p>
</div></body></html>`, htmlEsc(lang), htmlEsc(verifyDir(lang)), htmlEsc(text.sentence(class)), htmlEsc(text.hint), htmlEsc(loginURL), htmlEsc(text.action))
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
