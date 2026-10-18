package httpapi

// t_adc169da: a PDF whose bytes contain a `{` (0x7B) byte was sniffed as CSS
// and rejected with 400 on POST /v1/rest/uploads/image.
//
// A PDF is a binary container, and its compressed streams almost always carry
// a 0x7B byte — a Chrome "Print to PDF" of a one-line invoice does. The media
// upload sniffs image → ASSET → pdf (uploads.go, sniff order), and
// SniffAssetType classifies anything containing a brace as `.css`, so the
// `%PDF-` header never got a chance to speak: MagicByteMismatch then compared
// the declared `x.pdf` against the sniffed `.css` and rejected the ordinary
// document with "kandungan fail tidak sepadan dengan sambungan fail".
//
// These tests cover both halves of the acceptance:
//   - a PDF containing 0x7B uploads (200) and lands as `application/pdf`, and
//   - the signature guard keeps rejecting a `.css` that is really a PDF, an
//     HTML file declared as a `.pdf`, and keeps accepting real CSS.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/objstore"
)

// pdfFixture returns a structurally valid single-page PDF. When brace is true
// the page's content-stream string carries a `{` byte, which is the ordinary
// shape of every Chrome-printed invoice (and of most real PDFs).
func pdfFixture(t *testing.T, brace bool) []byte {
	t.Helper()
	text := "(INVOICE JF-1001)"
	if brace {
		text = "({INVOICE} JF-1001)"
	}
	stream := "BT /F1 12 Tf 20 50 Td " + text + " Tj ET\n"
	objects := []string{
		"<</Type/Catalog/Pages 2 0 R>>",
		"<</Type/Pages/Kids[3 0 R]/Count 1>>",
		"<</Type/Page/Parent 2 0 R/MediaBox[0 0 300 100]/Contents 4 0 R/Resources<</Font<</F1 5 0 R>>>>>>",
		fmt.Sprintf("<</Length %d>>stream\n%s\nendstream", len(stream), stream),
		"<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for i, body := range objects {
		offsets[i+1] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj%s endobj\n", i+1, body)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for i := 1; i <= len(objects); i++ {
		fmt.Fprintf(&b, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&b, "trailer<</Size %d/Root 1 0 R>>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return b.Bytes()
}

// TestPDFWithBraceSniffsAsPDF is the unit-level statement of the fix: the
// textual-asset heuristic must DECLINE a blob that starts with `%PDF-`, so the
// order the handler sniffs in cannot decide whether an ordinary PDF is a PDF.
func TestPDFWithBraceSniffsAsPDF(t *testing.T) {
	withBrace := pdfFixture(t, true)
	control := pdfFixture(t, false)

	if !bytes.HasPrefix(withBrace, []byte("%PDF-")) {
		t.Fatalf("fixture does not start with %%PDF-: %q", withBrace[:8])
	}
	if !bytes.Contains(withBrace, []byte{0x7B}) {
		t.Fatal("fixture must contain a 0x7B byte to reproduce the defect")
	}
	if bytes.Contains(control, []byte{0x7B}) {
		t.Fatal("control fixture must NOT contain a 0x7B byte")
	}

	for name, data := range map[string][]byte{"with-0x7B": withBrace, "control": control} {
		t.Run(name, func(t *testing.T) {
			if ext, ok := sniffImageType(data); ok {
				t.Fatalf("sniffImageType(pdf) = (%q,true); a PDF is not an image", ext)
			}
			// SniffAssetType runs BEFORE sniffPDFType in the handler, so it is
			// the one that decided this. It must not claim a binary container.
			ext, ok := sniffAssetType(data)
			if ok {
				t.Fatalf("sniffAssetType(pdf) = (%q,true), want (\"\",false): the textual "+
					"asset heuristic must not classify a %%PDF- blob", ext)
			}
			ext, ok = sniffPDFType(data)
			if !ok || ext != ".pdf" {
				t.Fatalf("sniffPDFType(pdf) = (%q,%v), want (.pdf,true)", ext, ok)
			}
			if msg := magicByteMismatch(objstore.KindMedia, "invoice.pdf", "application/pdf", ext); msg != "" {
				t.Fatalf("a real PDF declared as .pdf was rejected: %q", msg)
			}
		})
	}
}

// TestUploadPDFWithBraceByte is the acceptance test at the HTTP level: the
// real handler, the real sniff order, a real multipart body.
func TestUploadPDFWithBraceByte(t *testing.T) {
	h, _, raw := uploadQuotaFixture(t)
	withBrace := pdfFixture(t, true)

	// mediaRow returns the media-list row for the key an upload response names.
	mediaRow := func(t *testing.T, key string) map[string]any {
		t.Helper()
		code, resp := bundleGet(t, h, raw, "/v1/rest/media?limit=100")
		if code != http.StatusOK {
			t.Fatalf("GET /v1/rest/media = %d, body %v", code, resp)
		}
		items, _ := resp["items"].([]any)
		for _, it := range items {
			row, _ := it.(map[string]any)
			if k, _ := row["key"].(string); k == key {
				return row
			}
		}
		t.Fatalf("uploaded key %q has no row in GET /v1/rest/media (rows: %d)", key, len(items))
		return nil
	}

	t.Run("pdf containing 0x7B uploads as application/pdf", func(t *testing.T) {
		code, body := signatureUpload(t, h, raw, "/v1/rest/uploads/image", "invoice.pdf", "application/pdf", withBrace)
		if code != http.StatusOK {
			t.Fatalf("POST /v1/rest/uploads/image (PDF with 0x7B) = %d, want 200 (body %s)", code, body)
		}
		var resp struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			t.Fatalf("decode upload response %q: %v", body, err)
		}
		key := strings.TrimPrefix(resp.URL, fakeMediaPrefix)
		if key == "" || key == resp.URL {
			t.Fatalf("upload url %q is not a store key under %q", resp.URL, fakeMediaPrefix)
		}
		if !strings.HasSuffix(key, ".pdf") {
			t.Fatalf("stored key %q does not keep the .pdf extension", key)
		}
		// Stored byte-for-byte under application/pdf — not re-encoded, not
		// stored as text/css.
		stored, ct, err := h.Store.GetObject(t.Context(), key)
		if err != nil {
			t.Fatalf("stored object %q unreadable: %v", key, err)
		}
		if ct != "application/pdf" {
			t.Fatalf("stored content-type = %q, want application/pdf", ct)
		}
		if !bytes.Equal(stored, withBrace) {
			t.Fatalf("stored bytes differ from the uploaded PDF (%d vs %d bytes)", len(stored), len(withBrace))
		}
		row := mediaRow(t, key)
		if mime, _ := row["mime"].(string); mime != "application/pdf" {
			t.Fatalf("media row mime = %q, want application/pdf (row %v)", mime, row)
		}
	})

	t.Run("control: pdf without 0x7B still uploads", func(t *testing.T) {
		code, body := signatureUpload(t, h, raw, "/v1/rest/uploads/image", "plain.pdf", "application/pdf", pdfFixture(t, false))
		if code != http.StatusOK {
			t.Fatalf("POST /v1/rest/uploads/image (PDF without 0x7B) = %d, want 200 (body %s)", code, body)
		}
	})

	t.Run("real css still uploads as text/css", func(t *testing.T) {
		css := []byte(".card { color: #111; }\n")
		code, body := signatureUpload(t, h, raw, "/v1/rest/uploads/image", "theme.css", "text/css", css)
		if code != http.StatusOK {
			t.Fatalf("POST /v1/rest/uploads/image (css) = %d, want 200 (body %s)", code, body)
		}
		var resp struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			t.Fatalf("decode upload response %q: %v", body, err)
		}
		row := mediaRow(t, strings.TrimPrefix(resp.URL, fakeMediaPrefix))
		if mime, _ := row["mime"].(string); !strings.HasPrefix(mime, "text/css") {
			t.Fatalf("media row mime = %q, want text/css", mime)
		}
	})

	t.Run("evil.css containing %PDF- still rejected", func(t *testing.T) {
		code, body := signatureUpload(t, h, raw, "/v1/rest/uploads/image", "evil.css", "text/css", withBrace)
		if code != http.StatusBadRequest {
			t.Fatalf("POST /v1/rest/uploads/image (PDF bytes as .css) = %d, want 400 (body %s)", code, body)
		}
		if !strings.Contains(body, "error") {
			t.Fatalf("rejection body %q must carry an error field", body)
		}
	})

	t.Run("pdf that is really html still rejected", func(t *testing.T) {
		html := []byte("<!doctype html><html><body><script>alert(1)</script></body></html>")
		code, body := signatureUpload(t, h, raw, "/v1/rest/uploads/image", "doc.pdf", "application/pdf", html)
		if code != http.StatusBadRequest {
			t.Fatalf("POST /v1/rest/uploads/image (html bytes as .pdf) = %d, want 400 (body %s)", code, body)
		}
	})
}
