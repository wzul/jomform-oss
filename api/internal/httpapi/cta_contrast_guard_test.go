package httpapi

// The public (Go-rendered) form's submit CTA, in all three renderers.
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

// rendererCases drives the three PUBLIC call sites — not the templates by hand —
// so a missing map entry or a dropped rule at the call site is a red, which is
// exactly the shape this card's defect had (the token was declared and the
// consumer was never wired).
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

// TestNoRenderedTemplateHardCodesTheCtaFill is the narrowed source scan. It
// covers the one thing a render assertion cannot: a NEW Go-rendered FORM
// stylesheet (a fourth template, or a second copy inside an existing one) that
// paints the CTA from a literal instead of consuming `var(--jf-primary)`.
//
// The rule is "declares the brand token ⇒ consumes it", which is exactly the
// defect shape this card found (three templates declared `--jf-primary` and
// consumed it nowhere). A platform page that paints its own fixed button —
// renderPrivateGate's unlock prompt, which never reads the merchant's theme at
// all and declares no token — is deliberately NOT in scope: it is not the
// merchant's form, and theming only its button while its card, background and
// type stay platform-default would be a half-themed page rather than a fix.
// That surface is out of scope for this card and is recorded here so the
// exemption is a decision, not an oversight.
func TestNoRenderedTemplateHardCodesTheCtaFill(t *testing.T) {
	var declaring, consumers int
	for path, src := range renderedStylesheets(t) {
		// A declaration, not a mention: the token must be SET inside a `:root`
		// block to a value (a template action or a hex). The doc comments in this
		// package name `--jf-primary: …` on purpose and must not count.
		declaresToken := regexp.MustCompile(`:root\s*\{[^}]*--jf-primary\s*:\s*(?:#|\{\{)`).MatchString(src)
		if !declaresToken {
			continue
		}
		declaring++
		rules := buttonRe.FindAllStringSubmatch(src, -1)
		if len(rules) == 0 {
			t.Errorf("%s declares --jf-primary but emits no CTA rule — the guard cannot see the button this token is for", path)
			continue
		}
		for _, m := range rules {
			body := m[1]
			if !strings.Contains(body, "var(--jf-primary") {
				t.Errorf("%s renders a submit button that does not consume var(--jf-primary) (body: %q) — a token with no consumer is card t_8f5d6fb5's defect", path, strings.Join(strings.Fields(body), " "))
				continue
			}
			consumers++
			if fc := backgroundColorRe.FindStringSubmatch(body); fc != nil {
				if hardCodedFillRe.MatchString("background: " + strings.TrimSpace(fc[1])) {
					t.Errorf("%s renders a submit button whose fill is the literal `%s` — the CTA must paint the merchant's colour", path, strings.TrimSpace(fc[1]))
				}
			}
		}
	}
	// Non-vacuity, both directions: if the rule were renamed or the token
	// stopped being declared, "no offenders" above must not read as a pass.
	if declaring < 2 {
		t.Fatalf("only %d Go source(s) declare the brand token inside a :root block; render_blocks.go and render_custom.go both must", declaring)
	}
	// Three CTA rules are the three merchant templates: the block product form
	// and the response-only form in render_blocks.go, the custom-HTML form in
	// render_custom.go.
	if consumers < 3 {
		t.Fatalf("found %d rendered CTA rules consuming var(--jf-primary), want the three merchant form templates — the rule is not looking at the stylesheet it was written for, so a green run proves nothing", consumers)
	}

	// #991's fill is gone as a FILL (the rationale comments name it on purpose,
	// so a bare mention is not the defect — a `background:` of it is).
	for path, src := range renderedStylesheets(t) {
		if regexp.MustCompile(`background\s*:\s*#828fff`).MatchString(src) {
			t.Errorf("%s still paints `background:#828fff` — that is #991's defect: a white label measures 2.87:1 on it", path)
		}
	}
}
