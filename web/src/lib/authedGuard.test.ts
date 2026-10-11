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
//   6. THE `?next=` ORIGIN IS THE SERVER'S, NOT THE VISITOR'S (card
//      t_788c06ce). `loginRedirectUrl` used to be handed `req.nextUrl.origin`,
//      which on this build is the server's own identity (`0.0.0.0:3211` on a
//      local standalone run, the container's `0160b50ada3c:3000` on prod) and
//      never the host the browser is on — so the login page discarded the
//      value and the visitor was not returned to the page they asked for. The
//      origin is resolved from the request's own proxy headers in
//      `requestOrigin`, and the redirect TARGET stays a relative path so a
//      forged header can never move the redirect itself.
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
  loginRedirectPath,
  requestOrigin,
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
  assert.ok(src.includes("isGuardedPath") && src.includes("loginRedirectPath"));
});

test("the middleware resolves the visitor's origin from the request headers", () => {
  const src = readFileSync(join(SRC, "middleware.ts"), "utf8");
  // The WIRING, not the helper's existence: the origin handed to the redirect
  // must be resolved from THIS request's headers, with `req.nextUrl.origin` as
  // the fallback only. Passing `origin` straight through is the defect (card
  // t_788c06ce) — it is the SERVER's own origin, which the login page drops.
  assert.match(
    src,
    /requestOrigin\(\s*req\.headers\s*,\s*origin\s*\)/,
    "middleware.ts must resolve the redirect origin with requestOrigin(req.headers, origin)",
  );
  assert.match(
    src,
    /loginRedirectPath\(\s*visitor\s*,/,
    "the resolved visitor origin is not what loginRedirectPath is given — ?next= must name the " +
      "origin the BROWSER is on, or readNext() discards it and the visitor is not returned to the " +
      "page they asked for",
  );
  assert.ok(
    !/loginRedirectPath\(\s*origin\s*,/.test(src),
    "middleware.ts hands loginRedirectPath `origin` (req.nextUrl.origin) again — that is the " +
      "server's own origin (measured: 0.0.0.0:3211 locally, 0160b50ada3c:3000 on prod), which the " +
      "login page's readNext() rejects",
  );
});

test("the redirect TARGET is anchored on the server's own origin, never on a header", () => {
  const src = readFileSync(join(SRC, "middleware.ts"), "utf8");
  // The other half of the same property, and the one that keeps a forged
  // X-Forwarded-Host from becoming an OPEN REDIRECT: the resolved (visitor)
  // origin may only appear inside `?next=`. The URL the browser is sent to is
  // built on `origin` — the server's own, which no request header can move.
  assert.match(
    src,
    /NextResponse\.redirect\(\s*new URL\(\s*loginRedirectPath\([^)]*\)\s*,\s*origin\s*\)\s*\)/,
    "middleware.ts must build the redirect target as `new URL(<login path>, origin)`; if the " +
      "resolved visitor origin (or anything else header-derived) is used as the BASE, a forged " +
      "X-Forwarded-Host moves the redirect itself — an open redirect, worse than the bug this fixes",
  );
});

test("the redirect target is a /login path the login page will honour", () => {
  // The value is a PATH (the middleware must not be able to redirect off-origin
  // — see the source guard above); what the login page reads is the absolute
  // `next` it carries.
  const path = loginRedirectPath("https://jomform.com", "/profile", "?tab=passkeys");
  assert.ok(path.startsWith(LOGIN_PATH), `expected a ${LOGIN_PATH} path, got ${path}`);
  const url = new URL(path, "https://evil.example"); // base irrelevant: the value is relative
  assert.equal(url.pathname, LOGIN_PATH);
  assert.equal(url.origin, "https://evil.example", "the path alone must never change the origin");
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
  const url = new URL(loginRedirectPath("https://jomform.com", long), "https://jomform.com");
  assert.equal(url.search, "", "an over-long target must be dropped, not sent to be silently ignored");
});

// ── the origin the visitor is actually on (card t_788c06ce) ─────────────────
//
// MEASURED, 2026-10-11, on the built artifact: with `Host: jomform.com`,
// `X-Forwarded-Host: jomform.com` and `X-Forwarded-Proto: https` on an anonymous
// GET /profile, the pre-fix guard answered
//   location: /login?next=https%3A%2F%2F0.0.0.0%3A3211%2Fprofile
// — the SERVER's own origin, because the standalone build takes it from
// HOSTNAME/PORT and ignores both Host spellings (prod: `0160b50ada3c:3000`).
// `readNext` compares origins exactly, so that value is discarded and the
// post-sign-in return path is lost. These arms pin the resolution rule.

/** A stub `Headers` — the repo's unit runner has the real one, but this keeps
 *  each arm's input readable and independent of the runtime's own defaults. */
function headersOf(map: Record<string, string>): { get(name: string): string | null } {
  const lower: Record<string, string> = {};
  for (const [k, v] of Object.entries(map)) lower[k.toLowerCase()] = v;
  return { get: (name: string) => lower[name.toLowerCase()] ?? null };
}

test("the origin comes from the proxy headers, not from the server's own URL", () => {
  // The prod shape: Traefik adds X-Forwarded-Host/Proto, the container's own
  // origin is its hostname and port.
  assert.equal(
    requestOrigin(
      headersOf({ "x-forwarded-host": "jomform.com", "x-forwarded-proto": "https", host: "0160b50ada3c:3000" }),
      "http://0160b50ada3c:3000",
    ),
    "https://jomform.com",
  );
  // A chain that rewrote Host but set no forwarded host: Host is the fallback.
  assert.equal(
    requestOrigin(headersOf({ host: "jomform.com", "x-forwarded-proto": "https" }), "http://localhost:3211"),
    "https://jomform.com",
  );
  // A forwarded list carries the closest hop first.
  assert.equal(
    requestOrigin(headersOf({ "x-forwarded-host": "jomform.com, jf-web", "x-forwarded-proto": "https" }), "http://localhost:3211"),
    "https://jomform.com",
  );
  // No proxy headers at all (a direct hit on the app): the server's own origin,
  // exactly as before this change.
  assert.equal(requestOrigin(headersOf({}), "http://localhost:3211"), "http://localhost:3211");
});

test("a host the proxy could not have meant is refused, never concatenated", () => {
  const fallback = "http://0160b50ada3c:3000";
  // Each of these would otherwise become part of a longer URL: the guard builds
  // `origin + pathname`, and `new URL(path, origin)` would follow a smuggled
  // host/path/userinfo into somewhere else entirely.
  const refused: Record<string, string> = {
    "evil.example/path": "a path smuggled into the host",
    "user@evil.example": "userinfo",
    "evil.example?x=1": "a query",
    "evil.example#f": "a fragment",
    "evil.example:99999": "an impossible port",
    "": "empty",
    "not a host": "whitespace",
  };
  for (const [host, why] of Object.entries(refused)) {
    assert.equal(
      requestOrigin(headersOf({ "x-forwarded-host": host, "x-forwarded-proto": "https" }), fallback),
      fallback,
      `${JSON.stringify(host)} (${why}) must fall back to the server's own origin, not be trusted`,
    );
  }
  // A protocol the proxy could not have meant leaves the scheme alone rather
  // than inventing one.
  assert.equal(
    requestOrigin(headersOf({ "x-forwarded-host": "jomform.com", "x-forwarded-proto": "javascript" }), "https://0160b50ada3c:3000"),
    "https://jomform.com",
    "an unknown X-Forwarded-Proto must not become the scheme; the trusted fallback's scheme is used",
  );
});

test("the resolved origin is what the login page needs, and only ?next= carries it", () => {
  // The whole property, end to end in pure code: a request that arrived through
  // the proxy produces a relative Location whose `next` is an absolute URL on
  // the VISITOR's origin — the shape readNext() accepts.
  const origin = requestOrigin(
    headersOf({ "x-forwarded-host": "jomform.com", "x-forwarded-proto": "https", host: "0160b50ada3c:3000" }),
    "http://0160b50ada3c:3000",
  );
  const location = loginRedirectPath(origin, "/profile");
  assert.equal(location.startsWith("/login"), true, `expected a relative /login path, got ${location}`);
  assert.equal(
    location.includes("0160b50ada3c"),
    false,
    "the container's own origin must not appear anywhere in the redirect",
  );
  const next = new URL(location, "https://jomform.com").searchParams.get("next");
  assert.ok(next);
  assert.equal(new URL(next).origin, "https://jomform.com");
  // The OTHER direction, stated so the defect cannot come back unnoticed: with
  // the server's own origin (what the pre-fix middleware passed), the login page
  // discards the value — this is exactly what the fix is against.
  const preFixLocation = loginRedirectPath("http://0160b50ada3c:3000", "/profile");
  const preFixNext = new URL(preFixLocation, "https://jomform.com").searchParams.get("next");
  assert.equal(new URL(preFixNext!).origin, "http://0160b50ada3c:3000");
  assert.notEqual(
    new URL(preFixNext!).origin,
    "https://jomform.com",
    "the pre-fix value's origin is not the visitor's — that is the defect (a value readNext drops)",
  );
});

test("the gate cannot lock out a visitor who holds a session cookie", () => {
  // The middleware returns next() for ANY cookie presence — it never inspects
  // the value — so a stale cookie still renders the page and the page's own
  // 401 bounce takes over. This pins that the guard is presence-only.
  const src = readFileSync(join(SRC, "middleware.ts"), "utf8");
  assert.match(src, /req\.cookies\.has\(SESSION_COOKIE\)/);
  assert.ok(!/cookies\.get\(SESSION_COOKIE\)/.test(src), "presence only, never the value");
});
