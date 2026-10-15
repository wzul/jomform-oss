// The ARIA tabs pattern on the app's three TWO-TAB strips (card t_40253ae6).
//
// WHY THIS FILE EXISTS. `e2e/a11y-profile-tabs.spec.ts` (card t_b589a807) pins the
// pattern on `/profile` and `e2e/a11y-settings-first-run-tab.spec.ts` on
// `/settings` — the two strips whose tab sets are large enough to have their own
// guards. The app has THREE more strips with the SAME shape, the Active/Trash
// switcher on `/products`, `/media` and `/forms`, and this card's defect was in
// all five: every strip carried `role="tablist"` + `role="tab"` + `aria-selected`
// and NOTHING in `src/` carried `aria-controls` or `role="tabpanel"` (grep: 0
// hits), so no tab was associated with the panel it controls. Without this file
// the three two-tab strips would be fixed in the pages and MEASURED NOWHERE —
// exactly the shape of gap this board has been bitten by (a fix that no arm reads
// is a fix that can silently regress).
//
// WHAT IS DIFFERENT FROM THE TWO-TAB STRIPS' SIBLINGS, AND WHY IT DOES NOT WEAKEN
// THEM: the tab ids here are `active` / `trash` and the panels are the page's two
// big view branches, so the association is read WITHOUT the "the panel's first
// `<h2>`" heuristic those two specs use (a products trash pane with an empty bin
// has no `<h2>` of its own). The painted panel is instead identified by the
// property that makes it the painted one: the document renders exactly ONE
// `role="tabpanel"`, it is VISIBLE, and it has a non-zero box on screen. That is
// a stronger statement than a text match, and it is the fact the association has
// to be true about.
//
// WHY A BROWSER MEASUREMENT, NOT A SOURCE ASSERTION. "This tab's aria-controls
// names the panel that is actually painted" is a property of the RENDERED
// document: the idref is RESOLVED with `getElementById` (a tab naming a panel
// that does not exist fails here), and the resolved element is compared with the
// one tabpanel the page renders. A grep over JSX shows neither.
//
// THIS FILE DOES NOT RUN axe, and that is deliberate rather than an omission: the
// full-document scans of `/products` and `/media` live in
// `e2e/a11y-products.spec.ts` and `e2e/a11y-authed-pages.spec.ts`, and the defect
// this file is about is invisible to axe either way — measured on the live
// signed-in page, the partial pattern reported 0 violations of any impact. A
// second, scoped scan here would add a page-level failure mode to a tabs contract
// without measuring anything the arms below do not.
//
// THE API IS STUBBED AT THE BROWSER LEVEL (the pattern every sibling spec uses):
// no Go server, no session cookie, no DB. Storage is cleared and the language
// pinned to `en` on every case, so a `jf_lang` left behind by an earlier card
// cannot render this in a locale the assertions do not describe.
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

import { DICT } from "../src/components/i18nDict";

/**
 * The three two-tab strips, with the page's own tab ids (`active` / `trash`, the
 * values the pages render), the DOM id prefix the page composes them in, and the
 * dictionary keys of the two labels. Read from the pages' source rather than
 * restated would be nicer; what is pinned here is the SHAPE every strip shares,
 * and the label assertion below fails by name if a label key moves.
 */
const STRIPS = [
  {
    route: "/products",
    prefix: "prod",
    labelKeys: { active: "prod.trashTab", trash: "prod.trashTabDeleted" },
  },
  {
    route: "/media",
    prefix: "media",
    labelKeys: { active: "media.trashTab", trash: "media.trashTabDeleted" },
  },
  {
    route: "/forms",
    prefix: "forms",
    labelKeys: { active: "forms.trashTab", trash: "forms.trashTabDeleted" },
  },
] as const;

const TAB_IDS = ["active", "trash"] as const;

const ME = { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "en", is_super_admin: true };

const BUNDLE = {
  me: ME,
  workspaces: [{ id: "a1", name: "Demo", slug: "demo", role: "admin" }],
  chip_status: null, keys: [], domains: [], webhooks: [], social: [], einvoice: null,
  products: [], summary: { counts: {}, revenue_sen: 0 },
  sales: { items: [], page: 1, limit: 20, total: 0 },
  form_analytics: [], product_analytics: [],
};

async function stubAPI(page: Page, route: string) {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  // Catch-all FIRST, the page's own routes after it: Playwright resolves matching
  // routes in reverse registration order, so the later (specific) ones win.
  await page.route(/(.*)\/api\/.*$/, (r) => r.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(ME)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "en" })));
  await page.route(/(.*)\/api\/rest\/currency\/rates$/, (r) => r.fulfill(json({ rates: [], cached: false })));
  await page.route(/(.*)\/api\/rest\/products(\?.*)?$/, (r) => r.fulfill(json([])));
  await page.route(/(.*)\/api\/rest\/products\/trash(\?.*)?$/, (r) => r.fulfill(json([])));
  await page.route(/(.*)\/api\/rest\/forms(\?.*)?$/, (r) => r.fulfill(json([])));
  await page.route(/(.*)\/api\/rest\/forms\/trash(\?.*)?$/, (r) => r.fulfill(json([])));
  await page.route(/(.*)\/api\/rest\/media(\?.*)?$/, (r) =>
    r.fulfill(json({ items: [], page: 1, limit: 20, total: 0, usage: { used: 0, limit: 1073741824 } })),
  );
  await page.route(/(.*)\/api\/rest\/media\/trash(\?.*)?$/, (r) => r.fulfill(json([])));
  if (route === "/products") {
    await page.route(/(.*)\/api\/rest\/products\?include_archived=1(\?.*)?$/, (r) => r.fulfill(json([])));
  }
}

type StripState = {
  tabCount: number;
  labels: string[];
  selectedCount: number;
  selectedText: string;
  tabsWithoutControls: string[];
  tabIdsWithoutId: number;
  duplicateTabIds: string[];
  selectedId: string;
  selectedControls: string;
  controlledId: string;
  controlledRole: string;
  controlledIsTheRenderedPanel: boolean;
  controlledIsPainted: boolean;
  panelLabelledBy: string;
  tabpanelCount: number;
  tabStops: number;
  tabStopIsSelected: boolean;
  focusedIsTab: boolean;
};

/**
 * One read of the live DOM: the strip, which tab is selected, and the panel the
 * SELECTED tab names — resolved, not string-compared.
 */
async function readStrip(page: Page): Promise<StripState> {
  return page.evaluate(`(() => {
    const label = (t) => {
      const spans = [...t.querySelectorAll("span")];
      const vis = spans.find((s) => getComputedStyle(s).display !== "none");
      return ((vis ? vis.textContent : t.textContent) || "").trim();
    };
    const tabs = [...document.querySelectorAll('[role="tab"]')];
    const panels = [...document.querySelectorAll('[role="tabpanel"]')];
    const sel = tabs.filter((t) => t.getAttribute("aria-selected") === "true");
    const selTab = sel.length ? sel[0] : null;
    const controlsAttr = selTab ? (selTab.getAttribute("aria-controls") || "") : "";
    const controlled = controlsAttr ? document.getElementById(controlsAttr) : null;
    // The PAINTED panel, without relying on a heading: the page renders exactly
    // one, it is visible, and it occupies a non-zero box on screen.
    const painted = panels.filter((p) => {
      const cs = getComputedStyle(p);
      if (cs.display === "none" || cs.visibility === "hidden") return false;
      const b = p.getBoundingClientRect();
      return b.width > 0 && b.height > 0;
    });
    const ids = tabs.map((t) => t.id);
    const stops = tabs.filter((t) => t.getAttribute("tabindex") === "0");
    return {
      tabCount: tabs.length,
      labels: tabs.map(label),
      selectedCount: sel.length,
      selectedText: selTab ? label(selTab) : "",
      tabsWithoutControls: tabs.filter((t) => !t.getAttribute("aria-controls")).map(label),
      tabIdsWithoutId: ids.filter((id) => !id).length,
      duplicateTabIds: ids.filter((id, i) => !!id && ids.indexOf(id) !== i),
      selectedId: selTab ? selTab.id : "",
      selectedControls: controlsAttr,
      controlledId: controlled ? controlled.id : "",
      controlledRole: controlled ? (controlled.getAttribute("role") || "") : "",
      controlledIsTheRenderedPanel: !!controlled && panels.length === 1 && controlled === panels[0],
      controlledIsPainted: !!controlled && painted.length === 1 && controlled === painted[0],
      panelLabelledBy: controlled ? (controlled.getAttribute("aria-labelledby") || "") : "",
      tabpanelCount: panels.length,
      tabStops: stops.length,
      tabStopIsSelected: stops.length === 1 && !!selTab && stops[0] === selTab,
      focusedIsTab: !!document.activeElement && document.activeElement.getAttribute("role") === "tab",
    };
  })()`) as Promise<StripState>;
}

/** The card's acceptance for the strip of `route`, in one place. */
function expectAssociation(s: StripState, what: string) {
  expect(s.tabCount, `${what}: the strip renders ${TAB_IDS.length} tabs`).toBe(TAB_IDS.length);
  expect(s.selectedCount, `${what}: ${s.selectedCount} tabs carry aria-selected — a tablist has exactly one`).toBe(1);
  expect(
    s.tabsWithoutControls,
    `${what}: ${s.tabsWithoutControls.length} of ${s.tabCount} role="tab" nodes carry NO aria-controls ` +
      `(${JSON.stringify(s.tabsWithoutControls)}) — no tab is associated with the panel it controls, and axe ` +
      `reports nothing for the omission either way`,
  ).toEqual([]);
  expect(s.tabIdsWithoutId, `${what}: ${s.tabIdsWithoutId} tab(s) have no id for a panel to reference`).toBe(0);
  expect(s.duplicateTabIds, `${what}: duplicate tab id(s) ${JSON.stringify(s.duplicateTabIds)}`).toEqual([]);
  expect(
    s.tabpanelCount,
    `${what}: ${s.tabpanelCount} role="tabpanel" elements are rendered — the strip paints exactly one view`,
  ).toBe(1);
  expect(
    s.controlledRole,
    `${what}: the selected tab (${JSON.stringify(s.selectedText)}) names ` +
      `${JSON.stringify(s.selectedControls)}, which does not resolve to a role="tabpanel" element — the ` +
      `association points at nothing`,
  ).toBe("tabpanel");
  expect(
    s.controlledIsTheRenderedPanel,
    `${what}: the panel the selected tab controls (#${s.controlledId}) is not the single role="tabpanel" the ` +
      `page renders`,
  ).toBe(true);
  expect(
    s.controlledIsPainted,
    `${what}: the panel the selected tab controls (#${s.controlledId}) is not the one PAINTED (visible, non-zero ` +
      `box) — the association points somewhere other than what the reader sees`,
  ).toBe(true);
  expect(
    s.panelLabelledBy,
    `${what}: the panel #${s.controlledId} is labelled by ${JSON.stringify(s.panelLabelledBy)}, not by its own ` +
      `tab (#${s.selectedId})`,
  ).toBe(s.selectedId);
  expect(
    s.tabStops,
    `${what}: ${s.tabStops} tab(s) carry tabindex="0" — a tablist is ONE tab stop (roving tabindex)`,
  ).toBe(1);
  expect(
    s.tabStopIsSelected,
    `${what}: the strip's single tab stop is not the selected tab`,
  ).toBe(true);
}

/** Opens `route` with a fresh state and waits for the strip to render its tabs. */
async function openStrip(page: Page, route: string, prefix: string): Promise<StripState> {
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
  await stubAPI(page, route);
  await page.setViewportSize({ width: 320, height: 800 });
  const response = await page.goto(route, { waitUntil: "domcontentloaded" });
  expect(
    response?.status() ?? 0,
    `${route}: the page did not answer 2xx — nothing was measured`,
  ).toBeLessThan(300);
  await expect(page.getByRole("tab")).toHaveCount(TAB_IDS.length, { timeout: 45_000 });
  // The panel id the page composes for the tab it lands on must EXIST before this
  // arm claims anything about it: the pages hydrate the strip, and a read taken
  // before the first panel renders would report a missing panel rather than a
  // missing association. `document.getElementById` on the panel the landing tab
  // names is that readiness signal.
  await expect
    .poll(
      async () =>
        page.evaluate(`(() => {
          const sel = document.querySelector('[role="tab"][aria-selected="true"]');
          const id = sel ? sel.getAttribute("aria-controls") : "";
          return !!id && !!document.getElementById(id);
        })()`),
      { message: `${route}: the landing tab's aria-controls never resolved to a rendered panel`, timeout: 45_000 },
    )
    .toBe(true);
  const s = await readStrip(page);
  expect(
    s.selectedId.startsWith(`${prefix}-tab-`),
    `${route}: the selected tab's id is ${JSON.stringify(s.selectedId)} — the strip is expected to compose its ` +
      `ids with the "${prefix}" prefix (lib/tablist.ts), or this arm is reading the wrong strip`,
  ).toBe(true);
  return s;
}

for (const { route, prefix, labelKeys } of STRIPS) {
  test(`${route}: every tab names its panel, and the selected tab's panel is the one painted`, async ({ page }) => {
    test.setTimeout(120_000);
    const s = await openStrip(page, route, prefix);
    expect(
      s.labels,
      `${route}: the strip is not rendering its own two labels`,
    ).toEqual(TAB_IDS.map((id) => DICT[labelKeys[id] as keyof typeof DICT].en));
    expectAssociation(s, `${route} on arrival`);
  });

  test(`${route}: clicking each tab moves the selection, the panel and the association with it`, async ({ page }) => {
    test.setTimeout(120_000);
    const s = await openStrip(page, route, prefix);
    expectAssociation(s, `${route} click walk (on arrival)`);

    // The landing tab is `active` (the live list — never the bin), then the other
    // one, and back: each click has to move the selection AND leave the
    // association pointing at the panel that is now painted.
    for (const id of [...TAB_IDS.slice(1), TAB_IDS[0]]) {
      await page.getByRole("tab", { name: DICT[labelKeys[id] as keyof typeof DICT].en, exact: true }).click();
      await expect
        .poll(
          async () => {
            const t = await readStrip(page);
            return `${t.selectedText}|${t.controlledIsPainted}`;
          },
          {
            message: `${route}: clicking "${id}" did not move the selection to a tab whose panel is the painted one`,
            timeout: 20_000,
          },
        )
        .toBe(`${DICT[labelKeys[id] as keyof typeof DICT].en}|true`);
      expectAssociation(await readStrip(page), `${route} click walk (on ${id})`);
    }
  });

  test(`${route}: the arrow keys move the selection across the strip and wrap, and Tab still leaves it`, async ({ page }) => {
    test.setTimeout(120_000);
    const s = await openStrip(page, route, prefix);
    expectAssociation(s, `${route} keyboard (on arrival)`);

    // Right from the landing tab (index 0) → the other tab; then Right again →
    // the wrap back to the first. The association is re-read at every step.
    for (const id of [TAB_IDS[1], TAB_IDS[0]]) {
      await page.locator('[role="tab"][aria-selected="true"]').focus();
      await page.keyboard.press("ArrowRight");
      await expect
        .poll(
          async () => {
            const t = await readStrip(page);
            return `${t.selectedText}|${t.controlledIsPainted}|${t.focusedIsTab}`;
          },
          {
            message: `${route}: ArrowRight did not move the selection to "${id}" with the focus on the strip`,
            timeout: 20_000,
          },
        )
        .toBe(`${DICT[labelKeys[id] as keyof typeof DICT].en}|true|true`);
      expectAssociation(await readStrip(page), `${route} keyboard (ArrowRight -> ${id})`);
    }

    // Left goes back the other way.
    await page.keyboard.press("ArrowLeft");
    await expect
      .poll(async () => (await readStrip(page)).selectedText, {
        message: `${route}: ArrowLeft did not move the selection back`,
        timeout: 20_000,
      })
      .toBe(DICT[labelKeys[TAB_IDS[1]] as keyof typeof DICT].en);

    // Home returns to the first tab, and the association follows.
    await page.keyboard.press("Home");
    await expect
      .poll(async () => (await readStrip(page)).selectedText, {
        message: `${route}: Home did not return to the first tab`,
        timeout: 20_000,
      })
      .toBe(DICT[labelKeys[TAB_IDS[0]] as keyof typeof DICT].en);
    expectAssociation(await readStrip(page), `${route} keyboard (Home)`);

    // A key the widget does not own must not be swallowed: Tab has to leave the
    // strip, or a keyboard user is trapped inside it.
    await page.keyboard.press("Tab");
    await expect
      .poll(async () => (await readStrip(page)).focusedIsTab, {
        message: `${route}: pressing Tab did not leave the tab strip — the strip is swallowing the key`,
        timeout: 5_000,
      })
      .toBe(false);
  });
}
