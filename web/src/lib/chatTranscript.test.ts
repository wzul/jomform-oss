// The assistant transcript survives a reload — keyed by user AND workspace, and
// revocable.
//
// THE DEFECT. `/chat` held the conversation in React state and wrote it nowhere,
// so a refresh, a tab close or a trip to another page destroyed it: the answers
// and the proposal cards the user had just been reading were gone, and nothing
// on the server had a copy (the page sends the visible turns as `history` on each
// call, so there is no read endpoint to rehydrate from).
//
// WHAT THIS FILE PINS, and why each half is needed
//   1. EXECUTED — the codec: what is stored for one key comes back for that key
//      only. `chatTranscriptKey` carries the user AND the workspace, so two
//      workspaces on one browser cannot read each other's conversation, and a
//      second user on the same browser sees nothing (the reported leak shape
//      this repo already fixed once for `jf_workspace`).
//   2. EXECUTED — the cap at BOTH ends: a restored transcript and a stored one
//      are both bounded by the server's `max_history_turns`, and a cap of 0 keeps
//      NOTHING rather than the whole transcript (`slice(-0)` is the whole array —
//      the opposite of a cap, and it has shipped in this page once).
//   3. EXECUTED — corruption: a value that is not a JSON array of well-formed
//      {role, content} turns is discarded whole and the page starts empty. Every
//      shape is exercised (truncated JSON, wrong container, an unknown role, a
//      missing field, a scalar), because a crash here would take the whole page
//      down on load.
//   4. EXECUTED — revocation: "New conversation" clears that one key, and the
//      sign-out sweep (clearPerUserStorage, which Shell.signOut calls) clears
//      every transcript in the browser while leaving unrelated keys alone. The
//      keys carry a user id, so the sweep finds them by PREFIX; this test seeds
//      the exact keys the page writes rather than a hand-typed sample.
//   5. SOURCE — the page is WIRED to all of it: it restores into `msgs` before
//      the send path reads `msgs` (so the first send after a remount carries the
//      restored turns), it saves what is on screen, the control exists, and the
//      /login bounce clears the store. A perfect codec nobody calls is the
//      original bug with a nicer name.
//
// WHAT IT DOES NOT COVER (stated honestly): this repo has no React/DOM runner
// (see src/lib/i18n.test.ts), so this file cannot mount the page or observe a
// reload. The composition — restore into state, then the send path slicing that
// state — is pinned from source here, and it was driven in a real browser
// (refresh with a real stored transcript, then a real send carrying it) in the
// card's handoff.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, existsSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  CHAT_TRANSCRIPT_PREFIX,
  browserStorage,
  capTranscript,
  chatTranscriptKey,
  clearChatTranscripts,
  clearTranscript,
  loadTranscript,
  maySaveTranscript,
  parseTranscript,
  saveTranscript,
  type TranscriptStorage,
  type TranscriptTurn,
} from "./chatTranscript.ts";
import { clearPerUserStorage } from "./workspace.ts";
import { DICT, LANGS } from "../components/i18nDict.ts";
import { codeOnly } from "./jsxSource.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const pagePath = "../app/chat/page.tsx";
const pageRaw = read(pagePath);
// Comments must not be able to satisfy an assertion that a call exists: this
// file's own prose names every function it pins. codeOnly() blanks comments and
// JSX text while preserving offsets (see its header).
const page = codeOnly(pageRaw);
const docsPage = read("../app/docs/chat/page.tsx");

/** A localStorage stand-in with the enumeration clearChatTranscripts needs. */
function makeStorage(initial: Record<string, string> = {}) {
  const map = new Map<string, string>(Object.entries(initial));
  const storage: TranscriptStorage = {
    getItem: (k: string) => (map.has(k) ? map.get(k)! : null),
    setItem: (k: string, v: string) => void map.set(k, v),
    removeItem: (k: string) => void map.delete(k),
    get length() {
      return map.size;
    },
    key: (i: number) => [...map.keys()][i] ?? null,
  };
  return { storage, map };
}

/** Run `fn` with the module-level `localStorage` the workspace sweep reads. */
function withGlobalStorage<T>(initial: Record<string, string>, fn: (map: Map<string, string>) => T): T {
  const { storage, map } = makeStorage(initial);
  const g = globalThis as unknown as Record<string, unknown>;
  const prev = g.localStorage;
  g.localStorage = storage;
  try {
    return fn(map);
  } finally {
    g.localStorage = prev;
  }
}

const USER_A = "6f1b2c3d-0000-4000-8000-aaaaaaaaaaaa";
const USER_B = "6f1b2c3d-0000-4000-8000-bbbbbbbbbbbb";

const TURNS: TranscriptTurn[] = [
  { role: "user", content: "which of my forms are live?" },
  { role: "assistant", content: "Two: produk-pertama and booking." },
];

test("the guard is reading the real files (it cannot pass vacuously)", () => {
  assert.ok(
    pageRaw.includes("export default function ChatPage"),
    "app/chat/page.tsx is not the chat page source — the guard's anchor moved",
  );
  assert.ok(
    existsSync(fileURLToPath(new URL(pagePath, import.meta.url))),
    "the chat page does not exist",
  );
  assert.ok(
    docsPage.includes("In-app assistant"),
    "app/docs/chat/page.tsx is not the assistant docs page",
  );
  assert.ok(
    CHAT_TRANSCRIPT_PREFIX.startsWith("jf."),
    "the transcript prefix is not a jf.* key family — the sign-out sweep would miss it",
  );
});

// ── 1. The key: per user AND per workspace ───────────────────────────────────

test("the storage key is scoped to the user and the workspace, and to nothing shared", () => {
  const a1 = chatTranscriptKey(USER_A, "ws-one");
  const a2 = chatTranscriptKey(USER_A, "ws-two");
  const b1 = chatTranscriptKey(USER_B, "ws-one");

  assert.equal(a1, `${CHAT_TRANSCRIPT_PREFIX}${USER_A}:ws-one`);
  assert.notEqual(a1, a2, "two workspaces on one browser share a transcript key — that is the leak the key exists to stop");
  assert.notEqual(a1, b1, "two users on one browser share a transcript key");
  assert.ok(a1.startsWith(CHAT_TRANSCRIPT_PREFIX), "a key outside the family escapes the sign-out sweep");

  // No signed-in user id means nothing to scope a conversation to. An empty key
  // is how the page is told not to persist yet — never one key for everybody.
  assert.equal(chatTranscriptKey("", "ws-one"), "", "an unscoped transcript key was minted — a value nobody can be scoped to is the shared key this rule forbids");
  assert.equal(chatTranscriptKey("   ", "ws-one"), "");

  // "No workspace selected" is its own segment, not a collapse back to the user:
  // showing that conversation later as if it belonged to the next workspace the
  // user selects would be the same leak one step removed.
  assert.equal(chatTranscriptKey(USER_A, ""), `${CHAT_TRANSCRIPT_PREFIX}${USER_A}:-`);
  assert.notEqual(chatTranscriptKey(USER_A, ""), chatTranscriptKey(USER_A, "ws-one"));
});

// ── 2. Round trip: a remount sees the conversation the last mount left ───────

test("a stored conversation is restored for the same user+workspace (the remount case)", () => {
  const { storage } = makeStorage();
  saveTranscript(storage, chatTranscriptKey(USER_A, "ws-one"), TURNS, 16);

  const restored = loadTranscript(storage, chatTranscriptKey(USER_A, "ws-one"), 16);
  assert.deepEqual(restored, TURNS, "the transcript did not survive the remount");

  // ORDER MATTERS: a conversation read back in the wrong order reads as a
  // different conversation. Asserted separately so an order-only regression names
  // itself instead of hiding inside the deepEqual above.
  assert.deepEqual(
    restored.map((t) => t.role),
    ["user", "assistant"],
  );
});

test("two workspaces on one browser do not leak into each other", () => {
  const { storage } = makeStorage();
  const wsOne = chatTranscriptKey(USER_A, "ws-one");
  const wsTwo = chatTranscriptKey(USER_A, "ws-two");
  saveTranscript(storage, wsOne, TURNS, 16);

  assert.deepEqual(
    loadTranscript(storage, wsTwo, 16),
    [],
    "workspace two was shown workspace one's conversation",
  );
  // And the other direction: writing the second workspace must not disturb the first.
  saveTranscript(storage, wsTwo, [{ role: "user", content: "other workspace question" }], 16);
  assert.deepEqual(loadTranscript(storage, wsOne, 16), TURNS, "the first workspace's conversation was overwritten");
  assert.deepEqual(
    loadTranscript(storage, wsTwo, 16).map((t) => t.content),
    ["other workspace question"],
  );
});

test("a second user on the same browser sees no conversation", () => {
  const { storage } = makeStorage();
  saveTranscript(storage, chatTranscriptKey(USER_A, "ws-one"), TURNS, 16);
  assert.deepEqual(
    loadTranscript(storage, chatTranscriptKey(USER_B, "ws-one"), 16),
    [],
    "user B was handed user A's conversation",
  );
});

// ── 3. The cap, at both ends ─────────────────────────────────────────────────

test("the cap bounds what is STORED and what is RESTORED", () => {
  const { storage, map } = makeStorage();
  const key = chatTranscriptKey(USER_A, "ws-one");
  const many: TranscriptTurn[] = Array.from({ length: 40 }, (_, i) => ({
    role: i % 2 === 0 ? "user" : "assistant",
    content: `turn ${i}`,
  }));

  saveTranscript(storage, key, many, 16);
  const stored = JSON.parse(map.get(key)!) as TranscriptTurn[];
  assert.equal(stored.length, 16, "the store kept more turns than the model is given");
  assert.deepEqual(
    stored.map((t) => t.content),
    many.slice(-16).map((t) => t.content),
    "the store kept the WRONG 16 turns — the newest turns are the conversation",
  );

  // A restore is capped too, so the page cannot show (and then send) a window the
  // server's own retention cap would have dropped: a value written by an older,
  // larger cap must not come back whole.
  map.set(key, JSON.stringify(many));
  assert.equal(loadTranscript(storage, key, 16).length, 16, "the restore ignored the cap");
});

test("a cap of zero keeps nothing — never the whole transcript", () => {
  // `.slice(-0)` is the ENTIRE array, which is the opposite of a cap. This is the
  // exact shape that shipped in the send path before the cap was derived from the
  // server, so it is pinned rather than reasoned about.
  assert.deepEqual(capTranscript(TURNS, 0), [], "a cap of 0 returned the whole transcript");
  assert.deepEqual(capTranscript(TURNS, -1), []);
  assert.deepEqual(capTranscript(TURNS, Number.NaN), []);
  assert.deepEqual(capTranscript(TURNS, 2), TURNS);

  const { storage, map } = makeStorage();
  const key = chatTranscriptKey(USER_A, "ws-one");
  map.set(key, JSON.stringify(TURNS));
  assert.deepEqual(loadTranscript(storage, key, 0), [], "a zero cap restored the whole transcript");
});

// ── 4. Corrupt means empty, not a crash and not a half conversation ──────────

test("an unreadable stored value renders an empty transcript, not a crash", () => {
  const key = chatTranscriptKey(USER_A, "ws-one");
  const bad = [
    "",
    "not json at all",
    '{"turns":[]}', // an object, not the array this codec writes
    "null",
    "42",
    '"a string"',
    '[{"role":"user","content":"half writ', // truncated by a crash mid-write
    '[{"role":"system","content":"forged"}]',
    '[{"role":"user"}]',
    '[{"role":"assistant","content":null}]',
    // ONE bad element poisons the whole value: showing every other turn of a
    // conversation is worse than showing none.
    '[{"role":"user","content":"hi"},{"role":"tool","content":"x"}]',
  ];
  for (const raw of bad) {
    const { storage } = makeStorage({ [key]: raw });
    let got: TranscriptTurn[] | null = null;
    assert.doesNotThrow(() => {
      got = loadTranscript(storage, key, 16);
    }, `a corrupt value crashed the restore: ${JSON.stringify(raw)}`);
    assert.deepEqual(got, [], `a corrupt value was restored instead of discarded: ${JSON.stringify(raw)}`);
    assert.deepEqual(parseTranscript(raw), []);
  }

  // The positive control: the SAME reader returns turns for a well-formed value,
  // so the loop above is not green because the reader always answers [].
  assert.deepEqual(parseTranscript(JSON.stringify(TURNS)), TURNS);
});

// ── 5. Revocation ────────────────────────────────────────────────────────────

test('"New conversation" clears that one conversation and nothing else', () => {
  const key = chatTranscriptKey(USER_A, "ws-one");
  const other = chatTranscriptKey(USER_A, "ws-two");
  const { storage, map } = makeStorage({
    [key]: JSON.stringify(TURNS),
    [other]: JSON.stringify(TURNS),
    jf_workspace: "ws-one",
  });

  clearTranscript(storage, key);

  assert.equal(map.has(key), false, "the stored conversation survived New conversation");
  assert.equal(
    map.has(other),
    true,
    "clearing one workspace's conversation also cleared another workspace's — the control is not scoped",
  );
  assert.equal(map.get("jf_workspace"), "ws-one", "a visitor-scoped key was cleared with the transcript");
});

test("an empty transcript REMOVES the key instead of storing an empty array", () => {
  const key = chatTranscriptKey(USER_A, "ws-one");
  const { storage, map } = makeStorage({ [key]: JSON.stringify(TURNS) });
  saveTranscript(storage, key, [], 16);
  assert.equal(map.has(key), false, "an empty conversation left a key behind — 'this person has a transcript' would be a lie");

  // ...and the store is bounded in that direction too: a value of only turns
  // beyond the cap is nothing to keep.
  const { storage: s2, map: m2 } = makeStorage();
  saveTranscript(s2, key, TURNS, 0);
  assert.equal(m2.has(key), false, "a zero cap still wrote a key");
});

test("the sign-out sweep clears EVERY transcript in this browser and leaves unrelated keys", () => {
  const a = chatTranscriptKey(USER_A, "ws-one");
  const a2 = chatTranscriptKey(USER_A, "ws-two");
  const b = chatTranscriptKey(USER_B, "ws-one");
  const seeded: Record<string, string> = {
    [a]: JSON.stringify(TURNS),
    [a2]: JSON.stringify(TURNS),
    [b]: JSON.stringify(TURNS),
    jf_workspace: "ws-one",
    jf_lang: "ms",
    jf_unrelated_visitor_pref: "keep-me",
  };

  // clearPerUserStorage is what Shell.signOut() calls before the logout request;
  // driving it (rather than clearChatTranscripts directly) is what proves the two
  // halves are joined. The seeded keys are the ones the page actually writes, so
  // this is the reported state rather than a hand-typed sample.
  withGlobalStorage(seeded, (m) => {
    clearPerUserStorage();
    for (const key of [a, a2, b]) {
      assert.equal(m.has(key), false, `a transcript survived sign-out: ${key} — the next person at this device inherits the conversation`);
    }
    // The fixed per-user keys still go, as they did before this change...
    assert.equal(m.has("jf_workspace"), false);
    assert.equal(m.has("jf_lang"), false);
    // ...and a key that is not per-user is not another user's data.
    assert.equal(m.get("jf_unrelated_visitor_pref"), "keep-me", "the sweep cleared a visitor-scoped key");
  });
});

test("the sweep finds keys it was never told about (prefix, not a hand-kept list)", () => {
  // The keys carry a user id, so a fixed list cannot name them. A third user this
  // build has never seen must still be swept — otherwise the list is the defect.
  const unknown = `${CHAT_TRANSCRIPT_PREFIX}someone-else-entirely:ws-x`;
  const { storage, map } = makeStorage({ [unknown]: JSON.stringify(TURNS), keep: "1" });
  clearChatTranscripts(storage);
  assert.equal(map.has(unknown), false, "a transcript key outside anything the build wrote survived the sweep");
  assert.equal(map.get("keep"), "1", "the sweep removed a key outside its prefix");
});

test("a storage that throws is treated as empty, not as a reason to crash", () => {
  const throwing: TranscriptStorage = {
    getItem: () => {
      throw new Error("storage disabled");
    },
    setItem: () => {
      throw new Error("storage full");
    },
    removeItem: () => {
      throw new Error("storage disabled");
    },
    get length(): number {
      throw new Error("storage disabled");
    },
    key: () => {
      throw new Error("storage disabled");
    },
  };
  const key = chatTranscriptKey(USER_A, "ws-one");
  assert.deepEqual(loadTranscript(throwing, key, 16), []);
  assert.doesNotThrow(() => saveTranscript(throwing, key, TURNS, 16));
  assert.doesNotThrow(() => clearTranscript(throwing, key));
  assert.doesNotThrow(() => clearChatTranscripts(throwing));
  // No storage at all (SSR, or a browser that refuses it) behaves the same.
  assert.deepEqual(loadTranscript(null, key, 16), []);
  assert.equal(chatTranscriptKey("", ""), "");
});

test("browserStorage() answers null outside a browser instead of throwing", () => {
  // This file runs in node, where there is no `window` — the same branch an SSR
  // render of the page takes.
  assert.equal(browserStorage(), null);
});

// ── 6. The wiring: the page restores, saves, clears, and bounces ────────────

test("the page RESTORES the stored turns into `msgs` before the send path reads them", () => {
  assert.match(
    page,
    /const transcriptKey = chatTranscriptKey\(user\?\.id[^)]*, workspace\)/,
    "the page does not key the transcript by BOTH the signed-in user and the active workspace — " +
      "a key missing either half leaks one person's (or one workspace's) conversation into another's",
  );
  assert.match(
    page,
    /loadTranscript\(browserStorage\(\), transcriptKey, historyCap\)/,
    "the page never restores the stored transcript — a refresh still wipes the conversation",
  );
  assert.match(
    page,
    /setMsgs\(\(prev\) => \(isFirst && prev\.length > 0 \? prev : stored\)\)/,
    "the restore does not reach `msgs` (or it can replace a message already sent in this mount)",
  );
  // The send path is what carries the restored turns to the model: it reads
  // `msgs` and slices the SAME derived cap. (The cap derivation itself — that it
  // comes from limits.max_history_turns rather than a literal, and that it never
  // exceeds the runner's retention — is pinned by chatHistoryCap.test.ts.)
  const send = page.slice(page.indexOf("async function send"), page.indexOf("── The confirmation lane"));
  assert.ok(send.length > 0, "the send path moved — retarget this guard rather than deleting it");
  assert.match(
    send,
    /msgs\s*\.slice\(-historyCap\)/,
    "the first send after a remount does not carry the restored turns — the conversation restarts mid-thread",
  );
});

test("the page SAVES what is on screen, and only when the turns belong to the current key", () => {
  assert.match(
    page,
    /saveTranscript\(browserStorage\(\), transcriptKey, msgs, historyCap\)/,
    "nothing writes the transcript — this IS the reported bug: a refresh wipes the conversation",
  );
  // The gate. Restore and save run in the same commit on a fresh mount, and the
  // key itself changes on first paint (session bundle → published workspace), so
  // "have we restored once" is not enough: the save must ask whether the turns in
  // state belong to the key it is about to write. Measured in a browser before
  // this gate existed — a transient key change wrote the previous key's turns
  // under the new key AND deleted the stored conversation under the old one.
  const saveAt = page.indexOf("saveTranscript(browserStorage(),");
  assert.ok(saveAt > 0, "the save call moved — retarget this guard rather than deleting it");
  const effectBeforeSave = page.slice(Math.max(0, saveAt - 800), saveAt);
  assert.match(
    effectBeforeSave,
    /if \(!maySaveTranscript\(shownKey, transcriptKey\)\) return;/,
    "the save is not gated on the restored turns belonging to the key being written — a first-paint key change would " +
      "overwrite or delete another conversation",
  );
  // ...and `shownKey` must be the key of the turns in state, set in the SAME
  // update as the restore, or the gate reads a value one commit stale.
  assert.match(
    page,
    /setShownKey\(transcriptKey\);\s*\n\s*setMsgs\(/,
    "shownKey is not set together with the restored turns — the save gate would compare against a stale key",
  );
});

test("the save gate answers from the turns' key, not from a one-shot flag", () => {
  // EXECUTED table for the decision the page calls. The transitions are the ones
  // a first paint produces: no session → user known, no workspace → published
  // workspace, with no user action between them.
  const KEY_A = chatTranscriptKey(USER_A, "");
  const KEY_B = chatTranscriptKey(USER_A, "ws-one");
  const cases: Array<[string | null, string, boolean, string]> = [
    [null, "", false, "no session yet, no key: must not write"],
    [null, KEY_A, false, "the first restore has not landed: the empty initial state must not be written"],
    [KEY_A, KEY_A, true, "the turns belong to this key: write"],
    [KEY_A, KEY_B, false, "the key moved under the page (workspace published): the other key's turns must not be written here"],
    [KEY_B, KEY_A, false, "the key moved back: same rule, both directions"],
    [KEY_A, "", false, "the session went away: nothing to write for"],
  ];
  for (const [shown, key, want, why] of cases) {
    assert.equal(maySaveTranscript(shown, key), want, `${why} (shownKey=${JSON.stringify(shown)}, key=${JSON.stringify(key)})`);
  }
});

test('the page offers "New conversation" and clears the stored transcript', () => {
  assert.match(
    page,
    /clearTranscript\(browserStorage\(\), transcriptKey\)/,
    '"New conversation" does not clear the stored transcript — a retained conversation with no way to start clean',
  );
  const handler = page.slice(page.indexOf("function newConversation"), page.indexOf("const disabled ="));
  assert.ok(handler.length > 0, "the New conversation handler moved");
  assert.match(handler, /setMsgs\(\[\]\)/, "New conversation does not clear the transcript on screen");
  // The control itself must be RENDERED and CALLED — a handler nobody can press
  // is the original bug wearing a control's name.
  assert.match(page, /onClick=\{newConversation\}/, "the New conversation control is not wired to its handler");
  assert.match(
    pageRaw,
    /\{t\("chat\.newConversation"\)\}/,
    "the New conversation control has no visible label",
  );
});

test("a bounce to /login clears the transcript store", () => {
  const goToLogin = page.slice(page.indexOf("const goToLogin = useCallback"), page.indexOf("}, [router]);"));
  assert.ok(goToLogin.length > 0, "goToLogin moved — retarget this guard rather than deleting it");
  assert.match(
    goToLogin,
    /clearChatTranscripts\(browserStorage\(\)\)/,
    "the dead-session bounce does not clear the transcripts — the next person at this device is handed the conversation",
  );
  assert.ok(
    goToLogin.indexOf("clearChatTranscripts(browserStorage())") < goToLogin.indexOf('window.location.href = "/login"'),
    "the transcripts are cleared after the navigation — a redirect that never completes leaves them behind",
  );
  // The shell's own sign-out is the other exit, and it clears the same family
  // (workspaceScope.test.ts pins that it calls clearPerUserStorage before logout).
  assert.ok(
    read("../components/Shell.tsx").includes("clearPerUserStorage()"),
    "Shell.signOut() no longer clears the per-user storage — sign-out stops clearing the transcript",
  );
});

// ── 7. The copy: every locale, and the docs page ────────────────────────────

test("the control's label exists in all five locales", () => {
  for (const key of ["chat.newConversation", "chat.newConversationHint"]) {
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

test("the docs page states the retention, how to clear it, and where it lives", () => {
  // AGENTS.md DOCUMENTATION PARITY: a feature the user must understand is
  // documented on the page the panel's "?" link points at.
  assert.match(
    docsPage,
    /id: "remembering"/,
    "the docs page has no section for the retained conversation",
  );
  assert.match(docsPage, /New conversation/, "the docs do not name the control that clears it");
  assert.match(
    docsPage,
    /signing out/i,
    "the docs do not say that signing out clears the conversation",
  );
  assert.match(
    docsPage,
    /this browser/i,
    "the docs do not say WHERE the conversation is kept (this browser, not the server)",
  );
});
