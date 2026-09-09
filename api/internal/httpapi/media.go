// Media library REST handlers (issue #180).
//
// GET /v1/rest/media lists the merchant's uploaded media (account-scoped,
// paginated, newest first), optionally filtered by ?form=<id> and
// ?source=merchant|customer. DELETE /v1/rest/media/{id} soft-deletes the row
// and removes the object from MinIO.
package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/objstore"
)

// restMedia is the JSON shape returned by the media list endpoint.
type restMedia struct {
	ID        string `json:"id"`
	FormID    string `json:"form_id,omitempty"`
	Key       string `json:"key"`
	URL       string `json:"url"`
	Mime      string `json:"mime"`
	Size      int64  `json:"size"`
	Source    string `json:"source"`
	// ScanStatus is the #364 scan status ('pending' until the scan engine
	// lands). Exposed for the media UI + ops to see unscanned/quarantined
	// code assets; only 'ok' is served (issue #364).
	ScanStatus string `json:"scan_status"`
	// ScanReason is why a code asset was flagged/quarantined (issue #364).
	ScanReason string `json:"scan_reason,omitempty"`
	// Sizes maps a generated image size name (thumbnail/medium/full) to its
	// public URL for raster images (issue #382). Empty for non-images and
	// GIFs. The media grid renders the thumbnail; the detail modal offers
	// all sizes.
	Sizes     map[string]string `json:"sizes,omitempty"`
	// Preview is the auto-generated first-page thumbnail URL for a PDF
	// (issue #383), derived from the object key. Empty when no preview was
	// generated (e.g. Ghostscript unavailable at upload time). The media grid
	// renders this instead of the generic PDF tile when present.
	Preview   string            `json:"preview,omitempty"`
	CreatedAt string            `json:"created_at"`
}

// listMedia handles GET /v1/rest/media — account-scoped, paginated list of
// the merchant's uploaded media, newest first. Optional ?form=<id> and
// ?source=merchant|customer filters. Returns {"items":[...],"page":N,
// "limit":N,"total":N}.
func (h *RESTHandlers) listMedia(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if h.Media == nil {
		respond(w, http.StatusOK, map[string]any{"items": []restMedia{}, "page": 1, "limit": 20, "total": 0})
		return
	}
	page, limit := parsePageLimit(r)
	formID := r.URL.Query().Get("form")
	source := r.URL.Query().Get("source")
	if source != "" && source != dbstore.MediaSourceMerchant && source != dbstore.MediaSourceCustomer {
		writeErr(w, http.StatusBadRequest, "source mesti merchant|customer")
		return
	}
	items, total, err := h.Media.ListPage(r.Context(), actx.Account.ID, formID, source, page, limit)
	if err != nil {
		slog.Error("media_list", "err", err, "account", actx.Account.ID)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]restMedia, 0, len(items))
	for _, m := range items {
		rm := restMedia{
			ID: m.ID, FormID: m.FormID, Key: m.Key, URL: m.URL,
			Mime: m.Mime, Size: m.Size, Source: m.Source,
			ScanStatus: m.ScanStatus, ScanReason: m.ScanReason,
			CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339),
		}
		// Issue #382: expose the generated thumbnail/medium/full variant URLs
		// for raster images. They are derived from the object key (the
		// variants share the full-size key's namespace), so no extra column is
		// needed. h.Store may be nil in tests — then sizes are omitted.
		if h.Store != nil && isSizedImage(m.Mime) {
			sizes := map[string]string{}
			for _, size := range objstore.ImageSizes {
				if vkey := buildVariantKey(m.Key, size); vkey != "" {
					sizes[string(size)] = h.Store.PublicURL(vkey)
				}
			}
			if len(sizes) > 0 {
				rm.Sizes = sizes
			}
		}
		// Issue #383: expose the auto-generated PDF preview thumbnail URL,
		// derived from the object key (no extra column needed).
		if h.Store != nil && m.Mime == "application/pdf" {
			if pkey := buildPDFPreviewKey(m.Key); pkey != "" {
				rm.Preview = h.Store.PublicURL(pkey)
			}
		}
		out = append(out, rm)
	}
	respond(w, http.StatusOK, map[string]any{"items": out, "page": page, "limit": limit, "total": total})
}

// deleteMedia handles DELETE /v1/rest/media/{id} — soft-deletes the media row
// and removes the object from MinIO. Account-scoped; a foreign or already
// deleted id is a 404.
func (h *RESTHandlers) deleteMedia(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	if h.Media == nil {
		writeErr(w, http.StatusServiceUnavailable, "media tidak dikonfigurasi")
		return
	}
	key, ok, err := h.Media.SoftDelete(r.Context(), actx.Account.ID, r.PathValue("id"))
	if err != nil {
		slog.Error("media_delete", "err", err, "account", actx.Account.ID, "id", r.PathValue("id"))
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "media tidak dijumpai")
		return
	}
	// Remove the object from MinIO (best-effort; the row is already
	// soft-deleted so a storage failure must not fail the request).
	if h.Store != nil {
		if err := h.Store.DeleteObject(r.Context(), key); err != nil {
			slog.Warn("media_delete_object", "err", err, "key", key)
		}
	}
	// Issue #92: audit media delete.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "media.deleted", map[string]any{"media_id": r.PathValue("id"), "key": key})
	respond(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// recordMedia records an uploaded object in the media library (issue #180).
// It is best-effort: a metadata write failure is logged but never fails the
// upload. It returns the created media row (with its id) on success, nil on
// failure, so the caller can queue a background scan against it (issue #364).
// formID is "" for merchant uploads not tied to a form; customer uploads (a
// dependency) will pass the form id + customer source.
func (h *RESTHandlers) recordMedia(r *http.Request, actx *AccountCtx, key, mime string, size int64, source, formID string) *dbstore.Media {
	if h.Media == nil {
		return nil
	}
	m := &dbstore.Media{
		AccountID: actx.Account.ID, FormID: formID, Key: key,
		URL: h.Store.PublicURL(key), Mime: mime, Size: size, Source: source,
	}
	if err := h.Media.Create(r.Context(), m); err != nil {
		slog.Warn("media_record", "err", err, "key", key, "account", actx.Account.ID)
		return nil
	}
	return m
}
