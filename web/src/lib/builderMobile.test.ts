// Regression guard for the mobile builder report (Wan):
//
//   "input form ni kan, rasa mcm tak user friendly utk mobile user. nak kena
//    klik kat satu input fields tu lepas tu kena tap settings kat atas tu."
//
// THE BUG (three facts about the old mobile flow, all measured on the source):
//
//   1. Tapping a block card called `setSelected(b.id)` and NOTHING visible
//      happened. Editing that block required pressing the *Settings* tab in the
//      chrome at the top — a second, disconnected act after the tap the user
//      had already made.
//   2. The Settings tab had NO way out. `setMobileTab("canvas")` appeared
//      exactly once in the whole file, inside `addBlock()`; the panel itself had
//      no back button. On a phone the only exit was the Canvas tab at the top
//      (or adding a block).
//   3. The block card's action row was four 44px buttons (↑ ↓ ⧉ ✕) — ~176px of
//      a ~350px card — and the ↑/↓ pair, the most frequent action in a builder,
//      was two of them.
//
// THE FIX: the edit panel is now the CANVAS column's mobile face (it replaces
// the canvas while `panelOpen`), opened by tapping a block card and closed by
// the panel's own "← Canvas" row; the tab bar is Add | Canvas; and Duplicate +
// Remove moved into one ⋯ menu (44px) while ↑/↓ stay as small icon buttons.
//
// WHY THE TEST IS SHAPED THIS WAY
//   This repo has no DOM/JSX test runner (no vitest/jest — see
//   src/lib/blockReorder.test.ts, src/lib/adminResponsive.test.ts), so the page
//   is asserted from source. That is the right granularity here: the flow IS
//   the wiring, and the defect was a handler that existed and did nothing
//   visible. Mutation evidence is in the PR: neutralising the card's
//   `openBlockPanel` call turns test (1) red.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { DICT } from "../components/i18nDict.ts";

const builder = readFileSync(
  fileURLToPath(new URL("../app/builder/page.tsx", import.meta.url)),
  "utf8",
);

/** The source from an anchor, far enough to cover the construct it opens. */
function from(anchor: string, chars = 1600): string {
  const at = builder.indexOf(anchor);
  assert.ok(at >= 0, `anchor not found in web/src/app/builder/page.tsx: ${anchor}`);
  return builder.slice(at, at + chars);
}

/** The region between two anchors (used to delimit one subtree). */
function between(start: string, end: string): string {
  const at = builder.indexOf(start);
  assert.ok(at >= 0, `start anchor not found: ${start}`);
  const stop = builder.indexOf(end, at);
  assert.ok(stop > at, `end anchor not found after ${start}: ${end}`);
  return builder.slice(at, stop);
}

/** Walk out from an attribute match to the enclosing <button>. */
function buttonFor(attr: string): string {
  const at = builder.indexOf(attr);
  assert.ok(at >= 0, `no button carrying ${attr} found in the builder`);
  const open = builder.lastIndexOf("<button", at);
  const close = builder.indexOf("</button>", at);
  assert.ok(open >= 0 && close > open, `could not delimit the button carrying ${attr}`);
  return builder.slice(open, close + "</button>".length);
}

// ── (1) tapping a block opens the edit panel ───────────────────────────────

test("(1) tapping a block card opens the edit panel for that block", () => {
  // The reported flow: the tap must be what opens the editor, not a tab.
  assert.match(
    builder,
    /onClick=\{\(\) => openBlockPanel\(b\.id\)\}/,
    "the block card no longer opens the edit panel on tap — tapping a field goes back to " +
      "selecting it silently, and the user has to find another control to edit it",
  );
  const opener = from("function openBlockPanel");
  assert.match(opener, /setSelected\(id\)/, "openBlockPanel no longer selects the tapped block");
  assert.match(
    opener,
    /setPanelOpen\(true\)/,
    "the card tap no longer opens the panel — the tap would select the block and show nothing",
  );
  assert.match(
    opener,
    /setMobileTab\("canvas"\)/,
    "openBlockPanel does not return to the canvas tab — tapping a block from the palette tab would " +
      "open a panel that its own column is hiding",
  );
});

test("(1b) adding a block keeps the canvas (the panel must NOT auto-open)", () => {
  // #59's behaviour, which was right: add several fields in a row, each one tap.
  const add = from("function addBlock");
  assert.match(add, /setSelected\(b\.id\)/, "a newly added block is no longer highlighted");
  assert.match(add, /setPanelOpen\(false\)/, "adding a block now forces the edit panel open");
  assert.match(add, /setMobileTab\("canvas"\)/, "adding from the Add tab no longer returns to the canvas");
});

test("(1c) removing the block being edited closes the panel", () => {
  // Otherwise the canvas column stays hidden behind a panel with nothing to edit.
  const remover = from("function removeBlock");
  assert.match(remover, /setSelected\(null\)/);
  assert.match(
    remover,
    /setPanelOpen\(false\)/,
    "removing the selected block leaves the panel open — the canvas cannot be reached again",
  );
});

// ── (2) the way out ────────────────────────────────────────────────────────

test("(2) the panel has an exit, and it restores the canvas with the block highlighted", () => {
  const header = from("{/* #652: the exit.");
  assert.match(
    header,
    /onClick=\{closeBlockPanel\}/,
    "the panel header lost its back button — the edit panel is a trap with no way out (the old " +
      "Settings tab had none either; this change must not reinstate that, worse)",
  );
  assert.match(header, /← \{t\("bld\.canvas"\)\}/, "the back control no longer names the canvas");
  assert.match(header, /md:hidden/, "the exit row is not scoped to mobile");

  const closer = from("function closeBlockPanel", 400);
  assert.match(closer, /setPanelOpen\(false\)/, "closeBlockPanel does not close the panel");
  assert.doesNotMatch(
    closer,
    /setSelected\(null\)/,
    "closing the panel cleared the selection — the block must stay highlighted so the user can see " +
      "where they were (and tap it to return)",
  );
});

test("(2b) the canvas and the panel are the same mobile slot, driven by panelVisible", () => {
  assert.match(
    builder,
    /\$\{mobileTab === "canvas" && !panelVisible \? "block" : "hidden"\} md:block/,
    "the canvas is no longer hidden while the edit panel is open — on a phone both would render",
  );
  assert.match(
    builder,
    /\$\{mobileTab === "canvas" && panelVisible \? "block" : "hidden"\} md:block/,
    "the edit panel is no longer the canvas column's mobile face (or desktop lost md:block)",
  );
  // The panel body still renders the EXISTING controls, not a second editor.
  assert.match(
    builder,
    /\{selectedBlock && renderFieldSettings\(selectedBlock\)\}/,
    "the panel no longer renders the existing per-field settings",
  );
});

test("(2d) the panel is DERIVED from the selection, not trusted from a flag", () => {
  // The defect this pins was reproduced in a browser: AI-generate (Add tab)
  // replaces the canvas and calls setSelected(null), but it left panelOpen true.
  // Tapping Canvas then showed a panel reading "Select a block to edit" with NO
  // exit row — that row requires a selected block — while the freshly generated
  // blocks stayed hidden. A trap with no way out, which is the exact thing this
  // change exists to remove. Deriving the flag closes it for every path, not
  // just the one that was found.
  assert.match(
    builder,
    /const panelVisible = panelOpen && !!selectedBlock;/,
    "panelVisible is no longer derived from `panelOpen && selectedBlock` — the panel can again outlive " +
      "the block it edits, which strands the canvas behind a panel with no ← Canvas row",
  );
  // Every place that decides what the mobile slot shows must use the derived
  // value: a mixed use is how the trap came back last time.
  const gated = [...builder.matchAll(/mobileTab === "canvas" && (!?)panel(\w+)/g)];
  assert.equal(gated.length, 2, `expected the canvas and the panel to be the two gated slots (found ${gated.length})`);
  for (const m of gated) {
    assert.equal(
      m[2],
      "Visible",
      `a mobile slot is still gated on the raw \`panel${m[2]}\` — use the derived panelVisible so a ` +
        "cleared selection cannot leave the panel stranded",
    );
  }
  // The exit row must be gated the same way as the panel that contains it.
  assert.match(
    builder,
    /\{panelVisible && selectedBlock && \(/,
    "the panel's exit row is no longer gated on the derived panelVisible",
  );
});

test("(2c) the panel stays hidden when no block is selected", () => {
  // The header row requires BOTH an open panel and a selected block. Note this
  // is now the DERIVED `panelVisible`, not the raw flag: the raw-flag version
  // was not enough — see (2d), where a cleared selection stranded the panel.
  assert.match(
    builder,
    /\{panelVisible && selectedBlock && \(/,
    "the panel header row no longer requires both an open panel and a selected block",
  );
  // `panelOpen` is opened only by a card tap and cleared by the exit, a removal
  // or an add.
  const opens = [...builder.matchAll(/setPanelOpen\(true\)/g)].length;
  assert.equal(opens, 1, `expected exactly one place to open the panel (found ${opens})`);
  assert.match(from("function openBlockPanel", 400), /setPanelOpen\(true\)/);
});

// ── (3) two tabs, not three ────────────────────────────────────────────────

test("(3) MobileTab is exactly \"add\" | \"canvas\" — \"settings\" is not a tab any more", () => {
  const m = builder.match(/type MobileTab = ([^;]+);/);
  assert.ok(m, "the builder no longer declares MobileTab");
  const members = m[1]
    .split("|")
    .map((s) => s.trim().replace(/"/g, ""))
    .filter(Boolean);
  assert.deepEqual(
    members,
    ["add", "canvas"],
    `MobileTab accepts ${members.join(" | ")} — the removed Settings tab is back, and with it a ` +
      `second route into the edit panel`,
  );
  assert.doesNotMatch(
    builder,
    /setMobileTab\("settings"\)/,
    "something still switches the tab bar to \"settings\"",
  );
  assert.match(
    builder,
    /\{\(\["add", "canvas"\] as MobileTab\[\]\)\.map\(/,
    "the tab bar no longer renders exactly Add | Canvas",
  );
  assert.doesNotMatch(
    builder,
    /t\("bld\.settings"\)/,
    "the removed Settings tab's label is still rendered in the builder",
  );
});

// ── (4) one ⋯ menu replaces ⧉ and ✕ ───────────────────────────────────────

test("(4) the ⋯ menu holds Duplicate and Remove, and both still reach their handler", () => {
  const menuButton = buttonFor('aria-label={t("bld.blockMenu")}');
  assert.match(menuButton, /aria-haspopup="true"/, "the ⋯ button does not announce that it opens a menu");
  assert.match(
    menuButton,
    /aria-expanded=\{menuOpen === b\.id\}/,
    "the ⋯ button does not report whether its menu is open",
  );
  assert.match(menuButton, /e\.stopPropagation\(\)/, "the ⋯ button does not stop the tap reaching the card");
  // The aria-label is the button's ONLY accessible name (its visible content is
  // the ⋯ glyph), so it must be a real translated string, not a raw key.
  assert.match(
    menuButton,
    /aria-label=\{t\("bld\.blockMenu"\)\}/,
    "the ⋯ button's accessible name is no longer the translated label",
  );

  const item = (label: string) => buttonFor(`{t("${label}")}`);
  const duplicate = item("bld.duplicate");
  const remove = item("bld.removeBlock");
  assert.match(duplicate, /duplicateBlock\(b\.id\)/, "the menu's Duplicate item no longer duplicates");
  assert.match(remove, /removeBlock\(b\.id\)/, "the menu's Remove item no longer removes");
  for (const [name, src] of [["Duplicate", duplicate], ["Remove", remove]] as const) {
    assert.match(
      src,
      /e\.stopPropagation\(\)/,
      `the ${name} menu item does not stop propagation — the tap would ALSO open the edit panel`,
    );
    assert.match(src, /setMenuOpen\(null\)/, `the ${name} menu item leaves its own menu open`);
  }

  // The old standalone buttons must be gone from the card (one shape, not two).
  assert.doesNotMatch(
    builder,
    /title=\{t\("bld\.duplicate"\)\}/,
    "a standalone Duplicate button is back beside the ⋯ menu — the card has two shapes for one action",
  );
  assert.doesNotMatch(
    builder,
    /title=\{t\("bld\.removeBlock"\)\}/,
    "a standalone Remove button is back beside the ⋯ menu",
  );
});

test("(4b) Escape closes the open menu and returns focus to its ⋯ button", () => {
  const effectSrc = from('if (menuOpen === null) return;');
  assert.match(effectSrc, /e\.key !== "Escape"/, "the actions menu no longer closes on Escape");
  assert.match(
    effectSrc,
    /menuBtn\.current\?\.focus\(\)/,
    "closing with Escape drops focus on the body instead of the button that opened the menu",
  );
  assert.match(
    effectSrc,
    /closest\("\[data-block-menu\]"\)/,
    "a pointer press outside the menu no longer closes it",
  );
  assert.match(from('data-block-menu className="relative"').slice(0, 200), /^data-block-menu/, "the menu region lost its marker");
});

test("(4c) every control in the card's action row stops propagation", () => {
  // A tap on ↑/↓/⋯/menu-item must never open the edit panel.
  const row = between('<span className="flex items-center gap-1">', "<BlockPreview");
  const clicks = [...row.matchAll(/onClick=\{\(e\) => \{/g)].length;
  const stopped = [...row.matchAll(/onClick=\{\(e\) => \{\s*e\.stopPropagation\(\);/g)].length;
  assert.ok(clicks >= 4, `expected the card's action buttons (found ${clicks} handlers)`);
  assert.equal(
    stopped,
    clicks,
    `${clicks - stopped} control(s) in the card action row do not stop propagation — that tap would ` +
      `open the edit panel instead of running the action`,
  );
});

// ── (4d) reorder stays directly reachable ─────────────────────────────────

test("(4d) ↑/↓ stay as their own buttons and keep the reorder wiring", () => {
  // blockReorder.test.ts owns the maths (moveBlock) and the moveRow chain; this
  // pins the shape this change touched: the arrows are still buttons of their
  // own (not hidden behind the menu) and they are 32px icons on a phone.
  const up = buttonFor('title={t("bld.moveUp")}');
  const down = buttonFor('title={t("bld.moveDown")}');
  assert.match(up, /moveRow\(i,\s*-1\)/, "the move-up button no longer reorders by -1");
  assert.match(down, /moveRow\(i,\s*1\)/, "the move-down button no longer reorders by +1");
  assert.match(up, /disabled=\{!canMove\(blocks,\s*i,\s*-1\)\}/, "move-up is no longer disabled on the first row");
  assert.match(down, /disabled=\{!canMove\(blocks,\s*i,\s*1\)\}/, "move-down is no longer disabled on the last row");
  for (const [name, src] of [["up", up], ["down", down]] as const) {
    assert.match(src, /h-8 w-8/, `the move-${name} arrow is not the 32px mobile icon button`);
    assert.match(
      src,
      /md:h-auto md:w-auto/,
      `the move-${name} arrow lost its desktop override — the desktop row changes size`,
    );
    assert.match(src, /disabled:opacity-30/, `the disabled move-${name} arrow has no visible disabled styling`);
  }
});

test("(4e) the ⋯ button meets the 44px tap target", () => {
  const menuButton = buttonFor('aria-label={t("bld.blockMenu")}');
  assert.match(
    menuButton,
    /h-11 w-11/,
    "the block actions button is under 44x44 on a phone (the four buttons it replaces were 44x44)",
  );
  assert.match(from("{/* #652: the exit."), /min-h-11/, "the panel's back button is under 44px tall");
  const items = between('<div className="absolute right-0 top-full', "</div>");
  assert.match(items, /min-h-11/, "a menu item is under 44px tall");
});

// ── (5) the preview stops promising controls ──────────────────────────────

test("(5) BlockPreview draws no faux checkbox or radio that looks tappable", () => {
  // Measured before: a 16px bordered square / bordered ring — the two shapes a
  // person reads as "tap me", in a preview where nothing is interactive.
  assert.doesNotMatch(
    builder,
    /inline-block h-4 w-4 rounded border border-slate-300/,
    "the faux checkbox is back to a bordered square — it reads as a tappable control",
  );
  assert.doesNotMatch(
    builder,
    /inline-block h-4 w-4 rounded-full border border-slate-300/,
    "the faux radio is back to a bordered ring — it reads as a tappable control",
  );
  const fakeBoxes = [...builder.matchAll(/inline-block h-4 w-4 rounded(?:-full)? bg-slate-300/g)];
  assert.ok(
    fakeBoxes.length >= 3,
    `expected the flat placeholder boxes (checkbox, consent, radio) — found ${fakeBoxes.length}`,
  );
  const hidden = [...builder.matchAll(/aria-hidden className="inline-block h-4 w-4 rounded(?:-full)? bg-slate-300/g)];
  assert.equal(
    hidden.length,
    fakeBoxes.length,
    "a decorative placeholder box is not marked aria-hidden — a screen reader would announce a fake control",
  );
});

test("(5b) the image placeholder is a caption, not a field-shaped box", () => {
  // Line-anchored: the string also appears inside the explanatory comment that
  // replaced it, so a file-wide match would fail on the fix's own rationale.
  assert.doesNotMatch(
    builder,
    /^\s*Image URL →\s*$/m,
    'the preview still renders the literal "Image URL →" — an arrow that promised a field or a link',
  );
  const image = builder.slice(builder.indexOf('case "image":'));
  const placeholder = image.slice(0, image.indexOf('case "product":'));
  assert.match(
    placeholder,
    /\{t\("bld\.imageUrl"\)\}/,
    "the image placeholder no longer names the setting it is telling the merchant to use",
  );
  assert.doesNotMatch(
    placeholder,
    /border-dashed border-slate-300/,
    "the image placeholder is a dashed box again — the shape reads as an empty input/slot",
  );
});

// ── i18n ──────────────────────────────────────────────────────────────────

test("(6) the new menu label exists in all five locales", () => {
  const entry = DICT["bld.blockMenu"] as Record<string, string> | undefined;
  assert.ok(entry, "bld.blockMenu is missing from the dictionary");
  for (const lang of ["en", "ms", "zh", "ta", "ar"]) {
    assert.ok(
      typeof entry[lang] === "string" && entry[lang].trim() !== "",
      `bld.blockMenu has no ${lang} value`,
    );
  }
});

// ── docs parity (AGENTS.md: DOCUMENTATION PARITY) ─────────────────────────

test("(7) the builder guide exists, is in the docs nav, and is linked from the builder", () => {
  // The guide describes the flow this change created. The inline "?" (rule 4)
  // is what makes it reachable from the feature itself.
  const guide = readFileSync(
    fileURLToPath(new URL("../app/docs/builder/page.tsx", import.meta.url)),
    "utf8",
  );
  const nav = readFileSync(fileURLToPath(new URL("../app/docs/layout.tsx", import.meta.url)), "utf8");
  assert.match(nav, /href: "\/docs\/builder"/, "the builder guide is not in the docs sidebar");
  assert.match(
    builder,
    /href="\/docs\/builder"/,
    'the builder has no inline "?" doc link to the builder guide (AGENTS.md DOCUMENTATION PARITY rule 4)',
  );
  // The flow it must state, because the old flow is what was reported as broken.
  assert.match(guide, /<strong>← Canvas<\/strong>/, "the guide does not name the panel's exit control");
  assert.match(guide, /no separate settings tab/i, "the guide does not state that the settings tab is gone");
});
