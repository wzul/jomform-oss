// Every screenshot guide must OPEN ON ITS FIRST STEP, in prose that wraps like prose
// (card t_b5ea9103).
//
// THE DEFECT, as it was measured on the live page (jomform.com, v1.30.0 tree) at 390px:
// /docs/guides/wedding-rsvp rendered its own intro TWICE (once as `{guide.summary}`, once as
// the body's first paragraph), carried a maintainer's "9 screenshots, one per step … the red
// box marks …" paragraph between the two copies, and preserved every README hard-wrap as a
// REAL line break. The page measured 7,054px tall and 1,284 words, and "1. Start a new form"
// was not on the first screen. Wan: "susunan tu tak ok, tiba-tiba ada new line, wordy sgt".
//
// WHY A BROWSER SPEC. Three of the four claims cannot be made from a DOM-free render:
//   * the duplicate lived in the GuideArticle COMPONENT (JSX, so `node --test` cannot render
//     it — src/lib/guidesContent.test.ts pins the generator's half, this pins the page's);
//   * `white-space: pre-wrap` is a COMPUTED style, and the class that produces it could be
//     inherited from anywhere;
//   * an offset in pixels is a layout fact. "The first step is on the first screen" is a
//     statement about a 390x844 viewport, not about a word count.
// The unit guards next door stay in place: a fast red on the content, a real red on the page.
//
// WHAT MAKES IT BITE — each assertion names the shape it would fail on, and the loop covers
// EVERY guide in src/lib/guides.gen.ts, so a new guide cannot be added outside the budget:
//   * words: `document.body.innerText` at 390px, whitespace-collapsed, footer included;
//   * firstStepTop: the y of the first <h2> inside <article> with the page at scroll 0;
//   * preserved: paragraphs / list items whose COMPUTED white-space is not `normal`;
//   * ledeHits: occurrences of the guide's own summary sentence in the article's text.
//
// WHAT IT DOES NOT COVER (stated, not implied): it does not judge whether a screenshot's red
// box marks the right control (that is a review question), and it does not scan axe — the
// `a11y` gate owns contrast and semantics on the same three docs routes.

import { test, expect } from "@playwright/test";

import { GUIDES } from "../src/lib/guides.gen";

// The phone the card measured on: an iPhone 12/13/14 logical viewport.
const PHONE = { width: 390, height: 844 };

// The budget the card sets for a guide whose job is "do this, then this". It is the whole
// page's visible text at 390px, footer included — the strict reading of "<= 700 rendered
// words", so the unit guard's narrower article-only budget can never hide a regression here.
const WORD_BUDGET = 700;

// The first step has to be ON the first screen: its top edge above the fold.
const FIRST_STEP_MAX_TOP = PHONE.height;

test.describe("screenshot guides at 390px", () => {
  test.use({ viewport: PHONE });

  test("every guide opens on step 1, prints its lede once, and wraps like prose", async ({ page }) => {
    const problems: string[] = [];
    const measured: { slug: string; words: number; firstStepTop: number; height: number; preserved: number }[] = [];

    for (const guide of GUIDES) {
      const route = `/docs/guides/${guide.slug}`;
      const response = await page.goto(route, { waitUntil: "load" });
      expect(response?.status(), `${route} answered ${response?.status()} — this spec cannot judge a page that did not render`).toBe(200);

      await page.locator("article h1").first().waitFor({ state: "visible" });
      // The step headings are in the markdown body; wait for one so the measurement is of the
      // rendered article, not of a shell that has not hydrated/rendered its images yet.
      await page.locator("article h2").first().waitFor({ state: "visible" });
      await page.evaluate(() => window.scrollTo(0, 0));

      const reading = await page.evaluate((summary: string) => {
        const article = document.querySelector("article");
        if (!article) return { missingArticle: true } as const;

        const text = (article.innerText || "").replace(/\s+/g, " ").trim();
        const firstH2 = article.querySelector("h2");
        const paragraphs = [...article.querySelectorAll("p, li")];
        const preserved = paragraphs
          .map((el) => ({ text: (el.textContent || "").slice(0, 60), ws: getComputedStyle(el).whiteSpace }))
          .filter((p) => p.ws !== "normal" && p.ws !== "normal white-space");

        return {
          missingArticle: false as const,
          words: (document.body.innerText || "").split(/\s+/).filter(Boolean).length,
          articleWords: text.split(/\s+/).filter(Boolean).length,
          firstStepTop: firstH2 ? firstH2.getBoundingClientRect().top : Number.POSITIVE_INFINITY,
          firstStepText: firstH2 ? (firstH2.textContent || "").trim() : "",
          height: document.documentElement.scrollHeight,
          preserved,
          // The lede is the sentence the page prints under its <h1>; it must appear ONCE.
          ledeHits: summary ? text.split(summary.replace(/\s+/g, " ").trim()).length - 1 : -1,
          // The maintainer's screenshot-count note must be gone from the article.
          metaNote: /screenshots?,?\s+one per step/i.test(text) || /the red box marks/i.test(text),
          // A hard-wrapped source line would leave a trailing "…" style artefact only if it was
          // truncated; assert the lede is a complete sentence instead (no "…" cut).
          ledeTruncated: /…/.test(summary),
        };
      }, guide.summary);

      expect(reading.missingArticle, `${route} rendered no <article> — nothing below would mean anything`).toBe(false);
      if (reading.missingArticle) continue;

      measured.push({
        slug: guide.slug,
        words: reading.words,
        firstStepTop: Math.round(reading.firstStepTop),
        height: reading.height,
        preserved: reading.preserved.length,
      });

      if (reading.words > WORD_BUDGET) {
        problems.push(`${route}: ${reading.words} words at 390px (budget ${WORD_BUDGET})`);
      }
      if (!/^1\./.test(reading.firstStepText)) {
        problems.push(
          `${route}: the first <h2> is "${reading.firstStepText}", not the first step — the article ` +
            `opens with something other than "1. …"`,
        );
      }
      if (reading.firstStepTop > FIRST_STEP_MAX_TOP) {
        problems.push(
          `${route}: the first step's top is ${Math.round(reading.firstStepTop)}px, below the ` +
            `${FIRST_STEP_MAX_TOP}px first screen — the intro pushes it out of view`,
        );
      }
      if (reading.preserved.length > 0) {
        const sample = reading.preserved[0];
        problems.push(
          `${route}: ${reading.preserved.length} paragraph/list element(s) compute a preserving ` +
            `white-space (${sample.ws}: "${sample.text}…") — the README's hard wraps would render`,
        );
      }
      if (reading.ledeHits !== 1) {
        problems.push(
          `${route}: the lede sentence appears ${reading.ledeHits} time(s) in the article (want 1) — ` +
            `"${guide.summary.slice(0, 60)}…"`,
        );
      }
      if (reading.ledeTruncated) {
        problems.push(`${route}: the lede is a truncated sentence (ends in "…")`);
      }
      if (reading.metaNote) {
        problems.push(
          `${route}: the article still carries the maintainer's screenshot/red-box note — that ` +
            `belongs on /docs/guides once, not on every guide page`,
        );
      }
    }

    // The numbers this card is measured on, printed so a reviewer can read them without a
    // second run. Not an assertion: it is evidence.
    for (const m of measured) {
      // eslint-disable-next-line no-console
      console.log(
        `${m.slug.padEnd(20)} words=${String(m.words).padStart(4)}  firstStepTop=${String(m.firstStepTop).padStart(4)}px  ` +
          `height=${String(m.height).padStart(5)}px  preserved=${m.preserved}`,
      );
    }

    expect(measured.length, "the spec measured no guide — GUIDES was empty").toBe(GUIDES.length);
    expect(problems, `these guide pages are not readable at 390px:\n  ${problems.join("\n  ")}`).toEqual([]);
  });
});
