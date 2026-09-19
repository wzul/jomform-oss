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

// WorkspaceRef is the minimal shape needed to resolve the ACTIVE workspace: the
// bundle's WorkspaceRow satisfies it, and so does any list of {id, slug}.
export type WorkspaceRef = { id?: string; slug?: string };

// activeWorkspaceId resolves the id of the workspace the caller currently has
// selected, from the workspace list the session bundle carries. Returns "" when
// there is nothing to resolve (no session yet, or an empty list).
//
// WHY THIS EXISTS (issue #447): several dashboard settings are per WORKSPACE,
// but their REST endpoints are keyed by workspace ID while every list fetch is
// scoped by SLUG (?workspace=<slug>). A page that only carries the slug
// therefore cannot read or write that workspace's settings at all — which is
// why the /sales sound toggle never reached the API. The resolution rule is
// shared (rather than hand-rolled per page) because it is the same rule the
// settings page uses for its own per-workspace writes, and it must never pick a
// different workspace: the stored slug wins, and only when there is no match
// (or no selection yet) does it fall back to the first workspace, which is the
// workspace the Shell selects on login.
export function activeWorkspaceId(workspaces: WorkspaceRef[]): string {
  const ws = Array.isArray(workspaces) ? workspaces : [];
  if (ws.length === 0) return "";
  const slug = workspaceScope();
  const active = ws.find((w) => w.slug === slug) || ws[0];
  return active?.id || "";
}

// The GET/PUT body type of the per-workspace realtime sound setting (issue
// #447). `sound_enabled` is the ONE wire field; the server stores it on
// workspace_settings and reports it from the same settings read the settings
// page uses.
export type SoundSettings = { sound_enabled?: boolean };

// soundEnabledFromSettings reads the stored value. The server's contract is
// "muted unless explicitly enabled": the column is NOT NULL DEFAULT FALSE, so a
// missing field means OFF, and only a literal true turns the beep on. Reading
// `!!d.sound_enabled` would be equivalent for a well-formed payload but would
// also treat a string "false" as ON, so the comparison is explicit.
export function soundEnabledFromSettings(settings: SoundSettings | null | undefined): boolean {
  return settings?.sound_enabled === true;
}

// soundToggleBody is the write payload for the toggle: EXACTLY the one field, so
// the request can never carry a stale copy of another workspace setting.
// saveWorkspaceSettings keeps every omitted field at its stored value, so a
// single-field body is safe by construction — whereas posting a whole settings
// object built from page state would clobber the fields that page does not own
// (the same hazard AGENTS.md calls out for spam_judge_enabled).
export function soundToggleBody(next: boolean): SoundSettings {
  return { sound_enabled: next };
}
