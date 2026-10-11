// Regression guard: the workspace Trash view must render a binned workspace and
// its Restore button from the wire shape the server actually writes.
//
// THE DEFECT
// GET /v1/workspaces/trash answers a BARE ARRAY whose id field is `id`:
//
//     [{ "id": "...", "name": "...", "slug": "...", "deleted_at": "...",
//        "sales_count": 0, "can_purge": true }]
//
// (pinned on the Go side by TestRESTTrashLifecycle, which decodes the body into
// `var bin []struct{ ID string \`json:"id"\` … }`). `WorkspaceTab` read an
// `{"items":[...],"total":N}` envelope and keyed rows off `workspace_id`:
//
//     fetchJson<{ items: TrashedWorkspace[]; total: number }>("/api/workspaces/trash")
//     setTrash(Array.isArray(r.data?.items) ? r.data.items : []);
//
// `r.data.items` is `undefined` on a bare array, so `Array.isArray(...)` was
// always false and the list was always empty. The page then rendered "The Trash
// is empty." for a user who had just binned a workspace — and because the Restore
// button only exists inside a row, the ONLY way back out of the bin from the
// dashboard was unreachable. Every other part of the feature was green: the
// store, the REST route, the MCP tools and the i18n all had tests. Nothing
// rendered the page against the real wire shape, so nothing caught it.
//
// WHAT THIS GUARD PROVES, on the real built page with a stubbed API:
//   1. a row in the server's own wire shape renders in the Trash section
//      (its name, slug and "Moved to Trash" line are on screen);
//   2. a row reporting `can_purge:false` says WHY (its sales count), so the
//      refusal is visible before any permanent-delete affordance is offered;
//   3. the row's Restore button exists and POSTs to that row's id — the call
//      that brings the workspace back;
//   4. an EMPTY bin still renders its own sentence, so the assertion above
//      cannot be satisfied by rendering the row unconditionally.
//
// The API is stubbed at the browser level (no Go server, no session, no DB), so
// the gate needs nothing but the build it is already serving.
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
import { DICT } from "../src/components/i18nDict";

// The SERVER's shape, copied from the REST test's decode struct (not from the
// page's old type): `id`, not `workspace_id`; a bare array, not an envelope.
const BINNED = [
  {
    id: "ws_binned_1",
    name: "Binned shop",
    slug: "binned-shop",
    deleted_at: "2026-09-01T02:03:04Z",
    sales_count: 0,
    can_purge: true,
  },
  {
    id: "ws_binned_2",
    name: "Sold shop",
    slug: "sold-shop",
    deleted_at: "2026-09-02T02:03:04Z",
    sales_count: 4,
    can_purge: false,
  },
];

const ME = { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "en", is_super_admin: true };

const BUNDLE = {
  me: ME,
  // TWO workspaces, so the one under test is not the last-workspace case
  // (that refusal is the tab's other rule and has its own copy).
  workspaces: [
    { id: "ws1", name: "Demo", slug: "demo", role: "admin" },
    { id: "ws2", name: "Spare", slug: "spare", role: "admin" },
  ],
  chip_status: null, keys: [], domains: [], webhooks: [], social: [], einvoice: null,
  products: [], summary: { counts: {}, revenue_sen: 0 },
  sales: { items: [], page: 1, limit: 20, total: 0 },
  form_analytics: [], product_analytics: [],
};

const CHECK = {
  id: "ws1", name: "Demo", slug: "demo", is_owner: true, role: "admin",
  last_workspace: false, empty: true, blockers: [], can_delete: true,
};

/**
 * stubAPI stubs the dashboard so /settings reaches the Workspace tab without a
 * Go server. `trash` is the exact body the trash route answers with.
 */
async function stubAPI(page: Page, trash: unknown) {
  const json = (status: number, body: unknown) => ({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  // Catch-all FIRST, specific routes after it: Playwright matches a later
  // registration first, so the specific ones below win.
  await page.route(/(.*)\/api\/.*$/, (r) => r.fulfill(json(200, {})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(200, BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(200, ME)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json(200, { lang: "en" })));
  await page.route(/(.*)\/api\/rest\/currency\/rates$/, (r) => r.fulfill(json(200, { rates: [], cached: false })));
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/deletion-check(\?.*)?$/, (r) => r.fulfill(json(200, CHECK)));
  await page.route(/(.*)\/api\/workspaces\/trash(\?.*)?$/, (r) => r.fulfill(json(200, trash)));
}

/** Opens /settings and CLICKS the Workspace tab, with a FRESH browser state. */
async function openWorkspaceTab(page: Page, trash: unknown) {
  await page.addInitScript(() => {
    // Cleared first: jf_lang / jf_workspace left by an earlier spec would make
    // this render a state no new visitor has.
    try {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("jf_workspace", "demo");
      localStorage.setItem("jf_lang", "en");
      // Preselect the tab too. `role` (which gates the admin-only tabs) resolves
      // from the session bundle asynchronously, so the first paint can render a
      // strip WITHOUT the Workspace tab at all — and a helper that then has to
      // click its way in is racing the bundle rather than testing the trash.
      // Preselecting it is the same "fresh but already-signed-in" state a user
      // who was on this tab has.
      localStorage.setItem("jf_settings_tab", "workspace");
    } catch {
      /* storage unavailable */
    }
  });
  await stubAPI(page, trash);
  await page.goto("/settings", { waitUntil: "domcontentloaded" });

  // Wait for the session bundle to resolve an ADMIN role BEFORE clicking.
  //
  // `settings/page.tsx` gates the Workspace section on `isAdmin` (`tab ===
  // "workspace" && isAdmin`), and `role` arrives with the session bundle. The
  // first paint therefore has a 10-tab strip that does not include the two
  // admin-only tabs, and clicking Workspace in that state renders NOTHING —
  // which a version of this helper turned into a 60s click timeout that read
  // like a broken page. The admin-only CHIP tab is the page's OWN visible marker
  // that the role has landed, so waiting for it is derived from the page rather
  // than pinned to a tab count that a new tab would invalidate.
  await expect(page.getByRole("tab", { name: DICT["set.tab.chip"].en })).toBeVisible({ timeout: 45_000 });

  await page.getByRole("tab", { name: DICT["set.tab.workspace"].en }).click();
  await expect(page.getByRole("heading", { name: DICT["ws.dangerTitle"].en })).toBeVisible({ timeout: 45_000 });
}

test("a binned workspace renders in the Trash view with a working Restore", async ({ page }) => {
  test.setTimeout(120_000);
  await openWorkspaceTab(page, BINNED);

  // RULE 1 — both rows are on screen, by their own name and slug. A page reading
  // an envelope renders neither, and fails here.
  const first = page.locator("li", { hasText: "binned-shop" }).first();
  const second = page.locator("li", { hasText: "sold-shop" }).first();
  await expect(first).toBeVisible({ timeout: 20_000 });
  await expect(second).toBeVisible({ timeout: 20_000 });
  await expect(first).toContainText("Binned shop");
  await expect(first).toContainText(DICT["ws.trashDeletedAt"].en);

  // The empty-bin sentence must NOT be on screen while rows are listed — that
  // is exactly what the old code showed instead of the rows.
  await expect(page.getByText(DICT["ws.trashEmpty"].en)).toHaveCount(0);

  // RULE 2 — a row that cannot be purged says why, with its own count.
  await expect(second).toContainText(DICT["ws.trashHeldBySales"].en.replace("{count}", "4"));

  // RULE 3 — Restore exists per row and addresses THAT row's id (the wire's
  // `id`, which is what the route expects). The request is intercepted, so the
  // assertion is on the URL the page built, not on a stub's side effect.
  const restores = page.getByRole("button", { name: DICT["ws.trashRestore"].en, exact: true });
  await expect(restores).toHaveCount(2);

  const requested: string[] = [];
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/restore(\?.*)?$/, (r) => {
    requested.push(new URL(r.request().url()).pathname);
    return r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ status: "restored" }) });
  });
  // The FIRST row's button. `doRestore` reloads the page on success, so this
  // test asserts the CALL rather than waiting for a settled page afterwards.
  await first.getByRole("button", { name: DICT["ws.trashRestore"].en, exact: true }).click();
  await expect.poll(() => requested.length, { timeout: 20_000 }).toBeGreaterThan(0);
  expect(requested[0]).toBe("/api/workspaces/ws_binned_1/restore");
});

test("an empty bin still renders its own sentence (the guard above is not vacuous)", async ({ page }) => {
  test.setTimeout(120_000);
  await openWorkspaceTab(page, []);

  await expect(page.getByRole("heading", { name: DICT["ws.trashTitle"].en })).toBeVisible();
  await expect(page.getByText(DICT["ws.trashEmpty"].en)).toBeVisible({ timeout: 20_000 });
  await expect(page.getByRole("button", { name: DICT["ws.trashRestore"].en, exact: true })).toHaveCount(0);
});
