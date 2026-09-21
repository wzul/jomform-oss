// Package objstore — tenant-scoped object storage + media upload helpers.
//
// This leaf package lives below both httpapi and mcpsrv (neither imports the
// other, so they cannot share upload code directly: httpapi imports mcpsrv for
// the MCP server field). Extracting the S3/MinIO client and the pure upload
// pipeline here lets the REST handlers (httpapi/uploads.go) and the MCP
// upload_media tool (mcpsrv/media_mcp.go) reuse the exact same allowlist,
// size caps, image optimisation and scan-gated serving rules, so an agent
// uploading media via MCP gets byte-identical treatment to the dashboard.
package objstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // sniff GIF headers
	"image/jpeg"
	_ "image/jpeg" // sniff JPEG headers
	_ "image/png"  // sniff PNG headers
	"io"
	"os/exec"
	"strings"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/disintegration/imaging"
	"github.com/google/uuid"

	"github.com/wzul/jomform/api/internal/dbstore"
)

const (
	// MaxUploadBytes is the largest image a client may send.
	MaxUploadBytes = 20 << 20 // 20MB

	// MaxStoredBytes is the ceiling for the optimised artifact we store.
	MaxStoredBytes = 2 << 20 // 2MB

	// MaxLongEdge downscales images wider than this on the long edge.
	MaxLongEdge = 1920

	// MaxStoredLongEdge is used when shrinking further to hit the 2MB cap.
	MaxStoredLongEdge = 1280

	// JPEGQuality is the re-encode quality (strips EXIF on re-encode).
	JPEGQuality = 80

	// MaxAvatarEdge caps the long edge of an optimised avatar (issue #133).
	MaxAvatarEdge = 256

	// AvatarLongEdge downscales the avatar long edge at fit time.
	AvatarLongEdge = 256

	// MaxAssetBytes caps the stored size of a merchant code asset (issue #361).
	// Code assets are stored as-is, so a generous-but-bounded ceiling prevents
	// one giant file blowing up storage.
	MaxAssetBytes = 2 << 20 // 2MB stored cap for merchant code assets

	// MaxAssetUploadBytes caps the raw multipart body a code-asset upload may
	// send (issue #361).
	MaxAssetUploadBytes = 20 << 20 // 20MB input cap

	// MaxPDFBytes caps the stored size of a merchant PDF (issue #382). PDFs
	// are stored byte-for-byte (no re-encode), so a bounded ceiling prevents
	// one giant file blowing up storage.
	MaxPDFBytes = 2 << 20 // 2MB stored cap for PDFs

	// ThumbEdge is the long-edge target for the thumbnail image size (issue
	// #382). The media grid renders this variant.
	ThumbEdge = 150

	// MediumEdge is the long-edge target for the medium image size (issue
	// #382). The detail modal offers this alongside thumbnail and full.
	MediumEdge = 300

	// PDFPreviewEdge is the long-edge target for the auto-generated PDF preview
	// thumbnail (issue #383). The first page of a PDF is rendered to a JPEG at
	// this size so the media grid can show a real thumbnail instead of a generic
	// file tile.
	PDFPreviewEdge = 300

	// PDFPreviewTimeout bounds how long a single Ghostscript render may run so a
	// malicious or pathological PDF cannot exhaust CPU (issue #383).
	PDFPreviewTimeout = 15 * time.Second

	// MaxWorkspaceBytes is the per-workspace storage quota for all uploaded media/files
	// (issue #384). A single workspace cannot store more than this across every
	// media row (images, code assets, PDFs). Soft-deleting a media object
	// releases its bytes back into the quota.
	MaxWorkspaceBytes = 1 << 30 // 1GB
)

// ExceedsWorkspaceQuota reports whether adding `additional` bytes to an account
// that already stores `current` bytes would push the workspace over the per-workspace
// 1GB storage quota (issue #384). Both the REST upload handlers and the MCP
// upload_media tool share this so a workspace can never exceed the cap.
func ExceedsWorkspaceQuota(current, additional int64) bool {
	return current+additional > MaxWorkspaceBytes
}

// ImageUploadMIME maps an allowed canonical extension to its MIME type.
// Anything else is rejected before touching the bytes.
var ImageUploadMIME = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".webp": "image/webp",
	".gif":  "image/gif",
}

// UploadAssetMIME maps an allowed canonical extension to its MIME type for
// *code assets* — CSS/JS/HTML/fonts a merchant uploads to customise their
// landing/workspace page (issue #361). Unlike images these are stored and served
// byte-for-byte (no re-encode) under the merchant's account-scoped key.
var UploadAssetMIME = map[string]string{
	".css":   "text/css; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".html":  "text/html; charset=utf-8",
	".svg":   "image/svg+xml",
	".woff2": "font/woff2",
}

// UploadPDFMIME maps the canonical PDF extension to its MIME type (issue
// #382). PDFs are stored byte-for-byte (no re-encode) under the merchant's
// account-scoped key, like code assets.
var UploadPDFMIME = map[string]string{
	".pdf": "application/pdf",
}

// ObjectStore is a minimal S3/MinIO client for the media bucket.
type ObjectStore struct {
	client     *s3.Client
	bucket     string
	publicBase string // browser-reachable public URL prefix (issue #145)
}

// NewObjectStore builds an ObjectStore from config. endpoint empty uses AWS.
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

// GetObject streams an object's bytes and content-type from the bucket.
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

// GetBytes is the object-store accessor the asset-scan worker
// (internal/assetgate.Handler) uses to re-fetch an asset's bytes for the
// deterministic + AI scan. It is a thin alias over GetObject, kept as its own
// method so the worker's ObjectFetcher interface (a narrower contract) is
// satisfied without an import cycle.
func (o *ObjectStore) GetBytes(ctx context.Context, key string) ([]byte, string, error) {
	return o.GetObject(ctx, key)
}

// DeleteObject removes an object from the bucket (issue #180). Used by the
// media delete endpoint after the row is soft-deleted. A missing object is
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

// MediaKeyFromPath validates and extracts the object key from a public media
// route path. Uploaded keys are always exactly `u/{account}/{uuid}.{ext}`, so
// the key must be exactly three segments starting with `u/`, with no traversal
// segments, backslashes, percent-encoding, or empty segments. Returns "" when
// the path is not a safe key.
func MediaKeyFromPath(p string) string {
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

// IsCodeAsset reports whether a stored content-type is a merchant code asset
// (issue #361) rather than an image. Code assets are served with stricter
// headers (no cross-origin framing/embedding); images keep lenient embedding.
func IsCodeAsset(ct string) bool {
	switch ct {
	case "text/css; charset=utf-8",
		"text/javascript; charset=utf-8",
		"text/html; charset=utf-8",
		"image/svg+xml",
		"font/woff2",
		"application/javascript; charset=utf-8":
		return true
	}
	return strings.HasPrefix(ct, "text/") || ct == "font/woff2"
}

// AccountIDFromKey extracts the account segment from an object key
// (`u/{accountID}/{uuid}.{ext}`). Returns "" when the key isn't the canonical
// upload shape. Serving gates use it to look up the owning account's scan
// status without trusting the client host.
func AccountIDFromKey(key string) string {
	p := strings.TrimPrefix(key, "/")
	segs := strings.Split(p, "/")
	if len(segs) != 3 || segs[0] != "u" {
		return ""
	}
	return segs[1]
}

// ScanGateAllowed allows an asset to be served. Images are always allowed.
// Merchant code assets (CSS/JS/HTML/SVG) are gated: only an 'ok' scan status
// serves; every other status is fail-closed (404). A nil media store or an
// object with no media row is treated as allowed for images only; a code
// asset with no row is blocked rather than silently served.
func ScanGateAllowed(ctx context.Context, media *dbstore.MediaStore, key, ct string) bool {
	if media == nil {
		return true
	}
	if !IsCodeAsset(ct) {
		return true // images are unaffected by the code-asset gate
	}
	acct := AccountIDFromKey(key)
	if acct == "" {
		return false
	}
	m, err := media.ByKey(ctx, acct, key)
	if err != nil || m == nil {
		return false // no media row for a code asset → cannot prove scanned → block
	}
	return m.ScanStatus == "ok"
}

// EnsureBucket provisions the media bucket if it does not exist and makes it
// public-read (issue #79). It is idempotent.
func (o *ObjectStore) EnsureBucket(ctx context.Context) error {
	_, err := o.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &o.bucket})
	if err == nil {
		return nil
	}
	var notFound *types.NotFound
	var noSuchBucket *types.NoSuchBucket
	if !errors.As(err, &notFound) && !errors.As(err, &noSuchBucket) {
		return fmt.Errorf("s3 head bucket %q: %w", o.bucket, err)
	}
	if _, err := o.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: &o.bucket}); err != nil {
		var owned *types.BucketAlreadyOwnedByYou
		var exists *types.BucketAlreadyExists
		if !errors.As(err, &owned) && !errors.As(err, &exists) {
			return fmt.Errorf("s3 create bucket %q: %w", o.bucket, err)
		}
	}
	policy := PublicReadPolicy(o.bucket)
	if _, err := o.client.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
		Bucket: &o.bucket,
		Policy: &policy,
	}); err != nil {
		return fmt.Errorf("s3 put bucket policy %q: %w", o.bucket, err)
	}
	return nil
}

// PublicReadPolicy returns a bucket policy granting anonymous s3:GetObject on
// every object in the bucket. Scoped to exactly the given bucket.
func PublicReadPolicy(bucket string) string {
	return fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::%s/*"]}]}`, bucket)
}

// BuildUploadKey composes the S3 object key. The client NEVER supplies any
// part of it — account id and uuid are server-controlled, ext comes from the
// sniffed allowlist. The account segment is still sanitised so traversal is
// structurally impossible and the key is always exactly u/<acct>/<uuid>.<ext>.
func BuildUploadKey(accountID, ext string) string {
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

// SniffImageType inspects the leading magic bytes — NOT the client-supplied
// content type — and returns the canonical storage extension if the image is
// one of png/jpeg/webp/gif.
func SniffImageType(data []byte) (string, bool) {
	if len(data) < 16 {
		return "", false
	}
	if bytes.HasPrefix(data, []byte("RIFF")) && bytes.Contains(data[:12], []byte("WEBP")) {
		return ".webp", true
	}
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

// SniffAssetType inspects the leading bytes — NOT the client-supplied
// extension or content-type — and returns the canonical storage extension if
// the blob is a merchant code asset on the allowlist (issue #361):
// css/js/html/svg/woff2.
//
// Issue #455: SVG is text/XML with no binary magic bytes, so it is an explicit
// allowed exception — the root element (optionally behind an XML prolog or
// comment) is its signature.
func SniffAssetType(data []byte) (string, bool) {
	if len(data) == 0 {
		return "", false
	}
	t := strings.TrimLeftFunc(string(data), func(r rune) bool {
		return r <= ' ' // skip leading whitespace/newlines
	})
	lower := strings.ToLower(t)
	if strings.HasPrefix(lower, "<!doctype html") || strings.HasPrefix(lower, "<html") {
		return ".html", true
	}
	if strings.HasPrefix(lower, "<svg") {
		return ".svg", true
	}
	// Issue #455: XML documents (typically an SVG exported/hand-authored with
	// an `<?xml ...?>` prolog and/or a leading comment) have no binary magic.
	// Classify by root element so a real SVG is not rejected.
	if root := xmlRootElement(lower); root == "svg" {
		return ".svg", true
	}
	if len(data) >= 4 && bytes.Equal(data[:4], []byte{'w', 'O', 'F', 'F'}) {
		return ".woff2", true
	}
	for _, p := range []string{
		"/*", "//", `"`, "'", "`",
		"const ", "let ", "var ", "function ", "class ", "async ", "await ", "return ",
		"if ", "else ", "for ", "while ", "do ", "switch ", "case ", "try ", "catch ",
		"throw ", "new ", "typeof ", "instanceof ", "in ", "of ", "yield ", "this",
		"document.", "window.", "alert(", "fetch(",
		"import ", "export ", "function",
		"(function", "(async", "(()", "((",
	} {
		if strings.HasPrefix(lower, p) {
			return ".js", true
		}
	}
	first := string(t[0])
	hasBrace := strings.Contains(lower, "{") && first != "{" && first != "("
	if strings.HasPrefix(lower, "@charset") ||
		strings.HasPrefix(lower, "@import") ||
		strings.HasPrefix(lower, "@media") ||
		strings.HasPrefix(lower, "@supports") ||
		strings.HasPrefix(lower, "@font-face") ||
		strings.HasPrefix(lower, "@keyframes") ||
		strings.HasPrefix(lower, "@layer") ||
		strings.HasPrefix(lower, "@container") ||
		hasBrace {
		return ".css", true
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

// SniffPDFType inspects the leading magic bytes — NOT the client-supplied
// content-type or filename — and returns the canonical storage extension if
// the blob is a PDF (issue #382). A PDF must start with the `%PDF-` header.
func SniffPDFType(data []byte) (string, bool) {
	if len(data) < 5 {
		return "", false
	}
	if bytes.Equal(data[:5], []byte("%PDF-")) {
		return ".pdf", true
	}
	return "", false
}

// xmlRootElement returns the lower-cased name of the first XML element in doc,
// skipping an XML declaration (`<?xml ...?>`), processing instructions,
// comments, and a DOCTYPE. Returns "" when the document has no element or is
// not XML. It exists so an SVG carrying an `<?xml ...?>` prolog or a leading
// comment is still recognised as SVG (issue #455) — SVG is text/XML with no
// binary magic bytes, so its root element is the signature.
func xmlRootElement(doc string) string {
	trim := func(s string) string {
		return strings.TrimLeftFunc(s, func(r rune) bool { return r <= ' ' })
	}
	s := trim(doc)
	for len(s) > 0 && s[0] == '<' {
		switch {
		case strings.HasPrefix(s, "<!--"):
			end := strings.Index(s, "-->")
			if end < 0 {
				return ""
			}
			s = trim(s[end+3:])
		case strings.HasPrefix(s, "<?"):
			end := strings.Index(s, "?>")
			if end < 0 {
				return ""
			}
			s = trim(s[end+2:])
		case strings.HasPrefix(s, "<!"):
			end := strings.Index(s, ">")
			if end < 0 {
				return ""
			}
			s = trim(s[end+1:])
		default:
			// First real element — read its name.
			s = trim(s[1:])
			end := strings.IndexAny(s, " 	\r\n/>")
			if end < 0 {
				return ""
			}
			return strings.ToLower(s[:end])
		}
	}
	return ""
}

// ── Issue #455: magic-byte (file signature) enforcement ──────────────────────
//
// The sniffers above already derive the stored extension from the file's
// actual bytes, never from the client's declared content-type or filename.
// What was missing is *enforcement*: nothing verified that the sniffed
// signature belongs to the kind of upload the endpoint was asked to accept, so
// a file that merely happened to match a different allowlist entry could be
// stored as that other type (e.g. HTML bytes sent to the image endpoint as a
// `.png` were happily stored as `text/html`). The helpers below close that gap:
// the declared kind, the declared filename extension and the declared
// content-type must all be consistent with the sniffed signature, otherwise the
// upload is rejected before anything is stored.

// UploadKind names the family of upload an endpoint accepts (issue #455).
type UploadKind string

const (
	// KindMedia is the media-page/image endpoint, which accepts the full
	// allowlist — raster images, merchant code assets and PDFs (issue #382).
	KindMedia UploadKind = "media"
	// KindRasterImage is a raster-image-only path (profile avatar, MCP
	// upload_media kind=image): png/jpeg/webp/gif.
	KindRasterImage UploadKind = "image"
	// KindAsset is a merchant code-asset path (css/js/html/svg/woff2).
	KindAsset UploadKind = "asset"
	// KindPDF is a PDF-only path.
	KindPDF UploadKind = "pdf"
)

// uploadKindExts maps an upload kind to the canonical extensions its sniffed
// signature may legitimately resolve to.
var uploadKindExts = map[UploadKind]map[string]bool{
	KindMedia:       {".png": true, ".jpg": true, ".webp": true, ".gif": true, ".css": true, ".js": true, ".html": true, ".svg": true, ".woff2": true, ".pdf": true},
	KindRasterImage: {".png": true, ".jpg": true, ".webp": true, ".gif": true},
	KindAsset:       {".css": true, ".js": true, ".html": true, ".svg": true, ".woff2": true},
	KindPDF:         {".pdf": true},
}

// extFamily maps every allowlisted canonical extension to the upload family it
// belongs to, so a client-declared extension can be compared against the
// sniffed signature. Note `image/svg+xml` is a code asset (SVG is text/XML).
var extFamily = map[string]UploadKind{
	".png": KindRasterImage, ".jpg": KindRasterImage, ".jpeg": KindRasterImage,
	".webp": KindRasterImage, ".gif": KindRasterImage,
	".css": KindAsset, ".js": KindAsset, ".html": KindAsset, ".htm": KindAsset,
	".svg": KindAsset, ".woff2": KindAsset,
	".pdf": KindPDF,
}

// extAlias canonicalises equivalent extensions so a declared `.jpeg`/`.htm`
// is compared against the sniffer's `.jpg`/`.html`.
var extAlias = map[string]string{
	".jpeg": ".jpg",
	".htm":  ".html",
}

// mimeFamily maps a client-declared content-type to the upload family it
// claims. Only unambiguous, allowlisted MIME types are mapped; anything else
// (application/octet-stream, empty, unknown) yields no opinion and is ignored,
// because browsers routinely send a generic type for perfectly valid files.
var mimeFamily = map[string]UploadKind{
	"image/png": KindRasterImage, "image/jpeg": KindRasterImage, "image/jpg": KindRasterImage,
	"image/webp": KindRasterImage, "image/gif": KindRasterImage,
	"text/css": KindAsset, "text/html": KindAsset, "image/svg+xml": KindAsset,
	"text/xml": KindAsset, "application/xml": KindAsset,
	"text/javascript": KindAsset, "application/javascript": KindAsset,
	"text/ecmascript": KindAsset, "application/x-javascript": KindAsset,
	"font/woff2": KindAsset, "application/font-woff2": KindAsset,
	"application/pdf": KindPDF,
}

// canonicalExt normalises an extension through extAlias (identity when absent).
func canonicalExt(ext string) string {
	if c, ok := extAlias[ext]; ok {
		return c
	}
	return ext
}

// declaredExtOf returns the lower-cased extension of a client-declared filename
// (including the leading dot), or "" when there is none.
func declaredExtOf(filename string) string {
	name := strings.ToLower(strings.TrimSpace(filename))
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	dot := strings.LastIndex(name, ".")
	if dot <= 0 || dot == len(name)-1 {
		return ""
	}
	return name[dot:]
}

// textExts are the code assets with no binary magic bytes — they are
// classified by heuristics over their text, which is inherently ambiguous
// (e.g. a CSS file starting with a `/*! license */` comment reads like a JS
// block comment). Cross-checking a declared extension against a sniffed one
// inside this group would reject real uploads, so the declared-vs-sniffed
// comparison is only exact when a binary signature is involved; within the
// text group the kind allowlist is the gate.
var textExts = map[string]bool{".css": true, ".js": true, ".html": true, ".svg": true}

// familyOf maps an extension to its upload family, defaulting to "" when
// unknown.
func familyOf(ext string) UploadKind { return extFamily[ext] }

// MagicByteMismatch reports why a sniffed signature is not acceptable for an
// upload, or "" when it is fine (issue #455). `sniffedExt` is the extension the
// magic-byte sniffer derived from the bytes; `declaredFilename` and
// `declaredMIME` are the client's claims (either may be empty). The check is:
//
//  1. the sniffed signature must be one the declared kind may accept — a
//     mismatch here is the spoof the issue is about (HTML bytes uploaded to the
//     avatar path as a `.png`, PDF bytes uploaded as CSS, …);
//  2. when the client declared a *known* extension, it must agree with the
//     sniffed signature. Agreement is exact whenever either side is a binary
//     format (png/jpeg/webp/gif/pdf/woff2 — real magic bytes); only when BOTH
//     sides are in the heuristic text code-asset group (css/js/html/svg) is the
//     comparison relaxed to the kind allowlist, because there a real file can
//     legitimately sniff into a sibling (a CSS file starting with a `/*!`
//     comment reads as JS);
//  3. when the client declared a *known* content-type, its family must match
//     the sniffed signature's family.
//
// Unknown/ambiguous declarations are ignored so valid uploads with odd
// filenames (e.g. a JPEG named `.jfif`) or a generic
// `application/octet-stream` type are not rejected.
func MagicByteMismatch(kind UploadKind, declaredFilename, declaredMIME, sniffedExt string) string {
	if sniffedExt == "" {
		return "kandungan fail tidak dapat dikenal pasti"
	}
	allowed, ok := uploadKindExts[kind]
	if !ok || !allowed[sniffedExt] {
		return "kandungan fail tidak sepadan dengan jenis fail yang didakwa"
	}
	sniffed := canonicalExt(sniffedExt)
	sniffedFamily := familyOf(sniffed)
	if dex := canonicalExt(declaredExtOf(declaredFilename)); dex != "" {
		if declFamily, known := extFamily[dex]; known {
			// Exact comparison unless both sides are heuristic text assets.
			if !(textExts[dex] && textExts[sniffed]) && (declFamily != sniffedFamily || dex != sniffed) {
				return "kandungan fail tidak sepadan dengan sambungan fail"
			}
		}
	}
	if dm := strings.ToLower(strings.TrimSpace(strings.SplitN(declaredMIME, ";", 2)[0])); dm != "" {
		if declFamily, known := mimeFamily[dm]; known && declFamily != sniffedFamily {
			return "kandungan fail tidak sepadan dengan jenis kandungan yang didakwa"
		}
	}
	return ""
}

// ImageSize names a generated image variant (issue #382). Thumbnail and
// medium are downscaled re-encodes; full is the optimised original.
type ImageSize string

const (
	SizeThumb  ImageSize = "thumbnail"
	SizeMedium ImageSize = "medium"
	SizeFull   ImageSize = "full"
)

// ImageSizes lists the generated variants in display order.
var ImageSizes = []ImageSize{SizeThumb, SizeMedium, SizeFull}

// SizeLongEdge returns the long-edge target for a generated image size.
func SizeLongEdge(s ImageSize) int {
	switch s {
	case SizeThumb:
		return ThumbEdge
	case SizeMedium:
		return MediumEdge
	default:
		return MaxLongEdge
	}
}

// IsSizedImage reports whether a stored content-type is a raster image that
// gets thumbnail/medium/full variants (issue #382). GIFs are excluded — they
// are animated and stored as-is (no re-encode), so they have no generated
// sizes. SVG is a code asset, not a sized raster.
func IsSizedImage(ct string) bool {
	switch ct {
	case "image/png", "image/jpeg", "image/webp":
		return true
	}
	return false
}

// BuildVariantKey derives the object key for a generated image size from the
// full-size key. Full-size keys are `u/{account}/{uuid}.{ext}`; the variant
// key is `u/{account}/{uuid}-{size}.{ext}` so all three variants share the
// same account-scoped namespace and the same base filename. Returns "" when
// the key is not the canonical upload shape.
func BuildVariantKey(fullKey string, size ImageSize) string {
	// Only canonical `u/{acct}/{uuid}.{ext}` keys get variants.
	if AccountIDFromKey(fullKey) == "" {
		return ""
	}
	dot := strings.LastIndex(fullKey, ".")
	if dot <= 0 {
		return ""
	}
	base, ext := fullKey[:dot], fullKey[dot:]
	return base + "-" + string(size) + ext
}

// BuildPDFPreviewKey derives the object key for a PDF's auto-generated preview
// image from the PDF's full-size key (issue #383). Full-size keys are
// `u/{account}/{uuid}.pdf`; the preview key is `u/{account}/{uuid}-preview.jpg`
// so it shares the same account-scoped namespace and base filename. Returns ""
// when the key is not the canonical upload shape.
func BuildPDFPreviewKey(fullKey string) string {
	if AccountIDFromKey(fullKey) == "" {
		return ""
	}
	dot := strings.LastIndex(fullKey, ".")
	if dot <= 0 {
		return ""
	}
	return fullKey[:dot] + "-preview.jpg"
}

// RenderPDFPreview renders the first page of a PDF to a JPEG thumbnail using
// Ghostscript (the same approach WordPress uses, issue #383). The render is
// bounded: only the first page is rendered, the output is size-capped to
// PDFPreviewEdge on the long edge, and the whole render is killed after
// PDFPreviewTimeout so a malicious or pathological PDF cannot exhaust CPU.
//
// It is best-effort: if the `gs` binary is unavailable (e.g. a minimal test
// environment) it returns (nil, nil) so the caller can skip the preview rather
// than fail the upload. The Dockerfile installs Ghostscript for production.
func RenderPDFPreview(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("fail kosong")
	}
	gsPath, err := exec.LookPath("gs")
	if err != nil {
		// Ghostscript not installed — skip the preview (best-effort). The
		// api Dockerfile installs it for production.
		return nil, nil
	}
	// Render the first page to a PNG at a bounded resolution, then downscale
	// to the preview edge. -dFirstPage=1 -dLastPage=1 bounds the page count;
	// -r72 keeps the raster small; -dSAFER disables PostScript file access.
	ctx, cancel := context.WithTimeout(context.Background(), PDFPreviewTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, gsPath,
		"-q", "-dSAFER", "-dBATCH", "-dNOPAUSE",
		"-sDEVICE=png16m",
		"-dFirstPage=1", "-dLastPage=1",
		"-r72",
		"-sOutputFile=-", "-",
	)
	cmd.Stdin = bytes.NewReader(data)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, errors.New("papar PDF tamat masa")
		}
		return nil, errors.New("gagal papar PDF")
	}
	if out.Len() == 0 {
		return nil, errors.New("gagal papar PDF")
	}
	img, err := decodeImage(out.Bytes())
	if err != nil {
		return nil, errors.New("gagal decode paparan PDF")
	}
	if b := img.Bounds(); b.Dx() > PDFPreviewEdge || b.Dy() > PDFPreviewEdge {
		img = imaging.Fit(img, PDFPreviewEdge, PDFPreviewEdge, imaging.Lanczos)
	}
	preview := encodeJPEG(img, JPEGQuality)
	if len(preview) == 0 {
		return nil, errors.New("gagal papar PDF")
	}
	return preview, nil
}

// OptimizeImage re-encodes data so the stored artifact is ≤MaxStoredBytes.
func OptimizeImage(data []byte, ext string) ([]byte, error) {
	if len(data) <= MaxStoredBytes && ext == ".webp" {
		return data, nil
	}
	isGIF := ext == ".gif"

	img, err := decodeImage(data)
	if err != nil {
		return nil, errors.New("gagal decode imej")
	}

	b := img.Bounds()
	long := b.Dx()
	if b.Dy() > long {
		long = b.Dy()
	}
	if long > MaxLongEdge {
		img = imaging.Fit(img, MaxLongEdge, MaxLongEdge, imaging.Lanczos)
	}

	out := encodeJPEG(img, JPEGQuality)

	for _, q := range []int{70, 60, 50} {
		if len(out) <= MaxStoredBytes || isGIF {
			break
		}
		if len(out) > MaxStoredBytes {
			out = encodeJPEG(img, q)
		}
	}
	if len(out) > MaxStoredBytes {
		img = imaging.Fit(img, MaxStoredLongEdge, MaxStoredLongEdge, imaging.Lanczos)
		out = encodeJPEG(img, JPEGQuality)
	}
	if len(out) > MaxStoredBytes {
		return nil, errors.New("imej tidak dapat dimampatkan bawah 2MB")
	}
	return out, nil
}

// OptimizeImageSize re-encodes data into a specific image size variant (issue
// #382). It decodes once, fits the long edge to the size's target, and
// re-encodes to JPEG q~80 (stripping EXIF by construction). The thumbnail and
// medium variants are always small enough to skip the 2MB cap loop; the full
// variant reuses OptimizeImage's downscale-to-cap behaviour.
func OptimizeImageSize(data []byte, ext string, size ImageSize) ([]byte, error) {
	if size == SizeFull {
		return OptimizeImage(data, ext)
	}
	img, err := decodeImage(data)
	if err != nil {
		return nil, errors.New("gagal decode imej")
	}
	edge := SizeLongEdge(size)
	if b := img.Bounds(); b.Dx() > edge || b.Dy() > edge {
		img = imaging.Fit(img, edge, edge, imaging.Lanczos)
	}
	out := encodeJPEG(img, JPEGQuality)
	if len(out) > MaxStoredBytes {
		// A tiny source can still exceed 2MB only if it is enormous; shrink
		// further to the stored long edge to stay under the cap.
		img = imaging.Fit(img, MaxStoredLongEdge, MaxStoredLongEdge, imaging.Lanczos)
		out = encodeJPEG(img, JPEGQuality)
	}
	if len(out) > MaxStoredBytes {
		return nil, errors.New("imej tidak dapat dimampatkan bawah 2MB")
	}
	return out, nil
}

// OptimizeAvatar re-encodes data into a small avatar (≤MaxAvatarEdge on the
// long edge). Like OptimizeImage it re-encodes to JPEG q~80 so EXIF/GPS/phone
// metadata is stripped and the artifact is tiny.
func OptimizeAvatar(data []byte, ext string) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("fail kosong")
	}
	img, err := decodeImage(data)
	if err != nil {
		return nil, errors.New("gagal decode imej")
	}
	if b := img.Bounds(); b.Dx() > AvatarLongEdge || b.Dy() > AvatarLongEdge {
		img = imaging.Fit(img, AvatarLongEdge, AvatarLongEdge, imaging.Lanczos)
	}
	out := encodeJPEG(img, JPEGQuality)
	for _, q := range []int{70, 60} {
		if len(out) <= MaxStoredBytes {
			break
		}
		out = encodeJPEG(img, q)
	}
	if len(out) > MaxStoredBytes {
		return nil, errors.New("imej avatar tidak dapat dimampatkan")
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
