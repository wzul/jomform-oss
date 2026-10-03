// Guard for the workspace scope SILENTLY DROPPED by a second `?` in a URL
// (card t_4010be39).
//
// WHY THIS FILE EXISTS
//
// Most call sites are a bare path and append `workspaceQuery()`, which opens with `?`:
//
//     fetch(`/api/rest/forms${workspaceQuery()}`)
//
// Two sites instead built a query of their OWN and concatenated the scope after it:
//
//     /api/rest/products?include_archived=1${workspaceQuery()}
//     /api/rest/media?limit=100${workspaceQuery()}
//
// Two `?` in one URL. Measured against a real Go server, `r.URL.Query()` puts everything
// after the second `?` into the FIRST parameter's value:
//
//     RequestURI="/v1/rest/products?include_archived=1?workspace=ws-a"
//         include_archived="1?workspace=ws-a"   workspace=""   <-- scope DROPPED
//
// so the request ran UNSCOPED and `resolveAccountScope` fell back to the caller's FIRST
// owned account — a different workspace than the merchant selected, with no error and no
// wrong-looking data. The worst shape a tenancy bug can take.
//
// WHAT IS PINNED HERE, and why each assertion can fail
//
//   1. `workspaceUrl` COMPOSES. Driven directly, because the helper is the thing being
//      relied on: a bare path gets `?`, a path with a query gets `&`, an empty scope adds
//      nothing, and a slug that needs escaping is escaped. Each of those is a way a naive
//      `+ "?workspace="` would be wrong.
//   2. NO CALL SITE may build a URL that already has a `?` and then append
//      `workspaceQuery()`. This is the defect itself, scanned over the source tree so a
//      THIRD site is caught the day it is written rather than by the next tenancy report.
//      It is a source scan and not a runtime test because the bug is a property of string
//      concatenation, invisible until a request actually goes out.
//   3. VACUITY: the scan must find a non-trivial number of `workspaceQuery()` call sites.
//      A reflow that drops the pattern would otherwise leave this guard green checking
//      nothing — which is exactly how the original defect survived.
//
// SCOPE: this proves the COMPOSITION is right and the two-`?` shape is gone. It does not
// exercise the Go side (that is the api suite's job, and the card's own measurement lives
// there), and it cannot see a URL assembled by some future abstraction.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const webRoot = join(fileURLToPath(new URL(".", import.meta.url)), "..", "..");
const srcRoot = join(webRoot, "src");

// workspaceUrl/workspaceQuery need a confirmed scope to answer, so the module is driven
// through its own setter the way the app does rather than by reaching into its state.
type WorkspaceModule = {
  workspaceUrl: (url: string) => string;
  workspaceQuery: () => string;
  setConfirmedWorkspaceScope?: (slug: string | null) => void;
};
const mod = (await import("../lib/workspace.ts")) as unknown as WorkspaceModule;

// setScope drives the confirmed scope through whatever the module exports; if it has no
// setter the helper still reads the stored slug, which is enough for the shapes below.
function withScope<T>(slug: string | null, fn: () => T): T {
  if (typeof mod.setConfirmedWorkspaceScope === "function") {
    mod.setConfirmedWorkspaceScope(slug);
  }
  return fn();
}

test("workspaceUrl composes a SECOND parameter with `&`, never a second `?`", () => {
  const url = withScope("ws-a", () => mod.workspaceUrl("/api/rest/products?include_archived=1"));
  assert.ok(
    !url.includes("??") && url.split("?").length === 2,
    `a URL that already has a query must gain one more parameter, not a second '?': ${url}`,
  );
  assert.ok(url.includes("&workspace=ws-a"), `expected &workspace=ws-a in ${url}`);
  const q = new URLSearchParams(url.slice(url.indexOf("?") + 1));
  assert.equal(q.get("workspace"), "ws-a", "the workspace must survive as its own parameter");
  assert.equal(q.get("include_archived"), "1", "the original parameter must not absorb it");
});

test("workspaceUrl opens with `?` on a bare path", () => {
  const url = withScope("ws-a", () => mod.workspaceUrl("/api/rest/forms"));
  assert.equal(url, "/api/rest/forms?workspace=ws-a");
});

test("workspaceUrl adds NOTHING when there is no scope", () => {
  const url = withScope(null, () => mod.workspaceUrl("/api/rest/forms"));
  assert.equal(url, "/api/rest/forms", "an unscoped request must stay exactly the caller's URL");
});

test("workspaceUrl escapes a slug that needs it", () => {
  const url = withScope("ws a/b", () => mod.workspaceUrl("/api/rest/media?limit=100"));
  assert.ok(!url.slice(url.indexOf("?") + 1).includes(" "), `the slug must be escaped: ${url}`);
  const q = new URLSearchParams(url.slice(url.indexOf("?") + 1));
  assert.equal(q.get("workspace"), "ws a/b", "and it must round-trip through a query parser");
});

// The defect itself, scanned over the tree.
//
// A call site is wrong when a `?` in the same template literal is a QUERY SEPARATOR and
// `workspaceQuery()` is interpolated after it. That is the exact shape both original
// sites had.
//
// The first cut of this pattern was `\?[^`$]*\$\{workspaceQuery\(\)\}` and it reported a
// FALSE offender: `.../notifications/${editing?.id}${workspaceQuery()}` — TypeScript's
// OPTIONAL CHAINING `?.`, not a query separator. A guard that flags that would force a
// rewrite of correct code to satisfy a scan, which is how a guard gets deleted. So the
// `?` must be followed by something that looks like a query key and NOT by `.`:
//
//     ?include_archived=1${workspaceQuery()}      <- the defect
//     ?${workspaceQuery()}                        <- correct (bare path ending in a literal)
//     path${workspaceQuery()}                     <- correct
//     path?a=1&${workspaceQuery()}                <- correct (already fixed)
//     ${editing?.id}${workspaceQuery()}           <- correct (optional chaining)
const DEFECT = /\?[A-Za-z_][A-Za-z0-9_]*[=&#]?[^`$]*\$\{workspaceQuery\(\)\}/;

function walk(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    if (name === "node_modules" || name === ".next") continue;
    const p = join(dir, name);
    if (statSync(p).isDirectory()) walk(p, out);
    else if (/\.(ts|tsx)$/.test(name) && !/\.test\./.test(name)) out.push(p);
  }
  return out;
}

test("no call site builds a second `?` before workspaceQuery()", () => {
  const files = walk(srcRoot);
  assert.ok(files.length > 50, `only ${files.length} source files walked — the scan is broken`);

  const offenders: string[] = [];
  let sites = 0;
  for (const f of files) {
    const raw = readFileSync(f, "utf8");
    for (const line of raw.split("\n")) {
      if (line.includes("workspaceQuery()")) sites += 1;
      // A comment that names the defect is not the defect.
      const code = line.replace(/\/\/.*$/, "").replace(/^\s*\*.*$/, "");
      if (DEFECT.test(code)) offenders.push(`${relative(webRoot, f)}: ${line.trim()}`);
    }
  }
  assert.deepEqual(
    offenders,
    [],
    `a URL is composed with a '?' and then has workspaceQuery() appended, which produces TWO ` +
      `'?' — the Go side then folds the scope into the previous parameter's value and the ` +
      `request runs UNSCOPED (card t_4010be39). Use workspaceUrl("/path?existing=1") instead:\n  ` +
      offenders.join("\n  "),
  );
  // Vacuity, a hard failure: if the pattern no longer matches anything, this guard would
  // report green while checking nothing — the shape that let the defect ship.
  assert.ok(sites > 50, `only ${sites} workspaceQuery() call sites found — the scan went hollow`);
});
