// Unit tests for the "dead utility from a non-shipped file" decision (card
// t_850589b5). The build-time driver `scripts/cssScanScope.mjs` runs this logic
// over the real tree after `next build`; here it is proven to BITE on fixtures,
// which no source-only guard (e.g. tailwindScanScope.test.ts) can do.
//
// Run with:
//   npm run test:unit
import { test } from "node:test";
import assert from "node:assert/strict";
import {
  classLiterals,
  cssSelectorLiterals,
  occursAsClass,
  deadUtilities,
} from "./cssScanScope.ts";

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
