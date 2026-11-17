// Guard for `experimental.agentUpgrade: false` in web/next.config.mjs — the
// switch that keeps Next's agent upgrade REMINDER from red-ing a build that
// succeeded.
//
// THE DEFECT (reproduced on this tree, 2026-11-17)
//   A FRESH `next build` in an agent environment printed the whole 81-line route
//   table, then stopped with
//
//     Error [SecurityFatalError]: Your version of Next.js is affected by a known
//     security vulnerability. … Retry the same command to continue the original
//     task.
//     EXIT=1
//
//   and the same command re-run inside five minutes exited 0. So the `web` gate
//   (`cd web && npm run build`, the first half of which is this suite) reported a
//   red over a build that had already finished, and the only way past it was to
//   pay for a second complete production build.
//
// THE MECHANISM, read out of the pinned package (next 16.4.2)
//   * `next/dist/server/config-shared.js` ships `experimental.agentUpgrade:
//     'security'` in its default config, so the policy is on in EVERY checkout
//     here whether or not the app asks for it.
//   * With that policy `next/dist/build/index.js` requires
//     `next/dist/lib/upgrade/nudge.js`, which asks the npm advisory bulk endpoint
//     and, when `next/dist/telemetry/agent-name.js` (@vercel/detect-agent) sees an
//     AI agent driving the process, throws `SecurityFatalError` (exitCode 1).
//   * The stop is once-per-project-per-five-minutes by design:
//     `allowNudgeRetry()` continues the command only when a receipt under
//     `<app>/.next/cache/next-agentic-upgrade-retries/<sha256(project,version,
//     command,kind)>.json` is younger than `RETRY_TTL = 5 * 60 * 1000`. The
//     receipt lives INSIDE `.next`, so a fresh worktree or a clean build starts
//     without one — which is why the red is intermittent, not constant.
//
// WHAT THIS FILE PINS, AND WHY IT ASKS NEXT'S OWN LOADER
//   Asserting the object web/next.config.mjs exports is not enough: the value
//   that decides whether the build nudges is the one Next's config loader
//   produces AFTER defaults are merged in. `experimental.*` keys are exactly the
//   ones a major release is free to rename or drop, and if `agentUpgrade` stops
//   being honoured the effective value silently falls back to the shipped
//   `'security'` default — every other guard in this suite stays green while the
//   gate goes back to red-ing fresh builds. So the load-bearing test calls
//   `next/dist/server/config` (the same loader `next build` uses) with
//   PHASE_PRODUCTION_BUILD and asserts the EFFECTIVE policy, not the source text.
//
//   The second half is the control: the shipped default is read from
//   `next/dist/server/config-shared` in the same run. While that default is a
//   nudge-enabling policy, `false` in our config is load-bearing and the equality
//   below must hold. If a future Next stops shipping that default, the assertion
//   is reported as inapplicable instead of pinning a value that no longer means
//   anything — and the diagnostic names which case the run is in, so a green here
//   cannot be mistaken for "the nudge is still switched off".
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * It cannot start a build, so it does not observe a nudge being printed. That
//     was measured by hand: a fresh `npx next build` before the config change
//     exited 1 after the full route table, and a fresh one after it exits 0 (see
//     the PR evidence). What this file protects is that the switch stays in
//     place and stays EFFECTIVE.
//   * It says nothing about advisories. Dependency CVEs are the `npmaudit` gate's
//     job (`npm audit --audit-level=high`, web/scripts/npm-audit-gate.mjs), which
//     fails the build instead of stopping it once and continuing — see the
//     rationale block in next.config.mjs for why that is the signal this repo
//     keeps.
import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

// Literal file reads only, and inside web/ — src/lib/buildContext.test.ts checks
// every `new URL("<literal>", …)` in this suite against web/Dockerfile's COPY set.
const NEXT_CONFIG = fileURLToPath(new URL("../../next.config.mjs", import.meta.url));
const WEB_DIR = dirname(NEXT_CONFIG);
/** The module `next build` requires when the policy is a nudge-enabling one. */
const NUDGE_MODULE = "node_modules/next/dist/lib/upgrade/nudge.js";
/** `PHASE_PRODUCTION_BUILD` from next/constants — the phase `next build` loads. */
const PHASE_PRODUCTION_BUILD = "phase-production-build";
/** The only values that make `nudgeUpgrade()` do anything (nudge.js:policy check). */
const NUDGE_POLICIES = ["security", "latest", "experimental-future"];

type ConfigSlice = { experimental?: { agentUpgrade?: unknown } };
type LoadConfig = (phase: string, dir: string) => Promise<ConfigSlice>;

const exported = (await import(pathToFileURL(NEXT_CONFIG).href)).default as
  | ConfigSlice
  | undefined;

/**
 * `require` rather than `import` on purpose: `next/dist/server/config` is a
 * CommonJS build whose default export is the loader (`module.exports.default`),
 * and going through `createRequire` keeps the deep-path resolution out of the
 * TypeScript declaration graph.
 */
const nodeRequire = createRequire(import.meta.url);

function nextShippedDefaultPolicy(): { value: unknown; why: string } {
  try {
    const shared = nodeRequire("next/dist/server/config-shared") as {
      defaultConfig?: ConfigSlice;
    };
    const value = shared.defaultConfig?.experimental?.agentUpgrade;
    return { value, why: "read from next/dist/server/config-shared defaultConfig" };
  } catch (error) {
    return {
      value: undefined,
      why: `could not read next/dist/server/config-shared (${String(error)})`,
    };
  }
}

function nextLoadConfig(): { load: LoadConfig | null; why: string } {
  try {
    const mod = nodeRequire("next/dist/server/config.js") as
      | { default?: LoadConfig }
      | LoadConfig;
    const load =
      typeof mod === "function" ? mod : (mod as { default?: LoadConfig }).default;
    return {
      load: typeof load === "function" ? load : null,
      why: typeof load === "function" ? "next/dist/server/config.js" : "no default export",
    };
  } catch (error) {
    return { load: null, why: `could not require next/dist/server/config.js (${String(error)})` };
  }
}

test("the guard is looking at the shipped config (it cannot pass vacuously)", () => {
  assert.ok(
    existsSync(NEXT_CONFIG),
    "web/next.config.mjs moved — this guard is checking nothing",
  );
  assert.ok(
    exported && typeof exported === "object",
    "web/next.config.mjs no longer default-exports a config object",
  );
  assert.ok(
    exported?.experimental && typeof exported.experimental === "object",
    "web/next.config.mjs no longer exports an `experimental` block, so `agentUpgrade: false` " +
      "cannot be in it — see the rationale block above `const nextConfig`",
  );
});

test("the exported config switches Next's agent upgrade reminder off", () => {
  assert.equal(
    exported?.experimental?.agentUpgrade,
    false,
    "web/next.config.mjs must export `experimental: { agentUpgrade: false }`. With it absent the " +
      "effective policy is Next's shipped default 'security', which stops the FIRST `next build` " +
      "of every fresh worktree with SecurityFatalError (exit 1) after the app has already built — " +
      "the `web` gate then needs a second full build to go green. Next documents `false` as " +
      "'Disables upgrade reminders.' (node_modules/next/dist/docs/01-app/03-api-reference/05-config/" +
      "01-next-config-js/agentUpgrade.md); there is no environment-variable opt-out.",
  );
});

test("the policy Next ACTUALLY resolves for a production build is not a nudge policy", async (t) => {
  const shipped = nextShippedDefaultPolicy();
  const loader = nextLoadConfig();
  assert.ok(
    loader.load,
    `this guard must ask Next's own config loader what it resolves, and it could not: ` +
      `${loader.why}. node_modules/next is missing or moved — seed it with \`cd web && npm ci\`.`,
  );

  const effective = await loader.load(PHASE_PRODUCTION_BUILD, WEB_DIR);
  const effectivePolicy = effective?.experimental?.agentUpgrade;
  const shippedIsNudge = NUDGE_POLICIES.includes(String(shipped.value));

  t.diagnostic(
    `effective experimental.agentUpgrade = ${JSON.stringify(effectivePolicy)}; ` +
      `next's shipped default = ${JSON.stringify(shipped.value)} (${shipped.why}); ` +
      `nudge module present = ${existsSync(`${WEB_DIR}/${NUDGE_MODULE}`)}`,
  );

  if (shippedIsNudge) {
    // The pinned Next still defaults to a nudge-enabling policy, so `false` in
    // next.config.mjs is what keeps the build from stopping. Equality (not just
    // "not a policy") on purpose: it also catches a renamed key, where the
    // effective value would fall back to the shipped default and every
    // source-text assertion in this suite would still read false.
    assert.equal(
      effectivePolicy,
      false,
      `Next resolves experimental.agentUpgrade to ${JSON.stringify(effectivePolicy)} for this app, ` +
        `so the agent upgrade reminder WILL stop a fresh production build with SecurityFatalError ` +
        `(exit 1). The installed Next ships ${JSON.stringify(shipped.value)} as its default and ` +
        `this config is what has to override it; if the key was renamed or dropped upstream, the ` +
        `switch has to move with it (see next.config.mjs), not be deleted.`,
    );
  } else {
    // No nudge default upstream any more: there is nothing left to pin, and a
    // red here would be noise. Reported so a green never reads as "the nudge is
    // still switched off for a reason".
    t.diagnostic(
      "next no longer ships a nudge-enabling experimental.agentUpgrade default — the config key " +
        "and this half of the guard are now inert; remove them together with the rationale block " +
        "in next.config.mjs. The exported value above is still asserted.",
    );
  }
});
