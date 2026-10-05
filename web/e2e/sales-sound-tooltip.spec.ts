// The /sales realtime-sound switch keeps its hover tooltip, and the tooltip is
// LOAD-DEPENDENT (card t_5ca04dc0, follow-up to PR #987 / card t_b73eee21).
//
// THE DEFECT, MEASURED ON THE BUILT PAGE
//   The hand-rolled pill on /sales carried a `title` — the hover tooltip a mouse
//   user sees — and the conversion to the shared `<Switch>` component dropped it:
//   the component had no `title` prop at all. `label` becomes `aria-label`, which
//   is the ACCESSIBLE NAME a screen reader announces, and is NOT a tooltip. The
//   QA lane measured the same probe against two builds (fresh localStorage, API
//   stubbed at the browser level, /sales):
//
//     BEFORE (origin/main 5f212517): aria-label="Play a beep when a new order
//                                    arrives" title="Play a beep when a new order
//                                    arrives" hasTitleAttr=true
//     AFTER  (head e432f33a):        aria-label="Play a beep when a new order
//                                    arrives" title=null hasTitleAttr=false
//
//   The tooltip is not decoration: it is the ONLY place the LOADING copy was
//   visible. Before the workspace's stored value has been read the switch is
//   DISABLED and its markup says `t("sales.soundLoading")`; a sighted mouse user
//   hovering it is the one who needs that hint. So this spec measures the title
//   TWICE — while the settings read is held open, and after it resolves — and the
//   two readings must differ. A guard that only asserted "there is a title" would
//   pass on a build that hard-coded the loaded copy and lost the loading hint.
//
// WHY A BROWSER MEASUREMENT AND NOT ONLY THE UNIT GUARD
//   `src/lib/switchControl.test.ts` pins the source: the component forwards a
//   `title`, and /sales passes the load-dependent copy. What a source read cannot
//   do is prove the attribute reaches the DOM, is bound to the LIVE `soundLoaded`
//   state, and is absent before the read resolves — that is what a mis-wired
//   call site looks like, and only the rendered page answers it. This file is
//   also the NON-VACUITY control for the whole card: run against the pre-fix
//   build it FAILS on phase 1 (`title` is `null` while the read is held), which is
//   the exact reading the QA lane took.
//
// The API is stubbed at the browser level (no Go server, no session, no DB), so
// the gate needs nothing but the build it is already serving — the same technique
// e2e/a11y-switcher.spec.ts and e2e/affiliate-row.spec.ts use.
import { test, expect, type Page } from "@playwright/test";
import { DICT } from "../src/components/i18nDict";

/** The two copies under test, read from the dictionary so a reword fails here. */
const LOADED = DICT["sales.soundLabel"].en as string;
const LOADING = DICT["sales.soundLoading"].en as string;

/** Two workspaces, so the id↔slug resolution the sound read needs has a real map. */
const BUNDLE = {
  me: { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "", is_super_admin: true },
  workspaces: [
    { id: "ws1", name: "Demo", slug: "demo", role: "admin" },
    { id: "ws2", name: "Second", slug: "second", role: "admin" },
  ],
  chip_status: null,
  keys: [],
  domains: [],
  webhooks: [],
  social: [],
  einvoice: null,
  products: [],
  summary: { counts: {}, revenue_sen: 0 },
  sales: { items: [], page: 1, limit: 20, total: 0 },
  form_analytics: [],
  product_analytics: [],
};

/**
 * Every /api/* call the page makes, stubbed at the browser level. The
 * workspace-settings read is deliberately NOT resolved by this helper — the
 * caller holds it open (phase 1) and resolves it (phase 2), because the whole
 * subject of this card is what the switch renders while that read is in flight.
 */
async function stubAPI(page: Page) {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  // Catch-all FIRST, specific routes AFTER it: Playwright resolves matching
  // routes in reverse registration order, so the later (specific) ones win.
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(BUNDLE.me)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "en" })));
  await page.route(/(.*)\/api\/rest\/sales(\?.*)?$/, (r) =>
    r.fulfill(json({ items: [], page: 1, limit: 20, total: 0 })),
  );
  await page.route(/(.*)\/api\/rest\/forms(\?.*)?$/, (r) => r.fulfill(json([])));
  await page.route(/(.*)\/api\/rest\/currency\/rates$/, (r) =>
    r.fulfill(json({ rates: [], cached: false })),
  );
}

/**
 * A FRESH browser state, as e2e/a11y-switcher.spec.ts does it: the browser keeps
 * localStorage between runs, and a stale `jf_lang` (an earlier card left `ar` in
 * the shared profile once) or a `jf_workspace` naming a workspace this file never
 * described would render a different page. English and `demo` are what a new
 * visitor gets and what the copies above are written for.
 */
async function freshState(page: Page) {
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      localStorage.setItem("jf_workspace", "demo");
      localStorage.setItem("jf_lang", "en");
    } catch {
      /* storage unavailable */
    }
  });
}

/** The sound switch: the one shared component on /sales carrying `.jf-switch`. */
const SWITCH = 'button.jf-switch[role="switch"]';

async function readSwitch(page: Page) {
  const sw = page.locator(SWITCH).first();
  await sw.waitFor({ state: "visible", timeout: 30_000 });
  return {
    title: await sw.getAttribute("title"),
    ariaLabel: await sw.getAttribute("aria-label"),
    disabled: await sw.isDisabled(),
  };
}

test.describe("/sales realtime-sound switch tooltip", () => {
  test("the tooltip is the LOADING copy while the stored read is held, then the loaded copy", async ({
    page,
  }) => {
    await stubAPI(page);
    await freshState(page);

    // Hold the workspace-settings read open so the switch is measured in the
    // state it renders BEFORE the stored value is known — the state the loading
    // copy exists for.
    let release!: () => void;
    const held = new Promise<void>((r) => {
      release = r;
    });
    await page.route(/(.*)\/api\/workspaces\/[^/]+\/settings(\?.*)?$/, async (route) => {
      await held;
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ sound_enabled: true }),
      });
    });

    await page.goto("/sales", { waitUntil: "domcontentloaded" });

    // ── PHASE 1: the read is in flight ────────────────────────────────────────
    const before = await readSwitch(page);
    // The switch is DISABLED until the stored value is read (issue #447) — this
    // is the state the tooltip is for, so assert it rather than assume it.
    expect(before.disabled, "the sound switch was not disabled while the read is held").toBe(true);
    expect(
      before.title,
      `while the workspace's stored sound setting was still being read, the switch's title was ` +
        `${JSON.stringify(before.title)} — the loading hint a mouse user hovering the disabled ` +
        `switch needs is MISSING (this is the pre-fix reading: title=null)`,
    ).toBe(LOADING);
    // `label` is the accessible NAME, not a tooltip: it switches with the same
    // condition, and pinning it here is what keeps the two from being conflated.
    expect(before.ariaLabel, "the switch's accessible name is not the loading copy").toBe(LOADING);

    // ── PHASE 2: the read resolves ──────────────────────────────────────────
    release();
    const sw = page.locator(SWITCH).first();
    await expect(sw).not.toBeDisabled({ timeout: 10_000 });
    const after = await readSwitch(page);
    expect(
      after.title,
      `after the stored sound setting resolved, the switch's title was ` +
        `${JSON.stringify(after.title)} — expected the loaded copy ${JSON.stringify(LOADED)}`,
    ).toBe(LOADED);
    expect(after.ariaLabel, "the switch's accessible name is not the loaded copy").toBe(LOADED);

    // THE POINT OF THE CARD: the tooltip is LOAD-DEPENDENT, so the two readings
    // must differ. A build that hard-coded one copy would pass each `toBe` above
    // and still have lost the loading hint — this is the assertion that says so.
    expect(
      before.title,
      "the title did not change between the held read and the resolved read — the tooltip is " +
        "no longer load-dependent, so the loading hint is not actually conveyed",
    ).not.toBe(after.title);
    expect(before.title).toBe(LOADING);
    expect(after.title).toBe(LOADED);
  });

  test("CONTROL: the probe reads absence, so a build without the tooltip cannot pass", async ({
    page,
  }) => {
    // The non-vacuity arm. If the probe could not tell "no title" from "a title"
    // — e.g. it fell back to the accessible name — its green reading above would
    // prove nothing. Strip the attribute the fix adds and the probe must report
    // null, which is exactly the pre-fix measurement (`hasTitleAttr=false`).
    await stubAPI(page);
    await freshState(page);
    await page.route(/(.*)\/api\/workspaces\/[^/]+\/settings(\?.*)?$/, (r) =>
      r.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ sound_enabled: false }),
      }),
    );
    await page.goto("/sales", { waitUntil: "domcontentloaded" });

    const sw = page.locator(SWITCH).first();
    await sw.waitFor({ state: "visible", timeout: 30_000 });
    await expect(sw).not.toBeDisabled({ timeout: 10_000 });
    expect(await sw.getAttribute("title"), "the fixed build renders no title at all").toBe(LOADED);

    // Simulate the pre-fix markup: the attribute is gone.
    await page.evaluate(() => {
      document.querySelector('button.jf-switch[role="switch"]')?.removeAttribute("title");
    });
    expect(
      await sw.getAttribute("title"),
      "the probe reported a title for an element with no title attribute — it is not reading " +
        "the tooltip, so its green reading above is not evidence",
    ).toBeNull();
    // And the accessible name is still there, which is why `label` alone could
    // never stand in for the tooltip.
    expect(await sw.getAttribute("aria-label")).toBe(LOADED);
  });
});
