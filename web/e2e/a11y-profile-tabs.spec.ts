// Regression guard: the /profile tab strip is a real tablist.
//
// THE DEFECT (card t_b589a807, bucket-2 follow-up of the post-deploy verify of
// PR #667 — filed because it SURVIVED that release, not because #667 caused it)
//
//   `profile/page.tsx` rendered its five tabs as `<button role="tab"
//   aria-selected=…>` inside a PLAIN `<div>`. A `role="tab"`'s required parent is
//   a `role="tablist"`, so axe reported `aria-required-parent` — impact
//   **critical** — on every one of the five nodes:
//
//     page        role="tab"  role="tablist"  axe aria-required-parent
//     /profile    5           0               critical, 5 nodes
//     /settings   10          0               critical, 10 nodes
//
//   The strip container carried no role at all (`mt-4 flex flex-wrap gap-1
//   border-b`), and `grep -n 'tablist' src/app/profile/page.tsx` returned
//   nothing. The /settings half was fixed by card t_4d32da9a (PR #1098, merged),
//   which added the same one attribute to that strip; this file is the guard for
//   the /profile half, and it pins the whole contract rather than the attribute:
//   ONE tablist containing every tab, exactly ONE `aria-selected`, and the panel
//   under the strip belonging to that selected tab.
//
//   The repo's other four strips (`/products`, `/media`, `/forms`, `/settings`)
//   already carried `role="tablist"` — this was the last one that did not.
//
// WHAT THE FIRST RUN OF THIS FILE ALSO FOUND — AND WHY IT IS FIXED IN THE SAME
//   CARD: with the strip actually scanned, `/profile` carried TWO more criticals
//   that the seven-public-route gate had never looked at:
//
//     label:critical       the email-change `input[type="password"]` — its
//                          `<label>` was a SIBLING with no `for`/`id`.
//     select-name:critical the timezone `<select>` — the same shape.
//
//   The security panel's three password fields were that shape one tab away
//   (they pass axe only while that panel is not rendered). The page associated
//   every label by PROXIMITY, which axe's `label` rule accepts for a text field
//   only because those fields carry a `placeholder`: the controls WITHOUT one —
//   the four password inputs and the timezone select — were critical. So every
//   labelled control on the page now carries an `id` + `htmlFor` pair (the
//   pattern the dashboard-language field already used), and the arms below keep
//   it that way. A guard that is red on arrival is not a guard.
//
// WHY THE GATE NEVER SAW ANY OF IT
//   `web/e2e/a11y.spec.ts` holds the axe PAGE LIST and it was seven PUBLIC
//   routes (`/login`, `/landing`, `/reset-password`, `/magic-link`, `/brand`,
//   `/legal/privacy`, `/legal/terms`), so `npm run a11y` was green while two
//   signed-in pages carried 15 critical nodes between them. This spec is a
//   member of that gate by its NAME alone (`playwright.config.ts` selects the
//   `a11y` project with `testMatch: /a11y.*\.spec\.ts/`), which is the
//   signed-in half of that gap; `src/lib/a11yAuthedCoverage.test.ts` is the
//   source contract that keeps this route from silently falling out again.
//
// WHY A BROWSER MEASUREMENT, NOT A SOURCE ASSERTION
//   "every tab has a tablist ancestor" and "the panel below is the selected
//   tab's" are properties of the RENDERED DOM. A page that keys the strip on one
//   state and a panel branch on another still paints the wrong panel, and no
//   grep over JSX shows it. Every arm here reads the live DOM.
//
// WHY THE SESSION IS STUBBED (the pattern `e2e/a11y-products.spec.ts` and
//   `e2e/a11y-settings-first-run-tab.spec.ts` use): the dashboard pages need the
//   workspace-keyed `dashboard-bundle`, which the gate does not provision (no Go
//   server, no session cookie, no DB on the a11y gate's port). The real client
//   component is driven in a real browser against a stubbed API.
//
// TWO WAITS, BECAUSE THE ROUTE IS STATICALLY PRERENDERED. The served HTML always
//   carries the DEFAULT tab's `aria-selected`; the stored tab / `?tab=` value
//   only appears once the client component HYDRATES. Measured on the built page
//   at 320px with `jf_profile_tab=mcp`: selected "Identity" at 223ms with no
//   React key on the strip node, selected "MCP agents" at 304ms with one. An arm
//   that reads the strip as soon as five tabs exist therefore reads the SERVER's
//   markup and reports the wrong tab — it made the entry-state arms flaky (1 pass
//   / 2 fails over `--repeat-each=3`, at different arms). So `openProfile` waits
//   for BOTH: every tab rendered, and hydration + the expected tab landing.
//
// THE ARMS
//   1. THE CONTRACT (en)      — one tablist, every tab inside it, exactly one
//                               `aria-selected`, and its panel's first `<h2>`.
//                               Card t_40253ae6 adds the ASSOCIATION to this
//                               contract, so every arm below carries it: each tab
//                               names a panel with `aria-controls`, the SELECTED
//                               tab's resolves (getElementById) to the panel that
//                               is actually painted, that panel is a
//                               `role="tabpanel"` labelled by its own tab, and
//                               the strip is ONE tab stop (roving tabindex).
//   2. THE CLICK WALK         — clicking each of the five tabs moves the single
//                               selection and the panel follows it; the strip is
//                               never left with 0 or 2 selections.
//   3. THE ENTRY STATES       — no storage at all (the landing is Identity), a
//                               stored `jf_profile_tab`, `?tab=connected`, the
//                               device-flow email's `?tab=mcp&user_code=…` with
//                               nothing stored (plus the same link after the page
//                               has persisted Identity), and a stored value that
//                               is NOT a tab (the page's own fallback, which is
//                               what keeps `aria-selected` at exactly one). One
//                               arm per test, so each gets its own page and no
//                               storage can leak between them.
//   4. THE LOCALE MATRIX      — en/ms/ta/zh/ar @320px, `htmlLang` (and ar `dir`)
//                               asserted, so a locale that quietly rendered
//                               English cannot pass as Tamil.
//   5. THE AXE ARM            — a FULL-DOCUMENT axe scan of EVERY panel on the
//                               built page, failing on critical/serious, and
//                               THEN the same scan with `role="tablist"` removed
//                               from the live DOM: the rule has to fire
//                               (`aria-required-parent` on exactly the tab nodes)
//                               or this guard is not measuring the defect it was
//                               written for.
//   6. THE KEYBOARD (en)      — Left/Right walk the strip with wraparound at both
//                               ends, Home/End go to the ends, and at EVERY step
//                               the selection, the painted panel, the
//                               association and the roving tab stop move with the
//                               focus; then Tab must LEAVE the strip (a key the
//                               widget does not own must not be swallowed).
//   7. THE KEYBOARD (ar)      — the same, in `dir="rtl"`, where Right moves to the
//                               tab the reader sees on the right (the previous
//                               one in DOM order) — the choice lib/tablist.ts
//                               makes, pinned here.
//   8. THE DETECTOR IS LIVE   — the association arms re-read with `aria-controls`
//                               and `role="tabpanel"` removed from the live DOM:
//                               they have to report the pre-fix shape, or they are
//                               not measuring the two attributes this card adds.
//
// WHY THE CARD EXISTS AT ALL (t_40253ae6, filed from the post-deploy verify of
// v1.21.32): every strip in the app carried `role="tablist"` + `role="tab"` +
// `aria-selected` and NOTHING carried `aria-controls` or `role="tabpanel"` (grep
// over `src/`: 0 hits), so no tab was associated with the panel it controls
// (ARIA 1.2) and no strip had arrow-key navigation. Axe reports NOTHING for
// either omission — measured 0 violations of any impact on the live signed-in
// page — which is exactly why the arms have to READ the DOM.
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
import { expectOneMainLandmark } from "./landmark";

const IMPACTS = ["critical", "serious"] as const;
const IMPACT_LABEL = IMPACTS as readonly string[];

function blockersOf(results: AxeResults): Result[] {
  return results.violations.filter((v) => IMPACT_LABEL.includes(v.impact ?? ""));
}

const LANGS = ["en", "ms", "ta", "zh", "ar"] as const;
type Lang = (typeof LANGS)[number];
const PHONE_WIDTH = 320;

// The page's own `TAB_IDS`, read out of its source. Same reason
// `e2e/settings-tabs.spec.ts` reads its page's list rather than importing the
// page: the page is a client component whose module graph needs React, and
// `TAB_IDS` is the declaration this guard is written against. `__dirname`
// (CommonJS) rather than `import.meta.url` — package.json has no "type": "module".
const PAGE_SOURCE = readFileSync(join(__dirname, "../src/app/profile/page.tsx"), "utf8");

function readPageTabIds(): string[] {
  const decl = PAGE_SOURCE.match(/const TAB_IDS[^=]*=\s*\[([^\]]*)\]/);
  if (!decl) {
    throw new Error(
      "profile/page.tsx no longer declares TAB_IDS — this guard reads it to derive the tabs the strip renders",
    );
  }
  return [...decl[1].matchAll(/"([^"]+)"/g)].map((m) => m[1]);
}

const TAB_IDS = readPageTabIds();

// tab id -> the dictionary key of the panel heading that tab paints. The first
// `<h2>` inside `<main>` is the rendered panel's own title: the page's heading is
// an `<h1>`, the strip carries none, and every panel branch opens with this one.
// A tab whose panel heading is missing from this map is a hard failure below,
// not a skipped arm — "the panel matches the selected tab" is the acceptance.
const PANEL_TITLE: Record<string, keyof typeof DICT> = {
  identity: "prof.identity",
  security: "prof.password",
  passkeys: "prof.passkeys",
  connected: "prof.connectedTitle",
  mcp: "prof.mcpTitle",
};

// The tab label is the tab's VISIBLE span (the strip renders two per tab —
// `sm:hidden` and `hidden sm:inline` — so `textContent` is the doubled string
// "IdentityIdentity").
const TAB_KEY = (id: string): keyof typeof DICT => `prof.${id}` as keyof typeof DICT;

// The derived tables have to describe the page. Fail BEFORE a browser starts,
// naming the tab, so a page that gained or renamed a tab is reported as a stale
// guard rather than as a broken strip.
{
  if (TAB_IDS.length < 2) {
    throw new Error(
      `profile/page.tsx declares ${TAB_IDS.length} tab(s) — this guard asserts a tab strip with a selection`,
    );
  }
  for (const id of TAB_IDS) {
    if (!Object.prototype.hasOwnProperty.call(DICT, TAB_KEY(id))) {
      throw new Error(`profile/page.tsx declares the tab "${id}", which is not in the dictionary (${TAB_KEY(id)})`);
    }
    if (!PANEL_TITLE[id]) {
      throw new Error(
        `profile/page.tsx declares the tab "${id}" and this guard has no panel heading for it. ` +
          `Add its title key to PANEL_TITLE so "the panel below matches the selected tab" stays checked.`,
      );
    }
    if (!Object.prototype.hasOwnProperty.call(DICT, PANEL_TITLE[id])) {
      throw new Error(`PANEL_TITLE maps "${id}" to ${PANEL_TITLE[id]}, which is not in the dictionary`);
    }
  }
}

// The consent lookup the device-flow email's link drives (`?user_code=`). The
// catch-all above would answer `{}`, which leaves the card without a client or
// an Approve button — so the entry-point arms below stub the real shape.
const DEVICE_CONSENT = {
  user_code: "KX9M-X5BS",
  client_id: "client-1",
  client: "Claude Desktop",
  scopes: ["read"],
  scope_prose: "read-only — read your forms, products and sales",
  expires_at: "2099-01-01T00:00:00Z",
  state: "pending",
};

const ME = {
  id: "u1",
  email: "merchant@example.com",
  name: "Merchant",
  lang: "",
  avatar_url: "",
  phone: "",
  timezone: "",
  totp_enabled: false,
  is_super_admin: false,
};

// A resolved session: the strip renders from `TAB_IDS` (it is not role-filtered
// on this page), so what matters is that the bundle lands and the page is past
// its loading state.
const BUNDLE = {
  me: ME,
  workspaces: [{ id: "ws1", name: "Demo", slug: "demo", role: "admin" }],
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
  // lang:"" (never set) keeps the browser value in effect, which is how the
  // locale arms pin their locale to `jf_lang` instead of the profile.
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "" })));
  await page.route(/(.*)\/api\/auth\/(passkeys|social|mcp-agents|trust-devices)(\?.*)?$/, (r) =>
    r.fulfill(json([])),
  );
  await page.route(/(.*)\/api\/auth\/trust-status(\?.*)?$/, (r) => r.fulfill(json({ trusted: false })));
  // Registered last so it wins over the catch-all (Playwright resolves routes in
  // reverse registration order).
  await page.route(/(.*)\/api\/auth\/connected-apps\/device\?user_code=.*$/, (r) =>
    r.fulfill(json(DEVICE_CONSENT)),
  );
}

/**
 * A FRESH browser state per case. `storage` is what this arm puts in
 * localStorage AFTER clearing it — the browser keeps storage between runs and a
 * `jf_lang` / `jf_profile_tab` left behind by an earlier card would render a
 * state no new visitor has (that has already produced a false reading on this
 * board). An empty object is the "no storage at all" case.
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
  status: number;
  tabCount: number;
  labels: string[];
  tablistCount: number;
  tabsOutsideTablist: string[];
  selectedCount: number;
  selectedText: string;
  firstPanelHeading: string;
  htmlLang: string;
  dir: string;
  // ── the ARIA tabs ASSOCIATION (card t_40253ae6) ──────────────────────────
  // Each of these is a property of the RENDERED DOM: a tab's `aria-controls` is
  // resolved against the document (not string-compared), and the element it
  // resolves to is compared against the panel that is actually PAINTED (the one
  // holding the first `<h2>` of `main`). A tab that names a panel which does not
  // exist, or names the wrong one, shows up here.
  tabIds: string[];
  tabsWithoutControls: string[];
  duplicateTabIds: string[];
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
 * One read of the live DOM: the tabs, the tablist(s) they sit in, which tab (if
 * any) is selected, and the panel heading painted under the strip.
 */
async function readTabState(page: Page): Promise<TabState> {
  return page.evaluate(`(() => {
    const tabs = [...document.querySelectorAll('[role="tab"]')];
    const lists = [...document.querySelectorAll('[role="tablist"]')];
    const label = (t) => {
      const spans = [...t.querySelectorAll("span")];
      const vis = spans.find((s) => getComputedStyle(s).display !== "none");
      return ((vis ? vis.textContent : t.textContent) || "").trim();
    };
    const sel = tabs.filter((t) => t.getAttribute("aria-selected") === "true");
    // The panel heading: the FIRST h2 under <main>. The page title is an h1, the
    // strip is a sibling above every panel and carries no heading, and each panel
    // branch opens with its own h2 — so the first h2 in main is the painted
    // panel's. The heading TEXT is read with the anchors removed: three of the
    // five panels carry an inline "?" doc link inside the h2 (AGENTS.md
    // docs-parity rule 4), and textContent would read "Passkeys ?" where the
    // dictionary says "Passkeys".
    const main = document.querySelector("main");
    const headings = main ? [...main.querySelectorAll("h2")] : [];
    const textOf = (h) => {
      const c = h.cloneNode(true);
      for (const a of c.querySelectorAll("a")) a.remove();
      return (c.textContent || "").trim();
    };
    // The ASSOCIATION (card t_40253ae6). The selected tab's aria-controls is
    // RESOLVED against the document — a string comparison against an expected id
    // would pass on a panel that is not in the document at all — and the element
    // it names is compared with the panel that is actually PAINTED: the
    // [role="tabpanel"] the first h2 of main lives inside.
    const selTab = sel.length ? sel[0] : null;
    const controlsAttr = selTab ? (selTab.getAttribute("aria-controls") || "") : "";
    const controlled = controlsAttr ? document.getElementById(controlsAttr) : null;
    const painted = headings.length ? headings[0].closest('[role="tabpanel"]') : null;
    const ids = tabs.map((t) => t.id);
    const stops = tabs.filter((t) => t.getAttribute("tabindex") === "0");
    return {
      status: 0,
      tabCount: tabs.length,
      labels: tabs.map(label),
      tablistCount: lists.length,
      tabsOutsideTablist: tabs.filter((t) => !t.closest('[role="tablist"]')).map(label),
      selectedCount: sel.length,
      selectedText: sel.length ? label(sel[0]) : "",
      firstPanelHeading: headings.length ? textOf(headings[0]) : "",
      htmlLang: document.documentElement.lang,
      dir: document.documentElement.dir,
      tabIds: ids,
      tabsWithoutControls: tabs.filter((t) => !t.getAttribute("aria-controls")).map(label),
      duplicateTabIds: ids.filter((id, i) => !!id && ids.indexOf(id) !== i),
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
 * Opens /profile, asserts the document was really served (a 404 renders a
 * Next.js "not found" page with no tabs and no landmarks — a scan of it proves
 * nothing, the lesson of t_81b2a928), waits for the strip to render every tab,
 * and then waits for the page to SETTLE on `expectTab`.
 *
 * The second wait is not politeness: the route is statically prerendered, so the
 * served markup carries the DEFAULT tab's `aria-selected` until React hydrates
 * (measured: "Identity" with no React key on the strip node at 223ms, the stored
 * tab with one at 304ms). Reading at "every tab exists" reads the server's HTML.
 * Hydration is detected by the marker React itself leaves on the node it took
 * over, and the expected tab has to land inside the same window — so the
 * identity-landing arms cannot pass on the prerendered default either.
 */
async function openProfile(
  page: Page,
  lang: Lang,
  storage: Record<string, string>,
  expectTab: string,
  path = "/profile",
): Promise<TabState> {
  await freshState(page, storage);
  await stubAPI(page);
  await page.setViewportSize({ width: PHONE_WIDTH, height: 800 });
  const response = await page.goto(path, { waitUntil: "domcontentloaded" });
  const status = response?.status() ?? 0;
  expect(
    status >= 200 && status < 300,
    `a11y scan: ${path} answered HTTP ${status || "no response"} — nothing was scanned`,
  ).toBe(true);

  const wantLabel = JSON.stringify(DICT[TAB_KEY(expectTab)][lang]);
  const settle = (await page.evaluate(`(async () => {
    // React leaves __reactFiber$…/__reactProps$… on a DOM node it has taken
    // over: the deterministic "this node is hydrated" signal, no sleep needed.
    // The node it is read on is a TAB, not the strip: the arms below have to be
    // able to FAIL on a strip that is missing its role, and a marker read on
    // '[role="tablist"]' would instead time out with "never settled" — the wrong
    // assertion, naming the wrong cause.
    const marker = (el) =>
      !!el && Object.keys(el).some((k) => k.startsWith("__reactFiber$") || k.startsWith("__reactProps$"));
    const label = (t) => {
      const spans = [...t.querySelectorAll("span")];
      const vis = spans.find((s) => getComputedStyle(s).display !== "none");
      return ((vis ? vis.textContent : t.textContent) || "").trim();
    };
    const want = ${TAB_IDS.length};
    const wantTab = ${wantLabel};
    const deadline = Date.now() + 20000;
    let count = 0;
    let selected = "";
    let hydrated = false;
    while (Date.now() < deadline) {
      const tabs = [...document.querySelectorAll('[role="tab"]')];
      count = tabs.length;
      const sel = tabs.find((t) => t.getAttribute("aria-selected") === "true");
      selected = sel ? label(sel) : "";
      hydrated = marker(tabs[0]);
      if (count === want && hydrated && selected === wantTab) {
        return { count, hydrated, selected, timedOut: false };
      }
      await new Promise((r) => setTimeout(r, 25));
    }
    return { count, hydrated, selected, timedOut: true };
  })()`)) as { count: number; hydrated: boolean; selected: string; timedOut: boolean };

  expect(
    settle.timedOut,
    `the page never settled on the ${expectTab} tab (${wantLabel}): after 20s it had ` +
      `${settle.count} of ${TAB_IDS.length} tabs, hydrated=${settle.hydrated}, selected=${JSON.stringify(
        settle.selected,
      )}. A bundle that never resolved, a prerendered default that never gave way, or a stored/URL ` +
      `tab the page never honoured — each of those means the strip cannot be read as the contract.`,
  ).toBe(false);

  const s = await readTabState(page);
  return { ...s, status };
}

/**
 * The card's acceptance, in one place for the tab the page must have landed on:
 * ONE tablist holding every tab, exactly one `aria-selected`, and the panel
 * under the strip is that tab's.
 */
function expectContract(s: TabState, lang: Lang, landedOn: string, what: string) {
  expect(
    s.tablistCount,
    `${what}: the strip renders ${s.tablistCount} role="tablist" — the ${s.tabCount} tabs need exactly one`,
  ).toBe(1);
  expect(
    s.tabsOutsideTablist,
    `${what}: ${s.tabsOutsideTablist.length} of ${s.tabCount} role="tab" nodes have NO role="tablist" ` +
      `ancestor (${JSON.stringify(s.tabsOutsideTablist)}) — axe aria-required-parent, critical`,
  ).toEqual([]);
  expect(s.labels, `${what}: the strip is not rendering this locale's labels`).toEqual(
    TAB_IDS.map((id) => DICT[TAB_KEY(id)][lang]),
  );
  expect(
    s.selectedCount,
    `${what}: ${s.selectedCount} tabs carry aria-selected on a role="tab" strip ` +
      `(${s.tabCount} tabs, labels ${JSON.stringify(s.labels)}) — a tablist is expected to carry exactly one`,
  ).toBe(1);
  expect(
    s.selectedText,
    `${what}: the selected tab is ${JSON.stringify(s.selectedText)}, not ${JSON.stringify(
      DICT[TAB_KEY(landedOn)][lang],
    )}`,
  ).toBe(DICT[TAB_KEY(landedOn)][lang]);
  expect(
    s.firstPanelHeading,
    `${what}: the panel under the strip is ${JSON.stringify(s.firstPanelHeading)}, which is not ` +
      `the selected tab's (${JSON.stringify(DICT[PANEL_TITLE[landedOn]][lang])})`,
  ).toBe(DICT[PANEL_TITLE[landedOn]][lang]);
  expectAssociation(s, what);
}

/**
 * The ARIA tabs ASSOCIATION, read off the live DOM (card t_40253ae6).
 *
 * Before this card no tab carried `aria-controls` and no element anywhere in
 * `src/` carried `role="tabpanel"`, so a screen reader was told nothing about
 * which panel the selected tab controls. Axe reports NOTHING for the omission
 * (measured on the live signed-in page: 0 violations of any impact) — which is
 * why this is an arm and not a rule: the only way to see it is to READ the
 * association out of the document.
 *
 * Every assertion here is about the RENDERED document rather than about an
 * attribute string:
 *   * `aria-controls` on the SELECTED tab is RESOLVED with `getElementById` —
 *     so a tab naming a panel that is not in the document fails here — and the
 *     element it resolves to must be the panel that is actually PAINTED (the
 *     one holding the first `<h2>` of `main`). A page whose tab points at
 *     another tab's panel, or at a detached node, cannot pass.
 *   * that panel must carry `role="tabpanel"` and `aria-labelledby` naming THE
 *     TAB (not another one).
 *   * EVERY tab — selected or not — must carry a non-empty, unique `id` and an
 *     `aria-controls`: an unselected tab's idref is exempt from axe
 *     (`aria-valid-attr-value` skips `aria-controls` while `aria-selected` is
 *     "false"), so nothing else would ever notice one going missing.
 *   * roving tabindex: exactly ONE tab in the page tab sequence and it is the
 *     selected one. Without it a keyboard user Tabs through every tab one at a
 *     time, which is the behaviour the arrow keys replace.
 */
function expectAssociation(s: TabState, what: string) {
  expect(
    s.tabsWithoutControls,
    `${what}: ${s.tabsWithoutControls.length} of ${s.tabCount} role="tab" nodes carry NO aria-controls ` +
      `(${JSON.stringify(s.tabsWithoutControls)}) — no tab is associated with the panel it controls, ` +
      `and axe does not report it either way (measured: 0 violations of any impact)`,
  ).toEqual([]);
  expect(
    s.tabIds.filter((id) => !id),
    `${what}: ${s.tabIds.filter((id) => !id).length} tab(s) have no id — a panel's aria-labelledby has ` +
      `nothing to point at (ids: ${JSON.stringify(s.tabIds)})`,
  ).toEqual([]);
  expect(
    s.duplicateTabIds,
    `${what}: duplicate tab id(s) ${JSON.stringify(s.duplicateTabIds)} — every idref in the strip is ` +
      `then ambiguous`,
  ).toEqual([]);
  expect(
    s.controlledRole,
    `${what}: the selected tab (${JSON.stringify(s.selectedText)}) names ` +
      `${JSON.stringify(s.selectedControls)}, which does not resolve to an element in the document — ` +
      `a tab whose panel is not rendered has no announced relationship with what IS on screen`,
  ).toBe("tabpanel");
  expect(
    s.controlledIsPaintedPanel,
    `${what}: the panel the selected tab controls (#${s.controlledId}) is NOT the panel painted under ` +
      `the strip — the association points somewhere other than what the reader sees`,
  ).toBe(true);
  expect(
    s.panelLabelledBy,
    `${what}: the panel #${s.controlledId} is labelled by ${JSON.stringify(s.panelLabelledBy)}, not by ` +
      `its own tab (#${s.selectedId})`,
  ).toBe(s.selectedId);
  expect(
    s.tabpanelCount,
    `${what}: ${s.tabpanelCount} role="tabpanel" elements are rendered — the page paints exactly one panel`,
  ).toBe(1);
  expect(
    s.tabStops,
    `${what}: ${s.tabStops} tab(s) carry tabindex="0" — a tablist is ONE tab stop (roving tabindex), ` +
      `otherwise the Tab key walks every tab one at a time`,
  ).toBe(1);
  expect(
    s.tabStopIsSelected,
    `${what}: the strip's single tab stop is not the selected tab (${JSON.stringify(s.selectedText)})`,
  ).toBe(true);
}

/** Clicks a tab and waits until the panel under the strip is that tab's. */
async function clickTab(page: Page, id: string) {
  await page.getByRole("tab", { name: DICT[TAB_KEY(id)].en, exact: true }).click();
  await expect
    .poll(async () => (await readTabState(page)).firstPanelHeading, {
      message: `clicking "${id}" (${DICT[TAB_KEY(id)].en}) did not paint that tab's panel`,
    })
    .toBe(DICT[PANEL_TITLE[id]].en);
}

test("the strip is ONE tablist, every tab is inside it, exactly one is selected and its panel is painted (en)", async ({ page }) => {
  const s = await openProfile(page, "en", { jf_lang: "en", jf_workspace: "demo" }, "identity");
  expect(s.tabCount, `the strip renders ${TAB_IDS.length} tabs`).toBe(TAB_IDS.length);
  expectContract(s, "en", "identity", "en");
});

test("clicking each tab moves the single selection and the panel with it (en)", async ({ page }) => {
  const s = await openProfile(page, "en", { jf_lang: "en", jf_workspace: "demo" }, "identity");
  expectContract(s, "en", "identity", "en click walk (on arrival)");

  for (const id of TAB_IDS) {
    await clickTab(page, id);
    expectContract(await readTabState(page), "en", id, `en click walk (on ${id})`);
  }
});

test("no storage at all: the page lands on a rendered tab with exactly one selection (en)", async ({ page }) => {
  const s = await openProfile(page, "en", {}, "identity");
  expectContract(s, "en", "identity", "en no-storage");
});

test("a stored tab is honoured, and it is the only selected tab (en)", async ({ page }) => {
  const s = await openProfile(
    page,
    "en",
    { jf_lang: "en", jf_workspace: "demo", jf_profile_tab: "mcp" },
    "mcp",
  );
  expectContract(s, "en", "mcp", "en stored-mcp");
});

test("?tab=connected lands on Social accounts, not on the prerendered default (en)", async ({ page }) => {
  const s = await openProfile(
    page,
    "en",
    { jf_lang: "en", jf_workspace: "demo" },
    "connected",
    "/profile?tab=connected",
  );
  expectContract(s, "en", "connected", "en ?tab=connected");
});

// #1703 follow-up: THE EMAIL'S OWN ENTRY POINT. The device-flow consent mail
// links to `/profile?tab=mcp&user_code=…` (api/internal/httpapi/oauth_device.go).
// The page honoured `?tab=` for `connected` ONLY and otherwise used the stored
// tab, so a first-time user (nothing stored) landed on Identity with no code
// field and no Approve/Deny card — measured on prod v1.37.0 with cleared storage.
// This arm is that exact URL with NO `jf_profile_tab` stored, which is the state
// every first-time headless-onboarding user arrives in; the arm below it is the
// second click, when the page has already persisted the wrong tab.
test("the device-flow email link ?tab=mcp&user_code= opens the consent card with the code (en)", async ({
  page,
}) => {
  const s = await openProfile(
    page,
    "en",
    { jf_lang: "en", jf_workspace: "demo" },
    "mcp",
    "/profile?tab=mcp&user_code=KX9M-X5BS",
  );
  expectContract(s, "en", "mcp", "en ?tab=mcp (email entry point)");

  // The code is prefilled and the card is on screen — no further navigation.
  await expect(
    page.locator("#prof-device-code"),
    "the consent code field is not on screen for the email's link",
  ).toHaveValue(DEVICE_CONSENT.user_code);
  await expect(
    page.getByText(DEVICE_CONSENT.client, { exact: false }).first(),
    "the consent card does not name the client that is asking",
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: DICT["prof.deviceApprove"].en, exact: true }),
    "the email's link does not put the Approve button on screen",
  ).toBeVisible();
});

test("the email link still opens the consent card after the page persisted Identity (en)", async ({
  page,
}) => {
  // The defect's second half: the page persists whatever tab it landed on, so a
  // user who opened the link once (on Identity) then clicked it again had
  // `jf_profile_tab=identity` stored. An explicit `?tab=` must beat it.
  const s = await openProfile(
    page,
    "en",
    { jf_lang: "en", jf_workspace: "demo", jf_profile_tab: "identity" },
    "mcp",
    "/profile?tab=mcp&user_code=KX9M-X5BS",
  );
  expectContract(s, "en", "mcp", "en ?tab=mcp over stored identity");
  await expect(page.locator("#prof-device-code")).toHaveValue(DEVICE_CONSENT.user_code);
});

test("a stored value that is NOT a tab falls back to a rendered tab (en)", async ({ page }) => {
  // A stale key, a hand-edited entry: the page must land on a tab it actually
  // renders. This is what keeps `aria-selected` at exactly one instead of the
  // /settings first-run hole (card t_4d32da9a).
  const s = await openProfile(
    page,
    "en",
    { jf_lang: "en", jf_workspace: "demo", jf_profile_tab: "workspace" },
    "identity",
  );
  expectContract(s, "en", "identity", "en stored-non-tab");
});

for (const lang of LANGS) {
  test(`one aria-selected tab, its panel, and the locale's labels (${lang} @320)`, async ({ page }) => {
    const s = await openProfile(page, lang, { jf_lang: lang, jf_workspace: "demo" }, "identity");
    expect(s.htmlLang, `${lang}@320: the document language is not the one this arm pinned`).toBe(lang);
    // RTL is the direction a reader would notice; on the ltr arms a bare dir is
    // the absence of an attribute, and the label assertion is what proves the
    // locale actually rendered.
    if (lang === "ar") {
      expect(s.dir, "ar@320: the document direction").toBe("rtl");
    }
    expectContract(s, lang, "identity", `${lang}@320`);
  });
}

test("axe: every panel of /profile is free of critical/serious violations, and the rule that caught the defect fires when the tablist is removed", async ({ page }) => {
  await openProfile(page, "en", { jf_lang: "en", jf_workspace: "demo" }, "identity");

  // The document's ONE `main` landmark (card t_25a8fc88). It used to be this
  // page's own `<main>`; it now comes from the Shell, which wraps the page body,
  // so the landmark is measured here rather than assumed from the old markup.
  await expectOneMainLandmark(page, "/profile");

  // EVERY PANEL, not just the landing one. Each panel is a different set of
  // controls, and the gate's public-route scan never saw any of them: the
  // identity pane alone carried two criticals the first time this arm ran
  // (`label` on the email-change password field, `select-name` on the timezone
  // select — sibling `<label>`s with no `for`/`id`), and the security pane's
  // three password fields were the same shape one tab away. A FULL-DOCUMENT
  // scan per panel is what makes this coverage real; scoping it to `main` or to
  // the strip would have measured only the part already known to be fixed.
  for (const id of TAB_IDS) {
    await clickTab(page, id);

    const results = await new AxeBuilder({ page }).analyze();
    const blockers = blockersOf(results);
    if (blockers.length > 0) {
      for (const v of blockers) {
        console.log(`  [${id}] [${v.impact}] ${v.id}: ${v.help}`);
        for (const n of v.nodes.slice(0, 5)) console.log(`      ${n.target.join(" ")}`);
      }
    }
    expect(
      blockers.map((v) => v.id),
      `the ${id} panel of /profile has ${blockers.length} critical/serious axe violation(s)`,
    ).toEqual([]);
  }

  // AND THE DETECTOR IS LIVE ON THIS PAGE. The fix is one attribute, so the arm
  // that proves this guard can fail is the same scan with that attribute removed
  // from the live DOM — neutralised BY VALUE, never by deleting a test. If axe
  // stops reporting `aria-required-parent` here, this file is no longer measuring
  // the defect it was written for and goes red instead of quietly passing.
  await page.evaluate(`(() => {
    for (const el of document.querySelectorAll('[role="tablist"]')) el.removeAttribute("role");
  })()`);
  const neutral = await new AxeBuilder({ page }).analyze();
  const arp = neutral.violations.find((v) => v.id === "aria-required-parent");
  expect(
    arp,
    "with role=\"tablist\" removed from the strip, axe no longer reports aria-required-parent — " +
      "this guard is not measuring the defect it was written for",
  ).toBeTruthy();
  expect(arp?.impact, "aria-required-parent must be critical").toBe("critical");
  expect(
    arp?.nodes.length,
    `the neutralised strip must flag every one of its ${TAB_IDS.length} tabs`,
  ).toBe(TAB_IDS.length);
});

// ─── THE KEYBOARD (card t_40253ae6) ─────────────────────────────────────────
//
// APG's tabs pattern, automatic activation: Left/Right move to the previous /
// next tab with wraparound, Home/End to the ends, and the newly focused tab is
// activated. Before this card the strips had NO keydown handling at all — the
// only way across a strip was Tab, one tab at a time — so every arm below is a
// property the pre-fix page fails outright.

/**
 * Focuses the strip's tab stop, presses `key`, and waits until the strip has
 * MOVED: the selection is `id`'s, the panel under the strip is that tab's, and
 * the focus is on a tab (the moved-to one). Three facts, one wait — a page that
 * changes the selection without moving focus (or the other way round) cannot
 * pass any of them.
 *
 * The poll returns a joined string so a failure prints the read it timed out on
 * rather than only the expectation.
 */
async function keyMove(page: Page, key: string, id: string, lang: Lang): Promise<TabState> {
  await page.locator('[role="tab"][aria-selected="true"]').focus();
  await page.keyboard.press(key);
  const want = `${DICT[TAB_KEY(id)][lang]}|${DICT[PANEL_TITLE[id]][lang]}|true`;
  await expect
    .poll(
      async () => {
        const s = await readTabState(page);
        return `${s.selectedText}|${s.firstPanelHeading}|${s.focusedIsTab}`;
      },
      {
        message:
          `${key} did not move the strip to "${id}" (${want}): the selection, the painted panel and the ` +
          `focus have to move together`,
        timeout: 10_000,
      },
    )
    .toBe(want);
  return readTabState(page);
}

test("the arrow keys walk the strip, wrap at both ends, and the focus and the panel follow (en)", async ({ page }) => {
  const s = await openProfile(page, "en", { jf_lang: "en", jf_workspace: "demo" }, "identity");
  expectContract(s, "en", "identity", "en keyboard (on arrival)");

  // Right, one tab at a time, through the whole strip: the panel + the
  // association + the roving tab stop are re-checked at EVERY step, so a strip
  // that moves the selection but leaves the panel behind fails on the step it
  // happens.
  for (let i = 1; i < TAB_IDS.length; i++) {
    const st = await keyMove(page, "ArrowRight", TAB_IDS[i], "en");
    expectContract(st, "en", TAB_IDS[i], `en keyboard (ArrowRight -> ${TAB_IDS[i]})`);
  }
  // …and the wrap off the last tab back to the first.
  const wrapped = await keyMove(page, "ArrowRight", TAB_IDS[0], "en");
  expectContract(wrapped, "en", TAB_IDS[0], "en keyboard (ArrowRight wraps to the first tab)");

  // Left, and the wrap off the FIRST tab to the last.
  const back = await keyMove(page, "ArrowLeft", TAB_IDS[TAB_IDS.length - 1], "en");
  expectContract(back, "en", TAB_IDS[TAB_IDS.length - 1], "en keyboard (ArrowLeft wraps to the last tab)");
  const middle = await keyMove(page, "ArrowLeft", TAB_IDS[TAB_IDS.length - 2], "en");
  expectContract(middle, "en", TAB_IDS[TAB_IDS.length - 2], "en keyboard (ArrowLeft)");

  // Home/End go to the ends from wherever the focus is.
  const home = await keyMove(page, "Home", TAB_IDS[0], "en");
  expectContract(home, "en", TAB_IDS[0], "en keyboard (Home)");
  const end = await keyMove(page, "End", TAB_IDS[TAB_IDS.length - 1], "en");
  expectContract(end, "en", TAB_IDS[TAB_IDS.length - 1], "en keyboard (End)");

  // A KEY THIS WIDGET DOES NOT OWN MUST NOT BE SWALLOWED. `Tab` has to leave the
  // strip — APG says it moves to the next element in the page tab sequence
  // outside the tablist. If the strip consumed it (the classic roving-tabindex
  // bug: the handler preventDefaults anything it does not recognise), the focus
  // would stay on a tab and a keyboard user would be trapped in the strip.
  await page.keyboard.press("Tab");
  await expect
    .poll(async () => (await readTabState(page)).focusedIsTab, {
      message: "pressing Tab did not leave the tab strip — the strip is swallowing a key it does not own",
      timeout: 5_000,
    })
    .toBe(false);
});

test("in dir=rtl (ar) the arrow keys follow the reading order and the association holds in Arabic", async ({ page }) => {
  // The strip lays its first tab out RIGHTMOST under `dir="rtl"`, so "the tab to
  // the right" is the PREVIOUS one in DOM order. This arm pins that choice (see
  // lib/tablist.ts): Right from the first tab wraps to the LAST one.
  const s = await openProfile(page, "ar", { jf_lang: "ar", jf_workspace: "demo" }, "identity");
  expect(s.dir, "ar: the document direction").toBe("rtl");
  expectContract(s, "ar", "identity", "ar keyboard (on arrival)");

  const wrapped = await keyMove(page, "ArrowRight", TAB_IDS[TAB_IDS.length - 1], "ar");
  expectContract(wrapped, "ar", TAB_IDS[TAB_IDS.length - 1], "ar keyboard (ArrowRight wraps to the last tab)");

  const next = await keyMove(page, "ArrowLeft", TAB_IDS[0], "ar");
  expectContract(next, "ar", TAB_IDS[0], "ar keyboard (ArrowLeft returns to the first tab)");

  const second = await keyMove(page, "ArrowLeft", TAB_IDS[1], "ar");
  expectContract(second, "ar", TAB_IDS[1], "ar keyboard (ArrowLeft, DOM order)");
});

test("the association arms measure the live DOM: neutralising aria-controls / role=\"tabpanel\" makes them report the gap", async ({ page }) => {
  // The arms above are only worth their green if they can go RED, and the defect
  // they exist for is the ABSENCE of two attributes. So the same read is taken
  // again with those attributes removed from the live DOM — neutralised BY
  // VALUE, never by deleting a test — and the predicate has to report exactly the
  // pre-fix shape: every tab with no aria-controls, a panel that is not a
  // tabpanel, and not one role="tabpanel" element in the document.
  const s = await openProfile(page, "en", { jf_lang: "en", jf_workspace: "demo" }, "identity");
  expectAssociation(s, "en association (on arrival)");

  await page.evaluate(`(() => {
    for (const t of document.querySelectorAll('[role="tab"]')) t.removeAttribute("aria-controls");
    for (const p of document.querySelectorAll('[role="tabpanel"]')) p.removeAttribute("role");
  })()`);
  const neutral = await readTabState(page);
  expect(
    neutral.tabsWithoutControls.length,
    "with aria-controls removed from the live DOM, every tab still reads as associated — this arm is not " +
      "measuring the attribute it was written for",
  ).toBe(TAB_IDS.length);
  expect(neutral.controlledRole, "the tab still resolves to a tabpanel after its role was removed").toBe("");
  expect(neutral.tabpanelCount, "role=\"tabpanel\" survived its own removal").toBe(0);
  expect(neutral.controlledIsPaintedPanel, "the association still reads as painted").toBe(false);
});
