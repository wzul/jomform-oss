"use client";

import { useCallback, useEffect, useRef, useState } from "react";

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
};

type Product = { id: string; name: string; price_sen: number; type: string };

type FormRow = { id: string; slug: string; title: string; status: string };

const PALETTE: { type: BlockType; label: string; icon: string; hint: string }[] = [
  { type: "text", label: "Text", icon: "T", hint: "Paragraph / description" },
  { type: "image", label: "Image", icon: "🖼", hint: "Product photo / banner" },
  { type: "product", label: "Product", icon: "🛒", hint: "Buyable item with price" },
  { type: "name", label: "Name", icon: "🔤", hint: "Buyer name field" },
  { type: "email", label: "Email", icon: "✉", hint: "Buyer email field" },
  { type: "phone", label: "Phone", icon: "☎", hint: "Phone (WhatsApp) field" },
  { type: "textarea", label: "Note", icon: "📝", hint: "Free-form message" },
  { type: "checkbox", label: "Checkbox", icon: "☑", hint: "Extra option" },
  { type: "coupon", label: "Coupon", icon: "🏷", hint: "Discount code" },
  { type: "divider", label: "Divider", icon: "―", hint: "Separator line" },
];

const fieldTypes: BlockType[] = ["name", "email", "phone", "textarea", "checkbox", "coupon"];

function uid() {
  return Math.random().toString(36).slice(2, 9);
}

const money = (sen: number) =>
  `RM${(sen / 100).toLocaleString("en-MY", { minimumFractionDigits: 2 })}`;

function labelFor(t: BlockType) {
  switch (t) {
    case "name":
      return "Full name";
    case "email":
      return "Email";
    case "phone":
      return "Phone number";
    case "textarea":
      return "Additional note";
    default:
      return "";
  }
}

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
      const hydrated: Block[] = [];
      (cfg.blocks || []).forEach(
        (b: { type: BlockType; value?: string; product_id?: string; required?: boolean }) => {
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
    })();
  }, [load]);

  function makeBlock(t: BlockType): Block {
    const b: Block = { id: uid(), type: t, value: "" };
    if (t === "product" && products[0]) b.productId = products[0].id;
    if (fieldTypes.includes(t)) {
      b.required = t === "name" || t === "email";
      b.value = labelFor(t);
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
      (b: { type: BlockType; value?: string; product_id?: string; required?: boolean }) => {
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
      blocks: blocks.map((b) => ({
        type: b.type,
        value: b.value,
        src: b.src,
        product_id: b.productId,
        required: b.required,
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
      setPubUrl(`/f/${payload.slug}`);
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
          placeholder="Form title"
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
          {dirty && <span className="text-xs text-[#f59e0b]">• unsaved</span>}
          {pubUrl && (
            <a
              href={pubUrl}
              target="_blank"
              className="rounded-md border border-[var(--jf-border)] bg-white px-3 py-1.5 text-xs text-[#374151] hover:bg-slate-50"
            >
              View ↗
            </a>
          )}
          <button
            onClick={() => save(true)}
            disabled={saving}
            className="jf-btn-primary px-4 py-1.5 text-sm"
          >
            {saving ? "Saving…" : currentId ? "Publish" : "Save & publish"}
          </button>
        </div>
      </header>

      <div className="mx-auto grid max-w-7xl grid-cols-1 md:grid-cols-[240px_1fr_260px]">
        {/* ── left: palette (drag sources) ── */}
        <aside className="border-r border-[var(--jf-border)] p-4">
          <p className="mb-3 text-xs font-medium uppercase tracking-wider text-[#9ca3af]">
            Drag to add
          </p>
          <div className="grid grid-cols-2 gap-2">
            {PALETTE.map((p) => (
              <div
                key={p.type}
                draggable
                onDragStart={(e) => paletteDragStart(e, p.type)}
                onClick={() => addBlock(p.type)}
                title={p.hint}
                className={`cursor-grab select-none rounded-lg border border-[var(--jf-border)] bg-white px-2.5 py-2 transition-colors hover:border-[#7170ff]/40 hover:bg-indigo-50 active:cursor-grabbing ${
                  dragging ? "opacity-60" : ""
                }`}
              >
                <span className="text-sm">{p.icon}</span>
                <span className="mt-0.5 block text-[11px] font-medium text-[#374151]">{p.label}</span>
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
                Drag blocks here — or click one on the left.
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
                    <span>{PALETTE.find((p) => p.type === b.type)?.label}</span>
                    <span className="flex gap-1">
                      <button
                        onClick={(e) => {
                          e.stopPropagation();
                          move(i, i - 1);
                        }}
                        className="px-1 hover:text-[#374151]"
                        title="Move up"
                      >
                        ↑
                      </button>
                      <button
                        onClick={(e) => {
                          e.stopPropagation();
                          move(i, i + 1);
                        }}
                        className="px-1 hover:text-[#374151]"
                        title="Move down"
                      >
                        ↓
                      </button>
                      <button
                        onClick={(e) => {
                          e.stopPropagation();
                          removeBlock(b.id);
                        }}
                        className="px-1 hover:text-red-400"
                        title="Remove"
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
          <p className="mb-3 text-xs font-medium uppercase tracking-wider text-[#9ca3af]">Block settings</p>
          {!selectedBlock && <p className="text-xs text-[#9ca3af]">Select a block to edit.</p>}
          {selectedBlock && (
            <div className="space-y-3">
              <div>
                <label className="text-xs text-[var(--jf-muted)]">Text / label</label>
                <textarea
                  rows={2}
                  value={selectedBlock.value}
                  onChange={(e) => updateBlock(selectedBlock.id, { value: e.target.value })}
                  className="mt-1 w-full rounded-md border border-[var(--jf-border)] bg-white px-2.5 py-2 text-sm outline-none focus:border-[#7170ff]/60"
                />
              </div>
              {selectedBlock.type === "image" && (
                <div>
                  <label className="text-xs text-[var(--jf-muted)]">Image URL</label>
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
                  <label className="text-xs text-[var(--jf-muted)]">Product</label>
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
                  Required
                </label>
              )}
            </div>
          )}

          <p className="mb-2 mt-8 text-xs font-medium uppercase tracking-wider text-[#9ca3af]">
            Existing forms
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