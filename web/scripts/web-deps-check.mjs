#!/usr/bin/env node
// web/scripts/web-deps-check.mjs — "is THIS tree's web toolchain actually installed?"
//
// WHY THIS EXISTS
//   `npm run test:unit` is `node --test … src/lib/*.test.ts`. `node --test` needs
//   no dependencies to START, so in a tree whose `web/node_modules` is missing or
//   empty the run still happens — it collects fewer test files than the tree
//   defines and prints a plausible-looking count, because the files it cannot
//   load (the ones importing `react`) die at module load. Measured on a clean
//   worktree at one head: `254 tests / 253 pass / 1 fail`; the same head with a
//   real install: `260 tests / 260 pass / 0 fail`. That one failure is an
//   artifact of the missing install, and the 6 tests that never registered are
//   silently absent from the summary — a false RED and a silent under-report in
//   the same run. `npm run build` does fail in that tree, but for a different
//   reason (`next` is not there), so the *unit* half is the half that lies.
//
//   The preflight gate (`scripts/preflight.sh`) separately requires
//   `web/node_modules/.bin/next` to be executable, which catches "no install at
//   all". It cannot catch the shape that produced the readings above: a
//   `node_modules` DIRECTORY that exists and is empty, or one holding only a
//   stray subdirectory — `.bin/next` can be present in a tree that cannot run
//   the suite. So the install is asserted here, by the thing that is about to
//   depend on it, and `npm run test:unit` refuses instead of under-reporting.
//
// WHAT IT CHECKS, AND WHY THIS SHAPE
//   Every package named in `web/package.json` (`dependencies` + `devDependencies`)
//   must be INSTALLED: `<webDir>/node_modules/<name>/package.json` must exist.
//   Filesystem presence, not module resolution, on purpose — resolution is
//   package-shape dependent (`@types/*` packages have no runtime entry point, so
//   `import.meta.resolve("@types/node")` fails on a perfectly healthy tree) and a
//   false RED in a prerequisite check is its own defect. The list is derived from
//   package.json rather than hardcoded, so a new dependency cannot drift out of
//   this check.
//
// USAGE
//   node scripts/web-deps-check.mjs            # checks web/ (this script's tree)
//   node scripts/web-deps-check.mjs <webDir>   # checks another tree (the guard
//                                              # test uses this for its fixtures)
//   exit 0 = every declared package is present; exit 1 = at least one is not, and
//   the message names the packages and the command that fixes it.

import { existsSync, readFileSync, readdirSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const webDir = resolve(process.argv[2] ?? resolve(HERE, ".."));

const pkgPath = join(webDir, "package.json");
if (!existsSync(pkgPath)) {
  console.error(`FATAL: ${pkgPath} not found — this is not a web tree, so the install cannot be checked`);
  process.exit(1);
}

let pkg;
try {
  pkg = JSON.parse(readFileSync(pkgPath, "utf8"));
} catch (err) {
  console.error(`FATAL: ${pkgPath} is not readable JSON: ${err.message}`);
  process.exit(1);
}

const declared = [
  ...Object.keys(pkg.dependencies ?? {}),
  ...Object.keys(pkg.devDependencies ?? {}),
].sort();

const nodeModules = join(webDir, "node_modules");
const installed = existsSync(nodeModules);
const entries = installed ? readdirSync(nodeModules).length : 0;
const missing = declared.filter((name) => !existsSync(join(nodeModules, name, "package.json")));

if (missing.length > 0) {
  const seed = `cd ${webDir} && npm ci`;
  console.error(
    [
      "",
      "  ############################################################",
      "  #  web/node_modules IS NOT INSTALLED IN THIS TREE          #",
      "  ############################################################",
      `  tree:     ${webDir}`,
      `  state:    ${installed ? `directory present, ${entries} entr${entries === 1 ? "y" : "ies"}` : "no node_modules directory"}`,
      `  missing:  ${missing.join(", ")}`,
      "  ",
      "  'npm run test:unit' would STILL RUN in this tree and print a",
      "  count that looks authoritative: node --test needs no",
      "  dependencies, so the test files that import them fail at",
      "  module load, their remaining tests never register, and the",
      "  summary under-reports instead of failing loudly. That is not",
      "  evidence about the code.",
      "  ",
      "  Seed the install first:",
      `      ${seed}`,
      "  ############################################################",
      "",
    ].join("\n"),
  );
  process.exit(1);
}

console.log(
  `web toolchain present: ${declared.length} declared package(s) installed in ${nodeModules} (${entries} entries)`,
);
