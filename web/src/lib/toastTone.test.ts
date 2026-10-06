// The save-confirmation toast's own guard (rework of PR #997, card t_ad2af554).
//
// THE DEFECT THIS PINS
//   PR #997 added `web/src/components/Toast.tsx` and bound it to `msg` with a 6s
//   auto-dismiss. Two paths on /settings write `msg` for a FAILED save — the
//   `/api/rest/settings/tracking` and `/api/rest/settings/einvoice` PUTs — so a
//   failure was (a) painted in success chrome (an emerald fill and the emerald
//   tick) and then (b) deleted at 6s by `setMsg("")`, which also cleared the
//   page's inline banner. Measured on the built pre-fix tree (Chromium, a 500
//   forced on both PUTs): tracking `@1s` toast "Failed" on `bg-emerald-50`,
//   `@8s` toast absent and the banner empty. The identical probe on the built
//   `origin/main` (which has no Toast at all) left both banners up. The card's
//   own acceptance says it plainly: A FAILURE MUST NOT VANISH ON A TIMER, and a
//   failure must not be painted in success chrome.
//
// WHY A SOURCE GUARD *AND* AN E2E SPEC
//   This repo has no JSX/DOM unit runner (`npm run test:unit` is
//   `node --test src/lib/*.test.ts` — see src/lib/i18n.test.ts), so a unit test
//   cannot drive the component. What a source scan CAN do is stop the two
//   mechanisms coming back out of the code: the timer's `ok` guard and the tone
//   wiring at the call site. The BEHAVIOUR — that the failure really is still on
//   screen 8s later, in the real built page — is measured by
//   `web/e2e/settings-toast-failure.spec.ts` in the `e2e` gate. Neither half is
//   sufficient alone: the spec proves the behaviour on today's bytes, this proves
//   the next revert cannot be silent.
//
// `Toast.tsx` had NO test at all before this card (`grep -rl Toast web/src/*.test.*`
// was empty), which is why the card's own "a regression guard is warranted" for
// the absent-`aria-label` item was unmet and the pre-fix label could come back
// unnoticed.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const SRC = fileURLToPath(new URL("../", import.meta.url));
// Named `srcAt`, not `read`: `src/lib/buildContext.test.ts` resolves every
// `read("<lit>")` in this suite through a helper shape it must be able to see.
// A template-literal path (the shape saveFeedback.test.ts uses too) is invisible
// to that guard, which is correct — the file is under web/src and copied whole.
const srcAt = (rel: string) => readFileSync(`${SRC}${rel}`, "utf8");

const toast = srcAt("components/Toast.tsx");
const settings = srcAt("app/settings/page.tsx");

/**
 * The OPENING TAG of the element carrying `attr`, sliced from the `<` that opens
 * it to the `>` that closes it — so a sibling's attributes cannot satisfy a
 * check aimed at this element. Returns null when the attribute is absent.
 */
function openingTagWith(src: string, attr: string): string | null {
  const at = src.indexOf(attr);
  if (at < 0) return null;
  const open = src.lastIndexOf("<", at);
  const close = src.indexOf(">", at);
  if (open < 0 || close < 0) return null;
  return src.slice(open, close + 1);
}

test("the guard is looking at the real files (it cannot pass vacuously)", () => {
  // Anchor the two files by a fact each: the toast's own test hook, and the
  // settings page's message state. If either moves, re-anchor this guard rather
  // than trusting a green run.
  assert.ok(
    toast.includes('data-testid="save-toast"'),
    "components/Toast.tsx no longer carries data-testid=\"save-toast\" — the e2e spec and this guard both key on it",
  );
  assert.ok(
    settings.includes("const [msg, setMsg] = useState("),
    "app/settings/page.tsx no longer declares the `msg` state — this guard's call-site half is looking at the wrong page",
  );
  // The region this file judges must exist, or every assertion below is vacuous.
  assert.ok(
    openingTagWith(toast, 'role="status"') !== null,
    "components/Toast.tsx no longer renders a role=\"status\" region",
  );
});

test("the status region has NO aria-label (a label REPLACES the announced content)", () => {
  const tag = openingTagWith(toast, 'role="status"');
  assert.ok(tag, "no role=\"status\" region — re-anchor this guard");
  // This is the item the card found unguarded. A label on a live region replaces
  // what is announced, so a screen reader would read the generic button title
  // ("Close") instead of the message. The regression restored that label and
  // flipped the region's accessible name from the message to "Close" (QA
  // measured it: AX name empty with the fix, "Saved" with the pre-fix label).
  assert.doesNotMatch(
    tag!,
    /aria-label/,
    "the role=\"status\" region carries an aria-label — a label on a live region replaces the announced " +
      "content, so the message itself would no longer be read out (this is the exact revert the card was " +
      "filed for). The close button's own aria-label is fine and is a DIFFERENT element.",
  );
  // The region must still be a live region, or the message is only visible, never
  // announced. Both halves are asserted because either one alone is not enough.
  assert.match(tag!, /role="status"/, "the region lost role=\"status\"");
  assert.match(
    toast,
    /aria-live="polite"/,
    "the toast lost aria-live=\"polite\" — a status region with no live announcement is silent to a screen reader",
  );
});

test("the 6s auto-dismiss is armed ONLY for a success — a failure is not on a timer", () => {
  // The guard clause. `onDismiss` is `setMsg("")` at the call site, which clears
  // the page's inline banner as well as this toast — so arming the timer for a
  // failure erases the failure from the whole page. That is the defect.
  assert.match(
    toast,
    /if\s*\(\s*!message\s*\|\|\s*!ok\s*\)\s*return;/,
    "Toast.tsx no longer gates the dismiss timer on `ok` — the timer would then clear a FAILURE at 6s, " +
      "and because onDismiss is setMsg(\"\") it takes the inline banner with it (measured: both gone at 8s)",
  );
  // The timer must still exist and still be the measured 6000ms for a success —
  // a guard that only asserted the early return would pass on a toast that never
  // auto-dismisses a success at all.
  assert.match(
    toast,
    /window\.setTimeout\(\s*onDismiss\s*,\s*6000\s*\)/,
    "Toast.tsx no longer arms a 6000ms dismiss timer for a success (measured auto-dismiss 6098ms)",
  );
  // ...and the effect's dependency list must carry `ok`, or the clause above is
  // evaluated once and the timer is re-armed/skipped on a stale tone.
  const eff = toast.slice(toast.indexOf("useEffect("), toast.indexOf("if (!message) return null;"));
  assert.match(
    eff,
    /\[\s*message\s*,\s*ok\s*,\s*onDismiss\s*\]/,
    "the timer effect does not depend on `ok` — a message whose tone changed would keep the other tone's timer",
  );
});

test("a failure is painted on failure chrome, never success chrome", () => {
  const tag = openingTagWith(toast, 'data-testid="save-toast"');
  assert.ok(tag, "no data-testid=\"save-toast\" element — re-anchor this guard");
  // The tone is an INPUT, not a guess from the string: a hard-coded emerald fill
  // here is exactly the pre-fix defect.
  assert.match(tag!, /\bok\b/, "the toast's tone is not read from the `ok` prop");
  assert.match(tag!, /ok\s*\?\s*"border-emerald-200 bg-emerald-50"\s*:\s*"border-red-200 bg-red-50"/,
    "the toast's fill is no longer chosen by the tone — a failure and a success would paint the same colour");
  // The tick is the success MARK. Reusing it on a failure says the save
  // succeeded on a screen where it did not.
  assert.match(toast, /bg-emerald-600\b/, "the success tick's disc is gone");
  assert.match(toast, /bg-red-600\b/, "the failure icon's disc is gone");
  // The failure ink must clear AA on its own red-50 tint: red-600 is 4.36:1
  // there (measured from the installed theme) and is deliberately NOT used.
  assert.match(toast, /text-red-800/, "the failure message is not the AA-safe red-800 ink (red-600 on red-50 is 4.36:1)");
  assert.match(toast, /text-emerald-800/, "the success message is not the AA-safe emerald-800 ink");
  // The region must carry BOTH fills, chosen by the tone: a revert to a single
  // hard-coded emerald fill (the pre-fix shape) is caught here.
  assert.match(
    tag!,
    /bg-red-50/,
    "the region has no failure fill — a failure would paint the success chrome (the carded defect)",
  );
});

test("every failure writer on /settings hands the toast the failure tone", () => {
  // The call site. The tone and the text have to travel TOGETHER — that is why
  // the page sets both in one call rather than leaving `msgOk` to fall out of a
  // previous save.
  assert.match(
    settings,
    /<Toast\s[\s\S]*?\bmessage=\{msg\}\s[\s\S]*?\bok=\{msgOk\}/,
    "the Toast is not handed the message's tone (`ok={msgOk}`) — it would paint every message with one chrome",
  );
  // The two paths the card measured: a FAILED PUT must write the failure tone.
  // Anchored on the endpoint so a reworded message cannot move them.
  for (const [endpoint, failureKey] of [
    ["/api/rest/settings/tracking", "prof.failed"],
    ["/api/rest/settings/einvoice", "set.failedSave"],
  ] as const) {
    const call = settings.indexOf(endpoint);
    assert.ok(call > 0, `${endpoint} is no longer written by /settings — re-anchor this guard`);
    const block = settings.slice(call, call + 700);
    assert.match(
      block,
      /\(r\.ok \? showMsgOk : showMsgFail\)\(/,
      `after a FAILED PUT to ${endpoint} the page does not choose the tone from r.ok — the failure would ` +
        `inherit the success chrome AND the 6s auto-dismiss (this is the carded defect)`,
    );
    assert.match(
      block,
      new RegExp(`t\\(r\\.ok \\? "[^"]+" : "${failureKey.replace(".", "\\.")}"\\)`),
      `the ${endpoint} failure no longer writes its translated failure sentence (${failureKey})`,
    );
  }
  // And the inline banner must branch the same way, because `setMsg("")` clears
  // it too — the banner is half of what the timer used to erase.
  assert.match(
    settings,
    /msgOk \? "bg-emerald-50 text-emerald-700" : "bg-red-50 text-red-700"/,
    "the inline `msg` banner no longer branches its chrome on `msgOk` — a failure would read as a success there",
  );
  // The CHIP unverified save is a failure-class message and must use the failure tone.
  assert.match(
    settings,
    /\(d\.verified === true \? showMsgOk : showMsgFail\)\(/,
    "the CHIP save no longer tones `savedCredsUnverified` as a failure",
  );
});

test("the guard would fail on the code it replaced (in-file bite proof)", () => {
  // The exact pre-fix shapes, planted hermetically: each assertion above must
  // reject its own mutation. Without this, a scan that matched nothing would
  // read as a clean file. (The value-mutation proof — mutate the real files and
  // watch this suite go RED, then restore — is quoted on the card; this keeps a
  // fast in-file version beside it for the shapes most likely to be re-created.)
  const preFixTimer =
    "useEffect(() => {\n  if (!message) return;\n  const id = window.setTimeout(onDismiss, 6000);\n  return () => window.clearTimeout(id);\n}, [message, onDismiss]);";
  // 1. The pre-fix timer (no `ok` guard) must NOT satisfy the timer's guard rule.
  assert.doesNotMatch(
    preFixTimer,
    /if\s*\(\s*!message\s*\|\|\s*!ok\s*\)\s*return;/,
    "the pre-fix timer was read as gating the timer on the tone",
  );
  // ...and it must also fail the dependency rule (it lacks `ok`).
  assert.doesNotMatch(
    preFixTimer.slice(preFixTimer.indexOf("useEffect(")),
    /\[\s*message\s*,\s*ok\s*,\s*onDismiss\s*\]/,
    "the pre-fix timer was read as depending on the tone",
  );
  // 2. The pre-fix region carried the aria-label this guard refuses.
  const preFixRegion = '<div role="status" data-testid="save-toast" aria-label={closeTitle} className="bg-emerald-50">';
  assert.match(
    preFixRegion,
    /aria-label/,
    "the pre-fix region shape must carry the aria-label this guard rejects, or the bite proves nothing",
  );
  assert.doesNotMatch(
    preFixRegion,
    /ok\s*\?\s*"border-emerald-200 bg-emerald-50"/,
    "the pre-fix region must not already branch its fill on a tone",
  );
  // 3. The call site that shipped the defect: one setter, one tone, no choice —
  //    and no tone prop handed to the toast.
  const preFixCall = 'setMsg(r.ok ? t("set.trackingSaved") : t("prof.failed"));';
  assert.doesNotMatch(
    preFixCall,
    /showMsgOk : showMsgFail/,
    "the pre-fix tracking call was read as choosing a tone — the guard is not looking at the real shape",
  );
});
