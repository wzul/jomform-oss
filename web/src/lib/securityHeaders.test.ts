// Card t_1a491389 — the security-header contract, pinned at the CONFIG Next
// actually loads, so a silent change is visible in the fast gate (`npm run
// build` runs test:unit before `next build`), not only in the e2e family.
//
// WHY BOTH HALVES EXIST. This file proves the config Next imports really
// carries every header on the paths a visitor reaches — a source-text scan
// would pass over a headers() that is never returned, or a rule whose `source`
// matches nothing. `web/e2e/security-headers.spec.ts` proves the headers reach
// a REAL response from the built app; a config-shaped assertion cannot see a
// proxy that strips a value. Neither is redundant: this one runs in seconds on
// every `npm run build`, the other runs in the `e2e` gate.
//
// THE DEFECT (measured on prod v1.29.0, 2026-11-09): `curl -sSI
// https://jomform.com/` returned `x-powered-by: Next.js` and none of
// X-Content-Type-Options / X-Frame-Options / Referrer-Policy /
// Permissions-Policy / CSP / HSTS, while the Go API answered /healthz with all
// four of its own (api/internal/httpapi/middleware/secure.go). The web config's
// headers() only set Cache-Control for /_next/static.
//
// WHAT IS PINNED
//   1. `poweredByHeader` is `false` on the IMPORTED config — not defaulted.
//   2. Every baseline header is present, with the agreed value, on `/`, `/docs`
//      and a dashboard route, resolved through headers()' own `source` patterns
//      (a rule that matches no path is caught here, not in prod).
//   3. The static Cache-Control rule still applies — the new catch-all must not
//      replace it, and Next applies EVERY matching rule.
//   4. Framing is never widened: no ALLOWALL / wildcard / `https:` ancestor.
//   5. No script directive is set: a `default-src`/`script-src` here would need a
//      per-response nonce, which this layer cannot produce (see next.config.mjs).
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { fileURLToPath, pathToFileURL } from "node:url";

const NEXT_CONFIG = fileURLToPath(new URL("../../next.config.mjs", import.meta.url));

type Rule = { source: string; headers: { key: string; value: string }[] };

const config = (await import(pathToFileURL(NEXT_CONFIG).href)).default as {
  poweredByHeader?: boolean;
  headers?: () => Promise<Rule[]>;
};

/** Resolve Next's `source` patterns (`/:path*`, `/_next/static/:path*`) against
 *  a pathname, the way the router does: `:path*` is a greedy tail, `:path` is
 *  one segment. Deliberately small — a pattern this cannot parse THROWS rather
 *  than silently matching nothing, so a future rule cannot go unmeasured. */
function matches(source: string, pathname: string): boolean {
  const known = new Set([":path*", ":path"]);
  const segments = source.split("/");
  for (const seg of segments) {
    if (seg.startsWith(":") && !known.has(seg)) {
      throw new Error(`headers() source ${JSON.stringify(source)} uses an unsupported pattern segment ${seg}`);
    }
  }
  const re =
    "^" +
    segments
      .map((seg) => {
        if (seg === ":path*") return ".*";
        if (seg === ":path") return "[^/]+";
        return seg.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
      })
      .join("/") +
    "$";
  return new RegExp(re).test(pathname);
}

/** The headers a real request to `pathname` would receive, lowercased. */
async function headersFor(pathname: string): Promise<Record<string, string>> {
  assert.equal(typeof config.headers, "function", "web/next.config.mjs exports no headers() — nothing would be sent");
  const rules = await config.headers!();
  const out: Record<string, string> = {};
  for (const rule of rules) {
    if (!matches(rule.source, pathname)) continue;
    for (const { key, value } of rule.headers) out[key.toLowerCase()] = value;
  }
  return out;
}

const PAGES = ["/", "/docs", "/forms", "/login"];

test("poweredByHeader is off on the config Next loads", () => {
  assert.equal(
    config.poweredByHeader,
    false,
    "x-powered-by advertises the framework and its major version on every response; " +
      "prod v1.29.0 answered with only that header present (card t_1a491389)",
  );
});

test("every page carries the baseline security headers", async () => {
  for (const path of PAGES) {
    const h = await headersFor(path);
    assert.equal(h["x-content-type-options"], "nosniff", `${path}: X-Content-Type-Options`);
    assert.equal(h["x-frame-options"], "SAMEORIGIN", `${path}: X-Frame-Options`);
    assert.equal(
      h["referrer-policy"],
      "strict-origin-when-cross-origin",
      `${path}: Referrer-Policy (the API's own value)`,
    );
    assert.match(h["permissions-policy"] ?? "", /(^|,\s*)camera=\(\)/, `${path}: camera must be denied`);
    assert.match(h["permissions-policy"] ?? "", /(^|,\s*)microphone=\(\)/, `${path}: microphone must be denied`);
    assert.match(h["permissions-policy"] ?? "", /(^|,\s*)geolocation=\(\)/, `${path}: geolocation must be denied`);
    assert.match(h["strict-transport-security"] ?? "", /max-age=\d{6,}/, `${path}: HSTS max-age`);
    assert.match(
      h["content-security-policy"] ?? "",
      /(^|;)\s*frame-ancestors 'self'/,
      `${path}: CSP frame-ancestors`,
    );
  }
});

test("the Permissions-Policy leaves the clipboard alone", async () => {
  // 8 call sites use navigator.clipboard.writeText (Copy link / Copy code on
  // /builder, /media, /chat, /mcp, /brand, /profile). `clipboard-write=()` would
  // turn every one of them into a no-op that only surfaces in a browser.
  const policy = (await headersFor("/forms"))["permissions-policy"] ?? "";
  assert.doesNotMatch(policy, /clipboard-write/);
});

test("framing is restricted and never widened", async () => {
  for (const path of PAGES) {
    const h = await headersFor(path);
    assert.doesNotMatch(h["x-frame-options"], /ALLOWALL|ALLOW-FROM/, `${path}: framing was widened`);
    for (const m of (h["content-security-policy"] ?? "").matchAll(/frame-ancestors([^;]*)/gi)) {
      assert.doesNotMatch(m[1], /\*|https:/, `${path}: a wildcard/scheme-only ancestor lets any site frame us`);
    }
  }
});

test("no script directive is set — a nonce this layer cannot produce would break hydration", async () => {
  for (const path of PAGES) {
    const csp = (await headersFor(path))["content-security-policy"] ?? "";
    assert.doesNotMatch(
      csp,
      /(^|;)\s*(default-src|script-src|style-src|img-src|connect-src)/,
      `${path}: a script/style directive in next.config.mjs cannot be nonce-based (headers() is static, ` +
        `one value per build) — it would either block the RSC bootstrap ("self.__next_f.push") or ` +
        `force 'unsafe-inline', which is theatre`,
    );
  }
});

test("the immutable static cache header still applies", async () => {
  const h = await headersFor("/_next/static/chunks/abc123.js");
  assert.match(h["cache-control"] ?? "", /immutable/, "/_next/static/* lost its Cache-Control when the catch-all was added");
  assert.equal(h["x-content-type-options"], "nosniff", "/_next/static/* must carry nosniff too");
});
