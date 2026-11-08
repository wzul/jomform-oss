// The streamed answer's frame parser (card t_148d7100).
//
// WHAT THIS FILE OWNS. The page renders whatever this parser produces, so the
// cases that break it are frame-shape cases, and none of them are reachable by
// "click the button and look" — a browser hands its chunks over at whatever
// boundaries the network and the proxy chose:
//
//   1. A FRAME SPLIT ACROSS TWO CHUNKS. The tail of one read is the head of the
//      next (`data: {"type":"del` + `ta","text":"hi"}`). A parser that parses per
//      chunk drops the answer's first fragment silently.
//   2. SEVERAL FRAMES IN ONE CHUNK. The opposite boundary, and the common one on
//      a fast local network — the whole answer can arrive in one read.
//   3. CRLF. SSE permits it, and a proxy may normalise to it.
//   4. A COMMENT OR A BLANK LINE (`: ping`, `\n\n`). SSE keep-alives are legal and
//      carry no payload.
//   5. GARBAGE. A truncated tail must not throw: the answer already on screen is
//      worth more than the frame that failed to parse.
//
// It also owns `applyChatStreamEvent`, the rule that turns frames into the
// bubble's text — including RETRACT, which is what keeps a tool round's discarded
// preamble out of the transcript.
import { test } from "node:test";
import assert from "node:assert/strict";

import { applyChatStreamEvent, createChatStreamParser } from "./chatStream.ts";

const frame = (o: unknown) => `data: ${JSON.stringify(o)}\n\n`;

test("several frames in one chunk are all delivered, in order", () => {
  const p = createChatStreamParser();
  const events = p.push(
    frame({ type: "step", tool: "list_forms" }) +
      frame({ type: "delta", text: "You have " }) +
      frame({ type: "delta", text: "3 forms." }) +
      frame({ type: "done", result: { reply: "You have 3 forms." } }),
  );
  assert.deepEqual(
    events.map((e) => e.type),
    ["step", "delta", "delta", "done"],
    "one read carried four frames and the parser must deliver all of them — a per-chunk split loses " +
      "every frame after the first, which is what a fast local network produces",
  );
  assert.equal(events[1].type === "delta" && events[1].text, "You have ");
});

test("a frame split across two chunks is delivered once, whole", () => {
  const p = createChatStreamParser();
  assert.deepEqual(p.push('data: {"type":"del'), [], "a partial frame must not be emitted");
  const events = p.push('ta","text":"hello"}\n\n');
  assert.deepEqual(events, [{ type: "delta", text: "hello" }]);
});

test("a split in the MIDDLE of the newline pair still completes the frame", () => {
  const p = createChatStreamParser();
  p.push('data: {"type":"delta","text":"x"}\n');
  // The blank line that terminates the frame arrives separately; the frame is
  // already complete at the first \n, which is what the parser keys on.
  const events = p.push("\n");
  assert.deepEqual(events, [], "the second newline is the separator, not another frame");
});

test("CRLF line endings are accepted", () => {
  const p = createChatStreamParser();
  const events = p.push('data: {"type":"delta","text":"a"}\r\n\r\n');
  assert.deepEqual(events, [{ type: "delta", text: "a" }]);
});

test("comments, blank lines and unknown frame types are ignored, not fatal", () => {
  const p = createChatStreamParser();
  const events = p.push(
    ": ping\n\n" +
      "\n" +
      frame({ type: "heartbeat", at: 1 }) +
      frame({ type: "delta", text: "ok" }),
  );
  assert.deepEqual(
    events,
    [{ type: "delta", text: "ok" }],
    "an unknown type is a NEWER server; dropping it keeps the page working across a deploy",
  );
});

test("a malformed or truncated frame is dropped and the stream continues", () => {
  const p = createChatStreamParser();
  const events = p.push('data: {"type":"delta",\n\n' + frame({ type: "delta", text: "after" }));
  assert.deepEqual(events, [{ type: "delta", text: "after" }]);
});

test("a delta with no text, or a step with no tool, is not an event", () => {
  const p = createChatStreamParser();
  const events = p.push(frame({ type: "delta" }) + frame({ type: "step" }) + frame({ type: "delta", text: "" }));
  assert.deepEqual(events, [], "a shape the page cannot render is not passed on as if it could");
});

test("an error frame carries its machine code", () => {
  const p = createChatStreamParser();
  assert.deepEqual(p.push(frame({ type: "error", code: "unavailable" })), [
    { type: "error", code: "unavailable" },
  ]);
});

test("text accumulates fragment by fragment and a retract empties it", () => {
  let text = "";
  text = applyChatStreamEvent(text, { type: "delta", text: "Let me " });
  text = applyChatStreamEvent(text, { type: "delta", text: "check." });
  assert.equal(text, "Let me check.");
  text = applyChatStreamEvent(text, { type: "retract" });
  assert.equal(
    text,
    "",
    "a round that then calls a tool was not an answer — leaving its preamble on screen makes a " +
      "discarded thought look like the reply",
  );
  text = applyChatStreamEvent(text, { type: "delta", text: "You have 3." });
  assert.equal(text, "You have 3.");
});

test("a step does not clear text on its own (the server sends retract explicitly)", () => {
  assert.equal(
    applyChatStreamEvent("answer so far", { type: "step", tool: "list_forms" }),
    "answer so far",
    "clearing on step would mean a future server emitting steps elsewhere silently deletes text " +
      "the user was reading",
  );
});
