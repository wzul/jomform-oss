// Regression guard for "the landing header on a phone" (card t_2b2da6a7) — five
// controls on a row that was designed for one, wrapped into two on a 390px
// screen.
//
// THE DEFECT (measured on the SHIPPED page at a real 390px viewport —
// Playwright, isMobile, hasTouch, dpr 2, 390x844, fresh browser state, lang=en):
//
//   header height 111px           (desktop: 71px — 13% of the first screen,
//                                  because the row had WRAPPED into two)
//   Docs            x=20   34x20  font 14px   \
//   Log in          x=70   41x20  font 14px    |  five controls, three different
//   Build a form    x=126 113x36  (filled)     |  baselines (y=65, 57, 53) and
//   EN              x=256  44x44  (segmented)  |  two 20px-tall tap targets
//   BM              x=300  44x44  (touching EN, gap 0px)
//
//   only 25px of slack in a 350px content row. The row is the DESKTOP row: the
//   header never had a phone layout, so the card that added /docs and made
//   "Build a form" the filled primary (t_53bcd451 — that hierarchy is correct
//   and is NOT reverted here) spent the last of its room.
//
// THE FIX, and why each half is shaped the way it is:
//   * below `sm` the bar is [JF + wordmark] … [Build a form] [menu button]. The
//     two are the ONLY controls in the row and both are 44px (`max-sm:min-h-11`
//     on the anchor, `min-h-11 min-w-11` on the button), so the row cannot wrap
//     and the header measures 69px at 390px.
//   * Docs and Log in are `hidden … sm:inline`: the same two anchors, not
//     copies, so the header keeps ONE filled primary at every width and the
//     desktop row is the row it was.
//   * the menu button discloses a sheet that is ALWAYS in the DOM and carries
//     Docs · Log in · the two languages, each a `min-h-11` row. The language
//     switch is how a BM reader gets a BM site, so it is in the sheet and not
//     behind a settings page.
//   * the wordmark drops below 21em: mark + wordmark + CTA + menu button need
//     327px of row at 100% text, so 320–336px is the band where the wordmark is
//     what would force a second row (the card allows the mark alone there).
//
// WHY A SOURCE-CONTRACT TEST AND NOT A DOM TEST: this repo has no DOM/JSX unit
// runner (see src/lib/docsApiProseWrap.test.ts). The GEOMETRY is guarded by
// `e2e/landing-header-mobile.spec.ts`, which measures header height, hit areas
// and the sheet in a real browser; what a unit test can pin is the SHAPE that
// carries the behaviour in the suite every web change runs. The two are
// deliberately different questions: this file fails when the mobile branch is
// deleted or re-merged away, the browser spec fails when the branch is present
// but does not measure up.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const LANDING = "../app/landing/page.tsx";
const landing = readFileSync(fileURLToPath(new URL(LANDING, import.meta.url)), "utf8");

/** The primary FILL (`--jf-primary`, white label). Not a text colour. */
const FILL = /bg-\[var\(--jf-primary\)\]/;
/** 44px in Tailwind's scale: `min-h-11` / `min-w-11` are 2.75rem. */
const HIT_44 = /\bmin-h-11\b/;
/** The phone-width 44px minimum — scoped, so the desktop CTA does not grow. */
const HIT_44_PHONE = /\bmax-sm:min-h-11\b/;

const NAV_BLOCK = landing.match(/<nav\b[^>]*>([\s\S]*?)<\/nav>/);
const nav = NAV_BLOCK?.[1] ?? "";

function classOf(tag: string) {
  return tag.match(/className="([^"]*)"/)?.[1] ?? "";
}

/** Every anchor in the nav with its href, classes and body. */
function navAnchors(src: string) {
  return [...src.matchAll(/<a\b([^>]*)>([\s\S]*?)<\/a>/g)].map((m) => ({
    href: (m[1].match(/href="([^"]*)"/) ?? [])[1] ?? "",
    cls: classOf(m[0]),
    body: m[2],
    source: m[0],
  }));
}

/** The sheet, from the div that carries its id to the end of the nav. */
const SHEET_ID = 'id="landing-header-menu"';
const SHEET_AT = nav.indexOf(SHEET_ID);
const sheet = SHEET_AT === -1 ? "" : nav.slice(nav.lastIndexOf("<div", SHEET_AT));

/** The menu button: the one whose aria-controls owns the sheet. */
const MENU_TAG = nav.match(/<button\b([^>]*aria-controls="landing-header-menu"[^>]*)>/)?.[1] ?? "";

test("the guard is looking at the real header (it cannot pass vacuously)", () => {
  assert.ok(NAV_BLOCK, `no nav block could be read from ${LANDING} — every assertion below is anchored on it`);
  assert.ok(
    navAnchors(nav).length >= 4,
    "the nav no longer holds the Docs / Log in / Build a form anchors plus the sheet's rows",
  );
  assert.ok(
    SHEET_AT !== -1,
    `${LANDING}: the phone sheet (${SHEET_ID}) is gone — the mobile branch this card added is what every assertion below pins`,
  );
  assert.ok(MENU_TAG, `${LANDING}: no <button aria-controls="landing-header-menu"> — the sheet has no opener`);
});

test("the header has exactly ONE filled primary, and it is still the build path", () => {
  const filled = navAnchors(nav).filter((a) => FILL.test(a.cls));
  assert.equal(
    filled.length,
    1,
    `the header renders ${filled.length} filled anchors (${JSON.stringify(filled.map((f) => f.href))}) — the phone sheet must not introduce a second filled control: one primary per header at EVERY width, and it is the build path (card t_53bcd451's hierarchy, kept by t_2b2da6a7)`,
  );
  assert.equal(filled[0].href, "/builder", `the header's filled primary points at ${filled[0].href}, not /builder`);
  assert.match(
    filled[0].cls,
    HIT_44_PHONE,
    `the filled /builder anchor has className "${filled[0].cls}" without max-sm:min-h-11 — it measured 113x36 on the shipped page's 390px row, and the phone target is 44px`,
  );
  // The desktop row must not gain the phone sizing: `max-sm:` keeps every
  // override inside the below-`sm` band, so the 100% desktop anchor is unchanged
  // (measured before and after: 113x36 at 1280px and 1440px).
  assert.ok(
    !filled[0].cls.split(/\s+/).includes("min-h-11"),
    `the /builder anchor carries an UNCONDITIONAL min-h-11 ("${filled[0].cls}") — that grows the desktop CTA from 36px to 44px and the desktop header with it; the sizing belongs to max-sm:`,
  );
});

test("below sm the two desktop text links leave the bar, and they stay the same two anchors", () => {
  for (const href of ["/docs", "/login"]) {
    const anchors = navAnchors(nav).filter((a) => a.href === href);
    assert.equal(
      anchors.length,
      2,
      `the nav carries ${anchors.length} anchors to ${href} — expected two (the desktop row's and the sheet's). The sheet is how a phone reaches it now, so deleting one half is the same defect in a new shape`,
    );
    const desktop = anchors[0];
    assert.match(
      desktop.cls,
      /(^|\s)hidden(\s|$)/,
      `the desktop ${href} anchor has className "${desktop.cls}" without "hidden" — a 20px text link back in the phone row is exactly what wrapped the bar to 111px`,
    );
    assert.match(
      desktop.cls,
      /(^|\s)sm:inline(\s|$)/,
      `the desktop ${href} anchor has className "${desktop.cls}" without sm:inline — it would never come back at 640px`,
    );
    assert.doesNotMatch(desktop.cls, FILL, `the desktop ${href} anchor is filled — it is a text link (card t_53bcd451)`);
    // …and the sheet's copy is a 44px row, not a 20px link.
    const row = anchors[1];
    assert.match(
      row.cls,
      HIT_44,
      `the sheet's ${href} row has className "${row.cls}" with no min-h-11 — every row in the sheet is a 44px target`,
    );
    assert.doesNotMatch(row.cls, FILL, `the sheet's ${href} row is filled — the sheet must not add a second primary`);
  }
});

test("the phone row is the mark, the CTA and a 44px menu button", () => {
  const cls = classOf(MENU_TAG);
  assert.match(cls, HIT_44, `the menu button has className "${cls}" with no min-h-11 — it is one of the two controls on the phone row and needs its own 44px target`);
  assert.match(cls, /\bmin-w-11\b/, `the menu button has className "${cls}" with no min-w-11 — a 44px TALL but narrow target is still a miss`);
  assert.match(
    cls,
    /(^|\s)sm:hidden(\s|$)/,
    `the menu button has className "${cls}" without sm:hidden — the desktop row already has Docs · Log in · EN|BM, so the button must leave at 640px`,
  );
  assert.match(
    MENU_TAG,
    /aria-expanded=\{menuOpen\}/,
    `the menu button does not expose its state (${MENU_TAG}) — a disclosure with no aria-expanded is a mystery button to a screen reader`,
  );
  assert.match(
    MENU_TAG,
    /aria-label=\{t\("land\.menu"\)\}/,
    `the menu button's accessible name is not the translated land.menu (${MENU_TAG}) — the icon is aria-hidden, so this is the only name it has`,
  );
  assert.match(MENU_TAG, /type="button"/, `the menu button has no type="button" (${MENU_TAG})`);
  // The wordmark is what forces a second row between 320px and 336px at 100%
  // text; below 21em the mark stands alone (card item 6).
  const wordmark = nav.match(/<span className="([^"]*)">\s*Jom/);
  assert.ok(wordmark, `${LANDING}: the JomForm wordmark span could not be read`);
  assert.match(
    wordmark![1],
    /max-\[21em\]:hidden/,
    `the wordmark span has className "${wordmark![1]}" without max-[21em]:hidden — mark + wordmark + CTA + menu button need 327px of row, so a 320px phone is pushed into the second row this card removed`,
  );
});

test("the sheet holds the docs link, the login link and BOTH languages as 44px rows", () => {
  assert.ok(sheet, "the sheet block could not be read from the nav");
  assert.match(
    sheet,
    /(^|\s)sm:hidden/,
    `the sheet is not scoped away from the desktop row: ${sheet.slice(0, 200)}`,
  );
  assert.match(
    sheet,
    /menuOpen \? "flex" : "hidden"/,
    "the sheet has no closed state — it must stay in the DOM (the menu button's aria-controls needs a real target) but be display:none while collapsed",
  );
  const rows = [...sheet.matchAll(/<a\b([^>]*)>/g)].map((m) => ({
    href: (m[1].match(/href="([^"]*)"/) ?? [])[1] ?? "",
    cls: classOf(m[0]),
  }));
  for (const href of ["/docs", "/login"]) {
    const row = rows.find((r) => r.href === href);
    assert.ok(row, `the sheet has no ${href} row — a phone would then have no way to reach it at all`);
    assert.match(
      row!.cls,
      HIT_44,
      `the sheet's ${href} row has className "${row!.cls}" with no min-h-11 — every row in the sheet is a 44px target`,
    );
  }
  // The language rows: the same en/ms pair, one button template, 44px, with the
  // state and the endonym language carried the way the desktop control carries
  // them (cards t_53bcd451 and the endonymLang rule).
  assert.match(
    sheet,
    /\[\s*"en"\s*,\s*"ms"\s*\]/,
    "the sheet does not map over the en/ms pair — the phone would have no language switch",
  );
  const langButtons = [...sheet.matchAll(/<button\b([^>]*)>/g)].map((m) => m[1]);
  assert.equal(
    langButtons.length,
    1,
    `the sheet holds ${langButtons.length} <button> templates — expected exactly one, rendered over en/ms`,
  );
  assert.match(langButtons[0], HIT_44, `the sheet's language row has no min-h-11 (${langButtons[0]})`);
  assert.match(
    langButtons[0],
    /aria-pressed=\{lang === l\}/,
    `the sheet's language row does not expose its selected state (${langButtons[0]})`,
  );
  assert.match(
    sheet,
    /onClick=\{\(\) => setLang\(l\)\}/,
    `the sheet's language rows do not switch the language — the sheet must call setLang, exactly as the desktop control does`,
  );
});

test("the desktop row keeps its shape: docs, log in, the CTA and the segmented control", () => {
  // The segmented control is wrapped so it can leave the phone row; the wrapper
  // is the only thing that changed about it.
  assert.match(
    nav,
    /<div className="hidden shrink-0 sm:flex">\s*<LangButtons \/>/,
    `${LANDING}: LangButtons is no longer inside the desktop-only wrapper — at 390px it would put a 44px segmented control back on the phone row`,
  );
  // Order at sm+: Docs · Log in · CTA · EN|BM — the row the card says must stay
  // exactly as it is.
  const order = navAnchors(nav)
    .slice(0, 3)
    .map((a) => a.href);
  assert.deepEqual(
    order,
    ["/docs", "/login", "/builder"],
    `the desktop row's anchor order is ${JSON.stringify(order)}, not Docs · Log in · Build a form`,
  );
});
