package httpapi

// Regression guard for card t_5016ade2 — a form's THEME SETTINGS were served
// but never applied: every key the /form-settings panel writes (background,
// text, card_background, card_border_radius, font_family, layout, spacing) was
// emitted in the FIRST <style> element, ABOVE the template's base stylesheet.
// Equal-specificity CSS resolves by document order, so the base sheet won every
// declaration the theme also set:
//
//	style #0 (theme sheet)            style #1 (base sheet)
//	.card { padding: 48px; }          .card { background:#ffffff; …; padding:32px; }
//	body  { justify-content:center; }  body  { … display:flex; … padding:24px; }
//
// Measured in Chromium on a locally built API (577e6ba8) with a published
// response form whose theme was {"background":"rgb(200,0,0)","spacing":"spacious"}:
// `.card` computed padding 32px (the theme asked for 48px), i.e. a merchant who
// picked "spacious" on /form-settings saw a normal card.
//
// WHY THE EXISTING TESTS PASSED THROUGHOUT: form_patch_theme_keeps_test.go
// asserts that the theme CSS TEXT is served (`spacingCSS = ".card { padding:
// 32px; }"`), never that it WINS. "Served" and "applied" are different
// properties, exactly as they were for custom_css (card t_331059a8). The arms
// below therefore assert the ORDER of the <style> elements — the theme layer
// must be a LATER element than the base sheet, and still EARLIER than the
// merchant's own custom_css, which stays the escape hatch.
//
// They are DB-free on purpose: they execute the REAL templates against the view
// blockThemeView builds, which is the wiring the defect lived in. The end-to-end
// half (a real row through a real public GET, in a browser) is the card's
// measurement, quoted in the PR.

import (
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// baseSheetMarker identifies the template's BASE stylesheet — the sheet the
// theme layer has to beat. It is a rule only the base sheet declares, so a
// failure message can say which element is which.
const baseSheetMarker = "color-scheme: dark"

// themeCascadeDecls are the exact declarations the theme layer must carry for
// the theme below, written out rather than derived: the point is that a browser
// reading this layer applies THESE, and a helper that re-derived them from the
// template could drift without anything going red.
var themeCascadeDecls = []string{
	"body { background: #123456; }",
	"body { color: #101010; }",
	"body { font-family: Georgia; }",
	".card { background: #fefefe; }",
	".card { border-radius: 12px; }",
	"body { justify-content: flex-start; }",
	"body { padding: 0; }",
	".card { max-width: 100%; }",
	".card { padding: 48px; }",
}

// themeCascadeTheme is the shape the /form-settings panel writes for a merchant
// who picks a dark background and "spacious"/"full width". No CSS function
// values: a value holding one is a SEPARATE defect (card t_61c7e145) and mixing
// it in here would make this guard fail for a reason that is not its subject.
func themeCascadeTheme() map[string]any {
	return map[string]any{
		"background":         "#123456",
		"text":               "#101010",
		"card_background":    "#fefefe",
		"card_border_radius": "12px",
		"font_family":        "Georgia",
		"layout":             "full_width",
		"spacing":            "spacious",
		"primary":            "#b45309",
	}
}

// TestThemeSettingsWinTheCascadeOverTheBaseStylesheet is the card's headline:
// for BOTH public block renderers, the theme declarations live in a <style>
// element served AFTER the base sheet, and that element is the last one on a
// form with no custom_css — so nothing the template owns can override them.
func TestThemeSettingsWinTheCascadeOverTheBaseStylesheet(t *testing.T) {
	for _, r := range themeRenderers() {
		t.Run(r.name, func(t *testing.T) {
			page := renderThemePage(t, r.tmpl, themeCascadeTheme())
			if strings.Contains(page, "ZgotmplZ") {
				t.Fatalf("the page serves the html/template `ZgotmplZ` sentinel — a theme value was filtered "+
					"out of the CSS context instead of served (style blocks:%s)", allStyles(page))
			}
			blocks := styleBlocks(page)
			if len(blocks) < 3 {
				t.Fatalf("the themed page has %d <style> element(s); want at least 3 (token sheet + base sheet + "+
					"theme layer). The theme declarations were served in the token sheet before card t_5016ade2, "+
					"which is why they lost the cascade.\nstyle blocks:%s", len(blocks), allStyles(page))
			}

			base := styleBlockIndex(blocks, func(b string) bool { return strings.Contains(b, baseSheetMarker) })
			if base < 0 {
				t.Fatalf("no <style> element carries the template's base sheet (marker %q) — the shape of the "+
					"page changed and this test can no longer locate the sheet the theme must beat.\nstyle blocks:%s",
					baseSheetMarker, allStyles(page))
			}
			// The theme layer is the element carrying EVERY declaration above:
			// all-or-nothing, so a layer that lost one key (a broken `{{if}}`, a
			// key dropped from the view) cannot pass on the rest.
			layer := styleBlockIndex(blocks, func(b string) bool {
				for _, d := range themeCascadeDecls {
					if !strings.Contains(b, d) {
						return false
					}
				}
				return true
			})
			if layer < 0 {
				t.Fatalf("no single <style> element carries all of the theme's own declarations %v — the theme "+
					"settings are not served as one layer.\nstyle blocks:%s", themeCascadeDecls, allStyles(page))
			}
			if layer <= base {
				t.Fatalf("the theme layer is <style> #%d and the template's base sheet is #%d — the base sheet "+
					"is served LAST, so every equal-specificity theme declaration is overridden: a merchant who "+
					"picked spacing=spacious gets `.card{padding:32px}` from the base sheet, not the 48px they "+
					"asked for (card t_5016ade2).\nstyle blocks:%s", layer, base, allStyles(page))
			}
			if last := len(blocks) - 1; layer != last {
				t.Fatalf("the theme layer is <style> #%d but the page serves %d of them — template CSS after the "+
					"theme's own layer would override it again.\nstyle blocks:%s", layer, len(blocks), allStyles(page))
			}
		})
	}
}

// TestThemeSettingsLayerStaysBeforeTheMerchantStylesheet pins the other half of
// the order: custom_css is the merchant's escape hatch (card t_331059a8) and
// must still be served AFTER the theme layer, so a merchant rule that wants to
// override a theme setting can. A fix that appended the theme sheet last would
// silently demote custom_css.
func TestThemeSettingsLayerStaysBeforeTheMerchantStylesheet(t *testing.T) {
	theme := themeCascadeTheme()
	theme["custom_css"] = ".card { padding: 8px; }"

	for _, r := range themeRenderers() {
		t.Run(r.name, func(t *testing.T) {
			page := renderThemePage(t, r.tmpl, theme)
			blocks := styleBlocks(page)
			if len(blocks) != 4 {
				t.Fatalf("a form with theme settings AND custom_css has %d <style> element(s), want 4 (token "+
					"sheet, base sheet, theme layer, merchant sheet).\nstyle blocks:%s", len(blocks), allStyles(page))
			}
			base := styleBlockIndex(blocks, func(b string) bool { return strings.Contains(b, baseSheetMarker) })
			layer := styleBlockIndex(blocks, func(b string) bool { return strings.Contains(b, ".card { padding: 48px; }") })
			merchant := styleBlockIndex(blocks, func(b string) bool { return strings.Contains(b, ".card { padding: 8px; }") })
			if base < 0 || layer < 0 || merchant < 0 {
				t.Fatalf("could not locate all three layers (base=%d, theme=%d, merchant=%d).\nstyle blocks:%s",
					base, layer, merchant, allStyles(page))
			}
			if !(base < layer && layer < merchant) {
				t.Fatalf("the layers are out of order: base <style> #%d, theme <style> #%d, merchant <style> #%d. "+
					"Want base < theme < merchant so the theme settings apply but the merchant's own custom_css "+
					"still wins.\nstyle blocks:%s", base, layer, merchant, allStyles(page))
			}
			if last := len(blocks) - 1; merchant != last {
				t.Fatalf("the merchant's stylesheet is <style> #%d but the page serves %d — CSS after it would "+
					"override the merchant's own rules again.\nstyle blocks:%s", merchant, len(blocks), allStyles(page))
			}
		})
	}
}

// TestThemeSettingsAbsentGainsNoThemeLayer is the control: the theme layer is
// guarded by one flag (ThemeSheet), so a form that sets none of those keys keeps
// the element count it had before this card. A fourth, empty <style> on every
// page would be a silent change to every buyer's page.
func TestThemeSettingsAbsentGainsNoThemeLayer(t *testing.T) {
	themes := []struct {
		name  string
		theme map[string]any
	}{
		{"no theme at all", nil},
		{"theme with only the brand colour", map[string]any{"primary": "#5e6ad2"}},
		{"theme with only custom_css", map[string]any{"custom_css": ".card { padding: 8px; }"}},
	}
	for _, r := range themeRenderers() {
		for _, tt := range themes {
			t.Run(r.name+"/"+tt.name, func(t *testing.T) {
				blocks := styleBlocks(renderThemePage(t, r.tmpl, tt.theme))
				want := 2
				if tt.theme != nil {
					if _, ok := tt.theme["custom_css"]; ok {
						want = 3 // token sheet + base sheet + the merchant's own sheet
					}
				}
				if len(blocks) != want {
					t.Fatalf("a form with %s has %d <style> element(s), want %d — the theme layer keys on the "+
						"theme SETTINGS, not on the theme being present at all.\nstyle blocks:%s",
						tt.name, len(blocks), want, allStyles(renderThemePage(t, r.tmpl, tt.theme)))
				}
			})
		}
	}
}

// TestThemeSheetFlagIsOnOnlyForSettings pins the guard itself. Without it the
// templates cannot tell "a merchant set spacing=spacious" from "the row happens
// to carry a primary colour", and the layer would either be emitted empty on
// every page or not at all.
func TestThemeSheetFlagIsOnOnlyForSettings(t *testing.T) {
	cases := []struct {
		name  string
		theme map[string]any
		want  bool
	}{
		{"no theme", nil, false},
		{"brand colour only", map[string]any{"primary": "#5e6ad2"}, false},
		{"custom_css only", map[string]any{"custom_css": ".card{}"}, false},
		{"custom_js only", map[string]any{"custom_js": "1;"}, false},
		{"background", map[string]any{"background": "#123456"}, true},
		{"text", map[string]any{"text": "#101010"}, true},
		{"card_background", map[string]any{"card_background": "#fefefe"}, true},
		{"card_border_radius", map[string]any{"card_border_radius": "12px"}, true},
		{"font_family", map[string]any{"font_family": "Georgia"}, true},
		{"layout", map[string]any{"layout": "full_width"}, true},
		{"spacing", map[string]any{"spacing": "spacious"}, true},
		// A stored value that is not a string is treated as absent, not as a
		// reason to emit a layer whose rule would be empty.
		{"non-string background", map[string]any{"background": 42}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			view := blockThemeView(&dbstore.Form{Theme: tc.theme})
			got, ok := view["ThemeSheet"].(bool)
			if !ok {
				t.Fatalf("blockThemeView's ThemeSheet is %T, want bool — the templates guard the theme layer on it",
					view["ThemeSheet"])
			}
			if got != tc.want {
				t.Fatalf("ThemeSheet = %v for theme %v, want %v", got, tc.theme, tc.want)
			}
		})
	}
}

// TestCustomHTMLThemeBackgroundWinsTheCascade is the same defect in the third
// public renderer (render_custom.go, customFormTmpl): its theme `background`
// declaration sat in the token sheet ABOVE the base sheet, so
// `body { … background:#f7f8f8 … }` won on document order. It drives the REAL
// call site (renderCustomForm) rather than the template, because the wiring —
// which key reaches the view — is half of what was wrong in this renderer
// before (card t_6efd84e2).
func TestCustomHTMLThemeBackgroundWinsTheCascade(t *testing.T) {
	_, page := renderCustomHTMLJS(t, customHTMLForm(map[string]any{"background": "#123456"}))
	blocks := styleBlocks(page)
	if len(blocks) < 3 {
		t.Fatalf("the themed custom-HTML page has %d <style> element(s); want at least 3 (token sheet + base "+
			"sheet + theme background).\nstyle blocks:%s", len(blocks), allStyles(page))
	}
	base := styleBlockIndex(blocks, func(b string) bool { return strings.Contains(b, baseSheetMarker) })
	layer := styleBlockIndex(blocks, func(b string) bool { return strings.Contains(b, "body { background: #123456; }") })
	if base < 0 || layer < 0 {
		t.Fatalf("could not locate the base sheet (%d) and the theme background (%d).\nstyle blocks:%s",
			base, layer, allStyles(page))
	}
	if layer <= base {
		t.Fatalf("the theme background is <style> #%d and the base sheet is #%d — the base sheet is served "+
			"last, so `body { … background:#f7f8f8 … }` overrides the merchant's background and the custom-HTML "+
			"page ignores the theme entirely (card t_5016ade2).\nstyle blocks:%s", layer, base, allStyles(page))
	}
	if last := len(blocks) - 1; layer != last {
		t.Fatalf("the theme background is <style> #%d but the page serves %d — CSS after it would override the "+
			"merchant's background again.\nstyle blocks:%s", layer, len(blocks), allStyles(page))
	}
}

// TestCustomHTMLThemeBackgroundAbsentAddsNoElement is the control for the same
// template: an unthemed custom-HTML page keeps its two <style> elements.
func TestCustomHTMLThemeBackgroundAbsentAddsNoElement(t *testing.T) {
	_, page := renderCustomHTMLJS(t, customHTMLForm(nil))
	if got := len(styleBlocks(page)); got != 2 {
		t.Fatalf("an unthemed custom-HTML page has %d <style> element(s), want 2.\nstyle blocks:%s",
			got, allStyles(page))
	}
}

// styleBlockIndex returns the index of the first block matching pred, or -1.
func styleBlockIndex(blocks []string, pred func(string) bool) int {
	for i, b := range blocks {
		if pred(b) {
			return i
		}
	}
	return -1
}
