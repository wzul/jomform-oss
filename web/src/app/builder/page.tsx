"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { useI18n } from "../../components/i18n";
import { workspaceQuery } from "../../lib/workspace";
import { canMove, moveBlock } from "../../lib/blockReorder";
import { fetchJson, runLoad } from "../../lib/wsfetch";
import {
  adoptedTarget,
  builderControls,
  builderHref,
  builderTarget,
  loadPath,
  saveFormAsTemplate,
  savePlan,
  templateConfigWithStoredKeys,
  templateSaveBody,
  type BuilderTarget,
} from "../../lib/templateEdit";
import {
  PUBLISH_NEEDS_ACK_KEY,
  acknowledgmentBody,
  liveLink,
  publishErrorText,
  publishOutcome,
} from "../../lib/publishResult";
import TemplatePicker from "../../components/TemplatePicker";
import { apiErr, translateApiErr } from "../../lib/apiErr";

/* ─── types ──────────────────────────────────────────────────────────────── */

/* Onboarding gap: what the builder shows after a publish attempt.
   `live`    — the form is live; show the public URL with open + copy.
   `needsAck`— the API refused (409, code=no_products): a payment form with
               nothing to sell may not publish silently. The merchant confirms
               with "Publish anyway", which re-sends the publish acknowledged.
   `error`   — anything else. The notice carries no sentence: it is derived at
               render from the failure's machine code + the builder's own
               fallback (lib/publishResult.ts rule 3). */
type PublishNotice =
  | { kind: "live"; url: string }
  // Message-less on purpose: the sentence is rendered from the dictionary (see
  // lib/publishResult.ts), never from the API's Malay prose.
  | { kind: "needsAck" }
  | { kind: "error"; message: string };

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
  // Card t_c28b45ea: `file` is no longer PLACEABLE — the palette below does not
  // offer it and the AI generator's allowlist does not accept it — but it stays
  // in this union and in the dictionary because a form saved BEFORE that
  // withdrawal can still carry the block, and the canvas draws its chip
  // (typeLabel reads `blk.file`). Nothing here collects it: the API refuses a
  // config that carries the block, and both public renderers print the
  // merchant's label plus a note instead of a picker.
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
  | "divider";

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
  // Issue #388: per-field inline style (safe CSS only, sanitized server-side).
  style?: string;
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

type Product = { id: string; name: string; price_sen: number; type: string; images?: string[] };

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
      // Card t_c28b45ea: no `file` item. The block cannot be served to anyone:
      // no public upload route exists, so no renderer may collect an attachment,
      // and the API refuses a config that carries the block
      // (formfields.UnsupportedBlock — "blok file tidak disokong"). Offering it
      // here only let a merchant assemble a form that could not then be saved;
      // the same withdrawal the withdrawn discount-code block got (card
      // t_4975927e, whose guard scans this file for that block's name, so this
      // rationale names it only by role). A form that already stores the block
      // still loads and still shows its label chip.
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
    ],
  },
];

/* Field types that collect a buyer answer (used for required + conditional
   logic + response-mode collection). */
const fieldTypes: BlockType[] = [
  "name", "email", "phone", "textarea", "checkbox",
  "number", "select", "radio", "date", "time", "website",
  "address", "consent", "hidden", "list", "repeater", "image-choice", "captcha",
];

/* Pricing fields that add to the order total (payment-mode only). */
const pricingTypes: BlockType[] = ["product", "quantity", "option", "shipping", "total"];

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
  const router = useRouter();
  /* translated palette/field labels; block type "textarea" displays as "Note" */
  const typeLabel = (bt: BlockType) => t((bt === "textarea" ? "blk.note" : `blk.${bt}`) as import("../../components/i18n").Key);
  const typeHint = (bt: BlockType) => t((bt === "textarea" ? "blk.noteHint" : `blk.${bt}Hint`) as import("../../components/i18n").Key);
  const [products, setProducts] = useState<Product[]>([]);
  const [currentId, setCurrentId] = useState<string | null>(null);
  const [slug, setSlug] = useState("");
  const [title, setTitle] = useState("New form");
  // Issue #176: form status (draft | private | published | unlisted) + access password.
  const [status, setStatus] = useState<"draft" | "private" | "published" | "unlisted">("draft");
  const [accessPw, setAccessPw] = useState("");
  const [blocks, setBlocks] = useState<Block[]>([]);
  const [selected, setSelected] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [pubUrl, setPubUrl] = useState("");
  // Onboarding gap: the "your form is live" moment. After a successful publish
  // the builder used to show nothing at all (the View link renders only when a
  // URL already resolved), so a first-time publisher had no confirmation, no
  // link and no way to discover the form was on the internet. `publishNotice`
  // is the panel that fills that hole, and it also carries the payment-form
  // warning the API returns instead of publishing silently.
  const [publishNotice, setPublishNotice] = useState<PublishNotice | null>(null);
  // "Link copied ✓" feedback for the panel's copy action. Reset whenever a new
  // notice appears, so a stale ✓ cannot sit beside a different URL.
  const [copied, setCopied] = useState(false);
  const [loadErr, setLoadErr] = useState("");
  // Issue #174: template picker on the new-form path (no ?form= param).
  const [pickerOpen, setPickerOpen] = useState(false);
  // Issue #457: "save as template" — one-click save of the current form as a
  // reusable template (shown next to Publish once the form has been saved).
  const [tplMsg, setTplMsg] = useState("");
  // Issue #457 follow-up (Wan, 2026-09-19): editing one of your OWN templates.
  // The builder edits either a form (?form=<id>) or a template
  // (?template=<id>); a template saves through PUT /api/rest/templates/{id} and
  // must never touch a status/publish route (that would empty it from
  // /templates and push it into every merchant's gallery). The target + its
  // controls + its save plan all come from lib/templateEdit so the decision is
  // unit-tested rather than inline.
  const [target, setTarget] = useState<BuilderTarget>({ kind: "new" });
  const editingTemplate = target.kind === "template";
  const controls = builderControls(target.kind);
  // The template's stored theme and config. Forms keep their theme in Form
  // Settings, but a template has no settings page, so the builder round-trips
  // the theme verbatim AND preserves config keys it has no control for (e.g.
  // `category`, which a gallery card shows) instead of replacing the blob.
  const templateTheme = useRef<Record<string, unknown> | null>(null);
  const templateConfig = useRef<Record<string, unknown> | null>(null);

  // Issue #256: AI form builder — prompt input + generation state.
  const [aiPrompt, setAiPrompt] = useState("");
  const [aiGenerating, setAiGenerating] = useState(false);
  const [aiMsg, setAiMsg] = useState("");

  // Issue #59 → #652: mobile (<md) shows ONE section at a time via a tab bar
  // (Add | Canvas); canvas is the default and the palette is the least
  // important thing on a phone. Desktop ≥768px ignores this and keeps the
  // 3-col grid.
  //
  // The third "Settings" tab is GONE (Wan's report: tapping a field and then
  // having to reach for Settings at the top is not friendly on a phone). The
  // edit panel is now the CANVAS column's mobile face — it appears in place of
  // the canvas while a block is selected, and the panel's own "← Canvas" row is
  // the way out. A tab that could open an empty panel with no block selected
  // was the trap this removes.
  type MobileTab = "add" | "canvas";
  const [mobileTab, setMobileTab] = useState<MobileTab>("canvas");

  // #652: whether the edit panel is open on mobile. This is deliberately NOT
  // the same thing as `selected`: the highlight (selected) survives closing the
  // panel and follows a newly added block, while the panel itself opens only
  // when the user asks for it — by tapping a block card. Without the split, a
  // block added from the Add tab would drop the user straight into a panel they
  // did not ask for.
  const [panelOpen, setPanelOpen] = useState(false);

  // #652: which block card has its ⋯ actions menu open (null = none). One menu
  // at a time; opening another closes the first.
  const [menuOpen, setMenuOpen] = useState<string | null>(null);
  // The ⋯ button that opened the menu, so Escape can hand focus back to it.
  const menuBtn = useRef<HTMLButtonElement | null>(null);
  useEffect(() => {
    if (menuOpen === null) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      setMenuOpen(null);
      menuBtn.current?.focus();
    };
    // A pointer press outside the card's menu region closes it. Inside the
    // region the item's own click handler runs, so a menu item is never lost to
    // this handler.
    const onDown = (e: MouseEvent) => {
      const el = e.target as HTMLElement | null;
      if (!el || !el.closest("[data-block-menu]")) setMenuOpen(null);
    };
    document.addEventListener("keydown", onKey);
    document.addEventListener("mousedown", onDown);
    return () => {
      document.removeEventListener("keydown", onKey);
      document.removeEventListener("mousedown", onDown);
    };
  }, [menuOpen]);

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
  /* Card t_769a5cb9: the drag state the UI PAINTS is state, not a ref.
     `dragging` used to be computed during render as `dragType.current !== null ||
     dragIdx.current !== null` while the drag-start handlers only mutated those
     refs — and a ref mutation schedules no render, so the components committed
     before the drag kept reading `false` (that is what made the drop-zone tint,
     the insertion gap and the palette dim dead; the drop path reads the refs
     directly, which is why the drop worked anyway). Reading a ref during render
     is the defect, so the flag the three paint sites share is now real render
     input; the two refs stay as the payload the drop handlers consume. */
  const [dragging, setDragging] = useState(false);

  // Issue #503: 401/403 → /login, the same redirect Shell.signOut() uses.
  const goToLogin = useCallback(() => {
    router.replace("/login");
    window.location.href = "/login";
  }, [router]);

  const load = useCallback(async () => {
    // Issue #503: this used `r.ok ? r.json() : []`, so a failed products fetch
    // (including an expired session) silently emptied the palette with no
    // error. Surface it — 401/403 → /login, otherwise the translated message.
    // The palette is only an aid, so a failure does NOT block the builder.
    const r = await runLoad(
      async () => {
        const d = await fetchJson<unknown>(`/api/rest/products${workspaceQuery()}`);
        return Array.isArray(d) ? (d as Product[]) : [];
      },
      { onUnauth: goToLogin, message: t("bld.prodLoadFail"), errorKey: (b: unknown) => translateApiErr(b, t) },
    );
    if (!r.ok) {
      setProducts([]);
      setLoadErr(r.error);
      return;
    }
    setProducts(r.data);
  }, [t, goToLogin]);

  // hydrate from ?form=<id> (a form) or ?template=<id> (one of your own
  // templates — issue #457 follow-up). Both are `forms` rows, so they hydrate
  // the same way; only the route they load from and the route they save to
  // differ, and that difference is the whole point (a template must never touch
  // a status/publish route).
  useEffect(() => {
    load();
    const params = new URLSearchParams(window.location.search);
    const resolved = builderTarget({
      template: params.get("template"),
      form: params.get("form"),
      isNew: params.get("new") === "1",
    });
    setTarget(resolved);
    const id = resolved.kind === "new" ? null : resolved.id;
    if (!id) {
      // Issue #174: only the bare new-form path (no ?form=, no ?template= and
      // no ?new=) shows the template picker. ?new=1 means "start blank", so it
      // must NOT open the picker.
      if (!params.get("new")) setPickerOpen(true);
      return;
    }
    void (async () => {
      // Issue #503: `if (!r.ok) return;` silently abandoned hydration — an
      // expired session left the builder showing a blank default form with no
      // explanation. Now a 401/403 goes to /login and any other failure is a
      // visible load error.
      // A template is read from the TEMPLATE route: it is owner-scoped
      // (TemplateByIDForAccount), so another account's template — or a gallery
      // template — fails as 404 instead of opening read-only-able.
      const r = await runLoad(
        // The response is a JSON document read field-by-field (mode, blocks,
        // chip_mapping, products, …), so its shape is genuinely open here.
        // `Record<string, unknown>` is not a stricter version of this — it makes
        // every read a type error (measured: 8 TS2345/TS2339s in `next build`),
        // which is why the `any` is load-bearing rather than lazy.
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        () => fetchJson<Record<string, any>>(`${loadPath(resolved)(id)}${workspaceQuery()}`),
        { onUnauth: goToLogin, message: t(resolved.kind === "template" ? "tplmg.editLoadFail" : "bld.formLoadFail") },
      );
      if (!r.ok) {
        setLoadErr(r.error);
        return;
      }
      const d = r.data;
      setCurrentId(d.id);
      setSlug(d.slug || "");
      setTitle(d.title);
      setStatus(d.status === "private" ? "private" : d.status === "published" ? "published" : d.status === "unlisted" ? "unlisted" : "draft");
      setPubUrl(d.status === "published" || d.status === "unlisted" ? d.url : "");
      const cfg = d.config || {};
      setMode(cfg.mode === "response" ? "response" : "payment");
      setChipMapping(cfg.chip_mapping || {});
      if (resolved.kind === "template") {
        templateTheme.current = d.theme && typeof d.theme === "object" ? (d.theme as Record<string, unknown>) : null;
        templateConfig.current = cfg as Record<string, unknown>;
      }
      const hydrated: Block[] = [];
      (cfg.blocks || []).forEach(
        (b: Record<string, unknown>) => {
          hydrated.push(hydrateBlock(b));
        },
      );
      setBlocks(hydrated.length ? hydrated : defaultBlocks(cfg.products || []));
    })();
  }, [load, goToLogin, t]);

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
      style: b.style as string | undefined,
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
    // the canvas so the new block is immediately visible. #652: the panel does
    // NOT auto-open here — the new block is highlighted on the canvas instead,
    // which is the behaviour that already worked and that keeps "add several
    // fields in a row" one tap each.
    setPanelOpen(false);
    setMobileTab("canvas");
  }

  /* #652: opening and closing the mobile edit panel. The panel IS the canvas
     column's mobile face — it replaces the canvas below `md` while it is open.
     Two rules matter:

       * The way IN is a tap anywhere on the block card, which is what a phone
         user actually reaches for (the old flow selected the block silently and
         then needed the Settings tab).
       * The way OUT is the panel's own header row. Without it this change would
         swap one trap (a tab at the top) for a worse one (no exit at all): the
         old Settings tab had no back button either. */
  function openBlockPanel(id: string) {
    setSelected(id);
    setMenuOpen(null);
    setPanelOpen(true);
    setMobileTab("canvas");
  }

  function closeBlockPanel() {
    // Keep the block selected: after closing, the canvas keeps the highlight so
    // the user can see which block they were editing (and tap it to return).
    setMenuOpen(null);
    setPanelOpen(false);
    setMobileTab("canvas");
  }

  function updateBlock(id: string, patch: Partial<Block>) {
    setBlocks((prev) => prev.map((b) => (b.id === id ? { ...b, ...patch } : b)));
    setDirty(true);
  }

  function removeBlock(id: string) {
    setBlocks((prev) => prev.filter((b) => b.id !== id));
    if (selected === id) {
      setSelected(null);
      // #652: the panel edits the selected block, so removing that block must
      // close the panel — otherwise the canvas column stays hidden behind a
      // panel with nothing left to edit.
      setPanelOpen(false);
    }
    setMenuOpen(null);
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

  /** Move a canvas block one row in `dir` (-1 up, +1 down).
   *
   *  The arrow buttons used to call `move(i, i ± 1)`, but `insertAt` resolves
   *  its index against the array as it stood BEFORE the row was removed (the
   *  semantics the drag-and-drop gap zones need). For the down button that
   *  collapsed to `(i + 1) - 1 = i`, so ↓ spliced the row out and put it back
   *  where it was: a no-op at every position, while ↑ worked. A row move is a
   *  swap of adjacent rows — the same shape the other reorder controls in this
   *  repo use (products moveImage / moveVariation) — so up and down can no
   *  longer disagree about the arithmetic. `insertAt` is untouched and still
   *  serves the drag path. */
  function moveRow(i: number, dir: -1 | 1) {
    setBlocks((prev) => moveBlock(prev, i, dir));
    setDirty(true);
  }

  /* drag wiring */

  /**
   * Save the current record. Returns the id this save WROTE — the new row's id
   * after a create, the edited row's id otherwise — or `null` when any step of
   * the save failed (`null` rather than `undefined`, so "nothing was written" is
   * one value every caller can test).
   *
   * A caller that needs the id after awaiting this must use the RETURN VALUE
   * and not re-read `currentId`: that is a value captured by the render's
   * closure, so reading it again on the far side of the await reads the state
   * this handler closed over. That was the "Save as template" defect
   * (t_c33effe4) — `setCurrentId` re-rendered the component, but no re-render
   * can rebind a variable a running handler already holds.
   */
  async function save(publish: boolean): Promise<string | null> {
    // Issue #176: a blank slug is auto-generated by the backend on create, so
    // it is no longer required here. The merchant can still edit it manually.
    setSaving(true);
    setLoadErr("");
    // A template that failed to load has no id to write to. Without this guard
    // the isCreate branch below would POST a create to the template collection
    // from a page whose template never loaded — a new template out of an error
    // screen. Refuse and keep the load error visible instead.
    if (editingTemplate && !currentId) {
      setLoadErr(t("tplmg.editLoadFail"));
      setSaving(false);
      return null;
    }
    // Which record this save writes, and through which routes, is decided once
    // in lib/templateEdit (unit-tested): a TEMPLATE saves with
    // PUT /api/rest/templates/{id} and gets NO status write and NO publish,
    // because setting a template's status to 'published' would remove it from
    // /templates (ListTemplatesByAccount excludes published rows) and insert it
    // into the public gallery every merchant picks new forms from
    // (ListTemplates filters is_template AND published with NO account filter).
    const plan = savePlan(target, publish);
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
        style: b.style || undefined,
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
      products: products.map((p) => ({ id: p.id, name: p.name, price_sen: p.price_sen, images: p.images })),
    };
    const slugValue = slug.toLowerCase().replace(/[^a-z0-9-]/g, "-");
    const payload = { slug: slugValue, title, config };

    // Issue #501: an image block whose src is not https:// would render as the
    // html/template #ZgotmplZ sentinel on the buyer's page (HTTP 200, no
    // image). Catch it here so it shows in the builder error line, and let
    // the server's rejection message surface below if it still slips through
    // (the server is the authority).
    const badSrc = blocks.find((b) => b.type === "image" && (b.src || "").trim() && !(b.src || "").trim().startsWith("https://"));
    if (badSrc) {
      setLoadErr(t("url.notHttps"));
      setSaving(false);
      return null;
    }

    let id = currentId;
    if (plan.steps.includes("writeRecord")) {
      const isCreate = !id;
      // A template's own body: title + config (+ its unchanged theme, so the
      // PUT's COALESCE cannot clear the design). A form keeps its existing
      // PATCH body — the theme belongs to Form Settings, not the builder.
      const body = editingTemplate
        ? templateSaveBody(
            title,
            templateConfigWithStoredKeys(templateConfig.current, config as Record<string, unknown>),
            templateTheme.current,
          )
        : isCreate
          ? payload
          : { title, config };
      const wr = await fetch(`${plan.recordPath(id || "")}${workspaceQuery()}`, {
        method: isCreate ? "POST" : plan.method,
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      if (!wr.ok) {
        const d = await wr.json().catch(() => ({}));
        setLoadErr(
          apiErr(d, t(editingTemplate ? "tplmg.editFail" : "bld.saveFail"), t),
        );
        setSaving(false);
        return null;
      }
      if (isCreate) {
        const d = await wr.json();
        setCurrentId(d.id);
        setSlug(d.slug || "");
        id = d.id;
        // The builder ADOPTS the row it just made (t_b3cc2a7a). Two things were
        // missing here and each had its own symptom:
        //
        //   setTarget — the target stayed {kind:"new"}, so savePlan kept
        //   deciding POST for a row that already exists. The next Save then sent
        //   POST /api/rest/forms/{id}, which is not a route (only POST /forms
        //   and PATCH /forms/{id} exist) -> 405 with no machine code -> the
        //   generic "Failed to save." with nothing naming the cause.
        //
        //   replaceState — the URL stayed /builder?new=1, and builderTarget
        //   reads only ?form=/?template=, so a refresh after create+publish
        //   opened a BRAND-NEW empty form instead of the one just made.
        //
        // history.replaceState rather than a router navigation: this must not
        // re-run the mount effect below, which hydrates from
        // window.location.search and would reload (and re-parse) over the state
        // this very save just produced. Replacing the current history entry
        // means Back still leaves the builder rather than bouncing off a
        // ?form= entry the user never navigated to. The dropped ?new=1 is
        // deliberate — the session is no longer a new-form session.
        const adopted = adoptedTarget(d.id);
        if (adopted) {
          setTarget(adopted);
          window.history.replaceState(null, "", builderHref(adopted));
        }
      } else if (editingTemplate) {
        // A template has no slug and no public URL; the PUT answers with the
        // stored row, so keep the title canonical (the API trims it).
        const d = await wr.json().catch(() => null);
        if (d && typeof d.title === "string") setTitle(d.title);
      }
    }

    // Issue #176: apply the selected status (draft | private | published).
    // The Publish button forces published; private requires the access
    // password; published/draft clears any stored password. FORMS only —
    // `writeStatus` is absent from a template's plan by construction.
    if (plan.steps.includes("writeStatus") && id) {
      const targetStatus = publish ? "published" : status;
      const statusBody: { status: string; password?: string } = { status: targetStatus };
      if (targetStatus === "private") statusBody.password = accessPw;
      const sr = await fetch(`/api/rest/forms/${id}/status${workspaceQuery()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(statusBody),
      });
      if (!sr.ok) {
        // Onboarding gap: the status route is the one the dashboard's status
        // control reaches, and it refuses to make a payment form with nothing
        // to sell live without an acknowledgement. Surface it as the warning
        // panel (with "Publish anyway"), not as an opaque error line.
        const d = await sr.json().catch(() => ({}));
        // The `needs_ack` branch is the "Publish anyway" flow (#694) and stays:
        // the status route is the one the dashboard's status control reaches, and
        // it refuses to make a payment form with nothing to sell live without an
        // acknowledgement. The ERROR branch must not print the API's prose, so it
        // goes through apiErr (issue #6177) — the outcome's own message is a
        // server string and is deliberately not rendered.
        const outcome = publishOutcome({ ok: false, status: sr.status, body: d }, "");
        setPublishNotice(
          outcome.kind === "needs_ack"
            ? { kind: "needsAck" }
            : { kind: "error", message: apiErr(d, t("bld.statusSaveFail"), t) },
        );
        setSaving(false);
        return null;
      }
      setStatus(targetStatus as "draft" | "private" | "published" | "unlisted");
    }
    if (plan.steps.includes("publish") && id) {
      const pr = await fetch(`/api/rest/forms/${id}/publish${workspaceQuery()}`, { method: "POST" });
      const pd = await pr.json().catch(() => ({}));
      const outcome = publishOutcome({ ok: pr.ok, status: pr.status, body: pd }, "");
      if (outcome.kind !== "published") {
        // A `needsAck` notice carries no message: its sentence is rendered from
        // the dictionary (the API's prose for that refusal is Malay) — see
        // lib/publishResult.ts. The error notice likewise carries none: the
        // sentence comes from the failure's machine code (t_10453dd9), so the
        // API's prose cannot reach the panel through either branch.
        setPublishNotice(
          outcome.kind === "needs_ack"
            ? { kind: "needsAck" }
            : { kind: "error", message: publishErrorText(outcome, t("bld.saveFail"), t) },
        );
        setSaving(false);
        await load();
        return null;
      }
      // Issue #450: build the public URL from the API's url field (resolves the
      // owning workspace slug / custom domain server-side), never a client-side
      // "app" fallback which 404s.
      const refreshed = await fetch(`/api/rest/forms/${id}${workspaceQuery()}`)
        .then((r) => (r.ok ? r.json() : null))
        .catch(() => null);
      const wsSlug = (() => {
        try {
          return localStorage.getItem("jf_workspace") || "";
        } catch {
          return "";
        }
      })();
      const url =
        (refreshed && refreshed.url) ||
        (wsSlug ? `https://${wsSlug}.jomform.com/${slugValue}` : "");
      setPubUrl(url);
      // The missing moment: tell the merchant the form is live and hand over the
      // link (the panel renders no anchor unless the URL is a real https one —
      // see liveLink; an empty href would reload the current page).
      setCopied(false);
      setPublishNotice({ kind: "live", url });
      await load();
    }
    if (editingTemplate) setTplMsg(t("tplmg.editSaved"));
    setSaving(false);
    setDirty(false);
    // The id this save wrote (the created row's, after a create). Every failure
    // above has already returned null, so a non-null answer means the steps of
    // this plan all landed.
    return id;
  }

  /* Onboarding gap: copy the just-published link. Guarded on an https URL so
     nothing but a real one can ever reach the clipboard (or, one call up, an
     href). A failed clipboard write leaves the label unchanged rather than
     claiming success. */
  async function copyPublishUrl(url: string) {
    const link = liveLink(url);
    if (!link) return;
    try {
      await navigator.clipboard.writeText(link);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  }

  /* Onboarding gap: "Publish anyway" — the merchant confirmed that a payment
     form with nothing to sell should still go live. Both the status write and
     the publish call are re-sent with the acknowledgement, because either can
     be the one the API refused, and the publish route's own body carries it. */
  async function publishAnyway() {
    if (!currentId) return;
    setPublishNotice(null);
    setSaving(true);
    try {
      await fetch(`/api/rest/forms/${currentId}/status${workspaceQuery()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ status: "published", acknowledge: true }),
      });
      const pr = await fetch(`/api/rest/forms/${currentId}/publish${workspaceQuery()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(acknowledgmentBody()),
      });
      const pd = await pr.json().catch(() => ({}));
      const outcome = publishOutcome({ ok: pr.ok, status: pr.status, body: pd }, "");
      if (outcome.kind !== "published") {
        // The reported case (t_10453dd9): this retry answers an UNCODED 409, and
        // the sentence used to be the outcome's own `message` — i.e. whatever
        // Malay prose the API sent. It now comes from the machine code + the
        // builder's fallback, exactly like the publish path above.
        setPublishNotice({
          kind: "error",
          message: publishErrorText(outcome, t("bld.saveFail"), t),
        });
        return;
      }
      const refreshed = await fetch(`/api/rest/forms/${currentId}${workspaceQuery()}`)
        .then((r) => (r.ok ? r.json() : null))
        .catch(() => null);
      const url = (refreshed && refreshed.url) || "";
      setStatus("published");
      setPubUrl(url);
      setPublishNotice({ kind: "live", url });
      await load();
    } finally {
      setSaving(false);
      setDirty(false);
    }
  }

  // Issue #457: save the current form (its saved config) as a reusable
  // template. The form must exist first, so this saves before snapshotting.
  //
  // The id comes from the SAVE'S RETURN VALUE, never from a second read of
  // `currentId` (t_c33effe4): that is a value captured by this render's closure,
  // so the `setCurrentId` inside save() re-renders the component but cannot
  // rebind the variable a handler that is already awaiting holds. Re-reading it
  // saw the stale null and returned — which is why an unsaved form got its row
  // created, no template, and not one word of feedback. Both rules (which id,
  // and "a save that wrote nothing creates no template") live in the tested
  // helper; the component only renders its outcome.
  async function saveAsTemplate() {
    setTplMsg("");
    const outcome = await saveFormAsTemplate({
      currentId,
      ensureSaved: () => save(false),
      snapshot: async (id) => {
        const r = await fetch(`/api/rest/templates${workspaceQuery()}`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ form_id: id, title }),
        });
        return r.ok;
      },
    });
    if (outcome === "skipped") return; // the save failed; its error is shown already
    setTplMsg(
      t(outcome === "saved" ? "bld.saveAsTemplateDone" : "bld.saveAsTemplateFail"),
    );
  }

  /* Issue #256: AI form builder — send the prompt to the backend, which calls
     deepseek-v4.1-flash to draft a validated form config, then load the
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
      const r = await fetch(`/api/rest/forms/ai-generate${workspaceQuery()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ prompt }),
      });
      const d = await r.json().catch(() => ({}));
      if (!r.ok) {
        setAiMsg(
          r.status === 429
            ? t("bld.aiRateLimited")
            : t("bld.aiError", { error: apiErr(d, String(r.status), t) }),
        );
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
        style: b.style as string | undefined,
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

  /* #652: the panel is the canvas column's mobile face, and it exists ONLY
     while a block is selected — that invariant is DERIVED here rather than
     trusted at each call site. It has to be: `panelOpen` is not the only thing
     that can change, and a cleared selection used to be enough to strand the
     panel. Reproduced before this line existed — AI-generate on the Add tab
     (`aiGenerate` replaces the canvas and calls `setSelected(null)`) left
     `panelOpen` true, so tapping Canvas showed a panel reading "Select a block
     to edit" with NO ← Canvas row (that row requires a selected block) while
     the freshly drafted blocks stayed hidden — a trap with no exit, which is
     exactly what this change exists to remove. Deriving it means any future
     path that drops the selection closes the panel instead of re-creating it. */
  const panelVisible = panelOpen && !!selectedBlock;

  // `dragging` (the palette dim, the insertion gaps, the drop ghost's tint) is
  // declared with the other drag state at the top of the component — as STATE,
  // so the render that paints it is the render the drag started in (t_769a5cb9).

  // Issue #61: upload an image for the selected block via the direct S3
  // upload endpoint (presign superseded by the owner's server-side-optimise
  // flow). Returns the public URL, stores it into the block's src.
  //
  // The two client-side pre-flight refusals are TRANSLATED, not literals: the
  // builder is the one dashboard surface where the user's dashboard language is
  // the only language on screen, and a hardcoded sentence here reached an
  // English (or Chinese/Tamil/Arabic) merchant verbatim. The server re-checks
  // both (uploads.go sniffs the real bytes), so these are a round-trip saving —
  // never the only guard.
  async function handleImageUpload(file: File) {
    if (!selectedBlock || selectedBlock.type !== "image") return;
    if (!["image/png", "image/jpeg", "image/webp", "image/gif"].includes(file.type)) {
      setLoadErr(t("bld.imageTypeUnsupported"));
      return;
    }
    if (file.size > 20 * 1024 * 1024) {
      setLoadErr(t("media.uploadTooLarge"));
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
        setLoadErr(apiErr(d, t("bld.uploadImageFail"), t));
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
    // a11y (card t_524d186d): every control here needs a real accessible name.
    // The <label> used to be a SIBLING that wrapped nothing and had no htmlFor,
    // so these inputs were named only by their `placeholder` attribute — and a
    // placeholder is not a label: axe accepts it as a name, but it disappears
    // the moment you type, and WCAG 3.3.2 wants the instruction to persist.
    // The id is scoped by block so it stays unique even if the same key is ever
    // rendered twice in one panel. Per-choice rows add their 0-based INDEX for the
    // same reason: the choices editor renders the same key many times, so without
    // the index every row's id would collide.
    const fid = (key: string, ns?: number) => `jf-blk-${b.id}-${key}${ns === undefined ? "" : `-${ns}`}`;
    const input = (key: keyof Block, label: string, ph?: string) => (
      <div>
        <label htmlFor={fid(String(key))} className="text-xs text-[var(--jf-muted)]">
          {label}
        </label>
        <input
          id={fid(String(key))}
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
          {/* the same defect the helper had: a bare <label> sibling names
              nothing — axe `label` (critical). */}
          <label htmlFor="jf-block-value" className="text-xs text-[var(--jf-muted)]">
            {t("bld.textLabel")}
          </label>
          <textarea
            id="jf-block-value"
            rows={2}
            value={b.value}
            onChange={(e) => upd({ value: e.target.value })}
            className="mt-1 w-full rounded-md border border-[var(--jf-border)] bg-white px-2.5 py-2 text-sm outline-none focus:border-[#7170ff]/60"
          />
        </div>

        {/* Common: description */}
        {input("description", t("bld.description"), t("bld.descriptionPh"))}

        {/* Common: placeholder (input fields). `date` and `time` are NOT in
            the list (card t_23289ce3): browsers do not paint a placeholder on a
            date/time input (the control shows its own format hint), so offering
            it there was a second dead setting. The public renderers emit the
            attribute for every type that is offered here. */}
        {["text", "textarea", "number", "email", "phone", "website", "address", "captcha", "list", "repeater"].includes(b.type) &&
          input("placeholder", t("bld.placeholder"), t("bld.placeholderPh"))}

        {/* Common: CSS class */}
        {input("cssClass", t("bld.cssClass"), t("bld.cssClassPh"))}

        {/* Issue #388: per-field inline style — collapsed "advanced" so the
            default builder stays Google-Forms-simple. Safe CSS only; the
            server sanitizes it (no url()/background-image). */}
        <details className="rounded-md border border-[var(--jf-border)] px-2.5 py-2">
          <summary className="cursor-pointer text-xs font-medium text-[var(--jf-muted)]">
            {t("bld.advanced")}
          </summary>
          <div className="mt-2 space-y-2">
            {input("style", t("bld.inlineStyle"), t("bld.inlineStylePh"))}
            <p className="text-[11px] leading-relaxed text-[var(--jf-muted-on-tint)]">{t("bld.inlineStyleHint")}</p>
          </div>
        </details>

        {/* Text/textarea: max length + default value */}
        {(b.type === "text" || b.type === "textarea") && (
          <>
            <div>
              <label htmlFor={fid("maxLength")} className="text-xs text-[var(--jf-muted)]">
                {t("bld.maxLength")}
              </label>
              <input
                id={fid("maxLength")}
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

        {/* Date/time: format. The picker chooses the CONTROL the buyer gets
            (a date picker, a date+time picker or a time picker), which is what
            both public renderers now paint (render_blocks.go dateInputType).
            The `name` block's own format picker is GONE (card t_23289ce3): it
            had no reader in either renderer and no defined rendering — the name
            block has always been one `full_name` text input. */}
        {(b.type === "date" || b.type === "time") && (
          <div>
            <label htmlFor={fid("dateFormat")} className="text-xs text-[var(--jf-muted)]">
              {t("bld.dateFormat")}
            </label>
            <select
              id={fid("dateFormat")}
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

        {/* Choices editor: select/radio/image-choice/option/shipping.
            a11y (card t_c4abb920): each row's label input used to be named ONLY
            by its `placeholder` ("Label") and the price input ONLY by its
            `title` — a placeholder/title is not a label: axe accepts it as a
            name, but it disappears the moment you type, and WCAG 3.3.2 wants
            the instruction to persist. Each input now gets a real name via
            `aria-label` built from the row's 1-based position, and the id is
            index-scoped so two rows cannot share one id. The heading below is
            a `role="group"` labelled by `bld.choices`, so a screen reader gets
            a named group instead of a list of unnamed text boxes. */}
        {choiceTypes.includes(b.type) && (
          <div role="group" aria-labelledby={fid("choicesHeading")}>
            <label id={fid("choicesHeading")} className="text-xs text-[var(--jf-muted)]">
              {t("bld.choices")}
            </label>
            <div className="mt-1 space-y-1.5">
              {(b.choices || []).map((c, ci) => (
                <div key={ci} className="flex items-center gap-1.5">
                  <input
                    id={fid("choiceLabel", ci)}
                    aria-label={`${t("bld.choiceLabel")} ${ci + 1}`}
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
                      id={fid("choicePrice", ci)}
                      aria-label={`${t("bld.choicePrice")} ${ci + 1}`}
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
                    aria-label={`${t("bld.removeChoice")} ${ci + 1}`}
                    title={t("bld.removeChoice")}
                    className="rounded-md px-1.5 text-xs text-[var(--jf-muted-on-tint)] hover:text-[var(--jf-danger-text)]"
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
            <label htmlFor={fid("productId")} className="text-xs text-[var(--jf-muted)]">
              {t("bld.product")}
            </label>
            <select
              id={fid("productId")}
              value={b.productId || ""}
              onChange={(e) => {
                // Issue #461: the block label follows the product. Switching to
                // another product must not leave the previous product's name as
                // a stale label — but a label the merchant typed themselves is
                // kept (the server renders it as an explicit per-form override).
                const nextId = e.target.value;
                const prev = products.find((x) => x.id === b.productId);
                const next = products.find((x) => x.id === nextId);
                const patch: Partial<Block> = { productId: nextId };
                if (next && (!b.value || b.value === prev?.name)) patch.value = next.name;
                upd(patch);
              }}
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
            <label htmlFor={fid("src")} className="text-xs text-[var(--jf-muted)]">
              {t("bld.imageUrl")}
            </label>
            <input
              id={fid("src")}
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

        {/* Validation: required message + pattern. The pattern is offered only
            for the input types that HAVE the attribute (card t_23289ce3): the
            HTML spec ignores `pattern` on anything but text/search/url/tel/
            email/password inputs, and `<textarea>` has no pattern attribute at
            all — so a pattern on a number or a note could never fire. */}
        {fieldTypes.includes(b.type) && b.required && (
          <>
            {input("requiredMessage", t("bld.requiredMsg"), t("bld.requiredMsgPh"))}
            {["text", "email", "phone", "website"].includes(b.type) &&
              input("pattern", t("bld.pattern"), t("bld.patternPh"))}
          </>
        )}

        {/* conditional logic */}
        {fieldTypes.includes(b.type) && blocks.length > 1 && (
          <div className="rounded-md border border-slate-200 p-2">
            {/* a11y (card t_524d186d): this group heading names every control in
                the block. The rule picker had no accessible name at all (axe
                `select-name`, critical); the operator select and the value input
                only had one while `placeholder` supplied it. text-slate-500 was
                4.47 on --jf-bg — just under the 4.5 line; slate-600 is 7.13. */}
            <p id={`jf-cond-${b.id}`} className="text-[11px] font-medium text-slate-600">
              {t("bld.condLogic")}
            </p>
            <select
              aria-labelledby={`jf-cond-${b.id}`}
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
                  aria-labelledby={`jf-cond-${b.id}`}
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
                  aria-labelledby={`jf-cond-${b.id}`}
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
          <a
            href={editingTemplate ? "/templates" : "/forms"}
            className="shrink-0 rounded-md px-2 py-1 text-sm text-[var(--jf-muted)] hover:text-[#374151]"
          >
            ←
          </a>
          <input
            value={title}
            onChange={(e) => {
              setTitle(e.target.value);
              setDirty(true);
            }}
            aria-label={t(editingTemplate ? "tplmg.editTitle" : "bld.formTitle")}
            placeholder={t("bld.formTitle")}
            className="min-w-0 flex-1 rounded-md border border-transparent bg-transparent px-2 py-1 text-sm font-medium outline-none hover:border-[var(--jf-border)] focus:border-[#7170ff]/60 md:w-56 md:flex-none"
          />
          {/* Publish is row 1 on mobile; row 2 on desktop via ml-auto below.
              On mobile the actions block moves to row 2 (hidden here).
              Template mode (issue #457 follow-up): a template has no draft /
              published lifecycle — publishing it would remove it from
              /templates and put it in every merchant's gallery — so this
              control is replaced by a plain Save. */}
          {(controls.publish || editingTemplate) && (
            <button
              onClick={() => save(controls.publish)}
              disabled={saving}
              className="shrink-0 md:hidden jf-btn-primary"
            >
              {saving
                ? t("builder.saving")
                : editingTemplate
                  ? t("tplmg.save")
                  : currentId
                    ? t("builder.publish")
                    : t("builder.savePub")}
            </button>
          )}
        </div>
        <div className="flex min-w-0 items-center gap-2 border-t border-[var(--jf-border)] pt-1.5 md:ml-auto md:border-0 md:pt-0">
          {controls.slug && (
            <input
              value={slug}
              onChange={(e) => {
                setSlug(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, "-"));
                setDirty(true);
              }}
              placeholder="slug-url"
              className="min-w-0 flex-1 rounded-md border border-transparent bg-transparent px-2 py-1 font-mono text-xs text-[var(--jf-muted)] outline-none hover:border-[var(--jf-border)] focus:border-[#7170ff]/60 md:w-40 md:flex-none"
            />
          )}
          {editingTemplate && (
            <span className="shrink-0 font-mono text-xs text-[var(--jf-muted)]">{t("tplmg.editLabel")}</span>
          )}
          <span className="shrink-0">
            {dirty && <span className="text-xs text-[var(--jf-warning-text)]">• {t("builder.unsaved")}</span>}
          </span>
          {controls.view && pubUrl && (
            <a
              href={pubUrl}
              target="_blank"
              className="shrink-0 rounded-md border border-[var(--jf-border)] bg-white px-3 py-1.5 text-xs text-[#374151] hover:bg-slate-50"
            >
              {t("bld.view")}
            </a>
          )}
          {/* Issue #457: save this form's design as a reusable template. NOT in
              template mode — a template cannot be a template of itself. */}
          {controls.saveAsTemplate && (
            <button
              onClick={() => void saveAsTemplate()}
              disabled={saving}
              title={t("bld.saveAsTemplate")}
              className="hidden shrink-0 rounded-md border border-[var(--jf-border)] bg-white px-3 py-1.5 text-xs text-[#374151] hover:bg-slate-50 disabled:opacity-50 md:inline-flex"
            >
              {t("bld.saveAsTemplate")}
            </button>
          )}
          {/* desktop Publish — hidden on mobile (it's in row 1). */}
          {controls.publish && (
            <button
              onClick={() => save(true)}
              disabled={saving}
              className="hidden md:inline-flex jf-btn-primary"
            >
              {saving ? t("builder.saving") : currentId ? t("builder.publish") : t("builder.savePub")}
            </button>
          )}
          {/* Template mode: the only write action is Save (PUT /templates/{id}). */}
          {editingTemplate && (
            <button
              onClick={() => save(false)}
              disabled={saving}
              className="hidden shrink-0 md:inline-flex jf-btn-primary disabled:opacity-50"
            >
              {saving ? t("builder.saving") : t("tplmg.save")}
            </button>
          )}
        </div>
      </header>

      {/* Onboarding gap: the moment the form goes live. Placed directly under
          the builder header — the surface the merchant is already looking at
          when they press Publish — because that is exactly where the missing
          confirmation was. It carries the public URL with an open action and a
          copy action, or the payment-form warning with its confirm action. */}
      {publishNotice && (
        <div
          role={publishNotice.kind === "live" ? "status" : "alert"}
          className={`flex flex-col gap-2 border-b px-3 py-2.5 md:px-4 ${
            publishNotice.kind === "live"
              ? "border-emerald-200 bg-emerald-50"
              : publishNotice.kind === "needsAck"
                ? "border-amber-200 bg-amber-50"
                : "border-red-200 bg-red-50"
          }`}
        >
          {publishNotice.kind === "live" ? (
            <div className="flex min-w-0 flex-wrap items-center gap-2">
              <span className="shrink-0 text-sm font-medium text-emerald-800">{t("bld.liveTitle")}</span>
              {liveLink(publishNotice.url) ? (
                <>
                  <a
                    href={liveLink(publishNotice.url) as string}
                    target="_blank"
                    rel="noreferrer"
                    className="jf-btn-primary"
                  >
                    {t("bld.openForm")}
                  </a>
                  <button
                    type="button"
                    onClick={() => void copyPublishUrl(publishNotice.url)}
                    className="jf-btn-ghost"
                  >
                    {copied ? t("bld.urlCopied") : t("bld.copyUrl")}
                  </button>
                  <a
                    href={liveLink(publishNotice.url) as string}
                    target="_blank"
                    rel="noreferrer"
                    className="min-w-0 truncate font-mono text-[11px] text-emerald-800 underline"
                  >
                    {publishNotice.url}
                  </a>
                </>
              ) : (
                <span className="text-xs text-emerald-900">{t("bld.liveNoUrl")}</span>
              )}
              <button
                type="button"
                onClick={() => setPublishNotice(null)}
                className="ml-auto shrink-0 text-xs text-emerald-800 underline"
              >
                {t("bld.dismiss")}
              </button>
            </div>
          ) : (
            <div className="flex min-w-0 flex-wrap items-center gap-2">
              <span className="text-xs text-[var(--jf-text)]">
                {publishNotice.kind === "needsAck"
                  ? t(PUBLISH_NEEDS_ACK_KEY)
                  : publishNotice.message}
              </span>
              {/* Docs parity (AGENTS.md rule 4): the "?" beside the field that
                  raises the question deep-links the publish section of the
                  knowledge base. t_d768342e: the publish walk-through now lives
                  on the customize guide, so the link follows it there. */}
              <a
                href="/docs/customize#publishing"
                className="shrink-0 text-xs font-medium text-[var(--jf-primary)] underline"
              >
                ?
              </a>
              {publishNotice.kind === "needsAck" && (
                <button
                  type="button"
                  onClick={() => void publishAnyway()}
                  disabled={saving}
                  className="jf-btn-primary shrink-0"
                >
                  {t("bld.publishAnyway")}
                </button>
              )}
              <button
                type="button"
                onClick={() => setPublishNotice(null)}
                className="ml-auto shrink-0 text-xs underline"
              >
                {t("bld.dismiss")}
              </button>
            </div>
          )}
          {/* The "anyone with this link" hint only makes sense when there IS a
              link. With no resolvable URL the panel already says so, and adding
              "anyone who opens this link" beside it contradicts itself — which
              the render proof caught. */}
          {publishNotice.kind === "live" && Boolean(publishNotice.url) && (
            <p className="text-[11px] text-emerald-800">{t("bld.liveHint")}</p>
          )}
        </div>
      )}

      {/* mobile (<md) section switcher — issue #59: one tab at a time,
          canvas is default. Desktop keeps the 3-col grid (md:grid).
          #652: TWO tabs, not three — "Settings" is gone; editing a block is a
          tap on the block itself, so a tab for it would be a second, emptier
          route to the same panel. */}
      <nav className="flex border-b border-[var(--jf-border)] bg-white md:hidden">
        {(["add", "canvas"] as MobileTab[]).map((tab) => (
          <button
            key={tab}
            onClick={() => {
              // Leaving to the palette closes an open actions menu; the panel
              // itself stays open, because the canvas tab is its home.
              setMenuOpen(null);
              setMobileTab(tab);
            }}
            aria-current={mobileTab === tab ? "true" : undefined}
            className={`min-h-11 flex-1 py-2.5 text-sm font-medium ${
              mobileTab === tab
                ? "border-b-2 border-[#7170ff] text-[var(--jf-primary)]"
                : "text-[var(--jf-muted)] hover:text-[#374151]"
            }`}
          >
            {tab === "add" ? t("bld.addBlock") : t("bld.canvas")}
          </button>
        ))}
      </nav>

      <div className="grid w-full grid-cols-1 md:grid-cols-[240px_1fr_260px]">
        {/* ── left: categorized palette (drag sources) − mobile: only on the
             Add tab, desktop: always visible. Tap = add (already exists). */}
        <aside className={`border-r border-[var(--jf-border)] p-4 ${mobileTab === "add" ? "block" : "hidden"} md:block`}>
          {/* Issue #256: AI form builder — prompt input. The backend calls
              deepseek-v4.1-flash to draft a validated form config. Kept in
              template mode (controls.aiGenerate): it only drafts blocks into
              the canvas, which is exactly what a template is made of. */}
          {controls.aiGenerate && (
          <div className="mb-4 rounded-lg border border-[var(--jf-border)] bg-white p-3">
            <p className="mb-1 flex items-center gap-1 text-[11px] font-semibold uppercase tracking-wider text-[var(--jf-muted-on-tint)]">
              <span>✨</span>
              {t("bld.aiTitle")}
            </p>
            <p className="mb-2 text-[11px] leading-snug text-[var(--jf-muted-on-tint)]">{t("bld.aiHint")}</p>
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
          )}
          <p className="mb-3 text-xs font-medium uppercase tracking-wider text-[var(--jf-muted-on-tint)]">
            {t("bld.dragToAdd")}
          </p>
          {PALETTE_GROUPS.map((group) => {
            const items = group.items.filter((p) => !(mode === "response" && pricingTypes.includes(p.type)));
            if (items.length === 0) return null;
            return (
              <div key={group.key} className="mb-4">
                <p className="mb-1.5 flex items-center gap-1 text-[11px] font-semibold uppercase tracking-wider text-[var(--jf-muted-on-tint)]">
                  <span>{group.icon}</span>
                  {t(`bld.palette.${group.key}` as import("../../components/i18n").Key)}
                </p>
                <div className="grid grid-cols-2 gap-2">
                  {items.map((p) => (
                    <div
                      key={p.type}
                      draggable
                      onDragStart={(e) => paletteDragStart(e, p.type)}
                      onDragEnd={endDrag}
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

        {/* ── center: canvas (drop zone) − mobile: only on the Canvas tab with
             NO block selected; desktop always visible. max-w-lg on desktop,
             max-w-full mobile.
             #652: on mobile the canvas and the edit panel are the same slot —
             selecting a block swaps the canvas for the panel below, so a tap on
             a field goes straight to its settings. */}
        <section className={`p-4 md:p-6 ${mobileTab === "canvas" && !panelVisible ? "block" : "hidden"} md:block`}>
          <div className="mx-auto max-w-full md:max-w-lg">
            {blocks.length === 0 && (
              <div
                onDragOver={(e) => canvasDragOver(e, 0)}
                onDrop={(e) => canvasDrop(e, 0)}
                className={`rounded-xl border-2 border-dashed px-6 py-16 text-center text-sm transition-colors ${
                  dropAt === 0
                    ? "border-[#7170ff] bg-indigo-50/50 text-[var(--jf-primary-on-tint)]"
                    : "border-slate-300 text-[var(--jf-muted-on-tint)]"
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
                  onDragEnd={endDrag}
                  onDragOver={(e) => canvasDragOver(e, i + 1)}
                  onDrop={(e) => canvasDrop(e, i + 1)}
                  onClick={() => openBlockPanel(b.id)}
                  className={`cursor-grab rounded-lg border bg-white p-3 transition-colors active:cursor-grabbing ${
                    selected === b.id
                      ? "border-[#7170ff]/60 shadow-[0_0_0_1px_rgba(113,112,255,0.4)]"
                      : "border-[var(--jf-border)] hover:border-slate-300"
                  }`}
                >
                  <div className="mb-1.5 flex items-center justify-between text-[10px] uppercase tracking-wider text-[var(--jf-muted-on-tint)]">
                    <span>{typeLabel(b.type)}</span>
                    {/* #652: four 44px buttons ate ~176px of a ~350px card.
                        Reorder (the most frequent action) keeps its own small
                        icon buttons; the rarer Duplicate/Remove move into one ⋯
                        menu. Every control stops propagation so a tap on it does
                        not open the edit panel. */}
                    <span className="flex items-center gap-1">
                      <button
                        onClick={(e) => {
                          e.stopPropagation();
                          moveRow(i, -1);
                        }}
                        disabled={!canMove(blocks, i, -1)}
                        title={t("bld.moveUp")}
                        className="flex h-8 w-8 items-center justify-center rounded-md text-base text-[var(--jf-muted-on-tint)] hover:text-[#374151] active:bg-slate-100 disabled:opacity-30 disabled:hover:text-[var(--jf-muted-on-tint)] md:h-auto md:w-auto md:px-1 md:text-[10px]"
                      >
                        ↑
                      </button>
                      <button
                        onClick={(e) => {
                          e.stopPropagation();
                          moveRow(i, 1);
                        }}
                        disabled={!canMove(blocks, i, 1)}
                        title={t("bld.moveDown")}
                        className="flex h-8 w-8 items-center justify-center rounded-md text-base text-[var(--jf-muted-on-tint)] hover:text-[#374151] active:bg-slate-100 disabled:opacity-30 disabled:hover:text-[var(--jf-muted-on-tint)] md:h-auto md:w-auto md:px-1 md:text-[10px]"
                      >
                        ↓
                      </button>
                      {/* Issue #173: duplicate + remove, behind one menu (#652). */}
                      <span data-block-menu className="relative">
                        <button
                          type="button"
                          ref={menuOpen === b.id ? menuBtn : undefined}
                          onClick={(e) => {
                            e.stopPropagation();
                            setMenuOpen(menuOpen === b.id ? null : b.id);
                          }}
                          aria-label={t("bld.blockMenu")}
                          aria-haspopup="true"
                          aria-expanded={menuOpen === b.id}
                          title={t("bld.blockMenu")}
                          className="flex h-11 w-11 items-center justify-center rounded-md text-base text-[var(--jf-muted-on-tint)] hover:text-[#374151] active:bg-slate-100 aria-expanded:bg-slate-100 md:h-auto md:w-auto md:px-1 md:text-[10px]"
                        >
                          ⋯
                        </button>
                        {menuOpen === b.id && (
                          <div className="absolute right-0 top-full z-10 mt-1 w-40 rounded-md border border-[var(--jf-border)] bg-white py-1 shadow-lg">
                            <button
                              type="button"
                              onClick={(e) => {
                                e.stopPropagation();
                                setMenuOpen(null);
                                duplicateBlock(b.id);
                              }}
                              className="flex min-h-11 w-full items-center px-3 text-left text-sm normal-case tracking-normal text-[#374151] hover:bg-slate-50"
                            >
                              {t("bld.duplicate")}
                            </button>
                            <button
                              type="button"
                              onClick={(e) => {
                                e.stopPropagation();
                                setMenuOpen(null);
                                removeBlock(b.id);
                              }}
                              className="flex min-h-11 w-full items-center px-3 text-left text-sm normal-case tracking-normal text-[var(--jf-danger-text)] hover:bg-[var(--jf-danger-bg)]"
                            >
                              {t("bld.removeBlock")}
                            </button>
                          </div>
                        )}
                      </span>
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
          {loadErr && <p className="mt-4 text-center text-xs text-red-700">{loadErr}</p>}
          {/* Issue #457: save-as-template feedback. */}
          {tplMsg && <p className="mt-2 text-center text-xs text-[var(--jf-muted)]">{tplMsg}</p>}
        </section>

        {/* ── right: inspector − desktop: always visible (3-col grid, unchanged);
             mobile (#652): the canvas column's alternate face. It is shown only
             when the Canvas tab is active AND the panel is open, so tapping a
             block card on a phone opens this panel in place of the canvas, and
             the panel's own header row is the way back out. */}
        <aside className={`border-l border-[var(--jf-border)] p-4 ${mobileTab === "canvas" && panelVisible ? "block" : "hidden"} md:block`}>
          {/* #652: the exit. The old Settings tab had no back button either, so
              without this row the change would trade one trap (a tab at the
              top) for a worse one (no way out at all). Mobile only — `md:` and
              up keeps the plain three-column layout. */}
          {panelVisible && selectedBlock && (
            <div className="mb-3 flex items-center gap-2 md:hidden">
              <button
                type="button"
                onClick={closeBlockPanel}
                className="min-h-11 shrink-0 rounded-md border border-[var(--jf-border)] bg-white px-3 text-sm font-medium text-[#374151] hover:bg-slate-50"
              >
                ← {t("bld.canvas")}
              </button>
              <span className="min-w-0 flex-1 text-xs font-medium uppercase tracking-wider text-[var(--jf-muted-on-tint)]">
                {typeLabel(selectedBlock.type)}
              </span>
            </div>
          )}
          <p className="mb-3 hidden text-xs font-medium uppercase tracking-wider text-[var(--jf-muted-on-tint)] md:block">{t("bld.blockSettings")}</p>
          {/* AGENTS.md DOCUMENTATION PARITY rule 4: the inline "?" next to the
              feature it documents. It deep-links the builder guide, which is the
              page that carries "editing a block taps into its settings". */}
          <a
            href="/docs/builder"
            target="_blank"
            rel="noreferrer"
            title={t("bld.builderHelp")}
            aria-label={t("bld.builderHelp")}
            className="mb-3 ml-1 inline-flex h-6 w-6 items-center justify-center rounded-full border border-[var(--jf-border)] text-xs text-[var(--jf-muted)] hover:bg-slate-50 md:mb-2"
          >
            ?
          </a>
          {!selectedBlock && <p className="text-xs text-[var(--jf-muted-on-tint)]">{t("bld.selectBlock")}</p>}
          {selectedBlock && renderFieldSettings(selectedBlock)}


        {/* ── Form mode (issue #171) ── */}
        <div className="mt-8 rounded-lg border border-[var(--jf-border)] bg-white p-3">
          <div className="flex items-center justify-between gap-2">
            <p className="text-xs font-medium uppercase tracking-wider text-[var(--jf-muted-on-tint)]">
              {t("bld.mode")}
            </p>
            {/* The mode decides which default notification the form gets, and
                switching it re-points an untouched default (t_a58848bd). That is
                not visible from two buttons labelled Payment/Response, so the
                guide that explains it is one link away — same "?" pattern every
                sibling control uses (issue #924: title + aria-label + new tab). */}
            <a
              href="/docs/email#notifications"
              target="_blank"
              rel="noreferrer"
              title={t("bld.modeDocs")}
              aria-label={t("bld.modeDocs")}
              className="inline-flex h-5 w-5 items-center justify-center rounded-full border border-[var(--jf-border)] text-[11px] font-semibold text-[var(--jf-muted)] hover:bg-slate-50 hover:text-slate-700"
            >
              ?
            </a>
          </div>
          <div className="mt-2 grid grid-cols-2 gap-1.5">
            <button
              type="button"
              onClick={() => {
                setMode("payment");
                setDirty(true);
              }}
              className={`rounded-md border px-2 py-2 text-xs font-medium transition-colors ${
                mode === "payment"
                  ? "border-[#7170ff]/60 bg-indigo-50 text-[var(--jf-primary-on-tint)]"
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
                  ? "border-[#7170ff]/60 bg-indigo-50 text-[var(--jf-primary-on-tint)]"
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
            dedicated Form Settings view — the builder is fields-only. A
            TEMPLATE has no such view: its whole definition is this canvas, and
            Form Settings edits a `forms` row by id (not owner-scoped for
            templates), so the link is hidden in template mode. */}
        {controls.formSettings && currentId && (
          <div className="mt-8 rounded-lg border border-[var(--jf-border)] bg-white p-3">
            <p className="text-xs font-medium uppercase tracking-wider text-[var(--jf-muted-on-tint)]">
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
        </aside>
      </div>
    </main>
  );

  function paletteDragStart(e: React.DragEvent, t: BlockType) {
    dragType.current = t;
    dragIdx.current = null;
    setDragging(true);
    e.dataTransfer.effectAllowed = "copy";
    e.dataTransfer.setData("text/plain", `type:${t}`);
  }

  function blockDragStart(e: React.DragEvent, i: number) {
    dragIdx.current = i;
    dragType.current = null;
    setDragging(true);
    e.dataTransfer.effectAllowed = "move";
    e.dataTransfer.setData("text/plain", `idx:${i}`);
  }

  /* Card t_769a5cb9: ONE place where a drag stops. `dragend` fires on the SOURCE
     element, and for an ABANDONED drag (released outside the canvas) it is the
     ONLY signal there is — no `drop` runs at all, so `canvasDrop` never got to
     clear anything. Measured before this existed, with a real held drag released
     over the palette column: `dragend` was delivered, the trailing drop zone
     stayed painted at h=40 with its tint, every palette item stayed at
     `opacity: 0.6`, and `dragType`/`dragIdx` stayed SET — so the next unrelated
     drag over the canvas was taken for a block drag and dropped a block nobody
     carried. The block cards already cleared the refs here; the palette items
     had no `onDragEnd` at all. */
  function endDrag() {
    dragType.current = null;
    dragIdx.current = null;
    setDropAt(null);
    setDragging(false);
  }

  function canvasDragOver(e: React.DragEvent, i: number) {
    e.preventDefault();
    e.stopPropagation();
    // The guard below reads the REFS, not the render-time `dragging` const.
    //
    // `dragging` is `dragType.current !== null || dragIdx.current !== null`
    // evaluated during a render, and refs do not schedule one. So the
    // FIRST `dragover` after `dragstart` ran inside the previous render's
    // closure — where both refs were still null — and returned here: the
    // insertion highlight never appeared while dragging, even though the drop
    // itself worked, because `canvasDrop` reads the refs directly. Measured on a
    // production build with a real held mouse drag over the empty canvas: 35
    // `dragover` events delivered to the drop zone, 0 class mutations on it, and
    // the drop still landed. Reading the refs live is what makes the highlight
    // reachable at all; the `setDropAt` below then re-renders, which is the
    // render in which `dragging` reads correctly for the palette's dim and the
    // insertion gaps (card t_93b097c6).
    //
    // t_769a5cb9 keeps this check (the refs are the live truth for the drop)
    // and ALSO moves the painted flag to state, so the dim no longer has to wait
    // for this first `dragover` to happen at all.
    if (dragType.current === null && dragIdx.current === null) return;
    setDropAt(i);
  }

  function canvasDrop(e: React.DragEvent, i: number) {
    e.preventDefault();
    e.stopPropagation();
    const t = dragType.current;
    const from = dragIdx.current;
    endDrag();
    if (t !== null) insertAt(i, t, null);
    else if (from !== null) insertAt(i, null, from);
  }
}

/* renders a non-interactive preview of each block type */
function BlockPreview({ block, products }: { block: Block; products: Product[] }) {
  const { t } = useI18n();
  const p = products.find((x) => x.id === block.productId);
  switch (block.type) {
    case "text":
      // Card t_23289ce3: this block is a single-line text INPUT on both public
      // renderers (the builder offers placeholder / max length / default value
      // for it, settings that only exist for a control, and it is the palette's
      // only short-answer field). The canvas used to draw the value as prose —
      // "Paragraph / description" is the palette hint that misled it. The
      // sketch below is the same non-interactive one every other field uses.
      return (
        <div>
          <p className="mb-1 text-xs text-[var(--jf-muted)]">{block.value || "Text"}</p>
          <div className="flex h-8 items-center rounded-md border border-[var(--jf-border)] bg-slate-50 px-2 text-xs text-[var(--jf-muted-on-tint)]">
            {block.placeholder || ""}
          </div>
        </div>
      );
    case "html":
      return <p className="text-sm text-[#374151]">{block.value || "</>"}</p>;
    case "section":
      return <p className="text-base font-semibold text-[#17181c]">{block.value || "Section"}</p>;
    case "image":
      return block.src ? (
        // eslint-disable-next-line @next/next/no-img-element
        <img src={block.src} alt="" className="max-h-40 rounded-md border border-[var(--jf-border)] object-cover" />
      ) : (
        // #652: this used to read "Image URL →" inside a dashed, bordered box —
        // a shape that promised a text field or a link and delivered neither.
        // The URL is set in the block's own settings (the panel on mobile), so
        // the placeholder is now a flat caption naming the setting, not a
        // control-shaped box.
        <div className="flex h-24 flex-col items-center justify-center gap-1 rounded-md bg-slate-100 text-xs text-[var(--jf-muted-on-tint)]">
          <span aria-hidden className="text-base">🖼</span>
          <span>{t("bld.imageUrl")}</span>
        </div>
      );
    case "product":
      // t_5f315891: the canvas price spans must NOT use the base --jf-primary.
      // This block's card is `bg-slate-50` (#f8fafc), and #5e6ad2 on it measures
      // 4.492:1 — 0.008 under the 4.5:1 AA floor. The token was only ever
      // checked against white, where it passes at 4.700:1, which is exactly why
      // the defect hid: slate-50 is marginally darker than the surface it was
      // measured on. The on-tint token measures 7.554:1 on slate-50 and 7.903:1
      // on white, so it is safe unconditionally. Do NOT darken --jf-primary
      // itself: that shifts every other use of the token (4.700:1 on white)
      // across the whole app. Guarded, measured in a real browser, by
      // web/e2e/builder-canvas-price-contrast.spec.ts.
      return (
        <div className="rounded-md border border-[var(--jf-border)] bg-slate-50 px-3 py-2">
          <div className="flex items-center justify-between gap-2">
            <span className="flex min-w-0 items-center gap-2">
              {p?.images?.[0] ? (
                // eslint-disable-next-line @next/next/no-img-element
                <img
                  src={p.images[0]}
                  alt={p?.name || "Product"}
                  className="h-8 w-8 flex-none rounded-md object-cover"
                />
              ) : null}
              <span className="truncate text-sm">{block.value || p?.name || "Product"}</span>
            </span>
            <span className="text-sm font-medium text-[var(--jf-primary-on-tint)]">{p ? money(p.price_sen) : "—"}</span>
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
      // t_5f315891: same defect and same fix as the product block above — this
      // row carries `bg-slate-50` itself, so #5e6ad2 measured 4.492:1 here
      // before the change.
      return (
        <div className="flex items-center justify-between rounded-md border border-[var(--jf-border)] bg-slate-50 px-3 py-2">
          <span className="text-sm">{block.value || "Total"}</span>
          <span className="text-sm font-medium text-[var(--jf-primary-on-tint)]">RM0.00</span>
        </div>
      );
    case "divider":
      return <hr className="border-[var(--jf-border)]" />;
    case "checkbox":
      // #652: the fake box was a bordered 16px square — a control the merchant
      // would try to tap. It is decoration, so it must not look tappable: flat
      // grey fill, no border, and marked non-interactive for assistive tech.
      return (
        <div className="flex items-center gap-2 text-sm text-[#374151]">
          <span aria-hidden className="inline-block h-4 w-4 rounded bg-slate-300" /> {block.value}
        </div>
      );
    case "consent":
      return (
        <div className="flex items-center gap-2 text-sm text-[#374151]">
          <span aria-hidden className="inline-block h-4 w-4 rounded bg-slate-300" /> {block.value || "Consent"}
        </div>
      );
    case "select":
      return (
        <div>
          <p className="mb-1 text-xs text-[var(--jf-muted)]">
            {block.value || "Field"}
            {block.required && <span className="text-red-700"> *</span>}
          </p>
          <div className="flex h-8 items-center rounded-md border border-[var(--jf-border)] bg-slate-50 px-2 text-xs text-[var(--jf-muted-on-tint)]">
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
            {block.required && <span className="text-red-700"> *</span>}
          </p>
          <div className="space-y-1">
            {(block.choices || []).map((c, i) => (
              <div key={i} className="flex items-center gap-2 text-sm text-[#374151]">
                {/* #652: flat grey dot — the bordered ring looked like a tappable
                    radio button in a preview that has no controls at all. */}
                <span aria-hidden className="inline-block h-4 w-4 rounded-full bg-slate-300" /> {c.label}
              </div>
            ))}
          </div>
        </div>
      );
    case "hidden":
      return (
        <p className="text-xs text-[var(--jf-muted-on-tint)]">
          Hidden · {block.value || "field"} = {block.defaultValue || "…"}
        </p>
      );
    case "captcha":
      return (
        <div>
          <p className="mb-1 text-xs text-[var(--jf-muted)]">{block.value || "Captcha"}</p>
          <div className="flex h-8 items-center rounded-md border border-[var(--jf-border)] bg-slate-50 px-2 text-xs text-[var(--jf-muted-on-tint)]">
            🛡 Captcha
          </div>
        </div>
      );
    default:
      return (
        <div>
          <p className="mb-1 text-xs text-[var(--jf-muted)]">
            {block.value || "Field"}
            {block.required && <span className="text-red-700"> *</span>}
          </p>
          <div className="h-8 rounded-md border border-[var(--jf-border)] bg-slate-50" />
        </div>
      );
  }
}
