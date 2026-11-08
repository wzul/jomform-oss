// API error code → localized message (issue #6177).
//
// THE BUG. Every API failure answered with Malay prose and the dashboard
// rendered it verbatim:
//
//     setErr(d.error || t("tpl.useFail"));
//
// The fallback `t(...)` is localized; `d.error` — the branch that actually
// fires on a real server failure — was whatever Malay sentence the API sent. A
// user on `lang=en` therefore read "workspace not found".
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
 *   { "error": "workspace not found", "code": "workspace.notFound" }
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
  // The workspace NAME cap. Mapped for the same reason the two slug refusals
  // are: the name field is capped client-side, but a paste or an assistive-tech
  // path can still exceed it, and without this the merchant would be told their
  // ADDRESS was invalid for a refusal that is about the NAME. The code is the
  // authority on why, so the sentence is the name's own.
  "workspace.nameTooLong": "apiErr.workspaceNameTooLong",
  // A form create refused because the ACCOUNT has no workspace at all (card
  // t_6c0badbe). Distinct from workspace.scopeRequired below: that one means the
  // request did not NAME a workspace, and its remedy is a ?workspace= parameter.
  // This one means there is no workspace to name yet — the merchant must create
  // one — and the stored row it used to produce could be published and was then
  // 404 at every address.
  "workspace.required": "apiErr.workspaceRequired",
  // The reserved-SUBDOMAIN refusal (t_586ff0c3). It is NOT a `Code*` constant in
  // api/internal/httpapi/errcode.go — the token lives in urlguard
  // (ReservedSubdomainCode, mirrored by lib/workspaceSlug.ts) and both write
  // paths carry it verbatim, because it is a POLICY answer rather than a shape
  // one: the name is well formed and simply belongs to JomForm.
  //
  // It is mapped here as well as in workspace.ts's create path because the SAME
  // refusal reaches the dashboard through a second surface — the Settings →
  // Workspace rename card (WorkspaceTab.tsx doRename), which routes its failures
  // through translateApiErr. Without this row that card would answer its own
  // generic "couldn't rename" sentence for a refusal it can explain exactly.
  //
  // Mapping it to the EXISTING sentence rather than a second one: create and
  // rename refuse the same thing for the same reason, so two near-duplicate
  // strings would be two things to keep in step (the repo's rule against a
  // second copy of a rule).
  reserved_subdomain: "workspace.slugReserved",
  // A refused workspace DELETE. The page's own sentence ("We couldn't delete this
  // workspace.") hides WHY, and the why is actionable: the blockers list.
  //
  // RETIRED WITH ITS GUARD (workspace trash): the DELETE no longer refuses on
  // content, so the server never emits this code any more. Kept OUT of the map
  // deliberately — apiErr.test.ts fails on a mapping for a code errcode.go does
  // not declare at all ("every code this dictionary maps is one the server still
  // declares"), which is what stops this entry rotting into a translation nothing
  // can reach. Re-adding the row below without the code turns that guard red.
  //
  // The dictionary row it points at is NOT the Trash view's copy, although this
  // comment used to claim it was (measured: no call site renders
  // `apiErr.workspaceNotEmpty`; the Trash section renders its own rows —
  // `ws.notEmpty` / `ws.notEmptyHint` — because a move to the Trash destroys
  // nothing and so has nothing to refuse). The row is kept as the retired
  // refusal's sentence: i18nKeys.test.ts deliberately does not police dead keys.
  // "workspace.notEmpty": "apiErr.workspaceNotEmpty",
  //
  // A refused permanent PURGE: the workspace was ever sold, and a purge destroys
  // the merchant's record of money received. The count is in the body.
  "workspace.hasSales": "apiErr.workspaceHasSales",
  // The chat lane's ambiguous-scope refusal (t_fb393abe). Mapped because the
  // chat page must be able to tell THIS 400 from any other: a 400 from
  // /chat/confirm is not necessarily a scope refusal — the server maps every
  // unrecognised confirm failure to 400 too, including a tool it refused after
  // claiming the token — and only the code can separate them.
  //
  // It reuses the chat page's OWN sentence (chat.scopeRequired) rather than
  // adding a near-duplicate: the page says exactly what to do about this refusal
  // today, and a second wording would be a second thing to keep in step. Same
  // precedent as connect.tokenMissing below.
  "workspace.scopeRequired": "chat.scopeRequired",
  // The chat lane's "this card cannot be carried out" refusal (t_623676d1). A
  // proposal whose arguments the target tool would never accept is refused
  // BEFORE the token is claimed — so the token is UNSPENT and the same card can
  // never succeed. It gets its own sentence rather than the spent/expired one:
  // "expired or was already used" is false of it (the card was neither) and would
  // hide the one thing the user needs to know, which is that their token is still
  // good and a fresh card is the fix.
  "chat.proposalArgsInvalid": "chat.proposalUnexecutable",

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
  // The reserved-name refusal for a form slug (card t_f76f81eb). Like
  // `reserved_subdomain` above it is NOT a `Code*` constant in errcode.go: the
  // token lives in urlguard (FormSlugReservedCode, mirrored by lib/formSlug.ts)
  // and both form write paths carry it verbatim, because the wire message NAMES
  // the refused label and so can never be a key in the message→code table it is
  // a POLICY answer, not a shape one.
  "form.slugReserved": "apiErr.formSlugReserved",
  // The custom_js scan refusal (#388) — card t_2717ba30. The server assembles
  // this 400 around the scan's own reason ("custom_js ditolak: active code
  // execution marker: eval("), so before this code existed it shipped as
  // `request.badRequest` and /form-settings' Save rendered the bare word
  // "Failed": the merchant could not tell WHICH control was refused or why
  // (measured on prod v1.21.296). One code covers every door that writes that
  // sentence — REST form create/update, REST template create/update/use and the
  // MCP form/template tools — so one sentence here cannot miss a route.
  "form.customJsRejected": "apiErr.formCustomJsRejected",
  // The product bin's refusals: a sale (history) or a live subscription.
  "product.hasSales": "apiErr.productHasSales",
  "product.hasSubscriptions": "apiErr.productHasSubscriptions",
  "product.outOfStock": "apiErr.productOutOfStock",
  // A shipping-zone-set save that would DELETE a set a form still points at
  // (the dashboard's Form Settings → Shipping tab). The API refusal names the
  // set and the forms, but the DASHBOARD must not render that Malay prose
  // (#6177), and its own generic sentence ("Failed to save shipping rates")
  // hid the one actionable fact: a form has to be re-pointed first.
  "shipping.setInUse": "apiErr.shipSetInUse",
  // A shipping-zone-set save refused for its SHAPE (card t_417fbacb): a set with
  // no name, a set with no zone, an unnamed zone, a zone with no rate, a negative
  // price or an inverted weight band. ValidateShippingSets wraps every one of
  // those sentences in dbstore.ShippingInvalidLeadIn, which the server keys this
  // code on, and the page's own "Failed to save shipping rates" named neither the
  // field nor the reason. One sentence covers the class because the remedy is the
  // same for every shape.
  "shipping.invalid": "apiErr.shipInvalid",
  // The TYPED-CONFIRMATION refusals — the merchant mistyped the value the
  // destructive call has to name back (a form's slug, a workspace's slug, a
  // product's name). Each is a DIFFERENT sentence from the obstacle refusals
  // above: nothing is blocking the delete, the caller simply has not confirmed
  // it, and the remedy is to type the value — not to clear a sale or free a
  // slug. Without these three codes the server answered the class code
  // `request.badRequest`, which this dictionary deliberately does not
  // translate, so the merchant read the API's Malay prose (issue #6177).
  "form.confirmSlugMismatch": "apiErr.formConfirmSlugMismatch",
  "product.confirmNameMismatch": "apiErr.productConfirmNameMismatch",
  "workspace.confirmSlugMismatch": "apiErr.workspaceConfirmSlugMismatch",
  "template.needMetadata": "apiErr.templateNeedMetadata",

  // URL scheme (urlguard) and outbound webhook URLs.
  "url.notHttps": "url.notHttps",
  "webhook.urlInvalid": "apiErr.webhookUrlInvalid",

  // Uploads / media.
  "upload.storageQuota": "apiErr.storageQuota",
  "upload.typeUnsupported": "apiErr.uploadTypeUnsupported",
  // An SVG that is not well-formed XML (t_20af3915). Its own sentence because
  // the page's generic "Upload failed" hides the one thing the merchant has to
  // do: fix the file. The refusal is not about the TYPE (an SVG is supported)
  // and not about the quota, so neither existing row can say it.
  "upload.svgInvalid": "apiErr.uploadSvgInvalid",

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
 * A per-SURFACE re-mapping of one or more codes, passed into `apiErr`.
 *
 * WHY THIS EXISTS (card t_adf8e1e6). The dictionary above is keyed on the CODE
 * alone, which implicitly assumes a code means the same thing wherever it is
 * read. One code breaks that assumption by DESIGN: `/v1/auth/login` answers
 * `401 login.invalid` for a bad password AND for a bad 2FA code, because
 * distinguishing them would tell an attacker which half of the credential pair
 * is wrong (`failLogin` in api/internal/httpapi/auth.go). That is the right
 * wire contract and it must not change.
 *
 * The CONSEQUENCE was a wrong sentence on the second screen: the user passed the
 * password check, the page moved to the code step, and a wrong/expired code came
 * back as "Email or password is incorrect." — naming a credential that had just
 * been proven correct and saying nothing about the code. The step knows which
 * credential it just sent; the wire deliberately does not. So the surface
 * supplies its own sentence for the codes whose meaning depends on the step,
 * and every other code keeps the dictionary's answer.
 *
 * Absent, unknown or uncoded → `apiErrorKey`, i.e. exactly the pre-override
 * behaviour, so a caller that passes nothing is unaffected.
 */
export type SurfaceErrorKeys = Readonly<Record<string, Key>>;

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
 *
 * `surface` is the optional per-step override documented on `SurfaceErrorKeys`
 * (card t_adf8e1e6). It is checked FIRST, because a surface that names a code is
 * saying something the code alone cannot.
 */
export function apiErr(
  body: unknown,
  fallback: string,
  t: (k: Key) => string,
  surface?: SurfaceErrorKeys,
): string {
  const code = apiErrorCode(body);
  const key = (code === "" ? undefined : surface?.[code]) ?? apiErrorKey(body);
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
 * The codes that mean "the workspace you asked for is not one you have" — a
 * TENANCY refusal, not an authentication failure.
 *
 * WHY THIS LIST EXISTS (card t_da4b2031). The server answers a `?workspace=`
 * slug the session cannot resolve with **403** and the code below
 * (api/internal/httpapi/rest_scope.go's scopeOr403 →
 * "workspace not found"). web/src/lib/wsfetch.ts reads 401/403 as "the
 * session is no longer valid" and navigates to /login, so a slug sitting in
 * `localStorage` from a workspace that was since renamed or deleted could log a
 * perfectly signed-in merchant out. Measured on prod (v1.17.0): `/chat` with
 * `jf_workspace = ws-gone` ended on `https://jomform.com/login`.
 *
 * The server ALREADY distinguishes the two on the wire — a foreign slug carries
 * `workspace.notFound` while a dead session is a 401 with no tenancy code — so
 * the client reads the code rather than splitting the status (a 403 → 404 change
 * would be a public wire-contract change for every API-key, curl and MCP caller,
 * for a condition the machine code already names).
 *
 * The code itself is pinned cross-language by src/lib/wsfetch.test.ts, which
 * reads `CodeWorkspaceNotFound` out of api/internal/httpapi/errcode.go — so a
 * rename on the server fails the web suite instead of silently restoring the
 * sign-out.
 */
export const SCOPE_REFUSAL_CODES: ReadonlySet<string> = new Set(["workspace.notFound"]);

/**
 * True when the body carries a TENANCY refusal (see SCOPE_REFUSAL_CODES) rather
 * than an authentication failure. `false` for an absent or unknown code, so an
 * uncoded 403 is still treated as a dead session — the conservative direction.
 */
export function isScopeRefusal(body: unknown): boolean {
  return SCOPE_REFUSAL_CODES.has(apiErrorCode(body));
}

/**
 * The `?workspace=` slug a scope-refusal BODY names, or "" when it names none.
 *
 * WHY THE BODY HAS TO CARRY THIS (card t_1cfb4044). A scope refusal is only ever
 * a fact about ONE slug: the one that request was sent with. The client that
 * reads the refusal must therefore know WHICH slug was refused, or it can only
 * act on the assumption that it is the slug it is holding now — and that
 * assumption is false for every refusal that lands late, after the session has
 * already reconciled the scope (see the stale-slug race documented on
 * `dropWorkspaceScopeIfCurrent` in lib/workspace.ts). An unroutable "the scope is
 * wrong" is what let a 403 for a slug that had already been replaced wipe the
 * replacement.
 *
 * The API does not return the slug in the response today (`scopeOr403` writes
 * the prose and the code only), so this reads `workspace` when a caller has it
 * and returns "" otherwise. "" means "unattributed", and the caller treats that
 * as the conservative case rather than as "no slug" — never as a licence to act.
 * Reading the field here means the day the API starts naming the slug, the
 * client needs no change.
 */
export function workspaceScopeOf(body: unknown): string {
  const ws = (body as { workspace?: unknown } | null | undefined)?.workspace;
  return typeof ws === "string" ? ws : "";
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
