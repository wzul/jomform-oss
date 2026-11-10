import { test, expect } from "@playwright/test";

// The BUILT image's half of the `/_next/static/*` Cache-Control contract
// (card t_d101a943).
//
// WHY THIS EXISTS. `web/src/lib/devHosts.test.ts` guards the header that
// web/next.config.mjs DECLARES: it evaluates the config module under a given
// NODE_ENV and reads the value the module returns. That cannot be the whole
// contract, and the guard's own comment said so — it pointed at
// `web/src/lib/staticAssetCache.test.ts` as "the built-image side of this
// contract", and no such file has ever existed in this repo (`git log --all --
// '*staticAssetCache*'` finds nothing; only that comment ever named it). So the
// half that a reader of the site is actually served — what a RUNNING built
// server answers — had no test at all.
//
// WHAT IT PINS, against the app this gate itself serves (`npm run start`, the
// same production server web/playwright.config.ts starts for the a11y and
// feature-core gates):
//   1. An asset under `/_next/static/` that the served document really
//      references answers 200 with `Cache-Control: public, max-age=31536000,
//      immutable`.
//   2. A DOCUMENT route does not carry that value. The config's rule is scoped
//      to `/_next/static/:path*`; a rule widened to every path would put a
//      year-long immutable header on HTML that names a fresh chunk on every
//      deploy — and the unit guard cannot see the scope at all, because it
//      reads `rules[0]`'s Cache-Control VALUE and never the `source` that value
//      was declared under.
//
// The asset path is read OUT OF THE SERVED DOCUMENT rather than typed here, so
// this cannot pass over a stale filename, and the first test FAILS naming the
// reason when the document references no such asset — a guard that reads
// nothing proves nothing.
//
// MEASURED on this host against the built server, 2026-10-06: the chunk
// `/_next/static/chunks/3yfl9fgvivx3-.js` that `/landing` references answered
// `HTTP/1.1 200` with `Cache-Control: public, max-age=31536000, immutable`,
// while `/landing` itself answered `Cache-Control: s-maxage=31536000` — no
// `immutable` in it. The two readings therefore differ on the real server, so
// the assertions below are not the same assertion stated twice.
//
// WHAT IT DOES NOT COVER (stated, not implied): it does not start a DEV server,
// so it does not judge the `no-store` dev branch. That value is env-driven and
// belongs to the unit guard; no dev server runs in this gate. It reads the
// header the origin emits; it does not follow a CDN (Cloudflare re-serves the
// origin's own header, and the origin is what this measures).

// An asset the document loads. Deliberately a plain regex, not a list of
// filenames: a list would be a hand-copy that goes stale on the next build.
const STATIC_ASSET_IN_DOCUMENT = /\/_next\/static\/[A-Za-z0-9_./-]*\.js/;

const DOCUMENT_ROUTE = "/landing";

async function assetPathFromDocument(request: import("@playwright/test").APIRequestContext) {
  const response = await request.get(DOCUMENT_ROUTE);
  expect(
    response.status(),
    `${DOCUMENT_ROUTE} answered ${response.status()} — this test cannot read the document it ` +
      `derives the asset path from, so it would prove nothing about the deployed header`,
  ).toBe(200);
  const html = await response.text();
  const found = html.match(STATIC_ASSET_IN_DOCUMENT)?.[0];
  expect(
    found,
    `the document served for ${DOCUMENT_ROUTE} references no /_next/static/*.js asset, so the ` +
      `asset-path half of this test cannot be derived. Either the build emits its chunks ` +
      `elsewhere (then update this pattern) or the page no longer loads a script — do not let ` +
      `this pass as "the header is right".`,
  ).toBeTruthy();
  return found as string;
}

test("the served document references a /_next/static asset (premise)", async ({ request }) => {
  const asset = await assetPathFromDocument(request);
  expect(asset.startsWith("/_next/static/"), `${asset} is not under /_next/static/`).toBe(true);
});

test("a built /_next/static asset answers with the year-long immutable header", async ({
  request,
}) => {
  const asset = await assetPathFromDocument(request);
  const response = await request.get(asset);
  expect(response.status(), `${asset} did not serve — the header below would be a 404's`).toBe(200);
  expect(
    (response.headers()["cache-control"] ?? "").trim(),
    `${asset} must be served with the immutable header. Its filenames are content-hashed, so a ` +
      `built asset never changes for a given build, and losing this header to fix the dev case ` +
      `would trade one caching defect for another.`,
  ).toBe("public, max-age=31536000, immutable");
});

test("the immutable header is scoped to /_next/static — a document does not carry it", async ({
  request,
}) => {
  const response = await request.get(DOCUMENT_ROUTE);
  expect(response.status()).toBe(200);
  const cacheControl = (response.headers()["cache-control"] ?? "").trim();
  expect(
    cacheControl,
    `${DOCUMENT_ROUTE} is served with the ASSET header (${cacheControl}). The rule in ` +
      `web/next.config.mjs is scoped to \`/_next/static/:path*\`; widened to every path it would ` +
      `cache every HTML document for a year, and the document names the chunks of one deploy.`,
  ).not.toContain("immutable");
  expect(cacheControl).not.toContain("public, max-age=31536000");
});
