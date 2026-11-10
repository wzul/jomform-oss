// Guard for card t_6dbc1e16 — the form's COLLECTION MODE moved from the bottom
// of the builder's field-settings panel into the builder header.
//
// WHAT WAS WRONG (measured on the source, before this change):
//   * The aside is titled "Block settings" (`bld.blockSettings`) and renders ONE
//     selected block. The form-mode card and the "Form settings →" card were
//     drawn UNCONDITIONALLY at its bottom — two scopes, one heading, no divider
//     — while the builder's own comment said "the builder is fields-only".
//   * On a phone the panel is a full-screen scroll (~1,450px tall with a choice
//     field selected), so a form-level control sat at the bottom of a field
//     editor.
//   * Switching Payment → Response has consequences no two-button row can show:
//     an untouched default notification is re-pointed on the next save, and
//     payment blocks already placed are not drawn by the response page at all.
//
// WHAT THIS PINS (each is a way the defect comes back):
//   1. The header renders the mode control, and it needs no selected block.
//   2. The aside renders NO form-level control: no mode key, no notifications
//      doc link, no /form-settings link. Re-adding the old card to the aside
//      turns this red — that is the bite proof.
//   3. The choice is applied from ONE place (the dialog's Confirm button), so a
//      mode change cannot bypass the consequence statement again.
//   4. The dialog really states the consequences: the pricing-block warning and
//      the notification re-point note exist and are rendered only for a change.
//   5. Both new keys exist in all five locales.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * This repo has no JSX/DOM test runner (`npm run test:unit` is `node --test`
//     over `src/lib/*.test.ts`), so both files are read as TEXT. That proves the
//     control is DECLARED in the header and the dialog's wiring is intact; that
//     the browser paints it is shown by the screenshots on the card and by the
//     a11y-authed-pages axe run, not here.
//   * It does not judge translation quality (i18n.test.ts says the same of
//     itself).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { DICT, LANGS } from "../components/i18nDict.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const BUILDER = read("../app/builder/page.tsx");
const MODE_CONTROL = read("../components/FormModeControl.tsx");

/** The source between two anchors, which must both exist. */
function region(src: string, start: string, end: string): string {
  const at = src.indexOf(start);
  assert.ok(at >= 0, `anchor not found: ${start}`);
  const stop = src.indexOf(end, at);
  assert.ok(stop > at, `closing anchor ${end} not found after ${start}`);
  return src.slice(at, stop);
}

const HEADER = region(BUILDER, "<header", "</header>");
// The SETTINGS panel, not the palette: the left column is an <aside> too
// ("Drag to add"), and a slice of the wrong one would make every assertion
// below meaningless while still passing.
const ASIDE = region(BUILDER, "<aside className={`border-l", "</aside>");

// ── (1) the mode control is in the header ────────────────────────────────

test("the builder header renders the form-mode control", () => {
  assert.match(
    HEADER,
    /<FormModeControl\b/,
    "the builder header no longer renders <FormModeControl/> — the form's mode is not reachable without " +
      "opening a block's settings panel, which is the defect this card fixed",
  );
  // `saved` gates the notification note; `pricingBlockCount` is the number the
  // pricing warning states. A control mounted without them could not state the
  // consequences it exists to state.
  assert.match(HEADER, /pricingBlockCount=\{pricingBlockCount\}/, "the header passes no payment-block count to the mode control");
  assert.match(HEADER, /saved=\{!!currentId\}/, "the header no longer tells the mode control whether the form is saved yet");
});

test("the control reads the current mode and opens a dialog", () => {
  // The button's visible text is the current mode (bld.modePayment /
  // bld.modeResponse), and it advertises the dialog it opens.
  assert.match(
    MODE_CONTROL,
    /const currentLabel = t\(mode === "response" \? "bld\.modeResponse" : "bld\.modePayment"\)/,
    "the header button no longer names the current mode",
  );
  assert.match(MODE_CONTROL, /aria-haspopup="dialog"/, "the header button no longer advertises the dialog it opens");
  assert.match(MODE_CONTROL, /role="dialog"/, "the mode panel is no longer a dialog");
  assert.match(MODE_CONTROL, /aria-modal="true"/, "the mode dialog is no longer modal");
  assert.match(MODE_CONTROL, /aria-labelledby="jf-form-mode-title"/, "the mode dialog has no accessible name");
});

// ── (2) the aside is field settings only ─────────────────────────────────

test("the settings panel renders no form-level control", () => {
  // Every key and link the form-level surface owned, by name, so this fails for
  // the right reason and not because some unrelated text changed.
  for (const key of ["bld.mode", "bld.modePayment", "bld.modeResponse", "bld.modeDocs"]) {
    assert.ok(
      !ASIDE.includes(`t("${key}")`),
      `the block-settings aside renders t("${key}") again — a form-level control is back inside a panel ` +
        "whose own heading says it edits the selected block",
    );
  }
  assert.ok(
    !ASIDE.includes("/docs/email#notifications"),
    "the block-settings aside carries the notifications doc link again — that link documents the form mode, " +
      "which is not a block setting",
  );
  assert.ok(
    !ASIDE.includes("/form-settings?form="),
    'the "Form settings →" card is back in the block-settings aside — form settings is form-level, so the ' +
      "header carries the link next to the mode (card t_6dbc1e16)",
  );
});

test("the form-level cards are gone from the aside but the block settings remain", () => {
  // The removal must not have taken the block settings with it: the panel still
  // draws the selected block's fields and its own empty state.
  assert.match(ASIDE, /\{selectedBlock && renderFieldSettings\(selectedBlock\)\}/, "the aside no longer renders the selected block's settings");
  assert.match(ASIDE, /t\("bld.selectBlock"\)/, "the aside lost its empty state");
  assert.match(ASIDE, /t\("bld.blockSettings"\)/, "the aside lost its own scope heading");
});

// ── (3) nothing is applied without the dialog ────────────────────────────

test("the mode is applied from ONE place: the dialog's confirm button", () => {
  const applies = [...MODE_CONTROL.matchAll(/onApply\(/g)];
  assert.equal(
    applies.length,
    1,
    `onApply is called from ${applies.length} sites; expected exactly the confirm button, or a mode change can ` +
      "bypass the consequences the dialog states",
  );
  // The single call must be inside `apply()`, and `apply` must be gated on the
  // choice actually differing from the current mode.
  const applyFn = region(MODE_CONTROL, "function apply()", "function onKeyDown");
  assert.match(applyFn, /if \(pending !== mode\) onApply\(pending\)/, "apply() no longer refuses a no-op change or no longer applies the pending mode");
  assert.match(MODE_CONTROL, /onClick=\{apply\}/, "the confirm button no longer runs apply()");
  // The option buttons only move the PENDING choice — they must not apply it.
  const options = region(MODE_CONTROL, "MODES.map((m)", "The consequences");
  assert.match(options, /onClick=\{\(\) => setPending\(m\)\}/, "picking a mode no longer only stages it");
  assert.doesNotMatch(options, /onApply/, "an option button applies the mode directly, so the confirm step is gone");
});

test("the confirm button is inert until the choice actually changes", () => {
  assert.match(MODE_CONTROL, /const changed = pending !== mode;/, "the dialog no longer tracks whether the choice changes the form");
  assert.match(MODE_CONTROL, /disabled=\{!changed\}/, "Confirm is clickable when nothing would change");
});

// ── (4) the dialog states the consequences ───────────────────────────────

test("the mode dialog states the notification re-point and the payment-block consequence", () => {
  assert.match(
    MODE_CONTROL,
    /t\("bld\.modeNotifyNote", \{ event: eventFor\(pending\) \}\)/,
    "the dialog no longer says the default notification will be re-pointed — that is the side effect a merchant " +
      "cannot see from two buttons (t_a58848bd)",
  );
  assert.match(
    MODE_CONTROL,
    /t\("bld\.modePricingNote", \{ n: pricingBlockCount \}\)/,
    "the dialog no longer states how many payment blocks a response form will not draw",
  );
  // Both notes are rendered only for a CHANGE: shown on the current mode they
  // would read as a warning about a choice the merchant is not making.
  assert.match(
    MODE_CONTROL,
    /\{changed && \(\(pending === "response" && pricingBlockCount > 0\) \|\| saved\) && \(/,
    "the consequence block no longer depends on a changed choice / on the two facts it states",
  );
  // The notification note is about a rule that ALREADY EXISTS: a new form is
  // created on its mode, so there is nothing to re-point and the note is gated
  // on `saved`.
  assert.match(MODE_CONTROL, /\{saved && \(/, "the notification note is no longer gated on the form already existing");
});

test("the consequences name the events they claim", () => {
  // The event names are the API's own (dbstore.NotifyEventSalePaid /
  // NotifyEventResponseSubmitted). A note that named a different event would
  // describe a re-point the server does not perform.
  assert.match(MODE_CONTROL, /m === "response" \? "response\.submitted" : "sale\.paid"/, "the dialog no longer names the event each mode fires");
});

test("both new keys exist in all five locales", () => {
  // DICT is a `const` object, so a computed key needs a widened view.
  const dict = DICT as unknown as Record<string, Record<string, string>>;
  for (const key of ["bld.modePricingNote", "bld.modeNotifyNote"]) {
    const entry = dict[key];
    assert.ok(entry, `the dictionary has no "${key}" entry, so the dialog renders its raw key`);
    const missing = LANGS.filter((l) => !entry[l] || !String(entry[l]).trim());
    assert.deepEqual(missing, [], `"${key}" is missing these locales: ${missing.join(", ")}`);
  }
  // And their placeholders survive translation — a dropped {n} would print a
  // sentence with no number in it.
  for (const key of ["bld.modePricingNote", "bld.modeNotifyNote"]) {
    const en = dict[key].en;
    const token = en.includes("{n}") ? "{n}" : "{event}";
    for (const l of LANGS) {
      assert.ok(String(dict[key][l]).includes(token), `"${key}" loses ${token} in locale ${l}`);
    }
  }
});
