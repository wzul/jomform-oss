// Repo-wide guard: the ink on a `.jf-btn-ghost` button must clear 4.5:1 on the
// surfaces THAT BUTTON ITSELF paints — not on the white it happens to sit on.
//
// WHAT WAS WRONG (measured in a real browser on this worktree, not reasoned
// about — the ratios below are re-derived from the installed theme and pinned in
// `e2e/a11y-subscriptions-row.spec.ts` from a real render)
//
//   `.jf-btn-ghost` is not a transparent button: `globals.css` gives it three
//   backgrounds of its own,
//
//     .jf-btn-ghost                       { background: #fff     }
//     .jf-btn-ghost:hover:not(:disabled)  { background: #f3f4f6 }
//     .jf-btn-ghost:active:not(:disabled) { background: #e5e7eb }
//
//   so the surface a destructive label is painted on MOVES when the pointer
//   arrives. Eleven sites painted that label one step too light:
//
//     text-rose-600 #ec003f   rest 4.527:1   hover #f3f4f6 4.114:1   active #e5e7eb 3.657:1
//     text-red-600  #e7000b   rest 4.770:1   hover #f3f4f6 4.334:1   active #e5e7eb 3.853:1
//
//   Both PASS at rest on the button's own white and BOTH fail the moment the
//   button is hovered — the state every pointer passes through before it clicks
//   "Cancel" / "Remove" / "Delete". On `/subscriptions` the two buttons on the
//   SAME row measured, before this change, `5.856:1` (the sky update-card button,
//   card t_0fdbe189's named node) next to `4.527:1` at rest and `4.114:1` HOVERED
//   (the rose cancel button beside it).
//
// WHY NO EXISTING GATE SAW IT — the blind spot this file owns
//
//   `dangerText.test.ts` owns the red/sky family and derives each step's bad
//   surface set from the installed theme, but it attributes a token to a surface
//   it can SEE: a `bg-*` utility in the element's class list, the nearest surface
//   ancestor, or the page background. `.jf-btn-ghost` declares its background in
//   a CSS RULE, not a utility, so that guard resolved the ambient surface (a white
//   card), read 4.527 / 4.770 and abstained — for eleven real sites, with no
//   finding. `mutedText.test.ts` has the same blind spot for the same reason, and
//   it is filed separately (a `text-slate-500` ghost button measures 4.329:1 on
//   this same hover surface; the GRAY family is that guard's, not this one's).
//   axe cannot see it either: it judges the state that is RENDERED, and a
//   resting scan never hovers a button.
//
// THE FIX, AND WHY IT IS THE 700 STEP
//
//   The replacement step is COMPUTED here, not remembered: for the failing
//   token's own hue, the lightest step whose worst ratio over the three ghost
//   surfaces still clears 4.5:1. That is 700 for red, rose and sky — and it is
//   not a new choice either way: `text-red-700` is already the ink of the two
//   red ghost buttons in `form-settings`, `text-rose-700` is the
//   card-needs-update chip on `/subscriptions`, and `text-sky-700` is that page's
//   own update-card button. The hue survives, so the destructive semantics
//   survive, and — as `--jf-danger-text` already established — a colour that is
//   only ever a TEXT colour must be the darker step.
//
// OWNERSHIP (stated so no two guards judge the same class)
//   * dangerText.test.ts   owns the RED/SKY family on the surfaces it can see
//                          (a `bg-*` utility, an ancestor surface, the page bg).
//   * mutedText.test.ts    owns the muted GRAY family (`--jf-muted`,
//                          `text-slate-400/500`, literal `#9ca3af`).
//   * emeraldText.test.ts  owns the EMERALD/success family.
//   * THIS FILE owns the ink on a `.jf-btn-ghost` ELEMENT, judged against the
//                          three surfaces `.jf-btn-ghost` itself paints, for the
//                          red/rose/sky family. It is the rule that makes the
//                          eleven sites above visible at all; after the census no
//                          site is reported by both this file and dangerText,
//                          because a converted site is clean on every surface
//                          either guard can see.
//
// WHY THE SURFACES ARE READ FROM THE STYLESHEET, NOT REMEMBERED
//
//   The same reason the palette is: any of the three could change, and a guard
//   holding a hand-written `#f3f4f6` would keep passing about a pixel the app no
//   longer paints. The three rules are parsed out of `globals.css`, and a missing
//   or unparseable rule is a FAILURE, never a skip.
//
//   ONE CONSEQUENCE HAD TO BE SAID OUT LOUD, because it is why the sites above
//   were written the way they were: those buttons ALSO carried Tailwind's
//   `hover:bg-rose-50` / `hover:bg-sky-50`, and it had NO EFFECT. Tailwind v4
//   puts its utilities in `@layer utilities`, and `.jf-btn-ghost:hover:not(:disabled)`
//   is written OUTSIDE any layer, so the unlayered rule wins the cascade
//   regardless of specificity. Measured in Chromium on the built app: the hovered
//   pixel of that sky button is `#f3f4f6`, not the `#f0f9ff` its `hover:bg-sky-50`
//   named (`e2e/a11y-subscriptions-row.spec.ts` pins the pixel). A site whose
//   author believed the hover surface was `rose-50` / `sky-50` would have measured
//   4.121:1 / 3.661:1 — wrong in the same direction — so this guard judges the
//   surfaces the CSS rule actually paints.
//
//   Those nine dead `hover:bg-*` utilities have since been DELETED (card
//   t_ac414593), and the arm at the bottom of this file keeps them out: a `bg-*`
//   utility on a ghost button cannot win this cascade, so writing one is a claim
//   about a pixel that will never be painted. Deleting was chosen over moving the
//   `.jf-*` rules into `@layer components` because that move would also activate
//   every OTHER utility these buttons write (padding, font-size, min-height) and
//   resize 84 of the 106 ghost elements in `web/src` — a different change, with
//   its own re-derivation, not a cleanup.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { codeOnly, jsxTags, tsxFiles, type Tag } from "./jsxSource.ts";

const SRC = fileURLToPath(new URL("../", import.meta.url)); // web/src/

/** Read a `web/`-relative file; the base is `import.meta.url`, as the suite requires. */
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
const hexOf = (rgb: RGB) => "#" + rgb.map((v) => v.toString(16).padStart(2, "0")).join("");

/**
 * Tailwind v4's palette is OKLCH, so the sweep resolves OKLCH to the sRGB a
 * browser actually paints (Björn Ottosson's OKLab -> linear sRGB, then the sRGB
 * transfer function). The block is deliberately local to this file, as
 * `dangerText.test.ts` and `emeraldText.test.ts` each keep their own: a helper
 * defect here cannot blind another guard.
 */
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

/**
 * Tailwind's palette, read from the INSTALLED theme rather than remembered.
 * The path is `../../node_modules/tailwindcss/theme.css` from `web/src/lib`, i.e.
 * `web/node_modules/tailwindcss/theme.css`, which `web/Dockerfile`'s build stage
 * really has (`npm ci` with `WORKDIR /repo/web`) — `buildContext.test.ts` resolves
 * this literal and checks exactly that, so the read is valid in the image too.
 */
function tailwindPalette(): Map<string, RGB> {
  const theme = read("../../node_modules/tailwindcss/theme.css");
  const out = new Map<string, RGB>();
  for (const m of theme.matchAll(/--color-([a-z0-9-]+):\s*([^;]+);/g)) {
    const ok = /^oklch\(\s*([\d.]+)%\s+([\d.]+)\s+([\d.]+)/.exec(m[2].trim());
    if (!ok) continue;
    out.set(m[1], oklchToRgb(parseFloat(ok[1]) / 100, parseFloat(ok[2]), parseFloat(ok[3])));
  }
  return out;
}

const PALETTE = tailwindPalette();

// ─── the surfaces `.jf-btn-ghost` itself paints, read from globals.css ──────

/** `#rgb` / `#rrggbb`, or a `var(--jf-*)` token declared in the same stylesheet. */
function resolveColor(value: string, tokens: Map<string, RGB>): RGB | null {
  const v = value.trim();
  const tok = /^var\(\s*--([a-z0-9-]+)\s*\)$/.exec(v);
  if (tok) return tokens.get(tok[1]) ?? null;
  const h = /^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/.exec(v);
  if (!h) return null;
  const s = h[1].length === 3 ? h[1].split("").map((c) => c + c).join("") : h[1];
  return [0, 2, 4].map((i) => parseInt(s.slice(i, i + 2), 16)) as RGB;
}

type GhostState = "" | "hover" | "active";
type GhostSurface = { state: GhostState; rule: string; rgb: RGB };

/** A state and the class-rule that paints it, in the order a pointer visits them. */
const WANTED: Array<{ state: GhostState; rule: string; re: RegExp }> = [
  { state: "", rule: ".jf-btn-ghost", re: /(?:^|\n)\.jf-btn-ghost\s*\{([^}]*)\}/ },
  {
    state: "hover",
    rule: ".jf-btn-ghost:hover:not(:disabled)",
    re: /\.jf-btn-ghost:hover:not\(:disabled\)\s*\{([^}]*)\}/,
  },
  {
    state: "active",
    rule: ".jf-btn-ghost:active:not(:disabled)",
    re: /\.jf-btn-ghost:active:not\(:disabled\)\s*\{([^}]*)\}/,
  },
];

/**
 * The three backgrounds `.jf-btn-ghost` declares, keyed by the state they apply
 * in. A rule that is missing, or whose `background` cannot be resolved, throws:
 * an unreadable stylesheet must fail loudly rather than shrink the guard.
 */
function ghostSurfaces(): GhostSurface[] {
  const css = read("../app/globals.css");
  const tokens = new Map<string, RGB>();
  for (const m of css.matchAll(/--(jf-[a-z0-9-]+):\s*(#[0-9a-fA-F]{3,6})\s*;/g)) {
    const rgb = resolveColor(m[2], new Map());
    if (rgb) tokens.set(m[1], rgb);
  }
  return WANTED.map(({ state, rule, re }) => {
    const m = re.exec(css);
    assert.ok(m, `globals.css no longer declares \`${rule}\` — re-derive this guard, do not delete it`);
    const bg = /background(?:-color)?:\s*([^;]+);/.exec(m![1]);
    assert.ok(bg, `\`${rule}\` declares no background — this guard cannot judge a surface it cannot read`);
    const rgb = resolveColor(bg![1], tokens);
    assert.ok(rgb, `\`${rule}\` background \`${bg![1].trim()}\` did not resolve to a colour`);
    return { state, rule, rgb: rgb! };
  });
}

const GHOST = ghostSurfaces();
/** The class that puts these surfaces under a label. */
const GHOST_CLASS = "jf-btn-ghost";
const AA_FLOOR = 4.5;

const STATE_RE = /^((?:[a-z-]+:)*)/;
function splitState(cls: string): [string, string] {
  const m = STATE_RE.exec(cls)!;
  return [m[1], cls.slice(m[1].length)];
}

/**
 * The state prefixes `splitState` can return, as the pseudo-classes they are.
 * A breakpoint (`sm:`) is not a state a colour can differ in for this rule — it
 * is still the resting colour — so only these segments count.
 */
const PSEUDO = new Set(["hover", "focus", "focus-visible", "active", "visited", "disabled", "checked"]);

/**
 * The red/rose/sky TEXT token an element carries in `pseudo` ("" = the resting
 * state, i.e. a class list with no pseudo-class prefix at all).
 *
 * The prefix vocabulary comes from `jsxSource.splitState`, which returns the
 * state WITH its colon and keeps every variant (`sm:hover:`), so the match is on
 * the colon-separated segments rather than on string equality — a `sm:` in front
 * of a `hover:` must not hide the token from this rule.
 */
function tokenInState(tag: Tag, pseudo: GhostState): string {
  for (const v of tag.variants) {
    for (const cls of v.split(/\s+/)) {
      const [pre, bareCls] = splitState(cls);
      if (!bareCls || !/^text-(?:red|rose|sky)-\d+$/.test(bareCls)) continue;
      const segs = pre.split(":").filter(Boolean);
      const isRest = !segs.some((s) => PSEUDO.has(s));
      if (pseudo === "" ? isRest : segs.includes(pseudo)) return bareCls;
    }
  }
  return "";
}

function isGhost(tag: Tag): boolean {
  return tag.variants.some((v) =>
    v.split(/\s+/).some((cls) => {
      const [, bareCls] = splitState(cls);
      return bareCls === GHOST_CLASS;
    }),
  );
}

/**
 * The lightest step of `hue` that clears AA on ALL three ghost surfaces — the
 * replacement this guard recommends, computed from the installed theme rather
 * than remembered, and the LIGHTEST such step so the visual hierarchy is kept.
 */
function safeStep(hue: string): { token: string; rgb: RGB; worst: number } | null {
  let best: { token: string; rgb: RGB; worst: number } | null = null;
  for (const [name, rgb] of PALETTE) {
    if (!new RegExp(`^${hue}-\\d+$`).test(name)) continue;
    const worst = Math.min(...GHOST.map((g) => contrast(rgb, g.rgb)));
    if (worst < AA_FLOOR) continue;
    if (!best || luminance(rgb) > luminance(best.rgb)) best = { token: `text-${name}`, rgb, worst };
  }
  return best;
}

/**
 * The colour that is actually on the text in `state`: the state's own token, or
 * — because a pressed button is still hovered, and an unhovered button is what a
 * resting token is read on — the nearest ancestor state's token, else the
 * resting one. This is CSS's own inheritance, stated rather than assumed.
 */
function effectiveToken(tag: Tag, state: GhostState, resting: string): string {
  const own = tokenInState(tag, state);
  if (own) return own;
  if (state === "active") return tokenInState(tag, "hover") || resting;
  return resting;
}

/**
 * Every `.jf-btn-ghost` element whose label cannot clear AA on one of the three
 * surfaces the button paints, as `file:line — explanation`.
 *
 * One finding per element and token, naming its WORST surface but listing all
 * three, because the resting surface is the one that PASSES and a finding that
 * named only the failing ratio would not show that the button is fine until the
 * pointer arrives.
 */
function findingsIn(rel: string, src: string): string[] {
  const code = codeOnly(src);
  const events = jsxTags(code);
  const lineAt = (idx: number) => code.slice(0, idx).split("\n").length;
  const out: string[] = [];

  for (const e of events) {
    const tag = e.tag;
    if (!isGhost(tag)) continue;
    const resting = tokenInState(tag, "");
    if (!resting) continue;

    const rows: Array<{ surface: GhostSurface; token: string; rgb: RGB; ratio: number }> = [];
    for (const surface of GHOST) {
      const token = effectiveToken(tag, surface.state, resting);
      const rgb = PALETTE.get(token.replace(/^text-/, ""));
      if (!rgb) continue; // a step the installed theme does not declare
      rows.push({ surface, token, rgb, ratio: contrast(rgb, surface.rgb) });
    }
    const worst = rows.reduce((a, b) => (b.ratio < a.ratio ? b : a), rows[0]);
    if (!worst || worst.ratio >= AA_FLOOR) continue;

    const safe = safeStep(worst.token.replace(/^text-([a-z]+)-\d+$/, "$1"));
    const table = rows
      .map(
        (r) =>
          `${r.surface.state === "" ? "rest" : `:${r.surface.state}`} ${hexOf(r.surface.rgb)} ` +
          `${r.ratio.toFixed(3)}:1`,
      )
      .join(", ");
    out.push(
      `${rel}:${lineAt(tag.openIdx)} — ${worst.token} (${hexOf(worst.rgb)}) on a \`${GHOST_CLASS}\` button ` +
        `measures ${worst.ratio.toFixed(3)}:1 on \`${worst.surface.rule}\` (${hexOf(worst.surface.rgb)}), ` +
        `under the ${AA_FLOOR}:1 AA floor — its worst of the three surfaces the button paints (${table}). ` +
        (safe
          ? `Use ${safe.token} (${hexOf(safe.rgb)}, worst ghost surface ${safe.worst.toFixed(3)}:1).`
          : `No step of that hue clears all three surfaces; re-colour the site.`),
    );
  }
  return [...new Set(out)];
}

const FILES = tsxFiles(SRC);
function allFindings(): string[] {
  const out: string[] = [];
  for (const rel of FILES) out.push(...findingsIn(rel, readFileSync(`${SRC}${rel}`, "utf8")));
  return out;
}

test("no red/rose/sky label on a .jf-btn-ghost button is under AA on any surface it paints", () => {
  const findings = allFindings();
  assert.deepEqual(
    findings,
    [],
    `${findings.length} ghost-button label(s) are below the ${AA_FLOOR}:1 AA floor on a surface the ` +
      `button itself paints:\n  ` +
      findings.join("\n  ") +
      `\nThe surfaces are read from globals.css (\`${GHOST_CLASS}\`, its :hover and its :active) and the ` +
      `resting colour is judged on all three, because the label does not change when the pointer arrives.`,
  );
});

test("the rule is looking at real, reachable markup, and it judges every state", () => {
  // A guard whose subject never renders passes vacuously. Three legs:
  //  (a) the walk found ghost buttons at all;
  //  (b) the family tokens the app paints on them are actually seen;
  //  (c) the three surfaces were READ from globals.css, not defaulted.
  let ghosts = 0;
  let familyTokens = 0;
  for (const rel of FILES) {
    for (const e of jsxTags(codeOnly(readFileSync(`${SRC}${rel}`, "utf8")))) {
      if (!isGhost(e.tag)) continue;
      ghosts += 1;
      if (tokenInState(e.tag, "")) familyTokens += 1;
    }
  }
  assert.ok(ghosts >= 50, `the walk found only ${ghosts} .jf-btn-ghost elements in web/src`);
  assert.ok(
    familyTokens >= 5,
    `the walk found only ${familyTokens} ghost buttons carrying a red/rose/sky label — this rule is no ` +
      `longer looking at the sites it was written for, so re-anchor it rather than letting it pass`,
  );
  assert.deepEqual(
    GHOST.map((g) => `${g.state || "rest"} ${hexOf(g.rgb)}`),
    ["rest #ffffff", "hover #f3f4f6", "active #e5e7eb"],
    "the three surfaces .jf-btn-ghost paints have MOVED. Re-derive this guard (and the values quoted in " +
      "its header) and re-run e2e/a11y-subscriptions-row.spec.ts, which pins the hovered pixel in a browser.",
  );
  // The recommendation is computed too: if the palette darkens so that 600 passes,
  // the guard must stop recommending 700 rather than keep naming a stale step.
  const rose = safeStep("rose");
  assert.ok(rose, "no step of the rose hue clears the three ghost surfaces — the fix is no longer expressible");
  assert.equal(rose!.token, "text-rose-700", "the computed safe rose step moved");
});

test("a sub-AA label on a ghost button is REPORTED — the defect this file closes", () => {
  // The fixture is the defect itself, at the site it was measured at: the
  // /subscriptions cancel button one step too light. It must be found, with the
  // surfaces NAMED — including the hover one, whose 4.114:1 is the ratio the
  // browser measured on the built page while the resting surface PASSES.
  const fixture =
    `export const X = () => (\n` +
    `  <button className="jf-btn-ghost px-2.5 py-1 text-xs text-rose-600 hover:bg-rose-50">\n` +
    `    {t("sub.cancel")}\n` +
    `  </button>\n` +
    `);\n`;
  const found = findingsIn("fixture.tsx", fixture);
  assert.equal(found.length, 1, `expected one finding, got ${found.length}: ${found.join(" | ")}`);
  assert.match(found[0], /text-rose-600/);
  assert.match(found[0], /4\.114:1/, `the hovered ratio must be quoted: ${found[0]}`);
  assert.match(found[0], /3\.657:1/, `the worst (pressed) ratio must be quoted: ${found[0]}`);
  assert.match(
    found[0],
    /\.jf-btn-ghost:active:not\(:disabled\)/,
    `the finding must name the surface it failed worst on: ${found[0]}`,
  );
  assert.match(found[0], /text-rose-700/, "the finding must name the computed safe step");
  assert.match(found[0], /fixture\.tsx:2/, "the finding must carry file:line of the element");

  // …and the dead `hover:bg-rose-50` on that element must NOT be what the guard
  // judged: the hovered pixel is the CSS rule's, which is the whole point.
  assert.doesNotMatch(found[0], /rose-50/);

  // The same label on a NON-ghost element is not this rule's business: the
  // /products editor renders it as an opaque white pill, which clears AA at rest.
  const pill =
    `export const Y = () => (\n` +
    `  <button className="rounded-full bg-white px-1.5 text-xs text-rose-600">x</button>\n` +
    `);\n`;
  assert.deepEqual(findingsIn("pill.tsx", pill), []);

  // The red step behaves the same way, so the rule is about the PAIRING and not
  // about one hue: `text-red-600` in a ghost button fails the hover surface.
  const red =
    `export const R = () => <button className="jf-btn-ghost text-[11px] text-red-600">Remove</button>;\n`;
  const redFound = findingsIn("red.tsx", red);
  assert.equal(redFound.length, 1, `expected one finding, got ${redFound.length}`);
  assert.match(redFound[0], /4\.334:1/);
});

test("a PASSING label is left alone in every state, and a hover override is honoured", () => {
  // The step the census converted the eleven sites to.
  const ok = `export const Z = () => <button className="jf-btn-ghost text-xs text-rose-700">Cancel</button>;\n`;
  assert.deepEqual(findingsIn("ok.tsx", ok), []);

  // A `hover:text-*` override replaces the colour in the hover state, so a light
  // resting step the site darkens on hover is judged on what those states paint.
  // A guard that ignored the override would report this passing button.
  const overridden =
    `export const W = () => (\n` +
    `  <button className="jf-btn-ghost text-xs text-rose-600 hover:text-rose-700">Cancel</button>\n` +
    `);\n`;
  assert.deepEqual(
    findingsIn("over.tsx", overridden),
    [],
    "a hover override that clears AA must not be reported",
  );

  // …and the override is READ, not assumed: the same shape with a light hover
  // step is a defect, attributed to the hover surface.
  const badOverride =
    `export const V = () => (\n` +
    `  <button className="jf-btn-ghost text-xs text-rose-700 hover:text-rose-500">Cancel</button>\n` +
    `);\n`;
  const two = findingsIn("bad-over.tsx", badOverride);
  assert.equal(two.length, 1, `expected one finding, got ${two.length}: ${two.join(" | ")}`);
  assert.match(two[0], /text-rose-500/);
  assert.match(two[0], /:hover/);
});

// ─── a `bg-*` utility on a ghost button cannot paint: it is DEAD ────────────
//
// `.jf-btn-ghost` paints its own background from an UNLAYERED rule, and Tailwind
// v4 emits every utility inside `@layer utilities`. In the cascade an unlayered
// declaration beats every layer REGARDLESS of specificity, so a background
// utility on a ghost button loses in every state — `hover:bg-rose-50` no more
// than `bg-white`. Measured in Chromium on the built app: with the pointer on
// `/subscriptions`' cancel button the painted surface was `#f3f4f6` (the CSS
// rule's), never the `#fff1f2` the class list named; `e2e/a11y-subscriptions-row.spec.ts`
// pins that pixel.
//
// Nine sites carried one and this card deleted them. The reason it is a guard and
// not a one-off cleanup: reading the hover surface off the class list is how
// eleven sub-AA labels reached the tree behind four green colour guards and a
// green axe gate (card t_0fdbe189). The next author to write `hover:bg-<tint>` on
// a ghost button is entitled to know it cannot apply BEFORE they measure a pixel
// that will never be painted.
const BACKGROUND_UTILITY = /^bg-/;

/**
 * Every `bg-*` utility found on a `.jf-btn-ghost` element, as `file:line — tokens`.
 *
 * Self-closing elements (`<button … />`) arrive only as a CLOSE event from the
 * scanner, so every event is walked and deduped by `openIdx`: filtering on
 * `event.open` would silently ignore half the shapes a button is written in.
 */
function deadBackgroundsIn(rel: string, src: string): string[] {
  const code = codeOnly(src);
  const lineAt = (idx: number) => code.slice(0, idx).split("\n").length;
  const seen = new Set<number>();
  const out: string[] = [];
  for (const e of jsxTags(code)) {
    const tag = e.tag;
    if (seen.has(tag.openIdx) || !isGhost(tag)) continue;
    seen.add(tag.openIdx);
    if (tag.classNameUnreadable) {
      out.push(
        `${rel}:${lineAt(tag.openIdx)} — this ghost button's class list could not be read, so this ` +
          `arm cannot tell whether it carries a dead \`bg-*\` utility; inline the class list here or ` +
          `extend \`jsxSource.resolveClassExpr\` rather than letting the element go unjudged`,
      );
      continue;
    }
    const bad = tag.variants
      .flatMap((v) => v.split(/\s+/))
      .filter((cls) => cls.length > 0 && BACKGROUND_UTILITY.test(splitState(cls)[1]));
    if (bad.length > 0) out.push(`${rel}:${lineAt(tag.openIdx)} — ${bad.join(" ")}`);
  }
  return out;
}

test("no `.jf-btn-ghost` element carries a `bg-*` utility — the unlayered rule paints it", () => {
  const findings = FILES.flatMap((rel) => deadBackgroundsIn(rel, readFileSync(`${SRC}${rel}`, "utf8")));
  assert.deepEqual(
    findings,
    [],
    `${findings.length} background utilit(ies) on a \`${GHOST_CLASS}\` button cannot ever paint:\n  ` +
      findings.join("\n  ") +
      `\n\`${GHOST_CLASS}\` (and its :hover / :active) declare their backgrounds OUTSIDE any ` +
      `@layer in globals.css, while Tailwind v4 emits utilities inside \`@layer utilities\`, and ` +
      `an unlayered declaration beats every layer regardless of specificity. Delete the utility ` +
      `(the button's own hover/pressed colour is the CSS rule's) — or, if the tint is genuinely ` +
      `wanted, move the \`.jf-*\` rules into \`@layer components\` and re-derive every guard that ` +
      `reads the ghost surfaces, including the pixel pinned in e2e/a11y-subscriptions-row.spec.ts.`,
  );
  // Coverage: the arm has to be looking at ghosts at all, or it passes vacuously.
  let ghosts = 0;
  for (const rel of FILES) {
    const seen = new Set<number>();
    for (const e of jsxTags(codeOnly(readFileSync(`${SRC}${rel}`, "utf8")))) {
      if (seen.has(e.tag.openIdx) || !isGhost(e.tag)) continue;
      seen.add(e.tag.openIdx);
      ghosts += 1;
    }
  }
  assert.ok(ghosts >= 50, `the walk found only ${ghosts} .jf-btn-ghost elements in web/src`);
});

test("a `bg-*` utility on a ghost button is REPORTED — the defect this arm keeps out", () => {
  const fixture =
    `export const A = () => (\n` +
    `  <button className="jf-btn-ghost text-rose-700 hover:bg-rose-50">Cancel</button>\n` +
    `);\n` +
    `export const B = () => <button className="jf-btn-ghost text-rose-700 bg-white" />;\n` +
    `export const C = () => <button className="rounded bg-white hover:bg-rose-50">Not a ghost</button>;\n`;
  const found = deadBackgroundsIn("fixture.tsx", fixture);
  assert.equal(found.length, 2, `expected two findings, got ${found.length}: ${found.join(" | ")}`);
  // The exact shape the nine deleted sites had…
  assert.match(found[0], /fixture\.tsx:2/);
  assert.match(found[0], /hover:bg-rose-50/);
  // …and a self-closing element, which the scanner reports only as a close event.
  assert.match(found[1], /fixture\.tsx:4/);
  assert.match(found[1], /bg-white/);
  // A non-ghost element's background utility is that element's business, not this rule's.
  assert.doesNotMatch(found.join(" "), /Not a ghost|rounded/);
});
