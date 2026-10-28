// a11y guard for the legacy-`text` prose warning (card t_4823943a).
//
// The merchant-facing repair path this card adds is a WARNING plus one action,
// rendered inside the builder's block-settings panel:
//
//   <div role="alert">                      ← "this reads like an introduction…"
//     <button type="button">Treat as intro line</button>
//   </div>
//
// Two things make it usable without a mouse or with a screen reader, and neither
// is visible to the feature spec (which drives the flow):
//
//   1. ANNOUNCEMENT. The notice is a `role="alert"` — a live region, so a screen
//      reader announces it the moment it appears. It appears WHILE the merchant
//      is typing (the value crosses the prose threshold), which is the whole
//      point of putting it on the edit surface rather than at save time. This
//      spec asserts the role and that the region carries the sentence, and it
//      asserts the region is not empty (an aria-live region with no text
//      announces nothing).
//   2. KEYBOARD ORDER. The action is a real <button> placed directly after the
//      value textarea, so ONE Tab from the field the merchant is typing in lands
//      on it. This spec presses that key and reads `document.activeElement` —
//      the same shape `a11y-tabstrips.spec.ts` uses for focus order. A `div`
//      with an onClick, or a button rendered before the textarea, fails here.
//
// And the panel carries both states (warning → converted + undo) through the
// gate's own axe rule (`gateScan` from e2e/axeGate.ts), with the planted
// A-level control that proves the scan could see a defect if one were there.
import { test, expect, type Page } from "@playwright/test";

import { signIn } from "./auth";
import { blockersOf, expectGateSeesAriaViolation, gateScan } from "./axeGate";

// Dashboard paths are gated on the SERVER (web/src/middleware.ts); this spec
// visits one, so it declares the session it means. The API itself is stubbed
// below — no Go server, no DB, no real credential.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

const FORM_ID = "form_a11y_text_prose_warning";
const PROSE = "Register for our event — cuma rsvp ja";
const ACTION = "Treat as intro line";
const UNDO = "Undo — make it a field again";

const json = (body: unknown) => ({ status: 200, contentType: "application/json", body: JSON.stringify(body) });

async function stubAPI(page: Page) {
  await page.route(/(.*)\/api\/.*$/, (r) => r.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) =>
    r.fulfill(
      json({
        me: { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "en", is_super_admin: false },
        workspaces: [{ id: "ws1", name: "Demo", slug: "demo", role: "admin" }],
        chip_status: null, keys: [], domains: [], social: [], einvoice: null,
        webhooks: [], products: [], summary: { counts: {}, revenue_sen: 0 },
        sales: { items: [], page: 1, limit: 20, total: 0 },
        form_analytics: [], product_analytics: [],
      }),
    ),
  );
  await page.route(new RegExp(`(.*)/api/rest/forms/${FORM_ID}(\\?.*)?$`), (r) =>
    r.fulfill(
      json({
        id: FORM_ID,
        slug: "a11y-text-prose",
        title: "Text prose",
        status: "draft",
        url: "",
        config: { mode: "response", blocks: [{ id: "qoubfpj", type: "text", value: PROSE }] },
      }),
    ),
  );
}

// The settings panel is the SECOND <aside> in the builder's grid (the first is
// the palette); it is the last of its type among its siblings, which is why the
// selector is a `:last-of-type` rather than an index.
const PANEL = "aside:last-of-type";

/** Opens the builder in English with a clean profile and selects the text block. */
async function openPanel(page: Page) {
  await page.addInitScript(() => {
    localStorage.clear();
    localStorage.setItem("jf_workspace", "demo");
    localStorage.setItem("jf_lang", "en");
  });
  await stubAPI(page);
  await page.goto(`/builder?form=${FORM_ID}`, { waitUntil: "networkidle" });
  await expect(page.getByRole("button", { name: "Publish", exact: true }).first()).toBeVisible({ timeout: 45_000 });
  const cards = page.locator("section div[draggable]");
  await cards.first().click();
  await expect(page.locator("#jf-block-value")).toBeVisible();
  return page.locator(PANEL);
}

test("a11y: the prose warning is announced, and one Tab reaches its action", async ({ page }) => {
  await openPanel(page);

  // ANNOUNCEMENT. `role="alert"` is the live region; the text inside it is what
  // the screen reader reads, so an empty region would announce nothing. Scoped to
  // the panel: Next's own router announcer is a second `role="alert"` on every
  // page (`#__next-route-announcer__`), and it has nothing to do with this notice.
  const region = page.locator(PANEL).getByRole("alert");
  await expect(region, "the prose warning is not an announced live region").toBeVisible();
  await expect(region).toHaveAttribute("role", "alert");
  const copy = (await region.innerText()).trim();
  expect(copy.length, "the live region carries no sentence, so nothing is announced").toBeGreaterThan(40);
  expect(copy).toContain("reads like an introduction");

  // THE ACTION IS A REAL, NAMED BUTTON.
  const action = region.getByRole("button", { name: ACTION });
  await expect(action).toBeVisible();
  await expect(action).toHaveAccessibleName(ACTION);
  await expect(action).toBeEnabled();

  // KEYBOARD ORDER. The merchant is typing in the value textarea; ONE Tab must
  // land on the action, with no pointer and no other control in between.
  await page.locator("#jf-block-value").focus();
  await page.keyboard.press("Tab");
  const focused = await page.evaluate(() => {
    const el = document.activeElement as HTMLElement | null;
    return { tag: el?.tagName ?? "", text: (el?.textContent ?? "").trim(), id: el?.id ?? "" };
  });
  expect(
    focused,
    "one Tab from the block's label textarea did not reach the warning's action — a keyboard user cannot convert the block",
  ).toEqual({ tag: "BUTTON", text: ACTION, id: "" });

  // AXE, on the panel with the warning on screen — the state the gate had never
  // seen, because no spec had ever driven a prose value into a text block.
  const results = await gateScan(page, PANEL);
  const blockers = blockersOf(results);
  expect(
    blockers.map((v) => `${v.id} (${v.impact}) on ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`),
    "the builder panel with the prose warning carries a critical/serious axe violation",
  ).toEqual([]);
  expect(
    results.passes.length + results.incomplete.length + results.violations.length,
    "the scan evaluated nothing, so the clean reading above is not evidence",
  ).toBeGreaterThan(0);
  // CONTROL: the same scan must see a planted A-level defect inside this panel.
  await expectGateSeesAriaViolation(page, PANEL, "the builder's block-settings panel");

  // The converted state (status + undo) is scanned too — it replaces the warning
  // in the same panel and is the only surface the undo can be reached from.
  await action.click();
  const undo = page.getByRole("status").filter({ hasText: "Converted" }).getByRole("button", { name: UNDO });
  await expect(undo, "the conversion offered no keyboard-reachable undo").toBeVisible();
  await expect(undo).toHaveAccessibleName(UNDO);
  await page.locator("#jf-block-value").focus();
  await page.keyboard.press("Tab");
  const focusedAfter = await page.evaluate(() => {
    const el = document.activeElement as HTMLElement | null;
    return { tag: el?.tagName ?? "", text: (el?.textContent ?? "").trim() };
  });
  // The section block's panel starts with the value textarea as well, and the
  // undo notice sits directly under it — same order as the warning did.
  expect(focusedAfter, "a keyboard user cannot reach the undo from the field").toEqual({ tag: "BUTTON", text: UNDO });

  const after = await gateScan(page, PANEL);
  expect(
    blockersOf(after).map((v) => `${v.id} (${v.impact}) on ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`),
    "the converted panel (status + undo) carries a critical/serious axe violation",
  ).toEqual([]);
  await expectGateSeesAriaViolation(page, PANEL, "the builder panel in its converted state");
});
