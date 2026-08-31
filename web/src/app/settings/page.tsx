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
  const [secretMasked, setSecretMasked] = useState("");

  const [domain, setDomain] = useState("");
  const [domainInput, setDomainInput] = useState("");
  const [domainVerified, setDomainVerified] = useState(false);
  const [domainMsg, setDomainMsg] = useState("");

  const load = useCallback(() => {
    return Promise.all([
      fetch("/api/rest/settings/chip").then((r) => (r.ok ? r.json() : null)),
      fetch("/api/rest/keys").then((r) => (r.ok ? r.json() : [])),
      fetch("/api/rest/settings/custom-domain").then((r) => (r.ok ? r.json() : null)),
    ]).then(([s, k, d]) => {
      if (s) {
        setStatus(s);
        if (s.brand_id) setBrandId(s.brand_id);
        if (s.mode) setMode(s.mode);
        if (s.email_fallback) setEmailFallback(s.email_fallback);
        if (Array.isArray(s.whitelist) && s.whitelist.length) setWhitelist(s.whitelist);
        setSecretMasked(s.secret_key_masked || "");
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
      setMsg("Saved. CHIP is active for your forms.");
      setSecretKey("");
      await load();
    } else {
      const d = await r.json().catch(() => ({}));
      setErr(d.error || "Failed to save");
    }
  }

  async function clearCreds() {
    await fetch("/api/rest/settings/chip", { method: "DELETE" });
    setMsg("CHIP credentials cleared.");
    await load();
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
              <label className="text-xs text-slate-500">
                Secret Key {secretMasked ? `(stored: ${secretMasked} — re-enter to replace)` : ""}
              </label>
              <input
                value={secretKey}
                onChange={(e) => setSecretKey(e.target.value)}
                required
                type="password"
                className="jf-input mt-1"
                placeholder="sk_live_… / sk_test_…"
              />
            </div>
            <div>
              <label className="text-xs text-slate-500">Mode</label>
              <p className="mt-1 text-sm">
                {mode === "live"
                  ? "Live — pasted key was sk_live_…"
                  : "Test — pasted key was sk_test_… (or custom key)"}
              </p>
              <p className="mt-0.5 text-[11px] text-slate-400">
                Mode follows the Secret Key you store (sk_test_… = test, sk_live_… = live), same as CHIP for WooCommerce.
              </p>
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

        {/* ── API keys ── */}
        <section className="jf-card mt-6 p-6">
          <h2 className="text-lg font-semibold">API keys (AI agent / MCP)</h2>
          <p className="mt-1 text-sm text-slate-400">
            Endpoint <code className="rounded bg-slate-100 px-1">https://jomform.com/mcp</code>, Bearer auth.
          </p>
          <button onClick={genKey} className="mt-3 rounded-lg bg-emerald-500 px-5 py-2 text-sm font-medium text-white hover:bg-emerald-600">
            Generate key
          </button>
          {newKey && (
            <div className="mt-3 rounded-lg border border-emerald-200 bg-emerald-50 p-3">
              <p className="text-xs text-emerald-700">Save it now — never shown again:</p>
              <code className="mt-1 block break-all font-mono text-sm text-emerald-800">{newKey}</code>
            </div>
          )}
          <ul className="mt-4 space-y-2">
            {keys.map((k) => (
              <li key={k.id} className="flex items-center justify-between rounded-lg border border-[var(--jf-border)] px-4 py-2.5">
                <span className="text-sm">{k.name}</span>
                <span className="text-xs text-slate-500">{k.scopes.join(", ")}</span>
              </li>
            ))}
          </ul>
        </section>
      </main>
    </Shell>
  );
}