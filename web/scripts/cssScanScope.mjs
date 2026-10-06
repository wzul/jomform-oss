// Does the BUILT CSS carry a utility whose ONLY source is a non-shipped file?
//
// WHY THIS EXISTS (card t_850589b5)
//
// Tailwind v4 scans the whole `web/` tree with a plain-text class extractor, and
// it does NOT honour `//` comments. `web/src/app/globals.css` uses `@source not`
// to keep the unit tests (`../**/*.test.{ts,tsx}`) and the Playwright specs
// (`../../e2e/**/*.spec.{ts,tsx}`) out of the emitted bundle, because a class
// literal written in a test's prose — a rationale, an a11y measurement table, a
// fixture id — is compiled into a REAL utility and shipped on every page. This
// script is the EMITTED-BUNDLE half of that fix: it reads the CSS `next build`
// actually wrote and refuses a bundle that still carries such a utility.
//
// `web/src/lib/tailwindScanScope.test.ts` pins the DIRECTIVE and cannot run a
// build, so it can only prove the exclusion is *written*, not that the bundle is
// *clean*. This runs AFTER `next build` (appended to `npm run build`, the same
// placement as `docs-facts`) and reads what was emitted. The decision itself
// lives in `src/lib/cssScanScope.ts`; this file only walks the tree and reports.
//
// FAILS CLOSED. A missing `.next` (a build that wrote nothing), a missing
// stylesheet or an empty non-shipped corpus is an ERROR, never a silent pass —
// the same contract `docs-facts` uses.
//
// WHAT IT COVERS. `web/src/**` (shipped, minus CSS — Tailwind does not scan CSS)
// is the shipped corpus; the non-shipped corpus is `web/e2e/**`, `*.test.ts(x)`,
// **`web/scripts/**` and the web-root build configs** (`package.json`,
// `next.config.mjs`, `postcss.config.mjs`, `playwright.config.ts`) — the second
// half added by card t_bc75e313. Tailwind's extractor reads the WHOLE build tree
// as plain text and `web/` is that tree, so a class literal in a `scripts/*.sh`
// helper or a root config compiles into a real utility just like a test's prose
// does. Measured: appending a hover-prefixed arbitrary-value background literal
// to `web/scripts/operator-bite.sh` and rebuilding emitted that utility into the
// built CSS (+55 B, 61256 -> 61311; 669 -> 670 classes), and the
// pre-t_bc75e313 guard printed `ok` at exit 0. `scripts/**` is not
// excluded from the scan (option (a)); it is BLAMED (option (b)), because a
// helper's literal is dead weight on every page and excluding a whole directory
// from a scan is the silent, hard-to-re-measure half of the two. A class that is
// BOTH in a test/script and in real app code is not a violation, by design — the
// corpus split cannot and should not change it.
//
// Run with:
//   npm run build                       (this runs after `next build`)
//   node --disable-warning=MODULE_TYPELESS_PACKAGE_JSON scripts/cssScanScope.mjs

import { readFileSync, readdirSync, statSync, existsSync } from "node:fs";
import { dirname, join, resolve, relative } from "node:path";
import { fileURLToPath } from "node:url";
import { classLiterals, cssSelectorLiterals, deadUtilities } from "../src/lib/cssScanScope.ts";

const here = dirname(fileURLToPath(import.meta.url));
const webRoot = resolve(here, "..");

function walk(dir, keep) {
  const out = [];
  const stack = [dir];
  while (stack.length > 0) {
    const cur = stack.pop();
    for (const name of readdirSync(cur)) {
      if (name === "node_modules" || name === ".next" || name === ".git") continue;
      const p = join(cur, name);
      if (statSync(p).isDirectory()) stack.push(p);
      else if (keep(p)) out.push(p);
    }
  }
  return out;
}

function readAll(paths) {
  const map = new Map();
  for (const p of paths) {
    try {
      map.set(relative(webRoot, p), readFileSync(p, "utf8"));
    } catch {
      /* unreadable/binary — ignore */
    }
  }
  return map;
}

function cssFiles() {
  const staticDir = join(webRoot, ".next/static");
  if (!existsSync(staticDir)) return [];
  return walk(staticDir, (p) => p.endsWith(".css"));
}

/**
 * Assemble the source corpora this check decides on, from the real `web/` tree.
 *
 * EXPORTED for `src/lib/cssScanScope.test.ts` (card t_bc75e313): the decision in
 * `deadUtilities` was already proven on fixtures, but WHICH FILES it is handed
 * was not — so deleting `web/scripts/**` (or a root config) from `nonShipped`
 * left every unit test green while the gap this card is about came straight
 * back. The test calls this and asserts a script-sourced literal is reported and
 * an `src`-sourced one is not, so the corpus ASSEMBLY is the thing pinned.
 *
 * @returns shipped (real code), nonShipped (tests, e2e, scripts, root configs)
 *          and the stylesheet whose own selectors are the third, "defined" corpus.
 */
export function sourceCorpus() {
  const srcDir = join(webRoot, "src");
  const e2eDir = join(webRoot, "e2e");
  const scriptsDir = join(webRoot, "scripts");
  const isTest = (p) => /\.test\.tsx?$/.test(p);
  // Tailwind does NOT scan CSS files (docs: "Which files are scanned" lists CSS
  // as excluded), so globals.css's own prose never reaches the bundle and must
  // not be counted as a *shipped* source for a class literal its comment names.
  const isCss = (p) => p.endsWith(".css");

  const srcFiles = existsSync(srcDir) ? walk(srcDir, () => true) : [];
  const e2eFiles = existsSync(e2eDir) ? walk(e2eDir, () => true) : [];
  const scriptFiles = existsSync(scriptsDir) ? walk(scriptsDir, () => true) : [];

  // The web-root build files Tailwind's plain-text extractor also reads (build
  // context = repo root, so `web/` IS the tree it scans). Listed BY NAME, not
  // globbed with `*.mjs`: a glob would sweep in this very script, and the point
  // is the four files a build consults — not "every file at the web root".
  const rootConfigFiles = ["package.json", "next.config.mjs", "postcss.config.mjs", "playwright.config.ts"]
    .map((name) => join(webRoot, name))
    .filter(existsSync);

  const shipped = readAll(srcFiles.filter((p) => !isTest(p) && !isCss(p)));
  const nonShipped = readAll(
    [...srcFiles.filter(isTest), ...e2eFiles, ...scriptFiles, ...rootConfigFiles].filter(
      (p) => !isCss(p),
    ),
  );
  const stylesheetPath = join(srcDir, "app/globals.css");
  const stylesheet = existsSync(stylesheetPath) ? readFileSync(stylesheetPath, "utf8") : "";
  return { shipped, nonShipped, stylesheet };
}

// ------------------------------------------------------------------- the run

function main() {
  const css = cssFiles();
  if (css.length === 0) {
    console.error(
      `css-scan-scope: no CSS under ${join(webRoot, ".next/static")} — next build wrote nothing, ` +
        `so there is no bundle to check. This runs AFTER 'next build'; refusing rather than ` +
        `reporting a vacuous pass.`,
    );
    process.exit(1);
  }

  const { shipped, nonShipped, stylesheet } = sourceCorpus();
  if (stylesheet === "") {
    console.error(
      "css-scan-scope: web/src/app/globals.css is missing — the stylesheet whose own selectors " +
        "are the shipped half of this check cannot be read, so it would over-report. Refusing.",
    );
    process.exit(1);
  }
  if (nonShipped.size === 0) {
    console.error(
      "css-scan-scope: found NO non-shipped source files (web/e2e/**, *.test.ts(x), " +
        "web/scripts/** or the web-root build configs) — the corpus this check blames is " +
        "empty, so the check would pass vacuously. Refusing.",
    );
    process.exit(1);
  }

  const bundle = css.map((p) => readFileSync(p, "utf8")).join("\n");
  const violations = deadUtilities(bundle, shipped, nonShipped, cssSelectorLiterals(stylesheet));

  if (violations.length > 0) {
    console.error(
      `css-scan-scope: the BUILT CSS carries ${violations.length} utility(ies) whose only source ` +
        `is a non-shipped file. Tailwind scans its whole build tree as plain text, so a class ` +
        `literal written in a test's prose, a build script or a config is compiled into a REAL ` +
        `utility and shipped on every page. Remove the literal from the non-shipped source (or, ` +
        `for a test/e2e file, widen the \`@source not\` lines in web/src/app/globals.css):\n` +
        violations
          .map((v) => `  .${v.literal}\n      only in: ${v.sources.join(", ")}`)
          .join("\n"),
    );
    process.exit(1);
  }

  const classes = [...classLiterals(bundle)].length;
  console.log(
    `css-scan-scope: ok — ${classes} class(es) in ${css.length} built CSS file(s); none sourced ` +
      `only from web/e2e/**, *.test.ts(x), web/scripts/** or the web-root build configs ` +
      `(${nonShipped.size} non-shipped, ${shipped.size} shipped)`,
  );
}

if (process.argv[1] && resolve(process.argv[1]) === resolve(fileURLToPath(import.meta.url))) {
  main();
}
