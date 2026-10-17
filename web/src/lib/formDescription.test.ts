// Card t_7fb80479 — the builder's half of "a form's description is its INTRO
// LINE, never a field's label", and the guard that the shared rule really is
// shared: this test reads the Go source it must agree with
// (api/internal/formfields/formfields.go, Description + proseBlockTypes) so the
// dashboard and the API cannot drift apart again. The drift IS the bug this
// card settles: three derivations read the first `text` block's value while
// both renderers painted `text` as an input, so a form described itself with a
// field label.
//
// Run with the repo's Node test runner:
//
//   npm run test:unit

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import { formDescription, PROSE_BLOCK_TYPES } from "./formDescription.ts";

const HERE = dirname(fileURLToPath(import.meta.url));
const GO_SOURCE = join(HERE, "..", "..", "..", "api", "internal", "formfields", "formfields.go");

test("a section intro wins over a text field's label, wherever it sits", () => {
  assert.equal(
    formDescription([
      { type: "text", value: "Cuba. Nama" },
      { type: "section", value: "Get in touch — we usually reply within a day." },
    ]),
    "Get in touch — we usually reply within a day.",
  );
  // …and a text FIELD after the intro must not displace it either.
  assert.equal(
    formDescription([
      { type: "section", value: "We value your feedback." },
      { type: "text", value: "Nama" },
    ]),
    "We value your feedback.",
  );
});

test("html is prose too", () => {
  assert.equal(
    formDescription([
      { type: "text", value: "Nama" },
      { type: "html", value: "Tell us what you need." },
    ]),
    "Tell us what you need.",
  );
});

test("an empty display block is skipped", () => {
  assert.equal(
    formDescription([
      { type: "section", value: "" },
      { type: "section", value: "   " },
      { type: "section", value: "Book your slot." },
    ]),
    "Book your slot.",
  );
});

test("the old content model keeps its description", () => {
  // The seeded library's shape until card t_d46c21e0 migrates those rows: the
  // intro sentence is still a `text` block's value.
  assert.equal(
    formDescription([
      { type: "text", value: "Order from our menu — pay online when you submit." },
      { type: "name", value: "Full name" },
    ]),
    "Order from our menu — pay online when you submit.",
  );
});

test("fields only, or no blocks, is no description", () => {
  assert.equal(
    formDescription([
      { type: "name", value: "Full name" },
      { type: "text", value: "   " },
      { type: "email", value: "Email" },
    ]),
    "",
  );
  assert.equal(formDescription([]), "");
});

test("the merchant's sentence is copied verbatim (no truncation, inner spaces kept)", () => {
  const sentence = `  ${"x".repeat(400)}  `;
  assert.equal(formDescription([{ type: "section", value: sentence }]), "x".repeat(400));
});

test("the Go rule is the same rule: same prose types, same legacy fallback", () => {
  const src = readFileSync(GO_SOURCE, "utf8");

  // The prose allowlist: parse proseBlockTypes and compare as a set.
  const prose = src.match(/var proseBlockTypes = map\[string\]bool\{([^}]*)\}/);
  assert.ok(prose, "proseBlockTypes not found in formfields.go — the Go rule moved; update this guard");
  const goProse = [...prose[1].matchAll(/"([a-z-]+)":\s*true/g)].map((m) => m[1]).sort();
  assert.deepEqual(goProse, [...PROSE_BLOCK_TYPES].sort());

  // The legacy fallback: Description must still read `text`.
  assert.match(src, /b\.Type == "text" && legacy == ""/, "the Go fallback to a text block is gone");

  // …and `text` is a field type on the Go side, which is WHY it cannot be the
  // description source: the two lists must agree that text is an input.
  const valid = src.match(/var ValidTypes = map\[string\]bool\{([\s\S]*?)\n\}/);
  assert.ok(valid, "ValidTypes not found in formfields.go");
  assert.match(valid[1], /"text":\s*true/, "ValidTypes lost `text` — the two halves of this rule disagree");
  const collectable = src.match(/var collectableTypes = map\[string\]bool\{([\s\S]*?)\n\}/);
  assert.ok(collectable, "collectableTypes not found in formfields.go");
  assert.match(collectable[1], /"text":\s*true/, "collectableTypes lost `text` — a text field's answer is not collected");
});
