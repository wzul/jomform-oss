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
import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { useI18n } from "../../components/i18n";
import type { WorkspaceRow } from "../../components/session";

export default function WorkspacePickerPage() {
  const { t } = useI18n();
  const router = useRouter();
  const [workspaces, setWorkspaces] = useState<WorkspaceRow[]>([]);
  const [defaultId, setDefaultId] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  // Issue #163: short role label for the picker rows.
  function roleLabel(role?: string): string {
    if (!role) return "";
    if (role === "owner") return t("role.owner");
    return t(`role.${role}` as any) || role;
  }

  useEffect(() => {
    (async () => {
      try {
        const [wsRes, meRes] = await Promise.all([
          fetch("/api/workspaces"),
          fetch("/api/auth/me"),
        ]);
        if (wsRes.ok) {
          const d = await wsRes.json();
          if (Array.isArray(d)) setWorkspaces(d);
        }
        if (meRes.ok) {
          const me = await meRes.json();
          setDefaultId(me.default_workspace_id || "");
        }
      } catch {
        setError(t("workspace.setDefaultFail"));
      }
    })();
  }, [t]);

  function enter(slug: string) {
    try {
      localStorage.setItem("jf_workspace", slug);
    } catch {}
    router.replace("/");
  }

  async function setDefault(workspaceId: string) {
    setBusy(true);
    setError("");
    try {
      const r = await fetch("/api/auth/profile/default-workspace", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ workspace_id: workspaceId }),
      });
      if (!r.ok) {
        setError(t("workspace.setDefaultFail"));
        return;
      }
      setDefaultId(workspaceId);
    } catch {
      setError(t("workspace.setDefaultFail"));
    } finally {
      setBusy(false);
    }
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
            {t("workspace.pickerTitle")}
          </h1>
          <p className="mt-1.5 text-sm text-[var(--jf-muted)]">{t("workspace.pickerSubtitle")}</p>
        </div>

        <div className="rounded-xl border border-[var(--jf-border)] bg-white p-6 shadow-[0_0_0_1px_rgba(0,0,0,0.2),0_8px_24px_rgba(0,0,0,0.4)]">
          {error && (
            <p role="alert" className="mb-4 rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2 text-[13px] text-red-700">
              {error}
            </p>
          )}

          {workspaces.length === 0 ? (
            <p className="py-6 text-center text-sm text-[var(--jf-muted)]">…</p>
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
                          <span className="ml-2 rounded-full bg-indigo-50 px-2 py-0.5 text-[10px] font-medium text-[var(--jf-primary)]">
                            {t("workspace.defaultLabel")}
                          </span>
                        )}
                        {s.role && (
                          <span className="ml-2 rounded-full bg-slate-100 px-2 py-0.5 text-[10px] font-medium text-slate-500">
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
      </div>
    </main>
  );
}
