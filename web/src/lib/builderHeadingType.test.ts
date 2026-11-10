// /builder was the ONLY one of 62 routes with no `<h1>`, and the densest
// sub-12px page in the app (card t_a5334287).
//
// WHAT WAS MEASURED (the UX sweep, on the rendered document of every route):
//   * `/builder` h1Count = 0 — every other route rendered exactly 1. A screen
//     reader user therefore got NO page title on the one page where the
//     merchant spends their time, and the settings panel's own labels
//     ("BLOCK SETTINGS", the palette group names) were 10-11px uppercase text,
//     so the aside read as one undifferentiated block.
//   * 36 leaf elements at 11px on this page alone: the palette's label spans
//     (`text-jf-2xs`), the block-card type row (`text-jf-3xs`), the
//     condition-broken badge (`text-jf-4xs`) and the panel hints.
//
// 11px is below every accessible minimum for label/body text and mobile
// browsers inflate it unpredictably, so the floor this card sets is 12px —
// `text-xs`, the step the rest of the app already uses.
//
// WHY A SOURCE GUARD *AND* A BROWSER GUARD (`e2e/a11y-builder-heading-type.spec.ts`)
//   This file is the cheap half: it judges the JSX the author wrote, so a new
//   `text-jf-2xs` in this page fails `npm run test:unit` in seconds, with the
//   offending utility named. What it CANNOT see is the computed value — a
//   class inherited from a parent, a `.jf-*` rule in `globals.css`, or a
//   component the page embeds. That half is measured on the live DOM by the
//   sibling spec, which is in the `a11y` gate by its filename prefix. Neither
//   one is redundant: a source guard over a page that renders nothing would
//   pass, and a browser guard cannot name the class to change.
//
// The pixel map is read from `globals.css` rather than typed in here, so a
// token that is redefined below the floor fails the tree instead of silently
// re-introducing the defect (see the "redefined token" control below).

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = fileURLToPath(new URL(".", import.meta.url)); // web/src/lib/
const WEB = join(HERE, "..", ".."); // web/
const PAGE = join(HERE, "..", "app", "builder", "page.tsx");
const GLOBALS = join(HERE, "..", "app", "globals.css");

const PAGE_SRC = readFileSync(PAGE, "utf8");
const GLOBALS_SRC = readFileSync(GLOBALS, "utf8");

/** The accessible floor for label/body text this card sets. */
const FLOOR_PX = 12;

/** Tailwind's own size steps (the ones this repo writes, in px). */
const TAILWIND_STEPS: Readonly<Record<string, number>> = {
  xs: 12,
  sm: 14,
  base: 16,
  lg: 18,
  xl: 20,
  "2xl": 24,
  "3xl": 30,
  "4xl": 36,
};

/**
 * The design system's ladder, READ FROM `globals.css`.
 *
 * Two layers live there: the legacy rename scale (`--text-jf-2xs: 11px`, sized
 * with no paired line-height) and the reading scale (`--text-jf-read-*`, an
 * alias of `--jf-text-*` in `:root`). Both are utilities a call site writes, so
 * both are resolved to px here.
 */
export function sizeMapFromCss(css: string): Record<string, number> {
  const map: Record<string, number> = { ...TAILWIND_STEPS };
  const legacy = /--text-jf-(?!read-)([a-z0-9]+)\s*:\s*(\d+)px/g;
  for (const m of css.matchAll(legacy)) map[`jf-${m[1]}`] = Number(m[2]);
  const root: Record<string, number> = {};
  for (const m of css.matchAll(/--jf-text-([a-z0-9]+)\s*:\s*(\d+)px/g)) root[m[1]] = Number(m[2]);
  for (const m of css.matchAll(/--text-jf-read-([a-z0-9]+)\s*:\s*var\(--jf-text-([a-z0-9]+)\)/g)) {
    const px = root[m[2]];
    if (px !== undefined) map[`jf-read-${m[1]}`] = px;
  }
  return map;
}

export type SizeUse = { utility: string; px: number };

/**
 * Every size utility in `text` that resolves BELOW the floor.
 *
 * Only real size utilities are judged: `text-center`, `text-wrap` and the
 * colour tokens (`text-[var(--jf-muted)]`) are not lengths and are skipped.
 * An arbitrary length (`text-[10px]`, `text-[0.7rem]`) IS judged — that shape
 * is what the design gate bans, and it is exactly how a sub-12px value sneaks
 * back in.
 */
export function sizesBelowFloor(text: string, map: Record<string, number>): SizeUse[] {
  const out: SizeUse[] = [];
  const token = /\btext-(jf-[a-z0-9]+|xs|sm|base|lg|[2-9]xl|\[[^\]\s]+\])/g;
  for (const m of text.matchAll(token)) {
    const utility = m[0];
    const name = m[1];
    let px: number | undefined;
    if (name.startsWith("[")) {
      const inner = name.slice(1, -1);
      const pxMatch = /^(\d+(?:\.\d+)?)px$/.exec(inner);
      const remMatch = /^(\d+(?:\.\d+)?)rem$/.exec(inner);
      // `var(--jf-…)` and other non-length arbitrary values are colours.
      px = pxMatch
        ? Number(pxMatch[1])
        : remMatch
          ? Number((Number(remMatch[1]) * 16).toFixed(3)) // 0.7rem must read 11.2, not 11.200000000000001
          : undefined;
    } else {
      px = map[name];
    }
    if (px !== undefined && px < FLOOR_PX) out.push({ utility, px });
  }
  return out;
}

/**
 * Counts `<h1>` ELEMENTS in a source file.
 *
 * Comments are stripped first: this file's own explanation of the card quotes
 * `<h1>`, and a guard that counted a comment would either fail on its own
 * documentation or force the documentation to lie about the markup.
 */
export function h1Count(source: string): number {
  return [...stripComments(source).matchAll(/<h1(?=[\s>/])/g)].length;
}

export function stripComments(source: string): string {
  return source
    .replace(/\{\/\*[\s\S]*?\*\/\}/g, "") // JSX block comment
    .replace(/\/\*[\s\S]*?\*\//g, "") // TS block comment
    .replace(/^[ \t]*\/\/.*$/gm, ""); // TS line comment
}

/** The first `<h1 …>…</h1>` element's source, comments stripped. */
export function firstH1(source: string): string | null {
  const m = /<h1(?=[\s>/])[\s\S]*?<\/h1>/.exec(stripComments(source));
  return m ? m[0] : null;
}

/** A heading names itself from its CONTENT: does this h1 carry a text node? */
export function h1HasTextSource(h1: string): boolean {
  const inner = h1.replace(/^<h1[^>]*>/, "").replace(/<\/h1>$/, "");
  // An embedded control contributes no text to the heading's name, so the
  // heading must have text of its own — visible or `sr-only`.
  const withoutControls = inner.replace(/<(input|select|textarea|img|svg)[\s\S]*?(\/>|<\/\1>|>)/g, "");
  return /[A-Za-z]/.test(withoutControls.replace(/<[^>]*>/g, ""));
}

// ─── the size floor ─────────────────────────────────────────────────────────

test("builder page: no text size below 12px", () => {
  const map = sizeMapFromCss(GLOBALS_SRC);
  // The map has to be a real reading of the stylesheet before it can judge
  // anything — an empty map would pass every file vacuously.
  assert.equal(map["jf-2xs"], 11, "globals.css should declare --text-jf-2xs: 11px");
  assert.equal(map["jf-xs"], 12, "globals.css should declare --text-jf-xs: 12px");
  assert.equal(map["jf-read-xs"], 12, "text-jf-read-xs should resolve to 12px");
  assert.equal(map["xs"], 12, "Tailwind text-xs is 12px");

  const bad = sizesBelowFloor(PAGE_SRC, map);
  assert.deepEqual(
    bad,
    [],
    `web/src/app/builder/page.tsx still uses ${bad.length} sub-${FLOOR_PX}px text size(s): ` +
      JSON.stringify(bad) +
      ". Use text-xs (12px) for labels and text-sm (14px) for body; fit density with spacing, " +
      "never with smaller type.",
  );
});

test("builder page: the size scanner BITES on each shape that shipped", () => {
  const map = sizeMapFromCss(GLOBALS_SRC);
  // The three utilities the sweep measured on /builder (11px label spans, the
  // 10px block-card type row, the 9px badge) plus the arbitrary-value shape the
  // design gate bans.
  assert.deepEqual(sizesBelowFloor('<span className="text-jf-2xs">Templates</span>', map), [
    { utility: "text-jf-2xs", px: 11 },
  ]);
  assert.deepEqual(sizesBelowFloor('<div className="text-jf-3xs uppercase">Text</div>', map), [
    { utility: "text-jf-3xs", px: 10 },
  ]);
  assert.deepEqual(sizesBelowFloor('<span className="text-jf-4xs">!</span>', map), [
    { utility: "text-jf-4xs", px: 9 },
  ]);
  assert.deepEqual(sizesBelowFloor('<p className="text-[10px]">x</p>', map), [
    { utility: "text-[10px]", px: 10 },
  ]);
  assert.deepEqual(sizesBelowFloor('<p className="text-[0.7rem]">x</p>', map), [
    { utility: "text-[0.7rem]", px: 11.2 },
  ]);
  // A responsive prefix is judged too — `md:text-jf-3xs` shipped on the block
  // card's ↑ / ↓ / ⋯ buttons.
  assert.deepEqual(sizesBelowFloor('<b className="text-base md:text-jf-3xs">↑</b>', map), [
    { utility: "text-jf-3xs", px: 10 },
  ]);
  // …and the shapes that are NOT sizes must not be reported, or the guard would
  // be red on the fixed tree for reasons that are not this rule.
  assert.deepEqual(
    sizesBelowFloor(
      '<p className="text-[var(--jf-muted)] text-xs text-center">ok</p>',
      map,
    ),
    [],
  );
});

test("builder page: a token redefined below the floor is caught, not trusted", () => {
  // The pixel map is READ from globals.css. If someone edits `--text-jf-xs` to
  // 11px, `text-jf-xs` must become an offender — this is the control that
  // proves the guard is judging the stylesheet and not a constant in this file.
  const shrunk = GLOBALS_SRC.replace(/--text-jf-xs\s*:\s*12px/, "--text-jf-xs: 11px");
  assert.notEqual(shrunk, GLOBALS_SRC, "the fixture must actually change globals.css");
  const map = sizeMapFromCss(shrunk);
  assert.deepEqual(sizesBelowFloor('<p className="text-jf-xs">x</p>', map), [
    { utility: "text-jf-xs", px: 11 },
  ]);
});

// ─── exactly one h1, and it names the form ──────────────────────────────────

test("builder page: exactly one h1", () => {
  const count = h1Count(PAGE_SRC);
  assert.equal(
    count,
    1,
    `web/src/app/builder/page.tsx renders ${count} <h1> element(s). The builder must render exactly ` +
      "one: the form title in the top bar (the sweep measured h1Count=0 on this route and 1 everywhere else).",
  );
});

test("builder page: the h1 names the form (not an empty heading around the input)", () => {
  const h1 = firstH1(PAGE_SRC);
  assert.ok(h1, "no <h1> element found in web/src/app/builder/page.tsx");
  assert.ok(
    h1HasTextSource(h1),
    "the builder's <h1> has no text of its own — a heading whose only content is the title " +
      "<input> has an EMPTY accessible name (a heading names itself from its content, and an " +
      `embedded control contributes no text). Found: ${h1.slice(0, 240)}`,
  );
  // The heading is the form title, with the /forms page's own string for an
  // unsaved form — not a fixed word like "Builder".
  assert.match(h1, /sr-only/, "the h1's text node should be sr-only (the visible control is the input)");
  assert.match(h1, /\btitle\.trim\(\)/, "the h1 should carry the form title");
  assert.match(h1, /forms\.new/, 'the h1 should fall back to the "New form" string (forms.new)');
});

test("builder page: the h1 rules BITE on both defects", () => {
  // No heading at all — the state this card found on /builder.
  assert.equal(h1Count("<main><header><span>title</span></header></main>"), 0);
  // Two headings — how a "just add an h1 somewhere" fix goes wrong: the aside's
  // "Block settings" label becoming a second h1 would fail here.
  assert.equal(h1Count("<h1>title</h1><aside><h1>Block settings</h1></aside>"), 2);
  // A quoted <h1> in a comment is documentation, not an element.
  assert.equal(h1Count("{/* <h1> is the page title */}<h1>x</h1>"), 1);
  // An empty heading wrapped around the editable input — the plausible fix that
  // would count as one h1 and announce nothing.
  assert.equal(h1HasTextSource("<h1><input value={title} aria-label=\"Form title\" /></h1>"), false);
  // …and the shape that ships: the title text, visually hidden, beside the input.
  assert.equal(
    h1HasTextSource('<h1><span className="sr-only">{title.trim() || "New form"}</span><input /></h1>'),
    true,
  );
});
