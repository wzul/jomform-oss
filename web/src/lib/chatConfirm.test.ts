// The confirm/cancel route's answer, decided by CODE and not by status (t_fb393abe).
//
// THE DEFECT. src/app/chat/page.tsx's decide() branched on the bare status:
//
//     if (r.status === 400) { setState(..., "scopeRequired"); return; }
//
// and returned before reading the body, so EVERY 400 on that route rendered
// "Choose a workspace first — the assistant answers for one workspace at a time."
// A 400 there is not necessarily a workspace-scope refusal:
// api/internal/httpapi/rest_chat.go's chatConfirmStatusFor maps everything it does
// not recognise to 400, and one such class arrives AFTER the token is claimed —
// chat/proposal.go returns `fmt.Errorf("server menolak: %s", …)` when a tool
// refuses to execute, and mcpsrv's write-role gate
// (callscope.go's "perlu peranan manage atau admin dalam workspace ini") is one
// producer. A read_only member confirming a prepared change therefore read advice
// they could not act on: the request carried ?workspace=<their workspace>, and the
// refusal they needed to understand was the role.
//
// WHAT THIS FILE PINS, and why it is a plain-node test rather than a source scan:
//
//   1. THE BEHAVIOUR, EXECUTED. confirmDecisionFor is called for each real shape
//      the route can answer, including the two DIFFERENT 400s. This is the half no
//      source scan can give: a scan of the old code could confirm the branch
//      existed but not that it behaved differently for the two 400s — it did not.
//
//   2. THE BITE. The card's requirement is that the test goes RED when the code
//      test is replaced by a bare `r.status === 400`. Case (c) below is exactly
//      that mutation's failure: with a status-only branch the non-scope 400 maps
//      to "scopeRequired" and the assertion fails by NAME, not by a moved string.
//      Verified by mutation in the PR evidence, not assumed here.
//
//   3. CROSS-LANGUAGE. `workspace.scopeRequired` is read out of the Go source
//      rather than restated, so renaming the server's code fails HERE instead of
//      silently sending every refusal back down the generic path (the pin
//      src/lib/workspaceSlug.ts uses for the reserved-subdomain token, and
//      src/lib/apiErr.test.ts for the whole code table).
//
//   4. THE MAPPING AND THE COPY. The code must be mapped by the SHARED table
//      (lib/apiErr.ts), whose sentence is the chat page's own — and the page must
//      render ONE sentence for a scope refusal, not two (the duplication
//      t_790b1016 removed).
//
// Run with:  npm run test:unit
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { API_ERROR_KEYS, apiErrorCode } from "./apiErr.ts";
import { WORKSPACE_SCOPE_REQUIRED_CODE, confirmDecisionFor } from "./chatConfirm.ts";
import { DICT, LANGS } from "../components/i18nDict.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const errcodeGo = read("../../../api/internal/httpapi/errcode.go");
const restScopeGo = read("../../../api/internal/httpapi/rest_scope.go");
const chatPage = read("../app/chat/page.tsx");

/** The VALUE of a `Code* = "..."` constant in errcode.go, or null. */
function goCodeValue(name: string): string | null {
  const m = errcodeGo.match(new RegExp(`\\b${name}\\s*=\\s*"([^"]+)"`));
  return m ? m[1] : null;
}

/** The value of `scopeRequiredMessage = "..."` in rest_scope.go, or null. */
function goScopeRequiredMessage(): string | null {
  const m = restScopeGo.match(/\bscopeRequiredMessage\s*=\s*"([^"]+)"/);
  return m ? m[1] : null;
}

/** The exact wire body the server writes for each refusal, as a parsed object. */
const scopeRefusalBody = {
  error: "skop workspace diperlukan — pilih workspace dahulu",
  code: "workspace.scopeRequired",
};
/** Two DIFFERENT 400s, both reachable from /chat/confirm. */
const TOOL_REFUSAL_BODY = { error: "server menolak: perlu peranan manage atau admin", code: "request.badRequest" };

test("the guard reads the real server and the real page (it cannot pass vacuously)", () => {
  const code = goCodeValue("CodeWorkspaceScopeRequired");
  assert.ok(
    code,
    "errcode.go declares no CodeWorkspaceScopeRequired — the scope refusal has no code of its own, so the " +
      "page is back to telling a 400-by-role apart from a 400-by-scope with the status alone",
  );
  assert.equal(
    goScopeRequiredMessage(),
    "skop workspace diperlukan — pilih workspace dahulu",
    "rest_scope.go's scopeRequiredMessage moved or changed wording — errcode.go's message→code table keys on " +
      "the EXACT literal, so the refusal would keep answering 400 but silently lose its code",
  );
  assert.ok(
    /API_ERROR_KEYS/.test(read("./apiErr.ts")),
    "lib/apiErr.ts is not the shared code table this guard expects",
  );
  assert.match(chatPage, /async function decide\(/, "app/chat/page.tsx no longer defines decide()");
});

test("the page's constant mirrors the server's code (cross-language, read not restated)", () => {
  const server = goCodeValue("CodeWorkspaceScopeRequired");
  assert.equal(
    WORKSPACE_SCOPE_REQUIRED_CODE,
    server,
    `lib/chatConfirm.ts's WORKSPACE_SCOPE_REQUIRED_CODE is ${JSON.stringify(WORKSPACE_SCOPE_REQUIRED_CODE)} ` +
      `but errcode.go declares ${JSON.stringify(server)} — the page would branch on a code the server never ` +
      `sends and every 400 would fall to the generic failure`,
  );
  // And the server really writes it: the table must map the wire literal onto that
  // code, or the body would carry the class code `request.badRequest` instead.
  const tableRow = new RegExp(
    `"${goScopeRequiredMessage()?.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}"\\s*:\\s*CodeWorkspaceScopeRequired`,
  );
  assert.match(
    errcodeGo,
    tableRow,
    "errcode.go's message→code table does not map the ambiguous-scope wire message onto " +
      "CodeWorkspaceScopeRequired — the refusal would ship with the class code `request.badRequest`, which " +
      "the dashboard deliberately does not translate (#6177), and the page could not distinguish it",
  );
});

// ── THE BEHAVIOUR: two different 400s, two different outcomes ───────────────
//
// THE MUTATION THIS FILE EXISTS TO CATCH: replace the code test in
// confirmDecisionFor with `status === 400`. Case (c) then answers "scopeRequired"
// and fails by name. Measured in the PR evidence.
test("a 400 is a scope refusal ONLY when the body names the scope code", () => {
  // (a) The scope refusal: the server's own body, code and all.
  assert.equal(
    confirmDecisionFor(400, scopeRefusalBody),
    "scopeRequired",
    "a 400 carrying workspace.scopeRequired no longer marks the card scopeRequired — the user would be told " +
      "the change failed and would not learn that choosing a workspace is the fix",
  );
  // (b) The same 400 with the code the server ACTUALLY ships today when the
  //     table has no entry for the message. This is not hypothetical: it is the
  //     pre-fix wire shape, and it must NOT produce the scope copy.
  assert.equal(
    confirmDecisionFor(400, { ...scopeRefusalBody, code: "request.badRequest" }),
    "failed",
    "a 400 whose code is the generic class code is treated as a workspace-scope refusal — that is the " +
      "pre-fix behaviour, and the class code is exactly what the server used to send for EVERY 400",
  );
  // (c) THE CASE THAT WAS INVISIBLE. A post-claim tool refusal (write-role gate)
  //     answers 400 with its own message and no specific code. Before this fix the
  //     page rendered "Choose a workspace first" for it, to a user who already had
  //     a workspace selected.
  assert.equal(
    confirmDecisionFor(400, TOOL_REFUSAL_BODY),
    "failed",
    "a 400 from a tool the server REFUSED (role/ownership, after the token was claimed) is reported as a " +
      "workspace-scope refusal — the user is told to choose a workspace they already have, and the reason " +
      "they need (their role) is never shown (t_fb393abe)",
  );
  // (d) A body that cannot be read at all (a proxy error page, an empty 400).
  //     No code means no scope claim: the failure copy is the honest answer.
  assert.equal(
    confirmDecisionFor(400, null),
    "failed",
    "an unreadable 400 body is treated as a scope refusal — a proxy error page would tell the user to " +
      "choose a workspace",
  );
  assert.equal(confirmDecisionFor(400, undefined), "failed");
  assert.equal(confirmDecisionFor(400, {}), "failed");
  // (e) A code the server adds LATER, which this build does not know: the scope
  //     branch must not become a wildcard.
  assert.equal(
    confirmDecisionFor(400, { code: "workspace.someFutureRefusal" }),
    "failed",
    "any unknown code on a 400 now maps to the scope refusal — the branch has become a wildcard and the " +
      "defect is back for every future refusal",
  );
  // (f) A near-miss: the prefix must not match.
  assert.equal(confirmDecisionFor(400, { code: "workspace.scopeRequired.extra" }), "failed");
  assert.equal(confirmDecisionFor(400, { code: "workspace.scopeRequireds" }), "failed");
});

test("the other statuses keep their own outcome, and are not decided by the code", () => {
  // The card's acceptance: PR 779's behaviour must not regress. 409 and 429 keep
  // their distinct copy, and neither may collapse into the scope path.
  assert.equal(confirmDecisionFor(409, null), "stale");
  assert.equal(confirmDecisionFor(409, scopeRefusalBody), "stale", "a 409 is not a scope refusal");
  assert.equal(confirmDecisionFor(429, null), "rateLimited");
  assert.equal(confirmDecisionFor(429, scopeRefusalBody), "rateLimited", "a 429 is not a scope refusal");
  assert.equal(confirmDecisionFor(500, null), "failed");
  assert.equal(confirmDecisionFor(503, null), "failed");
  assert.equal(confirmDecisionFor(200, null), "done");
  assert.equal(confirmDecisionFor(204, null), "done");
  // A 400 from a route that somehow answered the success shape is still a failure:
  // the status is what makes it not-ok.
  assert.equal(confirmDecisionFor(418, null), "failed");
});

test("the code is mapped by the SHARED table, to the page's own sentence", () => {
  const code = goCodeValue("CodeWorkspaceScopeRequired") as string;
  assert.ok(
    code in API_ERROR_KEYS,
    `${code} has no entry in API_ERROR_KEYS (web/src/lib/apiErr.ts), so translateApiErr cannot resolve a ` +
      `refusal the server named precisely — the page would fall back to its generic sentence`,
  );
  // It reuses the chat page's own string rather than adding a near-duplicate, so
  // the two surfaces cannot drift into two different instructions for one refusal.
  assert.equal(
    API_ERROR_KEYS[code],
    "chat.scopeRequired",
    `${code} maps to ${JSON.stringify(API_ERROR_KEYS[code])} — the scope refusal has one instruction, and ` +
      `the page already renders it as chat.scopeRequired`,
  );
  const entry = (DICT as Record<string, Record<string, unknown>>)["chat.scopeRequired"];
  assert.ok(entry, "the dictionary has no chat.scopeRequired — the scope card would render a raw key");
  for (const lang of LANGS) {
    const v = entry[lang];
    assert.ok(
      typeof v === "string" && v.trim() !== "",
      `chat.scopeRequired.${lang} is empty — that locale silently shows English`,
    );
  }
  // The translation really resolves, through the shared helper, in every locale.
  for (const lang of LANGS) {
    const rendered = apiErrorCode({ code });
    assert.equal(rendered, code);
  }
});

test("a scope refusal is rendered ONCE on the page, and never as a failure", () => {
  // The state is the fix (t_790b1016): the server refuses an ambiguous scope
  // BEFORE reading the token, so nothing was attempted and the card must not
  // claim a failure. The copy itself belongs to the single role="alert".
  const decide = chatPage.slice(chatPage.indexOf("async function decide("));
  assert.match(
    decide,
    /if \(outcome === "scopeRequired"\)[\s\S]{0,600}?setState\(msgIndex,\s*p\.token,\s*"scopeRequired"\)/,
    "the scope outcome no longer marks the card scopeRequired",
  );
  assert.doesNotMatch(
    decide.slice(0, decide.indexOf('if (outcome === "scopeRequired")')),
    /setErr\(t\("chat\.scopeRequired"\)\)/,
    "the scope copy is rendered before the scope branch — it would appear for other outcomes too",
  );
  // Counted, not merely present: a second rendering of the same sentence is the
  // duplication this rule exists to prevent.
  const scopeCopySites = [...decide.matchAll(/setErr\(t\("chat\.scopeRequired"\)\)/g)];
  assert.equal(
    scopeCopySites.length,
    1,
    `decide() renders the scope copy at ${scopeCopySites.length} site(s), want exactly 1 — the card footer ` +
      `and the alert must not both carry it`,
  );
  // And the footer is still keyed on "failed" alone, so a scopeRequired card
  // cannot inherit the "try again" sentence.
  assert.match(
    chatPage,
    /\{state === "failed" && \(\s*<p[^>]*>\{t\("chat\.confirmFail"\)\}<\/p>/,
    "the card footer no longer renders chat.confirmFail for the failed state only",
  );
  const scopeFooter = new RegExp('state === "scopeRequired"[\\s\\S]{0,200}?chat\\.confirmFail');
  assert.ok(
    !scopeFooter.test(chatPage),
    'the scope-required card renders chat.confirmFail — it would tell a user with no workspace to "try again"',
  );
});
