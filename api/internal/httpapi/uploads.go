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
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/disintegration/imaging"
	"github.com/google/uuid"

	"github.com/wzul/jomform/api/internal/dbstore"
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
	publicBase string // browser-reachable public URL prefix (issue #145)
}

// NewObjectStore builds an ObjectStore from config. endpoint empty uses AWS.
//
// Serving pattern (issue #145): public images are served through the API's
// public media route (GET /media/{key}), which streams the object from
// MinIO/S3. publicBase is the browser-reachable base for that route (e.g.
// https://jomform.com/media) — it must NOT be the internal {S3_ENDPOINT}/
// {bucket} URL, which browsers cannot resolve. MinIO uses path-style
// addressing (force UsePathStyle for localhost/MinIO).
func NewObjectStore(endpoint, accessKey, secretKey, bucket, region, publicBase string) (*ObjectStore, error) {
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
	base := strings.TrimSuffix(publicBase, "/")
	if base == "" {
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

// PublicURL returns the canonical browser-reachable URL for a key. It points
// at the API's public media route (issue #145), never the internal
// S3/MinIO endpoint, so browsers can actually load the object.
func (o *ObjectStore) PublicURL(key string) string {
	return o.publicBase + "/" + key
}

// GetObject streams an object's bytes and content-type from the bucket. It is
// used by the public media route (GET /media/{key}) to serve uploaded media to
// browsers without exposing the internal S3/MinIO endpoint.
func (o *ObjectStore) GetObject(ctx context.Context, key string) ([]byte, string, error) {
	out, err := o.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &o.bucket,
		Key:    &key,
	})
	if err != nil {
		return nil, "", fmt.Errorf("s3 get %q: %w", key, err)
	}
	defer out.Body.Close()
	body, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, "", fmt.Errorf("s3 read %q: %w", key, err)
	}
	ct := "application/octet-stream"
	if out.ContentType != nil && *out.ContentType != "" {
		ct = *out.ContentType
	}
	return body, ct, nil
}

// DeleteObject removes an object from the bucket (issue #180). It is used by
// the media delete endpoint after the row is soft-deleted. A missing object is
// not an error (idempotent delete).
func (o *ObjectStore) DeleteObject(ctx context.Context, key string) error {
	_, err := o.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &o.bucket,
		Key:    &key,
	})
	if err != nil {
		return fmt.Errorf("s3 delete %q: %w", key, err)
	}
	return nil
}

// mediaKeyFromPath validates and extracts the object key from a public media
// route path. Uploaded keys are always exactly `u/{account}/{uuid}.{ext}`
// (see buildUploadKey), so the key must be exactly three segments starting
// with `u/`, with no traversal segments (".", ".."), no backslashes, no
// percent-encoding, and no empty segments. Returns "" when the path is not a
// safe key.
func mediaKeyFromPath(p string) string {
	p = strings.TrimPrefix(p, "/")
	if p == "" || strings.Contains(p, "\\") || strings.Contains(p, "%") {
		return ""
	}
	segs := strings.Split(p, "/")
	if len(segs) != 3 || segs[0] != "u" {
		return ""
	}
	for _, seg := range segs {
		if seg == "" || seg == "." || seg == ".." {
			return ""
		}
	}
	return p
}

// handleMedia serves GET /media/{key} — a public, unauthenticated route that
// streams an uploaded object from the media bucket (issue #145). The key is
// validated to a safe `u/...` path (no traversal), the object's stored
// content-type is preserved, and a short cache header is set so browsers and
// the edge don't re-fetch on every render. 404 when the object is missing.
func (h *RESTHandlers) handleMedia(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		writeErr(w, http.StatusServiceUnavailable, "media storage tidak dikonfigurasi")
		return
	}
	key := mediaKeyFromPath(r.PathValue("key"))
	if key == "" {
		writeErr(w, http.StatusBadRequest, "kunci media tidak sah")
		return
	}
	body, ct, err := h.Store.GetObject(r.Context(), key)
	if err != nil {
		// Distinguish a missing object (404) from a storage failure (500).
		var nf *types.NoSuchKey
		if errors.As(err, &nf) {
			writeErr(w, http.StatusNotFound, "media tidak dijumpai")
			return
		}
		slog.Error("media_get", "err", err, "key", key)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// EnsureBucket provisions the media bucket if it does not exist and makes it
// public-read (issue #79). It is idempotent: on a fresh MinIO/S3 the bucket is
// created and a public-read policy applied; on subsequent boots the bucket
// already exists and the call is a no-op. This is the durable in-repo guard so
// a rebuild/redeploy can never silently regress into the NoSuchBucket 500 that
// broke builder image uploads.
//
// Serving pattern (issue #145): public images are served through the API's
// public media route (GET /media/{key}), which streams the object from the
// bucket. The bucket is still made public-read so the route (and any direct
// object URL) can serve without signed URLs.
func (o *ObjectStore) EnsureBucket(ctx context.Context) error {
	// HeadBucket first: if it exists (and we can access it) there is nothing
	// to do. A missing bucket surfaces as a typed NotFound / NoSuchBucket.
	_, err := o.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &o.bucket})
	if err == nil {
		return nil
	}
	var notFound *types.NotFound
	var noSuchBucket *types.NoSuchBucket
	if !errors.As(err, &notFound) && !errors.As(err, &noSuchBucket) {
		// Some other failure (auth, network, endpoint down) — do not mask it.
		return fmt.Errorf("s3 head bucket %q: %w", o.bucket, err)
	}

	// Bucket absent: create it. MinIO/S3 use us-east-1 as the default region
	// for path-style local endpoints; CreateBucketConfiguration is only needed
	// for non-default regions and would break MinIO, so we omit it.
	if _, err := o.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: &o.bucket}); err != nil {
		// A concurrent bootstrap may have created it between our Head and
		// Create — treat that as success.
		var owned *types.BucketAlreadyOwnedByYou
		var exists *types.BucketAlreadyExists
		if !errors.As(err, &owned) && !errors.As(err, &exists) {
			return fmt.Errorf("s3 create bucket %q: %w", o.bucket, err)
		}
	}

	// Make the bucket public-read so objects render without signed URLs.
	policy := publicReadPolicy(o.bucket)
	if _, err := o.client.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
		Bucket: &o.bucket,
		Policy: &policy,
	}); err != nil {
		return fmt.Errorf("s3 put bucket policy %q: %w", o.bucket, err)
	}
	return nil
}

// publicReadPolicy returns a bucket policy granting anonymous s3:GetObject on
// every object in the bucket. Scoped to exactly the given bucket.
func publicReadPolicy(bucket string) string {
	return fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::%s/*"]}]}`, bucket)
}

// uploadImage handles POST /v1/rest/uploads/image (multipart). It optimises
// the uploaded image and stores it under an account-scoped key, returning the
// public URL.
//
// Request: multipart/form-data with a single "file" part (≤20MB).
// Response: {"url": "..."} 200, or 400 on validation errors.
func (h *RESTHandlers) uploadImage(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	if h.Store == nil {
		recordUploadFailure("not_configured")
		writeErr(w, http.StatusServiceUnavailable, "media storage tidak dikonfigurasi")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+1<<20)

	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		recordUploadFailure("bad_multipart")
		writeErr(w, http.StatusBadRequest, "fail imej tidak sah atau melebihi 20MB")
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	file, _, err := r.FormFile("file")
	if err != nil {
		recordUploadFailure("missing_file")
		writeErr(w, http.StatusBadRequest, "part 'file' diperlukan")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxUploadBytes+1))
	if err != nil {
		recordUploadFailure("read_failed")
		writeErr(w, http.StatusBadRequest, "gagal membaca fail")
		return
	}
	if len(data) == 0 {
		recordUploadFailure("empty")
		writeErr(w, http.StatusBadRequest, "fail kosong")
		return
	}
	if len(data) > maxUploadBytes {
		recordUploadFailure("too_large")
		writeErr(w, http.StatusRequestEntityTooLarge, "imej melebihi had 20MB")
		return
	}

	// Content-type allowlist from the file's actual bytes (magic), not the
	// client's declared header or filename (both spoofable).
	ext, ok := sniffImageType(data)
	if !ok {
		recordUploadFailure("bad_type")
		writeErr(w, http.StatusBadRequest, "jenis imej tidak disokong (png/jpeg/webp/gif sahaja)")
		return
	}
	mime := imageUploadMIME[ext]

	stored, err := optimizeImage(data, ext)
	if err != nil {
		recordUploadFailure("optimize_failed")
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(stored) > maxStoredBytes {
		recordUploadFailure("compress_failed")
		writeErr(w, http.StatusBadRequest, "imej gagal dimampatkan bawah 2MB; cuba imej lain")
		return
	}

	key := buildUploadKey(actx.Account.ID, ext)
	if err := h.Store.PutObject(r.Context(), key, mime, stored); err != nil {
		recordUploadFailure("store")
		slog.Error("upload_put", "err", err, "account", actx.Account.ID)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	slog.Info("upload_ok",
		"account", actx.Account.ID, "key", key,
		"in_bytes", len(data), "out_bytes", len(stored), "mime", mime)
	// Issue #92: audit image upload.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "upload.image", map[string]any{
		"mime": mime, "in_bytes": len(data), "out_bytes": len(stored),
	})
	// Issue #180: record the object in the media library so the dashboard can
	// list it (metadata + filtering). Best-effort — a metadata write failure
	// must not fail the upload itself.
	h.recordMedia(r, actx, key, mime, int64(len(stored)), dbstore.MediaSourceMerchant, "")
	respond(w, http.StatusOK, map[string]string{"url": h.Store.PublicURL(key)})
}

// maxAvatarEdge caps the long edge of an optimised avatar (issue #133).
// Avatars are small circles in the Shell/profile, so ~256px is plenty and
// keeps storage tiny (~50-150KB).
const maxAvatarEdge = 256

// avatarLongEdge downscales the avatar long edge at fit time.
const avatarLongEdge = 256

// optimizeAvatar re-encodes data into a small avatar (≤maxAvatarEdge on the
// long edge). Like optimizeImage it re-encodes to JPEG q~80 so EXIF/GPS/phone
// metadata is stripped and the artifact is tiny. The 2MB cap from the generic
// upload also applies so a pathological input can't blow up storage.
func optimizeAvatar(data []byte, ext string) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("fail kosong")
	}
	img, err := decodeImage(data)
	if err != nil {
		return nil, errors.New("gagal decode imej")
	}
	// Fit to a 256×256 box preserving aspect, then re-encode to JPEG.
	if b := img.Bounds(); b.Dx() > avatarLongEdge || b.Dy() > avatarLongEdge {
		img = imaging.Fit(img, avatarLongEdge, avatarLongEdge, imaging.Lanczos)
	}
	out := encodeJPEG(img, jpegQuality)
	// Shrink quality if somehow still over the 2MB cap (rare).
	for _, q := range []int{70, 60} {
		if len(out) <= maxStoredBytes {
			break
		}
		out = encodeJPEG(img, q)
	}
	if len(out) > maxStoredBytes {
		return nil, errors.New("imej avatar tidak dapat dimampatkan")
	}
	return out, nil
}

// uploadAvatar handles POST /v1/rest/uploads/avatar (multipart). It accepts a
// profile image ≤20MB, downscales it to avatar size (~256px, EXIF stripped)
// and stores it under the account-scoped key, returning the public URL. This
// mirrors the generic image block upload (uploadImage) with an avatar-sized
// output artifact (issue #133).
//
// Request: multipart/form-data with a single "file" part (≤20MB).
// Response: {"url": "..."} 200, or 400 on validation errors.
func (h *RESTHandlers) uploadAvatar(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	if h.Store == nil {
		recordUploadFailure("not_configured")
		writeErr(w, http.StatusServiceUnavailable, "media storage tidak dikonfigurasi")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+1<<20)

	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		recordUploadFailure("bad_multipart")
		writeErr(w, http.StatusBadRequest, "fail imej tidak sah atau melebihi 20MB")
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	file, _, err := r.FormFile("file")
	if err != nil {
		recordUploadFailure("missing_file")
		writeErr(w, http.StatusBadRequest, "part 'file' diperlukan")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxUploadBytes+1))
	if err != nil {
		recordUploadFailure("read_failed")
		writeErr(w, http.StatusBadRequest, "gagal membaca fail")
		return
	}
	if len(data) == 0 {
		recordUploadFailure("empty")
		writeErr(w, http.StatusBadRequest, "fail kosong")
		return
	}
	if len(data) > maxUploadBytes {
		recordUploadFailure("too_large")
		writeErr(w, http.StatusRequestEntityTooLarge, "imej melebihi had 20MB")
		return
	}

	ext, ok := sniffImageType(data)
	if !ok {
		recordUploadFailure("bad_type")
		writeErr(w, http.StatusBadRequest, "jenis imej tidak disokong (png/jpeg/webp/gif sahaja)")
		return
	}
	mime := imageUploadMIME[ext]

	stored, err := optimizeAvatar(data, ext)
	if err != nil {
		recordUploadFailure("optimize_failed")
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(stored) > maxStoredBytes {
		recordUploadFailure("compress_failed")
		writeErr(w, http.StatusBadRequest, "imej avatar tidak dapat dimampatkan")
		return
	}

	key := buildUploadKey(actx.Account.ID, ext)
	if err := h.Store.PutObject(r.Context(), key, mime, stored); err != nil {
		recordUploadFailure("store")
		slog.Error("upload_avatar_put", "err", err, "account", actx.Account.ID)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	slog.Info("upload_avatar_ok",
		"account", actx.Account.ID, "key", key,
		"in_bytes", len(data), "out_bytes", len(stored), "mime", mime)
	h.recordEvent(r, actx, h.sessionOwnerID(r), "upload.avatar", map[string]any{
		"mime": mime, "in_bytes": len(data), "out_bytes": len(stored),
	})
	// Issue #180: record the avatar in the media library (best-effort).
	h.recordMedia(r, actx, key, mime, int64(len(stored)), dbstore.MediaSourceMerchant, "")
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
