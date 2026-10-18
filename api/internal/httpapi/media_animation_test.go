package httpapi

// t_d55ce0d4: an animated GIF upload was silently stored as a still JPEG while
// the handler claimed the opposite ("GIFs are animated and stored as-is, so
// they get no sized variants"). Objstore.OptimizeImage decodes the first frame
// of a GIF and JPEG-encodes it, so the animation was never preserved by
// anything.
//
// THE DECISION (recorded on the card): keep the conversion — one representation
// per media item, the optimiser's JPEG — and TELL the merchant instead of
// keeping the loss silent. The reason it is the conversion and not "store the
// GIF bytes as-is": the stored cap is 2MB (MaxStoredBytes), and real animated
// GIFs are routinely larger than that, so a pass-through would either reject
// most animated GIFs or break the stored-size invariant the quota is built on.
// Preserving animation is a product/storage decision, not a bug fix.
//
// This test pins the chosen behaviour end to end through the real handler:
//   - the INPUT really is animated (2 frames) — otherwise the test would pass
//     vacuously;
//   - the stored artifact is ONE still JPEG (base + all three size variants):
//     not a GIF, served image/jpeg, media.mime image/jpeg;
//   - the merchant-facing copy that states this lives in
//     web/src/lib/mediaStillJpeg.test.ts, which pins the same decision on the
//     side the merchant actually reads.

import (
	"bytes"
	"context"
	"encoding/json"
	"image/gif"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/objstore"
)

func TestAnimatedGIFUploadIsStoredAsOneStillJPEG(t *testing.T) {
	ctx := context.Background()
	in := tinyGIF(t)

	// The fixture must carry a real animation, or "the animation is lost" is
	// not what this test measures.
	frames, err := gif.DecodeAll(bytes.NewReader(in))
	if err != nil {
		t.Fatalf("GIF fixture does not decode as a GIF: %v", err)
	}
	if len(frames.Image) != 2 {
		t.Fatalf("GIF fixture has %d frame(s), want 2 — the input must be animated", len(frames.Image))
	}

	h, accountID, raw := uploadQuotaFixture(t)
	code, body := signatureUpload(t, h, raw, "/v1/rest/uploads/image", "logo.gif", "image/gif", in)
	if code != http.StatusOK {
		t.Fatalf("animated GIF upload = %d, want 200 (body %s)", code, body)
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
		t.Fatalf("sizes = %v, want all %d variants (a GIF is stored as a sized JPEG now)",
			resp.Sizes, len(objstore.ImageSizes))
	}

	baseKey := strings.TrimPrefix(resp.URL, fakeMediaPrefix)
	if baseKey == resp.URL {
		t.Fatalf("unexpected public url %q (want the fake store's %q prefix)", resp.URL, fakeMediaPrefix)
	}
	if !strings.HasSuffix(baseKey, ".jpg") {
		t.Fatalf("base key %q must end .jpg — the stored bytes are the optimiser's JPEG", baseKey)
	}

	urls := map[string]string{"base": resp.URL}
	for size, u := range resp.Sizes {
		urls[size] = u
	}
	for label, u := range urls {
		key := strings.TrimPrefix(u, fakeMediaPrefix)
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
		// The whole card: the animation does NOT survive the upload. If this
		// ever stops holding, the merchant-facing copy is a lie and this test
		// (and web/src/lib/mediaStillJpeg.test.ts) has to be re-decided.
		if _, err := gif.DecodeAll(bytes.NewReader(stored)); err == nil {
			t.Errorf("%s: the stored artifact still decodes as a GIF, so the animation was kept "+
				"while the media page says it is saved as a still JPEG", label)
		}
		rec := restMediaServe(t, h, "acme.jomform.com", key)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: GET /media/%s = %d (%s), want 200", label, key, rec.Code, rec.Body.String())
		}
		if served := rec.Header().Get("Content-Type"); served != "image/jpeg" {
			t.Errorf("%s: served content-type %q, want image/jpeg", label, served)
		}
	}

	// The media library row must describe the still JPEG the merchant got, not
	// the GIF they uploaded.
	m, err := h.Media.ByKey(ctx, accountID, baseKey)
	if err != nil {
		t.Fatalf("media row ByKey(%q): %v", baseKey, err)
	}
	if m == nil {
		t.Fatalf("no media row recorded for %q", baseKey)
	}
	if m.Mime != "image/jpeg" {
		t.Errorf("media.mime = %q, want image/jpeg (the stored artifact's real type)", m.Mime)
	}
}
