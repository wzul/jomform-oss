// The CTA's hover label must clear AA (card t_9efae9f0).
//
// THE DEFECT, AND WHY IT NEEDED ITS OWN CARD
//   `.jf-btn-primary:hover:not(:disabled)` swapped its fill to
//   `--jf-primary-hover` and the class hard-codes its label white:
//
//       label #ffffff on hover  #828fff  ->  2.87:1   FAILS AA (white on rest
//                                                      #5e6ad2 is 4.70:1, on
//                                                      pressed #454ea8 7.22:1)
//
//   It is PRE-EXISTING on main (measured byte-identical on the v1.20.17 tree,
//   92261cc7) — card t_b73eee21 left `--jf-primary-hover` alone on purpose,
//   because a token darkens every button and link that inherits it and that is a
//   repo-wide decision, not a presentation card's. That is why the hover state
//   is carded separately (this card) rather than folded into #987.
//
//   What made it matter: #987 is what turned this class into the app's real CTA,
//   so the hover is now on 198 `jf-btn*` occurrences across 26 source files, and
//   the authenticated-route axe gate cannot see a hover state at all — axe reads
//   the page in its resting state. Nothing else in the repo would have caught it.
//
// THE FIX THIS FILE GUARDS
//   `--jf-primary-hover` is now `#616dd5` (label 4.51:1 — the LIGHTEST step on the
//   #5e6ad2 hue whose white label still clears AA), and the twelve hand-written
//   `hover:bg-[#828fff]` buttons — the identical pair, on the login / register /
//   reset-password / magic-link / connect / workspace-picker / sales CTAs — now
//   use the token. The Go-rendered public form carried the same hard-coded pair
//   and is fixed in `api/internal/httpapi/render_{custom,blocks}.go`.
//
// WHAT IS PINNED, AND THE LIMIT OF A SOURCE SCAN
//   A source scan cannot measure a computed style, and this repo has no DOM test
//   runner (see src/lib/i18n.test.ts). What it CAN do is:
//     1. compute the ratio the card measured, from the two hex values actually in
//        `:root`, and refuse a value that does not clear 4.5:1;
//     2. keep the three states in their intended ORDER (hover LIGHTER than rest,
//        pressed darker) so a "fix" cannot invert the ladder;
//     3. sweep every `.tsx` for the whole class — a fill painted under a WHITE
//        label, in the same state — instead of this one call site, so the next
//        hand-written button is caught by the rule and not by review;
//     4. refuse the literal that has no AA-safe use here (`#828fff` as a fill).
//
//   STATED LIMIT (measured, not hypothetical): this sweep judges only pairings
//   whose fill it can RESOLVE (a literal, a `--jf-*` token, `white`/`black`, or a
//   Tailwind palette colour read from the installed theme) and only when the
//   label is white. Four OTHER sub-AA hover pairs exist in the tree and are NOT
//   this card's defect and are NOT judged here — they carry a coloured label, not
//   a white one, and each is a separate finding:
//       components/Shell.tsx:369      text-[--jf-muted] hover:text-red-500 on slate-50   3.64
//       components/WorkspaceSwitcher.tsx:253  same shape                                 3.64
//       components/WorkspaceSwitcher.tsx:265  hover:text-[--jf-primary] on slate-50     4.49
//       media / products / templates  text-red-600 hover:bg-red-50                      4.36
//       templates / TemplatePicker    text-[--jf-muted] hover:bg-slate-100              4.41
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, existsSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { tsxFiles } from "./jsxSource.ts";

// The base this file reads `web/src` under, written as a `new URL("<lit>", import.meta.url)`
// constant on purpose: `src/lib/buildContext.test.ts` scans this suite for literal
// read targets and resolves them through exactly this shape, so a read expressed
// any other way is either invisible to it or reported as unresolved. It resolves
// to `web/src`, which the web image really has (web/Dockerfile runs `npm ci` with
// WORKDIR /repo/web).
const SRC = new URL("../", import.meta.url); // web/src/

const srcAt = (rel: string) => readFileSync(fileURLToPath(new URL(rel, SRC)), "utf8");

/**
 * Read a `web/`-relative file.
 *
 * `read` with the literal `new URL(rel, import.meta.url)` base is the helper shape
 * buildContext.test.ts resolves; a `read("../../<path>")` call is therefore
 * checked against what web/Dockerfile copies, which is the point.
 */
const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

// ─── WCAG 2.x relative luminance, from the spec ─────────────────────────────
// https://www.w3.org/TR/WCAG21/#dfn-relative-luminance
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

type RGB = [number, number, number];

function hexToRgb(hex: string): RGB | null {
  const h = hex.replace("#", "");
  const full = h.length === 3 ? h.split("").map((c) => c + c).join("") : h;
  if (!/^[0-9a-fA-F]{6}$/.test(full)) return null;
  return [0, 2, 4].map((i) => parseInt(full.slice(i, i + 2), 16)) as RGB;
}

// Tailwind v4's palette is OKLCH, so the sweep must resolve OKLCH to the sRGB the
// browser actually paints — reading the theme file, not a remembered hex. This is
// Björn Ottosson's OKLab -> linear sRGB, followed by the sRGB transfer function.
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

// ─── the theme: `--jf-*` tokens and the Tailwind palette ────────────────────
const css = srcAt("app/globals.css");

/** `--jf-*` colour tokens declared in `:root` of globals.css. */
function jfTokens(): Map<string, RGB> {
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
 * A remembered hex is how a sweep silently stops judging: this repo's Tailwind is
 * v4, whose palette is OKLCH (slate-500 is oklch(55.4% 0.046 257.417) = #62748e,
 * not the v3 #64748b this file's first draft assumed). The conversion below is
 * pinned by the test's own non-vacuity case against two values this repo already
 * measured and wrote into src/lib/mutedText.test.ts: slate-400 = #90a1b9 and
 * slate-500 = #62748e.
 */
function tailwindPalette(): Map<string, RGB> {
  // Resolve the palette from the INSTALLED Tailwind theme. `import.meta.url` is
  // this FILE (`web/src/lib/`), so the install is two levels up — and
  // `buildContext.test.ts` resolves this literal to `web/node_modules/…`, which
  // web/Dockerfile really has (its build stage runs `npm ci` with WORKDIR
  // /repo/web), rather than refusing a read the image cannot satisfy.
  const themeRel = "../../node_modules/tailwindcss/theme.css";
  assert.ok(
    existsSync(fileURLToPath(new URL(themeRel, import.meta.url))),
    `no Tailwind theme at web/node_modules/tailwindcss/theme.css — this guard resolves palette ` +
      `colours from the installed theme because the v4 palette is OKLCH, and it must not fall ` +
      `back to remembered hexes. Run \`npm run test:unit\` (its web-deps-check seeds the install).`,
  );
  const theme = read(themeRel);
  const out = new Map<string, RGB>();
  for (const m of theme.matchAll(/--color-([a-z0-9-]+):\s*([^;]+);/g)) {
    const name = m[1], value = m[2].trim();
    let rgb: RGB | null = null;
    const hex = /^#([0-9a-fA-F]{3,6})$/.exec(value);
    const ok = /^oklch\(\s*([\d.]+)%?\s+([\d.]+)\s+([\d.]+)/.exec(value);
    if (hex) {
      const h = hex[1];
      rgb = hexToRgb(h.length === 1 || h.length === 2 ? `#${h}` : `#${h}`);
    } else if (ok) {
      const L = parseFloat(ok[1]) / (ok[1].includes("%") || parseFloat(ok[1]) > 1 ? 100 : 1);
      rgb = oklchToRgb(L, parseFloat(ok[2]), parseFloat(ok[3]));
    }
    if (rgb) out.set(name, rgb);
  }
  return out;
}

const TOKENS = jfTokens();
const PALETTE = tailwindPalette();

// ─── resolving one class-list token to the colour it paints ─────────────────
/**
 * A Tailwind class (possibly state-prefixed) that sets a fill or a text colour,
 * resolved to the sRGB it paints — or null when this guard cannot see it.
 *
 * `null` is not a pass: it means the pair is outside what a source scan can
 * judge, and it is deliberately NOT an offender. The shapes the sweep cannot
 * resolve are a `bg-*` utility outside the theme (an arbitrary gradient, an
 * inline style, an alpha modifier such as `bg-white/10` whose composite depends
 * on what is behind it) — the same call mutedText.test.ts makes for
 * translucent backgrounds.
 */
function resolveColour(kind: "bg" | "text", value: string): RGB | null {
  // `bg-[#5e6ad2]` / `text-[#fff]`
  let m = /^\[#([0-9a-fA-F]{3,6})\]$/.exec(value);
  if (m) return hexToRgb(`#${m[1]}`);
  // `bg-[var(--jf-primary)]`
  m = /^\[var\(--(jf-[a-z0-9-]+)\)\]$/.exec(value);
  if (m) return TOKENS.get(m[1]) ?? null;
  // `bg-white` / `text-white` (the label this whole guard is about)
  if (value === "white") return [255, 255, 255];
  if (value === "black") return [0, 0, 0];
  if (value === "current" || value === "transparent" || value === "inherit") return null;
  // any palette colour, but ONLY from the installed theme (no remembered hexes)
  return PALETTE.get(value) ?? null;
}

const STATE_RE = /^((?:[a-z-]+:)*)/;
function splitState(cls: string): [string, string] {
  const m = STATE_RE.exec(cls)!;
  return [m[1], cls.slice(m[1].length)];
}

/** A colour a class list paints in ONE state: the first `bg-*` / `text-*` it declares there. */
function fillIn(classes: string[], state: string, kind: "bg" | "text"): RGB | null {
  for (const cls of classes) {
    const [s, bare] = splitState(cls);
    // The RESTING state ("") is also the inherited default for every other state,
    // so a `text-white` with no prefix applies on hover too. A prefixed colour
    // only applies in its own state.
    if (s !== state && !(s === "" && state !== "")) continue;
    if (!bare.startsWith(`${kind}-`)) continue;
    const rgb = resolveColour(kind, bare.slice(kind.length + 1));
    if (rgb) return rgb;
  }
  return null;
}

// ─── every pairing in the app ───────────────────────────────────────────────
type Pairing = { file: string; line: number; state: string; fg: RGB; bg: RGB; cls: string };

function allPairings(): Pairing[] {
  const out: Pairing[] = [];
  for (const rel of tsxFiles(fileURLToPath(SRC))) {
    const src = srcAt(rel);
    const re = /className=(?:"([^"]*)"|\{`([^`]*)`\})/g;
    let m: RegExpExecArray | null;
    while ((m = re.exec(src))) {
      const body = m[1] ?? m[2] ?? "";
      const classes = body.split(/\s+/).filter(Boolean);
      // The states this class list paints a fill in: the resting one, plus every
      // state prefix that carries a `bg-*`.
      const states = new Set<string>([""]);
      for (const cls of classes) {
        const [s, bare] = splitState(cls);
        // `disabled:` is excluded: a disabled control is not a hover target, and
        // the class rules this guard is about are all `:not(:disabled)`.
        if (s.includes("disabled:")) continue;
        if (bare.startsWith("bg-") && s) states.add(s);
      }
      for (const state of states) {
        const bg = fillIn(classes, state, "bg");
        const fg = fillIn(classes, state, "text");
        if (!bg || !fg) continue;
        // only WHITE labels are this guard's defect class (see the header's
        // STATED LIMIT for the coloured-label pairs that are separate cards)
        if (!(fg[0] === 255 && fg[1] === 255 && fg[2] === 255)) continue;
        out.push({
          file: rel,
          line: src.slice(0, m.index).split("\n").length,
          state: state || "rest",
          fg,
          bg,
          cls: classes.filter((c) => /bg-/.test(c)).join(" "),
        });
      }
    }
  }
  return out;
}

// ─── 1. the token itself: what the card measured ────────────────────────────
test("the primary CTA's hover label clears AA (the card's 2.87:1 pairing is gone)", () => {
  const hover = TOKENS.get("jf-primary-hover");
  const primary = TOKENS.get("jf-primary");
  const active = TOKENS.get("jf-primary-active");
  assert.ok(hover && primary && active, "globals.css no longer declares the three primary-state tokens — re-anchor this guard");

  const white: RGB = [255, 255, 255];
  const onHover = contrast(white, hover!);
  assert.ok(
    onHover >= 4.5,
    `--jf-primary-hover does not clear AA against the white label the CTA hard-codes: ` +
      `${onHover.toFixed(2)}:1 (need 4.5:1). This is the card's defect: #828fff measured 2.87:1.`,
  );
  // The value itself, so the pair cannot drift back to the light fill while some
  // other edit keeps the ratio green by accident.
  assert.equal(
    css.match(/--jf-primary-hover:\s*(#[0-9a-fA-F]{3,6})\s*;/)?.[1]?.toLowerCase(),
    "#616dd5",
    "--jf-primary-hover moved off the measured #616dd5 (white 4.51:1) — re-measure before changing it",
  );
  assert.ok(contrast(white, primary!) >= 4.5, `--jf-primary's white label is ${contrast(white, primary!).toFixed(2)}:1, below AA`);
  assert.ok(contrast(white, active!) >= 4.5, `--jf-primary-active's white label is ${contrast(white, active!).toFixed(2)}:1, below AA`);
  // THE PREMISE OF THIS WHOLE GUARD: the shared class hard-codes a WHITE label and
  // paints the hover fill from this token. If either stops being true, the ratio
  // above is being computed about a pairing the app does not render.
  const resting = css.match(/\.jf-btn-primary\s*\{([^}]*)\}/)?.[1] ?? "";
  assert.match(
    resting.replace(/\s+/g, " "),
    /color: ?#fff/,
    ".jf-btn-primary no longer hard-codes the white label — re-anchor this guard, the ratio it " +
      "asserts is about a pairing the class does not render",
  );
  assert.match(
    css.match(/\.jf-btn-primary:hover:not\(:disabled\)\s*\{([^}]*)\}/)?.[1] ?? "",
    /background:\s*var\(--jf-primary-hover\)/,
    ".jf-btn-primary's hover fill is no longer var(--jf-primary-hover) — the token clearing AA " +
      "would prove nothing about what the button paints",
  );
});

test("the three primary states keep their order: hover lighter than rest, pressed darker", () => {
  // #987 chose this direction on purpose and said so: a press that is LIGHTER than
  // the hover is what made the press invisible, so the ladder is
  // rest -> (hover, lighter) -> (pressed, darker). A "fix" that reversed it would
  // still clear AA and still be a regression, so the order is asserted too.
  // Relative LUMINANCE is the readable proxy for "lighter" here, and it is also
  // the quantity the contrast ratio is built from, so the two move together.
  const L = (name: string) => luminance(TOKENS.get(name)!);
  assert.ok(
    L("jf-primary-hover") > L("jf-primary"),
    "the hover fill is no longer a LIGHTER step than the resting fill — #987's direction was " +
      "rest -> lighter hover -> darker press, and inverting it is what made a press invisible",
  );
  assert.ok(
    L("jf-primary") > L("jf-primary-active"),
    "the pressed fill is no longer darker than the resting fill — a lighter press is invisible, " +
      "which is the defect #987 fixed",
  );
});

test("the hover fill is the LIGHTEST step on the primary hue that still clears AA", () => {
  // Why this is pinned: the whole point of #616dd5 rather than a darker indigo is
  // that it keeps the design's "hover is a lighter step" while clearing AA. If
  // someone darkens it past the resting fill to make some other ratio pass, the
  // ladder assertion above catches the direction; this one catches the case where
  // the fill stays lighter but is pushed so far up the hue that a white label
  // stops clearing — i.e. the exact regression this card exists for. It asserts
  // the ratio is at least 4.5 and at most the resting fill's, both measured.
  const white: RGB = [255, 255, 255];
  const onHover = contrast(white, TOKENS.get("jf-primary-hover")!);
  assert.ok(onHover >= 4.5, `hover label ${onHover.toFixed(2)}:1 is below AA`);
  assert.ok(
    onHover <= contrast(white, TOKENS.get("jf-primary")!) + 0.0001,
    "the hover label now has MORE contrast than the resting label while the hover fill is " +
      "supposed to be the lighter state — the ladder has been inverted",
  );
});

// ─── 2. the whole class: every white label over a resolvable fill ───────────
test("no element paints a fill under a white label that fails AA (repo-wide)", () => {
  const findings = allPairings()
    .map((p) => ({ ...p, ratio: contrast(p.fg, p.bg) }))
    .filter((p) => p.ratio < 4.5)
    .map(
      (p) =>
        `${p.file}:${p.line} — the white label is ${p.ratio.toFixed(2)}:1 on its ${p.state} fill ` +
        `rgb(${p.bg.join(",")}) (${p.cls})`,
    );
  assert.deepEqual(
    findings,
    [],
    `A white label on a fill must clear 4.5:1 (WCAG AA, normal text). The CTA defect this ` +
      `card fixes was exactly this pairing — #828fff measured 2.87:1:\n` +
      findings.join("\n"),
  );
});

test("the sweep is looking at the real app (it cannot pass vacuously)", () => {
  const files = tsxFiles(fileURLToPath(SRC));
  assert.ok(files.length >= 40, `the walk found only ${files.length} .tsx files under web/src — it is not reading the app`);
  assert.ok(
    files.includes("components/Shell.tsx") && files.includes("app/login/page.tsx"),
    `the walk is missing core files (found ${files.length})`,
  );
  const pairings = allPairings();
  assert.ok(
    pairings.length >= 20,
    `the sweep resolved only ${pairings.length} white-label/fill pairings — if the class names changed, ` +
      `re-anchor this guard (a rule keyed on a class string is invisible to a rename)`,
  );
  // The token the fix depends on is one of the resolved fills, i.e. the sweep can
  // actually see `bg-[var(--jf-primary-hover)]` and not just literals.
  assert.ok(
    pairings.some((p) => p.cls.includes("var(--jf-primary-hover)")),
    "the sweep no longer resolves the --jf-primary-hover token from a class list — the rule that " +
      "makes this guard about the shared CTA has stopped seeing it",
  );
});

// ─── 3. the literal with no AA-safe use here ────────────────────────────────
test("#828fff is gone as a fill: it has no AA-safe use with the white labels it was paired with", () => {
  // 2.87:1 against white. Any *lighter* fill on the primary hue is the same
  // defect, which is why the fix is a token every one of these sites now shares
  // rather than twelve edited literals that the next button would not inherit.
  const offenders: string[] = [];
  const globalsCss = srcAt("app/globals.css");
  for (const rel of [...tsxFiles(fileURLToPath(SRC)), "app/globals.css"]) {
    const src = rel === "app/globals.css" ? globalsCss : srcAt(rel);
    // Comment state, tracked across lines so a rationale block that names the old
    // colour cannot be read as a use of it (and a use inside a block comment is
    // not a use either).
    let inBlockComment = false;
    src.split("\n").forEach((line, i) => {
      const trimmed = line.trim();
      let code = line;
      if (inBlockComment) {
        const end = code.indexOf("*/");
        if (end === -1) return;
        code = code.slice(end + 2);
        inBlockComment = false;
      }
      const open = code.indexOf("/*");
      if (open !== -1) {
        const close = code.indexOf("*/", open + 2);
        if (close === -1) {
          inBlockComment = true;
          code = code.slice(0, open);
        } else {
          code = code.slice(0, open) + code.slice(close + 2);
        }
      }
      code = code.replace(/\/\/.*$/, "");
      if (/^\s*(\/\/|\*)/.test(trimmed)) return;
      if (/828fff/i.test(code)) offenders.push(`${rel}:${i + 1} — ${trimmed.slice(0, 100)}`);
    });
  }
  assert.deepEqual(
    offenders,
    [],
    `#828fff measures 2.87:1 against its white label — it is below AA wherever a white label sits ` +
      `on it. Use bg-[var(--jf-primary-hover)] (${contrast([255, 255, 255], TOKENS.get("jf-primary-hover")!).toFixed(2)}:1):\n` +
      offenders.join("\n"),
  );
});

// ─── 4. the conversion this guard depends on, against the repo's own numbers ─
test("the OKLCH -> sRGB conversion matches values this repo already measured", () => {
  // src/lib/mutedText.test.ts records slate-400 = #90a1b9 and slate-500 = #62748e
  // as the colours it measured with axe. If the conversion drifts, every palette
  // ratio in the sweep above is quietly wrong — a guard that reports a ratio it
  // did not compute is worse than no guard.
  const rgb = (name: string) => {
    const v = PALETTE.get(name);
    assert.ok(v, `Tailwind's theme no longer declares --color-${name}`);
    return `#${v!.map((c) => c.toString(16).padStart(2, "0")).join("")}`;
  };
  assert.equal(rgb("slate-400"), "#90a1b9", "the OKLCH conversion drifted from the measured slate-400");
  assert.equal(rgb("slate-500"), "#62748e", "the OKLCH conversion drifted from the measured slate-500");
  // And the card's own headline number, from the two hexes, end to end.
  assert.equal(contrast([255, 255, 255], hexToRgb("#828fff")!).toFixed(2), "2.87");
  assert.equal(contrast([255, 255, 255], hexToRgb("#5e6ad2")!).toFixed(2), "4.70");
  assert.equal(contrast([255, 255, 255], hexToRgb("#454ea8")!).toFixed(2), "7.22");
  assert.equal(contrast([255, 255, 255], hexToRgb("#616dd5")!).toFixed(2), "4.51");
});
