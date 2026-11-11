// The surau guide's screenshots must be READABLE ON A PHONE (card t_e1ea03a2).
//
// THE DEFECT, as measured on the live page (jomform.com v1.36.0, Playwright, isMobile, dpr 2):
// each step rendered its full 1900 px capture at the 358 px article column of a 390 px phone —
// a 0.19x scale, so a 16 px UI label inside the capture landed at ~3 px. There was no zoom
// affordance either: the images were not links, there was no lightbox, nothing said "view full
// size". Wan, on /panduan/surau: "gambar screenshot tu terlalu kecik … yg baca ni mungkin org
// berusia. so, gambar skrg ni mcm x sesuai."
//
// WHY A BROWSER SPEC. Every claim below is a layout fact about a 390 px viewport:
//   * the SCALE is rendered width / natural width of the image the figure draws;
//   * the full-bleed is the figure's own bounding box against the viewport;
//   * "the desktop guide is unchanged" is the figure's box against the article's column.
// A DOM-free unit guard (src/lib/panduanSurau.test.ts) pins the same thing from the manifest —
// the crop may never be wider than the floor allows, and every figure may never stop being a
// link — but only the browser knows what the page actually drew. This spec is the one that
// would catch a Tailwind class that stopped applying, or a max-width that re-capped the figure.
//
// WHAT IT DOES NOT COVER (stated, not implied): it does not judge whether a crop still contains
// the control its step is about (that is the review question the crops.json rects answer), and
// it does not scan axe — the `a11y` gate owns semantics on this route.

import { test, expect } from "@playwright/test";

const ROUTE = "/panduan/surau";

// The phone the card measured on: an iPhone 12/13/14 logical viewport.
const PHONE = { width: 390, height: 844 };

// The floor from the card, applied to the image the figure draws: at a 390 px viewport every
// figure renders at >= 0.45x its natural width (was 0.19x).
const SCALE_FLOOR = 0.45;

// The capture width the zoom target must still have (the full-page screenshots).
const FULL_WIDTH = 1900;

type Measured = { src: string; rendered: number; natural: number; scale: number; href: string };

/** The figures of the served article, each measured as the browser draws it. */
async function measureFigures(page: import("@playwright/test").Page): Promise<Measured[]> {
  const figures = page.locator("article figure");
  const count = await figures.count();
  const out: Measured[] = [];
  for (let i = 0; i < count; i += 1) {
    const img = figures.nth(i).locator("img");
    // loading="lazy": an image below the fold has naturalWidth 0 until it is scrolled in, so
    // scroll each one into view and wait for the browser to have the bytes before measuring.
    await img.scrollIntoViewIfNeeded();
    await expect
      .poll(() => img.evaluate((el) => (el as HTMLImageElement).naturalWidth), { timeout: 10_000 })
      .toBeGreaterThan(0);
    const box = await img.boundingBox();
    const natural = await img.evaluate((el) => (el as HTMLImageElement).naturalWidth);
    const src = (await img.getAttribute("src")) ?? "";
    const href = (await figures.nth(i).locator("a").getAttribute("href")) ?? "";
    expect(box, `${src} has no layout box`).not.toBeNull();
    out.push({ src, rendered: box!.width, natural, scale: box!.width / natural, href });
  }
  return out;
}

test.describe("the surau guide's figures on a 390px phone", () => {
  test.use({ viewport: PHONE });

  test("every figure is at least 0.45x its natural width, full-bleed, and links to the 1900px capture", async ({
    page,
    request,
  }) => {
    const response = await page.goto(ROUTE, { waitUntil: "load" });
    expect(
      response?.status(),
      `${ROUTE} answered ${response?.status()} — this spec cannot measure a page that did not render`,
    ).toBe(200);
    await page.locator("article h1").first().waitFor({ state: "visible" });

    const figures = page.locator("article figure");
    const count = await figures.count();
    // The guide is nine steps with one boxed screenshot each. A page that renders fewer has lost
    // a step's figure (the unit guard pins nine too — this names the served page).
    expect(count, `${ROUTE} renders ${count} figures, not 9`).toBe(9);

    const measured = await measureFigures(page);
    const problems: string[] = [];
    const report: string[] = [];

    for (const [i, m] of measured.entries()) {
      report.push(
        `${String(i + 1).padStart(2, "0")} ${m.src}: ${Math.round(m.rendered)}x / ${m.natural} ` +
          `= ${m.scale.toFixed(3)}x`,
      );
      if (m.scale < SCALE_FLOOR) {
        problems.push(
          `${m.src}: renders at ${m.scale.toFixed(3)}x its natural width ` +
            `(${Math.round(m.rendered)} of ${m.natural} px) — the floor is ${SCALE_FLOOR}x. ` +
            `Re-crop it in docs/guides/surau-donation/crops.json and run \`npm run guides:sync\` ` +
            `to rebuild the figure.`,
        );
      }
      // Full-bleed below `sm`: the figure spans the viewport, not the article's padding box.
      const figBox = await figures.nth(i).boundingBox();
      expect(figBox, `figure ${m.src} has no layout box`).not.toBeNull();
      if (figBox!.x > 1 || Math.abs(figBox!.width - PHONE.width) > 1) {
        problems.push(
          `figure ${m.src} is ${Math.round(figBox!.width)} px wide at x=${Math.round(figBox!.x)} ` +
            `on a ${PHONE.width} px viewport — it is not full-bleed (want x=0, width=${PHONE.width}).`,
        );
      }
    }

    // Printed so the measured pairs are in the run log (the card asks for them by number).
    console.log(`\n/panduan/surau figures at ${PHONE.width}px:\n  ${report.join("\n  ")}\n`);

    // EVERY FIGURE IS A LINK TO ITS FULL-SIZE CAPTURE, and says so.
    for (let i = 0; i < count; i += 1) {
      const figure = figures.nth(i);
      const link = figure.locator("a");
      await expect(link, `figure ${i + 1} is not a link`).toHaveCount(1);
      await expect(link).toHaveAttribute("target", "_blank");
      await expect(link).toHaveAttribute("rel", /noreferrer/);
      // The accessible name names the step AND says it can be enlarged, e.g.
      // "Langkah 5: Tetapkan caj CHIP pada pembayar — ketik untuk lihat penuh".
      await expect(link).toHaveAccessibleName(/^Langkah \d+: .+ — ketik untuk lihat penuh$/);
      await expect(figure.locator("figcaption")).toContainText("Ketik untuk lihat penuh");

      const href = (await link.getAttribute("href")) ?? "";
      const full = await request.get(href);
      expect(full.status(), `${href} answered ${full.status()} — the zoom target is not served`).toBe(200);
      const png = await full.body();
      expect(png.toString("ascii", 1, 4), `${href} is not a PNG`).toBe("PNG");
      expect(png.readUInt32BE(16), `${href} is ${png.readUInt32BE(16)} px wide, not ${FULL_WIDTH}`).toBe(
        FULL_WIDTH,
      );
    }

    expect(problems, `figures a phone reader cannot use:\n  ${problems.join("\n  ")}`).toEqual([]);
  });
});

test.describe("the surau guide at desktop width", () => {
  test.use({ viewport: { width: 1280, height: 900 } });

  test("the figures stay in the article column (the desktop guide is unchanged)", async ({ page }) => {
    const response = await page.goto(ROUTE, { waitUntil: "load" });
    expect(response?.status()).toBe(200);
    await page.locator("article h1").first().waitFor({ state: "visible" });

    // A paragraph of the article body is the reference for "the article column": same parent
    // chain, no bleed classes of its own. The figures must line up with it exactly.
    const reference = await page.locator("article h1").first().boundingBox();
    expect(reference, "the article has no <h1> to measure the column against").not.toBeNull();

    const figures = page.locator("article figure");
    const count = await figures.count();
    expect(count).toBe(9);
    const problems: string[] = [];
    for (let i = 0; i < count; i += 1) {
      const box = await figures.nth(i).boundingBox();
      expect(box).not.toBeNull();
      if (Math.abs(box!.x - reference!.x) > 1 || Math.abs(box!.width - reference!.width) > 1) {
        problems.push(
          `figure ${i + 1} is ${Math.round(box!.width)} px at x=${Math.round(box!.x)}, the article ` +
            `column is ${Math.round(reference!.width)} px at x=${Math.round(reference!.x)}`,
        );
      }
    }
    expect(
      problems,
      `above sm the figures must sit in the article column, exactly where they did before the ` +
        `full-bleed was added:\n  ${problems.join("\n  ")}`,
    ).toEqual([]);
    // Nothing above `sm` may scroll sideways: the bleed must be clipped at the breakpoint.
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );
    expect(overflow, `the page scrolls sideways by ${overflow} px at 1280px`).toBeLessThanOrEqual(0);
  });
});
