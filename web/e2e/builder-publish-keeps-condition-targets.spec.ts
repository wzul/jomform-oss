// A builder load+save must leave a stored `condition.field_id` pointing at a
// block that still exists after the save (card t_0e2d3d24).
//
// THE DEFECT, measured by card t_0e2d3d24 through the real builder on a
// production build of main@727af750 (`next build`, `next start`, real browser,
// the form GET stubbed and the PATCH captured). The stored config was:
//
//   blocks: [{id:"tx1",type:"text",value:"Syarikat"},
//            {id:"em1",type:"email",value:"Emel",required:true},
//            {id:"ph1",type:"phone",value:"Telefon",
//             condition:{field_id:"tx1",op:"equals",value:"x"}}]
//
// and one click on Publish wrote it back as `1g9m251`/`enkni5x`/`2hrovlk` while
// the rule still named `tx1` — every block renamed, the rule's target gone.
// The same regeneration was visible in the panel's DOM ids, which are built
// from the block id (`jf-blk-<block>-<key>`).
//
// WHY THAT IS NOT COSMETIC. A block id is the field name the public page emits
// (`name="<id>"`) and the key a submission stores (`{fields:{<id>: value}}`), so
// renaming one silently re-keys the merchant's answers (CSV export, webhooks,
// MCP readers, the merchant's own integration). And a dangling
// `condition.field_id` is worse than a broken reference: formfields.Condition.Met
// reads `answers[c.FieldID]`, gets "" for an id nothing carries, evaluates the
// rule as FALSE, and VisibleAnswers then DELETES that block's answer and skips
// it in validation — after one builder save a conditional field is permanently
// hidden and its answers are thrown away.
//
// The load path now keeps each stored id (lib/blockHydrate, pinned by
// `builder-publish-keeps-block-ids.spec.ts` for the registered_fields contract).
// THIS spec pins the two consequences that contract does not cover:
//
//   1. every `condition.field_id` in the saved config still names a block in
//      that same config, and the rule the merchant wrote survives the save;
//   2. duplicating a block mints a NEW id for the copy — the repair that keeps
//      (1) true has to stay per-block, not a "reuse whatever was there" rule —
//      while the original keeps its id and the rule that names it still resolves.
//
// The builder is driven as a real built page with the API stubbed at the browser
// level (same harness shape as `builder-save-keeps-custom-html.spec.ts`), and the
// assertion runs against the config the Publish actually put in the PATCH body.
//
// BITES ON THE PRE-FIX BUILD: arm 1 requires the saved ids to EQUAL the stored
// ones; the pre-fix `hydrateBlock` (`id: uid()` on every load) sent three random
// ids instead, which is the exact payload the card recorded.
import { test, expect, type Page } from "@playwright/test";

import { signIn } from "./auth";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page
// renders. This spec visits one, so it declares the session it means — a stub
// value, since the gate reads presence only and this spec stubs the API itself.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

const FORM_ID = "form_condition_targets_preserve";
const SLUG = "condition-targets-preserve";

// The card's fixture, verbatim: a text block, an email block, and a phone block
// whose rule fires when the text block equals "x".
const BLOCKS = [
  { id: "tx1", type: "text", value: "Syarikat" },
  { id: "em1", type: "email", value: "Emel", required: true },
  {
    id: "ph1",
    type: "phone",
    value: "Telefon",
    condition: { field_id: "tx1", op: "equals", value: "x" },
  },
];

const STORED_IDS = BLOCKS.map((b) => b.id);

/** The form as hydration reads it. `config` is the only part under test. */
const formWith = (config: Record<string, unknown>) => ({
  id: FORM_ID,
  slug: SLUG,
  title: "Condition targets preserve",
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
  const publicUrl = `https://demo.jomform.com/${SLUG}`;
  const formNow = () => ({
    ...formWith(config),
    status: published ? "published" : "draft",
    url: published ? publicUrl : "",
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
      return r.fulfill(json(200, { status: "updated", slug: SLUG }));
    }
    return r.fulfill(json(200, formNow()));
  });
  await page.route(new RegExp(`(.*)/api/rest/forms/${FORM_ID}/status(\\?.*)?$`), (r) => {
    published = true;
    return r.fulfill(json(200, { status: "published" }));
  });
  await page.route(new RegExp(`(.*)/api/rest/forms/${FORM_ID}/publish(\\?.*)?$`), (r) => {
    published = true;
    return r.fulfill(json(200, { status: "published", url: publicUrl }));
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
  return { patches };
}

/** The canvas block cards, in form order. Each card is a draggable div that owns one block menu. */
const blockCards = (page: Page) => page.locator('div[draggable="true"]:has([data-block-menu])');

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

type SavedBlock = {
  id?: string;
  type?: string;
  value?: string;
  condition?: { field_id?: string; op?: string; value?: string } | null;
};

test("a stored conditional rule still names a block that exists after Publish", async ({ page }) => {
  test.setTimeout(120_000);
  const { patches } = await openBuilder(page, { mode: "payment", blocks: BLOCKS });

  // The form hydrated with the three stored blocks (not a blank default form).
  await expect(blockCards(page)).toHaveCount(3);

  const sent = await publishAndRead(page, patches);
  const blocks = (sent.blocks ?? []) as SavedBlock[];
  const ids = blocks.map((b) => b.id);

  // THE ASSERTION THE PRE-FIX BUILDER FAILS — and the one the card measured.
  // Every stored id comes back unchanged: the ids the public page emits as
  // `name="…"` and a submission stores as its answer keys.
  expect(ids, "a builder save renamed the stored blocks, so every answer key changed").toEqual(STORED_IDS);

  // The rule the merchant wrote is still the rule that was saved.
  const phone = blocks.find((b) => b.type === "phone");
  expect(phone, "the block carrying the conditional rule was lost by the save").toBeTruthy();
  expect(phone!.condition, "the conditional rule was dropped by the save").toEqual({
    field_id: "tx1",
    op: "equals",
    value: "x",
  });

  // THE DEFECT ITSELF, stated as a resolution rather than an equality: every
  // rule in the saved config points at a block the same config carries. A
  // dangling id makes formfields.Condition.Met read "", the rule is FALSE, and
  // VisibleAnswers deletes the answer of the field the rule guards.
  for (const b of blocks) {
    if (!b.condition?.field_id) continue;
    expect(
      ids,
      `block "${b.id}" is shown only while "${b.condition.field_id}" equals "${b.condition.value}", but no saved block carries that id — the rule is permanently FALSE and its answers are discarded`,
    ).toContain(b.condition.field_id);
  }

  // The card's DOM observation, read AFTER the save: the per-block panel ids are
  // built from the block id (`jf-blk-<block>-<key>`), so opening the phone
  // block's panel must show `ph1` — not the random id a re-minting load produced
  // (`jf-blk-07ravrs-description` in the card) — and its rule picker must still
  // read `tx1`.
  await blockCards(page).nth(2).click();
  await expect(page.locator("#jf-blk-ph1-description")).toBeVisible();
  await expect(page.locator('select[aria-labelledby="jf-cond-ph1"]').first()).toHaveValue("tx1");
});

test("duplicating a block mints a NEW id for the copy and leaves the rule's target alone", async ({ page }) => {
  test.setTimeout(120_000);
  const { patches } = await openBuilder(page, { mode: "payment", blocks: BLOCKS });
  await expect(blockCards(page)).toHaveCount(3);

  // Duplicate the FIRST block — the one the phone block's rule names. The copy is
  // a new control, so it must NOT inherit `tx1`: two controls with one name
  // collapse into a single submitted value, and the rule would then fire off
  // whichever of them the renderer happened to emit.
  await page.locator('[data-block-menu] button[aria-haspopup="true"]').first().click();
  await page.getByRole("button", { name: "Duplicate", exact: true }).click();
  await expect(blockCards(page)).toHaveCount(4);

  const sent = await publishAndRead(page, patches);
  const blocks = (sent.blocks ?? []) as SavedBlock[];
  const ids = blocks.map((b) => b.id);

  expect(ids.length, "a block was lost on the way to the save").toBe(4);
  expect(ids[0], "the duplicated block's ORIGINAL lost its stored id").toBe("tx1");
  expect(ids[1], "the copy kept the original's id — two controls would share one name").not.toBe("tx1");
  expect(new Set(ids).size, "two saved blocks share an id").toBe(ids.length);
  // The copy is still the block the merchant duplicated, not an empty one.
  expect(blocks[1].value).toBe("Syarikat");

  // The rule still resolves — to the ORIGINAL, which is the id it was written against.
  const phone = blocks.find((b) => b.type === "phone") as SavedBlock;
  expect(phone.condition?.field_id, "duplicating a block re-pointed the rule that names it").toBe("tx1");
  expect(ids, "the rule's target does not exist after the save").toContain(phone.condition!.field_id!);
});
