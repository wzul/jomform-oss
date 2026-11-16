// Guard for card t_cff48545 — the LAST raw palette utility in the rendered
// /builder document.
//
// WHAT WAS WRONG (measured on prod v1.49.0 @ f62c45b2, signed-in, /builder):
//
//   A variant-aware scan of all 194 class-bearing elements — block settings
//   panel open, 5 blocks in the canvas, 2 of them added from the palette in the
//   same run — found exactly ONE raw Tailwind palette colour utility in the whole
//   document:
//
//     [{"tag":"BUTTON","util":"hover:bg-slate-50","text":"Form mode: Response form"}]
//
//   It is `components/FormModeControl.tsx:136`, a SHARED control the builder
//   header renders — which is why #1761's page-scoped count (/builder = 82 -> 0)
//   did not see it: the class lives in the component, not in the page.
//
//   A prefix-less regex misses this class entirely: it only ever appears under
//   `hover:`, so `^bg-slate-…` finds nothing while the browser paints it in the
//   state every pointer lands on. That is why the rule below reads the WHOLE
//   token, variant prefix included, and why this file's control test plants the
//   exact pre-fix class list.
//
// THE FIX (measured, not stylistic)
//
//   The trigger's class list is tokens only now:
//     rest   bg-white                -> bg-[var(--jf-surface)]     #ffffff in the light block
//     hover  hover:bg-slate-50       -> hover:bg-[var(--jf-bg-soft)] #f8f8f8
//   and the dialog's four remaining palette utilities moved with it
//   (`hover:bg-slate-50` x2, `bg-slate-50`, `bg-indigo-50` ->
//   `bg-[var(--jf-primary-tint)]`, the tint token `tintedSurface.test.ts` already
//   knows in both spellings and whose text pairs `designTokenRatios.ts` records).
//
//   The hover step, measured against the button's own surface:
//     --jf-bg-soft #f8f8f8 on #ffffff = 1.0620:1   (the utility it replaced,
//     #f8fafc, was 1.0463:1 — so the step is not weaker, and the label on it is
//     9.7055:1 with the ▾ caret (--jf-muted) at 4.5522:1.)
//   `--jf-surface-2` (#f1f2f5) — the other token the card offered — was REJECTED
//   ON A MEASUREMENT: it is a stronger step (1.1194:1) but it puts the same ▾
//   caret at 4.3187:1 in the LIGHT block, under the 4.5:1 AA floor (the dark
//   block clears both surfaces, so the light block is what decides). The control
//   test at the bottom keeps that rejection a measurement rather than a memory.
//
//   The RESTING fill is a token as well, because `--jf-bg-soft` MOVES in
//   `[data-theme="dark"]` (#12151a): a fixed `bg-white` under a themed hover step
//   would flash a near-black fill on a white button (18.2942:1) the moment that
//   subtree is used. `--jf-surface` is #ffffff in the light block, so the resting
//   pixel does not change — asserted below, because "no visual change" is a claim
//   that can rot.
//
// WHICH GUARD SEES THIS NOW (card item 4)
//
//   * THIS file is the only one that judges a palette COLOUR UTILITY on the
//     component. Nothing else did: `mutedText.test.ts` and
//     `tintedSurface.test.ts` read `web/src` repo-wide but both judge TEXT
//     TOKENS, so `hover:bg-slate-50` was invisible to them — and a rename with no
//     row here would have left the component unlooked-at.
//   * `mutedText.test.ts` still sees the tokens after the move, and that is not
//     an assumption: `--jf-bg-soft` and `--jf-surface` are already in its
//     LIGHT_SURFACE_RE light-surface model, and its rule for a class list that
//     carries both a muted token and a `hover:` surface compares the token with
//     the HOVER surface. The `?` doc link in this dialog is that exact shape
//     (`text-[var(--jf-muted)] hover:bg-[var(--jf-bg-soft)]`) and measures
//     4.5522:1 there.
//   * `tintedSurface.test.ts` sees the dialog's selected arm, because
//     `bg-[var(--jf-primary-tint)]` is one of the two spellings its
//     SOLID_TINT_RE matches.
//
// WHAT THIS DOES NOT COVER
//   * It reads the component as TEXT (this repo has no JSX test runner; unit
//     tests are `node --test` over `src/lib/*.test.ts`), so it proves the class
//     list is DECLARED with tokens and the pair is measured from globals.css.
//     That the browser paints #f8f8f8 on hover is shown by the CDP read-back on
//     the card, not here.
//   * It is component-scoped by design: ~1,449 raw palette utilities remain in
//     `web/src`, so a repo-wide zero would be a red tree, not a guard. Run
//     `rawPaletteUtilities` from `./jsxSource.ts` on the next page's file when
//     that page migrates.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { codeOnly, jsxTags, rawPaletteUtilities } from "./jsxSource.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const REL = "web/src/components/FormModeControl.tsx";
const SRC = read("../components/FormModeControl.tsx");
const GLOBALS = read("../app/globals.css");

// ── the colour model, recomputed from globals.css ────────────────────────────
type RGB = [number, number, number];

/** WCAG 2.x relative luminance / contrast — https://www.w3.org/TR/WCAG21/#dfn-contrast-ratio */
function channel(c: number): number {
  const v = c / 255;
  return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
}
function luminance([r, g, b]: RGB): number {
  return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
}
function contrast(a: RGB, b: RGB): number {
  const la = luminance(a), lb = luminance(b);
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
}

function parseHex(value: string): RGB | null {
  const h = /^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$/.exec(value.trim());
  if (!h) return null;
  let s = h[1];
  if (s.length === 3) s = s.split("").map((c) => c + c).join("");
  if (s.length === 8) s = s.slice(0, 6); // alpha is not part of the luminance this rule needs
  return [0, 2, 4].map((i) => parseInt(s.slice(i, i + 2), 16)) as RGB;
}

/**
 * The two token blocks, parsed separately. A token that exists in one block and
 * not the other is a hole the pairing question cannot see, so every lookup
 * THROWS on a miss rather than shrinking into a silent pass.
 */
const DARK_AT = GLOBALS.indexOf('\n[data-theme="dark"] {');
assert.ok(DARK_AT > 0, 'globals.css no longer declares a [data-theme="dark"] block — the pair cannot be measured in both themes');

function tokensOf(css: string): Map<string, RGB> {
  const out = new Map<string, RGB>();
  for (const m of css.matchAll(/--(jf-[a-z0-9-]+)\s*:\s*([^;]+);/g)) {
    const rgb = parseHex(m[2]);
    if (rgb) out.set(m[1], rgb);
  }
  return out;
}
const BLOCKS = {
  light: tokensOf(GLOBALS.slice(0, DARK_AT)),
  dark: tokensOf(GLOBALS.slice(DARK_AT)),
};

function token(block: "light" | "dark", name: string): RGB {
  const rgb = BLOCKS[block].get(name);
  if (!rgb) {
    throw new Error(
      `--${name} is not declared in the ${block} block of globals.css — this guard judges a pair, ` +
        `so it fails rather than measuring a token the app does not paint`,
    );
  }
  return rgb;
}

const AA = 4.5;
const pct = (r: number) => `${r.toFixed(4)}:1`;

// ── the trigger, found by IDENTITY (not by class order or line number) ───────
const CODE = codeOnly(SRC);
const TRIGGER = jsxTags(CODE)
  .filter((e) => e.open)
  .map((e) => e.tag)
  .find((t) => CODE.slice(t.attrStart, t.attrEnd).includes('aria-haspopup="dialog"'));

assert.ok(
  TRIGGER,
  `${REL} no longer declares a control with aria-haspopup="dialog" — the form-mode trigger is ` +
    `identified by that attribute so this guard cannot fail on the order of its attributes`,
);

const CLASSES = TRIGGER.variants.join(" ").split(/\s+/).filter(Boolean);
const BG_UTILS = CLASSES.filter((c) => /^(?:[a-z0-9[\]_./&-]+:)*bg-/.test(c));
const TOKEN_OF = (util: string) => /\[var\(--(jf-[a-z0-9-]+)\)\]$/.exec(util)?.[1] ?? null;

const REST_UTIL = BG_UTILS.find((c) => !c.includes(":")) ?? "";
const HOVER_UTIL = BG_UTILS.find((c) => c.startsWith("hover:")) ?? "";
const REST_TOKEN = TOKEN_OF(REST_UTIL);
const HOVER_TOKEN = TOKEN_OF(HOVER_UTIL);
const LABEL_TOKEN = TOKEN_OF(CLASSES.find((c) => /^text-\[var\(--jf-/.test(c)) ?? "") ?? "";

// The ▾ caret lives in a child span of the trigger, so it is read from the
// trigger's own element region — the surface under it is the button's.
const REGION = CODE.slice(TRIGGER.openIdx, CODE.indexOf("</button>", TRIGGER.openIdx));
const CARET_TOKEN =
  /aria-hidden="true"[^>]*className="[^"]*text-\[var\(--(jf-[a-z0-9-]+)\)\]/.exec(REGION)?.[1] ?? null;

// ── rule 1: the component paints no raw palette colour utility ───────────────

test("FormModeControl.tsx paints no raw Tailwind palette colour utility (variant prefixes read)", () => {
  const findings = rawPaletteUtilities(SRC);
  const named = findings.map((f) => `${REL}:${f.line} ${f.util}`).join("; ");
  assert.deepEqual(
    findings,
    [],
    `${REL} still carries raw palette colour utilities: ${named}. Each one is a colour from ` +
      `Tailwind's default scale instead of a --jf-* token: rename it to the token that carries the ` +
      `same role (hover:bg-slate-50 -> hover:bg-[var(--jf-bg-soft)], bg-indigo-50 -> ` +
      `bg-[var(--jf-primary-tint)]) rather than widening this rule.`,
  );
});

// ── rule 2: the rule BITES — controls, not "the tree is clean" ───────────────

test("the palette rule reports the exact pre-fix class lists this card was filed for", () => {
  const cases: Array<[string, string]> = [
    // the defect as measured on prod: only the VARIANT carries the prefix
    [
      'className="shrink-0 rounded-md border border-[var(--jf-border)] bg-white px-2 py-1 text-xs font-medium text-[var(--jf-text-secondary)] hover:bg-slate-50"',
      "hover:bg-slate-50",
    ],
    // the dialog's selected arm
    ['? "border-[var(--jf-accent)]/60 bg-indigo-50"', "bg-indigo-50"],
    // a nested + directional + alpha variant, so the prefix reader is not
    // only able to see a bare `hover:`
    ['className="md:hover:border-slate-200 bg-slate-50/60"', "md:hover:border-slate-200"],
    // a dark slab is a palette step too
    ['className="bg-slate-900 text-emerald-300"', "bg-slate-900"],
  ];
  for (const [src, expected] of cases) {
    const utils = rawPaletteUtilities(src).map((f) => f.util);
    assert.ok(
      utils.includes(expected),
      `the palette rule did not report ${expected} in ${src} — it reported ${JSON.stringify(utils)}. ` +
        `A rule that cannot see its own control is green for the wrong reason.`,
    );
  }

  // ...and it reports the variant-prefixed form WHOLE, because that is what the
  // browser paints and what the original prefix-less count dropped.
  const hoverOnly = rawPaletteUtilities(cases[0][0]).map((f) => f.util);
  assert.deepEqual(hoverOnly, ["hover:bg-slate-50"]);

  // A class quoted in a COMMENT must not be reported: comments are stripped, so
  // a doc line that explains the migration cannot fail the migration.
  assert.deepEqual(rawPaletteUtilities('// was hover:bg-slate-50, now a token\nconst x = 1;'), []);
});

test("the palette rule does not flag token form, the white/black bucket, or non-colour utilities", () => {
  const clean = [
    'className="bg-[var(--jf-surface)] hover:bg-[var(--jf-bg-soft)]"',
    'className="border border-[var(--jf-border)] text-[var(--jf-text-secondary)]"',
    'className="bg-white bg-black/40 text-white"',
    'className="text-xs border-b divide-y shadow-md outline-none rounded-md"',
  ].join("\n");
  assert.deepEqual(
    rawPaletteUtilities(clean).map((f) => f.util),
    [],
    "the palette rule flagged a class that is not a palette family + step; widening its meaning " +
      "would fail the tree for a different question than the one this card asks",
  );
});

// ── rule 3: the trigger's class list, and the pair measured in both blocks ───

test("the trigger's own class list is tokens only: one resting fill, one hover fill", () => {
  assert.equal(
    TRIGGER.classNameUnreadable,
    false,
    `${REL}'s trigger className is no longer statically readable (a prop, an import or a helper the ` +
      `reader cannot follow) — this guard would then judge an EMPTY class list and pass for the wrong ` +
      `reason. Resolve it to a literal at the call site, or teach jsxSource.ts the shape.`,
  );

  assert.deepEqual(
    BG_UTILS,
    ["bg-[var(--jf-surface)]", "hover:bg-[var(--jf-bg-soft)]"],
    `the trigger should paint exactly two backgrounds — a rest fill and a hover fill, both tokens — ` +
      `but paints ${JSON.stringify(BG_UTILS)}`,
  );
  assert.ok(REST_TOKEN, `the rest fill is not a token: ${REST_UTIL}`);
  assert.ok(HOVER_TOKEN, `the hover fill is not a token: ${HOVER_UTIL}`);
  assert.ok(LABEL_TOKEN, "the trigger declares no --jf-* text token for its label");
  assert.ok(CARET_TOKEN, "the trigger's ▾ caret no longer declares a --jf-* text token");

  // The resting pixel: the card's "no visual change in the light block" claim
  // is only true while --jf-surface is #ffffff.
  assert.deepEqual(
    token("light", REST_TOKEN),
    [255, 255, 255],
    `the trigger's rest fill is --${REST_TOKEN}, which is not #ffffff in the light block — the ` +
      `rename from bg-white was measured as a no-op at #ffffff, so re-measure that claim before ` +
      `accepting a different resting pixel`,
  );
});

// The surfaces a token is painted on for this control: the button's own fill in
// each state, in each theme block.
function pairTable(): Array<{ block: "light" | "dark"; where: string; fg: string; bg: string; ratio: number }> {
  const rows: Array<{ block: "light" | "dark"; where: string; fg: string; bg: string; ratio: number }> = [];
  for (const block of ["light", "dark"] as const) {
    for (const [where, surface] of [
      ["rest", REST_TOKEN!],
      ["hover", HOVER_TOKEN!],
    ] as const) {
      for (const fg of [LABEL_TOKEN!, CARET_TOKEN!]) {
        rows.push({ block, where, fg, bg: surface, ratio: contrast(token(block, fg), token(block, surface)) });
      }
    }
  }
  return rows;
}

test("the label and the caret clear AA on BOTH the resting and the hovered fill, in both theme blocks", () => {
  const rows = pairTable();
  const bad = rows.filter((r) => r.ratio < AA);
  const table = rows.map((r) => `${r.block} --${r.fg} on --${r.bg} (${r.where}) ${pct(r.ratio)}`).join(", ");
  assert.deepEqual(
    bad.map((r) => `${r.block} --${r.fg} on --${r.bg} ${pct(r.ratio)}`),
    [],
    `the trigger paints text below the ${AA}:1 AA floor in one of its states: ${table}. If the hover ` +
      `token is the cause, the app has a token that clears AA for this text — measure the pair before ` +
      `choosing it (--jf-bg-soft puts the caret at 4.5522:1; --jf-surface-2 puts it at 4.3187:1).`,
  );
});

test("the hover fill is a visible DARKENING step, no weaker than the utility it replaced", () => {
  for (const block of ["light", "dark"] as const) {
    const rest = token(block, REST_TOKEN!);
    const fill = token(block, HOVER_TOKEN!);
    assert.ok(
      luminance(fill) < luminance(rest),
      `${block}: the hover fill (--${HOVER_TOKEN}) is not darker than the rest fill (--${REST_TOKEN}); ` +
        `the utility it replaced darkened the button on hover, and a hover that lightens a white ` +
        `control reads as a different control`,
    );
    const step = contrast(fill, rest);
    assert.ok(
      step >= 1.04,
      `${block}: --${HOVER_TOKEN} on --${REST_TOKEN} is only ${pct(step)} — weaker than the ` +
        `#f8fafc-on-#ffffff step (1.0463:1) that this token replaced, i.e. the hover affordance got ` +
        `fainter rather than tokenised`,
    );
  }
});

// ── rule 4: the control that keeps the REJECTION a measurement ───────────────

test("the alternative token the card offered is still rejected BY MEASUREMENT (the ▾ caret)", () => {
  // `--jf-surface-2` is the stronger step (1.1194:1 on #ffffff) and would be the
  // obvious pick for a white button. It loses to --jf-bg-soft on ONE number, and
  // only in the light block: the ▾ caret is painted with --jf-muted, which
  // measures under AA on it. (In the dark block the caret clears both surfaces —
  // 6.11:1 on --jf-surface-2 — so the dark block does not decide this; the light
  // one does, and the light block is the one every reader gets by default.)
  const lightRatio = contrast(token("light", CARET_TOKEN!), token("light", "jf-surface-2"));
  assert.ok(
    lightRatio < AA,
    `light: --${CARET_TOKEN} on --jf-surface-2 now measures ${pct(lightRatio)}, at or above the AA ` +
      `floor — the recorded reason for choosing --jf-bg-soft over --jf-surface-2 has expired. ` +
      `Re-measure both pairs and update the comment in ${REL} and this test together; do not just ` +
      `delete the assertion.`,
  );
  // The chosen token must clear it — otherwise the pair above would only prove
  // that the rejected one is bad, not that the picked one is good.
  const chosen = contrast(token("light", CARET_TOKEN!), token("light", HOVER_TOKEN!));
  assert.ok(
    chosen >= AA,
    `light: --${CARET_TOKEN} on the CHOSEN hover fill --${HOVER_TOKEN} measures ${pct(chosen)} — ` +
      `under the AA floor, so this button needs a different hover token`,
  );
});
