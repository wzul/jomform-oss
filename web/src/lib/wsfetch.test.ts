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

import {
  HttpError,
  classifyLoadError,
  classifyStatus,
  fetchJson,
  isUnauthStatus,
  runLoad,
} from "./wsfetch.ts";

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

test("an error carrying a status is classified from that status", () => {
  assert.deepEqual(classifyLoadError(new HttpError(401)), { kind: "unauth", status: 401 });
  assert.deepEqual(classifyLoadError(new HttpError(502)), { kind: "http", status: 502 });
  assert.deepEqual(classifyLoadError(new Error("boom")), { kind: "network", status: 0 });
});
