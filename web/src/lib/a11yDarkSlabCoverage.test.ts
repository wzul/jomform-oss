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
//   A DYNAMIC (`[id]`) or ROUTE-GROUP (`(shop)`) segment (card t_70a79006):
//   `routeOf` returns null for such a path, because a dynamic directory is not a
//   path a static list can SPELL, so no `PAGES` entry can name it and
//   `scannedPaths.includes()` can never be true for it. Before this card those
//   carriers were dropped outright at the rule's `.filter`, so a slab on a
//   dynamic route was judged by neither layer — the same hole as `/mcp`, one
//   segment class over (measured: the same slab REDs at a static route and ran
//   5/5 GREEN at `app/probe-dyn/[id]/page.tsx`). They are now COUNTED and PINNED
//   pinned (`PINNED_DYNAMIC_CARRIERS`), so the set is a deliberate, reviewable
//   list rather than a number nothing reads. What that still cannot decide is
//   whether the route's CONCRETE instance is scanned: a prefix heuristic (option
//   2 on the card) was rejected because it would red on a legitimate scan entry,
//   and a scan-path check is impossible here by definition — `/products/1` is in
//   no `PAGES` entry today, which is exactly the residue. So this paragraph and
//   `docs/accessibility.md` state the blind spot rather than imply coverage.
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
 * How a spec reaches axe: `new AxeBuilder(...)` directly, or `gateScan()` from
 * `e2e/axeGate.ts` — the ONE definition of the rule set + failing impacts that
 * `a11y.spec.ts` and the nine scoped arms share (card t_5aa6c8ac; the arms used
 * to carry nine copies of `withTags(["wcag2aa","wcag21aa"])`, which reached 6 of
 * axe's 105 rules). This assertion exists to prove the file we parsed IS the
 * axe spec, so it accepts either entry point rather than pretending only one
 * shape exists.
 */
const AXE_SCAN = /(?:AxeBuilder|gateScan)/;

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
 * Route sources under `src/app` whose route is DYNAMIC — `routeOf` returns null
 * for them, because a dynamic (`[id]`) or route-group (`(shop)`) directory is not
 * a path a static list can spell. They therefore cannot be judged by the
 * scan-path arm at all, and before card t_70a79006 they were dropped by the rule
 * below altogether: a slab on such a route was in no gate, with nothing reading
 * the count. This list is the count, pinned and reviewed by hand.
 *
 * It is a list of FILES, not of routes: a dynamic directory can hold a `page.tsx`
 * plus the client components beside it, and every one of them serves the same
 * route. It names each carrier explicitly (rather than asserting only a NUMBER)
 * so that adding a dark-slab carrier at a dynamic route turns the rule after it
 * RED with the file in the message, and so that a carrier being RENAMED or
 * REMOVED also fails (an exact list cannot rot quietly in either direction).
 *
 * EMPTY IS THE MEASURED STATE, not an oversight — recorded here so the next
 * reader does not have to re-derive it. The only dynamic route sources under
 * `src/app` are `api/[...path]/route.ts` (a route handler, not a page, and it
 * paints nothing) and `products/[id]/edit/page.tsx` (a 9-line client wrapper
 * around `ProductEditor`, which is the file that carries the slab and declares
 * the session chrome — so the carrier is one `routeOf` spells as
 * `/products/ProductEditor`, and it is judged there by the abstention arm, not
 * here). A file that BOTH
 * declares the session chrome and sits at a dynamic route is likewise not a
 * residue: the abstention arm below already accepts it as unreachable without a
 * session, and listing it would file a judged answer as an unjudged one.
 */
const PINNED_DYNAMIC_CARRIERS: Array<{ rel: string; route: string; tokens: string[] }> = [];

/**
 * The route a file under `src/app` serves.
 *
 * `app/mcp/page.tsx` AND `app/mcp/mcp-client.tsx` both serve `/mcp`: a route's
 * source is its `page.tsx` and the client component beside it. That second shape
 * is load-bearing — the `/mcp` slab this card is about lives in
 * `mcp-client.tsx`, so a walk that read only `page.tsx` would have missed the
 * exact defect it exists for. A dynamic or route-group segment returns null: it
 * is not a path a static list can name, so no `scannedPaths.includes()` can ever
 * be true for it. The carriers this returns null for are pinned and counted in
 * `PINNED_DYNAMIC_CARRIERS` below — the count that was never read before card
 * t_70a79006.
 */
function routeOf(rel: string): string | null {
  const dir = rel.replace(/\/[^/]+$/, ""); // drop the file name
  const parts = dir.split("/").slice(1); // drop the leading `app`
  if (parts.some((p) => p.startsWith("[") || p.startsWith("("))) return null;
  return parts.length === 0 ? "/" : `/${parts.join("/")}`;
}

/**
 * The route a file under `src/app` serves, for a carrier `routeOf` refuses —
 * i.e. one with a dynamic (`[id]`) or route-group (`(shop)`) segment.
 *
 * It exists so a failure can SPELL the route it is reporting: `routeOf` returning
 * null is what makes the rule above inapplicable, and "null (app/…: paints …)" in
 * a message tells a reader nothing. A route GROUP is spelled here because Next
 * keeps it in the URL (`app/(shop)/cart/page.tsx` serves `/shop/cart`, not
 * `/cart` — https://nextjs.org/docs/app/api-reference/file-conventions/route-groups),
 * so `/…` from the segment list is the route as Next prints it. This function is
 * only ever used for a path `routeOf` already rejected, so it never disagrees with
 * `routeOf` about a static route.
 */
function dynamicRouteOf(rel: string): string {
  const dir = rel.replace(/\/[^/]+$/, "");
  const parts = dir.split("/").slice(1);
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

/**
 * What is wrong with a `PINNED_DYNAMIC_CARRIERS` entry, as a list of messages
 * (empty = the pin is honest). A pin is an ASSERTION about the tree — "this file,
 * at this route, paints this token, and no gate judges it" — so each half is
 * checked against the tree rather than trusted:
 *
 *   * the file must still be a source under `src/app`;
 *   * it must still paint the slab the pin claims;
 *   * nothing else may have started judging it: a pin whose carrier is now
 *     accepted by the abstention arm is a stale claim that reads as an unjudged
 *     residue (remove it, deliberately).
 *
 * It is a named function, and driven on synthetic pins in the test below, because
 * the pin is EMPTY on today's tree: a `for` loop over real entries would execute
 * zero assertions and the validation could rot unobserved — the same
 * "unread count" shape this card exists to remove.
 */
function pinProblems(
  pin: { rel: string; route: string; tokens: string[] },
  files: string[],
  all: Array<{ rel: string; route: string | null; tokens: string[] }>,
): string[] {
  const problems: string[] = [];
  if (!files.includes(pin.rel)) {
    problems.push(`${pin.rel} is not a source file under src/app any more`);
    return problems;
  }
  const found = all.find((c) => c.rel === pin.rel);
  if (!found) {
    problems.push(`${pin.rel} paints no dark slab — the pin names a claim that is no longer true`);
    return problems;
  }
  if (found.route !== null) {
    problems.push(
      `${pin.rel} now resolves to the static route ${found.route}, so this pin is stale — a ` +
        `static carrier is judged by the rule above and must not be filed as an unjudged residue`,
    );
  }
  const claimed = [...pin.tokens].sort().join(", ");
  const actual = [...found.tokens].sort().join(", ");
  if (claimed !== actual) {
    problems.push(`${pin.rel} paints ${actual}, but the pin claims ${claimed}`);
  }
  return problems;
}

test("the guard is looking at real files (it cannot pass vacuously)", () => {
  assert.ok(AXE_SCAN.test(SPEC), "e2e/a11y.spec.ts is not the axe spec");
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
  // STATIC and ROUTE-GROUP routes only. A carrier whose route is DYNAMIC
  // (`routeOf` -> null) cannot be judged by this arm by construction — no PAGES
  // entry can spell its path — so it is not silently dropped here: the next test
  // pins the exact set of such carriers, and the ones that also declare the
  // session chrome are accepted by the abstention below.
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

test("a dark-slab carrier at a DYNAMIC route is pinned, not silently dropped (t_70a79006)", () => {
  // THE HOLE THIS CARD CLOSES. `routeOf` returns null for a `[id]`/`(group)`
  // segment, and the rule above filters those out — so before this card a slab
  // planted at `app/probe-dyn/[id]/page.tsx` ran the whole suite GREEN while the
  // same slab at a static route went RED (both measured on origin/main). The
  // count was described as "counted rather than dropped" in a comment and read
  // by no assertion.
  //
  // The fix is to make the set EXACT and REVIEWED: every dynamic-route slab
  // carrier is named in PINNED_DYNAMIC_CARRIERS, and a carrier that is not is a
  // failure naming the file and the token. A dark slab at a dynamic route then
  // forces a deliberate decision (pin it, gate it, or move it) instead of
  // vanishing.
  //
  // A carrier that ALSO declares the session chrome is not a residue and is not
  // required in the pin: it is accepted by the rule above's abstention arm, the
  // same judged answer a static session-gated route gets.
  const dynamic = carriers
    .filter((c) => c.route === null)
    .filter((c) => !declaresOwnSessionGate(c.rel))
    .map((c) => ({ rel: c.rel, route: dynamicRouteOf(c.rel), tokens: c.tokens }))
    .sort((a, b) => (a.rel < b.rel ? -1 : a.rel > b.rel ? 1 : 0));

  const pinned = [...PINNED_DYNAMIC_CARRIERS].sort((a, b) => (a.rel < b.rel ? -1 : a.rel > b.rel ? 1 : 0));
  const describe = (cs: typeof dynamic) => cs.map((c) => `${c.rel} (${c.route}: paints ${c.tokens.join(", ")})`);

  assert.deepEqual(
    dynamic,
    pinned,
    `the dark-slab carriers at DYNAMIC routes are not the pinned set.\n` +
      `  found:  ${describe(dynamic).join("; ") || "(none)"}\n` +
      `  pinned: ${describe(pinned).join("; ") || "(none)"}\n` +
      `A dynamic route cannot be named in PAGES (the axe scan's list), so this arm ` +
      `cannot check a scan path for it — which is exactly how a slab there was judged by ` +
      `neither layer (card t_70a79006). Adding a carrier means a DECISION, not silence: ` +
      `move it behind \`components/session\`/\`components/Shell\` (or import that chrome in ` +
      `its own source), reach it from a SCANNED static route, or add it to ` +
      `PINNED_DYNAMIC_CARRIERS above with the reason, so the residue stays a reviewed ` +
      `list instead of an unread count.`,
  );
});

test("the dynamic-route pin is itself load-bearing (the derivation cannot go vacuous)", () => {
  // An exact-list rule is only as good as the list's MEANING, so the two things
  // that give this one meaning are asserted here rather than assumed:
  //
  //  1. `dynamicRouteOf` must really spell a dynamic segment's route, or every
  //     failure message and every pin would name nothing useful. Re-derived from
  //     synthetic paths, so this leg holds whatever today's file list happens to
  //     be — including the empty state below.
  //  2. The empty state must be the MEASURED one, not a list someone deleted. If
  //     the pin is allowed to be empty silently, the whole rule is neutered by one
  //     edit; so when it IS empty, the tree must contain NO dynamic-route carrier
  //     outside the abstention arm. If a carrier ever appears, the assertion names
  //     it and the reviewer must pin it, gate it, or move it.
  assert.equal(
    dynamicRouteOf("app/probe-dyn/[id]/page.tsx"),
    "/probe-dyn/[id]",
    "dynamicRouteOf stopped spelling a dynamic segment's route — the failure messages " +
      "and the pin would name nothing useful",
  );
  assert.equal(
    dynamicRouteOf("app/(shop)/cart/page.tsx"),
    "/(shop)/cart",
    "a route-GROUP segment is kept in the URL and must be derived the same way",
  );
  assert.equal(dynamicRouteOf("app/mcp/page.tsx"), "/mcp", "a static route must be spelled unchanged");

  const residue = carriers
    .filter((c) => c.route === null)
    .filter((c) => !declaresOwnSessionGate(c.rel))
    .map((c) => `${dynamicRouteOf(c.rel)} (${c.rel}: paints ${c.tokens.join(", ")})`);
  if (PINNED_DYNAMIC_CARRIERS.length === 0) {
    assert.deepEqual(
      residue,
      [],
      `PINNED_DYNAMIC_CARRIERS is empty, but this tree has a dark-slab carrier at a ` +
        `dynamic route:\n  ${residue.join("\n  ")}\n` +
        `An empty pin is allowed only while the measured residue is empty — otherwise a ` +
        `slab at a dynamic route is judged by neither layer again (card t_70a79006). Add ` +
        `the carrier to PINNED_DYNAMIC_CARRIERS with its reason, import the session chrome ` +
        `(components/session or components/Shell) in its own source, or reach the page from ` +
        `a SCANNED static route.`,
    );
  }

  // The pin-validation leg below is itself driven, not merely run: with the pin
  // EMPTY (today's measured state) a real-pins-only loop would execute zero
  // assertions and the validation could rot unobserved. The predicate takes its
  // files and carriers as ARGUMENTS for exactly this reason, so every branch is
  // driven here on synthetic inputs — a live honest pin, a dead file, a false
  // token claim, and a pin whose carrier a gate has since started judging.
  const probeRel = "app/probe-dyn/[id]/page.tsx";
  const probeFiles = [probeRel];
  const probeCarriers = [{ rel: probeRel, route: null, tokens: ["bg-slate-900"] }];
  assert.deepEqual(
    pinProblems({ rel: probeRel, route: "/probe-dyn/[id]", tokens: ["bg-slate-900"] }, probeFiles, probeCarriers),
    [],
    "an HONEST pin was refused — the validation is stricter than the rule it guards, so a " +
      "real carrier could not be pinned at all",
  );
  assert.ok(
    pinProblems({ rel: probeRel, route: "/probe-dyn/[id]", tokens: ["bg-slate-900"] }, [], probeCarriers).length > 0,
    "a pin naming a file that no longer exists was accepted — the exact-list rule would " +
      "keep passing while the carrier it names was deleted",
  );
  assert.ok(
    pinProblems({ rel: probeRel, route: "/probe-dyn/[id]", tokens: ["bg-slate-100"] }, probeFiles, probeCarriers).length > 0,
    "a pin whose token claim does not match the file was accepted — a stale pin would read " +
      "as coverage for a slab that moved",
  );
  const staticRel = "app/mcp/mcp-client.tsx";
  if (carriers.some((c) => c.rel === staticRel)) {
    assert.ok(
      pinProblems({ rel: staticRel, route: "/mcp", tokens: ["bg-slate-900"] }, appFiles, carriers).length > 0,
      "a pin whose carrier now resolves to a STATIC route was accepted — the rule above " +
        "judges that carrier, so the pin is a stale claim about an unjudged residue",
    );
  }

  // Every pin must be a real file on disk painting the token it claims, or the pin
  // is a claim about nothing (the shape that lets an exact-list guard stay green
  // while the thing it names was deleted).
  for (const p of PINNED_DYNAMIC_CARRIERS) {
    assert.deepEqual(
      pinProblems(p, appFiles, carriers),
      [],
      `PINNED_DYNAMIC_CARRIERS entry ${p.rel} is stale — see the messages above`,
    );
  }
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
