// The stored conversations, as the chat page reads them off the wire.
//
// WHY THIS FILE EXISTS. The transcript used to be a browser-only codec (see
// chatTranscript.test.ts for what is left of it). It is now a server resource:
// GET /v1/rest/chat/sessions returns the sidebar, GET …/{id} returns one
// conversation and its turns, and POST …/chat appends the exchange. Everything
// that turns that payload into state lives in lib/chatSessions.ts, and it is the
// only thing standing between a changed server shape and a page that renders
// "undefined" into the transcript.
//
// WHAT IS PINNED, and the failure each guard prevents:
//
//   1. EXECUTED — the list. One unreadable row is DROPPED, not the whole list:
//      one corrupt entry must not hide the conversations around it. A row with
//      no id is unreadable (nothing to open); a title that is not a string is
//      unreadable (it would render as "0" or "[object Object]").
//   2. EXECUTED — the conversation. ALL OR NOTHING, like the old codec: a turn
//      that is not a {role, content} pair makes the whole conversation
//      unreadable and the page starts empty. Half a transcript reads as a
//      conversation that lost its middle.
//   3. EXECUTED — the steps. Only `called` steps are restored. A `proposed` step
//      belonged to a confirmation card whose single-use token is never stored,
//      and a `refused` step reads as an amber block the page cannot rebuild, so
//      either one would render a chip for an action with no card behind it.
//   4. EXECUTED — the label and the delete-then-open decision: an unnamed
//      conversation shows the page's own "New conversation" wording (never a
//      blank row), and after a delete the page opens the conversation that was
//      open, else the most recently used remaining one, else none.
//   5. SOURCE — the page wires all of it: load, open, create, rename, delete.
//      A perfect codec nobody calls is the original bug with a nicer name.
//
// WHAT IT DOES NOT COVER: no React/DOM runner exists here, so this file cannot
// mount the page. The end-to-end behaviour was driven in a real browser and is
// pinned by e2e/chat-history.spec.ts.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  parseSessionDetail,
  parseSessionList,
  readSession,
  sessionAfterDelete,
  sessionLabel,
  type ChatSession,
} from "./chatSessions.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const ROW = {
  id: "8c1b2c3d-0000-4000-8000-aaaaaaaaaaaa",
  title: "which of my forms are live?",
  updated_at: "2026-11-07T10:00:00Z",
  message_count: 2,
  archived: false,
};

test("the guard is reading the real page (it cannot pass vacuously)", () => {
  const page = read("../app/chat/page.tsx");
  assert.ok(
    page.includes("export default function ChatPage"),
    "app/chat/page.tsx is not the chat page source — the guard's anchor moved",
  );
});

// ── 1. The list ──────────────────────────────────────────────────────────────

test("one unreadable row is dropped; the conversations around it survive", () => {
  const rows = parseSessionList({
    sessions: [
      ROW,
      null,
      { title: "no id — nothing to open" },
      { id: "x", title: 7 },
      { id: "y", title: "ok", message_count: "2" },
      { ...ROW, id: "z", title: "" },
    ],
  });
  assert.deepEqual(
    rows.map((r) => r.id),
    [ROW.id, "y", "z"],
    "the list dropped a readable row, or kept an unreadable one",
  );
  // A message count that is not a number becomes 0 rather than NaN: the sidebar
  // never shows NaN, and 0 is the honest "we do not know how many" for a row the
  // rest of which is readable.
  assert.equal(rows[1].messageCount, 0);
  assert.equal(rows[0].messageCount, 2);
});

test("a payload that is not a session list is an empty list, not a crash", () => {
  for (const raw of [null, undefined, 42, "x", [], { sessions: "no" }, { sessions: {} }]) {
    assert.deepEqual(parseSessionList(raw), [], `a bad list payload was not treated as empty: ${JSON.stringify(raw)}`);
  }
});

test("readSession normalises a real row and refuses a malformed one", () => {
  const s = readSession(ROW);
  assert.deepEqual(s, {
    id: ROW.id,
    title: ROW.title,
    updatedAt: ROW.updated_at,
    messageCount: 2,
    archived: false,
  });
  assert.equal(readSession({ id: "" }), null);
  assert.equal(readSession({ id: "x", title: 12 }), null);
  assert.equal(readSession({ id: "x", title: "t", message_count: -3 })?.messageCount, 0);
  assert.equal(readSession({ id: "x", title: "t", message_count: 1.7 })?.messageCount, 1);
});

// ── 2. One conversation ──────────────────────────────────────────────────────

test("a conversation and its turns round-trip, oldest first", () => {
  const detail = parseSessionDetail({
    session: ROW,
    messages: [
      { id: "m1", role: "user", content: "which forms are live?", created_at: "t1" },
      { id: "m2", role: "assistant", content: "Two of them.", created_at: "t2", steps: [{ tool: "list_forms", status: "called" }] },
    ],
  });
  assert.ok(detail);
  assert.equal(detail.session.id, ROW.id);
  assert.deepEqual(
    detail.turns.map((t) => [t.role, t.content]),
    [
      ["user", "which forms are live?"],
      ["assistant", "Two of them."],
    ],
  );
  assert.deepEqual(detail.turns[1].steps, [{ tool: "list_forms", status: "called" }]);
});

test("an unreadable turn makes the WHOLE conversation unreadable", () => {
  const bad = [
    { session: ROW, messages: [{ role: "user", content: "hi" }, { role: "tool", content: "x" }] },
    { session: ROW, messages: [{ role: "user", content: "hi" }, { role: "user" }] },
    { session: ROW, messages: [{ role: "assistant", content: null }] },
    { session: ROW, messages: "no" },
    { session: null, messages: [] },
    {},
  ];
  for (const raw of bad) {
    assert.equal(parseSessionDetail(raw), null, `half a conversation was restored: ${JSON.stringify(raw)}`);
  }
});

// ── 3. The tool trail ────────────────────────────────────────────────────────

test("only steps that RAN are restored — no chip for a card whose token is gone", () => {
  const detail = parseSessionDetail({
    session: ROW,
    messages: [
      {
        id: "m",
        role: "assistant",
        content: "answer",
        steps: [
          { tool: "list_forms", status: "called" },
          { tool: "upsert_form", status: "proposed" },
          { tool: "delete_form", status: "refused" },
          { tool: "", status: "called" },
          { tool: "list_sales" },
          "junk",
        ],
      },
    ],
  });
  assert.ok(detail);
  assert.deepEqual(
    detail.turns[0].steps,
    [{ tool: "list_forms", status: "called" }],
    "a proposed/refused step was restored — the page would render an action that cannot exist",
  );
});

test("a turn with no usable steps has no steps field at all", () => {
  const detail = parseSessionDetail({
    session: ROW,
    messages: [{ id: "m", role: "assistant", content: "answer", steps: [{ tool: "x", status: "proposed" }] }],
  });
  assert.ok(detail);
  assert.equal(detail.turns[0].steps, undefined);
});

// ── 4. The label and the post-delete open ────────────────────────────────────

test("an unnamed conversation is labelled with the page's own wording, never a blank row", () => {
  assert.equal(sessionLabel({ title: "" }, "New conversation"), "New conversation");
  assert.equal(sessionLabel({ title: "   " }, "New conversation"), "New conversation");
  assert.equal(sessionLabel({ title: "  Sales this week " }, "New conversation"), "Sales this week");
});

test("after a delete, the open conversation stays open; else the newest remaining opens", () => {
  const rows: ChatSession[] = [
    { id: "a", title: "a", updatedAt: "3", messageCount: 2, archived: false },
    { id: "b", title: "b", updatedAt: "2", messageCount: 2, archived: false },
    { id: "c", title: "c", updatedAt: "1", messageCount: 2, archived: false },
  ];
  // Deleting somebody else's row must not move the reader.
  assert.equal(sessionAfterDelete(rows, "c", "a"), "a");
  // Deleting the open row opens the newest remaining one, in the SERVER's order.
  assert.equal(sessionAfterDelete(rows, "a", "a"), "b");
  // Deleting the last row opens nothing.
  assert.equal(sessionAfterDelete([rows[0]], "a", "a"), null);
  // An open id that is somehow no longer in the list falls back to the newest.
  assert.equal(sessionAfterDelete(rows, "c", "gone"), "a");
});

// ── 5. The page wires all of it ──────────────────────────────────────────────

test("the page loads the list, opens a conversation, creates one, renames and deletes", () => {
  const page = read("../app/chat/page.tsx");
  assert.match(page, /parseSessionList\(r\.data\)/, "the page never parses the sidebar payload");
  assert.match(page, /parseSessionDetail\(r\.data\)/, "the page never parses a conversation payload");
  assert.match(page, /\/api\/rest\/chat\/sessions\$\{workspaceQuery\(\)\}/, "the page never calls the conversation collection — the sidebar and New conversation have nothing behind them");
  assert.match(page, /method:\s*"POST"/, "the page never creates a conversation");
  assert.match(page, /method:\s*"PATCH"/, "the page never renames a conversation");
  assert.match(page, /method:\s*"DELETE"/, "the page never deletes a conversation");
  // The exchange itself must carry the conversation id, or nothing is stored.
  assert.match(page, /session_id:\s*sessionId/, "the send path does not name the conversation — the exchange is answered but never stored");
});
