// API error code → localized message (issue #6177).
//
// THE BUG. Every API failure answered with Malay prose and the dashboard
// rendered it verbatim:
//
//     setErr(d.error || t("tpl.useFail"));
//
// The fallback `t(...)` is localized; `d.error` — the branch that actually
// fires on a real server failure — was whatever Malay sentence the API sent. A
// user on `lang=en` therefore read "workspace tidak dijumpai".
//
// THE RULE, IN ONE PLACE. No component may render the server's `error` prose.
// It goes through the API's machine `code` (see
// api/internal/httpapi/errcode.go) and the dictionary below. The prose is not
// deleted from the wire — API-key callers, curl/MCP clients and the public
// buyer-form JavaScript consume it directly and cannot look up an i18n key — it
// is simply never what the dashboard PRINTS.
//
// UNKNOWN CODE → the caller's OWN localized message, never the prose. That is
// deliberate: the calling page knows what it was doing ("Failed to use
// template.") and that sentence is a better answer than a generic line the user
// has to interpret, while the prose would be in the wrong language entirely.
//
// Kept out of the .tsx component tree so the repo's Node test runner can import
// it directly (node --test executes TypeScript but cannot load .tsx).

// A TYPE-ONLY import, so the Node test runner (which cannot load `.tsx`) never
// has to: TypeScript erases it. `Key` already exists in components/i18n.tsx and
// there must not be a second, drifting definition of it.
import type { Key } from "../components/i18n";

/**
 * The error envelope the API sends on every failure (issue #6177):
 *
 *   { "error": "workspace tidak dijumpai", "code": "workspace.notFound" }
 *
 * `error` is the human wire prose (never rendered by the dashboard), `code` the
 * stable machine key. Older/other endpoints can answer without `code` (a proxy
 * error page, an OAuth error document), hence optional.
 */
export type ApiErrorBody = { error?: unknown; code?: unknown };

/**
 * Dictionary key per API code. Codes with no entry here are not translated and
 * fall back to the caller's message on purpose — see the module doc.
 *
 * This mirrors the specific codes in api/internal/httpapi/errcode.go; the
 * server's class codes (`request.badRequest`, `internal`, …) are deliberately
 * ABSENT: they say nothing a page cannot say better itself, and mapping them
 * would replace "Failed to save." with a vague generic sentence.
 */
export const API_ERROR_KEYS: Record<string, Key> = {
  // Tenancy / workspace scope.
  "workspace.notFound": "apiErr.workspaceNotFound",
  "workspace.slugTaken": "apiErr.workspaceSlugTaken",
  "workspace.slugInvalid": "apiErr.workspaceSlugInvalid",
  // A refused workspace DELETE. The page's own sentence ("We couldn't delete this
  // workspace.") hides WHY, and the why is actionable: the blockers list.
  "workspace.notEmpty": "apiErr.workspaceNotEmpty",

  // Roles / key scope.
  "role.manageRequired": "apiErr.manageRequired",
  "role.adminRequired": "apiErr.adminRequired",
  "scope.readonly": "apiErr.scopeReadonly",

  // Payments / CHIP / subscriptions.
  "payment.notConfigured": "apiErr.paymentNotConfigured",
  "payment.inactive": "apiErr.paymentInactive",
  "chip.credentialsInvalid": "apiErr.chipCredentials",
  "refund.notAllowed": "apiErr.refundNotAllowed",
  "subscription.cancelled": "apiErr.subscriptionCancelled",

  // Form / product / template validation.
  "form.statusInvalid": "apiErr.formStatusInvalid",
  "form.passwordTooShort": "apiErr.formPasswordTooShort",
  // Trash refusals: each names the obstacle the merchant has to clear, which is
  // exactly what a generic "we couldn't restore/delete this form" cannot say.
  "form.slugTaken": "apiErr.formSlugTaken",
  "form.hasSales": "apiErr.formHasSales",
  // The two refusals a form-slug RENAME answers with (the Form Settings slug
  // field). Deliberately distinct from `form.slugTaken` above: the restore is
  // unblocked by freeing the OTHER form's slug, a rename by choosing a
  // different slug for THIS form, and each sentence has to give its own remedy.
  "form.slugInUse": "apiErr.formSlugInUse",
  "form.slugInvalid": "apiErr.formSlugInvalid",
  // The product bin's refusals: a sale (history) or a live subscription.
  "product.hasSales": "apiErr.productHasSales",
  "product.hasSubscriptions": "apiErr.productHasSubscriptions",
  "product.outOfStock": "apiErr.productOutOfStock",
  "template.needMetadata": "apiErr.templateNeedMetadata",

  // URL scheme (urlguard) and outbound webhook URLs.
  "url.notHttps": "url.notHttps",
  "webhook.urlInvalid": "apiErr.webhookUrlInvalid",

  // Uploads / media.
  "upload.storageQuota": "apiErr.storageQuota",
  "upload.typeUnsupported": "apiErr.uploadTypeUnsupported",

  // Custom-domain TLS material.
  "domain.certInvalid": "apiErr.certInvalid",

  // Throttling.
  "rate.limited": "apiErr.rateLimited",

  // Account / auth.
  "email.taken": "apiErr.emailTaken",
  "email.invalid": "apiErr.emailInvalid",
  "email.domainUnreachable": "apiErr.emailDomainUnreachable",
  "email.unverified": "apiErr.emailUnverified",
  "password.tooShort": "apiErr.passwordTooShort",
  "password.invalid": "apiErr.passwordInvalid",
  "login.invalid": "apiErr.loginInvalid",
  "auth.stepUpRequired": "apiErr.stepUpRequired",

  // The social-connect ownership link (/connect). These answer on a page whose
  // only context is the emailed link itself, so "we couldn't connect this
  // account" would hide which of the four things actually went wrong.
  "connect.invalid": "apiErr.connectInvalid",
  "connect.expired": "apiErr.connectExpired",
  "connect.accountMissing": "apiErr.connectNoAccount",
  "connect.identityTaken": "apiErr.connectIdentityTaken",

  // "No token in the request", one per token-consuming surface (issue #6177
  // follow-up). Each endpoint used to answer the SAME wire literal
  // ("token diperlukan"), so no code could say which link to reopen; they now
  // have their own wording, their own code here, and their own sentence.
  //
  // connect.tokenMissing reuses the /connect page's OWN sentence (connect.stale)
  // rather than adding a fifth near-duplicate: the page already tells the user
  // exactly what to do when the link it was handed is incomplete.
  "verify.tokenMissing": "apiErr.verifyTokenMissing",
  "reset.tokenMissing": "apiErr.resetTokenMissing",
  "magicLink.tokenMissing": "apiErr.magicLinkTokenMissing",
  "connect.tokenMissing": "connect.stale",
};

/** The machine code in an API error body, or "" when there is none. */
export function apiErrorCode(body: unknown): string {
  const code = (body as ApiErrorBody | null | undefined)?.code;
  return typeof code === "string" ? code : "";
}

/**
 * The dictionary key for an API error body, or "" when the code is unknown to
 * this build. Returning "" (rather than a generic key) is the contract: the
 * caller renders its own localized message.
 */
export function apiErrorKey(body: unknown): Key | "" {
  const code = apiErrorCode(body);
  return code === "" ? "" : (API_ERROR_KEYS[code] ?? "");
}

/**
 * The one call site rule. `fallback` is the calling page's OWN translated
 * message; the server's prose is NEVER returned.
 *
 *   setErr(apiErr(d, t("tpl.useFail")));
 *
 * `t` is passed in rather than imported so this stays a pure function the Node
 * test runner can exercise with a fake translator.
 *
 * Note the parameter is the whole parsed body, not a string: taking `body.error`
 * would make the prose reachable again by construction, which is the bug.
 */
export function apiErr(
  body: unknown,
  fallback: string,
  t: (k: Key) => string,
): string {
  const key = apiErrorKey(body);
  return key === "" ? fallback : t(key);
}

/**
 * Translate an API error body to the user's language, or "" when the code is
 * unknown / absent. Saves each call site the two-step dance:
 *
 *     setErr(translateApiErr(d, t) || t("prod.saveFail"));
 *
 * The `|| fallback` is the contract, not an accident: an unknown code must land
 * on the page's own localized message, never on the server's prose.
 */
export function translateApiErr(body: unknown, t: (k: Key) => string): string {
  const key = apiErrorKey(body);
  return key === "" ? "" : t(key);
}

/**
 * True when the parsed body carries a NON-empty `error` string. Used only by
 * the source guard (src/lib/apiErr.test.ts) and by tests that need to prove a
 * payload really is a server-prose response; the dashboard must not use it to
 * decide what to print.
 */
export function hasServerProse(body: unknown): boolean {
  const e = (body as ApiErrorBody | null | undefined)?.error;
  return typeof e === "string" && e.trim() !== "";
}
