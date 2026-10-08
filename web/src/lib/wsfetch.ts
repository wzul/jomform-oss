// Issue #503: the ONE place a workspace-scoped load decides what a response
// means.
//
// Before this, every page hand-rolled the same three-step pattern and the
// failure handling drifted. The worst case was /workspace-picker:
//
//   const wsRes = await fetch("/api/workspaces");
//   if (wsRes.ok) { ...setWorkspaces... }   // no `else` — a 401 vanished
//
// `fetch` only rejects on a NETWORK failure, so an HTTP 401 resolves normally
// and the missing `else` swallowed it: no error, no state, and the page's
// "empty list" branch rendered a permanent "…". Wrapping that in try/catch does
// nothing — the catch never fires for a 401.
//
// This module makes the decision explicit and testable (see
// wsfetch.test.ts), so it cannot drift per page again:
//
//   401 / 403      → the session is gone: redirect to /login
//   other non-ok   → a real, VISIBLE error message (never silence)
//   2xx            → the parsed payload
//   network / bad JSON → the error message too

import { isScopeRefusal, workspaceScopeOf } from "./apiErr.ts";
import { dropWorkspaceScopeIfCurrent } from "./workspace.ts";

export type LoadFailureKind =
  // The session is no longer valid (401, or an uncoded 403): the user must
  // re-login.
  | "unauth"
  // The request carried a workspace the caller does not have, and the server
  // said so with its tenancy code. NOT a session failure: the stale slug is
  // dropped and the page refetches, and the user stays signed in (t_da4b2031).
  | "scope"
  // Any other HTTP failure (404/500/…): show the error.
  | "http"
  // The request never got an HTTP response (offline, DNS, malformed JSON).
  | "network";

export type LoadOutcome<T> =
  | { ok: true; data: T }
  | { ok: false; kind: LoadFailureKind; status: number; error: string };

/**
 * True for the statuses that mean "this session is no longer valid".
 *
 * `body` is the parsed failure body when the caller has it. A 403 is
 * OVERLOADED on this API: as well as a real authorization failure it carries
 * the TENANCY refusal for a `?workspace=` slug the session cannot resolve
 * (api/internal/httpapi/rest_scope.go's scopeOr403, code
 * `workspace.notFound`). Those two are the same status but not the same thing,
 * and conflating them is card t_da4b2031: a slug left in localStorage by a
 * workspace that was since renamed or deleted turned into a /login redirect for
 * a signed-in merchant.
 *
 * So a 403 that NAMES the tenancy code is not an unauth status. The split is a
 * machine-code distinction, not a second status — changing the server's 403 to a
 * 404 would change the public wire contract for every API-key, curl and MCP
 * caller to describe a condition its `code` already names.
 *
 * Omitting `body` keeps the old strict reading (a 403 is unauth), so a call site
 * that has not read the body can never mistake a foreign slug for a dead session
 * in the permissive direction.
 */
export function isUnauthStatus(status: number, body?: unknown): boolean {
  if (status === 401) return true;
  if (status !== 403) return false;
  return !isScopeRefusal(body);
}

/** Classify an HTTP status for the load-error UI. */
export function classifyStatus(status: number, body?: unknown): LoadFailureKind {
  if (status === 403 && isScopeRefusal(body)) return "scope";
  return isUnauthStatus(status, body) ? "unauth" : "http";
}

/** The HTTP status carried by a thrown error, or 0 when there is none. */
export function httpStatusOf(err: unknown): number {
  const s = (err as { status?: unknown } | null | undefined)?.status;
  return typeof s === "number" && s > 0 ? s : 0;
}

/** Classify a thrown load error (an HttpError, or a network/parse failure). */
export function classifyLoadError(err: unknown): { kind: LoadFailureKind; status: number } {
  const status = httpStatusOf(err);
  if (status > 0) return { kind: classifyStatus(status, errorBodyOf(err)), status };
  return { kind: "network", status: 0 };
}

/**
 * A non-2xx response, thrown so it can never be silently skipped. Carries the
 * HTTP `status` so the caller (and runLoad) can tell 401 from 500, plus the
 * parsed error BODY when the response had one.
 *
 * The body matters because of issue #6177: the API's failure body carries a
 * machine `code` the dashboard translates (`web/src/lib/apiErr.ts`). Without it
 * here, the only thing a failed load could render was the server's Malay prose
 * — which is exactly the bug.
 */
export class HttpError extends Error {
  status: number;
  body: unknown;
  /**
   * The `?workspace=` slug the failed request WAS SENT WITH, when the caller
   * knows it (card t_1cfb4044). A scope refusal is a fact about one slug, and
   * runLoad must route it to the slug it is about — see `dropWorkspaceScopeIfCurrent`
   * in lib/workspace.ts for the race an unattributed refusal causes.
   *
   * "" = unattributed, which the drop treats as "possibly the current one".
   */
  scopeSlug: string;
  constructor(status: number, body?: unknown, scopeSlug = "") {
    super(`HTTP ${status}`);
    this.name = "HttpError";
    this.status = status;
    this.body = body;
    this.scopeSlug = scopeSlug;
  }
}

/** The parsed error body carried by a thrown HttpError, or undefined. */
export function errorBodyOf(err: unknown): unknown {
  return (err as { body?: unknown } | null | undefined)?.body;
}

/**
 * The `?workspace=` slug a thrown load error is ABOUT, or "" when the caller
 * cannot say (card t_1cfb4044).
 *
 * Two sources, in order: the HttpError's own `scopeSlug` (set by fetchOrThrow
 * from the refusal body or the request url), then the body's `workspace` field
 * for an HttpError built by hand. "" is not "no scope" — it is "unattributed",
 * and the drop it feeds treats it conservatively.
 */
export function errorScopeSlug(err: unknown): string {
  const own = (err as { scopeSlug?: unknown } | null | undefined)?.scopeSlug;
  if (typeof own === "string" && own !== "") return own;
  return workspaceScopeOf(errorBodyOf(err));
}

/**
 * fetchOrThrow resolves with the Response on 2xx and throws an HttpError
 * carrying the status — and the parsed JSON error body when the server sent
 * one — on anything else.
 *
 * The body is read from a CLONE so a caller that already consumed the response
 * is unaffected, and a non-JSON body (a proxy error page, an empty 500) simply
 * leaves `body` undefined rather than throwing a parse error over the status.
 */
export async function fetchOrThrow(input: string, init?: RequestInit): Promise<Response> {
  const r = await fetch(input, init);
  if (!r.ok) {
    const body = await readErrorBody(r);
    // The slug is attributed from the BODY when the server names it and from the
    // REQUEST otherwise (card t_1cfb4044): the body is authoritative — it says
    // which scope was actually refused — while the request url says which scope
    // was asked for. They agree in every case where both exist; the url is the
    // fallback for the API as it stands today, which does not name it.
    throw new HttpError(r.status, body, workspaceScopeOf(body) || scopeSlugOfRequest(r));
  }
  return r;
}

/** Best-effort parse of a failure body. Never throws. */
export async function readErrorBody(r: Response): Promise<unknown> {
  try {
    return await r.clone().json();
  } catch {
    return undefined;
  }
}

/**
 * The `?workspace=` slug this Response was REQUESTED with, or "" when it carried
 * none (card t_1cfb4044).
 *
 * `fetch` does not expose the request URL on the Response in every runtime, but
 * `Response.url` is the FINAL url of the response, and for these same-origin calls
 * it is the request url. It is read defensively: an empty or unparsable url
 * attributes nothing, which the drop treats as the conservative case rather than
 * as a licence to act.
 */
export function scopeSlugOfRequest(r: Response): string {
  const url = r.url || "";
  const m = /[?&]workspace=([^&]*)/.exec(url);
  if (!m) return "";
  try {
    return decodeURIComponent(m[1]);
  } catch {
    return m[1];
  }
}

/** fetchOrThrow + JSON parse. */
export async function fetchJson<T>(input: string, init?: RequestInit): Promise<T> {
  const r = await fetchOrThrow(input, init);
  return (await r.json()) as T;
}

/**
 * classifyResponseFailure says what a non-2xx Response MEANS, for the call sites
 * that read a raw `fetch` response instead of going through runLoad (the chat
 * page's message and confirm/cancel lanes). It is the same decision runLoad
 * takes, so a hand-rolled `if (r.status === 401 || r.status === 403)` and the
 * shared wrapper cannot drift.
 *
 * The body is read from a CLONE, so the caller's own `await r.json()` still
 * works afterwards.
 */
export async function classifyResponseFailure(r: Response): Promise<LoadFailureKind> {
  return classifyStatus(r.status, await readErrorBody(r));
}

/**
 * runLoad turns a request into a UI decision.
 *
 * `onUnauth` is called with the status for 401/403 — it does the /login
 * navigation. Navigation is NOT done here so the decision stays unit-testable
 * without a router (and so each page keeps one navigation helper). `message` is
 * the already-translated error text to RENDER on every non-auth failure: a
 * failure is never silent, and never looks like an empty list.
 *
 * A 403 that NAMES the tenancy code (`workspace.notFound`) is the "scope"
 * outcome, not "unauth": it means the request's `?workspace=` is not one the
 * caller has, which is a stale localStorage value rather than a dead session
 * (t_da4b2031). `onUnauth` is NOT called for it, and the stale slug is removed
 * and the session notified so the page refetches — so the merchant is never
 * bounced to /login for their own stale selection. The localized tenancy sentence
 * is rendered in place, which is why the outcome still carries `error`.
 *
 * The slug that is removed is the one the FAILED REQUEST was sent with, not
 * whichever slug happens to be current when the refusal lands (t_1cfb4044): by
 * the time a slow refusal resolves the session may already have reconciled onto a
 * real workspace, and an unattributed drop would wipe it. See
 * `dropWorkspaceScopeIfCurrent`.
 *
 * Issue #6177: when the server's failure body carries a machine error `code`
 * the caller understands (`opts.errorKey`), that localized message is rendered
 * instead of `message` — a precise answer ("that workspace is not yours") beats
 * the page's generic "we couldn't load your forms". The server's PROSE is never
 * rendered either way; when the code is unknown the localized `message` wins.
 */
export async function runLoad<T>(
  request: () => Promise<T>,
  opts: {
    onUnauth: (status: number) => void;
    message: string;
    /** Optional: translate the failure's machine code (see lib/apiErr.ts). */
    errorKey?: (body: unknown) => string;
  },
): Promise<LoadOutcome<T>> {
  try {
    const data = await request();
    return { ok: true, data };
  } catch (err) {
    const { kind, status } = classifyLoadError(err);
    if (kind === "unauth") opts.onUnauth(status);
    // The scope refusal is handled ONCE, here: the slug is provably not the
    // caller's (the server just said so), so it must not be sent again, and the
    // page must not sit on an error for a scope it has already discarded.
    //
    // ROUTED TO THE SLUG IT IS ABOUT (card t_1cfb4044). The refusal is a fact
    // about the scope the failed request was SENT with, and between that send and
    // this landing the session may already have reconciled onto a workspace the
    // caller really has. Dropping unconditionally would then destroy a value
    // somebody else had just validated — the intermittent `/chat` failure this
    // card characterises. `dropWorkspaceScopeIfCurrent("")` (the no-slug-known
    // case) keeps the old unconditional behaviour, so a caller that cannot
    // attribute the refusal still un-sends a slug that is stale.
    if (kind === "scope") dropWorkspaceScopeIfCurrent(errorScopeSlug(err));
    let message = opts.message;
    if (opts.errorKey) {
      const translated = opts.errorKey(errorBodyOf(err));
      if (translated !== "") message = translated;
    }
    return { ok: false, kind, status, error: message };
  }
}
