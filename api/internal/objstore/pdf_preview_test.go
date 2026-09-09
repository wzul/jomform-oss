package objstore

import (
	"bytes"
	"testing"
)

// TestRenderPDFPreview verifies the issue #383 Ghostscript render produces a
// JPEG preview from a real PDF's first page. Skipped when gs is unavailable.
func TestRenderPDFPreview(t *testing.T) {
	// Minimal single-page PDF (A4, "Hello" text).
	pdf := []byte("%PDF-1.4\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 612 792]/Contents 4 0 R/Resources<</Font<</F1 5 0 R>>>>>>endobj\n4 0 obj<</Length 44>>stream\nBT /F1 24 Tf 72 720 Td (Hello) Tj ET\nendstream\nendobj\n5 0 obj<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>endobj\ntrailer<</Root 1 0 R>>\n%%EOF\n")
	preview, err := RenderPDFPreview(pdf)
	if err != nil {
		t.Fatalf("RenderPDFPreview: %v", err)
	}
	if len(preview) == 0 {
		t.Skip("gs unavailable — preview skipped (best-effort)")
	}
	if !bytes.HasPrefix(preview, []byte{0xFF, 0xD8}) {
		t.Fatalf("expected JPEG magic, got %x", preview[:2])
	}
	if len(preview) > MaxStoredBytes {
		t.Fatalf("preview too large: %d bytes", len(preview))
	}
}

// TestBuildPDFPreviewKey verifies the preview key derivation.
func TestBuildPDFPreviewKey(t *testing.T) {
	got := BuildPDFPreviewKey("u/acct/uuid.pdf")
	if got != "u/acct/uuid-preview.jpg" {
		t.Fatalf("got %q", got)
	}
	if BuildPDFPreviewKey("not-a-key") != "" {
		t.Fatal("expected empty for non-canonical key")
	}
}
