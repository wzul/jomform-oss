// Card t_1c4a3f54: a paid buyer's receipt was lost when the queue enqueue failed
// with `context canceled` on the CHIP callback path, and the only trace was a
// WARN line — nothing the merchant could see.
//
// The failure is now recorded as an `enqueue_failed` row in
// email_delivery_events, which is what the /deliveries page lists. That makes
// this page the place the loss becomes VISIBLE, so the page has to render and
// filter the new event type: a row that is not in the filter's option list
// cannot be looked up, and a badge that falls through to the neutral fallback
// reads like an ordinary delivery state instead of a failure.
//
// The API is stubbed at the browser level (no Go server, no session, no DB), the
// pattern `e2e/affiliate-row.spec.ts` uses: every /api/* call is fulfilled by
// page.route and the assertions read the live DOM of the real built page.
import { test, expect, type Page } from "@playwright/test";

import { signIn } from "./auth";

test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

const BUNDLE = {
  me: { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "", is_super_admin: false },
  workspaces: [{ id: "ws1", name: "Demo", slug: "demo", role: "admin" }],
  chip_status: null, keys: [], domains: [], social: [], einvoice: null,
  products: [], summary: { counts: {}, revenue_sen: 0 },
  sales: { items: [], page: 1, limit: 20, total: 0 },
  form_analytics: [], product_analytics: [],
};

// One recorded loss: the receipt for a paid sale never reached the queue.
const LOST_RECEIPT = {
  id: 1,
  email: "pembeli@example.test",
  event_type: "enqueue_failed",
  sale_ref: "JF-374daeb0ce61",
  form_id: "",
  detail: { job_type: "receipt", attempts: 4, error: "context canceled" },
  created_at: "2026-11-06T21:16:15Z",
};

const DELIVERY_REQUESTS: string[] = [];

async function stubAPI(page: Page) {
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
  await page.route(/(.*)\/api\/rest\/forms(\?.*)?$/, (r) => r.fulfill(json([])));
  await page.route(/(.*)\/api\/rest\/email-deliveries(\?.*)?$/, (r) => {
    DELIVERY_REQUESTS.push(r.request().url());
    r.fulfill(json({ items: [LOST_RECEIPT], page: 1, limit: 20, total: 1 }));
  });
}

test("a lost receipt is listed and filterable as enqueue_failed", async ({ page }) => {
  DELIVERY_REQUESTS.length = 0;
  await stubAPI(page);
  await page.goto("/deliveries");

  // The row itself is rendered — a stub that never reached the page would make
  // every assertion below vacuous.
  const row = page.locator("tbody tr", { hasText: LOST_RECEIPT.sale_ref });
  await expect(row).toHaveCount(1);

  const badge = row.locator("span", { hasText: "enqueue_failed" });
  await expect(badge).toHaveCount(1);
  // The failure must not read as a neutral delivery state: the badge carries the
  // red failure tint, not the slate fallback an unknown event type gets.
  const badgeClass = await badge.getAttribute("class");
  expect(badgeClass).toContain("bg-red-50");
  expect(badgeClass).not.toContain("bg-slate-100");

  // The status filter offers the type, and choosing it is wired to the query the
  // API filters on — a decorative option would pass a "the option exists" check
  // and still never narrow the list.
  const statusSelect = page.locator('select[aria-label="Status filter"]');
  const option = statusSelect.locator('option[value="enqueue_failed"]');
  await expect(option).toHaveCount(1);

  const filtered = page.waitForRequest(
    (req) => req.url().includes("/api/rest/email-deliveries") && req.url().includes("event_type=enqueue_failed"),
  );
  await statusSelect.selectOption("enqueue_failed");
  await filtered;
});

test("the page requests every event type without a filter by default", async ({ page }) => {
  DELIVERY_REQUESTS.length = 0;
  await stubAPI(page);
  await page.goto("/deliveries");
  await expect(page.locator("tbody tr")).toHaveCount(1);
  // The first load must be unfiltered: if the page pre-applied a status, a
  // merchant would never see the failure rows this card added.
  expect(DELIVERY_REQUESTS[0] ?? "").not.toContain("event_type=");
});
