// "Should the transcript follow the answer?" (card t_148d7100).
//
// THE DEFECT THIS GUARD IS FOR. The page called `scrollIntoView` on every change
// of `msgs.length`/`sending`. A reader who had scrolled up to re-read an earlier
// answer was pulled back to the bottom by the next message; with a streamed
// answer that pull would have happened many times a second.
//
// The decision is a pure function of the three numbers a browser reports, so the
// rule is executed here (this repo has no DOM test runner — see
// src/lib/i18n.test.ts) and the page only applies it.
import { test } from "node:test";
import assert from "node:assert/strict";

import { STICK_THRESHOLD_PX, isAtBottom } from "./chatScroll.ts";

test("scrolled to the end is at the bottom", () => {
  assert.equal(isAtBottom({ scrollTop: 900, scrollHeight: 1400, clientHeight: 500 }), true);
});

test("a scrolled-up reader is NOT at the bottom", () => {
  assert.equal(
    isAtBottom({ scrollTop: 300, scrollHeight: 1400, clientHeight: 500 }),
    false,
    "this is the case the old page got wrong: it scrolled anyway, taking the reader away from " +
      "the answer they had scrolled up to read",
  );
});

test("a couple of pixels short still counts as the bottom (rounding, not intent)", () => {
  assert.equal(
    isAtBottom({ scrollTop: 898.5, scrollHeight: 1400, clientHeight: 500 }),
    true,
    "fractional device pixel ratios leave a box that is visually at the end a fraction short; a " +
      "strict equality would stop following for a reader who never moved",
  );
});

test("the tolerance is a small distance, not half the viewport", () => {
  const justInside = { scrollTop: 900 - STICK_THRESHOLD_PX, scrollHeight: 1400, clientHeight: 500 };
  const justOutside = { scrollTop: 900 - STICK_THRESHOLD_PX - 1, scrollHeight: 1400, clientHeight: 500 };
  assert.equal(isAtBottom(justInside), true);
  assert.equal(
    isAtBottom(justOutside),
    false,
    `the boundary must sit at ${STICK_THRESHOLD_PX}px — a larger tolerance would follow a reader ` +
      "who really did scroll up",
  );
});

test("a transcript shorter than its box is always at the bottom", () => {
  assert.equal(
    isAtBottom({ scrollTop: 0, scrollHeight: 200, clientHeight: 500 }),
    true,
    "nothing to scroll away from: treating a short transcript as scrolled-up would hide the first " +
      "answer behind a Jump-to-latest button that does nothing",
  );
});
