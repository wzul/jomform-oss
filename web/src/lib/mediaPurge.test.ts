// t_cb4e3d99 — the /media purge toast must not claim more than the API confirmed.
//
// THE DEFECT, measured on prod (v1.21.228): the app's own route answered
//
//   DELETE /api/rest/media/{id}/purge  ->  200
//   {"edge":"not_configured","edge_max_age_s":300,...,"status":"purged"}
//
// and the UI, at that moment, said "File deleted for good." The page read
// `res.ok` and nothing else, so the one fact that decides whether that sentence
// is true — the edge outcome — was thrown away. `edge_max_age_s` is present
// EXACTLY when the CDN copy was not cleared (api/internal/httpapi/media.go), so
// a `not_configured` purge still leaves a URL that can answer for 300 s.
//
// WHAT THIS PINS
//   1. The decision (`purgeNotice`) is only allowed to reach the plain
//      "deleted for good" key for `edge: purged` (or a zero window). The prod
//      body above must reach the key that NAMES the residual seconds.
//   2. Every dictionary key it can return exists in all five locales and names
//      the edge (CDN) in each — a translation that says "gone" without naming
//      where the copy lives is the same false claim in another language.
//   3. The rendered sentence for the prod body contains 300 and is NOT the
//      plain line — i.e. what a merchant reads changes with the response.
//   4. `purge()` in src/app/media/page.tsx actually consumes the JSON body and
//      routes the notice into setNotify. This is the assertion that fails
//      against the pre-fix tree (`setNotify(t("media.purged"))`, no json read):
//      the helper alone would be a library nobody calls, which is the shape of
//      the original defect.
//
// WHAT IT DOES NOT COVER
//   * Translation quality — a fluent review is the only judge of that.
//   * Whether a `edge: purged` response TRULY cleared the CDN; Cloudflare's 200
//     means accepted. That is measured by scripts/media-purge-probe.sh, not here.
//   * The browser's own copy of an already-fetched image. The copy here names
//     the CDN window (`edge_max_age_s`); the client-side longer window comes
//     from the zone's Browser Cache TTL, which this card deliberately does not
//     touch.

import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { purgeNotice, type PurgeNoticeKey } from "./mediaPurge.ts";
import { DICT, LANGS } from "../components/i18nDict.ts";

const here = path.dirname(fileURLToPath(import.meta.url));
const PAGE_SRC = readFileSync(path.join(here, "..", "app", "media", "page.tsx"), "utf8");

// The exact body prod returned for the purge that exposed this defect. Pinned
// verbatim so a change in the API's field names cannot quietly turn the warning
// into the optimistic branch.
const PROD_BODY = {
  status: "purged",
  media_id: "b07d31b6-8b17-4131-9bc2-3db606318193",
  key: "u/d9ee2f12-0000-0000-0000-000000000000/771b31ab-0000-0000-0000-000000000000.jpg",
  edge: "not_configured",
  edge_max_age_s: 300,
};

// Mirrors t() in src/components/i18n.tsx (same `{name}` substitution), so the
// assertions below test the sentence a merchant actually reads.
function render(lang: "en" | "ms" | "zh" | "ta" | "ar", key: PurgeNoticeKey, params?: Record<string, string | number>): string {
  const entry = DICT[key] as Record<string, string> | undefined;
  assert.ok(entry, `${key} is missing from i18nDict.ts — the toast would render the raw key`);
  let s = entry[lang] || entry.en;
  for (const [k, v] of Object.entries(params ?? {})) s = s.replaceAll(`{${k}}`, String(v));
  return s;
}

// purge()'s own source, from its signature to the next function in the file.
// Reading the handler (not the whole page) keeps the assertion about THIS call
// site: `setNotify(t("media.purged"))` may legitimately still appear elsewhere.
function purgeHandlerSrc(): string {
  const start = PAGE_SRC.indexOf("async function purge()");
  assert.ok(start >= 0, "purge() is missing from src/app/media/page.tsx — this guard has nothing to check");
  const rest = PAGE_SRC.slice(start);
  const end = rest.indexOf("\n  async function ", 1);
  return end > 0 ? rest.slice(0, end) : rest;
}

test("the prod purge body reaches the message that names the residual window", () => {
  const notice = purgeNotice(PROD_BODY);
  assert.equal(notice.key, "media.purgedEdgeStale", "a not_configured purge must NOT claim the file is gone everywhere");
  assert.deepEqual(notice.params, { seconds: 300 });
});

test("only a confirmed edge purge may use the plain 'deleted for good' line", () => {
  assert.equal(purgeNotice({ ...PROD_BODY, edge: "purged", edge_max_age_s: undefined }).key, "media.purged");
  for (const edge of ["not_configured", "no_urls", "out_of_zone", "failed"]) {
    const notice = purgeNotice({ edge, edge_max_age_s: 300 });
    assert.notEqual(notice.key, "media.purged", `edge=${edge} leaves a CDN copy, so the plain line is a false claim`);
  }
});

test("a body we cannot read falls back to the caveat, never to the plain claim", () => {
  // A proxy that strips the body, an older API build, a truncated response.
  for (const body of [null, undefined, {}, "purged", 200, { status: "purged" }, { edge_max_age_s: "nope" }]) {
    assert.equal(
      purgeNotice(body).key,
      "media.purgedEdgeUnnamed",
      `body ${JSON.stringify(body)} does not prove the CDN copy is gone, so the toast must say so`,
    );
  }
});

test("a string edge_max_age_s is still named (JSON from another layer)", () => {
  const notice = purgeNotice({ edge: "failed", edge_max_age_s: "300" });
  assert.equal(notice.key, "media.purgedEdgeStale");
  assert.deepEqual(notice.params, { seconds: 300 });
});

test("a zero residual window keeps the plain line: there is nothing to warn about", () => {
  assert.equal(purgeNotice({ edge: "not_configured", edge_max_age_s: 0 }).key, "media.purged");
});

test("the rendered toast for the prod body names 300 seconds and is not the old line", () => {
  const notice = purgeNotice(PROD_BODY);
  const plain = render("en", "media.purged");
  const shown = render("en", notice.key, notice.params);
  assert.notEqual(shown, plain, "the toast a merchant sees must differ from 'File deleted for good.'");
  assert.match(shown, /300/, "the residual window has to reach the text, not just the state");
  assert.match(shown, /CDN/, "the sentence must name where the surviving copy lives");
  for (const lang of LANGS) {
    const localized = render(lang, notice.key, notice.params);
    assert.match(localized, /300/, `${lang}: the seconds placeholder must be substituted`);
    assert.notEqual(localized, render(lang, "media.purged"), `${lang}: must not be the plain claim`);
  }
});

test("the copy keys exist in all five locales, name the edge, and the confirm body admits the copy", () => {
  for (const key of ["media.purgedEdgeStale", "media.purgedEdgeUnnamed"] as const) {
    const entry = DICT[key] as Record<string, string>;
    for (const lang of LANGS) {
      assert.ok(entry[lang] && entry[lang].trim() !== "", `${key}.${lang} is missing`);
      assert.match(entry[lang], /CDN/, `${key}.${lang} must name where the surviving copy lives`);
    }
  }
  // The confirm dialog is read BEFORE the outcome is known, so it cannot name a
  // window — but it must not imply the URL dies everywhere the moment you click.
  const confirm = DICT["media.purgeConfirmBody"] as Record<string, string>;
  for (const lang of LANGS) assert.match(confirm[lang], /CDN/, `media.purgeConfirmBody.${lang} must admit the cached copy`);
});

test("purge() reads the purge response body (res.ok alone drops the edge outcome)", () => {
  const src = purgeHandlerSrc();
  assert.match(src, /res\.json\(\)/, "purge() must read the body; res.ok alone drops the edge outcome (the measured defect)");
  assert.match(src, /purgeNotice\(\s*body\s*\)/, "purge() must run the parsed body through purgeNotice()");
});

test("the toast renders the notice, not the constant 'deleted for good' line", () => {
  const src = purgeHandlerSrc();
  assert.match(
    src,
    /setNotify\(\s*t\(\s*notice\.key\s*,\s*notice\.params\s*\)\s*\)/,
    "the toast must render the notice's key and params, not a constant",
  );
  assert.doesNotMatch(
    src,
    /setNotify\(\s*t\(\s*"media\.purged"\s*\)\s*\)/,
    "the constant 'File deleted for good.' toast is the defect this card fixes",
  );
  assert.match(
    PAGE_SRC,
    /import\s*\{\s*purgeNotice\s*\}\s*from\s*"\.\.\/\.\.\/lib\/mediaPurge"/,
    "the page must import the decision from src/lib/mediaPurge.ts",
  );
});
