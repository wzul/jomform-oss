package buyertext

import (
	"errors"
	"sort"
	"strings"
	"testing"
)

// TestCatalogCompleteness is the dictionary guard: every refusal code must carry
// a non-blank sentence in EVERY language a form can be served in. A missing
// translation is not a compile error and not a runtime error — Text falls back
// to the English arm — so without this guard it reaches a buyer as the wrong
// language, which is the exact defect the package exists to fix (a Malay refusal
// on an English page, or the reverse).
func TestCatalogCompleteness(t *testing.T) {
	if problems := catalogProblems(catalog); len(problems) > 0 {
		t.Fatalf("%d buyer refusal sentence(s) are missing or malformed — each one renders the "+
			"wrong language (or the bare code) to a real buyer:\n  %s",
			len(problems), strings.Join(problems, "\n  "))
	}
	if got := len(Codes()); got < 50 {
		t.Fatalf("catalog carries %d codes, expected at least 50 — the public refusal surface has "+
			"~55 sentences, so a catalog this small has lost part of its subject", got)
	}
	t.Logf("%d refusal codes × %d languages: no blanks, placeholders agree", len(Codes()), len(langs))
}

// TestCatalogGuardBitesOnEveryLanguage is the proof the guard above fails
// CLOSED: for every code and every locale it blanks the sentence in a copy of
// the catalog and requires exactly one finding naming that code/locale. A guard
// that cannot name a blanked string certifies nothing.
func TestCatalogGuardBitesOnEveryLanguage(t *testing.T) {
	codes := Codes()
	if len(codes) == 0 {
		t.Fatal("catalog is empty — the bite proof would test nothing")
	}
	blanked := 0
	for _, code := range codes {
		for _, lang := range langs {
			dict := cloneCatalog()
			dict[code][lang] = ""
			problems := catalogProblems(dict)
			want := code + "." + lang + ": blank sentence"
			found := false
			for _, p := range problems {
				if strings.HasPrefix(p, code+"."+lang+":") {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("blanking %s in %q produced no finding for it (got %d findings: %v); "+
					"want the guard to name %q", code, lang, len(problems), problems, want)
			}
			blanked++
		}
	}
	if want := len(codes) * len(langs); blanked != want {
		t.Fatalf("bite proof ran %d of the %d code/locale pairs it claims", blanked, want)
	}
	t.Logf("blanked all %d code/locale pairs — the guard named every one", blanked)
}

// TestCatalogPlaceholdersAgree pins the other half of a usable translation: the
// sentence a buyer sees must interpolate the SAME values as the English one. A
// Chinese arm that drops {label}, or invents {name}, either loses the merchant's
// field name or prints the placeholder literally.
func TestCatalogPlaceholdersAgree(t *testing.T) {
	for code, byLang := range catalog {
		want := placeholders(byLang["en"])
		for _, lang := range langs {
			if lang == "en" {
				continue
			}
			if got := placeholders(byLang[lang]); !equalStrings(got, want) {
				t.Errorf("%s.%s interpolates %v, but en interpolates %v — one arm would print a "+
					"placeholder literally or drop a value", code, lang, got, want)
			}
		}
	}
}

// TestTextResolvesEveryLocale pins the actual sentences the acceptance criterion
// names for the reported refusal (an invalid submit on an order form) and for a
// wrong private-form password: en, ms and zh must each be the language asked for.
func TestTextResolvesEveryLocale(t *testing.T) {
	cases := []struct {
		code string
		lang string
		want string
	}{
		{CodeOrderIncomplete, "en", "Incomplete details"},
		{CodeOrderIncomplete, "ms", "Butiran tidak lengkap"},
		{CodeOrderIncomplete, "zh", "信息不完整"},
		{CodeOrderIncomplete, "ta", "விவரங்கள் முழுமையடையவில்லை"},
		{CodeOrderIncomplete, "ar", "التفاصيل غير مكتملة"},
		{CodeFormWrongPassword, "en", "Wrong password"},
		{CodeFormWrongPassword, "ms", "Kata laluan salah"},
		{CodeFormWrongPassword, "zh", "密码错误"},
		{CodeFieldRequired, "zh", "请填写{label}"},
		{CodeShipNoDelivery, "ms", "Tiada penghantaran ke alamat ini"},
	}
	for _, tc := range cases {
		if got := Text(tc.lang, tc.code, nil); got != tc.want {
			t.Errorf("Text(%q, %q) = %q, want %q", tc.lang, tc.code, got, tc.want)
		}
	}
	// An unsupported language falls back to the wire language, never to a blank.
	if got := Text("fr", CodeOrderIncomplete, nil); got != "Incomplete details" {
		t.Errorf("Text(fr) = %q, want the English arm", got)
	}
	// An unknown code is visible, not blank.
	if got := Text("en", "no.suchCode", nil); got != "no.suchCode" {
		t.Errorf("Text(unknown) = %q, want the code itself", got)
	}
}

// TestMessageInterpolatesAndAsRecovers is the contract formfields/shipping rely
// on: the error IS the English wire prose, and the HTTP layer can recover the
// code and params to localize it.
func TestMessageInterpolatesAndAsRecovers(t *testing.T) {
	m := New(CodeOrderOutOfStockVariant, map[string]string{"name": "Widget", "variant": "Red"})
	if got, want := m.Error(), "Widget (Red) is out of stock"; got != want {
		t.Errorf("Error() = %q, want %q (English wire prose)", got, want)
	}
	var err error = m
	got := As(err)
	if got == nil || got.Code != CodeOrderOutOfStockVariant {
		t.Fatalf("As(err) = %+v, want the message behind it", got)
	}
	if s := Text("ms", got.Code, got.Params); s != "Widget (Red) kehabisan stok" {
		t.Errorf("ms = %q, want the Malay arm with the same params", s)
	}
	if As(errors.New("plain")) != nil {
		t.Error("As(plain error) must be nil — a non-refusal is not a buyer message")
	}
	var nilMsg *Message
	if nilMsg.Error() != "" {
		t.Error("a nil message must render empty, not panic")
	}
}

// --- helpers ---------------------------------------------------------------

func catalogProblems(dict map[string]map[string]string) []string {
	var problems []string
	codes := make([]string, 0, len(dict))
	for c := range dict {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	for _, code := range codes {
		byLang := dict[code]
		for _, lang := range langs {
			if strings.TrimSpace(byLang[lang]) == "" {
				problems = append(problems, code+"."+lang+": blank sentence")
			}
		}
		for lang := range byLang {
			if !contains(langs, lang) {
				problems = append(problems, code+"."+lang+": not a supported language")
			}
		}
		if byLang["en"] == code {
			problems = append(problems, code+".en: the wire prose is the code itself")
		}
	}
	sort.Strings(problems)
	return problems
}

func cloneCatalog() map[string]map[string]string {
	out := make(map[string]map[string]string, len(catalog))
	for code, byLang := range catalog {
		cp := make(map[string]string, len(byLang))
		for k, v := range byLang {
			cp[k] = v
		}
		out[code] = cp
	}
	return out
}

func placeholders(s string) []string {
	var out []string
	for {
		i := strings.Index(s, "{")
		if i < 0 {
			break
		}
		j := strings.Index(s[i:], "}")
		if j < 0 {
			break
		}
		out = append(out, s[i:i+j+1])
		s = s[i+j+1:]
	}
	sort.Strings(out)
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
