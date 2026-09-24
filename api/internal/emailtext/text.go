// Package emailtext — THE transactional-email dictionary.
//
// ONE PLACE decides what a JomForm transactional email says in each supported
// language. Every sender in the repo (auth mailer, workspace invites, feedback
// notifications, buyer receipts, per-form notifications) renders its subject and
// body through Text/Phrase here, so "the email follows the right language" is a
// property of the system rather than of whichever file happened to be edited
// last.
//
// # The two language axes (never conflate them)
//
//   - A PLATFORM USER's own choice: `users.lang` (en|ms|zh|ta|ar, "" when never
//     set). Governs mail addressed to a platform user: verify, magic link,
//     password reset, 2FA backup code, 2FA activated, social connect, feedback
//     replies. An unset or unknown value resolves to ENGLISH, never Malay.
//   - The PUBLIC FORM's language: `form.config.lang ?? workspace_settings.
//     default_lang`. Governs mail addressed to a buyer: receipts, the
//     merchant's receipt copy, per-form notifications. It is a workspace/form
//     setting and never the merchant's personal `users.lang`.
//
// A third case must be handled honestly: a GUEST BUYER has no profile at all, so
// a receipt resolves from the form/workspace axis — there is no user to ask.
//
// # Placeholders
//
// A body may contain `{name}` tokens, which Text fills from its vars map. A body
// that carries NO vars (the receipt and notification templates) keeps its
// merchant-facing `{{ref_code}}` / `{{amount}}` placeholders untouched — those
// belong to the receipt/notification template engine, which runs later. fill()
// skips a doubled `{{…}}` run precisely for that reason, so the two engines can
// share a file without one corrupting the other's syntax.
package emailtext

import "strings"

// The five supported locales. "en" is the documented default everywhere in the
// product (the dashboard, the public form renderer and this dictionary all fall
// back to it), so an unset or unknown code NEVER resolves to Malay.
const (
	En = "en"
	Ms = "ms"
	Zh = "zh"
	Ta = "ta"
	Ar = "ar"
)

// Langs is the supported locale set, in dictionary order.
var Langs = []string{En, Ms, Zh, Ta, Ar}

// NormalizeLang maps any language code onto one of the five supported locales.
//
// An empty string ("never set" — the common case for accounts created before the
// per-profile language shipped), an unknown code, and a code outside the set all
// resolve to English. A regional tag resolves through its primary subtag
// ("ms-MY" → "ms", "zh-CN" → "zh") so an Accept-Language header cannot produce a
// code the catalog has no entry for.
func NormalizeLang(lang string) string {
	code := strings.ToLower(strings.TrimSpace(lang))
	if i := strings.IndexByte(code, '-'); i > 0 {
		code = code[:i]
	}
	if i := strings.IndexByte(code, '_'); i > 0 {
		code = code[:i]
	}
	switch code {
	case En, Ms, Zh, Ta, Ar:
		return code
	}
	return En
}

// IsRTL reports whether lang is written right-to-left. Arabic is the only RTL
// locale in the supported set (the public form renderer already sets dir="rtl"
// for it at httpapi/menu.go; the branded email wrapper follows the same rule).
func IsRTL(lang string) bool { return NormalizeLang(lang) == Ar }

// Supported reports whether code is one of the five locales, WITHOUT
// normalising it. NormalizeLang cannot answer this: it maps an unknown code to
// English, so "fr" and "en" are indistinguishable afterwards. Callers that
// RESOLVE a language from a chain (form override ?? workspace default ?? en)
// need the distinction to know whether a step produced a usable value or
// whether the chain must fall through to the next one.
func Supported(code string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case En, Ms, Zh, Ta, Ar:
		return true
	}
	return false
}

// Kind identifies one transactional email. Every kind is a subject + a body in
// each of the five locales.
type Kind string

const (
	// Platform-user mail (resolved from the recipient's users.lang).
	VerifyEmail     Kind = "verify_email"
	PasswordReset   Kind = "password_reset"
	MagicLinkLogin  Kind = "magic_link_login"
	MagicLinkSignup Kind = "magic_link_signup"
	Email2FACode    Kind = "email_2fa_code"
	TwoFAActivated  Kind = "two_fa_activated"
	SocialConnect   Kind = "social_connect"
	WorkspaceInvite Kind = "workspace_invite"
	FeedbackNew     Kind = "feedback_new_thread"
	FeedbackReply   Kind = "feedback_reply"

	// Buyer-facing mail (resolved from the form/workspace axis). The merchant's
	// copy of a receipt is NOT a separate kind: it is the receipt template with
	// PhraseMerchantPrefix prepended to the subject, so there is one receipt body
	// to translate rather than two that can drift apart.
	Receipt              Kind = "receipt"
	NotificationSale     Kind = "notification_sale"
	NotificationResponse Kind = "notification_response"
)

// PhraseKey identifies a standalone string inside the shared branded wrapper.
type PhraseKey string

const (
	// PhraseDisclaimer is the footer sentence explaining that an unrequested
	// email can be ignored.
	PhraseDisclaimer PhraseKey = "branded_disclaimer"
	// PhraseCopyright is the footer copyright line.
	PhraseCopyright PhraseKey = "branded_copyright"
	// PhraseMerchantPrefix prefixes a receipt subject on the merchant's copy,
	// so the merchant can tell it apart from the buyer's identical receipt.
	PhraseMerchantPrefix PhraseKey = "merchant_prefix"
	// PhraseNoSubject stands in for a feedback thread whose filer left the
	// subject empty (the UI makes it optional, so it is often empty).
	PhraseNoSubject PhraseKey = "no_subject"
)

// Message is one rendered email: the language it was rendered in, plus its
// subject and body. Lang is carried so a test (and an operator reading a log)
// can assert which language a message actually went out in.
type Message struct {
	Lang    string
	Subject string
	Body    string
}

// Text renders a kind in lang. An unset/unknown lang resolves to English. vars
// fills the body's `{name}` tokens; a nil map leaves the body verbatim (the
// receipt and notification kinds rely on that — their `{{ref_code}}`-style
// placeholders belong to the receipt/notification template engine).
func Text(lang string, kind Kind, vars map[string]string) Message {
	l := NormalizeLang(lang)
	byLang, ok := catalog[kind]
	if !ok {
		// An unknown kind is a programming error, not a language question:
		// return an empty message rather than inventing content, so a caller
		// that renders garbage is visible instead of plausible.
		return Message{Lang: l}
	}
	msg, ok := byLang[l]
	if !ok {
		msg = byLang[En]
	}
	return Message{Lang: l, Subject: fill(msg.Subject, vars), Body: fill(msg.Body, vars)}
}

// Phrase returns a standalone localised string.
func Phrase(lang string, key PhraseKey) string {
	l := NormalizeLang(lang)
	if s, ok := phrases[key][l]; ok {
		return s
	}
	return phrases[key][En]
}

// Kinds returns every kind in the catalog, so a guard can iterate the whole
// dictionary instead of a hand-kept list that drifts.
func Kinds() []Kind {
	out := make([]Kind, 0, len(catalog))
	for k := range catalog {
		out = append(out, k)
	}
	return out
}

// The built-in templates the PRE-LANGUAGE release wrote into the database, and
// which every never-customised row still carries:
//
//   - workspace_email_settings.receipt_subject / receipt_body default to this
//     literal (migration 000021), so a workspace nobody customised holds Malay
//     and would keep mailing Malay in every language.
//   - the default notification rule EnsureDefault installs for a new form
//     (000032 / #597) is stored per row in the same way.
//
// A stored value that still matches one of these is not a merchant decision: it
// is the system's own default text, and the sender must render the language-
// aware current default instead. Anything else is the merchant's own words and
// is never translated.
var legacyDefaults = map[Kind]msg{
	Receipt: {
		Subject: "Resit pembayaran — {{ref_code}}",
		Body: "Terima kasih atas pembayaran anda!\n\n" +
			"Resit pembayaran JomForm\n" +
			"Rujukan : {{ref_code}}\n" +
			"Jumlah  : RM {{amount}}\n" +
			"Item    : {{items}}\n" +
			"Status  : Pembayaran diterima\n\n" +
			"Simpan emel ini sebagai resit rasmi anda.\n" +
			"Borang dibeli: {{form_url}}",
	},
	NotificationSale: {
		Subject: "Jualan baharu — {{ref_code}}",
		Body: "Jualan baharu diterima!\n\n" +
			"Rujukan : {{ref_code}}\n" +
			"Jumlah  : RM {{amount}}\n" +
			"Item    : {{items}}\n" +
			"Pembeli : {{buyer_name}}\n" +
			"Borang  : {{form_url}}",
	},
	NotificationResponse: {
		Subject: "Pendaftaran baharu diterima",
		Body: "Pendaftaran baharu diterima!\n\n" +
			"Nama   : {{full_name}}\n" +
			"Emel   : {{email}}\n" +
			"Borang : {{form_url}}\n" +
			"Tarikh : {{date}}",
	},
}

// LegacyDefault returns the built-in template the PRE-LANGUAGE release wrote into
// the database for kind, byte for byte — the Malay literal the row still carries
// in a workspace or notification nobody ever customised.
//
// It is exported because the WRITER of those rows needs the same bytes the
// READER recognises: dbstore.EnsureDefault stores the default rule for a new
// form, and if it held its own copy of this text the two could drift and
// IsSystemDefault would stop recognising what the database actually holds. One
// definition, two consumers — a merchant's own wording is never at risk, because
// only a value that still equals this literal is treated as the system's.
//
// Returns ("", "") for a kind the pre-language release never wrote.
func LegacyDefault(kind Kind) (subject, body string) {
	m, ok := legacyDefaults[kind]
	if !ok {
		return "", ""
	}
	return m.Subject, m.Body
}

// SubjectIsSystemDefault / BodyIsSystemDefault are the PER-FIELD form of
// IsSystemDefault, and they are what a partially-edited row needs.
//
// A merchant edits a subject and a body in one form, but they are two separate
// decisions: the settings page loads both stored values and posts both back on
// save, so editing only the body writes the untouched system subject back into
// the row verbatim. Judging that row as a PAIR (IsSystemDefault) then answers
// "neither is the system's text", and the Malay subject the pre-language release
// wrote ships to a buyer whose form is English.
//
// Each field is therefore classified on its own. A merchant's own wording is
// still never rewritten: only a value that equals a built-in template FOR ITS
// OWN HALF (one of the five locale renderings, or the pre-language literal) is
// treated as the system's, and everything else is passed through untouched —
// which is what makes the mixed row render "merchant subject + localised body"
// with neither half overwritten.
//
// A kind the dictionary does not carry (or a pre-language default it has none
// for) has no built-in text at all, so neither half can be one.
func SubjectIsSystemDefault(kind Kind, subject string) bool {
	if lg, ok := legacyDefaults[kind]; ok && lg.Subject == subject {
		return true
	}
	for _, m := range catalog[kind] {
		if m.Subject == subject {
			return true
		}
	}
	return false
}

// BodyIsSystemDefault is the body half; see SubjectIsSystemDefault.
func BodyIsSystemDefault(kind Kind, body string) bool {
	if lg, ok := legacyDefaults[kind]; ok && lg.Body == body {
		return true
	}
	for _, m := range catalog[kind] {
		if m.Body == body {
			return true
		}
	}
	return false
}

// IsSystemDefault reports whether a stored subject+body PAIR is still a BUILT-IN
// template for kind — either the current default in one of the five locales, or
// the pre-language literal the database defaults carry (legacyDefaults).
//
// It answers the whole-row question, so a row with even one edited half is NOT a
// system default by this test. A caller that must survive a partially-edited row
// (a subject untouched beside a body the merchant rewrote, or the reverse) asks
// SubjectIsSystemDefault / BodyIsSystemDefault instead, field by field — see
// their comment.
//
// A caller uses it to answer one question: "did a merchant write this, or is it
// the system's own text?" Only the former is sacrosanct — a built-in template
// must follow the recipient's language, and a merchant's own wording must never
// be rewritten under him. The two are otherwise indistinguishable in the
// database, because the never-customised row holds the same literal a merchant
// would get by typing the default back in.
func IsSystemDefault(kind Kind, subject, body string) bool {
	if lg, ok := legacyDefaults[kind]; ok && lg.Subject == subject && lg.Body == body {
		return true
	}
	byLang, ok := catalog[kind]
	if !ok {
		return false
	}
	for _, m := range byLang {
		if m.Subject == subject && m.Body == body {
			return true
		}
	}
	return false
}

// Locales returns the locales the dictionary carries for kind. It is the
// dictionary's own view, so a guard can prove all five exist rather than
// trusting a copied slice.
func Locales(kind Kind) []string {
	byLang, ok := catalog[kind]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(byLang))
	for _, l := range Langs {
		if _, ok := byLang[l]; ok {
			out = append(out, l)
		}
	}
	return out
}

// fill substitutes `{name}` tokens from vars. A doubled `{{…}}` run is emitted
// verbatim and skipped as a unit, so the merchant `{{ref_code}}` placeholders of
// the receipt/notification templates survive this function untouched (they are
// resolved later by the template engine that owns them).
func fill(tmpl string, vars map[string]string) string {
	if len(vars) == 0 || !strings.ContainsRune(tmpl, '{') {
		return tmpl
	}
	var b strings.Builder
	b.Grow(len(tmpl))
	for i := 0; i < len(tmpl); {
		if tmpl[i] != '{' {
			b.WriteByte(tmpl[i])
			i++
			continue
		}
		if i+1 < len(tmpl) && tmpl[i+1] == '{' {
			// A doubled brace belongs to the other template engine.
			j := strings.Index(tmpl[i:], "}}")
			if j < 0 {
				b.WriteString(tmpl[i:])
				break
			}
			b.WriteString(tmpl[i : i+j+2])
			i += j + 2
			continue
		}
		j := strings.IndexByte(tmpl[i:], '}')
		if j < 0 {
			b.WriteString(tmpl[i:])
			break
		}
		key := tmpl[i+1 : i+j]
		if v, ok := vars[key]; ok {
			b.WriteString(v)
			i += j + 1
			continue
		}
		b.WriteString(tmpl[i : i+j+1])
		i += j + 1
	}
	return b.String()
}
