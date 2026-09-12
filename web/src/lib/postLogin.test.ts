// Issue #503 regression test — the post-login resolver had the SAME
// silent-ignore shape as the picker: `/api/workspaces` was read as
// `if (wsRes.ok)` with no `else`, so an expired session (401) resolved as
// "no workspaces" and the user was dropped on the dashboard as if they had
// none. It must now send an unauthorised session to /login.
//
// Run with the repo's Node test runner: npm run test:unit
import { test } from "node:test";
import assert from "node:assert/strict";

import { resolvePostLogin } from "./postLogin.ts";

/**
 * Install a fake fetch that answers by URL. Each endpoint can be given a
 * response status so the failure paths are exercised, not just the happy path.
 */
function stubFetch(routes: Record<string, { status: number; body: unknown }>): () => void {
  const original = globalThis.fetch;
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    const url = String(input);
    const route = Object.keys(routes).find((k) => url.includes(k));
    if (!route) return new Response("not stubbed", { status: 404 });
    const r = routes[route];
    return new Response(JSON.stringify(r.body), {
      status: r.status,
      headers: { "Content-Type": "application/json" },
    });
  }) as typeof fetch;
  return () => {
    globalThis.fetch = original;
  };
}

test("resolvePostLogin: a 401 from /api/workspaces goes to /login (not the dashboard)", async () => {
  const restore = stubFetch({
    "/api/workspaces": { status: 401, body: { error: "unauthorized" } },
    "/api/auth/me": { status: 401, body: {} },
  });
  try {
    assert.equal(await resolvePostLogin(""), "/login");
  } finally {
    restore();
  }
});

test("resolvePostLogin: ?next still wins outright, no fetch at all", async () => {
  const restore = stubFetch({});
  try {
    assert.equal(await resolvePostLogin("/settings"), "/settings");
  } finally {
    restore();
  }
});

test("resolvePostLogin: a network failure still falls back to the dashboard", async () => {
  const original = globalThis.fetch;
  globalThis.fetch = (async () => {
    throw new TypeError("Failed to fetch");
  }) as typeof fetch;
  try {
    assert.equal(await resolvePostLogin(""), "/");
  } finally {
    globalThis.fetch = original;
  }
});

test("resolvePostLogin: a single workspace enters it (dashboard, no picker)", async () => {
  const restore = stubFetch({
    "/api/workspaces": { status: 200, body: [{ id: "ws_1", slug: "acme" }] },
    "/api/auth/me": { status: 200, body: {} },
  });
  try {
    assert.equal(await resolvePostLogin(""), "/");
  } finally {
    restore();
  }
});

test("resolvePostLogin: multi-workspace with no default opens the picker", async () => {
  const restore = stubFetch({
    "/api/workspaces": {
      status: 200,
      body: [
        { id: "ws_1", slug: "acme" },
        { id: "ws_2", slug: "beta" },
      ],
    },
    "/api/auth/me": { status: 200, body: {} },
  });
  try {
    assert.equal(await resolvePostLogin(""), "/workspace-picker");
  } finally {
    restore();
  }
});

test("resolvePostLogin: a 500 on the list is a non-auth failure, not a /login redirect", async () => {
  const restore = stubFetch({
    "/api/workspaces": { status: 500, body: {} },
    "/api/auth/me": { status: 200, body: {} },
  });
  try {
    assert.equal(await resolvePostLogin(""), "/");
  } finally {
    restore();
  }
});
