"use client";

import { useCallback, useEffect, useState } from "react";
import { Shell } from "../components/Shell";

type Product = {
  id: string;
  name: string;
  price_sen: number;
  type: string;
  billing_period?: string;
  active: boolean;
};

type SaleRow = {
  ref_code: string;
  status: string;
  total_sen: number;
  payment_method: string;
  buyer: string;
  created_at: string;
};

type Summary = { counts: Record<string, number>; revenue_sen: number };

const money = (sen: number) =>
  `RM${(sen / 100).toLocaleString("ms-MY", { minimumFractionDigits: 2 })}`;

const STATUS_BADGE: Record<string, string> = {
  pending: "bg-amber-50 text-amber-700 border-amber-200",
  paid: "bg-blue-50 text-blue-700 border-blue-200",
  confirmed: "bg-emerald-50 text-emerald-700 border-emerald-200",
  failed: "bg-red-50 text-red-700 border-red-200",
  cancelled: "bg-slate-100 text-slate-500 border-slate-200",
};

const STATUS_MS: Record<string, string> = {
  pending: "Menunggu",
  paid: "Dibayar",
  confirmed: "Disahkan",
  failed: "Gagal",
  cancelled: "Dibatalkan",
};

export default function Dashboard() {
  const [authed, setAuthed] = useState(false);
  const [checking, setChecking] = useState(true);
  const [products, setProducts] = useState<Product[]>([]);
  const [sales, setSales] = useState<SaleRow[]>([]);
  const [summary, setSummary] = useState<Summary | null>(null);

  const [pName, setPName] = useState("");
  const [pPrice, setPPrice] = useState("");
  const [pType, setPType] = useState("one_time");
  const [pPeriod, setPPeriod] = useState("month");
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    try {
      const me = await fetch("/api/auth/me");
      if (!me.ok) {
        window.location.href = "/login";
        return;
      }
      const [p, s, sum] = await Promise.all([
        fetch("/api/rest/products").then((r) => r.json()),
        fetch("/api/rest/sales").then((r) => r.json()),
        fetch("/api/rest/summary").then((r) => r.json()),
      ]);
      setProducts(Array.isArray(p) ? p : []);
      setSales(Array.isArray(s) ? s : []);
      setSummary(sum);
      setAuthed(true);
    } catch {
      window.location.href = "/login";
    } finally {
      setChecking(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  async function createProduct(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    const r = await fetch("/api/rest/products", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        name: pName,
        price_sen: Math.round(parseFloat(pPrice) * 100),
        type: pType,
        billing_period: pType === "subscription" ? pPeriod : undefined,
      }),
    });
    if (r.ok) {
      setPName("");
      setPPrice("");
      load();
    } else {
      const d = await r.json().catch(() => ({}));
      setError(d.error || "Gagal menambah produk");
    }
  }

  async function confirmSale(ref: string) {
    await fetch(`/api/rest/sales/${ref}/confirm`, { method: "POST" });
    load();
  }

  if (checking) {
    return (
      <main className="flex min-h-screen items-center justify-center text-slate-400">
        Memuatkan…
      </main>
    );
  }
  if (!authed) return null;

  return (
    <Shell active="dashboard">
      <div className="mx-auto w-full max-w-5xl flex-1 px-6 py-8">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-xl font-semibold tracking-tight">Dashboard</h1>
            <p className="text-sm text-[var(--jf-muted)]">Ringkasan jualan & borang anda</p>
          </div>
          <a href="/builder" className="jf-btn-primary px-4 py-2 text-sm">
            + Borang Baru
          </a>
        </div>

        {/* stat cards */}
        {summary && (
          <div className="mt-6 grid grid-cols-2 gap-3 lg:grid-cols-4">
            <StatCard label="Hasil (disahkan)" value={money(summary.revenue_sen)} accent />
            <StatCard label="Menunggu" value={String(summary.counts.pending ?? 0)} />
            <StatCard label="Disahkan" value={String(summary.counts.confirmed ?? 0)} />
            <StatCard label="Produk aktif" value={String(products.length)} />
          </div>
        )}

        {/* recent sales */}
        <section className="jf-card mt-6 overflow-hidden">
          <div className="flex items-center justify-between border-b border-[var(--jf-border)] px-5 py-3">
            <h2 className="text-sm font-semibold">Jualan terkini</h2>
            <span className="text-xs text-[var(--jf-muted)]">{sales.length} rekod</span>
          </div>
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-[var(--jf-border)] text-left text-xs uppercase tracking-wide text-[var(--jf-muted)]">
                <th className="px-5 py-2.5 font-medium">Rujukan</th>
                <th className="px-5 py-2.5 font-medium">Pembeli</th>
                <th className="px-5 py-2.5 font-medium">Jumlah</th>
                <th className="px-5 py-2.5 font-medium">Status</th>
                <th className="px-5 py-2.5 font-medium"></th>
              </tr>
            </thead>
            <tbody>
              {sales.map((s) => (
                <tr key={s.ref_code} className="border-b border-[var(--jf-border)] last:border-0 hover:bg-slate-50/60">
                  <td className="px-5 py-3 font-mono text-xs">{s.ref_code}</td>
                  <td className="px-5 py-3">{s.buyer || "—"}</td>
                  <td className="px-5 py-3 font-medium">{money(s.total_sen)}</td>
                  <td className="px-5 py-3">
                    <span className={`rounded-full border px-2.5 py-0.5 text-[11px] font-medium ${STATUS_BADGE[s.status] || ""}`}>
                      {STATUS_MS[s.status] || s.status}
                    </span>
                  </td>
                  <td className="px-5 py-3 text-right">
                    {s.status === "paid" && (
                      <button onClick={() => confirmSale(s.ref_code)} className="text-xs font-medium text-[var(--jf-primary)] hover:underline">
                        Sahkan
                      </button>
                    )}
                  </td>
                </tr>
              ))}
              {sales.length === 0 && (
                <tr>
                  <td colSpan={5} className="px-5 py-10 text-center text-sm text-[var(--jf-muted)]">
                    Belum ada jualan lagi.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </section>

        {/* products quick add */}
        <section className="jf-card mt-6 p-5">
          <h2 className="text-sm font-semibold">Tambah produk</h2>
          {error && <p className="mt-2 rounded-lg bg-red-50 px-3 py-2 text-xs text-red-600">{error}</p>}
          <form onSubmit={createProduct} className="mt-3 flex flex-wrap gap-2">
            <input value={pName} onChange={(e) => setPName(e.target.value)} required placeholder="Nama produk" className="jf-input flex-1" />
            <input value={pPrice} onChange={(e) => setPPrice(e.target.value)} required type="number" step="0.01" min="0" placeholder="Harga RM" className="jf-input w-28" />
            <select value={pType} onChange={(e) => setPType(e.target.value)} className="jf-input w-32">
              <option value="one_time">One-time</option>
              <option value="subscription">Langganan</option>
            </select>
            {pType === "subscription" && (
              <select value={pPeriod} onChange={(e) => setPPeriod(e.target.value)} className="jf-input w-28">
                <option value="month">Bulanan</option>
                <option value="year">Tahunan</option>
              </select>
            )}
            <button className="jf-btn-primary px-5 text-sm">Tambah</button>
          </form>
          <div className="mt-4 grid gap-2 sm:grid-cols-2">
            {products.map((p) => (
              <div key={p.id} className="flex items-center justify-between rounded-lg border border-[var(--jf-border)] px-4 py-2.5">
                <div>
                  <p className="text-sm font-medium">{p.name}</p>
                  <p className="text-[11px] text-[var(--jf-muted)]">
                    {p.type === "subscription" ? `Langganan ${p.billing_period === "year" ? "tahunan" : "bulanan"}` : "One-time"}
                  </p>
                </div>
                <span className="text-sm font-semibold text-[var(--jf-primary)]">{money(p.price_sen)}</span>
              </div>
            ))}
          </div>
        </section>
      </div>
    </Shell>
  );
}

function StatCard({ label, value, accent }: { label: string; value: string; accent?: boolean }) {
  return (
    <div className="jf-card p-4">
      <p className="text-xs text-[var(--jf-muted)]">{label}</p>
      <p className={`mt-1 text-xl font-semibold tracking-tight ${accent ? "text-[var(--jf-primary)]" : ""}`}>
        {value}
      </p>
    </div>
  );
}