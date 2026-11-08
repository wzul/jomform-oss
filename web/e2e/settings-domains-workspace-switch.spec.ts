// Regression guard for card t_de220c75 — Settings → Domains rendered the
// PREVIOUS workspace's custom-domain list after a workspace switch.
//
// THE DEFECT (measured on prod v1.22.91, deterministic in two switches)
//   Two workspaces, B (`wan`) with two verified custom domains and A (`abc`)
//   with none. Opening the tab on A showed an empty list (correct). Switching to
//   B left the list EMPTY — A's list, under B's chip — and switching back to A
//   then rendered B's two domains under A's chip. One switch behind, both ways,
//   with BOTH scoped `dashboard-bundle` reads on the wire, so the server was
//   answering correctly and the panel was APPLYING the wrong answer.
//
// WHAT THIS FILE PINS, AND WHY IT IS A BROWSER MEASUREMENT
//   The claim is about what the rendered list shows after a real switch through
//   the header switcher, i.e. about the bundle that the session publishes LAST
//   and about the render that is allowed to keep it. The failure mode is an
//   apply that runs with a closure's stale `bundle`, which a source assertion
//   cannot see, so the real client component runs in a real browser with
//   `dashboard-bundle` stubbed PER WORKSPACE — the way the API answers, because
//   each workspace owns its own account.
//
// THE ARMS
//   1. A → B: B's rows appear, and the switch issues no bundle read for the
//      workspace it is leaving. The wire snapshot is taken only after the
//      MOUNT has settled (`awaitMountSettled`), because the panel paints an
//      empty list before any bundle is applied and a log sampled that early
//      counts the mount's own reads as the switch's.
//   2. B → A: A's rows are gone. On the pre-fix tree B's two rows are painted
//      under A's chip — the cross-workspace leak, with a live `Remove` button
//      for a domain that workspace does not own.
//   3. THE ORDERING, made deterministic: the answer for the workspace being
//      LEFT is delayed so it lands AFTER the new workspace's — the prod order in
//      which the stale answer wins the assignment and the panel stays one
//      workspace behind.
//   4. THE COLD MOUNT: `jf_workspace` is stored, but the session's first bundle
//      request is UNSCOPED and answers for the first-owned account; that answer
//      is delayed past the scoped one and must not be applied.
//   5. A HOLD: the answer for the workspace being switched TO is held, so the
//      pending window is deterministic and the previous workspace's rows must
//      already be gone — carrying them across a switch is the same lie in a
//      different tense, and on this tab it is one click from a `Remove` that
//      deletes another workspace's domain.
//
// MEASURED (this file, both trees): on the pre-fix source arms 1-4 FAIL
// deterministically (4 failed / 1 passed); with the fix they all pass (5
// passed). Arm 5 is the pending-window assertion and is timing-sensitive on the
// pre-fix tree — it has been observed both red and green there — so it is not
// the arm the regression rests on.
//
// The API is stubbed at the browser level (the pattern
// `e2e/cf-status-workspace-scope.spec.ts` uses): no Go server, no DB, no account
// or workspace is created — the real client component is driven in a real
// browser with only the routes this defect is about controlled.
import { test, expect, type Page } from "@playwright/test";

import { DICT } from "../src/components/i18nDict";
import { signIn } from "./auth";

const DOMAINS_TITLE = DICT["set.domainsTitle"].en;

const ME = { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "", is_super_admin: false };

/**
 * The two workspaces. `EMPTY_WS` is FIRST in the list, so it is also the account
 * an UNSCOPED `/api/rest/dashboard-bundle` resolves to (`resolveSessionAccount`:
 * "Otherwise the user's first owned account is used") — that is the production
 * shape the cold-mount arm of the finding described, and it means a panel that
 * applies the wrong answer is visible as rows on a workspace that has none.
 */
const EMPTY_WS = { id: "ws1", name: "Empty Shop", slug: "abc", role: "admin" };
const FULL_WS = { id: "ws2", name: "Full Shop", slug: "wan", role: "admin" };

const FULL_DOMAINS = [
  { id: "d1", domain: "halo.wanzul-hosting.com", provider: "cloudflare", verified: true },
  { id: "d2", domain: "jftest.wanzul-hosting.com", provider: "cloudflare", verified: true },
];

const bundleFor = (slug: string) => ({
  me: ME,
  workspaces: [EMPTY_WS, FULL_WS],
  chip_status: null,
  keys: [],
  domains: slug === FULL_WS.slug ? FULL_DOMAINS : [],
  webhooks: [],
  social: [],
  einvoice: null,
  products: [],
  summary: { counts: {}, revenue_sen: 0 },
  sales: { items: [], page: 1, limit: 20, total: 0 },
  form_analytics: [],
  product_analytics: [],
});

type DomainStub = {
  /** The `workspace` param of every `/api/rest/dashboard-bundle` request; "<none>" when absent. */
  bundleCalls: string[];
  /**
   * Bundle reads the stub has MATCHED but not answered yet. 0 means nothing is
   * outstanding, which is the half of the mount-settled precondition that a
   * call log alone cannot state (`awaitMountSettled`).
   */
  readonly bundlePending: number;
  /** The `workspace` param of every `/api/rest/domains` request; "<none>" when absent. */
  domainCalls: string[];
  /**
   * Hold the bundle answer for this slug, so the pending window is deterministic.
   */
  hold: (slug: string) => void;
  /** Release every held bundle answer. */
  release: () => void;
  /**
   * Delay the bundle answer for this slug by `ms`. The prod defect is an ORDERING
   * defect: the previous workspace's read is issued at the switch and its answer
   * can land after the new workspace's, so this makes that order deterministic
   * instead of a race. The key is the `workspace` PARAM as it went out — "" is
   * the UNSCOPED bootstrap read, which is the cold-mount arm.
   */
  delay: (slug: string, ms: number) => void;
};

async function stubAPI(page: Page, opts: { firstOwned?: string } = {}): Promise<DomainStub> {
  // The account an UNSCOPED `dashboard-bundle` resolves to
  // (`resolveSessionAccount`: "Otherwise the user's first owned account is
  // used"). EMPTY_WS is first in the list, so that is the default.
  const firstOwned = opts.firstOwned ?? EMPTY_WS.slug;
  const bundleCalls: string[] = [];
  let bundlePending = 0;
  const domainCalls: string[] = [];
  const held = { slug: "" };
  const delays: Record<string, number> = {};
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });

  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });

  // Catch-all FIRST, specific routes after it: Playwright resolves matching
  // routes in reverse registration order, so the later (specific) ones win.
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(ME)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "" })));
  await page.route(/(.*)\/api\/rest\/currency\/rates$/, (r) => r.fulfill(json({ rates: [], cached: false })));

  // The bundle, answering PER WORKSPACE the way the API does. An UNSCOPED read
  // answers for the first-owned account (EMPTY_WS), which is the fallback the
  // session's cold-mount request rides in on.
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, async (route) => {
    const ws = new URL(route.request().url()).searchParams.get("workspace");
    bundleCalls.push(ws ?? "<none>");
    bundlePending += 1;
    try {
      const slug = ws ?? firstOwned;
      if (ws !== null && ws === held.slug) await gate;
      const ms = delays[ws ?? ""];
      if (ms) await new Promise((r) => setTimeout(r, ms));
      await route.fulfill(json(bundleFor(slug)));
    } finally {
      bundlePending -= 1;
    }
  });

  // `refreshDomains()` reads this list directly, so it answers per workspace too
  // — otherwise a correct implementation that re-reads would be judged by a
  // fixture that cannot tell the two workspaces apart.
  await page.route(/(.*)\/api\/rest\/domains(\?.*)?$/, async (route) => {
    const ws = new URL(route.request().url()).searchParams.get("workspace");
    domainCalls.push(ws ?? "<none>");
    await route.fulfill(json(ws === FULL_WS.slug ? FULL_DOMAINS : []));
  });

  return {
    bundleCalls,
    get bundlePending() {
      return bundlePending;
    },
    domainCalls,
    hold: (slug: string) => {
      held.slug = slug;
    },
    release: () => release(),
    delay: (slug: string, ms: number) => {
      delays[slug] = ms;
    },
  };
}

/**
 * A FRESH browser state on `slug`. The browser keeps localStorage between runs,
 * so a `jf_workspace` (or `jf_lang`) left behind by an earlier card would scope
 * this file's page to a workspace — or a language — it never described. Written
 * in ONE init script (clear, then set) so the order is not a property of the
 * registration order of two scripts.
 */
async function freshState(page: Page, slug: string) {
  await page.addInitScript((s) => {
    try {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("jf_workspace", s);
      localStorage.setItem("jf_lang", "en");
    } catch {
      /* storage unavailable */
    }
  }, slug);
}

/**
 * Open the topbar workspace switcher. The trigger is visible in the
 * server-rendered shell BEFORE React hydrates, so a click landing in that window
 * is a no-op; `aria-expanded` is the app's own truth for "the menu is open", and
 * the click is retried until it says so (the pattern `e2e/a11y-switcher.spec.ts`
 * uses — a blind click there was measured as a flake).
 */
async function openSwitcher(page: Page) {
  const trigger = page.locator('button[aria-haspopup="menu"]').first();
  await trigger.waitFor({ state: "visible", timeout: 30_000 });
  await expect
    .poll(
      async () => {
        if ((await trigger.getAttribute("aria-expanded")) !== "true") await trigger.click();
        return trigger.getAttribute("aria-expanded");
      },
      { timeout: 20_000 },
    )
    .toBe("true");
}

/** A REAL switch through the header switcher: Shell.switchWorkspace → enterWorkspace → jf:workspace-changed. */
async function switchTo(page: Page, ws: { name: string }) {
  await openSwitcher(page);
  await page.locator('button[role="menuitem"]', { hasText: ws.name }).first().click();
}

/** The domain rows the Domains panel is painting right now. */
function domainRows(page: Page) {
  return page.locator('[role="tabpanel"] li p.truncate');
}

async function renderedDomains(page: Page): Promise<string[]> {
  return (await domainRows(page).allInnerTexts()).map((s) => s.trim()).filter(Boolean);
}

/**
 * Let the MOUNT finish before the wire log is sampled.
 *
 * THE RACE THIS CLOSES. The Domains panel paints an EMPTY list on first paint,
 * before any bundle has been applied, so `renderedDomains(page) == []` can be
 * satisfied while the mount's own scoped reads are still on the wire. Sampling
 * the log at that moment (`bundleCalls.length = 0`) then counts the mount's own
 * late reads as "a read issued by the switch for the workspace being left".
 * Measured on this tree with the default 3 workers: 3 of 6 runs red on arm 1's
 * last assertion with `Received ["abc","abc"]` (and once
 * `["abc","abc","abc"]`), while the rows assertion above it was green in the
 * same runs — the app was right and only the log snapshot was wrong. The mount
 * issues TWO to FOUR `dashboard-bundle` reads (`abc` from the session's
 * workspace effect and from the page's no-bundle fallback, `<none>` from the
 * bootstrap), and under CPU contention they are recorded after an early reset.
 *
 * Two barriers, both REAL STATE — no timeout bump, and the assertion below
 * still fails a switch that re-reads:
 *
 *   1. OPEN THE SWITCHER FIRST. Toggling it is a React state update, so React
 *      has flushed the mount's passive effects — the session's workspace-scoped
 *      read and the page's fallback read are both ISSUED — before
 *      `aria-expanded` reads "true".
 *   2. WAIT FOR AN ANSWER. `bundlePending` counts bundle reads the stub has
 *      matched but not answered, so 0 means none is outstanding; requiring at
 *      least one scoped read for `abc` means the wait cannot be satisfied by
 *      "the mount has not started yet". With an answer DELIVERED for `abc`, the
 *      session's apply is scheduled and every later render takes the settled
 *      branch (`bundle` present → `load()` applies instead of re-fetching), so
 *      the mount cannot issue another read behind the snapshot.
 *
 * Leaves the switcher OPEN; `switchTo` re-checks `aria-expanded` and does not
 * click the trigger again.
 */
async function awaitMountSettled(page: Page, api: DomainStub) {
  await openSwitcher(page);
  await expect
    .poll(() => ({ read: api.bundleCalls.includes(EMPTY_WS.slug), pending: api.bundlePending }), {
      timeout: 20_000,
    })
    .toEqual({ read: true, pending: 0 });
}

test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

test("switching to a workspace with domains shows THAT workspace's rows", async ({ page }) => {
  const api = await stubAPI(page);
  await freshState(page, EMPTY_WS.slug);

  await page.goto("/settings?tab=domains", { waitUntil: "domcontentloaded" });
  await expect(page.getByRole("heading", { name: DOMAINS_TITLE })).toBeVisible();

  // The empty workspace renders an empty list — nothing to carry.
  await expect.poll(() => renderedDomains(page), { timeout: 20_000 }).toEqual([]);

  // …but an empty list is ALSO what the panel paints before any bundle has been
  // applied, so the wire snapshot below may only be taken once the MOUNT's own
  // reads have settled (see `awaitMountSettled` — the log reset used to race
  // them, which is the flake this card fixes).
  await awaitMountSettled(page, api);

  // Only the reads issued AFTER the switch are asserted on the wire: the mount
  // itself issues the session's bootstrap read, which is a separate (pre-scope)
  // matter and not what this card is about.
  api.bundleCalls.length = 0;
  await switchTo(page, FULL_WS);

  // The new workspace's rows. On the pre-fix tree this stays `[]` when the
  // previous workspace's answer lands last (the arm below makes that order
  // deterministic).
  await expect.poll(() => renderedDomains(page), { timeout: 20_000 }).toEqual([
    "halo.wanzul-hosting.com",
    "jftest.wanzul-hosting.com",
  ]);

  // A switch must not re-read the workspace it is LEAVING: that read is the
  // answer that can land after the new workspace's and repaint the old rows.
  expect(api.bundleCalls.filter((w) => w === EMPTY_WS.slug)).toEqual([]);
});

test("a bundle for the workspace being LEFT cannot repaint the panel when its answer lands last", async ({ page }) => {
  const api = await stubAPI(page);
  await freshState(page, EMPTY_WS.slug);

  await page.goto("/settings?tab=domains", { waitUntil: "domcontentloaded" });
  await expect(page.getByRole("heading", { name: DOMAINS_TITLE })).toBeVisible();
  await expect.poll(() => renderedDomains(page), { timeout: 20_000 }).toEqual([]);

  // THE ORDERING OF THE PROD DEFECT, made deterministic: the previous
  // workspace's answer arrives well after the new workspace's, so whichever
  // render applies it last is the list the merchant ends up looking at.
  api.delay(EMPTY_WS.slug, 1500);
  api.bundleCalls.length = 0;

  await switchTo(page, FULL_WS);
  await expect.poll(() => renderedDomains(page), { timeout: 20_000 }).toEqual([
    "halo.wanzul-hosting.com",
    "jftest.wanzul-hosting.com",
  ]);

  // …and it is still the new workspace's list once the delayed answer has
  // landed — this is the assertion that fails on the pre-fix tree, where that
  // answer is applied unconditionally and replaces `wan`'s rows with `abc`'s.
  await page.waitForTimeout(2500);
  await expect.poll(() => renderedDomains(page), { timeout: 20_000 }).toEqual([
    "halo.wanzul-hosting.com",
    "jftest.wanzul-hosting.com",
  ]);
});

test("switching back to an empty workspace drops the previous workspace's rows", async ({ page }) => {
  const api = await stubAPI(page);
  await freshState(page, FULL_WS.slug);

  await page.goto("/settings?tab=domains", { waitUntil: "domcontentloaded" });
  await expect(page.getByRole("heading", { name: DOMAINS_TITLE })).toBeVisible();
  await expect.poll(() => renderedDomains(page), { timeout: 20_000 }).toEqual([
    "halo.wanzul-hosting.com",
    "jftest.wanzul-hosting.com",
  ]);

  api.bundleCalls.length = 0;
  await switchTo(page, EMPTY_WS);
  // The empty workspace's list. On the pre-fix tree the two rows above can still
  // be painted here, each with a live `Remove` button for a domain `abc` does
  // not own — the cross-workspace leak the card measured on prod.
  await expect.poll(() => renderedDomains(page), { timeout: 20_000 }).toEqual([]);

  expect(api.bundleCalls.filter((w) => w === FULL_WS.slug)).toEqual([]);
});

test("the unscoped bootstrap bundle cannot paint the first-owned account's domains on a cold mount", async ({ page }) => {
  // THE COLD-MOUNT ARM of the finding (observed once on prod, so it is pinned
  // deterministically here instead of trusted): the tab opens with
  // `jf_workspace = abc` already stored, but the session's FIRST bundle request
  // goes out with no scope (`SessionProvider` starts at `workspace === ""`), and
  // that answer is the FIRST-OWNED account's — `wan`'s, with two domains. It is
  // held so it lands AFTER the scoped `abc` answer, which is the order that made
  // it win the assignment.
  const api = await stubAPI(page, { firstOwned: FULL_WS.slug });
  await freshState(page, EMPTY_WS.slug);
  api.delay("", 1500);

  await page.goto("/settings?tab=domains", { waitUntil: "domcontentloaded" });
  await expect(page.getByRole("heading", { name: DOMAINS_TITLE })).toBeVisible();

  // The panel settles on `abc`'s empty list…
  await expect.poll(() => renderedDomains(page), { timeout: 20_000 }).toEqual([]);
  // …and the unscoped answer, when it lands, is not this workspace's answer.
  await page.waitForTimeout(2500);
  await expect.poll(() => renderedDomains(page), { timeout: 20_000 }).toEqual([]);
});

test("the previous workspace's rows do not survive the switch while the new answer is in flight", async ({ page }) => {
  const api = await stubAPI(page);
  await freshState(page, FULL_WS.slug);

  await page.goto("/settings?tab=domains", { waitUntil: "domcontentloaded" });
  await expect(page.getByRole("heading", { name: DOMAINS_TITLE })).toBeVisible();
  await expect.poll(() => renderedDomains(page), { timeout: 20_000 }).toEqual([
    "halo.wanzul-hosting.com",
    "jftest.wanzul-hosting.com",
  ]);

  // Hold the answer for the workspace we are switching TO, so the window lasts
  // until `release()` instead of being a race.
  api.hold(EMPTY_WS.slug);
  const arrived = page.waitForRequest(
    (r) => {
      const u = new URL(r.url());
      return u.pathname.endsWith("/api/rest/dashboard-bundle") && u.searchParams.get("workspace") === EMPTY_WS.slug;
    },
    { timeout: 20_000 },
  );

  await switchTo(page, EMPTY_WS);
  await arrived;

  // While the new answer is pending the panel must not describe the workspace we
  // left: `Remove` here deletes `wan`'s verified domain from a workspace that is
  // no longer on screen.
  await expect.poll(() => renderedDomains(page), { timeout: 20_000 }).toEqual([]);

  api.release();
  await expect.poll(() => renderedDomains(page), { timeout: 20_000 }).toEqual([]);
});
