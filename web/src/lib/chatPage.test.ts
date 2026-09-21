// The in-app assistant (phase 1) — the dashboard half of the read-only chat.
//
// WHAT CAN BREAK HERE, and why a source scan is the right guard:
//
//   1. THE BOUNDARY CLAIM DRIFTS FROM THE SERVER. The page tells the user the
//      assistant cannot change their account. That claim is only true if the
//      server dispatches its tools under a read-only scope. So this test reads
//      the Go source and pins the scope line, the route, and the per-user
//      limiter — a page claiming read-only over a server that is not read-only
//      is the worst possible bug for this feature, and it is invisible in every
//      UI-level test.
//   2. THE DOC LINK (AGENTS.md DOCUMENTATION PARITY step 4). The "?" button must
//      point at a docs page that exists and that actually documents the
//      boundary, and the docs sidebar must link it (an unreachable docs page is
//      an undocumented feature).
//   3. THE UNAVAILABLE DIRECTION. A deployment without a usable LLM credential
//      reports available:false and the page must stop offering the box. Offering
//      a box that always errors is worse than no box, and inverting that read
//      turns a feature that is off into one that appears broken. The flag itself
//      must mean "can this deployment answer" — never "was a runner object
//      built", which is true even with no credential at all.
//   4. THE NAV ENTRY, and that /chat exists — a nav link to a missing page is a
//      404 in the sidebar.
//   5. THE FILING IS ANNOUNCED. The card requires the user to SEE a filing
//      happen; if the `filed` branch is dropped from the transcript the thread
//      still appears on /feedback, so nothing else would fail.
//   6. The i18n keys, in all five locales.
//
// WHAT IT DOES NOT COVER (stated honestly): it cannot execute React, so it does
// not observe the rendered transcript, the scroll, or the Enter-to-send key
// handling. The server-side behaviour (read-only refusal, filing, limits) is
// pinned by the Go tests, which is where that behaviour actually lives.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, existsSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { DICT, LANGS } from "../components/i18nDict.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");
const path = (rel: string) => fileURLToPath(new URL(rel, import.meta.url));

const page = read("../app/chat/page.tsx");
const shell = read("../components/Shell.tsx");
const docsNav = read("../app/docs/layout.tsx");
const DOCS_PAGE = "../app/docs/chat/page.tsx";

// The Go side of the same claim, read cross-language on purpose (the idiom of
// salesRows.test.ts / spamJudgeToggle.test.ts): "read-only" is one property
// living in two languages, and a UI that asserts it while the server does not
// enforce it is the failure this guard exists for.
const goChat = read("../../../api/internal/chat/chat.go");
const goChatProposal = read("../../../api/internal/chat/proposal.go");
const goRest = read("../../../api/internal/httpapi/rest_chat.go");
const goRestRoutes = read("../../../api/internal/httpapi/rest.go");
const goChatTools = read("../../../api/internal/mcpsrv/chat_tools.go");

test("the guard is looking at real files (it cannot pass vacuously)", () => {
  assert.ok(page.includes("chat-input"), "app/chat/page.tsx is not the chat page source");
  assert.ok(
    goChat.includes("func ReadOnlyScopes") || goChat.includes("ScopeReadonly"),
    "could not find the read-only scope in api/internal/chat/chat.go",
  );
  assert.ok(
    existsSync(path(DOCS_PAGE)),
    `the docs page the inline "?" points at (${DOCS_PAGE}) does not exist`,
  );
});

test("the server really runs the chat's tools read-only (the UI's claim is not just copy)", () => {
  // The scope the tool calls are dispatched under.
  assert.match(
    goRest,
    /Scopes:\s*chat\.ReadOnlyScopes\(\)/,
    "rest_chat.go no longer builds the identity with chat.ReadOnlyScopes() — the page still " +
      "tells the user the assistant cannot change their account; that claim must stay true",
  );
  assert.match(
    goChat,
    /func ReadOnlyScopes\(\)\s*\[\]string\s*\{\s*return\s*\[\]string\{dbstore\.ScopeReadonly\}/,
    "chat.ReadOnlyScopes no longer returns exactly the readonly scope",
  );
  // Session-only: an API key must not reach the chat.
  assert.ok(
    goRest.includes("sessionOwnerID"),
    "rest_chat.go no longer resolves the caller from the session — a chat belongs to a person",
  );
  // The in-process tools and nothing else.
  assert.match(
    goChatTools,
    /chatReadTools\s*=\s*\[\]string\{/,
    "the chat's read allowlist moved — this guard reads it to prove it contains no writer",
  );
  assert.ok(
    goChatTools.includes("ChatSubmitFeedbackTool") && goChatTools.includes("submit_feedback"),
    "chat_tools.go no longer declares the in-process filing tool (submit_feedback)",
  );
});

test("phase 2: a write is only reachable through a human's confirmation", () => {
  // The confirmed tool runs under a DIFFERENT scope from a message. Both facts are
  // read from the same file so the pair cannot drift: a message read-only, a
  // confirmation the human's write.
  assert.match(
    goRest,
    /Scopes:\s*chat\.WriteScopes\(\)/,
    "rest_chat.go no longer builds a write identity for the confirm path — either the feature " +
      "cannot work at all, or the write scope has been moved onto the message path",
  );
  assert.match(
    goChatProposal,
    /func WriteScopes\(\)\s*\[\]string\s*\{\s*return\s*\[\]string\{dbstore\.ScopeReadWrite\}/,
    "chat.WriteScopes no longer returns exactly the readwrite scope",
  );
  // The confirmation requires a token the MODEL never sees. This is the whole
  // design: a `confirm` boolean the model wrote is not a confirmation, so the
  // confirm input must not carry one.
  assert.match(
    goRest,
    /type chatConfirmRequestIn struct\s*\{[^}]*Token\s+string\s+`json:"token"`[\s\S]{0,200}?ArgsHash\s+string\s+`json:"args_hash"`/,
    "the confirm request no longer carries exactly {token, args_hash} — the token IS the " +
      "authorisation, and the hash is what binds the execution to the arguments the user read",
  );
  assert.doesNotMatch(
    goRest,
    /confirm\s*(?:bool|string)[^`]*`json:"confirm"/i,
    "the confirm request accepts a `confirm` field again — a boolean on the wire proves nothing " +
      "about who sent it, which is exactly the MCP shape this lane must not copy",
  );
  // The route is session-only and its own route, so the message path cannot reach it.
  assert.ok(
    goRestRoutes.includes('"POST /v1/rest/chat/confirm"'),
    "POST /v1/rest/chat/confirm is not registered — the card's Confirm button would 404",
  );
  assert.ok(
    goRestRoutes.includes('"POST /v1/rest/chat/cancel"'),
    "POST /v1/rest/chat/cancel is not registered — a Cancel that only hides the card would " +
      "leave a live authorisation in the browser",
  );
  // The confirmation has its OWN per-user bucket: a confirmation costs no model
  // call, so bounding it is about decision pace, not spend.
  assert.ok(
    goRest.includes("ChatConfirmLimiter") && goRest.includes("chatConfirmPerHour"),
    "the confirmation path has no per-user limit — a script could be walked through cards as " +
      "fast as it can post",
  );
});

test("phase 2: the proposable set excludes the destructive classes, and says so", () => {
  // The set itself: reversible field edits only. If this grows, the UI's promise
  // that the assistant can only prepare a small reversible change has to be
  // re-argued — which is what the assertion is for.
  assert.match(
    goChatTools,
    /var chatProposableTools\s*=\s*\[\]string\{[\s\S]*?update_product[\s\S]*?\}/,
    "the proposable set moved or no longer includes update_product",
  );
  for (const forbidden of ["delete_form", "refund_sale", "archive_product", "cancel_subscription"]) {
    assert.ok(
      !new RegExp(`chatProposableTools[\\s\\S]{0,900}?"${forbidden}"`).test(goChatTools),
      `${forbidden} is in the proposable set — delete/refund/archive/cancel are phase-3 decisions ` +
        "at the earliest, and the card is explicit that Wan must say so",
    );
  }
  // And the exclusions are WRITTEN DOWN, not merely absent.
  assert.ok(
    /WHAT IS EXCLUDED/i.test(goChatTools),
    "chat_tools.go no longer documents which classes are excluded — an unwritten exclusion is " +
      "an invitation to add one back",
  );
});

test("the page renders the proposal as a decision, not as done work", () => {
  // The card carries the token and the hash; nothing else may authorise.
  assert.match(
    page,
    /args_hash/,
    "the page no longer sends args_hash — without it the server has nothing binding the " +
      "execution to the arguments the user read",
  );
  // One decision call site, parameterised by action, hitting the two routes.
  assert.match(
    page,
    /"\/api\/rest\/chat\/confirm"\s*:\s*"\/api\/rest\/chat\/cancel"/,
    "the page no longer posts a decision to the literal /api/rest/chat/{confirm,cancel} routes — " +
      "a computed path is one the route-parity guard cannot resolve, so a typo would be a 404 " +
      "at the moment the user has committed to a decision",
  );
  assert.match(
    page,
    /"confirm"\s*\|\s*"cancel"/,
    "the decision call no longer distinguishes confirm from cancel",
  );
  // Both actions exist as buttons; a card with only Confirm is not a decision.
  assert.match(page, /chat\.confirm/, "the card has no Confirm button");
  assert.match(page, /chat\.cancel/, "the card has no Cancel button");
  // The badge that says nothing has happened yet.
  assert.match(
    page,
    /chat\.proposalBadge/,
    "the card carries no 'needs your confirmation' badge — a user who believes the change was " +
      "already made will not read the card",
  );
  // A spent or expired card must not keep offering the buttons: the server would
  // answer 409 to a click the user cannot act on.
  assert.ok(
    page.includes('"stale"') || page.includes("proposalStale"),
    "the page does not handle a stale (spent/expired) proposal — the user would get a bare error",
  );
  assert.match(
    page,
    /state\s*===\s*"pending"/,
    "the buttons are no longer gated on the card's state — a settled card would be re-clickable",
  );
  // A settled card must stop claiming it still needs the user. Verified in a real
  // browser against a production build: with the badge hard-coded, a CONFIRMED
  // card still read "NEEDS YOUR CONFIRMATION" and looked like an unfinished task.
  //
  // Matched as the literal CALL, not as a substring: a mutation run showed that
  // `page.includes("chat.proposalBadgeDone")` also passes for a mangled key like
  // `chat.proposalBadgeDoneMUTANT`, i.e. it would not have caught the key being
  // broken — only removed. The call form is the thing that has to exist.
  for (const key of [
    "chat.proposalBadgeDone",
    "chat.proposalBadgeCancelled",
    "chat.proposalBadgeStale",
    "chat.proposalBadgeFailed",
  ]) {
    assert.match(
      page,
      new RegExp(`t\\("${key}"\\)`),
      `the card's badge is not state-dependent (${key} is not used in a t() call) — a settled card ` +
        `would keep saying it needs your confirmation and invite a second click the server refuses`,
    );
  }
});

test("the chat routes are registered, GET for availability and POST for a message", () => {
  assert.ok(
    goRestRoutes.includes('"GET /v1/rest/chat"'),
    "GET /v1/rest/chat is not registered — the page could not ask whether the assistant is available",
  );
  assert.ok(
    goRestRoutes.includes('"POST /v1/rest/chat"'),
    "POST /v1/rest/chat is not registered — the page would post into a 404",
  );
});

test("the per-user limit is real, so the docs' 60/hour is not a decorative number", () => {
  assert.match(
    goRest,
    /chatRatePerHour\s*=\s*60/,
    "the chat rate limit changed — the UI and docs quote it, so update them together",
  );
  assert.ok(
    goRest.includes("ChatLimiter"),
    "rest_chat.go no longer consults a limiter: every message is at least one LLM call, and an " +
      "unlimited chat is an unbounded bill",
  );
});

test("the page reads availability and stops offering the box when it is false", () => {
  assert.match(
    page,
    /\/api\/rest\/chat/,
    "the page does not call the chat status endpoint",
  );
  assert.match(
    page,
    /available\s*===\s*false/,
    "the page no longer treats available:false as 'do not offer the box'",
  );
  assert.ok(
    page.includes("chat.unavailable"),
    "the unavailable state has no message",
  );
  // The inverted read: a truthy check on `available` would treat null (still
  // loading) as unavailable and hide the box for every user.
  assert.doesNotMatch(
    page,
    /available\s*\?\s*<textarea/,
    "the box must be gated on available === false, not on a truthy/null read",
  );
});

test("the availability flag means \"can this deployment answer\", not \"was a struct built\"", () => {
  // The server used to answer `available: h.Chat != nil`. The runner is built
  // unconditionally (main.go), so that answered "did we construct the object",
  // not "can a message be answered" — and the page offered a box whose every
  // message died, which the comment above promises it never does. Pin the
  // indirection so the flag cannot silently go back to an object check.
  assert.ok(
    goRest.includes("h.chatAvailable()"),
    "rest_chat.go no longer reports availability through h.chatAvailable() — the GET flag and " +
      "the POST refusal must answer the same question",
  );
  assert.doesNotMatch(
    goRest,
    /"available":\s*h\.Chat\s*!=\s*nil/,
    'rest_chat.go reports `available: h.Chat != nil` again — a runner object exists even with no ' +
      "LLM credential, so the page would offer a box that only errors",
  );
  // The runner itself answers it, and it must consult the credential, not just
  // the presence of a model object.
  assert.match(
    goChat,
    /func\s*\(r \*Runner\)\s*Ready\(\)\s*bool/,
    "chat.Runner has no Ready() — the REST edge would have nothing to ask",
  );
  assert.ok(
    goChat.includes("HasCredential"),
    "Ready() no longer consults the model's credential — a keyless deployment would report itself " +
      "available",
  );
  // A rejected key is a second, later answer: the POST must map it to the 503
  // the page's setAvailable(false) branch handles, not to a bare 500.
  assert.ok(
    goRest.includes("respondAIErr"),
    "rest_chat.go no longer maps a rejected model credential to 503 — a rotated key would turn " +
      "every message into a 500 and the page's unavailable branch would never fire",
  );
});

test("a filed feedback is announced in the transcript with a link to /feedback", () => {
  assert.match(
    page,
    /filed\s*\?/,
    "the page no longer renders the filed card — the user would discover the thread later on " +
      "/feedback instead of seeing it happen",
  );
  assert.ok(
    page.includes('href="/feedback"'),
    "the filed card has no way to go read the thread it just filed",
  );
  assert.ok(
    page.includes("chat.filed"),
    "the filed card has no message",
  );
});

test("a refused tool call is shown, not swallowed", () => {
  assert.match(
    page,
    /"refused"/,
    "the page no longer separates refused steps from successful ones",
  );
  assert.ok(
    page.includes("chat.refused"),
    "a refusal has no message — the user would read a silent refusal as a broken answer",
  );
});

test("the nav entry and the page exist together, and the docs page is reachable", () => {
  assert.ok(
    shell.includes('href: "/chat"'),
    "the sidebar has no /chat entry — the feature would be unreachable",
  );
  assert.ok(
    shell.includes('"chat"'),
    'the Shell "active" union does not accept "chat"',
  );
  assert.ok(
    docsNav.includes('"/docs/chat"'),
    "the docs sidebar does not link /docs/chat — the page would be unreachable",
  );
});

test('the inline "?" doc link points at the docs page that documents the boundary', () => {
  assert.ok(
    page.includes('href="/docs/chat"'),
    'the assistant has no inline "?" doc link (AGENTS.md DOCUMENTATION PARITY step 4)',
  );
  const docs = read(DOCS_PAGE);
  // The page must say what the assistant CANNOT do — that is the feature.
  assert.ok(
    /cannot change/i.test(docs) || /cannot delete/i.test(docs),
    "the docs page does not state what the assistant cannot do — the boundary is the whole design",
  );
  assert.ok(
    docs.includes("submit_feedback"),
    "the docs page does not name the MCP tool behind the one write (submit_feedback)",
  );
  // Phase 2: the docs page must document the confirmation lane too — how a
  // proposal is confirmed, and that the assistant cannot confirm it itself.
  assert.ok(
    docs.includes("propose_write"),
    "the docs page does not name propose_write — the phase-2 tool would be undocumented " +
      "(AGENTS.md DOCUMENTATION PARITY step 2)",
  );
  assert.ok(
    /confirm/i.test(docs),
    "the docs page does not explain confirmation — the whole phase-2 design is that a human " +
      "confirms and the server executes",
  );
});

test("every chat dictionary key defines all five locales", () => {
  const keys = Object.keys(DICT).filter((k) => k.startsWith("chat.") || k === "nav.chat");
  assert.ok(keys.length >= 12, `expected the chat dictionary, got ${keys.length} keys`);
  const missing: string[] = [];
  for (const k of keys) {
    const entry = DICT[k as keyof typeof DICT] as Record<string, unknown>;
    for (const lang of LANGS) {
      const v = entry[lang];
      if (typeof v !== "string" || v.trim() === "") missing.push(`${k}.${lang}`);
    }
  }
  assert.deepEqual(missing, [], `missing translations:\n  ${missing.join("\n  ")}`);
});
