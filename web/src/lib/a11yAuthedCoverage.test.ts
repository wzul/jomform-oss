// The a11y gate must actually SCAN a signed-in dashboard page (card t_b589a807),
// and every signed-in route must be one of them (card t_9500edf3).
//
// THE BUG. `web/e2e/a11y.spec.ts` holds the axe PAGE LIST and it was seven
// PUBLIC routes only (`/login`, `/landing`, `/reset-password`, `/magic-link`,
// `/brand`, `/legal/privacy`, `/legal/terms`). Every authenticated page —
// `/profile`, `/settings`, `/forms`, `/sales`, … — was outside the gate, so
// `npm run a11y` was GREEN while `/profile` carried 5 and `/settings` 10
// critical axe `aria-required-parent` nodes (a `role="tab"` whose required
// parent `role="tablist"` did not exist anywhere in either file). `t_d334fb02`
// closed the dialog half of that gap; `t_b589a807` closed the page half for
// `/profile`, and THIS contract now covers the routes `t_9500edf3` measured as
// still unreachable: twelve of them (`/sales`, `/deliveries`, `/templates`,
// `/media`, `/chat`, `/spam`, `/activity`, `/feedback`, `/form-settings`,
// `/builder`, `/connect` — and `/subscriptions`, whose DOCUMENT had no
// full-document scan either) plus the two shells that only exist to redirect.
//
// TWO KINDS OF ENTRY, and the difference is stated rather than smoothed over.
// A route entry names a page that is LOADED and SCANNED. `/space-picker` and
// `/post-login` are not that: the first is a permanent 307 to
// `/workspace-picker`, the second resolves the post-login target and
// `location.replace`s within a frame, so there is no document of their own for
// axe to judge — the scan under their name is of the route they land on, and
// `a11y-authed-pages.spec.ts` asserts that the landing route is itself covered.
// They are listed because the card named them and because an entry for them is
// how the next reader learns they are covered BY their target; they are NOT
// evidence that the shell was scanned, and the `what` text says so.
//
// WHY A SOURCE-CONTRACT TEST AND NOT A BROWSER TEST. This repo has no DOM/JSX
// unit runner (no vitest/jest — see src/lib/buttonWrap.test.ts). What CANNOT
// regress silently is the two-part contract that keeps the signed-in coverage
// real:
//
//   (a) REACHABLE ROUTES: a route the gate claims to scan must be a page that
//       exists on disk. `page.goto` resolves for a 404 too, and a scan of a
//       Next.js "not found" page finds no landmarks and no controls — so a
//       PAGES entry with a typo PASSES GREEN having scanned nothing
//       (t_81b2a928). The route→source-file mapping below is that half, checked
//       here instead of only at runtime.
//   (b) THE SPECS THAT SCAN THEM ARE IN THE GATE: the `a11y` project in
//       `playwright.config.ts` selects by FILENAME PREFIX (`testMatch:
//       /a11y.*\.spec\.ts/`), so a signed-in axe spec is in the gate by its name
//       alone — and a spec that stops visiting the route, or stops running axe,
//       has silently reduced coverage. Both are asserted below, per route.
//
// The assertion is deliberately about the CONTRACT (route exists, a gate spec
// visits it and runs axe), never about a class name or an attribute: the
// attribute that fixes the defect is measured by the browser spec itself, which
// also neutralises it in the live DOM and requires the axe rule to fire again.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";

const pathOf = (rel: string) => fileURLToPath(new URL(rel, import.meta.url));

const E2E_DIR = pathOf("../../e2e");
const CONFIG = readFileSync(pathOf("../../playwright.config.ts"), "utf8");

/** The axe scanner: a spec that visits a route without it is not a scan. */
const SCANNER = "AxeBuilder";

/**
 * A route a signed-in surface must be scanned at, and the page file behind it.
 * `appFile` is what makes the route REAL — the t_81b2a928 half of the contract.
 */
const AUTHED_ROUTES: { route: string; appFile: string; what: string }[] = [
  {
    route: "/profile",
    appFile: "../app/profile/page.tsx",
    what: "the tab strip + identity/security/passkeys/social/MCP panels",
  },
  {
    route: "/settings",
    appFile: "../app/settings/page.tsx",
    what: "the eleven-tab strip (card t_4d32da9a added its axe arm)",
  },
  {
    route: "/subscriptions",
    appFile: "../app/subscriptions/page.tsx",
    what: "the subscription table and its per-row write actions (card t_0fdbe189); its ROW is measured by a11y-subscriptions-row, its DOCUMENT by a11y-authed-pages",
  },
  // Card t_9500edf3: the twelve routes that no a11y-family spec scanned at all.
  // Every one is reached with the session + API stubbed at the browser level, and
  // every one gets a FULL-DOCUMENT axe scan in a11y-authed-pages.spec.ts — the
  // entry exists so the route cannot quietly fall out again.
  {
    route: "/sales",
    appFile: "../app/sales/page.tsx",
    what: "the sales table, filters, search, date range and CSV export",
  },
  {
    route: "/deliveries",
    appFile: "../app/deliveries/page.tsx",
    what: "the email-delivery table, its event-type filter and the per-sale timeline",
  },
  {
    route: "/templates",
    appFile: "../app/templates/page.tsx",
    what: "the templates list and its save/clone/rename/delete actions",
  },
  {
    route: "/media",
    appFile: "../app/media/page.tsx",
    what: "the media grid, its upload control, filters and per-item actions",
  },
  {
    route: "/chat",
    appFile: "../app/chat/page.tsx",
    what: "the assistant page, its transcript region and its composer",
  },
  {
    route: "/spam",
    appFile: "../app/spam/page.tsx",
    what: "the spam review queue (flagged submissions + flagged sales)",
  },
  {
    route: "/activity",
    appFile: "../app/activity/page.tsx",
    what: "the workspace audit trail, its filters and its pagination",
  },
  {
    route: "/feedback",
    appFile: "../app/feedback/page.tsx",
    what: "the feedback thread list and the file-a-report form",
  },
  {
    route: "/form-settings",
    appFile: "../app/form-settings/page.tsx",
    what: "all seven tabs of the per-form settings page (it carried nine critical `label` nodes across three of them)",
  },
  {
    route: "/builder",
    appFile: "../app/builder/page.tsx",
    what: "the builder's own chrome: palette, canvas and publish controls",
  },
  {
    route: "/connect",
    appFile: "../app/connect/page.tsx",
    what: "the social-connect landing step",
  },
  {
    route: "/workspace-picker",
    appFile: "../app/workspace-picker/page.tsx",
    what: "the multi-workspace login picker",
  },
  {
    route: "/space-picker",
    appFile: "../app/space-picker/page.tsx",
    what: "a legacy PATH only: it 307s to /workspace-picker, so the document scanned under this route is the picker's, and a11y-authed-pages asserts that redirect rather than claiming a scan of the shell",
  },
  {
    route: "/post-login",
    appFile: "../app/post-login/page.tsx",
    what: "a redirect shell: it resolves the post-login target and replaces the location within a frame, so the document scanned under it is the route it lands on (asserted in a11y-authed-pages)",
  },
];

/** Specs selected by the `a11y` project: the prefix IS the selection rule. */
const A11Y_SPECS = readdirSync(E2E_DIR).filter(
  (f) => f.startsWith("a11y") && f.endsWith(".spec.ts"),
);

/** Does this spec text visit `route`? A quoted/bracketed path, or one that
 *  continues into a query string (`/settings?tab=members`). */
function visits(text: string, route: string): boolean {
  return new RegExp(`${route.replace(/[/]/g, "\\/")}(\\?|["'\`$)])`).test(text);
}

test("the a11y project still selects its specs by the a11y filename prefix", () => {
  assert.match(
    CONFIG,
    /name:\s*"a11y"[\s\S]{0,200}?testMatch:\s*\/a11y\.\*\\\.spec\\\.ts\//,
    "playwright.config.ts no longer selects the `a11y` project with testMatch: /a11y.*\\.spec\\.ts/ — " +
      "the signed-in specs below are in the gate by that prefix and this contract has to match the selector",
  );
});

test("every signed-in route the gate claims to scan is a page on disk", () => {
  for (const { route, appFile } of AUTHED_ROUTES) {
    assert.ok(
      existsSync(pathOf(appFile)),
      `the a11y gate is expected to scan ${route}, but ${appFile.replace("../", "web/")} does not exist — ` +
        `a scan of a route with no page reads a 404 and finds nothing (t_81b2a928)`,
    );
  }
});

test("each signed-in route is visited by an a11y-family spec that runs axe", () => {
  for (const { route, what } of AUTHED_ROUTES) {
    // `a11y.spec.ts` is the public PAGES list; the signed-in surfaces live in
    // sibling specs (the session has to be stubbed, which its `page.goto`-only
    // shape does not do), so the public file cannot satisfy this on its own.
    const scanners = A11Y_SPECS.filter((f) => f !== "a11y.spec.ts").filter((f) => {
      const text = readFileSync(pathOf(`../../e2e/${f}`), "utf8");
      return visits(text, route) && text.includes(SCANNER);
    });
    assert.ok(
      scanners.length > 0,
      `${route} (${what}) is visited by NO a11y-family spec that runs ${SCANNER}. ` +
        `The axe gate would be green for a critical violation on it — the exact gap this contract exists for. ` +
        `Specs found: ${A11Y_SPECS.join(", ")}`,
    );
  }
});

/**
 * The twelve routes card `t_9500edf3` exists for must still be ENTRIES in the
 * table of the spec that scans them.
 *
 * WHY THIS IS A SEPARATE RULE FROM THE ONE ABOVE. That rule is a SUBSTRING
 * search over the family's texts, so ANY sibling spec that mentions the route
 * satisfies it — `a11y-singlechar-text.spec.ts` names eleven of these twelve in
 * its own `ROUTES` table, so deleting this file's arm for one of them would
 * leave the rule above green (the same weakness QA filed against the `/profile`
 * entry as `t_2275886d`). This one reads the scanner's own `ROUTES` table out of
 * `a11y-authed-pages.spec.ts` and requires every route below to be an entry, so
 * an arm that is deleted or renamed fails here BY NAME instead.
 */
const A11Y_AUTHED_PAGES = "a11y-authed-pages.spec.ts";

/** The card's twelve, without any query string (the table carries `?form=`). */
const REQUIRED_SCANNED_ROUTES = [
  "/sales",
  "/deliveries",
  "/templates",
  "/media",
  "/chat",
  "/spam",
  "/subscriptions",
  "/activity",
  "/feedback",
  "/form-settings",
  "/builder",
  "/connect",
];

test("a11y-authed-pages still carries an arm for every route this contract exists for", () => {
  const file = `${E2E_DIR}/${A11Y_AUTHED_PAGES}`;
  assert.ok(existsSync(file), `${A11Y_AUTHED_PAGES} is gone — the signed-in scans it carries are too`);
  const src = readFileSync(file, "utf8");
  const listed = [...src.matchAll(/path:\s*"([^"]+)"/g)].map((m) => m[1].split("?")[0]);
  assert.ok(
    listed.length > 0,
    `no \`path: "…"\` entry was parsed out of ${A11Y_AUTHED_PAGES} — this rule would be vacuous` +
      ` (its ROUTES table is a different shape now; re-point this reader at it rather than deleting the rule)`,
  );
  assert.deepEqual(
    REQUIRED_SCANNED_ROUTES.filter((r) => !listed.includes(r)),
    [],
    `route(s) with no arm left in ${A11Y_AUTHED_PAGES}: the table holds ${JSON.stringify(listed)}. ` +
      `A route deleted from that table is a route axe no longer scans — and the substring rule above cannot ` +
      `see it, because another spec in the family names the same path`,
  );
});
