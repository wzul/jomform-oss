// A form whose stored `condition.field_id` ALREADY dangles must show that rule
// as broken — not as "— Always show —" (card t_a81a9165).
//
// THE DEFECT, measured by t_0e2d3d24 on a production build (`next build` +
// `next start`, real browser, the form GET stubbed and the PATCH captured) with
// a stored config of:
//
//   {"mode":"payment","blocks":[
//     {"id":"tx1","type":"text","value":"Syarikat"},
//     {"id":"ph1","type":"phone","value":"Telefon",
//      "condition":{"field_id":"gone","op":"equals","value":"x"}}]}
//
// Opening `/builder?form=<id>` and clicking the phone block gave a rule picker
// whose `value` was "" and whose options were exactly
// ["— Always show —","Syarikat (#1)"] — i.e. the block read as ALWAYS SHOWN.
// One click on Publish then wrote the broken rule back byte-identically.
//
// WHY THAT IS NOT A DISPLAY NIT. `api/internal/formfields` Condition.Met reads
// `answers["gone"]`, gets "", evaluates the rule FALSE, and VisibleAnswers
// DELETES that block's answer and skips it in validation — so the phone field is
// hidden on the public page forever and every answer to it is thrown away, while
// the merchant is told the opposite and has no control to repair it.
//
// THIS SPEC pins the repair as decided on the card: KEEP the stored rule (never
// drop it on load — that would silently re-enable a field the merchant may have
// hidden on purpose) and SURFACE it, on both surfaces, with the two honest exits
// the merchant can take:
//
//   1. the canvas card carries a "rule broken" label, and the panel lists the
//      unknown id as "Not available" with a notice naming the id and the LIVE
//      consequence (`role="alert"`, so it is announced);
//   2. Publish writes the rule back unchanged while the merchant has not edited
//      it — nothing is rewritten behind their back;
//   3. choosing "— Always show —" clears the rule and the save then carries no
//      condition for that block — the merchant CAN repair it, which the old
//      picker made impossible;
//   4. a one-block form still surfaces a stored rule (the section used to open
//      only when `blocks.length > 1`, so the notice would have been unreachable);
//   5. a rule whose target is no longer an ANSWER FIELD (a `text` field retyped
//      to an intro `section`, which keeps its id) is flagged the same way,
//      because the picker never offers a section and the server's answers map
//      never carries one.
//
// The panel state with the notice is also scanned with the gate's own axe rule
// (`gateScan`/`blockersOf`/`expectGateSeesAriaViolation` from e2e/axeGate.ts),
// with the planted A-level control, so the new markup cannot ship a
// critical/serious violation that the plain page scan never renders.
import { test, expect, type Page } from "@playwright/test";

import { signIn } from "./auth";
import { blockersOf, expectGateSeesAriaViolation, gateScan } from "./axeGate";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page
// renders. This spec visits one, so it declares the session it means — a stub
// value, since the gate reads presence only and this spec stubs the API itself.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

const FORM_ID = "form_condition_dangling_target";
const SLUG = "condition-dangling-target";

// The card's fixture, verbatim: a text block, and a phone block whose rule
// names `gone`, an id no block carries.
const BLOCKS = [
  { id: "tx1", type: "text", value: "Syarikat" },
  {
    id: "ph1",
    type: "phone",
    value: "Telefon",
    condition: { field_id: "gone", op: "equals", value: "x" },
  },
];

/** The form as hydration reads it. `config` is the only part under test. */
const formWith = (config: Record<string, unknown>) => ({
  id: FORM_ID,
  slug: SLUG,
  title: "Condition dangling target",
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
    localStorage.clear();
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

/** The settings panel is the SECOND <aside> in the builder's grid (the first is the palette). */
const PANEL = "aside:last-of-type";

const rulePicker = (page: Page, blockId: string) =>
  page.locator(`select[aria-labelledby="jf-cond-${blockId}"]`).first();

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

test("a stored rule naming a missing field is shown as broken, not as Always show", async ({ page }) => {
  test.setTimeout(120_000);
  const { patches } = await openBuilder(page, { mode: "payment", blocks: BLOCKS });
  await expect(blockCards(page)).toHaveCount(2);

  // 1. THE CANVAS. The broken rule is flagged on the block's card, so it is not
  // invisible until someone happens to open that one block.
  const badge = page.getByText("rule broken", { exact: true });
  await expect(badge, "the canvas card does not flag the broken rule").toBeVisible();
  await expect(badge, "the flag is on the wrong block").toHaveCount(1);
  await expect(blockCards(page).nth(1)).toContainText("rule broken");
  await expect(blockCards(page).nth(0)).not.toContainText("rule broken");

  // 2. THE PANEL. Open the phone block and read the picker the way the card did.
  await blockCards(page).nth(1).click();
  await expect(page.locator("#jf-blk-ph1-description")).toBeVisible();

  const picker = rulePicker(page, "ph1");
  // THE ASSERTION THE PRE-FIX BUILDER FAILS: it reported "" here, which is the
  // "— Always show —" option, i.e. the opposite of the stored rule.
  await expect(picker, "the picker still reads as Always show for a rule that cannot match").toHaveValue("gone");

  // The option list names the unknown id, so the merchant can see what broke.
  const options = await picker.locator("option").evaluateAll((els) => els.map((e) => (e as HTMLOptionElement).value));
  expect(options, "the picker does not carry the stored-but-unavailable id").toEqual(["", "gone", "tx1"]);
  const labels = await picker.locator("option").allInnerTexts();
  expect(labels[1], "the unavailable option does not name the missing id").toContain("gone");
  expect(labels[1].toLowerCase(), "the unavailable option reads as a normal choice").toContain("not available");

  // 3. THE NOTICE. `role="alert"` so it is announced, and it states the live
  // consequence (hidden on the page, answers discarded) plus both exits.
  const panel = page.locator(PANEL);
  const notice = panel.getByRole("alert").filter({ hasText: "gone" });
  await expect(notice, "the panel does not explain the broken rule").toBeVisible();
  const copy = (await notice.innerText()).trim();
  expect(copy, "the notice does not name the missing field id").toContain("gone");
  expect(copy.toLowerCase(), "the notice does not say the field is hidden on the live form").toContain("hidden");
  expect(copy.toLowerCase(), "the notice does not say answers are discarded").toContain("discarded");
  expect(copy, "the notice does not offer the repair").toContain("Always show");

  // 4. AXE, on the panel state the page scan never renders, with the control
  // that proves the scan could see a defect if one were there.
  const results = await gateScan(page, PANEL);
  expect(
    blockersOf(results).map((v) => `${v.id} (${v.impact}) on ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`),
    "the builder panel with the broken-rule notice carries a critical/serious axe violation",
  ).toEqual([]);
  expect(
    results.passes.length + results.incomplete.length + results.violations.length,
    "the scan evaluated nothing, so the clean reading above is not evidence",
  ).toBeGreaterThan(0);
  await expectGateSeesAriaViolation(page, PANEL, "the builder panel with a broken conditional rule");

  // 5. PUBLISH. The rule the merchant did NOT edit is written back as it was:
  // the builder repairs nothing behind their back, it only shows the truth.
  const sent = await publishAndRead(page, patches);
  const blocks = (sent.blocks ?? []) as SavedBlock[];
  const phone = blocks.find((b) => b.type === "phone");
  expect(phone, "the block carrying the broken rule was lost by the save").toBeTruthy();
  expect(phone!.condition, "the builder silently dropped or rewrote a rule the merchant did not edit").toEqual({
    field_id: "gone",
    op: "equals",
    value: "x",
  });
});

test("choosing Always show clears the broken rule, and the save carries no condition", async ({ page }) => {
  test.setTimeout(120_000);
  const { patches } = await openBuilder(page, { mode: "payment", blocks: BLOCKS });
  await expect(blockCards(page)).toHaveCount(2);
  await blockCards(page).nth(1).click();

  // THE REPAIR THE OLD PICKER COULD NOT MAKE. Before this change the select had
  // no option carrying `gone`, so the browser showed "" — selecting "" again
  // fired no change event at all and the broken rule could never be removed.
  const picker = rulePicker(page, "ph1");
  await expect(picker).toHaveValue("gone");
  await picker.selectOption("");
  await expect(picker, "choosing Always show did not clear the rule").toHaveValue("");

  const panel = page.locator(PANEL);
  await expect(panel.getByRole("alert").filter({ hasText: "gone" }), "the notice survived the repair").toHaveCount(0);
  await expect(page.getByText("rule broken", { exact: true }), "the canvas flag survived the repair").toHaveCount(0);

  const sent = await publishAndRead(page, patches);
  const blocks = (sent.blocks ?? []) as SavedBlock[];
  const phone = blocks.find((b) => b.type === "phone");
  expect(phone, "the block was lost by the save").toBeTruthy();
  expect(phone!.condition, "the cleared rule was still saved").toBeFalsy();
});

test("a one-block form still surfaces a stored rule whose target is gone", async ({ page }) => {
  test.setTimeout(120_000);
  // The conditional-logic section used to open only when `blocks.length > 1`,
  // i.e. a form whose rule names a block that is not there had no UI at all.
  await openBuilder(page, {
    mode: "response",
    blocks: [{ id: "ph1", type: "phone", value: "Telefon", condition: { field_id: "gone", op: "equals", value: "x" } }],
  });
  await expect(blockCards(page)).toHaveCount(1);
  await expect(page.getByText("rule broken", { exact: true })).toBeVisible();

  await blockCards(page).first().click();
  await expect(rulePicker(page, "ph1"), "a one-block form still hides its stored rule").toHaveValue("gone");
  await expect(page.locator(PANEL).getByRole("alert").filter({ hasText: "gone" })).toBeVisible();
});

test("a rule whose target is no longer an answer field is flagged too", async ({ page }) => {
  test.setTimeout(120_000);
  // Reachable entirely through the builder: "Treat as intro line" retypes a
  // `text` FIELD to an intro `section` and keeps its id, so a rule written
  // against it names a block that still exists but that neither the picker nor
  // the server's answers map can use as a source.
  const { patches } = await openBuilder(page, {
    mode: "response",
    blocks: [
      { id: "tx1", type: "section", value: "Pengenalan" },
      { id: "ph1", type: "phone", value: "Telefon", condition: { field_id: "tx1", op: "equals", value: "x" } },
    ],
  });
  await expect(blockCards(page)).toHaveCount(2);
  await expect(blockCards(page).nth(1)).toContainText("rule broken");

  await blockCards(page).nth(1).click();
  const picker = rulePicker(page, "ph1");
  await expect(picker, "a rule naming a display block was treated as a live target").toHaveValue("tx1");
  const options = await picker.locator("option").evaluateAll((els) => els.map((e) => (e as HTMLOptionElement).value));
  expect(options, "the picker offered the section as a condition source").toEqual(["", "tx1"]);
  await expect(page.locator(PANEL).getByRole("alert").filter({ hasText: "tx1" })).toBeVisible();

  const sent = await publishAndRead(page, patches);
  const blocks = (sent.blocks ?? []) as SavedBlock[];
  expect(blocks.find((b) => b.type === "phone")!.condition, "the unedited rule was rewritten").toEqual({
    field_id: "tx1",
    op: "equals",
    value: "x",
  });
});
