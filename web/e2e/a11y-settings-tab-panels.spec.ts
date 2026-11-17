// /settings' THIRTEEN tab panels, every one of them axe-scanned with its content
// POPULATED (card t_ef7d5c04).
//
// THE GAP. `/settings` was in the a11y gate through ONE arm —
// `e2e/a11y-settings-first-run-tab.spec.ts`'s `axe:` test — and that arm renders
// the DEFAULT tab (the first tab the role filter shows, "Custom domains") at ONE
// width (320px). Two further specs touch the strip (`a11y-profile-tabs.spec.ts`
// reads the ARIA association of the strips it passes through, `settings-tabs.spec.ts`
// walks the panels and runs no axe at all), so of the thirteen panels the page
// declares, exactly ONE was ever inside an axe run.
//
// MEASURED on the base tree `3dd08e479` (the tree this spec was written against,
// built, API stubbed with every panel POPULATED — one verified + one pending
// domain, two webhooks with a failed and a succeeded delivery, a fee table, a
// member list, an invite, legal pages, currency rates) at 1440x900 and 390x844:
// EIGHT critical/serious violations per width, none of which the gate had ever
// seen:
//
//   domains    color-contrast (serious)  three outline buttons/links painting
//                                       `--jf-primary` (#5e6ad2, 4.43:1) on the
//                                       `--jf-bg-soft` panel row
//   webhooks   heading-order (moderate) the delivery-history <h4> under an <h2>
//   emel       label (critical)         the subject/body fields named by a
//   storefront label (critical)         sibling <label> with no for/id
//   currency   select-name (critical)
//   legal      label (critical) x9      every business field in LegalTab.tsx
//   members    select-name (critical),  the two role selects in MembersTab.tsx
//              label-title-only (serious)
//
// Those defects were FIXED by card t_1436a4aa (PR #1766) — its own acceptance was
// a clean axe run over the panels, measured on a scratch harness that was never
// committed. This file is that harness, committed: the coverage, not the fix.
// The same shape was filed and fixed for /form-settings' menu tab as t_e5b01cf0.
//
// WHAT THIS SPEC ASSERTS, per panel and per width:
//   1. THE PANEL IS THE SELECTED TAB'S — clicking `#set-tab-<id>` paints
//      `#set-panel-<id>`, whose first `<h2>` is that panel's own heading. A tab
//      that painted an empty card, or painted the PREVIOUS panel's markup, would
//      make every reading below a reading of the wrong document.
//   2. THE PANEL RENDERED ITS CONTENT — a text floor and a control count per
//      panel, MEASURED on the populated stubs. An empty panel proves nothing
//      about the rows inside it (the row is where all eight defects were).
//   3. NO critical/serious axe violation — through `gateScan()` from
//      `e2e/axeGate.ts`, i.e. the SAME rule set and the same failing impacts as
//      `a11y.spec.ts` and the rest of the family, over the WHOLE document.
//   4. THE SCAN IS NOT VACUOUS — axe has to have evaluated rule-nodes on each
//      panel, so a 0-violation reading from a scan of nothing cannot pass.
//   5. THE `heading-order` RULE, which the gate's critical/serious bar cannot
//      see: the webhooks panel's delivery history is expanded and its heading
//      order asserted, because that is the second of the eight measured defects
//      and a moderate violation is exactly what the bar filters out.
//
// HOW IT BITES (two proofs, both run by the gate itself):
//   * THE CONTROL ARM restores the PRE-FIX DOM in the live page — a text input
//     with no label inside the domains panel — and requires the SAME scan to
//     name it (`label`, critical), then removes it and requires the panel clean
//     again. A guard that cannot go red is not a guard.
//   * `expectGateSeesAriaViolation` (e2e/axeGate.ts) plants a prohibited ARIA
//     state in the panel and requires the scan to name that node, so a rule set
//     narrowed in the future cannot leave these green readings vacuous.
//
// FRESH BROWSER STATE ON EVERY ARM: storage cleared and the language pinned to
// `en` (`e2e/settle.ts` + `e2e/auth.ts` pattern). A `jf_lang` left behind by an
// earlier card has already produced a false reading on this board — a page
// rendered Arabic for a reader whose profile said `en`. The session is declared
// explicitly (`e2e/auth.ts`) because the dashboard paths are server-gated.
//
// THE TABS ARE READ OUT OF THE PAGE, not typed: `TAB_IDS` and `ADMIN_ONLY_TABS`
// are parsed from `src/app/settings/page.tsx`, so a tab added there without a
// heading witness here fails at import as a STALE GUARD instead of silently
// dropping out of the walk. `a11yAuthedCoverage.test.ts` pins the same two
// things from the outside (each declared tab has a witness, and this arm's scan
// is still the gate's), so an arm deleted or narrowed fails the unit suite too.
import { test, expect, type Page } from "@playwright/test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

import { signIn } from "./auth";
import { blockersOf, expectGateSeesAriaViolation, gateScan } from "./axeGate";
import { DICT } from "../src/components/i18nDict";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page
// renders. See e2e/auth.ts.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

/* ─── the page's own tab declaration ────────────────────────────────────────── */

// `__dirname` (CommonJS) rather than `import.meta.url` — package.json has no
// "type": "module". Same reader shape as `e2e/a11y-settings-first-run-tab.spec.ts`
// and `e2e/settings-tabs.spec.ts`.
const PAGE_SRC = readFileSync(join(__dirname, "../src/app/settings/page.tsx"), "utf8");

function tabIdsIn(declaration: string): string[] {
  const m = PAGE_SRC.match(new RegExp(`const ${declaration}[^=]*=\\s*\\[([^\\]]*)\\]`));
  if (!m) {
    throw new Error(
      `settings/page.tsx no longer declares ${declaration} — this guard reads it to derive the tab panels it walks`,
    );
  }
  return [...m[1].matchAll(/"([^"]+)"/g)].map((x) => x[1]);
}

const TAB_IDS = tabIdsIn("TAB_IDS");
const ADMIN_ONLY = tabIdsIn("ADMIN_ONLY_TABS");

/**
 * tab id -> the dictionary key of the FIRST `<h2>` its panel paints. That heading
 * is the panel's own witness that THIS tab rendered rather than the previous
 * one's markup, and it is what "the panel below the strip is the selected tab's"
 * is asserted against.
 *
 * PARSED (not just typed) by `a11yAuthedCoverage.test.ts`, so this literal keeps
 * ONE `tab: "dict.key"` pair per line at two-space indent — re-indenting it
 * breaks that reader, not this walk.
 */
const TAB_PANEL_HEADING: Record<string, keyof typeof DICT> = {
  chip: "set.chipTitle",
  domains: "set.domainsTitle",
  tracking: "set.trackingTitle",
  einvoice: "set.einvoiceTitle",
  fees: "set.feesTitle",
  webhooks: "set.webhooksTitle",
  emel: "set.emelTitle",
  members: "mem.title",
  storefront: "set.sfTitle",
  legal: "set.legalTitle",
  currency: "set.currencyTitle",
  affiliates: "set.affiliatesTitle",
  workspace: "ws.identityTitle",
};

/**
 * The rendered-text floor and control count per panel, MEASURED on the populated
 * stubs below at 1440x900 (each is a FLOOR: a panel that renders more is fine, a
 * panel that renders its empty/loading state is not).
 */
const PANEL_FLOOR: Record<string, { text: number; controls: number }> = {
  chip: { text: 400, controls: 8 },
  domains: { text: 600, controls: 8 },
  tracking: { text: 120, controls: 2 },
  einvoice: { text: 120, controls: 2 },
  fees: { text: 700, controls: 4 },
  webhooks: { text: 300, controls: 8 },
  emel: { text: 350, controls: 6 },
  members: { text: 250, controls: 5 },
  storefront: { text: 1100, controls: 12 },
  legal: { text: 500, controls: 12 },
  currency: { text: 800, controls: 2 },
  affiliates: { text: 200, controls: 4 },
  workspace: { text: 600, controls: 4 },
};

// The derived tables have to describe the page. Fail BEFORE a browser starts,
// naming the tab, so a renamed/added tab is reported as a STALE GUARD rather
// than as a broken panel.
{
  if (TAB_IDS.length < 2) {
    throw new Error(`settings/page.tsx declares ${TAB_IDS.length} tab(s) — this guard walks the page's panels`);
  }
  for (const id of TAB_IDS) {
    if (!TAB_PANEL_HEADING[id]) {
      throw new Error(
        `settings/page.tsx declares the tab "${id}" and this guard has no panel heading for it — add its title key ` +
          `to TAB_PANEL_HEADING so "every panel was scanned" stays checked`,
      );
    }
    if (!Object.prototype.hasOwnProperty.call(DICT, TAB_PANEL_HEADING[id])) {
      throw new Error(`TAB_PANEL_HEADING maps "${id}" to ${TAB_PANEL_HEADING[id]}, which is not in the dictionary`);
    }
    if (!PANEL_FLOOR[id]) {
      throw new Error(
        `settings/page.tsx declares the tab "${id}" and this guard has no rendered-content floor for it — add one ` +
          `to PANEL_FLOOR (an empty panel proves nothing about the rows inside it)`,
      );
    }
  }
  for (const id of ADMIN_ONLY) {
    if (!TAB_IDS.includes(id)) {
      throw new Error(`ADMIN_ONLY_TABS names "${id}", which is not in TAB_IDS — one of the two declarations moved`);
    }
  }
}

/* ─── the session + the POPULATED API the panels read ──────────────────────── */

const ME = {
  id: "u1",
  email: "merchant@example.com",
  name: "Merchant",
  lang: "en",
  avatar_url: "",
  phone: "",
  timezone: "",
  totp_enabled: false,
  // BOTH admin answers, as `a11y-settings-first-run-tab.spec.ts` does: `role`
  // filters the strip (so all thirteen tabs render) and `is_super_admin` is the
  // other gate the app reads in a different place.
  is_super_admin: true,
  default_workspace_id: "ws1",
};

const WORKSPACES = [{ id: "ws1", name: "Demo", slug: "demo", role: "admin" }];

/** One verified + one pending domain: the verified row is the one that paints
 *  the TLS controls and the live link, the pending one the CNAME row. */
const DOMAINS = [
  {
    id: "d1",
    domain: "shop.example.com",
    verified: true,
    auto_cname: "created",
    tls_mode: "auto",
    tls_status: "active",
    tls_email: "ops@example.com",
  },
  {
    id: "d2",
    domain: "pending.example.com",
    verified: false,
    auto_cname: "failed",
    tls_mode: "auto",
    tls_status: "pending",
  },
];

const WEBHOOKS = [
  { id: "w1", url: "https://yourapp.example.com/hook-one", events: ["sale.paid", "sale.failed"] },
  { id: "w2", url: "https://yourapp.example.com/hook-two", events: ["*"] },
];

/** The FIRST row is the failed one — the panel's status indicator reads
 *  `deliveries[0]`, and `bg-[var(--jf-danger-bg)]` is painted on any row whose
 *  `success` is false. Both states are present so the tinted row is scanned. */
const DELIVERIES = [
  {
    id: "dv2",
    event: "sale.failed",
    success: false,
    status_code: 500,
    error: "connection reset by peer",
    attempt: 6,
    created_at: "2026-09-02T03:04:05Z",
  },
  {
    id: "dv1",
    event: "sale.paid",
    success: true,
    status_code: 200,
    error: "",
    attempt: 1,
    created_at: "2026-09-01T03:04:05Z",
  },
];

/** Production carries 27 BNM rates (measured on v1.51.0, workspace `wan`), and the
 *  COUNT is what puts the currency panel rates box into the state this spec
 *  exists to scan (card t_8b4297e8). This stub shipped TWO rates, which never
 *  exceed the box `max-h-64`, so `scrollable-region-focusable` — the serious
 *  violation the box really had at 390px — was never evaluated on it and the
 *  arm read green over a container that is not clean in production. Keep the
 *  list at the production population: `readCurrencyGrid` pins the rendered tile
 *  count to it at both widths. */
const CURRENCY_RATES = [
  { currency_code: "USD", unit: 1, rate: 0.2128 },
  { currency_code: "SGD", unit: 1, rate: 0.2874 },
  { currency_code: "EUR", unit: 1, rate: 0.1965 },
  { currency_code: "GBP", unit: 1, rate: 0.1682 },
  { currency_code: "JPY", unit: 100, rate: 3.1874 },
  { currency_code: "CNY", unit: 1, rate: 1.5412 },
  { currency_code: "HKD", unit: 1, rate: 1.6612 },
  { currency_code: "AUD", unit: 1, rate: 0.3241 },
  { currency_code: "CAD", unit: 1, rate: 0.2903 },
  { currency_code: "CHF", unit: 1, rate: 0.1876 },
  { currency_code: "BND", unit: 1, rate: 0.2871 },
  { currency_code: "AED", unit: 1, rate: 0.7813 },
  { currency_code: "SAR", unit: 1, rate: 0.7982 },
  { currency_code: "QAR", unit: 1, rate: 0.7746 },
  { currency_code: "KWD", unit: 1, rate: 0.0654 },
  { currency_code: "THB", unit: 1, rate: 7.2418 },
  { currency_code: "IDR", unit: 1000, rate: 3.3512 },
  { currency_code: "PHP", unit: 1, rate: 12.0841 },
  { currency_code: "INR", unit: 1, rate: 17.7326 },
  { currency_code: "PKR", unit: 1, rate: 59.1420 },
  { currency_code: "BDT", unit: 1, rate: 25.6811 },
  { currency_code: "LKR", unit: 1, rate: 65.4420 },
  { currency_code: "NPR", unit: 1, rate: 28.3741 },
  { currency_code: "VND", unit: 1000, rate: 5.4812 },
  { currency_code: "KRW", unit: 100, rate: 297.4412 },
  { currency_code: "TWD", unit: 1, rate: 6.8114 },
  { currency_code: "NZD", unit: 1, rate: 0.3542 },
];

const BUNDLE = {
  me: ME,
  workspaces: WORKSPACES,
  chip_status: {
    configured: true,
    verified: true,
    mode: "sandbox",
    brand_id: "brand-1234",
    secret_key_masked: "••••4321",
    email_fallback: "store@example.com",
    whitelist: ["fpx", "card"],
  },
  keys: [{ id: "k1", name: "Agent key", scopes: ["forms:read"] }],
  domains: DOMAINS,
  webhooks: WEBHOOKS,
  social: [],
  einvoice: { tin: "C1234567890", msic: "62010" },
  products: [],
  summary: { counts: {}, revenue_sen: 0 },
  sales: { items: [], page: 1, limit: 20, total: 0 },
  form_analytics: [],
  product_analytics: [],
};

const WORKSPACE_SETTINGS = {
  business_name: "Demo Shop",
  company_reg: "202601234567",
  contact_phone: "+60123456789",
  contact_address: "1 Jalan Demo, Kuala Lumpur",
  privacy_url: "https://demo.example.com/privacy",
  terms_url: "https://demo.example.com/terms",
  refund_url: "https://demo.example.com/refund",
  footer_text: "© Demo Shop",
  indexing: true,
  sound_enabled: false,
  spam_judge_enabled: true,
  currency: "MYR",
};

/**
 * Every list/record the thirteen panels read, in the shape its Go handler writes.
 * Registered AFTER the catch-all: Playwright resolves matching routes in REVERSE
 * registration order, so the specific ones win.
 */
async function stubAPI(page: Page): Promise<void> {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  await page.route(/(.*)\/api\/.*$/, (r) => r.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(ME)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "en" })));
  await page.route(/(.*)\/api\/workspaces(\?.*)?$/, (r) => r.fulfill(json(WORKSPACES)));
  await page.route(/(.*)\/api\/rest\/domains(\?.*)?$/, (r) => r.fulfill(json(DOMAINS)));
  await page.route(/(.*)\/api\/rest\/webhooks(\?.*)?$/, (r) => r.fulfill(json(WEBHOOKS)));
  await page.route(/(.*)\/api\/rest\/webhooks\/[^/?]+\/deliveries(\?.*)?$/, (r) => r.fulfill(json(DELIVERIES)));
  await page.route(/(.*)\/api\/rest\/settings\/payment-fees(\?.*)?$/, (r) =>
    r.fulfill(
      json({
        rates: [
          {
            method: "fpx",
            fee_enabled: true,
            platform_fee_sen: 100,
            fee_fixed_sen: 0,
            fee_percent_bp: 0,
            rate_label: "FPX RM1.00",
          },
          // A LEGACY row, so the amber box (three wrapped labels + the
          // move-to-flat action) is on the panel and inside the scan.
          {
            method: "card",
            fee_enabled: true,
            platform_fee_sen: 200,
            fee_fixed_sen: 50,
            fee_percent_bp: 80,
            legacy_grossup: true,
            rate_label: "Card 2.00%",
          },
        ],
        acknowledged: true,
        acknowledged_by: "Merchant",
      }),
    ),
  );
  // Linked, so the auto-CNAME action and the "linked" notice are on the domains
  // panel — that action is one of the three controls the contrast defect was on.
  await page.route(/(.*)\/api\/cf\/status(\?.*)?$/, (r) => r.fulfill(json({ linked: true, configured: true })));
  await page.route(/(.*)\/api\/workspaces\/ws1\/email-settings(\?.*)?$/, (r) =>
    r.fulfill(
      json({
        receipt_subject: "Your receipt from Demo",
        receipt_body: "Thanks for your order, {buyer_name}.",
        receipt_enabled: true,
        notify_merchant: true,
        merchant_notify_email: "ops@example.com",
        html: true,
      }),
    ),
  );
  await page.route(/(.*)\/api\/workspaces\/ws1\/settings(\?.*)?$/, (r) => r.fulfill(json(WORKSPACE_SETTINGS)));
  await page.route(/(.*)\/api\/workspaces\/ws1\/affiliates(\?.*)?$/, (r) =>
    r.fulfill(
      json([
        {
          id: "a1",
          code: "DEMO10",
          name: "Aina",
          email: "aina@example.com",
          commission_pct: 10,
          referral_url: "https://demo.jomform.com/?ref=DEMO10",
        },
      ]),
    ),
  );
  await page.route(/(.*)\/api\/workspaces\/ws1\/members(\?.*)?$/, (r) =>
    r.fulfill(
      json([
        { user_id: "u1", email: "merchant@example.com", name: "Merchant", role: "admin" },
        { user_id: "u2", email: "staff@example.com", name: "Staff", role: "manage" },
      ]),
    ),
  );
  await page.route(/(.*)\/api\/workspaces\/ws1\/invites(\?.*)?$/, (r) =>
    r.fulfill(json([{ id: "i1", email: "new@example.com", role: "manage", created_at: "2026-09-01T00:00:00Z" }])),
  );
  await page.route(/(.*)\/api\/workspaces\/ws1\/legal(\?.*)?$/, (r) =>
    r.fulfill(json({ privacy: "Privacy policy text", terms: "Terms of service text", refund: "Refund policy text" })),
  );
  await page.route(/(.*)\/api\/workspaces\/ws1\/deletion-check$/, (r) =>
    r.fulfill(
      json({
        id: "ws1",
        name: "Demo",
        slug: "demo",
        is_owner: true,
        role: "admin",
        last_workspace: false,
        empty: false,
        blockers: [],
        can_delete: true,
      }),
    ),
  );
  await page.route(/(.*)\/api\/workspaces\/trash(\?.*)?$/, (r) => r.fulfill(json([])));
  await page.route(/(.*)\/api\/rest\/currency\/rates$/, (r) =>
    r.fulfill(
      json({
        // Production's population, not two rows — see CURRENCY_RATES.
        rates: CURRENCY_RATES,
        cached: false,
      }),
    ),
  );
}

/** A FRESH browser state at the app's default locale — never inherited. */
async function freshState(page: Page): Promise<void> {
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("jf_workspace", "demo");
      localStorage.setItem("jf_lang", "en");
    } catch {
      /* storage unavailable */
    }
  });
}

/* ─── the panel walk, and the gate's scan over each panel ───────────────────── */

/** What one panel rendered, read from the live DOM after its tab was clicked. */
type PanelRead = {
  id: string;
  heading: string;
  text: number;
  controls: number;
  painted: boolean;
};

/**
 * Opens /settings on the ADMIN bundle and waits until the strip renders every
 * declared tab — i.e. until the session bundle has resolved `role` (the
 * pre-bundle strip is a different, smaller set).
 */
async function openSettings(page: Page, baseURL: string | undefined): Promise<void> {
  await freshState(page);
  await stubAPI(page);
  await page.goto(`${baseURL}/settings`, { waitUntil: "domcontentloaded" });
  await expect(
    page.locator('[role="tab"]'),
    `the /settings strip must render all ${TAB_IDS.length} tabs (the admin bundle resolves the role)`,
  ).toHaveCount(TAB_IDS.length, { timeout: 30_000 });
}

/**
 * Clicks one tab and waits (bounded) for ITS panel to be painted: the element
 * `#set-panel-<id>` visible, the first `<h2>` inside it the tab's own heading,
 * and exactly one `role="tabpanel"` in the document. Returns the read either
 * way, so a panel that never paints fails on the caller's OWN assertion with the
 * live DOM in the message instead of hanging here.
 */
async function selectTab(page: Page, id: string): Promise<PanelRead> {
  const want = DICT[TAB_PANEL_HEADING[id]].en;
  await page.locator(`[role="tab"]#set-tab-${id}`).click();
  const read = `(() => {
    const panel = document.getElementById("set-panel-${id}");
    const painted = !!panel && panel.getBoundingClientRect().height > 0;
    const h2 = panel ? panel.querySelector("h2") : null;
    return {
      id: "${id}",
      heading: h2 ? (h2.textContent || "").trim() : "",
      text: panel ? (panel.innerText || "").replace(/\\s+/g, " ").trim().length : 0,
      controls: panel ? panel.querySelectorAll("input, select, textarea, button, a[href]").length : 0,
      painted,
      panelCount: document.querySelectorAll('[role="tabpanel"]').length,
    };
  })()`;
  let last = (await page.evaluate(read)) as PanelRead & { panelCount: number };
  const deadline = Date.now() + 15_000;
  while (Date.now() < deadline) {
    if (last.painted && last.heading === want && last.panelCount === 1) break;
    await page.waitForTimeout(100);
    last = (await page.evaluate(read)) as PanelRead & { panelCount: number };
  }
  expect(
    last.heading,
    `clicking the "${id}" tab did not paint its own panel: #set-panel-${id} is ${last.painted ? "painted" : "not painted"}, ` +
      `its first <h2> reads ${JSON.stringify(last.heading)} where the dictionary says ${JSON.stringify(want)}, and the ` +
      `document holds ${last.panelCount} role="tabpanel" element(s) — the scan below would measure the wrong panel`,
  ).toBe(want);
  expect(
    last.panelCount,
    `the "${id}" tab paints ${last.panelCount} role="tabpanel" elements — the strip paints exactly one panel`,
  ).toBe(1);
  return { id: last.id, heading: last.heading, text: last.text, controls: last.controls, painted: last.painted };
}

/**
 * The gate's scan over the WHOLE document, with the anti-vacuum arm — the same
 * shape `e2e/a11y-authed-pages.spec.ts` uses (`scanDocument`), so a page scan and
 * a signed-in page scan cannot disagree about the rule set or the failing bar.
 *
 * Returns the blocking violations (as `rule (impact) × nodes` strings) instead of
 * asserting them, so ONE red run names EVERY panel that fails rather than
 * stopping at the first — the walk below aggregates and asserts once.
 */
async function scanPanel(page: Page, what: string): Promise<string[]> {
  const results = await gateScan(page);
  const blockers = blockersOf(results);
  if (blockers.length > 0) {
    console.log(
      `\n[${what}] ${blockers.length} critical/serious violation(s):\n` +
        blockers
          .map(
            (v) =>
              `  - ${v.id} (${v.impact}): ${v.help}\n` +
              v.nodes
                .slice(0, 5)
                .map((n) => `      ${n.target.join(" ")} — ${(n.failureSummary ?? "").split("\n").slice(-1)[0]}`)
                .join("\n"),
          )
          .join("\n"),
    );
  }
  // NON-VACUITY: axe must have EVALUATED rules here. `violations: []` from a scan
  // that analysed nothing is not a clean panel — it is a scan of nothing. This
  // one DOES throw: an unread scan is a broken harness, not a page defect.
  expect(
    results.passes.length + results.incomplete.length + results.violations.length,
    `${what}: axe evaluated no rule-nodes at all — a 0-violation reading from a scan that analysed nothing is ` +
      `vacuous, which is exactly how a green gate over an unvisited panel looks`,
  ).toBeGreaterThan(0);
  return blockers.map((v) => `${what}: ${v.id} (${v.impact}) × ${v.nodes.length}`);
}

/**
 * The CURRENCY panel rates box, read out of the live DOM (card t_8b4297e8).
 *
 * WHY IT IS READ AT ALL. `scrollable-region-focusable` only fires on an element
 * that really overflows, so the arm is only evidence when the stub renders the
 * production rate count and the box is actually a scroll container at the width
 * being walked. Measured on the pre-fix tree at 390x844 with these 27 rates: the
 * box holds 27 tiles in TWO columns, 406px of rows inside a 256px `max-h-64`,
 * i.e. ~150px that a keyboard user could not reach — the exact serious
 * violation this card fixes. `tiles` is pinned to the stub at BOTH widths (a
 * shrunk stub re-vacates the scan), and the scroll / tab-stop / name readings
 * are pinned at 390px, where the overflow is guaranteed.
 */
async function readCurrencyGrid(
  page: Page,
): Promise<{ tiles: number; scrolls: boolean; tabStop: boolean; name: string; clientHeight: number; scrollHeight: number }> {
  return (await page.evaluate(`(() => {
    const panel = document.getElementById("set-panel-currency");
    const el = panel ? panel.querySelector("[data-testid=currency-rates-grid]") : null;
    if (!el) return { tiles: -1, scrolls: false, tabStop: false, name: "", clientHeight: 0, scrollHeight: 0 };
    return {
      tiles: el.children.length,
      scrolls: el.scrollHeight > el.clientHeight + 1 && getComputedStyle(el).overflowY !== "visible",
      tabStop: el.tabIndex >= 0,
      name: el.getAttribute("aria-label") || "",
      clientHeight: el.clientHeight,
      scrollHeight: el.scrollHeight,
    };
  })()`)) as {
    tiles: number;
    scrolls: boolean;
    tabStop: boolean;
    name: string;
    clientHeight: number;
    scrollHeight: number;
  };
}

/** Walks every declared tab at one width and scans each panel. */
async function walkEveryPanel(page: Page, baseURL: string | undefined, width: number): Promise<void> {
  await page.setViewportSize({ width, height: width === 390 ? 844 : 900 });
  await openSettings(page, baseURL);

  const seen: string[] = [];
  const failures: string[] = [];
  for (const id of TAB_IDS) {
    const panel = await selectTab(page, id);
    seen.push(panel.heading);
    const floor = PANEL_FLOOR[id];
    // The panel must have rendered its CONTENT: the row, the table, the form.
    // An empty panel proves nothing about the elements the eight defects were on.
    expect(
      panel.text,
      `the "${id}" panel rendered ${panel.text} characters (floor ${floor.text}) at ${width}px — that is its ` +
        `empty/loading state, and a scan of it proves nothing about the rows inside it`,
    ).toBeGreaterThanOrEqual(floor.text);
    expect(
      panel.controls,
      `the "${id}" panel rendered ${panel.controls} controls (floor ${floor.controls}) at ${width}px — the ` +
        `populated panel's controls are what the scan is for`,
    ).toBeGreaterThanOrEqual(floor.controls);
    console.log(`[/settings ${width}px] ${id}: text=${panel.text} controls=${panel.controls} heading=${JSON.stringify(panel.heading)}`);
    // THE CURRENCY SCROLL WITNESS (card t_8b4297e8): the stub must render the
    // rate count production carries, and at the phone width the box must really
    // be the scroll container `scrollable-region-focusable` is about —
    // reachable by the keyboard and announced by name. A green axe reading over
    // a box that never overflows is the vacuity this card was filed for.
    if (id === "currency") {
      const grid = await readCurrencyGrid(page);
      console.log(
        `[/settings ${width}px] currency rates box: tiles=${grid.tiles} scrolls=${grid.scrolls} ` +
          `client=${grid.clientHeight} scroll=${grid.scrollHeight} tabStop=${grid.tabStop} ` +
          `name=${JSON.stringify(grid.name)}`,
      );
      expect(
        grid.tiles,
        `the currency panel rendered ${grid.tiles} rate tile(s) where the stub carries ${CURRENCY_RATES.length} — ` +
          `the COUNT is what makes the rates box a scroll container at 390px (production carries 27), and a ` +
          `short table leaves \`scrollable-region-focusable\` nothing to evaluate on it`,
      ).toBe(CURRENCY_RATES.length);
      if (width === 390) {
        expect(
          grid.scrolls,
          `the currency rates box does not overflow at 390px (${grid.tiles} tiles) — with the real rate count ` +
            `it does, and that overflow is the serious axe violation this arm exists to see`,
        ).toBe(true);
        expect(
          grid.tabStop,
          `the scrollable currency rates box has no tab stop (tabIndex < 0) — a keyboard user cannot scroll it, ` +
            `which is exactly the \`scrollable-region-focusable\` violation (WCAG 2.1.1)`,
        ).toBe(true);
        expect(
          grid.name,
          `the scrollable currency rates box carries no accessible name (aria-label is empty) — a focusable box ` +
            `is announced as a bare group; it must say what it holds`,
        ).toBe(DICT["set.currencyRates"].en);

        // …and it really takes focus: `tabIndex >= 0` is the ATTRIBUTE, this is the
        // tab stop a keyboard user lands on (the same reading
        // `e2e/a11y-scroll-regions.spec.ts` takes of the /mcp and /design boxes).
        const focused = (await page.evaluate(`(() => {
          const panel = document.getElementById("set-panel-currency");
          const el = panel ? panel.querySelector("[data-testid=currency-rates-grid]") : null;
          if (!el) return "no rates box in the currency panel";
          el.focus();
          return document.activeElement === el
            ? ""
            : "focus landed on " + (document.activeElement ? document.activeElement.tagName : "nothing");
        })()`)) as string;
        expect(focused, `focusing the currency rates box did not make it the active element: ${focused}`).toBe("");

        // THE CONTROL, the same probe `e2e/a11y-scroll-regions.spec.ts` runs: strip
        // the tab stop off the REAL box and require the gate scan to name
        // `scrollable-region-focusable` on exactly that node, then put the tab stop
        // back. Without this half a green reading could be a scan of a rule that
        // never ran on this container — which is precisely the vacuity this card
        // was filed for.
        const stripped = (await page.evaluate(`(() => {
          const panel = document.getElementById("set-panel-currency");
          const el = panel ? panel.querySelector("[data-testid=currency-rates-grid]") : null;
          if (!el) return "no rates box in the currency panel";
          el.removeAttribute("tabindex");
          return el.tabIndex === -1 ? "" : "tabIndex still " + el.tabIndex;
        })()`)) as string;
        expect(stripped, `the control could not strip the tab stop off the currency rates box: ${stripped}`).toBe("");
        const scoped = blockersOf(await gateScan(page, "#set-panel-currency")).map(
          (v) => `${v.id} (${v.impact}) × ${v.nodes.length}`,
        );
        expect(
          scoped,
          `with the tab stop removed the gate scan must name scrollable-region-focusable on the currency rates box — ` +
            `it read ${JSON.stringify(scoped)} instead, so the clean reading above is not evidence for that rule`,
        ).toContain("scrollable-region-focusable (serious) × 1");
        await page.evaluate(`(() => {
          const panel = document.getElementById("set-panel-currency");
          const el = panel ? panel.querySelector("[data-testid=currency-rates-grid]") : null;
          if (el) el.setAttribute("tabindex", "0");
        })()`);
      }
    }
    failures.push(...(await scanPanel(page, `/settings → ${id} @${width}px`)));
  }
  expect(seen, `the walk must have read all ${TAB_IDS.length} declared panels`).toHaveLength(TAB_IDS.length);
  expect(
    failures,
    `${failures.length} critical/serious axe violation(s) across the /settings tab panels at ${width}px — every ` +
      `panel was walked and scanned with its content populated, so this is the panel list, not the first failure`,
  ).toEqual([]);
}

test.describe("a11y: /settings, every tab panel (card t_ef7d5c04)", () => {
  test("every tab panel renders its own populated content with no critical/serious axe violation (en @1440x900)", async ({
    page,
    baseURL,
  }) => {
    test.setTimeout(300_000);
    await walkEveryPanel(page, baseURL, 1440);
  });

  // The phone width is where the strip scrolls and where the panel lays out
  // differently (the fee table and the domain rows reflow), so the same walk runs
  // again — the eight measured defects reproduced at BOTH widths.
  test("every tab panel renders its own populated content with no critical/serious axe violation (en @390x844)", async ({
    page,
    baseURL,
  }) => {
    test.setTimeout(300_000);
    await walkEveryPanel(page, baseURL, 390);
  });

  // THE WEBHOOKS PANEL, EXPANDED. The delivery history is behind a click and the
  // FAILED row is the one that paints `bg-[var(--jf-danger-bg)]` — the tint the
  // cells' ink has to stay readable on, and the state the parent card's contrast
  // fix was measured in. Its heading was the second measured defect (<h4> under
  // an <h2>), and `heading-order` is MODERATE, so the critical/serious bar above
  // cannot see it: this arm asserts the rule explicitly.
  test("webhooks: the expanded delivery history is clean, and its heading order is not a skip (en @1440x900)", async ({
    page,
    baseURL,
  }) => {
    test.setTimeout(180_000);
    await page.setViewportSize({ width: 1440, height: 900 });
    await openSettings(page, baseURL);
    await selectTab(page, "webhooks");

    await page.getByRole("button", { name: DICT["wh.showHistory"].en }).first().click();
    await expect(
      page.getByRole("heading", { name: DICT["wh.history"].en }),
      "clicking Show history must render the delivery history",
    ).toBeVisible({ timeout: 15_000 });

    // THE SCAN FIRST, then the tint: measured on the base tree the failed row is
    // painted `bg-red-50/60` (an OKLAB colour at 60% alpha) while its heading is
    // an `<h4>`, so asserting the tint first would report the colour and stop
    // before the heading-order reading this arm exists for.
    const results = await gateScan(page);
    expect(
      results.violations.find((v) => v.id === "heading-order"),
      `the expanded delivery history skips a heading level (the measured defect was an <h4> under the panel's <h2>); ` +
        `headings found: ${JSON.stringify(
          await page.evaluate(`(() => [...document.querySelectorAll("#set-panel-webhooks h1,#set-panel-webhooks h2,#set-panel-webhooks h3,#set-panel-webhooks h4,#set-panel-webhooks h5,#set-panel-webhooks h6")].map((h) => h.tagName + " " + (h.textContent || "").trim().slice(0, 40)))()`),
        )}`,
    ).toBeUndefined();
    expect(
      blockersOf(results).map((v) => `${v.id} (${v.impact}) × ${v.nodes.length}`),
      "the expanded delivery history has critical/serious axe violation(s)",
    ).toEqual([]);

    // The FAILED row is on the page and tinted — otherwise this arm would be
    // scanning a history that carries none of the cells it exists for.
    const failedRow = page.locator("tr", { hasText: DICT["wh.failed"].en }).first();
    await expect(failedRow, "the failed delivery row must be rendered").toBeVisible();
    const tint = await failedRow.evaluate((el) => {
      const probe = document.createElement("div");
      probe.style.background = "var(--jf-danger-bg)";
      document.body.appendChild(probe);
      const want = getComputedStyle(probe).backgroundColor;
      probe.remove();
      return { want, got: getComputedStyle(el).backgroundColor };
    });
    expect(
      tint.got,
      `the failed delivery row is not painted with --jf-danger-bg (it reads ${tint.got}, the token is ${tint.want}) — ` +
        `the cells' readable ink on the tint is the state this arm is for`,
    ).toBe(tint.want);
  });

  // THE CONTROL: the arm above is only evidence if the SAME scan can report the
  // class it was written for. The PRE-FIX DOM is restored on the live page — a
  // text input with no `<label>`, no `aria-label` and no `title` in the domains
  // panel, which is the shape all nine `label` violations shipped in — and axe
  // must name it; then it is removed and the panel must read clean again.
  test("CONTROL: an unlabelled control planted in a panel is reported, and the panel reads clean again without it", async ({
    page,
    baseURL,
  }) => {
    test.setTimeout(180_000);
    await page.setViewportSize({ width: 1440, height: 900 });
    await openSettings(page, baseURL);
    await selectTab(page, "domains");

    const planted = await page.evaluate(`(() => {
      const panel = document.getElementById("set-panel-domains");
      if (!panel) return "no #set-panel-domains";
      const el = document.createElement("input");
      el.id = "jf-planted-unlabelled";
      el.type = "text";
      el.value = "unlabelled";
      panel.appendChild(el);
      return "";
    })()`) as string;
    expect(planted, `the control could not be planted in the domains panel: ${planted}`).toBe("");

    const after = await gateScan(page);
    const label = after.violations.find((v) => v.id === "label");
    expect(
      label,
      "an input with no label, no aria-label and no title was NOT reported — this guard is not measuring the class " +
        "the nine `label` violations shipped as",
    ).toBeTruthy();
    expect(label?.impact, "axe reports `label` as critical").toBe("critical");
    expect(
      label?.nodes.map((n) => n.target.join(" ")),
      "axe must name the control this arm planted",
    ).toContain("#jf-planted-unlabelled");

    await page.evaluate(`(() => document.querySelectorAll("#jf-planted-unlabelled").forEach((n) => n.remove()))()`);
    expect(
      await scanPanel(page, "/settings → domains (planted control removed)"),
      "with the planted control removed the domains panel must read clean again — otherwise the reading above is not " +
        "about the plant",
    ).toEqual([]);

    // …and the ARIA half, through the family's shared control: a prohibited ARIA
    // state planted INSIDE the panel must be named by the same scan, so a rule
    // set narrowed in the future cannot leave the walk's green readings vacuous.
    await expectGateSeesAriaViolation(page, "#set-panel-domains", "the /settings domains panel");
  });
});
