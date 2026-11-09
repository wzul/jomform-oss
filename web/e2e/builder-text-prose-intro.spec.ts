// Regression guard: a legacy `text` block whose value is a SENTENCE (card
// t_4823943a).
//
// THE DEFECT. A `text` block is a single-line answer INPUT: both public
// renderers paint
//
//   <label class="lbl">Register for our event — cuma rsvp ja</label>
//   <input type="text" name="qoubfpj" data-id="qoubfpj">
//
// so the block's `value` is the field's LABEL. Forms written under the old
// content model stored their intro SENTENCE there; measured read-only on prod
// 2026-10-20, ten merchant rows (`NOT is_template AND deleted_at IS NULL`) still
// do, five of them published. The merchant sees a form whose opening line is
// nailed to an empty box.
//
// WHY THE BUILDER CANNOT FIX IT FOR THEM. `text` is a legitimate label too —
// `Abc` and `Nama anda` are stored on published forms and are real fields — and
// only the merchant knows which of the two a given row is. So the builder WARNS
// where the block is edited and offers an explicit action, plus its exact
// inverse. That is what this spec drives, on the real built page with the API
// stubbed at the browser level (no Go server, no session, no DB — the same
// harness as `builder-publish-keeps-block-ids.spec.ts`):
//
//   1. DETECT: a prose value shows the warning and the action in the panel; the
//      canvas still paints the field (label + control-shaped box), so the two
//      readings cannot be confused.
//   2. CONVERT: the action retypes the block to `section`, and the config the
//      Publish actually PUT into the PATCH body carries the SAME id and the SAME
//      value bytes with `type:"section"` — asserted on the wire body, not on a
//      toast. The canvas then paints the prose line instead of the input box.
//   3. INVERSE: the same panel offers Undo, and convert-then-undo publishes the
//      ORIGINAL stored block, byte for byte (JSON-compared).
//   4. NO FALSE POSITIVE: a short label (`Abc`) never shows the warning, so the
//      builder does not train the merchant to dismiss it.
//
// THE RENDERED-FORM HALF is pinned at the renderer itself, where the label was
// measured: `api/internal/httpapi/response_text_block_render_test.go` renders
// the converted config through the real response handler and requires the
// sentence as `<div class="section">…</div>` with no `<label class="lbl">`.
// A browser spec cannot reach that page: the public form is served by the Go
// API, not by the Next app this suite starts.
import { test, expect, type Page } from "@playwright/test";

import { signIn } from "./auth";

test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

const FORM_ID = "form_text_prose_intro_guard";
const PUBLIC_URL = "https://demo.jomform.com/text-prose-intro";

// The block the card measured on `event-registration`, plus one real label field
// so the panel is not the only block on the canvas. Block 0 carries two FIELD
// SETTINGS on purpose: the inverse arm compares the published block to this
// object byte for byte, so a conversion (or an undo) that rebuilds the block and
// drops a setting fails there. Their key order is the order the builder's own
// save emits them in (id, type, value, placeholder, …, max_length).
const PROSE = "Register for our event — cuma rsvp ja";
const BLOCKS = [
  { id: "qoubfpj", type: "text", value: PROSE, placeholder: "Nama", max_length: 120 },
  { id: "nm", type: "name", value: "Nama anda" },
];

// A legitimately short label: the card names `Abc` as one of the ten prod rows
// that must NOT be converted.
const SHORT_LABEL_BLOCKS = [{ id: "nm", type: "text", value: "Abc" }];

const WARN_COPY = "This reads like an introduction";
const ACTION = "Treat as intro line";
const UNDO = "Undo. Make it a field again";

type Patch = { method: string; path: string; body: string };

const formWith = (config: Record<string, unknown>) => ({
  id: FORM_ID,
  slug: "text-prose-intro",
  title: "Text prose intro",
  status: "draft",
  url: "",
  config,
});

/**
 * stubAPI answers every /api/* call the builder makes, records the config write
 * (`PATCH /api/rest/forms/{id}`) and lets the publish flow complete.
 */
async function stubAPI(page: Page, blocks: { id: string; type: string; value: string }[]) {
  const json = (status: number, body: unknown) => ({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  const patches: Patch[] = [];
  let published = false;
  const config = () => ({ mode: "response", blocks });
  const formNow = () => ({
    ...formWith(config()),
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
  await page.route(new RegExp(`(.*)/api/rest/forms/${FORM_ID}(\\?.*)?$`), (r) => {
    const req = r.request();
    if (req.method() === "PATCH" || req.method() === "PUT") {
      patches.push({ method: req.method(), path: new URL(req.url()).pathname, body: req.postData() || "" });
      return r.fulfill(json(200, { status: "updated", slug: "text-prose-intro" }));
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
async function openBuilder(page: Page, blocks: { id: string; type: string; value: string }[]) {
  await page.addInitScript(() => {
    localStorage.clear();
    localStorage.setItem("jf_workspace", "demo");
    localStorage.setItem("jf_lang", "en");
  });
  const patches = await stubAPI(page, blocks);
  await page.goto(`/builder?form=${FORM_ID}`, { waitUntil: "networkidle" });
  // The header's Publish button only reads "Publish" once the form hydrated.
  await expect(page.getByRole("button", { name: "Publish", exact: true }).first()).toBeVisible({ timeout: 45_000 });
  return patches;
}

/** Opens the settings panel for the block at `index` with ONE plain click. */
async function selectBlock(page: Page, index: number) {
  const cards = page.locator("section div[draggable]");
  await expect(cards.first()).toBeVisible({ timeout: 25_000 });
  await cards.nth(index).click();
  await expect(page.locator("aside").last().locator("#jf-block-value")).toBeVisible();
}

/**
 * cardShape reads what the canvas card at `index` actually PAINTS: the text it
 * shows, whether it carries the input-shaped skeleton a field gets, and whether
 * it carries the prose line a section gets. Read off the live DOM, not inferred
 * from the block's type — a preview that stopped following the type is exactly
 * the kind of silent drift this card is about.
 */
async function cardShape(page: Page, index: number): Promise<{ text: string; inputBox: boolean; proseLine: boolean }> {
  return page.evaluate((i) => {
    const card = document.querySelectorAll("section div[draggable]")[i] as HTMLElement | undefined;
    if (!card) return { text: "", inputBox: false, proseLine: false };
    return {
      text: card.innerText,
      // BlockPreview's `text` arm paints `div.flex.h-8` (the control sketch)…
      inputBox: !!card.querySelector("div.flex.h-8"),
      // …and its `section` arm paints the value in a semibold paragraph.
      proseLine: !!card.querySelector("p.font-semibold"),
    };
  }, index);
}

/** The config the last recorded PATCH actually carried. */
async function publishAndRead(page: Page, patches: Patch[]): Promise<Record<string, unknown>> {
  await page.getByRole("button", { name: "Publish", exact: true }).first().click();

  // The save's own signal — and scoped by its own text, because the converted
  // block's panel carries a `role="status"` of its own.
  const live = page.getByRole("status").filter({ hasText: "Your form is live" });
  await expect(live).toBeVisible({ timeout: 20_000 });

  expect(patches.length, "the builder never wrote the form's config").toBeGreaterThan(0);
  const body = JSON.parse(patches[patches.length - 1].body) as { config?: Record<string, unknown> };
  return body.config ?? {};
}

test("a prose value warns where the block is edited, and the action saves it as an intro with the same id and value", async ({ page }) => {
  test.setTimeout(120_000);
  const patches = await openBuilder(page, BLOCKS);
  await selectBlock(page, 0);

  const panel = page.locator("aside").last();

  // ARM 1 — DETECT. The warning is announced (`role="alert"`) and names the
  // consequence, and the action exists as a real button with an accessible name.
  const warning = panel.getByRole("alert");
  await expect(warning, "a prose value in a text block showed no warning").toBeVisible();
  expect(await warning.innerText()).toContain(WARN_COPY);
  await expect(warning.getByRole("button", { name: ACTION })).toBeVisible();

  // The canvas still paints a FIELD before the conversion (label + control
  // sketch) — otherwise the "label is gone" reading below could pass on a card
  // that never drew one.
  const before = await cardShape(page, 0);
  expect(before.text).toContain(PROSE);
  expect(before.inputBox, `the canvas did not paint a field before the conversion: ${JSON.stringify(before)}`).toBe(true);
  expect(before.proseLine, `the card already painted prose before the conversion: ${JSON.stringify(before)}`).toBe(false);

  // ARM 2 — CONVERT. One explicit click.
  await warning.getByRole("button", { name: ACTION }).click();

  // The warning is gone (the value is no longer a text block's label) and the
  // inverse is offered in its place.
  await expect(panel.getByRole("alert")).toHaveCount(0);
  const status = panel.getByRole("status");
  await expect(status, "the conversion offered no undo").toBeVisible();
  expect(await status.innerText()).toContain("Converted");
  await expect(status.getByRole("button", { name: UNDO })).toBeVisible();

  // The canvas now paints PROSE: no control-shaped box, and the merchant's
  // sentence is still on the card, unedited.
  const after = await cardShape(page, 0);
  expect(after.text, "the merchant's sentence disappeared from the card").toContain(PROSE);
  expect(after.inputBox, `the card still paints an input box after the conversion: ${JSON.stringify(after)}`).toBe(false);
  expect(after.proseLine, `the card does not paint the intro line: ${JSON.stringify(after)}`).toBe(true);

  // THE SAVED CONFIG, read off the wire. This is the assertion a toast cannot
  // satisfy: the block the Publish writes carries the SAME id and the SAME value
  // bytes, and only its type moved.
  const sent = await publishAndRead(page, patches);
  const blocks = (sent.blocks ?? []) as { id?: string; type?: string; value?: string }[];
  const converted = blocks.find((b) => b.id === "qoubfpj");
  expect(converted, `the saved config no longer carries the block: ${JSON.stringify(blocks)}`).toBeTruthy();
  expect(converted!.type, "the saved block was not converted to a section").toBe("section");
  expect(converted!.value, "the conversion edited the merchant's sentence").toBe(PROSE);
  // The sibling field is untouched.
  expect(blocks.find((b) => b.id === "nm")!.type).toBe("name");
});

test("convert then undo publishes the original stored block, byte for byte", async ({ page }) => {
  test.setTimeout(120_000);
  const patches = await openBuilder(page, BLOCKS);
  await selectBlock(page, 0);
  const panel = page.locator("aside").last();

  await panel.getByRole("alert").getByRole("button", { name: ACTION }).click();
  await expect(panel.getByRole("status").getByRole("button", { name: UNDO })).toBeVisible();

  // ARM 3 — THE EXACT INVERSE, before anything is written.
  await panel.getByRole("status").getByRole("button", { name: UNDO }).click();

  // Back to a field: the warning is back (the value is prose in a text block
  // again) and so is the control sketch on the canvas.
  await expect(panel.getByRole("alert")).toBeVisible();
  const shape = await cardShape(page, 0);
  expect(shape.text).toContain(PROSE);
  expect(shape.inputBox, `the canvas did not go back to a field: ${JSON.stringify(shape)}`).toBe(true);

  const sent = await publishAndRead(page, patches);
  const blocks = (sent.blocks ?? []) as Record<string, unknown>[];
  // Byte-for-byte against the STORED block: same keys, same order, same values.
  // A conversion that dropped a setting (or an inverse that rebuilt the block
  // from scratch) fails here.
  expect(
    JSON.stringify(blocks.find((b) => b.id === "qoubfpj")),
    "convert-then-undo did not restore the stored block byte for byte",
  ).toBe(JSON.stringify(BLOCKS[0]));
});

test("a short label never shows the warning", async ({ page }) => {
  test.setTimeout(120_000);
  await openBuilder(page, SHORT_LABEL_BLOCKS);
  await selectBlock(page, 0);
  const panel = page.locator("aside").last();

  // The panel really is the text block's panel (a control that existed before
  // this card is there), so "no alert" cannot mean "no panel".
  await expect(panel.locator("#jf-block-value")).toHaveValue("Abc");
  await expect(
    panel.getByRole("alert"),
    "a one-word label was flagged as prose — the warning would train the merchant to ignore it",
  ).toHaveCount(0);

  // And the canvas still paints the field the merchant meant.
  const shape = await cardShape(page, 0);
  expect(shape.text).toContain("Abc");
  expect(shape.inputBox, `the canvas stopped painting the field: ${JSON.stringify(shape)}`).toBe(true);
});
