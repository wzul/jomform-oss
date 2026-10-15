// The twelve signed-in dashboard routes that NO a11y spec scanned (card
// t_9500edf3, the rest of the gap found while fixing t_b589a807).
//
// THE GAP. `web/e2e/a11y.spec.ts` holds the axe PAGE LIST and it is the seven
// PUBLIC routes (`/login`, `/landing`, `/reset-password`, `/magic-link`,
// `/brand`, `/legal/privacy`, `/legal/terms`) plus every `/docs/*` page. An
// authenticated route CANNOT simply be added to it: the `a11y` gate provisions
// no session, so such an entry loads a shell that bounces to `/login`/`/landing`,
// scans no controls, and stays GREEN (t_81b2a928). The signed-in half has to be
// sibling `a11y-*.spec.ts` files, which stub the session + the API at the
// browser level. Five routes had one (`/products`, `/settings`, `/forms`,
// `/admin`, `/profile`); TWELVE did not:
//
//   /activity  /builder  /chat  /connect  /deliveries  /feedback
//   /form-settings  /media  /sales  /spam  /subscriptions  /templates
//
// …which is the same shape that let 15 critical `aria-required-parent` nodes
// ship green on `/profile` + `/settings`, and the same shape that hid two more
// criticals on `/profile` until its first full-document scan ran (`label` on a
// password input, `select-name` on the timezone select — sibling `<label>`s with
// no `for`/`id`).
//
// WHAT THIS FILE ASSERTS, PER ROUTE (the card's acceptance, in one place):
//   1. THE DOCUMENT WAS REALLY SERVED — 2xx. A 404 renders a Next.js "not
//      found" page with no landmarks and no controls, and a scan of it proves
//      nothing (t_81b2a928).
//   2. THE ROUTE DID NOT BOUNCE — the final `page.url()` pathname is the one
//      asked for. Without this, a middleware redirect (whose body is a 200
//      once followed) passes as "reachable" and the scan measures /login.
//   3. THE CLIENT RENDER SETTLED — a React marker on the PAGE'S OWN node (the
//      Shell renders no `<h1>`, so a page `h1` is the page's), the page's own
//      dictionary title rendered, its shell (sidebar or its own chrome) present,
//      and the rendered text over a MEASURED floor. The routes are statically
//      prerendered, so the served HTML alone is not evidence the client
//      component ran — the marker is (the trap `a11y-profile-tabs.spec.ts`
//      documents at its top: an arm that reads the server's markup before
//      hydration reads the WRONG thing).
//   4. A FULL-DOCUMENT axe scan fails on critical/serious — the gate's own rule,
//      with the same IMPACTS list as `a11y.spec.ts` so the two cannot drift.
//   5. THE SCAN IS NOT VACUOUS — axe has to have EVALUATED rules on the page
//      (passes/incomplete/violations non-empty). A 0-violation reading from a
//      scan that analysed nothing is exactly how a green gate over an unvisited
//      route looks.
//
// WHY ONE ARM PER ROUTE. `fullyParallel` gives each route its own worker, so
// the family costs route time rather than a serial walk. The stubs are one
// shared table (below), so a route added here is one entry.
//
// HOW THE COVERAGE IS KEPT (and the honest limit of that). Two source rules read
// THIS file: `src/lib/a11yAuthedCoverage.test.ts` pins each of the twelve to a
// page on disk and to a spec in the family that runs axe, and — because that
// route→spec half is a SUBSTRING search any sibling mentioning the path would
// satisfy (`a11y-singlechar-text.spec.ts` names eleven of these twelve in its own
// table; the weakness QA filed as `t_2275886d`) — a second rule reads THIS file's
// `ROUTES` table and requires every one of the twelve to be an entry, so an arm
// deleted from it fails BY NAME. Neither rule can see an arm that still visits
// its route but stops asserting reachability: that half lives in this file, where
// the assertions are.
//
// THE STATE EACH SCAN RENDERS, measured, not assumed: the API is stubbed at the
// browser level with one row per list so the table pages paint a real row (a
// page that only ever renders its empty state hides every defect a row carries —
// the `/subscriptions` row card t_0fdbe189 found is exactly that). The stubs are
// the shapes the Go handlers write (an array for the notifications list, an
// object with `items` for the paged ones); `/form-settings` additionally walks
// ALL SEVEN of its tabs, because a tab is one click away in the SAME document and
// that is where its defect was — see the arm below.
//
// FRESH BROWSER STATE ON EVERY ARM: storage cleared and the language pinned to
// `en` (a `jf_lang` left behind by an earlier card has already produced a false
// reading on this board — a page rendered Arabic for a reader whose profile said
// `en`), and the session declared explicitly (`e2e/auth.ts`).
import { test, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import type { AxeResults, Result } from "axe-core";
import { readFileSync } from "node:fs";
import { join } from "node:path";

import { signIn } from "./auth";
import { DICT } from "../src/components/i18nDict";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page
// renders. This file visits twelve of them, so it declares the session it means
// — before that gate existed an anonymous visit rendered the account shell, so a
// spec could (and did) rely on the bug. See e2e/auth.ts.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

// The gate's own rule, kept identical to e2e/a11y.spec.ts so the two cannot drift.
const IMPACTS = ["critical", "serious"] as const;
const IMPACT_LABEL = IMPACTS as readonly string[];

function blockersOf(results: AxeResults): Result[] {
  return results.violations.filter((v) => IMPACT_LABEL.includes(v.impact ?? ""));
}

/** Logs every blocking violation with its nodes, so a red run names the site. */
function logBlockers(what: string, blockers: Result[]): void {
  if (blockers.length === 0) return;
  console.log(
    `\n[${what}] ${blockers.length} critical/serious violation(s):\n` +
      blockers
        .map(
          (v) =>
            `  - ${v.id} (${v.impact}): ${v.help}\n` +
            v.nodes
              .slice(0, 5)
              .map((n) => `      ${n.target.join(" ")} — ${(n.failureSummary ?? "").split("\n").slice(-1)[0]}`)
              .join("\n"),
        )
        .join("\n"),
  );
}

/* ─── the session + API the twelve routes need, stubbed once ───────────────── */

const ME = {
  id: "u1",
  email: "merchant@example.com",
  name: "Merchant",
  lang: "en",
  avatar_url: "",
  phone: "",
  timezone: "",
  totp_enabled: false,
  is_super_admin: true,
  default_workspace_id: "a1",
};

const WORKSPACES = [
  { id: "a1", name: "Demo", slug: "demo", role: "admin" },
  { id: "a2", name: "Other", slug: "other", role: "admin" },
];

const BUNDLE = {
  me: ME,
  workspaces: WORKSPACES,
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

const FORM = {
  id: "f1",
  slug: "demo-form",
  title: "Demo form",
  status: "draft",
  url: "https://demo.jomform.com/f/demo-form",
  mode: "payment",
  config: {},
  theme: {},
  allow_embed: true,
};

/** One row per list, so the table pages paint a row and not just the empty state. */
const SALE = {
  ref_code: "R1",
  status: "paid",
  total_sen: 1000,
  payment_method: "fpx",
  buyer: "Ali",
  buyer_email: "ali@example.com",
  form_id: "f1",
  created_at: "2026-09-01T00:00:00Z",
  items: [{ product_id: "p1", price_sen: 1000, qty: 1, name: "Alpha" }],
};

const DELIVERY = {
  id: 1,
  email: "ali@example.com",
  event_type: "delivered",
  sale_ref: "R1",
  form_id: "f1",
  detail: {},
  created_at: "2026-09-01T00:00:00Z",
};

const TEMPLATE = {
  id: "t1",
  slug: "starter",
  title: "Starter template",
  updated_at: "2026-09-01T00:00:00Z",
  theme: { primary: "#5e6ad2" },
};

const MEDIA_ITEM = {
  id: "m1",
  form_id: "f1",
  key: "uploads/a.png",
  url: "https://cdn.example.com/uploads/a.png",
  mime: "image/png",
  size: 12345,
  source: "upload",
  created_at: "2026-09-01T00:00:00Z",
};

const SPAM_SUBMISSION = {
  id: "sp1",
  form_id: "f1",
  data: { name: "bot", email: "bot@example.com" },
  spam_reason: "keyword",
  created_at: "2026-09-01T00:00:00Z",
};

const SPAM_SALE = {
  ref_code: "R9",
  status: "paid",
  total_sen: 500,
  buyer: "bot",
  buyer_email: "bot@example.com",
  form_id: "f1",
  spam_reason: "keyword",
  created_at: "2026-09-01T00:00:00Z",
};

const SUBSCRIPTION = {
  id: "su1",
  customer: "Ali",
  product_id: "p1",
  product_name: "Pro",
  amount_sen: 1000,
  period: "month",
  status: "active",
  next_charge_at: "2026-10-01T00:00:00Z",
  retry_attempts: 0,
};

const EVENT = {
  id: "e1",
  event_name: "sale.paid",
  properties: { ref: "R1" },
  created_at: "2026-09-01T00:00:00Z",
};

const THREAD = {
  id: "th1",
  subject: "Export is missing a column",
  status: "open",
  created_at: "2026-09-01T00:00:00Z",
  last_activity_at: "2026-09-02T00:00:00Z",
  message_count: 2,
};

const FEED = {
  id: "cf1",
  name: "Default feed",
  brand_id: "brand-1",
  email_fallback: "fallback@example.com",
  configured: true,
  condition: null,
};

const NOTIFICATION = {
  id: "n1",
  form_id: "f1",
  name: "Merchant alert",
  event: "sale.paid",
  to_email: "{admin_email}",
  subject: "New sale",
  body: "A sale was paid.",
  condition: null,
  enabled: true,
  is_default: false,
};

const CONFIRMATION = {
  id: "c1",
  name: "Default",
  type: "message",
  message: "Thank you!",
  redirect_url: "",
  condition: null,
  is_default: true,
  position: 0,
};

async function stubAPI(page: Page): Promise<void> {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  // Catch-all FIRST, specific routes AFTER it: Playwright resolves matching
  // routes in reverse registration order, so the later (specific) ones win.
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(ME)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "en" })));
  await page.route(/(.*)\/api\/auth\/profile\/default-workspace$/, (r) => r.fulfill(json({})));
  await page.route(/(.*)\/api\/workspaces(\?.*)?$/, (r) => r.fulfill(json(WORKSPACES)));

  // /form-settings: the form itself, then the sub-resources its tabs read. Each
  // one is stubbed in the shape its Go handler writes — the notifications list is
  // an ARRAY (rest_form_notifications.go writes `list`, and `[]` when the store
  // is not wired), the others are objects.
  await page.route(/(.*)\/api\/rest\/forms(\?.*)?$/, (r) => r.fulfill(json([FORM])));
  await page.route(/(.*)\/api\/rest\/forms\/f1\/notifications(\?.*)?$/, (r) => r.fulfill(json([NOTIFICATION])));
  await page.route(/(.*)\/api\/rest\/forms\/f1\/chip-feeds(\?.*)?$/, (r) => r.fulfill(json({ feeds: [FEED] })));
  await page.route(/(.*)\/api\/rest\/forms\/f1\/email-settings(\?.*)?$/, (r) => r.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/forms\/f1\/confirmations(\?.*)?$/, (r) =>
    r.fulfill(json({ confirmations: [CONFIRMATION] })),
  );
  await page.route(/(.*)\/api\/rest\/forms\/f1\/menu(\?.*)?$/, (r) => r.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/forms\/[^/?]+(\?.*)?$/, (r) => r.fulfill(json(FORM)));
  await page.route(/(.*)\/api\/rest\/webhooks(\?.*)?$/, (r) => r.fulfill(json([])));

  // The twelve routes' own lists.
  await page.route(/(.*)\/api\/rest\/sales[^?]*(.*)$/, (r) =>
    r.fulfill(json({ items: [SALE], page: 1, limit: 20, total: 1 })),
  );
  await page.route(/(.*)\/api\/rest\/email-deliveries[^?]*(.*)$/, (r) =>
    r.fulfill(json({ items: [DELIVERY], page: 1, limit: 20, total: 1 })),
  );
  await page.route(/(.*)\/api\/rest\/templates[^?]*(.*)$/, (r) => r.fulfill(json([TEMPLATE])));
  await page.route(/(.*)\/api\/rest\/media[^?]*(.*)$/, (r) =>
    r.fulfill(json({ items: [MEDIA_ITEM], page: 1, limit: 20, total: 1, used: 12345, limit_bytes: 1000000 })),
  );
  await page.route(/(.*)\/api\/rest\/spam\/submissions[^?]*(.*)$/, (r) =>
    r.fulfill(json({ items: [SPAM_SUBMISSION], page: 1, limit: 20, total: 1 })),
  );
  await page.route(/(.*)\/api\/rest\/spam\/sales[^?]*(.*)$/, (r) =>
    r.fulfill(json({ items: [SPAM_SALE], page: 1, limit: 20, total: 1 })),
  );
  await page.route(/(.*)\/api\/rest\/subscriptions[^?]*(.*)$/, (r) =>
    r.fulfill(json({ items: [SUBSCRIPTION], page: 1, limit: 20, total: 1 })),
  );
  await page.route(/(.*)\/api\/rest\/events(\?.*)?$/, (r) =>
    r.fulfill(json({ items: [EVENT], page: 1, limit: 20, total: 1 })),
  );
  await page.route(/(.*)\/api\/rest\/events\/count(\?.*)?$/, (r) => r.fulfill(json({ total: 1 })));
  await page.route(/(.*)\/api\/rest\/events\/breakdown(\?.*)?$/, (r) => r.fulfill(json({ items: [] })));
  await page.route(/(.*)\/api\/rest\/feedback[^?]*(.*)$/, (r) =>
    r.fulfill(json({ items: [THREAD], page: 1, limit: 20, total: 1 })),
  );
  await page.route(/(.*)\/api\/rest\/chat(\?.*)?$/, (r) =>
    r.fulfill(json({ available: true, max_history_turns: 16, limits: { max_message_chars: 2000, messages_per_hour: 20 } })),
  );
  await page.route(/(.*)\/api\/rest\/products[^?]*(.*)$/, (r) => r.fulfill(json([])));
  await page.route(/(.*)\/api\/rest\/currency\/rates(\?.*)?$/, (r) => r.fulfill(json({ rates: [], cached: false })));
}

/**
 * A FRESH browser state at the app's default locale: storage cleared, then `en`
 * and one workspace written explicitly. Never inherited from an earlier run.
 */
async function freshState(page: Page): Promise<void> {
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

/* ─── what "this route rendered" means, per route, MEASURED ────────────────── */

type Route = {
  name: string;
  path: string;
  /** The page's OWN title, from the dictionary (the Shell renders no `<h1>`). */
  titleKey?: keyof typeof DICT;
  /**
   * How the `<h1>` is compared. `exact` for every route whose heading is the
   * title alone; `prefix` for the ONE page that renders a badge INSIDE its h1
   * (`/chat`: "Assistant" + a `chat.readOnlyBadge` span), where `textContent` is
   * the two strings run together.
   */
  titleMode?: "exact" | "prefix";
  /** The page-owned node the hydration marker is read on. */
  settleSel: string;
  /** Does this route paint the sidebar (Shell) or its own chrome? */
  chrome: "shell" | "own";
  /** MEASURED floor on the page's rendered text. A loading shell is far under it. */
  bodyFloor: number;
  what: string;
};

// MEASURED 2026-10-15 on this tree (production build, fresh state, API stubbed,
// one row per list). Every floor is the text length the route rendered at the
// time this file was added; it is a FLOOR, so a page that renders MORE is fine
// and one that renders a loading shell (or an empty route) fails loudly. The
// chrome column is the sidebar's own nav count for Shell routes.
const ROUTES: Route[] = [
  {
    name: "sales",
    path: "/sales",
    titleKey: "sales.title",
    settleSel: "h1",
    chrome: "shell",
    bodyFloor: 400,
    what: "the sales table, its per-form/status filters, search and date range",
  },
  {
    name: "deliveries",
    path: "/deliveries",
    titleKey: "del.title",
    settleSel: "h1",
    chrome: "shell",
    bodyFloor: 400,
    what: "the email-delivery table and its event-type filter",
  },
  {
    name: "templates",
    path: "/templates",
    titleKey: "tplmg.title",
    settleSel: "h1",
    chrome: "shell",
    bodyFloor: 300,
    what: "the templates list and its save/clone/delete actions",
  },
  {
    name: "media",
    path: "/media",
    titleKey: "media.title",
    settleSel: "h1",
    chrome: "shell",
    bodyFloor: 400,
    what: "the media grid, its upload control and filters",
  },
  {
    name: "chat",
    path: "/chat",
    titleKey: "chat.title",
    // "Assistant" + the read-only badge span, in one h1.
    titleMode: "prefix",
    settleSel: "h1",
    chrome: "shell",
    bodyFloor: 300,
    what: "the assistant page, its transcript region and its composer",
  },
  {
    name: "spam",
    path: "/spam",
    titleKey: "spam.title",
    settleSel: "h1",
    chrome: "shell",
    bodyFloor: 400,
    what: "the spam review queue (submissions + sales)",
  },
  {
    name: "subscriptions",
    path: "/subscriptions",
    titleKey: "sub.title",
    settleSel: "h1",
    chrome: "shell",
    bodyFloor: 400,
    what: "the subscription table and its per-row write actions",
  },
  {
    name: "activity",
    path: "/activity",
    titleKey: "act.title",
    settleSel: "h1",
    chrome: "shell",
    bodyFloor: 400,
    what: "the audit trail, its filters and its pagination",
  },
  {
    name: "feedback",
    path: "/feedback",
    titleKey: "fb.title",
    settleSel: "h1",
    chrome: "shell",
    bodyFloor: 400,
    what: "the feedback threads list and the file-a-report form",
  },
  {
    name: "form-settings",
    path: "/form-settings?form=f1",
    titleKey: "fs.title",
    settleSel: "h1",
    chrome: "shell",
    bodyFloor: 600,
    what: "the seven-tab form settings page (every tab is scanned by its own arm below)",
  },
  {
    name: "builder",
    path: "/builder",
    settleSel: "main",
    chrome: "own",
    bodyFloor: 400,
    what: "the builder's own chrome: palette, canvas and publish controls",
  },
  {
    name: "connect",
    path: "/connect",
    titleKey: "connect.title",
    settleSel: "h1",
    chrome: "own",
    bodyFloor: 100,
    what: "the social-connect landing step (no token: the check-your-email state)",
  },
];

type Settled = {
  hydrated: boolean;
  h1: string;
  navLinks: number;
  hasMain: boolean;
  bodyText: number;
  timedOut: boolean;
};

/**
 * Opens a route and returns the live reading, after asserting the two things
 * that make the scan meaningful: the document was served with 2xx, and the route
 * did not bounce. The settle wait is INSIDE the page: hydration is detected by
 * the marker React leaves on the page's own node, and the page's title and text
 * floor have to land in the same window — so an arm cannot pass on the
 * statically prerendered markup.
 */
async function openRoute(page: Page, route: Route, baseURL?: string): Promise<{ status: number } & Settled> {
  await freshState(page);
  await stubAPI(page);
  const response = await page.goto(route.path, { waitUntil: "domcontentloaded" });
  const status = response?.status() ?? 0;
  expect(
    status >= 200 && status < 300,
    `a11y scan: ${route.path} answered HTTP ${status || "no response"} — a 404 renders "not found" and a ` +
      `scan of it proves nothing (t_81b2a928)`,
  ).toBe(true);

  const wantPath = new URL(route.path, baseURL).pathname;
  expect(
    new URL(page.url()).pathname,
    `${route.path} did not render — the request ended on ${page.url()}. A middleware redirect answers 200 ` +
      `once followed, so this has to be asserted separately from the status: the scan would otherwise measure ` +
      `/login (or /landing) and report the signed-in route as clean.`,
  ).toBe(wantPath);

  const wantTitle = route.titleKey ? JSON.stringify(DICT[route.titleKey].en) : "null";
  const titleMode = route.titleMode ?? "exact";
  const settled = (await page.evaluate(`(async () => {
    // React leaves __reactFiber$…/__reactProps$… on a node it has taken over:
    // the deterministic "this node is hydrated" signal, no sleep needed. Read on
    // a PAGE-OWNED node (the Shell renders no <h1>), so it cannot be satisfied by
    // the shell hydrating while the page itself is still a loading state.
    const marker = (el) =>
      !!el && Object.keys(el).some((k) => k.startsWith("__reactFiber$") || k.startsWith("__reactProps$"));
    const sel = ${JSON.stringify(route.settleSel)};
    const want = ${wantTitle};
    const floor = ${route.bodyFloor};
    const deadline = Date.now() + 20000;
    let last = null;
    const titleOk = (h1) => (want === null ? true : ${titleMode === "prefix" ? "h1.startsWith(want)" : "h1 === want"});
    while (Date.now() < deadline) {
      const node = document.querySelector(sel);
      const main = document.querySelector("#main-content") || document.querySelector("main");
      const read = {
        hydrated: marker(node),
        h1: ((document.querySelector("h1") || {}).textContent || "").trim(),
        navLinks: document.querySelectorAll("nav a[href]").length,
        hasMain: !!main,
        bodyText: (document.body.innerText || "").replace(/\\s+/g, " ").trim().length,
      };
      last = read;
      if (read.hydrated && titleOk(read.h1) && read.hasMain && read.bodyText >= floor) {
        return { ...read, timedOut: false };
      }
      await new Promise((r) => setTimeout(r, 25));
    }
    return { ...last, timedOut: true };
  })()`)) as Settled;

  expect(
    settled.timedOut,
    `${route.path} never settled: after 20s it was hydrated=${settled.hydrated}, h1=${JSON.stringify(
      settled.h1,
    )}, hasMain=${settled.hasMain}, bodyText=${settled.bodyText} (floor ${route.bodyFloor}). A bundle that never ` +
      `resolved, a prerendered page that never hydrated, or a shell that rendered without the page's own content — ` +
      `each means the axe scan below would be measuring the wrong document.`,
  ).toBe(false);

  if (route.chrome === "shell") {
    expect(
      settled.navLinks,
      `${route.path} rendered ${settled.navLinks} sidebar nav link(s) — the Shell did not render, so the scan ` +
        `is not measuring the page the card names`,
    ).toBeGreaterThanOrEqual(10);
  }
  if (route.titleKey) {
    const want = DICT[route.titleKey].en;
    const ok = route.titleMode === "prefix" ? settled.h1.startsWith(want) : settled.h1 === want;
    expect(
      ok,
      `${route.path} rendered ${JSON.stringify(settled.h1)} as its <h1> where the dictionary says ` +
        `${JSON.stringify(want)} (${route.titleMode ?? "exact"} match) — the page's own title is the cheapest witness ` +
        `that THIS route rendered rather than another one's shell`,
    ).toBe(true);
  }

  return { ...settled, status };
}

/** The gate's rule, on the whole document, with the anti-vacuum arm. */
async function scanDocument(page: Page, what: string): Promise<Result[]> {
  const results = await new AxeBuilder({ page }).analyze();
  const blockers = blockersOf(results);
  logBlockers(what, blockers);

  // NON-VACUITY: axe must have EVALUATED rules here. `passes` is empty for a
  // scan over an empty document, and `violations: []` from such a scan is not a
  // clean page — it is a scan of nothing.
  expect(
    results.passes.length + results.incomplete.length + results.violations.length,
    `${what}: axe evaluated no rule-nodes at all — a 0-violation reading from a scan that analysed nothing is ` +
      `vacuous, which is exactly how a green gate over an unvisited route looks`,
  ).toBeGreaterThan(0);

  expect(
    blockers.map((v) => `${v.id} (${v.impact}) × ${v.nodes.length}`),
    `${what} has ${blockers.length} critical/serious axe violation(s)`,
  ).toEqual([]);
  return blockers;
}

/* ─── the twelve routes ────────────────────────────────────────────────────── */

test.describe("a11y: every signed-in dashboard route the gate could not reach", () => {
  for (const route of ROUTES) {
    test(`${route.path} is reachable, settles, and has no critical/serious axe violation (${route.what})`, async ({
      page,
      baseURL,
    }) => {
      test.setTimeout(90_000);
      const settled = await openRoute(page, route, baseURL);
      console.log(
        `[authed] ${route.path} status=${settled.status} hydrated=${settled.hydrated} h1=${JSON.stringify(
          settled.h1,
        )} navLinks=${settled.navLinks} bodyText=${settled.bodyText}`,
      );
      await scanDocument(page, route.path);
    });
  }
});

/* ─── /form-settings: EVERY tab, plus the controls axe cannot name ──────────── */

// The page's own tab list, read out of its source the way
// `e2e/settings-tabs.spec.ts` reads its page's: this is the declaration the arm
// is written against, and a tab added or renamed there fails here as a STALE
// GUARD rather than as a broken page. `__dirname` (CommonJS) rather than
// `import.meta.url` — package.json has no "type": "module".
const PAGE_SOURCE = readFileSync(join(__dirname, "../src/app/form-settings/page.tsx"), "utf8");

function readTabDecl(field: "key" | "label"): string[] {
  const decl = PAGE_SOURCE.match(/const TABS[^=]*=\s*\[([\s\S]*?)\];/);
  if (!decl) {
    throw new Error(
      "form-settings/page.tsx no longer declares TABS — this guard reads it to derive the tabs the panel walks",
    );
  }
  return [...decl[1].matchAll(new RegExp(`${field}:\\s*"([^"]+)"`, "g"))].map((m) => m[1]);
}

const TAB_KEYS = readTabDecl("key");
const TAB_LABELS = readTabDecl("label");

// tab id -> the dictionary key of the FIRST `<h2>` its panel paints, which is
// the panel's own witness that THIS tab rendered rather than the previous one's
// markup. Notifications is the one tab whose first heading is not its own name:
// its panel opens with the per-form RECEIPT email card (`fs.emelTitle`) and the
// "Notifications" card is the second one — a map keyed on the tab's name would
// have asserted the wrong heading there.
const TAB_HEADING: Record<string, keyof typeof DICT> = {
  general: "fs.general",
  menu: "fs.menu",
  confirmation: "fs.confirmation",
  restrictions: "fs.restrictions",
  notifications: "fs.emelTitle",
  chip: "fs.chip",
  webhooks: "fs.webhooks",
};

// The derived tables have to describe the page. Fail BEFORE a browser starts,
// naming the tab, so a renamed tab is reported as a stale guard.
{
  if (TAB_KEYS.length < 2) throw new Error(`form-settings declares ${TAB_KEYS.length} tab(s)`);
  if (TAB_KEYS.length !== TAB_LABELS.length) {
    throw new Error(`form-settings: ${TAB_KEYS.length} tab keys but ${TAB_LABELS.length} labels`);
  }
  for (const key of TAB_KEYS) {
    if (!TAB_HEADING[key]) {
      throw new Error(
        `form-settings declares the tab "${key}" and this guard has no panel heading for it — add its heading key ` +
          `to TAB_HEADING so "every tab was scanned" stays checked`,
      );
    }
    if (!Object.prototype.hasOwnProperty.call(DICT, TAB_HEADING[key])) {
      throw new Error(`TAB_HEADING maps "${key}" to ${TAB_HEADING[key]}, which is not in the dictionary`);
    }
  }
}

/**
 * Every VISIBLE form control on the page whose only accessible name is its
 * placeholder. This is the class axe CANNOT report: axe accepts a placeholder as
 * a name (`a11y-products.spec.ts` found a `select` with none, but an input with a
 * placeholder is a pass), and the repo already refuses it in the builder panel
 * (`e2e/builder-panel-choice-names.spec.ts`) — a placeholder disappears the
 * moment the user types, so a screen-reader user loses the field's name.
 */
const UNNAMED = `(() => {
  const out = [];
  for (const el of document.querySelectorAll("input, select, textarea")) {
    const type = (el.getAttribute("type") || "").toLowerCase();
    if (["hidden", "submit", "button", "reset"].includes(type)) continue;
    const cs = getComputedStyle(el);
    if (cs.display === "none" || cs.visibility === "hidden") continue;
    const r = el.getBoundingClientRect();
    if (r.width === 0 || r.height === 0) continue;
    if (el.getAttribute("aria-label") || el.getAttribute("aria-labelledby")) continue;
    const wrap = el.closest("label");
    if (wrap && (wrap.textContent || "").trim()) continue;
    const id = el.getAttribute("id");
    if (id && document.querySelector('label[for="' + CSS.escape(id) + '"]')) continue;
    out.push(
      el.tagName.toLowerCase() +
        (type ? '[type="' + type + '"]' : "") +
        (el.getAttribute("id") ? "#" + el.getAttribute("id") : "") +
        ' placeholder=' + JSON.stringify(el.getAttribute("placeholder") || ""),
    );
  }
  return out;
})()`;

test.describe("a11y: /form-settings, every tab", () => {
  test("every tab renders its own panel with no critical/serious violation, and every control on it has a real name", async ({
    page,
    baseURL,
  }) => {
    test.setTimeout(180_000);
    const route = ROUTES.find((r) => r.name === "form-settings")!;
    await openRoute(page, route, baseURL);

    const tabs = page.locator("nav button");
    await expect(tabs, "the page's own tab strip must render every tab").toHaveCount(TAB_KEYS.length, {
      timeout: 30_000,
    });

    for (let i = 0; i < TAB_KEYS.length; i++) {
      const key = TAB_KEYS[i];
      await tabs.nth(i).click();
      const wantHeading = DICT[TAB_HEADING[key]].en;
      await expect
        .poll(async () => (await page.locator("h2").first().textContent())?.trim() ?? "", {
          message: `clicking the "${key}" tab did not paint its own panel heading (${wantHeading})`,
        })
        .toBe(wantHeading);

      // The panel must have RENDERED something: a tab that paints an empty card
      // would make the two assertions below vacuous.
      const bodyText = await page.evaluate(
        `(() => (document.body.innerText || "").replace(/\\s+/g, " ").trim().length)()`,
      );
      expect(bodyText, `the "${key}" tab rendered ${bodyText} characters — that is not a rendered panel`).toBeGreaterThan(
        300,
      );

      const controls = await page.evaluate(
        `(() => document.querySelectorAll("input, select, textarea, button").length)()`,
      );
      console.log(`[form-settings] ${key}: bodyText=${bodyText} controls=${controls} heading=${JSON.stringify(wantHeading)}`);

      await scanDocument(page, `/form-settings → ${key}`);

      const unnamed = (await page.evaluate(UNNAMED)) as string[];
      expect(
        unnamed,
        `the "${key}" tab has ${unnamed.length} control(s) named only by a placeholder (or not named at all): ` +
          `${JSON.stringify(unnamed)}. axe accepts a placeholder as a name, so a clean scan does NOT prove these are ` +
          `named — and the name is gone the moment the user types. Give each one an id + <label htmlFor>, or an ` +
          `aria-label.`,
      ).toEqual([]);
    }
  });

  // The notifications LIST is the one load on this page that used to trust its
  // payload's SHAPE. `listFormNotifications` answers an ARRAY
  // (api/internal/httpapi/rest_form_notifications.go), and anything else — a
  // proxy's error object, a stale cache entry — was stored as-is, so the
  // Notifications tab's `notifs.map` threw and React unmounted the WHOLE page:
  // measured, clicking the tab left `#__next_error__`, no `<h1>`, no
  // `document.title` and no `html[lang]` (two more SERIOUS axe nodes on a page
  // that had rendered nothing at all — the shape this card exists to catch). The
  // sibling webhooks load ten lines below has always carried the guard; the
  // notifications load now carries the same one, and this arm drives the tab with
  // a non-array body to keep it that way.
  test("a notifications payload that is not an array must not take the page down", async ({ page, baseURL }) => {
    test.setTimeout(120_000);
    const route = ROUTES.find((r) => r.name === "form-settings")!;
    await openRoute(page, route, baseURL);
    // Registered AFTER the shared stubs, and Playwright resolves matching routes
    // in REVERSE registration order, so this one wins for this endpoint.
    await page.route(/(.*)\/api\/rest\/forms\/f1\/notifications(\?.*)?$/, (r) =>
      r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({}) }),
    );

    await page.reload({ waitUntil: "domcontentloaded" });
    const tabs = page.locator("nav button");
    await expect(tabs, "the page must still render its tab strip").toHaveCount(TAB_KEYS.length, { timeout: 30_000 });
    const notifIndex = TAB_KEYS.indexOf("notifications");
    await tabs.nth(notifIndex).click();

    await expect
      .poll(async () => (await page.locator("h2").first().textContent())?.trim() ?? "", {
        message:
          "with a NON-ARRAY notifications body the page must still paint the tab's panel — before the guard it " +
          "replaced the whole document with an error",
      })
      .toBe(DICT[TAB_HEADING.notifications].en);
    expect(
      await page.evaluate(`(() => !!document.querySelector("#__next_error__"))()`),
      "the page rendered Next's error document instead of the panel",
    ).toBe(false);
    expect(
      (await page.locator("h1").first().textContent())?.trim(),
      "the page's own title must survive a malformed payload",
    ).toBe(DICT["fs.title"].en);
    await scanDocument(page, "/form-settings → notifications (non-array payload)");
  });

  // THE CONTROL, and the reason the arm above can fail. `/form-settings` carried
  // NINE critical `label` nodes across three of its seven tabs when this file was
  // first run (three in General: the title, slug and colour inputs; five in
  // Restrictions: two datetime-locals and three textareas; one in Notifications:
  // the email body textarea) — every one a sibling `<label>` with no `for`/`id`,
  // the same class `/profile` was fixed for. They are fixed now, so the arm that
  // proves this guard MEASURES them is the pre-fix DOM, restored live: remove the
  // `for` from a labelled control's label and require axe to name it again. A
  // guard that cannot go red is not a guard.
  test("CONTROL: the pre-fix unassociated <label> is reported again when restored in the live DOM", async ({
    page,
    baseURL,
  }) => {
    test.setTimeout(120_000);
    const route = ROUTES.find((r) => r.name === "form-settings")!;
    await openRoute(page, route, baseURL);

    const before = await new AxeBuilder({ page }).analyze();
    expect(
      blockersOf(before).map((v) => v.id),
      "the shipped General tab must be clean before the control neutralises anything",
    ).toEqual([]);

    // The PRE-FIX shape, restored on the real control: the `for` that binds the
    // label is dropped, which is exactly the defect the page shipped with.
    const restored = await page.evaluate(`(() => {
      const el = document.querySelector("#fs-title");
      if (!el) return [];
      const labels = [...document.querySelectorAll('label[for="fs-title"]')];
      for (const l of labels) l.removeAttribute("for");
      return labels.map((l) => (l.textContent || "").trim());
    })()`) as string[];
    expect(restored.length, "no label was bound to #fs-title, so this control proves nothing").toBeGreaterThan(0);

    const after = await new AxeBuilder({ page }).analyze();
    const labelRule = after.violations.find((v) => v.id === "label");
    expect(
      labelRule,
      "with the label's `for` removed, axe no longer reports `label` — this guard is not measuring the defect it " +
        "was written for",
    ).toBeTruthy();
    expect(labelRule?.impact, "axe reports `label` as critical").toBe("critical");
    expect(
      labelRule?.nodes.map((n) => n.target.join(" ")),
      "axe must name the control whose label lost its association",
    ).toContain('#fs-title');
  });
});

/* ─── the two shells: a redirect cannot be scanned, and that is asserted ───── */

// The card names three shells alongside the twelve routes. TWO of them are
// redirects by design and CANNOT be axe-scanned at all: `/space-picker` is a
// permanent redirect (it 307s to `/workspace-picker`, the route that carries the
// real picker), and `/post-login` resolves the post-login target and
// `router.replace()`s away within a frame — a full-document scan of either
// measures whatever it landed on, and reporting that as coverage of the shell
// would be the vacuous shape this whole card is about (t_81b2a928). So they are
// covered BY the routes they land on, and that is asserted here rather than left
// implicit: the redirect target must be a route this file (or a sibling a11y
// spec) actually scans.
const SCANNED_PATHS: string[] = [
  ...ROUTES.map((r) => new URL(r.path, "http://x").pathname),
  "/workspace-picker",
  "/profile",
  "/settings",
  "/products",
  "/forms",
  "/admin",
  "/",
];

test.describe("a11y: the redirect shells land on a route that IS scanned", () => {
  test("/space-picker redirects to /workspace-picker, which this file scans", async ({ page }) => {
    await freshState(page);
    await stubAPI(page);
    await page.goto("/space-picker", { waitUntil: "domcontentloaded" });
    expect(new URL(page.url()).pathname, "/space-picker is a permanent redirect to the real picker").toBe(
      "/workspace-picker",
    );
    await expect
      .poll(async () => (await page.locator("h1").first().textContent())?.trim() ?? "", {
        message: "the workspace picker must render its own title, not a shell",
      })
      .toBe(DICT["workspace.pickerTitle"].en);
    // Both stubbed workspaces are offered: a picker that rendered no row would
    // make the scan below a scan of an empty page.
    await expect(
      page.getByRole("button", { name: DICT["workspace.enter"].en }),
      "the picker must offer every workspace it was given",
    ).toHaveCount(WORKSPACES.length);
    await scanDocument(page, "/workspace-picker (reached through /space-picker)");
    expect(
      SCANNED_PATHS,
      "the route /space-picker lands on is not scanned by anything — the shell would be uncovered",
    ).toContain("/workspace-picker");
  });

  test("/post-login resolves to a route this file scans, and never renders a dead page", async ({ page }) => {
    await freshState(page);
    await stubAPI(page);
    await page.goto("/post-login", { waitUntil: "domcontentloaded" });
    // The page runs the SAME post-login resolution every other auth method uses
    // and replaces the location; with one workspace and no default it lands on
    // the dashboard.
    await expect
      .poll(async () => new URL(page.url()).pathname, {
        message: "/post-login never left its placeholder — resolvePostLogin did not resolve",
      })
      .not.toBe("/post-login");
    const landed = new URL(page.url()).pathname;
    expect(
      SCANNED_PATHS,
      `/post-login landed on ${landed}, which no a11y spec scans — the shell would be uncovered by construction`,
    ).toContain(landed);
    await page.waitForTimeout(1500);
    await scanDocument(page, `/post-login → ${landed}`);
  });
});

/* ─── the family's own non-vacuity, on a route in THIS file ────────────────── */

// Every arm above asserts "no critical/serious violation". That is only evidence
// if the same scan CAN report one on the same route. This arm plants a known
// critical violation (an `<img>` with no accessible name → `image-alt`,
// critical) in the live DOM of one of the twelve and requires axe to name it,
// then removes it and requires the page back at zero. Neutralised BY VALUE —
// nothing in the spec is deleted or skipped.
test.describe("a11y: the scan on a signed-in route bites", () => {
  test("a planted unlabelled image is reported on /sales and disappears when it is removed", async ({
    page,
    baseURL,
  }) => {
    test.setTimeout(120_000);
    const route = ROUTES.find((r) => r.name === "sales")!;
    await openRoute(page, route, baseURL);

    const before = await new AxeBuilder({ page }).analyze();
    const beforeAlt = before.violations.find((v) => v.id === "image-alt");
    expect(beforeAlt, "/sales already carries an image-alt violation before anything is planted").toBeUndefined();

    await page.evaluate(`(() => {
      const host = document.querySelector("#main-content") || document.body;
      const img = document.createElement("img");
      img.setAttribute("id", "jf-planted-img");
      img.setAttribute("src", "/logo.svg");
      img.setAttribute("width", "4");
      img.setAttribute("height", "4");
      host.appendChild(img);
    })()`);
    const planted = await new AxeBuilder({ page }).analyze();
    const alt = planted.violations.find((v) => v.id === "image-alt");
    expect(
      alt,
      "the planted <img> with no alt text was NOT reported — this file's scans are not measuring images, so a " +
        "critical of that class would ship green",
    ).toBeTruthy();
    expect(alt?.impact, "axe reports image-alt as critical").toBe("critical");
    const named = (alt?.nodes ?? []).map((n) => n.target.join(" "));
    console.log(`[control /sales] image-alt nodes: ${JSON.stringify(named)}`);
    expect(
      (alt?.nodes ?? []).length,
      `the scan must find exactly the one image this arm planted — the page carried none before it ` +
        `(nodes: ${JSON.stringify(named)})`,
    ).toBe(1);

    await page.evaluate(`(() => document.querySelectorAll("#jf-planted-img").forEach((n) => n.remove()))()`);
    const removed = await new AxeBuilder({ page }).analyze();
    expect(
      removed.violations.find((v) => v.id === "image-alt"),
      "with the planted node removed the page must be back at zero image-alt violations, or this arm is not " +
        "measuring the plant",
    ).toBeUndefined();
    await scanDocument(page, "/sales (planted node removed)");
  });

  test("the unlabelled-control census bites too: a placeholder-only input is refused", async ({ page, baseURL }) => {
    test.setTimeout(120_000);
    const route = ROUTES.find((r) => r.name === "form-settings")!;
    await openRoute(page, route, baseURL);

    expect((await page.evaluate(UNNAMED)) as string[], "the shipped General tab must be clean first").toEqual([]);

    await page.evaluate(`(() => {
      const host = document.querySelector("#main-content") || document.body;
      const input = document.createElement("input");
      input.setAttribute("id", "jf-planted-input");
      input.setAttribute("placeholder", "something");
      host.appendChild(input);
    })()`);
    const flagged = (await page.evaluate(UNNAMED)) as string[];
    expect(
      flagged.join(" "),
      "a control named ONLY by its placeholder was not refused — the census above would not catch the class it " +
        `exists for (axe accepts a placeholder as a name). Flagged: ${JSON.stringify(flagged)}`,
    ).toContain("#jf-planted-input");
  });
});
