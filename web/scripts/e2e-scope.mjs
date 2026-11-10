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
//   7. otherwise each changed path selects the specs whose row matches it, the
//      spec whose own file it is, and any spec whose TEXT names it (the import
//      it drives — derived from the tree, so a shared helper cannot be missed).
//   8. a selection of ZERO specs                      -> FULL, loudly: a
//      narrowed run that ran nothing must never be reported as a pass.
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
import { readFileSync, readdirSync, mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, join, resolve } from "node:path";
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
// playwright.config.ts encodes). `builder-*` is the `e2e-builder` half.
export const isFeatureSpec = (name) => name.endsWith(".spec.ts") && !name.startsWith("a11y");
export const isBuilderSpec = (name) => isFeatureSpec(name) && name.startsWith("builder-");
export const gateSpecs = (files, gate) =>
  files.filter((f) => (gate === "builder" ? isBuilderSpec(basename(f)) : isFeatureSpec(basename(f)) && !isBuilderSpec(basename(f))));

// Paths that make the whole family necessary regardless of the map. These are
// the harness itself: a change to the config, to a shared helper under web/e2e/,
// or to the map invalidates every attribution the map makes.
const HARNESS = [/^web\/playwright[.\w-]*\.config\.ts$/, /^web\/e2e\/areas\.tsv$/];
const isHarness = (p) => HARNESS.some((re) => re.test(p)) || (/^web\/e2e\//.test(p) && !p.endsWith(".spec.ts"));

// ---------------------------------------------------------------------------
// the decision
// ---------------------------------------------------------------------------
// specText: { basename -> source text } for the derived "the spec names this
// path" rule. A spec names a file as `../src/lib/x` — so the changed path is
// looked for as `src/lib/x` (the path after `web/`), which is exactly how the
// spec's own relative import reads once normalised.
export function decide({ specFiles, mapText, changedPaths, forceFull = false, specText = new Map() }) {
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
  let attribution = "";
  for (const p of paths) {
    const base = basename(p);
    const isSpecFile = p.startsWith("web/e2e/") && base.endsWith(".spec.ts");
    if (isSpecFile) {
      if (specFiles.includes(base)) selected.add(base);
      attribution = attribution || `matched ${p}`;
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
    // rule 6: a web/ change nothing claims might still reach any spec.
    if (p.startsWith("web/")) return full(`no area maps ${p} — an unattributed web change widens the run`);
    // Outside web/: the specs drive `next start` (they stub the API at the
    // browser level), so a non-web path that no row claims cannot reach one.
  }

  if (!selected.size) return full("the diff selected no spec — refusing to report a narrowed run that ran nothing");
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
  let d;
  try {
    d = decide({ specFiles: files, mapText: readFileSync(join(root, "web", "e2e", "areas.tsv"), "utf8"), changedPaths: paths, forceFull, specText: text });
  } catch (e) {
    process.stderr.write(`e2e-scope: ${e.message} — the caller runs the whole family\n`);
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
