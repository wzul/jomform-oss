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

  const load = useCallback(async () => {
    const me = await fetch("/api/auth/me");
    if (!me.ok) {
      window.location.href = "/landing";
      return;
    }
    const f = await fetch("/api/rest/forms").then((r) => r.json());
    setForms(Array.isArray(f) ? f : []);
    setChecking(false);
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  async function publish(id: string) {
    await fetch(`/api/rest/forms/${id}/publish`, { method: "POST" });
    load();
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
          <a href="/builder" className="jf-btn-primary px-4 py-2 text-sm">
            {t("forms.new")}
          </a>
        </div>

        <ul className="mt-6 space-y-2">
          {forms.map((f) => (
            <li key={f.id} className="jf-card flex items-center justify-between px-5 py-4">
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">{f.title}</p>
                <p className="font-mono text-[11px] text-[var(--jf-muted)]">{spaceHost}/{f.slug}</p>
              </div>
              <div className="flex items-center gap-2">
                {f.status === "published" ? (
                  <>
                    <span className="rounded-full border border-emerald-200 bg-emerald-50 px-2.5 py-0.5 text-[11px] font-medium text-emerald-700">
                      {t("forms.live")}
                    </span>
                    <a href={`/builder?form=${f.id}`} className="jf-btn-ghost px-3 py-1.5 text-xs">
                      Edit
                    </a>
                    <a href={`https://${spaceHost}/${f.slug}`} target="_blank" className="jf-btn-ghost px-3 py-1.5 text-xs">
                      View ↗
                    </a>
                  </>
                ) : (
                  <>
                    <span className="rounded-full border border-slate-200 bg-slate-50 px-2.5 py-0.5 text-[11px] font-medium text-slate-500">
                      {t("forms.draft")}
                    </span>
                    <a href={`/builder?form=${f.id}`} className="jf-btn-ghost px-3 py-1.5 text-xs">
                      Edit
                    </a>
                    <button onClick={() => publish(f.id)} className="jf-btn-primary px-3 py-1.5 text-xs">
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