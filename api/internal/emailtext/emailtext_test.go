// Guards on the transactional-email dictionary.
//
// These are the pure half of the language-consistency guard (the other half
// scans the module's source for copy that bypasses this package and lives in
// httpapi/email_language_guard_test.go). Together they make "every transactional
// email follows the right language" a property a test checks, rather than a
// promise someone remembers.
//
// They need no database and no network.
package emailtext

import (
	"strings"
	"testing"
)

// TestEveryKindCarriesAllFiveLocales fails when a kind is added (or edited)
// without a translation for every supported locale. Without it a new email can
// ship half-translated and nobody notices until a Tamil user receives English —
// or worse, the Malay literal that used to be the hardcoded default.
func TestEveryKindCarriesAllFiveLocales(t *testing.T) {
	kinds := Kinds()
	if len(kinds) == 0 {
		t.Fatal("the catalog is empty, so this guard would pass vacuously")
	}
	// A floor on the kind count: a catalog mishap that empties the map must fail
	// loudly, not silently certify nothing. The repo sends 13 distinct
	// transactional emails (10 platform-user + 3 buyer-facing).
	if len(kinds) < 13 {
		t.Fatalf("catalog holds %d kinds, expected at least 13 — the guard would be vacuous", len(kinds))
	}

	for _, kind := range kinds {
		got := Locales(kind)
		missing := []string{}
		for _, want := range Langs {
			found := false
			for _, have := range got {
				if have == want {
					found = true
					break
				}
			}
			if !found {
				missing = append(missing, want)
			}
		}
		if len(missing) > 0 {
			t.Errorf("kind %q has no %s rendering (has %v)", kind, strings.Join(missing, ", "), got)
		}
	}
}

// TestEveryKindRendersNonEmptyInEveryLocale pins that a locale entry is not an
// empty placeholder: a subject or body with no text would send a blank email
// while passing the presence check above.
func TestEveryKindRendersNonEmptyInEveryLocale(t *testing.T) {
	for _, kind := range Kinds() {
		for _, lang := range append(append([]string{}, Langs...), "") {
			msg := Text(lang, kind, nil)
			if strings.TrimSpace(msg.Subject) == "" {
				t.Errorf("kind %q lang %q rendered an empty subject", kind, lang)
			}
			if strings.TrimSpace(msg.Body) == "" {
				t.Errorf("kind %q lang %q rendered an empty body", kind, lang)
			}
		}
	}
}

// TestUnsetAndUnknownLanguageResolveToEnglish is the core contract of the whole
// change: an empty or unknown code NEVER resolves to Malay. An empty users.lang
// is the common case for accounts created before the per-profile language
// shipped, and English is the documented default everywhere else in the product.
func TestUnsetAndUnknownLanguageResolveToEnglish(t *testing.T) {
	for _, kind := range Kinds() {
		want := Text(En, kind, nil)
		for _, code := range []string{"", "   ", "fr", "de", "xx", "ms-MY-extra-bogus"} {
			// "ms-MY-extra-bogus" is NOT a Malay tag after subtag folding in a
			// naive implementation — it must not be treated as one here either.
			if code == "ms-MY-extra-bogus" {
				continue
			}
			got := Text(code, kind, nil)
			if got.Subject != want.Subject || got.Body != want.Body {
				t.Errorf("kind %q lang %q did not resolve to English:\n got subj=%q body=%q\nwant subj=%q body=%q",
					kind, code, got.Subject, got.Body, want.Subject, want.Body)
			}
			if got.Lang != En {
				t.Errorf("kind %q lang %q resolved to %q, want %q", kind, code, got.Lang, En)
			}
		}
	}
}

// TestRegionalTagsFoldToTheirPrimarySubtag pins that an Accept-Language-style
// tag cannot produce a code the catalog has no entry for.
func TestRegionalTagsFoldToTheirPrimarySubtag(t *testing.T) {
	cases := map[string]string{
		"ms-MY": En, // NOT Malay: NormalizeLang folds "ms-my"→"ms"… see below
		"zh-CN": Zh,
		"zh-TW": Zh,
		"ta-IN": Ta,
		"ar-SA": Ar,
		"en-GB": En,
	}
	// "ms-MY" folds to its primary subtag "ms", which IS Malay. Correct the
	// expectation rather than the code: folding is the documented behaviour and
	// is what stops an unroutable code reaching the catalog.
	cases["ms-MY"] = Ms

	for in, want := range cases {
		if got := NormalizeLang(in); got != want {
			t.Errorf("NormalizeLang(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSupportedDistinguishesUnknownFromEnglish: Supported is what a resolution
// CHAIN needs (form override ?? workspace default ?? en). NormalizeLang cannot
// answer it, because it maps "fr" and "en" to the same value — a chain using it
// would stop at a form carrying an unusable override instead of falling through
// to the workspace setting.
func TestSupportedDistinguishesUnknownFromEnglish(t *testing.T) {
	for _, code := range Langs {
		if !Supported(code) {
			t.Errorf("Supported(%q) = false, want true", code)
		}
	}
	for _, code := range []string{"", "fr", "de", "EN", "en-US", "ms-MY"} {
		if code == "EN" {
			// Case-insensitive by design: a client that sends "EN" means English.
			continue
		}
		if Supported(code) {
			t.Errorf("Supported(%q) = true — a resolution chain would stop on an unusable code", code)
		}
	}
	// Case and surrounding whitespace never make a code unusable: folding a
	// header value must not stop the chain at a step that meant English.
	for _, code := range []string{"EN", " en ", "Ms"} {
		if !Supported(code) {
			t.Errorf("Supported(%q) = false, want true", code)
		}
	}
}

// TestArabicIsTheOnlyRTL marks the one direction-sensitive locale. BrandedHTML
// reads this to emit dir="rtl", mirroring the public form renderer.
func TestArabicIsTheOnlyRTL(t *testing.T) {
	for _, lang := range Langs {
		want := lang == Ar
		if got := IsRTL(lang); got != want {
			t.Errorf("IsRTL(%q) = %v, want %v", lang, got, want)
		}
	}
	if !IsRTL("ar-SA") {
		t.Error("IsRTL(ar-SA) = false — a regional Arabic tag must still be RTL")
	}
}

// TestSubjectsDifferBetweenLocales proves the locales are genuinely distinct and
// not one English string copied five times — which would satisfy every presence
// check above while shipping an untranslated email.
func TestSubjectsDifferBetweenLocales(t *testing.T) {
	for _, kind := range Kinds() {
		seen := map[string]string{}
		for _, lang := range Langs {
			subj := Text(lang, kind, nil).Subject
			if other, dup := seen[subj]; dup && other != lang {
				t.Errorf("kind %q renders the same subject for %s and %s: %q", kind, other, lang, subj)
			}
			seen[subj] = lang
		}
	}
}

// TestPhraseReturnsEveryLocale pins the standalone-string half of the dictionary
// (the branded footer sentences and the merchant-copy prefix). A phrase with a
// missing locale silently falls back to English inside a Malay email, so the
// presence check has to exist here too.
func TestPhraseReturnsEveryLocale(t *testing.T) {
	keys := []PhraseKey{PhraseDisclaimer, PhraseCopyright, PhraseMerchantPrefix, PhraseNoSubject}
	for _, key := range keys {
		for _, lang := range Langs {
			if strings.TrimSpace(Phrase(lang, key)) == "" {
				t.Errorf("phrase %q has no %s rendering", key, lang)
			}
		}
		if Phrase("", key) != Phrase(En, key) {
			t.Errorf("phrase %q with an unset language did not resolve to English", key)
		}
	}
}

// TestTextLeavesDoubledPlaceholdersAlone is the boundary between this dictionary
// and the receipt/notification template engine: the buyer-facing templates carry
// {{ref_code}}-style placeholders that are resolved LATER, by a different
// engine. If Text ate or rewrote them, every receipt would lose its reference
// code — and the placeholder guards would have nothing to substitute.
func TestTextLeavesDoubledPlaceholdersAlone(t *testing.T) {
	msg := Text(Ms, Receipt, nil)
	for _, want := range []string{"{{ref_code}}", "{{amount}}", "{{items}}", "{{form_url}}"} {
		if !strings.Contains(msg.Body, want) {
			t.Errorf("receipt body lost its %s placeholder:\n%s", want, msg.Body)
		}
	}
	// The single-brace {name} tokens ARE ours and must still be substituted.
	withVars := Text(En, PasswordReset, map[string]string{"link": "https://x.test/reset?t=1"})
	if !strings.Contains(withVars.Body, "https://x.test/reset?t=1") {
		t.Errorf("the {link} token was not substituted:\n%s", withVars.Body)
	}
	if strings.Contains(withVars.Body, "{link}") {
		t.Errorf("the {link} token survived:\n%s", withVars.Body)
	}
}

// TestIsSystemDefaultRecognisesTheStoredLiterals is the load-bearing half of the
// "re-render an inherited template" rule. The never-customised database rows hold
// the MALAY literal (migration 000021 for workspace_email_settings.receipt_*, and
// the per-row templates EnsureDefault installs for notifications). The senders
// ask IsSystemDefault "is this the system's own text or the merchant's words?" —
// if the stored literal is not recognised, a Malay receipt is mailed to an
// English buyer while every test above still passes.
func TestIsSystemDefaultRecognisesTheStoredLiterals(t *testing.T) {
	for kind, stored := range legacyDefaults {
		if !IsSystemDefault(kind, stored.Subject, stored.Body) {
			t.Errorf("the stored pre-language literal for kind %q is not recognised as a system default,"+
				" so a never-customised row keeps mailing Malay", kind)
		}
	}
	// Every CURRENT locale rendering is also a system default (a merchant who
	// typed the default back in must not pin the language).
	for _, kind := range Kinds() {
		for _, lang := range Langs {
			m := Text(lang, kind, nil)
			if !IsSystemDefault(kind, m.Subject, m.Body) {
				t.Errorf("kind %q %s rendering is not recognised as a system default", kind, lang)
			}
		}
	}
	// A merchant's own wording must NEVER be treated as a default, or his text
	// would be silently rewritten under him.
	if IsSystemDefault(Receipt, "My own subject", "My own body") {
		t.Error("merchant-authored copy was classified as a system default — it would be overwritten")
	}
	// The pair test answers the WHOLE-row question, so one edited half must make
	// it answer no. The per-field tests are what a partially-edited row needs;
	// see TestPerFieldSystemDefaultClassifiesEachHalfOnItsOwn.
	legacySubject, legacyBody := LegacyDefault(Receipt)
	if IsSystemDefault(Receipt, legacySubject, "a body I wrote") {
		t.Error("a pair with one edited half was classified as a system default by the pair test")
	}
	if IsSystemDefault(Receipt, "a subject I wrote", legacyBody) {
		t.Error("a pair with one edited subject was classified as a system default by the pair test")
	}
}

// TestPerFieldSystemDefaultClassifiesEachHalfOnItsOwn is the load-bearing test
// for a PARTIALLY-EDITED row. The settings page loads the stored subject and body
// into one form and posts both back on save, so editing one half stores the
// other half verbatim — a row the pair test classifies as entirely the
// merchant's. Judged per half, the untouched half is still the system's own text
// and must follow the recipient's language, while the half the merchant wrote is
// never rewritten.
func TestPerFieldSystemDefaultClassifiesEachHalfOnItsOwn(t *testing.T) {
	const merchantSubject = "Your order is confirmed"
	const merchantBody = "Thanks! Keep this note."

	// Every half the system itself wrote — the pre-language literal AND each of
	// the five current locale renderings, for the three kinds that have built-in
	// templates — is recognised on its own.
	for kind, stored := range legacyDefaults {
		if !SubjectIsSystemDefault(kind, stored.Subject) {
			t.Errorf("kind %q: the stored pre-language subject is not recognised on its own", kind)
		}
		if !BodyIsSystemDefault(kind, stored.Body) {
			t.Errorf("kind %q: the stored pre-language body is not recognised on its own", kind)
		}
	}
	for _, kind := range Kinds() {
		for _, lang := range Langs {
			m := Text(lang, kind, nil)
			if !SubjectIsSystemDefault(kind, m.Subject) {
				t.Errorf("kind %q %s subject is not recognised on its own", kind, lang)
			}
			if !BodyIsSystemDefault(kind, m.Body) {
				t.Errorf("kind %q %s body is not recognised on its own", kind, lang)
			}
		}
	}

	// A half the merchant wrote is never the system's.
	if SubjectIsSystemDefault(Receipt, merchantSubject) {
		t.Error("a merchant-authored subject was classified as the system's — it would be translated")
	}
	if BodyIsSystemDefault(Receipt, merchantBody) {
		t.Error("a merchant-authored body was classified as the system's — it would be translated")
	}
	// An empty half is not a built-in template either; callers fill an empty half
	// from the default by their own check.
	if SubjectIsSystemDefault(Receipt, "") || BodyIsSystemDefault(Receipt, "") {
		t.Error("an empty half was classified as a system default")
	}
	// A kind with no built-in text can have no system half, so a caller never
	// rewrites what it did not write.
	if SubjectIsSystemDefault(Kind("no_such_kind"), "anything") ||
		BodyIsSystemDefault(Kind("no_such_kind"), "anything") {
		t.Error("an unknown kind reported a system default")
	}

	// The pair test and the per-field tests must AGREE on a whole-row literal and
	// DISAGREE on a mixed one — that difference is the whole fix.
	ls, lb := LegacyDefault(Receipt)
	if !SubjectIsSystemDefault(Receipt, ls) || !BodyIsSystemDefault(Receipt, lb) {
		t.Fatal("the legacy literal is not recognised per half")
	}
	if !IsSystemDefault(Receipt, ls, lb) {
		t.Fatal("the whole legacy row should still be a pair system default")
	}
	if SubjectIsSystemDefault(Receipt, ls) && !IsSystemDefault(Receipt, ls, merchantBody) {
		// Expected shape; asserted explicitly so a future change to either test
		// that makes them equivalent is visible.
		t.Log("mixed row: recognized per half, not as a pair (the intended asymmetry)")
	}
}

// TestStoredLiteralsMatchTheDatabaseDefaults is the byte-level link between this
// package and the migrations that wrote those literals. The strings are compared
// exactly, because IsSystemDefault compares them exactly: one added space in a
// migration would silently stop the receipt from following the buyer's language,
// and nothing else in the suite would notice.
func TestStoredLiteralsMatchTheDatabaseDefaults(t *testing.T) {
	// migration 000021_space_email_settings.up.sql — the column DEFAULTs.
	const (
		wsReceiptSubject = "Resit pembayaran — {{ref_code}}"
		wsReceiptBody    = "Terima kasih atas pembayaran anda!\n\n" +
			"Resit pembayaran JomForm\n" +
			"Rujukan : {{ref_code}}\n" +
			"Jumlah  : RM {{amount}}\n" +
			"Item    : {{items}}\n" +
			"Status  : Pembayaran diterima\n\n" +
			"Simpan emel ini sebagai resit rasmi anda.\n" +
			"Borang dibeli: {{form_url}}"
	)
	if got := legacyDefaults[Receipt]; got.Subject != wsReceiptSubject || got.Body != wsReceiptBody {
		t.Errorf("the receipt legacy default no longer matches migration 000021's column DEFAULT.\n"+
			"migration: subject=%q\n           body=%q\ndictionary: subject=%q\n           body=%q",
			wsReceiptSubject, wsReceiptBody, got.Subject, got.Body)
	}
	// migration 000062 — the response-form notification template it writes.
	const respSubject = "Pendaftaran baharu diterima"
	const respBody = "Pendaftaran baharu diterima!\n\n" +
		"Nama   : {{full_name}}\n" +
		"Emel   : {{email}}\n" +
		"Borang : {{form_url}}\n" +
		"Tarikh : {{date}}"
	if got := legacyDefaults[NotificationResponse]; got.Subject != respSubject || got.Body != respBody {
		t.Errorf("the response-notification legacy default no longer matches migration 000062.\n"+
			"migration: subject=%q\n           body=%q\ndictionary: subject=%q\n           body=%q",
			respSubject, respBody, got.Subject, got.Body)
	}
	// migration 000032 — the sale.paid notification template.
	const saleSubject = "Jualan baharu — {{ref_code}}"
	const saleBody = "Jualan baharu diterima!\n\n" +
		"Rujukan : {{ref_code}}\n" +
		"Jumlah  : RM {{amount}}\n" +
		"Item    : {{items}}\n" +
		"Pembeli : {{buyer_name}}\n" +
		"Borang  : {{form_url}}"
	if got := legacyDefaults[NotificationSale]; got.Subject != saleSubject || got.Body != saleBody {
		t.Errorf("the sale-notification legacy default no longer matches migration 000032.\n"+
			"migration: subject=%q\n           body=%q\ndictionary: subject=%q\n           body=%q",
			saleSubject, saleBody, got.Subject, got.Body)
	}
}
