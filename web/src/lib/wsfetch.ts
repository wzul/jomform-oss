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

export type LoadFailureKind =
  // The session is no longer valid (401/403): the user must re-login.
  | "unauth"
  // Any other HTTP failure (404/500/…): show the error.
  | "http"
  // The request never got an HTTP response (offline, DNS, malformed JSON).
  | "network";

export type LoadOutcome<T> =
  | { ok: true; data: T }
  | { ok: false; kind: LoadFailureKind; status: number; error: string };

/** True for the statuses that mean "this session is no longer valid". */
export function isUnauthStatus(status: number): boolean {
  return status === 401 || status === 403;
}

/** Classify an HTTP status for the load-error UI. */
export function classifyStatus(status: number): LoadFailureKind {
  return isUnauthStatus(status) ? "unauth" : "http";
}

/** The HTTP status carried by a thrown error, or 0 when there is none. */
export function httpStatusOf(err: unknown): number {
  const s = (err as { status?: unknown } | null | undefined)?.status;
  return typeof s === "number" && s > 0 ? s : 0;
}

/** Classify a thrown load error (an HttpError, or a network/parse failure). */
export function classifyLoadError(err: unknown): { kind: LoadFailureKind; status: number } {
  const status = httpStatusOf(err);
  return status > 0 ? { kind: classifyStatus(status), status } : { kind: "network", status: 0 };
}

/**
 * A non-2xx response, thrown so it can never be silently skipped. Carries the
 * HTTP `status` so the caller (and runLoad) can tell 401 from 500.
 */
export class HttpError extends Error {
  status: number;
  constructor(status: number) {
    super(`HTTP ${status}`);
    this.name = "HttpError";
    this.status = status;
  }
}

/**
 * fetchOrThrow resolves with the Response on 2xx and throws an HttpError
 * carrying the status on anything else.
 */
export async function fetchOrThrow(input: string, init?: RequestInit): Promise<Response> {
  const r = await fetch(input, init);
  if (!r.ok) throw new HttpError(r.status);
  return r;
}

/** fetchOrThrow + JSON parse. */
export async function fetchJson<T>(input: string, init?: RequestInit): Promise<T> {
  const r = await fetchOrThrow(input, init);
  return (await r.json()) as T;
}

/**
 * runLoad turns a request into a UI decision.
 *
 * `onUnauth` is called with the status for 401/403 — it does the /login
 * navigation. Navigation is NOT done here so the decision stays unit-testable
 * without a router (and so each page keeps one navigation helper). `message` is
 * the already-translated error text to RENDER on every non-auth failure: a
 * failure is never silent, and never looks like an empty list.
 */
export async function runLoad<T>(
  request: () => Promise<T>,
  opts: { onUnauth: (status: number) => void; message: string },
): Promise<LoadOutcome<T>> {
  try {
    const data = await request();
    return { ok: true, data };
  } catch (err) {
    const { kind, status } = classifyLoadError(err);
    if (kind === "unauth") opts.onUnauth(status);
    return { ok: false, kind, status, error: opts.message };
  }
}
