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
//   Two things per declared package, because there are two depths at which an
//   install stops being there:
//
//   1. PRESENCE — `<webDir>/node_modules/<name>/package.json` must exist. This is
//      the shape a SHALLOW interruption leaves: whole package directories absent.
//
//   2. PAYLOAD — the directory existing is not the package being usable. A DEEP
//      interruption (an `npm ci` killed after the directories are created but
//      before the tarballs are unpacked; a bind-mounted or cache-restored tree; a
//      partially pruned `node_modules`) leaves EVERY declared directory in place,
//      each holding a package.json and nothing else. Measured on such a tree at
//      this head: the presence check exited 0 and said "14 declared package(s)
//      installed", and `npm run test:unit` then printed `326 tests / 324 pass /
//      2 fail` against `350 / 350 / 0` installed, the failures being
//      ERR_MODULE_NOT_FOUND for `react` — the same false-RED, under-reported
//      shape one interruption deeper. `.bin/next` can survive that too, so the
//      preflight's executable check cannot see it either.
//
//      So for a non-`@types/*` package, the file that package's OWN manifest
//      names as its entry point must be on disk: `exports["."]` when declared
//      (the runtime condition a consumer gets), else `main`, else `module`. No
//      module resolution is involved, the rule is derived from the package's own
//      metadata rather than a hardcoded list, and a hollow directory cannot
//      satisfy it.
//
//   `@types/*` packages are PRESENCE-ONLY on purpose, and that is not a gap: they
//   declare no runtime entry point (some declare no `main` at all), so demanding
//   a runtime target would fail a perfectly healthy tree. A false RED in a
//   prerequisite is its own defect. The list of packages is derived from
//   `web/package.json` rather than hardcoded, so a new dependency cannot drift
//   out of this check.
//
// USAGE
//   node scripts/web-deps-check.mjs            # checks web/ (this script's tree)
//   node scripts/web-deps-check.mjs <webDir>   # checks another tree (the guard
//                                              # test uses this for its fixtures)
//   exit 0 = every declared package is present AND (non-`@types/*`) has its entry
//   point file; exit 1 = at least one is not, and the message names the packages,
//   what was looked for, and the command that fixes it.

import { existsSync, readFileSync, readdirSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const webDir = resolve(process.argv[2] ?? resolve(HERE, ".."));

/** `@types/*` packages ship type declarations, not a runtime entry point. */
const TYPES_ONLY = /^@types\//;

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

/** The conditions a consumer actually loads, most preferred first. A `types`
 *  condition is deliberately NOT in this list: it names a declaration file, not
 *  the payload, so a tree whose only missing file is a `.d.ts` must not be
 *  refused (that is a compile-time problem, not a run-the-suite one). */
const RUNTIME_CONDITIONS = ["default", "import", "require", "node", "browser"];

function firstLeaf(value) {
  if (typeof value === "string") return value.length > 0 ? value : null;
  if (Array.isArray(value)) {
    for (const item of value) {
      const found = firstLeaf(item);
      if (found) return found;
    }
    return null;
  }
  if (value && typeof value === "object") {
    for (const condition of RUNTIME_CONDITIONS) {
      if (condition in value) {
        const found = firstLeaf(value[condition]);
        if (found) return found;
      }
    }
    // No runtime condition at all (only custom conditions, or a types-only map):
    // fall back to whatever is declared, so the check still has something to look
    // at rather than silently passing a package it cannot describe.
    for (const key of Object.keys(value)) {
      const found = firstLeaf(value[key]);
      if (found) return found;
    }
  }
  return null;
}

/** The file `package.json` itself names as the entry point, and the field it came
 *  from (so the refusal can say what it looked at). Follows Node's own order:
 *  `exports["."]` wins when present — `main`/`module` are ignored by resolution in
 *  that case, and a stale `main` left in the manifest must not fail a good tree. */
function declaredEntry(manifest) {
  const root = typeof manifest.exports === "string"
    ? manifest.exports
    : manifest.exports && typeof manifest.exports === "object"
      ? manifest.exports["."]
      : undefined;
  const fromExports = firstLeaf(root);
  if (fromExports) return { path: fromExports, field: 'exports["."]' };
  if (typeof manifest.main === "string" && manifest.main.length > 0) {
    return { path: manifest.main, field: "main" };
  }
  if (typeof manifest.module === "string" && manifest.module.length > 0) {
    return { path: manifest.module, field: "module" };
  }
  return null;
}

const nodeModules = join(webDir, "node_modules");
const installed = existsSync(nodeModules);
const entries = installed ? readdirSync(nodeModules).length : 0;

const missing = []; // no package.json at all
const noPayload = []; // package.json present, its own entry-point file is not
let payloadChecked = 0;

for (const name of declared) {
  const dir = join(nodeModules, name);
  const manifestPath = join(dir, "package.json");
  if (!existsSync(manifestPath)) {
    missing.push(name);
    continue;
  }
  if (TYPES_ONLY.test(name)) {
    // Presence is all an @types package can be asked for (see the header).
    payloadChecked += 1;
    continue;
  }
  let manifest;
  try {
    manifest = JSON.parse(readFileSync(manifestPath, "utf8"));
  } catch (err) {
    noPayload.push(`${name} (package.json present but unreadable: ${err.message})`);
    continue;
  }
  const entry = declaredEntry(manifest);
  if (entry === null) {
    // Nothing declared to look for — presence is the whole assertion this
    // metadata allows. Not a failure: inventing a rule here is how a
    // prerequisite starts failing good trees.
    continue;
  }
  payloadChecked += 1;
  if (!existsSync(join(dir, entry.path))) {
    noPayload.push(`${name} (${entry.field} names ${entry.path} — not on disk)`);
  }
}

if (missing.length > 0 || noPayload.length > 0) {
  const seed = `cd ${webDir} && npm ci`;
  const detail = [];
  if (missing.length > 0) detail.push(`  missing:  ${missing.join(", ")}`);
  if (noPayload.length > 0) {
    detail.push(
      `  no payload: ${noPayload.join("; ")}`,
      "            (the directory for each of these exists, so a check for the",
      "            directory alone says 'installed' — but the package's own entry",
      "            point is not there, so it cannot be imported)",
    );
  }
  console.error(
    [
      "",
      "  ############################################################",
      "  #  web/node_modules IS NOT INSTALLED IN THIS TREE          #",
      "  ############################################################",
      `  tree:     ${webDir}`,
      `  state:    ${installed ? `directory present, ${entries} entr${entries === 1 ? "y" : "ies"}` : "no node_modules directory"}`,
      ...detail,
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
  `web toolchain present: ${declared.length} declared package(s) installed in ${nodeModules} ` +
    `(${entries} entries, ${payloadChecked} entry point(s) checked)`,
);
