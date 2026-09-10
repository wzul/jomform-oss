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
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// Issue #411: GitHub-style default Open Graph image. Every form gets its own
// on-the-fly branded 1200x630 social preview (title + space + JomForm
// branding) rendered server-side as a PNG — no headless browser dependency.
// If the merchant sets a custom og:image (or a form logo), that is used
// instead; otherwise the generated default is served at /og/{slug}.png.

const (
	ogImageW = 1200
	ogImageH = 630
)

// ogBrandColors mirror the JomForm brand gradient (indigo → violet).
var (
	ogIndigo = color.RGBA{0x63, 0x66, 0xf1, 0xff}
	ogViolet = color.RGBA{0x8b, 0x5c, 0xf6, 0xff}
	ogWhite  = color.RGBA{0xff, 0xff, 0xff, 0xff}
	ogMuted  = color.RGBA{0xe0, 0xe7, 0xff, 0xff}
)

// ogFonts are lazily parsed once (they are embedded in the x/image module).
var (
	ogFontOnce sync.Once
	// Faces at the sizes used by the card layout.
	ogFaceTitle font.Face // bold, large (title)
	ogFaceSpace font.Face // medium (space/business name)
	ogFaceMark  font.Face // bold, extra-large (JF mark + wordmark)
	ogFaceTag   font.Face // regular (tagline)
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
		ogFaceSpace = ogFace(gomedium.TTF, 34)
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
// spaces. Returns the wrapped lines.
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

// renderOGImage renders the branded 1200x630 social preview for a form.
// title is the form title; space is the space/business name (may be empty).
func renderOGImage(title, space string) ([]byte, error) {
	ensureOGFonts()
	if ogFaceTitle == nil || ogFaceSpace == nil || ogFaceMark == nil || ogFaceTag == nil {
		return nil, errOGFontUnavailable
	}

	img := image.NewRGBA(image.Rect(0, 0, ogImageW, ogImageH))

	// Vertical brand gradient background.
	for y := 0; y < ogImageH; y++ {
		t := float64(y) / float64(ogImageH-1)
		c := color.RGBA{
			R: uint8(float64(ogIndigo.R) + (float64(ogViolet.R)-float64(ogIndigo.R))*t),
			G: uint8(float64(ogIndigo.G) + (float64(ogViolet.G)-float64(ogIndigo.G))*t),
			B: uint8(float64(ogIndigo.B) + (float64(ogViolet.B)-float64(ogIndigo.B))*t),
			A: 0xff,
		}
		for x := 0; x < ogImageW; x++ {
			img.SetRGBA(x, y, c)
		}
	}

	// "JF" mark in a rounded-ish white translucent box (top-left).
	mark := image.Rect(72, 72, 288, 288)
	draw.Draw(img, mark, &image.Uniform{color.RGBA{0xff, 0xff, 0xff, 0x24}}, image.Point{}, draw.Src)
	ogDrawText(img, ogFaceMark, "JF", 180, 238, ogWhite)

	// Title block (wrapped, bold, large) — centered vertically.
	titleFace := ogFaceTitle
	titleLines := ogWrap(titleFace, title, 1000)
	// Cap at 4 lines so the card never overflows.
	if len(titleLines) > 4 {
		titleLines = titleLines[:4]
	}
	lineH := 84
	startY := 300 - (len(titleLines)-1)*lineH/2
	for i, ln := range titleLines {
		ogDrawText(img, titleFace, ln, ogImageW/2, startY+i*lineH, ogWhite)
	}

	// Space / business name under the title (muted).
	if space != "" {
		spaceLines := ogWrap(ogFaceSpace, space, 900)
		if len(spaceLines) > 1 {
			spaceLines = spaceLines[:1]
		}
		ogDrawText(img, ogFaceSpace, spaceLines[0], ogImageW/2, startY+len(titleLines)*lineH+40, ogMuted)
	}

	// JomForm wordmark at the bottom.
	ogDrawText(img, ogFaceMark, "JomForm", ogImageW/2, 560, ogWhite)
	ogDrawText(img, ogFaceTag, "Order forms, agent payments & sales", ogImageW/2, 596, ogMuted)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ogImageCache caches rendered PNGs keyed by form ID so repeated social
// crawler hits don't re-render. Bounded by the number of distinct forms.
var ogImageCache sync.Map // formID -> []byte

// cachedOGImage returns the cached PNG for a form, rendering + caching on miss.
func cachedOGImage(formID, title, space string) ([]byte, error) {
	if v, ok := ogImageCache.Load(formID); ok {
		return v.([]byte), nil
	}
	pngBytes, err := renderOGImage(title, space)
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
// /og/{slug}.png. Resolves the form the same way handleRender does (space
// subdomain or custom domain), renders the branded preview, and caches it.
func (h *PublicHandlers) handleOGImage(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSuffix(r.PathValue("slug"), ".png")
	var f *dbstore.Form
	var err error
	if sub := accountSubdomain(r.Host); sub != "" {
		f, err = h.Forms.BySpaceSlug(r.Context(), sub, slug)
	} else if cd := customDomainOwner(r.Context(), h.Accounts, h.Domains, r.Host); cd != "" {
		f, err = h.Forms.ByAccountSlug(r.Context(), cd, slug)
	} else {
		err = dbstore.ErrFormNotFound
	}
	if err != nil || f == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	space := h.spaceNameFor(r.Context(), f)
	pngBytes, err := cachedOGImage(f.ID, f.Title, space)
	if err != nil {
		slog.Error("og_image_render", "err", err)
		http.Error(w, "image error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(pngBytes)
}

// spaceNameFor resolves the space/business name for a form's account, used as
// the og:description / og:image subtitle. Falls back to the space slug, then
// empty.
func (h *PublicHandlers) spaceNameFor(ctx context.Context, f *dbstore.Form) string {
	if h.Spaces == nil {
		return ""
	}
	spaceID, err := h.Spaces.SpaceIDByAccount(ctx, f.AccountID)
	if err != nil || spaceID == "" {
		return ""
	}
	if st, err := h.Spaces.Settings(ctx, spaceID); err == nil && st.BusinessName != "" {
		return st.BusinessName
	}
	if slug, err := h.Spaces.SpaceSlugByAccount(ctx, f.AccountID); err == nil {
		return slug
	}
	return ""
}

// formOGMeta builds the Open Graph + Twitter meta tags for a form's public
// page (issue #411). og:image is the form logo when set, else the generated
// default at /og/{slug}.png. Returns template.HTML (already escaped).
func (h *PublicHandlers) formOGMeta(r *http.Request, f *dbstore.Form) template.HTML {
	title := f.Title
	desc := h.spaceNameFor(r.Context(), f)
	if desc == "" {
		desc = "Order form on JomForm"
	}
	// Canonical public URL for this form (space subdomain or custom domain).
	u := h.formPublicURL(r, f)
	img := f.LogoURL
	if img == "" {
		// Generated default at the origin root /og/{slug}.png, so it resolves
		// on the space/custom domain regardless of the form's path.
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
// request's host (space subdomain or custom domain). Falls back to the
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
