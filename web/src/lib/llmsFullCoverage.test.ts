// llms-full.txt coverage guard (t_f8015915).
//
// WHY THIS EXISTS
//   web/public/llms-full.txt is the machine-readable context AI agents fetch. Its
//   "Key pages" list is how an agent learns a guide EXISTS — a page missing from it
//   is a page the agent will not read, and no build step, type or e2e run notices.
//   /docs/security (two-factor, passkeys, API-key scopes) was added by PR 675 and
//   was absent from that list from then until this guard: llms-full.txt named ten
//   guides while the /docs index linked eleven, so the gap was invisible.
//
// WHAT IT PINS
//   Every guide the /docs index links to must have an entry in llms-full.txt. The
//   reverse is deliberately NOT pinned: the file may legitimately name a page the
//   index does not link (the landing page, /mcp), and a new entry is never a defect.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * It checks the LINK is present, not that the one-line description is accurate.
//   * It reads the source text of both files; it cannot execute the React page, so
//     it sees declared links, not the rendered layout.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

// guidePathsLinkedFromIndex returns every /docs/<slug> the index page links to.
// It reads the source as text (node --test cannot load JSX, the same constraint
// docsIndex.test.ts works under) and matches the href strings, which is how the
// index declares each "see the full guide" link.
function guidePathsLinkedFromIndex(src: string): Set<string> {
  const paths = new Set<string>();
  for (const m of src.matchAll(/["'`](\/docs\/[a-z0-9-]+)/g)) paths.add(m[1]);
  return paths;
}

// guidePathsNamedInLlmsFull returns every /docs/<slug> llms-full.txt links to.
function guidePathsNamedInLlmsFull(src: string): Set<string> {
  const paths = new Set<string>();
  for (const m of src.matchAll(/https:\/\/jomform\.com(\/docs\/[a-z0-9-]+)/g)) paths.add(m[1]);
  return paths;
}

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

  // POSITIVE CONTROL. Without this, the assertion above would be satisfied by a
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
});
