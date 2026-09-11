"use client";

// Issue #457: Templates management. Merchants manage their OWN reusable
// templates: save an existing form as a template, create a blank one, rename
// it, delete it, and clone one into a new draft form (which opens in the
// builder). All requests are workspace-scoped and the API enforces owner
// scoping, so a template is never visible or usable across tenants.
//
// Follow-up to #174 (the template picker + system gallery). The system
// gallery stays read-only and is still browsed from the New-form picker.

import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { Shell } from "../../components/Shell";
import { useI18n } from "../../components/i18n";
import { useSession } from "../../components/session";
import { workspaceQuery } from "../../lib/workspace";

type Template = {
  id: string;
  slug: string;
  title: string;
  updated_at?: string;
  theme?: { primary?: string };
};

type FormRow = { id: string; title: string; status: string };

export default function TemplatesPage() {
  const { t } = useI18n();
  const { anon, role, workspace } = useSession();
  const router = useRouter();
  const [templates, setTemplates] = useState<Template[]>([]);
  const [forms, setForms] = useState<FormRow[]>([]);
  const [checking, setChecking] = useState(true);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [msg, setMsg] = useState("");
  // Save-as-template: the form the merchant picked ("" = closed).
  const [saveFormId, setSaveFormId] = useState<string | null>(null);
  // Rename: the template being renamed + the draft title.
  const [renaming, setRenaming] = useState<string | null>(null);
  const [renameValue, setRenameValue] = useState("");

  // Issue #163: read_only members see no write actions.
  const canWrite = role !== "read_only";

  const load = useCallback(async () => {
    const [tpl, frm] = await Promise.all([
      fetch(`/api/rest/templates${workspaceQuery()}`).then((r) => (r.ok ? r.json() : [])),
      fetch(`/api/rest/forms${workspaceQuery()}`).then((r) => (r.ok ? r.json() : [])),
    ]);
    setTemplates(Array.isArray(tpl) ? tpl : []);
    setForms(Array.isArray(frm) ? frm : []);
    setChecking(false);
  }, []);

  // Re-fetch when the active workspace changes (issue #170).
  useEffect(() => {
    load();
  }, [load, workspace]);

  useEffect(() => {
    if (anon) router.replace("/landing");
  }, [anon, router]);

  async function createFromForm(formId: string) {
    setBusy(true);
    setErr("");
    setMsg("");
    try {
      const r = await fetch(`/api/rest/templates${workspaceQuery()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ form_id: formId }),
      });
      const d = await r.json().catch(() => ({}));
      if (!r.ok) {
        setErr(d.error || t("tplmg.saveFail"));
        return;
      }
      setSaveFormId(null);
      setMsg(t("tplmg.saved"));
      await load();
    } catch {
      setErr(t("tplmg.saveFail"));
    } finally {
      setBusy(false);
    }
  }

  async function createBlank() {
    setBusy(true);
    setErr("");
    setMsg("");
    try {
      const r = await fetch(`/api/rest/templates${workspaceQuery()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ title: t("tplmg.newTitle"), config: { mode: "response", blocks: [] } }),
      });
      const d = await r.json().catch(() => ({}));
      if (!r.ok) {
        setErr(d.error || t("tplmg.saveFail"));
        return;
      }
      setMsg(t("tplmg.saved"));
      await load();
    } catch {
      setErr(t("tplmg.saveFail"));
    } finally {
      setBusy(false);
    }
  }

  async function rename(id: string) {
    const title = renameValue.trim();
    if (!title) return;
    setBusy(true);
    setErr("");
    try {
      const r = await fetch(`/api/rest/templates/${id}${workspaceQuery()}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ title }),
      });
      if (!r.ok) {
        const d = await r.json().catch(() => ({}));
        setErr(d.error || t("tplmg.saveFail"));
        return;
      }
      setRenaming(null);
      await load();
    } catch {
      setErr(t("tplmg.saveFail"));
    } finally {
      setBusy(false);
    }
  }

  async function remove(id: string) {
    if (!window.confirm(t("tplmg.deleteConfirm"))) return;
    setBusy(true);
    setErr("");
    try {
      const r = await fetch(`/api/rest/templates/${id}${workspaceQuery()}`, { method: "DELETE" });
      if (!r.ok) {
        const d = await r.json().catch(() => ({}));
        setErr(d.error || t("tplmg.deleteFail"));
        return;
      }
      await load();
    } catch {
      setErr(t("tplmg.deleteFail"));
    } finally {
      setBusy(false);
    }
  }

  async function useTemplate(id: string) {
    setBusy(true);
    setErr("");
    try {
      const r = await fetch(`/api/rest/templates/${id}/use${workspaceQuery()}`, { method: "POST" });
      const d = await r.json().catch(() => ({}));
      if (!r.ok || !d.id) {
        setErr(d.error || t("tplmg.useFail"));
        return;
      }
      router.push(`/builder?form=${d.id}`);
    } catch {
      setErr(t("tplmg.useFail"));
    } finally {
      setBusy(false);
    }
  }

  if (checking) {
    return (
      <main className="flex min-h-screen items-center justify-center text-slate-400">…</main>
    );
  }

  return (
    <Shell active="templates">
      <div className="w-full flex-1 px-6 py-8">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="min-w-0">
            <h1 className="max-w-xl text-xl font-semibold tracking-tight">{t("tplmg.title")}</h1>
            <p className="max-w-xl text-sm text-[var(--jf-muted)]">{t("tplmg.subtitle")}</p>
          </div>
          <div className="flex shrink-0 items-center gap-2 self-start sm:self-auto">
            {/* Issue #457: inline doc link (AGENTS.md docs parity). */}
            <a
              href="/docs"
              target="_blank"
              rel="noopener noreferrer"
              title={t("tplmg.docs")}
              className="inline-flex h-5 w-5 items-center justify-center rounded-full bg-slate-200 text-[11px] font-bold text-slate-600 hover:bg-[var(--jf-primary)] hover:text-white"
            >
              ?
            </a>
            {canWrite && (
              <>
                <button
                  onClick={() => {
                    setSaveFormId(saveFormId === null ? "" : null);
                    setErr("");
                    setMsg("");
                  }}
                  disabled={busy || forms.length === 0}
                  className="jf-btn-ghost disabled:opacity-50"
                >
                  {t("tplmg.saveFromForm")}
                </button>
                <button onClick={createBlank} disabled={busy} className="jf-btn-primary disabled:opacity-50">
                  {t("tplmg.new")}
                </button>
              </>
            )}
          </div>
        </div>

        {/* Save-an-existing-form-as-a-template picker */}
        {saveFormId !== null && canWrite && (
          <div className="jf-card mt-4 border-[var(--jf-border)] px-5 py-4">
            <div className="flex items-center justify-between gap-3">
              <div>
                <p className="text-sm font-medium">{t("tplmg.pickForm")}</p>
                <p className="text-xs text-[var(--jf-muted)]">{t("tplmg.pickFormHint")}</p>
              </div>
              <button
                onClick={() => setSaveFormId(null)}
                aria-label={t("tplmg.cancel")}
                className="rounded-md px-2 py-1 text-sm text-[var(--jf-muted)] hover:bg-slate-100"
              >
                ✕
              </button>
            </div>
            <ul className="mt-3 space-y-1.5">
              {forms.map((f) => (
                <li key={f.id}>
                  <button
                    onClick={() => void createFromForm(f.id)}
                    disabled={busy}
                    className="flex w-full items-center justify-between gap-3 rounded-lg border border-[var(--jf-border)] px-3 py-2 text-left text-sm hover:bg-slate-50 disabled:opacity-50"
                  >
                    <span className="truncate">{f.title}</span>
                    <span className="shrink-0 text-xs text-[var(--jf-primary)]">{t("tplmg.saveThis")}</span>
                  </button>
                </li>
              ))}
              {forms.length === 0 && (
                <li className="text-sm text-[var(--jf-muted)]">{t("tplmg.noForms")}</li>
              )}
            </ul>
          </div>
        )}

        {msg && <p className="mt-4 text-sm text-emerald-600">{msg}</p>}
        {err && <p className="mt-4 text-sm text-red-500">{err}</p>}

        <ul className="mt-6 space-y-2">
          {templates.map((tpl) => (
            <li
              key={tpl.id}
              className="jf-card flex flex-col gap-3 px-5 py-4 sm:flex-row sm:items-center sm:justify-between"
            >
              <div className="flex min-w-0 items-center gap-3">
                <span
                  className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-lg"
                  style={{ backgroundColor: (tpl.theme?.primary || "#5e6ad2") + "22" }}
                >
                  📄
                </span>
                <div className="min-w-0">
                  {renaming === tpl.id ? (
                    <div className="flex items-center gap-2">
                      <input
                        autoFocus
                        value={renameValue}
                        onChange={(e) => setRenameValue(e.target.value)}
                        onKeyDown={(e) => {
                          if (e.key === "Enter") void rename(tpl.id);
                          if (e.key === "Escape") setRenaming(null);
                        }}
                        aria-label={t("tplmg.rename")}
                        className="min-w-0 rounded-md border border-[var(--jf-border)] px-2 py-1 text-sm"
                      />
                      <button
                        onClick={() => void rename(tpl.id)}
                        disabled={busy}
                        className="jf-btn-primary px-3 py-1 text-xs disabled:opacity-50"
                      >
                        {t("tplmg.save")}
                      </button>
                    </div>
                  ) : (
                    <p className="truncate text-sm font-medium">{tpl.title}</p>
                  )}
                  <p className="font-mono text-[11px] text-[var(--jf-muted)]">{tpl.slug}</p>
                </div>
              </div>
              <div className="flex flex-wrap items-center gap-2 sm:justify-end">
                {canWrite && (
                  <>
                    <button
                      onClick={() => void useTemplate(tpl.id)}
                      disabled={busy}
                      className="jf-btn-ghost min-h-[44px] sm:min-h-10 disabled:opacity-50"
                    >
                      {t("tplmg.use")}
                    </button>
                    <button
                      onClick={() => {
                        setRenaming(tpl.id);
                        setRenameValue(tpl.title);
                      }}
                      disabled={busy}
                      className="jf-btn-ghost min-h-[44px] sm:min-h-10 disabled:opacity-50"
                    >
                      {t("tplmg.rename")}
                    </button>
                    <button
                      onClick={() => void remove(tpl.id)}
                      disabled={busy}
                      className="jf-btn-ghost min-h-[44px] text-rose-600 hover:bg-rose-50 sm:min-h-10 disabled:opacity-50"
                    >
                      {t("forms.delete")}
                    </button>
                  </>
                )}
              </div>
            </li>
          ))}
          {templates.length === 0 && (
            <li className="rounded-lg text-sm text-[var(--jf-muted)]">{t("tplmg.empty")}</li>
          )}
        </ul>
      </div>
    </Shell>
  );
}
