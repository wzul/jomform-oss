// Guard for the files Next.js 16 writes INTO `web/` — they must never dirty the
// tree, and the two mechanisms that keep them out must not be removed.
//
// THE DEFECT (reproduced on this tree, not inferred)
//   `next dev` under an AI-agent environment scaffolds `web/AGENTS.md` AND
//   `web/CLAUDE.md`. Measured: `npm run dev -- --port 3213` with AI_AGENT set
//   added both as untracked files, and the same command with AI_AGENT unset
//   added neither — so the two runs are the control for each other, and the
//   writer is Next's own `next/dist/server/lib/generate-agent-files.js`
//   (`writeAgentFiles`, called from `start-server.js` ->
//   `ensureAgentRulesForDev`). It upserts a managed
//   `<!-- BEGIN:nextjs-agent-rules -->` block and, when neither file exists,
//   scaffolds both.
//
//   A separate run exposed the SECOND file in the same class, and the card's
//   acceptance covers it: `next dev` rewrites the TRACKED `web/next-env.d.ts`,
//   pointing it at `.next/dev/types` while `next build` writes `.next/types`.
//   Tracked, those two commands flip it back and forth forever, so
//   `git status --short` was never clean after `next dev` even once the agent
//   files were gone.
//
// THE DECISION, AND WHY BOTH HALVES ARE PINNED
//   1. `agentRules: false` in web/next.config.mjs — Next's documented opt-out
//      (node_modules/next/dist/docs/01-app/02-guides/ai-agents.md, "Opting out";
//      typed `agentRules?: boolean` on `NextConfig`). The writer's only value
//      here is a pointer to the version-matched Next docs, while this repo's
//      real agent guidance is the ROOT AGENTS.md (preflight gate, MCP + docs
//      parity, sen, urlguard, migration checklist) — scaffolding a nearer
//      `web/AGENTS.md` with none of that in it makes every agent-context
//      loader worse, not better.
//   2. web/.gitignore ignores `/AGENTS.md`, `/CLAUDE.md` and `/next-env.d.ts`,
//      and `next-env.d.ts` was removed from git (`git rm --cached`). Next
//      documents exactly that for the generated type file ("Add it to
//      .gitignore. If your project already tracks the file, remove it from
//      Git" — docs/01-app/03-api-reference/05-config/02-typescript.md), and the
//      precedent is the tsbuildinfo churn (#661).
//
//   Why not just one: the switch is a Next config key that a future major could
//   rename or drop, and the ignore rules are the half that keeps the tree clean
//   when it does. Why not just the ignore rules: they would keep the tree
//   clean while the writer still ran, and the point of the switch is that the
//   scaffold files should not be in a JomForm checkout at all. Each half alone
//   is a weaker guarantee than the one the card asked for, so both are pinned
//   here and neither can be dropped quietly.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
//
// WHAT IS PINNED, AND WHY EACH ONE MATTERS
//   1. The guard is looking at real files (it cannot pass vacuously).
//   2. The next.config.mjs that Next actually loads exports `agentRules: false`
//      — asserted on the IMPORTED module, not on the source text, so a rewrite
//      of the config's shape cannot leave a passing guard over a config Next
//      reads as `true` (the default).
//   3. web/.gitignore carries an anchored rule for each of the three paths.
//      Anchored (`/NAME`) on purpose: a bare `AGENTS.md` would also ignore a
//      nested `AGENTS.md` a component directory legitimately wants to track.
//   4. `next-env.d.ts` is still in web/tsconfig.json's `include`, which is what
//      makes removing it from git safe — Next regenerates it before every type
//      check, and the file must stay part of the program once it is there.
//   5. The three files are NOT TRACKED by git (asked of git, not derived from
//      the ignore rules), and each is actually ignored. This is the half the
//      ignore rules cannot see: a tracked file is never ignored.
//
// THE HOLE THIS FILE USED TO HAVE (reproduced, then closed)
//   The first version asserted the ignore RULES and never ran git, so it could
//   not see the one regression that put the repo in this state: the file being
//   TRACKED again. Measured on a tree at 527c97a with web/node_modules present:
//
//     git add -f web/next-env.d.ts     # one command back to the bug
//     npm run test:unit                # exit 0, 264 pass / 0 fail  <- green
//     git status --short               # A  web/next-env.d.ts       <- dirty
//
//   `web/.gitignore` cannot ignore a file git already tracks, so the rules were
//   green over a tree that had the defect verbatim. Test 5 asks git.
//
// THE SECOND HOLE — the skip was satisfied by any checkout that lost its git
//   Test 5 must skip in exactly one environment: the image build, which has no
//   repository to ask. The first version of that skip accepted `api/go.mod` as
//   proof of "the image build" — but `api/go.mod` ships in EVERY checkout, so
//   the skip also fired on any tree that had merely lost its git metadata.
//   Measured on a pruned linked worktree (`.git` is a file pointing at a gitdir
//   that no longer exists), with `web/` and `api/` intact:
//
//     git worktree add --detach /tmp/probe <head>
//     mv <repo>/.git/worktrees/probe /tmp/moved
//     cd /tmp/probe/web && node --test src/lib/nextManagedFiles.test.ts
//       # tests 5 / pass 5 / fail 0 / skipped 0     <- GREEN
//     git ls-files web/next-env.d.ts                # still tracked
//
//   i.e. green over the exact dirty tree this guard exists to catch — the same
//   shape as the first hole, one level down. The fix is to assert the skip's
//   PREMISE positively instead of by a proxy that every checkout satisfies:
//   the image build is the environment with no `.git` AND no repo root, and
//   `IN_IMAGE_BUILD`/`CHECKOUT_ONLY_ROOT_FILES` below are that assertion. A
//   tree that still has `web/` and `api/` but no working git is therefore a
//   FAILURE, not a skip.
//
//   `web/scripts/biteproof_next_managed_files.sh` asserts the same premise
//   (case 0 refuses to run when the skip would fire) so the two cannot drift.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * It cannot start a dev server, so it does not observe the scaffold being
//     suppressed. That was measured by hand (the AI_AGENT set/unset control
//     above); what this file protects is that neither mechanism gets reverted.
//   * Test 5 runs git and therefore has exactly ONE skip, and it is the one
//     environment with no repository to ask. Asserting there would turn the
//     image build red over files it cannot track.
import { test } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

// Literal file reads only — src/lib/buildContext.test.ts resolves every
// `new URL("<lit>", …)` in this suite against web/Dockerfile's COPY set, so a
// directory literal (e.g. `new URL("../../")`) reads as an uncopied image path
// and turns that guard red. Directories are derived from a file, below.
const NEXT_CONFIG = fileURLToPath(new URL("../../next.config.mjs", import.meta.url));
const GITIGNORE = fileURLToPath(new URL("../../.gitignore", import.meta.url));
const TSCONFIG = fileURLToPath(new URL("../../tsconfig.json", import.meta.url));
const WEB_DIR = dirname(GITIGNORE);
/** The repo root — `..` from web/. Present in a checkout, absent in an image. */
const REPO_ROOT = dirname(WEB_DIR);

/** The files Next writes into web/ itself, and which must stay out of git. */
const NEXT_MANAGED = ["/AGENTS.md", "/CLAUDE.md", "/next-env.d.ts"];
/**
 * The same three as bare names, for git. A pathspec is ALWAYS relative to the
 * process cwd — the helper below runs git with cwd = web/ — so `web/next-env.d.ts`
 * would be read as `web/web/next-env.d.ts` from there and match nothing.
 */
const NEXT_MANAGED_PATHS = NEXT_MANAGED.map((rule) => rule.slice(1));
/**
 * Repo-root entries that exist in EVERY checkout and in NO image.
 *
 * Measured at this commit, against web/Dockerfile's COPY set and the root
 * `.dockerignore` (both read before this list was written):
 *
 *   * The web image builds from the REPO ROOT (`docker build -f web/Dockerfile .`)
 *     but its COPY set is only `web/package.json`, `web/package-lock.json`,
 *     `web/`, `api/` and `MCP-PARITY.md`. It never copies the root directory
 *     itself, so the image's `/repo` is exactly {web, api, MCP-PARITY.md} — no
 *     root entry of any kind lands there.
 *   * `Makefile` and `AGENTS.md` are additionally listed in the root
 *     `.dockerignore` (lines 29-30), so they are not even uploaded into the
 *     build context. `.gitignore` is NOT in `.dockerignore`, which is why it is
 *     the load-bearing marker: its absence from the image cannot be explained
 *     by an ignore rule, only by the image simply having no repo root. A future
 *     `.dockerignore` edit cannot make this assertion pass on a checkout.
 *   * The card that asked for this fix suggested `../package.json` for this
 *     list. There is no root `package.json` in this repo (the npm manifests are
 *     `web/package.json` and `api/...`), so that hint was wrong and is replaced
 *     by the three paths below, each verified present at the worktree root and
 *     absent from `/repo` in the built image (the image `ls` is in the PR
 *     evidence).
 *
 * A conjunction on purpose: any one of these present means "this tree has a
 * repo root", and the skip must not fire.
 */
const CHECKOUT_ONLY_ROOT_ENTRIES = [".gitignore", "Makefile", "AGENTS.md"];

/**
 * One git subprocess with cwd pinned to web/, so the pathspecs below are the
 * bare file names and the answer does not depend on where the suite was invoked
 * from. (`cwd` is set here rather than relied on: `node --test src/lib/*.test.ts`
 * happens to run with cwd = web/ through the npm script, but running the file
 * directly from the repo root must give the same answer — verified both ways.)
 *
 * `shell: false` and an argument array on purpose: nothing here is
 * shell-interpolated, and `status` is null exactly when git never ran (no
 * binary) — which is one of the two cases test 5's skip covers.
 *
 * `timeout` is not decoration. This call runs at MODULE SCOPE inside a preflight
 * gate, so a git that blocks — a stale `.git/index.lock`, a credential prompt,
 * a wedged network mount — would hang the whole unit suite with no diagnostic.
 * A killed child comes back with `status: null` AND `error.code === "ETIMEDOUT"`,
 * which is why the caller checks `timedOut` BEFORE treating a non-zero result
 * as "no work tree": a timeout is not a repository-free environment and must
 * fail rather than silently skip the tracked check.
 */
const GIT_TIMEOUT_MS = 10_000;
function git(args: string[]): {
  status: number | null;
  stdout: string;
  stderr: string;
  timedOut: boolean;
} {
  const r = spawnSync("git", args, {
    cwd: WEB_DIR,
    encoding: "utf8",
    timeout: GIT_TIMEOUT_MS,
  });
  const timedOut =
    (r.error as NodeJS.ErrnoException | undefined)?.code === "ETIMEDOUT";
  return { status: r.status, stdout: r.stdout ?? "", stderr: r.stderr ?? "", timedOut };
}

/**
 * True only for the web image build — the one environment where test 5 must
 * skip, because it has no repository to ask.
 *
 * Asserted POSITIVELY (no `.git` AND the repo root gone), because a proxy that
 * every checkout also satisfies is how the skip went green over a checkout with
 * broken git metadata. `api/go.mod`, `web/` and `api/` are all useless here:
 * they ship in every checkout. See CHECKOUT_ONLY_ROOT_FILES for why the root
 * files identify an image and not a checkout.
 */
function imageBuildEvidence(): { isImage: boolean; reason: string } {
  const gitMetadata = existsSync(join(REPO_ROOT, ".git"));
  const rootEntries = CHECKOUT_ONLY_ROOT_ENTRIES.filter((e) => existsSync(join(REPO_ROOT, e)));
  return {
    isImage: !gitMetadata && rootEntries.length === 0,
    reason:
      `${REPO_ROOT}: .git ${gitMetadata ? "present" : "absent"}; ` +
      `checkout-only root entries present: [${rootEntries.join(", ")}]`,
  };
}

type NextConfigLike = { agentRules?: unknown };
const config = (await import(pathToFileURL(NEXT_CONFIG).href)).default as
  | NextConfigLike
  | undefined;
const ignoreRules = readFileSync(GITIGNORE, "utf8")
  .split(/\r?\n/)
  .map((l) => l.trim())
  .filter((l) => l !== "" && !l.startsWith("#"));

test("the guard is looking at real files (it cannot pass vacuously)", () => {
  assert.ok(existsSync(NEXT_CONFIG), "web/next.config.mjs moved — this guard is checking nothing");
  assert.ok(
    existsSync(GITIGNORE),
    "web/.gitignore is missing; the ignore half of the Next-managed-files fix is gone",
  );
  assert.ok(
    config && typeof config === "object",
    "web/next.config.mjs no longer default-exports a config object",
  );
  assert.ok(
    ignoreRules.length > 0,
    "web/.gitignore parsed to zero rules — the reading below would pass on an empty file",
  );
  assert.ok(
    ignoreRules.includes("/next-env.d.ts"),
    "web/.gitignore lost the /next-env.d.ts rule (or it is no longer a plain line)",
  );
});

test("Next's agent-file writer is switched off in the config Next loads", () => {
  assert.equal(
    config?.agentRules,
    false,
    "web/next.config.mjs must export `agentRules: false`. With it unset (or true) Next 16 " +
      "regenerates web/AGENTS.md + web/CLAUDE.md inside any agent environment — see " +
      "node_modules/next/dist/docs/01-app/02-guides/ai-agents.md ('Opting out'). This repo's " +
      "agent guidance is the ROOT AGENTS.md; do not re-enable the scaffold.",
  );
});

test("every file Next writes into web/ is ignored by web/.gitignore", () => {
  const missing = NEXT_MANAGED.filter((rule) => !ignoreRules.includes(rule));
  assert.deepEqual(
    missing,
    [],
    `web/.gitignore no longer ignores ${missing.join(", ")}. These are regenerated by ` +
      "`next dev` / `next build`, so without the rules `git status --short` is dirty before a " +
      "worker has written a line of code — the tsconfig.tsbuildinfo churn (#661) again. Note " +
      "the leading slash: a bare name would also ignore a nested file of the same name.",
  );
});

test("next-env.d.ts stays part of the TypeScript program it is generated into", () => {
  const tsconfig = JSON.parse(readFileSync(TSCONFIG, "utf8")) as { include?: unknown };
  assert.ok(
    Array.isArray(tsconfig.include) && tsconfig.include.includes("next-env.d.ts"),
    "web/tsconfig.json must keep \"next-env.d.ts\" in `include`. The file is no longer tracked " +
      "in git, so this entry is what guarantees Next's generated type references are compiled " +
      "once it regenerates them before the type check.",
  );
  // The file itself is an output: it is expected to be absent in a fresh
  // checkout / image and written by the build, so its presence here is NOT
  // asserted. (Verified by hand: a full `npm run build` with the file absent
  // exits 0 and regenerates it.)
  const writerStillShips = existsSync(
    `${WEB_DIR}/node_modules/next/dist/server/lib/generate-agent-files.js`,
  );
  assert.ok(
    !writerStillShips || config?.agentRules === false,
    "the installed Next still ships the agent-file writer and the config does not disable it",
  );
});

test("git does not track the files Next regenerates (a re-track is the whole regression)", (t) => {
  const inside = git(["rev-parse", "--is-inside-work-tree"]);
  if (inside.status !== 0 || inside.stdout.trim() !== "true") {
    // A wedged git is NOT "no repository". Checked first, so a timeout cannot
    // reach the skip below and quietly drop the tracked check this test exists
    // to run over a dirty tree.
    assert.ok(
      !inside.timedOut,
      `\`git rev-parse --is-inside-work-tree\` did not answer within ${GIT_TIMEOUT_MS} ms in ` +
        `${WEB_DIR}, so the tracked check below cannot run and this suite cannot tell a clean ` +
        "tree from a re-tracked one. Look for a stale .git/index.lock, a git prompting for " +
        "credentials, or a wedged mount — this suite is part of the preflight gate and " +
        "must not hang.",
    );
    // THE ONE LEGITIMATE SKIP, and it is asserted to be what it claims: the web
    // IMAGE build, whose context has no `.git` (root-context build, the root
    // `.dockerignore` drops it, and node:24-alpine ships no git). Test 3 — the
    // ignore rules — is the half that still holds there.
    //
    // `api/go.mod` is deliberately NOT the evidence: it ships in every
    // checkout, so it also matched a checkout that had merely lost its git
    // metadata, which is how this skip used to hide the very regression the
    // test is for. The premise is asserted positively instead.
    const evidence = imageBuildEvidence();
    assert.ok(
      evidence.isImage,
      "git says this is not a work tree, but this does not look like the image build either: " +
        `${evidence.reason}. That is a checkout whose git metadata is missing or broken — a ` +
        "pruned linked worktree, a renamed .git, a git missing from PATH — NOT the Docker build " +
        "context, and skipping here would leave the tracked check green over exactly the dirty " +
        "tree it exists to catch. Restore the repository (`git worktree repair`) and re-run.",
    );
    t.diagnostic(
      "git: no work tree at web/ (the Docker image build drops .git and ships no git) — " +
        "skipping the tracked check; the .gitignore rules asserted above are the guarantee in " +
        "this environment",
    );
    return;
  }

  const tracked = git(["ls-files", "-z", "--", ...NEXT_MANAGED_PATHS]);
  assert.equal(
    tracked.status,
    0,
    `\`git ls-files\` failed inside a work tree (exit ${tracked.status}): ${tracked.stderr.trim()}`,
  );
  const offenders = tracked.stdout.split("\0").filter((p) => p !== "");
  assert.deepEqual(
    offenders,
    [],
    `git TRACKS ${offenders.join(", ")}, so ${offenders.length > 1 ? "they" : "it"} can never be ` +
      "ignored again — the ignore rules pass while `next dev` / `next build` rewrite the tracked " +
      "blob on every run, which is the dirty-tree defect this fix removed. Un-track it with " +
      "`git rm --cached <path>` (a `web/.gitignore` rule cannot ignore a tracked file), keeping " +
      "the file on disk if you like: Next regenerates all three before the type check. Note that " +
      "`git add -f` on any of these paths restores the defect verbatim.",
  );

  // The second half of the same defect, asked of git as well: an untracked path
  // that the rules do NOT match returns exit 1, and check-ignore answers from
  // the rules without touching the working tree — so a *present* regenerated
  // file is judged by the rules, not by a stale on-disk copy.
  const notIgnored: string[] = [];
  for (const path of NEXT_MANAGED_PATHS) {
    if (git(["check-ignore", "--quiet", "--", path]).status !== 0) notIgnored.push(path);
  }
  assert.deepEqual(
    notIgnored,
    [],
    `git does not ignore ${notIgnored.join(", ")} even though web/.gitignore lists the rule — the ` +
      "rules are being read from the wrong file, or were unanchored/negated (`!` prefix) somewhere " +
      "else in the ignore chain. `git check-ignore -v <path>` names the deciding file.",
  );
});
