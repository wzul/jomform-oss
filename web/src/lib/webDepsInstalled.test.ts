// Guard: `npm run test:unit` must REFUSE in a tree whose web dependencies are not
// installed, instead of printing a count that looks like the suite's.
//
// THE DEFECT (measured, not inferred)
//   `test:unit` is `node --test … src/lib/*.test.ts`. `node --test` needs no
//   dependencies to start, so in a tree with NO `web/node_modules` — every fresh
//   worktree of this repo, and the primary checkout on the host that found this —
//   the command still runs and prints an authoritative-looking summary:
//
//     no install   : 254 tests / 253 pass / 1 fail   (src/lib/formPublicURL.test.ts)
//     real install : 260 tests / 260 pass / 0 fail   (same head, same code)
//
//   Six tests silently never register: the files that `import "react"` die at
//   module load, so the tests after the import line are absent from the count.
//   The "1 fail" is not a defect in the branch either — it is `ERR_MODULE_NOT_FOUND`
//   for `react`. A false RED and a silent under-report in the same run, and the
//   number it prints is the one people quote as evidence.
//
//   scripts/preflight.sh requires `web/node_modules/.bin/next` to be executable,
//   which catches "no install at all" — but not a `node_modules` DIRECTORY that
//   exists and is empty (the state the primary checkout was found in), nor a
//   partial install left by an interrupted `npm ci`. Presence of the tool is not
//   presence of the suite's dependencies.
//
// THE FIX
//   `web/scripts/web-deps-check.mjs` asserts that every package named in
//   `web/package.json` has `<web>/node_modules/<name>/package.json`, and
//   `test:unit` runs it BEFORE `node --test`. The preflight's `web`/`a11y`
//   prerequisites run the same checker, so the gate refuses at the same place.
//
// WHAT THIS FILE PINS
//   1. The checker is looking at a real tree (its declared list comes from the
//      real web/package.json, and includes react/react-dom — the package whose
//      absence produced the under-count).
//   2. The three broken shapes it must refuse: no `node_modules` at all, an
//      EMPTY `node_modules` directory, and a PARTIAL install — and the message
//      must name the missing packages and the exact seed command, or the reader
//      cannot act on it.
//   3. The healthy shape it must NOT refuse: a complete install exits 0 (a
//      prerequisite check that fails a good tree is its own defect).
//   4. THE REGRESSION THIS FILE EXISTS FOR: `web/package.json`'s `test:unit`
//      runs the dependency check before `node --test`, so a bare `node --test`
//      cannot come back and start under-reporting again.
//
// Hermetic: every fixture is built under os.tmpdir() and removed; the checker is
// invoked as a subprocess with the fixture as its argument, so nothing here reads
// or writes the real tree except the two assertions that are ABOUT the real tree.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const LIB_DIR = dirname(fileURLToPath(import.meta.url));
const WEB_DIR = resolve(LIB_DIR, "..", ".."); // <repo>/web
const CHECKER = join(WEB_DIR, "scripts", "web-deps-check.mjs");
const PKG = JSON.parse(readFileSync(join(WEB_DIR, "package.json"), "utf8")) as {
  scripts: Record<string, string>;
  dependencies?: Record<string, string>;
  devDependencies?: Record<string, string>;
};

/** Build a throwaway web tree: a package.json declaring `deps`, and an install
 *  containing exactly `installed` (null = no node_modules directory at all). */
function fixture(deps: string[], installed: string[] | null): string {
  const dir = mkdtempSync(join(tmpdir(), "jf-webdeps-"));
  writeFileSync(
    join(dir, "package.json"),
    JSON.stringify({ name: "fixture", private: true, dependencies: Object.fromEntries(deps.map((d) => [d, "1.0.0"])) }),
  );
  if (installed !== null) {
    mkdirSync(join(dir, "node_modules"), { recursive: true });
    for (const name of installed) {
      mkdirSync(join(dir, "node_modules", name), { recursive: true });
      writeFileSync(join(dir, "node_modules", name, "package.json"), JSON.stringify({ name, version: "1.0.0" }));
    }
  }
  return dir;
}

function runChecker(webDir: string) {
  const r = spawnSync(process.execPath, [CHECKER, webDir], { encoding: "utf8" });
  return { status: r.status, out: `${r.stdout}${r.stderr}` };
}

/** The `missing:` line's payload — the list the reader has to act on. */
function missingLine(out: string): string {
  const m = out.match(/^\s*missing:\s+(.*)$/m);
  return m ? m[1].trim() : "";
}

test("the checker reads the real web/package.json (it cannot pass vacuously)", () => {
  assert.ok(existsSync(CHECKER), `web/scripts/web-deps-check.mjs is missing — test:unit cannot refuse anything`);
  const declared = [...Object.keys(PKG.dependencies ?? {}), ...Object.keys(PKG.devDependencies ?? {})];
  // react/react-dom are the packages whose absence caused the under-count, so the
  // checker must be deriving its list from this file rather than a hardcoded one.
  for (const name of ["react", "react-dom"]) {
    assert.ok(declared.includes(name), `web/package.json no longer declares ${name} — re-anchor this guard`);
  }
  const src = readFileSync(CHECKER, "utf8");
  assert.match(src, /dependencies/, "the checker no longer reads the declared dependencies");
  assert.match(src, /devDependencies/, "the checker no longer reads the declared devDependencies");
});

test("no node_modules at all -> refused, naming the packages and the seed command", () => {
  const dir = fixture(["react", "react-dom"], null);
  try {
    const { status, out } = runChecker(dir);
    assert.notEqual(status, 0, `a tree with no node_modules must be refused, got exit 0:\n${out}`);
    assert.match(out, /IS NOT INSTALLED IN THIS TREE/, "the refusal must be unmissable");
    assert.equal(missingLine(out), "react, react-dom", "the refusal must name every missing package");
    assert.match(out, /npm ci/, "the refusal must give the exact command that fixes it");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("an EMPTY node_modules directory -> refused (the shape the primary checkout was in)", () => {
  // This is the state that produced 253/1: the directory exists, so a "does
  // node_modules exist" check passes; it holds nothing, so the suite cannot run.
  const dir = fixture(["react", "react-dom"], []);
  try {
    const { status, out } = runChecker(dir);
    assert.notEqual(status, 0, `an empty node_modules must be refused, got exit 0:\n${out}`);
    assert.equal(missingLine(out), "react, react-dom");
    assert.match(out, /directory present, 0 entries/, "the refusal must say what it actually saw");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("a PARTIAL install -> refused, and only the packages that are missing are named", () => {
  // An interrupted `npm ci` leaves exactly this: `.bin/next` can already exist,
  // so the preflight's executable check passes while the suite still cannot run.
  const dir = fixture(["react", "react-dom"], ["react"]);
  try {
    const { status, out } = runChecker(dir);
    assert.notEqual(status, 0, `a partial install must be refused, got exit 0:\n${out}`);
    assert.equal(
      missingLine(out),
      "react-dom",
      "the refusal must name the missing package and NOT the installed one — a wrong list sends the reader after the wrong problem",
    );
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("a COMPLETE install -> exit 0 (a prerequisite check must not fail a good tree)", () => {
  const dir = fixture(["react", "react-dom"], ["react", "react-dom"]);
  try {
    const { status, out } = runChecker(dir);
    assert.equal(status, 0, `a complete install must pass, got exit ${status}:\n${out}`);
    assert.match(out, /toolchain present/, "the success path must say so");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("the real tree passes its own check (this is the tree the suite runs in)", () => {
  // Reached only when `node --test` is running at all, which `test:unit` only
  // does AFTER the checker passed — so this is the healthy-tree control.
  const { status, out } = runChecker(WEB_DIR);
  assert.equal(status, 0, `the real web tree failed its own dependency check:\n${out}`);
});

test("test:unit runs the dependency check BEFORE node --test (the regression this file exists for)", () => {
  const script = PKG.scripts["test:unit"];
  assert.ok(script, "web/package.json no longer defines test:unit");
  const checkAt = script.indexOf("web-deps-check.mjs");
  const testAt = script.indexOf("node --test");
  assert.notEqual(checkAt, -1, `test:unit no longer runs the dependency check: ${script}`);
  assert.notEqual(testAt, -1, `test:unit no longer runs node --test: ${script}`);
  assert.ok(
    checkAt < testAt,
    `test:unit must run the dependency check FIRST — a bare 'node --test' in an uninstalled tree ` +
      `under-reports instead of failing: ${script}`,
  );
  // `build` keeps going through test:unit, so the gate inherits the refusal.
  assert.match(
    PKG.scripts["build"] ?? "",
    /npm run test:unit/,
    "npm run build no longer runs test:unit, so the web gate lost the dependency check",
  );
});
