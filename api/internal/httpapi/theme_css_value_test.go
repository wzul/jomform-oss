package httpapi

// Regression guard for card t_61c7e145 — a single-value theme key whose value is
// a CSS FUNCTION (`background: rgb(200,0,0)`) never reached the buyer's page:
// the renderer served the literal `ZgotmplZ` sentinel in its place.
//
// THE DEFECT, reproduced on main 2a8c536b by seeding the theme straight into
// Postgres (no PATCH, no dashboard) and GETting the form's public URL:
//
//	row    theme = {"background":"rgb(200,0,0)","spacing":"spacious","primary":"#b45309"}   stored ✓
//	page   … body { background: ZgotmplZ; } … .card { padding: 48px; }                       colour ✗
//	computed body backgroundColor = rgb(247, 248, 248)  (the template default)
//
// Cause: `blockThemeView` HTML-escaped the single-value keys into plain
// `string`s, and the templates interpolate them INSIDE a `<style>` element,
// where html/template is in CSS text state and runs cssValueFilter. That filter
// rejects `(` (along with quotes, `/`, `;`, braces and brackets), so every
// function-valued colour failed it and became the sentinel. A hex value passes
// the same filter — which is why the dashboard's colour picker never showed it.
//
// The arms below execute the REAL templates against the view blockThemeView
// builds, plus the real `renderCustomForm` call site for the custom-HTML page
// shape, so a change to how the renderers type the value is a red here.
//
// These arms FAIL on the pre-fix bytes: the headline arm on the sentinel, the
// typing arm on `%T` (string, want template.CSS), and the breakout arm is the
// guard that verbatim emission makes necessary — without it `template.CSS`
// would happily close the style element and hand the rest to the HTML parser.

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"html/template"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// themeCSSFunctionTheme is the prod reading, in the shape a merchant stores it:
// the background and the card background are functions, the text colour is an
// hsl() function, the font stack carries a quoted family (the old
// HTMLEscapeString mangled the quote into `&#34;`, which CSS does not
// unescape), and the radius is a calc().
var themeCSSFunctionTheme = map[string]any{
	"background":         "rgb(200,0,0)",
	"text":               "hsl(210 40% 20%)",
	"card_background":    "rgba(255,255,255,0.9)",
	"card_border_radius": "calc(0.5rem + 2px)",
	"font_family":        `"Inter", system-ui, sans-serif`,
	"spacing":            "spacious",
	"primary":            "#b45309",
}

// themeCSSFunctionDeclarations is the declaration each key must be served as,
// byte for byte. A value that arrives mangled (escaped, quoted, truncated at
// the `(`) fails these strings even when no sentinel is emitted.
var themeCSSFunctionDeclarations = map[string]string{
	"background":         "body { background: rgb(200,0,0); }",
	"text":               "body { color: hsl(210 40% 20%); }",
	"card_background":    ".card { background: rgba(255,255,255,0.9); }",
	"card_border_radius": ".card { border-radius: calc(0.5rem + 2px); }",
	"font_family":        `body { font-family: "Inter", system-ui, sans-serif; }`,
}

// TestThemeCSSFunctionValueReachesTheDeclaration is the card's headline: a
// function-valued theme key is served as the merchant wrote it, on both block
// templates, with no sentinel anywhere on the page.
func TestThemeCSSFunctionValueReachesTheDeclaration(t *testing.T) {
	for _, r := range themeRenderers() {
		t.Run(r.name, func(t *testing.T) {
			page := renderThemePage(t, r.tmpl, themeCSSFunctionTheme)

			// 1. The sentinel the defect shipped must be gone — this is the
			// assertion that fails on the pre-fix bytes.
			if strings.Contains(page, "ZgotmplZ") {
				t.Fatalf("the rendered page still contains the html/template `ZgotmplZ` sentinel — a CSS "+
					"function in a single-value theme key is being filtered out of the CSS context instead of "+
					"served (card t_61c7e145).\nstyle blocks:%s", allStyles(page))
			}

			// 2. Each declaration is on the page, verbatim, in the element that
			// carries the theme's own layer. Card t_5016ade2 moved these
			// declarations OUT of the token sheet (`blocks[0]`, which is where
			// they lived when this arm was written) and into a theme layer
			// served AFTER the base sheet so they win the cascade — so the
			// sheet to read is the one holding them, not the first one. Locate
			// it by a declaration rather than by index: the index is exactly
			// the property this card changed.
			blocks := styleBlocks(page)
			if len(blocks) == 0 {
				t.Fatalf("the page has no <style> block at all")
			}
			layer := styleBlockIndex(blocks, func(b string) bool {
				return strings.Contains(b, themeCSSFunctionDeclarations["background"])
			})
			if layer < 0 {
				t.Fatalf("no <style> element carries theme[\"background\"]=%q — the merchant's value did "+
					"not reach the declaration.\nstyle blocks:%s",
					themeCSSFunctionTheme["background"], allStyles(page))
			}
			themeLayer := blocks[layer]
			for key, want := range themeCSSFunctionDeclarations {
				if !strings.Contains(themeLayer, want) {
					t.Errorf("theme[%q] is not served as %q — the merchant's value did not reach the "+
						"declaration.\nstyle blocks:%s", key, want, allStyles(page))
				}
			}

			// 3. The enum-valued keys still map to the template's own fixed CSS:
			// typing the colour keys must not disturb them.
			if want := ".card { padding: 48px; }"; !strings.Contains(themeLayer, want) {
				t.Errorf("theme spacing=spacious no longer produces %q — the fix touched a key whose value "+
					"the template never interpolates.\nstyle blocks:%s", want, allStyles(page))
			}
		})
	}
}

// TestThemeCSSValuesAreTypedForTheCSSContext pins the MECHANISM, not just the
// symptom: `template.CSS` is what tells html/template the value is already safe
// CSS. An HTML-context type (string) is re-filtered in a CSS context and a
// function-valued colour does not survive it — so a future "cleanup" that turns
// these back into plain strings would bring the sentinel back for exactly the
// values this card is about.
func TestThemeCSSValuesAreTypedForTheCSSContext(t *testing.T) {
	keys := []string{"ThemeBg", "ThemeText", "ThemeCardBg", "ThemeRadius", "ThemeFont"}

	themed := blockThemeView(&dbstore.Form{Theme: themeCSSFunctionTheme})
	for _, k := range keys {
		if got := fmt.Sprintf("%T", themed[k]); got != "template.CSS" {
			t.Errorf("blockThemeView's %s is %s, want template.CSS — inside <style> html/template runs "+
				"cssValueFilter, which rejects `(` and substitutes the `ZgotmplZ` sentinel for a CSS function "+
				"(card t_61c7e145)", k, got)
		}
	}

	// An unthemed form still gets the typed empty value, so every
	// `{{if .ThemeBg}}` guard keeps evaluating false and an unthemed page
	// renders exactly as before.
	empty := blockThemeView(&dbstore.Form{Theme: map[string]any{}})
	for _, k := range keys {
		css, ok := empty[k].(template.CSS)
		if !ok {
			t.Errorf("an unthemed form's %s is %T, want template.CSS", k, empty[k])
			continue
		}
		if css != "" {
			t.Errorf("an unthemed form's %s must stay empty so its {{if}} guard is false; got %q", k, css)
		}
	}

	// Shapes a stored theme can take besides a string (a hand-written row, or a
	// future key type) must not panic the renderer or put a sentinel on a page.
	for _, r := range themeRenderers() {
		for _, bad := range []any{nil, 42, map[string]any{"v": "rgb(1,2,3)"}} {
			page := renderThemePage(t, r.tmpl, map[string]any{"background": bad, "font_family": bad})
			if strings.Contains(page, "ZgotmplZ") {
				t.Errorf("%s: a non-string background (%T) put the `ZgotmplZ` sentinel on the page"+
					"\nstyle blocks:%s", r.name, bad, allStyles(page))
			}
		}
	}
}

// TestThemeCSSValueCannotBreakOutOfTheStyleBlock is the guard that
// `template.CSS` makes necessary: the value is now emitted verbatim, and a
// `<style>` is an HTML raw-text element whose ONLY terminator is a
// case-insensitive `</style`. So the renderer truncates at the first such
// sequence and never serves anything past it.
func TestThemeCSSValueCannotBreakOutOfTheStyleBlock(t *testing.T) {
	cases := []struct {
		name string
		key  string
		val  string
		// kept is the legitimate part before the breakout attempt, which must
		// still reach the declaration.
		kept string
	}{
		{
			name: "end tag followed by a script",
			key:  "background",
			val:  "rgb(1,2,3)</style><script>jfBreakout('x')</script>",
			kept: "body { background: rgb(1,2,3); }",
		},
		{
			name: "end tag upper case with whitespace",
			key:  "font_family",
			val:  `"Inter"</STYLE ><img src=x onerror=jfBreakout('x')>`,
			kept: `body { font-family: "Inter"; }`,
		},
		{
			name: "end tag with no closing bracket",
			key:  "card_background",
			val:  "rgba(0,0,0,.5)</style",
			kept: ".card { background: rgba(0,0,0,.5); }",
		},
		{
			name: "nothing but the attempt",
			key:  "text",
			val:  "</style><script>jfBreakout('x')</script>",
			kept: "",
		},
	}

	for _, r := range themeRenderers() {
		t.Run(r.name, func(t *testing.T) {
			baseline := renderThemePage(t, r.tmpl, nil)
			baseOpens := strings.Count(baseline, "<style>")
			if baseOpens == 0 || strings.Count(baseline, "</style>") != baseOpens {
				t.Fatalf("the unthemed page is not a reference for this case: %d <style> opens, %d closers",
					baseOpens, strings.Count(baseline, "</style>"))
			}
			// Every case below sets exactly ONE theme key, so the theme layer
			// (card t_5016ade2) adds exactly ONE <style> element: the themed
			// page must carry baseOpens+1 opens and closers. An early close
			// pushes the closers past that; a value that swallows the element
			// leaves too few of either.
			wantElements := baseOpens + 1
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					page := renderThemePage(t, r.tmpl, map[string]any{tc.key: tc.val})
					if strings.Contains(page, "jfBreakout") {
						t.Fatalf("theme[%q]=%q got attacker-controlled code onto the page: the value left the "+
							"<style> block and the HTML parser took the rest as markup.\nstyle blocks:%s",
							tc.key, tc.val, allStyles(page))
					}
					if got, opens := strings.Count(page, "</style>"), strings.Count(page, "<style>"); got != opens {
						t.Fatalf("theme[%q]=%q left %d <style> opens but %d closers — an unbalanced style element "+
							"is what an early close looks like.\nstyle blocks:%s", tc.key, tc.val, opens, got, allStyles(page))
					}
					if got := strings.Count(page, "</style>"); got != wantElements {
						t.Fatalf("theme[%q]=%q left %d <style> element(s) on the page, want %d (the base page's "+
							"%d plus the one theme layer) — the value closed the style element early or "+
							"swallowed it.\nstyle blocks:%s",
							tc.key, tc.val, got, wantElements, baseOpens, allStyles(page))
					}
					if tc.kept != "" {
						if !strings.Contains(page, tc.kept) {
							t.Errorf("theme[%q]=%q lost the legitimate value before the breakout attempt — the "+
								"guard should truncate the value, not discard it.\nstyle blocks:%s",
								tc.key, tc.val, allStyles(page))
						}
					} else if strings.Contains(page, "body { color: ; }") {
						t.Errorf("theme[%q]=%q that is nothing but an end tag produced an empty declaration "+
							"instead of dropping the guard's `{{if}}`.\nstyle blocks:%s", tc.key, tc.val, allStyles(page))
					}
				})
			}
		})
	}
}

// customFormPageWithTheme drives the REAL custom-HTML call site —
// PublicHandlers.renderCustomForm, which serveForm reaches for a published form
// carrying custom_html — and returns the page it wrote. The custom-HTML renderer
// had its own copy of the escaping bug, so it needs its own arm: a fix in the
// block renderers does not reach it.
func customFormPageWithTheme(t *testing.T, theme map[string]any) string {
	t.Helper()
	f := &dbstore.Form{
		Slug: "checkout", Title: "Custom HTML checkout", Status: dbstore.FormStatusPublished,
		Theme: theme,
		Config: map[string]any{
			"mode": "response",
			"custom_html": map[string]any{
				"html":   `<form><label>Name</label><input type="text" name="name" required>` + `<button type="submit">Send</button></form>`,
				"fields": []any{map[string]any{"field_id": "name", "type": "text", "label": "Name"}},
			},
		},
	}
	h := &PublicHandlers{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://demo.jomform.com/checkout", nil)
	if !h.renderCustomForm(rec, req, f) {
		t.Fatal("renderCustomForm declined a form with enabled custom_html — this surface is no longer under test")
	}
	return rec.Body.String()
}

// TestCustomHTMLPageServesTheCSSFunctionBackground is the same defect on the
// third form-page renderer: it read `theme["background"]` itself and
// HTML-escaped it, so the sentinel appeared there too.
func TestCustomHTMLPageServesTheCSSFunctionBackground(t *testing.T) {
	page := customFormPageWithTheme(t, map[string]any{"background": "rgb(200,0,0)"})
	if strings.Contains(page, "ZgotmplZ") {
		t.Fatalf("the custom-HTML page still serves the `ZgotmplZ` sentinel for a function-valued "+
			"theme[\"background\"] (card t_61c7e145).\nstyle blocks:%s", allStyles(page))
	}
	if want := "body { background: rgb(200,0,0); }"; !strings.Contains(page, want) {
		t.Fatalf("the custom-HTML page does not serve %q — the merchant's background did not reach the "+
			"declaration.\nstyle blocks:%s", want, allStyles(page))
	}
}
