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
// "server menolak: %s"). A read_only workspace member confirming a prepared
// change hit exactly that: the request carried ?workspace=<their workspace>, and
// the page told them "Choose a workspace first — the assistant answers for one
// workspace at a time." The advice was unusable (they already had one selected)
// and the reason they needed was the role.
//
// THE RULE. The server now gives the scope refusal its own code
// (CodeWorkspaceScopeRequired in api/internal/httpapi/errcode.go, written by
// rest_scope.go's scopeAmbiguous arm). This module is the ONE place that turns a
// (status, body) pair into the card's outcome:
//
//   workspace.scopeRequired -> "scopeRequired"  the scope copy, and the card
//                                               must NOT claim a failure: the
//                                               server refuses this BEFORE it
//                                               reads the token, so nothing was
//                                               attempted and the token is live
//                                               (PR 779's rule, t_790b1016).
//   any other 400           -> "failed"         a failure of the change, like any
//                                               other non-2xx. The card says so,
//                                               which is accurate here: for the
//                                               post-claim refusal the token IS
//                                               spent, so the change really did
//                                               not happen.
//   409                     -> "stale"          spent or expired token
//   429                     -> "rateLimited"    the confirmation bucket
//   2xx                     -> "done"           confirmed (or cancelled)
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

/** The card outcome for one decision response. */
export type ConfirmDecision =
  // A missing/unusable workspace scope. The card says what it NEEDS.
  | "scopeRequired"
  // The token was already spent or has expired — ask for a fresh card.
  | "stale"
  // The per-user confirmation bucket refused it.
  | "rateLimited"
  // The change itself did not go through.
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
    // THE branch on the code. A 400 that names a different refusal is a failure of
    // the change, NOT a workspace-scope problem — and it must never be reported as
    // one.
    return apiErrorCode(body) === WORKSPACE_SCOPE_REQUIRED_CODE ? "scopeRequired" : "failed";
  }
  if (status === 409) return "stale";
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
