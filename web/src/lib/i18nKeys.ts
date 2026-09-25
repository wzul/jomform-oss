// Static scan of the i18n CALL SITES — the keys the dashboard code actually asks
// for — so a key that no longer exists in the dictionary fails a gate instead of
// rendering as a raw key.
//
// THE DEFECT THIS EXISTS FOR
//   `t()` in ../components/i18n.tsx falls back to the KEY ITSELF when the entry is
//   absent:
//
//     let s = (!entry ? k : entry[lang] || entry.en) as string;
//
//   src/lib/i18n.test.ts guards the other direction only — for each key IN the
//   dictionary, all five locales must be set. A key that DISAPPEARS (dropped in a
//   merge resolution, renamed, or typed wrong at the call site) is invisible to
//   every gate: `workspace.askTitle` is rendered literally to the user in all five
//   locales, HTTP is 200, nothing errors. Same shape as the #499 `#ZgotmplZ`
//   rewrite: only visible by looking at the page. Measured on PR 689's head, nine
//   keys main had were absent from the conflicted i18nDict.ts hunk while the whole
//   suite stayed green.
//
// WHY A STATIC SCAN AND NOT A RUNTIME THROW
//   Throwing in render is worse than a raw key: it takes the whole page down
//   instead of one label, and it would ship the fault to users before any gate
//   sees it. The dictionary is a compile-time artefact and so is the call-site key
//   list, so the check belongs in the suite (`npm run test:unit`, which the `web`
//   preflight gate runs before the Next build). No browser, no DB, deterministic.
//
// WHAT IT RESOLVES (and what it cannot — stated honestly, see src/lib/i18nKeys.test.ts)
//   * `t("a.b")` / `t('a.b')` — a quoted literal argument. Resolved.
//   * `t(`a.b`)` — a template with NO interpolation. Resolved (the static text IS
//     the key).
//   * `t(`fs.x${c ? "A" : "B"}`)` — a template whose every `${…}` is a string
//     literal or a single-level ternary of them. Resolved into every key it can
//     produce (bounded; a bigger explosion is refused rather than guessed).
//   * `t(c ? "a.b" : "c.d")` — a ternary of literals, and the same behind a
//     `as Key` / `as const` cast and/or one wrapping pair of parens. Resolved.
//   * `t(`prof.${id}`)`, `t(`bld.status${s[0].toUpperCase()}${s.slice(1)}`)`,
//     `t(passedInKey)` — anything whose key depends on runtime data. NOT resolved:
//     the string exists only at runtime. These are counted and reported as
//     `dynamic`, and the test pins BOTH a floor on what must stay resolved and a
//     ceiling on the dynamic remainder, so this cannot quietly degrade into "we
//     only check the easy half".
//
// This module holds no DICT import: it answers "which keys does the code ask for",
// the caller compares that against the dictionary.

import { readdirSync, statSync } from "node:fs";
import { join } from "node:path";

/** One `t(...)` call site: the key it asks for and where it is written. */
export type CallSite = { key: string; file: string; line: number; expr: string };

/** A `t(...)` whose key is produced from runtime data, so this scan cannot vouch for it. */
export type DynamicSite = { file: string; line: number; expr: string };

export type ScanResult = {
  /** Literal + statically-resolved call sites, one entry per key produced. */
  static: CallSite[];
  /** The subset of `static` that came from an interpolated template/ternary. */
  resolved: CallSite[];
  /** Call sites it could not resolve — the honest remainder. */
  dynamic: DynamicSite[];
  /** How many source files were read. */
  files: number;
};

type Lit =
  | { kind: "string"; value: string }
  | { kind: "template"; segments: string[]; exprs: string[] };

const isNewline = (c: string) => c === "\n" || c === "\r";

/** The span of every literal (and of every comment) found by maskSource. */
type Spans = { lits: Map<number, Lit>; ends: Map<number, number> };

/**
 * Blank every byte that is NOT code — comment bodies and the static text of
 * string/template literals — while keeping the source's length and every
 * newline, so an index into the masked copy is an index into the original.
 *
 * Comment masking is why `// t("commented.out")` and `const s = 't("inString")'`
 * cannot be mistaken for call sites. The literal DELIMITERS and the `${…}`
 * expressions are kept (a `t(...)` inside a template expression is real code), and
 * each literal's value and span are recorded against its opening-quote index.
 */
function maskSource(src: string): { masked: string } & Spans {
  const out = src.split("");
  const lits = new Map<number, Lit>();
  const ends = new Map<number, number>();
  const blank = (from: number, to: number) => {
    for (let k = from; k < to && k < out.length; k++) if (!isNewline(src[k])) out[k] = " ";
  };

  /** `i` is the opening quote. Returns the index just past the literal. */
  function readString(i: number): number {
    const quote = src[i];
    let j = i + 1;
    let value = "";
    while (j < src.length) {
      const c = src[j];
      if (c === "\\") {
        const next = src[j + 1] ?? "";
        // Only the escapes that can appear in a dictionary key matter; keep the
        // escaped character itself so `\"` does not terminate the literal.
        value += next === quote || next === "\\" ? next : c + next;
        j += 2;
        continue;
      }
      if (c === quote || isNewline(c)) break; // newline = unterminated literal; stop
      value += c;
      j++;
    }
    lits.set(i, { kind: "string", value });
    ends.set(i, j + 1);
    blank(i + 1, j);
    return j + 1;
  }

  /**
   * True when an apostrophe is PROSE, not a string opener. Two rules, both
   * one-way (they can only DECLINE to open, never open where a literal was not):
   *
   *   1. The previous non-space character is an identifier character. In valid
   *      TypeScript a string literal is always preceded by an operator, a
   *      separator or whitespace — `It's`, `don't`, `the user's` are none of
   *      those. This is the JSX-prose class, and it is the one that occurs here:
   *      measured on this tree, 536 `letter'letter` occurrences.
   *   2. There is no unescaped closing quote before the end of the line. A string
   *      literal cannot contain a raw newline, so "no closer on this line" means
   *      it was never a literal. `typeof'x'` is the only valid shape this
   *      misreads and it does not exist in this repo; a real multi-line
   *      single-quoted string is a syntax error.
   *
   * Both rules exist because being WRONG here is asymmetric: declining to open a
   * real literal costs a false POSITIVE only if that literal's text happens to
   * contain `t("…")`, while wrongly opening on prose BLANKS every call site after
   * it on the line — `<p>It's {t("nav.dashboard")}</p>` — and a guard that
   * silently stops seeing call sites is the failure mode this repo keeps
   * re-learning.
   */
  function quoteIsProse(i: number): boolean {
    let p = i - 1;
    while (p >= 0 && (src[p] === " " || src[p] === "\t")) p--;
    if (p >= 0 && /[A-Za-z0-9_$]/.test(src[p])) return true;
    const quote = src[i];
    let j = i + 1;
    while (j < src.length) {
      const c = src[j];
      if (c === "\\") { j += 2; continue; }
      if (isNewline(c)) return true; // ran off the line without a closer
      if (c === quote) return false;
      j++;
    }
    return true;
  }

  /** `i` is the opening backtick. Returns the index just past the literal. */
  function readTemplate(i: number): number {
    const segments: string[] = [];
    const exprs: string[] = [];
    let text = "";
    let j = i + 1;
    while (j < src.length) {
      const c = src[j];
      if (c === "\\") {
        text += src[j + 1] ?? "";
        blank(j, j + 2);
        j += 2;
        continue;
      }
      if (c === "`") break;
      if (c === "$" && src[j + 1] === "{") {
        // An expression: keep the code (so a nested t(...) is still seen), record
        // its text for ternary resolution.
        segments.push(text);
        text = "";
        const exprStart = j + 2;
        let depth = 1;
        j += 2;
        while (j < src.length) {
          const d = src[j];
          // Nested literals must be READ (not just skipped): the main loop never
          // walks into a template's `${…}`, so this is the only place they get a
          // registered span — and an unregistered string inside an expression
          // both escapes masking and breaks the brace-depth scan when it holds a
          // `}` or a `(`.
          if (d === '"' || d === "'") { j = quoteIsProse(j) ? j + 1 : readString(j); continue; }
          if (d === "`") { j = readTemplate(j); continue; }
          if (src.startsWith("//", j)) { while (j < src.length && !isNewline(src[j])) j++; continue; }
          if (d === "(" || d === "[" || d === "{") depth++;
          else if (d === ")" || d === "]" || d === "}") { depth--; if (depth === 0) break; }
          j++;
        }
        exprs.push(src.slice(exprStart, j));
        j++;
        continue;
      }
      // Static template text: part of the key, never code.
      text += c;
      if (!isNewline(c)) out[j] = " ";
      j++;
    }
    segments.push(text);
    lits.set(i, { kind: "template", segments, exprs });
    ends.set(i, j + 1);
    return j + 1;
  }

  let i = 0;
  while (i < src.length) {
    const c = src[i];
    if (c === "/" && src[i + 1] === "/") {
      let j = i;
      while (j < src.length && !isNewline(src[j])) j++;
      blank(i, j);
      i = j;
      continue;
    }
    if (c === "/" && src[i + 1] === "*") {
      const end = src.indexOf("*/", i + 2);
      const j = end === -1 ? src.length : end + 2;
      blank(i, j);
      i = j;
      continue;
    }
    // Quote characters (and comments) inside a template expression are prose or
    // separators, not string openers — see quoteIsProse. A quote there must not
    // blank the call sites that follow it.
    if (c === '"' || c === "'") {
      if (quoteIsProse(i)) { i++; continue; }
      i = readString(i);
      continue;
    }
    if (c === "`") { i = readTemplate(i); continue; }
    i++;
  }
  return { masked: out.join(""), lits, ends };
}

const isSpace = (c: string | undefined) => c !== undefined && /\s/.test(c);

/** Skip a literal (any kind) or a comment starting at `i`, else `i + 1`. */
function step(src: string, spans: Spans, i: number): number {
  const end = spans.ends.get(i);
  if (end !== undefined) return end;
  if (src[i] === "/" && src[i + 1] === "/") {
    let j = i;
    while (j < src.length && !isNewline(src[j])) j++;
    return j;
  }
  if (src[i] === "/" && src[i + 1] === "*") {
    const e = src.indexOf("*/", i + 2);
    return e === -1 ? src.length : e + 2;
  }
  return i + 1;
}

/** Index of the `)` closing the call whose `(` is at `open`, or -1. */
function matchParen(src: string, spans: Spans, open: number): number {
  let depth = 0;
  let i = open;
  while (i < src.length) {
    const c = src[i];
    if (c === '"' || c === "'" || c === "`" || (c === "/" && (src[i + 1] === "/" || src[i + 1] === "*"))) {
      i = step(src, spans, i);
      continue;
    }
    if (c === "(") depth++;
    else if (c === ")") { depth--; if (depth === 0) return i; }
    i++;
  }
  return -1;
}

/** `ch` at nesting depth 0 within [start,end), or -1. */
function findTop(src: string, spans: Spans, start: number, end: number, ch: string): number {
  let depth = 0;
  let i = start;
  while (i < end) {
    const c = src[i];
    if (c === '"' || c === "'" || c === "`" || (c === "/" && (src[i + 1] === "/" || src[i + 1] === "*"))) {
      i = step(src, spans, i);
      continue;
    }
    if (c === "(" || c === "[" || c === "{") depth++;
    else if (c === ")" || c === "]" || c === "}") depth--;
    else if (c === ch && depth === 0) return i;
    i++;
  }
  return -1;
}

/** The value of a quoted string literal at `start`, or null. */
function stringAt(src: string, spans: Spans, start: number): string | null {
  const lit = spans.lits.get(start);
  return lit && lit.kind === "string" ? lit.value : null;
}

/** Drop a trailing TypeScript cast: `as Key`, `as const`, `as import("…").Key`. */
function stripCast(src: string, start: number, end: number): [number, number] {
  let s = start;
  let e = end;
  for (;;) {
    const text = src.slice(s, e);
    const m = text.match(/\s+as\s+(?:import\([^)]*\)(?:\.[A-Za-z_$][\w$]*)*|const|[A-Za-z_$][\w$.<>[\]]*)\s*$/);
    if (!m) return [s, e];
    e -= m[0].length;
    while (e > s && isSpace(src[e - 1])) e--;
  }
}

/**
 * Every dictionary key the argument expression spanning [start,end) can produce,
 * or null when the key is runtime-only. `end` is the index of the closing `)`.
 */
function keysInArg(src: string, spans: Spans, start: number, end: number): string[] | null {
  let s = start;
  let e = end;
  while (s < e && isSpace(src[s])) s++;
  while (e > s && isSpace(src[e - 1])) e--;
  [s, e] = stripCast(src, s, e);
  if (s >= e) return null;

  // One wrapping pair of parens around the whole thing: `(c ? "a" : "b")`.
  while (src[s] === "(" && matchParen(src, spans, s) === e - 1) {
    s++;
    e--;
    while (s < e && isSpace(src[s])) s++;
    while (e > s && isSpace(src[e - 1])) e--;
    [s, e] = stripCast(src, s, e);
    if (s >= e) return null;
  }

  const litEnd = spans.ends.get(s);
  if (litEnd !== undefined && litEnd === e) {
    const lit = spans.lits.get(s)!;
    if (lit.kind === "string") return [lit.value];
    if (lit.exprs.length === 0) return [lit.segments[0]];
    // Template with interpolations: each `${…}` must itself resolve.
    const parts: string[][] = [];
    for (const expr of lit.exprs) {
      const at = src.indexOf(expr, s);
      const alts = at === -1 ? null : keysInArg(src, spans, at, at + expr.length);
      if (!alts) return null;
      parts.push(alts);
    }
    let combos = [lit.segments[0]];
    for (let i = 0; i < parts.length; i++) {
      const next: string[] = [];
      for (const c of combos) for (const a of parts[i]) next.push(c + a + lit.segments[i + 1]);
      combos = next;
      if (combos.length > 8) return null; // refuse to explode; report as dynamic
    }
    return combos;
  }

  // A ternary of resolvable leaves: `c ? "a.b" : "c.d"`.
  const qm = findTop(src, spans, s, e, "?");
  if (qm > s) {
    const colon = findTop(src, spans, qm + 1, e, ":");
    if (colon > qm) {
      const a = keysInArg(src, spans, qm + 1, colon);
      const b = keysInArg(src, spans, colon + 1, e);
      if (a && b) return [...new Set([...a, ...b])];
    }
  }
  const only = stringAt(src, spans, s);
  if (only !== null && spans.ends.get(s) === e) return [only];
  return null;
}

const lineOf = (src: string, index: number) => src.slice(0, index).split("\n").length;

/**
 * Scan one source text. `file` is only used to label the results.
 *
 * A call site is `t(` where the `t` is a standalone identifier (not `foo.t(`,
 * `at(`, `format(`) — matched with a lookbehind on the MASKED text, so a `t(`
 * inside a comment or a plain string never counts.
 */
export function scanSource(src: string, file: string): {
  static: CallSite[];
  resolved: CallSite[];
  dynamic: DynamicSite[];
} {
  const { masked, lits, ends } = maskSource(src);
  const spans: Spans = { lits, ends };
  const staticSites: CallSite[] = [];
  const resolvedSites: CallSite[] = [];
  const dynamicSites: DynamicSite[] = [];
  const call = /(?<![\w$.])t\s*\(/g;
  let m: RegExpExecArray | null;
  while ((m = call.exec(masked)) !== null) {
    const open = m.index + m[0].length - 1; // the '('
    const close = matchParen(src, spans, open);
    const line = lineOf(src, m.index);
    if (close === -1) continue;
    const arg = src.slice(open + 1, close);
    // i18n keys are the FIRST argument; `t("k", { email })` must not drag the
    // params object into key resolution (it is not parseable as a key).
    const comma = findTop(src, spans, open + 1, close, ",");
    const argEnd = comma === -1 ? close : comma;
    const keys = keysInArg(src, spans, open + 1, argEnd);
    if (!keys) {
      dynamicSites.push({
        file,
        line,
        expr: `${m[0]}${arg.split("\n")[0].slice(0, 70)}${arg.includes("\n") ? "…" : ""}`,
      });
      continue;
    }
    const interpolated = arg.includes("${") || arg.includes("?");
    const site = (key: string): CallSite => ({ key, file, line, expr: `t(${arg.split("\n")[0]})` });
    for (const k of keys) {
      staticSites.push(site(k));
      if (interpolated) resolvedSites.push(site(k));
    }
  }
  return { static: staticSites, resolved: resolvedSites, dynamic: dynamicSites };
}

/** `.ts`/`.tsx` sources under `dir`, skipping build output and the tests themselves. */
export function sourceFiles(dir: string): string[] {
  const out: string[] = [];
  const skip = new Set(["node_modules", ".next", ".git"]);
  const walk = (d: string) => {
    for (const name of readdirSync(d)) {
      if (skip.has(name)) continue;
      const p = join(d, name);
      if (statSync(p).isDirectory()) walk(p);
      else if (/\.tsx?$/.test(name) && !/\.test\.tsx?$/.test(name)) out.push(p);
    }
  };
  walk(dir);
  return out.sort();
}

/**
 * Scan a whole tree. `rootDir` is `<repo>/web/src`.
 *
 * `.test.ts`/`.test.tsx` files are excluded: the `t("…")` inside them are
 * assertions ABOUT a source file (e.g. `assert.ok(page.includes('t("x")'))`), not
 * keys the dashboard renders. A key a test only quotes is still caught at the real
 * call site.
 */
export function scanTree(rootDir: string, read: (file: string) => string): ScanResult {
  const staticSites: CallSite[] = [];
  const resolved: CallSite[] = [];
  const dynamic: DynamicSite[] = [];
  const files = sourceFiles(rootDir);
  for (const file of files) {
    const src = read(file);
    const r = scanSource(src, file);
    staticSites.push(...r.static);
    resolved.push(...r.resolved);
    dynamic.push(...r.dynamic);
  }
  return { static: staticSites, resolved, dynamic, files: files.length };
}
