// Chat transcript retention, in this browser only.
//
// THE DEFECT THIS EXISTS FOR: the assistant page held the transcript in React
// state and wrote it nowhere, so a refresh, a tab close, or navigating away and
// back destroyed the conversation — including the answers and the proposal cards
// the user had just been reading. Nothing in the request carried it either: the
// page sends the visible turns as `history` on each call, so the server never
// holds a copy to re-read.
//
// THE SHAPE OF THE FIX, and why each half is here rather than in the page:
//
//   1. KEYED BY USER **AND** WORKSPACE. One shared key would hand a second
//      workspace on the same browser the first workspace's conversation. So the
//      key is `jf.chat.v1:<userId>:<workspaceSlug>`, and an EMPTY userId yields
//      NO key at all — a value nobody can be scoped to is exactly the shared key
//      this rule forbids, so the page simply does not persist until the session
//      says who it is.
//   2. ONLY WHAT IS ON SCREEN. The stored value is the same array the page sends
//      as `history`: role + content. A proposal card is deliberately NOT stored:
//      its token is a single-use capability with a five-minute life, so keeping
//      it in localStorage would leave a spent write token at rest and restore a
//      card whose buttons cannot work.
//   3. CAP AT BOTH ENDS. Restoring and storing both take the server's
//      `max_history_turns` so the retained transcript is never longer than the
//      window the model is given — the same rule the send path follows.
//   4. CORRUPT MEANS EMPTY. A value that is not a JSON array of well-formed
//      {role, content} turns is discarded whole and the page starts empty. A
//      half-readable value is not partially restored: a transcript is a
//      conversation, and showing every other turn of one is worse than showing
//      none.
//
// Everything here is pure over a storage object, so `node --test` executes it
// (this repo has no DOM runner). The page's wiring — hydrate into `setMsgs`,
// persist on change, clear on "New conversation" and on a bounce to /login — is
// pinned from source by chatTranscript.test.ts, and driven in a real browser in
// the card's handoff.

export type TranscriptTurn = {
  role: "user" | "assistant";
  content: string;
};

/**
 * The storage surface this module uses. Structural, not `Storage`, so a test can
 * pass a stand-in without a DOM — and so the enumeration needed by
 * clearChatTranscripts is part of the contract rather than an implicit assumption.
 */
export type TranscriptStorage = {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
  readonly length: number;
  key(index: number): string | null;
};

/**
 * The family prefix every transcript key starts with.
 *
 * Exported because the sign-out sweep needs it: the keys are per user, so they
 * cannot be listed statically (workspace.ts's PER_USER_STORAGE_KEYS is a fixed
 * list, and a fixed list cannot name `jf.chat.v1:<uuid>:<slug>`). The sweep
 * removes every key under this prefix instead — ONE definition, so a rename here
 * cannot leave sign-out clearing a prefix nothing writes.
 */
export const CHAT_TRANSCRIPT_PREFIX = "jf.chat.v1:";

/** The browser's localStorage, or null when there is none (SSR, or a storage that throws). */
export function browserStorage(): TranscriptStorage | null {
  try {
    if (typeof window === "undefined") return null;
    return window.localStorage ?? null;
  } catch {
    // A storage that throws on ACCESS (a locked-down browser) is treated as none:
    // the conversation then simply does not survive a reload, which is the
    // pre-fix behaviour rather than a page that crashes.
    return null;
  }
}

/**
 * The storage key for one user in one workspace.
 *
 * Returns "" when there is no signed-in user id: a transcript that cannot be
 * scoped to a person must not be stored under a key two people would share.
 *
 * An empty workspace slug becomes "-" rather than collapsing the key back to the
 * user alone. Two workspaces must never share a transcript, and "no workspace
 * selected" is its own state, so it gets its own segment — the unscoped
 * conversation cannot be shown later as if it belonged to the workspace the user
 * happens to select next.
 */
export function chatTranscriptKey(userId: string, workspaceSlug: string): string {
  const uid = (userId || "").trim();
  if (!uid) return "";
  const ws = (workspaceSlug || "").trim();
  return `${CHAT_TRANSCRIPT_PREFIX}${uid}:${ws || "-"}`;
}

/**
 * One turn, or null when the value is not a turn this page could render.
 *
 * Strict on purpose: `role` must be exactly "user" or "assistant" (the two the
 * page renders and the two the server accepts in `history` — a forged "system"
 * turn is dropped server-side anyway, so restoring one would only put a row on
 * screen that is not part of the conversation), and `content` must be a string.
 */
function readTurn(v: unknown): TranscriptTurn | null {
  if (!v || typeof v !== "object") return null;
  const t = v as { role?: unknown; content?: unknown };
  if (t.role !== "user" && t.role !== "assistant") return null;
  if (typeof t.content !== "string") return null;
  return { role: t.role, content: t.content };
}

/**
 * Parse a stored value into turns. ALL OR NOTHING: any unreadable element makes
 * the whole value unreadable, and the caller starts empty.
 */
export function parseTranscript(raw: string | null): TranscriptTurn[] {
  if (!raw) return [];
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return [];
  }
  if (!Array.isArray(parsed)) return [];
  const out: TranscriptTurn[] = [];
  for (const item of parsed) {
    const turn = readTurn(item);
    if (!turn) return [];
    out.push(turn);
  }
  return out;
}

/**
 * Keep at most the last `cap` turns.
 *
 * `cap` 0 or below keeps NOTHING, never the whole transcript: `.slice(-0)` is
 * the entire array, which is the opposite of a cap and has shipped as a bug in
 * this page once already (the send path's own comment records it). Callers pass
 * the server's `max_history_turns` (or the page's pre-probe fallback) so this is
 * the cheap direction to fail in.
 */
export function capTranscript(turns: ReadonlyArray<TranscriptTurn>, cap: number): TranscriptTurn[] {
  const keep = Math.trunc(cap);
  if (!Number.isFinite(keep) || keep < 1) return [];
  return turns.slice(-keep);
}

/** The stored transcript for `key`, capped, or [] when absent/unreadable. */
export function loadTranscript(
  storage: TranscriptStorage | null,
  key: string,
  cap: number,
): TranscriptTurn[] {
  if (!storage || !key) return [];
  let raw: string | null = null;
  try {
    raw = storage.getItem(key);
  } catch {
    return [];
  }
  return capTranscript(parseTranscript(raw), cap);
}

/**
 * Write the transcript for `key`.
 *
 * An EMPTY transcript REMOVES the key rather than storing "[]": a key that says
 * "this person has a conversation here" should exist only while there is one.
 * Both the write and the removal are guarded — a full or unavailable storage
 * must not throw out of a React effect and take the page down.
 */
export function saveTranscript(
  storage: TranscriptStorage | null,
  key: string,
  turns: ReadonlyArray<TranscriptTurn>,
  cap: number,
): void {
  if (!storage || !key) return;
  const kept = capTranscript(turns, cap);
  try {
    if (kept.length === 0) {
      storage.removeItem(key);
      return;
    }
    storage.setItem(key, JSON.stringify(kept));
  } catch {
    // Storage unavailable or full: the conversation stays on screen and simply
    // does not survive a reload.
  }
}

/**
 * Whether the save may write for `key` right now.
 *
 * THE DEFECT THIS EXISTS FOR, measured in a browser: on first paint the page's
 * key changes under it — the session bundle answers, then the shell publishes the
 * workspace it selected, so the key can go "" → "<user>:-" → "<user>:<slug>" in
 * two commits with no user action. A save effect gated only on "the restore has
 * run once" then wrote with the turns of the PREVIOUS key still in state: the
 * transient `-` key was written empty and the stored conversation under it was
 * deleted, and a stale transcript could be written under the new key. Both are
 * data loss disguised as a sync.
 *
 * So the rule is not "have we restored yet" but "do the turns in state belong to
 * THIS key". `shownKey` is the key the current transcript was restored for (the
 * page keeps it in the same state update as the turns, so the two cannot
 * disagree in a render); `key` is the key the page is looking at. A mismatch
 * means the render is mid-transition and the state is somebody else's — never
 * write it.
 */
export function maySaveTranscript(shownKey: string | null, key: string): boolean {
  return key !== "" && shownKey === key;
}

/** Drop ONE conversation (the "New conversation" action). */
export function clearTranscript(storage: TranscriptStorage | null, key: string): void {
  if (!storage || !key) return;
  try {
    storage.removeItem(key);
  } catch {
    // storage unavailable — nothing was stored either
  }
}

/**
 * Drop EVERY chat transcript in this browser.
 *
 * This is the sign-out / dead-session sweep. The keys are per user, so they are
 * found by PREFIX rather than by name: the previous person's conversation must
 * not be handed to the next person at a shared device. Enumerating first and
 * removing afterwards matters — removing while walking a live localStorage
 * shifts every later index and silently skips entries.
 */
export function clearChatTranscripts(storage: TranscriptStorage | null): void {
  if (!storage) return;
  try {
    const doomed: string[] = [];
    const n = storage.length;
    for (let i = 0; i < n; i++) {
      const key = storage.key(i);
      if (key && key.startsWith(CHAT_TRANSCRIPT_PREFIX)) doomed.push(key);
    }
    for (const key of doomed) storage.removeItem(key);
  } catch {
    // storage unavailable — nothing was stored either
  }
}
