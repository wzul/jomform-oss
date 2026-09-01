"use client";

import { useCallback, useEffect, useState } from "react";
import { Shell } from "../../components/Shell";
import { useI18n } from "../../components/i18n";

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

const spaceHost =
  typeof window !== "undefined" ? `${localStorage.getItem("jf_space") || "app"}.jomform.com` : "app.jomform.com";

export default function FormsPage() {
  const { t } = useI18n();
  const [forms, setForms] = useState<FormRow[]>([]);
  const [checking, setChecking] = useState(true);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    const me = await fetch("/api/auth/me");
    if (!me.ok) {
      window.location.href = "/landing";
      return;
    }
    const f = await fetch("/api/rest/forms").then((r) => r.json());
    setForms(Array.isArray(f) ? f : []);
    setSelected(new Set());
    setChecking(false);
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  async function publish(id: string) {
    await fetch(`/api/rest/forms/${id}/publish`, { method: "POST" });
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
        `Move ${ids.length} form${ids.length === 1 ? "" : "s"} to trash? ` +
          "They will be soft-deleted (hidden from the dashboard and public pages) and can be restored later."
      );
      if (!ok) return;
    }
    setBusy(true);
    try {
      await fetch("/api/rest/forms/bulk", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ action, ids }),
      });
    } finally {
      setBusy(false);
      load();
    }
  }

  if (checking) return <main className="flex min-h-screen items-center justify-center text-slate-400">…</main>;

  return (
    <Shell active="forms">
      <div className="mx-auto w-full max-w-4xl flex-1 px-6 py-8">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-xl font-semibold tracking-tight">{t("forms.title")}</h1>
            <p className="text-sm text-[var(--jf-muted)]">{t("forms.subtitle")}</p>
          </div>
          <a href="/builder" className="jf-btn-primary">
            {t("forms.new")}
          </a>
        </div>

        {ids.length > 0 && (
          <div className="jf-card mt-4 flex flex-wrap items-center gap-2 border-[var(--jf-border)] px-4 py-3">
            <span className="text-sm font-medium">
              {ids.length} selected
            </span>
            <div className="ml-auto flex flex-wrap items-center gap-2">
              <button
                onClick={() => bulk("publish")}
                disabled={busy}
                className="jf-btn-ghost px-3 py-1.5 text-xs disabled:opacity-50"
              >
                Publish
              </button>
              <button
                onClick={() => bulk("draft")}
                disabled={busy}
                className="jf-btn-ghost px-3 py-1.5 text-xs"
              >
                Draft
              </button>
              <button
                onClick={() => bulk("delete")}
                disabled={busy}
                className="jf-btn-ghost px-3 py-1.5 text-xs text-rose-600 hover:bg-rose-50"
              >
                Delete
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
              <span className="text-xs text-[var(--jf-muted)]">Select all</span>
            </li>
          )}
          {forms.map((f) => (
            <li key={f.id} className="jf-card flex items-center justify-between px-5 py-4">
              <div className="flex min-w-0 items-center gap-3">
                <input
                  type="checkbox"
                  aria-label={`Select ${f.title}`}
                  checked={selected.has(f.id)}
                  onChange={() => toggle(f.id)}
                  className="h-4 w-4 shrink-0 accent-[var(--jf-primary)]"
                />
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium">{f.title}</p>
                  <p className="font-mono text-[11px] text-[var(--jf-muted)]">{spaceHost}/{f.slug}</p>
                </div>
              </div>
              <div className="flex items-center gap-2">
                {f.status === "published" ? (
                  <>
                    <span className="rounded-full border border-emerald-200 bg-emerald-50 px-2.5 py-0.5 text-[11px] font-medium text-emerald-700">
                      {t("forms.live")}
                    </span>
                    <a href={`/builder?form=${f.id}`} className="jf-btn-ghost">
                      Edit
                    </a>
                    <a href={`https://${spaceHost}/${f.slug}`} target="_blank" className="jf-btn-ghost">
                      View ↗
                    </a>
                  </>
                ) : (
                  <>
                    <span className="rounded-full border border-slate-200 bg-slate-50 px-2.5 py-0.5 text-[11px] font-medium text-slate-500">
                      {t("forms.draft")}
                    </span>
                    <a href={`/builder?form=${f.id}`} className="jf-btn-ghost">
                      Edit
                    </a>
                    <button onClick={() => publish(f.id)} className="jf-btn-primary">
                      Publish
                    </button>
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