// The /docs search scorer (card t_45fd1bee).
//
// Pure and client-safe: it takes the build-time index (`docsSearch.gen.json`, generated
// by web/scripts/docsNav.mjs) and a query string, and returns hits grouped by section.
// Nothing here fetches — the dialog calls this on every keystroke, and the whole point of
// generating the index at build time is that typing never issues a request.
//
// WHY A SCORER AND NOT `index.includes(query)`: every token must match (AND), and a match
// in a page's TITLE is worth far more than one buried in its prose — otherwise searching
// "shipping" ranks a long guide that mentions the word once above /docs/shipping. Ties are
// broken by the registry order, so the result list is stable between keystrokes.

export type SearchDoc = {
  path: string;
  title: string;
  description: string;
  layer: string;
  section: string;
  order: number;
  headings: string[];
  text: string;
};

export type SearchHit = SearchDoc & {
  score: number;
  /** A window of the page text around the first match, for the result list. */
  snippet: string;
};

export type SearchGroup = {
  layer: string;
  section: string;
  hits: SearchHit[];
};

const SNIPPET_RADIUS = 70;

function fold(value: string): string {
  return value
    .toLowerCase()
    .normalize("NFKD")
    .replace(/[\u0300-\u036f]/g, "");
}

/** Lower-case word tokens; a one-letter token is dropped unless it is a number. */
export function searchTokens(query: string): string[] {
  const tokens = fold(query)
    .split(/[^a-z0-9]+/)
    .filter((token) => token.length > 1 || /^[0-9]$/.test(token));
  return [...new Set(tokens)];
}

function occurrences(haystack: string, needle: string): number {
  if (!needle) return 0;
  let count = 0;
  let at = haystack.indexOf(needle);
  while (at !== -1) {
    count += 1;
    at = haystack.indexOf(needle, at + needle.length);
  }
  return count;
}

function snippetAround(text: string, tokens: string[]): string {
  const folded = fold(text);
  let at = -1;
  for (const token of tokens) {
    const found = folded.indexOf(token);
    if (found !== -1 && (at === -1 || found < at)) at = found;
  }
  if (at === -1) return text.slice(0, SNIPPET_RADIUS * 2).trim();
  const start = Math.max(0, at - SNIPPET_RADIUS);
  const end = Math.min(text.length, at + SNIPPET_RADIUS * 2);
  return `${start > 0 ? "…" : ""}${text.slice(start, end).trim()}${end < text.length ? "…" : ""}`;
}

/**
 * Rank the index against a query and group the best hits by their sidebar section.
 * A query whose tokens do not all appear somewhere on a page excludes that page.
 */
export function searchDocs(docs: SearchDoc[], query: string, limit = 12): SearchGroup[] {
  const tokens = searchTokens(query);
  if (tokens.length === 0) return [];

  const hits: SearchHit[] = [];
  for (const doc of docs) {
    const title = fold(doc.title);
    const headings = fold(doc.headings.join(" \n "));
    const description = fold(doc.description);
    const text = fold(doc.text);

    let score = 0;
    let matchesAll = true;
    for (const token of tokens) {
      const inTitle = title.includes(token);
      const inHeading = headings.includes(token);
      const inDescription = description.includes(token);
      const inText = text.includes(token);
      if (!inTitle && !inHeading && !inDescription && !inText) {
        matchesAll = false;
        break;
      }
      if (inTitle) score += 60;
      if (inHeading) score += 25;
      if (inDescription) score += 10;
      // A word repeated in the prose is relevant, but never enough to outrank a title.
      score += Math.min(10, occurrences(text, token));
      if (title.startsWith(token) || new RegExp(`\\b${token}`).test(title)) score += 40;
    }
    if (!matchesAll) continue;
    hits.push({ ...doc, score, snippet: snippetAround(doc.text, tokens) });
  }

  hits.sort((a, b) => b.score - a.score || a.order - b.order);

  const groups: SearchGroup[] = [];
  for (const hit of hits.slice(0, limit)) {
    let group = groups.find((g) => g.section === hit.section && g.layer === hit.layer);
    if (!group) {
      group = { layer: hit.layer, section: hit.section, hits: [] };
      groups.push(group);
    }
    group.hits.push(hit);
  }
  return groups;
}

/** The flat option order the dialog's arrow keys walk (one list across the groups). */
export function flattenHits(groups: SearchGroup[]): SearchHit[] {
  return groups.flatMap((group) => group.hits);
}
