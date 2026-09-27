"use client";

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

type Product = {
  id: string;
  name: string;
  description?: string;
  price_sen: number;
  images?: string[];
  type: string;
  billing_period?: string;
  // round 13c: from_payment | catch_up | skip_missed
  schedule_mode?: string;
  trial_days?: number;
  subscription_charges?: number | null; // null = infinite
  stock?: number | null;
  active: boolean;
  created_at?: string;
};

type SortKey = "name" | "price_sen" | "created_at";
type SortDir = "asc" | "desc";

// A row in the Trash view (GET /api/rest/products/trash). `can_purge` is false
// when the product has a sale or a subscription — those are the two obstacles
// the server refuses on, so the button is withheld rather than failing on click.
type TrashedProduct = {
  id: string;
  name: string;
  price_sen: number;
  active: boolean;
  deleted_at: string;
  sales_count: number;
  subscription_count: number;
  can_purge: boolean;
};

const money = (sen: number) =>
  `RM${(sen / 100).toLocaleString("en-MY", { minimumFractionDigits: 2 })}`;

const PER_PAGE = 10;

export default function Products() {
  const { t } = useI18n();
  const { anon, bundle, role, workspace, user } = useSession();
  const router = useRouter();

  // Issue #163: read_only members see no write actions (add/edit/archive).
  // manage + admin keep full write access.
  const canWrite = role !== "read_only";

  const [products, setProducts] = useState<Product[]>([]);
  const [checking, setChecking] = useState(true);
  const [error, setError] = useState("");

  // Issue #503: 401/403 → /login, the same redirect Shell.signOut() uses.
  const goToLogin = useCallback(() => {
    router.replace("/login");
    window.location.href = "/login";
  }, [router]);

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

  // Trash view: a second, explicitly-selected view over the SAME page. The
  // default is the catalogue — a new visitor must never land in the trash.
  const [view, setView] = useState<"active" | "trash">("active");
  const [trashed, setTrashed] = useState<TrashedProduct[]>([]);
  const [trashLoaded, setTrashLoaded] = useState(false);
  const [notify, setNotify] = useState("");
  const [confirmPurge, setConfirmPurge] = useState<TrashedProduct | null>(null);
  const [purgeTyped, setPurgeTyped] = useState("");
  const [purgeBusy, setPurgeBusy] = useState(false);

  // edit drawer
  const [editing, setEditing] = useState<Product | null>(null);
  const [drawerBusy, setDrawerBusy] = useState(false);
  const [drawerError, setDrawerError] = useState("");

  const load = useCallback(async () => {
    // Issue #29: no serial `await me → fetch` gate — the list fetch runs
    // immediately; a 401 flips `anon` and the auth bounce below handles it.
    // include_archived=1 so the WooCommerce-style admin list shows a status
    // column and lets owners toggle products back to active.
    // Issue #503: a non-ok response used to become an EMPTY list with no error
    // (`r.ok ? r.json() : []`). Route it through the shared helper: 401/403 →
    // /login, anything else → a visible message. Never empty-as-error.
    const r = await runLoad(
      async () => {
        const d = await fetchJson<unknown>(`/api/rest/products?include_archived=1${workspaceQuery()}`);
        return Array.isArray(d) ? (d as Product[]) : [];
      },
      { onUnauth: goToLogin, message: t("prod.loadFailList"), errorKey: (b: unknown) => translateApiErr(b, t) },
    );
    if (!r.ok) {
      setProducts([]);
      setError(r.error);
      setChecking(false);
      return;
    }
    setProducts(r.data);
    setError("");
    setChecking(false);
  }, [t, goToLogin]);

  useEffect(() => {
    // Always fetch the authoritative list (include_archived=1) so archived
    // products show for the status column/toggle. The bundle cache can
    // optimistically paint active products first, but must not suppress the
    // reload that surfaces archived ones.
    if (bundle && Array.isArray(bundle.products) && products.length === 0 && checking) {
      setProducts(bundle.products as Product[]);
    }
    load();
    // Issue #170: `workspace` re-runs load() when the active workspace changes
    // so the list re-fetches for the new workspace without a manual refresh.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [bundle, workspace]);

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
      await fetch(`/api/rest/products/bulk${workspaceQuery()}`, {
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

  // ── create ───────────────────────────────────────────────────────────────
  const [name, setName] = useState("");
  const [price, setPrice] = useState("");
  const [type, setType] = useState("one_time");
  const [period, setPeriod] = useState("month");
  const [trial, setTrial] = useState("0");
  const [stock, setStock] = useState("");
  // Issue #152: billing count — "infinite" (null) or a fixed N charges.
  const [chargesMode, setChargesMode] = useState<"infinite" | "fixed">("infinite");
  const [schedMode, setSchedMode] = useState<string>("from_payment");
  const [charges, setCharges] = useState("");

  async function add(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    // Issue #551: this is the ONE place RM (typed) becomes sen. Never rely on
    // Math.max/parseFloat to sanitise — a non-finite result serialises to null,
    // which the server decodes as 0 (a silent RM 0.00 product). Reject it visibly.
    const rm = parseFloat(price);
    if (!Number.isFinite(rm) || rm < 0) {
      setError(t("prod.priceInvalid", { name: name || t("prod.price") }));
      return;
    }
    const priceSen = Math.round(rm * 100);
    const r = await fetch(`/api/rest/products${workspaceQuery()}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        name,
        price_sen: priceSen,
        type,
        billing_period: type === "subscription" ? period : undefined,
        trial_days: type === "subscription" ? Math.max(0, parseInt(trial || "0", 10) || 0) : undefined,
        subscription_charges:
          type === "subscription" && chargesMode === "fixed"
            ? Math.max(1, parseInt(charges || "1", 10) || 1)
            : null,
        // round 13c: next-charge date policy.
        schedule_mode: type === "subscription" ? schedMode : undefined,
        stock: stock === "" ? null : Math.max(0, parseInt(stock, 10) || 0),
      }),
    });
    if (r.ok) {
      setName("");
      setPrice("");
      setStock("");
      setTrial("0");
      setCharges("");
      setChargesMode("infinite");
      load();
    } else {
      const d = await r.json().catch(() => ({}));
      setError(apiErr(d, t("prod.failedAdd"), t));
    }
  }

  // ── toggle status / delete (single row) ─────────────────────────────────
  async function toggleStatus(p: Product) {
    await fetch(`/api/rest/products/${p.id}${workspaceQuery()}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ active: !p.active }),
    });
    load();
  }

  async function archiveOne(p: Product) {
    const ok = window.confirm(t("prod.archiveOneConfirm", { name: p.name }));
    if (!ok) return;
    await fetch(`/api/rest/products/${p.id}${workspaceQuery()}`, { method: "DELETE" });
    setNotify(t("prod.deletedToTrash"));
    load();
  }

  // The trash is fetched only when it is opened. It is a SEPARATE listing
  // (GET /api/rest/products/trash) because the catalogue deliberately excludes
  // trashed rows — an archived product is still listed, a trashed one is not.
  const loadTrash = useCallback(async () => {
    const r = await runLoad(
      async () => {
        const d = await fetchJson<unknown>(`/api/rest/products/trash${workspaceQuery()}`);
        return Array.isArray(d) ? (d as TrashedProduct[]) : [];
      },
      { onUnauth: goToLogin, message: t("prod.loadTrashFail") },
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
    // eslint-disable-next-line react-hooks/exhaustive-deps
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

  async function restoreProduct(id: string) {
    setNotify("");
    const res = await fetch(`/api/rest/products/${id}/restore${workspaceQuery()}`, { method: "POST" });
    if (!res.ok) {
      setError(t("prod.restoreFail"));
      void loadTrash();
      return;
    }
    setNotify(t("prod.restored"));
    void loadTrash();
    load();
  }

  async function purgeProduct() {
    if (!confirmPurge || purgeBusy) return;
    if (purgeTyped.trim() !== confirmPurge.name) return;
    setPurgeBusy(true);
    setNotify("");
    try {
      const res = await fetch(`/api/rest/products/${confirmPurge.id}/purge${workspaceQuery()}`, {
        method: "DELETE",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ confirm_name: purgeTyped.trim() }),
      });
      if (!res.ok) {
        // A purge refused for a sale or a subscription names the obstacle in its
        // `code`, so the merchant gets that sentence in their OWN language
        // instead of the server's prose (issue #6177). Anything else falls back
        // to the page's own line.
        const d = await res.json().catch(() => null);
        setError(apiErr(d, t("prod.purgeFail"), t));
        setConfirmPurge(null);
        setPurgeTyped("");
        void loadTrash();
        return;
      }
      setNotify(t("prod.purged"));
      setConfirmPurge(null);
      setPurgeTyped("");
      void loadTrash();
    } catch {
      setError(t("prod.purgeFail"));
    } finally {
      setPurgeBusy(false);
    }
  }

  // ── edit drawer save ─────────────────────────────────────────────────────
  const [editForm, setEditForm] = useState({
    name: "",
    description: "",
    price: "",
    type: "one_time",
    period: "month",
    trial: "0",
    stock: "",
    active: true,
    images: [] as string[],
    chargesMode: "infinite" as "infinite" | "fixed",
    scheduleMode: "from_payment",
    charges: "",
  });
  const [editTab, setEditTab] = useState("general");
  const [uploadingImg, setUploadingImg] = useState(false);
  const [savedFlash, setSavedFlash] = useState("");

  const isDirty = useMemo(() => {
    if (!editing) return false;
    return (
      editForm.name !== editing.name ||
      editForm.description !== (editing.description || "") ||
      editForm.price !== (editing.price_sen / 100).toFixed(2) ||
      editForm.type !== editing.type ||
      (editForm.type === "subscription" &&
        editForm.period !== (editing.billing_period || "month")) ||
      editForm.trial !== String(editing.trial_days ?? 0) ||
      editForm.stock !==
        (typeof editing.stock === "number" ? String(editing.stock) : "") ||
      editForm.active !== editing.active ||
      editForm.chargesMode !==
        (typeof editing.subscription_charges === "number" ? "fixed" : "infinite") ||
      (editForm.chargesMode === "fixed" &&
        editForm.charges !== String(editing.subscription_charges ?? 1)) ||
      JSON.stringify(editForm.images) !== JSON.stringify(editing.images || [])
    );
  }, [editForm, editing]);

  function closeEditor() {
    if (isDirty && !window.confirm(t("prod.unsaved"))) return;
    setEditing(null);
    setEditTab("general");
    setSavedFlash("");
  }

  useEffect(() => {
    if (editing) {
      setEditForm({
        name: editing.name,
        description: editing.description || "",
        price: (editing.price_sen / 100).toFixed(2),
        type: editing.type,
        period: editing.billing_period || "month",
        trial: String(editing.trial_days ?? 0),
        stock: typeof editing.stock === "number" ? String(editing.stock) : "",
        active: editing.active,
        images: editing.images || [],
        chargesMode:
          typeof editing.subscription_charges === "number" ? "fixed" : "infinite",
        charges: String(editing.subscription_charges ?? 1),
        scheduleMode: editing.schedule_mode || "from_payment",
      });
      setEditTab("general");
      setDrawerError("");
      setSavedFlash("");
      setUploadingImg(false);
    }
  }, [editing]);

  async function uploadImage(file: File) {
    if (!file) return;
    if (!["image/png", "image/jpeg", "image/webp", "image/gif"].includes(file.type)) {
      setDrawerError(t("prod.uploadFail"));
      return;
    }
    if (file.size > 20 * 1024 * 1024) {
      setDrawerError(t("prod.uploadFail"));
      return;
    }
    setUploadingImg(true);
    setDrawerError("");
    try {
      const fd = new FormData();
      fd.append("file", file);
      const r = await fetch("/api/rest/uploads/image", { method: "POST", body: fd });
      const d = await r.json().catch(() => ({}));
      if (!r.ok || !d.url) {
        setDrawerError(t("prod.uploadFail"));
        return;
      }
      setEditForm((f) => ({ ...f, images: [...f.images, d.url] }));
      // Reordering/remove marks the form dirty (images changed).
    } finally {
      setUploadingImg(false);
    }
  }

  function moveImage(i: number, dir: -1 | 1) {
    setEditForm((f) => {
      const imgs = [...f.images];
      const j = i + dir;
      if (j < 0 || j >= imgs.length) return f;
      [imgs[i], imgs[j]] = [imgs[j], imgs[i]];
      return { ...f, images: imgs };
    });
  }

  async function saveEdit(e: React.FormEvent) {
    e.preventDefault();
    if (!editing) return;
    setDrawerBusy(true);
    setDrawerError("");
    // Issue #501: a product image whose URL is not https:// would render as
    // the html/template #ZgotmplZ sentinel on the buyer's page (form renderer
    // + restaurant menu). Catch it at the field; the server rejects it too.
    const badImage = editForm.images.find((u) => u.trim() && !u.trim().startsWith("https://"));
    if (badImage) {
      setDrawerError(t("url.notHttps"));
      setDrawerBusy(false);
      return;
    }
    const stockVal = editForm.stock === "" ? null : Math.max(0, parseInt(editForm.stock, 10) || 0);
    const trialVal = Math.max(0, parseInt(editForm.trial || "0", 10) || 0);
    // Issue #551: same single-conversion rule as the create path — RM typed here
    // becomes sen exactly once, and a non-finite price is refused visibly.
    const rm = parseFloat(editForm.price);
    if (!Number.isFinite(rm) || rm < 0) {
      setDrawerError(t("prod.priceInvalid", { name: editForm.name || t("prod.price") }));
      setDrawerBusy(false);
      return;
    }
    const body: Record<string, unknown> = {
      name: editForm.name,
      description: editForm.description,
      price_sen: Math.round(rm * 100),
      type: editForm.type,
      billing_period: editForm.type === "subscription" ? editForm.period : undefined,
      trial_days: editForm.type === "subscription" ? trialVal : undefined,
      subscription_charges:
        editForm.type === "subscription" && editForm.chargesMode === "fixed"
          ? Math.max(1, parseInt(editForm.charges || "1", 10) || 1)
          : null,
      schedule_mode: editForm.type === "subscription" ? editForm.scheduleMode : undefined,
      stock: stockVal,
      active: editForm.active,
      images: editForm.images,
    };
    try {
      const r = await fetch(`/api/rest/products/${editing.id}${workspaceQuery()}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      if (!r.ok) {
        const d = await r.json().catch(() => ({}));
        setDrawerError(apiErr(d, t("prod.failedUpdate"), t));
        return;
      }
      setSavedFlash(t("prod.updated"));
      load();
    } finally {
      setDrawerBusy(false);
    }
  }

  // In-flight: render inside the shell (see components/PageLoading.tsx) so the
  // sidebar and top bar stay mounted; the bare `<main>` this replaced dropped
  // the whole app chrome while the products fetch was outstanding.
  if (checking) {
    return <PageLoading active="products" />;
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
      ? `${t("prod.subscription")} · ${p.billing_period === "year" ? t("prod.yearly") : t("prod.monthly")}${
          typeof p.subscription_charges === "number"
            ? ` · ${t("prod.chargesNLabel", { n: p.subscription_charges })}`
            : ""
        }`
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

        {/* Add form */}
        {canWrite && (
        <form onSubmit={add} className="jf-card mt-6 flex flex-wrap gap-2 p-4">
          <input value={name} onChange={(e) => setName(e.target.value)} required placeholder={t("prod.name")} className="jf-input min-w-48 flex-1" />
          <input value={price} onChange={(e) => setPrice(e.target.value)} required type="number" step="0.01" min="0" placeholder={t("prod.price")} className="jf-input w-32" />
          <input value={stock} onChange={(e) => setStock(e.target.value)} type="number" min="0" placeholder={t("prod.stock")} className="jf-input w-40" />
          <select value={type} onChange={(e) => setType(e.target.value)} className="jf-input w-36">
            <option value="one_time">{t("prod.oneTime")}</option>
            <option value="subscription">{t("prod.subscription")}</option>
          </select>
          {type === "subscription" && (
            <>
              <select value={period} onChange={(e) => setPeriod(e.target.value)} className="jf-input w-32">
                <option value="month">{t("prod.monthly")}</option>
                <option value="year">{t("prod.yearly")}</option>
              </select>
              <input
                value={trial}
                onChange={(e) => setTrial(e.target.value)}
                type="number"
                min="0"
                step="1"
                placeholder={t("prod.trial")}
                title={t("prod.trial")}
                className="jf-input w-36"
              />
              <select
                value={chargesMode}
                onChange={(e) => setChargesMode(e.target.value as "infinite" | "fixed")}
                className="jf-input w-40"
                title={t("prod.billingCount")}
              >
                <option value="infinite">{t("prod.chargesInfinite")}</option>
                <option value="fixed">{t("prod.chargesFixed")}</option>
              </select>
              <select
                value={schedMode}
                onChange={(e) => setSchedMode(e.target.value)}
                className="jf-input w-56"
                title={t("prod.scheduleMode")}
              >
                <option value="from_payment">{t("prod.schedFromPayment")}</option>
                <option value="catch_up">{t("prod.schedCatchUp")}</option>
                <option value="skip_missed">{t("prod.schedSkipMissed")}</option>
              </select>
              {chargesMode === "fixed" && (
                <input
                  value={charges}
                  onChange={(e) => setCharges(e.target.value)}
                  type="number"
                  min="1"
                  step="1"
                  placeholder={t("prod.chargesN")}
                  title={t("prod.chargesN")}
                  className="jf-input w-24"
                />
              )}
            </>
          )}
          <button className="jf-btn-primary">{t("prod.add")}</button>
        </form>
        )}
        {error && (
          <p role="alert" className="mt-2 rounded-lg bg-red-50 px-3 py-2 text-xs text-red-600">
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

        {/* Active / Trash switcher. The catalogue is the DEFAULT: a fresh
            visitor must never land in the trash. */}
        <div className="mt-5 flex flex-wrap items-center gap-1 border-b border-[var(--jf-border)]" role="tablist">
          {(["active", "trash"] as const).map((v) => (
            <button
              key={v}
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
              {v === "active" ? t("prod.trashTab") : t("prod.trashTabDeleted")}
              {v === "trash" && trashLoaded && trashed.length > 0 ? ` (${trashed.length})` : ""}
            </button>
          ))}
        </div>

        {notify && (
          <p className="mt-3 rounded-lg bg-emerald-50 px-3 py-2 text-sm text-emerald-700">{notify}</p>
        )}

        {/* Trash view: the bin, with restore and delete-for-good. */}
        {view === "trash" && (
          <>
            <p className="mt-4 max-w-2xl text-sm text-[var(--jf-muted)]">{t("prod.trashNote")}</p>
            <ul className="mt-4 space-y-2">
              {trashed.map((p) => (
                <li
                  key={p.id}
                  className="jf-card flex flex-col gap-3 px-5 py-4 sm:flex-row sm:items-center sm:justify-between"
                >
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium">{p.name}</p>
                    <p className="text-[11px] text-[var(--jf-muted)]">
                      {money(p.price_sen)} · {t("prod.trashDeletedAt")}{" "}
                      {formatDate(p.deleted_at, user?.timezone)}
                    </p>
                    {/* Withheld rather than offered-and-refused: the server
                        refuses on a sale or a live subscription, and the count
                        says which obstacle to clear. */}
                    {!p.can_purge && (
                      <p className="mt-1 text-[11px] text-amber-700">
                        {t("prod.canPurgeFalse")}{" "}
                        {p.sales_count > 0
                          ? `${t("prod.salesCount")}: ${p.sales_count}`
                          : `${t("prod.subsCount")}: ${p.subscription_count}`}
                      </p>
                    )}
                  </div>
                  <div className="flex flex-wrap items-center gap-2 sm:justify-end">
                    {canWrite && (
                      <>
                        <button
                          type="button"
                          onClick={() => void restoreProduct(p.id)}
                          className="jf-btn-ghost min-h-[44px] sm:min-h-10"
                        >
                          {t("prod.restore")}
                        </button>
                        {p.can_purge && (
                          <button
                            type="button"
                            onClick={() => {
                              setConfirmPurge(p);
                              setPurgeTyped("");
                              setError("");
                            }}
                            className="jf-btn-ghost min-h-[44px] text-rose-600 hover:bg-rose-50 sm:min-h-10"
                          >
                            {t("prod.purge")}
                          </button>
                        )}
                      </>
                    )}
                  </div>
                </li>
              ))}
              {trashed.length === 0 && (
                <li className="rounded-lg text-sm text-[var(--jf-muted)]">{t("prod.trashEmpty")}</li>
              )}
            </ul>
          </>
        )}

        {view === "active" && (
        <>
        {/* Bulk action bar */}
        {canWrite && ids.length > 0 && (
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
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-[var(--jf-border)] text-left">
                <th className="hidden w-10 px-4 py-3 sm:table-cell">
                  {canWrite && (
                    <input
                      type="checkbox"
                      aria-label={t("forms.selectAll")}
                      checked={allOnPageSelected}
                      onChange={toggleAll}
                      className="h-4 w-4 accent-[var(--jf-primary)]"
                    />
                  )}
                </th>
                <th className="px-2 py-3">
                  <SortHeader label={t("prod.name")} k="name" />
                </th>
                <th className="hidden px-2 py-3 text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)] sm:table-cell">
                  {t("prod.type")}
                </th>
                <th className="hidden px-2 py-3 text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)] sm:table-cell">
                  {t("prod.stockShort")}
                </th>
                <th className="px-2 py-3">
                  <SortHeader label={t("prod.price")} k="price_sen" />
                </th>
                <th className="px-2 py-3 text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                  {t("dash.status")}
                </th>
                <th className="hidden px-2 py-3 sm:table-cell">
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
                  <td className="hidden px-4 py-3 sm:table-cell">
                    {canWrite && (
                      <input
                        type="checkbox"
                        aria-label={`Select ${p.name}`}
                        checked={selected.has(p.id)}
                        onChange={() => toggle(p.id)}
                        className="h-4 w-4 accent-[var(--jf-primary)]"
                      />
                    )}
                  </td>
                  <td className="px-2 py-3">
                    {canWrite ? (
                      <button
                        onClick={() => setEditing(p)}
                        className="text-left font-medium text-[var(--jf-primary)] hover:underline"
                      >
                        {p.name}
                      </button>
                    ) : (
                      <span className="font-medium">{p.name}</span>
                    )}
                  </td>
                  <td className="hidden px-2 py-3 text-[var(--jf-muted)] sm:table-cell">{typeLabel(p)}</td>
                  <td className="hidden px-2 py-3 sm:table-cell">{stockLabel(p)}</td>
                  <td className="px-2 py-3 font-semibold">{money(p.price_sen)}</td>
                  <td className="px-2 py-3">
                    {canWrite ? (
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
                    ) : (
                      <span
                        className={[
                          "inline-block rounded-full px-2.5 py-0.5 text-[11px] font-medium",
                          p.active
                            ? "border border-emerald-200 bg-emerald-50 text-emerald-700"
                            : "border border-slate-200 bg-slate-50 text-slate-500",
                        ].join(" ")}
                      >
                        {statusLabel(p)}
                      </span>
                    )}
                  </td>
                  <td className="hidden px-2 py-3 text-[var(--jf-muted)] sm:table-cell">
                    {p.created_at ? formatDate(p.created_at, user?.timezone) : "—"}
                  </td>
                  <td className="px-4 py-3">
                    {canWrite && (
                      <div className="flex items-center justify-end gap-1">
                        <button onClick={() => setEditing(p)} className="jf-btn-ghost px-2.5 py-1 text-xs">
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
                    )}
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

        {/* Edit drawer / view — WC-style product editor (#124) */}
        {editing && (
          <div className="fixed inset-0 z-50 flex justify-end bg-black/30" onClick={closeEditor}>
            <div
              className="h-full w-full max-w-xl overflow-y-auto bg-white p-6 shadow-xl"
              onClick={(e) => e.stopPropagation()}
              role="dialog"
              aria-label={t("prod.editTitle")}
            >
              <div className="flex items-center justify-between">
                <h2 className="text-lg font-semibold">{t("prod.editTitle")}</h2>
                <button onClick={closeEditor} className="text-lg text-[var(--jf-muted)]" aria-label={t("prof.twoFaClose")}>
                  ✕
                </button>
              </div>

              {/* tabs */}
              <div className="mt-4 flex flex-wrap gap-1 border-b border-[var(--jf-border)]">
                {(
                  [
                    ["general", t("prod.general")],
                    ["subscription", t("prod.subscriptionTab")],
                    ["inventory", t("prod.inventory")],
                    ["media", t("prod.media")],
                  ] as const
                ).map(([key, label]) => (
                  <button
                    key={key}
                    type="button"
                    onClick={() => setEditTab(key)}
                    className={`rounded-t-lg px-4 py-2 text-sm font-medium ${
                      editTab === key
                        ? "border border-b-0 border-[var(--jf-border)] text-[var(--jf-primary)]"
                        : "text-[var(--jf-muted)] hover:text-[var(--jf-primary)]"
                    }`}
                  >
                    {label}
                  </button>
                ))}
              </div>

              {savedFlash && (
                <p className="mt-3 rounded-lg bg-emerald-50 px-3 py-2 text-xs text-emerald-700">
                  {savedFlash}
                </p>
              )}

              <form onSubmit={saveEdit} className="mt-5 space-y-4">
                {editTab === "general" && (
                  <>
                    <label className="block text-sm font-medium">
                      {t("prod.name")}
                      <input
                        value={editForm.name}
                        onChange={(e) => setEditForm((f) => ({ ...f, name: e.target.value }))}
                        required
                        className="jf-input mt-1"
                      />
                    </label>
                    <label className="block text-sm font-medium">
                      {t("prod.description")}
                      <textarea
                        value={editForm.description}
                        onChange={(e) => setEditForm((f) => ({ ...f, description: e.target.value }))}
                        rows={4}
                        placeholder={t("prod.descriptionPh")}
                        className="jf-input mt-1 resize-y"
                      />
                    </label>
                    <label className="block text-sm font-medium">
                      {t("prod.price")}
                      <input
                        value={editForm.price}
                        onChange={(e) => setEditForm((f) => ({ ...f, price: e.target.value }))}
                        required
                        type="number"
                        step="0.01"
                        min="0"
                        className="jf-input mt-1"
                      />
                    </label>
                    <label className="block text-sm font-medium">
                      {t("prod.type")}
                      <select
                        value={editForm.type}
                        onChange={(e) => setEditForm((f) => ({ ...f, type: e.target.value }))}
                        className="jf-input mt-1"
                      >
                        <option value="one_time">{t("prod.oneTime")}</option>
                        <option value="subscription">{t("prod.subscription")}</option>
                      </select>
                    </label>
                  </>
                )}

                {editTab === "subscription" && (
                  <>
                    {editForm.type !== "subscription" ? (
                      <p className="rounded-lg bg-amber-50 px-3 py-2 text-xs text-amber-700">
                        {t("prod.general")}:{" "}
                        <button
                          type="button"
                          className="font-medium text-[var(--jf-primary)] underline"
                          onClick={() => setEditTab("general")}
                        >
                          {t("prod.subscription")}
                        </button>
                      </p>
                    ) : (
                      <>
                        <label className="block text-sm font-medium">
                          {t("prod.billingPeriod")}
                          <select
                            value={editForm.period}
                            onChange={(e) => setEditForm((f) => ({ ...f, period: e.target.value }))}
                            className="jf-input mt-1"
                          >
                            <option value="month">{t("prod.monthly")}</option>
                            <option value="year">{t("prod.yearly")}</option>
                          </select>
                        </label>
                        <label className="block text-sm font-medium">
                          {t("prod.trial")}
                          <input
                            value={editForm.trial}
                            onChange={(e) => setEditForm((f) => ({ ...f, trial: e.target.value }))}
                            type="number"
                            min="0"
                            step="1"
                            className="jf-input mt-1"
                          />
                          <span className="mt-1 block text-xs text-[var(--jf-muted)]">
                            {t("prod.trialHint")}
                          </span>
                        </label>
                        <label className="block text-sm font-medium">
                          {t("prod.billingCount")}
                          <select
                            value={editForm.chargesMode}
                            onChange={(e) =>
                              setEditForm((f) => ({
                                ...f,
                                chargesMode: e.target.value as "infinite" | "fixed",
                              }))
                            }
                            className="jf-input mt-1"
                          >
                            <option value="infinite">{t("prod.chargesInfinite")}</option>
                            <option value="fixed">{t("prod.chargesFixed")}</option>
                          </select>
                          <select
                            value={editForm.scheduleMode}
                            onChange={(e) =>
                              setEditForm((f) => ({ ...f, scheduleMode: e.target.value }))
                            }
                            className="jf-input mt-1"
                          >
                            <option value="from_payment">{t("prod.schedFromPayment")}</option>
                            <option value="catch_up">{t("prod.schedCatchUp")}</option>
                            <option value="skip_missed">{t("prod.schedSkipMissed")}</option>
                          </select>
                          <span className="mt-1 block text-xs text-[var(--jf-muted)]">
                            {t("prod.scheduleModeHint")}
                          </span>
                          {editForm.chargesMode === "fixed" && (
                            <input
                              value={editForm.charges}
                              onChange={(e) =>
                                setEditForm((f) => ({ ...f, charges: e.target.value }))
                              }
                              type="number"
                              min="1"
                              step="1"
                              className="jf-input mt-1"
                            />
                          )}
                          <span className="mt-1 block text-xs text-[var(--jf-muted)]">
                            {t("prod.chargesHint")}
                          </span>
                        </label>
                      </>
                    )}
                  </>
                )}

                {editTab === "inventory" && (
                  <>
                    <label className="block text-sm font-medium">
                      {t("prod.stock")}
                      <input
                        value={editForm.stock}
                        onChange={(e) => setEditForm((f) => ({ ...f, stock: e.target.value }))}
                        type="number"
                        min="0"
                        className="jf-input mt-1"
                      />
                      <span className="mt-1 block text-xs text-[var(--jf-muted)]">
                        {t("prod.noStockNote")}
                      </span>
                    </label>
                    {typeof editing.stock === "number" && editing.stock === 0 && (
                      <p className="rounded-lg bg-red-50 px-3 py-2 text-xs font-medium text-red-600">
                        {t("prod.soldOut")} — set a stock above to reinstate it.
                      </p>
                    )}
                  </>
                )}

                {editTab === "media" && (
                  <>
                    <div className="block text-sm font-medium">
                      <div className="mb-2">{t("prod.images")}</div>
                      <div className="flex flex-wrap gap-3">
                        {editForm.images.map((url, i) => (
                          <div key={`${i}-${url}`} className="group relative">
                            {/* eslint-disable-next-line @next/next/no-img-element */}
                            <img
                              src={url}
                              alt={t("prod.images")}
                              className="h-24 w-24 rounded-lg border border-[var(--jf-border)] object-cover"
                            />
                            <div className="absolute -top-2 -right-2 flex gap-1">
                              <button
                                type="button"
                                onClick={() => moveImage(i, -1)}
                                disabled={i === 0}
                                className="rounded-full bg-white px-1.5 text-xs shadow ring-1 ring-slate-200 disabled:opacity-30"
                                title={t("prod.moveUp")}
                              >
                                ↑
                              </button>
                              <button
                                type="button"
                                onClick={() => moveImage(i, 1)}
                                disabled={i === editForm.images.length - 1}
                                className="rounded-full bg-white px-1.5 text-xs shadow ring-1 ring-slate-200 disabled:opacity-30"
                                title={t("prod.moveDown")}
                              >
                                ↓
                              </button>
                            </div>
                            <button
                              type="button"
                              onClick={() =>
                                setEditForm((f) => ({
                                  ...f,
                                  images: f.images.filter((_, k) => k !== i),
                                }))
                              }
                              className="absolute bottom-1 left-1/2 -translate-x-1/2 rounded bg-black/60 px-2 py-0.5 text-[10px] text-white opacity-0 transition group-hover:opacity-100"
                            >
                              {t("prod.removeImage")}
                            </button>
                          </div>
                        ))}
                      </div>
                      <label className="mt-3 inline-flex cursor-pointer items-center gap-2 rounded-lg border border-dashed border-[var(--jf-border)] px-4 py-2 text-sm text-[var(--jf-muted)] hover:border-[var(--jf-primary)] hover:text-[var(--jf-primary)]">
                        {uploadingImg ? t("prod.uploading") : t("prod.addImage")}
                        <input
                          type="file"
                          accept="image/png,image/jpeg,image/webp,image/gif"
                          className="hidden"
                          disabled={uploadingImg}
                          onChange={(e) => {
                            const f = e.target.files?.[0];
                            e.target.value = "";
                            if (f) uploadImage(f);
                          }}
                        />
                      </label>
                      <span className="mt-2 block text-xs text-[var(--jf-muted)]">
                        {t("prod.imagesNote")}
                      </span>
                    </div>
                  </>
                )}

                {drawerError && (
                  <p className="rounded-lg bg-red-50 px-3 py-2 text-xs text-red-600">{drawerError}</p>
                )}

                {/* status / visibility */}
                <div className="rounded-lg border border-[var(--jf-border)] p-3">
                  <div className="mb-1 text-sm font-medium">{t("prod.status")}</div>
                  <label className="flex items-center gap-2 text-sm">
                    <input
                      type="checkbox"
                      checked={editForm.active}
                      onChange={(e) => setEditForm((f) => ({ ...f, active: e.target.checked }))}
                      className="h-4 w-4 accent-[var(--jf-primary)]"
                    />
                    {t("prod.active")}
                  </label>
                  {!editForm.active && (
                    <p className="mt-1 text-xs text-amber-600">{t("prod.inactiveNote")}</p>
                  )}
                  <p className="mt-2 text-xs text-[var(--jf-muted)]">{t("prod.activeHint")}</p>
                </div>

                <div className="flex items-center gap-2 pt-2">
                  <button type="submit" disabled={drawerBusy} className="jf-btn-primary disabled:opacity-50">
                    {drawerBusy ? t("builder.saving") : t("set.save")}
                  </button>
                  <button type="button" onClick={closeEditor} className="jf-btn-ghost">
                    {t("prod.cancel")}
                  </button>
                </div>
              </form>
            </div>
          </div>
        )}
        </>
        )}

        {/* Purge confirmation. A permanent delete cannot be undone and takes the
            product's variations with it, so it asks the caller to type the name
            back — the same level as the forms bin, using the product's NAME
            because a product has no slug. */}
        {confirmPurge && (
          <div
            className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
            onClick={() => setConfirmPurge(null)}
          >
            <div
              role="dialog"
              aria-modal="true"
              aria-label={t("prod.purgeConfirmTitle")}
              className="w-full max-w-md rounded-xl bg-white p-5 shadow-xl"
              onClick={(e) => e.stopPropagation()}
            >
              <p className="text-sm font-semibold">{t("prod.purgeConfirmTitle")}</p>
              <p className="mt-2 text-sm text-[var(--jf-muted)]">{t("prod.purgeConfirmBody")}</p>
              <label className="mt-3 block text-xs font-medium text-[var(--jf-muted)]">
                {t("prod.purgeConfirmFieldLabel")}
              </label>
              <input
                value={purgeTyped}
                onChange={(e) => setPurgeTyped(e.target.value)}
                className="jf-input mt-1 w-full"
                aria-label={t("prod.purgeConfirmFieldLabel")}
              />
              <div className="mt-4 flex flex-wrap items-center justify-end gap-2">
                <button
                  type="button"
                  onClick={() => setConfirmPurge(null)}
                  className="jf-btn-ghost px-3 py-1.5 text-sm"
                >
                  {t("prod.cancel")}
                </button>
                <button
                  type="button"
                  onClick={() => void purgeProduct()}
                  disabled={purgeBusy || purgeTyped.trim() !== confirmPurge.name}
                  className="rounded-lg border border-red-200 px-3 py-1.5 text-sm text-red-600 hover:bg-red-50 disabled:opacity-40"
                >
                  {t("prod.purge")}
                </button>
              </div>
            </div>
          </div>
        )}
      </div>
    </Shell>
  );
}
