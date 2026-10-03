// Does the BUILT documentation actually render the facts the docs guard asserts?
//
// WHY THIS EXISTS (card t_5cfeb788)
//
// `web/src/lib/docsIndex.test.ts` checks that every cut /docs entry's facts are
// readable at the guide it points to. It reads the guide's SOURCE and strips
// comments first — and that is as far as string matching can go. A mutation that
// deletes the paragraphs from the JSX and keeps their text in module-scope `const`s
// that nothing references was measured GREEN 269/269 with `next build` exit 0, while
// the built page carried the phrases 0 times. "Is this source reachable" is not a
// property of a string, so the source reader cannot answer it and no amount of
// stripping will make it able to.
//
// The built HTML can. Unreferenced module-scope source never reaches it: the thing a
// reader would fail to find is absent. So this reads `.next/server/app/docs*.html`
// instead of the `.tsx`, and it runs as part of `npm run build` AFTER `next build`
// has written those files.
//
// WHY IT IS IN `npm run build` AND NOT A GATE OF ITS OWN
//
// The ordering constraint is real: `npm run build` is `npm run test:unit && next
// build`, so a check INSIDE the unit suite would read the PREVIOUS build or none at
// all. Two placements satisfy that. A separate gate in scripts/preflight.sh is the
// other one, and it was rejected for a concrete reason: the preflight is a fixed
// ordered list of gates and the self-test mirrors it, so inserting a gate changes
// what every diff classifies to and the gate inventory, for an assertion that is
// really the second half of `web`'s own job. Appending to `npm run build` keeps the
// gate set untouched and needs no new arm in the self-test. The cost, stated plainly:
// this script is not a gate that can be selected or skipped on its own — if it fails,
// it fails as `web`.
//
// WHAT IT DOES NOT COVER. It is the SOURCE-side check's complement, not its
// replacement: this reader cannot see that a fact was moved into the WRONG section of
// the right page, and it says nothing about the anchors, the titles or the parity
// assertions the unit suite owns. It answers one question — is the text in the page a
// reader is served — and only for entries that have a destination guide.

import { readFileSync, existsSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { DESTINATION_FACTS, parseFactTargets, checkDestinationFacts } from "../src/lib/docsFacts.ts";

const here = dirname(fileURLToPath(import.meta.url));
const webRoot = resolve(here, "..");

const DOCS_PAGE = join(webRoot, "src/app/docs/page.tsx");
// `next build` writes the app router's server output here, including a static
// page's HTML. The route `/docs/customize` becomes `docs/customize.html`.
const SERVER_APP = join(webRoot, ".next/server/app");

// The reader-visible text of a BUILT page.
//
// The HTML must be de-tagged rather than searched raw, or a fact would pass on an
// attribute (`title="…"`, a JSON payload in a script tag) that the reader never
// sees — which is the same defect in a new costume. Tags are dropped, the handful of
// entities the docs use are decoded, and whitespace is collapsed. `stripUnrendered`
// is deliberately NOT applied: there are no comments in built HTML, and running a
// source-shaped filter over output would only hide text.
function readerVisibleHtml(html) {
  return html
    .replace(/<script[\s\S]*?<\/script>/gi, " ")
    .replace(/<style[\s\S]*?<\/style>/gi, " ")
    .replace(/<[^>]*>/g, " ")
    .replace(/&quot;/g, '"')
    .replace(/&#x27;|&apos;/g, "'")
    .replace(/&amp;/g, "&")
    .replace(/&mdash;/g, "—")
    .replace(/&rarr;/g, "→")
    .replace(/&nbsp;/g, " ")
    .replace(/\s+/g, " ")
    .trim();
}

if (!existsSync(DOCS_PAGE)) {
  console.error(`docs-facts: ${DOCS_PAGE} is missing — the fact tables cannot be checked`);
  process.exit(1);
}
if (!existsSync(SERVER_APP)) {
  // Failing closed, not skipping: a build that did not write its output would
  // otherwise report success for a check that read nothing.
  console.error(
    `docs-facts: ${SERVER_APP} does not exist, so there is no built page to check. ` +
      `This runs AFTER 'next build' — a build that wrote nothing must not read as a pass.`,
  );
  process.exit(1);
}

const targets = parseFactTargets(readFileSync(DOCS_PAGE, "utf8"));
if (targets.length === 0) {
  console.error(
    "docs-facts: the GROUPS table could not be parsed from src/app/docs/page.tsx, so there is " +
      "nothing to check — refusing rather than reporting a vacuous pass",
  );
  process.exit(1);
}

// An href (`/docs/customize#templates`) maps to the built file that must carry it.
function htmlFileFor(href) {
  const [pathname] = href.split("#");
  if (pathname === "/docs") return join(SERVER_APP, "docs.html");
  if (!pathname.startsWith("/docs/")) return undefined;
  const seg = pathname.slice("/docs/".length);
  if (!/^[a-z0-9-]+$/.test(seg)) return undefined;
  return join(SERVER_APP, `docs/${seg}.html`);
}

const htmlCache = new Map();
const textOf = (file) => {
  if (!existsSync(file)) return undefined;
  let t = htmlCache.get(file);
  if (t === undefined) {
    t = readerVisibleHtml(readFileSync(file, "utf8"));
    htmlCache.set(file, t);
  }
  return t;
};

const missing = checkDestinationFacts({
  targets,
  hrefToKey: htmlFileFor,
  readerTextOf: textOf,
});

// Only facts for entries the page actually links are checked; the unit test owns the
// "every linked entry has facts" and "no facts for an unlinked entry" halves, and
// duplicating them here would mean two places to change.
const linked = targets.filter((t) => t.href && DESTINATION_FACTS[t.title]).length;
if (linked === 0) {
  console.error(
    "docs-facts: no linked entry with facts was found — the tables and the page have drifted " +
      "apart, so this check would pass vacuously. Refusing.",
  );
  process.exit(1);
}

if (missing.length > 0) {
  console.error(
    "docs-facts: the BUILT documentation does not render these facts. The unit suite checks the " +
      "source and cannot see this — text that no component references never reaches the page:\n  " +
      missing.join("\n  ") +
      "\n\nA fact must be reachable by a reader, not merely present in the tree. If the copy " +
      "genuinely moved, assert it against wherever it now renders; do not delete it from " +
      "src/lib/docsFacts.ts without saying why in the same commit.",
  );
  process.exit(1);
}

console.log(
  `docs-facts: ok — ${linked} linked entries, all facts present in the built pages ` +
    `(.next/server/app/docs*.html)`,
);
