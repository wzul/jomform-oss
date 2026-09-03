"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { Shell } from "../../components/Shell";
import { useI18n } from "../../components/i18n";
import { useSession } from "../../components/session";

type Product = {
  id: string;
  name: string;
  description?: string;
  price_sen: number;
  images?: string[];
  type: string;
  billing_period?: string;
  trial_days?: number;
  stock?: number | null;
  active: boolean;
  created_at?: string;
};

type SortKey = "name" | "price_sen" | "created_at";
type SortDir = "asc" | "desc";

const money = (sen: number) =>
  `RM${(sen / 100).toLocaleString("en-MY", { minimumFractionDigits: 2 })}`;

const PER_PAGE = 10;

export default function Products() {
  const { t } = useI18n();
  const { anon, bundle } = useSession();
  const router = useRouter();

  const [products, setProducts] = useState<Product[]>([]);
  const [checking, setChecking] = useState(true);
  const [error, setError] = useState("");

  // search + filters
  const [search, setSearch] = useState("");
  const [typeFilter, setTypeFilter] = useState("all");
  const [stockFilter, setStockFilter] = useState("all");

  // sorting
  const [sortKey, setSortKey] = useState<SortKey>("created_at");
  const [sortDir, setSortDir] = useState<SortDir>("desc");

  // pagination
  const [page, setPage] = useState(1);

  // selection
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    // Issue #29: no serial `await me → fetch` gate — the list fetch runs
    // immediately; a 401 flips `anon` and the auth bounce below handles it.
    // include_archived=1 so the WooCommerce-style admin list shows a status
    // column and lets owners toggle products back to active.
    const p = await fetch("/api/rest/products?include_archived=1").then((r) =>
      r.ok ? r.json() : []
    );
    setProducts(Array.isArray(p) ? p : []);
    setChecking(false);
  }, []);

  useEffect(() => {
    // Always fetch the authoritative list (include_archived=1) so archived
    // products show for the status column/toggle. The bundle cache can
    // optimistically paint active products first, but must not suppress the
    // reload that surfaces archived ones.
    if (bundle && Array.isArray(bundle.products) && products.length === 0 && checking) {
      setProducts(bundle.products as Product[]);
    }
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [bundle]);

  // Auth bounce via router (client transition), not a full document reload.
  useEffect(() => {
    if (anon) router.replace("/landing");
  }, [anon, router]);

  // Reset pagination + selection when filters change.
  useEffect(() => {
    setPage(1);
    setSelected(new Set());
  }, [search, typeFilter, stockFilter]);

  // ── filtering + sorting + pagination (client-side for now) ──────────────
  const filtered = useMemo(() => {
    let rows = products;
    if (search.trim()) {
      const q = search.trim().toLowerCase();
      rows = rows.filter((p) => p.name.toLowerCase().includes(q));
    }
    if (typeFilter !== "all") {
      rows = rows.filter((p) => p.type === typeFilter);
    }
    if (stockFilter === "in") {
      rows = rows.filter((p) => typeof p.stock === "number" && p.stock > 0);
    } else if (stockFilter === "out") {
      rows = rows.filter(
        (p) => typeof p.stock === "number" && p.stock === 0
      );
    }
    const dir = sortDir === "asc" ? 1 : -1;
    return [...rows].sort((a, b) => {
      switch (sortKey) {
        case "name":
          return a.name.localeCompare(b.name) * dir;
        case "price_sen":
          return (a.price_sen - b.price_sen) * dir;
        default: {
          const ta = a.created_at || "";
          const tb = b.created_at || "";
          return (ta < tb ? -1 : ta > tb ? 1 : 0) * dir;
        }
      }
    });
  }, [products, search, typeFilter, stockFilter, sortKey, sortDir]);

  const totalPages = Math.max(1, Math.ceil(filtered.length / PER_PAGE));
  const safePage = Math.min(page, totalPages);
  const pageRows = useMemo(
    () => filtered.slice((safePage - 1) * PER_PAGE, safePage * PER_PAGE),
    [filtered, safePage]
  );

  function toggleSort(key: SortKey) {
    if (sortKey === key) {
      setSortDir((d) => (d === "asc" ? "desc" : "asc"));
    } else {
      setSortKey(key);
      setSortDir(key === "name" ? "asc" : "desc");
    }
  }

  function SortHeader({
    label,
    k,
  }: {
    label: string;
    k: SortKey;
  }) {
    const active = sortKey === k;
    return (
      <button
        onClick={() => toggleSort(k)}
        className="inline-flex items-center gap-1 text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)] hover:text-[var(--jf-primary)]"
        aria-sort={active ? (sortDir === "asc" ? "ascending" : "descending") : "none"}
      >
        {label}
        <span className="text-[10px]">{active ? (sortDir === "asc" ? "▲" : "▼") : "⇅"}</span>
      </button>
    );
  }

  // ── selection ────────────────────────────────────────────────────────────
  const ids = Array.from(selected);
  const allOnPageSelected =
    pageRows.length > 0 && pageRows.every((p) => selected.has(p.id));

  function toggle(id: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function toggleAll() {
    setSelected((prev) => {
      const next = new Set(prev);
      if (allOnPageSelected) {
        for (const p of pageRows) next.delete(p.id);
      } else {
        for (const p of pageRows) next.add(p.id);
      }
      return next;
    });
  }

  async function bulkArchive() {
    if (ids.length === 0 || busy) return;
    const ok = window.confirm(t("prod.bulkArchiveConfirm", { n: ids.length }));
    if (!ok) return;
    setBusy(true);
    try {
      await fetch("/api/rest/products/bulk", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ action: "archive", ids }),
      });
    } finally {
      setBusy(false);
      setSelected(new Set());
      load();
    }
  }

  // ── toggle status / delete (single row) ─────────────────────────────────
  async function toggleStatus(p: Product) {
    await fetch(`/api/rest/products/${p.id}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ active: !p.active }),
    });
    load();
  }

  async function archiveOne(p: Product) {
    const ok = window.confirm(t("prod.archiveOneConfirm", { name: p.name }));
    if (!ok) return;
    await fetch(`/api/rest/products/${p.id}`, { method: "DELETE" });
    load();
  }

  if (checking) {
    return (
      <main className="flex min-h-screen items-center justify-center text-slate-400">…</main>
    );
  }

  const countLabel = t("prod.countItems", { n: filtered.length });
  const statusLabel = (p: Product) => (p.active ? t("prod.active") : t("prod.archived"));
  const stockLabel = (p: Product) =>
    typeof p.stock === "number"
      ? p.stock > 0
        ? t("prod.inStock", { n: p.stock })
        : t("prod.soldOut")
      : "∞";
  const typeLabel = (p: Product) =>
    p.type === "subscription"
      ? `${t("prod.subscription")} · ${p.billing_period === "year" ? t("prod.yearly") : t("prod.monthly")}`
      : t("prod.oneTime");

  return (
    <Shell active="products">
      <div className="w-full flex-1 px-6 py-8">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h1 className="max-w-xl text-xl font-semibold tracking-tight">{t("prod.title")}</h1>
            <p className="max-w-xl text-sm text-[var(--jf-muted)]">{t("prod.subtitle")}</p>
          </div>
        </div>

        {/* Add product — dedicated editor page (issue #151) */}
        <div className="mt-6 flex flex-wrap items-center justify-between gap-3">
          <button
            onClick={() => router.push("/products/new")}
            className="jf-btn-primary"
          >
            {t("prod.add")}
          </button>
        </div>
        {error && <p className="mt-2 rounded-lg bg-red-50 px-3 py-2 text-xs text-red-600">{error}</p>}

        {/* Bulk action bar */}
        {ids.length > 0 && (
          <div className="jf-card mt-4 flex flex-wrap items-center gap-2 px-4 py-3">
            <span className="text-sm font-medium">{t("forms.nSelected", { n: ids.length })}</span>
            <div className="ml-auto flex flex-wrap items-center gap-2">
              <button
                onClick={bulkArchive}
                disabled={busy}
                className="jf-btn-ghost px-3 py-1.5 text-xs text-rose-600 hover:bg-rose-50 disabled:opacity-50"
              >
                {t("prod.bulkArchive")}
              </button>
            </div>
          </div>
        )}

        {/* Search + filters */}
        <div className="mt-4 flex flex-wrap items-center gap-2">
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={t("prod.searchPlaceholder")}
            aria-label={t("prod.searchPlaceholder")}
            className="jf-input min-w-48 flex-1 sm:max-w-xs"
          />
          <select value={typeFilter} onChange={(e) => setTypeFilter(e.target.value)} className="jf-input w-40" aria-label={t("prod.filterType")}>
            <option value="all">{t("prod.filterAll")}</option>
            <option value="one_time">{t("prod.oneTime")}</option>
            <option value="subscription">{t("prod.subscription")}</option>
          </select>
          <select value={stockFilter} onChange={(e) => setStockFilter(e.target.value)} className="jf-input w-44" aria-label={t("prod.filterStock")}>
            <option value="all">{t("prod.filterStockAll")}</option>
            <option value="in">{t("prod.inStockShort")}</option>
            <option value="out">{t("prod.outOfStock")}</option>
          </select>
          <span className="ml-auto whitespace-nowrap text-xs text-[var(--jf-muted)]">{countLabel}</span>
        </div>

        {/* Table */}
        <div className="jf-card mt-4 overflow-x-auto">
          <table className="w-full min-w-[640px] text-sm">
            <thead>
              <tr className="border-b border-[var(--jf-border)] text-left">
                <th className="w-10 px-4 py-3">
                  <input
                    type="checkbox"
                    aria-label={t("forms.selectAll")}
                    checked={allOnPageSelected}
                    onChange={toggleAll}
                    className="h-4 w-4 accent-[var(--jf-primary)]"
                  />
                </th>
                <th className="px-2 py-3">
                  <SortHeader label={t("prod.name")} k="name" />
                </th>
                <th className="px-2 py-3 text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                  {t("prod.type")}
                </th>
                <th className="px-2 py-3 text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                  {t("prod.stockShort")}
                </th>
                <th className="px-2 py-3">
                  <SortHeader label={t("prod.price")} k="price_sen" />
                </th>
                <th className="px-2 py-3 text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                  {t("dash.status")}
                </th>
                <th className="px-2 py-3">
                  <SortHeader label={t("prod.date")} k="created_at" />
                </th>
                <th className="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                  {t("bld.actions")}
                </th>
              </tr>
            </thead>
            <tbody>
              {pageRows.map((p) => (
                <tr
                  key={p.id}
                  className={`border-b border-[var(--jf-border)] last:border-0 ${
                    p.active ? "" : "opacity-60"
                  }`}
                >
                  <td className="px-4 py-3">
                    <input
                      type="checkbox"
                      aria-label={`Select ${p.name}`}
                      checked={selected.has(p.id)}
                      onChange={() => toggle(p.id)}
                      className="h-4 w-4 accent-[var(--jf-primary)]"
                    />
                  </td>
                  <td className="px-2 py-3">
                    <button
                      onClick={() => router.push(`/products/${p.id}/edit`)}
                      className="text-left font-medium text-[var(--jf-primary)] hover:underline"
                    >
                      {p.name}
                    </button>
                  </td>
                  <td className="px-2 py-3 text-[var(--jf-muted)]">{typeLabel(p)}</td>
                  <td className="px-2 py-3">{stockLabel(p)}</td>
                  <td className="px-2 py-3 font-semibold">{money(p.price_sen)}</td>
                  <td className="px-2 py-3">
                    <button
                      onClick={() => toggleStatus(p)}
                      className={[
                        "rounded-full px-2.5 py-0.5 text-[11px] font-medium",
                        p.active
                          ? "border border-emerald-200 bg-emerald-50 text-emerald-700"
                          : "border border-slate-200 bg-slate-50 text-slate-500",
                      ].join(" ")}
                      title={t("prod.toggleStatusTitle")}
                    >
                      {statusLabel(p)}
                    </button>
                  </td>
                  <td className="px-2 py-3 text-[var(--jf-muted)]">
                    {p.created_at ? new Date(p.created_at).toLocaleDateString() : "—"}
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex items-center justify-end gap-1">
                      <button onClick={() => router.push(`/products/${p.id}/edit`)} className="jf-btn-ghost px-2.5 py-1 text-xs">
                        {t("forms.edit")}
                      </button>
                      <button
                        onClick={() => archiveOne(p)}
                        className="jf-btn-ghost px-2.5 py-1 text-xs text-rose-600 hover:bg-rose-50"
                        title={t("prod.archiveOneTitle")}
                      >
                        ✕
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
              {pageRows.length === 0 && (
                <tr>
                  <td colSpan={8} className="px-4 py-8 text-center text-sm text-[var(--jf-muted)]">
                    {t("prod.empty")}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>

        {/* Pagination */}
        {totalPages > 1 && (
          <div className="mt-4 flex flex-wrap items-center justify-between gap-2 text-xs text-[var(--jf-muted)]">
            <span>
              {t("prod.pageOf", { page: safePage, total: totalPages })}
            </span>
            <div className="flex items-center gap-1">
              <button
                onClick={() => setPage((p) => Math.max(1, p - 1))}
                disabled={safePage <= 1}
                className="jf-btn-ghost px-3 py-1.5 disabled:opacity-40"
              >
                {t("common.prev")}
              </button>
              <button
                onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
                disabled={safePage >= totalPages}
                className="jf-btn-ghost px-3 py-1.5 disabled:opacity-40"
              >
                {t("common.next")}
              </button>
            </div>
          </div>
        )}

              </div>
    </Shell>
  );
}
