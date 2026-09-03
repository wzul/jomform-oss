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

  // Issue #59: mobile (<md) shows ONE section at a time via a tab bar
  // (Add | Canvas | Settings); canvas is the default and the palette is the
  // least important thing on a phone. Desktop ≥768px ignores this and keeps
  // the 3-col grid.
  type MobileTab = "add" | "canvas" | "settings";
  const [mobileTab, setMobileTab] = useState<MobileTab>("canvas");

  // Issue #61: image upload progress flag for the inspector file picker.
  const [uploading, setUploading] = useState(false);

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

  // per-form stats (submissions / paid / revenue) for the editor sidebar
  const [formStat, setFormStat] = useState<{
    submissions: number; paid_count: number; confirmed_count: number; revenue_sen: number;
  } | null>(null);

  const loadStats = useCallback(async (fid: string | null) => {
    if (!fid) {
      setFormStat(null);
      return;
    }
    const all = await fetch(`/api/rest/analytics/forms${spaceQuery()}`).then((r) => (r.ok ? r.json() : []));
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
      const sp = spaceQuery();
      const sep = sp ? "&" : "";
      const r = await fetch(`/api/rest/sales?${q.toString()}${sep}${sp.replace("?", "")}`);
      if (!r.ok) {
        setFormSales([]);
        setFormSalesTotal(0);
        setFormSalesErr(t("bld.loadSalesErr"));
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
      setFormSalesErr(t("bld.loadSalesErr"));
    }
  }, []);

  // Reload sales whenever the selected form changes; reset to page 1.
  useEffect(() => {
    setFormSalesPage(1);
    void loadFormSales(currentId, 1);
  }, [currentId, loadFormSales]);

  const formSalesTotalPages = Math.max(1, Math.ceil(formSalesTotal / FORM_SALES_PAGE_SIZE));

  // Issue #171: response-only submissions for the selected form.
  type SubmissionRow = { id: string; form_id: string; data: Record<string, unknown>; created_at: string };
  type SubmissionsPage = { items: SubmissionRow[]; page: number; limit: number; total: number };
  const [submissions, setSubmissions] = useState<SubmissionRow[]>([]);
  const [submissionsPage, setSubmissionsPage] = useState(1);
  const [submissionsTotal, setSubmissionsTotal] = useState(0);
  const [submissionsErr, setSubmissionsErr] = useState("");
  const SUBMISSIONS_PAGE_SIZE = 10;

  const loadSubmissions = useCallback(async (fid: string | null, page: number) => {
    if (!fid) {
      setSubmissions([]);
      setSubmissionsTotal(0);
      setSubmissionsErr("");
      return;
    }
    try {
      const q = new URLSearchParams({ page: String(page), limit: String(SUBMISSIONS_PAGE_SIZE) });
      const sp = spaceQuery();
      const sep = sp ? "&" : "";
      const r = await fetch(`/api/rest/forms/${fid}/submissions?${q.toString()}${sep}${sp.replace("?", "")}`);
      if (!r.ok) {
        setSubmissions([]);
        setSubmissionsTotal(0);
        setSubmissionsErr(t("bld.loadResponsesErr"));
        return;
      }
      const d = (await r.json()) as SubmissionsPage;
      setSubmissions(Array.isArray(d.items) ? d.items : []);
      setSubmissionsPage(d.page || page);
      setSubmissionsTotal(d.total || 0);
      setSubmissionsErr("");
    } catch {
      setSubmissions([]);
      setSubmissionsTotal(0);
      setSubmissionsErr(t("bld.loadResponsesErr"));
    }
  }, [t]);

  useEffect(() => {
    setSubmissionsPage(1);
    void loadSubmissions(currentId, 1);
  }, [currentId, loadSubmissions]);

  const submissionsTotalPages = Math.max(1, Math.ceil(submissionsTotal / SUBMISSIONS_PAGE_SIZE));

  function gotoSubmissionsPage(p: number) {
    const next = Math.min(Math.max(1, p), submissionsTotalPages);
    setSubmissionsPage(next);
    void loadSubmissions(currentId, next);
  }

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
      const r = await fetch(`/api/rest/sales/${encodeURIComponent(ref)}/capture${spaceQuery()}`, {
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
      const r = await fetch(`/api/rest/sales/${encodeURIComponent(ref)}/release${spaceQuery()}`, {
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

  // Bulk capture (issue #38, #12 addendum): capture every awaiting
  // authorization at once; per-ref results summarized in the status line.
  async function captureAllPending() {
    const awaiting = formSales.filter((s) => isCapturePending(s));
    if (awaiting.length === 0) return;
    const totalSen = awaiting.reduce((sum, s) => sum + s.total_sen, 0);
    const rm = (totalSen / 100).toFixed(2);
    if (!window.confirm(`Capture ALL ${awaiting.length} pending authorization(s) — total RM${rm}?`)) return;
    setSaleActionBusy("__all__");
    setSaleActionMsg("");
    try {
      const r = await fetch(`/api/rest/sales/bulk-capture${spaceQuery()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ refs: awaiting.map((s) => s.ref_code) }),
      });
      const d = await r.json().catch(() => ({}));
      if (!r.ok) {
        setSaleActionMsg(d.error || t("bld.bulkCaptureFail"));
        return;
      }
      setSaleActionMsg(t("bld.bulkCaptureDone", { captured: String(d.captured), requested: String(d.requested), failed: String(d.failed) }) + (d.failed ? t("bld.bulkCaptureCheck") : " ✓"));
      await loadFormSales(currentId, formSalesPage);
    } finally {
      setSaleActionBusy(null);
    }
  }

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
      if (d.theme?.primary) setFsTheme(d.theme.primary);
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
      choices,
    };
  }

  function makeBlock(bt: BlockType): Block {
    const b: Block = { id: uid(), type: bt, value: "" };
    if (bt === "product" && products[0]) b.productId = products[0].id;
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
      theme: { primary: fsTheme },
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

        {/* ── Form status (issue #176): draft | private | published ── */}
        <div className="mt-8 rounded-lg border border-[var(--jf-border)] bg-white p-3">
          <p className="text-xs font-medium uppercase tracking-wider text-[#9ca3af]">
            {t("bld.status")}
          </p>
          <div className="mt-2 grid grid-cols-3 gap-1.5">
            {(["draft", "private", "published"] as const).map((s) => (
              <button
                key={s}
                type="button"
                onClick={() => {
                  setStatus(s);
                  setDirty(true);
                }}
                className={`rounded-md border px-2 py-2 text-xs font-medium transition-colors ${
                  status === s
                    ? "border-[#7170ff]/60 bg-indigo-50 text-[var(--jf-primary)]"
                    : "border-[var(--jf-border)] text-[#374151] hover:bg-slate-50"
                }`}
              >
                {t(`bld.status${s[0].toUpperCase()}${s.slice(1)}` as import("../../components/i18n").Key)}
              </button>
            ))}
          </div>
          {status === "private" && (
            <div className="mt-2">
              <label className="block text-[11px] text-slate-500">{t("bld.accessPassword")}</label>
              <input
                type="password"
                value={accessPw}
                onChange={(e) => {
                  setAccessPw(e.target.value);
                  setDirty(true);
                }}
                placeholder={t("bld.accessPasswordPh")}
                className="jf-input mt-1 text-xs"
              />
              <p className="mt-1 text-[11px] text-[var(--jf-muted)]">{t("bld.accessPasswordHint")}</p>
            </div>
          )}
          <p className="mt-1.5 text-[11px] text-[var(--jf-muted)]">
            {status === "private"
              ? t("bld.statusPrivateHint")
              : status === "published"
                ? t("bld.statusPublishedHint")
                : t("bld.statusDraftHint")}
          </p>
        </div>

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
                const r = await fetch(`/api/rest/forms/${currentId}/settings${spaceQuery()}`, {
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
            <pre className="mt-1 overflow-x-auto rounded-md bg-slate-900 p-2 text-[10px] text-emerald-300">{`<iframe src="${origin}/embed/f/${localStorage.getItem("jf_space") || "app"}/${slug}" style="width:100%;height:640px;border:0" title="${title}"></iframe>`}</pre>
            <button
              onClick={() => {
                navigator.clipboard.writeText(
                  `<iframe src="${origin}/embed/f/${localStorage.getItem("jf_space") || "app"}/${slug}" style="width:100%;height:640px;border:0" title="${title}"></iframe>`,
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
                  const r = await fetch(`/api/rest/forms/${currentId}/tracking${spaceQuery()}`, {
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
                <div className="flex items-center gap-1.5">
                  {formSales.some((s) => isCapturePending(s)) && (
                    <button
                      type="button"
                      onClick={() => void captureAllPending()}
                      disabled={saleActionBusy !== null}
                      className="whitespace-nowrap rounded-md bg-[#5e6ad2] px-2.5 py-1 text-xs font-medium text-white hover:bg-[#828fff] disabled:cursor-not-allowed disabled:opacity-40"
                    >
                      {saleActionBusy === "__all__" ? "…" : `${t("bld.captureAll")} (${formSales.filter((s) => isCapturePending(s)).length})`}
                    </button>
                  )}
                  <a
                    href={`/sales?form=${currentId}`}
                    className="whitespace-nowrap rounded-md border border-[var(--jf-border)] px-2.5 py-1 text-xs text-slate-600 hover:bg-slate-50"
                  >
                    {t("nav.sales")}
                  </a>
                  <a
                    href={`/api/rest/sales/export.csv?form_id=${currentId}`}
                    className="whitespace-nowrap rounded-md border border-[var(--jf-border)] px-2.5 py-1 text-xs text-slate-600 hover:bg-slate-50"
                  >
                    {t("common.exportCsv")}
                  </a>
                </div>
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

          {/* per-form responses view (issue #171) — shown for response-mode forms */}
          {currentId && mode === "response" && (
            <div className="mt-8 rounded-lg border border-[var(--jf-border)] bg-white p-3">
              <div className="flex items-center justify-between gap-2">
                <p className="text-xs font-medium uppercase tracking-wider text-[#9ca3af]">
                  {t("bld.responsesThisForm")}
                </p>
                <span className="text-[10px] text-[var(--jf-muted)]">
                  {submissionsTotal} {t("dash.records")}
                </span>
              </div>
              <div className="mt-2 overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-[var(--jf-border)] text-left text-[10px] uppercase tracking-wide text-[var(--jf-muted)]">
                      <th className="px-2 py-1.5 font-medium">{t("bld.responseData")}</th>
                      <th className="px-2 py-1.5 font-medium">{t("dash.status")}</th>
                      <th className="px-2 py-1.5 font-medium">{t("dash.ref")}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {submissions.map((s) => (
                      <tr key={s.id} className="border-b border-[var(--jf-border)] last:border-0 hover:bg-slate-50/60">
                        <td className="max-w-56 truncate px-2 py-2 text-xs">
                          {Object.entries(s.data || {})
                            .filter(([k]) => !["full_name", "email", "phone"].includes(k))
                            .map(([k, v]) => `${k}: ${String(v)}`)
                            .join(" · ") || "—"}
                        </td>
                        <td className="whitespace-nowrap px-2 py-2">
                          <span className="rounded-full border border-emerald-200 bg-emerald-50 px-2 py-0.5 text-[10px] font-medium text-emerald-700">
                            {t("dash.confirmed")}
                          </span>
                        </td>
                        <td className="whitespace-nowrap px-2 py-2 text-right text-[10px] text-[var(--jf-muted)]">
                          {s.created_at.slice(0, 10)}
                        </td>
                      </tr>
                    ))}
                    {submissions.length === 0 && (
                      <tr>
                        <td colSpan={3} className="px-2 py-6 text-center text-xs text-[var(--jf-muted)]">
                          {submissionsErr || t("bld.noResponsesForm")}
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
              {submissionsTotal > 0 && (
                <div className="mt-2 flex items-center justify-between gap-2 border-t border-[var(--jf-border)] pt-2">
                  <span className="text-[10px] text-[var(--jf-muted)]">
                    {t("dash.pageOf", { page: submissionsPage, total: submissionsTotalPages })}
                  </span>
                  <div className="flex items-center gap-1.5">
                    <button
                      type="button"
                      onClick={() => gotoSubmissionsPage(submissionsPage - 1)}
                      disabled={submissionsPage <= 1}
                      className="rounded-md border border-[var(--jf-border)] px-2.5 py-1 text-xs text-slate-600 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-40"
                    >
                      {t("common.prev")}
                    </button>
                    <button
                      type="button"
                      onClick={() => gotoSubmissionsPage(submissionsPage + 1)}
                      disabled={submissionsPage >= submissionsTotalPages}
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
                  {(localStorage.getItem("jf_space") || "app")}.jomform.com/{f.slug}
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
                      const r = await fetch(`/api/rest/forms/${currentId}/chip-override${spaceQuery()}`, {
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
                      const r = await fetch(`/api/rest/forms/${currentId}/chip-override${spaceQuery()}`, { method: "DELETE" });
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
        <div className="flex items-center justify-between rounded-md border border-[var(--jf-border)] bg-slate-50 px-3 py-2">
          <span className="text-sm">{block.value || p?.name || "Product"}</span>
          <span className="text-sm font-medium text-[var(--jf-primary)]">{p ? money(p.price_sen) : "—"}</span>
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
