// Regression guard: a FIRST-RUN /settings (a browser with no storage at all, or
// a session whose role hides the stored tab) must still show EXACTLY ONE tab
// carrying `aria-selected`, and the panel below the strip must be THAT tab's.
//
// THE DEFECT (card t_4d32da9a, found while verifying PR 708)
//   `settings/page.tsx` stored the active tab in state (`tab`) and keyed BOTH
//   the strip (`aria-selected={tab === id}`) and the panel branches
//   (`{tab === "chip" && ...}`) on it. `loadStoredTab()` falls back to "chip"
//   (the pre-#163 default), but "chip" is admin-only, so on a first run — where
//   no session has resolved a role yet, or the visitor is a plain member — the
//   strip could not render it: `tab === id` was false for EVERY rendered tab,
//   `aria-selected` was true on none of them, and the CHIP panel was painted
//   underneath anyway. Measured by QA on a browser with no storage, built page,
//   real chromium, at 320/360/430/1024px, identically on origin/main and on the
//   PR-708 merge tree:
//
//     QA-DEFAULT en@320 {"tabCount":10,"selectedCount":0,"selectedText":"",
//       ... "bodyText":"...Workspace\nCHIP Collect\n\nGet your Brand ID &
//       Secret Key from CHIP Merchant Portal -> Developers. Keys ...
//
//   A `role="tab"` strip is expected to carry exactly ONE `aria-selected`, and
//   the panel under it has to be the selected tab's — that is the contract the
//   settings-tabs guard's RULE 5 pins for a settled 12-tab strip, and the
//   first-run state is what a new merchant sees before the session resolves.
//
// THE FIX
//   The page now derives the VIEW from the intent: `activeTab` is `tab` when the
//   role filter renders it, otherwise the first tab the filter DOES render (and
//   `null` when the strip is empty — the `read_only` state). The strip and every
//   panel branch key on `activeTab`.
//
//   The strip also carries `role="tablist"` now. It never did, so the tabs were
//   `role="tab"` children of a plain div — and this file's own axe arm found it,
//   measured, as a CRITICAL `aria-required-parent` violation on the first-run
//   page (axe's nodes were the tab buttons themselves: `.border-slate-900`,
//   `.border-transparent.hover\:text-slate-800.-mb-px`). The repo's other three
//   strips (`/products`, `/media`, `/forms`) already carry the role, so this is
//   the same one-attribute consistency fix inside the very contract this card is
//   about: "exactly one `aria-selected`" is a property OF a tablist, and the page
//   did not have one.
//
// WHY A BROWSER MEASUREMENT, NOT A SOURCE ASSERTION
//   The defect is a property of the RENDERED strip and the RENDERED panel: "no
//   tab carries aria-selected" and "the panel below is not the selected tab's"
//   are both invisible to a class-name or `activeTab` grep — a page that derives
//   the tab correctly but keys one panel branch on `tab` still paints the wrong
//   panel, and only the DOM shows it. So every arm here reads the live DOM.
//
// THE ARMS (each with its own storage state, named in the test title)
//   1. NO STORAGE AT ALL            — the card's case, en @320px.
//   2. THE LOCALE MATRIX            — en/ms/ta/zh/ar @320px, jf_lang only (so the
//      arm proves the dictionary it renders, and `htmlLang`/`dir` are asserted
//      so an arm that silently rendered English cannot pass as Tamil).
//   3. THE STORED-BUT-HIDDEN TAB    — jf_settings_tab = "chip" (a demoted admin,
//      i.e. the stored default that started this card) on a member session.
//   4. THE DEEP LINK TO A HIDDEN TAB — /settings?tab=members on a member session.
//   5. THE read_only MEMBER         — no tabs, so no panel either (the fix's
//      `null` branch; pre-fix this rendered the CHIP credentials panel under the
//      read-only notice, a tab nobody could see).
//   6. THE AXE ARM                  — a scoped scan of the settings pane, with
//      the gate's own rule (critical/serious fail), because the a11y family's
//      public-route scan never sees this page.
//
// WHAT THIS FILE DOES NOT COVER, DELIBERATELY: a tab that IS rendered while its
//   PANEL is gated further down. `workspace` is such a tab for a member —
//   `visibleTabs` keeps it (`#163` filters only `chip`/`members`) while the panel
//   branch is `isAdmin`-gated, so clicking it paints an empty pane. Measured:
//   `{selectedCount:1, selected:"Workspace", h2Count:0, sectionCount:0}`. That is
//   a separate defect (its own card, t_0ef9709c) because it needs a CLICK on a
//   rendered tab, it is a product-visible decision (hide the tab, or render a
//   read-only pane), and the arms here are about the first-run LANDING. When it
//   is fixed by hiding the tab, `ADMIN_ONLY` below has to move with it.
//
// THE SESSION IS STUBBED AT THE BROWSER LEVEL (the pattern `e2e/a11y-dialogs.spec.ts`
// and `e2e/settings-tabs.spec.ts` use): no Go server, no session cookie, no DB —
// the real client component is driven in a real browser and the role under test
// is the one the stubbed bundle carries.
import { test, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import type { AxeResults, Result } from "axe-core";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { DICT } from "../src/components/i18nDict";

const IMPACTS = ["critical", "serious"] as const;
const IMPACT_LABEL = IMPACTS as readonly string[];

function blockersOf(results: AxeResults): Result[] {
  return results.violations.filter((v) => IMPACT_LABEL.includes(v.impact ?? ""));
}

const LANGS = ["en", "ms", "ta", "zh", "ar"] as const;
type Lang = (typeof LANGS)[number];
/** The phone width QA measured the defect at, and where the strip scrolls. */
const PHONE_WIDTH = 320;

// The page's own `TAB_IDS`, read out of its source. Same reason
// `e2e/settings-tabs.spec.ts` reads it rather than importing the page: the page
// is a client component whose module graph needs React, and `TAB_IDS` is the
// declaration this guard is written against. `__dirname` (CommonJS) rather than
// `import.meta.url` — package.json has no `"type": "module"`.
const PAGE_TAB_IDS = readPageTabIds();

function readPageTabIds(): string[] {
  const src = readFileSync(join(__dirname, "../src/app/settings/page.tsx"), "utf8");
  const decl = src.match(/const TAB_IDS[^=]*=\s*\[([^\]]*)\]/);
  if (!decl) throw new Error("settings/page.tsx no longer declares TAB_IDS — this guard reads it to derive the tabs a member sees");
  return [...decl[1].matchAll(/"([^"]+)"/g)].map((m) => m[1]);
}

// The tabs `#163` hides from a member: CHIP credentials and member management
// are admin-only. This is the PAGE's own filter (its `visibleTabs`), which is
// what makes the derived list below the set a member really sees.
const ADMIN_ONLY = ["chip", "members"];
const MEMBER_TAB_IDS = PAGE_TAB_IDS.filter((id) => !ADMIN_ONLY.includes(id));
// The tab the first run has to land on: the first one the strip can render.
const LANDING_TAB = MEMBER_TAB_IDS[0];

// tab id -> the dictionary key of the panel heading that tab paints. Listed per
// tab because the LANDING is not pinned to one: the stored tab and the URL can
// move it, and the assertion below is "the panel is THIS tab's", which needs the
// expected heading whichever tab wins. `workspace` is absent on purpose: the
// panel behind it is a component gated on `isAdmin` (`WorkspaceTab`), and its
// heading is not a settings dictionary string.
const PANEL_TITLE: Record<string, keyof typeof DICT> = {
  chip: "set.chipTitle",
  domains: "set.domainsTitle",
  tracking: "set.trackingTitle",
  einvoice: "set.einvoiceTitle",
  webhooks: "set.webhooksTitle",
  emel: "set.emelTitle",
  storefront: "set.sfTitle",
  legal: "set.lpTitle",
  currency: "set.currencyTitle",
  affiliates: "set.affiliatesTitle",
};

// The derived lists have to describe the page. Fail BEFORE a browser starts, and
// name the tab, so a page that gained a tab is not reported as a broken arm.
{
  if (MEMBER_TAB_IDS.length < 2) {
    throw new Error(`settings/page.tsx declares ${PAGE_TAB_IDS.length} tabs, ${MEMBER_TAB_IDS.length} of them visible to a member — this guard asserts a first-run landing tab`);
  }
  for (const id of MEMBER_TAB_IDS) {
    const label = `set.tab.${id}`;
    if (!Object.prototype.hasOwnProperty.call(DICT, label)) {
      throw new Error(`settings/page.tsx declares the tab "${id}", which is not in the dictionary (${label})`);
    }
    if (id === "workspace") continue; // component panel, no settings title
    if (!PANEL_TITLE[id]) {
      throw new Error(
        `settings/page.tsx declares the tab "${id}" and this guard has no panel heading for it. ` +
          `Add its title key to PANEL_TITLE so "the panel below matches the selected tab" stays checked.`,
      );
    }
    if (!Object.prototype.hasOwnProperty.call(DICT, PANEL_TITLE[id])) {
      throw new Error(`PANEL_TITLE maps "${id}" to ${PANEL_TITLE[id]}, which is not in the dictionary`);
    }
  }
}

const ME = { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "", is_super_admin: false };

/**
 * The bundle a MEMBER sees: `role: "manage"` is not admin, so the two admin-only
 * tabs are filtered out — the state the card measured (ten tabs, no selection,
 * CHIP panel). `is_super_admin: false` so nothing else in the app answers as an
 * admin either.
 */
const MEMBER_BUNDLE = {
  me: ME,
  workspaces: [{ id: "ws1", name: "Demo", slug: "demo", role: "manage" }],
  chip_status: null, keys: [], domains: [], webhooks: [], social: [], einvoice: null,
  products: [], summary: { counts: {}, revenue_sen: 0 },
  sales: { items: [], page: 1, limit: 20, total: 0 },
  form_analytics: [], product_analytics: [],
};

/** The bundle a read_only member sees: no write tabs at all (#163). */
const READ_ONLY_BUNDLE = {
  ...MEMBER_BUNDLE,
  workspaces: [{ id: "ws1", name: "Demo", slug: "demo", role: "read_only" }],
};

async function stubAPI(page: Page, bundle: unknown) {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  // Catch-all FIRST, specific routes after it: Playwright resolves matching
  // routes in reverse registration order, so the later (specific) ones win.
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(bundle)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(ME)));
  // lang:"" (never set) keeps the browser value in effect, which is how the
  // locale arms pin their locale to `jf_lang` instead of the profile.
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "" })));
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/affiliates(\?.*)?$/, (r) => r.fulfill(json([])));
  await page.route(/(.*)\/api\/rest\/currency\/rates$/, (r) => r.fulfill(json({ rates: [], cached: false })));
}

/**
 * A FRESH browser state per case. `storage` is what this arm puts in
 * localStorage AFTER clearing it — the browser keeps storage between runs, so a
 * `jf_lang` / `jf_settings_tab` left behind by an earlier card would render a
 * state no new visitor has (that has already produced a false reading on this
 * board). An empty object is the card's "no storage at all" case.
 */
async function freshState(page: Page, storage: Record<string, string>) {
  await page.addInitScript((kv) => {
    try {
      localStorage.clear();
      sessionStorage.clear();
      for (const [k, v] of Object.entries(kv)) localStorage.setItem(k, v);
    } catch {
      /* storage unavailable */
    }
  }, storage);
}

type TabState = {
  tabCount: number;
  labels: string[];
  selectedCount: number;
  selectedText: string;
  selectedInView: boolean;
  firstPanelHeading: string;
  htmlLang: string;
  dir: string;
  hasChipHeading: boolean;
  hasReadOnlyNote: boolean;
};

/**
 * One read of the live DOM: the strip's tabs, which (if any) is selected, the
 * panel heading painted first under the strip, and the document language.
 */
async function readTabState(page: Page, lang: Lang): Promise<TabState> {
  // The two strings this arm looks for, in the locale it pinned: the CHIP
  // panel heading (the panel that must NOT be painted while no CHIP tab is) and
  // the read-only notice (the marker that a read_only role landed).
  const chipTitle = JSON.stringify(DICT["set.chipTitle"][lang]);
  const readOnlyNote = JSON.stringify(DICT["mem.readOnlyNote"][lang]);
  return page.evaluate(`(() => {
    const tabs = [...document.querySelectorAll('[role="tab"]')];
    const sel = tabs.filter((t) => t.getAttribute("aria-selected") === "true");
    const label = (t) => {
      const spans = [...t.querySelectorAll("span")];
      const vis = spans.find((s) => getComputedStyle(s).display !== "none");
      return ((vis ? vis.textContent : t.textContent) || "").trim();
    };
    // The panel heading: the FIRST h2 under the page's main. The strip is a
    // sibling above every panel, the page's own title is an h1, and each panel
    // opens with its own h2 — so the first h2 in main is the rendered panel's.
    const main = document.querySelector("main");
    const headings = main ? [...main.querySelectorAll("h2")] : [];
    const strip = tabs.length ? tabs[0].parentElement : null;
    const sr = strip ? strip.getBoundingClientRect() : null;
    const sb = sel.length ? sel[0].getBoundingClientRect() : null;
    return {
      tabCount: tabs.length,
      labels: tabs.map(label),
      selectedCount: sel.length,
      selectedText: sel.length ? label(sel[0]) : "",
      selectedInView: !!(sr && sb && sb.left >= sr.left - 1 && sb.right <= sr.right + 1),
      firstPanelHeading: headings.length ? (headings[0].textContent || "").trim() : "",
      htmlLang: document.documentElement.lang,
      dir: document.documentElement.dir,
      hasChipHeading: headings.some((h) => (h.textContent || "").trim() === ${chipTitle}),
      hasReadOnlyNote: (main ? main.innerText : "").includes(${readOnlyNote}),
    };
  })()`) as Promise<TabState>;
}

/**
 * Waits until the strip renders the member's tab set (the session bundle
 * resolves `role` asynchronously, and the pre-bundle strip is a different one),
 * then returns the DOM read. A bundle that never resolves fails HERE, naming the
 * session rather than reporting a broken strip.
 */
async function settleAndRead(page: Page, lang: Lang): Promise<TabState> {
  const settle = await page.evaluate(`(async () => {
    const want = ${MEMBER_TAB_IDS.length};
    const first = ${JSON.stringify(DICT[`set.tab.${LANDING_TAB}` as keyof typeof DICT][lang])};
    // The tab's label is its VISIBLE span: the tab renders two spans
    // (sm:hidden + hidden sm:inline, one per layout), so textContent is the
    // doubled string "DomainsDomains" — comparing that against the dictionary
    // would make this wait hang on a strip that is rendering correctly.
    const label = (t) => {
      const spans = [...t.querySelectorAll("span")];
      const vis = spans.find((s) => getComputedStyle(s).display !== "none");
      return ((vis ? vis.textContent : t.textContent) || "").trim();
    };
    const deadline = Date.now() + 20000;
    let count = 0;
    while (Date.now() < deadline) {
      const tabs = [...document.querySelectorAll('[role="tab"]')];
      count = tabs.length;
      const texts = tabs.map(label);
      if (count === want && texts.includes(first)) return { count, timedOut: false };
      await new Promise((r) => setTimeout(r, 50));
    }
    return { count, timedOut: true };
  })()`) as { count: number; timedOut: boolean };
  expect(
    settle.timedOut,
    `the strip never rendered the ${MEMBER_TAB_IDS.length} tabs a member sees ` +
      `(${MEMBER_TAB_IDS.join(", ")}) — it settled at ${settle.count}. The stubbed ` +
      `dashboard-bundle did not resolve role "manage" for the active workspace.`,
  ).toBe(false);
  return readTabState(page, lang);
}

/**
 * The card's acceptance, in one place: exactly one selected tab, it is the tab
 * the first run must land on, and the panel under the strip is that tab's.
 */
function expectOneSelectionMatchesPanel(s: TabState, lang: Lang, what: string) {
  expect(
    s.selectedCount,
    `${what}: ${s.selectedCount} tabs carry aria-selected on a role="tab" strip ` +
      `(${s.tabCount} tabs, labels ${JSON.stringify(s.labels)}) — a first run must ` +
      `land on a tab that IS rendered, so the strip has exactly one`,
  ).toBe(1);
  expect(
    s.selectedText,
    `${what}: the selected tab is ${JSON.stringify(s.selectedText)}, not the first tab a ` +
      `member is shown (${JSON.stringify(DICT[`set.tab.${LANDING_TAB}` as keyof typeof DICT][lang])})`,
  ).toBe(DICT[`set.tab.${LANDING_TAB}` as keyof typeof DICT][lang]);
  expect(
    s.firstPanelHeading,
    `${what}: the panel under the strip is ${JSON.stringify(s.firstPanelHeading)}, which is not ` +
      `the selected tab's (${JSON.stringify(DICT[PANEL_TITLE[LANDING_TAB]][lang])})`,
  ).toBe(DICT[PANEL_TITLE[LANDING_TAB]][lang]);
  expect(
    s.hasChipHeading,
    `${what}: the CHIP panel is painted while no CHIP tab is in this strip`,
  ).toBe(false);
}

test("first run, NO storage at all: exactly one aria-selected tab and its panel (en @320)", async ({ page, baseURL }) => {
  await freshState(page, {});
  await stubAPI(page, MEMBER_BUNDLE);
  await page.setViewportSize({ width: PHONE_WIDTH, height: 800 });
  await page.goto(`${baseURL}/settings`, { waitUntil: "domcontentloaded" });

  const s = await settleAndRead(page, "en");
  expect(s.tabCount, `the member strip renders ${MEMBER_TAB_IDS.length} tabs`).toBe(MEMBER_TAB_IDS.length);
  expectOneSelectionMatchesPanel(s, "en", "en@320 no-storage");
  expect(s.htmlLang, "en@320 no-storage: the document language").toBe("en");

  // The reveal is not a no-op when there IS a selection: the selected tab has to
  // be inside the strip's visible window (the mount reveal is an effect, so this
  // is a bounded read of the painted frame, not a one-shot).
  const inView = await page.evaluate(`(async () => {
    const deadline = Date.now() + 5000;
    let saw = false;
    while (Date.now() < deadline) {
      const tabs = [...document.querySelectorAll('[role="tab"]')];
      const strip = tabs.length ? tabs[0].parentElement : null;
      const sel = tabs.find((t) => t.getAttribute("aria-selected") === "true");
      if (strip && sel) {
        const sr = strip.getBoundingClientRect();
        const sb = sel.getBoundingClientRect();
        saw = sb.left >= sr.left - 1 && sb.right <= sr.right + 1;
        if (saw) return true;
      }
      await new Promise((r) => requestAnimationFrame(() => r(null)));
    }
    return saw;
  })()`) as boolean;
  expect(inView, "en@320 no-storage: the selected tab is scrolled out of the strip's visible window").toBe(true);
});

for (const lang of LANGS) {
  test(`first run, jf_lang only: one aria-selected tab and its panel (${lang} @320)`, async ({ page, baseURL }) => {
    // Only the LANGUAGE is preset — still no stored tab, which is the state the
    // fallback ("chip") has to survive. The locale is asserted below so an arm
    // that quietly rendered English cannot pass as Tamil.
    await freshState(page, { jf_lang: lang, jf_workspace: "demo" });
    await stubAPI(page, MEMBER_BUNDLE);
    await page.setViewportSize({ width: PHONE_WIDTH, height: 800 });
    await page.goto(`${baseURL}/settings`, { waitUntil: "domcontentloaded" });

    const s = await settleAndRead(page, lang);
    expect(s.htmlLang, `${lang}@320: the document language is not the one this arm pinned`).toBe(lang);
    // RTL is the direction a reader would notice: `dir` is only asserted where
    // the page must change it (a bare `dir=""` on the ltr arms is the absence of
    // an attribute, not a defect, and the label assertion below is what proves
    // the locale actually rendered).
    if (lang === "ar") {
      expect(s.dir, "ar@320: the document direction").toBe("rtl");
    }
    expect(s.labels, `${lang}@320: the strip is not rendering THIS locale's labels`).toEqual(
      MEMBER_TAB_IDS.map((id) => DICT[`set.tab.${id}` as keyof typeof DICT][lang]),
    );
    expectOneSelectionMatchesPanel(s, lang, `${lang}@320 jf_lang-only`);
  });
}

test("stored tab the role filter hides ('chip', a demoted admin): lands on a rendered tab (en @320)", async ({ page, baseURL }) => {
  await freshState(page, { jf_lang: "en", jf_workspace: "demo", jf_settings_tab: "chip" });
  await stubAPI(page, MEMBER_BUNDLE);
  await page.setViewportSize({ width: PHONE_WIDTH, height: 800 });
  await page.goto(`${baseURL}/settings`, { waitUntil: "domcontentloaded" });

  const s = await settleAndRead(page, "en");
  expectOneSelectionMatchesPanel(s, "en", "en@320 stored-chip-on-a-member");
});

test("deep link to a tab the role filter hides (?tab=members): lands on a rendered tab (en @320)", async ({ page, baseURL }) => {
  await freshState(page, { jf_lang: "en", jf_workspace: "demo", jf_settings_tab: "domains" });
  await stubAPI(page, MEMBER_BUNDLE);
  await page.setViewportSize({ width: PHONE_WIDTH, height: 800 });
  await page.goto(`${baseURL}/settings?tab=members`, { waitUntil: "domcontentloaded" });

  const s = await settleAndRead(page, "en");
  expectOneSelectionMatchesPanel(s, "en", "en@320 deep-link-to-members");
});

test("read_only member: no tabs, so no tab panel either (en @320)", async ({ page, baseURL }) => {
  await freshState(page, { jf_lang: "en", jf_workspace: "demo" });
  await stubAPI(page, READ_ONLY_BUNDLE);
  await page.setViewportSize({ width: PHONE_WIDTH, height: 800 });
  await page.goto(`${baseURL}/settings`, { waitUntil: "domcontentloaded" });

  // Wait for the read-only notice, which is what proves the role landed (the
  // strip is absent in this state, so there is no tab to wait for).
  await expect(page.getByText(DICT["mem.readOnlyNote"].en)).toBeVisible({ timeout: 45_000 });
  const s = await readTabState(page, "en");
  expect(s.tabCount, "read_only: the strip is not rendered at all for a read_only member (#163)").toBe(0);
  expect(s.hasReadOnlyNote, "read_only: the read-only notice is on screen, so the role DID land").toBe(true);
  expect(
    s.firstPanelHeading,
    `read_only: a panel (${JSON.stringify(s.firstPanelHeading)}) is painted under a strip that renders no tab`,
  ).toBe("");
  expect(s.hasChipHeading, "read_only: the admin-only CHIP panel is painted for a member who cannot see its tab").toBe(false);
});

test("axe: the settings pane on a first run has no critical/serious violation (en @320)", async ({ page, baseURL }) => {
  await freshState(page, {});
  await stubAPI(page, MEMBER_BUNDLE);
  await page.setViewportSize({ width: PHONE_WIDTH, height: 800 });
  await page.goto(`${baseURL}/settings`, { waitUntil: "domcontentloaded" });

  const s = await settleAndRead(page, "en");
  expectOneSelectionMatchesPanel(s, "en", "en@320 axe arm");

  const results = await new AxeBuilder({ page }).include("main").analyze();
  const blockers = blockersOf(results);
  if (blockers.length > 0) {
    for (const v of blockers) {
      console.log(`  [${v.impact}] ${v.id}: ${v.help}`);
      for (const n of v.nodes.slice(0, 5)) console.log(`      ${n.target.join(" ")}`);
    }
  }
  expect(
    blockers.map((v) => v.id),
    `the first-run settings pane has ${blockers.length} critical/serious axe violation(s)`,
  ).toEqual([]);
});
