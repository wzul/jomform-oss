// Regression guard for "/docs pans 25px sideways at 320px with the browser font
// raised to 24px" (card t_68456ead).
//
// THE DEFECT (measured on a production dev build of this tree, 2026-11-05,
// Chromium 320x844 dsf2, root font 24px via CDP Page.setFontSizes, fresh
// browser state, lang=en):
//
//   document.documentElement.scrollWidth 345 vs clientWidth 320  — a 25px pan
//   only at 320px; 360px and 390px read pan 0.
//
//   No ELEMENT rect exceeded the viewport (n=0): it was text-node INK overflow.
//   The FAQ answer
//
//     "Point any MCP client at the server URL https://mcp.jomform.com/mcp. It
//      registers itself via OAuth 2.1 — no API keys needed. …"
//
//   is one string in the FAQ data, and the URL inside it is a run with no
//   whitespace to break at, so the text node painted to right 344.95 in a 320px
//   viewport and dragged the document with it.
//
// THE FIX: `[overflow-wrap:anywhere]` on the FAQ ANSWER paragraph (the render
//   site, not the data — every answer is prose that can carry a token). `anywhere`
//   rather than `break-word` because it also drops the token's min-content width,
//   so the paragraph can shrink inside the column; and the break opportunity only
//   OPENS when the line would otherwise overflow, so the 100%-text layout is
//   unchanged (measured: pan 0 at 16px both before and after, same line breaks).
//
// WHY A SOURCE-CONTRACT TEST AND NOT A BROWSER TEST
//   This repo has no DOM/JSX unit runner (see src/lib/docsApiProseWrap.test.ts,
//   written the same way) and the Playwright a11y gate cannot measure layout
//   deformation. The pixel numbers above were measured in a real browser and are
//   re-checked in the card; what a unit test can pin is the class on the render
//   site, that the render site is real, and that the full URL is still there, so
//   "shorten the URL until it fits" is not a green path.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const PAGE = "../app/docs/page.tsx";
const page = readFileSync(fileURLToPath(new URL(PAGE, import.meta.url)), "utf8");

/** A class name that permits a break inside an unbreakable run. */
const BREAK_GUARD = /\[overflow-wrap:(?:anywhere|break-word)\]|\bbreak-words\b|\bbreak-all\b/;

/** The FAQ ANSWER render sites: a <p> whose whole content is the `a` field. */
const ANSWER_SITES = [...page.matchAll(/<p\b([^>]*)>\{f\.a\}<\/p>/g)].map((m) => m[1]);

/** The FAQ data block, in source order, as `{ q: …, a: … }` string literals. */
const FAQ_BLOCK = page.match(/const FAQ: FaqEntry\[\] = \[([\s\S]*?)\n\];/)?.[1] ?? "";
const FAQ_ANSWERS = [...FAQ_BLOCK.matchAll(/\ba:\s*"((?:[^"\\]|\\.)*)"/g)].map((m) => m[1]);

/** The MCP answer, the one that measured 344.95 at a 24px root. */
const MCP_URL = "https://mcp.jomform.com/mcp";

test("the guard is looking at real render sites (it cannot pass vacuously)", () => {
  // Exactly one paragraph renders the `a` field; if the render site is reshaped
  // the assertions below would silently pin nothing.
  assert.equal(
    ANSWER_SITES.length,
    1,
    `found ${ANSWER_SITES.length} FAQ answer render site(s) in ${PAGE} — the guard below is written ` +
      "against exactly one <p>{f.a}</p>, so its shape changed and this test must be re-read",
  );
  assert.ok(
    FAQ_ANSWERS.length >= 3,
    `found ${FAQ_ANSWERS.length} FAQ answer(s) with an \`a\` string — the page renders more than that, ` +
      "and the URL assertion below would be checking almost nothing",
  );
  // The data still carries the token that made the line wider than the viewport,
  // as ONE whitespace-free run long enough to need the guard. Same rule as the
  // /docs/api prose guards: shortening the text to make it fit is not a fix.
  const mcp = FAQ_ANSWERS.find((a) => a.includes(MCP_URL));
  assert.ok(mcp, `no FAQ answer carries ${MCP_URL} any more — re-measure /docs at 320px/24px before ` +
    "removing this guard: the pan it fixes was that URL's ink");
  const longestRun = Math.max(...(mcp as string).split(/\s+/).map((run) => run.length));
  assert.ok(
    longestRun >= 25,
    `the longest whitespace-free run in the MCP answer is ${longestRun} chars — it no longer needs a ` +
      "break guard, so re-check whether this fix is still required",
  );
});

test("the FAQ answer paragraph lets an unbreakable token break", () => {
  for (const attrs of ANSWER_SITES) {
    const cls = attrs.match(/className="([^"]*)"/)?.[1] ?? "";
    assert.match(
      cls,
      BREAK_GUARD,
      `the FAQ answer <p> has className "${cls}" with no break guard — an answer that carries a URL ` +
        "(https://mcp.jomform.com/mcp) is one unbreakable run at a raised browser font size and paints " +
        "past a 320px viewport (measured: text-node right 344.95, documentElement.scrollWidth 345 vs " +
        "clientWidth 320)",
    );
    // …and it must be `anywhere`, not a value that only breaks at an existing
    // opportunity: the URL has none, and `anywhere` is also what lowers the
    // paragraph's min-content width so it can shrink inside the column.
    assert.match(
      cls,
      /\[overflow-wrap:anywhere\]|\bbreak-all\b/,
      `the FAQ answer <p> guards with "${cls}" — a value that does not introduce a break opportunity ` +
        "inside a run with no whitespace cannot fix this token",
    );
  }
});
