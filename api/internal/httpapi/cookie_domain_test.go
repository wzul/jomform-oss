package httpapi

import "testing"

func TestSharedCookieDomain(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://jomform.com", ".jomform.com"},
		{"https://mcp.jomform.com", ".jomform.com"},
		{"https://app.jomform.com", ".jomform.com"},
		// Dev / localhost / IP hosts must NOT get a shared Domain.
		{"http://localhost:8080", ""},
		{"http://localhost", ""},
		{"http://127.0.0.1:8080", ""},
		{"http://192.168.1.10:3000", ""},
		{"", ""},
		{"not-a-url", ""},
	}
	for _, c := range cases {
		if got := SharedCookieDomain(c.in); got != c.want {
			t.Errorf("SharedCookieDomain(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
