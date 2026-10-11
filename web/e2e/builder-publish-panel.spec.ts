// Regression guard for the /builder publish panel — the two halves that the
// #6177 rebase had to UNION, and that a careless conflict resolution destroys.
//
// WHY THIS FILE EXISTS
// Two changes met in the same `if (!sr.ok)` block of the builder's status write:
//
//   * #694 (on main) turns a refused status write into the warning panel with a
//     "Publish anyway" action — a payment form with nothing to sell is not an
//     opaque failure, it is a question the merchant has to answer. Losing that
//     branch silently deletes the whole "publish anyway" flow.
//   * #6177 (the branch) says the ERROR branch of that panel must not print the
//     API's own `error` prose (which is Malay, because the API never learns the
//     caller's language on a JSON call). It routes the sentence through the
//     machine `code` and the dashboard dictionary.
//
// Resolving that conflict by taking either side alone passes every unit test
// while breaking one of the two behaviours. This guard drives the REAL built
// page with a stubbed API in both outcomes, in English, and asserts what the
// merchant actually reads:
//
//   1. code=no_products (409)  → the amber warning + a working "Publish anyway"
//      button, whose click re-sends the acknowledged publish and reaches the
//      green "your form is live" panel.
//   2. any other 409 with a Malay `error` → the ERROR panel shows the ENGLISH
//      dictionary sentence, and the Malay prose appears NOWHERE in the panel.
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

const FORM_ID = "form_publish_panel_guard";

// The API's wire prose for the no-products refusal, verbatim from
// api/internal/httpapi/rest_handlers.go (writePublishNeedsAck). It is what the
// dashboard must never print.
const SERVER_PROSE =
  "Borang bayaran ini tiada produk — ia tidak boleh menerima bayaran. " +
  "Tambah produk dahulu, atau sahkan untuk teruskan.";

// A second, unrelated Malay refusal the branch has NO specific code for, so the
// error branch falls back to the page's own English sentence.
const OTHER_PROSE = "sesuatu yang tidak dijangka berlaku pada pelayan";

const EN_STATUS_ERROR = "Failed to set the form status.";

const PUBLIC_URL = "https://demo.jomform.com/publish-panel";

const FORM = {
  id: FORM_ID,
  slug: "publish-panel",
  title: "Publish panel",
  status: "draft",
  url: "",
  config: { mode: "payment", blocks: [{ id: "b1", type: "text", value: "Hi" }], products: [] },
};

type Outcome = "needs_ack" | "other_error" | "ok";

/**
 * stubAPI answers every /api/* call the builder makes, and makes the STATUS
 * write (`POST /api/rest/forms/{id}/status`) answer `outcome` the first time and
 * succeed afterwards — so the guard can drive the refused write and then the
 * acknowledged retry.
 */
async function stubAPI(page: Page, outcome: Outcome) {
  const json = (status: number, body: unknown) => ({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  let statusCalls = 0;
  // The API resolves the form's public URL server-side, so it is empty until the
  // form is live. The stub mirrors that: the hydration read returns no URL, and
  // the post-publish refresh (which `publishAnyway` does to obtain the link)
  // returns one. A stub that answered the URL up front would not exercise the
  // path that actually produces the green panel's link.
  let published = false;
  const formNow = () => ({ ...FORM, status: published ? "published" : "draft", url: published ? PUBLIC_URL : "" });

  // Catch-all FIRST; Playwright resolves routes in REVERSE registration order,
  // so every specific route below it wins.
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
  await page.route(/(.*)\/api\/rest\/forms(\?.*)?$/, (r) => r.fulfill(json(200, [])));
  // The hydrated form. Registered before the {id}/status route so the more
  // specific status route still wins for a POST.
  await page.route(new RegExp(`(.*)/api/rest/forms/${FORM_ID}(\\?.*)?$`), (r) => r.fulfill(json(200, formNow())));
  await page.route(new RegExp(`(.*)/api/rest/forms/${FORM_ID}/status(\\?.*)?$`), (r) => {
    statusCalls += 1;
    const first = statusCalls === 1;
    if (!first || outcome === "ok") {
      published = true;
      return r.fulfill(json(200, { status: "published" }));
    }
    if (outcome === "needs_ack") {
      return r.fulfill(json(409, { error: SERVER_PROSE, code: "no_products", requires_ack: true }));
    }
    // No code the dashboard knows: the page's own sentence must win over prose.
    return r.fulfill(json(409, { error: OTHER_PROSE }));
  });
  await page.route(new RegExp(`(.*)/api/rest/forms/${FORM_ID}/publish(\\?.*)?$`), (r) => {
    published = true;
    return r.fulfill(json(200, { status: "published", url: PUBLIC_URL }));
  });
}

/** Opens the builder on the stubbed form, in English, with a clean profile. */
async function openBuilder(page: Page, outcome: Outcome) {
  await page.addInitScript(() => {
    localStorage.setItem("jf_workspace", "demo");
    localStorage.setItem("jf_lang", "en");
  });
  await stubAPI(page, outcome);
  await page.goto(`/builder?form=${FORM_ID}`, { waitUntil: "networkidle" });
  // The header's Publish button only reads "Publish" once the form hydrated, so
  // waiting for it is the page-ready signal (a "Save & publish" button means the
  // form never loaded and every assertion below would be meaningless).
  await expect(page.getByRole("button", { name: "Publish", exact: true }).first()).toBeVisible({
    timeout: 45_000,
  });
}

test("a no_products refusal shows the warning panel and 'Publish anyway' publishes", async ({ page }) => {
  test.setTimeout(120_000);
  await openBuilder(page, "needs_ack");

  await page.getByRole("button", { name: "Publish", exact: true }).first().click();

  // #694's half: the warning is a QUESTION with an action, not an error line.
  // Scoped to the builder's own panel: Next mounts a route announcer with
  // role=alert too, so a bare getByRole("alert") is ambiguous.
  const anyway = page.getByRole("button", { name: "Publish anyway", exact: true });
  await expect(anyway).toBeVisible({ timeout: 20_000 });
  const panel = page.locator("div[role=alert]").filter({ has: anyway });
  const panelText = (await panel.innerText()).trim();
  // #6177's half: the sentence comes from the dictionary, and the API's Malay
  // prose is nowhere in the panel.
  expect(panelText).toContain("payment form with no products");
  expect(panelText).not.toContain(SERVER_PROSE);
  expect(panelText).not.toContain("Borang bayaran ini tiada produk");

  await anyway.click();

  // The acknowledged retry reaches the "live" state — the panel is not a dead
  // end that only repeats the warning.
  const live = page.getByRole("status");
  await expect(live).toBeVisible({ timeout: 20_000 });
  const liveText = await live.innerText();
  expect(liveText).toContain("Your form is live");
  expect(liveText).toContain("https://demo.jomform.com/publish-panel");
  // The warning must be gone — a stale amber panel beside a green one is the
  // contradictory state the original render proof caught.
  await expect(page.getByRole("button", { name: "Publish anyway", exact: true })).toHaveCount(0);
});

test("an uncoded failure shows the ENGLISH sentence, never the server's Malay prose", async ({ page }) => {
  test.setTimeout(120_000);
  await openBuilder(page, "other_error");

  await page.getByRole("button", { name: "Publish", exact: true }).first().click();

  // The builder's own panel, not Next's route announcer (also role=alert).
  const errorPanel = page.locator("div[role=alert]").filter({ hasText: EN_STATUS_ERROR });
  await expect(errorPanel).toBeVisible({ timeout: 20_000 });
  const panelText = (await errorPanel.innerText()).trim();

  expect(panelText).toContain(EN_STATUS_ERROR);
  expect(panelText).not.toContain(OTHER_PROSE);
  // And it is the ERROR face, not the warning one: no confirm action is offered
  // for a failure the merchant cannot act on.
  await expect(page.getByRole("button", { name: "Publish anyway", exact: true })).toHaveCount(0);
});
