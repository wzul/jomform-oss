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
import { PageLoading } from "../../components/PageLoading";
import { Shell } from "../../components/Shell";
import { useI18n } from "../../components/i18n";
import { useSession } from "../../components/session";
import { workspaceQuery } from "../../lib/workspace";
import { fetchJson, runLoad } from "../../lib/wsfetch";
import { templateEditHref } from "../../lib/templateEdit";
import { apiErr, translateApiErr } from "../../lib/apiErr";

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
  // TWO error channels, because they have two different remedies (card t_c2bba586).
  //
  //   `err`       — the LIST LOAD failed. Its remedy is to fetch the list again, so
  //                 it renders with the Retry action Issue #503 added.
  //   `actionErr` — a ROW/PAGE ACTION was REFUSED (use/save/rename/delete). The
  //                 listing is fine; nothing about it changed. It renders with NO
  //                 action, because reloading the list cannot grant a refusal.
  //
  // They used to be ONE state, so a refused "Use template" rendered the refusal
  // sentence followed by a Retry that re-fetched /templates + /forms — a control
  // whose label promised the one thing it never did. Measured on prod v1.21.307
  // (web) as a signed-in merchant: `p[role="alert"]` text ended
  // "…Remove it and save again.Retry", the URL stayed /templates, and the clone
  // count did not move. The refusal was correct; only the affordance was wrong.
  const [err, setErr] = useState("");
  const [actionErr, setActionErr] = useState("");
  const [msg, setMsg] = useState("");
  // Save-as-template: the form the merchant picked ("" = closed).
  const [saveFormId, setSaveFormId] = useState<string | null>(null);
  // Rename: the template being renamed + the draft title.
  const [renaming, setRenaming] = useState<string | null>(null);
  const [renameValue, setRenameValue] = useState("");

  // Issue #163: read_only members see no write actions.
  const canWrite = role !== "read_only";

  // Issue #503: 401/403 → /login, the same redirect Shell.signOut() uses.
  const goToLogin = useCallback(() => {
    router.replace("/login");
    window.location.href = "/login";
  }, [router]);

  const load = useCallback(async () => {
    // Issue #503: both fetches used `r.ok ? r.json() : []`, so a 401/500 was
    // silently turned into an EMPTY list (no error, no redirect). Route them
    // through the shared helper: 401/403 → /login, anything else → a visible
    // message.
    const r = await runLoad(
      async () => {
        const [tpl, frm] = await Promise.all([
          fetchJson<unknown>(`/api/rest/templates${workspaceQuery()}`),
          fetchJson<unknown>(`/api/rest/forms${workspaceQuery()}`),
        ]);
        return {
          templates: Array.isArray(tpl) ? (tpl as Template[]) : [],
          forms: Array.isArray(frm) ? (frm as FormRow[]) : [],
        };
      },
      { onUnauth: goToLogin, message: t("tplmg.loadFail"), errorKey: (b: unknown) => translateApiErr(b, t) },
    );
    if (!r.ok) {
      setTemplates([]);
      setForms([]);
      // The LOAD channel — this is the one that keeps the Retry action below.
      setErr(r.error);
      setChecking(false);
      return;
    }
    setTemplates(r.data.templates);
    setForms(r.data.forms);
    setErr("");
    setChecking(false);
  }, [t, goToLogin]);

  // Re-fetch when the active workspace changes (issue #170).
  useEffect(() => {
    load();
  }, [load, workspace]);

  useEffect(() => {
    if (anon) router.replace("/landing");
  }, [anon, router]);

  async function createFromForm(formId: string) {
    setBusy(true);
    setActionErr("");
    setMsg("");
    try {
      const r = await fetch(`/api/rest/templates${workspaceQuery()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ form_id: formId }),
      });
      const d = await r.json().catch(() => ({}));
      if (!r.ok) {
        setActionErr(apiErr(d, t("tplmg.saveFail"), t));
        return;
      }
      setSaveFormId(null);
      setMsg(t("tplmg.saved"));
      await load();
    } catch {
      setActionErr(t("tplmg.saveFail"));
    } finally {
      setBusy(false);
    }
  }

  async function createBlank() {
    setBusy(true);
    setActionErr("");
    setMsg("");
    try {
      const r = await fetch(`/api/rest/templates${workspaceQuery()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ title: t("tplmg.newTitle"), config: { mode: "response", blocks: [] } }),
      });
      const d = await r.json().catch(() => ({}));
      if (!r.ok) {
        setActionErr(apiErr(d, t("tplmg.saveFail"), t));
        return;
      }
      setMsg(t("tplmg.saved"));
      await load();
    } catch {
      setActionErr(t("tplmg.saveFail"));
    } finally {
      setBusy(false);
    }
  }

  async function rename(id: string) {
    const title = renameValue.trim();
    if (!title) return;
    setBusy(true);
    setActionErr("");
    try {
      const r = await fetch(`/api/rest/templates/${id}${workspaceQuery()}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ title }),
      });
      if (!r.ok) {
        const d = await r.json().catch(() => ({}));
        setActionErr(apiErr(d, t("tplmg.saveFail"), t));
        return;
      }
      setRenaming(null);
      await load();
    } catch {
      setActionErr(t("tplmg.saveFail"));
    } finally {
      setBusy(false);
    }
  }

  async function remove(id: string) {
    if (!window.confirm(t("tplmg.deleteConfirm"))) return;
    setBusy(true);
    setActionErr("");
    try {
      const r = await fetch(`/api/rest/templates/${id}${workspaceQuery()}`, { method: "DELETE" });
      if (!r.ok) {
        const d = await r.json().catch(() => ({}));
        setActionErr(apiErr(d, t("tplmg.deleteFail"), t));
        return;
      }
      await load();
    } catch {
      setActionErr(t("tplmg.deleteFail"));
    } finally {
      setBusy(false);
    }
  }

  // NOT a React hook, and it must not LOOK like one: `rules-of-hooks` reads any
  // call whose name starts with `use` as a hook, so `useTemplate(...)` inside an
  // onClick was reported as "cannot be called inside a callback". It is a plain
  // async request handler, so the name says what it does.
  async function applyTemplate(id: string) {
    setBusy(true);
    setActionErr("");
    try {
      const r = await fetch(`/api/rest/templates/${id}/use${workspaceQuery()}`, { method: "POST" });
      const d = await r.json().catch(() => ({}));
      if (!r.ok || !d.id) {
        setActionErr(apiErr(d, t("tplmg.useFail"), t));
        return;
      }
      router.push(`/builder?form=${d.id}`);
    } catch {
      setActionErr(t("tplmg.useFail"));
    } finally {
      setBusy(false);
    }
  }

  // In-flight: render inside the shell (see components/PageLoading.tsx) so the
  // sidebar and top bar stay mounted; the bare `<main>` this replaced dropped
  // the whole app chrome while the templates fetch was outstanding.
  if (checking) {
    return <PageLoading active="templates" />;
  }

  return (
    <Shell active="templates">
      <div className="w-full flex-1 px-6 py-8">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="min-w-0">
            <h1 className="max-w-xl text-xl font-semibold tracking-tight">{t("tplmg.title")}</h1>
            <p className="max-w-xl text-sm text-[var(--jf-muted)]">{t("tplmg.subtitle")}</p>
          </div>
          {/* t_ea90e668: these two buttons are nowrap (globals.css), and this
              group is a `shrink-0` child of the column half of the header row
              at phone width. Without `flex-wrap` + `max-w-full` the group kept
              its max-content width (measured 367.6px at 320px in `en`, 461.5px
              in `ms`) inside a 272px column, so the page scrolled sideways —
              the nowrap buttons refused the shrink the group used to get for
              free by wrapping. Each label still fits the 272px column on its
              own line (en 197.4px + 134.2px; ms 271.6px + 153.9px), so the
              group wraps instead of the labels breaking. */}
          <div className="flex max-w-full shrink-0 flex-wrap items-center gap-2 self-start sm:self-auto">
            {/* Issue #457: inline doc link (AGENTS.md docs parity). */}
            <a
              href="/docs/customize#templates"
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
                    setActionErr("");
                    setMsg("");
                  }}
                  disabled={busy || forms.length === 0}
                  className="jf-btn-ghost"
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

        {msg && <p className="mt-4 text-sm text-emerald-700">{msg}</p>}
        {/* A REFUSED ACTION: no control. The listing did not change, so there is
            nothing to reload and no button can grant the refusal. Cleared at the
            START of the next action (every handler's first write) and by a
            successful relist. Card t_c2bba586. */}
        {actionErr && (
          <p role="alert" className="mt-4 text-sm text-red-700">
            {actionErr}
          </p>
        )}
        {/* A FAILED LOAD: keeps the Retry Issue #503 added — here the remedy really
            is to fetch the list again. */}
        {err && (
          <p role="alert" className="mt-4 text-sm text-red-700">
            {err}
            {/* Issue #503: a failed load gets an action, not a dead end. */}
            <button
              type="button"
              onClick={() => void load()}
              className="ml-2 underline decoration-dotted underline-offset-2"
            >
              {t("common.retry")}
            </button>
          </p>
        )}

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
                    {/* Issue #457 follow-up (Wan, 2026-09-19): Edit the
                        template's FIELDS and DESIGN in the builder. This list
                        is ListTemplatesByAccount (account_id + is_template), so
                        every row here is the caller's own — a gallery template
                        is owned by the system account and never appears, and
                        the builder's read route is owner-scoped anyway, so an
                        id from elsewhere fails as 404 rather than opening.
                        Edit saves through PUT /api/rest/templates/{id}: the
                        builder's normal form save would write status, and a
                        published template drops out of this list and lands in
                        every merchant's gallery. */}
                    <a
                      href={templateEditHref(tpl.id)}
                      title={t("tplmg.editHint")}
                      className="jf-btn-ghost"
                    >
                      {t("tplmg.edit")}
                    </a>
                    <button
                      onClick={() => void applyTemplate(tpl.id)}
                      disabled={busy}
                      className="jf-btn-ghost"
                    >
                      {t("tplmg.use")}
                    </button>
                    <button
                      onClick={() => {
                        setRenaming(tpl.id);
                        setRenameValue(tpl.title);
                      }}
                      disabled={busy}
                      className="jf-btn-ghost"
                    >
                      {t("tplmg.rename")}
                    </button>
                    <button
                      onClick={() => void remove(tpl.id)}
                      disabled={busy}
                      className="jf-btn-ghost text-rose-700"
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
