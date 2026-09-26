package httpapi

import (
	"bytes"
	"context"
	"html/template"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/disintegration/imaging"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// Issue #411: GitHub-style default Open Graph image. Every form gets its own
// on-the-fly branded 1200x630 social preview (title + workspace + JomForm
// branding) rendered server-side as a PNG — no headless browser dependency.
// If the merchant sets a custom og:image (or a form logo), that is used
// instead; otherwise the generated default is served at /og/{slug}.png.
//
// Issue #425: the default was redesigned to match Wan's approved mockup.
// Issue #434: the later correction comment said og:image must follow the
// JomForm branding (indigo→violet), NOT the mockup (indigo→white). So the
// default now uses the brand indigo→violet gradient, the real JomForm "JF"
// monogram (indigo→violet gradient tile with a white form sheet), white text
// on the gradient, and a subtle decorative indigo card/check icon on the
// right. When the merchant sets a theme color or a form logo, the
// gradient/accents are tinted with the theme color and the form logo is used
// in place of the JomForm monogram.

const (
	ogImageW = 1200
	ogImageH = 630
)

// ogBrandColors mirror the JomForm brand (/brand): Indigo 500 #6366f1,
// Violet 500 #8b5cf6, Indigo 600 #4f46e5, Ink 900 #17181c, Paper #f7f8f8.
// The og:image gradient is indigo→violet (branding), not indigo→white
// (mockup). Text is white on the gradient per the /brand usage rules.
var (
	ogIndigo    = color.RGBA{0x63, 0x66, 0xf1, 0xff} // Indigo 500
	ogViolet    = color.RGBA{0x8b, 0x5c, 0xf6, 0xff} // Violet 500
	ogIndigo600 = color.RGBA{0x4f, 0x46, 0xe5, 0xff} // Indigo 600 (monogram JF)
	ogInk       = color.RGBA{0x17, 0x18, 0x1c, 0xff} // Ink 900
	ogPaper     = color.RGBA{0xf7, 0xf8, 0xf8, 0xff} // Paper
	ogWhite     = color.RGBA{0xff, 0xff, 0xff, 0xff}
	ogLight     = color.RGBA{0xe0, 0xe7, 0xff, 0xff} // light indigo (subtitle/tagline)
)

// ogFonts are lazily parsed once (they are embedded in the x/image module).
var (
	ogFontOnce sync.Once
	// Faces at the sizes used by the card layout.
	ogFaceTitle     font.Face // bold, large (title)
	ogFaceWorkspace font.Face // medium (workspace/business name)
	ogFaceMark      font.Face // bold, extra-large (JF mark + wordmark)
	ogFaceTag       font.Face // regular (tagline)
)

// ogFace parses an embedded TTF into a font.Face at the given point size.
func ogFace(data []byte, size float64) font.Face {
	f, err := opentype.Parse(data)
	if err != nil {
		return nil
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72})
	if err != nil {
		return nil
	}
	return face
}

// ensureOGFonts parses the embedded Go fonts once.
func ensureOGFonts() {
	ogFontOnce.Do(func() {
		ogFaceTitle = ogFace(gobold.TTF, 64)
		ogFaceWorkspace = ogFace(gomedium.TTF, 34)
		ogFaceMark = ogFace(gobold.TTF, 120)
		ogFaceTag = ogFace(goregular.TTF, 26)
	})
}

// ogTextWidth measures a string's advance width in pixels for a face.
func ogTextWidth(face font.Face, s string) int {
	d := &font.Drawer{Face: face}
	return d.MeasureString(s).Ceil()
}

// ogWrap wraps s to at most maxWidth pixels for the given face, breaking on
// workspaces. Returns the wrapped lines.
func ogWrap(face font.Face, s string, maxWidth int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{s}
	}
	var lines []string
	cur := ""
	for _, w := range words {
		cand := w
		if cur != "" {
			cand = cur + " " + w
		}
		if ogTextWidth(face, cand) <= maxWidth {
			cur = cand
			continue
		}
		if cur != "" {
			lines = append(lines, cur)
		}
		// A single word wider than the box is hard-truncated.
		if ogTextWidth(face, w) > maxWidth {
			lines = append(lines, w)
			cur = ""
			continue
		}
		cur = w
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// ogDrawText draws s centered horizontally at x (center) and vertically at
// baseline y, in the given color.
func ogDrawText(img *image.RGBA, face font.Face, s string, x, y int, c color.RGBA) {
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(c),
		Face: face,
		Dot:  fixed.P(x-ogTextWidth(face, s)/2, y),
	}
	d.DrawString(s)
}

// ogParseHex parses a "#rrggbb" (or "#rgb") hex color. Returns ok=false for
// anything malformed so callers can fall back to the brand indigo.
func ogParseHex(s string) (color.RGBA, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		return color.RGBA{}, false
	}
	r, err1 := strconv.ParseUint(s[0:2], 16, 8)
	g, err2 := strconv.ParseUint(s[2:4], 16, 8)
	b, err3 := strconv.ParseUint(s[4:6], 16, 8)
	if err1 != nil || err2 != nil || err3 != nil {
		return color.RGBA{}, false
	}
	return color.RGBA{uint8(r), uint8(g), uint8(b), 0xff}, true
}

// ogBlend linearly interpolates a→b by t (0..1).
func ogBlend(a, b color.RGBA, t float64) color.RGBA {
	lerp := func(x, y uint8) uint8 {
		return uint8(float64(x) + (float64(y)-float64(x))*t)
	}
	return color.RGBA{lerp(a.R, b.R), lerp(a.G, b.G), lerp(a.B, b.B), 0xff}
}

// ogFillRoundedRect fills a rectangle with rounded corners of the given radius
// in the given color.
func ogFillRoundedRect(img *image.RGBA, r image.Rectangle, radius int, c color.RGBA) {
	u := image.NewUniform(c)
	// Body.
	draw.Draw(img, image.Rect(r.Min.X, r.Min.Y+radius, r.Max.X, r.Max.Y-radius), u, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(r.Min.X+radius, r.Min.Y, r.Max.X-radius, r.Max.Y), u, image.Point{}, draw.Src)
	// Corners.
	corner := func(cx, cy int) {
		for dy := -radius; dy <= radius; dy++ {
			for dx := -radius; dx <= radius; dx++ {
				if dx*dx+dy*dy <= radius*radius {
					img.SetRGBA(cx+dx, cy+dy, c)
				}
			}
		}
	}
	corner(r.Min.X+radius, r.Min.Y+radius)
	corner(r.Max.X-radius-1, r.Min.Y+radius)
	corner(r.Min.X+radius, r.Max.Y-radius-1)
	corner(r.Max.X-radius-1, r.Max.Y-radius-1)
}

// ogDrawCheck draws a simple checkmark centered in the given box, in color c.
func ogDrawCheck(img *image.RGBA, box image.Rectangle, c color.RGBA) {
	// A 3-segment polyline checkmark scaled to the box.
	w := float64(box.Dx())
	h := float64(box.Dy())
	pts := [][2]float64{
		{0.20 * w, 0.52 * h},
		{0.42 * w, 0.72 * h},
		{0.80 * w, 0.30 * h},
	}
	thick := int(0.10 * w)
	if thick < 6 {
		thick = 6
	}
	seg := func(p0, p1 [2]float64) {
		x0, y0 := box.Min.X+int(p0[0]), box.Min.Y+int(p0[1])
		x1, y1 := box.Min.X+int(p1[0]), box.Min.Y+int(p1[1])
		steps := 40
		for i := 0; i <= steps; i++ {
			t := float64(i) / float64(steps)
			x := x0 + int((float64(x1)-float64(x0))*t)
			y := y0 + int((float64(y1)-float64(y0))*t)
			for dy := -thick / 2; dy <= thick/2; dy++ {
				for dx := -thick / 2; dx <= thick/2; dx++ {
					if x+dx >= 0 && x+dx < ogImageW && y+dy >= 0 && y+dy < ogImageH {
						img.SetRGBA(x+dx, y+dy, c)
					}
				}
			}
		}
	}
	seg(pts[0], pts[1])
	seg(pts[1], pts[2])
}

// ogFetchLogo fetches and decodes a form logo image (used in place of the
// generic "JF" mark). Returns nil on any failure so the renderer falls back
// to the JomForm mark. The logo is owner-gated (set by the merchant), so this
// is not an arbitrary SSRF surface; a short timeout keeps the render fast.
func ogFetchLogo(rawURL string) image.Image {
	if rawURL == "" {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(rawURL)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	img, err := imaging.Decode(resp.Body, imaging.AutoOrientation(true))
	if err != nil {
		return nil
	}
	return img
}

// renderOGImage renders the branded 1200x630 social preview for a form,
// following the JomForm branding (issue #434):
//   - indigo→violet gradient background (tinted toward the theme color if set)
//   - real JomForm "JF" monogram (indigo→violet gradient tile with a white
//     form sheet), or the form logo when set
//   - white bold centered title
//   - light subtitle (workspace name + domain)
//   - footer with the JomForm wordmark + tagline
//   - subtle decorative indigo card/check icon on the right
func renderOGImage(title, workspace, domain, themePrimary, logoURL string) ([]byte, error) {
	ensureOGFonts()
	if ogFaceTitle == nil || ogFaceWorkspace == nil || ogFaceMark == nil || ogFaceTag == nil {
		return nil, errOGFontUnavailable
	}

	// Theme color (if set) tints the gradient top and the accents.
	accent := ogIndigo
	if c, ok := ogParseHex(themePrimary); ok {
		accent = c
	}
	gradTop := ogBlend(ogIndigo, accent, 0.5)

	img := image.NewRGBA(image.Rect(0, 0, ogImageW, ogImageH))

	// Vertical indigo→violet gradient background (branding, not mockup).
	for y := 0; y < ogImageH; y++ {
		t := float64(y) / float64(ogImageH-1)
		c := ogBlend(gradTop, ogViolet, t)
		for x := 0; x < ogImageW; x++ {
			img.SetRGBA(x, y, c)
		}
	}

	// Decorative indigo card/check icon on the right (subtle, semi-transparent).
	iconBox := image.Rect(1010, 250, 1110, 350)
	ogFillRoundedRect(img, iconBox, 24, color.RGBA{accent.R, accent.G, accent.B, 0x2e})
	ogDrawCheck(img, image.Rect(1032, 272, 1088, 328), color.RGBA{accent.R, accent.G, accent.B, 0xcc})

	// Logo mark (top-left): the form logo when set, else the real JomForm "JF"
	// monogram (indigo→violet gradient tile with a white form sheet).
	markBox := image.Rect(72, 72, 216, 216)
	if logo := ogFetchLogo(logoURL); logo != nil {
		// Draw the logo image scaled to fit the box (with a soft white backing
		// so it reads on the gradient).
		ogFillRoundedRect(img, markBox, 24, color.RGBA{0xff, 0xff, 0xff, 0xcc})
		fit := imaging.Fit(logo, markBox.Dx()-24, markBox.Dy()-24, imaging.Lanczos)
		off := image.Pt(markBox.Min.X+(markBox.Dx()-fit.Bounds().Dx())/2,
			markBox.Min.Y+(markBox.Dy()-fit.Bounds().Dy())/2)
		draw.Draw(img, fit.Bounds().Add(off), fit, image.Point{}, draw.Over)
	} else {
		ogDrawJFMonogram(img, markBox)
	}

	// Title block (wrapped, bold, white) — centered.
	titleFace := ogFaceTitle
	titleLines := ogWrap(titleFace, title, 1000)
	if len(titleLines) > 4 {
		titleLines = titleLines[:4]
	}
	lineH := 84
	startY := 300 - (len(titleLines)-1)*lineH/2
	for i, ln := range titleLines {
		ogDrawText(img, titleFace, ln, ogImageW/2, startY+i*lineH, ogWhite)
	}

	// Subtitle: workspace name + domain (light indigo).
	sub := workspace
	if domain != "" {
		if sub != "" {
			sub += " · " + domain
		} else {
			sub = domain
		}
	}
	if sub != "" {
		subLines := ogWrap(ogFaceWorkspace, sub, 900)
		if len(subLines) > 1 {
			subLines = subLines[:1]
		}
		ogDrawText(img, ogFaceWorkspace, subLines[0], ogImageW/2, startY+len(titleLines)*lineH+40, ogLight)
	}

	// Footer: JomForm wordmark + tagline (white, on the gradient).
	ogDrawText(img, ogFaceMark, "JomForm", ogImageW/2, 560, ogWhite)
	ogDrawText(img, ogFaceTag, "Order forms, agent payments & sales", ogImageW/2, 596, ogLight)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ogDrawJFMonogram draws the real JomForm "JF" monogram inside box: an
// indigo→violet gradient rounded tile with a white form sheet and the "JF"
// letters in Indigo 600, matching /brand/icon.svg.
func ogDrawJFMonogram(img *image.RGBA, box image.Rectangle) {
	// Gradient tile (indigo→violet, diagonal).
	tile := image.NewRGBA(box)
	for y := 0; y < box.Dy(); y++ {
		for x := 0; x < box.Dx(); x++ {
			t := (float64(x)/float64(box.Dx()-1) + float64(y)/float64(box.Dy()-1)) / 2
			tile.SetRGBA(x, y, ogBlend(ogIndigo, ogViolet, t))
		}
	}
	ogFillRoundedRect(img, box, 24, ogIndigo)
	draw.Draw(img, box, tile, image.Point{}, draw.Over)

	// White form sheet (rounded rect) centered in the tile.
	sheet := image.Rect(
		box.Min.X+box.Dx()/5,
		box.Min.Y+box.Dy()/5,
		box.Max.X-box.Dx()/5,
		box.Max.Y-box.Dy()/5,
	)
	ogFillRoundedRect(img, sheet, 16, ogWhite)

	// "JF" monogram in Indigo 600, centered on the sheet (smaller face so it
	// fits the inset form sheet).
	ogDrawText(img, ogFaceTitle, "JF", box.Min.X+box.Dx()/2, box.Min.Y+box.Dy()/2+22, ogIndigo600)
}

// ogImageCache caches rendered PNGs keyed by form ID so repeated social
// crawler hits don't re-render. Bounded by the number of distinct forms.
var ogImageCache sync.Map // formID -> []byte

// cachedOGImage returns the cached PNG for a form, rendering + caching on miss.
func cachedOGImage(formID, title, workspace, domain, themePrimary, logoURL string) ([]byte, error) {
	if v, ok := ogImageCache.Load(formID); ok {
		return v.([]byte), nil
	}
	pngBytes, err := renderOGImage(title, workspace, domain, themePrimary, logoURL)
	if err != nil {
		return nil, err
	}
	ogImageCache.Store(formID, pngBytes)
	return pngBytes, nil
}

// errOGFontUnavailable is returned when the embedded fonts fail to parse.
var errOGFontUnavailable = &ogFontError{}

type ogFontError struct{}

func (*ogFontError) Error() string { return "og:image font unavailable" }

// handleOGImage serves the generated default og:image for a form at
// /og/{slug}.png. Resolves the form the same way handleRender does (workspace
// subdomain or custom domain), renders the branded preview, and caches it.
func (h *PublicHandlers) handleOGImage(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSuffix(r.PathValue("slug"), ".png")
	// Live-first resolution with the rename fallback (see findPublicForm), so a
	// crawler holding a renamed form's old URL still gets the preview rather
	// than a 404.
	f := h.findPublicForm(r.Context(), r, slug)
	if f == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	workspace := h.workspaceNameFor(r.Context(), f)
	themePrimary := ""
	if f.Theme != nil {
		if v, ok := f.Theme["primary"].(string); ok {
			themePrimary = v
		}
	}
	pngBytes, err := cachedOGImage(f.ID, f.Title, workspace, r.Host, themePrimary, f.LogoURL)
	if err != nil {
		slog.Error("og_image_render", "err", err)
		http.Error(w, "image error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(pngBytes)
}

// workspaceNameFor resolves the workspace/business name for a form's account, used as
// the og:description / og:image subtitle. Falls back to the workspace slug, then
// empty.
func (h *PublicHandlers) workspaceNameFor(ctx context.Context, f *dbstore.Form) string {
	if h.Workspaces == nil {
		return ""
	}
	workspaceID, err := h.Workspaces.WorkspaceIDByAccount(ctx, f.AccountID)
	if err != nil || workspaceID == "" {
		return ""
	}
	if st, err := h.Workspaces.Settings(ctx, workspaceID); err == nil && st.BusinessName != "" {
		return st.BusinessName
	}
	if slug, err := h.Workspaces.WorkspaceSlugByAccount(ctx, f.AccountID); err == nil {
		return slug
	}
	return ""
}

// formOGMeta builds the Open Graph + Twitter meta tags for a form's public
// page (issue #411). og:image is the form logo when set, else the generated
// default at /og/{slug}.png. Returns template.HTML (already escaped).
func (h *PublicHandlers) formOGMeta(r *http.Request, f *dbstore.Form) template.HTML {
	title := f.Title
	desc := h.workspaceNameFor(r.Context(), f)
	if desc == "" {
		desc = "Order form on JomForm"
	}
	// Canonical public URL for this form (workspace subdomain or custom domain).
	u := h.formPublicURL(r, f)
	img := f.LogoURL
	if img == "" {
		// Generated default at the origin root /og/{slug}.png, so it resolves
		// on the workspace/custom domain regardless of the form's path.
		origin := u
		if pu, perr := url.Parse(u); perr == nil {
			origin = pu.Scheme + "://" + pu.Host
		}
		img = origin + "/og/" + url.PathEscape(f.Slug) + ".png"
	}
	esc := func(s string) string { return template.HTMLEscapeString(s) }
	return template.HTML(
		`<meta property="og:title" content="` + esc(title) + `">` +
			`<meta property="og:description" content="` + esc(desc) + `">` +
			`<meta property="og:url" content="` + esc(u) + `">` +
			`<meta property="og:type" content="website">` +
			`<meta property="og:image" content="` + esc(img) + `">` +
			`<meta property="og:site_name" content="JomForm">` +
			`<meta name="twitter:card" content="summary_large_image">` +
			`<meta name="twitter:title" content="` + esc(title) + `">` +
			`<meta name="twitter:description" content="` + esc(desc) + `">` +
			`<meta name="twitter:image" content="` + esc(img) + `">`,
	)
}

// formPublicURL builds the canonical public URL for a form on the current
// request's host (workspace subdomain or custom domain). Falls back to the
// request host + slug.
func (h *PublicHandlers) formPublicURL(r *http.Request, f *dbstore.Form) string {
	scheme := "https"
	if isDev {
		scheme = "http"
	}
	host := r.Host
	if sub := accountSubdomain(host); sub != "" {
		return scheme + "://" + sub + ".jomform.com/" + f.Slug
	}
	if cd := customDomainOwner(r.Context(), h.Accounts, h.Domains, host); cd != "" {
		return scheme + "://" + host + "/" + f.Slug
	}
	return scheme + "://" + host + "/" + f.Slug
}
