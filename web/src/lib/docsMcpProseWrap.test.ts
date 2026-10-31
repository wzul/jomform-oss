// Regression guard for "/docs/mcp pans sideways on a phone".
//
// THE BUG (measured, production build, Chromium 390x844 dsf2):
//   document.documentElement.scrollWidth 939 vs clientWidth 390 — the page
//   panned 549px sideways and the rest of /docs did not. It was one paragraph
//   class: the tool-description <p> inside each card. A description is DATA,
//   and several carry a long unbreakable run with no whitespace in it:
//
//     register_form_fields  "(name|email|phone|…|image-choice|captcha)"
//                           — ONE 138-character token (the tool's argument
//                             contract; the card that fixed this said not to
//                             shorten it)
//     a second entry        a 62-character run
//     others                "sale.paid|sale.failed|subscription.charged|*",
//                           "(privacy|terms|refund)", '{"full_name":"block_abc",…}'
//
//   Ordinary word wrapping has no break opportunity inside such a run, so the
//   paragraph measured scrollWidth 898px in a clientWidth 308px card (360px
//   viewport: 898 vs 278, 11 paragraphs overflowing), and the overflow
//   propagated up through div.max-w-3xl -> div.min-w-0.flex-1 -> the max-w-6xl
//   wrapper to the document. /docs/mcp was the ONLY one of the 20 /docs* pages
//   that overflowed at 390/360px — so this is a page-local defect, not a
//   shared-layout one, and no other page is given this treatment.
//
// THE FIX: `[overflow-wrap:anywhere]` on the two paragraphs this page renders
//   out of TOOL_GROUPS (each tool's `desc` and a group's `note`).
//   `anywhere` — not `break-word` — because it also drops the paragraph's
//   min-content width to one character, so the card is free to shrink inside
//   the flex column instead of being floored by its widest token. It does not
//   truncate: the text still reads in full, wrapped.
//
// WHY THIS FILE IS A SOURCE-CONTRACT TEST AND NOT A BROWSER TEST
//   This repo has no DOM/JSX unit runner (no vitest/jest — see
//   src/lib/adminResponsive.test.ts and src/lib/docsProseConvention.test.ts,
//   written the same way), and the Playwright a11y gate cannot measure layout
//   deformation. The pixel numbers above were measured in a real browser
//   against a production build and are re-checked in the PR; what a unit test
//   can pin is the class that carries the behaviour, plus the fact that the
//   data still contains a run long enough to need it.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const PAGE = "../app/docs/mcp/page.tsx";
const page = readFileSync(fileURLToPath(new URL(PAGE, import.meta.url)), "utf8");

/** A class name that permits a break inside an unbreakable run. */
const BREAK_GUARD = /\[overflow-wrap:(?:anywhere|break-word)\]|\bbreak-words\b|\bbreak-all\b/;

/** Every `<p>` or `<code>` that renders a field of a TOOL_GROUPS row or group. */
function dataElements(src: string) {
  const out: { tag: string; body: string; attrs: string }[] = [];
  for (const m of src.matchAll(/<(p|code)\b([^>]*)>([\s\S]*?)<\/\1>/g)) {
    const body = m[3];
    if (/\{(?:t|g)\.[A-Za-z_]+\}/.test(body)) out.push({ tag: m[1], body, attrs: m[2] });
  }
  return out;
}

const SITES = dataElements(page);

test("the guard is looking at real render sites (it cannot pass vacuously)", () => {
  // 189 strings sit in TOOL_GROUPS; exactly three places render them as prose or
  // as an identifier. If the page is refactored so this extractor finds nothing,
  // the assertions below would be vacuously true, so the shape is checked first.
  assert.ok(
    SITES.length >= 3,
    `found ${SITES.length} element(s) rendering a TOOL_GROUPS field — the extractor ` +
      `has drifted from ${PAGE}'s shape, or the descriptions moved to a new element`,
  );
  const rendered = SITES.map((s) => s.body.trim()).join(" ");
  for (const field of ["{t.name}", "{t.desc}", "{g.note}"]) {
    assert.ok(
      rendered.includes(field),
      `${field} is no longer rendered by an element this guard can see — check whether the ` +
        "text now lives on an element without a break guard",
    );
  }
});

test("every data-driven prose element lets an unbreakable run break", () => {
  for (const { tag, attrs, body } of SITES) {
    const cls = attrs.match(/className="([^"]*)"/)?.[1] ?? "";
    const field = body.match(/\{(?:t|g)\.[A-Za-z_]+\}/)?.[0] ?? body.slice(0, 40);
    assert.match(
      cls,
      BREAK_GUARD,
      `the <${tag}> rendering ${field} has className "${cls}" with no break guard and measured ` +
        "898px wide in a 308px card at 390px",
    );
  }
});

test("the contract text still contains a run that needs the guard", () => {
  // The guard is not decoration: it is load-bearing for as long as TOOL_GROUPS
  // carries a whitespace-free run wider than the mobile column (278px at 360px,
  // ~6.9px per character at text-sm). If the data changed so that no such run
  // remains, this fails on purpose — decide then whether to keep the guard.
  const start = page.indexOf("const TOOL_GROUPS");
  const end = page.indexOf("\n];", start);
  assert.ok(start >= 0 && end > start, `could not find the TOOL_GROUPS array in ${PAGE}`);
  const block = page.slice(start, end);

  const strings = [...block.matchAll(/(?:desc|note):\s*"((?:[^"\\]|\\.)*)"/g)].map((m) => m[1]);
  assert.ok(
    strings.length >= 150,
    `parsed only ${strings.length} desc/note string(s) out of TOOL_GROUPS — the extractor has drifted`,
  );

  let longest = 0;
  let who = "";
  for (const s of strings) {
    for (const run of s.split(/\s+/)) {
      if (run.length > longest) {
        longest = run.length;
        who = s.slice(0, 60);
      }
    }
  }
  assert.ok(
    longest >= 60,
    `the longest whitespace-free run in a TOOL_GROUPS string is ${longest} chars (${JSON.stringify(who)}…) ` +
      "— the long alternations that made this page overflow are gone, so re-check whether the " +
      "paragraphs still need [overflow-wrap:anywhere] and update this guard",
  );

  // The specific token this defect was filed on must still be present in full:
  // the card's acceptance is "still readable in full (not truncated)", so a
  // shortened contract text is not a valid way to go green.
  const rff = strings.find((s) => s.includes("type mesti jenis input JomForm")) ?? "";
  const alternation = rff.match(/\(([a-z-]+(?:\|[a-z-]+)+)\)/)?.[1] ?? "";
  assert.ok(
    alternation.split("|").length >= 15,
    `the register_form_fields type alternation parsed as ${JSON.stringify(alternation)} — it ` +
      "must stay a full (a|b|c) list of the field types; shortening it to stop the overflow " +
      "loses the tool's documented argument shape",
  );
});
