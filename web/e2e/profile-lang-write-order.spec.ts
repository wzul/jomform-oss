// Regression guard for card t_04b728a9 — two language picks made in quick
// succession through the real /profile picker could settle the PROFILE on the
// OLDER one, because the two POSTs were fired unordered.
//
// THE DEFECT
//   `setLang()` in `web/src/components/i18n.tsx` POSTs the choice to
//   `/api/auth/profile/lang` on every pick and nothing ordered them. Two picks
//   inside a few hundred milliseconds are two requests in flight at once, and a
//   concurrent pair has no defined arrival order at the server: if the FIRST is
//   slower (two HTTP/2 streams multiplexed in either order, a second connection,
//   a slow first request), the server COMMITS the older choice last. The page
//   shows the newer language, so nothing looks wrong until the next cold load —
//   or another browser, which is the whole point of a per-profile setting —
//   reads the profile back and shows the older one. Measured: "#1 sent ms | #2
//   sent zh | #2 committed zh | #2 committed ms"; the server ended on `ms` while
//   the page showed `zh`, and a reload showed `ms`.
//
// WHY THIS IS A BROWSER MEASUREMENT
//   The claim is about the ORDER two requests reach the server in, not about a
//   line of source. `src/lib/langPlacement.test.ts` executes the queue in
//   isolation and pins the provider to it from source; only this spec drives the
//   REAL picker against a route that can hold the first POST, which is the
//   standard way to give a request-ordering bug a deterministic shape instead of
//   a flake. The "server" here is the route handler: it records every request in
//   the order it COMMITS, so `state.server` is exactly what the profile would
//   hold afterwards.
//
// THE ARMS
//   1. THE DEFECT: the first POST is held open, the user picks `ms` and then
//      `zh` inside that window, then the first is released. With the write
//      ordered the second POST is not even SENT until the first settles, so the
//      server ends on `zh`; unordered it ends on `ms` while the page shows `zh`.
//      The arm also models the SECOND BROWSER by clearing the `jf_lang` mirror
//      before reloading, so only the profile's value can decide what the picker
//      shows — that is the user-visible symptom the card is about.
//   2. THE CONTROL: the same two picks with nothing held must also leave the
//      profile on the last choice. This passes on the pre-fix page too (the
//      requests really are ordered when nothing delays them), so it is what
//      stops a "fix" that simply stops writing.
//   3. A single pick is still written AT ONCE — a queue that only flushes on the
//      second call, or that defers the first write indefinitely, would pass
//      nothing but would look green on arm 1's committed count alone.
import { test, expect, type Page } from "@playwright/test";

import { signIn } from "./auth";

const ME = {
  id: "u1",
  email: "merchant@example.com",
  name: "Merchant",
  lang: "",
  avatar_url: "",
  phone: "",
  timezone: "",
  totp_enabled: false,
  is_super_admin: false,
};

// Enough session for /profile to render its tabs (the identity fields hydrate
// from this bundle, so the page needs no per-mount profile round-trip).
const BUNDLE = {
  me: ME,
  workspaces: [{ id: "ws1", name: "Demo", slug: "demo", role: "admin" }],
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

const PICKER = "#prof-lang";

const json = (body: unknown) => ({ status: 200, contentType: "application/json", body: JSON.stringify(body) });

type Server = {
  /** The language the PROFILE holds — updated on commit, in arrival order. */
  server: string;
  /** Every request that reached the route, and when it committed. */
  arrived: number;
  committed: number;
  order: string[];
  inFlight: string[];
  maxInFlight: number;
};

/**
 * Stub the dashboard API and give `/api/auth/profile/lang` a "server" that
 * records commits in the order they really happen.
 *
 * With `holdFirstPost`, the FIRST language POST is held open (the route handler
 * awaits a promise) while later ones are answered immediately — the shape that
 * reorders two concurrent writes. `release()` lets the held one settle.
 */
async function stubAPI(page: Page, opts: { holdFirstPost: boolean }) {
  const state: Server = { server: "en", arrived: 0, committed: 0, order: [], inFlight: [], maxInFlight: 0 };

  let release!: () => void;
  const released = new Promise<void>((resolve) => {
    release = resolve;
  });
  let firstArrivedResolve!: () => void;
  const firstArrived = new Promise<void>((resolve) => {
    firstArrivedResolve = resolve;
  });
  if (!opts.holdFirstPost) release();

  // Catch-all FIRST, specific routes after it: Playwright resolves matching
  // routes in reverse registration order, so the later (specific) ones win.
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(ME)));
  await page.route(/(.*)\/api\/auth\/(passkeys|social|mcp-agents|trust-devices)(\?.*)?$/, (r) => r.fulfill(json([])));
  await page.route(/(.*)\/api\/auth\/trust-status(\?.*)?$/, (r) => r.fulfill(json({ trusted: false })));
  // The profile read answers the value the "server" holds AT THE MOMENT IT IS
  // SERVED — so after the writes have committed, a reload sees the truth.
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: state.server })));

  await page.route(/(.*)\/api\/auth\/profile\/lang$/, async (route) => {
    const lang = String(JSON.parse(route.request().postData() || "{}").lang ?? "");
    const n = ++state.arrived;
    state.inFlight.push(lang);
    state.maxInFlight = Math.max(state.maxInFlight, state.inFlight.length);
    if (n === 1) {
      firstArrivedResolve();
      await released; // the slow FIRST request
    }
    state.inFlight.splice(state.inFlight.indexOf(lang), 1);
    state.server = lang; // committed
    state.committed += 1;
    state.order.push(`#${n} committed ${lang}`);
    await route.fulfill(json({}));
  });

  return { state, firstArrived, release };
}

/** A fresh browser state per case: a `jf_lang` (or a session) left behind by an
 *  earlier card would render a state no new visitor has, and every assertion
 *  here compares against the locale the picker really is in. */
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      sessionStorage.clear();
    } catch {
      /* storage unavailable */
    }
  });
});

/** The live `<select>` value. */
async function pickerValue(page: Page): Promise<string> {
  return page.evaluate((sel) => {
    const el = document.querySelector(sel) as HTMLSelectElement | null;
    return el ? el.value : "<no picker>";
  }, PICKER);
}

/**
 * The page is STATICALLY PRERENDERED, so the served HTML already carries the
 * picker at its default value; only after HYDRATION does changing it reach the
 * i18n context. React leaves `__reactFiber$…`/`__reactProps$…` on a node it has
 * taken over, which is the deterministic "this node is hydrated" signal (no
 * sleep), and the same marker `e2e/profile-lang-picker-race.spec.ts` waits on.
 */
async function waitForHydratedPicker(page: Page) {
  await page.waitForFunction(
    (sel) => {
      const el = document.querySelector(sel);
      return !!el && Object.keys(el).some((k) => k.startsWith("__reactFiber$") || k.startsWith("__reactProps$"));
    },
    PICKER,
    { timeout: 20_000 },
  );
}

/** Reload as a SECOND BROWSER would: the mirror is gone, so only the profile's
 *  language can decide what the picker shows. */
async function reloadWithoutMirror(page: Page) {
  await page.evaluate(() => {
    try {
      localStorage.clear();
    } catch {
      /* storage unavailable */
    }
  });
  const read = page.waitForResponse(
    (r) => /\/api\/auth\/profile$/.test(r.url()) && r.request().method() === "GET",
    { timeout: 20_000 },
  );
  await page.reload({ waitUntil: "domcontentloaded" });
  await read;
  await waitForHydratedPicker(page);
  // The provider's `.then` is a microtask chain behind that response; let it
  // commit (and give a wrong answer time to be painted) before reading.
  await page.waitForTimeout(400);
}

test("two rapid picks leave the PROFILE on the last choice, not the slower first one", async ({ page }) => {
  const { state, firstArrived, release } = await stubAPI(page, { holdFirstPost: true });

  await page.goto("/profile", { waitUntil: "domcontentloaded" });
  await waitForHydratedPicker(page);
  expect(await pickerValue(page), "the picker should start on the profile's value").toBe("en");

  // The user picks, then changes their mind while the first write is in flight.
  await page.selectOption(PICKER, "ms");
  await firstArrived; // the first POST has reached the route and is held open
  await page.selectOption(PICKER, "zh");
  await page.waitForTimeout(200); // long enough for an unordered second POST to go out

  // The page moved at once — that half always worked, and it is asserted so a
  // regression in the WRITE cannot hide behind the ordering.
  expect(await pickerValue(page), "the picker did not apply the second pick").toBe("zh");

  // Deliver the slow first write; the queued second one follows it.
  release();
  await expect
    .poll(() => state.committed, { timeout: 10_000, message: "the second pick was never written to the profile" })
    .toBe(2);

  console.log(`PROBE order: ${state.order.join(" | ")}`);
  console.log(`PROBE server ended on: ${state.server}; the page shows: ${await pickerValue(page)}`);

  // THE DEFECT: unordered, this reads `ms` — the page shows `zh` and the
  // profile kept the choice the user had already replaced.
  expect(state.server, "the profile settled on the OLDER choice; the page was showing the newer one").toBe("zh");
  // …because the second write was never in flight beside the first.
  expect(
    state.maxInFlight,
    "two language POSTs were in flight at once — their arrival order at the server is undefined",
  ).toBe(1);
  expect(await pickerValue(page), "a late write reverted the picker").toBe("zh");

  // THE USER-VISIBLE SYMPTOM: the next load reads the profile back. A second
  // browser is modelled by dropping the `jf_lang` mirror first.
  await reloadWithoutMirror(page);
  expect(
    await pickerValue(page),
    "the next cold load shows the older choice — the profile holds the wrong language",
  ).toBe("zh");
});

test("control: the same two picks with nothing held also leave the profile on the last choice", async ({ page }) => {
  const { state } = await stubAPI(page, { holdFirstPost: false });

  await page.goto("/profile", { waitUntil: "domcontentloaded" });
  await waitForHydratedPicker(page);
  expect(await pickerValue(page)).toBe("en");

  await page.selectOption(PICKER, "ms");
  await page.selectOption(PICKER, "zh");
  await expect.poll(() => state.committed, { timeout: 10_000 }).toBe(2);

  // Passes on the pre-fix page as well (nothing delays the first request): it is
  // what stops a "fix" that keeps the profile still by never writing at all.
  expect(state.server, "the profile does not hold the last choice").toBe("zh");
  expect(await pickerValue(page)).toBe("zh");

  await reloadWithoutMirror(page);
  expect(await pickerValue(page), "the reload does not show the last choice").toBe("zh");
});

test("a single pick is written at once, not deferred behind a queue that never flushes", async ({ page }) => {
  const { state } = await stubAPI(page, { holdFirstPost: false });

  await page.goto("/profile", { waitUntil: "domcontentloaded" });
  await waitForHydratedPicker(page);

  await page.selectOption(PICKER, "ta");
  await expect.poll(() => state.committed, { timeout: 5_000, message: "a lone pick was never written" }).toBe(1);
  expect(state.server).toBe("ta");
  expect(state.order).toEqual(["#1 committed ta"]);
});
