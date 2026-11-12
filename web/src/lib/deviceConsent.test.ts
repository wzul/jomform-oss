// The device-flow consent card's code lookup: one settled answer, and a
// superseded lookup that cannot paint anything (cards t_3368be9e, t_762ada65).
//
// THE DEFECT. Wan's screenshot of /profile?tab=mcp showed a RED banner "This code
// is not valid or has expired" directly above a FULLY RENDERED consent card for
// "Jom Form (Grok Bot)", read & write, with Approve / Deny. Both cannot be true.
//
//   `checkDevice()` was correct on its own — it cleared the error on success and
//   set it in `catch` — but TWO lookups could be in flight: the effect fired one
//   for the `?user_code=` the email link carried (the EXPIRED code from the
//   earlier attempt), and typing/pasting the fresh code fired another. Responses
//   were not sequenced, so the slower one settled last: its 404 cleared the fresh
//   consent and painted the error over the card. The half-and-half frame is the
//   bug, and no reader could tell which half was true.
//
// WHAT IS PINNED HERE
//   1. THE RACE: request A (dead code) settles with 404 AFTER request B resolved a
//      valid consent — the state keeps the consent and has NO problem. Remove the
//      id guard in `settleLookup` and this test goes red (bite proof in the PR).
//   2. THE MIRROR: a superseded SUCCESS cannot paint a card over a fresher error.
//   3. THE SHAPE: a state with a card has no problem and a state with a problem
//      has no card — the two-halves frame is not expressible.
//   4. CLEARING clears BOTH halves.
//   5. THE PAGE WIRES TO ALL OF IT: it renders the problem under `!deviceConsent`
//      (mutually exclusive with the card), every lookup claims an id and applies
//      only through `settleLookup`, and the input's change handler drops the
//      previous answer and strands the lookup in flight. Those are source
//      assertions because this repo has no DOM/JSX unit runner (see
//      src/lib/profileTab.test.ts) — the rendered DOM is measured by the browser
//      arms in web/e2e/a11y-profile-tabs.spec.ts, which load the built page with
//      the device endpoint stubbed and read the live text.
//   6. ONE NORMALISATION RULE: this module defines none, the page imports
//      lib/deviceCode.ts's, and the repo holds no second copy of the rewrite.
//
// Run with:
//
//   npm run test:unit

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  DEVICE_IDLE,
  beginLookup,
  checkingDevice,
  clearedDevice,
  deviceErrorKey,
  deviceLookupOf,
  isCurrentLookup,
  newLookupSequence,
  settleLookup,
  type DeviceConsent,
  type DeviceState,
} from "./deviceConsent.ts";

const HERE = fileURLToPath(new URL(".", import.meta.url));
const PAGE = readFileSync(`${HERE}../app/profile/page.tsx`, "utf8");
const LIB = readFileSync(`${HERE}./deviceConsent.ts`, "utf8");
const DICT = readFileSync(`${HERE}../components/i18nDict.ts`, "utf8");
const LIB_DIR = readdirSync(HERE);

const CONSENT: DeviceConsent = {
  user_code: "KX9M-X5BS",
  client_id: "client-1",
  client: "Jom Form (Grok Bot)",
  scopes: ["read", "readwrite"],
  choices: ["read", "readwrite"],
  scope_prose: "read and write — can create, change and delete your forms, products and sales",
  expires_at: "2099-01-01T00:00:00Z",
  state: "pending",
};

// ─── 1. the race the card was filed for ─────────────────────────────────────

test("RACE: a superseded 404 cannot paint a problem over a valid consent card", () => {
  // The exact frame: the email's dead code (A) is looked up first and answers
  // slowly; the fresh code (B) is looked up second and answers at once. Then A
  // settles — LAST, which is what used to win.
  const seq = newLookupSequence();
  const idA = beginLookup(seq); // the ?user_code= the link carried (expired)
  const idB = beginLookup(seq); // the code the person typed

  const afterB = settleLookup(seq, idB, "KX9M-X5BS", { consent: CONSENT });
  assert.notEqual(afterB, null, "the newest lookup must be allowed to paint");
  assert.deepEqual(afterB, { consent: CONSENT, problem: "", code: "KX9M-X5BS", checking: false });

  const afterA = settleLookup(seq, idA, "ZZZZ-ZZZZ", { failed: true });
  assert.equal(afterA, null, "the superseded lookup must be ignored outright");

  // The stale response cannot reach the state at all — not the problem, and not
  // the consent either (clearing it would be the same bug, mirrored).
  const state: DeviceState = afterA ?? afterB!;
  assert.equal(state.consent, CONSENT);
  assert.equal(state.problem, "", "a superseded 404 must not set the 'not valid or has expired' copy");
  assert.equal(state.code, "KX9M-X5BS", "and it must not leave the dead code behind as the problem's subject");
  assert.equal(deviceLookupOf(state), "idle");
});

test("a superseded SUCCESS cannot resurrect a card over a fresher problem", () => {
  // Mirror of the same defect: A resolves a consent after B already failed.
  // Ignoring only one half would let a card appear for a code the person has
  // already moved past.
  const seq = newLookupSequence();
  const idA = beginLookup(seq);
  const idB = beginLookup(seq);
  const afterB = settleLookup(seq, idB, "ZZZZ-ZZZZ", { failed: true });
  assert.deepEqual(afterB, { consent: null, problem: "not_found", code: "ZZZZ-ZZZZ", checking: false });
  assert.equal(deviceLookupOf(afterB!), "invalid");

  assert.equal(
    settleLookup(seq, idA, "KX9M-X5BS", { consent: CONSENT }),
    null,
    "a superseded success painted a card over an error that is still the current answer",
  );
});

test("editing the field strands the lookup in flight: its id is no longer current", () => {
  // What the page's onChange does: claim an id without starting a request. The
  // in-flight request for the old code then settles into nothing.
  const seq = newLookupSequence();
  const id = beginLookup(seq);
  assert.equal(isCurrentLookup(seq, id), true);
  beginLookup(seq); // the person edited the field
  assert.equal(isCurrentLookup(seq, id), false);
  assert.equal(
    settleLookup(seq, id, "KX9M-X5BS", { consent: CONSENT }),
    null,
    "a response for a code the person has already moved past must not paint",
  );
});

test("the guard is per-sequence, not a global: a fresh sequence starts at 1 and is never stale", () => {
  // Two page instances (two tabs, or a remount) must not invalidate each other,
  // and the first lookup of a new instance must still be able to paint.
  const a = newLookupSequence();
  const b = newLookupSequence();
  assert.equal(beginLookup(a), 1);
  assert.equal(beginLookup(b), 1);
  assert.notEqual(settleLookup(a, 1, "KX9M-X5BS", { consent: CONSENT }), null);
});

// ─── 2. the shape: card and problem are one answer ──────────────────────────

test("a settled answer never carries a card AND a problem", () => {
  const seq = newLookupSequence();
  const ok = settleLookup(seq, beginLookup(seq), "KX9M-X5BS", { consent: CONSENT })!;
  const bad = settleLookup(seq, beginLookup(seq), "ZZZZ-ZZZZ", { failed: true })!;
  for (const state of [ok, bad, DEVICE_IDLE]) {
    assert.ok(
      state.consent === null || state.problem === "",
      "a state carrying both a consent card and a problem is the two-halves frame the bug produced",
    );
    assert.equal(state.checking, false, "a settled or idle state is not 'checking'");
  }
  assert.deepEqual(DEVICE_IDLE, { consent: null, problem: "", code: "", checking: false });
  assert.deepEqual(checkingDevice(), { consent: null, problem: "", code: "", checking: true });
  assert.equal(deviceLookupOf(checkingDevice()), "checking");
});

test("clearing the field drops the consent AND the problem — neither may outlive the code", () => {
  assert.deepEqual(clearedDevice(), DEVICE_IDLE, "clearing must empty both halves");
  assert.equal(clearedDevice().problem, "");
  assert.equal(clearedDevice().consent, null);
  assert.equal(deviceLookupOf(clearedDevice()), "idle");
});

test("the problem is a KEY, so its sentence follows the language instead of freezing", () => {
  assert.equal(deviceErrorKey("not_found"), "prof.deviceNotFound");
  assert.equal(deviceErrorKey(""), null);
  // Both keys exist, in all five locales, and main's #1735 wording is intact:
  // the a11y arm asserts the sentence ends by naming the next step.
  for (const [key, en] of [
    ["prof.deviceNotFound", "This code is not valid or has expired. Ask your agent for a new one."],
    ["prof.deviceYourCode", "Your code"],
  ] as const) {
    const row = DICT.match(new RegExp(`"${key.replace(/\./g, "\\.")}": \\{([^}]*)\\}`));
    assert.ok(row, `${key} is missing from i18nDict.ts`);
    assert.ok(row![1].includes(`en: "${en}"`), `${key}'s English copy moved: ${row![1].slice(0, 120)}`);
    for (const lang of ["ms", "zh", "ta", "ar"]) {
      assert.ok(row![1].includes(`${lang}: "`), `${key} has no ${lang} translation`);
    }
  }
});

// ─── 3. the page is wired to all of it ──────────────────────────────────────

test("the page renders the problem only when there is NO consent card", () => {
  // Card point 2: the two are mutually exclusive in the RENDER, not just in
  // practice. The banner is the one JSX expression gated on `!deviceConsent`.
  assert.match(
    PAGE,
    /\{deviceErr && !deviceConsent && \(/,
    "the device problem is not gated on !deviceConsent — a stale banner could paint over a card again",
  );
  // And the panel's state is ONE value, so nothing can set half of it.
  assert.match(PAGE, /useState<DeviceState>\(DEVICE_IDLE\)/);
  assert.doesNotMatch(PAGE, /setDeviceConsent\s*\(/, "the page still sets the consent on its own, apart from the problem");
  assert.doesNotMatch(PAGE, /setDeviceLookup\s*\(/, "the page still keeps a second, independent lookup state");
});

test("every lookup claims an id and applies only through settleLookup", () => {
  // The guard is worth nothing if a call site can still set the state directly.
  assert.match(PAGE, /const id = beginLookup\(deviceSeq\.current\);/);
  assert.match(
    PAGE,
    /const next = settleLookup\(deviceSeq\.current, id, code, outcome\);[\s\S]{0,240}?if \(!next\) return;/,
    "the page does not drop a superseded answer before it touches the consent or the scope picker",
  );
  // The two ways a lookup can be started, both through the one sequenced path:
  // the email's `?user_code=` (once) and the form's submit.
  assert.match(PAGE, /void checkDevice\(uc\);/, "the ?user_code= entry point no longer goes through checkDevice");
  assert.match(PAGE, /void checkDevice\(deviceCode\);/, "the typed-code entry point no longer goes through checkDevice");
  assert.match(PAGE, /const seededDeviceCode = useRef\(false\);/, "the ?user_code= lookup must stay ONE-SHOT");
});

test("the input's change handler drops the whole previous answer and strands the lookup in flight", () => {
  assert.match(
    PAGE,
    /beginLookup\(deviceSeq\.current\);\s*\n\s*setDevice\(clearedDevice\(\)\);/,
    "the input does not clear the card and the problem when the code changes",
  );
  assert.match(PAGE, /setDeviceCode\(e\.target\.value\);/, "the input no longer stores what was typed");
});

test("the panel stays on the MCP tab even when the field is empty", () => {
  // #1735's shape, which this rework must not revert: with the panel gated on
  // `deviceCode || deviceConsent`, clearing the field unmounted the FIELD — the
  // one control needed to correct a mistyped code, gone with the code.
  assert.doesNotMatch(
    PAGE,
    /\{\(deviceCode \|\| deviceConsent\) && \(/,
    "the consent panel is gated on a code again — clearing the input would take the input with it",
  );
  assert.match(PAGE, /id="prof-device-code"/, "the always-on code field is gone");
  assert.match(PAGE, /<div className="mt-4 rounded-lg border border-\[var\(--jf-border\)\] bg-slate-50 p-4">/);
});

test("the card names the code it belongs to, in the email's own words", () => {
  assert.match(PAGE, /t\("prof\.deviceYourCode"\)/);
  assert.match(PAGE, /\{deviceConsent\.user_code\}/, "the card must show the code the consent belongs to");
});

test("ONE normalisation rule: this module defines none and the page imports deviceCode.ts's", () => {
  assert.doesNotMatch(
    LIB,
    /export function normalize/,
    "deviceConsent.ts grew a second normaliser — lib/deviceCode.ts already owns that rule",
  );
  assert.match(PAGE, /import \{ normalizeDeviceCode \} from "\.\.\/\.\.\/lib\/deviceCode";/);
  // No other web source file may define the rule. `normalizeUserCode` was the
  // name of the copy this rework deleted, so its return is a red test rather than
  // a silent third implementation.
  for (const file of LIB_DIR) {
    if (!file.endsWith(".ts") || file === "deviceConsent.test.ts" || file.startsWith("deviceCode")) continue;
    const src = readFileSync(`${HERE}./${file}`, "utf8");
    assert.doesNotMatch(
      src,
      /function normalize(UserCode|DeviceCode)\s*\(/,
      `${file} defines a user-code normaliser; there must be exactly one (lib/deviceCode.ts)`,
    );
  }
});
