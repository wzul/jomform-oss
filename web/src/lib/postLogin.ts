// Issue #164: post-login workspace resolution. After a successful login
// (password, passkey, magic link), decide where to send the user:
//   - default workspace set → sync jf_workspace to that workspace's slug, go to "/"
//   - >1 workspace, no default → go to "/workspace-picker" (prompt which one)
//   - single workspace      → sync jf_workspace to it, go to "/"
//
// `next` is the validated ?next redirect (OAuth authorize) — when present we
// honour it directly (the OAuth flow already scopes to a workspace) and skip
// the picker entirely.
//
// Issue #475: renamed from /space-picker; the old route now redirects here.
export async function resolvePostLogin(next: string): Promise<string> {
  if (next) return next;

  let workspaces: { id: string; slug: string }[] = [];
  let defaultWorkspaceId = "";
  try {
    const [wsRes, meRes] = await Promise.all([
      fetch("/api/workspaces"),
      fetch("/api/auth/me"),
    ]);
    if (wsRes.ok) {
      const d = await wsRes.json();
      if (Array.isArray(d)) workspaces = d;
    }
    if (meRes.ok) {
      const me = await meRes.json();
      defaultWorkspaceId = me.default_workspace_id || "";
    }
  } catch {
    // Network hiccup — fall through to the dashboard; the Shell will pick a
    // workspace from localStorage as before.
    return "/";
  }

  if (workspaces.length === 0) return "/";

  // Default set → go straight to that workspace.
  if (defaultWorkspaceId) {
    const def = workspaces.find((s) => s.id === defaultWorkspaceId);
    if (def) {
      try {
        localStorage.setItem("jf_workspace", def.slug);
      } catch {}
      return "/";
    }
  }

  // Single workspace → sync and go straight in (no prompt).
  if (workspaces.length === 1) {
    try {
      localStorage.setItem("jf_workspace", workspaces[0].slug);
    } catch {}
    return "/";
  }

  // Multi-workspace, no default → prompt.
  return "/workspace-picker";
}
