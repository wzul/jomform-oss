// The LEGACY browser transcript store: the sweep that erases copies left behind
// by the version that kept the conversation in localStorage.
//
// THE STORE ITSELF IS GONE. A chat conversation is stored on the server now
// (api/internal/dbstore/chat_session.go, read by lib/chatSessions.ts), scoped to
// the person AND the workspace, so a logout, a new login or a second device keeps
// it. The old version wrote `jf.chat.v1:<userId>:<workspaceSlug>` into
// localStorage and deleted it on sign-out, which is exactly the data loss this
// module exists to clean up after.
//
// WHY THIS MODULE SURVIVES AT ALL. Browsers that used the old version still hold
// those keys, and a shared device must not hand the next person the previous
// person's conversation. The sign-out sweep (lib/workspace.ts
// clearPerUserStorage) therefore still walks the prefix and removes what it finds,
// even though nothing writes it any more.
//
// The keys are per USER, so they cannot be listed statically — the sweep finds
// them by PREFIX. ONE definition of the prefix lives here, so a future rename
// cannot leave sign-out clearing a prefix nothing writes.

/** The family prefix every legacy transcript key starts with. */
export const CHAT_TRANSCRIPT_PREFIX = "jf.chat.v1:";

/**
 * The storage surface the sweep needs. Structural, not `Storage`, so a test can
 * pass a stand-in without a DOM — and so the enumeration the sweep depends on is
 * part of the contract rather than an implicit assumption.
 */
export type TranscriptStorage = {
  removeItem(key: string): void;
  readonly length: number;
  key(index: number): string | null;
};

/**
 * Drop EVERY legacy chat transcript in this browser, and nothing else.
 *
 * This is the sign-out / dead-session sweep. Enumerating first and removing
 * afterwards matters: removing while walking a live localStorage shifts every
 * later index and silently skips entries, which is how the previous person's
 * conversation would survive the sweep meant to delete it.
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
