// Regression guard for "/docs/api pans 128px sideways at 150% browser text size".
//
// THE DEFECT (measured on live prod v1.22.60, 2026-11-01, Chromium 390x844
//   dsf2, browser default font size raised to 24px — Chrome's "Very large",
//   the text-scaling a user turns on, fresh browser state, lang=en):
//
//     document.documentElement.scrollWidth 518 vs clientWidth 390   (128px pan)
//
//   At 100% (16px) the page is clean: 390 == 390 with 9 <pre> blocks (the four
//   source <pre> tags plus one per executed example), 7 of them narrower than
//   the column and self-scrolling at `overflow-x:auto`. The overflowing
//   elements at 24px were NOT those <pre> blocks (all 9 sit inside the column,
//   right edge 354) — they were PROSE inline <code> tokens in the nested lists
//   that `writeGatedReads()` renders:
//
//     GET /v1/rest/forms/{id}/email-settings
//     GET /v1/rest/forms/{id}/notifications
//     GET /v1/rest/settings/webhook-key
//
//   Each is one whitespace-free run (34/30/32 characters) inside a <code> whose
//   className was the bare `rounded bg-slate-100 px-1`: no break opportunity, so
//   the token is as wide as its glyphs and, at a raised root font size, wider
//   than the list column. The overflow then propagates up through
//   div.max-w-3xl to the document and the whole page pans.
//
// THE FIX: `[overflow-wrap:anywhere]` on EVERY prose inline <code> token on the
//   page (32 of them: the 31 with `rounded bg-slate-100 px-1` plus the one
//   `font-mono` token), the same rule /docs/mcp already carries (see
//   src/lib/docsMcpProseWrap.test.ts).
//   `anywhere` — not `break-word` — because it also drops the element's
//   min-content width to one character, so the token can shrink inside the
//   column instead of flooring it. The text still reads in full, wrapped; the
//   paths were NOT shortened.
//   The <code> INSIDE the four <pre> blocks is deliberately exempt: those are
//   code samples that must keep scrolling horizontally (`overflow-x:auto`), and
//   `white-space: pre` ignores `overflow-wrap` anyway.
//
// WHY THIS FILE IS A SOURCE-CONTRACT TEST AND NOT A BROWSER TEST
//   This repo has no DOM/JSX unit runner (no vitest/jest — see
//   src/lib/docsMcpProseWrap.test.ts, written the same way), and the Playwright
//   a11y gate cannot measure layout deformation. The pixel numbers above were
//   measured in a real browser against a production build and are re-checked in
//   the PR; what a unit test can pin is the class that carries the behaviour,
//   the exemption's justification (every <pre> still self-scrolls), and the fact
//   that the contract still carries the full paths that needed it.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { writeGatedReads } from "./openapi.ts";

const PAGE = "../app/docs/api/page.tsx";
const page = readFileSync(fileURLToPath(new URL(PAGE, import.meta.url)), "utf8");

/** A class name that permits a break inside an unbreakable run. */
const BREAK_GUARD = /\[overflow-wrap:(?:anywhere|break-word)\]|\bbreak-words\b|\bbreak-all\b/;

/** Every `<pre>…</pre>` block, in source order. */
const PRE_BLOCKS = [...page.matchAll(/<pre\b[^>]*>[\s\S]*?<\/pre>/g)].map((m) => m[0]);

/**
 * The page with its code samples removed.
 *
 * The scan for prose <code> tokens runs on this, and the removal is what makes
 * the `<pre>` exemption explicit rather than accidental: a sample's <code> is
 * meant to scroll, so requiring a break guard on it would be wrong, and
 * `white-space: pre` would ignore the guard anyway.
 */
const proseSource = page.replace(/<pre\b[^>]*>[\s\S]*?<\/pre>/g, "");

function codeElements(src: string) {
  return [...src.matchAll(/<code\b([^>]*)>([\s\S]*?)<\/code>/g)].map((m) => ({
    attrs: m[1],
    body: m[2],
  }));
}

const ALL_CODES = codeElements(page);
const SAMPLE_CODES = codeElements(PRE_BLOCKS.join("\n"));
const PROSE_CODES = codeElements(proseSource);

/** The three tokens that measured wider than the list column at root 24px. */
const OVERFLOWING = [
  "GET /v1/rest/forms/{id}/email-settings",
  "GET /v1/rest/forms/{id}/notifications",
  "GET /v1/rest/settings/webhook-key",
];

test("the guard is looking at real render sites (it cannot pass vacuously)", () => {
  // The page carries four <pre> tags in source; the rest of the nine rendered
  // <pre> blocks come from apiExamples.map() re-using the same template. If a
  // refactor drops or renames the tag, the exemption split below would quietly
  // scan the wrong set — so the split is checked first.
  assert.ok(
    PRE_BLOCKS.length >= 4,
    `found ${PRE_BLOCKS.length} <pre>…</pre> block(s) in ${PAGE} — the page ships four code ` +
      "samples and the exemption below is written against that shape",
  );
  assert.ok(
    SAMPLE_CODES.length >= PRE_BLOCKS.length,
    `found ${SAMPLE_CODES.length} <code> token(s) inside ${PRE_BLOCKS.length} <pre> block(s) — ` +
      "the sample/prose split has drifted from the page's shape",
  );
  assert.equal(
    ALL_CODES.length,
    SAMPLE_CODES.length + PROSE_CODES.length,
    "the <pre> split does not partition the page's <code> tokens — the prose scan is either " +
      "missing render sites or double-counting them",
  );
  assert.ok(
    PROSE_CODES.length >= 30,
    `found only ${PROSE_CODES.length} prose inline <code> token(s) in ${PAGE} — the page ` +
      "renders ~33 and the break-guard assertion below would be passing over almost nothing",
  );
  assert.ok(
    PROSE_CODES.every((c) => /className="/.test(c.attrs)),
    "a prose <code> without a className can never carry a break guard — name its render site",
  );

  // The dynamic render sites matter most: the three paths that overflowed are
  // not literals in this file, they arrive through writeGatedReads().
  const dynamic = PROSE_CODES.filter((c) => /\{e\.method\}\s*\{e\.path\}/.test(c.body));
  assert.ok(
    dynamic.length >= 2,
    `found ${dynamic.length} prose <code> rendering "{e.method} {e.path}" — the scope section's ` +
      "write-gated list and the rate-limit list both render rows that way, and those are the " +
      "sites that panned at root 24px",
  );

  // Every <pre> the <code> exemption relies on must still scroll on its own.
  for (const block of PRE_BLOCKS) {
    const open = block.match(/^<pre\b[^>]*>/)?.[0] ?? "";
    assert.match(
      open,
      /overflow-x-auto/,
      `a <pre> on ${PAGE} no longer carries overflow-x-auto: ${open} — without it the sample ` +
        "cannot scroll, so exempting its <code> from the break guard would push the overflow " +
        "back onto the page",
    );
  }
});

test("every prose inline <code> token lets an unbreakable run break", () => {
  const unguarded: string[] = [];
  for (const { attrs, body } of PROSE_CODES) {
    const cls = attrs.match(/className="([^"]*)"/)?.[1] ?? "";
    if (BREAK_GUARD.test(cls)) continue;
    const token = body.trim().replace(/\s+/g, " ").slice(0, 60);
    unguarded.push(
      `the inline <code> rendering "${token}" has className "${cls}" with no break guard — a ` +
        "token's width is measured in the page's font, not in a fixed number of pixels, so a " +
        "path that fits at 100% text size overflows the column at 150% (measured on " +
        "/docs/api: documentElement.scrollWidth 518 vs clientWidth 390 at root 24px, the " +
        "GET /v1/rest/forms/{id}/email-settings token in a 390px viewport)",
    );
  }
  assert.deepEqual(
    unguarded,
    [],
    `${unguarded.length} prose render site(s) on ${PAGE} carry an unbreakable run with no ` +
      `break guard:\n` +
      unguarded.map((u) => `  - ${u}`).join("\n"),
  );
});

test("the three paths that overflowed are still rendered in full from the contract", () => {
  // A guard is only load-bearing while the data still carries a run long enough
  // to need it. Exact set equality — not a substring check — is deliberate on
  // both counts: shortening a path to make it fit is the one way to go green
  // that the defect report explicitly rules out, and a NEW write-gated read
  // must make someone look at how its row is rendered.
  const rendered = writeGatedReads().map((e) => `${e.method} ${e.path}`);
  assert.deepEqual(
    [...rendered].sort(),
    [...OVERFLOWING].sort(),
    "the write-gated read list rendered by /docs/api has changed — if a route was added, check " +
      "its row still wraps and extend OVERFLOWING; if one of these three was shortened, that " +
      "is not a fix: the full path is what the reader needs",
  );

  // And each is a single whitespace-free run wide enough to need the guard.
  for (const token of OVERFLOWING) {
    const longest = Math.max(...token.split(/\s+/).map((run) => run.length));
    assert.ok(
      longest >= 25,
      `"${token}" has a longest whitespace-free run of ${longest} chars — it no longer needs ` +
        "the break guard, so re-check whether the fix is still required",
    );
  }
});
