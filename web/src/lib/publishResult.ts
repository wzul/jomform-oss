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
//   error       — anything else. It carries the response BODY, never a sentence:
//                 the sentence is derived at render time by `publishErrorText`
//                 from the machine `code` (lib/apiErr.ts) plus the calling page's
//                 own localized fallback.
//
// THREE RULES THAT ARE LOAD-BEARING:
//
//   1. `PUBLISH_NEEDS_ACK` must equal the Go constant `dbstore.PublishWarningNoProducts`.
//      A cross-language guard in src/lib/publishResult.test.ts reads the Go file
//      and fails if they drift, so a rename cannot silently turn the warning into
//      a generic error the merchant cannot act on.
//   2. A URL is only ever turned into a link when it is a non-empty absolute
//      http(s):// address with a host — `liveLink` returns null otherwise. An
//      `<a href="">` navigates to the CURRENT url (the reported "View form
//      reloads /forms" defect, t_457c4d87), so the confirmation panel must be
//      incapable of producing one. http is allowed because a self-hosted
//      install's own origin may be http (see liveLink); everything that is not
//      an absolute address with a host is still refused. This is defence in
//      depth for THIS surface; the resolution itself is not fixed here.
//   3. THE REFUSAL'S TEXT COMES FROM THE DICTIONARY, NEVER FROM THE RESPONSE.
//      The `needs_ack` outcome carries NO message at all, and the sentence the
//      merchant reads comes from `publishConfirmText`. The API's prose for this
//      refusal is a Malay literal ("Borang bayaran ini tiada produk — …"), and
//      the API never receives the caller's language on a JSON call, so rendering
//      it put Malay in front of an English dashboard (the same defect class as
//      issue #6177, on a path #6177's `.error || fallback` rule could not see:
//      the call sites read `outcome.message`, a field this module filled from
//      the response's `error`). The prose stays on the wire for API-key and
//      curl/MCP clients, which cannot look up an i18n key; the dashboard simply
//      stops printing it. `publishResult.test.ts` makes the shape unrepresentable
//      and fails if any call site reads a message off a publish outcome again.
//
//      t_10453dd9 completed the rule: the FIRST pass made only the refusal
//      message-less, and the `error` kind still carried the server's prose in a
//      `message` field — so the same Malay sentence reached the dashboard through
//      the other branch (reproduced in a browser on the "Publish anyway" retry,
//      which answers an uncoded 409). A refusal is not the only publish failure
//      that can carry prose, so making one branch message-less was a fix to one
//      path of a class, not to the class. The `error` kind now carries the parsed
//      BODY (`body`) exactly as `needs_ack` carries nothing, and the only way to
//      turn either into a sentence is `publishErrorText` / `publishConfirmText`
//      below. There is no `message` field left for a call site to print.

// `apiErr` is the repo's ONE rule for turning an API failure into what the
// dashboard prints: the machine `code` plus the caller's localized fallback, and
// the server's prose never. Re-using it (rather than re-implementing the rule
// here) is what keeps the publish surfaces on the same rule as every other page
// — issue #6177.
//
// Imported with an explicit `.ts` extension because this module is loaded
// DIRECTLY by the repo's Node test runner (`node --test src/lib/*.test.ts`),
// which resolves ESM specifiers literally and cannot guess an extension. The
// other plain-`.ts` libs here do the same (see lib/workspace.ts importing
// ./apiErr.ts), and TypeScript's `allowImportingTsExtensions` plus the bundler's
// resolution make the extension harmless for the Next build.
import { apiErr } from "./apiErr.ts";
// A TYPE-ONLY import, so the Node test runner (which cannot load `.tsx`) never
// has to: TypeScript erases it. `Key` already exists in components/i18n.tsx and
// there must not be a second, drifting definition of it.
import type { Key } from "../components/i18n";

/** Mirrors `dbstore.PublishWarningNoProducts` in the Go API. */
export const PUBLISH_NEEDS_ACK = "no_products";

/**
 * Mirrors `dbstore.PublishWarningNoProductSelected` in the Go API (card
 * t_983425e5). The form HAS a product block; the block is just not pointed at a
 * product (its `product_id` is empty, or the product was purged). It is a
 * separate code from `no_products` because the remedy is different — the
 * merchant does not need to create a product, they need to pick one in the block
 * — and because the refusal names the block (`block_id`) so the builder can link
 * straight to it.
 */
export const PUBLISH_NO_PRODUCT_SELECTED = "no_product_selected";

/**
 * The dictionary key that holds the merchant-facing sentence for the no-products
 * refusal. It is a KEY, not a string: the sentence has to follow the dashboard
 * language, which only `t()` knows (rule 3 above).
 */
export const PUBLISH_NEEDS_ACK_KEY = "bld.noProductsWarn";

/**
 * The sentence for `PUBLISH_NO_PRODUCT_SELECTED`. It quotes what the buyer is
 * served ("This form is not accepting orders right now") because that is the fact
 * the merchant could not see, and names the empty product block.
 */
export const PUBLISH_NO_PRODUCT_SELECTED_KEY = "bld.noProductSelectedWarn";

export type PublishOutcome =
  | { kind: "published"; url: string; hasUrl: boolean }
  // Deliberately message-less: the refusal is rendered from the dictionary by
  // `publishConfirmText`, because the API's own prose for it is Malay (rule 3).
  // `code` picks WHICH refusal this is; `blockId` is the product block to link to
  // (empty when the form has no product block at all).
  | { kind: "needs_ack"; code: string; blockId: string }
  // ALSO message-less, and for the same reason. It carries the parsed response
  // body so the sentence can be derived from the machine `code` at render time
  // (`publishErrorText`) — a pre-rendered string here is exactly how the API's
  // Malay prose reached the dashboard (t_10453dd9, reproduced on the
  // "Publish anyway" retry's uncoded 409).
  | { kind: "error"; body: unknown };

/** The subset of a publish response this module reads. */
export type PublishResponse = { ok: boolean; status: number; body?: unknown };

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
 *
 * The no-products branch is keyed on the machine `code` (never on the message),
 * so it keeps working when the server rewords the prose — and so nothing here
 * has a reason to carry that prose any further.
 */
export function publishOutcome(res: PublishResponse, url: string): PublishOutcome {
  if (res.ok) {
    return { kind: "published", url: url || "", hasUrl: Boolean(liveLink(url)) };
  }
  const code = errorCode(res.body);
  if (res.status === 409 && (code === PUBLISH_NEEDS_ACK || code === PUBLISH_NO_PRODUCT_SELECTED)) {
    return { kind: "needs_ack", code, blockId: errorBlockID(res.body) };
  }
  return { kind: "error", body: res.body };
}

/** The product block id a refusal names, or "" when it names none. */
function errorBlockID(body: unknown): string {
  if (body && typeof body === "object") {
    const b = (body as { block_id?: unknown }).block_id;
    if (typeof b === "string") return b;
  }
  return "";
}

/**
 * liveLink returns the URL to put in an href, or null when there is nothing
 * navigable.
 *
 * An absolute http:// or https:// address with a host qualifies. Anything else
 * (relative path, bare slug, protocol-relative //host, javascript:, empty) does
 * not: a relative path would resolve against the dashboard's own origin and land
 * the merchant back inside the app, and an empty string would navigate to the
 * current page. Both are the dead-link shape this repo already has a card for.
 *
 * WHY http IS ACCEPTED (card t_6c0badbe). This used to require https, and on a
 * SELF-HOSTED install that hid the only address that works. There the published
 * form lives on the operator's own origin — `{APP_BASE_URL}/f/{slug}` — and
 * APP_BASE_URL is legitimately `http://` for a local trial (docs/SELF-HOST.md
 * documents `bash deploy/selfhost/bootstrap.sh --base-url http://localhost:8080`).
 * Measured on a clean self-hosted stack: the API answered
 * `http://localhost:18099/f/browser-pub`, the builder printed "The form is live,
 * but its public URL is not available yet", and neither Open form nor Copy link
 * was rendered — a dead end on a working install. https-only never protected
 * against an injected host (an https URL from the server is just as injectable);
 * what it protects is the SCHEME, and http(s) with a real host is a real link.
 */
export function liveLink(url: string | null | undefined): string | null {
  const u = (url || "").trim();
  if (!u) return null;
  // The scheme and a host are both required: `http://` alone and `http:///path`
  // (empty authority) are not navigable addresses.
  if (!/^https?:\/\/[^\s"'<>\/][^\s"'<>]*$/i.test(u)) return null;
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
 * publishErrorText returns the localized sentence for a FAILED publish outcome,
 * or "" when the outcome is not a failure.
 *
 * This is the ONLY way a failed publish outcome becomes text (rule 3):
 *
 *   * a `needs_ack` outcome returns "" — its sentence comes from
 *     `publishConfirmText` (it is a question, not an error);
 *   * an `error` outcome goes through `apiErr` on the outcome's BODY, i.e. the
 *     API's machine `code` plus `fallback`, the calling page's OWN translated
 *     sentence. An unknown or absent code lands on `fallback`; the server's prose
 *     is never returned.
 *
 * `fallback` is passed in (rather than defaulted here) so the sentence stays the
 * calling page's own — the page knows what it was doing, and a generic line from
 * this module would replace "Failed to save." with something vaguer.
 *
 * The previous shape, `outcomeMessage(o) || t("…")`, could not uphold that: the
 * outcome carried a `message` string the module had filled from the response's
 * `error`, so `||` never fired and the API's Malay prose reached the screen
 * (t_10453dd9). There is no such field now.
 */
export function publishErrorText(o: PublishOutcome, fallback: string, t: Translate): string {
  return o.kind === "error" ? apiErr(o.body, fallback, t) : "";
}

/**
 * Minimal shape of the `t()` this module needs. It is `apiErr`'s translator: any
 * i18n key at all, because the sentence now comes from the shared machine-code
 * dictionary rather than from one hard-coded key.
 */
export type Translate = (key: Key) => string;

/**
 * publishConfirmText returns the localized sentence a caller shows (or asks the
 * merchant to confirm) for a publish outcome, or "" when the outcome is not one
 * the merchant is being asked about.
 *
 * It exists so the no-products refusal has exactly ONE rendering in the whole
 * dashboard, in the merchant's language, with no path back to the server's prose.
 */
export function publishConfirmText(o: PublishOutcome, t: Translate): string {
  if (o.kind !== "needs_ack") return "";
  return t(o.code === PUBLISH_NO_PRODUCT_SELECTED ? PUBLISH_NO_PRODUCT_SELECTED_KEY : PUBLISH_NEEDS_ACK_KEY);
}

/**
 * needsProductsIDs reads the ids a BULK publish withheld because they are
 * payment forms with nothing to sell (`needs_products` on the 409 body). The
 * bulk route reports them as an array; anything else is "no ids" rather than a
 * crash, and the caller then just shows the localized warning.
 */
export function needsProductsIDs(body: unknown): string[] {
  if (!body || typeof body !== "object") return [];
  const raw = (body as { needs_products?: unknown }).needs_products;
  if (!Array.isArray(raw)) return [];
  return raw.filter((v): v is string => typeof v === "string");
}
