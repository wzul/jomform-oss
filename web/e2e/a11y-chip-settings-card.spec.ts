// Settings → CHIP: the approved card redesign, measured on the RENDERED page
// (card t_9efc5ed7).
//
// THE TWO DEFECTS THIS GUARD IS BUILT AROUND
//   1. THE TEST/LIVE MODE CONTROL ASKED A QUESTION NOBODY READ. `chip_mode` never
//      chose the API endpoint (`ChipAPIBaseUrl` does), never blocked anything and
//      never changed a payment — the key itself decides what CHIP does. Card
//      t_8f38dc34 had already stopped INFERRING the label from the key prefix (CHIP
//      keys are opaque bearer tokens with no published prefix); it made the
//      merchant CHOOSE it instead. The control is removed, and with it the two
//      duplicated "✓ API credentials verified" chips and the literal "e.g.
//      5f2b…-uuid" placeholder, whose "…" was a CHARACTER in the string rather than
//      CSS overflow, so the Brand ID field read as truncated before it held
//      anything (Wan: "brand id tu ada truncate .... uuid").
//   2. THE CONNECTION STATE WAS SAID THREE TIMES and the actions a merchant needs
//      after pasting a key (test it, save it) were not together at the bottom.
//
// WHY A BROWSER MEASUREMENT
//   Every claim here is a property of the RENDERED card: "no radiogroup exists",
//   "exactly one status pill", "no placeholder carries an ellipsis", "the boxes are
//   announced as checkboxes", "the fallback email is inside a CLOSED disclosure".
//   A source grep cannot see any of them, and the page compiles, saves and says
//   "Saved" with all five wrong. The real client component is driven with only the
//   routes this card touches stubbed — no Go server, no DB, no CHIP network call
//   (the pattern `settings-chip-mobile-actions.spec.ts` uses).
//
// Storage is cleared and the locale pinned to `en` on every case: the browser keeps
// `localStorage` between runs, and a `jf_lang`/`jf_settings_tab` left behind by an
// earlier card would render this page in a state no new visitor has.
import { test, expect, type Page } from "@playwright/test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

import { DICT } from "../src/components/i18nDict";
import { signIn } from "./auth";
import { blockersOf, gateScan } from "./axeGate";

const PHONE = { width: 390, height: 844 } as const;
const DESKTOP = { width: 1440, height: 900 } as const;

const CONNECTED = DICT["set.connected"].en;
const NOT_CONNECTED = DICT["set.notConnected"].en;
const SAVED_ENDS = DICT["set.savedEncryptedEnds"].en.replace("{tail}", "4f2a");
const REPLACE = DICT["set.replace"].en;
const ADVANCED = DICT["set.advanced"].en;
const TEST_CONNECTION = DICT["set.testConnection"].en;
const TEST_OK = DICT["set.testConnectionOk"].en;
const EMAIL_FALLBACK = DICT["set.emailFallback"].en;

// The payment methods the PAGE declares, read out of its own source: the count is
// about the page that ships, so adding a method does not red this file.
const METHOD_IDS = (() => {
  const src = readFileSync(join(__dirname, "../src/app/settings/page.tsx"), "utf8");
  const decl = src.match(/const METHODS\s*=\s*\[([\s\S]*?)\n\];/);
  if (!decl) throw new Error("settings/page.tsx no longer declares METHODS — this guard reads it to derive the checkbox count");
  const ids = [...decl[1].matchAll(/id:\s*"([^"]+)"/g)].map((m) => m[1]);
  if (!ids.length) throw new Error("settings/page.tsx declares METHODS with no ids");
  return ids;
})();

const ME = { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "en", is_super_admin: true };
const WS = { id: "ws1", name: "Demo", slug: "demo", role: "admin" };

type ChipRow = {
  configured: boolean;
  verified: boolean;
  mode: string;
  brand_id: string;
  whitelist: string[];
  email_fallback: string;
  secret_key_masked: string;
};

/** Credentials stored AND confirmed by the live CHIP check at save time. */
const CONFIGURED: ChipRow = {
  configured: true,
  verified: true,
  mode: "live",
  brand_id: "brand-existing",
  whitelist: ["card"],
  email_fallback: "",
  secret_key_masked: "••••4f2a",
};

const json = (body: unknown) => ({ status: 200, contentType: "application/json", body: JSON.stringify(body) });

/**
 * The routes this card reads. `state` is returned so the arms can assert what the
 * server was actually ASKED (the Test-connection arm must not have written).
 */
async function stubAPI(page: Page) {
  const state = {
    puts: 0,
    tests: [] as string[],
    chip: { ...CONFIGURED } as ChipRow,
  };
  const bundle = () => ({
    me: ME,
    workspaces: [WS],
    chip_status: state.chip,
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

  // Catch-all FIRST, the page's own routes after it (Playwright takes the most
  // recently registered matching route).
  await page.route(/(.*)\/api\/.*$/, (r) => r.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(bundle())));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(ME)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "en" })));
  await page.route(/(.*)\/api\/rest\/settings\/chip\/test(\?.*)?$/, (r) => {
    const body = (r.request().postDataJSON() || {}) as { secret_key?: string };
    state.tests.push(body.secret_key || "");
    return r.fulfill(json({ ok: true, verified: true }));
  });
  await page.route(/(.*)\/api\/rest\/settings\/chip(\?.*)?$/, (r) => {
    if (r.request().method() === "PUT") {
      state.puts += 1;
      return r.fulfill(json({ status: "configured", mode: state.chip.mode, verified: true }));
    }
    return r.fulfill(json(state.chip));
  });
  await page.route(/(.*)\/api\/rest\/settings\/payment-fees(\?.*)?$/, (r) =>
    r.fulfill(json({ rates: [], acknowledged: false })),
  );
  await page.route(/(.*)\/api\/cf\/status(\?.*)?$/, (r) => r.fulfill(json({ linked: false, configured: false })));
  await page.route(/(.*)\/api\/rest\/currency\/rates(\?.*)?$/, (r) => r.fulfill(json({ rates: [] })));
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/(email-settings|settings)(\?.*)?$/, (r) => r.fulfill(json({})));
  await page.route(/(.*)\/api\/meta(\?.*)?$/, (r) => r.fulfill(json({})));
  return state;
}

/** A FRESH browser state, then the CHIP tab, at `viewport`. */
async function openChipTab(page: Page, viewport: { width: number; height: number }) {
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("jf_workspace", "demo");
      localStorage.setItem("jf_lang", "en");
      localStorage.setItem("jf_settings_tab", "chip");
    } catch {
      /* storage unavailable */
    }
  });
  await page.setViewportSize(viewport);
  await page.goto("/settings?tab=chip", { waitUntil: "domcontentloaded" });
  await expect(page.locator("#set-panel-chip")).toBeVisible({ timeout: 45_000 });
  // The pill renders only once `chip_status` has landed; waiting for it is what
  // makes "exactly one pill" a settled read rather than a race with mount.
  await expect(page.locator("#set-panel-chip [data-testid='chip-status-badge']")).toBeVisible({ timeout: 45_000 });
}

test("the CHIP card renders NO mode radiogroup and exactly ONE status pill (en @390)", async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
  await stubAPI(page);
  await openChipTab(page, PHONE);

  const counts = (await page.evaluate(`(() => {
    const panel = document.querySelector("#set-panel-chip");
    return {
      radiogroups: panel.querySelectorAll('[role="radiogroup"]').length,
      radios: panel.querySelectorAll('input[type="radio"]').length,
      pills: panel.querySelectorAll("[data-testid='chip-status-badge']").length,
      verifiedChips: (panel.textContent || "").includes("API credentials verified"),
      pillText: (panel.querySelector("[data-testid='chip-status-badge']") || {}).textContent,
    };
  })()`)) as {
    radiogroups: number;
    radios: number;
    pills: number;
    verifiedChips: boolean;
    pillText: string;
  };

  expect(counts.radiogroups, "the CHIP card renders a radiogroup again — the removed Test/Live mode control").toBe(0);
  expect(counts.radios, "the CHIP card renders a radio input again (the removed mode control)").toBe(0);
  expect(counts.pills, "the CHIP card's connection state is said more than once — it must be ONE pill").toBe(1);
  expect(counts.pillText?.trim(), `the pill reads ${JSON.stringify(counts.pillText)}, not the connection state`).toBe(CONNECTED);
  expect(counts.verifiedChips, "the duplicated '✓ API credentials verified' chip is back on a field").toBe(false);
});

test("the pill is truthful in the other two states (not connected, stored but unverified)", async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
  const state = await stubAPI(page);
  // Not configured at all: the same one pill, in the muted state.
  state.chip = { configured: false, verified: false, mode: "", brand_id: "", whitelist: [], email_fallback: "", secret_key_masked: "" };
  await openChipTab(page, PHONE);
  const pill = page.locator("#set-panel-chip [data-testid='chip-status-badge']");
  await expect(pill).toHaveCount(1);
  await expect(pill).toHaveText(NOT_CONNECTED);
  await expect(pill).toHaveAttribute("data-state", "off");

  // Stored but the live CHIP check did not pass: neither "Connected" nor
  // "Not connected" would be true, so the pill says so.
  state.chip = { ...CONFIGURED, verified: false };
  await page.reload({ waitUntil: "domcontentloaded" });
  await expect(pill).toBeVisible({ timeout: 45_000 });
  await expect(pill).toHaveText(DICT["set.notVerified"].en);
  await expect(pill).toHaveAttribute("data-state", "unverified");
});

test("no placeholder in the CHIP card carries an ellipsis (the old `5f2b…-uuid`)", async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
  await stubAPI(page);
  await openChipTab(page, PHONE);

  // A saved key is a masked LINE by default, so the paste field is behind Replace:
  // open it, or the Secret Key placeholder is not on screen to be read.
  await page.getByTestId("chip-secret-replace").click();

  const placeholders = (await page.evaluate(`(() => {
    const panel = document.querySelector("#set-panel-chip");
    return [...panel.querySelectorAll("[placeholder]")].map((el) => ({
      testid: el.getAttribute("data-testid") || el.id || el.tagName,
      value: el.getAttribute("placeholder"),
    }));
  })()`)) as { testid: string; value: string }[];

  expect(placeholders.length, "the CHIP card renders no placeholder at all — this arm would pass vacuously").toBeGreaterThanOrEqual(2);
  for (const { testid, value } of placeholders) {
    for (const ell of ["…", "..."]) {
      expect(
        (value || "").includes(ell),
        `${testid}'s placeholder contains ${JSON.stringify(ell)} (${JSON.stringify(value)}) — the field reads as truncated. ` +
          `The failing case this replaced was the literal "e.g. 5f2b…-uuid".`,
      ).toBe(false);
    }
  }
  // And the placeholders name the ACTION, in the shipped copy.
  await expect(page.getByTestId("chip-brand-id")).toHaveAttribute("placeholder", DICT["set.brandIdPh"].en);
  await expect(page.getByTestId("chip-secret-key")).toHaveAttribute("placeholder", DICT["set.secretKeyPh"].en);
});

test("a saved secret is a masked LINE with a Replace action, and the email folds into Advanced", async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
  await stubAPI(page);
  await openChipTab(page, PHONE);

  // The masked line is on screen; the paste field is NOT (it is a second field a
  // merchant reads as "my key is gone").
  await expect(page.getByTestId("chip-secret-masked")).toHaveText(SAVED_ENDS);
  await expect(page.getByTestId("chip-secret-key")).toHaveCount(0);

  // Replace is an ACTION that reveals the paste field…
  await page.getByTestId("chip-secret-replace").click();
  await expect(page.getByTestId("chip-secret-key")).toBeVisible();
  await expect(page.getByTestId("chip-secret-masked")).toHaveCount(0);
  // …and "Keep saved key" puts the masked line back without saving anything.
  await page.getByTestId("chip-secret-keep").click();
  await expect(page.getByTestId("chip-secret-masked")).toHaveText(SAVED_ENDS);

  // The fallback email lives inside the Advanced disclosure, which starts CLOSED
  // because it is a rare one-time-form setting — not a primary field.
  const advanced = page.getByTestId("chip-advanced");
  await expect(advanced).toBeVisible();
  await expect(advanced.locator("summary")).toHaveText(ADVANCED);
  const open = await advanced.evaluate((el: HTMLDetailsElement) => el.open);
  expect(open, "the Advanced disclosure is open on a card with no stored fallback email").toBe(false);
  // A closed <details> keeps its content in the DOM, so the state read is
  // VISIBILITY, not textContent.
  await expect(page.getByTestId("chip-email-fallback")).toBeHidden();
  await advanced.locator("summary").click();
  await expect(page.getByTestId("chip-email-fallback")).toBeVisible();
  await expect(advanced).toContainText(EMAIL_FALLBACK);
});

test("payment methods are announced as checkboxes with aria-checked, two to a row (en @390)", async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
  await stubAPI(page);
  await openChipTab(page, PHONE);

  const boxes = page.locator("#set-panel-chip [role='checkbox']");
  await expect(boxes, `the panel does not announce one checkbox per payment method (${METHOD_IDS.join(", ")})`).toHaveCount(
    METHOD_IDS.length,
  );
  // Every box carries the state by name, not only by paint.
  const states = (await boxes.evaluateAll((els) =>
    els.map((el) => ({
      id: el.getAttribute("data-testid"),
      aria: el.getAttribute("aria-checked"),
      checked: (el as HTMLInputElement).checked,
    })),
  )) as { id: string; aria: string | null; checked: boolean }[];
  for (const s of states) {
    expect(s.aria, `${s.id} carries no aria-checked — its state is not announced`).not.toBeNull();
    expect(s.aria === "true", `${s.id}: aria-checked (${s.aria}) disagrees with the control's checked state (${s.checked})`).toBe(
      s.checked,
    );
  }
  expect(states.filter((s) => s.aria === "true").length, "exactly one method is stored, so exactly one box is checked").toBe(1);

  // TWO columns at a phone width — the approved grid, not one column that only
  // becomes two on a desktop.
  const columns = (await page
    .locator("#set-panel-chip [aria-labelledby='chip-pm-label']")
    .evaluate((el) => getComputedStyle(el).gridTemplateColumns)) as string;
  expect(columns.trim().split(/\s+/).length, `the payment-method grid is ${JSON.stringify(columns)} — not two columns`).toBe(2);
});

test("Test connection runs the live CHIP check WITHOUT saving (en @390)", async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
  const state = await stubAPI(page);
  await openChipTab(page, PHONE);

  // With the stored key in force, the button tests THAT key (empty body).
  await page.getByTestId("chip-test-connection").click();
  await expect(page.getByText(TEST_OK).first()).toBeVisible({ timeout: 15_000 });
  expect(state.tests, "the Test connection button did not reach the API").toEqual([""]);
  expect(state.puts, "testing the connection WROTE the credentials — the action must not save").toBe(0);

  // With a pasted key it tests the key the merchant is about to save.
  await page.getByTestId("chip-secret-replace").click();
  await page.getByTestId("chip-secret-key").fill("brand-new-secret-key");
  await page.getByTestId("chip-test-connection").click();
  await expect.poll(() => state.tests.length, { message: "the second Test connection did not reach the API" }).toBe(2);
  expect(state.tests[1], "the test did not carry the pasted key").toBe("brand-new-secret-key");
  expect(state.puts, "testing a pasted key WROTE the credentials — the action must not save").toBe(0);
});

test("axe: the CHIP panel has no critical/serious violation at 390px or 1440px", async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
  const state = await stubAPI(page);

  const scan = async (label: string, viewport: { width: number; height: number }) => {
    await openChipTab(page, viewport);
    // The panel really painted this card before it is scanned (a scoped `include`
    // throws on nothing, but naming the heading makes the failure readable).
    await expect(page.locator("#set-panel-chip").getByRole("heading", { level: 2 })).toHaveText(DICT["set.chipTitle"].en);

    const results = await gateScan(page, "#set-panel-chip");
    const blockers = blockersOf(results);
    if (blockers.length > 0) {
      for (const v of blockers) {
        console.log(`  [${v.impact}] ${v.id}: ${v.help}`);
        for (const n of v.nodes.slice(0, 5)) console.log(`      ${n.target.join(" ")}`);
      }
    }
    expect(
      blockers.map((v) => v.id),
      `${label}: the CHIP panel has ${blockers.length} critical/serious axe violation(s)`,
    ).toEqual([]);
  };

  await scan("en@390 connected", PHONE);
  await scan("en@1440 connected", DESKTOP);

  // The pill has three states, and this card paints each of them on a tinted
  // fill; scanning only the green one let the unconfigured pill's muted ink
  // measure 4.31:1 on `--jf-surface-2` without any arm noticing (card
  // t_9efc5ed7 — the same class of miss as the green text on the success tint).
  state.chip = { configured: false, verified: false, mode: "", brand_id: "", whitelist: [], email_fallback: "", secret_key_masked: "" };
  await scan("en@390 not connected", PHONE);
});
