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
import { codeOnly, jsxTags, tintStates, tintedSurfaceFindings } from "./jsxSource.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const shell = read("../components/Shell.tsx");
const dictSrc = read("../components/i18nDict.ts");
const docsIndex = read("../app/docs/page.tsx");
const docsCustomize = read("../app/docs/customize/page.tsx");

// `codeOnly` (blank comments, keep every byte's offset and newline) comes from
// `./jsxSource.ts` — the shared copy. The local one this file used to carry was a
// second implementation of the same lexer, and it read a `/`+`*` or `/`+`/` sitting
// in JSX TEXT as a comment opener (see that module's header): the ABSENCE
// assertions below depend on the blanking being right, so they must not depend on
// a copy of it.

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
    // The drawer's call site is SPREAD over lines (its props grew when the list
    // moved to module scope), so this anchor spans whitespace instead of one
    // physical line. The property is unchanged: the drawer renders the shared
    // list WITH close-on-navigate and its own id prefix.
    /<NavList\b[\s\S]{0,160}?idPrefix="nav-mobile"[\s\S]{0,160}?\bonNavigate=\{\(\) => setMenuOpen\(false\)\}/,
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
  //
  // The avatar needle is the ELEMENT NAME plus its class, not the whole tag: the
  // account block passes its url/name/email/initial/failed/onFail props now (it
  // used to close over them), so the compact `<UserAvatar className="…" />`
  // spelling no longer exists and a full-tag anchor would fail here for a reason
  // unrelated to this guard.
  const avatarNeedle = "UserAvatar";
  const at = shell.indexOf(`<${avatarNeedle}`);
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

// ─── the tinted-active-surface contrast rule ────────────────────────────────
//
// WHAT WAS WRONG (measured, not a hunch): the tinted "active/selected" surface
// paints Tailwind's `indigo-50` (#eef2ff), and the two base text tokens do not
// clear WCAG AA on it — `--jf-primary` (#5e6ad2) measures 4.20:1 and
// `--jf-muted` (#6b7280) 4.32:1, both under the 4.5:1 floor for the 14px/12px
// text these surfaces use. Measured on #eef2ff, the on-tint pair clears it:
// `--jf-primary-on-tint` (#4338ca) 7.07:1 and `--jf-muted-on-tint` (#5b6472)
// 5.35:1. Both on-tint tokens ALSO pass off the tint, so they are safe on a row
// whose tint is conditional (that is why the substitution can be unconditional).
//
// WHY THIS IS A SOURCE GUARD, not a browser assertion: the repo's own a11y gate
// (`web/e2e/a11y.spec.ts`) scans PUBLIC routes only — it never sees the
// authenticated sidebar/drawer/account block/workspace switcher — so a tinted
// contrast regression in the dashboard chrome is invisible to the gate. That is
// exactly how the switcher's tinted active item shipped at 4.20:1 while the nav
// row was fixed. The EXECUTED proof is `web/e2e/a11y-switcher.spec.ts`, which the
// a11y gate runs (it opens the switcher with two workspaces stubbed at the browser
// level and MEASURES the painted contrast of every row, with a control arm that
// overrides the on-tint tokens to their pre-fix values). This file is the source
// guard that keeps the class from coming back in a file the gate does not reach.
//
// SCOPE: the dashboard CHROME (`Shell.tsx` — the sidebar / drawer / account block
// — and `WorkspaceSwitcher.tsx`), which is where this rule's card found the
// defect. The rule is applied repo-wide now, by `tintedSurface.test.ts`; this
// scoped assertion is not what makes the class guarded, it reports a regression in
// these two files by name, in the file that owns the design.
//
// ONE IMPLEMENTATION, TWO GUARDS — the rule itself lives in `./jsxSource.ts`
// (`tintedSurfaceFindings`, over `tintStates` / `baseTokensOnTint`), and
// `tintedSurface.test.ts` calls the very same function. That is not tidiness. The
// copy that used to sit here was state-UNAWARE: ANY base token in a class list
// plus ANY `bg-indigo-50` in it was a finding, whatever state each belonged to. It
// therefore reported the `+ New workspace` row at its pre-fix shape as a defect
// while the repo-wide guard was green on exactly the same bytes (card t_306dcc1e).
// That row's hover state overrides BOTH the surface and the text, so its base
// token never renders on the tint: measured, it is `--jf-primary` on white at
// 4.70:1 while resting, and the on-tint token at 7.07:1 while hovered — both above
// the 4.5:1 floor. A scoped rule that goes RED where the repo-wide rule is GREEN is
// worse than no rule: it sends a lane hunting a contrast defect that is not there,
// quoting 4.20:1 for a pairing that never renders at that site.
//
// ONE READER, TOO — and this file paid for the copy it no longer has. Every
// `codeOnly` / `jsxTags` call below comes from the same module, because until
// 5ba1a17e (#1112) this file ALSO carried its own byte-copy of the className
// reader (`type Seg` / `readExpr` / `classVariants` / `jsxTags` / `ancestorsAt`).
// That copy was the pre-#1103 parser: it could not follow a class list that is
// not a literal in the element's own attribute, so a nav row whose classes came
// from a module-level helper read back EMPTY and the element was SKIPPED. Card
// t_7d100f49 measured the cost by planting exactly that row into `Shell.tsx` on a
// scratch copy: this file passed 10/0 (GREEN, blind) while `tintedSurface.test.ts`
// — the shared reader, same bytes — reported
// `components/Shell.tsx:143 — <span> paints text-[var(--jf-primary)] on bg-indigo-50`.
// The bite below makes that shape a permanent failure here.

/** The chrome files this rule owns, with the surface each tints. */
const CHROME = [
  { rel: "../components/Shell.tsx", label: "Shell.tsx" },
  { rel: "../components/WorkspaceSwitcher.tsx", label: "WorkspaceSwitcher.tsx" },
];

test("no base text token is painted on the tinted active surface", () => {
  // "Painted on the tint" is a per-STATE question, and the answer is computed by
  // the ONE shared implementation both this guard and the repo-wide one call —
  // see the ONE IMPLEMENTATION note above, and the drift test below.
  for (const { rel, label } of CHROME) {
    const findings = tintedSurfaceFindings(label, read(rel));
    assert.deepEqual(
      findings,
      [],
      `${label}: a base text token is painted on the tinted active surface in the SAME ` +
        `state as the tint. Use the on-tint pair in that state ` +
        `(--jf-primary-on-tint / --jf-muted-on-tint): on #eef2ff (Tailwind indigo-50, the ` +
        `active surface) --jf-primary measures 4.20:1 and --jf-muted 4.32:1, both under ` +
        `the 4.5:1 AA floor, while #4338ca / #5b6472 measure 7.07:1 / 5.35:1. A tinted ` +
        `class list that ALSO carries an on-tint token in that state is NOT a finding ` +
        `(the outline-button shape: 4.70:1 resting, 7.07:1 hovered):\n  ${findings.join("\n  ")}`,
    );
  }
});

test("the chrome rule is state-aware: a hover tint that overrides its text is not a finding", () => {
  // The regression this card fixes (t_306dcc1e). The copy of the rule that used
  // to live in this file read a class list as a flat SET, so the first shape
  // below — the `+ New workspace` row before PR 731 — was reported as a defect
  // while the repo-wide guard was green on exactly the same bytes, and the
  // failure message quoted 4.20:1 for a pairing that never renders there.
  const of = (cls: string) =>
    tintedSurfaceFindings(
      "fixture.tsx",
      `export function P() {\n  return <button className="${cls}">x</button>;\n}`,
    );

  // 710's shape: the base token rests on white, where it passes (4.70:1), and the
  // hover state overrides BOTH the surface and the text (on-tint on the tint,
  // 7.07:1). Measuring this row is what PR 731's card did in a live browser.
  assert.deepEqual(
    of("font-medium text-[var(--jf-primary)] hover:bg-indigo-50 hover:text-[var(--jf-primary-on-tint)]"),
    [],
    "the outline-button shape is a finding again — its hover state overrides the TEXT as " +
      "well as the surface, so its base token is never painted on the tint",
  );

  // Still a finding: a hover tint that does NOT override its text in that state —
  // the base token is what is painted while the tint is showing.
  assert.match(
    of("font-medium text-[var(--jf-primary)] hover:bg-indigo-50").join("\n"),
    /paints text-\[var\(--jf-primary\)\] on bg-indigo-50/,
    "a hover tint whose hover state does not override its text must be a finding",
  );

  // Still a finding: the tint on the RESTING state (the switcher's active row).
  assert.match(
    of("bg-indigo-50 font-medium text-[var(--jf-primary)]").join("\n"),
    /paints text-\[var\(--jf-primary\)\] on bg-indigo-50/,
    "the resting tinted row with a base token must be a finding",
  );
});

test("the chrome rule bites on a nav row built through a helper (card t_7d100f49)", () => {
  // WHAT WAS WRONG: a class list built outside the element's own attribute was
  // read as an EMPTY list, so the element was SKIPPED and this guard reported a
  // clean chrome. Until 5ba1a17e (#1112) that was this file's own copy of the
  // reader; the shared one (./jsxSource.ts, t_10266084 / PR 1103) reads it. The
  // measurement that separated the two, on one scratch tree with the row below
  // planted into `Shell.tsx`:
  //
  //   this file, own copy (5ba1a17e^):  pass 10  fail 0                    <- GREEN, blind
  //   this file, shared reader (HEAD):  ✖ Shell.tsx:143 — <span> paints
  //                                     text-[var(--jf-primary)] on bg-indigo-50
  //
  // This is that reproduction as a permanent assertion, planted into the chrome
  // file's OWN source text: a reader that stops following a helper-built class
  // list fails here instead of walking past the row.
  // The anchor is the nav row's GLYPH, not its class string: the row's own
  // classes are restyled by other cards (t_07eb69c8 changed this very span), so
  // pinning them here would fail this bite for a reason unrelated to the read.
  const NAV_ROW = /<span className="[^"]*">\{n\.icon\}<\/span>/;
  const shellSrc = read("../components/Shell.tsx");
  assert.match(
    shellSrc,
    NAV_ROW,
    "the nav row's glyph span moved — re-anchor this bite (and the card's planted row) rather than deleting it",
  );
  const HELPER =
    'const navRowCls = (active: string) =>\n' +
    '  active === "" ? "bg-indigo-50 text-[var(--jf-primary)]" : "text-[var(--jf-text)]";';
  const planted = shellSrc
    .replace(
      NAV_ROW,
      "<span className={`flex items-center gap-2.5 rounded-lg px-3 py-3 text-sm ${navRowCls(active)}`}>{n.icon}</span>",
    )
    .replace("function NavList({", `${HELPER}\n\nfunction NavList({`);
  assert.notEqual(planted, shellSrc, "nothing was planted — this bite is vacuous");

  assert.match(
    tintedSurfaceFindings("Shell.tsx", planted).join("\n"),
    /paints text-\[var\(--jf-primary\)\] on bg-indigo-50/,
    "a nav row built through a helper is not reported: an element the guard does not read is " +
      "not an element it has cleared — its class list read back empty and the row was skipped " +
      "(the pre-#1112 failure, card t_7d100f49)",
  );

  // The other direction, so "report everything" cannot be how this passes: the
  // helper-built OUTLINE-BUTTON shape. Its base token rests on white, where it
  // measures 4.70:1 and passes, and its hover state overrides BOTH the surface
  // and the text (on-tint on the tint, 7.07:1). This is the shape the card says
  // had to be re-checked when the rule moved onto the shared reader — through a
  // helper — and it fails if the read flattens a conditional class list into one
  // set of tokens.
  const hoverHelper =
    'const navRowCls = (active: string) =>\n' +
    '  active === ""\n' +
    '    ? "text-[var(--jf-primary)] hover:bg-indigo-50 hover:text-[var(--jf-primary-on-tint)]"\n' +
    '    : "text-[var(--jf-text)]";';
  assert.deepEqual(
    tintedSurfaceFindings(
      "fixture.tsx",
      hoverHelper +
        "\nexport function P() {\n" +
        "  return <button className={`rounded ${navRowCls(active)}`}>x</button>;\n" +
        "}",
    ),
    [],
    "the helper-built outline-button shape is reported — its base token is never painted on the " +
      "tint, so the read has gone back to flattening a conditional class list into one set of tokens",
  );
});

test("the chrome rule and the repo-wide rule cannot drift (one implementation)", () => {
  // What this card fixes is TWO guards disagreeing about ONE class list: a
  // state-unaware copy here, a state-aware one in `tintedSurface.test.ts`. The
  // property that makes that impossible is that the decision lives in exactly one
  // module and both guards call it, so pin that — a local rule reappearing in
  // EITHER file is the regression, whichever file it lands in.
  const guards: Array<[string, string]> = [
    ["sidebarNav.test.ts", read("sidebarNav.test.ts")],
    ["tintedSurface.test.ts", read("tintedSurface.test.ts")],
  ];
  for (const [name, src] of guards) {
    assert.match(
      src,
      /import \{[^}]*tintedSurfaceFindings[^}]*\} from "\.\/jsxSource\.ts"/,
      `${name} no longer takes the tinted-surface rule from ./jsxSource.ts — the two guards ` +
        `would be free to disagree about one class list again`,
    );
    assert.ok(
      // Anchored at column 0: a DEFINITION is what would be the drift. The names
      // are also mentioned inside this very test (and in comments), so an
      // unanchored match would flag the check itself.
      !/^function (baseTokensOnTint|findingsIn|paintsTint|classVariants)\b/m.test(src) &&
        !/^const TINT_RE\b/m.test(src),
      `${name} defines its own copy of the tinted-surface rule again — that is the drift this ` +
        `card removed; call \`tintedSurfaceFindings\` in ./jsxSource.ts instead`,
    );
  }
  const shared = read("jsxSource.ts");
  assert.match(
    shared,
    /export function tintedSurfaceFindings\(/,
    "jsxSource.ts lost the shared rule — both guards import a name that is no longer the rule",
  );
  assert.match(
    shared,
    /export function baseTokensOnTint\(/,
    "jsxSource.ts lost the per-state reading — the rule would be pairing a class list as a flat set again",
  );
});

test("the tinted-surface rule is looking at real, reachable markup", () => {
  // The guard above passes on an empty file, so pin that it is actually reading
  // the chrome and that the tinted surface still exists in it. Without this, a
  // rename/move of either file or a refactor that drops the tint would read as a
  // clean pass. The tint is looked for as a RENDERED class list (through the
  // shared parser), not as a substring, so a tint class mentioned in a comment or
  // passed in as a prop cannot satisfy it.
  for (const { rel, label } of CHROME) {
    const src = read(rel);
    const code = codeOnly(src);
    assert.ok(code.length > 1000, `${label} is too small to be the chrome source`);
    const tags = jsxTags(code);
    const tintedElements = tags.filter((e) =>
      e.tag.variants.some((v) => tintStates(v).length > 0),
    );
    assert.ok(
      tintedElements.length > 0,
      `${label} no longer paints bg-indigo-50 anywhere — if the tinted active surface moved, ` +
        `re-anchor this guard rather than deleting it`,
    );
    assert.ok(
      code.includes("--jf-primary-on-tint"),
      `${label} does not use --jf-primary-on-tint at all — the tinted active surface lost its AA-safe token`,
    );
    // The rule above is ALSO green for an element whose class list came back
    // EMPTY, so count what the reader RESOLVED here. A reader that stopped
    // following a shape (a template, a helper call, a ternary) would leave this
    // guard judging fewer elements than it claims — which is the failure card
    // t_7d100f49 measured: a helper-built nav row was read as empty, skipped, and
    // this file reported a clean chrome. Measured on the tree this was written
    // against: Shell.tsx 40 of 41 className attributes, WorkspaceSwitcher.tsx 29
    // of 30 (the one exception in each is a `className` PROP, pinned in
    // `tintedSurface.test.ts`'s UNREADABLE_PIN).
    const resolved = tags.filter(
      (e) => e.open && e.tag.classNameExpr !== "" && !e.tag.classNameUnreadable,
    ).length;
    const floor = label === "Shell.tsx" ? 30 : 20;
    assert.ok(
      resolved >= floor,
      `${label}: the reader resolved only ${resolved} className attributes (below the ${floor} ` +
        `this guard records) — a shape it used to follow is no longer followed, so the rule above ` +
        `is judging fewer elements than it claims (a skipped element is not a cleared one)`,
    );
  }
});

