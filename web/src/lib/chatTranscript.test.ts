// The LEGACY browser transcript store: what must still happen to the keys the
// old version wrote, and what must no longer happen at all.
//
// THE CHANGE. The conversation used to live in `localStorage` under
// `jf.chat.v1:<userId>:<workspaceSlug>` and the sign-out sweep deleted it — so
// signing out destroyed every conversation the person had. The store is now the
// server (api/internal/dbstore/chat_session.go, read by lib/chatSessions.ts and
// pinned by chatSessions.test.ts), scoped to the person AND the workspace, and
// signing out deletes nothing.
//
// WHAT THIS FILE STILL PINS, and why the module survives:
//
//   1. EXECUTED — the sweep. A browser that used the old version still holds
//      those keys. A shared device must not hand the next person the previous
//      person's conversation, so `clearPerUserStorage` (Shell.signOut) still
//      walks the prefix and removes what it finds. It must remove EVERY one
//      (enumerate first, then delete: removing while walking a live localStorage
//      shifts every later index and silently skips entries) and nothing else.
//   2. EXECUTED — a null or throwing storage is a no-op, not a crash on the
//      sign-out path.
//   3. SOURCE — nothing writes the family any more. If a save/load pair comes
//      back to this module while the server copy exists, there are two stores
//      again and one of them will disagree; the guard fails rather than letting
//      the page silently drift back to browser-only history.
//   4. SOURCE — the page reads and writes history through the API only, and the
//      sign-out sweep is still wired (Shell.signOut → clearPerUserStorage).
//   5. COPY — the control and the sidebar labels exist in all five locales, and
//      the docs page says the conversation is stored on the server and survives
//      signing out (DOCUMENTATION PARITY: a user must not be told the opposite of
//      what the code does).
//
// WHAT IT DOES NOT COVER (stated honestly): no React/DOM runner exists here, so
// this file cannot mount the page. The behaviour — a saved conversation shown
// after a fresh login, a second conversation left in the sidebar — was driven in
// a real browser and is asserted end-to-end by e2e/chat-history.spec.ts.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { CHAT_TRANSCRIPT_PREFIX, clearChatTranscripts } from "./chatTranscript.ts";
import { clearPerUserStorage } from "./workspace.ts";
import { DICT, LANGS } from "../components/i18nDict.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

/** A localStorage stand-in that behaves like the real one, including index shifting. */
function fakeStorage(keys: string[]) {
  const data = new Map<string, string>(keys.map((k) => [k, "x"]));
  const removed: string[] = [];
  return {
    removed,
    get length() {
      return data.size;
    },
    key(i: number) {
      return [...data.keys()][i] ?? null;
    },
    removeItem(k: string) {
      removed.push(k);
      data.delete(k);
    },
  };
}

/** Run `fn` with a module-level `localStorage` the workspace sweep can reach. */
function withGlobalStorage<T>(keys: string[], fn: (removed: string[]) => T): T {
  const storage = fakeStorage(keys);
  const g = globalThis as unknown as Record<string, unknown>;
  const prev = g.localStorage;
  g.localStorage = storage;
  try {
    return fn(storage.removed);
  } finally {
    g.localStorage = prev;
  }
}

test("the sweep removes every legacy transcript and nothing else", () => {
  const storage = fakeStorage([
    `${CHAT_TRANSCRIPT_PREFIX}user-a:acme`,
    `${CHAT_TRANSCRIPT_PREFIX}user-a:-`,
    `${CHAT_TRANSCRIPT_PREFIX}user-b:acme`,
    "jf_workspace",
    "jf.dashboard.settings",
    "unrelated",
  ]);
  clearChatTranscripts(storage);
  assert.deepEqual(
    storage.removed.sort(),
    [
      `${CHAT_TRANSCRIPT_PREFIX}user-a:-`,
      `${CHAT_TRANSCRIPT_PREFIX}user-a:acme`,
      `${CHAT_TRANSCRIPT_PREFIX}user-b:acme`,
    ],
    "the sweep did not remove exactly the legacy chat keys",
  );
  // Keys that are NOT transcripts must survive: clearing the previous person's
  // workspace selection is the neighbouring module's job, not this one's.
  assert.equal(storage.length, 3);
  assert.equal(storage.key(0), "jf_workspace");
});

test("the sweep enumerates BEFORE removing, so a shifting index cannot skip a key", () => {
  // A real localStorage shifts every index when an entry is removed. A sweep that
  // removed while walking would visit 0, then 1 of the SHRUNK list, and leave
  // every second transcript behind — which is exactly how the previous person's
  // conversation would survive the sweep meant to delete it.
  const storage = fakeStorage([
    `${CHAT_TRANSCRIPT_PREFIX}a`,
    "keep",
    `${CHAT_TRANSCRIPT_PREFIX}b`,
    "keep2",
    `${CHAT_TRANSCRIPT_PREFIX}c`,
  ]);
  clearChatTranscripts(storage);
  assert.deepEqual(storage.removed.sort(), [
    `${CHAT_TRANSCRIPT_PREFIX}a`,
    `${CHAT_TRANSCRIPT_PREFIX}b`,
    `${CHAT_TRANSCRIPT_PREFIX}c`,
  ]);
  assert.deepEqual(
    [0, 1].map((i) => storage.key(i)),
    ["keep", "keep2"],
    "a non-transcript key was removed by the sweep",
  );
});

test("a missing or throwing storage does not break the sign-out path", () => {
  assert.doesNotThrow(() => clearChatTranscripts(null));
  const throwing = {
    get length(): number {
      throw new Error("storage blocked");
    },
    key(): string | null {
      throw new Error("storage blocked");
    },
    removeItem(): void {
      throw new Error("storage blocked");
    },
  };
  assert.doesNotThrow(() => clearChatTranscripts(throwing));
});

test("sign-out still drives the sweep (Shell.signOut → clearPerUserStorage)", () => {
  // The keys carry a user id, so a fixed list cannot name them; a prefix sweep is
  // the only shape that covers a user this build has never seen.
  const legacy = `${CHAT_TRANSCRIPT_PREFIX}someone-else-entirely:ws-x`;
  withGlobalStorage([legacy, "jf_workspace", "jf_lang", "keep-me"], (removed) => {
    clearPerUserStorage();
    assert.ok(removed.includes(legacy), "clearPerUserStorage did not sweep the legacy transcript that was actually there");
    assert.ok(removed.includes("jf_workspace"), "the fixed per-user keys stopped being cleared");
    assert.ok(!removed.includes("keep-me"), "the sweep removed a key outside its families");
  });
  const shell = read("../components/Shell.tsx");
  const signOutAt = shell.indexOf("signOut");
  assert.ok(signOutAt >= 0, "Shell.signOut moved — retarget this guard rather than deleting it");
  const signOut = shell.slice(signOutAt);
  assert.ok(
    signOut.includes("clearPerUserStorage()"),
    "Shell.signOut no longer clears the per-user storage — legacy transcripts survive sign-out",
  );
  assert.match(
    read("./workspace.ts"),
    /clearChatTranscripts\(localStorage\)/,
    "clearPerUserStorage stopped sweeping the legacy chat keys — a browser that used the old version keeps the conversation",
  );
});

test("nothing writes the legacy family any more — there is ONE store, on the server", () => {
  const mod = read("./chatTranscript.ts");
  // The module may export only the prefix and the sweep. A save/load pair is the
  // two-store shape this card removed: a browser copy that survives sign-out
  // would be visible to the next person at a shared device, and one cleared on
  // sign-out would lose the conversation the server just kept.
  for (const gone of [
    "saveTranscript",
    "loadTranscript",
    "clearTranscript",
    "chatTranscriptKey",
    "maySaveTranscript",
    "parseTranscript",
  ]) {
    assert.doesNotMatch(
      mod,
      new RegExp(`export\\s+(?:function|const)\\s+${gone}\\b`),
      `${gone} is back in lib/chatTranscript.ts — the browser is a store again, and the server copy is no longer the only one`,
    );
  }
  // The page must not reach into localStorage for the conversation either.
  const page = read("../app/chat/page.tsx");
  for (const gone of ["saveTranscript", "loadTranscript", "chatTranscriptKey", "clearTranscript"]) {
    assert.doesNotMatch(
      page,
      new RegExp(`\\b${gone}\\b`),
      `app/chat/page.tsx still calls ${gone} — the transcript is being written to (or read from) the browser again`,
    );
  }
});

test("the page writes and reads history through the API, and nothing is cleared on a dead session", () => {
  const page = read("../app/chat/page.tsx");
  assert.match(
    page,
    /\/api\/rest\/chat\/sessions/,
    "the chat page never calls the stored-conversation API — the sidebar and the restore would have nothing behind them",
  );
  // Signing out must not delete anything: the conversation is on the server and
  // the next login has to show it. A bounce to /login is not the place to clear
  // a store that is no longer the browser's.
  const goToLogin = page.slice(
    page.indexOf("const goToLogin = useCallback"),
    page.indexOf("}, [router]);"),
  );
  assert.ok(goToLogin.length > 0, "goToLogin moved — retarget this guard rather than deleting it");
  assert.doesNotMatch(
    goToLogin,
    /clearChatTranscripts\(browserStorage\(\)\)/,
    "the dead-session bounce still sweeps the transcript store — but the conversation is on the server now, so this is a leftover that reads as data loss",
  );
});

test("the history labels exist in all five locales", () => {
  const keys = [
    "chat.newConversation",
    "chat.newConversationHint",
    "chat.history",
    "chat.historyEmpty",
    "chat.rename",
    "chat.renameSave",
    "chat.delete",
    "chat.deleteConfirm",
    "chat.historyLoadFail",
    "chat.historyCreateFail",
    "chat.historySaveFail",
  ];
  for (const key of keys) {
    const row = (DICT as unknown as Record<string, Record<string, string>>)[key];
    assert.ok(row, `${key} is missing from the dictionary`);
    for (const lang of LANGS) {
      assert.equal(
        typeof row[lang],
        "string",
        `${key} has no ${lang} string — the control would render its raw key in that locale`,
      );
      assert.ok(row[lang].trim().length > 0, `${key}.${lang} is empty`);
    }
  }
});

test("the docs page says where the conversation lives and that signing out keeps it", () => {
  // AGENTS.md DOCUMENTATION PARITY: the panel's "?" link points here, and the
  // page must not describe the browser-only behaviour the code no longer has.
  const docs = read("../app/docs/chat/page.tsx");
  assert.match(
    docs,
    /id: "remembering"/,
    "the docs page has no section for stored conversations",
  );
  assert.match(
    docs,
    /saved to your JomForm account/i,
    "the docs do not say the conversation is stored on the server",
  );
  assert.match(
    docs,
    /sign out and sign back in|signing out (?:does not|keeps)/i,
    "the docs do not say that signing out keeps the conversation",
  );
  assert.match(docs, /history sidebar/i, "the docs do not name the sidebar");
  assert.match(docs, /New conversation/, "the docs do not name the control that starts a new conversation");
  assert.doesNotMatch(
    docs,
    /kept in this browser|Nothing of the conversation is stored on the server/i,
    "the docs still claim the conversation is browser-only — the opposite of what the code does",
  );
});
