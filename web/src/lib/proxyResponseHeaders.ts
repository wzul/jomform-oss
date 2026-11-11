// The response headers the dashboard's /api/* proxy RELAYS from the Go API to
// the browser (card t_2af1414e).
//
// THE DEFECT. web/src/app/api/[...path]/route.ts builds its own Headers() for the
// response half of the hop and copied three headers into it by hand
// (Content-Type, Location, Set-Cookie). Every OTHER response header the API set
// was dropped — on every /api/* response, not just one. The one that matters is
// the backoff on a refusal: the login lockout answers 429 + `Retry-After: 900`
// (api/internal/httpapi/auth.go; auth.LoginLockoutRetryAfter is the 15-minute
// window) beside a prose message, and the coarse per-IP throttle answers 429 +
// a `Retry-After` derived from the refusing client's bucket
// (api/internal/httpapi/middleware/ratelimit.go). Through the
// dashboard the browser received the status and the message and NEITHER header,
// so a locked-out visitor could not tell when the window clears.
//
//   measured from outside on prod v1.21.129 (card t_5fd36e64), the same locked
//   visitor, the same body:
//     POST https://api.jomform.com/v1/auth/login  -> 429, `Retry-After: 900`
//     POST https://jomform.com/api/auth/login     -> 429, no retry-after at all
//
// Direct API and MCP callers kept the header; only the browser path lost it.
//
// WHY A DECLARED SET, AND NOT A PASS-THROUGH. Copying the upstream headers
// wholesale would be a larger promise than this hop makes, and a different one.
// The API also writes headers for its own edge — X-Request-ID
// (middleware/middleware.go), the CSP / X-Frame-Options it puts on the HTML it
// renders itself — and those are not this proxy's to speak for. So what the
// browser is told is DECLARED here, once, and the hop copies exactly this list.
// Widening the list is a decision to take here, with the reason written at the
// entry — `cache-control` was added that way for the merchant's printable stand,
// which the dashboard opens through this hop (card t_b870d44b).
//
// The names are lower-case because Headers is case-insensitive and one spelling
// removes the "which case did the other half use" question; the cross-language pin
// (api/internal/httpapi/dashboard_proxy_responseheaders_test.go) compares the two
// sides as sets.
export const RELAYED_RESPONSE_HEADERS = [
  // The body's type. Without it neither the browser nor the dashboard's own JSON
  // callers (web/src/lib/apiErr.ts and the pages) can tell what the hop served.
  "content-type",
  // A redirect the browser must follow ITSELF. The hop fetches with
  // redirect: "manual" on purpose (issue #273): the OAuth providers (Google,
  // Cloudflare) answer 302 and the browser, not this container, has to go there.
  "location",
  // Every Set-Cookie the API set — the session cookie, and the state + PKCE
  // verifier pair the Cloudflare OAuth flow mints (issue #22). Several values per
  // response, which is why it is copied through getSetCookie() below.
  "set-cookie",
  // WHEN THE CALLER MAY TRY AGAIN — the header this list exists for. The API
  // states its backoff in a response header (the login lockout and the per-IP
  // throttle both), and a dashboard client can only back off by the value the
  // server chose if it actually receives that value. Relayed verbatim: the value
  // is a delta-seconds count (900) or an HTTP-date (RFC 9110 §10.2.3), and
  // re-deriving either here would be a second clock deciding what the API has
  // already decided.
  "retry-after",
  // WHETHER THIS RESPONSE MAY BE STORED — widened deliberately (card t_b870d44b).
  // The API marks the merchant's own printable stand
  // (`GET /v1/rest/forms/{id}/qr-stand`, api/internal/httpapi/rest_qr_stand.go)
  // `private, no-store`, because that document names and diagrams a form the
  // merchant has not published yet. The dashboard's "Print stand" action opens
  // exactly that route THROUGH this hop, so dropping the header made the browser
  // path promise less than the API does: measured on prod, the same session on
  // the same draft form got `cache-control: private, no-store` from the API host
  // and no cache-control at all from https://jomform.com/api/rest/forms/{id}/qr-stand
  // — the URL the dashboard's anchor carries. Relayed verbatim, and still never
  // invented: a response the API left without one stays without one. Every
  // Cache-Control the API sets on a route behind this hop is a RESTRICTION it
  // already chose (`private, no-store` on the stand, `no-store` on the menu,
  // `no-cache` on the realtime stream), so copying it can only tighten this hop;
  // the public artefacts the API caches for five minutes (`/og`, `/qr`, `/media`,
  // its own HTML) are served on their own hosts and do not cross it.
  "cache-control",
  // WHAT THE BODY IS CALLED AND WHETHER TO SAVE IT — widened deliberately
  // (card t_569121de). The API's CSV routes name their file in this header:
  // the admin dormancy report
  // (`GET /v1/admin/dormancy?format=csv`, api/internal/httpapi/rest_admin_dormancy.go)
  // and the merchant's own sales + responses exports
  // (api/internal/httpapi/rest_export.go) all answer
  // `attachment; filename="…csv"`. Dropping it through this hop still delivered
  // the bytes and the `text/csv` type, but with no filename the browser saves the
  // response under the URL's last path segment instead of the name the API chose:
  // measured on prod v1.35.0 @ 9c53ad2e8, `GET https://jomform.com/api/admin/dormancy?format=csv`
  // returned 200 + `text/csv; charset=utf-8` and no content-disposition, while the
  // same request directly to https://api.jomform.com/v1/admin/dormancy?format=csv
  // kept `attachment; filename="jomform-dormancy.csv"`. Relayed verbatim, and
  // still never invented: a response the API left without the header stays
  // without one.
  "content-disposition",
] as const;

/**
 * Copy each relayed response header the API sent onto the response this hop
 * returns to the browser.
 *
 * A header the API did not set is not invented: absent stays absent, so a
 * response that carries no backoff keeps meaning exactly that, and the caller
 * falls back to its own retry policy rather than to a number this proxy made up.
 */
export function relayResponseHeaders(upstream: Headers, out: Headers): void {
  for (const name of RELAYED_RESPONSE_HEADERS) {
    if (name === "set-cookie") {
      // OAuth flows (e.g. Cloudflare, issue #22) set multiple cookies per
      // response (state + PKCE verifier); forward ALL of them, not just the
      // first. `Headers.get` would join them into one invalid value, which is why
      // getSetCookie() is tried first and the single-value read is only the
      // fallback for a runtime that has none.
      const cookies =
        typeof upstream.getSetCookie === "function" ? upstream.getSetCookie() : [];
      if (cookies.length > 0) {
        for (const c of cookies) out.append("Set-Cookie", c);
      } else {
        const one = upstream.get("set-cookie");
        if (one) out.set("Set-Cookie", one);
      }
      continue;
    }
    const value = upstream.get(name);
    if (value) out.set(name, value);
  }
}
