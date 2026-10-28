// Guards for the legacy-`text` repair path (card t_4823943a).
//
// A `text` block is a short-answer INPUT now; its `value` is the field's LABEL.
// Forms written under the old content model stored their intro SENTENCE there,
// and on the live form that sentence is painted as the label of an empty input:
//
//   <label class="lbl">Register for our event — cuma rsvp ja</label>
//   <input type="text" name="qoubfpj" data-id="qoubfpj">
//
// The builder cannot migrate those rows — `text` is a legitimate label too, and
// only the merchant knows which is which — so it DETECTS the shape, warns, and
// offers an explicit "Treat as intro line" action plus its exact inverse.
//
// These tests pin both halves of that contract:
//
//   1. THE DETECTOR, both directions: every prose row the card measured on prod
//      flags, and the real label shapes (`Abc`, `Nama anda`, …) stay quiet. A
//      detector that flags everything would be as useless as one that flags
//      nothing: it would train the merchant to dismiss the warning.
//   2. THE ACTION + ITS INVERSE, asserted on the round-tripped CONFIG (JSON
//      bytes), not on a toast: the block id and value survive unchanged, every
//      other setting survives unchanged, and convert-then-undo restores the
//      stored JSON exactly.
//
// The five draft rows the card counted are not quoted byte-for-byte in the card
// (it says only "prose intros"), so the draft-class arms below use intros of the
// same shape in both languages rather than inventing a merchant's stored value.
import { test } from "node:test";
import assert from "node:assert/strict";

import { looksLikeProse, treatAsFieldLabel, treatAsIntro } from "./textBlockProse.ts";

// The prose rows the card measured read-only on prod, as quoted in it. The
// builder-demo one is the card's own truncation (the stored value is longer).
const PROD_PROSE_ROWS = [
  "Selamat datang! Borang ini dibina dengan drag-drop builder b…",
  "Support our cause — every donation makes a difference.",
  "Register for our event — cuma rsvp ja",
  "Order from our menu — pay online when you submit.",
];

test("detector: every prose row measured on prod flags", () => {
  for (const row of PROD_PROSE_ROWS) {
    assert.equal(
      looksLikeProse(row),
      true,
      `a merchant's intro sentence did not flag, so it will render as an input label: ${JSON.stringify(row)}`,
    );
  }
});

test("detector: draft-form intros of the same shape flag", () => {
  // The five DRAFT rows were counted ("prose intros") but their bytes are not in
  // the card, so these are the same shape rather than a claim about those rows:
  // a multi-word intro sentence in each language the product's forms are written
  // in.
  for (const intro of [
    "Isi borang di bawah untuk menyertai cabutan bertuah kami.",
    "Welcome! Fill in your details below and we will get back to you.",
    "Tempah slot anda hari ini — pilih tarikh dan masa yang sesuai.",
    "Join our contest by answering three quick questions.",
  ]) {
    assert.equal(looksLikeProse(intro), true, `intro sentence not flagged: ${JSON.stringify(intro)}`);
  }
});

test("detector: real field labels never flag", () => {
  // `Abc` and `Nama anda` are named in the card: both are stored in `text`
  // blocks on published forms and both are LEGITIMATE labels. Flagging them
  // would tell a merchant to convert a real field into prose.
  for (const label of [
    "Abc",
    "Nama anda",
    "Nama penuh",
    "Nombor telefon bimbit",
    "Tarikh lahir",
    "Alamat emel",
    "No. telefon",
    "Isi alamat emel anda",
    "Emel",
    "Syarikat",
  ]) {
    assert.equal(looksLikeProse(label), false, `a field label was flagged as prose: ${JSON.stringify(label)}`);
  }
});

test("detector: the word-count boundary is where it says it is", () => {
  // Five words without a terminator is a long label, not a sentence; the sixth
  // word is where this detector starts calling it prose (see the module header).
  assert.equal(looksLikeProse("Nama penuh seperti dalam kad"), false);
  assert.equal(looksLikeProse("Nama penuh seperti dalam kad pengenalan"), true);
  // A terminator lowers the bar to four words — a short sentence, not a label.
  // Three words and a "!" is still label-shaped ("Terima kasih semua!" sits on
  // the same shelf as "Siapa nama anda?"), so it stays quiet on purpose.
  assert.equal(looksLikeProse("Terima kasih semua!"), false);
  assert.equal(looksLikeProse("Terima kasih semua ya!"), true);
});

test("detector: a long caption flags by LENGTH even without punctuation", () => {
  // The card names LENGTH as a signal. This input reaches only that rule: five
  // words (under the six-word bar), no terminator, no line break.
  assert.equal(
    looksLikeProse("Maklumat peribadi pemohon diperlukan segera"),
    true,
    "a 43-character caption stayed quiet, so the length signal is dead",
  );
  // The same five-word shape just under the 40-character bar is still a label —
  // the boundary is real, not a rule that fires on every long-ish value.
  assert.equal(
    looksLikeProse("Maklumat peribadi pemohon diisi segera"),
    false,
    "a 38-character label was flagged; the length rule is firing below its own bar",
  );
});

test("detector: a value broken across lines flags even when short", () => {
  // The block's value textarea accepts a newline. Three words over two lines is
  // already an intro rather than a field name, and this input reaches only the
  // line-break rule (three words, no terminator, well under 40 characters).
  assert.equal(looksLikeProse("Nama\nanda sekarang"), true, "a multi-line value stayed quiet");
  // The identical words on one line are a perfectly ordinary label.
  assert.equal(looksLikeProse("Nama anda sekarang"), false, "a one-line three-word label was flagged");
});

test("detector: empty and whitespace-only values are never prose", () => {
  for (const v of ["", "   ", "\n\t "]) {
    assert.equal(looksLikeProse(v), false, `an empty label was flagged: ${JSON.stringify(v)}`);
  }
  // A block whose value is absent (a stored config that omits the key) must not
  // throw — the builder reads `b.value` straight through.
  assert.equal(looksLikeProse(undefined), false);
  assert.equal(looksLikeProse(null), false);
});

// The stored shape of the block the card measured on `event-registration`, plus
// the other settings such a block can carry. Key ORDER matters here: the
// round-trip assertion is on JSON bytes.
const STORED = [
  { id: "nm", type: "name", value: "Nama anda" },
  {
    id: "qoubfpj",
    type: "text",
    value: "Register for our event — cuma rsvp ja",
    placeholder: "Nama",
    max_length: 120,
    required: true,
  },
  { id: "b-last", type: "email", value: "Emel" },
];

test("action: 'treat as intro' keeps the block id and value byte for byte", () => {
  const out = treatAsIntro(STORED, "qoubfpj");
  const before = STORED[1];
  const after = out[1];

  assert.equal(after.type, "section");
  assert.equal(after.id, before.id, "the block id is the field name a stored contract may name — it must not move");
  assert.equal(after.value, before.value, "the merchant's sentence must not be edited by a retype");
  // Everything ELSE the merchant set on that block survives too, so the inverse
  // can restore the block exactly.
  assert.deepEqual(
    { ...after, type: "text" },
    before,
    "the retype changed a setting other than the block type",
  );
  // The other blocks are not copies and are not touched.
  assert.equal(out[0], STORED[0]);
  assert.equal(out[2], STORED[2]);
  // The input array is never mutated.
  assert.equal(STORED[1].type, "text");
});

test("action: the inverse restores the original JSON shape (round-tripped config)", () => {
  // What a save/load cycle sees: the config through JSON, as the API stores it.
  const storedJson = JSON.stringify({ mode: "response", blocks: STORED });
  const stored = JSON.parse(storedJson) as { mode: string; blocks: typeof STORED };

  const converted = treatAsIntro(stored.blocks, "qoubfpj");
  const convertedJson = JSON.stringify(converted);
  assert.ok(
    convertedJson.includes('"type":"section"'),
    `the save would not carry the converted type: ${convertedJson}`,
  );
  assert.ok(
    convertedJson.includes('"value":"Register for our event — cuma rsvp ja"'),
    `the save would not carry the merchant's sentence unchanged: ${convertedJson}`,
  );

  // The exact inverse, on the config the builder now holds.
  const restored = treatAsFieldLabel(converted, "qoubfpj");
  assert.equal(
    JSON.stringify(restored),
    JSON.stringify(stored.blocks),
    "convert-then-undo did not restore the stored config byte for byte",
  );
  assert.equal(JSON.stringify({ mode: stored.mode, blocks: restored }), storedJson);
});

test("action: it refuses to retype anything but the block it was asked about", () => {
  // An unknown id, an id whose type is not what the action expects, and a block
  // that is already a section: all three return the config unchanged, so a
  // stale click cannot rewrite a different block or a section the merchant
  // authored as prose.
  for (const [blocks, id] of [
    [STORED, "no-such-block"],
    [STORED, "nm"], // a `name` field, not `text`
    [treatAsIntro(STORED, "qoubfpj"), "qoubfpj"], // already a section
  ] as const) {
    const out = treatAsIntro(blocks as typeof STORED, id);
    assert.equal(JSON.stringify(out), JSON.stringify(blocks), `treatAsIntro rewrote something for ${JSON.stringify(id)}`);
  }
  // `treatAsFieldLabel` is the inverse BY TYPE. Scoping it to the block the
  // merchant just converted is the CALLER's job: the builder offers this button
  // only on the id it converted in this session (pinned in
  // web/e2e/builder-text-prose-intro.spec.ts), so a section the merchant
  // authored as prose has no path to it.
  const sectionOnly = [{ id: "s1", type: "section", value: "Selamat datang" }];
  assert.equal(
    JSON.stringify(treatAsFieldLabel(sectionOnly, "s1")),
    JSON.stringify([{ id: "s1", type: "text", value: "Selamat datang" }]),
    "the inverse did not restore a converted block to a text field",
  );
});
