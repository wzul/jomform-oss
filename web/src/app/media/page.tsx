"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { Shell } from "../../components/Shell";
import { useI18n } from "../../components/i18n";
import { useSession } from "../../components/session";
import { spaceQuery } from "../../lib/space";

// Issue #180: WordPress-media-library-style Media page. Grid of the
// merchant's uploaded images (thumbnail + name + date + size), filterable by
// form and source, with view/copy-URL and soft-delete (also removes the
// object from MinIO).

type MediaItem = {
  id: string;
  form_id?: string;
  key: string;
  url: string;
  mime: string;
  size: number;
  source: string;
  created_at: string;
};

type FormRow = { id: string; slug: string; title: string; status: string; url: string };

const PER_PAGE = 24;

// Human-readable file size (e.g. "1.2 MB").
function fmtSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

// Short date (e.g. "3 Sep 2026").
function fmtDate(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" });
}

// Filename from the object key (e.g. "u/<acct>/<uuid>.jpg" → "<uuid>.jpg").
function nameFromKey(key: string): string {
  const parts = key.split("/");
  return parts[parts.length - 1] || key;
}

export default function MediaPage() {
  const { t } = useI18n();
  const { anon, role, space } = useSession();
  const router = useRouter();

  // Issue #163: read_only members see no delete action.
  const canWrite = role !== "read_only";

  const [items, setItems] = useState<MediaItem[]>([]);
  const [total, setTotal] = useState(0);
  const [checking, setChecking] = useState(true);
  const [error, setError] = useState("");

  // filters
  const [formFilter, setFormFilter] = useState("");
  const [sourceFilter, setSourceFilter] = useState("");

  // pagination
  const [page, setPage] = useState(1);

  // forms for the filter dropdown
  const [forms, setForms] = useState<FormRow[]>([]);

  // detail modal
  const [detail, setDetail] = useState<MediaItem | null>(null);
  const [copied, setCopied] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const load = useCallback(async () => {
    const params = new URLSearchParams();
    params.set("page", String(page));
    params.set("limit", String(PER_PAGE));
    if (formFilter) params.set("form", formFilter);
    if (sourceFilter) params.set("source", sourceFilter);
    const r = await fetch(`/api/rest/media?${params.toString()}${spaceQuery()}`);
    if (r.ok) {
      const d = await r.json();
      setItems(Array.isArray(d.items) ? d.items : []);
      setTotal(typeof d.total === "number" ? d.total : 0);
    } else {
      setItems([]);
      setTotal(0);
    }
    setChecking(false);
  }, [page, formFilter, sourceFilter]);

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
    setChecking(true);
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [load, space]);

  // Reset to page 1 when filters change.
  useEffect(() => {
    setPage(1);
  }, [formFilter, sourceFilter]);

  // Auth bounce via router (client transition), not a full document reload.
  useEffect(() => {
    if (anon) router.replace("/landing");
  }, [anon, router]);

  const totalPages = Math.max(1, Math.ceil(total / PER_PAGE));
  const safePage = Math.min(page, totalPages);

  const countLabel = useMemo(() => t("media.countItems", { n: total }), [t, total]);

  async function copyUrl(url: string) {
    try {
      await navigator.clipboard.writeText(url);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard unavailable — ignore.
    }
  }

  async function deleteItem(m: MediaItem) {
    if (!window.confirm(t("media.deleteConfirm"))) return;
    setDeleting(true);
    setError("");
    try {
      const r = await fetch(`/api/rest/media/${m.id}${spaceQuery()}`, { method: "DELETE" });
      if (!r.ok) {
        setError(t("media.deleteFail"));
        return;
      }
      setDetail(null);
      load();
    } finally {
      setDeleting(false);
    }
  }

  if (checking) {
    return (
      <main className="flex min-h-screen items-center justify-center text-slate-400">…</main>
    );
  }

  return (
    <Shell active="media">
      <div className="w-full flex-1 px-6 py-8">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h1 className="max-w-xl text-xl font-semibold tracking-tight">{t("media.title")}</h1>
            <p className="max-w-xl text-sm text-[var(--jf-muted)]">{t("media.subtitle")}</p>
          </div>
        </div>

        {/* Filters */}
        <div className="mt-6 flex flex-wrap items-center gap-2">
          <select
            value={formFilter}
            onChange={(e) => setFormFilter(e.target.value)}
            className="jf-input w-56"
            aria-label={t("media.filterForm")}
          >
            <option value="">{t("media.filterForm")}</option>
            {forms.map((f) => (
              <option key={f.id} value={f.id}>
                {f.title}
              </option>
            ))}
          </select>
          <select
            value={sourceFilter}
            onChange={(e) => setSourceFilter(e.target.value)}
            className="jf-input w-44"
            aria-label={t("media.filterSource")}
          >
            <option value="">{t("media.filterSource")}</option>
            <option value="merchant">{t("media.sourceMerchant")}</option>
            <option value="customer">{t("media.sourceCustomer")}</option>
          </select>
          <span className="text-xs text-[var(--jf-muted)]">{countLabel}</span>
        </div>

        {error && <p className="mt-3 text-sm text-red-600">{error}</p>}

        {/* Grid */}
        {items.length === 0 ? (
          <div className="jf-card mt-6 p-10 text-center text-sm text-[var(--jf-muted)]">
            {t("media.empty")}
          </div>
        ) : (
          <div className="mt-6 grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
            {items.map((m) => (
              <button
                key={m.id}
                onClick={() => {
                  setDetail(m);
                  setCopied(false);
                }}
                className="jf-card group overflow-hidden text-left"
              >
                <div className="flex h-32 items-center justify-center overflow-hidden bg-slate-100">
                  {/* eslint-disable-next-line @next/next/no-img-element */}
                  <img
                    src={m.url}
                    alt={nameFromKey(m.key)}
                    className="h-full w-full object-cover"
                    loading="lazy"
                  />
                </div>
                <div className="p-3">
                  <p className="truncate text-sm font-medium" title={nameFromKey(m.key)}>
                    {nameFromKey(m.key)}
                  </p>
                  <p className="mt-0.5 text-xs text-[var(--jf-muted)]">
                    {fmtDate(m.created_at)} · {fmtSize(m.size)}
                  </p>
                  <p className="mt-0.5 text-[11px] text-[var(--jf-muted)]">
                    {m.source === "customer" ? t("media.sourceCustomer") : t("media.sourceMerchant")}
                  </p>
                </div>
              </button>
            ))}
          </div>
        )}

        {/* Pagination */}
        {totalPages > 1 && (
          <div className="mt-4 flex flex-wrap items-center justify-between gap-2 text-xs text-[var(--jf-muted)]">
            <span>
              {t("media.pageOf", { page: safePage, total: totalPages })} · {countLabel}
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

        {/* Detail modal */}
        {detail && (
          <div
            className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
            onClick={() => setDetail(null)}
          >
            <div
              className="w-full max-w-md overflow-hidden rounded-xl bg-white shadow-xl"
              onClick={(e) => e.stopPropagation()}
            >
              <div className="flex h-56 items-center justify-center overflow-hidden bg-slate-100">
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img src={detail.url} alt={nameFromKey(detail.key)} className="h-full w-full object-contain" />
              </div>
              <div className="p-4">
                <p className="truncate text-sm font-semibold" title={nameFromKey(detail.key)}>
                  {nameFromKey(detail.key)}
                </p>
                <p className="mt-1 text-xs text-[var(--jf-muted)]">
                  {fmtDate(detail.created_at)} · {fmtSize(detail.size)} · {detail.mime}
                </p>
                <p className="mt-0.5 text-xs text-[var(--jf-muted)]">
                  {detail.source === "customer" ? t("media.sourceCustomer") : t("media.sourceMerchant")}
                </p>
                <div className="mt-4 flex flex-wrap items-center gap-2">
                  <a
                    href={detail.url}
                    target="_blank"
                    rel="noreferrer"
                    className="jf-btn-ghost px-3 py-1.5 text-sm"
                  >
                    {t("media.view")}
                  </a>
                  <button
                    onClick={() => copyUrl(detail.url)}
                    className="jf-btn-ghost px-3 py-1.5 text-sm"
                  >
                    {copied ? t("media.copied") : t("media.copyUrl")}
                  </button>
                  {canWrite && (
                    <button
                      onClick={() => deleteItem(detail)}
                      disabled={deleting}
                      className="ml-auto rounded-lg border border-red-200 px-3 py-1.5 text-sm text-red-600 hover:bg-red-50 disabled:opacity-40"
                    >
                      {t("media.delete")}
                    </button>
                  )}
                </div>
              </div>
            </div>
          </div>
        )}
      </div>
    </Shell>
  );
}
