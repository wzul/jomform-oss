package httpapi

// The public (Go-rendered) form's submit CTA, in all four renderers.
//
// HISTORY — two cards, one subject.
//
//	#991 (t_9efae9f0) pinned the CTA's HOVER fill: the templates carried three
//	literal copies of `button:hover { background:#828fff; }`, and #828fff carries
//	the button's white label at 2.87:1 — below the AA 4.5:1 floor, in the one
//	state every pointer lands on before it clicks. The guard that shipped with
//	that fix was a SOURCE SCAN for the literal pairs.
//
//	t_8f5d6fb5 (this card) found the deeper defect the scan could not see: all
//	three templates declared the merchant's brand colour as
//	`:root { --jf-primary: … }` and consumed it NOWHERE (a repo-wide grep for
//	`var(--jf-primary` in api/ returned 0 — the string had never existed there),
//	while the CTA fill was a hard-coded #5e6ad2. A merchant who picked a colour
//	on /form-settings got the default indigo. That also made #991's literal scan
//	structurally unable to help: the fill it scanned for no longer described the
//	button once the fill became a token.
//
// WHAT THIS GUARD IS NOW
//   The scan is replaced by a RENDER assertion that drives the real call sites
//   (`PublicHandlers.renderBlocks` / `renderResponseForm` / `renderCustomForm`,
//   each with a theme set) and reads the emitted stylesheet, so it answers the
//   question #991's scan could not: does a form with `theme.primary` set paint
//   its CTA that colour? A theme the renderer forgets to thread through — the
//   exact shape of this defect — is a red here, whether it is the token
//   declaration, the map entry, or the CSS rule that was dropped.
//
//   The literal scan is KEPT, narrowed to the one thing a render cannot see:
//   a FOURTH Go template that hard-codes a fill instead of consuming the token.
//   That is the shape the original defect had three copies of.
//
// WHAT IS PINNED
//   * a themed form's CTA fill IS the merchant's colour, per renderer;
//   * the hover / pressed fills carry the button's LABEL at AA, measured on the
//     quantised hex the stylesheet actually paints;
//   * the label itself clears AA on the fill in all three states (which is why
//     a light brand colour gets ink text rather than white — amber #f59e0b
//     cannot carry white at any lightness);
//   * the unthemed default resolves to the SAME triple the web CTA uses
//     (`--jf-primary` / `--jf-primary-hover` / `--jf-primary-active` in
//     web/src/app/globals.css, read here), so the two cannot drift apart;
//   * no Go-rendered stylesheet paints a hard-coded CTA fill, and the
//     pre-#991 fill #828fff is gone as a fill everywhere;
//   * the brand colour as TEXT — `--jf-primary-text`, the token the price, the
//     footer links and the confirmation link paint — clears AA on the surface
//     those texts actually land on, the selected-card tint `#eef2ff`, for every
//     brand colour AND the unthemed default, and the derivation's own
//     `surfaceTint` is that painted surface (card t_48c36a54: before that guard
//     nothing in the repo read this token at all, so the comment claiming the
//     4.5 floor was measured there had no test behind it);
//   * a non-vacuity floor, so a renamed rule cannot read as a pass.

import (
	"fmt"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// ── measurement ──────────────────────────────────────────────────────────────

// wcagRelativeLuminance is the WCAG 2.x definition, per channel:
// https://www.w3.org/TR/WCAG21/#dfn-relative-luminance
func wcagRelativeLuminance(r, g, b uint8) float64 {
	lin := func(c uint8) float64 {
		v := float64(c) / 255.0
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

func hexLuminance(hex string) (float64, error) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 0, fmt.Errorf("not a 6-digit hex colour: %q", hex)
	}
	var rgb [3]uint8
	for i := 0; i < 3; i++ {
		var v int
		if _, err := fmt.Sscanf(hex[i*2:i*2+2], "%02x", &v); err != nil {
			return 0, fmt.Errorf("bad hex colour %q: %w", hex, err)
		}
		rgb[i] = uint8(v)
	}
	return wcagRelativeLuminance(rgb[0], rgb[1], rgb[2]), nil
}

func contrastRatio(a, b string) (float64, error) {
	la, err := hexLuminance(a)
	if err != nil {
		return 0, err
	}
	lb, err := hexLuminance(b)
	if err != nil {
		return 0, err
	}
	hi, lo := math.Max(la, lb), math.Min(la, lb)
	return (hi + 0.05) / (lo + 0.05), nil
}

// The measured pair (card t_9efae9f0). #5e6ad2 is the default resting fill,
// #616dd5 the default hover — the lightest step on that hue whose white label
// still clears AA (4.51:1) — and #454ea8 the pressed fill.
const (
	restFill  = "#5e6ad2"
	hoverFill = "#616dd5"
	white     = "#ffffff"
)

// ── reading the emitted stylesheet ───────────────────────────────────────────

var (
	// The `:root` token block the three templates emit.
	rootBlockRe = regexp.MustCompile(`:root\s*\{([^}]*)\}`)
	// One `--jf-primary*: #hex` declaration inside it.
	cssVarRe = regexp.MustCompile(`--(jf-primary[a-z-]*)\s*:\s*(#[0-9a-fA-F]{6})`)
	// The CTA rules. The body is captured so the fill can come from a var()
	// token (today) or from a literal (a regression).
	buttonRe      = regexp.MustCompile(`(?m)^\s*button\s*\{([^}]*)\}`)
	buttonHoverRe = regexp.MustCompile(`(?m)^\s*button:hover\s*\{([^}]*)\}`)
	buttonActRe   = regexp.MustCompile(`(?m)^\s*button:active\s*\{([^}]*)\}`)
	// `background: …` / `color: …` out of a rule body, either a var() token or
	// a literal hex.
	backgroundColorRe = regexp.MustCompile(`(?:^|;)\s*background\s*:\s*([^;]+?)\s*(?:;|$)`)
	textColorRe       = regexp.MustCompile(`(?:^|;)\s*color\s*:\s*([^;]+?)\s*(?:;|$)`)
	// A hard-coded fill in a stylesheet, which is the shape this card removed.
	hardCodedFillRe = regexp.MustCompile(`background\s*:\s*#[0-9a-fA-F]{3,6}`)
	// `var(--name)`.
	varRefRe = regexp.MustCompile(`var\(\s*--([a-z0-9-]+)\s*\)`)
)

// cssResolver resolves the var() tokens a rule body references, plus the literals
// the body carries directly.
type cssResolver struct {
	vars map[string]string
}

func newCSSResolver(html string) (*cssResolver, error) {
	r := &cssResolver{vars: map[string]string{}}
	m := rootBlockRe.FindStringSubmatch(html)
	if m == nil {
		return nil, fmt.Errorf("the rendered page carries no `:root { … }` token block")
	}
	for _, decl := range cssVarRe.FindAllStringSubmatch(m[1], -1) {
		r.vars[decl[1]] = decl[2]
	}
	if len(r.vars) == 0 {
		return nil, fmt.Errorf("the `:root` block declares no --jf-primary* token: %q", m[1])
	}
	return r, nil
}

// value resolves a CSS value: a `var(--x)` to what --x is set to, anything else
// (a literal hex) verbatim. A var() that is not declared resolves to "" so the
// caller reports it instead of silently comparing empty strings.
func (r *cssResolver) value(v string) string {
	v = strings.TrimSpace(v)
	if m := varRefRe.FindStringSubmatch(v); m != nil {
		return r.vars[m[1]]
	}
	return v
}

// ruleValue reads `prop` out of a rule body via `re`, resolving tokens.
func (r *cssResolver) ruleValue(ruleRe *regexp.Regexp, html string, prop *regexp.Regexp) (string, bool) {
	m := ruleRe.FindStringSubmatch(html)
	if m == nil {
		return "", false
	}
	p := prop.FindStringSubmatch(m[1])
	if p == nil {
		return "", false
	}
	return r.value(p[1]), true
}

// ── rendering the real call sites ────────────────────────────────────────────

// rendererCase is one Go-rendered public form route.
type rendererCase struct {
	name string
	// render produces the page for a form themed `primary`.
	render func(t *testing.T, primary string) string
}

// themedBlockForm builds the form the block renderer needs: a config with at
// least one block, and the theme colour under test.
func themedForm(slug, primary string, config map[string]any) *dbstore.Form {
	return &dbstore.Form{
		Slug: slug, Title: "Themed form", Status: dbstore.FormStatusPublished,
		Config: config,
		Theme:  map[string]any{"primary": primary, "background": "#ffffff"},
	}
}

// rendererCases drives the FOUR PUBLIC call sites — not the templates by hand —
// so a missing map entry or a dropped rule at the call site is a red, which is
// exactly the shape this card's defect had (the token was declared and the
// consumer was never wired).
//
// The fourth is card t_d48f0930's surface: the LAST-RESORT product-checkbox
// layout (`renderLegacy` → `formTmpl`), reached when a published payment form
// carries no builder blocks, is not response-mode and has no enabled menu. It
// was the one Go form renderer that read `f.Theme` zero times; driving it here
// means a merchant colour that stops reaching it is a red, not a silent
// default.
func rendererCases() []rendererCase {
	blockCfg := map[string]any{"blocks": []any{map[string]any{"type": "text", "value": "Hello"}}}
	respCfg := map[string]any{
		"mode":   "response",
		"blocks": []any{map[string]any{"type": "email", "value": "Email"}},
	}
	customCfg := map[string]any{
		"mode": "response",
		"custom_html": map[string]any{
			"html":   `<input type="text" name="name">`,
			"fields": []any{map[string]any{"field_id": "name", "type": "text", "label": "Name"}},
		},
	}
	// A `{description, products[]}` config with NO `blocks` key is the shape
	// `serveForm` falls through on: `renderBlocks` returns false and
	// `renderLegacy` takes the page (public.go, the `h.renderLegacy` tail).
	legacyCfg := map[string]any{
		"description": "Order something",
		"products": []any{map[string]any{
			"id": "p1", "name": "Item", "price_sen": 2500, "active": true,
		}},
	}
	return []rendererCase{
		{"block product form (render_blocks.go:blockTmpl)", func(t *testing.T, primary string) string {
			h := &PublicHandlers{}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "http://demo.jomform.com/demo", nil)
			if !h.renderBlocks(rec, req, themedForm("demo", primary, blockCfg)) {
				t.Fatal("renderBlocks returned false for a form with blocks")
			}
			return rec.Body.String()
		}},
		{"response-only form (render_blocks.go:responseTmpl)", func(t *testing.T, primary string) string {
			h := &PublicHandlers{}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "http://demo.jomform.com/resp", nil)
			if !h.renderResponseForm(rec, req, themedForm("resp", primary, respCfg)) {
				t.Fatal("renderResponseForm returned false for a response-mode form")
			}
			return rec.Body.String()
		}},
		{"custom-HTML form (render_custom.go:customFormTmpl)", func(t *testing.T, primary string) string {
			h := &PublicHandlers{}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "http://demo.jomform.com/custom", nil)
			if !h.renderCustomForm(rec, req, themedForm("custom", primary, customCfg)) {
				t.Fatal("renderCustomForm returned false for a custom-HTML form")
			}
			return rec.Body.String()
		}},
		{"legacy product-checkbox form (public.go:renderLegacy/formTmpl)", func(t *testing.T, primary string) string {
			h := &PublicHandlers{}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "http://demo.jomform.com/legacy", nil)
			f := themedForm("legacy", primary, legacyCfg)
			// Asserted, not assumed: this form must really fall THROUGH the
			// block renderer, or the case would be rendering the block layout
			// and calling it legacy.
			if h.renderBlocks(httptest.NewRecorder(), httptest.NewRequest("GET", "http://demo.jomform.com/legacy", nil), f) {
				t.Fatal("renderBlocks handled a form with no blocks — this case is no longer the legacy path")
			}
			h.renderLegacy(rec, req, f)
			return rec.Body.String()
		}},
	}
}

// brandColours are the brand colours the guard renders. The first two are the
// LIVE forms the card measured on prod (`event-registration` #f59e0b,
// `donation` #10b981); the rest bracket the cases the derivation has to handle —
// the indigo default, a dark brand, a saturated red, a very light brand and
// both extremes.
var brandColours = []string{
	"#f59e0b", // the card's live `event-registration` form
	"#10b981", // the card's live `donation` form
	"#5e6ad2", // the default / the form-settings starting colour
	"#1e3a8a", // dark brand: white label
	"#e11d48", // saturated: near the AA boundary for white
	"#ef4444", // the app's danger colour
	"#e6e8ec", // very light brand
}

// TestThemedPublicFormCtaIsTheMerchantColour is the guard this card asks for: a
// form with `theme.primary` set must render a CTA painted that colour.
func TestThemedPublicFormCtaIsTheMerchantColour(t *testing.T) {
	for _, rc := range rendererCases() {
		for _, primary := range brandColours {
			html := rc.render(t, primary)
			res, err := newCSSResolver(html)
			if err != nil {
				t.Errorf("%s / theme %s: %v", rc.name, primary, err)
				continue
			}
			if got := res.vars["jf-primary"]; !strings.EqualFold(got, primary) {
				t.Errorf("%s: a form themed %s rendered `--jf-primary: %s` — the merchant's colour is declared but not the one painted", rc.name, primary, got)
			}
			// The fill itself, resolved through the token, so a template that
			// declared the token and then painted a literal is a red.
			fill, ok := res.ruleValue(buttonRe, html, backgroundColorRe)
			if !ok {
				t.Errorf("%s: the emitted stylesheet has no `button { … background: … }` rule — re-anchor this guard", rc.name)
				continue
			}
			if !strings.EqualFold(fill, primary) {
				t.Errorf("%s: the CTA fill resolves to %s for a form themed %s — the submit button ignores the merchant's colour", rc.name, fill, primary)
			}
		}
	}
}

// TestThemedPublicFormCtaStatesClearAA measures the rendered triple of every
// brand colour: the label must clear AA on the fill, the hover AND the pressed
// state, and the hover and pressed fills must be distinguishable from the rest.
func TestThemedPublicFormCtaStatesClearAA(t *testing.T) {
	for _, rc := range rendererCases() {
		for _, primary := range brandColours {
			html := rc.render(t, primary)
			res, err := newCSSResolver(html)
			if err != nil {
				t.Errorf("%s / theme %s: %v", rc.name, primary, err)
				continue
			}
			label := res.vars["jf-primary-label"]
			if label == "" {
				t.Errorf("%s / theme %s: the stylesheet declares no --jf-primary-label; the button's text colour is not a token", rc.name, primary)
				continue
			}
			// The label the button rule actually paints must be the token.
			painted, ok := res.ruleValue(buttonRe, html, textColorRe)
			if !ok {
				t.Errorf("%s: the emitted `button { … }` rule carries no color", rc.name)
				continue
			}
			if !strings.EqualFold(painted, label) {
				t.Errorf("%s / theme %s: `button { color: %s }` is not the declared label token %s", rc.name, primary, painted, label)
			}

			states := []struct {
				name string
				rule *regexp.Regexp
				key  string
			}{
				{"rest", buttonRe, "jf-primary"},
				{"hover", buttonHoverRe, "jf-primary-hover"},
				{"pressed", buttonActRe, "jf-primary-active"},
			}
			for _, st := range states {
				fill, ok := res.ruleValue(st.rule, html, backgroundColorRe)
				if !ok {
					t.Errorf("%s / theme %s: no `button%s { … background: … }` rule in the emitted stylesheet", rc.name, primary, st.name)
					continue
				}
				if !strings.EqualFold(fill, res.vars[st.key]) {
					t.Errorf("%s / theme %s: the %s fill %s is not the token --%s (%s)", rc.name, primary, st.name, fill, st.key, res.vars[st.key])
				}
				ratio, err := contrastRatio(label, fill)
				if err != nil {
					t.Errorf("%s / theme %s / %s: %v", rc.name, primary, st.name, err)
					continue
				}
				if ratio < 4.5 {
					t.Errorf("%s / theme %s: the button label %s on the %s fill %s measures %.2f:1 — below the AA 4.5:1 floor", rc.name, primary, label, st.name, fill, ratio)
				}
			}
			// The three states must be three colours: a brand colour with no
			// room on its hue would otherwise render a button whose hover and
			// press are invisible.
			if strings.EqualFold(res.vars["jf-primary"], res.vars["jf-primary-hover"]) &&
				strings.EqualFold(res.vars["jf-primary"], res.vars["jf-primary-active"]) {
				t.Errorf("%s / theme %s: rest, hover and pressed all resolve to %s — the states are indistinguishable", rc.name, primary, res.vars["jf-primary"])
			}
		}
	}
}

// TestPublicFormDefaultCtaMatchesWebCta pins the unthemed default against the
// dashboard's own tokens, read from the shipped stylesheet rather than from a
// second copy of the constants: the public form and the web CTA must not drift
// into two different pairs.
func TestPublicFormDefaultCtaMatchesWebCta(t *testing.T) {
	css, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "src", "app", "globals.css"))
	if err != nil {
		t.Fatalf("reading the web CTA tokens: %v (the guard needs the shipped stylesheet, it does not guess)", err)
	}
	want := map[string]string{}
	for _, name := range []string{"jf-primary", "jf-primary-hover", "jf-primary-active"} {
		re := regexp.MustCompile(`--` + name + `\s*:\s*(#[0-9a-fA-F]{6})`)
		m := re.FindStringSubmatch(string(css))
		if m == nil {
			t.Fatalf("web/src/app/globals.css declares no --%s; the guard cannot compare the two CTAs", name)
		}
		want[name] = m[1]
	}
	if !strings.EqualFold(want["jf-primary"], defaultPrimary) ||
		!strings.EqualFold(want["jf-primary-hover"], defaultPrimaryHover) ||
		!strings.EqualFold(want["jf-primary-active"], defaultPrimaryActive) {
		t.Errorf("the public form's defaults (%s / %s / %s) are not the web CTA's (%s / %s / %s)",
			defaultPrimary, defaultPrimaryHover, defaultPrimaryActive,
			want["jf-primary"], want["jf-primary-hover"], want["jf-primary-active"])
	}
	// And the rendered UNTHEMED page must emit exactly that triple — the
	// constants above are not what the buyer's page paints.
	for _, rc := range rendererCases() {
		html := rc.render(t, "")
		res, err := newCSSResolver(html)
		if err != nil {
			t.Errorf("%s (unthemed): %v", rc.name, err)
			continue
		}
		for _, name := range []string{"jf-primary", "jf-primary-hover", "jf-primary-active"} {
			if got := res.vars[name]; !strings.EqualFold(got, want[name]) {
				t.Errorf("%s (unthemed): --%s renders %s, but the web CTA's is %s", rc.name, name, got, want[name])
			}
		}
	}
}

// TestPublicCtaHoverClearsAA keeps the #991 measurement itself: the default
// pair's ratios, reproduced from the hex values.
func TestPublicCtaHoverClearsAA(t *testing.T) {
	ratio, err := contrastRatio(white, hoverFill)
	if err != nil {
		t.Fatalf("computing the AA ratio: %v", err)
	}
	if ratio < 4.5 {
		t.Fatalf("the public form's white label on %s is %.2f:1 — below the AA 4.5:1 floor (card t_9efae9f0)", hoverFill, ratio)
	}
	restRatio, err := contrastRatio(white, restFill)
	if err != nil {
		t.Fatalf("computing the resting ratio: %v", err)
	}
	if restRatio < 4.5 {
		t.Fatalf("the public form's white label on %s is %.2f:1 — below AA", restFill, restRatio)
	}
	// The default hover must stay a LIGHTER step than the resting fill: that
	// direction is what the design uses to say "the pointer is here", and the
	// fix must not have reached AA by going darker (which would ALSO read as a
	// press).
	lH, _ := hexLuminance(hoverFill)
	lR, _ := hexLuminance(restFill)
	if lH <= lR {
		t.Fatalf("the hover fill %s is not lighter than the resting fill %s — the AA fix must not invert the state direction", hoverFill, restFill)
	}
	// #991's own headline measurement, reproduced.
	if got, _ := contrastRatio(white, "#828fff"); math.Abs(got-2.87) > 0.01 {
		t.Fatalf("the card's 2.87:1 measurement for #828fff did not reproduce: got %.2f", got)
	}
}

// renderedStylesheets walks the package's Go sources rather than naming the
// files, so a FOURTH copy of the CSS is scanned because it exists.
func renderedStylesheets(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[path] = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("walking the package for Go sources: %v", err)
	}
	return out
}

// ── source scan: the "no token declared" hole ────────────────────────────────

// formTemplateLiterals returns every `<!doctype html` raw-string literal in the
// package's Go sources, keyed "<file> #<n>" so a failure names the site by
// content rather than by a line that rots. The extraction is by backtick
// pairing: in these files a backtick only ever DELIMITS a raw string (none
// occurs inside one, and a `//` comment carrying one is never on a line that
// also carries a delimiter), so an odd index opens a literal and the next even
// one closes it.
//
// Literal scope — not FILE scope — is the whole point. public.go holds BOTH the
// legacy merchant form template and the private gate's own `fmt.Fprintf` page;
// a file-scoped scan cannot tell the merchant's form from the platform page that
// shares the file, which is how the gate's fixed `#5e6ad2` button reads as a
// violation of a merchant-form rule it was never subject to.
func formTemplateLiterals(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for path, src := range renderedStylesheets(t) {
		parts := strings.Split(src, "`")
		n := 0
		for i := 1; i+1 < len(parts); i += 2 {
			body := parts[i]
			if !strings.Contains(body, "<!doctype html") {
				continue
			}
			n++
			out[fmt.Sprintf("%s #%d", path, n)] = body
		}
	}
	if len(out) == 0 {
		t.Fatalf("found no Go-rendered page literal in the package — the extraction is not looking at the sources it was written for, so a green run proves nothing")
	}
	return out
}

// merchantFormTemplates identifies the four Go-rendered merchant form pages
// inside their own literal, by a marker only that template carries. Each marker
// is asserted to match EXACTLY ONE literal, so a renamed field fails loudly
// instead of quietly dropping a surface out of the scan.
var merchantFormTemplates = []struct {
	name, file, marker string
}{
	{"formTmpl (legacy product-checkbox layout)", "public.go", "{{.Ls.NameLabel}}"},
	{"blockTmpl (block product form)", "render_blocks.go", `<button type="submit" form="of">`},
	{"responseTmpl (response-only form)", "render_blocks.go", `type="submit">{{$.Ls.SubmitResponse}}`},
	{"customFormTmpl (custom-HTML form)", "render_custom.go", `id="jf-submit"`},
}

// ctaExemptPageLiterals are Go-rendered pages that draw a submit button and are
// NOT judged by the merchant brand-token rule, because they either carry no
// merchant theme at all or consume it through a DIFFERENT (equally valid)
// mechanism. Each entry is a recorded DECISION, keyed on content unique to that
// page so the exemption cannot silently widen, and it is asserted to still match
// a page so the exemption cannot rot into dead licence:
//
//	public.go / renderPrivateGate — the private-form password gate. It is rendered
//	BEFORE the merchant's theme is consulted and is platform chrome; theming only
//	its button while its card, background and type stay platform-default would be
//	a half-themed page rather than a fix. Same ruling as the exemption PR #996's
//	guard already records for this surface.
//
//	menu.go / menuPageTmpl — the restaurant menu. It DOES consume the merchant
//	colour, through its own `.Th.PrimaryColor` / `{{else}}#4f46e5{{end}}` pair
//	(formMenuTheme, menu.go), which predates the token triple and themes far more
//	than the CTA. This card does not re-plumb that mechanism; the entry records
//	that the surface is themed rather than forgotten.
var ctaExemptPageLiterals = []struct {
	file, marker, why string
}{
	{
		"public.go", "function unlock(e)",
		"renderPrivateGate — platform password gate, no merchant theme on that page",
	},
	{
		"menu.go", ".Th.PrimaryColor",
		"menuPageTmpl — themed through the menu's own .Th.PrimaryColor mechanism",
	},
}

// ctaButtonRules returns the body of every CTA rule in ONE template literal: a
// bare `button` selector, plus the class-based CTA selectors the Go templates
// paint a fill on (`.cta`, `.paybtn`, `.baropen`). Scoping to the literal is
// what keeps the gate's own `button{…}` rule out of a merchant-form rule that
// was never about it.
var ctaRuleRes = []*regexp.Regexp{
	regexp.MustCompile(`(?m)^\s*button\s*\{([^}]*)\}`),
	regexp.MustCompile(`(?m)^\s*\.(?:cta|paybtn|baropen)\s*\{([^}]*)\}`),
}

func ctaButtonRules(body string) []string {
	var out []string
	for _, re := range ctaRuleRes {
		for _, m := range re.FindAllStringSubmatch(body, -1) {
			out = append(out, m[1])
		}
	}
	return out
}

// drawnCtaSurface reports whether a Go-rendered page literal draws a CTA at all:
// a `<form>` element, or a submit button. This is the surface predicate Rule B
// judges on — deliberately NOT the CTA rule's selector name (see
// merchantCtaSurfaceVerdict).
//
// The predicate is intentionally kept as narrow as the card that wrote Rule B
// defined it (t_d48f0930: "ANY `<!doctype html` literal that draws a <form> or a
// submit button"). Widening it to, say, "any <button> or any class=cta" was
// measured and rejected in card t_ff9cde8b: it would newly judge
// `landingPageTmpl` (public.go), which draws its CTA as an `<a class="cta">`
// anchor with no form and themes it through `.Lp.PrimaryColor` — a surface the
// card puts explicitly out of scope. A false positive there is a guard nobody
// can satisfy, which is worse than a documented boundary.
func drawnCtaSurface(body string) bool {
	return strings.Contains(body, "<form ") || strings.Contains(body, `type="submit"`)
}

// merchantCtaSurfaceVerdict judges ONE Go-rendered page literal against Rule B
// and returns (exempt, violation). A pure function of (name, body) so the bite
// case below can drive it with a FIXTURE page: a guard whose bite can only be
// shown by editing the tree is bite-provable only on a tree somebody was
// willing to break.
//
// exempt is true for a recorded platform page (ctaExemptPageLiterals), keyed on
// the literal's file plus content unique to that page. violation is "" for a
// page that draws no CTA, for an exempt page, and for a page that consumes the
// token; it is the failure text otherwise.
//
// WHAT CHANGED AND WHY (card t_ff9cde8b). This used to require the page to carry
// a rule matched by ctaRuleRes — bare `button`, `.cta`, `.paybtn`, `.baropen` —
// and `continue`d otherwise, so a form that painted its submit button through
// an UNLISTED selector (`.buy`, `.submit`, `#go`, an inline `style=`) was
// skipped: indistinguishable, at the guard level, from a correctly themed page.
// That is the same "no token declared" defect class Rule B exists to close,
// exactly one selector name away from being invisible again — and the selector
// name is the one thing a NEW template is free to choose, so it cannot also be
// the gate. The fix drops that gate: drawing a form / submit button is now
// sufficient to be judged. ctaRuleRes survives as the narrower set
// TestNoRenderedTemplateHardCodesTheCtaFill needs, because that arm inspects the
// rule BODIES of a token-declaring template.
func merchantCtaSurfaceVerdict(name, body string) (exempt bool, violation string) {
	if !drawnCtaSurface(body) {
		return false, ""
	}
	file := strings.SplitN(name, " #", 2)[0]
	for _, p := range ctaExemptPageLiterals {
		if file == p.file && strings.Contains(body, p.marker) {
			return true, ""
		}
	}
	if !ctaTokenUseRe.MatchString(body) {
		return false, fmt.Sprintf("%s draws a CTA (a <form> or a submit button) and never consumes var(--jf-primary) — the merchant's brand colour cannot reach this surface, and no platform exemption covers it (card t_d48f0930's defect)", name)
	}
	return false, ""
}

// literalCtaFill returns the literal fill a rule body paints, including the
// pre-token gradient form (`linear-gradient(90deg,#6366f1,#8b5cf6)`) that card
// t_d48f0930 removed from the legacy layout. "" when the rule paints no literal.
func literalCtaFill(body string) string {
	return ctaLiteralFillRe.FindString(body)
}

var ctaLiteralFillRe = regexp.MustCompile(`background(?:-image)?\s*:\s*(?:linear-gradient\([^)]*#[0-9a-fA-F]{3,6}[^)]*\)|#[0-9a-fA-F]{3,6})`)

// ctaTokenUseRe is the merchant brand token, consumed.
var ctaTokenUseRe = regexp.MustCompile(`var\(\s*--jf-primary\s*\)`)

// TestGoRenderedMerchantFormCtasConsumeTheBrandToken is the arm card t_d48f0930
// asks for. `TestNoRenderedTemplateHardCodesTheCtaFill` below is gated on a
// `:root { --jf-primary … }` DECLARATION, so a template that declares the token
// nowhere is SKIPPED rather than reported — and that "no token declared" hole is
// exactly where this card's defect lived (the legacy `formTmpl` had a
// hard-coded indigo→violet CTA gradient and declared nothing).
//
// The rule here is the DUAL of that one and needs no declaration to fire: every
// Go-rendered MERCHANT FORM literal must consume `var(--jf-primary)` and must
// paint no literal CTA fill. A fourth merchant form template is therefore caught
// by the package-wide sweep below rather than surviving because nobody added it
// to a list.
func TestGoRenderedMerchantFormCtasConsumeTheBrandToken(t *testing.T) {
	literals := formTemplateLiterals(t)

	// Rule A — the four known merchant forms, each located by its own marker.
	var consumers int
	for _, mt := range merchantFormTemplates {
		var matched []string
		for _, body := range literals {
			if strings.Contains(body, mt.marker) {
				matched = append(matched, body)
			}
		}
		if len(matched) != 1 {
			t.Errorf("%s: its marker %q matched %d page literals, want exactly 1 — a renamed field must not quietly drop this surface out of the scan", mt.name, mt.marker, len(matched))
			continue
		}
		body := matched[0]
		if !ctaTokenUseRe.MatchString(body) {
			t.Errorf("%s (%s) renders a form and never consumes var(--jf-primary) — the merchant's brand colour cannot reach this surface (card t_d48f0930's defect)", mt.name, mt.file)
			continue
		}
		rules := ctaButtonRules(body)
		if len(rules) == 0 {
			t.Errorf("%s (%s) consumes the brand token but emits no CTA rule — the guard cannot see the button this token is for", mt.name, mt.file)
			continue
		}
		consumers++
		for _, rule := range rules {
			if fill := literalCtaFill(rule); fill != "" {
				t.Errorf("%s (%s) paints a CTA from the literal `%s` — the CTA must paint the merchant's colour", mt.name, mt.file, fill)
			}
		}
	}
	if consumers != len(merchantFormTemplates) {
		t.Fatalf("only %d of %d Go-rendered merchant form templates were scanned — a green run here would prove nothing about the other %d", consumers, len(merchantFormTemplates), len(merchantFormTemplates)-consumers)
	}

	// Rule B — the package-wide sweep that closes the hole for good: ANY
	// `<!doctype html` literal that draws a <form> or a submit button must
	// consume the brand token, unless it is a recorded platform page. A NEW
	// merchant form template is therefore RED on arrival instead of invisible.
	exempt := map[string]bool{}
	sawExempt := map[string]bool{}
	for _, p := range ctaExemptPageLiterals {
		exempt[p.file+"\x00"+p.marker] = true
	}
	for name, body := range literals {
		wasExempt, violation := merchantCtaSurfaceVerdict(name, body)
		if wasExempt {
			file := strings.SplitN(name, " #", 2)[0]
			for _, p := range ctaExemptPageLiterals {
				if file == p.file && strings.Contains(body, p.marker) {
					sawExempt[p.file+"\x00"+p.marker] = true
				}
			}
		}
		if violation != "" {
			t.Error(violation)
		}
	}
	// A recorded exemption that no longer matches a page is dead licence: it
	// would silently stop covering anything while still looking like a decision.
	for key := range exempt {
		if !sawExempt[key] {
			file, marker, _ := strings.Cut(key, "\x00")
			t.Errorf("the platform exemption for %s (marker %q) matched no Go-rendered page — a stale exemption is a licence nobody re-read", file, marker)
		}
	}

	// BITE — Rule B's recognition MUST NOT be gated on the CTA rule's SELECTOR
	// NAME. Card t_ff9cde8b measured the hole: at PR #1001's head, this arm
	// PASSED (rc=0) on a literal that draws `<form id="of"><button class="buy"
	// type="submit">` and paints `.buy { background:#123456 }` with no
	// `var(--jf-primary)` anywhere, because `.buy` is not in ctaRuleRes and the
	// old loop `continue`d — while flipping ONLY the selector name to `.cta`
	// reddened it at the exact assertion above. Identical page, one selector
	// name apart, one judged and one invisible.
	//
	// The page is injected here as a FIXTURE, through the same function the loop
	// above calls, so the bite does not depend on a later author's willingness
	// to break the tree: the "before" shape is asserted to violate and the
	// "after" shape is asserted not to, and the two differ ONLY in the selector
	// name. If Rule B ever re-introduces a name-based gate, the first case goes
	// green and this test fails.
	{
		const (
			unlistedSelectorPage = `<!doctype html><html><head><style>
  .buy { color:#fff; background:#123456; }
</style></head><body><form id="of"><button class="buy" type="submit">Go</button></form></body></html>`
			listedSelectorPage = `<!doctype html><html><head><style>
  :root { --jf-primary: #5e6ad2; }
  .cta { color:#fff; background:var(--jf-primary); }
</style></head><body><form id="of"><button class="cta" type="submit">Go</button></form></body></html>`
		)
		if _, v := merchantCtaSurfaceVerdict("zz_fixture.go #1", unlistedSelectorPage); v == "" {
			t.Errorf("Rule B judged a form page whose CTA is painted through an UNLISTED selector (`.buy`) as clean — " +
				"drawing a form / submit button must be sufficient to be judged, or the selector name is a blind spot (card t_ff9cde8b)")
		}
		if _, v := merchantCtaSurfaceVerdict("zz_fixture.go #1", listedSelectorPage); v != "" {
			t.Errorf("Rule B rejected a themed form page that consumes var(--jf-primary) via a LISTED selector — "+
				"the bite case above cannot be trusted if its control case fails: %s", v)
		}
	}

	// The two colours this card removed from the legacy layout, asserted gone as
	// FILLS specifically (the rationale comments name them on purpose, so a bare
	// mention is not the defect — a `background:` of one is).
	for path, src := range renderedStylesheets(t) {
		for _, fill := range []string{"linear-gradient(90deg,#6366f1,#8b5cf6)", "accent-color:#4f46e5"} {
			if strings.Contains(src, fill) {
				t.Errorf("%s still carries `%s` — the legacy layout's fixed indigo accent (card t_d48f0930)", path, fill)
			}
		}
	}
}

// TestNoRenderedTemplateHardCodesTheCtaFill is the declaration-gated source
// scan. It covers the one thing a render assertion cannot: a NEW Go-rendered
// MERCHANT FORM stylesheet (a fourth template, or a second copy inside an
// existing one) that paints the CTA from a literal instead of consuming
// `var(--jf-primary)`.
//
// The rule is "declares the brand token ⇒ consumes it", which is exactly the
// defect shape this arm was written for (three templates declared `--jf-primary`
// and consumed it nowhere). It is scoped to the template LITERAL, not the file:
// public.go holds both the legacy merchant form and the private gate's page, so
// a file-scoped scan judged the gate's platform button by a merchant-form rule.
//
// The declaration gate left a real hole — a template that declared no token was
// skipped rather than reported — and that hole is where card t_d48f0930's legacy
// layout lived until it was themed.
// TestGoRenderedMerchantFormCtasConsumeTheBrandToken above is the arm that
// closes it: it fires on a merchant form literal with no declaration at all.
func TestNoRenderedTemplateHardCodesTheCtaFill(t *testing.T) {
	literals := formTemplateLiterals(t)

	var declaring, consumers int
	for name, body := range literals {
		// A declaration, not a mention: the token must be SET inside a `:root`
		// block to a value (a template action or a hex). The doc comments in this
		// package name `--jf-primary: …` on purpose and must not count.
		if !regexp.MustCompile(`:root\s*\{[^}]*--jf-primary\s*:\s*(?:#|\{\{)`).MatchString(body) {
			continue
		}
		declaring++
		rules := ctaButtonRules(body)
		if len(rules) == 0 {
			t.Errorf("%s declares --jf-primary but emits no CTA rule — the guard cannot see the button this token is for", name)
			continue
		}
		for _, rule := range rules {
			if !strings.Contains(rule, "var(--jf-primary") {
				t.Errorf("%s renders a submit button that does not consume var(--jf-primary) (rule: %q) — a token with no consumer is card t_8f5d6fb5's defect", name, strings.Join(strings.Fields(rule), " "))
				continue
			}
			consumers++
			if fc := backgroundColorRe.FindStringSubmatch(rule); fc != nil {
				if hardCodedFillRe.MatchString("background: " + strings.TrimSpace(fc[1])) {
					t.Errorf("%s renders a submit button whose fill is the literal `%s` — the CTA must paint the merchant's colour", name, strings.TrimSpace(fc[1]))
				}
			}
		}
	}
	// Non-vacuity, both directions: if the rule were renamed or the token
	// stopped being declared, "no offenders" above must not read as a pass.
	if declaring < 3 {
		t.Fatalf("only %d Go-rendered page literal(s) declare the brand token inside a :root block; public.go (formTmpl), render_blocks.go (blockTmpl + responseTmpl) and render_custom.go (customFormTmpl) all must", declaring)
	}
	// Five CTA rules are the four merchant templates, one of which (the block
	// product form) paints both a `button` and a `.cta`-style rule.
	if consumers < 4 {
		t.Fatalf("found %d rendered CTA rules consuming var(--jf-primary), want the four merchant form templates — the rule is not looking at the stylesheet it was written for, so a green run proves nothing", consumers)
	}

	// #991's fill is gone as a FILL (the rationale comments name it on purpose,
	// so a bare mention is not the defect — a `background:` of it is).
	for path, src := range renderedStylesheets(t) {
		if regexp.MustCompile(`background\s*:\s*#828fff`).MatchString(src) {
			t.Errorf("%s still paints `background:#828fff` — that is #991's defect: a white label measures 2.87:1 on it", path)
		}
	}
}

// ── the brand colour as TEXT: price, footer links, confirmation link ─────────
//
// Card t_48c36a54. `--jf-primary-text` is painted as TEXT on the form's own
// surfaces — `.price` (render_blocks.go:446, public.go:1034), `.jf-flinks a`
// (render_blocks.go:495/:939, public.go:1054) and `.ok .msg a`
// (render_blocks.go:945, render_custom.go:120) — and it is the token
// `formtheme.go` derives as `d.textOn(surfaceTint)`, whose own comment says it
// is measured against the darkest of those surfaces: the selected-card tint
// `#eef2ff` that `.prod:has(input[type=checkbox]:checked)` paints.
//
// Nothing measured that pairing. The arms above pin the CTA triple
// (`--jf-primary`, `-hover`, `-active`) and the button's label, and never read
// `--jf-primary-text` — `grep -rn "jf-primary-text" api/ web/ --include=*_test.go`
// returned 0. So a future change to the derivation or to the tint could drop a
// price tag or a footer link below AA with every gate still green, and the 4.5
// floor the comment claims was enforced by nothing.
//
// The values are sound today — a sweep of all 4096 colours of a 16^3 RGB lattice
// through the shipped derivation found 0 below 4.5:1, minimum 4.500:1 — which is
// exactly why the missing guard was non-blocking. The point of this section is
// that the floor is now enforced rather than asserted in a comment.

// selectedCardTint is the surface the guard measures the text token against: the
// selected product card's tint, painted by
// `.prod:has(input[type=checkbox]:checked) { … background:#eef2ff }` in the two
// renderers that carry a product card. It is the surface `surfaceTint`
// (formtheme.go:106) names.
const selectedCardTint = "#eef2ff"

// pageSurface is the other surface the token's text lands on: the form page's own
// background (`--jf-bg` #f7f8f8), which the footer links sit on. It is lighter
// than the tint, so the tint is the conservative of the two to measure.
const pageSurface = "#f7f8f8"

var (
	// The selected-card rule, whose background is the tint.
	cardTintRuleRe = regexp.MustCompile(`(?m)^\s*\.prod:has\(input\[type=checkbox\]:checked\)\s*\{([^}]*)\}`)
	// The token, consumed as a text colour.
	textTokenPaintRe = regexp.MustCompile(`color\s*:\s*var\(\s*--jf-primary-text\s*\)`)
	// The price rule — the text that lands on the tint.
	priceRuleRe = regexp.MustCompile(`(?m)^\s*\.price\s*\{([^}]*)\}`)
	// The footer-link rule — the other text that lands on the tint (and on the
	// page surface).
	footerLinkRuleRe = regexp.MustCompile(`(?m)^\s*\.jf-flinks a\s*\{([^}]*)\}`)
	// The confirmation-link rule on the submitted page.
	confirmLinkRuleRe = regexp.MustCompile(`(?m)^\s*\.ok \.msg a\s*\{([^}]*)\}`)
)

// formTextTokenVerdict judges ONE rendered stylesheet for the text token:
//
//	declared   --jf-primary-text must exist and be a hex (a renamed token is a
//	           failure, not an absent check);
//	painted    the stylesheet must consume it as a text colour (a token no rule
//	           paints is a colour no buyer's eye ever sees). This check is
//	           page-global — ONE rule satisfies it — so the per-consumer,
//	           per-renderer coverage lives in
//	           TestEveryTextTokenConsumerPaintsTheToken;
//	measured   the value must clear AA on the selected-card tint.
//
// A pure function of the rendered HTML, so the bite case in
// TestPublicFormTextTokenVerdictBites can drive it with a FIXTURE page instead of
// depending on a later author's willingness to break the tree — the same shape
// merchantCtaSurfaceVerdict uses.
func formTextTokenVerdict(html string) (token string, ratio float64, violation string) {
	res, err := newCSSResolver(html)
	if err != nil {
		return "", 0, fmt.Sprintf("the rendered page carries no `:root` token block to read: %v", err)
	}
	token = res.vars["jf-primary-text"]
	if token == "" {
		return "", 0, "the stylesheet declares no --jf-primary-text — the price, the footer links and the confirmation link have no token for their text colour, and a renamed token must read as a failure here rather than as a pass"
	}
	if !textTokenPaintRe.MatchString(html) {
		return token, 0, "declares --jf-primary-text but no rule paints `color:var(--jf-primary-text)` — the derivation is measuring a colour no text on the page uses"
	}
	ratio, err = contrastRatio(token, selectedCardTint)
	if err != nil {
		return token, 0, fmt.Sprintf("measuring the text token against the tint: %v", err)
	}
	if ratio < 4.5 {
		return token, ratio, fmt.Sprintf("the text token %s measures %.3f:1 on the selected-card tint %s — below the AA 4.5:1 floor the derivation claims (formtheme.go textOn(surfaceTint)); a selected product's price and the footer links are painted this colour", token, ratio, selectedCardTint)
	}
	return token, ratio, ""
}

// TestThemedPublicFormTextTokenClearsAAOnTheSelectedCardTint is the guard card
// t_48c36a54 asks for: the rendered `--jf-primary-text` of EVERY Go-rendered
// public form, for every brand colour AND the unthemed default, clears AA on the
// card tint the code claims to derive it against.
func TestThemedPublicFormTextTokenClearsAAOnTheSelectedCardTint(t *testing.T) {
	// The unthemed default is a CASE (`""`), not an omission: it is the render a
	// form with no theme column gets, and its margin is the thinnest of the set
	// — #5964cc on the tint measures 4.559:1, down from the 5.623:1 the literal
	// #4f46e5 it replaced scored.
	colours := append(append([]string{}, brandColours...), "")
	cases := rendererCases()
	var read, painted int
	for _, rc := range cases {
		for _, primary := range colours {
			html := rc.render(t, primary)
			token, _, violation := formTextTokenVerdict(html)
			if token != "" {
				read++
			}
			if token != "" && textTokenPaintRe.MatchString(html) {
				painted++
			}
			if violation != "" {
				t.Errorf("%s / theme %q: %s", rc.name, primary, violation)
			}
		}
	}
	// Non-vacuity, both directions: a renamed token and a renamed paint rule must
	// each leave this run red rather than silently measuring nothing.
	want := len(cases) * len(colours)
	if read != want {
		t.Fatalf("only %d of %d rendered pages declare --jf-primary-text — the guard is not reading the stylesheets it was written for, so a green run would prove nothing", read, want)
	}
	if painted != want {
		t.Fatalf("only %d of %d rendered pages paint var(--jf-primary-text) — a renamed paint rule must not read as a pass", painted, want)
	}
}

// TestPublicFormTextTokenIsMeasuredAgainstThePaintedCardSurface ties the
// derivation's assumed surface to the one the page actually paints. The
// assertion above measures the token against the constant `#eef2ff`; this one
// proves that constant IS the surface in the rendered stylesheet AND that the
// text landing on it is painted with the token — so `surfaceTint` drifting away
// from the CSS, or `.price` dropping the token, is a red here.
func TestPublicFormTextTokenIsMeasuredAgainstThePaintedCardSurface(t *testing.T) {
	// The derivation's own surface must be the tint the guard measures against.
	if got := hexOf(surfaceTint); !strings.EqualFold(got, selectedCardTint) {
		t.Fatalf("formtheme.go derives the text colour against surfaceTint %s while this guard measures %s — the surface the token is derived against and the surface it is measured on are not the same one", got, selectedCardTint)
	}
	var tinted, priced int
	for _, rc := range rendererCases() {
		html := rc.render(t, "")
		m := cardTintRuleRe.FindStringSubmatch(html)
		if m == nil {
			// The response-only and custom-HTML renderers carry no product card,
			// so they paint no selected-card tint; their text token is still
			// judged above.
			continue
		}
		bg := backgroundColorRe.FindStringSubmatch(m[1])
		if bg == nil {
			t.Errorf("%s: the selected-card rule carries no `background:` — the guard cannot see the surface the price text sits on", rc.name)
			continue
		}
		tinted++
		paintedTint := strings.TrimSpace(bg[1])
		if !strings.EqualFold(paintedTint, hexOf(surfaceTint)) {
			t.Errorf("%s: the page paints the selected-card tint %s while formtheme.go derives the text token against surfaceTint %s — the derivation is measuring a surface this page does not paint", rc.name, paintedTint, hexOf(surfaceTint))
		}
		pm := priceRuleRe.FindStringSubmatch(html)
		if pm == nil {
			t.Errorf("%s: paints a selected-card tint but emits no `.price` rule — the text this token exists for is gone, and re-anchor this guard", rc.name)
			continue
		}
		if !textTokenPaintRe.MatchString(pm[1]) {
			t.Errorf("%s: the `.price` rule is %q — it does not paint var(--jf-primary-text), so the price on the tint is no longer the colour the derivation measured", rc.name, strings.Join(strings.Fields(pm[1]), " "))
			continue
		}
		priced++
	}
	// Non-vacuity: the surface has to be found, or the measurement above is about
	// nothing. The block product form and the legacy product-checkbox form both
	// paint it.
	if tinted < 2 {
		t.Fatalf("only %d renderer(s) paint the selected-card tint %s — the guard did not find the surface the text token is measured against", tinted, selectedCardTint)
	}
	if priced != tinted {
		t.Fatalf("only %d of %d renderers that paint the tint also paint `.price` with var(--jf-primary-text)", priced, tinted)
	}
}

// TestEveryTextTokenConsumerPaintsTheToken closes the gap the page-global paint
// check leaves open. `formTextTokenVerdict` matches `textTokenPaintRe` against
// the WHOLE page, so ONE rule that paints the token satisfied its `painted`
// counter no matter how many of the token's consumers had dropped it — and the
// arm above pins a rule body per renderer for `.price` only.
//
// QA measured (card t_7f3a9686) exactly that hole: with
// `render_blocks.go:495` changed from
// `  .jf-flinks a { color:var(--jf-primary-text); … }` to a literal
//
//	`  .jf-flinks a { color:#5964cc; … }`
//
// all five arms of this section stayed GREEN. The footer link is a live
// consumer, not a dead rule: on the unthemed public form in a fresh browser,
// after ticking the product checkbox, `.jf-flinks a` computes rgb(89,100,204) =
// #5964cc on the #eef2ff selected-card tint.
//
// So this arm walks every consumer the token exists for, per renderer, and
// requires that rule's OWN body to paint `var(--jf-primary-text)`. The
// consumers and the call sites that emit them, measured by rendering each
// rendererCase (not read off the templates):
//
//	.price        render_blocks.go:446  (blockTmpl)     public.go:1034 (formTmpl)
//	.jf-flinks a  render_blocks.go:495  (blockTmpl)     render_blocks.go:939 (responseTmpl)  public.go:1054 (formTmpl)
//	.ok .msg a    render_blocks.go:945  (responseTmpl)  render_custom.go:120 (customFormTmpl)
//
// A renderer that genuinely does not emit a selector is skipped (the
// response-only form has no price; the custom-HTML form has no footer links),
// but the pair floor below is the count those four renderers emit today, so a
// deleted rule or a renamed selector cannot turn this into a check of nothing.
func TestEveryTextTokenConsumerPaintsTheToken(t *testing.T) {
	consumers := []struct {
		name string
		rule *regexp.Regexp
	}{
		{".price", priceRuleRe},
		{".jf-flinks a", footerLinkRuleRe},
		{".ok .msg a", confirmLinkRuleRe},
	}
	// The unthemed default is the render a form with no theme column gets. These
	// are static rules in the stylesheets, so one render per renderer covers
	// every theme — the themed renders are walked by the arms above.
	const wantPairs = 7
	var checked int
	emittedBy := map[string]int{}
	consumersPerRenderer := map[string]int{}
	var renderers []string
	for _, rc := range rendererCases() {
		renderers = append(renderers, rc.name)
		html := rc.render(t, "")
		for _, c := range consumers {
			rules := c.rule.FindAllStringSubmatch(html, -1)
			if len(rules) == 0 {
				continue
			}
			emittedBy[c.name] += len(rules)
			consumersPerRenderer[rc.name] += len(rules)
			for _, m := range rules {
				checked++
				if !textTokenPaintRe.MatchString(m[1]) {
					t.Errorf("%s: the `%s` rule is %q — it paints a literal, not var(--jf-primary-text), so that text is no longer the colour the derivation measured against the selected-card tint %s", rc.name, c.name, strings.Join(strings.Fields(m[1]), " "), selectedCardTint)
				}
			}
		}
	}
	// Non-vacuity three ways: the pair count (a renamed selector or a deleted
	// rule must read as RED, never as a skipped check), then that each selector
	// still has a renderer at all, then that no renderer slipped out of the walk
	// entirely.
	if checked < wantPairs {
		t.Fatalf("only %d of the %d (renderer, selector) pairs these renderers emit were checked — a renamed selector or a deleted rule must read as RED here, not as a skipped check", checked, wantPairs)
	}
	for _, c := range consumers {
		if emittedBy[c.name] == 0 {
			t.Fatalf("no renderer emits the `%s` rule at all — the selector was renamed or the rule deleted; re-anchor this guard (the sites are listed above) instead of letting it measure nothing", c.name)
		}
	}
	for _, name := range renderers {
		if consumersPerRenderer[name] == 0 {
			t.Fatalf("%s emits none of the text-token consumers (`.price` / `.jf-flinks a` / `.ok .msg a`) — its stylesheet changed shape, so the renderer the token is for is no longer walked here; re-anchor this guard", name)
		}
	}
}

// TestPublicFormTextTokenTintIsTheBindingSurface pins WHY the tint is the surface
// to measure on, and reproduces the card's own numbers: the unthemed default
// (#5964cc at 4.559:1, thinnest margin) and the live themed forms. It also
// asserts the premise the measurement rests on — the base brand colour scores
// worse on the tint than on the page's white, and for the default (and the
// tightest colour in the set) it clears white while failing the tint, so the tint
// alone is what forces those derivations to move.
func TestPublicFormTextTokenTintIsTheBindingSurface(t *testing.T) {
	for _, p := range []struct {
		name, primary, wantText string
		wantRatio               float64
		// tintDecides marks the colours whose base clears AA on the page's
		// lightest surface and fails it on the tint — the tint alone is what
		// forces those derivations to move.
		tintDecides bool
	}{
		{"the unthemed default / the form-settings starting colour", defaultPrimary, "#5964cc", 4.559, true},
		{"the tightest colour in the set — #e11d48 clears white, only the tint decides", "#e11d48", "#db1143", 4.500, true},
		{"a live themed form (event-registration)", "#f59e0b", "#ab5900", 4.514, false},
		{"a live themed form (donation)", "#10b981", "#007f4c", 4.536, false},
	} {
		cta := resolveThemeCta(p.primary)
		if !strings.EqualFold(cta.Text, p.wantText) {
			t.Errorf("%s (%s): the derivation resolves the text token to %s, not the card's %s", p.name, p.primary, cta.Text, p.wantText)
		}
		got, err := contrastRatio(cta.Text, selectedCardTint)
		if err != nil {
			t.Fatalf("%s: %v", p.name, err)
		}
		if math.Abs(got-p.wantRatio) > 0.01 {
			t.Errorf("%s (%s -> %s): this tree measures %.3f:1 on the tint, the card measured %.3f:1", p.name, p.primary, cta.Text, got, p.wantRatio)
		}
		if got < 4.5 {
			t.Errorf("%s (%s -> %s): %.3f:1 on the tint %s — below the AA 4.5:1 floor", p.name, p.primary, cta.Text, got, selectedCardTint)
		}
		// The premise, asserted rather than assumed.
		onWhite, err := contrastRatio(p.primary, white)
		if err != nil {
			t.Fatalf("%s: %v", p.name, err)
		}
		onTint, err := contrastRatio(p.primary, selectedCardTint)
		if err != nil {
			t.Fatalf("%s: %v", p.name, err)
		}
		if onTint >= onWhite {
			t.Errorf("premise: %s measures %.3f:1 on white and %.3f:1 on the tint — the tint is not the worse of the two surfaces for this colour", p.primary, onWhite, onTint)
		}
		if p.tintDecides {
			if onWhite < 4.5 {
				t.Errorf("premise: %s does not clear AA on white either (%.3f:1), so the tint would not be the one deciding this colour — use a colour that clears white and fails only the tint", p.primary, onWhite)
			}
			if onTint >= 4.5 {
				t.Errorf("premise: %s already clears AA on the tint (%.3f:1), so the tint does not bind for it", p.primary, onTint)
			}
		}
	}

	// The tint must be darker than the page surface, or measuring on the tint
	// would be the optimistic of the two choices rather than the conservative one.
	lt, err := hexLuminance(selectedCardTint)
	if err != nil {
		t.Fatalf("%v", err)
	}
	lb, err := hexLuminance(pageSurface)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if lt >= lb {
		t.Fatalf("the selected-card tint %s (luminance %.4f) is not darker than the page surface %s (%.4f) — the guard's whole premise is that the tint is the darkest surface the token is painted on", selectedCardTint, lt, pageSurface, lb)
	}
	// And it must be the worst-scoring of the two for every brand colour.
	for _, primary := range brandColours {
		cta := resolveThemeCta(primary)
		onTint, err := contrastRatio(cta.Text, selectedCardTint)
		if err != nil {
			t.Fatalf("%s: %v", primary, err)
		}
		onPage, err := contrastRatio(cta.Text, pageSurface)
		if err != nil {
			t.Fatalf("%s: %v", primary, err)
		}
		if onTint > onPage+1e-9 {
			t.Errorf("%s -> %s scores %.3f:1 on the tint but %.3f:1 on the page surface — the tint is not the binding surface for this colour", primary, cta.Text, onTint, onPage)
		}
	}
}

// TestPublicFormTextTokenVerdictBites proves the assertion above can fail. Every
// page here is a FIXTURE handed to the same function the guard calls, so the bite
// does not depend on someone being willing to break the tree first — the pattern
// merchantCtaSurfaceVerdict's bite case uses.
func TestPublicFormTextTokenVerdictBites(t *testing.T) {
	const (
		// What the token is worth if `textOn(surfaceTint)` is dropped and the
		// base brand colour is painted instead: #5e6ad2 on the tint is 4.203:1
		// — the exact shape of a price that fell below AA while every gate
		// stayed green.
		underivedTokenPage = `<!doctype html><html><head><style>
  :root { --jf-primary: #5e6ad2; --jf-primary-text: #5e6ad2; }
  .prod:has(input[type=checkbox]:checked) { background:#eef2ff; }
  .price { color:var(--jf-primary-text); }
</style></head><body><div class="prod"><span class="price">RM 25.00</span></div></body></html>`
		// The token renamed away: the declaration the guard reads is gone.
		renamedTokenPage = `<!doctype html><html><head><style>
  :root { --jf-primary: #5e6ad2; --jf-primary-txt: #5964cc; }
  .price { color:var(--jf-primary-txt); }
</style></head><body><span class="price">RM 25.00</span></body></html>`
		// Declared, measured — and painted by nothing.
		unpaintedTokenPage = `<!doctype html><html><head><style>
  :root { --jf-primary: #5e6ad2; --jf-primary-text: #5964cc; }
  .price { color:#111111; }
</style></head><body><span class="price">RM 25.00</span></body></html>`
		// The shipped shape, as a control: the derivation's own answer on the
		// tint (4.559:1), painted by the price.
		goodTokenPage = `<!doctype html><html><head><style>
  :root { --jf-primary: #5e6ad2; --jf-primary-text: #5964cc; }
  .prod:has(input[type=checkbox]:checked) { background:#eef2ff; }
  .price { color:var(--jf-primary-text); }
</style></head><body><div class="prod"><span class="price">RM 25.00</span></div></body></html>`
	)
	for _, c := range []struct {
		name, page string
		mustFail   bool
		needle     string
	}{
		{"the derivation dropped, the base colour painted on the tint", underivedTokenPage, true, "4.5:1 floor"},
		{"the token renamed", renamedTokenPage, true, "declares no --jf-primary-text"},
		{"the token declared but painted by no rule", unpaintedTokenPage, true, "no rule paints"},
		{"the shipped shape (control)", goodTokenPage, false, ""},
	} {
		token, ratio, violation := formTextTokenVerdict(c.page)
		if c.mustFail {
			if violation == "" {
				t.Errorf("%s: formTextTokenVerdict judged the page clean (%s on the tint = %.3f:1) — the guard would not catch this shape", c.name, token, ratio)
				continue
			}
			if !strings.Contains(violation, c.needle) {
				t.Errorf("%s: the verdict did not name %q: %s", c.name, c.needle, violation)
			}
			continue
		}
		if violation != "" {
			t.Errorf("%s: formTextTokenVerdict rejected the shipped shape — the failing cases above cannot be trusted if this one fails: %s", c.name, violation)
		}
	}
}
