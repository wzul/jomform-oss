package httpapi

// Regression guard for card t_cae9c98e — a form's `custom_js` never ran on a
// block-rendered page: the renderer served it as a QUOTED JS STRING LITERAL, a
// valid no-op statement, so the merchant's code was on the page and did nothing.
//
// THE DEFECT, reproduced by seeding the theme straight into Postgres (no PATCH,
// no dashboard, so it is independent of every save path) and GETting the form's
// public URL:
//
//	row    theme = {"custom_js": "window.__jfMeasured = 1;"}                    stored ✓
//	page   <script>\n"window.__jfMeasured = 1;"\n</script>                      runs  ✗
//
// Cause: `custom_js` is interpolated inside a `<script>` element, where
// html/template is in JS state and runs jsValEscaper. The value was typed
// `template.HTML` — an HTML-context type, which that escaper does not honour —
// so it was JSON-marshalled: a quoted string expression the browser evaluates
// and discards. The response was 200, nothing was logged, and every other theme
// key still applied; only running the page shows it.
//
// Both block templates serve the value from ONE place, blockThemeView
// (payment: blockTmpl, response: responseTmpl), so the arms below execute the
// REAL templates against that view.
//
// These arms FAIL on the pre-fix bytes: the "reaches" arm on the quoted-literal
// shape, the typing arm on `%T` (template.HTML, want template.JS), and the
// breakout arm is the guard verbatim emission makes necessary — without it
// `template.JS` would happily close the script element and hand the rest to the
// HTML parser.

import (
	"fmt"
	"html/template"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// customJSMergeTheme renders a page with custom_js set alongside a single-value
// theme key, so a fix that happened to break the rest of the theme view cannot
// pass here.
func renderCustomJSPage(t *testing.T, tmpl *template.Template, js string) string {
	t.Helper()
	return renderThemePage(t, tmpl, map[string]any{
		"custom_js": js,
		"primary":   "#5e6ad2",
	})
}

// TestCustomJSReachesTheScriptBlock is the card's headline: the merchant's code
// is served verbatim inside a script element, so the browser runs it, and the
// quoted-literal shape the defect shipped is gone.
func TestCustomJSReachesTheScriptBlock(t *testing.T) {
	// Multi-statement, with its own double quotes and two lines: a value that
	// only survives because it was JSON-marshalled cannot satisfy this, and a
	// fix that HTML-escapes it (&#34;) fails too.
	const js = `window.__jfMeasured = 1;
  window.__jfLabel = "buyer page";
  document.documentElement.dataset.jfCustomJs = "on";`

	for _, r := range themeRenderers() {
		t.Run(r.name, func(t *testing.T) {
			page := renderCustomJSPage(t, r.tmpl, js)

			// 1. Verbatim, inside its own <script> element. This is the shape a
			// browser executes: no quotes wrapped around it, no HTML entities.
			if want := "<script>\n" + js + "\n</script>"; !strings.Contains(page, want) {
				t.Fatalf("the merchant's custom_js is not served verbatim inside a <script> element.\n"+
					"want the page to contain:\n%s\ngot:\n%s", want, scriptTail(page))
			}

			// 2. The defect's exact shape must be absent — a quoted string
			// literal is valid JS that does nothing, so the browser shows no
			// error and the merchant sees no symptom.
			for _, quoted := range []string{
				`"window.__jfMeasured = 1;`,
				`window.__jfLabel = \"buyer page\";`,
			} {
				if strings.Contains(page, quoted) {
					t.Errorf("the page still serves custom_js as a JSON-marshalled string literal (%q) — "+
						"the browser evaluates a no-op and the merchant's code never runs (card t_cae9c98e).\n"+
						"script tail:\n%s", quoted, scriptTail(page))
				}
			}

			// 3. An empty / missing value must never become the literal `null`
			// or `""` inside a script block.
			for _, bad := range []string{"<script>\nnull\n</script>", "<script>\n\"\"\n</script>"} {
				if strings.Contains(page, bad) {
					t.Errorf("a themed page serves %q — an empty custom_js must emit no script block at all", bad)
				}
			}

			// 4. custom_js adds its OWN script element; it must not have been
			// merged into (or substituted for) the page's own JS.
			baseline := renderThemePage(t, r.tmpl, nil)
			if got, want := strings.Count(page, "<script"), strings.Count(baseline, "<script")+1; got != want {
				t.Errorf("the page has %d <script elements, want %d (the unthemed page's %d plus one for "+
					"custom_js) — the merchant's JS is not in an element of its own", got, want,
					want-1)
			}
		})
	}
}

// scriptTail returns the page from the start of its theme <script> element, for
// failure messages: the whole page is thousands of lines of template output.
// The theme block is the next-to-last <script> on the page — the page's own
// pay/validation script precedes it and the iframe-height reporter follows it.
func scriptTail(page string) string {
	var opens []int
	for i := 0; ; {
		j := strings.Index(page[i:], "<script")
		if j < 0 {
			break
		}
		opens = append(opens, i+j)
		i += j + len("<script")
	}
	if len(opens) == 0 {
		return "(no <script> element on the page)"
	}
	start := opens[0]
	if len(opens) >= 2 {
		start = opens[len(opens)-2]
	}
	end := start + 400
	if end > len(page) {
		end = len(page)
	}
	return page[start:end]
}

// TestCustomJSIsTypedForTheJSContext pins the MECHANISM, not just the symptom.
// `template.JS` is what tells html/template the value is already safe JS; an
// HTML-context type inside `<script>` is re-escaped by jsValEscaper into a quoted
// string expression. A future "cleanup" that turned this back into
// template.HTML (or a plain string) could still put the merchant's text on the
// page, so the symptom test alone would not notice.
func TestCustomJSIsTypedForTheJSContext(t *testing.T) {
	view := blockThemeView(&dbstore.Form{Theme: map[string]any{
		"custom_js": "window.__jfMeasured = 1;",
	}})
	if got := fmt.Sprintf("%T", view["ThemeJS"]); got != "template.JS" {
		t.Fatalf("blockThemeView's ThemeJS is %s, want template.JS — inside <script> html/template runs "+
			"jsValEscaper, which JSON-marshals any HTML-context type into a quoted no-op string "+
			"(card t_cae9c98e)", got)
	}
	if got := view["ThemeJS"].(template.JS); got != "window.__jfMeasured = 1;" {
		t.Fatalf("ThemeJS = %q, want the merchant's code unchanged — custom_js is served as JS, not as "+
			"text in HTML, so it must not be HTML-escaped", got)
	}

	// An unthemed form still gets the typed empty value, so `{{if .ThemeJS}}`
	// keeps evaluating false and the block renders exactly as before.
	empty := blockThemeView(&dbstore.Form{Theme: map[string]any{}})
	js, ok := empty["ThemeJS"].(template.JS)
	if !ok {
		t.Fatalf("an unthemed form's ThemeJS is %T, want template.JS", empty["ThemeJS"])
	}
	if js != "" {
		t.Fatalf("an unthemed form's ThemeJS must stay empty so the template's {{if .ThemeJS}} guard is "+
			"false; got %q", js)
	}
}

// TestCustomJSCannotBreakOutOfTheScriptBlock is the guard that `template.JS`
// makes necessary: the value is now emitted verbatim, and a `<script>` is an
// HTML raw-text element whose ONLY terminator is a case-insensitive `</script`.
// So the renderer truncates at the first such sequence and never serves anything
// past it.
//
// Each arm asserts three things: no injected code survives, the page closes
// exactly as many script elements as an unthemed page does (no early close), and
// the legitimate code before the attempt still runs.
func TestCustomJSCannotBreakOutOfTheScriptBlock(t *testing.T) {
	cases := []struct {
		name string
		js   string
		// body is EXACTLY what must be served as the theme script's text: the
		// merchant's code up to the first close attempt, and nothing after it.
		// Written out per case so this arm does not simply re-run the guard's
		// own arithmetic — a value the guard failed to truncate fails here even
		// when it produced no parseable markup (a bare `</script` on its own is
		// completed into a real end tag by the template's following newline).
		body string
	}{
		{"end tag followed by an img/onerror payload",
			`window.__jfKept=1;</script><img src=x onerror=jfBreakout('x')>`, `window.__jfKept=1;`},
		{"end tag upper case with whitespace and an event handler",
			`window.__jfKept=1;</SCRIPT ><img src=x onerror=jfBreakout('x')>`, `window.__jfKept=1;`},
		{"end tag with no closing bracket",
			`window.__jfKept=1;</script`, `window.__jfKept=1;`},
		{"end tag completed by the template's own closer",
			"window.__jfKept=1;</script\n", `window.__jfKept=1;`},
		{"mixed-case end tag followed by a script",
			`window.__jfKept=1;</ScRiPt><script>jfBreakout('x')</script>`, `window.__jfKept=1;`},
		{"end tag behind a string that case-folds to a different length",
			`window.__jfKept=1;var s='İ';</script><img src=x onerror=jfBreakout('x')>`,
			`window.__jfKept=1;var s='İ';`},
		{"end tag inside the merchant's own string literal",
			`window.__jfKept=1;var s="</script>";jfBreakout('x')`, `window.__jfKept=1;var s="`},
	}

	for _, r := range themeRenderers() {
		t.Run(r.name, func(t *testing.T) {
			baseline := renderThemePage(t, r.tmpl, nil)
			// The unthemed page's closers plus the one the theme block adds:
			// custom_js is served in an element of its own (asserted by
			// TestCustomJSReachesTheScriptBlock), so a value that closes a
			// script element early shows up as an extra closer here.
			wantClosers := strings.Count(baseline, "</script>") + 1
			if wantClosers == 1 {
				t.Fatalf("the unthemed page has no closing </script> — this case could not tell an early " +
					"close from a normal one")
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					page := renderCustomJSPage(t, r.tmpl, tc.js)
					// A marker that appears nowhere in either template's own
					// script, so this cannot pass on the page's legitimate JS.
					if strings.Contains(page, "jfBreakout") {
						t.Fatalf("custom_js %q got attacker-controlled code onto the page: the value left "+
							"the <script> block and the HTML parser took the rest as markup.\nscript "+
							"tail:\n%s", tc.js, scriptTail(page))
					}
					if strings.Contains(page, "<img src=x") {
						t.Fatalf("custom_js %q put injected markup on the page — the value closed the "+
							"script element early.\nscript tail:\n%s", tc.js, scriptTail(page))
					}
					if got := strings.Count(page, "</script>"); got != wantClosers {
						t.Fatalf("custom_js %q changed the number of </script> on the page (%d, want %d) — "+
							"the value closed a script element early.\nscript tail:\n%s",
							tc.js, got, wantClosers, scriptTail(page))
					}
					if !strings.Contains(page, tc.body) {
						t.Errorf("custom_js %q lost the legitimate code %q before the breakout attempt — "+
							"the guard should truncate the value, not discard the merchant's script."+
							"\nscript tail:\n%s", tc.js, tc.body, scriptTail(page))
					}
					// The served body must be the code up to the close attempt
					// and nothing after it. This is what catches a guard that
					// let a bare `</script` through: no markup is injected
					// textually, but the browser ends the element there.
					if want := "<script>\n" + tc.body + "\n</script>"; !strings.Contains(page, want) {
						t.Fatalf("custom_js %q was not truncated at its first close attempt: the theme "+
							"script's body must be exactly %q.\nscript tail:\n%s",
							tc.js, tc.body, scriptTail(page))
					}
				})
			}
		})
	}
}

// TestCustomJSEndTagIndexIsAByteOffsetInTheOriginal pins why the guard does not
// simply lower-case the value and index that: Unicode case folding can change a
// string's LENGTH ('İ' U+0130 lower-cases to two runes), so an offset found in
// the folded copy does not point at the same byte in the original — and a guard
// that truncates at the wrong byte does not guard anything.
func TestCustomJSEndTagIndexIsAByteOffsetInTheOriginal(t *testing.T) {
	prefixes := []string{
		"",
		"window.__jfKept=1;",
		"var s='İ';",
		"'İ' 'İ' var b='İ';",
	}
	tags := []string{"</script>", "</SCRIPT >", "</script", "</scrIpt"}
	for _, prefix := range prefixes {
		for _, tag := range tags {
			s := prefix + tag + "<img src=x onerror=jfBreakout('x')>"
			if got, want := indexScriptEndTag(s), len(prefix); got != want {
				t.Errorf("indexScriptEndTag(%q) = %d, want %d (the byte offset of the end tag in the "+
					"original string)", s, got, want)
			}
		}
	}
	for _, s := range []string{"", "window.__jfKept=1;", "</scrip", "</scr"} {
		if got := indexScriptEndTag(s); got != -1 {
			t.Errorf("indexScriptEndTag(%q) = %d, want -1 (no end tag to find)", s, got)
		}
	}
	// The guard truncates on the bare prefix, not on a complete end tag: the
	// template follows the value with a newline and its own `</script>`, so a
	// value-final `</script` becomes a real end tag the browser honours. That
	// also means a value containing `</scriptx` is truncated even though a
	// browser would not end the element there — a deliberate over-truncation
	// shared with customCSSForStyleBlock, and the reason merchant JS must write
	// a literal end tag as `<\/script`.
	if got, want := indexScriptEndTag("</scriptx"), 0; got != want {
		t.Errorf("indexScriptEndTag(%q) = %d, want %d (the bare prefix is what the guard cuts on)", "</scriptx", got, want)
	}
}

// TestCustomJSAbsentAddsNoScriptBlock covers the shapes a stored theme can take
// besides a string: absent, null, a non-string (a hand-written row, or a future
// key type), and the empty string. None may add a script element, put a literal
// `null` on the page, or panic the renderer.
func TestCustomJSAbsentAddsNoScriptBlock(t *testing.T) {
	themes := []struct {
		name  string
		theme map[string]any
	}{
		{"no theme at all", nil},
		{"theme without custom_js", map[string]any{"primary": "#5e6ad2"}},
		{"null custom_js", map[string]any{"custom_js": nil}},
		{"non-string custom_js", map[string]any{"custom_js": 42}},
		{"empty custom_js", map[string]any{"custom_js": ""}},
	}
	for _, r := range themeRenderers() {
		t.Run(r.name, func(t *testing.T) {
			baseline := renderThemePage(t, r.tmpl, nil)
			for _, tt := range themes {
				t.Run(tt.name, func(t *testing.T) {
					page := renderThemePage(t, r.tmpl, tt.theme)
					if page != baseline {
						t.Fatalf("a theme of shape %q changed the page — no custom_js must render exactly "+
							"as an unthemed page. got:\n%s", tt.name, scriptTail(page))
					}
					if strings.Contains(page, "<script>\nnull") || strings.Contains(page, `<script>\n""`) {
						t.Fatalf("a theme of shape %q put a null/empty literal into a script block", tt.name)
					}
				})
			}
		})
	}
}
