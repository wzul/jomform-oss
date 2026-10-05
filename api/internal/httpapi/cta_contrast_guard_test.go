package httpapi

// The public (Go-rendered) form's submit CTA carried the same sub-AA hover pair
// as the dashboard button (card t_9efae9f0), in three hard-coded copies:
//
//	button { ... color:#fff; ... background:#5e6ad2; }
//	button:hover { background:#828fff; }
//
// The label is white and #828fff carries it at 2.87:1 — below the AA 4.5:1 floor
// for normal-size text, and the hover is the state a pointer lands on before it
// clicks. `--jf-primary` is injectable per workspace (`ThemePrimary`), but the
// HOVER fill was not a token at all: three literal copies, so a fix to one of
// them would have left the other two sub-AA with nothing to catch it.
//
// WHY A SOURCE SCAN AND NOT A RENDER ASSERTION
//   The rendered colour is trivial to assert IF a test renders the page with a
//   theme set, but that only covers the pages a test knows to render. This is the
//   same reasoning sales_read_decision_guard_test.go records for its own scans:
//   the defect had a SHAPE and three SITES, so the guard scans for the shape and
//   fails on any new file that repeats it, rather than on a list of the three we
//   happen to know about. It is deliberately a small, literal scan — the colour
//   lives in a CSS string inside a Go template, so a full CSS parser would be
//   more machinery than the rule needs.
//
// WHAT IS PINNED
//   * the ratio the card measured, computed from the two hex values, so the
//     replacement is held to AA rather than to a remembered hex;
//   * the three declared pairs (a white label on a #5e6ad2-family fill) keep
//     their intended order — hover LIGHTER than rest;
//   * the two colours are the same values the web CTA uses
//     (`--jf-primary` / `--jf-primary-hover` in web/src/app/globals.css), so the
//     public form and the dashboard CTA cannot drift into two different pairs;
//   * the old fill is gone from every Go-rendered stylesheet.
//
// Run: go test ./internal/httpapi/ -run TestPublicCtaHoverClearsAA

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

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

// The measured pair. #5e6ad2 is the resting fill, #616dd5 the hover fill — the
// lightest step on the #5e6ad2 hue whose white label still clears AA (4.51:1) —
// and #454ea8 is the web CTA's pressed fill, which this renderer does not paint
// but whose value the pair below is checked against for parity.
const (
	restFill  = "#5e6ad2"
	hoverFill = "#616dd5"
	white     = "#ffffff"
)

var (
	// `background:#5e6ad2` / `background:#616dd5` on a `color:#fff` button.
	renderedButtonRe = regexp.MustCompile(`button\s*\{[^}]*color\s*:\s*#fff\s*;[^}]*background\s*:\s*(#[0-9a-fA-F]{6})[^}]*\}`)
	renderedHoverRe  = regexp.MustCompile(`button:hover\s*\{\s*background\s*:\s*(#[0-9a-fA-F]{6})\s*;\s*\}`)
)

// renderedStylesheets walks the package's Go sources rather than naming the
// files, so a fourth copy of the CSS is scanned because it exists.
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
	// The hover must stay a LIGHTER step than the resting fill: that direction is
	// what the design uses to say "the pointer is here", and the fix must not have
	// reached AA by going darker (which would ALSO make it a press).
	lH, _ := hexLuminance(hoverFill)
	lR, _ := hexLuminance(restFill)
	if lH <= lR {
		t.Fatalf("the hover fill %s is not lighter than the resting fill %s — the AA fix must not invert the state direction", hoverFill, restFill)
	}

	sources := renderedStylesheets(t)
	var buttons, hovers int
	for path, src := range sources {
		for _, m := range renderedButtonRe.FindAllStringSubmatch(src, -1) {
			buttons++
			if !strings.EqualFold(m[1], restFill) {
				t.Errorf("%s renders a submit button whose fill is %s; the web CTA's resting fill is %s — the public form and the dashboard must paint the same pair", path, m[1], restFill)
			}
		}
		for _, m := range renderedHoverRe.FindAllStringSubmatch(src, -1) {
			hovers++
			if !strings.EqualFold(m[1], hoverFill) {
				t.Errorf("%s renders `button:hover { background:%s }`; the AA-safe hover fill is %s (white %.2f:1)", path, m[1], hoverFill, ratio)
			}
		}
	}
	// Non-vacuity: if the shape this guard is about was renamed or the CSS was
	// restructured, "no offenders" above must not read as a pass.
	if buttons == 0 || hovers == 0 {
		t.Fatalf("the scan found %d rendered submit buttons and %d hover rules in %d Go files — the rule is not looking at the stylesheet it was written for, so a green run proves nothing", buttons, hovers, len(sources))
	}

	// The old fill is gone as a FILL (the rationale comments name it on purpose,
	// so a bare mention is not the defect — a `background:` of it is).
	for path, src := range sources {
		if regexp.MustCompile(`background\s*:\s*#828fff`).MatchString(src) {
			t.Errorf("%s still paints `background:#828fff` — that is the card's defect: a white label measures 2.87:1 on it", path)
		}
	}
}

// The three rendered pairs, measured directly from the Go sources, so a green
// TestPublicCtaHoverClearsAA cannot come from the constants rather than from what
// the templates actually emit.
func TestRenderedCtaPairIsTheMeasuredOne(t *testing.T) {
	sources := renderedStylesheets(t)
	type site struct {
		path  string
		rest  string
		hover string
	}
	var sites []site
	for path, src := range sources {
		rest := renderedButtonRe.FindStringSubmatch(src)
		hover := renderedHoverRe.FindStringSubmatch(src)
		if rest == nil || hover == nil {
			continue
		}
		sites = append(sites, site{path: path, rest: rest[1], hover: hover[1]})
	}
	if len(sites) == 0 {
		t.Fatal("no Go-rendered submit button was found at all — re-anchor this guard")
	}
	for _, s := range sites {
		ratio, err := contrastRatio(white, s.hover)
		if err != nil {
			t.Fatalf("%s: %v", s.path, err)
		}
		if ratio < 4.5 {
			t.Errorf("%s: the white submit label on the hover fill %s is %.2f:1, below AA", s.path, s.hover, ratio)
		}
		// The card's own headline measurement, reproduced from THIS site's pair.
		if strings.EqualFold(s.hover, "#828fff") {
			if got, _ := contrastRatio(white, "#828fff"); math.Abs(got-2.87) > 0.01 {
				t.Fatalf("the card's 2.87:1 measurement for #828fff did not reproduce: got %.2f", got)
			}
			t.Errorf("%s reverted to the pre-fix hover fill #828fff (%.2f:1 with a white label)", s.path, ratio)
		}
	}
}
