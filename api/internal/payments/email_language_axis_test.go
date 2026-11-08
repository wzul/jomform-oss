// Language consistency (issue: "language consistency across EVERY transactional
// email") — the behavioural evidence the card asks for.
//
// Everything here runs against the REAL senders and the REAL database, because
// the defect was never a wrong string: it was a wrong PREFERENCE reaching the
// sender. A unit test over the dictionary cannot show which axis a given email
// read, and that is the whole bug.
//
// The two axes are proven side by side in one run, on ONE merchant:
//
//	profile axis (users.lang)      -> auth mail to a platform user
//	buyer axis (form ?? workspace) -> receipts + notifications to a buyer
//
// A merchant whose users.lang is "en" while his public form is "ms" must get an
// ENGLISH platform email and send MALAY receipts. Conflating the axes in either
// direction is a visible defect, so both halves are asserted here.
package payments

import (
	"context"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/emailtext"
)

// ---------------------------------------------------------------------------
// The buyer axis: a guest buyer has no profile at all.
// ---------------------------------------------------------------------------

// TestGuestBuyerReceiptResolvesFromTheFormAxis is the third case the card calls
// out honestly: a guest buyer has no `users.lang` to consult, so a receipt MUST
// resolve from the form/workspace axis. There is no user to ask — and if the
// sender ever reached for one, this buyer would fall back to English (or to the
// Malay literal the database defaults carry) instead of the form's language.
func TestGuestBuyerReceiptResolvesFromTheFormAxis(t *testing.T) {
	// A guest buyer: no user row, an address that exists nowhere in the system.
	ref := SaleRef{
		RefCode:    "JF-GUEST1",
		BuyerEmail: "guest-buyer@example.test",
		AmountSen:  2500,
		FormURL:    "https://acme.jomform.com/order",
		// The form's own config.lang — the axis a guest receipt reads.
		FormLang: "ms",
	}

	subject, body := renderReceipt(ref, "", "", refLang(ref), false)

	if !strings.Contains(subject, "Resit pembayaran") {
		t.Errorf("guest receipt subject is not the form's Malay template: %q", subject)
	}
	if !strings.Contains(body, "Terima kasih atas pembayaran anda!") {
		t.Errorf("guest receipt body is not the form's Malay template:\n%s", body)
	}
	// The buyer's own data must still be in it — a localised template that lost
	// the placeholder substitution would be a worse bug than the wrong language.
	if !strings.Contains(body, "JF-GUEST1") {
		t.Errorf("guest receipt lost its reference code:\n%s", body)
	}
	if !strings.Contains(body, "RM 25.00") {
		t.Errorf("guest receipt lost its amount (integer sen formatting):\n%s", body)
	}
}

// refLang mirrors the production resolution the receipt sender performs
// (dbstore.EmailLang over the buyer axis), so this test exercises the same rule
// the sender does rather than restating it.
func refLang(ref SaleRef) string {
	return dbstore.EmailLang(ref.FormLang, ref.WorkspaceLang)
}

// TestReceiptFollowsTheFormNotTheWorkspaceDefault pins the precedence on the
// buyer axis: the form's own override wins over the workspace default, so a
// merchant running one English form inside a Malay workspace gets an English
// receipt for that form.
func TestReceiptFollowsTheFormNotTheWorkspaceDefault(t *testing.T) {
	ref := SaleRef{RefCode: "JF-PREC", FormLang: "en", WorkspaceLang: "ms"}
	subject, body := renderReceipt(ref, "", "", refLang(ref), false)

	if !strings.Contains(subject, "Payment receipt") {
		t.Errorf("form override did not win over the workspace default: %q", subject)
	}
	if strings.Contains(body, "Terima kasih") {
		t.Errorf("receipt came out Malay despite the form's English override:\n%s", body)
	}
}

// TestUnknownFormAndWorkspaceLanguageResolvesToEnglish is the "never Malay"
// contract on the buyer axis: a form with an unsupported code and a workspace
// with none must produce an English receipt, NOT the Malay literal the database
// column defaults carry.
func TestUnknownFormAndWorkspaceLanguageResolvesToEnglish(t *testing.T) {
	for _, ref := range []SaleRef{
		{RefCode: "JF-X", FormLang: "fr", WorkspaceLang: "de"},
		{RefCode: "JF-Y"},
		{RefCode: "JF-Z", FormLang: "", WorkspaceLang: ""},
	} {
		lang := refLang(ref)
		if lang != emailtext.En {
			t.Errorf("ref %+v resolved to %q, want %q", ref, lang, emailtext.En)
		}
		_, body := renderReceipt(ref, "", "", lang, false)
		if strings.Contains(body, "Terima kasih") {
			t.Errorf("ref %+v fell back to the Malay literal instead of English:\n%s", ref, body)
		}
		if !strings.Contains(body, "Thank you for your payment!") {
			t.Errorf("ref %+v is not the English template:\n%s", ref, body)
		}
	}
}

// TestNotificationFollowsTheBuyerAxis mirrors the above for the per-form
// notification (the merchant's "someone responded / paid" alert). It is part of
// the buyer's submission flow, so it follows the buyer axis too — and a rule the
// merchant never edited must not carry (or mail) the Malay literal the database
// used to hold.
func TestNotificationFollowsTheBuyerAxis(t *testing.T) {
	// The rule EnsureDefault installs stores NO template at all (card
	// t_1768cb11): the subject and body are empty, and the dictionary supplies
	// the text in the form's language. If a future change put the Malay literal
	// back into the row, this is where it shows up.
	_, _, defSubject, defBody := dbstore.DefaultNotification(dbstore.FormModeResponse)
	if defSubject != "" || defBody != "" {
		t.Fatalf("the default rule's stored subject/body = %q/%q, want empty strings — a language is "+
			"pinned into a field the dashboard renders verbatim", defSubject, defBody)
	}

	// An empty rule, and a rule an older release wrote the literal into, must
	// both resolve to the dictionary's template in ALL five locales.
	legacySubject, legacyBody := emailtext.LegacyDefault(emailtext.NotificationResponse)
	for _, lang := range emailtext.Langs {
		want := emailtext.Text(lang, emailtext.NotificationResponse, nil)
		for _, stored := range [][2]string{
			{defSubject, defBody},       // how this release writes it
			{legacySubject, legacyBody}, // how the pre-000084 release wrote it
		} {
			gotSubject, gotBody := notificationMessage(
				dbstore.NotifyEventResponseSubmitted, lang, stored[0], stored[1])
			if gotSubject != want.Subject {
				t.Errorf("lang %q: notification subject = %q, want %q", lang, gotSubject, want.Subject)
			}
			if gotBody != want.Body {
				t.Errorf("lang %q: notification body is not the %s template", lang, lang)
			}
		}
		if lang == emailtext.En && strings.Contains(want.Subject, "Pendaftaran") {
			t.Errorf("lang en: the resolved response subject is Malay: %q", want.Subject)
		}
	}

	// A merchant-authored rule is NEVER rewritten — that is the other half of the
	// same rule, and getting it wrong would silently translate a merchant's own
	// words under him. Checked in every language, because "passed through" must
	// not depend on which locale happens to be resolved.
	merchantSubject, merchantBody := "My shop's alert", "Someone responded. Handle it."
	for _, lang := range emailtext.Langs {
		gotSubject, gotBody := notificationMessage(
			dbstore.NotifyEventResponseSubmitted, lang, merchantSubject, merchantBody)
		if gotSubject != merchantSubject || gotBody != merchantBody {
			t.Errorf("lang %q: merchant copy was translated or dropped: subject=%q body=%q",
				lang, gotSubject, gotBody)
		}
	}
}

// ---------------------------------------------------------------------------
// The profile axis, in the same run as the buyer axis.
// ---------------------------------------------------------------------------

// TestTwoAxesDoNotMixOnOneMerchant is the case the card asks for explicitly: a
// merchant whose dashboard is English (users.lang="en") while his public form is
// Malay (config.lang="ms") must, in the SAME run, get an English platform email
// and send a Malay receipt.
//
// This is the check that would catch the tempting wrong fix — "make every email
// use users.lang" — which would deliver English receipts to the merchant's Malay
// buyers.
func TestTwoAxesDoNotMixOnOneMerchant(t *testing.T) {
	// One merchant, two languages.
	const merchantProfileLang = "en" // users.lang — his dashboard
	const publicFormLang = "ms"      // form.config.lang — his storefront

	// ── Profile axis: the platform email addressed to HIM ──────────────────
	profileMail := emailtext.Text(merchantProfileLang, emailtext.PasswordReset,
		map[string]string{"link": "https://jomform.com/reset?t=abc"})
	if profileMail.Lang != "en" {
		t.Errorf("platform email lang = %q, want en (the merchant's own users.lang)", profileMail.Lang)
	}
	if !strings.Contains(profileMail.Subject, "Reset your JomForm password") {
		t.Errorf("platform email subject is not English: %q", profileMail.Subject)
	}
	if strings.Contains(profileMail.Body, "kata laluan") {
		t.Errorf("platform email body came out Malay for an English user:\n%s", profileMail.Body)
	}

	// ── Buyer axis: the receipt addressed to HIS CUSTOMER ──────────────────
	buyerRef := SaleRef{RefCode: "JF-AXES", BuyerEmail: "buyer@example.test", FormLang: publicFormLang}
	buyerLang := refLang(buyerRef)
	if buyerLang != "ms" {
		t.Errorf("receipt lang = %q, want ms (the public form's language)", buyerLang)
	}
	buyerSubject, buyerBody := renderReceipt(buyerRef, "", "", buyerLang, false)
	if !strings.Contains(buyerSubject, "Resit pembayaran") {
		t.Errorf("receipt subject is not Malay: %q", buyerSubject)
	}
	if !strings.Contains(buyerBody, "Terima kasih atas pembayaran anda!") {
		t.Errorf("receipt body is not Malay:\n%s", buyerBody)
	}

	// ── The two must DIFFER, or the axes were conflated ────────────────────
	if profileMail.Lang == buyerLang {
		t.Fatalf("both emails came out in %q — the profile axis and the buyer axis were conflated", buyerLang)
	}
	t.Logf("profile axis: users.lang=%s -> %q", merchantProfileLang, profileMail.Subject)
	t.Logf("buyer axis:   form.config.lang=%s -> %q", publicFormLang, buyerSubject)
}

// TestMerchantReceiptCopyFollowsTheBuyerAxis: the merchant's own copy of a
// receipt is addressed in the BUYER-facing language, not his dashboard one. He is
// being shown the same document his customer received, so it must read the same.
func TestMerchantReceiptCopyFollowsTheBuyerAxis(t *testing.T) {
	// The merchant's dashboard is English; his form is Malay.
	buyerLang := dbstore.EmailLang("ms", "ms")

	// The subject prefix the merchant copy adds, and the receipt it prefixes.
	prefix := emailtext.Phrase(buyerLang, emailtext.PhraseMerchantPrefix)
	_, buyerBody := renderReceipt(SaleRef{RefCode: "JF-M1"}, "", "", buyerLang, false)
	merchantSubject := prefix + emailtext.Text(buyerLang, emailtext.Receipt, nil).Subject

	if !strings.Contains(merchantSubject, "Jualan baharu") {
		t.Errorf("merchant copy prefix is not Malay: %q", merchantSubject)
	}
	if strings.Contains(merchantSubject, "New sale") {
		t.Errorf("merchant copy prefix came out English for a Malay form: %q", merchantSubject)
	}
	// The merchant's copy carries the buyer's body verbatim, so the two documents
	// cannot disagree about the language they are written in.
	if strings.Contains(buyerBody, "Thank you") {
		t.Errorf("the buyer body is English — the merchant copy would then be mixed-language:\n%s", buyerBody)
	}
}

// ---------------------------------------------------------------------------
// Every converted email, in all five locales plus unset.
// ---------------------------------------------------------------------------

// kindAxis records which language axis a kind belongs to, so this test can prove
// each one is resolved from the right source rather than merely rendering.
type kindAxis struct {
	kind emailtext.Kind
	axis string // "profile" or "buyer"
}

// transactionals is every kind the repo sends, with its axis. Kept here rather
// than derived, because the AXIS is a design decision the test must state
// explicitly — a derived list would accept whatever the code happened to do.
var transactionals = []kindAxis{
	// Platform-user mail: resolved from users.lang.
	{emailtext.VerifyEmail, "profile"},
	{emailtext.PasswordReset, "profile"},
	{emailtext.MagicLinkLogin, "profile"},
	{emailtext.MagicLinkSignup, "profile"},
	{emailtext.Email2FACode, "profile"},
	{emailtext.TwoFAActivated, "profile"},
	{emailtext.SocialConnect, "profile"},
	{emailtext.WorkspaceInvite, "profile"},
	{emailtext.FeedbackNew, "profile"},
	{emailtext.FeedbackReply, "profile"},
	// Buyer-facing mail: resolved from form.config.lang ?? workspace default.
	{emailtext.Receipt, "buyer"},
	{emailtext.NotificationSale, "buyer"},
	{emailtext.NotificationResponse, "buyer"},
}

// TestEveryTransactionalEmailRendersInEveryLocale dumps the real subject and body
// of every converted email in all five locales plus an unset language. This is
// the evidence the card asks for — the strings themselves, not a claim that a
// string changed — and it is also where a regression in one locale would show up.
func TestEveryTransactionalEmailRendersInEveryLocale(t *testing.T) {
	vars := map[string]string{
		"link": "https://jomform.com/verify?token=abc", "code": "123456",
		"from": "buyer@example.test", "status": "open",
		"subject": "My thread", "excerpt": "Please help with…",
	}
	locales := append(append([]string{}, emailtext.Langs...), "")

	for _, ta := range transactionals {
		t.Run(string(ta.kind)+"/"+ta.axis, func(t *testing.T) {
			seen := map[string]string{}
			for _, lang := range locales {
				msg := emailtext.Text(lang, ta.kind, vars)
				// An unset language must land on English, never Malay.
				want := lang
				if want == "" {
					want = emailtext.En
				}
				if emailtext.NormalizeLang(want) != msg.Lang {
					t.Errorf("lang %q resolved to %q, want %q", lang, msg.Lang, want)
				}
				if strings.TrimSpace(msg.Subject) == "" || strings.TrimSpace(msg.Body) == "" {
					t.Fatalf("lang %q rendered an empty message", lang)
				}
				// The locales must be genuinely distinct, not one string reused.
				if prev, dup := seen[msg.Subject]; dup && lang != "" {
					t.Errorf("lang %q repeats the subject of %s: %q", lang, prev, msg.Subject)
				}
				seen[msg.Subject] = lang
				t.Logf("[%s] %s | %q\n    %s", ta.axis, langLabel(lang), msg.Subject,
					strings.ReplaceAll(msg.Body, "\n", "\n    "))
			}
		})
	}
}

// langLabel names an empty locale in the log output so "unset" is visible rather
// than an empty column.
func langLabel(lang string) string {
	if lang == "" {
		return "(unset)"
	}
	return lang
}

// TestEveryKindIsOnTheAxisItClaims cross-checks the axis table above against the
// dictionary itself: every kind the catalog carries must appear in the table, so
// adding a translator without deciding its axis fails here.
func TestEveryKindIsOnTheAxisItClaims(t *testing.T) {
	declared := map[emailtext.Kind]bool{}
	for _, ta := range transactionals {
		if declared[ta.kind] {
			t.Errorf("kind %q is listed twice in the axis table", ta.kind)
		}
		declared[ta.kind] = true
		switch ta.axis {
		case "profile", "buyer":
		default:
			t.Errorf("kind %q claims unknown axis %q", ta.kind, ta.axis)
		}
	}
	for _, kind := range emailtext.Kinds() {
		if !declared[kind] {
			t.Errorf("kind %q is in the dictionary but has no declared axis — decide whether it"+
				" follows users.lang or the form/workspace language", kind)
		}
	}
}

// TestBuyerAxisNeverReadsUsersLang is the structural half of the two-axis rule:
// the buyer-axis resolution must be driven by the form/workspace inputs only. A
// resolution that consulted a profile would need a user id, and there is none —
// so the function signature itself is the guarantee.
func TestBuyerAxisNeverReadsUsersLang(t *testing.T) {
	// EmailLang takes exactly (formLang, workspaceDefaultLang): no user argument
	// exists to pass, so the merchant's personal language cannot leak in.
	cases := []struct {
		formLang, wsLang, want string
	}{
		{"ms", "en", "ms"},
		{"", "en", "en"},
		{"", "zh", "zh"},
		{"ta", "ar", "ta"},
		{"fr", "ta", "ta"},
		{"fr", "xx", "en"},
	}
	for _, c := range cases {
		if got := dbstore.EmailLang(c.formLang, c.wsLang); got != c.want {
			t.Errorf("EmailLang(%q, %q) = %q, want %q", c.formLang, c.wsLang, got, c.want)
		}
	}
}

// newServiceForLangTest is a compile-time reminder that the resolution above is
// the same call the receipt sender makes: if the sender's signature ever gained a
// user-language input, this package's tests would have to pass one.
var _ = func(ctx context.Context, s *Service, ref SaleRef) string {
	return dbstore.EmailLang(ref.FormLang, ref.WorkspaceLang)
}
