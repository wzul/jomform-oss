// The builder's save() must leave the merchant something to press when a step
// THROWS (t_d0532421).
//
// THE DEFECT: `save()` set `saving = true` and then awaited its writes with no
// try/catch and no finally. Its five early-return paths released the flag one
// by one, and the success tail released it too — so a fetch that REJECTED
// (`TypeError: Failed to fetch` on a dropped connection, an aborted request, a
// body that will not parse) exited the function without passing any of them.
// `saving` stayed true: both toolbar Save buttons and "Save as template" stayed
// `disabled` and the Save buttons read "Saving…" for good, no sentence appeared,
// and the rejection escaped the onClick handler uncaught.
//
// This file pins the SHAPE that makes that impossible, because a component
// handler cannot be rendered by this repo's unit runner (node --test, no JSX/DOM
// setup) — the behaviour itself is driven for real by
// `web/e2e/builder-save-failure.spec.ts`, which aborts each write at the
// transport level on the built page. Each assertion here is the cheapest thing
// that can go wrong the moment the wrapper is removed:
//
//   1. the release lives in a `finally`, and there is exactly ONE of it;
//   2. the step function does not touch `saving` at all (that is how a throw
//      escaped: it had to reach a per-path release that never ran);
//   3. a throw is answered with the save-failure sentence and an explicit null,
//      so no caller can mistake a failed save for a written one;
//   4. `dirty` is cleared ONLY on the success tail — a save that wrote nothing
//      must leave the "unsaved" marker up (the decision the card asked to be
//      made deliberately rather than copied from publishAnyway()).
//
// Run with the repo's Node test runner:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const page = readFileSync(
  fileURLToPath(new URL("../app/builder/page.tsx", import.meta.url)),
  "utf8",
);

/** A slice of the page between two anchors, anchored by INDEX (the same reason
 *  templateEdit.test.ts does it): a comment or a reflowed line must not make the
 *  pins below silently stop matching anything. */
function between(from: string, to: string): string {
  const start = page.indexOf(from);
  assert.ok(start >= 0, `app/builder/page.tsx no longer has \`${from}\``);
  const end = page.indexOf(to, start);
  assert.ok(end > start, `could not delimit \`${from}\` in app/builder/page.tsx`);
  return page.slice(start, end);
}

const saveFn = between("async function save(publish: boolean)", "async function saveUnchecked(publish: boolean)");
const stepsFn = between("async function saveUnchecked(publish: boolean)", "async function copyPublishUrl");

test("save() releases the busy flag in a finally, exactly once", () => {
  const releases = saveFn.match(/setSaving\(false\)/g) || [];
  assert.equal(
    releases.length,
    1,
    `save() releases \`saving\` ${releases.length} times — there must be exactly ONE release, in the finally, so a path nobody wrote cannot skip it`,
  );
  const finallyAt = saveFn.indexOf("finally {");
  const releaseAt = saveFn.indexOf("setSaving(false)");
  assert.ok(finallyAt >= 0, "save() has no `finally` — a rejecting fetch leaves `saving` true forever (t_d0532421)");
  assert.ok(
    releaseAt > finallyAt,
    "save()'s single `setSaving(false)` is not inside its `finally` — every exit must release it, including a throw",
  );
  assert.match(
    saveFn,
    /try \{\s*\n\s*return await saveUnchecked\(publish\);/,
    "save() no longer runs its steps inside the try — a throw would escape the wrapper that releases the flag",
  );
});

test("the step function never touches the busy flag itself", () => {
  assert.doesNotMatch(
    stepsFn,
    /setSaving\(/,
    "saveUnchecked() releases `saving` itself again: each per-path release is a path a THROW does not reach, so the flag sticks and both Save buttons die on \"Saving…\" (t_d0532421)",
  );
});

test("a thrown step is answered with a sentence and an explicit null", () => {
  assert.match(
    saveFn,
    /catch \{\s*\n(?:\s*\/\/.*\n)*\s*setLoadErr\(t\(editingTemplate \? "tplmg\.editFail" : "bld\.saveFail"\)\);/,
    'save() catches a thrown step without reporting it — the merchant would get a button back and no cause (the page\'s own sentence is bld.saveFail)',
  );
  assert.match(
    saveFn,
    /catch \{[\s\S]*?return null;/,
    "save()'s catch does not answer `null` — a caller (\"Save as template\") must be able to tell a failed save from one that wrote a row",
  );
});

test("a failed save keeps the \"unsaved\" marker", () => {
  const cleared = stepsFn.indexOf("setDirty(false)");
  const lastFailure = stepsFn.lastIndexOf("return null;");
  assert.ok(cleared >= 0, "saveUnchecked() no longer clears `dirty` on success — the marker would never go away");
  assert.ok(lastFailure >= 0, "saveUnchecked() has no failure return — the delimiters moved");
  assert.ok(
    cleared > lastFailure,
    "`setDirty(false)` runs before saveUnchecked()'s failure returns: a save that wrote nothing would clear the \"unsaved\" marker, i.e. claim the edits are saved",
  );
  const tail = stepsFn.slice(lastFailure);
  assert.match(tail, /setDirty\(false\)/, "the success tail no longer clears the \"unsaved\" marker");
  assert.match(tail, /return id;/, "the success tail no longer returns the id it wrote (t_c33effe4)");
});
