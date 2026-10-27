// A REJECTING fetch in the builder's save() used to leave BOTH Save buttons on
// "Saving…" forever, disabled, with no sentence anywhere (t_d0532421).
//
// THE DEFECT, as reported and re-measured on a running stack — `/builder?new=1`,
// a title typed, ONE click on "Save & publish", with the create aborted at the
// TRANSPORT level (no HTTP status at all: a dropped connection, which is what
// `TypeError: Failed to fetch` is):
//
//   PAGEERROR: Failed to fetch
//   BUTTONS_NOW=["Saving…","Save as template","Saving…", …]
//   UNHANDLED_REJECTIONS=["TypeError: Failed to fetch"]
//   HAS_FAILURE_SENTENCE=false
//
// i.e. `saving` stayed true, so the two save buttons stayed disabled and read
// "Saving…", the sibling "Save as template" button (also `disabled={saving}`)
// went dead with them, `document.body.innerText` carried no failure sentence,
// and the rejection escaped the click handler uncaught. The merchant had a dead
// button and no cause. Only a reload recovered the page.
//
// MECHANISM: `save()` set `saving = true` and had no try/catch/finally. Its five
// early-return paths released the flag one by one and the success tail released
// it too — but an exception out of any `await fetch(...)` exits the function
// without passing any of them. The sibling `publishAnyway()` already released it
// in a `finally`, so this was one function, not the file's style.
//
// WHAT THIS SPEC DRIVES. The real built page, with the API stubbed at the
// browser level (no Go server, no session beyond the middleware's cookie, no
// DB — the same harness shape as `builder-publish-panel.spec.ts`, so the gate
// needs nothing but the build it is already serving). Each arm aborts ONE call
// at the transport level and asserts the three things the merchant is owed:
//
//   * the button is not disabled and no longer reads "Saving…";
//   * a failure sentence is on the page;
//   * no unhandled rejection escaped into the console.
//
// The three arms are the three steps of a save — the record write on a new form
// (the reported case), the record write on an already-saved form, and the
// publish call (which the card left unverified).
import { test, expect, type Page } from "@playwright/test";

import { signIn } from "./auth";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page
// renders. This spec visits one, so it declares the session it means — a stub
// value, since the gate reads presence only and this spec stubs the API itself.
// See e2e/auth.ts.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

const NEW_TITLE = "Save failure probe";
const SAVED_ID = "form_save_failure_saved";
const SAVED_TITLE = "Saved form probe";

const FAIL = "Failed to save.";
const SAVE_PUBLISH = "Save & publish";
const PUBLISH = "Publish";

const SAVED_FORM = {
  id: SAVED_ID,
  slug: "save-failure-probe",
  title: SAVED_TITLE,
  status: "draft",
  url: "",
  config: { mode: "response", blocks: [{ id: "b1", type: "text", value: "Hi" }], products: [] },
};

/** Which of the save's own calls the stub actually saw. The abort means there is
 *  no response to read, so the count here is the wire evidence. */
type Hits = { create: number; patch: number; status: number; publish: number };

/**
 * stubAPI answers every /api/* call the builder makes, and aborts exactly ONE of
 * the save's calls at the transport level (`r.abort("failed")` — the browser
 * sees `TypeError: Failed to fetch`, no status code).
 */
async function stubAPI(page: Page, abort: "create" | "record" | "publish"): Promise<Hits> {
  const json = (status: number, body: unknown) => ({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  const hits: Hits = { create: 0, patch: 0, status: 0, publish: 0 };

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
  // The forms COLLECTION. A GET is the (empty) list the page loads; a POST is the
  // create, which has no id yet so the path carries a TRAILING SLASH
  // (`/api/rest/forms/?workspace=…` — the card's own captured wire). A pattern
  // that matched only `/api/rest/forms` would leave the create to the catch-all.
  await page.route(/(.*)\/api\/rest\/forms\/?(\?.*)?$/, (r) => {
    if (r.request().method() !== "POST") return r.fulfill(json(200, []));
    hits.create += 1;
    if (abort === "create") return r.abort("failed");
    return r.fulfill(json(201, { id: "form_created_by_probe", slug: "created-by-probe", title: NEW_TITLE, status: "draft", url: "", config: {} }));
  });
  // The hydrated form, and the record write (PATCH) on it.
  await page.route(new RegExp(`(.*)/api/rest/forms/${SAVED_ID}(\\?.*)?$`), (r) => {
    if (r.request().method() === "GET") return r.fulfill(json(200, SAVED_FORM));
    hits.patch += 1;
    if (abort === "record") return r.abort("failed");
    return r.fulfill(json(200, SAVED_FORM));
  });
  await page.route(new RegExp(`(.*)/api/rest/forms/${SAVED_ID}/status(\\?.*)?$`), (r) => {
    hits.status += 1;
    return r.fulfill(json(200, { status: "published" }));
  });
  await page.route(new RegExp(`(.*)/api/rest/forms/${SAVED_ID}/publish(\\?.*)?$`), (r) => {
    hits.publish += 1;
    if (abort === "publish") return r.abort("failed");
    return r.fulfill(json(200, { status: "published", url: "https://demo.jomform.com/save-failure-probe" }));
  });
  return hits;
}

/** A clean profile, in ENGLISH, on the stub — and the console half of the
 *  evidence: every uncaught exception and every unhandled rejection. */
async function cleanProfile(page: Page): Promise<string[]> {
  await page.addInitScript(() => {
    localStorage.setItem("jf_workspace", "demo");
    localStorage.setItem("jf_lang", "en");
    const w = window as unknown as { __rejections: string[] };
    w.__rejections = [];
    window.addEventListener("unhandledrejection", (e) => {
      w.__rejections.push(String((e as PromiseRejectionEvent).reason));
    });
  });
  const escaped: string[] = [];
  page.on("pageerror", (err) => escaped.push(`pageerror: ${err.message}`));
  return escaped;
}

const rejections = (page: Page) =>
  page.evaluate(() => (window as unknown as { __rejections?: string[] }).__rejections || []);

/** The save button the CURRENT viewport renders: at >=768px it is the desktop
 *  one ("Publish" once the form has an id, "Save & publish" otherwise); below
 *  that the mobile one in row 1, which never says "Publish". */
const saveButton = (page: Page, name: string) => page.getByRole("button", { name, exact: true }).first();

/**
 * The three assertions every arm owes, with a DOM dump on failure: a red here is
 * either this defect or the harness never reaching the handler, and the
 * difference is in the dumped button labels and request counts.
 */
async function expectRecovered(page: Page, button: ReturnType<typeof saveButton>, label: string, escaped: string[]) {
  try {
    await expect(button).toBeEnabled({ timeout: 20_000 });
    await expect(button).toHaveText(label);
  } catch (err) {
    const labels = await page.evaluate(() =>
      Array.from(document.querySelectorAll("button")).map((b) => `${b.disabled ? "[disabled] " : ""}${(b.textContent || "").trim()}`),
    );
    console.log(`BUTTONS_NOW=${JSON.stringify(labels)}`);
    console.log(`HAS_FAILURE_SENTENCE=${(await page.evaluate(() => document.body.innerText)).includes(FAIL)}`);
    console.log(`PAGE_ERRORS=${JSON.stringify(escaped)}`);
    throw err;
  }
}

test("a create whose transport fails releases both Save buttons, says so, and rejects nothing", async ({ page }) => {
  test.setTimeout(120_000);
  const escaped = await cleanProfile(page);
  const hits = await stubAPI(page, "create");
  await page.goto("/builder?new=1", { waitUntil: "networkidle" });

  // The button is rendered only once the page is a real, hydrated builder.
  const button = saveButton(page, SAVE_PUBLISH);
  await expect(button).toBeVisible({ timeout: 45_000 });
  await page.getByRole("textbox", { name: "Form title" }).fill(NEW_TITLE);

  await button.click();

  // The create really was attempted, and really was aborted — not a 500, not a
  // 200 with an empty body: nothing answered at all.
  await expect.poll(() => hits.create, {
    message: "the builder never attempted the create — this arm proves nothing",
    timeout: 20_000,
  }).toBe(1);

  // 1. The button leaves the busy state, under its own name: no id was written,
  //    so this is still an unsaved form.
  await expectRecovered(page, button, SAVE_PUBLISH, escaped);
  // 2. The merchant is told, in the panel the page already uses for a save that
  //    the API refused.
  await expect(page.getByText(FAIL, { exact: true })).toBeVisible({ timeout: 20_000 });
  // 3. The sibling "Save as template" button is not dead too — it is
  //    `disabled={saving}` as well, which is how one stuck flag killed three
  //    controls.
  await expect(page.getByRole("button", { name: "Save as template", exact: true })).toBeEnabled();
  // The form is still unsaved, so the marker must still say so (rendered as
  // "• unsaved" — the bullet is part of the element's text).
  await expect(page.getByText(/unsaved/).first()).toBeVisible();
  // 4. Nothing escaped: the rejection is caught, not merely tolerated by the
  //    test runner.
  expect(escaped, "an exception escaped the click handler").toEqual([]);
  expect(await rejections(page), "the click handler's promise rejected unhandled").toEqual([]);
});

test("the MOBILE save button (row 1) recovers from the same aborted create on a phone viewport", async ({ page }) => {
  test.setTimeout(120_000);
  await page.setViewportSize({ width: 390, height: 844 });
  const escaped = await cleanProfile(page);
  const hits = await stubAPI(page, "create");
  await page.goto("/builder?new=1", { waitUntil: "networkidle" });

  const button = saveButton(page, SAVE_PUBLISH);
  await expect(button).toBeVisible({ timeout: 45_000 });
  await page.getByRole("textbox", { name: "Form title" }).fill(NEW_TITLE);

  await button.click();
  await expect.poll(() => hits.create, {
    message: "the builder never attempted the create — this arm proves nothing",
    timeout: 20_000,
  }).toBe(1);

  await expectRecovered(page, button, SAVE_PUBLISH, escaped);
  await expect(page.getByText(FAIL, { exact: true })).toBeVisible({ timeout: 20_000 });
  // The error line lives in the canvas column, which is the default face on a
  // phone — so the sentence is VISIBLE there, not merely in the DOM.
  expect(escaped).toEqual([]);
  expect(await rejections(page)).toEqual([]);
});

test("an already-saved form whose PATCH is aborted recovers too (the same absence of a catch)", async ({ page }) => {
  test.setTimeout(120_000);
  const escaped = await cleanProfile(page);
  const hits = await stubAPI(page, "record");
  await page.goto(`/builder?form=${SAVED_ID}`, { waitUntil: "networkidle" });

  // The header's Publish button only reads "Publish" once the form hydrated, so
  // waiting for it is the page-ready signal (a "Save & publish" button means the
  // form never loaded and every assertion below would be meaningless).
  const button = saveButton(page, PUBLISH);
  await expect(button).toBeVisible({ timeout: 45_000 });
  await expect(page.getByRole("textbox", { name: "Form title" })).toHaveValue(SAVED_TITLE);

  await button.click();
  await expect.poll(() => hits.patch, {
    message: "the builder never attempted the record write — this arm proves nothing",
    timeout: 20_000,
  }).toBe(1);
  // The status write comes AFTER the record write, so a stuck flag here is not a
  // status that landed: nothing was written at all.
  expect(hits.status).toBe(0);

  await expectRecovered(page, button, PUBLISH, escaped);
  await expect(page.getByText(FAIL, { exact: true })).toBeVisible({ timeout: 20_000 });
  expect(escaped).toEqual([]);
  expect(await rejections(page)).toEqual([]);
});

test("a publish call aborted after the record write is reported, not stuck", async ({ page }) => {
  test.setTimeout(120_000);
  const escaped = await cleanProfile(page);
  const hits = await stubAPI(page, "publish");
  await page.goto(`/builder?form=${SAVED_ID}`, { waitUntil: "networkidle" });

  const button = saveButton(page, PUBLISH);
  await expect(button).toBeVisible({ timeout: 45_000 });
  await expect(page.getByRole("textbox", { name: "Form title" })).toHaveValue(SAVED_TITLE);

  await button.click();
  await expect.poll(() => hits.publish, {
    message: "the builder never attempted the publish call — this arm proves nothing",
    timeout: 20_000,
  }).toBe(1);
  // The two writes before it DID land: this arm is about the LAST step throwing,
  // which is the case the card left unmeasured.
  expect(hits.patch).toBe(1);
  expect(hits.status).toBe(1);

  await expectRecovered(page, button, PUBLISH, escaped);
  await expect(page.getByText(FAIL, { exact: true })).toBeVisible({ timeout: 20_000 });
  // No green "live" panel for a publish that never answered.
  await expect(page.getByRole("status")).toHaveCount(0);
  expect(escaped).toEqual([]);
  expect(await rejections(page)).toEqual([]);
});
