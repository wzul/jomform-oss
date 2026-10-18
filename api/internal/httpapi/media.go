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

	"github.com/wzul/jomform/api/internal/cdnpurge"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/objstore"
)

// restMedia is the JSON shape returned by the media list endpoint.
type restMedia struct {
	ID     string `json:"id"`
	FormID string `json:"form_id,omitempty"`
	Key    string `json:"key"`
	URL    string `json:"url"`
	Mime   string `json:"mime"`
	Size   int64  `json:"size"`
	Source string `json:"source"`
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
	Sizes map[string]string `json:"sizes,omitempty"`
	// Preview is the auto-generated first-page thumbnail URL for a PDF
	// (issue #383), derived from the object key. Empty when no preview was
	// generated (e.g. Ghostscript unavailable at upload time). The media grid
	// renders this instead of the generic PDF tile when present.
	Preview   string `json:"preview,omitempty"`
	CreatedAt string `json:"created_at"`
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
		// needed — BUT a derived key is not a written object, and this is where
		// the library used to advertise variants that were never generated
		// (t_466ff771): uploadAvatar writes no variants, so the account's own
		// avatar row handed out three URLs that all 404'd. The store is asked
		// what exists and only confirmed variants are advertised; the media grid
		// then falls back to the base url, which is real. The store may be
		// unconfigured in tests or when S3_* is unset — then sizes are omitted
		// (storeUnavailable also catches a nil *ObjectStore behind the
		// interface).
		if !storeUnavailable(h.Store) && isSizedImage(m.Mime) {
			rm.Sizes = objstore.VariantURLs(r.Context(), m.Key, h.Store, h.Store.PublicURL)
		}
		// Issue #383: expose the auto-generated PDF preview thumbnail URL,
		// derived from the object key (no extra column needed). Same rule: the
		// render is best-effort (Ghostscript missing, bounded timeout) and a
		// preview that was never written must not be advertised — the grid
		// prefers `preview` over the base url, so a dead one shows a broken tile.
		if !storeUnavailable(h.Store) && m.Mime == "application/pdf" {
			if pkey := buildPDFPreviewKey(m.Key); pkey != "" {
				rm.Preview = objstore.PublicURLIfPresent(r.Context(), pkey, h.Store, h.Store.PublicURL)
			}
		}
		out = append(out, rm)
	}
	// Issue #384/#545: surface the per-workspace storage usage so the merchant
	// knows their remaining quota. Best-effort — a usage read failure never fails
	// the list. Read from the quota COUNTER (the enforced figure) so the number
	// shown matches what is actually enforced; fall back to the media sum.
	usage := map[string]any{"limit": objstore.MaxWorkspaceBytes}
	if h.Media != nil {
		if used, err := h.Media.UsedBytes(r.Context(), actx.Account.ID); err == nil {
			usage["used"] = used
		} else if used, err := h.Media.TotalBytes(r.Context(), actx.Account.ID); err == nil {
			usage["used"] = used
		}
	}
	respond(w, http.StatusOK, map[string]any{"items": out, "page": page, "limit": limit, "total": total, "usage": usage})
}

// deleteMedia handles DELETE /v1/rest/media/{id} — moves the media row to the
// bin (soft delete). Account-scoped; a foreign or already-trashed id is a 404.
//
// THE BYTES ARE NOT TOUCHED HERE, and that is the card's whole point. This
// handler used to call Store.DeleteObject right after marking the row, so the
// "soft delete" destroyed the object: the row said "in the trash" while the
// bytes were already gone, and a restore could only ever have produced a row
// pointing at nothing. The object now lives until the PURGE
// (DELETE /v1/rest/media/{id}/purge), which is where the deletion belongs.
//
// The QUOTA is not touched here either, and that is the second half of the same
// decision: the object still occupies the bucket, so its bytes still count
// against the 1GB cap. Releasing at soft-delete time would make the bin free
// storage on the one number that is a billing boundary. Quota is released on
// purge. The rule is stated in full on MediaStore.Purge; the behaviour is
// asserted by TestMediaTrashKeepsTheObjectAndTheQuotaBite below.
func (h *RESTHandlers) deleteMedia(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	if h.Media == nil {
		writeErr(w, http.StatusServiceUnavailable, "media tidak dikonfigurasi")
		return
	}
	key, _, ok, err := h.Media.SoftDelete(r.Context(), actx.Account.ID, r.PathValue("id"))
	if err != nil {
		slog.Error("media_delete", "err", err, "account", actx.Account.ID, "id", r.PathValue("id"))
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "media tidak dijumpai")
		return
	}
	// Issue #92: audit media delete.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "media.deleted", map[string]any{"media_id": r.PathValue("id"), "key": key})
	respond(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// restTrashedMedia is a bin row: what the trash view renders, plus the
// pre-computed answer to "may this be destroyed yet".
type restTrashedMedia struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	URL    string `json:"url"`
	Mime   string `json:"mime"`
	Size   int64  `json:"size"`
	FormID string `json:"form_id,omitempty"`
	// DeletedAt is when the object entered the bin (RFC3339).
	DeletedAt string `json:"deleted_at"`
	// ObjectPresent reports whether the bytes are STILL IN THE STORE. It is the
	// honest half of a restore: after this card the row and the object have
	// independent lives (an operator or a lifecycle rule can remove the bytes),
	// so the UI can say "the file itself is gone" instead of restoring a URL
	// that 404s. False + a restore attempt is refused with 409.
	ObjectPresent bool `json:"object_present"`
}

// listTrashedMedia: GET /v1/rest/media/trash
//
// The bin's read side, for the workspace(s) the caller selected. It is a
// DEDICATED store query (MediaStore.ListTrashed), deliberately not a relaxed
// filter on the live list: loosening the live query's `deleted_at IS NULL` would
// expose trashed rows to every ordinary read (the issue #98 regression class).
//
// NOTE the route literal: `/v1/rest/media/trash` is registered before
// `/v1/rest/media/{id}`, and a literal segment wins in net/http's pattern
// matching, so a trash read is not shadowed by the per-id route.
func (h *RESTHandlers) listTrashedMedia(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if h.Media == nil {
		respond(w, http.StatusOK, []restTrashedMedia{})
		return
	}
	scope, ok := h.scopeOr403(w, r, actx)
	if !ok {
		return
	}
	items, err := h.Media.ListTrashed(r.Context(), scope.Accounts)
	if err != nil {
		slog.Error("media_trash_list", "err", err, "account", actx.Account.ID)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]restTrashedMedia, 0, len(items))
	for _, m := range items {
		row := restTrashedMedia{
			ID: m.ID, Key: m.Key, URL: m.URL, Mime: m.Mime, Size: m.Size,
			FormID: m.FormID, ObjectPresent: true,
		}
		if m.DeletedAt != nil {
			row.DeletedAt = m.DeletedAt.UTC().Format(time.RFC3339)
		}
		// Best-effort: a store that cannot be reached is not the same as "the
		// object is gone", so a failed probe leaves the flag optimistic and the
		// restore attempt is the place that has to answer for real.
		if !storeUnavailable(h.Store) {
			if present, perr := h.Store.ObjectExists(r.Context(), m.Key); perr == nil {
				row.ObjectPresent = present
			}
		}
		out = append(out, row)
	}
	// A BARE ARRAY, matching GET /v1/rest/forms/trash and
	// GET /v1/rest/products/trash. The trash family shares one shape so a reader
	// (and the dashboard) can treat the three bins the same way; only the LIVE
	// media list is a paginated envelope, because only it is paginated.
	respond(w, http.StatusOK, out)
}

// restoreMedia: POST /v1/rest/media/{id}/restore
//
// Brings a trashed object back: deleted_at is cleared and the SAME URL keeps
// working. No confirmation — it destroys nothing and a second delete undoes it.
//
// The bytes must still be in the store. After this card a trashed row and its
// object have INDEPENDENT lives (an operator, a bucket lifecycle rule or an
// earlier failed deploy can remove the object while the row sits in the bin), so
// the handler asks ObjectExists before it promises anything. When the object is
// GONE the restore is refused with 409 and NOTHING is changed: putting the row
// back would return a broken URL to the library, and the call would look like a
// success while the file is unfetchable. The merchant is told what is missing
// (the bytes) instead of being handed a row that 404s.
//
// Quota is untouched: the bytes were never released at soft-delete time, so
// trash → restore is quota-neutral by construction (MediaStore.Restore).
func (h *RESTHandlers) restoreMedia(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	if h.Media == nil {
		writeErr(w, http.StatusServiceUnavailable, "media tidak dikonfigurasi")
		return
	}
	id := r.PathValue("id")
	// Read the trashed row FIRST: GetTrashed is scoped to the bin and to the
	// caller's account, so this cannot be used to probe or restore a live row.
	row, err := h.Media.GetTrashed(r.Context(), actx.Account.ID, id)
	if err != nil {
		slog.Error("media_restore_read", "err", err, "account", actx.Account.ID, "id", id)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if row == nil {
		writeErr(w, http.StatusNotFound, "media tidak dijumpai")
		return
	}
	// The bytes must still be there. ObjectExists answers (false, nil) for
	// "gone" and an error for "could not tell" — both refuse, because promising
	// a URL we have not seen is the failure this check exists to prevent.
	if storeUnavailable(h.Store) {
		writeErr(w, http.StatusServiceUnavailable, "media storage tidak dikonfigurasi")
		return
	}
	present, perr := h.Store.ObjectExists(r.Context(), row.Key)
	if perr != nil {
		slog.Error("media_restore_head", "err", perr, "key", row.Key)
		writeErr(w, http.StatusServiceUnavailable, "storan tidak dapat disemak")
		return
	}
	if !present {
		writeErr(w, http.StatusConflict,
			"fail media ini sudah tiada dalam storan — ia tidak boleh dipulihkan")
		return
	}
	m, ok, err := h.Media.Restore(r.Context(), actx.Account.ID, id)
	if err != nil {
		slog.Error("media_restore", "err", err, "account", actx.Account.ID, "id", id)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "media tidak dijumpai")
		return
	}
	h.recordEvent(r, actx, h.sessionOwnerID(r), "media.restored", map[string]any{"media_id": id, "key": m.Key})
	respond(w, http.StatusOK, map[string]string{"status": "restored", "media_id": id, "url": m.URL})
}

// purgeMedia: DELETE /v1/rest/media/{id}/purge
//
// Destroys a TRASHED object for good, and THIS is where the bytes go. The order
// is deliberate:
//
//  1. the row is read from the bin (scoped to the caller's account),
//  2. the row is DELETED first, so a store failure below cannot leave a live row
//     whose object vanished — the worst case is a deleted row whose bytes linger,
//     which is recoverable (the bucket is the source of truth for a sweep) and
//     never serves a broken URL,
//  3. the object and its derived variants are removed from the store,
//  4. the bytes are released from the 1GB quota — the release that soft delete
//     used to do, moved here because this is the moment the storage is actually
//     given back.
//
// A live (non-trashed) row is a 404: the bin is the only way in, so a mistyped
// call cannot destroy bytes that are still being served.
func (h *RESTHandlers) purgeMedia(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	if h.Media == nil {
		writeErr(w, http.StatusServiceUnavailable, "media tidak dikonfigurasi")
		return
	}
	id := r.PathValue("id")
	key, _, ok, err := h.Media.Purge(r.Context(), actx.Account.ID, id)
	if err != nil {
		slog.Error("media_purge", "err", err, "account", actx.Account.ID, "id", id)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "media tidak dijumpai")
		return
	}
	// Remove the object and every derived variant (sized images + PDF preview)
	// so the purge leaves nothing behind in the bucket. Best-effort: the row is
	// already gone, so a storage failure must not fail the request — it is
	// logged so a sweep can find the orphan.
	var objectURLs []string
	if !storeUnavailable(h.Store) {
		for _, k := range mediaObjectKeys(key) {
			if err := h.Store.DeleteObject(r.Context(), k); err != nil {
				slog.Warn("media_purge_object", "err", err, "key", k)
			}
		}
		objectURLs = mediaObjectURLs(h.Store, key)
	}
	// The bytes leave the bucket above; the EDGE copy leaves the CDN here, in
	// the same request.
	//
	// This ordering is deliberate: the object is deleted FIRST, so a request that
	// arrives between the two steps can only miss the origin (404) — never
	// re-populate the CDN with the bytes we just destroyed. The reverse order
	// would leave a window where the edge is cleared and then instantly filled
	// again from an object that still exists.
	//
	// The object's URL and every derived variant URL are sent as prefixes, so
	// every query variant of the same object goes with them — a page, an embed or
	// a tracking link that appends `?something` creates a separate CDN entry, and
	// those queries are not enumerable from here (see internal/cdnpurge).
	//
	// A purge that is not configured, or that the CDN refuses, does NOT fail this
	// response: the row and the bytes are already gone and no error can bring
	// them back. It is NAMED in the response instead (`edge`), together with the
	// freshness window that then bounds the damage, because a silent skip would
	// leave an operator believing the edge was cleared.
	edge := cdnpurge.PurgeObjectURLs(r.Context(), objectURLs)
	if edge.Err != nil {
		slog.Warn("media_purge_edge", "err", edge.Err, "key", key, "status", edge.Status)
	}
	// Two things are deliberately absent here:
	//
	//   - a quota release. MediaStore.Purge releases the counter in the SAME
	//     transaction as the row delete, so releasing it again from the handler
	//     would subtract the same bytes twice and eat into the account's OTHER
	//     media (clamped at zero, so it would look like a successful purge);
	//   - a silent skip of the edge purge. The response and this audit event both
	//     carry the outcome, so an operator reading the activity log later can
	//     tell a purge that cleared the CDN from one that did not.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "media.purged",
		map[string]any{"media_id": id, "key": key, "edge": edge.Status})
	out := map[string]any{"status": "purged", "media_id": id, "key": key}
	for k, v := range edge.Fields() {
		out[k] = v
	}
	if edge.Status != cdnpurge.StatusPurged {
		// Name the residual window: how long the CDN may still answer from its
		// own copy now that no purge removed it.
		out["edge_max_age_s"] = mediaCacheMaxAgeSeconds
	}
	respond(w, http.StatusOK, out)
}

// mediaObjectURLs is every caller-visible URL a purge invalidates: the base
// object plus each derived variant, as the store advertises them. They are the
// URLs a form, a product or the media grid actually renders, so they are the
// ones the CDN holds.
func mediaObjectURLs(store ObjectStoreIface, key string) []string {
	keys := mediaObjectKeys(key)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if u := store.PublicURL(k); u != "" {
			out = append(out, u)
		}
	}
	return out
}

// mediaObjectKeys returns the base object key plus every derived key the upload
// pipeline may have written for it: the sized image variants (thumbnail /
// medium / full, issue #382) and the PDF preview (issue #383). Purging only the
// base key would leave those bytes in the bucket forever — invisible to the
// quota, which is why the sweep has to know all of them.
func mediaObjectKeys(key string) []string {
	keys := []string{key}
	for _, size := range objstore.ImageSizes {
		if vkey := buildVariantKey(key, size); vkey != "" {
			keys = append(keys, vkey)
		}
	}
	if pkey := buildPDFPreviewKey(key); pkey != "" {
		keys = append(keys, pkey)
	}
	return keys
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
