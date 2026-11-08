// What the confirm/cancel route's answer MEANS, decided by the API's machine
// code and not by the bare HTTP status.
//
// THE DEFECT (t_fb393abe, found by the QA lane while verifying PR 779).
// `decide()` in src/app/chat/page.tsx branched on the status alone:
//
//     if (r.status === 400) { setState(..., "scopeRequired"); return; }
//
// and returned BEFORE reading the body, so it could not tell one 400 from
// another. A 400 on this route is not necessarily a workspace-scope refusal:
// api/internal/httpapi/rest_chat.go's chatConfirmStatusFor maps everything it
// does not recognise to 400, and one reachable class happens AFTER the token is
// claimed — a tool the server refuses to execute (chat/proposal.go's
// "server refused: %s"). A read_only workspace member confirming a prepared
// change hit exactly that: the request carried ?workspace=<their workspace>, and
// the page told them "Choose a workspace first — the assistant answers for one
// workspace at a time." The advice was unusable (they already had one selected)
// and the reason they needed was the role.
//
// THE RULE. The server now gives the scope refusal its own code
// (CodeWorkspaceScopeRequired in api/internal/httpapi/errcode.go, written by
// rest_scope.go's scopeAmbiguous arm), and every other refusal this route can
// classify names itself the same way. This module is the ONE place that turns a
// (status, body) pair into the card's outcome. The list below is the WHOLE
// contract — it is what `confirmDecisionFor` implements, so
// change one and change the other:
//
//   400 workspace.scopeRequired  -> "scopeRequired"     the scope copy, and the card
//                                                       must NOT claim a failure: the
//                                                       server refuses this BEFORE it
//                                                       reads the token, so nothing was
//                                                       attempted and the token is live
//                                                       (PR 779's rule, t_790b1016).
//   400 role.manageRequired      -> "roleDenied"        the write RAN and the workspace
//                                                       role gate refused it, so the
//                                                       token is spent: the card says the
//                                                       change did not go through, names
//                                                       the cause, and must not offer the
//                                                       retry the generic failure copy
//                                                       does (t_1900adef).
//   400 any other machine code   -> "refused"           the server answered and
//                                                       CLASSIFIED the refusal, so the
//                                                       change did not run and the token
//                                                       is spent: the copy says exactly
//                                                       that and offers no retry. This is
//                                                       the route's `default:` arm writing
//                                                       `request.badRequest` for a tool it
//                                                       refused after claiming the token
//                                                       (t_bf4feea2).
//   400 no machine code at all   -> "failed"            nothing on the wire proves the
//                                                       server decided anything (a proxy
//                                                       error page, an empty body, a
//                                                       truncated response), so the token
//                                                       may still be live: this is the one
//                                                       outcome whose copy may suggest
//                                                       another attempt.
//   409 chat.proposalArgsInvalid -> "unexecutable"      the tool would never accept this
//                                                       card's arguments. Refused BEFORE
//                                                       the token is claimed, so the token
//                                                       is still live — but the same card
//                                                       can never work, so the remedy is a
//                                                       fresh one (t_623676d1).
//   409 workspace.required       -> "workspaceRequired" the account has no workspace at
//                                                       all. The token is spent, and the
//                                                       remedy is two steps in that order:
//                                                       create a workspace, then ask for a
//                                                       fresh card (t_bf4feea2).
//   409 anything else            -> "stale"             spent or expired token. No code, an
//                                                       unknown code, and `request.conflict`
//                                                       all land here.
//   403 workspace.notFound       -> "scopeRequired"     the `?workspace=` slug no longer
//                                                       resolves for this session (a stale
//                                                       localStorage value), not a dead
//                                                       session — so the user is told what
//                                                       to do instead of being signed out
//                                                       (t_da4b2031).
//   403 anything else            -> "failed"            a genuine authorization refusal;
//                                                       an absent or unknown code is never
//                                                       softened into "choose a workspace".
//   429 any body                 -> "rateLimited"       the confirmation bucket.
//   2xx any body                 -> "done"              confirmed (or cancelled).
//   any other status             -> "failed"            a 5xx, or a status this module
//                                                       does not know.
//
// WHY A CODE AND NOT THE PROSE. The wire `error` string is Malay and the API does
// not know the dashboard's language on a JSON call (issue #6177) — it is the
// message for API-key/curl/MCP callers and is never what the dashboard prints. So
// the branch reads `code`, and an unknown/absent code falls back to the page's own
// localized sentence. A response with no readable body at all (a proxy error page)
// therefore lands on "failed", never on the scope copy.
//
// WHY THIS IS A LIB AND NOT AN INLINE BRANCH. This repo has no React/DOM unit
// runner (see src/lib/i18n.test.ts), so a decision that lives inside the component
// can only be pinned by scanning its source — which cannot show that the branch
// BEHAVES differently for the two 400s. Kept here, the decision is executed
// directly by src/lib/chatConfirm.test.ts, which is what makes the card's bite
// requirement provable: restoring the bare `status === 400` test turns a named
// case red instead of merely moving a string.

import { apiErrorCode, isScopeRefusal } from "./apiErr.ts";

/**
 * Mirrors `CodeWorkspaceScopeRequired` in api/internal/httpapi/errcode.go.
 *
 * Read by src/lib/chatConfirm.test.ts out of the Go source, so a rename on the
 * server fails the web suite rather than silently sending every refusal back down
 * the generic path (the same cross-language pin src/lib/workspaceSlug.ts uses for
 * the reserved-subdomain token).
 */
export const WORKSPACE_SCOPE_REQUIRED_CODE = "workspace.scopeRequired";

/**
 * Mirrors `CodeProposalArgsInvalid` in api/internal/httpapi/errcode.go (t_623676d1).
 *
 * A proposal whose arguments the target tool would never accept is refused BEFORE
 * the token is claimed, so the token is still live and the card can never succeed
 * as shown. It gets its own outcome rather than riding "stale": the remedy is the
 * same (ask for a fresh card) but the REASON is different — the card neither
 * expired nor was used, and its click was never even attempted. Told "expired or
 * already used", a user has no way to know their token is still good, and told
 * "try again" they would retry an action that is guaranteed to fail.
 *
 * Read by src/lib/chatConfirm.test.ts out of the Go source, so a rename on the
 * server fails the web suite (the same cross-language pin as the scope code).
 */
export const PROPOSAL_ARGS_INVALID_CODE = "chat.proposalArgsInvalid";

/**
 * Mirrors `CodeRoleManageRequired` in api/internal/httpapi/errcode.go (t_1900adef).
 *
 * The workspace ROLE refusal, on the confirm route. It is NOT a new code — the REST
 * edge has answered it for years — but until this card it could never reach a
 * confirm response: the message the write gate emits is one word longer than the
 * table's exact key, so `ErrCodeFor` returned "" and the 400 shipped as
 * `request.badRequest`. A read_only member whose confirmed write was refused by
 * their role therefore landed on the generic failure copy, which tells them to try
 * again on a token the server had already spent.
 *
 * Read by src/lib/chatConfirm.test.ts out of the Go source, so a rename on the
 * server fails the web suite rather than silently sending this refusal back down
 * the generic path (the same cross-language pin as the two codes above).
 */
export const ROLE_MANAGE_REQUIRED_CODE = "role.manageRequired";

/**
 * Mirrors `CodeWorkspaceRequired` in api/internal/httpapi/errcode.go (t_bf4feea2).
 *
 * The CONFIRM-side refusal for a card whose write the WORKSPACE gate declined
 * because the ACCOUNT has no workspace at all — `accounts.workspace_id` NULL, the
 * shape a magic-link signup leaves behind. It is not new: the form-CREATE door has
 * answered this state with 409 + this code for a long time. What was new is that
 * the chat lane could not produce it: the refusal arrives AFTER the token is
 * claimed, the lane could only wrap it as `server refused: …`, and the REST edge
 * mapped that to its `default:` arm — a 400 carrying `request.badRequest`, which
 * the page does not translate, so it printed "The change could not be applied.
 * Please try again." for a card that was already spent and for a cause it could
 * not name.
 *
 * Read by src/lib/chatConfirm.test.ts out of the Go source, so a rename on the
 * server fails the web suite rather than silently sending this refusal back to the
 * generic path.
 */
export const WORKSPACE_REQUIRED_CODE = "workspace.required";

/** The card outcome for one decision response. */
export type ConfirmDecision =
  // A missing/unusable workspace scope. The card says what it NEEDS.
  | "scopeRequired"
  // The token was already spent or has expired — ask for a fresh card.
  | "stale"
  // The card's own arguments are not ones the tool accepts: refused unspent, and
  // the same card can never work. Ask for a fresh card.
  | "unexecutable"
  // The per-user confirmation bucket refused it.
  | "rateLimited"
  // The confirmed write was refused by the WORKSPACE ROLE (manager/admin needed).
  // The token is SPENT — the refusal happens after the claim — so the card cannot
  // be reused and the copy must not offer a retry (t_1900adef).
  | "roleDenied"
  // The confirmed write was refused because the ACCOUNT has no workspace at all
  // (t_bf4feea2). The token is SPENT, the change did not run, and the remedy is
  // two steps in that order: create a workspace, then ask for a fresh card.
  | "workspaceRequired"
  // The server answered and refused, but this build cannot classify why: a 4xx
  // that carries a machine code it does not know. The change did not run and the
  // card's token is spent, so the copy may not offer a retry (t_bf4feea2).
  | "refused"
  // The failure could not be attributed to a server decision at all — no readable
  // body (a proxy error page), a 5xx, or a transport error. The token may still be
  // live, so this is the one outcome whose copy may suggest another attempt.
  | "failed"
  // Confirmed (or cancelled) — the route answers 200.
  | "done";

/**
 * confirmDecisionFor maps a confirm/cancel response onto the card outcome.
 *
 * 401/403 are deliberately NOT handled here: the page treats them as "the session
 * is gone" and redirects to /login before ever calling this (wsfetch.ts's rule),
 * and that navigation is a page concern rather than a property of this decision.
 */
export function confirmDecisionFor(status: number, body: unknown): ConfirmDecision {
  if (status === 429) return "rateLimited";
  if (status === 400) {
    // THE branch on the code. A 400 that names a different refusal is NOT a
    // workspace-scope problem — and it must never be reported as one.
    //
    // TWO codes are named on this status, and they are opposite conditions:
    // the scope refusal (nothing was attempted, the token is live) and the ROLE
    // refusal (the write ran and was refused, the token is spent). They share the
    // 400 deliberately — the status is a MISSING PARAMETER and a permission answer
    // respectively, and wsfetch.ts reads 401/403 as "the session is gone" — so the
    // code is the only thing that can tell them apart (t_1900adef).
    const code = apiErrorCode(body);
    if (code === WORKSPACE_SCOPE_REQUIRED_CODE) return "scopeRequired";
    if (code === ROLE_MANAGE_REQUIRED_CODE) return "roleDenied";
    // ANY OTHER CODE means the SERVER answered and classified the request: this
    // route's `default:` arm writes the class code (`request.badRequest`) for a
    // tool it refused AFTER claiming the token (t_bf4feea2). Nothing was applied,
    // the card cannot be used again, and the user needs a new one — so this is
    // "refused", whose copy says exactly that.
    //
    // NO CODE AT ALL is a different situation: a proxy error page, an empty body, a
    // truncated response. Nothing there proves the token was spent, so it stays
    // "failed" — the one outcome whose copy may suggest another attempt.
    return code ? "refused" : "failed";
  }
  if (status === 409) {
    // MORE THAN ONE 409, and the code separates them. The default is the
    // spent/expired token (unchanged behaviour); a card the tool would never accept
    // carries its own code and gets its own outcome, because its token is UNSPENT
    // and its remedy is a fresh card rather than "it expired" (t_623676d1); and a
    // card refused because the account has no workspace at all gets one that names
    // that cause and its two-step remedy (t_bf4feea2).
    const code = apiErrorCode(body);
    if (code === PROPOSAL_ARGS_INVALID_CODE) return "unexecutable";
    if (code === WORKSPACE_REQUIRED_CODE) return "workspaceRequired";
    return "stale";
  }
  // A 403 carrying the TENANCY refusal (t_da4b2031). The page's 401/403 branch
  // used to send this to /login: the request's `?workspace=` is a slug the
  // session cannot resolve (a workspace renamed or deleted while selected), which
  // is a stale localStorage value and not a dead session. Answered as the scope
  // outcome so the user is told what to do instead of being signed out.
  //
  // An UNCoded 403 stays a failure: `isScopeRefusal` is false for an absent or
  // unknown code, so a genuine authorization refusal is never softened into
  // "choose a workspace".
  if (status === 403) return isScopeRefusal(body) ? "scopeRequired" : "failed";
  return status >= 200 && status < 300 ? "done" : "failed";
}
