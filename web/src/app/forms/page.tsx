"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Shell } from "../../components/Shell";
import { useI18n } from "../../components/i18n";
import { useSession } from "../../components/session";
import { workspaceQuery } from "../../lib/workspace";
import { fetchJson, runLoad } from "../../lib/wsfetch";
import TemplatePicker from "../../components/TemplatePicker";

type FormRow = { id: string; slug: string; title: string; status: string; url: string };

const HOST = typeof window !== "undefined" ? window.location.hostname : "";
// Subdomain URL when app itself runs on a subdomain? Dashboard always apex.
const subdomainBase = () => {
  const parts = window.location.hostname.split(".");
  if (parts.length >= 2) {
    const root = parts.slice(-2).join(".");
    return `https://<account>.${root}`;
  }
  return "";
};

// Issue #450: build the public form URL from the API's `url` field (which
// resolves the owning workspace slug / custom domain server-side) — never from
// a client-side localStorage fallback. A hardcoded "app" fallback produced
// https://app.jomform.com/{slug}, which 404s. The fallback here is only used
// when the API did not return a url (older payload).
const workspaceHost = (() => {
  if (typeof window === "undefined") return "";
  try {
    const slug = localStorage.getItem("jf_workspace") || "";
    return slug ? `https://${slug}.jomform.com` : "";
  } catch {
    return "";
  }
})();

// formPublicURL prefers the API-provided absolute URL; falls back to the
// workspace-subdomain host only when the payload has none.
function formPublicURL(f: { url?: string; slug: string }): string {
  if (f.url) return f.url;
  return workspaceHost ? `${workspaceHost}/${f.slug}` : "";
}

export default function FormsPage() {
  const { t } = useI18n();
  const { anon, role, workspace } = useSession();
  const router = useRouter();
  const [forms, setForms] = useState<FormRow[]>([]);
  const [checking, setChecking] = useState(true);
  const [err, setErr] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState(false);
  const [pickerOpen, setPickerOpen] = useState(false);

  // Issue #163: read_only members see no write actions (new/edit/publish/
  // delete). manage + admin keep full write access.
  const canWrite = role !== "read_only";

  // Issue #503: 401/403 → /login, the same redirect Shell.signOut() uses.
  const goToLogin = useCallback(() => {
    router.replace("/login");
    window.location.href = "/login";
  }, [router]);

  const load = useCallback(async () => {
    // Issue #29: no serial `await me → fetch` gate — the list fetch runs
    // immediately; a 401 flips `anon` and the auth bounce below handles it.
    // Issue #503: a non-ok response used to become an EMPTY list with no error
    // (`r.ok ? r.json() : []`), so a 401/500 was indistinguishable from "no
    // forms". Route it through the shared helper: 401/403 → /login, anything
    // else → a visible message.
    const r = await runLoad(
      async () => {
        const d = await fetchJson<unknown>(`/api/rest/forms${workspaceQuery()}`);
        return Array.isArray(d) ? (d as FormRow[]) : [];
      },
      { onUnauth: goToLogin, message: t("forms.loadFail") },
    );
    if (!r.ok) {
      setForms([]);
      setErr(r.error);
      setChecking(false);
      return;
    }
    setForms(r.data);
    setSelected(new Set());
    setErr("");
    setChecking(false);
  }, [t, goToLogin]);

  // Issue #170: re-fetch the list whenever the active space changes (keyed on
  // useSession().workspace), so switching workspaces shows the new forms
  // without a manual refresh.
  useEffect(() => {
    load();
  }, [load, workspace]);

  // Auth bounce via router (client transition), not a full document reload.
  useEffect(() => {
    if (anon) router.replace("/landing");
  }, [anon, router]);

  async function publish(id: string) {
    await fetch(`/api/rest/forms/${id}/publish${workspaceQuery()}`, { method: "POST" });
    load();
  }

  const ids = Array.from(selected);
  const allSelected = forms.length > 0 && selected.size === forms.length;

  function toggle(id: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function toggleAll() {
    setSelected(allSelected ? new Set() : new Set(forms.map((f) => f.id)));
  }

  async function bulk(action: "publish" | "draft" | "delete") {
    if (ids.length === 0 || busy) return;
    if (action === "delete") {
      const ok = window.confirm(
        t("forms.bulkDeleteConfirm", { n: ids.length })
      );
      if (!ok) return;
    }
    setBusy(true);
    try {
      await fetch(`/api/rest/forms/bulk${workspaceQuery()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ action, ids }),
      });
    } finally {
      setBusy(false);
      load();
    }
  }

  // Issue #503: a failed load must be visible and actionable — never an empty
  // list that reads as "you have no forms".
  if (checking) return <main className="flex min-h-screen items-center justify-center text-slate-400">…</main>;

  if (err) {
    return (
      <Shell active="forms">
        <div className="w-full flex-1 px-6 py-8">
          <div role="alert" className="jf-card flex flex-col items-start gap-3 p-6">
            <p className="text-sm text-red-600">{err}</p>
            <button type="button" onClick={() => void load()} className="jf-btn-ghost px-3 py-1.5 text-xs">
              {t("common.retry")}
            </button>
          </div>
        </div>
      </Shell>
    );
  }

  return (
    <Shell active="forms">
      <TemplatePicker open={pickerOpen} onClose={() => setPickerOpen(false)} />
      <div className="w-full flex-1 px-6 py-8">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="min-w-0">
            <h1 className="max-w-xl text-xl font-semibold tracking-tight">{t("forms.title")}</h1>
            <p className="max-w-xl text-sm text-[var(--jf-muted)]">{t("forms.subtitle")}</p>
          </div>
          {canWrite && (
            <button
              onClick={() => setPickerOpen(true)}
              className="jf-btn-primary shrink-0 self-start sm:self-auto"
            >
              {t("forms.new")}
            </button>
          )}
        </div>

        {canWrite && ids.length > 0 && (
          <div className="jf-card mt-4 flex flex-wrap items-center gap-2 border-[var(--jf-border)] px-4 py-3">
            <span className="text-sm font-medium">
              {t("forms.nSelected", { n: ids.length })}
            </span>
            <div className="ml-auto flex flex-wrap items-center gap-2">
              <button
                onClick={() => bulk("publish")}
                disabled={busy}
                className="jf-btn-ghost px-3 py-1.5 text-xs disabled:opacity-50"
              >
                {t("builder.publish")}
              </button>
              <button
                onClick={() => bulk("draft")}
                disabled={busy}
                className="jf-btn-ghost px-3 py-1.5 text-xs"
              >
                {t("forms.draft")}
              </button>
              <button
                onClick={() => bulk("delete")}
                disabled={busy}
                className="jf-btn-ghost px-3 py-1.5 text-xs text-rose-600 hover:bg-rose-50"
              >
                {t("forms.delete")}
              </button>
            </div>
          </div>
        )}

        <ul className="mt-6 space-y-2">
          {forms.length > 0 && (
            <li className="flex items-center gap-3 px-5 py-2">
              <input
                type="checkbox"
                aria-label="Select all forms"
                checked={allSelected}
                onChange={toggleAll}
                className="h-4 w-4 accent-[var(--jf-primary)]"
              />
              <span className="text-xs text-[var(--jf-muted)]">{t("forms.selectAll")}</span>
            </li>
          )}
          {forms.map((f) => (
            <li key={f.id} className="jf-card flex flex-col gap-3 px-5 py-4 sm:flex-row sm:items-center sm:justify-between">
              <div className="flex min-w-0 items-center gap-3">
                {canWrite && (
                  <input
                    type="checkbox"
                    aria-label={`Select ${f.title}`}
                    checked={selected.has(f.id)}
                    onChange={() => toggle(f.id)}
                    className="h-4 w-4 shrink-0 accent-[var(--jf-primary)]"
                  />
                )}
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium">{f.title}</p>
                  <p className="font-mono text-[11px] text-[var(--jf-muted)]">{formPublicURL(f)}</p>
                </div>
              </div>
              <div className="flex flex-wrap items-center gap-2 sm:justify-end">
                {f.status === "published" || f.status === "unlisted" ? (
                  <>
                    <span className="rounded-full border border-emerald-200 bg-emerald-50 px-2.5 py-0.5 text-[11px] font-medium text-emerald-700">
                      {f.status === "unlisted" ? t("forms.unlisted") : t("forms.live")}
                    </span>
                    {canWrite && (
                      <Link href={`/builder?form=${f.id}`} className="jf-btn-ghost min-h-[44px] sm:min-h-10">
                        {t("forms.edit")}
                      </Link>
                    )}
                    {/* Issue #177: dedicated Form Settings view */}
                    <Link href={`/form-settings?form=${f.id}`} className="jf-btn-ghost min-h-[44px] sm:min-h-10">
                      {t("fs.title")}
                    </Link>
                    {/* Issue #126: per-form Sales deep-link -> /sales?form=<id> */}
                    <Link href={`/sales?form=${f.id}`} className="jf-btn-ghost min-h-[44px] sm:min-h-10">
                      {t("nav.sales")}
                    </Link>
                    <a href={formPublicURL(f)} target="_blank" className="jf-btn-ghost min-h-[44px] sm:min-h-10">
                      {t("bld.view")}
                    </a>
                  </>
                ) : (
                  <>
                    <span className="rounded-full border border-slate-200 bg-slate-50 px-2.5 py-0.5 text-[11px] font-medium text-slate-500">
                      {f.status === "private" ? t("forms.private") : t("forms.draft")}
                    </span>
                    {canWrite && (
                      <Link href={`/builder?form=${f.id}`} className="jf-btn-ghost min-h-[44px] sm:min-h-10">
                        {t("forms.edit")}
                      </Link>
                    )}
                    {/* Issue #177: dedicated Form Settings view */}
                    <Link href={`/form-settings?form=${f.id}`} className="jf-btn-ghost min-h-[44px] sm:min-h-10">
                      {t("fs.title")}
                    </Link>
                    {/* Issue #126: per-form Sales deep-link (works for drafts too) */}
                    <Link href={`/sales?form=${f.id}`} className="jf-btn-ghost min-h-[44px] sm:min-h-10">
                      {t("nav.sales")}
                    </Link>
                    {canWrite && (
                      <button onClick={() => publish(f.id)} className="jf-btn-primary min-h-[44px] sm:min-h-10">
                        {t("builder.publish")}
                      </button>
                    )}
                  </>
                )}
              </div>
            </li>
          ))}
          {forms.length === 0 && (
            <li className="rounded-lg text-sm text-[var(--jf-muted)]">{t("forms.empty")}</li>
          )}
        </ul>
      </div>
    </Shell>
  );
}