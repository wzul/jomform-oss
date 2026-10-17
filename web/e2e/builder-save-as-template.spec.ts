// "Save as template" on an UNSAVED form: the form is created and the template
// silently never is (t_c33effe4).
//
// THE DEFECT, as reported and re-measured on a running stack — `/builder?new=1`,
// a title typed, ONE click on "Save as template":
//
//   POST /api/rest/forms/?workspace=…      201    <- the form WAS created
//   POST /api/rest/forms/{id}/status?…     200
//   -- no POST /api/rest/templates at all --
//
// and the page rendered neither "Saved as a template." nor "Failed to save as
// template.", so the button looked like it had done nothing beyond saving a
// draft. A form that ALREADY had an id was unaffected, because the block that
// ran was skipped — which is why this only bit the new-form path.
//
// MECHANISM: the handler awaited `save(false)` and then re-read `currentId`.
// That is a VALUE captured by the render's closure — the `setCurrentId` inside
// save() re-renders the component but cannot rebind a variable an already-running
// handler holds — so the second read saw its own stale null and returned BEFORE
// either feedback arm. It now snapshots the id save() REPORTS it wrote.
//
// WHAT THIS SPEC DRIVES. The real built page, with the API stubbed at the browser
// level (no Go server, no session, no DB — the same harness shape as
// `builder-publish-panel.spec.ts`, so the gate needs nothing but the build it is
// already serving). The assertions are the merchant's own two signals: the
// request the page actually made, in order, and the sentence it rendered.
//
// The create's 201 body is the ONLY place the new form's id exists, so an
// assertion that the template POST carries that id is exactly the assertion that
// the stale read is gone.
import { test, expect, type Page, type Request } from "@playwright/test";

import { signIn } from "./auth";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page renders.
// This spec visits one, so it declares the session it means — a stub value, since
// the gate reads presence only and this spec stubs the API itself. See e2e/auth.ts.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

const NEW_ID = "form_save_as_template_new";
const TITLE = "Save as template probe";
const DONE = "Saved as a template.";
const FAIL = "Failed to save as template.";

const FORM = {
  id: NEW_ID,
  slug: "save-as-template-probe",
  title: TITLE,
  status: "draft",
  url: "",
  config: { mode: "response", blocks: [{ id: "b1", type: "text", value: "Hi" }], products: [] },
};

type Call = { method: string; path: string; body: string };

/**
 * Every /api/* request the page makes, in order — the client-side half of the
 * evidence the card quotes (the API's own access log is the other half, and is
 * not available to a browser-level stub).
 */
function record(page: Page): Call[] {
  const calls: Call[] = [];
  page.on("request", (req: Request) => {
    const path = new URL(req.url()).pathname;
    if (!path.startsWith("/api/")) return;
    calls.push({ method: req.method(), path, body: req.postData() || "" });
  });
  return calls;
}

const templatePosts = (calls: Call[]) =>
  calls.filter((c) => c.method === "POST" && c.path === "/api/rest/templates");

/** A create POSTs to the collection, which has no id yet — so its path is
 *  `/api/rest/forms/` WITH the trailing slash (the card's own captured wire). */
const createPosts = (calls: Call[]) =>
  calls.filter((c) => c.method === "POST" && (c.path === "/api/rest/forms" || c.path === "/api/rest/forms/"));

/**
 * stubAPI answers every /api/* call the builder makes. The template POST answers
 * `templateStatus`, so the same harness drives the success sentence and the
 * failure sentence.
 */
async function stubAPI(page: Page, templateStatus: number) {
  const json = (status: number, body: unknown) => ({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });

  // Catch-all FIRST; Playwright resolves routes in REVERSE registration order,
  // so every specific route below wins.
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
  // The forms COLLECTION: a GET is the (empty) list the page loads, a POST is the
  // create this button performs first — and the 201 body is where the new id is
  // born. The trailing slash is NOT optional in this pattern: a create has no id
  // yet, so `plan.recordPath("")` builds `/api/rest/forms/`, which the card's own
  // captured wire shows (`POST /api/rest/forms/?workspace=…`). A pattern that
  // matched only `/api/rest/forms` would leave the create to the catch-all, whose
  // 200 `{}` has no id — i.e. the spec would reproduce this defect for a reason of
  // its own making.
  await page.route(/(.*)\/api\/rest\/forms\/?(\?.*)?$/, (r) =>
    r.request().method() === "POST" ? r.fulfill(json(201, FORM)) : r.fulfill(json(200, [])),
  );
  // The hydrated form, for the "already saved" arm. Registered before the status
  // route so the more specific one still wins for a POST.
  await page.route(new RegExp(`(.*)/api/rest/forms/${NEW_ID}(\\?.*)?$`), (r) => r.fulfill(json(200, FORM)));
  await page.route(new RegExp(`(.*)/api/rest/forms/${NEW_ID}/status(\\?.*)?$`), (r) =>
    r.fulfill(json(200, { status: "draft" })),
  );
  await page.route(/(.*)\/api\/rest\/templates(\?.*)?$/, (r) =>
    r.fulfill(json(templateStatus, templateStatus < 300 ? { id: "tpl_save_as_template" } : { error: "refused" })),
  );
}

/** A clean profile, in English, on the stub. */
async function cleanProfile(page: Page) {
  await page.addInitScript(() => {
    localStorage.setItem("jf_workspace", "demo");
    localStorage.setItem("jf_lang", "en");
  });
}

const saveAsTemplateButton = (page: Page) =>
  page.getByRole("button", { name: "Save as template", exact: true });

/**
 * Wait for the template POST, and DUMP the wire when it does not arrive — a
 * failure here is either this defect or the harness not reaching the handler, and
 * the difference is in the request list, not in the assertion text. The dump also
 * reads the page's own feedback line, because the reported symptom was TWO
 * things: no template posted AND neither sentence rendered.
 */
async function expectTemplatePosted(page: Page, calls: Call[]) {
  try {
    await expect
      .poll(() => templatePosts(calls).length, {
        message:
          "no POST /api/rest/templates — the form was created and the template was silently never posted (t_c33effe4)",
        timeout: 20_000,
      })
      .toBe(1);
  } catch (err) {
    const text = await page.evaluate(() => document.body.innerText);
    console.log(`feedback in the DOM: done=${text.includes(DONE)} fail=${text.includes(FAIL)}`);
    console.log(`requests the page made:\n${JSON.stringify(calls, null, 2)}`);
    throw err;
  }
}

test("an unsaved form posts the template for the id its own create produced, and says so", async ({ page }) => {
  test.setTimeout(120_000);
  const calls = record(page);
  await cleanProfile(page);
  await stubAPI(page, 201);
  await page.goto("/builder?new=1", { waitUntil: "networkidle" });
  // The button is rendered only once the page is a real, hydrated builder.
  await expect(saveAsTemplateButton(page)).toBeVisible({ timeout: 45_000 });
  await page.getByRole("textbox", { name: "Form title" }).fill(TITLE);

  await saveAsTemplateButton(page).click();

  await expectTemplatePosted(page, calls);

  const created = calls.findIndex(
    (c) => c.method === "POST" && (c.path === "/api/rest/forms" || c.path === "/api/rest/forms/"),
  );
  const snapshot = calls.findIndex((c) => c.method === "POST" && c.path === "/api/rest/templates");
  expect(created, "the builder never created the form at all").toBeGreaterThanOrEqual(0);
  expect(created, "the template was snapshotted BEFORE the form existed").toBeLessThan(snapshot);

  // The id can only have come from the create's own 201 body: the handler's
  // `currentId` was null when it started and no re-render can rebind it.
  expect(JSON.parse(calls[snapshot].body), "the template was snapshotted for an id the create did not return").toMatchObject({
    form_id: NEW_ID,
    title: TITLE,
  });

  // …and the merchant is told, which is the half that was missing too.
  await expect(page.getByText(DONE)).toBeVisible({ timeout: 20_000 });
});

test("a refused template POST is reported, not silent", async ({ page }) => {
  test.setTimeout(120_000);
  const calls = record(page);
  await cleanProfile(page);
  await stubAPI(page, 500);
  await page.goto("/builder?new=1", { waitUntil: "networkidle" });
  await expect(saveAsTemplateButton(page)).toBeVisible({ timeout: 45_000 });
  await page.getByRole("textbox", { name: "Form title" }).fill(TITLE);

  await saveAsTemplateButton(page).click();

  await expectTemplatePosted(page, calls);
  await expect(page.getByText(FAIL)).toBeVisible({ timeout: 20_000 });
  await expect(page.getByText(DONE)).toHaveCount(0);
});

test("a form that already has an id is snapshotted without being saved again", async ({ page }) => {
  test.setTimeout(120_000);
  const calls = record(page);
  await cleanProfile(page);
  await stubAPI(page, 201);
  await page.goto(`/builder?form=${NEW_ID}`, { waitUntil: "networkidle" });
  await expect(saveAsTemplateButton(page)).toBeVisible({ timeout: 45_000 });
  // The hydrated title, so the POST body below is evidence the page really loaded
  // the form rather than starting empty.
  await expect(page.getByRole("textbox", { name: "Form title" })).toHaveValue(TITLE);

  await saveAsTemplateButton(page).click();

  await expectTemplatePosted(page, calls);
  expect(JSON.parse(templatePosts(calls)[0].body)).toMatchObject({ form_id: NEW_ID, title: TITLE });
  expect(
    createPosts(calls),
    "the button SAVED an already-saved form — a write the merchant did not ask for",
  ).toHaveLength(0);
  await expect(page.getByText(DONE)).toBeVisible({ timeout: 20_000 });
});
