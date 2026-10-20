// Regression guard for card t_3c0cc8ce — Settings → Domains painted the FALSE
// "Cloudflare auto-setup is not configured on this deployment — add the CNAME
// record manually." sentence for ~0.36s on every cold load, before
// `/api/cf/status` had answered.
//
// THE DEFECT
//   `settings/page.tsx` initialised the Cloudflare status state to the NEGATIVE
//   answer (`cfConfigured = false`, `cfLinked = false`) and only replaced it when
//   `/api/cf/status` resolved. So a deployment where Cloudflare auto-setup IS
//   configured rendered, for the fetch's whole duration, an instruction telling
//   the merchant the deployment is broken and to add the CNAME by hand — while
//   the section above it (which reads `cfLinked`) offered no hint at all and the
//   same panel offered "Log in with Cloudflare" to an account that was already
//   linked. Measured by QA on prod v1.21.252, MutationObserver on the section,
//   `performance.now()` since navigation start: t=661ms `not-conf`, t=1019ms
//   `connected` — ~358ms of a false statement, and the two halves of the panel
//   disagreeing while the fetch was in flight.
//
// WHAT THIS SPEC PINS, AND WHY IT IS A BROWSER MEASUREMENT
//   The claim is "the sentence is not in the DOM at ANY point during the load",
//   which is a property of the rendered page over TIME. A source assertion
//   cannot see it (the guard could be present and still order the render wrong),
//   and a single snapshot cannot either — a snapshot after the fetch resolves
//   passes on the broken code. So the fetch is HELD OPEN at the browser level
//   and the DOM is read while it is pending, with a MutationObserver recording
//   every distinct panel state from the first paint.
//
//   Holding the request is what makes the window deterministic instead of a
//   race: on a fast local API the pre-fix window is a few milliseconds and a
//   timing-based spec would be flaky in exactly the way that teaches a lane to
//   mute the gate. Here the "before" state is stable for as long as the test
//   wants it, and the "after" state only exists once the test releases the
//   response.
//
// THE FOUR ARMS
//   1. configured + linked (the prod shape): while the fetch is pending the
//      panel paints NEITHER the negative sentence NOR a login affordance; after
//      release it paints exactly ONE state, `connected`.
//   2. configured:false (a deployment with no client id): the sentence still
//      appears once the answer is known — the guard suppresses the PRE-FETCH
//      paint, not the honest negative answer.
//   3. a failed fetch: the panel settles instead of hanging in a loading state.
//   4. no login/Disconnect swap: `cfLinked` is gated on the same answer, so an
//      already-linked account is never offered "Log in with Cloudflare".
//
// The API is stubbed at the browser level (the pattern `e2e/a11y-dialogs.spec.ts`
// and `e2e/a11y-settings-first-run-tab.spec.ts` use): no Go server, no DB — the
// real client component is driven in a real browser, and `/api/cf/status` is the
// one response this spec controls, because it is the one the defect is about.
import { test, expect, type Page } from "@playwright/test";

import { DICT } from "../src/components/i18nDict";
import { signIn } from "./auth";

const CF_TITLE = DICT["set.cfTitle"].en;
const CONNECTED = DICT["set.cfConnected"].en;
const LOGIN = DICT["set.cfLogin"].en;
const DISCONNECT = DICT["set.cfDisconnect"].en;
const NOT_CONFIGURED = DICT["set.cfNotConfigured"].en;

const ME = { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "", is_super_admin: false };

/** A `manage` member with one domain-less workspace: enough for the Domains tab. */
const BUNDLE = {
  me: ME,
  workspaces: [{ id: "ws1", name: "Demo", slug: "demo", role: "manage" }],
  chip_status: null, keys: [], domains: [], webhooks: [], social: [], einvoice: null,
  products: [], summary: { counts: {}, revenue_sen: 0 },
  sales: { items: [], page: 1, limit: 20, total: 0 },
  form_analytics: [], product_analytics: [],
};

async function stubAPI(page: Page) {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  // Catch-all FIRST, specific routes after it: Playwright resolves matching
  // routes in reverse registration order, so the later (specific) ones win.
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(ME)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "" })));
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/affiliates(\?.*)?$/, (r) => r.fulfill(json([])));
  await page.route(/(.*)\/api\/rest\/currency\/rates$/, (r) => r.fulfill(json({ rates: [], cached: false })));
}

/**
 * Install the panel-state recorder BEFORE the app's first paint.
 *
 * `record()` classifies the Domains panel by what is in the DOM, so the
 * assertion is about the rendered page rather than about a React variable:
 *
 *   "not-conf"  the false "auto-setup is not configured" sentence is on screen
 *   "connected" the "Cloudflare connected ✓" badge is on screen
 *   "login"     the `/api/cf/start` link is on screen (the unlinked branch)
 *   "loading"   the section is painted with neither answer (the guarded state)
 *
 * The login state is read from the ANCHOR, not from its words: `set.cfHelp`
 * opens with "Log in with Cloudflare and JomForm auto-creates…", so the phrase
 * is on screen in every state and a text test would report `login` forever.
 *
 * `null` means the settings page is not painted yet, so the recorder has nothing
 * to say. Only TRANSITIONS are kept (a repeated state is not a new transition),
 * which is what lets the spec demand exactly one transition per load.
 */
async function installRecorder(page: Page) {
  await page.addInitScript(
    ({ title, notConf, connected }) => {
      const w = window as unknown as { __cfStates?: string[] };
      w.__cfStates = [];
      const read = (): string | null => {
        const body = document.body;
        if (!body) return null;
        const text = body.innerText || "";
        if (!text.includes(title)) return null; // settings page not painted yet
        if (text.includes(notConf)) return "not-conf";
        if (text.includes(connected)) return "connected";
        if (document.querySelector('a[href="/api/cf/start"]')) return "login";
        return "loading";
      };
      const record = () => {
        const state = read();
        if (state === null) return;
        const states = w.__cfStates!;
        if (states[states.length - 1] !== state) states.push(state);
      };
      const start = () => {
        new MutationObserver(record).observe(document.documentElement, {
          childList: true,
          subtree: true,
          characterData: true,
        });
        record();
      };
      if (document.documentElement) start();
      else document.addEventListener("DOMContentLoaded", start);
    },
    { title: CF_TITLE, notConf: NOT_CONFIGURED, connected: CONNECTED },
  );
}

const states = (page: Page) => page.evaluate(() => ((window as unknown as { __cfStates?: string[] }).__cfStates ?? []) as string[]);
const bodyText = (page: Page) => page.evaluate(() => document.body.innerText);

/**
 * Hold `/api/cf/status` open until the test releases it, so the pre-fetch window
 * is deterministic. Returns `release` and a promise that resolves once the
 * request has actually arrived (the spec never reads the DOM before that, or it
 * would be measuring a page whose fetch had not started).
 */
async function holdCfStatus(page: Page) {
  let release!: () => void;
  const released = new Promise<void>((resolve) => {
    release = resolve;
  });
  const arrived = page.waitForRequest(/\/api\/cf\/status(\?.*)?$/, { timeout: 20_000 });
  await page.route(/(.*)\/api\/cf\/status(\?.*)?$/, async (route) => {
    await released;
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ configured: true, linked: true }),
    });
  });
  return { release, arrived };
}

/** A fresh browser state per case: a `jf_lang` left behind by an earlier card
 *  would render the panel in another language and every text assertion here
 *  would be measuring a locale no new visitor has. */
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

test("a deployment where auto-setup IS configured never paints the negative sentence", async ({ page }) => {
  await stubAPI(page);
  const { release, arrived } = await holdCfStatus(page);
  await installRecorder(page);

  await page.goto("/settings?tab=domains", { waitUntil: "domcontentloaded" });
  await arrived;

  // The panel is painted and its fetch is still in flight. This is the whole
  // window the defect lived in.
  await expect(page.getByRole("heading", { name: CF_TITLE })).toBeVisible();

  const pending = await bodyText(page);
  const pendingStates = await states(page);
  expect(pending).not.toContain(NOT_CONFIGURED);
  // The login affordance is read from its anchor: the phrase also opens the
  // section's help text, so a text test cannot tell the two apart.
  await expect(page.locator('a[href="/api/cf/start"]')).toHaveCount(0);
  expect(pendingStates).not.toContain("not-conf");
  expect(pendingStates).not.toContain("login");

  release();

  await expect(page.getByText(CONNECTED)).toBeVisible();
  await expect(page.getByRole("button", { name: DISCONNECT })).toBeVisible();

  // EXACTLY ONE transition: `loading` -> `connected`. A second transition would
  // mean the panel painted an answer before it had one.
  expect(await states(page)).toEqual(["loading", "connected"]);
  const settled = await bodyText(page);
  expect(settled).toContain(CONNECTED);
  expect(settled).not.toContain(NOT_CONFIGURED);
});

test("with no client id the honest negative answer still appears once the fetch resolves", async ({ page }) => {
  await stubAPI(page);
  let release!: () => void;
  const released = new Promise<void>((resolve) => {
    release = resolve;
  });
  const arrived = page.waitForRequest(/\/api\/cf\/status(\?.*)?$/, { timeout: 20_000 });
  await page.route(/(.*)\/api\/cf\/status(\?.*)?$/, async (route) => {
    await released;
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ configured: false, linked: false }),
    });
  });
  await installRecorder(page);

  await page.goto("/settings?tab=domains", { waitUntil: "domcontentloaded" });
  await arrived;
  await expect(page.getByRole("heading", { name: CF_TITLE })).toBeVisible();
  expect(await bodyText(page)).not.toContain(NOT_CONFIGURED);

  release();

  // The guard suppresses the PRE-FETCH paint, not the honest answer: once the
  // deployment really has no client id, the sentence is what the merchant must
  // see, and the unlinked branch offers the login.
  await expect(page.getByText(NOT_CONFIGURED)).toBeVisible();
  await expect(page.locator('a[href="/api/cf/start"]')).toHaveText(LOGIN);
  expect(await states(page)).toEqual(["loading", "not-conf"]);
});

test("a failed /api/cf/status settles the panel instead of hanging it", async ({ page }) => {
  await stubAPI(page);
  const arrived = page.waitForRequest(/\/api\/cf\/status(\?.*)?$/, { timeout: 20_000 });
  await page.route(/(.*)\/api\/cf\/status(\?.*)?$/, (route) => route.abort("failed"));
  await installRecorder(page);

  await page.goto("/settings?tab=domains", { waitUntil: "domcontentloaded" });
  await arrived;

  // A fetch that never answers must not leave the panel in a state where the
  // merchant is told nothing: the flag is set on the failure path too.
  await expect(page.getByText(NOT_CONFIGURED)).toBeVisible({ timeout: 10_000 });
});
