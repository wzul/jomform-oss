// The /docs inline grammar, kept free of JSX so a unit test can import it.
// `docsProse.tsx` renders these tokens; this module only decides what they are.
//
// A body string may carry four markers:
//   **bold**        a term worth seeing before the sentence
//   `code`          an API field, route or tool name
//   [label](href)   a link
//   [[zh|中文]]      a word in another language, with its language DECLARED
//
// The last one is not decoration: WCAG 3.1.2 wants every change of language
// marked, because a screen reader that pronounces 中文 with an English voice
// reads noise. The marker exists so the name keeps its own `lang` while sitting
// in an English sentence, and it is asserted live in e2e/a11y-docs-lang.spec.ts.
//
// Anything else is text. An unclosed marker stays literal text: the reader sees
// their own typo rather than a half-bold paragraph, and the guard in
// docsProseRender.test.ts fails the build before it can ship.

export type Block =
  | string
  | { list: string[] }
  | { steps: string[] }
  | { tip: string }
  | { note: string }
  | { warn: string }
  // A code SAMPLE. Rendered inside <pre><code> and never parsed, so a backtick
  // or an asterisk in a curl example prints as written.
  | { code: string; caption?: string }
  // A small table. Prose that needs three columns is a table, not a sentence.
  // `label` names the scroll region for a screen reader and must be unique on the
  // page (axe landmark-unique) — the renderer cannot invent one that stays unique.
  | { table: { label: string; head: string[]; rows: string[][] } }
  // A sub-heading inside a section, for a page with two ideas in one section.
  | { heading: string };

export type Section = {
  id: string;
  title: string;
  /** One emoji, shown beside the heading. Aids scanning, never the only cue. */
  icon?: string;
  body: Block[];
};

export type Token =
  | { kind: "text"; text: string }
  // A bold run. `children` holds the tokens of its INNER text when that text
  // carries a marker of its own (`**[label](/href)**`, `**read `x` first**`):
  // the `**…**` alternative matches across the inner marker, so without the
  // nested parse the inner marker used to reach the reader as literal markdown
  // (card t_e101869b). Absent when the run is plain text, so the renderer's
  // common path stays one string.
  | { kind: "strong"; text: string; children?: Token[] }
  | { kind: "code"; text: string }
  | { kind: "link"; text: string; href: string }
  | { kind: "lang"; text: string; lang: string };

const MARKER = /(\*\*[^*]+\*\*|`[^`]+`|\[\[[a-z]{2,3}\|[^\]]+\]\]|\[[^\]]+\]\([^)]+\))/g;
const LINK = /^\[([^\]]+)\]\(([^)]+)\)$/;
const LANG = /^\[\[([a-z]{2,3})\|([^\]]+)\]\]$/;

export function parseInline(text: string): Token[] {
  const out: Token[] = [];
  for (const part of text.split(MARKER)) {
    if (!part) continue;
    if (part.startsWith("**") && part.endsWith("**") && part.length > 4) {
      // A bold run can CONTAIN a marker: `**[label](/href)**`, `**read `x` first**`.
      // The `**…**` alternative matches the whole run in one piece (and `[^*]+`
      // happily matches a link), so the inner marker is only found by parsing the
      // slice. Without this the inner text was handed to the renderer as-is and
      // the reader saw `[label](/href)` with no anchor (card t_e101869b). One
      // level is enough: `[^*]+` cannot contain `*`, so the inner slice can hold
      // no bold run of its own.
      const inner = part.slice(2, -2);
      const children = parseInline(inner);
      out.push(
        children.length === 1 && children[0].kind === "text"
          ? { kind: "strong", text: inner }
          : { kind: "strong", text: inner, children },
      );
    } else if (part.startsWith("`") && part.endsWith("`") && part.length > 2) {
      out.push({ kind: "code", text: part.slice(1, -1) });
    } else if (LANG.test(part)) {
      const m = LANG.exec(part)!;
      out.push({ kind: "lang", text: m[2], lang: m[1] });
    } else if (LINK.test(part)) {
      const m = LINK.exec(part)!;
      out.push({ kind: "link", text: m[1], href: m[2] });
    } else {
      out.push({ kind: "text", text: part });
    }
  }
  return out;
}

// A marker that survived parsing: a stray `**`, a lone backtick, an unclosed
// `[[`, or a whole `[label](href)` that no link token consumed.
const LEAKED = /\*\*|`|\[\[|\[[^\]]*\]\([^)]*\)/;

/**
 * Does this token still SHOW a marker to the reader?
 *
 * It looks INSIDE a token, not just at the `text` kind. The defect this closes
 * (card t_e101869b) was a link written inside **bold**: the `**…**` alternative
 * swallows the whole run into ONE strong token, so a guard that counted only
 * `text` tokens scored it 0 while /docs/mcp/connect printed
 * `[Profile → MCP agents](/profile?tab=mcp)` at the reader. A guard that cannot
 * see the marker cannot fail on it, which is how it shipped green.
 *
 * A `code` token is skipped on purpose: its text is verbatim by definition and
 * cannot hold a marker (the alternation stops at the first backtick).
 */
function leaks(t: Token): boolean {
  if (t.kind === "code") return false;
  if (t.kind === "strong" && t.children) return t.children.some(leaks);
  return LEAKED.test(t.text);
}

/** How many markers survive parsing as literal text — must always be 0. */
export function leakedMarkers(text: string): number {
  return parseInline(text).filter(leaks).length;
}
