#!/usr/bin/env node
// e2e-scope.mjs — which feature specs does THIS diff make necessary?
//
// The `e2e-builder` / `e2e-core` preflight gates run the whole feature family on
// a default run. Under `--for-diff <base-ref>` they narrow to the specs this
// decision selects, so a release pays for the areas it touched instead of the
// whole suite (measured 2026-11-10: the family costs ~570s, 21% of a release
// run). `PREFLIGHT_E2E=full` forces every spec on any run.
//
// THE DATA IS `web/e2e/areas.tsv` — read its header for the format and for the
// safety argument. This file is only the matcher; it holds no area knowledge of
// its own beyond the rules below, which exist so that narrowing can only ever be
// justified by a positive attribution.
//
// THE RULES, in order (first match wins):
//   1. `--force-full` (PREFLIGHT_E2E=full)            -> FULL, every spec.
//   2. no changed path at all                         -> FULL (an empty diff is
//      not evidence that nothing is affected).
//   3. the playwright configs, this map, and every non-spec file under
//      `web/e2e/` (the shared helpers)                -> FULL.
//   4. a GLOBAL row in areas.tsv (`*` spec-glob)      -> FULL.
//   5. a feature spec with NO row in areas.tsv        -> FULL (the family's
//      coverage is not knowable, so it is never narrowed).
//   6. a changed path under `web/` that no row and no spec names, or under
//      `web/` that the map cannot attribute          -> FULL.
//   6a. EXCEPT a path the TREE can attribute (card t_902bd1f7). `web/public/`
//      asset -> the page(s) whose source names its URL -> the app route(s) they
//      serve; any other `web/src/` module -> the route(s) of the page(s) that
//      import it; `web/scripts/` -> inert when no npm script in the build/start
//      chain runs it. Then only the specs whose own text names that route can
//      render it, and none means NONE (a subset of zero, printed NOT RUN). It
//      widens the moment the route is named by code that serves a DIFFERENT
//      route, because a spec visiting that route could navigate to it — see
//      "the DERIVATION" below for the argument and its one assumption.
//   7. otherwise each changed path selects the specs whose row matches it, the
//      spec whose own file it is, and any spec whose TEXT names it (the import
//      it drives — derived from the tree, so a shared helper cannot be missed).
//   8. a selection of ZERO specs                      -> FULL, loudly: a
//      narrowed run that ran nothing must never be reported as a pass. TWO
//      exceptions, both positively attributed: (a) a diff whose only spec path is
//      owned by ANOTHER gate (`OTHER_GATE_SPECS`), and (b) a diff whose every
//      attributed path derived NO visiting spec under rule 6a. Both decide a
//      subset of zero and the caller prints NOT RUN — a narrowed verdict naming
//      the derivation, never a pass.
//
// Exit status: 0 when a decision was reached (FULL or subset), non-zero when the
// map or the tree could not be read. The caller treats a non-zero exit as FULL.
//
// USAGE
//   node web/scripts/e2e-scope.mjs --gate builder|core --base <ref> [--root DIR]
//   node web/scripts/e2e-scope.mjs --gate core --changed <file-with-paths>
//   node web/scripts/e2e-scope.mjs --check-map [--root DIR]
//   node web/scripts/e2e-scope.mjs --self-test
//
// OUTPUT (gate mode), first line, then one repo-relative spec path per line when
// the decision is a subset:
//   DECISION full|subset <n>/<total> <reason>
import { execFileSync } from "node:child_process";
import { readFileSync, readdirSync, mkdtempSync, mkdirSync, writeFileSync, rmSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, join, posix, resolve } from "node:path";
import { fileURLToPath } from "node:url";

// ---------------------------------------------------------------------------
// globs
// ---------------------------------------------------------------------------
// `**` spans directories (including none), `*` does not cross `/`, `?` is one
// character. Compiled once and cached: the matcher runs over ~100 paths x ~80
// globs on every gate run and forking or recompiling per pair would be silly.
const reCache = new Map();
export function globToRe(glob) {
  let re = reCache.get(glob);
  if (re) return re;
  let out = "^";
  for (let i = 0; i < glob.length; i++) {
    const c = glob[i];
    if (c === "*") {
      if (glob[i + 1] === "*") {
        // `**/` also matches the empty string, so `a/**/b` matches `a/b`.
        i++;
        if (glob[i + 1] === "/") {
          i++;
          out += "(?:.*/)?";
        } else {
          out += ".*";
        }
      } else {
        out += "[^/]*";
      }
    } else if (c === "?") {
      out += "[^/]";
    } else if ("\\^$.|+()[]{}".includes(c)) {
      out += "\\" + c;
    } else {
      out += c;
    }
  }
  out += "$";
  re = new RegExp(out);
  reCache.set(glob, re);
  return re;
}

const matchesAny = (globs, path) => globs.some((g) => globToRe(g).test(path));

// ---------------------------------------------------------------------------
// the map
// ---------------------------------------------------------------------------
export function parseMap(text) {
  const global = [];
  const rows = [];
  for (const raw of text.split("\n")) {
    const line = raw.replace(/\r$/, "");
    if (!line.trim() || line.trimStart().startsWith("#")) continue;
    const cells = line.split("\t").map((c) => c.trim()).filter(Boolean);
    if (cells.length < 2) {
      throw new Error(`areas.tsv: a row needs a spec glob and at least one area: ${JSON.stringify(line)}`);
    }
    const [specGlob, ...areas] = cells;
    if (specGlob === "*") global.push(...areas);
    else rows.push({ specGlob, areas });
  }
  if (!rows.length) throw new Error("areas.tsv has no per-spec rows at all — nothing can be narrowed");
  return { global, rows };
}

// A feature spec is every `*.spec.ts` in web/e2e/ that is NOT in the axe family
// (`a11y*`, owned by the `a11y` gate by filename prefix — the same rule
// playwright.config.ts encodes) and NOT owned by another gate. `builder-*` is the
// `e2e-builder` half.
//
// SPECS OWNED BY ANOTHER GATE (card t_440c9417, option B: one owner per test). The
// menu-checkout geometry spec needs `MENU_FIXTURE_URL` and the `api/cmd/menu-fixture`
// Go server, which the e2e gates' `next start` cannot serve, so `web/playwright.config.ts`
// testIgnore's it in `feature-core` and the `menufixture` gate is its ONLY runner.
// It is therefore not a member of this family at all: it is not in `specFiles`, it
// cannot be selected for `e2e-core`, and `areas.tsv` carries no row for it. Without
// this, a narrowed run would hand playwright a path its project does not enumerate,
// which runs NOTHING while the gate reports a pass — the one shape this file exists
// to prevent.
export const OTHER_GATE_SPECS = new Map([
  ["menu-checkout-option-group.spec.ts", "menufixture"],
]);
export const isFeatureSpec = (name) => name.endsWith(".spec.ts") && !name.startsWith("a11y") && !OTHER_GATE_SPECS.has(name);
export const isBuilderSpec = (name) => isFeatureSpec(name) && name.startsWith("builder-");
export const gateSpecs = (files, gate) =>
  files.filter((f) => (gate === "builder" ? isBuilderSpec(basename(f)) : isFeatureSpec(basename(f)) && !isBuilderSpec(basename(f))));

// Paths that make the whole family necessary regardless of the map. These are
// the harness itself: a change to the config, to a shared helper under web/e2e/,
// or to the map invalidates every attribution the map makes.
const HARNESS = [/^web\/playwright[.\w-]*\.config\.ts$/, /^web\/e2e\/areas\.tsv$/];
const isHarness = (p) => HARNESS.some((re) => re.test(p)) || (/^web\/e2e\//.test(p) && !p.endsWith(".spec.ts"));

// ---------------------------------------------------------------------------
// the DERIVATION (rule 6a) — a `web/` path the map does not claim, attributed
// from the TREE rather than from a typed row
// ---------------------------------------------------------------------------
// WHY THIS EXISTS (card t_902bd1f7). Rule 6 widens the run on ANY `web/` change
// no row maps, so a new page and its screenshots — which no row can map, because
// a screenshot is not an area — made a docs diff pay for both families in full.
// MEASURED on PR #1701 (20 PNGs + one unlisted page + its unit tests, no Go
// file): e2e-builder 755s, e2e-core 850s (the run's only red, on a page no spec
// visits), 2359s of browser work. Declaring `web/public/**` inert would switch
// the fail-closed valve off — an image swap changes pixels and a page that
// renders it can change layout for a spec that visits it — so this is a POSITIVE
// derivation instead, in the same spirit as rule 2 (a spec's own text names the
// sources it drives):
//
//   a `web/public/` asset  -> the page(s) whose source names its URL
//   any other `web/src/`   -> the app route(s) of the page(s) that import it
//   -> the feature specs whose own text names that route -> those run; none -> none
//
// THE SAFETY ARGUMENT, and its one assumption. A feature spec reaches a page by
// `page.goto(...)` with a route it spells out, so a route no spec names is a
// route no spec renders. MEASURED on this tree (2026-11-11): 79 feature specs,
// every route any of them renders is a goto target — 0 of them navigate by
// clicking a link to a route they do not name. The assumption is therefore that
// a spec does not CLICK its way to a route it never names; what makes that
// assumption safe to rely on is the guard below, which widens the moment the
// route is named by code that serves a DIFFERENT route (a nav, a link, a
// redirect), because a spec visiting that other route could click through.
//
// WHAT IT REFUSES TO GUESS. An entry whose route cannot be spelled statically
// (a route group is fine; a `[param]` segment is not), a route handler
// (`route.ts`), the root route (it stands for the whole tree), a module no page
// in the tree imports, and anything under `web/` that is not one of the three
// shapes above — each falls back to rule 6's full run.
const SRC_EXT = [".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".css"];
const APP_ENTRY = /^(page|layout|template|error|global-error|not-found|loading)\.(tsx|ts|jsx|js)$/;
const UNIT_TEST = /\.test\.(ts|tsx|js|jsx)$/;
const dirOf = (p) => (p.includes("/") ? p.slice(0, p.lastIndexOf("/")) : "");

// `route` named in `text` as a PATH PREFIX: the character before it may not
// continue a path (`public/panduan/surau` is not `/panduan/surau`) and the one
// after it may not continue the segment. A spec that names `/docs/guides/x`
// therefore also selects a change to `/docs` — that is over-selection, which
// costs run time; under-selection is what this file exists to prevent.
const routeReCache = new Map();
export function routeNamer(route) {
  let re = routeReCache.get(route);
  if (!re) {
    const esc = route.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    re = new RegExp(`(?<![A-Za-z0-9_/.\\-])${esc}(?![A-Za-z0-9_\\-])`);
    routeReCache.set(route, re);
  }
  return re;
}
export const namesRoute = (text, route) => routeNamer(route).test(text);

// Every specifier the module text statically names. Comments are not parsed
// out: an unresolved string is simply not an edge, and a false edge can only
// select MORE specs.
function importSpecs(text) {
  const out = new Set();
  for (const re of [
    /\bfrom\s+["']([^"'\n]+)["']/g,
    /\bimport\s*\(\s*["']([^"'\n]+)["']\s*\)/g,
    /\brequire\s*\(\s*["']([^"'\n]+)["']\s*\)/g,
  ])
    for (const m of text.matchAll(re)) out.add(m[1]);
  return [...out];
}

function resolveSpec(fromRel, spec, files) {
  let base;
  if (spec.startsWith("./") || spec.startsWith("../")) base = posix.normalize(dirOf(fromRel) + "/" + spec);
  else if (spec.startsWith("@/")) base = posix.normalize("web/src/" + spec.slice(2));
  else return null; // a package, not a file in this tree
  for (const c of [base, ...SRC_EXT.map((e) => base + e), ...SRC_EXT.map((e) => base + "/index" + e)])
    if (files.has(c)) return c;
  return null;
}

// The npm scripts that RUN while the bytes the e2e gates serve are produced: the
// build (and its pre/post hooks) and the start command playwright's webServer
// runs. A script outside this set cannot change those bytes; a script inside it
// can, and is left to rule 6 (full). Derived from package.json, so adding a
// generator to `prebuild` closes the hole by itself.
function buildScriptChain(files, packageJsonText) {
  const out = new Set();
  let pkg;
  try {
    pkg = JSON.parse(packageJsonText);
  } catch {
    return out;
  }
  const scripts = (pkg && pkg.scripts) || {};
  const seen = new Set();
  const queue = ["build", "prebuild", "postbuild", "start", "prestart", "poststart"];
  while (queue.length) {
    const name = queue.shift();
    if (seen.has(name) || typeof scripts[name] !== "string") continue;
    seen.add(name);
    const cmd = scripts[name];
    for (const m of cmd.matchAll(/npm run(?:-script)?\s+([A-Za-z0-9:_-]+)/g)) queue.push(m[1], "pre" + m[1], "post" + m[1]);
    for (const m of cmd.matchAll(/(?:^|[\s;&|])(scripts\/[\w./-]+\.(?:mjs|js|cjs))/g)) out.add("web/" + m[1]);
  }
  const stack = [...out];
  while (stack.length) {
    const rel = stack.pop();
    const text = files.get(rel);
    if (!text) continue;
    for (const s of importSpecs(text)) {
      const r = resolveSpec(rel, s, files);
      if (r && !out.has(r)) {
        out.add(r);
        stack.push(r);
      }
    }
  }
  return out;
}

export function buildTree(files, packageJson = "", helpers = new Map()) {
  return { files, packageJson, helpers, index: null, buildScripts: buildScriptChain(files, packageJson) };
}

function buildIndex(tree) {
  const files = tree.files;
  const imports = new Map();
  for (const [rel, text] of files) {
    const set = new Set();
    for (const s of importSpecs(text)) {
      const r = resolveSpec(rel, s, files);
      if (r) set.add(r);
    }
    imports.set(rel, [...set]);
  }
  const entries = [...files.keys()].filter((rel) => rel.startsWith("web/src/app/") && APP_ENTRY.test(basename(rel)));
  const closure = new Map();
  const reaches = new Map();
  for (const e of entries) {
    const seen = new Set([e]);
    const stack = [...(imports.get(e) || [])];
    while (stack.length) {
      const x = stack.pop();
      if (seen.has(x)) continue;
      seen.add(x);
      for (const y of imports.get(x) || []) if (!seen.has(y)) stack.push(y);
    }
    closure.set(e, seen);
    for (const m of seen) {
      let s = reaches.get(m);
      if (!s) reaches.set(m, (s = new Set()));
      s.add(e);
    }
  }
  return { files, imports, entries, closure, reaches };
}

// The route an app entry SERVES, or null when it cannot be spelled statically.
// `layout`/`template`/`error`/`loading`/`not-found` serve their subtree, which
// the prefix semantics of namesRoute already covers.
function entryRoute(rel) {
  if (!rel.startsWith("web/src/app/")) return null;
  const inner = rel.slice("web/src/app/".length);
  if (!APP_ENTRY.test(basename(inner))) return null;
  const segs = dirOf(inner)
    .split("/")
    .filter((s) => s && !/^\(.*\)$/.test(s));
  if (segs.some((s) => /[\[\]]/.test(s))) return null;
  return "/" + segs.join("/");
}

// Returns { specs, reason } (a positive attribution, possibly of nothing),
// { widen } (the tree says this path CAN reach a spec the derivation cannot
// bound), or null (no derivation: rule 6 widens).
function derive(p, { specFiles, specText, tree }) {
  if (!tree || !tree.files) return null;
  if (!tree.index) tree.index = buildIndex(tree);
  const idx = tree.index;
  if (!idx.entries.length) return null; // a tree with no app entry proves nothing

  const specsNaming = (routes) =>
    specFiles.filter((f) => {
      const t = specText.get(basename(f)) || "";
      return routes.some((r) => namesRoute(t, r));
    });
  const helperNaming = (routes) =>
    [...(tree.helpers || new Map())].filter(([, t]) => routes.some((r) => namesRoute(t, r))).map(([rel]) => rel);
  // A route is only provably unvisited when nothing else that SERVES A DIFFERENT
  // route names it: that is what a link/nav/redirect to it would look like.
  const namedFromAnotherRoute = (routes) => {
    for (const R of routes) {
      for (const [m, text] of idx.files) {
        if (!m.startsWith("web/src/") || UNIT_TEST.test(m)) continue;
        if (!namesRoute(text, R)) continue;
        const homes = idx.reaches.get(m);
        if (!homes || !homes.size) continue; // in no bundle (a unit test, dead code)
        for (const e of homes) {
          const r = entryRoute(e);
          if (r === null) return `${m} (reaching a route that cannot be spelled statically) names ${R}`;
          if (r !== R && !r.startsWith(R + "/")) return `${m} serves ${r} and names ${R}, so a spec visiting ${r} could navigate to it`;
        }
      }
    }
    return "";
  };
  const say = (routes, subject) => {
    const list = [...new Set(routes)].sort();
    if (!list.length) return `derived: ${subject} is read by no page under web/src — no feature spec can render it`;
    return `derived: ${subject} is rendered only by ${list.join(", ")}, and no feature spec names ${list.length === 1 ? "that route" : "those routes"}`;
  };

  // (1) a module under web/src: which app route(s) import it?
  if (p.startsWith("web/src/") && tree.files.has(p)) {
    const homes = idx.reaches.get(p);
    if (!homes || !homes.size) {
      if (UNIT_TEST.test(p))
        return {
          specs: [],
          reason: `derived: ${p} is a unit-test file that no page imports — \`npm run test:unit\` runs it, nothing the e2e gates serve includes it`,
        };
      return null; // dead code, or an app file nothing imports: not attributable
    }
    const routes = new Set();
    for (const e of homes) {
      const r = entryRoute(e);
      if (r === null || r === "/") return null;
      routes.add(r);
    }
    if (!routes.size) return null;
    const other = namedFromAnotherRoute(routes);
    if (other) return { widen: other };
    const helpers = helperNaming([...routes]);
    if (helpers.length) return { widen: `web/e2e/${basename(helpers[0])} names ${[...routes].join(", ")} — a shared helper can drive any spec` };
    const picks = specsNaming([...routes]);
    if (!picks.length) return { specs: picks, reason: say([...routes], p) };
    return {
      specs: picks,
      reason: `derived: ${p} is rendered by ${[...routes].sort().join(", ")}, and the spec(s) whose own text names that route were selected`,
    };
  }

  // (2) an asset under web/public: which page NAMES its URL?
  if (p.startsWith("web/public/")) {
    const rel = p.slice("web/public/".length);
    const parts = rel.split("/").filter(Boolean);
    const prefixes = [];
    for (let i = parts.length; i >= 1; i--) prefixes.push("/" + parts.slice(0, i).join("/"));
    const namesAsset = (text) => prefixes.some((pre) => namesRoute(text, pre)) || text.includes("public/" + rel);
    const routes = new Set();
    for (const e of idx.entries) {
      let named = false;
      for (const m of idx.closure.get(e)) {
        const text = idx.files.get(m);
        if (text && namesAsset(text)) {
          named = true;
          break;
        }
      }
      if (!named) continue;
      const r = entryRoute(e);
      if (r === null || r === "/") return null;
      routes.add(r);
    }
    const helpers = helperNaming(prefixes);
    if (helpers.length) return { widen: `web/e2e/${basename(helpers[0])} names /${rel} — a shared helper can drive any spec` };
    const picks = [...new Set([...specsNaming([...routes]), ...specsNaming(["/" + rel])])].sort();
    if (!picks.length) return { specs: picks, reason: say([...routes], `/${rel}`) };
    const list = [...routes].sort();
    return {
      specs: picks,
      reason: list.length
        ? `derived: /${rel} is rendered by ${list.join(", ")}, and the spec(s) whose own text names a route there were selected`
        : `derived: /${rel} is named by a feature spec directly`,
    };
  }

  // (3) a script: only the ones the build/start chain runs can change the served
  // bytes. With no package.json to read this cannot be decided, and it widens.
  if (p.startsWith("web/scripts/") && tree.files.has(p)) {
    if (!tree.packageJson) return null;
    if (tree.buildScripts.has(p)) return null;
    return {
      specs: [],
      reason: `derived: no npm script in the build/start chain runs ${p}, so it cannot change the bytes the e2e gates serve`,
    };
  }
  return null;
}

// ---------------------------------------------------------------------------
// the decision
// ---------------------------------------------------------------------------
// specText: { basename -> source text } for the derived "the spec names this
// path" rule. A spec names a file as `../src/lib/x` — so the changed path is
// looked for as `src/lib/x` (the path after `web/`), which is exactly how the
// spec's own relative import reads once normalised.
export function decide({ specFiles, mapText, changedPaths, forceFull = false, specText = new Map(), tree = null }) {
  const { global, rows } = parseMap(mapText);
  const total = specFiles.length;
  const full = (reason) => ({ full: true, reason, selected: [...specFiles].sort(), total });

  if (forceFull) return full("PREFLIGHT_E2E=full: every spec was requested");

  const paths = [...new Set(changedPaths.map((p) => p.trim()).filter(Boolean))];
  if (!paths.length) return full("no changed path was given (an empty diff is not evidence)");

  for (const p of paths) {
    if (isHarness(p)) return full(`matched ${p} (the e2e harness itself)`);
  }
  for (const p of paths) {
    if (matchesAny(global, p)) return full(`matched ${p} (a GLOBAL area in web/e2e/areas.tsv)`);
  }

  const unmapped = specFiles.filter((f) => !rows.some((r) => globToRe(r.specGlob).test(basename(f))));
  if (unmapped.length) {
    return full(`${basename(unmapped[0])}${unmapped.length > 1 ? ` (+${unmapped.length - 1} more)` : ""} has no row in web/e2e/areas.tsv`);
  }

  const selected = new Set();
  const derivedInert = [];
  let attribution = "";
  let ownedElsewhere = "";
  for (const p of paths) {
    const base = basename(p);
    const isSpecFile = p.startsWith("web/e2e/") && base.endsWith(".spec.ts");
    if (isSpecFile) {
      if (specFiles.includes(base)) {
        selected.add(base);
        attribution = attribution || `matched ${p}`;
      } else if (OTHER_GATE_SPECS.has(base))
        // Its own gate runs it, this family does not enumerate it — so it selects
        // nothing here and must not WIDEN the run either (the spec does not reach a
        // single feature spec; `web/playwright.config.ts` excludes it by design).
        ownedElsewhere = `${p} is owned by the \`${OTHER_GATE_SPECS.get(base)}\` gate, which web/playwright.config.ts testIgnore's here — no e2e spec can see it`;
      continue;
    }
    // rule 7a: the map attributes this path to a row
    let hit = false;
    for (const r of rows) {
      if (matchesAny(r.areas, p)) {
        for (const f of specFiles) if (globToRe(r.specGlob).test(basename(f))) selected.add(f);
        hit = true;
        attribution = attribution || `matched ${r.areas.find((a) => globToRe(a).test(p))}`;
      }
    }
    // rule 7b: a spec's own text names this path (the import it drives). The
    // spec writes the import WITHOUT the extension (`../src/lib/apiErr`), so the
    // extension-stripped form has to be tried too — otherwise this rule only
    // ever fired for a spec that spelled the file out with `.ts`.
    const suffix = p.startsWith("web/") ? p.slice("web/".length) : p;
    const bare = suffix.replace(/\.[a-z0-9]+$/i, "");
    for (const f of specFiles) {
      const text = specText.get(basename(f));
      if (text && (text.includes(suffix) || text.includes(bare))) {
        selected.add(f);
        hit = true;
        attribution = attribution || `matched a spec that names ${suffix}`;
      }
    }
    if (hit) continue;
    // rule 6a: the TREE may positively attribute this path to a bounded set of
    // specs (a public asset -> the page that names it -> that page's route -> the
    // specs naming the route). A derived zero is a narrowed verdict, not a pass.
    const d = derive(p, { specFiles, specText, tree });
    if (d && d.widen) return full(d.widen);
    if (d) {
      if (d.specs.length) {
        for (const s of d.specs) selected.add(s);
        attribution = attribution || d.reason;
      } else {
        derivedInert.push(d.reason);
      }
      continue;
    }
    // rule 6: a web/ change nothing claims might still reach any spec.
    if (p.startsWith("web/")) return full(`no area maps ${p} — an unattributed web change widens the run`);
    // Outside web/: the specs drive `next start` (they stub the API at the
    // browser level), so a non-web path that no row claims cannot reach one.
  }

  if (!selected.size) {
    // The only spec the diff touches belongs to another gate: this family owes
    // NOTHING, so the caller prints "NOT RUN — 0/N" (a narrowed verdict, never a
    // pass). Rule 8's widening is for a diff that attributes nothing at all; here
    // the attribution is positive and it says "not this gate's test".
    if (ownedElsewhere) return { full: false, reason: ownedElsewhere, selected: [], total };
    // Same shape for rule 6a: every web/ path was positively derived to a route
    // no feature spec names. Positive attribution of nothing is NOT "nothing was
    // attributed" — that one still widens.
    if (derivedInert.length) {
      const uniq = [...new Set(derivedInert)];
      const reason =
        uniq.length <= 2
          ? uniq.join(" ; ")
          : `${uniq[0]} ; (+${uniq.length - 1} more changed paths, each derived to a route no feature spec names)`;
      return { full: false, reason, selected: [], total };
    }
    return full("the diff selected no spec — refusing to report a narrowed run that ran nothing");
  }
  return { full: false, reason: attribution || "matched a mapped area", selected: [...selected].sort(), total };
}

// ---------------------------------------------------------------------------
// io helpers
// ---------------------------------------------------------------------------
function git(root, args) {
  return execFileSync("git", args, { cwd: root, encoding: "utf8" });
}

export function changedPaths(root, base) {
  const out = [git(root, ["diff", "--name-only", "--no-renames", base]), git(root, ["ls-files", "--others", "--exclude-standard"])];
  return out.join("\n").split("\n").map((s) => s.trim()).filter(Boolean);
}

function readSpecs(root) {
  const dir = join(root, "web", "e2e");
  const files = readdirSync(dir).filter((f) => f.endsWith(".spec.ts") && isFeatureSpec(f));
  const text = new Map();
  for (const f of files) text.set(f, readFileSync(join(dir, f), "utf8"));
  return { files: files.sort(), text };
}

// The tree rule 6a reads: every source module under web/src and web/scripts, the
// e2e helpers (a helper that names a route can drive any spec), and package.json
// (which scripts the build/start chain runs). A path missing from this map is not
// derivable, so a failed read can only WIDEN a run.
function readTree(root) {
  const files = new Map();
  const walk = (relDir) => {
    let ents;
    try {
      ents = readdirSync(join(root, relDir), { withFileTypes: true });
    } catch {
      return;
    }
    for (const e of ents) {
      if (e.name === "node_modules" || e.name === ".next") continue;
      const rel = `${relDir}/${e.name}`;
      if (e.isDirectory()) walk(rel);
      else if (SRC_EXT.some((x) => e.name.endsWith(x))) {
        try {
          files.set(rel, readFileSync(join(root, rel), "utf8"));
        } catch {
          /* an unreadable file is simply not an edge: the path it would claim stays unattributed */
        }
      }
    }
  };
  walk("web/src");
  walk("web/scripts");
  const helpers = new Map();
  try {
    for (const f of readdirSync(join(root, "web", "e2e"))) {
      if (f.endsWith(".ts") && !f.endsWith(".spec.ts"))
        helpers.set(`web/e2e/${f}`, readFileSync(join(root, "web", "e2e", f), "utf8"));
    }
  } catch {
    /* readSpecs already threw if the e2e dir is unreadable; an empty guard here only widens */
  }
  const pkgPath = join(root, "web", "package.json");
  const packageJson = existsSync(pkgPath) ? readFileSync(pkgPath, "utf8") : "";
  return buildTree(files, packageJson, helpers);
}

// ---------------------------------------------------------------------------
// --check-map: the map cannot rot into "always full" without saying so
// ---------------------------------------------------------------------------
// Two things are asserted against the TREE, not against the map's own claims:
//   * every feature spec has at least one row (otherwise narrowing silently
//     stops covering that spec and the runner widens to FULL forever);
//   * every row's spec glob and every area glob matches something that exists
//     (a row that matches nothing is a typo, and a typo in an area glob turns
//     into a WHOLE-suite run nobody can see the cause of).
// It reads `git ls-files`, so it also catches a row pointing at a file that was
// deleted or renamed.
function checkMap(root) {
  const problems = [];
  const mapText = readFileSync(join(root, "web", "e2e", "areas.tsv"), "utf8");
  const { global, rows } = parseMap(mapText);
  const { files } = readSpecs(root);
  if (!files.length) problems.push("no feature spec files were found under web/e2e/ — a guard that reads nothing proves nothing");

  for (const f of files) {
    if (!rows.some((r) => globToRe(r.specGlob).test(f))) problems.push(`${f} has no row in web/e2e/areas.tsv`);
  }
  // A spec declared as owned by another gate is not in the family, so it needs no row
  // — but the declaration itself must still name a file that is there, or a rename
  // silently takes the spec out of every gate at once (card t_440c9417).
  for (const spec of OTHER_GATE_SPECS.keys()) {
    if (!existsSync(join(root, "web", "e2e", spec)))
      problems.push(`'${spec}' is declared as owned by the \`${OTHER_GATE_SPECS.get(spec)}\` gate but does not exist under web/e2e/ (renamed? then the new name must be declared too)`);
  }
  for (const r of rows) {
    if (!files.some((f) => globToRe(r.specGlob).test(f))) problems.push(`row '${r.specGlob}' matches no feature spec`);
  }

  const tracked = git(root, ["ls-files"]).split("\n").filter(Boolean);
  const allGlobs = [...global, ...rows.flatMap((r) => r.areas)];
  for (const g of allGlobs) {
    if (!tracked.some((p) => globToRe(g).test(p))) problems.push(`area '${g}' matches no tracked file`);
  }

  if (problems.length) {
    process.stderr.write("E2E AREA MAP FAIL — web/e2e/areas.tsv does not agree with this tree:\n");
    for (const p of problems) process.stderr.write(`  - ${p}\n`);
    process.stderr.write("  Fix the map (a new spec needs a row naming the sources whose change makes it necessary).\n");
    return 1;
  }
  process.stdout.write(
    `e2e area map: ${files.length} feature specs, ${rows.length} rows, ${global.length} global areas — every spec has a row and every glob matches this tree\n`,
  );
  return 0;
}

// ---------------------------------------------------------------------------
// --self-test: the rules, asserted, including the ones that must WIDEN
// ---------------------------------------------------------------------------
function selfTest() {
  const cases = [];
  const made = [];
  const root0 = resolve(fileURLToPath(import.meta.url), "../../..");
  const syntheticTree = (specs, extra = {}) => {
    const root = mkdtempSync(join(tmpdir(), "e2e-scope-st-"));
    made.push(root);
    mkdirSync(join(root, "web", "e2e"), { recursive: true });
    for (const [name, text] of Object.entries(specs)) writeFileSync(join(root, "web", "e2e", name), text);
    for (const [rel, text] of Object.entries(extra)) {
      const p = join(root, rel);
      mkdirSync(p.slice(0, p.lastIndexOf("/")), { recursive: true });
      writeFileSync(p, text);
    }
    return root;
  };
  const t = (name, fn) => {
    try {
      fn();
      cases.push(`ok   ${name}`);
    } catch (e) {
      cases.push(`FAIL ${name}: ${e.message}`);
    }
  };
  const eq = (got, want, what) => {
    if (got !== want) throw new Error(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
  };

  const MAP = [
    "*\tweb/src/components/i18nDict.ts",
    "sales-*.spec.ts\tweb/src/app/sales/**",
    "chat-*.spec.ts\tweb/src/app/chat/**",
  ].join("\n");
  const specs = ["sales-a.spec.ts", "sales-b.spec.ts", "chat-a.spec.ts"];

  t("a one-area diff selects a STRICT SUBSET", () => {
    const d = decide({ specFiles: specs, mapText: MAP, changedPaths: ["web/src/app/sales/page.tsx"] });
    eq(d.full, false, "full");
    eq(JSON.stringify(d.selected), JSON.stringify(["sales-a.spec.ts", "sales-b.spec.ts"]), "selected");
    eq(d.reason, "matched web/src/app/sales/**", "reason");
  });
  t("a GLOBAL area selects the WHOLE family", () => {
    const d = decide({ specFiles: specs, mapText: MAP, changedPaths: ["web/src/components/i18nDict.ts"] });
    eq(d.full, true, "full");
    eq(d.selected.length, 3, "selected length");
  });
  t("the playwright config selects the WHOLE family", () => {
    eq(decide({ specFiles: specs, mapText: MAP, changedPaths: ["web/playwright.config.ts"] }).full, true, "full");
  });
  t("an e2e HELPER selects the WHOLE family", () => {
    eq(decide({ specFiles: specs, mapText: MAP, changedPaths: ["web/e2e/auth.ts"] }).full, true, "full");
  });
  t("the area map itself selects the WHOLE family", () => {
    eq(decide({ specFiles: specs, mapText: MAP, changedPaths: ["web/e2e/areas.tsv"] }).full, true, "full");
  });
  t("an UNATTRIBUTED web change selects the WHOLE family", () => {
    eq(decide({ specFiles: specs, mapText: MAP, changedPaths: ["web/src/lib/nowhere.ts"] }).full, true, "full");
  });
  t("a spec with NO row selects the WHOLE family", () => {
    const d = decide({ specFiles: [...specs, "orphan.spec.ts"], mapText: MAP, changedPaths: ["web/src/app/sales/page.tsx"] });
    eq(d.full, true, "full");
    if (!/orphan\.spec\.ts has no row/.test(d.reason)) throw new Error(`reason did not name the orphan: ${d.reason}`);
  });
  t("a changed SPEC file selects itself", () => {
    const d = decide({ specFiles: specs, mapText: MAP, changedPaths: ["web/e2e/chat-a.spec.ts"] });
    eq(d.full, false, "full");
    eq(JSON.stringify(d.selected), JSON.stringify(["chat-a.spec.ts"]), "selected");
  });
  t("a spec owned by ANOTHER gate selects nothing here and does NOT widen the run", () => {
    // Card t_440c9417: the menu-checkout spec belongs to the `menufixture` gate, and
    // `feature-core` testIgnore's it. Selecting it would hand playwright a file its
    // project does not enumerate — zero tests run, and the gate would call that a pass.
    const d = decide({ specFiles: specs, mapText: MAP, changedPaths: ["web/e2e/menu-checkout-option-group.spec.ts"] });
    eq(d.full, false, "full");
    eq(JSON.stringify(d.selected), JSON.stringify([]), "selected");
    if (!/menufixture/.test(d.reason)) throw new Error(`the reason did not name the owner gate: ${d.reason}`);
  });
  t("the owned-elsewhere spec is not a member of the family at all", () => {
    eq(isFeatureSpec("menu-checkout-option-group.spec.ts"), false, "isFeatureSpec");
    eq(gateSpecs(["menu-checkout-option-group.spec.ts", "sales-a.spec.ts"], "core").join(","), "sales-a.spec.ts", "gateSpecs(core)");
  });
  t("a diff touching BOTH the owned-elsewhere spec and a mapped area narrows to the area", () => {
    const d = decide({ specFiles: specs, mapText: MAP, changedPaths: ["web/e2e/menu-checkout-option-group.spec.ts", "web/src/app/sales/page.tsx"] });
    eq(d.full, false, "full");
    eq(JSON.stringify(d.selected), JSON.stringify(["sales-a.spec.ts", "sales-b.spec.ts"]), "selected");
    eq(d.reason, "matched web/src/app/sales/**", "reason");
  });
  t("a path a spec NAMES is selected even with no row", () => {
    const d = decide({
      specFiles: specs,
      mapText: MAP,
      changedPaths: ["web/src/lib/onlyChatNames.ts"],
      specText: new Map([["chat-a.spec.ts", 'import { x } from "../src/lib/onlyChatNames";']]),
    });
    eq(d.full, false, "full");
    eq(JSON.stringify(d.selected), JSON.stringify(["chat-a.spec.ts"]), "selected");
  });
  t("a non-web path nothing claims is inert (the specs drive next start)", () => {
    const d = decide({ specFiles: specs, mapText: MAP, changedPaths: ["api/internal/httpapi/rest.go"] });
    eq(d.full, true, "full");
    eq(d.reason.indexOf("selected no spec") >= 0, true, "reason");
  });
  // ---- rule 6a: the DERIVATION (card t_902bd1f7) ----------------------------
  // A new page and its screenshots match no row, so before this rule EVERY docs
  // diff ran both families in full (measured on PR #1701: 2359s of browser work,
  // e2e-core's only red on a page no spec visits). These arms pin the positive
  // derivation AND the two places it must refuse to narrow.
  const PKG = JSON.stringify({ scripts: { build: "npm run test:unit && next build", "test:unit": "node scripts/unit-tests.mjs", start: "next start" } });
  const treeOf = (files) => buildTree(new Map(Object.entries(files)), PKG);
  t("a new page + its screenshot, whose route no spec names, selects NO spec and says why", () => {
    const tree = treeOf({
      "web/src/app/panduan/surau/page.tsx": 'import { Article } from "../../../lib/article";\nexport default function P() { return Article(); }',
      "web/src/lib/article.ts": 'export const Article = () => `url(/panduan/surau/01.png)`;',
    });
    const d = decide({
      specFiles: specs,
      mapText: MAP,
      changedPaths: ["web/public/panduan/surau/01.png", "web/src/app/panduan/surau/page.tsx"],
      tree,
    });
    eq(d.full, false, "full");
    eq(JSON.stringify(d.selected), JSON.stringify([]), "selected");
    if (!/panduan\/surau/.test(d.reason) || !/no feature spec names/.test(d.reason))
      throw new Error(`the reason did not name the derivation: ${d.reason}`);
    // and the gate half: a subset of zero is printed NOT RUN, never a spec path
    eq(gateSpecs(d.selected, "core").length, 0, "gateSpecs(core)");
  });
  t("a page a spec DOES name still selects that spec (the derivation is not inertness)", () => {
    const tree = treeOf({ "web/src/app/docs/page.tsx": "export default function D() { return null; }" });
    const d = decide({
      specFiles: specs,
      mapText: MAP,
      changedPaths: ["web/src/app/docs/page.tsx"],
      specText: new Map([["chat-a.spec.ts", 'await page.goto("/docs");']]),
      tree,
    });
    eq(d.full, false, "full");
    eq(JSON.stringify(d.selected), JSON.stringify(["chat-a.spec.ts"]), "selected");
  });
  t("BITE: remove the spec's route literal and that spec leaves the selection", () => {
    // The same page, the same tree — only the named-reference edge is gone. If the
    // selection did not change, the arm above would be proving nothing.
    const tree = treeOf({ "web/src/app/docs/page.tsx": "export default function D() { return null; }" });
    const d = decide({
      specFiles: specs,
      mapText: MAP,
      changedPaths: ["web/src/app/docs/page.tsx"],
      specText: new Map([["chat-a.spec.ts", 'await page.goto("/landing");']]),
      tree,
    });
    eq(d.full, false, "full");
    eq(JSON.stringify(d.selected), JSON.stringify([]), "selected");
  });
  t("a page a DIFFERENT route's code names WIDENS (a spec could click through to it)", () => {
    const tree = treeOf({
      "web/src/app/docs/page.tsx": 'export default function D() { return <a href="/panduan/surau">x</a>; }',
      "web/src/app/panduan/surau/page.tsx": "export default function P() { return null; }",
    });
    const d = decide({
      specFiles: specs,
      mapText: MAP,
      changedPaths: ["web/src/app/panduan/surau/page.tsx"],
      specText: new Map([["chat-a.spec.ts", 'await page.goto("/docs");']]),
      tree,
    });
    eq(d.full, true, "full");
    if (!/could navigate to it/.test(d.reason)) throw new Error(`reason: ${d.reason}`);
  });
  t("an e2e HELPER that names the route WIDENS (it can drive any spec)", () => {
    const tree = buildTree(
      new Map([["web/src/app/panduan/surau/page.tsx", "export default function P() { return null; }"]]),
      PKG,
      new Map([["web/e2e/chatComposer.ts", 'export const R = "/panduan/surau";']]),
    );
    eq(decide({ specFiles: specs, mapText: MAP, changedPaths: ["web/src/app/panduan/surau/page.tsx"], tree }).full, true, "full");
  });
  t("a [param] route and a route handler are NOT derived (they widen)", () => {
    const tree = treeOf({
      "web/src/app/products/[id]/edit/page.tsx": "export default function P() { return null; }",
      "web/src/app/api/thing/route.ts": "export const GET = () => 1;",
    });
    eq(decide({ specFiles: specs, mapText: MAP, changedPaths: ["web/src/app/products/[id]/edit/page.tsx"], tree }).full, true, "dynamic");
    eq(decide({ specFiles: specs, mapText: MAP, changedPaths: ["web/src/app/api/thing/route.ts"], tree }).full, true, "route handler");
  });
  t("a unit-test file nothing imports is inert; a dead lib module still widens", () => {
    const tree = treeOf({
      "web/src/app/landing/page.tsx": "export default function L() { return null; }",
      "web/src/lib/x.test.ts": "// a unit test",
      "web/src/lib/dead.ts": "export const d = 1;",
    });
    const t1 = decide({ specFiles: specs, mapText: MAP, changedPaths: ["web/src/lib/x.test.ts"], tree });
    eq(t1.full, false, "test file full");
    eq(t1.selected.length, 0, "test file selected");
    eq(decide({ specFiles: specs, mapText: MAP, changedPaths: ["web/src/lib/dead.ts"], tree }).full, true, "dead module");
  });
  t("a web/scripts change is inert ONLY when no script in the build/start chain runs it", () => {
    const tree = buildTree(
      new Map([
        ["web/src/app/landing/page.tsx", "export default function L() { return null; }"],
        ["web/scripts/unit-tests.mjs", "export {};"],
        ["web/scripts/guides.mjs", "export {};"],
      ]),
      PKG,
    );
    const inert = decide({ specFiles: specs, mapText: MAP, changedPaths: ["web/scripts/guides.mjs"], tree });
    eq(inert.full, false, "guides full");
    eq(inert.selected.length, 0, "guides selected");
    eq(decide({ specFiles: specs, mapText: MAP, changedPaths: ["web/scripts/unit-tests.mjs"], tree }).full, true, "build-chain script");
  });
  t("an unattributed web path with NO tree still widens (the fail-closed valve)", () => {
    eq(decide({ specFiles: specs, mapText: MAP, changedPaths: ["web/public/nowhere/x.png"] }).full, true, "no tree");
    const tree = treeOf({ "web/src/app/docs/page.tsx": "export default function D() { return null; }" });
    eq(decide({ specFiles: specs, mapText: MAP, changedPaths: ["web/public/logo.png"], tree }).full, false, "no page names it");
  });
  t("PREFLIGHT_E2E=full forces EVERY spec", () => {
    const d = decide({ specFiles: specs, mapText: MAP, changedPaths: ["web/src/app/sales/page.tsx"], forceFull: true });
    eq(d.full, true, "full");
    eq(d.selected.length, 3, "selected length");
  });
  t("an empty diff is not a narrowing", () => {
    eq(decide({ specFiles: specs, mapText: MAP, changedPaths: [] }).full, true, "full");
  });
  t("the real tree's map covers every feature spec", () => {
    const rc = checkMap(root0);
    eq(rc, 0, "check-map exit");
  });
  for (const d of made) rmSync(d, { recursive: true, force: true });

  const failed = cases.filter((c) => c.startsWith("FAIL"));
  for (const c of cases) process.stdout.write(c + "\n");
  if (failed.length) {
    process.stderr.write(`e2e-scope self-test: ${failed.length} case(s) FAILED\n`);
    return 1;
  }
  process.stdout.write(`e2e-scope self-test: ${cases.length} cases, all pass\n`);
  return 0;
}

// ---------------------------------------------------------------------------
// cli
// ---------------------------------------------------------------------------
function main(argv) {
  const arg = (name, dflt = "") => {
    const i = argv.indexOf(name);
    return i >= 0 && argv[i + 1] ? argv[i + 1] : dflt;
  };
  const here = fileURLToPath(import.meta.url);
  const root = resolve(arg("--root", resolve(here, "../../..")));
  if (argv.includes("--self-test")) return selfTest();
  if (argv.includes("--check-map")) return checkMap(root);

  const gate = arg("--gate");
  if (gate !== "builder" && gate !== "core") {
    process.stderr.write("e2e-scope: --gate must be 'builder' or 'core' (or use --check-map / --self-test)\n");
    return 2;
  }
  const base = arg("--base");
  const changedFile = arg("--changed");
  if (!base && !changedFile) {
    process.stderr.write("e2e-scope: one of --base <ref> or --changed <file> is required\n");
    return 2;
  }
  let paths;
  try {
    paths = changedFile ? readFileSync(changedFile, "utf8").split("\n") : changedPaths(root, base);
  } catch (e) {
    process.stderr.write(`e2e-scope: cannot read the changed paths (${e.message}) — the caller runs the whole family\n`);
    return 3;
  }
  const { files, text } = readSpecs(root);
  const forceFull = argv.includes("--force-full") || process.env.PREFLIGHT_E2E === "full";
  let tree = null;
  try {
    tree = readTree(root);
  } catch {
    tree = null; // rule 6a off: every web/ path no row claims widens the run
  }
  let d;
  try {
    d = decide({ specFiles: files, mapText: readFileSync(join(root, "web", "e2e", "areas.tsv"), "utf8"), changedPaths: paths, forceFull, specText: text, tree });
  } catch (e) {
    process.stderr.write(`e2e-scope: ${e.message} — the caller runs the whole family\n`);
    if (process.env.E2E_SCOPE_DEBUG) process.stderr.write(`${e.stack}\n`);
    return 4;
  }
  const mine = d.full ? gateSpecs(files, gate) : gateSpecs(d.selected, gate);
  process.stdout.write(`DECISION ${d.full ? "full" : "subset"} ${mine.length}/${files.length} ${d.reason}\n`);
  if (!d.full) for (const s of mine) process.stdout.write(`web/e2e/${s}\n`);
  return 0;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  process.exit(main(process.argv.slice(2)));
}
