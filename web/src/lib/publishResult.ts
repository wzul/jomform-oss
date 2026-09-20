// The publish outcome, and the ONE place that decides what the merchant is
// shown when they press Publish.
//
// THE GAP (onboarding drop-off, not a crash). Three outside signups built a form,
// saved it as a draft and never published. Nothing blocked them — the publish
// handler answers 200 — and after a successful publish the builder showed no
// confirmation, no URL and no way to discover the form was on the internet. The
// `View ↗` link is rendered only when a URL already resolved, so a first-time
// publish produced *nothing at all*: the page looked exactly as it did before.
//
// This module turns the API's answer into the three states the builder has to
// distinguish, so the decision is testable in the plain-node runner (this repo
// has no JSX test setup — the same pattern as src/lib/formMode.ts):
//
//   published   — the form is live. The public URL is surfaced, with an open and
//                 a copy action. This is the missing "your form is live" moment.
//   needs_ack   — a PAYMENT form with nothing to sell. The API refuses to publish
//                 it silently (409 + code=no_products, mirroring
//                 dbstore.PublishWarningNoProducts). The merchant is told what is
//                 wrong and has to confirm before it goes live.
//   error       — anything else, with the server's own message.
//
// TWO RULES THAT ARE LOAD-BEARING:
//
//   1. `PUBLISH_NEEDS_ACK` must equal the Go constant `dbstore.PublishWarningNoProducts`.
//      A cross-language guard in src/lib/publishResult.test.ts reads the Go file
//      and fails if they drift, so a rename cannot silently turn the warning into
//      a generic error the merchant cannot act on.
//   2. A URL is only ever turned into a link when it is a non-empty absolute
//      https:// address — `liveLink` returns null otherwise. An `<a href="">`
//      navigates to the CURRENT url (the reported "View form reloads /forms"
//      defect, t_457c4d87), so the confirmation panel must be incapable of
//      producing one. This is defence in depth for THIS surface; the resolution
//      itself is not fixed here.

/** Mirrors `dbstore.PublishWarningNoProducts` in the Go API. */
export const PUBLISH_NEEDS_ACK = "no_products";

export type PublishOutcome =
  | { kind: "published"; url: string; hasUrl: boolean }
  | { kind: "needs_ack"; message: string }
  | { kind: "error"; message: string };

/** The subset of a publish response this module reads. */
export type PublishResponse = { ok: boolean; status: number; body?: unknown };

/** The error string of a JSON body, or "" when the body carries none. */
function errorMessage(body: unknown): string {
  if (body && typeof body === "object") {
    const e = (body as { error?: unknown }).error;
    if (typeof e === "string") return e;
  }
  return "";
}

/** The stable failure token of a JSON body, or "" when it carries none. */
function errorCode(body: unknown): string {
  if (body && typeof body === "object") {
    const c = (body as { code?: unknown }).code;
    if (typeof c === "string") return c;
  }
  return "";
}

/**
 * publishOutcome classifies a publish response.
 *
 * `url` is the form's public URL as the API resolved it (may be "" when the
 * workspace slug could not be resolved — see t_457c4d87); it is reported as-is
 * and `hasUrl` says whether there is something to show. Neither this module nor
 * the panel ever invents a URL from a client-side guess.
 */
export function publishOutcome(res: PublishResponse, url: string): PublishOutcome {
  if (res.ok) {
    return { kind: "published", url: url || "", hasUrl: Boolean(liveLink(url)) };
  }
  const message = errorMessage(res.body);
  if (res.status === 409 && errorCode(res.body) === PUBLISH_NEEDS_ACK) {
    return { kind: "needs_ack", message };
  }
  return { kind: "error", message };
}

/**
 * liveLink returns the URL to put in an href, or null when there is nothing
 * navigable.
 *
 * Only an absolute https:// address qualifies. A relative path or a bare slug
 * would resolve against the dashboard's own origin and land the merchant back
 * inside the app; an empty string would navigate to the current page. Both are
 * the dead-link shape this repo already has a card for, so neither is allowed to
 * reach an anchor from here.
 */
export function liveLink(url: string | null | undefined): string | null {
  const u = (url || "").trim();
  if (!u) return null;
  if (!/^https:\/\/[^\s"'<>]+$/i.test(u)) return null;
  return u;
}

/**
 * acknowledgmentBody builds the publish body that confirms the
 * "payment form with nothing to sell" warning. It is exactly the one field, so a
 * retry can never carry a stale copy of anything else.
 */
export function acknowledgmentBody(): { acknowledge: true } {
  return { acknowledge: true };
}

/**
 * outcomeMessage returns the human-readable text of a failed outcome, or "" when
 * the outcome is not a failure.
 *
 * This exists so callers never have to narrow the union by hand — a
 * `kind !== "needs_ack"` test leaves `published | error`, and reading `.message`
 * off that is a compile error (the published branch has no message). A caller
 * that wants a fallback writes `outcomeMessage(o) || fallback`.
 */
export function outcomeMessage(o: PublishOutcome): string {
  return o.kind === "error" || o.kind === "needs_ack" ? o.message : "";
}

/**
 * needsProductsIDs reads the ids a BULK publish withheld because they are
 * payment forms with nothing to sell (`needs_products` on the 409 body). The
 * bulk route reports them as an array; anything else is "no ids" rather than a
 * crash, and the caller then just shows the server's message.
 */
export function needsProductsIDs(body: unknown): string[] {
  if (!body || typeof body !== "object") return [];
  const raw = (body as { needs_products?: unknown }).needs_products;
  if (!Array.isArray(raw)) return [];
  return raw.filter((v): v is string => typeof v === "string");
}
