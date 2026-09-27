// Repo-wide guard: a BASE text token must not be painted on a TINTED surface.
//
// WHAT WAS WRONG (measured by axe on a production build, not a source grep)
//   Tailwind's `bg-indigo-50` (#eef2ff) is the app's "active / selected / hover"
//   surface. The two base text tokens do not clear WCAG AA on it:
//     --jf-primary (#5e6ad2) = 4.20:1
//     --jf-muted   (#6b7280) = 4.32:1
//   both under the 4.5:1 floor for the 14px/12px text these chips and buttons
//   use. They DO pass on white (4.70:1 / 4.83:1), which is exactly why the
//   defect hides: the same class list is fine until the tint appears.
//
//   Found in three rounds, each time in a file the previous guard did not own:
//     1. the sidebar nav row            (Shell.tsx, PR 667)
//     2. the workspace switcher's item  (WorkspaceSwitcher.tsx)
//     3. the page-level chips           (this file's card): the builder's
//        form-mode toggle at 4.2:1 and ProductEditor's Simple/Variable chip at
//        4.2:1, both axe-confirmed `impact: serious`, plus ~14 more sites.
//   A rule scoped to a hand-listed set of files is what let this class survive
//   three rounds, so this guard walks the whole `web/src` tree. Adding a new
//   page cannot add an unguarded site.
//
//   A FOURTH round came at the same defect from a different surface and is NOT
//   covered by this file (card t_5f315891): the builder canvas price spans use
//   the base `--jf-primary` on `bg-slate-50` (#f8fafc), where it measures
//   4.492:1 — 0.008 under the floor, while the same token on white measures
//   4.700:1. This guard cannot own it for two reasons: the tint it looks for is
//   indigo-50, and a source scan cannot measure a margin that small (the whole
//   question is the ratio, not the class list). It is guarded instead by
//   `web/e2e/builder-canvas-price-contrast.spec.ts`, which measures the ratio in
//   a real browser. If a fifth round appears on another surface, prefer a
//   measured guard over a class-name rule: this file is the fallback for
//   surfaces the browser gate cannot reach.
//
// THE FIX IS THE TOKEN, NOT THE CLASS
//   `--jf-primary-on-tint` (#4338ca) measures 7.07:1 on the tint and
//   `--jf-muted-on-tint` (#5b6472) 5.35:1; both also pass OFF the tint
//   (7.90:1 / 5.98:1 on white), so a site whose tint is conditional can use them
//   unconditionally. Do NOT darken `--jf-primary` itself — that was rejected as
//   a decision because it shifts every other use of the token.
//
// SCOPE OF *THIS* RULE
//   Only SOLID tints (`bg-indigo-50`) are flagged. The translucent variants
//   (`bg-indigo-50/40|/50|/60`) composite over white to #f8faff / #f7f9ff /
//   #f5f7ff, where --jf-primary measures 4.50 / 4.46 / 4.39:1 — i.e. at the
//   floor rather than clearly under it, and one of the three actually passes.
//   A source guard cannot know what is behind a translucent tint, so flagging
//   them here would be a guess; they are measured per site in the browser
//   instead. `bg-indigo-100` (#e0e7ff) is likewise a DIFFERENT, darker tint
//   (3.81:1 with --jf-primary) and is tracked separately, not folded in.
//
// WHY A SOURCE GUARD AND NOT THE BROWSER GATE
//   `web/e2e/a11y.spec.ts` scans PUBLIC routes only (/login, /landing,
//   /reset-password, /magic-link, /brand, /legal/*). Every page above is behind
//   a session, so `npm run a11y` cannot reach any of them. The EXECUTED proof is
//   the axe run quoted on the card; this is the part that stops it coming back.
//
// The class-list / JSX-shape parser (`codeOnly`, `jsxTags`, `tsxFiles`, …) is
// shared from `web/src/lib/jsxSource.ts`. It used to be a byte-identical copy of
// the one in `mutedText.test.ts`, and that copy was not harmless: the duplicated
// `codeOnly()` read a `/`+`*` or `/`+`/` sitting in JSX TEXT as a comment opener,
// so `web/src/app/docs/api/page.tsx:175` (`... <code>/v1/rest/*</code>`) made it
// blank 41 lines of that page — including the `</code>` that closes the tag. The
// fix lives in the module; this file now uses it instead of its own copy.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { codeOnly, jsxTags, ancestorsAt, tsxFiles } from "./jsxSource.ts";

const SRC = fileURLToPath(new URL("../", import.meta.url)); // web/src/

/** True when a single class token paints a SOLID indigo-50 tint (any state). */
function paintsTint(cls: string): boolean {
  // `bg-indigo-50`, `hover:bg-indigo-50`, `focus:bg-indigo-50`, `md:bg-indigo-50`
  // ... but NOT `bg-indigo-50/40` (translucent) and NOT `bg-indigo-100`.
  return TINT_RE.test(cls);
}

// Solid-tint class with its state prefix split out: "" for the resting state,
// "hover" / "focus" / "md:hover"... for a state-qualified one.
const TINT_RE = /^((?:[a-z-]+:)*)bg-indigo-50$/;
// Any text token declared through a --jf-* custom property, base or on-tint.
const ANY_JF_TEXT_RE = /^((?:[a-z-]+:)*)text-\[var\(--jf-[a-z-]+\)\]$/;
// The two BASE tokens (the on-tint pair deliberately does not match).
const BASE_JF_TEXT_RE = /^text-\[var\(--jf-(?:primary|muted)\)\]$/;

/**
 * The base tokens this class list paints ON the tint.
 *
 * State-aware, because "painted on the tint" is a per-state question:
 *
 *   `bg-indigo-50 text-[var(--jf-primary)]`
 *     resting state is tinted and its text is the base token -> DEFECT.
 *
 *   `text-[var(--jf-primary)] hover:bg-indigo-50 hover:text-[var(--jf-primary-on-tint)]`
 *     the resting text is on white, where the base token passes (4.70:1), and
 *     the hover state overrides BOTH the surface and the text -> fixed. Flagging
 *     this would be wrong, and it is the shape the outline buttons use.
 *
 *   `text-[var(--jf-primary)] hover:bg-indigo-50`   (no text override)
 *     the bare token is still what is painted while hovered -> DEFECT.
 *
 * So: if the variant declares ANY text token in that state, only the
 * state-qualified ones count and a base token among them is the defect;
 * otherwise the bare tokens are still in effect and they count.
 */
function baseTokensOnTint(variant: string, state: string): string[] {
  const inState: { cls: string; base: boolean }[] = [];
  const bare: { cls: string; base: boolean }[] = [];
  for (const cls of variant.split(/\s+/)) {
    const m = ANY_JF_TEXT_RE.exec(cls);
    if (!m) continue;
    const entry = { cls, base: BASE_JF_TEXT_RE.test(cls) };
    if (m[1] === state) inState.push(entry);
    else if (m[1] === "") bare.push(entry);
  }
  const effective = inState.length > 0 ? inState : bare;
  return effective.filter((e) => e.base).map((e) => e.cls);
}

/** Findings for one file: shape (a) same class list, shape (b) inherited. */
function findingsIn(rel: string, src: string): string[] {
  const code = codeOnly(src);
  const events = jsxTags(code);
  const findings: string[] = [];

  // (a) Same element: one class list that renders BOTH a tint and, in the state
  //     that tint applies, a base token. This is the shape the page-level chips
  //     have (`bg-indigo-50 text-[var(--jf-primary)]`), and also the shape a
  //     hover-tinted button has when it does NOT override its text on hover
  //     (`text-[…primary] hover:bg-indigo-50`) — the base token is what is
  //     painted while the tint is showing, which is the same AA obligation.
  //     A state-qualified on-tint override clears it (baseTokensOnTint).
  for (const e of events) {
    for (const v of e.tag.variants) {
      const tintStates = v
        .split(/\s+/)
        .map((cls) => TINT_RE.exec(cls)?.[1])
        .filter((s): s is string => s !== undefined);
      if (tintStates.length === 0) continue;
      const toks = [...new Set(tintStates.flatMap((s) => baseTokensOnTint(v, s)))];
      if (toks.length === 0) continue;
      const line = code.slice(0, e.tag.openIdx).split("\n").length;
      findings.push(
        `${rel}:${line} — <${e.tag.name}> paints ${toks.join(" + ")} on bg-indigo-50`,
      );
    }
  }

  // (b) Inherited: the token sits on a descendant of a tinted element, so it is
  //     painted on the tint even though its own class list is clean.
  //
  //     The token is attributed to the element that OWNS it (the tag whose
  //     attribute list contains the offset). A token written in the same
  //     element's className was already reported by (a) — reporting it again as
  //     "inherited" would double-count one site.
  //
  //     An ancestor only counts here when its tint is UNQUALIFIED, because the
  //     descendant's token and the ancestor's tint would have to be in the same
  //     state to overlap and a source scan cannot pair states across elements.
  //     That keeps this shape a real defect (the switcher's sub-line on a
  //     resting tint) and not a guess.
  const TOKEN_RE = /text-\[var\(--jf-(?:primary|muted)\)\]/g;
  let m: RegExpExecArray | null;
  while ((m = TOKEN_RE.exec(code))) {
    const owner = events.find((e) => e.tag.attrStart < m!.index && m!.index < e.tag.attrEnd)?.tag;
    if (!owner) continue;
    const ownIsBad = owner.variants.some((v) =>
      v.split(/\s+/).some((cls) => {
        const s = TINT_RE.exec(cls)?.[1];
        return s !== undefined && baseTokensOnTint(v, s).length > 0;
      }),
    );
    if (ownIsBad) continue; // already (a)
    const tintedEl = ancestorsAt(events, owner.openIdx).find(
      (t) => t !== owner && t.variants.some((v) => v.split(/\s+/).some((cls) => TINT_RE.exec(cls)?.[1] === "")),
    );
    if (!tintedEl) continue;
    const line = code.slice(0, m.index).split("\n").length;
    findings.push(
      `${rel}:${line} — ${m[0]} is inside <${tintedEl.name}> which paints bg-indigo-50`,
    );
  }

  // A finding must be reported once per site, not once per variant.
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

test("no base text token is painted on a tinted surface (repo-wide)", () => {
  const findings = allFindings();
  assert.deepEqual(
    findings,
    [],
    `Text on a bg-indigo-50 surface must use the on-tint tokens ` +
      `(--jf-primary-on-tint / --jf-muted-on-tint). --jf-primary measures 4.20:1 and ` +
      `--jf-muted 4.32:1 on #eef2ff (#eef2ff is Tailwind indigo-50), both under the ` +
      `4.5:1 AA floor for the 12px/14px text these chips and buttons use, while ` +
      `#4338ca / #5b6472 measure 7.07:1 / 5.35:1:\n  ${findings.join("\n  ")}`,
  );
});

test("the repo-wide rule is looking at real, reachable markup", () => {
  // The guard above passes on an empty file list, so pin that the walk found the
  // app, that tinted surfaces still exist in it, and that the on-tint token is
  // still what those surfaces use. Without this, a broken walk (or a refactor
  // that drops the tint entirely) reads as a clean pass.
  assert.ok(
    FILES.length >= 40,
    `the walk found only ${FILES.length} .tsx files under web/src — it is not reading the app`,
  );
  assert.ok(
    FILES.includes("components/Shell.tsx") && FILES.includes("app/builder/page.tsx"),
    `the walk is missing core files (found ${FILES.length})`,
  );

  const tintedFiles = FILES.filter((rel) => {
    const code = codeOnly(readFileSync(`${SRC}${rel}`, "utf8"));
    return [...code.matchAll(/(?:^|[\s"'`])bg-indigo-50(?![\w/.-])/g)].length > 0;
  });
  assert.ok(
    tintedFiles.length >= 8,
    `only ${tintedFiles.length} files paint a solid bg-indigo-50 tint — if the tinted ` +
      `surface moved or the tint class changed, re-anchor this guard rather than deleting it`,
  );

  const globals = readFileSync(`${SRC}app/globals.css`, "utf8");
  assert.match(
    globals,
    /--jf-primary-on-tint:\s*#4338ca/,
    `globals.css lost --jf-primary-on-tint — the AA-safe token the tinted surfaces need`,
  );
  assert.match(
    globals,
    /--jf-muted-on-tint:\s*#5b6472/,
    `globals.css lost --jf-muted-on-tint — the AA-safe token a tinted surface's muted line needs`,
  );
});
