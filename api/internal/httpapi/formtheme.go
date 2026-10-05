package httpapi

// The public (Go-rendered) form's brand colour: parsing it, and deriving the
// hover / pressed / label / text colours that go with it.
//
// WHY THIS EXISTS
//   All three Go form templates declared the merchant's brand colour as
//   `:root { --jf-primary: … }` and then consumed it NOWHERE — a repo-wide grep
//   for `var(--jf-primary` in api/ returned 0 — while the submit CTA was a
//   hard-coded #5e6ad2 and the checkbox/radio accent a hard-coded #4f46e5. A
//   merchant who picked a colour on /form-settings got a form painted the
//   default indigo.
//
//   Wiring the rest fill to the theme is half the job. The other half is that
//   the hover and pressed fills have to follow the same hue, and the button's
//   LABEL has to keep clearing AA (4.5:1) in all three states. A merchant colour
//   cannot simply reuse the default's relationship to white:
//
//     * #5e6ad2 carries a white label at 4.70:1, so its hover is a step LIGHTER
//       (#616dd5, 4.51:1) with room to spare;
//     * amber #f59e0b carries a white label at 2.15:1 — no fill on that hue can
//       reach 4.5 with white without going muddy — so the AA-safe direction for
//       a light brand colour is a DARKER label (the page ink, 8.26:1), and the
//       hover/pressed steps are measured against THAT label.
//
//   So: the base colour is the merchant's, quantised; the states are the same
//   hue and chroma at a different OKLCH lightness (the step the shipped indigo
//   pair uses); and the label is whichever of white / ink / black the base can
//   actually carry. Every candidate is measured on its QUANTISED 8-bit hex
//   before it is accepted — the sweep is a search, the measured ratio is the
//   verdict.
//
//   OKLab (Björn Ottosson, https://bottosson.github.io/posts/oklab/) rather than
//   HSL: in HSL the same lightness step moves a blue and a yellow by different
//   perceived amounts, so a derived hover would not read the same per brand
//   colour. OKLab lightness is perceptual, so the indigo pair's step is the same
//   visible step for amber or emerald.
//
//   Out of scope, deliberately: the merchant's `custom_css` / `background`
//   tokens. A merchant who paints their own background already owns the contrast
//   of every label on it; this derivation is against the page's own surfaces.

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// The default triple, byte-identical to web/src/app/globals.css
// (`--jf-primary`, `--jf-primary-hover`, `--jf-primary-active`). These are what
// an unthemed form resolves to (and what TestPublicFormCtaTokensMatchWebCtaTokens
// pins against that file), so the public form and the dashboard CTA cannot drift
// into two different pairs.
const (
	defaultPrimary       = "#5e6ad2"
	defaultPrimaryHover  = "#616dd5"
	defaultPrimaryActive = "#454ea8"
)

// Lightness deltas taken from the shipped default pair, so a merchant colour
// gets the same visible step the indigo one uses: rest -> hover is +0.0098
// (lightness only), rest -> active is -0.1011.
const (
	primaryHoverLightnessDelta  = 0.0098
	primaryActiveLightnessDelta = 0.1011
	// The pressed state also drops chroma slightly: #5e6ad2 -> #454ea8 is
	// L-0.1011 with C*0.9016 (the value globals.css ships as
	// --jf-primary-active), so a merchant colour gets the same kind of press
	// rather than a pure lightness move.
	primaryActiveChromaScale = 0.9016
	// Sweep step used when the nominal step does not already clear the floor on
	// its quantised hex, and the cap on how far the sweep will travel (in
	// OKLCH lightness) before it settles for the base colour.
	primaryLightnessStep = 0.002
	primarySweepLimit    = 0.6
	// AA floor for normal-size text.
	ctaAAFloor = 4.5
)

// The page's own surfaces text can land on, and the two label anchors. White and
// ink are tried first because they are what the design uses; black is the
// fallback for the narrow band of mid-tone brand colours where NEITHER of them
// can reach the floor (a light brand colour can always be darkened, but a
// mid-grey sits where white's ceiling and the ink floor cross, and the only
// label that clears there is pure black).
var (
	labelWhite = [3]uint8{255, 255, 255}
	labelInk   = [3]uint8{0x17, 0x18, 0x1c}
	labelBlack = [3]uint8{0, 0, 0}
	// The DARKEST surface the brand colour is painted on as text, which is the
	// one that decides: the price row sits on the card (white, or the
	// merchant's card_background) and on the indigo-50 selected tint
	// (#eef2ff), and the footer/confirmation links sit on the page background
	// (--jf-bg #f7f8f8). Indigo-50 is the darkest of the three, so a colour that
	// clears AA there clears it on all of them — the same finding globals.css
	// records for `--jf-primary` as text on the tint (4.20:1, which is why the
	// dashboard has a separate `--jf-primary-on-tint`).
	//
	// Out of scope, deliberately: a merchant who also sets `background` /
	// `card_background` / `custom_css` owns the contrast of text on those; this
	// derivation is against the form's own default surfaces.
	surfaceTint = [3]uint8{0xee, 0xf2, 0xff}
)

// themeCta is the resolved brand colour for one form render.
type themeCta struct {
	// Primary is the merchant's colour, quantised. It IS the merchant's value:
	// nothing here darkens or lightens it to reach a floor.
	Primary string
	// Hover and Active are one design step lighter / darker on the same hue and
	// chroma, each verified to carry Label at AA.
	Hover  string
	Active string
	// Label is the colour the button's text takes: white, ink, or (only for a
	// mid-tone brand colour that can carry neither) black.
	Label string
	// Text is the brand colour as TEXT on the page's surfaces: the merchant's
	// colour when it clears AA there, and the nearest lightness on the same
	// hue/chroma that does when it does not. Price tags, the confirmation link
	// and the footer links paint with it.
	Text string
}

// themePrimaryValues resolves the form's brand colour to the CTA triple the
// stylesheet paints. An absent, non-string or unusable value yields the default
// indigo triple, so an unthemed form renders exactly as it did before this
// change.
func themePrimaryValues(f *dbstore.Form) themeCta {
	raw := ""
	if f != nil && f.Theme != nil {
		if v, ok := f.Theme["primary"].(string); ok {
			raw = v
		}
	}
	return resolveThemeCta(raw)
}

// resolveThemeCta returns the resolved brand colour for a stored theme value.
// An absent or unusable value resolves the DEFAULT colour through the same
// derivation rather than a second hard-coded triple — a form that stores
// #5e6ad2 (which is what /form-settings starts from) and a form that stores
// nothing must render identically, so the two paths are one function. The
// guard asserts the result equals the constants the web CTA uses.
func resolveThemeCta(raw string) themeCta {
	base, ok := parseHexColor(raw)
	if !ok {
		base, _ = parseHexColor(defaultPrimary)
	}
	d := newDeriver(base)
	primary := d.at(d.l)
	label := labelFor(base)
	return themeCta{
		Primary: primary,
		Hover:   d.state(primary, label, +1),
		Active:  d.state(primary, label, -1),
		Label:   hexOf(label),
		Text:    d.textOn(surfaceTint),
	}
}

// hexColorRe matches a CSS hex colour we are willing to paint: #rgb or #rrggbb.
// Anything else (a name, a function, a stray value) is ignored and the form
// falls back to the default triple, rather than that text being interpolated
// into the stylesheet.
var hexColorRe = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// parseHexColor normalises #rgb / #rrggbb to its 8-bit channels.
func parseHexColor(s string) ([3]uint8, bool) {
	s = strings.TrimSpace(s)
	if !hexColorRe.MatchString(s) {
		return [3]uint8{}, false
	}
	h := strings.TrimPrefix(s, "#")
	if len(h) == 3 {
		// #abc is #aabbcc: each digit is doubled, not shifted.
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	var rgb [3]uint8
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseUint(h[i*2:i*2+2], 16, 8)
		if err != nil {
			return [3]uint8{}, false
		}
		rgb[i] = uint8(v)
	}
	return rgb, true
}

// labelFor picks the label colour for a button painted `fill`: white when white
// clears AA on it, else the page ink, else pure black. The order is deliberate —
// white first keeps every colour that works today looking exactly as it does.
func labelFor(fill [3]uint8) [3]uint8 {
	if ctaContrastRatio(fill, labelWhite) >= ctaAAFloor {
		return labelWhite
	}
	if ctaContrastRatio(fill, labelInk) >= ctaAAFloor {
		return labelInk
	}
	return labelBlack
}

// deriver holds a base colour in OKLCH; chroma and hue stay fixed and only
// lightness moves.
type deriver struct {
	l, c, h float64
}

func newDeriver(base [3]uint8) deriver {
	l, a, b := srgbToOKLab(float64(base[0]), float64(base[1]), float64(base[2]))
	return deriver{l: l, c: math.Hypot(a, b), h: math.Atan2(b, a)}
}

// rgbAt renders this hue/chroma at lightness `l` back to 8-bit sRGB.
func (d deriver) rgbAt(l float64) [3]uint8 { return d.rgbAtC(l, d.c) }

// rgbAtC is rgbAt with a chroma override (the pressed state drops chroma too).
func (d deriver) rgbAtC(l, c float64) [3]uint8 {
	return oklchToRGB(math.Max(0, math.Min(1, l)), c, d.h)
}

// at is the hex for lightness `l` on this hue/chroma.
func (d deriver) at(l float64) string { return hexOf(d.rgbAt(l)) }

// state returns the hover (dir=+1) or pressed (dir=-1) fill: the shipped pair's
// delta on this hue/chroma, verified to carry `label` at AA on its quantised
// hex.
//
// The pressed state is a two-dimensional move (lightness down, chroma down the
// small amount globals.css uses), so it is built directly; the hover is a pure
// lightness step. When the nominal move does not clear, or quantises back onto
// the base (pure white / pure black have no room in that direction), the
// lightness is swept on the state's own chroma — first in `dir`, then the other
// way — and the first value whose quantised colour both clears the floor and
// differs from the base wins. If nothing does, the base is returned unchanged,
// which the guard treats as "this state is not distinguishable by fill".
func (d deriver) state(base string, label [3]uint8, dir float64) string {
	pressedC := d.c
	if dir < 0 {
		pressedC = d.c * primaryActiveChromaScale
	}
	nominal := d.l + primaryHoverLightnessDelta
	if dir < 0 {
		nominal = d.l - primaryActiveLightnessDelta
	}
	for _, d1 := range []float64{dir, -dir} {
		if d1 == dir {
			if hex := hexOf(d.rgbAtC(nominal, pressedC)); hex != base && clearsFill(d.rgbAtC(nominal, pressedC), label) {
				return hex
			}
		}
		for i := 1; i <= int(primarySweepLimit/primaryLightnessStep); i++ {
			l := d.l + d1*float64(i)*primaryLightnessStep
			if l < 0 || l > 1 {
				break
			}
			if hex := hexOf(d.rgbAtC(l, pressedC)); hex == base {
				continue
			}
			if clearsFill(d.rgbAtC(l, pressedC), label) {
				return hexOf(d.rgbAtC(l, pressedC))
			}
		}
	}
	return base
}

// textOn returns the brand colour as TEXT on `bg`: the base when it clears AA
// there, else the first lightness (swept darker, then lighter) on the same
// hue/chroma whose quantised colour does.
func (d deriver) textOn(bg [3]uint8) string {
	if clearsFill(d.rgbAt(d.l), bg) {
		return d.at(d.l)
	}
	for _, dir := range []float64{-1, 1} {
		for i := 1; i <= int(primarySweepLimit/primaryLightnessStep); i++ {
			l := d.l + dir*float64(i)*primaryLightnessStep
			if l < 0 || l > 1 {
				break
			}
			if clearsFill(d.rgbAt(l), bg) {
				return d.at(l)
			}
		}
	}
	return d.at(d.l)
}

// clearsFill reports whether the 8-bit colour `c` carries `label` at AA. The
// check runs on the quantised value because that is what the stylesheet paints:
// an un-quantised lightness can clear the floor and land on a neighbouring
// 8-bit colour that does not.
func clearsFill(c, label [3]uint8) bool {
	return ctaContrastRatio(c, label) >= ctaAAFloor
}

// ── WCAG contrast ────────────────────────────────────────────────────────────
//
// The same definitions as api/internal/httpapi/cta_contrast_guard_test.go
// (WCAG 2.x relative luminance). This lives in non-test code because the
// renderer, not only a test, has to answer "can this colour carry its label at
// AA".

// ctaContrastRatio is the WCAG 2.x ratio between two 8-bit sRGB colours.
func ctaContrastRatio(a, b [3]uint8) float64 {
	la := relativeLuminance(a)
	lb := relativeLuminance(b)
	hi, lo := math.Max(la, lb), math.Min(la, lb)
	return (hi + 0.05) / (lo + 0.05)
}

// relativeLuminance is WCAG 2.x relative luminance:
// https://www.w3.org/TR/WCAG21/#dfn-relative-luminance
func relativeLuminance(c [3]uint8) float64 {
	return 0.2126*srgbToLinear(float64(c[0])/255) +
		0.7152*srgbToLinear(float64(c[1])/255) +
		0.0722*srgbToLinear(float64(c[2])/255)
}

// ── OKLab ────────────────────────────────────────────────────────────────────

func srgbToOKLab(r, g, b float64) (float64, float64, float64) {
	l := srgbToLinear(r / 255)
	m := srgbToLinear(g / 255)
	s := srgbToLinear(b / 255)

	l_ := math.Cbrt(0.4122214708*l + 0.5363325363*m + 0.0514459929*s)
	m_ := math.Cbrt(0.2119034982*l + 0.6806995451*m + 0.1073969566*s)
	s_ := math.Cbrt(0.0883024619*l + 0.2817188376*m + 0.6299787005*s)

	return 0.2104542553*l_ + 0.7936177850*m_ - 0.0040720468*s_,
		1.9779984951*l_ - 2.4285922050*m_ + 0.4505937099*s_,
		0.0259040371*l_ + 0.7827717662*m_ - 0.8086757660*s_
}

// oklchToRGB renders an OKLCH colour to 8-bit sRGB, clamping out-of-gamut
// channels. The caller measures the quantised result, so a clamped channel is
// accounted for rather than assumed safe (clamping is what desaturates a
// darkened light brand colour).
func oklchToRGB(l, c, h float64) [3]uint8 {
	a, b := c*math.Cos(h), c*math.Sin(h)
	l_ := l + 0.3963377774*a + 0.2158037573*b
	m_ := l - 0.1055613458*a - 0.0638541728*b
	s_ := l - 0.0894841775*a - 1.2914855480*b
	ll, mm, ss := l_*l_*l_, m_*m_*m_, s_*s_*s_
	return [3]uint8{
		clamp8(linearToSRGB(4.0767416621*ll - 3.3077115913*mm + 0.2309699292*ss)),
		clamp8(linearToSRGB(-1.2684380046*ll + 2.6097574011*mm - 0.3413193965*ss)),
		clamp8(linearToSRGB(-0.0041960863*ll - 0.7034186147*mm + 1.7076147010*ss)),
	}
}

func hexOf(c [3]uint8) string { return fmt.Sprintf("#%02x%02x%02x", c[0], c[1], c[2]) }

func srgbToLinear(v float64) float64 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func linearToSRGB(v float64) float64 {
	if v <= 0.0031308 {
		return v * 12.92
	}
	return 1.055*math.Pow(v, 1/2.4) - 0.055
}

func clamp8(v float64) uint8 {
	v *= 255
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(math.Round(v))
}
