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
//
// Card t_331059a8 is the second half of the same story: once the value reached
// the page it was still emitted in the token sheet ABOVE the template's base
// sheet, so every rule of equal specificity lost (`.card{padding:8px}` computed
// 32px, `body{background-color:…}` computed #f7f8f8, `!important` the only
// workaround). "Served" and "applied" are different properties, which is why the
// sentinel arm could not catch it: the rule was on the page, inside a <style>
// element, just not the winning one. The order of the elements is the contract;
// TestMerchantCSSWinsTheCascadeOverTheBaseStylesheet pins it.

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// themeRenderer is one of the two public block templates, rendered with a theme
// document. Both must serve a merchant's theme values identically — they are
// separate templates over separate view shapes, so a fix in one is not a fix in
// the other.
type themeRenderer struct {
	name string
	tmpl *template.Template
}

func themeRenderers() []themeRenderer {
	return []themeRenderer{
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

// styleBlocks returns the text of every `<style>` element on the page, in
// DOCUMENT ORDER — which is the order a browser applies them in, and therefore
// the only order an assertion about the cascade can be written against. An
// assertion that just says "the rule is somewhere in the response" cannot tell
// "served" from "served and then overridden".
func styleBlocks(page string) []string {
	const open, close = "<style>", "</style>"
	var out []string
	rest := page
	for {
		i := strings.Index(rest, open)
		if i < 0 {
			return out
		}
		rest = rest[i+len(open):]
		j := strings.Index(rest, close)
		if j < 0 {
			return append(out, rest)
		}
		out = append(out, rest[:j])
		rest = rest[j+len(close):]
	}
}

// merchantStyleBlock returns the stylesheet the merchant's `custom_css` is
// served in: the LAST `<style>` on the page. Equal-specificity CSS is resolved
// by document order, so a merchant rule only takes effect when nothing the
// template owns is served after it (card t_331059a8).
func merchantStyleBlock(page string) (string, bool) {
	blocks := styleBlocks(page)
	if len(blocks) == 0 {
		return "", false
	}
	return blocks[len(blocks)-1], true
}

// allStyles is every `<style>` block, numbered, for failure messages — the
// order is the finding, so a message that prints only one block hides it.
func allStyles(page string) string {
	blocks := styleBlocks(page)
	if len(blocks) == 0 {
		return "(no <style> block)"
	}
	var b strings.Builder
	for i, s := range blocks {
		fmt.Fprintf(&b, "\n--- <style> #%d ---%s", i, s)
	}
	return b.String()
}

// TestCustomCSSReachesTheStyleBlock is the card's headline: the merchant's rules
// are served, in the stylesheet, with no sentinel anywhere on the page.
func TestCustomCSSReachesTheStyleBlock(t *testing.T) {
	// The prod row's rule plus a second one, so a "part of the stylesheet
	// survived" pass cannot hide a truncation.
	const css = ".card { box-shadow: none; }\n  .prod { border: 0; }"

	for _, r := range themeRenderers() {
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
					"style blocks:%s", allStyles(page))
			}

			// 2. The rules themselves are served, and served INSIDE the style
			// element a browser applies LAST for the merchant — a rule in an
			// earlier block is CSS the template's own sheet then overrides
			// (card t_331059a8).
			style, ok := merchantStyleBlock(page)
			if !ok {
				t.Fatalf("the page has no <style> block at all — nothing to serve custom_css into")
			}
			if want := ".card { box-shadow: none; }"; !strings.Contains(style, want) {
				t.Errorf("the merchant's rule %q is not in the page's MERCHANT stylesheet (the last <style> "+
					"element), so the template's own sheet overrides it. Style blocks in document order:%s",
					want, allStyles(page))
			}
			if want := ".card { box-shadow: none; }"; !strings.Contains(page, want) {
				t.Errorf("the merchant's rule %q is not on the page at all (style blocks:%s)", want, allStyles(page))
			}
			if want := ".prod { border: 0; }"; !strings.Contains(style, want) {
				t.Errorf("the second rule %q of the same stylesheet is missing — the value is being truncated "+
					"somewhere before the whole stylesheet is served. Style blocks:%s", want, allStyles(page))
			}
		})
	}
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
// it. Each arm below asserts three things: no injected code survives, the page
// gains no style element beyond the merchant's own (no early close), and the
// legitimate CSS before the attempt still applies.
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
		// Nothing but the attempt: the guard truncates the value to empty, so
		// the merchant's <style> element must not be emitted at all — a
		// template that wrapped the value in an unconditional third <style>
		// would still be correct here, but this arm pins that an empty
		// stylesheet does not become an empty element.
		{"value that is nothing but an end tag", "</style><script>jfBreakout('x')</script>", ""},
	}

	for _, r := range themeRenderers() {
		t.Run(r.name, func(t *testing.T) {
			baseline := renderThemePage(t, r.tmpl, nil)
			baseStyles := strings.Count(baseline, "</style>")
			if baseStyles == 0 {
				t.Fatalf("the unthemed page has no closing </style> — this case could not tell an early close from a normal one")
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					page := renderThemePage(t, r.tmpl, map[string]any{"custom_css": tc.css})
					// The themed page carries ONE extra <style> — the merchant's
					// own sheet. Anything more means the value closed the style
					// element and the template's closer made a new one.
					wantClosers := baseStyles
					if tc.kept != "" {
						wantClosers++
					}
					// A marker that appears nowhere in either template's own script,
					// so this cannot pass on the page's legitimate JS.
					if strings.Contains(page, "jfBreakout") {
						t.Fatalf("custom_css %q got attacker-controlled code onto the page: the value left the "+
							"<style> block and the HTML parser took the rest as markup.\nstyle blocks:%s",
							tc.css, allStyles(page))
					}
					if got, opens := strings.Count(page, "</style>"), strings.Count(page, "<style>"); got != opens {
						t.Fatalf("custom_css %q left %d <style> opens but %d closers — an unbalanced style element is "+
							"what an early close looks like.\nstyle blocks:%s", tc.css, opens, got, allStyles(page))
					}
					if got := strings.Count(page, "</style>"); got != wantClosers {
						t.Fatalf("custom_css %q changed the number of </style> on the page (%d, want %d) — the value "+
							"closed the style element early.\nstyle blocks:%s", tc.css, got, wantClosers, allStyles(page))
					}
					if tc.kept != "" && !strings.Contains(page, tc.kept) {
						t.Errorf("custom_css %q lost the legitimate rule %q before the breakout attempt — the guard "+
							"should truncate the value, not discard the merchant's stylesheet.\nstyle blocks:%s",
							tc.css, tc.kept, allStyles(page))
					}
				})
			}
		})
	}
}

// TestMerchantCSSWinsTheCascadeOverTheBaseStylesheet is card t_331059a8: the
// merchant's stylesheet reached the page but was served in the <style> element
// ABOVE the template's base sheet, and equal-specificity CSS is resolved by
// document order — so every merchant declaration the base sheet also declares
// lost (measured on v1.21.281: `.card{padding:8px}` computed 32px,
// `body{background-color:rgb(18,52,86)}` computed rgb(247,248,248), and only
// `!important` appeared to work).
//
// Being "served" and being "applied" are different properties, so the earlier
// arm in this file cannot catch this: the rule was on the page, ZgotmplZ-free,
// inside a <style> element — just not the winning one. The assertion below is
// about the ORDER of the elements, which is the only thing that decides it.
func TestMerchantCSSWinsTheCascadeOverTheBaseStylesheet(t *testing.T) {
	// The prod reading, in the shape a merchant would write it.
	const css = ".card { padding: 8px; }\n  body { background-color: rgb(18,52,86); }"
	// Markers that identify the template's OWN sheet and the merchant's rules.
	const baseMarker = "color-scheme: dark"
	const cardRule = ".card { padding: 8px; }"
	const bodyRule = "body { background-color: rgb(18,52,86); }"

	for _, r := range themeRenderers() {
		t.Run(r.name, func(t *testing.T) {
			page := renderThemePage(t, r.tmpl, map[string]any{"custom_css": css})
			blocks := styleBlocks(page)
			if len(blocks) < 3 {
				t.Fatalf("the themed page has %d <style> element(s); the base sheet plus the merchant's own "+
					"sheet is 2, and the merchant's must be a LATER element than the base sheet so equal "+
					"specificity resolves in the merchant's favour.\nstyle blocks:%s", len(blocks), allStyles(page))
			}

			base, merchant := -1, -1
			for i, b := range blocks {
				if strings.Contains(b, baseMarker) {
					if base >= 0 {
						t.Fatalf("the base stylesheet appears in more than one <style> element (#%d and #%d) — "+
							"this assertion identifies it by one marker and cannot tell which one wins."+
							"\nstyle blocks:%s", base, i, allStyles(page))
					}
					base = i
				}
				if strings.Contains(b, cardRule) && strings.Contains(b, bodyRule) {
					merchant = i
				}
			}
			if base < 0 {
				t.Fatalf("no <style> element carries the template's base sheet (marker %q) — the shape of the "+
					"page changed and this test can no longer locate the sheet the merchant must beat."+
					"\nstyle blocks:%s", baseMarker, allStyles(page))
			}
			if merchant < 0 {
				t.Fatalf("the merchant's rules %q / %q are not together in one <style> element."+
					"\nstyle blocks:%s", cardRule, bodyRule, allStyles(page))
			}
			if merchant <= base {
				t.Fatalf("the merchant's stylesheet is <style> #%d and the template's base sheet is #%d — the base "+
					"sheet is served last, so every equal-specificity merchant declaration is overridden and a "+
					"merchant's `body { background-color: … }` silently does nothing (card t_331059a8)."+
					"\nstyle blocks:%s", merchant, base, allStyles(page))
			}
			if last := len(blocks) - 1; merchant != last {
				t.Fatalf("the merchant's stylesheet is <style> #%d but the page serves %d of them — template CSS "+
					"after the merchant's own sheet would override it again.\nstyle blocks:%s",
					merchant, len(blocks), allStyles(page))
			}
		})
	}
}

// TestUnthemedPageGainsNoExtraStyleElement is the no-custom_css control: the
// merchant's sheet is guarded by `{{if .ThemeCSS}}`, so a form without one must
// render exactly the elements it rendered before this card. A third, empty
// <style> on every page would be a silent change to every buyer's page.
func TestUnthemedPageGainsNoExtraStyleElement(t *testing.T) {
	for _, r := range themeRenderers() {
		t.Run(r.name, func(t *testing.T) {
			unthemed := styleBlocks(renderThemePage(t, r.tmpl, nil))
			if len(unthemed) != 2 {
				t.Fatalf("an unthemed page has %d <style> element(s), want 2 (theme tokens + base sheet) — the "+
					"merchant's sheet must not be emitted when there is none.\nstyle blocks:%s",
					len(unthemed), allStyles(renderThemePage(t, r.tmpl, nil)))
			}
			withOtherTheme := styleBlocks(renderThemePage(t, r.tmpl, map[string]any{
				"primary": "#5e6ad2", "spacing": "compact",
			}))
			if len(withOtherTheme) != 2 {
				t.Fatalf("a form themed WITHOUT custom_css has %d <style> element(s), want 2 — the `{{if .ThemeCSS}}` "+
					"guard keys on the stylesheet, not on the theme being present at all.\nstyle blocks:%s",
					len(withOtherTheme), allStyles(renderThemePage(t, r.tmpl, map[string]any{
						"primary": "#5e6ad2", "spacing": "compact"})))
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
	for _, r := range themeRenderers() {
		for _, tt := range themes {
			t.Run(r.name+"/"+tt.name, func(t *testing.T) {
				page := renderThemePage(t, r.tmpl, tt.theme)
				if strings.Contains(page, "ZgotmplZ") {
					t.Fatalf("a theme of shape %q put the `ZgotmplZ` sentinel on the page (style blocks:%s)",
						tt.name, allStyles(page))
				}
			})
		}
	}
}
