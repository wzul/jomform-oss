// Guard: `guides.gen.ts` is NOT the only file derived from docs/guides, so the guide workflow
// has to drive the docs generator too.
//
// THE DEFECT (card t_bd05950c, found while fixing the PR #1725 rework)
//   `npm run guides:sync` rewrote src/lib/guides.gen.ts and `npm run guides:check` compared
//   ONLY that file against docs/guides — while web/scripts/docsNav.mjs reads guides.gen.ts
//   (GUIDES_FILE) and indexes every guide's body into src/lib/docsSearch.gen.json. So after a
//   guide README edit the documented workflow reported GREEN and the `web` gate stayed RED on
//   docsNav.test.ts ("the committed registry and search index are fresh"):
//
//     $ npm run guides:sync
//     guides: wrote 8 guide(s) and 47 screenshot(s) → src/lib/guides.gen.ts + public/guides/
//     $ npm run guides:check
//     guides: 8 guide(s) in sync (affiliate-programme, … payment-fees …)        # exit 0
//     $ npm run docs:nav:check
//     the docs registry is out of date: src/lib/docsSearch.gen.json — run `npm run docs:nav`  # exit 1
//
//   `guides:check` is not a gate — it is what a PERSON runs while editing a guide, and its
//   exit 0 was handed back as evidence on card after card. The fix is wiring: both npm scripts
//   now run the docs generator, and `guides.mjs --check`'s failure text names `npm run docs:nav`.
//
// WHAT THIS FILE PINS
//   1. THE WIRING — `guides:sync` regenerates the search index, `guides:check` checks it, and
//      the premise that makes the pairing necessary (docsNav.mjs reads guides.gen.ts and writes
//      docsSearch.gen.json) is asserted rather than assumed.
//   2. THE COUPLING, on the committed data — every guide in guides.gen.ts has a search entry
//      whose description/headings/body prose came from that guide. That is what makes a stale
//      index a REAL defect rather than a wiring preference.
//   3. THE FALSE GREEN, DRIVEN — a throwaway copy of the parts the generators read (the REAL
//      package.json, the REAL scripts, the committed generated files) gets a guide README edit.
//      `npm run guides:sync` must leave BOTH derived files fresh; when the index is put back to
//      the committed (pre-edit) one, `npm run guides:check` must exit non-zero and name it.
//      Run through `npm run`, so a manifest that loses the docs step reds here.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * It does not run the `web` gate or a Next build: docsNav.test.ts pins the committed index
//     against this tree, and this file pins that the guides workflow keeps it that way.
//   * The scratch tree is not a checkout — it has no .git, so docsNav's `updated` dates come
//     from the committed registry (the documented no-git fallback). Dates are not this file's
//     subject; the indexed TEXT is.
//
// LEG 3 IS HOST ONLY — and the image build says so out loud
//   Leg 3 copies the guide SOURCE (repo-root docs/guides/<slug>/README.md + PNGs) into its
//   scratch tree. web/Dockerfile copies web/, api/ and MCP-PARITY.md, and the root .dockerignore
//   drops docs/ on purpose, so that tree does not exist inside the image: the cpSync threw
//   `ENOENT: no such file or directory, lstat '/repo/docs/guides'`, the unit suite reded, and the
//   webimage gate failed while the host `web` gate stayed green (measured on this file's first
//   head). It now skips itself there through imageBuildSkipReasonAt — the mechanism
//   src/lib/guidesContent.test.ts and src/lib/a11yScanProse.test.ts already use, recorded for this
//   file's literal in src/lib/buildContext.test.ts's HOST_ONLY registry. The read is written as a
//   `new URL("../../../docs/guides", import.meta.url)` literal so that registry can see it and
//   red if the skip is deleted. Legs 1 and 2 need only files the image DOES carry (the committed
//   generators' output and package.json), so only leg 3 is host only — not the whole file.

import { test } from "node:test";
import assert from "node:assert/strict";
import { copyFileSync, cpSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

import { GUIDES } from "./guides.gen.ts";

const LIB_DIR = dirname(fileURLToPath(import.meta.url)); // <repo>/web/src/lib
const WEB_DIR = resolve(LIB_DIR, "..", ".."); //            <repo>/web
const REPO_ROOT = resolve(WEB_DIR, ".."); //               <repo>

// The ONE host-only read in this file: the guide SOURCE tree, which the web image does not carry.
// A literal URL (not join(REPO_ROOT, "docs", "guides")) on purpose — src/lib/buildContext.test.ts
// resolves literals, so this is the read its HOST_ONLY registry pins the skip against. Deriving the
// same path from REPO_ROOT would hide it from that guard, which is how the missing skip survived a
// green host `web` gate and only failed in the image build.
const DOCS_GUIDES = fileURLToPath(new URL("../../../docs/guides", import.meta.url));

/**
 * The web image build has no repository and no repo-root docs/ tree: web/Dockerfile copies web/,
 * api/ and MCP-PARITY.md, and the root .dockerignore drops docs/. A real checkout always has at
 * least one checkout-only root entry, so an ABSENT docs/guides there still fails below instead of
 * quietly skipping the leg. (Same mechanism as src/lib/guidesContent.test.ts and
 * src/lib/a11yScanProse.test.ts; buildContext.test.ts pins that it is still here.)
 */
const CHECKOUT_ONLY_ROOT_ENTRIES = [".gitignore", "Makefile", "AGENTS.md"];

function imageBuildSkipReasonAt(root: string): string | false {
  if (existsSync(join(root, "docs", "guides"))) return false;
  const rootEntries = CHECKOUT_ONLY_ROOT_ENTRIES.filter((e) => existsSync(join(root, e)));
  if (existsSync(join(root, ".git")) || rootEntries.length > 0) return false; // a real checkout
  return (
    `the web image build has no repository and no repo-root docs/ tree: ${root} has no .git and ` +
    `no checkout-only root entry (${CHECKOUT_ONLY_ROOT_ENTRIES.join(", ")}), and web/Dockerfile ` +
    `does not copy docs/ — so this leg's subject is absent by construction, not by regression`
  );
}

function skipInImageBuild(t: { skip: (reason?: string) => void }): boolean {
  const reason = imageBuildSkipReasonAt(REPO_ROOT);
  if (reason === false) return false;
  t.skip(reason);
  return true;
}

const PKG = JSON.parse(readFileSync(join(WEB_DIR, "package.json"), "utf8")) as {
  scripts?: Record<string, string>;
};
const scripts: Record<string, string> = PKG.scripts ?? {};

/** One npm script's steps: `a && b || c; d` → [a, b, c, d] (the split webScripts.test.ts uses). */
function stepsOf(script: string): string[] {
  return script
    .split(/&&|\|\||;/)
    .map((s) => s.trim())
    .filter((s) => s.length > 0);
}

/** A step that WRITES the docs registry + search index. */
const REGENERATES_DOCS = /(?:^|\s)npm run docs:nav$|(?:^|\s)scripts\/docsNav\.mjs$/;
/** A step that CHECKS the docs registry + search index without writing. */
const CHECKS_DOCS = /(?:^|\s)npm run docs:nav:check$|(?:^|\s)scripts\/docsNav\.mjs\s+--check$/;

test("`guides:sync` and `guides:check` drive the docs generator, because it indexes the guide bodies", () => {
  const sync = scripts["guides:sync"];
  const check = scripts["guides:check"];
  assert.equal(typeof sync, "string", "web/package.json defines no `guides:sync` script");
  assert.equal(typeof check, "string", "web/package.json defines no `guides:check` script");

  // The premise first: if docsNav.mjs ever stops reading guides.gen.ts, the pairing below is
  // pointless and this file must be revisited rather than silently keep asserting it.
  const docsNav = readFileSync(join(WEB_DIR, "scripts", "docsNav.mjs"), "utf8");
  assert.match(
    docsNav,
    /GUIDES_FILE\s*=\s*join\(WEB_DIR,\s*"src",\s*"lib",\s*"guides\.gen\.ts"\)/,
    "web/scripts/docsNav.mjs no longer reads src/lib/guides.gen.ts — the search index is no longer " +
      "derived from the guide bodies, so this guard (and the guides scripts' docs step) describe nothing",
  );
  assert.match(
    docsNav,
    /OUT_SEARCH\s*=\s*join\(WEB_DIR,\s*"src",\s*"lib",\s*"docsSearch\.gen\.json"\)/,
    "web/scripts/docsNav.mjs no longer writes src/lib/docsSearch.gen.json — the file this guard exists for",
  );

  assert.ok(
    stepsOf(sync).some((step) => REGENERATES_DOCS.test(step)),
    "`guides:sync` only rewrites src/lib/guides.gen.ts. src/lib/docsSearch.gen.json indexes the guide " +
      "BODIES out of that module, so a guide edit leaves the search index stale and the `web` gate red " +
      "on docsNav.test.ts while guides:sync itself reports success. It must run `npm run docs:nav` too " +
      `(steps: ${JSON.stringify(stepsOf(sync))})`,
  );
  assert.ok(
    stepsOf(check).some((step) => CHECKS_DOCS.test(step)),
    "`guides:check` compares only guides.gen.ts against docs/guides, so it exits 0 on a tree whose " +
      "docsSearch.gen.json is stale — the false green this guard exists for. It must run " +
      `\`npm run docs:nav:check\` too (steps: ${JSON.stringify(stepsOf(check))})`,
  );
});

/** The first line of a guide body that is plain prose: no markers, no list number, long enough
 *  to be distinctive. The index stores the body with its markers stripped, so this substring is
 *  what a reader's search actually matches against. */
function firstProseLine(body: string): string {
  for (const line of body.split("\n")) {
    const t = line.trim();
    if (t.length < 60) continue;
    if (!/^[A-Za-z][^`*_[\]|#!<>]*$/.test(t)) continue;
    return t.slice(0, 60);
  }
  return "";
}

test("the committed docs search index carries the guide bodies guides.gen.ts exposes", () => {
  const index = JSON.parse(readFileSync(join(LIB_DIR, "docsSearch.gen.json"), "utf8")) as {
    docs: Array<{ path: string; title: string; description: string; headings: string[]; text: string }>;
  };
  assert.ok(GUIDES.length >= 2, `GUIDES carries ${GUIDES.length} guide(s) — the guard is looking at nothing`);
  assert.ok(index.docs.length >= GUIDES.length, `the search index holds ${index.docs.length} entries`);

  const problems: string[] = [];
  for (const guide of GUIDES) {
    const path = `/docs/guides/${guide.slug}`;
    const entry = index.docs.find((d) => d.path === path);
    if (!entry) {
      problems.push(`${path}: no search entry — a guide the docs search cannot find`);
      continue;
    }
    if (entry.description !== guide.summary) {
      problems.push(`${path}: description is not the guide's summary — the index did not come from guides.gen.ts`);
    }
    const headings = [...guide.body.matchAll(/^(#{2,3})\s+(.+)$/gm)].map((m) => m[2].trim());
    if (JSON.stringify(entry.headings) !== JSON.stringify(headings)) {
      problems.push(`${path}: headings ${JSON.stringify(entry.headings)} are not the guide body's ${JSON.stringify(headings)}`);
    }
    const prose = firstProseLine(guide.body);
    if (prose === "") {
      problems.push(`${path}: no plain prose line long enough to look for — this check would be vacuous`);
    } else if (!entry.text.includes(prose)) {
      problems.push(`${path}: the body prose "${prose}…" is not in the search text — the index is stale or does not index guide bodies`);
    }
  }
  assert.deepEqual(
    problems,
    [],
    "the committed docs search index does not carry what guides.gen.ts exposes, so `npm run docs:nav` has not " +
      `run since the guides changed:\n  ${problems.join("\n  ")}`,
  );
});

test("a guide edit leaves no stale derived file, and `guides:check` is red when the index lags", (t) => {
  // Host only: the scratch tree below is seeded from the repo-root guide SOURCE, which the web
  // image does not carry. Inside the image this prints its reason and skips; in a checkout with
  // docs/guides missing it still fails, because imageBuildSkipReasonAt returns false there.
  if (skipInImageBuild(t)) return;
  const root = mkdtempSync(join(tmpdir(), "jf-guides-check-"));
  const web = join(root, "web");

  /**
   * The parts the two generators READ, arranged exactly as in the repo. `web/package.json` is
   * copied verbatim, so the npm scripts this test drives are the real wiring, and node_modules
   * is linked because docsNav.mjs imports `typescript`.
   */
  const npm = (script: string) =>
    spawnSync("npm", ["run", "--silent", script], {
      cwd: web,
      encoding: "utf8",
      env: { ...process.env, PATH: `${dirname(process.execPath)}:${process.env.PATH ?? ""}` },
    });

  try {
    mkdirSync(join(web, "scripts"), { recursive: true });
    mkdirSync(join(web, "src", "lib"), { recursive: true });
    cpSync(DOCS_GUIDES, join(root, "docs", "guides"), { recursive: true });
    cpSync(join(WEB_DIR, "src", "app", "docs"), join(web, "src", "app", "docs"), { recursive: true });
    for (const file of ["guides.mjs", "docsNav.mjs"]) {
      copyFileSync(join(WEB_DIR, "scripts", file), join(web, "scripts", file));
    }
    for (const file of ["guides.gen.ts", "docsNav.gen.ts", "docsSearch.gen.json"]) {
      copyFileSync(join(LIB_DIR, file), join(web, "src", "lib", file));
    }
    copyFileSync(join(WEB_DIR, "package.json"), join(web, "package.json"));
    symlinkSync(join(WEB_DIR, "node_modules"), join(web, "node_modules"), "dir");

    // 1. A guide README edit — what every card that touches docs/guides/** does.
    const readme = join(root, "docs", "guides", "payment-fees", "README.md");
    const original = readFileSync(readme, "utf8");
    writeFileSync(readme, `${original.replace(/\s+$/, "")}\n\nThe guard probe phrase is quokka-lantern, written by a test.\n`);

    // 2. The documented workflow. It must leave BOTH derived files fresh — the search index is
    //    the one that used to be left behind.
    const sync = npm("guides:sync");
    assert.equal(
      sync.error,
      undefined,
      `could not run npm (${sync.error?.message}) — this guard drives the real npm scripts, so npm must be on PATH`,
    );
    assert.equal(sync.status, 0, `\`npm run guides:sync\` failed in the scratch tree:\n${sync.stdout}${sync.stderr}`);
    const freshModule = readFileSync(join(web, "src", "lib", "guides.gen.ts"), "utf8");
    assert.ok(freshModule.includes("quokka-lantern"), "`npm run guides:sync` did not pick up the README edit at all");
    const freshIndex = readFileSync(join(web, "src", "lib", "docsSearch.gen.json"), "utf8");
    assert.ok(
      freshIndex.includes("quokka-lantern"),
      "`npm run guides:sync` regenerated src/lib/guides.gen.ts but NOT src/lib/docsSearch.gen.json — the docs " +
        "search index still describes the old guide body, the `web` gate would be red on docsNav.test.ts, and " +
        "`guides:check` used to call this tree green",
    );

    // 3. The false green itself: put back the index an old `guides:sync` would have left — the
    //    committed one, which predates the edit — while the guides module stays fresh.
    const committedIndex = readFileSync(join(LIB_DIR, "docsSearch.gen.json"), "utf8");
    assert.equal(
      committedIndex.includes("quokka-lantern"),
      false,
      "the committed search index already carries the probe phrase, so this fixture would not be stale",
    );
    writeFileSync(join(web, "src", "lib", "docsSearch.gen.json"), committedIndex);
    const check = npm("guides:check");
    const checkText = `${check.stdout ?? ""}${check.stderr ?? ""}`;
    assert.notEqual(
      check.status,
      0,
      "`npm run guides:check` exited 0 while src/lib/docsSearch.gen.json was stale — that is exactly the false " +
        "green this card exists for: the guides module was fresh, the search index was not, and the `web` gate " +
        `would have been the first thing to say so:\n${checkText}`,
    );
    assert.match(
      checkText,
      /docsSearch\.gen\.json/,
      `\`npm run guides:check\` failed but did not name the stale search index, so a maintainer is pointed at the wrong file:\n${checkText}`,
    );

    // 4. …and when the guides module itself is stale, the failure text names the docs generator
    //    too: `npm run guides:sync` is the one command that fixes both.
    writeFileSync(join(web, "src", "lib", "guides.gen.ts"), `${freshModule}// drift\n`);
    const staleModule = npm("guides:check");
    const moduleText = `${staleModule.stdout ?? ""}${staleModule.stderr ?? ""}`;
    assert.notEqual(staleModule.status, 0, "`npm run guides:check` exited 0 with a stale src/lib/guides.gen.ts");
    assert.match(moduleText, /npm run guides:sync/, `the failure text does not name the fixing command:\n${moduleText}`);
    assert.match(
      moduleText,
      /npm run docs:nav/,
      "the failure text does not name `npm run docs:nav`, so a maintainer regenerates the guides module, sees " +
        `guides:check pass, and still hands back a tree whose search index is stale:\n${moduleText}`,
    );
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});
