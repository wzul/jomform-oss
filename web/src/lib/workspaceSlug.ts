// The reserved-workspace-name refusal, rendered from the DASHBOARD's dictionary.
//
// WHY THIS MODULE EXISTS (issue #694's rule, applied to the workspace slug field)
//
// The Go validator refuses a reserved name with a WHY sentence (see
// api/internal/urlguard/subdomains.go), and that sentence is English because it
// is also the wire message for API-key, curl and MCP callers who cannot look up
// an i18n key. A Malay or Arabic dashboard must not print it: workspace
// creation used to map any 400 to t("workspace.slugInvalid") — "Slug must be
// 3-40 chars…" — which is ALSO wrong here, because a reserved name is well-formed
// and the merchant needs to be told the name is JomForm's, not that their typing
// was bad.
//
// So the API sends a stable token (`reserved_subdomain`, mirrored in Go as
// urlguard.ReservedSubdomainCode) and this module turns it into the localised
// sentence. The prose is never read off the response body.
//
// This repo has no JSX test runner (see src/lib/publishResult.ts), so the
// decision lives here and src/lib/workspaceSlug.test.ts tests it on the real
// functions the switcher calls.

/** Mirrors `urlguard.ReservedSubdomainCode` in the Go API. */
export const RESERVED_SUBDOMAIN_CODE = "reserved_subdomain";

/** i18n keys this module needs. Kept here so the key names are spelled once. */
export const RESERVED_SUBDOMAIN_KEY = "workspace.slugReserved";

/** The subset of an error response body this module reads. */
export type ErrorBody = { code?: unknown; error?: unknown };

/**
 * isReservedSlugRefusal reports whether a failed workspace-create response is a
 * reserved-name refusal (as opposed to a taken slug, a malformed slug, or any
 * other 400).
 *
 * Matching is on the TOKEN only. Falling back to a substring of the prose would
 * re-introduce exactly what #694 removed: a dashboard whose behaviour depends on
 * English server text.
 */
export function isReservedSlugRefusal(body: unknown): boolean {
  if (!body || typeof body !== "object") return false;
  return (body as ErrorBody).code === RESERVED_SUBDOMAIN_CODE;
}
