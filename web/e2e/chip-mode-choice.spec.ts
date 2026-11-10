// Regression guard for card t_8f38dc34 — Settings → CHIP: the test/live mode is
// the merchant's CHOICE, not something read out of the secret key.
//
// THE DEFECT
//   The page derived the mode from the key's first characters (`sk_live_…` →
//   Live, `sk_test_…` → Test, everything else → Test) and the save sent no mode
//   at all, because the API ran the same prefix test. CHIP issues a SEPARATE key
//   per mode in the merchant portal and the key is an opaque bearer token with
//   no published prefix (docs.chip-in.asia → CHIP Collect → Authentication →
//   "Test vs live keys"), so a real merchant key matched nothing: every
//   workspace was stored as chip_mode='test' and this panel said Test beside a
//   live key. Measured on prod when the card was written: 24 test rows, zero
//   live.
//
// WHY A BROWSER MEASUREMENT
//   The claim is about the RENDERED control and the bytes the save sends. A
//   source assertion cannot see either: the pre-fix page compiled, saved, and
//   said "Saved", while the radio the merchant needed did not exist and the PUT
//   body carried no mode. So the real client component is driven in a real
//   browser with only the routes this panel touches stubbed — no Go server, no
//   database, no CHIP network call (the pattern `settings-save-reflects-status.spec.ts`
//   uses).
//
// THE ARMS
//   1. A stored `mode: "test"` renders Test SELECTED and Live not selected.
//   2. The hint says the mode is a choice and names no key prefix (the old copy
//      taught `sk_live_` / `sk_test_`, which CHIP keys do not have).
//   3. Choosing Live and saving sends `mode: "live"` and the badge reads
//      ACTIVE · LIVE, with no reload.
//   4. "Live" is reachable at all — the half of the bug that made the badge
//      permanently say TEST.
import { test, expect, type Page } from "@playwright/test";

import { DICT } from "../src/components/i18nDict";
import { signIn } from "./auth";

const SAVE = DICT["set.save"].en;
const activeMode = (mode: string) => DICT["set.activeMode"].en.replace("{mode}", mode.toUpperCase());
const HINT = DICT["set.modeHint"].en;

const ME = { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "", is_super_admin: false };
const WS = { id: "ws1", name: "Acme Shop", slug: "acme", role: "admin" };

const json = (body: unknown) => ({ status: 200, contentType: "application/json", body: JSON.stringify(body) });

/** The CHIP panel's header badge — the element that reports the stored mode. */
const badge = (page: Page) => page.locator("#set-panel-chip [data-testid='chip-status-badge']");

/**
 * The wire the CHIP panel reads, stubbed. The chip row is STATEFUL: a PUT stores
 * what the request carried and the bundle reports it back, exactly as the API
 * does, so a save that sends the wrong mode cannot pass.
 */
async function stubChipPanel(page: Page, startMode: string) {
  let chip = {
    configured: true,
    verified: true,
    mode: startMode,
    brand_id: "brand-123",
    whitelist: [] as string[],
    email_fallback: "",
    secret_key_masked: "••••1234",
  };
  let lastPut: Record<string, unknown> = {};

  const bundle = () => ({
    me: ME,
    workspaces: [WS],
    chip_status: chip,
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
    currency: "MYR",
  });

  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(bundle())));
  await page.route(/(.*)\/api\/rest\/settings\/chip(\?.*)?$/, (r) => {
    const req = r.request();
    if (req.method() === "PUT") {
      lastPut = (req.postDataJSON() || {}) as Record<string, unknown>;
      chip = {
        ...chip,
        // The API stores the mode the caller chose; it does not read one out of
        // the key (CHIP keys have no prefix).
        mode: lastPut.mode === "live" ? "live" : "test",
        brand_id: String(lastPut.brand_id || ""),
        email_fallback: String(lastPut.email_fallback || ""),
      };
      return r.fulfill(json({ status: "configured", mode: chip.mode, verified: chip.verified }));
    }
    return r.fulfill(json(chip));
  });

  // The rest of the page's mount reads: empty answers, so no panel shows stale
  // anything and no request escapes to a server this spec does not run.
  await page.route(/(.*)\/api\/rest\/settings\/payment-fees(\?.*)?$/, (r) =>
    r.fulfill(json({ rates: [], acknowledged: false })),
  );
  await page.route(/(.*)\/api\/cf\/status(\?.*)?$/, (r) => r.fulfill(json({ linked: false, configured: false })));
  await page.route(/(.*)\/api\/rest\/currency\/rates(\?.*)?$/, (r) => r.fulfill(json({ rates: [] })));
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/(email-settings|settings)(\?.*)?$/, (r) => r.fulfill(json({})));
  await page.route(/(.*)\/api\/meta(\?.*)?$/, (r) => r.fulfill(json({})));

  return () => lastPut;
}

test("a stored mode renders as the selected choice, and choosing Live saves live", async ({ page }, testInfo) => {
  const lastPut = await stubChipPanel(page, "test");
  await signIn(page, testInfo.project.use.baseURL as string | undefined);
  await page.goto("/settings?tab=chip");

  const testRadio = page.getByTestId("chip-mode-test");
  const liveRadio = page.getByTestId("chip-mode-live");

  // ARM 1: the stored mode is the SELECTED option. Before the fix there was no
  // option at all — the panel printed "Auto-detected: Test".
  await expect(badge(page)).toHaveText(activeMode("test"));
  await expect(testRadio).toBeChecked();
  await expect(liveRadio).not.toBeChecked();
  await expect(page.getByTestId("chip-mode-live")).toBeEnabled();

  // ARM 2: the copy describes a choice and names no prefix.
  await expect(page.getByText(HINT)).toBeVisible();
  expect(HINT).not.toMatch(/sk_(live|test)_/);
  await expect(page.getByText(/auto-detected/i)).toHaveCount(0);

  // The DEFAULT reading a fresh visitor gets (no storage, no language cookie):
  // Test selected, Live available. Assert VISIBLE first — a hidden node would
  // let the screenshot below capture an empty panel while these checks passed.
  await expect(page.getByTestId("chip-mode-live")).toBeVisible();
  await expect(page.getByTestId("chip-mode-test")).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("chip-mode-default.png"), fullPage: false });

  // ARM 3 + 4: choosing Live is possible, and the save carries it.
  await liveRadio.check();
  await expect(liveRadio).toBeChecked();
  await page.getByRole("button", { name: SAVE }).click();

  await expect(badge(page)).toHaveText(activeMode("live"));
  expect(lastPut().mode, `PUT body: ${JSON.stringify(lastPut())}`).toBe("live");

  await page.screenshot({ path: testInfo.outputPath("chip-mode-live.png"), fullPage: false });
});
