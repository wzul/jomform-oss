package httpapi

// The EDGE half of a form's generated social card.
//
// /og/{slug}.png is a public, cacheable URL that draws the form's TITLE, its
// workspace name and the request host into a PNG. The status gate in
// handleOGImage made the ORIGIN honest (draft -> 404 with the unknown-slug body,
// private -> 404 until unlocked). It does not touch what a CDN already holds:
// the public arm declares `public, max-age=...` (ogImageCacheControl), so a
// crawler that fetched the card while the form was live left a copy at the edge
// that keeps answering 200 after the page 404s.
//
// Measured on prod on 2026-10-26 (v1.21.330, nine minutes after the release that
// added the gate): a DRAFT form's card answered `cf-cache-status: HIT`,
// `age: 1243`, byte-identical to the PRE-release card, while the same URL with a
// cache-buster answered `404, 10 bytes "not found\n"` from the new origin. That
// is the window this file closes: the purge is requested in the SAME request
// that changes the status, exactly as httpapi purgeMedia does for an object, and
// the outcome is NAMED in the reply (`edge`, plus `edge_max_age_s` when nothing
// was purged) instead of being swallowed.
//
// WHICH TRANSITIONS. Only the ones that can leave a PUBLICLY CACHED card behind:
// dbstore.OGImagePubliclyCached(old) && !dbstore.OGImagePubliclyCached(new), and
// a trash/purge of a form that was in that set. A form moving INTO a live status
// needs no purge — the edge holds a 404 there, which Cloudflare expires on its
// own few-minute default, and that is the behaviour measured and accepted in
// t_8e3142f3.
//
// ORDER. The row is written FIRST and the purge second, the same order the media
// purge uses: a request that arrives in between can only miss the origin (404)
// and never re-populate the edge with the card we are invalidating. The reverse
// order would leave a window where the edge is cleared and instantly refilled
// from a form that is still published.

import (
	"context"
	"log/slog"

	"github.com/wzul/jomform/api/internal/cdnpurge"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// ogCardHosts lists every host this account's forms are publicly served on, and
// therefore every host a card for them can be cached on.
//
// publicHostFor returns ONE of them: the verified custom domain when there is
// one, else the workspace subdomain. But a form resolves on BOTH — findPublicForm
// accepts a workspace subdomain and a custom domain — and handleOGImage draws the
// REQUEST host into the subtitle, so the two hosts are two different edge entries.
// Purging only the canonical one would leave the other one serving the card.
//
// A host that is not in the configured purge zone is dropped by cdnpurge before
// anything is sent, so naming both costs nothing when only one is configured.
func (h *RESTHandlers) ogCardHosts(ctx context.Context, acc *dbstore.Account) []string {
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

// ogCardURLs is every absolute card URL for these form slugs across the hosts
// above. Deduplication happens in cdnpurge (a host listed twice is one prefix).
func (h *RESTHandlers) ogCardURLs(ctx context.Context, acc *dbstore.Account, slugs []string) []string {
	if len(slugs) == 0 {
		return nil
	}
	hosts := h.ogCardHosts(ctx, acc)
	urls := make([]string, 0, len(hosts)*len(slugs))
	for _, host := range hosts {
		for _, slug := range slugs {
			if u := cdnpurge.OGImageURL(host, slug); u != "" {
				urls = append(urls, u)
			}
		}
	}
	return urls
}

// purgeLeftPublicOGCards asks the CDN to drop the cached cards for the forms
// that just LEFT a publicly-cached status, and returns the outcome for the
// caller's reply. A zero Result (Status == "") means nothing was asked for, so
// cdnpurge.Result.OGFields() adds nothing to the reply.
//
// A refused or unconfigured purge never fails the request: the status change has
// already happened and no error can undo it. It is reported instead, and logged,
// because a silent skip would leave an operator believing the edge was cleared
// while the old card keeps naming the form.
func (h *RESTHandlers) purgeLeftPublicOGCards(ctx context.Context, acc *dbstore.Account, slugs []string) cdnpurge.Result {
	urls := h.ogCardURLs(ctx, acc, slugs)
	if len(urls) == 0 {
		return cdnpurge.Result{}
	}
	edge := cdnpurge.PurgeObjectURLs(ctx, urls)
	if edge.Err != nil {
		slog.Warn("form_og_edge_purge", "err", edge.Err, "status", edge.Status, "urls", len(urls))
	}
	return edge
}

// withEdgeFields merges a purge outcome into a response body. A nil/zero result
// (nothing to purge) leaves the body exactly as it was, so a status change that
// invalidated no cached card does not claim an edge action it never took.
func withEdgeFields(out map[string]any, edge cdnpurge.Result) map[string]any {
	for k, v := range edge.OGFields() {
		out[k] = v
	}
	return out
}
