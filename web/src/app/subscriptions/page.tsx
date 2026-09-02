"use client";

// Issue #131: Subscriptions dashboard — list, filter by status, paginate and
// cancel merchant subscriptions. Cancel reuses the REST endpoint, which keeps
// the customer's card token (Wan rule) so the subscription can be re-activated.

import { useCallback, useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { Shell } from "../../components/Shell";
import { useI18n } from "../../components/i18n";
import { useSession } from "../../components/session";

type Subscription = {
  id: string;
  customer: string;
  product_id: string;
  product_name: string;
  amount_sen: number;
  period: string; // month | year
  status: string; // active | past_due | cancelled
  next_charge_at?: string;
  retry_attempts: number;
};

type ListResp = { items: Subscription[]; page: number; limit: number; total: number };

const PER_PAGE = 10;
const money = (sen: number) =>
  `RM${(sen / 100).toLocaleString("en-MY", { minimumFractionDigits: 2 })}`;

export default function Subscriptions() {
  const { t } = useI18n();
  const { anon } = useSession();
  const router = useRouter();

  const [rows, setRows] = useState<Subscription[]>([]);
  const [total, setTotal] = useState(0);
  const [checking, setChecking] = useState(true);
  const [error, setError] = useState("");

  const [statusFilter, setStatusFilter] = useState("all");
  const [page, setPage] = useState(1);
  const [busyId, setBusyId] = useState<string | null>(null);

  const load = useCallback(async (status: string, pageNum: number) => {
    const q = new URLSearchParams({ page: String(pageNum), limit: String(PER_PAGE) });
    if (status !== "all") q.set("status", status);
    const r = await fetch(`/api/rest/subscriptions?${q.toString()}`);
    if (!r.ok) {
      setError(t("sub.cancelErr"));
      setChecking(false);
      return;
    }
    const d: ListResp = await r.json();
    setRows(Array.isArray(d.items) ? d.items : []);
    setTotal(d.total ?? 0);
    setError("");
    setChecking(false);
  }, [t]);

  useEffect(() => {
    setPage(1);
    load("all", 1);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    load(statusFilter, page);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [statusFilter, page]);

  // Auth bounce via router (client transition), not a full document reload.
  useEffect(() => {
    if (anon) router.replace("/landing");
  }, [anon, router]);

  const totalPages = Math.max(1, Math.ceil(total / PER_PAGE));
  const safePage = Math.min(page, totalPages);

  const statusLabel = (s: string) =>
    s === "active"
      ? t("sub.active")
      : s === "past_due"
        ? t("sub.pastDue")
        : t("sub.cancelled");

  const statusBadge = (s: string) =>
    s === "active"
      ? "border border-emerald-200 bg-emerald-50 text-emerald-700"
      : s === "past_due"
        ? "border border-amber-200 bg-amber-50 text-amber-700"
        : "border border-slate-200 bg-slate-50 text-slate-500";

  async function cancelOne(sub: Subscription) {
    const ok = window.confirm(
      t("sub.cancelConfirm", { customer: sub.customer })
    );
    if (!ok) return;
    setBusyId(sub.id);
    setError("");
    try {
      const r = await fetch(`/api/rest/subscriptions/${sub.id}/cancel`, {
        method: "POST",
      });
      if (!r.ok) {
        const d = await r.json().catch(() => ({}));
        setError(d.error || t("sub.cancelErr"));
        return;
      }
      await load(statusFilter, safePage);
    } finally {
      setBusyId(null);
    }
  }

  if (checking) {
    return (
      <main className="flex min-h-screen items-center justify-center text-slate-400">
        …
      </main>
    );
  }

  return (
    <Shell active="subscriptions">
      <div className="w-full flex-1 px-6 py-8">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h1 className="max-w-xl text-xl font-semibold tracking-tight">
              {t("sub.title")}
            </h1>
            <p className="max-w-xl text-sm text-[var(--jf-muted)]">
              {t("sub.subtitle")}
            </p>
          </div>
        </div>

        {/* Status filter */}
        <div className="mt-4 flex flex-wrap items-center gap-2">
          <select
            value={statusFilter}
            onChange={(e) => setStatusFilter(e.target.value)}
            className="jf-input w-44"
            aria-label={t("sub.filterStatus")}
          >
            <option value="all">{t("sub.statusAll")}</option>
            <option value="active">{t("sub.active")}</option>
            <option value="past_due">{t("sub.pastDue")}</option>
            <option value="cancelled">{t("sub.cancelled")}</option>
          </select>
          <span className="ml-auto whitespace-nowrap text-xs text-[var(--jf-muted)]">
            {t("sub.countItems", { n: total })}
          </span>
        </div>
        {error && (
          <p className="mt-2 rounded-lg bg-red-50 px-3 py-2 text-xs text-red-600">
            {error}
          </p>
        )}

        {/* Table */}
        <div className="jf-card mt-4 overflow-x-auto">
          <table className="w-full min-w-[720px] text-sm">
            <thead>
              <tr className="border-b border-[var(--jf-border)] text-left">
                <th className="px-4 py-3 text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                  {t("sub.customer")}
                </th>
                <th className="px-2 py-3 text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                  {t("sub.product")}
                </th>
                <th className="px-2 py-3 text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                  {t("sub.amountPeriod")}
                </th>
                <th className="px-2 py-3 text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                  {t("dash.status")}
                </th>
                <th className="px-2 py-3 text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                  {t("sub.nextCharge")}
                </th>
                <th className="px-2 py-3 text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                  {t("sub.retryAttempts")}
                </th>
                <th className="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                  {t("bld.actions")}
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.map((sub) => (
                <tr
                  key={sub.id}
                  className="border-b border-[var(--jf-border)] last:border-0"
                >
                  <td className="px-4 py-3 font-medium text-[var(--jf-text)]">
                    {sub.customer}
                  </td>
                  <td className="px-2 py-3 text-[var(--jf-muted)]">
                    {sub.product_name || "—"}
                  </td>
                  <td className="px-2 py-3 font-semibold">
                    {money(sub.amount_sen)}{" "}
                    <span className="font-normal text-[var(--jf-muted)]">
                      ·{" "}
                      {sub.period === "year"
                        ? t("sub.year")
                        : t("sub.month")}
                    </span>
                  </td>
                  <td className="px-2 py-3">
                    <span
                      className={`inline-flex rounded-full px-2.5 py-0.5 text-[11px] font-medium ${statusBadge(sub.status)}`}
                    >
                      {statusLabel(sub.status)}
                    </span>
                  </td>
                  <td className="px-2 py-3 text-[var(--jf-muted)]">
                    {sub.next_charge_at
                      ? new Date(sub.next_charge_at).toLocaleDateString()
                      : "—"}
                  </td>
                  <td className="px-2 py-3 text-[var(--jf-muted)]">
                    {sub.retry_attempts > 0 ? sub.retry_attempts : "—"}
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex items-center justify-end gap-1">
                      {sub.status !== "cancelled" && (
                        <button
                          onClick={() => cancelOne(sub)}
                          disabled={busyId === sub.id}
                          title={t("sub.tokenRetained")}
                          className="jf-btn-ghost px-2.5 py-1 text-xs text-rose-600 hover:bg-rose-50 disabled:opacity-50"
                        >
                          {busyId === sub.id ? "…" : t("sub.cancel")}
                        </button>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
              {rows.length === 0 && (
                <tr>
                  <td
                    colSpan={7}
                    className="px-4 py-8 text-center text-sm text-[var(--jf-muted)]"
                  >
                    {t("sub.empty")}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>

        {/* Pagination */}
        {totalPages > 1 && (
          <div className="mt-4 flex flex-wrap items-center justify-between gap-2 text-xs text-[var(--jf-muted)]">
            <span>{t("sub.pageOf", { page: safePage, total: totalPages })}</span>
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
