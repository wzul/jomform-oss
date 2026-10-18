package objstore

// t_8881000c: a stylesheet very commonly opens with a licence/theme comment,
// and `/* ... */` is byte-identical in CSS and in JS. SniffAssetType used to
// run its JS-signature prefix list — which contained `"/*"` — BEFORE the CSS
// heuristics, so a stylesheet whose first token was a comment was stored
// `.js` / `text/javascript`. `/media` sends `X-Content-Type-Options: nosniff`
// and browsers apply strict MIME checking to `<link rel="stylesheet">`, so the
// merchant's CSS was refused and silently did not apply.
//
// These cases pin both directions: comment-led CSS classifies as CSS, and
// comment-led JS is NOT stolen by the CSS heuristic.

import "testing"

func TestSniffAssetTypeSkipsLeadingComments(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		// ── comment-led CSS (the defect) ────────────────────────────────────
		{
			name: "css-prod-probe-body",
			body: "/* qa media probe t_0c3b261d: CSS with a leading comment */\n.jf-qa-probe{color:#123456;}\n",
			want: ".css",
		},
		{
			name: "css-multiline-licence-header",
			body: "/*\n * Theme: Acme 2026\n * Licence: MIT\n */\n@charset \"UTF-8\";\nbody { margin: 0 }\n",
			want: ".css",
		},
		{
			name: "css-comment-then-at-rule-without-brace",
			body: "/* licence */\n@import url(\"base.css\");\n",
			want: ".css",
		},
		{
			name: "css-two-leading-comments",
			body: "/* one */\n/* two */\n.a{color:red}\n",
			want: ".css",
		},
		{
			name: "css-comment-then-at-media",
			body: "/*! Acme */\n@media (max-width:600px){.a{display:block}}\n",
			want: ".css",
		},
		// ── comment-led JS must stay JS ─────────────────────────────────────
		{
			name: "js-block-comment-then-console",
			body: "/* x */ console.log(1)\n",
			want: ".js",
		},
		{
			name: "js-block-comment-then-const",
			body: "/* x */\nconst x = 1;\n",
			want: ".js",
		},
		{
			name: "js-preserve-comment-then-iife",
			body: "/*! Acme bundle */\n(function(){ var a = 1; })();\n",
			want: ".js",
		},
		{
			name: "js-comment-then-if-with-brace",
			body: "/* x */\nif (ready) { go(); }\n",
			want: ".js",
		},
		{
			name: "js-comment-then-module-exports-with-brace",
			body: "/* c */\nmodule.exports = {a: 1}\n",
			want: ".js",
		},
		{
			name: "js-comment-then-require",
			body: "// licence\nrequire('./boot');\n",
			want: ".js",
		},
		{
			name: "js-line-comment-then-document",
			body: "// hi\ndocument.getElementById('x');\n",
			want: ".js",
		},
		{
			name: "js-comment-then-arrow",
			body: "/** @preserve */\n(() => {})();\n",
			want: ".js",
		},
		// ── nothing to classify after the comment ──────────────────────────
		// Retained pre-existing classification: a body with no readable
		// signature after its comment is still `.js`, exactly as it was when
		// the comment itself counted as the signature. The fix changes only
		// blobs that ARE recognisable as CSS.
		{
			name: "comment-only-body-keeps-previous-classification",
			body: "/* nothing else */\n",
			want: ".js",
		},
		{
			name: "unterminated-comment-keeps-previous-classification",
			body: "/* x .a{color:red}\n",
			want: ".js",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ext, ok := SniffAssetType([]byte(c.body))
			if !ok {
				t.Fatalf("SniffAssetType(%q) = (\"\", false), want (%q, true)", c.body, c.want)
			}
			if ext != c.want {
				t.Fatalf("SniffAssetType(%q) = %q, want %q", c.body, ext, c.want)
			}
		})
	}
}

// TestSniffAssetTypeUncommentedBodiesUnchanged guards the fix against moving
// anything else: bodies with no leading comment classify exactly as before.
func TestSniffAssetTypeUncommentedBodiesUnchanged(t *testing.T) {
	cases := map[string]string{
		"body { color: red; }\n":        ".css",
		"@import url(\"x.css\");":       ".css",
		"const x = 1;":                  ".js",
		"(function(){ var a = 1; })();": ".js",
	}
	for body, want := range cases {
		ext, ok := SniffAssetType([]byte(body))
		if !ok || ext != want {
			t.Fatalf("SniffAssetType(%q) = (%q, %v), want (%q, true)", body, ext, ok, want)
		}
	}
}
