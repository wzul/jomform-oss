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
//   page, the same rule /docs/mcp already carries (see
//   src/lib/docsMcpProseWrap.test.ts).
//   `anywhere` — not `break-word` — because it also drops the element's
//   min-content width to one character, so the token can shrink inside the
//   column instead of flooring it. The text still reads in full, wrapped; the
//   paths were NOT shortened.
//   The <code> INSIDE a <pre> block is deliberately exempt: those are code
//   samples that must keep scrolling horizontally (`overflow-x:auto`), and
//   `white-space: pre` ignores `overflow-wrap` anyway.
//
// WHERE THE FACT LIVES NOW
//   The page was converted to the shared prose renderer, so it no longer writes
//   a <code> tag at all: its prose is `const SECTIONS` strings in which `code`
//   markers become an inline <code> — rendered ONCE, by
//   src/app/docs/docsProse.tsx — and a `{ code: … }` block becomes the <pre>.
//   The break guard therefore exists once, in the renderer; this file asserts
//   the page still ships its tokens and its samples through those two blocks,
//   and that the single inline render site carries the guard. A page that stops
//   routing through the renderer fails the vacuity test rather than passing over
//   an empty scan.
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

import { apiExamples, writeGatedReads } from "./openapi.ts";
import { sectionsOf, usesDocsProse } from "./docsProseReader.ts";
import { parseInline } from "../app/docs/docsProseText.ts";

// Card t_0356ed4c: /docs/api is a hub plus four task pages. The tokens and the
// samples this guard measured are spread across them, so it reads the SET — a
// token that moves to /docs/api/errors is still the same unbreakable run.
const PAGES = [
  "../app/docs/api/page.tsx",
  "../app/docs/api/authentication/page.tsx",
  "../app/docs/api/endpoints/page.tsx",
  "../app/docs/api/webhooks/page.tsx",
  "../app/docs/api/errors/page.tsx",
];
const PAGE = PAGES[0];
const RENDERER = "../app/docs/docsProse.tsx";
const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");
const page = PAGES.map(read).join("\n");
/** The one module that turns a prose token or a code block into markup. */
const renderer = read(RENDERER);

/** A class name that permits a break inside an unbreakable run. */
const BREAK_GUARD = /\[overflow-wrap:(?:anywhere|break-word)\]|\bbreak-words\b|\bbreak-all\b/;

/** Every `<pre>…</pre>` block in the renderer, in source order. */
const PRE_BLOCKS = [...renderer.matchAll(/<pre\b[^>]*>[\s\S]*?<\/pre>/g)].map((m) => m[0]);

/**
 * The renderer with its code samples removed.
 *
 * The scan for prose <code> tokens runs on this, and the removal is what makes
 * the `<pre>` exemption explicit rather than accidental: a sample's <code> is
 * meant to scroll, so requiring a break guard on it would be wrong, and
 * `white-space: pre` would ignore the guard anyway.
 */
const proseSource = renderer.replace(/<pre\b[^>]*>[\s\S]*?<\/pre>/g, "");

function codeElements(src: string) {
  return [...src.matchAll(/<code\b([^>]*)>([\s\S]*?)<\/code>/g)].map((m) => ({
    attrs: m[1],
    body: m[2],
  }));
}

const ALL_CODES = codeElements(renderer);
const SAMPLE_CODES = codeElements(PRE_BLOCKS.join("\n"));
const PROSE_CODES = codeElements(proseSource);

/**
 * The page's code SAMPLE blocks: `{ code: … }` entries in the SECTIONS literal.
 *
 * One of them is the example template (`code: `${e.curl}``), rendered once per
 * executed example — that is how four source blocks become nine rendered
 * `<pre>`s, the shape the measurement was taken on.
 */
const PAGE_CODE_SITES = [...page.matchAll(/\bcode:\s*[`'"]/g)].map((m) => m[0]);

/** The inline `code` tokens the page's prose carries, as the reader sees them. */
const PAGE_TOKENS = PAGES.flatMap((rel) => sectionsOf(read(rel), rel))
  .flatMap((s) => s.strings)
  .flatMap((s) => parseInline(s))
  .filter((t) => t.kind === "code");

/**
 * Inline code tokens inside a template literal with a substitution — the
 * reader collects only plain strings, so `` `${API_BASE}` `` and
 * `` `${e.path}` `` are render sites, not prose. They must still be counted:
 * they are the dynamic rows the measurement was taken on.
 */
const INTERPOLATED_TOKENS = [...page.matchAll(/`\$\{[A-Za-z_][^`]*\}`/g)];

/** The three tokens that measured wider than the list column at root 24px. */
const OVERFLOWING = [
  "GET /v1/rest/forms/{id}/email-settings",
  "GET /v1/rest/forms/{id}/notifications",
  "GET /v1/rest/settings/webhook-key",
];

test("the guard is looking at real render sites (it cannot pass vacuously)", () => {
  // The page renders through the prose renderer, and its samples are `{ code: }`
  // blocks. If either shape goes away, the split below would scan the wrong set —
  // so the routing is checked first.
  assert.ok(
    usesDocsProse(page),
    `${PAGE} no longer renders through src/app/docs/docsProse — the prose <code> tokens this ` +
      "guard is about are rendered by that module, so it must be in the path",
  );
  assert.ok(
    PAGE_CODE_SITES.length >= 4,
    `found ${PAGE_CODE_SITES.length} \`{ code: … }\` block(s) in ${PAGE} — the page ships four ` +
      "code samples (three literals plus the executed-example template) and the exemption " +
      "below is written against that shape",
  );
  assert.match(
    page,
    /code:\s*`\$\{e\.curl\}`/,
    `${PAGE} no longer renders the executed examples as code blocks — a reader would lose the ` +
      "curl command the walk-through is about",
  );

  // The renderer has exactly one sample site and one inline site; the split must
  // partition them, or the prose scan is either missing render sites or
  // double-counting them.
  assert.equal(
    PRE_BLOCKS.length,
    1,
    `found ${PRE_BLOCKS.length} <pre>…</pre> block(s) in ${RENDERER} — the renderer owns ` +
      "exactly one sample site, rendered once per `{ code: … }` block",
  );
  assert.ok(
    SAMPLE_CODES.length >= PRE_BLOCKS.length,
    `found ${SAMPLE_CODES.length} <code> token(s) inside ${PRE_BLOCKS.length} <pre> block(s) — ` +
      "the sample/prose split has drifted from the renderer's shape",
  );
  assert.equal(
    ALL_CODES.length,
    SAMPLE_CODES.length + PROSE_CODES.length,
    "the <pre> split does not partition the renderer's <code> tokens — the prose scan is " +
      "either missing render sites or double-counting them",
  );
  assert.equal(
    PROSE_CODES.length,
    1,
    `found ${PROSE_CODES.length} prose inline <code> render site(s) in ${RENDERER} — expected ` +
      "exactly one (every token on every page goes through it); more than one means a token " +
      "can bypass the guard asserted below",
  );
  assert.ok(
    PROSE_CODES.every((c) => /className="/.test(c.attrs)),
    "a prose <code> without a className can never carry a break guard — name its render site",
  );
  assert.ok(
    renderer.includes('if (t.kind === "code")'),
    "docsProse.tsx no longer has a branch that renders a parsed `code` token — the prose " +
      "tokens below would not reach the guarded element at all",
  );

  // The page's own tokens: the reader-visible ones, and the interpolated ones it
  // cannot see. Together they are the population the single render site covers.
  assert.ok(
    PAGE_TOKENS.length >= 20,
    `found only ${PAGE_TOKENS.length} plain-string inline code token(s) in ${PAGE}'s SECTIONS — ` +
      "the page renders ~23 and the break-guard assertion below would be passing over almost " +
      "nothing",
  );
  assert.ok(
    INTERPOLATED_TOKENS.length >= 3,
    `found ${INTERPOLATED_TOKENS.length} interpolated inline code token(s) in ${PAGE} — ` +
      "`${API_BASE}` and the two `${e.path}` route lists are the dynamic rows that measured " +
      "wide, and they must still reach the prose render site",
  );

  // Every <pre> the <code> exemption relies on must still scroll on its own.
  for (const block of PRE_BLOCKS) {
    const open = block.match(/^<pre\b[^>]*>/)?.[0] ?? "";
    assert.match(
      open,
      /overflow-x-auto/,
      `a <pre> in ${RENDERER} no longer carries overflow-x-auto: ${open} — without it the ` +
        "sample cannot scroll, so exempting its <code> from the break guard would push the " +
        "overflow back onto the page",
    );
  }
});

test("every prose inline <code> token lets an unbreakable run break", () => {
  const unguarded: string[] = [];
  for (const { attrs } of PROSE_CODES) {
    const cls = attrs.match(/className="([^"]*)"/)?.[1] ?? "";
    if (BREAK_GUARD.test(cls)) continue;
    unguarded.push(
      `the prose <code> render site (${RENDERER}) has className "${cls}" with no break guard — ` +
        "a token's width is measured in the page's font, not in a fixed number of pixels, so a " +
        "path that fits at 100% text size overflows the column at 150% (measured on " +
        "/docs/api: documentElement.scrollWidth 518 vs clientWidth 390 at root 24px, the " +
        "GET /v1/rest/forms/{id}/email-settings token in a 390px viewport)",
    );
  }
  assert.deepEqual(
    unguarded,
    [],
    `${unguarded.length} prose render site(s) in ${RENDERER} carry an unbreakable run with no ` +
      `break guard:\n` +
      unguarded.map((u) => `  - ${u}`).join("\n"),
  );

  // And every token on /docs/api is one this site renders: the page has no
  // <code> of its own to escape through. (A hand-written <code> in a page would
  // be invisible to the assertion above.)
  assert.deepEqual(
    [...page.matchAll(/<code\b/g)].map(() => "a hand-written <code> in the page"),
    [],
    `${PAGE} declares its own <code> element instead of a prose token — that element is not ` +
      "the guarded render site, so it can carry an unbreakable run the guard never sees",
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

  // The list that carries them is rendered as a prose list from the contract (the
  // page's `{ list: WRITE_GATED_READS }`), and the tokens arrive as backticked
  // `code` markers, so the guarded render site above is what they reach.
  assert.match(
    page,
    /const WRITE_GATED_READS: string\[\] = writeGatedReads\(\)\.map\(/,
    `${PAGE} no longer builds the write-gated list from writeGatedReads() — the routes these ` +
      "measurements were taken on would come from somewhere else",
  );
  assert.ok(
    page.includes("{ list: WRITE_GATED_READS }"),
    `${PAGE} no longer renders the write-gated list — the three routes below are then not on ` +
      "the page at all",
  );
  // The dynamic render sites matter most: the three paths that overflowed are
  // not literals in this file, they arrive through writeGatedReads() — and so
  // does the throttled list — as `code` markers inside a template literal (hence
  // the escaped backticks in the source). Both lists must still wrap `e.path`.
  const dynamicMarkers = [...page.matchAll(/\\?`\$\{e\.path\}\\?`/g)].length;
  assert.ok(
    dynamicMarkers >= 2,
    `found ${dynamicMarkers} code-marked \`\${e.path}\` site(s) in ${PAGE} — the scope section's ` +
      "write-gated list and the rate-limit list both render rows that way, and those are the " +
      "sites that panned at root 24px",
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

  // The executed examples are the other sample population; the page still renders
  // them, so the <pre> exemption is still justified by real content.
  assert.ok(
    apiExamples.length >= 6,
    `only ${apiExamples.length} executed example(s) — the page's code samples shrank`,
  );
});
