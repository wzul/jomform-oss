// THE AXE RULE THE WHOLE a11y FAMILY IS MEASURED AGAINST — defined ONCE.
//
// WHY THIS FILE EXISTS (card t_5aa6c8ac). Nine scoped arms — three in
// `a11y-switcher.spec.ts`, two in `a11y-admin-feedback-row.spec.ts`, three in
// `a11y-avatar-badge.spec.ts`, one in `a11y-dashboard-status-badge.spec.ts` —
// each wrote their own copy of
//
//     new AxeBuilder({ page }).withTags(["wcag2aa", "wcag21aa"]).include(scope)
//
// and each header called that "the gate's own rule, kept identical to
// `e2e/a11y.spec.ts` so the two cannot drift". It was not identical, and it had
// drifted in the direction nobody checks:
//
// MEASURED (axe-core 4.13.0, `node probe_axe_plant.mjs` from `web/`): those two
// tags reach **6 of axe's 105 rules** — color-contrast, meta-viewport,
// valid-lang, autocomplete-valid, avoid-inline-spacing, css-orientation-lock —
// while `a11y.spec.ts` runs `.analyze()` with the DEFAULT set. On a subtree
// carrying `role="menuitem" aria-checked="true"` (a state the role does not
// support), a `role="tab"` with no tablist ancestor, `aria-notarealthing="1"`
// and a button with no accessible name, the tag set reports **0 violations**
// and the default set reports **4 critical** ones (aria-allowed-attr,
// aria-required-parent, aria-valid-attr, button-name). All 63 rules carrying the
// A-level WCAG 4.1.2 tag were excluded — and the `IMPACTS` filter those arms
// apply ("critical, serious") means nothing without them. Nine arms could read
// CLEAN over the exact ARIA class they sit beside; the contrast half was really
// covered, which is why nothing ever went red.
//
// THE FIX IS NOT "ADD wcag2a TO THE LIST". That leaves a second copy of the rule
// which can drift again. `gateScan` and `blockersOf` below are the functions
// `a11y.spec.ts` itself calls, and the scoped arms call the SAME two, so a page
// gate and a subtree gate cannot disagree about which rules run or which impacts
// fail. axe's default set is a superset of the WCAG A/AA rules (it adds the
// best-practice ones); `IMPACTS` is what keeps the failure bar at
// critical/serious, exactly as before.
//
// HOW THAT CLAIM IS KEPT HONEST — `expectGateSeesAriaViolation`. A tag list read
// back is not evidence: the six-rule list read fine to everyone who looked at
// it. So each of the nine arms, after its clean reading, plants a PROHIBITED
// ARIA state inside the very subtree it just scanned and requires THAT SAME
// scan to name it on that node. If the rule set is ever narrowed again, the
// control goes red and the arm's green reading stops being evidence.

import AxeBuilder from "@axe-core/playwright";
import { expect, type Page } from "@playwright/test";
import type { AxeResults, Result } from "axe-core";

/** The only impacts that FAIL the gate. Minor/moderate findings stay reportable
 *  but non-blocking, which is the bar `a11y.spec.ts` has always used. */
export const IMPACTS = ["critical", "serious"] as const;

export type BlockingImpact = (typeof IMPACTS)[number];

/** The violations that fail the gate, out of any axe result set. */
export function blockersOf(results: AxeResults): Result[] {
  return results.violations.filter((v) => IMPACTS.includes((v.impact ?? "") as BlockingImpact));
}

/**
 * Run the gate's own axe rule — the DEFAULT rule set, i.e. exactly what
 * `e2e/a11y.spec.ts` runs on a page. `scope` narrows the scan to a subtree
 * through `include()`, which THROWS when nothing matches, so a scoped scan
 * cannot pass over an element that failed to render.
 *
 * There is deliberately no `withTags`/`withRules` here: that parameter is the
 * knob that produced this card. If a subtree genuinely needs a different rule
 * set, add a function here with its reason written down — do not re-open a
 * local copy at the call site.
 */
export function gateScan(page: Page, scope?: string): Promise<AxeResults> {
  const builder = new AxeBuilder({ page });
  return (scope ? builder.include(scope) : builder).analyze();
}

/** The rule axe reports for the planted defect below: an ARIA state the
 *  element's role does not support (WCAG 4.1.2, tag `wcag2a`, impact critical). */
const PLANTED_RULE = "aria-allowed-attr";

/** Marker attribute the plant carries, so the assertion can find it in axe's
 *  node targets — and so cleanup can remove exactly what was added. */
const PLANT_ATTR = "data-jf-axe-plant";

/**
 * CONTROL for every scoped gate arm: prove the scan can SEE an A-level (WCAG
 * 4.1.2 / ARIA) violation inside the subtree it just read.
 *
 * It appends a `<span role="menuitem" aria-checked="true">` — a state WAI-ARIA
 * 1.2 does not define for `menuitem`, so axe reports `aria-allowed-attr` (critical,
 * `wcag2a`) on that node — re-runs the SAME `gateScan(page, scope)`, requires the
 * plumbing to name the planted node, and removes the plant again.
 *
 * A span with text renders in every subtree the family scans (a menu, a row
 * button, a badge span, a table), so this does not depend on the host's layout.
 *
 * @param scope the same selector the arm passed to `gateScan`
 * @param what  the subject, for the failure message ("the open switcher menu")
 */
export async function expectGateSeesAriaViolation(page: Page, scope: string, what: string): Promise<void> {
  const planted = await page.evaluate(
    ([sel, attr]) => {
      const host = document.querySelector(sel);
      if (!host) return `no element matches ${sel}`;
      const el = document.createElement("span");
      el.setAttribute(attr, "1");
      el.setAttribute("role", "menuitem");
      el.setAttribute("aria-checked", "true");
      el.textContent = "axe control";
      host.appendChild(el);
      return "";
    },
    [scope, PLANT_ATTR] as [string, string],
  );
  try {
    expect(
      planted,
      `the A-level control could not be planted in ${what}: ${planted}. The arm's scope ` +
        `(${JSON.stringify(scope)}) matched nothing to plant into, so its green reading is not evidence`,
    ).toBe("");
    const results = await gateScan(page, scope);
    const named = blockersOf(results)
      .filter((v) => v.id === PLANTED_RULE)
      .flatMap((v) => v.nodes.map((n) => ({ target: n.target.join(" "), summary: n.failureSummary ?? "" })))
      .filter((n) => n.target.includes(PLANT_ATTR));
    expect(
      named,
      `the scan over ${what} did NOT report the planted A-level defect (` +
        `role="menuitem" aria-checked="true" -> axe ${PLANTED_RULE}, critical/WCAG 4.1.2). ` +
        `Critical/serious violations this scan DID report: ` +
        `${JSON.stringify(blockersOf(results).map((v) => `${v.id} (${v.impact})`))}. ` +
        `A scan that cannot see a planted prohibited ARIA state cannot see a real one either, ` +
        `so the arm's clean reading above is vacuous — check the rule set in gateScan().`,
    ).not.toEqual([]);
  } finally {
    await page.evaluate((attr) => {
      document.querySelectorAll(`[${attr}]`).forEach((el) => el.remove());
    }, PLANT_ATTR);
  }
}
