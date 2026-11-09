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
  | { kind: "strong"; text: string }
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
      out.push({ kind: "strong", text: part.slice(2, -2) });
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

/** How many markers survive parsing as literal text — must always be 0. */
export function leakedMarkers(text: string): number {
  return parseInline(text).filter((t) => t.kind === "text" && /\*\*|`|\[\[/.test(t.text)).length;
}
