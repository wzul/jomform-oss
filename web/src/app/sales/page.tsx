"use client";

// Issue #126: dedicated Sales page (Gravity-Forms-Entries-style).
// One page lists every sale across the merchant's forms with a per-form &
// status filter, search, date range, pagination, a row detail drawer
// (buyer / items / amounts / status), confirm/capture/release actions for
// skip-capture sales, and CSV export. Deep-linked from the forms list /
// builder via /sales?form=<id> which pre-applies the form filter.
//
// Mobile-first table approach mirrors #122/#59: the table scrolls
// horizontally inside a card on small screens (min-width), and the controls
// wrap into a clean stack.

import { Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Shell } from "../../components/Shell";
import { useI18n, type Key } from "../../components/i18n";
import { useSession } from "../../components/session";
import { formatDate, formatDateTime } from "../../lib/formatDate";
import { workspaceQuery } from "../../lib/workspace";

type Item = {
  product_id: string;
  price_sen: number;
  qty: number;
  name: string;
  // Issue #153: which variation was bought (empty for simple products).
  variation_id?: string;
  variation_name?: string;
};

type Sale = {
  ref_code: string;
  status: string;
  total_sen: number;
  payment_method: string;
  buyer: string;
  created_at: string;
  skip_capture?: boolean;
  authorized?: boolean;
  captured_at?: string;
  email_status?: string;
  form_id?: string;
  sale_type?: string;
  invoice_no?: string;
  buyer_email?: string;
  buyer_phone?: string;
  subtotal_sen?: number;
  shipping_sen?: number;
  discount_sen?: number;
  items?: Item[];
  updated_at?: string;
};

type SalesPage = { items: Sale[]; page: number; limit: number; total: number };
type FormRow = { id: string; slug: string; title: string; status: string };

const PAGE_SIZE = 15;

/* an uncaptured skip_capture authorization (issue #12) — CapturedAt is only
   set once the merchant captures; released sales go cancelled. */
const isCapturePending = (s: Sale) =>
  !!s.skip_capture &&
  !s.captured_at &&
  (s.status === "pending" || s.status === "authorized" || s.authorized === true);

const STATUSES = ["pending", "paid", "confirmed", "failed", "cancelled", "refunded"] as const;

const money = (sen: number) =>
  `RM${(sen / 100).toLocaleString("en-MY", { minimumFractionDigits: 2 })}`;

/* same status badge colors as the dashboard Recent sales table */
const saleBadge = (s: string) =>
  ({
    pending: "bg-amber-50 text-amber-700 border-amber-200",
    paid: "bg-blue-50 text-blue-700 border-blue-200",
    confirmed: "bg-emerald-50 text-emerald-700 border-emerald-200",
    failed: "bg-red-50 text-red-700 border-red-200",
    cancelled: "bg-slate-100 text-slate-500 border-slate-200",
    refunded: "bg-orange-50 text-orange-700 border-orange-200",
  })[s] || "bg-slate-100 text-slate-500 border-slate-200";

/* email delivery status badge colors (issue #194) */
const emailStatusBadge = (e: string) =>
  ({
    delivered: "bg-emerald-50 text-emerald-700 border-emerald-200",
    bounce: "bg-red-50 text-red-700 border-red-200",
    complaint: "bg-orange-50 text-orange-700 border-orange-200",
    open: "bg-blue-50 text-blue-700 border-blue-200",
    click: "bg-violet-50 text-violet-700 border-violet-200",
  })[e] || "bg-slate-100 text-slate-500 border-slate-200";

/* localized label for an email delivery event type (issue #194) */
const emailStatusLabel = (e: string, t: (k: Key) => string) =>
  ({
    delivered: t("dash.emailDelivered"),
    bounce: t("dash.emailBounced"),
    complaint: t("dash.emailComplaint"),
    open: t("dash.emailOpened"),
    click: t("dash.emailClicked"),
  })[e] || e;

function SalesPageInner() {
  const { t } = useI18n();
  const { anon, role, space, user } = useSession();
  const router = useRouter();
  const search = useSearchParams();
  const deepForm = search.get("form") || "";

  // Issue #163: read_only members see no write actions (capture/confirm).
  // manage + admin keep full write access.
  const canWrite = role !== "read_only";

  // forms for the filter dropdown (fetched from /api/rest/forms)
  const [forms, setForms] = useState<FormRow[]>([]);

  // filters — server-driven (mirror #122 structure but paging is server-side)
  const [formFilter, setFormFilter] = useState<string>(deepForm);
  const [statusFilter, setStatusFilter] = useState<string>("");
  const [dateFrom, setDateFrom] = useState<string>("");
  const [dateTo, setDateTo] = useState<string>("");
  const [q, setQ] = useState<string>("");
  const [page, setPage] = useState(1);

  // data
  const [rows, setRows] = useState<Sale[]>([]);
  const [total, setTotal] = useState(0);
  const [checking, setChecking] = useState(true);
  const [err, setErr] = useState("");

  // actions
  const [busyRef, setBusyRef] = useState<string | null>(null);
  const [msg, setMsg] = useState("");

  // row detail drawer
  const [detail, setDetail] = useState<Sale | null>(null);
  const drawerRef = useRef<HTMLDivElement>(null);
  const closeBtnRef = useRef<HTMLButtonElement>(null);

  // form id -> title lookup for the "form" column
  const formName = useMemo(() => {
    const m = new Map<string, string>();
    for (const f of forms) m.set(f.id, f.title);
    return m;
  }, [forms]);

  const loadForms = useCallback(async () => {
    const f = await fetch(`/api/rest/forms${workspaceQuery()}`).then((r) => (r.ok ? r.json() : []));
    setForms(Array.isArray(f) ? f : []);
  }, []);

  // Load forms on mount (and re-fetch when the active space changes, #170).
  useEffect(() => {
    void loadForms();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [space]);

  // Reset pagination whenever filters change.
  useEffect(() => {
    setPage(1);
  }, [formFilter, statusFilter, dateFrom, dateTo, q]);

  const loadSales = useCallback(
    async (p: number) => {
      setChecking(true);
      setErr("");
      const qs = new URLSearchParams({ page: String(p), limit: String(PAGE_SIZE) });
      if (formFilter) qs.set("form_id", formFilter);
      if (statusFilter) qs.set("status", statusFilter);
      if (dateFrom) qs.set("date_from", dateFrom);
      if (dateTo) qs.set("date_to", dateTo);
      if (q.trim()) qs.set("q", q.trim());
      const sp = workspaceQuery();
      const sep = sp ? "&" : "";
      try {
        const r = await fetch(`/api/rest/sales?${qs.toString()}${sep}${sp.replace("?", "")}`);
        if (!r.ok) {
          setRows([]);
          setTotal(0);
          setErr(t("bld.loadSalesErr"));
          return;
        }
        const d = (await r.json()) as SalesPage;
        setRows(Array.isArray(d.items) ? d.items : []);
        setTotal(d.total || 0);
      } catch {
        setRows([]);
        setTotal(0);
        setErr(t("bld.loadSalesErr"));
      } finally {
        setChecking(false);
      }
    },
    [formFilter, statusFilter, dateFrom, dateTo, q, t],
  );

  // Deep-link: when /sales?form=<id>, the form dropdown mirrors it.
  useEffect(() => {
    if (deepForm) setFormFilter(deepForm);
  }, [deepForm]);

  // Fetch whenever filters/page change (server-side paging + filtering).
  // Issue #170: `space` re-runs loadSales when the active space changes so the
  // sales list re-fetches for the new space without a manual refresh.
  useEffect(() => {
    void loadSales(page);
  }, [page, loadSales, space]);

  // Auth bounce via router (client transition), not a full reload.
  useEffect(() => {
    if (anon) router.replace("/landing");
  }, [anon, router]);

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const safePage = Math.min(page, totalPages);

  // ── actions: confirm / capture / release (reuse existing REST endpoints) ──
  async function confirmSale(ref: string) {
    setBusyRef(ref);
    setMsg("");
    try {
      const r = await fetch(`/api/rest/sales/${encodeURIComponent(ref)}/confirm${workspaceQuery()}`, {
        method: "POST",
      });
      setMsg(r.ok ? `${ref} ✓` : (await r.json().catch(() => ({}))).error || "Gagal");
      if (r.ok) await loadSales(safePage);
    } finally {
      setBusyRef(null);
    }
  }

  async function captureSale(ref: string) {
    const raw = window.prompt(
      `Capture amount in RM for ${ref} (empty = full authorized amount):`,
    );
    if (raw === null) return; // cancelled
    const rm = raw.trim();
    let body = "";
    if (rm !== "") {
      const rmNum = Number(rm);
      if (!Number.isFinite(rmNum) || rmNum <= 0) {
        setMsg("Jumlah capture tidak sah.");
        return;
      }
      body = JSON.stringify({ amount_sen: Math.round(rmNum * 100) });
    }
    setBusyRef(ref);
    setMsg("");
    try {
      const r = await fetch(`/api/rest/sales/${encodeURIComponent(ref)}/capture${workspaceQuery()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        ...(body ? { body } : {}),
      });
      const d = await r.json().catch(() => ({}));
      setMsg(r.ok ? `${t("bld.capture")} ${ref} ✓` : d.error || `Capture gagal ${ref}.`);
      if (r.ok) await loadSales(safePage);
    } finally {
      setBusyRef(null);
    }
  }

  async function releaseSale(ref: string) {
    if (!window.confirm(`Release authorization ${ref}? The buyer's card will NOT be charged.`))
      return;
    setBusyRef(ref);
    setMsg("");
    try {
      const r = await fetch(`/api/rest/sales/${encodeURIComponent(ref)}/release${workspaceQuery()}`, {
        method: "POST",
      });
      const d = await r.json().catch(() => ({}));
      setMsg(r.ok ? `Authorization ${ref} released. ✓` : d.error || `Release gagal ${ref}.`);
      if (r.ok) await loadSales(safePage);
    } finally {
      setBusyRef(null);
    }
  }

  // Issue #407: full or partial refund against a paid/confirmed sale.
  async function refundSale(ref: string) {
    const raw = window.prompt(
      `Refund amount in RM for ${ref} (empty = full refund):`,
    );
    if (raw === null) return; // cancelled
    const rm = raw.trim();
    let body = "";
    if (rm !== "") {
      const rmNum = Number(rm);
      if (!Number.isFinite(rmNum) || rmNum <= 0) {
        setMsg(t("sales.refundInvalid"));
        return;
      }
      body = JSON.stringify({ amount_sen: Math.round(rmNum * 100) });
    }
    if (!window.confirm(`Refund ${ref}? This cannot be undone.`)) return;
    setBusyRef(ref);
    setMsg("");
    try {
      const r = await fetch(`/api/rest/sales/${encodeURIComponent(ref)}/refund${workspaceQuery()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        ...(body ? { body } : {}),
      });
      const d = await r.json().catch(() => ({}));
      setMsg(r.ok ? `${t("sales.refund")} ${ref} ✓` : d.error || `Refund gagal ${ref}.`);
      if (r.ok) await loadSales(safePage);
    } finally {
      setBusyRef(null);
    }
  }

  async function captureAllPending() {
    const awaiting = rows.filter(isCapturePending);
    if (awaiting.length === 0) return;
    const rm = (awaiting.reduce((s, x) => s + x.total_sen, 0) / 100).toFixed(2);
    if (!window.confirm(`Capture ALL ${awaiting.length} pending authorization(s) — total RM${rm}?`))
      return;
    setBusyRef("__all__");
    setMsg("");
    try {
      const r = await fetch(`/api/rest/sales/bulk-capture${workspaceQuery()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ refs: awaiting.map((s) => s.ref_code) }),
      });
      const d = await r.json().catch(() => ({}));
      if (!r.ok) {
        setMsg(d.error || t("bld.bulkCaptureFail"));
        return;
      }
      setMsg(
        t("bld.bulkCaptureDone", {
          captured: String(d.captured),
          requested: String(d.requested),
          failed: String(d.failed),
        }) + (d.failed ? t("bld.bulkCaptureCheck") : " ✓"),
      );
      await loadSales(safePage);
    } finally {
      setBusyRef(null);
    }
  }

  function clearFilters() {
    setFormFilter("");
    setStatusFilter("");
    setDateFrom("");
    setDateTo("");
    setQ("");
    setPage(1);
  }

  // CSV export — same endpoint the builder uses, honors the form filter.
  const exportUrl = useMemo(() => {
    const qs = new URLSearchParams();
    if (formFilter) qs.set("form_id", formFilter);
    const sp = workspaceQuery();
    const sep = sp ? "&" : "";
    return `/api/rest/sales/export.csv?${qs.toString()}${sep}${sp.replace("?", "")}`;
  }, [formFilter]);

  // drawer focus management (mirrors Shell #104: focus + Escape + Tab trap)
  useEffect(() => {
    if (!detail) return;
    closeBtnRef.current?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setDetail(null);
        return;
      }
      if (e.key !== "Tab") return;
      const focusables = drawerRef.current?.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), select, input, textarea, [tabindex]:not([tabindex="-1"])',
      );
      if (!focusables || focusables.length === 0) return;
      const first = focusables[0];
      const last = focusables[focusables.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [detail]);

  const countLabel = total > 0 ? `${total} ${t("dash.records")}` : t("sales.empty");
  const hasFilters = !!(formFilter || statusFilter || dateFrom || dateTo || q.trim());

  return (
    <Shell active="sales">
      <div className="w-full flex-1 px-6 py-8">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h1 className="max-w-xl text-xl font-semibold tracking-tight">{t("sales.title")}</h1>
            <p className="max-w-xl text-sm text-[var(--jf-muted)]">{t("sales.subtitle")}</p>
          </div>
        </div>

        {/* Filter / control bar */}
        <div className="jf-card mt-6 flex flex-wrap items-end gap-2 p-4">
          <select
            value={formFilter}
            onChange={(e) => setFormFilter(e.target.value)}
            aria-label={t("sales.filterForm")}
            className="jf-input w-56"
          >
            <option value="">{t("sales.allForms")}</option>
            {forms.map((f) => (
              <option key={f.id} value={f.id}>
                {f.title}
              </option>
            ))}
          </select>
          <select
            value={statusFilter}
            onChange={(e) => setStatusFilter(e.target.value)}
            aria-label={t("sales.filterStatus")}
            className="jf-input w-40"
          >
            <option value="">{t("sales.allStatuses")}</option>
            {STATUSES.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
          <label className="text-xs text-[var(--jf-muted)]">
            {t("sales.dateFrom")}
            <input
              type="date"
              value={dateFrom}
              onChange={(e) => setDateFrom(e.target.value)}
              className="jf-input mt-0.5 block w-40"
            />
          </label>
          <label className="text-xs text-[var(--jf-muted)]">
            {t("sales.dateTo")}
            <input
              type="date"
              value={dateTo}
              onChange={(e) => setDateTo(e.target.value)}
              className="jf-input mt-0.5 block w-40"
            />
          </label>
          <input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder={t("sales.search")}
            aria-label={t("sales.search")}
            className="jf-input min-w-40 flex-1 sm:max-w-xs"
          />
          <div className="ml-auto flex flex-wrap items-center gap-2">
            {hasFilters && (
              <button
                onClick={clearFilters}
                className="jf-btn-ghost px-3 py-1.5 text-xs"
                disabled={checking}
              >
                {t("sales.clearFilters")}
              </button>
            )}
            {canWrite && rows.some(isCapturePending) && (
              <button
                onClick={captureAllPending}
                disabled={busyRef !== null}
                className="rounded-md bg-[#5e6ad2] px-3 py-1.5 text-xs font-medium text-white hover:bg-[#828fff] disabled:cursor-not-allowed disabled:opacity-40"
              >
                {busyRef === "__all__" ? "…" : `${t("sales.captureAll")} (${rows.filter(isCapturePending).length})`}
              </button>
            )}
            <a
              href={exportUrl}
              className="jf-btn-primary px-3 py-1.5 text-xs"
              title={t("sales.exportNote")}
            >
              {t("common.exportCsv")}
            </a>
          </div>
        </div>

        {msg && <p className="mt-3 text-xs text-[var(--jf-primary)]">{msg}</p>}

        {/* Table */}
        <div className="jf-card mt-4 overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-[var(--jf-border)] text-left text-[10px] uppercase tracking-wide text-[var(--jf-muted)]">
                <th className="px-4 py-3 font-medium">{t("dash.ref")}</th>
                <th className="px-2 py-3 font-medium">{t("dash.buyer")}</th>
                <th className="hidden px-2 py-3 font-medium sm:table-cell">{t("sales.items")}</th>
                <th className="px-2 py-3 font-medium">{t("dash.amount")}</th>
                <th className="px-2 py-3 font-medium">{t("dash.status")}</th>
                <th className="hidden px-2 py-3 font-medium sm:table-cell">{t("dash.email")}</th>
                <th className="hidden px-2 py-3 font-medium sm:table-cell">{t("sales.form")}</th>
                <th className="hidden px-2 py-3 font-medium sm:table-cell">{t("prod.date")}</th>
                <th className="px-4 py-3 text-right font-medium">{t("sales.actions")}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((s) => (
                <tr key={s.ref_code} className="border-b border-[var(--jf-border)] last:border-0 hover:bg-slate-50/60">
                  <td className="whitespace-nowrap px-4 py-3 font-mono text-[11px]">{s.ref_code}</td>
                  <td className="max-w-32 truncate px-2 py-3 text-xs">
                    {s.buyer || "—"}
                    {s.buyer_email && (
                      <span className="block truncate text-[10px] text-[var(--jf-muted)]">{s.buyer_email}</span>
                    )}
                  </td>
                  <td className="hidden max-w-40 truncate px-2 py-3 text-xs text-[var(--jf-muted)] sm:table-cell">
                    {Array.isArray(s.items) && s.items.length
                      ? s.items.map((i) => `${i.qty}× ${i.name}${i.variation_name ? ` (${i.variation_name})` : ""}`).join(", ")
                      : "—"}
                  </td>
                  <td className="whitespace-nowrap px-2 py-3 text-xs font-semibold">{money(s.total_sen)}</td>
                  <td className="whitespace-nowrap px-2 py-3">
                    <span className={`rounded-full border px-2 py-0.5 text-[10px] font-medium ${saleBadge(s.status)}`}>
                      {s.status}
                    </span>
                  </td>
                  <td className="hidden whitespace-nowrap px-2 py-3 sm:table-cell">
                    {s.email_status ? (
                      <span className={`rounded-full border px-2 py-0.5 text-[10px] font-medium ${emailStatusBadge(s.email_status)}`}>
                        {emailStatusLabel(s.email_status, t)}
                      </span>
                    ) : (
                      <span className="text-[10px] text-[var(--jf-muted)]">{t("dash.emailNotSent")}</span>
                    )}
                  </td>
                  <td className="hidden max-w-32 truncate px-2 py-3 text-xs text-[var(--jf-muted)] sm:table-cell">
                    {s.form_id ? formName.get(s.form_id) || "—" : "—"}
                  </td>
                  <td className="hidden whitespace-nowrap px-2 py-3 text-[11px] text-[var(--jf-muted)] sm:table-cell">
                    {formatDate(s.created_at, user?.timezone)}
                  </td>
                  <td className="whitespace-nowrap px-4 py-3">
                    <div className="flex items-center justify-end gap-1.5">
                      <button
                        onClick={() => setDetail(s)}
                        className="jf-btn-ghost px-2.5 py-1 text-xs"
                      >
                        {t("sales.viewDetail")}
                      </button>
                      {isCapturePending(s) ? (
                        <>
                          <button
                            onClick={() => captureSale(s.ref_code)}
                            disabled={busyRef !== null}
                            className="rounded-md bg-[#5e6ad2] px-2 py-1 text-[10px] font-medium text-white hover:bg-[#828fff] disabled:cursor-not-allowed disabled:opacity-40"
                          >
                            {busyRef === s.ref_code ? "…" : t("bld.capture")}
                          </button>
                          <button
                            onClick={() => releaseSale(s.ref_code)}
                            disabled={busyRef !== null}
                            className="rounded-md border border-[var(--jf-border)] px-2 py-1 text-[10px] text-slate-600 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-40"
                          >
                            {t("bld.release")}
                          </button>
                        </>
                      ) : canWrite && (s.status === "paid" || s.status === "confirmed") ? (
                        <button
                          onClick={() => refundSale(s.ref_code)}
                          disabled={busyRef !== null}
                          className="rounded-md border border-[var(--jf-border)] px-2 py-1 text-[10px] text-slate-600 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-40"
                        >
                          {busyRef === s.ref_code ? "…" : t("sales.refund")}
                        </button>
                      ) : (
                        <span className="text-[10px] text-[var(--jf-muted)]">—</span>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
              {!checking && rows.length === 0 && (
                <tr>
                  <td colSpan={9} className="px-4 py-10 text-center text-sm text-[var(--jf-muted)]">
                    {err || t("sales.empty")}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
          {checking && (
            <div className="px-4 py-10 text-center text-sm text-[var(--jf-muted)]">
              {t("sales.loading")}
            </div>
          )}
        </div>

        {/* Pagination */}
        {totalPages > 1 && (
          <div className="mt-4 flex flex-wrap items-center justify-between gap-2 text-xs text-[var(--jf-muted)]">
            <span>
              {t("prod.pageOf", { page: safePage, total: totalPages })} · {countLabel}
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

        {/* Row detail drawer */}
        {detail && (
          <div className="fixed inset-0 z-50 flex justify-end bg-black/30" onClick={() => setDetail(null)}>
            <div
              ref={drawerRef}
              className="h-full w-full max-w-lg overflow-y-auto bg-white p-6 shadow-xl md:max-w-xl"
              onClick={(e) => e.stopPropagation()}
              role="dialog"
              aria-label={t("sales.detail")}
            >
              <div className="flex items-center justify-between">
                <h2 className="text-lg font-semibold">
                  {t("sales.detail")} · <span className="font-mono text-sm">{detail.ref_code}</span>
                </h2>
                <button
                  ref={closeBtnRef}
                  onClick={() => setDetail(null)}
                  className="text-lg text-[var(--jf-muted)]"
                  aria-label={t("sales.close")}
                >
                  ✕
                </button>
              </div>

              <div className="mt-3">
                <span className={`rounded-full border px-2.5 py-0.5 text-xs font-medium ${saleBadge(detail.status)}`}>
                  {detail.status}
                </span>
                {isCapturePending(detail) && (
                  <span className="ml-2 text-xs text-amber-600">{t("sales.unauthorized")}</span>
                )}
                {detail.authorized && detail.captured_at && (
                  <span className="ml-2 text-xs text-emerald-600">{t("sales.authorized")}</span>
                )}
              </div>

              {/* Buyer */}
              <section className="mt-5">
                <h3 className="text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                  {t("sales.buyerInfo")}
                </h3>
                <dl className="mt-2 space-y-1 text-sm">
                  <div className="flex justify-between gap-4">
                    <dt className="text-[var(--jf-muted)]">Name</dt>
                    <dd className="font-medium">{detail.buyer || "—"}</dd>
                  </div>
                  <div className="flex justify-between gap-4">
                    <dt className="text-[var(--jf-muted)]">Email</dt>
                    <dd className="truncate">{detail.buyer_email || "—"}</dd>
                  </div>
                  <div className="flex justify-between gap-4">
                    <dt className="text-[var(--jf-muted)]">Phone</dt>
                    <dd>{detail.buyer_phone || "—"}</dd>
                  </div>
                  <div className="flex justify-between gap-4">
                    <dt className="text-[var(--jf-muted)]">{t("sales.form")}</dt>
                    <dd className="truncate">{detail.form_id ? formName.get(detail.form_id) || "—" : "—"}</dd>
                  </div>
                  <div className="flex justify-between gap-4">
                    <dt className="text-[var(--jf-muted)]">{t("sales.saleType")}</dt>
                    <dd>{detail.sale_type === "subscription" ? t("prod.subscription") : t("prod.oneTime")}</dd>
                  </div>
                  {detail.invoice_no && (
                    <div className="flex justify-between gap-4">
                      <dt className="text-[var(--jf-muted)]">{t("sales.invoiceNo")}</dt>
                      <dd className="font-mono text-xs">{detail.invoice_no}</dd>
                    </div>
                  )}
                </dl>
              </section>

              {/* Items */}
              <section className="mt-5">
                <h3 className="text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                  {t("sales.items")}
                </h3>
                {Array.isArray(detail.items) && detail.items.length ? (
                  <table className="mt-2 w-full text-sm">
                    <thead>
                      <tr className="border-b border-[var(--jf-border)] text-left text-[10px] uppercase tracking-wide text-[var(--jf-muted)]">
                        <th className="py-1 font-medium">Item</th>
                        <th className="py-1 text-right font-medium">{t("sales.qty")}</th>
                        <th className="py-1 text-right font-medium">{t("dash.amount")}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {detail.items.map((i, idx) => (
                        <tr key={idx} className="border-b border-[var(--jf-border)] last:border-0">
                          <td className="py-1.5">
                            {i.name}
                            {i.variation_name ? (
                              <span className="ml-1 text-xs text-[var(--jf-muted)]">({i.variation_name})</span>
                            ) : null}
                          </td>
                          <td className="py-1.5 text-right text-[var(--jf-muted)]">{i.qty}</td>
                          <td className="py-1.5 text-right font-medium">{money(i.price_sen * i.qty)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                ) : (
                  <p className="mt-2 text-sm text-[var(--jf-muted)]">{t("sales.noItems")}</p>
                )}
              </section>

              {/* Totals */}
              <section className="mt-5 rounded-lg bg-slate-50 p-3 text-sm">
                <div className="flex justify-between gap-4">
                  <span className="text-[var(--jf-muted)]">{t("dash.amount")}</span>
                  <span>{money(detail.total_sen)}</span>
                </div>
                {typeof detail.shipping_sen === "number" && detail.shipping_sen !== 0 && (
                  <div className="mt-1 flex justify-between gap-4 text-[var(--jf-muted)]">
                    <span>{t("sales.shipping")}</span>
                    <span>{money(detail.shipping_sen)}</span>
                  </div>
                )}
                {typeof detail.discount_sen === "number" && detail.discount_sen !== 0 && (
                  <div className="mt-1 flex justify-between gap-4 text-[var(--jf-muted)]">
                    <span>{t("sales.discount")}</span>
                    <span>−{money(detail.discount_sen)}</span>
                  </div>
                )}
                <div className="mt-1 flex justify-between gap-4 font-semibold">
                  <span>{t("sales.total")}</span>
                  <span>{money(detail.total_sen)}</span>
                </div>
              </section>

              {/* Payment + status */}
              <section className="mt-5">
                <h3 className="text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                  {t("sales.payment")}
                </h3>
                <div className="mt-2 space-y-1 text-sm">
                  <div className="flex justify-between gap-4">
                    <dt className="text-[var(--jf-muted)]">{t("sales.method")}</dt>
                    <dd>{detail.payment_method || "—"}</dd>
                  </div>
                  <div className="flex justify-between gap-4">
                    <dt className="text-[var(--jf-muted)]">{t("dash.ref")}</dt>
                    <dd className="font-mono text-xs">{detail.ref_code}</dd>
                  </div>
                  <div className="flex justify-between gap-4">
                    <dt className="text-[var(--jf-muted)]">Created</dt>
                    <dd>{formatDateTime(detail.created_at, user?.timezone)}</dd>
                  </div>
                  {detail.captured_at && (
                    <div className="flex justify-between gap-4">
                      <dt className="text-[var(--jf-muted)]">Captured</dt>
                      <dd>{formatDateTime(detail.captured_at, user?.timezone)}</dd>
                    </div>
                  )}
                </div>
              </section>

              {/* Email delivery timeline (issue #194) */}
              <section className="mt-5">
                <h3 className="text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                  {t("del.timeline")}
                </h3>
                {detail.email_status ? (
                  <div className="mt-2 space-y-1 text-sm">
                    <div className="flex justify-between gap-4">
                      <dt className="text-[var(--jf-muted)]">{t("dash.email")}</dt>
                      <dd>
                        <span className={`rounded-full border px-2 py-0.5 text-[10px] font-medium ${emailStatusBadge(detail.email_status)}`}>
                          {emailStatusLabel(detail.email_status, t)}
                        </span>
                      </dd>
                    </div>
                    <p className="text-xs text-[var(--jf-muted)]">{t("dash.emailStatusNote")}</p>
                  </div>
                ) : (
                  <p className="mt-2 text-sm text-[var(--jf-muted)]">{t("dash.emailNotSent")}</p>
                )}
              </section>

              {/* Actions */}
              <div className="mt-6 flex justify-end gap-2 border-t border-[var(--jf-border)] pt-4">
                {canWrite && isCapturePending(detail) && (
                  <>
                    <button
                      onClick={() => releaseSale(detail.ref_code)}
                      disabled={busyRef !== null}
                      className="jf-btn-ghost px-3 py-1.5 text-sm"
                    >
                      {t("bld.release")}
                    </button>
                    <button
                      onClick={() => captureSale(detail.ref_code)}
                      disabled={busyRef !== null}
                      className="jf-btn-primary disabled:opacity-40"
                    >
                      {busyRef === detail.ref_code ? "…" : t("bld.capture")}
                    </button>
                  </>
                )}
                {canWrite && detail.status !== "confirmed" && (
                  <button
                    onClick={() => confirmSale(detail.ref_code)}
                    disabled={busyRef !== null || isCapturePending(detail)}
                    className="jf-btn-ghost px-3 py-1.5 text-sm"
                  >
                    {t("sales.confirmSale")}
                  </button>
                )}
                {canWrite && (detail.status === "paid" || detail.status === "confirmed") && (
                  <button
                    onClick={() => refundSale(detail.ref_code)}
                    disabled={busyRef !== null}
                    className="jf-btn-ghost px-3 py-1.5 text-sm"
                  >
                    {busyRef === detail.ref_code ? "…" : t("sales.refund")}
                  </button>
                )}
              </div>
            </div>
          </div>
        )}
      </div>
    </Shell>
  );
}

export default function SalesPage() {
  return (
    <Suspense fallback={<Shell active="sales"><div className="px-6 py-8 text-sm text-[var(--jf-muted)]">…</div></Shell>}>
      <SalesPageInner />
    </Suspense>
  );
}
