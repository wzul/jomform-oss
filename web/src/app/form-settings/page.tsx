"use client";

// Issue #177: dedicated Form Settings view (Gravity-Forms-style).
// The builder is now fields-only; this page owns everything else about a
// form: General (title/slug/status/theme/logo/embed), Confirmation
// (success/failure redirects), Restrictions (schedule + entry limit),
// Notifications (per-form receipt email, reusing the #107 pattern) and the
// per-form CHIP feed. Sales/analytics
// live on the Sales page (#126), not here and not in the builder.

import { Suspense, useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import QRCode from "qrcode";
import { Shell } from "../../components/Shell";
import { useI18n } from "../../components/i18n";
import { useSession } from "../../components/session";
import { Switch } from "../../components/Switch";
import MediaPicker from "../../components/MediaPicker";
import ProductPicker, { type PickerProduct } from "../../components/ProductPicker";
import { activeWorkspaceScope, enterWorkspace, workspaceQuery, workspaceUrl } from "../../lib/workspace";
import { defaultNotificationEvent, formEntriesAction } from "../../lib/formMode";
import { acknowledgmentBody, publishConfirmText, publishErrorText, publishOutcome } from "../../lib/publishResult";
import { apiErr } from "../../lib/apiErr";
// t_106955f6: the General Save writes the theme it READ laid over the keys this
// tab owns — the API's PATCH replaces the whole jsonb, so an owned-keys-only map
// deleted `layout`/`spacing`/`background`/`font`/`custom_css` on every save.
import { generalSaveTheme } from "../../lib/formSettingsTheme";
// t_c845d0f6: the logo FIELD preview applies the SAME rule as the media grid and
// the picker's tiles. It needs the chosen row's mime, not just its url — a code
// asset's url is on the tenant host with CORP same-origin, so an <img> here is a
// broken tile on the dashboard while the published form loads it fine.
import { extBadge, selectedNeedsBadge, type SelectedMedia } from "../../lib/mediaThumb";
// t_b3991178: the reload lookup asks the server for the ONE row behind
// `logo_url` (GET /api/rest/media?url=…). The library list is a 100-row
// newest-first window, so scanning it left an older logo — or one whose host
// changed — with no mime, i.e. the broken <img> again.
import { mediaLookupPath, mediaRowFromLookup } from "../../lib/mediaLookup";

type FormDetail = {
  id: string;
  slug: string;
  title: string;
  status: string;
  url: string;
  // "payment" (default) or "response" — a response-only form can
  // never record a sale, so it must not be offered a "Sales" action.
  mode?: string;
  config?: Record<string, unknown>;
  theme?: Record<string, unknown>;
  redirect_success?: string;
  redirect_failure?: string;
  allow_embed?: boolean;
  logo_url?: string;
  failure_message?: string;
  start_at?: string;
  end_at?: string;
  entry_limit?: number | null;
  pending_message?: string;
  expired_message?: string;
  limit_message?: string;
  chip_configured?: boolean;
  chip_brand_id?: string;
};

// Issue #193: per-form notification rule (Gravity-Forms-style).
type Notification = {
  id: string;
  form_id: string;
  name: string;
  event: string;
  to_email: string;
  subject: string;
  body: string;
  condition?: { field_id: string; op: string; value: string } | null;
  enabled: boolean;
  is_default: boolean;
};

// Issue #453: the per-form email settings are an OVERRIDE of the workspace
// email settings (#107). The boolean toggles are tri-state — null = inherit the
// workspace default; true/false = explicit per-form override. The response also
// carries the workspace settings the form inherits from and the resolved
// effective values.
type EmailSettings = {
  receipt_subject: string;
  receipt_body: string;
  receipt_enabled: boolean;
  notify_merchant: boolean;
  merchant_notify_email: string;
  html: boolean;
  receipt_enabled_override?: boolean | null;
  notify_merchant_override?: boolean | null;
  html_override?: boolean | null;
};

type FormEmailSettingsResponse = {
  workspace?: EmailSettings | null;
  effective?: EmailSettings | null;
} & EmailSettings;

// Issue #196: dispatcher-supported webhook events (dispatch2.go + purchases.go).
const WEBHOOK_EVENTS = ["sale.paid", "sale.failed", "subscription.charged"] as const;

type Tab = "general" | "menu" | "confirmation" | "restrictions" | "notifications" | "chip" | "webhooks";

// Issue #208: a per-form CHIP feed (Gravity-Forms-style). A feed with no
// condition is the default feed and always matches.
type ChipFeed = {
  id: string;
  name: string;
  brand_id: string;
  email_fallback: string;
  payment_method_whitelist?: string[];
  configured: boolean;
  condition?: { field_id: string; op: string; value: string } | null;
};

type ChipFeedDraft = {
  id: string;
  name: string;
  brand_id: string;
  secret_key: string;
  email_fallback: string;
  condition?: { field_id: string; op: string; value: string } | null;
};

// Field types that collect a buyer answer (used for the condition selector).
const FEED_FIELD_TYPES = new Set([
  "text", "textarea", "number", "email", "phone", "website", "select",
  "radio", "image-choice", "option", "shipping", "date", "time", "hidden",
]);

const FEED_OPS = ["equals", "not_equals", "contains", "gt", "lt", "not_empty"];

function feedUid() {
  return "feed_" + Math.random().toString(36).slice(2, 9);
}

// Issue #392: field types whose value should be picked from the field's
// choices (a dropdown) rather than typed as free text.
const CHOICE_FIELD_TYPES = new Set(["select", "radio", "image-choice", "option"]);

// Issue #392: a form block as used by the conditional-logic picker.
type CondBlock = { id: string; value: string; type: string; product_id?: string; choices?: { label?: string; value?: string }[] };

// Issue #392: resolve a field's user-friendly label (falling back to its type).
function condFieldLabel(b: CondBlock | undefined): string {
  if (!b) return "";
  return b.value || b.type;
}

// Issue #392: the choices available for a field (used for the value dropdown).
function condFieldChoices(b: CondBlock | undefined): { label: string; value: string }[] {
  if (!b || !Array.isArray(b.choices)) return [];
  return b.choices
    .map((c) => ({ label: c.label || c.value || "", value: c.value || c.label || "" }))
    .filter((c) => c.value);
}

// Issue #392: human-readable summary of a condition, e.g. "If Package equals Premium".
function condSummary(
  t: (k: import("../../components/i18n").Key) => string,
  field: CondBlock | undefined,
  op: string,
  value: string,
): string {
  const f = condFieldLabel(field);
  const opKey = `fs.op${op[0].toUpperCase()}${op.slice(1)}` as import("../../components/i18n").Key;
  const opLabel = t(opKey);
  if (op === "not_empty") return `${t("fs.condIf")} ${f} ${opLabel}`;
  return `${t("fs.condIf")} ${f} ${opLabel} ${value}`;
}

// Issue #392: reusable "If [field] [operator] [value]" builder. Shows a field
// dropdown (label + type), an operator dropdown, and a context-aware value
// input (choices dropdown for select/radio/image-choice fields, text otherwise).
function ConditionPicker({
  fieldId,
  op,
  value,
  fields,
  onField,
  onOp,
  onValue,
  t,
  compact,
}: {
  fieldId: string;
  op: string;
  value: string;
  fields: CondBlock[];
  onField: (id: string) => void;
  onOp: (o: string) => void;
  onValue: (v: string) => void;
  t: (k: import("../../components/i18n").Key) => string;
  compact?: boolean;
}) {
  const field = fields.find((b) => b.id === fieldId);
  const isChoice = !!field && CHOICE_FIELD_TYPES.has(field.type);
  const choices = condFieldChoices(field);
  const w = compact ? "w-40" : "w-44";
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center gap-2">
        <select
          value={fieldId}
          onChange={(e) => onField(e.target.value)}
          className={`jf-input ${w} text-xs`}
        >
          <option value="">{t("fs.condSelectField")}</option>
          {fields.map((b) => (
            <option key={b.id} value={b.id}>
              {condFieldLabel(b)} ({b.type})
            </option>
          ))}
        </select>
        <select
          value={op}
          onChange={(e) => onOp(e.target.value)}
          className="jf-input w-32 text-xs"
        >
          {FEED_OPS.map((o) => (
            <option key={o} value={o}>
              {t(`fs.op${o[0].toUpperCase()}${o.slice(1)}` as import("../../components/i18n").Key)}
            </option>
          ))}
        </select>
        {op !== "not_empty" &&
          (isChoice && choices.length > 0 ? (
            <select
              value={value}
              onChange={(e) => onValue(e.target.value)}
              className={`jf-input ${w} text-xs`}
            >
              <option value="">{t("fs.condSelectField")}</option>
              {choices.map((c) => (
                <option key={c.value} value={c.value}>
                  {c.label}
                </option>
              ))}
            </select>
          ) : (
            <input
              value={value}
              onChange={(e) => onValue(e.target.value)}
              placeholder={t("fs.confValue")}
              className={`jf-input ${w} text-xs`}
            />
          ))}
      </div>
      {fieldId && (
        <p className="text-xs text-[var(--jf-muted)]">
          {condSummary(t, field, op, value)}
        </p>
      )}
    </div>
  );
}

// Issue #453: a tri-state override control — "use workspace default" plus an
// explicit on/off. Used for the per-form email settings toggles so a form can
// override the workspace without forcing a value on every field.
function TriToggle({
  label,
  value,
  onChange,
  disabled,
  inheritLabel,
  t,
}: {
  label: string;
  value: "" | "on" | "off";
  onChange: (v: "" | "on" | "off") => void;
  disabled?: boolean;
  inheritLabel: string;
  t: (k: import("../../components/i18n").Key) => string;
}) {
  const opts: { v: "" | "on" | "off"; label: string }[] = [
    { v: "", label: inheritLabel },
    { v: "on", label: t("fs.emelOn") },
    { v: "off", label: t("fs.emelOff") },
  ];
  return (
    <div>
      <label className="text-xs font-medium text-[var(--jf-muted)]">{label}</label>
      <div className="mt-1 flex flex-wrap gap-2">
        {opts.map((o) => (
          <button
            key={o.v}
            type="button"
            disabled={disabled}
            onClick={() => onChange(o.v)}
            className={`rounded-lg border px-3 py-1.5 text-xs ${
              value === o.v
                ? "border-[var(--jf-primary)] bg-[var(--jf-primary)] text-white"
                : "border-[var(--jf-border)] text-[var(--jf-muted)] hover:text-[#374151]"
            } disabled:opacity-50`}
          >
            {o.label}
          </button>
        ))}
      </div>
    </div>
  );
}

const NOTIF_EVENTS = [
  "sale.paid",
  "sale.failed",
  "response.submitted",
  "subscription.charged",
] as const;

// Issue #394: map each notification event value to its i18n key explicitly
// (avoids fragile string manipulation that produced keys like
// `fs.notifEventSalepaid` which don't exist in the dictionary).
const NOTIF_EVENT_KEYS: Record<string, import("../../components/i18n").Key> = {
  "sale.paid": "fs.notifEventSalePaid",
  "sale.failed": "fs.notifEventSaleFailed",
  "response.submitted": "fs.notifEventResponseSubmitted",
  "subscription.charged": "fs.notifEventSubscriptionCharged",
};

function notifEventKey(ev: string): import("../../components/i18n").Key {
  return NOTIF_EVENT_KEYS[ev] ?? ("fs.notifEvent" + ev) as import("../../components/i18n").Key;
}

// Issue #179: a per-form confirmation (Gravity-Forms-style). The default
// confirmation (is_default) always exists and can be edited but not deleted.
type Confirmation = {
  id: string;
  name: string;
  type: "message" | "redirect";
  message: string;
  redirect_url: string;
  condition?: { field_id: string; op: string; value: string } | null;
  is_default?: boolean;
  position?: number;
};

const TABS: { key: Tab; label: string }[] = [
  { key: "general", label: "fs.general" },
  // Issue #497: restaurant menu is a per-form setting.
  { key: "menu", label: "fs.menu" },
  { key: "confirmation", label: "fs.confirmation" },
  { key: "restrictions", label: "fs.restrictions" },
  { key: "notifications", label: "fs.notifications" },
  { key: "chip", label: "fs.chip" },
  { key: "webhooks", label: "fs.webhooks" },
];

// Issue #453: tri-state helpers for the per-form email override toggles.
// null/undefined = inherit the workspace default (""), otherwise "on"/"off".
function triFrom(v: boolean | null | undefined): "" | "on" | "off" {
  if (v === null || v === undefined) return "";
  return v ? "on" : "off";
}

// triTo converts the UI tri-state back to the API's nullable boolean.
function triTo(v: "" | "on" | "off"): boolean | null {
  if (v === "") return null;
  return v === "on";
}

function toLocalInput(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (isNaN(d.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function fromLocalInput(v: string): string | null {
  if (!v) return null;
  const d = new Date(v);
  if (isNaN(d.getTime())) return null;
  return d.toISOString();
}

// Issue #351: the form-settings page must not depend on the active jf_workspace.
// If the form isn't found in the active workspace, probe the user's other
// workspaces (from /api/workspaces) and, when found, auto-switch jf_workspace to
// the owning workspace so the form loads regardless of the current workspace
// selection. Returns the resolved form (or null) plus the HTTP status so the
// caller can tell a 401 (unauthenticated → redirect to login) from a 404 (form
// not found / wrong workspace → clear message).
async function resolveFormAcrossWorkspaces(
  formId: string,
): Promise<{ form: FormDetail | null; status: number }> {
  const probe = async (workspace: string) => {
    const q = workspace ? `?workspace=${encodeURIComponent(workspace)}` : "";
    const r = await fetch(`/api/rest/forms/${formId}${q}`);
    if (r.ok) return { form: (await r.json()) as FormDetail, status: r.status };
    return { form: null, status: r.status };
  };

  // Fast path: the ACTIVE workspace (the session-confirmed one once the server
  // has answered — lib/workspace.ts, t_da4b2031). Reading the stored value here
  // would probe a slug the session may already have rejected.
  const active = activeWorkspaceScope();
  const first = await probe(active);
  if (first.form || first.status === 401) return first;

  // Not found in the active workspace: probe the user's other workspaces.
  try {
    const spRes = await fetch("/api/workspaces");
    if (spRes.ok) {
      const workspaces = (await spRes.json()) as { id: string; slug: string }[];
      for (const s of workspaces) {
        if (s.slug === active) continue;
        const r = await probe(s.slug);
        if (r.form) {
          // Found in another workspace — auto-switch to the owning workspace.
          // `enterWorkspace` publishes the new scope AND dispatches
          // jf:workspace-changed, so the session context re-resolves the active
          // workspace and its keyed fetches re-run (a bare localStorage write
          // would not reach the CONFIRMED scope every scoped fetch reads).
          enterWorkspace(s.slug);
          return r;
        }
        if (r.status === 401) return r;
      }
    }
  } catch {
    // Network hiccup — fall through to the not-found message.
  }
  return { form: null, status: 404 };
}

export default function FormSettingsPage() {
  return (
    <Suspense fallback={<Shell active="forms"><div className="px-6 py-8 text-sm text-[var(--jf-muted)]">…</div></Shell>}>
      <FormSettingsInner />
    </Suspense>
  );
}

function FormSettingsInner() {
  const { t } = useI18n();
  const { anon, role } = useSession();
  const router = useRouter();
  const search = useSearchParams();
  const formId = search.get("form") || "";

  const canWrite = role !== "read_only";

  const [tab, setTab] = useState<Tab>("general");
  const [form, setForm] = useState<FormDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState("");
  // The page's one message state, and its TONE. `msg` carries both outcomes —
  // every save on this page writes a confirmation on success and a failure
  // sentence when the API refuses (`apiErr(...)`, `publishErrorText(...)`, the
  // `fs.*Fail` keys) — so the tone travels WITH the text, the rule /settings
  // established (card t_ad2af554). Before this, "Saved ✓" and "Failed" were the
  // same indigo 12px line with no fill and no icon: a merchant whose form
  // settings were NOT written saw nothing that read as a failure
  // (card t_c88c854d). Painted from this state alone, never from the sentence,
  // and never left stale — the source guard
  // (src/lib/actionFailureTone.test.ts) enforces that on every writer below.
  const [msg, setMsg] = useState("");
  const [msgOk, setMsgOk] = useState(true);
  const showMsgOk = useCallback((text: string) => {
    setMsgOk(true);
    setMsg(text);
  }, []);
  const showMsgFail = useCallback((text: string) => {
    setMsgOk(false);
    setMsg(text);
  }, []);

  // Issue #438: QR ordering flow — QR generation for the form's public URL.
  const [qrTable, setQrTable] = useState("");
  const [qrDataUrl, setQrDataUrl] = useState("");
  const [qrTarget, setQrTarget] = useState("");
  const [qrBusy, setQrBusy] = useState(false);

  // General
  const [title, setTitle] = useState("");
  const [slug, setSlug] = useState("");
  const [status, setStatus] = useState<"draft" | "private" | "published" | "unlisted">("draft");
  const [accessPw, setAccessPw] = useState("");
  const [fsTheme, setFsTheme] = useState("#5e6ad2");
  const [fsLogo, setFsLogo] = useState("");
  // t_c845d0f6: the mime of the CHOSEN media row, carried beside the url so the
  // preview can apply the shared code-asset rule. `url` is kept with it: the
  // value is only used while it still describes the current `fsLogo`, so a
  // late-arriving lookup can never label a logo the merchant has since replaced.
  const [fsLogoMedia, setFsLogoMedia] = useState<(SelectedMedia & { url: string }) | null>(null);
  // TRUE while a reloaded logo's mime is still being resolved from the library.
  // The preview paints a same-size placeholder in that window instead of the
  // <img> it is about to replace: without it an SVG logo flashes the broken-image
  // icon it showed permanently before this fix, and a DOM read a few ms after
  // load could still catch the broken frame.
  const [fsLogoLookupDone, setFsLogoLookupDone] = useState(true);
  // Issue #501: visible inline error for a rejected (non-https) logo URL, so
  // the merchant sees it at the field instead of getting a silently broken
  // image on the buyer's page.
  const [fsLogoErr, setFsLogoErr] = useState("");
  // Issue #391: WordPress-style media picker for the logo field.
  const [logoPickerOpen, setLogoPickerOpen] = useState(false);
  const [fsEmbed, setFsEmbed] = useState(true);
  // Issue #388: power-user custom JS (owner-gated + scanned server-side).
  const [fsCustomJS, setFsCustomJS] = useState("");
  // Issue #471: product image size + layout on the buyer's form.
  const [fsImgSize, setFsImgSize] = useState<"small" | "medium" | "large" | "hidden">("small");
  const [fsImgLayout, setFsImgLayout] = useState<"thumbnail-left" | "image-top">("thumbnail-left");

  // Issue #497: restaurant menu — a per-form setting. The menu config lives on
  // the form (forms.config.menu) and the menu is served at this form's own
  // public URL. `menuProducts` holds the products THIS form references (from
  // its own product blocks), so the category/best-seller pickers and the live
  // preview can resolve stored IDs to names without leaking account products.
  const [menuEnabled, setMenuEnabled] = useState(false);
  const [menuCategories, setMenuCategories] = useState<{ name: string; product_ids: string[] }[]>([]);
  const [menuBestSeller, setMenuBestSeller] = useState("");
  const [menuOrderFormSlug, setMenuOrderFormSlug] = useState("");
  const [menuProducts, setMenuProducts] = useState<PickerProduct[]>([]);
  // The workspace's published forms, so the menu's optional "order form"
  // selector lists real forms by title (not a raw slug).
  const [menuForms, setMenuForms] = useState<{ id: string; slug: string; title: string }[]>([]);
  const [menuPickerOpen, setMenuPickerOpen] = useState(false);
  const [menuPickerCatIndex, setMenuPickerCatIndex] = useState(-1); // -1 = best-seller picker
  const [menuSaving, setMenuSaving] = useState(false);

  // Confirmation
  const [fsRedS, setFsRedS] = useState("");
  const [fsRedF, setFsRedF] = useState("");
  // Issue #393: per-form payment-failed message (HTML, sanitized on render).
  const [fsFailMsg, setFsFailMsg] = useState("");
  // Issue #179: per-form confirmations (Gravity-Forms-style).
  const [confs, setConfs] = useState<Confirmation[]>([]);

  // Restrictions
  const [rsStart, setRsStart] = useState("");
  const [rsEnd, setRsEnd] = useState("");
  const [rsLimit, setRsLimit] = useState("");
  const [rsPending, setRsPending] = useState("");
  const [rsExpired, setRsExpired] = useState("");
  const [rsLimitMsg, setRsLimitMsg] = useState("");

  // Notifications (issue #193: per-form notification rules)
  const [notifs, setNotifs] = useState<Notification[]>([]);
  const [editing, setEditing] = useState<Notification | null>(null);
  const [isNew, setIsNew] = useState(false);
  // Editor fields
  const [edName, setEdName] = useState("");
  const [edEvent, setEdEvent] = useState<string>("");
  const [edTo, setEdTo] = useState("");
  const [edSubject, setEdSubject] = useState("");
  const [edBody, setEdBody] = useState("");
  const [edEnabled, setEdEnabled] = useState(true);
  const [edHasCond, setEdHasCond] = useState(false);
  const [edCondField, setEdCondField] = useState("");
  const [edCondOp, setEdCondOp] = useState("equals");
  const [edCondValue, setEdCondValue] = useState("");

  // CHIP feed
  const [ovBrand, setOvBrand] = useState("");
  const [ovSecret, setOvSecret] = useState("");
  const [ovFallback, setOvFallback] = useState("");
  const [chipConfigured, setChipConfigured] = useState(false);

  // Issue #196: per-form webhooks
  const [whUrl, setWhUrl] = useState("");
  const [whSecret, setWhSecret] = useState("");
  const [whEvents, setWhEvents] = useState<string[]>([...WEBHOOK_EVENTS]);
  const [formWebhooks, setFormWebhooks] = useState<{ id: string; url: string; events: string[] }[]>([]);
  // Issue #208: multiple per-form CHIP feeds with conditional logic.
  const [feeds, setFeeds] = useState<ChipFeedDraft[]>([]);
  const [formBlocks, setFormBlocks] = useState<CondBlock[]>([]);

  // Per-form email settings override (issue #177, extended by #453). The
  // booleans are tri-state: "" = inherit the workspace default, "on"/"off" =
  // explicit per-form override.
  const [emelSubject, setEmelSubject] = useState("");
  const [emelBody, setEmelBody] = useState("");
  const [emelReceiptEnabled, setEmelReceiptEnabled] = useState<"" | "on" | "off">("");
  const [emelNotifyMerchant, setEmelNotifyMerchant] = useState<"" | "on" | "off">("");
  const [emelMerchantEmail, setEmelMerchantEmail] = useState("");
  const [emelHtml, setEmelHtml] = useState<"" | "on" | "off">("");
  // The workspace settings this form inherits from + the resolved effective
  // values (issue #453), so the UI can show "use workspace default".
  const [emelWorkspace, setEmelWorkspace] = useState<EmailSettings | null>(null);
  const [emelEffective, setEmelEffective] = useState<EmailSettings | null>(null);
  const [emelSaving, setEmelSaving] = useState(false);

  const load = useCallback(async () => {
    if (!formId) return;
    setLoading(true);
    setErr("");
    try {
      // Issue #351: resolve the form across the user's spaces (not just the
      // active jf_workspace). 401 → redirect to login; 404 → clear "not found /
      // wrong space" message instead of a generic load error.
      const { form: d, status } = await resolveFormAcrossWorkspaces(formId);
      if (!d) {
        if (status === 401) {
          router.replace("/login");
          return;
        }
        setErr(t("fs.notFound"));
        setLoading(false);
        return;
      }
      setForm(d);
      setTitle(d.title || "");
      setSlug(d.slug || "");
      setStatus(d.status === "private" ? "private" : d.status === "published" ? "published" : d.status === "unlisted" ? "unlisted" : "draft");
      setFsTheme((d.theme?.primary as string) || "#5e6ad2");
      setFsLogo(d.logo_url || "");
      // t_c845d0f6: after a RELOAD the page has only `logo_url` — the picker's
      // callback that carries the mime has not run. Ask the server for that ONE
      // row (t_b3991178), so an SVG logo previews as an extension badge instead
      // of a broken <img> on a tenant-host URL (HTTP 200, CORP same-origin,
      // nothing errors). Best-effort by design: a url that is not a library row
      // (a pasted external link) or a failed read leaves the mime unknown and
      // the preview renders exactly as it did before this fix.
      setFsLogoMedia(null);
      setFsLogoLookupDone(!d.logo_url);
      if (d.logo_url) {
        const logoUrl = d.logo_url;
        // t_b3991178: ask the server for THIS ONE row instead of scanning the
        // newest page of the library. The page only holds `logo_url`, and the
        // library read is a 100-row newest-first window, so a logo older than
        // that window — or one whose advertised host changed since it was
        // picked — matched no row, left the mime unknown, and painted the
        // broken <img> this whole class of bug is about. The server resolves
        // the url to the object key, so neither limitation applies.
        fetch(workspaceUrl(mediaLookupPath(logoUrl)))
          .then((r) => (r.ok ? r.json() : null))
          .then((md) => {
            const hit = mediaRowFromLookup(md);
            if (hit) {
              // Never overwrite a row a PICK already delivered while this read
              // was in flight — the pick is the newer information.
              setFsLogoMedia((prev) => prev ?? { url: logoUrl, ...hit });
            }
          })
          .catch(() => {
            // no mime: the preview keeps its pre-fix <img>, which is the right
            // answer for the external-url case and a no-op for this one.
          })
          .finally(() => setFsLogoLookupDone(true));
      }
      setFsEmbed(d.allow_embed !== false);
      // Issue #388: hydrate the power-user custom JS from the theme.
      setFsCustomJS((d.theme?.custom_js as string) || "");
      // Issue #471: hydrate the product image size + layout from the theme.
      setFsImgSize(
        d.theme?.product_image_size === "medium" || d.theme?.product_image_size === "large" || d.theme?.product_image_size === "hidden"
          ? (d.theme.product_image_size as "medium" | "large" | "hidden")
          : "small",
      );
      setFsImgLayout(d.theme?.product_image_layout === "image-top" ? "image-top" : "thumbnail-left");
      setFsRedS(d.redirect_success || "");
      setFsRedF(d.redirect_failure || "");
      setFsFailMsg(d.failure_message || "");
      setRsStart(toLocalInput(d.start_at));
      setRsEnd(toLocalInput(d.end_at));
      setRsLimit(d.entry_limit != null ? String(d.entry_limit) : "");
      setRsPending(d.pending_message || "");
      setRsExpired(d.expired_message || "");
      setRsLimitMsg(d.limit_message || "");
      setChipConfigured(!!d.chip_configured);
      setOvBrand(d.chip_brand_id || "");
      // Issue #208: hydrate the form's CHIP feeds + the field blocks used by
      // the conditional-logic selector.
      const blocks = (d.config?.blocks as CondBlock[] | undefined) || [];
      setFormBlocks(blocks.filter((b) => FEED_FIELD_TYPES.has(b.type)));
      const fr = await fetch(`/api/rest/forms/${formId}/chip-feeds${workspaceQuery()}`);
      if (fr.ok) {
        const fd = (await fr.json()) as { feeds: ChipFeed[] | null };
        // Issue: chip-feeds returns {"feeds":null} when none configured —
        // guard so a null list doesn't throw and trip the loadErr catch.
        setFeeds(
          (fd.feeds || []).map((f) => ({
            id: f.id,
            name: f.name,
            brand_id: f.brand_id,
            secret_key: "",
            email_fallback: f.email_fallback,
            condition: f.condition || null,
          })),
        );
      }
      // Load per-form email settings (issue #177/#453).
      const er = await fetch(`/api/rest/forms/${formId}/email-settings${workspaceQuery()}`);
      if (er.ok) {
        const es = (await er.json()) as FormEmailSettingsResponse;
        setEmelSubject(es.receipt_subject || "");
        setEmelBody(es.receipt_body || "");
        setEmelReceiptEnabled(triFrom(es.receipt_enabled_override));
        setEmelNotifyMerchant(triFrom(es.notify_merchant_override));
        setEmelMerchantEmail(es.merchant_notify_email || "");
        setEmelHtml(triFrom(es.html_override));
        setEmelWorkspace(es.workspace || null);
        setEmelEffective(es.effective || null);
      }
      // Load per-form notification rules (issue #193).
      const nr = await fetch(`/api/rest/forms/${formId}/notifications${workspaceQuery()}`);
      if (nr.ok) {
        const list = (await nr.json()) as Notification[];
        // The endpoint answers an ARRAY (api/internal/httpapi/rest_form_notifications.go
        // writes `list`, and `[]` when the store is not wired). A 200 body of any
        // other shape — a proxy's error object, a stale cache entry — used to be
        // stored as-is, and `notifs.map` in the render took the WHOLE page down
        // with `#__next_error__` (no heading, no title, no lang: two more serious
        // axe nodes on a page that had rendered nothing at all). The sibling
        // webhooks load below has carried `Array.isArray(...)` for exactly this
        // reason; the same guard belongs here.
        setNotifs(Array.isArray(list) ? list : []);
      }
      // Issue #196: load this form's webhooks (form_id filter).
      const sp = activeWorkspaceScope();
      const wr = await fetch(`/api/rest/webhooks?form_id=${encodeURIComponent(formId)}${sp ? `&workspace=${encodeURIComponent(sp)}` : ""}`);
      if (wr.ok) {
        const whs = (await wr.json()) as { id: string; url: string; events: string[] }[];
        setFormWebhooks(Array.isArray(whs) ? whs : []);
      }
      // Issue #179: load per-form confirmations (default always present).
      const cr = await fetch(`/api/rest/forms/${formId}/confirmations${workspaceQuery()}`);
      if (cr.ok) {
        const cd = (await cr.json()) as { confirmations: Confirmation[] };
        setConfs(cd.confirmations || []);
      }
      // Issue #497: the form's restaurant-menu config (a per-form setting).
      const mr = await fetch(`/api/rest/forms/${formId}/menu${workspaceQuery()}`);
      if (mr.ok) {
        const md = (await mr.json()) as {
          enabled?: boolean;
          categories?: { name: string; product_ids: string[] }[];
          best_seller?: string;
          order_form_slug?: string;
        };
        setMenuEnabled(!!md.enabled);
        setMenuCategories(Array.isArray(md.categories) ? md.categories : []);
        setMenuBestSeller(md.best_seller || "");
        setMenuOrderFormSlug(md.order_form_slug || "");
      }
      // Issue #497: the menu's products are the ones THIS form references via
      // its product blocks — resolved to names/thumbnails/prices from the
      // workspace's active products. Account products the form does not
      // reference are never offered (that was the #497 leak).
      const referenced = ((d.config?.blocks as CondBlock[] | undefined) || [])
        .filter((b) => b.type === "product" && b.product_id)
        .map((b) => b.product_id as string);
      if (referenced.length > 0) {
        const pr = await fetch(`/api/rest/products${workspaceQuery()}`);
        if (pr.ok) {
          const all = (await pr.json()) as PickerProduct[];
          const byId = new Map(all.filter((p) => p.active !== false).map((p) => [p.id, p]));
          setMenuProducts(referenced.map((id) => byId.get(id)).filter((p): p is PickerProduct => !!p));
        }
      } else {
        setMenuProducts([]);
      }
      // The workspace's published forms for the optional order-form selector.
      const formsRes = await fetch(`/api/rest/forms${workspaceQuery()}`);
      if (formsRes.ok) {
        const fd = (await formsRes.json()) as { id: string; slug: string; title: string; status: string }[];
        setMenuForms(
          Array.isArray(fd)
            ? fd
                .filter((x) => x.slug && (x.status === "published" || x.status === "unlisted"))
                .map((x) => ({ id: x.id, slug: x.slug, title: x.title }))
            : []
        );
      }
    } catch {
      setErr(t("fs.loadErr"));
    } finally {
      setLoading(false);
    }
  }, [formId, t, router]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (anon) router.replace("/landing");
  }, [anon, router]);

  // Issue #438: QR ordering flow — generate a QR for the form's public URL,
  // optionally encoding a ?table=N query param so the order carries the table.
  async function generateQR() {
    if (!form?.url) return;
    setQrBusy(true);
    setQrDataUrl("");
    setQrTarget("");
    try {
      const target = qrTable.trim()
        ? `${form.url}${form.url.includes("?") ? "&" : "?"}table=${encodeURIComponent(qrTable.trim())}`
        : form.url;
      const dataUrl = await QRCode.toDataURL(target, { width: 320, margin: 1 });
      setQrDataUrl(dataUrl);
      setQrTarget(target);
    } catch {
      setErr(t("fs.qrError"));
    } finally {
      setQrBusy(false);
    }
  }

  async function saveGeneral() {
    setMsg("");
    setFsLogoErr("");
    const id = formId;
    if (!id) return;
    // Card t_1463b71e: the General tab RENDERS the logo picker and the "Allow
    // embedding" toggle, so this Save persists them — a visible change must
    // never be discarded by the button next to it. The https rule is checked
    // BEFORE the PATCH so a refused logo does not leave the title/slug/status
    // half-written.
    if (fsLogo.trim() && !fsLogo.trim().startsWith("https://")) {
      setFsLogoErr(t("url.notHttps"));
      showMsgFail(t("url.notHttps"));
      return;
    }
    // This tab owns four theme keys: Issue #388's power-user custom JS (the
    // server scans it — deterministic code-asset scan — and rejects obviously
    // malicious JS before it is ever served on the buyer's page), Issue #471's
    // product image size + layout (the server validates these against the same
    // allowlist and the renderer applies them, incl. the matching media variant
    // per size), and the primary colour.
    //
    // Card t_106955f6: the payload is those four laid over the theme the page
    // READ, never those four alone. The API's PATCH REPLACES the whole `theme`
    // jsonb when the key is present, so a map built only from this tab's keys
    // silently deleted every other one (`layout`, `spacing`, `background`,
    // `text`, `card_background`, `card_border_radius`, `font_family`,
    // `custom_css`) — and the block renderer emits its layout/spacing CSS only
    // when those keys exist, so one click on this button re-styled the buyer's
    // page (measured on prod: `layout:"centered"`/`spacing:"comfortable"`
    // vanished from a form that had both). `custom_js` is the one owned key that
    // can be cleared, which generalSaveTheme does by dropping it.
    const theme = generalSaveTheme(form?.theme, {
      primary: fsTheme,
      productImageSize: fsImgSize,
      productImageLayout: fsImgLayout,
      customJS: fsCustomJS,
    });
    // logo_url and allow_embed are NOT fields of this PATCH (its body is
    // title|slug|config|theme) — they belong to PUT /forms/{id}/settings and
    // are written below, by the same function the Confirmation tab's Save uses.
    const payload = { slug: slug.toLowerCase().replace(/[^a-z0-9-]/g, "-"), title, theme };
    const r = await fetch(`/api/rest/forms/${id}${workspaceQuery()}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    if (!r.ok) {
      // A rejected save must be VISIBLE with its reason, not a generic
      // "Failed" under a form whose URL quietly did not move. The two
      // user-actionable cases the slug write can return are a collision with
      // another form's live URL (409, code=form.slugInUse) and a slug that is
      // not a usable URL segment (400, code=form.slugInvalid); apiErr resolves
      // the machine code through the dashboard dictionary, so the merchant
      // reads the reason in their OWN language. The server's `error` prose is
      // never printed — it is a Malay literal and the API cannot know the
      // dashboard's language (issue #6177). An unknown or absent code falls
      // back to the page's own localized sentence, never to the prose. The
      // reported defect was the opposite of this: the endpoint returned 200 for
      // a payload it ignored, so the page showed "Saved ✓" while the form kept
      // its old slug.
      const d = await r.json().catch(() => null);
      showMsgFail(apiErr(d, t("prof.failed"), t));
      return;
    }
    // Adopt the theme this save just landed (card t_106955f6). The `load()` at
    // the end of this function re-reads the row, but until it resolves
    // `form.theme` is the PRE-save map — and a second click in that window would
    // rebuild the payload from stale page state, resurrecting a `custom_js` the
    // merchant had just cleared. The PATCH above already committed this write,
    // so adopting it is reading back, not guessing.
    setForm((prev) => (prev ? { ...prev, theme } : prev));
    // Apply status (draft | private | published).
    const statusBody: { status: string; password?: string; acknowledge?: boolean } = { status };
    if (status === "private") statusBody.password = accessPw;
    const sr = await fetch(`/api/rest/forms/${id}/status${workspaceQuery()}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(statusBody),
    });
    if (!sr.ok) {
      // Onboarding gap: making a payment form with nothing to sell live is
      // refused once (409, code=no_products) instead of happening silently.
      // Ask, and on confirm re-send the SAME status acknowledged — the guard is
      // a transition rule, so a form that is already live never asks again.
      const d = await sr.json().catch(() => ({}));
      const outcome = publishOutcome({ ok: false, status: sr.status, body: d }, "");
      if (outcome.kind !== "needs_ack") {
        showMsgFail(publishErrorText(outcome, t("prof.failed"), t));
        return;
      }
      // The refusal's text is localized (the API's own prose for it is Malay and
      // the API never receives the caller's language) — see lib/publishResult.ts.
      if (!window.confirm(publishConfirmText(outcome, t))) return;
      const retry = await fetch(`/api/rest/forms/${id}/status${workspaceQuery()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ ...statusBody, ...acknowledgmentBody() }),
      });
      if (!retry.ok) {
        const rd = await retry.json().catch(() => ({}));
        const ro = publishOutcome({ ok: false, status: retry.status, body: rd }, "");
        showMsgFail(publishErrorText(ro, t("prof.failed"), t));
        return;
      }
    }
    // Card t_1463b71e: the logo + embed toggle this tab renders. Written LAST
    // so a refusal here is reported, and so the reload below can only run once
    // every field on this tab has actually landed. No reload on failure on
    // purpose: discarding the merchant's input is exactly the symptom.
    if (!(await putFormDisplaySettings())) return;
    showMsgOk(t("fs.saved"));
    void load();
  }

  // Issue #497: save the form's restaurant-menu config. The menu is a per-form
  // setting (forms.config.menu); enabling it makes THIS form's own URL render
  // the menu page. Disabling clears the config so the form renders normally.
  async function saveMenu() {
    setMsg("");
    if (!formId) return;
    setMenuSaving(true);
    try {
      const r = await fetch(`/api/rest/forms/${formId}/menu${workspaceQuery()}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          enabled: menuEnabled,
          categories: menuCategories,
          best_seller: menuBestSeller,
          order_form_slug: menuOrderFormSlug,
        }),
      });
      if (r.ok) showMsgOk(t("fs.menuSaved"));
      else showMsgFail(t("prof.failed"));
    } finally {
      setMenuSaving(false);
    }
  }

  // Issue #497: resolve a stored product ID to the product it points at.
  function menuProductByID(id: string): PickerProduct | undefined {
    return menuProducts.find((p) => p.id === id);
  }

  // Issue #497: the live menu preview — sections in order, with the picked
  // items. Mirrors the renderer: configured categories first (only the ones
  // that resolve to a product this form references), then an "uncategorised"
  // section for the form's referenced products not placed in any category.
  function menuPreviewSections(): { name: string; ids: string[] }[] {
    const placed = new Set<string>();
    const sections: { name: string; ids: string[] }[] = [];
    for (const c of menuCategories) {
      const ids = (c.product_ids || []).filter((id) => menuProductByID(id));
      for (const id of ids) placed.add(id);
      if (ids.length > 0) sections.push({ name: c.name, ids });
    }
    const rest = menuProducts.filter((p) => !placed.has(p.id)).map((p) => p.id);
    if (rest.length > 0) sections.push({ name: t("set.menuPreviewOther"), ids: rest });
    return sections;
  }

  // Card t_1463b71e: ONE writer for the per-form display settings (logo, embed
  // toggle, redirects, payment-failed message).
  //
  // The page had TWO saves over this one endpoint and they drifted: the logo
  // picker and the "Allow embedding" toggle are rendered on the GENERAL tab,
  // whose Save sent only {slug,title,theme}, so both changes were dropped and
  // the following reload removed the badge from the preview — while the
  // merchant read "Saved ✓". Only the CONFIRMATION tab's Save wrote them, and
  // nothing said so. Every Save that owns one of these fields now goes through
  // here, so a field cannot be rendered under a button that never writes it.
  //
  // A failure is reported in the page banner as well as at the logo field: the
  // field error alone was invisible from the Confirmation tab (the logo input
  // lives on the General tab), so a rejected save there showed nothing at all.
  async function putFormDisplaySettings(): Promise<boolean> {
    if (!formId) return false;
    setFsLogoErr("");
    // Issue #501: catch a non-https logo before the round-trip so the
    // merchant sees the problem at the field, and surface the server's
    // message if it still rejects the value (the server is the authority).
    if (fsLogo.trim() && !fsLogo.trim().startsWith("https://")) {
      setFsLogoErr(t("url.notHttps"));
      showMsgFail(t("url.notHttps"));
      return false;
    }
    const r = await fetch(`/api/rest/forms/${formId}/settings${workspaceQuery()}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      // allow_embed is *bool on the handler and defaults to TRUE when omitted,
      // so the toggle is always sent explicitly — a body carrying only
      // logo_url would switch embedding back on behind the merchant's back.
      body: JSON.stringify({
        redirect_success: fsRedS, redirect_failure: fsRedF,
        allow_embed: fsEmbed, logo_url: fsLogo, failure_message: fsFailMsg,
      }),
    });
    if (!r.ok) {
      const d = await r.json().catch(() => ({}));
      const reason = apiErr(d, t("prof.failed"), t);
      setFsLogoErr(reason);
      showMsgFail(reason);
      return false;
    }
    return true;
  }

  async function saveConfirmation() {
    setMsg("");
    if (!formId) return;
    if (!(await putFormDisplaySettings())) return;
    showMsgOk(t("fs.saved"));
  }

  // Issue #179: save the form's confirmations. The default confirmation is
  // always preserved server-side (cannot be deleted).
  async function saveConfirmations() {
    setMsg("");
    if (!formId) return;
    const r = await fetch(`/api/rest/forms/${formId}/confirmations${workspaceQuery()}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ confirmations: confs }),
    });
    if (r.ok) showMsgOk(t("fs.saved"));
    else showMsgFail(t("prof.failed"));
    if (r.ok) void load();
  }

  function updateConf(i: number, patch: Partial<Confirmation>) {
    setConfs((prev) => prev.map((c, idx) => (idx === i ? { ...c, ...patch } : c)));
  }

  function addConfirmation() {
    setConfs((prev) => [
      ...prev,
      { id: "", name: "", type: "message", message: "", redirect_url: "", condition: null },
    ]);
  }

  function removeConfirmation(i: number) {
    setConfs((prev) => prev.filter((c, idx) => idx !== i || c.is_default));
  }

  async function saveRestrictions() {
    setMsg("");
    if (!formId) return;
    const limit = rsLimit.trim() === "" ? null : Math.max(0, parseInt(rsLimit, 10) || 0);
    const r = await fetch(`/api/rest/forms/${formId}/restrictions${workspaceQuery()}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        start_at: fromLocalInput(rsStart),
        end_at: fromLocalInput(rsEnd),
        entry_limit: limit,
        pending_message: rsPending,
        expired_message: rsExpired,
        limit_message: rsLimitMsg,
      }),
    });
    if (r.ok) showMsgOk(t("fs.saved"));
    else showMsgFail(t("prof.failed"));
  }

  function openNewNotification() {
    setEditing(null);
    setIsNew(true);
    setEdName("");
    // The event a NEW rule starts on follows the form's mode, because the two
    // events are not interchangeable: a response-mode form records submissions
    // and never a sale, so a rule defaulting to sale.paid would never fire.
    // Same rule the API applies.
    setEdEvent(defaultNotificationEvent(form));
    setEdTo("{admin_email}");
    setEdSubject("");
    setEdBody("");
    setEdEnabled(true);
    setEdHasCond(false);
    setEdCondField("");
    setEdCondOp("equals");
    setEdCondValue("");
  }

  function openEditNotification(n: Notification) {
    setEditing(n);
    setIsNew(false);
    setEdName(n.name || "");
    setEdEvent(n.event || "sale.paid");
    setEdTo(n.to_email || "");
    setEdSubject(n.subject || "");
    setEdBody(n.body || "");
    setEdEnabled(n.enabled !== false);
    setEdHasCond(!!n.condition);
    setEdCondField(n.condition?.field_id || "");
    setEdCondOp(n.condition?.op || "equals");
    setEdCondValue(n.condition?.value || "");
  }

  function closeEditor() {
    setEditing(null);
    setIsNew(false);
  }

  async function saveNotification() {
    setMsg("");
    if (!formId) return;
    const payload = {
      name: edName,
      event: edEvent,
      to_email: edTo,
      subject: edSubject,
      body: edBody,
      enabled: edEnabled,
      condition: edHasCond && edCondField ? { field_id: edCondField, op: edCondOp, value: edCondValue } : null,
    };
    const url = isNew
      ? `/api/rest/forms/${formId}/notifications${workspaceQuery()}`
      : `/api/rest/forms/${formId}/notifications/${editing?.id}${workspaceQuery()}`;
    const r = await fetch(url, {
      method: isNew ? "POST" : "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    if (!r.ok) {
      showMsgFail(t("fs.notifFail"));
      return;
    }
    showMsgOk(t("fs.notifSaved"));
    closeEditor();
    void load();
  }

  async function deleteNotification(n: Notification) {
    setMsg("");
    if (!formId) return;
    const r = await fetch(`/api/rest/forms/${formId}/notifications/${n.id}${workspaceQuery()}`, {
      method: "DELETE",
    });
    if (r.ok) showMsgOk(t("fs.notifDeleted"));
    else showMsgFail(t("fs.notifFail"));
    if (r.ok) {
      if (editing?.id === n.id) closeEditor();
      void load();
    }
  }

  async function saveChipOverride() {
    setMsg("");
    if (!formId || !ovBrand || !ovSecret) return;
    const r = await fetch(`/api/rest/forms/${formId}/chip-override${workspaceQuery()}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        brand_id: ovBrand,
        secret_key: ovSecret,
        email_fallback: ovFallback,
      }),
    });
    if (r.ok) showMsgOk(t("fs.chipSaved"));
    else showMsgFail(t("fs.chipFail"));
    if (r.ok) {
      setOvSecret("");
      setChipConfigured(true);
    }
  }

  async function clearChipOverride() {
    setMsg("");
    if (!formId) return;
    const r = await fetch(`/api/rest/forms/${formId}/chip-override${workspaceQuery()}`, { method: "DELETE" });
    if (r.ok) showMsgOk(t("fs.chipCleared"));
    else showMsgFail(t("prof.failed"));
    if (r.ok) {
      setOvBrand("");
      setOvSecret("");
      setOvFallback("");
      setChipConfigured(false);
    }
  }

  // Issue #196: per-form webhook add/delete.
  function toggleWhEvent(ev: string) {
    setWhEvents((prev) => (prev.includes(ev) ? prev.filter((x) => x !== ev) : [...prev, ev]));
  }

  async function addFormWebhook(e: React.FormEvent) {
    e.preventDefault();
    setMsg("");
    if (!formId) return;
    const sp = activeWorkspaceScope();
    const r = await fetch(`/api/rest/webhooks${sp ? `?workspace=${encodeURIComponent(sp)}` : ""}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ url: whUrl, secret: whSecret, events: whEvents, form_id: formId }),
    });
    if (r.ok) {
      setWhUrl("");
      setWhSecret("");
      setWhEvents([...WEBHOOK_EVENTS]);
      showMsgOk(t("fs.webhookAdded"));
      void load();
    } else {
      showMsgFail(t("fs.webhookFail"));
    }
  }

  async function deleteFormWebhook(id: string) {
    setMsg("");
    const sp = activeWorkspaceScope();
    const r = await fetch(`/api/rest/webhooks/${id}${sp ? `?workspace=${encodeURIComponent(sp)}` : ""}`, { method: "DELETE" });
    if (r.ok) showMsgOk(t("fs.webhookDeleted"));
    else showMsgFail(t("prof.failed"));
    if (r.ok) void load();
  }

  // Issue #208: save the form's CHIP feeds. A blank secret_key on an existing
  // feed keeps the stored key server-side.
  async function saveChipFeeds() {
    setMsg("");
    if (!formId) return;
    const r = await fetch(`/api/rest/forms/${formId}/chip-feeds${workspaceQuery()}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        feeds: feeds.map((f) => ({
          id: f.id,
          name: f.name,
          brand_id: f.brand_id,
          secret_key: f.secret_key,
          email_fallback: f.email_fallback,
          condition: f.condition || null,
        })),
      }),
    });
    if (r.ok) showMsgOk(t("fs.chipFeedsSaved"));
    else showMsgFail(t("fs.chipFeedsFail"));
    if (r.ok) {
      // Clear the secret inputs so a re-save doesn't re-encrypt a stale key.
      setFeeds((prev) => prev.map((f) => ({ ...f, secret_key: "" })));
    }
  }

  function addChipFeed() {
    setFeeds((prev) => [
      ...prev,
      { id: feedUid(), name: "", brand_id: "", secret_key: "", email_fallback: "", condition: null },
    ]);
  }

  // Issue #453: save the per-form email override. Tri-state toggles ("") are
  // sent as null so the form inherits the workspace setting for that field.
  async function saveFormEmailSettings() {
    if (!formId) return;
    setMsg("");
    setErr("");
    setEmelSaving(true);
    try {
      const r = await fetch(`/api/rest/forms/${formId}/email-settings${workspaceQuery()}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          receipt_subject: emelSubject,
          receipt_body: emelBody,
          merchant_notify_email: emelMerchantEmail,
          receipt_enabled_override: triTo(emelReceiptEnabled),
          notify_merchant_override: triTo(emelNotifyMerchant),
          html_override: triTo(emelHtml),
        }),
      });
      if (r.ok) {
        showMsgOk(t("fs.emelSaved"));
        await load();
      } else {
        const d = await r.json().catch(() => ({}));
        setErr(apiErr(d, t("fs.emelFail"), t));
      }
    } finally {
      setEmelSaving(false);
    }
  }

  // Issue #453: clear every per-form override so the form inherits the
  // workspace email settings again.
  async function clearFormEmailOverrides() {
    if (!formId) return;
    setMsg("");
    setErr("");
    setEmelSaving(true);
    try {
      const r = await fetch(`/api/rest/forms/${formId}/email-settings${workspaceQuery()}`, {
        method: "DELETE",
      });
      if (r.ok) {
        showMsgOk(t("fs.emelCleared"));
        await load();
      } else {
        const d = await r.json().catch(() => ({}));
        setErr(apiErr(d, t("fs.emelFail"), t));
      }
    } finally {
      setEmelSaving(false);
    }
  }

  function updateChipFeed(id: string, patch: Partial<ChipFeedDraft>) {
    setFeeds((prev) => prev.map((f) => (f.id === id ? { ...f, ...patch } : f)));
  }

  function removeChipFeed(id: string) {
    setFeeds((prev) => prev.filter((f) => f.id !== id));
  }

  const inputCls = "jf-input mt-1 w-full max-w-xl";
  const labelCls = "text-xs text-slate-500";

  // The header's way into this form's entries: Sales for a payment form,
  // Submission for a response form. Both are /sales?form=<id>; that page
  // renders a response form's submissions in the answers layout (#604).
  //
  // Deliberately null until the form has RESOLVED (t_105bdd71): the label comes
  // from the mode and there is no mode before the payload arrives, so a
  // response-mode form used to show the sales label while its GET was in flight
  // — and forever after a failed load, which leaves `form` null (`setErr(…);
  // return` on the 404 path, and `catch` on the throw path). Withholding the
  // action is the honest state; no label can be true before the mode is known.
  const entries = formEntriesAction(form, formId);

  // t_c845d0f6: how the chosen logo is previewed. `fsLogoMedia` is only trusted
  // while it still describes `fsLogo` (a pick sets both, a reload resolves the
  // row, "Remove" clears the url). When the row's mime is known, the SHARED
  // rule decides — an SVG is a code asset on the tenant host with
  // `Cross-Origin-Resource-Policy: same-origin`, so an <img> here paints a
  // broken tile and nothing errors (HTTP 200). It gets the same extension badge
  // the media grid and the picker's tiles draw; the url itself is untouched and
  // still saved, so the published form keeps rendering it (same-origin there).
  const logoMedia = fsLogoMedia && fsLogoMedia.url === fsLogo ? fsLogoMedia : null;
  const logoBadge = logoMedia && selectedNeedsBadge(logoMedia) ? extBadge(logoMedia.mime) : "";
  // Between "the form loaded with a logo_url" and "the library answered", the
  // mime is unknown, so the <img> is not painted yet (see fsLogoLookupDone).
  const logoPending = !!fsLogo && !logoMedia && !fsLogoLookupDone;

  return (
    <Shell active="forms">
      <div className="w-full flex-1 px-6 py-8">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h1 className="text-xl font-semibold tracking-tight">{t("fs.title")}</h1>
            <p className="max-w-xl text-sm text-[var(--jf-muted)]">{t("fs.subtitle")}</p>
          </div>
          {/* t_8b35a9f4: the header's action cluster must be able to wrap. In a
              long-label locale the three nowrap buttons are wider than the
              viewport's content box on their own (measured on the built page at
              320px: 371.64px in `ta` against a 272px content box), and a
              non-wrapping flex row cannot shrink below its widest button's
              min-content, so the cluster forced the DOCUMENT's min-content
              wider than the viewport and the whole page scrolled sideways
              (`ta` 76px, `ms` 8px, `ar` 11px at 320px). `flex-wrap` lets the
              buttons stack instead; `max-w-full` is what makes the wrap
              reachable inside the header row's column. Same shape as the
              /templates header group (see web/src/lib/buttonWrap.test.ts). */}
          <div className="flex max-w-full flex-wrap items-center gap-2">
            {formId && (
              <>
                <Link href={`/builder?form=${formId}`} className="jf-btn-ghost px-3 py-1.5 text-xs">
                  {t("fs.editFields")}
                </Link>
                {/* Issue #126: per-form entries deep-link -> /sales?form=<id>.
                    A payment form's entries are Sales; a response form's
                    entries are Submissions — the same destination, which
                    /sales?form=<id> renders in the answers layout for a
                    response form (#604). Both modes get an action; only the
                    label differs, so a response form is neither mislabelled
                    "Sales" (#602) nor left with no route to its submissions.
                    `entries` is null until the form resolves (t_105bdd71) —
                    the label is the form's mode and the mode is not known
                    before the payload, so the row simply waits. */}
                {entries && (
                  <Link href={entries.href} className="jf-btn-ghost px-3 py-1.5 text-xs">
                    {t(entries.labelKey)}
                  </Link>
                )}
                {form?.url && (
                  <a href={form.url} target="_blank" rel="noreferrer" className="jf-btn-ghost px-3 py-1.5 text-xs">
                    {t("bld.view")}
                  </a>
                )}
              </>
            )}
          </div>
        </div>

        {!formId && (
          <div className="jf-card mt-6 p-6 text-sm text-[var(--jf-muted)]">{t("fs.noForm")}</div>
        )}

        {formId && (
          <>
            {/* Tab bar.
                The ACTIVE label must clear AA on the surface the strip is
                actually painted on. This nav paints no background of its own,
                so its nearest painting ancestor is the PAGE background
                (`globals.css`: `html, body { background: var(--jf-bg) }` =
                #f7f8f8). Base `--jf-primary` (#5e6ad2) measures 4.417:1 there
                — below the 4.5:1 AA floor at this 14px/500 weight — while the
                same token PASSES on white (4.700:1), which is why the builder's
                identical strip (`builder/page.tsx`, whose nav declares
                `bg-white`) is clean and this one is not. Found by axe
                (`color-contrast`, serious) on a production build; the
                `--jf-primary-on-tint` token (#4338ca, 7.428:1 here) is the
                AA-safe pair this app already uses wherever the base token's
                surface is not white. Guard:
                `web/e2e/form-settings-tabstrip-contrast.spec.ts` — it measures
                the painted colours in a real browser, not the class list, and
                is part of the `a11y` preflight gate. */}
            <nav className="mt-6 flex flex-wrap gap-1 border-b border-[var(--jf-border)]">
              {TABS.map((tb) => (
                <button
                  key={tb.key}
                  onClick={() => setTab(tb.key)}
                  className={`px-3 py-2 text-sm font-medium ${
                    tab === tb.key
                      ? "border-b-2 border-[#7170ff] text-[var(--jf-primary-on-tint)]"
                      : "text-[var(--jf-muted)] hover:text-[#374151]"
                  }`}
                >
                  {t(tb.label as import("../../components/i18n").Key)}
                </button>
              ))}
            </nav>

            {loading && <p className="mt-6 text-sm text-[var(--jf-muted)]">{t("fs.loading")}</p>}
            {err && <p className="mt-6 text-sm text-red-700">{err}</p>}
            {/* The page's save feedback. ONE state (`msg`) carries BOTH outcomes
                — every save below writes a confirmation on success and a
                failure sentence when the API refuses — so its TONE is painted
                from `msgOk`, never from the sentence. Before this, "Saved ✓"
                and "Failed" were the same indigo 12px line: a merchant whose
                settings were NOT written saw nothing that read as a failure
                (card t_c88c854d). A failure is red-filled AND a live
                `role="alert"`, so it is announced as well as seen. */}
            {msg && (
              <p
                data-testid="action-msg"
                data-tone={msgOk ? "success" : "failure"}
                role={msgOk ? undefined : "alert"}
                className={`mt-3 inline-block rounded-md px-2.5 py-1.5 text-xs ${
                  msgOk
                    ? "border border-emerald-200 bg-emerald-50 text-emerald-700"
                    : "border border-red-200 bg-red-50 font-medium text-red-700"
                }`}
              >
                {msg}
              </p>
            )}

            {!loading && !err && (
              <div className="mt-6 max-w-2xl space-y-6">
                {/* ── General ── */}
                {tab === "general" && (
                  <section className="jf-card p-6">
                    <h2 className="text-base font-semibold">{t("fs.general")}</h2>
                    <div className="mt-4 space-y-3">
                      <div>
                        <label className={labelCls} htmlFor="fs-title">{t("bld.formTitle")}</label>
                        <input id="fs-title" value={title} onChange={(e) => setTitle(e.target.value)} className={inputCls} />
                      </div>
                      <div>
                        <label htmlFor="fs-slug" className={`${labelCls} flex items-center gap-1.5`}>
                          {t("fs.slug")}
                          {/* Inline "?" doc link (AGENTS.md docs-parity rule 4):
                              the slug is the one field whose save changes a URL
                              the merchant already published, so the rule right
                              where the decision is made must be reachable. */}
                          <a
                            href="/docs#form-url-slug"
                            title={t("fs.slugDocTitle")}
                            aria-label={t("fs.slugDocTitle")}
                            className="inline-flex h-4 w-4 items-center justify-center rounded-full border border-slate-300 text-[10px] font-semibold text-slate-500 hover:bg-slate-50 hover:text-slate-700"
                            target="_blank"
                            rel="noreferrer"
                          >
                            ?
                          </a>
                        </label>
                        <input
                          id="fs-slug"
                          value={slug}
                          onChange={(e) => setSlug(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, "-"))}
                          className={`${inputCls} font-mono`}
                        />
                        {/* The rename is not obvious from a bare "Slug (URL)"
                            label: a merchant needs to know the save MOVES the
                            form and that the old URL keeps working, otherwise
                            the field reads as cosmetic (the reported defect was
                            exactly that — a field that looked like it worked). */}
                        <p className="mt-1 text-[11px] text-slate-500">{t("fs.slugHint")}</p>
                      </div>
                      <div>
                        <label className={labelCls}>{t("bld.status")}</label>
                        {/* Wrap, do not divide: a `grid-cols-4` row forced four
                            EQUAL columns at every width, so on a phone each
                            chip got a 51px box for a label that does not fit —
                            measured, Tamil "பட்டியலிடப்படாதது" is 96px and Malay
                            "Tidak disenaraikan" is 113px — and the glyph run
                            painted over the chip's own border. Each chip is now
                            content-sized and the row wraps when it runs out of
                            room: the same `flex max-w-xl flex-wrap gap-1.5`
                            shape the product-image-size row below already uses.
                            `whitespace-nowrap` is defensive only (measured: a
                            one-word label cannot wrap anyway, so removing it
                            changes nothing today) — it documents the intent so
                            a future label with a space still cannot break.
                            Do NOT shrink the font or widen past max-w-xl. */}
                        <div className="mt-1 flex max-w-xl flex-wrap gap-1.5">
                          {(["draft", "private", "published", "unlisted"] as const).map((s) => (
                            <button
                              key={s}
                              type="button"
                              onClick={() => setStatus(s)}
                              className={`whitespace-nowrap rounded-md border px-2 py-2 text-xs font-medium transition-colors ${
                                status === s
                                  ? "border-[#7170ff]/60 bg-indigo-50 text-[var(--jf-primary-on-tint)]"
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
                              onChange={(e) => setAccessPw(e.target.value)}
                              placeholder={t("bld.accessPasswordPh")}
                              className="jf-input mt-1 w-full max-w-xl text-xs"
                            />
                          </div>
                        )}
                      </div>
                      <div>
                        <label htmlFor="fs-theme-color" className={`${labelCls} flex items-center gap-1.5`}>
                          {t("bld.themeColor")}
                          {/* Inline "?" doc link (AGENTS.md docs-parity rule 4):
                              the colour decides the public form's button, so
                              the rule for how it is applied — including the
                              label colour that keeps it readable — is reachable
                              where the choice is made. */}
                          <a
                            href="/docs/customize#brand-colour"
                            title={t("fs.themeDocTitle")}
                            aria-label={t("fs.themeDocTitle")}
                            className="inline-flex h-4 w-4 items-center justify-center rounded-full border border-slate-300 text-[10px] font-semibold text-slate-500 hover:bg-slate-50 hover:text-slate-700"
                            target="_blank"
                            rel="noreferrer"
                          >
                            ?
                          </a>
                        </label>
                        <input
                          id="fs-theme-color"
                          type="color"
                          value={fsTheme}
                          onChange={(e) => setFsTheme(e.target.value)}
                          className="mt-1 h-8 w-full max-w-xl cursor-pointer rounded border border-[var(--jf-border)]"
                        />
                      </div>
                      {/* Issue #471: product image size + layout with a live
                          preview (a restaurant menu wants a bigger image or
                          the image-top menu-card layout). */}
                      <div>
                        <label className={labelCls}>{t("fs.prodImgSize")}</label>
                        <div className="mt-1 flex max-w-xl flex-wrap gap-1.5">
                          {(["small", "medium", "large", "hidden"] as const).map((sz) => (
                            <button
                              key={sz}
                              type="button"
                              onClick={() => setFsImgSize(sz)}
                              className={`rounded-md border px-3 py-2 text-xs font-medium transition-colors ${
                                fsImgSize === sz
                                  ? "border-[#7170ff]/60 bg-indigo-50 text-[var(--jf-primary-on-tint)]"
                                  : "border-[var(--jf-border)] text-[#374151] hover:bg-slate-50"
                              }`}
                            >
                              {t(`fs.prodImgSize${sz[0].toUpperCase()}${sz.slice(1)}` as import("../../components/i18n").Key)}
                            </button>
                          ))}
                        </div>
                        <div className="mt-2">
                          <label className={labelCls}>{t("fs.prodImgLayout")}</label>
                          <div className="mt-1 flex max-w-xl gap-1.5">
                            {(["thumbnail-left", "image-top"] as const).map((ly) => (
                              <button
                                key={ly}
                                type="button"
                                onClick={() => setFsImgLayout(ly)}
                                disabled={fsImgSize === "hidden"}
                                className={`rounded-md border px-3 py-2 text-xs font-medium transition-colors disabled:opacity-40 ${
                                  fsImgLayout === ly
                                    ? "border-[#7170ff]/60 bg-indigo-50 text-[var(--jf-primary-on-tint)]"
                                    : "border-[var(--jf-border)] text-[#374151] hover:bg-slate-50"
                                }`}
                              >
                                {t(`fs.prodImgLayout${ly === "image-top" ? "Top" : "Left"}` as import("../../components/i18n").Key)}
                              </button>
                            ))}
                          </div>
                        </div>
                        {/* Live preview of the chosen size/layout. */}
                        {fsImgSize !== "hidden" && (
                          <div className="mt-3 max-w-xl rounded-lg border border-[var(--jf-border)] bg-white p-3">
                            <p className="mb-2 text-[10px] uppercase tracking-wide text-[var(--jf-muted)]">
                              {t("fs.prodImgPreview")}
                            </p>
                            <div
                              className={`flex gap-3 rounded-lg border border-[var(--jf-border)] p-3 ${
                                fsImgLayout === "image-top" ? "flex-col items-stretch" : "items-center"
                              }`}
                            >
                              <div
                                className="shrink-0 rounded-[10px] bg-slate-100"
                                style={
                                  fsImgLayout === "image-top"
                                    ? { width: "100%", height: fsImgSize === "large" ? 120 : 84 }
                                    : { width: fsImgSize === "small" ? 52 : fsImgSize === "medium" ? 96 : 160, height: fsImgSize === "small" ? 52 : fsImgSize === "medium" ? 96 : 160 }
                                }
                              />
                              <div className={fsImgLayout === "image-top" ? "flex w-full items-center gap-3" : "flex flex-1 items-center gap-3"}>
                                <span className="inline-block h-5 w-5 shrink-0 rounded border-2 border-slate-300 bg-white" />
                                <span className="flex-1 text-sm font-medium text-slate-700">{t("fs.prodImgSample")}</span>
                                <span className="ml-auto text-sm font-semibold text-[var(--jf-primary)]">RM 12.00</span>
                              </div>
                            </div>
                          </div>
                        )}
                      </div>
                      <div>
                        <label className={labelCls}>{t("bld.logoUrl")}</label>
                        {/* Issue #501: a rejected (non-https) logo shows here,
                            at the field, instead of a silently broken image
                            on the buyer's page. */}
                        {fsLogoErr && (
                          <p className="mt-1 rounded-md bg-red-50 px-2 py-1 text-[11px] font-medium text-red-700">
                            {fsLogoErr}
                          </p>
                        )}
                        {/* Issue #391: WordPress-style media picker — choose
                            from the media library or upload a new image, with
                            a preview of the current logo. A paste-URL
                            fallback is available inside the picker for power
                            users who want an external image. */}
                        {/* Card t_fbfe40ca: `flex-wrap` because the button below now
                            actually applies `.jf-btn-primary` (it used to carry the
                            undefined `jf-btn`, so its label WRAPPED and it never
                            pushed anything). With the shared class in force the
                            label is `white-space: nowrap` (the #655 fix for the
                            header rows) and the button is `flex-shrink: 0`, so at
                            320px this row measured 222.27px wide — 7px past the
                            viewport, scrolling the whole page sideways. Wrapping
                            the row is the same remedy, for the same reason, that
                            the /form-settings header cluster and the /templates
                            action group already use. */}
                        <div className="mt-1 flex flex-wrap items-center gap-3">
                          {fsLogo ? (
                            /* t_c845d0f6: the SELECTED logo is previewed with the
                               same rule as the picker's tiles. It used to be a
                               plain image tag pointed straight at the chosen
                               url: for a code asset (SVG/CSS/JS/HTML/WOFF2) that
                               url is on the tenant host and served
                               `Cross-Origin-Resource-Policy: same-origin`, so
                               the dashboard painted a broken-image icon at HTTP
                               200 with nothing in the console — while the
                               published form, same-origin with it, showed the
                               logo fine. Known mime → shared rule; unknown mime
                               (a pasted external link) → the image tag as
                               before. */
                            logoBadge ? (
                              <div
                                role="img"
                                aria-label={logoBadge}
                                title={logoBadge}
                                className="flex h-12 w-12 items-center justify-center rounded-lg border border-[var(--jf-border)] bg-white text-center text-[11px] font-bold text-[#4f46e5]"
                              >
                                {logoBadge}
                              </div>
                            ) : logoPending ? (
                              /* the mime is still resolving — same box, no text,
                                 so an SVG logo never flashes its broken frame */
                              <div className="h-12 w-12 rounded-lg border border-[var(--jf-border)] bg-slate-50" />
                            ) : (
                              /* eslint-disable-next-line @next/next/no-img-element */
                              <img
                                src={fsLogo}
                                alt={t("mediaPicker.title")}
                                className="h-12 w-12 rounded-lg border border-[var(--jf-border)] object-contain"
                              />
                            )
                          ) : (
                            <div className="flex h-12 w-12 items-center justify-center rounded-lg border border-dashed border-[var(--jf-border)] text-[10px] text-[var(--jf-muted)]">
                              {t("mediaPicker.noLogo")}
                            </div>
                          )}
                          <div className="flex flex-col gap-1.5">
                            <button
                              type="button"
                              onClick={() => setLogoPickerOpen(true)}
                              className="jf-btn-primary jf-cta-wrap px-3 py-1.5 text-xs"
                            >
                              {t("mediaPicker.choose")}
                            </button>
                            {fsLogo && (
                              <button
                                type="button"
                                onClick={() => {
                                  setFsLogo("");
                                  setFsLogoMedia(null);
                                }}
                                className="jf-btn-ghost px-3 py-1 text-[11px] text-red-700"
                              >
                                {t("mediaPicker.remove")}
                              </button>
                            )}
                          </div>
                        </div>
                      </div>
                      {/* Issue #388: power-user custom JS. Owner-gated (write
                          role) + scanned server-side; runs on the buyer's page. */}
                      <details className="rounded-md border border-[var(--jf-border)] p-3">
                        <summary className="cursor-pointer text-sm font-medium text-slate-700">
                          {t("bld.customJs")}
                        </summary>
                        <div className="mt-2 space-y-2">
                          <textarea
                            aria-label={t("bld.customJs")}
                            value={fsCustomJS}
                            onChange={(e) => setFsCustomJS(e.target.value)}
                            rows={5}
                            spellCheck={false}
                            placeholder="// e.g. document.querySelector('#of').addEventListener('submit', ...)"
                            className={`${inputCls} font-mono text-xs`}
                          />
                          <p className="text-[11px] leading-relaxed text-amber-600">
                            {t("bld.customJsWarning")}
                          </p>
                        </div>
                      </details>
                      <label className="flex items-center gap-2 text-sm text-slate-700">
                        <input type="checkbox" checked={fsEmbed} onChange={(e) => setFsEmbed(e.target.checked)} className="accent-[#5e6ad2]" />
                        {t("bld.allowEmbed")}
                        {/* Issue #456: inline doc link to the embed guide (auto-height + snippet). */}
                        <a
                          href="/docs/embed"
                          target="_blank"
                          rel="noopener noreferrer"
                          title={t("bld.embedDocs")}
                          className="inline-flex h-4 w-4 items-center justify-center rounded-full bg-slate-200 text-[10px] font-bold text-slate-600 hover:bg-[var(--jf-primary)] hover:text-white"
                        >
                          ?
                        </a>
                      </label>
                      {/* Issue #438: QR ordering flow — generate a QR for the
                          form's public URL, optionally encoding a table number. */}
                      <details className="rounded-md border border-[var(--jf-border)] p-3">
                        <summary className="cursor-pointer text-sm font-medium text-slate-700">
                          {t("fs.qrTitle")}
                        </summary>
                        <div className="mt-2 space-y-2">
                          <p className="text-[11px] leading-relaxed text-slate-500">{t("fs.qrHelp")}</p>
                          <div className="flex flex-wrap items-center gap-2">
                            <input
                              aria-label={t("fs.qrTablePh")}
                              value={qrTable}
                              onChange={(e) => setQrTable(e.target.value)}
                              className="jf-input max-w-[160px] font-mono"
                              placeholder={t("fs.qrTablePh")}
                            />
                            <button
                              type="button"
                              onClick={generateQR}
                              disabled={!form?.url || qrBusy}
                              className="jf-btn-primary px-3 py-1.5 text-xs"
                            >
                              {qrBusy ? "…" : t("fs.qrGenerate")}
                            </button>
                          </div>
                          {qrDataUrl && (
                            <div className="flex flex-col items-start gap-2">
                              {/* eslint-disable-next-line @next/next/no-img-element */}
                              <img src={qrDataUrl} alt={t("fs.qrTitle")} className="h-40 w-40 rounded-lg border border-[var(--jf-border)]" />
                              <p className="break-all font-mono text-[10px] text-[var(--jf-muted)]">{qrTarget}</p>
                              <div className="flex flex-wrap items-center gap-2">
                                <a
                                  href={qrDataUrl}
                                  download={`qr-${slug || "form"}.png`}
                                  className="jf-btn-ghost px-3 py-1.5 text-xs"
                                >
                                  {t("fs.qrDownload")}
                                </a>
                                {form?.url && (
                                  <a
                                    href={`${form.url.replace(/\/[^/]*$/, "")}/qr/${slug || "form"}/stand${qrTable.trim() ? `?table=${encodeURIComponent(qrTable.trim())}` : ""}`}
                                    target="_blank"
                                    rel="noreferrer"
                                    className="jf-btn-ghost px-3 py-1.5 text-xs"
                                  >
                                    {t("fs.qrStand")}
                                  </a>
                                )}
                              </div>
                            </div>
                          )}
                        </div>
                      </details>
                      <div className="pt-1">
                        <button onClick={saveGeneral} disabled={!canWrite} className="jf-btn-primary">
                          {t("fs.save")}
                        </button>
                      </div>
                    </div>
                  </section>
                )}

                {/* ── Restaurant menu (issue #497: a per-form setting) ── */}
                {tab === "menu" && (
                  <section className="jf-card p-6">
                    <div className="flex flex-wrap items-start justify-between gap-3">
                      <div>
                        <h2 className="text-base font-semibold">{t("fs.menu")}</h2>
                        <p className="mt-1 max-w-xl text-sm text-[var(--jf-muted)]">{t("fs.menuHint")}</p>
                      </div>
                      {/* Issue #513: inline "?" doc link (AGENTS.md
                          documentation-parity rule 4) — covers the QR menu and
                          the diner-side basket it renders. */}
                      <a
                        href="/docs/customize"
                        target="_blank"
                        rel="noopener noreferrer"
                        title={t("fs.menuDocs")}
                        aria-label={t("fs.menuDocs")}
                        className="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-slate-200 text-[11px] font-bold text-slate-600 hover:bg-[var(--jf-primary)] hover:text-white"
                      >
                        ?
                      </a>
                    </div>

                    <div className="mt-4 space-y-4">
                      <div className="flex items-start justify-between gap-4 rounded-lg border border-[var(--jf-border)] p-3">
                        <div>
                          <p className="text-sm font-medium">{t("set.menuEnabledTitle")}</p>
                          <p className="mt-0.5 max-w-md text-xs text-slate-500">{t("set.menuEnabledHelp")}</p>
                        </div>
                        <Switch
                          checked={menuEnabled}
                          onChange={() => setMenuEnabled((v) => !v)}
                          label={t("set.menuEnabledTitle")}
                        />
                      </div>

                      {menuEnabled && (
                        <>
                          {menuProducts.length === 0 ? (
                            <p className="rounded-md border border-amber-200 bg-amber-50 px-2.5 py-2 text-[11px] leading-relaxed text-amber-800">
                              {t("fs.menuNoProducts")}
                            </p>
                          ) : (
                            <>
                              <div>
                                <label className={labelCls}>{t("set.menuBestSeller")}</label>
                                <div className="mt-1 flex max-w-xl flex-wrap items-center gap-2 rounded-lg border border-[var(--jf-border)] p-2">
                                  {menuBestSeller && menuProductByID(menuBestSeller) ? (
                                    <>
                                      <span className="flex h-9 w-9 shrink-0 items-center justify-center overflow-hidden rounded-md bg-slate-100">
                                        {menuProductByID(menuBestSeller)?.images?.[0] ? (
                                          /* eslint-disable-next-line @next/next/no-img-element */
                                          <img
                                            src={menuProductByID(menuBestSeller)!.images![0]}
                                            alt=""
                                            className="h-full w-full object-cover"
                                          />
                                        ) : (
                                          <span className="text-[10px] text-[var(--jf-muted-on-tint)]">—</span>
                                        )}
                                      </span>
                                      <span className="min-w-0 flex-1 truncate text-sm">
                                        {menuProductByID(menuBestSeller)!.name}
                                      </span>
                                      <button
                                        type="button"
                                        onClick={() => {
                                          setMenuPickerCatIndex(-1);
                                          setMenuPickerOpen(true);
                                        }}
                                        className="jf-btn-ghost px-2 py-1 text-[11px]"
                                      >
                                        {t("set.menuChangeBestSeller")}
                                      </button>
                                      <button
                                        type="button"
                                        onClick={() => setMenuBestSeller("")}
                                        className="jf-btn-ghost px-2 py-1 text-[11px] text-red-700"
                                      >
                                        {t("set.menuClearBestSeller")}
                                      </button>
                                    </>
                                  ) : (
                                    <>
                                      <span className="flex-1 truncate text-sm text-[var(--jf-muted)]">
                                        {menuBestSeller ? t("set.menuUnknownProduct") : t("set.menuBestSellerNone")}
                                      </span>
                                      <button
                                        type="button"
                                        onClick={() => {
                                          setMenuPickerCatIndex(-1);
                                          setMenuPickerOpen(true);
                                        }}
                                        className="jf-btn-primary px-2.5 py-1 text-[11px]"
                                      >
                                        {t("set.menuChooseBestSeller")}
                                      </button>
                                    </>
                                  )}
                                </div>
                              </div>

                              <div>
                                <label className={labelCls}>{t("set.menuCategories")}</label>
                                <div className="mt-1 space-y-2">
                                  {menuCategories.map((c, i) => (
                                    <div key={i} className="flex flex-col gap-2 rounded-lg border border-[var(--jf-border)] p-3">
                                      <div className="flex items-center gap-2">
                                        <input
                                          value={c.name}
                                          onChange={(e) =>
                                            setMenuCategories((arr) =>
                                              arr.map((x, j) => (j === i ? { ...x, name: e.target.value } : x))
                                            )
                                          }
                                          className="jf-input flex-1"
                                          placeholder={t("set.menuCatName")}
                                        />
                                        <button
                                          type="button"
                                          onClick={() => setMenuCategories((arr) => arr.filter((_, j) => j !== i))}
                                          className="jf-btn-ghost px-2 py-1 text-[11px] text-red-700"
                                        >
                                          {t("set.menuCatRemove")}
                                        </button>
                                      </div>
                                      <div className="flex flex-wrap items-center gap-2">
                                        {(c.product_ids || []).map((id) => {
                                          const p = menuProductByID(id);
                                          return (
                                            <span
                                              key={id}
                                              className={`inline-flex items-center gap-1.5 rounded-full border py-0.5 pl-0.5 pr-2 text-xs ${
                                                p ? "border-[var(--jf-border)] bg-white" : "border-amber-300 bg-amber-50"
                                              }`}
                                            >
                                              <span className="flex h-6 w-6 shrink-0 items-center justify-center overflow-hidden rounded-full bg-slate-100">
                                                {p?.images?.[0] ? (
                                                  /* eslint-disable-next-line @next/next/no-img-element */
                                                  <img src={p.images[0]} alt="" className="h-full w-full object-cover" />
                                                ) : (
                                                  <span className="text-[9px] text-[var(--jf-muted-on-tint)]">—</span>
                                                )}
                                              </span>
                                              <span className="max-w-[12rem] truncate">
                                                {p ? p.name : t("set.menuUnknownProduct")}
                                              </span>
                                              <button
                                                type="button"
                                                aria-label={t("set.menuCatRemove")}
                                                onClick={() =>
                                                  setMenuCategories((arr) =>
                                                    arr.map((x, j) =>
                                                      j === i
                                                        ? { ...x, product_ids: (x.product_ids || []).filter((v) => v !== id) }
                                                        : x
                                                    )
                                                  )
                                                }
                                                className="text-[var(--jf-muted)] hover:text-red-700"
                                              >
                                                ✕
                                              </button>
                                            </span>
                                          );
                                        })}
                                        <button
                                          type="button"
                                          onClick={() => {
                                            setMenuPickerCatIndex(i);
                                            setMenuPickerOpen(true);
                                          }}
                                          className="jf-btn-ghost px-2.5 py-1 text-[11px]"
                                        >
                                          + {t("set.menuPickProducts")}
                                        </button>
                                      </div>
                                    </div>
                                  ))}
                                  <button
                                    type="button"
                                    onClick={() => setMenuCategories((arr) => [...arr, { name: "", product_ids: [] }])}
                                    className="jf-btn-ghost"
                                  >
                                    + {t("set.menuCatAdd")}
                                  </button>
                                </div>
                              </div>

                              {/* Issue #493/#497: optional order form for extra
                                  checkout fields. Its field blocks ARE the
                                  extra fields the menu checkout collects. */}
                              <div>
                                <label className={labelCls}>{t("set.menuOrderForm")}</label>
                                <div className="mt-1 max-w-xl space-y-2">
                                  <select
                                    value={menuOrderFormSlug}
                                    onChange={(e) => setMenuOrderFormSlug(e.target.value)}
                                    className="jf-input"
                                  >
                                    <option value="">{t("set.menuOrderFormNone")}</option>
                                    {menuForms.map((f) => (
                                      <option key={f.id} value={f.slug}>
                                        {f.title} — {f.slug}
                                      </option>
                                    ))}
                                  </select>
                                  <p className="text-[11px] leading-relaxed text-slate-500">
                                    {t("set.menuOrderFormHelp")}
                                  </p>
                                  <p className="rounded-md border border-amber-200 bg-amber-50 px-2.5 py-2 text-[11px] leading-relaxed text-amber-800">
                                    {t("set.menuOrderFormMandatory")}
                                  </p>
                                  {menuOrderFormSlug && (
                                    <div className="flex flex-wrap items-center gap-2">
                                      {(() => {
                                        const f = menuForms.find((x) => x.slug === menuOrderFormSlug);
                                        return (
                                          <>
                                            <span className="text-[11px] text-[var(--jf-muted)]">
                                              {f
                                                ? t("set.menuOrderFormSelected", { n: f.title })
                                                : t("set.menuOrderFormMissing")}
                                            </span>
                                            {f && (
                                              <a
                                                href={`/builder?form=${f.id}`}
                                                className="jf-btn-ghost px-2.5 py-1 text-[11px]"
                                              >
                                                {t("set.menuOrderFormEdit")}
                                              </a>
                                            )}
                                          </>
                                        );
                                      })()}
                                    </div>
                                  )}
                                </div>
                              </div>

                              {/* Issue #497: live preview of the menu structure,
                                  scoped to the products THIS form references. */}
                              <div className="rounded-lg border border-[var(--jf-border)] p-3">
                                <div className="flex items-center justify-between gap-2">
                                  <p className="text-sm font-medium">{t("set.menuPreviewTitle")}</p>
                                  <span className="text-xs text-[var(--jf-muted)]">
                                    {t("set.menuPreviewItems", {
                                      n: menuPreviewSections().reduce((n, s) => n + s.ids.length, 0),
                                    })}
                                  </span>
                                </div>
                                <p className="mt-0.5 max-w-xl text-xs text-slate-500">
                                  {t("set.menuPreviewHelp")}
                                </p>
                                {menuPreviewSections().length === 0 && (
                                  <p className="mt-2 text-sm text-[var(--jf-muted)]">{t("set.menuPreviewEmpty")}</p>
                                )}
                                <div className="mt-2 space-y-2">
                                  {menuPreviewSections().map((s, si) => (
                                    <div key={si}>
                                      <p className="text-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                                        {s.name || t("set.menuCatName")}
                                      </p>
                                      <div className="mt-1 flex flex-wrap gap-2">
                                        {s.ids.map((id) => {
                                          const p = menuProductByID(id);
                                          if (!p) return null;
                                          return (
                                            <span
                                              key={id}
                                              className="inline-flex items-center gap-2 rounded-lg border border-[var(--jf-border)] bg-white px-2 py-1"
                                            >
                                              <span className="flex h-8 w-8 shrink-0 items-center justify-center overflow-hidden rounded-md bg-slate-100">
                                                {p.images?.[0] ? (
                                                  /* eslint-disable-next-line @next/next/no-img-element */
                                                  <img src={p.images[0]} alt="" className="h-full w-full object-cover" />
                                                ) : (
                                                  <span className="text-[9px] text-[var(--jf-muted-on-tint)]">—</span>
                                                )}
                                              </span>
                                              <span className="text-xs">
                                                <span className="block max-w-[10rem] truncate font-medium text-slate-700">
                                                  {p.name}
                                                </span>
                                                <span className="block text-[10px] text-[var(--jf-muted)]">
                                                  {`RM${(p.price_sen / 100).toLocaleString("en-MY", { minimumFractionDigits: 2 })}`}
                                                </span>
                                              </span>
                                            </span>
                                          );
                                        })}
                                      </div>
                                    </div>
                                  ))}
                                </div>
                              </div>
                            </>
                          )}

                          <div className="flex flex-wrap items-center gap-2 pt-1">
                            <button onClick={saveMenu} disabled={menuSaving} className="jf-btn-primary">
                              {t("fs.save")}
                            </button>
                            {form?.url && (
                              <a href={form.url} target="_blank" rel="noreferrer" className="jf-btn-ghost">
                                {t("set.menuPreview")}
                              </a>
                            )}
                          </div>
                        </>
                      )}
                    </div>
                  </section>
                )}

                {/* ── Confirmation ── */}
                {tab === "confirmation" && (
                  <section className="jf-card p-6">
                    <h2 className="text-base font-semibold">{t("fs.confirmation")}</h2>
                    <p className="mt-1 text-sm text-[var(--jf-muted)]">{t("fs.confirmationHint")}</p>
                    <div className="mt-4 space-y-3">
                      <div>
                        <label className={labelCls} htmlFor="fs-redir-success">{t("bld.redirSuccess")}</label>
                        <input id="fs-redir-success" value={fsRedS} onChange={(e) => setFsRedS(e.target.value)} placeholder="https://yourapp.com/thanks" className={inputCls} />
                      </div>
                      <div>
                        <label className={labelCls} htmlFor="fs-redir-failure">{t("bld.redirFailure")}</label>
                        <input id="fs-redir-failure" value={fsRedF} onChange={(e) => setFsRedF(e.target.value)} placeholder="https://yourapp.com/fail" className={inputCls} />
                      </div>
                      {/* Issue #393: per-form payment-failed message. */}
                      <div>
                        <label className={labelCls} htmlFor="fs-failure-message">{t("fs.failureMessage")}</label>
                        <textarea
                          id="fs-failure-message"
                          value={fsFailMsg}
                          onChange={(e) => setFsFailMsg(e.target.value)}
                          rows={3}
                          className={`${inputCls} font-mono text-xs`}
                          placeholder={t("fs.builtInDefaultPlaceholder")}
                        />
                        <p className="mt-1 text-xs text-[var(--jf-muted)]">{t("fs.failureMessageHint")}</p>
                      </div>
                      <div className="pt-1">
                        {/* Card t_1463b71e: this button saves the display
                            settings (redirects + failed message); the one at
                            the bottom of the tab saves the confirmations list.
                            Two buttons both reading "Save" on one screen is
                            what made a dropped field invisible — each now
                            names what it writes. */}
                        <button onClick={saveConfirmation} disabled={!canWrite} className="jf-btn-primary">
                          {t("fs.saveConfirmationSettings")}
                        </button>
                      </div>
                    </div>

                    {/* Issue #179: per-form confirmations (Gravity-Forms-style). */}
                    <div className="mt-8 border-t border-[var(--jf-border)] pt-6">
                      <h3 className="text-base font-semibold">{t("fs.confirmations")}</h3>
                      <p className="mt-1 text-sm text-[var(--jf-muted)]">{t("fs.confirmationsHint")}</p>
                      <div className="mt-4 space-y-4">
                        {confs.map((c, i) => (
                          <div key={i} className="rounded-lg border border-[var(--jf-border)] p-4">
                            <div className="flex flex-wrap items-center justify-between gap-2">
                              <div className="flex items-center gap-2">
                                <input
                                  aria-label={t("fs.confName")}
                                  value={c.name}
                                  onChange={(e) => updateConf(i, { name: e.target.value })}
                                  placeholder={t("fs.confName")}
                                  className="jf-input w-56 text-sm"
                                />
                                {c.is_default && (
                                  <span className="rounded-full border border-indigo-200 bg-indigo-50 px-2 py-0.5 text-[11px] font-medium text-indigo-600">
                                    {t("fs.confDefault")}
                                  </span>
                                )}
                              </div>
                              {!c.is_default && (
                                <button
                                  onClick={() => removeConfirmation(i)}
                                  disabled={!canWrite}
                                  className="jf-btn-ghost px-2 py-1 text-xs text-red-700"
                                >
                                  {t("fs.confDelete")}
                                </button>
                              )}
                            </div>
                            <div className="mt-3 flex flex-wrap items-center gap-3">
                              <label className="flex items-center gap-2 text-sm text-slate-700">
                                <input
                                  type="radio"
                                  name={`conf-type-${i}`}
                                  checked={c.type === "message"}
                                  onChange={() => updateConf(i, { type: "message" })}
                                  className="accent-[#5e6ad2]"
                                />
                                {t("fs.confMessage")}
                              </label>
                              <label className="flex items-center gap-2 text-sm text-slate-700">
                                <input
                                  type="radio"
                                  name={`conf-type-${i}`}
                                  checked={c.type === "redirect"}
                                  onChange={() => updateConf(i, { type: "redirect" })}
                                  className="accent-[#5e6ad2]"
                                />
                                {t("fs.confRedirect")}
                              </label>
                            </div>
                            {c.type === "message" ? (
                              <div className="mt-3">
                                <label className={labelCls} htmlFor={`fs-conf-${i}-message`}>{t("fs.confMessageBody")}</label>
                                <textarea
                                  id={`fs-conf-${i}-message`}
                                  value={c.message}
                                  onChange={(e) => updateConf(i, { message: e.target.value })}
                                  rows={3}
                                  className={`${inputCls} font-mono text-xs`}
                                  placeholder={t("fs.builtInDefaultPlaceholder")}
                                />
                              </div>
                            ) : (
                              <div className="mt-3">
                                <label className={labelCls} htmlFor={`fs-conf-${i}-redirect`}>{t("fs.confRedirectUrl")}</label>
                                <input
                                  id={`fs-conf-${i}-redirect`}
                                  value={c.redirect_url}
                                  onChange={(e) => updateConf(i, { redirect_url: e.target.value })}
                                  placeholder="https://yourapp.com/thanks"
                                  className={inputCls}
                                />
                              </div>
                            )}
                            <div className="mt-3">
                              <label className="flex items-center gap-2 text-sm text-slate-700">
                                <input
                                  type="checkbox"
                                  checked={!!c.condition}
                                  onChange={(e) =>
                                    updateConf(i, {
                                      condition: e.target.checked
                                        ? { field_id: "", op: "equals", value: "" }
                                        : null,
                                    })
                                  }
                                  className="accent-[#5e6ad2]"
                                />
                                {t("fs.confConditional")}
                              </label>
                              {c.condition && (
                                <div className="mt-2">
                                  <ConditionPicker
                                    fieldId={c.condition.field_id}
                                    op={c.condition.op}
                                    value={c.condition.value}
                                    fields={formBlocks}
                                    onField={(id) => updateConf(i, { condition: { ...c.condition!, field_id: id } })}
                                    onOp={(o) => updateConf(i, { condition: { ...c.condition!, op: o } })}
                                    onValue={(v) => updateConf(i, { condition: { ...c.condition!, value: v } })}
                                    t={t}
                                    compact
                                  />
                                </div>
                              )}
                            </div>
                          </div>
                        ))}
                      </div>
                      <div className="mt-4 flex flex-wrap items-center gap-2">
                        <button onClick={addConfirmation} disabled={!canWrite} className="jf-btn-ghost px-3 py-1.5 text-xs">
                          {t("fs.confAdd")}
                        </button>
                        <button onClick={saveConfirmations} disabled={!canWrite} className="jf-btn-primary">
                          {t("fs.saveConfirmations")}
                        </button>
                      </div>
                    </div>
                  </section>
                )}

                {/* ── Restrictions ── */}
                {tab === "restrictions" && (
                  <section className="jf-card p-6">
                    <h2 className="text-base font-semibold">{t("fs.restrictions")}</h2>
                    <div className="mt-4 space-y-3">
                      <p className="text-[11px] font-medium uppercase tracking-wider text-[var(--jf-muted)]">{t("bld.scheduleTitle")}</p>
                      <div>
                        <label className={labelCls} htmlFor="fs-rs-start">{t("bld.startAt")}</label>
                        <input id="fs-rs-start" type="datetime-local" value={rsStart} onChange={(e) => setRsStart(e.target.value)} className={inputCls} />
                      </div>
                      <div>
                        <label className={labelCls} htmlFor="fs-rs-end">{t("bld.endAt")}</label>
                        <input id="fs-rs-end" type="datetime-local" value={rsEnd} onChange={(e) => setRsEnd(e.target.value)} className={inputCls} />
                      </div>
                      <div>
                        <label className={labelCls} htmlFor="fs-rs-pending">{t("bld.pendingMessage")}</label>
                        <textarea id="fs-rs-pending" value={rsPending} onChange={(e) => setRsPending(e.target.value)} rows={2} className={inputCls} />
                      </div>
                      <div>
                        <label className={labelCls} htmlFor="fs-rs-expired">{t("bld.expiredMessage")}</label>
                        <textarea id="fs-rs-expired" value={rsExpired} onChange={(e) => setRsExpired(e.target.value)} rows={2} className={inputCls} />
                      </div>
                      <p className="text-[11px] font-medium uppercase tracking-wider text-[var(--jf-muted)]">{t("bld.entryLimitTitle")}</p>
                      <div>
                        <label className={labelCls} htmlFor="fs-rs-limit">{t("bld.entryLimit")}</label>
                        <input id="fs-rs-limit" type="number" min={0} value={rsLimit} onChange={(e) => setRsLimit(e.target.value)} placeholder="100" className={inputCls} />
                      </div>
                      <div>
                        <label className={labelCls} htmlFor="fs-rs-limit-msg">{t("bld.limitMessage")}</label>
                        <textarea id="fs-rs-limit-msg" value={rsLimitMsg} onChange={(e) => setRsLimitMsg(e.target.value)} rows={2} className={inputCls} />
                      </div>
                      <div className="pt-1">
                        <button onClick={saveRestrictions} disabled={!canWrite} className="jf-btn-primary">
                          {t("fs.save")}
                        </button>
                      </div>
                    </div>
                  </section>
                )}

                {/* ── Notifications (issue #193) ── */}
                {tab === "notifications" && (
                  <>
                  {/* Issue #453: per-form email settings override of the workspace
                      email settings (#107). Every field has a "use workspace
                      default" option; the resolution is per-form -> workspace
                      -> default. */}
                  <section className="jf-card p-6">
                    <div className="flex flex-wrap items-center justify-between gap-3">
                      <div>
                        <h2 className="text-base font-semibold">{t("fs.emelTitle")}</h2>
                        <p className="mt-1 text-sm text-[var(--jf-muted)]">{t("fs.emelHint")}</p>
                      </div>
                      <a
                        href="/docs/placeholders"
                        target="_blank"
                        rel="noopener noreferrer"
                        title={t("fs.notifDocs")}
                        className="inline-flex h-5 w-5 items-center justify-center rounded-full bg-slate-200 text-[11px] font-bold text-slate-600 hover:bg-[var(--jf-primary)] hover:text-white"
                      >
                        ?
                      </a>
                    </div>

                    {emelEffective && (
                      <p className="mt-3 rounded-lg border border-[var(--jf-border)] bg-[var(--jf-bg-soft,#f8f8f8)] px-3 py-2 text-[11px] text-[var(--jf-muted)]">
                        {t("fs.emelEffective")}:{" "}
                        <span className="font-medium">
                          {emelEffective.receipt_enabled ? t("fs.emelOn") : t("fs.emelOff")}
                        </span>{" "}
                        · {t("fs.emelNotifyShort")}:{" "}
                        <span className="font-medium">
                          {emelEffective.notify_merchant ? t("fs.emelOn") : t("fs.emelOff")}
                        </span>
                      </p>
                    )}

                    <div className="mt-4 space-y-4">
                      <div>
                        <label className={labelCls} htmlFor="fs-emel-subject">
                          {t("fs.emelSubject")} <span className="text-[var(--jf-muted)]">{t("fs.emelInheritHint")}</span>{" "}
                          {/* The per-field rule, stated at the field it applies to
                              (AGENTS.md documentation-parity rule). */}
                          <a
                            href="/docs/customize#language"
                            target="_blank"
                            rel="noopener noreferrer"
                            className="text-[var(--jf-muted)] underline hover:text-slate-600"
                            title={t("set.emelLanguageRule")}
                            aria-label={t("set.emelLanguageRule")}
                          >
                            ?
                          </a>
                        </label>
                        <input
                          id="fs-emel-subject"
                          value={emelSubject}
                          onChange={(e) => setEmelSubject(e.target.value)}
                          disabled={!canWrite}
                          className={inputCls}
                          placeholder={emelWorkspace?.receipt_subject || "Resit pembayaran — {{ref_code}}"}
                        />
                      </div>
                      <div>
                        <label className={labelCls} htmlFor="fs-emel-body">
                          {t("fs.emelBody")} <span className="text-[var(--jf-muted)]">{t("fs.emelPlaceholders")}</span>{" "}
                          <a
                            href="/docs/customize#language"
                            target="_blank"
                            rel="noopener noreferrer"
                            className="text-[var(--jf-muted)] underline hover:text-slate-600"
                            title={t("set.emelLanguageRule")}
                            aria-label={t("set.emelLanguageRule")}
                          >
                            ?
                          </a>
                        </label>
                        <textarea
                          id="fs-emel-body"
                          value={emelBody}
                          onChange={(e) => setEmelBody(e.target.value)}
                          disabled={!canWrite}
                          rows={6}
                          className={`${inputCls} font-mono text-xs`}
                          placeholder={emelWorkspace?.receipt_body || ""}
                        />
                      </div>

                      <TriToggle
                        label={t("fs.emelReceiptEnabled")}
                        value={emelReceiptEnabled}
                        onChange={setEmelReceiptEnabled}
                        disabled={!canWrite}
                        t={t}
                        inheritLabel={
                          emelWorkspace
                            ? `${t("fs.emelWorkspaceDefault")} (${emelWorkspace.receipt_enabled ? t("fs.emelOn") : t("fs.emelOff")})`
                            : t("fs.emelWorkspaceDefault")
                        }
                      />
                      <TriToggle
                        label={t("fs.emelNotifyMerchant")}
                        value={emelNotifyMerchant}
                        onChange={setEmelNotifyMerchant}
                        disabled={!canWrite}
                        t={t}
                        inheritLabel={
                          emelWorkspace
                            ? `${t("fs.emelWorkspaceDefault")} (${emelWorkspace.notify_merchant ? t("fs.emelOn") : t("fs.emelOff")})`
                            : t("fs.emelWorkspaceDefault")
                        }
                      />
                      <TriToggle
                        label={t("fs.emelHtml")}
                        value={emelHtml}
                        onChange={setEmelHtml}
                        disabled={!canWrite}
                        t={t}
                        inheritLabel={
                          emelWorkspace
                            ? `${t("fs.emelWorkspaceDefault")} (${emelWorkspace.html ? t("fs.emelOn") : t("fs.emelOff")})`
                            : t("fs.emelWorkspaceDefault")
                        }
                      />

                      <div>
                        <label className={labelCls} htmlFor="fs-emel-merchant-email">
                          {t("fs.emelMerchantEmail")} <span className="text-[var(--jf-muted)]">{t("fs.emelInheritHint")}</span>
                        </label>
                        <input
                          id="fs-emel-merchant-email"
                          value={emelMerchantEmail}
                          onChange={(e) => setEmelMerchantEmail(e.target.value)}
                          disabled={!canWrite}
                          type="email"
                          className={inputCls}
                          placeholder={emelWorkspace?.merchant_notify_email || t("fs.emelAccountEmail")}
                        />
                      </div>

                      <div className="flex flex-wrap items-center gap-2 pt-1">
                        <button onClick={saveFormEmailSettings} disabled={!canWrite || emelSaving} className="jf-btn-primary">
                          {t("fs.emelSave")}
                        </button>
                        <button onClick={clearFormEmailOverrides} disabled={!canWrite || emelSaving} className="jf-btn-ghost text-slate-600">
                          {t("fs.emelClear")}
                        </button>
                      </div>
                    </div>
                  </section>

                  <section className="jf-card p-6">
                    <div className="flex flex-wrap items-center justify-between gap-3">
                      <div>
                        <h2 className="text-base font-semibold">{t("fs.notifications")}</h2>
                        <p className="mt-1 text-sm text-[var(--jf-muted)]">{t("fs.notifHint")}</p>
                      </div>
                      {canWrite && (
                        <button onClick={openNewNotification} className="jf-btn-primary px-3 py-1.5 text-xs">
                          + {t("fs.notifAdd")}
                        </button>
                      )}
                    </div>

                    <div className="mt-4 space-y-3">
                      {notifs.length === 0 && (
                        <p className="text-sm text-[var(--jf-muted)]">{t("fs.notifEmpty")}</p>
                      )}
                      {notifs.map((n) => (
                        <div key={n.id} className="rounded-lg border border-[var(--jf-border)] p-3">
                          <div className="flex flex-wrap items-center justify-between gap-2">
                            <div className="flex flex-wrap items-center gap-2">
                              <span className="text-sm font-medium">{n.name || t("fs.notifName")}</span>
                              {n.is_default && (
                                <span className="rounded-full border border-indigo-200 bg-indigo-50 px-2 py-0.5 text-[10px] font-medium text-indigo-600">
                                  {t("fs.notifDefault")}
                                </span>
                              )}
                              <span className="rounded-full border border-slate-200 bg-slate-50 px-2 py-0.5 text-[10px] text-slate-500">
                                {t(notifEventKey(n.event))}
                              </span>
                              {!n.enabled && (
                                <span className="text-[10px] text-[var(--jf-muted)]">{t("fs.notifEnabled")}: off</span>
                              )}
                            </div>
                            <div className="flex items-center gap-2">
                              <button onClick={() => openEditNotification(n)} disabled={!canWrite} className="jf-btn-ghost px-2 py-1 text-xs">
                                {t("fs.notifEdit")}
                              </button>
                              <button onClick={() => deleteNotification(n)} disabled={!canWrite} className="jf-btn-ghost px-2 py-1 text-xs text-red-700">
                                {t("fs.notifDelete")}
                              </button>
                            </div>
                          </div>
                          <div className="mt-1 text-xs text-[var(--jf-muted)]">
                            {t("fs.notifTo")}: <span className="font-mono">{n.to_email || "—"}</span>
                            {n.condition && (
                              <span className="ml-2">
                                · {condSummary(t, formBlocks.find((b) => b.id === n.condition!.field_id), n.condition.op, n.condition.value)}
                              </span>
                            )}
                          </div>
                        </div>
                      ))}
                    </div>

                    {(isNew || editing) && (
                      <div className="mt-5 rounded-lg border border-[var(--jf-border)] p-4">
                        <h3 className="text-sm font-semibold">
                          {isNew ? t("fs.notifAdd") : t("fs.notifEdit")}
                        </h3>
                        <div className="mt-3 space-y-3">
                          <div>
                            <label className={labelCls}>{t("fs.notifName")}</label>
                            <input value={edName} onChange={(e) => setEdName(e.target.value)} className={inputCls} placeholder="Admin Notification" />
                          </div>
                          <div>
                            <label className={labelCls}>{t("fs.notifEvent")}</label>
                            <select value={edEvent} onChange={(e) => setEdEvent(e.target.value)} className={inputCls}>
                              {NOTIF_EVENTS.map((ev) => (
                                <option key={ev} value={ev}>
                                  {t(notifEventKey(ev))}
                                </option>
                              ))}
                            </select>
                          </div>
                          <div>
                            <label className={labelCls} htmlFor="fs-notif-to">
                              {t("fs.notifTo")} <span className="text-[var(--jf-muted)]">{t("fs.notifToHint")}</span>{" "}
                              <a
                                href="/docs/placeholders"
                                target="_blank"
                                rel="noopener noreferrer"
                                title={t("fs.notifDocs")}
                                className="ml-1 inline-flex h-4 w-4 items-center justify-center rounded-full bg-slate-200 text-[10px] font-bold text-slate-600 hover:bg-[var(--jf-primary)] hover:text-white"
                              >
                                ?
                              </a>
                            </label>
                            <input id="fs-notif-to" value={edTo} onChange={(e) => setEdTo(e.target.value)} className={inputCls} placeholder="{admin_email}" />
                          </div>
                          <div>
                            <label className={labelCls} htmlFor="fs-notif-subject">
                              {t("fs.notifSubject")}{" "}
                              {/* The per-field language rule, stated at the field it
                                  applies to — the same affordance the per-form RECEIPT
                                  fields above carry. The rule the docs sentence describes
                                  is applied to exactly these stored values
                                  (api/internal/payments/notifications.go decides the rule's
                                  subject and body half-by-half), so a merchant editing a
                                  RULE had no pointer to it (card t_864adaee). */}
                              <a
                                href="/docs/customize#language"
                                target="_blank"
                                rel="noopener noreferrer"
                                className="text-[var(--jf-muted)] underline hover:text-slate-600"
                                title={t("set.emelLanguageRule")}
                                aria-label={t("set.emelLanguageRule")}
                              >
                                ?
                              </a>
                            </label>
                            <input id="fs-notif-subject" value={edSubject} onChange={(e) => setEdSubject(e.target.value)} className={inputCls} placeholder="Jualan baharu — {{ref_code}}" />
                          </div>
                          <div>
                            <label className={labelCls} htmlFor="fs-notif-body">
                              {t("fs.notifBody")} <span className="text-[var(--jf-muted)]">{t("fs.notifPlaceholders")}</span>{" "}
                              {/* The per-field LANGUAGE rule for this field (card
                                  t_864adaee). The placeholder link below answers a
                                  different question, so both are kept. */}
                              <a
                                href="/docs/customize#language"
                                target="_blank"
                                rel="noopener noreferrer"
                                className="text-[var(--jf-muted)] underline hover:text-slate-600"
                                title={t("set.emelLanguageRule")}
                                aria-label={t("set.emelLanguageRule")}
                              >
                                ?
                              </a>
                              <a
                                href="/docs/placeholders"
                                target="_blank"
                                rel="noopener noreferrer"
                                title={t("fs.notifDocs")}
                                className="ml-1 inline-flex h-4 w-4 items-center justify-center rounded-full bg-slate-200 text-[10px] font-bold text-slate-600 hover:bg-[var(--jf-primary)] hover:text-white"
                              >
                                ?
                              </a>
                            </label>
                            <textarea id="fs-notif-body" value={edBody} onChange={(e) => setEdBody(e.target.value)} rows={6} className={`${inputCls} font-mono text-xs`} />
                          </div>
                          <div>
                            <label className="flex items-center gap-2 text-sm text-slate-700">
                              <input type="checkbox" checked={edEnabled} onChange={(e) => setEdEnabled(e.target.checked)} />
                              {t("fs.notifEnabled")}
                            </label>
                          </div>
                          <div>
                            <p className="text-[11px] font-medium uppercase tracking-wider text-[var(--jf-muted)]">{t("fs.notifCondition")}</p>
                            {!edHasCond ? (
                              <button onClick={() => setEdHasCond(true)} className="jf-btn-ghost mt-1 px-2 py-1 text-xs">
                                + {t("fs.notifCondAdd")}
                              </button>
                            ) : (
                              <div className="mt-2 space-y-2">
                                <ConditionPicker
                                  fieldId={edCondField}
                                  op={edCondOp}
                                  value={edCondValue}
                                  fields={formBlocks}
                                  onField={setEdCondField}
                                  onOp={setEdCondOp}
                                  onValue={setEdCondValue}
                                  t={t}
                                  compact
                                />
                                <button onClick={() => setEdHasCond(false)} className="jf-btn-ghost px-2 py-1 text-xs text-slate-600">
                                  {t("fs.notifCondRemove")}
                                </button>
                              </div>
                            )}
                          </div>
                          <div className="flex items-center gap-2 pt-1">
                            <button onClick={saveNotification} disabled={!canWrite} className="jf-btn-primary">
                              {t("fs.notifSave")}
                            </button>
                            <button onClick={closeEditor} className="jf-btn-ghost text-slate-600">
                              {t("fs.notifCancel")}
                            </button>
                          </div>
                        </div>
                      </div>
                    )}
                  </section>
                  </>
                )}

                {/* ── CHIP feed ── */}
                {tab === "chip" && (
                  <section className="jf-card p-6">
                    <h2 className="text-base font-semibold">{t("fs.chip")}</h2>
                    <p className="mt-1 text-sm text-[var(--jf-muted)]">{t("fs.chipHint")}</p>
                    <div className="mt-3">
                      <span className={`rounded-full border px-2.5 py-0.5 text-[11px] font-medium ${
                        chipConfigured ? "border-emerald-200 bg-emerald-50 text-emerald-700" : "border-slate-200 bg-slate-50 text-slate-500"
                      }`}>
                        {chipConfigured ? t("fs.chipConfigured") : t("fs.chipNotConfigured")}
                      </span>
                      {chipConfigured && ovBrand && (
                        <span className="ml-2 text-[11px] text-[var(--jf-muted)]">{t("fs.chipBrand")} {ovBrand}</span>
                      )}
                    </div>
                    <div className="mt-4 space-y-3">
                      <div>
                        <label className={labelCls} htmlFor="fs-chip-brand">{t("fs.chipBrandId")}</label>
                        <input id="fs-chip-brand" value={ovBrand} onChange={(e) => setOvBrand(e.target.value)} placeholder="Brand ID" className={inputCls} />
                      </div>
                      <div>
                        <label className={labelCls} htmlFor="fs-chip-secret">{t("set.secretKey")}</label>
                        <input id="fs-chip-secret" value={ovSecret} onChange={(e) => setOvSecret(e.target.value)} type="text" placeholder="sk_live_… / sk_test_…" className={inputCls} />
                      </div>
                      <div>
                        <label className={labelCls} htmlFor="fs-chip-fallback">{t("set.emailFallback")}</label>
                        <input id="fs-chip-fallback" value={ovFallback} onChange={(e) => setOvFallback(e.target.value)} type="email" placeholder="Email fallback (one-time forms)" className={inputCls} />
                      </div>
                      <p className="text-[11px] text-[var(--jf-muted)]">{t("bld.emptyDefaults")}</p>
                      <div className="flex flex-wrap items-center gap-2 pt-1">
                        <button onClick={saveChipOverride} disabled={!canWrite || !ovBrand || !ovSecret} className="jf-btn-primary">
                          {t("bld.saveOverride")}
                        </button>
                        <button onClick={clearChipOverride} disabled={!canWrite} className="jf-btn-ghost text-slate-600">
                          {t("set.clear")}
                        </button>
                      </div>
                    </div>

                    {/* Issue #208: multiple per-form CHIP feeds with conditional logic. */}
                    <div className="mt-8 border-t border-[var(--jf-border)] pt-6">
                      <h3 className="text-base font-semibold">{t("fs.chipFeedsTitle")}</h3>
                      <p className="mt-1 text-sm text-[var(--jf-muted)]">{t("fs.chipFeedsHint")}</p>

                      <div className="mt-4 space-y-4">
                        {feeds.length === 0 && (
                          <p className="text-sm text-[var(--jf-muted)]">{t("fs.chipNotConfigured")}</p>
                        )}
                        {feeds.map((f, idx) => (
                          <div key={f.id} className="rounded-md border border-[var(--jf-border)] p-3">
                            <div className="flex flex-wrap items-center justify-between gap-2">
                              <span className="text-[11px] font-medium uppercase tracking-wider text-[var(--jf-muted)]">
                                {t("fs.chipFeedName")} #{idx + 1}
                              </span>
                              <button
                                onClick={() => removeChipFeed(f.id)}
                                disabled={!canWrite}
                                className="text-[11px] text-red-700 hover:text-red-800"
                              >
                                {t("fs.chipFeedRemove")}
                              </button>
                            </div>
                            <div className="mt-2 grid gap-3 sm:grid-cols-2">
                              <div>
                                <label className={labelCls} htmlFor={`fs-feed-${f.id}-name`}>{t("fs.chipFeedName")}</label>
                                <input
                                  id={`fs-feed-${f.id}-name`}
                                  value={f.name}
                                  onChange={(e) => updateChipFeed(f.id, { name: e.target.value })}
                                  placeholder={t("fs.chipFeedNamePh")}
                                  className={inputCls}
                                />
                              </div>
                              <div>
                                <label className={labelCls} htmlFor={`fs-feed-${f.id}-brand`}>{t("fs.chipBrandId")}</label>
                                <input
                                  id={`fs-feed-${f.id}-brand`}
                                  value={f.brand_id}
                                  onChange={(e) => updateChipFeed(f.id, { brand_id: e.target.value })}
                                  placeholder="Brand ID"
                                  className={inputCls}
                                />
                              </div>
                              <div>
                                <label className={labelCls} htmlFor={`fs-feed-${f.id}-secret`}>
                                  {t("set.secretKey")}{" "}
                                  <span className="text-[var(--jf-muted)]">{t("fs.chipSecretKeep")}</span>
                                </label>
                                <input
                                  id={`fs-feed-${f.id}-secret`}
                                  value={f.secret_key}
                                  onChange={(e) => updateChipFeed(f.id, { secret_key: e.target.value })}
                                  type="text"
                                  placeholder="sk_live_… / sk_test_…"
                                  className={inputCls}
                                />
                              </div>
                              <div>
                                <label className={labelCls} htmlFor={`fs-feed-${f.id}-fallback`}>{t("set.emailFallback")}</label>
                                <input
                                  id={`fs-feed-${f.id}-fallback`}
                                  value={f.email_fallback}
                                  onChange={(e) => updateChipFeed(f.id, { email_fallback: e.target.value })}
                                  type="email"
                                  placeholder="Email fallback (one-time forms)"
                                  className={inputCls}
                                />
                              </div>
                            </div>

                            {/* Conditional logic */}
                            <div className="mt-3">
                              <label className={labelCls} htmlFor={`fs-feed-${f.id}-cond`}>{t("fs.chipFeedConditional")}</label>
                              <div className="mt-1">
                                <select
                                  id={`fs-feed-${f.id}-cond`}
                                  value={f.condition?.field_id || ""}
                                  onChange={(e) =>
                                    updateChipFeed(f.id, {
                                      condition: e.target.value
                                        ? { ...(f.condition || { op: "equals", value: "" }), field_id: e.target.value }
                                        : null,
                                    })
                                  }
                                  className="w-full rounded border border-slate-200 px-2 py-1 text-xs sm:w-44"
                                >
                                  <option value="">{t("fs.chipFeedNoCondition")}</option>
                                  {formBlocks.map((b) => (
                                    <option key={b.id} value={b.id}>
                                      {condFieldLabel(b)} ({b.type})
                                    </option>
                                  ))}
                                </select>
                                {f.condition && (
                                  <div className="mt-2">
                                    <ConditionPicker
                                      fieldId={f.condition.field_id}
                                      op={f.condition.op}
                                      value={f.condition.value}
                                      fields={formBlocks}
                                      onField={(id) =>
                                        updateChipFeed(f.id, { condition: { ...f.condition!, field_id: id } })
                                      }
                                      onOp={(o) => updateChipFeed(f.id, { condition: { ...f.condition!, op: o } })}
                                      onValue={(v) => updateChipFeed(f.id, { condition: { ...f.condition!, value: v } })}
                                      t={t}
                                    />
                                  </div>
                                )}
                              </div>
                            </div>
                          </div>
                        ))}
                      </div>

                      <div className="mt-4 flex flex-wrap items-center gap-2">
                        <button onClick={addChipFeed} disabled={!canWrite} className="jf-btn-ghost px-3 py-1.5 text-xs">
                          + {t("fs.chipFeedAdd")}
                        </button>
                        <button onClick={saveChipFeeds} disabled={!canWrite} className="jf-btn-primary">
                          {t("fs.save")}
                        </button>
                      </div>
                    </div>
                  </section>
                )}

                {/* ── Webhooks (per-form) ── */}
                {tab === "webhooks" && (
                  <section className="jf-card p-6">
                    <h2 className="text-base font-semibold">{t("fs.webhooks")}</h2>
                    <p className="mt-1 text-sm text-[var(--jf-muted)]">{t("fs.webhooksHint")}</p>
                    <form onSubmit={addFormWebhook} className="mt-4">
                      <div className="flex flex-wrap gap-2">
                        {/* The FOURTH instance of the `min-w-56`-on-a-`flex-1`-child defect
                            (card t_ceb9d7a3). Measured on the fixed build at 320px in all
                            five locales: the row was scrollWidth 224 > clientWidth 222 and
                            the URL placeholder — a 244.98px glyph run in 198px of content —
                            rendered cut, exactly as the domains and settings webhooks rows
                            did before #709. `basis-56` + `min-w-0` keeps the 224px desktop
                            preference without the shrink floor. */}
                        <input aria-label={t("fs.webhooks")} value={whUrl} onChange={(e) => setWhUrl(e.target.value)} required type="url"
                          placeholder="https://yourapp.com/jomform-hook" className="jf-input min-w-0 max-w-xl flex-1 basis-56 sm:basis-auto" />
                        <input aria-label={t("set.signingSecret")} value={whSecret} onChange={(e) => setWhSecret(e.target.value)} required type="text"
                          placeholder={t("set.signingSecret")} className="jf-input w-48" />
                        <button className="jf-btn-primary" disabled={!canWrite}>{t("set.add")}</button>
                      </div>
                      <div className="mt-2">
                        <p className="text-xs text-slate-500">{t("webhook.eventsTitle")}</p>
                        <div className="mt-1 flex flex-wrap items-center gap-3">
                          {WEBHOOK_EVENTS.map((ev) => (
                            <label key={ev} className="flex items-center gap-1 text-sm text-slate-700">
                              <input type="checkbox" checked={whEvents.includes(ev)} onChange={() => toggleWhEvent(ev)} />
                              {ev}
                            </label>
                          ))}
                          <label className="flex items-center gap-1 text-sm text-[var(--jf-muted)]">
                            <input type="checkbox" checked={whEvents.includes("*")} onChange={() => toggleWhEvent("*")} />
                            {t("webhook.event.all")}
                          </label>
                        </div>
                      </div>
                    </form>
                    {formWebhooks.length === 0 ? (
                      <p className="mt-4 text-sm text-[var(--jf-muted)]">{t("fs.webhooksEmpty")}</p>
                    ) : (
                      <ul className="mt-4 space-y-2">
                        {formWebhooks.map((wh) => (
                          <li key={wh.id} className="flex items-center justify-between gap-3 rounded-lg border border-[var(--jf-border)] px-4 py-2.5">
                            <div className="min-w-0">
                              <p className="truncate font-mono text-xs">{wh.url}</p>
                              <p className="mt-0.5 text-[11px] text-[var(--jf-muted)]">{(wh.events || []).join(", ")}</p>
                            </div>
                            <button onClick={() => deleteFormWebhook(wh.id)} disabled={!canWrite}
                              className="shrink-0 text-xs text-slate-500 hover:text-red-700">✕</button>
                          </li>
                        ))}
                      </ul>
                    )}
                  </section>
                )}
              </div>
            )}
          </>
        )}
      </div>
      {/* Issue #391: WordPress-style media picker for the logo field. */}
      <MediaPicker
        open={logoPickerOpen}
        onClose={() => setLogoPickerOpen(false)}
        onSelect={(url, media) => {
          setFsLogo(url);
          // t_c845d0f6: a picked ROW brings its mime, so the preview can apply
          // the shared code-asset rule immediately (no lookup round-trip). A
          // pasted url brings none — the known mime is kept only while it still
          // describes this same url, otherwise the preview stays an <img>.
          setFsLogoMedia((prev) => (media ? { url, ...media } : prev && prev.url === url ? prev : null));
          setFsLogoLookupDone(true);
          setLogoPickerOpen(false);
        }}
        current={fsLogo}
      />
      {/* Issue #497: product picker for a menu category (multi-select) or the
          best-seller (single-select), scoped to the products THIS form
          references. Returns product IDs only, so the stored menu JSON shape is
          unchanged. */}
      <ProductPicker
        open={menuPickerOpen}
        onClose={() => setMenuPickerOpen(false)}
        multi={menuPickerCatIndex >= 0}
        selected={
          menuPickerCatIndex >= 0
            ? menuCategories[menuPickerCatIndex]?.product_ids || []
            : menuBestSeller
              ? [menuBestSeller]
              : []
        }
        title={
          menuPickerCatIndex >= 0 ? t("set.menuPickerTitleCategory") : t("set.menuPickerTitleBestSeller")
        }
        onlyIds={menuProducts.map((p) => p.id)}
        onConfirm={(ids) => {
          if (menuPickerCatIndex >= 0) {
            setMenuCategories((arr) =>
              arr.map((x, j) => (j === menuPickerCatIndex ? { ...x, product_ids: ids } : x))
            );
          } else {
            setMenuBestSeller(ids[0] || "");
          }
          setMenuPickerOpen(false);
        }}
      />
    </Shell>
  );
}
