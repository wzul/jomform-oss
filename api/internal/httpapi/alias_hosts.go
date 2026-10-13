// Former workspace hosts: keep the URLs already in the wild alive.
//
// A workspace's slug IS its public host ({slug}.jomform.com). Until renames were
// supported the slug was fixed at creation, so no address could ever go stale.
// Renaming is now allowed, which means every URL a merchant has already handed
// out — a printed QR stand on a table, an iframe snippet pasted into their site,
// an affiliate link with ?aff=CODE, a booking link in an email — names the OLD
// host.
//
// This file is the ONE place a former host is answered. The redirect is applied
// to the whole router rather than to each public handler, because the host is
// read independently by a dozen routes (form render, submit, QR image, QR stand,
// og:image, assets, legal pages, the storefront root, the menu order) — patching
// them one by one is how a "one rule" becomes a dozen copies that drift, and a
// missed route is a 404 for a merchant who cannot see why.
//
// WHY A REDIRECT AND NOT "SERVE THE SAME WORKSPACE ON BOTH HOSTS". Serving both
// would put one tenant on two public origins, with session cookies, SEO
// canonical URLs and any relative asset link split across them, and it would
// hide the rename from the merchant forever. A permanent redirect moves the
// visitor (and the bookmark, and the crawler) to the one current address.
//
// WHY 308 AND NOT 301 — the hop must not drop the request. A 301 lets the CLIENT
// discard the method: RFC 9110 §15.4.2 carries the note that "for historical
// reasons, a user agent MAY change the request method from POST to GET for the
// subsequent request", and browsers do exactly that ("if this behavior is
// undesired, the 308 (Permanent Redirect) status code can be used instead").
// This wrapper is around the WHOLE router, so the stale-page case is not an edge
// case, it is the product: a served page submits to its OWN origin
// (fetch(location.pathname + "/order"), fetch(location.pathname + "/unlock") —
// public.go, embed.go), and a buyer with the page open across a rename therefore
// posts to the FORMER host by construction. Answered 301, a browser re-issues
// that request as a GET against a POST-only route and the buyer gets a 405
// instead of a submission. 308 is the permanent, METHOD-PRESERVING status
// RFC 9110 §15.4 added for precisely this ("307 (Temporary Redirect) and 308
// (Permanent Redirect) were later added to unambiguously indicate
// method-preserving redirects"): the method and the body travel with the hop.
//
// WHY NO-STORE — the answer must not outlive its row. A 308 is heuristically
// cacheable (RFC 9110 §15.4.9), exactly as the 301 it replaces was: with no
// explicit freshness information a cache MAY store it and invent its own
// lifetime (RFC 9111 §3, §4.2.2). The design intent is the opposite — a stale
// alias can never shadow or outlive anything, which is why
// dbstore.CurrentSlugForHost consults the LIVE slug first and why a rename back
// onto the old slug, or a deleted workspace, takes the row away. An edge or
// browser holding a cached redirect for a host whose row is gone is the one path
// where a former host keeps answering, so the response says no-store instead of
// leaving that to a heuristic. This is the same arm qr.go uses for the fallback
// a rename can move (a cached image there encoded a slug the form had left).
// No Vary is emitted because there is nothing to negotiate: the answer depends on
// the HOST alone, and the host is part of every cache key rather than a request
// header a cache varies on.
//
// The lookup order lives in dbstore.WorkspaceStore.CurrentSlugForHost: the LIVE
// workspace wins, and the alias table is consulted only when nothing live
// matches, so a slug that comes back into service can never be shadowed by a
// stale alias.
package httpapi

import (
	"net/http"
)

// productHosts are jomform.com subdomains that belong to the PRODUCT, never to a
// tenant: they serve the dashboard, the API and the MCP endpoint. A slug rule
// ("must not end in jomform") stops a merchant minting "jomform.mcp", but it does
// NOT stop one from legitimately creating a workspace called "mcp" — which would
// then make the product's own host look like a tenant address. accountSubdomain
// already drops www/app/api/mail/s3 for the same reason; the hosts it does not
// know about are listed here, and a request on one of them is never redirected on
// (and never resolved as an alias), whatever a workspace may once have been
// called.
//
// This is a guard on the REDIRECT path only. It does not change how any existing
// route resolves a host, so it cannot alter behaviour for a host that never had
// an alias.
var productHosts = map[string]bool{
	"mcp":    true, // the MCP endpoint (issue #140)
	"custom": true, // the custom-domain CNAME target (issue #323)
	"docs":   true,
	"blog":   true,
}

// AliasHostRedirect answers a request that arrives on a FORMER workspace host
// with a permanent, METHOD-PRESERVING redirect (HTTP 308, Cache-Control:
// no-store — see the package comment for why neither a 301 nor a cacheable
// answer is acceptable here) to the workspace's current host, preserving path and
// query so the specific form, table number or affiliate code is not lost.
//
// It does nothing when:
//   - the host is not a {slug}.jomform.com subdomain (the apex, the dashboard
//     hosts, the api/mcp hosts and every custom domain are out of scope — an
//     alias is a workspace-subdomain concept);
//   - the subdomain is a PRODUCT host (see productHosts);
//   - the host resolves to no workspace at all, live or former (an unknown slug
//     stays a 404, exactly as before);
//   - the host is the workspace's CURRENT slug (the ordinary case: one indexed
//     primary-key read, and the request continues untouched).
func (h *PublicHandlers) AliasHostRedirect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h == nil || h.Workspaces == nil {
			next.ServeHTTP(w, r)
			return
		}
		sub := accountSubdomain(r.Host)
		if sub == "" || productHosts[sub] {
			next.ServeHTTP(w, r)
			return
		}
		current, err := h.Workspaces.CurrentSlugForHost(r.Context(), sub)
		if err != nil || current == "" || current == sub {
			next.ServeHTTP(w, r)
			return
		}
		scheme := "https"
		if isDev {
			scheme = "http"
		}
		target := scheme + "://" + current + ".jomform.com" + r.URL.Path
		if q := r.URL.RawQuery; q != "" {
			target += "?" + q
		}
		// no-store BEFORE the redirect: the row this answer comes from can go
		// away (a rename back onto the old slug, a deleted workspace), and a 308
		// carrying no freshness information would otherwise be stored on a
		// cache's own heuristic (RFC 9110 §15.4.9, RFC 9111 §3/§4.2.2) — the one
		// way a former host outlives its row. Headers must be set before
		// http.Redirect writes the status line.
		w.Header().Set("Cache-Control", "no-store")
		// 308, not 301: a 301 lets a browser re-issue a POST as a GET (RFC 9110
		// §15.4.2), and the page a stale buyer is holding posts to its own origin
		// — see the package comment.
		http.Redirect(w, r, target, http.StatusPermanentRedirect)
	})
}
