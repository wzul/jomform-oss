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
type Seg = { text?: string; alts?: string[] };

export type Tag = {
  name: string;
  variants: Variant[];
  openIdx: number;
  attrStart: number;
  attrEnd: number;
  closeIdx: number;
};

/** Element open/close events, in source order. */
export type Event = { open: boolean; tag: Tag };

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
  const re = /"((?:[^"\\]|\\.)*)"|'((?:[^'\\]|\\.)*)'|`((?:[^`\\]|\\.)*)`/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(body))) alts.push(m[1] ?? m[2] ?? m[3] ?? "");
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

/**
 * Parse the source into tags and, for each, the index just past its closing
 * `</name>`. Pairing uses an explicit stack so a token's ancestor chain is
 * exact: a source-order "everything opened before this offset" heuristic gets
 * siblings and nested same-name elements wrong, and would either miss a defect
 * or flag clean markup.
 */
export function jsxTags(code: string): Event[] {
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

