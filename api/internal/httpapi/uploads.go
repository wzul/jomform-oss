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
// The object store, allowlists, size caps, image optimisation and key building
// live in internal/objstore so the dashboard (REST) and the MCP upload_media
// tool share exactly the same pipeline (issue #363 MCP parity). This file
// keeps the HTTP handlers + the small re-exports the other httpapi files and
// tests rely on.
package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/wzul/jomform/api/internal/assetgate"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/objstore"
)

// ── Re-exported objstore symbols (kept so existing call sites + tests read
// ── identically; implementations live in internal/objstore). ────────────────

// ObjectStore is the S3/MinIO client for the media bucket.
type ObjectStore = objstore.ObjectStore

// ObjectStoreIface is the subset of the object store the HTTP handlers use.
// It lets tests inject a fake store without a live S3/MinIO (issue #379
// regression tests exercise the tenant-ownership gate on asset serving).
type ObjectStoreIface interface {
	GetObject(ctx context.Context, key string) ([]byte, string, error)
	PutObject(ctx context.Context, key, contentType string, body []byte) error
	PublicURL(key string) string
	DeleteObject(ctx context.Context, key string) error
}

// Compile-time check: the concrete store satisfies the handler interface.
var _ ObjectStoreIface = (*ObjectStore)(nil)

// Allowlists + constants.
var (
	imageUploadMIME = objstore.ImageUploadMIME
	uploadAssetMIME = objstore.UploadAssetMIME
	uploadPDFMIME   = objstore.UploadPDFMIME
)

// Constant aliases (objstore exports untyped consts so callers can use them
// in untyped-constant contexts like http.MaxBytesReader's int64).
const (
	maxUploadBytes      = objstore.MaxUploadBytes
	maxStoredBytes      = objstore.MaxStoredBytes
	maxLongEdge         = objstore.MaxLongEdge
	maxStoredLongEdge   = objstore.MaxStoredLongEdge
	jpegQuality         = objstore.JPEGQuality
	maxAvatarEdge       = objstore.MaxAvatarEdge
	avatarLongEdge      = objstore.AvatarLongEdge
	maxAssetBytes       = objstore.MaxAssetBytes
	maxAssetUploadBytes = objstore.MaxAssetUploadBytes
	maxPDFBytes         = objstore.MaxPDFBytes
	thumbEdge           = objstore.ThumbEdge
	mediumEdge          = objstore.MediumEdge
)

func NewObjectStore(endpoint, accessKey, secretKey, bucket, region, publicBase string) (*ObjectStore, error) {
	return objstore.NewObjectStore(endpoint, accessKey, secretKey, bucket, region, publicBase)
}
func mediaKeyFromPath(p string) string   { return objstore.MediaKeyFromPath(p) }
func isCodeAsset(ct string) bool         { return objstore.IsCodeAsset(ct) }
func accountIDFromKey(key string) string { return objstore.AccountIDFromKey(key) }

// scanGateAllowed wraps the objstore scan gate with the httpapi signature
// (context.Context + *dbstore.MediaStore).
func scanGateAllowed(ctx context.Context, media *dbstore.MediaStore, key, ct string) bool {
	return objstore.ScanGateAllowed(ctx, media, key, ct)
}

func publicReadPolicy(bucket string) string { return objstore.PublicReadPolicy(bucket) }
func sniffImageType(data []byte) (string, bool) { return objstore.SniffImageType(data) }
func sniffAssetType(data []byte) (string, bool) { return objstore.SniffAssetType(data) }
func sniffPDFType(data []byte) (string, bool) { return objstore.SniffPDFType(data) }
func buildUploadKey(accountID, ext string) string { return objstore.BuildUploadKey(accountID, ext) }
func buildVariantKey(fullKey string, size objstore.ImageSize) string { return objstore.BuildVariantKey(fullKey, size) }
func isSizedImage(ct string) bool { return objstore.IsSizedImage(ct) }
func optimizeImage(data []byte, ext string) ([]byte, error) { return objstore.OptimizeImage(data, ext) }
func optimizeImageSize(data []byte, ext string, size objstore.ImageSize) ([]byte, error) { return objstore.OptimizeImageSize(data, ext, size) }
func optimizeAvatar(data []byte, ext string) ([]byte, error) { return objstore.OptimizeAvatar(data, ext) }

// ── HTTP handlers ────────────────────────────────────────────────────────────

// handleMedia serves GET /media/{key} — a public, unauthenticated route that
// streams an uploaded object from the media bucket (issue #145). The key is
// validated to a safe `u/...` path (no traversal), the object's stored
// content-type is preserved, and a short cache header is set. 404 when the
// object is missing.
//
// Issue #361 serving hardening: every response carries X-Content-Type-Options:
// nosniff so a spoofed/ambiguous asset is never MIME-sniffed into an
// executable type. Merchant code assets additionally get X-Frame-Options:
// DENY, frame-ancestors 'none' CSP and Cross-Origin-Resource-Policy:
// same-origin so another site cannot frame or embed them. Images keep lenient
// embedding (forms/products render them from any origin).
//
// Issue #379: code assets are tenant-scoped even on /media. The requesting
// space/domain's account must match the asset key's account, so a merchant
// cannot serve another account's uploaded code via /media. Images remain
// intentionally public (forms/products render them from any origin).
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
	// Issue #379: code assets are tenant-scoped even on /media. Resolve the
	// requesting space/domain's account and require the asset key's account to
	// match it. Images are intentionally public and unaffected.
	if isCodeAsset(ct) {
		requestingAccount := requestingAccountID(r.Context(), h.Accounts, h.Domains, h.Spaces, r.Host)
		if !assetOwnedByRequestingTenant(key, requestingAccount) {
			writeErr(w, http.StatusNotFound, "media tidak tersedia")
			return
		}
	}
	// Issue #364: code-asset scan gate. A not-yet-scanned, needs-review, or
	// quarantined code asset is never served (404). Images are unaffected.
	if !scanGateAllowed(r.Context(), h.Media, key, ct) {
		writeErr(w, http.StatusNotFound, "media tidak tersedia")
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	if isCodeAsset(ct) {
		// Code assets: never framed, never embedded cross-origin.
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// uploadImage handles POST /v1/rest/uploads/image (multipart). It accepts the
// full media allowlist — images (png/jpeg/webp/gif), merchant code assets
// (css/js/html/svg/woff2) and PDFs — and stores the object under an
// account-scoped key, returning the public URL(s).
//
// Issue #382: for raster images (png/jpeg/webp) it additionally generates
// thumbnail / medium / full variants and stores them under the same
// account-scoped key namespace (`u/{acct}/{uuid}-{size}.{ext}`). The response
// carries a `sizes` map so the media grid can render the thumbnail and the
// detail modal can offer all three.
//
// Request: multipart/form-data with a single "file" part (≤20MB).
// Response: {"url": "...", "sizes": {"thumbnail": "...", "medium": "...",
// "full": "..."}} 200, or 400 on validation errors.
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
	// client's declared header or filename (both spoofable). Issue #382: the
	// media-page upload accepts the full allowlist — images, code assets and
	// PDFs — so we sniff in that order.
	ext, ok := sniffImageType(data)
	if !ok {
		ext, ok = sniffAssetType(data)
	}
	if !ok {
		ext, ok = sniffPDFType(data)
	}
	if !ok {
		recordUploadFailure("bad_type")
		writeErr(w, http.StatusBadRequest, "jenis fail tidak disokong (png/jpeg/webp/gif/css/js/html/svg/woff2/pdf sahaja)")
		return
	}

	var mime string
	if m, isImg := imageUploadMIME[ext]; isImg {
		mime = m
	} else if m, isAsset := uploadAssetMIME[ext]; isAsset {
		mime = m
	} else {
		mime = uploadPDFMIME[ext]
	}

	// Issue #382: code assets and PDFs are stored byte-for-byte (no re-encode)
	// under a bounded stored cap; images are optimised and get sized variants.
	if isCodeAsset(mime) || mime == "application/pdf" {
		if len(data) > maxAssetBytes {
			recordUploadFailure("asset_too_large")
			writeErr(w, http.StatusRequestEntityTooLarge, "fail melebihi had 2MB")
			return
		}
		key := buildUploadKey(actx.Account.ID, ext)
		if err := h.Store.PutObject(r.Context(), key, mime, data); err != nil {
			recordUploadFailure("store")
			slog.Error("upload_put", "err", err, "account", actx.Account.ID)
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
		slog.Info("upload_ok",
			"account", actx.Account.ID, "key", key,
			"in_bytes", len(data), "out_bytes", len(data), "mime", mime)
		h.recordEvent(r, actx, h.sessionOwnerID(r), "upload.image", map[string]any{
			"mime": mime, "in_bytes": len(data), "out_bytes": len(data),
		})
		rec := h.recordMedia(r, actx, key, mime, int64(len(data)), dbstore.MediaSourceMerchant, "")
		// Issue #364: code assets are scan-gated; PDFs are not code assets and
		// serve immediately. Only run the scan pipeline for code assets.
		if isCodeAsset(mime) {
			h.scanCodeAsset(r, actx, rec, key, mime, data)
		}
		respond(w, http.StatusOK, map[string]string{"url": h.Store.PublicURL(key)})
		return
	}

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

	// Issue #382: generate + store thumbnail/medium/full variants for raster
	// images (png/jpeg/webp). GIFs are animated and stored as-is, so they get
	// no sized variants. Variant keys share the full-size key's namespace.
	sizes := map[string]string{}
	if isSizedImage(mime) {
		for _, size := range objstore.ImageSizes {
			variant, verr := optimizeImageSize(data, ext, size)
			if verr != nil {
				slog.Warn("upload_size_failed", "err", verr, "size", size, "account", actx.Account.ID)
				continue
			}
			vkey := buildVariantKey(key, size)
			if verr := h.Store.PutObject(r.Context(), vkey, "image/jpeg", variant); verr != nil {
				slog.Warn("upload_size_put", "err", verr, "size", size, "key", vkey)
				continue
			}
			sizes[string(size)] = h.Store.PublicURL(vkey)
		}
	}

	slog.Info("upload_ok",
		"account", actx.Account.ID, "key", key,
		"in_bytes", len(data), "out_bytes", len(stored), "mime", mime)
	// Issue #92: audit image upload.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "upload.image", map[string]any{
		"mime": mime, "in_bytes": len(data), "out_bytes": len(stored),
	})
	// Issue #180: record the object in the media library (best-effort).
	h.recordMedia(r, actx, key, mime, int64(len(stored)), dbstore.MediaSourceMerchant, "")
	respond(w, http.StatusOK, map[string]any{"url": h.Store.PublicURL(key), "sizes": sizes})
}

// uploadAvatar handles POST /v1/rest/uploads/avatar (multipart). It accepts a
// profile image ≤20MB, downscales it to avatar size (~256px, EXIF stripped)
// and stores it under the account-scoped key, returning the public URL.
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

// uploadAsset handles POST /v1/rest/uploads/asset (multipart). It accepts a
// merchant code asset — CSS/JS/HTML/font/SVG — for customising a landing/space
// page (issue #361). Bytes are stored as-is (no re-encode) under the same
// account-scoped `u/<acct>/<uuid>.<ext>` key. Customer (form-attachment)
// uploads stay images-only; code assets are merchant-only.
//
// Serving hardening (folded in from #364): the asset is stored tenant-scoped,
// capped in size, never executed server-side, and streamed by the public media
// route with restrictive headers. The media row carries a scan_status column
// the #364 background scan flips to gate serving.
func (h *RESTHandlers) uploadAsset(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	if h.Store == nil {
		recordUploadFailure("not_configured")
		writeErr(w, http.StatusServiceUnavailable, "media storage tidak dikonfigurasi")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAssetUploadBytes+1<<20)

	if err := r.ParseMultipartForm(maxAssetUploadBytes); err != nil {
		recordUploadFailure("bad_multipart")
		writeErr(w, http.StatusBadRequest, "fail tidak sah atau melebihi 20MB")
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

	data, err := io.ReadAll(io.LimitReader(file, maxAssetUploadBytes+1))
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
	if len(data) > maxAssetUploadBytes {
		recordUploadFailure("too_large")
		writeErr(w, http.StatusRequestEntityTooLarge, "fail melebihi had 20MB")
		return
	}

	// Content-type allowlist from the file's actual bytes (magic), not the
	// client's declared header or filename (both spoofable).
	ext, ok := sniffAssetType(data)
	if !ok {
		recordUploadFailure("bad_type")
		writeErr(w, http.StatusBadRequest, "jenis fail tidak disokong (css/js/html/svg/woff2 sahaja)")
		return
	}
	mime := uploadAssetMIME[ext]

	// Size cap: code assets are stored as-is, so enforce a stored ceiling.
	if len(data) > maxAssetBytes {
		recordUploadFailure("asset_too_large")
		writeErr(w, http.StatusRequestEntityTooLarge, "fail melebihi had 2MB")
		return
	}

	key := buildUploadKey(actx.Account.ID, ext)
	if err := h.Store.PutObject(r.Context(), key, mime, data); err != nil {
		recordUploadFailure("store")
		slog.Error("upload_asset_put", "err", err, "account", actx.Account.ID)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	slog.Info("upload_asset_ok",
		"account", actx.Account.ID, "key", key,
		"in_bytes", len(data), "mime", mime)
	// Issue #92: audit asset upload.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "upload.asset", map[string]any{
		"mime": mime, "bytes": len(data),
	})
	// Issue #180: record the object in the media library (best-effort).
	rec := h.recordMedia(r, actx, key, mime, int64(len(data)), dbstore.MediaSourceMerchant, "")

	// Issue #364: code-asset scan gate (shared with the media-page upload).
	h.scanCodeAsset(r, actx, rec, key, mime, data)

	respond(w, http.StatusOK, map[string]string{"url": h.Store.PublicURL(key)})
}

// scanCodeAsset runs the issue #364 code-asset scan gate on a freshly stored
// code asset. Deterministic heuristics first; if they flag the file we persist
// 'quarantined' immediately. Otherwise set 'scanning' and enqueue a background
// asset_scan job. Fail-closed: any gap leaves the row 'pending'/'scanning',
// which never serves. rec may be nil (media metadata unavailable) — the scan
// status is then simply not persisted.
func (h *RESTHandlers) scanCodeAsset(r *http.Request, actx *AccountCtx, rec *dbstore.Media, key, mime string, data []byte) {
	if rec == nil || h.Media == nil {
		return
	}
	det := assetgate.DeterministicScan(data, mime)
	var scanStatus, scanReason string
	switch {
	case det.Status == assetgate.StatusQuarantined:
		scanStatus = assetgate.StatusQuarantined
		scanReason = strings.Join(det.Reasons, "; ")
	case h.AssetQueue != nil:
		scanStatus = assetgate.StatusScanning
		scanReason = "queued for AI review"
		_ = h.Media.MarkScanning(r.Context(), actx.Account.ID, rec.ID, scanReason)
		if err := assetgate.Enqueue(r.Context(), h.AssetQueue, assetgate.ScanJob{
			MediaID: rec.ID, Key: key, AccountID: actx.Account.ID, Mime: mime,
		}); err != nil {
			slog.Warn("asset_scan_enqueue_failed", "media", rec.ID, "err", err)
		} else {
			slog.Info("asset_scan_enqueued", "media", rec.ID, "key", key)
		}
	default:
		scanStatus = assetgate.StatusPending
	}
	if scanStatus != "" {
		_ = h.Media.SetScanStatus(r.Context(), actx.Account.ID, rec.ID, scanStatus, scanReason)
	}
}
