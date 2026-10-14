// The Activity page (card t_eb09b25a) — the dashboard's audit-trail surface,
// rendered in a real browser and scanned by axe.
//
// WHY THIS FILE EXISTS. `web/e2e/a11y.spec.ts` scans PUBLIC routes and the
// `/docs/*` pages; `src/lib/docsA11y.test.ts` fails the build when a docs page
// is missing from that list, and nothing polices dashboard routes at all. So a
// new authenticated page sits OUTSIDE the one gate in this repo that renders a
// real page in a real browser — silently, which is exactly how the two
// typed-slug dialogs were missed (t_d334fb02, see e2e/a11y-dialogs.spec.ts).
// Adding `/activity` here (and asserting it in src/lib/activityPage.test.ts) is
// how the new route is inside the gate deliberately rather than by accident.
//
// HOW THE PAGE IS REACHED WITHOUT A SESSION COOKIE. `playwright.config.ts`
// says authenticated surfaces need a live session from the Go API, which the
// scan does not provision. That is true of the page's DATA, not of the page:
// the shell renders from the browser's `dashboard-bundle` fetch, and the audit
// trail comes from `/api/rest/events`, so stubbing both at the browser level
// (the pattern e2e/affiliate-row.spec.ts and e2e/a11y-dialogs.spec.ts already
// use — no Go server, no session, no DB) puts the real component in a real
// browser with no backend at all. No credential is fabricated, so this stays a
// gate: it runs in the `e2e` gate (`npm run e2e`, part of scripts/preflight.sh) —
// the project that selects every spec under `web/e2e` that is not named `a11y*`,
// which is why `npm run a11y` never executed this file's axe arm.
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

// The gate's own rule: critical/serious fail, everything else is reported.
// Kept identical to e2e/a11y.spec.ts so the two cannot drift.
const IMPACTS = ["critical", "serious"] as const;

function blockersOf(results: AxeResults): Result[] {
  return results.violations.filter((v) =>
    IMPACTS.includes((v.impact ?? "") as (typeof IMPACTS)[number]),
  );
}

// Copy read from the app's OWN dictionary, so the assertions compare against
// what ships in English rather than a string typed into this file.
const EN = {
  title: DICT["act.title"].en,
  subtitle: DICT["act.subtitle"].en,
  colEvent: DICT["act.colEvent"].en,
  colProperties: DICT["act.colProperties"].en,
  colTime: DICT["act.colTime"].en,
  filterEvent: DICT["act.filterEvent"].en,
  allEvents: DICT["act.allEvents"].en,
  filterFrom: DICT["act.filterFrom"].en,
  filterTo: DICT["act.filterTo"].en,
  empty: DICT["act.empty"].en,
  docs: DICT["act.docs"].en,
  navTitle: DICT["act.title"].en,
};

const WS_SLUG = "demo";

/** The rows the stubbed events endpoint answers with — the real vocabulary. */
const EVENTS = [
  {
    id: 3,
    event_name: "workspace.deleted",
    properties: { slug: "acme-store", accounts: 1 },
    user_id: "u1",
    created_at: "2026-09-20T02:30:00Z",
  },
  {
    id: 2,
    event_name: "settings.api_key_created",
    properties: { name: "AI Agent Key", scopes: ["readwrite"] },
    user_id: "u1",
    created_at: "2026-09-20T01:00:00Z",
  },
  {
    id: 1,
    event_name: "mcp.tool_call",
    properties: { tool: "list_sales", agent: "Claude Desktop", success: true },
    created_at: "2026-09-19T09:15:00Z",
  },
];

const BUNDLE = {
  me: { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "", timezone: "" },
  workspaces: [{ id: "ws1", name: "Demo", slug: WS_SLUG, role: "admin" }],
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

/** Every /api/* call the page makes, stubbed at the browser level. */
async function stubAPI(page: Page) {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  // Catch-all FIRST, specific routes after it (Playwright resolves matching
  // routes in reverse registration order, so the later, specific ones win).
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(BUNDLE.me)));
  // lang:"" (never set) keeps the browser's value in effect, which is how the
  // init script below pins the locale to en.
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "" })));
  await page.route(/(.*)\/api\/rest\/events(\?.*)?$/, (r) =>
    r.fulfill(json({ items: EVENTS, page: 1, limit: 20, total: EVENTS.length })),
  );
}

/**
 * A FRESH browser state, English, with one workspace selected. Storage is
 * cleared first — the browser keeps localStorage between runs, and a `jf_lang`
 * left behind by an earlier card would render this page in a locale this file
 * never asserts (that false reading has already happened once on this board).
 */
async function freshState(page: Page) {
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      localStorage.setItem("jf_workspace", "demo");
      localStorage.setItem("jf_lang", "en");
    } catch {
      /* storage unavailable */
    }
  });
}

test.describe("activity page (dashboard audit trail)", () => {
  test("renders the trail and every column, from the events list endpoint", async ({ page, baseURL }) => {
    await stubAPI(page);
    await freshState(page);

    // Record what the page actually requested, so this test proves the LIST
    // endpoint was called with the workspace scope — not merely that a table
    // with rows appeared (a wrong-endpoint page can look identical).
    const urls: string[] = [];
    page.on("request", (req) => {
      if (req.url().includes("/api/rest/events")) urls.push(req.url());
    });

    await page.goto(`${baseURL}/activity`, { waitUntil: "domcontentloaded" });

    // Reachability is asserted, not assumed: a bounced or 404 page FAILS here.
    await expect(page.getByRole("heading", { name: EN.title, exact: true })).toBeVisible({
      timeout: 15_000,
    });
    await expect(page.getByText(EN.subtitle)).toBeVisible();

    // The rows: the event NAME is the load-bearing cell, and the card's whole
    // point is that `workspace.deleted` is now visible to the merchant who did
    // the delete.
    const rows = page.locator("tbody tr");
    await expect(rows).toHaveCount(EVENTS.length);
    await expect(page.getByRole("cell", { name: "workspace.deleted" })).toBeVisible();
    await expect(page.getByRole("cell", { name: "settings.api_key_created" })).toBeVisible();
    await expect(page.getByRole("cell", { name: "mcp.tool_call" })).toBeVisible();

    // The detail payload and the timestamp column are both rendered.
    await expect(page.getByText('"slug":"acme-store"')).toBeVisible();
    await expect(page.getByRole("columnheader", { name: EN.colEvent })).toBeVisible();
    await expect(page.getByRole("columnheader", { name: EN.colProperties })).toBeVisible();
    await expect(page.getByRole("columnheader", { name: EN.colTime })).toBeVisible();

    // The filters exist and are labelled — an unlabelled <select>/<input> is
    // both an a11y defect and unusable by a screen reader.
    await expect(page.getByLabel(EN.filterEvent)).toBeVisible();
    await expect(page.getByLabel(EN.filterFrom)).toBeVisible();
    await expect(page.getByLabel(EN.filterTo)).toBeVisible();

    // The endpoint the rows came from, with the scope. Asserted from the real
    // request log rather than the source, so a wired-but-wrong URL fails here.
    expect(urls.length, "the page never called /api/rest/events").toBeGreaterThan(0);
    expect(urls[0]).toContain("/api/rest/events?");
    expect(urls[0]).toContain(`workspace=${WS_SLUG}`);
    expect(urls[0]).toContain("limit=20");

    // The inline "?" doc link (AGENTS.md DOCUMENTATION PARITY rule 4).
    const docLink = page.getByRole("link", { name: "?" });
    await expect(docLink).toBeVisible();
    expect(await docLink.getAttribute("href")).toBe("/docs/activity");
    expect(await docLink.getAttribute("title")).toBe(EN.docs);

    // The nav row, and the fact that it is the ACTIVE one on this page.
    const nav = page.getByRole("link", { name: EN.navTitle });
    await expect(nav).toBeVisible();
    expect(await nav.getAttribute("href")).toBe("/activity");
    expect(await nav.getAttribute("aria-current")).toBe("page");
  });

  test("filtering by event type asks the SERVER for that family", async ({ page, baseURL }) => {
    await stubAPI(page);
    await freshState(page);
    await page.goto(`${baseURL}/activity`, { waitUntil: "domcontentloaded" });
    await expect(page.getByRole("heading", { name: EN.title, exact: true })).toBeVisible({
      timeout: 15_000,
    });

    const urls: string[] = [];
    page.on("request", (req) => {
      if (req.url().includes("/api/rest/events")) urls.push(req.url());
    });

    await page.getByLabel(EN.filterEvent).selectOption("workspace.*");
    await expect.poll(() => urls.length, { timeout: 10_000 }).toBeGreaterThan(0);
    // The prefix filter is a SERVER request, not client-side hiding: a page that
    // filtered rows in the browser would still show "page 1 of N" of the
    // unfiltered set.
    expect(urls.some((u) => u.includes("event_name=workspace."))).toBe(true);
    expect(urls.some((u) => u.includes("page=1"))).toBe(true);
  });

  test("an empty trail says so, and does not read as a broken page", async ({ page, baseURL }) => {
    await page.route(/(.*)\/api\/.*$/, (route) =>
      route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({}) }),
    );
    await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) =>
      r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(BUNDLE) }),
    );
    await page.route(/(.*)\/api\/auth\/profile$/, (r) =>
      r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ lang: "" }) }),
    );
    await page.route(/(.*)\/api\/rest\/events(\?.*)?$/, (r) =>
      r.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ items: [], page: 1, limit: 20, total: 0 }),
      }),
    );
    await freshState(page);
    await page.goto(`${baseURL}/activity`, { waitUntil: "domcontentloaded" });
    await expect(page.getByText(EN.empty)).toBeVisible({ timeout: 15_000 });
  });

  test("a failed load is shown with a retry, never as an empty log", async ({ page, baseURL }) => {
    await page.route(/(.*)\/api\/.*$/, (route) =>
      route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({}) }),
    );
    await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) =>
      r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(BUNDLE) }),
    );
    await page.route(/(.*)\/api\/auth\/profile$/, (r) =>
      r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ lang: "" }) }),
    );
    await page.route(/(.*)\/api\/rest\/events(\?.*)?$/, (r) =>
      r.fulfill({
        status: 500,
        contentType: "application/json",
        body: JSON.stringify({ error: "ralat dalaman", code: "internal" }),
      }),
    );
    await freshState(page);
    await page.goto(`${baseURL}/activity`, { waitUntil: "domcontentloaded" });

    await expect(page.getByText(DICT["act.loadErr"].en)).toBeVisible({ timeout: 15_000 });
    await expect(page.getByRole("button", { name: DICT["common.retry"].en })).toBeVisible();
    // The server's PROSE must never be rendered (issue #6177's rule) — an
    // unknown code falls back to the page's own localized sentence.
    await expect(page.getByText("ralat dalaman")).toHaveCount(0);
  });

  test("has no critical/serious axe violations", async ({ page, baseURL }) => {
    await stubAPI(page);
    await freshState(page);
    await page.goto(`${baseURL}/activity`, { waitUntil: "domcontentloaded" });
    await expect(page.getByRole("heading", { name: EN.title, exact: true })).toBeVisible({
      timeout: 15_000,
    });

    const results = await new AxeBuilder({ page }).analyze();
    const blockers = blockersOf(results);
    if (blockers.length > 0) {
      console.log(
        `\n[/activity] ${blockers.length} critical/serious violation(s):\n` +
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
    expect(blockers.map((v) => v.id), "/activity must have no critical/serious axe violations").toEqual(
      [],
    );
  });
});
