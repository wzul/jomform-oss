// The pure half of the "does the BUILT CSS carry a utility whose only source is
// a non-shipped file" check (card t_850589b5).
//
// WHY THIS EXISTS
//   Tailwind v4 scans the whole `web/` tree with a plain-text class extractor
//   that does NOT honour `//` comments, so a class literal written in a test's
//   prose — a rationale, an a11y measurement table, a fixture id — is compiled
//   into a REAL utility and shipped on every page. `web/src/app/globals.css`
//   narrows the scan with two `@source not` lines (unit tests, and the
//   Playwright specs under `web/e2e`). This module reads the CSS `next build`
//   actually emitted and decides which emitted classes are sourced ONLY from a
//   non-shipped file — the property a source-only guard cannot check, because it
//   does not run a build.
//
//   It is a standalone `.ts` module (no `node:fs`, no `next` import) so the unit
//   suite can prove the DECISION bites on fixtures, and `scripts/cssScanScope.mjs`
//   drives it over the real tree as the build-time assertion — the same split as
//   `src/lib/docsFacts.ts` (<- `scripts/docsFacts.mjs`).
//
// THE DECISION, per emitted class literal (de-escaped from the CSS back to the
// source text Tailwind matched):
//   - a class the stylesheet's OWN rules declare (`selectorDefined`) → fine;
//   - present in a SHIPPED file → fine; a real class in real code may be emitted;
//   - otherwise, present in a NON-SHIPPED file (any file outside `web/src/**`:
//     a `*.test.ts(x)`, `web/e2e/**`, a `web/scripts/**` helper, a build config,
//     `web/Dockerfile`, `web/public/**`, …) → VIOLATION: the bundle carries a
//     utility that only a non-shipped file names;
//   - present in neither → skipped. Tailwind defaults, arbitrary values and
//     literals no file carries are outside this scope.
//
// WHAT IT COVERS. Only the corpora it is handed are searched. The driver builds
// them from the tree by Tailwind's own "which files are scanned" rule (cards
// t_bc75e313, t_682ebba5), so any file that can reach the bundle without being
// shipped code is in `nonShipped`: a class sourced ONLY from a test, a build
// script, a Dockerfile or a config is a violation; one that is BOTH in a
// non-shipped file and in real app code is not, by design.

// A tail of characters Tailwind can put in a class, in the ESCAPED form it emits
// in CSS (`.hover\:bg-\[\#828fff\]:hover`).
const CSS_CLASS = /\.[-!A-Za-z_][A-Za-z0-9_\\#\[\]().%/:-]*/g;
// The same, but only where a SELECTOR can begin (`^`, whitespace or one of
// `,{()>+~`) — so a class merely mentioned in a stylesheet comment is not read
// as one the stylesheet defines.
const CSS_SELECTOR_CLASS = /(?:^|[\s,{()>+~])(\.[-A-Za-z_][A-Za-z0-9_\\#\[\]().%/:-]*)/gm;

/**
 * The literal Tailwind MATCHED, from an emitted class token that may carry
 * structural pseudo-classes Tailwind appends for a variant. A `hover:`-prefixed
 * utility with an arbitrary value is emitted with its class colon ESCAPED (`\:`),
 * followed by a trailing `:hover` pseudo that is NOT part of the class — so the
 * class ends at the first unescaped `:`. Without this, a variant-prefixed dead
 * utility would never match the source literal that caused it, because the
 * bundle only carries the `:hover` form.
 */
function coreClass(escapedToken: string): string {
  const body = escapedToken.slice(1); // drop the leading '.'
  let end = body.length;
  for (let i = 0; i < body.length; i++) {
    if (body[i] === ":" && body[i - 1] !== "\\") {
      end = i;
      break;
    }
  }
  return body.slice(0, end).replace(/\\(.)/g, "$1");
}

/** Every class token in a bundle, reduced to the literal Tailwind matched. */
export function classLiterals(cssText: string): Set<string> {
  const out = new Set<string>();
  for (const token of cssText.match(CSS_CLASS) ?? []) out.add(coreClass(token));
  return out;
}

/**
 * Every class literal a STYLESHEET's own selectors declare. Tailwind does not
 * scan CSS files, so a hand-written rule in `globals.css` (e.g.
 * `.jf-btn-primary:hover:not(:disabled)`) does not arrive via the extractor — it
 * IS the CSS — and must never be read as a "dead utility sourced from a test".
 * Comments are stripped first: globals.css's OWN prose names class literals, and
 * prose is not a definition.
 */
export function cssSelectorLiterals(cssText: string): Set<string> {
  const stripped = cssText.replace(/\/\*[\s\S]*?\*\//g, " ");
  const out = new Set<string>();
  for (const m of stripped.matchAll(CSS_SELECTOR_CLASS)) out.add(coreClass(m[1]));
  return out;
}

/** True when `literal` occurs in `text` as a whole class, not inside `form-1`. */
export function occursAsClass(text: string, literal: string): boolean {
  if (!text.includes(literal)) return false;
  const esc = literal.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return new RegExp(`(?<![\\w-])${esc}(?![\\w-])`).test(text);
}

export interface DeadUtility {
  /** The source class literal (de-escaped), e.g. the red background utility. */
  literal: string;
  /** The non-shipped files that name it, in path order. */
  sources: string[];
}

/**
 * Class literals in `cssText` whose only source is a non-shipped file.
 *
 * @param cssText        the emitted bundle
 * @param shipped        path -> text, shipped source (real code)
 * @param nonShipped     path -> text, tests + e2e
 * @param selectorDefined class literals the stylesheet's own rules declare
 */
export function deadUtilities(
  cssText: string,
  shipped: ReadonlyMap<string, string>,
  nonShipped: ReadonlyMap<string, string>,
  selectorDefined: ReadonlySet<string> = new Set(),
): DeadUtility[] {
  const shippedText = [...shipped.values()].join("\n");
  const violations: DeadUtility[] = [];
  for (const literal of classLiterals(cssText)) {
    if (selectorDefined.has(literal)) continue;
    if (occursAsClass(shippedText, literal)) continue;
    const sources = [...nonShipped.entries()]
      .filter(([, text]) => occursAsClass(text, literal))
      .map(([path]) => path);
    if (sources.length > 0) violations.push({ literal, sources });
  }
  return violations.sort((a, b) => a.literal.localeCompare(b.literal));
}
