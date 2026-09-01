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
  const [domainMsg, setDomainMsg] = useState("");
  const [tin, setTin] = useState("");
  const [msic, setMsic] = useState("");
  const [webhooks, setWebhooks] = useState<{ id: string; url: string; events: string[] }[]>([]);
  const [whUrl, setWhUrl] = useState("");
  const [whSecret, setWhSecret] = useState("");
  const [trackingHead, setTrackingHead] = useState("");
  const [social, setSocial] = useState<{ provider: string; email: string; linked_at: string }[]>([]);

  const load = useCallback(() => {
    return Promise.all([
      fetch("/api/rest/settings/chip").then((r) => (r.ok ? r.json() : null)),
      fetch("/api/rest/keys").then((r) => (r.ok ? r.json() : [])),
      fetch("/api/rest/settings/custom-domain").then((r) => (r.ok ? r.json() : null)),
      fetch("/api/rest/webhooks").then((r) => (r.ok ? r.json() : [])),
      fetch("/api/rest/settings/einvoice").then((r) => (r.ok ? r.json() : null)),
      fetch("/api/auth/social").then((r) => (r.ok ? r.json() : [])),
    ]).then(([s, k, d, whs, ei, so]) => {
      setSocial(Array.isArray(so) ? so : []);
      setWebhooks(Array.isArray(whs) ? whs : []);
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
      setKeys(k || []);
      if (d && d.domain) {
        setDomain(d.domain);
        setDomainVerified(!!d.verified);
      }
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
      setMsg("Saved — credentials verified with CHIP. ✓");
      setVerified(true);
      await load();
    } else {
      const d = await r.json().catch(() => ({}));
      setVerified(false);
      setErr(d.error || "Failed to save");
    }
  }

  async function clearCreds() {
    await fetch("/api/rest/settings/chip", { method: "DELETE" });
    setMsg("CHIP credentials cleared.");
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
    const r = await fetch("/api/rest/settings/custom-domain", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ domain: domainInput }),
    });
    if (r.ok) {
      const d = await r.json();
      setMsg(`Custom domain "${d.domain}" saved. Add a CNAME to jomform.com, then verify.`);
      await load();
    } else {
      const d = await r.json().catch(() => ({}));
      setDomainMsg(d.error || "Failed to save domain");
    }
  }

  async function verifyDomain() {
    setDomainMsg("");
    const r = await fetch("/api/rest/settings/custom-domain/verify", { method: "POST" });
    const d = await r.json().catch(() => ({}));
    if (d.verified) {
      setDomainVerified(true);
      setDomainMsg(`Verified! Your form links now work on https://${d.domain}/f/<slug>`);
    } else {
      setDomainMsg(d.detail || "Not verified yet — check the CNAME records.");
    }
    await load();
  }


  async function clearDomain() {
    await fetch("/api/rest/settings/custom-domain", { method: "DELETE" });
    setDomain("");
    setDomainVerified(false);
    setDomainMsg("Custom domain removed.");
    await load();
  }

  function toggleMethod(id: string) {
    setWhitelist((prev) => (prev.includes(id) ? prev.filter((m) => m !== id) : [...prev, id]));
  }

  return (
    <Shell active="settings">
      <main className="mx-auto max-w-3xl flex-1 px-6 py-8">
        <h1 className="text-2xl font-bold">
          Settings <span className="text-[var(--jf-primary)]">JomForm</span>
        </h1>

        {/* ── CHIP gateway ── */}
        <section className="jf-card mt-8 p-6">
          <div className="flex items-center justify-between">
            <h2 className="text-lg font-semibold">CHIP Payment Gateway</h2>
            {status && (
              <span
                className={`rounded-full px-3 py-1 text-xs font-semibold ${
                  status.configured
                    ? "border border-emerald-200 bg-emerald-50 text-emerald-700"
                    : "border border-slate-200 bg-slate-50 text-slate-500"
                }`}
              >
                {status.configured ? `ACTIVE · ${status.mode.toUpperCase()}` : "NOT SET"}
              </span>
            )}
          </div>
          <p className="mt-2 text-sm text-slate-400">
            Get your <code className="rounded bg-slate-100 px-1">Brand ID</code> &{" "}
            <code className="rounded bg-slate-100 px-1">Secret Key</code> from{" "}
            <a
              href="https://portal.chip-in.asia/collect/developers/brands"
              className="text-[var(--jf-primary)] underline"
              target="_blank"
            >
              CHIP Merchant Portal → Developers
            </a>
            . Keys are stored encrypted — never shown again.
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
                Secret Key
                {verified && (
                  <span className="rounded-full border border-emerald-200 bg-emerald-50 px-2 py-0.5 text-[10px] font-semibold text-emerald-700">
                    ✓ API credentials verified
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
              <label className="text-xs text-slate-500">Mode</label>
              <p className="mt-1 text-sm">{mode === "live" ? "Live" : "Test"}</p>
            </div>

            <div>
              <label className="text-xs text-slate-500">
                Email fallback <span className="text-slate-400">(optional — used when buyer email missing; one-time forms only)</span>
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
                Payment methods <span className="text-slate-400">(blank = all available)</span>
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
              <button className="jf-btn-primary px-5 text-sm">Save</button>
              {status?.configured && (
                <button type="button" onClick={clearCreds} className="jf-btn-ghost px-4 text-sm text-slate-600">
                  Clear
                </button>
              )}
            </div>
          </form>
        </section>

        {/* ── custom domain ── */}
        <section className="jf-card mt-6 p-6">
          <div className="flex items-center justify-between">
            <h2 className="text-lg font-semibold">Custom domain</h2>
            {domain && (
              <span
                className={`rounded-full px-3 py-1 text-xs font-semibold ${
                  domainVerified
                    ? "border border-emerald-200 bg-emerald-50 text-emerald-700"
                    : "border border-amber-200 bg-amber-50 text-amber-700"
                }`}
              >
                {domainVerified ? "VERIFIED" : "PENDING DNS"}
              </span>
            )}
          </div>
          <p className="mt-2 text-sm text-slate-400">
            Serve your forms on your own domain. Add a{" "}
            <code className="rounded bg-slate-100 px-1">CNAME {domain || "<your-domain>"} → jomform.com</code>{" "}
            (DNS only), then verify.
          </p>
          {domainMsg && <p className="mt-3 rounded-lg bg-indigo-50 px-3 py-2 text-sm text-indigo-700">{domainMsg}</p>}
          <form onSubmit={setDomainClaim} className="mt-4 flex flex-wrap gap-2">
            <input
              value={domainInput}
              onChange={(e) => setDomainInput(e.target.value)}
              placeholder="shop.yourdomain.com"
              className="jf-input min-w-56 flex-1"
            />
            <button className="jf-btn-primary px-5 text-sm" type="submit">
              {domain ? "Replace" : "Add domain"}
            </button>
            {domain && !domainVerified && (
              <button type="button" onClick={verifyDomain} className="jf-btn-ghost px-4 text-sm">
                Verify now
              </button>
            )}
          </form>
          {domain && (
            <p className="mt-2 text-xs text-slate-500">
              Current claim: <strong>{domain}</strong>
              {domainVerified ? " — verified ✓" : " — pending DNS"}
            </p>
          )}

        </section>

        {/* ── Tracking scripts ── */}
        <section className="jf-card mt-6 p-6">
          <h2 className="text-lg font-semibold">Tracking scripts (global)</h2>
          <p className="mt-1 text-sm text-slate-400">
            Injected into every public form head. Allowed: <code className="rounded bg-slate-100 px-1">&lt;script src="https://…"&gt;</code>, <code className="rounded bg-slate-100 px-1">&lt;meta&gt;</code>, comments. Event handlers & cookie writes stripped.
          </p>
          <textarea value={trackingHead} onChange={(e) => setTrackingHead(e.target.value)} rows={4}
            className="jf-input mt-3 font-mono text-xs"
            placeholder={'<script src="https://www.googletagmanager.com/gtag/js?id=G-XXXX"></-script>'} />
          <button onClick={async () => {
            const r = await fetch("/api/rest/settings/tracking", {
              method: "PUT", headers: { "Content-Type": "application/json" },
              body: JSON.stringify({ head: trackingHead }),
            });
            setMsg(r.ok ? "Tracking saved ✓" : "Failed");
          }} className="jf-btn-primary mt-3 px-4 text-sm">Save tracking</button>
        </section>

        {/* ── e-Invoice (LHDN) ── */}
        <section className="jf-card mt-6 p-6">
          <h2 className="text-lg font-semibold">e-Invoice (LHDN)</h2>
          <p className="mt-1 text-sm text-slate-400">
            Buyer details collected on forms; TIN/NRIC auto-collected for B2B receipts. Receipt PDFs attach LHDN-ready fields.
          </p>
          <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <label className="text-xs text-slate-500">Company TIN</label>
              <input value={tin} onChange={(e) => setTin(e.target.value)} className="jf-input mt-1" placeholder="e.g. C1234567890" />
            </div>
            <div>
              <label className="text-xs text-slate-500">MSIC code</label>
              <input value={msic} onChange={(e) => setMsic(e.target.value)} className="jf-input mt-1" placeholder="e.g. 62010" />
            </div>
          </div>
          <button onClick={async () => {
            const r = await fetch("/api/rest/settings/einvoice", {
              method: "PUT", headers: { "Content-Type": "application/json" },
              body: JSON.stringify({ tin, msic }),
            });
            setMsg(r.ok ? "e-Invoice details saved ✓" : "Failed to save");
          }} className="jf-btn-primary mt-3 px-4 text-sm">Save e-Invoice</button>
        </section>

        {/* ── Outbound webhooks ── */}
        <section className="jf-card mt-6 p-6">
          <h2 className="text-lg font-semibold">Outbound webhooks</h2>
          <p className="mt-1 text-sm text-slate-400">
            Events: <code className="rounded bg-slate-100 px-1">sale.paid</code>,{" "}
            <code className="rounded bg-slate-100 px-1">sale.failed</code>,{" "}
            <code className="rounded bg-slate-100 px-1">subscription.charged</code>. HMAC-SHA256 signed with your secret.
          </p>
          <form onSubmit={addWebhook} className="mt-3 flex flex-wrap gap-2">
            <input value={whUrl} onChange={(e) => setWhUrl(e.target.value)} required type="url"
              placeholder="https://yourapp.com/jomform-hook" className="jf-input min-w-56 flex-1" />
            <input value={whSecret} onChange={(e) => setWhSecret(e.target.value)} required type="text"
              placeholder="Signing secret" className="jf-input w-48" />
            <button className="jf-btn-primary px-4 text-sm">Add</button>
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
          <h2 className="text-lg font-semibold">Connected agents (MCP)</h2>
          <p className="mt-1 text-sm text-slate-400">
            Server URL <code className="rounded bg-slate-100 px-1">https://mcp.jomform.com/mcp</code> —
            agents discover everything automatically via{" "}
            <code className="rounded bg-slate-100 px-1">/.well-known/oauth-authorization-server</code>{" "}
            (OAuth 2.1, dynamic client registration, PKCE). No API keys needed.
          </p>
          <ul className="mt-4 space-y-2">
            {keys.map((k) => (
              <li key={k.id} className="flex items-center justify-between rounded-lg border border-[var(--jf-border)] px-4 py-2.5">
                <div>
                  <p className="text-sm">{k.name}</p>
                  <p className="text-[11px] text-slate-400">client id: {k.name === "AI Agent Key" ? "(oauth-managed)" : k.name}</p>
                </div>
                <button onClick={async () => { await fetch(`/api/rest/keys/${k.id}`, { method: "DELETE" }); load(); }}
                  className="text-xs text-slate-500 hover:text-red-500">Revoke</button>
              </li>
            ))}
            {!keys.length && (
              <li className="rounded-lg border border-dashed border-[var(--jf-border)] px-4 py-3 text-sm text-slate-400">
                No agents connected yet. Point any MCP client at the server URL — it registers itself.
              </li>
            )}
          </ul>
        </section>

        {/* ── Social login connectors ── */}
        <section className="jf-card mt-6 p-6">
          <h2 className="text-lg font-semibold">Connected accounts (login)</h2>
          <p className="mt-1 text-sm text-slate-400">
            Link a Google account, then sign in with one click. More providers (Microsoft/Azure, GitHub) can be enabled later — same connector system.
          </p>
          <ul className="mt-4 space-y-2">
            {social.map((l) => (
              <li key={l.provider} className="flex items-center justify-between rounded-lg border border-[var(--jf-border)] px-4 py-2.5">
                <div>
                  <p className="text-sm capitalize">{l.provider} — {l.email}</p>
                  <p className="text-[11px] text-slate-400">linked {new Date(l.linked_at).toLocaleDateString()}</p>
                </div>
                <button onClick={async () => { await fetch(`/api/auth/social/${l.provider}`, { method: "DELETE" }); load(); }}
                  className="text-xs text-slate-500 hover:text-red-500">Unlink</button>
              </li>
            ))}
            {social.length === 0 && (
              <li className="text-sm text-slate-400">No linked accounts yet.</li>
            )}
          </ul>
          <a href="/api/auth/oauth/google" className="jf-btn-ghost mt-3 inline-block px-4 text-sm">
            Link Google account
          </a>
        </section>
      </main>
    </Shell>
  );
}