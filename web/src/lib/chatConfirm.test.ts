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
import { WORKSPACE_SCOPE_REQUIRED_CODE, PROPOSAL_ARGS_INVALID_CODE, ROLE_MANAGE_REQUIRED_CODE, confirmDecisionFor } from "./chatConfirm.ts";
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

// ── THE UNEXECUTABLE CARD (t_623676d1) ────────────────────────────────────────
//
// THE DEFECT. A proposal whose arguments the target tool would never accept — the
// model keyed the object `id` while update_product requires `product_id`, which is
// what the chat's READ tools return — rendered a card naming NO object, and Confirm
// then answered a bare 400 ("The change could not be applied. Please try again.")
// AFTER spending the token. The user was told to retry a card that can never work,
// over a token that was already gone.
//
// WHAT THE FIX DOES, and what this file pins by EXECUTING it:
//   * the refusal now carries its own code (`chat.proposalArgsInvalid`) on a 409;
//   * confirmDecisionFor maps it to its OWN outcome — "unexecutable" — rather than
//     "stale", because the card neither expired nor was used (the token is UNSPENT)
//     and its remedy sentence has to say so;
//   * the page gives the card its own badge and does NOT render chat.confirmFail
//     for it, so it cannot tell the user to "try again".
test("a 409 for an unexecutable card is its own outcome, not 'stale'", () => {
  const unexecutableBody = {
    error: "cadangan itu tidak boleh dilaksanakan — minta pembantu menyediakan kad baharu",
    code: "chat.proposalArgsInvalid",
  };
  // (a) THE MAPPING, executed. A spent/expired 409 keeps "stale"; this one does not.
  assert.equal(
    confirmDecisionFor(409, unexecutableBody),
    "unexecutable",
    "a 409 naming the unexecutable code is reported as 'stale' — the user is told the card 'expired or " +
      "was already used', which is false of it (the token is unspent), and never learns a fresh card is all they need",
  );
  // (b) THE BITE. With the code test reduced to the bare status (the regression
  //     shape), this case flips to "stale" and fails by NAME — which is what makes
  //     the guard meaningful rather than a moved string.
  assert.notEqual(
    confirmDecisionFor(409, unexecutableBody),
    "stale",
    "the unexecutable 409 fell through to the spent/expired outcome",
  );
  // (c) The DEFAULT 409s are unchanged: no code, the spent-class code itself, and a
  //     code this build does not know all stay "stale". The branch is not a wildcard.
  assert.equal(confirmDecisionFor(409, null), "stale");
  assert.equal(confirmDecisionFor(409, {}), "stale");
  assert.equal(confirmDecisionFor(409, { code: "request.conflict" }), "stale");
  assert.equal(confirmDecisionFor(409, { code: "chat.someFutureRefusal" }), "stale");
  // (d) A near-miss prefix must not match, the same rule the scope code follows.
  assert.equal(confirmDecisionFor(409, { code: "chat.proposalArgsInvalid.extra" }), "stale");
  // (e) The unexecutable code is NOT a 400: it is decided by the code on a 409 only,
  //     so it can never be confused with the scope refusal on this route.
  assert.equal(confirmDecisionFor(400, unexecutableBody), "failed");
});

test("the unexecutable code is cross-language, mapped, and defined in every locale", () => {
  // The page's constant must equal the server's declaration — read out of the Go
  // source, so a rename on the server fails HERE instead of silently sending this
  // refusal back to the spent/expired sentence.
  const server = goCodeValue("CodeProposalArgsInvalid");
  assert.ok(
    server,
    "errcode.go declares no CodeProposalArgsInvalid — the unexecutable refusal has no code of its own, so " +
      "the page is back to telling it apart from a spent token with the bare 409",
  );
  assert.equal(
    PROPOSAL_ARGS_INVALID_CODE,
    server,
    `lib/chatConfirm.ts's PROPOSAL_ARGS_INVALID_CODE is ${JSON.stringify(PROPOSAL_ARGS_INVALID_CODE)} but ` +
      `errcode.go declares ${JSON.stringify(server)}`,
  );
  // The handler's wire literal must be the table key: errcode.go looks the code up
  // BY MESSAGE, so a reworded refusal would keep answering 409 and lose its code.
  const tableRow = new RegExp(
    `"${server?.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}"\\s*:\\s*CodeProposalArgsInvalid`,
  );
  // (the row is keyed by the MESSAGE, whose value is the code, so the direction above
  //  is code→constant; the message→code direction is pinned on the Go side.)
  const codeConst = new RegExp(`CodeProposalArgsInvalid\\s*=\\s*"${server}"`);
  assert.ok(
    codeConst.test(errcodeGo),
    "errcode.go declares CodeProposalArgsInvalid with a different value than the page mirrors",
  );
  void tableRow;
  // Mapped by the SHARED table, to a sentence that is NOT the spent/expired one.
  assert.ok(
    (server as string) in API_ERROR_KEYS,
    `${server} has no entry in API_ERROR_KEYS — translateApiErr cannot resolve the refusal and the page ` +
      `falls back to its generic sentence`,
  );
  const mapped = API_ERROR_KEYS[server as string];
  assert.notEqual(
    mapped,
    "chat.proposalStale",
    `${server} maps to chat.proposalStale — the unexecutable card would reuse the "expired or was already ` +
      `used" sentence, which is false of a card whose token is UNSPENT`,
  );
  const entry = (DICT as Record<string, Record<string, unknown>>)[mapped];
  assert.ok(entry, `the dictionary has no ${mapped} — the card would render a raw key`);
  for (const lang of LANGS) {
    const v = entry[lang];
    assert.ok(
      typeof v === "string" && v.trim() !== "",
      `${mapped}.${lang} is empty — that locale silently shows English`,
    );
  }
  // And it must not be the same string as the spent/expired sentence in ANY locale,
  // or the "does not claim expiry" property would be a distinction without a difference.
  const stale = (DICT as Record<string, Record<string, unknown>>)["chat.proposalStale"];
  for (const lang of LANGS) {
    assert.notEqual(entry[lang], stale[lang], `${mapped}.${lang} is the spent/expired sentence verbatim`);
  }
});

test("the unexecutable card claims neither a failure nor an expiry on the page", () => {
  const decide = chatPage.slice(chatPage.indexOf("async function decide("));
  // The page handles the outcome and marks the card with its own state.
  assert.match(
    decide,
    /if \(outcome === "unexecutable"\)[\s\S]{0,900}?setState\(msgIndex,\s*p\.token,\s*"unexecutable"\)/,
    "the page has no unexecutable outcome branch, or it does not mark the card with its own state — the " +
      "card would fall to the generic failure branch and read 'Did not go through' / 'try again' for a " +
      "condition where nothing was attempted and the token is still live",
  );
  // THE SENTENCE IS RENDERED ONCE — the alert, exactly like the scope refusal.
  const copySites = [...decide.matchAll(/setErr\(t\("chat\.proposalUnexecutable"\)\)/g)];
  assert.equal(
    copySites.length,
    1,
    `decide() renders the unexecutable copy at ${copySites.length} site(s), want exactly 1 — the card footer ` +
      `and the alert must not both carry it`,
  );
  // No chat.confirmFail anywhere near the unexecutable state: the failing sentence
  // tells the user to retry, and this card can never succeed as shown.
  const badFooter = new RegExp('state === "unexecutable"[\\s\\S]{0,300}?chat\\.confirmFail');
  assert.ok(
    !badFooter.test(chatPage),
    "the unexecutable card renders chat.confirmFail — it would tell the user to retry an action that is " +
      "guaranteed to fail",
  );
  // And the badge is its own: not the spent/expired one and not the failure one.
  assert.match(
    chatPage,
    /state === "unexecutable"[\s\S]{0,120}?chat\.proposalBadgeUnexecutable/,
    "the unexecutable card does not wear its own badge — it would read as an expiry or a failure",
  );
});

// ── THE ROLE REFUSAL ON THE CONFIRM PATH (t_1900adef) ────────────────────────
//
// THE DEFECT, measured on live prod (v1.19.1) by the QA lane. A read_only member
// of a workspace confirmed a prepared change. The write-role gate refused it AFTER
// the token was claimed, the wire answered
//
//     400 {"code":"request.badRequest","error":"cadangan tidak dapat dilaksanakan"}
//
// and the page rendered ONE sentence TWICE, naming no cause and advising a retry:
//
//     card footer : "The change could not be applied. Please try again."
//     role=alert  : "The change could not be applied. Please try again."
//
// Three separate things were wrong, and this file pins the fix for each:
//
//   1. THE SERVER THREW THE CODE AWAY. errcode.go keys its code by wire MESSAGE,
//      and it keyed this refusal as the exact literal the REST edge writes
//      ("perlu peranan manage atau admin") while the write gate emits one word
//      longer ("… dalam workspace ini"), so ErrCodeFor answered "" and writeErr
//      re-coded the 400 as the untranslatable class code. Fixed on the Go side
//      (a lead-in rule, pinned by chat_role_refusal_errcode_test.go); what THIS
//      file pins is the client half — the page's constant must equal the server's
//      declaration, read out of the Go source, and the code must be mapped.
//   2. THE PAGE COULD NOT NAME THE CAUSE. It can now, because the answer carries
//      the code: `roleDenied` is its own outcome with its OWN sentence.
//   3. IT RENDERED THE SENTENCE TWICE, over a SPENT token, advising a retry that
//      cannot work. The new state carries its copy exactly once (the alert) and
//      renders chat.confirmFail nowhere near it.
test("a 400 naming the role code is its own outcome, never the generic failure", () => {
  const roleBody = {
    // The gate's own wire prose, exactly as the confirm route surfaces it
    // (api/internal/mcpsrv's RoleManageRequiredMessage).
    error: "perlu peranan manage atau admin dalam workspace ini",
    code: "role.manageRequired",
  };
  // (a) THE MAPPING, executed.
  assert.equal(
    confirmDecisionFor(400, roleBody),
    "roleDenied",
    "a 400 carrying role.manageRequired is not recognised — the card falls to the generic failure " +
      "branch, which names no cause and tells the user to retry a card the server has already spent " +
      "(t_1900adef)",
  );
  // (b) THE BITE. With the code test reduced to the bare status — the regression
  //     shape this route's history is made of — this case flips to something else
  //     and fails by NAME. Both wrong answers are excluded explicitly: `failed` is
  //     the copy-with-a-retry and `scopeRequired` is the pre-fix behaviour.
  for (const wrong of ["failed", "scopeRequired"]) {
    assert.notEqual(
      confirmDecisionFor(400, roleBody),
      wrong,
      `the role refusal is reported as "${wrong}" — the user either gets a cause they cannot see and a ` +
        `retry that cannot work, or advice to choose a workspace they already had selected`,
    );
  }
  // (c) The OTHER 400s are unchanged: this branch is not a wildcard.
  assert.equal(confirmDecisionFor(400, { ...roleBody, code: "request.badRequest" }), "failed");
  assert.equal(confirmDecisionFor(400, { ...roleBody, code: "" }), "failed");
  assert.equal(confirmDecisionFor(400, { error: roleBody.error }), "failed", "the PROSE must not decide");
  assert.equal(confirmDecisionFor(400, { code: "role.manageRequired.extra" }), "failed");
  assert.equal(confirmDecisionFor(400, { code: "role.adminRequired" }), "failed", "a near-miss code");
  // (d) The two named 400s stay distinct: the scope refusal must not become a role
  //     refusal, and vice versa.
  assert.equal(confirmDecisionFor(400, scopeRefusalBody), "scopeRequired");
  assert.equal(
    confirmDecisionFor(400, { code: WORKSPACE_SCOPE_REQUIRED_CODE }),
    "scopeRequired",
    "the scope branch stopped working once the role branch was added",
  );
});

test("the role code is cross-language, mapped, and defined in every locale", () => {
  // Read out of the Go source, so a rename on the server fails HERE rather than
  // silently sending this refusal back down the generic path.
  const server = goCodeValue("CodeRoleManageRequired");
  assert.ok(
    server,
    "errcode.go declares no CodeRoleManageRequired — the role refusal has no code, so the page is back " +
      "to printing its generic failure copy for it",
  );
  assert.equal(
    ROLE_MANAGE_REQUIRED_CODE,
    server,
    `lib/chatConfirm.ts's ROLE_MANAGE_REQUIRED_CODE is ${JSON.stringify(ROLE_MANAGE_REQUIRED_CODE)} but ` +
      `errcode.go declares ${JSON.stringify(server)}`,
  );
  // Mapped by the SHARED table. It is reused (the REST edge has answered this code
  // for years) rather than added, so this is a check that the mapping is still
  // there — the confirm path is a second consumer of it.
  assert.ok(
    (server as string) in API_ERROR_KEYS,
    `${server} has no entry in API_ERROR_KEYS — translateApiErr cannot resolve a refusal the server ` +
      `named precisely`,
  );
  // The page's OWN sentence exists, in every locale, and is not the generic failure
  // copy in any of them: the point of the split is that they say different things.
  const own = (DICT as Record<string, Record<string, unknown>>)["chat.confirmRoleDenied"];
  assert.ok(own, "the dictionary has no chat.confirmRoleDenied — the card would render a raw key");
  const generic = (DICT as Record<string, Record<string, unknown>>)["chat.confirmFail"];
  for (const lang of LANGS) {
    const v = own[lang];
    assert.ok(
      typeof v === "string" && v.trim() !== "",
      `chat.confirmRoleDenied.${lang} is empty — that locale silently shows English`,
    );
    assert.notEqual(
      own[lang],
      generic[lang],
      `chat.confirmRoleDenied.${lang} is the generic failure sentence verbatim`,
    );
    // THE RETRY. The refusal arrives after the token is claimed, so a sentence that
    // tells the user to try again is false in every language.
    const retry: Record<string, RegExp> = {
      en: /try again/i,
      ms: /cuba lagi/i,
      zh: /重试/,
      ta: /மீண்டும் முயற்சிக்கவும்/,
      ar: /يرجى المحاولة/,
    };
    assert.ok(
      !retry[lang].test(v as string),
      `chat.confirmRoleDenied.${lang} tells the user to try again ("${v}") — the token is spent, so ` +
        `retrying the same card cannot work`,
    );
  }
});

test("the role-denied card carries its sentence ONCE and never the retry copy", () => {
  const decide = chatPage.slice(chatPage.indexOf("async function decide("));
  // The page handles the outcome and marks the card with its own state.
  assert.match(
    decide,
    /if \(outcome === "roleDenied"\)[\s\S]{0,1800}?setState\(msgIndex,\s*p\.token,\s*"roleDenied"\)/,
    "the page has no roleDenied outcome branch, or it does not mark the card with its own state — the " +
      "card would fall to the generic failure branch and read 'Did not go through' / 'try again' over a " +
      "token the server already spent (the measured defect)",
  );
  // THE SENTENCE IS RENDERED ONCE — the alert, exactly like the scope and
  // unexecutable refusals.
  const copySites = [...decide.matchAll(/setErr\(t\("chat\.confirmRoleDenied"\)\)/g)];
  assert.equal(
    copySites.length,
    1,
    `decide() renders the role-denied copy at ${copySites.length} site(s), want exactly 1 — the card ` +
      `footer and the alert must not both carry it, which is the duplication QA measured on prod`,
  );
  // AND NOTHING ELSE CARRIES IT. The card body has NO render site for this state at
  // all: the badge ternary falls through to the failure pill and no footer is keyed
  // on it. That is what makes the copy exactly one node — the count QA measures —
  // rather than one sentence written twice.
  assert.ok(
    !/state === "roleDenied"/.test(chatPage),
    "the role-denied state has a JSX render site (a footer or a dedicated badge arm). The copy belongs " +
      "to the page's single role=\"alert\", like the scope and unexecutable refusals; a card-level " +
      "sentence puts the same words on screen twice, which is the defect (t_1900adef)",
  );
  // The pill the card therefore wears is the FAILURE one, deliberately: unlike the
  // scope refusal, this write really was attempted and really did not go through.
  assert.match(
    chatPage,
    /chat\.proposalBadgeScopeRequired[\s\S]{0,600}?t\("chat\.proposalBadgeFailed"\)/,
    "the badge ternary's final arm is not chat.proposalBadgeFailed — the role-denied card would wear " +
      "no pill (or an unrelated one) over a change that did not go through",
  );
  // The state exists as a member, or the setState above would not type-check.
  assert.match(
    chatPage,
    /type ProposalState\s*=[\s\S]{0,600}?"roleDenied"/,
    "ProposalState has no roleDenied member — the setState above would not type-check",
  );
});

