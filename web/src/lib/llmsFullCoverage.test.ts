// llms-full.txt coverage guard (t_f8015915; widened to the pages on disk by t_96fb6ab1).
//
// WHY THIS EXISTS
//   web/public/llms-full.txt is the machine-readable context AI agents fetch. Its
//   "Key pages" list is how an agent learns a guide EXISTS — a page missing from it
//   is a page the agent will not read, and no build step, type or e2e run notices.
//   /docs/security (two-factor, passkeys, API-key scopes) was added by PR 675 and
//   was absent from that list from then until this guard: llms-full.txt named ten
//   guides while the /docs index linked eleven, so the gap was invisible.
//
//   The first version of this guard compared llms-full.txt against the /docs
//   INDEX's links only, and left the reverse unpinned on purpose. That scoping was
//   right for /docs/security (the index links it) and wrong for the three guides
//   the index deliberately does NOT link: /docs/builder, /docs/chat and
//   /docs/feedback are reached from their own feature's inline "?" (recorded in
//   NOT_INDEXED_BY_DESIGN in docsIndex.test.ts), so BOTH sides of the index-scoped
//   comparison were 14 and it stayed green while three real pages were unnamed
//   (t_96fb6ab1). The comparison that sees them is against the pages ON DISK, which
//   is the first check below.
//
// WHAT IT PINS
//   1. (disk -> named) Every real page under app/docs is named in llms-full.txt. A
//      new guide cannot be added without its entry, however it is reached — the
//      check the index-scoped version could not make, and the one that would have
//      caught /docs/builder, /docs/chat and /docs/feedback.
//   2. (index -> named) Every guide the /docs index links is named in llms-full.txt
//      (the original check, kept: it is the reader a /docs/security-style
//      regression hides behind).
//   The reverse is deliberately NOT pinned as an equality: the file legitimately
//   names pages that are not docs routes (the landing page, /mcp, /legal/*), and a
//   new entry is never a defect. That every /docs path this file NAMES resolves to
//   a real page is pinned in docsIndex.test.ts ("the agent-facing llms-full.txt
//   index points at the same pages"), not duplicated here.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * It checks the LINK is present, not that the one-line description is accurate.
//   * It reads the source text of both files; it cannot execute the React page, so
//     it sees declared links, not the rendered layout.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, existsSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { join } from "node:path";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");
const path = (rel: string) => fileURLToPath(new URL(rel, import.meta.url));

const DOCS_DIR = path("../app/docs");

// guidePathsLinkedFromIndex returns every /docs/<slug> the index page links to.
// It reads the source as text (node --test cannot load JSX, the same constraint
// docsIndex.test.ts works under) and matches the href strings, which is how the
// index declares each "see the full guide" link.
function guidePathsLinkedFromIndex(src: string): Set<string> {
  const paths = new Set<string>();
  for (const m of src.matchAll(/[\"'`](\/docs\/[a-z0-9-]+)/g)) paths.add(m[1]);
  return paths;
}

// guidePathsNamedInLlmsFull returns every /docs and /docs/<slug> llms-full.txt
// names, as absolute URLs. Both shapes are captured because the comparison now
// includes the index route: `/docs` is itself a page on disk. The bare form is
// anchored with a lookahead so `/docs` cannot be read as a prefix of
// `/docs/sales` — the slug alternative is tried first, and the bare one only
// matches when the next character is neither `/` nor a slug character.
function guidePathsNamedInLlmsFull(src: string): Set<string> {
  const paths = new Set<string>();
  for (const m of src.matchAll(/https:\/\/jomform\.com(\/docs\/[a-z0-9-]+|\/docs(?![\/a-z0-9-]))/g))
    paths.add(m[1]);
  return paths;
}

// docsRoutesOnDisk lists every real page under app/docs as `/docs` or
// `/docs/<slug>` — the set the agent's context file must cover, regardless of
// whether the index links the page. A directory with no `page.tsx` is not a route
// (the filter keeps a helper directory from being reported as a missing entry).
function docsRoutesOnDisk(): string[] {
  const routes = existsSync(join(DOCS_DIR, "page.tsx")) ? ["/docs"] : [];
  for (const e of readdirSync(DOCS_DIR, { withFileTypes: true })) {
    if (!e.isDirectory()) continue;
    if (!existsSync(join(DOCS_DIR, e.name, "page.tsx"))) continue;
    routes.push(`/docs/${e.name}`);
  }
  return routes.sort();
}

test("every docs page on disk is named in llms-full.txt (a feature-only guide cannot go missing)", () => {
  const llms = read("../../public/llms-full.txt");

  const onDisk = docsRoutesOnDisk();
  const named = guidePathsNamedInLlmsFull(llms);

  // Guard against passing vacuously: the walk and the pattern must both have
  // matched a real list, or the comparison below would be between two empty sets.
  // The docs tree carries 18 routes today (17 directories plus the index).
  assert.ok(
    onDisk.length >= 15,
    `found only ${onDisk.length} docs routes under ${DOCS_DIR} — the walk (or its page.tsx ` +
      `filter) is broken, so this guard would report nothing missing on any tree and pass ` +
      `for anything`,
  );
  assert.ok(
    named.size > 5,
    `only ${named.size} paths found in llms-full.txt — the URL pattern probably stopped ` +
      `matching, so this guard would pass for anything`,
  );

  const missing = onDisk.filter((p) => !named.has(p));
  assert.deepEqual(
    missing,
    [],
    `these pages exist under app/docs but are not named in web/public/llms-full.txt, so an AI ` +
      `agent reading that file never learns they exist — including any whose only inbound link ` +
      `is a feature's own inline "?" (the three the index-scoped check can never see):\n  ` +
      `${missing.join("\n  ")}\n\n` +
      `Add a "- [Title](https://jomform.com<path>) — one line" entry beside the other guides in ` +
      `its "Key pages" list.`,
  );
});

test("every guide the /docs index links to is named in llms-full.txt", () => {
  const index = read("../app/docs/page.tsx");
  const llms = read("../../public/llms-full.txt");

  const linked = guidePathsLinkedFromIndex(index);
  const named = guidePathsNamedInLlmsFull(llms);

  // Guard against passing vacuously: both sides must have matched a real list, or
  // the pattern has drifted and this test would compare two empty sets.
  assert.ok(
    linked.size > 5,
    `only ${linked.size} guide links found in the /docs index — the href pattern ` +
      `probably stopped matching, so this guard would pass for anything`,
  );
  assert.ok(
    named.size > 5,
    `only ${named.size} guide links found in llms-full.txt — the URL pattern ` +
      `probably stopped matching, so this guard would pass for anything`,
  );

  const missing = [...linked].filter((p) => !named.has(p));
  assert.deepEqual(
    missing,
    [],
    `these /docs guides are linked from the index but have no entry in ` +
      `web/public/llms-full.txt, so an AI agent reading that file never learns they ` +
      `exist:\n  ${missing.join("\n  ")}\n\n` +
      `Add a "- [Title](https://jomform.com<path>) — one line" entry beside the other ` +
      `guides in its "Key pages" list.`,
  );
});

test("the comparison is not vacuous: a sentinel path is absent from both sides", () => {
  const index = read("../app/docs/page.tsx");
  const llms = read("../../public/llms-full.txt");

  // POSITIVE CONTROL. Without this, the assertions above would be satisfied by a
  // comparison that can never find anything (e.g. both patterns matching nothing, or
  // a helper that returns a Set containing every input).
  assert.equal(
    guidePathsLinkedFromIndex(index).has("/docs/zzz-nonexistent"),
    false,
    "the index source unexpectedly contains the sentinel path — the control cannot " +
      "prove the comparison works",
  );
  assert.equal(
    guidePathsNamedInLlmsFull(llms).has("/docs/zzz-nonexistent"),
    false,
    "llms-full.txt unexpectedly names the sentinel path — the control cannot prove " +
      "the comparison works",
  );

  // And the helpers must actually discriminate: a path deliberately absent from the
  // index is not returned by it.
  assert.equal(
    guidePathsLinkedFromIndex('href: "/docs/security"').has("/docs/security"),
    true,
    "the index helper failed to find a quoted href — the extraction is broken",
  );
  assert.equal(
    guidePathsNamedInLlmsFull("see https://jomform.com/docs/security").has("/docs/security"),
    true,
    "the llms helper failed to find an absolute link — the extraction is broken",
  );
  // It requires the ABSOLUTE form: a relative path in that file is not an entry,
  // because the list is read by agents that are not on the site.
  assert.equal(
    guidePathsNamedInLlmsFull('see "/docs/security"').has("/docs/security"),
    false,
    "the llms helper matched a relative path — entries must be absolute URLs, or an " +
      "agent reading the file elsewhere cannot follow them",
  );

  // The widened helper must capture the INDEX route, and must NOT read the bare
  // `/docs` as a prefix of a slug: a `named` set holding "/docs" for every guide
  // would satisfy the disk check vacuously.
  assert.equal(
    guidePathsNamedInLlmsFull("the index at https://jomform.com/docs").has("/docs"),
    true,
    "the llms helper failed to capture the bare /docs route — the disk comparison " +
      "would report the index page missing",
  );
  assert.equal(
    guidePathsNamedInLlmsFull("see https://jomform.com/docs/sales").has("/docs"),
    false,
    "the llms helper read `/docs` as a prefix of `/docs/sales`, so a named guide " +
      "would spuriously satisfy the bare index route",
  );
});
