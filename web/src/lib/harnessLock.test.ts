// The per-worktree lock in web/scripts/biteproof_docs_tool_claims.sh (card
// t_ed66094c) and — since card t_03a646e2 — in
// web/scripts/biteproof_docs_index_render_sites.sh, which mutates the SAME SHAPE
// of shared input and shipped with no lock at all.
//
// WHY THIS EXISTS (the bug it was written for): the tool-claims harness mutates two
// SHARED docs pages in place (src/app/docs/chat/page.tsx, src/app/docs/embed/page.tsx)
// and runs its guard over the WHOLE docs tree. PR 846 fixed the verdict RULE and the
// verdict CHANNEL (per-run output, per-run `.ts` copies), but the shared INPUT stayed:
// a second run started while the first was mid-case read a page the first had already
// mutated and attributed that mutation to its own case. Reproduced on PR 846's head,
// 3/3 rounds: two runs 2s apart in ONE worktree printed 33 false `***` verdict lines
// across 6 runs while the exit code stayed 1 — `OLD guard wanted GREEN, got RED`,
// `NEW guard wanted GREEN, got RED`, `NEW guard RED but the failure does not name …`.
// No per-run channel can fix that: the thing being shared is the input. The fix is a
// per-worktree `flock`, taken before the premise check (which READS the pages) and
// released when the process exits.
//
// THE SECOND HARNESS IS THE SAME DEFECT, UNFIXED (card t_03a646e2). The render-sites
// harness rewrites `src/app/docs/page.tsx` — every `render_sites_off` / `comment_out_entry`
// / `insert_at_line` writes it — and `run_guard` reads it throughout, but it carried NO
// lock (`grep -c flock` was 0) and this file guarded only its sibling, because the
// HARNESS const below named one script. Reproduced by QA on the tag tree f63fa325: two
// runs 1s apart in ONE worktree printed 7 and 4 false `***` lines (a false
// `baseline: NEW guard wanted GREEN, got RED` among them — the suite reading as broken)
// against 0 for a solo run at the same base, 9 and 11 BITES instead of 13, rc=1 for both.
// No false PASS was reachable, but the wrong verdict lines are the whole harm. Every
// harness of the family is therefore driven here, one entry per harness, and the legs are
// the same for each.
//
// THE THIRD HARNESS IS THE SAME DEFECT, UNFIXED (card t_f4f4681c). The
// prose-convention harness mutates FOUR SHARED files under `src/app/docs`
// (`chat/page.tsx`, `security/page.tsx`, `sales/page.tsx`, `layout.tsx`) and its
// `run_guard` reads the WHOLE tree, but it carried NO lock (`grep -c flock` was 0)
// and this file guarded only its two siblings. Reproduced in ONE worktree on
// `origin/main`: two runs started together printed a FALSE `6/6 HOLE cases RED,
// 0/3 CONTROLS GREEN` (the three CONTROLS read RED because a peer's marker was on
// disk when the guard read the tree) with both rc=1, and the collision LEFT TWO
// PROBE MUTATIONS ON DISK — so every later `test:unit` in that tree judged a
// mutated tree. Those are the counts the harness stood at on `origin/main` when
// this lock was written (six HOLE / three CONTROLS); on the tree it now sits on it
// is EIGHT HOLE / FIVE CONTROLS over the fourteen labelled cases `CASE 0`..`CASE 13`
// (cards `t_8b3a508b` and `t_763c28c2` added CASE 10..13), and a solo run prints
// `8/8 HOLE cases RED, 5/5 CONTROLS GREEN`.
//
// THE LOCK IS PER FAMILY, KEYED ON THE SHARED INPUT (card t_76b3356d). Cards
// t_ed66094c, t_03a646e2 and t_f4f4681c each gave ONE harness a lock under its OWN
// directory, which serialised two runs of the SAME script and left two DIFFERENT
// harnesses of the family OVERLAPPING — and they all mutate the SAME tree. Measured
// 3/3 rounds with prose + tool_claims started together in ONE worktree: tool_claims
// printed 6 false `***` lines against 4 for a solo run of the same bytes, including a
// FALSE `registered name: NEW guard wanted GREEN, got RED` on its OWN negative control
// (a control that must stay GREEN read RED). The shared thing is the INPUT, so no
// per-script channel could fix it. EVERY harness of the family now derives the SAME
// lock file, `jomform-biteproof-docs-bite`, so any two of them serialise in a
// worktree; the two that carried no lock at all (`biteproof_docs_index_comment_anchor.sh`,
// `biteproof_mcp_tool_inventory.sh`) were given it here for the same reason. This file
// therefore drives TEN harnesses, one entry each, and the cross-harness leg below is
// the one that fails if the key goes back to per-script.
//
// THE TWO ENTRIES CARD t_df41e675 ADDED make the list EIGHT (it was six): the save-button
// harness this PR added (`biteproof_save_button.sh`) and its already-on-main sibling
// (`biteproof_settings_tabs.sh`) both mutate SHARED sources and shipped with NO lock
// (`grep -c flock` was 0 in both). The save-button one rewrites
// `src/components/SaveButton.tsx`, `src/app/settings/page.tsx` and
// `src/components/i18nDict.ts`; the settings-tabs one rewrites that SAME `settings/page.tsx`
// byte for byte, and `i18nDict.ts` is also mutated by the prose harness's CASE 17 above — so
// all three overlap. Measured pre-fix: two concurrent runs of the save-button harness in ONE
// worktree, 12 rounds, 9 of 12 rounds printed a FALSE verdict line or left a source holding a
// peer's bytes, every round still printing `all sources restored byte-identically`. They now
// take the SAME family key, which is why they are entries here rather than a sentence in the
// "does not cover" paragraph below.
//
// WHAT THIS PINS. Every leg below RUNS the real script and reads its real exit code and
// its real message — none of them greps the file's text, because the sharpest way to
// break this lock leaves the text perfectly healthy. Each leg is chosen so that removing
// the thing it guards makes the harness RUN instead of REFUSING, and the two outcomes
// have different exit codes:
//
//   A. A peer holds this worktree's lock and `BITEPROOF_LOCK_WAIT=0`: the run must exit
//      75 with the timeout reason, having mutated nothing. Without the lock take the
//      harness instead runs its whole body and exits 1 — so this leg proves the lock
//      is really taken on the path the harness derives, not merely mentioned. It is also
//      what proves the derived path is THIS worktree's: the peer holds the file whose
//      name is re-derived here from the same documented rule, so a key that drifted would
//      make this leg's exit code 1 instead of 75.
//   B. `BITEPROOF_LOCKED=1` with a lock file that is FREE: the run must exit 2 naming the
//      lock it could not prove it held. This is the leg that keeps the fix from being
//      inert — `flock` blocks whether or not anyone checks, so without an explicit check
//      a decorative lock would pass leg A by accident.
//   C. `flock` not on PATH: the run must exit 2 naming flock, rather than running
//      unserialised. A harness that silently degrades to "no lock" is the same defect
//      with a green light on it.
//   D. The key is PER WORKTREE. With THIS worktree's lock held by the peer: the run in
//      the repo root must be refused (D1) and the same run started from `web/` must be
//      refused too (D2 — the key does not depend on the caller's CWD). D3 holds a
//      DIFFERENT worktree's key in the SAME repo and requires the run NOT to be blocked:
//      a key that collapsed to one file per repo would make every card's lane queue
//      behind every other card's.
//   E. The key is the FAMILY'S, not the script's. With THIS worktree's lock held by the
//      peer, a DIFFERENT harness of the family must ALSO be refused (exit 75, nothing
//      run). This is the leg card t_76b3356d exists for: it fails (the peer runs its body
//      and exits 1) if any harness goes back to a per-script directory. It asserts the
//      shared key as a hard equality across every entry, so the property is checked, not
//      merely documented.
//
// D3 IS DERIVED, NOT HARDCODED (card t_03a646e2). The first cut named
// `<repoRoot>/.worktrees/t_ed66094c` and guarded the whole leg with `existsSync`, so on
// every card's worktree — the only place this gate actually runs — the directory does not
// exist and the leg was SILENTLY SKIPPED, which is how a leg that proves "two worktrees
// do not serialise" comes to prove nothing. The peer's tree here is the PRIMARY tree,
// resolved from `git rev-parse --git-common-dir` (a linked worktree resolves it to
// `<primary>/.git`, the primary tree to its own `.git`), so it exists on every tree. Run
// FROM the primary tree the leg is vacuous by construction (both roots are the same), so
// it logs that and skips rather than pretending — every card's lane is a linked worktree,
// which is the case the leg exists for.
//
// None of these legs reaches node: they all exit inside the harness's lock block or its
// base resolution. That is a measured constraint, not a preference — a full harness run
// inside `npm run test:unit` nests node's test runner inside node's test runner, and
// measured while building this file, that nesting made the harness's premise check read
// the base guard as "did not RUN": a false REFUSAL produced by the guard rather than
// found by it. The legs that need a real guard run stay in the biteproof harness itself,
// which self-bites (see the BITE-HARNESSES.md rows for these scripts).
//
// WHAT IT DOES NOT COVER (stated honestly): that a lock-HOLDING run produces no false
// verdict lines is measured by each harness on a staggered pair of runs, not here. It
// also cannot see a reader that mutates a shared page WITHOUT calling these scripts.
// It DOES now serialise two DIFFERENT harnesses of the family against each other (leg E,
// card t_76b3356d): they all key on the SAME file because they all mutate the same docs
// tree. It also now covers the two settings-side harnesses (card t_df41e675) and the
// forms / next-config pair (card t_f6408658) — entries 7..10 — because they mutate the
// same shared sources. What it still does not serialise is a harness OUTSIDE this family
// (e.g. `operator-bite.sh`, which mutates `i18nDict.ts` but takes no lock) that mutates a
// file one of these walks. `operator-bite.sh` is outside the family and is NOT an entry
// here: it takes no lock at all, so an entry would have to assert a lock that does not
// exist.
//
// THE FILE NAME IS LOAD-BEARING, and deliberately avoids "bite": the harness-inventory
// guard (`api/internal/biteharness`, inside the `api test` gate) classifies any file whose
// BASENAME contains "bite" case-insensitively as a manual bite harness and fails the build
// unless `BITE-HARNESSES.md` carries a row for it — the exclusion it applies is only the
// `_test.go` suffix, so a `*.test.ts` in `src/lib` is not exempt. This file is a GATE test,
// not an on-demand instrument, so a row for it would put a false claim in a table whose
// whole meaning is "none of these is a per-merge gate". Measured, not guessed: a first cut
// named `biteproofLock.test.ts` failed `TestEveryBiteHarnessIsRegisteredInTheInventory`
// with `does not name 1 bite harness(es) that exist on disk`. Hence `harnessLock.test.ts`.

import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync, spawn, spawnSync } from "node:child_process";
import { existsSync, mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));            // web/src/lib
const webRoot = join(here, "..", "..");                          // web/
const repoRoot = join(webRoot, "..");                            // repo root

// One entry per harness this lock has to cover. The lock directory is the SAME for every
// entry — one file per worktree, keyed on the family's shared INPUT — so it is derived
// once here and asserted identical across entries below. (Until card t_76b3356d each
// harness carried its OWN directory, which serialised two runs of the same script but left
// two DIFFERENT harnesses of the family overlapping; the D3 assertion below used to assert
// that the two harnesses derive DIFFERENT files, and card t_76b3356d INVERTS it: every
// harness derives the SAME file, because every one of them mutates the same docs tree.)
const FAMILY_LOCK_SUBDIR = "jomform-biteproof-docs-bite";
type HarnessEntry = {
  label: string;
  script: string;
  lockSubdir: string;
  // true when this harness MUTATES the family's shared input (files under src/app/docs, or
  // the settings/component sources the two card-t_df41e675 entries mutate). Every entry here
  // does; the flag exists so the cross-harness leg below can state WHY a peer holding the
  // family key must block this run, and so a future reader-only entry can be marked honestly
  // instead of silently inheriting the claim.
  mutatesSharedInput: boolean;
  // How D3 reaches this harness's deterministic, node-free refusal AFTER it has taken
  // ITS OWN (free) lock: an unresolvable base ref for the four that read one, and a
  // missing guard path for the prose harness, and a missing PAGE for the MCP-inventory
  // harness (checked before any base resolution). The needle is the message that proves
  // the run got PAST the lock to that refusal, not the lock timeout.
  d3ProbeEnv: Record<string, string>;
  d3ProbeNeedle: RegExp;
};

const HARNESSES: HarnessEntry[] = [
  {
    label: "biteproof_docs_tool_claims.sh",
    script: join(webRoot, "scripts", "biteproof_docs_tool_claims.sh"),
    lockSubdir: FAMILY_LOCK_SUBDIR,
    mutatesSharedInput: true,
    d3ProbeEnv: { BITEPROOF_BASE: "refs/heads/jomform-nonexistent-probe" },
    d3ProbeNeedle: /cannot resolve refs\/heads\/jomform-nonexistent-probe/,
  },
  {
    label: "biteproof_docs_index_render_sites.sh",
    script: join(webRoot, "scripts", "biteproof_docs_index_render_sites.sh"),
    lockSubdir: FAMILY_LOCK_SUBDIR,
    mutatesSharedInput: true,
    d3ProbeEnv: { BITEPROOF_BASE: "refs/heads/jomform-nonexistent-probe" },
    d3ProbeNeedle: /cannot resolve refs\/heads\/jomform-nonexistent-probe/,
  },
  {
    label: "biteproof_docs_prose_convention.sh",
    script: join(webRoot, "scripts", "biteproof_docs_prose_convention.sh"),
    lockSubdir: FAMILY_LOCK_SUBDIR,
    mutatesSharedInput: true,
    d3ProbeEnv: { BITEPROOF_GUARD: join(webRoot, "scripts", ".no-such-guard-probe.ts") },
    d3ProbeNeedle: /is missing — nothing to drive/,
  },
  {
    label: "biteproof_llms_full_coverage.sh",
    script: join(webRoot, "scripts", "biteproof_llms_full_coverage.sh"),
    lockSubdir: FAMILY_LOCK_SUBDIR,
    mutatesSharedInput: true,
    d3ProbeEnv: { BITEPROOF_BASE: "refs/heads/jomform-nonexistent-probe" },
    d3ProbeNeedle: /cannot resolve refs\/heads\/jomform-nonexistent-probe/,
  },
  {
    label: "biteproof_docs_index_comment_anchor.sh",
    script: join(webRoot, "scripts", "biteproof_docs_index_comment_anchor.sh"),
    lockSubdir: FAMILY_LOCK_SUBDIR,
    mutatesSharedInput: true,
    d3ProbeEnv: { BITEPROOF_BASE: "refs/heads/jomform-nonexistent-probe" },
    d3ProbeNeedle: /cannot resolve refs\/heads\/jomform-nonexistent-probe/,
  },
  {
    // Card t_f6408658. Mutates web/next.config.mjs, web/.gitignore and
    // web/tsconfig.json in place, and web/scripts/biteproof_dev_hosts.sh mutates
    // next.config.mjs too — so it must hold the FAMILY key, not its own.
    label: "biteproof_next_managed_files.sh",
    script: join(webRoot, "scripts", "biteproof_next_managed_files.sh"),
    lockSubdir: FAMILY_LOCK_SUBDIR,
    mutatesSharedInput: true,
    // A missing config file: refused AFTER the lock is taken, before any node run.
    d3ProbeEnv: { BITEPROOF_CONFIG: "next.config.mjs.not-a-real-file" },
    d3ProbeNeedle: /BITEPROOF REFUSED: next\.config\.mjs\.not-a-real-file is missing/,
  },
  {
    // Card t_f6408658. Mutates src/app/forms/page.tsx in place; nothing else in
    // the family does, but it shares the family's /tmp lock DIRECTORY and the
    // same "restore from a fixed name" hazard, so it takes the same key.
    label: "biteproof_forms_bulk_failure.sh",
    script: join(webRoot, "scripts", "biteproof_forms_bulk_failure.sh"),
    lockSubdir: FAMILY_LOCK_SUBDIR,
    mutatesSharedInput: true,
    d3ProbeEnv: { BITEPROOF_PAGE: "src/app/forms/.no-such-page-probe.tsx" },
    d3ProbeNeedle: /BITEPROOF REFUSED: src\/app\/forms\/\.no-such-page-probe\.tsx is missing/,
  },
  {
    label: "biteproof_mcp_tool_inventory.sh",
    script: join(webRoot, "scripts", "biteproof_mcp_tool_inventory.sh"),
    lockSubdir: FAMILY_LOCK_SUBDIR,
    mutatesSharedInput: true,
    d3ProbeEnv: { BITEPROOF_PAGE: "src/app/docs/.no-such-page-probe.tsx" },
    d3ProbeNeedle: /missing src\/app\/docs\/\.no-such-page-probe\.tsx/,
  },
  // CARD t_df41e675 ADDED THESE TWO, and the reason is the same one leg E exists for: they
  // mutate the SAME shared sources the rest of the family does, and they shipped with NO
  // lock (`grep -c flock` was 0 in both). `biteproof_save_button.sh` rewrites
  // src/components/SaveButton.tsx, src/app/settings/page.tsx and src/components/i18nDict.ts;
  // `biteproof_settings_tabs.sh` rewrites that SAME settings/page.tsx byte for byte — so those
  // two overlap with EACH OTHER, and `i18nDict.ts` is also mutated by
  // `biteproof_docs_prose_convention.sh` (its CASE 17), which already carries the family key.
  // Measured pre-fix on `biteproof_save_button.sh`: two concurrent runs in ONE worktree, 12
  // rounds, 9 of 12 rounds printed a FALSE verdict line (`DID NOT BITE — missing dict key`,
  // `— forked base class`, `— false return ignored`, `— rejection treated as success`,
  // `— no unmount cleanup`, `— no success boolean from the handler`) or left a source holding a
  // peer's bytes, EVERY round still printing `all sources restored byte-identically`. The
  // per-run `mktemp -d` the save-button harness shipped fixed only the BACKUP path; the restore
  // still wrote the shared file in place, which is the same half PR #1013/t_d101a943 fixed for
  // `biteproof_dev_hosts.sh`. `settings_tabs` had the worse shape: a HOST-GLOBAL
  // `/tmp/biteproof-dc06.<name>.bak`, so a peer overwrote this run's own backups. Both now
  // take the family key (they share input with the family, so they must serialise against it),
  // which is why they belong in THIS list rather than the "does not cover" paragraph.
  {
    label: "biteproof_save_button.sh",
    script: join(webRoot, "scripts", "biteproof_save_button.sh"),
    lockSubdir: FAMILY_LOCK_SUBDIR,
    mutatesSharedInput: true,
    d3ProbeEnv: { BITEPROOF_GUARD: join(webRoot, "scripts", ".no-such-guard-probe.ts") },
    d3ProbeNeedle: /is missing — nothing to drive/,
  },
  {
    label: "biteproof_settings_tabs.sh",
    script: join(webRoot, "scripts", "biteproof_settings_tabs.sh"),
    lockSubdir: FAMILY_LOCK_SUBDIR,
    mutatesSharedInput: true,
    d3ProbeEnv: { BITEPROOF_SPEC: join(webRoot, "scripts", ".no-such-spec-probe.ts") },
    d3ProbeNeedle: /is missing — nothing to drive/,
  },
];

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

const git = (args: string[], cwd: string) =>
  execFileSync("git", args, { cwd, encoding: "utf8" }).trim();

// The lock path a harness derives, re-derived here from the same documented rule so a
// change to one without the other FAILS rather than agreeing by luck: sha256 of the
// worktree's absolute git dir, first 16 hex chars, under <base>/<lockSubdir>. Note the
// digest is over `printf '%s'` (no trailing newline) — the harness pipes the git output
// through `printf '%s'`, and a `sha256sum` over the command's own newline-terminated
// output derives a DIFFERENT key that never matches, which would make every leg here
// read "not blocked" and pass vacuously.
function lockFileFor(worktree: string, base: string, lockSubdir: string): string {
  const gitDir = git(["rev-parse", "--absolute-git-dir"], worktree);
  const key = execFileSync("sha256sum", [], { input: gitDir, encoding: "utf8" }).slice(0, 16);
  return join(base, lockSubdir, `${key}.lock`);
}

// Is <lock> free right now? Asked by TRYING to take it with a separate process — the same
// observation the harness itself makes.
function lockIsFree(lock: string): boolean {
  return spawnSync("flock", ["-n", lock, "true"], { encoding: "utf8" }).status === 0;
}

// Poll a condition instead of sleeping a fixed time. A fixed sleep makes this guard flaky
// on the loaded agent host (a `flock` process may take longer than the sleep to start),
// and a flaky guard would red the `web build` gate for a reason unrelated to the lock.
async function waitUntil(cond: () => boolean, timeoutMs: number, what: string): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (cond()) return;
    await sleep(50);
  }
  assert.fail(`${what} did not happen within ${timeoutMs}ms`);
}

// THE ONE LEGITIMATE SKIP, asserted positively rather than inferred from a failed
// command: `web/Dockerfile` runs `npm run build` (= this suite) inside
// `node:24-alpine`, which ships no git, on a build context whose root has no `.git`
// (the root `.dockerignore` drops it). Without this the whole `webimage` gate dies
// with `spawnSync git ENOENT` before any leg can run — which is what it did, measured
// on a pristine `origin/main` tree as well as this branch (card `t_10065a1e`).
//
// The conjunction is the identical one `web/src/lib/nextManagedFiles.test.ts` uses
// (`imageBuildEvidence`, card `t_7add0cfa`): no `.git` AND no checkout-only root
// entry. `.gitignore` is the load-bearing marker — it is NOT in the root
// `.dockerignore`, so its absence cannot be explained by an ignore rule, only by
// there being no repo root at all, whereas ANY other root file (`api/go.mod`, `web/`)
// ships in every checkout and would also match a checkout that merely lost its git
// metadata. A real checkout with broken git keeps `.gitignore` and still FAILS here.
const CHECKOUT_ONLY_ROOT_ENTRIES = [".gitignore", "Makefile", "AGENTS.md"];
// Exported for the premise test below via an injectable root: the predicate must be
// provably keyed on BOTH halves, and that is only demonstrable against synthetic trees.
function imageBuildSkipReasonAt(root: string): string | false {
  const rootEntries = CHECKOUT_ONLY_ROOT_ENTRIES.filter((e) => existsSync(join(root, e)));
  const noRepo = !existsSync(join(root, ".git")) && rootEntries.length === 0;
  return noRepo
    ? `the web image build has no repository: ${root} has no .git and no ` +
      `checkout-only root entry (${CHECKOUT_ONLY_ROOT_ENTRIES.join(", ")})`
    : false;
}
const imageBuildSkipReason = () => imageBuildSkipReasonAt(repoRoot);

function runHarness(env: NodeJS.ProcessEnv, cwd = repoRoot, script: string) {
  const r = spawnSync("bash", [script], { cwd, env, encoding: "utf8" });
  return { status: r.status, out: `${r.stdout ?? ""}${r.stderr ?? ""}` };
}

// The PRIMARY tree of this repo, derived from any tree in it: a linked worktree resolves
// `--git-common-dir` to `<primary>/.git`, the primary tree to its own `.git`. Used only by
// D3, and it exists on every tree — unlike a hardcoded sibling worktree path.
//
// DELIBERATELY NOT EVALUATED AT MODULE LOAD. It was, and that was a regression on the one
// tree this test cannot serve: `web/Dockerfile` runs `npm run build` (= this suite) inside
// `node:24-alpine`, which ships no git, so a module-scope `git` spawn kills the IMAGE BUILD
// before any in-test skip can run (the skip in flight for that is card `t_10065a1e`/PR 866,
// and a load-time spawn here would defeat it). Every other git call in this file is inside
// the test body; this one is now too, and it fails soft with the reason stated rather than
// throwing at discovery.
function gitCommonDirOf(worktree: string): string | undefined {
  try {
    return git(["rev-parse", "--path-format=absolute", "--git-common-dir"], worktree);
  } catch {
    return undefined;
  }
}

test("every /docs bite harness serialises on ONE per-worktree lock, keyed on the shared input (t_ed66094c, t_03a646e2, t_f4f4681c, t_76b3356d)", async (t) => {
  const imageReason = imageBuildSkipReason();
  if (imageReason) {
    t.skip(imageReason);
    return;
  }
  const gitCommonDir = gitCommonDirOf(repoRoot);
  const primaryRoot = gitCommonDir === undefined ? undefined : dirname(gitCommonDir);
  for (const { label, script, lockSubdir, d3ProbeEnv, d3ProbeNeedle } of HARNESSES) {
    assert.ok(existsSync(script), `the harness is missing at ${script}`);

    // A private lock base per run: this guard never touches a real lane's lock, and two
    // concurrent runs of THIS guard cannot collide either.
    //
    // THE INHERITED LOCK STATE IS STRIPPED (card t_df41e675). This guard is itself run BY a
    // harness that holds the family lock and exports `BITEPROOF_LOCKED=1` /
    // `BITEPROOF_LOCK_FILE` — `biteproof_settings_tabs.sh` runs `npm run build` (=
    // `test:unit` = this file) from inside its lock block. A spawned harness that inherits
    // that state takes the CHILD path and skips its own lock take, so leg A read exit 2
    // (the harness ran past its lock block to an unrelated refusal) where it requires 75 —
    // measured, the whole `web` gate went red with `biteproof_docs_tool_claims.sh: ... must
    // exit 75 ... Got 2`. A guard whose verdicts depend on the lock state of whoever
    // spawned it is the same unverified-lock shape this family's rule removes. `delete`
    // (not `= ""`) so `BITEPROOF_LOCKED` is genuinely absent for a leg that wants the
    // harness to take the lock itself; leg B re-adds both explicitly, and the `env -u`
    // in the harness that spawned us keeps its own children clean too.
    const base = mkdtempSync(join(tmpdir(), "biteguard-"));
    const inherited = { ...process.env };
    delete inherited.BITEPROOF_LOCKED;
    delete inherited.BITEPROOF_LOCK_FILE;
    const env = { ...inherited, BITEPROOF_LOCK_DIR: base };
    const ours = lockFileFor(repoRoot, base, lockSubdir);
    const nowait = { ...env, BITEPROOF_LOCK_WAIT: "0" };
    const run = (e: NodeJS.ProcessEnv, cwd = repoRoot, s = script) => runHarness(e, cwd, s);

    try {
      mkdirSync(join(base, lockSubdir), { recursive: true });
      writeFileSync(ours, "");
      assert.ok(lockIsFree(ours), `${label}: the lock must be free before leg A (premise of this leg)`);

      // The holder runs `sleep` as a CHILD, which inherits the locked descriptor — so the
      // hold lasts until that child exits, not until flock is signalled. Both are awaited
      // before leg B for that reason: killing flock alone leaves the lock held (measured),
      // and it silently turned leg B into a full run of the harness's body.
      const holder = spawn("flock", [ours, "bash", "-c", "sleep 8"], { stdio: "ignore" });
      const holderDone = new Promise<void>((res) => holder.on("close", () => res()));
      await waitUntil(() => !lockIsFree(ours), 5000, `${label}: the holder taking the lock`);

      // --- A: a peer holds the lock and the wait is 0 -> refuse, do not run. --------
      const blocked = run(nowait);
      assert.equal(
        blocked.status, 75,
        `${label}: with a peer holding this worktree's lock and a zero wait, the harness ` +
        `must exit 75 (it never got the lock, so it must not run). Got ${blocked.status}:\n${blocked.out}`,
      );
      assert.match(
        blocked.out, /could not take the per-worktree lock/,
        `${label}: the timeout must say which lock it could not take. Got:\n${blocked.out}`,
      );
      assert.doesNotMatch(
        blocked.out, /BITES|\*\*\*|^old guard \(/m,
        `${label}: the harness ran its body without the lock, so it would have read pages a ` +
        `peer may have mutated. Got:\n${blocked.out}`,
      );

      // --- E: a DIFFERENT harness of the family is refused by the SAME lock. --------
      // THE CROSS-HARNESS LEG (card t_76b3356d). Until this card each harness keyed its
      // lock on its OWN directory, so two runs of the same script serialised but two
      // DIFFERENT harnesses of the family overlapped in one worktree and read each other's
      // mutations (measured 3/3 rounds: tool_claims printed 6 false `***` lines against 4
      // for a solo run, including a false `NEW guard wanted GREEN, got RED` on its own
      // negative control). The family shares the SAME input — files under `src/app/docs` —
      // so every harness must derive the SAME file. A revert to a per-script directory
      // makes the peer's key FREE here and it runs its body (exit 1) instead of 75, so
      // this leg is what turns RED when the shared-key change is undone.
      const peerEntry = HARNESSES.find((h) => h.label !== label && h.mutatesSharedInput);
      assert.ok(
        peerEntry,
        `no DIFFERENT harness of the family to drive — the cross-harness leg proves nothing`,
      );
      assert.equal(
        peerEntry.lockSubdir, lockSubdir,
        `${label} and ${peerEntry.label} derive DIFFERENT lock directories, so two harnesses ` +
        `that mutate the SAME docs tree would overlap in one worktree (t_76b3356d)`,
      );
      const peerBlocked = run(nowait, repoRoot, peerEntry.script);
      assert.equal(
        peerBlocked.status, 75,
        `${label}: while this worktree's lock is held, ${peerEntry.label} must be refused by ` +
        `the SAME lock (the family shares its input). Got ${peerBlocked.status} — a per-script ` +
        `key leaves them overlapping:\n${peerBlocked.out}`,
      );
      assert.match(
        peerBlocked.out, /could not take the per-worktree lock/,
        `${label}: ${peerEntry.label} was refused without naming the lock:\n${peerBlocked.out}`,
      );
      assert.doesNotMatch(
        peerBlocked.out, /BITES|\*\*\*|^old guard \(/m,
        `${label}: ${peerEntry.label} RAN while a peer held the family lock, so it would have ` +
        `read docs files a peer may have mutated. Got:\n${peerBlocked.out}`,
      );

      // --- D1/D2: the key does not depend on the caller's CWD. ---------------------
      for (const [where, cwd] of [["repo root", repoRoot], ["web/", webRoot]] as const) {
        const r = run(nowait, cwd);
        assert.equal(
          r.status, 75,
          `${label}: started from ${where}, the harness must derive the SAME per-worktree ` +
          `lock and be refused by the peer holding it. Got ${r.status}:\n${r.out}`,
        );
        assert.match(r.out, /could not take the per-worktree lock/,
          `${label}: started from ${where}, the refusal lost its reason:\n${r.out}`);
      }

      await holderDone;
      await waitUntil(() => lockIsFree(ours), 5000, `${label}: the holder releasing the lock`);

      // --- D3: a DIFFERENT worktree in the same repo is NOT serialised against this. -
      // Runs only once `ours` is free, so the ONLY lock held is the other tree's: if the
      // key collapsed to one file per repo, this run would read 75 instead of 2.
      if (primaryRoot === undefined) {
        // No git here (the shipping image build). Stated, not silently skipped — the rest
        // of the legs above still ran, since only this one derives the peer tree.
        console.log(`  ${label}: D3 skipped — git is not available here, so the peer tree ` +
          `cannot be derived (this is the image build, where the whole file is skipped anyway)`);
      } else if (primaryRoot === repoRoot) {
        // Run from the primary tree both roots are one tree, so there is no second key to
        // hold. Stated, not silently skipped: this leg's subject does not exist here.
        console.log(`  ${label}: D3 skipped — this run IS the primary tree (${repoRoot})`);
      } else {
        const peerLock = lockFileFor(primaryRoot, base, lockSubdir);
        assert.notEqual(
          ours, peerLock,
          `${label}: the linked worktree and the primary tree derive the SAME lock file, so ` +
          `every card's lane would serialise behind every other card's`,
        );
        // Hold the OTHER tree's key, then run with this harness's `d3ProbeEnv` — which
        // reaches ITS OWN deterministic, node-free refusal after taking its own (free)
        // lock. If the peer's key wrongly covered this tree, the exit code would be 75
        // instead.
        writeFileSync(peerLock, "");
        const peerHolder = spawn("flock", [peerLock, "bash", "-c", "sleep 8"], { stdio: "ignore" });
        const peerDone = new Promise<void>((res) => peerHolder.on("close", () => res()));
        try {
          await waitUntil(() => !lockIsFree(peerLock), 5000,
            `${label}: the peer taking the other tree's lock`);
          const other = run(
            { ...nowait, ...d3ProbeEnv },
            repoRoot,
          );
          assert.equal(
            other.status, 2,
            `${label}: a run in this worktree was blocked by ANOTHER worktree's lock, so the ` +
            `key is not per-worktree. Got ${other.status}:\n${other.out}`,
          );
          assert.match(
            other.out, d3ProbeNeedle,
            `${label}: the run did not get as far as its own pre-guard refusal, so its exit 2 proves ` +
            `nothing about the lock. Got:\n${other.out}`,
          );
          assert.doesNotMatch(
            other.out, /could not take the per-worktree lock/,
            `${label}: this worktree queued behind another tree's lock:\n${other.out}`,
          );
        } finally {
          await peerDone;
        }
      }

      // --- B: locked=1 with a FREE lock file -> refuse, do not run. ----------------
      const unverified = run({ ...env, BITEPROOF_LOCKED: "1", BITEPROOF_LOCK_FILE: ours });
      assert.equal(
        unverified.status, 2,
        `${label}: a BITEPROOF_LOCKED=1 run whose lock file is FREE must REFUSE (exit 2), not ` +
        `print verdicts. Got ${unverified.status}:\n${unverified.out}`,
      );
      assert.match(
        unverified.out, /was not held here/,
        `${label}: the refusal must name the lock it could not prove it held. Got:\n${unverified.out}`,
      );

      // --- C: no flock on PATH -> refuse, do not run unserialised. -----------------
      // DETERMINISTIC, not PATH-conditional (card `t_10065a1e`'s second finding): a
      // scratch PATH directory populated with the tools the harness needs and NOTHING
      // else, so the leg runs on every host. `PATH=/usr/bin:/bin` never covered the
      // refusal anywhere flock ships in those directories — which is every host here,
      // so the refusal was unpinned while appearing covered.
      //
      // THE TOOL LIST MUST COVER THE HARNESS'S PRE-LOCK PROLOGUE, NOT JUST ITS LOCK
      // BLOCK (card `t_cd035f15`). Every entry above resolves `SELF` with
      // `basename "$0"` BEFORE it tests `command -v flock`, so with `basename` absent
      // the run died at its own line 67/53 (`basename: command not found`) and only
      // THEN reached the refusal this leg names — the assertion below was satisfied by
      // a harness that had already failed for an unrelated reason, so the leg did not
      // isolate the missing-`flock` condition it claims to. The absence of a
      // `command not found` line is asserted after the refusal (below) so the leg can
      // only pass when the refusal is the run's FIRST failure, not its second.
      const noFlockPath = mkdtempSync(join(tmpdir(), "bite-nopath-"));
      try {
        for (const tool of ["bash", "basename", "dirname", "pwd", "mktemp", "sha256sum", "cut", "git"]) {
          const which = spawnSync("bash", ["-c", `command -v ${tool}`], { encoding: "utf8" });
          const at = which.stdout.trim();
          if (which.status === 0 && at !== "") {
            writeFileSync(join(noFlockPath, tool), `#!/bin/sh\nexec ${at} "$@"\n`, { mode: 0o755 });
          }
        }
        // The premise of THIS leg, asserted rather than assumed: flock must be ABSENT
        // from the scratch PATH and a needed tool must be present (or the leg would be
        // measuring a harness that cannot start at all).
        const scratchHasFlock =
          spawnSync("bash", ["-c", "command -v flock"], { env: { ...env, PATH: noFlockPath }, encoding: "utf8" }).status === 0;
        assert.ok(!scratchHasFlock, `${label}: premise failed — flock resolves on the scratch PATH`);
        const noFlock = run({ ...env, PATH: noFlockPath });
        assert.equal(
          noFlock.status, 2,
          `${label}: with no flock on PATH the harness must REFUSE (exit 2) rather than run ` +
          `unserialised. Got ${noFlock.status}:\n${noFlock.out}`,
        );
        assert.match(
          noFlock.out, /flock is not installed/,
          `${label}: the refusal must name the missing tool. Got:\n${noFlock.out}`,
        );
        // THE REFUSAL MUST BE THE RUN'S FIRST FAILURE. Without this, the leg above is
        // satisfied by a harness that already died earlier on a tool the scratch PATH
        // forgot — which is exactly what `basename` was (card `t_cd035f15`): the run
        // printed `basename: command not found` and only THEN the flock refusal, so the
        // leg passed while measuring a harness that could not even resolve `SELF`. A
        // scratch PATH missing any tool the pre-lock prologue calls turns this red.
        assert.doesNotMatch(
          noFlock.out, /command not found/,
          `${label}: the scratch PATH is missing a tool this harness needs BEFORE its ` +
          `flock check, so this leg is not isolating the missing-flock condition it ` +
          `names — the refusal it asserts came second:\n${noFlock.out}`,
        );
      } finally {
        rmSync(noFlockPath, { recursive: true, force: true });
      }
    } finally {
      rmSync(base, { recursive: true, force: true });
    }
  }
});

// The premise of the skip above, asserted rather than assumed (the pattern main's
// `nextManagedFiles.test.ts` uses): an absence-as-evidence skip also goes green over a
// real checkout whose git metadata was pruned or renamed, which is exactly the tree this
// test exists to catch. Driven against SYNTHETIC trees so the assertion holds on the host
// AND inside the image build (where there is no `.git` at all), and so both halves of the
// conjunction are each shown to be load-bearing rather than agreeing by luck.
test("the image-build skip fires only on the image build, never on a repo without git", () => {
  const scratch = mkdtempSync(join(tmpdir(), "bite-skip-"));
  try {
    const bare = join(scratch, "bare");           // no .git, no checkout-only entry
    const brokenCheckout = join(scratch, "broken"); // no .git, but .gitignore present
    const realCheckout = join(scratch, "real");     // .git present
    mkdirSync(bare);
    mkdirSync(brokenCheckout);
    mkdirSync(realCheckout);
    mkdirSync(join(realCheckout, ".git"));
    writeFileSync(join(brokenCheckout, ".gitignore"), "/node_modules\n");

    assert.notEqual(
      imageBuildSkipReasonAt(bare), false,
      "the skip did NOT fire on a tree with no .git and no checkout-only root entry — that is " +
      "the image build, and without the skip the whole `webimage` gate dies on `spawnSync git " +
      "ENOENT` (measured on pristine main, card t_10065a1e)",
    );
    assert.equal(
      imageBuildSkipReasonAt(brokenCheckout), false,
      "the skip fired on a checkout that merely LOST its git metadata (it still has .gitignore). " +
      "Skipping there would leave the lock unpinned over exactly the pruned/renamed-git tree " +
      "this test exists to catch. The .gitignore half of the conjunction is load-bearing.",
    );
    assert.equal(
      imageBuildSkipReasonAt(realCheckout), false,
      "the skip fired on a tree with a .git directory — it must never skip a real checkout",
    );
  } finally {
    rmSync(scratch, { recursive: true, force: true });
  }
});
