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
// WHAT IT COVERS (card t_682ebba5 — this half used to be a hand-written list)
//
// The corpora are the SHAPE OF THE TREE, not a list of names. `web/src/**`
// (shipped, minus CSS) is the shipped corpus; everything ELSE the tree holds is
// the non-shipped corpus — `web/e2e/**`, `*.test.ts(x)`, `web/scripts/**`, the
// build configs, `web/public/**`, `web/.gitignore`, `web/Dockerfile` — because
// the question this check asks is "must this file be able to contribute a
// utility?" and for every one of those the answer is no.
//
// WHY THE LIST HAD TO GO. It named four root configs by hand
// (`package.json`, `next.config.mjs`, `postcss.config.mjs`,
// `playwright.config.ts`), so a class literal in any OTHER file Tailwind reads
// shipped a real utility while the guard printed `ok`. Measured on the
// t_bc75e313 head: a hover-prefixed arbitrary-value literal appended to
// `web/Dockerfile` rebuilt the bundle 61329 -> 61384 B (670 -> 671 classes) with
// the literal emitted, and the driver exited 0. `web/tsconfig.json` is the same
// shape (measured: both literals in the bundle, driver exit 0). The by-name list
// is exactly what drifts, so the corpus is now derived from the tree.
//
// WHAT THE WALK MIRRORS (Tailwind's own rule, measured rather than assumed;
// docs: https://tailwindcss.com/docs/detecting-classes-in-source-files#which-files-are-scanned
// — "Tailwind will scan every file in your project … except: files in your
// .gitignore; files in node_modules; binary files; CSS files; common package
// manager lock files"):
//
//   shape                              Tailwind   this corpus
//   src/app/page.tsx (real code)        scanned    SHIPPED
//   src/lib/x.test.ts                   excluded   non-shipped
//   e2e/x.spec.ts                       excluded   non-shipped
//   scripts/*.sh, *.mjs                 scanned    non-shipped
//   web/Dockerfile (no extension)       scanned    non-shipped   <- was in NO corpus
//   web/tsconfig.json, .gitignore       scanned    non-shipped   <- tsconfig.json was in NO corpus
//   public/**/*.{txt,json,svg}          scanned    non-shipped   <- was in NO corpus
//   public/**/*.png (binary)            excluded   not read
//   a *.css file                        excluded   not read
//   package-lock.json                   excluded   not read
//   next-env.d.ts (gitignored)          excluded   non-shipped
//
// The measured column is a real `next build` per row (cards t_bc75e313 and
// t_682ebba5): the literal in `Dockerfile`, `tsconfig.json`, `public/robots.txt`,
// `public/og-default.svg` and a dot-file each reached `.next/static/chunks/*.css`;
// the same literal in a `.png`, in an unimported `.css` and in `package-lock.json`
// did not. Note the last row: `next-env.d.ts` is generated, so it stays in the
// corpus even though Tailwind skips it — an over-inclusive corpus can only BLAME
// one file too many (loudly, and visible), never clear a real violation.
//
// `@source not` (excluding a directory from the scan) is deliberately NOT used
// for these files: the decision recorded for this guard is blame, not exclude —
// the bundle is the thing a reader can measure, and an exclusion is the silent
// half of the two.
//
// Run with:
//   npm run build                       (this runs after `next build`)
//   node --disable-warning=MODULE_TYPELESS_PACKAGE_JSON scripts/cssScanScope.mjs

import { readFileSync, readdirSync, statSync, existsSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { dirname, join, resolve, relative, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { classLiterals, cssSelectorLiterals, deadUtilities } from "../src/lib/cssScanScope.ts";

const here = dirname(fileURLToPath(import.meta.url));
const webRoot = resolve(here, "..");

/** Exported so `src/lib/cssScanScope.test.ts` can name the same tree this driver
 *  walks, rather than recomputing the web root from its own location (a test
 *  that hardcodes `../../` silently checks a different tree if this file moves). */
export { webRoot };

/** Directories Tailwind's walker never descends into: dependencies, build
 *  output, and VCS metadata (the `.git` half of "files in your .gitignore" —
 *  nothing under it is ever a source). */
const SKIP_DIRS = new Set(["node_modules", ".next", ".git"]);

/** Tailwind does not scan CSS files (the docs' "Which files are scanned" lists
 *  CSS as excluded), so globals.css's own prose must not count as a *shipped*
 *  source for a class literal its comment names — nor as a non-shipped one. */
const CSS_FILE = /\.css$/;

/** A unit test — non-shipped even though it lives under `src/`. */
const TEST_FILE = /\.test\.tsx?$/;

/** Package-manager lock files, which Tailwind skips (docs, and measured: a class
 *  literal in `web/package-lock.json` never reached the bundle). Matched on the
 *  basename so a nested lock file is covered too. */
const LOCK_FILE =
  /(^|\/)(package-lock\.json|npm-shrinkwrap\.json|yarn\.lock|pnpm-lock\.ya?ml|bun\.lockb?|deno\.lock)$/;

/** Path relative to the web root, always `/`-separated (the keys the tests and
 *  the messages use, on every platform). */
const relOf = (p) => relative(webRoot, p).split(sep).join("/");

/** Every file under `dir` whose path passes `keep`, minus `SKIP_DIRS`. */
function walk(dir, keep) {
  const out = [];
  const stack = [dir];
  while (stack.length > 0) {
    const cur = stack.pop();
    for (const name of readdirSync(cur)) {
      if (SKIP_DIRS.has(name)) continue;
      const p = join(cur, name);
      if (statSync(p).isDirectory()) stack.push(p);
      else if (keep(p)) out.push(p);
    }
  }
  return out;
}

/** Every file under `dir`, minus `SKIP_DIRS`. Absolute paths. */
const walkTree = (dir) => walk(dir, () => true);

/**
 * The text of a file, or null when Tailwind would not read it as text.
 *
 * "Binary" is judged the way git's and Tailwind's walkers judge it — a NUL byte
 * in the content — and NOT by extension: measured, `public/brand/icon.svg` (text
 * XML) IS scanned and a `.png` is not, so an extension rule would be wrong in
 * both directions. `null` also covers an unreadable file: it is not a source
 * this check can judge, and guessing at it would invent a literal.
 */
function readSource(path) {
  let buf;
  try {
    buf = readFileSync(path);
  } catch {
    return null;
  }
  if (buf.includes(0)) return null;
  return buf.toString("utf8");
}

/**
 * The paths git IGNORES, asked of git — the walker's ignore crate and this check
 * then agree by construction rather than by two implementations of the same
 * rules. ONE process for the whole tree (`--stdin -z`: NUL-delimited in, NUL-
 * delimited out, so a path with a space or a quote cannot shift the answer).
 *
 * Returns null when the question CANNOT be asked: no git on PATH, or not a work
 * tree — the shape `nextManagedFiles.test.ts` also skips on, and the one the web
 * IMAGE is (it ships no git and no `.git`; `next build` runs `npm run build`
 * inside it). Null means "no filtering is possible here", never "nothing is
 * ignored": the corpus then keeps every file it can read, which can only blame
 * one file too many, never clear a real violation.
 */
function gitIgnoredPaths(paths) {
  if (paths.length === 0) return new Set();
  try {
    const out = execFileSync("git", ["-C", webRoot, "check-ignore", "--stdin", "-z"], {
      input: paths.join("\0"),
      encoding: "utf8",
      maxBuffer: 64 * 1024 * 1024,
    });
    return new Set(out.split("\0").filter(Boolean));
  } catch (err) {
    // Exit 1 with no output is `check-ignore`'s "no path matched a rule" answer,
    // not a failure. Anything else (128 = not a work tree, ENOENT = no git on
    // PATH) means the rule set is unknowable here.
    if (err && err.status === 1 && !String(err.stdout ?? "")) return new Set();
    return null;
  }
}

/**
 * Assemble the source corpora this check decides on, from the real `web/` tree
 * and by Tailwind's own "which files are scanned" rule (see the header).
 *
 * EXPORTED for `src/lib/cssScanScope.test.ts` (cards t_bc75e313, t_682ebba5):
 * the decision in `deadUtilities` was already proven on fixtures, but WHICH FILES
 * it is handed was not — so deleting `web/scripts/**`, a root config or (before
 * this card) `web/Dockerfile` from `nonShipped` left every fixture test green
 * while the gap came straight back. The test calls this and asserts a literal
 * sourced only from one of those files is reported and an `src`-sourced one is
 * not, so the corpus ASSEMBLY is the thing pinned.
 *
 * @returns shipped (real code), nonShipped (every other readable file in the
 *          tree), the stylesheet whose own selectors are the third, "defined"
 *          corpus, and the git-ignored set (`null` when git could not answer).
 */
export function sourceCorpus() {
  const allFiles = walkTree(webRoot);
  const rels = allFiles.map(relOf);
  const ignored = gitIgnoredPaths(rels);

  const shipped = new Map();
  const nonShipped = new Map();
  for (let i = 0; i < allFiles.length; i++) {
    const rel = rels[i];
    // Tailwind cannot read these as source at all, so they are in neither corpus.
    if (CSS_FILE.test(rel) || LOCK_FILE.test(rel)) continue;
    // A file git ignores is not source. `null` (no git, no work tree) keeps it.
    if (ignored !== null && ignored.has(rel)) continue;
    const text = readSource(allFiles[i]);
    if (text === null) continue; // binary or unreadable
    if (rel.startsWith("src/") && !TEST_FILE.test(rel)) shipped.set(rel, text);
    else nonShipped.set(rel, text);
  }

  const stylesheetPath = join(webRoot, "src/app/globals.css");
  const stylesheet = existsSync(stylesheetPath) ? readFileSync(stylesheetPath, "utf8") : "";
  return { shipped, nonShipped, stylesheet, ignored };
}

// ------------------------------------------------------------------- the run

function main() {
  const staticDir = join(webRoot, ".next/static");
  const css = existsSync(staticDir) ? walk(staticDir, (p) => p.endsWith(".css")) : [];
  if (css.length === 0) {
    console.error(
      `css-scan-scope: no CSS under ${staticDir} — next build wrote nothing, ` +
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
      "css-scan-scope: found NO non-shipped source files — every file outside web/src/** is the " +
        "corpus this check blames, so an empty one means the walk is not seeing this tree (a " +
        "wrong root, an unreadable tree) and the check would pass vacuously. Refusing.",
    );
    process.exit(1);
  }
  if (shipped.size === 0) {
    console.error(
      "css-scan-scope: found NO shipped source files under web/src/** — without them every " +
        "emitted class looks like it came only from a non-shipped file, so this is a broken " +
        "walk, not a clean bundle. Refusing.",
    );
    process.exit(1);
  }

  const bundle = css.map((p) => readFileSync(p, "utf8")).join("\n");
  const violations = deadUtilities(bundle, shipped, nonShipped, cssSelectorLiterals(stylesheet));

  if (violations.length > 0) {
    console.error(
      `css-scan-scope: the BUILT CSS carries ${violations.length} utility(ies) whose only source ` +
        `is a non-shipped file. Tailwind scans its whole build tree as plain text, so a class ` +
        `literal written in a test's prose, a build script, a Dockerfile or a config is compiled ` +
        `into a REAL utility and shipped on every page. Remove the literal from the non-shipped ` +
        `source (or, for a test/e2e file, widen the \`@source not\` lines in web/src/app/globals.css):\n` +
        violations
          .map((v) => `  .${v.literal}\n      only in: ${v.sources.join(", ")}`)
          .join("\n"),
    );
    process.exit(1);
  }

  const classes = [...classLiterals(bundle)].length;
  console.log(
    `css-scan-scope: ok — ${classes} class(es) in ${css.length} built CSS file(s); none sourced ` +
      `only from a file outside web/src/** (${nonShipped.size} non-shipped, ${shipped.size} shipped)`,
  );
}

if (process.argv[1] && resolve(process.argv[1]) === resolve(fileURLToPath(import.meta.url))) {
  main();
}
