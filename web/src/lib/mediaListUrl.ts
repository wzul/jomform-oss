import { workspaceUrl } from "./workspace.ts";

// The media library's live-list URL (card t_e19368af).
//
// THE DEFECT THIS EXISTS TO PREVENT. /media built its request as
//
//     `/api/rest/media?${params.toString()}${workspaceQuery()}`
//
// and `workspaceQuery()` opens with `?` — it is designed for a bare path, e.g.
// `/api/rest/forms${workspaceQuery()}`. With the switchable scope `abc` the page
// therefore requested
//
//     /api/rest/media?page=1&limit=24?workspace=abc
//
// Go reads everything after the second `?` as part of the `limit` value, so the
// parameter name became `24?workspace`: `workspace` was ABSENT and the API fell
// back to the caller's FIRST account. Measured on prod: the grid rendered the
// first account's library while the switcher said `abc` — the one page whose
// whole purpose is "the library of the workspace you are in" silently served a
// different workspace, with no error and nothing looking wrong.
//
// So the composition lives HERE, as a value a test can drive: `workspaceUrl` is
// the shared `&`-join helper (lib/workspace.ts), and mediaListUrl.test.ts asserts
// exactly one `?` plus `workspace` parsing as its OWN parameter — a guard that
// fails on the `${params}${workspaceQuery()}` shape this replaced.
export function mediaListUrl(params: URLSearchParams): string {
  return workspaceUrl(`/api/rest/media?${params.toString()}`);
}
