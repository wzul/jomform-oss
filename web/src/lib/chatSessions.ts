// The chat's stored conversations, as the page reads them.
//
// WHY THIS MOVED OFF THE BROWSER. The transcript used to live in localStorage
// under a per-user, per-workspace key, and the sign-out sweep deleted it — so
// logging out (or a session expiring, or picking up another device) destroyed the
// conversation. The assistant's history is the user's own work, so it is stored
// on the server and read back here; there is exactly ONE copy, and it is not this
// browser's.
//
// THE JOB OF THIS MODULE is the boundary between the wire and the page, and it is
// pure so `node --test` can execute it (this repo has no DOM runner):
//
//   1. PARSE, DO NOT TRUST. The payload is validated shape by shape. A LIST drops
//      a single unreadable row rather than hiding every conversation over one bad
//      entry; a CONVERSATION is all-or-nothing, because showing every other turn
//      of a transcript is worse than showing none (the rule the old codec had,
//      kept because the reason did not change).
//   2. ONLY WHAT CAN BE RENDERED COMES BACK. A stored answer carries the tool
//      trail it left ("called" steps), and nothing else: a proposal card's token
//      is a single-use capability with a five-minute life and is never stored, so
//      a restored "proposed" or "refused" step would render a chip for an action
//      whose card cannot exist any more. Those are dropped here.
//   3. A CONVERSATION WITH NO NAME HAS ONE. An unnamed session (the first message
//      has not been sent yet) is labelled with the page's own "New conversation"
//      string, so the sidebar never shows an empty row.

export type ChatSession = {
  id: string;
  title: string;
  /** ISO timestamp. Kept as the wire sends it; the page does not re-sort blindly. */
  updatedAt: string;
  messageCount: number;
  archived: boolean;
};

/** One restored turn: the two roles the transcript renders, and the tool trail. */
export type RestoredTurn = {
  role: "user" | "assistant";
  content: string;
  steps?: Array<{ tool: string; status: string }>;
};

function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}

/** One session row, or null when the payload is not one this page can render. */
export function readSession(v: unknown): ChatSession | null {
  if (!v || typeof v !== "object") return null;
  const o = v as Record<string, unknown>;
  const id = str(o.id);
  if (!id) return null;
  // A title that is missing is "" (unnamed), which the page labels; a title that
  // is a NUMBER is a payload this page does not understand, so the row is dropped
  // rather than rendered as "0".
  if (o.title !== undefined && typeof o.title !== "string") return null;
  const count = typeof o.message_count === "number" && Number.isFinite(o.message_count)
    ? Math.max(0, Math.trunc(o.message_count))
    : 0;
  return {
    id,
    title: str(o.title),
    updatedAt: str(o.updated_at),
    messageCount: count,
    archived: o.archived === true,
  };
}

/**
 * The sidebar's rows. One unreadable row is dropped, not the whole list: a
 * corrupt entry must not hide the conversations around it.
 */
export function parseSessionList(raw: unknown): ChatSession[] {
  if (!raw || typeof raw !== "object") return [];
  const rows = (raw as { sessions?: unknown }).sessions;
  if (!Array.isArray(rows)) return [];
  const out: ChatSession[] = [];
  for (const row of rows) {
    const s = readSession(row);
    if (s) out.push(s);
  }
  return out;
}

/**
 * One restored turn, or null when it is not a turn the transcript can render.
 * Strict on `role` for the same reason the server is: "system" and "tool" are
 * constructed server-side and a forged one would be a row that is not part of the
 * conversation.
 */
function readTurn(v: unknown): RestoredTurn | null {
  if (!v || typeof v !== "object") return null;
  const o = v as Record<string, unknown>;
  if (o.role !== "user" && o.role !== "assistant") return null;
  if (typeof o.content !== "string") return null;
  const turn: RestoredTurn = { role: o.role, content: o.content };
  if (Array.isArray(o.steps)) {
    // Only the steps that RAN. A "proposed" step belonged to a confirmation card
    // (whose token is never stored) and a "refused" step reads as a refusal block
    // the page can no longer reconstruct, so neither is restored: a chip for an
    // action with no card, or an amber block with no sentence, is worse than a
    // plain answer.
    const steps: Array<{ tool: string; status: string }> = [];
    for (const s of o.steps) {
      if (!s || typeof s !== "object") continue;
      const step = s as Record<string, unknown>;
      if (typeof step.tool !== "string" || !step.tool) continue;
      if (step.status !== "called") continue;
      steps.push({ tool: step.tool, status: step.status });
    }
    if (steps.length > 0) turn.steps = steps;
  }
  return turn;
}

/**
 * One conversation and its turns. ALL OR NOTHING: an unreadable turn makes the
 * whole conversation unreadable, and the page starts a fresh one — half a
 * transcript reads as a conversation that lost its middle.
 */
export function parseSessionDetail(
  raw: unknown,
): { session: ChatSession; turns: RestoredTurn[] } | null {
  if (!raw || typeof raw !== "object") return null;
  const o = raw as { session?: unknown; messages?: unknown };
  const session = readSession(o.session);
  if (!session) return null;
  if (!Array.isArray(o.messages)) return null;
  const turns: RestoredTurn[] = [];
  for (const m of o.messages) {
    const t = readTurn(m);
    if (!t) return null;
    turns.push(t);
  }
  return { session, turns };
}

/**
 * The sidebar label. An unnamed conversation shows the page's own "New
 * conversation" wording rather than a blank row — a blank row is a control the
 * user cannot identify, and it is what an untouched new conversation would
 * otherwise leave behind.
 */
export function sessionLabel(session: Pick<ChatSession, "title">, untitled: string): string {
  const title = (session.title || "").trim();
  return title || untitled;
}

/**
 * Which conversation to open after one is deleted: the one that was open if it is
 * still there, else the most recently used remaining one, else none.
 *
 * The order is the server's (most recently used first), and it is PRESERVED here
 * rather than re-sorted by timestamp: the server's `updated_at` is the one place
 * that order is decided, and a second sort in the browser is how the two would
 * disagree after a rename.
 */
export function sessionAfterDelete(
  rows: ReadonlyArray<ChatSession>,
  deletedId: string,
  openId: string | null,
): string | null {
  const remaining = rows.filter((r) => r.id !== deletedId);
  if (openId && openId !== deletedId && remaining.some((r) => r.id === openId)) return openId;
  return remaining.length > 0 ? remaining[0].id : null;
}
