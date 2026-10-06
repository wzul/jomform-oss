// Guard for the shared `SaveButton` — the button that confirms on itself.
//
// THE DEFECT THIS PINS (measured on staging 2026-10-06)
//   The Storefront tab's "Save footer" button held a "Saved ✓" state for 2s
//   (PR #997), so the confirmation landed where the merchant pressed. That fix
//   was written INLINE, on ONE button. Every other save button on the same page
//   kept the old behaviour, so the same product answered the same press two
//   different ways:
//
//     "Save footer"       -> the button turns "Saved ✓" (green) AND a toast
//     "Save landing page" -> only a toast; the button never changed
//
//   Measured: pressing "Save landing page" showed the toast "Landing page saved ✓"
//   while the button's own label and colour stayed exactly as before. A merchant
//   watching the control they just clicked saw nothing happen.
//
//   The fix is the extraction, not a second inline copy: `components/SaveButton.tsx`
//   carries the state machine, and BOTH buttons render it, so the two cannot drift
//   again — which is precisely how this was found.
//
// WHAT IS PINNED, AND WHY EACH ONE MATTERS
//   1. Both save buttons on the page are wired through the component. One
//      unwired button is the whole defect.
//   2. A FAILURE MUST NOT CONFIRM. `onSave` returning `false`, or throwing,
//      leaves the button idle — otherwise a failed write displays "Saved ✓".
//      This is the same class as the tone defect on these pages, and it is the
//      half a copy of the footer's original inline version did not have.
//   3. The confirmation label is a SHORTER dictionary key, not the full
//      sentence: a longer string widens the button mid-press, which reads as a
//      layout jump exactly when the user is looking at it. And it must be a DICT
//      key — `t()` falls back to the key itself, so a literal ships English to
//      every locale silently.
//   4. The timer is cleared BEFORE re-arming and on unmount: a stale timer
//      flipping a NEWER label back to idle is the bug this prevents.
//   5. The state class APPENDS to `jf-btn-primary` rather than forking it.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * It reads source, so it cannot observe the rendered state. That was
//     measured in a real browser on staging: the landing button reached
//     `Saved ✓` / `rgb(4,120,87)` at ~204ms and reverted at ~2204ms, while the
//     footer (moved to the same component) reached it at ~256ms and reverted at
//     ~2254ms — so the move did not regress the button that already worked.
//   * It does not re-test the tone work on /sales, /spam and /form-settings:
//     that is a separate, pre-existing contract owned by
//     `src/lib/actionFailureTone.test.ts`.
import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const SRC = fileURLToPath(new URL("../", import.meta.url));
const settings = readFileSync(`${SRC}app/settings/page.tsx`, "utf8");
const saveBtn = readFileSync(`${SRC}components/SaveButton.tsx`, "utf8");

test("the guard is looking at real files (it cannot pass vacuously)", () => {
  assert.ok(existsSync(`${SRC}components/SaveButton.tsx`),
    "components/SaveButton.tsx moved — this guard is checking nothing");
  assert.ok(saveBtn.length > 500, "components/SaveButton.tsx is suspiciously small — did the read break?");
  assert.ok(settings.includes("async function saveLandingPage"),
    "settings/page.tsx no longer defines saveLandingPage — the guard's anchor moved");
  assert.ok(settings.includes("async function saveStorefrontSettings"),
    "settings/page.tsx no longer defines saveStorefrontSettings — the guard's anchor moved");
});

test("BOTH save buttons are wired through the shared component", () => {
  // The landing button is the one that was reported. If it goes back to a plain
  // <button>, the symptom returns exactly: a toast appears and the button does
  // not change.
  assert.ok(settings.includes("onSave={saveLandingPage}"),
    "the landing-page Save button is no longer wired through SaveButton — it would go back to " +
      "showing only a toast while the button itself never changes (the reported symptom)");
  assert.ok(settings.includes("onSave={saveStorefrontSettings}"),
    "the footer Save button is no longer wired through SaveButton");
  assert.equal(settings.match(/<SaveButton/g)?.length, 2,
    "settings/page.tsx renders a different number of SaveButton components than the two save " +
      "buttons it has — one of them was hand-rolled again, which is how the two drifted apart");
});

test("a FAILURE must not confirm on the button", () => {
  // Both signals: the caller's explicit `false`, and a thrown error.
  assert.match(saveBtn, /if \(\(await onSave\(\)\) === false\)\s*confirmed = false;/,
    "components/SaveButton.tsx no longer honours a `false` return from onSave — a FAILED save " +
      "would display the success confirmation");
  assert.match(saveBtn, /\}\s*catch\s*\{[\s\S]{0,400}?confirmed = false;/,
    "components/SaveButton.tsx no longer treats a rejected onSave as a failure — a thrown error " +
      "would display the success confirmation");
  assert.match(saveBtn, /if \(!confirmed\) return;/,
    "components/SaveButton.tsx sets the saved state regardless of the outcome");
  // And the page must actually SIGNAL failure for both handlers, or the
  // component's contract is decorative.
  for (const fn of ["saveStorefrontSettings", "saveLandingPage"]) {
    assert.ok(
      settings.includes(`async function ${fn}(): Promise<boolean>`),
      `${fn} no longer returns a success boolean — SaveButton cannot tell a failed write from a ` +
        "good one and would confirm both",
    );
  }
});

test("the confirmation label is the SHORT dictionary key", () => {
  // On the button: the short form, so the control does not widen mid-press.
  assert.match(settings, /savedLabel=\{t\("set\.lpSavedShort"\)\}/,
    "the landing Save button no longer uses the short saved label — the full " +
      '"Landing page saved ✓" widens the button as it switches');
  assert.match(settings, /savedLabel=\{t\("set\.sfSavedShort"\)\}/,
    "the footer Save button no longer uses the short saved label");
  // And the key must exist in the dictionary, or t() renders the raw key.
  const dict = readFileSync(`${SRC}components/i18nDict.ts`, "utf8");
  for (const key of ["set.lpSavedShort", "set.sfSavedShort"]) {
    assert.ok(dict.includes(`"${key}"`),
      `i18nDict.ts has no "${key}" entry — t() falls back to the key itself, so the button would ` +
        "show the literal string to every locale");
  }
  // Five locales, like every other entry in this dictionary.
  const entry = dict.slice(dict.indexOf('"set.lpSavedShort"'));
  const body = entry.slice(0, entry.indexOf("},"));
  for (const loc of ["en:", "ms:", "zh:", "ta:", "ar:"]) {
    assert.ok(body.includes(loc), `set.lpSavedShort is missing its \`${loc}\` locale`);
  }
});

test("the timer is cleared before re-arming and on unmount", () => {
  assert.match(saveBtn, /if \(timer\.current !== null\) \{\s*window\.clearTimeout\(timer\.current\);/,
    "components/SaveButton.tsx does not clear a pending timer before arming a new one — saving " +
      "twice would let the FIRST save's timer revert the second save's label early");
  assert.match(saveBtn, /return \(\) => \{\s*if \(timer\.current !== null\) window\.clearTimeout\(timer\.current\);/,
    "components/SaveButton.tsx's unmount cleanup is gone — a save completing after navigation " +
      "would set state on a dead component");
});

test("the saved state APPENDS to the base class rather than forking it", () => {
  assert.match(saveBtn, /className = "jf-btn-primary"/,
    "components/SaveButton.tsx no longer defaults to the base .jf-btn-primary class");
  assert.match(saveBtn, /savedClassName = "jf-btn-primary jf-btn-saved"/,
    "components/SaveButton.tsx no longer appends jf-btn-saved to jf-btn-primary — a fork loses " +
      "the base rule");
  // The page must not override either default.
  assert.doesNotMatch(settings, /<SaveButton[\s\S]{0,300}?className=/,
    "settings/page.tsx overrides SaveButton's className — the shared base/state pair would no " +
      "longer apply to that button");
});
