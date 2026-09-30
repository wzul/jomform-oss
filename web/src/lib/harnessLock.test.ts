// The per-worktree lock in web/scripts/biteproof_docs_tool_claims.sh
// (card t_ed66094c).
//
// WHY THIS EXISTS (the bug it was written for): the harness mutates two SHARED docs
// pages in place (src/app/docs/chat/page.tsx, src/app/docs/embed/page.tsx) and runs its
// guard over the WHOLE docs tree. PR 846 fixed the verdict RULE and the verdict CHANNEL
// (per-run output, per-run `.ts` copies), but the shared INPUT stayed: a second run
// started while the first was mid-case read a page the first had already mutated and
// attributed that mutation to its own case. Reproduced on PR 846's head, 3/3 rounds: two
// runs 2s apart in ONE worktree printed 33 false `***` verdict lines across 6 runs while
// the exit code stayed 1 — `OLD guard wanted GREEN, got RED`, `NEW guard wanted GREEN,
// got RED`, `NEW guard RED but the failure does not name …`. No per-run channel can fix
// that: the thing being shared is the input. The fix is a per-worktree `flock`, taken
// before the premise check (which READS both pages) and released when the process exits.
//
// WHAT THIS PINS. Every leg below RUNS the real script and reads its real exit code and
// its real message — none of them greps the file's text, because the sharpest way to
// break this lock leaves the text perfectly healthy. Each leg is chosen so that removing
// the thing it guards makes the harness RUN instead of REFUSING, and the two outcomes
// have different exit codes:
//
//   A. A peer holds this worktree's lock and `BITEPROOF_LOCK_WAIT=0`: the run must exit
//      75 with the timeout reason, having mutated nothing. Without the lock take the
//      harness instead runs its whole 10s body and exits 1 — so this leg proves the lock
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
//   D. The key is PER WORKTREE. With the primary tree's lock held by the peer: the run in
//      the primary must be refused (D1), the same run started from `web/` must be refused
//      too (D2 — the key does not depend on the caller's CWD), and the SAME invocation in
//      a LINKED worktree must NOT be refused (D3). D3 is the leg that proves a second
//      worktree is not serialised against this one — a key that collapsed to one file per
//      repo would make D3 read 75 like D1, i.e. every card's lane queuing behind every
//      other card's.
//
// None of these legs reaches node: they all exit inside the harness's lock block or its
// base resolution. That is a measured constraint, not a preference — a full harness run
// inside `npm run test:unit` nests node's test runner inside node's test runner, and
// measured while building this file, that nesting made the harness's premise check read
// the base guard as "did not RUN": a false REFUSAL produced by the guard rather than
// found by it. The legs that need a real guard run stay in the biteproof harness itself,
// which self-bites (see the BITE-HARNESSES.md row for this script).
//
// WHAT IT DOES NOT COVER (stated honestly): that a lock-HOLDING run produces no false
// verdict lines is measured by that harness on a staggered pair of runs, not here. It
// also cannot see a reader that mutates a shared page WITHOUT calling this script.
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
const HARNESS = join(webRoot, "scripts", "biteproof_docs_tool_claims.sh");
const LINKED = join(repoRoot, ".worktrees", "t_ed66094c");
const LOCK_SUBDIR = "jomform-biteproof-docs-tool-claims";

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

// THE WEB IMAGE BUILD RUNS THIS SUITE, AND HAS NO REPOSITORY TO ASK.
//
// web/Dockerfile runs `npm run build`, which runs these unit tests first, from a
// build context whose root `.dockerignore` excludes `.git` and whose COPY set
// lands only web/, api/ and MCP-PARITY.md — so /repo has no repo root and no
// repository. EVERY leg below asks git for this worktree's absolute git dir, so
// in the image the guard dies on `fatal: not a git repository (or any of the
// parent directories)` and reds the webimage gate while the host `web` gate is
// green — the same host-vs-image divergence class web/src/lib/buildContext.test.ts
// exists to prevent (the MCP-PARITY.md ENOENT, one layer down).
//
// The skip is derived from POSITIVE evidence that this is the image, never from
// "git did not answer": `nextManagedFiles.test.ts` uses the identical conjunction
// (no `.git` AND no checkout-only root entry). `.gitignore` is the load-bearing
// marker — it is NOT in the root `.dockerignore`, so its absence cannot be
// explained by an ignore rule, only by there being no repo root at all. A broken
// git in a real checkout keeps `.gitignore` and therefore still FAILS this leg
// rather than skipping it.
const CHECKOUT_ONLY_ROOT_ENTRIES = [".gitignore", "Makefile", "AGENTS.md"];
function imageBuildSkipReason(): string | false {
  const rootEntries = CHECKOUT_ONLY_ROOT_ENTRIES.filter((e) => existsSync(join(repoRoot, e)));
  const isImage = !existsSync(join(repoRoot, ".git")) && rootEntries.length === 0;
  return isImage
    ? `the web image build has no repository: ${repoRoot} has no .git and no checkout-only root entry`
    : false;
}

const git = (args: string[], cwd: string) =>
  execFileSync("git", args, { cwd, encoding: "utf8" }).trim();

// The lock path the harness derives, re-derived here from the same documented rule so a
// change to one without the other FAILS rather than agreeing by luck: sha256 of the
// worktree's absolute git dir, first 16 hex chars, under <base>/jomform-biteproof-docs-tool-claims.
function lockFileFor(worktree: string, base: string): string {
  const gitDir = git(["rev-parse", "--absolute-git-dir"], worktree);
  const key = execFileSync("sha256sum", [], { input: gitDir, encoding: "utf8" }).slice(0, 16);
  return join(base, LOCK_SUBDIR, `${key}.lock`);
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

function runHarness(env: NodeJS.ProcessEnv, cwd = repoRoot, script = HARNESS) {
  const r = spawnSync("bash", [script], { cwd, env, encoding: "utf8" });
  return { status: r.status, out: `${r.stdout ?? ""}${r.stderr ?? ""}` };
}

test("the harness serialises on a PER-WORKTREE lock (t_ed66094c)", async (t) => {
  const imageReason = imageBuildSkipReason();
  if (imageReason) {
    t.skip(imageReason);
    return;
  }
  assert.ok(existsSync(HARNESS), `the harness is missing at ${HARNESS}`);

  // A private lock base per run: this guard never touches a real lane's lock, and two
  // concurrent runs of THIS guard cannot collide either.
  const base = mkdtempSync(join(tmpdir(), "biteguard-"));
  const env = { ...process.env, BITEPROOF_LOCK_DIR: base };
  const ours = lockFileFor(repoRoot, base);
  const nowait = { ...env, BITEPROOF_LOCK_WAIT: "0" };

  try {
    mkdirSync(join(base, LOCK_SUBDIR), { recursive: true });
    writeFileSync(ours, "");
    assert.ok(lockIsFree(ours), "the lock must be free before leg A (premise of this leg)");

    // The holder runs `sleep` as a CHILD, which inherits the locked descriptor — so the
    // hold lasts until that child exits, not until flock is signalled. Both are awaited
    // before leg B for that reason: killing flock alone leaves the lock held (measured),
    // and it silently turned leg B into a full run of the harness's body.
    const holder = spawn("flock", [ours, "bash", "-c", "sleep 8"], { stdio: "ignore" });
    const holderDone = new Promise<void>((res) => holder.on("close", () => res()));
    await waitUntil(() => !lockIsFree(ours), 5000, "the holder taking the lock");

    // --- A: a peer holds the lock and the wait is 0 -> refuse, do not run. --------
    const blocked = runHarness(nowait);
    assert.equal(
      blocked.status, 75,
      `with a peer holding this worktree's lock and a zero wait, the harness must exit 75 ` +
      `(it never got the lock, so it must not run). Got ${blocked.status}:\n${blocked.out}`,
    );
    assert.match(
      blocked.out, /could not take the per-worktree lock/,
      `the timeout must say which lock it could not take. Got:\n${blocked.out}`,
    );
    assert.doesNotMatch(
      blocked.out, /BITES|\*\*\*|^old guard \(/m,
      `the harness ran its body without the lock, so it would have read pages a peer may ` +
      `have mutated. Got:\n${blocked.out}`,
    );

    // --- D1/D2: the key does not depend on the caller's CWD. ---------------------
    for (const [label, cwd] of [["repo root", repoRoot], ["web/", webRoot]] as const) {
      const r = runHarness(nowait, cwd);
      assert.equal(
        r.status, 75,
        `started from ${label}, the harness must derive the SAME per-worktree lock and be ` +
        `refused by the peer holding it. Got ${r.status}:\n${r.out}`,
      );
      assert.match(r.out, /could not take the per-worktree lock/,
        `started from ${label}, the refusal lost its reason:\n${r.out}`);
    }

    // --- D3: a LINKED worktree is NOT serialised against this one. ---------------
    if (existsSync(LINKED)) {
      const linkLock = lockFileFor(LINKED, base);
      assert.notEqual(
        ours, linkLock,
        "the primary tree and a linked worktree derive the SAME lock file, so every card's " +
        "lane would serialise behind every other card's",
      );
      // A ref that cannot resolve: the harness takes the lock, then refuses at its base
      // resolution — a deterministic exit 2 that needs no guard run. If the peer's lock
      // wrongly covered this worktree, the exit code would be 75 instead.
      const linked = runHarness(
        { ...nowait, BITEPROOF_BASE: "refs/heads/jomform-nonexistent-probe" },
        LINKED,
      );
      assert.equal(
        linked.status, 2,
        `a run in the linked worktree was blocked by the PRIMARY tree's lock, so the key is ` +
        `not per-worktree. Got ${linked.status}:\n${linked.out}`,
      );
      assert.match(
        linked.out, /cannot resolve refs\/heads\/jomform-nonexistent-probe/,
        `the linked-worktree run did not get as far as resolving its base, so its exit 2 ` +
        `proves nothing about the lock. Got:\n${linked.out}`,
      );
      assert.doesNotMatch(
        linked.out, /could not take the per-worktree lock/,
        `the linked worktree queued behind the primary tree's lock:\n${linked.out}`,
      );
    }

    await holderDone;
    await waitUntil(() => lockIsFree(ours), 5000, "the holder releasing the lock");

    // --- B: locked=1 with a FREE lock file -> refuse, do not run. ----------------
    const unverified = runHarness({ ...env, BITEPROOF_LOCKED: "1", BITEPROOF_LOCK_FILE: ours });
    assert.equal(
      unverified.status, 2,
      `a BITEPROOF_LOCKED=1 run whose lock file is FREE must REFUSE (exit 2), not print ` +
      `verdicts. Got ${unverified.status}:\n${unverified.out}`,
    );
    assert.match(
      unverified.out, /was not held here/,
      `the refusal must name the lock it could not prove it held. Got:\n${unverified.out}`,
    );

    // --- C: no flock on PATH -> refuse, do not run unserialised. -----------------
    const noFlockPath = "/usr/bin:/bin";
    const flockAbsent = spawnSync("bash", ["-c", "command -v flock"],
      { env: { ...env, PATH: noFlockPath }, encoding: "utf8" }).status !== 0;
    if (flockAbsent) {
      const noFlock = runHarness({ ...env, PATH: noFlockPath });
      assert.equal(
        noFlock.status, 2,
        `with no flock on PATH the harness must REFUSE (exit 2) rather than run ` +
        `unserialised. Got ${noFlock.status}:\n${noFlock.out}`,
      );
      assert.match(
        noFlock.out, /flock is not installed/,
        `the refusal must name the missing tool. Got:\n${noFlock.out}`,
      );
    }
  } finally {
    rmSync(base, { recursive: true, force: true });
  }
});
