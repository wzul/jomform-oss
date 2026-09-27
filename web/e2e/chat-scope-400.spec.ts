// The chat confirm path's 400: ONE message, and it is not "try again".
//
// THE DEFECT (card t_790b1016, filed by the QA lane while verifying PR 747).
// A 400 on the confirm/cancel path is a WORKSPACE-SCOPE REFUSAL, not a failure of
// the change. The PR gave it its own alert copy (`chat.scopeRequired`) but left
// the proposal card on `setState(..., "failed")`, and the card's footer renders
// `chat.confirmFail` for that state. One 400 therefore put TWO sentences on
// screen at once, and the second one was wrong advice:
//
//     role=alert  : "Choose a workspace first — the assistant answers for one
//                    workspace at a time."
//     card footer : "The change could not be applied. Please try again."
//
// Retrying without a workspace fails identically, so the footer instructed the
// user to repeat an action that cannot work.
//
// WHY THIS IS A BROWSER TEST AND NOT ONLY A SOURCE GUARD. `chatPage.test.ts`
// pins the branch, and that is the guard that bites on a revert — but the page
// has no React/DOM runner in `npm run test:unit`, so nothing there can observe
// that the two strings actually appeared TOGETHER on a rendered page. This file
// measures the rendered DOM of the real production build, which is how the
// defect was found in the first place.
//
// HOW THE STATE IS REACHED WITHOUT A SESSION OR A BACKEND. The reachable shape
// is "propose while a workspace is selected, then lose the scope WITHOUT a
// reload" (a second tab's sign-out, a storage clear). Both halves are reproduced
// exactly: an init script selects `ws-b`, then AFTER the proposal card is on
// screen the test clears `jf_workspace` and presses Confirm — no navigation, so
// the Shell never re-selects a workspace and the request goes out unscoped, which
// is what the server answers 400 to. The API is stubbed at the browser level
// (the pattern `e2e/a11y-dialogs.spec.ts` uses), so no credential is fabricated
// and no Go server or database is involved.
//
// WHAT MAKES IT BITE. The assertions read the shipped English copy out of the
// app's own dictionary rather than typing it in, and they count occurrences:
// "renders the scope copy ONCE" is a count, not a substring. A revert to
// `setState(..., "failed")` puts `chat.confirmFail` back on the page and fails
// both the absence assertion and the count — verified by mutation, not assumed.
//
// Run with:
//
//   npm run a11y                       # the whole gate (this file included)
//   npx playwright test e2e/chat-scope-400.spec.ts    # this file alone
import { test, expect, type Page } from "@playwright/test";
import { DICT } from "../src/components/i18nDict";

const EN = {
  scopeRequired: DICT["chat.scopeRequired"].en,
  confirmFail: DICT["chat.confirmFail"].en,
  proposalStale: DICT["chat.proposalStale"].en,
  confirmRateLimited: DICT["chat.confirmRateLimited"].en,
  badgeFailed: DICT["chat.proposalBadgeFailed"].en,
  badgeScopeRequired: DICT["chat.proposalBadgeScopeRequired"].en,
  confirm: DICT["chat.confirm"].en,
  send: DICT["chat.send"].en,
  placeholder: DICT["chat.placeholder"].en,
};

const TOKEN = "tok-1";
const WS = "ws-b";

/** The proposal the assistant hands back, and the card that renders it. */
const PROPOSAL = {
  token: TOKEN,
  args_hash: "hash-1",
  tool: "update_product",
  title: "Rename the product",
  summary: "Tee 2026 → Tee 2026 (XL)",
  fields: [{ label: "Name", value: "Tee 2026 (XL)" }],
  expires_at: "2027-01-01T00:00:00Z",
  reversible: true,
};

const BUNDLE = {
  me: { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "", is_super_admin: false },
  workspaces: [{ id: "ws-id-b", name: "Second", slug: WS, role: "admin" }],
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

/**
 * Stub every call the page makes. `decisionStatus` is the status the
 * confirm/cancel route answers — the one variable these tests turn.
 */
async function stubAPI(page: Page, decisionStatus: number) {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  // Catch-all FIRST, specific routes after it: Playwright resolves matching
  // routes in reverse registration order, so the later (specific) ones win.
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(BUNDLE.me)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "" })));
  // Availability, then the message that produces the card. Same path, so the
  // method decides which stub answers.
  await page.route(/(.*)\/api\/rest\/chat(\?.*)?$/, (r) =>
    r.request().method() === "GET"
      ? r.fulfill(json({ available: true, limits: { messages_per_hour: 60, max_message_chars: 2000 } }))
      : r.fulfill(json({ reply: "Here is the change I would make.", proposals: [PROPOSAL] })),
  );
  const statusText: Record<number, string> = {
    400: "skop workspace diperlukan — pilih workspace dahulu",
    409: "cadangan ini sudah luput atau telah digunakan",
    429: "had pengesahan dicapai",
    500: "ralat dalaman",
  };
  await page.route(/(.*)\/api\/rest\/chat\/(confirm|cancel)(\?.*)?$/, (r) =>
    r.fulfill({
      status: decisionStatus,
      contentType: "application/json",
      body: JSON.stringify({ error: statusText[decisionStatus] ?? "ralat" }),
    }),
  );
}

/**
 * A FRESH browser state (cleared storage — the browser keeps localStorage
 * between runs and a `jf_lang` left by an earlier card would render these
 * strings in a locale this file never asserts), English, with a workspace
 * selected so the FIRST chat call is scoped.
 */
async function freshState(page: Page) {
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      localStorage.setItem("jf_workspace", "ws-b");
      localStorage.setItem("jf_lang", "en");
    } catch {
      /* storage unavailable */
    }
  });
}

/**
 * Get a proposal on screen, then LOSE the workspace scope without reloading,
 * and press Confirm. Returns the request URL the decision went out to, so the
 * test can assert the shape it believes it reproduced.
 */
async function confirmWithoutScope(page: Page, baseURL: string | undefined) {
  await page.goto(`${baseURL}/chat`, { waitUntil: "domcontentloaded" });

  const box = page.locator("#chat-input");
  await expect(box, "the composer must be offered when the assistant is available").toBeVisible({
    timeout: 15_000,
  });
  await box.fill("rename my product");
  await page.getByRole("button", { name: EN.send }).click();

  const card = page.locator('[class*="border-sky-200"]').filter({ hasText: PROPOSAL.title });
  await expect(card, "the proposal card must render — otherwise this test is vacuous").toHaveCount(1);

  // The scope is dropped with NO navigation: this is a second tab's sign-out or
  // a storage clear, not a reload. Nothing re-runs the Shell's workspace
  // reconciliation, so `workspaceQuery()` now returns "" and the decision is
  // sent unscoped — the request the server refuses with a 400.
  await page.evaluate(() => localStorage.removeItem("jf_workspace"));
  expect(
    await page.evaluate(() => localStorage.getItem("jf_workspace")),
    "the workspace scope must really be gone before Confirm — otherwise this test proves nothing",
  ).toBe(null);

  const decision: string[] = [];
  page.on("request", (r) => {
    if (/\/api\/rest\/chat\/(confirm|cancel)/.test(r.url())) decision.push(r.url());
  });

  await card.getByRole("button", { name: EN.confirm }).click();
  // Scoped to a `<p>`: Next renders its own route announcer as
  // `<div role="alert" id="__next-route-announcer__">`, so the bare role locator
  // is ambiguous and the page still holds a SECOND empty alert node. The chat
  // page's message paragraph is the `p[role="alert"]`.
  await expect(page.locator('p[role="alert"]')).toBeVisible({ timeout: 10_000 });
  return decision;
}

test.describe("chat confirm path: a scope refusal is not a failed change", () => {
  test("a 400 renders the scope copy once and never the 'try again' footer", async ({
    page,
    baseURL,
  }) => {
    await stubAPI(page, 400);
    await freshState(page);
    const decision = await confirmWithoutScope(page, baseURL);

    // The shape is asserted, not assumed: if the decision ever starts carrying a
    // scope again this test is no longer reproducing the defect and must fail
    // rather than pass on a branch that is no longer reachable.
    expect(decision, "the decision must have been sent exactly once").toHaveLength(1);
    expect(
      decision[0],
      `the decision must have gone out UNSCOPED — that is the request a 400 answers. Sent: ${JSON.stringify(decision)}`,
    ).not.toContain("workspace=");

    // ONE message. Counted, because the defect was two.
    const scopeNodes = page.getByText(EN.scopeRequired, { exact: true });
    await expect(
      scopeNodes,
      "the scope refusal must be rendered exactly once — the pre-fix shape duplicated the copy",
    ).toHaveCount(1);
    await expect(page.locator('p[role="alert"]')).toHaveText(EN.scopeRequired);

    // And NOT the advice that cannot work. This is the assertion the defect
    // fails: the card footer used to render exactly this sentence.
    await expect(
      page.getByText(EN.confirmFail, { exact: true }),
      `nothing on the page may tell the user to try again: retrying without a workspace fails ` +
        `identically ("${EN.confirmFail}")`,
    ).toHaveCount(0);

    // The card itself stops claiming a failure: it says what it NEEDS.
    const card = page.locator('[class*="border-sky-200"]').filter({ hasText: PROPOSAL.title });
    await expect(card).toContainText(EN.badgeScopeRequired);
    await expect(
      card,
      `the card must not wear the "did not go through" badge — nothing was attempted`,
    ).not.toContainText(EN.badgeFailed);
  });

  test("a genuine failure still renders 'try again' on the card", async ({ page, baseURL }) => {
    // The other direction, so the fix cannot be satisfied by deleting the
    // failure path: a 5xx IS a failure of the change and keeps its own copy —
    // including on the card, which is the element this card's fix touched.
    await stubAPI(page, 500);
    await freshState(page);
    await confirmWithoutScope(page, baseURL);

    const card = page.locator('[class*="border-sky-200"]').filter({ hasText: PROPOSAL.title });
    await expect(
      card,
      `a genuine failure must still render chat.confirmFail on the card — deleting the failure path ` +
        `would satisfy the 400 assertions for the wrong reason`,
    ).toContainText(EN.confirmFail);
    await expect(
      page.getByText(EN.scopeRequired, { exact: true }),
      "a 5xx is not a scope refusal — the scope copy must not appear",
    ).toHaveCount(0);
    // The card reports the failure it actually had, not "needs a workspace".
    await expect(card).toContainText(EN.badgeFailed);
    await expect(card).not.toContainText(EN.badgeScopeRequired);
  });

  test("a 409 (stale) keeps its own distinct copy, and is not reported as a failure", async ({
    page,
    baseURL,
  }) => {
    await stubAPI(page, 409);
    await freshState(page);
    await confirmWithoutScope(page, baseURL);

    // The card's 409 branch is unchanged by this fix and must stay reachable.
    // Count is NOT asserted: the 409 pre-dates this card with the alert and the
    // footer carrying the SAME sentence (duplicated but not contradictory), and
    // the card's acceptance is that the copy stays distinct — not that it moves.
    await expect(
      page.getByText(EN.proposalStale, { exact: true }).first(),
      "a 409 must keep its own 'expired or already used' copy",
    ).toBeVisible();
    await expect(
      page.getByText(EN.confirmFail, { exact: true }),
      "a spent proposal is not a failed change — the 'try again' copy must not appear",
    ).toHaveCount(0);
  });

  test("a 429 keeps the rate-limit copy", async ({ page, baseURL }) => {
    await stubAPI(page, 429);
    await freshState(page);
    await confirmWithoutScope(page, baseURL);

    await expect(
      page.getByText(EN.confirmRateLimited, { exact: true }).first(),
      "a rate-limited confirmation must keep its own limit copy, not the generic failure",
    ).toBeVisible();
    await expect(
      page.getByText(EN.scopeRequired, { exact: true }),
      "a rate limit is not a scope refusal — the scope copy must not appear",
    ).toHaveCount(0);
  });
});
