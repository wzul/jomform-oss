// MCP tool: list media library (issue #180).
package mcpsrv

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/assetgate"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/objstore"
)

type mediaOut struct {
	ID         string `json:"id"`
	FormID     string `json:"form_id,omitempty"`
	Key        string `json:"key"`
	URL        string `json:"url"`
	Mime       string `json:"mime"`
	Size       int64  `json:"size"`
	Source     string `json:"source"`
	ScanStatus string `json:"scan_status,omitempty"`
	ScanReason string `json:"scan_reason,omitempty"`
	// Sizes maps a generated image size name (thumbnail/medium/full) to its
	// public URL for raster images (issue #382). Empty for non-images/GIFs.
	Sizes map[string]string `json:"sizes,omitempty"`
	// Preview is the auto-generated first-page thumbnail URL for a PDF
	// (issue #383), derived from the object key. Empty when no preview was
	// generated.
	Preview   string `json:"preview,omitempty"`
	CreatedAt string `json:"created_at"`
}

type listMediaIn struct {
	FormID string `json:"form_id,omitempty" jsonschema:"Optional — tapis media mengikut borang (id dari list_forms)"`
	Source string `json:"source,omitempty" jsonschema:"Optional — merchant|customer (lalai: semua)"`
	Page   int    `json:"page,omitempty" jsonschema:"Optional — muka surat (mulai 1)"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Optional — had setiap muka (1-100, lalai 20)"`
}

// listMedia returns one page of the user's uploaded media, newest first,
// optionally filtered by form and source. Issue #307: spans all of the user's
// workspaces (ownership enforced inside the store queries). Read-only.
func (s *Server) listMedia(ctx context.Context, _ *mcp.CallToolRequest, in listMediaIn) (*mcp.CallToolResult, map[string]any, error) {
	accs, err := s.accountScopes(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Media == nil {
		return nil, nil, fmt.Errorf("media tidak tersedia")
	}
	if in.Source != "" && in.Source != "merchant" && in.Source != "customer" {
		return nil, nil, fmt.Errorf("source mesti merchant|customer")
	}
	page, limit := in.Page, in.Limit
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	out := make([]mediaOut, 0)
	var total int64
	for _, accountID := range accs {
		items, tot, err := s.deps.Media.ListPage(ctx, accountID, in.FormID, in.Source, page, limit)
		if err != nil {
			return nil, nil, fmt.Errorf("db: %w", err)
		}
		total += tot
		for _, m := range items {
			out = append(out, s.mediaOutFromRow(m))
		}
	}
	return nil, map[string]any{
		"items": out, "page": page, "limit": limit, "total": total,
	}, nil
}

// ── upload_media (issue #363) ───────────────────────────────────────────────
// MCP parity for the dashboard media upload (image #61 + code asset #361). The
// agent sends the raw bytes base64-encoded and the asset kind (image|asset),
// and the tool runs the EXACT same tenant-scoped pipeline as the REST upload:
// allowlisted sniffing (magic bytes), size caps, image optimisation (EXIF
// strip / downscale), account-scoped S3 key, media row, and the #364 code-asset
// scan gate (fail-closed). Images are served immediately; code assets wait for
// the background scan to flip them to 'ok' before they render.

type uploadMediaIn struct {
	Kind string `json:"kind" jsonschema:"Jenis fail: 'image' (png/jpeg/webp/gif — dioptimumkan, strip EXIF; png/jpeg/webp dapat saiz thumbnail/medium/full), 'asset' (css/js/html/svg/woff2 — disimpan asli, scan-gated) atau 'pdf' (disimpan asli)"`
	Data string `json:"data" jsonschema:"Fail content base64-encoded. Imej ≤20MB input / ≤2MB stored; asset ≤20MB input / ≤2MB stored; pdf ≤20MB input / ≤2MB stored."`
}

func (s *Server) uploadMedia(
	ctx context.Context, _ *mcp.CallToolRequest, in uploadMediaIn,
) (*mcp.CallToolResult, mediaOut, error) {
	// Issue #307: resolve the owning account within the per-call scope.
	acc, err := s.accountAndScope(ctx, true)
	if err != nil {
		return nil, mediaOut{}, err
	}
	// Write gate: manage-or-admin role in the workspace owning the account.
	if err := s.requireWriteRoleIn(ctx, acc); err != nil {
		return nil, mediaOut{}, err
	}
	if s.deps.Store == nil {
		return nil, mediaOut{}, errors.New("media storage tidak dikonfigurasi")
	}
	if in.Data == "" {
		return nil, mediaOut{}, errors.New("data wajib diisi (base64)")
	}
	raw, err := base64.StdEncoding.DecodeString(in.Data)
	if err != nil {
		return nil, mediaOut{}, errors.New("data bukan base64 yang sah")
	}

	switch in.Kind {
	case "image":
		return s.uploadMediaImage(ctx, acc, raw)
	case "asset":
		return s.uploadMediaAsset(ctx, acc, raw)
	case "pdf":
		return s.uploadMediaPDF(ctx, acc, raw)
	default:
		return nil, mediaOut{}, errors.New("kind mesti 'image', 'asset' atau 'pdf'")
	}
}

// uploadMediaImage mirrors the dashboard POST /v1/rest/uploads/image: sniffed
// image allowlist, optimise (downscale + EXIF strip), store account-scoped,
// record a merchant media row. Images are never scan-gated, so they serve
// immediately.
func (s *Server) uploadMediaImage(ctx context.Context, acc string, raw []byte) (*mcp.CallToolResult, mediaOut, error) {
	if len(raw) == 0 {
		return nil, mediaOut{}, errors.New("fail kosong")
	}
	if len(raw) > objstore.MaxUploadBytes {
		return nil, mediaOut{}, fmt.Errorf("imej melebihi had %dMB", objstore.MaxUploadBytes>>20)
	}
	ext, ok := objstore.SniffImageType(raw)
	if !ok {
		return nil, mediaOut{}, errors.New("jenis imej tidak disokong (png/jpeg/webp/gif sahaja)")
	}
	// Issue #455: enforce the magic-byte (file signature) check — the sniffed
	// signature must match the declared image kind.
	if msg := objstore.MagicByteMismatch(objstore.KindRasterImage, "", "", ext); msg != "" {
		return nil, mediaOut{}, errors.New(msg)
	}
	mime := objstore.ImageUploadMIME[ext]
	stored, err := objstore.OptimizeImage(raw, ext)
	if err != nil {
		return nil, mediaOut{}, err
	}
	if len(stored) > objstore.MaxStoredBytes {
		return nil, mediaOut{}, errors.New("imej gagal dimampatkan bawah 2MB; cuba imej lain")
	}
	// Issue #384/#545: reserve ATOMICALLY before storing, then settle to the true
	// stored total (base + variants) once every object is written.
	key := objstore.BuildUploadKey(acc, ext)
	if err := s.reserveQuota(ctx, acc, int64(len(stored))); err != nil {
		return nil, mediaOut{}, err
	}
	if err := s.deps.Store.PutObject(ctx, key, mime, stored); err != nil {
		s.releaseQuota(ctx, acc, int64(len(stored)))
		return nil, mediaOut{}, fmt.Errorf("store: %w", err)
	}
	// Issue #382: generate + store thumbnail/medium/full variants for raster
	// images (png/jpeg/webp). GIFs are animated and stored as-is, so they get
	// no sized variants.
	sizes := map[string]string{}
	storedTotal := int64(len(stored))
	if objstore.IsSizedImage(mime) {
		for _, size := range objstore.ImageSizes {
			variant, verr := objstore.OptimizeImageSize(raw, ext, size)
			if verr != nil {
				continue
			}
			vkey := objstore.BuildVariantKey(key, size)
			if vkey == "" {
				continue
			}
			if verr := s.deps.Store.PutObject(ctx, vkey, "image/jpeg", variant); verr != nil {
				continue
			}
			storedTotal += int64(len(variant))
			sizes[string(size)] = s.deps.Store.PublicURL(vkey)
		}
	}
	s.settleQuota(ctx, acc, int64(len(stored)), storedTotal)
	// Issue #92: audit upload.image (best-effort).
	s.recordMCPMediaEvent(ctx, acc, "upload.image", map[string]any{
		"mime": mime, "out_bytes": len(stored),
	})
	out := s.mediaOutFrom(ctx, acc, key, mime, int64(len(stored)))
	out.Sizes = sizes
	return nil, out, nil
}

// uploadMediaAsset mirrors the dashboard POST /v1/rest/uploads/asset: sniffed
// code-asset allowlist, stored byte-for-byte, merchant media row, then the
// #364 deterministic scan + 'scanning' enqueue. Code assets only serve once
// the background scan flips them to 'ok'; otherwise fail-closed.
func (s *Server) uploadMediaAsset(ctx context.Context, acc string, raw []byte) (*mcp.CallToolResult, mediaOut, error) {
	if s.deps.Media == nil {
		return nil, mediaOut{}, errors.New("media metadata tidak tersedia")
	}
	if len(raw) == 0 {
		return nil, mediaOut{}, errors.New("fail kosong")
	}
	if len(raw) > objstore.MaxAssetUploadBytes {
		return nil, mediaOut{}, fmt.Errorf("fail melebihi had %dMB", objstore.MaxAssetUploadBytes>>20)
	}
	if len(raw) > objstore.MaxAssetBytes {
		return nil, mediaOut{}, fmt.Errorf("fail melebihi had %dMB", objstore.MaxAssetBytes>>20)
	}
	// Issue #384: reject an upload that would push the workspace over the 1GB
	// per-workspace storage quota.
	if err := s.workspaceQuotaExceeded(ctx, acc, int64(len(raw))); err != nil {
		return nil, mediaOut{}, err
	}
	ext, ok := objstore.SniffAssetType(raw)
	if !ok {
		return nil, mediaOut{}, errors.New("jenis fail tidak disokong (css/js/html/svg/woff2 sahaja)")
	}
	// Issue #455: enforce the magic-byte (file signature) check — the sniffed
	// signature must match the declared asset kind.
	if msg := objstore.MagicByteMismatch(objstore.KindAsset, "", "", ext); msg != "" {
		return nil, mediaOut{}, errors.New(msg)
	}
	mime := objstore.UploadAssetMIME[ext]
	// Issue #545: this MCP path had NO quota check at all.
	key := objstore.BuildUploadKey(acc, ext)
	if err := s.reserveQuota(ctx, acc, int64(len(raw))); err != nil {
		return nil, mediaOut{}, err
	}
	if err := s.deps.Store.PutObject(ctx, key, mime, raw); err != nil {
		s.releaseQuota(ctx, acc, int64(len(raw)))
		return nil, mediaOut{}, fmt.Errorf("store: %w", err)
	}
	// Issue #180: record the media row so list_media + the serving gate work.
	rec := &dbstore.Media{
		AccountID: acc, Key: key, URL: s.deps.Store.PublicURL(key),
		Mime: mime, Size: int64(len(raw)), Source: dbstore.MediaSourceMerchant,
	}
	if err := s.deps.Media.Create(ctx, rec); err != nil {
		return nil, mediaOut{}, fmt.Errorf("media: %w", err)
	}
	// Issue #92: audit upload.asset (best-effort).
	s.recordMCPMediaEvent(ctx, acc, "upload.asset", map[string]any{
		"mime": mime, "bytes": len(raw),
	})
	// Issue #364: code-asset scan gate (same logic as the REST asset handler).
	det := assetgate.DeterministicScan(raw, mime)
	var scanStatus, scanReason string
	switch {
	case det.Status == assetgate.StatusQuarantined:
		scanStatus = assetgate.StatusQuarantined
		scanReason = strings.Join(det.Reasons, "; ")
	case s.deps.AssetQueue != nil:
		scanStatus = assetgate.StatusScanning
		scanReason = "queued for AI review"
		_ = s.deps.Media.MarkScanning(ctx, acc, rec.ID, scanReason)
		if err := assetgate.Enqueue(ctx, s.deps.AssetQueue, assetgate.ScanJob{
			MediaID: rec.ID, Key: key, AccountID: acc, Mime: mime,
		}); err != nil {
			// Leave 'scanning' — the asset is not served until a successful
			// scan, so this is safe fail-closed behaviour.
		}
	default:
		scanStatus = assetgate.StatusPending
	}
	if scanStatus != "" {
		_ = s.deps.Media.SetScanStatus(ctx, acc, rec.ID, scanStatus, scanReason)
	}
	return nil, s.mediaOutFrom(ctx, acc, key, mime, int64(len(raw))), nil
}

// uploadMediaPDF mirrors the dashboard media-page upload for PDFs (issue
// #382): sniffed PDF allowlist, stored byte-for-byte under a bounded stored
// cap, merchant media row. PDFs are not code assets, so they are never
// scan-gated and serve immediately.
func (s *Server) uploadMediaPDF(ctx context.Context, acc string, raw []byte) (*mcp.CallToolResult, mediaOut, error) {
	if s.deps.Media == nil {
		return nil, mediaOut{}, errors.New("media metadata tidak tersedia")
	}
	if len(raw) == 0 {
		return nil, mediaOut{}, errors.New("fail kosong")
	}
	if len(raw) > objstore.MaxUploadBytes {
		return nil, mediaOut{}, fmt.Errorf("fail melebihi had %dMB", objstore.MaxUploadBytes>>20)
	}
	if len(raw) > objstore.MaxPDFBytes {
		return nil, mediaOut{}, fmt.Errorf("fail melebihi had %dMB", objstore.MaxPDFBytes>>20)
	}
	// Issue #384: reject an upload that would push the workspace over the 1GB
	// per-workspace storage quota.
	if err := s.workspaceQuotaExceeded(ctx, acc, int64(len(raw))); err != nil {
		return nil, mediaOut{}, err
	}
	ext, ok := objstore.SniffPDFType(raw)
	if !ok {
		return nil, mediaOut{}, errors.New("jenis fail tidak disokong (pdf sahaja)")
	}
	// Issue #455: enforce the magic-byte (file signature) check — the sniffed
	// signature must match the declared PDF kind.
	if msg := objstore.MagicByteMismatch(objstore.KindPDF, "", "", ext); msg != "" {
		return nil, mediaOut{}, errors.New(msg)
	}
	mime := objstore.UploadPDFMIME[ext]
	// Issue #545: this MCP path had NO quota check at all.
	key := objstore.BuildUploadKey(acc, ext)
	if err := s.reserveQuota(ctx, acc, int64(len(raw))); err != nil {
		return nil, mediaOut{}, err
	}
	if err := s.deps.Store.PutObject(ctx, key, mime, raw); err != nil {
		s.releaseQuota(ctx, acc, int64(len(raw)))
		return nil, mediaOut{}, fmt.Errorf("store: %w", err)
	}
	// Issue #383: auto-generate a first-page preview thumbnail with
	// Ghostscript (bounded render) and store it alongside the PDF under the
	// same account-scoped key namespace. Best-effort: if the render fails or
	// gs is unavailable, the PDF still uploads and the grid falls back to
	// the generic PDF tile.
	previewURL := ""
	storedTotal := int64(len(raw))
	if preview, perr := objstore.RenderPDFPreview(raw); perr == nil && len(preview) > 0 {
		if pkey := objstore.BuildPDFPreviewKey(key); pkey != "" {
			if perr := s.deps.Store.PutObject(ctx, pkey, "image/jpeg", preview); perr == nil {
				previewURL = s.deps.Store.PublicURL(pkey)
				storedTotal += int64(len(preview))
			}
		}
	}
	// Issue #545: settle the reservation with the true total (PDF + preview).
	s.settleQuota(ctx, acc, int64(len(raw)), storedTotal)
	// Issue #180: record the media row so list_media works.
	rec := &dbstore.Media{
		AccountID: acc, Key: key, URL: s.deps.Store.PublicURL(key),
		Mime: mime, Size: storedTotal, Source: dbstore.MediaSourceMerchant,
	}
	if err := s.deps.Media.Create(ctx, rec); err != nil {
		return nil, mediaOut{}, fmt.Errorf("media: %w", err)
	}
	// Issue #92: audit upload.pdf (best-effort).
	s.recordMCPMediaEvent(ctx, acc, "upload.pdf", map[string]any{
		"mime": mime, "bytes": len(raw),
	})
	out := s.mediaOutFrom(ctx, acc, key, mime, int64(len(raw)))
	out.Preview = previewURL
	return nil, out, nil
}

// mediaOutFromRow builds a mediaOut from a stored media row, deriving the
// generated image-size variant URLs from the object key (issue #382).
func (s *Server) mediaOutFromRow(m *dbstore.Media) mediaOut {
	out := mediaOut{
		ID: m.ID, FormID: m.FormID, Key: m.Key, URL: m.URL,
		Mime: m.Mime, Size: m.Size, Source: m.Source,
		ScanStatus: m.ScanStatus, ScanReason: m.ScanReason,
		CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339),
	}
	if s.deps.Store != nil && objstore.IsSizedImage(m.Mime) {
		sizes := map[string]string{}
		for _, size := range objstore.ImageSizes {
			if vkey := objstore.BuildVariantKey(m.Key, size); vkey != "" {
				sizes[string(size)] = s.deps.Store.PublicURL(vkey)
			}
		}
		if len(sizes) > 0 {
			out.Sizes = sizes
		}
	}
	// Issue #383: expose the auto-generated PDF preview thumbnail URL,
	// derived from the object key (no extra column needed).
	if s.deps.Store != nil && m.Mime == "application/pdf" {
		if pkey := objstore.BuildPDFPreviewKey(m.Key); pkey != "" {
			out.Preview = s.deps.Store.PublicURL(pkey)
		}
	}
	return out
}

// mediaOutFrom builds a mediaOut for a freshly stored object, best-effort
// reading its scan status from the media row.
func (s *Server) mediaOutFrom(ctx context.Context, acc, key, mime string, size int64) mediaOut {
	out := mediaOut{
		Key: key, URL: s.deps.Store.PublicURL(key), Mime: mime, Size: size,
		Source:    dbstore.MediaSourceMerchant,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if s.deps.Media != nil {
		if m, err := s.deps.Media.ByKey(ctx, acc, key); err == nil && m != nil {
			out.ID = m.ID
			out.ScanStatus = m.ScanStatus
			out.ScanReason = m.ScanReason
		} else if objstore.IsCodeAsset(mime) {
			// A code asset with no row is not proven scanned → never served.
			out.ScanStatus = assetgate.StatusPending
		}
	}
	return out
}

// ── Tong sampah media: delete / list / restore / purge ─────────────────────
// Parity for the media trash routes (GET /v1/rest/media/trash, POST
// /v1/rest/media/{id}/restore, DELETE /v1/rest/media/{id}/purge).
//
// The quota rule these four tools share: trash is quota-NEUTRAL (the bytes stay
// in the bucket and keep counting against the 1GB cap), and purge is the ONLY
// place the bytes leave the bucket and the counter is released. That is the
// decision this card made, stated on MediaStore.Purge and asserted by the tests.

// trashedMediaOut is one row of the media bin listing.
type trashedMediaOut struct {
	ID            string `json:"id"`
	Key           string `json:"key"`
	URL           string `json:"url"`
	Mime          string `json:"mime"`
	Size          int64  `json:"size"`
	FormID        string `json:"form_id,omitempty"`
	DeletedAt     string `json:"deleted_at"`
	ObjectPresent bool   `json:"object_present"`
}

// deleteMediaMCP moves a media row to the bin, matching
// DELETE /v1/rest/media/{id}. The object is deliberately NOT removed: the row
// and the bytes have independent lives now, so the bin is restorable and the
// public URL keeps working until a purge.
func (s *Server) deleteMediaMCP(
	ctx context.Context, _ *mcp.CallToolRequest,
	in struct {
		MediaID string `json:"media_id" jsonschema:"ID media (dari list_media)"`
	},
) (*mcp.CallToolResult, map[string]any, error) {
	if in.MediaID == "" {
		return nil, nil, errors.New("media_id wajib")
	}
	acc, err := s.requireMediaWrite(ctx, in.MediaID)
	if err != nil {
		return nil, nil, err
	}
	key, size, ok, err := s.deps.Media.SoftDelete(ctx, acc, in.MediaID)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, map[string]any{
			"status":   "not_found",
			"error":    "media tidak dijumpai dalam senarai aktif (mungkin sudah dalam tong sampah)",
			"media_id": in.MediaID,
		}, nil
	}
	return nil, map[string]any{
		"status": "deleted", "media_id": in.MediaID, "key": key, "size": size,
		"bytes_released": false, "quota_released": false,
		"note": "objek masih di storan dan masih dikira terhadap had 1GB; " +
			"guna purge_media untuk memusnahkannya secara kekal",
	}, nil
}

// listDeletedMedia is the media bin across the caller's accessible accounts.
func (s *Server) listDeletedMedia(
	ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn,
) (*mcp.CallToolResult, *listOutput[trashedMediaOut], error) {
	if s.deps.Media == nil {
		return nil, nil, errors.New("media tidak tersedia")
	}
	accs, err := s.accountScopes(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	items, err := s.deps.Media.ListTrashed(ctx, accs)
	if err != nil {
		return nil, nil, err
	}
	out := make([]trashedMediaOut, 0, len(items))
	for _, m := range items {
		row := trashedMediaOut{
			ID: m.ID, Key: m.Key, URL: m.URL, Mime: m.Mime, Size: m.Size,
			FormID: m.FormID, ObjectPresent: true,
		}
		if m.DeletedAt != nil {
			row.DeletedAt = m.DeletedAt.UTC().Format(time.RFC3339)
		}
		// Best-effort, exactly like the REST bin: an unreachable store is not
		// the same as "the object is gone", so a failed probe stays optimistic
		// and the restore attempt is where it is answered for real.
		if s.deps.Store != nil {
			if present, perr := s.deps.Store.ObjectExists(ctx, m.Key); perr == nil {
				row.ObjectPresent = present
			}
		}
		out = append(out, row)
	}
	return nil, newListOut(out), nil
}

// restoreMediaMCP brings a trashed object back. The SAME URL keeps working —
// but only if the bytes are still in the store: a row whose object was removed
// (an operator, a bucket lifecycle rule) is REFUSED rather than restored into a
// URL that 404s. Quota is untouched; it was never released at trash time.
func (s *Server) restoreMediaMCP(
	ctx context.Context, _ *mcp.CallToolRequest,
	in struct {
		MediaID string `json:"media_id" jsonschema:"ID media dalam tong sampah (dari list_deleted_media)"`
	},
) (*mcp.CallToolResult, map[string]any, error) {
	if in.MediaID == "" {
		return nil, nil, errors.New("media_id wajib")
	}
	acc, err := s.requireTrashedMediaWrite(ctx, in.MediaID)
	if err != nil {
		return nil, nil, err
	}
	row, err := s.deps.Media.GetTrashed(ctx, acc, in.MediaID)
	if err != nil {
		return nil, nil, err
	}
	if row == nil {
		return nil, map[string]any{
			"status":   "not_found",
			"error":    "media tidak dijumpai dalam tong sampah",
			"media_id": in.MediaID,
		}, nil
	}
	if s.deps.Store == nil {
		return nil, nil, errors.New("media storage tidak dikonfigurasi")
	}
	present, perr := s.deps.Store.ObjectExists(ctx, row.Key)
	if perr != nil {
		return nil, nil, fmt.Errorf("storan tidak dapat disemak: %w", perr)
	}
	if !present {
		return nil, map[string]any{
			"status": "refused",
			"error": "fail media ini sudah tiada dalam storan — ia tidak boleh dipulihkan " +
				"(memulihkannya hanya akan memberi URL yang 404)",
			"media_id": in.MediaID, "key": row.Key,
		}, nil
	}
	m, ok, err := s.deps.Media.Restore(ctx, acc, in.MediaID)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, errors.New("media tidak dijumpai dalam tong sampah")
	}
	return nil, map[string]any{
		"status": "restored", "media_id": in.MediaID, "url": m.URL,
	}, nil
}

// purgeMediaMCP is the ONLY path that destroys a media object, and the only one
// that gives its bytes back to the workspace quota.
//
// Order, matching the REST handler: the row is deleted first (scoped to the bin
// and to the caller's account), so a store failure cannot leave a LIVE row whose
// object vanished — the worst case is a deleted row whose bytes linger, which a
// bucket sweep can find, and which can never serve a broken URL. The object and
// every derived variant (sized images, PDF preview) are then removed.
//
// The quota release happens inside MediaStore.Purge's transaction; there is NO
// release call here, because releasing the same bytes twice would eat into the
// account's OTHER media.
func (s *Server) purgeMediaMCP(
	ctx context.Context, _ *mcp.CallToolRequest,
	in struct {
		MediaID string `json:"media_id" jsonschema:"ID media dalam tong sampah (dari list_deleted_media)"`
	},
) (*mcp.CallToolResult, map[string]any, error) {
	if in.MediaID == "" {
		return nil, nil, errors.New("media_id wajib")
	}
	acc, err := s.requireTrashedMediaWrite(ctx, in.MediaID)
	if err != nil {
		return nil, nil, err
	}
	key, size, ok, err := s.deps.Media.Purge(ctx, acc, in.MediaID)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, map[string]any{
			"status":   "not_found",
			"error":    "media tidak dijumpai dalam tong sampah (hantar ke bin dulu guna delete_media)",
			"media_id": in.MediaID,
		}, nil
	}
	// Best-effort: the row is already gone, so a storage failure must not fail
	// the request — it is logged so a sweep can find the orphan.
	if s.deps.Store != nil {
		for _, k := range mediaObjectKeysMCP(key) {
			if derr := s.deps.Store.DeleteObject(ctx, k); derr != nil {
				slog.Warn("mcp_media_purge_object", "err", derr, "key", k)
			}
		}
	}
	return nil, map[string]any{
		"status": "purged", "media_id": in.MediaID, "key": key,
		"bytes_released": size, "quota_released": true,
	}, nil
}

// mediaObjectKeysMCP is httpapi.mediaObjectKeys' counterpart in this package:
// the base key plus every derived key the upload pipeline may have written
// (sized image variants, PDF preview). Purging only the base key would leave
// those bytes in the bucket forever, invisible to the quota.
func mediaObjectKeysMCP(key string) []string {
	keys := []string{key}
	for _, size := range objstore.ImageSizes {
		if vkey := objstore.BuildVariantKey(key, size); vkey != "" {
			keys = append(keys, vkey)
		}
	}
	if pkey := objstore.BuildPDFPreviewKey(key); pkey != "" {
		keys = append(keys, pkey)
	}
	return keys
}

// recordMCPMediaEvent appends an upload audit event (issue #92, best-effort).
func (s *Server) recordMCPMediaEvent(ctx context.Context, accountID, event string, props map[string]any) {
	if s.deps.Events == nil {
		return
	}
	id := IdentityFromContext(ctx)
	if id == nil {
		return
	}
	if err := s.deps.Events.Record(ctx, accountID, id.UserID, event, props, "", ""); err != nil {
		// best-effort — a failed event write must not fail the upload
		return
	}
}

// workspaceQuotaExceeded is the MCP equivalent of the REST pre-flight check
// (issue #384). It is a cheap read-only guard; the ATOMIC enforcement is
// reserveQuota below (issue #545).
func (s *Server) workspaceQuotaExceeded(ctx context.Context, accountID string, additional int64) error {
	if s.deps.Media == nil {
		return nil
	}
	current, err := s.deps.Media.TotalBytes(ctx, accountID)
	if err != nil {
		return nil // best-effort — a usage read failure must not block uploads
	}
	if objstore.ExceedsWorkspaceQuota(current, additional) {
		return errors.New("had storan workspace dicapai (1GB)")
	}
	return nil
}

// reserveQuota atomically reserves `bytes` against the workspace storage cap
// (issue #545). Returns an error when the cap would be exceeded. On success the
// caller owns the reservation and must settle or release it.
func (s *Server) reserveQuota(ctx context.Context, accountID string, bytes int64) error {
	if s.deps.Media == nil || accountID == "" {
		return nil
	}
	if err := s.deps.Media.ReserveQuota(ctx, accountID, bytes); err != nil {
		if errors.Is(err, dbstore.ErrQuotaExceeded) {
			return errors.New("had storan workspace dicapai (1GB)")
		}
		return fmt.Errorf("quota check: %w", err)
	}
	return nil
}

// releaseQuota gives back a reservation whose object was not stored.
func (s *Server) releaseQuota(ctx context.Context, accountID string, bytes int64) {
	if s.deps.Media == nil || accountID == "" || bytes <= 0 {
		return
	}
	if err := s.deps.Media.ReleaseQuota(ctx, accountID, bytes); err != nil {
		slog.Warn("mcp_media_quota_release", "err", err, "account", accountID, "bytes", bytes)
	}
}

// settleQuota records the true stored total for an upload (base + variants +
// preview) against the reservation (issue #545).
func (s *Server) settleQuota(ctx context.Context, accountID string, reserved, actual int64) {
	if s.deps.Media == nil || accountID == "" || actual <= 0 {
		return
	}
	if err := s.deps.Media.SettleQuotaBytes(ctx, accountID, reserved, actual); err != nil {
		slog.Warn("mcp_media_quota_settle", "err", err, "account", accountID,
			"reserved", reserved, "actual", actual)
	}
}
