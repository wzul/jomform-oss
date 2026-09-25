"use client";

// Issue #164: multi-workspace login picker. Shown after login when the user has
// more than one workspace and no default workspace is set. Lists the user's
// workspaces; clicking one enters it (and optionally sets it as the default).
// Single-workspace users and users with a default never land here (see
// lib/postLogin.ts).
//
// Issue #475: the route was /space-picker before the Space→Workspace rename
// (#431); the old path now redirects here so bookmarked links keep working
// (see app/space-picker/page.tsx).
//
// Issue #503: this page used to render "…" FOREVER when /api/workspaces
// answered 401 — the response was swallowed by `if (wsRes.ok)` with no `else`,
// the page had no loading flag (it used `workspaces.length === 0` to mean
// "still loading"), and so an expired session left a terminal placeholder with
// no error and no way forward. The load now goes through the shared
// lib/wsfetch decision helper: 401/403 redirect to /login, any other failure
// shows a real message plus a Retry action, the loading state is a genuine
// flag, and a truly empty list gets its own message.
import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { useI18n } from "../../components/i18n";
import type { WorkspaceRow } from "../../components/session";
import { fetchJson, runLoad } from "../../lib/wsfetch";
import { createWorkspace, enterWorkspace } from "../../lib/workspace";
import { translateApiErr } from "../../lib/apiErr";

// The subset of /api/auth/me this page needs.
type Me = { default_workspace_id?: string };

export default function WorkspacePickerPage() {
  const { t } = useI18n();
  const router = useRouter();
  const [workspaces, setWorkspaces] = useState<WorkspaceRow[]>([]);
  const [defaultId, setDefaultId] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // Issue #503: a REAL loading flag. Never infer "loading" from an empty list —
  // that is what made empty, failed and loading indistinguishable.
  const [checking, setChecking] = useState(true);
  // Ask-first onboarding: with zero workspaces this page IS the first-run
  // prompt, so it carries the create form (name + slug) instead of the bare
  // "You don't have any workspaces yet." + Retry that used to be a dead end.
  const [creating, setCreating] = useState(false);
  const [newName, setNewName] = useState("");
  const [newSlug, setNewSlug] = useState("");

  // Issue #163: short role label for the picker rows.
  function roleLabel(role?: string): string {
    if (!role) return "";
    if (role === "owner") return t("role.owner");
    return t(`role.${role}` as any) || role;
  }

  // 401/403 → the session is gone; bounce to /login. Mirrors the existing
  // pattern in components/Shell.tsx.signOut() (router.replace + a hard
  // window.location.href so the stale client state cannot survive).
  const goToLogin = useCallback(() => {
    router.replace("/login");
    window.location.href = "/login";
  }, [router]);

  const load = useCallback(async () => {
    setChecking(true);
    setError("");
    // The workspaces list is the one that decides the page state. `me` only
    // supplies the default-workspace badge, so a failure there must never hide
    // a perfectly good list.
    const ws = await runLoad(
      async () => {
        const d = await fetchJson<unknown>("/api/workspaces");
        return Array.isArray(d) ? (d as WorkspaceRow[]) : [];
      },
      { onUnauth: goToLogin, message: t("workspace.loadFail"), errorKey: (b: unknown) => translateApiErr(b, t) },
    );
    if (!ws.ok) {
      // A failure is NEVER silent: the message is rendered, and the list is
      // cleared so a failed load can never look like an empty one.
      setWorkspaces([]);
      setError(ws.error);
      setChecking(false);
      return;
    }
    setWorkspaces(ws.data);

    const me = await runLoad(
      () => fetchJson<Me>("/api/auth/me"),
      { onUnauth: goToLogin, message: t("workspace.loadFail"), errorKey: (b: unknown) => translateApiErr(b, t) },
    );
    if (me.ok) setDefaultId(me.data.default_workspace_id || "");

    setChecking(false);
  }, [t, goToLogin]);

  useEffect(() => {
    void load();
  }, [load]);

  function enter(slug: string) {
    enterWorkspace(slug);
    router.replace("/");
  }

  // askFirstCreate is the first-run action: create the workspace, enter it, and
  // land on the dashboard inside it. No reload: the session bundle is
  // workspace-keyed (session.tsx) and enterWorkspace dispatches the change, so
  // the dashboard re-fetches for the new workspace. A user who reaches this page
  // with zero workspaces is expected to take exactly this path before any create
  // surface is reachable.
  async function askFirstCreate(e: React.FormEvent) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    const r = await createWorkspace({ name: newName, slug: newSlug });
    if (!r.ok) {
      // The create-failed copy is a rendered i18n string from the shared
      // dictionary — never the server's own text (the API answers in Malay
      // prose, which is the separate i18n defect).
      setError(t(r.errorKey));
      setBusy(false);
      return;
    }
    enterWorkspace(r.slug);
    setCreating(false);
    setBusy(false);
    router.replace("/");
  }

  async function setDefault(workspaceId: string) {
    setBusy(true);
    setError("");
    const r = await runLoad(
      () => fetchJson<unknown>("/api/auth/profile/default-workspace", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ workspace_id: workspaceId }),
      }),
      { onUnauth: goToLogin, message: t("workspace.setDefaultFail"), errorKey: (b: unknown) => translateApiErr(b, t) },
    );
    if (!r.ok) {
      setError(r.error);
      setBusy(false);
      return;
    }
    setDefaultId(workspaceId);
    setBusy(false);
  }

  return (
    <main className="relative flex min-h-screen items-center justify-center overflow-hidden bg-[var(--jf-bg)] px-6">
      <div
        aria-hidden
        className="pointer-events-none absolute left-1/2 top-[-260px] h-[420px] w-[720px] -translate-x-1/2 rounded-full opacity-40 blur-3xl"
        style={{
          background:
            "radial-gradient(closest-side, rgba(94,106,210,0.35), rgba(113,112,255,0.12), transparent)",
        }}
      />
      <div className="relative w-full max-w-md">
        <div className="mb-8 text-center">
          <div className="mx-auto mb-4 flex h-11 w-11 items-center justify-center rounded-xl bg-[#5e6ad2] text-lg font-bold text-white shadow-[0_0_24px_rgba(94,106,210,0.45)]">
            JF
          </div>
          <h1 className="text-xl font-semibold text-[var(--jf-text)]" style={{ letterSpacing: "-0.02em" }}>
            {!checking && !error && workspaces.length === 0
              ? t("workspace.askPageTitle")
              : t("workspace.pickerTitle")}
          </h1>
          <p className="mt-1.5 text-sm text-[var(--jf-muted)]">
            {!checking && !error && workspaces.length === 0
              ? t("workspace.askPageSubtitle")
              : t("workspace.pickerSubtitle")}
          </p>
        </div>

        <div className="rounded-xl border border-[var(--jf-border)] bg-white p-6 shadow-[0_0_0_1px_rgba(0,0,0,0.2),0_8px_24px_rgba(0,0,0,0.4)]">
          {/* Issue #503: the four states are now distinct and explicit —
              loading / error (+ retry) / empty / list. */}
          {checking ? (
            <p role="status" aria-live="polite" className="py-6 text-center text-sm text-[var(--jf-muted)]">
              {t("workspace.loading")}
            </p>
          ) : error ? (
            <div role="alert" className="flex flex-col items-center gap-3 py-4 text-center">
              <p className="rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2 text-[13px] text-red-700">
                {error}
              </p>
              <button
                type="button"
                onClick={() => void load()}
                className="rounded-md border border-[var(--jf-border)] px-3 py-1.5 text-xs font-medium text-[var(--jf-text)] transition-colors hover:bg-slate-50"
              >
                {t("common.retry")}
              </button>
            </div>
          ) : workspaces.length === 0 ? (
            /* Ask-first onboarding (Wan's decision, 2026-09-20): a user with no
               workspace at all is ASKED to create one, and this is the prompt.
               It replaces a dead end that rendered
               t("workspace.empty") + a Retry button and nothing else — the only
               create affordance was the hidden "+ New workspace" inside the
               topbar switcher, which is why nobody found it. The flow finishes
               by entering the new workspace, so the user can immediately create
               a form with no error. */
            <form onSubmit={askFirstCreate} className="space-y-3">
              <p className="text-sm font-medium text-[var(--jf-text)]">
                {t("workspace.askTitle")}
              </p>
              <p className="text-[13px] leading-relaxed text-[var(--jf-muted)]">
                {t("workspace.askBody")}
              </p>
              <div>
                <label htmlFor="ask-ws-name" className="mb-1 block text-xs font-medium text-[var(--jf-muted)]">
                  {t("workspace.name")}
                </label>
                <input
                  id="ask-ws-name"
                  value={newName}
                  onChange={(e) => setNewName(e.target.value)}
                  required
                  autoFocus
                  disabled={busy}
                  className="min-h-11 w-full rounded-md border border-[var(--jf-border)] bg-white px-3 text-sm text-[var(--jf-text)] outline-none focus:border-[#7170ff]/60 focus:bg-indigo-50 disabled:opacity-50"
                />
              </div>
              <div>
                <label htmlFor="ask-ws-slug" className="mb-1 block text-xs font-medium text-[var(--jf-muted)]">
                  {t("workspace.slug")}
                </label>
                <input
                  id="ask-ws-slug"
                  value={newSlug}
                  onChange={(e) => setNewSlug(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ""))}
                  required
                  disabled={busy}
                  className="min-h-11 w-full rounded-md border border-[var(--jf-border)] bg-white px-3 text-sm text-[var(--jf-text)] outline-none focus:border-[#7170ff]/60 focus:bg-indigo-50 disabled:opacity-50"
                />
                {newSlug && (
                  <p className="mt-1 text-[11px] text-[var(--jf-muted)]">
                    {t("workspace.askHost", { slug: newSlug })}
                  </p>
                )}
              </div>
              {error && (
                <p role="alert" className="rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2 text-[13px] text-red-700">
                  {error}
                </p>
              )}
              <button
                type="submit"
                disabled={busy}
                className="min-h-11 w-full rounded-md bg-[#5e6ad2] px-3 py-2 text-sm font-medium text-white transition-colors hover:bg-[#828fff] disabled:opacity-50"
              >
                {busy ? "…" : t("workspace.create")}
              </button>
            </form>
          ) : (
            <ul className="space-y-2">
              {workspaces.map((s) => {
                const isDefault = s.id === defaultId;
                return (
                  <li
                    key={s.id}
                    className="flex flex-col gap-3 rounded-lg border border-[var(--jf-border)] px-4 py-3 sm:flex-row sm:items-center sm:justify-between"
                  >
                    <div className="min-w-0">
                      <p className="text-sm font-medium text-[var(--jf-text)]">
                        {s.name}
                        {isDefault && (
                          <span className="ml-2 rounded-full bg-indigo-50 px-2 py-0.5 text-[10px] font-medium text-[var(--jf-primary-on-tint)]">
                            {t("workspace.defaultLabel")}
                          </span>
                        )}
                        {s.role && (
                          <span className="ml-2 rounded-full bg-slate-100 px-2 py-0.5 text-[10px] font-medium text-[var(--jf-muted-on-tint)]">
                            {roleLabel(s.role)}
                          </span>
                        )}
                      </p>
                      <p className="truncate text-[11px] text-[var(--jf-muted)]">{s.slug}.jomform.com</p>
                      {!isDefault && (
                        <button
                          type="button"
                          disabled={busy}
                          onClick={() => setDefault(s.id)}
                          title={t("workspace.setDefault")}
                          className="mt-1.5 text-[11px] text-[var(--jf-muted)] underline decoration-dotted underline-offset-2 hover:text-[var(--jf-primary)] disabled:opacity-50"
                        >
                          {t("workspace.setDefault")}
                        </button>
                      )}
                    </div>
                    <div className="flex shrink-0 items-center gap-2 sm:justify-end">
                      <button
                        type="button"
                        onClick={() => enter(s.slug)}
                        className="rounded-md bg-[#5e6ad2] px-3 py-1.5 text-xs font-medium text-white transition-colors hover:bg-[#828fff]"
                      >
                        {t("workspace.enter")}
                      </button>
                    </div>
                  </li>
                );
              })}
            </ul>
          )}
        </div>

        {/* Issue #503: a genuinely empty list is still a dead end without an
            action — offer a re-check, plus the inline "?" docs link AGENTS.md
            documentation parity asks for next to the feature. */}
        {!checking && !error && workspaces.length === 0 && (
          <div className="mt-4 text-center">
            <button
              type="button"
              onClick={() => void load()}
              className="text-xs text-[var(--jf-muted)] underline decoration-dotted underline-offset-2 hover:text-[var(--jf-primary)]"
            >
              {t("common.retry")}
            </button>
            <a
              href="/docs#workspaces"
              className="ml-3 text-xs text-[var(--jf-muted)] underline decoration-dotted underline-offset-2 hover:text-[var(--jf-primary)]"
            >
              {t("workspace.docs")}
            </a>
          </div>
        )}
      </div>
    </main>
  );
}
