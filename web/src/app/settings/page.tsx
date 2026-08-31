"use client";

import { useEffect, useState } from "react";
import { Shell } from "../../components/Shell";

type ChipStatus = {
  configured: boolean;
  mode: string;
};

type KeyRow = { id: string; name: string; scopes: string[] };

export default function Settings() {
  const [status, setStatus] = useState<ChipStatus | null>(null);
  const [brandId, setBrandId] = useState("");
  const [secretKey, setSecretKey] = useState("");
  const [mode, setMode] = useState("test");
  const [msg, setMsg] = useState("");
  const [err, setErr] = useState("");
  const [keys, setKeys] = useState<KeyRow[]>([]);
  const [newKey, setNewKey] = useState("");

  async function load() {
    const [s, k] = await Promise.all([
      fetch("/api/rest/settings/chip").then((r) => (r.ok ? r.json() : null)),
      fetch("/api/rest/keys").then((r) => (r.ok ? r.json() : [])),
    ]);
    if (s) setStatus(s);
    setKeys(k || []);
  }

  useEffect(() => {
    load();
  }, []);

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setMsg("");
    setErr("");
    const r = await fetch("/api/rest/settings/chip", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ brand_id: brandId, secret_key: secretKey, mode }),
    });
    if (r.ok) {
      setMsg("Dikemaskini. Bayaran CHIP kini aktif untuk borang anda.");
      setSecretKey("");
      await load();
    } else {
      const d = await r.json().catch(() => ({}));
      setErr(d.error || "Gagal menyimpan");
    }
  }

  async function clearCreds() {
    await fetch("/api/rest/settings/chip", { method: "DELETE" });
    setMsg("Kredensial CHIP dipadam.");
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

  return (
    <Shell active="settings"><main className="mx-auto max-w-3xl flex-1 px-6 py-8">
      <a href="/" className="text-sm text-indigo-400">
        ← Dashboard
      </a>
      <h1 className="mt-2 text-2xl font-bold">
        Tetapan <span className="text-indigo-400">JomForm</span>
      </h1>

      <section className="mt-8 rounded-2xl border jf-card p-6">
        <div className="flex items-center justify-between">
          <h2 className="text-lg font-semibold">CHIP Payment Gateway</h2>
          {status && (
            <span
              className={`rounded-full px-3 py-1 text-xs font-semibold ${
                status.configured
                  ? "bg-emerald-500/15 text-emerald-400"
                  : "bg-slate-700/50 text-slate-400"
              }`}
            >
              {status.configured ? `AKTIF · ${status.mode.toUpperCase()}` : "BELUM SET"}
            </span>
          )}
        </div>
        <p className="mt-2 text-sm text-slate-400">
          Dapatkan <code className="rounded bg-slate-800 px-1">Brand ID</code> &{" "}
          <code className="rounded bg-slate-800 px-1">Secret Key</code> dari{" "}
          <a
            href="https://portal.chip-in.asia/collect/developers/brands"
            className="text-indigo-400 underline"
            target="_blank"
          >
            CHIP Merchant Portal → Developers
          </a>
          . Kunci disimpan terenkripsi — tidak dipapar semula.
        </p>
        {msg && <p className="mt-3 rounded-lg bg-emerald-500/10 px-3 py-2 text-sm text-emerald-400">{msg}</p>}
        {err && <p className="mt-3 rounded-lg bg-red-500/10 px-3 py-2 text-sm text-red-400">{err}</p>}
        <form onSubmit={save} className="mt-4 space-y-3">
          <div>
            <label className="text-xs text-slate-500">Brand ID</label>
            <input
              value={brandId}
              onChange={(e) => setBrandId(e.target.value)}
              required
              className="mt-1 w-full rounded-xl border jf-input px-3 py-2 text-sm"
              placeholder="cth. 5f2b...-uuid"
            />
          </div>
          <div>
            <label className="text-xs text-slate-500">Secret Key {status?.configured && "(masukkan semula untuk tukar)"}</label>
            <input
              value={secretKey}
              onChange={(e) => setSecretKey(e.target.value)}
              required
              type="password"
              className="mt-1 w-full rounded-xl border jf-input px-3 py-2 text-sm"
              placeholder="sk_live_... / sk_test_..."
            />
          </div>
          <div>
            <label className="text-xs text-slate-500">Mode</label>
            <select
              value={mode}
              onChange={(e) => setMode(e.target.value)}
              className="mt-1 rounded-xl border jf-input px-3 py-2 text-sm"
            >
              <option value="test">Test (kad ujian sahaja)</option>
              <option value="live">Live</option>
            </select>
          </div>
          <div className="flex gap-2">
            <button className="rounded-xl bg-indigo-500 px-5 py-2 text-sm font-semibold">Simpan</button>
            {status?.configured && (
              <button type="button" onClick={clearCreds} className="rounded-xl border border-slate-600 px-4 py-2 text-sm text-slate-300">
                Padam
              </button>
            )}
          </div>
        </form>
      </section>

      <section className="mt-6 rounded-2xl border jf-card p-6">
        <h2 className="text-lg font-semibold">Kunci API (AI Agent / MCP)</h2>
        <p className="mt-1 text-sm text-slate-400">
          Untuk kawalan kedai melalui AI agent: endpoint{" "}
          <code className="rounded bg-slate-800 px-1">https://jomform.com/mcp</code>, Bearer authentication.
        </p>
        <button onClick={genKey} className="mt-3 rounded-xl bg-emerald-500/90 px-5 py-2 text-sm font-semibold text-emerald-950">
          Jana Key
        </button>
        {newKey && (
          <div className="mt-3 rounded-xl border border-emerald-600/40 bg-emerald-500/10 p-3">
            <p className="text-xs text-emerald-300">Simpan sekarang — takkan dipapar lagi:</p>
            <code className="mt-1 block break-all font-mono text-sm text-emerald-200">{newKey}</code>
          </div>
        )}
        <ul className="mt-4 space-y-2">
          {keys
            .filter((k) => !k.id.startsWith("REVOKE")) // revoked keys omitted server-side
            .map((k) => (
              <li key={k.id} className="flex items-center justify-between rounded-xl border border-slate-800 px-4 py-2.5">
                <span className="text-sm">{k.name}</span>
                <span className="text-xs text-slate-500">{k.scopes.join(", ")}</span>
              </li>
            ))}
        </ul>
      </section>
    </main></Shell>
  );
}