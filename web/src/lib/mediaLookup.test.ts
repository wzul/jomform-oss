// t_b3991178 — the form-settings logo preview must be able to NAME its row.
//
// THE DEFECT this pins: after a reload the page has only `logo_url`, and the
// read it used to resolve that url's mime was the media library page
// (`/api/rest/media?limit=100`, newest first) matched by exact `url`. A logo
// whose row is NOT on that page — older than the newest 100 rows, or a row
// whose advertised host changed after it was picked — therefore stayed
// "unknown mime", and an unknown mime is rendered as an <img>. For a code asset
// on the tenant host that is a broken tile with HTTP 200 and no error anywhere
// (CORP: same-origin), which is why it survived a green suite.
//
// The fix is `GET /api/rest/media?url=<url>`: the server resolves the url to
// the object key and reads the row by key, so the paging window and the
// advertised host stop mattering. These tests cover the two halves — the helper
// that builds/parses that read, and (source scan) the page actually using it
// instead of the window scan, because there is no DOM here and the failure mode
// is silent.

import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { mediaLookupPath, mediaRowFromLookup } from "./mediaLookup.ts";

test("the lookup names the url as a single query parameter, encoded", () => {
  // A stored url can carry its own query (`?v=2`); encoded, it cannot splice a
  // second parameter (e.g. `&limit=`) into the request.
  assert.equal(
    mediaLookupPath("https://ws.jomform.com/media/u/acc/x.svg"),
    "/api/rest/media?url=https%3A%2F%2Fws.jomform.com%2Fmedia%2Fu%2Facc%2Fx.svg",
  );
  assert.equal(
    mediaLookupPath("https://ws.jomform.com/media/u/acc/x.svg?v=2&limit=1"),
    "/api/rest/media?url=https%3A%2F%2Fws.jomform.com%2Fmedia%2Fu%2Facc%2Fx.svg%3Fv%3D2%26limit%3D1",
  );
  // Exactly ONE '?' in the built url: the read is a lookup, not a page scan.
  assert.equal(mediaLookupPath("https://x.example/a.svg").split("?").length, 2);
});

test("the one-row reply yields the mime (and a preview when there is one)", () => {
  assert.deepEqual(
    mediaRowFromLookup({ items: [{ url: "https://ws/media/u/a/x.svg", mime: "image/svg+xml" }], total: 1 }),
    { mime: "image/svg+xml" },
  );
  assert.deepEqual(
    mediaRowFromLookup({ items: [{ mime: "application/pdf", preview: "https://ws/media/u/a/x.p1.jpg" }] }),
    { mime: "application/pdf", preview: "https://ws/media/u/a/x.p1.jpg" },
  );
});

test("an empty reply means 'not a library row', not a failure", () => {
  // The pasted external url case: the page keeps its <img>, which is correct
  // for a url this library does not own.
  assert.equal(mediaRowFromLookup({ items: [], total: 0 }), null);
  // …and a reply that never arrived / was not JSON.
  assert.equal(mediaRowFromLookup(null), null);
  assert.equal(mediaRowFromLookup({}), null);
  assert.equal(mediaRowFromLookup({ items: "nope" }), null);
  // A row without a usable mime cannot be judged by the shared rule.
  assert.equal(mediaRowFromLookup({ items: [{ url: "https://ws/media/u/a/x.svg" }] }), null);
  assert.equal(mediaRowFromLookup({ items: [{ mime: "" }] }), null);
});

test("form-settings resolves the logo row by url, not by scanning a page (t_b3991178)", () => {
  const page = readFileSync(fileURLToPath(new URL("../app/form-settings/page.tsx", import.meta.url)), "utf8");

  assert.match(
    page,
    /import\s*\{[^}]*mediaLookupPath[^}]*mediaRowFromLookup[^}]*\}\s*from\s*"\.\.\/\.\.\/lib\/mediaLookup"/,
    "form-settings no longer imports the single-row lookup — if it went back to scanning the library " +
      "page, a logo older than the newest 100 rows renders as a broken <img> again",
  );

  // THE regression: the list read with a page window, then matched by url.
  assert.doesNotMatch(
    page,
    /\/api\/rest\/media\?limit=/,
    "form-settings is reading a PAGE of the media library for the logo again — that page holds the " +
      "newest 100 rows, so a logo older than them has no mime (broken <img>)",
  );
  assert.doesNotMatch(
    page,
    /\.find\(\(m\)\s*=>\s*m\.url\s*===\s*logoUrl\)/,
    "form-settings is matching the logo by advertised url again — a row whose host changed since it " +
      "was picked equals no advertised url, so the mime stays unknown",
  );

  // And the read it does make must be the by-url one, consumed by the helper.
  assert.ok(
    page.includes("workspaceUrl(mediaLookupPath(logoUrl))"),
    "form-settings does not fetch workspaceUrl(mediaLookupPath(logoUrl))",
  );
  assert.ok(
    page.includes("mediaRowFromLookup(md)"),
    "form-settings does not read the reply through mediaRowFromLookup",
  );
});
