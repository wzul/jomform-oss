// Regression guard: a `text` block's builder panel offers the field settings
// its renderer already honours — Required, Required message and Validation
// pattern.
//
// THE DEFECT (card t_28e6658d, found while verifying v1.21.138). Both public
// renderers serve a `text` block as a real `<input type="text">` that honours
// `required`, `required_message` and `pattern`:
//
//   <input type="text" name="qa38t1" data-id="qa38t1" class="qa38-wide"
//          placeholder="QA38 placeholder" maxlength="20" value="QA38"
//          pattern="[A-Za-z ]{1,20}">
//
// but the builder's `fieldTypes` list left `text` out, and the panel gates its
// Required toggle (and therefore the Required-message and Validation-pattern
// inputs) on `fieldTypes.includes(b.type)`. Measured on prod with a session,
// the text block's panel carried exactly `description, placeholder, cssClass,
// style, maxLength, defaultValue` — no Required checkbox — while the email
// block's panel in the same page carried `requiredMessage` and `pattern`. So
// "make a required short-answer field" was not buildable in the UI at all, for
// the palette's only short-answer field.
//
// `text` is a field type now (card t_7fb80479 — one meaning: a single-line
// answer input whose value is its label; prose is `section`/`html`), and the
// server contract carries it too (formfields.collectableTypes), so `required`
// is enforced on submit rather than being an HTML attribute a direct POST can
// ignore. The API half is pinned by httpapi/required_blocks_submit_test.go.
// THIS file pins the UI half at the level the card measured it: the rendered
// panel's controls.
//
// WHY THE DOM AND NOT THE LIST. `web/src/lib/fieldTypeLists.test.ts` pins the
// CONTENTS of `fieldTypes`; it cannot see whether the panel actually renders the
// controls for a text block, and it stays green if a gate is re-pointed at some
// other list. This spec renders the real built page in a real browser and reads
// the real controls, so a panel that stops offering them fails here.
//
// WHAT MAKES IT BITE (verified, not assumed): with `text` removed from
// `fieldTypes` — the pre-fix tree's list — the Required checkbox does not
// render, so the first assertion fails and the two settings can never appear.
// The API is stubbed at the browser level (no Go server, no session, no DB), so
// the gate needs nothing but the build it is already serving.
import { test, expect, type Page } from "@playwright/test";

import { signIn } from "./auth";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page
// renders. This spec visits one, so it declares the session it means. See
// e2e/auth.ts.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

const FORM_ID = "form_text_block_settings_guard";

// stubAPI answers every /api/* call the builder makes, so the page reaches its
// loaded state without a Go API, a session cookie or a database. The form
// carries one `text` block (the subject) and one `email` block, so the canvas
// has more than one block and the panel is the one the card measured.
async function stubAPI(page: Page) {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });

  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));

  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (route) =>
    route.fulfill(
      json({
        me: { id: "u1", email: "merchant@example.com", name: "Merchant" },
        workspaces: [{ id: "ws1", name: "Demo", slug: "demo", role: "admin" }],
      }),
    ),
  );

  await page.route(/(.*)\/api\/rest\/forms\/[^/]+(\?.*)?$/, (route) =>
    route.fulfill(
      json({
        id: FORM_ID,
        slug: "text-settings-form",
        title: "Text settings form",
        status: "draft",
        url: "",
        config: {
          mode: "payment",
          blocks: [
            // No `id`: the builder hydrates its own, so the assertions below
            // key on the panel's `jf-blk-…` id PREFIX rather than on a value
            // this fixture could have invented.
            { type: "text", value: "Syarikat" },
            { type: "email", value: "Emel", required: true },
          ],
        },
        theme: {},
      }),
    ),
  );
}

// panelControlIds reads the ids of every control in the settings panel (the
// LAST <aside>: the first is the palette). The ids are `jf-blk-<block>-<key>`
// (page.tsx `fid`), so a caller can ask what the panel offers a block WITHOUT
// knowing the hydrated block id.
async function panelControlIds(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    const panel = [...document.querySelectorAll("aside")].pop();
    if (!panel) return [];
    return [...panel.querySelectorAll("input, textarea, select")]
      .filter((el) => (el as HTMLElement).offsetParent !== null)
      .map((el) => el.getAttribute("id") || "");
  });
}

// selectBlock opens the settings panel for the block at `index` on the canvas,
// using the app's own flow: ONE plain click on the block card.
async function selectBlock(page: Page, index: number) {
  await page.goto(`/builder?form=${FORM_ID}`, { waitUntil: "domcontentloaded" });
  const cards = page.locator("section div[draggable]");
  await expect(cards.first()).toBeVisible({ timeout: 25000 });
  await cards.nth(index).click();
  await expect(
    page.locator("aside").last().locator("input, textarea").first(),
  ).toBeVisible();
}

test("builder panel: a text block can be marked required, with a message and a pattern", async ({
  page,
}) => {
  await stubAPI(page);
  await selectBlock(page, 0);

  const before = await panelControlIds(page);
  // The fixture must really be the text block's panel: a control that existed
  // BEFORE the fix has to be there, or "nothing rendered" would satisfy every
  // assertion below.
  expect(
    before.some((id) => id.endsWith("-placeholder")),
    `the panel rendered no placeholder input — it is not the text block's panel: ${JSON.stringify(before)}`,
  ).toBe(true);

  // THE DEFECT: no Required toggle for a text block, so its renderer's
  // `required`/`required_message`/`pattern` were unreachable from the UI.
  const required = page.locator("aside").last().locator('input[type="checkbox"]');
  expect(
    await required.count(),
    `a text block's panel offers no Required toggle (panel ids: ${JSON.stringify(before)})`,
  ).toBe(1);
  expect(await required.first().isChecked()).toBe(false);

  // Unticked, the two dependent settings stay hidden — they are consequences of
  // Required, not extra always-on fields.
  expect(before.filter((id) => id.endsWith("-requiredMessage")).length).toBe(0);
  expect(before.filter((id) => id.endsWith("-pattern")).length).toBe(0);

  await required.first().check();

  const after = await panelControlIds(page);
  const requiredMessage = page.locator("aside").last().locator('input[id$="-requiredMessage"]');
  const pattern = page.locator("aside").last().locator('input[id$="-pattern"]');
  expect(
    await requiredMessage.count(),
    `ticking Required on a text block revealed no Required-message input (panel ids: ${JSON.stringify(after)})`,
  ).toBe(1);
  expect(
    await pattern.count(),
    `ticking Required on a text block revealed no Validation-pattern input (panel ids: ${JSON.stringify(after)})`,
  ).toBe(1);
  await expect(requiredMessage).toBeVisible();
  await expect(pattern).toBeVisible();
});

// THE BITE, RUN IN THE SAME SUITE. The reading above proves nothing about
// whether it CAN see a control's absence: if the selector walked past the panel
// (the mistake the sibling builder spec made once), a green reading would be
// vacuous. So remove a control that is always in a text block's panel
// (`placeholder`) and require the SAME reading to report it gone — while still
// listing the controls that were not touched, so a reading that simply stopped
// finding the panel cannot pass either.
test("builder panel: the text-block settings reading BITES when a control is gone", async ({
  page,
}) => {
  await stubAPI(page);
  await selectBlock(page, 0);

  const before = await panelControlIds(page);
  expect(
    before.filter((id) => id.endsWith("-placeholder")).length,
    `the panel had no placeholder input to remove: ${JSON.stringify(before)}`,
  ).toBe(1);

  const removed = await page.evaluate(() => {
    const panel = [...document.querySelectorAll("aside")].pop();
    if (!panel) return 0;
    let n = 0;
    panel.querySelectorAll('input[id$="-placeholder"]').forEach((el) => {
      if ((el as HTMLElement).offsetParent === null) return;
      el.parentElement?.remove();
      n++;
    });
    return n;
  });
  expect(
    removed,
    "the mutation reached no control — the proof would be vacuous",
  ).toBe(1);

  const after = await panelControlIds(page);
  expect(
    after.some((id) => id.endsWith("-placeholder")),
    `the reading still reports a placeholder after it was removed: ${JSON.stringify(after)}`,
  ).toBe(false);
  // CONTROL: the reading did not simply stop finding the panel.
  expect(
    after.some((id) => id.endsWith("-cssClass")),
    `the reading lost the whole panel, not just the removed control: ${JSON.stringify(after)}`,
  ).toBe(true);
});
