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
//   7. THE TRANSLATION HALF OF THE SAME FILE THAT #747'S SCOPE IS GUARDED IN.
//      The availability fetch passes runLoad an `errorKey` routing the server's
//      machine `code` through lib/apiErr, so a refused request reads as the
//      localized reason rather than the page's generic "the assistant is
//      unavailable". Losing ONLY that half is invisible to the whole suite —
//      measured, not reasoned about (see the test's comment for the numbers) —
//      so this file pins it in the direction that was missing. The scope half
//      and the translation half are pinned by the two adjacent tests.
//
// WHAT IT DOES NOT COVER (stated honestly): it cannot execute React, so it does
// not observe the rendered transcript, the scroll, or the Enter-to-send key
// handling. The server-side behaviour (read-only refusal, filing, limits) is
// pinned by the Go tests, which is where that behaviour actually lives. The
// translation half is pinned ONLY once the tree carries the #6177 mechanism —
// the guard says which state it judged rather than passing quietly, and it
// cannot make the mechanism's own absence a failure (that is a different card's
// business, and this file must stay green on the tree that has no lib/apiErr).

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, existsSync, readdirSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { join, relative } from "node:path";

import { DICT, LANGS } from "../components/i18nDict.ts";
// The repo's shared comment/JSX-text-aware blanker (see its header): a class or
// an option quoted in a comment must neither raise nor satisfy an assertion.
// Imported rather than re-implemented — the copy this replaces had a defect that
// only surfaced on a page neither guard had seen yet.
import { codeOnly } from "./jsxSource.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");
const path = (rel: string) => fileURLToPath(new URL(rel, import.meta.url));
const readAbs = (abs: string) => readFileSync(abs, "utf8");
const appDir = fileURLToPath(new URL("../app", import.meta.url));

/**
 * Every `page.tsx` under `src/app`, as absolute paths.
 *
 * Module scope rather than inside a test: two guards read this census now (the
 * workspace scope, and the machine-code translation on runLoad call sites), and
 * a page that one of them judges must be judged by both. Hoisted so the two
 * cannot drift into looking at different sets of files.
 */
function walkAppPages(dir: string): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(dir)) {
    const p = join(dir, entry);
    if (statSync(p).isDirectory()) out.push(...walkAppPages(p));
    else if (entry === "page.tsx") out.push(p);
  }
  return out;
}
const pages = walkAppPages(appDir);

/** The source root (`web/src`), for censuses that are not page-shaped. */
const srcDir = fileURLToPath(new URL("..", import.meta.url));

/**
 * Every source file under `src` that MENTIONS `runLoad` — pages, components and
 * lib alike — as paths relative to `src`.
 *
 * Found by content, not by an allowlist of names: the guard's job is to judge
 * every call site that exists, and a hard-coded list of files is how a new one
 * ships unjudged. `.test.*` files are excluded (they hold expected strings, not
 * call sites).
 */
function walkRunLoadFiles(dir: string, base: string = dir): string[] {
  const out: string[] = [];
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    const full = `${dir}${e.name}`;
    if (e.isDirectory()) {
      if (e.name === "node_modules" || e.name.startsWith(".")) continue;
      out.push(...walkRunLoadFiles(`${full}/`, base));
    } else if (
      (e.name.endsWith(".tsx") || e.name.endsWith(".ts")) &&
      !/\.test\.tsx?$/.test(e.name) &&
      readFileSync(full, "utf8").includes("runLoad(")
    ) {
      out.push(full.slice(base.length));
    }
  }
  return out.sort();
}

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

/**
 * The number a Go `const` is declared with, read out of its source.
 *
 * NOT a copy of the value: a web test carrying "30" would be a THIRD copy of the
 * number and a fourth thing to edit — the defect this guard exists for, wearing
 * a guard's clothes. A name that is missing, or is no longer a simple numeric
 * literal, FAILS rather than handing the comparisons `undefined` (which is a
 * substring of nothing, so every check below would pass).
 */
function goConstNumber(src: string, name: string): number {
  const m = src.match(new RegExp(`\\b${name}\\s*=\\s*(\\d+(?:\\.\\d+)?)\\b`));
  assert.ok(
    m,
    `${name} is no longer declared as a numeric literal in the chat's Go source — retarget this ` +
      `guard at whatever replaced it. The docs and the 429 bodies quote this number, so it has to ` +
      `stay readable from here`,
  );
  return Number(m[1]);
}

/**
 * A Go `const X = N * time.Minute` declaration, as whole minutes.
 *
 * Minutes rather than seconds on purpose: the sentence this is compared against
 * is written in minutes ("it expires 5 minutes after it appears"), so converting
 * the duration here (and not in the sentence) keeps both sides readable.
 */
function goDurationMinutes(src: string, name: string): number {
  const m = src.match(new RegExp(`\\b${name}\\s*=\\s*(\\d+)\\s*\\*\\s*time\\.Minute\\b`));
  assert.ok(
    m,
    `${name} is no longer declared as "<n> * time.Minute" — this guard compares it to a sentence ` +
      `written in minutes, so retarget the guard rather than letting it compare nothing`,
  );
  return Number(m[1]);
}

test("the phase-2 caps the docs quote are read from the constants, so prose and enforcement move together", () => {
  // WHY THIS EXISTS, AND WHY IT IS NOT PR 695's TEST AGAIN.
  //
  // PR 695 made the WIRE follow the constant: chatLimits() references chat.*
  // instead of restating the number, and TestChatLimitsSourcesEveryCapFromItsConstant
  // refuses a numeric literal in that struct. It did not touch the PROSE, and
  // nothing else watches the prose — measured on this tree before this guard
  // existed: chatConfirmPerHour 30 -> 45 left `go test ./internal/httpapi/
  // -run TestChatLimits` GREEN (correct: the wire follows the constant) and the
  // whole web suite GREEN, while app/docs/chat/page.tsx still told the user "at
  // 30 per hour". That is the same drift PR 695 removed one layer out, so the
  // guard reads the constants and demands the prose quote THOSE values.
  //
  // The phase-1 pair is in here too: the 60/hour test above pins the constant,
  // but it never made the docs page, the dictionary, or the 429 body follow it.
  const docs = read(DOCS_PAGE);
  const msgPerHour = goConstNumber(goRest, "chatRatePerHour");
  const burst = goConstNumber(goRest, "chatBurst");
  const confirmPerHour = goConstNumber(goRest, "chatConfirmPerHour");
  const confirmBurst = goConstNumber(goRest, "chatConfirmBurst");
  const maxProposals = goConstNumber(goChatProposal, "MaxProposalsPerRun");
  const ttlMinutes = goDurationMinutes(goChatProposal, "ProposalTTL");

  // Vacuity, in the neighbours' idiom: prove this is reading the real numbers
  // and the real docs page before any of the comparisons below are believed.
  for (const [name, n] of Object.entries({
    chatRatePerHour: msgPerHour,
    chatBurst: burst,
    chatConfirmPerHour: confirmPerHour,
    chatConfirmBurst: confirmBurst,
    MaxProposalsPerRun: maxProposals,
    ProposalTTLMinutes: ttlMinutes,
  })) {
    assert.ok(
      Number.isFinite(n) && n > 0,
      `${name} read as ${n} — this guard is not reading the constant it thinks it is`,
    );
  }
  assert.ok(
    docs.includes("Confirming a proposed change") && docs.includes("Limits and cost"),
    `${DOCS_PAGE} is not the chat docs page this guard means to judge`,
  );

  // ── 1. the docs page ─────────────────────────────────────────────────────
  // Each entry is the sentence WITH the number built from the constant, so a
  // constant change and a docs change have to land together. The phrasing is
  // pinned as tightly as the 60/hour test pins the Go line; a copyedit that
  // rewrites the sentence has to update this list, which is the point.
  const sentences: Array<{ want: string; what: string }> = [
    {
      want: `${msgPerHour} messages per hour, in bursts of ${burst}`,
      what: "the per-user message cap and its burst",
    },
    {
      want: `at ${confirmPerHour} per hour in bursts of ${confirmBurst}`,
      what: "the confirmation cap and its burst",
    },
    {
      want: `at most ${maxProposals} proposals`,
      what: "the proposals-per-answer cap",
    },
    {
      want: `expires ${ttlMinutes} minutes after it appears`,
      what: "the proposal expiry",
    },
  ];
  for (const { want, what } of sentences) {
    assert.ok(
      docs.includes(want),
      `${DOCS_PAGE} no longer says "${want}" (${what}) — the docs quote the cap the server ` +
        `enforces, so the constant and this sentence cannot move apart`,
    );
  }

  // ── 2. the dictionary the page shows on a 429 ────────────────────────────
  // Digit presence, not the whole phrasing: a translation is free to be reworded
  // (and is, in four locales), but the NUMBER a user is told on a refusal has to
  // be the enforced one. The live rate-limit note (chat.limitNote) needs no pin
  // here — the page fills its {n} from the wire's limits block, which PR 695's
  // Go guard already ties to the constant.
  const dictCaps: Array<{ key: string; n: number; what: string }> = [
    { key: "chat.rateLimited", n: msgPerHour, what: "message cap" },
    { key: "chat.confirmRateLimited", n: confirmPerHour, what: "confirmation cap" },
  ];
  for (const { key, n, what } of dictCaps) {
    const entry = DICT[key as keyof typeof DICT] as Record<string, string> | undefined;
    assert.ok(entry, `${key} is gone from the dictionary — the page shows this string on a 429`);
    for (const lang of LANGS) {
      assert.ok(
        String(entry[lang]).includes(String(n)),
        `${key}.${lang} does not quote the enforced ${what} (${n}): "${entry[lang]}" — the UI ` +
          `tells the user a number on a 429, so the constant and this string move together`,
      );
    }
  }

  // ── 3. the 429 bodies ────────────────────────────────────────────────────
  // The third prose copy, and the one that used to be a literal: phase 1's
  // refusal said "(60 mesej sejam)" as text, so it would have kept telling a user
  // 60 after the constant moved. Every sentence that quotes a bucket has to be
  // BUILT from the constant, which is what the two assertions below measure.
  const chatLines = goRest.split("\n").filter((l) => l.includes("mesej sejam"));
  assert.equal(
    chatLines.length,
    1,
    "rest_chat.go no longer answers a rate-limited MESSAGE with one 'mesej sejam' sentence — " +
      "retarget this guard at whatever replaced it",
  );
  assert.ok(
    chatLines[0].includes("chatRatePerHour"),
    `the message 429 body quotes its own number instead of the enforced one: ` +
      `${chatLines[0].trim()} — a constant change would leave the refusal stating the old limit`,
  );
  assert.doesNotMatch(
    chatLines[0],
    /\(\s*\d+\s*mesej sejam/,
    `the message 429 body restates the number as text (${chatLines[0].trim()}) — build it from ` +
      `chatRatePerHour instead`,
  );

  const confirmLines = goRest.split("\n").filter((l) => l.includes("had pengesahan dicapai"));
  assert.equal(
    confirmLines.length,
    2,
    "the confirmation bucket is enforced in two handlers (confirm and cancel) and their 429 " +
      "bodies must both be here — a guard that reads one is blind to the other",
  );
  for (const l of confirmLines) {
    assert.ok(
      l.includes("chatConfirmPerHour"),
      `a confirmation 429 body quotes its own number instead of the enforced one: ${l.trim()}`,
    );
  }
  assert.doesNotMatch(
    goRest,
    /\(\s*\d+\s*sejam/,
    "a 429 body restates the confirmation number as text — build it from chatConfirmPerHour " +
      "(fmt.Sprintf) instead",
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

// ── THE WORKSPACE SCOPE (the defect this guard exists for) ───────────────────
//
// WHAT BROKE. This page talked to /api/rest/chat, /chat/confirm and /chat/cancel
// with no ?workspace= at all, so the server resolved `Accounts.ByOwner(...)[0]` —
// the user's FIRST account, not the workspace selected in the Shell. Phase 1 that
// answered about the wrong workspace; phase 2 made it WRITABLE, because the
// confirmation executes the stored proposal under the account the request
// resolved. A merchant with two workspaces selected the second, read a card naming
// the first one's product, and confirming it edited the first one.
//
// WHY A SOURCE GUARD AND NOT A UI TEST. This repo has no React/DOM runner (see
// src/lib/i18n.test.ts), so the only place the request URLs exist is the page
// source. Pinning the three call sites there is what makes the omission
// impossible: a fourth chat call added without the scope, or a hand-rolled
// `?workspace=` string, both fail here.
//
// The Go half is pinned cross-language in the same test: the server refuses an
// unscoped chat call from a multi-workspace session (requireResolvedWorkspaceScope
// in api/internal/httpapi/rest_scope.go), so the two halves cannot drift into
// "the page sends it" / "the server stopped caring" or the reverse.
const workspaceHelper = read("./workspace.ts");
const scopeFile = read("../../../api/internal/httpapi/rest_scope.go");
const chatTest = read("./chatPage.test.ts");

test("the chat page scopes EVERY call with workspaceQuery() — the shared helper, not a hand-rolled string", () => {
  // Every chat call site the page builds must go through the helper. Counted
  // rather than merely "present": one call site left unscoped is the whole defect,
  // and a substring check for `workspaceQuery` passes with two of three converted.
  //
  // The call sites are counted by their FETCH, not by a URL literal, because the
  // decision route is written literally into `url` and fetched as
  // `${url}${workspaceQuery()}` — the route-parity rule demands the literal path
  // (see the phase-2 test above), so the guard has to look at the fetch site.
  const chatFetches = [...page.matchAll(/fetch(?:Json<[^>]*>)?\(\s*(`[^`]*`|"[^"]*")/g)].map(
    (m) => m[1],
  );
  const chatCalls = chatFetches.filter(
    (u) => u.includes("/api/rest/chat") || u === "`${url}${workspaceQuery()}`",
  );
  assert.ok(
    chatCalls.length >= 3,
    `expected the page to build at least three chat calls (status, message, decision), found ` +
      `${chatCalls.length}: ${JSON.stringify(chatFetches)}`,
  );
  for (const u of chatCalls) {
    assert.ok(
      u.includes("${workspaceQuery()}"),
      `the chat call ${u} does not append \${workspaceQuery()} — an unscoped chat call resolves the ` +
        `user's FIRST account, not the workspace they selected, and the confirmation lane WRITES there`,
    );
  }
  // The decision URL still gets scoped even though its path is literal.
  assert.ok(
    page.includes("await fetch(`${url}${workspaceQuery()}`"),
    "the confirm/cancel decision fetch no longer appends the workspace scope — the confirmation " +
      "would write to the user's first account",
  );
  // The page renders the scope refusal as its own message. Without this the user
  // sees the generic "could not answer" for a condition they can fix by picking a
  // workspace, and the server's explicit refusal is wasted.
  assert.ok(
    page.includes('t("chat.scopeRequired")'),
    "the page no longer renders a message for the workspace-scope refusal — the server refuses " +
      "explicitly precisely so the user can act on it",
  );
  // The PROBE's 400 must be separated from the generic failure too, not only the
  // message path's. The availability GET is the page's FIRST chat call and it
  // fires before the Shell has selected a workspace on a fresh session, so on a
  // two-workspace session it is the call that actually meets the scope refusal
  // (t_997bb646). Falling through to `setAvailable(false)` renders
  // chat.unavailable ("not available right now") — false, and it closes the box
  // for a condition the user fixes by choosing a workspace.
  //
  // MEASURED, not assumed (both mutations applied to this tree, the file's test
  // run alone, then reverted):
  //   * the 400 branch deleted            -> RED here (19/20)
  //   * `setAvailable(false)` replaced by the scope setErr on the generic path
  //     -> RED here (19/20), because the unavailable branch must survive
  // The browser-level half of the same defect is e2e/chat-probe-400.spec.ts,
  // which drives the built page and fails on the pre-fix build with
  // "locator('p[role=\"alert\"]') element(s) not found".
  //
  // The branch is located by its own site, the runLoad outcome's `!r.ok` guard —
  // not by a line number — and the slice runs to the statement after that block
  // (`setAvailable(r.data.available …)`), so it covers the WHOLE !r.ok branch
  // including the generic fall-through the scope branch must not replace.
  const probeAt = page.indexOf("const r = await runLoad(");
  assert.ok(
    probeAt !== -1,
    "app/chat/page.tsx no longer reads availability through runLoad — this guard cannot judge the " +
      "probe's status handling and refuses to guess",
  );
  const probeFrom = page.indexOf("if (!r.ok) {", probeAt);
  assert.ok(
    probeFrom !== -1,
    "the chat availability probe has no !r.ok branch — the scope refusal would be consumed by " +
      "runLoad and the page would offer a box whose every message 400s",
  );
  const probeEnd = page.indexOf("setAvailable(r.data.available", probeFrom);
  assert.ok(
    probeEnd > probeFrom,
    "could not find the statement after the availability probe's !r.ok branch — this guard refuses " +
      "to slice a branch it cannot delimit",
  );
  const probeBranch = page.slice(probeFrom, probeEnd);
  assert.match(
    probeBranch,
    /if \(r\.status === 400\) \{\s*setErr\(t\("chat\.scopeRequired"\)\);\s*return;/,
    `the availability probe's !r.ok branch does not answer a 400 with t("chat.scopeRequired") — a ` +
      `two-workspace session with no workspace selected would read "The assistant is not available ` +
      `right now." (chat.unavailable) instead of the scope copy, and the composer would be ` +
      `disabled. Branch was:\n${probeBranch}`,
  );
  // And the generic branch survives: a 5xx / network failure is still
  // "unavailable", so the scope branch cannot have replaced it.
  assert.match(
    probeBranch,
    /setAvailable\(false\)/,
    "the availability probe no longer closes the box on a genuine failure — the scope branch must " +
      "be ADDITIONAL to the unavailable branch, not a replacement for it",
  );
  // 400 must not be folded into the 401/403 branch (that branch redirects to
  // /login, which would be wrong for a signed-in user whose scope was missing).
  assert.ok(
    !/status === 400[^\n]*goToLogin/.test(page),
    "the page treats a 400 as a dead session — a missing workspace scope would redirect a " +
      "signed-in merchant to /login",
  );
  // No hand-rolled query string in CODE: the active-workspace rule lives in one
  // place (lib/workspace.ts), and a second spelling is how it drifts. Comments are
  // stripped first — this page documents the `?workspace=` rule in prose, and a
  // guard that fires on its own documentation is a guard people delete.
  const pageCode = page
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .split("\n")
    .filter((l) => !/^\s*(\/\/|\*)/.test(l))
    .join("\n");
  assert.ok(
    !/workspace=\$\{|[?&]workspace=/.test(pageCode),
    "app/chat/page.tsx builds ?workspace= by hand instead of using workspaceQuery() — the " +
      "active-workspace rule must stay in ONE place (lib/workspace.ts)",
  );
  assert.ok(
    page.includes('import { workspaceQuery } from "../../lib/workspace"'),
    "app/chat/page.tsx does not import workspaceQuery from the shared helper",
  );
  // And the helper is still the real thing, not a stub the page could satisfy by
  // importing it and sending nothing.
  assert.match(
    workspaceHelper,
    /export function workspaceQuery\(\): string \{\s*const sp = workspaceScope\(\);\s*return sp \? `\?workspace=\$\{encodeURIComponent\(sp\)\}` : "";/,
    "lib/workspace.ts workspaceQuery() no longer returns `?workspace=<slug>` — the page would import a " +
      "helper that scopes nothing",
  );
});

test("the SERVER refuses a chat call whose workspace would be guessed", () => {
  // The gate exists, is wired into the chat lane, and is not merely a 403 the
  // page could ignore: a single-workspace session must still be served.
  assert.match(
    scopeFile,
    /func \(h \*RESTHandlers\) requireResolvedWorkspaceScope\(/,
    "rest_scope.go no longer defines the refusal for an unresolvable workspace scope — the chat would " +
      "fall back to Accounts.ByOwner(...)[0] again",
  );
  assert.match(
    scopeFile,
    /case scopeUnresolved:[\s\S]{0,200}?http\.StatusForbidden/,
    "the foreign-slug refusal is no longer a 403",
  );
  // The MISSING-scope refusal must NOT be a 401/403. wsfetch.ts is the module
  // every dashboard fetch goes through, and the chat page repeats the rule by
  // hand: 401/403 means "the session is gone" and redirects to /login. A
  // signed-in, two-workspace merchant whose client omitted the scope would be
  // logged out of the page they are reading — a worse bug than the one this card
  // fixes. Pinned here because it is a CLIENT contract living in Go code.
  assert.match(
    scopeFile,
    /case scopeAmbiguous:[\s\S]{0,200}?http\.StatusBadRequest/,
    "the missing-scope refusal must be a 400, not a 401/403 — wsfetch.ts redirects 401/403 to " +
      "/login, so a 403 here would bounce a signed-in merchant to the login page",
  );
  assert.ok(
    read("./wsfetch.ts").includes("return status === 401 || status === 403;"),
    "wsfetch.ts no longer treats 401/403 as an invalid session — re-argue the status choice above " +
      "before changing this guard",
  );
  assert.match(
    goRest,
    /func \(h \*RESTHandlers\) chatWorkspaceScopeOr403\(/,
    "rest_chat.go no longer applies the scope gate — the page sending ?workspace= must not be the only " +
      "thing standing between a two-workspace merchant and a write to the wrong workspace",
  );
  // The gate is applied on all FOUR chat routes: a decision endpoint left
  // unscoped is the same defect with a narrower door.
  const gateCalls = [...goRest.matchAll(/h\.chatWorkspaceScopeOr403\(w, r, actx\)/g)].length;
  assert.ok(
    gateCalls >= 4,
    `rest_chat.go applies the scope gate at ${gateCalls} call site(s), want 4 (GET status, POST message, ` +
      `POST confirm, POST cancel) — an unscoped route resolves the first account`,
  );
  // Each of the four handlers must be one of the call sites (a call sitting in a
  // dead helper would satisfy the count above and gate nothing).
  for (const handler of [
    "handleChatStatus",
    "handleChat",
    "handleChatConfirm",
    "handleChatCancel",
  ]) {
    const body = goRest.slice(goRest.indexOf(`func (h *RESTHandlers) ${handler}(`));
    const next = body.indexOf("\nfunc ", 1);
    const fn = next > 0 ? body.slice(0, next) : body;
    assert.ok(
      fn.includes("chatWorkspaceScopeOr403"),
      `${handler} does not refuse an unresolvable workspace scope — that route would resolve the ` +
        `caller's first account`,
    );
  }
  // The two halves must be tested together, or this file could pass while the Go
  // guard is deleted: the server-side criteria live in
  // chat_workspace_scope_test.go.
  assert.ok(
    existsSync(path("../../../api/internal/httpapi/chat_workspace_scope_test.go")),
    "the Go guard for this rule is gone — the source pins above would then be the only thing " +
      "asserting it, and they would pass with the server refusing nothing",
  );
  assert.ok(
    chatTest.includes("chatWorkspaceScopeOr403"),
    "this guard no longer reads the gate it claims to pin",
  );
});

test("no dashboard page calls an ACCOUNT-scoped REST route without the workspace scope", () => {
  // THE CLASS, not the instance. The chat's omission was not special: every
  // dashboard page that resolves its account from ?workspace= must send it, and
  // the next page written the same way fails here rather than shipping.
  //
  // WHAT MAKES A PAGE SUBJECT TO THE RULE. Only pages that reach an
  // ACCOUNT-scoped route. Two families are legitimately unscoped and must NOT be
  // flagged, because on those routes the scope is not what identifies the data:
  //
  //   * USER-level routes — /api/rest/feedback*, /api/rest/uploads/avatar,
  //     /api/rest/profile/avatar. The handlers key them on sessionOwnerID (the
  //     person), not on an account, so there is no workspace to send. Sending one
  //     would be noise, and requiring it would be a rule that cannot be obeyed.
  //   * Non-REST and public-URL fetches (a form's own /f/<slug>, docs assets).
  //
  // Everything else under /api/rest/ that a Shell page calls IS account-scoped:
  // the lists, the products, the builder, settings, deliveries, templates.
  //
  // `pages` is the module-level census (walkAppPages above) so this guard and the
  // machine-code guard below judge the same set of files.
  assert.ok(
    pages.length >= 13,
    `expected the dashboard's pages, got ${pages.length} — the walk must be finding the real app tree`,
  );

  // Routes whose data is the PERSON's, not an account's (see above).
  const USER_LEVEL = /\/api\/rest\/(feedback|uploads\/avatar|profile\/avatar)/;
  // Comments stripped by the repo's SHARED, JSX-text-aware blanker — the pages
  // document the ?workspace= rule in prose, and a class quoted in a comment must
  // not satisfy this census.
  //
  // It used to hand-roll `src.replace(/\/\*[\s\S]*?\*\//g, "")`, and that copy
  // carried the bug jsxSource.ts exists to remove (measured on this tree, not
  // reasoned about): JSX text renders literals like `/v1/rest/*`, whose `/`+`*`
  // opened a phantom block comment that blanked 12,419 characters of
  // app/forms/page.tsx — including its /api/rest/ URLs. A page with EVERY
  // workspaceQuery removed then read GREEN, because the census could no longer
  // see what it was supposed to judge. `codeOnly` keeps every byte offset, so the
  // census sees the same bytes a compiler would.
  const stripComments = (src: string) => codeOnly(src);

  const offenders: string[] = [];
  let subjectPages = 0;
  for (const abs of pages) {
    const code = stripComments(readAbs(abs));
    // Every /api/rest/... URL this page builds as a string/template literal.
    const urls = [...code.matchAll(/[`"](\/api\/rest\/[^`"]*)[`"]/g)].map((m) => m[1]);
    const accountScoped = urls.filter((u) => !USER_LEVEL.test(u));
    if (accountScoped.length === 0) continue;
    subjectPages += 1;
    const scoped = code.includes("workspaceQuery");
    if (!scoped) {
      offenders.push(
        `${relative(appDir, abs)}: calls ${JSON.stringify([...new Set(accountScoped)])} with no ` +
          `workspaceQuery() — the server resolves Accounts.ByOwner(...)[0], the user's FIRST ` +
          `account, not the workspace they selected`,
      );
    }
  }
  assert.ok(
    subjectPages >= 10,
    `only ${subjectPages} page(s) were judged account-scoped — the census is not seeing the real ` +
      `pages, so this guard would pass vacuously`,
  );
  assert.deepEqual(offenders, [], `account-scoped pages missing the workspace scope:\n  ${offenders.join("\n  ")}`);
  // The chat is the page this guard exists for, so it must be one of the judged
  // pages — not excluded by the filters above.
  assert.ok(
    pages.some((p) => p.endsWith(join("app", "chat", "page.tsx"))),
    "the walk did not include app/chat/page.tsx — the page whose omission this guard exists for",
  );
});

// ── THE TRANSLATION HALF (the #6177 direction this file was missing) ─────────
//
// WHAT BROKE, AND WHY IT WAS INVISIBLE. The scope test above fires the moment
// #747's `workspaceQuery()` leaves the chat page. The other half of the SAME
// call site — #6177's `errorKey: (b) => translateApiErr(b, t)`, which turns the
// server's machine `code` into the user's language — had no guard at all.
// Measured on a real merge candidate (PR 742 x main, chat/page.tsx resolved as
// the union of both), neutralising that `errorKey` by value:
//
//     node --test src/lib/chatPage.test.ts src/lib/apiErr.test.ts
//       -> tests 24, pass 24, fail 0        (before this test existed)
//
// and the page silently went back to printing only its own generic
// "the assistant is unavailable" for a refusal the server named precisely.
// `apiErr.test.ts`'s banned-shape scan looks for `body.error ||` fallbacks —
// dropping the option entirely is not that shape, so nothing noticed.
//
// WHY THE EXTRACTOR IS THE POINT. The obvious guard is `page.includes(...)`,
// and it is the one that fails: `errorKey` is a property name that also occurs
// in `lib/workspace.ts`, so a substring check passes on a page that dropped it.
// The two helpers below read the call site's actual argument object and the
// actual value of its `errorKey`, and every assertion quotes what it saw — so a
// wrong read reports a wrong value rather than a confident green.

/**
 * The source of every `runLoad(...)` call in `src`.
 *
 * Balanced-paren slice from the call's `(` to its matching `)`. Deliberately not
 * a full TypeScript parse: the file under test is the thing being judged, and a
 * guard that needs the parser stack to hold an opinion about it would be a
 * heavier dependency than the two properties it checks (the call is a chat call;
 * its `errorKey` routes through apiErr). Offsets are irrelevant here, only the
 * slice's text is.
 */
function runLoadCalls(src: string): string[] {
  const out: string[] = [];
  const re = /\brunLoad\s*\(/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(src))) {
    let i = m.index + m[0].length;
    let depth = 1;
    let quote = "";
    for (; i < src.length && depth > 0; i++) {
      const c = src[i];
      if (quote) {
        if (c === "\\") i++;
        else if (c === quote) quote = "";
        continue;
      }
      if (c === '"' || c === "'" || c === "`") {
        quote = c;
        continue;
      }
      if (c === "(" || c === "{" || c === "[") depth++;
      else if (c === ")" || c === "}" || c === "]") depth--;
    }
    out.push(src.slice(m.index, i));
  }
  return out;
}

/**
 * The literal source text of `key`'s value inside a call slice, or null when the
 * key is absent. Stops at the depth-0 `,` or closing bracket, so a caller reads
 * `"\"" ` for `errorKey: ""` and the arrow function itself for the real site.
 */
function propValue(slice: string, key: string): string | null {
  const m = new RegExp(`(?<![\\w$])${key}\\s*:\\s*`).exec(slice);
  if (!m) return null;
  let i = m.index + m[0].length;
  let depth = 0;
  let quote = "";
  const start = i;
  for (; i < slice.length; i++) {
    const c = slice[i];
    if (quote) {
      if (c === "\\") i++;
      else if (c === quote) quote = "";
      continue;
    }
    if (c === '"' || c === "'" || c === "`") {
      quote = c;
      continue;
    }
    if (c === "(" || c === "{" || c === "[") depth++;
    else if (c === ")" || c === "}" || c === "]") {
      if (depth === 0) break;
      depth--;
    } else if (c === "," && depth === 0) break;
  }
  return slice.slice(start, i).trim();
}

test("the chat page's availability fetch keeps the machine-code translation, not only the scope", () => {
  // The tree's state, read rather than assumed. On the branch that carries
  // #6177 the mechanism is lib/apiErr; on a tree without it the option does not
  // exist anywhere in the app and there is nothing to pin yet. Both states are
  // stated in the assertion message below, so neither can be mistaken for the
  // other in a log.
  const treeHasApiErr = existsSync(path("./apiErr.ts"));
  const appHasErrorKey = walkRunLoadFiles(srcDir).some((rel) =>
    /(?<![\w$])errorKey\s*:/.test(readAbs(join(srcDir, rel))),
  );
  assert.ok(
    !appHasErrorKey || treeHasApiErr,
    "a page passes runLoad an `errorKey` but lib/apiErr.ts does not exist — the option " +
      "has no meaning on a tree without the #6177 mechanism",
  );

  // THE call site. Isolated by its URL rather than by a line number, so an edit
  // above it cannot silently move the guard onto a different call.
  const pageCode = codeOnly(page);
  const availability = runLoadCalls(pageCode).filter((s) => /\/api\/rest\/chat\b/.test(s));
  assert.equal(
    availability.length,
    1,
    `expected exactly one runLoad site on the chat availability route, found ` +
      `${availability.length}: this guard judges one call site and refuses to guess which`,
  );
  const site = availability[0];

  if (!treeHasApiErr) {
    // The #6177 mechanism is not in this tree, so the option does not exist to
    // pass. NOT a pass on the behaviour — the state the guard judged, said out
    // loud. The moment lib/apiErr.ts lands, the branches below are live and the
    // failure above (a page using `errorKey` without the module) is impossible.
    assert.equal(
      propValue(site, "errorKey"),
      null,
      "the tree has no lib/apiErr.ts yet the page passes an errorKey — see the assertion " +
        "above this one",
    );
    return;
  }

  // The union is the thing that must survive: the scope AND the translation on
  // the one call. The scope's half is asserted by the test above; its presence
  // is re-checked here in the same slice only so a failure names the whole
  // call site rather than one half of it.
  assert.match(
    site,
    /\/api\/rest\/chat\$\{workspaceQuery\(\)\}/,
    `the chat availability fetch lost #747's workspace scope — an unscoped chat call ` +
      `resolves the user's FIRST account, not the workspace they selected. Site: ${site.trim()}`,
  );

  const value = propValue(site, "errorKey");
  assert.notEqual(
    value,
    null,
    `the chat availability fetch has no errorKey — the server's machine code is dropped and ` +
      `the user reads only the page's generic "the assistant is unavailable" for a refusal ` +
      `the server named precisely (#6177). Site: ${site.trim()}`,
  );
  assert.match(
    value as string,
    /\btranslateApiErr\s*\(/,
    `the chat availability fetch's errorKey (${JSON.stringify(value)}) does not route through ` +
      `translateApiErr, so the API's machine code is not translated to the user's language (#6177)`,
  );
  assert.match(
    value as string,
    /\bt\b/,
    `the errorKey at ${JSON.stringify(value)} does not receive the page's translator, so it ` +
      `cannot render anything localized`,
  );

  // And #6177's helper is really in this file: the option is a function of the
  // imported module, not a local re-implementation the guard would happily read.
  assert.match(
    pageCode,
    /import\s*\{[^}]*\btranslateApiErr\b[^}]*\}\s*from\s*"\.\.\/\.\.\/lib\/apiErr"/,
    "app/chat/page.tsx's errorKey names translateApiErr without importing it from lib/apiErr",
  );
});

test("every runLoad call site that translates an API code routes through lib/apiErr", () => {
  // THE CLASS, not just the chat instance above. A hand-rolled body read —
  // `errorKey: (b) => (b as any).code` — re-introduces exactly the defect the
  // option exists to remove: an untranslated machine key rendered to the user.
  //
  // The census is every runLoad CALL SITE under src/, found by reading the files
  // that mention runLoad — NOT the app/ page walk. Five of the app's sites live
  // in a component (components/MembersTab.tsx) rather than a page.tsx, and a
  // census that judged only pages would leave those unread while looking
  // complete. Measured: with the page-shaped census, neutralising MembersTab's
  // errorKey left this file fully green (25/25) and the hole was invisible.
  //
  // Guarded on the tree's state for the same reason the chat test is: without
  // lib/apiErr.ts in the tree the option does not exist to judge.
  if (!existsSync(path("./apiErr.ts"))) return;

  const ROUTES_THROUGH_APIERR = /\b(translateApiErr|apiErrorKey|apiErr)\s*\(/;
  const callSiteFiles = walkRunLoadFiles(srcDir);
  assert.ok(
    callSiteFiles.length >= 15,
    `expected the app's runLoad call sites across at least 15 files, found ` +
      `${callSiteFiles.length} — the census is not seeing the real tree`,
  );
  const offenders: string[] = [];
  let errorKeySites = 0;
  for (const rel of callSiteFiles) {
    for (const s of runLoadCalls(codeOnly(readAbs(join(srcDir, rel))))) {
      const v = propValue(s, "errorKey");
      if (v === null) continue;
      errorKeySites += 1;
      if (!ROUTES_THROUGH_APIERR.test(v)) {
        offenders.push(`${rel}: errorKey: ${v}`);
      }
    }
  }
  // A census that reads few errorKeys would pass vacuously the moment the
  // extractor stopped parsing — the count is asserted for the same reason the
  // page walk's is. The floor is a little below the 26 sites the app has today,
  // so adding or removing a call site is not itself a failure.
  assert.ok(
    errorKeySites >= 20,
    `only ${errorKeySites} errorKey call site(s) were read across ${callSiteFiles.length} files ` +
      `— the extractor is not seeing the argument objects it claims to judge`,
  );
  assert.deepEqual(
    offenders,
    [],
    `these runLoad sites build their errorKey without the lib/apiErr helpers, so the server's ` +
      `machine code is not translated the way #6177 requires:\n  ${offenders.join("\n  ")}`,
  );
  // The component the page-shaped census missed must really be in the walk,
  // otherwise this guard can silently shrink back to pages-only.
  assert.ok(
    callSiteFiles.some((rel) => rel.endsWith("components/MembersTab.tsx")),
    "MembersTab.tsx is not in the call-site walk — the census is pages-only again, and a " +
      "component's errorKey would go unjudged (the measured hole this test exists for)",
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
  // The workspace scope is user-VISIBLE: a member of two workspaces can hit the
  // refusal, so it must be documented where the inline "?" leads, or the user
  // reads a 403 with no explanation of what to do about it.
  assert.ok(
    /selected workspace/i.test(docs),
    "the docs page does not say a confirmation writes in the SELECTED workspace — a user who hits " +
      "the scope refusal has nothing telling them why, or that choosing a workspace is the fix",
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

// ── THE CONFIRM PATH'S 400 IS DECIDED BY CODE, NOT BY STATUS ─────────────────
//
// THE FIRST DEFECT (t_790b1016, found while verifying PR 747). The 400 branch on
// the confirm/cancel path did TWO things at once:
//
//     setState(msgIndex, p.token, "failed");   // → card footer: chat.confirmFail
//     setErr(t("chat.scopeRequired"));         // → role=alert: chat.scopeRequired
//
// so one 400 rendered both sentences at once, and the footer's advice was the
// wrong one: "The change could not be applied. Please try again." tells the user
// to repeat an action that fails identically without a workspace. Measured in a
// real browser on the PR 747 merge build (role=alert = the scope copy, card
// footer = the "try again" copy, same 400).
//
// WHY THE STATE IS THE FIX AND NOT THE FOOTER. The server refuses an unscoped
// call BEFORE it reads the proposal token (requireResolvedWorkspaceScope runs
// ahead of the token checks in handleChatConfirm), so nothing was attempted and
// "failed" was never true of this request. Giving the refusal its own state lets
// the card stop claiming a failure without inventing a second rendering of the
// scope copy — that duplication is what the pre-fix shape did, and this card says
// explicitly that the alert is the ONE place the copy belongs.
//
// THE SECOND DEFECT (t_fb393abe, found by the QA lane while verifying PR 779 and
// fixed here). The branch that got its own state was keyed on the BARE STATUS —
// `if (r.status === 400)` — and it returned before the body was ever parsed. A 400
// from this route is NOT necessarily a workspace-scope refusal:
// chatConfirmStatusFor maps everything it does not recognise to 400, and one such
// class arrives AFTER the token is claimed (rest_chat.go's default arm over
// chat/proposal.go's "server menolak: %s"). A read_only member confirming a
// prepared change got "Choose a workspace first" — advice they cannot act on,
// since the request carried their workspace, and the refusal they needed to read
// was the role. So the decision now branches on the API's machine CODE
// (lib/chatConfirm.ts, code `workspace.scopeRequired`), which is the only thing
// that can tell the two 400s apart.
//
// WHAT MAKES IT BITE. Two layers, and neither alone is enough:
//   * src/lib/chatConfirm.test.ts EXECUTES confirmDecisionFor, so a bare
//     `status === 400` there turns the non-scope case RED (the case that was
//     invisible while the decision lived inside the component).
//   * this file pins the PAGE: the outcome branch keeps the card out of "failed",
//     no `r.status === 400` survives anywhere in decide(), and the helper really is
//     the one the branch reads and really is imported.
// The card-footer half asserts the footer is keyed on chat.confirmFail ONLY when
// the state is "failed", and the dictionary half pins that a scopeRequired card
// cannot borrow the failure string: the two keys must not be the same sentence, or
// the "does not render confirmFail" property would be a distinction without a
// difference.
test("the confirm path's scope refusal is its own state, and cannot reach chat.confirmFail", () => {
  // THE BRANCH MOVED (t_fb393abe). What this guard pinned was the slice from
  // `if (r.status === 400) {` — i.e. the branch keyed on the BARE STATUS, which was
  // itself the defect this card fixes: any 400 answered "Choose a workspace first",
  // including a tool the server refused by ROLE after claiming the token. The
  // decision now lives in lib/chatConfirm.ts and is keyed on the API's machine
  // CODE, so the property this guard owns is re-anchored on the outcome branch.
  // The BEHAVIOUR of the decision (which 400 means what) is executed in
  // src/lib/chatConfirm.test.ts; what is pinned HERE is that the page keeps the
  // card out of the failure state for the scope outcome.
  const at = page.indexOf('if (outcome === "scopeRequired") {', page.indexOf("async function decide("));
  assert.ok(
    at !== -1,
    "app/chat/page.tsx has no scopeRequired outcome branch in decide() — the scope refusal would " +
      "either fall through to the generic failure branch (the user gets 'try again' for a condition " +
      "they fix by choosing a workspace) or be decided by the bare status again (t_fb393abe)",
  );
  const branch = page.slice(at, page.indexOf("return;", at) + "return;".length);

  assert.match(
    branch,
    /setState\(msgIndex,\s*p\.token,\s*"scopeRequired"\)/,
    `the scope refusal no longer marks the proposal "scopeRequired" — it would set a state the ` +
      `card renders as a failed change while the alert says to choose a workspace (both at once, ` +
      `which is the defect). Branch was:\n${branch}`,
  );
  assert.doesNotMatch(
    branch,
    /setState\(msgIndex,\s*p\.token,\s*"failed"\)/,
    "the scope refusal still marks the proposal \"failed\", so the card renders chat.confirmFail " +
      "('Please try again.') under an alert telling the user to choose a workspace — retrying without " +
      "a workspace fails identically, so the footer's advice is wrong",
  );
  assert.match(
    branch,
    /setErr\(t\("chat\.scopeRequired"\)\)/,
    "the scope refusal no longer renders the scope copy — the user would see only the card's " +
      "failure sentence and would not learn that choosing a workspace is the fix",
  );
  // The branch must be reached by the DECISION, not by a status test. A
  // `r.status === 400` left anywhere in decide() is the defect back: it cannot
  // tell the scope refusal from the post-claim tool refusal, and it renders the
  // scope copy for both. Counted rather than merely absent, because the point is
  // that NO status-only 400 branch survives.
  const fourHundreds = page
    .slice(page.indexOf("async function decide("))
    .match(/r\.status === 400/g);
  assert.equal(
    fourHundreds,
    null,
    `decide() still branches on the bare 400 status (${fourHundreds?.length} site(s)). That branch ` +
      `cannot distinguish a workspace-scope refusal from any other 400 the route answers — including ` +
      `the tool refusal that arrives AFTER the token is claimed — so it renders "Choose a workspace ` +
      `first" to a user who already had a workspace selected (t_fb393abe). The decision belongs to ` +
      `confirmDecisionFor (lib/chatConfirm.ts), which branches on the machine code.`,
  );
  // And the decision really is the one the branch reads: the outcome names must
  // come from the shared helper, not from a local re-implementation that could
  // drift from the tested one.
  assert.match(
    page.slice(page.indexOf("async function decide(")),
    /const outcome = confirmDecisionFor\(r\.status,\s*body\)/,
    "decide() no longer routes its answer through confirmDecisionFor — the branch would be deciding " +
      "what a response means by itself, which is the shape t_fb393abe is about",
  );
  assert.match(
    page,
    /import \{ confirmDecisionFor \} from "\.\.\/\.\.\/lib\/chatConfirm"/,
    "app/chat/page.tsx names confirmDecisionFor without importing it from lib/chatConfirm",
  );

  // The card's own footer. "failed" must be the ONLY state that renders
  // chat.confirmFail, and a scopeRequired card must not.
  const footerAt = page.indexOf('{state === "failed"');
  assert.ok(footerAt !== -1, "the card footer no longer renders a 'failed' state at all");
  const footer = page.slice(footerAt, page.indexOf("\n", page.indexOf("</p>", footerAt)));
  assert.ok(
    footer.includes('t("chat.confirmFail")'),
    "a genuine failure (5xx / network) on the confirm path no longer renders chat.confirmFail",
  );
  assert.ok(
    !new RegExp('state === "scopeRequired"[\\s\\S]{0,200}?chat\\.confirmFail').test(page),
    'the scope-required card renders chat.confirmFail — the card would tell a user with no workspace ' +
      'to "try again", which cannot work',
  );
  // The card must say what it NEEDS rather than wearing the red "did not go
  // through" badge over a request the server never ran.
  assert.match(
    page,
    /state === "scopeRequired"[\s\S]{0,120}?t\("chat\.proposalBadgeScopeRequired"\)/,
    'the scope-required card falls back to chat.proposalBadgeFailed ("Did not go through") — nothing ' +
      'was attempted, so the badge would claim a failure that did not happen',
  );
  assert.match(
    page,
    /type ProposalState\s*=[\s\S]{0,400}?"scopeRequired"/,
    "ProposalState has no scopeRequired member — the setState above would not type-check",
  );

  // The two strings must be DIFFERENT sentences, or "does not render
  // chat.confirmFail" would be satisfied by a key that says the same thing.
  const scopeCopy = DICT["chat.scopeRequired"].en;
  const failCopy = DICT["chat.confirmFail"].en;
  assert.notEqual(
    scopeCopy,
    failCopy,
    "the scope refusal and the generic confirm failure carry the SAME English sentence — the point of " +
      "the split was that they say different things, so one of the two has lost its copy",
  );
  assert.doesNotMatch(
    scopeCopy,
    /try again/i,
    `the scope refusal tells the user to "try again" ("${scopeCopy}") — retrying without a workspace ` +
      `fails identically; the actionable instruction is to choose a workspace`,
  );
  for (const lang of LANGS) {
    const v = (DICT["chat.proposalBadgeScopeRequired"] as Record<string, string>)[lang];
    assert.ok(
      typeof v === "string" && v.trim() !== "",
      `chat.proposalBadgeScopeRequired.${lang} is empty — the badge would render a blank pill`,
    );
  }

  // 409 (stale) and 429 (rate-limited) keep their own distinct copy: the two
  // other non-failure refusals this card names as unchanged. They are asserted on
  // the OUTCOME branch now, since the status test lives in lib/chatConfirm.ts
  // (whose behaviour chatConfirm.test.ts executes).
  const decide = page.slice(page.indexOf("async function decide("));
  assert.match(
    decide,
    /outcome === "stale"[\s\S]{0,300}?setState\(msgIndex,\s*p\.token,\s*"stale"\)/,
    "the stale outcome no longer marks the card stale — a spent proposal would keep offering buttons " +
      "the server refuses",
  );
  assert.match(
    decide,
    /outcome === "rateLimited"[\s\S]{0,300}?t\("chat\.confirmRateLimited"\)/,
    "the rate-limited outcome no longer renders chat.confirmRateLimited — a rate-limited user would " +
      "get the generic failure instead of the limit copy",
  );
});
