// Guard: three dashboard affordances that read WRONG to a person even though
// every existing test was green (card t_bb0d0bee, from a UX sweep at 390px).
//
// WHY A SOURCE-READING TEST IN THIS REPO. There is no JSX/component test setup
// (`npm run test:unit` is `node --test` over `src/lib/*.test.ts`), and the render
// itself is checked the only way it can be — by driving the built app in a
// browser and reading the DOM back (that evidence lives on the card). What this
// file pins is the SOURCE SHAPE each fix depends on, because each defect is one
// line away from coming back and none of the existing guards look at it:
//
//   1. /forms — the status pill ("Draft") sat in the SAME flex row as "Edit" and
//      "Form Settings", so it read as a third button. Status is information; it
//      belongs beside the form's name. This test fails if the pill moves back
//      into the action row.
//   2. /products — the page shipped TWO entry points to one task: an always-open
//      inline create form AND a "New product" link to /products/new above it.
//      On a 390x844 phone the always-open form put the first catalogue row at
//      y=781, below the fold. One entry point now: the header button REVEALS the
//      panel (and the panel is where the full-editor route is offered). This test
//      fails if the panel is ever rendered unconditionally again, or if a second
//      always-visible creation control comes back.
//   3. /builder — the "Explore form templates" modal sat over a shell whose
//      "Save & publish" was still a live, saturated control. The picker is a real
//      modal now: it renders OUTSIDE the shell, the shell carries `inert` while
//      it is open, the scrim is no longer the 40% wash, and the blank path has
//      its own CTA instead of being the only card without one.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const formsPage = read("../app/forms/page.tsx");
const productsPage = read("../app/products/page.tsx");
const builderPage = read("../app/builder/page.tsx");
const picker = read("../components/TemplatePicker.tsx");
const dict = read("../components/i18nDict.ts");

/** The `/forms` row template, split at the action row. Both halves are the
 *  thing under test, so the split itself is asserted non-vacuously. */
function formsRowHalves(): { titleBlock: string; actionRow: string } {
  const start = formsPage.indexOf("{forms.map((f, rowIndex) => {");
  assert.notEqual(start, -1, "the /forms row renderer moved — this guard is blind");
  const end = formsPage.indexOf("</li>", start);
  assert.notEqual(end, -1, "the /forms row never closes");
  const row = formsPage.slice(start, end);
  const actionAt = row.indexOf("sm:justify-end");
  assert.notEqual(actionAt, -1, "the action-row anchor (sm:justify-end) is gone from the /forms row");
  return { titleBlock: row.slice(0, actionAt), actionRow: row.slice(actionAt) };
}

test("/forms: the status pill is beside the form name, not in the action row", () => {
  const { titleBlock, actionRow } = formsRowHalves();

  // The three status labels are what the pill renders. They must ALL be in the
  // title block: a pill split across the two halves would render twice.
  for (const key of ["forms.live", "forms.unlisted", "forms.private", "forms.draft"]) {
    assert.ok(
      titleBlock.includes(`t("${key}")`),
      `the status pill's ${key} label must render beside the form title`,
    );
  }
  assert.doesNotMatch(
    actionRow,
    /t\("forms\.(live|unlisted|private|draft)"\)/,
    "the action row is actions only — a status label in it reads as a third button",
  );
  // Non-vacuity: the action row still holds the actions it is named for.
  assert.match(actionRow, /t\("forms\.edit"\)/, "the action row lost its Edit action");
  assert.match(actionRow, /t\("fs\.title"\)/, "the action row lost Form Settings");
});

test("/products: exactly one creation entry point, and it REVEALS the panel", () => {
  // There is one creation control on the page: it renders "New product" when the
  // panel is closed and "Cancel" when it is open (same button, one state).
  const triggers = productsPage.match(/\{createOpen \? t\("common\.cancel"\) : t\("prod\.newTitle"\)\}/g) ?? [];
  assert.equal(triggers.length, 1, "the page must have exactly one New product control");
  assert.equal(
    (productsPage.match(/t\("prod\.newTitle"\)/g) ?? []).length,
    1,
    "a second always-visible creation entry point is back",
  );

  // It is a BUTTON that toggles the reveal (not a Link to a second surface),
  // and it is announced as a disclosure.
  const at = productsPage.indexOf('{createOpen ? t("common.cancel")');
  const buttonOpen = productsPage.lastIndexOf("<button", at);
  assert.notEqual(buttonOpen, -1, "the New product control is not a <button>");
  const button = productsPage.slice(buttonOpen, at);
  assert.match(button, /setCreateOpen\(\(v\) => !v\)/, "the button must toggle the create panel");
  assert.match(button, /aria-expanded=\{createOpen\}/, "the button must announce its expanded state");
  assert.match(button, /aria-controls="prod-create"/, "the button must name the panel it controls");

  // The panel is COLLAPSED by default and only rendered when revealed.
  assert.match(
    productsPage,
    /const \[createOpen, setCreateOpen\] = useState\(false\)/,
    "the create panel must start collapsed",
  );
  assert.match(
    productsPage,
    /\{canWrite && createOpen && \(\s*\n\s*<form id="prod-create"/,
    "the creation form must be gated on the reveal, not always rendered",
  );
  assert.match(productsPage, /<form id="prod-create"/, "the panel's id must match aria-controls");

  // The full editor (/products/new) stays reachable — card t_7eb3b623's guard —
  // but as a link INSIDE the one panel, not as a second header control.
  const links = productsPage.match(/href="\/products\/new"/g) ?? [];
  assert.equal(links.length, 1, "/products/new must be linked exactly once from the page");
  const linkAt = productsPage.indexOf('href="/products/new"');
  const formAt = productsPage.indexOf('<form id="prod-create"');
  const formEnd = productsPage.indexOf("</form>", formAt);
  assert.ok(
    linkAt > formAt && linkAt < formEnd,
    "/products/new belongs inside the quick-create panel, not in the page header",
  );

  // And the empty state points at that button instead of at the panel.
  assert.match(
    dict,
    /"prod\.empty": \{ en: "[^"]*New product[^"]*"/,
    "the empty state must point at the New product button it can actually see",
  );
});

test("/builder: the template picker is a real modal over an inert shell", () => {
  // The picker renders OUTSIDE the <main> it covers, so `inert` can be put on
  // the whole shell without disabling the picker itself.
  const pickerCall = builderPage.indexOf("<TemplatePicker");
  const mainOpen = builderPage.indexOf("<main");
  const mainClose = builderPage.indexOf("</main>");
  assert.notEqual(pickerCall, -1, "the builder no longer renders TemplatePicker");
  assert.ok(mainOpen > -1 && mainClose > mainOpen, "the builder shell (<main>) is gone");
  assert.ok(
    pickerCall < mainOpen,
    "the picker must not be rendered inside the shell it makes inert",
  );

  // The shell is inert exactly while the picker is open: "Save & publish" behind
  // the modal is not clickable, not focusable and not a competing first step.
  const shell = builderPage.slice(mainOpen, mainClose);
  assert.match(shell, /inert=\{pickerOpen\}/, "the builder shell must be inert while the picker is open");
  assert.equal(
    (builderPage.match(/inert=/g) ?? []).length,
    1,
    "only the covered shell carries inert — the picker must stay interactive",
  );

  // The scrim is no longer the 40% wash that left a saturated Publish button
  // reading as active (measured 390px: 56,63,126 under the 40% black wash). The
  // assertion is on the class attribute, not on the file, so the comment above
  // cannot satisfy it for the code.
  const scrim = picker.match(/className="(fixed inset-0 z-50[^"]*)"/);
  assert.ok(scrim, "the picker backdrop class is gone");
  assert.match(scrim![1], /bg-slate-950\/65/, "the picker backdrop must be an unambiguous scrim");

  // The blank path is as actionable as a template card: it has its own filled
  // CTA, and the card is no longer a <button> that would wrap it in another.
  assert.match(picker, /t\("tpl\.blankCta"\)/, "the blank card needs its own call to action");
  const blankStart = picker.indexOf("{/* Blank form");
  const blankEnd = picker.indexOf("{/* Issue #457", blankStart);
  assert.notEqual(blankStart, -1, "the blank-form card moved");
  assert.ok(blankEnd > blankStart, "the blank-form card has no end anchor");
  const blankCard = picker.slice(blankStart, blankEnd);
  assert.equal(
    (blankCard.match(/<button/g) ?? []).length,
    1,
    "the blank card must contain exactly one button (its CTA), not a button wrapping a button",
  );
  assert.match(blankCard, /jf-btn-primary/, "the blank CTA must be a filled button like Use template");
  assert.match(blankCard, /router\.push\("\/builder\?new=1"\)/, "the blank CTA must open a blank form");
});

test("the three new/edited dictionary strings exist in all five locales", () => {
  for (const key of ["prod.fullEditor", "tpl.blankCta"]) {
    const line = dict.split("\n").find((l) => l.trimStart().startsWith(`"${key}":`));
    assert.ok(line, `${key} is missing from the dictionary`);
    for (const lang of ["en", "ms", "zh", "ta", "ar"]) {
      assert.match(line!, new RegExp(`\\b${lang}: "`), `${key} is missing its ${lang} locale`);
    }
  }
});
