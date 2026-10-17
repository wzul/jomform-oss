// The dashboard's /api/* proxy must RELAY the API's response headers the browser
// needs — first among them the login lockout's Retry-After (card t_2af1414e).
//
// THE DEFECT. web/src/app/api/[...path]/route.ts built a FRESH Headers() for the
// response half of its hop and copied three headers into it by hand
// (Content-Type, Location, Set-Cookie). Everything else the API set was dropped.
// The loudest one is the backoff: the API answers a lockout with 429 +
// `Retry-After: 900` (api/internal/httpapi/auth.go; LoginLockoutRetryAfter is the
// 15-minute window) and a throttled request with 429 + `Retry-After: 30`
// (middleware/ratelimit.go).
//
//   measured from outside on prod v1.21.129 (card t_5fd36e64), the same locked
//   visitor and the same body: `Retry-After: 900` direct to api.jomform.com, and
//   no retry-after at all through https://jomform.com/api/auth/login.
//
// WHAT IS PINNED HERE
//   1. Retry-After reaches the browser VERBATIM, whether it is a delta-seconds
//      count (900 / 30) or an HTTP-date — the client backs off by the value the
//      server chose, not by one this hop re-derived.
//   2. Nothing is invented when the API sent no backoff, and nothing else the API
//      sent is relayed: the relayed set is a DECISION (one declared list), not a
//      pass-through of whatever the upstream carried.
//   3. The three headers the hop has always copied still are (Content-Type,
//      Location, and ALL the Set-Cookie values — the Cloudflare OAuth flow sets a
//      state + PKCE pair, issue #22).
//   4. route.ts actually USES the rule, and still builds its own response header
//      set. A green test on a helper the proxy never calls proves nothing — the
//      trap web/src/lib/clientAddress.test.ts and passkeyProxyHost.test.ts both
//      name.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * It cannot run the route handler: importing route.ts under node --test needs
//     Next's server runtime (`next/server` is not resolvable by Node's ESM loader
//     here), which is why the rule lives in a plain module and the wiring is
//     pinned as source text.
//   * It cannot see whether the API still STATES a backoff, or whether the
//     declared set has drifted one entry beyond what the API owes a browser: that
//     is the cross-language half,
//     api/internal/httpapi/dashboard_proxy_responseheaders_test.go, which derives
//     the backoff header from the API's own source and requires the two sides to
//     agree in BOTH directions.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { RELAYED_RESPONSE_HEADERS, relayResponseHeaders } from "./proxyResponseHeaders.ts";

const ROUTE = readFileSync(
  fileURLToPath(new URL("../app/api/[...path]/route.ts", import.meta.url)),
  "utf8",
);

/** The hop's response header set, as route.ts builds it: fresh, then filled. */
function relay(upstream: Headers): Headers {
  const out = new Headers();
  relayResponseHeaders(upstream, out);
  return out;
}

test("the login lockout's Retry-After reaches the browser verbatim", () => {
  // The shipped lockout response: 429 + this header beside the Malay message
  // (api/internal/httpapi/auth.go, auth.LoginLockoutRetryAfter = "900").
  const out = relay(
    new Headers({
      "content-type": "application/json; charset=utf-8",
      "retry-after": "900",
    }),
  );

  assert.equal(out.get("retry-after"), "900");
  assert.equal(out.get("content-type"), "application/json; charset=utf-8");
});

test("the coarse throttle's Retry-After is relayed too", () => {
  // api/internal/httpapi/middleware/ratelimit.go answers a throttled request with
  // 429 + `Retry-After: 30`; that path is a different writer of the same contract,
  // and it was dropped by the same omission.
  assert.equal(relay(new Headers({ "retry-after": "30" })).get("retry-after"), "30");
});

test("an HTTP-date Retry-After survives unchanged (no re-derivation here)", () => {
  // RFC 9110 §10.2.3 allows either form, and the hop is not the clock that
  // decides which one the API chose.
  const when = "Wed, 21 Oct 2026 07:28:00 GMT";
  assert.equal(relay(new Headers({ "retry-after": when })).get("retry-after"), when);
});

test("a response with no backoff does not get one invented for it", () => {
  // The rule copies; it never derives. A 200 (or a 401 with no window to report)
  // must stay header-less, or the client would back off for a reason nobody gave.
  const out = relay(new Headers({ "content-type": "application/json" }));
  assert.equal(out.get("retry-after"), null);
  assert.deepEqual(Object.fromEntries(out.entries()), {
    "content-type": "application/json",
  });
});

test("every Set-Cookie the API set survives, not just the first", () => {
  // The Cloudflare OAuth flow mints state + PKCE verifier in one response
  // (issue #22); a single-cookie relay would silently drop one and break the flow.
  const out = relay(
    new Headers([
      ["set-cookie", "cf_oauth_state=abc; Path=/; HttpOnly; Secure; Max-Age=600"],
      ["set-cookie", "cf_pkce_v=xyz; Path=/; HttpOnly; Secure; Max-Age=600"],
    ]),
  );

  assert.deepEqual(out.getSetCookie(), [
    "cf_oauth_state=abc; Path=/; HttpOnly; Secure; Max-Age=600",
    "cf_pkce_v=xyz; Path=/; HttpOnly; Secure; Max-Age=600",
  ]);
});

test("the copies the hop has always made still happen", () => {
  const out = relay(
    new Headers({
      "content-type": "text/html; charset=utf-8",
      location: "https://accounts.google.com/o/oauth2/v2/auth?client_id=x",
      "set-cookie": "jf_session=s; Path=/; HttpOnly",
    }),
  );

  assert.equal(out.get("content-type"), "text/html; charset=utf-8");
  assert.equal(out.get("location"), "https://accounts.google.com/o/oauth2/v2/auth?client_id=x");
  assert.equal(out.get("set-cookie"), "jf_session=s; Path=/; HttpOnly");
});

test("nothing else the API sends is relayed (this is a decision, not a pass-through)", () => {
  // X-Request-ID (middleware/middleware.go), and the CSP / frame / cache headers
  // the API puts on the HTML it renders ITSELF, are for the API's own edge. A
  // wholesale copy would hand the browser promises this hop never meant to make.
  const out = relay(
    new Headers({
      "x-request-id": "0f1e2d3c",
      "cache-control": "public, max-age=86400",
      "content-security-policy": "frame-ancestors 'none'",
      "x-frame-options": "DENY",
      "cross-origin-resource-policy": "same-origin",
      vary: "Accept-Encoding",
      "content-type": "application/json",
    }),
  );

  assert.deepEqual(Object.fromEntries(out.entries()), {
    "content-type": "application/json",
  });
});

test("the relayed set is exactly these four, named", () => {
  // Named, not counted: dropping one (the defect) or adding one (a promise nobody
  // decided on) is a change to what every dashboard caller is told.
  assert.deepEqual([...RELAYED_RESPONSE_HEADERS].sort(), [
    "content-type",
    "location",
    "retry-after",
    "set-cookie",
  ]);
});

test("route.ts relays the response through this rule, into a set it builds itself", () => {
  assert.ok(
    ROUTE.includes("relayResponseHeaders(r.headers, respHeaders)"),
    "web/src/app/api/[...path]/route.ts no longer relays the API's response headers through " +
      "lib/proxyResponseHeaders.ts — a browser then never sees the login lockout's Retry-After " +
      "(measured on prod v1.21.129: `Retry-After: 900` direct to the API, none through the hop), and " +
      "the rule above is dead code",
  );

  // The control for the assertion above: it must still be a HOP that builds its own
  // response header set, not the upstream headers handed straight to NextResponse.
  assert.ok(
    /const respHeaders = new Headers\(\);/.test(ROUTE),
    "web/src/app/api/[...path]/route.ts no longer builds its own response header set — if the " +
      "upstream headers now reach the browser wholesale, the API's internal headers " +
      "(X-Request-ID, the CSP on its own HTML) are relayed too, which is a promise nobody decided on",
  );
  assert.ok(
    /relayResponseHeaders\(r\.headers, respHeaders\)/.test(ROUTE) &&
      !/headers:\s*r\.headers/.test(ROUTE),
    "web/src/app/api/[...path]/route.ts hands the upstream response headers to NextResponse " +
      "wholesale instead of copying the declared set (lib/proxyResponseHeaders.ts)",
  );
});
