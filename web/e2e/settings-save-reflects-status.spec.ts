// Regression guard for card t_818349ab — Settings → CHIP: after a successful
// Save the status badge kept showing the OLD state until the page was manually
// refreshed (reported by Wan on prod: "bila aku dh set api key, aku kena refresh
// page tu baru ACTIVE · TEST tu muncul").
//
// THE DEFECT
//   `save()` PUT the credentials, took the server's `verified` flag, and then
//   called `load()`. `load()` is the page's APPLY function: it reads `bundle`
//   from the session context and writes it into the page's state. The closure it
//   runs in was created on the render BEFORE the save, so `bundle` was still the
//   pre-save payload — the badge (`status`), the `✓ API credentials verified`
//   marks (`verified`), the brand id and the masked tail were all written back
//   to their pre-save values. Nothing re-fetched, so nothing could correct them:
//   the merchant had to reload the page, which re-mounted the session and read a
//   fresh bundle.
//
// THE FIX (settings/page.tsx)
//   A successful write no longer re-applies the cached bundle. It applies the
//   server's OWN answer (the PUT returns `status`/`mode`/`verified`) and then
//   drops the shared session cache (`reload()`), so the fields the PUT does not
//   echo — brand id, masked key tail, whitelist — converge from a FRESH bundle
//   instead of the stale one. The same class was swept in the other writers on
//   this page that had the identical `load()` call after a write: clearing the
//   credentials and adding/editing/deleting a webhook.
//
// WHY A BROWSER MEASUREMENT
//   The claim is "the RENDERED badge changes without a page reload". A source
//   assertion cannot see that: the pre-fix code compiles, the PUT succeeds, the
//   message says "Saved", and only the DOM shows the old text. So the real client
//   component is driven in a real browser with only the routes this defect is
//   about stubbed (the pattern `e2e/settings-domains-workspace-switch.spec.ts`
//   uses) — no Go server, no DB, no credentials, no CHIP network call.
//
//   The `dashboard-bundle` stub is STATEFUL on purpose: it answers with what the
//   server would hold at that moment. That is what makes the arms honest — a
//   save that only refetches converges because the refetch is fresh, and a save
//   that re-applies a cached payload cannot.
//
// THE ARMS (no `page.reload()` anywhere in this file — that is the point)
//   1. Save CHIP credentials  → the badge reads ACTIVE · TEST.
//   2. Disconnect (clear)     → the badge reads NOT SET again, and the Clear
//                               button is gone.
//   3. Add a webhook          → the created URL is in the list.
//
// MEASURED on both trees with `npm run e2e -- settings-save-reflects-status`:
//   pre-fix  → 3 failed (each badge/list still showed the pre-save value)
//   post-fix → 3 passed
import { test, expect, type Page } from "@playwright/test";

import { DICT } from "../src/components/i18nDict";
import { signIn } from "./auth";

const NOT_SET = DICT["set.notSet"].en;
const CLEAR = DICT["set.clear"].en;
const ADD = DICT["set.add"].en;
const SAVE = DICT["set.save"].en;

/** `set.activeMode` with its `{mode}` filled the way the page fills it. */
const activeMode = (mode: string) => DICT["set.activeMode"].en.replace("{mode}", mode.toUpperCase());

const ME = { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "", is_super_admin: false };
const WS = { id: "ws1", name: "Acme Shop", slug: "acme", role: "admin" };

type ChipRow = {
  configured: boolean;
  verified: boolean;
  mode: string;
  brand_id: string;
  whitelist: string[];
  email_fallback: string;
  secret_key_masked: string;
};

const NO_CHIP: ChipRow = {
  configured: false,
  verified: false,
  mode: "",
  brand_id: "",
  whitelist: [],
  email_fallback: "",
  secret_key_masked: "",
};

type WebhookRow = { id: string; url: string; events: string[] };

type SettingsStub = {
  /** The bundle reads issued so far — the fix must issue MORE of them than the pre-fix code. */
  readonly bundleCalls: () => number;
  /** The server's CHIP row, as the stubs below currently answer it. */
  readonly chip: () => ChipRow;
  /** The server's webhook list. */
  readonly webhooks: () => WebhookRow[];
};

const json = (body: unknown) => ({ status: 200, contentType: "application/json", body: JSON.stringify(body) });

/**
 * Stub the routes this defect touches. Everything else the settings page reads
 * is answered empty — the page treats those as "nothing stored yet".
 */
async function stubSettingsAPI(page: Page): Promise<SettingsStub> {
  // THE SERVER'S OWN STATE. The PUT/DELETE stubs mutate it; the bundle stub
  // reports it. A page that re-reads the server converges; a page that re-applies
  // a cached payload does not.
  let chip: ChipRow = { ...NO_CHIP };
  let webhooks: WebhookRow[] = [];
  let bundleCalls = 0;

  const bundle = () => ({
    me: ME,
    workspaces: [WS],
    chip_status: chip,
    keys: [],
    domains: [],
    webhooks,
    social: [],
    einvoice: null,
    products: [],
    summary: { counts: {}, revenue_sen: 0 },
    sales: { items: [], page: 1, limit: 20, total: 0 },
    form_analytics: [],
    product_analytics: [],
    currency: "MYR",
  });

  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => {
    bundleCalls += 1;
    return r.fulfill(json(bundle()));
  });

  // CHIP credentials: GET reports, PUT stores, DELETE clears — as the API does.
  await page.route(/(.*)\/api\/rest\/settings\/chip(\?.*)?$/, (r) => {
    const req = r.request();
    const method = req.method();
    if (method === "PUT") {
      const body = (req.postDataJSON() || {}) as {
        brand_id?: string;
        secret_key?: string;
        email_fallback?: string;
        payment_method_whitelist?: string[];
      };
      const secret = body.secret_key || "";
      chip = {
        configured: true,
        verified: true,
        mode: secret.startsWith("sk_live") ? "live" : "test",
        brand_id: body.brand_id || "",
        whitelist: body.payment_method_whitelist || [],
        email_fallback: body.email_fallback || "",
        secret_key_masked: secret ? `••••${secret.slice(-4)}` : "",
      };
      // The real handler's 200 body — the fields the page is allowed to trust.
      return r.fulfill(json({ status: "configured", mode: chip.mode, verified: chip.verified }));
    }
    if (method === "DELETE") {
      chip = { ...NO_CHIP };
      return r.fulfill(json({ status: "cleared" }));
    }
    return r.fulfill(json(chip));
  });

  // Webhooks: POST appends to the server's list, as `createWebhook` does.
  await page.route(/(.*)\/api\/rest\/webhooks\/[^/]+\/deliveries(\?.*)?$/, (r) => r.fulfill(json([])));
  await page.route(/(.*)\/api\/rest\/webhooks(\?.*)?$/, (r) => {
    const req = r.request();
    if (req.method() === "POST") {
      const b = (req.postDataJSON() || {}) as { url?: string; events?: string[] };
      const row: WebhookRow = { id: `wh${webhooks.length + 1}`, url: b.url || "", events: b.events || [] };
      webhooks = [...webhooks, row];
      return r.fulfill({ status: 201, contentType: "application/json", body: JSON.stringify({ id: row.id, url: row.url }) });
    }
    return r.fulfill(json(webhooks));
  });

  // The rest of the page's mount reads. Empty answers, so no panel shows stale
  // anything and no request escapes to a server this spec does not run.
  await page.route(/(.*)\/api\/rest\/settings\/payment-fees(\?.*)?$/, (r) =>
    r.fulfill(json({ rates: [], acknowledged: false })),
  );
  await page.route(/(.*)\/api\/cf\/status(\?.*)?$/, (r) => r.fulfill(json({ linked: false, configured: false })));
  await page.route(/(.*)\/api\/rest\/currency\/rates(\?.*)?$/, (r) => r.fulfill(json({ rates: [] })));
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/(email-settings|settings)(\?.*)?$/, (r) => r.fulfill(json({})));
  await page.route(/(.*)\/api\/meta(\?.*)?$/, (r) => r.fulfill(json({})));

  return { bundleCalls: () => bundleCalls, chip: () => chip, webhooks: () => webhooks };
}

/** The badge in the CHIP panel's header — the element this card is about. */
const badge = (page: Page) => page.locator("#set-panel-chip [data-testid='chip-status-badge']");

test("Saving CHIP credentials flips the status badge without a page reload", async ({ page }) => {
  const stub = await stubSettingsAPI(page);
  await signIn(page);
  await page.goto("/settings?tab=chip");

  // Precondition: the panel is up and reports the server's "not configured".
  await expect(badge(page)).toHaveText(NOT_SET);

  await page.getByPlaceholder("e.g. 5f2b…-uuid").fill("brand-123");
  await page.getByPlaceholder("sk_live_… / sk_test_…").fill("sk_test_abcdefgh1234");
  await page.getByRole("button", { name: SAVE }).click();

  // THE ASSERTION THAT BITES: the badge, read from the live DOM, with no reload.
  await expect(badge(page)).toHaveText(activeMode("test"));

  // …and the page converged on the SERVER's own state, not on guessed fields:
  // the masked tail the PUT does not echo comes from the refetched bundle.
  await expect(page.getByText(stub.chip().secret_key_masked, { exact: false })).toBeVisible();
  expect(stub.bundleCalls()).toBeGreaterThan(2); // mount, mount-after-scope, post-save refresh
});

test("Disconnecting CHIP credentials clears the badge without a page reload", async ({ page }) => {
  const stub = await stubSettingsAPI(page);
  await signIn(page);
  await page.goto("/settings?tab=chip");

  await page.getByPlaceholder("e.g. 5f2b…-uuid").fill("brand-123");
  await page.getByPlaceholder("sk_live_… / sk_test_…").fill("sk_test_abcdefgh1234");
  await page.getByRole("button", { name: SAVE }).click();
  await expect(badge(page)).toHaveText(activeMode("test"));

  // `set.clear` only renders while the page believes credentials are stored —
  // so its presence is itself part of what the save has to publish.
  await page.getByRole("button", { name: CLEAR }).click();

  await expect(badge(page)).toHaveText(NOT_SET);
  expect(stub.chip().configured).toBe(false);
});

test("Adding a webhook lists it without a page reload", async ({ page }) => {
  const stub = await stubSettingsAPI(page);
  await signIn(page);
  await page.goto("/settings?tab=webhooks");

  const url = "https://example.com/jomform-hook";
  await page.getByPlaceholder("https://yourapp.com/jomform-hook").fill(url);
  await page.getByPlaceholder(DICT["set.signingSecret"].en).fill("supersecret1");
  await page.getByRole("button", { name: ADD }).click();

  // `webhooks` is bundle-backed: the new row can only appear if the page re-read
  // the server instead of re-applying the payload it cached at mount.
  await expect(page.getByText(url, { exact: true })).toBeVisible();
  expect(stub.webhooks().map((w) => w.url)).toContain(url);
});
