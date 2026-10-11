// The server-side gate for the dashboard pages. The DECISION lives in
// src/lib/authedGuard.ts (with the full rationale and the path list); this file
// is only the Next plumbing, so the decision is testable in the repo's own
// `node --test` runner — which cannot import `next/server` (measured: Node
// resolves the bare specifier to `next/server.js` and throws).
import { NextResponse, type NextRequest } from "next/server";
import { SESSION_COOKIE } from "./lib/sessionCookie";
import { isGuardedPath, loginRedirectUrl } from "./lib/authedGuard";

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

  return NextResponse.redirect(loginRedirectUrl(origin, pathname, search));
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
