// Regression guard for "the built CSS still ships a dead utility from a class
// literal written in a comment" (card t_aafe45cc).
//
// THE DEFECT, AND WHY IT IS A SOURCE-CONTRACT TEST
//   Tailwind v4 (`@import "tailwindcss"` in `app/globals.css`, config
//   `@tailwindcss/postcss`) scans the source tree for anything that *looks*
//   like a class — its extractor does NOT parse the file, so it does not honour
//   `//` line comments. A class literal sitting in a test's prose is therefore
//   compiled into a REAL utility and shipped in the page bundle. After PR #991
//   the only surviving `hover:bg-[#828fff]` in the tree was inside a comment in
//   `src/lib/ctaContrast.test.ts`, and the production CSS still emitted
//   `.hover\:bg-\[\#828fff\]:hover{background-color:#828fff}` — dead bytes on
//   every page, and worse, a *working* class re-armed for whoever later writes
//   it on an element (that pairing is the 2.87:1 fill #991 removed).
//
//   Measured on origin/main (e5228f56): the built bundle carried 19 utilities
//   whose ONLY source occurrence was a `*.test.ts` file — `hover:bg-[#828fff]`
//   plus `text-emerald-600`, `text-slate-400/500`, `text-[#9ca3af]`, `left-[22px]`,
//   `left-0.5`, `top-0.5`, `md:bg-indigo-50`, `text-[--jf-muted]`, and the guards'
//   own fixture literals. Excluding test files from the scan (below) removed all
//   19 and left every real class intact (`hover:bg-[var(--jf-primary-hover)]`,
//   `text-emerald-700`, `jf-btn-primary`, `hover:bg-indigo-50/40` all still emit).
//   The class-set diff of the two builds is in PR for t_aafe45cc.
//
// WHY THE FIX IS A SCAN EXCLUSION AND NOT A REWORDED COMMENT
//   Rewording one comment fixes the one literal and leaves the mechanism: the
//   next `text-red-600` or `left-[22px]` written in a guard's rationale ships a
//   real utility again, silently. Unit tests are not shipped code and their
//   strings must never generate a class, so the scan is narrowed once, here.
//   `@source not` is the documented Tailwind v4.1+ mechanism for exactly this
//   (docs: Detecting classes in source files -> "Ignoring specific paths").
//   The pattern is relative to this stylesheet, so `..` is `web/src` and
//   `../**/*.test.{ts,tsx}` is every `src/lib/*.test.ts`.
//
// THE STATED LIMIT
//   This guard pins the DIRECTIVE, not the emitted bundle: it does not run a
//   Next build (there is no build step inside `npm run test:unit`). It cannot
//   see a `*.test.tsx` that does not exist yet, and a class that is both in a
//   test AND in real app code is unaffected by the exclusion by design. What it
//   makes impossible is the failure this card was: the exclusion being deleted
//   or reworded, so the old dead utility silently returns.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { join } from "node:path";

const css = readFileSync(fileURLToPath(new URL("../app/globals.css", import.meta.url)), "utf8");

/** The exact directive this card's fix depends on. */
const EXCLUSION = '@source not "../**/*.test.{ts,tsx}";';

test("unit tests are excluded from Tailwind's scan (the fix for this card)", () => {
  // The directive, verbatim. A reworded or glob-widened pattern fails here on
  // purpose: the value being pinned is the one measured to drop the dead
  // utilities, and `@source not` order/scope is subtle enough that an "equivalent"
  // rewrite is worth a re-measure, not a silent swap.
  assert.ok(
    css.includes(EXCLUSION),
    `globals.css no longer excludes test files from Tailwind's scan — the dead ` +
      `\`.hover\\:bg-\\[\\#828fff\\]:hover\` utility (and every class literal a guard's ` +
      `prose names) will be compiled back into the shipped bundle. Expected the line:\n  ${EXCLUSION}`,
  );
});

test("the exclusion is scoped to the Tailwind stylesheet it is written in", () => {
  // `@source not` is resolved relative to the stylesheet, so it only means
  // "every src/lib/*.test.ts" while globals.css is the file that imports
  // Tailwind. If the import ever moves, this pattern points at the wrong root
  // and the guard above would pass while nothing was excluded.
  assert.match(
    css,
    /@import\s+"tailwindcss";/,
    "globals.css no longer opens with `@import \"tailwindcss\"` — the @source not pattern is " +
      "relative to this stylesheet, so its scope is no longer what this card measured",
  );
  // The import must come before the negative rule, matching the documented shape
  // (@source rules are collected after the import they scope).
  assert.ok(
    css.indexOf('@import "tailwindcss";') < css.indexOf(EXCLUSION),
    "the @source not line now precedes `@import \"tailwindcss\"` — re-check that the exclusion " +
      "still applies to the utility layer before trusting it",
  );
});

test("the guard is reading the real stylesheet (it cannot pass vacuously)", () => {
  // A renamed/moved globals.css would make every assertion above run against the
  // wrong text (or an empty string), so anchor on the app's own rules.
  assert.match(css, /\.jf-btn-primary\s*\{/, "no .jf-btn-primary rule — this is not the app stylesheet");
  assert.match(css, /--jf-primary-hover:\s*#/, "no --jf-primary-hover token — this is not the app stylesheet");
  assert.ok(css.length > 2000, "globals.css is implausibly small — the guard is not reading the built theme");
});

test("there really are test files under src for the exclusion to cover", () => {
  // If the pattern's target directory ever empties, the exclusion is a no-op and
  // the guard above is checking a directive that protects nothing. Walk the tree
  // (no build) and prove the excluded set is non-empty.
  const lib = fileURLToPath(new URL("../", import.meta.url)); // web/src/
  const tests: string[] = [];
  const walk = (dir: string) => {
    for (const name of readdirSync(dir)) {
      const p = join(dir, name);
      if (statSync(p).isDirectory()) walk(p);
      else if (/\.test\.tsx?$/.test(name)) tests.push(p);
    }
  };
  walk(lib);
  assert.ok(
    tests.length >= 40,
    `the walk found only ${tests.length} *.test.ts files under web/src — the @source not pattern ` +
      `protects a tree this guard can no longer see, so re-anchor it`,
  );
});
