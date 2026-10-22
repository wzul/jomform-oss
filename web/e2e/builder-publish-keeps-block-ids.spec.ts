// A builder Publish must not rename the blocks the stored
// `config.registered_fields` contract points at (card t_cbeafc7c).
//
// THE DEFECT, measured on prod v1.21.279 / 4b8fa0dc by card t_bf407d24 with a
// throwaway response form in `test-wan`:
//
//   stored:  blocks [{id:"b1",type:"text"}, {id:"b2",type:"text",value:"Nama",required:true}]
//            registered_fields [{field_id:"b2", type:"text", label:"Nama", required:true}]
//   action:  /builder?form=<id> -> Publish. The PATCH body carried
//            registered_fields VERBATIM (card t_59bc3055's fix) but wrote the
//            blocks back as `265kivw`/`db8x0qh`, because hydrateBlock assigned
//            `id: uid()` on every load.
//   result:  the public page rendered `input[name="db8x0qh"]`, its own submit
//            sent `{265kivw:"", db8x0qh:"QA submit probe"}`, and POST /order
//            answered 400 "medan wajib tidak diisi: Nama" — for every visitor,
//            on a form that accepted submits before the save. formfields.Contract
//            prefers the contract over the blocks (api/internal/formfields), so
//            the contract still required a field no control emits.
//
// WHAT THIS SPEC DRIVES. The real built builder page, with the API stubbed at
// the browser level (no Go server, no session, no DB — the same harness shape as
// `builder-save-keeps-custom-html.spec.ts`), then reads the config the Publish
// actually PUT into the PATCH body:
//
//   1. every stored block id comes back unchanged — that is what keeps the
//      contract's `field_id` referring to a control the public page emits;
//   2. every `registered_fields[].field_id` names a block the page wrote, and
//      the block it names still carries the contract's own `required` marker —
//      the contract↔rendered-name AGREEMENT the card asks for, not merely
//      "registered_fields survived";
//   3. a stored config that cannot give a usable id (missing / duplicate) still
//      saves a unique id per block, so a load never leaves two controls with one
//      name (which would collapse their submitted values).
//
// The server half — that the page the Go renderer serves carries a control named
// by each contract field_id, and that such a submit is accepted while the
// renamed-blocks config is refused — is
// api/internal/httpapi/builder_publish_block_ids_test.go.
//
// BITES ON THE PRE-FIX BUILD: arm 1 requires the PATCH block ids to equal the
// stored ones, which the pre-fix builder never sent.
import { test, expect, type Page } from "@playwright/test";

import { signIn } from "./auth";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page
// renders. This spec visits one, so it declares the session it means — a stub
// value, since the gate reads presence only and this spec stubs the API itself.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

const FORM_ID = "form_block_ids_preserve";
const PUBLIC_URL = "https://demo.jomform.com/block-ids-preserve";

// The submit contract an agent registers on a BLOCK form
// (MCP register_form_fields → config.registered_fields). Its field_id IS a block
// id — formfields.Contract returns this list instead of deriving one from the
// blocks, and the response submit validates against it.
const REGISTERED = [{ field_id: "b2", type: "text", label: "Nama", required: true }];

/** A response-mode block form: one prose/optional text field and the required one. */
const BLOCKS = [
  { id: "b1", type: "text", value: "Salam" },
  { id: "b2", type: "text", value: "Nama", required: true },
];

/** The form as hydration reads it. `config` is the only part under test. */
const formWith = (config: Record<string, unknown>) => ({
  id: FORM_ID,
  slug: "block-ids-preserve",
  title: "Block ids preserve",
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
      return r.fulfill(json(200, { status: "updated", slug: "block-ids-preserve" }));
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

/** The config the last recorded PATCH actually carried. */
async function publishAndRead(page: Page, patches: Patch[]): Promise<Record<string, unknown>> {
  await page.getByRole("button", { name: "Publish", exact: true }).first().click();

  // The save's own signal — proof the click reached the write, so a missing
  // PATCH below cannot read as "preserved".
  const live = page.getByRole("status");
  await expect(live).toBeVisible({ timeout: 20_000 });
  expect(await live.innerText()).toContain("Your form is live");

  expect(patches.length, "the builder never wrote the form's config").toBeGreaterThan(0);
  const body = JSON.parse(patches[patches.length - 1].body) as { config?: Record<string, unknown> };
  return body.config ?? {};
}

test("Publish keeps the stored block ids, so registered_fields still names a rendered field", async ({ page }) => {
  test.setTimeout(120_000);
  const patches = await openBuilder(page, { mode: "response", blocks: BLOCKS, registered_fields: REGISTERED });
  const sent = await publishAndRead(page, patches);

  const blocks = (sent.blocks ?? []) as { id?: string; value?: string; required?: boolean }[];
  const ids = blocks.map((b) => b.id);
  const storedIds = BLOCKS.map((b) => b.id);

  // THE ASSERTION THE PRE-FIX BUILDER FAILS. These ids are what the public page
  // emits as `name="…"` and what its submit sends — a fresh id here means the
  // stored contract below points at nothing.
  expect(ids, "a builder save renamed the stored blocks, so the contract's field_ids stop existing").toEqual(storedIds);
  expect(new Set(ids).size, "two blocks were saved with the same id").toBe(ids.length);
  // The blocks are still the merchant's blocks (this is a save, not a no-op).
  expect(blocks.map((b) => b.value)).toEqual(["Salam", "Nama"]);

  // THE AGREEMENT THE CARD ASKS FOR: every registered field names a block the
  // save wrote, and that block still carries the contract's own marker. A
  // "registered_fields survived" assertion alone passes while this is broken —
  // that is precisely how the defect shipped.
  const contract = (sent.registered_fields ?? []) as { field_id?: string; required?: boolean }[];
  expect(contract.length, "the registered_fields contract was dropped by the save").toBe(REGISTERED.length);
  for (const field of contract) {
    const block = blocks.find((b) => b.id === field.field_id);
    expect(
      block,
      `registered_fields requires field_id "${field.field_id}" but no saved block carries that id — the public page emits no such control`,
    ).toBeTruthy();
    expect(block!.required, `the contract's required marker is not on the block it names (${field.field_id})`).toBe(
      Boolean(field.required),
    );
  }
});

test("a stored config whose blocks cannot supply usable ids still saves one unique id per block", async ({ page }) => {
  test.setTimeout(120_000);
  // A legacy / hand-written config: one stored id, one block with none, one
  // DUPLICATE. Two controls sharing a name collapse into a single submitted
  // value (and collide as React keys), so the load has to repair these — while
  // keeping what it can.
  const stored = [
    { id: "keepme", type: "text", value: "Satu" },
    { type: "text", value: "Tiada id" },
    { id: "keepme", type: "text", value: "Pendua" },
  ];
  const patches = await openBuilder(page, { mode: "response", blocks: stored });
  const sent = await publishAndRead(page, patches);

  const ids = ((sent.blocks ?? []) as { id?: string }[]).map((b) => b.id);
  expect(ids.length, "a block was lost on load").toBe(3);
  expect(ids[0], "a usable stored id was replaced").toBe("keepme");
  expect(new Set(ids).size, "the save wrote two blocks with one id — their answers would collapse").toBe(3);
  expect(ids.every((id) => typeof id === "string" && id.length > 0), "a block was saved without an id").toBe(true);
});
