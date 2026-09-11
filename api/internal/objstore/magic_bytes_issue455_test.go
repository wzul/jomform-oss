package objstore

// Issue #455: magic-byte (file signature) enforcement — package-level unit
// coverage for the enforcement helper. These run without a database or S3.

import (
	"strings"
	"testing"
)

func TestMagicByteMismatch(t *testing.T) {
	cases := []struct {
		name     string
		kind     UploadKind
		filename string
		mime     string
		sniffed  string
		wantErr  bool
	}{
		{"html-as-png", KindRasterImage, "evil.png", "image/png", ".html", true},
		{"pdf-as-png", KindRasterImage, "evil.png", "image/png", ".pdf", true},
		{"png-as-css", KindAsset, "app.css", "text/css", ".png", true},
		{"png-as-pdf", KindPDF, "doc.pdf", "application/pdf", ".png", true},
		{"png-vs-jpg", KindRasterImage, "photo.jpg", "image/jpeg", ".png", true},
		{"empty", KindMedia, "x.png", "image/png", "", true},
		{"png-ok", KindRasterImage, "photo.png", "image/png", ".png", false},
		{"jpeg-ok", KindRasterImage, "photo.jpg", "image/jpeg", ".jpg", false},
		{"svg-ok", KindAsset, "logo.svg", "image/svg+xml", ".svg", false},
		{"pdf-ok", KindPDF, "doc.pdf", "application/pdf", ".pdf", false},
		{"media-pdf-ok", KindMedia, "doc.pdf", "application/pdf", ".pdf", false},
		{"octet-stream-ignored", KindRasterImage, "photo", "application/octet-stream", ".png", false},
		{"no-declared-ok", KindRasterImage, "", "", ".png", false},
		{"jpeg-alias", KindRasterImage, "photo.jpeg", "image/jpeg", ".jpg", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			msg := MagicByteMismatch(c.kind, c.filename, c.mime, c.sniffed)
			if c.wantErr != (msg != "") {
				t.Fatalf("MagicByteMismatch(%s,%q,%q,%q) = %q, wantErr=%v", c.kind, c.filename, c.mime, c.sniffed, msg, c.wantErr)
			}
		})
	}
}

// TestMagicByteMismatchMessagesAreSafe: rejection messages must be
// user-facing text with no internal identifiers leaked.
func TestMagicByteMismatchMessagesAreSafe(t *testing.T) {
	msg := MagicByteMismatch(KindRasterImage, "evil.png", "image/png", ".html")
	if msg == "" {
		t.Fatal("expected a rejection message")
	}
	for _, leak := range []string{"objstore", "KindRasterImage", "httpapi", "panic"} {
		if strings.Contains(msg, leak) {
			t.Fatalf("rejection message %q leaks internal identifier %q", msg, leak)
		}
	}
}
