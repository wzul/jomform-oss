// Regression guard for "the built CSS still ships a dead utility from a class
// literal written in a comment" (cards t_aafe45cc, t_850589b5).
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
//   THE SAME MECHANISM REACHES A SECOND TREE (card t_850589b5). The first
//   pattern is scoped to `web/src`, so the Playwright specs under `web/e2e` were
//   never excluded and their prose was still compiled: measured on the PR #994
//   head bundle, `.bg-red-700` and `.bg-sky-600` came solely from
//   `a11y-chat-card.spec.ts` (an a11y contrast table) and `.m-1`/`.m-2`/`.m-3`
//   from a fixture's message ids in `feedback-notify-marker.spec.ts`. A SECOND
//   `@source not` line excludes that tree. So "every class literal a guard's prose
//   names" was true only for `src` specs until this card; both patterns are pinned
//   below.
//
// WHY THE FIX IS A SCAN EXCLUSION AND NOT A REWORDED COMMENT
//   Rewording one comment fixes the one literal and leaves the mechanism: the
//   next `text-red-600` or `left-[22px]` written in a guard's rationale ships a
//   real utility again, silently. Unit tests and e2e specs are not shipped code
//   and their strings must never generate a class, so the scan is narrowed once,
//   here. `@source not` is the documented Tailwind v4.1+ mechanism for exactly
//   this (docs: Detecting classes in source files -> "Ignoring specific paths").
//   Both patterns are relative to THIS stylesheet (`web/src/app/`), so
//   `../**/*.test.{ts,tsx}` is every `src/**/*.test.ts` and `../../e2e/…` is the
//   `web/e2e` tree.
//
// THE STATED LIMIT
//   This guard pins the two DIRECTIVES, not the emitted bundle: it does not run a
//   Next build (there is no build step inside `npm run test:unit`). It cannot see
//   a `*.test.tsx` that does not exist yet, and a class that is both in a test AND
//   in real app code is unaffected by the exclusion by design. What it makes
//   impossible is the failure these cards were: an exclusion being deleted or
//   reworded, so the old dead utility silently returns. The EMITTED bundle is
//   checked by `scripts/cssScanScope.mjs`, which `npm run build` runs after
//   `next build` — this guard and that check are the two halves.
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

/** The directive card t_aafe45cc's fix depends on (the `web/src` unit tests). */
const SRC_EXCLUSION = '@source not "../**/*.test.{ts,tsx}";';
/** The directive card t_850589b5's fix depends on (the `web/e2e` specs). */
const E2E_EXCLUSION = '@source not "../../e2e/**/*.spec.{ts,tsx}";';

test("unit tests under src are excluded from Tailwind's scan (card t_aafe45cc)", () => {
  // The directive, verbatim. A reworded or glob-widened pattern fails here on
  // purpose: the value being pinned is the one measured to drop the dead
  // utilities, and `@source not` order/scope is subtle enough that an "equivalent"
  // rewrite is worth a re-measure, not a silent swap.
  assert.ok(
    css.includes(SRC_EXCLUSION),
    `globals.css no longer excludes src test files from Tailwind's scan — the dead ` +
      `\`.hover\\:bg-\\[\\#828fff\\]:hover\` utility will be compiled back into the ` +
      `shipped bundle. Expected the line:\n  ${SRC_EXCLUSION}`,
  );
});

test("the e2e specs are excluded from Tailwind's scan (card t_850589b5)", () => {
  // The second non-shipped tree. Its prose shipped `.bg-red-700`, `.bg-sky-600`
  // and `.m-1`/`.m-2`/`.m-3` (see the header) until this line existed; deleting
  // it brings all five back.
  assert.ok(
    css.includes(E2E_EXCLUSION),
    `globals.css no longer excludes the web/e2e specs from Tailwind's scan — the ` +
      `\`.bg-red-700\`, \`.bg-sky-600\` and \`.m-1\`/\`.m-2\`/\`.m-3\` utilities sourced only ` +
      `from e2e prose will be compiled back into the shipped bundle. Expected the line:\n  ` +
      E2E_EXCLUSION,
  );
});

test("the exclusions are scoped to the Tailwind stylesheet they are written in", () => {
  // `@source not` is resolved relative to the stylesheet, so a pattern only means
  // what this card measured while globals.css is the file that imports Tailwind.
  // If the import ever moves, the patterns point at the wrong root and the guards
  // above would pass while nothing was excluded.
  assert.match(
    css,
    /@import\s+"tailwindcss";/,
    "globals.css no longer opens with `@import \"tailwindcss\"` — the @source not patterns are " +
      "relative to this stylesheet, so their scope is no longer what these cards measured",
  );
  // Each negative rule must come before `@import` (the documented shape: @source
  // rules are collected after the import they scope).
  const importAt = css.indexOf('@import "tailwindcss";');
  for (const [label, exclusion] of [
    ["src test", SRC_EXCLUSION],
    ["e2e spec", E2E_EXCLUSION],
  ] as const) {
    assert.ok(
      importAt < css.indexOf(exclusion),
      `the ${label} @source not line now precedes \`@import "tailwindcss"\` — re-check that the ` +
        `exclusion still applies to the utility layer before trusting it`,
    );
  }
  // Both patterns must be present as a set: a tree that dropped one of them is
  // exactly the half-coverage these guards exist to prevent.
  assert.ok(
    css.indexOf(SRC_EXCLUSION) !== css.indexOf(E2E_EXCLUSION),
    "the two @source not lines collapsed into one — the scan now covers only one tree",
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

test("there really are e2e specs for the e2e exclusion to cover", () => {
  // The same no-op check for the second tree: `web/e2e` (where globals.css's
  // `../../e2e` resolves) must actually hold `*.spec.ts` files, or the exclusion
  // protects nothing and the guard above is checking a directive that is dead.
  const e2e = fileURLToPath(new URL("../../e2e", import.meta.url)); // web/e2e/
  const specs = readdirSync(e2e).filter((n) => /\.spec\.tsx?$/.test(n));
  assert.ok(
    specs.length >= 10,
    `the walk found only ${specs.length} *.spec.ts files under web/e2e — the ` +
      `\`../../e2e/**/*.spec.{ts,tsx}\` pattern protects a tree this guard can no longer see, ` +
      `so re-anchor it`,
  );
});
