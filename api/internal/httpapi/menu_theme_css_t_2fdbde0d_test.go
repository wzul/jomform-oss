package httpapi

// Card t_2fdbde0d: the restaurant-menu page's buyer-facing theme colours must
// reach the browser as CSS. They were interpolated into the page's <style>
// block as plain strings, so html/template ran cssValueFilter over them:
// `#4f46e5` was served as `%234f46e5`, which is not a declaration, so the
// browser DROPPED every themed rule (the Pay button lost its background and
// rendered white-on-grey).
//
// These tests assert the SERVED stylesheet, not the template source: a colour
// the browser cannot parse is the defect, and only the bytes matter. They go
// through formMenuTheme, the resolver the handler uses, so a value never
// reaches the sheet except the way production sends it (typed template.CSS by
// themeCSSValue, including its `</style` breakout guard).

import (
	"bytes"
	"html/template"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// grepLines returns only the lines of s that contain needle, for readable
// failure output on a served stylesheet (which is hundreds of lines).
func grepLines(s, needle string) string {
	var out []string
	for _, ln := range strings.Split(s, "\n") {
		if strings.Contains(ln, needle) {
			out = append(out, "  "+ln)
		}
	}
	if len(out) == 0 {
		return "  (none)"
	}
	return strings.Join(out, "\n")
}

// menuServedStyle returns the contents of the first <style> element in the
// served page — the sheet every themed rule lands in.
func menuServedStyle(t *testing.T, body string) string {
	t.Helper()
	open := strings.Index(body, "<style>")
	if open < 0 {
		t.Fatalf("served menu page has no <style> block")
	}
	open += len("<style>")
	close := strings.Index(body[open:], "</style>")
	if close < 0 {
		t.Fatalf("served menu page's <style> block is never closed")
	}
	return body[open : open+close]
}

// menuThemeFixture renders the menu page with a one-dish basket and the given
// form theme, resolved exactly as the handler resolves it (formMenuTheme), the
// cheapest way to get real template output (no DB).
func menuThemeFixture(t *testing.T, theme map[string]any) string {
	t.Helper()
	f := &dbstore.Form{Title: "Western Menu", Theme: theme}
	menu := &dbstore.MenuPage{
		Enabled:    true,
		Categories: []dbstore.MenuCategory{{Name: "Makanan", ProductIDs: []string{"p1"}}},
	}
	p := &dbstore.Product{ID: "p1", Name: "Nasi Lemak", PriceSen: 780, Active: true}
	h := &PublicHandlers{}
	data := workspaceMenuData{
		Slug: "western-menu", Menu: menu, Th: h.formMenuTheme(f, nil),
		Ls:    formLangs["en"],
		Table: "8",
		Categories: []menuCategoryView{{Name: "Makanan", Products: []menuProductView{
			h.menuProductView(p.ID, p.Name, p.Description, p.PriceSen, "", false,
				formLangs["en"].MenuBestBadge, formLangs["en"].MenuAdd),
		}}},
		HasItems: true,
	}
	var buf bytes.Buffer
	if err := menuPageTmpl.Execute(&buf, data); err != nil {
		t.Fatalf("menuPageTmpl.Execute: %v", err)
	}
	return buf.String()
}

// menuCSSRule returns the declaration body of the first `.sel {` rule in the
// served sheet, so an assertion names the rule the buyer sees rather than
// matching anywhere in 200 lines of CSS.
func menuCSSRule(t *testing.T, style, sel string) string {
	t.Helper()
	i := strings.Index(style, sel+" {")
	if i < 0 {
		t.Fatalf("served <style> has no %s rule", sel)
	}
	i += len(sel + " {")
	j := strings.Index(style[i:], "}")
	if j < 0 {
		t.Fatalf("served <style> %s rule is never closed", sel)
	}
	return style[i : i+j]
}

// TestMenuPayButtonKeepsItsThemeBackground is the card's headline: the CTA the
// buyer clicks must carry its fill as a declaration. Measured on prod, the
// `.paybtn` rule had no background at all and the button rendered white text
// on the UA's grey (contrast ≈1.2:1).
func TestMenuPayButtonKeepsItsThemeBackground(t *testing.T) {
	style := menuServedStyle(t, menuThemeFixture(t, map[string]any{"primary": "#b45309"}))
	rule := menuCSSRule(t, style, ".paybtn")
	if strings.Contains(rule, "%23") || strings.Contains(rule, "ZgotmplZ") {
		t.Errorf(".paybtn rule's theme colour was rewritten; the browser drops the declaration:\n%s", rule)
	}
	if !strings.Contains(rule, "background:#b45309") {
		t.Errorf(".paybtn rule does not carry the theme background:\n%s", rule)
	}
}

// TestMenuThemeColourReachesTheStylesheet is the card's reproduction: with a
// `#4f46e5` theme (the menu's own default accent), the served sheet must carry
// `background:#4f46e5` on the Pay button and must contain no `%23` anywhere.
func TestMenuThemeColourReachesTheStylesheet(t *testing.T) {
	body := menuThemeFixture(t, map[string]any{"primary": "#4f46e5"})
	style := menuServedStyle(t, body)

	if strings.Contains(style, "%23") {
		t.Errorf("served <style> percent-encodes the theme colour; the browser drops those rules.\n"+
			"offending lines:\n%s", grepLines(style, "%23"))
	}
	// The Pay/Checkout button is the CTA the buyer actually clicks; its fill is
	// the declaration that was measured missing on prod. The rest are the other
	// themed rules the card lists as inert.
	for _, want := range []string{
		"background:#4f46e5",               // .paybtn + .plus + .baropen
		"box-shadow:inset 3px 0 0 #4f46e5", // checked payment row's accent bar
		"outline:2px solid #4f46e5",        // focus rings
		"accent-color:#4f46e5",             // radio accent
		"color:#4f46e5",                    // info summary / section headings
	} {
		if !strings.Contains(style, want) {
			t.Errorf("served <style> missing %q", want)
		}
	}
}

// TestMenuThemeBackgroundColourReachesTheStylesheet covers the other theme
// colour on the page (the body background), interpolated separately.
func TestMenuThemeBackgroundColourReachesTheStylesheet(t *testing.T) {
	body := menuThemeFixture(t, map[string]any{"background": "#fafafa"})
	style := menuServedStyle(t, body)
	if !strings.Contains(style, "background:#fafafa") {
		t.Errorf("served <style> missing body background:%q", "#fafafa")
	}
	if strings.Contains(style, "%23") {
		t.Errorf("served <style> percent-encodes the theme background:\n%s", grepLines(style, "%23"))
	}
}

// TestMenuThemeColourAcceptsRGBNotZgotmplZ pins the same failure shape
// themeCSSValue was written for: cssValueFilter rejects `(`, so a perfectly
// ordinary `rgb(...)` theme colour became the ZgotmplZ sentinel.
func TestMenuThemeColourAcceptsRGBNotZgotmplZ(t *testing.T) {
	body := menuThemeFixture(t, map[string]any{"primary": "rgb(180, 83, 9)"})
	style := menuServedStyle(t, body)
	if strings.Contains(style, "ZgotmplZ") {
		t.Errorf("served <style> carries the ZgotmplZ sentinel instead of the merchant colour:\n%s",
			grepLines(style, "ZgotmplZ"))
	}
	if !strings.Contains(style, "background:rgb(180, 83, 9)") {
		t.Errorf("served <style> missing background:rgb(180, 83, 9)")
	}
}

// TestMenuThemeColourCannotCloseTheStyleElement is the guard that comes with
// typing the value template.CSS: verbatim emission must still not let a stored
// value leave the <style> element.
func TestMenuThemeColourCannotCloseTheStyleElement(t *testing.T) {
	body := menuThemeFixture(t, map[string]any{
		"primary": "#4f46e5</style><script>alert(1)</script>",
	})
	style := menuServedStyle(t, body)
	if strings.Contains(style, "</style") || strings.Contains(style, "<script") {
		t.Errorf("theme value escaped the <style> element:\n%s", style)
	}
	if strings.Contains(body, "alert(1)") {
		t.Errorf("theme value's script payload was served:\n%s", grepLines(body, "alert(1)"))
	}
}

// TestMenuStylesheetCommentCannotEndEarly guards the OTHER trigger behind the
// same symptom, measured while fixing this card: a `*/` inside a CSS comment
// (`mvar*/mvarstock`) ends the comment in both html/template's CSS tokenizer
// and the browser's, which left the rest of the sheet in a CSS STRING state —
// so even a template.CSS colour was percent-encoded there.
func TestMenuStylesheetCommentCannotEndEarly(t *testing.T) {
	style := menuServedStyle(t, menuThemeFixture(t, map[string]any{"primary": "#4f46e5"}))
	depth, i := 0, 0
	for i < len(style) {
		switch {
		case strings.HasPrefix(style[i:], "/*"):
			depth++
			i += 2
		case strings.HasPrefix(style[i:], "*/"):
			depth--
			if depth < 0 {
				t.Fatalf("served <style> has a stray */ at byte %d:\n%s",
					i, style[max(0, i-80):min(len(style), i+80)])
			}
			i += 2
		default:
			i++
		}
	}
	if depth != 0 {
		t.Errorf("served <style> has %d unclosed CSS comment(s); everything after it is tokenized as CSS garbage", depth)
	}
}

// TestFormMenuThemeTypesColoursForCSS checks the resolver that feeds the
// template: it must hand the sheet values typed for CSS, not plain strings
// that the escaper will rewrite.
func TestFormMenuThemeTypesColoursForCSS(t *testing.T) {
	f := &dbstore.Form{Title: "Western Menu", Theme: map[string]any{
		"primary":    "  #b45309  ",
		"background": "#fafafa",
	}}
	th := (&PublicHandlers{}).formMenuTheme(f, nil)
	if got, want := string(th.PrimaryColor), "#b45309"; got != want {
		t.Errorf("PrimaryColor = %q, want %q (trimmed, verbatim)", got, want)
	}
	if got, want := string(th.BackgroundColor), "#fafafa"; got != want {
		t.Errorf("BackgroundColor = %q, want %q", got, want)
	}
	var _ template.CSS = th.PrimaryColor
	var _ template.CSS = th.BackgroundColor
}
