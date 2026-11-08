// The composer's growth rule (card t_148d7100).
//
// THE DEFECT. `rows={2}` + `resize-none` was a fixed two-line window onto a
// message the page accepts up to 2,000 characters of, with the one affordance
// that could have made it bigger disabled.
//
// The height is arithmetic on three numbers the browser reports (content height,
// line height, padding+border), so it is executed here rather than inferred from
// a rendering test: "does a 20-line paste stop at 8 lines" is a claim about the
// CLAMP, and the clamp is this function.
import { test } from "node:test";
import assert from "node:assert/strict";

import { CHAT_COMPOSER_MAX_ROWS, composerHeight } from "./chatComposerGrow.ts";

const LINE = 20;
const CHROME = 18; // 8px padding top + 8px bottom + 1px border each side

test("one line of text gives a one-line box", () => {
  assert.equal(composerHeight(LINE + CHROME, LINE, CHROME), LINE + CHROME);
});

test("the box grows with the content", () => {
  assert.equal(composerHeight(3 * LINE + CHROME, LINE, CHROME), 3 * LINE + CHROME);
  assert.equal(composerHeight(5 * LINE + CHROME, LINE, CHROME), 5 * LINE + CHROME);
});

test("it stops at the maximum and scrolls instead of swallowing the page", () => {
  const huge = 40 * LINE + CHROME;
  assert.equal(
    composerHeight(huge, LINE, CHROME),
    CHAT_COMPOSER_MAX_ROWS * LINE + CHROME,
    "a long paste must not make the composer taller than the transcript it sits under",
  );
  assert.ok(CHAT_COMPOSER_MAX_ROWS > 2, "the card asks for more than the two fixed rows it replaces");
});

test("never smaller than one line, whatever the browser reports", () => {
  // An empty textarea reports a scrollHeight that can be below one line (and 0 on
  // a box that has not been laid out yet). A height of 0 would collapse the input.
  assert.equal(composerHeight(4, LINE, CHROME), LINE + CHROME);
  assert.equal(composerHeight(0, LINE, CHROME), LINE + CHROME);
  assert.equal(composerHeight(Number.NaN, LINE, CHROME), LINE + CHROME);
});

test("chrome is carried through the clamp, not added to it", () => {
  const capped = composerHeight(99 * LINE, LINE, CHROME);
  assert.equal(
    capped - CHROME,
    CHAT_COMPOSER_MAX_ROWS * LINE,
    "scrollHeight already includes padding: adding chrome twice would make the ceiling taller than " +
      "the documented number of lines",
  );
});
