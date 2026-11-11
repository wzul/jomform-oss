// The client-address headers the dashboard's /api/* proxy must RELAY to the Go
// API (card t_ae8791f9).
//
// WHY THIS EXISTS AT ALL. jomform.com's /api/* paths are routed to the Next
// container (deploy/vps/traefik-jomform.yml, router jf-web-api -> service jf-web
// -> http://jomform-web-1:3000), so a dashboard request reaches the Go API FROM
// THE WEB CONTAINER's address on the compose network, never from the browser.
// The API resolves the client from the forwarding headers and reads them only
// when the socket peer is an address a request can arrive THROUGH — the
// container network is one of those (api/internal/httpapi/middleware/clientip.go,
// intermediaryRanges). This proxy used to relay NONE of them, so ClientIP fell
// through to the socket peer and the answer was one address for every browser:
//
//   measured on prod, revision 72092730 (t_730c552d):
//     POST https://jomform.com/api/auth/login        -> 401
//       key: jf:loginfail:ip:172.18.0.6   (jomform-web-1 on the compose network)
//     POST https://api.jomform.com/v1/auth/login     -> 401
//       key: jf:loginfail:ip:<the visitor's own address>
//
// That single shared key is an availability defect, not just a counter:
// auth.LoginLimiter locks at FailLockoutThreshold = 10 failures in a 15-minute
// window, so ten wrong-password logins from ANY mix of accounts through the
// dashboard answered 429 + Retry-After 900 to EVERY subsequent dashboard login —
// a legitimate owner with the correct password included. The same key is the
// per-IP bucket of the coarse RateLimit on /v1/auth/login (1/s, burst 10), so the
// web container was the single shared egress for all dashboard users there too.
//
// WHAT THIS DOES, AND WHAT IT DELIBERATELY DOES NOT. It RELAYS what the hop in
// front of us wrote and makes NO trust decision of its own — the decision stays
// in ONE place, middleware/clientip.go's TrustedProxy.resolveClientIP, which
// walks the visible chain right to left and accepts CF-Connecting-IP only when
// every hop is a proxy AND one of them is a Cloudflare edge address. A browser
// can set these headers itself (neither is in the Fetch spec's forbidden-header
// list), so relaying is only safe because the hop that wrote them sanitises them:
//
//   * Traefik (the edge in front of this container) DELETES a caller-supplied
//     X-Forwarded-For / X-Real-IP and replaces it with the address of the peer it
//     accepted the connection from — measured against the deployed
//     traefik:v3.3 entrypoint configuration, 2026-10-11.
//   * Cloudflare sets CF-Connecting-IP at the edge; per its own documentation it
//     "will only be sent on the traffic from Cloudflare's edge to your origin web
//     server" (developers.cloudflare.com/fundamentals/reference/http-headers/),
//     and it passes through Traefik untouched.
//
// A header that did not come from behind either of those is still believed
// nowhere: the chain the API walks then contains no Cloudflare hop, so
// CF-Connecting-IP is skipped and the chain's own nearest non-proxy hop wins
// (clientip.go, resolveClientIP). Relaying therefore hands the API the evidence
// it already knows how to judge, and adds no new trust input.
export const CLIENT_ADDRESS_HEADERS = [
  // The chain Traefik wrote: the nearest hop is its own record of the peer, and
  // caller-written entries sit to the LEFT of it (resolveClientIP walks right to
  // left). Behind Cloudflare this is the edge address.
  "x-forwarded-for",
  // The same nearest hop recorded separately by Traefik; clientip.go consults it
  // only when no chain reached us.
  "x-real-ip",
  // The visitor, set by Cloudflare. clientip.go honours it only when the chain
  // shows a Cloudflare edge, which is the only thing that can put one there.
  "cf-connecting-ip",
  // The edge's city for that visitor (card t_9a6cc85f). DISPLAY ONLY: the API
  // reads it for the device-authorisation consent email ("this came from Kuala
  // Lumpur, is that you?"), under the same Cloudflare-edge condition, and never
  // as an authorization input. Relayed because the Go-side guard
  // (dashboard_proxy_clientaddr_test.go) requires every header clientip.go reads
  // to survive this hop — an API input the dashboard silently drops is a rule
  // that only holds on one of the two call paths.
  "cf-ipcity",
] as const;

/**
 * Copy each client-address header the incoming request carries onto the outgoing
 * one, byte for byte.
 *
 * The rule never DERIVES a value: a header the request does not carry is not
 * copied, so what an absent header means stays the API's decision (its rule keeps
 * the socket peer then — which is how every server-side caller of this route, a
 * fetch from this same container, arrives).
 *
 * Note what "carries" already includes on this path, measured rather than
 * assumed: Next's own server assigns `x-forwarded-for` from the socket's remote
 * address when a request arrives without one —
 * `req.headers['x-forwarded-for'] ??= originalRequest?.socket?.remoteAddress`
 * (next/dist/server/base-server.js, next 16.3.8) — so a request sent to this
 * route with no client-address header reached the upstream as
 * `x-forwarded-for: ::ffff:127.0.0.1` when measured through the built server.
 * That synthesised value is the hop in front of THIS container, not the browser.
 * Behind the deployed front it is therefore not what gets relayed: Traefik has
 * already written the real chain by then, and what this rule copies is Traefik's
 * record plus Cloudflare's visitor header — which is the whole point.
 */
export function forwardClientAddress(incoming: Headers, out: Headers): void {
  for (const name of CLIENT_ADDRESS_HEADERS) {
    const value = incoming.get(name);
    if (value) out.set(name, value);
  }
}
