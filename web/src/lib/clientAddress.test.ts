// The dashboard's /api/* proxy must RELAY the client address (card t_ae8791f9).
//
// THE DEFECT. web/src/app/api/[...path]/route.ts built a FRESH Headers() for its
// hop (Authorization / Cookie / Content-Type / X-Forwarded-Host) and relayed no
// client-address header at all, so every dashboard request reached the Go API
// from the WEB CONTAINER's address on the compose network. That address is the
// key of the API's per-IP rate limit and of the login lockout, so all browsers
// shared ONE bucket:
//
//   measured on prod, revision 72092730 (t_730c552d), reading prod valkey:
//     POST https://jomform.com/api/auth/login       -> 401, key
//       jf:loginfail:ip:172.18.0.6   (== jomform-web-1 on the compose network)
//     POST https://api.jomform.com/v1/auth/login    -> 401, key on the visitor
//
// With FailLockoutThreshold = 10 in a 15-minute window, ten wrong-password
// logins from any mix of accounts through the dashboard refused EVERY dashboard
// login for 15 minutes — correct password included.
//
// WHAT IS PINNED HERE
//   1. Every client-address header the incoming request carries is relayed
//      VERBATIM (the chain Traefik wrote, the Cloudflare visitor header, and the
//      same-hop X-Real-IP). This is the whole fix: it is what makes the API
//      resolve the visitor instead of the container.
//   2. Nothing is invented when a request carries none — a server-side fetch from
//      this same container must keep falling back to the socket peer, as it
//      always did.
//   3. Nothing else is copied. The hop's header set is deliberately built, not
//      the incoming request's headers passed through: relaying Host, Cookie or
//      Authorization wholesale would change what the API is told about the
//      connection (route.ts replaces Host with JOMFORM_API_URL on purpose, and
//      forwards the browser's host as X-Forwarded-Host instead).
//   4. route.ts actually USES the rule. A green test on a helper the proxy never
//      calls proves nothing — the same trap the passkey host guard
//      (web/src/lib/passkeyProxyHost.test.ts) names.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * It cannot run the route handler: importing route.ts under node --test needs
//     Next's server runtime (its `next/server` import is not resolvable by Node's
//     ESM loader without an exports map or a loader shim), which is why the rule
//     lives in a plain module and the wiring is pinned as source text here.
//   * It says nothing about what Traefik or Cloudflare put in those headers. It
//     asserts what this hop does with what it was given; the trust decision on
//     the values is the API's, in one place
//     (api/internal/httpapi/middleware/clientip.go), and is driven end-to-end by
//     TestDashboardProxyRelaysEveryHeaderTheAPITrusts
//     (api/internal/httpapi/dashboard_proxy_clientaddr_test.go).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { CLIENT_ADDRESS_HEADERS, forwardClientAddress } from "./clientAddress.ts";

const ROUTE = readFileSync(
  fileURLToPath(new URL("../app/api/[...path]/route.ts", import.meta.url)),
  "utf8",
);

/** The hop's own header set, as route.ts builds it: fresh, then filled. */
function relay(incomingPairs: Record<string, string>): Record<string, string> {
  const out = new Headers();
  forwardClientAddress(new Headers(incomingPairs), out);
  return Object.fromEntries(out.entries());
}

test("every client-address header the request carries is relayed verbatim", () => {
  // The deployed shape: Traefik's sanitised chain (behind Cloudflare this is the
  // edge address), the same hop recorded as X-Real-IP, and the visitor Cloudflare
  // put in CF-Connecting-IP.
  const got = relay({
    "x-forwarded-for": "173.245.48.9",
    "x-real-ip": "173.245.48.9",
    "cf-connecting-ip": "198.51.100.7",
  });

  assert.deepEqual(got, {
    "x-forwarded-for": "173.245.48.9",
    "x-real-ip": "173.245.48.9",
    "cf-connecting-ip": "198.51.100.7",
  });
});

test("a chain and an IPv6 visitor survive the hop unchanged", () => {
  const got = relay({
    "x-forwarded-for": "198.51.100.9, 173.245.48.9",
    "cf-connecting-ip": "2407:3640:2318:7266::1",
  });

  assert.deepEqual(got, {
    "x-forwarded-for": "198.51.100.9, 173.245.48.9",
    "cf-connecting-ip": "2407:3640:2318:7266::1",
  });
});

test("a request with no client-address header relays nothing (the rule invents nothing)", () => {
  // The rule copies; it never derives a value. A server-side fetch from this
  // container carries no client-address header, so nothing is relayed and the
  // API's own rule decides what that means (it keeps the socket peer).
  //
  // Next's server does synthesise an `x-forwarded-for` from the socket peer
  // before the handler runs when a request arrives without one
  // (next/dist/server/base-server.js, next 16.3.8; measured on the built server),
  // which is why the deployed path never shows this arm: Traefik has already
  // written the real chain by then. What this arm pins is only that THIS code
  // adds nothing of its own when the request carries none.
  assert.deepEqual(relay({ "x-request-id": "abc" }), {});
  assert.deepEqual(relay({}), {});
});

test("an empty header is not relayed as an empty value", () => {
  // `Header.Set("X-Forwarded-For", "")` would send a header the API must then
  // reject as a non-address; absent is the honest shape for "no evidence".
  const got = relay({ "x-forwarded-for": "", "cf-connecting-ip": "198.51.100.7" });
  assert.deepEqual(got, { "cf-connecting-ip": "198.51.100.7" });
});

test("nothing but the client address is copied", () => {
  // The hop's header set is built deliberately. A pass-through of the whole
  // incoming request would hand the API this container's cookie jar, the bearer
  // token, the internal Host — none of which this proxy means to forward.
  const got = relay({
    cookie: "jf_session=secret",
    authorization: "Bearer secret",
    host: "jomform.com",
    "x-forwarded-for": "173.245.48.9",
  });

  assert.deepEqual(got, { "x-forwarded-for": "173.245.48.9" });
});

test("the forwarded set is exactly the headers the API reads from this hop", () => {
  // Named, not counted: the API reads these four and nothing else
  // (middleware/clientip.go). Three resolve the CLIENT ADDRESS; CF-IPCity is
  // display-only — the device-authorisation consent email says where the request
  // came from — and it is listed here because the Go-side guard
  // (dashboard_proxy_clientaddr_test.go) derives the same set from clientip.go
  // and fails if this hop drops one of them. Dropping one — or adding a fifth —
  // silently changes what the API is told about the connection.
  assert.deepEqual([...CLIENT_ADDRESS_HEADERS].sort(), [
    "cf-connecting-ip",
    "cf-ipcity",
    "x-forwarded-for",
    "x-real-ip",
  ]);
});

test("route.ts relays the client address through this rule", () => {
  assert.ok(
    ROUTE.includes("forwardClientAddress(req.headers, headers)"),
    "web/src/app/api/[...path]/route.ts no longer relays the client address: every dashboard request " +
      "reaches the API from THIS container's address, so the API's per-IP rate limit and the 10-failure " +
      "login lockout key on one shared bucket for all browsers (measured on prod: jf:loginfail:ip:172.18.0.6)",
  );
});

test("route.ts still builds the header set it relays into (not a wholesale pass-through)", () => {
  // The control for the assertion above: it must be about a file that proxies
  // into the API's /v1 namespace and builds its OWN headers object, so the rule
  // is used as a hop and not as a copy-everything loop.
  assert.ok(
    ROUTE.includes("/v1"),
    "web/src/app/api/[...path]/route.ts no longer targets the API's /v1 namespace — the assertion above " +
      "would be reading a file that no longer proxies anything",
  );
  assert.ok(
    /const headers = new Headers\(\);/.test(ROUTE),
    "web/src/app/api/[...path]/route.ts no longer builds its own header set for the hop — if the incoming " +
      "headers are now passed through wholesale, Host/Cookie/Authorization reach the API unfiltered",
  );
});
