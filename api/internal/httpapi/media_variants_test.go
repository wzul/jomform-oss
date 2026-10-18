package httpapi

// t_466ff771: the media library advertised DERIVED variant URLs for every row
// whose mime is a sized raster, on the ASSUMPTION that the upload pipeline had
// written them. `uploadAvatar` never generates variants — an avatar is
// single-rendition (a ≤256px JPEG) — so the account's own avatar row advertised
// -thumbnail/-medium/-full URLs whose objects do not exist. All three 404, and
// the media grid prefers `sizes.thumbnail` over the base url, so the account's
// avatar tile rendered a broken image.
//
// The rule these tests pin is the one the listing must obey: NEVER advertise a
// derived URL that has not been confirmed to exist. Fixing it at the
// advertisement site (rather than by generating avatar variants) is what makes
// rows written BEFORE the fix — every avatar already sitting in a library —
// honest as well, and it closes the same failure for any future partial variant
// write (the upload path warns and continues when a variant Put fails).
//
// The same derivation exists for the PDF preview URL in the same handler: a PDF
// whose preview was never rendered (Ghostscript unavailable, render failed —
// RenderPDFPreview is best-effort and returns nil) advertised a -preview.jpg
// that 404s. It is the same defect in the same function, so it is covered by
// the same rule and the same test.
//
// Integration against a real Postgres; skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/objstore"
)

// advertisedMediaURLs returns every URL a media-list row advertises: the base
// url plus, when present, each size variant and the PDF preview. These are
// exactly the URLs a client can be handed from GET /v1/rest/media.
func advertisedMediaURLs(t *testing.T, row map[string]any) map[string]string {
	t.Helper()
	out := map[string]string{}
	if u, _ := row["url"].(string); u != "" {
		out["url"] = u
	}
	if sizes, ok := row["sizes"].(map[string]any); ok {
		for name, v := range sizes {
			u, _ := v.(string)
			if u == "" {
				t.Errorf("row %v advertises an empty url for size %q", row["key"], name)
				continue
			}
			out["size:"+name] = u
		}
	}
	if p, _ := row["preview"].(string); p != "" {
		out["preview"] = p
	}
	return out
}

// TestMediaListAdvertisesOnlyExistingVariantURLs is the card's acceptance test:
// after an avatar upload, every URL the media list advertises for that row must
// resolve 200 with image/* bytes and a non-zero size. It also pins that the fix
// is not achieved by dropping size variants altogether (a control upload that
// really does have variants must keep advertising all three) and that a row
// written before the fix is honest too.
func TestMediaListAdvertisesOnlyExistingVariantURLs(t *testing.T) {
	ctx := context.Background()
	h, accountID, raw := uploadQuotaFixture(t)

	// (1) THE DEFECT: the avatar path records a media row and writes NO variants.
	code, body := signatureUpload(t, h, raw, "/v1/rest/uploads/avatar", "me.png", "image/png", tinyPNG(t))
	if code != http.StatusOK {
		t.Fatalf("avatar upload = %d, want 200 (body %s)", code, body)
	}
	var avatarResp struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(body), &avatarResp); err != nil {
		t.Fatalf("decode avatar response %q: %v", body, err)
	}
	avatarKey := strings.TrimPrefix(avatarResp.URL, fakeMediaPrefix)
	if avatarKey == avatarResp.URL || avatarKey == "" {
		t.Fatalf("avatar upload url %q is not a store key under %q", avatarResp.URL, fakeMediaPrefix)
	}
	for _, size := range objstore.ImageSizes {
		if vkey := buildVariantKey(avatarKey, size); vkey != "" {
			if _, _, err := h.Store.GetObject(ctx, vkey); err == nil {
				t.Fatalf("fixture assumption broken: the avatar path wrote the %q variant %q", size, vkey)
			}
		}
	}

	// (2) THE CONTROL: a normal image upload really does write all three
	// variants. A "fix" that stopped advertising sizes would satisfy (1) and
	// silently remove the grid thumbnail from every ordinary image — this row
	// is what refuses that.
	code, body = signatureUpload(t, h, raw, "/v1/rest/uploads/image", "photo.png", "image/png", tinyPNG(t))
	if code != http.StatusOK {
		t.Fatalf("image upload = %d, want 200 (body %s)", code, body)
	}
	var imageResp struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(body), &imageResp); err != nil {
		t.Fatalf("decode image response %q: %v", body, err)
	}
	controlKey := strings.TrimPrefix(imageResp.URL, fakeMediaPrefix)
	if controlKey == imageResp.URL || controlKey == "" {
		t.Fatalf("image upload url %q is not a store key under %q", imageResp.URL, fakeMediaPrefix)
	}

	// (3) A ROW WRITTEN BEFORE THIS FIX: every avatar already in a library. The
	// base object exists and the variants never did, so no rewrite of the upload
	// path can reach it — the listing has to answer for it.
	legacyKey := "u/" + accountID + "/" + randSuffix() + ".jpg"
	legacyBytes := fakeJPEG(t)
	if err := h.Store.PutObject(ctx, legacyKey, "image/jpeg", legacyBytes); err != nil {
		t.Fatalf("seed legacy avatar object: %v", err)
	}
	if err := h.Media.Create(ctx, &dbstore.Media{
		AccountID: accountID, Key: legacyKey, URL: h.Store.PublicURL(legacyKey),
		Mime: "image/jpeg", Size: int64(len(legacyBytes)), Source: dbstore.MediaSourceMerchant,
	}); err != nil {
		t.Fatalf("seed legacy avatar row: %v", err)
	}

	// (4) THE SAME DERIVATION FOR PDFs: a PDF row whose preview was never
	// rendered. RenderPDFPreview is best-effort, so this state is reachable.
	pdfKey := "u/" + accountID + "/" + randSuffix() + ".pdf"
	pdfBytes := []byte("%PDF-1.4\n% fixture\n")
	if err := h.Store.PutObject(ctx, pdfKey, "application/pdf", pdfBytes); err != nil {
		t.Fatalf("seed pdf object: %v", err)
	}
	if err := h.Media.Create(ctx, &dbstore.Media{
		AccountID: accountID, Key: pdfKey, URL: h.Store.PublicURL(pdfKey),
		Mime: "application/pdf", Size: int64(len(pdfBytes)), Source: dbstore.MediaSourceMerchant,
	}); err != nil {
		t.Fatalf("seed pdf row: %v", err)
	}

	code, resp := bundleGet(t, h, raw, "/v1/rest/media?limit=100")
	if code != http.StatusOK {
		t.Fatalf("GET /v1/rest/media = %d, body %v", code, resp)
	}
	rawItems, _ := resp["items"].([]any)
	if len(rawItems) == 0 {
		t.Fatalf("media list is empty, want the uploads + seeded rows")
	}
	byKey := map[string]map[string]any{}
	for _, it := range rawItems {
		row, _ := it.(map[string]any)
		key, _ := row["key"].(string)
		byKey[key] = row
	}

	// ACCEPTANCE: every URL the list advertises for a row resolves 200 with a
	// non-zero body, and image/* bytes for a raster image row.
	for key, row := range byKey {
		mime, _ := row["mime"].(string)
		for label, u := range advertisedMediaURLs(t, row) {
			vkey := strings.TrimPrefix(u, fakeMediaPrefix)
			if vkey == u {
				t.Errorf("row %q advertises %s url %q, which is not a store key", key, label, u)
				continue
			}
			rec := restMediaServe(t, h, "acme.jomform.com", vkey)
			if rec.Code != http.StatusOK {
				t.Errorf("row %q (mime %q) advertises %s %q but GET /media/%s = %d (%s)",
					key, mime, label, u, vkey, rec.Code, rec.Body.String())
				continue
			}
			if rec.Body.Len() == 0 {
				t.Errorf("row %q advertises %s %q but the served body is empty", key, label, u)
			}
			if isSizedImage(mime) {
				if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "image/") {
					t.Errorf("row %q advertises %s %q served as %q, want image/*", key, label, u, ct)
				}
			}
		}
	}

	// The avatar row and the pre-fix row: no variant was ever generated, so the
	// listing must advertise none. The grid then falls back to the base url,
	// which is the object that does exist.
	for _, key := range []string{avatarKey, legacyKey} {
		row, ok := byKey[key]
		if !ok {
			t.Fatalf("row %q is missing from the media list", key)
		}
		if sizes, present := row["sizes"]; present && sizes != nil {
			if n := len(sizes.(map[string]any)); n != 0 {
				t.Errorf("row %q advertises %d size variants (%v) that were never generated", key, n, sizes)
			}
		}
	}

	// The control row keeps all three real variants.
	control, ok := byKey[controlKey]
	if !ok {
		t.Fatalf("control row %q is missing from the media list", controlKey)
	}
	sizes, _ := control["sizes"].(map[string]any)
	if len(sizes) != len(objstore.ImageSizes) {
		t.Errorf("control row %q advertises %d size variants (%v), want all %d",
			controlKey, len(sizes), sizes, len(objstore.ImageSizes))
	}

	// The PDF row advertises no preview it does not have.
	pdfRow, ok := byKey[pdfKey]
	if !ok {
		t.Fatalf("pdf row %q is missing from the media list", pdfKey)
	}
	if p, present := pdfRow["preview"]; present && p != "" {
		t.Errorf("pdf row %q advertises preview %q but no preview object was ever written", pdfKey, p)
	}
}
