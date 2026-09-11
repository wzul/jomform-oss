package httpapi

// Issue #455: magic-byte (file signature) enforcement on uploads.
//
// The sniffers already derive the stored extension from the file's actual
// bytes, but nothing verified the sniffed signature matched the *kind* of
// upload the endpoint was asked to accept — so HTML bytes sent to the image
// endpoint as a `.png` were silently stored as text/html. These tests prove the
// enforcement now rejects a mismatched signature with 400 on every upload path
// (image/media, asset, avatar), and that a matching signature still uploads.
//
// The HTTP-level tests need a real Postgres (JOMFORM_TEST_DATABASE_URL) and use
// the shared fake object store, so no live S3/MinIO is required. The pure
// MagicByteMismatch table test runs everywhere.

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/objstore"
)

// ── Pure unit coverage (no DB) ───────────────────────────────────────────────

// TestMagicByteMismatchTable is the authoritative table of the enforcement
// rule: the sniffed signature must be consistent with the declared kind,
// filename extension and content-type. Unknown/ambiguous declarations are
// ignored so valid uploads are not rejected.
func TestMagicByteMismatchTable(t *testing.T) {
	cases := []struct {
		name     string
		kind     objstore.UploadKind
		filename string
		mime     string
		sniffed  string
		wantErr  bool
	}{
		// The issue's canonical spoof: HTML bytes claiming to be a PNG image.
		{"html-as-png-image", objstore.KindRasterImage, "evil.png", "image/png", ".html", true},
		{"html-as-png-media", objstore.KindMedia, "evil.png", "image/png", ".html", true},
		{"pdf-as-png-image", objstore.KindRasterImage, "evil.png", "image/png", ".pdf", true},
		{"png-as-css-asset", objstore.KindAsset, "app.css", "text/css", ".png", true},
		{"png-as-pdf", objstore.KindPDF, "doc.pdf", "application/pdf", ".png", true},
		{"pdf-as-asset", objstore.KindAsset, "x.js", "text/javascript", ".pdf", true},
		// Extension-only mismatch (content-type generic/absent).
		{"png-bytes-css-name", objstore.KindMedia, "style.css", "", ".png", true},
		{"css-bytes-png-name", objstore.KindMedia, "logo.png", "", ".css", true},
		// Binary-vs-binary mismatch: real magic bytes must match exactly.
		{"png-bytes-jpg-name", objstore.KindRasterImage, "photo.jpg", "image/jpeg", ".png", true},
		{"jpg-bytes-png-name", objstore.KindRasterImage, "photo.png", "image/png", ".jpg", true},
		{"pdf-bytes-image-name", objstore.KindMedia, "photo.png", "image/png", ".pdf", true},
		// Cross-family: text bytes declared as a binary format.
		{"html-bytes-png-name", objstore.KindMedia, "photo.png", "", ".html", true},
		// Within the heuristic text group (css/js/html/svg) the sniffers are
		// ambiguous (a CSS file starting with `/*!` reads as JS), so the kind
		// allowlist is the gate — a declared .js with HTML-sniffed bytes is
		// accepted as a code asset rather than false-rejected.
		{"html-bytes-js-name-text-ambiguous", objstore.KindAsset, "app.js", "", ".html", false},
		{"css-bytes-html-name-text-ambiguous", objstore.KindAsset, "page.html", "", ".css", false},
		// Content-type-only mismatch.
		{"png-bytes-css-mime", objstore.KindMedia, "blob", "text/css", ".png", true},
		// Genuine uploads must pass.
		{"png-ok-image", objstore.KindRasterImage, "photo.png", "image/png", ".png", false},
		{"jpeg-ok-image", objstore.KindRasterImage, "photo.jpg", "image/jpeg", ".jpg", false},
		{"gif-ok-image", objstore.KindRasterImage, "anim.gif", "image/gif", ".gif", false},
		{"svg-ok-asset", objstore.KindAsset, "logo.svg", "image/svg+xml", ".svg", false},
		{"css-ok-asset", objstore.KindAsset, "app.css", "text/css", ".css", false},
		{"js-ok-asset", objstore.KindAsset, "app.js", "text/javascript", ".js", false},
		{"html-ok-asset", objstore.KindAsset, "page.html", "text/html", ".html", false},
		{"woff2-ok-asset", objstore.KindAsset, "f.woff2", "font/woff2", ".woff2", false},
		{"pdf-ok-pdf", objstore.KindPDF, "doc.pdf", "application/pdf", ".pdf", false},
		// The media endpoint accepts the full allowlist, so a PDF/image/code
		// asset is all legitimate there.
		{"pdf-ok-media", objstore.KindMedia, "doc.pdf", "application/pdf", ".pdf", false},
		{"png-ok-media", objstore.KindMedia, "photo.png", "image/png", ".png", false},
		{"svg-ok-media", objstore.KindMedia, "logo.svg", "image/svg+xml", ".svg", false},
		// Unknown/ambiguous declarations are ignored (no false positives).
		{"png-octet-stream", objstore.KindRasterImage, "photo", "application/octet-stream", ".png", false},
		{"png-no-ext-no-mime", objstore.KindRasterImage, "blob", "", ".png", false},
		{"jpeg-jfif-name", objstore.KindRasterImage, "photo.jfif", "", ".jpg", false},
		{"jpeg-alias-name", objstore.KindRasterImage, "photo.jpeg", "image/jpeg", ".jpg", false},
		{"htm-alias-name", objstore.KindAsset, "page.htm", "text/html", ".html", false},
		{"svg-xml-mime", objstore.KindAsset, "logo.svg", "text/xml", ".svg", false},
		{"png-unknown-ext", objstore.KindRasterImage, "photo.bin", "", ".png", false},
		// An empty signature is always rejected.
		{"empty-signature", objstore.KindMedia, "x.png", "image/png", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			msg := magicByteMismatch(c.kind, c.filename, c.mime, c.sniffed)
			if c.wantErr && msg == "" {
				t.Fatalf("kind=%s file=%q mime=%q sniff=%q: want rejection, got none", c.kind, c.filename, c.mime, c.sniffed)
			}
			if !c.wantErr && msg != "" {
				t.Fatalf("kind=%s file=%q mime=%q sniff=%q: want accept, got %q", c.kind, c.filename, c.mime, c.sniffed, msg)
			}
		})
	}
}

// TestSniffAssetTypeAcceptsXmlPrologSvg covers the explicit SVG exception: SVG
// is text/XML with no binary magic bytes, so an SVG carrying an `<?xml ...?>`
// prolog and/or a leading comment must still sniff as `.svg` (issue #455).
func TestSniffAssetTypeAcceptsXmlPrologSvg(t *testing.T) {
	cases := map[string]string{
		"xml-prolog": "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>",
		"comment":    "<!-- exported -->\n<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>",
		"prolog+cmt": "<?xml version=\"1.0\"?><!-- c --><svg xmlns=\"http://www.w3.org/2000/svg\"/>",
		"doctype":    "<?xml version=\"1.0\"?><!DOCTYPE svg PUBLIC \"-//W3C//DTD SVG 1.1//EN\" \"http://www.w3.org/Graphics/SVG/1.1/DTD/svg11.dtd\"><svg></svg>",
		"whitespace": "\n\n  <?xml version=\"1.0\"?>\n  <svg></svg>",
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			ext, ok := sniffAssetType([]byte(doc))
			if !ok || ext != ".svg" {
				t.Fatalf("sniffAssetType(%s) = (%q,%v), want (.svg,true)", name, ext, ok)
			}
			// And it must pass the media-endpoint enforcement as a real SVG.
			if msg := magicByteMismatch(objstore.KindMedia, "logo.svg", "image/svg+xml", ext); msg != "" {
				t.Fatalf("legit svg rejected: %s", msg)
			}
		})
	}
}

// TestMagicByteMismatchRejectsHtmlBytesAsImage is the issue's literal
// acceptance case at the pure level: a `.png` containing HTML must be rejected.
func TestMagicByteMismatchRejectsHtmlBytesAsImage(t *testing.T) {
	html := []byte("<!doctype html><html><body><script>alert(1)</script></body></html>")
	ext, ok := sniffAssetType(html)
	if !ok || ext != ".html" {
		t.Fatalf("html fixture sniffed as (%q,%v), want (.html,true)", ext, ok)
	}
	if _, isImg := sniffImageType(html); isImg {
		t.Fatal("html bytes must not sniff as an image")
	}
	if msg := magicByteMismatch(objstore.KindRasterImage, "evil.png", "image/png", ext); msg == "" {
		t.Fatal("html bytes declared as a png must be rejected")
	}
	if msg := magicByteMismatch(objstore.KindMedia, "evil.png", "image/png", ext); msg == "" {
		t.Fatal("html bytes declared as a png must be rejected on the media endpoint")
	}
}

// ── HTTP-level coverage (integration; skipped without a test Postgres) ───────

// signatureUpload posts a multipart body to a REST upload path with a
// controllable declared filename + part content-type, returning status + body.
func signatureUpload(t *testing.T, h *RESTHandlers, rawKey, path, filename, partCT string, body []byte) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", `form-data; name="file"; filename="`+filename+`"`)
	if partCT != "" {
		hdr.Set("Content-Type", partCT)
	}
	part, err := mw.CreatePart(hdr)
	if err != nil {
		t.Fatalf("create part: %v", err)
	}
	if _, err := part.Write(body); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// TestUploadRejectsMismatchedSignature is the issue #455 acceptance test: on
// every upload path (image/media, asset, avatar) a file whose bytes do not
// match its declared type is rejected with 400, while a matching file uploads.
func TestUploadRejectsMismatchedSignature(t *testing.T) {
	ctx := context.Background()
	h, _, raw := uploadQuotaFixture(t)
	_ = ctx

	html := []byte("<!doctype html><html><body><script>alert(1)</script></body></html>")
	png := tinyPNG(t)
	css := []byte("body { color: red; }")

	t.Run("image: html bytes as .png rejected", func(t *testing.T) {
		code, body := signatureUpload(t, h, raw, "/v1/rest/uploads/image", "evil.png", "image/png", html)
		if code != http.StatusBadRequest {
			t.Fatalf("= %d, want 400 (body %s)", code, body)
		}
	})

	t.Run("image: png bytes named .css rejected", func(t *testing.T) {
		code, body := signatureUpload(t, h, raw, "/v1/rest/uploads/image", "style.css", "", png)
		if code != http.StatusBadRequest {
			t.Fatalf("= %d, want 400 (body %s)", code, body)
		}
	})

	t.Run("image: pdf bytes as .pdf accepted (pdf is on the media allowlist)", func(t *testing.T) {
		code, body := signatureUpload(t, h, raw, "/v1/rest/uploads/image", "doc.pdf", "application/pdf", []byte("%PDF-1.7\n1 0 obj\n"))
		if code != http.StatusOK {
			t.Fatalf("= %d, want 200 (body %s)", code, body)
		}
	})

	t.Run("image: png bytes declared .pdf rejected", func(t *testing.T) {
		code, body := signatureUpload(t, h, raw, "/v1/rest/uploads/image", "doc.pdf", "application/pdf", png)
		if code != http.StatusBadRequest {
			t.Fatalf("= %d, want 400 (body %s)", code, body)
		}
	})

	t.Run("image: matching png accepted", func(t *testing.T) {
		code, body := signatureUpload(t, h, raw, "/v1/rest/uploads/image", "ok.png", "image/png", png)
		if code != http.StatusOK {
			t.Fatalf("= %d, want 200 (body %s)", code, body)
		}
	})

	t.Run("image: xml-prolog svg accepted (explicit exception)", func(t *testing.T) {
		svg := []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>")
		code, body := signatureUpload(t, h, raw, "/v1/rest/uploads/image", "logo.svg", "image/svg+xml", svg)
		if code != http.StatusOK {
			t.Fatalf("= %d, want 200 (body %s)", code, body)
		}
	})

	t.Run("asset: png bytes as .css rejected", func(t *testing.T) {
		code, body := signatureUpload(t, h, raw, "/v1/rest/uploads/asset", "app.css", "text/css", png)
		if code != http.StatusBadRequest {
			t.Fatalf("= %d, want 400 (body %s)", code, body)
		}
	})

	t.Run("asset: png bytes as .js rejected (binary vs text)", func(t *testing.T) {
		code, body := signatureUpload(t, h, raw, "/v1/rest/uploads/asset", "app.js", "text/javascript", png)
		if code != http.StatusBadRequest {
			t.Fatalf("= %d, want 400 (body %s)", code, body)
		}
	})

	t.Run("asset: pdf bytes as .css rejected (binary vs text)", func(t *testing.T) {
		code, body := signatureUpload(t, h, raw, "/v1/rest/uploads/asset", "app.css", "text/css", []byte("%PDF-1.7\n..."))
		if code != http.StatusBadRequest {
			t.Fatalf("= %d, want 400 (body %s)", code, body)
		}
	})

	t.Run("asset: css with leading comment accepted (sniffer ambiguity)", func(t *testing.T) {
		// Regression: the sniffer reads a CSS `/*! license */` comment as JS.
		// The enforcement must not false-reject a real CSS upload.
		withComment := []byte("/*! JomForm license */\nbody { color: red; }")
		code, body := signatureUpload(t, h, raw, "/v1/rest/uploads/asset", "app.css", "text/css", withComment)
		if code != http.StatusOK {
			t.Fatalf("= %d, want 200 (body %s)", code, body)
		}
	})

	t.Run("asset: matching css accepted", func(t *testing.T) {
		code, body := signatureUpload(t, h, raw, "/v1/rest/uploads/asset", "app.css", "text/css", css)
		if code != http.StatusOK {
			t.Fatalf("= %d, want 200 (body %s)", code, body)
		}
	})

	t.Run("avatar: html bytes as .png rejected", func(t *testing.T) {
		code, body := signatureUpload(t, h, raw, "/v1/rest/uploads/avatar", "evil.png", "image/png", html)
		if code != http.StatusBadRequest {
			t.Fatalf("= %d, want 400 (body %s)", code, body)
		}
	})

	t.Run("avatar: pdf bytes as .png rejected", func(t *testing.T) {
		code, body := signatureUpload(t, h, raw, "/v1/rest/uploads/avatar", "evil.png", "image/png", []byte("%PDF-1.7\n..."))
		if code != http.StatusBadRequest {
			t.Fatalf("= %d, want 400 (body %s)", code, body)
		}
	})

	t.Run("avatar: matching png accepted", func(t *testing.T) {
		code, body := signatureUpload(t, h, raw, "/v1/rest/uploads/avatar", "me.png", "image/png", png)
		if code != http.StatusOK {
			t.Fatalf("= %d, want 200 (body %s)", code, body)
		}
	})

	t.Run("rejection body is a clear error", func(t *testing.T) {
		_, body := signatureUpload(t, h, raw, "/v1/rest/uploads/image", "evil.png", "image/png", html)
		if !strings.Contains(body, "error") {
			t.Fatalf("rejection body %q must carry an error field", body)
		}
	})
}
