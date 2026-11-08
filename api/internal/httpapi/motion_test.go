package httpapi

// Card t_d5336ec6 — the public form's motion layer.
//
// THE DEFECT THIS GUARDS. Before this card the Go-rendered buyer pages (/f/*)
// had no motion and no reduced-motion baseline at all: 0 `@keyframes`, 0
// occurrences of `prefers-reduced-motion` anywhere in the web app or the Go
// templates. The motion that did exist (a colour fade on the CTA) ignored the
// OS accessibility setting.
//
// WHAT IS PINNED, per rendered page (not per template source — the assertion is
// on the bytes a buyer's browser receives):
//
//  1. the reduced-motion block is present, in every renderer. This is the
//     baseline the card asks to ship WITH the first animation, never after it.
//  2. the page's card carries the entrance class, so the entrance is actually
//     wired and not just declared.
//  3. no duration exceeds 300ms (the card's ceiling), measured from the tokens
//     the page declares rather than from the keyframe that uses them.
//  4. no `transition-all` and no infinite animation: a bare transition-all can
//     animate a layout property, and an infinite loop is out of bounds.
//  5. the two renderers with an in-page success state call jfSwapSuccess with
//     the animated ok block — the crossfade is wired, not merely styled.
//
// THE CONTROL. TestMotionFindingsBite feeds the same checker a page with the
// reduced-motion block removed and a page with a 400ms duration token, and
// requires it to name each one. Without that, "the live pages have no findings"
// could just mean the checker cannot find anything.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// motionDurRe reads a declared duration token: `--jf-dur-fast: 120ms`.
var motionDurRe = regexp.MustCompile(`--jf-dur-[a-z]+:\s*(\d+)ms`)

// motionFindings is the whole rule set, as a function so the control can hand it
// doctored bytes. It answers the list of things WRONG with the page.
func motionFindings(html string) []string {
	var out []string

	if !strings.Contains(html, "@media (prefers-reduced-motion: reduce)") {
		out = append(out, "no `@media (prefers-reduced-motion: reduce)` baseline — the page's motion ignores the OS accessibility setting")
	}
	if !strings.Contains(html, `class="card jf-enter"`) {
		out = append(out, `the page's card does not carry the entrance class (class="card jf-enter")`)
	}
	for _, m := range motionDurRe.FindAllStringSubmatch(html, -1) {
		ms, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		if ms > 300 {
			out = append(out, fmt.Sprintf("duration token %q is %dms, over the 300ms ceiling", m[0], ms))
		}
	}
	if strings.Contains(html, "transition-all") {
		out = append(out, "`transition-all` is present — it can animate a layout property (width/height/top/left) and the card's bounds forbid that")
	}
	if strings.Contains(html, "infinite") {
		out = append(out, "an `infinite` animation is present — auto-playing loops are out of bounds")
	}
	return out
}

// TestPublicFormMotionLayer pins the motion layer on every Go-rendered public
// form page. The four renderers are ONE buyer-facing surface (/f/*), so a
// buyer must not get a different movement vocabulary — or no reduced-motion
// baseline — depending on which renderer drew the page.
func TestPublicFormMotionLayer(t *testing.T) {
	pages := orderBodyTemplates(t)
	if len(pages) != 4 {
		t.Fatalf("expected the four public form renderers, got %d", len(pages))
	}
	for name, html := range pages {
		for _, finding := range motionFindings(html) {
			t.Errorf("%s: %s", name, finding)
		}
	}
}

// TestPublicSuccessSwapIsWired pins the submit -> success crossfade in the two
// renderers that have an in-page success state.
func TestPublicSuccessSwapIsWired(t *testing.T) {
	pages := orderBodyTemplates(t)
	for _, name := range []string{
		"response-only block form (render_blocks.go)",
		"custom-HTML response form (render_custom.go)",
	} {
		html, ok := pages[name]
		if !ok {
			t.Fatalf("%s missing from the render set", name)
		}
		if !strings.Contains(html, "jfSwapSuccess(") {
			t.Errorf("%s: the success state is not swapped through jfSwapSuccess — the crossfade is not wired", name)
		}
		if !strings.Contains(html, `class="ok jf-ok-in"`) {
			t.Errorf("%s: the success block does not carry jf-ok-in", name)
		}
		if !strings.Contains(html, `class="jf-check"`) {
			t.Errorf("%s: the success check glyph does not carry jf-check (no scale)", name)
		}
		// The JS must read the reduced-motion preference itself: a CSS media
		// query cannot stop a setTimeout.
		if !strings.Contains(html, "(prefers-reduced-motion: reduce)") {
			t.Errorf("%s: the JS swap does not consult prefers-reduced-motion", name)
		}
		// Card t_5b61dbaa: the outgoing state is DEAD unless the element leaves
		// the entrance animation and the style is flushed before the class is
		// added — the animation's `both` fill outranks the opacity, and a
		// transition does not start from an animation-held value. Pinning the
		// order here keeps the shared constant honest on BOTH renderers; the
		// behavioural half is TestPublicSwapOutDropsTheEntranceClass.
		drop := strings.Index(html, "classList.remove('jf-enter')")
		flush := strings.Index(html, "void el.offsetWidth;")
		add := strings.Index(html, "classList.add('jf-swap-out')")
		if drop < 0 || flush < 0 || add < 0 {
			t.Errorf("%s: the swap does not drop the entrance class, flush style and then apply `jf-swap-out` (remove=%d flush=%d add=%d) — without all three the outgoing half never paints", name, drop, flush, add)
		} else if !(drop < flush && flush < add) {
			t.Errorf("%s: the swap must drop `jf-enter`, then flush style, then add `jf-swap-out` (found at %d/%d/%d) — any other order starts the transition from the entrance animation's value", name, drop, flush, add)
		}
	}
}

// TestMotionFindingsBite is the control: the checker above must NAME the two
// defects it exists to catch. Handing it bytes that are wrong on purpose is the
// only way to tell a real PASS from a checker that finds nothing anywhere.
func TestMotionFindingsBite(t *testing.T) {
	clean := orderBodyTemplates(t)["response-only block form (render_blocks.go)"]
	if got := motionFindings(clean); len(got) != 0 {
		t.Fatalf("the control page must start clean, got %v", got)
	}

	// 1. the baseline is removed -> the checker must say so.
	noBaseline := strings.Replace(clean, "@media (prefers-reduced-motion: reduce)", "@media (prefers-reduced-motion: no-preference)", 1)
	if got := motionFindings(noBaseline); len(got) == 0 || !strings.Contains(got[0], "prefers-reduced-motion") {
		t.Fatalf("removing the reduced-motion baseline did NOT bite: %v", got)
	}

	// 2. an over-ceiling duration -> the checker must say so.
	tooSlow := strings.Replace(clean, "--jf-dur-slow: 280ms", "--jf-dur-slow: 400ms", 1)
	if got := motionFindings(tooSlow); len(got) == 0 || !strings.Contains(strings.Join(got, " "), "400ms") {
		t.Fatalf("a 400ms duration did NOT bite: %v", got)
	}

	// 3. the entrance is no longer wired -> the checker must say so.
	noEnter := strings.Replace(clean, `class="card jf-enter"`, `class="card"`, 1)
	if got := motionFindings(noEnter); len(got) == 0 || !strings.Contains(strings.Join(got, " "), "entrance class") {
		t.Fatalf("dropping the entrance class did NOT bite: %v", got)
	}
}

// ── the outgoing half of the submit -> success crossfade ────────────────────
//
// Card t_5b61dbaa. The crossfade's outgoing half was DEAD on prod while every
// byte assertion in this file passed: jfSwapSuccess did add `jf-swap-out` and
// the page did serve its rule, but `.card.jf-enter`'s entrance animation fills
// FORWARD (`both`), so once it had finished it still held opacity at the
// animation's origin value — and an animation-origin value outranks every
// normal author declaration, whatever its specificity. `.jf-swap-out { opacity:
// 0 }` therefore computed to 1 and the transition never started: the class was
// applied, nothing faded, and the content was replaced on the next frame.
// Measured on prod: opacity 1 at every ~15ms sample of the whole 120ms window;
// remove `jf-enter` first and the same class faded 1 -> 0.073 -> 0.006 -> 0.
//
// "The class is in the bytes" is exactly the assertion that shipped this green,
// so the two guards below pin the OUTCOME on both halves:
//
//	TestPublicSwapOutDropsTheEntranceClass — drives the served page's own script
//	  in the shared goja DOM, submits the form, and reads the class list at the
//	  moment the outgoing state is applied: `jf-swap-out` present AND `jf-enter`
//	  gone, content not yet replaced, and a style flush forced BETWEEN those two
//	  mutations (without it Chromium creates no transition at all and the card
//	  cuts 1 -> 0 in one frame — measured).
//	TestPublicSwapOutOutranksTheEntranceFill — runs the served CSS through a
//	  small cascade model for an element carrying all three classes and requires
//	  the winning `animation` declaration to be `none`.
//
// Each has a control fed the pre-fix shape, so a green run means the checker
// can see the defect rather than finding nothing anywhere.

// pageStyleCSS concatenates every <style> block the page serves.
func pageStyleCSS(html string) string {
	var sb strings.Builder
	for {
		open := strings.Index(html, "<style")
		if open < 0 {
			break
		}
		gt := strings.IndexByte(html[open:], '>')
		if gt < 0 {
			break
		}
		start := open + gt + 1
		end := strings.Index(html[start:], "</style>")
		if end < 0 {
			break
		}
		sb.WriteString(html[start : start+end])
		sb.WriteByte('\n')
		html = html[start+end+len("</style>"):]
	}
	return sb.String()
}

// blockEnd answers the index of the `}` matching the `{` at open, or -1.
func blockEnd(css string, open int) int {
	depth := 0
	for i := open; i < len(css); i++ {
		switch css[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// stripAtRuleBlocks removes every at-rule (@media, @keyframes, @supports …)
// with its whole body, leaving only top-level style rules.
//
// The reduced-motion block is why this exists: `@media (prefers-reduced-motion:
// reduce)` switches the entrance OFF, and a model that read it would report the
// outgoing state as correct for every user who is NOT asking for reduced motion
// — clearing precisely the defect this file guards.
func stripAtRuleBlocks(css string) string {
	var sb strings.Builder
	for i := 0; i < len(css); {
		open := strings.IndexByte(css[i:], '{')
		if open < 0 {
			sb.WriteString(css[i:])
			break
		}
		open += i
		end := blockEnd(css, open)
		if end < 0 {
			break
		}
		if !strings.HasPrefix(strings.TrimSpace(css[i:open]), "@") {
			sb.WriteString(css[i : end+1])
			sb.WriteByte('\n')
		}
		i = end + 1
	}
	return sb.String()
}

// classCompoundRe matches a selector made only of class tokens (`.card.jf-enter`).
var classCompoundRe = regexp.MustCompile(`^(\.[A-Za-z_][A-Za-z0-9_-]*)+$`)

// winningDeclaration answers the value of `prop` that the cascade gives an
// element with exactly `classes`, over the page's top-level style rules.
//
// Modelled: compound class selectors, `!important`, specificity (number of
// classes — every selector this model accepts has nothing else to count) and
// source order. That is all the question needs; the motion layer is written in
// exactly that shape.
func winningDeclaration(css string, classes map[string]bool, prop string) (string, bool) {
	best, bestSpec, bestOrder, found, important := "", 0, 0, false, false
	for order, rule := range cssRules(css) {
		spec, ok := 0, false
		for _, sel := range strings.Split(rule.selectors, ",") {
			if n, m := matchClassCompound(strings.TrimSpace(sel), classes); m {
				if !ok || n > spec {
					spec, ok = n, true
				}
			}
		}
		if !ok {
			continue
		}
		for _, decl := range strings.Split(rule.body, ";") {
			name, value, cut := strings.Cut(decl, ":")
			if !cut || strings.ToLower(strings.TrimSpace(name)) != prop {
				continue
			}
			value = strings.TrimSpace(value)
			imp := false
			if strings.HasSuffix(strings.ToLower(value), "!important") {
				value = strings.TrimSpace(value[:len(value)-len("!important")])
				imp = true
			}
			switch {
			case !found:
			case imp == important && spec < bestSpec:
				continue
			case imp == important && spec == bestSpec && order < bestOrder:
				continue
			case important && !imp:
				continue
			}
			best, bestSpec, bestOrder, found, important = value, spec, order, true, imp
		}
	}
	return best, found
}

// matchClassCompound reports whether a class-only selector matches the element
// and, if it does, how many classes it names (its specificity).
func matchClassCompound(sel string, classes map[string]bool) (int, bool) {
	if !classCompoundRe.MatchString(sel) {
		return 0, false
	}
	parts := strings.Split(sel, ".")
	n := 0
	for _, p := range parts[1:] {
		if !classes[p] {
			return 0, false
		}
		n++
	}
	return n, true
}

// cssRules splits top-level `selector { body }` pairs. It assumes at-rules are
// already stripped (stripAtRuleBlocks).
type cssRule struct{ selectors, body string }

func cssRules(css string) []cssRule {
	var out []cssRule
	for i := 0; i < len(css); {
		open := strings.IndexByte(css[i:], '{')
		if open < 0 {
			break
		}
		open += i
		end := blockEnd(css, open)
		if end < 0 {
			break
		}
		out = append(out, cssRule{selectors: css[i:open], body: css[open+1 : end]})
		i = end + 1
	}
	return out
}

// animationNameOf answers the animation NAME an `animation`/`animation-name`
// value resolves to (`none` when the declaration switches animation off).
func animationNameOf(value string) string {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// swapOutCascadeFindings is what the served CSS computes for an element that
// carries the entrance state and the outgoing state at the same time. The
// element is real: it is exactly what the old jfSwapSuccess left behind
// (`card jf-enter jf-swap-out`).
func swapOutCascadeFindings(html string) []string {
	css := stripAtRuleBlocks(stripCSSComments(pageStyleCSS(html)))
	classes := map[string]bool{"card": true, "jf-enter": true, "jf-swap-out": true}

	var out []string
	if anim, ok := winningDeclaration(css, classes, "animation"); ok {
		if name := animationNameOf(anim); name != "none" {
			out = append(out, fmt.Sprintf("an element carrying `card jf-enter jf-swap-out` still computes `animation: %s` — the entrance fills forward at opacity 1, so `.jf-swap-out`'s opacity never takes effect and the outgoing fade paints nothing", strings.Join(strings.Fields(anim), " ")))
		}
	}
	op, ok := winningDeclaration(css, classes, "opacity")
	if !ok || op != "0" {
		out = append(out, fmt.Sprintf("the outgoing state does not compute `opacity: 0` (got %q) — there is nothing for the transition to animate to", op))
	}
	return out
}

func TestPublicSwapOutOutranksTheEntranceFill(t *testing.T) {
	pages := orderBodyTemplates(t)
	for _, name := range []string{
		"response-only block form (render_blocks.go)",
		"custom-HTML response form (render_custom.go)",
	} {
		page, ok := pages[name]
		if !ok || page == "" {
			t.Fatalf("%s is missing from the render set", name)
		}
		if got := swapOutCascadeFindings(page); len(got) != 0 {
			t.Errorf("%s: the served page would paint no outgoing fade:\n  %s", name, strings.Join(got, "\n  "))
		}
	}

	// The controls run on the response renderer: both renderers serve the same
	// publicMotionCSS constant, so one doctored copy pins the checker.
	page := pages["response-only block form (render_blocks.go)"]

	// CONTROL 1 — the shipped defect: the rule still declares `opacity: 0` but
	// no longer takes the entrance animation out of the cascade.
	noNone := strings.Replace(page, ".jf-swap-out { animation: none !important;", ".jf-swap-out {", 1)
	if noNone == page {
		t.Fatal("control could not be built: `.jf-swap-out { animation: none !important;` is not in the served CSS")
	}
	if got := swapOutCascadeFindings(noNone); len(got) == 0 {
		t.Fatal("the pre-fix CSS was NOT rejected: the checker cannot see the defect it exists to catch")
	}

	// CONTROL 2 — the naive fix a reviewer will reach for: `animation: none`
	// WITHOUT `!important`, which loses the cascade to `.card.jf-enter` (0,2,0
	// against 0,1,0) and leaves the fade just as dead.
	naive := strings.Replace(page, ".jf-swap-out { animation: none !important;", ".jf-swap-out { animation: none;", 1)
	if naive == page {
		t.Fatal("control could not be built: the `!important` flag is not on the outgoing rule")
	}
	if got := swapOutCascadeFindings(naive); len(got) == 0 {
		t.Fatal("a `!important`-less `animation: none` was NOT rejected — that shape loses the cascade to `.card.jf-enter` and paints nothing")
	}
}

// swapObservation is the served page's form, read at the two moments the swap
// is made of: the frame the outgoing state is applied, and after the timer that
// replaces the content — plus every style flush the page forced while doing it.
type swapObservation struct {
	classesAtSwap, contentAtSwap string
	classesAfter, contentAfter   string
	reflows                      []string
}

// observePublicSwap runs the response page's OWN script against its own served
// markup, submits #of the way a buyer does, and reports what the swap did.
//
// The page reads `--jf-dur-fast` through getComputedStyle and the reduced-motion
// preference through matchMedia; the harness has no layout engine, so both are
// stubbed here. Without the duration stub jfSwapSuccess takes its documented
// instant-swap fallback (no class is ever applied) and the observation would
// prove nothing about the fade.
func observePublicSwap(t *testing.T, body string, reducedMotion bool) swapObservation {
	t.Helper()

	d := newPageDOM(t, body)
	if _, err := d.vm.RunString(`var getComputedStyle=function(){return {getPropertyValue:function(){return '120ms';}};};`); err != nil {
		t.Fatalf("install getComputedStyle: %v", err)
	}
	if reducedMotion {
		if _, err := d.vm.RunString(`var matchMedia=function(){return {matches:true};};`); err != nil {
			t.Fatalf("install matchMedia: %v", err)
		}
	}
	d.runPageScript(body, "jfSwapSuccess")
	d.orderResponse = map[string]any{"status": "submitted", "message": "Terima kasih"}

	form := d.byID["of"]
	if form == nil {
		t.Fatal("the served response page has no #of form to submit")
	}
	d.dispatch("submit", form)

	obs := swapObservation{
		classesAtSwap: menuAttr(form, "class"),
		contentAtSwap: nodeText(form),
		reflows:       append([]string(nil), d.reflows...),
	}
	d.flushTimers()
	obs.classesAfter = menuAttr(form, "class")
	obs.contentAfter = nodeText(form)
	return obs
}

func TestPublicSwapOutDropsTheEntranceClass(t *testing.T) {
	page := orderBodyTemplates(t)["response-only block form (render_blocks.go)"]
	if page == "" {
		t.Fatal("the response-only renderer is missing from the render set")
	}

	obs := observePublicSwap(t, page, false)
	atSwap := strings.Fields(obs.classesAtSwap)

	if !containsField(atSwap, "jf-swap-out") {
		t.Errorf("the outgoing state was not applied at all: class list at swap was %q", obs.classesAtSwap)
	}
	// THE DEFECT: with `jf-enter` still on the element, the entrance animation's
	// `both` fill pins opacity at 1 and the fade paints nothing.
	if containsField(atSwap, "jf-enter") {
		t.Errorf("the form still carries `jf-enter` when the outgoing state is applied (class list %q): the entrance animation fills forward at opacity 1 and outranks .jf-swap-out, so the fade never paints", obs.classesAtSwap)
	}
	// And with both class mutations in ONE task the transition is still never
	// created: measured in Chromium the card cuts 1 -> 0 in a single frame, so
	// the page has to force a style flush between them. The flush is the ONLY
	// layout read the swap is allowed to make, and it must happen while the
	// element is between the two states (`card`, no jf-enter, no jf-swap-out).
	if len(obs.reflows) == 0 {
		t.Errorf("the swap forced no style flush between dropping `jf-enter` and adding `jf-swap-out` — measured in Chromium the transition is then never created and the card cuts to opacity 0 in one frame instead of fading")
	}
	for _, cls := range obs.reflows {
		if strings.TrimSpace(cls) != "card" {
			t.Errorf("the style flush happened with class %q, not between the two states (`card`): the transition would still start from the entrance animation's value", cls)
		}
	}
	if strings.Contains(obs.contentAtSwap, "jf-ok-in") {
		t.Errorf("the success content replaced the form in the same frame as the outgoing class — there is no window to fade over")
	}
	if containsField(strings.Fields(obs.classesAfter), "jf-swap-out") {
		t.Errorf("the outgoing class was not removed after the window: %q", obs.classesAfter)
	}
	if !strings.Contains(obs.contentAfter, "jf-ok-in") || !strings.Contains(obs.contentAfter, "jf-check") {
		t.Errorf("the success block (ok jf-ok-in + jf-check) was not swapped in after the window: %q", obs.contentAfter)
	}

	// CONTROL: the pre-fix script (add the class, keep the entrance). If the
	// driver cannot see THAT, the green above means nothing.
	preFix := strings.Replace(page, "el.classList.remove('jf-enter');", "", 1)
	if preFix == page {
		t.Fatal("control could not be built: the served script does not drop `jf-enter` before the outgoing class")
	}
	old := observePublicSwap(t, preFix, false)
	if !containsField(strings.Fields(old.classesAtSwap), "jf-enter") {
		t.Fatalf("the pre-fix bytes were NOT rejected: the driver read classes %q at swap, and the control must still carry `jf-enter`", old.classesAtSwap)
	}

	// The reduced-motion arm must not enter the fade at all: one frame, no
	// outgoing class, content replaced.
	rm := observePublicSwap(t, page, true)
	if containsField(strings.Fields(rm.classesAtSwap), "jf-swap-out") {
		t.Errorf("reduced motion still applied the outgoing class (%q) — the swap must be a single frame", rm.classesAtSwap)
	}
	if !strings.Contains(rm.contentAtSwap, "jf-ok-in") {
		t.Errorf("reduced motion did not replace the content in the same frame: %q", rm.contentAtSwap)
	}
}

func containsField(fields []string, want string) bool {
	for _, f := range fields {
		if f == want {
			return true
		}
	}
	return false
}
