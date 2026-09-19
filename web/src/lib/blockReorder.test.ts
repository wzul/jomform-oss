// Regression guard for the reported defect: the canvas ↓ ("move down") button
// in the form builder did nothing, while ↑ worked.
//
// THE BUG (one expression): the two buttons were wired
//
//   up   : move(i, i - 1)
//   down : move(i, i + 1)
//
// and `move(from, to)` delegated to `insertAt(to, null, from)`, whose target
// index is resolved against the array as it stood BEFORE the dragged row was
// removed (`const target = fromIdx < i ? i - 1 : i` — the semantics the
// drag-and-drop gap drop-zones require). For ↓ that expression collapses to
// `(i + 1) - 1 = i`, i.e. the row is spliced out and re-inserted at the index it
// already occupied. Verified on the pre-fix logic: ↓ was a no-op at EVERY index
// (0..n-1) in a 4-row list, while ↑ moved the row correctly at every index
// except the first.
//
// WHY THE TEST IS SHAPED THIS WAY
//   1. The fix is a pure function (`moveBlock`), so the reorder maths is
//      asserted directly, at every index, in both directions — the exact axis
//      the bug lived on. A test that only checked "the list changed" would have
//      caught it, but one that checks the precise resulting order also pins the
//      direction (a reversed swap would otherwise pass a changed-length check).
//   2. A pure function can be fully correct and still not be what the buttons
//      call, so the call sites in builder/page.tsx are pinned too — by source
//      scan, since this repo has no DOM/JSX test runner (see
//      src/lib/adminResponsive.test.ts for the same technique). Without this
//      the whole feature could be dead code behind a green suite.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { canMove, moveBlock } from "./blockReorder.ts";

const ROWS = ["a", "b", "c", "d"];

test("move down moves a row exactly one position lower (the reported bug)", () => {
  // Pre-fix this returned the input unchanged for every one of these indices.
  assert.deepEqual(moveBlock(ROWS, 0, 1), ["b", "a", "c", "d"]);
  assert.deepEqual(moveBlock(ROWS, 1, 1), ["a", "c", "b", "d"]);
  assert.deepEqual(moveBlock(ROWS, 2, 1), ["a", "b", "d", "c"]);
});

test("move up moves a row exactly one position higher and is unchanged", () => {
  assert.deepEqual(moveBlock(ROWS, 3, -1), ["a", "b", "d", "c"]);
  assert.deepEqual(moveBlock(ROWS, 2, -1), ["a", "c", "b", "d"]);
  assert.deepEqual(moveBlock(ROWS, 1, -1), ["b", "a", "c", "d"]);
});

test("up and down are exact inverses at every interior index", () => {
  for (let i = 1; i < ROWS.length - 1; i++) {
    assert.deepEqual(
      moveBlock(moveBlock(ROWS, i, 1), i + 1, -1),
      ROWS,
      `down-then-up at index ${i} did not return to the original order`,
    );
    assert.deepEqual(
      moveBlock(moveBlock(ROWS, i, -1), i - 1, 1),
      ROWS,
      `up-then-down at index ${i} did not return to the original order`,
    );
  }
});

test("down is a no-op at the last position and up at the first", () => {
  // The array reference must come back identical, not merely equal — a click at
  // a boundary should not mark the form dirty or schedule a re-render.
  assert.equal(moveBlock(ROWS, ROWS.length - 1, 1), ROWS, "down on the last row must be a no-op");
  assert.equal(moveBlock(ROWS, 0, -1), ROWS, "up on the first row must be a no-op");
  assert.equal(canMove(ROWS, ROWS.length - 1, 1), false);
  assert.equal(canMove(ROWS, 0, -1), false);
  assert.equal(canMove(ROWS, 0, 1), true);
  assert.equal(canMove(ROWS, ROWS.length - 1, -1), true);
});

test("a row walked down repeatedly stops at the last position", () => {
  // The index comes from the row's map position, so it advances with the row.
  let rows = ROWS.slice();
  let at = 1;
  for (let n = 0; n < 10; n++) {
    const next = moveBlock(rows, at, 1);
    if (next === rows) break; // at the last position: the button is disabled
    rows = next;
    at += 1;
  }
  assert.deepEqual(rows, ["a", "c", "d", "b"], "the row did not settle at the last position");
  assert.equal(at, ROWS.length - 1, "the row did not stop at the last index");
  assert.equal(canMove(rows, at, 1), false);
});

test("out-of-range indices cannot corrupt the list", () => {
  assert.equal(moveBlock(ROWS, -1, 1), ROWS);
  assert.equal(moveBlock(ROWS, ROWS.length, 1), ROWS);
  assert.equal(canMove(ROWS, -1, 1), false);
  assert.equal(canMove(ROWS, ROWS.length, 1), false);
});

test("a single-row canvas cannot be reordered in either direction", () => {
  const only = ["only"];
  assert.equal(moveBlock(only, 0, 1), only, "down on a single-row canvas must be a no-op");
  assert.equal(moveBlock(only, 0, -1), only, "up on a single-row canvas must be a no-op");
  assert.equal(canMove(only, 0, 1), false);
  assert.equal(canMove(only, 0, -1), false);
});

test("reordering never mutates the input array (React state must be replaced)", () => {
  for (let i = 0; i < ROWS.length; i++) {
    for (const dir of [-1, 1] as const) {
      const before = ROWS.slice();
      const out = moveBlock(ROWS, i, dir);
      assert.deepEqual(ROWS, before, `moveBlock mutated the input for i=${i} dir=${dir}`);
      if (out !== ROWS) {
        assert.notEqual(out, ROWS, "a successful move must return a new array");
      }
    }
  }
});

// ── wiring ───────────────────────────────────────────────────────────────────
// The buttons live in the canvas block card of web/src/app/builder/page.tsx.
// Both are found by their translated title so the assertions cannot be
// satisfied by some other button in the file.

const builder = readFileSync(
  fileURLToPath(new URL("../app/builder/page.tsx", import.meta.url)),
  "utf8",
);

function buttonFor(titleKey: string): string {
  // Locate the title and walk out to the enclosing <button>. A regex over the
  // button's attribute span cannot work: onClick={(e) => {...}} contains a ">".
  const at = builder.indexOf(`title={t("${titleKey}")}`);
  assert.ok(at >= 0, `no button with title t("${titleKey}") found in the builder canvas`);
  const open = builder.lastIndexOf("<button", at);
  const close = builder.indexOf("</button>", at);
  assert.ok(open >= 0 && close > open, `could not delimit the button titled t("${titleKey}")`);
  return builder.slice(open, close + "</button>".length);
}

test("the canvas ↑/↓ buttons both call the shared reorder, with opposite directions", () => {
  const up = buttonFor("bld.moveUp");
  const down = buttonFor("bld.moveDown");
  assert.match(up, /moveRow\(i,\s*-1\)/, "the move-up button no longer reorders by -1");
  assert.match(down, /moveRow\(i,\s*1\)/, "the move-down button no longer reorders by +1");
});

test("the reorder handler applies moveBlock through setBlocks", () => {
  // The full chain: button -> moveRow -> setBlocks -> moveBlock. A correct
  // moveBlock that the handler never reaches is dead code.
  const handler = builder.slice(builder.indexOf("function moveRow"));
  assert.match(
    handler.slice(0, 400),
    /setBlocks\(\(prev\)\s*=>\s*moveBlock\(prev,\s*i,\s*dir\)\)/,
    "moveRow no longer applies the shared moveBlock to the blocks state",
  );
  assert.match(
    handler.slice(0, 400),
    /setDirty\(true\)/,
    "moveRow does not mark the builder dirty — a reorder would not prompt a save",
  );
});

test("neither canvas arrow still uses the index-mapping call that made down a no-op", () => {
  // `move(i, i + 1)` was the defect: insertAt resolves `to` against the
  // pre-removal array, so that call re-inserted the row at its own index.
  for (const [name, key] of [["up", "bld.moveUp"], ["down", "bld.moveDown"]] as const) {
    assert.doesNotMatch(
      buttonFor(key),
      /move\(/,
      `the move-${name} button is back on move(), whose insertAt index mapping made down a no-op`,
    );
  }
});

test("the down control is disabled at the last row and up at the first", () => {
  assert.match(
    buttonFor("bld.moveUp"),
    /disabled=\{!canMove\(blocks,\s*i,\s*-1\)\}/,
    "the move-up button is not disabled on the first row",
  );
  assert.match(
    buttonFor("bld.moveDown"),
    /disabled=\{!canMove\(blocks,\s*i,\s*1\)\}/,
    "the move-down button is not disabled on the last row",
  );
  assert.match(
    buttonFor("bld.moveDown"),
    /disabled:opacity-30/,
    "the disabled move-down button has no visible disabled styling",
  );
  assert.match(
    buttonFor("bld.moveUp"),
    /disabled:opacity-30/,
    "the disabled move-up button has no visible disabled styling",
  );
});

test("the order is persisted through the same save path as before", () => {
  // Reordering only sets component state; the order reaches the backend because
  // `save()` serialises `blocks` in array order. Pin that the serialiser still
  // reads the live `blocks` array, or a reorder would silently not survive save.
  assert.match(
    builder,
    /blocks:\s*blocks\.map\(\(b\)\s*=>/,
    "the save payload no longer serialises the live blocks array — a reorder would not persist",
  );
  assert.match(
    builder,
    /setBlocks\(\(prev\)\s*=>\s*moveBlock\(prev,\s*i,\s*dir\)\)/,
    "the reorder is no longer applied through setBlocks — the new order would not reach the canvas or the save payload",
  );
});
