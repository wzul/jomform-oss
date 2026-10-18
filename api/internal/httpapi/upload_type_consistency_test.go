package httpapi

// t_05fd8d2a: one media item used to be described three different ways — the
// object key extension and the stored content-type came from the INPUT the
// merchant uploaded, while the bytes (and every size variant) were the
// optimiser's JPEG. A PNG upload was therefore a `.png` key holding JPEG bytes
// served `image/png`, with `-thumbnail`/`-medium`/`-full` siblings declaring
// `image/jpeg`. Downloads and any consumer that trusts the declared type got a
// file that was not what it said it was.
//
// These tests pin the one-representation rule end to end:
//   - the optimiser emits JPEG for every raster input it accepts (base and
//     every size variant);
//   - after an upload the bytes' sniffed type == the stored content-type ==
//     media.mime == what /media serves, for the base key and all three
//     variants, and every key ends in the extension those bytes have;
//   - an object whose stored metadata lies (every upload made before this fix)
//     is served by its bytes.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/objstore"
)

// fakeMediaPrefix is what the fake object store's PublicURL prefixes keys with.
const fakeMediaPrefix = "https://jomform.com/media/"

// tinyRealWebP is a real, decodable 4x4 WebP (98 bytes). It must be real bytes:
// the WebP case is only meaningful if the optimiser DECODES it and re-encodes,
// which a hand-made "RIFF....WEBPVP8" header with zero padding would not.
const tinyRealWebP = "UklGRloAAABXRUJQVlA4IE4AAAAQAgCdASoEAAQAAUAmJbACdEyAAdWtw2wAAP7eNd5GtJyP/lxWvkUd8/veWE3o/pZfNo7GT1IrxAff/5XaopI3Pg5MVH4h2f/v/+R8gAA="

func tinyWebP(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(tinyRealWebP)
	if err != nil {
		t.Fatalf("decode webp fixture: %v", err)
	}
	return data
}

// tinyGIF is a real two-frame GIF so the optimiser has a decodable input.
func tinyGIF(t *testing.T) []byte {
	t.Helper()
	pal := color.Palette{color.RGBA{R: 10, G: 20, B: 30, A: 255}, color.RGBA{R: 240, G: 230, B: 220, A: 255}}
	frames := make([]*image.Paletted, 0, 2)
	for f := 0; f < 2; f++ {
		fr := image.NewPaletted(image.Rect(0, 0, 8, 8), pal)
		for i := range fr.Pix {
			fr.Pix[i] = uint8((i + f) % len(pal))
		}
		frames = append(frames, fr)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, &gif.GIF{
		Image:  frames,
		Delay:  []int{10, 10},
		Config: image.Config{ColorModel: pal, Width: 8, Height: 8},
	}); err != nil {
		t.Fatalf("gif fixture: %v", err)
	}
	return buf.Bytes()
}

// assertStoredArtifactIsJPEG pins the optimiser's output contract: whatever the
// input was, the artifact is JPEG, and the artifact's own sniffed type, the MIME
// ImageArtifactMeta reports and http.DetectContentType all agree.
func assertStoredArtifactIsJPEG(t *testing.T, label string, data []byte) {
	t.Helper()
	if len(data) == 0 {
		t.Fatalf("%s: artifact is empty", label)
	}
	mime, ext, ok := objstore.ImageArtifactMeta(data)
	if !ok {
		t.Fatalf("%s: artifact bytes are not a recognisable image", label)
	}
	if mime != "image/jpeg" || ext != ".jpg" {
		t.Fatalf("%s: artifact meta = (%q, %q), want (image/jpeg, .jpg)", label, mime, ext)
	}
	if got := http.DetectContentType(data); got != mime {
		t.Fatalf("%s: http.DetectContentType = %q but the artifact is declared %q", label, got, mime)
	}
}

// TestOptimizedArtifactsAreOneJPEGType: for every accepted raster input, the
// base artifact AND all three size variants are the same JPEG type. This is the
// property the upload path relies on to stamp one MIME on key, metadata and
// media row.
func TestOptimizedArtifactsAreOneJPEGType(t *testing.T) {
	inputs := []struct {
		name string
		ext  string
		data []byte
	}{
		{"png", ".png", tinyPNG(t)},
		{"jpeg", ".jpg", fakeJPEG(t)},
		{"webp", ".webp", tinyWebP(t)},
		{"gif", ".gif", tinyGIF(t)},
		{"large-png-downscaled", ".png", generateLargeImageFixture()},
	}
	for _, in := range inputs {
		t.Run(in.name, func(t *testing.T) {
			base, err := optimizeImage(in.data, in.ext)
			if err != nil {
				t.Fatalf("optimizeImage(%s): %v", in.ext, err)
			}
			assertStoredArtifactIsJPEG(t, "base", base)
			for _, size := range objstore.ImageSizes {
				variant, err := optimizeImageSize(in.data, in.ext, size)
				if err != nil {
					t.Fatalf("optimizeImageSize(%s, %s): %v", in.ext, size, err)
				}
				assertStoredArtifactIsJPEG(t, "variant "+string(size), variant)
			}
		})
	}
}

// TestPNGUploadIsOneTypeEndToEnd is the card's acceptance test: upload a PNG
// through the real handler, then require that for the base key and every size
// variant the stored bytes' sniffed type, the stored content-type, the served
// content-type and media.mime are all the same, and that the key extension
// matches those bytes.
func TestPNGUploadIsOneTypeEndToEnd(t *testing.T) {
	ctx := context.Background()
	h, accountID, raw := uploadQuotaFixture(t)

	code, body := signatureUpload(t, h, raw, "/v1/rest/uploads/image", "logo.png", "image/png", tinyPNG(t))
	if code != http.StatusOK {
		t.Fatalf("PNG upload = %d, want 200 (body %s)", code, body)
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
		t.Fatalf("sizes = %v, want all %d variants", resp.Sizes, len(objstore.ImageSizes))
	}

	baseKey := strings.TrimPrefix(resp.URL, fakeMediaPrefix)
	if baseKey == resp.URL {
		t.Fatalf("unexpected public url %q (want the fake store's %q prefix)", resp.URL, fakeMediaPrefix)
	}
	if !strings.HasPrefix(baseKey, "u/"+accountID+"/") {
		t.Fatalf("base key %q is not account-scoped under u/%s/", baseKey, accountID)
	}

	// Base + every variant: key extension, stored content-type, served
	// content-type and the bytes must all describe the same JPEG.
	urls := map[string]string{"base": resp.URL}
	for size, u := range resp.Sizes {
		urls[size] = u
		if want := fakeMediaPrefix + buildVariantKey(baseKey, objstore.ImageSize(size)); u != want {
			t.Errorf("variant %q url = %q, want %q (variant keys derive from the base key)", size, u, want)
		}
	}
	for label, u := range urls {
		key := strings.TrimPrefix(u, fakeMediaPrefix)
		if !strings.HasSuffix(key, ".jpg") {
			t.Errorf("%s: key %q must end in the extension of the bytes stored under it", label, key)
		}
		stored, storedCT, err := h.Store.GetObject(ctx, key)
		if err != nil {
			t.Fatalf("%s: GetObject(%q): %v", label, key, err)
		}
		detected := http.DetectContentType(stored)
		if detected != "image/jpeg" {
			t.Errorf("%s: the stored bytes are %q, want image/jpeg", label, detected)
		}
		if storedCT != detected {
			t.Errorf("%s: object metadata declares %q but the bytes are %q", label, storedCT, detected)
		}
		rec := restMediaServe(t, h, "acme.jomform.com", key)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: GET /media/%s = %d (%s), want 200", label, key, rec.Code, rec.Body.String())
		}
		if served := rec.Header().Get("Content-Type"); served != detected {
			t.Errorf("%s: served content-type %q but the bytes are %q", label, served, detected)
		}
	}

	// The media row must record the same type as the object it points at.
	m, err := h.Media.ByKey(ctx, accountID, baseKey)
	if err != nil {
		t.Fatalf("media row ByKey(%q): %v", baseKey, err)
	}
	if m == nil {
		t.Fatalf("no media row recorded for %q", baseKey)
	}
	stored, _, err := h.Store.GetObject(ctx, baseKey)
	if err != nil {
		t.Fatalf("GetObject(%q): %v", baseKey, err)
	}
	if m.Mime != http.DetectContentType(stored) {
		t.Errorf("media.mime = %q but the stored bytes are %q", m.Mime, http.DetectContentType(stored))
	}
	if m.Mime != "image/jpeg" {
		t.Errorf("media.mime = %q, want image/jpeg (the stored artifact's real type)", m.Mime)
	}
}

// TestLegacyMislabeledObjectServedByItsBytes: every object uploaded before this
// fix carries metadata that lies (a `.png` key, declared image/png, holding JPEG
// bytes). Those rows cannot be rewritten without their bytes, so the serve path
// trusts the bytes for raster images and publishes what is actually being sent.
func TestLegacyMislabeledObjectServedByItsBytes(t *testing.T) {
	ctx := context.Background()
	store := newFakeObjectStore()
	h := &RESTHandlers{Store: store}

	// A pre-fix upload: PNG name + image/png metadata over the optimiser's JPEG.
	legacyKey := "u/acct-legacy/legacy.png"
	if err := store.PutObject(ctx, legacyKey, "image/png", fakeJPEG(t)); err != nil {
		t.Fatalf("seed legacy object: %v", err)
	}
	rec := restMediaServe(t, h, "acme.jomform.com", legacyKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /media/%s = %d (%s), want 200", legacyKey, rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/jpeg" {
		t.Fatalf("legacy .png key holding JPEG bytes served %q, want image/jpeg", got)
	}
	if got := http.DetectContentType(rec.Body.Bytes()); got != rec.Header().Get("Content-Type") {
		t.Fatalf("served %q over %q bytes", rec.Header().Get("Content-Type"), got)
	}

	// A truthful object is untouched: real PNG bytes declared image/png.
	goodKey := "u/acct-legacy/good.png"
	if err := store.PutObject(ctx, goodKey, "image/png", tinyPNG(t)); err != nil {
		t.Fatalf("seed png object: %v", err)
	}
	rec = restMediaServe(t, h, "acme.jomform.com", goodKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /media/%s = %d (%s), want 200", goodKey, rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("real PNG served %q, want image/png", got)
	}
}

// TestServedContentType pins which stored types the serve path is allowed to
// correct: only claimed raster images. Code assets and PDFs keep their metadata
// because the image sniffer cannot judge them.
func TestServedContentType(t *testing.T) {
	jpeg := fakeJPEG(t)
	png := tinyPNG(t)
	cases := []struct {
		name       string
		storedType string
		body       []byte
		want       string
	}{
		{"legacy-png-key-over-jpeg", "image/png", jpeg, "image/jpeg"},
		{"legacy-gif-key-over-jpeg", "image/gif", jpeg, "image/jpeg"},
		{"legacy-webp-key-over-jpeg", "image/webp", jpeg, "image/jpeg"},
		{"truthful-png", "image/png", png, "image/png"},
		{"jpeg-declared-png-bytes", "image/jpeg", png, "image/png"},
		{"css-untouched", "text/css; charset=utf-8", jpeg, "text/css; charset=utf-8"},
		{"svg-untouched", "image/svg+xml", png, "image/svg+xml"},
		{"pdf-untouched", "application/pdf", png, "application/pdf"},
		{"unreadable-image-keeps-metadata", "image/png", []byte("not an image at all, just text"), "image/png"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := objstore.ServedContentType(c.storedType, c.body); got != c.want {
				t.Fatalf("ServedContentType(%q, %s bytes) = %q, want %q", c.storedType, c.name, got, c.want)
			}
		})
	}
}
