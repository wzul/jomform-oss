// Shared workspace-scope helper (issue #158, renamed from space per #431).
// The active workspace is persisted in localStorage under "jf_workspace" by
// the Shell's WorkspaceSwitcher. Every dashboard fetch must append
// ?workspace=<slug> so the backend resolves the account from the selected
// workspace (each workspace owns its own account) — never the user's first
// account. Empty string = no workspace selected yet.
export function workspaceScope(): string {
  if (typeof window === "undefined") return "";
  try {
    return localStorage.getItem("jf_workspace") || "";
  } catch {
    return "";
  }
}

// workspaceQuery returns the "?workspace=<slug>" query string (or "") to
// append to a dashboard fetch URL.
export function workspaceQuery(): string {
  const sp = workspaceScope();
  return sp ? `?workspace=${encodeURIComponent(sp)}` : "";
}
