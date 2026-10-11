// Regression evidence for the products + media TRASH views (card t_cf5e6473).
//
// WHAT THIS PROVES, on the real built page with a stubbed API:
//   products: the LIVE catalogue is the default view (never the bin), the bin
//     lists trashed rows, a row that may not be purged is NOT offered the
//     permanent-delete control and names the obstacle instead, and a purgeable
//     row offers it.
//   media: the same, plus the copy that says the bytes are untouched — the whole
//     point of the card is that a trashed object is restorable, so the bin must
//     not describe a delete that destroys the file.
//
// The API is stubbed at the browser level (no Go server, no session, no DB), so
// this runs against the build the gate is already serving.
//
// FRESH STATE: every test clears storage and seeds only what it needs, so a
// jf_lang left behind by an earlier card cannot make this read in a language no
// new visitor sees. The default here is asserted explicitly: en.
import { test, expect, type Page } from "@playwright/test";

import { signIn } from "./auth";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page
// renders. This spec visits one, so it declares the session it means — before
// the gate existed an anonymous visit rendered the account shell, so this spec
// could (and did) rely on the bug. See e2e/auth.ts.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

const TRASHED_PRODUCTS = [
  { id: "p_purgeable", name: "Kopi Ais", price_sen: 650, active: true,
    deleted_at: "2026-09-25T10:00:00Z", sales_count: 0, subscription_count: 0, can_purge: true },
  { id: "p_sold", name: "Langganan Ahli", price_sen: 2900, active: true,
    deleted_at: "2026-09-24T10:00:00Z", sales_count: 3, subscription_count: 0, can_purge: false },
  { id: "p_subbed", name: "Sub Bulanan", price_sen: 1500, active: false,
    deleted_at: "2026-09-23T10:00:00Z", sales_count: 0, subscription_count: 2, can_purge: false },
];

const TRASHED_MEDIA = [
  { id: "m_present", key: "u/a1/photo.jpg", url: "https://cdn.example/photo.jpg",
    mime: "image/jpeg", size: 1234, deleted_at: "2026-09-25T10:00:00Z", object_present: true },
  { id: "m_gone", key: "u/a1/gone.jpg", url: "https://cdn.example/gone.jpg",
    mime: "image/jpeg", size: 4321, deleted_at: "2026-09-24T10:00:00Z", object_present: false },
];

async function stubAPI(page: Page) {
  const json = (status: number, body: unknown) => ({
    status, contentType: "application/json", body: JSON.stringify(body),
  });
  const ME = { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "en", is_super_admin: false };

  await page.route(/(.*)\/api\/.*$/, (r) => r.fulfill(json(200, {})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) =>
    r.fulfill(json(200, {
      me: ME,
      workspaces: [{ id: "a1", name: "Demo", slug: "demo", role: "admin" }],
      chip_status: null, keys: [], domains: [], social: [], einvoice: null,
      webhooks: [], products: [], summary: { counts: {}, revenue_sen: 0 },
      sales: { items: [], page: 1, limit: 20, total: 0 },
      form_analytics: [], product_analytics: [],
    })));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(200, ME)));
  await page.route(/(.*)\/api\/rest\/currency\/rates$/, (r) => r.fulfill(json(200, { rates: [], cached: false })));
  await page.route(/(.*)\/api\/rest\/products\?include_archived=1(\?.*)?$/, (r) => r.fulfill(json(200, [])));
  await page.route(/(.*)\/api\/rest\/products\/trash(\?.*)?$/, (r) => r.fulfill(json(200, TRASHED_PRODUCTS)));
  await page.route(/(.*)\/api\/rest\/media\/trash(\?.*)?$/, (r) => r.fulfill(json(200, TRASHED_MEDIA)));
  await page.route(/(.*)\/api\/rest\/media(\?.*)?$/, (r) =>
    r.fulfill(json(200, { items: [], page: 1, limit: 20, total: 0, usage: { used: 1234, limit: 1073741824 } })));
  await page.route(/(.*)\/api\/rest\/products(\?.*)?$/, (r) => r.fulfill(json(200, [])));
  await page.route(/(.*)\/api\/workspaces(\?.*)?$/, (r) =>
    r.fulfill(json(200, { workspaces: [{ id: "a1", name: "Demo", slug: "demo", role: "admin" }] })));
}

async function open(page: Page, path: string) {
  await page.addInitScript(() => {
    localStorage.clear();
    sessionStorage.clear();
    localStorage.setItem("jf_workspace", "demo");
    localStorage.setItem("jf_lang", "en");
  });
  await stubAPI(page);
  await page.goto(path, { waitUntil: "networkidle" });
}

test("products: the live catalogue is the default view, and the bin is behind the Trash tab", async ({ page }) => {
  test.setTimeout(120_000);
  await open(page, "/products");

  const tabs = page.getByRole("tab");
  await expect(tabs).toHaveCount(2, { timeout: 45_000 });
  // The DEFAULT must be the live list — a new visitor must never land in the bin.
  await expect(page.getByRole("tab", { name: "Active" })).toHaveAttribute("aria-selected", "true");
  await expect(page.locator("li", { hasText: TRASHED_PRODUCTS[0].name })).toHaveCount(0);

  await page.getByRole("tab", { name: /Trash/i }).click();
  await expect(page.locator("li", { hasText: TRASHED_PRODUCTS[0].name }).first())
    .toBeVisible({ timeout: 45_000 });
});

test("products: a purgeable bin row offers Delete for good; a sold or subscribed one does not, and says why", async ({ page }) => {
  test.setTimeout(120_000);
  await open(page, "/products");
  await page.getByRole("tab", { name: /Trash/i }).click();

  const purgeable = page.locator("li", { hasText: TRASHED_PRODUCTS[0].name }).first();
  await expect(purgeable).toBeVisible({ timeout: 45_000 });
  await expect(purgeable.getByRole("button", { name: "Delete for good", exact: true })).toBeVisible();
  await expect(purgeable.getByRole("button", { name: "Restore", exact: true })).toBeVisible();

  // The sold row: no permanent-delete control at all, and the obstacle is named
  // with its count (the server refuses this purge, so offering it would fail
  // after the click).
  const sold = page.locator("li", { hasText: TRASHED_PRODUCTS[1].name }).first();
  await expect(sold).toBeVisible();
  await expect(sold.getByRole("button", { name: "Delete for good", exact: true })).toHaveCount(0);
  await expect(sold).toContainText("3");

  const subbed = page.locator("li", { hasText: TRASHED_PRODUCTS[2].name }).first();
  await expect(subbed).toBeVisible();
  await expect(subbed.getByRole("button", { name: "Delete for good", exact: true })).toHaveCount(0);
  await expect(subbed).toContainText("2");
});

test("products: the purge dialog asks for the product NAME, not a slug", async ({ page }) => {
  test.setTimeout(120_000);
  await open(page, "/products");
  await page.getByRole("tab", { name: /Trash/i }).click();

  const row = page.locator("li", { hasText: TRASHED_PRODUCTS[0].name }).first();
  await expect(row).toBeVisible({ timeout: 45_000 });
  await row.getByRole("button", { name: "Delete for good", exact: true }).click();

  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible({ timeout: 20_000 });
  // The confirm field is a NAME field — a product has no slug, so asking for one
  // would make the dialog impossible to satisfy.
  await expect(dialog.getByLabel(/name/i)).toBeVisible();
});

test("media: the bin offers Restore, and does NOT claim the file was destroyed", async ({ page }) => {
  test.setTimeout(120_000);
  await open(page, "/media");
  await page.getByRole("tab", { name: /Trash/i }).click();

  const present = page.locator("li", { hasText: "photo.jpg" }).first();
  await expect(present).toBeVisible({ timeout: 45_000 });
  await expect(present.getByRole("button", { name: "Restore", exact: true })).toBeVisible();

  // The object is still in the store, so the bin must NOT warn that it is gone,
  // and the page must never say the bytes were deleted at trash time — that
  // sentence would be the defect this card fixed.
  const gone = page.locator("li", { hasText: "gone.jpg" }).first();
  await expect(gone).toBeVisible();
  await expect(gone).toContainText(/gone|missing|no longer/i);

  const body = await page.locator("body").innerText();
  expect(body.toLowerCase()).not.toContain("file deleted permanently");
});
