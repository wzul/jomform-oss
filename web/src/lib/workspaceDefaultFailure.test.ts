// The header switcher's "Make this the default workspace" / "Remove default
// workspace" rows must never fail SILENTLY (card t_f6de6395).
//
// THE DEFECT (measured live on a `next build` of the pre-fix tree, API stubbed at
// the browser level to answer 400): clicking the row sent exactly one
// `PUT /api/auth/profile/default-workspace`, the menu closed
// (`aria-expanded="false"`), and then NOTHING — `document.querySelectorAll(
// '[role="alert"]').length === 0`, no failure text anywhere on the page, and the
// switcher still offered the same action. `Shell.setDefaultWorkspace()` never
// read the response status, its `catch {}` swallowed transport failures, and it
// called `reload()` unconditionally, so a 400/403/500 and a dead network were
// indistinguishable from "the click did nothing". The dictionary already carried
// the two sentences (`workspace.setDefaultFail`, `workspace.clearDefaultFail`)
// and no surface rendered them.
//
// This file pins the SHAPE, because a component handler cannot be rendered by
// this repo's unit runner (node --test, no JSX/DOM setup) — the rendered
// behaviour is driven for real by `web/e2e/workspace-default-refusal.spec.ts`,
// which refuses both directions (and drops the request at the transport level)
// on the built page and reads the live DOM. Each assertion here is the cheapest
// thing that can go wrong the moment the fix is undone:
//
//   1. the write goes through the SHARED `fetchOrThrow` (lib/wsfetch.ts) and its
//      outcome is READ — no bare `await fetch(`, no `catch {}` with no binding;
//   2. the refusal returns the direction's own sentence and returns BEFORE
//      `reload()`, so only a successful write re-reads the bundle;
//   3. the sentence comes from the dictionary (`workspace.setDefaultFail` /
//      `workspace.clearDefaultFail`), selected by direction — never typed prose;
//   4. the switcher renders it in a live region OUTSIDE the dropdown panel,
//      because the panel has already closed by the time the answer lands;
//   5. no status check is hand-rolled into a /login bounce.
//
// Run with the repo's Node test runner:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const shell = readFileSync(
  fileURLToPath(new URL("../components/Shell.tsx", import.meta.url)),
  "utf8",
);
const switcher = readFileSync(
  fileURLToPath(new URL("../components/WorkspaceSwitcher.tsx", import.meta.url)),
  "utf8",
);

/** A slice anchored by INDEX, the way builderSaveFailure.test.ts does it: a
 *  comment or a reflowed line must not make the pins silently stop matching. */
function between(src: string, from: string, to: string, label: string): string {
  const start = src.indexOf(from);
  assert.ok(start >= 0, `${label} no longer has \`${from}\``);
  const end = src.indexOf(to, start);
  assert.ok(end > start, `could not delimit \`${from}\` in ${label}`);
  return src.slice(start, end);
}

const writeFn = between(
  shell,
  "async function setDefaultWorkspace",
  "async function signOut",
  "components/Shell.tsx",
);

test("the write reads its outcome through the shared wrapper, never a bare fetch", () => {
  assert.match(
    writeFn,
    /await fetchOrThrow\(\s*"\/api\/auth\/profile\/default-workspace"/,
    "the default-workspace write no longer goes through lib/wsfetch's fetchOrThrow — a bare fetch resolves on a 400 and the refusal is invisible again",
  );
  assert.doesNotMatch(writeFn, /await fetch\(/, "the write still uses a bare fetch, whose 400/500 resolves normally");
  // `catch {` with no binding is the exact shape that swallowed a dropped
  // connection (TypeError: Failed to fetch) without a trace.
  assert.doesNotMatch(writeFn, /catch\s*\{/, "a bindingless `catch {}` is back — transport failures vanish again");
  assert.match(writeFn, /catch \(err\)/, "the catch must see the error to translate the server's machine code");
});

test("a refusal returns the sentence BEFORE reload(), so only success re-reads", () => {
  const returned = writeFn.indexOf("return translateApiErr(errorBodyOf(err), t)");
  const reloaded = writeFn.indexOf("reload()");
  assert.ok(returned >= 0, "the refusal no longer answers with a sentence");
  assert.ok(reloaded >= 0, "the success path no longer reloads the session bundle");
  assert.ok(
    returned < reloaded,
    "the refusal falls through to reload() — a failed write would re-read the unchanged state and still say nothing",
  );
  assert.equal(
    (writeFn.match(/reload\(\)/g) || []).length,
    1,
    "reload() must appear once: on the success tail only",
  );
});

test("the sentence is the dictionary's, selected by DIRECTION", () => {
  assert.match(
    writeFn,
    /workspaceId === ""\s*\?\s*t\("workspace\.clearDefaultFail"\)\s*:\s*t\("workspace\.setDefaultFail"\)/,
    "the two directions must pick workspace.clearDefaultFail / workspace.setDefaultFail — set (a non-empty id) and clear (\"\") are different sentences",
  );
  // The sentences themselves live in i18nDict.ts and are rendered through `t`;
  // typing either one here would freeze it in one language.
  assert.doesNotMatch(writeFn, /Failed to (set|remove) default workspace/, "prose is typed into the handler");
});

test("a refused write is not turned into a /login bounce or a hand-rolled status check", () => {
  assert.doesNotMatch(writeFn, /status\s*===?\s*40[13]/, "a hand-rolled 401/403 check is back (a tenancy 403 is not a dead session — t_da4b2031)");
  assert.doesNotMatch(writeFn, /\/login/, "a refused default-workspace write must not navigate to /login");
});

test("the switcher renders the refusal in a live region OUTSIDE the panel", () => {
  const panel = between(
    switcher,
    "{open && (",
    "{/* Card t_f6de6395: a REFUSED",
    "components/WorkspaceSwitcher.tsx",
  );
  assert.doesNotMatch(
    panel,
    /role="alert"/,
    "the failure sentence is inside the dropdown panel — the panel closes on click, so the sentence unmounts unseen (the original defect)",
  );
  const alert = between(switcher, "{defaultError && (", "</div>\n  );\n}", "components/WorkspaceSwitcher.tsx");
  assert.match(alert, /role="alert"/, "the refusal is not a live region, so a screen reader is never told");
  assert.match(alert, /\{defaultError\}/, "the live region does not render the sentence it was given");
});

test("both directions await the outcome instead of firing and forgetting", () => {
  assert.match(
    switcher,
    /onSetDefault\?: \(workspaceId: string\) => Promise<string \| null>;/,
    "onSetDefault no longer carries the outcome back — the caller cannot tell a refusal from a success",
  );
  assert.equal(
    (switcher.match(/void applyDefault\(/g) || []).length,
    2,
    "both rows (set and clear) must go through applyDefault, which stores the returned sentence",
  );
  assert.doesNotMatch(
    switcher,
    /^\s+onSetDefault\(/m,
    "a row calls onSetDefault directly and drops the sentence it resolves to",
  );
});
