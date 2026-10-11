// The server-side gate for the dashboard pages. The DECISION lives in
// src/lib/authedGuard.ts (with the full rationale and the path list); this file
// is only the Next plumbing, so the decision is testable in the repo's own
// `node --test` runner — which cannot import `next/server` (measured: Node
// resolves the bare specifier to `next/server.js` and throws).
import { NextResponse, type NextRequest } from "next/server";
import { SESSION_COOKIE } from "./lib/sessionCookie";
import { isGuardedPath, loginRedirectPath, requestOrigin } from "./lib/authedGuard";

export function middleware(req: NextRequest) {
  const { origin, pathname, search } = req.nextUrl;

  // The matcher below already narrows this, but the check is repeated HERE so
  // the gate holds on its own: the two are pinned together by
  // authedGuard.test.ts, and a path the matcher forgets is still refused.
  if (!isGuardedPath(pathname)) return NextResponse.next();

  // HttpOnly, minted only by the API. Its ABSENCE means this browser has never
  // been through a login, so the page must not be rendered at all — that is the
  // bug this fixes (an anonymous visitor used to receive the account shell).
  // Its VALIDITY is not checked here and is not claimed: an expired or revoked
  // session still carries the cookie, the page renders, the dashboard-bundle
  // fetch 401s and the page's own client bounce handles it.
  if (req.cookies.has(SESSION_COOKIE)) return NextResponse.next();

  // The visitor is sent to the login page ON the host they are already on. Two
  // separate values, deliberately:
  //
  //   * the REDIRECT TARGET is anchored on the SERVER's own origin, which no
  //     request header can influence, so a forged X-Forwarded-Host can never
  //     move the redirect itself (an absolute target built on a header would be
  //     an open redirect). Next relativises a same-origin target into a plain
  //     `/login?…` Location — measured on this build both locally and on prod
  //     (`location: /login?next=…`), and independently of the Host header (the
  //     local run's own origin is `0.0.0.0:<port>` while Host is
  //     `127.0.0.1:<port>`, and the Location is still relative). A plain
  //     `Response` with a relative Location is NOT an option here: Next parses
  //     the header as a URL and answers 500 `ERR_INVALID_URL` (measured).
  //   * the ORIGIN THE BROWSER IS ON travels inside `?next=`, which the login
  //     page validates and drops if it is not its own — the server cannot know
  //     it from its own URL (see requestOrigin).
  const visitor = requestOrigin(req.headers, origin);
  return NextResponse.redirect(new URL(loginRedirectPath(visitor, pathname, search), origin));
}

// Kept a literal array so Next can analyse it at build time. `/:path*` covers
// the bare path and every path under it (`/products/123/edit`).
export const config = {
  matcher: [
    "/activity/:path*",
    "/admin/:path*",
    "/builder/:path*",
    "/chat/:path*",
    "/deliveries/:path*",
    "/feedback/:path*",
    "/form-settings/:path*",
    "/forms/:path*",
    "/media/:path*",
    "/products/:path*",
    "/profile/:path*",
    "/sales/:path*",
    "/settings/:path*",
    "/spam/:path*",
    "/subscriptions/:path*",
    "/templates/:path*",
    "/workspace-picker/:path*",
  ],
};
