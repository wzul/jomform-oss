// Regression guard: EVERY visible control in the builder's block-settings panel
// must report an accessible name from a real source — a `<label>` (`for=` or
// wrapping) or an aria reference — and NOT from `placeholder`/`title` alone.
//
// THE DEFECT (QA card t_c4abb920, found while verifying t_524d186d / PR #701):
// PR #701 named every <input>/<textarea>/<select> in the panel that had a bare
// sibling <label>, but the CHOICES editor's per-choice rows were left out. Each
// choice's label input carried no id, no <label>, no aria-label and no
// aria-labelledby: its only accessible name was
// `placeholder={t("bld.choiceLabel")}` ("Label"), and the price input was named
// by `title` only. Measured in the live DOM on a production build, signed in:
//   {"id":null,"labels":0,"ariaLabel":null,"ariaLabelledby":null,
//    "placeholder":"Label","value":"One","announced":"PLACEHOLDER-ONLY"}
// and after typing: {"value":"Alpha","labels":0} — the name is GONE.
//
// WHY AXE IS NOT THE TEST HERE
// axe ACCEPTS a placeholder as an accessible name, so a placeholder-only control
// is invisible to `npm run a11y`. QA proved axe is blind to this class of
// control with glyph-only probes rather than assuming it, and in the same run axe
// reported NO findings on the choices panel. So "0 critical" is not evidence for
// this defect: the guard has to read the ACTUAL name sources off the ACTUAL DOM.
//
// WHAT MAKES IT BITE (verified on the pre-fix tree, not assumed): on the tree
// before this card's fix the choice-row inputs report `nameSource:
// "PLACEHOLDER-ONLY"` and this file goes red; deleting the fix's `aria-label`
// turns it red again (the mutation test below runs the SAME invariant).
//
// The API is stubbed at the browser level (no Go server, no session, no DB), so
// the gate needs nothing but the build it is already serving.
import { test, expect, type Page } from "@playwright/test";

const FORM_ID = "form_panel_choice_guard";
const BLOCK = (type: string, extra: Record<string, unknown> = {}) => ({
  type,
  value: type,
  ...extra,
});

// Block types whose panel renders the choices editor. `select` renders the
// label inputs only; `option` additionally renders the per-choice PRICE input
// (the one named by `title` alone before the fix). A `text` block renders no
// choices editor at all and is kept as a control so a panel that rendered
// nothing cannot pass vacuously.
const CASES = [
  { label: "select — choices editor, label inputs", index: 0, type: "select", minInputs: 3 },
  { label: "option — choices editor, label + price inputs", index: 1, type: "option", minInputs: 4 },
  { label: "text — no choices editor (control)", index: 2, type: "text", minInputs: 0 },
] as const;

type Reading = {
  tag: string;
  nameSource: string;
  name: string;
  placeholder: string | null;
  outer: string;
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
            BLOCK("select", {
              choices: [
                { label: "One", value: "One" },
                { label: "Two", value: "Two" },
                { label: "Three", value: "Three" },
              ],
            }),
            BLOCK("option", {
              choices: [
                { label: "Small", value: "Small", price_sen: 1000 },
                { label: "Large", value: "Large", price_sen: 2000 },
              ],
            }),
            BLOCK("text"),
          ],
        },
        theme: {},
      }),
    ),
  );
}

// readPanelControls reads the live DOM of the settings panel (the LAST <aside>:
// the first is the palette) and reports, for every VISIBLE control, WHERE its
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
          nameSource = name ? "label" : "label-EMPTY";
        } else if (labelledby) {
          name = labelledby
            .split(/\s+/)
            .map((id) => (document.getElementById(id)?.textContent || "").trim())
            .join(" ")
            .trim();
          nameSource = name ? "aria-labelledby" : "aria-labelledby-DANGLING";
        } else if (e.getAttribute("aria-label")) {
          name = (e.getAttribute("aria-label") || "").trim();
          nameSource = name ? "aria-label" : "aria-label-EMPTY";
        } else if (e.getAttribute("placeholder") || e.getAttribute("title")) {
          name = (e.getAttribute("placeholder") || e.getAttribute("title") || "").trim();
          nameSource = "PLACEHOLDER-ONLY";
        }
        return {
          tag: e.tagName.toLowerCase(),
          nameSource,
          name,
          placeholder: e.getAttribute("placeholder"),
          outer: (e.outerHTML || "").slice(0, 200),
        };
      });
  });
}

// assertEveryControlNamed is THE invariant this file guards, extracted so the
// same function is used by the clean-reading test and by the mutation test that
// proves it bites. It throws a plain Error (not a Playwright assertion) so
// `expect(() => assertEveryControlNamed(...)).toThrow()` can call it directly.
//
// The rule: every visible control must report its accessible name from a
// `<label>` or an aria reference. A name that comes only from a `placeholder`
// (or `title`) is a FAILURE even though axe accepts it — a placeholder
// disappears the moment you type, and WCAG 3.3.2 wants the instruction to
// persist. `NONE` is the outright defect; a dangling `aria-labelledby` resolves
// to the empty string and names nothing.
function assertEveryControlNamed(label: string, all: Reading[]): void {
  const bad = all.filter((r) => r.nameSource === "NONE" || r.nameSource === "PLACEHOLDER-ONLY");
  if (bad.length > 0) {
    throw new Error(
      `${label}: ${bad.length} control(s) have no accessible name from a label or an aria ` +
        `reference — a placeholder is not a label. Readings: ${JSON.stringify(bad)}`,
    );
  }
  const weak = all.filter(
    (r) => r.nameSource === "aria-labelledby-DANGLING" || r.nameSource === "label-EMPTY" || r.nameSource === "aria-label-EMPTY",
  );
  if (weak.length > 0) {
    throw new Error(
      `${label}: a control reports a name source that resolves to an EMPTY name. ` +
        `Readings: ${JSON.stringify(weak)}`,
    );
  }
  if (all.some((r) => !r.name)) {
    throw new Error(
      `${label}: a control reports a name source but an EMPTY name. Readings: ${JSON.stringify(all)}`,
    );
  }
}

for (const c of CASES) {
  test(`builder panel: every control on the ${c.label} has a real name`, async ({ page }) => {
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
    expect(
      readings.length,
      `${c.label}: expected at least ${c.minInputs} control(s) in the panel, read ${JSON.stringify(readings)}`,
    ).toBeGreaterThanOrEqual(c.minInputs || 1);
    assertEveryControlNamed(c.label, readings);
  });

  // THE BITE, RUN IN THE SAME SUITE. A clean reading above proves nothing about
  // whether the reading CAN see the defect. So: render the same panel, unname
  // EVERY control the way the defect did (drop the label association and both
  // aria references), and require the SAME assertion to fail on the SAME
  // reading. `applied` is reported so a mutation that matched nothing can never
  // read as a pass.
  test(`builder panel: the name invariant BITES on the ${c.label}`, async ({ page }) => {
    if (c.minInputs === 0) test.skip(true, "this block type renders the choices editor's inputs — nothing to unname");
    await stubAPI(page);
    await page.goto(`/builder?form=${FORM_ID}`, { waitUntil: "domcontentloaded" });
    const cards = page.locator("section div[draggable]");
    await expect(cards.first()).toBeVisible({ timeout: 25000 });
    await cards.nth(c.index).click();
    await expect(page.locator("aside").last().locator("select, input, textarea").first()).toBeVisible();

    const applied = await page.evaluate(() => {
      const panel = [...document.querySelectorAll("aside")].pop();
      if (!panel) return 0;
      let n = 0;
      panel.querySelectorAll("select, input, textarea").forEach((s) => {
        if ((s as HTMLElement).offsetParent === null) return;
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
    expect(applied, `${c.label}: the mutation reached no control — the proof would be vacuous`).toBeGreaterThan(0);

    const readings = await readPanelControls(page);
    // …and the invariant that guards the fix must reject it.
    expect(
      () => assertEveryControlNamed(`${c.label} (mutated)`, readings),
      `${c.label}: the name invariant did NOT bite on an unnamed control — the clean reading ` +
        `above is therefore not evidence. Readings: ${JSON.stringify(readings)}`,
    ).toThrow();
  });
}

// The card's criterion 2: the `bld.choices` heading must be associated with the
// group of rows it describes, so a screen-reader user gets a named group rather
// than a list of unnamed text boxes under an unrelated heading.
test("builder panel: the choices heading names a role=group", async ({ page }) => {
  await stubAPI(page);
  await page.goto(`/builder?form=${FORM_ID}`, { waitUntil: "domcontentloaded" });
  const cards = page.locator("section div[draggable]");
  await expect(cards.first()).toBeVisible({ timeout: 25000 });
  await cards.nth(0).click();
  await expect(page.locator("aside").last().locator("select, input, textarea").first()).toBeVisible();

  const group = await page.evaluate(() => {
    const panel = [...document.querySelectorAll("aside")].pop();
    if (!panel) return null;
    const g = panel.querySelector('[role="group"][aria-labelledby]');
    if (!g) return { found: false };
    const ids = (g.getAttribute("aria-labelledby") || "").split(/\s+/).filter(Boolean);
    const text = ids
      .map((id) => (document.getElementById(id)?.textContent || "").trim())
      .join(" ")
      .trim();
    return { found: true, text, hasInputs: g.querySelectorAll("input").length };
  });
  expect(group, "the choices editor must expose the group it describes").not.toBeNull();
  expect(group!.found, "no role=group + aria-labelledby found in the panel").toBe(true);
  expect(group!.text, "the group's accessible name resolves to empty").not.toBe("");
  // A group that names nothing is not a group — it must actually contain the rows.
  expect(group!.hasInputs, "the named group contains no inputs — it does not describe the choice rows").toBeGreaterThan(0);
});
