package objstore

// t_5f6a5d37: objects stored BEFORE t_8881000c keep the type the old sniffer
// recorded. A stylesheet that opens with a licence/theme comment is
// byte-indistinguishable from a script at its first token, so such a file was
// stored as `.js` / `text/javascript`; `/media` and `/assets` send
// `X-Content-Type-Options: nosniff`, and a browser refuses a stylesheet served
// as text/javascript — silently, so the merchant's CSS simply never applies.
//
// The serve path now publishes what the BYTES are for the css/js pair (the only
// allowlisted pair whose signatures overlap), in both directions, and only on a
// POSITIVE classification: a body the sniffer cannot identify keeps the type it
// was recorded with rather than being re-labelled on a guess.
//
// These cases pin:
//   - the defect: `.js`/text/javascript holding comment-led CSS → text/css;
//   - the converse: `.css`/text/css holding obvious JS → text/javascript;
//   - truthful objects are untouched, including the minified-IIFE family a
//     brace heuristic mistakes for CSS;
//   - ambiguous bodies (comment-only, unterminated comment) keep their record;
//   - markup and PDFs are never re-labelled as stylesheets.

import "testing"

const (
	mimeCSS = "text/css; charset=utf-8"
	mimeJS  = "text/javascript; charset=utf-8"

	// legacyCSSProdProbe is the body measured on prod v1.21.220 during
	// t_0c3b261d: a comment-led stylesheet stored under a `.js` key.
	legacyCSSProdProbe = "/* qa media probe t_0c3b261d: CSS with a leading comment */\n.jf-qa-probe{color:#123456;}\n"
	legacyCSSLicence   = "/*\n * Theme: Acme 2026\n * Licence: MIT\n */\n@charset \"UTF-8\";\nbody { margin: 0 }\n"
	minifiedIIFE       = "/*! Bootstrap v5.3.0 */\n!function(e){var t={};e.x=t}();"
)

func TestServedContentTypeCorrectsLegacyCodeAssets(t *testing.T) {
	cases := []struct {
		name   string
		stored string
		body   string
		want   string
	}{
		// ── the defect: a comment-led stylesheet recorded as a script ────────
		{"legacy-js-key-holding-comment-led-css", mimeJS, legacyCSSProdProbe, mimeCSS},
		{"legacy-application-js-key-holding-licence-css", "application/javascript; charset=utf-8", legacyCSSLicence, mimeCSS},
		{"legacy-js-type-without-charset", "text/javascript", legacyCSSProdProbe, mimeCSS},
		{"legacy-js-key-holding-css-at-rule", mimeJS, "@import url(\"base.css\");", mimeCSS},

		// ── the converse: a script recorded as a stylesheet ─────────────────
		{"legacy-css-key-holding-comment-led-js", mimeCSS, "/* licence */\nconst x = 1;", mimeJS},
		{"legacy-css-key-holding-minified-iife", mimeCSS, "!function(e){e.x=1}();", mimeJS},
		{"legacy-css-type-without-charset-holding-js", "text/css", "document.querySelector('.a');", mimeJS},

		// ── truthful objects keep their recorded type ───────────────────────
		{"genuine-js", mimeJS, "const x = 1;", mimeJS},
		{"genuine-js-comment-led", mimeJS, "/* x */\nconst x = 1;", mimeJS},
		{"genuine-minified-js", mimeJS, minifiedIIFE, mimeJS},
		{"genuine-js-array-literal-start", mimeJS, "[1,2].forEach(function(x){return x});", mimeJS},
		{"genuine-css", mimeCSS, "body { color: red; }", mimeCSS},
		{"genuine-css-comment-led", mimeCSS, legacyCSSLicence, mimeCSS},

		// ── a UTF-8 BOM is not a signature ──────────────────────────────────
		{"bom-prefixed-js", mimeJS, "\ufeffconst x = 1;", mimeJS},
		{"bom-prefixed-css-recorded-as-js", mimeJS, "\ufeff.a{color:red}", mimeCSS},

		// ── ambiguous bodies: never re-labelled on a guess ──────────────────
		{"comment-only-recorded-js", mimeJS, "/* nothing else */\n", mimeJS},
		{"comment-only-recorded-css", mimeCSS, "/* nothing else */\n", mimeCSS},
		{"unterminated-comment-recorded-js", mimeJS, "/* x .a{color:red}", mimeJS},
		{"empty-recorded-css", mimeCSS, "", mimeCSS},

		// ── markup and PDFs are not stylesheets, brace or not ───────────────
		{"html-recorded-as-js", mimeJS, "<!doctype html><html><head><style>.a{color:red}</style></head></html>", mimeJS},
		{"svg-recorded-as-js", mimeJS, "<svg xmlns=\"http://www.w3.org/2000/svg\"><style>.a{fill:red}</style></svg>", mimeJS},
		{"pdf-recorded-as-js", mimeJS, "%PDF-1.7\n1 0 obj { << /Type /Catalog >> }\n%%EOF", mimeJS},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ServedContentType(c.stored, []byte(c.body)); got != c.want {
				t.Fatalf("ServedContentType(%q, %q bytes) = %q, want %q", c.stored, c.name, got, c.want)
			}
		})
	}
}

// TestSniffAssetTypeMinifiedIIFEIsJS pins the signature additions the serve
// correction relies on. A minified bundle opens with a bare IIFE (`!function`)
// or a defensive `;`, and the brace heuristic alone called that CSS — which
// would have made the serve path re-label a CORRECTLY stored script `.css`.
func TestSniffAssetTypeMinifiedIIFEIsJS(t *testing.T) {
	for _, body := range []string{
		"!function(e){var t={};e.x=t}();",
		"!function(){}();",
		"!(()=>{})();",
		";!function(){var a=1}();",
		minifiedIIFE,
		"/* licence */\n!function(e){e.x=1}();",
	} {
		ext, ok := SniffAssetType([]byte(body))
		if !ok || ext != ".js" {
			t.Errorf("SniffAssetType(%q) = (%q, %v), want (.js, true)", body, ext, ok)
		}
	}

	// An array-literal start is not a stylesheet either: no CSS rule can begin
	// with `[`, so the brace inside the callback must not decide the type.
	if ext, _ := SniffAssetType([]byte("[1,2].forEach(function(x){return x});")); ext == ".css" {
		t.Fatalf("array-literal script classified %q; the brace heuristic must not read it as CSS", ext)
	}
}
