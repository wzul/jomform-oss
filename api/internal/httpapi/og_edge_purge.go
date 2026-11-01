package httpapi

// The EDGE half of a form's generated public artefacts: /og/{slug}.png and
// /qr/{slug}.png.
//
// Both are public, cacheable URLs the API renders per form. The card draws the
// form's TITLE, its workspace name and the request host into a PNG; the QR PNG
// encodes the form's public URL and therefore discloses its EXISTENCE at that
// slug. The status gates in handleOGImage and handleQRImage made the ORIGIN
// honest (draft -> 404 with the unknown-slug body, private -> 404 until
// unlocked). They do not touch what a CDN already holds: the public arm of each
// declares `public, max-age=...`, so a crawler that fetched the card — or a
// merchant's table stand that fetched the QR — left a copy at the edge that keeps
// answering 200 after the page 404s.
//
// Measured on prod on 2026-10-26 (v1.21.330, nine minutes after the release that
// added the card's gate): a DRAFT form's card answered `cf-cache-status: HIT`,
// `age: 1243`, byte-identical to the PRE-release card, while the same URL with a
// cache-buster answered `404, 10 bytes "not found\n"` from the new origin.
// Measured again on 2026-10-31, after the QR routes were gated (v1.22.58): the
// same shape, for the QR PNG — `200, cf-cache-status: HIT, age: 1566,
// cache-control: public, max-age=86400` on a draft form's bare URL, and 404 from
// the origin and from any cache-busted fetch. That is the window this file
// closes: the purge is requested in the SAME request that changes the status,
// exactly as httpapi purgeMedia does for an object, and the outcome is NAMED in
// the reply (`edge`, plus `edge_max_age_s` when nothing was purged) instead of
// being swallowed.
//
// WHICH TRANSITIONS. Only the ones that can leave a PUBLICLY CACHED artefact
// behind: dbstore.OGImagePubliclyCached(old) && !dbstore.OGImagePubliclyCached(new),
// and a trash/purge of a form that was in that set. The QR PNG is published on
// exactly the same statuses (published, unlisted — see qrPublicAccess), so one
// predicate covers both artefacts and one purge request names both. A form moving
// INTO a live status needs no purge — the edge holds a 404 there, which
// Cloudflare expires on its own few-minute default, and that is the behaviour
// measured and accepted in t_8e3142f3.
//
// ORDER. The row is written FIRST and the purge second, the same order the media
// purge uses: a request that arrives in between can only miss the origin (404)
// and never re-populate the edge with the artefact we are invalidating. The
// reverse order would leave a window where the edge is cleared and instantly
// refilled from a form that is still published.

import (
	"context"
	"log/slog"

	"github.com/wzul/jomform/api/internal/cdnpurge"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// publicArtefactHosts lists every host this account's forms are publicly served
// on, and therefore every host their artefacts can be cached on.
//
// publicHostFor returns ONE of them: the verified custom domain when there is
// one, else the workspace subdomain. But a form resolves on BOTH — findPublicForm
// accepts a workspace subdomain and a custom domain — and handleOGImage draws the
// REQUEST host into the subtitle, while the QR encodes the request host into the
// target URL, so the two hosts are two different edge entries.
// Purging only the canonical one would leave the other one serving the artefact.
//
// A host that is not in the configured purge zone is dropped by cdnpurge before
// anything is sent, so naming both costs nothing when only one is configured.
func (h *RESTHandlers) publicArtefactHosts(ctx context.Context, acc *dbstore.Account) []string {
	if acc == nil {
		return nil
	}
	hosts := make([]string, 0, 2)
	if primary := h.publicHostFor(ctx, acc); primary != "" {
		hosts = append(hosts, primary)
	}
	if slug := h.publicHostSlugFor(ctx, acc.ID); slug != "" {
		hosts = append(hosts, slug+".jomform.com")
	}
	return hosts
}

// artefactURLs is every absolute public URL for these form slugs across the hosts
// above — the share card AND the QR PNG of each. Deduplication happens in
// cdnpurge (a host listed twice is one prefix).
func (h *RESTHandlers) artefactURLs(ctx context.Context, acc *dbstore.Account, slugs []string) []string {
	if len(slugs) == 0 {
		return nil
	}
	hosts := h.publicArtefactHosts(ctx, acc)
	urls := make([]string, 0, len(hosts)*len(slugs)*2)
	for _, host := range hosts {
		for _, slug := range slugs {
			urls = append(urls, cdnpurge.ArtefactURLs(host, slug)...)
		}
	}
	return urls
}

// purgeLeftPublicArtefacts asks the CDN to drop the cached card and QR PNG of the
// forms that just LEFT a publicly-cached status, and returns the outcome for the
// caller's reply. A zero Result (Status == "") means nothing was asked for, so
// cdnpurge.Result.ArtefactFields adds nothing to the reply.
//
// A refused or unconfigured purge never fails the request: the status change has
// already happened and no error can undo it. It is reported instead, and logged,
// because a silent skip would leave an operator believing the edge was cleared
// while the old artefact keeps naming the form.
func (h *RESTHandlers) purgeLeftPublicArtefacts(ctx context.Context, acc *dbstore.Account, slugs []string) cdnpurge.Result {
	urls := h.artefactURLs(ctx, acc, slugs)
	if len(urls) == 0 {
		return cdnpurge.Result{}
	}
	edge := cdnpurge.PurgeObjectURLs(ctx, urls)
	if edge.Err != nil {
		slog.Warn("form_edge_purge", "err", edge.Err, "status", edge.Status, "urls", len(urls))
	}
	return edge
}

// withEdgeFields merges a purge outcome into a response body. A nil/zero result
// (nothing to purge) leaves the body exactly as it was, so a status change that
// invalidated no cached artefact does not claim an edge action it never took.
func withEdgeFields(out map[string]any, edge cdnpurge.Result) map[string]any {
	for k, v := range edge.ArtefactFields() {
		out[k] = v
	}
	return out
}
