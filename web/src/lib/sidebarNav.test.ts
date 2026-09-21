// Sidebar / drawer redesign guard — the account block IS the Profile
// destination, the destination list is grouped, and both surfaces share ONE
// list.
//
// WHAT WAS WRONG (measured on the live drawer, not a hunch)
//   1. 13 destinations sat in one flat list with no grouping, so finding one
//      meant scanning all 13 rows every time.
//   2. `Profile` was its own nav row WHILE the account block at the foot of the
//      sidebar/`drawer` already showed the user's email — one destination in two
//      places, and the block itself looked like a control but was not clickable.
//   3. The desktop <aside> and the mobile drawer each carried their own copy of
//      `nav.map(...)` with their own classes. Every nav change had to be made
//      twice, and the two copies could — and did — drift.
//
// This repo has no JSX/DOM test runner (no vitest/jest — see src/lib/i18n.test.ts,
// src/lib/pageShellLoading.test.ts for the same approach), so the rules are
// asserted against the component SOURCE. That is the granularity the defect
// lives at: it was the shape of a list and the presence/absence of rows.
// The EXECUTED proof is the production build driven in a browser (drawer at
// 412x915, desktop at 1280x800) — see the card's screenshots.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { DICT, LANGS } from "../components/i18nDict.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const shell = read("../components/Shell.tsx");
const dictSrc = read("../components/i18nDict.ts");
const docsIndex = read("../app/docs/page.tsx");
const docsCustomize = read("../app/docs/customize/page.tsx");

/**
 * Blank out comments while keeping every byte's offset (and every newline), for
 * ABSENCE assertions. The component's own comments quote the markup the redesign
 * removed ("copies of `nav.map(...)`", "this text used to be truncate"), and a
 * raw-source match would read those as the defect being back.
 */
function codeOnly(src: string): string {
  let out = "";
  let i = 0;
  let state: "code" | "line" | "block" = "code";
  while (i < src.length) {
    const c = src[i];
    const n = src[i + 1];
    if (state === "code") {
      if (c === "/" && n === "/") { state = "line"; out += "  "; i += 2; continue; }
      if (c === "/" && n === "*") { state = "block"; out += "  "; i += 2; continue; }
      out += c; i += 1; continue;
    }
    if (state === "line") { if (c === "\n") { state = "code"; out += c; } else out += " "; i += 1; continue; }
    if (c === "*" && n === "/") { state = "code"; out += "  "; i += 2; continue; }
    out += c === "\n" ? "\n" : " ";
    i += 1;
  }
  return out;
}

const shellCode = codeOnly(shell);

/**
 * The component body of NavList — the ONE list both surfaces render. Slicing it
 * out means an assertion about rows cannot be satisfied by markup elsewhere in
 * the file (a lesson from adminResponsive.test.ts, where a file-wide match was
 * satisfied by whichever of two call sites still carried the class).
 */
function navListBody(src: string): string {
  const start = src.indexOf("function NavList(");
  assert.ok(start !== -1, "NavList is gone from Shell.tsx — re-anchor this guard");
  const end = src.indexOf("return (\n    <div className=\"flex min-h-screen\">", start);
  assert.ok(end !== -1, "could not find the end of NavList in Shell.tsx");
  return src.slice(start, end);
}

const navList = navListBody(shell);

/** Every destination href declared in Shell.tsx, in source order. */
const hrefs = [...shell.matchAll(/href: "([^"]+)"/g)].map((m) => m[1]);

/** Every `key: "…"` destination identifier declared in Shell.tsx. */
const navKeys = [...shell.matchAll(/\{ key: "([a-z]+)", href:/g)].map((m) => m[1]);

test("the guard is looking at real source (it cannot pass vacuously)", () => {
  assert.ok(
    shell.includes("export function Shell("),
    "Shell.tsx is not the app shell source — the guard's anchor moved",
  );
  assert.ok(navList.length > 500, `NavList slice is too small to be the list: ${navList.length} chars`);
  assert.ok(
    hrefs.length >= 12,
    `expected the dashboard's destinations, got ${hrefs.length}: ${hrefs.join(", ")}`,
  );
  assert.ok(
    Object.keys(DICT).some((k) => k === "navgroup.build"),
    "the dictionary is not the one this guard reads",
  );
});

test("Profile is not a nav row any more — it is the account block", () => {
  // The row itself: no destination keyed `profile`, and no 👤 icon left behind.
  assert.ok(
    !navKeys.includes("profile"),
    `a nav row is keyed "profile" again — the standalone Profile destination came back: ${navKeys.join(", ")}`,
  );
  assert.ok(
    !hrefs.includes("/profile"),
    "/profile is a nav destination again — it must be reached from the account block, which already shows the same email",
  );
  // And the block: a link to /profile on BOTH surfaces, highlighted like a nav
  // row when the profile page is open (aria-current="page" is what makes the
  // account block behave like every other destination).
  const profileLinks = shell.split('href="/profile"').length - 1;
  assert.equal(
    profileLinks,
    2,
    `expected the account block to link /profile on the desktop sidebar AND the mobile drawer, found ${profileLinks}`,
  );
  assert.equal(
    [...shell.matchAll(/aria-current=\{active === "profile" \? "page" : undefined\}/g)].length,
    2,
    "the account block's /profile links lost aria-current — the highlight would not follow the profile page",
  );
  // The block is a real control now, not styled-lookalike markup: it must be a
  // <Link>, and the sign-out action must NOT share its tap area (it is
  // destructive).
  for (const m of shell.matchAll(/href="\/profile"/g)) {
    const before = shell.slice(Math.max(0, m.index - 300), m.index);
    assert.match(
      before,
      /<Link\b/,
      "the /profile account block is not a <Link> — it would render as dead text that looks clickable",
    );
  }
});

test("every destination that existed before is still reachable", () => {
  // The redesign moved rows between groups; it must not delete a destination.
  // Profile is the one intentional exception (it became the account block), and
  // it is asserted above.
  const expected = [
    "/",
    "/forms",
    "/templates",
    "/products",
    "/sales",
    "/spam",
    "/subscriptions",
    "/deliveries",
    "/settings",
    "/media",
    "/chat",
    "/feedback",
  ];
  const missing = expected.filter((h) => !hrefs.includes(h));
  assert.deepEqual(
    missing,
    [],
    `these destinations were dropped from the nav: ${missing.join(", ")} — grouping must not remove reachability`,
  );
  // /admin stays, and stays super-admin gated.
  assert.match(
    shell,
    /if \(user\?\.is_super_admin\)[\s\S]{0,300}?href: "\/admin"/,
    "the super-admin /admin entry is no longer behind the is_super_admin gate — a regular user would be offered a page they cannot use",
  );
});

test("the list is grouped into four labelled sections", () => {
  // The four groups, in order, using t() so they are translated (the i18n guard
  // covers completeness; this pins that the labels are real keys, not literals).
  const labelKeys = [...shell.matchAll(/labelKey: "([^"]+)"/g)].map((m) => m[1]);
  assert.deepEqual(
    labelKeys,
    ["navgroup.build", "navgroup.sales", "navgroup.settings", "navgroup.help"],
    "the nav groups changed — re-check the grouping and update this guard",
  );
  for (const k of labelKeys) {
    assert.ok(k in DICT, `nav group label ${k} is not a dictionary key — it would render as its raw key`);
    assert.match(
      navList,
      /className=\{`px-3 pb-1 text-xs/,
      "the group label lost its styling — an unlabelled section is not a grouping",
    );
  }
  // Each group heading must actually be rendered through t(), and each list must
  // be NAMED by that heading so assistive tech reads the grouping too.
  assert.match(navList, /\{t\(g\.labelKey\)\}/, "the group label is no longer rendered through t()");
  assert.match(
    navList,
    /<ul aria-labelledby=\{`\$\{idPrefix\}-grp-\$\{g\.id\}`\}/,
    "the groups are no longer <ul aria-labelledby> — a screen reader would read 12 ungrouped links",
  );
  assert.match(
    navList,
    /id=\{`\$\{idPrefix\}-grp-\$\{g\.id\}`\}/,
    "the group label lost the id its list points aria-labelledby at (a dangling reference)",
  );
});

test("both surfaces render ONE list", () => {
  // The duplication that let the two surfaces drift: the desktop <aside> and the
  // drawer each mapped the array themselves.
  assert.equal(
    [...shell.matchAll(/g\.items\.map\(/g)].length,
    1,
    "the nav items are mapped in more than one place — the desktop sidebar and the mobile drawer must share one list",
  );
  assert.ok(
    !/\bnav\.map\(/.test(shellCode),
    "a `nav.map(...)` copy is back in Shell.tsx — that is the duplication this redesign removed",
  );
  assert.equal(
    [...shell.matchAll(/<NavList\b/g)].length,
    2,
    "expected exactly two NavList call sites (desktop sidebar + mobile drawer)",
  );
  assert.match(
    shell,
    /<NavList idPrefix="nav-mobile" touch onNavigate=\{\(\) => setMenuOpen\(false\)\}/,
    "the drawer must render the same list with close-on-navigate (tapping a destination closes the drawer)",
  );
  // Distinct id prefixes: the desktop <aside> stays in the DOM (hidden by
  // `hidden md:flex`) while the drawer is open, so a shared id would be a
  // duplicate-id / broken aria-labelledby pair.
  assert.match(
    navList,
    /idPrefix/,
    "the list no longer takes an id prefix — the two surfaces would emit duplicate label ids",
  );
});

test("the account block's own text is readable (no truncated email)", () => {
  // Measured before: the sidebar is 224px wide and the email rendered as
  // "owner00@example.c…" — unreadable on a touch device, because the
  // `title` tooltip that carried the full value is hover-only.
  const avatarNeedle = '<UserAvatar className="h-8 w-8 rounded-full" />';
  const at = shell.indexOf(avatarNeedle);
  assert.ok(at !== -1, "the account block lost its avatar — re-anchor this guard");
  const block = shell.slice(at);
  // Comment-stripped for the ABSENCE check: the block's own comment quotes the
  // class it replaced ("this text used to be `truncate`").
  const blockCode = shellCode.slice(at);
  const deskLinkCode = blockCode.slice(0, blockCode.indexOf("</Link>"));
  assert.ok(
    deskLinkCode.length > 0,
    "could not find the end of the desktop account link — re-anchor this guard",
  );
  assert.ok(
    !/\btruncate\b/.test(deskLinkCode),
    "the account block truncates its own text again — the user's email is unreadable on a phone",
  );
  assert.match(
    block,
    /break-all text-xs text-\[var\(--jf-muted-on-tint\)\]/,
    "the email line no longer breaks, or is painted in a token whose contrast on the tinted account block fails AA (--jf-muted measures 4.32:1 on #eef2ff; the tint token measures 5.35:1)",
  );
  // Sign out stays a separate control below the link (destructive actions must
  // not share a tap area with a navigation link).
  assert.match(
    block,
    /<\/Link>[\s\S]{0,120}?<button\s+onClick=\{signOut\}/,
    "sign out is no longer a separate control after the account link",
  );
});

test("the nav group labels exist in all five locales", () => {
  for (const k of ["navgroup.build", "navgroup.sales", "navgroup.settings", "navgroup.help"]) {
    const entry = DICT[k as keyof typeof DICT] as Record<string, unknown> | undefined;
    assert.ok(entry, `${k} is missing from the dictionary — the sidebar would show the raw key`);
    const missing = LANGS.filter((l) => typeof entry[l] !== "string" || String(entry[l]).trim() === "");
    assert.deepEqual(missing, [], `${k} is untranslated for ${missing.join("/")}`);
  }
  // The dictionary copy read here must be the same one the component renders
  // from — pinned so a second, unguarded dictionary cannot appear.
  assert.ok(dictSrc.includes('"navgroup.build"'), "i18nDict.ts no longer defines navgroup.build");
});

test("the docs no longer send a reader looking for a Profile nav row", () => {
  // AGENTS.md documentation parity: the row is gone, so a sentence that says
  // "go to Profile" without saying how to get there is now a dead instruction.
  // Both docs mentions that name the destination must name the account block.
  const customize = docsCustomize.slice(docsCustomize.indexOf('id="language"'));
  assert.match(
    customize,
    /account block/,
    "the language docs still tell the reader to change the setting on Profile without saying where Profile is — the nav row it referred to no longer exists",
  );
  assert.match(
    docsIndex,
    /account block/,
    "the 2FA trusted-devices docs still assume a Profile nav row exists — name the account block instead",
  );
  assert.ok(
    !/sidebar[^.]{0,80}Profile\b/.test(docsIndex),
    "a docs sentence still names Profile as a sidebar entry",
  );
});
