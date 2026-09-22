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
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";

const SRC = fileURLToPath(new URL("../", import.meta.url)); // web/src/

/**
 * Blank out comments while keeping every byte's offset (and every newline).
 * A class quoted in a comment must neither raise nor satisfy an assertion — the
 * fixed sites carry comments explaining the fix, and they quote the class they
 * replaced.
 *
 * It is STRING-AWARE, and that is load-bearing rather than polish: the pages are
 * full of `placeholder="https://yourapp.com/thanks"`. A naive scanner reads the
 * `//` inside that string as a line comment and blanks the rest of the line —
 * including the string's CLOSING quote. The JSX scanner downstream then treats
 * the next `"` in the file as the end of the attribute, swallows the following
 * markup into the current tag, and reports that tag's className as carrying
 * classes from a completely different element. That produced four phantom
 * findings (<input> at builder/page.tsx:1182 with twelve tokens, <a> at
 * settings/page.tsx:1058, and two more) before this was made string-aware.
 *
 * `'` is only a string delimiter when it is not preceded by a word character, so
 * a prose apostrophe in JSX text (`don't`) does not open a string. An
 * unterminated `"`/`'` stops at the newline instead of running to the end of the
 * file, which bounds the damage of any other mis-read.
 */
function codeOnly(src: string): string {
  let out = "";
  let i = 0;
  let state: "code" | "line" | "block" | "str" = "code";
  let quote = "";
  while (i < src.length) {
    const c = src[i];
    const n = src[i + 1];
    if (state === "code") {
      if (c === "/" && n === "/") { state = "line"; out += "  "; i += 2; continue; }
      if (c === "/" && n === "*") { state = "block"; out += "  "; i += 2; continue; }
      // `"`/`` ` `` always open a string; `'` only in a literal position.
      if (c === '"' || c === "`" || (c === "'" && !/[\w$]/.test(src[i - 1] ?? ""))) {
        state = "str"; quote = c; out += c; i += 1; continue;
      }
      out += c; i += 1; continue;
    }
    if (state === "str") {
      if (c === "\\") { out += c + (n ?? ""); i += 2; continue; }
      if (c === quote) { state = "code"; out += c; i += 1; continue; }
      // An unterminated string stops at the newline rather than eating the file.
      if (c === "\n" && quote !== "`") { state = "code"; out += c; i += 1; continue; }
      out += c; i += 1; continue;
    }
    if (state === "line") { if (c === "\n") { state = "code"; out += c; } else out += " "; i += 1; continue; }
    if (c === "*" && n === "/") { state = "code"; out += "  "; i += 2; continue; }
    out += c === "\n" ? "\n" : " ";
    i += 1;
  }
  return out;
}

/**
 * Every .tsx under web/src, relative to web/src, sorted for a stable message.
 *
 * A walk rather than a manifest: the whole point is that a NEW page cannot add
 * an unguarded tinted surface. `.test.ts`/`.test.tsx` files are excluded — they
 * hold expected strings, not rendered markup.
 */
function tsxFiles(dir: string, base = SRC): string[] {
  const out: string[] = [];
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    const full = `${dir}${e.name}`;
    if (e.isDirectory()) {
      if (e.name === "node_modules" || e.name.startsWith(".")) continue;
      out.push(...tsxFiles(`${full}/`, base));
    } else if (e.name.endsWith(".tsx") && !/\.test\.tsx$/.test(e.name)) {
      out.push(full.slice(base.length));
    }
  }
  return out.sort();
}

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

// ─── className -> the class lists it can actually render ─────────────────────
//
// `className` is usually a template literal whose `${cond ? "a" : "b"}` islands
// are COMPLETE class lists of their own. Checking the flattened text of such an
// expression is wrong in both directions: a tint in the static prefix reads as
// paired with a token that lives in a different branch and never renders beside
// it (Shell's nav row), while a branch that only applies under one condition is
// invisible if you stop at the first alternative. So every `$`-island becomes an
// ALTERNATIVE (each string literal inside it, plus "" because a conditional
// whose else path is `undefined` renders no class at all) and the class lists
// are the cross product.
//
// Getting this wrong is how the FIRST version of this guard went green on a
// tree with the defect re-planted: it left the branch's quote characters on the
// class names, so `"bg-indigo-50` never matched the tint pattern.

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

// ─── a minimal JSX tag scanner ───────────────────────────────────────────────
//
// Enough to answer "is this token inside an element whose own className carries
// the tint?" plus the ancestor chain at any offset — the two shapes this defect
// takes. attrStart/attrEnd bound the element's own attribute list, so a
// `text-[…]` occurrence can be attributed to the element that owns it rather
// than being read as inherited from an ancestor.

type Tag = {
  name: string;
  variants: Variant[];
  openIdx: number;
  attrStart: number;
  attrEnd: number;
  closeIdx: number;
};

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
