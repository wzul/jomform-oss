// Issue #189: the email verify page's "log in" link must point at the web
// login page (not the POST-only /api/auth/login endpoint, which returns 405 on
// a browser GET), and the text must not mix languages. These are pure unit
// tests — no Postgres/Valkey needed.
package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoginURL(t *testing.T) {
	cases := []struct {
		name string
		base string
		want string
	}{
		{"app base set", "https://jomform.com", "https://jomform.com/login"},
		{"app base with trailing slash", "https://jomform.com/", "https://jomform.com/login"},
		{"no app base falls back to relative", "", "/login"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := &AuthHandlers{BaseURL: c.base}
			if got := h.loginURL(); got != c.want {
				t.Fatalf("loginURL() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestWriteVerifyPageLoginLinkAndLanguage(t *testing.T) {
	rec := httptest.NewRecorder()
	writeVerifyPage(rec, "en", "https://jomform.com/login")
	body := rec.Body.String()

	// The link must point at the web login page, never the POST-only API.
	if !strings.Contains(body, `href="https://jomform.com/login"`) {
		t.Errorf("verify page link does not point at the web login page:\n%s", body)
	}
	if strings.Contains(body, "/api/auth/login") {
		t.Errorf("verify page still links to the POST-only API endpoint:\n%s", body)
	}

	// The text must not mix languages (issue #189: "Tekan here").
	if strings.Contains(body, "Tekan") {
		t.Errorf("verify page still contains mixed-language 'Tekan here':\n%s", body)
	}
	// The anchor splits the sentence, so assert the English fragments around it.
	if !strings.Contains(body, "Click <a href=") || !strings.Contains(body, ">here</a> to log in.") {
		t.Errorf("verify page missing consistent English 'Click here to log in.':\n%s", body)
	}
}

func TestWriteVerifyPageLocalized(t *testing.T) {
	cases := []struct {
		lang string
		msg  string
	}{
		{"en", "Your email has been verified!"},
		{"ms", "Emel anda telah disahkan!"},
		{"zh", "您的邮箱已验证！"},
		{"ta", "உங்கள் மின்னஞ்சல் சரிபார்க்கப்பட்டது!"},
		{"ar", "تم التحقق من بريدك الإلكتروني!"},
	}
	for _, c := range cases {
		t.Run(c.lang, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeVerifyPage(rec, c.lang, "https://jomform.com/login")
			body := rec.Body.String()
			if !strings.Contains(body, c.msg) {
				t.Errorf("verify page for lang %q missing localized message %q:\n%s", c.lang, c.msg, body)
			}
			if !strings.Contains(body, `lang="`+c.lang+`"`) {
				t.Errorf("verify page for lang %q missing html lang attribute:\n%s", c.lang, body)
			}
			// THE DIRECTION, asserted per locale (card t_75ce0ac6). The page is the
			// one surface a user reaches straight from an email client, so no
			// dashboard JS has set `document.documentElement.dir` by the time it
			// renders — the document has to carry it. The FAILURE page already
			// asserted this and the SUCCESS page did not, so an Arabic confirmation
			// rendered RTL prose inside an LTR document.
			want := "ltr"
			if c.lang == "ar" {
				want = "rtl"
			}
			if !strings.Contains(body, `dir="`+want+`"`) {
				t.Errorf("verify page for lang %q has no dir=%q — an RTL locale renders in an LTR "+
					"document. body:\n%s", c.lang, want, body)
			}
		})
	}
}

func TestWriteVerifyPageUnknownLangDefaultsToEnglish(t *testing.T) {
	rec := httptest.NewRecorder()
	writeVerifyPage(rec, "fr", "https://jomform.com/login")
	body := rec.Body.String()
	if !strings.Contains(body, "Your email has been verified!") {
		t.Errorf("unknown lang should fall back to English:\n%s", body)
	}
	if !strings.Contains(body, `lang="en"`) {
		t.Errorf("unknown lang should set html lang=en:\n%s", body)
	}
	// The fallback must carry the direction of the language it FELL BACK TO —
	// en is ltr — rather than the (absent) direction of the requested one.
	if !strings.Contains(body, `dir="ltr"`) {
		t.Errorf("unknown lang should set dir=ltr with its en fallback:\n%s", body)
	}
}

func TestDetectVerifyLang(t *testing.T) {
	cases := []struct {
		name   string
		query  string
		accept string
		want   string
	}{
		{"lang param wins", "&lang=ms", "", "ms"},
		{"lang param zh", "&lang=zh", "en-US", "zh"},
		{"unknown lang param falls to accept", "&lang=xx", "ta", "ta"},
		{"accept primary subtag", "", "zh-CN,en;q=0.8", "zh"},
		{"accept ms", "", "ms-MY", "ms"},
		{"accept ar", "", "ar", "ar"},
		{"accept en", "", "en-US,en;q=0.9", "en"},
		{"no lang no accept defaults en", "", "", "en"},
		{"unsupported accept defaults en", "", "fr-FR,de", "en"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/v1/auth/verify?token=x"+c.query, nil)
			if c.accept != "" {
				req.Header.Set("Accept-Language", c.accept)
			}
			if got := detectVerifyLang(req); got != c.want {
				t.Fatalf("detectVerifyLang() = %q, want %q", got, c.want)
			}
		})
	}
}
