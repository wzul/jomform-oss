// The ONE rule for a post-sign-in `?next=` target (card t_5e39dc9f).
//
// WHY THIS MODULE EXISTS. The SSR gate sends an anonymous visitor of a guarded
// path to `/login?next=<the absolute URL they asked for>`
// (web/src/middleware.ts → lib/authedGuard.ts `loginRedirectPath`), and the
// login page honours that value after a successful sign-in. The rule that
// decides WHICH values may be honoured used to live only inside
// `app/login/page.tsx` (`readNext`), so the other half of the same promise —
// the DEFAULT passwordless (magic-link) flow — could not reach it at all: the
// emailed link carried no `next`, and `app/magic-link/page.tsx` hard-coded
// `resolvePostLogin("")`. A visitor bounced from /profile and signing in through
// the emailed link therefore landed on /workspace-picker.
//
// The fix keeps ONE copy of the rule and lets both surfaces use it:
//   - `app/login/page.tsx`  validates the `?next=` it was given and forwards it
//     with the magic-link request (the server puts it in the email link),
//   - `app/magic-link/page.tsx` validates the `next` the emailed link carries
//     before navigating to it.
//
// WHAT THE RULE IS (unchanged from the login page's `readNext`): the value must
// be an ABSOLUTE http(s) URL whose origin is the browser's own origin, its
// `www.` alias, or NEXT_PUBLIC_OAUTH_BASE; it must be within NEXT_MAX_LEN.
// Everything else — relative, protocol-relative, another scheme, a foreign
// host, oversized — resolves to "" and the caller falls back to today's
// behaviour (resolvePostLogin("")), so there is no open redirect and no new
// error surface.
//
// WHY THE `www.` ALIAS IS PART OF THE RULE. The emailed link is always built on
// the API's own base URL (prod: https://jomform.com), but the visitor may have
// been gated on the app's other spelling — measured on prod 2026-10-11,
// `www.jomform.com/profile` hands /login `next=https://www.jomform.com/profile`.
// That value's origin is the app, not a foreign host; without the alias the
// apex-rendered magic-link page would drop it and that visitor would still land
// on the picker. The alias is derived from the BROWSER's own host (never from
// the value being validated), so only `h` and `www.h` of the origin the page is
// already on can ever pass. The session cookie survives the hop on prod: it is
// issued with Domain `.jomform.com` (httpapi.SharedCookieDomain).
export const NEXT_MAX_LEN = 700;

// The OAuth authorize flow (issue #140) redirects to /login?next=<authorize URL>
// on this host, so it is a legitimate second origin.
export const DEFAULT_OAUTH_BASE = "https://mcp.jomform.com";

/**
 * `origin` with a leading `www.` toggled on its hostname, or "" when `origin`
 * is not a URL. Only ever used on the browser's OWN origin — see the header.
 */
function wwwAlias(origin: string): string {
  let u: URL;
  try {
    u = new URL(origin);
  } catch {
    return "";
  }
  if (!u.hostname) return "";
  u.hostname = u.hostname.startsWith("www.") ? u.hostname.slice(4) : "www." + u.hostname;
  return u.hostname ? u.origin : "";
}

/** The origins a `?next=` value may name, derived from where the page is. */
export function nextAllowedOrigins(selfOrigin: string, oauthBase: string): Set<string> {
  const allowed = new Set<string>();
  for (const origin of [selfOrigin, wwwAlias(selfOrigin), oauthBase]) {
    if (origin) allowed.add(origin);
  }
  return allowed;
}

/**
 * The target to honour for `raw`, or "" when it must be dropped.
 *
 * Order matters only for the reader: `new URL` already rejects a
 * protocol-relative value ("//host") because it has no scheme to parse against,
 * so the explicit check below is belt-and-braces rather than the deciding rule.
 */
export function safeNextTarget(
  raw: string | null | undefined,
  selfOrigin: string,
  oauthBase: string = DEFAULT_OAUTH_BASE,
): string {
  const value = raw || "";
  if (!value) return "";
  if (value.startsWith("//")) return "";
  let u: URL;
  try {
    u = new URL(value);
  } catch {
    return "";
  }
  if (u.protocol !== "https:" && u.protocol !== "http:") return "";
  if (!nextAllowedOrigins(selfOrigin, oauthBase).has(u.origin)) return "";
  // Sanity cap on the value itself (an OAuth authorize URL is long, but not
  // unbounded) — the same 700 bytes the SSR gate caps its own value with.
  if (u.href.length > NEXT_MAX_LEN) return "";
  return u.href;
}

/** NEXT_PUBLIC_OAUTH_BASE, or the documented default. */
export function oauthBaseOrigin(): string {
  return (process.env.NEXT_PUBLIC_OAUTH_BASE as string) || DEFAULT_OAUTH_BASE;
}

/**
 * Validate `raw` against the BROWSER's own origin. Returns "" during SSR
 * (prerender has no window to compare against), exactly like the login page's
 * original `readNext`.
 */
export function browserNextTarget(raw: string | null): string {
  if (typeof window === "undefined") return "";
  return safeNextTarget(raw, window.location.origin, oauthBaseOrigin());
}
