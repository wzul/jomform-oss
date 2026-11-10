// The Activity page — the dashboard surface of the audit trail (card t_eb09b25a).
//
// WHAT WAS WRONG (measured on main, not inferred)
//   The events table was written, and `GET /v1/rest/events` (plus `list_events`
//   over MCP) could read it, but NO shipped page rendered it:
//
//     grep -rn 'event_name=' web/src       -> 0 hits
//     grep -rn 'api/rest/events' web/src   -> only /sales:325, and that is the
//                                             SSE STREAM, not the list
//
//   The one UI that ever showed these rows (Settings → MCP "Request history")
//   was deleted by d42a972 (#311) while issue #195 stayed CLOSED, so the
//   merchant who deleted a workspace could not read the `workspace.deleted` row
//   anywhere in the product. `workspace.deleted` is the row a destructive,
//   irreversible action records — the exact case the surface exists for.
//
// WHY A SOURCE GUARD AND NOT ONLY A BROWSER TEST
//   This repo has no JSX/DOM unit runner (no vitest/jest — see src/lib/i18n.test.ts),
//   so the request URLs, the nav row and the dictionary can only be asserted
//   against source here. The EXECUTED half is the axe scan of the rendered page
//   (web/e2e/activity-page.spec.ts), which is a deliberate addition to the a11y
//   gate: a dashboard route is NOT auto-scanned (`docsA11y.test.ts` polices
//   `/docs/*` only, and a11y.spec.ts's PAGES list is public + docs routes), so
//   without that spec this page would sit outside the one gate that renders a
//   real page in a real browser.
//
// WHAT IS PINNED, AND WHAT MAKES IT BITE
//   1. The page reads the events LIST endpoint, and NOT the SSE stream. The
//      stream carries five event names (sale.paid, sale.failed, sale.refunded,
//      subscription.charged, response.submitted) — swapping the list for the
//      stream would leave a green page showing a merchant a fraction of their
//      own history, which is the failure this assertion exists for.
//   2. Every dashboard fetch carries the workspace scope through the SHARED
//      helper. An unscoped call resolves Accounts.ByOwner(...)[0] — the user's
//      FIRST account, not the workspace selected in the top bar — so with two
//      workspaces this page would silently show the wrong one's audit trail.
//   3. The nav row and the `active` union move together: a row the Shell cannot
//      mark active, or an `active` value with no row, is a nav entry that never
//      highlights.
//   4. The docs page exists, is linked from the dashboard (the inline "?"), from
//      the docs sidebar, from the /docs index and from the sitemap, and is
//      listed in the a11y gate's PAGES. An unreachable or unscanned docs page is
//      the #513 failure mode.
//   5. The 14 orphan keys this card measured are GONE, and the MCP keys that are
//      still live are NOT (a sweep that took a live key with it would break the
//      Profile → MCP tab, which renders `set.serverUrl`, `set.clientId`,
//      `set.revoke` and `set.activeMode`).
//   6. (card t_07d8f4a4) The event-type dropdown offers EVERY family the API
//      actually records, and no family it does not: the families are read out of
//      the recorder call sites under api/internal, so `chat.*` / `admin.*` /
//      `feedback.*` cannot go missing again — those rows were readable in the
//      unfiltered list and unselectable in the filter.
//   7. (card t_804040d3) The event CELL is a sentence, not a token. Every event
//      name the recorders can write has a `act.event.*` label in all five
//      locales, the raw name survives as the cell's `title`, and a label for a
//      name nothing records fails too. The three names built at the call site
//      (`form.status_*`, `form.bulk_*`, `product.bulk_*`) are expanded from the
//      Go source, so a new bulk action cannot ship as snake_case.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, existsSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

import { DICT, LANGS } from "../components/i18nDict.ts";
import { EVENT_LABEL_KEYS } from "./activityEvents.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");
const path = (rel: string) => fileURLToPath(new URL(rel, import.meta.url));

const PAGE = read("../app/activity/page.tsx");
const SHELL = read("../components/Shell.tsx");
const DICT_SRC = read("../components/i18nDict.ts");

const DOCS_PAGE_REL = "../app/docs/activity/page.tsx";
const DOCS_LAYOUT = read("../app/docs/layout.tsx");
const DOCS_INDEX = read("../app/docs/page.tsx");
const SITEMAP = read("../app/sitemap.ts");
const A11Y_SPEC = read("../../e2e/a11y.spec.ts");
const ACTIVITY_SPEC = read("../../e2e/activity-page.spec.ts");
// The Go half of the same contract, read cross-language on purpose (the idiom
// of chatPage.test.ts): the page's request is only real if the server has the
// route AND honours the same query params.
const GO_REST = read("../../../api/internal/httpapi/rest_events.go");
const GO_ROUTES = read("../../../api/internal/httpapi/rest.go");
const GO_SCOPE = read("../../../api/internal/httpapi/rest_scope.go");
const GO_STORE = read("../../../api/internal/dbstore/event.go");
const GO_HANDLERS = read("../../../api/internal/httpapi/rest_handlers.go");
const GO_FORM_STORE = read("../../../api/internal/dbstore/form.go");

const wsHelper = read("./workspace.ts");

/** The five event names the SSE stream carries — see app/sales/page.tsx. */
const STREAM_ONLY_EVENTS = [
  "sale.paid",
  "sale.failed",
  "sale.refunded",
  "subscription.charged",
  "response.submitted",
];

test("the guard is looking at real files (it cannot pass vacuously)", () => {
  assert.ok(
    PAGE.includes("export default function ActivityPage"),
    "app/activity/page.tsx is not the Activity page source — the guard's anchor moved",
  );
  assert.ok(
    existsSync(path(DOCS_PAGE_REL)),
    `the docs page the inline "?" points at (${DOCS_PAGE_REL}) does not exist`,
  );
  assert.ok(SHELL.includes("export function Shell("), "Shell.tsx is not the app shell source");
  assert.ok(
    GO_REST.includes("func (h *RESTHandlers) listEvents("),
    "rest_events.go is not the events handler source — the cross-language half of this guard moved",
  );
});

test("the page reads the events LIST endpoint, not the SSE stream", () => {
  // The list endpoint itself, with its filters. `event_name` is the filter the
  // card's own grep used to prove no page called the list at all.
  assert.ok(
    PAGE.includes("/api/rest/events?") && PAGE.includes('params.set("event_name"'),
    "the Activity page no longer queries the events LIST endpoint with an event_name filter",
  );
  // The stream is the WRONG channel: it is a live feed of five event names, so
  // reading the audit trail from it would show a fraction of the history while
  // rendering as a complete log.
  assert.ok(
    !PAGE.includes("/api/rest/events/stream"),
    "the Activity page subscribes to the SSE stream — that channel carries only " +
      `${STREAM_ONLY_EVENTS.join(", ")}, so the audit trail would render as a live feed ` +
      "and silently omit every other event",
  );
  assert.ok(
    !/new EventSource\(/.test(PAGE),
    "the Activity page opens an EventSource; the audit trail is the paginated LIST, not a stream",
  );
  // The other two read endpoints stay what they are: count/breakdown exist for
  // analytics, and this page shows rows.
  assert.ok(
    !PAGE.includes("/api/rest/events/count") && !PAGE.includes("/api/rest/events/breakdown"),
    "the page reaches for the count/breakdown analytics endpoints instead of the list",
  );
  // Pagination is server-side: a page that fetched everything and sliced it in
  // the browser would stop being the list endpoint's contract.
  assert.ok(
    PAGE.includes('params.set("page"') && PAGE.includes('params.set("limit"'),
    "the page no longer pages server-side (page/limit are not sent) — it would load the whole table",
  );
});

test("the server has the route and honours the same params (the two halves cannot drift)", () => {
  assert.ok(
    GO_ROUTES.includes('"GET /v1/rest/events"'),
    "GET /v1/rest/events is not registered — the Activity page would 404",
  );
  // The filter the page's event-type dropdown sends. `*` is a PREFIX wildcard,
  // and it is the STORE that implements it (dbstore/event.go), not the handler —
  // so both files are read, or this assertion pins nothing.
  assert.match(
    GO_STORE,
    /strings\.HasSuffix\(f\.EventName, "\*"\)[\s\S]{0,120}?TrimSuffix\(f\.EventName, "\*"\)\+"%"/,
    "dbstore/event.go no longer turns a trailing `*` into a LIKE prefix — the page's " +
      "event-family filter (workspace.*, sale.*) would silently return nothing",
  );
  assert.match(
    GO_REST,
    /f\.Page, f\.Limit = page, limit/,
    "listEvents no longer passes the page/limit it parsed into the filter — the page would always " +
      "receive the store's default page size regardless of what it asked for",
  );
});

// ---------------------------------------------------------------------------
// THE DROPDOWN vs THE RECORDED VOCABULARY (card t_07d8f4a4)
//
// The filter's options were a hand-written list of 11 families while the API
// recorded 14: `chat.message` / `chat.proposal_confirmed` (rest_chat.go),
// `admin.super_admin_action` (rest_admin.go + mcpsrv/superadmin.go) and
// `feedback.*` (rest_feedback.go) had no option, so those rows could only be
// read in the unfiltered list. The list was never wrong about the API's
// capability — a trailing `*` is a prefix match, so `chat.*` already worked —
// it had simply fallen behind what the writers emit, and nothing could notice.
//
// So the families are DERIVED here from the recorder call sites, and both
// directions are asserted: a recorded family with no option fails (the defect),
// and an option with no recorded family fails too (a filter that can only ever
// return an empty page).
// ---------------------------------------------------------------------------

const GO_INTERNAL = fileURLToPath(new URL("../../../api/internal", import.meta.url));

/** Every non-test .go file under api/internal. */
function goSources(dir: string, acc: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) goSources(p, acc);
    else if (name.endsWith(".go") && !name.endsWith("_test.go")) acc.push(p);
  }
  return acc;
}

/**
 * The event families the API WRITES, read from the source that writes them.
 *
 * The call shapes are the eight recorder helpers plus the store call they all
 * funnel into (`Events.Record(...)`); the name literals on those lines are the
 * event names. A dynamically built name (`"form.bulk_"+in.Action`) still yields
 * its family, which is all a prefix filter needs.
 */
function recordedEventFamilies(): Map<string, Set<string>> {
  const CALL = /(?:\b(?:recordEvent|recordEventFor|recordAuthEvent|recordChatEvent|recordChatConfirmEvent|recordFeedbackEvent|recordMCPEvent|recordMCPMediaEvent)\(|Events\.Record\()/;
  const families = new Map<string, Set<string>>();
  for (const file of goSources(GO_INTERNAL)) {
    for (const line of readFileSync(file, "utf8").split("\n")) {
      if (/^func\s/.test(line) || !CALL.test(line)) continue;
      for (const m of line.matchAll(/"([a-z][a-z_]*\.[a-z_.]*)"/g)) {
        const [fam, ...rest] = m[1].split(".");
        if (!families.has(fam)) families.set(fam, new Set());
        families.get(fam)!.add([fam, ...rest].join("."));
      }
    }
  }
  return families;
}

test("the event-type filter offers every family the API records — and no dead one", () => {
  const block = PAGE.match(/const EVENT_CLASSES = \[([\s\S]*?)\] as const;/);
  assert.ok(block, "EVENT_CLASSES is no longer a literal array — this guard's anchor moved");
  const offered = [...block[1].matchAll(/"([a-z_]+)\.\*"/g)].map((m) => m[1]);

  const recorded = recordedEventFamilies();
  // Non-vacuity: if the scan stops seeing the recorders (a helper renamed, a
  // package moved) this fails instead of reporting a clean, empty comparison.
  const FOUND_NOTHING =
    `the recorder scan found ${recorded.size} families (${[...recorded.keys()].join(", ")}) — ` +
    "it is no longer reading the writers, so this guard would pass on anything";
  assert.ok(recorded.size >= 10, FOUND_NOTHING);
  for (const anchor of ["chat", "admin", "feedback", "workspace", "form"]) {
    assert.ok(recorded.has(anchor), `${FOUND_NOTHING} (no ${anchor}.* calls)`);
  }

  // Direction 1 — THE DEFECT: a family that is written but cannot be filtered to.
  const missing = [...recorded.keys()].filter((f) => !offered.includes(f)).sort();
  assert.deepEqual(
    missing,
    [],
    `the API records ${missing.map((f) => `${f}.* (e.g. ${[...recorded.get(f)!][0]})`).join(", ")} ` +
      "but the Activity page's event filter has no option for it — those rows are visible only in " +
      "the unfiltered list (card t_07d8f4a4)",
  );

  // Direction 2 — the other way round: an option nothing ever writes is a filter
  // that can only answer an empty page.
  const dead = offered.filter((f) => !recorded.has(f)).sort();
  assert.deepEqual(
    dead,
    [],
    `the event filter offers ${dead.map((f) => `${f}.*`).join(", ")} but no recorder writes that ` +
      "family — selecting it can only return an empty list",
  );

  // The documented list is the same list, so the docs cannot keep claiming 11
  // families after the page offers 14.
  const DOCS_PAGE = read(DOCS_PAGE_REL);
  const undocumented = offered.filter((f) => !DOCS_PAGE.includes(`${f}.*`));
  assert.deepEqual(
    undocumented,
    [],
    `/docs/activity does not mention ${undocumented.map((f) => `${f}.*`).join(", ")} — the page ` +
      "offers a filter the guide does not list (AGENTS.md DOCUMENTATION PARITY)",
  );
});

test("EVERY call is workspace-scoped — a foreign slug is refused, never resolved to another account", () => {
  // The page builds the URL as `...?${params}${sep}${sp.replace("?","")}`, where
  // `sp` is the shared helper's result. Both halves are asserted, because
  // either one alone passes with a bare URL: a `${sep}` with no `sp` scope, or a
  // `workspaceQuery()` bound to a variable the URL then ignores.
  assert.match(
    PAGE,
    /const sp = workspaceQuery\(\);/,
    "the page never calls workspaceQuery() — its request would be unscoped",
  );
  const calls = [...PAGE.matchAll(/fetch(?:Json<[^>]*>)?\(\s*(`[^`]*`|"[^"]*")/g)].map((m) => m[1]);
  assert.ok(calls.length >= 1, `expected the page's events fetch, found none: ${JSON.stringify(calls)}`);
  for (const u of calls) {
    assert.ok(
      u.includes("${sep}${sp.replace("),
      `the request ${u} does not append the workspace scope — an unscoped call resolves the user's ` +
        "FIRST account, not the workspace they selected, so the page would show another " +
        "workspace's audit trail",
    );
  }
  // No hand-rolled query string: the active-workspace rule lives in ONE place.
  const code = PAGE.replace(/\/\*[\s\S]*?\*\//g, "")
    .split("\n")
    .filter((l) => !/^\s*(\/\/|\*)/.test(l))
    .join("\n");
  assert.ok(
    !/[?&]workspace=/.test(code),
    "app/activity/page.tsx builds ?workspace= by hand instead of using workspaceQuery() — the " +
      "active-workspace rule must stay in ONE place (lib/workspace.ts)",
  );
  assert.ok(
    PAGE.includes('import { workspaceQuery } from "../../lib/workspace"'),
    "the page does not import workspaceQuery from the shared helper",
  );
  assert.match(
    wsHelper,
    /export function workspaceQuery\(\): string \{\s*const sp = [^;]+;\s*return sp \? `\?workspace=\$\{encodeURIComponent\(sp\)\}` : "";/,
    "lib/workspace.ts workspaceQuery() no longer returns `?workspace=<slug>` — the page would import a " +
      "helper that scopes nothing",
  );
  // The scope SOURCE is the helper's own concern, but it must still consult a
  // slug at all: the confirmed scope it prefers, falling back to the stored one
  // while the session has not answered (t_da4b2031). Dropping the localStorage
  // fallback would leave the very first request of every page unscoped.
  assert.match(
    wsHelper,
    /confirmedScope \?\? workspaceScope\(\)/,
    "workspaceQuery() no longer falls back to the stored slug — before the session answers, every first " +
      "request of every page would go out UNSCOPED and the server would resolve the user's FIRST account",
  );
  // And the SERVER is what makes the scope safe: a slug that is not the
  // caller's is refused, and the refusal is a 403 the page already handles
  // (wsfetch treats 401/403 as a dead session → /login).
  assert.match(
    GO_SCOPE,
    /func \(h \*RESTHandlers\) scopeOr403\(/,
    "rest_scope.go no longer defines scopeOr403 — the events endpoints must resolve the ?workspace= scope",
  );
  assert.match(
    GO_REST,
    /h\.scopeOr403\(w, r, actx\)/,
    "listEvents no longer resolves the workspace scope — ?workspace= would be ignored and a " +
      "two-workspace merchant would read their first account's trail",
  );
});

test("the page cannot be read as a complete log when a request failed", () => {
  // A failed load must not render the same thing as an empty trail: the empty
  // branch falls back to `err` and offers a retry (issue #503's rule).
  assert.ok(
    PAGE.includes("{err || t(\"act.empty\")}"),
    "the empty state no longer surfaces the load error — a broken request would read as an empty log",
  );
  assert.ok(
    PAGE.includes("t(\"common.retry\")"),
    "a failed load has no retry action — the user is left on a dead end",
  );
  assert.ok(
    PAGE.includes("runLoad("),
    "the page hand-rolls its failure handling instead of going through runLoad (lib/wsfetch.ts) — " +
      "a 401 would not redirect to /login",
  );
});

test("the shell keeps the nav row and the active key in step", () => {
  assert.ok(
    SHELL.includes('{ key: "activity", href: "/activity"'),
    "the sidebar has no /activity row — the surface would be unreachable by navigation",
  );
  assert.ok(
    /active:[\s\S]{0,400}?"activity"/.test(SHELL),
    'the Shell "active" union does not accept "activity"',
  );
  assert.match(
    PAGE,
    /<Shell active="activity">/,
    'the page does not render <Shell active="activity"> — the sidebar would highlight no entry, or ' +
      "the wrong one",
  );
  // The loading state uses the key too, or the highlight flickers on load
  // (lib/pageShellLoading.test.ts pins the shape; this pins THIS page's key).
  assert.match(
    PAGE,
    /<PageLoading active="activity" \/>/,
    "the in-flight state does not load under the same nav key as the page",
  );
  // The nav label is a real dictionary key, not a literal.
  assert.ok(
    SHELL.includes('label: t("act.title")'),
    "the nav row renders a hardcoded label instead of t(\"act.title\")",
  );
});

test("the docs page is reachable from every surface a reader looks at", () => {
  assert.ok(
    PAGE.includes('href="/docs/activity"'),
    'the Activity page has no inline "?" doc link (AGENTS.md DOCUMENTATION PARITY rule 4)',
  );
  assert.ok(
    DOCS_LAYOUT.includes('"/docs/activity"'),
    "the docs sidebar does not link /docs/activity — the page would be unreachable from the docs",
  );
  // The /docs index restructure (PR 675) made the entries DATA: a feature entry
  // links its guide with `href: "…"`, and only the quick-link row renders a JSX
  // `href="…"`. Both are a real route to the guide, so assert the ROUTE rather
  // than one of the two shapes — pinning the JSX form alone is what this
  // assertion did before the rebase, and it went red on a page that was correct.
  assert.ok(
    DOCS_INDEX.includes('href: "/docs/activity"') || DOCS_INDEX.includes('href="/docs/activity"'),
    "the /docs index has no route to the activity-log guide",
  );
  assert.ok(SITEMAP.includes('"/docs/activity"'), "/docs/activity is missing from sitemap.ts");
  assert.ok(
    A11Y_SPEC.includes('path: "/docs/activity"'),
    "the a11y gate's PAGES list does not scan /docs/activity — src/lib/docsA11y.test.ts fails the " +
      "build for a docs page left outside the scan",
  );
});

test("the DASHBOARD route is inside the a11y gate, deliberately", () => {
  // A dashboard route is not auto-scanned: docsA11y.test.ts polices /docs/* and
  // a11y.spec.ts's PAGES list is public + docs routes. Without this spec the new
  // page would sit outside the one gate that renders a real page in a browser —
  // and nobody would notice, which is how the dialogs were missed (t_d334fb02).
  assert.ok(
    ACTIVITY_SPEC.includes("AxeBuilder"),
    "e2e/activity-page.spec.ts is not an axe spec",
  );
  assert.ok(
    ACTIVITY_SPEC.includes('"/activity"') && ACTIVITY_SPEC.includes("/api/rest/events"),
    "the dashboard spec does not reach /activity with a stubbed events response — it cannot be scanning " +
      "the page under test",
  );
  // It must assert reachability, so the scan cannot pass against a bounced page
  // (a spec that scans /landing after an auth bounce reports a clean pass for a
  // page this card never rendered).
  assert.match(
    ACTIVITY_SPEC,
    /toBeVisible|toHaveCount|toBeFocused/,
    "the spec makes no reachability assertion — an unauthenticated bounce would scan the wrong page",
  );
});

test("the two date filters send an instant the API can actually parse", () => {
  // `from`/`to` are RFC3339 and an unparseable value is IGNORED in silence
  // (eventFilterFromRequest). A bare `yyyy-mm-dd` from a date input therefore
  // renders a filter that does nothing, which is worse than no filter.
  assert.match(
    PAGE,
    /`\$\{date\}T\$\{end \? "23:59:59" : "00:00:00"\}Z`/,
    "dayBound no longer widens a date input to an RFC3339 instant — the API would drop an " +
      "unparseable from/to and answer the unfiltered list while the UI shows a filter applied",
  );
  assert.match(
    GO_REST,
    /time\.Parse\(time\.RFC3339, v\)/,
    "rest_events.go no longer parses from/to as RFC3339 — this page's contract moved",
  );
});

test("the page states the retention it cannot show past", () => {
  assert.ok(
    PAGE.includes('t("act.retention")'),
    "the page does not state the retention window — a reader would take an empty older range as " +
      "\"nothing happened\" rather than \"swept\"",
  );
  // And the numbers in that sentence match the server's defaults, so the page
  // cannot drift into claiming a window the sweep does not implement.
  const defaults = read("../../../api/internal/config/config.go");
  assert.match(
    defaults,
    /parsePositiveInt\(os\.Getenv\("EVENT_RETENTION_DAYS"\), 90\)/,
    "the default event retention is no longer 90 days — the Activity page quotes it",
  );
  assert.match(
    defaults,
    /parsePositiveInt\(os\.Getenv\("EVENT_AUDIT_RETENTION_DAYS"\), 180\)/,
    "the audit-class retention is no longer 180 days — the Activity page and the docs quote it",
  );
});

test("the orphan dictionary keys are gone, and the live MCP keys are not", () => {
  // Card t_eb09b25a's own measurement: 14 keys with zero call sites, 11 killed
  // by d42a972 (#311) and 3 more by 90c2d74 (#428).
  const ORPHANS = [
    "set.mcpTitle",
    "set.mcpHelp",
    "set.mcpNoKeys",
    "set.noAgents",
    "set.mcpHistoryTitle",
    "set.mcpHistoryHelp",
    "set.mcpNoHistory",
    "set.mcpColTime",
    "set.mcpColTool",
    "set.mcpColAgent",
    "set.mcpColResult",
    "set.mcpSuccess",
    "set.mcpFailed",
    "set.mcpPageOf",
  ];
  for (const k of ORPHANS) {
    assert.ok(
      !(k in DICT),
      `${k} is back in the dictionary with no call site — it is the dead-key drift this card removed`,
    );
    assert.ok(
      !DICT_SRC.includes(`"${k}"`),
      `${k} is declared in i18nDict.ts again`,
    );
  }
  // The keys the app REALLY renders. A sweep that took these would blank the UI,
  // and it would look like a dictionary edit nobody connected. They do not all
  // live in one file: the MCP block is in profile/page.tsx and the Chip status
  // line that uses set.activeMode is in settings/page.tsx — so a guard that only
  // read profile/ would report a live key as dead and get it deleted.
  const LIVE_KEYS: Array<[string, string]> = [
    ["set.serverUrl", "../app/profile/page.tsx"],
    ["set.clientId", "../app/profile/page.tsx"],
    ["set.revoke", "../app/profile/page.tsx"],
    ["set.activeMode", "../app/settings/page.tsx"],
  ];
  for (const [k, where] of LIVE_KEYS) {
    assert.ok(k in DICT, `${k} was deleted with the orphans — ${where} renders it`);
    const src = read(where);
    assert.ok(
      src.includes(`t("${k}"`) || src.includes(`"${k}"`),
      `${where} no longer uses ${k}; if it really stopped, remove the key deliberately rather than ` +
        "leaving it for the next sweep",
    );
  }
});

test("every act.* dictionary key defines all five locales", () => {
  const keys = Object.keys(DICT).filter((k) => k.startsWith("act."));
  assert.ok(keys.length >= 14, `expected the Activity dictionary, got ${keys.length} keys`);
  const missing: string[] = [];
  for (const k of keys) {
    const entry = DICT[k as keyof typeof DICT] as Record<string, unknown>;
    for (const lang of LANGS) {
      const v = entry[lang];
      if (typeof v !== "string" || v.trim() === "") missing.push(`${k}.${lang}`);
    }
  }
  assert.deepEqual(missing, [], `missing translations:\n  ${missing.join("\n  ")}`);
  // And the page uses them through t(), so the guard is not just asserting a
  // dictionary nothing renders from.
  for (const k of ["act.title", "act.subtitle", "act.empty", "act.loadErr", "act.retention"]) {
    assert.ok(PAGE.includes(`t("${k}"`), `app/activity/page.tsx does not render ${k}`);
  }
});

// ---------------------------------------------------------------------------
// THE EVENT CELL vs THE RECORDED VOCABULARY (card t_804040d3)
//
// The Activity table printed the machine token — `sale.paid`,
// `workspace.renamed`, `admin.super_admin_action` — into the merchant's own
// table. The label now comes from the dictionary (src/lib/activityEvents.ts's
// EVENT_LABEL_KEYS), and the raw token stays on the cell as its `title`.
//
// The map is declared by hand, so it can fall behind the recorders exactly the
// way the event-family filter did (t_07d8f4a4). Both directions are pinned here,
// off the same recorder scan that already reads the families:
//
//   * a name the API records with no label  -> the merchant reads a raw token
//   * a label for a name nothing records    -> dead weight, or a typo in the
//                                              event name
//
// And because three names are built at the CALL SITE (`"form.status_"+status`,
// `"form.bulk_"+action`, `"product.bulk_"+action`), their expansions are read
// out of the Go source too — the status list through the `FormStatus*`
// constants the store defines, the bulk actions from the switch that builds the
// name. That is what stops a new bulk action shipping as a raw token.
// ---------------------------------------------------------------------------

/** Every literal event name a recorder call passes, plus the prefixes it builds. */
function recordedEventNames(): { names: Set<string>; prefixes: Set<string> } {
  const CALL =
    /(?:\b(?:recordEvent|recordEventFor|recordAuthEvent|recordChatEvent|recordChatConfirmEvent|recordFeedbackEvent|recordMCPEvent|recordMCPMediaEvent)\(|Events\.Record\()/;
  const names = new Set<string>();
  const prefixes = new Set<string>();
  for (const file of goSources(GO_INTERNAL)) {
    for (const line of readFileSync(file, "utf8").split("\n")) {
      if (/^func\s/.test(line) || !CALL.test(line)) continue;
      for (const m of line.matchAll(/"([a-z][a-z_]*\.[a-z_.]*)"/g)) names.add(m[1]);
      // `"form.status_"+in.Status` — the literal stops at the `_` that joins it.
      for (const m of line.matchAll(/"([a-z][a-z_]*\.[a-z_]+_)"\s*\+/g)) prefixes.add(m[1]);
    }
  }
  // A prefix is not a name; it is the head of a family built at the call site.
  for (const p of prefixes) names.delete(p);
  return { names, prefixes };
}

/** The Go source of the top-level function containing `marker`. */
function goFunctionAround(src: string, marker: string, where: string): string {
  const at = src.indexOf(marker);
  assert.ok(at >= 0, `${where} no longer contains ${marker} — this guard's anchor moved`);
  const start = src.lastIndexOf("\nfunc ", at);
  const end = src.indexOf("\nfunc ", at);
  assert.ok(start >= 0, `${marker} in ${where} is not inside a top-level Go function`);
  return src.slice(start + 1, end < 0 ? undefined : end);
}

/** The `case` values of one `switch <head> {` block, resolving Go constants. */
function goSwitchValues(body: string, head: string, consts: Map<string, string>): string[] {
  const open = body.indexOf(`switch ${head} {`);
  assert.ok(open >= 0, `no \`switch ${head} {\` in the function that records this event — the guard's anchor moved`);
  // Brace-match from the switch's own `{`, so a nested block cannot end it early.
  let depth = 0;
  let i = open + `switch ${head} `.length;
  const from = i + 1;
  for (; i < body.length; i++) {
    if (body[i] === "{") depth++;
    else if (body[i] === "}") {
      depth--;
      if (depth === 0) break;
    }
  }
  const block = body.slice(from, i);
  const out: string[] = [];
  for (const m of block.matchAll(/case\s+([^:]+):/g)) {
    for (const raw of m[1].split(",")) {
      const token = raw.trim();
      const literal = token.match(/^"([^"]*)"$/);
      if (literal) {
        out.push(literal[1]);
        continue;
      }
      // `dbstore.FormStatusDraft` — resolve through the store's constants.
      const resolved = consts.get(token.replace(/^dbstore\./, ""));
      assert.ok(resolved, `the switch on \`${head}\` names ${token}, which is not a \`X = "…"\` constant this guard can read`);
      out.push(resolved!);
    }
  }
  assert.ok(out.length > 0, `the switch on \`${head}\` has no case values — the guard is reading nothing`);
  return out;
}

/** The form statuses the store defines (`FormStatusDraft = "draft"`, …). */
function goFormStatuses(): Map<string, string> {
  const values = new Map<string, string>();
  for (const m of GO_FORM_STORE.matchAll(/\b(FormStatus[A-Za-z]+)\s*=\s*"([^"]*)"/g)) values.set(m[1], m[2]);
  assert.ok(
    values.size >= 4,
    `could not read the FormStatus* constants in api/internal/dbstore/form.go (found ${values.size})`,
  );
  return values;
}

test("the event cell renders a dictionary label, and keeps the machine name as its tooltip", () => {
  assert.ok(
    PAGE.includes('import { eventLabelKey } from "../../lib/activityEvents"'),
    "app/activity/page.tsx no longer imports eventLabelKey — the cell would print the raw event name again",
  );
  assert.match(
    PAGE,
    /const labelKey = eventLabelKey\(e\.event_name\);/,
    "the row no longer resolves a label key from the event name",
  );
  assert.match(
    PAGE,
    /\{labelKey \? t\(labelKey\) : e\.event_name\}/,
    "the cell does not render the dictionary label (the raw token is only a fallback)",
  );
  assert.ok(
    PAGE.includes("title={e.event_name}"),
    "the raw event name is not the cell's title — an operator can no longer see the code the API answers in",
  );
});

test("EVERY event the API records has a label — in all five locales — and no label is dead", () => {
  const { names, prefixes } = recordedEventNames();

  // Non-vacuity: if the scan stops seeing the recorders this fails instead of
  // comparing an empty set against an empty map.
  assert.ok(
    names.size >= 40,
    `the recorder scan found ${names.size} event names (${[...names].join(", ")}) — it is no longer reading the writers`,
  );
  for (const must of ["sale.captured", "workspace.deleted", "form.created", "mcp.tool_call", "auth.login"]) {
    assert.ok(names.has(must), `the recorder scan no longer sees ${must} — this guard would pass on anything`);
  }

  // The three names built at the call site, expanded from the Go source.
  assert.ok(prefixes.has("form.status_"), '`"form.status_"+in.Status` is no longer how a status change is recorded');
  assert.ok(prefixes.has("form.bulk_"), '`"form.bulk_"+in.Action` is no longer how a bulk form action is recorded');
  assert.ok(
    prefixes.has("product.bulk_"),
    '`"product.bulk_"+in.Action` is no longer how a bulk product action is recorded',
  );

  const statusConsts = goFormStatuses();
  const statusBody = goFunctionAround(GO_HANDLERS, '"form.status_"', "rest_handlers.go");
  const formBulkBody = goFunctionAround(GO_HANDLERS, '"form.bulk_"', "rest_handlers.go");
  const productBulkBody = goFunctionAround(GO_HANDLERS, '"product.bulk_"', "rest_handlers.go");
  const productBulkAction = productBulkBody.match(/in\.Action\s*!=\s*"([a-z_]+)"/);
  assert.ok(
    productBulkAction,
    'the product bulk handler no longer guards `in.Action != "…"` — the only action it accepts cannot be read',
  );

  const required = new Set<string>(names);
  for (const s of goSwitchValues(statusBody, "in.Status", statusConsts)) required.add(`form.status_${s}`);
  for (const a of goSwitchValues(formBulkBody, "in.Action", statusConsts)) required.add(`form.bulk_${a}`);
  required.add(`product.bulk_${productBulkAction![1]}`);

  // Direction 1 — THE DEFECT: a recorded event with no label is a raw token in
  // the merchant's table, in every locale.
  const unlabelled = [...required].filter((n) => !(n in EVENT_LABEL_KEYS)).sort();
  assert.deepEqual(
    unlabelled,
    [],
    `the API records ${unlabelled.join(", ")} but src/lib/activityEvents.ts has no label for it — ` +
      "the Activity table would print the raw event name to the merchant (card t_804040d3)",
  );

  // Direction 2 — the other way round: a label for a name nothing records is
  // dead weight or a typo, and either way the row it was meant for falls back
  // to its raw token.
  const invented = Object.keys(EVENT_LABEL_KEYS)
    .filter((n) => !required.has(n))
    .sort();
  assert.deepEqual(
    invented,
    [],
    `src/lib/activityEvents.ts labels ${invented.join(", ")}, which no recorder writes — a rename or a typo`,
  );

  // Each key exists in the dictionary with all five locales. A key that names a
  // real event but is absent from i18nDict renders the KEY itself (`t()`'s
  // fallback), which is worse than the token it replaced.
  const missing: string[] = [];
  for (const [name, key] of Object.entries(EVENT_LABEL_KEYS)) {
    const entry = (DICT as Record<string, Record<string, unknown>>)[key];
    if (!entry) {
      missing.push(`${name} -> ${key} (no such dictionary key)`);
      continue;
    }
    for (const lang of LANGS) {
      const v = entry[lang];
      if (typeof v !== "string" || v.trim() === "") missing.push(`${name} -> ${key}.${lang}`);
    }
  }
  assert.deepEqual(missing, [], `event labels with no usable translation:\n  ${missing.join("\n  ")}`);

  // A label that IS the event name is the defect wearing a dictionary entry:
  // `sale.paid` must not ship as the English "translation" of `sale.paid`.
  const echoed = Object.entries(EVENT_LABEL_KEYS)
    .filter(([name, key]) => {
      const en = (DICT as Record<string, Record<string, unknown>>)[key]?.en;
      return typeof en === "string" && en.includes(name);
    })
    .map(([name]) => name);
  assert.deepEqual(echoed, [], `these labels still carry the machine name in English: ${echoed.join(", ")}`);
});
