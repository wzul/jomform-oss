"use client";

import { useCallback, useEffect, useState } from "react";
import { Shell } from "../../components/Shell";
import { useRouter } from "next/navigation";
import { useI18n } from "../../components/i18n";
import { useSession } from "../../components/session";

type ChipStatus = { configured: boolean; mode?: string };
type KeyRow = { id: string; name: string; scopes: string[] };
type DomainStatus = { domain: string; verified: boolean } | null;
type SocialRow = { provider: string; email?: string; linked_at?: string };

// Issue #68: Settings page is organised into tabs (same pattern as Profile
// page tabs — #5) instead of one long scroll of 8 stacked sections.
//   chip      -> CHIP gateway
//   domains   -> Custom domains + Cloudflare connect (one workflow)
//   tracking  -> Tracking scripts (global GTM/analytics)
//   einvoice  -> e-Invoice TIN/MSIC
//   webhooks  -> Outbound webhooks + MCP/API section
//   social    -> Social login connectors
//   emel      -> Per-space receipt/notification email templates (issue #107)
type SettingsTab = "chip" | "domains" | "tracking" | "einvoice" | "webhooks" | "social" | "emel";

const TAB_IDS: SettingsTab[] = ["chip", "domains", "tracking", "einvoice", "webhooks", "social", "emel"];

function isSettingsTab(v: string | null): v is SettingsTab {
  return v === "chip" || v === "domains" || v === "tracking" || v === "einvoice" || v === "webhooks" || v === "social" || v === "emel";
}

// Restore the last active tab. Falls back to "chip".
function loadStoredTab(): SettingsTab {
  if (typeof window === "undefined") return "chip";
  try {
    const v = window.localStorage.getItem("jf_settings_tab");
    if (isSettingsTab(v)) return v;
  } catch {
    // storage unavailable — fall through
  }
  return "chip";
}

const METHODS = [
  { id: "fpx", label: "FPX" },
  { id: "fpx_b2b1", label: "FPX B2B1" },
  { id: "card", label: "Card (Visa, Mastercard, Maestro)" },
  { id: "mpgs_google_pay", label: "Google Pay" },
  { id: "mpgs_apple_pay", label: "Apple Pay" },
  { id: "razer_atome", label: "Atome" },
  { id: "razer_grabpay", label: "GrabPay" },
  { id: "razer_maybankqr", label: "Maybank QRPay" },
  { id: "shopee_pay", label: "ShopeePay" },
  { id: "razer_tng", label: "Touch 'n Go eWallet" },
  { id: "duitnow_qr", label: "DuitNow QR" },
  { id: "crypto_coin", label: "Crypto Coin" },
];

// Issue #128: dispatcher-supported webhook events (dispatch2.go + purchases.go).
// An explicit "*" option subscribes to all events.
const WEBHOOK_EVENTS = ["sale.paid", "sale.failed", "subscription.charged"] as const;

export default function Settings() {
  const { t } = useI18n();
  const router = useRouter();
  const [tab, setTab] = useState<SettingsTab>(loadStoredTab);
  const [status, setStatus] = useState<ChipStatus | null>(null);
  const [brandId, setBrandId] = useState("");
  const [secretKey, setSecretKey] = useState("");
  const [mode, setMode] = useState("test");
  const [emailFallback, setEmailFallback] = useState("");
  const [whitelist, setWhitelist] = useState<string[]>([]);
  const [msg, setMsg] = useState("");
  const [err, setErr] = useState("");
  const [keys, setKeys] = useState<KeyRow[]>([]);
  const [newKey, setNewKey] = useState("");
  const [verified, setVerified] = useState(false);

  const [domain, setDomain] = useState("");
  const [domainInput, setDomainInput] = useState("");
  const [domainVerified, setDomainVerified] = useState(false);
  const [domains, setDomains] = useState<{ id: string; domain: string; verified: boolean }[]>([]);
  const [domainMsg, setDomainMsg] = useState("");
  const [tin, setTin] = useState("");
  const [msic, setMsic] = useState("");
  const [webhooks, setWebhooks] = useState<{ id: string; url: string; events: string[] }[]>([]);
  const [whUrl, setWhUrl] = useState("");
  const [whSecret, setWhSecret] = useState("");
  // Issue #128: event selection (add form defaults to all) + inline edit drawer.
  const [whEvents, setWhEvents] = useState<string[]>([...WEBHOOK_EVENTS]);
  const [editId, setEditId] = useState<string | null>(null);
  const [editUrl, setEditUrl] = useState("");
  const [editSecret, setEditSecret] = useState("");
  const [editEvents, setEditEvents] = useState<string[]>([]);
  // Issue #130: per-webhook delivery log — status indicator + expandable history.
  type Delivery = { id: string; event: string; status_code?: number | null; success: boolean; attempt: number; error?: string; created_at?: string };
  const [deliveries, setDeliveries] = useState<Record<string, Delivery[]>>({});
  const [openWh, setOpenWh] = useState<string | null>(null);
  const [trackingHead, setTrackingHead] = useState("");
  const [social, setSocial] = useState<SocialRow[]>([]);
  // Cloudflare OAuth link state (issue #22): Login vs Disconnect+✓ per #9.
  const [cfLinked, setCfLinked] = useState(false);
  const [cfConfigured, setCfConfigured] = useState(false);
  // Cloudflare manual connect (issue #82): API token / global key form.
  const [cfManualOpen, setCfManualOpen] = useState(false);
  const [cfKind, setCfKind] = useState<"apitoken" | "globalkey">("apitoken");
  const [cfAccName, setCfAccName] = useState("");
  const [cfAccEmail, setCfAccEmail] = useState("");
  const [cfSecret, setCfSecret] = useState("");
  const [cfManualBusy, setCfManualBusy] = useState(false);
  const [cfManualErr, setCfManualErr] = useState("");
  // Issue #68: transient banner shown after the Cloudflare OAuth callback
  // redirects back to /settings?cloudflare=linked|error.
  const [cfNotice, setCfNotice] = useState("");
  // Issue #107: per-space email template + behaviour settings.
  const [emelSubject, setEmelSubject] = useState("");
  const [emelBody, setEmelBody] = useState("");
  const [emelReceiptEnabled, setEmelReceiptEnabled] = useState(true);
  const [emelNotifyMerchant, setEmelNotifyMerchant] = useState(false);
  const [emelMerchantEmail, setEmelMerchantEmail] = useState("");
  const [emelHtml, setEmelHtml] = useState(false);
  const [emelSpaceId, setEmelSpaceId] = useState("");
  const [emelPreview, setEmelPreview] = useState(false);

  // Persist the active tab (issue #68).
  useEffect(() => {
    try {
      window.localStorage.setItem("jf_settings_tab", tab);
    } catch {
      // storage unavailable — ignore
    }
  }, [tab]);

  // Issue #68: auto-select the right tab from the URL on mount —
  //  /settings#webhooks, /settings?tab=domains, and the Cloudflare OAuth
  //  callback  /settings?cloudflare=linked|error. Anything not matched keeps
  //  the persisted tab.
  useEffect(() => {
    if (typeof window === "undefined") return;
    const params = new URLSearchParams(window.location.search);
    const hash = window.location.hash.replace("#", "");
    const explicit = params.get("tab");
    const cf = params.get("cloudflare");
    if (cf === "linked") {
      setTab("domains");
      setCfNotice(t("set.cfConnected"));
    } else if (cf === "error") {
      setTab("domains");
      setCfNotice(t("set.cfLinkFailed"));
    } else if (isSettingsTab(explicit)) {
      setTab(explicit);
    } else if (isSettingsTab(hash)) {
      setTab(hash);
    }
  }, [t]);

  // Issue #13: single aggregated fetch replaces the 6-request waterfall
  // (chip, keys, domains, webhooks, einvoice, social) on mount.
  // Issue #29: routed through the shared session cache — the first page of
  // the session fetches the bundle once; later pages (dashboard, forms,
  // products, profile) reuse it. Saves call reloadSession() to re-fetch.
  const { bundle, anon, reload: reloadSession } = useSession();
  const reload = useCallback(() => {
    // Saves mutate bundle-backed sections: drop the shared cache first so
    // the next load() re-fetches fresh data (and other pages inherit it).
    reloadSession();
  }, [reloadSession]);
  const load = useCallback(() => {
    const apply = (b: NonNullable<ReturnType<typeof useSession>["bundle"]>) => {
      if (!b) return;
      const dList = b.domains ?? [];
      const whs = b.webhooks ?? [];
      const k = b.keys ?? [];
      const ei = b.einvoice;
      const s = b.chip_status;
      setDomains(Array.isArray(dList) ? dList : []);
      setWebhooks(Array.isArray(whs) ? whs : []);
      setSocial(Array.isArray(b.social) ? b.social : []);
      if (ei) { setTin(ei.tin || ""); setMsic(ei.msic || ""); }
      if (s) {
        setStatus(s);
        if (s.brand_id) setBrandId(s.brand_id);
        if (s.mode) setMode(s.mode);
        if (s.email_fallback) setEmailFallback(s.email_fallback);
        if (Array.isArray(s.whitelist) && s.whitelist.length) setWhitelist(s.whitelist);
        if (s.secret_key) setSecretKey(s.secret_key);
        setVerified(s.configured && !!(s.secret_key || s.secret_key_masked));
      }
      setKeys(Array.isArray(k) ? k : []);
    };
    if (bundle) { apply(bundle); return Promise.resolve(); }
    // Issue #48: when the bundle resolved as null (anonymous) there is
    // nothing to load here — the anon redirect below bounces to /landing.
    if (bundle === null) return Promise.resolve();
    return fetch("/api/rest/dashboard-bundle")
      .then((r) => (r.ok ? r.json() : null))
      .then(apply);
  }, [bundle]);

  useEffect(() => {
    load();
  }, [load]);

  // Issue #130: populate each webhook's delivery-status indicator from the
  // REST delivery-log endpoint whenever the webhook list changes.
  useEffect(() => {
    webhooks.forEach((wh) => fetchDeliveries(wh.id));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [webhooks]);

  // Issue #107: load the per-space email settings for the active space.
  const { spaces } = useSession();
  const loadEmailSettings = useCallback(() => {
    const sp = Array.isArray(spaces) ? spaces : [];
    if (!sp.length) return;
    // Resolve the active space (same key the Shell uses).
    let slug = "";
    try { slug = window.localStorage.getItem("jf_space") || ""; } catch { /* ignore */ }
    const active = sp.find((x) => x.slug === slug) || sp[0];
    if (!active) return;
    setEmelSpaceId(active.id);
    fetch(`/api/rest/spaces/${active.id}/email-settings`)
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => {
        if (!d) return;
        setEmelSubject(d.receipt_subject || "");
        setEmelBody(d.receipt_body || "");
        setEmelReceiptEnabled(d.receipt_enabled !== false);
        setEmelNotifyMerchant(!!d.notify_merchant);
        setEmelMerchantEmail(d.merchant_notify_email || "");
        setEmelHtml(!!d.html);
      })
      .catch(() => {});
  }, [spaces]);

  useEffect(() => {
    loadEmailSettings();
  }, [loadEmailSettings]);

  // Issue #48: anonymous visit (401 / session expired) bounces to Landing
  // instead of hanging on a shell or flashing a logged-out settings form.
  useEffect(() => {
    if (anon) router.replace("/landing");
  }, [anon, router]);

  const loadCfStatus = useCallback(() => {
    return fetch("/api/cf/status")
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => {
        if (d) {
          setCfLinked(!!d.linked);
          setCfConfigured(d.configured !== false);
        }
      })
      .catch(() => {});
  }, []);

  useEffect(() => {
    loadCfStatus();
  }, [loadCfStatus]);

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setMsg("");
    setErr("");
    const r = await fetch("/api/rest/settings/chip", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        brand_id: brandId,
        secret_key: secretKey,
        mode,
        email_fallback: emailFallback,
        payment_method_whitelist: whitelist,
      }),
    });
    if (r.ok) {
      setMsg(t("set.savedCreds"));
      setVerified(true);
      await load();
    } else {
      const d = await r.json().catch(() => ({}));
      setVerified(false);
      setErr(d.error || t("set.failedSave"));
    }
  }

  async function clearCreds() {
    await fetch("/api/rest/settings/chip", { method: "DELETE" });
    setMsg(t("set.credsCleared"));
    await load();
  }

  async function addWebhook(e: React.FormEvent) {
    e.preventDefault();
    setErr("");
    const r = await fetch("/api/rest/webhooks", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ url: whUrl, secret: whSecret, events: whEvents }),
    });
    if (r.ok) {
      setWhUrl(""); setWhSecret(""); setWhEvents([...WEBHOOK_EVENTS]);
      load();
    } else {
      // Issue #128: show the backend's error inline instead of swallowing it.
      const d = await r.json().catch(() => ({}));
      setErr(t("set.whAddFail", { error: d.error || t("set.failedSave") }));
    }
  }

  // Issue #128: persist edits via the new PUT /v1/rest/webhooks/{id} endpoint.
  async function saveWebhook(e: React.FormEvent) {
    e.preventDefault();
    if (!editId) return;
    setErr("");
    const r = await fetch(`/api/rest/webhooks/${editId}`, {
      method: "PUT", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ url: editUrl, secret: editSecret, events: editEvents }),
    });
    if (r.ok) {
      setMsg(t("set.whUpdated"));
      setEditId(null); setEditUrl(""); setEditSecret(""); setEditEvents([]);
      load();
    } else {
      const d = await r.json().catch(() => ({}));
      setErr(t("set.whUpdateFail", { error: d.error || t("set.failedSave") }));
    }
  }

  function toggleWhEvent(target: string[], setter: (v: string[]) => void, ev: string) {
    setter(target.includes(ev) ? target.filter((x) => x !== ev) : [...target, ev]);
  }

  // Issue #128: open (or close) the inline edit drawer for a webhook row.
  function openEdit(wh: { id: string; url: string; events: string[] }) {
    setEditId(wh.id);
    setEditUrl(wh.url);
    setEditSecret("");
    setEditEvents([...(wh.events && wh.events.length ? wh.events : ["sale.paid"])]);
    setErr("");
  }
  function closeEdit() {
    setEditId(null); setEditUrl(""); setEditSecret(""); setEditEvents([]);
  }

  // Issue #130: fetch the latest deliveries for one webhook via the REST
  // endpoint (account-scoped server-side). Called when a row is expanded or
  // on mount to populate the status indicator.
  async function fetchDeliveries(webhookID: string) {
    try {
      const r = await fetch(`/api/rest/webhooks/${webhookID}/deliveries?limit=10`);
      if (!r.ok) return;
      const list = (await r.json().catch(() => ([]))) as Delivery[];
      setDeliveries((prev) => ({ ...prev, [webhookID]: Array.isArray(list) ? list : [] }));
    } catch {
      // network blip — leave existing state untouched
    }
  }

  // Derived per-webhook status for the indicator: pending (last failed, more
  // retries remain), success (last attempt ok), failed (last attempt failed).
  function deliveryStatus(list: Delivery[] | undefined) {
    const latest = list && list.length ? list[0] : undefined;
    if (!latest) return null; // no deliveries yet
    if (latest.success) return "success";
    if (latest.attempt < 3) return "pending"; // auto-retry is still scheduled
    return "failed";
  }

  async function genKey() {
    setErr("");
    const r = await fetch("/api/rest/keys", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: "AI Agent Key", scopes: ["readwrite"] }),
    });
    if (r.ok) {
      const d = await r.json();
      setNewKey(d.key);
      await load();
    }
  }

  async function setDomainClaim(e: React.FormEvent) {
    e.preventDefault();
    setDomainMsg("");
    const r = await fetch("/api/rest/domains", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ domain: domainInput }),
    });
    if (r.ok) {
      const d = await r.json();
      setDomainInput("");
      setDomainMsg(t("set.domainAdded", { domain: d.domain }));
      await load();
    } else {
      const d = await r.json().catch(() => ({}));
      setDomainMsg(d.error || t("set.failedAddDomain"));
    }
  }

  function toggleMethod(id: string) {
    setWhitelist((prev) => (prev.includes(id) ? prev.filter((m) => m !== id) : [...prev, id]));
  }

  // Issue #107: save the per-space email settings.
  async function saveEmailSettings() {
    if (!emelSpaceId) return;
    setMsg("");
    setErr("");
    const r = await fetch(`/api/rest/spaces/${emelSpaceId}/email-settings`, {
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
    if (r.ok) {
      setMsg(t("set.emelSaved"));
    } else {
      const d = await r.json().catch(() => ({}));
      setErr(d.error || t("set.failedSave"));
    }
  }

  // Issue #107: render a client-side preview of the template with sample data.
  function renderPreview(src: string): string {
    const sample: Record<string, string> = {
      "{{ref_code}}": "JF-ABC123",
      "{{amount}}": "123.45",
      "{{items}}": "1× Tee",
      "{{form_url}}": "https://acme.jomform.com/tee",
      "{{buyer_name}}": "Ali",
      "{{date}}": new Date().toISOString().slice(0, 10),
    };
    let out = src;
    for (const [k, v] of Object.entries(sample)) out = out.split(k).join(v);
    return out;
  }

  return (
    <Shell active="settings">
      <main className="w-full flex-1 px-6 py-8">
        <h1 className="max-w-xl text-2xl font-bold">
          {t("set.title")} <span className="text-[var(--jf-primary)]">JomForm</span>
        </h1>

        {/* Tab bar (issue #68) — same styling as Profile. */}
        <div className="mt-4 flex gap-1 overflow-x-auto border-b border-slate-200">
          {TAB_IDS.map((id) => (
            <button
              key={id}
              onClick={() => setTab(id)}
              aria-selected={tab === id}
              role="tab"
              className={`-mb-px whitespace-nowrap border-b-2 px-3 py-2 text-sm font-medium sm:px-4 ${
                tab === id
                  ? "border-slate-900 text-slate-900"
                  : "border-transparent text-slate-500 hover:text-slate-800"
              }`}
            >
              <span className="sm:hidden">{t(`set.tab.${id}` as const)}</span>
              <span className="hidden sm:inline">{t(`set.tab.${id}` as const)}</span>
            </button>
          ))}
        </div>

        {/* Issue #68: transient Cloudflare OAuth callback banner + shared
            status/error banners shown above tab content regardless of the
            active tab (Profile-style). */}
        {cfNotice && <p className="mt-4 rounded-lg bg-emerald-50 px-3 py-2 text-sm text-emerald-700">{cfNotice}</p>}
        {msg && <p className="mt-4 rounded-lg bg-emerald-50 px-3 py-2 text-sm text-emerald-600">{msg}</p>}
        {err && <p className="mt-4 rounded-lg bg-red-50 px-3 py-2 text-sm text-red-600">{err}</p>}

        {/* ── CHIP tab ── */}
        {tab === "chip" && (
          <section className="jf-card mt-6 p-6">
            <div className="flex items-center justify-between">
              <h2 className="text-lg font-semibold">{t("set.chipTitle")}</h2>
              {status && (
                <span
                  className={`rounded-full px-3 py-1 text-xs font-semibold ${
                    status.configured
                      ? "border border-emerald-200 bg-emerald-50 text-emerald-700"
                      : "border border-slate-200 bg-slate-50 text-slate-500"
                  }`}
                >
                  {status.configured ? t("set.activeMode", { mode: (status.mode || "test").toUpperCase() }) : t("set.notSet")}
                </span>
              )}
            </div>
            <p className="mt-2 max-w-xl text-sm text-slate-400">
              {t("set.getYour")} <code className="rounded bg-slate-100 px-1">Brand ID</code> {t("set.and")}{" "}
              <code className="rounded bg-slate-100 px-1">{t("set.secretKey")}</code> {t("set.from")}{" "}
              <a
                href="https://portal.chip-in.asia/collect/developers/brands"
                className="text-[var(--jf-primary)] underline"
                target="_blank"
              >
                CHIP Merchant Portal → Developers
              </a>
              . {t("set.keysEncrypted")}
            </p>
            <form onSubmit={save} className="mt-4 space-y-3">
              <div>
                <label className="text-xs text-slate-500">Brand ID</label>
                <input
                  value={brandId}
                  onChange={(e) => setBrandId(e.target.value)}
                  required
                  className="jf-input mt-1 max-w-xl"
                  placeholder="e.g. 5f2b…-uuid"
                />
              </div>
              <div>
                <label className="flex items-center gap-2 text-xs text-slate-500">
                  {t("set.secretKey")}
                  {verified && (
                    <span className="rounded-full border border-emerald-200 bg-emerald-50 px-2 py-0.5 text-[10px] font-semibold text-emerald-700">
                      {t("set.apiVerified")}
                    </span>
                  )}
                </label>
                <input
                  value={secretKey}
                  onChange={(e) => setSecretKey(e.target.value)}
                  required
                  type="text"
                  className="jf-input mt-1 max-w-xl font-mono"
                  placeholder="sk_live_… / sk_test_…"
                />
              </div>
              <div>
                <label className="text-xs text-slate-500">{t("set.mode")}</label>
                <p className="mt-1 text-sm">{mode === "live" ? t("set.live") : t("set.test")}</p>
              </div>

              <div>
                <label className="text-xs text-slate-500">
                  {t("set.emailFallback")} <span className="text-slate-400">{t("set.emailFallbackHint")}</span>
                </label>
                <input
                  value={emailFallback}
                  onChange={(e) => setEmailFallback(e.target.value)}
                  type="email"
                  className="jf-input mt-1 max-w-xl"
                  placeholder="store@yourshop.my"
                />
              </div>

              <div>
                <label className="text-xs text-slate-500">
                  {t("set.paymentMethods")} <span className="text-slate-400">{t("set.allMethods")}</span>
                </label>
                <div className="mt-1.5 flex flex-wrap gap-1.5">
                  {METHODS.map((m) => (
                    <button
                      key={m.id}
                      type="button"
                      onClick={() => toggleMethod(m.id)}
                      className={`rounded-full border px-3 py-1 text-xs font-medium transition-colors ${
                        whitelist.includes(m.id)
                          ? "border-[var(--jf-primary)] bg-indigo-50 text-[var(--jf-primary)]"
                          : "border-slate-200 bg-white text-slate-500 hover:border-slate-300"
                      }`}
                    >
                      {m.label}
                    </button>
                  ))}
                </div>
              </div>

              <div className="flex gap-2 pt-1">
                <button className="jf-btn-primary">{t("set.save")}</button>
                {status?.configured && (
                  <button type="button" onClick={clearCreds} className="jf-btn-ghost text-slate-600">
                    {t("set.clear")}
                  </button>
                )}
              </div>
            </form>
          </section>
        )}

        {/* ── Domains tab: custom domains + Cloudflare connect ── */}
        {tab === "domains" && (
          <>
            <section className="jf-card mt-6 p-6">
              <h2 className="text-lg font-semibold">{t("set.domainsTitle")}</h2>
              <p className="mt-2 max-w-xl text-sm text-slate-400">
                {t("set.domainsHelp")}
              </p>
              {domainMsg && <p className="mt-3 rounded-lg bg-indigo-50 px-3 py-2 text-sm text-indigo-700">{domainMsg}</p>}
              <form onSubmit={setDomainClaim} className="mt-4 flex flex-wrap gap-2">
                <input
                  value={domainInput}
                  onChange={(e) => setDomainInput(e.target.value)}
                  placeholder="shop.yourdomain.com"
                  className="jf-input min-w-56 max-w-xl flex-1"
                />
                <button className="jf-btn-primary" type="submit">{t("set.addDomain")}</button>
              </form>
              <ul className="mt-4 space-y-2">
                {domains.map((d: { id: string; domain: string; verified: boolean }) => (
                  <li key={d.id} className="flex items-center justify-between gap-3 rounded-lg border border-[var(--jf-border)] px-4 py-2.5">
                    <div className="min-w-0">
                      <p className="truncate text-sm font-medium">{d.domain}</p>
                      <p className={`text-[11px] ${d.verified ? "text-emerald-600" : "text-amber-600"}`}>
                        {d.verified ? t("set.verified") : t("set.pendingDns")}
                      </p>
                    </div>
                    <div className="flex shrink-0 items-center gap-2">
                      {!d.verified && (
                        <button
                          onClick={async () => {
                            const r = await fetch(`/api/rest/domains/${d.id}/verify`, { method: "POST" });
                            setDomainMsg(r.ok ? t("set.domainVerified", { domain: d.domain }) : t("set.verifyFailed", { domain: d.domain }));
                            load();
                          }}
                          className="text-xs text-[#5e6ad2] hover:underline"
                        >{t("set.verify")}</button>
                      )}
                      <button
                        onClick={async () => { await fetch(`/api/rest/domains/${d.id}`, { method: "DELETE" }); load(); }}
                        className="text-xs text-slate-500 hover:text-red-500"
                      >{t("set.remove")}</button>
                    </div>
                  </li>
                ))}
              </ul>
            </section>

            {/* Cloudflare connect (auto CNAME) — issue #22 + manual #82 */}
            <section className="jf-card mt-6 p-6">
              <div className="flex items-center justify-between">
                <h2 className="text-lg font-semibold">{t("set.cfTitle")}</h2>
                {cfLinked && (
                  <span className="rounded-full border border-emerald-200 bg-emerald-50 px-3 py-1 text-xs font-semibold text-emerald-700">
                    {t("set.cfConnected")}
                  </span>
                )}
              </div>
              <p className="mt-2 text-sm text-slate-400">
                {t("set.cfHelp")}
              </p>
              <div className="mt-3 flex gap-2">
                {!cfLinked && (
                  <a href="/api/cf/start" className="jf-btn-primary inline-block">
                    {t("set.cfLogin")}
                  </a>
                )}
                {cfLinked && (
                  <button
                    onClick={async () => {
                      await fetch("/api/cf/disconnect", { method: "POST" });
                      setCfLinked(false);
                    }}
                    className="jf-btn-ghost text-slate-600"
                  >
                    {t("set.cfDisconnect")}
                  </button>
                )}
                {!cfLinked && (
                  <button
                    onClick={() => { setCfManualOpen(!cfManualOpen); setCfManualErr(""); }}
                    className="jf-btn-ghost text-slate-600"
                  >
                    {t("set.cfManualToggle")}
                  </button>
                )}
              </div>
              {!cfLinked && cfManualOpen && (
                <div className="mt-4 rounded-lg border border-slate-200 bg-slate-50 p-4">
                  <p className="text-xs text-slate-500">
                    {t("set.cfManualHelp")}{" "}
                    <a
                      href="https://dash.cloudflare.com/profile/api-tokens"
                      target="_blank"
                      rel="noreferrer"
                      className="text-sky-600 underline"
                    >
                      {t("set.cfApiTokensLink")}
                    </a>
                  </p>
                  <div className="mt-3 flex gap-2">
                    {(["apitoken", "globalkey"] as const).map((k) => (
                      <label key={k} className="flex items-center gap-1 text-sm text-slate-700">
                        <input
                          type="radio"
                          name="cfKind"
                          checked={cfKind === k}
                          onChange={() => { setCfKind(k); setCfManualErr(""); }}
                        />
                        {k === "apitoken" ? t("set.cfKindToken") : t("set.cfKindGlobal")}
                      </label>
                    ))}
                  </div>
                  <div className="mt-3 grid gap-2">
                    <input
                      value={cfAccName}
                      onChange={(e) => setCfAccName(e.target.value)}
                      placeholder={t("set.cfAccountName")}
                      className="jf-input text-sm"
                    />
                    <input
                      value={cfAccEmail}
                      onChange={(e) => setCfAccEmail(e.target.value)}
                      placeholder={t("set.cfAccountEmail")}
                      type="email"
                      className="jf-input text-sm"
                    />
                    <input
                      value={cfSecret}
                      onChange={(e) => setCfSecret(e.target.value)}
                      placeholder={cfKind === "apitoken" ? t("set.cfApiToken") : t("set.cfGlobalKey")}
                      type="password"
                      autoComplete="off"
                      className="jf-input text-sm"
                    />
                  </div>
                  {cfManualErr && (
                    <p className="mt-2 text-xs text-red-600">{t("set.cfManualErr")}{cfManualErr}</p>
                  )}
                  <button
                    disabled={cfManualBusy}
                    onClick={async () => {
                      setCfManualBusy(true);
                      setCfManualErr("");
                      try {
                        const body = cfKind === "apitoken"
                          ? { account_email: cfAccEmail, api_token: cfSecret, account_name: cfAccName }
                          : { account_email: cfAccEmail, api_key: cfSecret, account_name: cfAccName };
                        const r = await fetch(
                          cfKind === "apitoken" ? "/api/cf/api-token" : "/api/cf/global-key",
                          { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }
                        );
                        const d = await r.json().catch(() => ({}));
                        if (!r.ok) {
                          setCfManualErr(d.error || `HTTP ${r.status}`);
                          return;
                        }
                        setCfLinked(true);
                        setCfManualOpen(false);
                        setCfAccName(""); setCfAccEmail(""); setCfSecret("");
                      } finally {
                        setCfManualBusy(false);
                      }
                    }}
                    className="jf-btn-primary mt-3 inline-block"
                  >
                    {cfManualBusy ? t("set.cfConnecting") : t("set.cfConnect")}
                  </button>
                </div>
              )}
              {!cfConfigured && (
                <p className="mt-2 text-xs text-slate-400">{t("set.cfNotConfigured")}</p>
              )}
            </section>
          </>
        )}

        {/* ── Tracking tab: global tracking scripts (GTM/analytics) ── */}
        {tab === "tracking" && (
          <section className="jf-card mt-6 p-6">
            <h2 className="text-lg font-semibold">{t("set.trackingTitle")}</h2>
            <p className="mt-1 max-w-xl text-sm text-slate-400">
              {t("set.trackingHelp")} <code className="rounded bg-slate-100 px-1">&lt;script src="https://…"&gt;</code>, <code className="rounded bg-slate-100 px-1">&lt;meta&gt;</code>, comments. Event handlers & cookie writes stripped.
            </p>
            <textarea value={trackingHead} onChange={(e) => setTrackingHead(e.target.value)} rows={4}
              className="jf-input mt-3 max-w-xl font-mono text-xs"
              placeholder={'<script src="https://www.googletagmanager.com/gtag/js?id=G-XXXX"></-script>'} />
            <button onClick={async () => {
              const r = await fetch("/api/rest/settings/tracking", {
                method: "PUT", headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ head: trackingHead }),
              });
              setMsg(r.ok ? t("set.trackingSaved") : t("prof.failed"));
            }} className="jf-btn-primary mt-3">{t("set.saveTracking")}</button>
          </section>
        )}

        {/* ── e-Invoice tab: e-Invoice (LHDN) TIN/MSIC ── */}
        {tab === "einvoice" && (
          <section className="jf-card mt-6 p-6">
            <h2 className="text-lg font-semibold">{t("set.einvoiceTitle")}</h2>
            <p className="mt-1 max-w-xl text-sm text-slate-400">
              {t("set.einvoiceHelp")}
            </p>
            <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div>
                <label className="text-xs text-slate-500">{t("set.companyTin")}</label>
                <input value={tin} onChange={(e) => setTin(e.target.value)} className="jf-input mt-1 max-w-xl" placeholder="e.g. C1234567890" />
              </div>
              <div>
                <label className="text-xs text-slate-500">{t("set.msicCode")}</label>
                <input value={msic} onChange={(e) => setMsic(e.target.value)} className="jf-input mt-1 max-w-xl" placeholder="e.g. 62010" />
              </div>
            </div>
            <button onClick={async () => {
              const r = await fetch("/api/rest/settings/einvoice", {
                method: "PUT", headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ tin, msic }),
              });
              setMsg(r.ok ? t("set.einvoiceSaved") : t("set.failedSave"));
            }} className="jf-btn-primary mt-3">{t("set.saveEinvoice")}</button>
          </section>
        )}

        {/* ── Webhooks tab: outbound webhooks + MCP/API section ── */}
        {tab === "webhooks" && (
          <>
            <section className="jf-card mt-6 p-6">
              <h2 className="text-lg font-semibold">{t("set.webhooksTitle")}</h2>
              <p className="mt-1 max-w-xl text-sm text-slate-400">
                {t("set.eventsLabel")} <code className="rounded bg-slate-100 px-1">sale.paid</code>,{" "}
                <code className="rounded bg-slate-100 px-1">sale.failed</code>,{" "}
                <code className="rounded bg-slate-100 px-1">subscription.charged</code>. {t("set.hmacSigned")}
              </p>
              <form onSubmit={addWebhook} className="mt-3">
                <div className="flex flex-wrap gap-2">
                  <input value={whUrl} onChange={(e) => setWhUrl(e.target.value)} required type="url"
                    placeholder="https://yourapp.com/jomform-hook" className="jf-input min-w-56 max-w-xl flex-1" />
                  <input value={whSecret} onChange={(e) => setWhSecret(e.target.value)} required type="text"
                    placeholder={t("set.signingSecret")} className="jf-input w-48" />
                  <button className="jf-btn-primary">{t("set.add")}</button>
                </div>
                {/* Issue #128: event selection checkboxes (default all). */}
                <div className="mt-2">
                  <p className="text-xs text-slate-500">{t("webhook.eventsTitle")}</p>
                  <div className="mt-1 flex flex-wrap items-center gap-3">
                    {WEBHOOK_EVENTS.map((ev) => (
                      <label key={ev} className="flex items-center gap-1 text-sm text-slate-700">
                        <input
                          type="checkbox"
                          checked={whEvents.includes(ev)}
                          onChange={() => toggleWhEvent(whEvents, setWhEvents, ev)}
                        />
                        {ev}
                      </label>
                    ))}
                    <label className="flex items-center gap-1 text-sm text-slate-400">
                      <input
                        type="checkbox"
                        checked={whEvents.includes("*")}
                        onChange={() => toggleWhEvent(whEvents, setWhEvents, "*")}
                      />
                      {t("webhook.event.all")}
                    </label>
                  </div>
                </div>
              </form>
              <ul className="mt-3 space-y-2">
                {webhooks.map((wh) => {
                  const deliv = deliveries[wh.id];
                  const status = deliveryStatus(deliv);
                  const open = openWh === wh.id;
                  const last = deliv && deliv.length ? deliv[0] : undefined;
                  return (
                    <li key={wh.id} className="rounded-lg border border-[var(--jf-border)]">
                      <div className="flex items-center justify-between gap-3 px-4 py-2.5">
                        <div className="min-w-0">
                          <p className="truncate font-mono text-xs">{wh.url}</p>
                          <p className="mt-0.5 text-[11px] text-slate-400">{(wh.events || []).join(", ")}</p>
                        </div>
                        <div className="flex shrink-0 items-center gap-3">
                          {/* Issue #130: last-delivery status indicator. */}
                          {last && (
                            <span className="flex items-center gap-1.5 text-[11px] font-medium"
                              title={t("wh.lastAttempt") + ": " + new Date(last.created_at || "").toLocaleString()}>
                              <span className={`inline-block h-2 w-2 rounded-full ${
                                status === "success" ? "bg-emerald-500"
                                : status === "pending" ? "bg-amber-400"
                                : "bg-red-500"
                              }`} />
                              {status === "success" && t("wh.success")}
                              {status === "pending" && t("wh.pending")}
                              {status === "failed" && t("wh.failed")}
                              <span className="text-slate-400">· {t("wh.attemptCount", { n: last.attempt })}</span>
                            </span>
                          )}
                          <button onClick={() => openEdit(wh)}
                            className="shrink-0 text-xs text-slate-500 hover:text-slate-800">{t("set.webhookEdit")}</button>
                          <button onClick={() => {
                            const next = open ? null : wh.id;
                            setOpenWh(next);
                            if (next) fetchDeliveries(wh.id);
                          }} className="shrink-0 text-xs text-slate-500 hover:text-slate-800">
                            {open ? t("wh.hideHistory") : t("wh.showHistory")}
                          </button>
                          <button onClick={async () => { await fetch(`/api/rest/webhooks/${wh.id}`, { method: "DELETE" }); load(); }}
                            className="shrink-0 text-xs text-slate-500 hover:text-red-500">✕</button>
                        </div>
                      </div>
                      {/* Issue #130: expandable delivery history for this webhook. */}
                      {open && (
                        <div className="border-t border-[var(--jf-border)] px-4 py-3">
                          <h4 className="mb-2 text-xs font-semibold text-slate-700">{t("wh.history")}</h4>
                          {(!deliv || deliv.length === 0) ? (
                            <p className="text-xs text-slate-400">{t("wh.noDeliveries")}</p>
                          ) : (
                            <div className="overflow-x-auto">
                              <table className="w-full min-w-96 text-left text-xs">
                                <thead>
                                  <tr className="text-slate-400">
                                    <th className="py-1 pr-3 font-medium">{t("wh.colTime")}</th>
                                    <th className="py-1 pr-3 font-medium">{t("wh.colEvent")}</th>
                                    <th className="py-1 pr-3 font-medium">{t("wh.colStatus")}</th>
                                    <th className="py-1 pr-3 font-medium">HTTP</th>
                                    <th className="py-1 font-medium">{t("wh.colError")}</th>
                                  </tr>
                                </thead>
                                <tbody>
                                  {deliv.map((d) => (
                                    <tr key={d.id} className={`border-t border-[var(--jf-border)] ${!d.success ? "bg-red-50/60" : ""}`}>
                                      <td className="py-1.5 pr-3 text-slate-500 whitespace-nowrap">
                                        {d.created_at ? new Date(d.created_at).toLocaleString() : "—"}
                                      </td>
                                      <td className="py-1.5 pr-3 font-mono text-slate-700">{d.event}</td>
                                      <td className={`py-1.5 pr-3 font-medium ${d.success ? "text-emerald-600" : "text-red-600"}`}>
                                        {d.success ? t("wh.success") : t("wh.failed")}
                                      </td>
                                      <td className="py-1.5 pr-3 font-mono text-slate-500">{d.status_code ?? "—"}</td>
                                      <td className="py-1.5 font-mono text-red-600">{d.error || "—"}</td>
                                    </tr>
                                  ))}
                                </tbody>
                              </table>
                            </div>
                          )}
                          <p className="mt-2 text-[11px] text-slate-400">{t("wh.retryNote")}</p>
                        </div>
                      )}
                    </li>
                  );
                })}
              </ul>
              {/* Issue #128: inline edit drawer for an existing webhook. */}
              {editId !== null && (
                <form onSubmit={saveWebhook} className="mt-4 rounded-lg border border-slate-200 bg-slate-50 p-4">
                  <h3 className="text-sm font-semibold text-slate-700">{t("set.webhookEditTitle")}</h3>
                  <div className="mt-2 flex flex-wrap gap-2">
                    <input value={editUrl} onChange={(e) => setEditUrl(e.target.value)} required type="url"
                      placeholder="https://yourapp.com/jomform-hook" className="jf-input min-w-56 max-w-xl flex-1" />
                    <input value={editSecret} onChange={(e) => setEditSecret(e.target.value)} type="text"
                      placeholder={t("set.secretKeep")} className="jf-input w-48" />
                  </div>
                  <div className="mt-2">
                    <p className="text-xs text-slate-500">{t("webhook.eventsTitle")}</p>
                    <div className="mt-1 flex flex-wrap items-center gap-3">
                      {WEBHOOK_EVENTS.map((ev) => (
                        <label key={ev} className="flex items-center gap-1 text-sm text-slate-700">
                          <input
                            type="checkbox"
                            checked={editEvents.includes(ev)}
                            onChange={() => toggleWhEvent(editEvents, setEditEvents, ev)}
                          />
                          {ev}
                        </label>
                      ))}
                      <label className="flex items-center gap-1 text-sm text-slate-400">
                        <input
                          type="checkbox"
                          checked={editEvents.includes("*")}
                          onChange={() => toggleWhEvent(editEvents, setEditEvents, "*")}
                        />
                        {t("webhook.event.all")}
                      </label>
                    </div>
                  </div>
                  <div className="mt-3 flex gap-2">
                    <button className="jf-btn-primary">{t("set.whSave")}</button>
                    <button type="button" onClick={closeEdit} className="jf-btn-ghost text-slate-600">{t("set.whCancel")}</button>
                  </div>
                </form>
              )}
            </section>

            {/* MCP / OAuth 2.1 connected agents */}
            <section className="jf-card mt-6 p-6">
              <h2 className="text-lg font-semibold">{t("set.mcpTitle")}</h2>
              <p className="mt-1 max-w-xl text-sm text-slate-400">
                {t("set.serverUrl")} <code className="rounded bg-slate-100 px-1">https://mcp.jomform.com/mcp</code> —
                {t("set.mcpHelp")}{" "}
                <code className="rounded bg-slate-100 px-1">/.well-known/oauth-authorization-server</code>{" "}
                {t("set.mcpNoKeys")}
              </p>
              <ul className="mt-4 space-y-2">
                {keys.map((k) => (
                  <li key={k.id} className="flex items-center justify-between rounded-lg border border-[var(--jf-border)] px-4 py-2.5">
                    <div>
                      <p className="text-sm">{k.name}</p>
                      <p className="text-[11px] text-slate-400">{t("set.clientId")} {k.name === "AI Agent Key" ? "(oauth-managed)" : k.name}</p>
                    </div>
                    <button onClick={async () => { await fetch(`/api/rest/keys/${k.id}`, { method: "DELETE" }); load(); }}
                      className="text-xs text-slate-500 hover:text-red-500">{t("set.revoke")}</button>
                  </li>
                ))}
                {!keys.length && (
                  <li className="rounded-lg border border-dashed border-[var(--jf-border)] px-4 py-3 text-sm text-slate-400">
                    {t("set.noAgents")}
                  </li>
                )}
              </ul>
            </section>
          </>
        )}

        {/* ── Social tab: login connectors ── */}
        {tab === "social" && (
          <section className="jf-card mt-6 p-6">
            <h2 className="text-lg font-semibold">{t("set.socialTitle")}</h2>
            <p className="mt-1 max-w-xl text-sm text-slate-400">
              {t("set.socialHelp")}
            </p>
            <ul className="mt-4 space-y-2">
              {social.map((l) => (
                <li key={l.provider} className="flex items-center justify-between rounded-lg border border-[var(--jf-border)] px-4 py-2.5">
                  <div>
                    <p className="text-sm capitalize">{l.provider} — {l.email}</p>
                    <p className="text-[11px] text-slate-400">{t("set.linked")} {l.linked_at ? new Date(l.linked_at).toLocaleDateString() : ""}</p>
                  </div>
                  <button onClick={async () => { await fetch(`/api/auth/social/${l.provider}`, { method: "DELETE" }); load(); }}
                    className="text-xs text-slate-500 hover:text-red-500">{t("set.unlink")}</button>
                </li>
              ))}
              {social.length === 0 && (
                <li className="text-sm text-slate-400">{t("set.noLinked")}</li>
              )}
            </ul>
            <a href="/api/auth/oauth/google" className="jf-btn-ghost mt-3 inline-block">
              {t("set.linkGoogle")}
            </a>
          </section>
        )}
        {/* ── Emel tab: per-space receipt/notification email (issue #107) ── */}
        {tab === "emel" && (
          <section className="jf-card mt-6 p-6">
            <h2 className="text-lg font-semibold">{t("set.emelTitle")}</h2>
            <p className="mt-1 max-w-xl text-sm text-slate-400">
              {t("set.emelHelp")}
            </p>
            <div className="mt-4 space-y-3">
              <div>
                <label className="text-xs text-slate-500">{t("set.emelSubject")}</label>
                <input
                  value={emelSubject}
                  onChange={(e) => setEmelSubject(e.target.value)}
                  className="jf-input mt-1 max-w-xl"
                  placeholder="Resit pembayaran — {{ref_code}}"
                />
              </div>
              <div>
                <label className="text-xs text-slate-500">
                  {t("set.emelBody")} <span className="text-slate-400">{t("set.emelPlaceholders")} <code className="rounded bg-slate-100 px-1">{"{{ref_code}}"}</code> <code className="rounded bg-slate-100 px-1">{"{{amount}}"}</code> <code className="rounded bg-slate-100 px-1">{"{{items}}"}</code> <code className="rounded bg-slate-100 px-1">{"{{form_url}}"}</code> <code className="rounded bg-slate-100 px-1">{"{{buyer_name}}"}</code> <code className="rounded bg-slate-100 px-1">{"{{date}}"}</code></span>
                </label>
                <textarea
                  value={emelBody}
                  onChange={(e) => setEmelBody(e.target.value)}
                  rows={8}
                  className="jf-input mt-1 max-w-xl font-mono text-xs"
                />
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
                  <label className="text-xs text-slate-500">{t("set.emelMerchantEmail")}</label>
                  <input
                    value={emelMerchantEmail}
                    onChange={(e) => setEmelMerchantEmail(e.target.value)}
                    type="email"
                    className="jf-input mt-1 max-w-xl"
                    placeholder="store@yourshop.my"
                  />
                </div>
              )}
              <div className="flex flex-wrap items-center gap-2 pt-1">
                <button onClick={saveEmailSettings} className="jf-btn-primary">{t("set.emelSave")}</button>
                <button type="button" onClick={() => setEmelPreview(!emelPreview)} className="jf-btn-ghost text-slate-600">
                  {t("set.emelPreview")}
                </button>
              </div>
              {emelPreview && (
                <div className="mt-3 rounded-lg border border-slate-200 bg-slate-50 p-4">
                  <p className="text-xs text-slate-400">{t("set.emelPreviewHint")}</p>
                  <p className="mt-2 text-sm font-medium">{renderPreview(emelSubject)}</p>
                  <pre className="mt-2 whitespace-pre-wrap rounded bg-white p-3 text-xs text-slate-700">{renderPreview(emelBody)}</pre>
                </div>
              )}
            </div>
          </section>
        )}
      </main>
    </Shell>
  );
}
