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
  value: string; // text content / label
  src?: string; // image
  productId?: string;
  required?: boolean;
};

type Product = { id: string; name: string; price_sen: number; type: string };

type FormRow = { id: string; slug: string; title: string; status: string };

const PALETTE: { type: BlockType; label: string; icon: string; hint: string }[] = [
  { type: "text", label: "Teks", icon: "T", hint: "Perenggan / penerangan" },
  { type: "image", label: "Imej", icon: "🖼", hint: "Foto produk / banner" },
  { type: "product", label: "Produk", icon: "🛒", hint: "Pilihan beli dengan harga" },
  { type: "name", label: "Nama", icon: "🔤", hint: "Medan nama pembeli" },
  { type: "email", label: "Emel", icon: "✉", hint: "Medan emel pembeli" },
  { type: "phone", label: "Telefon", icon: "☎", hint: "Medan telefon (WhatsApp)" },
  { type: "textarea", label: "Nota", icon: "📝", hint: "Mesej bebas pembeli" },
  { type: "checkbox", label: "Checkbox", icon: "☑", hint: "Pilihan tambahan" },
  { type: "coupon", label: "Kupon", icon: "🏷", hint: "Kod diskaun" },
  { type: "divider", label: "Pemisah", icon: "―", hint: "Garis pemisah" },
];

const fieldTypes: BlockType[] = ["name", "email", "phone", "textarea", "checkbox", "coupon"];

function uid() {
  return Math.random().toString(36).slice(2, 9);
}

const money = (sen: number) =>
  `RM${(sen / 100).toLocaleString("ms-MY", { minimumFractionDigits: 2 })}`;

/* ─── builder page ───────────────────────────────────────────────────────── */

export default function Builder() {
  const [products, setProducts] = useState<Product[]>([]);
  const [forms, setForms] = useState<FormRow[]>([]);
  const [currentId, setCurrentId] = useState<string | null>(null);
  const [slug, setSlug] = useState("");
  const [title, setTitle] = useState("Borang Baharu");
  const [blocks, setBlocks] = useState<Block[]>([]);
  const [selected, setSelected] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [pubUrl, setPubUrl] = useState("");
  const [loadErr, setLoadErr] = useState("");
  const dragIndex = useRef<number | null>(null);

  const load = useCallback(async () => {
    const [p, f] = await Promise.all([
      fetch("/api/rest/products").then((r) => (r.ok ? r.json() : [])),
      fetch("/api/rest/forms").then((r) => (r.ok ? r.json() : [])),
    ]);
    setProducts(p);
    setForms(f);
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  async function fetchForm(id: string) {
    const r = await fetch(`/api/rest/forms/${id}`);
    if (!r.ok) return;
    const d = await r.json();
    setCurrentId(d.id);
    setSlug(d.slug);
    setTitle(d.title);
    setPubUrl(d.status === "published" ? d.url : "");
    // hydrate blocks from config
    const cfg = d.config || {};
    const hydrated: Block[] = [];
    (cfg.blocks || []).forEach((b: { type: BlockType; value?: string; product_id?: string; required?: boolean }) => {
      hydrated.push({ id: uid(), type: b.type, value: b.value || "", productId: b.product_id, required: b.required });
    });
    setBlocks(hydrated.length ? hydrated : defaultBlocks(cfg.products || []));
  }

  function addBlock(t: BlockType) {
    const b: Block = { id: uid(), type: t, value: "" };
    if (t === "product" && products[0]) b.productId = products[0].id;
    if (fieldTypes.includes(t)) {
      b.required = t === "name" || t === "email";
      b.value = labelFor(t);
    }
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

  function move(from: number, to: number) {
    if (from === to || to < 0 || to >= blocks.length) return;
    setBlocks((prev) => {
      const copy = [...prev];
      const [b] = copy.splice(from, 1);
      copy.splice(to, 0, b);
      return copy;
    });
    setDirty(true);
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
        setLoadErr(d.error || "Gagal menyimpan");
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

  /* drop handlers */
  function onDropInto(i: number) {
    if (dragIndex.current === null) return;
    move(dragIndex.current, i);
    dragIndex.current = null;
  }

  const selectedBlock = blocks.find((b) => b.id === selected);

  return (
    <main className="min-h-screen bg-[#08090a] text-[#f7f8f8]">
      {/* top bar */}
      <header className="sticky top-0 z-20 flex items-center gap-3 border-b border-white/[0.06] bg-[#0f1011]/95 px-4 py-2.5 backdrop-blur">
        <a href="/" className="rounded-md px-2 py-1 text-sm text-[#8a8f98] hover:text-[#d0d6e0]">
          ←
        </a>
        <input
          value={title}
          onChange={(e) => {
            setTitle(e.target.value);
            setDirty(true);
          }}
          placeholder="Tajuk borang"
          className="w-56 rounded-md border border-transparent bg-transparent px-2 py-1 text-sm font-medium outline-none hover:border-white/[0.08] focus:border-[#7170ff]/60"
        />
        <input
          value={slug}
          onChange={(e) => {
            setSlug(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, "-"));
            setDirty(true);
          }}
          placeholder="slug-url"
          className="w-40 rounded-md border border-transparent bg-transparent px-2 py-1 font-mono text-xs text-[#8a8f98] outline-none hover:border-white/[0.08] focus:border-[#7170ff]/60"
        />
        <div className="ml-auto flex items-center gap-2">
          {dirty && <span className="text-xs text-[#f59e0b]">• belum disimpan</span>}
          {pubUrl && (
            <a
              href={pubUrl}
              target="_blank"
              className="rounded-md border border-white/[0.08] bg-white/[0.02] px-3 py-1.5 text-xs text-[#d0d6e0] hover:bg-white/[0.05]"
            >
              Lihat ↗
            </a>
          )}
          <button
            onClick={() => save(true)}
            disabled={saving}
            className="rounded-md bg-[#5e6ad2] px-4 py-1.5 text-sm font-medium text-white hover:bg-[#828fff] disabled:opacity-50"
          >
            {saving ? "Menyimpan…" : currentId ? "Publish" : "Simpan & Publish"}
          </button>
        </div>
      </header>

      <div className="mx-auto grid max-w-7xl grid-cols-1 gap-0 md:grid-cols-[240px_1fr_260px]">
        {/* ── left: palette ── */}
        <aside className="border-r border-white/[0.06] p-4">
          <p className="mb-3 text-xs font-medium uppercase tracking-wider text-[#62666d]">Tambah blok</p>
          <div className="grid grid-cols-2 gap-2">
            {PALETTE.map((p) => (
              <button
                key={p.type}
                onClick={() => addBlock(p.type)}
                title={p.hint}
                className="group rounded-lg border border-white/[0.06] bg-white/[0.02] px-2.5 py-2 text-left transition-colors hover:border-[#7170ff]/40 hover:bg-[#5e6ad2]/10"
              >
                <span className="text-sm">{p.icon}</span>
                <span className="mt-0.5 block text-[11px] font-medium text-[#d0d6e0]">{p.label}</span>
              </button>
            ))}
          </div>
        </aside>

        {/* ── center: canvas ── */}
        <section className="p-6">
          <div className="mx-auto max-w-lg space-y-2">
            {blocks.length === 0 && (
              <div className="rounded-xl border border-dashed border-white/[0.12] px-6 py-16 text-center text-sm text-[#62666d]">
                Klik blok di kiri untuk mula membina borang anda.
              </div>
            )}
            {blocks.map((b, i) => (
              <div
                key={b.id}
                draggable
                onDragStart={() => (dragIndex.current = i)}
                onDragOver={(e) => e.preventDefault()}
                onDrop={() => move(dragIndex.current ?? i, i)}
                onClick={() => setSelected(b.id)}
                className={`cursor-grab rounded-lg border bg-white/[0.02] p-3 transition-colors active:cursor-grabbing ${
                  selected === b.id
                    ? "border-[#7170ff]/60 shadow-[0_0_0_1px_rgba(113,112,255,0.4)]"
                    : "border-white/[0.06] hover:border-white/[0.12]"
                }`}
              >
                <div className="mb-1.5 flex items-center justify-between text-[10px] uppercase tracking-wider text-[#62666d]">
                  <span>{PALETTE.find((p) => p.type === b.type)?.label}</span>
                  <span className="flex gap-1">
                    <button
                      onClick={(e) => {
                        e.stopPropagation();
                        move(i, i - 1);
                      }}
                      className="px-1 hover:text-[#d0d6e0]"
                      title="Naik"
                    >
                      ↑
                    </button>
                    <button
                      onClick={(e) => {
                        e.stopPropagation();
                        move(i, i + 1);
                      }}
                      className="px-1 hover:text-[#d0d6e0]"
                      title="Turun"
                    >
                      ↓
                    </button>
                    <button
                      onClick={(e) => {
                        e.stopPropagation();
                        removeBlock(b.id);
                      }}
                      className="px-1 hover:text-red-400"
                      title="Buang"
                    >
                      ✕
                    </button>
                  </span>
                </div>
                <BlockPreview block={b} products={products} />
              </div>
            ))}
          </div>
          {loadErr && <p className="mt-4 text-center text-xs text-red-400">{loadErr}</p>}
        </section>

        {/* ── right: inspector ── */}
        <aside className="border-l border-white/[0.06] p-4">
          <p className="mb-3 text-xs font-medium uppercase tracking-wider text-[#62666d]">Ciri blok</p>
          {!selectedBlock && <p className="text-xs text-[#62666d]">Pilih satu blok untuk edit.</p>}
          {selectedBlock && (
            <div className="space-y-3">
              <div>
                <label className="text-xs text-[#8a8f98]">Teks / Label</label>
                <textarea
                  rows={2}
                  value={selectedBlock.value}
                  onChange={(e) => updateBlock(selectedBlock.id, { value: e.target.value })}
                  className="mt-1 w-full rounded-md border border-white/[0.08] bg-white/[0.02] px-2.5 py-2 text-sm outline-none focus:border-[#7170ff]/60"
                />
              </div>
              {selectedBlock.type === "image" && (
                <div>
                  <label className="text-xs text-[#8a8f98]">URL imej</label>
                  <input
                    value={selectedBlock.src || ""}
                    onChange={(e) => updateBlock(selectedBlock.id, { src: e.target.value })}
                    placeholder="https://…"
                    className="mt-1 w-full rounded-md border border-white/[0.08] bg-white/[0.02] px-2.5 py-2 text-sm outline-none focus:border-[#7170ff]/60"
                  />
                </div>
              )}
              {selectedBlock.type === "product" && (
                <div>
                  <label className="text-xs text-[#8a8f98]">Produk</label>
                  <select
                    value={selectedBlock.productId || ""}
                    onChange={(e) => updateBlock(selectedBlock.id, { productId: e.target.value })}
                    className="mt-1 w-full rounded-md border border-white/[0.08] bg-[#191a1b] px-2.5 py-2 text-sm outline-none"
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
                <label className="flex items-center gap-2 text-sm text-[#d0d6e0]">
                  <input
                    type="checkbox"
                    checked={!!selectedBlock.required}
                    onChange={(e) => updateBlock(selectedBlock.id, { required: e.target.checked })}
                    className="accent-[#5e6ad2]"
                  />
                  Wajib diisi
                </label>
              )}
            </div>
          )}

          <p className="mb-2 mt-8 text-xs font-medium uppercase tracking-wider text-[#62666d]">Borang sedia ada</p>
          <div className="space-y-1.5">
            {forms.map((f) => (
              <button
                key={f.id}
                onClick={() => fetchForm(f.id)}
                className={`w-full rounded-md border px-3 py-2 text-left text-[13px] transition-colors ${
                  currentId === f.id
                    ? "border-[#7170ff]/50 bg-[#5e6ad2]/10 text-[#f7f8f8]"
                    : "border-white/[0.06] bg-white/[0.02] text-[#d0d6e0] hover:bg-white/[0.04]"
                }`}
              >
                <span className="block truncate">{f.title}</span>
                <span className="text-[10px] text-[#62666d]">
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
}

function labelFor(t: BlockType) {
  switch (t) {
    case "name":
      return "Nama penuh";
    case "email":
      return "Emel";
    case "phone":
      return "No. telefon";
    case "textarea":
      return "Nota tambahan";
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
  blocks.push({ id: uid(), type: "name", value: "Nama penuh", required: true });
  blocks.push({ id: uid(), type: "email", value: "Emel", required: true });
  blocks.push({ id: uid(), type: "phone", value: "No. telefon" });
  return blocks;
}

/* renders a non-interactive preview of each block type */
function BlockPreview({ block, products }: { block: Block; products: Product[] }) {
  const p = products.find((x) => x.id === block.productId);
  switch (block.type) {
    case "text":
      return <p className="text-sm text-[#d0d6e0]">{block.value || "Teks…"}</p>;
    case "image":
      return block.src ? (
        // eslint-disable-next-line @next/next/no-img-element
        <img src={block.src} alt="" className="max-h-40 rounded-md border border-white/[0.06] object-cover" />
      ) : (
        <div className="flex h-24 items-center justify-center rounded-md border border-dashed border-white/[0.12] text-xs text-[#62666d]">
          URL imej →
        </div>
      );
    case "product":
      return (
        <div className="flex items-center justify-between rounded-md border border-white/[0.08] bg-[#191a1b] px-3 py-2">
          <span className="text-sm">{block.value || p?.name || "Produk"}</span>
          <span className="text-sm font-medium text-[#a5b4fc]">{p ? money(p.price_sen) : "—"}</span>
        </div>
      );
    case "divider":
      return <hr className="border-white/[0.08]" />;
    case "checkbox":
      return (
        <label className="flex items-center gap-2 text-sm text-[#d0d6e0]">
          <span className="inline-block h-4 w-4 rounded border border-white/20" /> {block.value}
        </label>
      );
    default:
      return (
        <div>
          <p className="mb-1 text-xs text-[#8a8f98]">
            {block.value || "Medan"}
            {block.required && <span className="text-red-400"> *</span>}
          </p>
          <div className="h-8 rounded-md border border-white/[0.08] bg-black/20" />
        </div>
      );
  }
}