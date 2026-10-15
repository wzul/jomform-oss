// Repo-wide guard: on a SOLID `bg-indigo-100` tint, a BASE text token has to be
// justified as LARGE TEXT — otherwise it must be the on-tint token.
//
// WHY THIS EXISTS (card t_86755e43)
//   The tinted-surface guard (`tintedSurface.test.ts`) owns `bg-indigo-50`
//   (#eef2ff) only, and its scope note deliberately records `bg-indigo-100` as a
//   DIFFERENT, darker tint "tracked separately, not folded in". This is the
//   separate owner. Its two sites are the avatar-initial badges, both of which
//   painted `--jf-primary` on Tailwind's indigo-100:
//
//     web/src/components/Shell.tsx          sidebar account badge (what a user
//                                           with no uploaded avatar sees)
//     web/src/app/profile/page.tsx          the 80px identity badge
//
//   MEASURED on the real values (indigo-100 is `oklch(93% 0.034 272.788)` in
//   tailwindcss/theme.css, which is #e0e7ff):
//
//     --jf-primary        #5e6ad2  on #e0e7ff = 3.814:1   <- under the 4.5 floor
//     --jf-primary-on-tint#4338ca  on #e0e7ff = 6.414:1
//     --jf-muted          #6b7280  on #e0e7ff = 3.924:1   <- under the 4.5 floor
//     --jf-muted-on-tint  #5b6472  on #e0e7ff = 4.855:1
//
//   Both tokens also pass OFF the tint (--jf-primary-on-tint 7.903:1 and
//   --jf-muted-on-tint 5.982:1 on white), so a site whose tint is conditional can
//   use the on-tint pair unconditionally. Do NOT darken `--jf-primary` itself —
//   that was already rejected as a decision because it shifts every other use.
//
// THE DECISION, PER SITE (the card asked for one, and the two sites differ)
//   1. IS THE INITIAL DECORATION? No, at either site. WCAG's definition is
//      "pure decoration: serving only an aesthetic purpose, providing no
//      information, and having no functionality", with the note that text "is
//      only purely decorative if the words can be rearranged or substituted
//      without changing their purpose"
//      (https://www.w3.org/WAI/WCAG21/Understanding/contrast-minimum.html#dfn-pure-decoration).
//      Substituting a different letter changes whose account this is — the
//      initial is the text stand-in for the avatar image whose `alt` is the
//      user's name — so the SC applies to both sites and no decoration
//      exemption is claimed.
//   2. WHICH FLOOR APPLIES? WCAG large-scale text is "at least 18 point or 14
//      point bold", and the same Understanding page states the conversion:
//      "The ratio between sizes in points and CSS pixels is 1pt = 1.333px,
//      therefore 14pt and 18pt are equivalent to approximately 18.5px and 24px."
//      * Shell.tsx  badge is `text-xs`   = 12px, semibold -> NORMAL text, floor 4.5
//        -> 3.814:1 FAILS. Fixed in this card to `--jf-primary-on-tint`
//        (6.414:1 measured on the live badge).
//      * profile/page.tsx badge is `text-2xl` = 24px = 18pt -> LARGE text, floor 3.0
//        -> 3.814:1 PASSES. The token was deliberately NOT changed: swapping a
//        colour to "please" a ratio that does not apply is exactly what the card
//        said not to do. What it gets instead is the declaration below.
//   3. SO THE RULE CARRIES A LARGE-SIZE REQUIREMENT, not a size table. A base
//      token on this tint is only legal when the SAME class list declares a
//      large-scale size on the element itself (`text-2xl` or larger, or an
//      arbitrary value that resolves to >= 24px). That keeps the profile site's
//      verdict from being an accident of its size: drop `text-2xl` from that
//      badge and this guard goes RED at its file:line, which is the state the
//      sidebar badge was in.
//
//   An element with NO size class is treated as normal text (flagged), because
//   the size it inherits is not a fact this scan can read. That is the
//   conservative direction on purpose: an over-report names a file and a line
//   for a human to look at, while a miss ships the 3.814:1 pairing.
//
// WHY A SOURCE GUARD AND NOT ONLY THE MEASUREMENT
//   `web/e2e/a11y-avatar-badge.spec.ts` is the EXECUTED half — it drives both
//   badges in a real browser and measures the painted colours. It can only reach
//   the sites it knows about. This is the half that stops a THIRD badge from
//   appearing anywhere in the tree with the base token on this tint.
//
// NOT IN SCOPE (deliberate)
//   * solid `bg-indigo-50` — `tintedSurface.test.ts`, whose scope is NOT widened here.
//   * translucent `bg-indigo-50/NN` — its composite depends on what is behind it,
//     which no class-name scan can see; measured per site (card t_2f2ad15a).
//   * `border-indigo-100` (landing, admin, feedback) paints no surface under text,
//     so it is not a text-contrast site at all.
//   * An INHERITED shape (a base token in a child of a `bg-indigo-100` element) is
//     not judged here: this rule reads one class list at a time. Both real sites
//     carry the tint and the token on the same element. If a third one ever
//     appears in the inherited shape, extend this file with the ancestor replay
//     `tintedSurface.test.ts` already uses rather than widening its scope.
//
// The class-list / JSX-shape parser is shared from `web/src/lib/jsxSource.ts` —
// the same module `mutedText.test.ts` and `tintedSurface.test.ts` use, and the
// same file that carries the story of why it was extracted instead of copied.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { codeOnly, jsxTags, tsxFiles } from "./jsxSource.ts";

const SRC = fileURLToPath(new URL("../", import.meta.url)); // web/src/

/**
 * A variant's class tokens, with the JSX wrapper braces stripped.
 *
 * LOAD-BEARING, and found by running this guard rather than by reading it: for a
 * TEMPLATE-LITERAL className the shared scanner glues the expression's own braces
 * onto the outer tokens, so
 *
 *   <div className={`${className} … bg-indigo-100 text-xs … text-[var(--jf-primary)]`}>
 *
 * arrives as the class list `{ … text-[var(--jf-primary)]}` — the last token ends
 * in `}` and matches NO exact-token rule. Measured on the unfixed tree: the rule
 * in this file reported nothing for Shell.tsx:75, which is the sidebar badge it
 * was written for. (The same hole in `tintedSurface.test.ts` is filed as its own
 * card, t_10266084; it is NOT fixed here, because that file's parser ownership is
 * not this card's — this guard normalises the tokens it is handed instead.)
 */
function classTokens(variant: string): string[] {
  return variant
    .split(/\s+/)
    .map((cls) => cls.replace(/^\{+/, "").replace(/\}+$/, ""))
    .filter((cls) => cls !== "");
}

// Solid indigo-100 with its state prefix split out: "" for the resting state,
// "hover" / "focus" / "md:hover" for a state-qualified one. NOT `bg-indigo-100/50`.
const TINT_RE = /^((?:[a-z-]+:)*)bg-indigo-100$/;
// Any text token declared through a --jf-* custom property, base or on-tint.
const ANY_JF_TEXT_RE = /^((?:[a-z-]+:)*)text-\[var\(--jf-[a-z-]+\)\]$/;
// The two BASE tokens (the on-tint pair deliberately does not match).
const BASE_JF_TEXT_RE = /^text-\[var\(--jf-(?:primary|muted)\)\]$/;

/**
 * Tailwind's own size scale, as the large-scale half of it: `--text-2xl` is
 * `1.5rem` in `tailwindcss/theme.css`, i.e. 24px = 18pt = WCAG large text.
 * `text-xl` (1.25rem = 20px) is deliberately NOT here — 20px is normal text.
 */
const LARGE_NAMED = new Set([
  "text-2xl",
  "text-3xl",
  "text-4xl",
  "text-5xl",
  "text-6xl",
  "text-7xl",
  "text-8xl",
  "text-9xl",
]);

/**
 * True when a single class token declares LARGE-SCALE text ON THIS ELEMENT.
 *
 *  * a named scale step of `2xl` or larger;
 *  * an arbitrary length that resolves to >= 24px (`text-[24px]`, `text-[1.5rem]`);
 *  * an arbitrary length in ANY other unit is not judged — unknown, so not a
 *    justification, and the site is flagged.
 *
 * State- or breakpoint-qualified sizes (`md:text-2xl`) do NOT count: the base
 * state would then be an inherited size this scan cannot read, which is the case
 * the rule must not wave through.
 */
function declaresLargeText(cls: string): boolean {
  if (LARGE_NAMED.has(cls)) return true;
  const m = /^text-\[(\d*\.?\d+)(px|rem)\]$/.exec(cls);
  if (!m) return false;
  const n = Number(m[1]);
  return m[2] === "px" ? n >= 24 : n >= 1.5;
}

/**
 * The base tokens this class list paints ON the tint, in `state`.
 *
 * State-aware, and identical in shape to the rule `tintedSurface.test.ts` uses
 * for indigo-50, because "painted on the tint" is a per-state question:
 *
 *   `bg-indigo-100 text-[var(--jf-primary)]`
 *     resting state is tinted and its text is the base token -> judged.
 *
 *   `text-[var(--jf-primary)] hover:bg-indigo-100 hover:text-[var(--jf-primary-on-tint)]`
 *     the tint and the text override arrive together -> the base token is never
 *     painted on the tint, so this is clean.
 *
 *   `text-[var(--jf-primary)] hover:bg-indigo-100`   (no text override)
 *     the bare token is still what is painted while hovered -> judged.
 */
function baseTokensOnTint(variant: string, state: string): string[] {
  const inState: { cls: string; base: boolean }[] = [];
  const bare: { cls: string; base: boolean }[] = [];
  for (const cls of classTokens(variant)) {
    const m = ANY_JF_TEXT_RE.exec(cls);
    if (!m) continue;
    const entry = { cls, base: BASE_JF_TEXT_RE.test(cls) };
    if (m[1] === state) inState.push(entry);
    else if (m[1] === "") bare.push(entry);
  }
  const effective = inState.length > 0 ? inState : bare;
  return effective.filter((e) => e.base).map((e) => e.cls);
}

/** The elements of one file that paint a solid `bg-indigo-100` tint. */
function tintedElementsIn(src: string): { variant: string; name: string; line: number }[] {
  const code = codeOnly(src);
  const out: { variant: string; name: string; line: number }[] = [];
  for (const e of jsxTags(code)) {
    for (const v of e.tag.variants) {
      if (!classTokens(v).some((cls) => TINT_RE.test(cls))) continue;
      out.push({
        variant: v,
        name: e.tag.name,
        line: code.slice(0, e.tag.openIdx).split("\n").length,
      });
    }
  }
  return out;
}

/** Findings for one file: a base token on the tint without a large-size class. */
function findingsIn(rel: string, src: string): string[] {
  const findings: string[] = [];
  for (const el of tintedElementsIn(src)) {
    const classes = classTokens(el.variant);
    const tintStates = classes
      .map((cls) => TINT_RE.exec(cls)?.[1])
      .filter((s): s is string => s !== undefined);
    const toks = [...new Set(tintStates.flatMap((s) => baseTokensOnTint(el.variant, s)))];
    if (toks.length === 0) continue;
    // The large-text justification, read off the SAME class list.
    if (classes.some(declaresLargeText)) continue;
    findings.push(
      `${rel}:${el.line} — <${el.name}> paints ${toks.join(" + ")} on bg-indigo-100 ` +
        `without a large-scale size on the element. Use the on-tint tokens ` +
        `(--jf-primary-on-tint / --jf-muted-on-tint), or declare text-2xl (24px = 18pt) ` +
        `or larger if this site really is large text`,
    );
  }
  // One finding per site, not one per variant.
  return [...new Set(findings)];
}

const FILES = tsxFiles(SRC);

function allFindings(): string[] {
  const out: string[] = [];
  for (const rel of FILES) {
    out.push(...findingsIn(rel, readFileSync(`${SRC}${rel}`, "utf8")));
  }
  return out;
}

test("a base text token on a solid bg-indigo-100 tint is large text, or on-tint", () => {
  const findings = allFindings();
  assert.deepEqual(
    findings,
    [],
    `Text painted on a bg-indigo-100 surface (#e0e7ff) must use the on-tint tokens ` +
      `(--jf-primary-on-tint / --jf-muted-on-tint) unless the element itself declares ` +
      `large-scale text. --jf-primary measures 3.814:1 and --jf-muted 3.924:1 on #e0e7ff, ` +
      `both under the 4.5:1 AA floor for normal text, while #4338ca / #5b6472 measure ` +
      `6.414:1 / 4.855:1. A base token is only defensible here as 18pt-or-larger text ` +
      `(24px = text-2xl):\n  ${findings.join("\n  ")}`,
  );
});

test("the rule bites on the shape it is for, and the large-text carve-out is narrow", () => {
  // Fixtures, so the rule's own judgement is pinned rather than assumed. The
  // first shape IS the defect this card fixed at Shell.tsx; the second is the
  // profile badge's shape, which the card measured as a PASS (24px = 18pt).
  const bad = `export function A() { return <div className="rounded-full bg-indigo-100 text-xs font-semibold text-[var(--jf-primary)]">W</div>; }`;
  assert.deepEqual(
    findingsIn("fixture.tsx", bad).length,
    1,
    `a 12px base token on the tint must be reported — if this passes, the guard is ` +
      `looking at nothing: ${JSON.stringify(findingsIn("fixture.tsx", bad))}`,
  );

  const largeText = `export function B() { return <div className="rounded-full bg-indigo-100 text-2xl font-semibold text-[var(--jf-primary)]">W</div>; }`;
  assert.deepEqual(
    findingsIn("fixture.tsx", largeText),
    [],
    `a 24px (18pt) base token on the tint is the measured pass this card recorded for ` +
      `/profile and must NOT be reported — otherwise the carve-out is not the one written`,
  );

  // ... and it is narrow: the same declaration at 20px, or with a size that is
  // only there at a breakpoint, is NOT large text.
  const almost = `export function C() { return <div className="rounded-full bg-indigo-100 text-xl text-[var(--jf-primary)]">W</div>; }`;
  assert.equal(findingsIn("fixture.tsx", almost).length, 1, "text-xl is 20px — normal text");
  const responsive =
    `export function D() { return <div className="rounded-full bg-indigo-100 md:text-2xl text-[var(--jf-primary)]">W</div>; }`;
  assert.equal(
    findingsIn("fixture.tsx", responsive).length,
    1,
    "a breakpoint-qualified size leaves the base state on an inherited size — flagged",
  );

  // The on-tint token needs no justification at any size.
  const fixed = `export function E() { return <div className="rounded-full bg-indigo-100 text-xs font-semibold text-[var(--jf-primary-on-tint)]">W</div>; }`;
  assert.deepEqual(findingsIn("fixture.tsx", fixed), [], "the on-tint token is the fix");
});

test("the repo-wide rule is looking at real, reachable markup", () => {
  // Every assertion above passes on an empty file list, so pin that the walk
  // found the app and that the two badges this card is about are still in it.
  assert.ok(
    FILES.length >= 40,
    `the walk found only ${FILES.length} .tsx files under web/src — it is not reading the app`,
  );
  assert.ok(
    FILES.includes("components/Shell.tsx") && FILES.includes("app/profile/page.tsx"),
    `the walk is missing the two files this guard was written for (found ${FILES.length})`,
  );

  const tinted: string[] = [];
  const tokensSeen: Record<string, string[]> = {};
  for (const rel of FILES) {
    for (const el of tintedElementsIn(readFileSync(`${SRC}${rel}`, "utf8"))) {
      tinted.push(`${rel}:${el.line}`);
      (tokensSeen[rel] ??= []).push(
        ...classTokens(el.variant).filter((cls) => ANY_JF_TEXT_RE.test(cls)),
      );
    }
  }
  assert.ok(
    tinted.length >= 2,
    `only ${tinted.length} element(s) paint a solid bg-indigo-100 tint — if the tint moved or ` +
      `the class name changed, re-anchor this guard rather than deleting it`,
  );
  for (const rel of ["components/Shell.tsx", "app/profile/page.tsx"]) {
    assert.ok(
      tinted.some((t) => t.startsWith(rel)),
      `no element in ${rel} paints bg-indigo-100 any more, so this guard no longer covers the ` +
        `site this card measured. Found: ${JSON.stringify(tinted)}`,
    );
    // ANTI-VACUITY, and it is the check that caught this guard being blind: a
    // badge whose --jf-* token the scanner cannot NAME is a site the rule above
    // silently skips — which is how a template-literal className read as clean
    // (see `classTokens`). Every tinted badge must expose at least one token.
    assert.ok(
      (tokensSeen[rel] ?? []).length > 0,
      `the tinted element(s) in ${rel} expose NO --jf-* text token to this scanner, so the rule ` +
        `above is not judging them (a className shape it cannot read). Found: ${JSON.stringify(tokensSeen[rel] ?? [])}`,
    );
  }

  const globals = readFileSync(`${SRC}app/globals.css`, "utf8");
  assert.match(
    globals,
    /--jf-primary-on-tint:\s*#4338ca/,
    `globals.css lost --jf-primary-on-tint — the AA-safe token this tint needs`,
  );
});
