// The a11y gate must SCAN /products, and it must catch the two CRITICAL defects
// that sat on it unreported (card t_3fbc6953).
//
// THE GAP. `web/e2e/a11y.spec.ts` scans the public routes plus every `/docs/*`
// page, and the three sibling `a11y-*.spec.ts` files reach the typed-slug
// dialogs, the workspace switcher and the chat confirmation card. NO a11y spec
// visited a dashboard page with a TABLE, so `/products` was scanned by nothing:
//
//     grep -rn '"/products"' web/e2e/*.spec.ts      -> (no scan entry)
//
// The `a11y` gate therefore recorded `pass` for a tree whose live `/products`
// carried 4 critical axe-core 4.13.0 nodes, measured by QA on the served bundle:
//
//   aria-allowed-attr (critical) x3 — `<button aria-sort="…">` inside each
//     sortable `<th>`. `aria-sort` is supported by `columnheader`/`rowheader`
//     ONLY, so the attribute was both an axe violation and invisible to assistive
//     technology as a sort state. Fixed in `SortHeader`, which now renders the
//     `<th>` itself.
//   select-name (critical) x1 — the create row's `<select>` had no accessible
//     name (no wrapped/explicit `<label>`, no `aria-label`, no `aria-labelledby`).
//
// WHY A NEW SPEC AND NOT AN ENTRY IN `a11y.spec.ts`. That file's PAGES list holds
// routes reachable with no session and it starts from `page.goto` alone; the
// dashboard pages need the `dashboard-bundle` stubbed at the browser level. The
// sibling `a11y-*.spec.ts` files exist for exactly that and are selected by the
// `a11y` project automatically (`testMatch: /a11y.*\.spec\.ts/` in
// `web/playwright.config.ts`), so a new one is in the gate by its name alone.
//
// WHAT MAKES IT BITE (every assertion is asserted to be REACHABLE first — a
// selector that stops matching FAILS rather than skipping, because a guard that
// quietly passes over a page it never rendered is the exact hole this card is
// about):
//   * `button[aria-sort]` must be ZERO — the pre-fix shape, named directly.
//   * the sorted `<th>` must carry `aria-sort`, and the state must MOVE when the
//     sort changes (asserted both ways, not just that the attribute exists).
//   * the direction glyph must stay out of the button's accessible name.
//   * every `<select>` on the page must have a non-empty accessible name.
//   * a FULL-PAGE axe scan failing on critical/serious, so the next regression of
//     either rule on this page is a red gate rather than a green gate over an
//     unvisited route.
//
// The API is stubbed at the browser level (no Go server, no session, no DB), the
// pattern `e2e/affiliate-row.spec.ts` and `e2e/a11y-dialogs.spec.ts` use.
// Storage is cleared and the language pinned to `en` on every case so a
// `jf_lang` left behind by an earlier card cannot render this in a locale the
// assertions do not describe.
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
import AxeBuilder from "@axe-core/playwright";
import type { AxeResults, Result } from "axe-core";
import { DICT } from "../src/components/i18nDict";
import {
  expectEveryControlNamed,
  plantPlaceholderOnlyInput,
  readUnnamedControls,
  removePlanted,
} from "./controlNames";

// Kept identical to a11y.spec.ts / a11y-dialogs.spec.ts so the three cannot drift.
const IMPACTS = ["critical", "serious"] as const;

function blockersOf(results: AxeResults): Result[] {
  return results.violations.filter((v) =>
    IMPACTS.includes((v.impact ?? "") as (typeof IMPACTS)[number]),
  );
}

const EN = DICT;

const ME = {
  id: "u1",
  email: "merchant@example.com",
  name: "Merchant",
  lang: "en",
  is_super_admin: false,
};

const BUNDLE = {
  me: ME,
  workspaces: [{ id: "a1", name: "Demo", slug: "demo", role: "admin" }],
  chip_status: null,
  keys: [],
  domains: [],
  webhooks: [],
  social: [],
  einvoice: null,
  products: [],
  summary: { counts: {}, revenue_sen: 0 },
  sales: { items: [], page: 1, limit: 20, total: 0 },
  form_analytics: [],
  product_analytics: [],
};

// Three rows so the sort has something to move, and a mix of types so the create
// row's own selects render against a realistic list.
const PRODUCTS = [
  { id: "p1", name: "Alpha", price_sen: 100, type: "one_time", active: true, stock: 5, created_at: "2026-09-01T00:00:00Z" },
  { id: "p2", name: "Bravo", price_sen: 5000, type: "subscription", billing_period: "month", active: true, stock: null, created_at: "2026-09-03T00:00:00Z" },
  { id: "p3", name: "Charlie", price_sen: 1000, type: "one_time", active: false, stock: 0, created_at: "2026-09-02T00:00:00Z" },
];

async function stubAPI(page: Page) {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  // Catch-all FIRST, specific routes after it: Playwright resolves matching
  // routes in reverse registration order, so the later (specific) ones win.
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(ME)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "" })));
  await page.route(/(.*)\/api\/rest\/currency\/rates$/, (r) =>
    r.fulfill(json({ rates: [], cached: false })),
  );
  await page.route(/(.*)\/api\/rest\/products\/trash(\?.*)?$/, (r) => r.fulfill(json([])));
  await page.route(/(.*)\/api\/rest\/products(\?.*)?$/, (r) => r.fulfill(json(PRODUCTS)));
}

/**
 * A FRESH browser state: storage cleared first (the browser keeps localStorage
 * between runs, and a `jf_lang` left behind by an earlier card would render this
 * page in a locale these assertions do not describe), then the language pinned
 * explicitly and one workspace selected.
 */
async function freshState(page: Page) {
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("jf_workspace", "demo");
      localStorage.setItem("jf_lang", "en");
    } catch {
      /* storage unavailable */
    }
  });
}

/** Opens /products with the API stubbed and the product row visible. */
async function openProducts(page: Page) {
  await stubAPI(page);
  await freshState(page);
  await page.goto("/products", { waitUntil: "networkidle" });
  // REACHABILITY IS ASSERTED: a page that rendered the loading shell or bounced
  // to /landing has no table, and a scan of it would pass vacuously.
  await expect(
    page.locator("table tbody tr"),
    "the product table must render — a scan of the loading shell proves nothing",
  ).toHaveCount(PRODUCTS.length, { timeout: 30_000 });
}

test.describe("a11y: the products table and create row", () => {
  test("the sort state is on the column header, never on a button", async ({ page }) => {
    await openProducts(page);

    // THE PRE-FIX SHAPE, named directly. This is the assertion that fails on the
    // defect: `aria-sort` on a `<button>` is `aria-allowed-attr` (critical).
    await expect(
      page.locator("button[aria-sort]"),
      "aria-sort must not be on a <button> — the role button does not support it " +
        "(axe aria-allowed-attr, critical)",
    ).toHaveCount(0);

    // The state lives on the sorted <th> (role columnheader, which DOES support
    // it), and the default sort is Date descending.
    const dateCol = page.locator("th").filter({ hasText: EN["prod.date"].en });
    await expect(dateCol).toHaveAttribute("aria-sort", "descending");

    // Exactly ONE column is announced as sorted, as the APG pattern requires —
    // the inactive columns must omit the attribute, not carry "none".
    await expect(page.locator("th[aria-sort]")).toHaveCount(1);

    // The state MOVES on a click: Name ascending, and Date drops it.
    await page.getByRole("button", { name: EN["prod.name"].en }).click();
    const nameCol = page.locator("th").filter({ hasText: EN["prod.name"].en });
    await expect(nameCol).toHaveAttribute("aria-sort", "ascending");
    await expect(page.locator("th[aria-sort]")).toHaveCount(1);
    await expect(dateCol).not.toHaveAttribute("aria-sort", /.+/);

    // Clicking again flips the direction on the SAME header.
    await page.getByRole("button", { name: EN["prod.name"].en }).click();
    await expect(nameCol).toHaveAttribute("aria-sort", "descending");

    // The direction glyph is decorative: it must not enter the accessible name,
    // or a screen reader announces "Product name ▲" instead of "Product name".
    await expect(
      page.getByRole("button", { name: EN["prod.name"].en, exact: true }),
      "the ▲/▼ glyph must be aria-hidden so the button keeps its plain label",
    ).toBeVisible();
  });

  test("every select on the page has an accessible name", async ({ page }) => {
    await openProducts(page);

    const selects = page.locator("select");
    const count = await selects.count();
    // REACHABILITY FIRST: the create row renders only for a writer, and the
    // filter row always does — so fewer than the type select + two filters means
    // the page did not finish rendering and this check would be vacuous.
    expect(
      count,
      "the page must render at least the create-row type select and the two filter selects",
    ).toBeGreaterThanOrEqual(3);

    for (let i = 0; i < count; i++) {
      const sel = selects.nth(i);
      const name = await sel.evaluate((el) => {
        const aria = el.getAttribute("aria-label");
        if (aria && aria.trim()) return aria.trim();
        const labelledBy = el.getAttribute("aria-labelledby");
        if (labelledBy) {
          const txt = labelledBy
            .split(/\s+/)
            .map((id) => document.getElementById(id)?.textContent ?? "")
            .join(" ")
            .trim();
          if (txt) return txt;
        }
        const wrap = el.closest("label");
        if (wrap && (wrap.textContent ?? "").trim()) return (wrap.textContent ?? "").trim();
        const id = el.getAttribute("id");
        if (id) {
          const explicit = document.querySelector(`label[for="${CSS.escape(id)}"]`);
          if (explicit && (explicit.textContent ?? "").trim()) return (explicit.textContent ?? "").trim();
        }
        return "";
      });
      expect(
        name,
        `select #${i} (value="${await sel.inputValue()}") has no accessible name — axe reports ` +
          `select-name (critical)`,
      ).not.toBe("");
    }
  });

  // THE CONTROL-NAME CENSUS (card t_becb0f50). The create row's name / price /
  // stock inputs were named ONLY by their placeholders while the row's three
  // `<select>`s had already been given `aria-label`s — and axe reported NOTHING
  // for any of the three inputs, because axe ACCEPTS a placeholder as an
  // accessible name. So the gate's full-page scan above passed on a row whose
  // fields lose their name the moment a buyer types. The census that sees the
  // class is shared with `a11y-authed-pages.spec.ts` (`e2e/controlNames.ts`) and
  // runs here on the whole page, with the plant arm below proving it bites.
  test("no control on /products is named only by its placeholder", async ({ page }) => {
    await openProducts(page);

    // REACHABILITY FIRST: this is the surface the class shipped on. A census of
    // a page that rendered only its table would pass without looking at the row.
    const createRow = page.locator("form.jf-card");
    await expect(createRow, "the create row must render — it is where this class shipped").toBeVisible();
    expect(
      await createRow.locator("input, select, textarea").count(),
      "the create row must render its own controls (name, price, stock, and its selects)",
    ).toBeGreaterThanOrEqual(4);

    await expectEveryControlNamed(page, "/products");

    // THE BITE, ON THIS PAGE: the pre-fix shape is a control whose only name is
    // its placeholder. Plant one and require THIS census to refuse it …
    await plantPlaceholderOnlyInput(page);
    expect(
      (await readUnnamedControls(page)).map((u) => u.selector).join(" "),
      "a control named only by its placeholder was NOT refused on /products — this arm would not catch the " +
        "class it exists for (axe accepts a placeholder as a name)",
    ).toContain("#jf-planted-input");
    // … and require the clean reading back once the plant is removed.
    await removePlanted(page, "jf-planted-input");
    await expectEveryControlNamed(page, "/products (planted node removed)");
  });

  test("/products has no critical/serious axe violations", async ({ page }) => {
    await openProducts(page);

    const results = await new AxeBuilder({ page }).analyze();
    const blockers = blockersOf(results);
    if (blockers.length > 0) {
      console.log(
        `\n[/products] ${blockers.length} critical/serious violation(s):\n` +
          blockers
            .map(
              (v) =>
                `  - ${v.id} (${v.impact}): ${v.help}\n` +
                v.nodes
                  .slice(0, 3)
                  .map((n) => `      ${n.target.join(" ")} — ${n.failureSummary?.split("\n")[0] ?? ""}`)
                  .join("\n"),
            )
            .join("\n"),
      );
    }
    expect(
      blockers.map((v) => v.id),
      "/products must have no critical/serious axe violations",
    ).toEqual([]);
  });
});
