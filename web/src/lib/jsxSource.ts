// Shared source-scanning helpers for the repo-wide CSS guards
// (`mutedText.test.ts`, `tintedSurface.test.ts`).
//
// WHY THIS IS A MODULE AND NOT A COPY
//   Both guards began with their own `codeOnly()`, and the header of the first
//   one already said it: "if a third guard ever needs this parser, extract it
//   once rather than copying it again". This is that extraction, and it is not
//   cosmetic — the copy carried a defect that only surfaced on a tree with a
//   page neither guard had seen yet.
//
// THE DEFECT THIS FIXES (measured, not reasoned about)
//   `codeOnly()` blanks comments so a class quoted in a comment can neither
//   raise nor satisfy an assertion. It scanned raw characters and treated any
//   `/`+`*` (or `/`+`/`) in `code` state as a comment opener — it had no idea
//   which bytes are JSX *text*. `web/src/app/docs/api/page.tsx:175` renders the
//   literal route pattern in prose:
//
//     ... use it for <code className="rounded bg-slate-100 px-1">/v1/rest/*</code>
//
//   The text `/v1/rest/*` ends in `/`+`*`, so the lexer entered `block` and
//   blanked forward, swallowing that line's `</code>`. A comment's body is
//   SUPPOSED to be blanked, so nothing flagged it: the tag simply never closed,
//   `<code@175>` became a phantom ancestor of every later section, and 41 lines
//   of the file became invisible to all four rules — which is how three
//   findings were reported against spans 180 lines away in a different section.
//   Ten more sites in the same shape (`https://…` written as JSX text) were
//   being read as `//` line comments.
//
//   The sibling guard was hit by the same bytes. `tintedSurface.test.ts` walks
//   `web/src` too, and it carried a byte-identical `codeOnly()`, so
//   `docs/api/page.tsx` was blanked from line 175 the same way there — 41 lines
//   it could not judge. It reported nothing on that file before or after the
//   fix (the file paints `bg-slate-100`, not the `bg-indigo-50` tint that guard
//   tracks), so it was a silent blind spot rather than a false positive: a
//   defect planted in those 41 lines would not have been seen. Sharing this
//   module removes the blind spot in both guards at once, which is the reason
//   the extraction happens here instead of a second patch to the copy.
//
// THE RULE
//   A comment opener is only honoured outside JSX text. Deciding "is this byte
//   JSX text?" needs a real JSX parse — `<` in code is a comparison or a type
//   argument as often as it is a tag, and a `/` in JSX text is prose. So the
//   spans come from the TypeScript parser (`typescript` is already a
//   devDependency of this tree, and `next build` is the thing that requires it),
//   and the character-level walk keeps its existing string/nesting rules.
//
//   Offsets and newlines are preserved byte-for-byte either way: callers report
//   `file:line` from these offsets, and blanking a newline would shift every
//   line number after it.

import { readFileSync, readdirSync } from "node:fs";
import ts from "typescript";

/**
 * The `[start, end)` byte ranges of every JSX TEXT node in a `.tsx` source.
 *
 * JSX text is the literal children of an element — prose, punctuation and
 * anything else that is neither a tag nor a `{…}` expression. Those bytes are
 * markup, not code, so no comment can open inside them.
 */
export function jsxTextSpans(src: string): Array<[number, number]> {
  const sf = ts.createSourceFile(
    "guard.tsx",
    src,
    ts.ScriptTarget.Latest,
    /* setParentNodes */ true,
    ts.ScriptKind.TSX,
  );
  const spans: Array<[number, number]> = [];
  const walk = (node: ts.Node): void => {
    if (ts.isJsxText(node)) spans.push([node.getStart(sf), node.getEnd()]);
    ts.forEachChild(node, walk);
  };
  walk(sf);
  return spans;
}

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
 *
 * It is also JSX-TEXT-AWARE (see this module's header): a `/` that sits in an
 * element's children is markup, never a comment opener. That is the fix for the
 * `</code>`-swallowing mis-read above, and it is why this walk consults
 * `jsxTextSpans` instead of trusting the raw characters.
 */
export function codeOnly(src: string): string {
  const text = jsxTextSpans(src);
  // Offsets are ascending, so a single cursor beats a scan per byte.
  let ti = 0;
  const inJsxText = (idx: number): boolean => {
    while (ti < text.length && text[ti][1] <= idx) ti += 1;
    return ti < text.length && idx >= text[ti][0] && idx < text[ti][1];
  };

  let out = "";
  let i = 0;
  let state: "code" | "line" | "block" | "str" = "code";
  let quote = "";
  while (i < src.length) {
    const c = src[i];
    const n = src[i + 1];
    if (state === "code") {
      if (!inJsxText(i)) {
        if (c === "/" && n === "/") { state = "line"; out += "  "; i += 2; continue; }
        if (c === "/" && n === "*") { state = "block"; out += "  "; i += 2; continue; }
      }
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
 * Every `.tsx` under `dir`, relative to `base`, sorted for a stable message.
 *
 * A walk rather than a manifest: the point is that a NEW page cannot add an
 * unguarded site. `.test.ts`/`.test.tsx` files are excluded — they hold expected
 * strings, not rendered markup.
 */
export function tsxFiles(dir: string, base: string = dir): string[] {
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

/** Read a file as UTF-8; kept here so callers do not each import `node:fs`. */
export function readSource(path: string): string {
  return readFileSync(path, "utf8");
}

/**
 * The `layout.tsx` files that wrap an app-relative page, OUTERMOST first.
 *
 * `app/docs/api/page.tsx` is wrapped by `app/layout.tsx` then
 * `app/docs/layout.tsx`; `app/settings/page.tsx` by `app/layout.tsx` alone.
 * Files that are not `page.tsx` (components, fragments) return `[]`: a
 * component's ambient background is chosen by its caller, which a scan of this
 * tree cannot follow — see `ambientSurfaceClasses` for what that means.
 */
export function layoutChain(rel: string, allFiles: string[]): string[] {
  if (!/(^|\/)page\.tsx$/.test(rel)) return [];
  const parts = rel.split("/");
  parts.pop(); // drop `page.tsx`
  const chain: string[] = [];
  for (let n = 1; n <= parts.length; n += 1) {
    const candidate = `${parts.slice(0, n).join("/")}/layout.tsx`;
    if (allFiles.includes(candidate)) chain.push(candidate);
  }
  return chain;
}

/**
 * The class lists of the elements that enclose a layout's `{children}` slot —
 * i.e. the surfaces a page's markup is actually painted on, NEAREST first.
 *
 * This exists because rule (b)'s "no surface ancestor in this file, so the text
 * is on the page background" is a claim the file cannot always support: the
 * layout that wraps the page can paint a surface the page never mentions.
 * `web/src/app/docs/layout.tsx:61` is a live example —
 * `<main className="min-h-screen bg-white">` — which is why the docs pages'
 * `text-slate-500` measures 4.76:1 on white rather than 4.48:1 on
 * `--jf-bg` (#f7f8f8). Both numbers are real; the pairing is what differs.
 *
 * A layout that declares no light surface over its children returns an empty
 * list, so the caller keeps looking (next layout inward) or falls back.
 */
export function ambientSurfaceClasses(
  rel: string,
  allFiles: string[],
  srcOf: (relPath: string) => string,
): string[][] {
  const out: string[][] = [];
  // Innermost layout first: the nearest wrapper is the one that wins.
  for (const layoutRel of layoutChain(rel, allFiles).reverse()) {
    const src = srcOf(layoutRel);
    const code = codeOnly(src);
    const events = jsxTags(code);
    // `{children}` is the slot the page is rendered into. A layout can also
    // reach it through a prop (`props.children`), which this walk cannot follow;
    // such a layout contributes nothing rather than a guess.
    for (let from = 0; ; ) {
      const at = code.indexOf("{children}", from);
      if (at === -1) break;
      from = at + 1;
      const owner = events.find((e) => e.tag.attrStart <= at && at <= e.tag.attrEnd)?.tag;
      const chain = ancestorsAt(events, at);
      // `owner` is the innermost element whose attribute list covers the slot;
      // the enclosing elements follow. Nearest first for the caller.
      const enclosing = owner && chain.includes(owner) ? chain.slice(chain.indexOf(owner) + 1) : chain;
      for (let k = enclosing.length - 1; k >= 0; k -= 1) {
        if (enclosing[k].variants.length > 0) out.push(enclosing[k].variants);
      }
      break; // one `{children}` slot per layout
    }
  }
  return out;
}

// ─── a minimal JSX tag scanner ───────────────────────────────────────────────
//
// Enough to answer "is this token inside an element whose own className carries
// the surface?" plus the ancestor chain at any offset — the shapes these CSS
// guards take. attrStart/attrEnd bound the element's own attribute list, so a
// token occurrence can be attributed to the element that owns it rather than
// being read as inherited from an ancestor.

export type Variant = string;

export type Tag = {
  name: string;
  variants: Variant[];
  openIdx: number;
  attrStart: number;
  attrEnd: number;
  closeIdx: number;
  /**
   * The element's `className` expression with its `{…}` container stripped and
   * its whitespace collapsed, or "" when the element declares none.
   */
  classNameExpr: string;
  /**
   * True when part of that expression is not something this scanner can read —
   * a prop, an import, a call it cannot follow. Such an element is NOT judged
   * clean by a rule that only looks at `variants`: the caller has to say so out
   * loud. `tintedSurface.test.ts` pins the sites that are legitimately
   * unreadable and fails on any new one.
   */
  classNameUnreadable: boolean;
};

/** Element open/close events, in source order. */
export type Event = { open: boolean; tag: Tag };

// ─── reading a `className` into the class lists it can render ────────────────
//
// WHAT THIS USED TO MISS (measured on `web/src` before this was written)
//
//   The scanner handed everything after `className=` straight to a template
//   reader, so three shapes were read as an empty (or partial) class list and
//   the element was silently skipped — the failure mode a guard exists to
//   prevent, in the guard itself:
//
//     1. the `{…}` CONTAINER was read as class text. `className={`bg-indigo-50
//        text-[var(--jf-primary)]`}` has no `${…}` island, so nothing separated
//        the container's braces from the class list and the tokens came back as
//        `{bg-indigo-50` and `text-[var(--jf-primary)]}` — neither matches a
//        class regex, so that element was invisible in BOTH directions.
//     2. `className={cls}` / `className={`  ${tintCls(active)}`}` — a local
//        constant or a local helper call is not a literal at all, so the
//        variants were `{cls}` / `rounded ` and the rendered tint was never
//        read.
//     3. a ternary written directly in the container (`className={cond ? "…" :
//        "…"}`) was flattened into ONE variant with the condition operands glued
//        in, which MERGES branches that never co-paint. That is a false-positive
//        risk, not a miss, and it is why the branches are resolved properly
//        below instead of being read as one class list.
//
// HOW IT READS NOW
//
//   `stripWrapper` drops the container (and any single wrapping paren), then the
//   expression is resolved by shape, in precedence order:
//
//     "…" / '…'        one class list
//     `…`              its static text, with each `${…}` island resolved and
//                      CONCATENATED
//     [ … ]            its elements resolved and concatenated (the `.join(" ")`
//                      tail is what makes them one class list)
//     cond ? a : b     ALTERNATIVES — a and b are never joined
//     a || b, a ?? b   alternatives
//     a && b           b's alternatives, or "" (a falsy left operand renders
//                      nothing)
//     a + b            concatenated
//     name / name(…)   resolved through a LOCAL declaration in the same file —
//                      `const labelCls = "…"`, `const tint = (on) => on ? "…" :
//                      "…"`, `function cls() { return "…" }`. This is the shape
//                      the card was filed for.
//     anything else    UNREADABLE, and reported as such: a prop
//                      (`className={className}`, the caller owns the list), an
//                      import, a call this file cannot follow. Marking it beats
//                      guessing, and `Tag.classNameUnreadable` is what carries
//                      it to the caller — a guard that cannot read an element
//                      must not report it clean.
//
//   Depth-capped (MAX_DEPTH) so a self-referential declaration terminates.

/** The closer for each bracket that can wrap an expression piece. */
const CLOSERS: Record<string, string> = { "{": "}", "(": ")", "[": "]" };

/**
 * The index of the bracket that closes the one at `openIdx`, skipping brackets
 * that sit in a string, a template literal or a nested bracket pair.
 */
function matchPair(src: string, openIdx: number): number {
  const stack: string[] = [];
  let i = openIdx;
  while (i < src.length) {
    const c = src[i];
    if (c === '"' || c === "'" || c === "`") {
      const q = c;
      i += 1;
      while (i < src.length && src[i] !== q) {
        if (src[i] === "\\") i += 1;
        i += 1;
      }
    } else if (CLOSERS[c]) {
      stack.push(CLOSERS[c]);
    } else if (stack.length > 0 && c === stack[stack.length - 1]) {
      stack.pop();
      if (stack.length === 0) return i;
    }
    i += 1;
  }
  return src.length;
}

/** An operator that can appear at bracket depth 0 in a class-list expression. */
type Mark = { at: number; op: "?" | ":" | "+" | "&&" | "||" | "??" | ","; len: number };

/**
 * Every top-level operator in `src`, in source order.
 *
 * "Top level" means bracket depth 0 and outside strings/templates: a `?` in a
 * template's static text is prose, and a `+` inside `(a ? "x" : "y")` belongs to
 * the parenthesised branch, not to this expression.
 */
function scanTopLevel(src: string): Mark[] {
  const marks: Mark[] = [];
  const stack: string[] = [];
  let i = 0;
  while (i < src.length) {
    const c = src[i];
    if (c === '"' || c === "'" || c === "`") {
      const q = c;
      i += 1;
      while (i < src.length && src[i] !== q) {
        if (src[i] === "\\") i += 1;
        i += 1;
      }
      i += 1;
      continue;
    }
    if (CLOSERS[c]) {
      stack.push(CLOSERS[c]);
      i += 1;
      continue;
    }
    if (stack.length > 0 && c === stack[stack.length - 1]) {
      stack.pop();
      i += 1;
      continue;
    }
    if (stack.length === 0) {
      const n = src[i + 1] ?? "";
      const p = src[i - 1] ?? "";
      if (c === "?" && n === ".") {
        i += 2; // optional chaining: `a?.b` is a reference, not a conditional
        continue;
      }
      if (c === "?" && n === "?") {
        marks.push({ at: i, op: "??", len: 2 });
        i += 2;
        continue;
      }
      if (c === "?") {
        marks.push({ at: i, op: "?", len: 1 });
        i += 1;
        continue;
      }
      if (c === ":") {
        marks.push({ at: i, op: ":", len: 1 });
        i += 1;
        continue;
      }
      if (c === "+" && n !== "+" && n !== "=" && p !== "+") {
        marks.push({ at: i, op: "+", len: 1 });
        i += 1;
        continue;
      }
      if (c === "&" && n === "&") {
        marks.push({ at: i, op: "&&", len: 2 });
        i += 2;
        continue;
      }
      if (c === "|" && n === "|") {
        marks.push({ at: i, op: "||", len: 2 });
        i += 2;
        continue;
      }
      if (c === ",") {
        marks.push({ at: i, op: ",", len: 1 });
        i += 1;
        continue;
      }
    }
    i += 1;
  }
  return marks;
}

/** `src` split at every top-level occurrence of one of `ops`, or null if none. */
function splitOn(src: string, marks: Mark[], ops: Array<Mark["op"]>): string[] | null {
  const at = marks.filter((m) => ops.includes(m.op));
  if (at.length === 0) return null;
  const out: string[] = [];
  let from = 0;
  for (const m of at) {
    out.push(src.slice(from, m.at));
    from = m.at + m.len;
  }
  out.push(src.slice(from));
  return out;
}

/**
 * A ternary split into `[condition, then, else]`.
 *
 * The condition is returned rather than dropped because the caller has to
 * DISCARD it: a condition's operands (`active === "profile"`) are not class
 * lists, and reading them as one is how a guard invents findings.
 */
function splitTernary(src: string, marks: Mark[]): [string, string, string] | null {
  const q = marks.find((m) => m.op === "?" || m.op === "??");
  if (!q) return null;
  if (q.op === "??") return null; // handled as an alternative pair, not a branch
  let pending = 0;
  for (const m of marks) {
    if (m.at < q.at) continue;
    if (m.op === "?") pending += 1;
    else if (m.op === ":") {
      pending -= 1;
      if (pending === 0) return [src.slice(0, q.at), src.slice(q.at + 1, m.at), src.slice(m.at + 1)];
    }
  }
  // `cond ? "x"` with no else: the else path renders no class at all.
  return [src.slice(0, q.at), src.slice(q.at + 1), ""];
}

/** The index just past the backtick that closes the template at `src[0]`, or -1. */
function templateEnd(src: string): number {
  let i = 1;
  while (i < src.length) {
    const c = src[i];
    if (c === "\\") {
      i += 2;
      continue;
    }
    if (c === "`") return i + 1;
    if (c === "$" && src[i + 1] === "{") {
      i = matchPair(src, i + 1) + 1;
      continue;
    }
    i += 1;
  }
  return -1;
}

/**
 * The `className` VALUE out of an element's attribute list, and nothing else.
 *
 * `attrText` is the element's whole attribute list, so a reader that took
 * everything after `className=` also swallowed the attributes that follow it:
 * on `className={`…`}` + `title={t("…")}` the class list came back with
 * `title={t(...)}` welded on the end, and once the container's braces stopped
 * being treated as class text that tail made the whole expression unreadable —
 * which a rule that falls back to "no surface declared" then mis-reports. The
 * value ends at its own `}` / closing quote / closing backtick.
 */
function classNameValue(attrText: string, from: number): string {
  const rest = attrText.slice(from).replace(/^\s+/, "");
  if (rest === "") return "";
  const c = rest[0];
  if (c === "{") {
    const end = matchPair(rest, 0);
    return end < rest.length ? rest.slice(0, end + 1) : rest;
  }
  if (c === '"' || c === "'") {
    const lit = readQuoted(rest);
    return lit ? rest.slice(0, lit.end) : rest;
  }
  if (c === "`") {
    const end = templateEnd(rest);
    return end > 0 ? rest.slice(0, end) : rest;
  }
  return rest.split(/[\s>{]/)[0];
}

/** `src` without a single `(…)`/`{…}` wrapper, when that wrapper is the whole expression. */
function stripWrapper(src: string): string {
  if (src[0] === "(" || src[0] === "{") {
    const end = matchPair(src, 0);
    if (end === src.length - 1) return src.slice(1, end).trim();
  }
  return src;
}

/** A string literal read off the front of an expression, and where it ended. */
type Quoted = { value: string; end: number };

/**
 * The string literal `src` starts with.
 *
 * `end` is returned rather than assumed to be `src.length`: `"a" + "b"` starts
 * with a literal that is only PART of the expression, and a reader that stopped
 * at the closing quote and reported `a` would drop the rest while claiming a
 * complete read — which is exactly the silent-skip failure this module exists to
 * not have.
 */
function readQuoted(src: string): Quoted | null {
  const q = src[0];
  if (q !== '"' && q !== "'") return null;
  let out = "";
  let i = 1;
  while (i < src.length) {
    const c = src[i];
    if (c === "\\") {
      out += unescapeChar(src[i + 1] ?? "");
      i += 2;
      continue;
    }
    if (c === q) return { value: out, end: i + 1 };
    out += c;
    i += 1;
  }
  return null;
}

/** The character an escape sequence stands for, as far as a class list needs. */
function unescapeChar(c: string): string {
  if (c === "n") return "\n";
  if (c === "t") return "\t";
  if (c === "r") return "\r";
  return c;
}

/**
 * The class-list expressions a local declaration can produce.
 *
 * The TypeScript parser answers "what is this name", so `const tint = (on) =>
 * on ? "a" : "b"` resolves to its returned ternary and `function cls() { return
 * "a"; }` to its return statement. A declaration holding anything else (`const
 * n = 3`, an imported class map) contributes nothing, which is what leaves a
 * reference to it unreadable rather than guessed at.
 */
function classTexts(node: ts.Node, sf: ts.SourceFile): string[] {
  let n = node;
  while (ts.isParenthesizedExpression(n)) n = n.expression;
  if (ts.isArrowFunction(n) || ts.isFunctionExpression(n) || ts.isFunctionDeclaration(n)) {
    const body: ts.ConciseBody | undefined = n.body;
    if (!body) return [];
    if (!ts.isBlock(body)) return classTexts(body, sf);
    // EVERY returned class list, not just a top-level `return`: a `switch`
    // helper (`function badge(s) { switch (s) { case "paid": return "…" } }`)
    // returns from inside its cases, and reading only the first top-level
    // `return` left those badges unreadable. A nested function's returns are
    // its own and are not collected.
    const outs: string[] = [];
    const collect = (node: ts.Node): void => {
      if (node !== body && ts.isFunctionLike(node)) return;
      if (ts.isReturnStatement(node)) {
        if (node.expression) outs.push(...classTexts(node.expression, sf));
        return;
      }
      ts.forEachChild(node, collect);
    };
    collect(body);
    return outs;
  }
  const classLike =
    ts.isStringLiteral(n) ||
    ts.isNoSubstitutionTemplateLiteral(n) ||
    ts.isTemplateExpression(n) ||
    ts.isConditionalExpression(n) ||
    ts.isArrayLiteralExpression(n) ||
    // An object literal is a CLASS MAP held in a name: `const S = { paid: "…" }`
    // read at a use site as `S[s]`. Without it the name was absent from `locals`
    // entirely, so `className={S[s]}` came back as `resolved: false` — the reader
    // could not follow the shape the app's status badges use, and a guard that
    // sees no class list reports nothing. The map's text is read by key in
    // `resolveClassExpr`, which appends the access and hands it to
    // `resolveClassMap`; a bare `className={S}` stays unreadable on purpose (the
    // key is not known there, and guessing a value would be a false positive).
    ts.isObjectLiteralExpression(n) ||
    ts.isIdentifier(n) ||
    ts.isPropertyAccessExpression(n) ||
    ts.isCallExpression(n) ||
    (ts.isBinaryExpression(n) &&
      (n.operatorToken.kind === ts.SyntaxKind.PlusToken ||
        n.operatorToken.kind === ts.SyntaxKind.AmpersandAmpersandToken ||
        n.operatorToken.kind === ts.SyntaxKind.BarBarToken ||
        n.operatorToken.kind === ts.SyntaxKind.QuestionQuestionToken));
  return classLike ? [n.getText(sf)] : [];
}

/** Every local `const`/`function` name in `code` that can hold a class list. */
function localClassExprs(code: string): Map<string, string[]> {
  const sf = ts.createSourceFile("guard.tsx", code, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const out = new Map<string, string[]>();
  const add = (name: string, exprs: string[]) => {
    if (exprs.length === 0) return;
    const list = out.get(name);
    if (list) list.push(...exprs);
    else out.set(name, [...exprs]);
  };
  const walk = (node: ts.Node): void => {
    if (ts.isVariableDeclaration(node) && ts.isIdentifier(node.name) && node.initializer) {
      add(node.name.text, classTexts(node.initializer, sf));
    } else if (ts.isFunctionDeclaration(node) && node.name) {
      add(node.name.text, classTexts(node, sf));
    }
    ts.forEachChild(node, walk);
  };
  walk(sf);
  return out;
}

const MAX_DEPTH = 5;

/** What a class-list expression resolved to, and whether the read was complete. */
type Resolved = { alts: Variant[]; resolved: boolean };

/** Two halves of one class list joined — a space only when both are non-empty. */
function joinParts(a: string, b: string): string {
  return a && b ? `${a} ${b}` : a + b;
}

/**
 * The class lists formed by several pieces that render TOGETHER (a template's
 * static text plus its islands, an array's elements, a `+` chain).
 */
function cross(parts: Resolved[]): Variant[] {
  let out: Variant[] = [""];
  for (const p of parts) {
    const opts = p.alts.length > 0 ? p.alts : [""];
    const next: Variant[] = [];
    for (const o of out) {
      for (const a of opts) {
        next.push(joinParts(o, a));
        if (next.length >= 256) break;
      }
      if (next.length >= 256) break;
    }
    out = next;
  }
  return out;
}

/** Every alternative of several pieces that are mutually EXCLUSIVE. */
function union(parts: Resolved[]): Variant[] {
  return [...new Set(parts.flatMap((p) => p.alts))];
}

/** A resolved piece that also says how much of the source it consumed. */
type ResolvedSpan = Resolved & { end: number };

/**
 * A template literal: static text, with each `${…}` island resolved and joined.
 *
 * `end` is the index just past the closing backtick, or -1 when the template
 * never closes — `` `a` + `b` `` starts with a complete template, but it is not
 * the whole expression, and the caller has to split the `+` itself.
 */
function resolveTemplate(src: string, locals: Map<string, string[]>, depth: number): ResolvedSpan {
  const parts: Resolved[] = [];
  let buf = "";
  let resolved = true;
  let closed = false;
  let end = -1;
  let i = 1;
  while (i < src.length) {
    const c = src[i];
    if (c === "\\") {
      buf += unescapeChar(src[i + 1] ?? "");
      i += 2;
      continue;
    }
    if (c === "`") {
      closed = true;
      end = i + 1;
      break;
    }
    if (c === "$" && src[i + 1] === "{") {
      parts.push({ alts: [buf], resolved: true });
      buf = "";
      const braceEnd = matchPair(src, i + 1);
      const inner = resolveClassExpr(src.slice(i + 2, braceEnd), locals, depth + 1);
      if (!inner.resolved) resolved = false;
      parts.push(inner);
      i = braceEnd + 1;
      continue;
    }
    buf += c;
    i += 1;
  }
  parts.push({ alts: [buf], resolved: true });
  if (!closed) resolved = false; // an unterminated template is not readable
  return { alts: closed ? cross(parts) : [], resolved, end };
}

/** A method chain that only re-joins an array literal's elements, e.g. `.filter(Boolean).join(" ")`. */
const JOIN_TAIL_RE = /^(?:\.[A-Za-z_$][\w$]*\([^()]*\))*$/;

/** An array literal: its elements are class lists that the `.join(" ")` tail concatenates. */
function resolveArray(src: string, locals: Map<string, string[]>, depth: number): ResolvedSpan {
  const close = matchPair(src, 0);
  if (close >= src.length) return { alts: [], resolved: false, end: -1 }; // never closes
  const body = src.slice(1, close);
  const marks = scanTopLevel(body);
  const elements = splitOn(body, marks, [","]) ?? [body];
  const parts = elements
    .map((e) => e.trim())
    .filter((e) => e !== "")
    .map((e) => resolveClassExpr(e, locals, depth + 1));
  // Everything after `]` must be a join/filter chain; `[a][0]` or `[a] + b`
  // reads a different class list and is NOT this shape.
  const tail = src.slice(close + 1).trim();
  if (!JOIN_TAIL_RE.test(tail)) return { alts: [], resolved: false, end: -1 };
  // The join chain is part of the value this shape renders, so the shape
  // consumed the whole expression.
  if (parts.length === 0) return { alts: [""], resolved: true, end: src.length };
  return { alts: cross(parts), resolved: parts.every((p) => p.resolved), end: src.length };
}

/**
 * An inline CLASS MAP read by key: `({ ok: "…", bad: "…" })[status]`.
 *
 * The key is a name, not a class list, so the map's VALUES are the
 * alternatives — any of them can render. A status badge written
 * `({ paid: "…" })[s] || "…"` is this shape, and it is why a rule that followed
 * only string literals never saw the colours those badges paint.
 *
 * `end` is `src.length` (the access chain is part of the read), or -1 when this
 * is not the shape at all.
 */
function resolveClassMap(src: string, locals: Map<string, string[]>, depth: number): ResolvedSpan {
  const miss: ResolvedSpan = { alts: [], resolved: false, end: -1 };
  let body: string;
  let rest: string;
  if (src[0] === "(") {
    const close = matchPair(src, 0);
    body = src.slice(1, close);
    rest = src.slice(close + 1);
  } else if (src[0] === "{") {
    const close = matchPair(src, 0);
    body = src.slice(0, close + 1);
    rest = src.slice(close + 1);
  } else {
    return miss;
  }
  if (body.trim()[0] !== "{") return miss;
  const open = body.indexOf("{");
  const closeBrace = body.lastIndexOf("}");
  if (closeBrace <= open) return miss;
  // Only a property/index access may follow the object; `({…}) + x` is an
  // operand of something else and the split above owns it.
  if (!/^(?:\[[^\[\]]*\]|\.[A-Za-z_$][\w$]*)+$/.test(rest.trim())) return miss;
  const inner = body.slice(open + 1, closeBrace);
  const props = splitOn(inner, scanTopLevel(inner), [","]) ?? [inner];
  const parts: Resolved[] = [];
  for (const prop of props) {
    if (prop.trim() === "") continue;
    // A property's own ternary `:` sits at the same depth as the key's, so all
    // the pieces after the first colon are rejoined — the value is what renders.
    const value = splitOn(prop, scanTopLevel(prop), [":"]);
    if (!value) return miss; // a shorthand or spread property holds no class list
    parts.push(resolveClassExpr(value.slice(1).join(":"), locals, depth + 1));
  }
  if (parts.length === 0) return miss;
  return { alts: union(parts), resolved: parts.every((p) => p.resolved), end: src.length };
}

/**
 * A bare name or a call: resolved through a LOCAL declaration when there is one.
 *
 * `clsConst` (a `const` holding a string), `tintCls(active)` (a local arrow
 * whose body is the class list) and `function cls() { return "…" }` all resolve.
 * A prop (`className={className}` — the caller owns the list), an import or a
 * member access (`styles.label`) does not, and says so.
 */
function resolveReference(src: string, locals: Map<string, string[]>, depth: number): Resolved {
  const call = /^([A-Za-z_$][\w$]*)\s*(?:\??\.[\w$]+\s*)*\(/.exec(src);
  const bare = /^[A-Za-z_$][\w$]*$/.exec(src);
  const name = call ? call[1] : bare ? bare[0] : null;
  if (name === null || depth > MAX_DEPTH) return { alts: [], resolved: false };
  const bodies = locals.get(name);
  if (!bodies || bodies.length === 0) return { alts: [], resolved: false };
  const parts = bodies.map((b) => resolveClassExpr(b, locals, depth + 1));
  return { alts: union(parts), resolved: parts.every((p) => p.resolved) };
}

/** `undefined` / `null` / `false` render no class at all — a readable empty list. */
const RENDERS_NOTHING = /^(?:undefined|null|false|void 0)$/;

/**
 * The class lists one `className` expression can render, and whether every part
 * of it could be read.
 *
 * The `resolved` flag is the point of this function as much as the class lists
 * are: an expression the scanner cannot read must not come back looking like a
 * clean one. When only part of it resolves, the alternatives that WERE read are
 * still returned (a rule should see what it can), with `resolved: false` so the
 * caller can report the element as unjudged instead of clean.
 */
function resolveClassExpr(expr: string, locals: Map<string, string[]>, depth = 0): Resolved {
  const src = stripWrapper(expr.trim());
  if (src === "") return { alts: [""], resolved: true };
  if (depth > MAX_DEPTH) return { alts: [], resolved: false };
  if (RENDERS_NOTHING.test(src)) return { alts: [""], resolved: true };

  // A literal, a template or an array is only the WHOLE class list when nothing
  // follows it. Otherwise it is one operand of a `+`/ternary the split below
  // handles — and reading just its first operand while reporting a complete read
  // is how `"a" + "b"` came back as `a`.
  if (src[0] === '"' || src[0] === "'") {
    const lit = readQuoted(src);
    if (lit === null) return { alts: [], resolved: false }; // unterminated
    if (src.slice(lit.end).trim() === "") return { alts: [lit.value], resolved: true };
  }
  if (src[0] === "`") {
    const t = resolveTemplate(src, locals, depth);
    if (t.end > 0 && src.slice(t.end).trim() === "") return { alts: t.alts, resolved: t.resolved };
  }
  if (src[0] === "[") {
    const a = resolveArray(src, locals, depth);
    if (a.end > 0 && src.slice(a.end).trim() === "") return { alts: a.alts, resolved: a.resolved };
  }

  const marks = scanTopLevel(src);

  const ternary = splitTernary(src, marks);
  if (ternary) {
    const then = resolveClassExpr(ternary[1], locals, depth + 1);
    const otherwise = resolveClassExpr(ternary[2], locals, depth + 1);
    return { alts: union([then, otherwise]), resolved: then.resolved && otherwise.resolved };
  }

  // Precedence, loosest first: `||` / `??`, then `&&`, then `+`.
  const alt = splitOn(src, marks, ["||", "??"]);
  if (alt) {
    const parts = alt.map((p) => resolveClassExpr(p, locals, depth + 1));
    return { alts: union(parts), resolved: parts.every((p) => p.resolved) };
  }
  const and = splitOn(src, marks, ["&&"]);
  if (and) {
    // A falsy left operand renders nothing, so "" is always an alternative.
    const parts = and.map((p) => resolveClassExpr(p, locals, depth + 1));
    const last = parts[parts.length - 1];
    return { alts: [...last.alts, ""], resolved: last.resolved };
  }
  const concat = splitOn(src, marks, ["+"]);
  if (concat) {
    const parts = concat.map((p) => resolveClassExpr(p, locals, depth + 1));
    return { alts: cross(parts), resolved: parts.every((p) => p.resolved) };
  }

  if (src[0] === "(" || src[0] === "{") {
    const map = resolveClassMap(src, locals, depth);
    if (map.end > 0 && src.slice(map.end).trim() === "") return map;
  }

  // The same class map, held in a LOCAL name and read by key: `const S = { paid:
  // "…" }; S[s]`. `resolveClassMap` only takes the map's text inline, and the
  // map is the shape this repo's status badges are written in (a page map for
  // /sales, /deliveries, /admin suppressions, the dashboard e-mail status), so
  // leaving the name unresolvable meant an element whose className is a badge
  // lookup carried no class list at all and every rule skipped it in silence.
  //
  // The map's text and the ACCESS are joined before the read, so `S[s]` and
  // `S.cancelled` are read by the same code path as `({ paid: "…" })[s]`, and a
  // local holding anything that is not a map (`S` a string, an array) simply
  // does not resolve here and falls through to the unreadable path below.
  const localMap = /^([A-Za-z_$][\w$]*)\s*((?:\[[^[\]]*\]|\.[A-Za-z_$][\w$]*)+)$/.exec(src);
  if (localMap) {
    const bodies = locals.get(localMap[1]);
    if (bodies && bodies.length > 0) {
      const parts = bodies.map((b) => resolveClassMap(`${b}${localMap[2]}`, locals, depth + 1));
      if (parts.every((p) => p.end > 0)) {
        return {
          alts: union(parts.map((p) => ({ alts: p.alts, resolved: p.resolved }))),
          resolved: parts.every((p) => p.resolved),
        };
      }
    }
  }

  return resolveReference(src, locals, depth);
}

/**
 * Parse the source into tags and, for each, the index just past its closing
 * `</name>`. Pairing uses an explicit stack so a token's ancestor chain is
 * exact: a source-order "everything opened before this offset" heuristic gets
 * siblings and nested same-name elements wrong, and would either miss a defect
 * or flag clean markup.
 *
 * Each tag also carries the class lists its `className` renders (`variants`,
 * via `resolveClassExpr`) and whether that read was complete
 * (`classNameUnreadable`) — an element whose list is a prop or an import this
 * file cannot follow is NOT the same as an element with no classes, and a guard
 * that conflates them reports it clean.
 */
export function jsxTags(code: string): Event[] {
  const events: Event[] = [];
  const stack: Tag[] = [];
  const locals = localClassExprs(code);
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
    const value = cm ? classNameValue(attrText, cm.index + cm[0].length) : "";
    const read = value === "" ? null : resolveClassExpr(value, locals);
    const tag: Tag = {
      name,
      variants: read ? read.alts : [],
      classNameExpr: cm ? stripWrapper(value.trim()).replace(/\s+/g, " ") : "",
      classNameUnreadable: read !== null && !read.resolved,
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
export function ancestorsAt(events: Event[], offset: number): Tag[] {
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

/**
 * Every element opened INSIDE `owner` (not including itself), in source order.
 *
 * Rule (b2) needs these: an inline descendant that declares its own surface
 * repaints text it inherits from an ancestor, so a token that was legal on its
 * own element can still end up below 4.5:1 when it is carried into a child.
 */
export function descendantsAt(events: Event[], owner: Tag): Tag[] {
  const out: Tag[] = [];
  const chain: Tag[] = [];
  for (const e of events) {
    if (e.tag.openIdx > owner.closeIdx) break;
    if (e.open) {
      chain.push(e.tag);
      if (e.tag !== owner && chain.includes(owner)) out.push(e.tag);
    } else {
      const at = chain.lastIndexOf(e.tag);
      if (at !== -1) chain.length = at;
    }
  }
  return out;
}

// ─── the tinted-surface rule (ONE implementation, two guards) ────────────────
//
// "A BASE text token must not be painted on the TINTED surface" is a per-STATE
// question, and that is the whole reason the rule lives here instead of inside a
// guard: `tintedSurface.test.ts` applies it repo-wide by walking `web/src`, and
// `sidebarNav.test.ts` applies it to the dashboard chrome. Both call
// `tintedSurfaceFindings` below, so the two cannot disagree about one class list.
//
// They did disagree, and this module is the fix for it (card t_306dcc1e). The
// chrome copy treated a class list as a flat SET — ANY base token anywhere in it
// plus ANY `bg-indigo-50` anywhere in it was a finding — so the outline-button
// shape
//
//     `text-[var(--jf-primary)] hover:bg-indigo-50 hover:text-[var(--jf-primary-on-tint)]`
//
// was reported as a defect even though the hover state overrides the TEXT as well
// as the surface. Measured on that row: the base token rests on white at 4.70:1
// and the hovered text is the on-tint token at 7.07:1 — both clear the 4.5:1
// floor, and the repo-wide guard was green on exactly the same bytes. A scoped
// rule that is red where the repo-wide rule is green is worse than no rule: it
// sends a lane hunting a contrast defect that is not there, and its message then
// quotes a ratio (4.20:1) for a pairing that never renders at that site.

/**
 * A SOLID indigo-50 tint class, with its state prefix split out: "" for the
 * resting state, "hover" / "focus" / "md:hover" … for a state-qualified one.
 *
 * Translucent tints (`bg-indigo-50/40`) and `bg-indigo-100` are deliberately NOT
 * matched: they composite to different surfaces and are tracked separately.
 */
export const SOLID_TINT_RE = /^((?:[a-z-]+:)*)bg-indigo-50$/;

/** The states ("", "hover", …) in which a class list paints a solid tint. */
export function tintStates(variant: string): string[] {
  return variant
    .split(/\s+/)
    .map((cls) => SOLID_TINT_RE.exec(cls)?.[1])
    .filter((s): s is string => s !== undefined);
}

/** Any text token declared through a `--jf-*` custom property, base or on-tint. */
const JF_TEXT_RE = /^((?:[a-z-]+:)*)text-\[var\(--jf-[a-z-]+\)\]$/;
/** The two BASE tokens (the on-tint pair deliberately does not match). */
const BASE_JF_TEXT_RE = /^text-\[var\(--jf-(?:primary|muted)\)\]$/;

/**
 * The base tokens this class list paints ON the tint, in that state.
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
export function baseTokensOnTint(variant: string, state: string): string[] {
  const inState: { cls: string; base: boolean }[] = [];
  const bare: { cls: string; base: boolean }[] = [];
  for (const cls of variant.split(/\s+/)) {
    const m = JF_TEXT_RE.exec(cls);
    if (!m) continue;
    const entry = { cls, base: BASE_JF_TEXT_RE.test(cls) };
    if (m[1] === state) inState.push(entry);
    else if (m[1] === "") bare.push(entry);
  }
  const effective = inState.length > 0 ? inState : bare;
  return effective.filter((e) => e.base).map((e) => e.cls);
}

/**
 * Findings for one file: shape (a) same class list, shape (b) inherited.
 *
 * `rel` is used only to label a finding (`<file>:<line>`); it is the caller's
 * name for the file, not a path this function reads.
 */
export function tintedSurfaceFindings(rel: string, src: string): string[] {
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
      const states = tintStates(v);
      if (states.length === 0) continue;
      const toks = [...new Set(states.flatMap((s) => baseTokensOnTint(v, s)))];
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
  //     element's className was already reported by (a) — reporting it again
  //     as "inherited" would double-count one site.
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
    const ownIsBad = owner.variants.some((v) => tintStates(v).some((s) => baseTokensOnTint(v, s).length > 0));
    if (ownIsBad) continue; // already (a)
    const tintedEl = ancestorsAt(events, owner.openIdx).find(
      (t) => t !== owner && t.variants.some((v) => tintStates(v).includes("")),
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


