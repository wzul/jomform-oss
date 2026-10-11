// Regression guard: every <select> in the builder's block-settings panel must
// carry a REAL accessible name — a `<label htmlFor>` or an aria reference — not
// a fallback.
//
// THE DEFECT (QA card t_64b61482, a rework of t_524d186d / PR #701):
// PR #701 named every <input> and <textarea> in the panel by pairing
// `<label htmlFor={fid(key)}>` with `id={fid(key)}`, but left the very same
// bare-sibling `<label>` (no `htmlFor`, wrapping nothing) on the panel's
// <select>s. Three of them — the name-format picker, the date/time-format
// picker and the product picker — therefore had NO accessible name at all, and
// axe reported `select-name` (CRITICAL) on each. Measured in the live DOM on a
// production build, signed in: `labels=0`, `id=false`, `aria-labelledby=null`,
// `aria-label=null`, with the label right next to it carrying
// `labelHasFor=false`. The card's own criterion — "0 critical violations on
// the builder panel" — failed on all three.
//
// WHY THIS GUARD IS A DOM READING AND NOT AN ATTRIBUTE GREP
// `npm run test:unit` (node --test over src/lib/*.test.ts) has no DOM, so it
// cannot tell "the label is a sibling with no htmlFor" from "the label is
// associated"; and an assertion on the source text ("the file contains
// `id={fid(\"nameFormat\")}`") would stay green while a different markup change
// re-created the same unnamed select. This file runs in the axe gate
// (`npm run a11y`, part of `scripts/preflight.sh`) because that is the one gate
// in this repo that renders the real built page in a real browser, and it reads
// the ACTUAL accessible-name sources off the ACTUAL rendered elements.
//
// WHY IT IS NOT JUST AXE AGAIN
// axe already covers an <input> named only by its `placeholder` (it accepts the
// placeholder as a name), so a placeholder-only control is invisible to the
// scan. This guard refuses placeholder-only outright: a placeholder disappears
// the moment you type, and WCAG 3.3.2 wants the instruction to persist. That is
// the same argument the panel's own code comment makes for the fields PR #701
// fixed.
//
// WHY THE FIVE STATES: the guard scans every block type whose panel renders a
// select — name and date (their format picker, plus the conditional rule picker,
// hence 2 each), the `select` block (choices + rule picker), the product block
// (its product picker) — and keeps a text block as a control that renders none.
// Scanning only one panel state is what let the defect through the first time.
//
// WHAT MAKES IT BITE (verified, not assumed): on the pre-fix tree the name-,
// date- and product-block cases fail with `nameSource: "NONE"` — see the PR.
// The API is stubbed at the browser level (no Go server, no session, no DB), so
// the gate needs nothing but the build it is already serving.
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

const FORM_ID = "form_panel_select_guard";
const BLOCK = (type: string, extra: Record<string, unknown> = {}) => ({
  type,
  value: type,
  ...extra,
});

// The block types whose panel renders the controls this card fixed, in the
// order the canvas lists them. Each also renders the conditional-logic rule
// picker (they are field types and the form has more than one block), which is
// why the count is 2 where the card names one picker: the guard asserts the
// count so a panel that rendered nothing cannot pass vacuously.
const CASES = [
  { label: "name — the format picker", index: 0, selects: 2 },
  { label: "date — the format picker", index: 1, selects: 2 },
  // A `select` block renders the choices editor (guarded by its own card,
  // t_c4abb920) but no format picker: its only <select> is the rule picker.
  { label: "select — choices + rule picker", index: 2, selects: 1 },
  { label: "product — the product picker", index: 3, selects: 1 },
  { label: "text — no select at all (control)", index: 4, selects: 0 },
] as const;

type Reading = {
  tag: string;
  nameSource: string;
  name: string;
  outer: string;
  box: string;
};

// stubAPI answers every /api/* call the builder makes, so the page reaches its
// loaded state without a Go API, a session cookie or a database.
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
  // One product, so the product block's select is POPULATED — an empty select
  // could be argued to be a fixture artefact.
  await page.route(/(.*)\/api\/rest\/products(\?.*)?$/, (route) =>
    route.fulfill(
      json([{ id: "p1", name: "Guard Widget", price_sen: 1500, active: true }]),
    ),
  );
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+(\?.*)?$/, (route) =>
    route.fulfill(
      json({
        id: FORM_ID,
        slug: "guard-form",
        title: "Guard form",
        status: "draft",
        url: "",
        config: {
          mode: "payment",
          blocks: [
            BLOCK("name"),
            BLOCK("date"),
            BLOCK("select", {
              choices: [
                { label: "One", value: "One" },
                { label: "Two", value: "Two" },
              ],
            }),
            BLOCK("product", { product_id: "p1" }),
            BLOCK("text"),
          ],
        },
        theme: {},
      }),
    ),
  );
}

// readPanelControls reads the live DOM of the settings panel (the LAST <aside>:
// the first is the palette) and reports, for every visible control, WHERE its
// accessible name comes from.
async function readPanelControls(page: Page): Promise<Reading[]> {
  return page.evaluate(() => {
    const panel = [...document.querySelectorAll("aside")].pop();
    if (!panel) return [];
    const visible = (el: Element) => (el as HTMLElement).offsetParent !== null;
    return [...panel.querySelectorAll("select, input, textarea")]
      .filter(visible)
      .map((el) => {
        const e = el as HTMLInputElement;
        const labelledby = e.getAttribute("aria-labelledby");
        let name = "";
        let nameSource = "NONE";
        if (e.labels && e.labels.length > 0) {
          // Resolved text of the associated <label> (explicit for= or wrapped).
          name = [...e.labels].map((l) => (l.textContent || "").trim()).join(" ").trim();
          nameSource = "label";
        } else if (labelledby) {
          name = labelledby
            .split(/\s+/)
            .map((id) => (document.getElementById(id)?.textContent || "").trim())
            .join(" ")
            .trim();
          nameSource = name ? "aria-labelledby" : "aria-labelledby-DANGLING";
        } else if (e.getAttribute("aria-label")) {
          name = (e.getAttribute("aria-label") || "").trim();
          nameSource = "aria-label";
        } else if (e.getAttribute("placeholder") || e.getAttribute("title")) {
          name = (e.getAttribute("placeholder") || e.getAttribute("title") || "").trim();
          nameSource = "PLACEHOLDER-ONLY";
        }
        return {
          tag: e.tagName.toLowerCase(),
          nameSource,
          name,
          outer: (e.outerHTML || "").slice(0, 160),
          box: `${Math.round(e.getBoundingClientRect().width)}x${Math.round(e.getBoundingClientRect().height)}`,
        };
      });
  });
}

// assertNamed is THE invariant this file guards, extracted so the same function
// is used by the clean-reading test and by the mutation test that proves it
// bites. It throws a plain Error (not a Playwright assertion) so
// `expect(() => assertNamed(...)).toThrow()` can call it directly.
//
// The rule: every <select> in the panel must report its accessible name from a
// `<label>` or an aria reference. A name that comes only from a `placeholder`
// (or `title`) is a FAILURE here even though axe accepts it — a placeholder
// disappears the moment you type, and WCAG 3.3.2 wants the instruction to
// persist. A `NONE` name is the outright defect axe reports as `select-name`
// (critical). A dangling `aria-labelledby` resolves to the empty string and
// names nothing, so it is a failure too.
function assertNamed(label: string, selects: Reading[]): void {
  const bad = selects.filter(
    (r) => r.nameSource === "NONE" || r.nameSource === "PLACEHOLDER-ONLY",
  );
  if (bad.length > 0) {
    throw new Error(
      `${label}: ${bad.length} select(s) have no accessible name from a label or an aria ` +
        `reference — that is axe select-name (critical). Readings: ${JSON.stringify(selects)}`,
    );
  }
  const dangling = selects.filter((r) => r.nameSource === "aria-labelledby-DANGLING");
  if (dangling.length > 0) {
    throw new Error(
      `${label}: an aria-labelledby points at an id that does not exist (the name resolves ` +
        `to ""). Readings: ${JSON.stringify(dangling)}`,
    );
  }
  if (selects.some((r) => !r.name)) {
    throw new Error(
      `${label}: a select reports a name source but an EMPTY name. Readings: ${JSON.stringify(selects)}`,
    );
  }
}

for (const c of CASES) {
  test(`builder panel: every select on the ${c.label} has a real name`, async ({ page }) => {
    await stubAPI(page);
    await page.goto(`/builder?form=${FORM_ID}`, { waitUntil: "domcontentloaded" });
    // The panel renders only once a block is selected. ONE plain click on the
    // block card is the app's own flow — no DOM forcing, no display override.
    const cards = page.locator("section div[draggable]");
    await expect(cards.first()).toBeVisible({ timeout: 25000 });
    await cards.nth(c.index).click();
    // Wait for the panel to actually render controls for this block type.
    await expect(page.locator("aside").last().locator("select, input, textarea").first()).toBeVisible();

    const readings = await readPanelControls(page);
    const selects = readings.filter((r) => r.tag === "select");
    expect(
      selects.length,
      `${c.label}: expected ${c.selects} <select> in the panel, read ${JSON.stringify(readings)}`,
    ).toBe(c.selects);
    assertNamed(c.label, selects);
  });

  // THE BITE, RUN IN THE SAME SUITE. A clean reading above proves nothing about
  // whether the reading CAN see the defect — the previous card's proof used a
  // selector (`aside select[aria-labelledby]`) that walked straight past the
  // unnamed select on a name/date/product panel and reported a green mutation.
  // So: render the same panel, unname the element the way the defect did
  // (detach its `<label htmlFor>` / strip the aria reference), and require the
  // SAME assertion to fail on the SAME reading. If the invariant stops biting,
  // this test goes red even though the panel is still correct.
  test(`builder panel: the name invariant BITES on the ${c.label}`, async ({ page }) => {
    if (c.selects === 0) test.skip(true, "this block type renders no select — nothing to unname");
    await stubAPI(page);
    await page.goto(`/builder?form=${FORM_ID}`, { waitUntil: "domcontentloaded" });
    const cards = page.locator("section div[draggable]");
    await expect(cards.first()).toBeVisible({ timeout: 25000 });
    await cards.nth(c.index).click();
    await expect(page.locator("aside").last().locator("select, input, textarea").first()).toBeVisible();

    // Unname EVERY select in the panel, the way each of them was unnamed: drop
    // the label association and both aria references. `applied` is reported so a
    // mutation that matched nothing can never read as a pass.
    const applied = await page.evaluate(() => {
      const panel = [...document.querySelectorAll("aside")].pop();
      if (!panel) return 0;
      let n = 0;
      panel.querySelectorAll("select").forEach((s) => {
        if (s.offsetParent === null) return;
        const id = s.getAttribute("id");
        if (id) {
          const lab = panel.querySelector(`label[for="${id}"]`);
          if (lab) lab.removeAttribute("for");
        }
        s.removeAttribute("aria-labelledby");
        s.removeAttribute("aria-label");
        n++;
      });
      return n;
    });
    expect(applied, `${c.label}: the mutation reached no select — the proof would be vacuous`).toBe(
      c.selects,
    );

    const readings = await readPanelControls(page);
    const selects = readings.filter((r) => r.tag === "select");
    // The reading must now SEE the defect: every select unnamed.
    expect(
      selects.every((r) => r.nameSource === "NONE" || r.nameSource === "PLACEHOLDER-ONLY"),
      `${c.label}: after unnaming every select the reading still reports a name, so it is ` +
        `reading something other than the element under test: ${JSON.stringify(selects)}`,
    ).toBe(true);
    // …and the invariant that guards the fix must reject it.
    expect(
      () => assertNamed(`${c.label} (mutated)`, selects),
      `${c.label}: the name invariant did NOT bite on an unnamed select — the clean reading ` +
        `above is therefore not evidence`,
    ).toThrow();
  });
}
