// The a11y gate's axe RULE SET has ONE definition (card t_5aa6c8ac).
//
// THE BUG. Nine scoped arms — three in `e2e/a11y-switcher.spec.ts`, two in
// `e2e/a11y-admin-feedback-row.spec.ts`, three in `e2e/a11y-avatar-badge.spec.ts`,
// one in `e2e/a11y-dashboard-status-badge.spec.ts` — each carried its own
//
//     .withTags(["wcag2aa", "wcag21aa"])
//
// and each header called that "the gate's own rule, kept identical to
// `e2e/a11y.spec.ts` so the two cannot drift". It was not identical: those two
// tags reach 6 of axe-core 4.13.0's 105 rules, while `a11y.spec.ts` runs the
// DEFAULT set. A subtree carrying `role="menuitem" aria-checked="true"`, a
// context-less `role="tab"`, `aria-notarealthing="1"` and a nameless button read
// 0 violations under the tag set and 4 critical ARIA violations under the
// default one. The `IMPACTS = ["critical","serious"]` filter those arms apply
// only means something if A-level rules run, so nine arms were strict about
// contrast and blind to the WCAG 4.1.2 / ARIA class beside it.
//
// WHAT THIS PINS. The rule now lives in `e2e/axeGate.ts`: `gateScan()` (the rule
// set) and `blockersOf()` (the failing impacts), and BOTH `a11y.spec.ts` and the
// nine arms call those two functions. This test fails if
//   (a) any a11y spec goes back to narrowing the rule set at a call site, or
//   (b) the shared gate itself narrows it, or
//   (c) one of the four scoped specs stops going through the shared gate.
//
// The RUNTIME half of the same card is `expectGateSeesAriaViolation` in
// `e2e/axeGate.ts`, which all nine arms call: it plants a prohibited ARIA state
// inside the subtree the arm just read and requires that same scan to report it,
// so a narrowed rule set fails the GATE, not only this file. That is the
// assertion the card asks for — this one only keeps the shape from rotting back.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";

const pathOf = (rel: string) => fileURLToPath(new URL(rel, import.meta.url));
const E2E_DIR = pathOf("../../e2e");

/** The axe rule set + failing impacts, shared by the whole family. */
const GATE = "axeGate.ts";

/** The four specs whose nine arms used to carry their own copy of the rule. */
const SCOPED_ARMS = [
  "a11y-switcher.spec.ts",
  "a11y-admin-feedback-row.spec.ts",
  "a11y-avatar-badge.spec.ts",
  "a11y-dashboard-status-badge.spec.ts",
];

const read = (name: string) => readFileSync(`${E2E_DIR}/${name}`, "utf8");

/** Every spec the `a11y` project selects (playwright.config.ts: filename prefix). */
const A11Y_SPECS = readdirSync(E2E_DIR).filter((f) => f.startsWith("a11y") && f.endsWith(".spec.ts"));

/**
 * `src` with its COMMENT-ONLY lines dropped, so a file may explain the defect in
 * prose (`// the arms used to call .withTags([...])`) without tripping the rule.
 *
 * A line is treated as commentary only when it STARTS with a comment marker. A
 * trailing comment on a code line is kept, which can only ever make this test
 * MORE strict — a false red is loud and reviewable, a false green is the defect
 * this file exists for. The pre-fix shape is a chained `.withTags(` on its own
 * line, and that line is kept.
 */
function codeOnly(src: string): string {
  return src
    .split("\n")
    .filter((line) => !/^\s*(?:\/\/|\*|\/\*)/.test(line))
    .join("\n");
}

/** A call site narrowing axe's rule set. `withTags`/`withRules` are the two knobs
 *  that produce a strict SUBSET of the gate; `disableRules` is reported by axe
 *  as `inapplicable` evidence and is not the shape this card is about. */
const NARROWING = /\.(?:withTags|withRules)\s*\(/;

const narrowsRuleSet = (src: string) => NARROWING.test(codeOnly(src));

/**
 * The ONE place a narrowed rule set is the arm's SUBJECT rather than an
 * oversight, so it is exempted by name with its reason written down (the same
 * shape as the `intentionallyUnselected` allowlist in the migration/scan guard).
 *
 * `a11y-singlechar-text.spec.ts` exists because axe NEVER reports a
 * one-character element as a `color-contrast` VIOLATION (it lands in
 * `incomplete`: "Element content is too short…"), so its control runs that one
 * rule twice — 1 character reads 0 violations, the SAME paint on 2 characters
 * reads the violation — which is a statement about axe, not a covered subtree.
 * It claims nothing outside the rule it names, and its route census is measured
 * in the browser rather than asserted through axe.
 *
 * The exemption is BIDIRECTIONAL: a stale entry (a file that no longer narrows)
 * fails the test below, so this list cannot quietly become a place where the
 * rule set is narrowed unread.
 */
const NARROWING_BY_DESIGN: Record<string, string> = {
  "a11y-singlechar-text.spec.ts": "its control runs color-contrast ALONE to demonstrate axe's single-character blind spot",
};

test("the narrowing detector sees the PRE-FIX shape and ignores prose about it", () => {
  // The exact bytes the nine arms carried. If this reads false the test below is
  // vacuous and a narrowed rule set could come back unnoticed.
  const preFix = [
    "    const results = await new AxeBuilder({ page })",
    '      .withTags(["wcag2aa", "wcag21aa"])',
    "      .include(scope)",
    "      .analyze();",
  ].join("\n");
  assert.equal(narrowsRuleSet(preFix), true, "the detector does not see the nine-arms tag set — it proves nothing");
  assert.equal(
    narrowsRuleSet("    // the arms used to call .withTags([\"wcag2aa\", \"wcag21aa\"])"),
    false,
    "a comment explaining the defect is not a narrowed rule set",
  );
  assert.equal(
    narrowsRuleSet("    const builder = new AxeBuilder({ page }).withRules([\"color-contrast\"]);"),
    true,
    "withRules is the same knob as withTags and must be refused too",
  );
});

test("no a11y spec narrows axe's rule set at a call site", () => {
  assert.ok(A11Y_SPECS.length > 0, `no a11y spec was found in ${E2E_DIR} — this rule would be vacuous`);
  const narrowing = A11Y_SPECS.filter((f) => narrowsRuleSet(read(f)));
  const offenders = narrowing.filter((f) => !(f in NARROWING_BY_DESIGN));
  assert.deepEqual(
    offenders,
    [],
    `these a11y specs narrow axe's rule set at a call site: ${JSON.stringify(offenders)}. ` +
      `The rule set is defined ONCE, in web/e2e/${GATE} (gateScan), and a local subset ` +
      `is exactly card t_5aa6c8ac: withTags(["wcag2aa","wcag21aa"]) reaches 6 of axe's 105 ` +
      `rules, so no A-level ARIA rule runs and critical WCAG 4.1.2 violations read clean. ` +
      `Call gateScan(page, scope) instead; if a subtree genuinely needs a narrower set, add it ` +
      `to ${GATE} with the reason written down.`,
  );
  // …and an exemption has to be EARNED: a file that stopped narrowing must drop
  // its entry, or the allowlist becomes a blind spot of its own.
  const stale = Object.keys(NARROWING_BY_DESIGN).filter((f) => !narrowing.includes(f));
  assert.deepEqual(
    stale,
    [],
    `these exemptions are stale — the spec no longer narrows the rule set: ` +
      JSON.stringify(stale.map((f) => `${f} (${NARROWING_BY_DESIGN[f]})`)) +
      `. Delete the entry; an allowlist nobody prunes is where the next narrowed rule set hides.`,
  );
});

test("the shared gate itself does not narrow axe's rule set", () => {
  const src = read(GATE);
  assert.ok(GATE.endsWith(".ts") && src.length > 0, `${GATE} is missing or empty`);
  for (const fn of ["gateScan", "blockersOf", "expectGateSeesAriaViolation"]) {
    assert.ok(
      src.includes(`function ${fn}`),
      `e2e/${GATE} no longer exports ${fn}() — the nine scoped arms and a11y.spec.ts call it`,
    );
  }
  assert.equal(
    narrowsRuleSet(src),
    false,
    `e2e/${GATE} narrows axe's rule set. That function IS the gate's rule (card t_5aa6c8ac): ` +
      `a11y.spec.ts and the nine scoped arms both run it, so narrowing it here narrows every ` +
      `scoped arm, and the page gate, at once.`,
  );
});

test("every scoped arm goes through the shared gate, not its own AxeBuilder", () => {
  for (const f of SCOPED_ARMS) {
    const src = read(f);
    assert.ok(
      /from\s+"\.\/axeGate"/.test(src),
      `${f} does not import the shared gate from ./axeGate — its arms are measuring a rule set ` +
        `of their own again (card t_5aa6c8ac)`,
    );
    assert.ok(
      src.includes("gateScan("),
      `${f} imports the shared gate but never calls gateScan() — its axe arms would be reading ` +
        `something else`,
    );
    assert.equal(
      /new\s+AxeBuilder\s*\(/.test(codeOnly(src)),
      false,
      `${f} still constructs its own AxeBuilder, which is the call site that carried the ` +
        `six-rule tag set (card t_5aa6c8ac). Call gateScan(page, scope) instead.`,
    );
  }
});
