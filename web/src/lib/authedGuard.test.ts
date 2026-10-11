// The server-side gate that keeps the account shell out of an anonymous
// response (see src/lib/authedGuard.ts, src/middleware.ts).
//
// WHAT CAN BREAK HERE, and why each check is the right guard:
//
//   1. A DASHBOARD PAGE IS NOT LISTED, so it keeps serving the shell to an
//      anonymous visitor — exactly the bug this gate exists to fix, and exactly
//      how it happened the first time (measured 2026-10-11: anonymous
//      `GET /profile` answered 200 with the shell in the HTML while all fifteen
//      siblings bounced client-side). The list cannot be derived at runtime, so
//      it is derived HERE, from the app directory: a new page that consumes
//      `useSession()` and is not listed fails this test by name.
//   2. A PUBLIC PAGE IS LISTED, which is worse than the bug: /login,
//      /magic-link or /post-login refusing a cookie-less visitor is a flow a
//      real user cannot complete. Pinned in both directions.
//   3. THE MATCHER DRIFTS FROM THE LIST. Next needs a literal matcher array, so
//      the gate's list and the matcher are two copies of the same fact. A page
//      in the list but missing from the matcher is not gated in production at
//      all — the decision inside `middleware()` only runs on paths the matcher
//      lets through.
//   4. THE COOKIE NAME IS TYPED AS A LITERAL. `lib/sessionCookie.ts` is the
//      shared constant (its own test reads the Go side); a literal here would
//      drift silently and turn every signed-in visitor into an anonymous one.
//   5. THE REDIRECT LOSES WHAT THE VISITOR ASKED FOR. `?next=` is the
//      mechanism issue #140 built for the OAuth flow, and the login page
//      REQUIRES an absolute, same-origin http(s) URL — a relative path is
//      silently discarded there, so a wrong shape degrades quietly.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { join, dirname } from "node:path";

import {
  AUTHED_PATHS,
  LOGIN_PATH,
  NEXT_MAX_LEN,
  isGuardedPath,
  loginRedirectUrl,
} from "./authedGuard.ts";

const here = dirname(fileURLToPath(import.meta.url));
const SRC = join(here, "..");
const APP = join(SRC, "app");

/** Directories under src/app that contain a page.tsx. */
function pageDirs(): string[] {
  return readdirSync(APP)
    .filter((n) => !n.startsWith(".") && statSync(join(APP, n)).isDirectory())
    .filter((n) => {
      try {
        return statSync(join(APP, n, "page.tsx")).isFile();
      } catch {
        return false;
      }
    });
}

test("the guard matches the dashboard pages and every path under them", () => {
  for (const p of AUTHED_PATHS) {
    assert.equal(isGuardedPath(p), true, `${p} itself must be guarded`);
    assert.equal(isGuardedPath(`${p}/`), true, `${p}/ must be guarded`);
    assert.equal(isGuardedPath(`${p}/nested`), true, `${p}/nested must be guarded`);
  }
  // /products/[id]/edit is a real route under a guarded page.
  assert.equal(isGuardedPath("/products/abc/edit"), true);
});

test("the guard never matches a public page, an API path or an asset", () => {
  const publicPaths = [
    "/",
    "/login",
    "/login/",
    "/magic-link",
    "/post-login",
    "/reset-password",
    "/landing",
    "/docs",
    "/docs/some-entry",
    "/brand",
    "/connect",
    "/mcp",
    "/legal",
    "/space-picker",
    // A guarded name must not leak into a longer sibling name.
    "/profiles",
    "/salesforce",
    "/admin-login",
    // The API and static assets are not pages.
    "/api/auth/profile",
    "/api/rest/dashboard-bundle",
    "/_next/static/chunks/main.js",
    "/favicon.ico",
  ];
  for (const p of publicPaths) {
    assert.equal(isGuardedPath(p), false, `${p} must NOT be guarded`);
  }
});

test("every app page that needs a session is listed, and no public page is", () => {
  const authedByCode: string[] = [];
  const publicDirs: string[] = [];
  for (const d of pageDirs()) {
    const src = readFileSync(join(APP, d, "page.tsx"), "utf8");
    // The two marks the codebase already uses for "this page needs a session":
    // it consumes useSession(), and/or it redirects a dead session to /login.
    const needsSession =
      /useSession\s*\(/.test(src) || /router\.replace\(\s*"\/login"\s*\)/.test(src);
    (needsSession ? authedByCode : publicDirs).push(d);
  }

  assert.ok(authedByCode.length > 10, `expected the dashboard pages, found ${authedByCode.length}`);
  for (const d of authedByCode) {
    assert.equal(
      isGuardedPath(`/${d}`),
      true,
      `/${d} consumes a session but is NOT in AUTHED_PATHS — an anonymous visitor still gets the account shell for it`,
    );
  }
  for (const d of publicDirs) {
    assert.equal(
      isGuardedPath(`/${d}`),
      false,
      `/${d} does not need a session but IS guarded — a cookie-less visitor would be locked out of it`,
    );
  }
});

test("the middleware matcher covers exactly the guarded paths", () => {
  const src = readFileSync(join(SRC, "middleware.ts"), "utf8");
  const matcher = [...src.matchAll(/"(\/[a-z-]+)\/:path\*"/g)].map((m) => m[1]);
  assert.ok(matcher.length > 10, `expected a literal matcher array, found ${matcher.length} entries`);
  assert.deepEqual(
    [...matcher].sort(),
    [...AUTHED_PATHS].sort(),
    "the matcher and AUTHED_PATHS must name the same paths — a path in one and not the other is not gated in production",
  );
});

test("the middleware reads the shared cookie constant and runs the guard", () => {
  const src = readFileSync(join(SRC, "middleware.ts"), "utf8");
  assert.ok(src.includes("SESSION_COOKIE"), "the cookie name must come from lib/sessionCookie");
  assert.ok(
    !src.includes('"jf_session"') && !src.includes("'jf_session'"),
    "the cookie name must not be a literal — lib/sessionCookie.ts is the shared constant",
  );
  assert.ok(src.includes("isGuardedPath") && src.includes("loginRedirectUrl"));
});

test("the redirect target is a /login URL the login page will honour", () => {
  const url = new URL(loginRedirectUrl("https://jomform.com", "/profile", "?tab=passkeys"));
  assert.equal(url.pathname, LOGIN_PATH);
  assert.equal(url.origin, "https://jomform.com");
  const next = url.searchParams.get("next");
  assert.ok(next, "?next= must be set so the visitor returns to the page they asked for");
  // The login page's readNext(): absolute, http(s), same origin, under the cap.
  const parsed = new URL(next);
  assert.equal(parsed.origin, "https://jomform.com");
  assert.equal(parsed.pathname, "/profile");
  assert.equal(parsed.search, "?tab=passkeys");
  assert.ok(next.length <= NEXT_MAX_LEN);
});

test("a ?next= the login page would discard is not sent at all", () => {
  const long = "/settings?" + "x".repeat(NEXT_MAX_LEN);
  const url = new URL(loginRedirectUrl("https://jomform.com", long));
  assert.equal(url.search, "", "an over-long target must be dropped, not sent to be silently ignored");
});

test("the gate cannot lock out a visitor who holds a session cookie", () => {
  // The middleware returns next() for ANY cookie presence — it never inspects
  // the value — so a stale cookie still renders the page and the page's own
  // 401 bounce takes over. This pins that the guard is presence-only.
  const src = readFileSync(join(SRC, "middleware.ts"), "utf8");
  assert.match(src, /req\.cookies\.has\(SESSION_COOKIE\)/);
  assert.ok(!/cookies\.get\(SESSION_COOKIE\)/.test(src), "presence only, never the value");
});
