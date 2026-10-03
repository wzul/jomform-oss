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
// SCOPE: the dashboard CHROME this guard's card owns — Shell.tsx (the sidebar /
// drawer / account block) and WorkspaceSwitcher.tsx. It is deliberately not
// applied repo-wide yet: 16 other sites paint a base token on `bg-indigo-50`
// (page-level selected chips, badges, a message panel). Those are a wider,
// separate class, filed as its own card, so this guard cannot go red on files
// this card does not touch.

/** The chrome files this rule owns, with the surface each tints. */
const CHROME = [
  { rel: "../components/Shell.tsx", label: "Shell.tsx" },
  { rel: "../components/WorkspaceSwitcher.tsx", label: "WorkspaceSwitcher.tsx" },
];

const TINTED = /(?:^|[\s"'`])bg-indigo-50(?![\w/.-])/;
const BASE_TOKEN = /text-\[var\(--jf-(primary|muted)\)\]/;

/**
 * A className expression expands into every class list it can actually render.
 *
 * `className` here is usually a template literal whose `${cond ? "a" : "b"}`
 * branches are COMPLETE class lists of their own. Checking the flattened text of
 * such an expression is wrong in both directions — a tint in the static prefix
 * reads as paired with a token that lives in a different branch and never
 * renders beside it (Shell's nav row), while a branch that only applies under
 * one condition is invisible. So every `$`-island becomes an ALTERNATIVE (each
 * string literal inside it, plus "" because a conditional renders nothing on
 * its else path) and the class lists are the cross product.
 *
 * Getting this wrong is how the first version of this guard went green on a tree
 * with the defect re-planted: it left the branch's quote characters attached to
 * the class names, so `"bg-indigo-50` never matched the tint pattern.
 */
type Variant = string;
type Seg = { text?: string; alts?: string[] };

/**
 * Read an expression, appending static text and alternative islands to `segs`.
 *
 * Recursive rather than a flat scan because a `${…}` island can itself contain a
 * template literal with its own islands, and the closing `}` has to be paired
 * with the `$`-`{` that opened it. Returns the index just past what it consumed.
 */
function readExpr(expr: string, i: number, inIsland: boolean, segs: Seg[]): number {
  let buf = "";
  const flush = () => {
    if (buf) segs.push({ text: buf });
    buf = "";
  };
  while (i < expr.length) {
    const c = expr[i];
    if (inIsland && c === "}") {
      flush();
      return i + 1;
    }
    if (c === '"' || c === "'") {
      // A plain string literal: its body is static text in this context (a
      // class list, or an operand of the branch condition).
      let j = i + 1;
      while (j < expr.length && expr[j] !== c) {
        if (expr[j] === "\\") j += 1;
        buf += expr[j] ?? "";
        j += 1;
      }
      i = j + 1;
      continue;
    }
    if (c === "`") {
      let j = i + 1;
      while (j < expr.length && expr[j] !== "`") {
        if (expr[j] === "\\") {
          j += 1;
          buf += expr[j] ?? "";
          j += 1;
          continue;
        }
        if (expr[j] === "$" && expr[j + 1] === "{") {
          flush();
          const end = matchBrace(expr, j + 1);
          segs.push({ alts: branchLiterals(expr.slice(j + 2, end)) });
          j = end + 1;
          continue;
        }
        buf += expr[j];
        j += 1;
      }
      i = j + 1; // past the closing backtick
      continue;
    }
    buf += c;
    i += 1;
  }
  flush();
  return i;
}

/** The index of the `}` closing the `{` at `open`. */
function matchBrace(expr: string, open: number): number {
  let depth = 0;
  let i = open;
  while (i < expr.length) {
    const c = expr[i];
    if (c === '"' || c === "'" || c === "`") {
      const q = c;
      i += 1;
      while (i < expr.length && expr[i] !== q) {
        if (expr[i] === "\\") i += 1;
        i += 1;
      }
    } else if (c === "{") depth += 1;
    else if (c === "}") {
      depth -= 1;
      if (depth === 0) return i;
    }
    i += 1;
  }
  return expr.length;
}

/**
 * The class lists a `${…}` island can render.
 *
 * Only the branch RESULTS count. An island is a ternary
 * (`active === n.key ? "a" : "b"`), and its condition is not a class list — a
 * condition operand like `"profile"` would otherwise be read as one. Everything
 * before the first `?` is therefore dropped, and the rest contributes one
 * alternative per string literal. "" is always an alternative because a
 * conditional whose else path is `undefined` renders no class at all.
 */
function branchLiterals(island: string): string[] {
  const q = island.indexOf("?");
  const body = q === -1 ? island : island.slice(q + 1);
  const alts: string[] = [];
  const re = /"((?:[^"\\]|\\.)*)"|'((?:[^'\\]|\\.)*)'/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(body))) alts.push(m[1] ?? m[2] ?? "");
  alts.push("");
  return alts;
}


function classVariants(expr: string): Variant[] {
  const segs: Seg[] = [];
  readExpr(expr, 0, false, segs);
  if (segs.length === 0) return [];
  let out: Variant[] = [""];
  for (const s of segs) {
    const opts = s.text !== undefined ? [s.text] : (s.alts ?? []);
    const next: Variant[] = [];
    for (const o of out) {
      for (const a of opts) {
        next.push(o && a ? `${o} ${a}` : o + a);
        if (next.length >= 256) break;
      }
      if (next.length >= 256) break;
    }
    out = next;
  }
  return out;
}

/**
 * True when a class list paints the tinted active surface in some state, so a
 * hover/focus tint (`hover:bg-indigo-50`) counts — the text is painted on the
 * tint while the state is active, which is the same AA obligation.
 *
 * Translucent tints (`bg-indigo-50/40`) are deliberately NOT matched here: they
 * composite to something lighter, they are a wider class across the page-level
 * chips, and they are tracked separately rather than folded into this rule.
 */
function paintsTint(variants: Variant[]): boolean {
  return variants.some((v) => v.split(/\s+/).some((cls) => /(^|:)bg-indigo-50$/.test(cls)));
}

/** Every base (non-on-tint) text token in a class list. */
function baseTokens(variant: string): string[] {
  return [...variant.matchAll(/text-\[var\(--jf-(?:primary|muted)\)\]/g)].map((m) => m[0]);
}

/**
 * Every JSX tag in the source, in order. Enough of a scanner to answer "is this
 * token inside an element whose own className carries the tint?" plus the
 * ancestor chain at any offset — the two shapes this defect takes. Comments are
 * blanked first (via codeOnly) so a class quoted in a comment can neither raise
 * nor satisfy an assertion.
 *
 * attrStart/attrEnd bound the element's own attribute list, so a `text-[…]`
 * occurrence can be attributed to the element that owns it rather than being
 * read as inherited from an ancestor.
 */
type Tag = {
  name: string;
  variants: Variant[];
  openIdx: number;
  attrStart: number;
  attrEnd: number;
  closeIdx: number;
};

/** True when a class list paints the tinted active surface. */
const tinted = (variants: Variant[]) => paintsTint(variants);

/** Element open/close events, in source order. */
type Event = { open: boolean; tag: Tag };

/**
 * Parse the source into tags and, for each, the index just past its closing
 * `</name>`. Pairing is done with an explicit stack so a token's ancestor chain
 * is exact: a source-order "everything opened before this offset" heuristic
 * gets siblings and nested same-name elements wrong, and would either miss the
 * defect or flag clean markup.
 */
function jsxTags(code: string): Event[] {
  const events: Event[] = [];
  const stack: Tag[] = [];
  let i = 0;
  while (i < code.length) {
    if (code[i] !== "<") {
      i += 1;
      continue;
    }
    const next = code[i + 1] ?? "";
    if (next === "/") {
      let j = i + 2;
      let name = "";
      while (j < code.length && /[\w.:$-]/.test(code[j])) {
        name += code[j];
        j += 1;
      }
      while (j < code.length && code[j] !== ">") j += 1;
      const end = j + 1;
      // Close the nearest matching open tag; anything nested inside it that was
      // never explicitly closed closes with it.
      for (let k = stack.length - 1; k >= 0; k--) {
        if (stack[k].name === name) {
          for (let n = stack.length - 1; n >= k; n--) {
            stack[n].closeIdx = end;
            events.push({ open: false, tag: stack[n] });
          }
          stack.length = k;
          break;
        }
      }
      i = end;
      continue;
    }
    if (!/[A-Za-z_$]/.test(next)) {
      i += 1;
      continue;
    }
    let j = i + 1;
    let name = "";
    while (j < code.length && /[\w.:$-]/.test(code[j])) {
      name += code[j];
      j += 1;
    }
    // Attributes run to the tag's closing '>' at brace depth 0; skip over
    // quoted strings so a `>` inside one cannot end the tag early.
    let depth = 0;
    let k = j;
    while (k < code.length) {
      const ch = code[k];
      if (ch === "{") depth += 1;
      else if (ch === "}") depth -= 1;
      else if (ch === '"' || ch === "'" || ch === "`") {
        const q = ch;
        k += 1;
        while (k < code.length && code[k] !== q) {
          if (code[k] === "\\") k += 1;
          k += 1;
        }
      } else if (ch === ">" && depth === 0) break;
      k += 1;
    }
    const attrText = code.slice(j, k);
    const cm = /className\s*=\s*/.exec(attrText);
    const tag: Tag = {
      name,
      variants: cm ? classVariants(attrText.slice(cm.index + cm[0].length)) : [],
      openIdx: i,
      attrStart: j,
      attrEnd: k,
      closeIdx: k + 1,
    };
    if (attrText.trimEnd().endsWith("/")) {
      // Self-closing: it opens and closes at once and never nests anything.
      tag.closeIdx = k + 1;
      events.push({ open: false, tag });
    } else {
      stack.push(tag);
      events.push({ open: true, tag });
    }
    i = k + 1;
  }
  return events;
}

/**
 * The ancestor chain (outermost first, the token's own element last) at a source
 * offset, built by replaying the events in order.
 */
function ancestorsAt(events: Event[], offset: number): Tag[] {
  const chain: Tag[] = [];
  for (const e of events) {
    if (e.tag.openIdx > offset) break;
    if (e.open) {
      chain.push(e.tag);
    } else {
      // The element closed before (or at) the offset, so it is not an ancestor.
      if (e.tag.closeIdx > offset) continue;
      const at = chain.lastIndexOf(e.tag);
      if (at !== -1) chain.length = at;
    }
  }
  return chain;
}

test("no base text token is painted on the tinted active surface", () => {
  for (const { rel, label } of CHROME) {
    const src = read(rel);
    const code = codeOnly(src);
    const events = jsxTags(code);
    const findings: string[] = [];

    // (a) Same element: a single class list that renders BOTH the tint and a
    //     base token. This is the shape the switcher's active item had
    //     (`bg-indigo-50 font-medium text-[var(--jf-primary)]`) and what the
    //     `+ New workspace` row had on its hover state.
    for (const e of events) {
      for (const v of e.tag.variants) {
        if (!paintsTint([v])) continue;
        const toks = baseTokens(v);
        if (toks.length === 0) continue;
        const line = code.slice(0, e.tag.openIdx).split("\n").length;
        findings.push(`<${e.tag.name}> at line ${line} paints ${toks[0]} on bg-indigo-50`);
      }
    }

    // (b) Inherited: the token sits on a descendant of a tinted element, so it
    //     is painted on the tint even though its own class list is clean. This
    //     is how the switcher's sub-line escaped review — the tint was on the
    //     <button>, the token on the <span> inside it.
    //
    //     The token is attributed to the element that OWNS it (the tag whose
    //     attribute list contains the offset). A token written in the same
    //     element's className was already reported by (a) — reporting it again
    //     as "inherited" would double-count one site.
    const re = /text-\[var\(--jf-(?:primary|muted)\)\]/g;
    let m: RegExpExecArray | null;
    while ((m = re.exec(code))) {
      const owner = events.find(
        (e) => e.tag.attrStart < m!.index && m!.index < e.tag.attrEnd,
      )?.tag;
      if (!owner) continue;
      const ownIsBad = owner.variants.some((v) => paintsTint([v]) && baseTokens(v).length > 0);
      if (ownIsBad) continue; // already (a)
      const chain = ancestorsAt(events, owner.openIdx);
      const tintedEl = chain.find((t) => t !== owner && tinted(t.variants));
      if (!tintedEl) continue;
      const line = code.slice(0, m.index).split("\n").length;
      findings.push(
        `${m[0]} at line ${line} is inside <${tintedEl.name}> which paints bg-indigo-50`,
      );
    }

    // A finding must be reported once per site, not once per variant.
    const unique = [...new Set(findings)];

    assert.deepEqual(
      unique,
      [],
      `${label}: text on the tinted active surface must use the on-tint tokens ` +
        `(--jf-primary-on-tint / --jf-muted-on-tint). --jf-primary measures 4.20:1 and ` +
        `--jf-muted 4.32:1 on #eef2ff, both under the 4.5:1 AA floor:\n  ${unique.join("\n  ")}`,
    );
  }
});

test("the tinted-surface rule is looking at real, reachable markup", () => {
  // The guard above passes on an empty file, so pin that it is actually reading
  // the chrome and that the tinted surface still exists in it. Without this, a
  // rename/move of either file or a refactor that drops the tint would read as a
  // clean pass.
  for (const { rel, label } of CHROME) {
    const code = codeOnly(read(rel));
    assert.ok(code.length > 1000, `${label} is too small to be the chrome source`);
    assert.ok(
      TINTED.test(code),
      `${label} no longer paints bg-indigo-50 anywhere — if the tinted active surface moved, ` +
        `re-anchor this guard rather than deleting it`,
    );
    assert.ok(
      code.includes("--jf-primary-on-tint"),
      `${label} does not use --jf-primary-on-tint at all — the tinted active surface lost its AA-safe token`,
    );
  }
});

