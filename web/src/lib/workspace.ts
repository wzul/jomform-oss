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

// ── Per-user cached scope: one sweep list, one reconciler ────────────────────
//
// THE REPORTED DEFECT
//   jf_workspace was never cleared anywhere (`grep removeItem("jf_workspace")
//   web/src` used to return zero hits) and Shell.signOut() only called
//   /api/auth/logout. So on a browser where anyone had ever selected a
//   workspace, the NEXT user's session started with the previous user's slug in
//   localStorage, and every scoped write sent it as ?workspace=<other tenant>:
//   the server correctly answered 403 "workspace tidak dijumpai" while the
//   forms list still answered 200 (that is resolveSessionAccount falling back to
//   the caller's own account for authed(), then resolveAccountScope rejecting
//   the foreign slug). A brand-new user's very first action — create a form —
//   failed with a message about a workspace they never had.
//
// THE TWO HALVES OF THE FIX, and why BOTH are needed
//   1. SIGN-OUT CLEARS THE SET. Every localStorage key the dashboard caches
//      about a signed-in user is listed in PER_USER_STORAGE_KEYS and dropped by
//      clearPerUserStorage(). Fixing only jf_workspace would leave its siblings
//      (the settings/profile tab keys) to be inherited the same way, so the
//      sweep is driven by the list — and a guard asserts the list covers every
//      jf_* key any localStorage call site names.
//   2. AN UNKNOWN SLUG IS NEVER SENT. Clearing on sign-out only covers a real
//      sign-out; a session that merely expired, or a key written by an older
//      build, still arrives with a foreign slug. clearUnknownWorkspaceScope()
//      is given the workspace list the server just returned and drops any
//      stored slug that is not in it, so ?workspace= can only ever carry a
//      workspace the caller actually has.
//
// These are the exact keys this dashboard caches per signed-in user. jf_lang is
// included deliberately: it is a per-visitor value that a signed-in user's
// PROFILE supersedes, so a value left by the previous user must not be the next
// user's first paint.
export const PER_USER_STORAGE_KEYS = [
  "jf_workspace",
  "jf_space",
  "jf_lang",
  "jf_profile_tab",
  "jf_settings_tab",
] as const;

// clearPerUserStorage drops every per-user cached key, so nothing about the
// previous user survives a sign-out on this browser.
export function clearPerUserStorage(): void {
  for (const k of PER_USER_STORAGE_KEYS) {
    try {
      localStorage.removeItem(k);
    } catch {
      // storage unavailable — nothing was cached either
    }
  }
}

// clearUnknownWorkspaceScope drops a stored slug that is not in `workspaces`.
//
// Callers pass the list the server returned. `undefined` means "no list was
// returned at all" (a failed fetch, or a payload that carries no list) and is a
// NO-OP on purpose: reconciling against a list we do not have would wipe a
// valid selection on a network blip, which is a worse bug than the one being
// fixed. An EMPTY list is a real answer — the caller has no workspace — and it
// clears the stale slug, which is the case the reported bug hit.
//
// Returns the stored slug that survives ("" when nothing is stored or the
// stored value was dropped).
export function clearUnknownWorkspaceScope(workspaces: WorkspaceRef[] | undefined): string {
  const ws = Array.isArray(workspaces) ? workspaces : undefined;
  if (!ws) return workspaceScope();
  const stored = workspaceScope();
  if (stored === "") return "";
  if (ws.some((w) => w.slug === stored)) return stored;
  try {
    localStorage.removeItem("jf_workspace");
  } catch {
    // storage unavailable — ignore
  }
  return "";
}

// createWorkspaceInput is the create-workspace body (POST /v1/workspaces).
export type CreateWorkspaceInput = { name: string; slug: string };

// CreateWorkspaceOutcome is a discriminated result: a failure carries the i18n
// KEY the caller must render, never a rendered string — the module has no
// locale, and two call sites (the first-run ask and the switcher) must not
// drift into two different messages for the same status.
export type CreateWorkspaceOutcome =
  | { ok: true; id: string; slug: string }
  | { ok: false; errorKey: "workspace.slugTaken" | "workspace.slugInvalid" | "workspace.createFail" };

// createWorkspace POSTs a new workspace. ONE implementation shared by the
// first-run ask (app/workspace-picker) and the topbar switcher: the status → key
// mapping is the part that must not be written twice, because 409 (slug taken)
// and 400 (slug invalid) have distinct, actionable copy.
//
// The slug is normalised to the server's rule (lowercase a-z0-9-dash) before it
// is sent, so a client that types "My Shop" gets the same 400-vs-success
// outcome the server would return for the normalised value rather than a
// rejection the user cannot act on.
export async function createWorkspace(input: CreateWorkspaceInput): Promise<CreateWorkspaceOutcome> {
  const slug = input.slug.trim().toLowerCase().replace(/[^a-z0-9-]/g, "");
  try {
    const res = await fetch("/api/workspaces", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: input.name.trim(), slug }),
    });
    if (!res.ok) {
      if (res.status === 409) return { ok: false, errorKey: "workspace.slugTaken" };
      if (res.status === 400) return { ok: false, errorKey: "workspace.slugInvalid" };
      return { ok: false, errorKey: "workspace.createFail" };
    }
    const d = (await res.json().catch(() => ({}))) as { id?: string; slug?: string };
    return { ok: true, id: d.id || "", slug: d.slug || slug };
  } catch {
    return { ok: false, errorKey: "workspace.createFail" };
  }
}

// enterWorkspace makes `slug` the active workspace (the same write Shell and the
// picker perform) and notifies the session context so the workspace-keyed bundle
// re-fetches for it.
export function enterWorkspace(slug: string): void {
  try {
    localStorage.setItem("jf_workspace", slug);
  } catch {
    // storage unavailable — the caller still navigates
  }
  if (typeof window !== "undefined") {
    window.dispatchEvent(new Event("jf:workspace-changed"));
  }
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
