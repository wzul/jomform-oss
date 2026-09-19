// Issue #447 — the /sales notification-sound switch is PER WORKSPACE.
//
// THE BUG: the switch was `useState(false)` on the page and nothing ever read or
// wrote the stored value, so it reset on every reload, navigation and login, and
// was identical-on-nobody: a kitchen/counter screen went silent on reload with
// no indication. This file EXECUTES the wire behaviour the fix depends on, using
// the page's own module-level helpers (lib/workspace.ts) rather than a copy of
// them, and a fake `fetch` that records what the page sends:
//
//   1. the READ. The stored value must come back from the settings payload and
//      seed the switch — `true` when the workspace has it on, `false` when off
//      or when the field is absent (the server column is NOT NULL DEFAULT FALSE).
//      Reading a stored `true` as off is the bug: the merchant enables the beep
//      and the next load silences it again.
//   2. the WRITE. The request must be a PUT to the settings path for the ACTIVE
//      workspace's ID (not its slug — the route is /v1/workspaces/{id}/settings)
//      carrying exactly `{ "sound_enabled": <new value> }`. A body carrying other
//      settings would let this one switch clobber them, and a page-state body
//      would too, because saveWorkspaceSettings writes every column from the
//      struct it is handed.
//   3. the REVERT. A rejected write must restore the previous value, so the
//      switch never displays a value the server does not hold.
//   4. the WORKSPACE BOUNDARY. The id must resolve to the ACTIVE workspace, never
//      to another one in the list — a per-workspace setting written to the wrong
//      workspace is the cross-workspace leak shape, not a cosmetic bug.
//
// WHAT IT DOES NOT COVER (stated honestly): it does not run React, so the page's
// own wiring is pinned by the source guard in soundToggle.test.ts, and this file
// asserts the shared helpers plus the request shapes the page is required to use.
// The two together are the proof; neither alone is.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";

import {
  activeWorkspaceId,
  soundEnabledFromSettings,
  soundToggleBody,
} from "./workspace.ts";

// ── a minimal localStorage/window stand-in, so the helpers' scope lookup works
// (Node has no DOM; workspace.ts only touches storage INSIDE its functions, so a
// static import above is safe).
function withScope<T>(slug: string | null, fn: () => T): T {
  const store = new Map<string, string>();
  if (slug !== null) store.set("jf_workspace", slug);
  const g = globalThis as unknown as Record<string, unknown>;
  const prev = { window: g.window, localStorage: g.localStorage };
  g.window = globalThis;
  g.localStorage = {
    getItem: (k: string) => (store.has(k) ? store.get(k)! : null),
    setItem: (k: string, v: string) => void store.set(k, v),
    removeItem: (k: string) => void store.delete(k),
  };
  try {
    return fn();
  } finally {
    g.window = prev.window;
    g.localStorage = prev.localStorage;
  }
}

const WORKSPACES = [
  { id: "ws-first", slug: "first" },
  { id: "ws-active", slug: "active" },
];

test("the stored value seeds the switch, and an absent field means OFF", () => {
  // The read direction that the bug got wrong: a stored TRUE must come back ON.
  assert.equal(
    soundEnabledFromSettings({ sound_enabled: true }),
    true,
    "a workspace with sound_enabled = true does not read back as ON — the beep " +
      "would be off on the next load, which is exactly the reported bug",
  );
  assert.equal(soundEnabledFromSettings({ sound_enabled: false }), false);
  // The server's default (NOT NULL DEFAULT FALSE): absent = muted, never "on".
  assert.equal(soundEnabledFromSettings({}), false, "an absent sound_enabled must read as OFF");
  assert.equal(soundEnabledFromSettings(null), false);
  assert.equal(soundEnabledFromSettings(undefined), false);
  // A truthy-but-not-true value must not be read as ON (a JSON string would be).
  assert.equal(
    soundEnabledFromSettings(JSON.parse('{"sound_enabled":"false"}')),
    false,
    'a string "false" read as ON switches the beep on against the stored setting',
  );
});

test("the toggle writes exactly one field, so no other workspace setting can be clobbered", () => {
  assert.deepEqual(soundToggleBody(true), { sound_enabled: true });
  assert.deepEqual(soundToggleBody(false), { sound_enabled: false });
  assert.deepEqual(
    Object.keys(soundToggleBody(true)),
    ["sound_enabled"],
    "the write body carries more than sound_enabled — saveWorkspaceSettings would " +
      "persist the extra fields over the merchant's stored values",
  );
});

test("the write is addressed to the ACTIVE workspace, never a sibling", () => {
  assert.equal(
    withScope("active", () => activeWorkspaceId(WORKSPACES)),
    "ws-active",
    "the sound setting was addressed to the wrong workspace",
  );
  // Fallback: no stored slug yet — the Shell selects the first workspace on
  // login, so the write must follow the same choice.
  assert.equal(withScope(null, () => activeWorkspaceId(WORKSPACES)), "ws-first");
  // An unknown slug must not silently address a sibling; it falls back to the
  // same first workspace the UI is actually showing.
  assert.equal(withScope("deleted-workspace", () => activeWorkspaceId(WORKSPACES)), "ws-first");
  // Nothing to resolve: no request must be attempted at all.
  assert.equal(withScope("active", () => activeWorkspaceId([])), "");
});

// ── the sequence the page performs, expressed with the module's real helpers and
// a fake fetch. The page's wiring (which calls it makes, and in what order) is
// pinned by the source guard; this asserts the round trip those calls produce.
type Sent = { url: string; method: string; body: unknown };

function fakeServer(stored: boolean, opts: { failWrite?: boolean } = {}) {
  const sent: Sent[] = [];
  const state = { stored };
  const fetchImpl = async (url: string, init?: { method?: string; body?: string }) => {
    const method = init?.method || "GET";
    const body = init?.body ? JSON.parse(init.body) : undefined;
    sent.push({ url, method, body });
    if (method === "GET") return { ok: true, json: async () => ({ sound_enabled: state.stored }) };
    if (opts.failWrite) return { ok: false, status: 500, json: async () => ({ error: "simpan gagal" }) };
    state.stored = body?.sound_enabled === true;
    return { ok: true, json: async () => ({ status: "saved" }) };
  };
  return { sent, state, fetchImpl };
}

/** The page's toggle handler, using the same helpers the page calls. */
async function toggle(
  fetchImpl: (url: string, init?: { method?: string; body?: string }) => Promise<unknown>,
  workspaceId: string,
  ui: { value: boolean },
  next: boolean,
) {
  const previous = ui.value;
  ui.value = next; // optimistic flip
  const r = (await fetchImpl(`/api/workspaces/${workspaceId}/settings`, {
    method: "PUT",
    body: JSON.stringify(soundToggleBody(next)),
  })) as { ok: boolean };
  if (!r.ok) ui.value = previous; // honest failure
  return r.ok;
}

test("turn on, reload, still on — and a failed write reverts instead of lying", async () => {
  const srv = fakeServer(false);
  const ui = { value: false };
  const wsId = withScope("active", () => activeWorkspaceId(WORKSPACES));

  // 1. enable the beep
  assert.equal(await toggle(srv.fetchImpl, wsId, ui, true), true);
  assert.equal(ui.value, true);
  assert.deepEqual(srv.sent.at(-1), {
    url: "/api/workspaces/ws-active/settings",
    method: "PUT",
    body: { sound_enabled: true },
  });
  assert.equal(srv.state.stored, true, "the server did not store the enabled value");

  // 2. RELOAD: a fresh switch seeded from a fresh GET of the stored value.
  const reloaded = await srv.fetchImpl("/api/workspaces/ws-active/settings");
  const payload = await (reloaded as { json: () => Promise<unknown> }).json();
  ui.value = soundEnabledFromSettings(payload as { sound_enabled?: boolean });
  assert.equal(ui.value, true, "after a reload the switch is OFF — the beep resets (the bug)");

  // 3. a DIFFERENT browser/profile in the same workspace sees the same value:
  //    that is what per-workspace means. The value lives on the server, so a
  //    second switch seeded from the same GET reads ON with no local storage.
  const otherProfile = { value: soundEnabledFromSettings(payload as { sound_enabled?: boolean }) };
  assert.equal(otherProfile.value, true, "a second session does not see the stored setting");

  // 4. turn it off, reload, still off.
  assert.equal(await toggle(srv.fetchImpl, wsId, ui, false), true);
  const afterOff = await srv.fetchImpl("/api/workspaces/ws-active/settings");
  const offPayload = await (afterOff as { json: () => Promise<unknown> }).json();
  assert.equal(soundEnabledFromSettings(offPayload as { sound_enabled?: boolean }), false);
  assert.equal(srv.state.stored, false);

  // 5. a FAILING write must not leave the switch claiming the new value.
  const failing = fakeServer(false, { failWrite: true });
  assert.equal(await toggle(failing.fetchImpl, wsId, ui, true), false);
  assert.equal(
    ui.value,
    false,
    "a rejected save left the switch ON — it now displays a value the server does not hold",
  );
  assert.equal(failing.state.stored, false, "the server holds the old value; the switch must match");
});
