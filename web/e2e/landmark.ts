// The `main` LANDMARK arm, shared by every signed-in a11y spec (card t_25a8fc88).
//
// THE DEFECT IT GUARDS. Every dashboard route renders its page body inside the
// app chrome (`web/src/components/Shell.tsx`), and that slot was a plain
// `<div id="main-content">`. Only `/profile` and `/settings` happened to carry a
// `<main>` of their own, so a full-document axe 4.13.0 scan of a signed-in page
// reported, on every other route:
//
//   * `landmark-one-main` (moderate) — the document has no `main` landmark, and
//   * `region` (moderate) — one node per block of page content outside every
//     landmark (`.items-start`, `.mt-6`, `.jf-card`, … on `/products`).
//
// Both rules are MODERATE, so the gate's critical/serious bar stayed green over a
// document with no landmark at all. The Shell now renders the document's one
// `<main>` around the page body; the pages keep `<div>` bodies (a page that kept
// its own `<main>` would nest two — invalid, and axe reports
// `landmark-main-is-top-level`). `SessionFault` is the one exception: it is a
// full-page `<main>` of its own, replaced into the same slot by the Shell and
// also rendered from `/`, where no Shell exists.
//
// WHAT IS MEASURED. The live document first, not an inference from axe: the
// number of `main` elements, whether one of them CONTAINS the page's own `<h1>`
// (the landmark has to own the page, not sit beside it), and whether any is
// nested inside another. Then the two rules themselves, in their own scan, so a
// route that also grew a critical violation still fails here naming the landmark.
//
// Call it from any arm that has already driven its route into the signed-in
// settled state — it reads the document it is handed.
import { expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

export async function expectOneMainLandmark(page: Page, what: string): Promise<void> {
  const dom = (await page.evaluate(`(() => {
    const mains = Array.from(document.querySelectorAll("main"));
    const h1 = document.querySelector("h1");
    return {
      count: mains.length,
      ownsH1: h1 === null || mains.some((m) => m.contains(h1)),
      nested: mains.some((m) => m.parentElement !== null && m.parentElement.closest("main") !== null),
    };
  })()`)) as { count: number; ownsH1: boolean; nested: boolean };

  expect(
    dom.count,
    `${what} renders ${dom.count} <main> element(s) — a signed-in page must have exactly one, ` +
      "the Shell's (landmark-one-main fails at 0 and landmark-main-is-top-level at 2)",
  ).toBe(1);
  expect(
    dom.nested,
    `${what} nests a <main> inside another one — invalid HTML, and axe reports landmark-main-is-top-level`,
  ).toBe(false);
  expect(
    dom.ownsH1,
    `${what}: no <main> contains the page's <h1> — the landmark exists but does not wrap the page, ` +
      "so the content is still outside every landmark (the `region` violation)",
  ).toBe(true);

  const results = await new AxeBuilder({ page }).withRules(["landmark-one-main", "region"]).analyze();
  const named = results.violations.map(
    (v) => `${v.id} (${v.impact}) × ${v.nodes.length}: ${v.nodes.map((n) => n.target.join(" ")).join(" | ")}`,
  );
  console.log(
    `[landmark] ${what} main=${dom.count} ownsH1=${dom.ownsH1} nested=${dom.nested} ` +
      `axe=${named.length === 0 ? "landmark-one-main+region PASS" : named.join(" ; ")}`,
  );
  if (named.length > 0) console.log(`\n[${what}] landmark violations:\n  ${named.join("\n  ")}`);
  expect(named, `${what} has ${named.length} landmark/region axe violation(s)`).toEqual([]);

  // NON-VACUITY: `landmark-one-main` has to have EVALUATED on this document and
  // passed. A reading of "no violations" from a run where the rule never applied
  // is how a page with no `main` at all would read as clean.
  expect(
    results.passes.map((r) => r.id),
    `axe never evaluated landmark-one-main on ${what} (it is not in passes), so the 0-violation ` +
      "reading above is vacuous",
  ).toContain("landmark-one-main");
}
