"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useI18n } from "../../components/i18n";

/* ─── types ──────────────────────────────────────────────────────────────── */

type BlockType =
  | "header"
  | "text"
  | "image"
  | "product"
  | "divider"
  | "name"
  | "email"
  | "phone"
  | "textarea"
  | "checkbox"
  | "coupon";

type Block = {
  id: string;
  type: BlockType;
  value: string;
  src?: string;
  productId?: string;
  required?: boolean;
  condition?: { field_id: string; op: string; value: string } | null;
};

type Product = { id: string; name: string; price_sen: number; type: string };

type FormRow = { id: string; slug: string; title: string; status: string };

type SaleRow = {
  ref_code: string;
  status: string;
  total_sen: number;
  payment_method: string;
  buyer: string;
  created_at: string;
  skip_capture?: boolean;
  authorized?: boolean;
  captured_at?: string;
};
type SalesPage = { items: SaleRow[]; page: number; limit: number; total: number };

/* issue #12: an uncaptured skip_capture authorization — captured_at is only
   set once the merchant captures (MarkCaptured); released sales go cancelled. */
const isCapturePending = (s: SaleRow) =>
  !!s.skip_capture &&
  !s.captured_at &&
  (s.status === "pending" || s.status === "authorized" || s.authorized === true);

const PALETTE: { type: BlockType; icon: string }[] = [
  { type: "text", icon: "T" },
  { type: "image", icon: "🖼" },
  { type: "product", icon: "🛒" },
  { type: "name", icon: "🔤" },
  { type: "email", icon: "✉" },
  { type: "phone", icon: "☎" },
  { type: "textarea", icon: "📝" },
  { type: "checkbox", icon: "☑" },
  { type: "coupon", icon: "🏷" },
  { type: "divider", icon: "―" },
];

const fieldTypes: BlockType[] = ["name", "email", "phone", "textarea", "checkbox", "coupon"];

function uid() {
  return Math.random().toString(36).slice(2, 9);
}

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
  })[s] || "";

const FORM_SALES_PAGE_SIZE = 10;

/* default field labels, keyed for i18n */
const FIELD_LABEL_KEY: Partial<Record<BlockType, import("../../components/i18n").Key>> = {
  name: "blk.fullName",
  email: "blk.email",
  phone: "blk.phoneNum",
  textarea: "blk.addNote",
};

/* default blocks when hydrating legacy forms (products list) */
function defaultBlocks(products: { id: string; name: string; price_sen: number }[]): Block[] {
  const blocks: Block[] = [];
  products.slice(0, 4).forEach((p) => {
    blocks.push({ id: uid(), type: "product", value: p.name, productId: p.id });
  });
  blocks.push({ id: uid(), type: "name", value: "Full name", required: true });
  blocks.push({ id: uid(), type: "email", value: "Email", required: true });
  blocks.push({ id: uid(), type: "phone", value: "Phone number" });
  return blocks;
}

/* ─── builder page ───────────────────────────────────────────────────────── */

export default function Builder() {
  const { t } = useI18n();
  /* translated palette/field labels; block type "textarea" displays as "Note" */
  const typeLabel = (bt: BlockType) => t((bt === "textarea" ? "blk.note" : `blk.${bt}`) as import("../../components/i18n").Key);
  const typeHint = (bt: BlockType) => t((bt === "textarea" ? "blk.noteHint" : `blk.${bt}Hint`) as import("../../components/i18n").Key);
  const [products, setProducts] = useState<Product[]>([]);
  const [forms, setForms] = useState<FormRow[]>([]);
  const [currentId, setCurrentId] = useState<string | null>(null);
  const [slug, setSlug] = useState("");
  const [title, setTitle] = useState("New form");
  const [blocks, setBlocks] = useState<Block[]>([]);
  const [selected, setSelected] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [pubUrl, setPubUrl] = useState("");
  const [loadErr, setLoadErr] = useState("");

  // Per-form CHIP override (saved via dedicated endpoints).
  const [ovBrand, setOvBrand] = useState("");
  const [ovSecret, setOvSecret] = useState("");
  const [ovFallback, setOvFallback] = useState("");
  const [ovMsg, setOvMsg] = useState("");
  const [formTrack, setFormTrack] = useState("");
  const [fsTheme, setFsTheme] = useState("#5e6ad2");
  const [fsLogo, setFsLogo] = useState("");
  const [fsRedS, setFsRedS] = useState("");
  const [fsRedF, setFsRedF] = useState("");
  const [fsEmbed, setFsEmbed] = useState(true);

  // Real drag state: which palette type or block index is being dragged, and
  // which index the insertion indicator currently points at.
  const dragType = useRef<BlockType | null>(null);
  const dragIdx = useRef<number | null>(null);
  const [dropAt, setDropAt] = useState<number | null>(null);

  const load = useCallback(async () => {
    const [p, f] = await Promise.all([
      fetch("/api/rest/products").then((r) => (r.ok ? r.json() : [])),
      fetch("/api/rest/forms").then((r) => (r.ok ? r.json() : [])),
    ]);
    setProducts(p);
    setForms(f);
  }, []);

  // per-form stats (submissions / paid / revenue) for the editor sidebar
  const [formStat, setFormStat] = useState<{
    submissions: number; paid_count: number; confirmed_count: number; revenue_sen: number;
  } | null>(null);

  const loadStats = useCallback(async (fid: string | null) => {
    if (!fid) {
      setFormStat(null);
      return;
    }
    const all = await fetch("/api/rest/analytics/forms").then((r) => (r.ok ? r.json() : []));
    const mine = (Array.isArray(all) ? all : []).find((x: { form_id: string }) => x.form_id === fid);
    setFormStat(mine || null);
  }, []);

  useEffect(() => {
    void loadStats(currentId);
  }, [currentId, loadStats]);

  // Per-form sales table (issue #11): same row shape as dashboard Recent sales,
  // paginated 10/page via the /api/rest/sales?page&limit&form_id filter.
  const [formSales, setFormSales] = useState<SaleRow[]>([]);
  const [formSalesPage, setFormSalesPage] = useState(1);
  const [formSalesTotal, setFormSalesTotal] = useState(0);
  const [formSalesErr, setFormSalesErr] = useState("");

  const loadFormSales = useCallback(async (fid: string | null, page: number) => {
    if (!fid) {
      setFormSales([]);
      setFormSalesTotal(0);
      setFormSalesErr("");
      return;
    }
    try {
      const q = new URLSearchParams({
        page: String(page),
        limit: String(FORM_SALES_PAGE_SIZE),
        form_id: fid,
      });
      const r = await fetch(`/api/rest/sales?${q.toString()}`);
      if (!r.ok) {
        setFormSales([]);
        setFormSalesTotal(0);
        setFormSalesErr("Gagal memuatkan jualan.");
        return;
      }
      const d = (await r.json()) as SalesPage;
      setFormSales(Array.isArray(d.items) ? d.items : []);
      setFormSalesPage(d.page || page);
      setFormSalesTotal(d.total || 0);
      setFormSalesErr("");
    } catch {
      setFormSales([]);
      setFormSalesTotal(0);
      setFormSalesErr("Gagal memuatkan jualan.");
    }
  }, []);

  // Reload sales whenever the selected form changes; reset to page 1.
  useEffect(() => {
    setFormSalesPage(1);
    void loadFormSales(currentId, 1);
  }, [currentId, loadFormSales]);

  const formSalesTotalPages = Math.max(1, Math.ceil(formSalesTotal / FORM_SALES_PAGE_SIZE));

  function gotoFormSalesPage(p: number) {
    const next = Math.min(Math.max(1, p), formSalesTotalPages);
    setFormSalesPage(next);
    void loadFormSales(currentId, next);
  }

  /* issue #12: capture/release of skip_capture authorizations */
  const [saleActionMsg, setSaleActionMsg] = useState("");
  const [saleActionBusy, setSaleActionBusy] = useState<string | null>(null);

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
        setSaleActionMsg("Jumlah capture tidak sah.");
        return;
      }
      body = JSON.stringify({ amount_sen: Math.round(rmNum * 100) });
    }
    setSaleActionBusy(ref);
    setSaleActionMsg("");
    try {
      const r = await fetch(`/api/rest/sales/${encodeURIComponent(ref)}/capture`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        ...(body ? { body } : {}),
      });
      if (!r.ok) {
        const d = await r.json().catch(() => ({}));
        setSaleActionMsg(d.error || `Capture gagal untuk ${ref}.`);
        return;
      }
      setSaleActionMsg(`Capture berjaya untuk ${ref}. ✓`);
      await loadFormSales(currentId, formSalesPage);
    } finally {
      setSaleActionBusy(null);
    }
  }

  async function releaseSale(ref: string) {
    if (!window.confirm(`Release authorization ${ref}? The buyer's card will NOT be charged.`)) return;
    setSaleActionBusy(ref);
    setSaleActionMsg("");
    try {
      const r = await fetch(`/api/rest/sales/${encodeURIComponent(ref)}/release`, {
        method: "POST",
      });
      if (!r.ok) {
        const d = await r.json().catch(() => ({}));
        setSaleActionMsg(d.error || `Release gagal untuk ${ref}.`);
        return;
      }
      setSaleActionMsg(`Authorization ${ref} released. ✓`);
      await loadFormSales(currentId, formSalesPage);
    } finally {
      setSaleActionBusy(null);
    }
  }

  // hydrate from ?form=<id>
  useEffect(() => {
    load();
    const id = new URLSearchParams(window.location.search).get("form");
    if (id) void (async () => {
      const r = await fetch(`/api/rest/forms/${id}`);
      if (!r.ok) return;
      const d = await r.json();
      setCurrentId(d.id);
      setSlug(d.slug);
      setTitle(d.title);
      setPubUrl(d.status === "published" ? d.url : "");
      const cfg = d.config || {};
      if (d.theme?.primary) setFsTheme(d.theme.primary);
      const hydrated: Block[] = [];
      (cfg.blocks || []).forEach(
        (b: { type: BlockType; value?: string; product_id?: string; required?: boolean; condition?: { field_id: string; op: string; value: string } | null }) => {
          hydrated.push({
            id: uid(),
            type: b.type,
            value: b.value || "",
            productId: b.product_id,
            required: b.required,
            condition: b.condition || null,
          });
        },
      );
      setBlocks(hydrated.length ? hydrated : defaultBlocks(cfg.products || []));
    })();
  }, [load]);

  function makeBlock(bt: BlockType): Block {
    const b: Block = { id: uid(), type: bt, value: "" };
    if (bt === "product" && products[0]) b.productId = products[0].id;
    if (fieldTypes.includes(bt)) {
      b.required = bt === "name" || bt === "email";
      const k = FIELD_LABEL_KEY[bt];
      b.value = k ? t(k) : "";
    }
    return b;
  }

  function addBlock(t: BlockType) {
    const b = makeBlock(t);
    setBlocks((prev) => [...prev, b]);
    setSelected(b.id);
    setDirty(true);
  }

  function updateBlock(id: string, patch: Partial<Block>) {
    setBlocks((prev) => prev.map((b) => (b.id === id ? { ...b, ...patch } : b)));
    setDirty(true);
  }

  function removeBlock(id: string) {
    setBlocks((prev) => prev.filter((b) => b.id !== id));
    if (selected === id) setSelected(null);
    setDirty(true);
  }

  /** Insert a block at index i (or move an existing one there). */
  function insertAt(i: number, t: BlockType | null, fromIdx: number | null) {
    setBlocks((prev) => {
      const copy = [...prev];
      if (fromIdx !== null) {
        const [moved] = copy.splice(fromIdx, 1);
        const target = fromIdx < i ? i - 1 : i;
        copy.splice(Math.max(0, Math.min(target, copy.length)), 0, moved);
      } else if (t) {
        copy.splice(Math.max(0, Math.min(i, copy.length)), 0, makeBlock(t));
      }
      return copy;
    });
    setDirty(true);
  }

  function move(from: number, to: number) {
    insertAt(to, null, from);
  }

  /* drag wiring */




  async function fetchForm(id: string) {
    const r = await fetch(`/api/rest/forms/${id}`);
    if (!r.ok) return;
    const d = await r.json();
    setCurrentId(d.id);
    setSlug(d.slug);
    setTitle(d.title);
    setPubUrl(d.status === "published" ? d.url : "");
    const cfg = d.config || {};
    const hydrated: Block[] = [];
    (cfg.blocks || []).forEach(
      (b: { type: BlockType; value?: string; product_id?: string; required?: boolean; condition?: { field_id: string; op: string; value: string } | null }) => {
        hydrated.push({
          id: uid(),
          type: b.type,
          value: b.value || "",
          productId: b.product_id,
          required: b.required,
        });
      },
    );
    setBlocks(hydrated.length ? hydrated : defaultBlocks(cfg.products || []));
  }

  async function save(publish: boolean) {
    if (!slug) {
      setLoadErr("Slug diperlukan sebelum simpan.");
      return;
    }
    setSaving(true);
    setLoadErr("");
    const config = {
      description: (blocks.find((b) => b.type === "text")?.value || "").slice(0, 300),
      theme: { primary: fsTheme },
      blocks: blocks.map((b) => ({
        type: b.type,
        value: b.value,
        src: b.src,
        product_id: b.productId,
        required: b.required,
        condition: b.condition || undefined,
      })),
      products: products.map((p) => ({ id: p.id, name: p.name, price_sen: p.price_sen })),
    };
    const payload = { slug: slug.toLowerCase().replace(/[^a-z0-9-]/g, "-"), title, config };

    let id = currentId;
    if (!id) {
      const r = await fetch("/api/rest/forms", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      });
      if (!r.ok) {
        const d = await r.json().catch(() => ({}));
        setLoadErr(d.error || "Failed to save");
        setSaving(false);
        return;
      }
      const d = await r.json();
      setCurrentId(d.id);
      id = d.id;
    } else {
      await fetch(`/api/rest/forms/${id}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ title, config }),
      });
    }
    if (publish && id) {
      await fetch(`/api/rest/forms/${id}/publish`, { method: "POST" });
      setPubUrl(`https://${localStorage.getItem("jf_space") || "app"}.jomform.com/${payload.slug}`);
      await load();
    }
    setSaving(false);
    setDirty(false);
  }

  const selectedBlock = blocks.find((b) => b.id === selected);
  const dragging = dragType.current !== null || dragIdx.current !== null;

  return (
    <main className="min-h-screen bg-[var(--jf-bg)] text-[var(--jf-text)]">
      {/* top bar */}
      <header className="sticky top-0 z-20 flex items-center gap-3 border-b border-[var(--jf-border)] bg-white/95 px-4 py-2.5 backdrop-blur">
        <a href="/forms" className="rounded-md px-2 py-1 text-sm text-[var(--jf-muted)] hover:text-[#374151]">
          ←
        </a>
        <input
          value={title}
          onChange={(e) => {
            setTitle(e.target.value);
            setDirty(true);
          }}
          placeholder={t("bld.formTitle")}
          className="w-56 rounded-md border border-transparent bg-transparent px-2 py-1 text-sm font-medium outline-none hover:border-[var(--jf-border)] focus:border-[#7170ff]/60"
        />
        <input
          value={slug}
          onChange={(e) => {
            setSlug(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, "-"));
            setDirty(true);
          }}
          placeholder="slug-url"
          className="w-40 rounded-md border border-transparent bg-transparent px-2 py-1 font-mono text-xs text-[var(--jf-muted)] outline-none hover:border-[var(--jf-border)] focus:border-[#7170ff]/60"
        />
        <div className="ml-auto flex items-center gap-2">
          {dirty && <span className="text-xs text-[#f59e0b]">• {t("builder.unsaved")}</span>}
          {pubUrl && (
            <a
              href={pubUrl}
              target="_blank"
              className="rounded-md border border-[var(--jf-border)] bg-white px-3 py-1.5 text-xs text-[#374151] hover:bg-slate-50"
            >
              {t("bld.view")}
            </a>
          )}
          <button
            onClick={() => save(true)}
            disabled={saving}
            className="jf-btn-primary"
          >
            {saving ? t("builder.saving") : currentId ? t("builder.publish") : t("builder.savePub")}
          </button>
        </div>
      </header>

      <div className="grid w-full grid-cols-1 md:grid-cols-[240px_1fr_260px]">
        {/* ── left: palette (drag sources) ── */}
        <aside className="border-r border-[var(--jf-border)] p-4">
          <p className="mb-3 text-xs font-medium uppercase tracking-wider text-[#9ca3af]">
            {t("bld.dragToAdd")}
          </p>
          <div className="grid grid-cols-2 gap-2">
            {PALETTE.map((p) => (
              <div
                key={p.type}
                draggable
                onDragStart={(e) => paletteDragStart(e, p.type)}
                onClick={() => addBlock(p.type)}
                title={typeHint(p.type)}
                className={`cursor-grab select-none rounded-lg border border-[var(--jf-border)] bg-white px-2.5 py-2 transition-colors hover:border-[#7170ff]/40 hover:bg-indigo-50 active:cursor-grabbing ${
                  dragging ? "opacity-60" : ""
                }`}
              >
                <span className="text-sm">{p.icon}</span>
                <span className="mt-0.5 block text-[11px] font-medium text-[#374151]">{typeLabel(p.type)}</span>
              </div>
            ))}
          </div>
        </aside>

        {/* ── center: canvas (drop zone) ── */}
        <section className="p-6">
          <div className="mx-auto max-w-lg">
            {blocks.length === 0 && (
              <div
                onDragOver={(e) => canvasDragOver(e, 0)}
                onDrop={(e) => canvasDrop(e, 0)}
                className={`rounded-xl border-2 border-dashed px-6 py-16 text-center text-sm transition-colors ${
                  dropAt === 0
                    ? "border-[#7170ff] bg-indigo-50/50 text-[var(--jf-primary)]"
                    : "border-slate-300 text-[#9ca3af]"
                }`}
              >
                {t("bld.dragHere")}
              </div>
            )}
            {blocks.map((b, i) => (
              <div key={b.id}>
                {/* insertion gap */}
                <div
                  onDragOver={(e) => canvasDragOver(e, i)}
                  onDrop={(e) => canvasDrop(e, i)}
                  className={`-my-0.5 rounded transition-all ${
                    dropAt === i && dragging
                      ? "h-8 border-2 border-dashed border-[#7170ff] bg-indigo-50/60"
                      : "h-2"
                  }`}
                />
                <div
                  draggable
                  onDragStart={(e) => blockDragStart(e, i)}
                  onDragEnd={() => {
                    dragIdx.current = null;
                    dragType.current = null;
                    setDropAt(null);
                  }}
                  onDragOver={(e) => canvasDragOver(e, i + 1)}
                  onDrop={(e) => canvasDrop(e, i + 1)}
                  onClick={() => setSelected(b.id)}
                  className={`cursor-grab rounded-lg border bg-white p-3 transition-colors active:cursor-grabbing ${
                    selected === b.id
                      ? "border-[#7170ff]/60 shadow-[0_0_0_1px_rgba(113,112,255,0.4)]"
                      : "border-[var(--jf-border)] hover:border-slate-300"
                  }`}
                >
                  <div className="mb-1.5 flex items-center justify-between text-[10px] uppercase tracking-wider text-[#9ca3af]">
                    <span>{typeLabel(b.type)}</span>
                    <span className="flex gap-1">
                      <button
                        onClick={(e) => {
                          e.stopPropagation();
                          move(i, i - 1);
                        }}
                        className="px-1 hover:text-[#374151]"
                        title={t("bld.moveUp")}
                      >
                        ↑
                      </button>
                      <button
                        onClick={(e) => {
                          e.stopPropagation();
                          move(i, i + 1);
                        }}
                        className="px-1 hover:text-[#374151]"
                        title={t("bld.moveDown")}
                      >
                        ↓
                      </button>
                      <button
                        onClick={(e) => {
                          e.stopPropagation();
                          removeBlock(b.id);
                        }}
                        className="px-1 hover:text-red-400"
                        title={t("bld.removeBlock")}
                      >
                        ✕
                      </button>
                    </span>
                  </div>
                  <BlockPreview block={b} products={products} />
                </div>
              </div>
            ))}
            {/* trailing drop zone */}
            {blocks.length > 0 && (
              <div
                onDragOver={(e) => canvasDragOver(e, blocks.length)}
                onDrop={(e) => canvasDrop(e, blocks.length)}
                className={`-mt-1 rounded transition-all ${
                  dropAt === blocks.length && dragging
                    ? "h-10 border-2 border-dashed border-[#7170ff] bg-indigo-50/60"
                    : "h-4"
                }`}
              />
            )}
          </div>
          {loadErr && <p className="mt-4 text-center text-xs text-red-400">{loadErr}</p>}
        </section>

        {/* ── right: inspector ── */}
        <aside className="border-l border-[var(--jf-border)] p-4">
          <p className="mb-3 text-xs font-medium uppercase tracking-wider text-[#9ca3af]">{t("bld.blockSettings")}</p>
          {!selectedBlock && <p className="text-xs text-[#9ca3af]">{t("bld.selectBlock")}</p>}
          {selectedBlock && (
            <div className="space-y-3">
              <div>
                <label className="text-xs text-[var(--jf-muted)]">{t("bld.textLabel")}</label>
                <textarea
                  rows={2}
                  value={selectedBlock.value}
                  onChange={(e) => updateBlock(selectedBlock.id, { value: e.target.value })}
                  className="mt-1 w-full rounded-md border border-[var(--jf-border)] bg-white px-2.5 py-2 text-sm outline-none focus:border-[#7170ff]/60"
                />
              </div>
              {selectedBlock.type === "image" && (
                <div>
                  <label className="text-xs text-[var(--jf-muted)]">{t("bld.imageUrl")}</label>
                  <input
                    value={selectedBlock.src || ""}
                    onChange={(e) => updateBlock(selectedBlock.id, { src: e.target.value })}
                    placeholder="https://…"
                    className="mt-1 w-full rounded-md border border-[var(--jf-border)] bg-white px-2.5 py-2 text-sm outline-none focus:border-[#7170ff]/60"
                  />
                </div>
              )}
              {selectedBlock.type === "product" && (
                <div>
                  <label className="text-xs text-[var(--jf-muted)]">{t("bld.product")}</label>
                  <select
                    value={selectedBlock.productId || ""}
                    onChange={(e) => updateBlock(selectedBlock.id, { productId: e.target.value })}
                    className="mt-1 w-full rounded-md border border-[var(--jf-border)] bg-white px-2.5 py-2 text-sm outline-none"
                  >
                    {products.map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.name} — {money(p.price_sen)}
                      </option>
                    ))}
                  </select>
                </div>
              )}
              {fieldTypes.includes(selectedBlock.type) && (
                <label className="flex items-center gap-2 text-sm text-[#374151]">
                  <input
                    type="checkbox"
                    checked={!!selectedBlock.required}
                    onChange={(e) => updateBlock(selectedBlock.id, { required: e.target.checked })}
                    className="accent-[#5e6ad2]"
                  />
                  {t("bld.required")}
                </label>
              )}

              {/* conditional logic */}
              {fieldTypes.includes(selectedBlock.type) && blocks.length > 1 && (
                <div className="rounded-md border border-slate-200 p-2">
                  <p className="text-[11px] font-medium text-slate-500">
                    {t("bld.condLogic")}
                  </p>
                  <select
                    value={selectedBlock.condition?.field_id || ""}
                    onChange={(e) =>
                      updateBlock(selectedBlock.id, {
                        condition: e.target.value
                          ? { ...(selectedBlock.condition || { op: "equals", value: "" }), field_id: e.target.value }
                          : null,
                      })
                    }
                    className="mt-1 w-full rounded border border-slate-200 px-2 py-1 text-xs"
                  >
                    <option value="">{t("bld.alwaysShow")}</option>
                    {blocks
                      .filter((x) => x.id !== selectedBlock.id && fieldTypes.includes(x.type))
                      .map((x) => (
                        <option key={x.id} value={x.id}>
                          {x.value || x.type} (#{blocks.indexOf(x) + 1})
                        </option>
                      ))}
                  </select>
                  {selectedBlock.condition && (
                    <div className="mt-1 flex gap-1">
                      <select
                        value={selectedBlock.condition.op}
                        onChange={(e) =>
                          updateBlock(selectedBlock.id, {
                            condition: { ...selectedBlock.condition!, op: e.target.value },
                          })
                        }
                        className="w-28 rounded border border-slate-200 px-1 py-1 text-xs"
                      >
                        <option value="equals">equals</option>
                        <option value="not_equals">not equals</option>
                        <option value="contains">contains</option>
                        <option value="gt">&gt;</option>
                        <option value="lt">&lt;</option>
                        <option value="not_empty">not empty</option>
                      </select>
                      <input
                        value={selectedBlock.condition.value}
                        onChange={(e) =>
                          updateBlock(selectedBlock.id, {
                            condition: { ...selectedBlock.condition!, value: e.target.value },
                          })
                        }
                        placeholder="value"
                        className="min-w-0 flex-1 rounded border border-slate-200 px-2 py-1 text-xs"
                      />
                    </div>
                  )}
                </div>
              )}
            </div>
          )}


        {/* ── Display settings ── */}
        {currentId && (
          <div className="mt-8 rounded-lg border border-[var(--jf-border)] bg-white p-3">
            <p className="text-xs font-medium uppercase tracking-wider text-[#9ca3af]">
              {t("bld.displaySettings")}
            </p>
            <label className="mt-2 block text-[11px] text-slate-500">{t("bld.themeColor")}</label>
            <input
              type="color"
              value={fsTheme}
              onChange={(e) => setFsTheme(e.target.value)}
              className="mt-1 h-8 w-full cursor-pointer rounded border border-[var(--jf-border)]"
            />
            <label className="mt-2 block text-[11px] text-slate-500">{t("bld.logoUrl")}</label>
            <input
              value={fsLogo}
              onChange={(e) => setFsLogo(e.target.value)}
              placeholder="https://cdn.example/logo.png"
              className="jf-input mt-1 text-xs"
            />
            <label className="mt-2 block text-[11px] text-slate-500">{t("bld.redirSuccess")}</label>
            <input
              value={fsRedS}
              onChange={(e) => setFsRedS(e.target.value)}
              placeholder="https://yourapp.com/thanks"
              className="jf-input mt-1 text-xs"
            />
            <label className="mt-2 block text-[11px] text-slate-500">{t("bld.redirFailure")}</label>
            <input
              value={fsRedF}
              onChange={(e) => setFsRedF(e.target.value)}
              placeholder="https://yourapp.com/fail"
              className="jf-input mt-1 text-xs"
            />
            <label className="mt-2 flex items-center gap-2 text-xs text-slate-600">
              <input type="checkbox" checked={fsEmbed} onChange={(e) => setFsEmbed(e.target.checked)} className="accent-[#5e6ad2]" />
              {t("bld.allowEmbed")}
            </label>
            <button
              onClick={async () => {
                const r = await fetch(`/api/rest/forms/${currentId}/settings`, {
                  method: "PUT", headers: { "Content-Type": "application/json" },
                  body: JSON.stringify({
                    redirect_success: fsRedS, redirect_failure: fsRedF,
                    allow_embed: fsEmbed, logo_url: fsLogo,
                  }),
                });
                setOvMsg(r.ok ? t("bld.displaySaved") : t("prof.failed"));
              }}
              className="mt-2 w-full rounded-md bg-[var(--jf-primary)] py-1.5 text-xs font-medium text-white"
            >
              {t("bld.saveDisplay")}
            </button>

            <p className="mt-3 text-[11px] font-medium uppercase tracking-wider text-[#9ca3af]">{t("bld.embedOnSite")}</p>
            <pre className="mt-1 overflow-x-auto rounded-md bg-slate-900 p-2 text-[10px] text-emerald-300">{`<iframe src="${origin}/embed/f/wan/${slug}" style="width:100%;height:640px;border:0" title="${title}"></iframe>`}</pre>
            <button
              onClick={() => {
                navigator.clipboard.writeText(
                  `<iframe src="${origin}/embed/f/wan/${slug}" style="width:100%;height:640px;border:0" title="${title}"></iframe>`,
                );
                setOvMsg(t("bld.embedCopied"));
              }}
              className="mt-1 w-full rounded-md border border-[var(--jf-border)] py-1.5 text-xs text-slate-600 hover:bg-slate-50"
            >
              {t("bld.copyEmbed")}
            </button>
          </div>
        )}

          {/* per-form tracking */}
          {currentId && (
            <div className="mt-8 rounded-lg border border-[var(--jf-border)] bg-white p-3">
              <p className="text-xs font-medium uppercase tracking-wider text-[#9ca3af]">
                {t("bld.trackingTitle")}
              </p>
              <textarea
                value={formTrack}
                onChange={(e) => setFormTrack(e.target.value)}
                rows={3}
                placeholder={'<script src="https://…pixel/js"></script>'}
                className="mt-2 w-full rounded-md border border-[var(--jf-border)] px-2.5 py-2 font-mono text-[11px]"
              />
              <button
                onClick={async () => {
                  const r = await fetch(`/api/rest/forms/${currentId}/tracking`, {
                    method: "PUT", headers: { "Content-Type": "application/json" },
                    body: JSON.stringify({ head: formTrack }),
                  });
                  setOvMsg(r.ok ? t("bld.formTrackingSaved") : t("prof.failed"));
                }}
                className="mt-1.5 w-full rounded-md border border-[var(--jf-border)] py-1.5 text-xs text-slate-600 hover:bg-slate-50"
              >
                {t("bld.saveFormTracking")}
              </button>
            </div>
          )}

          {/* per-form analytics mini-card */}
          {currentId && formStat && (
            <div className="mt-8 rounded-lg border border-[var(--jf-border)] bg-white p-3">
              <p className="text-xs font-medium uppercase tracking-wider text-[#9ca3af]">
                {t("bld.thisFormSoFar")}
              </p>
              <div className="mt-2 grid grid-cols-2 gap-1.5 text-[11px] text-[var(--jf-muted)]">
                <span>{t("dash.submissions")} <b>{formStat.submissions}</b></span>
                <span>{t("dash.paidConfirmed")} <b className="text-emerald-600">{formStat.paid_count + formStat.confirmed_count}</b></span>
              </div>
              <p className="mt-1 text-sm font-semibold text-[var(--jf-primary)]">
                {`RM${(formStat.revenue_sen / 100).toFixed(2)}`} {t("bld.confirmedRevenue")}
              </p>
            </div>
          )}

          {/* per-form sales table (issue #11) */}
          {currentId && (
            <div className="mt-8 rounded-lg border border-[var(--jf-border)] bg-white p-3">
              <div className="flex items-center justify-between gap-2">
                <p className="text-xs font-medium uppercase tracking-wider text-[#9ca3af]">
                  {t("bld.salesThisForm")}
                </p>
                <a
                  href={`/api/rest/sales/export.csv?form_id=${currentId}`}
                  className="whitespace-nowrap rounded-md border border-[var(--jf-border)] px-2.5 py-1 text-xs text-slate-600 hover:bg-slate-50"
                >
                  {t("common.exportCsv")}
                </a>
              </div>
              <div className="mt-2 overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-[var(--jf-border)] text-left text-[10px] uppercase tracking-wide text-[var(--jf-muted)]">
                      <th className="px-2 py-1.5 font-medium">{t("dash.ref")}</th>
                      <th className="px-2 py-1.5 font-medium">{t("dash.buyer")}</th>
                      <th className="px-2 py-1.5 font-medium">{t("dash.amount")}</th>
                      <th className="px-2 py-1.5 font-medium">{t("dash.status")}</th>
                      <th className="px-2 py-1.5" />
                      <th className="px-2 py-1.5 font-medium">{t("bld.actions")}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {formSales.map((s) => (
                      <tr key={s.ref_code} className="border-b border-[var(--jf-border)] last:border-0 hover:bg-slate-50/60">
                        <td className="whitespace-nowrap px-2 py-2 font-mono text-[11px]">{s.ref_code}</td>
                        <td className="max-w-24 truncate px-2 py-2 text-xs">{s.buyer || "—"}</td>
                        <td className="whitespace-nowrap px-2 py-2 text-xs font-medium">{money(s.total_sen)}</td>
                        <td className="whitespace-nowrap px-2 py-2">
                          <span className={`rounded-full border px-2 py-0.5 text-[10px] font-medium ${saleBadge(s.status)}`}>
                            {s.status}
                          </span>
                        </td>
                        <td className="px-2 py-2 text-right text-[10px] text-[var(--jf-muted)]">
                          {s.created_at.slice(0, 10)}
                        </td>
                        <td className="whitespace-nowrap px-2 py-2 text-right">
                          {isCapturePending(s) ? (
                            <div className="flex items-center justify-end gap-1.5">
                              <button
                                type="button"
                                onClick={() => void captureSale(s.ref_code)}
                                disabled={saleActionBusy !== null}
                                className="rounded-md bg-[#5e6ad2] px-2 py-1 text-[10px] font-medium text-white hover:bg-[#828fff] disabled:cursor-not-allowed disabled:opacity-40"
                              >
                                {saleActionBusy === s.ref_code ? "…" : t("bld.capture")}
                              </button>
                              <button
                                type="button"
                                onClick={() => void releaseSale(s.ref_code)}
                                disabled={saleActionBusy !== null}
                                className="rounded-md border border-[var(--jf-border)] px-2 py-1 text-[10px] text-slate-600 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-40"
                              >
                                {t("bld.release")}
                              </button>
                            </div>
                          ) : (
                            <span className="text-[10px] text-[var(--jf-muted)]">—</span>
                          )}
                        </td>
                      </tr>
                    ))}
                    {formSales.length === 0 && (
                      <tr>
                        <td colSpan={6} className="px-2 py-6 text-center text-xs text-[var(--jf-muted)]">
                          {formSalesErr || t("bld.noSalesForm")}
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
              {saleActionMsg && (
                <p className="mt-1.5 text-[11px] text-[var(--jf-primary)]">{saleActionMsg}</p>
              )}
              {formSalesTotal > 0 && (
                <div className="mt-2 flex items-center justify-between gap-2 border-t border-[var(--jf-border)] pt-2">
                  <span className="text-[10px] text-[var(--jf-muted)]">
                    {formSalesTotal} {t("dash.records")} · {t("dash.pageOf", { page: formSalesPage, total: formSalesTotalPages })}
                  </span>
                  <div className="flex items-center gap-1.5">
                    <button
                      type="button"
                      onClick={() => gotoFormSalesPage(formSalesPage - 1)}
                      disabled={formSalesPage <= 1}
                      className="rounded-md border border-[var(--jf-border)] px-2.5 py-1 text-xs text-slate-600 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-40"
                    >
                      {t("common.prev")}
                    </button>
                    <button
                      type="button"
                      onClick={() => gotoFormSalesPage(formSalesPage + 1)}
                      disabled={formSalesPage >= formSalesTotalPages}
                      className="rounded-md border border-[var(--jf-border)] px-2.5 py-1 text-xs text-slate-600 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-40"
                    >
                      {t("common.next")}
                    </button>
                  </div>
                </div>
              )}
            </div>
          )}

          <p className="mb-2 mt-8 text-xs font-medium uppercase tracking-wider text-[#9ca3af]">
            {t("bld.existingForms")}
          </p>
          <div className="space-y-1.5">
            {forms.map((f) => (
              <button
                key={f.id}
                onClick={() => fetchForm(f.id)}
                className={`w-full rounded-md border px-3 py-2 text-left text-[13px] transition-colors ${
                  currentId === f.id
                    ? "border-[#7170ff]/50 bg-indigo-50 text-[var(--jf-text)]"
                    : "border-[var(--jf-border)] bg-white text-[#374151] hover:bg-slate-50"
                }`}
              >
                <span className="block truncate">{f.title}</span>
                <span className="text-[10px] text-[#9ca3af]">
                  /f/{f.slug}
                  {f.status === "published" ? " · LIVE" : ""}
                </span>
              </button>
            ))}
          </div>

          {/* ── per-form CHIP override ── */}
          {currentId && (
            <div className="mt-8 rounded-lg border border-[var(--jf-border)] bg-white p-3">
              <p className="text-xs font-medium uppercase tracking-wider text-[#9ca3af]">
                {t("bld.chipOverride")}
              </p>
              <p className="mt-1 text-[11px] text-[var(--jf-muted)]">
                {t("bld.emptyDefaults")}
              </p>
              <div className="mt-2 space-y-2">
                <input
                  value={ovBrand}
                  onChange={(e) => setOvBrand(e.target.value)}
                  placeholder="Brand ID"
                  className="w-full rounded-md border border-[var(--jf-border)] px-2.5 py-1.5 text-xs outline-none focus:border-[#7170ff]/60"
                />
                <input
                  value={ovSecret}
                  onChange={(e) => setOvSecret(e.target.value)}
                  type="text"
                  placeholder="Secret Key (sk_live_… / sk_test_…)"
                  className="w-full rounded-md border border-[var(--jf-border)] px-2.5 py-1.5 text-xs outline-none focus:border-[#7170ff]/60"
                />
                <input
                  value={ovFallback}
                  onChange={(e) => setOvFallback(e.target.value)}
                  type="email"
                  placeholder="Email fallback (one-time forms)"
                  className="w-full rounded-md border border-[var(--jf-border)] px-2.5 py-1.5 text-xs outline-none focus:border-[#7170ff]/60"
                />
                <div className="flex gap-1.5">
                  <button
                    onClick={async () => {
                      if (!currentId || !ovBrand || !ovSecret) return;
                      setOvMsg("");
                      const r = await fetch(`/api/rest/forms/${currentId}/chip-override`, {
                        method: "PUT",
                        headers: { "Content-Type": "application/json" },
                        body: JSON.stringify({
                          brand_id: ovBrand,
                          secret_key: ovSecret,
                          email_fallback: ovFallback,
                        }),
                      });
                      setOvMsg(r.ok ? t("bld.overrideSaved") : t("bld.failedOverride"));
                      if (r.ok) setOvSecret("");
                    }}
                    disabled={!currentId || !ovBrand || !ovSecret}
                    className="flex-1 rounded-md bg-[#5e6ad2] py-1.5 text-xs font-medium text-white hover:bg-[#828fff] disabled:opacity-50"
                  >
                    {t("bld.saveOverride")}
                  </button>
                  <button
                    onClick={async () => {
                      if (!currentId) return;
                      const r = await fetch(`/api/rest/forms/${currentId}/chip-override`, { method: "DELETE" });
                      setOvMsg(r.ok ? t("bld.overrideCleared") : t("prof.failed"));
                      setOvBrand("");
                      setOvSecret("");
                      setOvFallback("");
                    }}
                    disabled={!currentId}
                    className="rounded-md border border-[var(--jf-border)] px-3 py-1.5 text-xs text-slate-600 hover:bg-slate-50 disabled:opacity-50"
                  >
                    {t("set.clear")}
                  </button>
                </div>
                {ovMsg && <p className="text-[11px] text-[var(--jf-primary)]">{ovMsg}</p>}
              </div>
            </div>
          )}
        </aside>
      </div>
    </main>
  );

  function paletteDragStart(e: React.DragEvent, t: BlockType) {
    dragType.current = t;
    dragIdx.current = null;
    e.dataTransfer.effectAllowed = "copy";
    e.dataTransfer.setData("text/plain", `type:${t}`);
  }

  function blockDragStart(e: React.DragEvent, i: number) {
    dragIdx.current = i;
    dragType.current = null;
    e.dataTransfer.effectAllowed = "move";
    e.dataTransfer.setData("text/plain", `idx:${i}`);
  }

  function canvasDragOver(e: React.DragEvent, i: number) {
    e.preventDefault();
    e.stopPropagation();
    if (!dragging) return;
    setDropAt(i);
  }

  function canvasDrop(e: React.DragEvent, i: number) {
    e.preventDefault();
    e.stopPropagation();
    const t = dragType.current;
    const from = dragIdx.current;
    dragType.current = null;
    dragIdx.current = null;
    setDropAt(null);
    if (t !== null) insertAt(i, t, null);
    else if (from !== null) insertAt(i, null, from);
  }
}

/* renders a non-interactive preview of each block type */
function BlockPreview({ block, products }: { block: Block; products: Product[] }) {
  const p = products.find((x) => x.id === block.productId);
  switch (block.type) {
    case "text":
      return <p className="text-sm text-[#374151]">{block.value || "Text…"}</p>;
    case "image":
      return block.src ? (
        // eslint-disable-next-line @next/next/no-img-element
        <img src={block.src} alt="" className="max-h-40 rounded-md border border-[var(--jf-border)] object-cover" />
      ) : (
        <div className="flex h-24 items-center justify-center rounded-md border border-dashed border-slate-300 text-xs text-[#9ca3af]">
          Image URL →
        </div>
      );
    case "product":
      return (
        <div className="flex items-center justify-between rounded-md border border-[var(--jf-border)] bg-slate-50 px-3 py-2">
          <span className="text-sm">{block.value || p?.name || "Product"}</span>
          <span className="text-sm font-medium text-[var(--jf-primary)]">{p ? money(p.price_sen) : "—"}</span>
        </div>
      );
    case "divider":
      return <hr className="border-[var(--jf-border)]" />;
    case "checkbox":
      return (
        <label className="flex items-center gap-2 text-sm text-[#374151]">
          <span className="inline-block h-4 w-4 rounded border border-slate-300" /> {block.value}
        </label>
      );
    default:
      return (
        <div>
          <p className="mb-1 text-xs text-[var(--jf-muted)]">
            {block.value || "Field"}
            {block.required && <span className="text-red-400"> *</span>}
          </p>
          <div className="h-8 rounded-md border border-[var(--jf-border)] bg-slate-50" />
        </div>
      );
  }
}