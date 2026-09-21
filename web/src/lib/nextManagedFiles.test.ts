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
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * It does not run git, so it does not itself observe that `next-env.d.ts`
//     is untracked or that a path is ignored; it asserts the ignore RULES. The
//     `git check-ignore -v` output and the two `git status --short` runs (before
//     and after the fix) are in the PR evidence.
//   * It cannot start a dev server, so it does not observe the scaffold being
//     suppressed. That was measured by hand (the AI_AGENT set/unset control
//     above); what this file protects is that neither mechanism gets reverted.
import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { dirname } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

// Literal file reads only — src/lib/buildContext.test.ts resolves every
// `new URL("<lit>", …)` in this suite against web/Dockerfile's COPY set, so a
// directory literal (e.g. `new URL("../../")`) reads as an uncopied image path
// and turns that guard red. Directories are derived from a file, below.
const NEXT_CONFIG = fileURLToPath(new URL("../../next.config.mjs", import.meta.url));
const GITIGNORE = fileURLToPath(new URL("../../.gitignore", import.meta.url));
const TSCONFIG = fileURLToPath(new URL("../../tsconfig.json", import.meta.url));
const WEB_DIR = dirname(GITIGNORE);

/** The files Next writes into web/ itself, and which must stay out of git. */
const NEXT_MANAGED = ["/AGENTS.md", "/CLAUDE.md", "/next-env.d.ts"];

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
