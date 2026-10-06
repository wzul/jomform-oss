// Unit tests for the "dead utility from a non-shipped file" decision (card
// t_850589b5). The build-time driver `scripts/cssScanScope.mjs` runs this logic
// over the real tree after `next build`; here it is proven to BITE on fixtures,
// which no source-only guard (e.g. tailwindScanScope.test.ts) can do.
//
// Run with:
//   npm run test:unit
import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import {
  classLiterals,
  cssSelectorLiterals,
  occursAsClass,
  deadUtilities,
} from "./cssScanScope.ts";
// The build-time driver, imported for its `sourceCorpus` and `webRoot`. The
// corpus it assembles was the uncovered half of this guard (cards t_bc75e313,
// t_682ebba5); see the tests at the bottom. `allowJs: true` in web/tsconfig.json
// types the import.
import { sourceCorpus, webRoot } from "../../scripts/cssScanScope.mjs";

test("classLiterals de-escapes an emitted utility back to the literal Tailwind matched", () => {
  // The escaped form is what the bundle literally contains; the unescaped form
  // is what a source file (or a test's prose) would have to contain to cause it.
  const lits = classLiterals(".hover\\:bg-\\[\\#828fff\\]:hover{background-color:#828fff}");
  assert.ok(
    lits.has("hover:bg-[#828fff]"),
    `expected the de-escaped literal, got: ${[...lits].join(", ")}`,
  );
});

test("classLiterals sees a plain utility and the m-1 the fixture id compiles to", () => {
  const lits = classLiterals(".bg-red-700{background-color:var(--color-red-700)}.m-1{margin:var(--spacing)}");
  assert.ok(lits.has("bg-red-700"));
  assert.ok(lits.has("m-1"));
});

test("occursAsClass matches a quoted fixture id but not a longer word containing it", () => {
  // `m-1` really is what Tailwind extracts from the JSON string `"m-1"` in
  // feedback-notify-marker.spec.ts — so the string must count …
  assert.equal(occursAsClass('{ id: "m-1", author_kind: "user" }', "m-1"), true);
  // … while a class that merely CONTAINS the literal must not be blamed.
  assert.equal(occursAsClass("form-1 submit-1", "m-1"), false);
  assert.equal(occursAsClass("mb-1 mt-1", "m-1"), false);
  assert.equal(occursAsClass("nothing here", "m-1"), false);
});

test("cssSelectorLiterals reads a stylesheet's own selector but ignores a comment mention", () => {
  const css = [
    "/* Rationale: the dead .bg-red-700 utility came from a spec's prose. */",
    ".jf-btn-primary:hover:not(:disabled) { background: var(--jf-primary-hover); }",
  ].join("\n");
  const lits = cssSelectorLiterals(css);
  assert.ok(
    lits.has("jf-btn-primary"),
    "the real selector must be read as a class the stylesheet defines",
  );
  assert.equal(
    lits.has("bg-red-700"),
    false,
    "a class named in a comment is not a selector the stylesheet defines",
  );
});

// --------------------------------------------------------------- the decision

test("a utility whose only source is an e2e spec is reported", () => {
  const css = ".bg-red-700{background-color:var(--color-red-700)}";
  const sheet = ".jf-btn-primary { }";
  const shipped = new Map([["src/app/globals.css", sheet]]);
  const nonShipped = new Map([
    ["e2e/a11y-chat-card.spec.ts", "// .bg-sky-600 … and bg-red-700 (#b91c1c) is 6.47:1."],
  ]);
  const violations = deadUtilities(css, shipped, nonShipped, cssSelectorLiterals(sheet));
  assert.deepEqual(
    violations.map((v) => v.literal),
    ["bg-red-700"],
    "the e2e-only utility must be reported",
  );
  assert.deepEqual(violations[0].sources, ["e2e/a11y-chat-card.spec.ts"]);
});

test("the same utility is NOT reported when real app code also names it", () => {
  // The exclusion cannot and must not change a class that real code uses.
  const css = ".bg-red-700{background-color:var(--color-red-700)}";
  const shipped = new Map([["src/app/foo.tsx", '<div className="bg-red-700" />']]);
  const nonShipped = new Map([["e2e/x.spec.ts", "bg-red-700 in prose"]]);
  assert.deepEqual(deadUtilities(css, shipped, nonShipped), []);
});

test("a class the stylesheet itself defines is not read as a dead utility", () => {
  // `.jf-btn-primary:hover:not(:disabled)` is emitted because globals.css DEFINES
  // it, not because ctaContrast.test.ts names it. Tailwind does not scan CSS, so
  // the definition lives in `selectorDefined` (globals.css's own selectors), not
  // in the shipped text corpus (real, non-CSS code). Without that set the class
  // would be a false positive; with it, it is cleared.
  const css = ".jf-btn-primary:hover:not(:disabled){background:var(--jf-primary-hover)}";
  const sheet = ".jf-btn-primary:hover:not(:disabled) { background: var(--jf-primary-hover); }";
  const nonShipped = new Map([["src/lib/ctaContrast.test.ts", "`.jf-btn-primary:hover:not(:disabled)`"]]);
  const selectors = cssSelectorLiterals(sheet);
  assert.deepEqual(deadUtilities(css, new Map(), nonShipped, selectors), []);
  // And the same input WITHOUT the selector set proves the guard has teeth.
  assert.deepEqual(
    deadUtilities(css, new Map(), nonShipped).map((v) => v.literal),
    ["jf-btn-primary"],
  );
});

test("a utility sourced from neither corpus is skipped, not reported", () => {
  // Tailwind's own defaults, arbitrary values and hand-written rules have no
  // source literal; they must not be blamed on a test.
  const css = "*,::before,::after{box-sizing:border-box}.text-\\[\\#123456\\]{color:#123456}";
  assert.deepEqual(deadUtilities(css, new Map(), new Map()), []);
});

// ------------------------------------------- the corpus the DRIVER assembles
//
// Cards t_850589b5/t_bc75e313/t_682ebba5. The tests above prove the DECISION
// bites, but they hand `deadUtilities` a fixture corpus, so they cannot see WHICH
// FILES the build-time driver actually feeds it. That is the gap these cards
// were, in two rounds:
//
//   * t_bc75e313: the driver's non-shipped corpus was tests + e2e only, so a
//     literal in `web/scripts/**` or in a web-root config shipped silently;
//   * t_682ebba5: the fix for that named four root configs BY HAND, so a literal
//     in `web/Dockerfile` or `web/tsconfig.json` — scanned by Tailwind, absent
//     from every corpus — still shipped silently (measured: bundle 61329 -> 61384
//     B, the literal in `.next/static/chunks/*.css`, driver exit 0).
//
// So the corpus is now derived from the TREE by Tailwind's own "which files are
// scanned" rule, and these tests pin that shape rather than a list of names: any
// file outside `web/src/**` that Tailwind reads is non-shipped, whatever it is
// called. `sourceCorpus()` is the real function `scripts/cssScanScope.mjs` runs,
// so this pins the wiring end to end.

test("the driver's non-shipped corpus is the whole tree outside src, not a list of names", () => {
  const { shipped, nonShipped } = sourceCorpus();
  const paths = [...nonShipped.keys()];

  // Each of these was a measured gap: a class literal in it reached the built CSS
  // while the guard printed `ok`. They are different SHAPES on purpose — no
  // extension, a root `.json`, a dotfile, a text file under `public/` — because
  // the previous fix failed by enumerating names instead of shapes.
  for (const rel of [
    "scripts/operator-bite.sh", // t_bc75e313: a build helper
    "next.config.mjs", // t_bc75e313: a root config
    "package.json",
    "postcss.config.mjs",
    "playwright.config.ts",
    "Dockerfile", // t_682ebba5: extensionless, was in NO corpus
    "tsconfig.json", // t_682ebba5: root json, was in NO corpus
    "public/llms.txt", // t_682ebba5: text shipped as a static asset
    "public/og-default.svg", // t_682ebba5: text XML (measured: scanned)
  ]) {
    if (!existsSync(join(webRoot, rel))) continue; // the tree may drop one; the split below still guards
    assert.ok(
      paths.includes(rel),
      `${rel} is not in the driver's non-shipped corpus — Tailwind scans the whole web/ tree, ` +
        `so a class literal in it is compiled into the bundle and this check would call it clean. ` +
        `Cards t_bc75e313/t_682ebba5. Sampled non-shipped paths: ${paths.slice(0, 12).join(", ")}`,
    );
  }

  // The split must stay a split: a config/script/public literal is NOT shipped
  // code, and the shipped corpus must not have quietly absorbed those trees.
  for (const prefix of ["scripts/", "e2e/", "public/", "Dockerfile", "tsconfig.json"]) {
    assert.equal(
      [...shipped.keys()].some((p) => p === prefix || p.startsWith(prefix)),
      false,
      `${prefix} appeared in the SHIPPED corpus — its literal would then be cleared as if real ` +
        `code named it, which is exactly the class of bug this check exists for`,
    );
  }

  // ... and the files Tailwind CANNOT read must be in NEITHER corpus, so the
  // driver is not blaming a `.css` or a lock file the scanner never opened.
  for (const rel of ["package-lock.json", "src/app/globals.css"]) {
    if (!existsSync(join(webRoot, rel))) continue;
    assert.equal(paths.includes(rel), false, `${rel} must not be in the non-shipped corpus`);
    assert.equal([...shipped.keys()].includes(rel), false, `${rel} must not be in the shipped corpus`);
  }

  assert.ok(shipped.size > 0 && nonShipped.size > 0);
});

test("the shipped corpus is src minus its tests, and every non-shipped file is outside it", () => {
  const { shipped, nonShipped } = sourceCorpus();

  // Shipped = real app code: `src/**` and not a test. Nothing else may qualify,
  // or a class only a test/script/config names would be cleared as "real code".
  for (const p of shipped.keys()) {
    assert.ok(
      p.startsWith("src/"),
      `${p} is in the SHIPPED corpus but is not under src/ — a non-shipped literal would be ` +
        `cleared by it`,
    );
    assert.equal(
      /\.test\.tsx?$/.test(p),
      false,
      `${p} is a unit test and must not be shipped code`,
    );
  }
  // Every `src` test file is non-shipped (the exclusion in globals.css relies on
  // the same split), and the two corpora are disjoint.
  const srcTests = [...nonShipped.keys()].filter((p) => p.startsWith("src/"));
  assert.ok(srcTests.length >= 40, `expected the src test files in nonShipped, found ${srcTests.length}`);
  for (const p of srcTests) assert.match(p, /\.test\.tsx?$/, `${p} is under src/ and non-shipped, so it must be a test file`);
  for (const p of shipped.keys()) {
    assert.equal(nonShipped.has(p), false, `${p} is in BOTH corpora — the split is not a split`);
  }
});

test("the corpus the driver assembles makes a Dockerfile- or tsconfig-only utility a VIOLATION", () => {
  // The two literals card t_682ebba5 measured: each present ONLY in one of the
  // two scanned-but-unlisted files the hand-written corpus never named. They are
  // DIFFERENT shapes — `Dockerfile` has no extension, `tsconfig.json` is a
  // web-root `.json` — so both are exercised: fixing this by name would otherwise
  // pass for one and fail for the other, which is how the previous fix broke.
  const { shipped, nonShipped } = sourceCorpus();
  // The ESCAPED CSS form each literal emits (`:`/`[`/`#`/`]` backslash-escaped,
  // plus the trailing `:hover` pseudo that is not part of the class). Written out
  // rather than built by a replace chain so the fixture cannot drift from the
  // real emitted shape these tests are standing in for.
  const emitted: Record<string, string> = {
    "hover:bg-[#f0a1b2]": ".hover\\:bg-\\[\\#f0a1b2\\]:hover{background-color:#f0a1b2}",
    "hover:bg-[#f0b2c3]": ".hover\\:bg-\\[\\#f0b2c3\\]:hover{background-color:#f0b2c3}",
  };
  for (const [file, literal] of [
    ["Dockerfile", "hover:bg-[#f0a1b2]"],
    ["tsconfig.json", "hover:bg-[#f0b2c3]"],
  ] as const) {
    const css = emitted[literal];
    const inFile = new Map([[file, `<div className="${literal}">`], ...nonShipped]);
    assert.deepEqual(
      deadUtilities(css, shipped, inFile).map((v) => v.literal),
      [literal],
      `a utility named only by web/${file} was NOT reported — the gap card t_682ebba5 is about is ` +
        `open again`,
    );
    // ... and the same utility must be CLEARED once real app code names it, so
    // this is not simply "report everything".
    assert.deepEqual(
      deadUtilities(css, new Map([["src/app/foo.tsx", `<div className="${literal}">`]]), inFile),
      [],
      `a utility that BOTH web/${file} and real app code name must stay clean — the corpus split ` +
        `is not allowed to change a class real code uses`,
    );
  }
});

test("a binary file in the tree is in neither corpus, and its bytes are not read as text", () => {
  // Tailwind does not scan binary files, and neither may this corpus. The failure
  // mode is silent and one-directional: reading a PNG as text can only ADD
  // literals that were never scanned, so it over-blames a file that cannot have
  // caused a utility — and (mutation D of this card's table) NO other test in this
  // file goes red for it. Hence this one.
  const { shipped, nonShipped } = sourceCorpus();
  const png = "public/brand/icon-512.png";
  if (!existsSync(join(webRoot, png))) {
    console.log(`  (skipped: ${png} not present in this tree)`);
    return;
  }
  // Assert the FIXTURE still has the property the rule is about, so a future
  // text-only PNG makes this test say so instead of passing vacuously.
  assert.ok(
    readFileSync(join(webRoot, png)).includes(0),
    `${png} contains no NUL byte, so it no longer exercises the binary rule — pick another fixture`,
  );
  assert.equal(shipped.has(png), false, `${png} is binary and must not be shipped code`);
  assert.equal(nonShipped.has(png), false, `${png} is binary and must not be blamed for a literal`);
});

test("a git-ignored file is in neither corpus when git can answer, and never silently shipped", () => {
  // Tailwind's first exclusion is "files in your .gitignore", so the corpus must
  // not carry one: `web/next-env.d.ts` is `.gitignore`d (and IS scanned by
  // Tailwind — it is excluded by the ignore rule, not by a binary/extension
  // filter), so a literal in it cannot reach the bundle and must not be blamed.
  //
  // TWO ENVIRONMENTS, because this runs in both: the tree can BE asked (a
  // checkout with git) or it cannot (the shipping IMAGE — `node:24-alpine` with
  // no git installed, which `npm run build` runs inside; the webimage gate caught
  // this exact assumption). When it cannot, the driver's contract is to keep the
  // file as source — over-blame, never clear — so a gitignored literal is still
  // REPORTED if it ships, which is fail-closed and is what is asserted here.
  //
  // SKIPPED when the fixture is absent: `npm run test:unit` runs BEFORE
  // `next build`, so on a clean checkout the generated file may not exist yet.
  // That is the fixture missing, not the assertion failing.
  const { shipped, nonShipped, ignored } = sourceCorpus();
  const ignoredPath = "next-env.d.ts";
  if (!existsSync(join(webRoot, ignoredPath))) {
    console.log(`  (skipped: ${ignoredPath} not present in this tree — it is generated by next build)`);
    return;
  }
  assert.equal(
    shipped.has(ignoredPath),
    false,
    `${ignoredPath} is a generated file and must never be read as SHIPPED code, or a literal in ` +
      `it would be cleared as if real app code named it`,
  );

  if (ignored === null) {
    // No git here (the image). The rule set is unknowable, so the file stays in
    // the non-shipped corpus and a literal sourced only from it is REPORTED.
    const literal = "hover:bg-[#f0c3d4]";
    const css = ".hover\\:bg-\\[\\#f0c3d4\\]:hover{background-color:#f0c3d4}";
    assert.ok(
      nonShipped.has(ignoredPath),
      `git could not answer AND ${ignoredPath} was dropped from the corpus — an unreadable rule ` +
        `set must over-blame, never silently clear a file`,
    );
    assert.deepEqual(
      deadUtilities(css, shipped, nonShipped).map((v) => v.literal),
      [literal],
      `with no git to ask, a literal sourced only from ${ignoredPath} must still be reported`,
    );
    return;
  }

  // Git answered: the ignore rule is applied, so the file is in NEITHER corpus.
  assert.ok(ignored.has(ignoredPath), `git did not report ${ignoredPath} as ignored`);
  assert.equal(nonShipped.has(ignoredPath), false, `${ignoredPath} is git-ignored and must not be blamed`);
});
