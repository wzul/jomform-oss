package httpapi

// Card t_8ef93e64: the custom-HTML order page never showed the merchant logo.
//
// The page sends its OWN Content-Security-Policy, and the header carried a bare
// `img-src data:` while the same template paints the merchant's logo as
// `<img class="logo" src="{{.LogoURL}}">`. Form.LogoURL is https-only
// (internal/urlguard on every write path), so the two correct halves were fatal
// together: EVERY logo was blocked by the page's own policy — HTTP 200, image
// never loaded, nothing logged.
//
// Measured in Chrome before the fix (page rendered by renderCustomForm and
// served with the response's own header, form logo_url
// https://example.com/logo.png):
//
//	logoSrc: "https://example.com/logo.png"
//	naturalWidth: 0   complete: true
//	securitypolicyviolation: {violatedDirective: "img-src",
//	                          blockedURI: "https://example.com/logo.png", line: 41}
//
// and ZERO violations on the same page with no logo — the header was the cause.
//
// THE SUITE DID NOT CATCH IT because every existing assertion is about the
// header's BYTES (`default-src 'none'` present) or the body's bytes (the
// sandbox attribute, the sanitized body). A bare `img-src data:` is byte-present
// and fatal. These guards decide what a BROWSER decides: they take the <img>
// the SAME response wrote, resolve its URL to the CSP source expression the
// browser will match it against, and require the SAME response's policy to admit
// it. Spec: https://www.w3.org/TR/CSP3/#match-element-to-source-list

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// renderCustomWithLogo drives the real call site — PublicHandlers.renderCustomForm,
// the function serveForm reaches for a response-mode form with custom HTML — and
// returns the CSP it emitted beside the body it wrote, because that PAIR is what
// the browser decides on.
func renderCustomWithLogo(t *testing.T, logoURL string) (csp string, body string) {
	t.Helper()
	cfg := map[string]any{
		"mode": "response",
		"custom_html": map[string]any{
			"html":   customCSPBody,
			"fields": []any{map[string]any{"field_id": "name", "type": "text", "label": "Name"}},
		},
	}
	f := themedForm("checkout", "#5e6ad2", cfg)
	f.LogoURL = logoURL
	h := &PublicHandlers{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://demo.jomform.com/checkout", nil)
	if !h.renderCustomForm(rec, req, f) {
		t.Fatal("renderCustomForm declined a form with enabled custom_html — this is no longer the surface under test")
	}
	return rec.Header().Get("Content-Security-Policy"), rec.Body.String()
}

// logoImgRe finds the logo <img> the page painted.
var logoImgRe = regexp.MustCompile(`<img class="logo" src="([^"]+)"`)

// frameImgSrcRe finds the img-src of the IFRAME's own meta CSP, which the page
// writes into its srcdoc as a JS string (so the value stops at the `;` inside
// that string). There is exactly one img-src in the body: the frame's.
var frameImgSrcRe = regexp.MustCompile(`img-src ([^;\x5c]*)`)

// TestCustomPageCSPAdmitsTheLogoItPaints is the card's defect expressed as one
// invariant: the logo THIS response rendered must be admitted by the policy THIS
// response sent.
func TestCustomPageCSPAdmitsTheLogoItPaints(t *testing.T) {
	const logo = "https://cdn.example.com/logo.png"
	csp, body := renderCustomWithLogo(t, logo)

	m := logoImgRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("the page did not paint <img class=\"logo\"> at all — this guard measured nothing")
	}
	if m[1] != logo {
		t.Fatalf("logo src = %q, want %q", m[1], logo)
	}

	d := parseCustomCSP(t, csp)
	imgSrc, ok := d["img-src"]
	if !ok {
		t.Fatalf("the page sent no img-src directive: %q — the logo falls back to default-src 'none'", csp)
	}
	if got, want := strings.Join(imgSrc, " "), "data: https://cdn.example.com"; got != want {
		t.Errorf("img-src = %q, want %q — the logo's own origin, admitted by its exact origin", got, want)
	}
	if !hasCSPSource(imgSrc, "data:") {
		t.Errorf("img-src = %q no longer admits data: — the page's own inline images (and the frame's meta CSP) rely on it", imgSrc)
	}

	// The loosening must be exactly one origin: never a wildcard, never a bare
	// scheme (which would admit every https host in the world).
	for _, broad := range []string{"*", "https:", "http:", "data:*"} {
		if hasCSPSource(imgSrc, broad) {
			t.Errorf("img-src = %q admits %q, which is wider than the logo's origin", imgSrc, broad)
		}
	}

	// The frame keeps its OWN layer: the loosened page policy must not travel
	// into the srcdoc meta CSP, which is what governs merchant markup.
	metas := frameImgSrcRe.FindAllStringSubmatch(body, -1)
	if len(metas) != 1 {
		t.Fatalf("found %d img-src occurrences in the page body, want exactly 1 (the iframe's own meta CSP)", len(metas))
	}
	if got := strings.TrimSpace(metas[0][1]); got != "data:" {
		t.Errorf("the iframe's meta CSP img-src = %q, want %q — the merchant frame must keep the rule that constrains its markup", got, "data:")
	}
}

// TestCustomPageCSPKeepsEverythingElseStrict pins the rest of the policy, so
// admitting the logo cannot be paid for with an unrelated loosening.
func TestCustomPageCSPKeepsEverythingElseStrict(t *testing.T) {
	csp, _ := renderCustomWithLogo(t, "https://cdn.example.com/logo.png")
	d := parseCustomCSP(t, csp)

	want := map[string]string{
		"default-src":     "'none'",
		"style-src":       "'unsafe-inline'",
		"connect-src":     "'self'",
		"form-action":     "'self'",
		"frame-ancestors": "'self'",
	}
	for name, value := range want {
		if got := strings.Join(d[name], " "); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
	if scripts := d["script-src"]; len(scripts) != 1 || !strings.HasPrefix(scripts[0], "'nonce-") {
		t.Errorf("script-src = %q, want exactly one 'nonce-…' source", scripts)
	}
}

// TestCustomPageImgSrcAdmitsOnlyTheLogoOriginUnit drives the decision function
// itself, so the edge cases of a stored LogoURL are pinned one by one.
func TestCustomPageImgSrcAdmitsOnlyTheLogoOriginUnit(t *testing.T) {
	for _, tc := range []struct {
		name    string
		logoURL string
		want    string
	}{
		{"plain https logo", "https://cdn.example.com/logo.png", "data: https://cdn.example.com"},
		{"non-default port", "https://cdn.example.com:8443/logo.png", "data: https://cdn.example.com:8443"},
		{"host case + scheme case", "HTTPS://CDN.Example.COM/logo.png", "data: https://cdn.example.com"},
		{"surrounding whitespace", "  https://cdn.example.com/logo.png  ", "data: https://cdn.example.com"},
		{"userinfo is dropped", "https://user:pw@cdn.example.com/logo.png", "data: https://cdn.example.com"},
		{"semicolon in the path", "https://cdn.example.com/logo.png; script-src *", "data: https://cdn.example.com"},
		{"ipv6 literal", "https://[2001:db8::1]/logo.png", "data: https://[2001:db8::1]"},
		{"idn host becomes punycode", "https://bücher.example/logo.png", "data: https://xn--bcher-kva.example"},

		// Anything that is not a clean absolute https URL keeps the page at the
		// policy it shipped with: the logo stays as broken as it was, the policy
		// is never widened and never rewritten.
		{"no logo", "", "data:"},
		{"blank logo", "   ", "data:"},
		{"http", "http://cdn.example.com/logo.png", "data:"},
		{"protocol relative", "//cdn.example.com/logo.png", "data:"},
		{"javascript", "javascript:alert(1)", "data:"},
		{"data url", "data:image/png;base64,AAAA", "data:"},
		{"no host", "https:///logo.png", "data:"},
		{"space in host", "https://cdn example.com/x.png", "data:"},
		{"quote in host", `https://cdn.example.com"x/logo.png`, "data:"},
		{"port out of range", "https://cdn.example.com:99999/x.png", "data:"},
	} {
		if got := customPageImgSrc(tc.logoURL); got != tc.want {
			t.Errorf("%s: customPageImgSrc(%q) = %q, want %q", tc.name, tc.logoURL, got, tc.want)
		}
	}
}

// TestCustomPageCSPLogoValueCannotRewriteThePolicy is the header-injection
// guard: the value is concatenated into a header, so a stored LogoURL must never
// be able to end a directive or add one. Whatever the value, the policy the
// browser receives is exactly the same seven directives.
func TestCustomPageCSPLogoValueCannotRewriteThePolicy(t *testing.T) {
	for _, logoURL := range []string{
		`https://cdn.example.com"x/logo.png`,
		"https://cdn.example.com/logo.png; script-src *",
		"https://cdn.example.com;script-src *",
		"https://cdn example.com/x.png",
		"https://cdn.example.com/\nscript-src *",
		"https://cdn.example.com/%22%3B%20script-src%20*",
		"javascript:alert(1)",
		"*",
		"https://*",
	} {
		csp, _ := renderCustomWithLogo(t, logoURL)
		d := parseCustomCSP(t, csp)

		names := make([]string, 0, len(d))
		for name := range d {
			names = append(names, name)
		}
		if len(d) != 7 {
			t.Errorf("logo %q: the policy has %d directives (%v), want exactly 7 — a stored value added or removed one", logoURL, len(d), names)
		}
		for name, srcs := range d {
			for _, s := range srcs {
				if s == "*" || strings.ContainsAny(s, ";\\\"") {
					t.Errorf("logo %q: %s carries the source %q — the value rewrote the policy", logoURL, name, s)
				}
			}
		}
		// Whatever the value, the page policy never gains a wildcard: the logo
		// either gets its own origin or the page keeps `data:` only.
		if hasCSPSource(d["img-src"], "*") || hasCSPSource(d["img-src"], "https:") {
			t.Errorf("logo %q: img-src = %q was widened instead of kept at data:", logoURL, d["img-src"])
		}
	}
}
