// Regression guard for card t_88485157 — the Settings → Domains Cloudflare
// section read and wrote the session user's FIRST-OWNED account instead of the
// workspace the switcher was on.
//
// THE DEFECT
//   `settings/page.tsx` fetched `/api/cf/status` with NO workspace scope:
//
//       const loadCfStatus = useCallback(() => fetch("/api/cf/status") …
//
//   and the API resolves an UNSCOPED session call to `Accounts.ByOwner(...)[0]`
//   (`resolveSessionAccount` — "Otherwise the user's first owned account is
//   used"). The cf routes are all account-scoped (`cfStatus` reads
//   `actx.Account.ID`'s sealed token, `cfDisconnect` clears that same account's
//   token), so the panel described — and mutated — a DIFFERENT account than the
//   one whose domains the tab lists. Measured on prod v1.21.257 for a user who is
//   a member of two workspaces whose accounts differ in link state:
//
//       GET /api/cf/status              -> {"configured":true,"linked":true}
//       GET /api/cf/status?workspace=abc -> {"configured":true,"linked":false}
//
//   and with `jf_workspace = "abc"` the Domains tab still rendered "Cloudflare
//   connected ✓ … Disconnect". Two edges, one destructive:
//     1. a merchant on an unlinked workspace is told they are linked and is
//        offered no "Log in with Cloudflare", so auto-CNAME silently does not
//        apply to the workspace they are looking at;
//     2. Disconnect (and the manual token form) was unscoped too, so pressing it
//        while on `abc` revoked/deleted the `wan` account's sealed token — a link
//        the user cannot even see.
//
// WHAT THIS FILE PINS, AND WHY IT IS A BROWSER MEASUREMENT
//   The claim is about WHICH ACCOUNT a rendered panel is describing and which
//   account a button mutates. That is a property of the request the component
//   actually makes and of the DOM it paints from the answer, so a source
//   assertion cannot see it (the string `?workspace=` could be present in a
//   template and unused) and a component unit test would have to re-state the
//   URL-building rule it is meant to check. Here the real client component runs
//   in a real browser with `/api/cf/status` stubbed PER WORKSPACE — the way the
//   API answers, because each workspace owns its own account — and an UNSCOPED
//   call gets the first-owned account's answer. Every request is recorded, so
//   "the panel is scoped" is asserted on the wire, not inferred.
//
// THE FOUR ARMS
//   1. `jf_workspace = abc` (the workspace with NO credential): the panel must
//      report that workspace — no "connected ✓", no Disconnect, the login link
//      present — and every status read must have been scoped to `abc`. On the
//      pre-fix tree the unscoped read answers "linked" and the panel claims
//      connected.
//   2. `jf_workspace = wan` (the linked workspace, which is ALSO the account an
//      unscoped call resolves to — the state the defect got right by accident):
//      connected + Disconnect, no login link, and the Disconnect POST must name
//      `?workspace=wan`. Unscoped there is the destructive edge.
//   3. A real SWITCH through the UI switcher (the mechanism `Shell.switchWorkspace`
//      uses: `enterWorkspace` → publish the confirmed scope → `jf:workspace-changed`
//      → `useSession().workspace`). The status answer for the new workspace is
//      HELD, so the pending window is deterministic: while it is in flight the
//      panel must show no link state AT ALL (carrying `wan`'s "connected ✓" across
//      a switch is the same lie in a different tense), and once it answers the
//      login link must name the new workspace.
//   4. A COLD MOUNT (card t_a7e3c4d4): no `jf_workspace`, so the scope is unknown
//      for the first ~500ms. Arms 1–3 cannot see this state — they all start with
//      the slug already stored — and it is where the panel used to issue a BARE
//      status read and paint a BARE OAuth href.
//
// The API is stubbed at the browser level (the pattern `e2e/cf-status-prefetch.spec.ts`
// and `e2e/a11y-dialogs.spec.ts` use): no Go server, no DB — the real client
// component is driven in a real browser, and this file controls only the CF
// routes, because those are the ones the defect is about.
import { test, expect, type Page } from "@playwright/test";

import { DICT } from "../src/components/i18nDict";
import { signIn } from "./auth";

const CF_TITLE = DICT["set.cfTitle"].en;
const CONNECTED = DICT["set.cfConnected"].en;
const LOGIN = DICT["set.cfLogin"].en;
const DISCONNECT = DICT["set.cfDisconnect"].en;

const ME = { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "", is_super_admin: false };

/**
 * The two workspaces, chosen so the fixture can DISTINGUISH the defect's answer
 * from the correct one:
 *
 *   LINKED_WS is FIRST in the list, so it is also the account an UNSCOPED
 *   `/api/cf/status` resolves to (`Accounts.ByOwner(...)[0]`), and it is LINKED.
 *   UNLINKED_WS is the one the merchant selects and has no credential.
 *
 * Any panel that reports the first workspace while the second is selected is
 * therefore visible as "connected" on a workspace that is not.
 */
const LINKED_WS = { id: "ws1", name: "Wan Shop", slug: "wan", role: "manage" };
const UNLINKED_WS = { id: "ws2", name: "Second Shop", slug: "abc", role: "manage" };

const BUNDLE = {
  me: ME,
  workspaces: [LINKED_WS, UNLINKED_WS],
  chip_status: null, keys: [], domains: [], webhooks: [], social: [], einvoice: null,
  products: [], summary: { counts: {}, revenue_sen: 0 },
  sales: { items: [], page: 1, limit: 20, total: 0 },
  form_analytics: [], product_analytics: [],
};

type CFStub = {
  /** The `workspace` param of every `/api/cf/status` request, "<none>" when absent. */
  statusCalls: string[];
  /** The full query string (`""` when absent) of every `/api/cf/disconnect` POST. */
  disconnectCalls: string[];
  /** Hold the status answer for this slug, so the pending window is deterministic. */
  hold: (slug: string) => void;
  /** Release every held status answer. */
  release: () => void;
};

type StubOpts = {
  /** The workspace list the dashboard bundle returns. LINKED_WS first by default. */
  workspaces?: unknown[];
  /**
   * The slug of the user's FIRST-OWNED account — the account an UNSCOPED
   * `/api/cf/status` resolves to (`resolveSessionAccount`). It is NOT the first
   * row of the workspace list in general (that list is ordered by the server,
   * and ownership is a separate relation), so the fixture can name it
   * independently. Defaults to LINKED_WS, which is what the existing arms
   * describe.
   */
  firstOwned?: string;
};

async function stubAPI(page: Page, opts: StubOpts = {}): Promise<CFStub> {
  const firstOwned = opts.firstOwned ?? LINKED_WS.slug;
  const bundle = { ...BUNDLE, workspaces: opts.workspaces ?? [LINKED_WS, UNLINKED_WS] };
  const statusCalls: string[] = [];
  const disconnectCalls: string[] = [];
  const held = { slug: "" };
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
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(bundle)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(ME)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "" })));
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/affiliates(\?.*)?$/, (r) => r.fulfill(json([])));
  await page.route(/(.*)\/api\/rest\/currency\/rates$/, (r) => r.fulfill(json({ rates: [], cached: false })));

  // The ONE endpoint this spec controls, answering per WORKSPACE the way the API
  // does. `linked` is keyed on the account the request resolved to: the selected
  // workspace's own account when scoped, and the first-owned account (LINKED_WS)
  // when not — which is exactly the production fallback the defect rode in on.
  await page.route(/(.*)\/api\/cf\/status(\?.*)?$/, async (route) => {
    const ws = new URL(route.request().url()).searchParams.get("workspace");
    statusCalls.push(ws ?? "<none>");
    if (ws !== null && ws === held.slug) await gate;
    await route.fulfill(json({ configured: true, linked: (ws ?? firstOwned) === LINKED_WS.slug }));
  });

  // Disconnect: recorded, never applied (there is no API here). The assertion is
  // about WHICH account the call names, which is the destructive half of the
  // defect.
  await page.route(/(.*)\/api\/cf\/disconnect(\?.*)?$/, async (route) => {
    disconnectCalls.push(new URL(route.request().url()).search);
    await route.fulfill(json({ status: "disconnected" }));
  });

  return {
    statusCalls,
    disconnectCalls,
    hold: (slug: string) => {
      held.slug = slug;
    },
    release: () => release(),
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
 * A COLD browser state: everything cleared, and — unlike `freshState` — NO
 * `jf_workspace` written. That is what a first-time visitor, a fresh profile, or
 * anyone after a sign-out has until the session publishes a workspace, and it is
 * the state in which `workspaceQuery()` used to answer "" (card t_a7e3c4d4).
 *
 * It also installs, from document start, a recorder of every `/api/cf/start`
 * href the DOM is EVER given. The defect is a WINDOW, not an end state: on the
 * pre-fix tree the href is corrected the moment the scope arrives, so reading
 * the attribute after the panel settles sees only the right value. A
 * MutationObserver sees what was actually painted.
 */
async function coldState(page: Page) {
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("jf_lang", "en");
    } catch {
      /* storage unavailable */
    }
    const seen: string[] = [];
    (window as unknown as { __cfStartHrefs: string[] }).__cfStartHrefs = seen;
    const scan = () => {
      document.querySelectorAll('a[href^="/api/cf/start"]').forEach((a) => {
        const href = a.getAttribute("href") || "";
        if (seen[seen.length - 1] !== href) seen.push(href);
      });
    };
    // `document` (not `document.documentElement`) because this script runs before
    // the document element exists; `subtree` still reaches every later node.
    new MutationObserver(scan).observe(document, {
      subtree: true,
      childList: true,
      attributes: true,
      attributeFilter: ["href"],
    });
  });
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

/** The workspace a URL names, read from its own query — `null` when unscoped. */
function workspaceOf(href: string | null): string | null {
  return new URL(href ?? "", "http://e2e.invalid").searchParams.get("workspace");
}

test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

test("an unlinked workspace is reported as UNLINKED, not as the first-owned account", async ({ page }) => {
  const cf = await stubAPI(page);
  await freshState(page, UNLINKED_WS.slug);

  await page.goto("/settings?tab=domains", { waitUntil: "domcontentloaded" });
  await expect(page.getByRole("heading", { name: CF_TITLE })).toBeVisible();

  // The unlinked branch: the login affordance, and NOTHING claiming a link.
  const login = page.locator('a[href^="/api/cf/start"]');
  await expect(login).toBeVisible();
  await expect(login).toHaveText(LOGIN);
  await expect(page.getByText(CONNECTED)).toHaveCount(0);
  await expect(page.getByRole("button", { name: DISCONNECT })).toHaveCount(0);

  // The OAuth link must carry the workspace as well: `/api/cf/callback` seals the
  // token against the account it resolves, so an unscoped start would link the
  // FIRST-OWNED account from a workspace the merchant is not looking at.
  expect(workspaceOf(await login.getAttribute("href"))).toBe(UNLINKED_WS.slug);

  // Every status read was scoped to the workspace on screen. A single unscoped
  // one is the defect itself: it resolves to the first owned account, whose
  // answer is "linked".
  expect(cf.statusCalls.length).toBeGreaterThan(0);
  expect(cf.statusCalls.filter((w) => w !== UNLINKED_WS.slug)).toEqual([]);
});

test("the linked workspace still reads connected, and Disconnect names THAT workspace", async ({ page }) => {
  const cf = await stubAPI(page);
  await freshState(page, LINKED_WS.slug);

  await page.goto("/settings?tab=domains", { waitUntil: "domcontentloaded" });
  await expect(page.getByRole("heading", { name: CF_TITLE })).toBeVisible();

  await expect(page.getByText(CONNECTED)).toBeVisible();
  await expect(page.locator('a[href^="/api/cf/start"]')).toHaveCount(0);
  const disconnect = page.getByRole("button", { name: DISCONNECT });
  await expect(disconnect).toBeVisible();

  expect(cf.statusCalls.filter((w) => w !== LINKED_WS.slug)).toEqual([]);

  // The destructive edge: an unscoped POST clears the FIRST-OWNED account's
  // sealed token. The call must name the workspace whose state the merchant is
  // looking at.
  await disconnect.click();
  await expect(page.getByText(CONNECTED)).toHaveCount(0);
  expect(cf.disconnectCalls).toEqual([`?workspace=${LINKED_WS.slug}`]);
});

test("switching workspace re-reads the status and carries no answer across the switch", async ({ page }) => {
  const cf = await stubAPI(page);
  await freshState(page, LINKED_WS.slug);

  await page.goto("/settings?tab=domains", { waitUntil: "domcontentloaded" });
  await expect(page.getByRole("heading", { name: CF_TITLE })).toBeVisible();
  await expect(page.getByText(CONNECTED)).toBeVisible();

  // Hold the answer for the workspace we are switching TO. Without the hold the
  // "pending" half of this arm would be a race (and a race is how a guard gets
  // muted); with it, the window lasts until `release()`.
  cf.hold(UNLINKED_WS.slug);
  const arrived = page.waitForRequest(
    (r) => {
      const u = new URL(r.url());
      return u.pathname.endsWith("/api/cf/status") && u.searchParams.get("workspace") === UNLINKED_WS.slug;
    },
    { timeout: 20_000 },
  );

  // A REAL switch: the switcher calls `Shell.switchWorkspace` → `enterWorkspace`
  // (publish the confirmed scope + `jf:workspace-changed`) → `useSession().workspace`.
  await openSwitcher(page);
  await page.locator('button[role="menuitem"]', { hasText: UNLINKED_WS.name }).first().click();
  await arrived;

  // While the new answer is in flight the panel must show NO link state. The
  // previous workspace's "connected ✓" is a claim about an account the merchant
  // is no longer looking at, and its Disconnect button would revoke it.
  await expect(page.getByText(CONNECTED)).toHaveCount(0);
  await expect(page.getByRole("button", { name: DISCONNECT })).toHaveCount(0);
  await expect(page.locator('a[href^="/api/cf/start"]')).toHaveCount(0);

  cf.release();

  const login = page.locator('a[href^="/api/cf/start"]');
  await expect(login).toBeVisible();
  expect(workspaceOf(await login.getAttribute("href"))).toBe(UNLINKED_WS.slug);
  await expect(page.getByText(CONNECTED)).toHaveCount(0);
  expect(cf.statusCalls.at(-1)).toBe(UNLINKED_WS.slug);
});

// CARD t_a7e3c4d4 — THE COLD MOUNT.
//
// Arms 1–3 all start with `jf_workspace` ALREADY in localStorage, so the scope
// is known before the first render. A cold document is the state they cannot
// see: `workspaceQuery()` answers "" until the session publishes a workspace
// (~500ms on prod), and on the pre-fix tree the panel fired anyway.
//
//   t=186ms  GET /api/cf/status                    -> 200   <-- BARE, first-owned account
//   t=546ms  GET /api/cf/status?workspace=wan      -> 200   <-- the scoped read
//
// The bare answer was APPLIED (the scoped request is issued after it lands, so
// `cfStatusSeq` had nothing newer to drop it for), so the panel described an
// account nobody had selected for ~360ms — and the same window left a bare
// `/api/cf/start` href in the DOM, one click from an OAuth flow whose callback
// seals against the first-owned account.
//
// The fixture is ONE workspace, UNLINKED, and it is also the first-owned
// account, so the unscoped answer is "not linked" — the state in which the
// panel paints the OAuth link. That makes both windows observable in one run:
// the bare read (recorded by the route) and the bare href (recorded from
// document start by `coldState`). The settled panel is asserted too, but it is
// NOT what this arm is for: on the pre-fix tree the settled panel is also
// correct, which is why an end-state DOM check would pass the defect.
test("a COLD mount issues no unscoped status read and never paints a bare login href", async ({ page }) => {
  const cf = await stubAPI(page, { workspaces: [UNLINKED_WS], firstOwned: UNLINKED_WS.slug });
  await coldState(page);

  await page.goto("/settings?tab=domains", { waitUntil: "domcontentloaded" });
  await expect(page.getByRole("heading", { name: CF_TITLE })).toBeVisible();

  // The panel still settles — gated, not muted: the login link appears once the
  // session has published the workspace, and it names that workspace.
  const login = page.locator('a[href^="/api/cf/start"]');
  await expect(login).toBeVisible();
  expect(workspaceOf(await login.getAttribute("href"))).toBe(UNLINKED_WS.slug);
  await expect(page.getByText(CONNECTED)).toHaveCount(0);

  // 1. THE WIRE. Every status read named a workspace; "<none>" is the unscoped
  //    call the defect issued at t=186ms on prod. One is enough to fail: that
  //    answer resolves to the first-owned account and is applied.
  expect(cf.statusCalls.length).toBeGreaterThan(0);
  expect(cf.statusCalls.filter((w) => w === "<none>")).toEqual([]);

  // 2. THE DOM. No anchor ever carried an unscoped href — at ANY point in the
  //    mount, not merely at the end.
  const hrefs = await page.evaluate(
    () => (window as unknown as { __cfStartHrefs: string[] }).__cfStartHrefs ?? [],
  );
  expect(hrefs.length).toBeGreaterThan(0);
  expect(hrefs.filter((h) => workspaceOf(h) === null)).toEqual([]);
});
