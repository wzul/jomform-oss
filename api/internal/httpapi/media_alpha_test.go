package httpapi

// t_ffc7f502: a transparent PNG upload was stored as an opaque JPEG. Go's JPEG
// encoder has no alpha channel — it writes each pixel's RGB and ignores A — so
// the stored artifact was fully opaque, painted with whatever colour the
// exporter had left under alpha=0 (usually black). Every accepted image is
// re-encoded by objstore.OptimizeImage, so a merchant's transparent logo came
// back as a solid box wherever it was placed, and nothing said so.
//
// THE DECISION (recorded on the card): keep the transparency. The stored
// representation now follows the PIXELS: an image with any non-opaque pixel is
// stored as PNG (alpha kept), an opaque image is stored as JPEG exactly as
// before. That is still ONE representation per media item (t_05fd8d2a) — key
// extension, stored content-type, media.mime, the size variants and the bytes
// all have to agree; the rule was never that the type must be JPEG.
//
// Why not flatten onto white (the other branch of the card's fork): the
// merchant's logo is placed on a themed page whose background this path cannot
// know, so a white/composited box is a visible defect of the same class. And why
// not fail: a transparent logo is kilobytes as PNG (measured 104KB for a
// 1920x1920 flat logo, 31KB with BestCompression), so the common case is
// comfortable. A photographic cutout that cannot fit under the 2MB stored cap
// even after downscaling is REJECTED by the optimiser with a message naming
// transparency — never silently flattened.
//
// This test pins the behaviour end to end through the real handler:
//   - the INPUT really has a transparent pixel (else it proves nothing);
//   - the base artifact AND all three size variants are PNG, served image/png,
//     recorded image/png, and still carry alpha=0 where the input had it;
//   - an opaque PNG still takes the JPEG path (the default did not move).
//
// The merchant-facing half of the same decision lives in
// web/src/lib/mediaAlphaTransparency.test.ts.

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/objstore"
)

// transparentPNGFixture builds a PNG whose left half is fully transparent (with
// a non-black colour stored under alpha=0, so a flatten-to-RGB artifact cannot
// pass by accident) and whose right half is opaque green.
func transparentPNGFixture(t *testing.T) []byte {
	t.Helper()
	const w, h = 40, 40
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x < w/2 {
				img.SetNRGBA(x, y, color.NRGBA{R: 200, G: 30, B: 30, A: 0})
			} else {
				img.SetNRGBA(x, y, color.NRGBA{G: 200, A: 255})
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode fixture: %v", err)
	}
	return buf.Bytes()
}

// opaquePNGFixture is an ordinary opaque PNG: it must keep the JPEG path, so the
// card cannot be "fixed" by switching every upload to PNG.
func opaquePNGFixture(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 16), G: uint8(y * 16), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode fixture: %v", err)
	}
	return buf.Bytes()
}

// alphaAt decodes a stored artifact and reports the 16-bit alpha channel of one
// pixel. Alpha is the whole point of the card: RGB can be anything.
func alphaAt(t *testing.T, label string, stored []byte, x, y int) (uint32, uint32, uint32, uint32) {
	t.Helper()
	img, format, err := image.Decode(bytes.NewReader(stored))
	if err != nil {
		t.Fatalf("%s: stored bytes do not decode as an image: %v", label, err)
	}
	if format != "png" {
		t.Fatalf("%s: stored bytes decode as %q, want png (the alpha channel is the artifact)", label, format)
	}
	r, g, b, a := img.At(x, y).RGBA()
	return r, g, b, a
}

func TestTransparentPNGUploadKeepsItsAlpha(t *testing.T) {
	ctx := context.Background()
	in := transparentPNGFixture(t)

	// The fixture must really carry a transparent pixel, or the assertion below
	// would pass vacuously.
	if _, _, _, a := alphaAt(t, "fixture", in, 0, 0); a != 0 {
		t.Fatalf("fixture pixel (0,0) alpha = %d, want 0 — the input must be transparent", a)
	}

	h, accountID, raw := uploadQuotaFixture(t)
	code, body := signatureUpload(t, h, raw, "/v1/rest/uploads/image", "logo.png", "image/png", in)
	if code != http.StatusOK {
		t.Fatalf("transparent PNG upload = %d, want 200 (body %s)", code, body)
	}
	var resp struct {
		URL   string            `json:"url"`
		Sizes map[string]string `json:"sizes"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode upload response %q: %v", body, err)
	}
	if resp.URL == "" {
		t.Fatalf("upload response has no url: %s", body)
	}
	if len(resp.Sizes) != len(objstore.ImageSizes) {
		t.Fatalf("sizes = %v, want all %d variants (a PNG base is a sized image)",
			resp.Sizes, len(objstore.ImageSizes))
	}

	baseKey := strings.TrimPrefix(resp.URL, fakeMediaPrefix)
	if baseKey == resp.URL {
		t.Fatalf("unexpected public url %q (want the fake store's %q prefix)", resp.URL, fakeMediaPrefix)
	}
	if !strings.HasSuffix(baseKey, ".png") {
		t.Fatalf("base key %q must end .png — the stored bytes are PNG so they can hold alpha", baseKey)
	}

	urls := map[string]string{"base": resp.URL}
	for size, u := range resp.Sizes {
		urls[size] = u
	}
	for label, u := range urls {
		key := strings.TrimPrefix(u, fakeMediaPrefix)
		if !strings.HasSuffix(key, ".png") {
			t.Errorf("%s: key %q must end .png — every variant of a transparent base is PNG", label, key)
		}
		stored, storedCT, err := h.Store.GetObject(ctx, key)
		if err != nil {
			t.Fatalf("%s: GetObject(%q): %v", label, key, err)
		}
		if detected := http.DetectContentType(stored); detected != "image/png" {
			t.Errorf("%s: the stored bytes are %q, want image/png", label, detected)
		}
		if storedCT != "image/png" {
			t.Errorf("%s: object metadata declares %q but the bytes are PNG", label, storedCT)
		}
		// THE CARD: the pixel that was transparent is still transparent. Before
		// t_ffc7f502 this was 65535 (opaque) at every size.
		if _, _, _, a := alphaAt(t, label, stored, 0, 0); a != 0 {
			t.Errorf("%s: stored pixel (0,0) alpha = %d, want 0 — the transparency was flattened", label, a)
		}
		// ...and the opaque half is untouched.
		if _, g, _, a := alphaAt(t, label, stored, 39, 0); a != 0xffff || g>>8 != 200 {
			t.Errorf("%s: stored pixel (39,0) = green %d alpha %d, want green 200 alpha 65535", label, g>>8, a)
		}
		rec := restMediaServe(t, h, "acme.jomform.com", key)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: GET /media/%s = %d (%s), want 200", label, key, rec.Code, rec.Body.String())
		}
		if served := rec.Header().Get("Content-Type"); served != "image/png" {
			t.Errorf("%s: served content-type %q, want image/png", label, served)
		}
	}

	// The media library row must describe the PNG the merchant got, not the
	// "JPEG" every upload used to be recorded as.
	m, err := h.Media.ByKey(ctx, accountID, baseKey)
	if err != nil {
		t.Fatalf("media row ByKey(%q): %v", baseKey, err)
	}
	if m == nil {
		t.Fatalf("no media row recorded for %q", baseKey)
	}
	if m.Mime != "image/png" {
		t.Errorf("media.mime = %q, want image/png (the stored artifact's real type)", m.Mime)
	}
}

// TestOpaquePNGUploadStillStoresJPEG keeps the default honest: a PNG that
// carries NO transparency has nothing to preserve, so it is stored as JPEG (with
// the quality ladder and the smaller bytes that come with it). If this ever goes
// PNG, "keep the alpha" has quietly become "store every image as PNG".
func TestOpaquePNGUploadStillStoresJPEG(t *testing.T) {
	ctx := context.Background()
	h, _, raw := uploadQuotaFixture(t)
	code, body := signatureUpload(t, h, raw, "/v1/rest/uploads/image", "photo.png", "image/png", opaquePNGFixture(t))
	if code != http.StatusOK {
		t.Fatalf("opaque PNG upload = %d, want 200 (body %s)", code, body)
	}
	var resp struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode upload response %q: %v", body, err)
	}
	baseKey := strings.TrimPrefix(resp.URL, fakeMediaPrefix)
	if !strings.HasSuffix(baseKey, ".jpg") {
		t.Fatalf("base key %q must end .jpg — an opaque image has no alpha to preserve", baseKey)
	}
	stored, storedCT, err := h.Store.GetObject(ctx, baseKey)
	if err != nil {
		t.Fatalf("GetObject(%q): %v", baseKey, err)
	}
	if detected := http.DetectContentType(stored); detected != "image/jpeg" {
		t.Errorf("the stored bytes are %q, want image/jpeg", detected)
	}
	if storedCT != "image/jpeg" {
		t.Errorf("object metadata declares %q but the bytes are JPEG", storedCT)
	}
}

// TestTransparentAvatarUploadKeepsItsAlpha: the avatar path runs its own
// optimiser (OptimizeAvatar) and had the same flattening bug, so it is pinned
// here too — a profile picture cut out with a transparent background is the same
// defect on a different surface.
func TestTransparentAvatarUploadKeepsItsAlpha(t *testing.T) {
	ctx := context.Background()
	in := transparentPNGFixture(t)
	h, _, raw := uploadQuotaFixture(t)
	code, body := signatureUpload(t, h, raw, "/v1/rest/uploads/avatar", "me.png", "image/png", in)
	if code != http.StatusOK {
		t.Fatalf("transparent avatar upload = %d, want 200 (body %s)", code, body)
	}
	var resp struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode avatar response %q: %v", body, err)
	}
	key := strings.TrimPrefix(resp.URL, fakeMediaPrefix)
	if !strings.HasSuffix(key, ".png") {
		t.Fatalf("avatar key %q must end .png — the avatar keeps its transparency", key)
	}
	stored, storedCT, err := h.Store.GetObject(ctx, key)
	if err != nil {
		t.Fatalf("GetObject(%q): %v", key, err)
	}
	if storedCT != "image/png" {
		t.Errorf("avatar object metadata declares %q, want image/png", storedCT)
	}
	if _, _, _, a := alphaAt(t, "avatar", stored, 0, 0); a != 0 {
		t.Errorf("avatar stored pixel (0,0) alpha = %d, want 0 — the transparency was flattened", a)
	}
}
