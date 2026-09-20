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
  // The one write must be the feedback tool and nothing else.
  assert.match(
    goChatTools,
    /chatReadTools\s*=\s*\[\]string\{/,
    "the chat's read allowlist moved — this guard reads it to prove it contains no writer",
  );
  assert.ok(
    goChatTools.includes("ChatSubmitFeedbackTool") && goChatTools.includes("submit_feedback"),
    "chat_tools.go no longer declares the one write (submit_feedback)",
  );
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
