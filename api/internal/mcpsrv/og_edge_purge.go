package mcpsrv

// The EDGE half of a form's generated social card, on the AGENT surface.
//
// /og/{slug}.png draws the form's title, workspace name and host into a public,
// cacheable PNG. httpapi's status gate made the ORIGIN honest (draft -> 404,
// private -> 404 until unlocked) but nothing drops what a CDN already holds, so
// a card a crawler fetched while the form was live kept being served after the
// page 404ed — measured on prod 2026-10-26 on a DRAFT form: `cf-cache-status:
// HIT`, `age: 1243`, while the cache-busted origin answered 404.
//
// The fix is one rule with two front doors. httpapi/og_edge_purge.go applies it to
// the dashboard/REST write paths; this file applies the SAME rule to the MCP
// tools, because an agent can unpublish a form too (publish_form status=draft,
// unpublish_form, delete_form, bulk_forms, purge_form) and a fix that covered only
// the REST half would be bypassed by the tool an agent is told to use.
//
// Nothing here is a second copy of the decision: the predicate is
// dbstore.OGImagePubliclyCached, the URL is cdnpurge.OGImageURL, the purge and its
// reported outcome are cdnpurge.PurgeObjectURLs / Result.OGFields.

import (
	"context"
	"log/slog"

	"github.com/wzul/jomform/api/internal/cdnpurge"
)

// ogCardHosts lists every host this account's forms are publicly served on, and
// so every host their cards can be cached on: the canonical public host (a
// verified custom domain when there is one, else the workspace subdomain) plus
// the workspace subdomain itself when it differs from the canonical host. A form
// resolves on both, and the card draws the request host into its subtitle, so the
// two hosts are two separate CDN entries.
func (s *Server) ogCardHosts(ctx context.Context, accountID string) []string {
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

// ogCardURLs is every absolute card URL for these slugs across those hosts.
func (s *Server) ogCardURLs(ctx context.Context, accountID string, slugs []string) []string {
	return ogCardURLsForHosts(s.ogCardHosts(ctx, accountID), slugs)
}

// ogCardURLsForHosts builds the card URLs for one account's hosts. Split out so a
// bulk call resolves an account's hosts ONCE and reuses them for every id in that
// workspace, instead of re-reading the account row per form.
func ogCardURLsForHosts(hosts, slugs []string) []string {
	if len(hosts) == 0 || len(slugs) == 0 {
		return nil
	}
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

// purgeLeftPublicOGCards drops the cached cards of the forms that just LEFT a
// publicly-cached status. A zero Result (Status == "") means nothing was asked
// for and Result.OGFields adds nothing to the tool reply.
//
// The tool call is never failed by a purge it could not complete: the status
// write has already happened. The outcome is reported (`edge`, plus the residual
// window) and logged instead — a silent skip would leave the agent, and the
// merchant reading its answer, believing the old card was withdrawn.
func (s *Server) purgeLeftPublicOGCards(ctx context.Context, accountID string, slugs []string) cdnpurge.Result {
	urls := s.ogCardURLs(ctx, accountID, slugs)
	if len(urls) == 0 {
		return cdnpurge.Result{}
	}
	edge := cdnpurge.PurgeObjectURLs(ctx, urls)
	if edge.Err != nil {
		slog.Warn("mcp_form_og_edge_purge", "err", edge.Err, "status", edge.Status, "urls", len(urls))
	}
	return edge
}

// withEdgeFields merges a purge outcome into a tool result map, leaving it
// untouched when nothing was purged (a zero result adds no fields).
func withEdgeFields(out map[string]any, edge cdnpurge.Result) map[string]any {
	for k, v := range edge.OGFields() {
		out[k] = v
	}
	return out
}
