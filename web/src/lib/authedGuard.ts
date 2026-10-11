// The SERVER-SIDE gate for the pages inside the signed-in shell.
//
// WHY THIS EXISTS. Every dashboard page bounces an anonymous visitor itself —
// after hydration, from `useSession()`'s 401 (`if (anon) router.replace(…)`)
// — because the session is a CLIENT fetch. That guard cannot run before the
// HTML is sent, so the account shell (nav, "Sign out", the page's own tab
// strip) reached an anonymous visitor as static markup and only disappeared
// once JavaScript ran. Measured 2026-10-11: `GET https://jomform.com/profile`
// with no cookies answered 200 with the shell in the HTML (21961 bytes), and
// so did /settings, /sales and /chat.
//
// The server has exactly ONE signal before any JavaScript runs: the session
// cookie. `/` already decides what to render from it (`src/app/page.tsx`, which
// serves the marketing landing to a cookie-less crawler and the dashboard to a
// cookie-bearing visitor). This module extends that same decision to the
// dashboard paths: no cookie means the visitor has never been through a login,
// so send them to /login BEFORE the page renders.
//
// PRESENCE, NOT VALIDITY. The cookie is HttpOnly and only the API mints it, so
// its ABSENCE is trustworthy; its VALIDITY is not — an expired or revoked
// session still carries it. That case is unchanged: the page renders, the
// dashboard-bundle fetch 401s, and the page's own client bounce takes over
// (that is the documented contract in `lib/sessionCookie.ts`). So this gate can
// only ever ADD a redirect for a visitor who is certainly anonymous, and can
// never lock out a visitor who holds a session cookie.
//
// THE PATH LIST IS A DENY LIST, DELIBERATELY. Guarding "everything except
// <public list>" fails in the dangerous direction: one forgotten public route
// (a password-reset or magic-link landing, an OAuth mid-flow page) locks real
// users out of a flow they cannot complete. A forgotten entry HERE only keeps
// today's behaviour for that page, which is what `authedGuard.test.ts` guards:
// it reads the app directory and fails if a dashboard page uses `useSession()`
// without being listed, so a new page cannot be forgotten silently.
//
// WHY IT IS NOT A `catch-all`: the Next app never serves tenant form pages —
// measured 2026-10-11, `test-wan.jomform.com/test` is answered by the Go API
// (`x-request-id`, no `x-nextjs-*`) while the dashboard host answers with
// `x-powered-by: Next.js`. Tenant hosts therefore cannot collide with these
// paths, and the guard stays a plain path check with no host logic.
export const AUTHED_PATHS = [
  "/activity",
  "/admin",
  "/builder",
  "/chat",
  "/deliveries",
  "/feedback",
  "/form-settings",
  "/forms",
  "/media",
  "/products",
  "/profile",
  "/sales",
  "/settings",
  "/spam",
  "/subscriptions",
  "/templates",
  "/workspace-picker",
] as const;

export const LOGIN_PATH = "/login";

// The login page's own cap on a `next` value (`readNext` in app/login/page.tsx).
export const NEXT_MAX_LEN = 700;

/** True when `pathname` is one of the dashboard pages, or a path under one. */
export function isGuardedPath(pathname: string): boolean {
  // "/products/" and "/products" are the same page; Next normalises the former
  // away, but a trailing slash must not slip past the comparison.
  const p = pathname.length > 1 && pathname.endsWith("/") ? pathname.slice(0, -1) : pathname;
  return AUTHED_PATHS.some((a) => p === a || p.startsWith(a + "/"));
}

/**
 * The /login URL an anonymous visitor of `origin`+`pathname` should land on.
 *
 * `?next=` carries the page they asked for, which is the mechanism the OAuth
 * authorize flow already uses (issue #140) — a signed-in visitor comes back to
 * /profile instead of the dashboard root. The login page requires an ABSOLUTE,
 * same-origin http(s) URL there (`readNext` parses it with `new URL` and
 * rejects anything whose origin is not the app's), so the value is built from
 * the request's own origin rather than from anything the caller supplied.
 */
export function loginRedirectUrl(origin: string, pathname: string, search = ""): string {
  const url = new URL(LOGIN_PATH, origin);
  const target = origin + pathname + search;
  // A `next` the login page would discard is worse than none: it would look
  // deliberate while silently doing nothing.
  if (target.length <= NEXT_MAX_LEN) url.searchParams.set("next", target);
  return url.toString();
}
