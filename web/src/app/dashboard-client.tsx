"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { Shell } from "../components/Shell";
import { SessionFault } from "../components/SessionFault";
import { useI18n } from "../components/i18n";
import { useSession } from "../components/session";
import { activeWorkspaceScope, workspaceQuery } from "../lib/workspace";
import { formEntriesCountLabel, paidConfirmedCount } from "../lib/dashboardStats";
import { answersPreview, rowKey, rowKind } from "../lib/salesRows";
import { currencySymbol, formatCurrencyTotals, formatMoney, normaliseCurrency } from "../lib/money";
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
  // The currency the sale was RECORDED in (the API emits it from the sale row
  // itself, never from the workspace's current setting). Optional because a
  // response row has no money; formatMoney then falls back to MYR, which is
  // what the pre-currency rows were charged in.
  currency?: string;
  payment_method: string;
  buyer: string;
  created_at: string;
  email_status?: string;
  // The bundle's recent list is the account's record set, so it can carry a
  // response entry too: the API marks it sale_type "response" and gives it the
  // submission id + recorded answers instead of a payment reference.
  sale_type?: string;
  submission_id?: string;
  data?: Record<string, unknown> | null;
};
type SalesPage = { items: SaleRow[]; page: number; limit: number; total: number };
type FormStats = {
  title: string;
  slug: string;
  // Issue #598: the form's collection mode, so the counter is labelled for what
  // it actually counts (see src/lib/dashboardStats.ts).
  mode?: string;
  submissions: number;
  paid_count: number;
  confirmed_count: number;
  failed_count: number;
  pending_count: number;
  revenue_sen: number;
  // Issue #185: the same NET revenue split by the currency each sale was
  // recorded in, so the panel can label US$ for a USD workspace instead of
  // printing a literal RM over the sum.
  revenue_by_currency?: Record<string, number>;
};
type ProductStats = {
  product_id: string;
  name: string;
  units_sold: number;
  paid_units: number;
  revenue_sen: number;
  paid_revenue_sen: number;
  // Issue #185: per-currency splits of the two figures above.
  revenue_by_currency?: Record<string, number>;
  paid_revenue_by_currency?: Record<string, number>;
};

type Summary = {
  counts: Record<string, number>;
  revenue_sen: number;
  // Issue #185: the NET revenue grouped by the currency each sale was recorded
  // in, plus the scope's own currency for a figure with no money behind it yet.
  revenue_by_currency?: Record<string, number>;
  currency?: string;
};

const PAGE_SIZE = 10;

// One colour per currency band in the 14-day chart. Design-system tokens only
// (the raw-hex gate forbids literals); a fourth currency wraps around, which is
// an acceptable rarity — the tooltip names the currency either way.
const SERIES_COLORS = ["var(--jf-primary)", "var(--jf-accent)", "var(--jf-chart-axis)"];

export default function Dashboard() {
  const { t } = useI18n();
  // Issue #29: me + landing data (products, summary, sales, analytics) all
  // come from the single dashboard-bundle round-trip fetched once per
  // session — the 914ms me→5-fetch waterfall is gone.
  const { bundle, anon, sessionError, role, workspace } = useSession();
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
      const sp = activeWorkspaceScope();
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

  // 14-day revenue series, one band per currency.
  //
  // Issue #185: this used to sum every row into one `value` and let the
  // Y axis print "RM${v}", so a workspace that had recorded sales in more than
  // one currency charted an invented total under the wrong symbol. Each band
  // keeps the currency of the sale it came from; the product never adds across
  // currencies (that is the conversion it refuses to do silently).
  const { series, bands } = (() => {
    const paid = sales.filter((s) => s.status === "paid" || s.status === "confirmed");
    if (!paid.length) return { series: [] as Record<string, number | string>[], bands: [] as string[] };
    const bands: string[] = [];
    const byDay = new Map<string, Map<string, number>>();
    for (const s of paid) {
      const cur = normaliseCurrency(s.currency);
      if (!bands.includes(cur)) bands.push(cur);
      const day = s.created_at.slice(0, 10);
      const dayMap = byDay.get(day) ?? new Map<string, number>();
      dayMap.set(cur, (dayMap.get(cur) ?? 0) + s.total_sen);
      byDay.set(day, dayMap);
    }
    const days: Record<string, number | string>[] = [];
    for (let i = 13; i >= 0; i--) {
      const d = new Date();
      d.setDate(d.getDate() - i);
      const key = d.toISOString().slice(0, 10);
      const row: Record<string, number | string> = { day: key, dayLabel: key.slice(5) };
      for (const cur of bands) {
        // Recharts plots numbers, so the band is major units with 2 decimals.
        row[`v_${cur}`] = Math.round(((byDay.get(key)?.get(cur) ?? 0) / 100) * 100) / 100;
      }
      days.push(row);
    }
    return { series: days, bands };
  })();

  async function confirmSale(ref: string) {
    await fetch(`/api/rest/sales/${ref}/confirm`, { method: "POST" });
    await loadSales(salesPage);
  }

  // Issue #48: resolve anon BEFORE the loading shell so an anonymous visitor
  // (an authoritative 401 on the session) lands on the Landing page instead
  // of hanging on the spinner. `bundle` stays undefined while loading.
  //
  // Card t_48319be9: an UNRESOLVED bundle after the read settled (`sessionError`)
  // is a FAULT (5xx / dropped connection), not a sign-out — so it is shown as a
  // failure with a retry, never as the landing page. The old 5s wedge collapsed
  // this state into `anon`, which is what bounced a slow (but valid) session to
  // /landing under contention.
  if (anon) return <Landing />;
  if (!ready) {
    // A settled session-bootstrap FAULT is rendered by the ONE shared surface
    // (card t_d1c9a43a) — the same component the app chrome shows on every other
    // dashboard route, so the sentence and the retry cannot drift between `/`
    // and the rest of the app. See components/SessionFault.tsx.
    if (sessionError) return <SessionFault />;
    return <main className="flex min-h-screen items-center justify-center text-[var(--jf-muted)]">…</main>;
  }

  // Card t_10266084. `cancelled` sits on `bg-slate-100`, where `text-slate-500`
  // (#62748e) measures 4.349:1 — under the 4.5:1 floor for this 10px badge;
  // `--jf-muted-on-tint` (#5b6472) is 5.460:1 on the same tint. Invisible to
  // `mutedText.test.ts` until the shared reader learned to follow a local helper.
  //
  // Card t_df0857c6. This map's FALLBACK was the empty string, so any sale status
  // the map did not name painted the wrapper's classes only — a pill with no
  // background and no text colour. `authorized` and `refunded` are both legal
  // sales statuses (`sales_status_check`, migration 000002) and both reach this
  // table: measured on prod (v1.21.30) the `refunded` pill painted rgb(23,24,28)
  // on #ffffff while `/sales` paints the same sale with the orange entry below.
  // So this helper now has the shape its siblings have: `refunded` gets the entry
  // `/sales` uses, and anything the map does not name gets the design system's
  // on-tint pair instead of an uncoloured pill. `src/lib/statusBadgeArms.test.ts`
  // reads both helpers against the DB's own CHECK list so neither can drift back.
  const badge = (s: string) =>
    ({
      pending: "bg-amber-50 text-amber-700 border-amber-200",
      paid: "bg-blue-50 text-blue-700 border-blue-200",
      confirmed: "bg-emerald-50 text-emerald-700 border-emerald-200",
      failed: "bg-red-50 text-red-700 border-red-200",
      cancelled: "bg-slate-100 text-[var(--jf-muted-on-tint)] border-slate-200",
      refunded: "bg-orange-50 text-orange-700 border-orange-200",
    })[s] || "bg-slate-100 text-[var(--jf-muted-on-tint)] border-slate-200";

  // issue #106: per-sale receipt email status label + badge colour.
  const emailLabel = (s?: string) =>
    ({
      delivered: t("dash.emailDelivered"),
      open: t("dash.emailOpened"),
      click: t("dash.emailClicked"),
      bounce: t("dash.emailBounced"),
      complaint: t("dash.emailComplaint"),
    })[s || ""] || (s ? s : t("dash.emailNotSent"));
  // Card t_10266084. The FALLBACK (an email status the map does not name — e.g.
  // a bare "sent", or none at all) sits on `bg-slate-100`, where `text-slate-500`
  // (#62748e) measures 4.349:1, under the 4.5:1 floor for this 10px/11px badge.
  // `--jf-muted-on-tint` (#5b6472) measures 5.460:1 on the same tint.
  const emailBadge = (s?: string) =>
    ({
      open: "bg-emerald-50 text-emerald-700 border-emerald-200",
      click: "bg-emerald-50 text-emerald-700 border-emerald-200",
      delivered: "bg-blue-50 text-blue-700 border-blue-200",
      bounce: "bg-red-50 text-red-700 border-red-200",
      complaint: "bg-red-50 text-red-700 border-red-200",
    })[s || ""] || "bg-slate-100 text-[var(--jf-muted-on-tint)] border-slate-200";

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
            <Stat label={t("dash.revenue")} value={formatCurrencyTotals(summary.revenue_by_currency, summary.currency)} accent />
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
                  {bands.map((cur, i) => (
                    <linearGradient key={cur} id={`rev-${i}`} x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor={SERIES_COLORS[i % SERIES_COLORS.length]} stopOpacity={0.25} />
                      <stop offset="100%" stopColor={SERIES_COLORS[i % SERIES_COLORS.length]} stopOpacity={0} />
                    </linearGradient>
                  ))}
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="var(--jf-chart-grid)" vertical={false} />
                <XAxis dataKey="dayLabel" tick={{ fontSize: 11, fill: "var(--jf-chart-axis)" }} axisLine={false} tickLine={false} />
                <YAxis tick={{ fontSize: 11, fill: "var(--jf-chart-axis)" }} axisLine={false} tickLine={false} width={48}
                       tickFormatter={(v) => (bands.length === 1 ? `${currencySymbol(bands[0])}${v}` : String(v))} />
                <Tooltip
                  formatter={(v, name) => [
                    // Chart values are major units; formatMoney takes minor units.
                    formatMoney(Math.round(Number(v) * 100), String(name)),
                    bands.length > 1 ? String(name) : t("dash.revenue"),
                  ]}
                  labelFormatter={(l) => String(l)}
                  contentStyle={{ borderRadius: 8, borderColor: "var(--jf-border)", fontSize: 12 }}
                />
                {bands.map((cur, i) => (
                  <Area
                    key={cur}
                    type="monotone"
                    dataKey={`v_${cur}`}
                    name={cur}
                    // NOT stacked: a stack's height is the SUM of its bands, and
                    // bands here are different currencies — the top of such a
                    // stack would be a number nobody can spend. Each band's
                    // height is its own currency's revenue for that day.
                    stroke={SERIES_COLORS[i % SERIES_COLORS.length]}
                    strokeWidth={2}
                    fill={`url(#rev-${i})`}
                  />
                ))}
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </section>

        {/* Form analytics */}
        <section className="jf-card mt-6 p-5">
          <h2 className="text-sm font-semibold">
            {t("dash.formAnalytics")}
            {/* Issue #598 / AGENTS.md docs-parity rule 4: the inline "?" that
                explains what each per-form counter counts. t_d768342e: the
                section moved to the sales guide, so the link follows it. */}
            <a
              href="/docs/sales#analytics"
              target="_blank"
              rel="noopener noreferrer"
              title={t("dash.formAnalyticsDocs")}
              className="ml-1.5 align-middle font-mono text-jf-2xs text-[var(--jf-primary)] hover:underline"
            >
              ?
            </a>
          </h2>
          <div className="mt-3 grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
            {formStats.map((f) => (
              <div key={f.slug} className="rounded-lg border border-[var(--jf-border)] p-3">
                <p className="truncate text-sm font-medium" title={f.title}>{f.title}</p>
                <p className="font-mono text-jf-2xs text-[var(--jf-muted)]">{activeWorkspaceScope() || "app"}.jomform.com/{f.slug}</p>
                <div className="mt-2 grid grid-cols-2 gap-x-2 gap-y-0.5 text-jf-2xs text-[var(--jf-muted)]">
                  {/* Issue #598: the figure is "entries recorded for this form"
                      — submissions for a response form, sales rows for a
                      payment form — so the label follows the mode. */}
                  <span>{t(formEntriesCountLabel(f.mode))} <b>{f.submissions}</b></span>
                  {/* Issue #598: paid_count ALREADY includes confirmed sales, so
                      adding confirmed_count counted every confirmed sale twice.
                      The union is the honest figure for this label. */}
                  <span>{t("dash.paidConfirmed")} <b className="text-emerald-700">{paidConfirmedCount(f)}</b></span>
                  <span>{t("dash.pendingLabel")} <b>{f.pending_count}</b></span>
                  <span>{t("dash.failedLabel")} <b>{f.failed_count}</b></span>
                </div>
                <p className="mt-1.5 text-sm font-semibold text-[var(--jf-primary)]">{formatCurrencyTotals(f.revenue_by_currency, summary?.currency)}</p>
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
                  <p className="text-jf-2xs text-[var(--jf-muted)]">
                    {pr.units_sold} {t("dash.sold")} · {pr.paid_units} {t("dash.paid")}
                  </p>
                </div>
                <div className="text-right">
                  <p className="text-sm font-semibold">{formatCurrencyTotals(pr.paid_revenue_by_currency, summary?.currency)}</p>
                  <p className="text-jf-3xs text-[var(--jf-muted)]">{t("dash.gross")} {formatCurrencyTotals(pr.revenue_by_currency, summary?.currency)}</p>
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
          <p className="border-b border-[var(--jf-border)] px-5 py-2 text-jf-2xs text-[var(--jf-muted)]">
            {t("dash.emailStatusNote")}
          </p>
          {/* mobile: stacked cards (no table scroll needed) */}
          <ul className="divide-y divide-[var(--jf-border)] md:hidden">
            {sales.map((s) => (
              <li key={rowKey(s)} className="flex items-center justify-between gap-3 px-4 py-3">
                <div className="min-w-0">
                  <p className="truncate font-mono text-xs text-[var(--jf-muted)]">
                    {rowKind(s) === "response" ? (s.submission_id || "").slice(0, 8) : s.ref_code}
                  </p>
                  <p className="mt-0.5 truncate text-sm font-medium">{s.buyer || "—"}</p>
                  {rowKind(s) === "response" && answersPreview(s) && (
                    <p className="truncate text-jf-3xs text-[var(--jf-muted)]">{answersPreview(s)}</p>
                  )}
                  <span className={`mt-1 inline-block rounded-full border px-2 py-0.5 text-jf-3xs font-medium ${badge(s.status)}`}>
                    {s.status}
                  </span>
                  {rowKind(s) !== "response" && (
                    <span className={`ml-1 inline-block rounded-full border px-2 py-0.5 text-jf-3xs font-medium ${emailBadge(s.email_status)}`}>
                      {emailLabel(s.email_status)}
                    </span>
                  )}
                </div>
                <div className="flex shrink-0 flex-col items-end gap-1.5">
                  <span className="text-sm font-semibold">
                    {rowKind(s) === "response" ? "—" : formatMoney(s.total_sen, s.currency)}
                  </span>
                  {s.status === "paid" && rowKind(s) !== "response" && canWrite && (
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
                  <tr key={rowKey(s)} className="border-b border-[var(--jf-border)] last:border-0 hover:bg-slate-50/60">
                    <td className="whitespace-nowrap px-5 py-3 font-mono text-xs">
                      {rowKind(s) === "response" ? (
                        <span className="rounded-full border border-indigo-200 bg-indigo-50 px-2 py-0.5 text-jf-3xs font-medium text-[var(--jf-primary-on-tint)]">
                          {t("bld.responses")}
                        </span>
                      ) : (
                        s.ref_code
                      )}
                    </td>
                    <td className="max-w-40 truncate px-3 py-3">{s.buyer || "—"}</td>
                    <td className="whitespace-nowrap px-5 py-3 font-medium">
                      {rowKind(s) === "response" ? "—" : formatMoney(s.total_sen, s.currency)}
                    </td>
                    <td className="px-5 py-3">
                      <span className={`whitespace-nowrap rounded-full border px-2.5 py-0.5 text-jf-2xs font-medium ${badge(s.status)}`}>
                        {s.status}
                      </span>
                    </td>
                    <td className="px-5 py-3">
                      {rowKind(s) === "response" ? (
                        <span className="text-jf-2xs text-[var(--jf-muted)]">
                          {answersPreview(s) || "—"}
                        </span>
                      ) : (
                        <span className={`whitespace-nowrap rounded-full border px-2.5 py-0.5 text-jf-2xs font-medium ${emailBadge(s.email_status)}`}>
                          {emailLabel(s.email_status)}
                        </span>
                      )}
                    </td>
                    <td className="px-5 py-3 text-right">
                      {s.status === "paid" && rowKind(s) !== "response" && canWrite && (
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
