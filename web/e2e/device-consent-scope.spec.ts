// THE CONSENT CARD LETS THE PERSON PICK THE ACCESS LEVEL (card t_cf45b84c).
//
// THE DEFECT. The RFC 8628 consent card printed the scope the harness had asked
// for as TEXT and offered only Approve / Deny. The grant was therefore whatever
// the client had requested: an agent that asked for `read` stayed read-only and
// had to be re-granted the first time it needed to write, and the only way to
// change it afterwards was Connected apps → scope, which ROTATES the credential
// and forces the harness to re-authenticate. Wan: "default scope tu jgn read
// only. bagi semua supaya user tak perlu grant banyak kali."
//
// WHAT THIS SPEC PINS, read out of the LIVE DOM of the real page:
//   1. two radios, in the order the card shows them, with the plain-word labels
//      from the dictionary (no "scope", no "grant" on screen);
//   2. the radio the CLIENT ASKED FOR is the one checked — readwrite asked →
//      readwrite selected, read asked → read selected. Never silently read-only;
//   3. either direction can be picked, and the approve call carries the picked
//      value on the wire (the request body is intercepted and read, not inferred
//      from the UI);
//   4. a response WITHOUT `choices` (an API older than this card, i.e. a rolling
//      deploy) still renders both levels rather than collapsing to read.
//
// WHY A BROWSER TEST. The selection is React state seeded from the fetched
// consent: a grep can see `defaultDeviceScope` in the source and still be looking
// at a page that paints `read` checked, because the wiring is a call site. The
// session and the consent response are stubbed at the network boundary (the
// pattern e2e/a11y-profile-tabs.spec.ts uses for this exact page), so the real
// client component runs with no Go server, no Postgres and no credential.
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

/** A consent response for a harness that asked for `requested`. `choices` is
 *  omitted for the rolling-deploy arm, which is exactly what an API older than
 *  this card sends. */
function consent(requested: string, choices?: string[]) {
  return {
    user_code: USER_CODE,
    client_id: "client-1",
    client: "Claude Desktop",
    scopes: [requested],
    ...(choices ? { choices } : {}),
    scope_prose: requested === "read" ? "read-only" : "read and write",
    expires_at: "2099-01-01T00:00:00Z",
    state: "pending",
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
 *  produced a false reading on this board (a page rendered Arabic because a
 *  previous card left `jf_lang=ar` behind). en is the default a new visitor gets. */
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

/** Open the email's own entry point and return the approve request's body once
 *  the card has rendered. */
async function openCard(page: Page, body: unknown, capture?: { seen?: string }) {
  await freshState(page);
  await stubAPI(page, body);
  if (capture) {
    await page.route(/(.*)\/api\/auth\/connected-apps\/device$/, async (r) => {
      capture.seen = r.request().postData() || "";
      await r.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ status: "approved" }),
      });
    });
  }
  await page.goto(`/profile?tab=mcp&user_code=${USER_CODE}`, { waitUntil: "domcontentloaded" });
  // The card renders only after the client component hydrates and fetches the
  // consent; without this wait the radios may not exist yet and a count of 0
  // would read as "the card is broken".
  await expect(page.locator('input[type="radio"][name="prof-device-scope"]')).toHaveCount(2, {
    timeout: 20_000,
  });
}

const radios = (page: Page) => page.locator('input[type="radio"][name="prof-device-scope"]');

test("a harness that asked for readwrite opens on readwrite, with both levels offered", async ({
  page,
}) => {
  await openCard(page, consent("readwrite", ["read", "readwrite"]));

  await expect(radios(page)).toHaveCount(2);
  // Both levels are really on screen, in the card's order, with the
  // dictionary's plain words — not "read"/"readwrite" and not "scope".
  await expect(radios(page).nth(0)).toHaveValue("read");
  await expect(radios(page).nth(1)).toHaveValue("readwrite");
  const text = await page.locator("fieldset", { has: radios(page).first() }).innerText();
  for (const key of ["prof.deviceScopeRead", "prof.deviceScopeReadWrite"] as const) {
    expect(text, `${key} must be on the card in English`).toContain(DICT[key].en);
    expect(text, `${key} must not render the raw key`).not.toContain(key);
  }
  expect(text, "the visible copy must not use the word 'scope'").not.toMatch(/\bscope\b/i);
  expect(text, "the visible copy must not use the word 'grant'").not.toMatch(/\bgrant\b/i);

  // THE POINT: the requested level is preselected, so one click approves it.
  await expect(radios(page).nth(1), "a readwrite request must open on readwrite").toBeChecked();
  await expect(radios(page).nth(0)).not.toBeChecked();
});

test("a harness that asked for read opens on read", async ({ page }) => {
  await openCard(page, consent("read", ["read", "readwrite"]));
  await expect(radios(page).nth(0), "a read request must open on read").toBeChecked();
  await expect(radios(page).nth(1)).not.toBeChecked();
});

test("read asked, readwrite approved reaches the wire in one click", async ({ page }) => {
  // The widening Wan asked for: the agent never has to be re-granted later.
  const capture: { seen?: string } = {};
  await openCard(page, consent("read", ["read", "readwrite"]), capture);

  await radios(page).nth(1).check();
  await expect(radios(page).nth(1)).toBeChecked();
  await expect(radios(page).nth(0)).not.toBeChecked();

  await page.getByRole("button", { name: DICT["prof.deviceApprove"].en, exact: true }).click();
  await expect
    .poll(() => capture.seen, { timeout: 10_000 })
    .toBeTruthy();
  const sent = JSON.parse(capture.seen!);
  expect(sent, "the approve call must carry the picked level").toEqual({
    user_code: USER_CODE,
    decision: "approve",
    scope: "readwrite",
  });
});

test("readwrite asked, read approved reaches the wire as read", async ({ page }) => {
  // The cautious direction, in the same single click.
  const capture: { seen?: string } = {};
  await openCard(page, consent("readwrite", ["read", "readwrite"]), capture);

  await radios(page).nth(0).check();
  await page.getByRole("button", { name: DICT["prof.deviceApprove"].en, exact: true }).click();
  await expect.poll(() => capture.seen, { timeout: 10_000 }).toBeTruthy();
  expect(JSON.parse(capture.seen!)).toEqual({
    user_code: USER_CODE,
    decision: "approve",
    scope: "read",
  });
});

test("an API that sends no `choices` still offers both levels", async ({ page }) => {
  // Rolling deploy: the new dashboard can be served by an API older than this
  // card. Falling back to read alone would silently reinstate read-only.
  await openCard(page, consent("readwrite"));
  await expect(radios(page)).toHaveCount(2);
  await expect(radios(page).nth(1)).toBeChecked();
});
