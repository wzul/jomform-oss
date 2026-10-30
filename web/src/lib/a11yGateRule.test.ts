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
//   (c) one of the FIVE scoped specs stops going through the shared gate, or
//   (d) a HAND-RUN probe under `web/scripts/` carries a rule set of its own
//       (card t_c912998e — `a11y-switcher.mjs` kept the six-rule tag pair for
//       one merge after the gate was fixed, because no gate executes the probes
//       and nothing scanned them for the shape).
//
// The RUNTIME half of the same card is `expectGateSeesAriaViolation` in
// `e2e/axeGate.ts`, which every scoped arm calls — the nine original ones, and
// the three dialog arms card t_1d032719 moved onto this file's list: it plants a
// prohibited ARIA state inside the subtree the arm just read and requires that
// same scan to report it, so a narrowed rule set fails the GATE, not only this
// file. That is the
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

/** The HAND-RUN a11y probes. No gate executes them (docs/accessibility.md), so
 *  nothing but this file stands between one of them and a rule set of its own. */
const SCRIPTS_DIR = pathOf("../../scripts");

/** The axe rule set + failing impacts, shared by the whole family. */
const GATE = "axeGate.ts";

/** The five specs whose arms must read the rule from the shared gate: the four
 *  below carried their own copy of it (card t_5aa6c8ac), and `a11y-dialogs.spec.ts`
 *  carried a second copy of the same two declarations without narrowing the set
 *  (card t_1d032719), which is why nothing caught it until it was migrated. */
const SCOPED_ARMS = [
  "a11y-switcher.spec.ts",
  "a11y-admin-feedback-row.spec.ts",
  "a11y-avatar-badge.spec.ts",
  "a11y-dashboard-status-badge.spec.ts",
  "a11y-dialogs.spec.ts",
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

// ---------------------------------------------------------------------------
// Card t_c912998e — the HAND-RUN probes are measured against the same rule.
//
// `web/scripts/a11y-switcher.mjs` kept its own
// `withTags(["wcag2aa", "wcag21aa"])` for one merge AFTER the gate was fixed,
// because `npm run a11y` never runs a `scripts/` file: the card that fixed the
// nine spec arms recorded the probe as out of scope, and the two tests above
// read only `e2e/`. So the probe printed `critical/serious: 0` over a rule set
// that cannot see an A-level ARIA defect — a hand-run reading that could call a
// menu clean which the gate fails. Nothing about that was caught by a gate; this
// file is where it is now caught, because it is the only test that reads these
// files at all.
//
// The probes are NOT promoted into the gate here (they need a `SESSION` and a
// local build — docs/accessibility.md says so and `a11y-switcher.spec.ts` owns
// the switcher's real coverage). What is pinned is narrower and sufficient: a
// probe may scan whatever it likes with axe's DEFAULT set, and may not narrow it
// at a call site.
// ---------------------------------------------------------------------------

/** Every hand-run script in `web/scripts/`, by the extension the probes use. */
const PROBES = readdirSync(SCRIPTS_DIR).filter((f) => f.endsWith(".mjs"));
const probeSource = (f: string) => readFileSync(`${SCRIPTS_DIR}/${f}`, "utf8");

test("no hand-run script narrows axe's rule set at a call site", () => {
  assert.ok(
    PROBES.length > 0,
    `no .mjs script was found in ${SCRIPTS_DIR} — the rule below would be vacuous`,
  );
  const offenders = PROBES.filter((f) => narrowsRuleSet(probeSource(f)));
  assert.deepEqual(
    offenders,
    [],
    `these hand-run scripts narrow axe's rule set at a call site: ${JSON.stringify(offenders)}. ` +
      `No gate executes them (docs/accessibility.md), so nothing else would ever notice: ` +
      `a subset of the gate's rules read as a clean probe over a page the gate fails. ` +
      `Import gateScan()/blockersOf() from ../e2e/${GATE} and run the default set.`,
  );
});

test("the switcher probe reads the gate's rule, not a copy of it", () => {
  const src = probeSource("a11y-switcher.mjs");
  assert.ok(src.length > 0, "web/scripts/a11y-switcher.mjs is missing or empty");
  assert.ok(
    /from\s+"\.\.\/e2e\/axeGate(?:\.ts)?"/.test(src),
    `a11y-switcher.mjs does not import the shared rule from ../e2e/${GATE}. It printed a ` +
      `critical/serious verdict from its own six-rule tag set (card t_c912998e) — a hand-run ` +
      `reading that can contradict the gate's.`,
  );
  for (const fn of ["gateScan(", "blockersOf("]) {
    assert.ok(
      src.includes(fn),
      `a11y-switcher.mjs imports ../e2e/${GATE} but never calls ${fn}) — its scan is reading ` +
        `something other than the gate's rule set and failing impacts.`,
    );
  }
  assert.equal(
    /new\s+AxeBuilder\s*\(/.test(codeOnly(src)),
    false,
    `a11y-switcher.mjs constructs its own AxeBuilder again — that call site is exactly where ` +
      `the six-rule tag set lived (card t_c912998e). Call gateScan(page, scope).`,
  );
});

