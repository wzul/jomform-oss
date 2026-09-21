// The a11y gate must actually SCAN the docs surfaces (card t_f5a8730d).
//
// THE BUG. `web/e2e/a11y.spec.ts` is the axe scan run by the a11y gate
// (`npm run a11y` = `playwright test` over `playwright.config.ts` testDir
// "./e2e"), and at the time of the report its PAGES list held the seven public
// routes with no `/docs/*` path. So PR 676 could edit
// `web/src/app/docs/customize/page.tsx` and a full `npm run a11y` run would
// still pass 7/7 without ever looking at the changed page — the docs knowledge
// base was outside the scan. (Other specs have since been added to ./e2e; this
// one is still the only place the axe PAGE LIST lives.) Measured in a real
// browser against this tree's own production build, all 11 docs routes carried
// at least one critical/serious axe node (color-contrast on the sidebar label,
// plus landmark-unique, plus link-in-text-block on the pages with an in-prose
// cross-link, plus the scrollable <pre>).
//
// WHY A SOURCE-CONTRACT TEST AND NOT A BROWSER TEST. This repo has no DOM/JSX
// unit runner (no vitest/jest — see src/lib/buttonWrap.test.ts and
// src/lib/chatPage.test.ts, written the same way). What CANNOT regress silently
// is the two-part contract that makes the gate meaningful:
//
//   (a) COVERAGE: every docs page that exists is a page the scan visits. A page
//       directory added later without a PAGES entry is a page outside the gate
//       again, and nothing else would notice — that is the exact bug here. It
//       caught main's new /docs/builder on the merge of this very branch.
//   (b) THE FIXES THE ENLARGED GATE DEPENDS ON: each docs `<pre>` that can
//       scroll sideways stays keyboard reachable, and each in-prose
//       cross-page link stays distinguishable without relying on colour.
//       Without these the enlarged gate is red on arrival.
//
// The colour-contrast rule is NOT pinned here: no source-level scan can tell
// slate-400 (2.63:1) from slate-600 (7.58:1) without re-implementing axe's
// compositing, and axe itself is what the gate runs. Its fix is asserted
// indirectly — `text-slate-400` is checked out of the docs tree entirely, which
// is the shape of the change. The real proof is the gate run in the PR evidence.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";

const pathOf = (rel: string) => fileURLToPath(new URL(rel, import.meta.url));

const SPEC = readFileSync(pathOf("../../e2e/a11y.spec.ts"), "utf8");
const DOCS_DIR = pathOf("../app/docs");

// Route -> source file. app/docs/layout.tsx is the shared chrome, not a route.
const DOCS: { route: string; file: string }[] = readdirSync(DOCS_DIR)
  .filter((e) => statSync(`${DOCS_DIR}/${e}`).isDirectory())
  .map((e) => ({ route: `/docs/${e}`, file: `${DOCS_DIR}/${e}/page.tsx` }))
  .concat({ route: "/docs", file: pathOf("../app/docs/page.tsx") })
  .sort((a, b) => a.route.localeCompare(b.route));

const LAYOUT_FILE = pathOf("../app/docs/layout.tsx");

// Paths the scan visits, read out of the real spec file.
const scannedPaths = [...SPEC.matchAll(/path:\s*"([^"]+)"/g)].map((m) => m[1]);

// Every opening `<pre …>` / `<a …>` tag in a file. Matching the OPENING TAG (not
// the whole element) is what stops a className on an unrelated element from
// satisfying a rule. `<Link>` is deliberately not read: next/link renders an
// anchor carrying the caller's class, and the docs pages use it for
// button-styled cards, not for in-prose links.
function tags(src: string, name: string): string[] {
  return [...src.matchAll(new RegExp(`<${name}\\b[^>]*>`, "g"))].map((m) => m[0]);
}

// Exact class TOKEN match. A substring check (`.includes("underline")`) would
// pass `hover:underline`, which is the pre-fix shape this card removes.
function hasClass(tag: string, token: string): boolean {
  const m = tag.match(/className=(?:"([^"]*)"|\{`([^`]*)`\})/);
  if (!m) return false;
  return (m[1] ?? m[2] ?? "").split(/\s+/).includes(token);
}

test("the guard is looking at real files (it cannot pass vacuously)", () => {
  assert.ok(SPEC.includes("AxeBuilder"), "e2e/a11y.spec.ts is not the axe spec");
  assert.ok(scannedPaths.length >= 7, `only ${scannedPaths.length} scanned paths were parsed out of the spec`);
  assert.ok(DOCS.length >= 11, `expected the docs tree, found ${DOCS.length} routes`);
  assert.ok(
    DOCS.some((d) => d.route === "/docs") && DOCS.some((d) => d.route === "/docs/customize"),
    "the docs tree is not the one this guard was written against",
  );
  for (const d of DOCS) {
    assert.ok(readFileSync(d.file, "utf8").length > 0, `${d.file} could not be read`);
  }
  assert.ok(LAYOUT_FILE.endsWith("docs/layout.tsx"));
});

test("the scan visits the seven public routes it always did (nothing was dropped to make room)", () => {
  for (const p of [
    "/login",
    "/landing",
    "/reset-password",
    "/magic-link",
    "/brand",
    "/legal/privacy",
    "/legal/terms",
  ]) {
    assert.ok(scannedPaths.includes(p), `the a11y gate no longer scans ${p}`);
  }
});

test("every docs page is scanned — including the one a docs PR edits", () => {
  const missing = DOCS.filter((d) => !scannedPaths.includes(d.route)).map((d) => d.route);
  assert.deepEqual(
    missing,
    [],
    `docs route(s) outside the a11y gate: ${missing.join(", ")} — a page not in PAGES cannot be checked by the gate`,
  );
  assert.ok(
    scannedPaths.includes("/docs/customize"),
    "/docs/customize must be scanned: it is the page a docs PR actually edits",
  );
});

test("no route is listed twice in the scan", () => {
  const dupes = scannedPaths.filter((p, i) => scannedPaths.indexOf(p) !== i);
  assert.deepEqual(dupes, [], `duplicate PAGES entries: ${dupes.join(", ")}`);
});

test("every sideways-scrolling docs <pre> is keyboard reachable (axe scrollable-region-focusable)", () => {
  let checked = 0;
  for (const d of DOCS) {
    const src = readFileSync(d.file, "utf8");
    for (const tag of tags(src, "pre")) {
      if (!hasClass(tag, "overflow-x-auto")) continue;
      checked++;
      assert.ok(
        /\btabIndex=/.test(tag),
        `${d.route}: a scrollable <pre> has no tabIndex — axe fails it with scrollable-region-focusable (serious)`,
      );
    }
  }
  assert.ok(checked > 0, "no overflow-x-auto <pre> was found, so this rule proved nothing");
});

test("every in-prose docs cross-page link carries a permanent underline (axe link-in-text-block)", () => {
  let checked = 0;
  for (const d of DOCS) {
    const src = readFileSync(d.file, "utf8");
    for (const tag of tags(src, "a")) {
      const href = tag.match(/href=(?:"([^"]*)"|\{`([^`]*)`\})/);
      const target = href?.[1] ?? href?.[2];
      // A cross-page docs link rendered as plain text: not a hash anchor, not a
      // route that is not docs, not a URL built at runtime.
      if (!target || !target.startsWith("/docs")) continue;
      checked++;
      assert.ok(
        hasClass(tag, "underline"),
        `${d.route}: <a href="${target}"> has no bare \`underline\` class — axe fails link-in-text-block (serious); hover:underline is not enough`,
      );
    }
  }
  assert.ok(checked > 0, "no in-prose cross-page docs link was found, so this rule proved nothing");
});

test("no docs page or the docs chrome still carries text-slate-400 (2.63:1 at 11px)", () => {
  // Read class TOKENS out of every className in the file, not the raw text, so
  // a comment that names the old class cannot be mistaken for a live usage.
  const classesIn = (src: string): string[] =>
    [...src.matchAll(/className=(?:"([^"]*)"|\{`([^`]*)`\})/g)].flatMap((m) =>
      (m[1] ?? m[2] ?? "").split(/\s+/),
    );
  const offenders: string[] = [];
  let checked = 0;
  for (const file of [LAYOUT_FILE, ...DOCS.map((d) => d.file)]) {
    const tokens = classesIn(readFileSync(file, "utf8"));
    checked += tokens.length;
    if (tokens.includes("text-slate-400")) offenders.push(file);
  }
  assert.ok(checked > 0, "no className was found, so this rule proved nothing");
  assert.deepEqual(
    offenders,
    [],
    `text-slate-400 is 2.63:1 on the white page — a serious color-contrast failure: ${offenders.join(", ")}`,
  );
});
