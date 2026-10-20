// Card t_3abab22c — a stored block whose palette entry was withdrawn still has
// to render honestly on the builder canvas.
//
// THE DEFECT. Two surfaces read a block's own type and trusted it:
//   1. the card's badge, `typeLabel` -> `t("blk.<type>")`, and `t()` returns the
//      KEY when the dictionary has no entry — so a block whose entry left with
//      its palette item painted `blk.<type>` (uppercased by the card's styling)
//      where a word belongs. Measured live: `BLK.COUPON` above the merchant's
//      label `Kupon`.
//   2. `BlockPreview`'s `default` arm, which draws an empty bordered box that
//      reads as an input. For a type the public renderers deliberately serve as
//      "label + note, no control", that box promises a field no buyer will get.
//
// WHAT THIS FILE OWNS
//   * the two pure helpers that fix it (lib/withdrawnBlock.ts), against real
//     values — the raw key must never come back as a label;
//   * the WIRING, by reading the page source: the helpers are useless if the
//     canvas stops calling them, and a pure-helper test alone would stay green
//     on exactly that regression. The rendered half of the guard is
//     `web/e2e/builder-canvas-withdrawn-block.spec.ts`, which drives the real
//     canvas in a browser and reads the DOM.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { blockTypeLabel, isWithdrawnBlockType } from "./withdrawnBlock.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

// The withdrawn type this card was filed about. Named here (a test, not a web
// surface the withdrawal guard scans) so the case is the real one, not a
// synthetic stand-in.
const WITHDRAWN_KEY = "blk.coupon";

test("a type the dictionary still carries keeps its translation", () => {
  assert.equal(blockTypeLabel("blk.text", "Text"), "Text");
  assert.equal(blockTypeLabel("blk.note", "Note"), "Note");
  // …and a translation that happens to differ only in case is still used.
  assert.equal(blockTypeLabel("blk.email", "E-mel"), "E-mel");
});

test("a key the dictionary no longer carries becomes a real label, never the raw key", () => {
  const label = blockTypeLabel(WITHDRAWN_KEY, WITHDRAWN_KEY);
  assert.equal(label, "Coupon");
  assert.notEqual(label, WITHDRAWN_KEY, "the raw key came back as the label — that is the defect");
  assert.ok(!label.includes("."), `the label still looks like a key: ${label}`);
  assert.match(label, /^[A-Z]/, `the label is not capitalised: ${label}`);
});

test("a hyphenated or underscored stale type is humanised, not glued together", () => {
  const key = "blk.image-choice";
  assert.equal(blockTypeLabel(key, key), "Image choice");
  const underscored = "blk.legacy_widget";
  assert.equal(blockTypeLabel(underscored, underscored), "Legacy widget");
});

test("a key with no tail is handed back rather than turned into an empty label", () => {
  assert.equal(blockTypeLabel("blk.", "blk."), "blk.");
  assert.equal(blockTypeLabel("", ""), "");
});

test("the palette's own set decides what is withdrawn", () => {
  // The set the canvas builds from PALETTE_GROUPS. An offered type is drawn with
  // its control; only a type that is NOT offered is drawn as a stored label.
  const offered = new Set(["text", "textarea", "product", "total"]);
  assert.equal(isWithdrawnBlockType("text", offered), false);
  assert.equal(isWithdrawnBlockType("total", offered), false);
  assert.equal(isWithdrawnBlockType("coupon", offered), true);
  // An emptied set withdraws everything — the predicate is "not offered", not
  // "in a list of known withdrawals", so nothing has to be maintained twice.
  assert.equal(isWithdrawnBlockType("text", new Set<string>()), true);
});

test("the canvas wires both helpers: badge label and the preview gate", () => {
  const page = read("../app/builder/page.tsx");

  assert.ok(
    page.includes('from "../../lib/withdrawnBlock"') &&
      /import\s*\{[^}]*\bblockTypeLabel\b[^}]*\bisWithdrawnBlockType\b[^}]*\}/.test(page),
    "app/builder/page.tsx no longer imports the two helpers from lib/withdrawnBlock",
  );

  // 1. The badge. `typeLabel` must route the dictionary result through the
  //    fallback; calling `t()` alone is the defect.
  assert.ok(
    page.includes("blockTypeLabel(key, t(key))"),
    "typeLabel stopped passing the dictionary result through blockTypeLabel — a withdrawn type paints its raw key again",
  );
  assert.ok(
    !/const typeLabel = \(bt: BlockType\) => t\(/.test(page),
    "typeLabel is a bare t() again — a stored type with no dictionary entry renders `blk.<type>`",
  );

  // 2. The preview. The gate has to run BEFORE the switch: after it, the
  //    withdrawn type has already fallen into `default` (the input-shaped box).
  const gate = page.indexOf("isWithdrawnBlockType(block.type, OFFERED_BLOCK_TYPES)");
  const sw = page.indexOf("switch (block.type) {");
  assert.ok(gate > 0, "BlockPreview lost its isWithdrawnBlockType gate — a withdrawn block falls back to the input-shaped default");
  assert.ok(sw > 0, "BlockPreview's switch moved; re-anchor this guard rather than deleting it");
  assert.ok(gate < sw, "the withdrawn-block gate now runs after the switch, so it is unreachable for the block it guards");

  // 3. The note is the one the canvas renders, in the default locale.
  assert.ok(
    page.includes('t("bld.withdrawnBlock")'),
    'BlockPreview no longer renders t("bld.withdrawnBlock") — the preview would show a label with no explanation',
  );

  // 4. The offered set is derived from the palette, so a re-added palette item
  //    needs no second edit here.
  assert.ok(
    /const OFFERED_BLOCK_TYPES: ReadonlySet<string> = new Set\(\s*PALETTE_GROUPS\.flatMap/.test(page),
    "OFFERED_BLOCK_TYPES is no longer derived from PALETTE_GROUPS — the canvas and the palette can now disagree",
  );
});
