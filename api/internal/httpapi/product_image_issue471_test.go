package httpapi

// Issue #471: product image size + layout.
//
// Unit tests for the pure helpers (size/layout validation, the media-variant
// URL rewrite) and the rendered output (the .pimg data-size / menu-card class
// and the variant URL per size). No DB: the block template is exercised
// directly, the same way field_types_render_test.go does.

import (
	"bytes"
	"strings"
	"testing"
)

func TestProductImageSizeAndLayoutAllowlists(t *testing.T) {
	for _, ok := range []string{"", "small", "medium", "large", "hidden"} {
		if !ValidProductImageSize(ok) {
			t.Errorf("ValidProductImageSize(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"tiny", "SMALL", "52px", "xl"} {
		if ValidProductImageSize(bad) {
			t.Errorf("ValidProductImageSize(%q) = true, want false", bad)
		}
	}
	for _, ok := range []string{"", "thumbnail-left", "image-top"} {
		if !ValidProductImageLayout(ok) {
			t.Errorf("ValidProductImageLayout(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"left", "top", "IMAGE-TOP", "card"} {
		if ValidProductImageLayout(bad) {
			t.Errorf("ValidProductImageLayout(%q) = true, want false", bad)
		}
	}
}

func TestProductImageSizeDefaultsToSmall(t *testing.T) {
	// An unset or invalid size must fall back to the pre-#471 default.
	if got := productImageSizeOrDefault(""); got != ProductImageSizeSmall {
		t.Errorf("default size = %q, want small", got)
	}
	if got := productImageSizeOrDefault("bogus"); got != ProductImageSizeSmall {
		t.Errorf("invalid size = %q, want small", got)
	}
	if got := productImageLayoutOrDefault(""); got != ProductImageLayoutThumbnailLeft {
		t.Errorf("default layout = %q, want thumbnail-left", got)
	}
	if got := productImageLayoutOrDefault("bogus"); got != ProductImageLayoutThumbnailLeft {
		t.Errorf("invalid layout = %q, want thumbnail-left", got)
	}
}

func TestProductImageVariantNamePerSize(t *testing.T) {
	cases := map[string]string{
		"":       "thumbnail",
		"small":  "thumbnail",
		"medium": "medium",
		"large":  "full",
	}
	for size, want := range cases {
		if got := productImageVariantName(size); got != want {
			t.Errorf("variant for %q = %q, want %q", size, got, want)
		}
	}
}

func TestProductImageVariantURLRewritesCanonicalMediaURL(t *testing.T) {
	const base = "https://jomform.com/media/u/acct-1/11111111-2222-3333-4444-555555555555.jpg"
	cases := []struct{ size, want string }{
		{"small", "https://jomform.com/media/u/acct-1/11111111-2222-3333-4444-555555555555-thumbnail.jpg"},
		{"medium", "https://jomform.com/media/u/acct-1/11111111-2222-3333-4444-555555555555-medium.jpg"},
		{"large", "https://jomform.com/media/u/acct-1/11111111-2222-3333-4444-555555555555-full.jpg"},
		// An unset size still resolves to the thumbnail variant (default).
		{"", "https://jomform.com/media/u/acct-1/11111111-2222-3333-4444-555555555555-thumbnail.jpg"},
	}
	for _, c := range cases {
		if got := ProductImageVariantURL(base, c.size); got != c.want {
			t.Errorf("variant URL for size %q = %q, want %q", c.size, got, c.want)
		}
	}
	// .png / .webp keep their extension.
	png := "https://jomform.com/media/u/acct-1/aaaa.png"
	if got := ProductImageVariantURL(png, "medium"); got != "https://jomform.com/media/u/acct-1/aaaa-medium.png" {
		t.Errorf("png variant URL = %q", got)
	}
}

func TestProductImageVariantURLLeavesNonMediaURLsAlone(t *testing.T) {
	// External URLs, GIFs (stored as-is), SVG code assets, query strings and
	// empty URLs must never be rewritten — a rewrite would break the image.
	for _, url := range []string{
		"",
		"https://cdn.example/photo.jpg",
		"https://cdn.example/u/only-two-segments.jpg",
		"https://cdn.example/u/acct-1/extra/deep.jpg",
		"https://jomform.com/media/u/acct-1/animated.gif",
		"https://jomform.com/media/u/acct-1/logo.svg",
		"https://jomform.com/media/u/acct-1/pic.jpg?v=2",
		"https://jomform.com/media/u/acct-1/noext",
	} {
		if got := ProductImageVariantURL(url, "large"); got != url {
			t.Errorf("URL %q was rewritten to %q, want unchanged", url, got)
		}
	}
}

func TestProductImageForSizeHidden(t *testing.T) {
	base := "https://jomform.com/media/u/acct-1/x.jpg"
	if got := productImageForSize(base, "hidden"); got != "" {
		t.Errorf("hidden size returned %q, want empty (no image)", got)
	}
	if got := productImageForSize(base, "large"); got == "" {
		t.Error("large size returned empty image URL")
	}
	if got := productImageForSize("", "large"); got != "" {
		t.Errorf("empty image returned %q, want empty", got)
	}
}

// productImageBlock mirrors the blockView fields the product template reads.
type productImageBlock struct {
	ID, Type, Value string
	ProductID       string
	PriceSen        int64
	HasPrice        bool
	HasQuantity     bool
	// Card t_06851d88: pay-what-you-want product blocks.
	CustomAmount     bool
	AmountMinSen     int64
	AmountMaxSen     int64
	AmountSuggestSen int64
	AmountHint       string
	Image            string
	ImgSize          string
	ImgTop           bool
	BaseImage        string
	Style            string
	Placeholder      string
	Description      string
	CSSClass         string
	Format           string
	Pattern          string
	RequiredMsg      string
	MaxLength        int
	DefaultValue     string
	Min, Max         string
	Step             string
	Variations       []struct{}
	// CondJSON is the block's conditional-logic rule (card t_9e875bca). The
	// template reads it for EVERY block in the range — a conditional block is
	// wrapped in the element the page's script shows/hides — so a fixture that
	// hand-builds the view needs the field even when it renders no rule.
	CondJSON string
}

func renderProductBlock(t *testing.T, b productImageBlock) string {
	t.Helper()
	var buf bytes.Buffer
	if err := blockTmpl.Execute(&buf, map[string]any{
		"Slug": "menu", "Title": "Menu", "Blocks": []productImageBlock{b},
		"Lang": "en", "Ls": formLangs["en"],
	}); err != nil {
		t.Fatalf("blockTmpl.Execute: %v", err)
	}
	return buf.String()
}

func TestProductImageRendersChosenSize(t *testing.T) {
	body := renderProductBlock(t, productImageBlock{
		Type: "product", Value: "Nasi Lemak", ProductID: "p1", PriceSen: 2100, HasPrice: true,
		Image:     "https://jomform.com/media/u/a/p1-medium.jpg",
		BaseImage: "https://jomform.com/media/u/a/p1.jpg",
		ImgSize:   "medium",
	})
	if !strings.Contains(body, `<img class="pimg" data-size="medium" src="https://jomform.com/media/u/a/p1-medium.jpg"`) {
		t.Errorf("medium product image not rendered with its size\nbody:\n%s", body)
	}
	if !strings.Contains(body, `data-base="https://jomform.com/media/u/a/p1.jpg"`) {
		t.Error("full-size fallback URL (data-base) missing")
	}
	// thumbnail-left is the default layout: no menu-card class on the card.
	if strings.Contains(body, `<div class="prod menu-card">`) {
		t.Error("thumbnail-left layout must not add the menu-card class")
	}
	if !strings.Contains(body, ".pimg[data-size=medium]") || !strings.Contains(body, ".pimg[data-size=large]") {
		t.Error("size CSS rules missing")
	}
}

func TestProductImageRendersImageTopMenuCard(t *testing.T) {
	body := renderProductBlock(t, productImageBlock{
		Type: "product", Value: "Nasi Lemak", ProductID: "p1", PriceSen: 2100, HasPrice: true,
		Image:     "https://jomform.com/media/u/a/p1-full.jpg",
		BaseImage: "https://jomform.com/media/u/a/p1.jpg",
		ImgSize:   "large",
		ImgTop:    true,
	})
	if !strings.Contains(body, `<div class="prod menu-card">`) {
		t.Errorf("image-top layout did not add the menu-card class\nbody:\n%s", body)
	}
	// The image must sit BEFORE the checkbox/name row (menu-card: image above).
	imgIdx := strings.Index(body, `class="pimg"`)
	nameIdx := strings.Index(body, `class="pname"`)
	if imgIdx < 0 || nameIdx < 0 || imgIdx > nameIdx {
		t.Errorf("menu-card image must precede the name (img=%d name=%d)", imgIdx, nameIdx)
	}
	if !strings.Contains(body, `.prod.menu-card .pimg {`) {
		t.Error("menu-card CSS missing")
	}
}

func TestProductImageHiddenRendersNoImage(t *testing.T) {
	// The handler clears Image/ImgSize when the merchant chose hidden, so the
	// template renders a plain product row (no <img class="pimg">).
	body := renderProductBlock(t, productImageBlock{
		Type: "product", Value: "Nasi Lemak", ProductID: "p1", PriceSen: 2100, HasPrice: true,
	})
	if strings.Contains(body, `class="pimg"`) {
		t.Errorf("hidden size rendered a product image\nbody:\n%s", body)
	}
	if !strings.Contains(body, `class="pname"`) {
		t.Error("product name missing")
	}
}

func TestProductImageVariantFallbackScriptPresent(t *testing.T) {
	// The rendered page ships an onerror fallback so an image uploaded before
	// #382 (no generated variants) still displays.
	body := renderProductBlock(t, productImageBlock{
		Type: "product", Value: "X", ProductID: "p1", PriceSen: 100, HasPrice: true,
		Image: "https://jomform.com/media/u/a/x-full.jpg", BaseImage: "https://jomform.com/media/u/a/x.jpg",
		ImgSize: "large",
	})
	if !strings.Contains(body, "img.pimg[data-base]") {
		t.Error("variant fallback script missing")
	}
}
