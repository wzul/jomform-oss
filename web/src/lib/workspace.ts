// Shared workspace-scope helper (issue #158, renamed from space per #431).
// The active workspace is persisted in localStorage under "jf_workspace" by
// the Shell's WorkspaceSwitcher. Every dashboard fetch must append
// ?workspace=<slug> so the backend resolves the account from the selected
// workspace (each workspace owns its own account) — never the user's first
// account. Empty string = no workspace selected yet.
//
// Issue #450: migrate the legacy "jf_space" key (pre-#431 rename) so a user
// who logged in before the rename keeps their active workspace instead of
// falling back to an empty scope (which produced app.jomform.com URLs).
function migrateLegacyScope() {
  try {
    const legacy = localStorage.getItem("jf_space");
    if (legacy && !localStorage.getItem("jf_workspace")) {
      localStorage.setItem("jf_workspace", legacy);
    }
    if (legacy) localStorage.removeItem("jf_space");
  } catch {
    // storage unavailable — ignore
  }
}

export function workspaceScope(): string {
  if (typeof window === "undefined") return "";
  try {
    migrateLegacyScope();
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
