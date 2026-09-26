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
//      So the entry point the package's OWN manifest declares must be reachable
//      ON DISK, judged the way the runtime that will load it (this Node) reaches
//      it — not by testing one declared string for existence. Judging the verbatim
//      string was this rule's own defect: on a healthy `npm ci` tree it disagreed
//      with Node for these legitimate manifest shapes and refused a tree that runs
//      the suite fine (found by QA; `decimal.js-light`'s `main: "decimal"` is live
//      in this repo's install graph as a transitive dependency of `recharts`):
//
//        * `exports["."]` when declared, else `main`, else `module` — see
//          `declaredEntries`. `exports` wins because Node ignores `main`/`module`
//          entirely when `exports` is present, and a stale `main` must not fail
//          a good tree.
//        * ARRAY targets fall back on the first element that is INVALID as an
//          export target (not a `./`-relative specifier), which is Node's rule:
//          "array fallbacks are supported for all invalid specifiers"
//          (https://nodejs.org/api/packages.html#package-exports-fallbacks). A
//          VALID element whose file is missing does NOT fall back — verified with
//          a real `import()` here — so that is a refusal.
//        * CONDITION keys are read in MANIFEST ORDER (Node: "key order is
//          significant"), not against a hardcoded preference list. `default` no
//          longer outranks an earlier `import`/`require`.
//        * FILE targets get Node's extension search — `X`, `X.js`, `X.json`,
//          `X.node` (https://nodejs.org/api/modules.html, LOAD_AS_FILE) — and a
//          DIRECTORY target must contain `index.js`/`index.json`/`index.node`
//          (LOAD_INDEX). A nested `package.json` inside the target directory is
//          deliberately NOT followed: for a `main`-declared directory Node
//          ignores it (measured on this tree's Node — `lib/package.json` with
//          `main: "x.js"` beside `lib/x.js` still fails to resolve, and the same
//          package with `lib/index.js` resolves), so following it would make this
//          check agree with a loader nobody has.
//        * The check is PER CONSUMER. `require` and `import` enable different
//          conditions, so one manifest can name a present file for one and an
//          absent file for the other. The package is refused only when NO consumer
//          Node enables can reach an entry point, and then every unreachable entry
//          is named with the condition that reaches it. Requiring both would
//          refuse an import-only package, which Node loads fine.
//        * `browser` is NOT one of the enabled conditions: it is a BUNDLER
//          condition, enabled with `--conditions=browser`, not a Node one. The
//          manifests in this tree that put it first — `@jridgewell/resolve-uri`,
//          `nanoid` — resolve to the browser file for a bundler and to the
//          `require`/`import` file for Node, so preferring it refused a tree Node
//          loads.
//
//   SHAPES THAT ARE PRESENCE-ONLY, AND WHY (this list is exhaustive — anything
//   not in it is judged on its resolved payload):
//     * `@types/*` packages: they ship declarations, and Node exposes no runtime
//       entry point for them on a healthy tree (measured for all four declared
//       here — both `require.resolve` and a real `import()` fail).
//     * A package that declares no entry point at all.
//     * An `exports` map with no `"."` entry (only subpaths): the bare package
//       name is not importable at all in that shape, and the consumer here may
//       only be using a subpath, so there is no main target to judge.
//     * A condition map whose enabled keys reach no valid target (`types` alone,
//       or a custom condition such as `react-server` standing alone, or a target
//       list with no valid element). Node's own answer there is
//       ERR_INVALID_PACKAGE_TARGET / ERR_PACKAGE_PATH_NOT_EXPORTED — the package
//       is not loadable by name whatever this check says. A `.d.ts` is a
//       compile-time problem, not a run-the-suite one, and demanding one would
//       refuse a good tree — the header has always said so; the code now agrees.
//
//   The list of packages is derived from `web/package.json` rather than
//   hardcoded, so a new dependency cannot drift out of this check. The rule is
//   still PRESENCE-based rather than "does the module load": loading a module
//   executes it, and this script runs before `node --test` in the same process
//   tree. Only Node's RESOLUTION rules are mirrored, never its loading.
//
// USAGE
//   node scripts/web-deps-check.mjs            # checks web/ (this script's tree)
//   node scripts/web-deps-check.mjs <webDir>   # checks another tree (the guard
//                                              # test uses this for its fixtures)
//   exit 0 = every declared package is present AND every non-`@types/*` package
//   has a reachable entry point; exit 1 = at least one is not, and the message
//   names the packages, what was looked for, and the command that fixes it.

import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const webDir = resolve(process.argv[2] ?? resolve(HERE, ".."));

/** `@types/*` packages ship type declarations, not a runtime entry point. */
const TYPES_ONLY = /^@types\//;

/** Node's LOAD_AS_FILE extension search, in order. Note `X` itself comes first,
 *  so an extension-ful target that exists is found by its own name. */
const FILE_EXTENSIONS = ["", ".js", ".json", ".node"];

/** Node's LOAD_INDEX, in order. */
const INDEX_FILES = ["index.js", "index.json", "index.node"];

/** Is this an export target Node accepts at all? All targets must be relative
 *  URLs starting with `./`, with no `.`/`..`/`node_modules` segments
 *  (https://nodejs.org/api/packages.html#path-rules-and-validation-for-export-targets).
 *  Only invalid ELEMENTS of an array fall through; a valid element that points at
 *  a missing file does not. */
function isValidExportTarget(value) {
  if (typeof value !== "string" || !value.startsWith("./")) return false;
  const segments = value.slice(2).split("/");
  return segments.every((s) => s.length > 0 && s !== "." && s !== ".." && s !== "node_modules");
}

/** Walk an `exports` value the way Node resolves the "." subpath for ONE
 *  consumer, whose enabled condition keys `conditions` holds.
 *
 *  Semantics, each measured on this tree's Node rather than assumed:
 *   * A string that is NOT a valid export target (`./`-relative, no
 *     `.`/`..`/`node_modules` segment) is SKIPPED and resolution continues — to
 *     the next array element, or the next condition key. That is Node's rule
 *     ("array fallbacks are supported for all invalid specifiers",
 *     https://nodejs.org/api/packages.html#package-exports-fallbacks) and it is
 *     not array-only: `{ ".": { import: "not:valid", default: "./a.js" } }`
 *     resolves `./a.js` for `require`.
 *   * A KEY the consumer does not enable is skipped and the next key is tried, in
 *     MANIFEST ORDER (Node: "key order is significant").
 *   * A VALID target whose file is missing does NOT fall through — Node throws
 *     MODULE_NOT_FOUND there — so it is returned and becomes the refusal.
 *
 *  Returns the target the consumer gets, or null when no enabled key reaches a
 *  valid one (Node's answer in that shape is ERR_INVALID_PACKAGE_TARGET, which
 *  this check cannot describe — see the header's presence-only list). */
function consumerTargetFor(value, conditions) {
  if (typeof value === "string") {
    return isValidExportTarget(value) ? { path: value, via: "default" } : null;
  }
  if (Array.isArray(value)) {
    for (const item of value) {
      const found = consumerTargetFor(item, conditions);
      if (found) return found;
    }
    return null;
  }
  if (value && typeof value === "object") {
    for (const key of Object.keys(value)) {
      if (!conditions.has(key)) continue;
      const found = consumerTargetFor(value[key], conditions);
      // The condition that actually selected this branch, so a refusal can say
      // which half of the manifest it is talking about.
      if (found) return { ...found, via: key };
    }
    return null;
  }
  return null;
}

/** Every target a consumer could be handed, with the condition set that reaches
 *  it — one per ENTRY POINT a runtime consumer gets.
 *
 *  WHY MORE THAN ONE: `require` and `import` enable different conditions
 *  (`require` has no `import`; `import` has no `require`), so one manifest can
 *  legitimately name a present file for one and an absent file for the other.
 *  `{ ".": { browser: "./browser.js", require: "./cjs/index.js" } }` is exactly
 *  that shape, and `@jridgewell/resolve-uri` and `nanoid` — both installed here —
 *  write it. Judging a single "most preferred" target is what produced the false
 *  RED this rule was fixed for.
 *
 *  Node's default sets: `node` is always enabled, `require`/`import` depend on how
 *  the module was asked for, `default` is the universal fallback, and `browser` is
 *  a BUNDLER condition that is deliberately NOT enabled (preferring it would
 *  refuse a tree Node loads). */
function consumerEntries(value) {
  const bases = [["node", "import", "default"], ["node", "require", "default"]];
  const seen = new Set();
  const entries = [];
  for (const base of bases) {
    const conditions = new Set([...base, "node-addons"]);
    const found = consumerTargetFor(value, conditions);
    if (found && !seen.has(found.path)) {
      seen.add(found.path);
      entries.push({ path: found.path, via: found.via });
    }
  }
  return entries;
}

/** What the manifest declares for the "." subpath, as the list of entry points a
 *  runtime consumer gets (see `consumerEntries`), or null when it declares no
 *  target this check can judge. `exports` wins when present — `main`/`module` are
 *  ignored by resolution in that case. */
function declaredEntries(manifest) {
  const exp = manifest.exports;
  if (typeof exp === "string" || Array.isArray(exp)) {
    const entries = consumerEntries(exp);
    return entries.length > 0 ? { field: 'exports["."]', entries } : null;
  }
  if (exp && typeof exp === "object") {
    // A SUBPATH map has keys like `"."` or `"./x"`; a CONDITION map has bare
    // condition names. `"."` is a subpath key, and treating it as a condition
    // — which it is not — would silently drop the payload assertion for every
    // package shaped `exports: { ".": … }`: react, react-dom and tailwindcss in
    // this very tree. (That is how a "presence-only" regression would hide.)
    const isSubpathMap = Object.keys(exp).some((k) => k === "." || k.startsWith("./"));
    if (isSubpathMap) {
      // Subpaths only: the bare package name is not importable at all in that
      // shape, so there is no main target to judge (presence-only, in the
      // header's list).
      if (exp["."] === undefined) return null;
      const entries = consumerEntries(exp["."]);
      return entries.length > 0 ? { field: 'exports["."]', entries } : null;
    }
    // A bare condition map IS the "." target.
    const entries = consumerEntries(exp);
    return entries.length > 0 ? { field: 'exports["."]', entries } : null;
  }
  if (typeof manifest.main === "string" && manifest.main.length > 0) {
    return { field: "main", entries: [{ path: manifest.main, via: "main" }] };
  }
  if (typeof manifest.module === "string" && manifest.module.length > 0) {
    return { field: "module", entries: [{ path: manifest.module, via: "module" }] };
  }
  return null;
}

/** Can this Node reach `declared` from inside `dir`? Mirrors LOAD_AS_FILE
 *  (verbatim, then the extension search) followed by LOAD_INDEX for a directory
 *  target — a target ending in a separator is a directory by definition. */
function resolvesOnDisk(dir, declared) {
  const base = join(dir, declared);
  if (declared.endsWith("/") || declared.endsWith("\\")) {
    return INDEX_FILES.some((f) => existsSync(join(base, f)));
  }
  for (const ext of FILE_EXTENSIONS) {
    const candidate = `${base}${ext}`;
    if (existsSync(candidate) && statSync(candidate).isFile()) return true;
  }
  if (existsSync(base) && statSync(base).isDirectory()) {
    return INDEX_FILES.some((f) => existsSync(join(base, f)));
  }
  return false;
}

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

const missing = []; // no package.json at all
const noPayload = []; // package.json present, its own entry-point file is not
let payloadChecked = 0;
let presenceOnly = 0;

for (const name of declared) {
  const dir = join(nodeModules, name);
  const manifestPath = join(dir, "package.json");
  if (!existsSync(manifestPath)) {
    missing.push(name);
    continue;
  }
  if (TYPES_ONLY.test(name)) {
    // Presence is all an @types package can be asked for (see the header).
    presenceOnly += 1;
    continue;
  }
  let manifest;
  try {
    manifest = JSON.parse(readFileSync(manifestPath, "utf8"));
  } catch (err) {
    noPayload.push(`${name} (package.json present but unreadable: ${err.message})`);
    continue;
  }
  const declaredForPackage = declaredEntries(manifest);
  if (declaredForPackage === null) {
    // Nothing a runtime consumer resolves through — presence is the whole
    // assertion this metadata allows. Not a failure: inventing a rule here is
    // how a prerequisite starts failing good trees.
    presenceOnly += 1;
    continue;
  }
  payloadChecked += 1;
  // A package is usable when AT LEAST ONE consumer Node enables by default can
  // reach an entry point. Requiring every one of them would refuse an import-only
  // (or require-only) package, which Node loads fine and which is therefore not
  // an install hole; and because a VALID-but-missing target does not fall back,
  // which target a consumer gets is fully determined — so "no consumer can reach
  // it" is the only shape that means the payload is not there. Each unreachable
  // entry is named with the condition that reaches it, so a reader can tell which
  // half of the manifest resolved where.
  const unreachable = declaredForPackage.entries.filter((t) => !resolvesOnDisk(dir, t.path));
  if (unreachable.length === declaredForPackage.entries.length) {
    for (const target of unreachable) {
      const via = declaredForPackage.field.startsWith("exports") ? ` [${target.via}]` : "";
      noPayload.push(`${name} (${declaredForPackage.field}${via} names ${target.path} — not on disk)`);
    }
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
      "            point cannot be reached from it, so it cannot be imported)",
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
    `(${entries} entries, ${payloadChecked} entry point(s) checked, ${presenceOnly} presence-only)`,
);
