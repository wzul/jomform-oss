// The a11y gate must SCAN every route that paints text on a DARK slab
// (card t_2db0fc94).
//
// THE GAP THIS CLOSES (measured on main, not inferred)
//
//   `src/lib/emeraldText.test.ts` judges a text token against the surface it is
//   painted on — and it deliberately ABSTAINS (its rule b) when the token's own
//   element declares a background the source scan cannot composite: a dark slab
//   (`bg-slate-900`), a gradient, an alpha tint. The abstention is CORRECT (the
//   pairing has to be measured, not class-name-read), and its comment justifies
//   it by handing the case to the browser: "…the browser gate's job (both pages
//   are in its route list), not this rule's."
//
//   That premise was true for `/landing` and FALSE for `/mcp`. Measured:
//
//     grep -nE '\{ *name: *"(mcp|landing)"' web/e2e/a11y.spec.ts
//       16:  { name: "landing", path: "/landing" }
//       (no /mcp)
//     grep -rn '"/mcp"' web/e2e/          -> only `docs-mcp` = "/docs/mcp"
//
//   `/docs/mcp` is a DIFFERENT route (a marketing prose page). So the
//   `bg-slate-900` + `text-emerald-300` snippet on `src/app/mcp/mcp-client.tsx`
//   was judged by NEITHER layer: the class guard abstains by design, and the axe
//   scan never rendered the route. Both green, coverage zero. (The pairing
//   itself measures 11.72:1 and is fine — the gap is that nothing would have
//   noticed if it were not.)
//
// WHY A SOURCE GUARD AND NOT JUST THE SCAN ENTRY
//
//   Adding `/mcp` to `e2e/a11y.spec.ts` fixes today's page. It does not fix the
//   CLASS: the next dark-slab page can ship on a route the list omits, and the
//   two layers go quiet on it in exactly the same way. This file derives the
//   carriers from the SOURCE (through the repo's own JSX parser, so a class
//   quoted in a comment cannot count — see the `codeOnly` header) and requires
//   each one to be either a path the scan visits, or a route whose OWN source
//   declares the session chrome (which is what makes it unreachable without a
//   session — the public scan serves /landing instead).
//
//   A third case FAILS here, naming the route, the file and the token. That is
//   the whole point of writing it down: `/mcp` was the third case.
//
// The abstention arm is deliberately NARROW: it reads the route's own file, not
// its `layout.tsx`. The root layout wraps every page in `SessionProvider`, so a
// layout-chain check would classify the whole app as session-gated and make this
// rule vacuous — a guard that abstains everywhere reads exactly like one that
// passes.
//
// WHAT IT DOES NOT COVER (stated, not implied)
//
//   A `.tsx` outside `src/app/` has no route of its own: it is rendered by
//   whichever page imports it, and a source scan cannot follow that. Those files
//   are outside this walk by construction (`docsA11y.test.ts` has the same limit
//   for the docs tree). `bg-slate-100`-and-up, `/nn` translucent tints and the
//   brand's own `--jf-*` tokens are out of scope by the same reasoning: the
//   surface behind them is not a single resolved colour.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { codeOnly, jsxTags, readSource, tsxFiles } from "./jsxSource.ts";

const SRC = fileURLToPath(new URL("../", import.meta.url)); // web/src/
const SPEC = readFileSync(
  fileURLToPath(new URL("../../e2e/a11y.spec.ts", import.meta.url)),
  "utf8",
);

/**
 * A single class token that paints a SOLID DARK background — the shape a
 * colour-class guard has to hand to the browser, because the ratio depends on
 * the foreground composited over it. Deliberately NOT the light steps, NOT
 * `/nn` (translucent — what is behind it is unknown) and NOT a `--jf-*` token
 * (those carry their own measured pairing).
 */
const DARK_SLAB_RE =
  /^((?:[a-z-]+:)*)bg-(?:black|slate-(?:800|900|950)|gray-(?:800|900|950)|neutral-(?:800|900|950)|zinc-(?:800|900|950))$/;

/** Paths the axe scan visits, read out of the real spec file (as docsA11y does). */
const scannedPaths = [...SPEC.matchAll(/path:\s*"([^"]+)"/g)].map((m) => m[1]);

/**
 * The route a file under `src/app` serves.
 *
 * `app/mcp/page.tsx` AND `app/mcp/mcp-client.tsx` both serve `/mcp`: a route's
 * source is its `page.tsx` and the client component beside it. That second shape
 * is load-bearing — the `/mcp` slab this card is about lives in
 * `mcp-client.tsx`, so a walk that read only `page.tsx` would have missed the
 * exact defect it exists for. A dynamic or route-group segment returns null: it
 * is not a path a static list can name, and it is counted rather than dropped.
 */
function routeOf(rel: string): string | null {
  const dir = rel.replace(/\/[^/]+$/, ""); // drop the file name
  const parts = dir.split("/").slice(1); // drop the leading `app`
  if (parts.some((p) => p.startsWith("[") || p.startsWith("("))) return null;
  return parts.length === 0 ? "/" : `/${parts.join("/")}`;
}

/**
 * True when the route's OWN source declares the session chrome. That is what
 * makes a route unreachable by the public scan: its page (or the client
 * component it renders) resolves the session from `/api/rest/dashboard-bundle`
 * and serves a visitor with no session the landing page instead.
 *
 * It reads only the file itself — see the header for why a layout-chain check
 * would make this rule vacuous.
 */
function declaresOwnSessionGate(rel: string): boolean {
  return /components\/(session|Shell)["']/.test(readSource(`${SRC}${rel}`));
}

const appFiles = tsxFiles(`${SRC}`).filter((f) => f.startsWith("app/"));

/** Every route source under `src/app` that paints a dark slab. */
const carriers = appFiles
  .map((rel) => {
    const tokens = new Set<string>();
    for (const e of jsxTags(codeOnly(readSource(`${SRC}${rel}`)))) {
      for (const v of e.tag.variants ?? []) {
        for (const cls of v.split(/\s+/)) if (DARK_SLAB_RE.test(cls)) tokens.add(cls);
      }
    }
    return { rel, route: routeOf(rel), tokens: [...tokens].sort() };
  })
  .filter((c) => c.tokens.length > 0);

const staticRoutes = new Set(
  appFiles
    .filter((f) => /^app\/(?:.*\/)?page\.tsx$/.test(f))
    .map((f) => routeOf(f))
    .filter((r): r is string => r !== null),
);

test("the guard is looking at real files (it cannot pass vacuously)", () => {
  assert.ok(SPEC.includes("AxeBuilder"), "e2e/a11y.spec.ts is not the axe spec");
  assert.ok(
    scannedPaths.length >= 26,
    `only ${scannedPaths.length} scanned paths were parsed out of the spec — the a11y list shrank`,
  );
  assert.ok(
    staticRoutes.size >= 20,
    `only ${staticRoutes.size} static routes were found under src/app — the walk is broken`,
  );
  assert.ok(
    carriers.length >= 5,
    `only ${carriers.length} route source(s) paint a dark slab, so this rule proved nothing`,
  );
  // The walk must reach a route's CLIENT component, not only its `page.tsx`: the
  // slab this guard was written for is on `app/mcp/mcp-client.tsx`, and a walk
  // narrowed to `page.tsx` would report a clean zero over it.
  const inClientComponent = carriers.filter((c) => !/\/page\.tsx$/.test(c.rel));
  assert.ok(
    inClientComponent.length > 0,
    "no dark slab was found in a client component under src/app — either the walk lost the " +
      "non-page files or the slabs moved; fix the guard, do not delete it",
  );
  // The abstention arm must be live: if the marker stops matching anything, the
  // second way this rule accepts a route has quietly become dead code, and an
  // unscanned session-gated slab would be reported as covered.
  const gated = appFiles.filter(declaresOwnSessionGate);
  assert.ok(
    gated.length >= 5,
    `only ${gated.length} route source(s) declare the session chrome, so the "behind a ` +
      `session" arm of this rule is dead — the marker regex is wrong`,
  );
});

test("every route painting a dark slab is scanned or declares its own session gate", () => {
  const unowned = carriers
    .filter((c) => c.route !== null)
    .filter((c) => !scannedPaths.includes(c.route as string))
    .filter((c) => !declaresOwnSessionGate(c.rel))
    .map((c) => `${c.route} (${c.rel}: paints ${c.tokens.join(", ")})`);

  assert.deepEqual(
    unowned,
    [],
    `these routes paint text on a DARK slab, where neither src/lib/emeraldText.test.ts ` +
      `(which abstains on a background it cannot composite) nor the axe scan can judge the ` +
      `pairing:\n  ${unowned.join("\n  ")}\n` +
      `Fix one of the two ways this guard accepts: add the route to PAGES in ` +
      `web/e2e/a11y.spec.ts so the browser measures it, or — if it renders only behind a ` +
      `session — import components/session or components/Shell in its own source so this ` +
      `guard can see it is unreachable by the public scan. A dark-slab pairing on a route ` +
      `that is in neither is exactly the hole card t_2db0fc94 found on /mcp.`,
  );
});

test("the MCP setup page is scanned (the route this guard exists for)", () => {
  // Named, not merely covered by the rule above: /mcp is the page the gap was
  // measured on, it is public (HTTP 200 with cleared storage) and it carries the
  // second dark slab on a marketing route. Pinned so the entry cannot be dropped.
  assert.ok(scannedPaths.includes("/mcp"), "the a11y gate no longer scans /mcp");
  assert.ok(
    scannedPaths.includes("/docs/mcp"),
    "/docs/mcp dropped out of the scan — it is a DIFFERENT page and both must be listed",
  );
});

test("every scanned path is a route that exists (a typo scans a 404 green)", () => {
  const missing = scannedPaths.filter((p) => !staticRoutes.has(p));
  assert.deepEqual(
    missing,
    [],
    `web/e2e/a11y.spec.ts scans path(s) with no page.tsx on disk: ${missing.join(", ")} — ` +
      `a path that does not exist answers 404 and the scan reads it as "no violations"`,
  );
});

test("no route is listed twice in the scan", () => {
  const dupes = scannedPaths.filter((p, i) => scannedPaths.indexOf(p) !== i);
  assert.deepEqual(dupes, [], `duplicate PAGES entries: ${dupes.join(", ")}`);
});
