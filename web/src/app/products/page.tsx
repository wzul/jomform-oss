"use client";

import { useCallback, useEffect, useState } from "react";
import { Shell } from "../../components/Shell";
import { useI18n } from "../../components/i18n";

type Product = {
  id: string;
  name: string;
  price_sen: number;
  type: string;
  billing_period?: string;
  stock?: number | null;
  active: boolean;
};

const money = (sen: number) =>
  `RM${(sen / 100).toLocaleString("en-MY", { minimumFractionDigits: 2 })}`;

export default function Products() {
  const { t } = useI18n();
  const [products, setProducts] = useState<Product[]>([]);
  const [checking, setChecking] = useState(true);
  const [name, setName] = useState("");
  const [price, setPrice] = useState("");
  const [type, setType] = useState("one_time");
  const [period, setPeriod] = useState("month");
  const [stock, setStock] = useState("");
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    const me = await fetch("/api/auth/me");
    if (!me.ok) {
      window.location.href = "/landing";
      return;
    }
    const p = await fetch("/api/rest/products").then((r) => r.json());
    setProducts(Array.isArray(p) ? p : []);
    setChecking(false);
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  async function add(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    const r = await fetch("/api/rest/products", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        name,
        price_sen: Math.round(parseFloat(price) * 100),
        type,
        billing_period: type === "subscription" ? period : undefined,
        stock: stock === "" ? null : Math.max(0, parseInt(stock, 10)),
      }),
    });
    if (r.ok) {
      setName("");
      setPrice("");
      setStock("");
      load();
    } else {
      const d = await r.json().catch(() => ({}));
      setError(d.error || "Failed to add product");
    }
  }

  async function remove(id: string) {
    // archive via update (no delete endpoint) — flip active with confirm
    await fetch(`/api/rest/products/${id}`, { method: "DELETE" }).catch(() => {});
    load();
  }

  if (checking) {
    return (
      <main className="flex min-h-screen items-center justify-center text-slate-400">…</main>
    );
  }

  return (
    <Shell active="products">
      <div className="mx-auto w-full max-w-4xl flex-1 px-6 py-8">
        <h1 className="text-xl font-semibold tracking-tight">{t("prod.title")}</h1>
        <p className="text-sm text-[var(--jf-muted)]">{t("prod.subtitle")}</p>

        <form onSubmit={add} className="jf-card mt-6 flex flex-wrap gap-2 p-4">
          <input value={name} onChange={(e) => setName(e.target.value)} required placeholder={t("prod.name")} className="jf-input min-w-48 flex-1" />
          <input value={price} onChange={(e) => setPrice(e.target.value)} required type="number" step="0.01" min="0" placeholder={t("prod.price")} className="jf-input w-32" />
          <input value={stock} onChange={(e) => setStock(e.target.value)} type="number" min="0" placeholder="Stock (blank = ∞)" className="jf-input w-40" />
          <select value={type} onChange={(e) => setType(e.target.value)} className="jf-input w-36">
            <option value="one_time">{t("prod.oneTime")}</option>
            <option value="subscription">{t("prod.subscription")}</option>
          </select>
          {type === "subscription" && (
            <select value={period} onChange={(e) => setPeriod(e.target.value)} className="jf-input w-32">
              <option value="month">{t("prod.monthly")}</option>
              <option value="year">{t("prod.yearly")}</option>
            </select>
          )}
          <button className="jf-btn-primary">{t("prod.add")}</button>
        </form>
        {error && <p className="mt-2 rounded-lg bg-red-50 px-3 py-2 text-xs text-red-600">{error}</p>}

        <ul className="mt-4 space-y-2">
          {products.map((p) => (
            <li key={p.id} className="jf-card flex items-center justify-between px-4 py-3">
              <div>
                <p className="text-sm font-medium">{p.name}</p>
                <p className="text-[11px] text-[var(--jf-muted)]">
                  {p.type === "subscription"
                    ? `${t("prod.subscription")} · ${p.billing_period === "year" ? t("prod.yearly") : t("prod.monthly")}`
                    : t("prod.oneTime")}
                  {typeof p.stock === "number"
                    ? p.stock > 0
                      ? ` · ${p.stock} in stock`
                      : " · SOLD OUT"
                    : ""}
                </p>
              </div>
              <div className="flex items-center gap-3">
                <span className="text-sm font-semibold text-[var(--jf-primary)]">{money(p.price_sen)}</span>
                <button onClick={() => remove(p.id)} className="text-xs text-[var(--jf-muted)] hover:text-red-500">
                  ✕
                </button>
              </div>
            </li>
          ))}
          {products.length === 0 && (
            <li className="rounded-lg text-sm text-[var(--jf-muted)]">{t("prod.empty")}</li>
          )}
        </ul>
      </div>
    </Shell>
  );
}