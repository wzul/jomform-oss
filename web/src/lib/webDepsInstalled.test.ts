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
//   also asserts PAYLOAD: the entry point a package's own manifest declares must
//   be reachable from its directory.
//
// AND THE FALSE RED THAT RULE ITSELF CAUSED (this file's newest section)
//   The first payload rule tested the declared entry point as a VERBATIM on-disk
//   path. That disagrees with Node for manifest shapes Node happily loads, so it
//   refused healthy trees:
//
//     * `main: "decimal"` with `decimal.js` present — Node's LOAD_AS_FILE tries
//       `X`, `X.js`, `X.json`, `X.node`. Live in this repo's install graph:
//       `decimal.js-light` (transitive via `recharts`) declares exactly that.
//     * `exports: { ".": ["not:valid", "./present.js"] }` — Node falls through an
//       element that is not a valid export target. The old `firstLeaf()` returned
//       the FIRST string, i.e. the invalid one.
//     * `exports: { ".": { import: "./b.js", default: "./a.js" } }` with only
//       `b.js` on disk — Node reads condition keys IN ORDER; the old hardcoded
//       `["default","import","require",…]` list made `default` outrank `import`.
//     * `exports: { ".": { types: "./index.d.ts" } }` — the header said a types
//       target must not be demanded, but the fallback loop took it anyway.
//
//   The rule now resolves like Node (extension search, directory index, array
//   fallbacks, manifest key order, per-consumer entry points) and narrows to
//   presence-only for the shapes where no runtime consumer resolves a target.
//   Those false-RED shapes are pinned below AS FIXTURES, each asserted to be
//   genuinely loadable through Node's own resolver inside the fixture —
//   hand-written expectations cannot catch a resolver disagreement, which is
//   exactly what this was. One of them (`browser` listed before `require`) was
//   found the same way, while fixing this card.
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
//   4. The false-RED guards: `@types/*` packages, a package that declares no
//      entry point, a subpath-only `exports` map, a types-only condition map, an
//      import-only package, and the resolver-shaped manifests above may not be
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
 *  containing exactly `pkgs` (null = no node_modules directory at all).
 *
 *  It also writes a `probe.mjs` next to the fixture's package.json, so a test can
 *  ask NODE's own resolvers whether a package is loadable from that tree — the
 *  resolver is the oracle for every false-RED case below. */
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
  writeFileSync(
    join(dir, "probe.mjs"),
    `import { createRequire } from "node:module";
const spec = process.argv[2];
const out = { require: null, import: null };
try { out.require = createRequire(import.meta.url).resolve(spec); } catch (e) { out.require = "ERR " + e.code; }
// A REAL import(), not import.meta.resolve(): the latter resolves the SPECIFIER
// without checking the file exists (measured — it returns a path for a missing
// target while the load throws ERR_MODULE_NOT_FOUND), so it cannot be the oracle
// for "Node can load this".
try { await import(spec); out.import = "OK"; } catch (e) { out.import = "ERR " + (e.code ?? e.message.split("\\n")[0]); }
console.log(JSON.stringify(out));
`,
  );
  return dir;
}

function runChecker(webDir: string) {
  const r = spawnSync(process.execPath, [CHECKER, webDir], { encoding: "utf8" });
  return { status: r.status, out: `${r.stdout}${r.stderr}` };
}

/** Ask NODE whether `spec` is loadable from inside `dir` — the oracle. Returns
 *  `{"require":…,"import":…}` where a value of `"OK"` means the module actually
 *  loaded. `require` is a resolution (a `require()` of a real package would
 *  execute it), `import` is a real dynamic import, which is what makes this a
 *  reliable oracle: `import.meta.resolve()` alone does NOT check existence. */
function resolvesUnderNode(dir: string, spec: string): { require: string; import: string } {
  const r = spawnSync(process.execPath, [join(dir, "probe.mjs"), spec], { encoding: "utf8" });
  const raw = (r.stdout || "").trim();
  assert.ok(raw.length > 0, `the resolver probe produced nothing for ${spec}: ${r.stderr}`);
  return JSON.parse(raw) as { require: string; import: string };
}

/** Assert Node loads the package by BOTH routes (CJS `require` and ESM `import`),
 *  which is the shape a fixture in these tests must have to be a valid oracle. */
function assertNodeLoads(dir: string, spec: string, why: string) {
  const r = resolvesUnderNode(dir, spec);
  assert.match(r.require, /^\//, `${why}: Node's require() cannot resolve ${spec} (${r.require}) — the fixture is not a valid false-RED case`);
  assert.equal(r.import, "OK", `${why}: Node's import cannot load ${spec} (${r.import}) — the fixture is not a valid false-RED case`);
}

/** Assert Node loads the package by AT LEAST ONE route. Needed for a condition map
 *  that is asymmetric on purpose: `{ import: "./b.js", default: "./a.js" }` with
 *  only `b.js` on disk is loadable via ESM and NOT via `require` (CJS takes
 *  `default` → the absent `./a.js`, and a VALID-but-missing target does not fall
 *  back). Demanding both routes would reject a real false-RED case. */
function assertNodeLoadsAnyRoute(dir: string, spec: string, why: string) {
  const r = resolvesUnderNode(dir, spec);
  assert.ok(
    r.require.startsWith("/") || r.import === "OK",
    `${why}: Node cannot load ${spec} by either route (${JSON.stringify(r)}) — the fixture is not a valid false-RED case`,
  );
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

/** How many declared packages the checker actually judged on payload (as opposed
 *  to leaving presence-only) — so a "the rule silently stopped checking" change
 *  cannot pass as a green suite. The success line is the only place this is
 *  printed, so a REFUSED run has no count: it returns -1, and a test that asserts
 *  a count on a refusal is pinning the wrong thing. */
function payloadChecked(out: string): number {
  const m = out.match(/(\d+) entry point\(s\) checked/);
  return m ? Number(m[1]) : -1;
}

function presenceOnly(out: string): number {
  const m = out.match(/, (\d+) presence-only/);
  return m ? Number(m[1]) : -1;
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
    // The sibling file means the DIRECTORY is not empty — so this refusal can
    // only be the payload rule, not "the directory is missing".
    assert.equal(missingLine(out), "", "the package directory exists; it is the payload that is absent");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("main naming a DIRECTORY with no index -> refused (an empty lib/ still passes nothing)", () => {
  // A surviving directory used to satisfy the payload assertion outright, so a
  // package whose entry point is `./lib` was never checked one layer deeper.
  // Node's LOAD_INDEX needs lib/index.js|.json|.node; an empty (or index-less)
  // lib/ is MODULE_NOT_FOUND. Verified against Node in the fixture below.
  const empty = fixture(["react"], [{ name: "react", manifest: { main: "./lib" }, files: [] }]);
  const indexed = fixture(["react"], [{ name: "react", manifest: { main: "./lib" }, files: ["lib/index.js"] }]);
  const stray = fixture(["react"], [{ name: "react", manifest: { main: "./lib" }, files: ["lib/other.js"] }]);
  try {
    const emptyRun = runChecker(empty);
    assert.notEqual(emptyRun.status, 0, `main: "./lib" with an EMPTY lib/ must be refused:\n${emptyRun.out}`);
    assert.deepEqual(noPayloadNames(emptyRun.out), ["react"]);
    assert.equal(missingLine(emptyRun.out), "", "the package directory exists; the target inside it is what is absent");
    const strayRun = runChecker(stray);
    assert.notEqual(strayRun.status, 0, `main: "./lib" with only lib/other.js must be refused:\n${strayRun.out}`);
    const okRun = runChecker(indexed);
    assert.equal(okRun.status, 0, `main: "./lib" with lib/index.js present must pass, got:\n${okRun.out}`);
    // The oracle: Node itself agrees with all three verdicts.
    assert.equal(resolvesUnderNode(empty, "react").require, "ERR MODULE_NOT_FOUND");
    assert.equal(resolvesUnderNode(stray, "react").require, "ERR MODULE_NOT_FOUND");
    assertNodeLoads(indexed, "react", "a directory main with an index");
  } finally {
    for (const dir of [empty, indexed, stray]) rmSync(dir, { recursive: true, force: true });
  }
});

test('exports["."] is what is checked, and a stale main must not fail a good tree', () => {
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
    assertNodeLoads(good, "react", 'exports["."].default');
    const badRun = runChecker(bad);
    assert.notEqual(badRun.status, 0, `a tree whose exports["."] target is absent must be refused:\n${badRun.out}`);
    assert.deepEqual(noPayloadNames(badRun.out), ["react"]);
    assert.match(badRun.out, /exports\["\."\]( \[[a-z]+\])? names \.\/index\.mjs/, "the refusal must name the export that was followed");
    assert.equal(
      missingLine(badRun.out),
      "",
      "main is ignored when exports is present, so the missing index.js must NOT be reported",
    );
  } finally {
    rmSync(good, { recursive: true, force: true });
    rmSync(bad, { recursive: true, force: true });
  }
});

test("the false-RED set: every manifest shape below is LOADABLE by Node and must not be refused", () => {
  // This is the regression the follow-up card filed. Each case is asserted
  // LOADABLE through Node's own resolver inside its fixture FIRST, so a case that
  // stops being a valid oracle fails here loudly instead of passing vacuously.
  const cases: Array<{ label: string; deps: string[]; pkgs: PkgSpec[]; expectJudged: number; expectPresenceOnly: number }> = [
    {
      // A. LOAD_AS_FILE: `main: "decimal"` + decimal.js present. Live in this
      // repo's install graph as decimal.js-light (transitive via recharts).
      label: "extension-less main",
      deps: ["decimal"],
      pkgs: [{ name: "decimal", manifest: { main: "decimal" }, files: ["decimal.js"] }],
      expectJudged: 1,
      expectPresenceOnly: 0,
    },
    {
      // B. Array fallback: the FIRST element is invalid as an export target, so
      // Node uses the second. The old firstLeaf() returned the invalid one.
      label: "exports array with an invalid first element",
      deps: ["fallback"],
      pkgs: [{ name: "fallback", manifest: { exports: { ".": ["not:valid", "./present.js"] } }, files: ["present.js"] }],
      expectJudged: 1,
      expectPresenceOnly: 0,
    },
    {
      // C. `require` and `import` enable different conditions, so a manifest can
      // legitimately be loadable for one and not the other. Only b.js is present:
      // an ESM `import` gets it (key order puts `import` first), a CJS `require`
      // falls through to the absent `default`. Node confirms exactly that below.
      label: "condition order (import before default)",
      deps: ["ordered"],
      pkgs: [{ name: "ordered", manifest: { exports: { ".": { import: "./b.js", default: "./a.js" } } }, files: ["b.js"] }],
      expectJudged: 1,
      expectPresenceOnly: 0,
    },
    {
      // D. A types-only map names a declaration file, not a payload. Node refuses
      // it for both routes (ERR_PACKAGE_PATH_NOT_EXPORTED), so it cannot be
      // refused for a missing .d.ts — the header always said so.
      label: "types-only condition map",
      deps: ["typed"],
      pkgs: [{ name: "typed", manifest: { exports: { ".": { types: "./index.d.ts" } } }, files: [] }],
      expectJudged: 0,
      expectPresenceOnly: 1,
    },
    {
      // E. Subpaths only: the bare name is not importable in this shape at all.
      label: "subpath-only exports map",
      deps: ["subs"],
      pkgs: [{ name: "subs", manifest: { exports: { "./sub": "./sub.js" } }, files: ["sub.js"] }],
      expectJudged: 0,
      expectPresenceOnly: 1,
    },
    {
      // F. `browser` is a BUNDLER condition, not one Node enables by default. Here
      // it names a file that is absent while `require`/`import` name present ones,
      // so Node loads the package and only a rule that PREFERRED `browser` would
      // refuse it. `@jridgewell/resolve-uri` and `nanoid` (both installed in this
      // tree) declare `browser` before `require` — this is not hypothetical.
      label: "browser condition listed before require, browser file absent",
      deps: ["browserish"],
      pkgs: [
        {
          name: "browserish",
          manifest: { exports: { ".": { browser: "./browser.js", require: "./cjs/index.js", import: "./esm/index.mjs" } } },
          files: ["cjs/index.js", "esm/index.mjs"],
        },
      ],
      expectJudged: 1,
      expectPresenceOnly: 0,
    },
  ];

  const built: string[] = [];
  try {
    for (const c of cases) {
      const dir = fixture(c.deps, c.pkgs);
      built.push(dir);
      // Oracle first: for the shapes where Node CAN load the package, prove it.
      // `typed`/`subs` are loadable by NEITHER route by construction, so their
      // oracle is "Node agrees there is no runtime target", asserted separately.
      if (c.label === "types-only condition map" || c.label === "subpath-only exports map") {
        const oracle = resolvesUnderNode(dir, c.deps[0]);
        assert.match(
          `${oracle.require} ${oracle.import}`,
          /ERR_PACKAGE_PATH_NOT_EXPORTED/,
          `${c.label}: expected Node to expose no runtime "." target, got ${JSON.stringify(oracle)}`,
        );
      } else if (c.label === "condition order (import before default)") {
        // Asymmetric on purpose: ESM gets ./b.js, CJS takes `default` → ./a.js.
        assertNodeLoadsAnyRoute(dir, c.deps[0], c.label);
      } else {
        assertNodeLoads(dir, c.deps[0], c.label);
      }
      const { status, out } = runChecker(dir);
      assert.equal(status, 0, `${c.label}: a loadable tree must NOT be refused, got exit ${status}:\n${out}`);
      assert.equal(payloadChecked(out), c.expectJudged, `${c.label}: payload-checked count`);
      assert.equal(presenceOnly(out), c.expectPresenceOnly, `${c.label}: presence-only count`);
    }
  } finally {
    for (const dir of built) rmSync(dir, { recursive: true, force: true });
  }
});

test("an import-only package (its `require` half names an absent file) is NOT refused", () => {
  // A VALID-but-missing target does not fall back, so `require` really does get
  // ./a.js — but `import` gets ./b.js and loads. Node therefore loads the package,
  // and refusing it would be the same false RED this rule was fixed for. The
  // oracle below asserts BOTH halves: ESM OK, CJS broken.
  const dir = fixture(["ordered"], [
    { name: "ordered", manifest: { exports: { ".": { import: "./b.js", default: "./a.js" } } }, files: ["b.js"] },
  ]);
  try {
    const oracle = resolvesUnderNode(dir, "ordered");
    assert.equal(oracle.import, "OK", `the fixture must be ESM-loadable (${oracle.import})`);
    assert.match(oracle.require, /^ERR/, `and NOT CJS-loadable (${oracle.require}) — that asymmetry is the point`);
    const { status, out } = runChecker(dir);
    assert.equal(status, 0, `a package at least one consumer can load must NOT be refused:\n${out}`);
    assert.equal(payloadChecked(out), 1, "it must still be judged, not skipped");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("a VALID export target whose file is missing does NOT fall back -> still refused", () => {
  // The other half of the array-fallback rule: Node only falls through INVALID
  // elements. A valid element pointing at an absent file is MODULE_NOT_FOUND, so
  // falling back here (the way the old firstLeaf() did not, but the new rule
  // must not either) would turn a genuine hole green.
  const dir = fixture(["fallback"], [
    { name: "fallback", manifest: { exports: { ".": ["./missing.js", "./present.js"] } }, files: ["present.js"] },
  ]);
  try {
    assert.equal(
      resolvesUnderNode(dir, "fallback").require,
      "ERR MODULE_NOT_FOUND",
      "Node must NOT fall back past a valid-but-absent element — otherwise this test is not pinning what it claims",
    );
    const { status, out } = runChecker(dir);
    assert.notEqual(status, 0, `a valid target whose file is absent must be refused:\n${out}`);
    assert.deepEqual(noPayloadNames(out), ["fallback"]);
    assert.match(out, /names \.\/missing\.js/, "the refusal must name the element that was resolved");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test('a package whose ONLY "." target is invalid -> presence-only, not a crash or a refusal', () => {
  // Node's answer for this manifest is ERR_INVALID_PACKAGE_TARGET for both routes
  // (the package is not importable by name), which is not a payload hole this
  // check can describe. It must not throw and must not invent a target.
  const dir = fixture(["broken"], [{ name: "broken", manifest: { exports: { ".": ["not:valid"] } } }]);
  try {
    const oracle = resolvesUnderNode(dir, "broken");
    assert.match(
      `${oracle.require} ${oracle.import}`,
      /ERR_INVALID_PACKAGE_TARGET/,
      `expected Node to report an invalid target, got ${JSON.stringify(oracle)}`,
    );
    const { status, out } = runChecker(dir);
    assert.equal(status, 0, `a package with no valid "." target must not be refused:\n${out}`);
    assert.deepEqual(noPayloadNames(out), []);
    assert.equal(presenceOnly(out), 1, "it must be reported as presence-only, not silently counted as payload-checked");
  } finally {
    rmSync(dir, { recursive: true, force: true });
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
    assert.equal(presenceOnly(out), 1, "the @types package must be counted presence-only");
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
    assert.equal(presenceOnly(out), 1);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("a bare condition map with a runtime target IS judged (the check did not go vacuous)", () => {
  // A condition map without subpath keys is the "." target itself. Pinned so the
  // subpath-only narrowing above cannot swallow these packages too.
  const good = fixture(["cond"], [{ name: "cond", manifest: { exports: { default: "./index.js" } }, files: ["index.js"] }]);
  const bad = fixture(["cond"], [{ name: "cond", manifest: { exports: { default: "./index.js" } }, files: [] }]);
  try {
    assertNodeLoads(good, "cond", "a bare condition map");
    const goodRun = runChecker(good);
    assert.equal(goodRun.status, 0, `a bare condition map whose target exists must pass:\n${goodRun.out}`);
    assert.equal(payloadChecked(goodRun.out), 1);
    const badRun = runChecker(bad);
    assert.notEqual(badRun.status, 0, `a bare condition map whose target is absent must be refused:\n${badRun.out}`);
    assert.deepEqual(noPayloadNames(badRun.out), ["cond"]);
  } finally {
    rmSync(good, { recursive: true, force: true });
    rmSync(bad, { recursive: true, force: true });
  }
});

test('exports: { ".": … } is judged, not mistaken for a condition map (real packages here are shaped that way)', () => {
  // react/react-dom/tailwindcss declare `exports: { ".": … }`. A subpath-key
  // detector that only looked for `./` would classify `"."` as a condition name,
  // silently make the single biggest group of packages presence-only, and leave
  // this guard green. So this test's real tree assertion below would not catch it —
  // this fixture does.
  const bad = fixture(["react"], [
    { name: "react", manifest: { main: "index.js", exports: { ".": { default: "./index.js" } } }, files: [] },
  ]);
  try {
    const { status, out } = runChecker(bad);
    assert.notEqual(status, 0, `exports["."] is a subpath key — a missing target must be refused:\n${out}`);
    assert.deepEqual(noPayloadNames(out), ["react"]);
    // The refusal path prints its own detail rather than the success line, so the
    // payload-half proof here is the `no payload:` line naming react — a
    // presence-only classification would have produced no such line at all.
    assert.match(out, /exports\["\."\]( \[[a-z]+\])? names \.\/index\.js/, "the refusal must name the export it followed");
  } finally {
    rmSync(bad, { recursive: true, force: true });
  }
});

test("a dual-consumer package is refused only when NO consumer can reach an entry, and the refusal names each half", () => {
  // `require` and `import` enable different conditions, so a package can name a
  // present file for one consumer and an absent file for the other — the shape
  // `{ ".": { browser, require, import } }` has, which @jridgewell/resolve-uri and
  // nanoid write. One half surviving is NOT an install hole (Node still loads the
  // package); neither half surviving is, and then the refusal must name BOTH with
  // the condition that reaches each, or the reader cannot tell which is gone.
  const bothPresent = fixture(["dual"], [
    {
      name: "dual",
      manifest: { exports: { ".": { require: "./cjs/index.js", import: "./esm/index.mjs" } } },
      files: ["cjs/index.js", "esm/index.mjs"],
    },
  ]);
  const cjsOnly = fixture(["dual"], [
    {
      name: "dual",
      manifest: { exports: { ".": { require: "./cjs/index.js", import: "./esm/index.mjs" } } },
      files: ["cjs/index.js"],
    },
  ]);
  const neither = fixture(["dual"], [
    { name: "dual", manifest: { exports: { ".": { require: "./cjs/index.js", import: "./esm/index.mjs" } } }, files: [] },
  ]);
  try {
    const okRun = runChecker(bothPresent);
    assert.equal(okRun.status, 0, `both entry points present must pass:\n${okRun.out}`);
    assert.equal(payloadChecked(okRun.out), 1);

    // Half-gone: Node's CJS route works, its ESM route does not. The checker must
    // NOT refuse it — that is the false RED this rule exists to avoid.
    const oracle = resolvesUnderNode(cjsOnly, "dual");
    assert.match(oracle.require, /^\//, `Node's require() must still resolve it (${oracle.require})`);
    assert.match(oracle.import, /^ERR/, `Node's import must NOT load it (${oracle.import})`);
    const halfRun = runChecker(cjsOnly);
    assert.equal(halfRun.status, 0, `a package one consumer can still load must NOT be refused:\n${halfRun.out}`);

    // Nothing left: both halves must be named, each with its condition.
    const deadOracle = resolvesUnderNode(neither, "dual");
    assert.match(`${deadOracle.require} ${deadOracle.import}`.replaceAll("OK", ""), /ERR/, "no route may load it");
    const deadRun = runChecker(neither);
    assert.notEqual(deadRun.status, 0, `a package no consumer can load must be refused:\n${deadRun.out}`);
    assert.deepEqual(noPayloadNames(deadRun.out), ["dual", "dual"], "both entry points must be named");
    assert.match(deadRun.out, /exports\["\."\] \[import\] names \.\/esm\/index\.mjs/, "the ESM half must be named");
    assert.match(deadRun.out, /exports\["\."\] \[require\] names \.\/cjs\/index\.js/, "the CJS half must be named");
  } finally {
    for (const dir of [bothPresent, cjsOnly, neither]) rmSync(dir, { recursive: true, force: true });
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
    assert.equal(payloadChecked(out), 2, "both packages must be judged on payload, not skipped");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("the real tree passes its own check (this is the tree the suite runs in)", () => {
  // Reached only when `node --test` is running at all, which `test:unit` only
  // does AFTER the checker passed — so this is the healthy-tree control.
  const { status, out } = runChecker(WEB_DIR);
  assert.equal(status, 0, `the real web tree failed its own dependency check:\n${out}`);
  // ...and it is genuinely checking payloads on it, not just presence.
  assert.ok(
    payloadChecked(out) >= 8,
    `the real tree must have a substantial number of payload-checked packages, got ${payloadChecked(out)}:\n${out}`,
  );
  assert.match(out, /presence-only/, "the success line must account for presence-only packages");
});

test("test:unit runs the dependency check BEFORE node --test (the regression this file exists for)", () => {
  const script = PKG.scripts["test:unit"];
  assert.ok(script, "web/package.json no longer defines test:unit");

  // THE CHECK IS ON THE EXECUTED PATH, NOT ON THE STRING (card t_70c0588b).
  //
  // This test used to assert `script.indexOf("web-deps-check.mjs") < script.indexOf("node --test")`.
  // That is satisfied by a `test:unit` that merely MENTIONS the checker: delete the bare
  // `node scripts/web-deps-check.mjs && ` prefix, leave the path reachable from a comment,
  // an `echo`, a later `||` branch, or the suite's own reference to CHECKER, and the old
  // assertion still passed. Measured: the mutation below kept the old test GREEN.
  //
  // The chain is `a && b && c`, so split it and require the checker to be its own
  // EXECUTABLE pre-`&&` step: a step that starts with `node`, names the checker, and
  // carries no `||` fallback.
  const steps = script.split("&&").map((step) => step.trim());
  const isExecutedStep = (step: string, file: string): boolean =>
    step.startsWith("node ") && step.includes(file) && !step.includes("||");
  const checkStep = steps.findIndex((step) => isExecutedStep(step, "web-deps-check.mjs"));
  const testStep = steps.findIndex((step) => step.startsWith("node --test"));

  assert.notEqual(
    checkStep,
    -1,
    `test:unit no longer EXECUTES the dependency check as its own && step — a mention is not a ` +
      `run (a comment, an echo, a || fallback, or a step that reordered the flags all satisfy a ` +
      `substring test): ${script}`,
  );
  assert.notEqual(testStep, -1, `test:unit no longer runs node --test: ${script}`);
  assert.ok(
    checkStep < testStep,
    `test:unit must run the dependency check FIRST — a bare 'node --test' in an uninstalled tree ` +
      `under-reports instead of failing: ${script}`,
  );

  // `build` keeps going through test:unit, so the gate inherits the refusal. Same rule
  // applies to the hop: it must be an executed step, not a mention.
  const buildSteps = (PKG.scripts["build"] ?? "").split("&&").map((step) => step.trim());
  assert.ok(
    buildSteps.some((step) => step === "npm run test:unit"),
    "npm run build no longer runs test:unit as an executed step, so the web gate lost the " +
      "dependency check",
  );
});
