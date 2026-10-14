"use client";

// Issue #131: Subscriptions dashboard — list, filter by status, paginate and
// cancel merchant subscriptions. Cancel reuses the REST endpoint, which keeps
// the customer's card token (Wan rule) so the subscription can be re-activated.

import { useCallback, useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { PageLoading } from "../../components/PageLoading";
import { Shell } from "../../components/Shell";
import { useI18n } from "../../components/i18n";
import { useSession } from "../../components/session";
import { formatDate } from "../../lib/formatDate";
import { workspaceQuery } from "../../lib/workspace";
import { fetchJson, runLoad } from "../../lib/wsfetch";
import { apiErr, translateApiErr } from "../../lib/apiErr";

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
  card_update_pending?: boolean;
};

type ListResp = { items: Subscription[]; page: number; limit: number; total: number };

const PER_PAGE = 10;
const money = (sen: number) =>
  `RM${(sen / 100).toLocaleString("en-MY", { minimumFractionDigits: 2 })}`;

export default function Subscriptions() {
  const { t } = useI18n();
  const { anon, role, workspace, user } = useSession();
  const router = useRouter();

  // Issue #163: read_only members see no write actions (cancel/update card).
  // manage + admin keep full write access.
  const canWrite = role !== "read_only";

  const [rows, setRows] = useState<Subscription[]>([]);
  const [total, setTotal] = useState(0);
  const [checking, setChecking] = useState(true);
  const [error, setError] = useState("");

  const [statusFilter, setStatusFilter] = useState("all");
  const [page, setPage] = useState(1);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [notice, setNotice] = useState("");

  // Issue #503: 401/403 → /login, the same redirect Shell.signOut() uses.
  const goToLogin = useCallback(() => {
    router.replace("/login");
    window.location.href = "/login";
  }, [router]);

  const load = useCallback(async (status: string, pageNum: number) => {
    const q = new URLSearchParams({ page: String(pageNum), limit: String(PER_PAGE) });
    if (status !== "all") q.set("status", status);
    const sp = workspaceQuery();
    const sep = sp ? "&" : "";
    // Issue #503: a non-ok response was reported as the CANCEL error string
    // (sub.cancelErr) and, worse, a 401 was never recognised — an expired
    // session just showed "failed to cancel". Route it through the shared
    // helper: 401/403 → /login, anything else → the visible message.
    const r = await runLoad(
      () => fetchJson<ListResp>(`/api/rest/subscriptions?${q.toString()}${sep}${sp.replace("?", "")}`),
      { onUnauth: goToLogin, message: t("sub.loadFail"), errorKey: (b: unknown) => translateApiErr(b, t) },
    );
    if (!r.ok) {
      setRows([]);
      setTotal(0);
      setError(r.error);
      setChecking(false);
      return;
    }
    setRows(Array.isArray(r.data.items) ? r.data.items : []);
    setTotal(r.data.total ?? 0);
    setError("");
    setChecking(false);
  }, [t, goToLogin]);

  useEffect(() => {
    setPage(1);
    load("all", 1);
    // Issue #170: `workspace` re-runs load when the active workspace changes so
    // the list re-fetches for the new workspace without a manual refresh.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [workspace]);

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
        : s === "completed"
          ? t("sub.completed")
          : t("sub.cancelled");

  const statusBadge = (s: string) =>
    s === "active"
      ? "border border-emerald-200 bg-emerald-50 text-emerald-700"
      : s === "past_due"
        ? "border border-amber-200 bg-amber-50 text-amber-700"
        : s === "completed"
          ? "border border-sky-200 bg-sky-50 text-sky-700"
          : "border border-slate-200 bg-slate-50 text-slate-500";

  async function cancelOne(sub: Subscription) {
    const ok = window.confirm(
      t("sub.cancelConfirm", { customer: sub.customer })
    );
    if (!ok) return;
    setBusyId(sub.id);
    setError("");
    try {
      const r = await fetch(`/api/rest/subscriptions/${sub.id}/cancel${workspaceQuery()}`, {
        method: "POST",
      });
      if (!r.ok) {
        const d = await r.json().catch(() => ({}));
        setError(apiErr(d, t("sub.cancelErr"), t));
        return;
      }
      await load(statusFilter, safePage);
    } finally {
      setBusyId(null);
    }
  }

  // Issue #154: merchant-initiated card update — creates a new CHIP token
  // purchase and opens the checkout so the customer can authorize a new card.
  // The subscription keeps its schedule; the old token is retained (Wan rule).
  async function updateCardOne(sub: Subscription) {
    const ok = window.confirm(
      t("sub.updateCardConfirm", { customer: sub.customer })
    );
    if (!ok) return;
    setBusyId(sub.id);
    setError("");
    try {
      const r = await fetch(`/api/rest/subscriptions/${sub.id}/update-card${workspaceQuery()}`, {
        method: "POST",
      });
      if (!r.ok) {
        const d = await r.json().catch(() => ({}));
        setError(apiErr(d, t("sub.updateCardErr"), t));
        return;
      }
      const d = await r.json();
      if (d && d.checkout_url) {
        window.open(d.checkout_url, "_blank", "noopener,noreferrer");
      }
      setNotice(t("sub.updateCardSent"));
      await load(statusFilter, safePage);
    } finally {
      setBusyId(null);
    }
  }

  // In-flight: render inside the shell (see components/PageLoading.tsx) so the
  // sidebar and top bar stay mounted; the bare `<main>` this replaced dropped
  // the whole app chrome while the subscriptions fetch was outstanding.
  if (checking) {
    return <PageLoading active="subscriptions" />;
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
            <option value="completed">{t("sub.completed")}</option>
            <option value="cancelled">{t("sub.cancelled")}</option>
          </select>
          <span className="ml-auto whitespace-nowrap text-xs text-[var(--jf-muted)]">
            {t("sub.countItems", { n: total })}
          </span>
        </div>
        {error && (
          <p role="alert" className="mt-2 rounded-lg bg-red-50 px-3 py-2 text-xs text-red-700">
            {error}
            {/* Issue #503: a failed load gets an action, not a dead end. */}
            <button
              type="button"
              onClick={() => void load(statusFilter, safePage)}
              className="ml-2 underline decoration-dotted underline-offset-2"
            >
              {t("common.retry")}
            </button>
          </p>
        )}
        {notice && (
          <p className="mt-2 rounded-lg bg-emerald-50 px-3 py-2 text-xs text-emerald-700">
            {notice}
          </p>
        )}

        {/* Table */}
        <div className="jf-card mt-4 overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-[var(--jf-border)] text-left">
                <th className="px-4 py-3 text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                  {t("sub.customer")}
                </th>
                <th className="hidden px-2 py-3 text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)] sm:table-cell">
                  {t("sub.product")}
                </th>
                <th className="px-2 py-3 text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                  {t("sub.amountPeriod")}
                </th>
                <th className="px-2 py-3 text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                  {t("dash.status")}
                </th>
                <th className="hidden px-2 py-3 text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)] sm:table-cell">
                  {t("sub.nextCharge")}
                </th>
                <th className="hidden px-2 py-3 text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)] sm:table-cell">
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
                  <td className="hidden px-2 py-3 text-[var(--jf-muted)] sm:table-cell">
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
                    {sub.card_update_pending && (
                      <div className="mt-1">
                        <span
                          className="inline-flex rounded-full border border-rose-200 bg-rose-50 px-2.5 py-0.5 text-[11px] font-medium text-rose-700"
                          title={t("sub.cardUpdatePaused")}
                        >
                          {t("sub.cardNeedsUpdate")}
                        </span>
                      </div>
                    )}
                  </td>
                  <td className="hidden px-2 py-3 text-[var(--jf-muted)] sm:table-cell">
                    {sub.next_charge_at
                      ? formatDate(sub.next_charge_at, user?.timezone)
                      : "—"}
                  </td>
                  <td className="hidden px-2 py-3 text-[var(--jf-muted)] sm:table-cell">
                    {sub.retry_attempts > 0 ? sub.retry_attempts : "—"}
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex items-center justify-end gap-1">
                      {canWrite && sub.status !== "cancelled" && (
                        <>
                          <button
                            onClick={() => updateCardOne(sub)}
                            disabled={busyId === sub.id}
                            title={t("sub.updateCard")}
                            className="jf-btn-ghost px-2.5 py-1 text-xs text-sky-700 hover:bg-sky-50 disabled:opacity-50"
                          >
                            {busyId === sub.id ? "…" : t("sub.updateCard")}
                          </button>
                          <button
                            onClick={() => cancelOne(sub)}
                            disabled={busyId === sub.id}
                            title={t("sub.tokenRetained")}
                            className="jf-btn-ghost px-2.5 py-1 text-xs text-rose-700 hover:bg-rose-50 disabled:opacity-50"
                          >
                            {busyId === sub.id ? "…" : t("sub.cancel")}
                          </button>
                        </>
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
