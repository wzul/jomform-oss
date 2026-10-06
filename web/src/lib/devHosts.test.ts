// Guard for JOMFORM_DEV_HOSTS and the dev-mode Cache-Control header in
// web/next.config.mjs.
//
// THE DEFECT THESE PIN (measured on the staging stack, not inferred)
//   `next dev` behind a proxy on a real hostname serves its own dev chunks 200
//   but CORS-blocked. Next 16 refuses requests to its internal paths
//   (`/_next/*` — router-utils/block-cross-site-dev.js, `isInternalEndpoint()`)
//   whose Origin/Referer is cross-site and whose host is not allowlisted;
//   `localhost` and `**.localhost` are built in, so this only appears once dev
//   is proxied. Result: React never mounts. Measured on staging —
//   `window.TURBOPACK` empty, `__reactFiber` absent on body, a hydration error
//   overlay on screen, and a click on "Login with password" changing nothing.
//   The app looked broken; the environment was.
//
//   The second half is the cache header. `Cache-Control: public,
//   max-age=31536000, immutable` is correct for a BUILT image (content-hashed
//   names) and wrong for `next dev`, where a chunk keeps ONE name across edits
//   — its hash covers the module GRAPH, not the content. So a CDN in front of
//   dev serves the FIRST copy of any chunk whose import graph did not change:
//   measured on staging, origin served `--jf-switch-off: #64748b` while the
//   edge served `#94a3b8` at the same URL (`cf-cache-status: HIT, age: 1663`),
//   and a newly added i18n key rendered as its own raw key string.
//
// WHAT IS PINNED, AND WHY EACH ONE MATTERS
//   1. A valid host is parsed into `allowedDevOrigins` — the fix is present at
//      all. Asserted on the IMPORTED module, not on source text, so a rewrite
//      of the config's shape cannot leave this passing over a config Next reads
//      as allowlist-empty.
//   2. Unset means empty, NOT a baked-in hostname. A committed literal would
//      have to be edited per environment and kept in sync, which is the drift
//      that made this a staging-only uncommitted patch the first time. It also
//      keeps a plain `npm run dev` on localhost behaving exactly as before.
//   3. A malformed entry THROWS at config load. A typo that quietly disabled
//      the allowlist would bring back the dead-page symptom with nothing in the
//      log to explain it — silent no-op is the failure mode to design against
//      here, so the guard asserts the loud path.
//   4. `NODE_ENV=production` gets the immutable header back, and dev does not.
//      This is the half that keeps a built image — the same hostname can serve
//      one — caching correctly. Asserting only the dev branch would let someone
//      "fix" the header by breaking prod caching.
//
// WHY EACH CASE IS A SEPARATE MODULE EVALUATION
//   The config derives DEV_HOSTS and the header at MODULE SCOPE from
//   process.env, so within one process the first import wins and every later
//   case would silently re-assert the first. Each case therefore imports a
//   distinct URL (`?case=N`), which is a separate ESM cache entry and a fresh
//   evaluation. The module has no imports of its own, so this is safe.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * It cannot start a dev server, so it does not itself observe Next's
//     cross-site block being lifted. That was measured by hand on staging (a
//     real click on a real page); what this file protects against is the fix
//     being reverted or silently neutered.
//   * It does not prove the block is dev-gated. That is Next's own behaviour,
//     verified by reading its call sites (router-server.js: every
//     `blockCrossSiteDEV` call sits inside `if (development)`), not asserted
//     here — asserting a vendored file's internals from this suite would pin
//     Next's private layout and break on every upgrade.
import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync } from "node:fs";
import { fileURLToPath, pathToFileURL } from "node:url";

// A literal read (not a directory literal) because src/lib/buildContext.test.ts
// resolves every `new URL("<lit>", …)` in this suite against web/Dockerfile's
// COPY set, and a directory would read as an uncopied image path.
const NEXT_CONFIG = fileURLToPath(new URL("../../next.config.mjs", import.meta.url));

type NextConfigLike = {
  allowedDevOrigins?: unknown;
  headers?: () => Promise<Array<{ headers: Array<{ key: string; value: string }> }>>;
};

/**
 * Import the config in a FRESH module evaluation, with the given environment
 * applied, and hand it to `fn` BEFORE the environment is restored.
 *
 * The env must stay applied for the whole callback, not just the import: the
 * config computes DEV_HOSTS at module scope, but its `headers()` reads
 * process.env.NODE_ENV when it is CALLED. Restoring the environment right after
 * the import made the dev case assert the production header — a real bug in the
 * first version of this guard, caught by running it.
 *
 * See "WHY EACH CASE IS A SEPARATE MODULE EVALUATION" above: a second plain
 * import of the same specifier returns the first case's result.
 */
let caseId = 0;
async function withConfig<T>(
  env: Record<string, string | undefined>,
  fn: (cfg: NextConfigLike) => T | Promise<T>,
): Promise<T> {
  const saved = new Map<string, string | undefined>();
  for (const [k, v] of Object.entries(env)) {
    saved.set(k, process.env[k]);
    if (v === undefined) delete process.env[k];
    else process.env[k] = v;
  }
  try {
    const url = `${pathToFileURL(NEXT_CONFIG).href}?case=${++caseId}`;
    const cfg = ((await import(url)) as { default: NextConfigLike }).default;
    return await fn(cfg);
  } finally {
    for (const [k, v] of saved) {
      if (v === undefined) delete process.env[k];
      else process.env[k] = v;
    }
  }
}

/** The Cache-Control the config returns for /_next/static/* in this env. */
async function staticCacheHeader(
  env: Record<string, string | undefined>,
): Promise<string | undefined> {
  return withConfig(env, async (cfg) => {
    const headers = cfg.headers?.bind(cfg);
    assert.ok(typeof headers === "function", "web/next.config.mjs no longer exports headers()");
    const rules = await headers!();
    assert.ok(rules.length > 0, "headers() returned no rules — the static-asset header is gone");
    return rules[0].headers.find((h) => h.key === "Cache-Control")?.value;
  });
}

test("the guard is looking at the real config (it cannot pass vacuously)", () => {
  assert.ok(
    existsSync(NEXT_CONFIG),
    "web/next.config.mjs moved — this guard is checking nothing",
  );
});

test("a valid JOMFORM_DEV_HOSTS entry reaches allowedDevOrigins", async () => {
  await withConfig(
    { JOMFORM_DEV_HOSTS: "jomform-staging.example.com" },
    (cfg) =>
      assert.deepEqual(
        cfg.allowedDevOrigins,
        ["jomform-staging.example.com"],
        "web/next.config.mjs must pass JOMFORM_DEV_HOSTS through to allowedDevOrigins. Without " +
          "it, Next 16 blocks its own /_next/* chunks as cross-site whenever dev sits behind a " +
          "proxy on a real hostname (localhost and **.localhost are its built-in defaults), so " +
          "React never mounts and no control on the page responds to a click.",
      ),
  );
});

test("entries are split on commas, trimmed and lower-cased", async () => {
  await withConfig({ JOMFORM_DEV_HOSTS: " A.Example.com , b.example.com " }, (cfg) =>
    assert.deepEqual(
      cfg.allowedDevOrigins,
      ["a.example.com", "b.example.com"],
      "comma-separated hosts must all land, normalised. Next matches on the parsed hostname, " +
        "which is lower-case, so a case-preserving pass-through would allowlist a host that " +
        "never matches a request — the same silent no-op the loud validation exists to prevent.",
    ),
  );
});

test("an unset JOMFORM_DEV_HOSTS means EMPTY, not a baked-in hostname", async () => {
  await withConfig({ JOMFORM_DEV_HOSTS: undefined }, (cfg) =>
    assert.deepEqual(
      cfg.allowedDevOrigins,
      [],
      "with JOMFORM_DEV_HOSTS unset the allowlist must be empty, so a plain `npm run dev` on " +
        "localhost keeps Next's built-in defaults. A committed hostname literal would have to " +
        "be edited per environment and kept in sync — that drift is what made this a " +
        "staging-only uncommitted patch the first time. The value is a property of the " +
        "ENVIRONMENT, not of the tree.",
    ),
  );
});

test("a malformed JOMFORM_DEV_HOSTS entry fails LOUDLY at config load", async () => {
  // Each of these is accepted by a naive `split(",")` and then never matches a
  // request, which is exactly the silent dead-page regression to design against.
  for (const bad of [
    "https://jomform-staging.example.com", // scheme
    "jomform-staging.example.com:443", // port
    "jomform-staging.example.com/x", // path
    "*.example.com", // wildcard
  ]) {
    await assert.rejects(
      () => withConfig({ JOMFORM_DEV_HOSTS: bad }, () => undefined),
      /JOMFORM_DEV_HOSTS entry .* is not a bare hostname/,
      `JOMFORM_DEV_HOSTS=${JSON.stringify(bad)} must throw at config load. A value that is ` +
        "quietly dropped would disable the allowlist and bring back the symptom (page " +
        "renders, nothing responds) with nothing in the log to explain it.",
    );
  }
});

test("dev gets no-store, so an edited chunk cannot be edge-cached", async () => {
  const dev = await staticCacheHeader({ NODE_ENV: "development" });
  assert.equal(
    dev,
    "no-store, must-revalidate",
    "in development the /_next/static/* header must be no-store. Under `next dev` a chunk " +
      "keeps ONE name across edits (its hash covers the module GRAPH, not the content), so " +
      "an immutable header lets a CDN in front of dev serve the previous code indefinitely: " +
      "measured at cf-cache-status HIT on an edited stylesheet, and a new i18n key rendering " +
      "as its own raw key string.",
  );
});

test("production keeps the immutable header", async () => {
  const prod = await staticCacheHeader({ NODE_ENV: "production" });
  assert.equal(
    prod,
    "public, max-age=31536000, immutable",
    "a BUILT image has content-hashed filenames, so /_next/static/* must stay long-lived. " +
      "The same staging hostname can serve a built image, and losing this to fix the dev " +
      "case would trade one caching defect for another. See " +
      "web/src/lib/staticAssetCache.test.ts for the built-image side of this contract.",
  );
});

test("the dev no-store has an explicit opt-out, and it is the only one", async () => {
  const escaped = await staticCacheHeader({
    NODE_ENV: "development",
    JOMFORM_DEV_CACHE: "immutable",
  });
  assert.equal(
    escaped,
    "public, max-age=31536000, immutable",
    "JOMFORM_DEV_CACHE=immutable must restore the immutable header for a dev build that " +
      "wants it. The escape hatch is asserted so it cannot be dropped silently — without it, " +
      "running a built image under NODE_ENV=development would be uncacheable and the only " +
      "way back would be editing next.config.mjs, i.e. the drift this change removes.",
  );
});
