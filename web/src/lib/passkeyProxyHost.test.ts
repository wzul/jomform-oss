// Card t_ce549b48, web half: the dashboard's /api/* proxy must tell the Go API
// which host the BROWSER is on.
//
// THE DEFECT. web/src/app/api/[...path]/route.ts built a FRESH Headers() for its
// hop (Authorization / Cookie / Content-Type only) and fetched JOMFORM_API_URL,
// so the API received `Host: api:8080` — its own internal address — and the
// passkey (WebAuthn) origin guard, which is keyed on that host, answered 403 to
// the product's own path:
//
//   POST https://jomform.com/api/auth/passkeys/login/start   -> 403 (proxy hop)
//   POST https://api.jomform.com/v1/auth/passkeys/login/start -> 200 (direct)
//
// i.e. passkey sign-in AND registration were dead in every browser while the one
// URL no browser uses kept working. v1.21.52 was rolled back on prod.
//
// WHAT THIS PINS
//   1. The proxy sends the browser's host on as X-Forwarded-Host. Without it the
//      API has nothing true to read for a request that arrived through the proxy.
//   2. The value comes from the connection's own `host` header FIRST. A
//      forwarded header is caller-supplied (a page can set X-Forwarded-Host with
//      fetch), so it is a fallback for a chain that already rewrote Host, never
//      the preferred source.
//   3. The route still proxies into the API's /v1 namespace, so (1) and (2) are
//      not assertions about a file that no longer carries the hop at all.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * It cannot run the route handler: it reads the file as TEXT, the same
//     constraint every src/lib/*.test.ts works under (importing route.ts would
//     need Next's server runtime). The behaviour behind the header — that the API
//     accepts a proxied ceremony — is driven end-to-end in Go by
//     TestPasskeyCeremonySurvivesTheProxyHop
//     (api/internal/httpapi/passkey_rpid_test.go); api/internal/httpapi/
//     passkey_rpid_test.go's TestPasskeyProxyForwardsTheBrowserHost is the same
//     source-level pin read from the API side, so the contract is guarded from
//     both gates.
//   * It says nothing about how Traefik sets Host on the way in; it asserts what
//     this proxy does with the host it was given.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const ROUTE = readFileSync(fileURLToPath(new URL("../app/api/[...path]/route.ts", import.meta.url)), "utf8");

test("the /api proxy forwards the browser's host to the API", () => {
  assert.ok(
    ROUTE.includes('headers.set("X-Forwarded-Host", browserHost)'),
    "web/src/app/api/[...path]/route.ts no longer sends the browser's host to the API as X-Forwarded-Host — " +
      "this proxy replaces Host with the API's own address (JOMFORM_API_URL), so without this header the " +
      "passkey origin guard cannot know the browser's origin and answers 403 to the product's own " +
      "passkeys endpoints, the ones the login and profile pages call under the auth path prefix " +
      "(measured on prod in v1.21.52)",
  );
});

test("the forwarded host is the connection's own Host, not a caller-supplied header", () => {
  const hostIdx = ROUTE.indexOf('req.headers.get("host")');
  const fwdIdx = ROUTE.indexOf('req.headers.get("x-forwarded-host")');
  assert.ok(
    hostIdx >= 0,
    "web/src/app/api/[...path]/route.ts no longer reads req.headers.get(\"host\") — the name the client " +
      "connected to is the value the API must be told",
  );
  assert.ok(
    fwdIdx < 0 || fwdIdx > hostIdx,
    "web/src/app/api/[...path]/route.ts prefers a caller-supplied X-Forwarded-Host over the connection's " +
      "own Host header — a page can set that header with fetch, so it is only worth falling back to",
  );
});

test("the route still carries the API hop it is asserted about", () => {
  assert.ok(
    ROUTE.includes("/v1"),
    "web/src/app/api/[...path]/route.ts no longer targets the API's /v1 namespace — the assertions above " +
      "would be reading a file that no longer proxies anything",
  );
});
