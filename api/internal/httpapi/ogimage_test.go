package httpapi

import (
	"bytes"
	"image/png"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// Issue #411: the generated default og:image must render a valid 1200x630 PNG
// and the form OG meta must include title/description/url/image + twitter:card.
func TestRenderOGImageProducesPNG(t *testing.T) {
	pngBytes, err := renderOGImage("Wedding RSVP", "Aisyah & Hafiz", "wan.jomform.com", "", "")
	if err != nil {
		t.Fatalf("renderOGImage: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != ogImageW || b.Dy() != ogImageH {
		t.Fatalf("og image size = %dx%d, want %dx%d", b.Dx(), b.Dy(), ogImageW, ogImageH)
	}
	// The approved mockup has a white lower half, so the dark title/footer
	// text must be present (not a blank card).
	dark := 0
	for y := 0; y < b.Dy(); y += 4 {
		for x := 0; x < b.Dx(); x += 4 {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r < 30000 && g < 30000 && bl < 30000 {
				dark++
			}
		}
	}
	if dark < 50 {
		t.Fatalf("og image has too few dark text pixels: %d", dark)
	}
}

func TestRenderOGImageWrapsLongTitle(t *testing.T) {
	// A very long title must not panic and must still render.
	if _, err := renderOGImage(strings.Repeat("A very long form title ", 20), "Workspace", "wan.jomform.com", "", ""); err != nil {
		t.Fatalf("renderOGImage long title: %v", err)
	}
}

func TestRenderOGImageAcceptsThemeAndLogo(t *testing.T) {
	// A theme color + logo URL must not break rendering (logo fetch fails
	// gracefully and falls back to the JF mark).
	if _, err := renderOGImage("RSVP Majlis Kahwin", "Abu & Aminah", "wan.jomform.com", "#7c3aed", "https://cdn.example.com/logo.png"); err != nil {
		t.Fatalf("renderOGImage with theme+logo: %v", err)
	}
}

func TestFormOGMetaIncludesTags(t *testing.T) {
	h := &PublicHandlers{}
	f := &dbstore.Form{Slug: "rsvp", Title: "Wedding RSVP"}
	r := httptest.NewRequest("GET", "https://wan.jomform.com/rsvp", nil)
	meta := h.formOGMeta(r, f)
	for _, want := range []string{
		`property="og:title" content="Wedding RSVP"`,
		`property="og:type" content="website"`,
		`property="og:image"`,
		`name="twitter:card" content="summary_large_image"`,
	} {
		if !strings.Contains(string(meta), want) {
			t.Errorf("og meta missing %q", want)
		}
	}
	// No custom logo -> generated default og:image on the workspace subdomain.
	// Scheme depends on the package-global isDev (set by other tests), so
	// assert on the host+path only.
	if !strings.Contains(string(meta), "wan.jomform.com/og/rsvp.png") {
		t.Errorf("og meta missing generated default image URL")
	}
}

func TestFormOGMetaUsesCustomLogo(t *testing.T) {
	h := &PublicHandlers{}
	f := &dbstore.Form{Slug: "rsvp", Title: "Wedding RSVP", LogoURL: "https://cdn.example.com/logo.png"}
	r := httptest.NewRequest("GET", "https://wan.jomform.com/rsvp", nil)
	meta := h.formOGMeta(r, f)
	if !strings.Contains(string(meta), `property="og:image" content="https://cdn.example.com/logo.png"`) {
		t.Errorf("og meta should use the custom logo, got %s", string(meta))
	}
}
