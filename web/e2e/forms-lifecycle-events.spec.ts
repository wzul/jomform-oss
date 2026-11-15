// The /forms row's pill and its buttons come from the SERVER (card t_3d5a8d32).
//
// Renders the REAL /forms page against a stubbed wire and drives the actions the
// page now renders from the API's available_events: the LIVE pill and the amber
// pill both come from accepting_orders, and Publish/Unpublish/Delete exist only
// where the server says the row accepts them. The stub is the point of the
// test — it lets one run put the five shapes on screen at once (live+sellable,
// live+closed, draft, and a template whose publish guard refuses), which a real
// account cannot be made to hold in a single page load.
import { test, expect, type Page } from "@playwright/test";

import { signIn } from "./auth";

test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

const LIVE = {
  id: "form_live_probe",
  slug: "live-form",
  title: "Live sellable form",
  status: "published",
  url: "https://demo.jomform.com/live-form",
  mode: "payment",
  accepting_orders: true,
  not_accepting_orders: false,
  available_events: [
    { event: "unpublish", label_key: "forms.unpublish", enabled: true },
    { event: "delete", label_key: "forms.delete", enabled: true },
  ],
};

const DEAD = {
  id: "form_dead_probe",
  slug: "closed-form",
  title: "Closed form",
  status: "published",
  url: "https://demo.jomform.com/closed-form",
  mode: "payment",
  accepting_orders: false,
  code: "closed",
  reason: "this form has closed",
  not_accepting_orders: false,
  available_events: [
    { event: "unpublish", label_key: "forms.unpublish", enabled: true },
    { event: "delete", label_key: "forms.delete", enabled: true },
  ],
};

const DRAFT = {
  id: "form_draft_probe",
  slug: "draft-form",
  title: "Draft form",
  status: "draft",
  url: "",
  mode: "response",
  accepting_orders: false,
  code: "not_published",
  reason: "this form is not published",
  available_events: [
    { event: "publish", label_key: "builder.publish", enabled: true },
    { event: "delete", label_key: "forms.delete", enabled: true },
  ],
};

const TEMPLATE = {
  id: "form_template_probe",
  slug: "template-form",
  title: "Template without metadata",
  status: "draft",
  url: "",
  mode: "payment",
  accepting_orders: false,
  code: "not_published",
  reason: "this form is not published",
  available_events: [
    {
      event: "publish",
      label_key: "builder.publish",
      enabled: false,
      reason: "a template needs a category and one description block (Section or text) before it can be published to the gallery",
    },
    { event: "delete", label_key: "forms.delete", enabled: true },
  ],
};

async function stubAPI(page: Page) {
  const json = (status: number, body: unknown) => ({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  const posts: string[] = [];
  const deletes: string[] = [];

  await page.route(/(.*)\/api\/.*$/, (r) => r.fulfill(json(200, {})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) =>
    r.fulfill(
      json(200, {
        me: { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "en", is_super_admin: false },
        workspaces: [{ id: "ws1", name: "Demo", slug: "demo", role: "admin" }],
        chip_status: null, keys: [], domains: [], social: [], einvoice: null,
        webhooks: [], products: [], summary: { counts: {}, revenue_sen: 0 },
        sales: { items: [], page: 1, limit: 20, total: 0 },
        form_analytics: [], product_analytics: [],
      }),
    ),
  );
  await page.route(/(.*)\/api\/auth\/me$/, (r) =>
    r.fulfill(json(200, { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "en", is_super_admin: false })),
  );
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json(200, { lang: "en" })));
  await page.route(/(.*)\/api\/rest\/currency\/rates$/, (r) => r.fulfill(json(200, { rates: [], cached: false })));
  await page.route(/(.*)\/api\/rest\/products(\?.*)?$/, (r) => r.fulfill(json(200, [])));
  await page.route(/(.*)\/api\/rest\/forms\/trash(\?.*)?$/, (r) => r.fulfill(json(200, [])));
  await page.route(/(.*)\/api\/rest\/forms\/[^/?]+\/status(\?.*)?$/, (r) => {
    posts.push(`${r.request().method()} ${r.request().url()} ${r.request().postData() ?? ""}`);
    return r.fulfill(json(200, { status: "draft" }));
  });
  await page.route(/(.*)\/api\/rest\/forms\/[^/?]+(\?.*)?$/, (r) => {
    if (r.request().method() === "DELETE") {
      deletes.push(r.request().url());
      return r.fulfill(json(200, { status: "deleted" }));
    }
    return r.fulfill(json(200, {}));
  });
  await page.route(/(.*)\/api\/rest\/forms(\?.*)?$/, (r) => r.fulfill(json(200, [LIVE, DEAD, DRAFT, TEMPLATE])));
  return { posts, deletes };
}

test("the /forms row renders its pill and its buttons from the server's answer", async ({ page }) => {
  test.setTimeout(120_000);
  await page.addInitScript(() => {
    localStorage.clear();
    localStorage.setItem("jf_workspace", "demo");
    localStorage.setItem("jf_lang", "en");
  });
  const stub = await stubAPI(page);
  await page.goto("/forms", { waitUntil: "networkidle" });

  const row = (title: string) => page.locator("li").filter({ hasText: title });

  // 1. published + accepting → clean LIVE pill, Unpublish + Delete
  const live = row(LIVE.title);
  await expect(live, "the live row must render").toBeVisible({ timeout: 45_000 });
  await expect(live.getByText("LIVE", { exact: true })).toBeVisible();
  await expect(live.getByText("Not accepting orders", { exact: true })).toHaveCount(0);
  await expect(live.getByRole("button", { name: "Unpublish", exact: true })).toBeVisible();
  await expect(live.getByRole("button", { name: "Delete", exact: true })).toBeVisible();

  // 2. published + NOT accepting (closed window) → amber pill carrying the SERVER's reason
  const dead = row(DEAD.title);
  await expect(dead.getByText("Not accepting orders", { exact: true })).toBeVisible();
  await expect(
    dead.locator('[title="this form has closed"]'),
    "the amber pill must carry the server's own sentence, not a products guess",
  ).toHaveCount(1);
  await expect(dead.getByRole("button", { name: "Unpublish", exact: true })).toBeVisible();
  await expect(dead.getByRole("button", { name: "Delete", exact: true })).toBeVisible();

  // 3. draft → Publish + Delete, and NO Unpublish (the table does not offer a no-op)
  const draft = row(DRAFT.title);
  await expect(draft.getByRole("button", { name: "Publish", exact: true })).toBeVisible();
  await expect(draft.getByRole("button", { name: "Delete", exact: true })).toBeVisible();
  await expect(draft.getByRole("button", { name: "Unpublish", exact: true })).toHaveCount(0);

  // 4. a template whose publish guard refuses → NO Publish button, Delete still there
  const tpl = row(TEMPLATE.title);
  await expect(tpl.getByRole("button", { name: "Delete", exact: true })).toBeVisible();
  await expect(tpl.getByRole("button", { name: "Publish", exact: true })).toHaveCount(0);

  // 5. the Unpublish button really drives the lifecycle edge (status → draft)
  await live.getByRole("button", { name: "Unpublish", exact: true }).click();
  await expect.poll(() => stub.posts.length, { timeout: 10_000 }).toBe(1);
  expect(stub.posts[0]).toContain("/api/rest/forms/form_live_probe/status");
  expect(stub.posts[0]).toContain('{"status":"draft"}');
  await expect(page.getByText("Form unpublished. It is a draft again.", { exact: true })).toBeVisible({ timeout: 10_000 });

  // 6. the Delete button is a confirmed soft delete (DELETE /forms/{id})
  page.on("dialog", (d) => d.accept());
  await draft.getByRole("button", { name: "Delete", exact: true }).click();
  await expect.poll(() => stub.deletes.length, { timeout: 10_000 }).toBe(1);
  expect(stub.deletes[0]).toContain("/api/rest/forms/form_draft_probe");
  await expect(page.getByText("Form moved to the trash. You can restore it from the Trash tab.", { exact: true })).toBeVisible({ timeout: 10_000 });

  await expect(page.locator("html")).toHaveAttribute("lang", "en");
  console.log(`t_3d5a8d32 evidence: posts=${JSON.stringify(stub.posts)} deletes=${JSON.stringify(stub.deletes)}`);
});
