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
  /**
   * The line the result row shows under the title: the window of page text that explains
   * WHY this page matched, or the page description when the description is what matched.
   */
  snippet: string;
};

export type SearchGroup = {
  layer: string;
  section: string;
  hits: SearchHit[];
};

const SNIPPET_RADIUS = 70;

/** A page plus the folded forms the scorer compares against, folded once per search. */
type FoldedDoc = {
  doc: SearchDoc;
  title: string;
  headings: string;
  description: string;
  text: string;
};

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

/**
 * The result row's second line: the page text that explains WHY this page matched.
 *
 * It used to anchor on the EARLIEST occurrence of ANY query token. `text` begins with the
 * page's title and description, so a multi-word query anchored on its first stopword ("the",
 * "how", "a") and the row showed the description for every query that matched the page —
 * never the sentence that matched (card t_a92da047).
 *
 * A window is now only a candidate if it carries a token the page's title, description and
 * headings do NOT already contain: when the match is explained by the metadata the row
 * already shows, the description comes back unchanged. Among the candidates the winner is
 * the window covering the most query tokens, each weighted by how rare that token is in the
 * index — a rare word ("appends") says more about why a page matched than a word every page
 * carries ("the"), and token length alone cannot tell those apart.
 */
function snippetAround(folded: FoldedDoc, tokens: string[], weight: Map<string, number>): string {
  const { doc, text } = folded;

  // Every occurrence of every token is an anchor, not just the first: a word the title
  // mentions early usually occurs again in the paragraph that actually matched.
  const anchors: number[] = [];
  for (const token of tokens) {
    let at = text.indexOf(token);
    while (at !== -1) {
      anchors.push(at);
      at = text.indexOf(token, at + 1);
    }
  }
  anchors.sort((a, b) => a - b);
  // A token can match only the description (guide entries index their body as `text`), in
  // which case there is no prose to point at and the description is the explanation.
  if (anchors.length === 0) {
    return doc.description.trim() || doc.text.slice(0, SNIPPET_RADIUS * 2).trim();
  }

  let best: { score: number; start: number; end: number } | null = null;
  for (const at of anchors) {
    const start = Math.max(0, at - SNIPPET_RADIUS);
    const end = Math.min(text.length, at + SNIPPET_RADIUS * 2);
    const window = text.slice(start, end);
    let score = 0;
    let matchedOutsideTheMetadata = false;
    for (const token of tokens) {
      if (!window.includes(token)) continue;
      score += weight.get(token) ?? 1;
      if (
        !folded.title.includes(token) &&
        !folded.description.includes(token) &&
        !folded.headings.includes(token)
      ) {
        matchedOutsideTheMetadata = true;
      }
    }
    // `anchors` is sorted, so a tie keeps the earliest window: the result is stable
    // between keystrokes instead of depending on Map iteration order.
    if (matchedOutsideTheMetadata && (best === null || score > best.score)) {
      best = { score, start, end };
    }
  }

  // Nothing in the prose explains the match: title, description or a heading already does.
  if (best === null) {
    return doc.description.trim() || doc.text.slice(0, SNIPPET_RADIUS * 2).trim();
  }

  const { start, end } = best;
  const [from, to] = snapToWordBoundary(doc.text, start, end);
  return `${from > 0 ? "…" : ""}${doc.text.slice(from, to).trim()}${
    to < doc.text.length ? "…" : ""
  }`;
}

/** Pull a slice out to the nearest space so a snippet never opens mid-word ("…he fields"). */
function snapToWordBoundary(text: string, start: number, end: number): [number, number] {
  let from = start;
  let to = end;
  if (from > 0) {
    const space = text.lastIndexOf(" ", from);
    if (space !== -1 && from - space <= 20) from = space + 1;
  }
  if (to < text.length) {
    const space = text.indexOf(" ", to);
    if (space !== -1 && space - to <= 20) to = space;
  }
  return [from, to];
}

/**
 * Rank the index against a query and group the best hits by their sidebar section.
 * A query whose tokens do not all appear somewhere on a page excludes that page.
 */
export function searchDocs(docs: SearchDoc[], query: string, limit = 12): SearchGroup[] {
  const tokens = searchTokens(query);
  if (tokens.length === 0) return [];

  const folded: FoldedDoc[] = docs.map((doc) => ({
    doc,
    title: fold(doc.title),
    headings: fold(doc.headings.join(" \n ")),
    description: fold(doc.description),
    text: fold(doc.text),
  }));

  // Document frequency: how many pages carry each token. A token every page carries ("the",
  // "form") says almost nothing about why THIS page matched; a token two pages carry is the
  // reason. The snippet window is scored with these so a description full of common words
  // cannot outbid the paragraph that carries the rare one.
  const corpus = folded.map(
    (page) => `${page.title} \n ${page.headings} \n ${page.description} \n ${page.text}`,
  );
  const weight = new Map<string, number>();
  for (const token of tokens) {
    const df = corpus.filter((haystack) => haystack.includes(token)).length;
    weight.set(token, Math.log((folded.length + 1) / (df + 1)) + 1);
  }

  const hits: SearchHit[] = [];
  for (const page of folded) {
    const { title, headings, description, text } = page;

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
    hits.push({ ...page.doc, score, snippet: snippetAround(page, tokens, weight) });
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
