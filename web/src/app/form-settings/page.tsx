"use client";

// Issue #177: dedicated Form Settings view (Gravity-Forms-style).
// The builder is now fields-only; this page owns everything else about a
// form: General (title/slug/status/theme/logo/embed), Confirmation
// (success/failure redirects), Restrictions (schedule + entry limit),
// Notifications (per-form receipt email, reusing the #107 pattern) and the
// per-form CHIP feed (mirroring chip-for-gravity-forms). Sales/analytics
// live on the Sales page (#126), not here and not in the builder.

import { Suspense, useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Shell } from "../../components/Shell";
import { useI18n } from "../../components/i18n";
import { useSession } from "../../components/session";
import { spaceQuery, spaceScope } from "../../lib/space";

type FormDetail = {
  id: string;
  slug: string;
  title: string;
  status: string;
  url: string;
  config?: Record<string, unknown>;
  theme?: Record<string, unknown>;
  redirect_success?: string;
  redirect_failure?: string;
  allow_embed?: boolean;
  logo_url?: string;
  start_at?: string;
  end_at?: string;
  entry_limit?: number | null;
  pending_message?: string;
  expired_message?: string;
  limit_message?: string;
  chip_configured?: boolean;
  chip_brand_id?: string;
};

type EmailSettings = {
  receipt_subject: string;
  receipt_body: string;
  receipt_enabled: boolean;
  notify_merchant: boolean;
  merchant_notify_email: string;
  html: boolean;
};

// Issue #196: dispatcher-supported webhook events (dispatch2.go + purchases.go).
const WEBHOOK_EVENTS = ["sale.paid", "sale.failed", "subscription.charged"] as const;

type Tab = "general" | "confirmation" | "restrictions" | "notifications" | "chip" | "webhooks";

const TABS: { key: Tab; label: string }[] = [
  { key: "general", label: "fs.general" },
  { key: "confirmation", label: "fs.confirmation" },
  { key: "restrictions", label: "fs.restrictions" },
  { key: "notifications", label: "fs.notifications" },
  { key: "chip", label: "fs.chip" },
  { key: "webhooks", label: "fs.webhooks" },
];

function toLocalInput(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (isNaN(d.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function fromLocalInput(v: string): string | null {
  if (!v) return null;
  const d = new Date(v);
  if (isNaN(d.getTime())) return null;
  return d.toISOString();
}

export default function FormSettingsPage() {
  return (
    <Suspense fallback={<Shell active="forms"><div className="px-6 py-8 text-sm text-[var(--jf-muted)]">…</div></Shell>}>
      <FormSettingsInner />
    </Suspense>
  );
}

function FormSettingsInner() {
  const { t } = useI18n();
  const { anon, role } = useSession();
  const router = useRouter();
  const search = useSearchParams();
  const formId = search.get("form") || "";

  const canWrite = role !== "read_only";

  const [tab, setTab] = useState<Tab>("general");
  const [form, setForm] = useState<FormDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState("");
  const [msg, setMsg] = useState("");

  // General
  const [title, setTitle] = useState("");
  const [slug, setSlug] = useState("");
  const [status, setStatus] = useState<"draft" | "private" | "published">("draft");
  const [accessPw, setAccessPw] = useState("");
  const [fsTheme, setFsTheme] = useState("#5e6ad2");
  const [fsLogo, setFsLogo] = useState("");
  const [fsEmbed, setFsEmbed] = useState(true);

  // Confirmation
  const [fsRedS, setFsRedS] = useState("");
  const [fsRedF, setFsRedF] = useState("");

  // Restrictions
  const [rsStart, setRsStart] = useState("");
  const [rsEnd, setRsEnd] = useState("");
  const [rsLimit, setRsLimit] = useState("");
  const [rsPending, setRsPending] = useState("");
  const [rsExpired, setRsExpired] = useState("");
  const [rsLimitMsg, setRsLimitMsg] = useState("");

  // Notifications
  const [emelSubject, setEmelSubject] = useState("");
  const [emelBody, setEmelBody] = useState("");
  const [emelReceiptEnabled, setEmelReceiptEnabled] = useState(true);
  const [emelNotifyMerchant, setEmelNotifyMerchant] = useState(false);
  const [emelMerchantEmail, setEmelMerchantEmail] = useState("");
  const [emelHtml, setEmelHtml] = useState(false);

  // CHIP feed
  const [ovBrand, setOvBrand] = useState("");
  const [ovSecret, setOvSecret] = useState("");
  const [ovFallback, setOvFallback] = useState("");
  const [chipConfigured, setChipConfigured] = useState(false);

  // Issue #196: per-form webhooks
  const [whUrl, setWhUrl] = useState("");
  const [whSecret, setWhSecret] = useState("");
  const [whEvents, setWhEvents] = useState<string[]>([...WEBHOOK_EVENTS]);
  const [formWebhooks, setFormWebhooks] = useState<{ id: string; url: string; events: string[] }[]>([]);

  const load = useCallback(async () => {
    if (!formId) return;
    setLoading(true);
    setErr("");
    try {
      const r = await fetch(`/api/rest/forms/${formId}${spaceQuery()}`);
      if (!r.ok) {
        setErr(t("fs.loadErr"));
        setLoading(false);
        return;
      }
      const d = (await r.json()) as FormDetail;
      setForm(d);
      setTitle(d.title || "");
      setSlug(d.slug || "");
      setStatus(d.status === "private" ? "private" : d.status === "published" ? "published" : "draft");
      setFsTheme((d.theme?.primary as string) || "#5e6ad2");
      setFsLogo(d.logo_url || "");
      setFsEmbed(d.allow_embed !== false);
      setFsRedS(d.redirect_success || "");
      setFsRedF(d.redirect_failure || "");
      setRsStart(toLocalInput(d.start_at));
      setRsEnd(toLocalInput(d.end_at));
      setRsLimit(d.entry_limit != null ? String(d.entry_limit) : "");
      setRsPending(d.pending_message || "");
      setRsExpired(d.expired_message || "");
      setRsLimitMsg(d.limit_message || "");
      setChipConfigured(!!d.chip_configured);
      setOvBrand(d.chip_brand_id || "");
      // Load per-form email settings.
      const er = await fetch(`/api/rest/forms/${formId}/email-settings${spaceQuery()}`);
      if (er.ok) {
        const es = (await er.json()) as EmailSettings;
        setEmelSubject(es.receipt_subject || "");
        setEmelBody(es.receipt_body || "");
        setEmelReceiptEnabled(es.receipt_enabled !== false);
        setEmelNotifyMerchant(!!es.notify_merchant);
        setEmelMerchantEmail(es.merchant_notify_email || "");
        setEmelHtml(!!es.html);
      }
      // Issue #196: load this form's webhooks (form_id filter).
      const sp = spaceScope();
      const wr = await fetch(`/api/rest/webhooks?form_id=${encodeURIComponent(formId)}${sp ? `&space=${encodeURIComponent(sp)}` : ""}`);
      if (wr.ok) {
        const whs = (await wr.json()) as { id: string; url: string; events: string[] }[];
        setFormWebhooks(Array.isArray(whs) ? whs : []);
      }
    } catch {
      setErr(t("fs.loadErr"));
    } finally {
      setLoading(false);
    }
  }, [formId, t]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (anon) router.replace("/landing");
  }, [anon, router]);

  async function saveGeneral() {
    setMsg("");
    const payload = { slug: slug.toLowerCase().replace(/[^a-z0-9-]/g, "-"), title, theme: { primary: fsTheme } };
    let id = formId;
    if (!id) return;
    const r = await fetch(`/api/rest/forms/${id}${spaceQuery()}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    if (!r.ok) {
      setMsg(t("prof.failed"));
      return;
    }
    // Apply status (draft | private | published).
    const statusBody: { status: string; password?: string } = { status };
    if (status === "private") statusBody.password = accessPw;
    const sr = await fetch(`/api/rest/forms/${id}/status${spaceQuery()}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(statusBody),
    });
    if (!sr.ok) {
      setMsg(t("prof.failed"));
      return;
    }
    setMsg(t("fs.saved"));
    void load();
  }

  async function saveConfirmation() {
    setMsg("");
    if (!formId) return;
    const r = await fetch(`/api/rest/forms/${formId}/settings${spaceQuery()}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        redirect_success: fsRedS, redirect_failure: fsRedF,
        allow_embed: fsEmbed, logo_url: fsLogo,
      }),
    });
    setMsg(r.ok ? t("fs.saved") : t("prof.failed"));
  }

  async function saveRestrictions() {
    setMsg("");
    if (!formId) return;
    const limit = rsLimit.trim() === "" ? null : Math.max(0, parseInt(rsLimit, 10) || 0);
    const r = await fetch(`/api/rest/forms/${formId}/restrictions${spaceQuery()}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        start_at: fromLocalInput(rsStart),
        end_at: fromLocalInput(rsEnd),
        entry_limit: limit,
        pending_message: rsPending,
        expired_message: rsExpired,
        limit_message: rsLimitMsg,
      }),
    });
    setMsg(r.ok ? t("fs.saved") : t("prof.failed"));
  }

  async function saveNotifications() {
    setMsg("");
    if (!formId) return;
    const r = await fetch(`/api/rest/forms/${formId}/email-settings${spaceQuery()}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        receipt_subject: emelSubject,
        receipt_body: emelBody,
        receipt_enabled: emelReceiptEnabled,
        notify_merchant: emelNotifyMerchant,
        merchant_notify_email: emelMerchantEmail,
        html: emelHtml,
      }),
    });
    setMsg(r.ok ? t("fs.saved") : t("prof.failed"));
  }

  async function saveChipOverride() {
    setMsg("");
    if (!formId || !ovBrand || !ovSecret) return;
    const r = await fetch(`/api/rest/forms/${formId}/chip-override${spaceQuery()}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        brand_id: ovBrand,
        secret_key: ovSecret,
        email_fallback: ovFallback,
      }),
    });
    setMsg(r.ok ? t("fs.chipSaved") : t("fs.chipFail"));
    if (r.ok) {
      setOvSecret("");
      setChipConfigured(true);
    }
  }

  async function clearChipOverride() {
    setMsg("");
    if (!formId) return;
    const r = await fetch(`/api/rest/forms/${formId}/chip-override${spaceQuery()}`, { method: "DELETE" });
    setMsg(r.ok ? t("fs.chipCleared") : t("prof.failed"));
    if (r.ok) {
      setOvBrand("");
      setOvSecret("");
      setOvFallback("");
      setChipConfigured(false);
    }
  }

  // Issue #196: per-form webhook add/delete.
  function toggleWhEvent(ev: string) {
    setWhEvents((prev) => (prev.includes(ev) ? prev.filter((x) => x !== ev) : [...prev, ev]));
  }

  async function addFormWebhook(e: React.FormEvent) {
    e.preventDefault();
    setMsg("");
    if (!formId) return;
    const sp = spaceScope();
    const r = await fetch(`/api/rest/webhooks${sp ? `?space=${encodeURIComponent(sp)}` : ""}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ url: whUrl, secret: whSecret, events: whEvents, form_id: formId }),
    });
    if (r.ok) {
      setWhUrl("");
      setWhSecret("");
      setWhEvents([...WEBHOOK_EVENTS]);
      setMsg(t("fs.webhookAdded"));
      void load();
    } else {
      setMsg(t("fs.webhookFail"));
    }
  }

  async function deleteFormWebhook(id: string) {
    setMsg("");
    const sp = spaceScope();
    const r = await fetch(`/api/rest/webhooks/${id}${sp ? `?space=${encodeURIComponent(sp)}` : ""}`, { method: "DELETE" });
    setMsg(r.ok ? t("fs.webhookDeleted") : t("prof.failed"));
    if (r.ok) void load();
  }

  const inputCls = "jf-input mt-1 w-full max-w-xl";
  const labelCls = "text-xs text-slate-500";

  return (
    <Shell active="forms">
      <div className="w-full flex-1 px-6 py-8">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h1 className="text-xl font-semibold tracking-tight">{t("fs.title")}</h1>
            <p className="max-w-xl text-sm text-[var(--jf-muted)]">{t("fs.subtitle")}</p>
          </div>
          <div className="flex items-center gap-2">
            {formId && (
              <>
                <Link href={`/builder?form=${formId}`} className="jf-btn-ghost px-3 py-1.5 text-xs">
                  {t("fs.editFields")}
                </Link>
                <Link href={`/sales?form=${formId}`} className="jf-btn-ghost px-3 py-1.5 text-xs">
                  {t("nav.sales")}
                </Link>
                {form?.url && (
                  <a href={form.url} target="_blank" rel="noreferrer" className="jf-btn-ghost px-3 py-1.5 text-xs">
                    {t("bld.view")}
                  </a>
                )}
              </>
            )}
          </div>
        </div>

        {!formId && (
          <div className="jf-card mt-6 p-6 text-sm text-[var(--jf-muted)]">{t("fs.noForm")}</div>
        )}

        {formId && (
          <>
            {/* Tab bar */}
            <nav className="mt-6 flex flex-wrap gap-1 border-b border-[var(--jf-border)]">
              {TABS.map((tb) => (
                <button
                  key={tb.key}
                  onClick={() => setTab(tb.key)}
                  className={`px-3 py-2 text-sm font-medium ${
                    tab === tb.key
                      ? "border-b-2 border-[#7170ff] text-[var(--jf-primary)]"
                      : "text-[var(--jf-muted)] hover:text-[#374151]"
                  }`}
                >
                  {t(tb.label as import("../../components/i18n").Key)}
                </button>
              ))}
            </nav>

            {loading && <p className="mt-6 text-sm text-[var(--jf-muted)]">{t("fs.loading")}</p>}
            {err && <p className="mt-6 text-sm text-red-400">{err}</p>}
            {msg && <p className="mt-3 text-xs text-[var(--jf-primary)]">{msg}</p>}

            {!loading && !err && (
              <div className="mt-6 max-w-2xl space-y-6">
                {/* ── General ── */}
                {tab === "general" && (
                  <section className="jf-card p-6">
                    <h2 className="text-base font-semibold">{t("fs.general")}</h2>
                    <div className="mt-4 space-y-3">
                      <div>
                        <label className={labelCls}>{t("bld.formTitle")}</label>
                        <input value={title} onChange={(e) => setTitle(e.target.value)} className={inputCls} />
                      </div>
                      <div>
                        <label className={labelCls}>{t("fs.slug")}</label>
                        <input
                          value={slug}
                          onChange={(e) => setSlug(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, "-"))}
                          className={`${inputCls} font-mono`}
                        />
                      </div>
                      <div>
                        <label className={labelCls}>{t("bld.status")}</label>
                        <div className="mt-1 grid max-w-xl grid-cols-3 gap-1.5">
                          {(["draft", "private", "published"] as const).map((s) => (
                            <button
                              key={s}
                              type="button"
                              onClick={() => setStatus(s)}
                              className={`rounded-md border px-2 py-2 text-xs font-medium transition-colors ${
                                status === s
                                  ? "border-[#7170ff]/60 bg-indigo-50 text-[var(--jf-primary)]"
                                  : "border-[var(--jf-border)] text-[#374151] hover:bg-slate-50"
                              }`}
                            >
                              {t(`bld.status${s[0].toUpperCase()}${s.slice(1)}` as import("../../components/i18n").Key)}
                            </button>
                          ))}
                        </div>
                        {status === "private" && (
                          <div className="mt-2">
                            <label className="block text-[11px] text-slate-500">{t("bld.accessPassword")}</label>
                            <input
                              type="password"
                              value={accessPw}
                              onChange={(e) => setAccessPw(e.target.value)}
                              placeholder={t("bld.accessPasswordPh")}
                              className="jf-input mt-1 w-full max-w-xl text-xs"
                            />
                          </div>
                        )}
                      </div>
                      <div>
                        <label className={labelCls}>{t("bld.themeColor")}</label>
                        <input
                          type="color"
                          value={fsTheme}
                          onChange={(e) => setFsTheme(e.target.value)}
                          className="mt-1 h-8 w-full max-w-xl cursor-pointer rounded border border-[var(--jf-border)]"
                        />
                      </div>
                      <div>
                        <label className={labelCls}>{t("bld.logoUrl")}</label>
                        <input
                          value={fsLogo}
                          onChange={(e) => setFsLogo(e.target.value)}
                          placeholder="https://cdn.example/logo.png"
                          className={inputCls}
                        />
                      </div>
                      <label className="flex items-center gap-2 text-sm text-slate-700">
                        <input type="checkbox" checked={fsEmbed} onChange={(e) => setFsEmbed(e.target.checked)} className="accent-[#5e6ad2]" />
                        {t("bld.allowEmbed")}
                      </label>
                      <div className="pt-1">
                        <button onClick={saveGeneral} disabled={!canWrite} className="jf-btn-primary">
                          {t("fs.save")}
                        </button>
                      </div>
                    </div>
                  </section>
                )}

                {/* ── Confirmation ── */}
                {tab === "confirmation" && (
                  <section className="jf-card p-6">
                    <h2 className="text-base font-semibold">{t("fs.confirmation")}</h2>
                    <p className="mt-1 text-sm text-[var(--jf-muted)]">{t("fs.confirmationHint")}</p>
                    <div className="mt-4 space-y-3">
                      <div>
                        <label className={labelCls}>{t("bld.redirSuccess")}</label>
                        <input value={fsRedS} onChange={(e) => setFsRedS(e.target.value)} placeholder="https://yourapp.com/thanks" className={inputCls} />
                      </div>
                      <div>
                        <label className={labelCls}>{t("bld.redirFailure")}</label>
                        <input value={fsRedF} onChange={(e) => setFsRedF(e.target.value)} placeholder="https://yourapp.com/fail" className={inputCls} />
                      </div>
                      <div className="pt-1">
                        <button onClick={saveConfirmation} disabled={!canWrite} className="jf-btn-primary">
                          {t("fs.save")}
                        </button>
                      </div>
                    </div>
                  </section>
                )}

                {/* ── Restrictions ── */}
                {tab === "restrictions" && (
                  <section className="jf-card p-6">
                    <h2 className="text-base font-semibold">{t("fs.restrictions")}</h2>
                    <div className="mt-4 space-y-3">
                      <p className="text-[11px] font-medium uppercase tracking-wider text-[#9ca3af]">{t("bld.scheduleTitle")}</p>
                      <div>
                        <label className={labelCls}>{t("bld.startAt")}</label>
                        <input type="datetime-local" value={rsStart} onChange={(e) => setRsStart(e.target.value)} className={inputCls} />
                      </div>
                      <div>
                        <label className={labelCls}>{t("bld.endAt")}</label>
                        <input type="datetime-local" value={rsEnd} onChange={(e) => setRsEnd(e.target.value)} className={inputCls} />
                      </div>
                      <div>
                        <label className={labelCls}>{t("bld.pendingMessage")}</label>
                        <textarea value={rsPending} onChange={(e) => setRsPending(e.target.value)} rows={2} className={inputCls} />
                      </div>
                      <div>
                        <label className={labelCls}>{t("bld.expiredMessage")}</label>
                        <textarea value={rsExpired} onChange={(e) => setRsExpired(e.target.value)} rows={2} className={inputCls} />
                      </div>
                      <p className="text-[11px] font-medium uppercase tracking-wider text-[#9ca3af]">{t("bld.entryLimitTitle")}</p>
                      <div>
                        <label className={labelCls}>{t("bld.entryLimit")}</label>
                        <input type="number" min={0} value={rsLimit} onChange={(e) => setRsLimit(e.target.value)} placeholder="100" className={inputCls} />
                      </div>
                      <div>
                        <label className={labelCls}>{t("bld.limitMessage")}</label>
                        <textarea value={rsLimitMsg} onChange={(e) => setRsLimitMsg(e.target.value)} rows={2} className={inputCls} />
                      </div>
                      <div className="pt-1">
                        <button onClick={saveRestrictions} disabled={!canWrite} className="jf-btn-primary">
                          {t("fs.save")}
                        </button>
                      </div>
                    </div>
                  </section>
                )}

                {/* ── Notifications ── */}
                {tab === "notifications" && (
                  <section className="jf-card p-6">
                    <h2 className="text-base font-semibold">{t("fs.notifications")}</h2>
                    <p className="mt-1 text-sm text-[var(--jf-muted)]">{t("fs.notificationsHint")}</p>
                    <div className="mt-4 space-y-3">
                      <div>
                        <label className={labelCls}>
                          {t("set.emelSubject")} <span className="text-slate-400">{t("fs.blankUsesSpace")}</span>
                        </label>
                        <input value={emelSubject} onChange={(e) => setEmelSubject(e.target.value)} className={inputCls} placeholder="Resit pembayaran — {{ref_code}}" />
                      </div>
                      <div>
                        <label className={labelCls}>
                          {t("set.emelBody")} <span className="text-slate-400">{t("set.emelPlaceholders")} <code className="rounded bg-slate-100 px-1">{"{{ref_code}}"}</code> <code className="rounded bg-slate-100 px-1">{"{{amount}}"}</code> <code className="rounded bg-slate-100 px-1">{"{{items}}"}</code> <code className="rounded bg-slate-100 px-1">{"{{form_url}}"}</code> <code className="rounded bg-slate-100 px-1">{"{{buyer_name}}"}</code> <code className="rounded bg-slate-100 px-1">{"{{date}}"}</code></span>
                        </label>
                        <textarea value={emelBody} onChange={(e) => setEmelBody(e.target.value)} rows={8} className={`${inputCls} font-mono text-xs`} />
                      </div>
                      <div className="flex flex-wrap items-center gap-4">
                        <label className="flex items-center gap-2 text-sm text-slate-700">
                          <input type="checkbox" checked={emelReceiptEnabled} onChange={(e) => setEmelReceiptEnabled(e.target.checked)} />
                          {t("set.emelReceiptEnabled")}
                        </label>
                        <label className="flex items-center gap-2 text-sm text-slate-700">
                          <input type="checkbox" checked={emelNotifyMerchant} onChange={(e) => setEmelNotifyMerchant(e.target.checked)} />
                          {t("set.emelNotifyMerchant")}
                        </label>
                        <label className="flex items-center gap-2 text-sm text-slate-700">
                          <input type="checkbox" checked={emelHtml} onChange={(e) => setEmelHtml(e.target.checked)} />
                          {t("set.emelHtml")}
                        </label>
                      </div>
                      {emelNotifyMerchant && (
                        <div>
                          <label className={labelCls}>{t("set.emelMerchantEmail")}</label>
                          <input value={emelMerchantEmail} onChange={(e) => setEmelMerchantEmail(e.target.value)} type="email" className={inputCls} placeholder="store@yourshop.my" />
                        </div>
                      )}
                      <div className="pt-1">
                        <button onClick={saveNotifications} disabled={!canWrite} className="jf-btn-primary">
                          {t("fs.save")}
                        </button>
                      </div>
                    </div>
                  </section>
                )}

                {/* ── CHIP feed ── */}
                {tab === "chip" && (
                  <section className="jf-card p-6">
                    <h2 className="text-base font-semibold">{t("fs.chip")}</h2>
                    <p className="mt-1 text-sm text-[var(--jf-muted)]">{t("fs.chipHint")}</p>
                    <div className="mt-3">
                      <span className={`rounded-full border px-2.5 py-0.5 text-[11px] font-medium ${
                        chipConfigured ? "border-emerald-200 bg-emerald-50 text-emerald-700" : "border-slate-200 bg-slate-50 text-slate-500"
                      }`}>
                        {chipConfigured ? t("fs.chipConfigured") : t("fs.chipNotConfigured")}
                      </span>
                      {chipConfigured && ovBrand && (
                        <span className="ml-2 text-[11px] text-[var(--jf-muted)]">{t("fs.chipBrand")} {ovBrand}</span>
                      )}
                    </div>
                    <div className="mt-4 space-y-3">
                      <div>
                        <label className={labelCls}>{t("fs.chipBrandId")}</label>
                        <input value={ovBrand} onChange={(e) => setOvBrand(e.target.value)} placeholder="Brand ID" className={inputCls} />
                      </div>
                      <div>
                        <label className={labelCls}>{t("set.secretKey")}</label>
                        <input value={ovSecret} onChange={(e) => setOvSecret(e.target.value)} type="text" placeholder="sk_live_… / sk_test_…" className={inputCls} />
                      </div>
                      <div>
                        <label className={labelCls}>{t("set.emailFallback")}</label>
                        <input value={ovFallback} onChange={(e) => setOvFallback(e.target.value)} type="email" placeholder="Email fallback (one-time forms)" className={inputCls} />
                      </div>
                      <p className="text-[11px] text-[var(--jf-muted)]">{t("bld.emptyDefaults")}</p>
                      <div className="flex flex-wrap items-center gap-2 pt-1">
                        <button onClick={saveChipOverride} disabled={!canWrite || !ovBrand || !ovSecret} className="jf-btn-primary">
                          {t("bld.saveOverride")}
                        </button>
                        <button onClick={clearChipOverride} disabled={!canWrite} className="jf-btn-ghost text-slate-600">
                          {t("set.clear")}
                        </button>
                      </div>
                    </div>
                  </section>
                )}

                {/* ── Webhooks (per-form) ── */}
                {tab === "webhooks" && (
                  <section className="jf-card p-6">
                    <h2 className="text-base font-semibold">{t("fs.webhooks")}</h2>
                    <p className="mt-1 text-sm text-[var(--jf-muted)]">{t("fs.webhooksHint")}</p>
                    <form onSubmit={addFormWebhook} className="mt-4">
                      <div className="flex flex-wrap gap-2">
                        <input value={whUrl} onChange={(e) => setWhUrl(e.target.value)} required type="url"
                          placeholder="https://yourapp.com/jomform-hook" className="jf-input min-w-56 max-w-xl flex-1" />
                        <input value={whSecret} onChange={(e) => setWhSecret(e.target.value)} required type="text"
                          placeholder={t("set.signingSecret")} className="jf-input w-48" />
                        <button className="jf-btn-primary" disabled={!canWrite}>{t("set.add")}</button>
                      </div>
                      <div className="mt-2">
                        <p className="text-xs text-slate-500">{t("webhook.eventsTitle")}</p>
                        <div className="mt-1 flex flex-wrap items-center gap-3">
                          {WEBHOOK_EVENTS.map((ev) => (
                            <label key={ev} className="flex items-center gap-1 text-sm text-slate-700">
                              <input type="checkbox" checked={whEvents.includes(ev)} onChange={() => toggleWhEvent(ev)} />
                              {ev}
                            </label>
                          ))}
                          <label className="flex items-center gap-1 text-sm text-slate-400">
                            <input type="checkbox" checked={whEvents.includes("*")} onChange={() => toggleWhEvent("*")} />
                            {t("webhook.event.all")}
                          </label>
                        </div>
                      </div>
                    </form>
                    {formWebhooks.length === 0 ? (
                      <p className="mt-4 text-sm text-[var(--jf-muted)]">{t("fs.webhooksEmpty")}</p>
                    ) : (
                      <ul className="mt-4 space-y-2">
                        {formWebhooks.map((wh) => (
                          <li key={wh.id} className="flex items-center justify-between gap-3 rounded-lg border border-[var(--jf-border)] px-4 py-2.5">
                            <div className="min-w-0">
                              <p className="truncate font-mono text-xs">{wh.url}</p>
                              <p className="mt-0.5 text-[11px] text-slate-400">{(wh.events || []).join(", ")}</p>
                            </div>
                            <button onClick={() => deleteFormWebhook(wh.id)} disabled={!canWrite}
                              className="shrink-0 text-xs text-slate-500 hover:text-red-500">✕</button>
                          </li>
                        ))}
                      </ul>
                    )}
                  </section>
                )}
              </div>
            )}
          </>
        )}
      </div>
    </Shell>
  );
}
