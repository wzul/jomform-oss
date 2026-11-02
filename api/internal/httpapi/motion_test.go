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
