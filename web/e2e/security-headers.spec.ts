// Card t_1a491389 — every HTML response from the Next.js app must carry the
// baseline security headers, and must not advertise the stack.
//
// THE DEFECT (measured on prod v1.29.0, 2026-11-09):
//
//   $ curl -sSI https://jomform.com/
//   cache-control: private, no-cache, no-store, max-age=0, must-revalidate
//   vary: rsc, next-router-state-tree, ...
//   x-powered-by: Next.js
//
// ...and NOTHING else — no X-Content-Type-Options, no X-Frame-Options, no
// Referrer-Policy, no Permissions-Policy, no CSP, no HSTS — while the Go API on
// the same product answers /healthz with all four of those
// (`api/internal/httpapi/middleware/secure.go`). `web/next.config.mjs`'s
// headers() only set Cache-Control for /_next/static, and `poweredByHeader` was
// left at Next's default of `true`, so the banner line was the ONLY security
// signal a scanner saw.
//
// WHY THIS SPEC AND NOT ONLY A UNIT TEST. A unit test can read the config's
// header LIST, but the defect is a property of a real HTTP response from the
// BUILT app: a config block that Next never applies (a source pattern that
// matches nothing, a headers() that throws and is swallowed, a proxy that
// strips the value) reads as green in a config-shaped assertion. So this spec
// makes real requests against the production build playwright starts
// (`next start`, playwright.config.ts webServer) and reads the headers off the
// responses. It is the same harness as `ssr-gate-next-origin.spec.ts`.
//
// WHAT IS NOT ASSERTED HERE, DELIBERATELY: that `/embed/f/...` stays framable.
// That route is served by the Go API, never by this app — Caddy routes
// `@api path ... /embed/*` (deploy/selfhost/Caddyfile) and Traefik's
// `jf-api` rule carries `PathPrefix(`/embed/`)` (deploy/vps/traefik-jomform.yml)
// — and its permissive framing (`X-Frame-Options: ALLOWALL` +
// `frame-ancestors *`) is already pinned in Go by
// `api/internal/httpapi/embed_docs_snippet_test.go`. The arms below therefore
// pin the app's own value AND the fact that it was not widened to ALLOWALL to
// "fix" an embed nobody serves here.
import { test, expect } from "@playwright/test";

/** Routes a visitor reaches with no session: prerendered (/), static-with-RSC
 *  (/docs) and dynamic (/login). One per rendering path, so a headers() block
 *  that only reaches prerendered output is visible. */
const PAGES = ["/", "/docs", "/login"] as const;

/** header name -> what a correct value satisfies. Kept as data so a new header
 *  is one line, and so each failure message can name the consequence. */
const BASELINE: { header: string; ok: (v: string) => boolean; want: string }[] = [
  {
    header: "x-content-type-options",
    ok: (v) => v === "nosniff",
    want: `exactly "nosniff"`,
  },
  {
    header: "x-frame-options",
    ok: (v) => v === "SAMEORIGIN",
    want: `exactly "SAMEORIGIN" (this app never frames a public form; /embed/* is the API's)`,
  },
  {
    header: "referrer-policy",
    ok: (v) => v === "strict-origin-when-cross-origin",
    want: `exactly "strict-origin-when-cross-origin" (the API's value)`,
  },
  {
    header: "permissions-policy",
    // Deny-by-default for the sensors this app never asks for. `clipboard-write`
    // is NOT in the list on purpose: 8 call sites use navigator.clipboard.
    ok: (v) => /(^|,\s*)camera=\(\)/.test(v) && /(^|,\s*)microphone=\(\)/.test(v) && /(^|,\s*)geolocation=\(\)/.test(v),
    want: `camera=(), microphone=() and geolocation=() all denied`,
  },
  {
    header: "strict-transport-security",
    ok: (v) => /(^|;)\s*max-age=\d{6,}/.test(v),
    want: `a max-age of at least 1000000 seconds`,
  },
  {
    header: "content-security-policy",
    ok: (v) => /(^|;)\s*frame-ancestors 'self'/.test(v),
    want: `frame-ancestors 'self' (the one directive we can set without touching scripts)`,
  },
];

test("every page a visitor can reach answers the baseline security headers", async ({ request }) => {
  const missing: string[] = [];
  for (const path of PAGES) {
    const res = await request.get(path);
    expect(res.status(), `${path} did not answer 200 — this arm is about its headers`).toBe(200);
    const headers = res.headers();
    for (const { header, ok, want } of BASELINE) {
      const value = headers[header];
      if (value === undefined) {
        missing.push(`${path}: ${header} is ABSENT (want ${want})`);
        continue;
      }
      if (!ok(value)) missing.push(`${path}: ${header}: ${JSON.stringify(value)} — want ${want}`);
    }
  }
  expect(
    missing,
    `the built app answered without the baseline security headers. Measured on prod ` +
      `v1.29.0: only "x-powered-by: Next.js" was present. Missing/wrong:\n  ` + missing.join("\n  "),
  ).toEqual([]);
});

test("the stack is not advertised", async ({ request }) => {
  const seen: string[] = [];
  for (const path of PAGES) {
    const res = await request.get(path);
    const powered = res.headers()["x-powered-by"];
    if (powered !== undefined) seen.push(`${path}: x-powered-by=${JSON.stringify(powered)}`);
  }
  expect(
    seen,
    `x-powered-by names the framework on every response, which is free reconnaissance ` +
      `for a scanner picking a known CVE. It is removed by poweredByHeader: false in ` +
      `web/next.config.mjs. Seen: ${seen.join(", ") || "(none)"}`,
  ).toEqual([]);
});

test("framing is restricted to this origin and never widened to a wildcard", async ({ request }) => {
  const widened: string[] = [];
  for (const path of PAGES) {
    const res = await request.get(path);
    const headers = res.headers();
    const xfo = headers["x-frame-options"] ?? "";
    const csp = headers["content-security-policy"] ?? "";
    if (/ALLOWALL|ALLOW-FROM/i.test(xfo)) widened.push(`${path}: X-Frame-Options=${JSON.stringify(xfo)}`);
    // A wildcard or a scheme-only source in frame-ancestors would let ANY site
    // frame the dashboard — the clickjacking hole this header exists to close.
    for (const m of csp.matchAll(/frame-ancestors([^;]*)/gi)) {
      if (/\*|https:/.test(m[1])) widened.push(`${path}: CSP frame-ancestors${m[1]}`);
    }
  }
  expect(
    widened,
    `the app's framing policy was widened with no change to the Go API's embed route ` +
      `(which is the only framable surface). Widened: ${widened.join("; ") || "(none)"}`,
  ).toEqual([]);
});

test("the static cache header survives the new security-header block", async ({ request }) => {
  const home = await request.get("/");
  expect(home.status()).toBe(200);
  const html = await home.text();
  const src = html.match(/src="(\/_next\/static\/[^"]+\.js)"/)?.[1];
  expect(src, "the built home page referenced no /_next/static/*.js chunk").toBeTruthy();

  const asset = await request.get(src!);
  expect(asset.status(), `${src} did not answer 200`).toBe(200);
  const headers = asset.headers();
  expect(
    headers["cache-control"] ?? "",
    `a content-hashed chunk lost its immutable Cache-Control when the security-header block ` +
      `was added — headers() rules all apply, so the two entries must not overwrite each other`,
  ).toContain("immutable");
  expect(
    headers["x-content-type-options"],
    `/_next/static/* is served by the same app; nosniff is the one header that matters most ` +
      `on a .js response and it must reach assets too`,
  ).toBe("nosniff");
});
