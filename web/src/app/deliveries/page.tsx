"use client";

// Issue #194: Email delivery status view. One page lists every email delivery
// event (delivered / bounce / complaint / open / click) across the merchant's
// forms, filterable by form and event type, with pagination. Each row shows
// the email, event type, sale ref, form, timestamp and detail. A per-sale
// timeline (Sent → Delivered → Opened → Clicked, or bounce/complaint) is
// shown in a row detail drawer.

import { useCallback, useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { Shell } from "../../components/Shell";
import { useI18n } from "../../components/i18n";
import { useSession } from "../../components/session";
import { spaceQuery } from "../../lib/space";

type Delivery = {
  id: number;
  email: string;
  event_type: string;
  bounce_type?: string;
  sale_ref?: string;
  form_id?: string;
  detail?: Record<string, unknown>;
  created_at: string;
};

type DeliveryPage = { items: Delivery[]; page: number; limit: number; total: number };
type FormRow = { id: string; slug: string; title: string; status: string };

const PER_PAGE = 20;
const EVENT_TYPES = ["delivered", "bounce", "complaint", "open", "click"] as const;

// Event-type badge colors (mirror the dashboard email status palette).
const eventBadge = (e: string) =>
  ({
    delivered: "bg-emerald-50 text-emerald-700 border-emerald-200",
    bounce: "bg-red-50 text-red-700 border-red-200",
    complaint: "bg-orange-50 text-orange-700 border-orange-200",
    open: "bg-blue-50 text-blue-700 border-blue-200",
    click: "bg-violet-50 text-violet-700 border-violet-200",
  })[e] || "bg-slate-100 text-slate-500 border-slate-200";

// Short date-time (e.g. "3 Sep 2026, 14:05").
function fmtDateTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString(undefined, {
    day: "numeric",
    month: "short",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export default function DeliveriesPage() {
  const { t } = useI18n();
  const { anon, space } = useSession();
  const router = useRouter();

  const [rows, setRows] = useState<Delivery[]>([]);
  const [total, setTotal] = useState(0);
  const [checking, setChecking] = useState(true);
  const [err, setErr] = useState("");

  // filters
  const [formFilter, setFormFilter] = useState("");
  const [statusFilter, setStatusFilter] = useState("");

  // pagination
  const [page, setPage] = useState(1);

  // forms for the filter dropdown
  const [forms, setForms] = useState<FormRow[]>([]);

  // row detail drawer (per-sale timeline)
  const [detail, setDetail] = useState<Delivery | null>(null);

  const formName = useMemo(() => {
    const m = new Map<string, string>();
    for (const f of forms) m.set(f.id, f.title);
    return m;
  }, [forms]);

  const load = useCallback(async () => {
    setChecking(true);
    setErr("");
    const params = new URLSearchParams();
    params.set("page", String(page));
    params.set("limit", String(PER_PAGE));
    if (formFilter) params.set("form_id", formFilter);
    if (statusFilter) params.set("event_type", statusFilter);
    try {
      const sp = spaceQuery();
      const sep = sp ? "&" : "";
      const r = await fetch(`/api/rest/email-deliveries?${params.toString()}${sep}${sp.replace("?", "")}`);
      if (!r.ok) {
        setRows([]);
        setTotal(0);
        setErr(t("del.loadErr"));
        return;
      }
      const d = (await r.json()) as DeliveryPage;
      setRows(Array.isArray(d.items) ? d.items : []);
      setTotal(typeof d.total === "number" ? d.total : 0);
    } catch {
      setRows([]);
      setTotal(0);
      setErr(t("del.loadErr"));
    } finally {
      setChecking(false);
    }
  }, [page, formFilter, statusFilter, t]);

  // Load forms for the filter dropdown once.
  useEffect(() => {
    fetch(`/api/rest/forms${spaceQuery()}`)
      .then((r) => (r.ok ? r.json() : []))
      .then((f) => setForms(Array.isArray(f) ? f : []))
      .catch(() => {});
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [space]);

  // Issue #170: re-fetch when the active space changes.
  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [load, space]);

  // Reset to page 1 when filters change.
  useEffect(() => {
    setPage(1);
  }, [formFilter, statusFilter]);

  // Auth bounce via router (client transition), not a full document reload.
  useEffect(() => {
    if (anon) router.replace("/landing");
  }, [anon, router]);

  const totalPages = Math.max(1, Math.ceil(total / PER_PAGE));
  const safePage = Math.min(page, totalPages);
  const countLabel = useMemo(() => t("del.countItems", { n: total }), [t, total]);

  // Per-sale timeline: fetch the full event history for the selected sale.
  const [timeline, setTimeline] = useState<Delivery[] | null>(null);
  const [timelineLoading, setTimelineLoading] = useState(false);

  async function openTimeline(saleRef: string) {
    setTimelineLoading(true);
    setTimeline(null);
    try {
      const params = new URLSearchParams();
      params.set("sale_ref", saleRef);
      params.set("limit", "100");
      const sp = spaceQuery();
      const sep = sp ? "&" : "";
      const r = await fetch(`/api/rest/email-deliveries?${params.toString()}${sep}${sp.replace("?", "")}`);
      const d = (await r.json()) as DeliveryPage;
      setTimeline(Array.isArray(d.items) ? d.items : []);
    } catch {
      setTimeline([]);
    } finally {
      setTimelineLoading(false);
    }
  }

  return (
    <Shell active="deliveries">
      <div className="w-full flex-1 px-6 py-8">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h1 className="max-w-xl text-xl font-semibold tracking-tight">{t("del.title")}</h1>
            <p className="max-w-xl text-sm text-[var(--jf-muted)]">{t("del.subtitle")}</p>
          </div>
        </div>

        {/* Filters */}
        <div className="mt-6 flex flex-wrap items-center gap-2">
          <select
            value={formFilter}
            onChange={(e) => setFormFilter(e.target.value)}
            className="jf-input w-56"
            aria-label={t("del.filterForm")}
          >
            <option value="">{t("del.allForms")}</option>
            {forms.map((f) => (
              <option key={f.id} value={f.id}>
                {f.title}
              </option>
            ))}
          </select>
          <select
            value={statusFilter}
            onChange={(e) => setStatusFilter(e.target.value)}
            className="jf-input w-48"
            aria-label={t("del.filterStatus")}
          >
            <option value="">{t("del.allStatuses")}</option>
            {EVENT_TYPES.map((et) => (
              <option key={et} value={et}>
                {et}
              </option>
            ))}
          </select>
        </div>

        {/* Table */}
        <div className="jf-card mt-4 overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-[var(--jf-border)] text-left text-[10px] uppercase tracking-wide text-[var(--jf-muted)]">
                <th className="px-4 py-3 font-medium">{t("del.colEmail")}</th>
                <th className="hidden px-2 py-3 font-medium sm:table-cell">{t("del.colEvent")}</th>
                <th className="px-2 py-3 font-medium">{t("del.colSale")}</th>
                <th className="hidden px-2 py-3 font-medium sm:table-cell">{t("del.colForm")}</th>
                <th className="hidden px-2 py-3 font-medium sm:table-cell">{t("del.colTime")}</th>
                <th className="px-4 py-3 text-right font-medium">{t("sales.actions")}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((d) => (
                <tr
                  key={d.id}
                  className="border-b border-[var(--jf-border)] last:border-0 hover:bg-slate-50/60"
                >
                  <td className="max-w-48 truncate px-4 py-3 text-xs">{d.email}</td>
                  <td className="hidden whitespace-nowrap px-2 py-3 sm:table-cell">
                    <span className={`rounded-full border px-2 py-0.5 text-[10px] font-medium ${eventBadge(d.event_type)}`}>
                      {d.event_type}
                    </span>
                  </td>
                  <td className="whitespace-nowrap px-2 py-3 font-mono text-[11px]">
                    {d.sale_ref || "—"}
                  </td>
                  <td className="hidden max-w-32 truncate px-2 py-3 text-xs text-[var(--jf-muted)] sm:table-cell">
                    {d.form_id ? formName.get(d.form_id) || "—" : "—"}
                  </td>
                  <td className="hidden whitespace-nowrap px-2 py-3 text-[11px] text-[var(--jf-muted)] sm:table-cell">
                    {fmtDateTime(d.created_at)}
                  </td>
                  <td className="whitespace-nowrap px-4 py-3 text-right">
                    {d.sale_ref ? (
                      <button
                        onClick={() => {
                          setDetail(d);
                          void openTimeline(d.sale_ref!);
                        }}
                        className="jf-btn-ghost px-2.5 py-1 text-xs"
                      >
                        {t("del.timeline")}
                      </button>
                    ) : (
                      <span className="text-[10px] text-[var(--jf-muted)]">—</span>
                    )}
                  </td>
                </tr>
              ))}
              {!checking && rows.length === 0 && (
                <tr>
                  <td colSpan={6} className="px-4 py-10 text-center text-sm text-[var(--jf-muted)]">
                    {err || t("del.empty")}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
          {checking && (
            <div className="px-4 py-10 text-center text-sm text-[var(--jf-muted)]">
              {t("del.loading")}
            </div>
          )}
        </div>

        {/* Pagination */}
        {totalPages > 1 && (
          <div className="mt-4 flex flex-wrap items-center justify-between gap-2 text-xs text-[var(--jf-muted)]">
            <span>
              {t("del.pageOf", { page: safePage, total: totalPages })} · {countLabel}
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

        {/* Row detail drawer — per-sale email timeline */}
        {detail && (
          <div className="fixed inset-0 z-50 flex justify-end bg-black/30" onClick={() => setDetail(null)}>
            <div
              className="h-full w-full max-w-lg overflow-y-auto bg-white p-6 shadow-xl md:max-w-xl"
              onClick={(e) => e.stopPropagation()}
              role="dialog"
              aria-label={t("del.timeline")}
            >
              <div className="flex items-center justify-between">
                <h2 className="text-lg font-semibold">
                  {t("del.timeline")} · <span className="font-mono text-sm">{detail.sale_ref}</span>
                </h2>
                <button
                  onClick={() => setDetail(null)}
                  className="text-lg text-[var(--jf-muted)]"
                  aria-label={t("sales.close")}
                >
                  ✕
                </button>
              </div>

              <div className="mt-3 text-sm">
                <div className="flex justify-between gap-4">
                  <dt className="text-[var(--jf-muted)]">{t("del.colEmail")}</dt>
                  <dd className="truncate">{detail.email}</dd>
                </div>
                <div className="mt-1 flex justify-between gap-4">
                  <dt className="text-[var(--jf-muted)]">{t("del.colEvent")}</dt>
                  <dd>
                    <span className={`rounded-full border px-2 py-0.5 text-[10px] font-medium ${eventBadge(detail.event_type)}`}>
                      {detail.event_type}
                    </span>
                  </dd>
                </div>
                {detail.bounce_type && (
                  <div className="mt-1 flex justify-between gap-4">
                    <dt className="text-[var(--jf-muted)]">{t("del.bounceType")}</dt>
                    <dd>
                      {detail.bounce_type === "Permanent" ? t("del.permanent") : t("del.transient")}
                    </dd>
                  </div>
                )}
                <div className="mt-1 flex justify-between gap-4">
                  <dt className="text-[var(--jf-muted)]">{t("del.colTime")}</dt>
                  <dd>{fmtDateTime(detail.created_at)}</dd>
                </div>
              </div>

              {/* Timeline */}
              <section className="mt-6">
                <h3 className="text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                  {t("del.timeline")}
                </h3>
                {timelineLoading ? (
                  <p className="mt-2 text-sm text-[var(--jf-muted)]">{t("del.loading")}</p>
                ) : timeline && timeline.length > 0 ? (
                  <ol className="mt-3 space-y-3">
                    {timeline.map((ev) => (
                      <li key={ev.id} className="flex items-start gap-3">
                        <span
                          className={`mt-0.5 rounded-full border px-2 py-0.5 text-[10px] font-medium ${eventBadge(ev.event_type)}`}
                        >
                          {ev.event_type}
                        </span>
                        <div className="text-xs">
                          <div className="text-[var(--jf-muted)]">{fmtDateTime(ev.created_at)}</div>
                          {ev.bounce_type && (
                            <div className="text-[var(--jf-muted)]">
                              {ev.bounce_type === "Permanent" ? t("del.permanent") : t("del.transient")}
                            </div>
                          )}
                          {ev.detail && Object.keys(ev.detail).length > 0 && (
                            <div className="mt-0.5 break-words text-[var(--jf-muted)]">
                              {JSON.stringify(ev.detail)}
                            </div>
                          )}
                        </div>
                      </li>
                    ))}
                  </ol>
                ) : (
                  <p className="mt-2 text-sm text-[var(--jf-muted)]">{t("del.noEvents")}</p>
                )}
              </section>
            </div>
          </div>
        )}
      </div>
    </Shell>
  );
}
