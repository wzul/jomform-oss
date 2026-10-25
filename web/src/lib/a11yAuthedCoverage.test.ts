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
//   (c) THE SPEC ACTUALLY VISITS THE ROUTE (card t_2275886d): the (b) half used
//       to be a SUBSTRING SEARCH over the spec's whole file text, so a spec that
//       only STUBBED the API path satisfied the route — `$` sat inside the
//       match class of `/(.*)\/api\/auth\/profile$/`, so seven specs that never
//       load `/profile` counted as its visitor, and a route named in a comment
//       counted too (`e2e/a11y-profile-tabs.spec.ts` lists "/products",
//       "/media", "/forms", "/settings" in its header). Measured: moving the one
//       spec that visits `/profile` out of `e2e/` left this file GREEN, 829
//       passed / 0 failed. A visit is now read out of the ARGUMENTS of `goto(`
//       calls — a string or template literal, or a name bound to one in the same
//       file — and nothing else. `visits()` below is the whole rule, and the
//       second test pins its shape so it cannot quietly become a substring
//       search again. A goto of a route the APP redirects (`/space-picker` 307s
//       to `/workspace-picker`) counts for the route it lands on, because the
//       browser really does end up there and the document really is scanned.
//
// The assertion is deliberately about the CONTRACT (route exists, a gate spec
// VISITS it and runs axe), never about a class name or an attribute: the
// attribute that fixes the defect is measured by the browser spec itself, which
// also neutralises it in the live DOM and requires the axe rule to fire again.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const pathOf = (rel: string) => fileURLToPath(new URL(rel, import.meta.url));

const E2E_DIR = pathOf("../../e2e");
const CONFIG = readFileSync(pathOf("../../playwright.config.ts"), "utf8");

/**
 * The axe entry points: `new AxeBuilder(...)` directly, or `gateScan()` from
 * `e2e/axeGate.ts` — the ONE definition of the rule set + failing impacts shared
 * by `a11y.spec.ts` and the nine scoped arms (card t_5aa6c8ac). A spec that
 * visits a route with NEITHER is not a scan.
 */
const SCANNERS = ["AxeBuilder", "gateScan"] as const;
const SCANNER = SCANNERS.join(" or ");

/**
 * The /settings strip's size, MEASURED out of the page's own source.
 *
 * This description used to read "the **eleven**-tab strip" — a typed count —
 * and by the time card t_2275886d ran, the array behind it held TWELVE ids
 * (`src/app/settings/page.tsx:59`) of which three are admin-only
 * (`ADMIN_ONLY_TABS`: chip, members, workspace), so a member session renders
 * nine and a `read_only` member renders no strip at all. A typed count is what
 * GATE-INVENTORY.md says rots, so these are read from the file and the test
 * below fails if the parse ever comes back empty.
 */
const SETTINGS_PAGE_SRC = readFileSync(pathOf("../app/settings/page.tsx"), "utf8");

function tabIdsIn(declaration: string): string[] {
  const m = SETTINGS_PAGE_SRC.match(new RegExp(`const ${declaration}[^=]*=\\s*\\[([^\\]]*)\\]`));
  return m ? [...m[1].matchAll(/"([^"]+)"/g)].map((x) => x[1]) : [];
}

const SETTINGS_TAB_IDS = tabIdsIn("TAB_IDS");
const SETTINGS_ADMIN_ONLY = tabIdsIn("ADMIN_ONLY_TABS");
const SETTINGS_TABS = {
  ids: SETTINGS_TAB_IDS.length,
  adminOnly: SETTINGS_ADMIN_ONLY.length,
  member: SETTINGS_TAB_IDS.length - SETTINGS_ADMIN_ONLY.length,
};

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
    what:
      `the settings tab strip — MEASURED from src/app/settings/page.tsx: ${SETTINGS_TABS.ids} ids in ` +
      `TAB_IDS, ${SETTINGS_TABS.adminOnly} of them admin-only, so ${SETTINGS_TABS.member} on a member session ` +
      `(card t_4d32da9a added its axe arm)`,
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

/**
 * `src` with its comments blanked out and its string/template bodies kept.
 *
 * Quote state is tracked so a `//` inside a string or a regex (a URL, the
 * `/(.*)\/api\/.*$/` stubs) is not mistaken for a comment. Mis-stripping can
 * only ever REMOVE text here, which loses a visit and fails loudly — it can
 * never invent one.
 */
function withoutComments(src: string): string {
  let out = "";
  let i = 0;
  while (i < src.length) {
    const c = src[i];
    if (c === "/" && src[i + 1] === "/") {
      while (i < src.length && src[i] !== "\n") i++;
      continue;
    }
    if (c === "/" && src[i + 1] === "*") {
      i += 2;
      while (i < src.length && !(src[i] === "*" && src[i + 1] === "/")) i++;
      i += 2;
      out += " ";
      continue;
    }
    if (c === '"' || c === "'" || c === "`") {
      const quote = c;
      out += c;
      i += 1;
      while (i < src.length && src[i] !== quote) {
        if (src[i] === "\\") {
          out += src[i];
          i += 1;
        }
        if (i < src.length) {
          out += src[i];
          i += 1;
        }
      }
      if (i < src.length) {
        out += src[i];
        i += 1;
      }
      continue;
    }
    out += c;
    i += 1;
  }
  return out;
}

/** The FIRST argument of every `goto(` call in `src`, as the source text it was
 *  written as. Reads the argument list rather than the file: the second argument
 *  (`{ waitUntil: … }`) and every `page.route(...)` call are simply not read. */
function gotoArguments(src: string): string[] {
  const text = withoutComments(src);
  const args: string[] = [];
  const call = /\bgoto\s*\(/g;
  let m: RegExpExecArray | null;
  while ((m = call.exec(text))) {
    const open = m.index + m[0].length - 1;
    let i = open + 1;
    let depth = 0;
    let quote: string | null = null;
    for (; i < text.length; i++) {
      const c = text[i];
      if (quote) {
        if (c === "\\") i++;
        else if (c === quote) quote = null;
        continue;
      }
      if (c === '"' || c === "'" || c === "`") {
        quote = c;
        continue;
      }
      if (c === "(") depth++;
      else if (c === ")") {
        if (depth === 0) break;
        depth--;
      } else if (c === "," && depth === 0) break;
    }
    args.push(text.slice(open + 1, i).trim());
  }
  return args;
}

/** Reads the string/template literal starting at `src[at]`; an interpolation
 *  (`${baseURL}`, `${FORM_ID}`) becomes a `*`, so it cannot be read as a route
 *  segment. Returns null when `src[at]` does not open a literal. */
function readLiteral(src: string, at: number): { value: string; end: number } | null {
  const quote = src[at];
  if (quote !== '"' && quote !== "'" && quote !== "`") return null;
  let out = "";
  let i = at + 1;
  while (i < src.length && src[i] !== quote) {
    if (src[i] === "\\") {
      out += src[i + 1] ?? "";
      i += 2;
      continue;
    }
    if (quote === "`" && src[i] === "$" && src[i + 1] === "{") {
      let depth = 0;
      for (i += 1; i < src.length; i++) {
        if (src[i] === "{") depth++;
        else if (src[i] === "}") {
          depth--;
          if (depth === 0) break;
        }
      }
      out += "*";
      i += 1;
      continue;
    }
    out += src[i];
    i += 1;
  }
  if (i >= src.length) return null;
  return { value: out, end: i + 1 };
}

/** Every literal bound to `name` in `src`: an assignment (`const path = "…"`), a
 *  parameter default (`path = "…"`), or a table entry (`{ path: "…" }`). Those
 *  are the two shapes the signed-in specs navigate by — a helper called with the
 *  route as a default, and a `ROUTES` table read through `route.path`. A name
 *  that is only a TYPE (`path: string`) or only passed positionally binds to
 *  nothing, and then the spec is not counted. `name === "…"` (a comparison) is
 *  deliberately NOT a binding. */
function bindingsOf(src: string, name: string): string[] {
  const found: string[] = [];
  const at = new RegExp(`(^|[^\\w$])${name}(?![\\w$])`, "g");
  for (const m of src.matchAll(at)) {
    let i = m.index + m[0].length;
    while (/\s/.test(src[i] ?? "")) i++;
    if (src[i] === "?" && src[i + 1] === ":") i += 2;
    else if (src[i] === ":" && src[i + 1] !== ":") i += 1;
    else if (src[i] === "=" && src[i + 1] !== "=") i += 1;
    else continue;
    while (/\s/.test(src[i] ?? "")) i++;
    const lit = readLiteral(src, i);
    if (lit) found.push(lit.value);
  }
  return found;
}

/** The routes one `goto` argument can be. A literal IS a route; a bare name (or
 *  `x.name`) is resolved through `bindingsOf`; anything else — a stub, a
 *  `page.path` read off a table this file does not own, a concatenation —
 *  resolves to nothing rather than to a guess. */
function argumentRoutes(arg: string, src: string): string[] {
  const lit = readLiteral(arg, 0);
  if (lit && lit.end === arg.length) return [lit.value];
  if (/^[A-Za-z_$][\w$]*$/.test(arg)) return bindingsOf(src, arg);
  const member = arg.match(/^[A-Za-z_$][\w$]*\.([A-Za-z_$][\w$]*)$/);
  if (member) return bindingsOf(src, member[1]);
  return [];
}

/** The pathname a goto value lands on: a leading absolute prefix — an
 *  interpolation such as `${baseURL}` or a scheme and host — is dropped, as is
 *  any `?query`. `/settings?tab=members` and `${baseURL}/settings` are both
 *  `/settings`. */
function pathnameOf(value: string): string {
  let v = value;
  const slash = v.indexOf("/");
  if (slash > 0) v = v.slice(slash);
  if (v.startsWith("//")) v = v.replace(/^\/\/[^/]*/, "");
  return v.split(/[?#]/)[0];
}

/**
 * The app's OWN redirect table, read from `src/app/<route>/page.tsx`.
 *
 * A `goto` of a route that 307s LANDS on its target, so the target is visited:
 * `/space-picker` is a permanent redirect to `/workspace-picker`
 * (`src/app/space-picker/page.tsx:8`), and the spec that scans the picker
 * document reaches it that way. A rule that only accepted the literal route
 * would call that route uncovered and turn the gate RED on a scan that really
 * happens.
 */
const APP_DIR = pathOf("../app");

function collectRedirects(dir: string, route: string, into: Map<string, string>): void {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.isDirectory()) {
      // A route group — `(dashboard)` — and a private folder (`_x`) add no path
      // segment: skip the grouping rather than map a URL that does not exist.
      const here =
        entry.name.startsWith("(") || entry.name.startsWith("_")
          ? route
          : `${route === "/" ? "" : route}/${entry.name}`;
      collectRedirects(join(dir, entry.name), here, into);
      continue;
    }
    if (entry.name !== "page.tsx" || route === "/") continue;
    const src = withoutComments(readFileSync(join(dir, entry.name), "utf8"));
    const call = src.match(/\b(?:permanentRedirect|redirect)\s*\(\s*("(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*')/);
    const lit = call ? readLiteral(call[1], 0) : null;
    if (lit) into.set(route, pathnameOf(lit.value));
  }
}

const APP_REDIRECTS = new Map<string, string>();
collectRedirects(APP_DIR, "/", APP_REDIRECTS);

/** Does a goto of `from` land on `route`? Directly, one segment deeper, or
 *  through the app's own redirect table (bounded, so a cycle cannot hang the
 *  run). */
function landsOn(from: string, route: string): boolean {
  let at = from;
  for (let hop = 0; hop < 4; hop++) {
    if (at === route || at.startsWith(`${route}/`)) return true;
    const next = APP_REDIRECTS.get(at);
    if (!next || next === at) return false;
    at = next;
  }
  return false;
}

/** Does this spec source VISIT `route`? Only the arguments of `goto(` calls
 *  count, so a `page.route(/(.*)\/api\/auth\/profile$/, …)` stub, a route named
 *  in a comment and a route that only appears in a prose list are all NOT
 *  visits — each of those satisfied the substring predicate this replaces. A
 *  visit one segment DEEPER (`/profile/security` for `/profile`) counts: those
 *  are the page's own sub-routes, and so does a route the app redirects to,
 *  because the browser really does end up there. */
function visits(src: string, route: string): boolean {
  return gotoArguments(src)
    .flatMap((arg) => argumentRoutes(arg, src))
    .map(pathnameOf)
    .some((p) => landsOn(p, route));
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

test("a visit is an argument of page.goto — a route stub, a comment or a prose list is NOT one", () => {
  // The shapes the substring predicate accepted, verbatim: the stub the seven
  // signed-in specs share (its `$` sits inside the match class of the regex, so
  // the API path it stubs read as a visit of `/profile`), a route named in a
  // comment, and a route that only ever appears inside a string that is not a
  // navigation.
  //
  // NOTE — no API path below (or in this prose) follows a backtick or a double
  // quote directly: `api/internal/httpapi/route_parity_test.go` extracts
  // quote-delimited API-path literals out of web/src WITHOUT stripping comments,
  // so a path written that way in PROSE is read as a frontend call and fails
  // that guard (measured: the full preflight's `test` gate, tree 5040d100).
  const notAVisit = [
    `await page.route(/(.*)\\/api\\/auth\\/profile$/, (r) => r.fulfill(json({ lang: "" })));`,
    `// The strip's routes: "/products", "/media", "/forms", "/settings".`,
    `const NOTE = "/profile is scanned in its own spec";`,
    `await page.goto("/login", { waitUntil: "networkidle" });`,
  ].join("\n");
  assert.equal(
    visits(notAVisit, "/profile"),
    false,
    "a page.route() stub for the account's profile endpoint is not a visit of /profile — " +
      "this is the defect card t_2275886d exists for",
  );
  assert.equal(
    visits(notAVisit, "/settings"),
    false,
    "a route named in a comment is not a visit of /settings — the /settings entry was satisfied by one",
  );

  const cases: [string, string, boolean, string][] = [
    [`await page.goto("/profile", { waitUntil: "domcontentloaded" });`, "/profile", true, "a string literal"],
    ["await page.goto(`/profile`);", "/profile", true, "a template literal with no interpolation"],
    ["await page.goto(`${baseURL}/settings`);", "/settings", true, "an absolute base inside the template"],
    [`await page.goto("/settings?tab=members");`, "/settings", true, "a ?query suffix"],
    [`await page.goto("/profile/security");`, "/profile", true, "a trailing path segment"],
    [`await page.goto("/profile-ish");`, "/profile", false, "a path that only STARTS with the route"],
    [`await page.goto("/subscriptions-lite");`, "/subscriptions", false, "a path that only STARTS with the route"],
    [`await page.goto("/profile");\nawait page.route("**/api/rest/sales", (r) => r.abort());`, "/profile", true, "a stub beside a real visit"],
    [
      `async function open(p: Page, path = "/profile") {\n  await p.goto(path, { waitUntil: "domcontentloaded" });\n}`,
      "/profile",
      true,
      "a helper's parameter default",
    ],
    [
      `const ROUTES: Route[] = [{ path: "/settings" }];\nfor (const r of ROUTES) await page.goto(r.path, {});`,
      "/settings",
      true,
      "a route table read through `route.path`",
    ],
    [
      `const ROUTES: Route[] = [{ path: "/sales" }];\nfor (const r of ROUTES) await page.goto(r.path, {});`,
      "/settings",
      false,
      "another route's table entry",
    ],
    // A legacy path the app itself 307s (`src/app/space-picker/page.tsx`) LANDS
    // on its target, so the target is visited — the shape PR #1243's spec scans
    // `/workspace-picker` by. A rule that only read the literal would call that
    // route uncovered and turn a real scan red.
    [`await page.goto("/space-picker");`, "/workspace-picker", true, "a route the app 307s to"],
    [`await page.goto("/space-picker");`, "/profile", false, "a redirect to a DIFFERENT route"],
  ];
  for (const [src, route, expected, what] of cases) {
    assert.equal(
      visits(src, route),
      expected,
      `${what}: visits(${JSON.stringify(src)}, ${JSON.stringify(route)}) must be ${expected}`,
    );
  }
});

test("the /settings entry carries the tab count READ out of the page, not a typed one", () => {
  assert.ok(
    SETTINGS_TABS.ids > 0 && SETTINGS_TABS.adminOnly > 0 && SETTINGS_TABS.member < SETTINGS_TABS.ids,
    `the tab census parsed out of src/app/settings/page.tsx is vacuous (${JSON.stringify(SETTINGS_TABS)}): ` +
      `TAB_IDS = ${JSON.stringify(SETTINGS_TAB_IDS)}, ADMIN_ONLY_TABS = ${JSON.stringify(SETTINGS_ADMIN_ONLY)}. ` +
      `The regexes here have to be re-pointed at whatever the declarations look like now — not deleted, ` +
      `because then the description below is a guess again`,
  );
  const entry = AUTHED_ROUTES.find((r) => r.route === "/settings")!;
  assert.ok(
    entry.what.includes(`${SETTINGS_TABS.member} on a member session`),
    `the /settings entry no longer carries the MEASURED tab count (it reads ${JSON.stringify(entry.what)}). ` +
      `It used to read "the eleven-tab strip" while TAB_IDS held 12 ids and a member session rendered 9 — ` +
      `a typed count inside a description rots the moment the page changes`,
  );
});

test("each signed-in route is visited by an a11y-family spec that runs axe", () => {
  for (const { route, what } of AUTHED_ROUTES) {
    // `a11y.spec.ts` is the public PAGES list; the signed-in surfaces live in
    // sibling specs (the session has to be stubbed, which its `page.goto`-only
    // shape does not do), so the public file cannot satisfy this on its own.
    const scanners = A11Y_SPECS.filter((f) => f !== "a11y.spec.ts").filter((f) => {
      const text = readFileSync(pathOf(`../../e2e/${f}`), "utf8");
      return visits(text, route) && SCANNERS.some((s) => text.includes(s));
    });
    assert.ok(
      scanners.length > 0,
      `${route} (${what}) is visited by NO a11y-family spec that runs ${SCANNER}: no spec has a ` +
        `page.goto argument that resolves to it (a stub for its API path, a route named in a comment ` +
        `and a route in a prose list are not visits — t_2275886d). The axe gate would be green for a ` +
        `critical violation on it — the exact gap this contract exists for. ` +
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
