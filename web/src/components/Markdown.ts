// Markdown for model-written (and user-written) text in the dashboard.
//
// WHY A HAND-WRITTEN RENDERER AND NOT react-markdown:
//   * The text this renders is ASSISTANT OUTPUT. A model answer can be steered
//     by the merchant's own data — a form answer, a product name, a customer's
//     message, all of it is echoed into the reply. So the renderer must never
//     put that text anywhere but a React text node. This module builds a React
//     ELEMENT TREE with createElement; it never touches innerHTML and there is
//     no dangerouslySetInnerHTML anywhere in it. Nothing else in the repo needs
//     a raw-HTML escape hatch, and this is not the place to introduce one.
//   * Only a small subset is supported, on purpose (see SUPPORTED below). The
//     subset is what the assistant is told to emit (api/internal/chat/prompt.go,
//     HOW TO ANSWER), and the pair — prompt rule + renderer — is what makes the
//     output predictable. A general parser would silently support more than the
//     prompt promises, and the two would drift.
//
// SUPPORTED: **bold**, *italic* / _italic_, `inline code`, fenced code blocks
//   (```), "-"/"*"/"+" and "1." lists, [links](https://…), and blank lines as
//   paragraph breaks. Single newlines inside a paragraph are kept (the page used
//   to render with whitespace-pre-wrap, and losing a line break a user typed
//   would be a regression).
//
// INTRAWORD `_` IS NOT EMPHASIS. `_italic_` works at the edges of a word
//   (`_condong_`, `kata _condong_ kata`) but a `_` inside one is literal text, so
//   `form_answers_value` / `jf_2fa_pending` keep their underscores on screen —
//   CommonMark's rule, and the reason is in the inline section below. `*` is
//   unchanged: `foo*bar*baz` is still emphasis, as CommonMark says it should be.
//
// DELIBERATELY NOT SUPPORTED (and therefore shown as their literal text, which
// is still better than the pre-fix behaviour of showing every marker):
//   tables, images, raw HTML, headings, block quotes, nested lists. The prompt
//   tells the model not to emit the first three. `![alt](url)` renders its alt
//   text; a `<script>` tag renders as visible text; a table row renders as a row
//   of pipe characters.
//
// LINKS: only http/https become an <a>. Anything else (javascript:, data:, a
//   relative path) is rendered as plain text, per the same rule the repo applies
//   to caller-supplied URLs elsewhere. External links get rel="noopener
//   noreferrer" and open in a new tab.
//
// WHY .ts AND NOT .tsx: the web gate is `node --test` over src/lib/*.test.ts and
//   node strips types but cannot parse JSX. A .tsx renderer could not be
//   imported by the guard tests that prove the bold/link/XSS behaviour, so the
//   component is written with createElement. Keep it that way; converting it to
//   .tsx removes the tests' ability to look at what it actually renders.

import { createElement, Fragment, type ReactNode } from "react";

export type MarkdownVariant = "assistant" | "user" | "plain";

type Block =
  | { kind: "p"; text: string }
  | { kind: "code"; text: string }
  | { kind: "list"; ordered: boolean; start: number; items: string[] };

export type Inline =
  | { t: "text"; v: string }
  | { t: "code"; v: string }
  | { t: "strong"; v: Inline[] }
  | { t: "em"; v: Inline[] }
  | { t: "link"; v: string; href: string };

// ── block level ──────────────────────────────────────────────────────────────

const FENCE = /^\s*(```|~~~)\s*[\w+-]*\s*$/;
const BULLET = /^(\s*)[-*+]\s+(.*)$/;
const ORDERED = /^(\s*)(\d{1,9})[.)]\s+(.*)$/;

// parseMarkdown splits text into paragraphs, fenced code blocks and lists.
// Exported because the tests assert the block structure directly as well as the
// rendered DOM.
export function parseMarkdown(src: string): Block[] {
  const lines = (src ?? "").replace(/\r\n?/g, "\n").split("\n");
  const blocks: Block[] = [];
  let i = 0;

  while (i < lines.length) {
    const line = lines[i];

    const fence = FENCE.exec(line);
    if (fence) {
      const marker = fence[1];
      const body: string[] = [];
      i++;
      while (i < lines.length) {
        const close = FENCE.exec(lines[i]);
        if (close && close[1] === marker) {
          i++;
          break;
        }
        body.push(lines[i]);
        i++;
      }
      // An unclosed fence is still a code block: the model's answer is capped at
      // 4000 characters, so treating the remainder as code is the honest read.
      blocks.push({ kind: "code", text: body.join("\n") });
      continue;
    }

    if (line.trim() === "") {
      i++;
      continue;
    }

    const ordered = ORDERED.exec(line);
    const bullet = BULLET.exec(line);
    if (ordered || bullet) {
      const isOrdered = ordered !== null;
      const start = ordered ? Number(ordered[2]) : 1;
      const items: string[] = [];
      while (i < lines.length) {
        const o = ORDERED.exec(lines[i]);
        const b = BULLET.exec(lines[i]);
        const m = isOrdered ? o : b;
        if (!m) break;
        // Group 2 differs between the two patterns (the number vs the marker).
        let item = isOrdered ? (o as RegExpExecArray)[3] : (b as RegExpExecArray)[2];
        i++;
        // A wrapped list item continues on an indented line. Nested lists are
        // NOT modelled: an indented marker starts a new item of this list.
        while (
          i < lines.length &&
          lines[i].trim() !== "" &&
          !BULLET.exec(lines[i]) &&
          !ORDERED.exec(lines[i]) &&
          /^\s{2,}/.test(lines[i])
        ) {
          item += "\n" + lines[i].replace(/^\s{2,}/, "");
          i++;
        }
        items.push(item);
      }
      blocks.push({ kind: "list", ordered: isOrdered, start, items });
      continue;
    }

    const para: string[] = [];
    while (
      i < lines.length &&
      lines[i].trim() !== "" &&
      !FENCE.test(lines[i]) &&
      !BULLET.exec(lines[i]) &&
      !ORDERED.exec(lines[i])
    ) {
      para.push(lines[i]);
      i++;
    }
    blocks.push({ kind: "p", text: para.join("\n") });
  }

  return blocks;
}

// ── inline level ─────────────────────────────────────────────────────────────

// Sticky patterns: each is applied at the current scan position only.
const CODE = /`([^`\n]+)`/y;
// An image is matched so it can be turned into its alt text (unsupported on
// purpose — see the header) rather than leaving "![alt](url)" on screen.
const IMAGE = /!\[([^\]\n]*)\]\(([^()\s]*)\)/y;
const LINK = /\[([^\]\n]*)\]\(([^()\s]*)\)/y;
const STRONG_STAR = /\*\*(?=\S)([\s\S]*?\S)\*\*/y;
const EM_STAR = /\*(?=\S)([^*\n]*?\S)\*/y;

// ── underscore emphasis: the delimiter-run rule, not "any two underscores" ───
//
// WHY THIS IS NOT JUST ANOTHER STICKY PATTERN. The previous `_` patterns were
// `/_(?=\S)([^_\n]*?)\S)_/y` and `/__(?=\S)([\s\S]*?\S)__/y`: `\S` at each end
// and nothing else, so a single underscore ANYWHERE inside an identifier was
// read as emphasis and — worse than mis-styling — the underscores were REMOVED
// from the text the reader sees. `form_answers_value` rendered as
// `formanswersvalue` (three characters of the merchant's own string lost),
// `jf_2fa_pending` as `jf2fapending`, `report_final dan nota_akhir` as
// `report<em>final dan nota</em>akhir`.
//
// Those are real strings here: `jf_2fa_pending` is a flag in
// api/internal/auth/session.go, `feedback_threads` / `form_answers_value` are
// real table and column names, and `_`-separated identifiers are this repo's
// house style — so naming a setting, a column or a status is the common case,
// not the edge case.
//
// The rule now follows CommonMark 0.31.2 §6.2, which restricts intraword
// emphasis to asterisks: "intraword emphasis is restricted to asterisks, since
// underscores are too frequently encountered as a replacement for spaces, e.g.
// in file names"
// (https://spec.commonmark.org/0.31.2/#emphasis-and-strong-emphasis).
// A `_` delimiter run must be LEFT-FLANKING to open (rule 2) and RIGHT-FLANKING
// to close (rule 4), with the punctuation clause that follows from that: a run
// that is both left- and right-flanking (i.e. inside a word) can do neither
// unless a punctuation character is on the outside of it.
//
// `*` is deliberately NOT changed: in CommonMark `foo*bar*baz` IS emphasis, and
// the existing `*` patterns and their tests are left exactly as they were.

// Unicode whitespace and Unicode punctuation, as the spec defines them for the
// flanking test (a Unicode whitespace character, or a character in the P/S
// categories). The end of the line counts as whitespace.
const SPACE = /\s/u;
const PUNCT = /[\p{P}\p{S}]/u;

function isPunct(ch: string): boolean {
  return PUNCT.test(ch);
}

// A delimiter run is the WHOLE sequence of `_` characters, so `underRun(i)` is
// how long the run starting at `i` is (and 0 when `src[i]` is not an underscore).
function underRun(src: string, i: number): number {
  if (src[i] !== "_") return 0;
  let end = i;
  while (end < src.length && src[end] === "_") end++;
  return end - i;
}

type Flanking = { left: boolean; right: boolean; beforeWsOrPunct: boolean; afterWsOrPunct: boolean };

function flanking(src: string, start: number, len: number): Flanking {
  const before = start === 0 ? "\n" : src[start - 1];
  const after = start + len >= src.length ? "\n" : src[start + len];
  const beforeWsOrPunct = SPACE.test(before) || isPunct(before);
  const afterWsOrPunct = SPACE.test(after) || isPunct(after);
  return {
    // left-flanking: not followed by whitespace, and either not followed by
    // punctuation, or followed by punctuation and preceded by whitespace/punctuation
    left: !SPACE.test(after) && (!isPunct(after) || beforeWsOrPunct),
    right: !SPACE.test(before) && (!isPunct(before) || afterWsOrPunct),
    beforeWsOrPunct,
    afterWsOrPunct,
  };
}

// A `_` run can OPEN emphasis only if it is left-flanking and either not
// right-flanking (a run inside a word is both) or preceded by punctuation.
function underCanOpen(src: string, start: number, len: number): boolean {
  const f = flanking(src, start, len);
  return f.left && (!f.right || f.beforeWsOrPunct);
}

// ...and can CLOSE it only if it is right-flanking and either not left-flanking
// or followed by punctuation.
function underCanClose(src: string, start: number, len: number): boolean {
  const f = flanking(src, start, len);
  return f.right && (!f.left || f.afterWsOrPunct);
}

type UnderEmphasis = { inner: string; length: number };

// underEmphasis looks for `_inner_` (len 1) or `__inner__` (len 2) starting
// exactly at `pos`, pairing a run that can open with the NEXT run of the same
// length that can close. Returning null leaves the underscore as literal text,
// which is the whole point: an identifier's underscores are the merchant's own
// characters and must survive to the screen.
//
// Like the `*` patterns it replaces, the inner text cannot start or end on
// whitespace (that is how `_` surrounded by spaces stays a literal underscore),
// and single-underscore emphasis does not span a line break.
function underEmphasis(pos: number, len: 1 | 2, src: string): UnderEmphasis | null {
  if (underRun(src, pos) !== len) return null;
  if (!underCanOpen(src, pos, len)) return null;
  const contentStart = pos + len;
  for (let j = contentStart; j < src.length; j++) {
    // Single-underscore emphasis must not run past the end of its line; the
    // `__` form may (matching the `*` patterns, which differ the same way).
    if (len === 1 && src[j] === "\n") return null;
    if (src[j] !== "_") continue;
    const closeRun = underRun(src, j);
    if (closeRun !== len) {
      j += closeRun - 1;
      continue;
    }
    if (underCanClose(src, j, len)) {
      return { inner: src.slice(contentStart, j), length: j + len - pos };
    }
    j += len - 1;
  }
  return null;
}

// Emphasis may not start or end on whitespace (that is how `2 * 3 * 4` stays
// arithmetic rather than becoming italic); the lookahead pins the start and the
// greedy-to-`\S` group pins the end. For `_` that is now implied by the flanking
// test above, which also rejects a run inside a word.
const MAX_DEPTH = 3;

function at(pos: number, re: RegExp, src: string): RegExpExecArray | null {
  re.lastIndex = pos;
  return re.exec(src);
}

function emphasis(kind: "strong" | "em", inner: string, depth: number): Inline {
  // Nested emphasis inside emphasis is parsed, but only a few levels deep: the
  // recursion is bounded so a crafted answer cannot turn rendering quadratic.
  return { t: kind, v: depth < MAX_DEPTH ? parseInline(inner, depth + 1) : [{ t: "text", v: inner }] };
}

export function parseInline(src: string, depth = 0): Inline[] {
  const out: Inline[] = [];
  let text = "";
  let i = 0;

  while (i < src.length) {
    let m: RegExpExecArray | null;

    if ((m = at(i, CODE, src))) {
      // No markdown inside code: the span is verbatim.
      if (text) out.push({ t: "text", v: text });
      text = "";
      out.push({ t: "code", v: m[1] });
      i += m[0].length;
      continue;
    }

    if ((m = at(i, IMAGE, src))) {
      if (text) out.push({ t: "text", v: text });
      text = "";
      out.push({ t: "text", v: m[1] }); // alt text only — images are not rendered
      i += m[0].length;
      continue;
    }

    if ((m = at(i, LINK, src))) {
      if (text) out.push({ t: "text", v: text });
      text = "";
      const href = m[2];
      // The scheme rule: only http/https become a link. javascript:, data:, a
      // relative path — plain text, per urlguard's rule for caller-supplied URLs.
      if (/^https?:\/\//i.test(href)) {
        out.push({ t: "link", v: m[1] || href, href });
      } else {
        out.push({ t: "text", v: m[1] });
      }
      i += m[0].length;
      continue;
    }

    if ((m = at(i, STRONG_STAR, src))) {
      if (text) out.push({ t: "text", v: text });
      text = "";
      out.push(emphasis("strong", m[1], depth));
      i += m[0].length;
      continue;
    }

    // `__strong__` before `_em_`: a `__` run must not be read as two `_` runs.
    const strongUnder = underEmphasis(i, 2, src);
    if (strongUnder) {
      if (text) out.push({ t: "text", v: text });
      text = "";
      out.push(emphasis("strong", strongUnder.inner, depth));
      i += strongUnder.length;
      continue;
    }

    if ((m = at(i, EM_STAR, src))) {
      if (text) out.push({ t: "text", v: text });
      text = "";
      out.push(emphasis("em", m[1], depth));
      i += m[0].length;
      continue;
    }

    const emUnder = underEmphasis(i, 1, src);
    if (emUnder) {
      if (text) out.push({ t: "text", v: text });
      text = "";
      out.push(emphasis("em", emUnder.inner, depth));
      i += emUnder.length;
      continue;
    }

    text += src[i];
    i++;
  }

  if (text) out.push({ t: "text", v: text });
  return out;
}

// ── rendering ────────────────────────────────────────────────────────────────

// Colours differ per surface because the chat has a dark (indigo) user bubble
// and a light (slate-50) assistant bubble. A code chip that is legible on one is
// invisible on the other, so the tone is a prop rather than a global class.
function tone(variant: MarkdownVariant) {
  switch (variant) {
    case "user":
      // white text on var(--jf-primary)
      return {
        code: "bg-white/20 text-white",
        pre: "bg-black/20 text-white",
        link: "text-white",
      };
    default:
      // var(--jf-text) on white / slate-50
      return {
        code: "bg-slate-200/70 text-[var(--jf-text)]",
        pre: "bg-slate-900/5 text-[var(--jf-text)]",
        link: "text-[var(--jf-primary)]",
      };
  }
}

function renderInline(nodes: Inline[], variant: MarkdownVariant, key = "i"): ReactNode[] {
  const t = tone(variant);
  return nodes.map((n, i) => {
    const k = `${key}-${i}`;
    switch (n.t) {
      case "code":
        return createElement(
          "code",
          // break-words matters on a phone: a long unbroken token in an inline
          // code span is a horizontal-overflow source at 412px.
          { key: k, className: `break-words rounded px-1 py-0.5 font-mono text-[0.85em] ${t.code}` },
          n.v,
        );
      case "strong":
        return createElement("strong", { key: k, className: "font-semibold" }, ...renderInline(n.v, variant, k));
      case "em":
        return createElement("em", { key: k }, ...renderInline(n.v, variant, k));
      case "link":
        return createElement(
          "a",
          {
            key: k,
            href: n.href,
            target: "_blank",
            rel: "noopener noreferrer",
            className: `underline decoration-dotted underline-offset-2 ${t.link}`,
          },
          ...renderInline(parseInline(n.v), variant, k),
        );
      default:
        return n.v;
    }
  });
}

function renderBlock(b: Block, variant: MarkdownVariant, key: string): ReactNode {
  const t = tone(variant);
  if (b.kind === "code") {
    return createElement(
      "pre",
      // overflow-x-auto keeps a wide code block scrolling INSIDE the card instead
      // of pushing the card (and the page) wider than the viewport.
      { key, className: `mt-2 overflow-x-auto rounded-md p-2 text-xs ${t.pre}` },
      createElement("code", { className: "whitespace-pre font-mono" }, b.text),
    );
  }
  if (b.kind === "list") {
    return createElement(
      b.ordered ? "ol" : "ul",
      {
        key,
        className: `mt-2 space-y-1 pl-5 ${b.ordered ? "list-decimal" : "list-disc"}`,
        ...(b.ordered && b.start > 1 ? { start: b.start } : {}),
      },
      ...b.items.map((item, i) =>
        createElement(
          "li",
          { key: `${key}-${i}`, className: "whitespace-pre-wrap break-words" },
          ...renderInline(parseInline(item), variant, `${key}-${i}`),
        ),
      ),
    );
  }
  return createElement(
    "p",
    // whitespace-pre-wrap preserves the line breaks the old raw-text rendering
    // showed; break-words handles a long unbroken token.
    { key, className: "whitespace-pre-wrap break-words" },
    ...renderInline(parseInline(b.text), variant, key),
  );
}

export type MarkdownProps = {
  /** The raw text to render. Never interpreted as HTML. */
  text: string;
  /** Which surface it sits on, so code/links stay legible. Default "assistant". */
  variant?: MarkdownVariant;
  className?: string;
};

/**
 * Markdown renders the assistant/user/feedback subset as React elements. It is
 * the ONE renderer for chat answers, feedback thread bodies (the /feedback page
 * and the /admin triage view) — do not write a second one.
 */
export function Markdown({ text, variant = "assistant", className = "" }: MarkdownProps) {
  const blocks = parseMarkdown(text);
  if (blocks.length === 0) return null;
  return createElement(
    "div",
    // min-w-0 lets the block shrink inside a flex bubble (a flex item will not
    // go below its content width otherwise), which is what stops a wide code
    // block from widening the whole card at 412px.
    { className: `min-w-0 space-y-2 break-words ${className}`.trim() },
    ...blocks.map((b, i) => renderBlock(b, variant, `b${i}`)),
  );
}
