package mcpsrv

// The EDGE half of a form's generated public artefacts — its share card
// (/og/{slug}.png) and its QR PNG (/qr/{slug}.png) — on the AGENT surface.
//
// The card draws the form's title, workspace name and host into a public,
// cacheable PNG, and the QR PNG encodes the form's public URL, so it confirms the
// form EXISTS at that slug. httpapi's status gates made the ORIGIN honest (draft
// -> 404, private -> 404 until unlocked) but nothing drops what a CDN already
// holds, so an artefact fetched while the form was live kept being served after
// the page 404ed — measured on prod 2026-10-26 on a DRAFT form's card:
// `cf-cache-status: HIT`, `age: 1243`, while the cache-busted origin answered
// 404 — and again on 2026-10-31 on a DRAFT form's QR PNG: `HIT`, `age: 1566`,
// while every cache-busted fetch answered 404.
//
// The fix is one rule with two front doors. httpapi/og_edge_purge.go applies it to
// the dashboard/REST write paths; this file applies the SAME rule to the MCP
// tools, because an agent can unpublish a form too (publish_form status=draft,
// unpublish_form, delete_form, bulk_forms, purge_form) and a fix that covered only
// the REST half would be bypassed by the tool an agent is told to use.
//
// Nothing here is a second copy of the decision: the predicate is
// dbstore.OGImagePubliclyCached (the QR PNG is served on exactly the same two
// arms), the URLs are cdnpurge.ArtefactURLs (card + QR, one set), the purge and
// its reported outcome are cdnpurge.PurgeObjectURLs / Result.ArtefactFields.

import (
	"context"
	"log/slog"

	"github.com/wzul/jomform/api/internal/cdnpurge"
)

// publicArtefactHosts lists every host this account's forms are publicly served
// on, and so every host their artefacts can be cached on: the canonical public
// host (a verified custom domain when there is one, else the workspace subdomain)
// plus the workspace subdomain itself when it differs from the canonical host. A
// form resolves on both, and both the card and the QR PNG encode the request
// host, so the two hosts are two separate CDN entries.
func (s *Server) publicArtefactHosts(ctx context.Context, accountID string) []string {
	if accountID == "" {
		return nil
	}
	hosts := make([]string, 0, 2)
	if primary := s.publicHostForID(ctx, accountID); primary != "" {
		hosts = append(hosts, primary)
	}
	if slug := s.publicHostSlugFor(ctx, accountID); slug != "" {
		hosts = append(hosts, slug+".jomform.com")
	}
	return hosts
}

// artefactURLs is every absolute artefact URL (card + QR PNG) for these slugs
// across those hosts.
func (s *Server) artefactURLs(ctx context.Context, accountID string, slugs []string) []string {
	return artefactURLsForHosts(s.publicArtefactHosts(ctx, accountID), slugs)
}

// artefactURLsForHosts builds the artefact URLs for one account's hosts. Split out
// so a bulk call resolves an account's hosts ONCE and reuses them for every id in
// that workspace, instead of re-reading the account row per form.
func artefactURLsForHosts(hosts, slugs []string) []string {
	if len(hosts) == 0 || len(slugs) == 0 {
		return nil
	}
	urls := make([]string, 0, len(hosts)*len(slugs)*2)
	for _, host := range hosts {
		for _, slug := range slugs {
			urls = append(urls, cdnpurge.ArtefactURLs(host, slug)...)
		}
	}
	return urls
}

// purgeLeftPublicArtefacts drops the cached card and QR PNG of the forms that
// just LEFT a publicly-cached status. A zero Result (Status == "") means nothing
// was asked for and Result.ArtefactFields adds nothing to the tool reply.
//
// The tool call is never failed by a purge it could not complete: the status
// write has already happened. The outcome is reported (`edge`, plus the residual
// window) and logged instead — a silent skip would leave the agent, and the
// merchant reading its answer, believing the artefact was withdrawn.
func (s *Server) purgeLeftPublicArtefacts(ctx context.Context, accountID string, slugs []string) cdnpurge.Result {
	urls := s.artefactURLs(ctx, accountID, slugs)
	if len(urls) == 0 {
		return cdnpurge.Result{}
	}
	edge := cdnpurge.PurgeObjectURLs(ctx, urls)
	if edge.Err != nil {
		slog.Warn("mcp_form_edge_purge", "err", edge.Err, "status", edge.Status, "urls", len(urls))
	}
	return edge
}

// withEdgeFields merges a purge outcome into a tool result map, leaving it
// untouched when nothing was purged (a zero result adds no fields).
func withEdgeFields(out map[string]any, edge cdnpurge.Result) map[string]any {
	for k, v := range edge.ArtefactFields() {
		out[k] = v
	}
	return out
}
