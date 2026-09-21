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
const STRONG_UNDER = /__(?=\S)([\s\S]*?\S)__/y;
const EM_STAR = /\*(?=\S)([^*\n]*?\S)\*/y;
const EM_UNDER = /_(?=\S)([^_\n]*?\S)_/y;

// Emphasis may not start or end on whitespace (that is how `2 * 3 * 4` stays
// arithmetic rather than becoming italic), so each pattern needs \S at both
// ends. The lookahead pins the start; the greedy-to-\S group pins the end.
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

    if ((m = at(i, STRONG_UNDER, src))) {
      if (text) out.push({ t: "text", v: text });
      text = "";
      out.push(emphasis("strong", m[1], depth));
      i += m[0].length;
      continue;
    }

    if ((m = at(i, EM_STAR, src))) {
      if (text) out.push({ t: "text", v: text });
      text = "";
      out.push(emphasis("em", m[1], depth));
      i += m[0].length;
      continue;
    }

    if ((m = at(i, EM_UNDER, src))) {
      if (text) out.push({ t: "text", v: text });
      text = "";
      out.push(emphasis("em", m[1], depth));
      i += m[0].length;
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
