// Repo-wide guard: a RED or SKY (failure / destructive / informational-link)
// text token must not be painted on a surface it cannot clear 4.5:1.
//
// WHAT WAS WRONG (measured, and re-derived here from the installed theme)
//
//   The red/sky family had no owner. `emeraldText.test.ts:46` named
//   "t_0fdbe189's guard" as the owner of `text-red-500`/`text-sky-600`, but that
//   card never landed a guard — so the family was unowned while the app painted
//   it 106 times. Two of those sites were the LOAD-failure sentence on a page
//   whose whole job is to tell the merchant something went wrong:
//
//     web/src/app/form-settings/page.tsx:1283   {err && <p className="… text-red-400">{err}</p>}
//     web/src/app/builder/page.tsx:1824         {loadErr && <p className="… text-red-400">{loadErr}</p>}
//
//   Tailwind v4's palette is OKLCH, resolved to sRGB exactly as a browser paints
//   it (the conversion below), so these are the real values and not v3 hexes:
//
//     text-red-400  #ff6467   white 2.89:1   --jf-bg 2.71:1   FAILS everywhere
//     text-red-500  #fb2c36   white 3.81:1   --jf-bg 3.58:1   FAILS everywhere
//     text-red-600  #e7000b   white 4.77:1   --jf-bg 4.48:1   PASSES on white,
//                                                             UNDER on --jf-bg
//                                                             and on red-50 (4.36)
//     text-red-700  #c10007   white 6.42:1   --jf-bg 6.03:1   PASSES
//     text-sky-600  #0084d1   white 4.02:1   --jf-bg 3.78:1   FAILS everywhere
//     text-sky-700  #0069a8   white 5.86:1   --jf-bg 5.50:1   PASSES
//
//   The worst of them is 2.71:1 — the least legible ink the app can paint, on the
//   sentence that says "this page failed to load". That is the same "the merchant
//   cannot see the failure" outcome this repo has now fixed for three other
//   surfaces (`--jf-danger-text`, the emerald-600 card t_fc1b67f1, the failure
//   tone card t_c88c854d).
//
// WHY NO GATE SAW IT
//
//   axe judges only the state that is RENDERED, on the surface it is rendered on.
//   `web/e2e/a11y.spec.ts` scans PUBLIC routes only (/login, /landing, /docs/*,
//   …), and every site here is behind a session — so the axe gate has never
//   rendered one of them. The remaining sites are banners that render only after a
//   mutation or a failed load, which a settled-page scan does not reach even in
//   the authenticated chrome. This is therefore a SOURCE-level obligation, and it
//   is the reason `text-red-400` could sit on a page background through every
//   green run.
//
// THE FIX, AND WHICH GUARD OWNS WHAT (so the colour-class guards do not overlap)
//
//   `text-red-700` is the replacement token for EVERY converted red site and
//   `text-sky-700` for the sky ones. Neither is a new choice: `/profile` already
//   painted its failure banner with `text-red-700`, the banners card t_c88c854d
//   added are `text-red-700`, and `/subscriptions:124` already used `text-sky-700`
//   for its sibling chip. Both clear AA on every surface in the list below, they
//   keep the hue so the failure/destructive semantics survive, and — as
//   `--jf-danger-text` already established — a colour that is only ever a TEXT
//   colour must be the darker step.
//
//   Ownership, stated so no two guards judge the same class:
//     * mutedText.test.ts       owns the muted GRAY family (`--jf-muted`,
//                               `text-slate-400/500`, literal `#9ca3af`).
//     * tintedSurface.test.ts   owns the indigo TINT pair (`--jf-primary` /
//                               `--jf-muted` on `bg-indigo-50`).
//     * emeraldText.test.ts     owns the EMERALD/success family.
//     * ctaContrast.test.ts     owns the CTA fill/label pairings (a fill under a
//                               white label), not a coloured label on a surface.
//     * THIS FILE (`dangerText.test.ts`) owns the RED and SKY TEXT family
//                               (`text-red-50..950`, `text-sky-50..950`).
//   The five rules are disjoint: each one's `bad` set is computed from the theme,
//   so a step that passes is simply absent from that guard's set rather than
//   listed as an exception.
//
// WHY THE RULE IS DERIVED FROM THE THEME, NOT A REMEMBERED HEX
//
//   The `bad` set of surfaces for each red/sky step is COMPUTED from the installed
//   Tailwind theme and the `--jf-*` tokens in `globals.css`, so a future
//   `text-red-400` (or any new step) is judged the moment it is written — the step
//   is looked up in the palette, its ratios measured, and it is forbidden on
//   whatever surfaces it fails. A guard with a hand-written hex cannot do that: a
//   palette upgrade would silently move the numbers and the guard would keep
//   passing about a colour the app no longer paints. This mirrors
//   `ctaContrast.test.ts` and `emeraldText.test.ts`; the small OKLCH/contrast
//   block below is deliberately local to this file so a helper defect here cannot
//   blind the other guards (the `jsxSource.ts` header records what a shared copy
//   cost once).
//
// WHAT IT DELIBERATELY DOES NOT DO
//
//   * It does NOT judge `bg-red-*` / `border-red-*` — a fill or a border is not
//     text. `bg-red-500` status DOTS (media, settings) and every `border-red-200`
//     banner edge are untouched.
//   * It does not judge a token against a surface it cannot SEE — a DARK slab
//     (`bg-slate-900`), an overlay (`bg-black/40`), a gradient, or a media
//     query. A source scan cannot composite those, so the token is left to the
//     browser gate instead.
//   * It DOES judge a translucent LIGHT tint (`bg-red-50/60`) — at its OPAQUE
//     base AND at the composite. That is the one alpha shape this guard refuses
//     to abstain on, and the reason is measured twice: a `text-red-600` on
//     `bg-red-50/60` reads 4.511:1 on the tint but 4.360:1 on the `bg-red-50` it
//     names, so the whole margin over AA is carried by the `/60` and a later edit
//     that drops it is invisible to every gate (t_624b7275); and for a tint
//     LIGHTER than `--jf-bg` (slate-50, emerald-50, amber-50) the OPAQUE base
//     reads legal while some `bg-<tint>/n` reads under AA, so the base alone
//     would pass a pairing that ships below AA (t_8c10928a). The guard takes the
//     MINIMUM of the opaque base and the composite — see the `compositeBackdrop`
//     note for the exact numbers.
//   * The COMPOSITE itself is a bound, not a faithful blend, and it says so. It
//     is measured against the DARKEST opaque surface this app paints that is
//     darker than the tint (falling back to `--jf-bg`), because a tint over a
//     darker surface composites darker and can be sub-AA where the `--jf-bg`
//     composite passes (t_fea22a0b defect 2); and it takes the conservative side
//     of the residual 8-bit rounding ambiguity, because a source scan cannot
//     reproduce the browser's premultiplied 8-bit blend (t_fea22a0b defect 1) —
//     see the `shiftToward` note. Both backdrops and both sides are named in the
//     finding, so the guard never implies a "worst case" it did not judge.
//   * It still does NOT judge the `bg-red-500/10` failure banners (login,
//     workspace-picker, magic-link, connect, reset-password). A 500-step fill is
//     not one of the LIGHT surfaces above, so those tokens are not attributed to
//     `bg-red-500` — the alpha composite stays with the browser, and the two
//     specs that PAINT those banners assert their measured ratio
//     (`e2e/load-failure-legibility.spec.ts`). That is a deliberate boundary, not
//     an oversight: adding a saturated `bg-red-500` to this list would judge a
//     status-dot fill as if it were a surface, and nothing on the tree today
//     needs it. It is pinned as an abstention by a fixture, not left implied.
//   * It does not flag a token whose own class list already paints a surface this
//     guard cannot resolve — a DARK slab (`bg-slate-900`) or a gradient. Both are
//     read by the browser gate instead (`a11yDarkSlabCoverage.test.ts` fails the
//     unit suite when such a slab appears on a route `a11y.spec.ts` omits).
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import {
  codeOnly,
  jsxTags,
  ancestorsAt,
  descendantsAt,
  tsxFiles,
  ambientSurfaceClasses,
  type Tag,
} from "./jsxSource.ts";

const SRC = fileURLToPath(new URL("../", import.meta.url)); // web/src/

/**
 * Read a `web/`-relative file through a helper named `read` whose base is
 * `import.meta.url`. The name and the base are load-bearing: `buildContext.test.ts`
 * resolves every literal `read("<lit>")` in this suite against this exact shape and
 * checks the target is copied into the web image, so a read written any other way is
 * either invisible to that guard or reported as unresolved.
 */
const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

// ─── WCAG 2.x relative luminance, from the spec ─────────────────────────────
// https://www.w3.org/TR/WCAG21/#dfn-relative-luminance
type RGB = [number, number, number];

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
/**
 * Move every channel of a composited pixel ONE 8-bit step TOWARD the token it is
 * measured against. `conservativeContrast` uses this to take the dark corner of
 * the pixel's +/-1 rounding box.
 *
 * WHY A +/-1 SHADOW IS NEEDED AT ALL (measured, not assumed)
 *
 *   A composited pixel is NOT reproducible in this file. The browser blends in
 *   PREMULTIPLIED 8-bit — a translucent `oklch(slate-50 / 30%)` is resolved to
 *   `rgb(248 250 252 / 0.3)`, then rounded at two points (alpha and the
 *   premultiplied source) before the sum — whereas the guard's source scan can
 *   only resolve the theme's `oklch()` to sRGB and blend that. Rasterised in
 *   Chromium 145 through `foreignObject` and compared against every model that
 *   can be written here (float blend, round-the-8-bit-tint blend, premultiplied
 *   with the alpha quantised to 8 bits, rounded or not): the BEST model still
 *   mismatches the browser on ~34% of (tint, alpha) pixels — 585/891 on
 *   `--jf-bg` — which is why the honest bound is not a faithful blend but the
 *   conservative side of the residual ambiguity.
 *
 *   The ambiguity is at most one 8-bit step per channel, and its worst verdict
 *   case is a pixel DARKER than the model (a lighter pixel cannot lower the
 *   token's ratio). `conservativeContrast` therefore evaluates the ratio on the
 *   modelled pixel AND on the pixel one step toward the token, and reports the
 *   LOWER — the corner that a rounded-down renderer would paint.
 */
function shiftToward(px: RGB, token: RGB, step = 1): RGB {
  return [0, 1, 2].map((i) => (token[i] < px[i] ? px[i] - step : token[i] > px[i] ? px[i] + step : px[i])) as RGB;
}
function conservativeContrast(token: RGB, px: RGB, step = 1): number {
  return Math.min(contrast(token, px), contrast(token, shiftToward(px, token, step)));
}
/**
 * Source-over compositing of a translucent surface (`fg`, alpha `0..1`) onto the
 * backdrop `bg` it is painted over, in the sRGB the browser blends in. A source
 * scan cannot see the real backdrop, so the guard fixes it — see `surfaceColor`
 * for which tint / backdrop pairs actually change a verdict.
 *
 * This is a MODEL, not the browser's pixel: it resolves the tint's `oklch()` to
 * sRGB and blends that, while the browser blends a premultiplied 8-bit tint that
 * can land one step away (see `shiftToward`). Callers must judge the result
 * through `conservativeContrast`, never with a bare `contrast`.
 */
function compositeOver(fg: RGB, bg: RGB, alpha: number): RGB {
  return [0, 1, 2].map((i) => Math.round(bg[i] + (fg[i] - bg[i]) * alpha)) as RGB;
}
function hexToRgb(hex: string): RGB | null {
  const h = hex.replace("#", "");
  const full = h.length === 3 ? h.split("").map((c) => c + c).join("") : h;
  if (!/^[0-9a-fA-F]{6}$/.test(full)) return null;
  return [0, 2, 4].map((i) => parseInt(full.slice(i, i + 2), 16)) as RGB;
}

// Tailwind v4's palette is OKLCH, so the sweep must resolve OKLCH to the sRGB the
// browser actually paints. This is Björn Ottosson's OKLab -> linear sRGB followed
// by the sRGB transfer function — the same conversion `ctaContrast.test.ts` and
// `emeraldText.test.ts` use, pinned below against the red values this card
// measured (#ff6467 / #fb2c36 / #e7000b / #c10007).
function oklchToRgb(L: number, C: number, hDeg: number): RGB {
  const h = (hDeg * Math.PI) / 180;
  const a = C * Math.cos(h), b = C * Math.sin(h);
  const l = (L + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const m = (L - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const s = (L - 0.0894841775 * a - 1.2914855480 * b) ** 3;
  const out = [
    4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s,
    -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s,
    -0.0041960863 * l - 0.7034186147 * m + 1.7076147010 * s,
  ];
  return out.map((v) => {
    const c = Math.max(0, Math.min(1, v));
    const e = c <= 0.0031308 ? 12.92 * c : 1.055 * c ** (1 / 2.4) - 0.055;
    return Math.round(e * 255);
  }) as RGB;
}

// ─── the palette and the jf tokens ──────────────────────────────────────────

/** `--jf-*` colour tokens declared in `:root` of globals.css. */
function jfTokens(): Map<string, RGB> {
  const css = read("../app/globals.css");
  const out = new Map<string, RGB>();
  for (const m of css.matchAll(/--(jf-[a-z0-9-]+):\s*(#[0-9a-fA-F]{3,6})\s*;/g)) {
    const rgb = hexToRgb(m[2]);
    if (rgb) out.set(m[1], rgb);
  }
  return out;
}

/**
 * Tailwind's palette, read from the INSTALLED theme rather than remembered.
 *
 * The path is `../../node_modules/tailwindcss/theme.css` from `web/src/lib`, i.e.
 * `web/node_modules/tailwindcss/theme.css`, which `web/Dockerfile`'s build stage
 * really has (`npm ci` with WORKDIR /repo/web) — `buildContext.test.ts` resolves
 * this literal and checks exactly that, so the read is valid in the image too.
 */
function tailwindPalette(): Map<string, RGB> {
  const themeRel = "../../node_modules/tailwindcss/theme.css";
  const theme = read(themeRel);
  const out = new Map<string, RGB>();
  for (const m of theme.matchAll(/--color-([a-z0-9-]+):\s*([^;]+);/g)) {
    const name = m[1], value = m[2].trim();
    const ok = /^oklch\(\s*([\d.]+)%\s+([\d.]+)\s+([\d.]+)/.exec(value);
    if (!ok) continue;
    out.set(name, oklchToRgb(parseFloat(ok[1]) / 100, parseFloat(ok[2]), parseFloat(ok[3])));
  }
  return out;
}

const TOKENS = jfTokens();
const PALETTE = tailwindPalette();

// ─── the surfaces this app paints, and the red/sky tokens ───────────────────
//
// Each surface is the class that declares it. The `jf-*` component classes count
// because they are the app's own surfaces: `globals.css` declares
// `.jf-card { background: var(--jf-surface) }` (#ffffff) and `.jf-input { background:
// #fff }`. A red token inside a card therefore sits on WHITE even when no `bg-*`
// utility appears in its class list.
//
// The tinted surfaces are the ones this family is actually painted on: `bg-red-50`
// (the failure banner fill), `bg-slate-50/100` (hover and chip surfaces),
// `bg-indigo-50` (active/selected), `bg-sky-50` (the /chat card and the invite
// chip), `bg-amber-50` (the invite chip) and the two emerald tints (the success
// banner that sits next to a red one). `bg-[var(--jf-danger-bg)]` is the same
// `#fef2f2` as `bg-red-50` and is compared as that name.
const SURFACES: Record<string, RGB> = (() => {
  const out: Record<string, RGB> = {
    "bg-white": [255, 255, 255],
    "bg-[var(--jf-surface)]": TOKENS.get("jf-surface") ?? [255, 255, 255],
    "bg-[var(--jf-bg)]": TOKENS.get("jf-bg") ?? [247, 248, 248],
  };
  for (const name of [
    "slate-50", "slate-100", "slate-200", "indigo-50",
    "emerald-50", "emerald-100", "red-50", "sky-50", "amber-50",
  ]) {
    const rgb = PALETTE.get(name);
    if (rgb) out[`bg-${name}`] = rgb;
  }
  return out;
})();

// The trailing `(\d+)` captures the alpha of a translucent tint (`bg-red-50/60`),
// so the same rule can judge the class's OPAQUE base AND the colour it actually
// paints over the page background. An opaque class leaves the group undefined.
const LIGHT_SURFACE_RE =
  /^((?:[a-z-]+:)*)(bg-(?:white|slate-50|slate-100|slate-200|indigo-50|emerald-50|emerald-100|red-50|sky-50|amber-50)|bg-\[var\(--jf-(?:surface|bg|danger-bg)\)\]|jf-card|jf-input)(?:\/(\d+))?$/;

/**
 * `jf-card` / `jf-input` resolve to the same white surface, and
 * `bg-[var(--jf-danger-bg)]` is the same `#fef2f2` as `bg-red-50`, so each pair
 * is compared under one name and the per-token `bad` sets stay keyed on one entry
 * per surface.
 *
 * A TRANSLUCENT LIGHT TINT (`bg-red-50/60`, `bg-indigo-50/50`, `bg-amber-50/50`)
 * canonicalises to its OPAQUE base, and that is a deliberate change of rule
 * rather than a rounding choice. A source scan cannot composite the alpha — the
 * browser decides the pixel — so the two honest options are to abstain or to
 * judge the base the class names. Abstaining is what let `bg-red-50/60` hide: a
 * `text-red-600` there reads **4.511:1** on the tint but only **4.360:1** on the
 * `bg-red-50` it names, and the 0.011 of margin over AA is carried entirely by
 * the `/60`, which any later edit can drop with every gate still green (card
 * t_624b7275).
 *
 * THE OPAQUE BASE IS NOT ALWAYS THE STRICTEST BOUND — so the rule takes the
 * MINIMUM of the opaque base and the composite (see `compositeBackdrop` for how
 * the composite's backdrop and rounding are chosen).
 *
 * This comment used to claim that the opaque step is "the DARKEST member of
 * {tint, white, --jf-bg}" and therefore the strictest bound. Re-derived from the
 * installed theme, that is FALSE for three of these tints, which are lighter
 * than `--jf-bg` (lum 0.93686): `bg-slate-50` #f8fafc 0.95356, `bg-emerald-50`
 * #ecfdf5 0.94676, `bg-amber-50` #fffbeb 0.96253. Contrast is likewise not
 * monotonic across alpha over a DARKER backdrop, so for exactly those three the
 * opaque base reads LEGAL while some `bg-<tint>/n` reads UNDER AA:
 *
 *   text-red-600 on bg-slate-50   opaque 4.5589:1, composite over --jf-bg 4.4831:1 (/1), over bg-slate-200 3.9171:1 (/10)
 *   text-red-600 on bg-emerald-50 opaque 4.5280:1, composite over --jf-bg 4.4748:1 (/5), over bg-slate-200 3.9096:1 (/10)
 *   text-red-600 on bg-amber-50   opaque 4.5997:1, composite over --jf-bg 4.4803:1 (/4), over bg-slate-200 3.9219:1 (/10)
 *
 * For the other tints the opaque base is the minimum over alpha (they are darker
 * than `--jf-bg`, so the composite can only lighten toward it), which is why the
 * two shapes cannot overlap and why this rule adds no false refusal for the
 * replacement tokens: `text-red-700` and `text-sky-700` clear AA on every opaque
 * tint AND on every composite over `--jf-bg` (worst 5.2085:1 and 4.7502:1), so
 * the census and the repo-wide rule are unchanged by the bound.
 */
function canonicalSurface(cls: string): string {
  const bare = cls.replace(/\/\d+$/, "");
  if (bare === "jf-card" || bare === "jf-input") return "bg-white";
  if (bare === "bg-[var(--jf-danger-bg)]") return "bg-red-50";
  return bare;
}

/**
 * The colour a translucent LIGHT surface composite is measured on: the
 * source-over composite of the tint (`tintClass`, alpha from `bg-<tint>/n`) onto
 * the DARKEST opaque surface in `SURFACES` that is strictly darker than the
 * tint's own opaque base, falling back to the page background `--jf-bg`.
 *
 * WHY NOT A FIXED BACKDROP
 *
 *   The page background is not the worst-case backdrop. A tint darker than
 *   `--jf-bg` composites to a LIGHTER pixel over it and a DARKER one over a
 *   darker surface, and the darker composite can be sub-AA where the `--jf-bg`
 *   one passes (measured in Chromium: `text-red-600` on `bg-slate-50/20` is
 *   4.4859:1 over `--jf-bg` but **3.9817:1** over `bg-slate-200`, and on
 *   `bg-slate-50/10` 4.4470 vs **3.9123**). `SURFACES` is exactly the set of
 *   opaque surfaces this app paints, so compositing against the darkest one a
 *   site could sit on is the conservative bound. The backdrop actually used is
 *   resolved once here and named in the finding, so the "worst case" the guard
 *   claims is the case it shows — never an implied one it did not judge.
 */
function compositeBackdrop(tintClass: string): RGB | null {
  const m = LIGHT_SURFACE_RE.exec(tintClass);
  if (!m) return null;
  const name = canonicalSurface(m[2]);
  const opaque = SURFACES[name];
  if (!opaque) return null;
  if (m[3] === undefined) return opaque;
  const alpha = Number(m[3]) / 100;
  const darker = Object.entries(SURFACES)
    .filter(([, s]) => luminance(s) < luminance(opaque))
    .sort((a, b) => luminance(a[1]) - luminance(b[1]));
  const backdrop = darker.length > 0 ? darker[0][1] : SURFACES["bg-[var(--jf-bg)]"];
  return compositeOver(opaque, backdrop, alpha);
}

/** The backdrop `compositeBackdrop` chose for a translucent class, for the finding. */
function compositeBackdropName(tintClass: string): string {
  const m = LIGHT_SURFACE_RE.exec(tintClass);
  if (!m) return "bg-[var(--jf-bg)]";
  const opaque = SURFACES[canonicalSurface(m[2])];
  if (!opaque) return "bg-[var(--jf-bg)]";
  const darker = Object.entries(SURFACES)
    .filter(([, s]) => luminance(s) < luminance(opaque))
    .sort((a, b) => luminance(a[1]) - luminance(b[1]));
  return darker.length > 0 ? darker[0][0] : "bg-[var(--jf-bg)]";
}

const STATE_RE = /^((?:[a-z-]+:)*)/;
function splitState(cls: string): [string, string] {
  const m = STATE_RE.exec(cls)!;
  return [m[1], cls.slice(m[1].length)];
}

/**
 * Elements that can carry an inherited colour into a surface they paint
 * themselves. Rule (b2) is restricted to these: a block child starts its own
 * line box, so the ancestor's colour never lands on the child's background in
 * the way this defect needs.
 */
const INLINE = new Set(["code", "span", "strong", "em", "b", "i", "a", "small", "kbd", "samp"]);

// A class that sets the text COLOUR (any of them) — used only by the inherited
// shape below, to know whether something between the token and an inline child
// re-declares the colour.
const COLOUR_CLS_RE =
  /^(?:[a-z-]+:)*text-(?:\[(?![^\]]*(?:px|rem|em|%|vh|vw))[^\]]+\]|white|black|current|transparent|inherit|[a-z]+-(?:50|100|200|300|400|500|600|700|800|900|950))$/;
function declaresTextColour(t: Tag): boolean {
  return t.variants.some((v) =>
    v.split(/\s+/).some((cls) => {
      const [, bareCls] = splitState(cls);
      return bareCls !== "" && COLOUR_CLS_RE.test(cls);
    }),
  );
}

/**
 * Every red/sky TEXT token, mapped to the surfaces it cannot clear 4.5:1 on.
 *
 * Computed, not hand-written: for each `red-<step>` / `sky-<step>` the theme
 * declares, the token's `bad` set is every surface above where the measured ratio
 * is under 4.5. `text-red-700` and `text-sky-700` therefore come out with an EMPTY
 * bad set and are never flagged, while `text-red-400`/`500` and `text-sky-600`
 * come out forbidden everywhere — which is exactly the census this card fixed and
 * the rule that stops the next step drifting in.
 */
const TEXT_TOKENS: Record<string, { token: string; bad: string[]; note: string }> = (() => {
  const out: Record<string, { token: string; bad: string[]; note: string }> = {};
  for (const name of PALETTE.keys()) {
    const m = /^(red|sky)-(\d+)$/.exec(name);
    if (!m) continue;
    const rgb = PALETTE.get(name)!;
    const token = `text-${name}`;
    const bad = Object.entries(SURFACES)
      .filter(([, s]) => contrast(rgb, s) < 4.5)
      .map(([n]) => n);
    const white = SURFACES["bg-white"];
    const pageBg = SURFACES["bg-[var(--jf-bg)]"];
    const hex = `#${rgb.map((v) => v.toString(16).padStart(2, "0")).join("")}`;
    out[token] = {
      token,
      bad,
      note:
        `${token} (${hex}) is ${contrast(rgb, white).toFixed(2)}:1 on white and ` +
        `${contrast(rgb, pageBg).toFixed(2)}:1 on --jf-bg`,
    };
  }
  return out;
})();

/** `text-red-600` -> the `red-600` palette key it measures. */
const paletteKey = (tok: string) => tok.replace(/^text-/, "");

/**
 * The translucent-tint counterpart of `TEXT_TOKENS[tok].bad`: the `bg-<tint>/n`
 * classes a token fails AA on ONCE COMPOSITED, judged with the conservative
 * contrast of `compositeBackdrop`.
 *
 * It catches the three tints lighter than `--jf-bg` (slate-50, emerald-50,
 * amber-50), where the opaque base reads legal and only the composite is under
 * AA (t_8c10928a); the 0.03-wide alpha bands `bg-red-50/60` hides in (t_624b7275);
 * and — new here — a tint over a DARKER surface, which the `--jf-bg` composite
 * under-bounds. It never fires for `text-red-700` / `text-sky-700` on any
 * `--jf-bg` composite (worst 5.2085:1 / 4.7502:1), but it does fire for them
 * over a darker backdrop where they genuinely are sub-AA — a real tightening,
 * not a false refusal.
 *
 * The finding names the backdrop actually composited (`over bg-slate-200`), so
 * the case the guard refused on is the case it prints.
 */
function translucentFailures(cls: string, rgb: RGB): string[] {
  const alpha = /\/(\d+)$/.exec(cls);
  if (!alpha) return [];
  const comp = compositeBackdrop(cls);
  if (!comp || conservativeContrast(rgb, comp) >= 4.5) return [];
  const where = compositeBackdropName(cls);
  const suffix = where === "bg-[var(--jf-bg)]" ? "" : ` over ${where}`;
  return [`${canonicalSurface(cls.replace(/\/\d+$/, ""))}/${alpha[1]}${suffix}`];
}

// ─── rule (a): one class list paints both a surface and a forbidden token ────

function surfacesFor(variant: string, state: string): string[] {
  const inState: string[] = [];
  const bare: string[] = [];
  for (const cls of variant.split(/\s+/)) {
    const [s, bareCls] = splitState(cls);
    if (!bareCls) continue;
    const m = LIGHT_SURFACE_RE.exec(cls);
    if (!m) continue;
    if (s === state) inState.push(canonicalSurface(m[2]));
    else if (s === "") bare.push(canonicalSurface(m[2]));
  }
  return inState.length > 0 ? inState : bare;
}

/**
 * The RAW translucent surface classes in a variant (`bg-slate-50/20`), in the
 * given state. They are kept uncanonicalised because the ALPHA is what
 * `surfaceColor` composites; each one is then judged by `translucentFailures`.
 */
function translucentSurfacesFor(variant: string, state: string): string[] {
  const inState: string[] = [];
  const bare: string[] = [];
  for (const cls of variant.split(/\s+/)) {
    const [s, bareCls] = splitState(cls);
    if (!bareCls) continue;
    const m = LIGHT_SURFACE_RE.exec(cls);
    if (!m || m[3] === undefined) continue;
    if (s === state) inState.push(cls);
    else if (s === "") bare.push(cls);
  }
  return inState.length > 0 ? inState : bare;
}

function tokensIn(variant: string, state: string): string[] {
  const out: string[] = [];
  for (const cls of variant.split(/\s+/)) {
    const [s, bareCls] = splitState(cls);
    if (s !== state) continue;
    if (TEXT_TOKENS[bareCls] && TEXT_TOKENS[bareCls].bad.length > 0) out.push(bareCls);
  }
  return out;
}

function declaredSurfaces(variants: string[], state = ""): string[] {
  const out: string[] = [];
  for (const v of variants) {
    for (const cls of v.split(/\s+/)) {
      const [s, bareCls] = splitState(cls);
      if (!bareCls || s !== state) continue;
      if (LIGHT_SURFACE_RE.test(cls)) out.push(canonicalSurface(bareCls));
    }
  }
  return out;
}

/** Findings for one file: shape (a) same element, shape (b) inherited/page-bg. */
function findingsIn(rel: string, src: string, ambient: string[][] = []): string[] {
  const code = codeOnly(src);
  const events = jsxTags(code);
  const findings: string[] = [];
  const lineAt = (idx: number) => code.slice(0, idx).split("\n").length;

  // (a) Same element: one class list that renders BOTH a surface and, in the
  //     state that surface applies, a red/sky token that cannot clear 4.5:1 on it.
  for (const e of events) {
    for (const v of e.tag.variants) {
      const states = new Set<string>([""]);
      for (const cls of v.split(/\s+/)) {
        const [s, bareCls] = splitState(cls);
        if (bareCls && (TEXT_TOKENS[bareCls] || LIGHT_SURFACE_RE.test(cls))) states.add(s);
      }
      for (const state of states) {
        const surfaces = surfacesFor(v, state);
        const translucent = translucentSurfacesFor(v, state);
        if (surfaces.length === 0 && translucent.length === 0) continue;
        for (const tok of tokensIn(v, state)) {
          const rgb = PALETTE.get(paletteKey(tok));
          if (!rgb) continue;
          const bad = TEXT_TOKENS[tok].bad.filter((b) => surfaces.includes(b));
          // The opaque verdict is not the whole bound. A translucent tint on this
          // SAME element (`bg-slate-50/20`) can be under AA where its opaque base
          // is legal, so its composited ratio is asked too.
          const all = [...bad, ...translucent.flatMap((cls) => translucentFailures(cls, rgb))];
          if (all.length === 0) continue;
          findings.push(
            `${rel}:${lineAt(e.tag.openIdx)} — <${e.tag.name}> paints ${tok} on ${all.join(" / ")}` +
              `${state ? ` (in the ${state} state)` : ""}: ${TEXT_TOKENS[tok].note}; use text-red-700 / text-sky-700`,
          );
        }
      }
    }
  }

  // (b) Inherited, or on the page background.
  //
  //     * the token sits on a descendant of a surface element, so it is painted
  //       on that surface even though its own class list declares none;
  //     * NO ancestor declares a surface, so it is painted on the app's page
  //       background — `globals.css` sets `html, body { background: var(--jf-bg) }`.
  //
  //     The token is attributed to the element that OWNS it (the tag whose
  //     attribute list contains the offset). A token written in the same
  //     element's className was already reported by (a).
  //
  //     An ancestor only counts here when its surface is UNQUALIFIED, because the
  //     descendant's token and the ancestor's surface would have to be in the same
  //     state to overlap and a source scan cannot pair states across elements.
  const PAGE_BG = "bg-[var(--jf-bg)]";
  const layoutSurface = ambient.flatMap((variants) => declaredSurfaces(variants))[0];
  const TOKEN_RE = new RegExp(
    `(?:^|[\\s"'\`])(${Object.keys(TEXT_TOKENS)
      .filter((t) => TEXT_TOKENS[t].bad.length > 0)
      .map((t) => t.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"))
      .join("|")})(?![\\w-])`,
    "g",
  );
  let m: RegExpExecArray | null;
  while ((m = TOKEN_RE.exec(code))) {
    const idx = m.index + m[0].length - m[1].length;
    const owner = events.find((e) => e.tag.attrStart < idx && idx < e.tag.attrEnd)?.tag;
    if (!owner) continue;
    const tok = m[1];
    const ownIsBad = owner.variants.some((v) =>
      v.split(/\s+/).some((cls) => {
        const [s, bareCls] = splitState(cls);
        if (!bareCls || !LIGHT_SURFACE_RE.test(cls)) return false;
        return tokensIn(v, s).some((tk) => {
          const rgb = PALETTE.get(paletteKey(tk));
          if (!rgb) return false;
          const opaque = TEXT_TOKENS[tk].bad.filter((b) => surfacesFor(v, s).includes(b));
          const comp = translucentSurfacesFor(v, s).flatMap((c) => translucentFailures(c, rgb));
          return opaque.length > 0 || comp.length > 0;
        });
      }),
    );
    if (ownIsBad) continue; // already (a)

    // The token's OWN element may declare a background this guard deliberately
    // cannot judge: a DARK slab (`bg-slate-900`), a gradient, a translucent tint
    // (`bg-red-500/10`), or a palette colour that is not a "light surface" step
    // above. In that case the ancestor's light surface is NOT what the token is
    // painted on, and attributing it there is a false positive. A source scan
    // cannot composite a gradient or an alpha tint either, so this abstains
    // instead of guessing — the same call the header states.
    const ownBackgrounds = owner.variants.flatMap((v) =>
      v
        .split(/\s+/)
        .map((cls) => splitState(cls))
        .filter(([s, bareCls]) => s === "" && bareCls && bareCls.startsWith("bg-"))
        .map(([, bareCls]) => bareCls),
    );
    if (ownBackgrounds.length > 0 && !ownBackgrounds.some((b) => LIGHT_SURFACE_RE.test(b))) continue;

    // The NEAREST surface ancestor wins, not the outermost one. CSS paints
    // through the innermost opaque background between the text and the page, so
    // taking the first match mis-attributes any token nested inside two surfaces.
    // Measured on this tree: the `text-red-600` in the /chat card footer sits on
    // the card's own `bg-sky-50` (4.474:1, BELOW AA) but the outermost surface in
    // its ancestor chain is the `bg-white` wrapper around the transcript, so the
    // guard read 4.77:1 and abstained — which is how this site reached the tree
    // with the guard green (t_624b7275). The same `.find()` shape is still in
    // `emeraldText.test.ts:402` and `mutedText.test.ts:419`.
    const surfaceEl = ancestorsAt(events, owner.openIdx)
      .filter((t) => t !== owner)
      .filter((t) =>
        t.variants.some((v) =>
          v.split(/\s+/).some((cls) => {
            const [s] = splitState(cls);
            return s === "" && LIGHT_SURFACE_RE.test(cls);
          }),
        ),
      )
      .at(-1);
    const surfaces = surfaceEl
      ? surfaceEl.variants.flatMap((v) =>
          v
            .split(/\s+/)
            .map((cls) => splitState(cls))
            .filter(([, bareCls]) => bareCls && LIGHT_SURFACE_RE.test(bareCls))
            .map(([s, bareCls]) => (s === "" ? canonicalSurface(bareCls) : ""))
            .filter(Boolean),
        )
      : (layoutSurface ? [layoutSurface] : [PAGE_BG]);
    // The ancestor may be a TRANSLUCENT tint (`bg-slate-50/20`), where the
    // opaque base is legal but the composite over `--jf-bg` is not. Judging only
    // the base is exactly the hole this card closes.
    const translucent = surfaceEl
      ? surfaceEl.variants.flatMap((v) => translucentSurfacesFor(v, ""))
      : [];
    const rgb = PALETTE.get(paletteKey(tok));
    const bad = [
      ...TEXT_TOKENS[tok].bad.filter((b) => surfaces.includes(b)),
      ...(rgb ? translucent.flatMap((cls) => translucentFailures(cls, rgb)) : []),
    ];
    if (bad.length === 0) continue;
    const where = surfaceEl
      ? `inside <${surfaceEl.name}> which paints`
      : layoutSurface
        ? "on a surface its layout paints"
        : "on the page background";
    findings.push(
      `${rel}:${lineAt(idx)} — ${tok} is ${where} ${bad.join(" / ")}: ${TEXT_TOKENS[tok].note}; use text-red-700 / text-sky-700`,
    );

    // (b2) Inherited by an inline descendant that repaints a failing surface.
    //      A token legal on its own element is NOT safe if an inline descendant
    //      declares a surface the token cannot clear: CSS colour is inherited.
    const pageSurfaces = surfaceEl ? surfaces : (layoutSurface ? [layoutSurface] : [PAGE_BG]);
    for (const child of descendantsAt(events, owner)) {
      if (!INLINE.has(child.name)) continue;
      const childSurfaces = child.variants.flatMap((v) =>
        v
          .split(/\s+/)
          .map((cls) => splitState(cls))
          .filter(([s, bareCls]) => s === "" && bareCls && LIGHT_SURFACE_RE.test(bareCls))
          .map(([, bareCls]) => canonicalSurface(bareCls)),
      );
      const childTranslucent = child.variants.flatMap((v) => translucentSurfacesFor(v, ""));
      if (childSurfaces.length === 0 && childTranslucent.length === 0) continue;
      const nearest = events
        .filter((e) => e.open && e.tag.openIdx <= child.openIdx && e.tag.closeIdx > child.openIdx && declaresTextColour(e.tag))
        .pop()?.tag;
      if (nearest !== owner) continue;
      const okBehind = TEXT_TOKENS[tok].bad.every((b) => !pageSurfaces.includes(b));
      if (!okBehind) continue;
      const rgbB2 = PALETTE.get(paletteKey(tok));
      const bad2 = [
        ...TEXT_TOKENS[tok].bad.filter((b) => childSurfaces.includes(b)),
        ...(rgbB2 ? childTranslucent.flatMap((cls) => translucentFailures(cls, rgbB2)) : []),
      ];
      if (bad2.length === 0) continue;
      findings.push(
        `${rel}:${lineAt(child.openIdx)} — ${tok} inherited into <${child.name}> which paints ` +
          `${bad2.join(" / ")}: ${TEXT_TOKENS[tok].note}; use text-red-700 / text-sky-700`,
      );
    }
  }

  return [...new Set(findings)];
}

const FILES = tsxFiles(SRC);
const SOURCES = new Map<string, string>();
function sourceOf(rel: string): string {
  let s = SOURCES.get(rel);
  if (s === undefined) {
    s = readFileSync(`${SRC}${rel}`, "utf8");
    SOURCES.set(rel, s);
  }
  return s;
}

function allFindings(): string[] {
  const out: string[] = [];
  for (const rel of FILES) {
    out.push(...findingsIn(rel, sourceOf(rel), ambientSurfaceClasses(rel, FILES, sourceOf)));
  }
  return out;
}

test("no red or sky TEXT token is painted on a surface it fails AA on (repo-wide)", () => {
  const findings = allFindings();
  assert.deepEqual(
    findings,
    [],
    `Red/sky text must clear 4.5:1 on the surface it is painted on. ` +
      `text-red-400 (#ff6467) is 2.89:1, text-red-500 (#fb2c36) 3.81:1 and ` +
      `text-sky-600 (#0084d1) 4.02:1 on white — text-red-700 (#c10007) and ` +
      `text-sky-700 (#0069a8) clear AA everywhere and are the tokens this family uses:\n` +
      findings.join("\n"),
  );
});

test("the repo-wide rule is looking at real, reachable markup", () => {
  // The guard above passes on an empty file list, so pin that the walk found the
  // app, that the token the fix depends on still exists, and that the palette
  // resolution it relies on really works. Without this, a broken walk (or a
  // rename of every token) reads as a clean pass.
  assert.ok(
    FILES.length >= 40,
    `the walk found only ${FILES.length} .tsx files under web/src — it is not reading the app`,
  );
  assert.ok(
    FILES.includes("components/Shell.tsx") && FILES.includes("app/form-settings/page.tsx"),
    `the walk is missing core files (found ${FILES.length})`,
  );

  // The OKLCH conversion is real: it reproduces the red values this card measured
  // from the installed theme, so the ratios above are about the colours the
  // browser paints and not a remembered v3 hex (#ef4444 is the v3 value and is
  // NOT what Tailwind v4 emits for red-500).
  const hex = (name: string) => {
    const rgb = PALETTE.get(name);
    assert.ok(rgb, `the installed Tailwind theme no longer declares ${name} — re-anchor this guard`);
    return `#${rgb!.map((v) => v.toString(16).padStart(2, "0")).join("")}`;
  };
  assert.equal(hex("red-400"), "#ff6467", "red-400 no longer resolves to the measured #ff6467");
  assert.equal(hex("red-500"), "#fb2c36", "red-500 no longer resolves to the measured #fb2c36");
  assert.equal(hex("red-600"), "#e7000b", "red-600 no longer resolves to the measured #e7000b");
  assert.equal(hex("red-700"), "#c10007", "red-700 no longer resolves to the measured #c10007");
  assert.equal(hex("sky-600"), "#0084d1", "sky-600 no longer resolves to the measured #0084d1");
  assert.equal(hex("sky-700"), "#0069a8", "sky-700 no longer resolves to the measured #0069a8");

  // The forbidden steps are forbidden because they were MEASURED below AA, not by
  // a hardcoded list.
  for (const doomed of ["text-red-400", "text-red-500", "text-red-600", "text-sky-600"]) {
    assert.ok(
      TEXT_TOKENS[doomed].bad.length >= 3,
      `${doomed} came out with ${TEXT_TOKENS[doomed].bad.length} failing surfaces — ` +
        `the ratio computation has gone wrong`,
    );
  }
  // The replacement tokens clear AA on every surface this guard compares. That is
  // the property the fix depends on, and it is a MEASUREMENT in the same run.
  for (const surface of Object.keys(SURFACES)) {
    for (const ok of ["text-red-700", "text-sky-700"]) {
      assert.ok(
        !TEXT_TOKENS[ok].bad.includes(surface),
        `${ok} is now measured below AA on ${surface} — re-measure before trusting this guard`,
      );
    }
  }

  // The tokens the fix uses are what the app paints, and the steps it replaced are
  // gone: a rename is invisible to a rule keyed on a class string.
  const source = FILES.map((rel) => codeOnly(sourceOf(rel))).join("\n");
  const count = (re: RegExp) => [...source.matchAll(re)].length;
  assert.ok(
    count(/text-red-700(?![\w-])/g) >= 20,
    `only ${count(/text-red-700(?![\w-])/g)} uses of text-red-700 — if the token was renamed, re-anchor this guard`,
  );
  assert.ok(
    count(/text-sky-700(?![\w-])/g) >= 2,
    `only ${count(/text-sky-700(?![\w-])/g)} uses of text-sky-700 — if the token was renamed, re-anchor this guard`,
  );
  for (const gone of ["text-red-400", "text-red-500", "text-sky-600"]) {
    assert.ok(
      !new RegExp(`${gone}(?![\\w-])`).test(source),
      `${gone} is back in the app — this is a step card t_5c17adc9 removed, ` +
        `and it has no AA-safe use on any surface this app paints`,
    );
  }

  // The shapes this rule must still SEE must be pinned, or a later edit can turn a
  // fix into a silent abstention and the site it was written for reaches the tree
  // with this guard green (which is exactly how the /chat footer and the /settings
  // `/60` cell got here).
  //
  // (1) A translucent LIGHT tint is judged at its composite, not skipped.
  const tinted = findingsIn(
    "fixture.tsx",
    [
      "export function P() {",
      "  return (",
      '    <div className="jf-card">',
      '      <tr className="bg-red-50/60">',
      '        <td className="text-red-600">Failed</td>',
      "      </tr>",
      "    </div>",
      "  );",
      "}",
      "",
    ].join("\n"),
  );
  assert.ok(
    tinted.some((f) => f.includes("text-red-600") && f.includes("bg-red-50")),
    `a text-red-600 on bg-red-50/60 (4.360:1 on the surface it names) must be ` +
      `reported; the guard abstained instead: ${JSON.stringify(tinted)}`,
  );

  // (2) The NEAREST surface ancestor is the one the text is painted on, not the
  //     outermost — the /chat footer's exact shape (a sky-50 card inside a white
  //     wrapper). 4.474:1 on the card, 4.770:1 on the wrapper, so an outermost
  //     attribution reads it as legal.
  const nested = findingsIn(
    "fixture.tsx",
    [
      "export function P() {",
      "  return (",
      '    <div className="bg-white">',
      '      <div className="rounded-lg border border-sky-200 bg-sky-50 px-3 py-2 text-xs">',
      '        <p className="mt-2 font-medium text-red-600">The change could not be applied.</p>',
      "      </div>",
      "    </div>",
      "  );",
      "}",
      "",
    ].join("\n"),
  );
  assert.ok(
    nested.some((f) => f.includes("text-red-600") && f.includes("bg-sky-50")),
    `a text-red-600 inside an inner bg-sky-50 card must be attributed to the CARD ` +
      `(4.474:1), not to the outermost bg-white wrapper (4.770:1): ${JSON.stringify(nested)}`,
  );

  // (3) A tint over a DARKER resolvable surface is judged against THAT surface,
  //     not against the page background. `bg-slate-50/75` over `--jf-bg` reads
  //     4.5560:1 for text-red-600 — LEGAL — but the darkest surface this app
  //     paints, `bg-slate-200` (lum 0.80173), reads 4.3592:1, UNDER AA. Naming
  //     only the `--jf-bg` composite under-bounds the real pixel (t_fea22a0b
  //     defect 2). The rule resolves the darkest surface in SURFACES and the
  //     finding names it. (This alpha is chosen so the site passes on `--jf-bg`:
  //     a lower one like /20 is under AA on BOTH backdrops, so it would not pin
  //     the backdrop choice — it fires whichever one is used.)
  const darkerBackdrop = findingsIn(
    "fixture.tsx",
    [
      "export function P() {",
      "  return (",
      '    <div className="bg-slate-200">',
      '      <div className="bg-slate-50/75">',
      '        <span className="text-red-600">Failed</span>',
      "      </div>",
      "    </div>",
      "  );",
      "}",
      "",
    ].join("\n"),
  );
  assert.ok(
    darkerBackdrop.some(
      (f) => f.includes("text-red-600") && f.includes("bg-slate-50/75") && f.includes("over bg-slate-200"),
    ),
    `a text-red-600 on bg-slate-50/75 must be judged against the darker surface it sits ` +
      `on (4.359:1), not against --jf-bg (4.556:1), and the finding must name the ` +
      `backdrop: ${JSON.stringify(darkerBackdrop)}`,
  );

  // (5) The 8-bit rounding ambiguity — the shadow ONLY, with the backdrop choice
  //     held fixed. Here `bg-slate-50/93` composited over the darkest surface is
  //     #f6f9fb, 4.5114:1 with the ROUNDED pixel (PASS); the same pixel one 8-bit
  //     step down (`#f5f8fa`) is 4.4723:1, UNDER AA. The browser's premultiplied
  //     8-bit blend really does land on either side (measured in Chromium 145:
  //     the model is 4.486:1 at /30 where a bare `contrast` read 4.514:1 and
  //     PASSED — a real sub-AA miss), so a correct guard must refuse the darker
  //     corner. This alpha is the one where the SHADOW is the whole reason the
  //     fixture fires: the plain rounded ratio passes, so a model that drops
  //     `conservativeContrast` passes this pairing the browser can paint sub-AA.
  const roundingShadow = findingsIn(
    "fixture.tsx",
    [
      "export function P() {",
      "  return (",
      '    <div className="bg-slate-50/93">',
      '      <span className="text-red-600">Failed</span>',
      "    </div>",
      "  );",
      "}",
      "",
    ].join("\n"),
  );
  assert.ok(
    roundingShadow.some((f) => f.includes("text-red-600") && f.includes("bg-slate-50/93")),
    `text-red-600 on bg-slate-50/93 is 4.5114:1 on the ROUNDED pixel (PASS) but 4.4723:1 ` +
      `one 8-bit step down (UNDER AA), which is the side the browser can paint; the ` +
      `conservative bound must refuse it: ${JSON.stringify(roundingShadow)}`,
  );

  // …and the bound must NOT fire for the replacement tokens on the composites the
  // app actually paints: text-red-700 and text-sky-700 clear AA on every tint and
  // every composite over --jf-bg (worst 5.2085:1 and 4.7502:1), so the same
  // markup with the token this family actually ships is legal and must stay
  // unreported.
  const lighterTintSafe = findingsIn(
    "fixture.tsx",
    [
      "export function P() {",
      "  return (",
      '    <div className="bg-slate-50/20">',
      '      <span className="text-red-700">Retry</span>',
      "    </div>",
      "  );",
      "}",
      "",
    ].join("\n"),
  );
  assert.deepEqual(
    lighterTintSafe,
    [],
    `text-red-700 clears AA on bg-slate-50/20 (6.04:1 over --jf-bg, 5.35:1 over the ` +
      `darker bg-slate-200); the composite bound must not refuse it: ${JSON.stringify(lighterTintSafe)}`,
  );

  // (4) The SAME element may paint the tint and the token together
  //     (`bg-slate-50/20 text-red-600`); shape (a) must judge the composite too,
  //     or a class list that carries both re-opens the hole the ancestor shape
  //     covers.
  const sameElement = findingsIn(
    "fixture.tsx",
    [
      "export function P() {",
      "  return (",
      '    <p className="bg-slate-50/20 text-red-600">Failed</p>',
      "  );",
      "}",
      "",
    ].join("\n"),
  );
  assert.ok(
    sameElement.some((f) => f.includes("text-red-600") && f.includes("bg-slate-50/20")),
    `a same-element bg-slate-50/20 + text-red-600 (4.486:1 over --jf-bg, 3.987:1 over the ` +
      `darker bg-slate-200) must be reported by shape (a), not only by the ancestor ` +
      `shape: ${JSON.stringify(sameElement)}`,
  );
});

// ─── the deliberate abstentions, pinned so they stay deliberate ──────────────
//
// The composite bound must NOT reach into two shapes it does not model. Left
// implicit, either could be read as an oversight; pinned, a change to one is a
// visible failing fixture instead of a silent widening of the rule.

test("the composite bound abstains on a saturated translucent fill, and says so", () => {
  // `bg-red-500/10` (login / workspace-picker / magic-link / connect banners) is a
  // SATURATED fill, not a light surface, so it is not in the list the composite
  // bound knows. The token keeps its alpha and the ratio stays with the browser
  // gate that paints it (`e2e/load-failure-legibility.spec.ts`) —
  // `docs/accessibility.md` states this boundary, and this fixture makes it a
  // tested one.
  assert.deepEqual(
    translucentFailures("bg-red-500/10", PALETTE.get("red-600")!),
    [],
    `the composite bound must not treat a saturated fill as a surface: ` +
      `${JSON.stringify(translucentFailures("bg-red-500/10", PALETTE.get("red-600")!))}`,
  );
  const abstained = findingsIn(
    "fixture.tsx",
    [
      "export function P() {",
      "  return (",
      '    <div className="jf-card">',
      '      <p className="rounded bg-red-500/10 text-red-600">Failed</p>',
      "    </div>",
      "  );",
      "}",
      "",
    ].join("\n"),
  );
  assert.deepEqual(
    abstained,
    [],
    `a text-red-600 on bg-red-500/10 is the documented abstention; if the guard started ` +
      `reporting one, the abstention is no longer the documented one: ${JSON.stringify(abstained)}`,
  );
});

test("a translucent WHITE surface is not a light tint and is left to the browser", () => {
  // `bg-white/95` (the builder header) is not in LIGHT_SURFACE_RE — a translucent
  // WHITE is a different shape from a light TINT, and the guard abstains rather
  // than compositing it as if it were one. Pinned so the abstention is recorded.
  assert.deepEqual(
    translucentFailures("bg-white/95", PALETTE.get("red-600")!),
    [],
    `bg-white/95 must not be judged as a light tint: ${JSON.stringify(translucentFailures("bg-white/95", PALETTE.get("red-600")!))}`,
  );
});

// ─── the inherited shape, against a real defect this rule is FOR ─────────────
test("a forbidden token is reported when an inline child paints the surface", () => {
  const src = [
    "export function P() {",
    "  return (",
    '    <div className="jf-card">',
    '      <p className="text-red-700">Careful <code className="bg-slate-100">ID</code></p>',
    "    </div>",
    "  );",
    "}",
    "",
  ].join("\n");
  // red-700 on the card's white is fine, but it is 5.86:1 on slate-100 — still
  // passing, so this fixture asserts the rule does NOT fire on a legal pairing…
  assert.deepEqual(findingsIn("fixture.tsx", src), []);

  // …and does fire once the inherited surface genuinely fails. `text-red-500` on
  // white is 3.81:1: below AA before any tint is involved.
  const bad = src.replace("text-red-700", "text-red-500");
  const findings = findingsIn("fixture.tsx", bad);
  assert.ok(
    findings.some((f) => f.includes("text-red-500")),
    `the forbidden red token stopped being reported: ${JSON.stringify(findings)}`,
  );
});
