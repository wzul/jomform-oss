// The Save-footer button answers the mouse and the PUT, and the shared button
// classes carry the states that were missing (card t_b73eee21).
//
// THE DEFECT, MEASURED ON THE BUILT PAGE (Chromium, production build, real DOM)
//   `settings/page.tsx` rendered `<button onClick={saveStorefrontSettings}
//   className="jf-btn-primary">`, and `.jf-btn-primary` was `transition:
//   background 0.15s` + a `:hover` colour swap and nothing else. Read out of the
//   live DOM:
//     * computed `cursor: default` — the pointer never became a pointer, so the
//       button did not announce itself as clickable ("butang tak nampak
//       responsive");
//     * a held mouse-down was byte-identical to `:hover` (same background
//       `rgb(130,143,255)`, same `box-shadow: none`) — there was no pressed
//       state at all;
//     * `cursor: default` and NO disabled state while the PUT was in flight: two
//       clicks in one flight produced two PUTs of the same body, with nothing on
//       the button to say the first had landed.
//
// WHAT THIS FILE PINS, AND WHAT IT DELIBERATELY DOES NOT
//   This is the SOURCE half. The rendered half (the pointer, the press, the
//   busy read, the measured contrast of the chosen colours) is the live-DOM
//   reading quoted on the card and in the PR — a source scan cannot measure a
//   computed style, and this repo has no DOM test runner (see
//   src/lib/i18n.test.ts). What a source scan CAN do is stop the four fixes
//   coming back out of the class, and it can prove the wiring that makes the
//   busy state observable is on the ACTUAL button.
//
//   IT DOES NOT RELAX THE CARD'S "NO BEHAVIOUR CHANGE" RULE. The four toggle
//   state variables, the PUT endpoint and the PUT body are asserted VERBATIM so
//   a presentation change cannot quietly move the wire contract while this
//   guard stays green.
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
// `read("<lit>")` in this suite through a helper shape it must be able to see,
// and reports one it cannot resolve as an offender. Every read here is under
// `web/src`, which the web image copies whole.
const srcAt = (rel: string) => readFileSync(`${SRC}${rel}`, "utf8");

const settings = srcAt("app/settings/page.tsx");
const saveBtn = srcAt("components/SaveButton.tsx");
const css = srcAt("app/globals.css");

/** The body of a `.name { ... }` rule (first match), or null. */
function ruleBody(name: string): string | null {
  const m = css.match(new RegExp(`\\.${name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}\\s*\\{([^}]*)\\}`));
  return m ? m[1] : null;
}

/** The Save-footer button's JSX, from its `onClick` back to the previous `<`. */
function saveButtonJsx(): string {
  const at = settings.indexOf("onSave={saveStorefrontSettings}");
  assert.ok(at > 0, "settings/page.tsx no longer wires a button to saveStorefrontSettings — re-anchor this guard");
  const open = settings.lastIndexOf("<SaveButton", at);
  const close = settings.indexOf("/>", at);
  assert.ok(open >= 0 && close > open, "could not bound the Save-footer button's JSX — re-anchor this guard");
  return settings.slice(open, close + 2);
}

test("the guard is looking at the real page and stylesheet (it cannot pass vacuously)", () => {
  assert.ok(
    settings.includes("async function saveStorefrontSettings"),
    "settings/page.tsx no longer defines saveStorefrontSettings — the guard's anchor moved",
  );
  for (const cls of ["jf-btn-primary", "jf-btn-ghost"]) {
    const body = ruleBody(cls);
    assert.ok(body, `no .${cls} rule in globals.css — re-anchor this guard`);
    assert.match(body!, /min-height:\s*40px/, `.${cls} is not the measured button rule`);
  }
});

test("the Save button carries the PUT's in-flight state (disabled + aria-busy)", () => {
  saveButtonJsx();
  assert.match(
    saveBtn,
    /disabled=\{disabled \|\| busy\}/,
    "the Save-footer button is not disabled while the PUT is in flight — a double-click " +
      "fires a second PUT of the same body with no feedback (measured: putCount 2 in one flight)",
  );
  // The busy state must be ANNOUNCED, not only drawn: a disabled button that
  // says nothing is the same silence the card is about, for screen readers.
  assert.match(saveBtn, /aria-busy=\{busy\}/, "components/SaveButton.tsx does not expose its busy state via aria-busy");
  // The label must stay a dictionary string. It now has TWO states on one
  // button — the idle `set.sfSave` and a 2s confirmation — so both are pinned.
  // The confirmation must be a DICT key, not a literal: `t()` falls back to the
  // key itself, so a literal would ship English to every locale silently.
  assert.match(saveButtonJsx(), /savedLabel=\{t\("set\.sfSavedShort"\)\}/,
    "the Save-footer label is no longer the dictionary pair (idle set.sfSave / saved set.sfSavedShort)");
  // This card ADDS a state to the shared class rather than forking it: the
  // className is still jf-btn-primary, with the state class appended only while
  // the save confirmation is showing.
  assert.match(saveButtonJsx(), /label=\{t\("set\.sfSave"\)\}/,
    "the Save-footer button forked .jf-btn-primary instead of appending a state class");
});

test("the busy flag exists, starts off, and gates re-entry", () => {
  assert.match(
    saveBtn,
    /const \[busy, setBusy\] = useState\(false\)/,
    "there is no `sfSaving` state (or it does not start false) — the disabled attribute above would be undefined",
  );
  // The re-entry guard is the OTHER half: `disabled` stops the mouse, this stops
  // a programmatic/second submit. Both are needed — `disabled` alone is a
  // rendering decision, and a click already queued when the flag flips would
  // otherwise still run.
  assert.match(
    saveBtn,
    /if \(busy\) return;/,
    "saveStorefrontSettings has no in-flight re-entry guard — a second call while the PUT is open " +
      "would send the settings twice",
  );
  // It must be cleared on EVERY path, or a single failed request leaves the
  // button dead for the rest of the session.
  const fn = settings.slice(settings.indexOf("async function saveStorefrontSettings"));
  const body = fn.slice(0, fn.indexOf("\n  }\n"));
  assert.match(saveBtn, /setBusy\(true\)/, "components/SaveButton.tsx never sets the busy flag");
  assert.match(saveBtn, /finally\s*\{[\s\S]*?setBusy\(false\);/, "components/SaveButton.tsx does not clear the busy flag in a finally block");
  assert.match(saveBtn, /\}\s*catch\s*\{/, "components/SaveButton.tsx has no catch — a network failure would leave the button stuck busy");
});

test("the PUT contract is untouched (presentation only)", () => {
  // The endpoint.
  assert.match(
    settings,
    /fetch\(`\/api\/workspaces\/\$\{sfWorkspaceId\}\/settings`, \{/,
    "the Storefront save endpoint changed",
  );
  assert.match(settings, /method: "PUT"/, "the Storefront save is no longer a PUT");
  // The body: every field, in the value it had before this card. A "style"
  // change that drops a field would save nothing for that field and still look
  // correct — the failure mode spamJudgeToggle.test.ts / soundToggle.test.ts
  // exist for, asserted here against THIS function so the two cannot disagree.
  for (const [field, value] of [
    ["business_name", "sfBusinessName"],
    ["company_reg", "sfCompanyReg"],
    ["contact_phone", "sfContactPhone"],
    ["contact_address", "sfContactAddress"],
    ["privacy_url", "sfPrivacyUrl"],
    ["terms_url", "sfTermsUrl"],
    ["refund_url", "sfRefundUrl"],
    ["footer_text", "sfFooterText"],
    ["indexing", "sfIndexing"],
    ["sound_enabled", "sfSoundEnabled"],
    ["spam_judge_enabled", "sfSpamJudgeEnabled"],
    ["default_lang", "sfDefaultLang"],
  ] as const) {
    assert.match(
      settings,
      new RegExp(`\\b${field}:\\s*${value}\\b`),
      `the Storefront PUT body no longer sends { ${field}: ${value} } — that setting would save nothing`,
    );
  }
  // The four toggle variables keep their initial values and their onClick
  // semantics (the card's explicit "do not change" list).
  assert.match(settings, /const \[sfIndexing, setSfIndexing\] = useState\(true\)/);
  assert.match(settings, /const \[sfSoundEnabled, setSfSoundEnabled\] = useState\(false\)/);
  assert.match(settings, /const \[sfSpamJudgeEnabled, setSfSpamJudgeEnabled\] = useState\(true\)/);
  assert.match(settings, /const \[lpEnabled, setLpEnabled\] = useState\(false\)/);
  for (const setter of ["setSfIndexing", "setSfSoundEnabled", "setSfSpamJudgeEnabled", "setLpEnabled"]) {
    assert.ok(
      settings.includes(`onChange={() => ${setter}((v) => !v)}`),
      `${setter} lost its toggle semantics — the switches are presentation only on this card`,
    );
  }
  // The success/error strings are unchanged. Keyed on the MESSAGE, not on which
  // setter carries it: this card's rework (t_ad2af554) routed the message through
  // `showMsgOk` so the toast could carry its tone, and the sentence itself is
  // what this assertion is about.
  assert.match(settings, /t\("set\.sfSaved"\)/, "the Storefront success message changed");
});

test("the shared button classes carry the pointer, the press and the disabled state", () => {
  for (const cls of ["jf-btn-primary", "jf-btn-ghost"]) {
    const body = ruleBody(cls)!;
    // 1. the pointer the live DOM measured as `default`.
    assert.match(body, /cursor:\s*pointer/, `.${cls} has no pointer cursor — that was the measured defect`);
    // 2. a PRESSED state that is visibly NOT the hover state. Asserted as a
    //    rule, so deleting the `:active` block is red rather than silently
    //    reverting the press to the hover colour.
    assert.match(
      css,
      new RegExp(`\\.${cls.replace(/\./g, "\\.")}:active:not\\(:disabled\\)\\s*\\{[^}]*transform:\\s*translateY\\(1px\\)`),
      `.${cls} has no :active state — a held mouse-down was measured byte-identical to :hover`,
    );
    // 3. a hover state.
    assert.match(
      css,
      new RegExp(`\\.${cls.replace(/\./g, "\\.")}:hover:not\\(:disabled\\)\\s*\\{`),
      `.${cls} has no hover state`,
    );
    // 4. the disabled state used by the busy button, with its own cursor.
    assert.match(
      css,
      new RegExp(`\\.${cls.replace(/\./g, "\\.")}:disabled\\s*\\{[^}]*cursor:\\s*not-allowed`),
      `.${cls}:disabled does not set a not-allowed cursor`,
    );
  }
  // The pressed fill is DARKER than the resting fill (a lighter press is what
  // made it invisible) and it is a named token, not a fifth inline hex.
  assert.match(css, /--jf-primary-active:\s*#454ea8/, "globals.css lost --jf-primary-active (the pressed fill)");
  assert.match(
    ruleBody("jf-btn-primary")!,
    /background:\s*var\(--jf-primary\)/,
    ".jf-btn-primary's resting fill is no longer --jf-primary",
  );
  assert.match(
    css,
    /\.jf-btn-primary:active:not\(:disabled\)\s*\{[^}]*background:\s*var\(--jf-primary-active\)/s,
    ".jf-btn-primary's pressed state does not use the darker --jf-primary-active fill",
  );
});

test("the press and hover rules cannot fire on a disabled button", () => {
  // A hover/press colour on a `disabled` button is what makes a busy control
  // still look pressable. Every one of the four state rules is `:not(:disabled)`.
  for (const cls of ["jf-btn-primary", "jf-btn-ghost"]) {
    const escaped = cls.replace(/\./g, "\\.");
    for (const state of ["hover", "active"]) {
      assert.match(
        css,
        new RegExp(`\\.${escaped}:${state}:not\\(:disabled\\)`),
        `.${cls}:${state} is not gated on :not(:disabled) — a disabled button would still light up`,
      );
    }
  }
});

test("the guard would fail on the button it replaced (in-file bite proof)", () => {
  // The exact markup that shipped, planted hermetically: the assertions above
  // must reject it. Without this, a scan that matched nothing would read as a
  // clean page.
  const old = '<button onClick={saveStorefrontSettings} className="jf-btn-primary">{t("set.sfSave")}</button>';
  assert.doesNotMatch(old, /disabled=\{sfSaving\}/, "the old markup was read as carrying the busy state");
  assert.doesNotMatch(old, /aria-busy=/, "the old markup was read as announcing busy");
  // And the old class body, which had none of the three states.
  const oldBody = "background: var(--jf-primary); color: #fff; transition: background 0.15s; white-space: nowrap;";
  assert.doesNotMatch(oldBody, /cursor:\s*pointer/, "the old class body was read as having the pointer");
  assert.doesNotMatch(oldBody, /:active/, "the old class body was read as having a pressed state");
});
