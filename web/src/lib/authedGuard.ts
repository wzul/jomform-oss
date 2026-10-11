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

/** Anything that can read a request header — `Headers`, and the test's stub. */
export type HeaderReader = { get(name: string): string | null };

/** A `X-Forwarded-*` list is comma-separated; the leftmost entry is the origin. */
function firstHeaderValue(raw: string | null): string {
  if (!raw) return "";
  const comma = raw.indexOf(",");
  return (comma === -1 ? raw : raw.slice(0, comma)).trim();
}

/** The scheme of an origin we already trust, as a URL scheme. */
function schemeOfOrigin(origin: string): string {
  try {
    return new URL(origin).protocol === "https:" ? "https:" : "http:";
  } catch {
    return "http:";
  }
}

/**
 * `scheme://host[:port]` when `host` really is a host and nothing more, else
 * `null`. This is a VALIDATION, not a formality: the value is caller-reachable
 * (a page can set X-Forwarded-Host with a same-origin fetch), and the guard's
 * caller concatenates it with a path — so a host carrying `@`, `/`, `?` or a
 * path of its own must not be allowed to become a longer URL by construction.
 * Anything that does not round-trip through `URL` as a bare origin is refused
 * and the caller falls back.
 */
function safeOrigin(scheme: string, host: string): string | null {
  if (!host || /[\s"\\]/.test(host)) return null;
  let u: URL;
  try {
    u = new URL(`${scheme}//${host}`);
  } catch {
    return null;
  }
  if (u.username || u.password) return null; // "user@host"
  if (u.pathname !== "/" || u.search || u.hash) return null; // "host/path?q#f"
  if (u.host.toLowerCase() !== host.toLowerCase()) return null; // normalisation is not a match
  return u.origin;
}

/**
 * The ORIGIN the visitor actually reached the app on.
 *
 * WHY THIS IS NOT `req.nextUrl.origin`. It is the SERVER's own idea of where it
 * is, and on this deployment that is not where the browser is. Measured
 * 2026-10-11 on the built standalone artifact (PORT=3211, `Host: jomform.com`,
 * `X-Forwarded-Host: jomform.com`, `X-Forwarded-Proto: https` on an anonymous
 * `GET /profile`): `location: /login?next=https%3A%2F%2F0.0.0.0%3A3211%2Fprofile`
 * — the origin is the server's own, and neither Host spelling changes it (only
 * `X-Forwarded-Proto` does, and it changes the scheme). Measured again on prod
 * (`web 19a67f29` / v1.21.58): `next=https%3A%2F%2F0160b50ada3c%3A3000%2Fprofile`,
 * i.e. the container's hostname and port, because the standalone server builds
 * its origin from `HOSTNAME`/`PORT` (web/Dockerfile) and never from the request.
 *
 * WHY THE FORWARDED HOST IS READ FIRST. Traefik (v3.3 here,
 * deploy/vps/traefik-docker-compose.yml) ADDS X-Forwarded-Host from the request
 * it routed, and only trusts a client-supplied one when `forwardedHeaders` is
 * configured — this deployment sets neither `trustedIPs` nor `insecure`, so the
 * value is the proxy's own:
 *   https://doc.traefik.io/traefik/routing/entrypoints/ and the advisory it
 *   shares its shape with, https://github.com/traefik/traefik/security/advisories/GHSA-62c8-mh53-4cqv
 *   ("certain HTTP headers such as X-Forwarded-Host are added by Traefik before
 *   the request is routed to the application").
 * `Host` is the fallback for a chain that rewrote it, and the server's own
 * origin is the last resort — the pre-fix value, which the login page discards.
 * NOTE: this is the opposite precedence to `src/app/api/[...path]/route.ts`,
 * deliberately. There the web app is the one REPLACING Host (it dials the API
 * under its own name), so the connection's Host is the browser's. Here the app
 * is the backend: Host is whatever the proxy left, and the forwarded header is
 * the one the proxy vouches for.
 *
 * A WRONG OR FORGED RESULT IS NOT A REDIRECT. Only `?next=` uses this value and
 * `loginRedirectPath` returns a RELATIVE path, so the worst case is a `next` the
 * login page drops (`readNext` requires the browser's own origin) — the visitor
 * lands on /login, which is what they get today.
 */
export function requestOrigin(headers: HeaderReader, fallbackOrigin: string): string {
  const host =
    firstHeaderValue(headers.get("x-forwarded-host")) || firstHeaderValue(headers.get("host"));
  if (!host) return fallbackOrigin;
  const forwardedProto = firstHeaderValue(headers.get("x-forwarded-proto")).toLowerCase();
  const scheme = forwardedProto === "https" || forwardedProto === "http" ? `${forwardedProto}:` : schemeOfOrigin(fallbackOrigin);
  return safeOrigin(scheme, host) ?? fallbackOrigin;
}

/**
 * The relative `/login?next=…` path an anonymous visitor of `origin`+`pathname`
 * should land on.
 *
 * `?next=` carries the page they asked for, which is the mechanism the OAuth
 * authorize flow already uses (issue #140) — a signed-in visitor comes back to
 * /profile instead of the dashboard root. The login page requires an ABSOLUTE,
 * same-origin http(s) URL there (`readNext` parses it with `new URL` and rejects
 * anything whose origin is not the app's), so `origin` must be the origin the
 * BROWSER is on — see `requestOrigin`.
 *
 * THE RETURN VALUE IS A PATH, NOT A URL, ON PURPOSE (card t_788c06ce). The
 * origin above is caller-reachable, and building the redirect target as
 * `new URL(LOGIN_PATH, origin)` would let a forged X-Forwarded-Host move the
 * REDIRECT itself (`https://evil.example/login`) — an open redirect, worse than
 * the bug this fixes. Only the path is ever returned, so the browser can never
 * be sent off-origin; the origin survives in the `next` value, where the login
 * page validates it and drops it if it is not its own.
 */
export function loginRedirectPath(origin: string, pathname: string, search = ""): string {
  const url = new URL(LOGIN_PATH, origin);
  const target = origin + pathname + search;
  // A `next` the login page would discard is worse than none: it would look
  // deliberate while silently doing nothing.
  if (target.length <= NEXT_MAX_LEN) url.searchParams.set("next", target);
  return url.pathname + url.search;
}
