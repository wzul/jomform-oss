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
	writeVerifyPage(rec, "Emel anda telah disahkan! Anda kini boleh log masuk.", "https://jomform.com/login")
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
