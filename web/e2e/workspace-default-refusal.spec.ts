// A REFUSED "Make this the default workspace" / "Remove default workspace" in
// the HEADER SWITCHER must say so (card t_f6de6395).
//
// THE DEFECT (measured live 2026-11-05 on a `next build` of the pre-fix tree,
// API stubbed at the browser level to answer 400, fresh state, `jf_lang=en`, two
// workspaces with `beta` active and `alpha` default): clicking the row sent
// exactly one `PUT /api/auth/profile/default-workspace` with
// `{"workspace_id":"ws-beta"}`, the menu closed (`aria-expanded="false"`), and
// then NOTHING — `document.querySelectorAll('[role="alert"]').length === 0`, no
// failure text anywhere on the page, and the switcher still offered the same
// action. `Shell.setDefaultWorkspace()` never read the response status, its
// `catch {}` swallowed transport failures, and it called `reload()`
// unconditionally, so a 400/403/500 and a dead network both looked like a click
// that did nothing. The dictionary already carried `workspace.setDefaultFail`
// and `workspace.clearDefaultFail` and no surface rendered either.
//
// WHAT THIS FILE PINS, AND WHY IT IS A BROWSER MEASUREMENT
//   The claim is about what a merchant SEES after a real click through the real
//   client component. The panel closes on click (the write is fire-and-wait), so
//   the sentence has to be rendered OUTSIDE the panel or it unmounts unseen —
//   a fact about the live DOM, not about a source line. The API is stubbed at
//   the browser level (the pattern `e2e/settings-domains-workspace-switch.spec.ts`
//   uses): no Go server, no DB, no account or workspace is created.
//
// THE ARMS
//   1. SET refused (400, no machine code): one PUT with the ACTIVE workspace's
//      id, the menu closes, `workspace.setDefaultFail` renders ONCE in a live
//      region, and re-opening the switcher still offers the action (the default
//      did not move).
//   2. CLEAR refused (500): one PUT with `""`, `workspace.clearDefaultFail`
//      renders once, and the clear is still offered — the other direction has
//      its own sentence.
//   3. the request dies at the TRANSPORT level (`route.abort`): the set sentence
//      still renders. This is the `catch {}` half of the defect — a dropped
//      connection must not be silence either.
//   4. MOBILE drawer (390×844): the sentence is visible in the drawer WITHOUT
//      closing it, because the drawer's switcher renders its panel in flow.
//
// The source half of the guard is `src/lib/workspaceDefaultFailure.test.ts`.
import { test, expect, type Page } from "@playwright/test";

import { DICT } from "../src/components/i18nDict";
import { signIn } from "./auth";

const SET_ROW = DICT["workspace.setDefault"].en;
const CLEAR_ROW = DICT["workspace.clearDefault"].en;
const SET_FAIL = DICT["workspace.setDefaultFail"].en;
const CLEAR_FAIL = DICT["workspace.clearDefaultFail"].en;

const ALPHA = { id: "ws-alpha", name: "Alpha Shop", slug: "alpha", role: "admin" };
const BETA = { id: "ws-beta", name: "Beta Shop", slug: "beta", role: "admin" };

/** The account the session publishes. `default_workspace_id` is what the
 *  switcher reads to decide whether to offer SET or CLEAR — and a refused write
 *  must leave it exactly here. */
const meWith = (defaultWorkspaceId: string) => ({
  id: "u1",
  email: "merchant@example.com",
  name: "Merchant",
  lang: "",
  default_workspace_id: defaultWorkspaceId,
  is_super_admin: false,
});

/** The dashboard bundle shape `e2e/settings-domains-workspace-switch.spec.ts`
 *  already drives /settings with (only the fields the page reads are populated). */
const bundleFor = (defaultWorkspaceId: string) => ({
  me: meWith(defaultWorkspaceId),
  workspaces: [ALPHA, BETA],
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
});

type Refusal =
  | { kind: "status"; status: number; body: unknown }
  | { kind: "abort" };

type Stub = {
  /** Every `PUT /api/auth/profile/default-workspace` body, in order. */
  puts: unknown[];
  /** The `workspace` param of every bundle read; "" when unscoped. */
  bundleScopes: string[];
};

async function stubAPI(
  page: Page,
  opts: { defaultId: string; refusal: Refusal },
): Promise<Stub> {
  const puts: unknown[] = [];
  const bundleScopes: string[] = [];
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });

  // Catch-all FIRST, specific routes after it: Playwright resolves matching
  // routes in reverse registration order, so the later (specific) ones win.
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(meWith(opts.defaultId))));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "" })));
  await page.route(/(.*)\/api\/rest\/currency\/rates$/, (r) => r.fulfill(json({ rates: [], cached: false })));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (route) => {
    const ws = new URL(route.request().url()).searchParams.get("workspace");
    bundleScopes.push(ws ?? "");
    return route.fulfill(json(bundleFor(opts.defaultId)));
  });

  // The write under test — ALWAYS registered last, so it wins over the catch-all.
  // The refusal does NOT change `default_workspace_id`: that is the whole point —
  // the server said no, so the state the shell re-reads is the old one.
  await page.route(/(.*)\/api\/auth\/profile\/default-workspace$/, (route) => {
    if (route.request().method() !== "PUT") return route.fulfill(json({}));
    puts.push(route.request().postDataJSON());
    if (opts.refusal.kind === "abort") return route.abort("failed");
    return route.fulfill({
      status: opts.refusal.status,
      contentType: "application/json",
      body: JSON.stringify(opts.refusal.body),
    });
  });

  return { puts, bundleScopes };
}

/** A FRESH browser state on `slug`: the browser keeps localStorage between runs,
 *  so a `jf_workspace`/`jf_lang` left behind by an earlier card would scope this
 *  page to a workspace or language it never described. */
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

/** The switcher trigger that is VISIBLE at the current viewport: the desktop
 *  topbar's chip and the drawer's row are two instances of one component, and on
 *  a phone the desktop one is CSS-hidden (still in the DOM). */
function trigger(page: Page) {
  return page.locator('button[aria-haspopup="menu"]:visible').first();
}

/** The live region the switcher renders a refusal into, scoped to the switcher
 *  itself so a page-level alert cannot be mistaken for it. */
function refusal(page: Page) {
  return trigger(page).locator("..").getByRole("alert");
}

/** Open the switcher. The trigger exists in the server-rendered shell before
 *  hydration, so a click landing in that window is a no-op; `aria-expanded` is
 *  the app's own truth, and the click is retried until it says so. */
async function openSwitcher(page: Page) {
  const t = trigger(page);
  await t.waitFor({ state: "visible", timeout: 30_000 });
  await expect
    .poll(
      async () => {
        if ((await t.getAttribute("aria-expanded")) !== "true") await t.click();
        return t.getAttribute("aria-expanded");
      },
      { timeout: 20_000 },
    )
    .toBe("true");
}

async function landOnDashboard(page: Page, mobile = false) {
  await page.goto("/settings?tab=domains", { waitUntil: "domcontentloaded" });
  // At a phone width the topbar switcher is CSS-hidden (still in the DOM) and
  // the shell's own affordance is the drawer's hamburger, so that is what proves
  // the page rendered as the mobile shell.
  if (mobile) await expect(page.getByRole("button", { name: "Open menu" })).toBeVisible();
  else await expect(trigger(page)).toBeVisible();
}

/** Open the mobile drawer. The hamburger is in the server-rendered markup, so a
 *  click before hydration is a no-op — the dialog appearing is the app's own
 *  truth, and the click is retried until it does. */
async function openDrawer(page: Page) {
  const hamburger = page.getByRole("button", { name: "Open menu" });
  await expect
    .poll(
      async () => {
        if (!(await page.getByRole("dialog", { name: "Navigation menu" }).isVisible())) await hamburger.click();
        return page.getByRole("dialog", { name: "Navigation menu" }).isVisible();
      },
      { timeout: 20_000 },
    )
    .toBe(true);
}

test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

test("a REFUSED set-default says so, once, and the action is still offered", async ({ page }) => {
  const api = await stubAPI(page, {
    defaultId: ALPHA.id,
    refusal: { kind: "status", status: 400, body: { error: "gagal menetapkan workspace default", code: "" } },
  });
  await freshState(page, BETA.slug);
  await landOnDashboard(page);

  await openSwitcher(page);
  await expect(page.getByRole("menuitem", { name: SET_ROW })).toBeVisible();
  await expect(refusal(page)).toHaveCount(0);

  await page.getByRole("menuitem", { name: SET_ROW }).click();

  // The write went out exactly once, for the ACTIVE workspace.
  await expect.poll(() => api.puts.length, { timeout: 20_000 }).toBe(1);
  expect(api.puts).toEqual([{ workspace_id: BETA.id }]);

  // The menu closes (unchanged behaviour) and the refusal is SAID.
  await expect(trigger(page)).toHaveAttribute("aria-expanded", "false");
  await expect(refusal(page)).toHaveCount(1);
  await expect(refusal(page)).toHaveText(SET_FAIL);

  // The default did not move: the same action is still offered, the clear is not.
  await openSwitcher(page);
  await expect(page.getByRole("menuitem", { name: SET_ROW })).toBeVisible();
  await expect(page.getByRole("menuitem", { name: CLEAR_ROW })).toHaveCount(0);
  await expect(refusal(page)).toHaveText(SET_FAIL);
});

test("a REFUSED clear-default has its OWN sentence, once, and the clear is still offered", async ({ page }) => {
  const api = await stubAPI(page, {
    defaultId: ALPHA.id,
    refusal: { kind: "status", status: 500, body: { error: "gagal membuang workspace default" } },
  });
  await freshState(page, ALPHA.slug);
  await landOnDashboard(page);

  await openSwitcher(page);
  await expect(page.getByRole("menuitem", { name: CLEAR_ROW })).toBeVisible();
  await expect(page.getByRole("menuitem", { name: SET_ROW })).toHaveCount(0);

  await page.getByRole("menuitem", { name: CLEAR_ROW }).click();

  await expect.poll(() => api.puts.length, { timeout: 20_000 }).toBe(1);
  expect(api.puts).toEqual([{ workspace_id: "" }]);

  await expect(refusal(page)).toHaveCount(1);
  await expect(refusal(page)).toHaveText(CLEAR_FAIL);

  // Still the default, so the same control is still there to try again.
  await openSwitcher(page);
  await expect(page.getByRole("menuitem", { name: CLEAR_ROW })).toBeVisible();
});

test("a write that DIES at the transport level is not silence either", async ({ page }) => {
  const api = await stubAPI(page, { defaultId: ALPHA.id, refusal: { kind: "abort" } });
  await freshState(page, BETA.slug);
  await landOnDashboard(page);

  await openSwitcher(page);
  await page.getByRole("menuitem", { name: SET_ROW }).click();

  await expect.poll(() => api.puts.length, { timeout: 20_000 }).toBe(1);
  await expect(refusal(page)).toHaveCount(1);
  await expect(refusal(page)).toHaveText(SET_FAIL);

  // The pre-fix `catch {}` is the arm being guarded: a rejected fetch reached no
  // state at all, so nothing on the page changed.
  await openSwitcher(page);
  await expect(page.getByRole("menuitem", { name: SET_ROW })).toBeVisible();
});

test("in the MOBILE drawer the sentence is visible without closing the drawer", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await stubAPI(page, {
    defaultId: ALPHA.id,
    refusal: { kind: "status", status: 403, body: { error: "tidak dibenarkan", code: "" } },
  });
  await freshState(page, BETA.slug);
  await landOnDashboard(page, true);

  // Open the drawer, then the switcher inside it. Both live in the drawer's own
  // shrink-0 header block, which is why the panel (and the refusal under it)
  // renders in flow rather than being clipped or covered.
  await openDrawer(page);
  const drawer = page.getByRole("dialog", { name: "Navigation menu" });
  await expect(drawer).toBeVisible();

  await openSwitcher(page);
  await page.getByRole("menuitem", { name: SET_ROW }).click();

  await expect(refusal(page)).toHaveCount(1);
  await expect(refusal(page)).toHaveText(SET_FAIL);
  await expect(refusal(page)).toBeVisible();
  // The drawer is STILL OPEN — the merchant reads the sentence where they acted.
  await expect(drawer).toBeVisible();
});
