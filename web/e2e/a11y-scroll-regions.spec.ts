// Regression guard: a horizontally scrolling box on a PUBLIC page must be
// reachable and named for the keyboard — /mcp (1 node) and /design (2 nodes)
// failed axe `scrollable-region-focusable` (serious) in the sweep behind card
// t_804040d3.
//
// THE DEFECT (measured on the parent commit, production build of this tree,
// 390x844 — the width the sweep used, because the a11y project's own 1280x720
// desktop viewport does not make these boxes scroll at all, which is why the
// `a11y` gate was GREEN over all three nodes):
//
//   /mcp     <pre class="overflow-x-auto …">        309px of code in a 292px box   tabIndex -1, no role, no name
//   /design  <div class="overflow-x-auto">          444px of table in a 358px box  tabIndex -1, no role, no name
//   /design  <div class="overflow-x-auto">          444px of table in a 358px box  tabIndex -1, no role, no name
//
//   A scroll container with no tab stop cannot be scrolled from the keyboard: a
//   keyboard-only reader sees the first 358px of that table and nothing else.
//   axe's fix is the one the repo already applies to every /docs table
//   (src/lib/docsTableScroll.test.ts): `tabIndex={0}` for the tab stop and
//   `role="region"` + a name so the focusable box is announced as a NAMED
//   region rather than a bare group.
//
// WHY THIS FILE AND NOT A SOURCE GREP. The question is "which boxes really
// scroll at this width" — that is layout, and a class name does not answer it
// (`overflow-x-auto` is on four /design wrappers; only two of them overflow at
// 390px, and none at 1280px). The census below therefore asks the browser, and
// every finding is measured off the live element.
//
// WHAT MAKES IT BITE — three ways, because "0 violations" is not evidence:
//   1. the census is pinned by COUNT (1 on /mcp, 2 on /design): a region that
//      stops being detected fails, so a scan of nothing cannot read as clean;
//   2. each region is FOCUSED and `document.activeElement` is checked, so a
//      `tabIndex` attribute that does not actually produce a tab stop fails;
//   3. a CONTROL ARM strips `tabindex` off the real /mcp region and requires
//      that same page's axe scan to name `scrollable-region-focusable` on it —
//      the probe that proves the rule set can see the defect it is measuring.
import { test, expect, type Page } from "@playwright/test";

import AxeBuilder from "@axe-core/playwright";
import type { AxeResults, Result } from "axe-core";
import { DICT } from "../src/components/i18nDict";

// The gate's own rule: critical/serious fail, everything else is reported.
// Kept identical to e2e/a11y.spec.ts so the three cannot drift.
const IMPACTS = ["critical", "serious"] as const;

function blockersOf(results: AxeResults): Result[] {
  return results.violations.filter((v) => IMPACTS.includes((v.impact ?? "") as (typeof IMPACTS)[number]));
}

/** The phone width the sweep measured at (and the width these boxes scroll at). */
const PHONE = { width: 390, height: 844 };

/**
 * A fresh, English browser state. The browser keeps localStorage between runs,
 * and a `jf_lang` left behind by an earlier card once made a page render in a
 * locale its spec never asserted — so every arm here starts from a cleared one.
 */
async function freshState(page: Page) {
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("jf_lang", "en");
    } catch {
      /* storage unavailable */
    }
  });
}

type Region = {
  tag: string;
  cls: string;
  tabIndex: number;
  role: string | null;
  label: string | null;
  clientWidth: number;
  scrollWidth: number;
  scrollsX: boolean;
  scrollsY: boolean;
};

/** Every element on the page that really scrolls in at least one axis. */
async function scrollableRegions(page: Page): Promise<Region[]> {
  return page.evaluate(() => {
    const out: Region[] = [];
    document.querySelectorAll<HTMLElement>("*").forEach((el) => {
      const cs = getComputedStyle(el);
      const scrollsX = (cs.overflowX === "auto" || cs.overflowX === "scroll") && el.scrollWidth - el.clientWidth > 1;
      const scrollsY = (cs.overflowY === "auto" || cs.overflowY === "scroll") && el.scrollHeight - el.clientHeight > 1;
      if (!scrollsX && !scrollsY) return;
      out.push({
        tag: el.tagName.toLowerCase(),
        cls: el.className?.toString() ?? "",
        tabIndex: el.tabIndex,
        role: el.getAttribute("role"),
        label: el.getAttribute("aria-label"),
        clientWidth: el.clientWidth,
        scrollWidth: el.scrollWidth,
        scrollsX,
        scrollsY,
      });
    });
    return out;
  }) as Promise<Region[]>;
}

/** One line per region, for a failure message that names what it found. */
const summarise = (rs: Region[]) =>
  rs.length === 0
    ? "(none)"
    : rs
        .map(
          (r) =>
            `<${r.tag} class="${r.cls.slice(0, 60)}"> ${r.scrollWidth}px in ${r.clientWidth}px, ` +
            `tabIndex=${r.tabIndex}, role=${r.role ?? "—"}, label=${r.label ?? "—"}`,
        )
        .join("; ");

/**
 * The assertion: every scrolling box is a focusable, named region.
 *
 * `expected` is the MEASURED count at this viewport, so the arm cannot pass by
 * finding nothing (a page that failed to render has no scroll boxes either).
 */
async function expectFocusableRegions(page: Page, expected: number, where: string, labelFor: string[]) {
  const regions = await scrollableRegions(page);
  expect(
    regions.length,
    `${where} has ${regions.length} scrollable region(s) at ${PHONE.width}px, expected ${expected} — ` +
      `the census found: ${summarise(regions)}. A scan that finds nothing proves nothing.`,
  ).toBe(expected);

  for (const r of regions) {
    expect(
      r.tabIndex,
      `${where}: the scrollable <${r.tag} class="${r.cls.slice(0, 60)}"> has tabIndex=${r.tabIndex} — a scroll box with no ` +
        `tab stop cannot be scrolled from the keyboard, which is axe's scrollable-region-focusable (serious). ` +
        `Regions found: ${summarise(regions)}`,
    ).toBeGreaterThanOrEqual(0);
    expect(
      r.role,
      `${where}: the focusable scroll box <${r.tag} class="${r.cls.slice(0, 60)}"> carries no role="region" — a focusable div/pre ` +
        "with no role is announced as a bare group.",
    ).toBe("region");
    expect(
      r.label && r.label.trim().length > 0,
      `${where}: the scroll region <${r.tag} class="${r.cls.slice(0, 60)}"> has no accessible name — a focusable box with no name ` +
        "tells a screen-reader user nothing about what they have landed on.",
    ).toBeTruthy();
  }

  // Names must be unique per page (axe `landmark-unique`): two regions called
  // "Table" are two landmarks a screen reader cannot tell apart.
  const labels = regions.map((r) => r.label ?? "");
  expect(new Set(labels).size, `${where}: two scroll regions share a name: ${JSON.stringify(labels)}`).toBe(
    labels.length,
  );

  // The /mcp region's name comes from the dictionary (that page is multilingual),
  // so it must be one of the locale's own strings rather than an English literal.
  if (labelFor.length > 0) {
    for (const want of labelFor) {
      expect(
        labels,
        `${where}: no scroll region is named ${JSON.stringify(want)} — the dictionary name was replaced by a literal ` +
          `or the region is unnamed. Found: ${JSON.stringify(labels)}`,
      ).toContain(want);
    }
  }

  // AND IT REALLY TAKES A TAB STOP. `tabIndex={0}` in the source is a claim;
  // focusing the element is the measurement. The first region is enough — the
  // attribute is asserted on all of them above.
  const tag = regions[0].tag;
  const focused = await page.evaluate((tagName) => {
    const el = [...document.querySelectorAll<HTMLElement>(tagName)].find(
      (e) => e.getAttribute("role") === "region" && e.tabIndex >= 0,
    );
    if (!el) return "no focusable region found in the DOM";
    el.focus();
    return document.activeElement === el ? "" : `focus() landed on <${document.activeElement?.tagName.toLowerCase()}>`;
  }, tag);
  expect(focused, `${where}: the scroll region does not take focus: ${focused}`).toBe("");
}

for (const target of [
  { path: "/mcp", expected: 1, labelKeys: ["mcp.configExample"] as const },
  { path: "/design", expected: 2, labelKeys: [] as const },
] as const) {
  test.describe(`${target.path} scroll regions`, () => {
    test.beforeEach(async ({ page }) => {
      await freshState(page);
      await page.setViewportSize(PHONE);
    });

    test(`${target.path}: every scrollable box is a focusable, named region`, async ({ page, baseURL }) => {
      const res = await page.goto(`${baseURL}${target.path}`, { waitUntil: "networkidle" });
      expect(res?.status() ?? 0, `${target.path} did not answer 2xx — nothing was measured`).toBeLessThan(300);

      // The page really rendered: /mcp has its heading, /design its h1.
      await expect(page.locator("h1, h2").first()).toBeVisible();

      const wanted = target.labelKeys.map((k) => DICT[k].en);
      await expectFocusableRegions(page, target.expected, target.path, [...wanted]);

      // And the axe rule itself, at the width where the boxes scroll: the
      // explicit assertions above are the regression, this is the gate's own
      // verdict on the same page.
      const results = await new AxeBuilder({ page }).analyze();
      const blockers = blockersOf(results);
      if (blockers.length > 0) {
        console.log(
          `\n[${target.path} @ ${PHONE.width}px] ${blockers.length} critical/serious violation(s):\n` +
            blockers
              .map(
                (v) =>
                  `  - ${v.id} (${v.impact}): ${v.help}\n` +
                  v.nodes
                    .slice(0, 3)
                    .map((n) => `      ${n.target.join(" ")} — ${n.failureSummary?.split("\n")[0] ?? ""}`)
                    .join("\n"),
              )
              .join("\n"),
        );
      }
      expect(
        blockers.map((v) => v.id),
        `${target.path} must have no critical/serious axe violations at ${PHONE.width}px`,
      ).toEqual([]);
    });
  });
}

// THE CONTROL ARM — the census above is only evidence if the rule can see the
// defect. Strip `tabindex` from the REAL /mcp region and require THIS page's own
// axe scan to name `scrollable-region-focusable` on that very element.
test("the control arm: removing tabindex from the real /mcp region makes axe name it", async ({ page, baseURL }) => {
  await freshState(page);
  await page.setViewportSize(PHONE);
  await page.goto(`${baseURL}/mcp`, { waitUntil: "networkidle" });

  const stripped = await page.evaluate(() => {
    const el = [...document.querySelectorAll<HTMLElement>("pre")].find((e) => e.scrollWidth - e.clientWidth > 1);
    if (!el) return "no scrolling <pre> on /mcp — the region this arm mutates is gone";
    el.removeAttribute("tabindex");
    return "";
  });
  expect(stripped, `${stripped}. A control arm that cannot plant the defect proves nothing.`).toBe("");

  const results = await new AxeBuilder({ page }).analyze();
  const named = blockersOf(results)
    .filter((v) => v.id === "scrollable-region-focusable")
    .flatMap((v) => v.nodes.map((n) => n.target.join(" ")));
  expect(
    named,
    "axe did NOT report scrollable-region-focusable on the /mcp code box once its tabindex was removed — " +
      "the scan this file trusts cannot see the defect it measures, so the clean reading above is vacuous.",
  ).not.toEqual([]);
});
