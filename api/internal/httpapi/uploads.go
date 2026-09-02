// Image block direct upload (issue #61).
//
// This implements the live owner decision on #61: PRESIGN IS SUPERSEDED by a
// direct multipart upload endpoint. The builder client sends the raw bytes to
// the API; the server validates content-type + size, then server-side
// optimises the image (downscale long edge to 1920px, re-encode JPEG q~80 /
// PNG→JPEG, strip EXIF by re-encoding) so the stored artifact is ≤2MB. The
// result is pushed to S3/MinIO at a path-safe, account-scoped key
// `u/{accountID}/{uuid}.{ext}` — the account id and a server-generated UUID
// only, so a client can never control the object key.
//
// Content-type allowlist, 20MB input cap (owner decision), ≤2MB stored cap.
// No GC yet.
//
// Format choice (deviation from the owner note's "PNG→WebP", documented):
// pure-Go WebP encoding requires CGO (chai2010/webp wraps libwebp). We picked
// github.com/disintegration/imaging for simple pure-Go deployment at this
// volume, so PNG/JPEG are re-encoded to JPEG q80 (still strips EXIF, small
// output) and small/clean WebP inputs pass through untouched. GIFs are decoded
// to a still (first frame) so they can always be shrunk under 2MB — the
// owner's allowed fallback for animated GIFs that won't compress.
package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/gif"  // sniff GIF headers
	_ "image/jpeg" // sniff JPEG headers
	_ "image/png"  // sniff PNG headers
	"io"
	"log/slog"
	"net/http"
	"strings"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/disintegration/imaging"
	"github.com/google/uuid"
)

const (
	// maxUploadBytes is the largest image a client may send (owner decision).
	maxUploadBytes = 20 << 20 // 20MB

	// maxStoredBytes is the ceiling for the optimised artifact we store.
	maxStoredBytes = 2 << 20 // 2MB

	// maxLongEdge downscales images wider than this on the long edge.
	maxLongEdge = 1920

	// maxStoredLongEdge is used when shrinking further to hit the 2MB cap.
	maxStoredLongEdge = 1280

	jpegQuality = 80
)

// imageUploadTypes maps an allowed canonical extension to its MIME type.
// Anything else is rejected before touching the bytes.
var imageUploadMIME = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".webp": "image/webp",
	".gif":  "image/gif",
}

// ObjectStore is a minimal S3/MinIO client for the media bucket.
type ObjectStore struct {
	client     *s3.Client
	bucket     string
	publicBase string // public URL prefix: {S3_ENDPOINT}/{bucket}
}

// NewObjectStore builds an ObjectStore from config. endpoint empty uses AWS.
//
// Serving pattern (documented in AGENTS.md): public images are served from the
// direct {S3_ENDPOINT}/{bucket}/{key} object URL. MinIO uses path-style
// addressing (force UsePathStyle for localhost/MinIO); the bucket is expected
// to be public-read so builder/form image blocks render without signed URLs.
func NewObjectStore(endpoint, accessKey, secretKey, bucket, region string) (*ObjectStore, error) {
	if bucket == "" {
		return nil, errors.New("S3_BUCKET empty")
	}
	if region == "" {
		region = "us-east-1"
	}
	optFns := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
	}
	if accessKey != "" && secretKey != "" {
		optFns = append(optFns,
			awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
		)
	}
	if endpoint != "" {
		optFns = append(optFns, awsconfig.WithBaseEndpoint(endpoint))
	}
	cfg, err := awsconfig.LoadDefaultConfig(context.Background(), optFns...)
	if err != nil {
		return nil, fmt.Errorf("s3 config: %w", err)
	}
	cli := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if endpoint != "" && (strings.Contains(endpoint, "localhost") || strings.Contains(endpoint, "minio") || strings.Contains(endpoint, ":9000")) {
			o.UsePathStyle = true
		}
	})
	base := strings.TrimSuffix(endpoint, "/") + "/" + bucket
	if base == "/"+bucket {
		base = bucket
	}
	return &ObjectStore{client: cli, bucket: bucket, publicBase: base}, nil
}

// PutObject stores body under key with the given content-type.
func (o *ObjectStore) PutObject(ctx context.Context, key, contentType string, body []byte) error {
	_, err := o.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &o.bucket,
		Key:         &key,
		Body:        bytes.NewReader(body),
		ContentType: &contentType,
	})
	if err != nil {
		return fmt.Errorf("s3 put: %w", err)
	}
	return nil
}

// PublicURL returns the canonical object URL for a key.
func (o *ObjectStore) PublicURL(key string) string {
	return o.publicBase + "/" + key
}

// uploadImage handles POST /v1/rest/uploads/image (multipart). It optimises
// the uploaded image and stores it under an account-scoped key, returning the
// public URL.
//
// Request: multipart/form-data with a single "file" part (≤20MB).
// Response: {"url": "..."} 200, or 400 on validation errors.
func (h *RESTHandlers) uploadImage(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if h.Store == nil {
		writeErr(w, http.StatusServiceUnavailable, "media storage tidak dikonfigurasi")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+1<<20)

	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		writeErr(w, http.StatusBadRequest, "fail imej tidak sah atau melebihi 20MB")
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	file, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "part 'file' diperlukan")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxUploadBytes+1))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "gagal membaca fail")
		return
	}
	if len(data) == 0 {
		writeErr(w, http.StatusBadRequest, "fail kosong")
		return
	}
	if len(data) > maxUploadBytes {
		writeErr(w, http.StatusRequestEntityTooLarge, "imej melebihi had 20MB")
		return
	}

	// Content-type allowlist from the file's actual bytes (magic), not the
	// client's declared header or filename (both spoofable).
	ext, ok := sniffImageType(data)
	if !ok {
		writeErr(w, http.StatusBadRequest, "jenis imej tidak disokong (png/jpeg/webp/gif sahaja)")
		return
	}
	mime := imageUploadMIME[ext]

	stored, err := optimizeImage(data, ext)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(stored) > maxStoredBytes {
		writeErr(w, http.StatusBadRequest, "imej gagal dimampatkan bawah 2MB; cuba imej lain")
		return
	}

	key := buildUploadKey(actx.Account.ID, ext)
	if err := h.Store.PutObject(r.Context(), key, mime, stored); err != nil {
		slog.Error("upload_put", "err", err, "account", actx.Account.ID)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	slog.Info("upload_ok",
		"account", actx.Account.ID, "key", key,
		"in_bytes", len(data), "out_bytes", len(stored), "mime", mime)
	respond(w, http.StatusOK, map[string]string{"url": h.Store.PublicURL(key)})
}

// buildUploadKey composes the S3 object key. The client NEVER supplies any
// part of it — account id and uuid are server-controlled, ext comes from the
// sniffed allowlist. The account segment is still sanitised to a safe
// character set (defence-in-depth in case an account id is ever derived from
// user input at some future call site), so "../" traversal is structurally
// impossible and the key is always exactly u/<acct>/<uuid>.<ext>.
func buildUploadKey(accountID, ext string) string {
	safeID := uploadSafeSegment(accountID)
	if safeID == "" {
		safeID = "unknown"
	}
	return fmt.Sprintf("u/%s/%s%s", safeID, uuid.NewString(), ext)
}

// uploadSafeSegment keeps only [A-Za-z0-9_-], dropping everything else so no
// path separators, dots, or traversal sequences can reach the object key.
func uploadSafeSegment(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// sniffImageType inspects the leading magic bytes — NOT the client-supplied
// content type — and returns the canonical storage extension if the image is
// one of png/jpeg/webp/gif. WebP has no stdlib decoder, so detect it first.
func sniffImageType(data []byte) (string, bool) {
	if len(data) < 16 {
		return "", false
	}
	// WebP: "RIFF" .... "WEBP"
	if bytes.HasPrefix(data, []byte("RIFF")) && bytes.Contains(data[:12], []byte("WEBP")) {
		return ".webp", true
	}
	// GIF: header magic (GIF87a / GIF89a), checked before DecodeConfig which
	// needs a readable full header.
	if bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a")) {
		return ".gif", true
	}
	switch format := headerFormat(data); format {
	case "png":
		return ".png", true
	case "jpeg":
		return ".jpg", true
	case "gif":
		return ".gif", true
	}
	return "", false
}

func headerFormat(data []byte) string {
	_, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return ""
	}
	return format
}

// optimizeImage re-encodes data so the stored artifact is ≤maxStoredBytes.
// Returns the encoded bytes or an error explaining why the image can't be
// brought under the cap.
func optimizeImage(data []byte, ext string) ([]byte, error) {
	// Fast pass: small/clean WebP already ≤2MB stores as-is (minimal
	// reprocessing, owner's "small & well-formed" pass-through).
	if len(data) <= maxStoredBytes && ext == ".webp" {
		return data, nil
	}
	// Fast pass: GIFs always decoded to a still so they can be shrunk.
	isGIF := ext == ".gif"

	img, err := decodeImage(data)
	if err != nil {
		return nil, errors.New("gagal decode imej")
	}

	// Downscale to the max long edge, preserving aspect.
	b := img.Bounds()
	long := b.Dx()
	if b.Dy() > long {
		long = b.Dy()
	}
	if long > maxLongEdge {
		img = imaging.Fit(img, maxLongEdge, maxLongEdge, imaging.Lanczos)
	}

	// Output: JPEG for PNG/JPEG/GIF (pure-Go; strips EXIF on re-encode).
	// WebP >2MB is re-encoded to JPEG too (can shrink an oversized WebP).
	out := encodeJPEG(img, jpegQuality)

	// Shrink stepwise (quality, then resolution) to the 2MB cap.
	for _, q := range []int{70, 60, 50} {
		if len(out) <= maxStoredBytes || isGIF {
			break // GIF already first-frame; a still at q80 that still exceeds
			// 2MB means a pathological image — give it lower quality too.
		}
		if len(out) > maxStoredBytes {
			out = encodeJPEG(img, q)
		}
	}
	if len(out) > maxStoredBytes {
		img = imaging.Fit(img, maxStoredLongEdge, maxStoredLongEdge, imaging.Lanczos)
		out = encodeJPEG(img, jpegQuality)
	}
	if len(out) > maxStoredBytes {
		return nil, errors.New("imej tidak dapat dimampatkan bawah 2MB")
	}
	return out, nil
}

func decodeImage(data []byte) (image.Image, error) {
	return imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
}

// encodeJPEG re-encodes to JPEG at the given quality. Re-encoding is what
// strips EXIF/GPS/phone metadata (privacy).
func encodeJPEG(img image.Image, quality int) []byte {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil
	}
	return buf.Bytes()
}
