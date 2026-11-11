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
// DELIBERATELY NOT SUPPORTED in CHAT mode (and therefore shown as their literal
// text, which is still better than the pre-fix behaviour of showing every
// marker): tables, images, raw HTML, headings, block quotes, nested lists. The
// prompt tells the model not to emit the first three. `![alt](url)` renders its
// alt text; a `<script>` tag renders as visible text; a table row renders as a row
// of pipe characters. `mode="docs"` (see MarkdownMode) adds headings, tables and
// images — for /docs/guides, which renders checked-in repo content, not model
// output. Chat mode is unchanged by it and every guard below still reads chat mode.
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

// WHICH SURFACE IS BEING RENDERED (card t_7b9f6d18).
//
//   "chat" (default) — the deliberately small subset described above. Unchanged, deliberately:
//     every security and performance guard in src/lib/markdown.test.ts reads this mode.
//   "docs"          — additionally supports the three block shapes the repo's OWN screenshot
//     guides are written in: `#`/`##`/`###` headings, pipe tables, and images. Those guides are
//     checked-in repo content (docs/guides/<slug>/README.md, materialised into
//     src/lib/guides.gen.ts), not model output, so the reason images are refused above — an
//     assistant answer echoing merchant data — does not apply to them.
//
//   This is a MODE on the one renderer, not a second parser: /docs/guides renders through this
//   component (AGENTS.md, and the card that added the routes, both say "do not write a second
//   markdown parser"). The chat surface cannot reach the extra shapes by accident, and an
//   image in an assistant answer is still reduced to its alt text.
export type MarkdownMode = "chat" | "docs";

/** A resolved <img>: the URL plus the intrinsic size, so the tag cannot shift layout. */
export type MarkdownImage = {
  src: string;
  width?: number;
  height?: number;
  /**
   * The FULL-SIZE image this figure links to (card t_e1ea03a2). When set, the block figure
   * becomes a tappable link that opens `href` in a new tab, and it is also drawn full-bleed
   * below `sm` — the article column on a 390 px phone is 358 px, and a figure whose whole point
   * is that a 60-year-old can read a screenshot label cannot spend 32 of them on page padding.
   *
   * `/docs/guides` never sets it: those figures are not links, so their layout is unchanged.
   */
  href?: string;
};

/**
 * Turns one image reference in the markdown into a renderable <img>, or returns null to fall
 * back to its alt text. Passed by /docs/guides, which closes over that guide's screenshot
 * manifest — so the only URLs that can ever be emitted are the ones the generator copied.
 * Never called in "chat" mode.
 */
export type ResolveImage = (src: string) => MarkdownImage | null;

type Block =
  | { kind: "p"; text: string }
  | { kind: "code"; text: string }
  | { kind: "list"; ordered: boolean; start: number; items: string[] }
  | { kind: "heading"; level: number; text: string }
  | { kind: "table"; head: string[]; rows: string[][] }
  | { kind: "image"; alt: string; src: string };

export type Inline =
  | { t: "text"; v: string }
  | { t: "code"; v: string }
  | { t: "strong"; v: Inline[] }
  | { t: "em"; v: Inline[] }
  | { t: "link"; v: string; href: string }
  | { t: "image"; v: string; src: string };

// ── block level ──────────────────────────────────────────────────────────────

const FENCE = /^\s*(```|~~~)\s*[\w+-]*\s*$/;
const BULLET = /^(\s*)[-*+]\s+(.*)$/;
const ORDERED = /^(\s*)(\d{1,9})[.)]\s+(.*)$/;

// ── docs mode only (see MarkdownMode) ────────────────────────────────────────
const HEADING = /^(#{1,6})\s+(.+?)\s*$/;
/** A table's delimiter row: | --- | :---: |. Two or more dashes per column, as GFM asks. */
const TABLE_SEP = /^\s*\|?\s*:?-{2,}:?\s*(?:\|\s*:?-{2,}:?\s*)*\|?\s*$/;
/** A paragraph that is ONE image and nothing else, so it can become a <figure>. */
const IMAGE_ONLY = /^!\[([^\]]*)\]\(([^()\s]+)\)$/;
/**
 * The image references docs mode will render: a BARE filename. No scheme, no slash, no `..` —
 * so a `javascript:`/`data:`/off-site URL can never reach an <img src> even if the guide file
 * is edited carelessly. The generator (web/scripts/guides.mjs) rejects anything else at sync
 * time, and resolveImage then maps the filename to the copied asset.
 */
const IMAGE_FILE = /^[A-Za-z0-9][A-Za-z0-9._-]*\.png$/;

/** Cells of one pipe-table row. `\|` escapes are not supported — no guide uses one. */
function tableRow(line: string): string[] {
  let s = line.trim();
  if (s.startsWith("|")) s = s.slice(1);
  if (s.endsWith("|")) s = s.slice(0, -1);
  return s.split("|").map((c) => c.trim());
}

// parseMarkdown splits text into paragraphs, fenced code blocks, lists (and, in `docs` mode,
// headings, tables and images). Exported because the tests assert the block structure directly
// as well as the rendered DOM.
export function parseMarkdown(src: string, mode: MarkdownMode = "chat"): Block[] {
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

    if (mode === "docs") {
      const heading = HEADING.exec(line);
      if (heading) {
        blocks.push({ kind: "heading", level: heading[1].length, text: heading[2].trim() });
        i++;
        continue;
      }
      // A table needs the delimiter row directly under the header row; both are consumed.
      if (line.trim().startsWith("|") && i + 1 < lines.length && TABLE_SEP.test(lines[i + 1])) {
        const head = tableRow(line);
        const rows: string[][] = [];
        i += 2;
        while (i < lines.length && lines[i].trim().startsWith("|")) {
          rows.push(tableRow(lines[i]));
          i++;
        }
        blocks.push({ kind: "table", head, rows });
        continue;
      }
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
      !ORDERED.exec(lines[i]) &&
      !(mode === "docs" && (HEADING.test(lines[i]) || lines[i].trim().startsWith("|")))
    ) {
      para.push(lines[i]);
      i++;
    }
    const text = para.join("\n");
    // A paragraph that is exactly one image becomes a <figure> in docs mode; anywhere else it
    // stays a paragraph and the image is handled inline (or, in chat mode, as its alt text).
    const img = mode === "docs" ? IMAGE_ONLY.exec(text.trim()) : null;
    if (img) blocks.push({ kind: "image", alt: img[1], src: img[2] });
    else blocks.push({ kind: "p", text });
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

// ── link/image delimiters: resolved once per string ──────────────────────────
//
// WHY THE PRECOMPUTED DELIMITERS, AND NOT JUST THE TWO STICKY PATTERNS. Both
// patterns are applied at EVERY position from parseInline's loop, and a rejected
// attempt is only rejected once its `[^\]\n]*` text group has walked to the next
// `]` or to the end of the line. A `[`-heavy answer therefore costs O(rest of the
// line) at every `[`, and parseInline advances one character on a miss, so
// nothing bounds it. The shapes and the cap are real: 4000 characters is the chat
// reply cap (api/internal/chat/chat.go MaxReplyChars), the block splitter has
// already joined the answer into one paragraph, and the chat page mounts its WHOLE
// transcript in one synchronous render — 33 <Markdown> is the fixture used here
// (the page's history cap bounds what it SENDS, not what it renders). Measured
// through this component on this host, best-of-5, ratios per doubling (2 = linear):
//
//     input          1000     2000     4000     8000   ratios
//     '[' x N       2.7ms    7.3ms   20.4ms   78.9ms   2.7/2.8/3.9
//     '![' x N      1.9ms    5.4ms   20.8ms   82.9ms   2.8/3.9/4.0
//     '[a' x N      1.0ms    3.6ms   10.6ms   36.2ms   3.6/3.0/3.4
//
// ...and with a full history the browser itself reported it: 33 `[`-heavy bubbles
// blocked the main thread for 585-655 ms per render (100 long tasks, 33.4 s of
// blocking across the transcript) before this change, against 0 long tasks over
// 90 ms after it. See the guard in web/src/lib/markdown.test.ts.
//
// A link/image candidate is fully determined by TWO positions, so both are
// precomputed for the whole string in one pass and a candidate that is not a
// link is then rejected by two array reads:
//
//   * where the text group stops — the next `]` or `\n`, whichever comes first
//     (the group may not cross a line, and cannot contain a `]`);
//   * where the URL group stops — the next `(`, `)` or whitespace (`[^()\s]`).
//
// The patterns themselves still extract the groups; only the search they used to
// repeat at every position is precomputed, so the rendered output is unchanged
// (proved by a differential fuzz against the previous module).
type LinkDelims = {
  /** Next position of a `]` or `\n` at or after i (where `[^\]\n]*` must stop), or -1. */
  nextText: Int32Array;
  /** Next position of a `(`, `)` or whitespace at or after i (where `[^()\s]*` must stop), or -1. */
  nextUrl: Int32Array;
};

// The URL group's own exclusion class, kept EXACTLY as the pattern spells it
// (non-unicode `\s`, so U+FEFF stays a stop character).
const URL_STOP = /[()\s]/;

function scanLinkDelims(src: string): LinkDelims {
  const n = src.length;
  // n + 1 so a position just past the end reads -1 instead of undefined.
  const nextText = new Int32Array(n + 1).fill(-1);
  const nextUrl = new Int32Array(n + 1).fill(-1);
  let text = -1;
  let url = -1;
  for (let j = n - 1; j >= 0; j--) {
    // Cost accounting for the guard: one unit per character this scan examines.
    // A quadratic version (resolving the closer per `[` instead of once per
    // string) cannot keep this under a multiple of the answer's length.
    if (__mdScanProbe) __mdScanProbe.linkWork++;
    const ch = src[j];
    if (ch === "]" || ch === "\n") text = j;
    if (ch === "(" || ch === ")" || URL_STOP.test(ch)) url = j;
    nextText[j] = text;
    nextUrl[j] = url;
  }
  return { nextText, nextUrl };
}

// linkExtent is the end index (exclusive) of the LINK match at i — or, with
// `image`, of the IMAGE match, which is the same shape with a leading `!` — or
// -1 when the pattern cannot match there. It reads the precomputed delimiters
// only, so a position that is not a link costs O(1) instead of walking the rest
// of the line.
function linkExtent(src: string, i: number, d: LinkDelims, image: boolean): number {
  const n = src.length;
  if (image) {
    if (src[i] !== "!" || src[i + 1] !== "[") return -1;
  } else if (src[i] !== "[") {
    return -1;
  }
  // The text group's stop: it must be a `]` (a `\n` first means the group could
  // not reach a closer, which is a failed match, not a match to the newline).
  const stop = d.nextText[i + (image ? 2 : 1)];
  if (stop < 0 || src[stop] !== "]") return -1;
  const open = stop + 1;
  if (open >= n || src[open] !== "(") return -1;
  // The URL group's stop must be a `)` for the pattern to close.
  const close = d.nextUrl[stop + 2];
  if (close < 0 || src[close] !== ")") return -1;
  return close + 1;
}

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

type Flanking = { left: boolean; right: boolean; beforeWsOrPunct: boolean; afterWsOrPunct: boolean };

function flanking(src: string, start: number, len: number): Flanking {
  if (__mdScanProbe) __mdScanProbe.work++;
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

// The delimiters of ONE source string, resolved in a single left-to-right pass.
//
// WHY THIS IS A SCAN AND NOT A LOOP PER `_`. The obvious shape — at each `_` that
// can open, walk forward for a run that can close — is O(n²): a long answer made
// of opener-ish runs with no closer ahead (`'_a '` repeated) re-walks the whole
// remainder once per underscore, and a 4000-character answer (the chat reply cap,
// api/internal/chat/chat.go MaxReplyChars) took ~220 ms instead of ~1.5 ms. With a
// whole transcript mounting in one render (the chat page renders every message it
// holds; the 33 bubbles are markdown.test.ts's fixture) that is seconds of frozen
// tab. Two passes make it linear:
//
//   1. record the run at every position, and with it the next run that can close
//      a run of that length (`nextSingle` / `nextDouble`) — one backwards sweep;
//   2. pair an opener with that precomputed closer, instead of searching again.
//
// Pairing is still greedy from the left, which is what the previous forward
// search did, so the rendered result is unchanged; only the search is.
type Delims = {
  /**
   * Length of the `_` run STARTING at each position (0 where src[i] is not an
   * underscore) — the same number the old `underRun(src, i)` returned, capped at
   * 3 because only a run of 1 or 2 can ever be a delimiter of interest and every
   * longer one is rejected identically.
   */
  run: Uint8Array;
  /** Position of the next run of exactly ONE `_` that can close, at or after i, or -1. */
  nextSingle: Int32Array;
  /** Same for a run of exactly two. */
  nextDouble: Int32Array;
  /** Position of the next newline at or after i (a single `_` may not cross a line), or -1. */
  nextNewline: Int32Array;
};

// Cost accounting, for the guard that proves this scan is linear. Zero in the
// app; a test sets the probe and reads how much work a render actually did.
//
// `work` counts FLANKING TESTS — the one operation that examines a `_` delimiter.
// Counted there and not once per scan on purpose: a scan that looks at each
// delimiter once (this one) makes O(n) such calls, while the per-underscore
// rescan this replaced makes O(n²) of them, so the counter separates the two
// without a clock and cannot flake on a loaded host.
//
// `linkWork` counts the characters the LINK/IMAGE delimiter scan examines
// (scanLinkDelims), one unit per character — the same shape of accounting for the
// other quadratic in this file. The old sticky patterns did that walk inside the
// regex engine at every `[`, so they reported none of it; that is exactly the
// unbounded hidden work the counter makes visible.
export let __mdScanProbe: { work: number; linkWork: number } | null = null;

/** Test seam: count the delimiter work done by render/parse calls in `fn`. */
export function __withScanProbe<T>(fn: () => T): { result: T; work: number; linkWork: number } {
  const prev = __mdScanProbe;
  const probe = { work: 0, linkWork: 0 };
  __mdScanProbe = probe;
  try {
    return { result: fn(), work: probe.work, linkWork: probe.linkWork };
  } finally {
    __mdScanProbe = prev;
  }
}

function scanUnderDelims(src: string): Delims {
  const n = src.length;
  // Three linear passes, each bounded by the length of THIS string, plus one
  // flanking test per run start: O(n) in total. A quadratic version cannot do
  // that — it re-examines the answer's delimiters once per underscore.
  const run = new Uint8Array(n);
  const nextSingle = new Int32Array(n).fill(-1);
  const nextDouble = new Int32Array(n).fill(-1);
  const nextNewline = new Int32Array(n).fill(-1);

  // Pass 1 (left to right): the run length at every position, once.
  for (let i = n - 1; i >= 0; i--) {
    if (src[i] !== "_") continue;
    const next = i + 1 < n ? run[i + 1] : 0;
    run[i] = next > 0 ? Math.min(next + 1, 3) : 1;
  }

  // Pass 2 (right to left): for every position, the closest delimiter to its
  // right that could CLOSE a run of that length — so an opener is paired by one
  // array read instead of by re-walking the rest of the answer.
  //
  // Only the START of a run is a candidate. A position in the middle of a longer
  // run is not a delimiter of its own (that is the `underRun(src, i) === len`
  // test the forward search applied), and offering one would pair an opener with
  // a suffix of a `__` run — the fuzz caught exactly that, e.g. `b_ ,_2 2__-*)_`).
  let single = -1;
  let double = -1;
  let nl = -1;
  for (let j = n - 1; j >= 0; j--) {
    if (src[j] === "\n") nl = j;
    nextSingle[j] = single;
    nextDouble[j] = double;
    nextNewline[j] = nl;
    if (src[j] !== "_" || (j > 0 && src[j - 1] === "_")) continue; // not a run start
    if (run[j] === 1) {
      if (underCanClose(src, j, 1)) single = j;
    } else if (run[j] === 2) {
      if (underCanClose(src, j, 2)) double = j;
    }
  }

  return { run, nextSingle, nextDouble, nextNewline };
}

// underEmphasis looks for `_inner_` (len 1) or `__inner__` (len 2) starting
// exactly at `pos`, pairing a run that can open with the NEXT run of the same
// length that can close. Returning null leaves the underscore as literal text,
// which is the whole point: an identifier's underscores are the merchant's own
// characters and must survive to the screen.
//
// Like the `*` patterns it replaces, the inner text cannot start or end on
// whitespace (that is how `_` surrounded by spaces stays a literal underscore),
// and single-underscore emphasis does not span a line break.
function underEmphasis(pos: number, len: 1 | 2, src: string, d: Delims): UnderEmphasis | null {
  if (d.run[pos] !== len) return null;
  if (!underCanOpen(src, pos, len)) return null;
  const contentStart = pos + len;
  const close = len === 1 ? d.nextSingle[contentStart] : d.nextDouble[contentStart];
  if (close < 0) return null;
  // Single-underscore emphasis must not run past the end of its line; the `__`
  // form may (matching the `*` patterns, which differ the same way). The closer
  // index decides it in one comparison instead of a walk to the newline.
  if (len === 1 && d.nextNewline[contentStart] >= 0 && d.nextNewline[contentStart] < close) return null;
  return { inner: src.slice(contentStart, close), length: close + len - pos };
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

function emphasis(kind: "strong" | "em", inner: string, depth: number, mode: MarkdownMode): Inline {
  // Nested emphasis inside emphasis is parsed, but only a few levels deep. The
  // recursion is bounded by MAX_DEPTH AND the scan is linear (scanUnderDelims),
  // so a crafted answer cannot turn rendering quadratic.
  return {
    t: kind,
    v: depth < MAX_DEPTH ? parseInline(inner, depth + 1, mode) : [{ t: "text", v: inner }],
  };
}

export function parseInline(src: string, depth = 0, mode: MarkdownMode = "chat"): Inline[] {
  const out: Inline[] = [];
  // One delimiter scan for this string, shared by every `_` position below —
  // this is the half that makes rendering linear (see scanUnderDelims).
  const delims = scanUnderDelims(src);
  // ...and one for the link/image delimiters, shared by every `[` position. Only
  // when the string contains a `[` at all: both patterns need a literal one, so a
  // string without it can have no link and needs no scan. See scanLinkDelims.
  const links = src.indexOf("[") < 0 ? null : scanLinkDelims(src);
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

    // A `[` or `![` that the precomputed delimiters say cannot complete is not a
    // link, and is not asked of the pattern at all — that is what keeps a
    // `[`-heavy answer linear (the pattern would walk to the end of the line
    // before failing). The pattern still does the extraction.
    if (links && src[i] === "!" && linkExtent(src, i, links, true) >= 0 && (m = at(i, IMAGE, src))) {
      if (text) out.push({ t: "text", v: text });
      text = "";
      // Chat mode reduces an image to its alt text on purpose (the text can be model output).
      // Docs mode renders it, but only a BARE filename — never a scheme or a path — so the
      // resolver in /docs/guides can map it to a copied asset and nothing else can be reached.
      if (mode === "docs" && IMAGE_FILE.test(m[2])) out.push({ t: "image", v: m[1], src: m[2] });
      else out.push({ t: "text", v: m[1] });
      i += m[0].length;
      continue;
    }

    if (links && src[i] === "[" && linkExtent(src, i, links, false) >= 0 && (m = at(i, LINK, src))) {
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
      out.push(emphasis("strong", m[1], depth, mode));
      i += m[0].length;
      continue;
    }

    // `__strong__` before `_em_`: a `__` run must not be read as two `_` runs.
    const strongUnder = underEmphasis(i, 2, src, delims);
    if (strongUnder) {
      if (text) out.push({ t: "text", v: text });
      text = "";
      out.push(emphasis("strong", strongUnder.inner, depth, mode));
      i += strongUnder.length;
      continue;
    }

    if ((m = at(i, EM_STAR, src))) {
      if (text) out.push({ t: "text", v: text });
      text = "";
      out.push(emphasis("em", m[1], depth, mode));
      i += m[0].length;
      continue;
    }

    const emUnder = underEmphasis(i, 1, src, delims);
    if (emUnder) {
      if (text) out.push({ t: "text", v: text });
      text = "";
      out.push(emphasis("em", emUnder.inner, depth, mode));
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

/** Everything the block/inline renderers need besides the node itself. */
type RenderCtx = { variant: MarkdownVariant; mode: MarkdownMode; resolveImage?: ResolveImage };

/**
 * How a line break INSIDE a paragraph or a list item is rendered.
 *
 * `whitespace-pre-wrap` is the right rule for USER-TYPED text: a newline the merchant or the
 * model typed into a chat answer or a feedback body is a break the reader meant, and the chat
 * surface used to render raw text, so it must keep them (src/lib/markdown.test.ts pins that).
 * It is the WRONG rule for AUTHORED prose: the /docs guides' READMEs are hard-wrapped at ~100
 * columns, so under pre-wrap every one of those source wraps became a real line break on the
 * page — at 390px the source wrap inside "Every screenshot below was captured…" stranded
 * "screenshot" on a line of its own and left a ragged right edge that reads as broken (card
 * t_b5ea9103, Wan: "tiba-tiba ada new line"). Authored prose is wrapped by the browser, at the
 * width it is actually read.
 *
 * So the rule is SCOPED ON THE MODE, not deleted: docs prose wraps normally, chat keeps its
 * typed line breaks. Deleting it outright would silently un-break every typed newline in the
 * chat, which is the regression this shape exists to avoid.
 */
const lineBreakClass = (mode: MarkdownMode) => (mode === "docs" ? "" : "whitespace-pre-wrap ");

/**
 * One image reference → an <img>, or the alt text when there is nothing safe to point at.
 *
 * The resolver is what makes the URL safe: /docs/guides closes over that guide's generated
 * screenshot manifest, so the only sources it can return are the files the generator copied
 * into public/guides. A guide image that the manifest does not know falls back to its alt
 * text — the same honest failure chat mode uses, instead of a broken <img>.
 *
 * A resolver MAY return `href` (card t_e1ea03a2): the full-size capture, when the figure it
 * resolves is a CROP of it. That turns the figure into a link (open the full image in a new
 * tab, where a phone can pinch-zoom) and, because the whole point is legibility on a small
 * screen, draws it full-bleed below `sm` instead of inside the article's padding. `step` is the
 * heading the figure sits under, so the link's accessible name names the step ("Langkah 5: …")
 * rather than only describing the picture.
 */
function renderImage(
  n: { v: string; src: string },
  ctx: RenderCtx,
  key: string,
  block: boolean,
  step?: string | null,
): ReactNode {
  const resolved =
    ctx.mode === "docs" && ctx.resolveImage && IMAGE_FILE.test(n.src) ? ctx.resolveImage(n.src) : null;
  if (!resolved) return n.v;
  // A figure that links to its full capture is drawn edge to edge on a phone: below `sm` the
  // article's 16 px side padding is 32 of the 390 px the reader has, and a screenshot whose job
  // is legibility cannot spend them on whitespace. Above `sm` it is back in the column.
  const linked = Boolean(block && resolved.href);
  const img = createElement("img", {
    key: `${key}-img`,
    src: resolved.src,
    alt: n.v,
    // Intrinsic size (from the PNG header, via the generator): with w-full + the attribute
    // pair the browser reserves the right box before the bytes arrive, so a step's screenshot
    // never shoves the text under it down the page.
    width: resolved.width,
    height: resolved.height,
    loading: "lazy",
    decoding: "async",
    className: block
      ? linked
        ? "w-full rounded-none border-y border-slate-200 sm:rounded-lg sm:border"
        : "w-full rounded-lg border border-slate-200"
      : "inline-block max-w-full align-middle",
  });
  if (!block) return img;
  if (!resolved.href) {
    return createElement(
      "figure",
      { key, className: "mt-4" },
      img,
      // The caption repeats the alt text for SIGHTED readers; aria-hidden keeps a screen
      // reader from announcing the same sentence twice (the <img alt> already carries it).
      createElement(
        "figcaption",
        { "aria-hidden": "true", className: "mt-1 text-xs text-slate-500" },
        n.v,
      ),
    );
  }
  // "Langkah 5: Tetapkan caj CHIP pada pembayar" — the step's own heading, so a screen reader
  // hears which step the link belongs to (the alt describes the picture, not the step).
  const named = step ? /^(\d{1,2})\.\s+(.+)$/.exec(step) : null;
  const label = `${named ? `Langkah ${named[1]}: ${named[2]}` : (step ?? n.v)} — ketik untuk lihat penuh`;
  return createElement(
    "figure",
    { key, className: "mt-4 -mx-4 sm:mx-0" },
    createElement(
      "a",
      {
        key: `${key}-link`,
        href: resolved.href,
        target: "_blank",
        // noreferrer: the full capture is a static PNG on this origin, and a guide handed to a
        // surau committee has no business telling that URL where the click came from.
        rel: "noreferrer",
        className: "block",
        "aria-label": label,
      },
      img,
    ),
    createElement(
      "figcaption",
      { className: "mt-1 px-4 text-xs text-slate-500 sm:px-0" },
      // The card's caption, in Malay. It does NOT repeat the alt text the way the unlinked
      // figure's caption does: the picture sits directly under its step heading, the alt text
      // already describes it for assistive tech, and repeating nine descriptions here put the
      // guide 23 words over its 700-word budget for no reader's benefit.
      "Ketik untuk lihat penuh",
    ),
  );
}

function renderInline(nodes: Inline[], ctx: RenderCtx, key = "i"): ReactNode[] {
  const t = tone(ctx.variant);
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
        return createElement("strong", { key: k, className: "font-semibold" }, ...renderInline(n.v, ctx, k));
      case "em":
        return createElement("em", { key: k }, ...renderInline(n.v, ctx, k));
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
          ...renderInline(parseInline(n.v, 0, ctx.mode), ctx, k),
        );
      case "image":
        return renderImage(n, ctx, k, false);
      default:
        return n.v;
    }
  });
}

const HEADING_CLASS: Record<number, string> = {
  1: "mt-8 text-2xl font-bold text-slate-900 first:mt-0",
  2: "mt-8 text-xl font-semibold text-slate-900 first:mt-0",
  3: "mt-6 text-lg font-semibold text-slate-900",
};

function renderBlock(b: Block, ctx: RenderCtx, key: string, step: string | null): ReactNode {
  const t = tone(ctx.variant);
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
          { key: `${key}-${i}`, className: `${lineBreakClass(ctx.mode)}break-words` },
          ...renderInline(parseInline(item, 0, ctx.mode), ctx, `${key}-${i}`),
        ),
      ),
    );
  }
  if (b.kind === "heading") {
    const level = Math.min(6, Math.max(1, b.level));
    return createElement(
      `h${level}`,
      // id from the text would be a second slugger to keep in step with the nav; the guides
      // do not deep-link their own steps, so no id is emitted.
      { key, className: HEADING_CLASS[level] ?? "mt-4 text-base font-semibold text-slate-900" },
      ...renderInline(parseInline(b.text, 0, ctx.mode), ctx, key),
    );
  }
  if (b.kind === "table") {
    const cell = (tag: "th" | "td", text: string, at: string) =>
      createElement(
        tag,
        {
          key: at,
          className:
            tag === "th"
              ? "border-b border-slate-200 px-2 py-1 text-left font-semibold text-slate-900"
              : "border-b border-slate-100 px-2 py-1 align-top",
        },
        ...renderInline(parseInline(text, 0, ctx.mode), ctx, at),
      );
    return createElement(
      "div",
      // A table is the one block that can be wider than a phone: let it scroll INSIDE its
      // own box (the same rule the docs <pre> uses) instead of panning the page.
      { key, className: "mt-3 overflow-x-auto" },
      createElement(
        "table",
        { className: "w-full border-collapse text-sm" },
        createElement("thead", null, createElement("tr", null, ...b.head.map((h, i) => cell("th", h, `${key}-h${i}`)))),
        createElement(
          "tbody",
          null,
          ...b.rows.map((row, r) =>
            createElement(
              "tr",
              { key: `${key}-r${r}` },
              ...row.map((c, i) => cell("td", c, `${key}-r${r}c${i}`)),
            ),
          ),
        ),
      ),
    );
  }
  if (b.kind === "image") {
    const rendered = renderImage({ v: b.alt, src: b.src }, ctx, key, true, step);
    // The resolver returned nothing (an unknown filename): keep the alt text visible as a
    // paragraph rather than emitting a broken image.
    if (typeof rendered === "string") {
      return createElement("p", { key, className: "mt-2 text-sm text-slate-600" }, rendered);
    }
    return rendered;
  }
  return createElement(
    "p",
    // A typed line break survives in chat and is collapsed in docs prose (lineBreakClass);
    // break-words handles a long unbroken token either way.
    { key, className: `${lineBreakClass(ctx.mode)}break-words` },
    ...renderInline(parseInline(b.text, 0, ctx.mode), ctx, key),
  );
}

export type MarkdownProps = {
  /** The raw text to render. Never interpreted as HTML. */
  text: string;
  /** Which surface it sits on, so code/links stay legible. Default "assistant". */
  variant?: MarkdownVariant;
  className?: string;
  /** "chat" (default, the assistant subset) or "docs" (headings/tables/images). */
  mode?: MarkdownMode;
  /** docs mode only: maps a bare screenshot filename to the asset the generator copied. */
  resolveImage?: ResolveImage;
};

/**
 * Markdown renders the assistant/user/feedback subset as React elements. It is
 * the ONE renderer for chat answers, feedback thread bodies (the /feedback page
 * and the /admin triage view) — do not write a second one. `/docs/guides` renders
 * through it too, in `mode="docs"` (card t_7b9f6d18).
 */
export function Markdown({
  text,
  variant = "assistant",
  className = "",
  mode = "chat",
  resolveImage,
}: MarkdownProps) {
  const blocks = parseMarkdown(text, mode);
  if (blocks.length === 0) return null;
  const ctx: RenderCtx = { variant, mode, resolveImage };
  // The heading each block sits under, so a figure can name its step in the link it becomes
  // (renderImage's `step`). Precomputed once: renderBlock stays a pure function of its block.
  let heading: string | null = null;
  const steps = blocks.map((b) => {
    if (b.kind === "heading") heading = b.text;
    return heading;
  });
  return createElement(
    "div",
    // min-w-0 lets the block shrink inside a flex bubble (a flex item will not
    // go below its content width otherwise), which is what stops a wide code
    // block from widening the whole card at 412px.
    { className: `min-w-0 space-y-2 break-words ${className}`.trim() },
    ...blocks.map((b, i) => renderBlock(b, ctx, `b${i}`, steps[i])),
  );
}
