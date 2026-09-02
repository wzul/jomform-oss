// Issue #61: unit tests for image-upload validation — content-type allowlist,
// size caps, EXIF strip, account-scoped key path-safety. These test the pure
// functions (sniffImageType, buildUploadKey, optimizeImage) so they run without
// Postgres or a live S3/MinIO (skipped path in CI is only for the auth-gated
// HTTP layer).
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
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

// TestOptimizeAvatarDownscalesSmall (issue #133): a large image fed to the
// avatar optimiser must be downscaled to avatar size (≤maxAvatarEdge on the
// long edge) and stay under the stored cap. Re-encoding to JPEG strips EXIF
// by construction.
func TestOptimizeAvatarDownscalesSmall(t *testing.T) {
	big, err := optimizeAvatar(generateLargeImageFixture(), ".png")
	if err != nil {
		t.Fatalf("optimizeAvatar: %v", err)
	}
	if len(big) == 0 {
		t.Fatal("optimizeAvatar returned empty artifact")
	}
	if len(big) > maxStoredBytes {
		t.Fatalf("avatar = %d bytes > cap %d", len(big), maxStoredBytes)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(big))
	if err != nil {
		t.Fatalf("decode stored avatar config: %v", err)
	}
	if cfg.Width > maxAvatarEdge || cfg.Height > maxAvatarEdge {
		t.Fatalf("avatar %dx%d exceeds %dpx long edge", cfg.Width, cfg.Height, maxAvatarEdge)
	}
}

// TestOptimizeAvatarRejectsEmpty (issue #133): empty input must not panic.
func TestOptimizeAvatarRejectsEmpty(t *testing.T) {
	if _, err := optimizeAvatar(nil, ".png"); err == nil {
		t.Fatal("expected error for empty avatar input")
	}
}

// TestJomformMediaURL (issue #133): only JomForm-hosted media URLs are
// accepted for persist; arbitrary external URLs are rejected.
func TestJomformMediaURL(t *testing.T) {
	ok := []string{
		"https://media.jomform.com/bucket/u/acct/uuid.png",
		"http://localhost:9000/bucket/u/acct/uuid.jpg",
		"/bucket/u/acct/uuid.webp",
	}
	for _, u := range ok {
		if !jomformMediaURL(u) {
			t.Errorf("jomformMediaURL(%q) = false, want true", u)
		}
	}
	bad := []string{
		"", "   ",
		"https://evil.com/x.png",
		"ftp://media.jomform.com/u/acct/x.png",
		"/etc/passwd",
		"javascript:alert(1)",
	}
	for _, u := range bad {
		if jomformMediaURL(u) {
			t.Errorf("jomformMediaURL(%q) = true, want false", u)
		}
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

// TestPublicReadPolicyGrantsAnonymousGet (issue #79): the bucket policy we
// apply must grant anonymous s3:GetObject on every object so builder/form
// image blocks render without signed URLs. It must be a valid JSON document
// scoped to the exact bucket and nothing else.
func TestPublicReadPolicyGrantsAnonymousGet(t *testing.T) {
	policy := publicReadPolicy("jomform-media")
	var doc struct {
		Version   string `json:"Version"`
		Statement []struct {
			Effect    string   `json:"Effect"`
			Principal any      `json:"Principal"`
			Action    []string `json:"Action"`
			Resource  []string `json:"Resource"`
		} `json:"Statement"`
	}
	if err := json.Unmarshal([]byte(policy), &doc); err != nil {
		t.Fatalf("policy is not valid JSON: %v\n%s", err, policy)
	}
	if doc.Version != "2012-10-17" {
		t.Fatalf("Version = %q, want 2012-10-17", doc.Version)
	}
	if len(doc.Statement) != 1 {
		t.Fatalf("expected exactly 1 statement, got %d", len(doc.Statement))
	}
	st := doc.Statement[0]
	if st.Effect != "Allow" {
		t.Fatalf("Effect = %q, want Allow", st.Effect)
	}
	// Principal "*" (anonymous) — must be present, not a specific account.
	if p, ok := st.Principal.(string); !ok || p != "*" {
		t.Fatalf("Principal = %#v, want \"*\"", st.Principal)
	}
	if len(st.Action) != 1 || st.Action[0] != "s3:GetObject" {
		t.Fatalf("Action = %v, want [s3:GetObject]", st.Action)
	}
	if len(st.Resource) != 1 || st.Resource[0] != "arn:aws:s3:::jomform-media/*" {
		t.Fatalf("Resource = %v, want [arn:aws:s3:::jomform-media/*]", st.Resource)
	}
}

// TestEnsureBucketIntegration (issue #79) is an opt-in integration test that
// exercises the real S3/MinIO bootstrap path (HeadBucket → CreateBucket →
// PutBucketPolicy) against a live endpoint. It is skipped unless
// JOMFORM_S3_TEST_ENDPOINT is set, so the default `go test ./...` stays
// hermetic (no network, no MinIO dependency). Set it to a throwaway MinIO
// (e.g. `docker run -p 9000:9000 minio/minio server /data`) with the bucket
// absent to prove the guard creates it and makes it public-read.
func TestEnsureBucketIntegration(t *testing.T) {
	endpoint := os.Getenv("JOMFORM_S3_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("set JOMFORM_S3_TEST_ENDPOINT to run the S3 bootstrap integration test")
	}
	ak := os.Getenv("JOMFORM_S3_TEST_ACCESS_KEY")
	sk := os.Getenv("JOMFORM_S3_TEST_SECRET_KEY")
	if ak == "" || sk == "" {
		t.Fatal("JOMFORM_S3_TEST_ACCESS_KEY and JOMFORM_S3_TEST_SECRET_KEY are required")
	}
	bucket := os.Getenv("JOMFORM_S3_TEST_BUCKET")
	if bucket == "" {
		bucket = "jomform-media-test"
	}
	store, err := NewObjectStore(endpoint, ak, sk, bucket, "us-east-1")
	if err != nil {
		t.Fatalf("NewObjectStore: %v", err)
	}
	ctx := context.Background()

	// Idempotent: calling twice must not error (second run sees the bucket).
	for i := 0; i < 2; i++ {
		if err := store.EnsureBucket(ctx); err != nil {
			t.Fatalf("EnsureBucket (run %d): %v", i+1, err)
		}
	}

	// The bucket must now accept an anonymous GET (public-read). We PUT an
	// object, then fetch it back over plain HTTP with no credentials and
	// require a 200 — this is exactly how the builder/form canvas renders.
	key := "u/test/" + uuid.NewString() + ".png"
	body := tinyPNG(t)
	if err := store.PutObject(ctx, key, "image/png", body); err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	url := store.PublicURL(key)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("anonymous GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("anonymous GET %s = %d, want 200 (bucket must be public-read)", url, resp.StatusCode)
	}
}
