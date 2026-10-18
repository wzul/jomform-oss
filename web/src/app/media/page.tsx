"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { PageLoading } from "../../components/PageLoading";
import { Shell } from "../../components/Shell";
import { useI18n } from "../../components/i18n";
import { useSession } from "../../components/session";
import { formatDate } from "../../lib/formatDate";
import { workspaceQuery } from "../../lib/workspace";
import { fetchJson, runLoad } from "../../lib/wsfetch";
import { apiErr, translateApiErr } from "../../lib/apiErr";
import { panelDomId, tabDomId, tabStop, tablistKeyDown } from "../../lib/tablist";

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
  // Issue #382: generated image-size variant URLs (thumbnail/medium/full) for
  // raster images. The grid renders the thumbnail; the detail modal offers
  // all sizes.
  sizes?: Record<string, string>;
  // Issue #383: auto-generated first-page preview thumbnail URL for a PDF.
  // The grid renders this instead of the generic PDF tile when present.
  preview?: string;
  created_at: string;
};

// Issue #384: per-workspace storage usage returned by the media list endpoint.
type MediaUsage = {
  used?: number;
  limit?: number;
};

// A row in the Trash view (GET /api/rest/media/trash). `object_present` is the
// honest half of a restore: the row and the object have independent lives, so a
// file whose bytes were removed while it sat in the bin cannot be restored and
// the UI must say so rather than offer a button that fails.
type TrashedMedia = {
  id: string;
  key: string;
  url: string;
  mime: string;
  size: number;
  form_id?: string;
  deleted_at: string;
  object_present: boolean;
};

type FormRow = { id: string; slug: string; title: string; status: string; url: string };

const PER_PAGE = 24;

// Human-readable file size (e.g. "1.2 MB").
function fmtSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

// Issue #384: human-readable size for the storage usage bar (handles GB).
function fmtBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

// Filename from the object key (e.g. "u/<acct>/<uuid>.jpg" → "<uuid>.jpg").
function nameFromKey(key: string): string {
  const parts = key.split("/");
  return parts[parts.length - 1] || key;
}

// Issue #361: media items now include merchant code files (CSS/JS/HTML/SVG/
// woff2) alongside images. isImage decides whether to render a real thumbnail
// or a code-file tile (extension badge) for the media library grid.
function isImage(mime: string): boolean {
  return mime.startsWith("image/");
}

// Extension badge label for a code file (e.g. "CSS", "JS", "HTML") or a PDF
// (issue #382).
function extBadge(mime: string): string {
  if (mime === "application/pdf") return "PDF";
  if (mime === "text/css; charset=utf-8") return "CSS";
  if (mime === "text/javascript; charset=utf-8" || mime === "application/javascript; charset=utf-8") return "JS";
  if (mime === "text/html; charset=utf-8") return "HTML";
  if (mime === "image/svg+xml") return "SVG";
  if (mime === "font/woff2") return "WOFF2";
  return mime.slice(0, 4).toUpperCase();
}

export default function MediaPage() {
  const { t } = useI18n();
  const { anon, role, workspace, user } = useSession();
  const router = useRouter();

  // Issue #163: read_only members see no delete action.
  const canWrite = role !== "read_only";

  const [items, setItems] = useState<MediaItem[]>([]);
  const [total, setTotal] = useState(0);
  const [checking, setChecking] = useState(true);
  const [error, setError] = useState("");

  // Issue #384: per-workspace storage usage (used / limit) for the quota bar.
  const [usage, setUsage] = useState<MediaUsage>({});

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

  // Trash view: a second, explicitly-selected view over the SAME page, the same
  // shape the Forms page uses. The DEFAULT is the live library — a new visitor
  // must never land in the trash.
  const [view, setView] = useState<"active" | "trash">("active");
  const [trashed, setTrashed] = useState<TrashedMedia[]>([]);
  const [trashLoaded, setTrashLoaded] = useState(false);
  const [notify, setNotify] = useState("");
  const [confirmPurge, setConfirmPurge] = useState<TrashedMedia | null>(null);
  const [purgeBusy, setPurgeBusy] = useState(false);

  // Issue #382: WordPress-style "Add New Media" upload (drag-and-drop +
  // file picker). Uploads go through the existing uploadImage endpoint,
  // extended to accept images + code assets + PDFs.
  const [uploading, setUploading] = useState(false);
  const [uploadMsg, setUploadMsg] = useState("");
  const [dragOver, setDragOver] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  // Issue #503: 401/403 → /login, the same redirect Shell.signOut() uses.
  const goToLogin = useCallback(() => {
    router.replace("/login");
    window.location.href = "/login";
  }, [router]);

  // Issue #382: accept the full media allowlist — images, code files and PDFs.
  const ACCEPTED_TYPES = [
    "image/png",
    "image/jpeg",
    "image/webp",
    "image/gif",
    "text/css",
    "text/javascript",
    "application/javascript",
    "text/html",
    "image/svg+xml",
    "font/woff2",
    "application/pdf",
  ];
  const MAX_UPLOAD = 20 * 1024 * 1024;

  async function uploadFile(file: File) {
    if (!file) return;
    if (file.size > MAX_UPLOAD) {
      setUploadMsg(t("media.uploadTooLarge"));
      return;
    }
    setUploading(true);
    setUploadMsg("");
    try {
      const fd = new FormData();
      fd.append("file", file);
      const r = await fetch("/api/rest/uploads/image", { method: "POST", body: fd });
      const d = await r.json().catch(() => ({}));
      if (!r.ok) {
        setUploadMsg(apiErr(d, t("media.uploadFail"), t));
        return;
      }
      // t_d55ce0d4: the optimiser stores a GIF as a still JPEG (first frame),
      // so say it at the moment of upload too — not only in the hint above.
      setUploadMsg(t(file.type === "image/gif" ? "media.uploadedGifStill" : "media.uploaded"));
      // Refresh the grid so the new item (with its sizes) appears.
      setPage(1);
      load();
    } catch {
      setUploadMsg(t("media.uploadFail"));
    } finally {
      setUploading(false);
    }
  }

  function onFiles(files: FileList | null) {
    if (!files || files.length === 0) return;
    for (const f of Array.from(files)) {
      uploadFile(f);
    }
  }

  const load = useCallback(async () => {
    const params = new URLSearchParams();
    params.set("page", String(page));
    params.set("limit", String(PER_PAGE));
    if (formFilter) params.set("form", formFilter);
    if (sourceFilter) params.set("source", sourceFilter);
    // Issue #503: a non-ok response used to render an EMPTY grid with no error
    // (the `else { setItems([]) }` branch), so an expired session looked like an
    // empty library. Route it through the shared helper: 401/403 → /login,
    // anything else → a visible message.
    const r = await runLoad(
      () => fetchJson<{ items?: unknown[]; total?: number; usage?: MediaUsage }>(
        `/api/rest/media?${params.toString()}${workspaceQuery()}`,
      ),
      { onUnauth: goToLogin, message: t("media.loadFail"), errorKey: (b: unknown) => translateApiErr(b, t) },
    );
    if (!r.ok) {
      setItems([]);
      setTotal(0);
      setError(r.error);
      setChecking(false);
      return;
    }
    const d = r.data;
    setItems(Array.isArray(d.items) ? (d.items as MediaItem[]) : []);
    setTotal(typeof d.total === "number" ? d.total : 0);
    // Issue #384: capture the per-workspace storage usage for the quota bar.
    if (d.usage && typeof d.usage === "object") {
      setUsage(d.usage);
    }
    setError("");
    setChecking(false);
  }, [page, formFilter, sourceFilter, t, goToLogin]);

  // Load forms for the filter dropdown once.
  useEffect(() => {
    // Issue #503: a non-ok response used to silently become an empty dropdown.
    // 401/403 → /login; a non-auth failure leaves the dropdown empty (the grid
    // itself already surfaces the load error).
    void (async () => {
      const r = await runLoad(
        async () => {
          const d = await fetchJson<unknown>(`/api/rest/forms${workspaceQuery()}`);
          return Array.isArray(d) ? (d as FormRow[]) : [];
        },
        { onUnauth: goToLogin, message: t("forms.loadFail"), errorKey: (b: unknown) => translateApiErr(b, t) },
      );
      setForms(r.ok ? r.data : []);
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [workspace]);

  // Issue #170: re-fetch when the active workspace changes.
  useEffect(() => {
    setChecking(true);
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [load, workspace]);

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
      const r = await fetch(`/api/rest/media/${m.id}${workspaceQuery()}`, { method: "DELETE" });
      if (!r.ok) {
        setError(t("media.deleteFail"));
        return;
      }
      setDetail(null);
      setNotify(t("media.deletedToTrash"));
      load();
    } finally {
      setDeleting(false);
    }
  }

  // The trash is fetched only when it is opened. It is a SEPARATE listing
  // (GET /api/rest/media/trash) because the normal list deliberately excludes
  // trashed rows — and because the live list is paginated while the bin is not.
  const loadTrash = useCallback(async () => {
    const r = await runLoad(
      async () => {
        const d = await fetchJson<unknown>(`/api/rest/media/trash${workspaceQuery()}`);
        return Array.isArray(d) ? (d as TrashedMedia[]) : [];
      },
      { onUnauth: goToLogin, message: t("media.loadTrashFail") },
    );
    if (!r.ok) {
      setTrashed([]);
      setError(r.error);
      return;
    }
    setTrashed(r.data);
    setTrashLoaded(true);
    setError("");
  }, [t, goToLogin]);

  useEffect(() => {
    if (view === "trash") void loadTrash();
  }, [view, loadTrash, workspace]);

  // Escape closes the purge dialog.
  useEffect(() => {
    if (!confirmPurge) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setConfirmPurge(null);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [confirmPurge]);

  async function restore(id: string) {
    setNotify("");
    const res = await fetch(`/api/rest/media/${id}/restore${workspaceQuery()}`, { method: "POST" });
    if (!res.ok) {
      // 409 (code upload.objectGone) means the bytes were removed while the row
      // sat in the bin, so there is nothing to restore. Say WHICH failure it is
      // rather than the generic line: "we couldn't restore this" sends the
      // merchant to retry a call that can never succeed.
      if (res.status === 409) {
        setError(t("media.restoreObjectGone"));
      } else {
        setError(t("media.restoreFail"));
      }
      void loadTrash();
      return;
    }
    setNotify(t("media.restored"));
    void loadTrash();
    load();
  }

  async function purge() {
    if (!confirmPurge || purgeBusy) return;
    setPurgeBusy(true);
    setNotify("");
    try {
      const res = await fetch(`/api/rest/media/${confirmPurge.id}/purge${workspaceQuery()}`, {
        method: "DELETE",
      });
      if (!res.ok) {
        setError(t("media.purgeFail"));
        setConfirmPurge(null);
        void loadTrash();
        return;
      }
      setNotify(t("media.purged"));
      setConfirmPurge(null);
      void loadTrash();
      // The quota bar moves on a purge, so the live list (which carries usage)
      // has to be re-read too, not just the bin.
      load();
    } catch {
      setError(t("media.purgeFail"));
    } finally {
      setPurgeBusy(false);
    }
  }

  // In-flight: render inside the shell (see components/PageLoading.tsx) so the
  // sidebar and top bar stay mounted; the bare `<main>` this replaced dropped
  // the whole app chrome while the list fetch was outstanding.
  if (checking) {
    return <PageLoading active="media" />;
  }

  return (
    <Shell active="media">
      <div className="w-full flex-1 px-6 py-8">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h1 className="max-w-xl text-xl font-semibold tracking-tight">{t("media.title")}</h1>
            <p className="max-w-xl text-sm text-[var(--jf-muted)]">{t("media.subtitle")}</p>
          </div>
          {/* Issue #382: WordPress-style "Add New Media" upload button. */}
          {canWrite && (
            <button
              onClick={() => fileInputRef.current?.click()}
              disabled={uploading}
              className="jf-btn-primary px-4 py-2 text-sm disabled:opacity-50"
            >
              {uploading ? t("media.uploading") : `+ ${t("media.addNew")}`}
            </button>
          )}
        </div>

        {/* Issue #384: per-workspace storage usage bar so the merchant knows their
            remaining quota. Rendered when the API reports a limit. */}
        {typeof usage.limit === "number" && usage.limit > 0 && (
          <div className="mt-4">
            <div className="flex items-center justify-between text-xs text-[var(--jf-muted)]">
              <span>{t("media.usageLabel")}</span>
              <span>
                {fmtBytes(usage.used ?? 0)} / {fmtBytes(usage.limit)}
              </span>
            </div>
            <div className="mt-1.5 h-2 w-full overflow-hidden rounded-full bg-slate-200">
              <div
                className={`h-full rounded-full ${
                  (usage.used ?? 0) >= usage.limit
                    ? "bg-red-500"
                    : (usage.used ?? 0) / usage.limit > 0.8
                      ? "bg-amber-500"
                      : "bg-[#7170ff]"
                }`}
                style={{
                  width: `${Math.min(100, ((usage.used ?? 0) / usage.limit) * 100)}%`,
                }}
              />
            </div>
          </div>
        )}

        {/* Issue #382: drag-and-drop upload zone (hidden file input). */}
        {canWrite && (
          <div
            onDragOver={(e) => {
              e.preventDefault();
              setDragOver(true);
            }}
            onDragLeave={() => setDragOver(false)}
            onDrop={(e) => {
              e.preventDefault();
              setDragOver(false);
              onFiles(e.dataTransfer.files);
            }}
            className={`mt-4 flex cursor-pointer flex-col items-center justify-center rounded-xl border-2 border-dashed px-4 py-6 text-center transition ${
              dragOver ? "border-[#7170ff] bg-[#7170ff]/5" : "border-[var(--jf-border)]"
            }`}
            onClick={() => fileInputRef.current?.click()}
          >
            <p className="text-sm font-medium text-[var(--jf-muted)]">{t("media.dropHint")}</p>
            <p className="mt-1 text-xs text-[var(--jf-muted)]">{t("media.uploadTypes")}</p>
            {/* t_d55ce0d4: what the optimiser actually does to an image. The
                upload path stores every raster input as a still JPEG, so an
                animated GIF loses its animation — the merchant is told here,
                where they upload, instead of finding out afterwards. */}
            <p className="mt-1 text-xs text-[var(--jf-muted)]">{t("media.stillJpeg")}</p>
            {uploadMsg && <p className="mt-2 text-sm text-[#4f46e5]">{uploadMsg}</p>}
            <input
              ref={fileInputRef}
              type="file"
              multiple
              accept={ACCEPTED_TYPES.join(",")}
              className="hidden"
              onChange={(e) => {
                onFiles(e.target.files);
                e.target.value = "";
              }}
            />
          </div>
        )}

        {/* Active / Trash switcher. The live library is the DEFAULT: a fresh
            visitor must never land in the trash. Same shape as the Forms page. */}
        <div
          className="mt-5 flex flex-wrap items-center gap-1 border-b border-[var(--jf-border)]"
          role="tablist"
          onKeyDown={tablistKeyDown}
        >
          {(["active", "trash"] as const).map((v) => (
            <button
              key={v}
              id={tabDomId("media", v)}
              aria-controls={panelDomId("media", v)}
              tabIndex={tabStop(v, view)}
              type="button"
              role="tab"
              aria-selected={view === v}
              onClick={() => {
                setView(v);
                setError("");
                setNotify("");
              }}
              className={`-mb-px whitespace-nowrap border-b-2 px-3 py-2 text-sm font-medium ${
                view === v
                  ? "border-slate-900 text-slate-900"
                  : "border-transparent text-[var(--jf-muted)] hover:text-slate-800"
              }`}
            >
              {v === "active" ? t("media.trashTab") : t("media.trashTabDeleted")}
              {v === "trash" && trashLoaded && trashed.length > 0 ? ` (${trashed.length})` : ""}
            </button>
          ))}
        </div>

        {notify && (
          <p className="mt-3 rounded-lg bg-emerald-50 px-3 py-2 text-sm text-emerald-700">{notify}</p>
        )}

        {error && (
          <p role="alert" className="mt-3 text-sm text-red-700">
            {error}
            {/* Issue #503: a failed load gets an action, not a dead end. */}
            <button
              type="button"
              onClick={() => void (view === "trash" ? loadTrash() : load())}
              className="ml-2 underline decoration-dotted underline-offset-2"
            >
              {t("common.retry")}
            </button>
          </p>
        )}

        {/* Trash view: the bin, with restore and delete-for-good. */}
        {view === "trash" && (
          <div
            role="tabpanel"
            id={panelDomId("media", "trash")}
            aria-labelledby={tabDomId("media", "trash")}
          >
            <p className="mt-4 max-w-2xl text-sm text-[var(--jf-muted)]">{t("media.trashNote")}</p>
            <ul className="mt-4 space-y-2">
              {trashed.map((m) => (
                <li
                  key={m.id}
                  className="jf-card flex flex-col gap-3 px-5 py-4 sm:flex-row sm:items-center sm:justify-between"
                >
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium">{nameFromKey(m.key)}</p>
                    <p className="text-[11px] text-[var(--jf-muted)]">
                      {t("media.trashDeletedAt")} {formatDate(m.deleted_at, user?.timezone)} · {fmtSize(m.size)}
                    </p>
                    {/* The bytes are gone, so a restore can never work. Say so
                        here rather than offering a button that fails. */}
                    {!m.object_present && (
                      <p className="mt-1 text-[11px] text-amber-700">{t("media.restoreObjectGone")}</p>
                    )}
                  </div>
                  <div className="flex flex-wrap items-center gap-2 sm:justify-end">
                    {canWrite && (
                      <>
                        <button
                          type="button"
                          onClick={() => void restore(m.id)}
                          disabled={!m.object_present}
                          className="jf-btn-ghost min-h-[44px] disabled:cursor-not-allowed disabled:opacity-50 sm:min-h-10"
                        >
                          {t("media.restore")}
                        </button>
                        <button
                          type="button"
                          onClick={() => {
                            setConfirmPurge(m);
                            setError("");
                          }}
                          className="jf-btn-ghost min-h-[44px] text-rose-700 sm:min-h-10"
                        >
                          {t("media.purge")}
                        </button>
                      </>
                    )}
                  </div>
                </li>
              ))}
              {trashed.length === 0 && (
                <li className="rounded-lg text-sm text-[var(--jf-muted)]">{t("media.trashEmpty")}</li>
              )}
            </ul>
          </div>
        )}

        {view === "active" && (
        <div
          role="tabpanel"
          id={panelDomId("media", "active")}
          aria-labelledby={tabDomId("media", "active")}
        >
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
                  {isImage(m.mime) || m.preview ? (
                    /* eslint-disable-next-line @next/next/no-img-element */
                    <img
                      src={m.preview || m.sizes?.thumbnail || m.url}
                      alt={nameFromKey(m.key)}
                      className="h-full w-full object-cover"
                      loading="lazy"
                    />
                  ) : (
                    <div className="flex h-16 w-16 items-center justify-center rounded-xl border bg-white text-center text-xs font-bold text-[#4f46e5]">
                      {extBadge(m.mime)}
                    </div>
                  )}
                </div>
                <div className="p-3">
                  <p className="truncate text-sm font-medium" title={nameFromKey(m.key)}>
                    {nameFromKey(m.key)}
                  </p>
                  <p className="mt-0.5 text-xs text-[var(--jf-muted)]">
                    {formatDate(m.created_at, user?.timezone)} · {fmtSize(m.size)}
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
                {isImage(detail.mime) || detail.preview ? (
                  /* eslint-disable-next-line @next/next/no-img-element */
                  <img
                    src={detail.preview || detail.url}
                    alt={nameFromKey(detail.key)}
                    className="h-full w-full object-contain"
                  />
                ) : (
                  <div className="flex h-24 w-24 items-center justify-center rounded-2xl border bg-white text-sm font-bold text-[#4f46e5]">
                    {extBadge(detail.mime)}
                  </div>
                )}
              </div>
              <div className="p-4">
                <p className="truncate text-sm font-semibold" title={nameFromKey(detail.key)}>
                  {nameFromKey(detail.key)}
                </p>
                <p className="mt-1 text-xs text-[var(--jf-muted)]">
                  {formatDate(detail.created_at, user?.timezone)} · {fmtSize(detail.size)} · {detail.mime}
                </p>
                <p className="mt-0.5 text-xs text-[var(--jf-muted)]">
                  {detail.source === "customer" ? t("media.sourceCustomer") : t("media.sourceMerchant")}
                </p>
                {/* Issue #382: offer all generated image sizes (thumbnail /
                    medium / full) in the detail modal. */}
                {detail.sizes && Object.keys(detail.sizes).length > 0 && (
                  <div className="mt-3">
                    <p className="text-xs font-medium text-[var(--jf-muted)]">{t("media.sizes")}</p>
                    <div className="mt-1.5 flex flex-wrap gap-2">
                      {(["thumbnail", "medium", "full"] as const).map((size) => {
                        const url = detail.sizes?.[size];
                        if (!url) return null;
                        const label =
                          size === "thumbnail"
                            ? t("media.thumbnail")
                            : size === "medium"
                              ? t("media.medium")
                              : t("media.full");
                        return (
                          <a
                            key={size}
                            href={url}
                            target="_blank"
                            rel="noreferrer"
                            className="rounded-lg border border-[var(--jf-border)] px-3 py-1.5 text-xs text-[#4f46e5] hover:bg-slate-50"
                          >
                            {label}
                          </a>
                        );
                      })}
                    </div>
                  </div>
                )}
                <div className="mt-4 flex flex-wrap items-center gap-2">
                  <a
                    href={detail.url}
                    target="_blank"
                    rel="noreferrer"
                    className="jf-btn-ghost px-3 py-1.5 text-sm"
                  >
                    {detail.mime === "application/pdf" ? t("media.viewPdf") : t("media.view")}
                  </a>
                  {/* Issue #383: for PDFs with an auto-generated preview,
                      offer the preview image alongside the original. */}
                  {detail.mime === "application/pdf" && detail.preview && (
                    <a
                      href={detail.preview}
                      target="_blank"
                      rel="noreferrer"
                      className="jf-btn-ghost px-3 py-1.5 text-sm"
                    >
                      {t("media.preview")}
                    </a>
                  )}
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
                      className="ml-auto rounded-lg border border-red-200 px-3 py-1.5 text-sm text-red-700 hover:bg-red-50 disabled:opacity-40"
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
        )}

        {/* Purge confirmation. A permanent delete destroys the object and gives
            the storage back, so it asks for confirmation — the same level as the
            forms bin, minus the typed slug (media has no slug). */}
        {confirmPurge && (
          <div
            className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
            onClick={() => setConfirmPurge(null)}
          >
            <div
              role="dialog"
              aria-modal="true"
              aria-label={t("media.purgeConfirmTitle")}
              className="w-full max-w-md rounded-xl bg-white p-5 shadow-xl"
              onClick={(e) => e.stopPropagation()}
            >
              <p className="text-sm font-semibold">{t("media.purgeConfirmTitle")}</p>
              <p className="mt-2 text-sm text-[var(--jf-muted)]">{t("media.purgeConfirmBody")}</p>
              <p className="mt-2 truncate font-mono text-[11px] text-[var(--jf-muted)]">
                {nameFromKey(confirmPurge.key)}
              </p>
              <div className="mt-4 flex flex-wrap items-center justify-end gap-2">
                <button
                  type="button"
                  onClick={() => setConfirmPurge(null)}
                  className="jf-btn-ghost px-3 py-1.5 text-sm"
                >
                  {t("common.cancel")}
                </button>
                <button
                  type="button"
                  onClick={() => void purge()}
                  disabled={purgeBusy}
                  className="rounded-lg border border-red-200 px-3 py-1.5 text-sm text-red-700 hover:bg-red-50 disabled:opacity-40"
                >
                  {t("media.purge")}
                </button>
              </div>
            </div>
          </div>
        )}
      </div>
    </Shell>
  );
}
