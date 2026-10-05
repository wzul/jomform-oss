// Repo-wide guard: an EMERALD (success/paid/confirmed) text token must not be
// painted on a surface it cannot clear 4.5:1.
//
// WHAT WAS WRONG (measured, and independently re-derived for this card)
//
//   `text-emerald-600` is the app's "success / paid / confirmed" text colour and
//   it is below the WCAG AA floor everywhere it is painted. Tailwind v4's palette
//   is OKLCH, resolved to sRGB exactly as a browser paints it (see the conversion
//   below) — so these are the real values, not v3 hexes:
//
//     text-emerald-500  #00bc7d   white 2.47:1   --jf-bg 2.32:1   emerald-50 2.35:1   FAILS
//     text-emerald-600  #009966   white 3.65:1   --jf-bg 3.43:1   emerald-50 3.47:1   FAILS
//     text-emerald-700  #007a55   white 5.36:1   --jf-bg 5.04:1   emerald-50 5.09:1   PASSES
//
//   The class was internally inconsistent in the SAME file: `settings/page.tsx`
//   painted two adjacent save banners — one `text-emerald-700` (5.09:1, passes)
//   and the next line `text-emerald-600` (3.47:1, fails) — on the identical
//   `bg-emerald-50` surface. `text-emerald-600` was used ~13 times across the
//   authenticated app (dashboard, sales, templates, settings, profile-ish tabs).
//
// WHY NO GATE SAW IT
//
//   axe judges only the state that is RENDERED and the surface it is rendered on.
//   Every emerald-600 site is a save/verify banner or a live status line that
//   renders only after a mutation, and the page-level axe scans are taken on a
//   settled page — so `web/e2e/a11y.spec.ts` and the authenticated scans read them
//   as clean. This is a source-level obligation. (Same reason as the danger-text
//   card t_0fdbe189.)
//
// THE FIX, AND WHICH GUARD OWNS WHAT (so the colour-class guards do not overlap)
//
//   `text-emerald-700` is the replacement token for EVERY converted site. It is
//   not a new choice: `settings/page.tsx` already painted its sibling banner with
//   it, it clears AA on every surface above, and it keeps the emerald hue so the
//   success semantics survive. The same reasoning the repo used for
//   `--jf-danger-text` applies — a colour that is only ever a TEXT colour must be
//   the darker step.
//
//   Ownership, stated so no two guards judge the same class:
//     * mutedText.test.ts      owns the muted GRAY family (`--jf-muted`,
//                              `text-slate-400/500`, literal `#9ca3af`).
//     * tintedSurface.test.ts  owns the indigo TINT pair (`--jf-primary` /
//                              `--jf-muted` on `bg-indigo-50`).
//     * THIS FILE (`emeraldText.test.ts`) owns the EMERALD/success family
//                              (`text-emerald-50..950`).
//     * t_0fdbe189's guard     owns the red/sky family (`text-red-500`,
//                              `text-sky-600`). That card and this one are
//                              deliberately separate PRs: one family, one census,
//                              one guard, so each diff stays reviewable.
//
// WHY THE RULE IS DERIVED FROM THE THEME, NOT A REMEMBERED HEX
//
//   The `bad` set of surfaces for each emerald step is COMPUTED from the installed
//   Tailwind theme and the `--jf-*` tokens in `globals.css`, so a future
//   `text-emerald-500` (or any new step) is judged the moment it is written — the
//   step is looked up in the palette, its ratios measured, and it is forbidden on
//   whatever surfaces it fails. A guard with a hand-written hex cannot do that: a
//   palette upgrade would silently move the numbers and the guard would keep
//   passing about a colour the app no longer paints. This mirrors
//   `ctaContrast.test.ts` (card t_9efae9f0, PR #991), which resolves its fills the
//   same way; the small OKLCH/contrast block below is deliberately local to this
//   file so a helper defect here cannot blind the other guard (the `jsxSource.ts`
//   header records what a shared copy cost once).
//
// WHAT IT DELIBERATELY DOES NOT DO
//
//   * It does not judge `bg-emerald-*` / `border-emerald-*` — a fill or a border is
//     not text. `bg-emerald-500` status DOTS (sales `live` indicator, settings
//     webhook status) are untouched.
//   * It does not judge a token against a surface it cannot SEE. A token whose
//     nearest background comes from a translucent composite (`bg-emerald-500/10`),
//     an overlay, or a media query is left to the browser; a source scan cannot
//     judge it. (The one such site, `connect/page.tsx`, already paints
//     `text-emerald-700` on `bg-emerald-500/10`; measured by hand, the composited
//     tint is #e6f8f2 and emerald-700 on it is 4.87:1 — it passes, so it was never
//     part of this card and is left alone.)
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
function hexToRgb(hex: string): RGB | null {
  const h = hex.replace("#", "");
  const full = h.length === 3 ? h.split("").map((c) => c + c).join("") : h;
  if (!/^[0-9a-fA-F]{6}$/.test(full)) return null;
  return [0, 2, 4].map((i) => parseInt(full.slice(i, i + 2), 16)) as RGB;
}

// Tailwind v4's palette is OKLCH, so the sweep must resolve OKLCH to the sRGB the
// browser actually paints. This is Björn Ottosson's OKLab -> linear sRGB followed
// by the sRGB transfer function — the same conversion `ctaContrast.test.ts` uses,
// pinned below against the emerald values this card measured (#009966 / #007a55).
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

// ─── the surfaces this app paints, and the emerald tokens ───────────────────
//
// Each surface is the class that declares it. The `jf-*` component classes count
// because they are the app's own surfaces: `globals.css` declares
// `.jf-card { background: var(--jf-surface) }` (#ffffff) and `.jf-input { background:
// #fff }`. A muted token inside a card therefore sits on WHITE even when no `bg-*`
// utility appears in its class list.
const SURFACES: Record<string, RGB> = (() => {
  const out: Record<string, RGB> = {
    "bg-white": [255, 255, 255],
    "bg-[var(--jf-surface)]": TOKENS.get("jf-surface") ?? [255, 255, 255],
    "bg-[var(--jf-bg)]": TOKENS.get("jf-bg") ?? [247, 248, 248],
  };
  for (const name of ["slate-50", "slate-100", "slate-200", "indigo-50", "emerald-50", "emerald-100"]) {
    const rgb = PALETTE.get(name);
    if (rgb) out[`bg-${name}`] = rgb;
  }
  return out;
})();

const LIGHT_SURFACE_RE =
  /^((?:[a-z-]+:)*)(bg-(?:white|slate-50|slate-100|slate-200|indigo-50|emerald-50|emerald-100)|bg-\[var\(--jf-(?:surface|bg)\)\]|jf-card|jf-input)$/;

/** `jf-card` / `jf-input` resolve to the same white surface, so compare as one name. */
function canonicalSurface(cls: string): string {
  if (cls === "jf-card" || cls === "jf-input") return "bg-white";
  return cls;
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
      const [s, bareCls] = splitState(cls);
      return bareCls !== "" && COLOUR_CLS_RE.test(cls);
    }),
  );
}

/**
 * Every emerald TEXT token, mapped to the surfaces it cannot clear 4.5:1 on.
 *
 * Computed, not hand-written: for each `emerald-<step>` the theme declares, the
 * token's `bad` set is every surface above where the measured ratio is under 4.5.
 * `text-emerald-700`/`800` therefore come out with an EMPTY bad set and are never
 * flagged, while `text-emerald-500`/`600` come out forbidden everywhere — which is
 * exactly the census this card fixed and the rule that stops the next step
 * drifting in.
 */
const TEXT_TOKENS: Record<string, { token: string; bad: string[]; note: string }> = (() => {
  const out: Record<string, { token: string; bad: string[]; note: string }> = {};
  for (const name of PALETTE.keys()) {
    const m = /^emerald-(\d+)$/.exec(name);
    if (!m) continue;
    const rgb = PALETTE.get(name)!;
    const token = `text-emerald-${m[1]}`;
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
  //     state that surface applies, an emerald token that cannot clear 4.5:1 on it.
  for (const e of events) {
    for (const v of e.tag.variants) {
      const states = new Set<string>([""]);
      for (const cls of v.split(/\s+/)) {
        const [s, bareCls] = splitState(cls);
        if (bareCls && (TEXT_TOKENS[bareCls] || LIGHT_SURFACE_RE.test(cls))) states.add(s);
      }
      for (const state of states) {
        const surfaces = surfacesFor(v, state);
        if (surfaces.length === 0) continue;
        for (const tok of tokensIn(v, state)) {
          const bad = TEXT_TOKENS[tok].bad.filter((b) => surfaces.includes(b));
          if (bad.length === 0) continue;
          findings.push(
            `${rel}:${lineAt(e.tag.openIdx)} — <${e.tag.name}> paints ${tok} on ${bad.join(" / ")}` +
              `${state ? ` (in the ${state} state)` : ""}: ${TEXT_TOKENS[tok].note}; use text-emerald-700`,
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
        return tokensIn(v, s).length > 0;
      }),
    );
    if (ownIsBad) continue; // already (a)

    // The token's OWN element may declare a background this guard deliberately
    // cannot judge — a DARK slab (`bg-slate-900`, where the landing page and
    // /mcp paint their code snippets), a gradient, or a translucent tint. In that
    // case the ancestor's light surface is NOT what the token is painted on, and
    // attributing it there is a false positive: `text-emerald-300` (#5ee9b5) on
    // the `bg-slate-900` slab measures 11.72:1 in the live DOM (measured on the
    // built page for this card) and passes — it is a dark-surface pairing, which
    // the browser gate's job (both pages are in its route list), not this rule's.
    // A source scan cannot composite a gradient or an alpha tint either, so this
    // abstains instead of guessing — the same call the header states.
    const ownBackgrounds = owner.variants.flatMap((v) =>
      v
        .split(/\s+/)
        .map((cls) => splitState(cls))
        .filter(([s, bareCls]) => s === "" && bareCls && bareCls.startsWith("bg-"))
        .map(([, bareCls]) => bareCls),
    );
    if (ownBackgrounds.length > 0 && !ownBackgrounds.some((b) => LIGHT_SURFACE_RE.test(b))) continue;

    const surfaceEl = ancestorsAt(events, owner.openIdx)
      .filter((t) => t !== owner)
      .find((t) =>
        t.variants.some((v) =>
          v.split(/\s+/).some((cls) => {
            const [s] = splitState(cls);
            return s === "" && LIGHT_SURFACE_RE.test(cls);
          }),
        ),
      );
    const surfaces = surfaceEl
      ? surfaceEl.variants.flatMap((v) =>
          v
            .split(/\s+/)
            .map((cls) => splitState(cls))
            .filter(([, bareCls]) => bareCls && LIGHT_SURFACE_RE.test(bareCls))
            .map(([s, bareCls]) => (s === "" ? canonicalSurface(bareCls) : "")),
        )
      : (layoutSurface ? [layoutSurface] : [PAGE_BG]);
    const bad = TEXT_TOKENS[tok].bad.filter((b) => surfaces.includes(b));
    if (bad.length === 0) continue;
    const where = surfaceEl
      ? `inside <${surfaceEl.name}> which paints`
      : layoutSurface
        ? "on a surface its layout paints"
        : "on the page background";
    findings.push(
      `${rel}:${lineAt(idx)} — ${tok} is ${where} ${bad.join(" / ")}: ${TEXT_TOKENS[tok].note}; use text-emerald-700`,
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
      if (childSurfaces.length === 0) continue;
      const nearest = events
        .filter((e) => e.open && e.tag.openIdx <= child.openIdx && e.tag.closeIdx > child.openIdx && declaresTextColour(e.tag))
        .pop()?.tag;
      if (nearest !== owner) continue;
      const okBehind = TEXT_TOKENS[tok].bad.every((b) => !pageSurfaces.includes(b));
      if (!okBehind) continue;
      const bad2 = TEXT_TOKENS[tok].bad.filter((b) => childSurfaces.includes(b));
      if (bad2.length === 0) continue;
      findings.push(
        `${rel}:${lineAt(child.openIdx)} — ${tok} inherited into <${child.name}> which paints ` +
          `${bad2.join(" / ")}: ${TEXT_TOKENS[tok].note}; use text-emerald-700`,
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

test("no emerald TEXT token is painted on a surface it fails AA on (repo-wide)", () => {
  const findings = allFindings();
  assert.deepEqual(
    findings,
    [],
    `Emerald/success text must clear 4.5:1 on the surface it is painted on. ` +
      `text-emerald-500 (#00bc7d) is 2.47:1 and text-emerald-600 (#009966) 3.65:1 on white — ` +
      `text-emerald-700 (#007a55) clears AA everywhere and is the token this class uses:\n` +
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
    FILES.includes("components/Shell.tsx") && FILES.includes("app/settings/page.tsx"),
    `the walk is missing core files (found ${FILES.length})`,
  );

  // The OKLCH conversion is real: it reproduces the two emerald values the card
  // measured from the installed theme, so the ratios above are about the colours
  // the browser paints and not a remembered v3 hex.
  const hex = (name: string) => {
    const rgb = PALETTE.get(name);
    assert.ok(rgb, `the installed Tailwind theme no longer declares ${name} — re-anchor this guard`);
    return `#${rgb!.map((v) => v.toString(16).padStart(2, "0")).join("")}`;
  };
  assert.equal(hex("emerald-600"), "#009966", "emerald-600 no longer resolves to the measured #009966");
  assert.equal(hex("emerald-700"), "#007a55", "emerald-700 no longer resolves to the measured #007a55");

  // The forbidden steps are forbidden because they were MEASURED below AA, not by
  // a hardcoded list: emerald-600 is bad on every surface here, emerald-700 on
  // none of the app's own ones.
  assert.ok(
    TEXT_TOKENS["text-emerald-600"].bad.length >= 5,
    `text-emerald-600 came out with ${TEXT_TOKENS["text-emerald-600"].bad.length} failing surfaces — ` +
      `the ratio computation has gone wrong`,
  );
  // emerald-700 clears AA on the app's OWN surfaces — white / --jf-bg / emerald-50.
  // It is NOT asserted bad=[] outright: slate-200 is a genuinely darker gray tint
  // (4.31:1) where emerald-700 is also under AA, and this guard should flag that
  // pairing if anyone paints it. Pinning the three surfaces the conversion used is
  // the honest assertion.
  for (const surface of ["bg-white", "bg-[var(--jf-bg)]", "bg-emerald-50"]) {
    assert.ok(
      !TEXT_TOKENS["text-emerald-700"].bad.includes(surface),
      `text-emerald-700 is now measured below AA on ${surface} — re-measure before trusting this guard`,
    );
  }

  // The token the conversion uses is still what the app paints, and the literal
  // the class was is gone: a rename is invisible to a rule keyed on a class string.
  const source = FILES.map((rel) => codeOnly(sourceOf(rel))).join("\n");
  const ok = [...source.matchAll(/text-emerald-700(?![\w-])/g)].length;
  assert.ok(
    ok >= 20,
    `only ${ok} uses of text-emerald-700 — if the token was renamed, re-anchor this guard`,
  );
  for (const gone of ["text-emerald-600", "text-emerald-500"]) {
    assert.ok(
      !new RegExp(`${gone}(?![\\w-])`).test(source),
      `${gone} is back in the app — this is the class card t_fc1b67f1 removed`,
    );
  }
});

// ─── the inherited shape, against a real defect this rule is FOR ─────────────
test("a forbidden token is reported when an inline child paints the surface", () => {
  const src = [
    "export function P() {",
    "  return (",
    '    <div className="jf-card">',
    '      <p className="text-emerald-600">Brand <code className="bg-emerald-100">ID</code></p>',
    "    </div>",
    "  );",
    "}",
    "",
  ].join("\n");
  const findings = findingsIn("fixture.tsx", src);
  assert.ok(
    findings.some((f) => f.includes("text-emerald-600")),
    `the forbidden emerald token stopped being reported: ${JSON.stringify(findings)}`,
  );
});
