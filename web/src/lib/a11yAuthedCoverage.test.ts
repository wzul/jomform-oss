// The a11y gate must actually SCAN a signed-in dashboard page (card t_b589a807).
//
// THE BUG. `web/e2e/a11y.spec.ts` holds the axe PAGE LIST and it was seven
// PUBLIC routes only (`/login`, `/landing`, `/reset-password`, `/magic-link`,
// `/brand`, `/legal/privacy`, `/legal/terms`). Every authenticated page —
// `/profile`, `/settings`, `/forms`, `/sales`, … — was outside the gate, so
// `npm run a11y` was GREEN while `/profile` carried 5 and `/settings` 10
// critical axe `aria-required-parent` nodes (a `role="tab"` whose required
// parent `role="tablist"` did not exist anywhere in either file). `t_d334fb02`
// closed the dialog half of that gap; this is the page half.
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
