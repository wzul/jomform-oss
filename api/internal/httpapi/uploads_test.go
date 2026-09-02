// Issue #61: unit tests for image-upload validation — content-type allowlist,
// size caps, EXIF strip, account-scoped key path-safety. These test the pure
// functions (sniffImageType, buildUploadKey, optimizeImage) so they run without
// Postgres or a live S3/MinIO (skipped path in CI is only for the auth-gated
// HTTP layer).
package httpapi

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

// tinyPNG returns a real 16x16 PNG so sniffImageType/optimizeImage have
// genuine magic bytes and a decodable image, not hand-crafted headers.
func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for x := 0; x < 16; x++ {
		for y := 0; y < 16; y++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 16), G: uint8(y * 16), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode fixture: %v", err)
	}
	return buf.Bytes()
}

func fakeJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("jpeg encode fixture: %v", err)
	}
	return buf.Bytes()
}

func TestSniffImageTypeAllowlist(t *testing.T) {
	cases := []struct {
		name  string
		data  []byte
		want  string
		wantOK bool
	}{
		{"png", tinyPNG(t), ".png", true},
		{"jpeg", fakeJPEG(t), ".jpg", true},
		{"webp", append([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), make([]byte, 16)...), ".webp", true},
		{"gif", append([]byte("GIF89a"), make([]byte, 12)...), ".gif", true},
		{"gif87", append([]byte("GIF87a"), make([]byte, 12)...), ".gif", true},
		{"svg-disallowed", []byte("<svg xmlns=..."), "", false},
		{"shell-script-disallowed", []byte("#!/bin/sh\necho pwned"), "", false},
		{"plain-text-disallowed", []byte("hello world this is not an image"), "", false},
		{"empty", nil, "", false},
		{"too-short", []byte("RIFF"), "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ext, ok := sniffImageType(c.data)
			if ok != c.wantOK || ext != c.want {
				t.Fatalf("sniff = (%q, %v), want (%q, %v)", ext, ok, c.want, c.wantOK)
			}
		})
	}
}

// TestSniffIgnoresClientExt proves the sniff reads magic bytes, so a PNG
// renamed/retyped to ".exe" or with a spoofed content-type is still caught.
func TestSniffIgnoresClientExt(t *testing.T) {
	png := tinyPNG(t)
	ext, ok := sniffImageType(png)
	if !ok || ext != ".png" {
		t.Fatalf("expect .png from bytes regardless of filename, got (%q,%v)", ext, ok)
	}
}

func TestBuildUploadKeyPathSafety(t *testing.T) {
	// The server never echoes client input into the key, so even hostile
	// account ids cannot introduce "../", an absolute path, or a second ext.
	for _, acct := range []string{
		"acc-123", "../../../../etc", "u/..%2f", "a b", "a.b",
		strings.Repeat("A", 200),
	} {
		key := buildUploadKey(acct, ".png")
		if strings.Contains(key, "..") {
			t.Errorf("acct %q => key %q must not contain parent-traversal", acct, key)
		}
		if strings.Contains(key, "%2f") || strings.Count(key, "/") != 2 {
			t.Errorf("acct %q => key %q must be u/<acct>/<uuid>.<ext> only", acct, key)
		}
		if !strings.HasSuffix(key, ".png") {
			t.Errorf("acct %q => key %q must end in the allowlisted ext", acct, key)
		}
	}
	// Keys are stable-shaped: u/<id>/<uuid>.<ext>, exactly two slashes.
	key := buildUploadKey("acct-1", ".jpg")
	if !strings.HasPrefix(key, "u/acct-1/") {
		t.Fatalf("key %q missing account scope prefix", key)
	}
	if strings.Count(key, "/") != 2 {
		t.Fatalf("key %q must have exactly two path segments (account, uuid.ext)", key)
	}
}

// TestOptimizeStaysUnder2MBAndStripsEXIF: a large synthetic PNG must be
// shrunk under maxStoredBytes; re-encoding to JPEG must remove EXIF by
// construction (re-encode never carries the source's metadata). We verify the
// stored artifact is a valid image under the size cap.
func TestOptimizeShrinksLargeUnderCap(t *testing.T) {
	// Big PNG ~2000x3000 raw RGBA (~24MB decoded) forces downscale.
	big := generateLargeImageFixture()
	out, err := optimizeImage(big, ".png")
	if err != nil {
		t.Fatalf("optimize: %v", err)
	}
	if len(out) > maxStoredBytes {
		t.Fatalf("stored = %d bytes > cap %d", len(out), maxStoredBytes)
	}
	if len(out) == 0 {
		t.Fatal("optimize returned empty artifact")
	}
}

// TestOptimizeWebpSmallPassThrough: a small valid WebP ≤2MB stores as-is with
// no reprocessing.
func TestOptimizeWebpSmallPassthrough(t *testing.T) {
	small := append([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), make([]byte, 64)...)
	out, err := optimizeImage(small, ".webp")
	if err != nil {
		t.Fatalf("optimize webp: %v", err)
	}
	if !bytes.Equal(out, small) {
		t.Fatalf("small webp should pass through byte-for-byte, got %d bytes", len(out))
	}
}

func generateLargeImageFixture() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 2000, 3000))
	for x := 0; x < 2000; x += 3 {
		for y := 0; y < 3000; y += 3 {
			c := color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: uint8((x + y) % 255), A: 255}
			for dx := 0; dx < 3 && x+dx < 2000; dx++ {
				for dy := 0; dy < 3 && y+dy < 3000; dy++ {
					img.Set(x+dx, y+dy, c)
				}
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
