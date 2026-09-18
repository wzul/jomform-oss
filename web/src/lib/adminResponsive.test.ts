// Regression guard for the "/admin is not mobile friendly" report.
//
// THE BUG (one line, whole page): `web/src/app/admin/page.tsx` used
// `mx-auto max-w-6xl px-4 py-8` as its page root. Shell puts a page's children
// into `#main-content`, which is a COLUMN flex container (Shell.tsx), so the
// page root is a flex item. `mx-auto` (margin-left/right: auto) cancels
// `align-self: stretch`, so a flex item without a definite width is laid out at
// MAX-CONTENT — and its min-content floor is the widest thing inside it (the
// six-column dead-letters table). Measured on a production build at 360px:
// document scrollWidth 1047px vs clientWidth 360. Every section was ~1015px
// wide, every stat card ~501px, and the page's one action (Requeue) sat 687px
// inside a sideways-scrolling table. `/admin` was the ONLY Shell page in the
// repo with this pattern — the other 11 all use `w-full`.
//
// This repo has no DOM/JSX test runner (no vitest/jest — see
// src/lib/i18n.test.ts, src/lib/adminGrowth.test.ts), so the guard is written
// against source + the class names that carry the behaviour. That is exactly
// the granularity the defect lives at: it was one class on one line, and the
// class name IS the fix. It cannot measure pixels — the browser measurements
// are in the PR description and were re-run against a production build.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
//
// WHAT IS PINNED, AND WHY
//   1. Every Shell page root carries a definite width. This is the class of bug,
//      not the instance: a `mx-auto` root WITHOUT `w-full` anywhere in the app
//      re-creates the 1047px phone page.
//   2. The /admin page root keeps `w-full` AND `max-w-6xl` + `mx-auto` (the
//      desktop centring must survive — removing mx-auto also "fixes" the phone
//      but shifts desktop content 32px left).
//   3. The dead-letters long tokens can break (dedup_key / last_error), so the
//      table's min-content no longer floors the page, and the phone gets a card
//      list with the Requeue action in view instead of 687px inside a scroller.
//   4. The page-shell tap targets are >=44px: hamburger, drawer close, drawer
//      nav rows, the two sign-out buttons, and the Requeue button.
//   5. No text below 12px anywhere in these files, and the /admin body text
//      that a reader has to read is at least 14px (text-sm). The 11px monospace
//      identifiers were the worst case (they were also what held the table open).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const adminPage = read("../app/admin/page.tsx");
const shell = read("../components/Shell.tsx");
const switcher = read("../components/WorkspaceSwitcher.tsx");
const i18n = read("../components/i18n.tsx");
const dict = read("../components/i18nDict.ts");

// Every page that renders inside Shell, so rule 1 is checked across the app and
// not just on the file that was broken.
const SHELL_PAGES = [
  "../app/dashboard-client.tsx",
  "../app/forms/page.tsx",
  "../app/templates/page.tsx",
  "../app/products/page.tsx",
  "../app/sales/page.tsx",
  "../app/spam/page.tsx",
  "../app/deliveries/page.tsx",
  "../app/subscriptions/page.tsx",
  "../app/media/page.tsx",
  "../app/settings/page.tsx",
  "../app/profile/page.tsx",
  "../app/form-settings/page.tsx",
  "../app/admin/page.tsx",
];

/** The opening tag of every page's content root (the direct child of <Shell>). */
function contentRoots(src: string): string[] {
  // `<Shell ...>` then the first element after it. A JSX comment may sit between
  // them (as on /admin), so a leading {/* ... */} is skipped.
  const after = src.split(/<Shell\b[^>]*>/)[1];
  if (!after) return [];
  const rest = after.replace(/^\s*\{\/\*[\s\S]*?\*\/\}\s*/, "");
  const m = rest.match(/^\s*<(\w+)\b[^>]*>/);
  return m ? [m[0]] : [];
}

test("the guard is looking at real content roots (it cannot pass vacuously)", () => {
  // If the regex stops matching (a wrapper element is added, <Shell> is swapped
  // for a layout file) this test fails instead of the suite silently checking
  // zero pages.
  for (const rel of SHELL_PAGES) {
    const roots = contentRoots(read(rel));
    assert.equal(roots.length, 1, `could not locate the content root under <Shell> in ${rel}`);
    assert.match(roots[0], /className=/, `content root in ${rel} has no className to check`);
  }
});

test("no Shell page root sizes to max-content: `mx-auto` implies `w-full`", () => {
  for (const rel of SHELL_PAGES) {
    const root = contentRoots(read(rel))[0];
    const classes = (root.match(/className="([^"]*)"/) || [])[1] || "";
    if (/\bmx-auto\b/.test(classes)) {
      assert.match(
        classes,
        /\bw-full\b/,
        `${rel}: the page root uses mx-auto (which cancels align-self:stretch in Shell's ` +
          `column-flex #main-content) without w-full — the flex item sizes to max-content and the ` +
          `whole document becomes as wide as the widest table inside it. classes: ${classes}`,
      );
    }
  }
});

test("the /admin page root keeps its definite width and its desktop centring", () => {
  const root = contentRoots(adminPage)[0];
  const classes = (root.match(/className="([^"]*)"/) || [])[1] || "";
  assert.match(classes, /\bw-full\b/, `admin page root lost w-full: ${classes}`);
  assert.match(classes, /\bmax-w-6xl\b/, `admin page root lost max-w-6xl: ${classes}`);
  assert.match(classes, /\bmx-auto\b/, `admin page root lost mx-auto (desktop centring): ${classes}`);
});

test("the dead-letters long tokens can break, so they no longer floor the table", () => {
  // The dedup_key is an opaque 41-64 char token with no spaces; unbroken it set
  // the table's min-content to ~1013px (measured). The failure text is a URL.
  assert.match(
    adminPage,
    /break-all[^"]*font-mono[^"]*text-xs[^"]*">\{d\.dedup_key\}/,
    "the dedup_key cell no longer breaks (word-break: break-all) — the table's min-content goes back to ~1013px",
  );
  assert.match(
    adminPage,
    /\[overflow-wrap:anywhere\]/,
    "no cell allows overflow-wrap: anywhere — a long unbreakable failure URL floors the table again",
  );
});

test("a long stat value cannot paint outside its card", () => {
  // Measured on the fixed page at 1024px: a 6-digit `text-2xl` value (~100px
  // wide) painted 1.2px past its 116px card, and 26.8px past a 90px card on the
  // unfixed 1280px page. The value line must be allowed to break.
  const statCard = adminPage.slice(adminPage.indexOf("function StatCard"), adminPage.indexOf("export default function AdminPage"));
  assert.match(
    statCard,
    /className="[^"]*text-2xl[^"]*\[overflow-wrap:anywhere\][^"]*">\{value\}</,
    "the StatCard value line cannot wrap — a wide count escapes the card border",
  );
});

test("on a phone the Requeue action is in a card in view, not inside the sideways scroller", () => {
  // The six-column table is kept for xl and up, where its ~823px min-content
  // fits inside the page box (1056px at >=1216px). Below that each dead letter
  // is a card whose Requeue button is in the flow — measured, the table's
  // Requeue was 687px to the right at 360px and still 24px past the edge at
  // 1024px.
  assert.match(adminPage, /className="space-y-3 xl:hidden"/, "the dead-letters card list is gone");
  assert.match(
    adminPage,
    /className="hidden overflow-x-auto[^"]*xl:block"/,
    "the dead-letters table is no longer xl-and-up only — below that it shows a table wider than the page box",
  );
  const card = adminPage.slice(adminPage.indexOf("space-y-3 xl:hidden"));
  assert.match(
    card.slice(0, card.indexOf("hidden overflow-x-auto")),
    /min-h-11 w-full rounded-md border[^"]*"[^>]*>[\s\S]{0,200}?admin\.dlRequeue/,
    "the phone card lost its full-width 44px-tall Requeue button",
  );
});

test("the page-shell tap targets are at least 44px tall", () => {
  // Measured before: hamburger 32x32, drawer close 21x32, drawer nav rows 40px,
  // desktop sidebar sign-out 46x17, mobile sign-out button 34px, Requeue 71x26.
  assert.match(
    shell,
    /aria-label="Open menu"[\s\S]{0,200}?className="[^"]*\bh-11\b[^"]*\bw-11\b/,
    "the mobile hamburger is not 44x44 (measured 32x32)",
  );
  assert.match(
    shell,
    /aria-label="Close menu"[\s\S]{0,200}?className="[^"]*\bh-11\b[^"]*\bw-11\b/,
    "the drawer close button is not 44x44 (measured 21x32)",
  );
  // Both sign-out buttons (desktop sidebar text link + drawer button).
  const signOuts = [...shell.matchAll(/onClick=\{signOut\}[\s\S]{0,220}?className="([^"]*)"/g)].map((m) => m[1]);
  assert.equal(signOuts.length, 2, "expected the two sign-out buttons");
  for (const classes of signOuts) {
    assert.match(classes, /\bmin-h-11\b/, `a sign-out button is under 44px tall (measured 46x17 text link): ${classes}`);
    assert.match(classes, /\btext-sm\b/, `sign-out text is under 14px (measured 11px): ${classes}`);
  }
  // Drawer nav rows.
  assert.match(
    shell,
    /onClick=\{\(\) => setMenuOpen\(false\)\}[\s\S]{0,240}?className=\{`flex min-h-11 items-center/,
    "the mobile drawer nav rows are under 44px tall (measured 40px)",
  );
  // The page's primary recovery action, in both the card list and the table.
  const requeues = [...adminPage.matchAll(/onClick=\{\(\) => requeue\(d\.dedup_key\)\}[\s\S]{0,200}?className="([^"]*)"/g)].map(
    (m) => m[1],
  );
  assert.equal(requeues.length, 2, "expected the Requeue button in the card list and in the table");
  for (const classes of requeues) {
    assert.match(classes, /\bmin-h-11\b/, `a Requeue button is under 44px tall (measured 71x26): ${classes}`);
  }
});

test("the mobile topbar controls are 44px tall and the language control is a single target", () => {
  // Five 27-37px-wide buttons sat in the phone topbar; growing them all to 44px
  // does not fit a 360px bar, so the phone uses one native select.
  assert.match(
    shell,
    /md:hidden[\s\S]{0,1600}?<LangSelect \/>/,
    "the mobile topbar no longer uses the single-target language select (measured 5 targets of 23px height)",
  );
  assert.match(
    i18n,
    /export function LangToggle\(\)[\s\S]*?className=\{`flex min-h-11 min-w-11 items-center justify-center/,
    "the desktop language toggle buttons are under 44px (measured 23px tall; 37px wide once only the height was raised)",
  );
  assert.match(
    i18n,
    /export function LangSelect\(\)[\s\S]*?className="min-h-11/,
    "the mobile language select is under 44px tall",
  );
});

test("the drawer's workspace switcher is not below the fold", () => {
  // Measured at 360x640: the switcher sat at y 596..643 inside a drawer body
  // ending at 537, and elementFromPoint at its centre returned "Sign out". It is
  // now a shrink-0 row BEFORE the scrollable nav body.
  // Anchor on the JSX comment that opens the drawer, not the words (the words
  // also appear in an earlier keyboard-handling comment).
  const drawer = shell.slice(shell.indexOf("{/* mobile drawer */}"));
  const switcherAt = drawer.indexOf("<WorkspaceSwitcher");
  const scrollBodyAt = drawer.indexOf('min-h-0 flex-1 overflow-y-auto');
  assert.ok(switcherAt > 0, "the drawer no longer renders the workspace switcher");
  assert.ok(
    switcherAt < scrollBodyAt,
    "the drawer switcher is inside/below the scrollable body again — on a 640px-tall phone it starts below the fold",
  );
  const row = drawer.slice(0, switcherAt);
  assert.match(
    row.slice(row.lastIndexOf("<div")),
    /className="shrink-0/,
    "the drawer switcher row is not shrink-0, so the scroll body can push it out of view",
  );
});

test("body text in the admin surface is at least 14px, and nothing is under 12px", () => {
  const files = {
    "admin/page.tsx": adminPage,
    "Shell.tsx": shell,
    "WorkspaceSwitcher.tsx": switcher,
  } as const;
  for (const [name, src] of Object.entries(files)) {
    const tiny = [...src.matchAll(/text-\[(10|11)px\]/g)].map((m) => m[0]);
    assert.deepEqual(tiny, [], `${name} still uses sub-12px type (measured 10px/11px leaves): ${tiny.join(", ")}`);
  }
  // The labels a reader actually reads on /admin (stat-card label + sub line,
  // section headings, the dead-letters hint, table headers and cells).
  const statCard = adminPage.slice(adminPage.indexOf("function StatCard"), adminPage.indexOf("export default function AdminPage"));
  assert.doesNotMatch(statCard, /text-xs/, "a StatCard label/sub line is back under 14px (measured 12px)");
  assert.equal(
    [...adminPage.matchAll(/<thead className="[^"]*text-xs/g)].length,
    0,
    "a table header is back at 12px (measured 12px on all four tables)",
  );
  assert.match(
    adminPage,
    /\{t\("admin\.deadLettersHint"\)\}/,
    "the dead-letters hint text moved — re-check its size",
  );
  assert.match(
    adminPage,
    /className="mb-3 text-sm text-\[var\(--jf-muted\)\]">\{t\("admin\.deadLettersHint"\)\}/,
    "the dead-letters hint is back under 14px (measured 12px)",
  );
});

test("the email stat row does not force five columns into a half-width section", () => {
  // `sm:grid-cols-5` inside the `lg:grid-cols-2` section made 93px cards at
  // 768px and 70px at 1024px, and the text-2xl values painted past the card
  // border (6 glyph spills at 1024px, 2 at 1280px — pre-existing).
  assert.doesNotMatch(
    adminPage,
    /grid grid-cols-2 gap-3 sm:grid-cols-5/,
    "the email stat row is back to five columns — the text-2xl values spill outside their 93px cards",
  );
});

test("the phone language control's accessible name is translated in all locales", () => {
  // LangSelect's aria-label is the only name for the control (its visible text
  // is the current value), so it must exist in all five locales — the i18n
  // completeness guard covers the dictionary, this pins the call site.
  assert.match(
    i18n,
    /aria-label=\{t\("common\.language"\)\}/,
    "the mobile language select lost its translated accessible name",
  );
  for (const lang of ["en", "ms", "zh", "ta", "ar"]) {
    assert.match(
      dict,
      new RegExp(`"common\\.language": \\{[^}]*\\b${lang}:\\s*"[^"]+"`),
      `common.language has no ${lang} value`,
    );
  }
});

test("the viewport meta tag is declared (do not 'fix' a correct one)", () => {
  const layout = read("../app/layout.tsx");
  assert.match(
    layout,
    /export const viewport: Viewport = \{[\s\S]*?width:\s*"device-width"/,
    "the viewport export lost width=device-width — without it nothing else in this file matters on a phone",
  );
});
