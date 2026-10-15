// Regression guard for the ARIA tabs pattern's index math (card t_40253ae6).
//
// THE DEFECT. Every one of the app's five tab strips carried `role="tablist"` +
// `role="tab"` + `aria-selected` and no `aria-controls`/`role="tabpanel"` (grep
// over src/: 0 hits), and not one of them had any keydown handling at all — so a
// keyboard user could not move across a strip with the arrow keys, and a screen
// reader was told nothing about which panel a tab controls.
//
// WHY THIS FILE IS THE PURE HALF. The DOM-facing half (`tablistKeyDown`) is
// covered in a real browser by `e2e/a11y-profile-tabs.spec.ts` and
// `e2e/a11y-settings-first-run-tab.spec.ts`: "press ArrowRight and the selection
// moves AND the panel follows" is a property of the rendered page, and this repo
// has no DOM unit runner (see src/lib/buttonWrap.test.ts) — a jsdom copy of the
// strip would prove the test's own mock. What is worth pinning here is the part
// that is pure arithmetic and that a browser test can only sample: every key,
// both wrap edges, an empty/one-tab strip, and the RTL reversal.
//
// EACH ASSERTION IS A RULE THE WIDGET WOULD BE WRONG WITHOUT:
//   1. The id pair cannot DRIFT. `aria-controls` on the tab and `id` on the
//      panel are one fact written twice (so are `aria-labelledby` and the tab's
//      `id`), and a drifted pair is SILENT — axe's `aria-valid-attr-value`
//      exempts `aria-controls` on a tab whose `aria-selected` is "false", so
//      nothing reports it and only a screen reader notices. The pair is composed
//      in one place, and this test pins that place.
//   2. Roving tabindex: exactly the selected tab is a tab stop, and a strip with
//      no selection has none — the alternative (every tab tabbable) makes Tab
//      walk the strip one tab at a time, which is what the arrow keys replace.
//   3. Arrow keys move by one with WRAPAROUND at both ends, Home/End go to the
//      ends, and the four keys are the ONLY ones owned — a key this widget does
//      not own must come back `null` so the caller leaves the event alone (a
//      swallowed Tab would trap the keyboard in the strip).
//   4. RTL: in a `dir="rtl"` strip the first tab is laid out RIGHTMOST, so Right
//      Arrow goes to the PREVIOUS tab (movement follows the reading order, per
//      the APG's keyboard-interface guidance). Home/End stay DOM order, because
//      APG calls them "the first/last tab" and not "the left/right one".
//   5. A strip of 0 or 1 tabs cannot produce an out-of-range index. The
//      read_only settings state renders NO strip, and a strip can shrink when a
//      role changes under it; `(current + dir + count) % count` with count 0 is
//      NaN, and a NaN index would be `tabs[NaN].focus()` — a TypeError in the
//      middle of a keydown.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";

import { nextTabStop, panelDomId, tabDomId, tabStop } from "./tablist.ts";

// The five strips' prefixes (profile, settings, products, media, forms). Each is
// the namespace the ids above are composed in, and the pages are asserted to use
// them in their own source by nothing here — the browser specs read the ids off
// the live DOM, which is where a wrong prefix would show.
const PREFIXES = ["prof", "set", "prod", "media", "forms"] as const;
const IDS = ["active", "trash", "mcp"] as const;

test("the tab/panel id pair cannot drift, and it is distinct per strip and per tab", () => {
  const seen = new Set<string>();
  for (const p of PREFIXES) {
    for (const id of IDS) {
      const tab = tabDomId(p, id);
      const panel = panelDomId(p, id);
      assert.notEqual(
        tab,
        panel,
        `${p}/${id}: a tab and its panel must not share an id — one element cannot be both, ` +
          `and a duplicate id makes every idref ambiguous`,
      );
      // The panel's `aria-labelledby` names the TAB id and the tab's
      // `aria-controls` names the PANEL id, so both have to resolve to the tab
      // they belong to: same prefix, same tab id, no cross-strip bleed.
      assert.ok(tab.startsWith(`${p}-tab-`), `${p}/${id}: the tab id is ${tab}`);
      assert.ok(panel.startsWith(`${p}-panel-`), `${p}/${id}: the panel id is ${panel}`);
      assert.ok(
        tab.endsWith(`-${id}`) && panel.endsWith(`-${id}`),
        `${p}/${id}: the pair must carry the tab's own id, or two tabs of one strip share a panel`,
      );
      assert.ok(!seen.has(tab) && !seen.has(panel), `${p}/${id}: duplicate id ${tab} / ${panel}`);
      seen.add(tab);
      seen.add(panel);
    }
  }
});

test("roving tabindex: only the selected tab is a tab stop", () => {
  assert.equal(tabStop("identity", "identity"), 0, "the selected tab must be the strip's tab stop");
  assert.equal(tabStop("security", "identity"), -1, "an unselected tab must be out of the tab sequence");
  assert.equal(
    tabStop("chip", null),
    -1,
    "a strip with no selection (the read_only settings state) must not put a tab in the tab order",
  );
});

test("arrow keys move by one with wraparound; Home/End go to the ends", () => {
  const n = 5;
  // Right from the middle, and the wrap off the last tab (APG: "If focus is on
  // the last tab element, moves focus to the first tab").
  assert.equal(nextTabStop(n, 0, "ArrowRight"), 1);
  assert.equal(nextTabStop(n, 3, "ArrowRight"), 4);
  assert.equal(nextTabStop(n, 4, "ArrowRight"), 0, "ArrowRight off the last tab must wrap to the first");
  // Left, and the wrap off the first tab.
  assert.equal(nextTabStop(n, 3, "ArrowLeft"), 2);
  assert.equal(nextTabStop(n, 0, "ArrowLeft"), n - 1, "ArrowLeft off the first tab must wrap to the last");
  // Home/End, from anywhere.
  assert.equal(nextTabStop(n, 2, "Home"), 0);
  assert.equal(nextTabStop(n, 2, "End"), n - 1);
  assert.equal(nextTabStop(n, 0, "End"), n - 1);
  assert.equal(nextTabStop(n, n - 1, "Home"), 0);
});

test("the widget owns Left/Right/Home/End and NOTHING else", () => {
  // A key the caller must keep: Tab/Shift+Tab leave the strip, typing goes to a
  // search field, Down/Up keep their normal scrolling function on a horizontal
  // tablist (APG note 5). Each must come back null so the event is not consumed.
  for (const key of ["Tab", "Enter", " ", "ArrowUp", "ArrowDown", "Escape", "a", "PageDown"]) {
    assert.equal(
      nextTabStop(5, 2, key),
      null,
      `${key} is not a key this tablist owns — returning an index would swallow it`,
    );
  }
});

test("RTL: Right Arrow moves to the tab the reader sees on the right (the previous one)", () => {
  const n = 5;
  assert.equal(nextTabStop(n, 2, "ArrowRight", true), 1, "in dir=rtl, Right must go to the previous tab");
  assert.equal(nextTabStop(n, 0, "ArrowRight", true), n - 1, "…and wrap from the leftmost to the rightmost");
  assert.equal(nextTabStop(n, 2, "ArrowLeft", true), 3, "…and Left must go to the next tab");
  assert.equal(nextTabStop(n, n - 1, "ArrowLeft", true), 0);
  // Home/End are DOM order in both directions: APG's words are "the first/last
  // tab", not "the left/right one".
  assert.equal(nextTabStop(n, 2, "Home", true), 0);
  assert.equal(nextTabStop(n, 2, "End", true), n - 1);
});

test("an empty or single-tab strip can never produce an out-of-range index", () => {
  // count 0: every owned key is refused (the strip is not rendered at all in the
  // read_only settings state, so this is a real state, not a hypothetical).
  for (const key of ["ArrowRight", "ArrowLeft", "Home", "End"]) {
    assert.equal(nextTabStop(0, -1, key), null, `count 0 must refuse ${key} rather than compute NaN`);
    assert.equal(nextTabStop(0, 0, key), null);
  }
  // count 1: every move stays on the one tab.
  for (const key of ["ArrowRight", "ArrowLeft", "Home", "End"]) {
    assert.equal(nextTabStop(1, 0, key), 0, `one tab: ${key} must stay on index 0`);
  }
  // No selection in a rendered strip (defensive: the contract is exactly one
  // aria-selected, and a strip that briefly has none must still land on a tab).
  assert.equal(nextTabStop(3, -1, "ArrowRight"), 0);
  assert.equal(nextTabStop(3, -1, "ArrowLeft"), 2, "…from the other side for the reverse key");
  assert.equal(nextTabStop(3, -1, "Home"), 0);
  assert.equal(nextTabStop(3, -1, "End"), 2);
});
