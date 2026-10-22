package httpapi

// Regression guard for card t_a7bf0fb2 — a form's `custom_css` never reached the
// buyer's page: the block renderer served the literal `ZgotmplZ` sentinel where
// the merchant's stylesheet belongs.
//
// THE DEFECT, reproduced on main 21258799 by seeding the theme straight into
// Postgres (no PATCH, no dashboard, so it is independent of every save path) and
// GETting the form's public URL:
//
//	row    theme = {"custom_css": ".card { box-shadow: none; }", "primary": …}   stored ✓
//	page   … .card { padding: 32px; }\n          ZgotmplZ\n        </style>      rule ✗
//
// Cause: `custom_css` is interpolated inside a `<style>` element, where
// html/template is in CSS text state and runs cssValueFilter. The value was
// typed `template.HTML` — an HTML-context type, which that filter does not
// honour — so any multi-declaration stylesheet failed the filter and was
// replaced by the sentinel. The response was 200, nothing was logged, and every
// other theme key (background / layout / spacing / font) still applied; only
// reading the page shows it. Both block templates serve the value from ONE
// place, blockThemeView (payment: blockTmpl, response: responseTmpl).
//
// The arms below are DB-free on purpose: they execute the REAL templates against
// the view blockThemeView builds, which is the whole wiring the defect lived in.
// The end-to-end half — a real row through a real public GET — is asserted by
// TestFormPatchRoundTrippedThemeKeepsUnownedKeysAndTheirCSS.
//
// These arms FAIL on the pre-fix bytes: the first one on the sentinel, the
// typing one on `%T` (string/template.HTML, want template.CSS), and the breakout
// one is the guard that raw emission makes necessary — without it `template.CSS`
// would happily close the style element and hand the rest to the HTML parser.

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// customCSSRenderer is one of the two public block templates. Both must serve a
// merchant's stylesheet identically — they are separate templates over separate
// view shapes, so a fix in one is not a fix in the other.
type customCSSRenderer struct {
	name string
	tmpl *template.Template
}

func customCSSRenderers() []customCSSRenderer {
	return []customCSSRenderer{
		{"payment blockTmpl", blockTmpl},
		{"response responseTmpl", responseTmpl},
	}
}

// renderThemePage executes one public block template with the theme view the
// renderers merge — the exact wiring, not a hand-built map, so a change to how
// the renderers type or escape the value shows up here.
func renderThemePage(t *testing.T, tmpl *template.Template, theme map[string]any) string {
	t.Helper()
	view := map[string]any{
		"Slug": "custom-css", "Title": "Custom CSS", "Lang": "en",
		"Ls": formLangs["en"],
	}
	for k, v := range blockThemeView(&dbstore.Form{Theme: theme}) {
		view[k] = v
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, view); err != nil {
		t.Fatalf("execute %s: %v", tmpl.Name(), err)
	}
	return buf.String()
}

// firstStyleBlock returns the text between the page's first <style> and its
// closing </style>, so an assertion can say "the rule is inside the block a
// browser will parse as CSS" rather than merely "the rule is somewhere in the
// response".
func firstStyleBlock(page string) (string, bool) {
	const open = "<style>"
	i := strings.Index(page, open)
	if i < 0 {
		return "", false
	}
	rest := page[i+len(open):]
	if end := strings.Index(rest, "</style>"); end >= 0 {
		return rest[:end], true
	}
	return rest, true
}

// TestCustomCSSReachesTheStyleBlock is the card's headline: the merchant's rules
// are served, in the stylesheet, with no sentinel anywhere on the page.
func TestCustomCSSReachesTheStyleBlock(t *testing.T) {
	// The prod row's rule plus a second one, so a "part of the stylesheet
	// survived" pass cannot hide a truncation.
	const css = ".card { box-shadow: none; }\n  .prod { border: 0; }"

	for _, r := range customCSSRenderers() {
		t.Run(r.name, func(t *testing.T) {
			page := renderThemePage(t, r.tmpl, map[string]any{
				"custom_css": css,
				"primary":    "#5e6ad2",
			})

			// 1. The sentinel the defect shipped must be gone — this is the
			// assertion that fails on the pre-fix bytes.
			if strings.Contains(page, "ZgotmplZ") {
				t.Fatalf("the rendered page still contains the html/template `ZgotmplZ` sentinel — the merchant's "+
					"custom_css is being filtered out of the CSS context instead of served (card t_a7bf0fb2).\n"+
					"style block:\n%s", firstStyle(page))
			}

			// 2. The rules themselves are served, and served INSIDE the style
			// element (a rule anywhere else is not CSS a browser applies).
			style, ok := firstStyleBlock(page)
			if !ok {
				t.Fatalf("the page has no <style> block at all — nothing to serve custom_css into")
			}
			if want := ".card { box-shadow: none; }"; !strings.Contains(style, want) {
				t.Errorf("the merchant's rule %q is not in the page's stylesheet.\nstyle block:\n%s", want, style)
			}
			if want := ".card { box-shadow: none; }"; !strings.Contains(page, want) {
				t.Errorf("the merchant's rule %q is not on the page at all (style block:\n%s)", want, style)
			}
			if want := ".prod { border: 0; }"; !strings.Contains(style, want) {
				t.Errorf("the second rule %q of the same stylesheet is missing — the value is being truncated "+
					"somewhere before the whole stylesheet is served.\nstyle block:\n%s", want, style)
			}
		})
	}
}

// firstStyle is firstStyleBlock ignoring the OK flag, for failure messages.
func firstStyle(page string) string {
	s, ok := firstStyleBlock(page)
	if !ok {
		return "(no <style> block)"
	}
	return s
}

// TestCustomCSSIsTypedForTheCSSContext pins the MECHANISM, not just the symptom.
// `template.CSS` is what tells html/template the value is already safe CSS — an
// HTML-context type (string, template.HTML) is re-filtered in a CSS context and
// a multi-declaration stylesheet does not survive it. A future "cleanup" that
// turns this back into a plain string would still render single values, so the
// symptom test above could pass while the defect returned for real stylesheets.
func TestCustomCSSIsTypedForTheCSSContext(t *testing.T) {
	view := blockThemeView(&dbstore.Form{Theme: map[string]any{
		"custom_css": ".card { box-shadow: none; }",
	}})
	if got := fmt.Sprintf("%T", view["ThemeCSS"]); got != "template.CSS" {
		t.Fatalf("blockThemeView's ThemeCSS is %s, want template.CSS — inside <style> html/template runs "+
			"cssValueFilter, which ignores HTML-context types and substitutes the `ZgotmplZ` sentinel for any "+
			"multi-declaration stylesheet (card t_a7bf0fb2)", got)
	}

	// An unthemed form still gets the typed empty value, so `{{if .ThemeCSS}}`
	// keeps evaluating false and the block renders exactly as before.
	empty := blockThemeView(&dbstore.Form{Theme: map[string]any{}})
	if got := fmt.Sprintf("%T", empty["ThemeCSS"]); got != "template.CSS" {
		t.Fatalf("an unthemed form's ThemeCSS is %s, want template.CSS", got)
	}
	if !falseIfEmpty(empty["ThemeCSS"]) {
		t.Fatalf("an unthemed form's ThemeCSS must stay empty so the template's {{if .ThemeCSS}} guard is false; got %q",
			empty["ThemeCSS"])
	}
}

// falseIfEmpty reports whether v is an empty template.CSS (i.e. renders falsy in
// the template's {{if}}).
func falseIfEmpty(v any) bool {
	if css, ok := v.(template.CSS); ok {
		return css == ""
	}
	return false
}

// TestCustomCSSCannotBreakOutOfTheStyleBlock is the guard that `template.CSS`
// makes necessary: the value is now emitted verbatim, and a `<style>` is an HTML
// raw-text element whose ONLY terminator is a case-insensitive `</style`. So the
// renderer truncates at the first such sequence and never serves anything past
// it. Each arm below asserts three things: no injected code survives, the style
// element is closed exactly as many times as an unthemed page closes it (no
// early close), and the legitimate CSS before the attempt still applies.
func TestCustomCSSCannotBreakOutOfTheStyleBlock(t *testing.T) {
	cases := []struct {
		name string
		css  string
		// kept is the part of the value that must survive — the legitimate CSS
		// before the breakout attempt.
		kept string
	}{
		{"end tag followed by a script", ".a{color:red}</style><script>jfBreakout('x')</script>", ".a{color:red}"},
		{"end tag upper case, with whitespace and an event handler", ".a{color:red}</STYLE \n><img src=x onerror=jfBreakout('x')>", ".a{color:red}"},
		{"end tag with no closing bracket", ".a{color:red}</style", ".a{color:red}"},
		{"end tag completed by the template's own closer", ".a{color:red}</style\n", ".a{color:red}"},
		{"partial end tag the template's closer would finish", ".a{color:red}</styl", ".a{color:red}"},
		{"end tag behind a string that case-folds to a different length", ".a::after{content:'İ'}</style><script>jfBreakout('x')</script>", ".a::after{content:'İ'}"},
	}

	for _, r := range customCSSRenderers() {
		t.Run(r.name, func(t *testing.T) {
			baseline := renderThemePage(t, r.tmpl, nil)
			wantClosers := strings.Count(baseline, "</style>")
			if wantClosers == 0 {
				t.Fatalf("the unthemed page has no closing </style> — this case could not tell an early close from a normal one")
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					page := renderThemePage(t, r.tmpl, map[string]any{"custom_css": tc.css})
					// A marker that appears nowhere in either template's own script,
					// so this cannot pass on the page's legitimate JS.
					if strings.Contains(page, "jfBreakout") {
						t.Fatalf("custom_css %q got attacker-controlled code onto the page: the value left the "+
							"<style> block and the HTML parser took the rest as markup.\nstyle block:\n%s",
							tc.css, firstStyle(page))
					}
					if got := strings.Count(page, "</style>"); got != wantClosers {
						t.Fatalf("custom_css %q changed the number of </style> on the page (%d, want %d) — the value "+
							"closed the style element early.\nstyle block:\n%s", tc.css, got, wantClosers, firstStyle(page))
					}
					if !strings.Contains(page, tc.kept) {
						t.Errorf("custom_css %q lost the legitimate rule %q before the breakout attempt — the guard "+
							"should truncate the value, not discard the merchant's stylesheet.\nstyle block:\n%s",
							tc.css, tc.kept, firstStyle(page))
					}
				})
			}
		})
	}
}

// TestCustomCSSEndTagIndexIsAByteOffsetInTheOriginal pins why the guard does not
// simply lower-case the value and index that: Unicode case folding can change a
// string's LENGTH ('İ' U+0130 lower-cases to two runes), so an offset found in
// the folded copy does not point at the same byte in the original — and a guard
// that truncates at the wrong byte does not guard anything.
func TestCustomCSSEndTagIndexIsAByteOffsetInTheOriginal(t *testing.T) {
	prefixes := []string{
		"",
		".a{color:red}",
		".a::after{content:'İ'}",
		"'İ' 'İ' .b{color:blue}",
	}
	tags := []string{"</style>", "</STYLE >", "</style", "</sTyLe"}
	for _, prefix := range prefixes {
		for _, tag := range tags {
			s := prefix + tag + "<script>alert(1)</script>"
			if got, want := indexStyleEndTag(s), len(prefix); got != want {
				t.Errorf("indexStyleEndTag(%q) = %d, want %d (the byte offset of the end tag in the original string)",
					s, got, want)
			}
		}
	}
	for _, s := range []string{"", ".a{color:red}", "</styl", "</sty", "<style>.a{}"} {
		if got := indexStyleEndTag(s); got != -1 {
			t.Errorf("indexStyleEndTag(%q) = %d, want -1 (no end tag to find)", s, got)
		}
	}
}

// TestCustomCSSAbsentIsNotASentinel covers the shapes a stored theme can take
// besides a string: absent, null, and a non-string (a hand-written row, or a
// future key type). None may put a sentinel on the page or panic the renderer.
func TestCustomCSSAbsentIsNotASentinel(t *testing.T) {
	themes := []struct {
		name  string
		theme map[string]any
	}{
		{"no theme at all", nil},
		{"theme without custom_css", map[string]any{"primary": "#5e6ad2"}},
		{"null custom_css", map[string]any{"custom_css": nil}},
		{"non-string custom_css", map[string]any{"custom_css": 42}},
	}
	for _, r := range customCSSRenderers() {
		for _, tt := range themes {
			t.Run(r.name+"/"+tt.name, func(t *testing.T) {
				page := renderThemePage(t, r.tmpl, tt.theme)
				if strings.Contains(page, "ZgotmplZ") {
					t.Fatalf("a theme of shape %q put the `ZgotmplZ` sentinel on the page (style block:\n%s)",
						tt.name, firstStyle(page))
				}
			})
		}
	}
}
