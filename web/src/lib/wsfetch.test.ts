// Issue #503 regression test — the whole point of the bug: a NON-OK response
// must never be silently ignored, and a 401 must reach the /login redirect.
//
// This repo has no JS component-test runner (Playwright is used only for the
// axe-core accessibility scan; there is no vitest/jest), so the decision logic
// was extracted into ../wsfetch.ts and is tested here with the built-in Node
// test runner, which executes TypeScript directly (Node 24). Run it with:
//
//   node --test src/lib/*/*.test.ts
//   (or: npm run test:unit)
//
// The assertions below are the exact regression the issue describes:
//   1. 401 → kind "unauth" and onUnauth(401) fires → the page redirects to
//      /login instead of hanging on "…".
//   2. 500 → kind "http", ok:false, and a NON-EMPTY error message → the page
//      renders a visible error, never a permanent "…".
//   3. A network failure also surfaces a message (never silence).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  HttpError,
  classifyLoadError,
  classifyStatus,
  fetchJson,
  isUnauthStatus,
  runLoad,
} from "./wsfetch.ts";
import {
  resetConfirmedWorkspaceScope,
  setConfirmedWorkspaceScope,
  workspaceQuery,
} from "./workspace.ts";

const MESSAGE = "Something went wrong loading your workspaces.";

test("401 from the workspaces fetch redirects to /login (never silent)", async () => {
  const redirected: number[] = [];
  const outcome = await runLoad(
    () => Promise.reject(new HttpError(401)),
    { onUnauth: (status) => redirected.push(status), message: MESSAGE },
  );

  // The regression: this used to be swallowed by `if (wsRes.ok)` with no else.
  assert.equal(outcome.ok, false);
  assert.equal(outcome.ok === false && outcome.kind, "unauth");
  assert.deepEqual(redirected, [401], "401 must trigger the /login redirect");
});

test("403 (expired/forbidden session) also redirects to /login", async () => {
  const redirected: number[] = [];
  const outcome = await runLoad(
    () => Promise.reject(new HttpError(403)),
    { onUnauth: (status) => redirected.push(status), message: MESSAGE },
  );

  assert.equal(outcome.ok, false);
  assert.equal(outcome.ok === false && outcome.kind, "unauth");
  assert.deepEqual(redirected, [403]);
});

test("a non-auth HTTP failure yields a visible error message, not silence", async () => {
  const redirected: number[] = [];
  const outcome = await runLoad(
    () => Promise.reject(new HttpError(500)),
    { onUnauth: (status) => redirected.push(status), message: MESSAGE },
  );

  assert.equal(outcome.ok, false);
  assert.equal(outcome.ok === false && outcome.kind, "http");
  // A real, non-empty message is what the error state renders. An empty string
  // is the bug (no error state → the "…" placeholder stayed forever).
  assert.equal(outcome.ok === false && outcome.error, MESSAGE);
  assert.notEqual(outcome.ok === false && outcome.error, "");
  assert.deepEqual(redirected, [], "a 500 must NOT bounce to /login");
});

test("a network failure yields a visible error message and no redirect", async () => {
  const redirected: number[] = [];
  const outcome = await runLoad(
    () => Promise.reject(new TypeError("Failed to fetch")),
    { onUnauth: (status) => redirected.push(status), message: MESSAGE },
  );

  assert.equal(outcome.ok, false);
  assert.equal(outcome.ok === false && outcome.kind, "network");
  assert.equal(outcome.ok === false && outcome.error, MESSAGE);
  assert.deepEqual(redirected, []);
});

test("malformed JSON (a 2xx whose body cannot be parsed) surfaces the message", async () => {
  const redirected: number[] = [];
  const outcome = await runLoad(
    () => Promise.reject(new SyntaxError("Unexpected token < in JSON")),
    { onUnauth: (status) => redirected.push(status), message: MESSAGE },
  );

  assert.equal(outcome.ok, false);
  assert.equal(outcome.ok === false && outcome.kind, "network");
  assert.equal(outcome.ok === false && outcome.error, MESSAGE);
});

test("a 2xx passes the payload through and never reports an error", async () => {
  const rows = [{ id: "ws_1", name: "Acme", slug: "acme" }];
  const redirected: number[] = [];
  const outcome = await runLoad(
    () => Promise.resolve(rows),
    { onUnauth: (status) => redirected.push(status), message: MESSAGE },
  );

  assert.equal(outcome.ok, true);
  assert.deepEqual(outcome.ok === true && outcome.data, rows);
  assert.deepEqual(redirected, []);
});

test("fetchJson throws HttpError(401) on a 401 so it cannot be skipped", async () => {
  const original = globalThis.fetch;
  globalThis.fetch = (async () =>
    new Response("unauthorized", { status: 401 })) as typeof fetch;
  try {
    await assert.rejects(fetchJson("/api/workspaces"), (err: unknown) => {
      assert.ok(err instanceof HttpError);
      assert.equal((err as HttpError).status, 401);
      return true;
    });
  } finally {
    globalThis.fetch = original;
  }
});

test("fetchJson resolves with the parsed body on a 200", async () => {
  const original = globalThis.fetch;
  globalThis.fetch = (async () =>
    new Response(JSON.stringify([{ id: "ws_1" }]), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    })) as typeof fetch;
  try {
    const d = await fetchJson<{ id: string }[]>("/api/workspaces");
    assert.deepEqual(d, [{ id: "ws_1" }]);
  } finally {
    globalThis.fetch = original;
  }
});

test("status classification: only 401/403 are unauth", () => {
  assert.equal(isUnauthStatus(401), true);
  assert.equal(isUnauthStatus(403), true);
  assert.equal(isUnauthStatus(404), false);
  assert.equal(isUnauthStatus(500), false);
  assert.equal(classifyStatus(401), "unauth");
  assert.equal(classifyStatus(403), "unauth");
  assert.equal(classifyStatus(500), "http");
});

// ── THE TENANCY SPLIT (t_da4b2031) ───────────────────────────────────────────
//
// THE DEFECT. A `jf_workspace` slug the session cannot resolve (a workspace
// renamed or deleted while selected) makes every scoped fetch carry a foreign
// `?workspace=`, the server answers 403 "workspace tidak dijumpai", and the
// 401/403 rule above read that as a dead session and navigated to /login.
// Measured on prod (v1.17.0): `/chat` with `jf_workspace = ws-gone` landed on
// https://jomform.com/login for a signed-in merchant.
//
// THE FIX IS A CODE SPLIT, NOT A STATUS CHANGE. The server ALREADY names the
// difference on the wire (`workspace.notFound` for a foreign slug vs a bare 401
// for a dead session), so moving the refusal to a 404 would change the public
// contract for every API-key, curl and MCP caller to describe something its
// `code` already says. These cases are the bite: reverting `isUnauthStatus` to
// the old `status === 401 || status === 403` makes the FIRST case fail by name,
// and reverting `runLoad`'s scope branch makes the last two fail — the
// onUnauth spy records the /login navigation the defect performed.
//
// The code is read out of the Go source rather than typed here, so a rename on
// the server fails this file instead of silently restoring the sign-out.

/** The value of `CodeWorkspaceNotFound = "..."` in errcode.go, or null. */
function goWorkspaceNotFoundCode(): string | null {
  const go = readFileSync(
    fileURLToPath(new URL("../../../api/internal/httpapi/errcode.go", import.meta.url)),
    "utf8",
  );
  const m = go.match(/\bCodeWorkspaceNotFound\s*=\s*"([^"]+)"/);
  return m ? m[1] : null;
}

/** The wire body the server sends for a slug the caller cannot resolve. */
function scopeRefusalBody(): { error: string; code: string } {
  return { error: "workspace tidak dijumpai", code: goWorkspaceNotFoundCode() ?? "" };
}

test("the guard reads the real server code (it cannot pass vacuously)", () => {
  assert.equal(
    goWorkspaceNotFoundCode(),
    "workspace.notFound",
    "api/internal/httpapi/errcode.go no longer declares CodeWorkspaceNotFound — the split below would " +
      "key on a code the server never sends, and every foreign-slug 403 would become a sign-out again",
  );
  // And the refusal really is written by the scope gate (not some other path).
  assert.match(
    readFileSync(
      fileURLToPath(new URL("../../../api/internal/httpapi/rest_scope.go", import.meta.url)),
      "utf8",
    ),
    /writeErr\(w, http\.StatusForbidden, "workspace tidak dijumpai"\)/,
    "rest_scope.go no longer writes the foreign-slug 403 — this guard's whole premise moved",
  );
  assert.ok(
    readFileSync(fileURLToPath(new URL("./wsfetch.ts", import.meta.url)), "utf8").includes(
      "isScopeRefusal",
    ),
    "wsfetch.ts no longer consults the tenancy code at all",
  );
});

test("a foreign-workspace 403 is NOT a dead session (the sign-out this card fixes)", () => {
  const body = scopeRefusalBody();
  // The measured production shape: `GET /api/rest/chat?workspace=ws-gone` -> 403
  // with the tenancy code. Reading it as unauth IS the reported defect.
  assert.equal(
    isUnauthStatus(403, body),
    false,
    "a 403 carrying workspace.notFound is still classified as a dead session — a stale jf_workspace " +
      "would redirect a signed-in merchant to /login, which is exactly the reported defect",
  );
  assert.equal(
    classifyStatus(403, body),
    "scope",
    "a foreign-workspace 403 is not its own outcome — the page cannot tell it apart from an expired " +
      "session, so it can only navigate away",
  );
  // The OTHER direction must keep working: the tenancy code is what separates
  // them, so an uncoded 403 (a genuine authorization refusal) is still unauth.
  assert.equal(
    isUnauthStatus(403, { error: "perlu admin workspace" }),
    true,
    "an uncoded 403 stopped being a session failure — an authorization refusal would be softened into " +
      "a 'choose a workspace' message the user cannot act on",
  );
  assert.equal(isUnauthStatus(403), true, "a 403 with no body read at all must stay conservative (unauth)");
  assert.equal(classifyStatus(403, { code: "request.forbidden" }), "unauth");
  assert.equal(classifyStatus(403, { code: "workspace.scopeRequired" }), "unauth");
  // 401 is untouched: a dead session is still a dead session.
  assert.equal(isUnauthStatus(401, body), true);
  assert.equal(classifyStatus(401, body), "unauth");
});

test("an HttpError carrying the tenancy refusal classifies as scope, not unauth", () => {
  const body = scopeRefusalBody();
  assert.deepEqual(classifyLoadError(new HttpError(403, body)), { kind: "scope", status: 403 });
  assert.deepEqual(classifyLoadError(new HttpError(403)), { kind: "unauth", status: 403 });
  assert.deepEqual(classifyLoadError(new HttpError(401, body)), { kind: "unauth", status: 401 });
  // Unchanged: a 5xx is still a plain error and a bare Error is still network.
  assert.deepEqual(classifyLoadError(new HttpError(502)), { kind: "http", status: 502 });
  assert.deepEqual(classifyLoadError(new Error("boom")), { kind: "network", status: 0 });
});

test("runLoad keeps the session on a tenancy refusal and never calls onUnauth", async () => {
  // The whole point, executed through the shared wrapper every dashboard page
  // uses: NO /login navigation, and the stale slug is dropped so the next request
  // cannot send it again. Reverting the scope branch in runLoad makes the last
  // assertion fail — it would assert onUnauth was NOT called while the pre-fix
  // code called it.
  const store = new Map<string, string>([["jf_workspace", "ws-gone"]]);
  const g = globalThis as unknown as Record<string, unknown>;
  const prev = { window: g.window, localStorage: g.localStorage };
  // `dropCurrentWorkspaceScope` dispatches an event, which this stand-in must
  // provide: the module checks `typeof window !== "undefined"`, so the real Node
  // `globalThis` would be used and has no dispatchEvent.
  g.window = { dispatchEvent: () => true };
  g.localStorage = {
    getItem: (k: string) => (store.has(k) ? store.get(k)! : null),
    setItem: (k: string, v: string) => void store.set(k, v),
    removeItem: (k: string) => void store.delete(k),
  };
  const redirected: number[] = [];
  try {
    const outcome = await runLoad(
      () => Promise.reject(new HttpError(403, scopeRefusalBody())),
      {
        onUnauth: (status) => redirected.push(status),
        message: MESSAGE,
        errorKey: () => "That workspace was not found, or it is not one you have access to.",
      },
    );
    assert.equal(outcome.ok, false);
    assert.equal(
      outcome.ok === false && outcome.kind,
      "scope",
      "a foreign-workspace 403 is not reported as a scope refusal",
    );
    assert.deepEqual(
      redirected,
      [],
      "runLoad navigated to /login for a slug the user's own localStorage held — this IS the reported " +
        "sign-out (t_da4b2031)",
    );
    // The slug is gone, so the retry the page issues is UNSCOPED (the server then
    // resolves the caller's own account instead of rejecting one they never had).
    assert.equal(
      store.get("jf_workspace"),
      undefined,
      "the stale slug survived the refusal — the very next request would carry it again",
    );
    // The refusal still RENDERS something: the outcome carries the localized
    // tenancy sentence, so the user is told rather than left on a blank page.
    assert.equal(
      outcome.ok === false && outcome.error,
      "That workspace was not found, or it is not one you have access to.",
    );
  } finally {
    g.window = prev.window;
    g.localStorage = prev.localStorage;
  }

  // And the control: a DEAD SESSION still navigates. If the split swallowed
  // 401s too, an expired session would leave the user staring at an error
  // instead of being sent to /login.
  const redirected2: number[] = [];
  const outcome2 = await runLoad(
    () => Promise.reject(new HttpError(401)),
    { onUnauth: (status) => redirected2.push(status), message: MESSAGE },
  );
  assert.deepEqual(redirected2, [401], "a 401 stopped redirecting to /login — an expired session now hangs");
  assert.equal(outcome2.ok === false && outcome2.kind, "unauth");

  // ...and so does an UNCoded 403 (a real authorization refusal).
  const redirected3: number[] = [];
  await runLoad(
    () => Promise.reject(new HttpError(403)),
    { onUnauth: (status) => redirected3.push(status), message: MESSAGE },
  );
  assert.deepEqual(
    redirected3,
    [403],
    "an uncoded 403 stopped redirecting to /login — a genuine authorization refusal would be treated as " +
      "a stale workspace",
  );
});

test("runLoad routes a refusal to the slug it is ABOUT, not to the current one (t_1cfb4044)", async () => {
  // THE RACE, executed through the shared wrapper every dashboard page uses.
  // Sequence on a stale jf_workspace:
  //   1. the page fires its first scoped request carrying `ws-gone`;
  //   2. the session bundle (UNSCOPED, so never refused) answers while that
  //      request is still in flight and the `[bundle]` effect reconciles the
  //      scope onto `ws-a`, a workspace the caller really has;
  //   3. the refusal for `ws-gone` finally lands.
  // The drop must be a no-op at (3). The pre-fix code dropped unconditionally and
  // wiped `ws-a`; nothing re-publishes it, so `jf_workspace` stayed unset and
  // every later request went out UNscoped. Measured: the spec's 10s poll read
  // null, and it was still null at 30s.
  const store = new Map<string, string>([["jf_workspace", "ws-a"]]);
  const g = globalThis as unknown as Record<string, unknown>;
  const prev = { window: g.window, localStorage: g.localStorage };
  g.window = { dispatchEvent: () => true };
  g.localStorage = {
    getItem: (k: string) => (store.has(k) ? store.get(k)! : null),
    setItem: (k: string, v: string) => void store.set(k, v),
    removeItem: (k: string) => void store.delete(k),
  };
  try {
    // The session has already reconciled: `ws-a` is the confirmed scope.
    setConfirmedWorkspaceScope("ws-a");
    const outcome = await runLoad(
      () => Promise.reject(new HttpError(403, scopeRefusalBody(), "ws-gone")),
      { onUnauth: () => {}, message: MESSAGE },
    );
    assert.equal(outcome.ok === false && outcome.kind, "scope");
    assert.equal(
      store.get("jf_workspace"),
      "ws-a",
      "a refusal for a slug that had already been replaced wiped the reconciled workspace — this is the " +
        "stale-slug race (t_1cfb4044)",
    );
    assert.equal(
      workspaceQuery(),
      "?workspace=ws-a",
      "the reconciled scope stopped reaching the wire, so the page loads with no workspace at all",
    );
  } finally {
    resetConfirmedWorkspaceScope();
    g.window = prev.window;
    g.localStorage = prev.localStorage;
  }
});

test("runLoad still drops the slug the refusal IS about, and an unattributed one", async () => {
  // The two directions the routing must NOT lose:
  //   * `ws-gone` refused while it is STILL the scope in use → dropped, or the
  //     next request repeats the 403 forever;
  //   * a refusal with no slug known (a hand-built HttpError, an older caller) →
  //     dropped, because "unattributed" is not "not mine".
  const cases: { name: string; slug: string; err: () => HttpError }[] = [
    { name: "attributed to the current slug", slug: "ws-gone", err: () => new HttpError(403, scopeRefusalBody(), "ws-gone") },
    { name: "unattributed", slug: "ws-gone", err: () => new HttpError(403, scopeRefusalBody()) },
  ];
  for (const c of cases) {
    const store = new Map<string, string>([["jf_workspace", "ws-gone"]]);
    const g = globalThis as unknown as Record<string, unknown>;
    const prev = { window: g.window, localStorage: g.localStorage };
    g.window = { dispatchEvent: () => true };
    g.localStorage = {
      getItem: (k: string) => (store.has(k) ? store.get(k)! : null),
      setItem: (k: string, v: string) => void store.set(k, v),
      removeItem: (k: string) => void store.delete(k),
    };
    try {
      setConfirmedWorkspaceScope("ws-gone");
      await runLoad(() => Promise.reject(c.err()), { onUnauth: () => {}, message: MESSAGE });
      assert.equal(
        store.get("jf_workspace"),
        undefined,
        `${c.name}: the stale slug survived the refusal — the very next request would carry it again`,
      );
      assert.equal(workspaceQuery(), "", `${c.name}: a rejected slug is still sent on the next request`);
    } finally {
      resetConfirmedWorkspaceScope();
      g.window = prev.window;
      g.localStorage = prev.localStorage;
    }
  }
});

test("an error carrying a status is classified from that status", () => {
  assert.deepEqual(classifyLoadError(new HttpError(401)), { kind: "unauth", status: 401 });
  assert.deepEqual(classifyLoadError(new HttpError(502)), { kind: "http", status: 502 });
  assert.deepEqual(classifyLoadError(new Error("boom")), { kind: "network", status: 0 });
});
