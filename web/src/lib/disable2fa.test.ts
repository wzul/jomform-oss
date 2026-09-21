// Guard for the reported defect: the Profile → Security "Disable 2FA" control
// asked for one thing and sent another.
//
// THE BUG (as shipped in v1.10.0, measured on the live web chunk):
//
//     let t = window.prompt(e("prof.enter2faCode"));   // "Enter a current 2FA code…"
//     t && await ex("/api/auth/2fa/disable", { password: t })
//
// The prompt asks for the 6-digit code from the authenticator app, and the
// answer was posted as `password` — a field the backend verifies against the
// account's password hash. A magic-link user has never typed that password
// (a magic-link signup gets a random UUID), so disabling 2FA was impossible for
// them, and anyone else reading the label typed their code and was rejected.
//
// WHAT THIS PINS (each one is a way the defect comes back)
//   1. The prompt on the disable control is the 2FA-code prompt, and the answer
//      is sent as `totp_code` — never as `password`. This is the whole fix, so
//      it is asserted on the page SOURCE: the page cannot regain the old shape.
//   2. The prompt is asked exactly once for that action. Asking twice inside one
//      submit burns two codes from the same 30s window (#528 makes each code
//      single-use), which is what made every retry fail in the report.
//   3. The step-up predicate has ONE definition: the page calls
//      stepUpRequired()/isValidStepUpCode() from lib/stepUp.ts rather than
//      restating `magic_session && totp_enabled` inline (a second copy is how
//      the two sides drift apart again).
//   4. The change-password control still carries its password AND the code —
//      the #458 prompt there is additive, not a replacement.
//   5. The docs describe the behaviour a user will meet (AGENTS.md docs-parity).
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * It cannot click. The rendered prompt text and the actual request body are
//     driven in a real browser against a production build — reported in the
//     card handoff, not here.
//   * It cannot prove the Go handler consumes `totp_code`; that is the api-side
//     test (disable2fa_reported_case_test.go).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { isValidStepUpCode, stepUpRequired } from "./stepUp.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const PAGE = read("../app/profile/page.tsx");
const DOCS = read("../app/docs/page.tsx");

// The disable control's onClick handler, sliced out so the assertions below are
// about the disable action specifically and cannot be satisfied by an unrelated
// call site (change-password also posts to /api/auth/*).
function disableHandlerSource(): string {
  const start = PAGE.indexOf('"/api/auth/2fa/disable"');
  assert.ok(start > 0, "app/profile/page.tsx no longer calls /api/auth/2fa/disable");
  return PAGE.slice(start, start + 900);
}

test("the disable control asks for a 2FA code and sends it as totp_code", () => {
  const handler = disableHandlerSource();
  // The label the user reads.
  assert.ok(
    handler.includes('t("prof.enter2faCode")'),
    'the disable control no longer prompts with prof.enter2faCode — the label and the ' +
      "field must describe the same value",
  );
  // The field actually sent.
  assert.ok(
    handler.includes("totp_code"),
    'the disable control does not send totp_code — the backend verifies a fresh TOTP ' +
      "code, so the request would be rejected",
  );
  assert.doesNotMatch(
    handler,
    /password\s*:/,
    "the disable control sends a `password` field again — that is the reported defect " +
      "(the prompt asks for a 2FA code, the backend verifies a password)",
  );
  // The old literal shape, in case someone reintroduces it verbatim.
  assert.doesNotMatch(
    PAGE,
    /2fa\/disable"\s*,\s*\{\s*password/,
    'app/profile/page.tsx is back to { password } for /api/auth/2fa/disable',
  );
});

test("the prompt label and the sent field are not swapped anywhere", () => {
  // Every call site that prompts with a label must send a value matching it.
  // The only other prompt on this page is the #458 step-up, which adds
  // totp_code — so `password:` may appear ONLY in the change-password body.
  const passwordsSentWithCodePrompt = [...PAGE.matchAll(/t\("prof\.enter2faCode"\)[^;]{0,200}password\s*:/g)];
  assert.deepEqual(
    passwordsSentWithCodePrompt.map((m) => m[0]),
    [],
    "a prompt labelled `enter a 2FA code` is still feeding a password field",
  );
});

test("disabling asks for the code ONCE per submit", () => {
  const handler = disableHandlerSource();
  // The handler must not prompt directly: the code is collected by
  // postSensitive, which is the single place that knows about the #458 retry.
  const prompts = handler.match(/window\.prompt/g) ?? [];
  assert.deepEqual(prompts, [], "the disable handler prompts directly again — the code is collected by postSensitive");
  // …and it must FORCE its own prompt (opts.prompt), because the disable action
  // takes a code on every session type. Relying on the #458 step-up alone would
  // ask a non-magic user for a password instead.
  assert.match(
    handler,
    /\{\s*prompt:/,
    "the disable control no longer passes a forced prompt to postSensitive — without it the " +
      "action would follow the step-up path and prompt for a password on a non-magic session",
  );
  // The forced prompt is postSensitive's single pre-emptive prompt; assert the
  // helper prompts at most once BEFORE the request and at most once after a 401,
  // so a future edit cannot reintroduce a double prompt for this action.
  const helper = PAGE.slice(PAGE.indexOf("async function postSensitive"), PAGE.indexOf("// Issue #133: upload a profile picture"));
  const helperPrompts = helper.match(/window\.prompt/g) ?? [];
  assert.equal(
    helperPrompts.length,
    2,
    "postSensitive no longer prompts exactly twice (pre-emptive + one 401 retry); a second " +
      "prompt inside one submit needs a second single-use code and fails",
  );
});

test("the step-up predicate has one definition, used by the page", () => {
  assert.ok(
    PAGE.includes('from "../../lib/stepUp"'),
    "app/profile/page.tsx no longer imports the shared step-up helpers",
  );
  assert.ok(
    PAGE.includes("stepUpRequired(") && PAGE.includes("isValidStepUpCode("),
    "the page restates the step-up rule inline instead of calling stepUpRequired/…",
  );
  assert.doesNotMatch(
    PAGE,
    /magic_session\s*&&\s*p\?\.totp_enabled/,
    "the `magic_session && totp_enabled` condition is inline again — that is the copy " +
      "that drifted from the backend",
  );

  // The predicate itself.
  assert.equal(stepUpRequired({ magic_session: true }, { totp_enabled: true }), true);
  assert.equal(stepUpRequired({ magic_session: false }, { totp_enabled: true }), false);
  assert.equal(stepUpRequired({ magic_session: true }, { totp_enabled: false }), false);
  assert.equal(stepUpRequired(null, { totp_enabled: true }), false);
  assert.equal(stepUpRequired({ magic_session: true }, null), false);
  assert.equal(stepUpRequired(undefined, undefined), false);
});

test("a code is required and normalised identically on both prompt paths", () => {
  assert.equal(isValidStepUpCode("123456"), true);
  assert.equal(isValidStepUpCode("  123456 "), true);
  assert.equal(isValidStepUpCode(""), false);
  assert.equal(isValidStepUpCode("   "), false);
  assert.equal(isValidStepUpCode(null), false);
  // The page trims what it sends, so a code typed with stray whitespace works.
  assert.ok(PAGE.includes("entered!.trim()"), "a prompted code is sent untrimmed");
});

test("change-password still sends its password alongside the step-up code", () => {
  const start = PAGE.indexOf('"/api/auth/change-password"');
  assert.ok(start > 0, "the change-password call site is gone");
  const handler = PAGE.slice(start, start + 400);
  assert.ok(handler.includes("password: pwCurrent"), "change-password no longer confirms the current password");
  assert.ok(handler.includes("totp_code"), "change-password lost the #458 step-up code (#458 regressed)");
});

test("the docs describe what a user disabling 2FA will actually be asked for", () => {
  assert.ok(
    DOCS.includes('slug: "two-factor"'),
    "app/docs/page.tsx has no `two-factor` section for the disable flow to point at",
  );
  assert.ok(DOCS.includes("id={f.slug}"), "the docs page does not render section ids, so an anchor would not scroll");
  const start = DOCS.indexOf('slug: "two-factor"');
  const section = DOCS.slice(start, start + 1600);
  assert.ok(
    /6-digit|six-digit/.test(section),
    "the 2FA docs section does not say the code comes from the authenticator app",
  );
  assert.ok(
    /magic[- ]link/i.test(section),
    "the 2FA docs section does not explain the magic-link step-up",
  );
});
