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
// WHAT IT DOES NOT COVER. Only `web/src/**` (shipped, minus CSS — Tailwind does
// not scan CSS) and `web/e2e/**` plus `*.test.ts(x)` (non-shipped) are searched,
// so a class literal living in a config (`web/next.config.mjs`), a build script
// or a stylesheet comment is neither blamed nor cleared. A class that is BOTH in
// a test and in real app code is not a violation, by design — the exclusion
// cannot and should not change it.
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

function sourceCorpus() {
  const srcDir = join(webRoot, "src");
  const e2eDir = join(webRoot, "e2e");
  const isTest = (p) => /\.test\.tsx?$/.test(p);
  // Tailwind does NOT scan CSS files (docs: "Which files are scanned" lists CSS
  // as excluded), so globals.css's own prose never reaches the bundle and must
  // not be counted as a *shipped* source for a class literal its comment names.
  const isCss = (p) => p.endsWith(".css");

  const srcFiles = existsSync(srcDir) ? walk(srcDir, () => true) : [];
  const e2eFiles = existsSync(e2eDir) ? walk(e2eDir, () => true) : [];

  const shipped = readAll(srcFiles.filter((p) => !isTest(p) && !isCss(p)));
  const nonShipped = readAll([...srcFiles.filter(isTest), ...e2eFiles].filter((p) => !isCss(p)));
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
      "css-scan-scope: found NO non-shipped source files (web/e2e/** or *.test.ts(x)) — the " +
        "corpus this check blames is empty, so the check would pass vacuously. Refusing.",
    );
    process.exit(1);
  }

  const bundle = css.map((p) => readFileSync(p, "utf8")).join("\n");
  const violations = deadUtilities(bundle, shipped, nonShipped, cssSelectorLiterals(stylesheet));

  if (violations.length > 0) {
    console.error(
      `css-scan-scope: the BUILT CSS carries ${violations.length} utility(ies) whose only source ` +
        `is a non-shipped file. Tailwind scans test/e2e prose, so a class literal written there ` +
        `ships on every page and re-arms a real class for whoever later uses it. Widen the ` +
        `\`@source not\` lines in web/src/app/globals.css instead of rewording the comment:\n` +
        violations
          .map((v) => `  .${v.literal}\n      only in: ${v.sources.join(", ")}`)
          .join("\n"),
    );
    process.exit(1);
  }

  const classes = [...classLiterals(bundle)].length;
  console.log(
    `css-scan-scope: ok — ${classes} class(es) in ${css.length} built CSS file(s); none sourced ` +
      `only from web/e2e/** or *.test.ts(x) (${nonShipped.size} non-shipped, ${shipped.size} shipped)`,
  );
}

if (process.argv[1] && resolve(process.argv[1]) === resolve(fileURLToPath(import.meta.url))) {
  main();
}
