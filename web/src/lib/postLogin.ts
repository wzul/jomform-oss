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
// The explicit ".ts" extension is required by the Node test runner (which runs
// this module's regression test directly; see postLogin.test.ts).
import { fetchJson, runLoad } from "./wsfetch.ts";
import { clearUnknownWorkspaceScope, enterWorkspace } from "./workspace.ts";

export async function resolvePostLogin(next: string): Promise<string> {
  if (next) return next;

  let workspaces: { id: string; slug: string }[] = [];
  let defaultWorkspaceId = "";

  // Issue #503: both responses used to be read as `if (res.ok)` with no `else`,
  // so an expired session (401) resolved as "no workspaces" and the user was
  // bounced to "/" as if they had none. Now the decision is explicit: an
  // unauthorised response goes to /login, and a transient failure still falls
  // back to the dashboard (the Shell picks a workspace from localStorage).
  // `me` only supplies the default-workspace id, so its failure stays
  // non-fatal — it is fired alongside the list fetch to keep the flow parallel.
  const mePromise = fetchJson<{ default_workspace_id?: string }>("/api/auth/me").catch(() => null);

  const ws = await runLoad(
    async () => {
      const d = await fetchJson<unknown>("/api/workspaces");
      return Array.isArray(d) ? (d as { id: string; slug: string }[]) : [];
    },
    { onUnauth: () => {}, message: "" },
  );
  if (!ws.ok) {
    if (ws.kind === "unauth") return "/login";
    // Network hiccup — fall through to the dashboard; the Shell will pick a
    // workspace from localStorage as before.
    return "/";
  }
  workspaces = ws.data;

  // Reconcile the stored scope against the list the server just returned, so a
  // slug from a PREVIOUS user on this browser (or a workspace since removed) can
  // never be sent as ?workspace= by the first page load after login. An empty
  // list is a real answer and clears the stale slug; a failed fetch never
  // reaches here (it returned above).
  clearUnknownWorkspaceScope(workspaces);

  const me = await mePromise;
  if (me) defaultWorkspaceId = me.default_workspace_id || "";

  // A user with NO workspace is ASKED to create one before they can reach any
  // create surface (Wan's spec, 2026-09-20: "klu utk kes lain (yg baru daftar),
  // lepas login tu terus kena create workspace"). This used to return "/",
  // which dropped a workspace-less user on the dashboard with no workspace and
  // no prompt — the first thing they tried (create a form) then 403'd.
  // /workspace-picker carries the first-run state: the create form lives there.
  if (workspaces.length === 0) return "/workspace-picker";

  // Default set → go straight to that workspace.
  if (defaultWorkspaceId) {
    const def = workspaces.find((s) => s.id === defaultWorkspaceId);
    if (def) {
      // `enterWorkspace`, not a raw setItem: this resolver runs BEFORE the first
      // dashboard page mounts, and it must publish the session-confirmed scope
      // every scoped fetch reads (t_da4b2031), not just the stored key.
      enterWorkspace(def.slug);
      return "/";
    }
  }

  // Single workspace → sync and go straight in (no prompt).
  if (workspaces.length === 1) {
    enterWorkspace(workspaces[0].slug);
    return "/";
  }

  // Multi-workspace, no default → prompt.
  return "/workspace-picker";
}
