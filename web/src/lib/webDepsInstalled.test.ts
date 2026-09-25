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
// THE DEEPER DEPTH (the follow-up this file now also pins)
//   "Package directory present, no payload" is the same failure one interruption
//   deeper: an `npm ci` killed after the directories exist but before the
//   tarballs unpack, or a pruned/cache-restored `node_modules`, leaves every
//   declared directory holding a bare `package.json`. A presence-only check said
//   "14 declared package(s) installed" on exactly that tree, and `test:unit` then
//   printed `326 tests / 324 pass / 2 fail` against `350 / 350 / 0` installed —
//   the failures again being `ERR_MODULE_NOT_FOUND` for `react`. So the checker
//   also asserts PAYLOAD: for a package that declares a runtime entry point, the
//   file its own manifest names (`exports["."]` when present, else `main`, else
//   `module`) must be on disk.
//
// THE FIX
//   `web/scripts/web-deps-check.mjs` asserts that every package named in
//   `web/package.json` has `<web>/node_modules/<name>/package.json`, that each
//   non-`@types/*` package has the file its own entry declaration names, and
//   `test:unit` runs it BEFORE `node --test`. The preflight's `web`/`a11y`
//   prerequisites run the same checker, so the gate refuses at the same place.
//
// WHAT THIS FILE PINS
//   1. The checker is looking at a real tree (its declared list comes from the
//      real web/package.json, and includes react/react-dom — the package whose
//      absence produced the under-count).
//   2. The broken shapes it must refuse: no `node_modules` at all, an EMPTY
//      `node_modules` directory, a PARTIAL install, and a HOLLOW install (every
//      directory present, no payload) — and the message must name the offending
//      packages and the exact seed command, or the reader cannot act on it.
//   3. The payload rule is about the file the package's OWN manifest names, not
//      "the directory has something in it", and it follows `exports["."]` rather
//      than a stale `main`.
//   4. The false-RED guard: `@types/*` packages have no runtime entry point and a
//      package that declares none cannot be judged on payload — neither may be
//      refused, because a prerequisite check that fails a good tree is its own
//      defect.
//   5. The healthy shape it must NOT refuse: a complete install exits 0.
//   6. THE REGRESSION THIS FILE EXISTS FOR: `web/package.json`'s `test:unit`
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

/** One package in a fixture's `node_modules`. `manifest` is merged into the
 *  written package.json; `files` are written relative to the package directory. */
interface PkgSpec {
  name: string;
  manifest?: Record<string, unknown>;
  files?: string[];
}

/** An installed package that satisfies the payload rule: it declares `main` and
 *  the file is there. */
function installed(name: string): PkgSpec {
  return { name, manifest: { main: "index.js" }, files: ["index.js"] };
}

/** Build a throwaway web tree: a package.json declaring `deps`, and an install
 *  containing exactly `pkgs` (null = no node_modules directory at all). */
function fixture(deps: string[], pkgs: PkgSpec[] | null): string {
  const dir = mkdtempSync(join(tmpdir(), "jf-webdeps-"));
  writeFileSync(
    join(dir, "package.json"),
    JSON.stringify({ name: "fixture", private: true, dependencies: Object.fromEntries(deps.map((d) => [d, "1.0.0"])) }),
  );
  if (pkgs !== null) {
    mkdirSync(join(dir, "node_modules"), { recursive: true });
    for (const spec of pkgs) {
      const pkgDir = join(dir, "node_modules", spec.name);
      mkdirSync(pkgDir, { recursive: true });
      writeFileSync(join(pkgDir, "package.json"), JSON.stringify({ name: spec.name, version: "1.0.0", ...spec.manifest }));
      for (const file of spec.files ?? []) {
        const target = join(pkgDir, file);
        mkdirSync(dirname(target), { recursive: true });
        writeFileSync(target, `// ${spec.name}\n`);
      }
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

/** The package names on the `no payload:` line (each entry is
 *  `name (field names path — not on disk)`), in the order they are printed. */
function noPayloadNames(out: string): string[] {
  const m = out.match(/^\s*no payload:\s+(.*)$/m);
  if (!m) return [];
  return m[1]
    .split(";")
    .map((s) => s.trim().split(" ")[0])
    .filter((s) => s.length > 0);
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
  const dir = fixture(["react", "react-dom"], [installed("react")]);
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

test("a HOLLOW install (every package directory present, no payload) -> refused", () => {
  // The shape the follow-up card measured: `npm ci` interrupted after the
  // directories are created. Every `<name>/package.json` exists, so the
  // presence-only check exited 0 and said "14 declared package(s) installed",
  // and `npm run test:unit` then printed 326/324/2 against 350/350/0 — a
  // plausible-looking count with an install-caused failure in it.
  const hollow = (name: string): PkgSpec => ({ name, manifest: { main: "index.js" }, files: [] });
  const dir = fixture(["react", "react-dom"], [hollow("react"), hollow("react-dom")]);
  try {
    const { status, out } = runChecker(dir);
    assert.notEqual(status, 0, `a hollow install must be refused, got exit 0:\n${out}`);
    assert.match(out, /IS NOT INSTALLED IN THIS TREE/, "the refusal must be unmissable");
    assert.deepEqual(
      noPayloadNames(out),
      ["react", "react-dom"],
      "the refusal must name the packages whose payload is absent — that is the list the reader acts on",
    );
    assert.equal(
      missingLine(out),
      "",
      "a hollow package is NOT a missing one — reporting it as missing sends the reader to the wrong problem",
    );
    assert.match(out, /main names index\.js/, "the refusal must say which declaration it followed");
    assert.match(out, /npm ci/, "the refusal must give the exact command that fixes it");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("payload means the file the manifest NAMES, not 'the directory has something in it'", () => {
  // A pruned install can leave the package's other files behind (chunks, a
  // subdirectory) and still be unusable, so the assertion has to be on the entry
  // point itself. Without this the check degrades into "directory non-empty".
  const dir = fixture(["react"], [
    { name: "react", manifest: { main: "index.js" }, files: ["cjs/react.development.js"] },
  ]);
  try {
    const { status, out } = runChecker(dir);
    assert.notEqual(status, 0, `a package missing its declared entry point must be refused, got exit 0:\n${out}`);
    assert.deepEqual(noPayloadNames(out), ["react"]);
    assert.match(out, /main names index\.js/, "the refusal must name the declaration and the path it pointed at");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("exports[\".\"] is what is checked, and a stale main must not fail a good tree", () => {
  // Node ignores `main` entirely when `exports` is present, and real packages here
  // do exactly that (react: exports["."].default = ./index.js, main = index.js).
  // Judging such a package by `main` would be a false RED in a prerequisite.
  const good = fixture(["react"], [
    { name: "react", manifest: { main: "index.js", exports: { ".": { default: "./index.mjs" } } }, files: ["index.mjs"] },
  ]);
  const bad = fixture(["react"], [
    { name: "react", manifest: { main: "index.js", exports: { ".": { default: "./index.mjs" } } }, files: ["index.js"] },
  ]);
  try {
    const goodRun = runChecker(good);
    assert.equal(goodRun.status, 0, `a tree whose exports["."] target exists must pass, got:\n${goodRun.out}`);
    const badRun = runChecker(bad);
    assert.notEqual(badRun.status, 0, `a tree whose exports["."] target is absent must be refused:\n${badRun.out}`);
    assert.deepEqual(noPayloadNames(badRun.out), ["react"]);
    assert.match(badRun.out, /exports\["\."\] names \.\/index\.mjs/, "the refusal must name the export that was followed");
  } finally {
    rmSync(good, { recursive: true, force: true });
    rmSync(bad, { recursive: true, force: true });
  }
});

test("@types/* packages are presence-only -> a hollow @types package is NOT refused", () => {
  // `@types/*` ships declarations, not a runtime entry point (some declare no main
  // at all), so a payload rule cannot be applied to them. Their contents are
  // stripped by the same prunes that make a hollow tree, so this is not
  // hypothetical — and a prerequisite that fails a good tree is its own defect.
  const dir = fixture(["react", "@types/react"], [
    installed("react"),
    { name: "@types/react", manifest: { main: "./index.d.ts" }, files: [] },
  ]);
  try {
    const { status, out } = runChecker(dir);
    assert.equal(status, 0, `a hollow @types package must not fail the check, got exit ${status}:\n${out}`);
    assert.deepEqual(noPayloadNames(out), []);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("a package that declares no entry point cannot be judged on payload -> NOT refused", () => {
  // Nothing to look for means presence is the whole assertion the metadata allows;
  // inventing a path here is how a prerequisite starts refusing good trees.
  const dir = fixture(["react", "noentry"], [installed("react"), { name: "noentry", manifest: {} }]);
  try {
    const { status, out } = runChecker(dir);
    assert.equal(status, 0, `a package with no declared entry point must not fail the check, got exit ${status}:\n${out}`);
    assert.deepEqual(noPayloadNames(out), []);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("a COMPLETE install -> exit 0 (a prerequisite check must not fail a good tree)", () => {
  // Note this fixture install is NOT hollow: each package carries the payload its
  // manifest declares. Before the payload check existed this test passed on a
  // fixture that was itself the hollow shape, which is why it could not catch it.
  const dir = fixture(["react", "react-dom"], [installed("react"), installed("react-dom")]);
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
