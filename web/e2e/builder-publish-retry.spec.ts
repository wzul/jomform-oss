// Regression guard for the "Publish anyway" RETRY path — the second half of the
// publish-failure class that #6177's rule could not see (t_10453dd9).
//
// THE DEFECT, reproduced in a real browser before this file existed. Press
// Publish on a payment form with no products: the API answers 409
// code=no_products and the builder offers "Publish anyway". Press it, and let
// the acknowledged RETRY answer an UNCODED 409 whose `error` is Malay prose —
// which is what the server does whenever a refusal has no machine code yet. The
// panel rendered that sentence verbatim:
//
//   notice 1: ["This is a payment form with no products, … Publish anyway …"]
//   notice 2: ["penerbitan gagal atas sebab dalaman pada pelayan"]
//   >> MALAY prose rendered to the merchant? True
//
// WHY #6177's SOURCE GUARD MISSED IT (both halves of it, which is why this spec
// drives the page instead):
//   * the `d.error || fallback` rule bans a `.error` read feeding a `||` — the
//     call site wrote `outcomeMessage(outcome) || t("bld.saveFail")`, a helper
//     call, so no `.error` appeared in the expression;
//   * the render-reachability rule resolves a setter's argument to apiErr — this
//     argument was a local helper call, so it resolved to nothing.
// The prose had travelled through a `message` FIELD that lib/publishResult.ts
// filled from the response's `error`. A source rule that reasons about syntax
// cannot see a field. Hence: measure the merchant's screen.
//
// WHAT THIS PINS: an uncoded failure on the retry path renders the builder's own
// ENGLISH sentence, and the Malay prose appears NOWHERE in the panel. The API is
// stubbed at the browser level, so the gate needs nothing but the build it is
// already serving.
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

const FORM_ID = "form_publish_retry_guard";

// The API's wire prose for the no-products refusal, verbatim from
// api/internal/httpapi/rest_handlers.go (writePublishNeedsAck).
const NEEDS_ACK_PROSE =
  "Borang bayaran ini tiada produk — ia tidak boleh menerima bayaran. " +
  "Tambah produk dahulu, atau sahkan untuk teruskan.";

// The uncoded refusal the retry answers. This is the sentence from the report:
// a plain server failure with NO machine code, so the dashboard can only fall
// back to its own localized line — and used to print this instead.
const RETRY_PROSE = "penerbitan gagal atas sebab dalaman pada pelayan";

// The builder's own sentence for a failed save/publish (`bld.saveFail`), which
// is what the retry must render. Read from the dictionary above so the two
// cannot drift:
//   "bld.saveFail": { en: "Failed to save.", … }
const EN_RETRY_FALLBACK = "Failed to save.";

const FORM = {
  id: FORM_ID,
  slug: "publish-retry",
  title: "Publish retry",
  status: "draft",
  url: "",
  config: { mode: "payment", blocks: [{ id: "b1", type: "text", value: "Hi" }], products: [] },
};

/**
 * stubAPI answers every /api/* call the builder makes.
 *
 *   * the STATUS write refuses ONCE with code=no_products (that is what raises
 *     the warning panel), then succeeds — exactly what `publishAnyway` re-sends;
 *   * the PUBLISH call — reached only by the acknowledged retry — answers an
 *     UNCODED 409 carrying Malay prose.
 *
 * The call counts mirror the report: 1 publish call, 2 status calls.
 */
async function stubAPI(page: Page) {
  const json = (status: number, body: unknown) => ({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  let statusCalls = 0;
  let publishCalls = 0;

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
  await page.route(new RegExp(`(.*)/api/rest/forms/${FORM_ID}(\\?.*)?$`), (r) =>
    r.fulfill(json(200, FORM)),
  );
  await page.route(new RegExp(`(.*)/api/rest/forms/${FORM_ID}/status(\\?.*)?$`), (r) => {
    statusCalls += 1;
    if (statusCalls === 1) {
      return r.fulfill(json(409, { error: NEEDS_ACK_PROSE, code: "no_products", requires_ack: true }));
    }
    return r.fulfill(json(200, { status: "published" }));
  });
  await page.route(new RegExp(`(.*)/api/rest/forms/${FORM_ID}/publish(\\?.*)?$`), (r) => {
    publishCalls += 1;
    // The reported shape: a failure with NO machine code, so the dashboard has
    // nothing to translate and must use its own sentence.
    return r.fulfill(json(409, { error: RETRY_PROSE }));
  });
  return {
    counts: () => ({ statusCalls, publishCalls }),
  };
}

/** Opens the builder on the stubbed form, in English, with a clean profile. */
async function openBuilder(page: Page) {
  await page.addInitScript(() => {
    localStorage.setItem("jf_workspace", "demo");
    localStorage.setItem("jf_lang", "en");
  });
  const stub = await stubAPI(page);
  await page.goto(`/builder?form=${FORM_ID}`, { waitUntil: "networkidle" });
  await expect(page.getByRole("button", { name: "Publish", exact: true }).first()).toBeVisible({
    timeout: 45_000,
  });
  return stub;
}

test("the 'Publish anyway' retry renders ENGLISH, never the API's Malay prose", async ({ page }) => {
  test.setTimeout(120_000);
  const stub = await openBuilder(page);

  await page.getByRole("button", { name: "Publish", exact: true }).first().click();

  // Step 1: the warning panel with its action (the #694 half, unchanged).
  const anyway = page.getByRole("button", { name: "Publish anyway", exact: true });
  await expect(anyway).toBeVisible({ timeout: 20_000 });
  const warning = page.locator("div[role=alert]").filter({ has: anyway });
  const warningText = (await warning.innerText()).trim();
  expect(warningText).toContain("payment form with no products");
  expect(warningText).not.toContain("Borang bayaran ini tiada produk");

  // Step 2: THE REPORTED CASE. The retry answers an uncoded 409 with Malay prose.
  await anyway.click();

  const errorPanel = page.locator("div[role=alert]").filter({ hasText: EN_RETRY_FALLBACK });
  await expect(errorPanel).toBeVisible({ timeout: 20_000 });
  const panelText = (await errorPanel.innerText()).trim();

  // The merchant reads the builder's own sentence…
  expect(panelText).toContain(EN_RETRY_FALLBACK);
  // …and the server's prose, in either wording, is nowhere in it.
  expect(panelText).not.toContain(RETRY_PROSE);
  expect(panelText).not.toContain("penerbitan gagal");
  expect(panelText).not.toContain(NEEDS_ACK_PROSE);

  const counts = stub.counts();
  expect(counts.publishCalls, "the retry must be the publish call under test").toBe(1);
  expect(counts.statusCalls, "the status write is re-sent acknowledged").toBe(2);

  // The failure face, not the warning one: no confirm action is offered for a
  // failure the merchant cannot act on.
  await expect(page.getByRole("button", { name: "Publish anyway", exact: true })).toHaveCount(0);
});
