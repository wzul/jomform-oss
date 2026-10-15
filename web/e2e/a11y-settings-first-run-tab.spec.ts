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
//   7. EVERY TAB A MEMBER SEES      — clicks each one and requires its own pane
//      (t_0ef9709c: no dead-end tab; pre-fix it fails on the tenth, Workspace).
//   8. THE ADMIN'S WORKSPACE TAB    — the other direction: the tab must still be
//      offered to the role whose panel it IS, and paint the identity card.
//   9. THE ASSOCIATION (card t_40253ae6) — every arm above now ALSO reads the ARIA
//      tabs association off the live DOM: each tab carries `aria-controls`, the
//      SELECTED tab's resolves to the element that is actually painted, that panel
//      is a `role="tabpanel"` labelled by its own tab, and the strip is one tab
//      stop (roving `tabIndex`). It was 0 attributes of either kind before the
//      card, and axe reports nothing for the omission — so this is read, not
//      scanned.
//  10. THE ARROW KEYS               — Left/Right/Home/End move the selection, the
//      focus AND the painted panel across the member strip (with wraparound), the
//      moved-to tab is revealed into the scroller, and Tab still LEAVES the strip.
//  11. THE ADMIN'S FIRST-RUN LANDING — NO stored tab on an ADMIN session: the
//      default intent (`chip`) has to survive the first paint, where the session
//      bundle has not resolved `role` and the strip cannot render it yet, so the
//      admin lands on CHIP with the CHIP panel. This is the arm the ten above
//      CANNOT be: every one of them expects the first tab a MEMBER is shown,
//      which is exactly what clamping the intent produces — see "WHY THIS ARM
//      EXISTS" below the admin arm.
//
// THE DEAD-END TAB (card t_0ef9709c, the mirror image of the defect above)
//   A tab that IS rendered while its PANEL is gated further down. `workspace`
//   was such a tab for a member: `visibleTabs` filtered only `chip`/`members`
//   (#163) while the panel branch was `{activeTab === "workspace" && isAdmin &&
//   …}`, so clicking Workspace selected a tab and painted an EMPTY pane.
//   Measured on the built page as a `manage` member at 320px, pre-fix:
//
//     after clicking Workspace {"tabCount":10,"selectedCount":1,
//       "selected":"Workspace","firstPanelHeading":"","h2Count":0,
//       "sectionCount":0,"mainTextLen":105}
//
//   (`h2Count 0` is the pane — the h1 and the banners only.) The fix hides the
//   tab from the role whose panel is gated, so the strip offers exactly the tabs
//   it can paint; ADMIN_ONLY below moves with the page's filter, and the
//   click-through and admin arms at the bottom are what keep the two sides in
//   step. That is the same contract this file's first defect is about — the
//   strip and the panel agree on which tabs exist — from the other side.
//
// THE SESSION IS STUBBED AT THE BROWSER LEVEL (the pattern `e2e/a11y-dialogs.spec.ts`
// and `e2e/settings-tabs.spec.ts` use): no Go server, no session cookie, no DB —
// the real client component is driven in a real browser and the role under test
// is the one the stubbed bundle carries.
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

// The tabs the PAGE's own role filter hides from a member (`visibleTabs`): CHIP
// credentials and member management (#163), and — since card t_0ef9709c — the
// Workspace tab, whose PANEL is `isAdmin`-gated, so a member's strip offered a
// tab that painted an EMPTY pane when it was clicked. This list IS the page's
// admin-only list; it is the one place the member tab set is derived, so if the
// page's filter moves, the arms below move with it (and their failure messages
// carry the tab list that moved).
const ADMIN_ONLY = ["chip", "members", "workspace"];
const MEMBER_TAB_IDS = PAGE_TAB_IDS.filter((id) => !ADMIN_ONLY.includes(id));
// The tab the first run has to land on: the first one the strip can render.
const LANDING_TAB = MEMBER_TAB_IDS[0];

// tab id -> the dictionary key of the panel heading that tab paints. Listed per
// tab because the LANDING is not pinned to one: the stored tab and the URL can
// move it, and the assertion below is "the panel is THIS tab's", which needs the
// expected heading whichever tab wins. `workspace` is the component panel
// (`WorkspaceTab`, admin/owner-gated), whose first heading is its identity card
// rather than a settings-tab title; a member never reaches it (see ADMIN_ONLY),
// so it is listed for the ADMIN arms.
const PANEL_TITLE: Record<string, keyof typeof DICT> = {
  chip: "set.chipTitle",
  domains: "set.domainsTitle",
  tracking: "set.trackingTitle",
  einvoice: "set.einvoiceTitle",
  webhooks: "set.webhooksTitle",
  emel: "set.emelTitle",
  storefront: "set.sfTitle",
  legal: "set.legalTitle",
  currency: "set.currencyTitle",
  affiliates: "set.affiliatesTitle",
  workspace: "ws.identityTitle",
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
 * The bundle a MEMBER sees: `role: "manage"` is not admin, so the three
 * admin-only tabs are filtered out — the state the original card measured (no
 * selection, CHIP panel). `is_super_admin: false` so nothing else in the app
 * answers as an admin either. The strip is NINE tabs since t_0ef9709c hid
 * `workspace` from this role (it was ten).
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

/**
 * The bundle an ADMIN member sees: every declared tab, `workspace` included.
 * Used by the arm that pins the other direction of t_0ef9709c — hiding the
 * Workspace tab from a member must not hide it from the role whose panel it IS
 * — and by the arm that pins the admin's FIRST-RUN landing (card t_50d602cf).
 * `is_super_admin: true` as well as the admin role: the merchant whose stored
 * tab is CHIP in production is a super-admin, and the two gates are read in
 * different places (`role` from the workspace row filters the strip;
 * `is_super_admin` adds the platform /admin nav entry in `Shell`), so the
 * bundle carries both — a bundle that answered admin to one and member to the
 * other would not be the session this arm means to pin.
 */
const ADMIN_BUNDLE = {
  ...MEMBER_BUNDLE,
  me: { ...ME, is_super_admin: true },
  workspaces: [{ id: "ws1", name: "Demo", slug: "demo", role: "admin" }],
};

/** GET /v1/workspaces/{id}/deletion-check — what WorkspaceTab's identity card reads. */
const DELETION_CHECK = {
  id: "ws1", name: "Demo", slug: "demo", is_owner: true, role: "admin",
  last_workspace: false, empty: true, blockers: [], can_delete: true,
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
  // WorkspaceTab (admin/owner roles only) reads these two on mount; without a
  // route the catch-all answers `{}` and its identity card would report a load
  // failure instead of painting the heading the admin arm asserts.
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/deletion-check$/, (r) => r.fulfill(json(DELETION_CHECK)));
  await page.route(/(.*)\/api\/workspaces\/trash(\?.*)?$/, (r) => r.fulfill(json([])));
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
  // ── the ARIA tabs ASSOCIATION (card t_40253ae6) ──────────────────────────
  // The same reads `e2e/a11y-profile-tabs.spec.ts` takes, so the two strips are
  // judged by one contract: a tab's `aria-controls` is RESOLVED against the
  // document and compared with the panel that is actually PAINTED, the panel
  // points back at its own tab, and the strip is one tab stop.
  tabsWithoutControls: string[];
  tabIdsWithoutId: number;
  selectedId: string;
  selectedControls: string;
  controlledId: string;
  controlledRole: string;
  controlledIsPaintedPanel: boolean;
  panelLabelledBy: string;
  tabpanelCount: number;
  tabStops: number;
  tabStopIsSelected: boolean;
  focusedIsTab: boolean;
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
    // The ASSOCIATION (card t_40253ae6): the selected tab's aria-controls is
    // RESOLVED against the document — never string-compared against an expected
    // id, which would pass on a panel that is not in the document — and the
    // element it names is compared with the panel that is actually PAINTED (the
    // [role="tabpanel"] holding the first h2 of main).
    const selTab = sel.length ? sel[0] : null;
    const controlsAttr = selTab ? (selTab.getAttribute("aria-controls") || "") : "";
    const controlled = controlsAttr ? document.getElementById(controlsAttr) : null;
    const painted = headings.length ? headings[0].closest('[role="tabpanel"]') : null;
    const ids = tabs.map((t) => t.id);
    const stops = tabs.filter((t) => t.getAttribute("tabindex") === "0");
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
      tabsWithoutControls: tabs.filter((t) => !t.getAttribute("aria-controls")).map(label),
      tabIdsWithoutId: ids.filter((id) => !id).length,
      selectedId: selTab ? selTab.id : "",
      selectedControls: controlsAttr,
      controlledId: controlled ? controlled.id : "",
      controlledRole: controlled ? (controlled.getAttribute("role") || "") : "",
      controlledIsPaintedPanel: !!controlled && !!painted && controlled === painted,
      panelLabelledBy: controlled ? (controlled.getAttribute("aria-labelledby") || "") : "",
      tabpanelCount: document.querySelectorAll('[role="tabpanel"]').length,
      tabStops: stops.length,
      tabStopIsSelected: stops.length === 1 && !!selTab && stops[0] === selTab,
      focusedIsTab: !!document.activeElement && document.activeElement.getAttribute("role") === "tab",
    };
  })()`) as Promise<TabState>;
}

/**
 * Waits until the strip renders the tab set under test (the session bundle
 * resolves `role` asynchronously, and the pre-bundle strip is a different one),
 * then returns the DOM read. A bundle that never resolves fails HERE, naming the
 * session rather than reporting a broken strip.
 *
 * `wantIds` is the set the arm expects, so the ADMIN arm waits for the admin set
 * instead of settling on the nine tabs a member's strip carries.
 */
async function settleAndRead(
  page: Page,
  lang: Lang,
  wantIds: readonly string[] = MEMBER_TAB_IDS,
  landingTab: string = LANDING_TAB,
): Promise<TabState> {
  const settle = await page.evaluate(`(async () => {
    const want = ${wantIds.length};
    const first = ${JSON.stringify(DICT[`set.tab.${landingTab}` as keyof typeof DICT][lang])};
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
    `the strip never rendered the ${wantIds.length} tabs this arm expects ` +
      `(${wantIds.join(", ")}) — it settled at ${settle.count}. Either the stubbed ` +
      `dashboard-bundle did not resolve the role for the active workspace, or the page ` +
      `renders a tab set this file does not expect (its ADMIN_ONLY list and the page's own ` +
      `role filter have to agree).`,
  ).toBe(false);
  return readTabState(page, lang);
}

/**
 * Bounded read of the PAINTED strip: returns once the strip has tabs, one of
 * them is selected, and the pane below has painted a heading — i.e. once the
 * page is in the state the assertions are about. Returns the last read either
 * way, so a strip that never paints fails on the caller's OWN assertion (with
 * the DOM in the message) rather than on a wait.
 *
 * Deliberately NOT `settleAndRead`'s "the strip holds exactly N tabs" wait: this
 * arm has to READ a strip that renders the WRONG tab set, because "the member
 * strip offers a tab whose panel is not rendered" is the assertion. A wait that
 * refused to return on an unexpected set would report the defect as a broken
 * session. The arm is safe to read an early frame because it waits for the
 * `dashboard-bundle` response first — see the arm.
 */
async function waitForStrip(page: Page, lang: Lang): Promise<TabState> {
  const deadline = Date.now() + 10_000;
  let s = await readTabState(page, lang);
  while (Date.now() < deadline) {
    if (s.tabCount > 0 && s.selectedCount === 1 && s.firstPanelHeading !== "") return s;
    await page.waitForTimeout(100);
    s = await readTabState(page, lang);
  }
  return s;
}

/**
 * Clicks a tab and waits (bounded) for the pane below to paint THAT tab's own
 * heading. Returns the last read either way, so a pane that never paints a
 * heading fails on the caller's assertion with the real DOM in the message
 * instead of hanging here.
 */
async function clickTabAndReadPanel(page: Page, id: string, lang: Lang): Promise<TabState> {
  const label = DICT[`set.tab.${id}` as keyof typeof DICT][lang];
  const want = DICT[PANEL_TITLE[id]][lang];
  await page.getByRole("tab", { name: label, exact: true }).click();
  const deadline = Date.now() + 10_000;
  let s = await readTabState(page, lang);
  while (Date.now() < deadline) {
    if (s.selectedText === label && s.firstPanelHeading === want) return s;
    await page.waitForTimeout(100);
    s = await readTabState(page, lang);
  }
  return s;
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
  expectSettingsAssociation(s, lang, what);
}

/**
 * The ARIA tabs ASSOCIATION on the settings strip (card t_40253ae6).
 *
 * The same contract `e2e/a11y-profile-tabs.spec.ts` asserts, on a strip whose tab
 * set is FILTERED by role and whose panels are twelve branches (five of them
 * wrapped here, seven carrying the attributes on their own `<section>`). Before
 * the card no tab carried `aria-controls` and no element in `src/` carried
 * `role="tabpanel"`; axe reports nothing for the omission, so the only way to see
 * it is to read the association out of the document — which is what this does,
 * resolving the idref rather than comparing strings, and comparing the resolved
 * element with the panel that is actually painted.
 */
function expectSettingsAssociation(s: TabState, lang: Lang, what: string) {
  expect(
    s.tabsWithoutControls,
    `${what}: ${s.tabsWithoutControls.length} of ${s.tabCount} role="tab" nodes carry NO aria-controls ` +
      `(${JSON.stringify(s.tabsWithoutControls)}) — no tab is associated with the panel it controls, and ` +
      `axe does not report it either way`,
  ).toEqual([]);
  expect(s.tabIdsWithoutId, `${what}: ${s.tabIdsWithoutId} tab(s) have no id for a panel to reference`).toBe(0);
  expect(
    s.controlledRole,
    `${what}: the selected tab (${JSON.stringify(s.selectedText)}) names ` +
      `${JSON.stringify(s.selectedControls)}, which does not resolve to a role="tabpanel" element — the ` +
      `association points at nothing, or at something that is not a panel`,
  ).toBe("tabpanel");
  expect(
    s.controlledIsPaintedPanel,
    `${what}: the panel the selected tab controls (#${s.controlledId}) is NOT the panel painted under the ` +
      `strip (the one holding the first h2 of main)`,
  ).toBe(true);
  expect(
    s.panelLabelledBy,
    `${what}: the panel #${s.controlledId} is labelled by ${JSON.stringify(s.panelLabelledBy)}, not by its ` +
      `own tab (#${s.selectedId})`,
  ).toBe(s.selectedId);
  expect(
    s.tabpanelCount,
    `${what}: ${s.tabpanelCount} role="tabpanel" elements are rendered — the strip paints exactly one panel`,
  ).toBe(1);
  expect(
    s.tabStops,
    `${what}: ${s.tabStops} tab(s) carry tabindex="0" — the strip is ONE tab stop (roving tabindex)`,
  ).toBe(1);
  expect(
    s.tabStopIsSelected,
    `${what}: the strip's single tab stop is not the selected tab (${JSON.stringify(s.selectedText)})`,
  ).toBe(true);
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

// ─── The DEAD-END TAB (card t_0ef9709c) ────────────────────────────────────────
//
// A tab that IS rendered while its PANEL is gated further down. `workspace` was
// such a tab for a member: `#163`'s filter hid only `chip`/`members` while the
// panel branch was `{activeTab === "workspace" && isAdmin && …}`, so clicking
// Workspace selected a tab and painted an EMPTY pane. Measured pre-fix on the
// built page as a `manage` member at 320px:
//
//   {"tabCount":10,"selectedCount":1,"selected":"Workspace",
//    "firstPanelHeading":"","h2Count":0,"sectionCount":0,"mainTextLen":105}
//
// So this arm does not only count tabs — it CLICKS every tab the strip offers and
// requires the pane under it to be that tab's. A tab whose panel is not rendered
// is the defect, whichever of the two sides drifts first.
test("member: every tab the strip offers paints its own pane — no dead-end tab (en @320)", async ({ page, baseURL }) => {
  await freshState(page, { jf_lang: "en", jf_workspace: "demo" });
  // The strip changes when the session bundle lands (`role` arrives with it), so
  // the read below waits for THAT response rather than for a tab count: on the
  // pre-fix page the pre-bundle strip is a nine-tab set that would satisfy a
  // count wait and hide the defect.
  const bundled = page.waitForResponse((r) => /dashboard-bundle/.test(r.url()));
  await stubAPI(page, MEMBER_BUNDLE);
  await page.setViewportSize({ width: PHONE_WIDTH, height: 800 });
  await page.goto(`${baseURL}/settings`, { waitUntil: "domcontentloaded" });
  await bundled;

  const s = await waitForStrip(page, "en");

  // The admin-only tabs, named: this is the assertion the card's defect fails.
  for (const id of ADMIN_ONLY) {
    expect(
      s.labels,
      `the member strip offers the admin-only tab "${id}" ` +
        `(${JSON.stringify(DICT[`set.tab.${id}` as keyof typeof DICT].en)}) — its panel is not rendered for ` +
        `this role, so clicking it paints an empty pane. Labels: ${JSON.stringify(s.labels)}`,
    ).not.toContain(DICT[`set.tab.${id}` as keyof typeof DICT].en);
  }
  expect(
    s.tabCount,
    `the member strip renders ${MEMBER_TAB_IDS.length} tabs (${MEMBER_TAB_IDS.join(", ")})`,
  ).toBe(MEMBER_TAB_IDS.length);

  for (const id of MEMBER_TAB_IDS) {
    const pane = await clickTabAndReadPanel(page, id, "en");
    expect(
      pane.firstPanelHeading,
      `clicking the "${id}" tab paints a pane with no heading of its own ` +
        `(the pane's first h2 reads ${JSON.stringify(pane.firstPanelHeading)}) — the strip offers a tab whose ` +
        `panel the page does not render`,
    ).toBe(DICT[PANEL_TITLE[id]]["en"]);
  }
});

// The OTHER direction: hiding a tab is only right for the role whose panel is
// gated. An admin must still get the Workspace tab — otherwise "no dead-end tab"
// would quietly take the identity card away from the owners who need it.
test("admin: the Workspace tab is still offered and its pane is the identity card (en @320)", async ({ page, baseURL }) => {
  await freshState(page, { jf_lang: "en", jf_workspace: "demo" });
  await stubAPI(page, ADMIN_BUNDLE);
  await page.setViewportSize({ width: PHONE_WIDTH, height: 800 });
  await page.goto(`${baseURL}/settings`, { waitUntil: "domcontentloaded" });

  const s = await settleAndRead(page, "en", PAGE_TAB_IDS, PAGE_TAB_IDS[0]);
  expect(
    s.labels.map((l) => l),
    `the admin strip does not render every declared tab (${PAGE_TAB_IDS.join(", ")})`,
  ).toEqual(PAGE_TAB_IDS.map((id) => DICT[`set.tab.${id}` as keyof typeof DICT].en));

  const pane = await clickTabAndReadPanel(page, "workspace", "en");
  expect(
    pane.selectedText,
    `the admin strip's Workspace tab does not select when clicked`,
  ).toBe(DICT["set.tab.workspace"].en);
  expect(
    pane.firstPanelHeading,
    `the admin's Workspace pane paints ${JSON.stringify(pane.firstPanelHeading)}, not the workspace identity card`,
  ).toBe(DICT["ws.identityTitle"].en);
});

// ─── THE ADMIN'S FIRST-RUN LANDING (card t_50d602cf) ───────────────────────────
//
// WHY THIS ARM EXISTS
//   `settings/page.tsx` deliberately does NOT clamp the tab INTENT. `tab` is the
//   stored / URL / clicked tab and it is left alone ("`tab` itself is left alone
//   on purpose: it is the merchant's intent, the persist effect below writes it,
//   and an admin's stored "chip" must survive a first paint that has not
//   resolved `role` yet"), while the VIEW is derived from it:
//   `activeTab = visibleTabs.includes(tab) ? tab : (visibleTabs[0] ?? null)`.
//
//   Nothing pinned that. Every arm above expects the landing `MEMBER_TAB_IDS[0]`
//   ("Domains") — which is EXACTLY what a clamp produces — so a page that clamps
//   the intent at initialisation instead of deriving the view passes all of
//   them: on the first paint `role` is unresolved, so `visibleTabs` is the member
//   set, an admin's default `chip` is not in it, the clamp rewrites `tab` to
//   "domains" AND the persist effect writes that back — so once `role` resolves
//   the admin is on Domains instead of CHIP, with the CHIP credentials one click
//   away from a merchant who never asked to be moved. The arm below is the only
//   one that can see it, because it is the only one whose landing is NOT the
//   first tab a member is shown. (The sibling `e2e/workspace-trash.spec.ts`
//   preselects `jf_settings_tab = "workspace"` on an admin bundle, but it waits
//   only for the admin strip to render and then CLICKS its way to its pane — a
//   clamp changes the landing tab and none of its assertions.)
//
//   MEASURED for the parent card (t_7fb39e38, arm E) on prod revision 7e7bdcd2
//   and on the local HEAD build: 12 tabs, exactly ONE aria-selected = "CHIP",
//   panel h2 "CHIP Collect".
test("admin, NO stored tab: the default intent (chip) survives the unresolved first paint, so the landing is CHIP (en @320)", async ({ page, baseURL }) => {
  // `jf_lang`/`jf_workspace` only — NO `jf_settings_tab`, exactly like the
  // member first-run arms. The intent this arm pins is `loadStoredTab()`'s
  // fallback "chip", read during the first paint, before the bundle has
  // resolved `role`.
  await freshState(page, { jf_lang: "en", jf_workspace: "demo" });
  await stubAPI(page, ADMIN_BUNDLE);
  await page.setViewportSize({ width: PHONE_WIDTH, height: 800 });
  await page.goto(`${baseURL}/settings`, { waitUntil: "domcontentloaded" });

  // Waits for the ADMIN strip (all twelve tabs, CHIP among them) — i.e. for the
  // bundle to have resolved `role` — and settles the read on CHIP.
  const s = await settleAndRead(page, "en", PAGE_TAB_IDS, "chip");
  const what = "en@320 admin no-stored-tab";
  expect(s.tabCount, `${what}: the admin strip renders ${PAGE_TAB_IDS.length} tabs`).toBe(PAGE_TAB_IDS.length);
  expect(
    s.selectedCount,
    `${what}: ${s.selectedCount} tabs carry aria-selected on the admin strip ` +
      `(${s.tabCount} tabs, labels ${JSON.stringify(s.labels)}) — a settled role="tablist" ` +
      `carries exactly one`,
  ).toBe(1);
  expect(
    s.selectedText,
    `${what}: the admin landed on ${JSON.stringify(s.selectedText)}, not CHIP — the tab INTENT was ` +
      `rewritten while \`role\` was still unresolved (a first paint that clamps \`tab\` to the ` +
      `first tab a member is shown), so the merchant's default did not survive the unresolved ` +
      `first paint`,
  ).toBe(DICT["set.tab.chip"].en);
  expect(
    s.firstPanelHeading,
    `${what}: the panel under the strip is ${JSON.stringify(s.firstPanelHeading)}, not the CHIP ` +
      `credentials panel the selected tab stands for`,
  ).toBe(DICT["set.chipTitle"].en);
  expectSettingsAssociation(s, "en", what);

  // The INTENT itself, not only the view: the persist effect writes the tab the
  // page holds, so a clamp that rewrote the state to "domains" leaves that
  // value in storage — and the merchant is moved for every later visit too.
  await expect
    .poll(
      () => page.evaluate(`(() => localStorage.getItem("jf_settings_tab"))()`) as Promise<string | null>,
      {
        message:
          `${what}: the stored tab is not the intent this session started with — the page rewrote ` +
          `"chip" before \`role\` resolved, and persisted the rewrite`,
        timeout: 5_000,
      },
    )
    .toBe("chip");
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

// ─── THE ARROW KEYS (card t_40253ae6) ───────────────────────────────────────
//
// Before this card the settings strip had no keydown handling at all: the only
// way across its nine tabs was Tab, one at a time, and the reveal that keeps the
// selection in view only ran on FOCUS — which the keyboard could not move within
// the strip. This arm walks the strip the way a keyboard user does and requires
// the selection, the painted panel, the association and the roving tab stop to
// move with the focus at every step.
test("the arrow keys walk the member strip, wrap at both ends, and Tab still leaves it (en @320)", async ({ page, baseURL }) => {
  await freshState(page, { jf_lang: "en", jf_workspace: "demo" });
  await stubAPI(page, MEMBER_BUNDLE);
  await page.setViewportSize({ width: PHONE_WIDTH, height: 800 });
  await page.goto(`${baseURL}/settings`, { waitUntil: "domcontentloaded" });

  const s = await settleAndRead(page, "en");
  expectOneSelectionMatchesPanel(s, "en", "en@320 keyboard (on arrival)");

  /** Focuses the strip's tab stop, presses a key, and waits for the strip to move. */
  const press = async (key: string, id: string) => {
    await page.locator('[role="tab"][aria-selected="true"]').focus();
    await page.keyboard.press(key);
    const want = `${DICT[`set.tab.${id}` as keyof typeof DICT]["en"]}|${DICT[PANEL_TITLE[id]]["en"]}|true`;
    await expect
      .poll(
        async () => {
          const t = await readTabState(page, "en");
          return `${t.selectedText}|${t.firstPanelHeading}|${t.focusedIsTab}`;
        },
        { message: `${key} did not move the strip to "${id}" (${want})`, timeout: 10_000 },
      )
      .toBe(want);
    return readTabState(page, "en");
  };

  // Right from the landing tab, one tab at a time, re-checking the association
  // (and the roving tab stop) at EVERY step.
  for (let i = 1; i < MEMBER_TAB_IDS.length; i++) {
    const st = await press("ArrowRight", MEMBER_TAB_IDS[i]);
    expectSettingsAssociation(st, "en", `en@320 keyboard (ArrowRight -> ${MEMBER_TAB_IDS[i]})`);
  }
  // …the wrap off the LAST tab back to the first, and off the FIRST back to the last.
  expectSettingsAssociation(
    await press("ArrowRight", MEMBER_TAB_IDS[0]),
    "en",
    "en@320 keyboard (ArrowRight wraps to the first tab)",
  );
  expectSettingsAssociation(
    await press("ArrowLeft", MEMBER_TAB_IDS[MEMBER_TAB_IDS.length - 1]),
    "en",
    "en@320 keyboard (ArrowLeft wraps to the last tab)",
  );
  // Home/End, from wherever the focus is.
  expectSettingsAssociation(await press("Home", MEMBER_TAB_IDS[0]), "en", "en@320 keyboard (Home)");
  const last = await press("End", MEMBER_TAB_IDS[MEMBER_TAB_IDS.length - 1]);
  expectSettingsAssociation(last, "en", "en@320 keyboard (End)");
  // The reveal has to keep up: the tab the keyboard just landed on is the LAST
  // one in a strip that scrolls at 320px, so an unrevealed selection is a
  // keyboard user looking at the wrong end of the strip.
  await expect
    .poll(
      async () =>
        page.evaluate(`(() => {
          const tabs = [...document.querySelectorAll('[role="tab"]')];
          if (!tabs.length || !tabs[0].parentElement) return false;
          const strip = tabs[0].parentElement;
          const sr = strip.getBoundingClientRect();
          const sel = tabs.find((t) => t.getAttribute("aria-selected") === "true");
          if (!sel) return false;
          const sb = sel.getBoundingClientRect();
          return sb.left >= sr.left - 1 && sb.right <= sr.right + 1;
        })()`),
      { message: "the tab the arrow keys landed on is outside the strip's visible window", timeout: 5_000 },
    )
    .toBe(true);

  // Tab must LEAVE the strip: a widget that swallows a key it does not own traps
  // the keyboard user inside it.
  await page.keyboard.press("Tab");
  await expect
    .poll(async () => (await readTabState(page, "en")).focusedIsTab, {
      message: "pressing Tab did not leave the settings tab strip — the strip is swallowing the key",
      timeout: 5_000,
    })
    .toBe(false);
});
