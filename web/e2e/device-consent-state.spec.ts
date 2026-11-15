// THE CONSENT CARD RENDERS FROM THE SERVER'S STATE, NOT FROM ITS OWN OPINION
// (card t_c6ca84a7).
//
// THE DEFECT. `oauth_device_codes` has no status column: pending / approved /
// denied / consumed / expired come from four nullable timestamps. Every reader
// derived them again, and the consent card asked "approved?" BEFORE "expired?"
// — so a code the person approved and the harness never collected rendered as a
// live card with Approve and Deny on it, while the token endpoint answered
// `expired_token` for the very same row. The page also compared the state string
// with `"pending"` in the browser, which made it a further reader of a fact the
// API owns.
//
// WHAT THIS SPEC PINS, read out of the LIVE DOM of the real page:
//   1. the server's `available_events` decides whether Approve/Deny exist at all,
//      so a terminal state can never paint a decision;
//   2. the "approved but never collected, now expired" arm shows the EXPIRED
//      status line and no buttons — the arm no page test covered before;
//   3. the server's state wins over the browser's reading of the state string;
//   4. a response WITHOUT `available_events` (an API older than this card)
//      keeps the old rule exactly, so a rolling deploy renders the same card.
//
// WHY A BROWSER TEST. This is a call site: a grep can see `available_events` in
// the source and still be looking at a page that paints Approve, because the JSX
// gate is what decides. The session and every API response are stubbed at the
// network boundary (the pattern e2e/device-consent-scope.spec.ts uses for this
// page), so the real client component runs with no Go server, no Postgres and no
// credential.
import { test, expect, type Page } from "@playwright/test";

import { signIn } from "./auth";
import { DICT } from "../src/components/i18nDict";

test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

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

const USER_CODE = "KX9M-X5BS";

/** One consent payload in the shape the API sends after card t_c6ca84a7.
 *  `state` is the derived state ("used" is the wire spelling of consumed);
 *  `available_events` is what the transition table still allows. Omitting
 *  `available_events` reproduces an API older than this card. */
function consent(payload: {
  state: string;
  events?: string[];
  remaining?: number;
}) {
  return {
    user_code: USER_CODE,
    client_id: "client-1",
    client: "Claude Desktop",
    scopes: ["read"],
    choices: ["read", "readwrite"],
    scope_prose: "read-only",
    expires_at: "2099-01-01T00:00:00Z",
    state: payload.state,
    ...(payload.events ? { available_events: payload.events } : {}),
    ...(payload.remaining === undefined ? {} : { remaining_seconds: payload.remaining }),
  };
}

/** Stub every API the page calls; the consent lookup is registered LAST so it
 *  wins over the catch-all (Playwright resolves routes in reverse order). */
async function stubAPI(page: Page, body: unknown) {
  const json = (b: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(b),
  });
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(ME)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "en" })));
  await page.route(/(.*)\/api\/auth\/(passkeys|social|mcp-agents|trust-devices)(\?.*)?$/, (r) =>
    r.fulfill(json([])),
  );
  await page.route(/(.*)\/api\/auth\/trust-status(\?.*)?$/, (r) => r.fulfill(json({ trusted: false })));
  await page.route(/(.*)\/api\/auth\/connected-apps\/device\?user_code=.*$/, (r) =>
    r.fulfill(json(body)),
  );
}

/** A FRESH browser state: cleared storage and `lang=en`.
 *
 *  The host keeps localStorage between runs, and a `jf_lang` left by an earlier
 *  card renders a locale this test did not ask for — the trap that already
 *  produced a false reading on this board. en is the default a new visitor gets. */
async function freshState(page: Page) {
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("jf_lang", "en");
      localStorage.setItem("jf_workspace", "demo");
    } catch {
      /* storage unavailable */
    }
  });
}

/** Open the email's own entry point. Each test waits for the FIRST positive
 *  sign of its own answer before asserting anything is absent — an absence
 *  asserted too early passes vacuously, which is how a broken card stays green. */
async function openCard(page: Page, body: unknown) {
  await freshState(page);
  await stubAPI(page, body);
  await page.goto(`/profile?tab=mcp&user_code=${USER_CODE}`, { waitUntil: "domcontentloaded" });
}

const approveButton = (page: Page) =>
  page.getByRole("button", { name: DICT["prof.deviceApprove"].en, exact: true });
const denyButton = (page: Page) =>
  page.getByRole("button", { name: DICT["prof.deviceDeny"].en, exact: true });

test("pending: the server's events are what draw Approve and Deny", async ({ page }) => {
  await openCard(page, consent({ state: "pending", events: ["poll", "approve", "deny", "expire"], remaining: 540 }));

  // Positive first: the card really rendered (it names the code the email
  // carried), so the absence assertions below cannot pass on an empty page.
  await expect(page.getByText(USER_CODE, { exact: false })).toBeVisible({ timeout: 20_000 });
  await expect(approveButton(page)).toHaveCount(1);
  await expect(denyButton(page)).toHaveCount(1);
  // No red "not valid" banner above a card that is clearly valid.
  await expect(page.getByText(DICT["prof.deviceNotFound"].en, { exact: false })).toHaveCount(0);
});

test("approved, never collected, now expired: no buttons, and the card says expired", async ({
  page,
}) => {
  // THE ARM THE CARD EXISTS FOR. The old page asked "approved?" first, so this
  // row painted a live card with Approve on it while the token endpoint
  // answered expired_token. The API now derives one state for both.
  await openCard(page, consent({ state: "expired", events: [], remaining: 0 }));

  await expect(page.getByText(DICT["prof.deviceExpired"].en, { exact: false })).toBeVisible({
    timeout: 20_000,
  });
  await expect(approveButton(page)).toHaveCount(0);
  await expect(denyButton(page)).toHaveCount(0);
});

test("a terminal state offers nothing even when the state string is not the reason", async ({
  page,
}) => {
  // The payload decides. A "pending" string with a table that allows no
  // approve/deny renders read-only — the browser is not a sixth reader.
  await openCard(page, consent({ state: "pending", events: ["expire"], remaining: 12 }));

  await expect(page.getByText(DICT["prof.deviceNotFound"].en, { exact: false })).toBeVisible({
    timeout: 20_000,
  });
  await expect(approveButton(page)).toHaveCount(0);
  await expect(denyButton(page)).toHaveCount(0);
});

test("an older API without available_events keeps the old rule exactly", async ({ page }) => {
  // Rolling deploy: the dashboard may run one release ahead of the API. With no
  // `available_events` on the wire the card falls back to `state === "pending"`,
  // which is exactly what it did before this card.
  await openCard(page, consent({ state: "pending" }));
  await expect(approveButton(page)).toHaveCount(1, { timeout: 20_000 });
  await expect(denyButton(page)).toHaveCount(1);
});

test("an older API's approved card stays read-only", async ({ page }) => {
  await openCard(page, consent({ state: "approved" }));
  await expect(page.getByText(DICT["prof.deviceApproved"].en, { exact: false })).toBeVisible({
    timeout: 20_000,
  });
  await expect(approveButton(page)).toHaveCount(0);
});
