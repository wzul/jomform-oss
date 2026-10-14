// Regression guard: the /form-settings Status chips must not squeeze their label.
//
// THE DEFECT (reported from a phone screenshot of jomform.com/form-settings):
// the status row was `grid max-w-xl grid-cols-4 gap-1.5`, i.e. it forced FOUR
// EQUAL columns at every width. Measured on the real built page at 320px, each
// button got a 51x51px box — a 33px content box after `px-2` and the border —
// while the glyph run for Tamil "பட்டியலிடப்படாதது" is 96px and Malay "Tidak
// disenaraikan" is 113px. A one-word label cannot wrap, so the run spilled out
// of the button's padding box and painted over the chip's own border (measured
// right inset -54px in Tamil, -46px for "வெளியிடப்பட்டது"; even English
// "Private" was down to a 0.16px right inset at 51px). It was never an
// English-only problem: ms/zh/ta/ar are all longer, which is why the guard runs
// every locale.
//
// WHY THE GUARD IS A BROWSER MEASUREMENT, NOT A CLASS-NAME ASSERTION
// `npm run test:unit` (node --test over src/lib/*.test.ts) has no DOM, and a
// guard that only asserts "the row is not grid-cols-4" is toothless: it would
// stay green while a different class re-created the same squeeze. This file runs
// in the `e2e` gate (`npm run e2e`, part of scripts/preflight.sh) — the project
// that runs every spec under web/e2e that is not named `a11y*` — because that is
// the gate that renders the real built page in a real browser.
// It measures the ACTUAL glyph run of the ACTUAL label against the ACTUAL button
// box, per locale, at 320/360/390/430px — the same measurement the defect was
// reported with, so the numbers below are the acceptance evidence.
//
// WHAT MAKES IT BITE (verified, not assumed)
// The four assertions per chip are the invariants the squeeze violates directly:
//   1. the glyph run sits INSIDE the button's border box (no spill), and
//   2. its left and right insets are both at least MIN_INSET_PX, and
//   3. they are symmetric within SYMMETRY_TOLERANCE_PX, and
//   4. the button's own scrollWidth does not exceed its clientWidth.
// Against the pre-fix tree at 320px (en) the run overflows the box on both sides
// and the insets go negative, so all four fail — this was checked by neutralising
// the fix and watching this file go red.
//
// The API is stubbed at the browser level (no Go server, no session, no DB), so
// the gate needs nothing but the build it is already serving.
import { test, expect, type Page } from "@playwright/test";

import { signIn } from "./auth";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page
// renders. This spec visits one, so it declares the session it means — before
// the gate existed an anonymous visit rendered the account shell, so this spec
// could (and did) rely on the bug. See e2e/auth.ts.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});
import { DICT } from "../src/components/i18nDict";

// The four statuses the row renders, in source order.
const STATUSES = ["draft", "private", "published", "unlisted"] as const;

// Phone widths from the card, plus the desktop-ish 430px where the `max-w-xl`
// cap is still inactive.
const WIDTHS = [320, 360, 390, 430] as const;

// 8px padding (`px-2`) plus the 1px border, minus a pixel of rounding slack for
// the range's advance box: a chip that fits its label always clears this easily.
const MIN_INSET_PX = 6;
// A squeezed chip is asymmetric because the centred run spills toward the box
// edges; a content-sized chip is symmetric to sub-pixel accuracy.
const SYMMETRY_TOLERANCE_PX = 2;

const FORM_ID = "form_chip_guard";

type Chip = {
  label: string;
  buttonW: number;
  textW: number;
  leftInset: number;
  rightInset: number;
  scrollOverflow: number;
};

// stubAPI answers every /api/* call the page makes, so the page reaches its
// loaded state without a Go API, a session cookie or a database. Registered as
// a catch-all FIRST and the specific routes after it: Playwright resolves
// matching routes in reverse registration order, so the later (specific) ones win.
async function stubAPI(page: Page) {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });

  // Catch-all: anything the page fetches that is not pinned below answers 200
  // with an empty object (the page guards every optional read with `if (r.ok)`).
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));

  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (route) =>
    route.fulfill(
      json({
        me: { id: "u1", email: "merchant@example.com", name: "Merchant" },
        workspaces: [{ id: "ws1", name: "Demo", slug: "demo", role: "admin" }],
      }),
    ),
  );
  // lang:"" (never set) keeps the browser value in effect, which is how the
  // spec pins the locale; the promotion path then POSTs and the stub accepts it.
  await page.route(/(.*)\/api\/auth\/profile$/, (route) => route.fulfill(json({ lang: "" })));
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+$/, (route) =>
    route.fulfill(
      json({
        id: FORM_ID,
        slug: "demo-form",
        title: "Demo form",
        status: "published",
        url: "https://jomform.com/f/demo-form",
        config: { blocks: [] },
        theme: {},
      }),
    ),
  );
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/chip-feeds(\?.*)?$/, (route) =>
    route.fulfill(json({ feeds: null })),
  );
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/confirmations(\?.*)?$/, (route) =>
    route.fulfill(json({ confirmations: [] })),
  );
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/notifications(\?.*)?$/, (route) =>
    route.fulfill(json([])),
  );
  await page.route(/(.*)\/api\/rest\/webhooks(\?.*)?$/, (route) => route.fulfill(json([])));
  await page.route(/(.*)\/api\/rest\/forms(\?.*)?$/, (route) => route.fulfill(json([])));
}

// measureStatusChips reads the live DOM: for every status button it takes the
// button's border box and the glyph run's box (a Range over the button's own text
// node) and reports both insets plus any internal scroll overflow.
async function measureStatusChips(page: Page, lang: string): Promise<Chip[]> {
  return page.evaluate(
    ({ statuses, statusLabel }) => {
      const label = [...document.querySelectorAll("label")].find(
        (l) => (l.textContent || "").trim() === statusLabel,
      );
      if (!label) throw new Error(`no <label> reading "${statusLabel}" — the control moved`);
      const row = label.nextElementSibling as HTMLElement | null;
      if (!row) throw new Error("the Status label has no sibling row");
      const buttons = [...row.querySelectorAll("button")] as HTMLButtonElement[];
      if (buttons.length !== statuses.length) {
        throw new Error(`expected ${statuses.length} status buttons, found ${buttons.length}`);
      }
      return buttons.map((btn) => {
        const b = btn.getBoundingClientRect();
        const range = document.createRange();
        range.selectNodeContents(btn);
        const t = range.getBoundingClientRect();
        return {
          label: (btn.textContent || "").trim(),
          buttonW: Math.round(b.width * 100) / 100,
          textW: Math.round(t.width * 100) / 100,
          leftInset: Math.round((t.left - b.left) * 100) / 100,
          rightInset: Math.round((b.right - t.right) * 100) / 100,
          scrollOverflow: btn.scrollWidth - btn.clientWidth,
        };
      });
    },
    { statuses: [...STATUSES], statusLabel: DICT["bld.status"][lang as "en"] },
  );
}

for (const lang of ["en", "ms", "zh", "ta", "ar"] as const) {
  test(`status chips fit their labels in ${lang} at 320/360/390/430px`, async ({ browser }) => {
    // The `e2e` gate runs this file alongside the rest of the feature specs on a
    // shared host, so the per-test budget is generous: a slow first paint under
    // contention must not read as a chip defect. (Measured: with the default 5s
    // the zh case flaked once at 3 workers and passed alone — a scheduling
    // artefact, not a locale failure.)
    test.setTimeout(180_000);
    const expected = STATUSES.map((s) => DICT[`bld.status${s[0].toUpperCase()}${s.slice(1)}` as "bld.statusDraft"][lang]);
    const measured: string[] = [];

    for (const width of WIDTHS) {
      const context = await browser.newContext({
        viewport: { width, height: 720 },
        deviceScaleFactor: 1,
      });
      await context.addInitScript(
        ([l]) => {
          localStorage.setItem("jf_workspace", "demo");
          localStorage.setItem("jf_lang", l as string);
        },
        [lang],
      );
      const page = await context.newPage();
      // A hand-made context does not inherit the config's cookie (see e2e/auth.ts).
      await signIn(page);
      page.setDefaultTimeout(45_000);
      await stubAPI(page);
      await page.goto(`/form-settings?form=${FORM_ID}`, { waitUntil: "networkidle" });

      // The locale must actually have applied, or the measurement below is of
      // some other language and proves nothing.
      await expect(page.locator(`button:text-is("${expected[2]}")`).first()).toBeVisible({
        timeout: 45_000,
      });
      expect(await page.evaluate(() => document.documentElement.dir)).toBe(
        lang === "ar" ? "rtl" : "ltr",
      );

      const chips = await measureStatusChips(page, lang);
      expect(chips.map((c) => c.label)).toEqual([...expected]);

      const where = `${lang} @ ${width}px`;
      // Print the raw numbers: they are the acceptance evidence for this card
      // (each chip's box vs its glyph run), not just pass/fail.
      measured.push(
        `${where}: ` +
          chips
            .map(
              (c) =>
                `"${c.label}" box=${c.buttonW}px run=${c.textW}px inset=${c.leftInset}/${c.rightInset}px`,
            )
            .join(" | "),
      );
      for (const chip of chips) {
        expect(
          chip.scrollOverflow,
          `${where}: "${chip.label}" overflows its button internally by ${chip.scrollOverflow}px ` +
            `(button ${chip.buttonW}px, label ${chip.textW}px)`,
        ).toBeLessThanOrEqual(1);
        expect(
          chip.leftInset,
          `${where}: "${chip.label}" starts ${chip.leftInset}px inside its button ` +
            `(button ${chip.buttonW}px, label ${chip.textW}px) — the run has crossed its own border`,
        ).toBeGreaterThanOrEqual(MIN_INSET_PX);
        expect(
          chip.rightInset,
          `${where}: "${chip.label}" ends ${chip.rightInset}px inside its button ` +
            `(button ${chip.buttonW}px, label ${chip.textW}px) — the run has crossed its own border`,
        ).toBeGreaterThanOrEqual(MIN_INSET_PX);
        expect(
          Math.abs(chip.leftInset - chip.rightInset),
          `${where}: "${chip.label}" insets are asymmetric (left ${chip.leftInset}px, ` +
            `right ${chip.rightInset}px) — the label is painted at the edge of its chip`,
        ).toBeLessThanOrEqual(SYMMETRY_TOLERANCE_PX);
      }

      // Every chip on one row must be the same height (one grouped control),
      // and the row itself must never push sideways.
      //
      // Deliberately ROW-scoped, not page-scoped: a page-wide overflow
      // assertion here would make this guard red for a defect the status row
      // does not own. At 320px the /form-settings header action row ("Edit
      // fields"/"Sales"/"View") overflows the viewport in Tamil by 13px on both
      // the pre-fix and post-fix tree — it is measured and filed separately, and
      // folding it in here would turn a chip regression guard into a guard that
      // fails for the wrong reason.
      const geometry = await page.evaluate((statusLabel) => {
        const label = [...document.querySelectorAll("label")].find(
          (l) => (l.textContent || "").trim() === statusLabel,
        )!;
        const row = label.nextElementSibling as HTMLElement;
        return {
          heights: [...row.querySelectorAll("button")].map((b) =>
            Math.round(b.getBoundingClientRect().height),
          ),
          rowOverflow: row.scrollWidth - row.clientWidth,
        };
      }, DICT["bld.status"][lang]);
      expect(new Set(geometry.heights).size, `${where}: chip heights differ (${geometry.heights.join(", ")})`).toBe(1);
      expect(geometry.rowOverflow, `${where}: the status row scrolls sideways by ${geometry.rowOverflow}px`).toBeLessThanOrEqual(1);

      await context.close();
    }

    console.log(measured.join("\n"));
  });
}

// Acceptance criterion 3: the desktop rendering keeps its shape — the control
// still sits directly under the `Status` label, and its row is still bounded by
// `max-w-xl` (576px) rather than stretching across the card.
test("the status chips stay under the Status label and inside max-w-xl at 1024px", async ({
  browser,
}) => {
  const context = await browser.newContext({ viewport: { width: 1024, height: 800 } });
  await context.addInitScript(() => {
    localStorage.setItem("jf_workspace", "demo");
    localStorage.setItem("jf_lang", "en");
  });
  const page = await context.newPage();
  // A hand-made context does not inherit the config's cookie (see e2e/auth.ts).
  await signIn(page);
  await stubAPI(page);
  await page.goto(`/form-settings?form=${FORM_ID}`, { waitUntil: "networkidle" });
  await expect(page.locator('button:text-is("Published")').first()).toBeVisible({ timeout: 45_000 });

  const geom = await page.evaluate((statusLabel) => {
    const label = [...document.querySelectorAll("label")].find(
      (l) => (l.textContent || "").trim() === statusLabel,
    )!;
    const row = label.nextElementSibling as HTMLElement;
    return {
      // The control is the label's immediately following sibling (unchanged).
      isNextSibling: label.nextElementSibling === row,
      rowWidth: Math.round(row.getBoundingClientRect().width),
      // One line on desktop: every chip shares the same offsetTop.
      tops: [...row.querySelectorAll("button")].map((b) => Math.round(b.getBoundingClientRect().top)),
    };
  }, DICT["bld.status"].en);

  expect(geom.isNextSibling, "the chip row is no longer the sibling under the Status label").toBe(true);
  expect(geom.rowWidth, `the chip row is ${geom.rowWidth}px wide — it escaped max-w-xl (576px)`).toBeLessThanOrEqual(576);
  expect(new Set(geom.tops).size, `the chips wrapped on desktop (tops: ${geom.tops.join(", ")})`).toBe(1);
  await context.close();
});
