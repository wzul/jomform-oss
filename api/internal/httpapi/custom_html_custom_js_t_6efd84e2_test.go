package httpapi

// Regression guard for card t_6efd84e2 — the merchant's `custom_js` (issue
// #388) never reached a custom-HTML form page (issue #255) at all.
//
// THE DEFECT, measured by driving the real call site:
//
//	row    theme = {"custom_js": "window.__jfCustomHtmlProbe = 1;"}            stored ✓
//	page   served=true status=200 bytes=7412 contains-marker=false             runs  ✗
//
// Two independent reasons, and both had to be fixed:
//
//  1. renderCustomForm's view had no `custom_js`/ThemeJS entry and
//     customFormTmpl had no site for it, so no code path could put the
//     merchant's JS on the page (the block renderers got it in card
//     t_cae9c98e; this renderer was never wired).
//  2. This page sends `script-src 'nonce-<per-response>'` with no
//     'unsafe-inline' (card t_c5a278c0), so even a block interpolated into the
//     page is DROPPED by the browser unless its tag carries that exact nonce.
//     A byte-presence assertion would pass on a page whose merchant JS can
//     never execute — the shape of every t_c5a278c0 regression.
//
// So the arms below decide what a browser decides: the emitted block is parsed
// out of the document, weighed against the CSP the SAME response sent, and
// required to be admitted by the response's nonce (and NOT by 'unsafe-inline').
//
// The breakout arm exists because serving the value verbatim (template.JS) is
// only safe with the `</script" guard that card t_cae9c98e added to
// customJSForScriptBlock — this page reuses that function through
// customJSFromTheme rather than growing a second copy.

import (
	"fmt"
	"html/template"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// customHTMLJSCfg is the config of a response-mode custom-HTML form: the shape
// renderCustomForm is reached with in production (an enabled custom_html
// section, mode=response, a real form body for the bridge to hook).
func customHTMLJSCfg() map[string]any {
	return map[string]any{
		"mode": "response",
		"custom_html": map[string]any{
			"enabled": true,
			"mode":    "response",
			"html":    customCSPBody,
			"fields":  []any{map[string]any{"field_id": "name", "type": "text", "label": "Name"}},
		},
	}
}

// customHTMLForm builds the form the renderer is handed. theme nil means the
// stored row carries no theme at all.
func customHTMLForm(theme map[string]any) *dbstore.Form {
	f := themedForm("checkout", "#5e6ad2", customHTMLJSCfg())
	f.Theme = theme
	return f
}

// renderCustomHTMLJS drives the REAL call site — PublicHandlers.renderCustomForm,
// the function serveForm reaches for a response-mode form with custom HTML — and
// returns the CSP header beside the body, because that PAIR is what the browser
// decides on. Rendering customFormTmpl by hand would test the template and not
// the wiring (which is exactly what was missing).
func renderCustomHTMLJS(t *testing.T, f *dbstore.Form) (csp string, page string) {
	t.Helper()
	h := &PublicHandlers{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://demo.jomform.com/checkout", nil)
	if !h.renderCustomForm(rec, req, f) {
		t.Fatal("renderCustomForm declined a form with enabled custom_html — this is no longer the surface under test")
	}
	return rec.Header().Get("Content-Security-Policy"), rec.Body.String()
}

// customHTMLNonce returns the single nonce the emitted policy names. More than
// one (or none) means the header no longer has the shape this guard weighs.
func customHTMLNonce(t *testing.T, header string) string {
	t.Helper()
	var names []string
	for _, s := range parseCustomCSP(t, header).effective("script-src") {
		if strings.HasPrefix(strings.ToLower(s), "'nonce-") {
			names = append(names, strings.TrimSuffix(strings.TrimPrefix(s, "'nonce-"), "'"))
		}
	}
	if len(names) != 1 {
		t.Fatalf("the custom-HTML page's effective script-src names %d nonce(s) (%v) — this guard "+
			"cannot tell which one admits an emitted block", len(names), names)
	}
	return names[0]
}

// themeScriptBlock returns the ONE emitted inline block whose text contains
// marker — i.e. the merchant's own block, located by its content rather than by
// position, so a reordered template cannot make this guard measure the page's
// own bridge instead.
func themeScriptBlock(t *testing.T, page, marker string) scriptBlock {
	t.Helper()
	for _, b := range inlineScripts(t, page) {
		if strings.Contains(b.body, marker) {
			return b
		}
	}
	t.Fatalf("no inline <script> block carries %q — the merchant's custom_js is not on the page "+
		"(the served document has %d inline block(s))", marker, len(inlineScripts(t, page)))
	return scriptBlock{}
}

// TestCustomHTMLPageServesTheMerchantsCustomJS is the card's headline: a
// custom-HTML form's `custom_js` is served verbatim, in a block of its own, and
// that block is admitted by the SAME response's CSP nonce — so it can actually
// run.
func TestCustomHTMLPageServesTheMerchantsCustomJS(t *testing.T) {
	// Multi-statement, with its own double quotes and newlines: a value that
	// only survived by being JSON-marshalled (the t_cae9c98e failure mode)
	// cannot satisfy this, and an HTML-escaped one (&#34;) fails too.
	const js = `window.__jfMeasured = 1;
  window.__jfLabel = "buyer page";
  document.documentElement.dataset.jfCustomJs = "on";`

	baseline := renderCustomHTMLJSPage(t, customHTMLForm(map[string]any{"primary": "#5e6ad2"}))
	header, page := renderCustomHTMLJS(t, customHTMLForm(map[string]any{"primary": "#5e6ad2", "custom_js": js}))

	nonce := customHTMLNonce(t, header)

	// 1. Verbatim, inside its own block, carrying the response's nonce. This
	// is the exact bytes a browser executes.
	want := `<script nonce="` + nonce + `">` + "\n" + js + "\n</script>"
	if !strings.Contains(page, want) {
		t.Fatalf("the merchant's custom_js is not served verbatim in a nonce-carrying <script> block.\n"+
			"want the page to contain:\n%s\ngot script tail:\n%s", want, customHTMLScriptTail(page))
	}

	// 2. The defect's exact shape must be absent: a JSON-marshalled string
	// literal is valid JS that does nothing, so nothing errors and nothing
	// logs.
	for _, quoted := range []string{
		`"window.__jfMeasured = 1;`,
		`window.__jfLabel = \"buyer page\";`,
	} {
		if strings.Contains(page, quoted) {
			t.Errorf("the page serves custom_js as a JSON-marshalled string literal (%q) — the "+
				"browser evaluates a no-op and the merchant's code never runs (card t_cae9c98e). "+
				"script tail:\n%s", quoted, customHTMLScriptTail(page))
		}
	}

	// 3. custom_js gets an element of its own: the page has one more inline
	// block than the same form without it.
	if got, wantN := strings.Count(page, "<script"), strings.Count(baseline, "<script")+1; got != wantN {
		t.Errorf("the page has %d <script elements, want %d (the same form without custom_js has "+
			"%d, plus one for the merchant's JS) — the merchant's code is not in an element of its own",
			got, wantN, wantN-1)
	}

	// 4. Acceptance 2: the header and the tag must agree. The block the marker
	// sits in has to be admitted by the nonce the SAME response named, and a
	// copy of it WITHOUT the nonce must NOT be admitted — otherwise this test
	// would be satisfied by a policy that admits script by shape.
	block := themeScriptBlock(t, page, "__jfMeasured")
	if block.nonce != nonce {
		t.Fatalf("the merchant's block carries nonce %q but the response's CSP names %q — the "+
			"browser drops a block whose nonce does not match, which is the measured t_c5a278c0 "+
			"regression. tag was: %q", block.nonce, nonce, block.attrs)
	}
	scriptSrc := parseCustomCSP(t, header).effective("script-src")
	if ok, why := scriptAdmitted(scriptSrc, block.nonce, block.body); !ok {
		t.Fatalf("the merchant's custom_js block cannot execute under the CSP the SAME response "+
			"sent (%s). Its text is in the document, so a byte-presence assertion passes while the "+
			"browser drops the block. tag was: %q", why, block.attrs)
	}
	if ok, _ := scriptAdmitted(scriptSrc, "", block.body); ok {
		t.Errorf("the merchant's block would be admitted even with NO nonce — the nonce is then not "+
			"what admits it (effective script-src %q)", strings.Join(scriptSrc, " "))
	}
	if hasCSPSource(scriptSrc, "'unsafe-inline'") {
		t.Errorf("the page's effective script-src carries 'unsafe-inline' (%q) — this header exists "+
			"to keep merchant/agent-supplied markup inert, so script is admitted by nonce only",
			strings.Join(scriptSrc, " "))
	}
}

// renderCustomHTMLJSPage renders a form and returns only the page.
func renderCustomHTMLJSPage(t *testing.T, f *dbstore.Form) string {
	t.Helper()
	_, page := renderCustomHTMLJS(t, f)
	return page
}

// customHTMLScriptTail returns the page from its LAST script element on, for
// failure messages (the whole page is thousands of bytes of template output).
// The merchant's block is the second-to-last block on the custom page: the
// page-owned height reporter follows it.
func customHTMLScriptTail(page string) string {
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
	end := start + 500
	if end > len(page) {
		end = len(page)
	}
	return page[start:end]
}

// TestCustomHTMLCustomJSIsTypedForTheJSContext pins the mechanism. Inside
// `<script>` html/template runs jsValEscaper, which JSON-marshals any
// HTML-context type into a quoted no-op string; `template.JS` is what makes it
// emit the value verbatim. A future "cleanup" back to template.HTML (or a plain
// string) would still put the merchant's text on the page, so the headline arm
// alone would not notice — this one would.
func TestCustomHTMLCustomJSIsTypedForTheJSContext(t *testing.T) {
	// Compile-time half: if customJSFromTheme ever stops returning
	// template.JS, this assignment no longer builds — a plain string here is
	// the defect itself, because jsValEscaper JSON-marshals it.
	var typed template.JS = customJSFromTheme(&dbstore.Form{Theme: map[string]any{"custom_js": "window.__jfMeasured = 1;"}})
	if got := fmt.Sprintf("%T", typed); got != "template.JS" {
		t.Fatalf("customJSFromTheme yields %s, want template.JS — jsValEscaper JSON-marshals an "+
			"HTML-context type into a quoted no-op (card t_cae9c98e)", got)
	}
	if typed != "window.__jfMeasured = 1;" {
		t.Fatalf("customJSFromTheme = %q, want the merchant's code unchanged — custom_js is served "+
			"as JS, not as text in HTML, so it must not be HTML-escaped", typed)
	}

	// An unthemed / custom_js-free / non-string row still gets the TYPED empty
	// value, so the template's {{if .ThemeJS}} guard stays false and the page
	// renders exactly as before. Boxed into `any` so a future change of the
	// signature to `any` (returning template.HTML, say) is still measured.
	for _, theme := range []map[string]any{nil, {}, {"custom_js": nil}, {"custom_js": 42}, {"custom_js": ""}} {
		var boxed any = customJSFromTheme(&dbstore.Form{Theme: theme})
		js, ok := boxed.(template.JS)
		if !ok {
			t.Fatalf("a theme of shape %v yields %T, want template.JS", theme, boxed)
		}
		if js != "" {
			t.Fatalf("a theme of shape %v yields %q, want the empty JS value so {{if .ThemeJS}} is false", theme, js)
		}
	}
}

// TestCustomHTMLCustomJSCannotBreakOutOfTheScriptBlock is the guard verbatim
// emission makes necessary. A `<script>` is an HTML raw-text element whose ONLY
// terminator is a case-insensitive `</script`, so customJSForScriptBlock
// truncates the value there. Each arm asserts the injected code is gone, no
// markup appeared, exactly one closer was added, and — because this page is
// nonce-gated — the surviving prefix still sits in a block carrying the
// response's nonce.
func TestCustomHTMLCustomJSCannotBreakOutOfTheScriptBlock(t *testing.T) {
	cases := []struct {
		name string
		js   string
		// body is EXACTLY what must be served as the merchant block's text:
		// the code up to the first close attempt, and nothing after it.
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

	baseline := renderCustomHTMLJSPage(t, customHTMLForm(map[string]any{"primary": "#5e6ad2"}))
	wantClosers := strings.Count(baseline, "</script>") + 1
	if wantClosers == 1 {
		t.Fatalf("the unthemed custom-HTML page has no closing </script> — this case could not tell " +
			"an early close from a normal one")
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			header, page := renderCustomHTMLJS(t, customHTMLForm(map[string]any{
				"primary":   "#5e6ad2",
				"custom_js": tc.js,
			}))
			nonce := customHTMLNonce(t, header)

			if strings.Contains(page, "jfBreakout") {
				t.Fatalf("custom_js %q got attacker-controlled code onto the page: the value left its "+
					"<script> block and the HTML parser took the rest as markup.\nscript tail:\n%s",
					tc.js, customHTMLScriptTail(page))
			}
			if strings.Contains(page, "<img src=x") {
				t.Fatalf("custom_js %q put injected markup on the page — the value closed the script "+
					"element early.\nscript tail:\n%s", tc.js, customHTMLScriptTail(page))
			}
			if got := strings.Count(page, "</script>"); got != wantClosers {
				t.Fatalf("custom_js %q changed the number of </script> on the page (%d, want %d) — the "+
					"value closed a script element early.\nscript tail:\n%s",
					tc.js, got, wantClosers, customHTMLScriptTail(page))
			}

			// The served body is the code up to the close attempt and nothing
			// after it — in an element that still carries the response nonce,
			// so the legitimate prefix keeps running.
			want := `<script nonce="` + nonce + `">` + "\n" + tc.body + "\n</script>"
			if !strings.Contains(page, want) {
				t.Fatalf("custom_js %q was not truncated at its first close attempt into a "+
					"nonce-carrying block: want the page to contain %q.\nscript tail:\n%s",
					tc.js, want, customHTMLScriptTail(page))
			}

			block := themeScriptBlock(t, page, "__jfKept")
			if ok, why := scriptAdmitted(parseCustomCSP(t, header).effective("script-src"), block.nonce, block.body); !ok {
				t.Fatalf("the truncated merchant block cannot execute under the page's own CSP (%s) — "+
					"the guard kept the code but the browser would still drop it. tag: %q", why, block.attrs)
			}
		})
	}
}

// TestCustomHTMLPageWithoutCustomJSRendersUnchanged covers the shapes a stored
// theme can take besides a script: absent, null, a non-string (a hand-written
// row, or a future key type) and the empty string. None may add a script
// element, put a literal `null`/`""` on the page, or change the page at all.
func TestCustomHTMLPageWithoutCustomJSRendersUnchanged(t *testing.T) {
	const primary = "#5e6ad2"
	themes := []struct {
		name  string
		theme map[string]any
	}{
		{"no theme at all", nil},
		{"theme without custom_js", map[string]any{"primary": primary}},
		{"null custom_js", map[string]any{"primary": primary, "custom_js": nil}},
		{"non-string custom_js", map[string]any{"primary": primary, "custom_js": 42}},
		{"empty custom_js", map[string]any{"primary": primary, "custom_js": ""}},
	}
	baselineHeader, baselinePage := renderCustomHTMLJS(t, customHTMLForm(map[string]any{"primary": primary}))
	baseline := stripCustomHTMLNonce(baselinePage, customHTMLNonce(t, baselineHeader))
	for _, tt := range themes {
		t.Run(tt.name, func(t *testing.T) {
			header, page := renderCustomHTMLJS(t, customHTMLForm(tt.theme))
			got := stripCustomHTMLNonce(page, customHTMLNonce(t, header))
			if got != baseline {
				t.Fatalf("a theme of shape %q changed the page — no custom_js must render exactly as "+
					"the same form without it.\ngot script tail:\n%s", tt.name, customHTMLScriptTail(page))
			}
			if n, want := strings.Count(page, "<script"), strings.Count(baselinePage, "<script"); n != want {
				t.Errorf("a theme of shape %q changed the <script> element count (%d, want %d)", tt.name, n, want)
			}
			for _, bad := range []string{"<script null>", "<script>\nnull\n</script>", "<script>\n\"\"\n</script>"} {
				if strings.Contains(page, bad) {
					t.Errorf("a custom_js-free page serves %q — an absent value must emit no block at all", bad)
				}
			}
		})
	}
}

// stripCustomHTMLNonce replaces the per-response nonce with a fixed token so two
// renders of the same markup can be compared. The nonce is fresh on every
// response by design (card t_c5a278c0), so two pages can never be byte-identical
// while it is in them — comparing raw would make every arm here red for a reason
// that has nothing to do with custom_js.
func stripCustomHTMLNonce(page, nonce string) string {
	if nonce == "" {
		return page
	}
	return strings.ReplaceAll(page, nonce, "NONCE")
}

// TestCustomHTMLCustomJSStaysOutOfTheSandbox is the do-not-regress half of the
// decision: custom_js is the merchant's OWN code and runs on the outer page,
// while the agent-authored HTML keeps its inert sandbox. A fix that reached the
// merchant's JS by handing the iframe allow-scripts (or by interpolating it into
// the srcdoc body) would trade the whole boundary away.
func TestCustomHTMLCustomJSStaysOutOfTheSandbox(t *testing.T) {
	const js = `window.__jfMeasured = 1;`
	_, page := renderCustomHTMLJS(t, customHTMLForm(map[string]any{"primary": "#5e6ad2", "custom_js": js}))

	// The JS is in its own top-level block, not inside the body the bridge
	// writes into the frame.
	var bridge *scriptBlock
	blocks := inlineScripts(t, page)
	for i := range blocks {
		if strings.Contains(blocks[i].body, "frame.srcdoc=") {
			bridge = &blocks[i]
			break
		}
	}
	if bridge == nil {
		t.Fatal("no inline <script> writes the iframe's srcdoc — the bridge is gone, so this guard cannot see where the merchant's JS landed")
	}
	if strings.Contains(bridge.body, "__jfMeasured") {
		t.Error("the merchant's custom_js was interpolated into the iframe body (frame.srcdoc=…): it " +
			"belongs in its own top-level block, outside the sandbox that keeps the agent-authored HTML inert")
	}
	if !strings.Contains(page, "__jfMeasured") {
		t.Fatal("the marker appears nowhere on the page at all — this guard measured nothing")
	}

	// The sandbox keeps every flag that stops the merchant's markup from acting
	// on its own. allow-same-origin is required by the bridge; combined with
	// allow-scripts it would un-sandbox the frame (the frame could reach into
	// the parent document), which is why custom_js must not be served there.
	sandbox := sandboxValue(t, page)
	for _, forbidden := range []string{"allow-scripts", "allow-top-navigation", "allow-popups", "allow-modals"} {
		if strings.Contains(sandbox, forbidden) {
			t.Errorf("the custom-HTML iframe sandbox gained %q (%q) — merchant-authored markup must stay inert; the merchant's own custom_js belongs in a top-level block instead",
				forbidden, sandbox)
		}
	}

	// The frame keeps its own nonce-free policy: the nonce this page hands its
	// own blocks is not something the frame's markup can borrow.
	if !strings.Contains(bridge.body, `default-src \'none\'`) {
		t.Error("the srcdoc meta CSP is no longer `default-src 'none'` — the merchant frame must keep forbidding scripts and remote resources")
	}
}
