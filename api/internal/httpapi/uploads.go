// Image block direct upload (issue #61).
//
// This implements the live owner decision on #61: PRESIGN IS SUPERSEDED by a
// direct multipart upload endpoint. The builder client sends the raw bytes to
// the API; the server validates content-type + size, then server-side
// optimises the image (downscale long edge to 1920px, re-encode to JPEG q~80 —
// or PNG when the image carries transparency, which JPEG cannot represent
// (t_ffc7f502) — and strip EXIF by re-encoding) so the stored artifact is
// ≤2MB. The
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
	"strconv"
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
	// ObjectExists is a HEAD: the media restore path asks it before promising
	// the merchant a URL, because after this card a trashed row and its object
	// have independent lives (the object can be gone while the row is still in
	// the bin).
	ObjectExists(ctx context.Context, key string) (bool, error)
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

// workspaceQuotaExceeded reports whether adding `additional` stored bytes to the
// account would push the workspace over the 1GB per-workspace storage quota (issue
// #384). It sums the account's current non-deleted media bytes and applies the
// shared objstore cap. Returns the human-readable rejection message when the
// upload would exceed the quota, or "" when it is allowed. A nil media store
// (bare-mode tests) is treated as no quota pressure.
//
// Issue #545: this READ-ONLY check is no longer the enforcement mechanism — it
// remains only as a cheap pre-flight so an obviously-too-large upload is rejected
// before the expensive encode. Enforcement is reserveMediaQuota, which takes the
// decision atomically in the database. Do not rely on this function for the cap.
func (h *RESTHandlers) workspaceQuotaExceeded(ctx context.Context, accountID string, additional int64) string {
	if h.Media == nil {
		return ""
	}
	current, err := h.Media.TotalBytes(ctx, accountID)
	if err != nil {
		slog.Error("media_quota_total", "err", err, "account", accountID)
		return ""
	}
	if objstore.ExceedsWorkspaceQuota(current, additional) {
		return "workspace storage limit reached (1GB)"
	}
	return ""
}

// reserveMediaQuota atomically reserves `bytes` against the workspace storage cap.
// Returns false (having written the error response) when the cap would be
// exceeded; on success the caller OWNS the reservation and must either settle it
// (settleMediaQuota) or release it (releaseMediaQuota).
//
// Issue #545: this is the actual enforcement. The previous handler sequence read
// the used total, compared, then stored — so concurrent uploads all passed against
// the same snapshot and collectively blew past the cap. The decision is now a
// single conditional UPDATE in the database, which serialises concurrent racers.
func (h *RESTHandlers) reserveMediaQuota(w http.ResponseWriter, ctx context.Context, accountID string, bytes int64) bool {
	if h.Media == nil || accountID == "" {
		return true // bare mode: no store, nothing to enforce
	}
	if err := h.Media.ReserveQuota(ctx, accountID, bytes); err != nil {
		if errors.Is(err, dbstore.ErrQuotaExceeded) {
			recordUploadFailure("workspace_quota")
			writeErr(w, http.StatusRequestEntityTooLarge, "workspace storage limit reached (1GB)")
			return false
		}
		slog.Error("media_quota_reserve", "err", err, "account", accountID)
		// Fail closed: never store bytes we could not account for.
		recordUploadFailure("workspace_quota")
		writeErr(w, http.StatusServiceUnavailable, "the storage limit could not be checked")
		return false
	}
	return true
}

// releaseMediaQuota gives back a reservation whose object was not stored.
func (h *RESTHandlers) releaseMediaQuota(ctx context.Context, accountID string, bytes int64) {
	if h.Media == nil || accountID == "" || bytes <= 0 {
		return
	}
	if err := h.Media.ReleaseQuota(ctx, accountID, bytes); err != nil {
		slog.Warn("media_quota_release", "err", err, "account", accountID, "bytes", bytes)
	}
}

// settleMediaQuota records the FINAL byte total for an upload (base object plus any
// sized variants and PDF preview), adjusting the up-front reservation. Without
// this, variant/preview bytes are stored but never counted (issue #545).
func (h *RESTHandlers) settleMediaQuota(ctx context.Context, accountID string, reserved, actual int64) {
	if h.Media == nil || accountID == "" || actual <= 0 {
		return
	}
	if err := h.Media.SettleQuotaBytes(ctx, accountID, reserved, actual); err != nil {
		slog.Warn("media_quota_settle", "err", err, "account", accountID,
			"reserved", reserved, "actual", actual)
	}
}

func publicReadPolicy(bucket string) string     { return objstore.PublicReadPolicy(bucket) }
func sniffImageType(data []byte) (string, bool) { return objstore.SniffImageType(data) }
func sniffAssetType(data []byte) (string, bool) { return objstore.SniffAssetType(data) }
func sniffPDFType(data []byte) (string, bool)   { return objstore.SniffPDFType(data) }

// magicByteMismatch enforces the issue #455 magic-byte (file signature) check:
// the sniffed signature must be consistent with the declared upload kind, the
// client-declared filename extension and the client-declared content-type.
// Returns "" when consistent, else a 400-worthy message.
func magicByteMismatch(kind objstore.UploadKind, declaredFilename, declaredMIME, sniffedExt string) string {
	return objstore.MagicByteMismatch(kind, declaredFilename, declaredMIME, sniffedExt)
}

// svgWellFormednessError returns the 400 message for an `.svg` upload whose
// bytes are not a well-formed XML document, or "" when the document is fine.
//
// WHY THE UPLOAD PATH HAS TO ASK (t_20af3915). An SVG is served as
// `image/svg+xml` with `X-Content-Type-Options: nosniff`, so the browser parses
// it as XML. A body with text before the root element — the `/* ... */` banner
// merchants put on hand-written files — is a fatal XML parse error, so the
// asset is stored, is listed, and never renders. The type classification is
// right (v1.21.233); the document is not. Refusing it at upload is the only
// point where the merchant still has the file in hand and can fix it. See
// objstore.ValidateXMLDocument for what "well-formed" means here.
func svgWellFormednessError(data []byte) string { return objstore.SVGNotXMLError(data) }

func buildUploadKey(accountID, ext string) string { return objstore.BuildUploadKey(accountID, ext) }
func buildVariantKey(fullKey string, size objstore.ImageSize) string {
	return objstore.BuildVariantKey(fullKey, size)
}
func buildPDFPreviewKey(fullKey string) string              { return objstore.BuildPDFPreviewKey(fullKey) }
func renderPDFPreview(data []byte) ([]byte, error)          { return objstore.RenderPDFPreview(data) }
func isSizedImage(ct string) bool                           { return objstore.IsSizedImage(ct) }
func optimizeImage(data []byte, ext string) ([]byte, error) { return objstore.OptimizeImage(data, ext) }
func optimizeImageSize(data []byte, ext string, size objstore.ImageSize) ([]byte, error) {
	return objstore.OptimizeImageSize(data, ext, size)
}
func optimizeAvatar(data []byte, ext string) ([]byte, error) {
	return objstore.OptimizeAvatar(data, ext)
}

// ── HTTP handlers ────────────────────────────────────────────────────────────

// mediaCacheControl is the freshness lifetime every /media and /assets response
// declares, and it is the SELF-HEALING half of the purge contract.
//
// It used to be `public, max-age=86400`. That is wrong for objects that can be
// destroyed: `DELETE /v1/rest/media/{id}/purge` removes the row and the object,
// the origin then answers 404 — and the CDN kept serving its own copy for up to
// a day, so a purged (or trashed-then-purged) object stayed downloadable by
// anyone holding the URL. Measured live on 2026-10-18: origin 404 with a
// cache-buster, and the same object 200 from the edge with
// `cf-cache-status: HIT`, `age: 35`, `cache-control: public, max-age=86400`.
// Measuring only the origin is exactly how that stayed invisible, because the
// origin is honest the moment the bytes go.
//
// So the lifetime is bounded to minutes. A purge also asks the CDN to drop the
// entry in the same request (httpapi purgeMedia → internal/cdnpurge), which
// makes the window zero when that is configured; this constant is what bounds
// it when it is not, and what bounds the copies no purge reaches (a different
// zone, an operator's own CDN, a browser that already holds the bytes).
//
// Shortening it cannot serve stale content: every media object is stored under an
// immutable per-upload key, so the bytes behind a URL never change (a re-upload is
// a new key), and a page that references the stored URL simply re-fetches the same
// object — or gets a 404 once it has been purged. The cost is one origin re-fetch
// per object per window instead of one per day, which is the price of being able
// to delete.
const mediaCacheControl = "public, max-age=300"

// mediaCacheMaxAgeSeconds is the same figure as a number, for responses that
// report the residual edge window (mediaCacheControl must stay the single
// source of truth, so it is derived from the header text and tested for it).
var mediaCacheMaxAgeSeconds = cacheMaxAgeSeconds(mediaCacheControl)

// cacheMaxAgeSeconds parses the max-age of a Cache-Control value. It returns 0
// when the value declares no max-age, which a caller must treat as "unbounded
// is impossible" rather than "fresh for ever".
func cacheMaxAgeSeconds(cacheControl string) int {
	for _, part := range strings.Split(cacheControl, ",") {
		part = strings.TrimSpace(part)
		if v, ok := strings.CutPrefix(part, "max-age="); ok {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
				return n
			}
		}
	}
	return 0
}

// publishServedContentType writes the Content-Type for an object being served
// and, when the bytes contradict a legacy css/js record, corrects media.mime in
// the same breath (t_5f6a5d37).
//
// objstore.ServedContentType is what the bytes say to publish; media.mime is
// what the media library (GET /v1/rest/media), MCP list_media/
// list_deleted_media and the trash bin report. Serving one type while every
// listing names the other is the same silent wrongness this card removes one
// layer down, so a corrected object corrects its row too — once: after the
// rewrite the stored row equals the served type, so the branch is dead for
// every later request.
//
// It never fails the response. A missing media row (legacy object), an
// unconfigured store in tests, or an unreachable DB must not turn a working
// stylesheet into a 500; the correction is bookkeeping.
func publishServedContentType(ctx context.Context, w http.ResponseWriter, media *dbstore.MediaStore, key, storedType string, body []byte) {
	served := objstore.ServedContentType(storedType, body)
	w.Header().Set("Content-Type", served)
	if served == storedType || !objstore.IsCodeAsset(storedType) || media == nil {
		return
	}
	if _, err := media.CorrectMimeByKey(ctx, objstore.AccountIDFromKey(key), key, served); err != nil {
		slog.Warn("media_mime_correct", "key", key, "err", err)
	}
}

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
// workspace/domain's account must match the asset key's account, so a merchant
// cannot serve another account's uploaded code via /media. Images remain
// intentionally public (forms/products render them from any origin).
func (h *RESTHandlers) handleMedia(w http.ResponseWriter, r *http.Request) {
	if storeUnavailable(h.Store) {
		buyerErr(w, "en", http.StatusServiceUnavailable, errMediaUnconfigured)
		return
	}
	key := mediaKeyFromPath(r.PathValue("key"))
	if key == "" {
		buyerErr(w, "en", http.StatusBadRequest, errMediaInvalidKey)
		return
	}
	// The key names its account, and that workspace's default language is the
	// buyer's (t_874021eb). A key that names no account resolves to English.
	lang := buyerLangByAccountStore(r.Context(), h.Workspaces, accountIDFromKey(key))
	body, ct, err := h.Store.GetObject(r.Context(), key)
	if err != nil {
		// A store that reports itself unconfigured is a deployment gap, not a
		// server fault: answer the same 503 the guard above writes. The guard
		// already catches a typed nil, so this only fires if the store itself
		// says so — one meaning, one status, never a 500.
		if errors.Is(err, objstore.ErrNotConfigured) {
			buyerErr(w, lang, http.StatusServiceUnavailable, errMediaUnconfigured)
			return
		}
		// Distinguish a missing object (404) from a storage failure (500).
		var nf *types.NoSuchKey
		if errors.As(err, &nf) {
			buyerErr(w, lang, http.StatusNotFound, errMediaNotFound)
			return
		}
		slog.Error("media_get", "err", err, "key", key)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Issue #379: code assets are tenant-scoped even on /media. Resolve the
	// requesting workspace/domain's account and require the asset key's account to
	// match it. Images are intentionally public and unaffected.
	if isCodeAsset(ct) {
		requestingAccount := requestingAccountID(r.Context(), h.Accounts, h.Domains, h.Workspaces, r.Host)
		if !assetOwnedByRequestingTenant(key, requestingAccount) {
			writeErr(w, http.StatusNotFound, "media is not available")
			return
		}
	}
	// Issue #364: code-asset scan gate. A not-yet-scanned, needs-review, or
	// quarantined code asset is never served (404). Images are unaffected.
	if !scanGateAllowed(r.Context(), h.Media, key, ct) {
		writeErr(w, http.StatusNotFound, "media is not available")
		return
	}
	// t_05fd8d2a: publish what the bytes ARE, not what the uploader once
	// recorded — a pre-fix PNG upload is JPEG bytes whose metadata says
	// image/png. t_5f6a5d37: the same for a pre-t_8881000c code asset recorded
	// under the other member of the css/js pair; the row is corrected with it so
	// the media library and MCP listings agree.
	publishServedContentType(r.Context(), w, h.Media, key, ct, body)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", mediaCacheControl)
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
	if storeUnavailable(h.Store) {
		recordUploadFailure("not_configured")
		writeErr(w, http.StatusServiceUnavailable, "media storage is not configured")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+1<<20)

	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		recordUploadFailure("bad_multipart")
		writeErr(w, http.StatusBadRequest, "invalid image file or larger than 20MB")
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		recordUploadFailure("missing_file")
		writeErr(w, http.StatusBadRequest, "part 'file' is required")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxUploadBytes+1))
	if err != nil {
		recordUploadFailure("read_failed")
		writeErr(w, http.StatusBadRequest, "failed to read the file")
		return
	}
	if len(data) == 0 {
		recordUploadFailure("empty")
		writeErr(w, http.StatusBadRequest, "empty file")
		return
	}
	if len(data) > maxUploadBytes {
		recordUploadFailure("too_large")
		writeErr(w, http.StatusRequestEntityTooLarge, "image exceeds the 20MB limit")
		return
	}

	// Content-type allowlist from the file's actual bytes (magic), not the
	// client's declared header or filename (both spoofable). Issue #382: the
	// media-page upload accepts the full allowlist — images, code assets and
	// PDFs — so we sniff in that order. Issue #455: the sniffed signature is
	// then ENFORCED against the declared kind, filename extension and
	// content-type, so a file whose bytes are not what it claims is rejected
	// (400) instead of being silently stored under a different type.
	ext, ok := sniffImageType(data)
	if !ok {
		ext, ok = sniffAssetType(data)
	}
	if !ok {
		ext, ok = sniffPDFType(data)
	}
	if !ok {
		recordUploadFailure("bad_type")
		writeErr(w, http.StatusBadRequest, "unsupported file type (png/jpeg/webp/gif/css/js/html/svg/woff2/pdf only)")
		return
	}
	if msg := magicByteMismatch(objstore.KindMedia, fileHeader.Filename, fileHeader.Header.Get("Content-Type"), ext); msg != "" {
		recordUploadFailure("signature_mismatch")
		writeErr(w, http.StatusBadRequest, msg)
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
			writeErr(w, http.StatusRequestEntityTooLarge, "file exceeds the 2MB limit")
			return
		}
		// t_20af3915: an SVG is served as image/svg+xml, so the browser parses
		// it as XML — a document that is not well-formed never renders.
		if ext == ".svg" {
			if msg := svgWellFormednessError(data); msg != "" {
				recordUploadFailure("svg_not_xml")
				writeErr(w, http.StatusBadRequest, msg)
				return
			}
		}
		// Issue #384: reject an upload that would push the workspace over the 1GB
		// per-workspace storage quota.
		if msg := h.workspaceQuotaExceeded(r.Context(), actx.Account.ID, int64(len(data))); msg != "" {
			recordUploadFailure("workspace_quota")
			writeErr(w, http.StatusRequestEntityTooLarge, msg)
			return
		}
		// Issue #384/#545: reserve atomically before storing (see uploadImage).
		key := buildUploadKey(actx.Account.ID, ext)
		if !h.reserveMediaQuota(w, r.Context(), actx.Account.ID, int64(len(data))) {
			return
		}
		if err := h.Store.PutObject(r.Context(), key, mime, data); err != nil {
			h.releaseMediaQuota(r.Context(), actx.Account.ID, int64(len(data)))
			recordUploadFailure("store")
			slog.Error("upload_put", "err", err, "account", actx.Account.ID)
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
		// Issue #383: for PDFs, auto-generate a first-page preview thumbnail
		// with Ghostscript (bounded render) and store it alongside the PDF
		// under the same account-scoped key namespace. Best-effort: if the
		// render fails or gs is unavailable, the PDF still uploads and the
		// grid falls back to the generic PDF tile.
		//
		// Issue #545: the preview's bytes are real stored bytes, so they are now
		// counted against the reservation.
		previewURL := ""
		storedTotal := int64(len(data))
		if mime == "application/pdf" {
			if preview, perr := renderPDFPreview(data); perr == nil && len(preview) > 0 {
				pkey := buildPDFPreviewKey(key)
				if pkey != "" {
					if perr := h.Store.PutObject(r.Context(), pkey, "image/jpeg", preview); perr != nil {
						slog.Warn("upload_pdf_preview_put", "err", perr, "key", pkey, "account", actx.Account.ID)
					} else {
						previewURL = h.Store.PublicURL(pkey)
						storedTotal += int64(len(preview))
					}
				}
			} else if perr != nil {
				slog.Warn("upload_pdf_preview_failed", "err", perr, "account", actx.Account.ID)
			}
		}
		h.settleMediaQuota(r.Context(), actx.Account.ID, int64(len(data)), storedTotal)
		slog.Info("upload_ok",
			"account", actx.Account.ID, "key", key,
			"in_bytes", len(data), "out_bytes", len(data), "stored_total", storedTotal, "mime", mime)
		h.recordEvent(r, actx, h.sessionOwnerID(r), "upload.image", map[string]any{
			"mime": mime, "in_bytes": len(data), "out_bytes": len(data),
		})
		rec := h.recordMedia(r, actx, key, mime, storedTotal, dbstore.MediaSourceMerchant, "")
		// Issue #364: code assets are scan-gated; PDFs are not code assets and
		// serve immediately. Only run the scan pipeline for code assets.
		if isCodeAsset(mime) {
			h.scanCodeAsset(r, actx, rec, key, mime, data)
		}
		resp := map[string]string{"url": h.merchantMediaURL(r.Context(), actx.Account, h.Store.PublicURL(key), mime)}
		if previewURL != "" {
			resp["preview"] = previewURL
		}
		respond(w, http.StatusOK, resp)
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
		writeErr(w, http.StatusBadRequest, "the image could not be compressed below 2MB; try another image")
		return
	}
	// t_05fd8d2a: the stored artifact's type is a property of the BYTES, never
	// of the file that was uploaded. The optimiser re-encodes (JPEG for an
	// opaque image, PNG when it carries transparency — t_ffc7f502), so the key
	// extension, the object's content-type and the media row must all be read
	// off `stored` — taking them from the input's `ext`/`mime` published a
	// `.png` key holding JPEG bytes as `image/png`.
	storedMIME, storedExt, ok := objstore.ImageArtifactMeta(stored)
	if !ok {
		recordUploadFailure("optimize_failed")
		slog.Error("upload_artifact_type", "account", actx.Account.ID, "out_bytes", len(stored))
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Issue #384/#545: reserve against the 1GB per-workspace cap ATOMICALLY before
	// storing anything. The previous code read the used total, compared, then
	// stored, so concurrent uploads all passed against the same snapshot.
	key := buildUploadKey(actx.Account.ID, storedExt)
	if !h.reserveMediaQuota(w, r.Context(), actx.Account.ID, int64(len(stored))) {
		return
	}
	if err := h.Store.PutObject(r.Context(), key, storedMIME, stored); err != nil {
		h.releaseMediaQuota(r.Context(), actx.Account.ID, int64(len(stored)))
		recordUploadFailure("store")
		slog.Error("upload_put", "err", err, "account", actx.Account.ID)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Issue #382: generate + store thumbnail/medium/full variants for raster
	// images. A GIF is re-encoded to a still image by the optimiser (they always
	// were), so after t_05fd8d2a it is sized like any other one; a transparent
	// image is PNG and gets PNG variants (t_ffc7f502), which is why the variant
	// type is checked against the base below.
	// Variant keys share the full-size key's namespace.
	//
	// Issue #545: variant bytes were previously stored WITHOUT being counted, so
	// up to ~64% of an upload's real bytes were invisible to the quota. Their real
	// sizes are now summed and settled against the reservation.
	sizes := map[string]string{}
	storedTotal := int64(len(stored))
	if isSizedImage(storedMIME) {
		for _, size := range objstore.ImageSizes {
			variant, verr := optimizeImageSize(data, ext, size)
			if verr != nil {
				slog.Warn("upload_size_failed", "err", verr, "size", size, "account", actx.Account.ID)
				continue
			}
			// One representation per media item: the media library derives every
			// variant key from the BASE key's extension, so a variant of another
			// type would land on a key nothing links to. Skip rather than publish
			// an object no URL can reach.
			vMIME, vExt, vok := objstore.ImageArtifactMeta(variant)
			if !vok || vMIME != storedMIME || vExt != storedExt {
				slog.Warn("upload_size_type_mismatch", "size", size, "account", actx.Account.ID,
					"base_mime", storedMIME, "variant_mime", vMIME)
				continue
			}
			vkey := buildVariantKey(key, size)
			if verr := h.Store.PutObject(r.Context(), vkey, vMIME, variant); verr != nil {
				slog.Warn("upload_size_put", "err", verr, "size", size, "key", vkey)
				continue
			}
			storedTotal += int64(len(variant))
			sizes[string(size)] = h.Store.PublicURL(vkey)
		}
	}
	// Settle to the true total (base + variants).
	h.settleMediaQuota(r.Context(), actx.Account.ID, int64(len(stored)), storedTotal)

	slog.Info("upload_ok",
		"account", actx.Account.ID, "key", key,
		"in_bytes", len(data), "out_bytes", len(stored), "stored_total", storedTotal, "mime", storedMIME)
	// Issue #92: audit image upload.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "upload.image", map[string]any{
		"mime": storedMIME, "in_bytes": len(data), "out_bytes": len(stored),
	})
	// Issue #180: record the object in the media library (best-effort). The row
	// carries the true stored total so the usage report matches real storage.
	h.recordMedia(r, actx, key, storedMIME, storedTotal, dbstore.MediaSourceMerchant, "")
	respond(w, http.StatusOK, map[string]any{
		"url":   h.merchantMediaURL(r.Context(), actx.Account, h.Store.PublicURL(key), storedMIME),
		"sizes": sizes,
	})
}

// uploadAvatar handles POST /v1/rest/uploads/avatar (multipart). It accepts a
// profile image ≤20MB, downscales it to avatar size (~256px, EXIF stripped)
// and stores it under the account-scoped key, returning the public URL.
func (h *RESTHandlers) uploadAvatar(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	if storeUnavailable(h.Store) {
		recordUploadFailure("not_configured")
		writeErr(w, http.StatusServiceUnavailable, "media storage is not configured")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+1<<20)

	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		recordUploadFailure("bad_multipart")
		writeErr(w, http.StatusBadRequest, "invalid image file or larger than 20MB")
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		recordUploadFailure("missing_file")
		writeErr(w, http.StatusBadRequest, "part 'file' is required")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxUploadBytes+1))
	if err != nil {
		recordUploadFailure("read_failed")
		writeErr(w, http.StatusBadRequest, "failed to read the file")
		return
	}
	if len(data) == 0 {
		recordUploadFailure("empty")
		writeErr(w, http.StatusBadRequest, "empty file")
		return
	}
	if len(data) > maxUploadBytes {
		recordUploadFailure("too_large")
		writeErr(w, http.StatusRequestEntityTooLarge, "image exceeds the 20MB limit")
		return
	}

	// Issue #455: the avatar path is raster-image-only. The magic-byte check
	// must match the declared image kind, filename extension and content-type.
	ext, ok := sniffImageType(data)
	if !ok {
		recordUploadFailure("bad_type")
		writeErr(w, http.StatusBadRequest, "unsupported image type (png/jpeg/webp/gif only)")
		return
	}
	if msg := magicByteMismatch(objstore.KindRasterImage, fileHeader.Filename, fileHeader.Header.Get("Content-Type"), ext); msg != "" {
		recordUploadFailure("signature_mismatch")
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	stored, err := optimizeAvatar(data, ext)
	if err != nil {
		recordUploadFailure("optimize_failed")
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(stored) > maxStoredBytes {
		recordUploadFailure("compress_failed")
		writeErr(w, http.StatusBadRequest, "the avatar image could not be compressed")
		return
	}
	// t_05fd8d2a: an avatar is re-encoded (JPEG, or PNG when it carries
	// transparency — t_ffc7f502), so its key extension, its stored content-type
	// and its media row all come from the BYTES — not from the (possibly PNG)
	// file that was uploaded.
	storedMIME, storedExt, ok := objstore.ImageArtifactMeta(stored)
	if !ok {
		recordUploadFailure("optimize_failed")
		slog.Error("upload_avatar_artifact_type", "account", actx.Account.ID, "out_bytes", len(stored))
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Issue #545: the avatar path previously stored without ANY quota check, so it
	// could be the upload that tipped an account over the cap uncounted. It now
	// reserves like every other path.
	key := buildUploadKey(actx.Account.ID, storedExt)
	if !h.reserveMediaQuota(w, r.Context(), actx.Account.ID, int64(len(stored))) {
		return
	}
	if err := h.Store.PutObject(r.Context(), key, storedMIME, stored); err != nil {
		h.releaseMediaQuota(r.Context(), actx.Account.ID, int64(len(stored)))
		recordUploadFailure("store")
		slog.Error("upload_avatar_put", "err", err, "account", actx.Account.ID)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	slog.Info("upload_avatar_ok",
		"account", actx.Account.ID, "key", key,
		"in_bytes", len(data), "out_bytes", len(stored), "mime", storedMIME)
	h.recordEvent(r, actx, h.sessionOwnerID(r), "upload.avatar", map[string]any{
		"mime": storedMIME, "in_bytes": len(data), "out_bytes": len(stored),
	})
	// Issue #180: record the avatar in the media library (best-effort).
	h.recordMedia(r, actx, key, storedMIME, int64(len(stored)), dbstore.MediaSourceMerchant, "")
	respond(w, http.StatusOK, map[string]string{
		"url": h.merchantMediaURL(r.Context(), actx.Account, h.Store.PublicURL(key), storedMIME)})
}

// uploadAsset handles POST /v1/rest/uploads/asset (multipart). It accepts a
// merchant code asset — CSS/JS/HTML/font/SVG — for customising a landing/workspace
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
	if storeUnavailable(h.Store) {
		recordUploadFailure("not_configured")
		writeErr(w, http.StatusServiceUnavailable, "media storage is not configured")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAssetUploadBytes+1<<20)

	if err := r.ParseMultipartForm(maxAssetUploadBytes); err != nil {
		recordUploadFailure("bad_multipart")
		writeErr(w, http.StatusBadRequest, "invalid file or larger than 20MB")
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		recordUploadFailure("missing_file")
		writeErr(w, http.StatusBadRequest, "part 'file' is required")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxAssetUploadBytes+1))
	if err != nil {
		recordUploadFailure("read_failed")
		writeErr(w, http.StatusBadRequest, "failed to read the file")
		return
	}
	if len(data) == 0 {
		recordUploadFailure("empty")
		writeErr(w, http.StatusBadRequest, "empty file")
		return
	}
	if len(data) > maxAssetUploadBytes {
		recordUploadFailure("too_large")
		writeErr(w, http.StatusRequestEntityTooLarge, "file exceeds the 20MB limit")
		return
	}

	// Content-type allowlist from the file's actual bytes (magic), not the
	// client's declared header or filename (both spoofable). Issue #455: the
	// sniffed signature is then ENFORCED against the declared asset kind,
	// filename extension and content-type.
	ext, ok := sniffAssetType(data)
	if !ok {
		recordUploadFailure("bad_type")
		writeErr(w, http.StatusBadRequest, "unsupported file type (css/js/html/svg/woff2 only)")
		return
	}
	if msg := magicByteMismatch(objstore.KindAsset, fileHeader.Filename, fileHeader.Header.Get("Content-Type"), ext); msg != "" {
		recordUploadFailure("signature_mismatch")
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	mime := uploadAssetMIME[ext]

	// Size cap: code assets are stored as-is, so enforce a stored ceiling.
	if len(data) > maxAssetBytes {
		recordUploadFailure("asset_too_large")
		writeErr(w, http.StatusRequestEntityTooLarge, "file exceeds the 2MB limit")
		return
	}
	// t_20af3915: an SVG is served as image/svg+xml, so the browser parses it as
	// XML — a document that is not well-formed never renders.
	if ext == ".svg" {
		if msg := svgWellFormednessError(data); msg != "" {
			recordUploadFailure("svg_not_xml")
			writeErr(w, http.StatusBadRequest, msg)
			return
		}
	}

	// Issue #545: reserve atomically before storing (see uploadImage).
	key := buildUploadKey(actx.Account.ID, ext)
	if !h.reserveMediaQuota(w, r.Context(), actx.Account.ID, int64(len(data))) {
		return
	}
	if err := h.Store.PutObject(r.Context(), key, mime, data); err != nil {
		h.releaseMediaQuota(r.Context(), actx.Account.ID, int64(len(data)))
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

	respond(w, http.StatusOK, map[string]string{
		"url": h.merchantMediaURL(r.Context(), actx.Account, h.Store.PublicURL(key), mime)})
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
