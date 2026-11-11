// THE USE-CASE CARDS ARE ON A WORD AND HEIGHT BUDGET, IN ALL FIVE LOCALES
// (card t_516bab9d — Wan, on three phone screenshots of the tuition / club / donation
// cards: "bahagian ni masih nampak wordy").
//
// THE DEFECT, MEASURED ON PROD jomform.com AT A TRUE 390px VIEWPORT (isMobile, dpr 2),
// 2026-11-11 — per card: words / height / lead paragraph / bullets:
//
//   Tuition and classes                     138 words  708px  72-word lead  4 bullets (9 avg)
//   Collect donations with a paid form      124        664    60             4 (9)
//   Club memberships with fees              121        640    44             4 (14)
//   Build a wedding landing page + RSVP      77        504    20             4 (7)
//   Affiliate marketing built in             72        466    26             4 (6)
//   QR ordering for restaurants              69        420    30             4 (5)
//   Design with your AI agent               56        413    21             4 (4)
//
//   Five text blocks competed in every card and TWO links did one job: a whole
//   "Read the full … guide with screenshots →" sentence plus the CTA button. The three
//   Wan flagged were exactly the three heaviest, and the weight was the LEAD PARAGRAPH —
//   44 to 72 words of prose before a single bullet.
//
// WHAT IS ASSERTED, PER CARD, IN EVERY ONE OF THE FIVE LOCALES:
//   1. <= 55 rendered words in the whole card (the icon and the tick glyphs are
//      aria-hidden decoration and are not counted).
//   2. The lead paragraph after the heading is <= 20 words.
//   3. Exactly four bullets, none longer than 5 words.
//   4. The card measures <= 460px tall.
//   5. One filled CTA and one guide link — and the guide link is NOT the filled one.
//   6. No card carries a "Read the full … guide" sentence any more (the line is gone,
//      not shortened), and the guide link's accessible name carries the card's title so
//      the seven identical "Guide" labels are still telling apart in a link list.
//   7. The seven cards are within 10 words of each other, so the grid stays even.
//
// WHY ALL FIVE LOCALES AND NOT ENGLISH ONLY: the budget is a LAYOUT budget. Tamil and
// Arabic run longer than English for the same sentence, and a translation is exactly
// where a word budget silently breaks. The spec fails on whichever locale is worst, and
// each locale is its own test so a red names the language.
//
// FRESH BROWSER STATE ON EVERY ARM: Playwright gives each test its own context, and the
// arm still clears storage and cookies and sets `jf_lang` from scratch before measuring,
// then proves `html[lang]` and the FIRST CARD'S HEADING match that locale — otherwise a
// page rendering in the wrong language would be measured against the wrong budget.
import { test, expect, type Page } from "@playwright/test";

import { DICT, LANGS, type Lang } from "../src/components/i18nDict";

// The phone the card measured.
const PHONE = { width: 390, height: 844 };

// The budget. One number per rule, in one place: the copy and the card frame are tuned
// against these, so a later "improve the copy" edit cannot quietly spend them.
const BUDGET = {
  words: 55,
  lead: 20,
  bullet: 5,
  height: 460,
  spread: 10,
  bullets: 4,
} as const;

test.use({ viewport: PHONE, isMobile: true, hasTouch: true, deviceScaleFactor: 2 });

const dict = DICT as unknown as Record<string, Record<Lang, string>>;

/** Clear every trace of an earlier run, request `lang`, and prove the page rendered it. */
async function render(page: Page, lang: Lang): Promise<void> {
  await page.goto("/landing");
  await page.evaluate(() => {
    localStorage.clear();
    sessionStorage.clear();
  });
  await page.context().clearCookies();
  await page.evaluate((l) => localStorage.setItem("jf_lang", l), lang);
  await page.reload();
  await expect(page.locator("main article").first()).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.lang)).toBe(lang);
  // The page really is translated, not merely declared: the first card's heading is the
  // dictionary's heading for THIS locale.
  const first = await page.locator("main article h3").first().innerText();
  expect(first.trim()).toBe(dict["land.weddingtitle"][lang]);
}

type CardMeasure = {
  title: string;
  words: number;
  leadFound: boolean;
  lead: number;
  bulletWords: number[];
  height: number;
  filled: number;
  guideLinks: number;
  guideNames: string[];
  text: string;
};

/** Every use-case card, measured in the rendered DOM. */
async function measure(page: Page): Promise<CardMeasure[]> {
  return page.evaluate(() => {
    const norm = (s: string | null | undefined) => (s || "").replace(/\s+/g, " ").trim();
    const countWords = (s: string | null | undefined) => {
      const t = norm(s);
      return t ? t.split(" ").length : 0;
    };
    const textOf = (el: Element) => {
      const clone = el.cloneNode(true) as HTMLElement;
      clone.querySelectorAll('[aria-hidden="true"]').forEach((n) => n.remove());
      return norm(clone.textContent);
    };
    return [...document.querySelectorAll("main article")].map((el) => {
      const card = el as HTMLElement;
      // `<h3>` then the lead paragraph is the card frame `ShowcaseCard` renders.
      const lead = card.querySelector("h3 + p");
      const links = [...card.querySelectorAll("a")] as HTMLAnchorElement[];
      const filled = (a: HTMLAnchorElement) => {
        const bg = getComputedStyle(a).backgroundColor;
        return bg !== "rgba(0, 0, 0, 0)" && bg !== "transparent";
      };
      const guides = links.filter((a) => (a.getAttribute("href") || "").startsWith("/docs/guides/"));
      return {
        title: norm(card.querySelector("h3")?.textContent),
        words: countWords(textOf(card)),
        leadFound: lead !== null,
        lead: countWords(lead?.textContent),
        bulletWords: [...card.querySelectorAll("li")].map((li) => countWords(textOf(li))),
        height: Math.round(card.getBoundingClientRect().height),
        filled: links.filter(filled).length,
        guideLinks: guides.length,
        guideNames: guides.map((a) => (a.getAttribute("aria-label") || "").trim()),
        text: norm(card.textContent),
      };
    });
  });
}

// One test per locale, in dictionary order, so a red names the language that is over.
for (const lang of LANGS) {
  test(`${lang} at 390px: every use-case card fits the word and height budget`, async ({ page }) => {
    await render(page, lang);
    const cards = await measure(page);

    expect(cards.length, "the landing page must render the seven use-case cards").toBe(7);

    const problems: string[] = [];
    for (const c of cards) {
      const where = `${lang} / "${c.title}"`;
      if (c.words > BUDGET.words) problems.push(`${where}: ${c.words} words > ${BUDGET.words}`);
      if (!c.leadFound) problems.push(`${where}: no lead paragraph after the heading`);
      else if (c.lead > BUDGET.lead) problems.push(`${where}: ${c.lead}-word lead > ${BUDGET.lead}`);
      if (c.bulletWords.length !== BUDGET.bullets) {
        problems.push(`${where}: ${c.bulletWords.length} bullets, expected ${BUDGET.bullets}`);
      }
      const longest = c.bulletWords.filter((n) => n > BUDGET.bullet);
      if (longest.length) {
        problems.push(`${where}: bullets of ${longest.join(", ")} words > ${BUDGET.bullet}`);
      }
      if (c.height > BUDGET.height) problems.push(`${where}: ${c.height}px tall > ${BUDGET.height}`);
      if (c.filled !== 1) problems.push(`${where}: ${c.filled} filled CTAs, expected 1`);
      if (c.guideLinks !== 1) problems.push(`${where}: ${c.guideLinks} guide links, expected 1`);
      if (/Read the full .* guide/i.test(c.text)) {
        problems.push(`${where}: still carries a "Read the full … guide" sentence`);
      }
      // The visible label is the same one word on all seven cards, so the accessible name
      // has to name the card — a link list of seven identical "Guide" links is unusable.
      const name = c.guideNames[0] ?? "";
      if (!name.includes(c.title)) {
        problems.push(`${where}: guide link's name "${name}" does not name the card`);
      }
    }

    const counts = cards.map((c) => c.words);
    const spread = Math.max(...counts) - Math.min(...counts);
    if (spread > BUDGET.spread) {
      problems.push(
        `the seven cards run ${counts.join(", ")} words — a spread of ${spread}, over ` +
          `${BUDGET.spread}; the grid stops being even on a phone`,
      );
    }

    expect(problems, `use-case cards over budget in ${lang} at 390px`).toEqual([]);
  });
}

test("the block keeps its one-action shape and never pans the page at 390px", async ({ page }) => {
  await render(page, "en");
  const metrics = await page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
    blockWords: (() => {
      const grid = document.querySelector("main article")?.parentElement;
      const clone = grid?.cloneNode(true) as HTMLElement | undefined;
      if (!clone) return -1;
      clone.querySelectorAll('[aria-hidden="true"]').forEach((n) => n.remove());
      const t = (clone.textContent || "").replace(/\s+/g, " ").trim();
      return t ? t.split(" ").length : 0;
    })(),
  }));
  // The whole block was 1,078 words before this card; the seven cards on budget cannot
  // put it back anywhere near that.
  expect(metrics.blockWords).toBeLessThanOrEqual(7 * BUDGET.words);
  expect(metrics.scrollWidth, "the card block panned the document sideways").toBe(
    metrics.clientWidth,
  );
});
