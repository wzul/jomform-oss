"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { Shell } from "../components/Shell";
import { useI18n } from "../components/i18n";
import { useSession } from "../components/session";
import { workspaceQuery, workspaceScope } from "../lib/workspace";
import Landing from "./landing/page";
import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

type Product = { id: string; name: string; price_sen: number; type: string; active: boolean };
type SaleRow = {
  ref_code: string;
  status: string;
  total_sen: number;
  payment_method: string;
  buyer: string;
  created_at: string;
  email_status?: string;
};
type SalesPage = { items: SaleRow[]; page: number; limit: number; total: number };
type FormStats = {
  title: string;
  slug: string;
  submissions: number;
  paid_count: number;
  confirmed_count: number;
  failed_count: number;
  pending_count: number;
  revenue_sen: number;
};
type ProductStats = {
  product_id: string;
  name: string;
  units_sold: number;
  paid_units: number;
  revenue_sen: number;
  paid_revenue_sen: number;
};

type Summary = { counts: Record<string, number>; revenue_sen: number };

const money = (sen: number) =>
  `RM${(sen / 100).toLocaleString("en-MY", { minimumFractionDigits: 2 })}`;

const PAGE_SIZE = 10;

export default function Dashboard() {
  const { t } = useI18n();
  // Issue #29: me + landing data (products, summary, sales, analytics) all
  // come from the single dashboard-bundle round-trip fetched once per
  // session — the 914ms me→5-fetch waterfall is gone.
  const { bundle, anon, role, workspace } = useSession();
  // Issue #163: read_only members see no write actions (confirm sale).
  const canWrite = role !== "read_only";
  const [products, setProducts] = useState<Product[]>([]);
  const [sales, setSales] = useState<SaleRow[]>([]);
  const [salesPage, setSalesPage] = useState(1);
  const [salesTotal, setSalesTotal] = useState(0);
  const [summary, setSummary] = useState<Summary | null>(null);
  const [formStats, setFormStats] = useState<FormStats[]>([]);
  const [prodStats, setProdStats] = useState<ProductStats[]>([]);
  const [ready, setReady] = useState(false);

  const loadSales = useCallback(
    async (p: number) => {
      const sp = workspaceScope();
      const q = new URLSearchParams({ page: String(p), limit: String(PAGE_SIZE) });
      if (sp) q.set("workspace", sp);
      const r = await fetch(`/api/rest/sales?${q.toString()}`);
      if (!r.ok) {
        setSales([]);
        setSalesTotal(0);
        return;
      }
      const d = (await r.json()) as SalesPage;
      setSales(Array.isArray(d.items) ? d.items : []);
      setSalesPage(d.page || p);
      setSalesTotal(d.total || 0);
    },
    [],
  );

  // Adopt the bundle landing sections as soon as the session context has
  // them (immediately on later navigations — cached).
  // Issue #170: `workspace` re-runs this when the active workspace changes so
  // the landing sections (products, summary, sales, analytics) re-fetch for the
  // new workspace without a manual refresh.
  useEffect(() => {
    if (!bundle) return;
    setProducts(Array.isArray(bundle.products) ? (bundle.products as Product[]) : []);
    setSummary(bundle.summary ?? null);
    setFormStats(Array.isArray(bundle.form_analytics) ? (bundle.form_analytics as FormStats[]) : []);
    setProdStats(
      Array.isArray(bundle.product_analytics) ? (bundle.product_analytics as ProductStats[]) : [],
    );
    if (bundle.sales) {
      setSales((bundle.sales.items as SaleRow[]) ?? []);
      setSalesPage(bundle.sales.page || 1);
      setSalesTotal(bundle.sales.total || 0);
    } else {
      loadSales(1);
    }
    setReady(true);
  }, [bundle, loadSales, workspace]);

  const salesTotalPages = Math.max(1, Math.ceil(salesTotal / PAGE_SIZE));

  async function gotoSalesPage(p: number) {
    const next = Math.min(Math.max(1, p), salesTotalPages);
    await loadSales(next);
  }

  // 14-day revenue series
  const series = (() => {
    if (!sales.length) return [];
    const days: { day: string; dayLabel: string; value: number }[] = [];
    const byDay = new Map<string, number>();
    for (const s of sales) {
      if (s.status !== "paid" && s.status !== "confirmed") continue;
      const day = s.created_at.slice(0, 10);
      byDay.set(day, (byDay.get(day) || 0) + s.total_sen);
    }
    for (let i = 13; i >= 0; i--) {
      const d = new Date();
      d.setDate(d.getDate() - i);
      const key = d.toISOString().slice(0, 10);
      days.push({ day: key, dayLabel: key.slice(5), value: Math.round(((byDay.get(key) || 0) / 100) * 100) / 100 });
    }
    return days;
  })();

  async function confirmSale(ref: string) {
    await fetch(`/api/rest/sales/${ref}/confirm`, { method: "POST" });
    await loadSales(salesPage);
  }

  // Issue #48: resolve anon BEFORE the loading shell so an anonymous visitor
  // (401 / session expired / wedge timeout) lands on the Landing page instead
  // of hanging on the spinner. `bundle` stays undefined while loading.
  if (anon) return <Landing />;
  if (!ready) {
    return <main className="flex min-h-screen items-center justify-center text-slate-400">…</main>;
  }

  const badge = (s: string) =>
    ({
      pending: "bg-amber-50 text-amber-700 border-amber-200",
      paid: "bg-blue-50 text-blue-700 border-blue-200",
      confirmed: "bg-emerald-50 text-emerald-700 border-emerald-200",
      failed: "bg-red-50 text-red-700 border-red-200",
      cancelled: "bg-slate-100 text-slate-500 border-slate-200",
    })[s] || "";

  // issue #106: per-sale receipt email status label + badge colour.
  const emailLabel = (s?: string) =>
    ({
      delivered: t("dash.emailDelivered"),
      open: t("dash.emailOpened"),
      click: t("dash.emailClicked"),
      bounce: t("dash.emailBounced"),
      complaint: t("dash.emailComplaint"),
    })[s || ""] || (s ? s : t("dash.emailNotSent"));
  const emailBadge = (s?: string) =>
    ({
      open: "bg-emerald-50 text-emerald-700 border-emerald-200",
      click: "bg-emerald-50 text-emerald-700 border-emerald-200",
      delivered: "bg-blue-50 text-blue-700 border-blue-200",
      bounce: "bg-red-50 text-red-700 border-red-200",
      complaint: "bg-red-50 text-red-700 border-red-200",
    })[s || ""] || "bg-slate-100 text-slate-500 border-slate-200";

  return (
    <Shell active="dashboard">
      <div className="w-full flex-1 px-6 py-8">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="max-w-xl text-xl font-semibold tracking-tight">{t("dash.title")}</h1>
            <p className="max-w-xl text-sm text-[var(--jf-muted)]">{t("dash.subtitle")}</p>
          </div>
          <Link href="/builder" className="jf-btn-primary">
            {t("dash.newForm")}
          </Link>
        </div>

        {summary && (
          <div className="mt-6 grid grid-cols-2 gap-3 lg:grid-cols-4">
            <Stat label={t("dash.revenue")} value={money(summary.revenue_sen)} accent />
            <Stat label={t("dash.pending")} value={String(summary.counts.pending ?? 0)} />
            <Stat label={t("dash.confirmed")} value={String(summary.counts.confirmed ?? 0)} />
            <Stat label={t("dash.activeProducts")} value={String(products.length)} />
          </div>
        )}

        {/* Revenue chart — last 14 days */}
        <section className="jf-card mt-6 p-5">
          <h2 className="text-sm font-semibold">
            {t("dash.revenue")} · 14d
          </h2>
          <div className="mt-3 h-56">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={series} margin={{ left: -18, right: 8, top: 8 }}>
                <defs>
                  <linearGradient id="rev" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="#5e6ad2" stopOpacity={0.25} />
                    <stop offset="100%" stopColor="#5e6ad2" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="#eceef2" vertical={false} />
                <XAxis dataKey="dayLabel" tick={{ fontSize: 11, fill: "#9ca3af" }} axisLine={false} tickLine={false} />
                <YAxis tick={{ fontSize: 11, fill: "#9ca3af" }} axisLine={false} tickLine={false} width={48}
                       tickFormatter={(v) => `RM${v}`} />
                <Tooltip
                  formatter={(v) => [money(Number(v)), t("dash.revenue")]}
                  labelFormatter={(l) => String(l)}
                  contentStyle={{ borderRadius: 8, borderColor: "#e6e8ec", fontSize: 12 }}
                />
                <Area type="monotone" dataKey="value" stroke="#5e6ad2" strokeWidth={2} fill="url(#rev)" />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </section>

        {/* Form analytics */}
        <section className="jf-card mt-6 p-5">
          <h2 className="text-sm font-semibold">{t("dash.formAnalytics")}</h2>
          <div className="mt-3 grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
            {formStats.map((f) => (
              <div key={f.slug} className="rounded-lg border border-[var(--jf-border)] p-3">
                <p className="truncate text-sm font-medium" title={f.title}>{f.title}</p>
                <p className="font-mono text-[11px] text-[var(--jf-muted)]">{workspaceScope() || "app"}.jomform.com/{f.slug}</p>
                <div className="mt-2 grid grid-cols-2 gap-x-2 gap-y-0.5 text-[11px] text-[var(--jf-muted)]">
                  <span>{t("dash.submissions")} <b>{f.submissions}</b></span>
                  <span>{t("dash.paidConfirmed")} <b className="text-emerald-600">{f.paid_count + f.confirmed_count}</b></span>
                  <span>{t("dash.pendingLabel")} <b>{f.pending_count}</b></span>
                  <span>{t("dash.failedLabel")} <b>{f.failed_count}</b></span>
                </div>
                <p className="mt-1.5 text-sm font-semibold text-[var(--jf-primary)]">{money(f.revenue_sen)}</p>
              </div>
            ))}
            {formStats.length === 0 && <p className="text-xs text-[var(--jf-muted)]">{t("dash.noForms")}</p>}
          </div>
        </section>

        {/* Product analytics */}
        <section className="jf-card mt-6 p-5">
          <h2 className="text-sm font-semibold">{t("dash.productPerf")}</h2>
          <ul className="mt-2 divide-y divide-[var(--jf-border)]">
            {prodStats.map((pr) => (
              <li key={pr.product_id} className="flex items-center justify-between gap-3 py-2.5">
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium">{pr.name}</p>
                  <p className="text-[11px] text-[var(--jf-muted)]">
                    {pr.units_sold} {t("dash.sold")} · {pr.paid_units} {t("dash.paid")}
                  </p>
                </div>
                <div className="text-right">
                  <p className="text-sm font-semibold">{money(pr.paid_revenue_sen)}</p>
                  <p className="text-[10px] text-[var(--jf-muted)]">{t("dash.gross")} {money(pr.revenue_sen)}</p>
                </div>
              </li>
            ))}
            {prodStats.length === 0 && (
              <li className="py-4 text-xs text-[var(--jf-muted)]">{t("dash.noProdSales")}</li>
            )}
          </ul>
        </section>

        {/* Recent sales */}
        <section className="jf-card mt-6 overflow-hidden">
          <div className="flex items-center justify-between border-b border-[var(--jf-border)] px-5 py-3">
            <h2 className="text-sm font-semibold">{t("dash.recentSales")}</h2>
            <div className="flex items-center gap-3">
              <span className="text-xs text-[var(--jf-muted)]">
                {salesTotal} {t("dash.records")}
              </span>
              <a
                href={`/api/rest/sales/export.csv${workspaceQuery()}`}
                className="rounded-md border border-[var(--jf-border)] px-2.5 py-1 text-xs text-slate-600 hover:bg-slate-50"
              >
                {t("common.exportCsv")}
              </a>
            </div>
          </div>
          {/* issue #106: honest privacy note about the open-tracking pixel. */}
          <p className="border-b border-[var(--jf-border)] px-5 py-2 text-[11px] text-[var(--jf-muted)]">
            {t("dash.emailStatusNote")}
          </p>
          {/* mobile: stacked cards (no table scroll needed) */}
          <ul className="divide-y divide-[var(--jf-border)] md:hidden">
            {sales.map((s) => (
              <li key={s.ref_code} className="flex items-center justify-between gap-3 px-4 py-3">
                <div className="min-w-0">
                  <p className="truncate font-mono text-xs text-[var(--jf-muted)]">{s.ref_code}</p>
                  <p className="mt-0.5 truncate text-sm font-medium">{s.buyer || "—"}</p>
                  <span className={`mt-1 inline-block rounded-full border px-2 py-0.5 text-[10px] font-medium ${badge(s.status)}`}>
                    {s.status}
                  </span>
                  <span className={`ml-1 inline-block rounded-full border px-2 py-0.5 text-[10px] font-medium ${emailBadge(s.email_status)}`}>
                    {emailLabel(s.email_status)}
                  </span>
                </div>
                <div className="flex shrink-0 flex-col items-end gap-1.5">
                  <span className="text-sm font-semibold">{money(s.total_sen)}</span>
                  {s.status === "paid" && canWrite && (
                    <button onClick={() => confirmSale(s.ref_code)} className="text-xs font-medium text-[var(--jf-primary)] hover:underline">
                      {t("common.confirm")}
                    </button>
                  )}
                </div>
              </li>
            ))}
            {sales.length === 0 && (
              <li className="px-4 py-10 text-center text-sm text-[var(--jf-muted)]">{t("dash.emptySales")}</li>
            )}
          </ul>

          {/* desktop: full table */}
          <div className="hidden overflow-x-auto md:block">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-[var(--jf-border)] text-left text-xs uppercase tracking-wide text-[var(--jf-muted)]">
                  <th className="px-5 py-2.5 font-medium">{t("dash.ref")}</th>
                  <th className="px-5 py-2.5 font-medium">{t("dash.buyer")}</th>
                  <th className="px-5 py-2.5 font-medium">{t("dash.amount")}</th>
                  <th className="px-5 py-2.5 font-medium">{t("dash.status")}</th>
                  <th className="px-5 py-2.5 font-medium">{t("dash.email")}</th>
                  <th className="px-5 py-2.5" />
                </tr>
              </thead>
              <tbody>
                {sales.map((s) => (
                  <tr key={s.ref_code} className="border-b border-[var(--jf-border)] last:border-0 hover:bg-slate-50/60">
                    <td className="whitespace-nowrap px-5 py-3 font-mono text-xs">{s.ref_code}</td>
                    <td className="max-w-40 truncate px-3 py-3">{s.buyer || "—"}</td>
                    <td className="whitespace-nowrap px-5 py-3 font-medium">{money(s.total_sen)}</td>
                    <td className="px-5 py-3">
                      <span className={`whitespace-nowrap rounded-full border px-2.5 py-0.5 text-[11px] font-medium ${badge(s.status)}`}>
                        {s.status}
                      </span>
                    </td>
                    <td className="px-5 py-3">
                      <span className={`whitespace-nowrap rounded-full border px-2.5 py-0.5 text-[11px] font-medium ${emailBadge(s.email_status)}`}>
                        {emailLabel(s.email_status)}
                      </span>
                    </td>
                    <td className="px-5 py-3 text-right">
                      {s.status === "paid" && canWrite && (
                        <button onClick={() => confirmSale(s.ref_code)} className="whitespace-nowrap text-xs font-medium text-[var(--jf-primary)] hover:underline">
                          {t("common.confirm")}
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
                {sales.length === 0 && (
                  <tr>
                    <td colSpan={6} className="px-5 py-10 text-center text-sm text-[var(--jf-muted)]">
                      {t("dash.emptySales")}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>

          {/* pagination */}
          {salesTotal > 0 && (
            <div className="flex items-center justify-between gap-3 border-t border-[var(--jf-border)] px-5 py-3">
              <span className="text-xs text-[var(--jf-muted)]">
                {t("dash.pageOf", { page: salesPage, total: salesTotalPages })}
              </span>
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={() => gotoSalesPage(salesPage - 1)}
                  disabled={salesPage <= 1}
                  className="rounded-md border border-[var(--jf-border)] px-2.5 py-1 text-xs text-slate-600 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  {t("common.prev")}
                </button>
                <button
                  type="button"
                  onClick={() => gotoSalesPage(salesPage + 1)}
                  disabled={salesPage >= salesTotalPages}
                  className="rounded-md border border-[var(--jf-border)] px-2.5 py-1 text-xs text-slate-600 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  {t("common.next")}
                </button>
              </div>
            </div>
          )}
        </section>
      </div>
    </Shell>
  );
}

function Stat({ label, value, accent }: { label: string; value: string; accent?: boolean }) {
  return (
    <div className="jf-card p-4">
      <p className="text-xs text-[var(--jf-muted)]">{label}</p>
      <p className={`mt-1 text-xl font-semibold tracking-tight ${accent ? "text-[var(--jf-primary)]" : ""}`}>
        {value}
      </p>
    </div>
  );
}
