// Structural net for card t_3c0cc8ce: the Settings → Domains panel must not
// paint either Cloudflare STATUS ANSWER before `/api/cf/status` has answered.
//
// THE DEFECT
//   `cfLinked` and `cfConfigured` both start at `false` — the NEGATIVE answer —
//   and the panel rendered straight off them. So every cold load of the Domains
//   tab painted, for the fetch's duration, "Cloudflare auto-setup is not
//   configured on this deployment — add the CNAME record manually." on a
//   deployment where it IS configured, plus a "Log in with Cloudflare" link for
//   an account that was already linked. Measured by QA on prod v1.21.252:
//   t=661ms the false sentence, t=1019ms "Cloudflare connected ✓".
//
// WHY THIS FILE EXISTS AT ALL, GIVEN THE BROWSER SPEC
//   The behavioural proof is `web/e2e/cf-status-prefetch.spec.ts`, which holds
//   `/api/cf/status` open and reads the live DOM (the repo has no JSX/DOM unit
//   runner — see `src/lib/i18n.test.ts` and `src/lib/spamJudgeToggle.test.ts`).
//   That spec runs in the `e2e` gate, which needs a production build; this file
//   runs in the `web` gate's unit half in seconds and pins the three SHAPES the
//   fix has to keep. Neither replaces the other: a source scan cannot see what
//   is painted, and the browser spec cannot tell you which line came back.
//
// WHAT IT PINS
//   1. The flag exists and starts `false` — `true` would suppress the honest
//      negative answer on the every-load-on-a-deployment-with-no-client-id case.
//   2. `loadCfStatus()` sets it on BOTH paths. A flag set only where the JSON
//      parsed leaves a failed request with no answer and no note, i.e. a panel
//      that never settles (the third arm of the browser spec).
//   3. No status-derived branch is rendered without the flag: the negative
//      sentence, the login link, and the manual-token toggle. The badge and the
//      Disconnect button are keyed on `cfLinked` alone, which is only ever `true`
//      once the fetch has set it, so they cannot paint early.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const settings = read("../app/settings/page.tsx");

test("the guard is looking at the real settings page (it cannot pass vacuously)", () => {
  assert.ok(
    settings.includes("set.cfNotConfigured"),
    "app/settings/page.tsx no longer mentions set.cfNotConfigured — the guard's anchor moved",
  );
  assert.ok(
    settings.includes("const loadCfStatus = useCallback("),
    "app/settings/page.tsx no longer declares loadCfStatus — the guard's anchor moved",
  );
});

test("the answered-flag starts false and the loader settles it on BOTH paths", () => {
  // `true` would assert an answer before there is one in the other direction:
  // it would hide the honest "not configured" note forever.
  assert.match(
    settings,
    /const \[cfStatusLoaded, setCfStatusLoaded\] = useState\(false\)/,
    "cfStatusLoaded must exist and start false — it means \"the fetch answered\", not \"the panel is ready\"",
  );

  const start = settings.indexOf("const loadCfStatus = useCallback(");
  assert.ok(start >= 0, "loadCfStatus is gone");
  // The body ends at the useCallback's dependency array, whatever it names —
  // card t_88485157 re-keyed the loader on the active workspace, and a
  // delimiter hard-coded to `[]` would silently slice PAST the function and
  // assert against unrelated code (which is how it broke).
  const tail = /\n {2}\}, \[[^\]]*\]\);/.exec(settings.slice(start));
  assert.ok(tail, "could not delimit loadCfStatus's body (no `}, […]` dependency array)");
  const body = settings.slice(start, start + tail.index + tail[0].length);

  // A `.finally` runs on resolution, rejection and a non-2xx answer alike, which
  // is the shape that covers every way the request can end. `then`/`catch`
  // spellings are accepted too, as long as the setter appears on both paths.
  const viaFinally = /\.finally\(/.test(body) && /setCfStatusLoaded\(true\)/.test(body.slice(body.indexOf(".finally(")));
  const setters = body.match(/setCfStatusLoaded\(true\)/g) ?? [];
  assert.ok(
    viaFinally || setters.length >= 2,
    "loadCfStatus() does not set cfStatusLoaded on BOTH paths: a fetch that rejects (or answers non-2xx) " +
      "would leave the Domains panel with no note and no answer at all — the state the browser spec's " +
      "failed-fetch arm pins (card t_3c0cc8ce)",
  );
  // …and it is not set where only a parsed body reaches it.
  assert.ok(
    !/if \(d\) \{[^}]*setCfStatusLoaded\(true\)/.test(body),
    "cfStatusLoaded is set inside the `if (d)` branch — a failed or non-2xx fetch would never settle the panel",
  );
});

test("no status-derived branch renders before the answer", () => {
  // The three branches that read a status state whose default is the negative
  // answer. Each must carry the flag; the badge/Disconnect pair is keyed on
  // `cfLinked`, which is false until the fetch sets it, so it cannot paint early
  // (asserted below as the reason it is deliberately absent from this list).
  const branches: Array<{ name: string; pattern: RegExp }> = [
    { name: "the negative answer (set.cfNotConfigured)", pattern: /\{cfStatusLoaded && !cfConfigured && \(/ },
    // The href is held in state (card t_88485157) rather than written inline, so
    // the anchor is named by its binding. The `eslint-disable-next-line` note this
    // used to pin was prose that moved; what must hold is the GATE, then the anchor.
    { name: "the login link (/api/cf/start)", pattern: /\{cfStatusLoaded && !cfLinked && \(\s*<a href=\{cfStartHref\}/ },
    { name: "the manual-token toggle", pattern: /\{cfStatusLoaded && !cfLinked && \(\s*<button\s*\n\s*onClick=\{\(\) => \{ setCfManualOpen/ },
  ];
  for (const { name, pattern } of branches) {
    assert.match(
      settings,
      pattern,
      `${name} is not gated on cfStatusLoaded — it paints an answer the deployment may not deserve before ` +
        `the status fetch (/api/cf/status) resolves (card t_3c0cc8ce)`,
    );
  }

  // The unguarded shapes this replaces must be gone. A second copy of the
  // sentence with its own `!cfConfigured` gate would re-open the same window
  // even with the first one fixed.
  const unguardedNote = [...settings.matchAll(/\{!cfConfigured && \(/g)].length;
  assert.equal(unguardedNote, 0, "app/settings/page.tsx still renders {!cfConfigured && (…) without cfStatusLoaded");
  const unguardedLinks = [...settings.matchAll(/\{!cfLinked && \(/g)].length;
  assert.equal(
    unguardedLinks,
    0,
    "app/settings/page.tsx still renders {!cfLinked && (…) without cfStatusLoaded — the unlinked branch offers " +
      "\"Log in with Cloudflare\" to an account whose link state is still unknown",
  );
});
