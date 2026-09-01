"use client";

import { useCallback, useEffect, useState } from "react";
import { Shell } from "../../components/Shell";
import { useI18n } from "../../components/i18n";

type ChipStatus = { configured: boolean; mode: string };
type KeyRow = { id: string; name: string; scopes: string[] };
type DomainStatus = { domain: string; verified: boolean } | null;

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

export default function Settings() {
  const { t } = useI18n();
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
  const [trackingHead, setTrackingHead] = useState("");
  const [social, setSocial] = useState<{ provider: string; email: string; linked_at: string }[]>([]);

  // Issue #13: single aggregated fetch replaces the 6-request waterfall
  // (chip, keys, domains, webhooks, einvoice, social) on mount.
  const load = useCallback(() => {
    return fetch("/api/rest/dashboard-bundle")
      .then((r) => (r.ok ? r.json() : null))
      .then((b) => {
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
      });
  }, []);

  useEffect(() => {
    load();
  }, [load]);

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
    const r = await fetch("/api/rest/webhooks", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ url: whUrl, secret: whSecret, events: ["sale.paid", "sale.failed", "subscription.charged"] }),
    });
    if (r.ok) { setWhUrl(""); setWhSecret(""); load(); }
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

  return (
    <Shell active="settings">
      <main className="mx-auto max-w-3xl flex-1 px-6 py-8">
        <h1 className="text-2xl font-bold">
          {t("set.title")} <span className="text-[var(--jf-primary)]">JomForm</span>
        </h1>

        {/* ── CHIP gateway ── */}
        <section className="jf-card mt-8 p-6">
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
                {status.configured ? t("set.activeMode", { mode: status.mode.toUpperCase() }) : t("set.notSet")}
              </span>
            )}
          </div>
          <p className="mt-2 text-sm text-slate-400">
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
          {msg && <p className="mt-3 rounded-lg bg-emerald-50 px-3 py-2 text-sm text-emerald-600">{msg}</p>}
          {err && <p className="mt-3 rounded-lg bg-red-50 px-3 py-2 text-sm text-red-600">{err}</p>}
          <form onSubmit={save} className="mt-4 space-y-3">
            <div>
              <label className="text-xs text-slate-500">Brand ID</label>
              <input
                value={brandId}
                onChange={(e) => setBrandId(e.target.value)}
                required
                className="jf-input mt-1"
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
                className="jf-input mt-1 font-mono"
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
                className="jf-input mt-1"
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

        {/* ── custom domains (multi) ── */}
        <section className="jf-card mt-6 p-6">
          <h2 className="text-lg font-semibold">{t("set.domainsTitle")}</h2>
          <p className="mt-2 text-sm text-slate-400">
            {t("set.domainsHelp")}
          </p>
          {domainMsg && <p className="mt-3 rounded-lg bg-indigo-50 px-3 py-2 text-sm text-indigo-700">{domainMsg}</p>}
          <form onSubmit={setDomainClaim} className="mt-4 flex flex-wrap gap-2">
            <input
              value={domainInput}
              onChange={(e) => setDomainInput(e.target.value)}
              placeholder="shop.yourdomain.com"
              className="jf-input min-w-56 flex-1"
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

        {/* ── Tracking scripts ── */}
        <section className="jf-card mt-6 p-6">
          <h2 className="text-lg font-semibold">{t("set.trackingTitle")}</h2>
          <p className="mt-1 text-sm text-slate-400">
            {t("set.trackingHelp")} <code className="rounded bg-slate-100 px-1">&lt;script src="https://…"&gt;</code>, <code className="rounded bg-slate-100 px-1">&lt;meta&gt;</code>, comments. Event handlers & cookie writes stripped.
          </p>
          <textarea value={trackingHead} onChange={(e) => setTrackingHead(e.target.value)} rows={4}
            className="jf-input mt-3 font-mono text-xs"
            placeholder={'<script src="https://www.googletagmanager.com/gtag/js?id=G-XXXX"></-script>'} />
          <button onClick={async () => {
            const r = await fetch("/api/rest/settings/tracking", {
              method: "PUT", headers: { "Content-Type": "application/json" },
              body: JSON.stringify({ head: trackingHead }),
            });
            setMsg(r.ok ? t("set.trackingSaved") : t("prof.failed"));
          }} className="jf-btn-primary mt-3">{t("set.saveTracking")}</button>
        </section>

        {/* ── e-Invoice (LHDN) ── */}
        <section className="jf-card mt-6 p-6">
          <h2 className="text-lg font-semibold">{t("set.einvoiceTitle")}</h2>
          <p className="mt-1 text-sm text-slate-400">
            {t("set.einvoiceHelp")}
          </p>
          <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <label className="text-xs text-slate-500">{t("set.companyTin")}</label>
              <input value={tin} onChange={(e) => setTin(e.target.value)} className="jf-input mt-1" placeholder="e.g. C1234567890" />
            </div>
            <div>
              <label className="text-xs text-slate-500">{t("set.msicCode")}</label>
              <input value={msic} onChange={(e) => setMsic(e.target.value)} className="jf-input mt-1" placeholder="e.g. 62010" />
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

        {/* ── Outbound webhooks ── */}
        <section className="jf-card mt-6 p-6">
          <h2 className="text-lg font-semibold">{t("set.webhooksTitle")}</h2>
          <p className="mt-1 text-sm text-slate-400">
            {t("set.eventsLabel")} <code className="rounded bg-slate-100 px-1">sale.paid</code>,{" "}
            <code className="rounded bg-slate-100 px-1">sale.failed</code>,{" "}
            <code className="rounded bg-slate-100 px-1">subscription.charged</code>. {t("set.hmacSigned")}
          </p>
          <form onSubmit={addWebhook} className="mt-3 flex flex-wrap gap-2">
            <input value={whUrl} onChange={(e) => setWhUrl(e.target.value)} required type="url"
              placeholder="https://yourapp.com/jomform-hook" className="jf-input min-w-56 flex-1" />
            <input value={whSecret} onChange={(e) => setWhSecret(e.target.value)} required type="text"
              placeholder={t("set.signingSecret")} className="jf-input w-48" />
            <button className="jf-btn-primary">{t("set.add")}</button>
          </form>
          <ul className="mt-3 space-y-2">
            {webhooks.map((wh) => (
              <li key={wh.id} className="flex items-center justify-between gap-3 rounded-lg border border-[var(--jf-border)] px-4 py-2.5">
                <div className="min-w-0">
                  <p className="truncate font-mono text-xs">{wh.url}</p>
                  <p className="mt-0.5 text-[11px] text-slate-400">{(wh.events || []).join(", ")}</p>
                </div>
                <button onClick={async () => { await fetch(`/api/rest/webhooks/${wh.id}`, { method: "DELETE" }); load(); }}
                  className="shrink-0 text-xs text-slate-500 hover:text-red-500">✕</button>
              </li>
            ))}
          </ul>
        </section>

        {/* ── MCP / OAuth 2.1 connected agents ── */}
        <section className="jf-card mt-6 p-6">
          <h2 className="text-lg font-semibold">{t("set.mcpTitle")}</h2>
          <p className="mt-1 text-sm text-slate-400">
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

        {/* ── Social login connectors ── */}
        <section className="jf-card mt-6 p-6">
          <h2 className="text-lg font-semibold">{t("set.socialTitle")}</h2>
          <p className="mt-1 text-sm text-slate-400">
            {t("set.socialHelp")}
          </p>
          <ul className="mt-4 space-y-2">
            {social.map((l) => (
              <li key={l.provider} className="flex items-center justify-between rounded-lg border border-[var(--jf-border)] px-4 py-2.5">
                <div>
                  <p className="text-sm capitalize">{l.provider} — {l.email}</p>
                  <p className="text-[11px] text-slate-400">{t("set.linked")} {new Date(l.linked_at).toLocaleDateString()}</p>
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
      </main>
    </Shell>
  );
}