"use client";

// Issue #184: per-workspace hosted legal pages (privacy / terms / refund).
// For each page the merchant can auto-generate a draft via the LLM (fills a
// prompt template with business details → generate → preview/edit → save) or
// write their own manually. The stored content is rendered on the public page
// ({workspace}.jomform.com/privacy, /terms, /refund).
import { useCallback, useEffect, useState } from "react";
import { useI18n, type Key } from "./i18n";

type LegalPage = { workspace_id: string; page_type: string; mode: string; content: string };
type LegalMap = Record<string, LegalPage>;

const PAGE_TYPES = ["privacy", "terms", "refund"] as const;
type PageType = (typeof PAGE_TYPES)[number];

const PAGE_TITLE_KEY: Record<PageType, Key> = {
  privacy: "set.legalPrivacy",
  terms: "set.legalTerms",
  refund: "set.legalRefund",
};

export function LegalTab({ workspaceId, workspaceSlug }: { workspaceId: string; workspaceSlug: string }) {
  const { t } = useI18n();
  const [pages, setPages] = useState<LegalMap>({});
  const [mode, setMode] = useState<Record<PageType, "auto" | "manual">>({
    privacy: "manual",
    terms: "manual",
    refund: "manual",
  });
  const [content, setContent] = useState<Record<PageType, string>>({
    privacy: "",
    terms: "",
    refund: "",
  });
  // Business-detail fields for the prompt template (shared across pages).
  const [biz, setBiz] = useState({
    business_name: "",
    registration_no: "",
    contact: "",
    address: "",
    data_collected: "",
    payment_terms: "",
    refund_terms: "",
    website: "",
    effective_date: "",
  });
  const [generating, setGenerating] = useState<PageType | null>(null);
  const [msg, setMsg] = useState("");
  const [err, setErr] = useState("");

  const load = useCallback(() => {
    if (!workspaceId) return;
    fetch(`/api/workspaces/${workspaceId}/legal`)
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => {
        if (!d) return;
        const next: LegalMap = {};
        const nextMode = { ...mode };
        const nextContent = { ...content };
        for (const pt of PAGE_TYPES) {
          const p = d[pt];
          if (p) {
            next[pt] = p;
            nextMode[pt] = p.mode === "auto" ? "auto" : "manual";
            nextContent[pt] = p.content || "";
          }
        }
        setPages(next);
        setMode(nextMode);
        setContent(nextContent);
      })
      .catch(() => setErr(t("set.legalLoadFail")));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [workspaceId]);

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [load]);

  async function generate(pt: PageType) {
    setErr("");
    setGenerating(pt);
    try {
      const r = await fetch(`/api/workspaces/${workspaceId}/legal/generate`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ type: pt, ...biz }),
      });
      const d = await r.json().catch(() => ({}));
      if (!r.ok) {
        setErr(d.error || t("set.legalGenerateFail"));
        return;
      }
      setContent((prev) => ({ ...prev, [pt]: d.content || "" }));
      setMode((prev) => ({ ...prev, [pt]: "auto" }));
      setMsg(t("set.legalGenerate") + " ✓");
    } catch {
      setErr(t("set.legalGenerateFail"));
    } finally {
      setGenerating(null);
    }
  }

  async function save(pt: PageType) {
    setErr("");
    setMsg("");
    const r = await fetch(`/api/workspaces/${workspaceId}/legal`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ type: pt, content: content[pt], mode: mode[pt] }),
    });
    if (r.ok) {
      setMsg(t("set.legalSaved"));
      load();
    } else {
      const d = await r.json().catch(() => ({}));
      setErr(d.error || t("set.legalSaveFail"));
    }
  }

  const setBizField = (k: keyof typeof biz, v: string) => setBiz((prev) => ({ ...prev, [k]: v }));

  return (
    <section className="jf-card mt-6 p-6">
      <h2 className="text-lg font-semibold">{t("set.legalTitle")}</h2>
      <p className="mt-1 max-w-xl text-sm text-[var(--jf-muted)]">
        {t("set.legalHelp", { workspace: workspaceSlug || "your-workspace" })}
      </p>

      {msg && <p className="mt-3 rounded-lg bg-emerald-50 px-3 py-2 text-sm text-emerald-600">{msg}</p>}
      {err && <p className="mt-3 rounded-lg bg-red-50 px-3 py-2 text-sm text-red-600">{err}</p>}

      {/* Business details shared by the prompt template */}
      <div className="mt-4 grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div>
          <label className="text-xs text-slate-500">{t("set.legalBusinessName")}</label>
          <input value={biz.business_name} onChange={(e) => setBizField("business_name", e.target.value)} className="jf-input mt-1" />
        </div>
        <div>
          <label className="text-xs text-slate-500">{t("set.legalRegNo")}</label>
          <input value={biz.registration_no} onChange={(e) => setBizField("registration_no", e.target.value)} className="jf-input mt-1" />
        </div>
        <div>
          <label className="text-xs text-slate-500">{t("set.legalContact")}</label>
          <input value={biz.contact} onChange={(e) => setBizField("contact", e.target.value)} className="jf-input mt-1" />
        </div>
        <div>
          <label className="text-xs text-slate-500">{t("set.legalAddress")}</label>
          <input value={biz.address} onChange={(e) => setBizField("address", e.target.value)} className="jf-input mt-1" />
        </div>
        <div>
          <label className="text-xs text-slate-500">{t("set.legalWebsite")}</label>
          <input value={biz.website} onChange={(e) => setBizField("website", e.target.value)} className="jf-input mt-1" />
        </div>
        <div>
          <label className="text-xs text-slate-500">{t("set.legalEffectiveDate")}</label>
          <input value={biz.effective_date} onChange={(e) => setBizField("effective_date", e.target.value)} className="jf-input mt-1" placeholder="2026-09-01" />
        </div>
        <div>
          <label className="text-xs text-slate-500">{t("set.legalDataCollected")}</label>
          <input value={biz.data_collected} onChange={(e) => setBizField("data_collected", e.target.value)} className="jf-input mt-1" />
        </div>
        <div>
          <label className="text-xs text-slate-500">{t("set.legalPaymentTerms")}</label>
          <input value={biz.payment_terms} onChange={(e) => setBizField("payment_terms", e.target.value)} className="jf-input mt-1" />
        </div>
        <div>
          <label className="text-xs text-slate-500">{t("set.legalRefundTerms")}</label>
          <input value={biz.refund_terms} onChange={(e) => setBizField("refund_terms", e.target.value)} className="jf-input mt-1" />
        </div>
      </div>

      {/* One editor per page type */}
      <div className="mt-6 space-y-6">
        {PAGE_TYPES.map((pt) => {
          const page = pages[pt];
          const isSet = !!page && !!page.content;
          return (
            <div key={pt} className="rounded-lg border border-[var(--jf-border)] p-4">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <h3 className="text-sm font-semibold text-slate-700">{t(PAGE_TITLE_KEY[pt])}</h3>
                <div className="flex items-center gap-3 text-xs">
                  {isSet ? (
                    <span className="rounded-full border border-emerald-200 bg-emerald-50 px-2 py-0.5 font-semibold text-emerald-700">
                      {page.mode === "auto" ? t("set.legalModeAuto") : t("set.legalModeManual")}
                    </span>
                  ) : (
                    <span className="rounded-full border border-slate-200 bg-slate-50 px-2 py-0.5 font-semibold text-slate-500">
                      {t("set.legalNotSet")}
                    </span>
                  )}
                  {isSet && workspaceSlug && (
                    <a
                      href={`https://${workspaceSlug}.jomform.com/${pt}`}
                      target="_blank"
                      rel="noreferrer"
                      className="text-[var(--jf-primary)] hover:underline"
                    >
                      {t("set.legalView")}
                    </a>
                  )}
                </div>
              </div>

              {/* Mode choice */}
              <div className="mt-3 flex flex-wrap items-center gap-4 text-sm text-slate-700">
                <label className="flex items-center gap-1.5">
                  <input
                    type="radio"
                    name={`mode-${pt}`}
                    checked={mode[pt] === "auto"}
                    onChange={() => setMode((prev) => ({ ...prev, [pt]: "auto" }))}
                  />
                  {t("set.legalAuto")}
                </label>
                <label className="flex items-center gap-1.5">
                  <input
                    type="radio"
                    name={`mode-${pt}`}
                    checked={mode[pt] === "manual"}
                    onChange={() => setMode((prev) => ({ ...prev, [pt]: "manual" }))}
                  />
                  {t("set.legalManual")}
                </label>
              </div>

              {/* Auto-generate button (only in auto mode) */}
              {mode[pt] === "auto" && (
                <button
                  onClick={() => generate(pt)}
                  disabled={generating !== null}
                  className="jf-btn-ghost mt-3 text-slate-600"
                >
                  {generating === pt ? t("set.legalGenerating") : t("set.legalGenerate")}
                </button>
              )}

              {/* Content editor */}
              <div className="mt-3">
                <label className="text-xs text-slate-500">{t("set.legalContent")}</label>
                <textarea
                  value={content[pt]}
                  onChange={(e) => setContent((prev) => ({ ...prev, [pt]: e.target.value }))}
                  rows={8}
                  className="jf-input mt-1 w-full font-mono text-xs"
                  placeholder={t("set.legalContentPh")}
                />
              </div>

              <div className="mt-3 flex items-center gap-2">
                <button onClick={() => save(pt)} className="jf-btn-primary">
                  {t("set.legalSave")}
                </button>
              </div>
            </div>
          );
        })}
      </div>
    </section>
  );
}
