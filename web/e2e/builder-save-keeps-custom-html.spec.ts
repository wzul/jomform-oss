// Publish on a custom-HTML form must not delete its config.custom_html
// (card t_59bc3055 — pre-existing, found on prod v1.21.273 / d838f7a8).
//
// THE DEFECT, measured on prod with a throwaway form in the `test-wan`
// workspace: a custom-HTML response form is still listed in /forms with an
// "Edit" link to /builder?form=<id>. That builder is the BLOCK editor and knows
// nothing about custom_html, so pressing its only visible save button (Publish)
// sent a config map without that key, the API replaced the whole jsonb
// (`config=COALESCE($4, config)` in api/internal/dbstore/form.go only protects a
// nil map, not a map missing an entry), and the merchant's HTML body AND its
// field-registration contract were gone — while the panel reported "Your form is
// live". There is no undo: the stored HTML is kept nowhere else.
//
// WHAT THIS SPEC DRIVES. The real built page, with the API stubbed at the
// browser level (no Go server, no session, no DB — the same harness shape as
// `builder-publish-panel.spec.ts`), so the gate needs nothing but the build it
// is already serving. It asserts the PATCH body the page actually produced:
//
//   1. a custom-HTML form: config.custom_html is present and byte-identical to
//      the section that was loaded (html + fields), and the save still writes
//      its own blocks;
//   2. an ordinary block form: the same save does NOT invent a custom_html key
//      (the carry must not become a blanket rewrite), and it keeps the
//      `registered_fields` contract an agent registered — the second victim of
//      the same wholesale replace, and the one that changes what POST /order
//      accepts;
//   3. the panel's own signal ("Your form is live") is asserted in arm 1, so a
//      green cannot come from a save that quietly bailed before the write.
//
// BITES ON THE PRE-FIX BUILD: arm 1 requires custom_html in the body, which the
// pre-fix builder never sent — measured by running this file against a build of
// d838f7a8 (see the card's handoff for the output).
import { test, expect, type Page } from "@playwright/test";

import { signIn } from "./auth";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page
// renders. This spec visits one, so it declares the session it means — a stub
// value, since the gate reads presence only and this spec stubs the API itself.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

const FORM_ID = "form_custom_html_preserve";

// The section exactly as MCP upsert_form stores it: the body under html and the
// registration contract under fields (api/internal/customform/customform.go).
const CUSTOM_HTML = {
  html:
    '<p>Selepas embed</p>\n<label for="email">E-mel</label>\n' +
    '<input id="email" name="email" type="email" required>\n' +
    '<button type="submit">Hantar</button>',
  fields: [{ field_id: "email", type: "email", label: "E-mel", required: true }],
};

// The submit contract an agent registers on a BLOCK form
// (MCP register_form_fields → config.registered_fields).
const REGISTERED = [{ field_id: "email", type: "email", label: "E-mel", required: true }];

const PUBLIC_URL = "https://demo.jomform.com/custom-html-preserve";

/** The form as hydration reads it. `config` is the only part under test. */
const formWith = (config: Record<string, unknown>) => ({
  id: FORM_ID,
  slug: "custom-html-preserve",
  title: "Custom HTML preserve",
  status: "draft",
  url: "",
  config,
});

type Patch = { method: string; path: string; body: string };

/**
 * stubAPI answers every /api/* call the builder makes, records the config write
 * (`PATCH /api/rest/forms/{id}`) and lets the publish flow complete.
 */
async function stubAPI(page: Page, config: Record<string, unknown>) {
  const json = (status: number, body: unknown) => ({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  const patches: Patch[] = [];
  let published = false;
  const formNow = () => ({
    ...formWith(config),
    status: published ? "published" : "draft",
    url: published ? PUBLIC_URL : "",
  });

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
  // The hydrated form. A PATCH to this same path is the config write under test.
  await page.route(new RegExp(`(.*)/api/rest/forms/${FORM_ID}(\\?.*)?$`), (r) => {
    const req = r.request();
    if (req.method() === "PATCH" || req.method() === "PUT") {
      patches.push({ method: req.method(), path: new URL(req.url()).pathname, body: req.postData() || "" });
      return r.fulfill(json(200, { status: "updated", slug: "custom-html-preserve" }));
    }
    return r.fulfill(json(200, formNow()));
  });
  await page.route(new RegExp(`(.*)/api/rest/forms/${FORM_ID}/status(\\?.*)?$`), (r) => {
    published = true;
    return r.fulfill(json(200, { status: "published" }));
  });
  await page.route(new RegExp(`(.*)/api/rest/forms/${FORM_ID}/publish(\\?.*)?$`), (r) => {
    published = true;
    return r.fulfill(json(200, { status: "published", url: PUBLIC_URL }));
  });
  return patches;
}

/** Opens the builder on the stubbed form, in English, with a clean profile. */
async function openBuilder(page: Page, config: Record<string, unknown>) {
  await page.addInitScript(() => {
    localStorage.setItem("jf_workspace", "demo");
    localStorage.setItem("jf_lang", "en");
  });
  const patches = await stubAPI(page, config);
  await page.goto(`/builder?form=${FORM_ID}`, { waitUntil: "networkidle" });
  // The header's Publish button only reads "Publish" once the form hydrated, so
  // waiting for it is the page-ready signal (a "Save & publish" button means the
  // form never loaded and every assertion below would be meaningless).
  await expect(page.getByRole("button", { name: "Publish", exact: true }).first()).toBeVisible({
    timeout: 45_000,
  });
  return patches;
}

test("Publish on a custom-HTML form keeps its custom_html section in the PATCH body", async ({ page }) => {
  test.setTimeout(120_000);
  const patches = await openBuilder(page, { mode: "response", custom_html: CUSTOM_HTML });

  await page.getByRole("button", { name: "Publish", exact: true }).first().click();

  // The save's own signal — proof the click reached the write, so a missing
  // PATCH below cannot read as "preserved".
  const live = page.getByRole("status");
  await expect(live).toBeVisible({ timeout: 20_000 });
  expect(await live.innerText()).toContain("Your form is live");

  expect(patches.length, "the builder never wrote the form's config").toBeGreaterThan(0);
  const body = JSON.parse(patches[patches.length - 1].body) as {
    title?: string;
    config?: Record<string, unknown>;
  };
  const sent = body.config ?? {};

  // THE ASSERTION THE PRE-FIX BUILDER FAILS: the stored section survives.
  expect(sent.custom_html, "the PATCH body carried no custom_html — the merchant's body would be deleted").toEqual(
    CUSTOM_HTML,
  );
  expect((sent.custom_html as typeof CUSTOM_HTML).html).toBe(CUSTOM_HTML.html);
  expect((sent.custom_html as typeof CUSTOM_HTML).fields).toEqual(CUSTOM_HTML.fields);

  // And this is still a save: the builder's own keys are there. They are inert
  // for a custom form — customform.Enabled() is true (html non-empty), so the
  // public page renders the merchant's body and never these blocks (pinned by
  // api/internal/httpapi/custom_form_builder_save_t_59bc3055_test.go). `mode`
  // must stay "response" for the same reason: the custom renderer is reached
  // only for a response-mode form (api/internal/httpapi/public.go, serveForm).
  expect(sent.mode).toBe("response");
  expect(Array.isArray(sent.blocks)).toBe(true);
  expect((sent.blocks as unknown[]).length).toBeGreaterThan(0);
  expect(body.title).toBe("Custom HTML preserve");
});

test("Publish on a block form keeps registered_fields and invents no custom_html", async ({ page }) => {
  test.setTimeout(120_000);
  const patches = await openBuilder(page, {
    mode: "response",
    blocks: [{ id: "b1", type: "text", value: "Nama" }],
    registered_fields: REGISTERED,
  });

  await page.getByRole("button", { name: "Publish", exact: true }).first().click();

  const live = page.getByRole("status");
  await expect(live).toBeVisible({ timeout: 20_000 });
  expect(await live.innerText()).toContain("Your form is live");

  expect(patches.length, "the builder never wrote the form's config").toBeGreaterThan(0);
  const sent = (JSON.parse(patches[patches.length - 1].body) as { config?: Record<string, unknown> }).config ?? {};

  // The carry must not be a blanket rewrite: a form that has no custom body gets
  // no custom_html key.
  expect("custom_html" in sent, "a block form's save grew a custom_html key").toBe(false);
  // The other victim of the same wholesale replace.
  expect(sent.registered_fields, "the registered_fields contract was deleted by a dashboard save").toEqual(
    REGISTERED,
  );
});
