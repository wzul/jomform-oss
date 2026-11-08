// The SSE half of the assistant's answer, on the browser side (card t_148d7100).
//
// WHY THIS IS A MODULE AND NOT INLINE IN THE PAGE. The page is 1,000 lines of
// rendering and decisions; a frame parser buried in it can only be tested by
// driving a browser, and the cases that break a parser are the ones a browser
// hides: a frame split ACROSS two network chunks, several frames arriving in ONE
// chunk, CRLF line endings, and a `data:` line that arrives without its trailing
// blank line. Those are pure-input cases, so they live here and are executed by
// src/lib/chatStream.test.ts with node's test runner.
//
// THE WIRE, as the API emits it (api/internal/httpapi/rest_chat_stream.go): one
// `data:` line per frame, the type INSIDE the JSON payload:
//
//	data: {"type":"step","tool":"list_forms"}
//	data: {"type":"delta","text":"You have "}
//	data: {"type":"retract"}
//	data: {"type":"done","result":{…}}
//	data: {"type":"error","code":"unavailable"}
//
// A proxy may insert comment lines (`: ping`) or blank lines; both are ignored.
// Anything that is not a parseable frame is DROPPED, not thrown: a truncated tail
// at the end of a stream must not take the whole answer down with it.

export type ChatStreamEvent =
  | { type: "step"; tool: string }
  | { type: "delta"; text: string }
  | { type: "retract" }
  | { type: "done"; result: unknown }
  | { type: "error"; code?: string };

/** A parser instance keeps the unfinished tail of the previous chunk. */
export type ChatStreamParser = {
  /** Feed one decoded chunk; returns the frames it completed. */
  push(chunk: string): ChatStreamEvent[];
};

function parseFrame(payload: string): ChatStreamEvent | null {
  let raw: unknown;
  try {
    raw = JSON.parse(payload);
  } catch {
    return null;
  }
  if (typeof raw !== "object" || raw === null) return null;
  const o = raw as Record<string, unknown>;
  switch (o.type) {
    case "step":
      return typeof o.tool === "string" && o.tool ? { type: "step", tool: o.tool } : null;
    case "delta":
      // An empty fragment carries no information and would put a needless render
      // through the page on every keep-alive-shaped frame.
      return typeof o.text === "string" && o.text !== "" ? { type: "delta", text: o.text } : null;
    case "retract":
      return { type: "retract" };
    case "done":
      return o.result && typeof o.result === "object"
        ? { type: "done", result: o.result }
        : null;
    case "error":
      return { type: "error", code: typeof o.code === "string" ? o.code : undefined };
    default:
      // An unknown type is a NEWER server talking to an older page. Ignoring it
      // is what keeps this page working across a deploy; throwing would break
      // the answer for a frame it does not need.
      return null;
  }
}

export function createChatStreamParser(): ChatStreamParser {
  let buffer = "";
  return {
    push(chunk: string): ChatStreamEvent[] {
      buffer += chunk;
      const out: ChatStreamEvent[] = [];
      // Frames are newline-delimited. Keep the last (possibly partial) line in
      // the buffer — that is the whole point of the state.
      let nl = buffer.indexOf("\n");
      while (nl >= 0) {
        const line = buffer.slice(0, nl).replace(/\r$/, "");
        buffer = buffer.slice(nl + 1);
        if (line.startsWith("data:")) {
          const payload = line.slice("data:".length).trim();
          if (payload) {
            const ev = parseFrame(payload);
            if (ev) out.push(ev);
          }
        }
        nl = buffer.indexOf("\n");
      }
      return out;
    },
  };
}

/**
 * The text the streamed answer has produced so far.
 *
 * `retract` EMPTIES it, and that is deliberate: a round that ends by calling a
 * tool was not an answer, so the model's preamble ("let me check") must not stay
 * on screen as the assistant's words. The server sends the retract frame for
 * exactly that case (api/internal/chat/stream.go).
 *
 * A `step` does NOT clear it on its own — the server sends an explicit retract
 * before the step when there is anything to withdraw. Clearing here as well
 * would mean a future server that starts emitting steps at another point could
 * silently delete text the user was reading.
 */
export function applyChatStreamEvent(text: string, ev: ChatStreamEvent): string {
  switch (ev.type) {
    case "delta":
      return text + ev.text;
    case "retract":
      return "";
    default:
      return text;
  }
}
