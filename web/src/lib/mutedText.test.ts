// Repo-wide guard: a MUTED-GRAY text token must not be painted on a surface it
// cannot clear 4.5:1, and the literal gray that has no AA-safe use is gone.
//
// WHAT WAS WRONG (measured by axe on the production standalone build of this
// worktree, with a real session and cleared storage — see the card, not this
// comment, for the run)
//
//   Three foregrounds were painting below the WCAG AA floor for the 11-14px text
//   that uses them:
//
//     text-[#9ca3af]  a literal gray   2.38:1 on --jf-bg, 2.53:1 on white
//                                      -> 11 axe-serious nodes on /builder
//     text-slate-400  == #90a1b9      2.63:1 on white, 2.47:1 on --jf-bg,
//                                      2.40:1 on slate-100 -> 7 nodes on
//                                      /profile, 6 more on /settings
//     text-slate-500  == #62748e      4.47:1 on --jf-bg — it PASSES on white
//                                      (4.76:1) and on slate-50 (4.55:1), and
//                                      that is the whole trap: the same utility
//                                      is fine on a card and 0.03 short on the
//                                      page background behind the tab strip
//                                      -> 14 nodes on /settings + /profile
//
//   Not one of those is reachable by `npm run a11y`: the gate's PAGES list is
//   the public routes plus /docs/*, and every page above is behind a session.
//   That is why this is a source guard and not a browser assertion.
//
// THE DECISION (what replaces what)
//
//   The muted line has ONE token: `text-[var(--jf-muted)]` (#6b7280).
//   Measured — 4.54:1 on --jf-bg (#f7f8f8), 4.83:1 on white, 4.62:1 on
//   slate-50, 4.41:1 on slate-100. It is the muted text token this app already
//   uses 381 times, so a converted line does not change visual weight, only
//   brightness where brightness was the defect.
//
//   On a SOLID GRAY TINT (`bg-slate-50`, `bg-slate-100`, `bg-slate-200`) or the
//   indigo tint, `--jf-muted` (4.41:1 / 4.32:1) is itself short, so those use
//   the existing `text-[var(--jf-muted-on-tint)]` (#5b6472, 5.72:1 on slate-50,
//   5.46:1 on slate-100, 5.35:1 on indigo-50). `--jf-muted-on-tint` is not
//   "indigo-only": it is the token for muted text on ANY tinted surface, which
//   is what its name says.
//
//   Neither token may be the literal `#9ca3af`: it has no surface in this app
//   where it clears 4.5:1 (2.54:1 on white is its best case), so there is no
//   correct place for it and rule 2 below fails on any use at all.
//
// WHY EACH TOKEN NEEDS ITS OWN SURFACE RULE (this is what a grep cannot do)
//
//   `jf-muted` PASSES on white and slate-50 and FAILS on slate-100, so a guard
//   that flagged the token itself would be wrong 400+ times. The pairing is what
//   is checkable: the token and the surface in the SAME state of the SAME class
//   list, or the token on a descendant of a surface element.
//
// WHAT THIS DELIBERATELY DOES NOT DO
//
//   * It does not judge `text-slate-600` / `text-slate-700` — both clear 4.5:1
//     on every light surface above (7.13:1 / 9.73:1 on --jf-bg).
//   * It does not judge a token against a surface it cannot SEE. A token whose
//     nearest background comes from a parent's `bg-black/40` overlay, an
//     `aria-selected` attribute or a media query is left alone; those are
//     measured in a browser (this card's axe run) and pinned here afterwards if
//     they turn out to be a defect.
//   * It does not require a surface to be declared. `text-[var(--jf-muted)]`
//     with no background of its own is the 381-site majority and is correct on
//     white, which is what the app's cards are.
//
// The class-list / JSX-shape parser (`codeOnly`, `jsxTags`, `ancestorsAt`,
// `descendantsAt`, `tsxFiles`) now lives in `web/src/lib/jsxSource.ts`, shared
// with `web/src/lib/tintedSurface.test.ts` — which carried a byte-identical copy
// of it. That copy, not a preference for tidiness, is why this moved: the
// duplicated `codeOnly()` had a defect that made THIS guard report three false
// positives on the merge tree, and the same defect was silently blanking 41
// lines of the same file in the other guard.
//
// THE DEFECT (measured with the TypeScript parser as ground truth, and live axe
// on a production build of the merge tree — see the card, not this comment)
//
//   `codeOnly()` blanks comments so a class quoted in a comment can neither
//   raise nor satisfy an assertion. It scanned raw characters and read any
//   `/`+`*` in `code` state as a block-comment opener — it could not tell JSX
//   TEXT from code. `web/src/app/docs/api/page.tsx:175` writes the literal route
//   pattern in prose:
//
//     ... use it for <code className="rounded bg-slate-100 px-1">/v1/rest/*</code>
//
//   The text `/v1/rest/*` ends in `/`+`*`. The lexer entered `block` and blanked
//   forward, swallowing that line's `</code>`; blanking a comment's body is what
//   the function is FOR, so nothing flagged it. The tag never closed,
//   `<code@175>` became a phantom ancestor of everything below, and this guard
//   reported `text-slate-500` at 208/357/361 as sitting on a `<code>` that is
//   180 lines away in a different `<section>` — while 41 lines of the file were
//   invisible to all four rules. Ten more sites in the same shape (`https://…`
//   written as JSX text, and a `//` inside it) were being read as comments.
//
//   The fix is in `jsxSource.ts`: comment openers are only honoured outside JSX
//   text, and the JSX text spans come from the TypeScript parser for the reason
//   in that module's header.
//
// WHY THE PAGE-BACKGROUND FALLBACK IS NO LONGER TAKEN BLINDLY
//
//   Rule (b) below used to treat "no light surface in this file" as "therefore
//   the page background". That is a claim a single file often cannot support:
//   `web/src/app/docs/layout.tsx:61` is `<main className="min-h-screen
//   bg-white">`, so every docs page renders on WHITE, and the three findings
//   above were substantively wrong as well as mis-attributed —
//   `text-slate-500` (#62748e) is 4.76:1 on white (PASSES AA) and 4.48:1 on
//   `--jf-bg` (#f7f8f8, the real defect class). Live axe on a standalone build
//   of this tree reads `/docs/api` with 0 color-contrast violations and the 123
//   affected spans at 4.76:1. So the fallback now consults the page's
//   `layout.tsx` chain and only claims the page background when no layout
//   declares a light surface over `{children}`. For the dashboard pages that is
//   still the case (Shell paints nothing, and `globals.css` sets
//   `html, body { background: var(--jf-bg) }`), which is what keeps the
//   tab-strip defect at 4.48:1 reported.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, mkdtempSync, writeFileSync, mkdirSync, rmSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { tmpdir } from "node:os";
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

// ─── the tokens and the surfaces ─────────────────────────────────────────────
//
// A class token with its state prefix split out: "hover" / "md:hover" / "focus"
// ... and "" for the resting state.
const STATE_RE = /^((?:[a-z-]+:)*)/;

/** Strip the state prefix; returns [state, bareClass]. */
function splitState(cls: string): [string, string] {
  const m = STATE_RE.exec(cls)!;
  return [m[1], cls.slice(m[1].length)];
}

// ── the muted-gray text tokens, and the light surfaces in this app ──────────

/**
 * Every muted-gray TEXT token, mapped to the surfaces it cannot clear 4.5:1 on.
 *
 * `bad` is the set of light surfaces that must not be painted under this token.
 * A surface a token PASSES on is simply absent from the set, which is what lets
 * `--jf-muted` be legal on white and illegal on slate-100 in the same guard.
 */
const TEXT_TOKENS: Record<string, { token: string; bad: string[]; note: string }> = {
  "text-[var(--jf-muted)]": {
    token: "text-[var(--jf-muted)]",
    // 4.54:1 on #f7f8f8, 4.83:1 white, 4.62:1 slate-50, but 4.41:1 slate-100,
    // 3.92:1 slate-200, 4.32:1 indigo-50.
    bad: ["bg-slate-100", "bg-slate-200", "bg-indigo-50"],
    note: "--jf-muted is 4.54:1 on the page background and 4.83:1 on white, but 4.41:1 on slate-100",
  },
  "text-[var(--jf-muted-on-tint)]": {
    token: "text-[var(--jf-muted-on-tint)]",
    // 5.35:1 or better on every light surface in the app; nothing to forbid.
    bad: [],
    note: "--jf-muted-on-tint clears 4.5:1 on every surface in the app",
  },
  "text-slate-400": {
    // #90a1b9: 2.63:1 white, 2.47:1 #f7f8f8, 2.40:1 slate-100 — it is short
    // everywhere, which is why there is no surface it may be paired with.
    token: "text-slate-400",
    bad: [
      "bg-white",
      "bg-[var(--jf-surface)]",
      "bg-[var(--jf-bg)]",
      "bg-slate-50",
      "bg-slate-100",
      "bg-slate-200",
      "bg-indigo-50",
    ],
    note: "text-slate-400 (#90a1b9) is 2.63:1 on white — too light for any surface here",
  },
  "text-slate-500": {
    // #62748e: 4.76:1 white and 4.55:1 slate-50 — those two PASS. 4.48:1 on
    // #f7f8f8 and 4.26:1 on indigo-50 do not.
    token: "text-slate-500",
    bad: ["bg-[var(--jf-bg)]", "bg-slate-100", "bg-slate-200", "bg-indigo-50"],
    note: "text-slate-500 (#62748e) passes on white/slate-50 but is 4.48:1 on the page background",
  },
};

// ── 2. the literal gray that has no AA-safe use at all ──────────────────────
//
// The literal `#9ca3af` is not in TEXT_TOKENS because there is no placement
// that makes it legal: its BEST case on any light surface in this app is
// 2.54:1 (white), so rule (c) below flags every use with no surface rule. Its
// pattern lives in `LITERAL_RE` inside `findingsIn`, so the scan reports an
// offset (and therefore a correct line) like the other rules.

// ── surfaces ─────────────────────────────────────────────────────────────────

/**
 * A SOLID light surface a token is painted ON.
 *
 * The `jf-*` component classes count because they are the app's own surfaces:
 * `globals.css` declares `.jf-card { background: var(--jf-surface) }` (#ffffff)
 * and `.jf-input { background: #fff }`, and they are used 69 + 100+ times. A
 * muted token inside a card therefore sits on WHITE even when no `bg-*` utility
 * appears in its class list — the case the first version of this guard missed,
 * and the only reason it did not flag the two `/profile` help lines the card's
 * axe run named (they are `text-slate-400` inside a `.jf-card`).
 *
 * Translucent backgrounds (`bg-slate-900/5`, `bg-black/40`) are NOT matched:
 * their composite depends on what is behind them, so a source scan cannot judge
 * them. `bg-white/10` on the MCP code block is one of those and is measured in
 * the browser instead.
 */
const LIGHT_SURFACE_RE =
  /^((?:[a-z-]+:)*)(bg-(?:white|slate-50|slate-100|slate-200|indigo-50)|bg-\[var\(--jf-(?:surface|bg)\)\]|jf-card|jf-input)$/;

/**
 * Canonical surface name for the class that declares it.
 *
 * `jf-card` and `jf-input` resolve to the same white `--jf-surface`/`#fff` in
 * `globals.css`, so they are compared as `bg-white` and the per-token `bad` sets
 * stay keyed on one name per surface. `bg-[var(--jf-surface)]`/`bg-[var(--jf-bg)]`
 * are kept distinct because the latter is #f7f8f8, where `jf-muted` passes.
 */
function canonicalSurface(cls: string): string {
  if (cls === "jf-card" || cls === "jf-input") return "bg-white";
  return cls;
}

/**
 * Elements that can carry an inherited colour into a surface they paint
 * themselves. Rule (b2) is restricted to these: a block child starts its own
 * line box, so the ancestor's colour never lands on the child's background in
 * the way this defect needs.
 */
const INLINE = new Set(["code", "span", "strong", "em", "b", "i", "a", "small", "kbd", "samp"]);

/**
 * A class that sets the text COLOUR — any of them, not just the muted tokens
 * this guard tracks.
 *
 * Rule (b2) has to know whether something between the token and the child
 * re-declares the colour, and that something is usually a perfectly legal
 * `text-slate-600` or `text-[var(--jf-primary)]`. Matching only this guard's own
 * token table would call those "no colour declared" and attribute the ancestor's
 * muted token to a child it never reaches — which is a false positive, not a
 * conservative miss. Font-size (`text-sm`, `text-[11px]`) and alignment
 * (`text-center`) utilities set no colour and must not match.
 */
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

// ─── the rules, applied to one class list ────────────────────────────────────

/**
 * The light surfaces a token in `state` is actually painted on.
 *
 * Effective-per-state, because "painted on the surface" is a per-state question:
 *
 *   `bg-slate-50 text-[var(--jf-muted)]`
 *     the resting text sits on slate-50 -> the pairing is real.
 *
 *   `text-[var(--jf-muted)] hover:bg-slate-100`
 *     the token's own state is the resting one and no surface is declared in it,
 *     so nothing is compared — a token and a surface that only ever appear in
 *     different states cannot overlap.
 *
 *   `bg-slate-50 text-[var(--jf-muted)] hover:bg-slate-100`
 *     the hover surface REPLACES the resting one (Tailwind emits variants after
 *     base), so the resting token is compared with slate-50, and any token the
 *     hover state declares is compared with slate-100.
 */
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

/** The muted tokens painted in `state` by this class list. */
function tokensIn(variant: string, state: string): string[] {
  const out: string[] = [];
  for (const cls of variant.split(/\s+/)) {
    const [s, bareCls] = splitState(cls);
    if (s !== state) continue;
    if (TEXT_TOKENS[bareCls]) out.push(bareCls);
  }
  return out;
}

/**
 * The light surfaces an element declares, in `state`, as canonical names.
 *
 * Used for the layout chain: a layout's `{children}` slot is where the page's
 * markup lands, so the elements enclosing that slot are the surfaces the page is
 * painted on even though the page's own file never names them.
 */
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

/** Findings for one file: shape (a) same element, shape (b) inherited, shape (c) literal. */
function findingsIn(rel: string, src: string, ambient: string[][] = []): string[] {
  const code = codeOnly(src);
  const events = jsxTags(code);
  const findings: string[] = [];
  const lineAt = (idx: number) => code.slice(0, idx).split("\n").length;

  // (a) Same element: one class list that renders BOTH a light surface and, in
  //     the state that surface applies, a muted token that cannot clear 4.5:1 on
  //     it.
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
              `${state ? ` (in the ${state} state)` : ""}: ${TEXT_TOKENS[tok].note}`,
          );
        }
      }
    }
  }

  // (b) Inherited, or on the page background.
  //
  //     Two shapes, and the second is why an earlier version of this rule let the
  //     tab-strip labels through:
  //
  //       * the token sits on a descendant of a surface element, so it is painted
  //         on that surface even though its own class list declares none;
  //       * NO ancestor declares a surface at all, so the token is painted on the
  //         app's page background — `globals.css` sets
  //         `html, body { background: var(--jf-bg) }` (#f7f8f8). That is the case
  //         the `/settings` and `/profile` tab strips are in, and where
  //         `text-slate-500` measures 4.48:1 (axe: 4.47) — the card's
  //         "off by 0.03" node.
  //
  //     The token is attributed to the element that OWNS it (the tag whose
  //     attribute list contains the offset). A token written in the same
  //     element's className was already reported by (a) — reporting it again as
  //     "inherited" would double-count one site.
  //
  //     An ancestor only counts here when its surface is UNQUALIFIED, because the
  //     descendant's token and the ancestor's surface would have to be in the
  //     same state to overlap and a source scan cannot pair states across
  //     elements. That keeps this shape a real defect and not a guess.
  //
  //     WHEN THERE IS NO ANCESTOR SURFACE, the page background is only claimed
  //     when the page's `layout.tsx` chain does not declare one either. A guard
  //     that cannot prove a negative must not assert it: `docs/layout.tsx:61` is
  //     `<main className="min-h-screen bg-white">`, so every docs page renders on
  //     WHITE (where `text-slate-500` is 4.76:1 and passes) and an in-file scan
  //     that assumed `--jf-bg` reported 4.48:1 for text that is fine. `ambient`
  //     carries those layout surfaces, nearest first.
  const PAGE_BG = "bg-[var(--jf-bg)]";
  const ambientSurfaces = ambient.flatMap((variants) => declaredSurfaces(variants));
  const TOKEN_RE = new RegExp(
    `(?:^|[\\s"'\`])(${Object.keys(TEXT_TOKENS)
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
        return tokensIn(v, s).some((t) => TEXT_TOKENS[t].bad.length > 0);
      }),
    );
    if (ownIsBad) continue; // already (a)
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
    // Which of the surfaces that ancestor declares is the token painted on? The
    // nearest one wins, but the ancestor list is outermost-first, so take the
    // LAST surface in every variant it declares. With no surface ancestor, the
    // surface is whatever the page's layout chain paints (nearest first), and
    // only failing that is it the app page background.
    const layoutSurface = ambientSurfaces[0];
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
    // NOT `continue` when `bad` is empty: rule (b2) below only reports a site
    // that is clear on its OWN surface, so it needs this branch to have run.
    if (bad.length > 0) {
      const where = surfaceEl
        ? `inside <${surfaceEl.name}> which paints`
        : layoutSurface
          ? "on a surface its layout paints"
          : "on the page background";
      findings.push(
        `${rel}:${lineAt(idx)} — ${tok} is ${where} ${bad.join(" / ")}: ${TEXT_TOKENS[tok].note}`,
      );
    }

    // (b2) Inherited by an inline descendant that repaints.
    //
    //      A token that is legal on its own element is NOT safe if an inline
    //      descendant declares a surface the token cannot clear: CSS colour is
    //      inherited, so `<p class="… jf-muted">Brand <code class="bg-slate-100">ID</code></p>`
    //      paints `--jf-muted` (#6b7280) on slate-100, which is 4.41:1 — below
    //      the floor, and a real node that shipped.
    //
    //      Restricted to inline elements on purpose: a block child starts its own
    //      line box, so ordinary nesting is not this defect. And the child's
    //      NEAREST colour-declaring ancestor must be this element — if something
    //      between them declares a colour (or the child declares its own), this
    //      token never reaches the child and some other occurrence owns it.
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
      // `<=` on openIdx so the child counts as its own colour owner: an inline
      // element that sets its own colour (e.g. a badge `<a>` with
      // `text-slate-600`) never receives the ancestor's token at all.
      const nearest = events
        .filter(
          (e) =>
            e.open &&
            e.tag.openIdx <= child.openIdx &&
            e.tag.closeIdx > child.openIdx &&
            declaresTextColour(e.tag),
        )
        .pop()?.tag;
      if (nearest !== owner) continue;
      // Only report where the token was ALREADY clear of the surface behind it;
      // if it was not, the branch above already reported that site.
      const okBehind = TEXT_TOKENS[tok].bad.every((b) => !pageSurfaces.includes(b));
      if (!okBehind) continue;
      const bad2 = TEXT_TOKENS[tok].bad.filter((b) => childSurfaces.includes(b));
      if (bad2.length === 0) continue;
      findings.push(
        `${rel}:${lineAt(child.openIdx)} — ${tok} inherited into <${child.name}> which paints ` +
          `${bad2.join(" / ")}: ${TEXT_TOKENS[tok].note}`,
      );
    }
  }

  // (c) The literal gray with no AA-safe surface in the app.
  //
  //     Offset-based like the rules above, NOT line-based: several of these
  //     sites sit on a line that also appears verbatim elsewhere in the file, so
  //     locating one by its text reported the wrong line number.
  const LITERAL_RE = /(?:^|[\s"'`])(text-\[#9ca3af\])(?![\w-])/g;
  let lit: RegExpExecArray | null;
  while ((lit = LITERAL_RE.exec(code))) {
    const idx = lit.index + lit[0].length - lit[1].length;
    findings.push(
      `${rel}:${lineAt(idx)} — text-[#9ca3af] (#9ca3af) is 2.54:1 on white, its BEST case here; ` +
        `use text-[var(--jf-muted)] (4.83:1 on white) or text-[var(--jf-muted-on-tint)] on a tint`,
    );
  }

  // A finding must be reported once per site, not once per variant.
  return [...new Set(findings)];
}

const FILES = tsxFiles(SRC);

/**
 * The file contents, read once. `ambientSurfaceClasses` needs the layout files
 * too, and re-reading them per page would be `FILES.length` extra reads.
 */
const SOURCES = new Map<string, string>();
function sourceOf(rel: string): string {
  let s = SOURCES.get(rel);
  if (s === undefined) {
    s = readFileSync(`${SRC}${rel}`, "utf8");
    SOURCES.set(rel, s);
  }
  return s;
}

/**
 * Run the rules over every `.tsx` under `root`, with the layout chain resolved
 * inside THAT tree.
 *
 * `root` is a parameter rather than the closed-over `SRC` so the cross-tree
 * case below can run the guard against a checkout it has never scanned — a tree
 * holding a page this branch has not seen. That failure mode is exactly how a
 * green branch turned red on merge: the walk is filesystem-driven, so a page
 * added on `main` is scanned for the first time on the merge tree.
 */
function findingsInTree(root: string): string[] {
  // `tsxFiles` joins with `+`, so the root needs exactly one trailing slash.
  const base = root.endsWith("/") ? root : `${root}/`;
  const files = tsxFiles(base);
  const read = (rel: string) => readFileSync(`${base}${rel}`, "utf8");
  const out: string[] = [];
  for (const rel of files) {
    out.push(...findingsIn(rel, read(rel), ambientSurfaceClasses(rel, files, read)));
  }
  return out;
}

function allFindings(): string[] {
  const out: string[] = [];
  for (const rel of FILES) {
    out.push(...findingsIn(rel, sourceOf(rel), ambientSurfaceClasses(rel, FILES, sourceOf)));
  }
  return out;
}

test("no muted-gray text token is painted on a surface it fails AA on (repo-wide)", () => {
  const findings = allFindings();
  assert.deepEqual(
    findings,
    [],
    `Muted-gray text must use a token that clears 4.5:1 on the surface it is painted on:\n` +
      `  text-[var(--jf-muted)]            (#6b7280) — default muted line, NOT on slate-100/200/indigo-50\n` +
      `  text-[var(--jf-muted-on-tint)]    (#5b6472) — muted line ON a solid tint\n` +
      `  text-slate-400 / text-[#9ca3af]   are below AA on EVERY light surface in this app\n` +
      `  text-slate-500                    is 4.48:1 on --jf-bg although it passes on white\n\n` +
      findings.join("\n"),
  );
});

test("the repo-wide rule is looking at real, reachable markup", () => {
  // The guard above passes on an empty file list, so pin that the walk found the
  // app, that the tokens it judges still exist in it, and that the surfaces it
  // compares against are still the ones the app paints. Without this, a broken
  // walk (or a rename of every token) reads as a clean pass.
  assert.ok(
    FILES.length >= 40,
    `the walk found only ${FILES.length} .tsx files under web/src — it is not reading the app`,
  );
  assert.ok(
    FILES.includes("components/Shell.tsx") && FILES.includes("app/builder/page.tsx"),
    `the walk is missing core files (found ${FILES.length})`,
  );

  const source = FILES.map((rel) => codeOnly(readFileSync(`${SRC}${rel}`, "utf8"))).join("\n");
  for (const cls of ["bg-white", "bg-[var(--jf-bg)]", "bg-slate-50", "bg-slate-100"]) {
    assert.ok(
      source.includes(cls),
      `no page paints ${cls} any more — the surface set this guard compares against has moved, ` +
        `so re-anchor it rather than trusting a green run`,
    );
  }
  const mutedUses = [...source.matchAll(/text-\[var\(--jf-muted\)\]/g)].length;
  assert.ok(
    mutedUses >= 100,
    `only ${mutedUses} uses of text-[var(--jf-muted)] — if the token was renamed, re-anchor this ` +
      `guard (a renamed token is invisible to a rule keyed on the class string)`,
  );
});

// ─── the lexer, against the defect that made this guard mis-report ──────────
//
// THE REAL CASE, verbatim from `web/src/app/docs/api/page.tsx:175`. Its JSX TEXT
// `/v1/rest/*` ends in `/`+`*`, which the pre-fix lexer read as a block-comment
// opener; it blanked forward, took that line's `</code>` with it, and left
// `<code@175>` an unclosed ancestor of everything below — 41 lines invisible to
// every rule, and three findings attributed to a `<code>` 180 lines away.
test("a comment opener inside JSX text is text, not a comment", () => {
  const src = [
    "export function P() {",
    "  return (",
    "    <section>",
    '      <p className="text-[var(--jf-muted)]">',
    '        use it for <code className="rounded bg-slate-100 px-1">/v1/rest/*</code>{" "}',
    "        and for MCP.",
    "      </p>",
    '      <span className="text-slate-500">later section</span>',
    "    </section>",
    "  );",
    "}",
    "",
  ].join("\n");

  const code = codeOnly(src);
  // Byte-for-byte outside comments: nothing in this file is a comment.
  assert.equal(code, src, "JSX text was blanked as if it were a comment");

  // The `</code>` survives, so the tag closes and nothing after it is nested
  // inside it. This is the assertion that fails on the pre-fix lexer.
  const events = jsxTags(code);
  const codeTag = events.find((e) => e.open && e.tag.name === "code")!.tag;
  assert.equal(
    codeTag.closeIdx,
    src.indexOf("</code>") + "</code>".length,
    "the <code> tag did not close — a following section would read as its descendant",
  );
  const spanAt = src.indexOf("later section");
  assert.ok(
    !ancestorsAt(events, spanAt).includes(codeTag),
    "<code> is a phantom ancestor of a span in a later section",
  );

  // And the three shapes the guard is FOR still fire on this file's own markup,
  // so the fix did not make the lexer blind. `text-slate-500` here sits on the
  // page background (this fixture has no layout), which is the real defect class.
  const findings = findingsIn("fixture.tsx", src);
  assert.ok(
    findings.some((f) => f.includes("text-slate-500") && f.includes("page background")),
    `the muted token on the page background stopped being reported: ${JSON.stringify(findings)}`,
  );
});

test("a `//` inside JSX text does not blank the rest of the line", () => {
  // The same defect in its line-comment form, which hit 11 sites in this app —
  // every `https://…` written as JSX text. Blanking the line here would swallow
  // the closing tag AND the `text-[#9ca3af]` that rule (c) has to see.
  const src = [
    "export function P() {",
    "  return (",
    '    <p className="text-sm">',
    "      Point a client at <code>https://mcp.jomform.com/mcp</code> and continue.",
    '      <span className="text-[#9ca3af]">legacy</span>',
    "    </p>",
    "  );",
    "}",
    "",
  ].join("\n");

  const code = codeOnly(src);
  assert.equal(code, src, "JSX text after a `//` was blanked");
  assert.deepEqual(
    findingsIn("fixture.tsx", src).filter((f) => f.includes("#9ca3af")).length,
    1,
    "the literal gray after the `https://` text was not reported — the line was blanked",
  );
});

test("a real comment is still blanked, and its bytes still line up", () => {
  const src = [
    "// a comment quoting the class it replaced: text-[#9ca3af]",
    "export function P() {",
    "  /* block comment: text-slate-400 */",
    "  const url = \"https://example.com//x\";",
    "  return <p className=\"text-slate-500\">ok</p>;",
    "}",
    "",
  ].join("\n");

  const code = codeOnly(src);
  assert.equal(code.length, src.length, "the blanker changed the byte count");
  assert.equal(
    code.split("\n").length,
    src.split("\n").length,
    "the blanker changed the line count — every reported line number would shift",
  );
  assert.ok(!code.includes("text-[#9ca3af]"), "a class quoted in a line comment was not blanked");
  assert.ok(!code.includes("text-slate-400"), "a class in a block comment was not blanked");
  // The URL's `//` is inside a string, so it is code: the string survives.
  assert.ok(code.includes('"https://example.com//x"'), "a `//` inside a string literal was blanked");
  // Only the one real finding on this file.
  assert.equal(findingsIn("fixture.tsx", src).length, 1);
});

// ─── the cross-tree case: WHY this guard went red only on the merge tree ────
//
// The walk is filesystem-driven, so a page added on `main` after this branch
// forked is scanned for the FIRST time when the two are merged. That is how a
// green branch (373/373 on its own tree) turned red on `origin/main` +
// this branch (375/374/1) with the finding in a file this branch has never
// contained. A guard that only ever sees its own tree cannot catch that, so the
// fixture here is a tree this file does not ship, holding a page with the shape
// that broke it.
test("a page this branch has never seen is scanned correctly (cross-tree)", () => {
  const root = mkdtempSync(`${tmpdir()}/jf-guard-`);
  try {
    const write = (rel: string, body: string) => {
      mkdirSync(`${root}/${rel.slice(0, rel.lastIndexOf("/"))}`, { recursive: true });
      writeFileSync(`${root}/${rel}`, body);
    };
    // A layout that paints WHITE over its children, exactly as
    // `app/docs/layout.tsx:61` does. `text-slate-500` PASSES on white (4.76:1)
    // and fails on `--jf-bg` (4.48:1), so treating this page as surface-less
    // reports a defect that is not there.
    write(
      "app/docs/layout.tsx",
      [
        "export default function L({ children }: { children: React.ReactNode }) {",
        "  return <main className=\"min-h-screen bg-white\">{children}</main>;",
        "}",
        "",
      ].join("\n"),
    );
    write(
      "app/docs/api/page.tsx",
      [
        "export default function P() {",
        "  return (",
        "    <ul>",
        "      <li>",
        '        <code className="rounded bg-slate-100 px-1">/v1/rest/*</code>{" "}',
        '        <span className="text-slate-500"> — a summary</span>',
        "      </li>",
        "    </ul>",
        "  );",
        "}",
        "",
      ].join("\n"),
    );

    // The walk reaches the new page, and the rules judge it against the WHITE its
    // layout paints rather than the page background it does not.
    const found = findingsInTree(root);
    assert.deepEqual(
      found,
      [],
      `the cross-tree walk reported findings that are not defects:\n  ${found.join("\n  ")}`,
    );

    // Guard the guard: with the page's own markup made genuinely defective, the
    // same walk must report it — otherwise "no findings" above proves nothing.
    // `text-slate-400` (#90a1b9) is below AA on white too, so this is a defect
    // regardless of which surface the layout resolves to.
    write(
      "app/docs/api/page.tsx",
      [
        "export default function P() {",
        "  return (",
        "    <ul>",
        "      <li>",
        '        <code className="rounded bg-slate-100 px-1">/v1/rest/*</code>{" "}',
        '        <span className="text-slate-400"> — a summary</span>',
        "      </li>",
        "    </ul>",
        "  );",
        "}",
        "",
      ].join("\n"),
    );
    const mutated = findingsInTree(root);
    assert.ok(
      mutated.some((f) => f.includes("text-slate-400")),
      `the cross-tree walk stopped reporting a real defect: ${JSON.stringify(mutated)}`,
    );

    // And the layout chain is consulted, not assumed: with the layout's surface
    // removed, `text-slate-500` (fine on white, 4.48:1 on `--jf-bg`) becomes a
    // genuine defect on the page background, and must be reported.
    write(
      "app/docs/api/page.tsx",
      [
        "export default function P() {",
        "  return (",
        "    <ul>",
        "      <li>",
        '        <code className="rounded bg-slate-100 px-1">/v1/rest/*</code>{" "}',
        '        <span className="text-slate-500"> — a summary</span>',
        "      </li>",
        "    </ul>",
        "  );",
        "}",
        "",
      ].join("\n"),
    );
    write(
      "app/docs/layout.tsx",
      [
        "export default function L({ children }: { children: React.ReactNode }) {",
        "  return <main className=\"min-h-screen\">{children}</main>;",
        "}",
        "",
      ].join("\n"),
    );
    const noLayoutSurface = findingsInTree(root);
    assert.ok(
      noLayoutSurface.some((f) => f.includes("text-slate-500") && f.includes("page background")),
      `with no surface in the layout chain, the muted token on the page background was not ` +
        `reported: ${JSON.stringify(noLayoutSurface)}`,
    );
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

