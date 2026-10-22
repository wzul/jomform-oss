package httpapi

// Card t_c5a278c0: the custom-HTML order page sends its OWN Content-Security
// Policy, and until this card that header named no script-src at all — so
// `default-src 'none'` applied to scripts and the browser blocked the page's
// own three blocks: the payment-failure renderer (jfPayFail), the custom-form
// bridge (srcdoc + the /order POST) and the embed height reporter.
//
// It was measured on prod (2026-10-20, v1.21.263): `typeof window.jfPayFail`
// was "undefined", iframe#jf-custom srcdoc length 0, and a click on Submit
// fired ZERO requests. Three script-src-elem violations at the served lines of
// exactly those blocks.
//
// THE SUITE DID NOT CATCH IT because every existing assertion is BYTE PRESENCE
// — order_chip_failure_t_e2024698_test.go:216 looks for "function jfPayFail(d){"
// in the document, and the custom arm of rendererCases() renders through the
// same recorder that writes both the header and the script. Both are true of a
// page whose scripts can never run.
//
// So these guards decide what a BROWSER decides: they parse the CSP the
// renderer emitted, resolve it through the CSP3 fallback chain
// (script-src-elem -> script-src -> default-src, connect-src -> default-src),
// and weigh every inline <script> the same response wrote against it. A header
// the page's own scripts cannot run under is a RED here while every byte is
// still present.
//
// Spec: https://www.w3.org/TR/CSP3/#match-element-to-source-list

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// customCSPBody is merchant-supplied HTML of the shape a custom order form
// carries: a real <form> with a named input, which is what the bridge hooks.
const customCSPBody = `<form><label>Name</label><input type="text" name="name" required>` +
	`<button type="submit">Send</button></form>`

// renderCustomForCSP drives the REAL call site — PublicHandlers.renderCustomForm,
// the same function serveForm reaches for a response-mode form with custom HTML
// — and returns the CSP it emitted beside the body it wrote, because that PAIR
// is what the browser decides on. Rendering the template by hand would test the
// template and not the header.
func renderCustomForCSP(t *testing.T) (csp string, body string) {
	t.Helper()
	cfg := map[string]any{
		"mode": "response",
		"custom_html": map[string]any{
			"html":   customCSPBody,
			"fields": []any{map[string]any{"field_id": "name", "type": "text", "label": "Name"}},
		},
	}
	h := &PublicHandlers{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://demo.jomform.com/checkout", nil)
	f := themedForm("checkout", "#5e6ad2", cfg)
	// Card t_6efd84e2: this page now also serves the merchant's own custom_js,
	// as a fourth inline block. Carrying it in this fixture means the "EVERY
	// inline block must be admitted" rule below weighs that block too — it is
	// the newest block, and therefore the one a future change is most likely to
	// emit without the response's nonce.
	f.Theme["custom_js"] = "window.__jfCSPProbe = 1;"
	if !h.renderCustomForm(rec, req, f) {
		t.Fatal("renderCustomForm declined a form with enabled custom_html — this is no longer the surface under test")
	}
	return rec.Header().Get("Content-Security-Policy"), rec.Body.String()
}

// cspDirectives is a parsed policy: lowercased directive name -> source list.
// A directive that is PRESENT with an empty list is kept, because `script-src`
// with no sources means "nothing runs" — which is not the same as the directive
// being absent (that falls back).
type cspDirectives map[string][]string

func parseCustomCSP(t *testing.T, header string) cspDirectives {
	t.Helper()
	if strings.TrimSpace(header) == "" {
		t.Fatal("the custom-HTML page sent no Content-Security-Policy at all — this guard measured nothing")
	}
	out := cspDirectives{}
	for _, part := range strings.Split(header, ";") {
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}
		out[strings.ToLower(fields[0])] = fields[1:]
	}
	return out
}

// effective resolves a directive through the CSP3 fallback chain the browser
// uses. A nil result means no policy governs that fetch type (i.e. allowed).
func (d cspDirectives) effective(name string) []string {
	chain := []string{name}
	switch name {
	case "script-src":
		chain = []string{"script-src-elem", "script-src", "default-src"}
	case "connect-src":
		chain = []string{"connect-src", "default-src"}
	}
	for _, n := range chain {
		if srcs, ok := d[n]; ok {
			return srcs
		}
	}
	return nil
}

func hasCSPSource(srcs []string, want string) bool {
	for _, s := range srcs {
		if s == want {
			return true
		}
	}
	return false
}

// scriptAdmitted answers for ONE emitted inline <script> the question the
// browser asks: will this execute under the policy the SAME response sent?
//
// The rules implemented are the ones that decide this card's outcome:
//   - a nonce source matches the tag's nonce attribute, byte for byte;
//   - otherwise a hash source matches sha256 of the block's exact text;
//   - 'unsafe-inline' counts ONLY when the directive carries no nonce and no
//     hash, because CSP3 says a nonce/hash source makes 'unsafe-inline' IGNORED.
//     A guard that forgot that would call a `script-src 'nonce-x' 'unsafe-inline'`
//     header green — and would also miss that 'unsafe-inline' alone is not the
//     fix this card is allowed to take.
func scriptAdmitted(srcs []string, nonce, body string) (bool, string) {
	if srcs == nil {
		return true, ""
	}
	hasNonceOrHash := false
	for _, s := range srcs {
		ls := strings.ToLower(s)
		if strings.HasPrefix(ls, "'nonce-") || strings.HasPrefix(ls, "'sha256-") ||
			strings.HasPrefix(ls, "'sha384-") || strings.HasPrefix(ls, "'sha512-") ||
			ls == "'strict-dynamic'" {
			hasNonceOrHash = true
		}
	}
	if nonce != "" && hasCSPSource(srcs, "'nonce-"+nonce+"'") {
		return true, ""
	}
	if body != "" {
		sum := sha256.Sum256([]byte(body))
		if hasCSPSource(srcs, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'") {
			return true, ""
		}
	}
	if !hasNonceOrHash && hasCSPSource(srcs, "'unsafe-inline'") {
		return true, ""
	}
	return false, "effective script-src is " + strings.Join(srcs, " ")
}

// scriptBlock is one inline <script> as emitted: its attribute text, its exact
// body (what a hash would be computed over) and where it sits in the document.
type scriptBlock struct {
	attrs  string
	body   string
	nonce  string
	offset int
}

var (
	scriptTagRe   = regexp.MustCompile(`(?is)<script\b([^>]*)>(.*?)</script>`)
	nonceAttrRe   = regexp.MustCompile(`(?i)\bnonce\s*=\s*"([^"]*)"`)
	srcAttrRe     = regexp.MustCompile(`(?i)\bsrc\s*=\s*"([^"]*)"`)
	nonceAlphabet = regexp.MustCompile(`^[A-Za-z0-9+/=_-]+$`)
)

func inlineScripts(t *testing.T, page string) []scriptBlock {
	t.Helper()
	var out []scriptBlock
	for _, m := range scriptTagRe.FindAllStringSubmatchIndex(page, -1) {
		attrs := page[m[2]:m[3]]
		if srcAttrRe.MatchString(attrs) {
			t.Fatalf("the custom-HTML page emits an EXTERNAL <script src=…> (%q) — this guard only weighs inline blocks, so a policy that admits it proves nothing about the page's own scripts", attrs)
		}
		b := scriptBlock{
			attrs:  attrs,
			body:   page[m[4]:m[5]],
			offset: m[0],
		}
		if nm := nonceAttrRe.FindStringSubmatch(attrs); nm != nil {
			b.nonce = nm[1]
		}
		out = append(out, b)
	}
	return out
}

// TestCustomHTMLPageCSPAdmitsItsOwnScripts is the guard the card asks for: the
// header the renderer sends and the markup it writes must agree, so the page's
// own scripts actually execute. Reverting the header to the shipped
// `default-src 'none'` (no script-src) is the mutation that must redden it.
func TestCustomHTMLPageCSPAdmitsItsOwnScripts(t *testing.T) {
	header, page := renderCustomForCSP(t)
	dirs := parseCustomCSP(t, header)
	scriptSrc := dirs.effective("script-src")
	blocks := inlineScripts(t, page)

	// The three blocks this page owns, identified by their CONTENT, so the
	// non-vacuity floor below cannot be satisfied by a document that lost one.
	owned := []struct{ what, marker string }{
		{"the payment-failure renderer (jfPayFail)", "function jfPayFail(d){"},
		{"the custom-form bridge (srcdoc + the /order submit)", "frame.srcdoc="},
		{"the embed height reporter", "jomform:height"},
	}
	if len(blocks) < len(owned) {
		t.Fatalf("the page emitted %d inline <script> block(s), want at least %d — this guard is measuring the wrong document", len(blocks), len(owned))
	}
	for _, ow := range owned {
		matched := 0
		for _, b := range blocks {
			if !strings.Contains(b.body, ow.marker) {
				continue
			}
			matched++
			if ok, why := scriptAdmitted(scriptSrc, b.nonce, b.body); !ok {
				t.Errorf("%s cannot execute under the CSP the SAME response sent (%s).\n"+
					"Its text is in the document, so a byte-presence assertion passes while the browser drops the block — that is the measured prod defect (jfPayFail undefined, iframe srcdoc empty, Submit fires no request).\n"+
					"Put the nonce the header names on the page's own tags, or list a matching sha256 source. Tag was: %q",
					ow.what, why, b.attrs)
			}
		}
		if matched == 0 {
			t.Errorf("%s: no inline <script> carrying %q was emitted — re-anchor this guard", ow.what, ow.marker)
		}
	}

	// Every inline block, not only the three known ones: a fourth page-owned
	// script added later must not ship dead either.
	for i, b := range blocks {
		if ok, why := scriptAdmitted(scriptSrc, b.nonce, b.body); !ok {
			t.Errorf("inline <script> #%d (document offset %d) cannot execute under the page's own CSP (%s); tag was %q",
				i+1, b.offset, why, b.attrs)
		}
	}

	// The card's expected value: admitted by a NONCE or a HASH, never by
	// 'unsafe-inline'. This header exists to keep merchant/agent-supplied HTML
	// inert; a page that admits script by shape re-opens the door it was
	// written to close (and 'unsafe-inline' would also be ignored next to the
	// nonce anyway, so it buys nothing and hides the intent).
	if hasCSPSource(scriptSrc, "'unsafe-inline'") {
		t.Errorf("the page's effective script-src carries 'unsafe-inline': %q — the page's own scripts must be admitted by a nonce or a hash",
			strings.Join(scriptSrc, " "))
	}

	// The bridge POSTs to location.pathname + "/order" with fetch(). connect-src
	// falls back to default-src, so the shipped `default-src 'none'` would have
	// blocked that request even once the scripts ran — the Submit button would
	// still be dead, one layer later. 'self' is the narrowest source that
	// admits the page's own route; the bridge never calls another origin.
	if connect := dirs.effective("connect-src"); connect != nil &&
		!hasCSPSource(connect, "'self'") && !hasCSPSource(connect, "*") {
		t.Errorf("effective connect-src is %q — it does not admit the page's own fetch() to /<slug>/order, so a Submit click still reaches no route",
			strings.Join(connect, " "))
	}
}

// TestCustomHTMLPageCSPNonceIsFreshPerResponse pins the nonce's two properties.
// A REUSED nonce is a constant, and a constant cannot distinguish the page's own
// script from markup that learned it — which is the whole reason CSP nonces are
// per response. And the header's nonce is worthless unless every page-owned tag
// carries it, so both halves are asserted for the same response.
func TestCustomHTMLPageCSPNonceIsFreshPerResponse(t *testing.T) {
	headerA, pageA := renderCustomForCSP(t)
	headerB, pageB := renderCustomForCSP(t)

	nonceOf := func(header, page string) string {
		t.Helper()
		srcs := parseCustomCSP(t, header).effective("script-src")
		var names []string
		for _, s := range srcs {
			if strings.HasPrefix(strings.ToLower(s), "'nonce-") {
				names = append(names, strings.TrimSuffix(strings.TrimPrefix(s, "'nonce-"), "'"))
			}
		}
		if len(names) != 1 {
			t.Fatalf("the header names %d nonce source(s) (%v), want exactly 1 (effective script-src %q)",
				len(names), names, strings.Join(srcs, " "))
		}
		for i, b := range inlineScripts(t, page) {
			if b.nonce != names[0] {
				t.Errorf("inline <script> #%d carries nonce %q but the header names %q — the browser drops a block whose nonce does not match",
					i+1, b.nonce, names[0])
			}
		}
		return names[0]
	}

	nonceA := nonceOf(headerA, pageA)
	nonceB := nonceOf(headerB, pageB)

	if len(nonceA) < 22 {
		t.Errorf("nonce %q is %d characters — CSP requires at least 128 bits of entropy (>= 22 base64/base32 characters) from a nonce this page hands out", nonceA, len(nonceA))
	}
	if !nonceAlphabet.MatchString(nonceA) {
		t.Errorf("nonce %q has characters a CSP source expression will not accept (base64-value grammar)", nonceA)
	}
	if nonceA == nonceB {
		t.Errorf("two responses both carry the nonce %q — a reused nonce is a constant, not a nonce", nonceA)
	}
}

// TestCustomHTMLPageBridgeCanReachTheFrame pins the one sandbox flag the submit
// bridge cannot work without, in the direction that matters: the bridge reads
// the frame's document, so the frame must be same-origin-readable.
//
// Measured in Chrome on the fixed CSP with the shipped sandbox="allow-forms":
// `frame.contentDocument` was null, every read threw, no submit hook was ever
// installed, and clicking the CTA still fired zero requests. A guard that only
// asserted the CSP would call that page green.
func TestCustomHTMLPageBridgeCanReachTheFrame(t *testing.T) {
	_, page := renderCustomForCSP(t)

	var bridge *scriptBlock
	bridges := inlineScripts(t, page)
	for i := range bridges {
		if strings.Contains(bridges[i].body, "frame.srcdoc=") {
			bridge = &bridges[i]
			break
		}
	}
	if bridge == nil {
		t.Fatal("no inline <script> writes the iframe's srcdoc — the bridge is gone, so this guard cannot see what it reads")
	}
	if !strings.Contains(bridge.body, "contentDocument") {
		t.Skip("the bridge no longer reads the frame document — re-derive this guard rather than keeping it")
	}

	sandbox := sandboxValue(t, page)
	if !strings.Contains(sandbox, "allow-same-origin") {
		t.Errorf("the custom-HTML iframe is sandbox=%q but the bridge reads frame.contentDocument: without allow-same-origin the frame has an "+
			"opaque origin, contentDocument is null, and the buyer's Submit click fires no request (measured)", sandbox)
	}
	if !strings.Contains(sandbox, "allow-forms") {
		t.Errorf("the custom-HTML iframe is sandbox=%q — the merchant's form cannot be submitted without allow-forms", sandbox)
	}
}

// sandboxValue extracts the iframe's sandbox attribute from the rendered page.
func sandboxValue(t *testing.T, page string) string {
	t.Helper()
	m := regexp.MustCompile(`(?is)<iframe\b[^>]*\bsandbox="([^"]*)"`).FindStringSubmatch(page)
	if m == nil {
		t.Fatal("the custom-HTML page renders no sandboxed iframe — merchant-supplied HTML would be unframed")
	}
	return m[1]
}

// TestCustomHTMLPageKeepsTheMerchantHTMLInert is the do-not-regress half: the
// fix must admit the page's own scripts WITHOUT admitting the merchant-supplied
// HTML that lives in the iframe. That HTML is guarded by the sandbox and by its
// own meta CSP, and the fix must not touch either.
func TestCustomHTMLPageKeepsTheMerchantHTMLInert(t *testing.T) {
	header, page := renderCustomForCSP(t)
	dirs := parseCustomCSP(t, header)

	// The sandbox must keep every flag that stops the merchant's markup from
	// acting on its own: allow-scripts would execute it, allow-top-navigation
	// would move the buyer off the page, allow-popups would open a window of its
	// choosing. allow-same-origin is present by necessity (see
	// TestCustomHTMLPageBridgeCanReachTheFrame) and is only reachable by THIS
	// page's script, because the frame may run no script of its own.
	sandbox := sandboxValue(t, page)
	for _, forbidden := range []string{"allow-scripts", "allow-top-navigation", "allow-popups", "allow-modals", "allow-pointer-lock"} {
		if strings.Contains(sandbox, forbidden) {
			t.Errorf("the custom-HTML iframe sandbox gained %q (%q) — merchant-supplied HTML must stay inert", forbidden, sandbox)
		}
	}

	// The srcdoc body keeps its OWN policy: default-src 'none', no nonce. That
	// is what still stops the merchant's markup from executing even though the
	// parent page now runs its own scripts.
	var bridge *scriptBlock
	bridges := inlineScripts(t, page)
	for i := range bridges {
		if strings.Contains(bridges[i].body, "frame.srcdoc=") {
			bridge = &bridges[i]
			break
		}
	}
	if bridge == nil {
		t.Fatal("no inline <script> writes the iframe's srcdoc — the bridge is gone, so this guard cannot see the merchant frame's policy")
	}
	if !strings.Contains(bridge.body, `http-equiv="Content-Security-Policy"`) {
		t.Errorf("the srcdoc no longer carries its own meta Content-Security-Policy — the merchant frame's second layer is gone")
	}
	if !strings.Contains(bridge.body, `default-src \'none\'`) {
		t.Errorf("the srcdoc meta CSP is no longer `default-src 'none'`: the merchant frame must keep forbidding scripts and remote resources")
	}

	// And the parent policy must not admit script by scheme, wildcard or eval —
	// any of those would let merchant-supplied markup run without the nonce.
	scriptSrc := dirs.effective("script-src")
	for _, loose := range []string{"*", "https:", "http:", "data:", "'unsafe-eval'"} {
		if hasCSPSource(scriptSrc, loose) {
			t.Errorf("the page's effective script-src carries %q (%q) — that admits script the page does not own",
				loose, strings.Join(scriptSrc, " "))
		}
	}
}
