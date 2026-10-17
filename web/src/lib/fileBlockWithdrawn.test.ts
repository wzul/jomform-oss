// Card t_c28b45ea — the builder palette must not offer a `file` block, and no
// web surface may promise an attachment JomForm cannot deliver.
//
// THE DEFECT. Of the 30 palette types, `file` is the one that can neither be
// answered nor stored: no public upload route exists (every upload endpoint sits
// behind an authenticated account and charges that workspace's media quota), so
// an anonymous buyer has no way to deposit bytes. Both public renderers print
// the merchant's label plus a note instead of a picker, and the API refuses a
// config that carries the block (`formfields.UnsupportedBlock` — card t_4086b8f0,
// PR 1219; the AI generator's allowlist was the other surface still offering it
// and is pinned by api/internal/formgen/file_block_withdrawn_test.go). What was
// left was the palette item itself: a merchant could drag `📎 File upload` onto
// the canvas, build the form, and only learn at save time that the block is
// refused. `coupon` got exactly this withdrawal (card t_4975927e).
//
// WHY A SOURCE SCAN AND NOT A RENDER. `PALETTE_GROUPS` is a plain array inside a
// .tsx module; this repo has no DOM/JSX test runner, so the palette's copy is
// pinned the way the other CSS/source guards pin theirs (`lib/jsxSource.ts`
// blanks comments, then the item is matched). The API half of the rule is a Go
// test beside the code. WHAT THIS DOES NOT COVER, stated honestly: it cannot tell
// that a palette renders correctly, and it says nothing about the served HTML —
// only that the literal is absent from the source a merchant is served.
//
// BOTH DIRECTIONS. `paletteFileItems` is asserted against a fixture that DOES
// carry the item, and against a comment that quotes it, so a scan that silently
// matches nothing (a wrong path, a broken regex) fails instead of reporting a
// clean tree.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { codeOnly } from "./jsxSource.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

/**
 * The `file` palette ITEM lines of a builder source. Comments are blanked first:
 * the page and this file both quote the item they removed, so a raw scan would
 * report the explanation instead of a re-added entry. Anchored to the item's own
 * shape (`{ type: "file"`) so the image-upload code in the same file — which
 * carries `type="file"` JSX attributes and `fd.append("file", …)` — cannot
 * satisfy or trip it.
 */
function paletteFileItems(src: string): string[] {
  return codeOnly(src)
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => /^\{ type: "file"[,}]/.test(line));
}

test("the scan finds the item in a fixture that carries it (both directions)", () => {
  const fixture = [
    "const PALETTE_GROUPS = [",
    '  { key: "advanced", items: [',
    '    { type: "address", icon: "⌂" },',
    '    { type: "file", icon: "📎" },',
    "  ] },",
    "];",
  ].join("\n");
  assert.equal(
    paletteFileItems(fixture).length,
    1,
    "the scan matched nothing in a fixture that carries the palette item — check the regex and the path",
  );

  // …it is not a blanket matcher: an ordinary palette has no findings…
  assert.deepEqual(
    paletteFileItems('const P = [{ type: "consent", icon: "✓" }, { type: "captcha", icon: "🛡" }];'),
    [],
  );

  // …and a COMMENT quoting the withdrawn item cannot satisfy it either.
  assert.deepEqual(paletteFileItems('// withdrawn again: { type: "file", icon: "📎" }'), []);
});

test("the builder palette no longer offers a file block", () => {
  const builder = read("../app/builder/page.tsx");
  assert.ok(
    builder.length > 50_000,
    `builder/page.tsx read back suspiciously short (${builder.length} chars) — check the path before trusting a clean scan`,
  );
  assert.deepEqual(
    paletteFileItems(builder),
    [],
    "the palette offers a `file` block again. No renderer can collect it and the API refuses a config that " +
      "carries it (formfields.UnsupportedBlock), so placing one only builds a form that cannot be saved. " +
      "Withdraw the item — do not re-offer it.",
  );
});

test("the Advanced group is otherwise intact", () => {
  // Without this, deleting the whole group would satisfy the check above. The
  // block that was withdrawn is `file`; the rest of the group stays.
  const builder = codeOnly(read("../app/builder/page.tsx"));
  for (const t of ["address", "consent", "hidden", "list", "repeater", "image-choice", "captcha"]) {
    assert.ok(
      builder.includes(`{ type: "${t}"`),
      `the Advanced palette lost its ${t} block — the withdrawal must be scoped to file`,
    );
  }
});

test("the withdrawn block keeps its dictionary entry — a legacy form still draws its chip", () => {
  // The withdrawal is about PLACING the block, not about a form that already
  // stored it. The canvas paints `typeLabel(b.type)` for every stored block
  // (builder/page.tsx), and t() falls back to the RAW KEY when the dictionary
  // entry is missing — deleting these would print "blk.file" as the chip of a
  // form saved before the withdrawal. Same reason the BlockType union keeps the
  // member.
  const dict = read("../components/i18nDict.ts");
  for (const key of ['"blk.file":', '"blk.fileHint":']) {
    assert.ok(
      dict.includes(key),
      `${key} is gone from the dictionary: the builder renders typeLabel(b.type) for stored blocks and falls ` +
        `back to the raw key, so a legacy form would show "blk.file" as its label. Keep the entry.`,
    );
  }
});
