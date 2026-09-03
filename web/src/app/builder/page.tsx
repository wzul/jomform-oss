"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useI18n } from "../../components/i18n";
import { spaceQuery } from "../../lib/space";
import TemplatePicker from "../../components/TemplatePicker";

/* ─── types ──────────────────────────────────────────────────────────────── */

/* Issue #173: Gravity-Forms-style field palette. BlockType is the union of
   every field the builder can place. Categories mirror Gravity Forms:
   Standard / Advanced / Pricing. */
type BlockType =
  // Standard
  | "text"
  | "textarea"
  | "number"
  | "select"
  | "radio"
  | "checkbox"
  | "name"
  | "email"
  | "phone"
  | "date"
  | "time"
  | "website"
  | "section"
  | "html"
  // Advanced
  | "address"
  | "file"
  | "consent"
  | "hidden"
  | "list"
  | "repeater"
  | "image-choice"
  | "captcha"
  // Pricing
  | "product"
  | "quantity"
  | "option"
  | "shipping"
  | "total"
  // Legacy / decorative
  | "image"
  | "divider"
  | "coupon";

type Choice = { label: string; value: string; price_sen?: number };

type Block = {
  id: string;
  type: BlockType;
  value: string;
  src?: string;
  productId?: string;
  required?: boolean;
  condition?: { field_id: string; op: string; value: string } | null;
  // Issue #173: per-field settings.
  placeholder?: string;
  description?: string;
  cssClass?: string;
  maxLength?: number;
  defaultValue?: string;
  min?: string;
  max?: string;
  step?: string;
  format?: string;
  pattern?: string;
  requiredMessage?: string;
  priceSen?: number;
  choices?: Choice[];
  // Issue #249: per-product quantity selector (Gravity-Forms-style). When true,
  // the product block renders its own quantity input scoped to that product.
  quantity?: boolean;
};

type Product = { id: string; name: string; price_sen: number; type: string };

type FormRow = { id: string; slug: string; title: string; status: string };

/* Issue #173: categorized palette. Each group is a Gravity-Forms-style
   section. Items are draggable onto the canvas and click-to-add (mobile). */
type PaletteGroup = { key: string; icon: string; items: { type: BlockType; icon: string }[] };

const PALETTE_GROUPS: PaletteGroup[] = [
  {
    key: "standard",
    icon: "▤",
    items: [
      { type: "text", icon: "T" },
      { type: "textarea", icon: "📝" },
      { type: "number", icon: "#" },
      { type: "select", icon: "▾" },
      { type: "radio", icon: "◉" },
      { type: "checkbox", icon: "☑" },
      { type: "name", icon: "🔤" },
      { type: "email", icon: "✉" },
      { type: "phone", icon: "☎" },
      { type: "date", icon: "📅" },
      { type: "time", icon: "🕐" },
      { type: "website", icon: "🔗" },
      { type: "section", icon: "§" },
      { type: "html", icon: "</>" },
      { type: "image", icon: "🖼" },
      { type: "divider", icon: "―" },
    ],
  },
  {
    key: "advanced",
    icon: "⚙",
    items: [
      { type: "address", icon: "⌂" },
      { type: "file", icon: "📎" },
      { type: "consent", icon: "✓" },
      { type: "hidden", icon: "👁" },
      { type: "list", icon: "☰" },
      { type: "repeater", icon: "⇅" },
      { type: "image-choice", icon: "🖼" },
      { type: "captcha", icon: "🛡" },
    ],
  },
  {
    key: "pricing",
    icon: "₨",
    items: [
      { type: "product", icon: "🛒" },
      { type: "quantity", icon: "×" },
      { type: "option", icon: "＋" },
      { type: "shipping", icon: "🚚" },
      { type: "total", icon: "Σ" },
      { type: "coupon", icon: "🏷" },
    ],
  },
];

/* Field types that collect a buyer answer (used for required + conditional
   logic + response-mode collection). */
const fieldTypes: BlockType[] = [
  "name", "email", "phone", "textarea", "checkbox", "coupon",
  "number", "select", "radio", "date", "time", "website",
  "address", "consent", "hidden", "list", "repeater", "image-choice", "captcha",
];

/* Pricing fields that add to the order total (payment-mode only). */
const pricingTypes: BlockType[] = ["product", "quantity", "option", "shipping", "total", "coupon"];

/* Field types that use a choices editor. */
const choiceTypes: BlockType[] = ["select", "radio", "image-choice", "option", "shipping"];

function uid() {
  return Math.random().toString(36).slice(2, 9);
}

const money = (sen: number) =>
  `RM${(sen / 100).toLocaleString("en-MY", { minimumFractionDigits: 2 })}`;

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
  // Issue #176: form status (draft | private | published) + access password.
  const [status, setStatus] = useState<"draft" | "private" | "published">("draft");
  const [accessPw, setAccessPw] = useState("");
  const [blocks, setBlocks] = useState<Block[]>([]);
  const [selected, setSelected] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [pubUrl, setPubUrl] = useState("");
  const [loadErr, setLoadErr] = useState("");
  // Issue #174: template picker on the new-form path (no ?form= param).
  const [pickerOpen, setPickerOpen] = useState(false);

  // Issue #256: AI form builder — prompt input + generation state.
  const [aiPrompt, setAiPrompt] = useState("");
  const [aiGenerating, setAiGenerating] = useState(false);
  const [aiMsg, setAiMsg] = useState("");

  // Issue #59: mobile (<md) shows ONE section at a time via a tab bar
  // (Add | Canvas | Settings); canvas is the default and the palette is the
  // least important thing on a phone. Desktop ≥768px ignores this and keeps
  // the 3-col grid.
  type MobileTab = "add" | "canvas" | "settings";
  const [mobileTab, setMobileTab] = useState<MobileTab>("canvas");

  // Issue #61: image upload progress flag for the inspector file picker.
  const [uploading, setUploading] = useState(false);

  // Issue #186: per-form CHIP client field mapping (CHIP field → block id).
  // Saved inside the form config as `chip_mapping`. Empty = implicit default
  // (name→full_name, email→email, phone→phone).
  const [chipMapping, setChipMapping] = useState<Record<string, string>>({});

  // Issue #171: form collection mode — "payment" (default) or "response".
  const [mode, setMode] = useState<"payment" | "response">("payment");

  // Real drag state: which palette type or block index is being dragged, and
  // which index the insertion indicator currently points at.
  const dragType = useRef<BlockType | null>(null);
  const dragIdx = useRef<number | null>(null);
  const [dropAt, setDropAt] = useState<number | null>(null);

  const load = useCallback(async () => {
    const [p, f] = await Promise.all([
      fetch(`/api/rest/products${spaceQuery()}`).then((r) => (r.ok ? r.json() : [])),
      fetch(`/api/rest/forms${spaceQuery()}`).then((r) => (r.ok ? r.json() : [])),
    ]);
    setProducts(p);
    setForms(f);
  }, []);

  // hydrate from ?form=<id>
  useEffect(() => {
    load();
    const id = new URLSearchParams(window.location.search).get("form");
    if (!id) {
      // Issue #174: new-form path (no ?form=) → show the template picker.
      setPickerOpen(true);
      return;
    }
    if (id) void (async () => {
      const r = await fetch(`/api/rest/forms/${id}${spaceQuery()}`);
      if (!r.ok) return;
      const d = await r.json();
      setCurrentId(d.id);
      setSlug(d.slug);
      setTitle(d.title);
      setStatus(d.status === "private" ? "private" : d.status === "published" ? "published" : "draft");
      setPubUrl(d.status === "published" ? d.url : "");
      const cfg = d.config || {};
      setMode(cfg.mode === "response" ? "response" : "payment");
      setChipMapping(cfg.chip_mapping || {});
      const hydrated: Block[] = [];
      (cfg.blocks || []).forEach(
        (b: Record<string, unknown>) => {
          hydrated.push(hydrateBlock(b));
        },
      );
      setBlocks(hydrated.length ? hydrated : defaultBlocks(cfg.products || []));
    })();
  }, [load]);

  /* Issue #173: map a stored block (snake_case) to the builder Block shape,
     carrying the new per-field settings through. */
  function hydrateBlock(b: Record<string, unknown>): Block {
    const choices = Array.isArray(b.choices)
      ? (b.choices as { label?: string; value?: string; price_sen?: number }[]).map((c) => ({
          label: c.label || "",
          value: c.value || c.label || "",
          price_sen: c.price_sen,
        }))
      : undefined;
    return {
      id: uid(),
      type: (b.type as BlockType) || "text",
      value: (b.value as string) || "",
      src: b.src as string | undefined,
      productId: b.product_id as string | undefined,
      required: b.required as boolean | undefined,
      condition: (b.condition as Block["condition"]) || null,
      placeholder: b.placeholder as string | undefined,
      description: b.description as string | undefined,
      cssClass: b.css_class as string | undefined,
      maxLength: b.max_length as number | undefined,
      defaultValue: b.default_value as string | undefined,
      min: b.min as string | undefined,
      max: b.max as string | undefined,
      step: b.step as string | undefined,
      format: b.format as string | undefined,
      pattern: b.pattern as string | undefined,
      requiredMessage: b.required_message as string | undefined,
      priceSen: b.price_sen as number | undefined,
      quantity: b.quantity as boolean | undefined,
      choices,
    };
  }

  function makeBlock(bt: BlockType): Block {
    const b: Block = { id: uid(), type: bt, value: "" };
    if (bt === "product" && products[0]) b.productId = products[0].id;
    // Issue #249: product blocks get a per-product quantity selector by default.
    if (bt === "product") b.quantity = true;
    if (fieldTypes.includes(bt)) {
      b.required = bt === "name" || bt === "email";
      const k = FIELD_LABEL_KEY[bt];
      b.value = k ? t(k) : "";
    }
    // Issue #173: sensible defaults for the new field types.
    if (bt === "select" || bt === "radio" || bt === "image-choice") {
      b.choices = [
        { label: t("bld.choiceOne"), value: "1" },
        { label: t("bld.choiceTwo"), value: "2" },
      ];
    }
    if (bt === "option" || bt === "shipping") {
      b.choices = [{ label: t("bld.choiceOne"), value: "1", price_sen: 0 }];
      // Pricing blocks submit with product_id = the block's own id; the
      // server resolves the price from the block config (server authority).
      b.productId = b.id;
    }
    if (bt === "quantity") b.defaultValue = "1";
    if (bt === "consent") b.value = t("bld.consentDefault");
    if (bt === "section") b.value = t("bld.sectionDefault");
    if (bt === "total") b.value = t("bld.totalDefault");
    if (bt === "captcha") b.value = t("bld.captchaDefault");
    return b;
  }

  function addBlock(t: BlockType) {
    const b = makeBlock(t);
    setBlocks((prev) => [...prev, b]);
    setSelected(b.id);
    setDirty(true);
    // Issue #59: on mobile, after adding from the Add tab, drop the user onto
    // the canvas so the new block is immediately visible/editable.
    setMobileTab("canvas");
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

  /* Issue #173: duplicate a block (deep copy with a fresh id). */
  function duplicateBlock(id: string) {
    setBlocks((prev) => {
      const idx = prev.findIndex((b) => b.id === id);
      if (idx < 0) return prev;
      const copy: Block = { ...prev[idx], id: uid() };
      if (copy.choices) copy.choices = prev[idx].choices?.map((c) => ({ ...c }));
      const next = [...prev];
      next.splice(idx + 1, 0, copy);
      return next;
    });
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
    const r = await fetch(`/api/rest/forms/${id}${spaceQuery()}`);
    if (!r.ok) return;
    const d = await r.json();
    setCurrentId(d.id);
    setSlug(d.slug);
    setTitle(d.title);
    setStatus(d.status === "private" ? "private" : d.status === "published" ? "published" : "draft");
    setPubUrl(d.status === "published" ? d.url : "");
    const cfg = d.config || {};
    setMode(cfg.mode === "response" ? "response" : "payment");
    setChipMapping(cfg.chip_mapping || {});
    const hydrated: Block[] = [];
    (cfg.blocks || []).forEach(
      (b: Record<string, unknown>) => {
        hydrated.push(hydrateBlock(b));
      },
    );
    setBlocks(hydrated.length ? hydrated : defaultBlocks(cfg.products || []));
  }

  async function save(publish: boolean) {
    // Issue #176: a blank slug is auto-generated by the backend on create, so
    // it is no longer required here. The merchant can still edit it manually.
    setSaving(true);
    setLoadErr("");
    const config = {
      mode,
      description: (blocks.find((b) => b.type === "text")?.value || "").slice(0, 300),
      // Issue #186: merchant-configured CHIP client field mapping. Only
      // non-empty entries are persisted; empty = implicit default.
      chip_mapping: Object.fromEntries(
        Object.entries(chipMapping).filter(([, v]) => v !== ""),
      ),
      blocks: blocks.map((b) => ({
        id: b.id,
        type: b.type,
        value: b.value,
        src: b.src,
        product_id: b.productId,
        required: b.required,
        condition: b.condition || undefined,
        // Issue #173: per-field settings.
        placeholder: b.placeholder || undefined,
        description: b.description || undefined,
        css_class: b.cssClass || undefined,
        max_length: b.maxLength,
        default_value: b.defaultValue || undefined,
        min: b.min || undefined,
        max: b.max || undefined,
        step: b.step || undefined,
        format: b.format || undefined,
        pattern: b.pattern || undefined,
        required_message: b.requiredMessage || undefined,
        price_sen: b.priceSen,
        quantity: b.quantity === undefined ? undefined : b.quantity,
        choices: b.choices?.map((c) => ({ label: c.label, value: c.value, price_sen: c.price_sen })),
      })),
      products: products.map((p) => ({ id: p.id, name: p.name, price_sen: p.price_sen })),
    };
    const payload = { slug: slug.toLowerCase().replace(/[^a-z0-9-]/g, "-"), title, config };

    let id = currentId;
    if (!id) {
      const r = await fetch(`/api/rest/forms${spaceQuery()}`, {
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
      setSlug(d.slug);
      id = d.id;
    } else {
      await fetch(`/api/rest/forms/${id}${spaceQuery()}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ title, config }),
      });
    }
    // Issue #176: apply the selected status (draft | private | published).
    // The Publish button forces published; private requires the access
    // password; published/draft clears any stored password.
    const targetStatus = publish ? "published" : status;
    if (id) {
      const statusBody: { status: string; password?: string } = { status: targetStatus };
      if (targetStatus === "private") statusBody.password = accessPw;
      const sr = await fetch(`/api/rest/forms/${id}/status${spaceQuery()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(statusBody),
      });
      if (!sr.ok) {
        const d = await sr.json().catch(() => ({}));
        setLoadErr(d.error || "Failed to set status");
        setSaving(false);
        return;
      }
      setStatus(targetStatus as "draft" | "private" | "published");
    }
    if (publish && id) {
      await fetch(`/api/rest/forms/${id}/publish${spaceQuery()}`, { method: "POST" });
      setPubUrl(`https://${localStorage.getItem("jf_space") || "app"}.jomform.com/${payload.slug}`);
      await load();
    }
    setSaving(false);
    setDirty(false);
  }

  /* Issue #256: AI form builder — send the prompt to the backend, which calls
     deepseek-v4-flash:0731 to draft a validated form config, then load the
     generated blocks into the canvas for the user to review/edit before
     saving. The backend validates the output (no bad fields, no price
     tampering) and rate-limits the endpoint. */
  async function aiGenerate() {
    const prompt = aiPrompt.trim();
    if (!prompt || aiGenerating) return;
    setAiGenerating(true);
    setAiMsg("");
    setLoadErr("");
    try {
      const r = await fetch(`/api/rest/forms/ai-generate${spaceQuery()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ prompt }),
      });
      const d = await r.json().catch(() => ({}));
      if (!r.ok) {
        setAiMsg(r.status === 429 ? t("bld.aiRateLimited") : t("bld.aiError", { error: d.error || r.status }));
        return;
      }
      // Apply the generated config: title + blocks (fresh ids so they are
      // editable in the canvas). Product blocks keep their product_id; the
      // server resolves prices from the product DB.
      if (d.title) setTitle(d.title);
      const generated: Block[] = (d.blocks || []).map((b: Record<string, unknown>) => ({
        id: uid(),
        type: (b.type as BlockType) || "text",
        value: (b.value as string) || "",
        src: b.src as string | undefined,
        productId: b.product_id as string | undefined,
        required: b.required as boolean | undefined,
        placeholder: b.placeholder as string | undefined,
        description: b.description as string | undefined,
        cssClass: b.css_class as string | undefined,
        maxLength: b.max_length as number | undefined,
        defaultValue: b.default_value as string | undefined,
        min: b.min as string | undefined,
        max: b.max as string | undefined,
        step: b.step as string | undefined,
        format: b.format as string | undefined,
        pattern: b.pattern as string | undefined,
        requiredMessage: b.required_message as string | undefined,
        priceSen: b.price_sen as number | undefined,
        quantity: b.quantity as boolean | undefined,
        choices: Array.isArray(b.choices)
          ? (b.choices as { label?: string; value?: string; price_sen?: number }[]).map((c) => ({
              label: c.label || "",
              value: c.value || c.label || "",
              price_sen: c.price_sen,
            }))
          : undefined,
      }));
      if (generated.length) {
        setBlocks(generated);
        setSelected(null);
        setDirty(true);
        setAiMsg(t("bld.aiApplied"));
      } else {
        setAiMsg(t("bld.aiError", { error: "no blocks" }));
      }
    } catch {
      setAiMsg(t("bld.aiError", { error: "network" }));
    } finally {
      setAiGenerating(false);
    }
  }

  const selectedBlock = blocks.find((b) => b.id === selected);
  const dragging = dragType.current !== null || dragIdx.current !== null;

  // Issue #61: upload an image for the selected block via the direct S3
  // upload endpoint (presign superseded by the owner's server-side-optimise
  // flow). Returns the public URL, stores it into the block's src.
  async function handleImageUpload(file: File) {
    if (!selectedBlock || selectedBlock.type !== "image") return;
    if (!["image/png", "image/jpeg", "image/webp", "image/gif"].includes(file.type)) {
      setLoadErr("Jenis imej tidak disokong (png/jpeg/webp/gif sahaja).");
      return;
    }
    if (file.size > 20 * 1024 * 1024) {
      setLoadErr("Imej melebihi had 20MB.");
      return;
    }
    setUploading(true);
    setLoadErr("");
    try {
      const fd = new FormData();
      fd.append("file", file);
      const r = await fetch("/api/rest/uploads/image", { method: "POST", body: fd });
      const d = await r.json().catch(() => ({}));
      if (!r.ok || !d.url) {
        setLoadErr(d.error || t("bld.uploadImageFail"));
        return;
      }
      updateBlock(selectedBlock.id, { src: d.url });
    } finally {
      setUploading(false);
    }
  }

  /* Issue #173: per-field settings panel. Renders the field-type-specific
     controls for the selected block. */
  function renderFieldSettings(b: Block) {
    const upd = (patch: Partial<Block>) => updateBlock(b.id, patch);
    const input = (key: keyof Block, label: string, ph?: string) => (
      <div>
        <label className="text-xs text-[var(--jf-muted)]">{label}</label>
        <input
          value={(b[key] as string) || ""}
          onChange={(e) => upd({ [key]: e.target.value } as Partial<Block>)}
          placeholder={ph}
          className="mt-1 w-full rounded-md border border-[var(--jf-border)] bg-white px-2.5 py-2 text-sm outline-none focus:border-[#7170ff]/60"
        />
      </div>
    );

    return (
      <div className="space-y-3">
        {/* Common: label / value */}
        <div>
          <label className="text-xs text-[var(--jf-muted)]">{t("bld.textLabel")}</label>
          <textarea
            rows={2}
            value={b.value}
            onChange={(e) => upd({ value: e.target.value })}
            className="mt-1 w-full rounded-md border border-[var(--jf-border)] bg-white px-2.5 py-2 text-sm outline-none focus:border-[#7170ff]/60"
          />
        </div>

        {/* Common: description */}
        {input("description", t("bld.description"), t("bld.descriptionPh"))}

        {/* Common: placeholder (input fields) */}
        {["text", "textarea", "number", "email", "phone", "date", "time", "website", "address", "captcha", "list", "repeater"].includes(b.type) &&
          input("placeholder", t("bld.placeholder"), t("bld.placeholderPh"))}

        {/* Common: CSS class */}
        {input("cssClass", t("bld.cssClass"), t("bld.cssClassPh"))}

        {/* Text/textarea: max length + default value */}
        {(b.type === "text" || b.type === "textarea") && (
          <>
            <div>
              <label className="text-xs text-[var(--jf-muted)]">{t("bld.maxLength")}</label>
              <input
                type="number"
                min={0}
                value={b.maxLength ?? ""}
                onChange={(e) => upd({ maxLength: e.target.value === "" ? undefined : Number(e.target.value) })}
                className="mt-1 w-full rounded-md border border-[var(--jf-border)] bg-white px-2.5 py-2 text-sm outline-none focus:border-[#7170ff]/60"
              />
            </div>
            {input("defaultValue", t("bld.defaultValue"), t("bld.defaultValuePh"))}
          </>
        )}

        {/* Number: min/max/step */}
        {b.type === "number" && (
          <>
            {input("min", t("bld.min"))}
            {input("max", t("bld.max"))}
            {input("step", t("bld.step"))}
          </>
        )}

        {/* Name: format simple/full */}
        {b.type === "name" && (
          <div>
            <label className="text-xs text-[var(--jf-muted)]">{t("bld.nameFormat")}</label>
            <select
              value={b.format || "full"}
              onChange={(e) => upd({ format: e.target.value })}
              className="mt-1 w-full rounded-md border border-[var(--jf-border)] bg-white px-2.5 py-2 text-sm outline-none"
            >
              <option value="full">{t("bld.nameFull")}</option>
              <option value="simple">{t("bld.nameSimple")}</option>
            </select>
          </div>
        )}

        {/* Date/time: format */}
        {(b.type === "date" || b.type === "time") && (
          <div>
            <label className="text-xs text-[var(--jf-muted)]">{t("bld.dateFormat")}</label>
            <select
              value={b.format || (b.type === "date" ? "date" : "time")}
              onChange={(e) => upd({ format: e.target.value })}
              className="mt-1 w-full rounded-md border border-[var(--jf-border)] bg-white px-2.5 py-2 text-sm outline-none"
            >
              <option value="date">{t("bld.fmtDate")}</option>
              <option value="datetime">{t("bld.fmtDateTime")}</option>
              <option value="time">{t("bld.fmtTime")}</option>
            </select>
          </div>
        )}

        {/* Choices editor: select/radio/image-choice/option/shipping */}
        {choiceTypes.includes(b.type) && (
          <div>
            <label className="text-xs text-[var(--jf-muted)]">{t("bld.choices")}</label>
            <div className="mt-1 space-y-1.5">
              {(b.choices || []).map((c, ci) => (
                <div key={ci} className="flex items-center gap-1.5">
                  <input
                    value={c.label}
                    onChange={(e) => {
                      const ch = [...(b.choices || [])];
                      ch[ci] = { ...ch[ci], label: e.target.value, value: e.target.value || ch[ci].value };
                      upd({ choices: ch });
                    }}
                    placeholder={t("bld.choiceLabel")}
                    className="min-w-0 flex-1 rounded-md border border-[var(--jf-border)] bg-white px-2 py-1.5 text-xs outline-none focus:border-[#7170ff]/60"
                  />
                  {(b.type === "option" || b.type === "shipping") && (
                    <input
                      type="number"
                      min={0}
                      step="0.01"
                      value={c.price_sen ? (c.price_sen / 100).toFixed(2) : ""}
                      onChange={(e) => {
                        const ch = [...(b.choices || [])];
                        const rm = Number(e.target.value);
                        ch[ci] = { ...ch[ci], price_sen: Number.isFinite(rm) && rm > 0 ? Math.round(rm * 100) : 0 };
                        upd({ choices: ch });
                      }}
                      placeholder="RM"
                      title={t("bld.choicePrice")}
                      className="w-20 rounded-md border border-[var(--jf-border)] bg-white px-2 py-1.5 text-xs outline-none focus:border-[#7170ff]/60"
                    />
                  )}
                  <button
                    onClick={() => upd({ choices: (b.choices || []).filter((_, i) => i !== ci) })}
                    title={t("bld.removeChoice")}
                    className="rounded-md px-1.5 text-xs text-[#9ca3af] hover:text-red-400"
                  >
                    ✕
                  </button>
                </div>
              ))}
              <button
                onClick={() => upd({ choices: [...(b.choices || []), { label: "", value: String((b.choices || []).length + 1) }] })}
                className="w-full rounded-md border border-dashed border-[var(--jf-border)] py-1.5 text-xs text-[var(--jf-muted)] hover:border-[#7170ff]/40 hover:text-[var(--jf-primary)]"
              >
                + {t("bld.addChoice")}
              </button>
            </div>
          </div>
        )}

        {/* Product: pick a product */}
        {b.type === "product" && (
          <div>
            <label className="text-xs text-[var(--jf-muted)]">{t("bld.product")}</label>
            <select
              value={b.productId || ""}
              onChange={(e) => upd({ productId: e.target.value })}
              className="mt-1 w-full rounded-md border border-[var(--jf-border)] bg-white px-2.5 py-2 text-sm outline-none"
            >
              {products.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name} — {money(p.price_sen)}
                </option>
              ))}
            </select>
            {/* Issue #249: per-product quantity selector (Gravity-Forms-style). */}
            <label className="mt-3 flex items-center gap-2 text-xs text-[var(--jf-muted)]">
              <input
                type="checkbox"
                checked={b.quantity !== false}
                onChange={(e) => upd({ quantity: e.target.checked })}
                className="h-4 w-4 accent-[#7170ff]"
              />
              {t("bld.perProductQty")}
            </label>
          </div>
        )}

        {/* Quantity: default value */}
        {b.type === "quantity" && input("defaultValue", t("bld.defaultQty"), "1")}

        {/* Image: URL + upload */}
        {b.type === "image" && (
          <div>
            <label className="text-xs text-[var(--jf-muted)]">{t("bld.imageUrl")}</label>
            <input
              value={b.src || ""}
              onChange={(e) => upd({ src: e.target.value })}
              placeholder="https://…"
              className="mt-1 w-full rounded-md border border-[var(--jf-border)] bg-white px-2.5 py-2 text-sm outline-none focus:border-[#7170ff]/60"
            />
            <label className="mt-2 inline-flex w-full cursor-pointer items-center justify-center rounded-md border border-[var(--jf-border)] py-2.5 text-xs font-medium text-[#374151] hover:bg-slate-50 disabled:opacity-50">
              <input
                type="file"
                accept="image/png,image/jpeg,image/webp,image/gif"
                disabled={uploading}
                onChange={(e) => {
                  const f = e.target.files?.[0];
                  if (f) void handleImageUpload(f);
                  e.target.value = "";
                }}
                className="hidden"
              />
              {uploading ? t("bld.uploading") : t("bld.uploadImage")}
            </label>
          </div>
        )}

        {/* Required */}
        {fieldTypes.includes(b.type) && (
          <label className="flex items-center gap-2 text-sm text-[#374151]">
            <input
              type="checkbox"
              checked={!!b.required}
              onChange={(e) => upd({ required: e.target.checked })}
              className="accent-[#5e6ad2]"
            />
            {t("bld.required")}
          </label>
        )}

        {/* Validation: required message + pattern */}
        {fieldTypes.includes(b.type) && b.required && (
          <>
            {input("requiredMessage", t("bld.requiredMsg"), t("bld.requiredMsgPh"))}
            {["text", "textarea", "number", "email", "phone", "website"].includes(b.type) &&
              input("pattern", t("bld.pattern"), t("bld.patternPh"))}
          </>
        )}

        {/* conditional logic */}
        {fieldTypes.includes(b.type) && blocks.length > 1 && (
          <div className="rounded-md border border-slate-200 p-2">
            <p className="text-[11px] font-medium text-slate-500">
              {t("bld.condLogic")}
            </p>
            <select
              value={b.condition?.field_id || ""}
              onChange={(e) =>
                upd({
                  condition: e.target.value
                    ? { ...(b.condition || { op: "equals", value: "" }), field_id: e.target.value }
                    : null,
                })
              }
              className="mt-1 w-full rounded border border-slate-200 px-2 py-1 text-xs"
            >
              <option value="">{t("bld.alwaysShow")}</option>
              {blocks
                .filter((x) => x.id !== b.id && fieldTypes.includes(x.type))
                .map((x) => (
                  <option key={x.id} value={x.id}>
                    {x.value || x.type} (#{blocks.indexOf(x) + 1})
                  </option>
                ))}
            </select>
            {b.condition && (
              <div className="mt-1 flex gap-1">
                <select
                  value={b.condition.op}
                  onChange={(e) =>
                    upd({ condition: { ...b.condition!, op: e.target.value } })
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
                  value={b.condition.value}
                  onChange={(e) =>
                    upd({ condition: { ...b.condition!, value: e.target.value } })
                  }
                  placeholder="value"
                  className="min-w-0 flex-1 rounded border border-slate-200 px-2 py-1 text-xs"
                />
              </div>
            )}
          </div>
        )}
      </div>
    );
  }

  return (
    <main className="min-h-screen bg-[var(--jf-bg)] text-[var(--jf-text)]">
      {/* Issue #174: template picker on the new-form path (no ?form= param). */}
      <TemplatePicker open={pickerOpen} onClose={() => setPickerOpen(false)} />
      {/* top bar — issue #59: stacks on mobile. Row1: back + title (flex-1,
          min-w-0, truncate) + Publish. Row2: slug + View + unsaved. Desktop
          (md+) keeps the single flat row; inputs shrink (w-56/w-40 →
          flex-1 min-w-0 on mobile). */}
      <header className="sticky top-0 z-20 flex flex-col gap-1.5 border-b border-[var(--jf-border)] bg-white/95 px-3 py-2 backdrop-blur md:flex-row md:items-center md:gap-3 md:px-4 md:py-2.5">
        <div className="flex min-w-0 items-center gap-1.5">
          <a href="/forms" className="shrink-0 rounded-md px-2 py-1 text-sm text-[var(--jf-muted)] hover:text-[#374151]">
            ←
          </a>
          <input
            value={title}
            onChange={(e) => {
              setTitle(e.target.value);
              setDirty(true);
            }}
            placeholder={t("bld.formTitle")}
            className="min-w-0 flex-1 rounded-md border border-transparent bg-transparent px-2 py-1 text-sm font-medium outline-none hover:border-[var(--jf-border)] focus:border-[#7170ff]/60 md:w-56 md:flex-none"
          />
          {/* Publish is row 1 on mobile; row 2 on desktop via ml-auto below.
              On mobile the actions block moves to row 2 (hidden here). */}
          <button
            onClick={() => save(true)}
            disabled={saving}
            className="shrink-0 md:hidden jf-btn-primary"
          >
            {saving ? t("builder.saving") : currentId ? t("builder.publish") : t("builder.savePub")}
          </button>
        </div>
        <div className="flex min-w-0 items-center gap-2 border-t border-[var(--jf-border)] pt-1.5 md:ml-auto md:border-0 md:pt-0">
          <input
            value={slug}
            onChange={(e) => {
              setSlug(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, "-"));
              setDirty(true);
            }}
            placeholder="slug-url"
            className="min-w-0 flex-1 rounded-md border border-transparent bg-transparent px-2 py-1 font-mono text-xs text-[var(--jf-muted)] outline-none hover:border-[var(--jf-border)] focus:border-[#7170ff]/60 md:w-40 md:flex-none"
          />
          <span className="shrink-0">
            {dirty && <span className="text-xs text-[#f59e0b]">• {t("builder.unsaved")}</span>}
          </span>
          {pubUrl && (
            <a
              href={pubUrl}
              target="_blank"
              className="shrink-0 rounded-md border border-[var(--jf-border)] bg-white px-3 py-1.5 text-xs text-[#374151] hover:bg-slate-50"
            >
              {t("bld.view")}
            </a>
          )}
          {/* desktop Publish — hidden on mobile (it's in row 1). */}
          <button
            onClick={() => save(true)}
            disabled={saving}
            className="hidden md:inline-flex jf-btn-primary"
          >
            {saving ? t("builder.saving") : currentId ? t("builder.publish") : t("builder.savePub")}
          </button>
        </div>
      </header>

      {/* mobile (<md) section switcher — issue #59: one tab at a time,
          canvas is default. Desktop keeps the 3-col grid (md:grid). */}
      <nav className="flex border-b border-[var(--jf-border)] bg-white md:hidden">
        {(["add", "canvas", "settings"] as MobileTab[]).map((tab) => (
          <button
            key={tab}
            onClick={() => setMobileTab(tab)}
            className={`flex-1 py-2.5 text-sm font-medium capitalize ${
              mobileTab === tab
                ? "border-b-2 border-[#7170ff] text-[var(--jf-primary)]"
                : "text-[var(--jf-muted)] hover:text-[#374151]"
            }`}
          >
            {tab === "add" ? t("bld.addBlock") : tab === "canvas" ? t("bld.canvas") : t("bld.settings")}
          </button>
        ))}
      </nav>

      <div className="grid w-full grid-cols-1 md:grid-cols-[240px_1fr_260px]">
        {/* ── left: categorized palette (drag sources) − mobile: only on the
             Add tab, desktop: always visible. Tap = add (already exists). */}
        <aside className={`border-r border-[var(--jf-border)] p-4 ${mobileTab === "add" ? "block" : "hidden"} md:block`}>
          {/* Issue #256: AI form builder — prompt input. The backend calls
              deepseek-v4-flash:0731 to draft a validated form config. */}
          <div className="mb-4 rounded-lg border border-[var(--jf-border)] bg-white p-3">
            <p className="mb-1 flex items-center gap-1 text-[11px] font-semibold uppercase tracking-wider text-[#9ca3af]">
              <span>✨</span>
              {t("bld.aiTitle")}
            </p>
            <p className="mb-2 text-[11px] leading-snug text-[#9ca3af]">{t("bld.aiHint")}</p>
            <textarea
              value={aiPrompt}
              onChange={(e) => setAiPrompt(e.target.value)}
              placeholder={t("bld.aiPromptPh")}
              rows={3}
              className="w-full rounded-md border border-[var(--jf-border)] bg-white px-2.5 py-2 text-xs outline-none focus:border-[#7170ff]/60"
            />
            <button
              onClick={() => void aiGenerate()}
              disabled={aiGenerating || !aiPrompt.trim()}
              className="mt-2 w-full rounded-md bg-[#5e6ad2] py-2 text-xs font-medium text-white hover:bg-[#4f46e5] disabled:opacity-50"
            >
              {aiGenerating ? t("bld.aiGenerating") : t("bld.aiGenerate")}
            </button>
            {aiMsg && <p className="mt-2 text-[11px] leading-snug text-[#374151]">{aiMsg}</p>}
          </div>
          <p className="mb-3 text-xs font-medium uppercase tracking-wider text-[#9ca3af]">
            {t("bld.dragToAdd")}
          </p>
          {PALETTE_GROUPS.map((group) => {
            const items = group.items.filter((p) => !(mode === "response" && pricingTypes.includes(p.type)));
            if (items.length === 0) return null;
            return (
              <div key={group.key} className="mb-4">
                <p className="mb-1.5 flex items-center gap-1 text-[11px] font-semibold uppercase tracking-wider text-[#9ca3af]">
                  <span>{group.icon}</span>
                  {t(`bld.palette.${group.key}` as import("../../components/i18n").Key)}
                </p>
                <div className="grid grid-cols-2 gap-2">
                  {items.map((p) => (
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
              </div>
            );
          })}
        </aside>

        {/* ── center: canvas (drop zone) − mobile: only on the Canvas tab;
             desktop always visible. max-w-lg on desktop, max-w-full mobile. */}
        <section className={`p-4 md:p-6 ${mobileTab === "canvas" ? "block" : "hidden"} md:block`}>
          <div className="mx-auto max-w-full md:max-w-lg">
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
                        title={t("bld.moveUp")}
                        className="flex h-11 w-11 items-center justify-center rounded-md text-base text-[#9ca3af] hover:text-[#374151] active:bg-slate-100 md:h-auto md:w-auto md:px-1 md:text-[10px]"
                      >
                        ↑
                      </button>
                      <button
                        onClick={(e) => {
                          e.stopPropagation();
                          move(i, i + 1);
                        }}
                        title={t("bld.moveDown")}
                        className="flex h-11 w-11 items-center justify-center rounded-md text-base text-[#9ca3af] hover:text-[#374151] active:bg-slate-100 md:h-auto md:w-auto md:px-1 md:text-[10px]"
                      >
                        ↓
                      </button>
                      {/* Issue #173: duplicate */}
                      <button
                        onClick={(e) => {
                          e.stopPropagation();
                          duplicateBlock(b.id);
                        }}
                        title={t("bld.duplicate")}
                        className="flex h-11 w-11 items-center justify-center rounded-md text-base text-[#9ca3af] hover:text-[#374151] active:bg-slate-100 md:h-auto md:w-auto md:px-1 md:text-[10px]"
                      >
                        ⧉
                      </button>
                      <button
                        onClick={(e) => {
                          e.stopPropagation();
                          removeBlock(b.id);
                        }}
                        title={t("bld.removeBlock")}
                        className="flex h-11 w-11 items-center justify-center rounded-md text-base text-[#9ca3af] hover:text-red-400 active:bg-slate-100 md:h-auto md:w-auto md:px-1 md:text-[10px]"
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

        {/* ── right: inspector − mobile: only on the Settings tab; hosts the
             existing right-aside unchanged. */}
        <aside className={`border-l border-[var(--jf-border)] p-4 ${mobileTab === "settings" ? "block" : "hidden"} md:block`}>
          <p className="mb-3 text-xs font-medium uppercase tracking-wider text-[#9ca3af]">{t("bld.blockSettings")}</p>
          {!selectedBlock && <p className="text-xs text-[#9ca3af]">{t("bld.selectBlock")}</p>}
          {selectedBlock && renderFieldSettings(selectedBlock)}


        {/* ── Form mode (issue #171) ── */}
        <div className="mt-8 rounded-lg border border-[var(--jf-border)] bg-white p-3">
          <p className="text-xs font-medium uppercase tracking-wider text-[#9ca3af]">
            {t("bld.mode")}
          </p>
          <div className="mt-2 grid grid-cols-2 gap-1.5">
            <button
              type="button"
              onClick={() => {
                setMode("payment");
                setDirty(true);
              }}
              className={`rounded-md border px-2 py-2 text-xs font-medium transition-colors ${
                mode === "payment"
                  ? "border-[#7170ff]/60 bg-indigo-50 text-[var(--jf-primary)]"
                  : "border-[var(--jf-border)] text-[#374151] hover:bg-slate-50"
              }`}
            >
              {t("bld.modePayment")}
            </button>
            <button
              type="button"
              onClick={() => {
                setMode("response");
                setDirty(true);
              }}
              className={`rounded-md border px-2 py-2 text-xs font-medium transition-colors ${
                mode === "response"
                  ? "border-[#7170ff]/60 bg-indigo-50 text-[var(--jf-primary)]"
                  : "border-[var(--jf-border)] text-[#374151] hover:bg-slate-50"
              }`}
            >
              {t("bld.modeResponse")}
            </button>
          </div>
          <p className="mt-1.5 text-[11px] text-[var(--jf-muted)]">
            {mode === "response" ? t("bld.modeResponseHint") : t("bld.modePaymentHint")}
          </p>
        </div>

        {/* Issue #177: everything else about the form (status, display,
            restrictions, notifications, CHIP feed, sales) lives in the
            dedicated Form Settings view — the builder is fields-only. */}
        {currentId && (
          <div className="mt-8 rounded-lg border border-[var(--jf-border)] bg-white p-3">
            <p className="text-xs font-medium uppercase tracking-wider text-[#9ca3af]">
              {t("fs.title")}
            </p>
            <p className="mt-1 text-[11px] text-[var(--jf-muted)]">
              {t("fs.subtitle")}
            </p>
            <a
              href={`/form-settings?form=${currentId}`}
              className="mt-2 block w-full rounded-md bg-[var(--jf-primary)] py-1.5 text-center text-xs font-medium text-white"
            >
              {t("fs.title")} →
            </a>
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
                  {(localStorage.getItem("jf_space") || "app")}.jomform.com/{f.slug}
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
    case "html":
      return <p className="text-sm text-[#374151]">{block.value || "</>"}</p>;
    case "section":
      return <p className="text-base font-semibold text-[#17181c]">{block.value || "Section"}</p>;
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
        <div className="rounded-md border border-[var(--jf-border)] bg-slate-50 px-3 py-2">
          <div className="flex items-center justify-between">
            <span className="text-sm">{block.value || p?.name || "Product"}</span>
            <span className="text-sm font-medium text-[var(--jf-primary)]">{p ? money(p.price_sen) : "—"}</span>
          </div>
          {/* Issue #249: per-product quantity selector preview. */}
          {block.quantity !== false && (
            <div className="mt-2 flex items-center gap-2 text-xs text-[var(--jf-muted)]">
              <span>Qty</span>
              <span className="rounded-md border border-[var(--jf-border)] bg-white px-2 py-0.5">1</span>
            </div>
          )}
        </div>
      );
    case "quantity":
      return (
        <div className="flex items-center gap-2 text-sm text-[#374151]">
          <span>{block.value || "Quantity"}</span>
          <span className="rounded-md border border-[var(--jf-border)] bg-slate-50 px-2 py-0.5 text-xs">
            {block.defaultValue || "1"}
          </span>
        </div>
      );
    case "option":
    case "shipping":
      return (
        <div className="rounded-md border border-[var(--jf-border)] bg-slate-50 px-3 py-2">
          <p className="text-sm text-[#374151]">{block.value || (block.type === "option" ? "Option" : "Shipping")}</p>
          <div className="mt-1 space-y-0.5">
            {(block.choices || []).map((c, i) => (
              <p key={i} className="text-xs text-[var(--jf-muted)]">
                {c.label || "—"} {c.price_sen ? `+ ${money(c.price_sen)}` : ""}
              </p>
            ))}
          </div>
        </div>
      );
    case "total":
      return (
        <div className="flex items-center justify-between rounded-md border border-[var(--jf-border)] bg-slate-50 px-3 py-2">
          <span className="text-sm">{block.value || "Total"}</span>
          <span className="text-sm font-medium text-[var(--jf-primary)]">RM0.00</span>
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
    case "consent":
      return (
        <label className="flex items-center gap-2 text-sm text-[#374151]">
          <span className="inline-block h-4 w-4 rounded border border-slate-300" /> {block.value || "Consent"}
        </label>
      );
    case "select":
      return (
        <div>
          <p className="mb-1 text-xs text-[var(--jf-muted)]">
            {block.value || "Field"}
            {block.required && <span className="text-red-400"> *</span>}
          </p>
          <div className="flex h-8 items-center rounded-md border border-[var(--jf-border)] bg-slate-50 px-2 text-xs text-[#9ca3af]">
            {(block.choices || []).map((c) => c.label).filter(Boolean).join(", ") || "Select…"}
          </div>
        </div>
      );
    case "radio":
    case "image-choice":
      return (
        <div>
          <p className="mb-1 text-xs text-[var(--jf-muted)]">
            {block.value || "Field"}
            {block.required && <span className="text-red-400"> *</span>}
          </p>
          <div className="space-y-1">
            {(block.choices || []).map((c, i) => (
              <label key={i} className="flex items-center gap-2 text-sm text-[#374151]">
                <span className="inline-block h-4 w-4 rounded-full border border-slate-300" /> {c.label}
              </label>
            ))}
          </div>
        </div>
      );
    case "hidden":
      return (
        <p className="text-xs text-[#9ca3af]">
          Hidden · {block.value || "field"} = {block.defaultValue || "…"}
        </p>
      );
    case "captcha":
      return (
        <div>
          <p className="mb-1 text-xs text-[var(--jf-muted)]">{block.value || "Captcha"}</p>
          <div className="flex h-8 items-center rounded-md border border-[var(--jf-border)] bg-slate-50 px-2 text-xs text-[#9ca3af]">
            🛡 Captcha
          </div>
        </div>
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
