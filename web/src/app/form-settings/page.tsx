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

// Issue #193: per-form notification rule (Gravity-Forms-style).
type Notification = {
  id: string;
  form_id: string;
  name: string;
  event: string;
  to_email: string;
  subject: string;
  body: string;
  condition?: { field_id: string; op: string; value: string } | null;
  enabled: boolean;
  is_default: boolean;
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

// Issue #208: a per-form CHIP feed (Gravity-Forms-style). A feed with no
// condition is the default feed and always matches.
type ChipFeed = {
  id: string;
  name: string;
  brand_id: string;
  email_fallback: string;
  payment_method_whitelist?: string[];
  configured: boolean;
  condition?: { field_id: string; op: string; value: string } | null;
};

type ChipFeedDraft = {
  id: string;
  name: string;
  brand_id: string;
  secret_key: string;
  email_fallback: string;
  condition?: { field_id: string; op: string; value: string } | null;
};

// Field types that collect a buyer answer (used for the condition selector).
const FEED_FIELD_TYPES = new Set([
  "text", "textarea", "number", "email", "phone", "website", "select",
  "radio", "image-choice", "option", "shipping", "date", "time", "hidden",
]);

const FEED_OPS = ["equals", "not_equals", "contains", "gt", "lt", "not_empty"];

function feedUid() {
  return "feed_" + Math.random().toString(36).slice(2, 9);
}

const NOTIF_EVENTS = [
  "sale.paid",
  "sale.failed",
  "response.submitted",
  "subscription.charged",
] as const;

const NOTIF_OPS = ["equals", "not_equals", "contains", "gt", "lt", "not_empty"] as const;

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

  // Notifications (issue #193: per-form notification rules)
  const [notifs, setNotifs] = useState<Notification[]>([]);
  const [editing, setEditing] = useState<Notification | null>(null);
  const [isNew, setIsNew] = useState(false);
  // Editor fields
  const [edName, setEdName] = useState("");
  const [edEvent, setEdEvent] = useState<string>("sale.paid");
  const [edTo, setEdTo] = useState("");
  const [edSubject, setEdSubject] = useState("");
  const [edBody, setEdBody] = useState("");
  const [edEnabled, setEdEnabled] = useState(true);
  const [edHasCond, setEdHasCond] = useState(false);
  const [edCondField, setEdCondField] = useState("");
  const [edCondOp, setEdCondOp] = useState("equals");
  const [edCondValue, setEdCondValue] = useState("");

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
  // Issue #208: multiple per-form CHIP feeds with conditional logic.
  const [feeds, setFeeds] = useState<ChipFeedDraft[]>([]);
  const [formBlocks, setFormBlocks] = useState<{ id: string; value: string; type: string }[]>([]);

  // Per-form email settings (issue #177).
  const [emelSubject, setEmelSubject] = useState("");
  const [emelBody, setEmelBody] = useState("");
  const [emelReceiptEnabled, setEmelReceiptEnabled] = useState(true);
  const [emelNotifyMerchant, setEmelNotifyMerchant] = useState(false);
  const [emelMerchantEmail, setEmelMerchantEmail] = useState("");
  const [emelHtml, setEmelHtml] = useState(false);

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
      // Issue #208: hydrate the form's CHIP feeds + the field blocks used by
      // the conditional-logic selector.
      const blocks = (d.config?.blocks as { id: string; value: string; type: string }[] | undefined) || [];
      setFormBlocks(blocks.filter((b) => FEED_FIELD_TYPES.has(b.type)));
      const fr = await fetch(`/api/rest/forms/${formId}/chip-feeds${spaceQuery()}`);
      if (fr.ok) {
        const fd = (await fr.json()) as { feeds: ChipFeed[] };
        setFeeds(
          fd.feeds.map((f) => ({
            id: f.id,
            name: f.name,
            brand_id: f.brand_id,
            secret_key: "",
            email_fallback: f.email_fallback,
            condition: f.condition || null,
          })),
        );
      }
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
      // Load per-form notification rules (issue #193).
      const nr = await fetch(`/api/rest/forms/${formId}/notifications${spaceQuery()}`);
      if (nr.ok) {
        const list = (await nr.json()) as Notification[];
        setNotifs(list);
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

  function openNewNotification() {
    setEditing(null);
    setIsNew(true);
    setEdName("");
    setEdEvent("sale.paid");
    setEdTo("{admin_email}");
    setEdSubject("");
    setEdBody("");
    setEdEnabled(true);
    setEdHasCond(false);
    setEdCondField("");
    setEdCondOp("equals");
    setEdCondValue("");
  }

  function openEditNotification(n: Notification) {
    setEditing(n);
    setIsNew(false);
    setEdName(n.name || "");
    setEdEvent(n.event || "sale.paid");
    setEdTo(n.to_email || "");
    setEdSubject(n.subject || "");
    setEdBody(n.body || "");
    setEdEnabled(n.enabled !== false);
    setEdHasCond(!!n.condition);
    setEdCondField(n.condition?.field_id || "");
    setEdCondOp(n.condition?.op || "equals");
    setEdCondValue(n.condition?.value || "");
  }

  function closeEditor() {
    setEditing(null);
    setIsNew(false);
  }

  async function saveNotification() {
    setMsg("");
    if (!formId) return;
    const payload = {
      name: edName,
      event: edEvent,
      to_email: edTo,
      subject: edSubject,
      body: edBody,
      enabled: edEnabled,
      condition: edHasCond && edCondField ? { field_id: edCondField, op: edCondOp, value: edCondValue } : null,
    };
    const url = isNew
      ? `/api/rest/forms/${formId}/notifications${spaceQuery()}`
      : `/api/rest/forms/${formId}/notifications/${editing?.id}${spaceQuery()}`;
    const r = await fetch(url, {
      method: isNew ? "POST" : "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    if (!r.ok) {
      setMsg(t("fs.notifFail"));
      return;
    }
    setMsg(t("fs.notifSaved"));
    closeEditor();
    void load();
  }

  async function deleteNotification(n: Notification) {
    setMsg("");
    if (!formId) return;
    const r = await fetch(`/api/rest/forms/${formId}/notifications/${n.id}${spaceQuery()}`, {
      method: "DELETE",
    });
    setMsg(r.ok ? t("fs.notifDeleted") : t("fs.notifFail"));
    if (r.ok) {
      if (editing?.id === n.id) closeEditor();
      void load();
    }
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

  // Issue #208: save the form's CHIP feeds. A blank secret_key on an existing
  // feed keeps the stored key server-side.
  async function saveChipFeeds() {
    setMsg("");
    if (!formId) return;
    const r = await fetch(`/api/rest/forms/${formId}/chip-feeds${spaceQuery()}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        feeds: feeds.map((f) => ({
          id: f.id,
          name: f.name,
          brand_id: f.brand_id,
          secret_key: f.secret_key,
          email_fallback: f.email_fallback,
          condition: f.condition || null,
        })),
      }),
    });
    setMsg(r.ok ? t("fs.chipFeedsSaved") : t("fs.chipFeedsFail"));
    if (r.ok) {
      // Clear the secret inputs so a re-save doesn't re-encrypt a stale key.
      setFeeds((prev) => prev.map((f) => ({ ...f, secret_key: "" })));
    }
  }

  function addChipFeed() {
    setFeeds((prev) => [
      ...prev,
      { id: feedUid(), name: "", brand_id: "", secret_key: "", email_fallback: "", condition: null },
    ]);
  }

  function updateChipFeed(id: string, patch: Partial<ChipFeedDraft>) {
    setFeeds((prev) => prev.map((f) => (f.id === id ? { ...f, ...patch } : f)));
  }

  function removeChipFeed(id: string) {
    setFeeds((prev) => prev.filter((f) => f.id !== id));
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

                {/* ── Notifications (issue #193) ── */}
                {tab === "notifications" && (
                  <section className="jf-card p-6">
                    <div className="flex flex-wrap items-center justify-between gap-3">
                      <div>
                        <h2 className="text-base font-semibold">{t("fs.notifications")}</h2>
                        <p className="mt-1 text-sm text-[var(--jf-muted)]">{t("fs.notifHint")}</p>
                      </div>
                      {canWrite && (
                        <button onClick={openNewNotification} className="jf-btn-primary px-3 py-1.5 text-xs">
                          + {t("fs.notifAdd")}
                        </button>
                      )}
                    </div>

                    <div className="mt-4 space-y-3">
                      {notifs.length === 0 && (
                        <p className="text-sm text-[var(--jf-muted)]">{t("fs.notifEmpty")}</p>
                      )}
                      {notifs.map((n) => (
                        <div key={n.id} className="rounded-lg border border-[var(--jf-border)] p-3">
                          <div className="flex flex-wrap items-center justify-between gap-2">
                            <div className="flex flex-wrap items-center gap-2">
                              <span className="text-sm font-medium">{n.name || t("fs.notifName")}</span>
                              {n.is_default && (
                                <span className="rounded-full border border-indigo-200 bg-indigo-50 px-2 py-0.5 text-[10px] font-medium text-indigo-600">
                                  {t("fs.notifDefault")}
                                </span>
                              )}
                              <span className="rounded-full border border-slate-200 bg-slate-50 px-2 py-0.5 text-[10px] text-slate-500">
                                {t(`fs.notifEvent${n.event[0].toUpperCase()}${n.event.slice(1).replace(/\./g, "")}` as import("../../components/i18n").Key)}
                              </span>
                              {!n.enabled && (
                                <span className="text-[10px] text-slate-400">{t("fs.notifEnabled")}: off</span>
                              )}
                            </div>
                            <div className="flex items-center gap-2">
                              <button onClick={() => openEditNotification(n)} disabled={!canWrite} className="jf-btn-ghost px-2 py-1 text-xs">
                                {t("fs.notifEdit")}
                              </button>
                              <button onClick={() => deleteNotification(n)} disabled={!canWrite} className="jf-btn-ghost px-2 py-1 text-xs text-red-500">
                                {t("fs.notifDelete")}
                              </button>
                            </div>
                          </div>
                          <div className="mt-1 text-xs text-[var(--jf-muted)]">
                            {t("fs.notifTo")}: <span className="font-mono">{n.to_email || "—"}</span>
                            {n.condition && (
                              <span className="ml-2">
                                · {n.condition.field_id} {t(`fs.notifOp${n.condition.op[0].toUpperCase()}${n.condition.op.slice(1)}` as import("../../components/i18n").Key)} {n.condition.value}
                              </span>
                            )}
                          </div>
                        </div>
                      ))}
                    </div>

                    {(isNew || editing) && (
                      <div className="mt-5 rounded-lg border border-[var(--jf-border)] p-4">
                        <h3 className="text-sm font-semibold">
                          {isNew ? t("fs.notifAdd") : t("fs.notifEdit")}
                        </h3>
                        <div className="mt-3 space-y-3">
                          <div>
                            <label className={labelCls}>{t("fs.notifName")}</label>
                            <input value={edName} onChange={(e) => setEdName(e.target.value)} className={inputCls} placeholder="Admin Notification" />
                          </div>
                          <div>
                            <label className={labelCls}>{t("fs.notifEvent")}</label>
                            <select value={edEvent} onChange={(e) => setEdEvent(e.target.value)} className={inputCls}>
                              {NOTIF_EVENTS.map((ev) => (
                                <option key={ev} value={ev}>
                                  {t(`fs.notifEvent${ev[0].toUpperCase()}${ev.slice(1).replace(/\./g, "")}` as import("../../components/i18n").Key)}
                                </option>
                              ))}
                            </select>
                          </div>
                          <div>
                            <label className={labelCls}>
                              {t("fs.notifTo")} <span className="text-slate-400">{t("fs.notifToHint")}</span>
                            </label>
                            <input value={edTo} onChange={(e) => setEdTo(e.target.value)} className={inputCls} placeholder="{admin_email}" />
                          </div>
                          <div>
                            <label className={labelCls}>{t("fs.notifSubject")}</label>
                            <input value={edSubject} onChange={(e) => setEdSubject(e.target.value)} className={inputCls} placeholder="Jualan baharu — {{ref_code}}" />
                          </div>
                          <div>
                            <label className={labelCls}>
                              {t("fs.notifBody")} <span className="text-slate-400">{t("fs.notifPlaceholders")}</span>
                            </label>
                            <textarea value={edBody} onChange={(e) => setEdBody(e.target.value)} rows={6} className={`${inputCls} font-mono text-xs`} />
                          </div>
                          <div>
                            <label className="flex items-center gap-2 text-sm text-slate-700">
                              <input type="checkbox" checked={edEnabled} onChange={(e) => setEdEnabled(e.target.checked)} />
                              {t("fs.notifEnabled")}
                            </label>
                          </div>
                          <div>
                            <p className="text-[11px] font-medium uppercase tracking-wider text-[#9ca3af]">{t("fs.notifCondition")}</p>
                            {!edHasCond ? (
                              <button onClick={() => setEdHasCond(true)} className="jf-btn-ghost mt-1 px-2 py-1 text-xs">
                                + {t("fs.notifCondAdd")}
                              </button>
                            ) : (
                              <div className="mt-2 space-y-2">
                                <div className="flex flex-wrap items-end gap-2">
                                  <div>
                                    <label className={labelCls}>{t("fs.notifCondField")}</label>
                                    <input value={edCondField} onChange={(e) => setEdCondField(e.target.value)} className="jf-input mt-1 w-40" placeholder="field_id" />
                                  </div>
                                  <div>
                                    <label className={labelCls}>{t("fs.notifCondOp")}</label>
                                    <select value={edCondOp} onChange={(e) => setEdCondOp(e.target.value)} className="jf-input mt-1 w-40">
                                      {NOTIF_OPS.map((op) => (
                                        <option key={op} value={op}>
                                          {t(`fs.notifOp${op[0].toUpperCase()}${op.slice(1)}` as import("../../components/i18n").Key)}
                                        </option>
                                      ))}
                                    </select>
                                  </div>
                                  {edCondOp !== "not_empty" && (
                                    <div>
                                      <label className={labelCls}>{t("fs.notifCondValue")}</label>
                                      <input value={edCondValue} onChange={(e) => setEdCondValue(e.target.value)} className="jf-input mt-1 w-40" />
                                    </div>
                                  )}
                                  <button onClick={() => setEdHasCond(false)} className="jf-btn-ghost px-2 py-1 text-xs text-slate-500">
                                    {t("fs.notifCondRemove")}
                                  </button>
                                </div>
                              </div>
                            )}
                          </div>
                          <div className="flex items-center gap-2 pt-1">
                            <button onClick={saveNotification} disabled={!canWrite} className="jf-btn-primary">
                              {t("fs.notifSave")}
                            </button>
                            <button onClick={closeEditor} className="jf-btn-ghost text-slate-600">
                              {t("fs.notifCancel")}
                            </button>
                          </div>
                        </div>
                      </div>
                    )}
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

                    {/* Issue #208: multiple per-form CHIP feeds with conditional logic. */}
                    <div className="mt-8 border-t border-[var(--jf-border)] pt-6">
                      <h3 className="text-base font-semibold">{t("fs.chipFeedsTitle")}</h3>
                      <p className="mt-1 text-sm text-[var(--jf-muted)]">{t("fs.chipFeedsHint")}</p>

                      <div className="mt-4 space-y-4">
                        {feeds.length === 0 && (
                          <p className="text-sm text-[var(--jf-muted)]">{t("fs.chipNotConfigured")}</p>
                        )}
                        {feeds.map((f, idx) => (
                          <div key={f.id} className="rounded-md border border-[var(--jf-border)] p-3">
                            <div className="flex flex-wrap items-center justify-between gap-2">
                              <span className="text-[11px] font-medium uppercase tracking-wider text-[#9ca3af]">
                                {t("fs.chipFeedName")} #{idx + 1}
                              </span>
                              <button
                                onClick={() => removeChipFeed(f.id)}
                                disabled={!canWrite}
                                className="text-[11px] text-red-400 hover:text-red-600"
                              >
                                {t("fs.chipFeedRemove")}
                              </button>
                            </div>
                            <div className="mt-2 grid gap-3 sm:grid-cols-2">
                              <div>
                                <label className={labelCls}>{t("fs.chipFeedName")}</label>
                                <input
                                  value={f.name}
                                  onChange={(e) => updateChipFeed(f.id, { name: e.target.value })}
                                  placeholder={t("fs.chipFeedNamePh")}
                                  className={inputCls}
                                />
                              </div>
                              <div>
                                <label className={labelCls}>{t("fs.chipBrandId")}</label>
                                <input
                                  value={f.brand_id}
                                  onChange={(e) => updateChipFeed(f.id, { brand_id: e.target.value })}
                                  placeholder="Brand ID"
                                  className={inputCls}
                                />
                              </div>
                              <div>
                                <label className={labelCls}>
                                  {t("set.secretKey")}{" "}
                                  <span className="text-slate-400">{t("fs.chipSecretKeep")}</span>
                                </label>
                                <input
                                  value={f.secret_key}
                                  onChange={(e) => updateChipFeed(f.id, { secret_key: e.target.value })}
                                  type="text"
                                  placeholder="sk_live_… / sk_test_…"
                                  className={inputCls}
                                />
                              </div>
                              <div>
                                <label className={labelCls}>{t("set.emailFallback")}</label>
                                <input
                                  value={f.email_fallback}
                                  onChange={(e) => updateChipFeed(f.id, { email_fallback: e.target.value })}
                                  type="email"
                                  placeholder="Email fallback (one-time forms)"
                                  className={inputCls}
                                />
                              </div>
                            </div>

                            {/* Conditional logic */}
                            <div className="mt-3">
                              <label className={labelCls}>{t("fs.chipFeedConditional")}</label>
                              <div className="mt-1 flex flex-wrap items-center gap-1">
                                <select
                                  value={f.condition?.field_id || ""}
                                  onChange={(e) =>
                                    updateChipFeed(f.id, {
                                      condition: e.target.value
                                        ? { ...(f.condition || { op: "equals", value: "" }), field_id: e.target.value }
                                        : null,
                                    })
                                  }
                                  className="w-full rounded border border-slate-200 px-2 py-1 text-xs sm:w-44"
                                >
                                  <option value="">{t("fs.chipFeedNoCondition")}</option>
                                  {formBlocks.map((b) => (
                                    <option key={b.id} value={b.id}>
                                      {b.value || b.type} (#{formBlocks.indexOf(b) + 1})
                                    </option>
                                  ))}
                                </select>
                                {f.condition && (
                                  <>
                                    <select
                                      value={f.condition.op}
                                      onChange={(e) =>
                                        updateChipFeed(f.id, { condition: { ...f.condition!, op: e.target.value } })
                                      }
                                      className="w-28 rounded border border-slate-200 px-1 py-1 text-xs"
                                    >
                                      {FEED_OPS.map((op) => (
                                        <option key={op} value={op}>
                                          {op}
                                        </option>
                                      ))}
                                    </select>
                                    <input
                                      value={f.condition.value}
                                      onChange={(e) =>
                                        updateChipFeed(f.id, { condition: { ...f.condition!, value: e.target.value } })
                                      }
                                      placeholder="value"
                                      className="min-w-0 flex-1 rounded border border-slate-200 px-2 py-1 text-xs"
                                    />
                                  </>
                                )}
                              </div>
                            </div>
                          </div>
                        ))}
                      </div>

                      <div className="mt-4 flex flex-wrap items-center gap-2">
                        <button onClick={addChipFeed} disabled={!canWrite} className="jf-btn-ghost px-3 py-1.5 text-xs">
                          + {t("fs.chipFeedAdd")}
                        </button>
                        <button onClick={saveChipFeeds} disabled={!canWrite} className="jf-btn-primary">
                          {t("fs.save")}
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
