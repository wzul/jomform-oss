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
//   6. The settings panel is found by IDENTITY — its own `data-jf-aside` marker,
//      or the border-l column before the page carried markers — so the order of
//      attributes on that tag is not something this guard can fail on, and a
//      failure names the page state that was found rather than a slice anchor
//      (t_85ed0e60: an attribute written before `className` failed all 9 tests).
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

// ── the settings aside, found by IDENTITY (t_85ed0e60) ───────────────────
//
// This used to read `region(BUILDER, "<aside className={`border-l", "</aside>")`
// — a slice anchored on the literal PREFIX of the opening tag. Any attribute
// written before `className` (`<aside data-jf-aside="settings" className={…}>`,
// a page with identical behaviour) failed the whole file with
//   anchor not found: <aside className={`border-l
// which names the guard's own anchor and says nothing about the page. The panel
// is a right-hand column with its own marker, so find it by that — attribute
// order is not a fact about the page — and make every failure name the page
// state that was found instead.

const PAGE = "web/src/app/builder/page.tsx";
const SETTINGS_MARK = 'data-jf-aside="settings"';

/** Index of the `>` closing the tag that opens at `at`, skipping a `>` inside a
 *  quoted string, a template literal or a `{…}` expression. -1 if it never
 *  closes. */
function tagEnd(src: string, at: number): number {
  let depth = 0;
  let quote: string | null = null;
  for (let i = at; i < src.length; i++) {
    const c = src[i];
    if (quote !== null) {
      if (c === "\\") i++; // an escaped character cannot end the string
      else if (c === quote) quote = null;
      continue;
    }
    if (c === '"' || c === "'" || c === "`") quote = c;
    else if (c === "{") depth++;
    else if (c === "}") depth = Math.max(0, depth - 1);
    else if (c === ">" && depth === 0) return i;
  }
  return -1;
}

type Element = { openTag: string; source: string; closed: boolean };

/** Every `<tag …>…</tag>` in `src`: its opening tag and the whole element. */
function elements(src: string, tag: string): Element[] {
  const out: Element[] = [];
  for (const m of src.matchAll(new RegExp(`<${tag}(?=[\\s/>])`, "g"))) {
    const at = m.index ?? -1;
    if (at < 0) continue;
    const end = tagEnd(src, at);
    if (end < 0) {
      out.push({ openTag: src.slice(at, at + 60), source: src.slice(at), closed: false });
      continue;
    }
    const openTag = src.slice(at, end + 1);
    const close = new RegExp(`</${tag}\\s*>`).exec(src.slice(end + 1));
    if (!close) {
      out.push({ openTag, source: openTag, closed: false });
      continue;
    }
    out.push({ openTag, source: src.slice(at, end + 1 + close.index + close[0].length), closed: true });
  }
  return out;
}

/** A className token, as its own class — `border-l`, but not `border-l-4`. */
const hasClass = (openTag: string, cls: string) => new RegExp(`(?<![\\w-])${cls}(?![\\w-])`).test(openTag);

/** What the page actually has, for a failure message that names the page and
 *  not an anchor in this file. */
const asideState = (asides: Element[]) =>
  asides.length
    ? `${asides.length} <aside> opening tag(s) present:\n` +
      asides.map((a, i) => `    [${i}] ${a.openTag.replace(/\s+/g, " ").slice(0, 140)}`).join("\n")
    : `${PAGE} renders no <aside> at all`;

/**
 * The field-settings panel — the aside this guard judges — by identity:
 *
 *   1. the aside whose opening tag carries `data-jf-aside="settings"`, where the
 *      page marks its columns; attribute order does not matter;
 *   2. otherwise the right-hand column: `border-l` and not `border-r` (the
 *      palette column is the `border-r` one). This is the pre-marker fallback.
 *
 * Throws a message naming the page state — never an anchor literal.
 */
function settingsAside(src: string): Element {
  const asides = elements(src, "aside");
  const marked = asides.filter((a) => a.openTag.includes(SETTINGS_MARK));

  if (marked.length > 1) {
    throw new Error(
      `${PAGE} marks ${marked.length} asides with ${SETTINGS_MARK}; this guard judges the ONE field-settings ` +
        `panel and cannot tell them apart. ${asideState(marked)}`,
    );
  }

  const matches = marked.length
    ? marked
    : asides.filter((a) => hasClass(a.openTag, "border-l") && !hasClass(a.openTag, "border-r"));

  if (matches.length !== 1) {
    const why = marked.length
      ? `the aside marked ${SETTINGS_MARK} also has to be a single element`
      : `no aside carries ${SETTINGS_MARK}, and ${matches.length} aside(s) match the border-l right column`;
    throw new Error(
      `the builder's field-settings panel cannot be located in ${PAGE} (${why}). This guard checks that the ` +
        `panel which edits ONE selected block holds no form-level control (mode, notifications doc link, ` +
        `"Form settings →"). ${asideState(asides)}`,
    );
  }

  const [panel] = matches;
  if (!panel.closed) {
    throw new Error(
      `the settings aside in ${PAGE} has no matching </aside>, so the panel cannot be delimited: ` +
        panel.openTag.replace(/\s+/g, " ").slice(0, 140),
    );
  }
  return panel;
}

/** Resolved per test, not at module load: a page that loses the panel should
 *  fail the assertions that judge it, each naming the page state — not abort the
 *  whole file as "the guard is broken". */
const settingsPanel = () => settingsAside(BUILDER).source;

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

test("the settings panel is found by identity, not by attribute ORDER", () => {
  // t_85ed0e60: the slice was anchored on the opening tag's literal prefix, so
  // moving an attribute in front of `className` — a page with identical
  // behaviour — failed the whole file with "anchor not found: <aside
  // className={`border-l". These four are the same two columns written four
  // ways; each must resolve to the settings panel and never to the palette.
  const palette = '<aside className={`border-r p-4`}>Drag to add</aside>';
  const writings = [
    '<aside data-jf-aside="settings" className={`border-l p-4`}>Block settings</aside>',
    '<aside className={`border-l p-4`} data-jf-aside="settings">Block settings</aside>',
    '<aside className={`border-l p-4`}>Block settings</aside>', // the pre-marker tree
  ];
  for (const settings of writings) {
    const panel = settingsAside(`<main>${palette}${settings}</main>`);
    assert.equal(panel.source, settings, `the settings aside was not resolved in: ${settings}`);
  }
  // The palette column is never the panel: a page with only the palette has no
  // settings aside, and that must be an error rather than a silent pass.
  assert.throws(
    () => settingsAside(`<main>${palette}</main>`),
    (e: Error) => /field-settings panel cannot be located/.test(e.message) && /1 <aside> opening tag/.test(e.message),
    "an unlocatable settings aside must say what the page rendered, not quote a slice anchor",
  );
  assert.throws(
    () => settingsAside(`<main>${palette}<aside class="x"></aside></main>`),
    /field-settings panel cannot be located[\s\S]*2 <aside> opening tag/,
    "an unmarked, unstyled aside must not be silently accepted as the settings panel",
  );
  // Two markers is ambiguous — the guard must not judge one of two panels.
  assert.throws(
    () => settingsAside(`<main>${writings[0]}${writings[1]}</main>`),
    /marks 2 asides with data-jf-aside="settings"/,
  );
});

test("the settings panel renders no form-level control", () => {
  const ASIDE = settingsPanel();
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
  const ASIDE = settingsPanel();
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
