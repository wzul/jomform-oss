"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Shell } from "../../components/Shell";
import { useRouter } from "next/navigation";
import { useI18n, LANGS_SHOWN, LANG_LABEL } from "../../components/i18n";
import { endonymLangProps } from "../../lib/endonymLang";
import { useSession } from "../../components/session";
import { formatDateTime } from "../../lib/formatDate";
import { formatPrice, currencyLabel } from "../../lib/money";
import { activeWorkspaceScope, workspaceQuery } from "../../lib/workspace";
import { fetchJson, runLoad } from "../../lib/wsfetch";
import { MembersTab } from "../../components/MembersTab";
import WorkspaceTab from "../../components/WorkspaceTab";
import { LegalTab } from "../../components/LegalTab";
import { Switch } from "../../components/Switch";
import { apiErr, translateApiErr } from "../../lib/apiErr";
// The save-confirmation toast. Rendered once for the whole page and bound to the
// existing `msg` state, so every save that already calls setMsg announces near
// the button that was pressed instead of only at the page top.
import { Toast } from "../../components/Toast";
import { SaveButton } from "../../components/SaveButton";
import { panelDomId, tabDomId, tabStop, tablistKeyDown } from "../../lib/tablist";
// Card t_e8b4f7da: the API's per-field verdict on a saved legal URL, and the
// dictionary keys that say why the public footer will not show it. The RULE
// (host / path / content) stays server-side; this only renders its verdict.
import { legalLinkWarningLines, type LegalLinkField, type LegalLinkStatusMap } from "../../lib/legalLinkWarning";
import { cnameTarget, workspaceAddressLabel } from "../../lib/deployment";
import { useDeployment } from "../../components/useDeployment";
// Card t_9efc5ed7: the Test/Live mode control is GONE from this page — the
// label never chose the API endpoint (ChipAPIBaseUrl does) and never changed a
// payment, so it asked a question nobody could act on. `lib/chipMode.ts` and its
// options/labels were deleted with it; `accounts.chip_mode` survives as
// write-only legacy (see api/internal/dbstore/chipmode.go).


type ChipStatus = { configured: boolean; verified?: boolean; mode?: string; secret_key_masked?: string };

type KeyRow = { id: string; name: string; scopes: string[] };
// Issue #444: affiliate marketing.
type AffiliateRow = { id: string; code: string; name: string; email: string; commission_pct: number; referral_url: string };
type DomainStatus = { domain: string; verified: boolean } | null;

// Issue #68: Settings page is organised into tabs (same pattern as Profile
// page tabs — #5) instead of one long scroll of 8 stacked sections.
//   chip      -> CHIP gateway
//   domains   -> Custom domains + Cloudflare connect (one workflow)
//   tracking  -> Tracking scripts (global GTM/analytics)
//   einvoice  -> e-Invoice TIN/MSIC
//   webhooks  -> Outbound webhooks
//   emel      -> Per-space receipt/notification email templates (issue #107)
//   members   -> Workspace members management (issue #163)
//   storefront-> Per-workspace public-form footer (issue #183)
//   legal     -> Per-workspace hosted legal pages (issue #184)
//   currency  -> Per-workspace currency + BNM rates (issue #185)
// (Connected accounts — social login connectors — live on the Profile page,
//  issue #301: they are user-level, not per-workspace. The MCP / connected-agents
//  tab moved to Profile too, issue #307: an OAuth agent is a user-level
//  credential bound to the user, not to a single workspace.)
type SettingsTab = "chip" | "domains" | "tracking" | "einvoice" | "fees" | "webhooks" | "emel" | "members" | "storefront" | "legal" | "currency" | "affiliates" | "workspace";

const TAB_IDS: SettingsTab[] = ["chip", "domains", "tracking", "einvoice", "fees", "webhooks", "emel", "members", "storefront", "legal", "currency", "affiliates", "workspace"];

function isSettingsTab(v: string | null): v is SettingsTab {
  return v === "chip" || v === "domains" || v === "tracking" || v === "einvoice" || v === "fees" || v === "webhooks" || v === "emel" || v === "members" || v === "storefront" || v === "legal" || v === "currency" || v === "affiliates" || v === "workspace";
}

// FeeRateRow is one CHIP payment method's stored charge as the API returns it
// (cards t_135c3c72 and t_1a17d22d). Amounts are integer SEN. `platform_fee_sen`
// is the flat PLATFORM FEE added to the buyer's price as its own CHIP line;
// `fee_fixed_sen`/`fee_percent_bp` are the legacy gross-up rate (basis points,
// 100 = 1%) and `legacy_grossup` says this row is still priced that way.
// `rate_label` is the server's wording ("Card 2.00%", "FPX RM1.00") so the
// dashboard cannot drift from the receipt.
type FeeRateRow = {
  method: string;
  fee_enabled: boolean;
  platform_fee_sen: number;
  fee_fixed_sen: number;
  fee_percent_bp: number;
  legacy_grossup?: boolean;
  rate_label?: string;
  updated_by?: string;
};

// FEE_METHOD_NAMES are the names the SETTINGS screen shows for the storable
// methods. The keys are the API's, never displayed raw to a merchant. fpx and
// fpx_b2b1 are two rows (card t_1d0198e3): FPX B2C and FPX B2B1 carry different
// rates, and this screen sets an amount for each (card t_69d2dd19).
const FEE_METHOD_NAMES: Record<string, string> = {
  fpx: "FPX (B2C)",
  fpx_b2b1: "FPX B2B1",
  card: "Card",
  ewallet: "e-Wallet",
  duitnow_qr: "DuitNow QR",
};

// The per-method help line lives at five LITERAL t() call sites inside the
// component (see `feeMethodDesc` below), not in a method→key map passed to
// `t(...)`: a dynamic key is a call site the i18n guard cannot resolve, and it
// spends one of the 65 it allows.

// feeSenFromRM parses a money draft into integer sen. Blank, unparseable and
// negative all mean 0 = "no fee for this method", which is the mockup's rule.
// (The field's unit is the workspace's currency, labelled by lib/money.ts.)
const feeSenFromRM = (v: string | undefined): number => {
  const n = Number((v ?? "").replace(/[^0-9.]/g, ""));
  return Number.isFinite(n) && n > 0 ? Math.round(n * 100) : 0;
};

// feeLegacyRateText is the legacy row as the amber box prints it, e.g.
// "RM0.50 + 0.80%". Percent is stored in basis points (100 = 1%).
const feeLegacyRateText = (fixedSen: number, percentBp: number, currency: string): string => {
  const parts: string[] = [];
  if (fixedSen > 0) parts.push(formatPrice(fixedSen, currency));
  if (percentBp > 0) parts.push(`${(percentBp / 100).toFixed(2)}%`);
  return parts.join(" + ");
};

// The tab the URL names, if any — `/settings#webhooks` (the hash) or
// `/settings?tab=domains` (issue #68). Readable during the CLIENT's FIRST
// render, which is why the tab state initializes from it (see `useState` below):
// a deep link then paints its OWN panel in the hydration frame instead of
// painting the stored tab and swapping to the deep-linked one on the mount
// effect. Measured on the built page with `jf_settings_tab = "affiliates"`, the
// transitions the page went through for `/settings?tab=domains` were
// `Custom domains` @169ms -> `Affiliate marketing` @226ms -> `Custom domains`
// @248ms: the middle frame is a 22ms wrong-panel flash for the reader, and it is
// also what a guard that latched onto the first frame (it waits for the domains
// input, which the FIRST frame has) then read — `e2e/affiliate-row.spec.ts`'s
// domains row threw `Cannot read properties of null (reading 'closest')` in it.
// Returns `null` on the server, so the prerendered HTML is UNCHANGED by this
// function: it is still the first tab the role filter renders, which is what the
// server's intent (no session, `loadStoredTab`'s "chip") derives to.
function tabFromUrl(): SettingsTab | null {
  if (typeof window === "undefined") return null;
  const params = new URLSearchParams(window.location.search);
  const explicit = params.get("tab");
  if (isSettingsTab(explicit)) return explicit;
  const hash = window.location.hash.replace("#", "");
  return isSettingsTab(hash) ? hash : null;
}

// Restore the last active tab. Falls back to "chip" — the pre-#163 default,
// which the role filter may hide; the render derives the tab that is actually
// shown from this intent (`activeTab` below).
function loadStoredTab(): SettingsTab {
  if (typeof window === "undefined") return "chip";
  try {
    const v = window.localStorage.getItem("jf_settings_tab");
    if (isSettingsTab(v)) return v;
  } catch {
    // storage unavailable — fall through
  }
  return "chip";
}

const METHODS = [
  // Card t_1d0198e3: FPX is TWO buyer choices at CHIP — B2C (personal) and B2B1
  // (corporate) — with different fees, so the whitelist ticks them separately and
  // the labels have to say which is which. An existing ["fpx"] keeps meaning B2C.
  { id: "fpx", label: "FPX — personal online banking (B2C)" },
  { id: "fpx_b2b1", label: "FPX B2B1 — corporate online banking" },
  { id: "card", label: "Card (Visa, Mastercard, Maestro)" },
  { id: "mpgs_google_pay", label: "Google Pay" },
  { id: "mpgs_apple_pay", label: "Apple Pay" },
  { id: "razer_atome", label: "Atome" },
  { id: "razer_grabpay", label: "GrabPay" },
  { id: "razer_maybankqr", label: "Maybank QRPay" },
  { id: "shopee_pay", label: "ShopeePay" },
  { id: "razer_tng", label: "Touch 'n Go eWallet" },
  { id: "duitnow_qr", label: "DuitNow QR" },
  { id: "crypto_coin", label: "Crypto Coin" },
];

/**
 * The payment-method whitelist as ONE comparable string (card t_f11d676f).
 *
 * The sticky Save bar has to know whether the picked methods differ from the
 * SAVED ones, and two arrays answer that badly: order is irrelevant to the
 * server and `["card","fpx"]` is not `["fpx","card"]`. Sorting and joining
 * makes "the merchant ticked nothing new" a string comparison.
 */
function methodSetKey(ids: readonly string[]): string {
  return [...ids].sort().join(",");
}

// Issue #185: supported settlement currencies (default MYR). Mirrors the
// backend CHECK constraint in migration 000029_space_currency.
const CURRENCIES = [
  { code: "MYR", name: "Malaysian Ringgit" },
  { code: "USD", name: "US Dollar" },
  { code: "SGD", name: "Singapore Dollar" },
  { code: "EUR", name: "Euro" },
  { code: "GBP", name: "British Pound" },
  { code: "AUD", name: "Australian Dollar" },
  { code: "CNY", name: "Chinese Yuan" },
  { code: "JPY", name: "Japanese Yen" },
  { code: "THB", name: "Thai Baht" },
  { code: "IDR", name: "Indonesian Rupiah" },
  { code: "PHP", name: "Philippine Peso" },
  { code: "HKD", name: "Hong Kong Dollar" },
  { code: "KRW", name: "South Korean Won" },
  { code: "INR", name: "Indian Rupee" },
  { code: "AED", name: "UAE Dirham" },
  { code: "SAR", name: "Saudi Riyal" },
  { code: "NZD", name: "New Zealand Dollar" },
  { code: "CAD", name: "Canadian Dollar" },
  { code: "CHF", name: "Swiss Franc" },
  { code: "VND", name: "Vietnamese Dong" },
  { code: "BND", name: "Brunei Dollar" },
  { code: "TWD", name: "New Taiwan Dollar" },
  { code: "MMK", name: "Myanmar Kyat" },
  { code: "KHR", name: "Cambodian Riel" },
  { code: "NPR", name: "Nepalese Rupee" },
  { code: "PKR", name: "Pakistani Rupee" },
  { code: "EGP", name: "Egyptian Pound" },
];

// Issue #128: dispatcher-supported webhook events (dispatch2.go + purchases.go).
// An explicit "*" option subscribes to all events.
const WEBHOOK_EVENTS = ["sale.paid", "sale.failed", "subscription.charged"] as const;

// The landing page's two colour fields round-trip a CONCRETE colour string:
// `<input type="color">` accepts only a colour value and the value is persisted
// as-is, so `var(--jf-…)` is not valid in this position. These are DATA
// defaults, not styling. (Card t_e2f3a553; before it, each literal appeared
// three times in this file.)
const DEFAULT_LP_PRIMARY = "#7c3aed"; // design-tokens-exempt: a persisted colour-field default, not a style
const DEFAULT_LP_BACKGROUND = "#ffffff"; // design-tokens-exempt: a persisted colour-field default, not a style

// One edge arrow of a horizontally scrolling tab strip (card t_f11d676f).
//
// Module scope, not inside `Settings`: a component declared in the render body
// is a NEW type on every render, and React answers a new type by unmounting and
// remounting the subtree (the `react-hooks/static-components` rule that the
// `Shell` nav components were moved out for). It renders the same chevron
// mirrored for the two ends, named for the direction it scrolls, so the pair is
// not two identically-named buttons to a screen reader. RTL mirrors the glyph
// in `globals.css` (`html[dir="rtl"] .jf-tabstrip-arrow svg`), because the
// button itself sits at the INLINE start/end.
function StripEdgeArrow({
  side,
  label,
  testId,
  onClick,
}: {
  side: "start" | "end";
  label: string;
  testId: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      data-testid={testId}
      className={`jf-tabstrip-arrow absolute inset-y-0 z-10 flex w-7 items-center justify-center border-b border-slate-200 bg-white text-[var(--jf-muted)] hover:text-slate-900 ${
        side === "start" ? "start-0" : "end-0"
      }`}
    >
      <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4" aria-hidden="true">
        <path
          d={side === "start" ? "M12 5l-5 5 5 5" : "M8 5l5 5-5 5"}
          stroke="currentColor"
          strokeWidth="1.8"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    </button>
  );
}

export default function Settings() {
  const { t } = useI18n();
  // Card t_48a93084: the custom-domain target and the workspace address the
  // copy names are DEPLOYMENT facts — `custom.jomform.com` and
  // `<slug>.jomform.com` exist on the hosted product only. null until
  // /api/meta answers, and the copy below prints nothing until then rather
  // than a host this install does not have.
  const deployment = useDeployment();
  const cnameTargetHost = cnameTarget(deployment);
  const router = useRouter();
  // The tab the URL names wins over the stored one, and it is resolved in the
  // INITIALIZER (not only in the mount effect below) so the client's first paint
  // already agrees with the deep link — see `tabFromUrl`.
  const [tab, setTab] = useState<SettingsTab>(() => tabFromUrl() ?? loadStoredTab());
  const [status, setStatus] = useState<ChipStatus | null>(null);
  const [brandId, setBrandId] = useState("");
  const [secretKey, setSecretKey] = useState("");
  const [secretMasked, setSecretMasked] = useState("");
  // Card t_9efc5ed7: a saved secret is a MASKED LINE with a Replace action, not
  // a second always-empty field. `replacingSecret` opens the paste field for a
  // new key; closing it (or a successful save) puts the masked line back.
  const [replacingSecret, setReplacingSecret] = useState(false);
  // The email fallback is a rare one-time-form setting, so it folds into an
  // "Advanced" disclosure. `advancedTouched` stops a background reload from
  // re-opening a disclosure the merchant has deliberately closed.
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const advancedTouched = useRef(false);
  // "Test connection" runs the live CHIP check without saving.
  const [testing, setTesting] = useState(false);
  const [emailFallback, setEmailFallback] = useState("");
  const [whitelist, setWhitelist] = useState<string[]>([]);
  // Card t_f11d676f: the CHIP card's SAVED values, so a control can say whether
  // there is anything to save. It is written from the payload the page APPLIED
  // (never from what is on screen): the sticky bar's Save is disabled while the
  // form holds exactly this, and re-enabled the moment a field or a tick moves.
  // The secret key is deliberately NOT here — it is write-only and its field is
  // empty whenever the stored key is in force, so any typing is a new key.
  // Card t_9efc5ed7 dropped `mode`: it is no longer a form field (nothing reads
  // it), so a control can no longer be dirty because of it.
  const [chipSaved, setChipSaved] = useState<{ brand: string; email: string; methods: string }>({
    brand: "",
    email: "",
    methods: "",
  });
  // The page's two message states. `msg` holds the banner/toast text and `msgOk`
  // its TONE: several of the writers below are a FAILED save, not a
  // confirmation (the tracking and e-Invoice PUTs, the CHIP unverified save), and
  // the card's rule is that a failure must not be painted in success chrome nor
  // vanish on a timer. Tone is therefore explicit state, set with the message in
  // one call — never inferred from the string, and never left stale from a
  // previous save.
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
  const [err, setErr] = useState("");
  const [keys, setKeys] = useState<KeyRow[]>([]);
  const [verified, setVerified] = useState(false);

  // Issue #444: affiliate marketing.
  const [affiliates, setAffiliates] = useState<AffiliateRow[]>([]);
  const [affCode, setAffCode] = useState("");
  const [affName, setAffName] = useState("");
  const [affCommission, setAffCommission] = useState("10");
  const [affErr, setAffErr] = useState("");
  const [affMsg, setAffMsg] = useState("");

  const [domain, setDomain] = useState("");
  const [domainInput, setDomainInput] = useState("");
  const [domainVerified, setDomainVerified] = useState(false);
  // Issue #322: per-domain auto-CNAME status ("created" | "failed" | "").
  // Issue #397: per-domain TLS config + status (tls_mode/tls_status/tls_error).
  // Issue #398: per-domain ACME email (tls_email).
  const [domains, setDomains] = useState<{ id: string; domain: string; verified: boolean; auto_cname?: string; tls_mode?: string; tls_status?: string; tls_error?: string; tls_email?: string }[]>([]);
  const [domainMsg, setDomainMsg] = useState("");
  // Issue #322: domains currently running a one-click auto-CNAME request.
  const [autoCnameBusy, setAutoCnameBusy] = useState<Record<string, boolean>>({});
  // Issue #324: domains currently being auto-polled for DNS propagation.
  const [polling, setPolling] = useState<Record<string, boolean>>({});
  // Issue #397: per-domain TLS mode selector + bring-your-own-cert upload.
  const [tlsBusy, setTlsBusy] = useState<Record<string, boolean>>({});
  const [certOpen, setCertOpen] = useState<Record<string, boolean>>({});
  const [certInput, setCertInput] = useState<Record<string, { cert: string; key: string }>>({});
  // Issue #398: per-domain ACME email editor (keyed by domain id).
  const [tlsEmailInput, setTlsEmailInput] = useState<Record<string, string>>({});
  const [tin, setTin] = useState("");
  const [msic, setMsic] = useState("");
  const [webhooks, setWebhooks] = useState<{ id: string; url: string; events: string[] }[]>([]);
  const [whUrl, setWhUrl] = useState("");
  const [whSecret, setWhSecret] = useState("");
  // Issue #128: event selection (add form defaults to all) + inline edit drawer.
  const [whEvents, setWhEvents] = useState<string[]>([...WEBHOOK_EVENTS]);
  const [editId, setEditId] = useState<string | null>(null);
  const [editUrl, setEditUrl] = useState("");
  const [editSecret, setEditSecret] = useState("");
  const [editEvents, setEditEvents] = useState<string[]>([]);
  // Issue #130: per-webhook delivery log — status indicator + expandable history.
  type Delivery = { id: string; event: string; status_code?: number | null; success: boolean; attempt: number; error?: string; created_at?: string };
  const [deliveries, setDeliveries] = useState<Record<string, Delivery[]>>({});
  const [openWh, setOpenWh] = useState<string | null>(null);
  const [trackingHead, setTrackingHead] = useState("");
  // Cloudflare OAuth link state (issue #22): Login vs Disconnect+✓ per #9.
  const [cfLinked, setCfLinked] = useState(false);
  const [cfConfigured, setCfConfigured] = useState(false);
  // Whether `/api/cf/status` has ANSWERED — and the reason the two states above
  // are not read until it has. Both start at `false`, which is the NEGATIVE
  // answer, so rendering them before the fetch resolves paints a claim the
  // deployment may not deserve. Measured on prod v1.21.252 (card t_3c0cc8ce):
  // the Domains panel rendered "Cloudflare auto-setup is not configured on this
  // deployment — add the CNAME record manually." at t=661ms and swapped to
  // "Cloudflare connected ✓" at t=1019ms on a deployment where auto-setup IS
  // configured, while the same panel simultaneously offered "Log in with
  // Cloudflare" to an account already linked. ~358ms of a false instruction to
  // do the DNS work by hand. Set on the failure path too, or a request that
  // never answers would leave the panel permanently answerless.
  const [cfStatusLoaded, setCfStatusLoaded] = useState(false);
  // Cloudflare manual connect (issue #82): API token / global key form.
  const [cfManualOpen, setCfManualOpen] = useState(false);
  const [cfKind, setCfKind] = useState<"apitoken" | "globalkey">("apitoken");
  const [cfAccName, setCfAccName] = useState("");
  const [cfAccEmail, setCfAccEmail] = useState("");
  const [cfSecret, setCfSecret] = useState("");
  const [cfManualBusy, setCfManualBusy] = useState(false);
  const [cfManualErr, setCfManualErr] = useState("");
  // Issue #68: transient banner shown after the Cloudflare OAuth callback
  // redirects back to /settings?cloudflare=linked|error.
  // The Cloudflare OAuth callback notice. It carries a SUCCESS (`connected`) and
  // a FAILURE (`linking failed`) — the same one-state-two-tones shape the `msg`
  // pair above exists to fix — so it carries its tone too and no failure is
  // painted in success chrome (card t_ad2af554, acceptance item 2).
  const [cfNotice, setCfNotice] = useState("");
  const [cfNoticeOk, setCfNoticeOk] = useState(true);
  // Issue #107: per-workspace email template + behaviour settings.
  const [emelSubject, setEmelSubject] = useState("");
  const [emelBody, setEmelBody] = useState("");
  const [emelReceiptEnabled, setEmelReceiptEnabled] = useState(true);
  const [emelNotifyMerchant, setEmelNotifyMerchant] = useState(false);
  const [emelMerchantEmail, setEmelMerchantEmail] = useState("");
  const [emelHtml, setEmelHtml] = useState(false);
  const [emelWorkspaceId, setEmelWorkspaceId] = useState("");
  // Issue #163: the active workspace's id, resolved from the session list
  // + localStorage (same key the Shell uses). Used to scope the Members tab.
  const [activeWorkspaceId, setActiveWorkspaceId] = useState("");
  // Issue #184: the active workspace's slug, used to build the public
  // legal-page URLs ({workspace}.jomform.com/privacy, /terms, /refund).
  const [activeWorkspaceSlug, setActiveWorkspaceSlug] = useState("");
  const [emelPreview, setEmelPreview] = useState(false);
  // Issue #183: per-workspace public-form footer (Storefront tab).
  const [sfBusinessName, setSfBusinessName] = useState("");
  const [sfCompanyReg, setSfCompanyReg] = useState("");
  const [sfContactPhone, setSfContactPhone] = useState("");
  const [sfContactAddress, setSfContactAddress] = useState("");
  const [sfPrivacyUrl, setSfPrivacyUrl] = useState("");
  const [sfTermsUrl, setSfTermsUrl] = useState("");
  const [sfRefundUrl, setSfRefundUrl] = useState("");
  const [sfFooterText, setSfFooterText] = useState("");
  const [sfWorkspaceId, setSfWorkspaceId] = useState("");
  // Card t_e8b4f7da: the API's own verdict on each saved legal URL — whether the
  // PUBLIC FOOTER will publish it, and when it will not, why. The rule itself
  // lives in the API (it is the footer's rule: rewriteHostedLegalURL +
  // servableHostedLegalLink); this page only renders the sentence, so the two
  // cannot drift into two different answers. Before this the merchant got
  // "Footer saved ✓" for a URL the footer silently dropped.
  const [sfLegalStatus, setSfLegalStatus] = useState<LegalLinkStatusMap>({});
  // Issue #276: per-workspace SEO indexing toggle (Storefront tab).
  const [sfIndexing, setSfIndexing] = useState(true);
  // The Save-footer PUT's in-flight state (card t_b73eee21). Presentation only:
  // it disables the button and is what makes the "busy" the card asks for
  // observable. Without it a double-click fired two PUTs of the same body.
  // The transient "Saved ✓" state of the Save-footer button. Held for 2s (the
  // timer ref below) so the confirmation is on the control the merchant clicked,
  // not only in the page-top banner several screens above it. The timer lives in
  // a ref rather than state so a re-render cannot orphan it.
  // Issue #447: per-workspace realtime notification sound toggle (Storefront tab).
  const [sfSoundEnabled, setSfSoundEnabled] = useState(false);
  // Card t_135c3c72: per-method payment-fee settings (Settings -> Payment fees).
  // Card t_69d2dd19: the editor sets the PLATFORM FEE amount per method, in RM
  // on screen and integer SEN in state/on the wire. `feeAmountDrafts` holds the
  // raw text the merchant is typing (keyed by method) so a half-typed "1." is
  // not rewritten under the cursor; the sen value is parsed at the field's edge.
  const [feeRates, setFeeRates] = useState<FeeRateRow[]>([]);
  const [feeAmountDrafts, setFeeAmountDrafts] = useState<Record<string, string>>({});
  // The legacy box stages a move in one save: the input holds the platform
  // amount the row will carry, pre-filled with its legacy fixed part.
  const [feeLegacyDrafts, setFeeLegacyDrafts] = useState<Record<string, string>>({});
  // Which method the example box narrates. Set on focus of a platform row, so
  // the merchant sees the consequence of the field they are editing.
  const [feePreviewMethod, setFeePreviewMethod] = useState("");
  const [feeAck, setFeeAck] = useState(false);
  const [feeAckBy, setFeeAckBy] = useState("");
  // Per-workspace AI spam-judge toggle (Storefront tab). Default
  // true — the judge runs unless the merchant explicitly turns it off.
  const [sfSpamJudgeEnabled, setSfSpamJudgeEnabled] = useState(true);
  // Language placement: the WORKSPACE default language for public forms — what
  // a buyer sees when a form has no per-form override. Always a real code
  // (default "en"). This is the buyer-facing language, NOT the dashboard UI
  // language (which is per-profile, on /profile).
  const [sfDefaultLang, setSfDefaultLang] = useState("en");
  // Issue #360: per-workspace customizable landing page (Storefront tab).
  const [lpEnabled, setLpEnabled] = useState(false);
  const [lpHero, setLpHero] = useState("");
  const [lpSubtitle, setLpSubtitle] = useState("");
  const [lpBody, setLpBody] = useState("");
  const [lpImageUrl, setLpImageUrl] = useState("");
  const [lpPrimaryColor, setLpPrimaryColor] = useState(DEFAULT_LP_PRIMARY);
  const [lpBackgroundColor, setLpBackgroundColor] = useState(DEFAULT_LP_BACKGROUND);
  const [lpLogoUrl, setLpLogoUrl] = useState("");
  // Issue #501: inline error for a rejected (non-https) landing image/logo,
  // shown at the field rather than only as a page-level banner.
  const [lpUrlErr, setLpUrlErr] = useState("");
  const [lpCtas, setLpCtas] = useState<{ label: string; form_slug: string }[]>([]);
  // Issue #362: published forms embedded inline on the landing page.
  const [lpForms, setLpForms] = useState<{ label: string; form_slug: string }[]>([]);
  const [lpWorkspaceId, setLpWorkspaceId] = useState("");
  // Issue #497: the restaurant menu is a PER-FORM setting now — its config
  // lives on the form and its UI is in Form Settings. Settings → Storefront no
  // longer owns a menu.
  // Issue #185: per-workspace currency + BNM rates.
  const [currency, setCurrency] = useState("MYR");
  const [currencyWorkspaceId, setCurrencyWorkspaceId] = useState("");
  const [rates, setRates] = useState<{ currency_code: string; unit: number; rate: number }[]>([]);
  const [ratesCached, setRatesCached] = useState(false);

  // Persist the active tab (issue #68).
  useEffect(() => {
    try {
      window.localStorage.setItem("jf_settings_tab", tab);
    } catch {
      // storage unavailable — ignore
    }
  }, [tab]);

  // Issue #68: auto-select the right tab from the URL on mount —
  //  /settings#webhooks, /settings?tab=domains, and the Cloudflare OAuth
  //  callback  /settings?cloudflare=linked|error. Anything not matched keeps
  //  the persisted tab.
  //
  // The tab half of this is now also done in the state initializer
  // (`tabFromUrl`), so this effect is a no-op for a deep link that has not
  // changed; it still has to run for the language change (`t` is a dependency
  // because the NOTICES it sets are translated) and for the Cloudflare callback.
  useEffect(() => {
    if (typeof window === "undefined") return;
    const params = new URLSearchParams(window.location.search);
    const cf = params.get("cloudflare");
    if (cf === "linked") {
      setTab("domains");
      setCfNoticeOk(true);
      setCfNotice(t("set.cfConnected"));
    } else if (cf === "error") {
      setTab("domains");
      setCfNoticeOk(false);
      setCfNotice(t("set.cfLinkFailed"));
    } else {
      const fromUrl = tabFromUrl();
      if (fromUrl) setTab(fromUrl);
    }
  }, [t]);

  // Issue #13: single aggregated fetch replaces the 6-request waterfall
  // (chip, keys, domains, webhooks, einvoice) on mount.
  // Issue #29: routed through the shared session cache — the first page of
  // the session fetches the bundle once; later pages (dashboard, forms,
  // products, profile) reuse it.
  // THE RULE AFTER A WRITE (card t_818349ab): call `reload()` (the wrapper
  // below), NEVER `load()`. `load` APPLIES the bundle this render closed over,
  // which after a successful write is the PRE-write payload — re-applying it is
  // what left the CHIP status badge reading NOT SET, and a just-added webhook
  // missing, until the page was reloaded. `reload()` drops the cache and
  // re-fetches, so `load` then runs against a fresh payload.
  const { bundle, bundleScope, anon, reload: reloadSession, role, user, workspace } = useSession();
  // Phone tab strip: the settings tab strip is one scrolling row, not a wrapped
  // block (see the tab bar below). This ref points at it.
  const tabStripRef = useRef<HTMLDivElement | null>(null);
  const reload = useCallback(() => {
    // Saves mutate bundle-backed sections: drop the shared cache first so
    // the next load() re-fetches fresh data (and other pages inherit it).
    reloadSession();
  }, [reloadSession]);
  // THE BUNDLE ON SCREEN MUST BE THE ACTIVE WORKSPACE'S (card t_de220c75).
  // `bundle` is workspace-scoped (chip status, keys, domains, webhooks, einvoice)
  // and a switch publishes the new scope one commit BEFORE the new payload can
  // arrive, so for that window this page holds the PREVIOUS workspace's payload.
  // Applying it is what painted `wan`'s two verified domains under the `abc`
  // chip — one switch behind, in both directions, measured on prod v1.22.91,
  // each row carrying a live Remove button for a domain that workspace does not
  // own. So the apply is gated on the payload's OWN scope (`bundleScope`, the
  // slug the session fetched it for), and while the page holds another
  // workspace's payload the LISTS it fills are emptied rather than left
  // describing a workspace nobody is looking at.
  const load = useCallback(() => {
    const apply = (b: NonNullable<ReturnType<typeof useSession>["bundle"]>) => {
      if (!b) return;
      const dList = b.domains ?? [];
      const whs = b.webhooks ?? [];
      const k = b.keys ?? [];
      const ei = b.einvoice;
      const s = b.chip_status;
      setDomains(Array.isArray(dList) ? dList : []);
      setWebhooks(Array.isArray(whs) ? whs : []);
      if (ei) { setTin(ei.tin || ""); setMsic(ei.msic || ""); }
      if (s) {
        setStatus(s);
        // Card t_f11d676f: the fields are assigned from the payload
        // UNCONDITIONALLY, and the saved baseline with them.
        //   * `&& s.whitelist.length` used to keep the previous ticks when the
        //     server held an EMPTY whitelist — i.e. after a merchant unticked
        //     everything and saved, the pills came back ticked from the stale
        //     state while the server had none. The baseline cannot be honest
        //     while a field and its saved value disagree, and the sticky Save
        //     bar is driven by that comparison.
        //   * an absent field is the empty string, which is what the input
        //     shows, so a workspace switch cannot leave the previous
        //     workspace's Brand ID in the box with a baseline that says
        //     otherwise.
        setBrandId(s.brand_id || "");
        setEmailFallback(s.email_fallback || "");
        setWhitelist(Array.isArray(s.whitelist) ? s.whitelist : []);
        setChipSaved({
          brand: s.brand_id || "",
          email: s.email_fallback || "",
          methods: methodSetKey(Array.isArray(s.whitelist) ? s.whitelist : []),
        });
        // Issue #327: the full secret is never returned — only a masked tail.
        // Never populate the editable field with a real secret; the merchant
        // can only enter a NEW key to replace the stored one (blank keeps it).
        setSecretKey("");
        setSecretMasked(s.secret_key_masked || "");
        // Issue #325: "verified" is TRUE only after a live CHIP API check
        // passed at save time — never merely because credentials are stored.
        setVerified(!!s.verified);
        // Card t_9efc5ed7: a stored fallback email is a fact the merchant should
        // be able to SEE without hunting for it, so the Advanced disclosure opens
        // itself when one is stored — until the merchant closes it themselves.
        if (!advancedTouched.current) setAdvancedOpen(!!s.email_fallback);

      }
      setKeys(Array.isArray(k) ? k : []);
    };
    if (bundle && bundleScope === workspace) { apply(bundle); return Promise.resolve(); }
    if (bundle) {
      // Another workspace's payload: the rows it carried are not this
      // workspace's rows, and every one of these lists renders a destructive
      // control against the row (`Remove` a domain, delete a webhook, revoke a
      // key). Empty is the only honest thing to paint until the active scope's
      // payload lands.
      setDomains([]);
      setWebhooks([]);
      setKeys([]);
      return Promise.resolve();
    }
    // Issue #48: when the bundle resolved as null (anonymous) there is
    // nothing to load here — the anon redirect below bounces to /landing.
    if (bundle === null) return Promise.resolve();
    // The no-bundle fallback fetches for the scope in force NOW, and drops the
    // answer if the scope moved while it was in flight — the same stale-apply
    // this card is about, one layer down.
    const q = workspaceQuery();
    return fetch(`/api/rest/dashboard-bundle${q}`)
      .then((r) => (r.ok ? r.json() : null))
      .then((b) => {
        if (!b) return;
        if (workspaceQuery() !== q) return;
        apply(b);
      });
  }, [bundle, bundleScope, workspace]);

  // Card t_135c3c72: load the stored fee rates + acknowledgement for the
  // Payment fees tab. Read directly from the REST endpoint rather than the
  // bundle. The ACKNOWLEDGEMENT SENTENCE is not read from here: the endpoint
  // serves it in English (api/internal/payments/fee.go) because a JSON call
  // carries no caller language (issue #6177), so the checkbox renders the
  // dashboard's own arm — t("set.feesAck") — for the same disclosure (t_541a31d5).
  const refreshFees = useCallback(() => {
    const q = workspaceQuery();
    return fetch(`/api/rest/settings/payment-fees${q}`)
      .then((r) => (r.ok ? r.json() : null))
      .then((b) => {
        // An answer for a workspace the merchant has since left must not land.
        if (workspaceQuery() !== q) return;
        if (!b || !Array.isArray(b.rates)) return;
        const rows = b.rates as FeeRateRow[];
        setFeeRates(rows);
        // A fixed-only legacy row IS a flat platform fee (same arithmetic, the
        // buyer's amount unchanged), so the platform field shows its fixed
        // part; a percentage row is legacy and stays in the amber box.
        setFeeAmountDrafts(
          Object.fromEntries(
            rows.map((row) => {
              const platform = row.platform_fee_sen > 0 ? row.platform_fee_sen : row.legacy_grossup ? 0 : row.fee_fixed_sen;
              return [row.method, platform > 0 ? (platform / 100).toFixed(2) : ""];
            }),
          ),
        );
        setFeeLegacyDrafts(
          Object.fromEntries(rows.map((row) => [row.method, row.fee_fixed_sen > 0 ? (row.fee_fixed_sen / 100).toFixed(2) : ""])),
        );
        setFeeAck(b.acknowledged === true);
        setFeeAckBy(typeof b.acknowledged_by === "string" ? b.acknowledged_by : "");
      })
      .catch(() => {});
  }, []);

  useEffect(() => {
    if (bundle === null) return;
    refreshFees();
  }, [bundle, bundleScope, workspace, refreshFees]);

  // Issue #242: re-fetch the custom-domain list directly from the REST
  // endpoint. load() reads bundle.domains from the closure's stale bundle
  // value, so a freshly added domain wouldn't appear until reloadSession()
  // re-renders. Fetching the list here makes new domains show immediately.
  const refreshDomains = useCallback(() => {
    // The URL is built once and compared after the read, so an answer for a
    // scope the merchant has since left cannot land in `domains` (card
    // t_de220c75 — this is the one path that also CLEARS the list to `[]`).
    const q = workspaceQuery();
    return fetch(`/api/rest/domains${q}`)
      .then((r) => (r.ok ? r.json() : null))
      .then((list) => {
        if (workspaceQuery() !== q) return;
        if (Array.isArray(list)) setDomains(list);
      });
  }, []);

  // Issue #324: verify a single domain and surface the result. Returns true
  // when the domain is now verified (so callers can stop polling).
  const verifyDomain = useCallback(
    async (d: { id: string; domain: string }): Promise<boolean> => {
      const r = await fetch(`/api/rest/domains/${d.id}/verify${workspaceQuery()}`, { method: "POST" });
      // Issue #242: the backend returns HTTP 200 with {verified:false, detail}
      // on failure, so r.ok is not enough — only treat it as verified when the
      // body says verified:true.
      const body = await r.json().catch(() => ({}));
      if (body && body.verified === true) {
        setDomainMsg(t("set.domainVerified", { domain: d.domain }));
        return true;
      }
      return false;
    },
    [t],
  );

  // Issue #324: auto-poll a pending domain every ~20s (up to ~3 minutes) so the
  // merchant doesn't have to keep clicking Verify while DNS propagates. Stops
  // as soon as the domain flips to verified.
  const startPolling = useCallback(
    (d: { id: string; domain: string }) => {
      setPolling((prev) => ({ ...prev, [d.id]: true }));
      let attempts = 0;
      const maxAttempts = 9; // 9 × 20s ≈ 3 minutes
      const tick = async () => {
        attempts += 1;
        const ok = await verifyDomain(d);
        if (ok) {
          setPolling((prev) => ({ ...prev, [d.id]: false }));
          await refreshDomains();
          return;
        }
        if (attempts >= maxAttempts) {
          setPolling((prev) => ({ ...prev, [d.id]: false }));
          setDomainMsg(t("set.verifyFailDetail"));
          return;
        }
        setTimeout(tick, 20000);
      };
      tick();
    },
    [verifyDomain, refreshDomains, t],
  );

  useEffect(() => {
    load();
  }, [load]);

  // Issue #130: populate each webhook's delivery-status indicator from the
  // REST delivery-log endpoint whenever the webhook list changes.
  useEffect(() => {
    webhooks.forEach((wh) => fetchDeliveries(wh.id));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [webhooks]);

  // Issue #107: load the per-workspace email settings for the active workspace.
  const { workspaces } = useSession();
  const loadEmailSettings = useCallback(() => {
    const ws = Array.isArray(workspaces) ? workspaces : [];
    if (!ws.length) return;
    // Resolve the active workspace (same key the Shell uses).
    let slug = "";
    try { slug = window.localStorage.getItem("jf_workspace") || ""; } catch { /* ignore */ }
    const active = ws.find((x) => x.slug === slug) || ws[0];
    if (!active) return;
    setEmelWorkspaceId(active.id);
    setActiveWorkspaceId(active.id);
    setActiveWorkspaceSlug(active.slug);
    fetch(`/api/workspaces/${active.id}/email-settings`)
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => {
        if (!d) return;
        setEmelSubject(d.receipt_subject || "");
        setEmelBody(d.receipt_body || "");
        setEmelReceiptEnabled(d.receipt_enabled !== false);
        setEmelNotifyMerchant(!!d.notify_merchant);
        setEmelMerchantEmail(d.merchant_notify_email || "");
        setEmelHtml(!!d.html);
      })
      .catch(() => {});
  }, [workspaces]);

  useEffect(() => {
    loadEmailSettings();
  }, [loadEmailSettings]);

  // Issue #183: load the per-workspace storefront/footer settings for the
  // active workspace (same resolution as the email settings above).
  const loadStorefrontSettings = useCallback(() => {
    const ws = Array.isArray(workspaces) ? workspaces : [];
    if (!ws.length) return;
    let slug = "";
    try { slug = window.localStorage.getItem("jf_workspace") || ""; } catch { /* ignore */ }
    const active = ws.find((x) => x.slug === slug) || ws[0];
    if (!active) return;
    setSfWorkspaceId(active.id);
    setCurrencyWorkspaceId(active.id);
    setLpWorkspaceId(active.id);
    fetch(`/api/workspaces/${active.id}/settings`)
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => {
        if (!d) return;
        setSfBusinessName(d.business_name || "");
        setSfCompanyReg(d.company_reg || "");
        setSfContactPhone(d.contact_phone || "");
        setSfContactAddress(d.contact_address || "");
        setSfPrivacyUrl(d.privacy_url || "");
        setSfTermsUrl(d.terms_url || "");
        setSfRefundUrl(d.refund_url || "");
        setSfFooterText(d.footer_text || "");
        // Issue #276: per-space SEO indexing toggle (default true).
        setSfIndexing(d.indexing !== false);
        // Issue #447: per-space realtime notification sound toggle (default
        // false — muted so no merchant is surprised by sound).
        setSfSoundEnabled(!!d.sound_enabled);
        // Per-space AI spam-judge toggle. `!== false` so a
        // response that predates the field (or omits it) reads as ON, which is
        // what the server actually does — the UI must not show "off" for a
        // workspace that is still judging spam.
        setSfSpamJudgeEnabled(d.spam_judge_enabled !== false);
        // Language placement: workspace default language for PUBLIC forms.
        setSfDefaultLang(d.default_lang || "en");
        // Card t_e8b4f7da: the footer's verdict on the STORED legal URLs. Loaded
        // with the settings so a merchant who saved a dropped URL before this
        // shipped still gets told — without having to press Save again.
        setSfLegalStatus((d.legal_link_status || {}) as LegalLinkStatusMap);
        if (d.currency) setCurrency(d.currency);
        // Issue #360: per-space customizable landing page.
        const lp = d.landing_page || {};
        setLpEnabled(!!lp.enabled);
        setLpHero(lp.hero || "");
        setLpSubtitle(lp.subtitle || "");
        setLpBody(lp.body || "");
        setLpImageUrl(lp.image_url || "");
        setLpPrimaryColor(lp.primary_color || DEFAULT_LP_PRIMARY);
        setLpBackgroundColor(lp.background_color || DEFAULT_LP_BACKGROUND);
        setLpLogoUrl(lp.logo_url || "");
        setLpCtas(Array.isArray(lp.ctas) ? lp.ctas : []);
        setLpForms(Array.isArray(lp.forms) ? lp.forms : []);
      })
      .catch(() => {});
    fetch(`/api/rest/currency/rates`)
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => {
        if (d) {
          setRates(Array.isArray(d.rates) ? d.rates : []);
          setRatesCached(!!d.cached);
        }
      })
      .catch(() => {});
  }, [workspaces]);

  useEffect(() => {
    loadStorefrontSettings();
  }, [loadStorefrontSettings]);

  // Issue #185: save the active space's currency.
  async function saveCurrency() {
    if (!currencyWorkspaceId) return;
    setMsg("");
    setErr("");
    const r = await fetch(`/api/workspaces/${currencyWorkspaceId}/settings`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ currency }),
    });
    if (r.ok) {
      showMsgOk(t("set.currencySaved"));
      // Card t_d5f359c7: every PRICE surface (products, subscriptions, builder,
      // shipping rates, the menu picker) labels its amount in the workspace's
      // currency, which rides in the session bundle. Dropping the bundle makes
      // the rest of the app read the NEW symbol instead of the one it cached
      // when this page was opened.
      reload();
    } else {
      const d = await r.json().catch(() => ({}));
      setErr(apiErr(d, t("set.failedSave"), t));
    }
  }

  // Issue #48: anonymous visit (401 / session expired) bounces to Landing
  // instead of hanging on a shell or flashing a logged-out settings form.
  useEffect(() => {
    if (anon) router.replace("/landing");
  }, [anon, router]);

  // Issue #503: 401/403 → /login, the same redirect Shell.signOut() uses.
  // (The bundle-backed sections bounce to /landing via `anon`; the direct
  // fetches below have no such signal, so they use this.)
  const goToLogin = useCallback(() => {
    router.replace("/login");
    window.location.href = "/login";
  }, [router]);

  // Issue #444: affiliate marketing — load the workspace's affiliates.
  // Issue #503: `if (r.ok)` had no `else`, so a 401/500 left "no affiliates
  // yet" on screen — an empty-state lie. Now it is explicit.
  const loadAffiliates = useCallback(async () => {
    if (!activeWorkspaceId) return;
    const r = await runLoad(
      async () => {
        const d = await fetchJson<unknown>(`/api/workspaces/${activeWorkspaceId}/affiliates`);
        return Array.isArray(d) ? (d as AffiliateRow[]) : [];
      },
      { onUnauth: goToLogin, message: t("set.affiliatesLoadFail"), errorKey: (b: unknown) => translateApiErr(b, t) },
    );
    if (!r.ok) {
      setAffiliates([]);
      setErr(r.error);
      return;
    }
    setAffiliates(r.data);
    setErr("");
  }, [activeWorkspaceId, t, goToLogin]);

  // `tab`, not the derived `activeTab`: this is a fetch keyed on the merchant's
  // INTENT, and `affiliates` is never role-filtered, so the two agree wherever
  // the panel can actually render (the derivation lives further down, beside the
  // role filter it depends on — reading it here would be a use-before-declare).
  useEffect(() => {
    if (tab === "affiliates") loadAffiliates();
  }, [tab, loadAffiliates]);

  async function createAffiliate() {
    if (!activeWorkspaceId) return;
    setAffErr("");
    setAffMsg("");
    const r = await fetch(`/api/workspaces/${activeWorkspaceId}/affiliates`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        code: affCode,
        name: affName,
        commission_pct: Number(affCommission) || 0,
      }),
    });
    if (r.ok) {
      setAffCode("");
      setAffName("");
      setAffCommission("10");
      setAffMsg(t("set.affiliateAdded"));
      loadAffiliates();
    } else {
      const d = await r.json().catch(() => ({}));
      setAffErr(apiErr(d, t("set.failedSave"), t));
    }
  }

  // THE CLOUDFLARE SECTION IS ACCOUNT-SCOPED, SO IT MUST BE WORKSPACE-SCOPED
  // (card t_88485157). The cf routes read and write `actx.Account` — `cfStatus`
  // reads that account's sealed token, `cfDisconnect` clears it — and each
  // workspace owns its own account. An UNSCOPED call resolves the session user's
  // FIRST-OWNED account (`resolveSessionAccount`: "Otherwise the user's first
  // owned account is used"), so the panel described and mutated a different
  // account than the one whose domains this tab lists. Measured on prod
  // v1.21.257: on a second, unlinked workspace the panel still rendered
  // "Cloudflare connected ✓ … Disconnect", and pressing Disconnect there deleted
  // the FIRST account's token — a link the user could not even see. Every call
  // below therefore carries `?workspace=`.
  //
  // `cfStatusSeq` drops an answer that is no longer about the scope on screen: a
  // switch can leave the previous request in flight, and applying it would be
  // the same lie in a different tense.
  const cfStatusSeq = useRef(0);
  // The (workspace, URL) pair already asked about. A switch must re-read even
  // when the URL it builds happens to be identical, which is why the key carries
  // `workspace` and not just the query — the effect can re-run with the same
  // scope (mount, then the session publishing one), and that first load DOES
  // send the read twice: measured on the built app, `/settings?tab=domains`
  // issued `/api/cf/status?workspace=<ws>` twice before the panel settled. One
  // extra GET on load is the cost of never applying a reply to the wrong scope;
  // the seq guard drops the superseded answer.
  const cfStatusAsked = useRef("");
  // The OAuth link's href. `/api/cf/callback` seals the token against the account
  // it resolves (see cfLinkTarget in cf_oauth.go), so the link must name the
  // workspace on screen or it links the first-owned account. Held in state and
  // updated in an effect rather than computed during render: `workspaceQuery()`
  // reads the browser scope, which the server render does not have, and building
  // it inline would be a hydration mismatch. Keyed on `workspace` (not on the
  // status fetch) so the href follows the switcher even when the section is in
  // its pending state.
  //
  // IT STARTS EMPTY, NOT `/api/cf/start` (card t_a7e3c4d4). A COLD mount — no
  // `jf_workspace` in localStorage — renders with `workspace === ""` until the
  // session publishes the scope (~500ms on prod), and the pre-fix initial value
  // was the BARE start URL. On a first-owned account with no credential the
  // panel answers `cfStatusLoaded && !cfLinked` inside that window and painted
  // an unscoped "Log in with Cloudflare"; a click there started an OAuth flow
  // whose callback seals against `Accounts.ByOwner(...)[0]` — the wrong-account
  // write #1350 fixed, still reachable by a click. An empty href cannot be
  // painted (the anchor is also gated on a non-empty href, and on
  // `cfStatusLoaded`, which only a SCOPED read ever sets).
  const [cfStartHref, setCfStartHref] = useState("");
  useEffect(() => {
    // Built from `workspace`, the session-published scope — the same value the
    // read below is gated on, so the href can never name a scope the gate did
    // not check (`workspaceQuery()` reads the module-level scope, which the
    // render has not necessarily caught up with).
    setCfStartHref(workspace ? `/api/cf/start?workspace=${encodeURIComponent(workspace)}` : "");
  }, [workspace]);

  const loadCfStatus = useCallback(() => {
    // NO /api/cf REQUEST MAY LEAVE BEFORE THE SCOPE IS KNOWN (card t_a7e3c4d4).
    // The panel is the one section whose every route is ACCOUNT-scoped
    // (cf_oauth.go reads and writes `actx.Account`), and an UNSCOPED session
    // call resolves to the user's FIRST-OWNED account. On a cold mount
    // `workspaceQuery()` used to answer "" and this effect fired anyway:
    // measured on prod v1.21.261, `GET /api/cf/status` with NO query at t=186ms,
    // answered for the first-owned account, before the scoped read at t=546ms —
    // and it was APPLIED, because the scoped request is issued after the bare
    // answer lands, so `cfStatusSeq` had nothing newer to drop it for. The panel
    // painted another account's link state for ~360ms.
    //
    // The gate is `workspace` (the session's published scope), not
    // `workspaceQuery()`: the URL is then built from the very value that was
    // checked, so an unscoped call is structurally impossible rather than
    // merely unlikely.
    if (!workspace) {
      // …and the previous scope's answer must not survive the drop: the badge
      // and the Disconnect button are gated on `cfLinked`, so a stale `true`
      // here would describe an account the panel can no longer name.
      setCfLinked(false);
      setCfConfigured(false);
      setCfStatusLoaded(false);
      return Promise.resolve();
    }
    const q = `?workspace=${encodeURIComponent(workspace)}`;
    const asked = `${workspace}|${q}`;
    if (cfStatusAsked.current === asked) return Promise.resolve();
    cfStatusAsked.current = asked;
    const seq = ++cfStatusSeq.current;
    // The PREVIOUS workspace's answer is not this workspace's answer, and the
    // badge and the Disconnect button are gated on `cfLinked` rather than on
    // `cfStatusLoaded` — so they must be dropped here or they keep describing an
    // account the merchant has switched away from while the new read is in
    // flight.
    setCfLinked(false);
    setCfConfigured(false);
    setCfStatusLoaded(false);
    return fetch(`/api/cf/status${q}`)
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => {
        if (seq !== cfStatusSeq.current) return; // a newer scope was asked for
        if (d) {
          setCfLinked(!!d.linked);
          setCfConfigured(d.configured !== false);
        }
      })
      .catch(() => {})
      // BOTH paths, deliberately: a rejected fetch or a non-2xx answer is not an
      // answer either, and a flag set only where the JSON parsed would leave the
      // panel with no `cfNotConfigured` note and no explanation — a section that
      // never settles. See the `cfStatusLoaded` declaration for the measured
      // defect this flag exists to prevent.
      .finally(() => {
        if (seq === cfStatusSeq.current) setCfStatusLoaded(true);
      });
  }, [workspace]);

  useEffect(() => {
    loadCfStatus();
  }, [loadCfStatus]);

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setMsg("");
    setErr("");
    const r = await fetch(`/api/rest/settings/chip${workspaceQuery()}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        brand_id: brandId,
        secret_key: secretKey,
        // Card t_9efc5ed7: no `mode`. The Test/Live control is gone because the
        // label changes no payment — ChipAPIBaseUrl picks the endpoint and the
        // key itself decides what CHIP does. The API still ACCEPTS the field for
        // older clients and ignores it; this client no longer sends it.
        email_fallback: emailFallback,
        payment_method_whitelist: whitelist,
      }),
    });
    if (r.ok) {
      // Issue #325: the backend only reports verified:true after a live CHIP
      // API check passed. Trust that value — never assume success.
      const d = await r.json().catch(() => ({}));
      setVerified(d.verified === true);
      // Card t_818349ab: the status badge and the "✓ API credentials verified"
      // marks are rendered from THIS state, and this branch used to end with
      // `await load()` — which APPLIES `bundle`, the payload this render closed
      // over, i.e. the pre-save one. It wrote the old `chip_status` back over
      // the fresh answer, so the badge stayed NOT SET until the merchant
      // reloaded the page (the reported bug; reproduced in
      // `web/e2e/settings-save-reflects-status.spec.ts`). The write's own answer
      // is authoritative for the fields it carries…
      const configured = typeof d.status === "string" ? d.status === "configured" : true;
      setStatus((prev) => ({
        ...(prev || { configured }),
        configured,
        verified: d.verified === true,
        mode: typeof d.mode === "string" ? d.mode : prev?.mode,
      }));
      // Card t_9efc5ed7: a key that was just stored is now a masked line, so the
      // paste field closes and the next "Replace" is a deliberate action.
      setSecretKey("");
      setReplacingSecret(false);
      // A save that reached the API but whose CHIP check could not be confirmed
      // is NOT a clean success. It is shown on the failure tone so the merchant
      // reads the caveat on the same chrome as its meaning (the sentence itself
      // already says the check was not confirmed).
      (d.verified === true ? showMsgOk : showMsgFail)(t(d.verified === true ? "set.savedCreds" : "set.savedCredsUnverified"));
      // …and the fields it does not echo (brand id, the masked key tail, the
      // method whitelist) come from a FRESH bundle: `reload()` drops the shared
      // session cache and re-fetches, where `load()` only re-read the cached one.
      reload();
    } else {
      const d = await r.json().catch(() => ({}));
      setVerified(false);
      setErr(apiErr(d, t("set.failedSave"), t));
    }
  }

  // Card t_9efc5ed7 — the card's "Test connection" action: run the SAME live
  // CHIP credential check the save path runs, without storing anything. A key the
  // merchant has just pasted is the one tested; with the field closed the stored
  // key is tested (empty body). The API answers 400 with `verified:false` when
  // CHIP refuses the pair, so the failure tone is the API's, not a guess.
  async function testConnection() {
    setMsg("");
    setErr("");
    setTesting(true);
    try {
      const r = await fetch(`/api/rest/settings/chip/test${workspaceQuery()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ secret_key: secretKey.trim() }),
      });
      const d = await r.json().catch(() => ({}));
      if (r.ok) showMsgOk(t("set.testConnectionOk"));
      else showMsgFail(apiErr(d, t("set.testConnectionFailed"), t));
    } catch {
      showMsgFail(t("set.testConnectionFailed"));
    } finally {
      setTesting(false);
    }
  }

  async function clearCreds() {
    setMsg("");
    setErr("");
    const r = await fetch(`/api/rest/settings/chip${workspaceQuery()}`, { method: "DELETE" });
    if (!r.ok) {
      // A refused DELETE used to be painted as a successful Disconnect.
      const d = await r.json().catch(() => ({}));
      setErr(apiErr(d, t("set.failedSave"), t));
      return;
    }
    showMsgOk(t("set.credsCleared"));
    // Card t_818349ab: the same root cause as `save()` above. `load()` re-applies
    // the bundle this render closed over — still "configured" — so a successful
    // Disconnect left the badge reading ACTIVE · TEST. Publish the cleared state
    // from the answer, then refresh the bundle for everything else.
    setStatus((prev) => ({ ...(prev || { configured: false }), configured: false, verified: false, mode: "" }));
    setVerified(false);
    setSecretMasked("");
    // Clearing removes the stored key, so the masked line goes with it and the
    // paste field is the next thing the merchant needs (card t_9efc5ed7).
    setSecretKey("");
    setReplacingSecret(false);
    reload();
  }

  async function addWebhook(e: React.FormEvent) {
    e.preventDefault();
    setErr("");
    const r = await fetch(`/api/rest/webhooks${workspaceQuery()}`, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ url: whUrl, secret: whSecret, events: whEvents }),
    });
    if (r.ok) {
      setWhUrl(""); setWhSecret(""); setWhEvents([...WEBHOOK_EVENTS]);
      // Card t_818349ab: `webhooks` is bundle-backed, and `load()` re-applies
      // the bundle this render closed over — the list from BEFORE the POST — so
      // the new hook only appeared after a page reload. `reload()` re-fetches.
      reload();
    } else {
      // Issue #128: show the backend's error inline instead of swallowing it.
      const d = await r.json().catch(() => ({}));
      setErr(t("set.whAddFail", { error: apiErr(d, t("set.failedSave"), t) }));
    }
  }

  // Issue #128: persist edits via the new PUT /v1/rest/webhooks/{id} endpoint.
  async function saveWebhook(e: React.FormEvent) {
    e.preventDefault();
    if (!editId) return;
    setErr("");
    const r = await fetch(`/api/rest/webhooks/${editId}${workspaceQuery()}`, {
      method: "PUT", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ url: editUrl, secret: editSecret, events: editEvents }),
    });
    if (r.ok) {
      showMsgOk(t("set.whUpdated"));
      setEditId(null); setEditUrl(""); setEditSecret(""); setEditEvents([]);
      // Card t_818349ab: same stale re-apply as `addWebhook` above.
      reload();
    } else {
      const d = await r.json().catch(() => ({}));
      setErr(t("set.whUpdateFail", { error: apiErr(d, t("set.failedSave"), t) }));
    }
  }

  function toggleWhEvent(target: string[], setter: (v: string[]) => void, ev: string) {
    setter(target.includes(ev) ? target.filter((x) => x !== ev) : [...target, ev]);
  }

  // Issue #128: open (or close) the inline edit drawer for a webhook row.
  function openEdit(wh: { id: string; url: string; events: string[] }) {
    setEditId(wh.id);
    setEditUrl(wh.url);
    setEditSecret("");
    setEditEvents([...(wh.events && wh.events.length ? wh.events : ["sale.paid"])]);
    setErr("");
  }
  function closeEdit() {
    setEditId(null); setEditUrl(""); setEditSecret(""); setEditEvents([]);
  }

  // Issue #130: fetch the latest deliveries for one webhook via the REST
  // endpoint (account-scoped server-side). Called when a row is expanded or
  // on mount to populate the status indicator.
  async function fetchDeliveries(webhookID: string) {
    try {
      const r = await fetch(`/api/rest/webhooks/${webhookID}/deliveries?limit=10&workspace=${encodeURIComponent(activeWorkspaceScope())}`);
      if (!r.ok) return;
      const list = (await r.json().catch(() => ([]))) as Delivery[];
      setDeliveries((prev) => ({ ...prev, [webhookID]: Array.isArray(list) ? list : [] }));
    } catch {
      // network blip — leave existing state untouched
    }
  }

  // Issue #197: manually retry a failed webhook delivery (or a dead-lettered
  // retry) immediately. Re-enqueues it so the background sweep re-delivers.
  // Dead-lettered rows (no `success` field) are retried via the retries
  // endpoint; failed deliveries via the deliveries endpoint.
  async function retryDelivery(webhookID: string, row: Delivery) {
    setErr("");
    try {
      const isDeadLettered = typeof row.success !== "boolean";
      const path = isDeadLettered
        ? `/api/rest/webhooks/${webhookID}/retries/${row.id}/retry`
        : `/api/rest/webhooks/${webhookID}/deliveries/${row.id}/retry`;
      const r = await fetch(`${path}?workspace=${encodeURIComponent(activeWorkspaceScope())}`, { method: "POST" });
      if (r.ok) {
        showMsgOk(t("wh.retryQueued"));
        await fetchDeliveries(webhookID);
      } else {
        const d = await r.json().catch(() => ({}));
        setErr(apiErr(d, t("wh.retryFail"), t));
      }
    } catch {
      setErr(t("wh.retryFail"));
    }
  }

  // Issue #195: MCP request history moved to Profile → Connected agents
  // (issue #307); nothing to fetch here in space-scoped Settings.

  // Derived per-webhook status for the indicator: pending (last failed, more
  // retries remain), success (last attempt ok), failed (last attempt failed).
  function deliveryStatus(list: Delivery[] | undefined) {
    const latest = list && list.length ? list[0] : undefined;
    if (!latest) return null; // no deliveries yet
    if (latest.success) return "success";
    if (latest.attempt < 6) return "pending"; // auto-retry is still scheduled
    return "failed";
  }

  // Issue #307: the MCP "AI Agent Key" generator moved to Profile → Connected
  // agents (user-level), so there is no genKey here anymore.

  async function setDomainClaim(e: React.FormEvent) {
    e.preventDefault();
    setDomainMsg("");
    const r = await fetch(`/api/rest/domains${workspaceQuery()}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ domain: domainInput }),
    });
    if (r.ok) {
      const d = await r.json();
      setDomainInput("");
      // Issue #322: surface the auto-CNAME result instead of silent best-effort.
      if (d.auto_cname === "created") {
        setDomainMsg(t("set.autoCnameCreated"));
      } else if (d.auto_cname === "failed") {
        setDomainMsg(t("set.autoCnameFailed", { error: d.cloudflare || "" }));
      } else {
        setDomainMsg(t("set.domainAdded", { domain: d.domain, target: cnameTargetHost }));
      }
      await refreshDomains();
      // Issue #324: auto-poll the freshly added domain so the merchant doesn't
      // have to keep clicking Verify while DNS propagates.
      if (d && d.id) startPolling({ id: d.id, domain: d.domain });
    } else {
      const d = await r.json().catch(() => ({}));
      setDomainMsg(apiErr(d, t("set.failedAddDomain"), t));
    }
  }

  // Issue #322: one-click auto-CNAME for a domain via the linked Cloudflare
  // account. Surfaces success/failure instead of silently falling back.
  async function autoCreateCname(d: { id: string; domain: string }) {
    setAutoCnameBusy((prev) => ({ ...prev, [d.id]: true }));
    setDomainMsg("");
    try {
      const r = await fetch(`/api/rest/domains/${d.id}/auto-cname${workspaceQuery()}`, { method: "POST" });
      const res = await r.json().catch(() => ({}));
      if (res.auto_cname === "created") {
        setDomainMsg(t("set.autoCnameCreated"));
      } else if (res.auto_cname === "failed") {
        setDomainMsg(t("set.autoCnameFailed", { error: res.error || "" }));
      } else {
        setDomainMsg(t("set.autoCnameNotLinked"));
      }
      await refreshDomains();
    } finally {
      setAutoCnameBusy((prev) => ({ ...prev, [d.id]: false }));
    }
  }

  function toggleMethod(id: string) {
    setWhitelist((prev) => (prev.includes(id) ? prev.filter((m) => m !== id) : [...prev, id]));
  }

  // Issue #397: set the per-domain TLS mode (auto | custom).
  async function setDomainTlsMode(d: { id: string; domain: string }, mode: string) {
    setTlsBusy((prev) => ({ ...prev, [d.id]: true }));
    setDomainMsg("");
    try {
      const r = await fetch(`/api/rest/domains/${d.id}/tls-mode${workspaceQuery()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ mode }),
      });
      if (r.ok) {
        setDomainMsg(mode === "auto" ? t("set.tlsModeAuto") : t("set.tlsModeCustom"));
      } else {
        const e = await r.json().catch(() => ({}));
        setDomainMsg(apiErr(e, t("set.tlsModeFailed"), t));
      }
      await refreshDomains();
    } finally {
      setTlsBusy((prev) => ({ ...prev, [d.id]: false }));
    }
  }

  // Issue #398: set the per-domain ACME/Let's Encrypt contact email.
  async function setDomainTlsEmail(d: { id: string; domain: string }) {
    const email = (tlsEmailInput[d.id] ?? "").trim();
    setTlsBusy((prev) => ({ ...prev, [d.id]: true }));
    setDomainMsg("");
    try {
      const r = await fetch(`/api/rest/domains/${d.id}/tls-email${workspaceQuery()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email }),
      });
      if (r.ok) {
        setDomainMsg(t("set.tlsEmailSaved"));
      } else {
        const e = await r.json().catch(() => ({}));
        setDomainMsg(apiErr(e, t("set.tlsEmailFailed"), t));
      }
      await refreshDomains();
    } finally {
      setTlsBusy((prev) => ({ ...prev, [d.id]: false }));
    }
  }

  // Issue #397: upload a bring-your-own-cert (cert + key PEM) for a domain.
  async function uploadCustomCert(d: { id: string; domain: string }) {
    const inp = certInput[d.id];
    if (!inp || !inp.cert.trim() || !inp.key.trim()) {
      setDomainMsg(t("set.tlsCertRequired"));
      return;
    }
    setTlsBusy((prev) => ({ ...prev, [d.id]: true }));
    setDomainMsg("");
    try {
      const r = await fetch(`/api/rest/domains/${d.id}/custom-cert${workspaceQuery()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ cert: inp.cert, key: inp.key }),
      });
      if (r.ok) {
        setDomainMsg(t("set.tlsCertSaved"));
        setCertOpen((prev) => ({ ...prev, [d.id]: false }));
        setCertInput((prev) => ({ ...prev, [d.id]: { cert: "", key: "" } }));
      } else {
        const e = await r.json().catch(() => ({}));
        setDomainMsg(apiErr(e, t("set.tlsCertFailed"), t));
      }
      await refreshDomains();
    } finally {
      setTlsBusy((prev) => ({ ...prev, [d.id]: false }));
    }
  }

  // Issue #107: save the per-space email settings.
  async function saveEmailSettings() {
    if (!emelWorkspaceId) return;
    setMsg("");
    setErr("");
    const r = await fetch(`/api/workspaces/${emelWorkspaceId}/email-settings`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        receipt_subject: emelSubject,
        receipt_body: emelBody,
        receipt_enabled: emelReceiptEnabled,
        notify_merchant: emelNotifyMerchant,
        merchant_notify_email: emelMerchantEmail,
        html: emelHtml,
      }),
    });
    if (r.ok) {
      showMsgOk(t("set.emelSaved"));
    } else {
      const d = await r.json().catch(() => ({}));
      setErr(apiErr(d, t("set.failedSave"), t));
    }
  }

  // Issue #183: save the per-space public-form footer (Storefront tab).
  //
  // The body, the endpoint, the toggle variables and the `setMsg` strings are
  // untouched (card t_b73eee21 is presentation). What is new is only the busy
  // flag: without it the button could be pressed again while the PUT was still
  // open, so one impatient double-click sent the settings twice and the second
  // response overwrote the first message. Measured on the built page before
  // this: two clicks in flight produced putCount 2 with no visible feedback on
  // the button.
  // Returns whether the write succeeded, so SaveButton only confirms on success:
  // a `false` leaves the button idle and the page's error is the only claim.
  async function saveStorefrontSettings(): Promise<boolean> {
    if (!sfWorkspaceId) return false;
    setMsg("");
    setErr("");
    try {
      const r = await fetch(`/api/workspaces/${sfWorkspaceId}/settings`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          business_name: sfBusinessName,
          company_reg: sfCompanyReg,
          contact_phone: sfContactPhone,
          contact_address: sfContactAddress,
          privacy_url: sfPrivacyUrl,
          terms_url: sfTermsUrl,
          refund_url: sfRefundUrl,
          footer_text: sfFooterText,
          // Issue #276: per-space SEO indexing toggle.
          indexing: sfIndexing,
          // Issue #447: per-space realtime notification sound toggle.
          sound_enabled: sfSoundEnabled,
          // Per-space AI spam-judge toggle.
          spam_judge_enabled: sfSpamJudgeEnabled,
          // Language placement: workspace default language for public forms
          // (buyer-facing). Always a real code — the column is NOT NULL with a
          // CHECK on the five locales, so "English" is sent as "en", not "".
          default_lang: sfDefaultLang,
        }),
      });
      if (r.ok) {
        // Card t_e8b4f7da: the save succeeded, but a legal URL the footer will
        // DROP must not be confirmed silently — that was the reported bug
        // ("Footer saved ✓" + an empty footer + no reason). The API answers with
        // its own verdict per field; keep it so the fields can say why. A body
        // that cannot be read must NOT cost the confirmation: the save DID land.
        const d = await r.json().catch(() => ({}));
        setSfLegalStatus(((d as { legal_link_status?: LegalLinkStatusMap })?.legal_link_status || {}) as LegalLinkStatusMap);
        showMsgOk(t("set.sfSaved"));
        return true;
      } else {
        const d = await r.json().catch(() => ({}));
        setErr(apiErr(d, t("set.failedSave"), t));
      }
    } catch {
      setErr(t("set.failedSave"));
    }
    // A failure must NOT confirm on the button.
    return false;
  }

  // Card t_e8b4f7da: what a legal-URL field says when the PUBLIC FOOTER will not
  // publish that link. The verdict is the API's (the footer's own rule) and the
  // wording comes from lib/legalLinkWarning — this only draws it, so no host or
  // path test is re-implemented here.
  //
  // The two `t()` calls are a literal ternary rather than `t(l.key, …)`: a
  // computed key lands in i18nKeys.ts's unresolved remainder, and the dictionary
  // entry for these two strings is what makes the warning readable at all — a raw
  // `set.sfLegalLinkNotServed` rendered to the merchant is the same class of bug
  // as showing no warning.
  function legalUrlWarning(field: LegalLinkField) {
    const lines = legalLinkWarningLines(sfLegalStatus[field]);
    if (!lines.length) return null;
    return (
      <>
        {lines.map((l) => (
          <p key={l.key} className="text-xs font-medium text-amber-800">
            {l.key === "set.sfLegalLinkNotServed"
              ? t("set.sfLegalLinkNotServed", l.vars)
              : t("set.sfLegalLinkNoContent", l.vars)}
          </p>
        ))}
      </>
    );
  }

  // Issue #360: save the per-space customizable landing page (Storefront tab).
  // Same boolean contract as saveStorefrontSettings.
  //
  // THE THROWN/network FAILURE (card t_9fad3d6a). The `fetch` used to sit
  // OUTSIDE a `try`. A rejection — lost connectivity, an aborted request — then
  // propagated out of this function, `SaveButton`'s own `catch` read it as "not
  // saved" (correct: the button stays idle), and NOTHING wrote the page's error
  // state, so the merchant was told nothing at all. Measured in a real browser
  // with the PUT aborted at the browser level: button idle, `banners after: []`,
  // `toast after: null`, no page error, and no new text anywhere on the page.
  // They could not tell "saved" from "not saved".
  //
  // The catch writes the SAME element and the SAME sentence as this handler's
  // own HTTP-refusal branch below (`setLpUrlErr` with `set.failedSave`), so a
  // refused request and a request that never arrived answer identically, in the
  // one place the merchant is looking: the red banner at the landing fields,
  // immediately above the button they pressed. `setErr` was the alternative —
  // it is what the sibling footer handler uses — but on this page `err` renders
  // in the banner at the very TOP of a 2600-line page, several screens from the
  // button, which is exactly the distance this tab's SaveButton/Toast work
  // (PR #1017) exists to remove.
  async function saveLandingPage(): Promise<boolean> {
    if (!lpWorkspaceId) return false;
    setMsg("");
    setErr("");
    setLpUrlErr("");
    // Issue #501: a non-https landing image/logo would render as the
    // html/template #ZgotmplZ sentinel (a silently broken image) on the
    // storefront. Catch it at the field and surface the server's rejection
    // message too, since the server is the authority.
    for (const u of [lpImageUrl, lpLogoUrl]) {
      if (u.trim() && !u.trim().startsWith("https://")) {
        setLpUrlErr(t("url.notHttps"));
        return false;
      }
    }
    try {
      const r = await fetch(`/api/workspaces/${lpWorkspaceId}/settings`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          landing_page: {
            enabled: lpEnabled,
            hero: lpHero,
            subtitle: lpSubtitle,
            body: lpBody,
            image_url: lpImageUrl,
            primary_color: lpPrimaryColor,
            background_color: lpBackgroundColor,
            logo_url: lpLogoUrl,
            ctas: lpCtas,
            forms: lpForms,
          },
        }),
      });
      if (r.ok) {
        showMsgOk(t("set.lpSaved"));
        return true;
      } else {
        const d = await r.json().catch(() => ({}));
        setLpUrlErr(apiErr(d, t("set.failedSave"), t));
      }
    } catch {
      // A request that never arrived is a FAILED SAVE, and it must be on screen:
      // the button correctly stays idle, so this message is the only claim.
      setLpUrlErr(t("set.failedSave"));
    }
    // A failure must NOT confirm on the button.
    return false;
  }

  // Issue #107: render a client-side preview of the template with sample data.
  function renderPreview(src: string): string {
    const sample: Record<string, string> = {
      "{{ref_code}}": "JF-ABC123",
      "{{amount}}": "123.45",
      "{{items}}": "1× Tee",
      "{{form_url}}": "https://acme.jomform.com/tee",
      "{{buyer_name}}": "Ali",
      "{{date}}": new Date().toISOString().slice(0, 10),
    };
    let out = src;
    for (const [k, v] of Object.entries(sample)) out = out.split(k).join(v);
    return out;
  }

  // Issue #163: role-gate the settings tabs. CHIP credentials, member
  // management and the Workspace tab are admin-only; read_only members see no
  // write tabs at all.
  //
  // `workspace` BELONGS IN THIS LIST because its PANEL is admin-gated
  // (`{activeTab === "workspace" && isAdmin && …}` below, #682). A tab the strip
  // renders whose panel branch is gated further down is a dead end: clicking it
  // selects a tab and paints NOTHING under the strip. Measured on the built page
  // as a `manage` member at 320px, before this line changed:
  //
  //   after clicking Workspace {"tabCount":10,"selectedCount":1,
  //     "selected":"Workspace","h2Count":0,"sectionCount":0,"mainTextLen":105}
  //
  // — h2Count 0 is the pane: the h1 and the banners only. The strip and the
  // panel have to agree on WHICH TABS EXIST, which is the same contract the
  // `activeTab` derivation below enforces from the other side (an intent the
  // strip cannot render falls back to one it can). Card t_0ef9709c.
  const ADMIN_ONLY_TABS: readonly SettingsTab[] = ["chip", "fees", "members", "workspace"];
  const isAdmin = role === "admin" || role === "owner";
  const isReadOnly = role === "read_only";
  const visibleTabs: SettingsTab[] = isReadOnly
    ? []
    : TAB_IDS.filter((id) => !ADMIN_ONLY_TABS.includes(id) || isAdmin);

  // The tab that is actually RENDERED, derived from `tab` (the intent: the
  // stored / URL / clicked tab) and the role filter above.
  //
  // `tab` can name a tab `visibleTabs` does not contain, and on a FIRST RUN it
  // always does: `loadStoredTab` falls back to "chip" (the pre-#163 default) and
  // "chip" is admin-only. Keying the panel branches on `tab` then renders the
  // CHIP panel while no rendered tab is `tab` — so the strip carried ZERO
  // `aria-selected` (a `role="tab"` list is expected to carry exactly one) and
  // the panel did not match any visible tab. Measured on a browser with no
  // storage at all: ten tabs, selectedCount 0, CHIP panel underneath. The same
  // hole opens whenever the role is demoted (stored "chip"/"members") or a
  // deep-link names a hidden tab.
  //
  // So the VIEW is derived: an intent the strip cannot render falls back to the
  // first tab it CAN render (no tab is filtered out unless the list is empty),
  // and stays `null` only when the strip is empty — the `read_only` state, where
  // no tab is shown and therefore no panel may be either (previously read_only
  // rendered the CHIP panel below the read-only notice).
  //
  // `tab` itself is left alone on purpose: it is the merchant's intent, the
  // persist effect below writes it, and an admin's stored "chip" must survive a
  // first paint that has not resolved `role` yet (clamping in state would
  // overwrite it before the session landed).
  const activeTab: SettingsTab | null =
    visibleTabs.includes(tab) ? tab : (visibleTabs[0] ?? null);

  // Keep a tab inside the strip's visible window.
  //
  // The strip is one scrolling row (`overflow-x-auto`), so a tab that is
  // selected but off-screen is a dead end on a phone: measured at 320px the
  // default tab (Affiliates, the last of the eleven) sat 867px inside a
  // 272px-wide box.
  //
  // `visibleTabs.length` is a dependency, not just `tab`: the tab set GROWS
  // once the session bundle resolves (`role` arrives with it, which is what
  // adds CHIP + Members), and the first paint after that change has to reveal
  // the selection again. A `tab`-only effect runs before those two tabs exist
  // and leaves the strip scrolled to the start edge.
  //
  // `behavior: "instant"` is deliberate, not a missed `"smooth"`: a smooth
  // reveal animates the strip under the reader's thumb on first paint, and the
  // reveal only has to be correct in the painted frame.
  //
  // `block: "nearest"` (not `"start"`) is what keeps the PAGE still: the tab is
  // already inside the strip vertically, so the vertical position is untouched.
  const revealTab = useCallback((el: HTMLElement | null) => {
    if (!el) return;
    // A browser without the options object scrolls instantly anyway.
    try {
      el.scrollIntoView({ block: "nearest", inline: "nearest", behavior: "instant" });
    } catch {
      el.scrollIntoView(false);
    }
  }, []);

  useEffect(() => {
    revealTab(tabStripRef.current?.querySelector<HTMLElement>('[role="tab"][aria-selected="true"]') ?? null);
  }, [revealTab, activeTab, visibleTabs.length]);

  // WHICH EDGE OF THE STRIP STILL HIDES A TAB (card t_f11d676f).
  //
  // The strip is one scrolling row, and a row whose ninth tab is clipped at the
  // right edge with no fade, arrow or scrollbar reads as a COMPLETE row: the
  // merchant has no reason to swipe. Measured on the built page at 390px: 11
  // tabs in a 342px box, `scrollWidth` 1,013 — two thirds of the tab set was off
  // screen behind an edge that looked like the end.
  //
  // So the strip reports where its content is hidden (`data-edge`, consumed by
  // `.jf-tabstrip` in globals.css: a fade over that edge) and the fade follows
  // the thumb instead of sitting there permanently: at the start edge only the
  // END fades, at the end edge only the START does. A strip that does not
  // overflow reports `none` and carries no mask at all.
  //
  // `Math.abs(scrollLeft)` is what makes this RTL-correct without asking the
  // direction: the strip starts at scrollLeft 0 in LTR and at 0 in RTL too
  // (scrollLeft goes NEGATIVE from there in Chromium/Firefox), so distance from
  // the start edge is the absolute value in both writing modes.
  const [stripEdge, setStripEdge] = useState<"none" | "start" | "end" | "both">("none");
  const syncStripEdge = useCallback(() => {
    const el = tabStripRef.current;
    if (!el) return;
    const max = el.scrollWidth - el.clientWidth;
    if (max <= 1) {
      setStripEdge("none");
      return;
    }
    const fromStart = Math.abs(el.scrollLeft);
    const atStart = fromStart <= 1;
    const atEnd = fromStart >= max - 1;
    setStripEdge(atStart && atEnd ? "none" : atStart ? "end" : atEnd ? "start" : "both");
  }, []);

  // The edge arrows move the strip one "page" toward that end. Direction is
  // resolved through the element's OWN `direction` (RTL scrolls negative), and
  // the step is most of the visible width so a tab never lands half under the
  // arrow. No `behavior: "smooth"`: the repo's motion layer honours
  // `prefers-reduced-motion` for CSS, and a JS smooth scroll would ignore it.
  const scrollStrip = useCallback(
    (dir: 1 | -1) => {
      const el = tabStripRef.current;
      if (!el) return;
      const step = Math.max(120, Math.round(el.clientWidth * 0.8));
      const rtl = getComputedStyle(el).direction === "rtl";
      el.scrollLeft += (rtl ? -1 : 1) * dir * step;
      syncStripEdge();
    },
    [syncStripEdge],
  );

  // Re-read on: a scroll (the cue has to move with the thumb), a resize of the
  // strip (a wider phone shows more tabs and can stop overflowing), and a change
  // of the tab set (the admin-only tabs arrive with the session, which is what
  // turns a non-overflowing strip into an overflowing one — the first paint
  // measured 9 tabs and the settled strip 13).
  useEffect(() => {
    const el = tabStripRef.current;
    if (!el) return;
    syncStripEdge();
    el.addEventListener("scroll", syncStripEdge, { passive: true });
    const ro = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(syncStripEdge);
    ro?.observe(el);
    window.addEventListener("resize", syncStripEdge, { passive: true });
    return () => {
      el.removeEventListener("scroll", syncStripEdge);
      ro?.disconnect();
      window.removeEventListener("resize", syncStripEdge);
    };
  }, [syncStripEdge, activeTab, visibleTabs.length]);

  // ── the CHIP card's "is there anything to save?" (card t_f11d676f) ──
  //
  // The sticky bar is the only Save a merchant sees in the first viewport on a
  // phone, so it must not be a control that does nothing: disabled until the
  // form differs from what the server holds, enabled the moment it does. The
  // secret is the one field with no server-side counterpart to compare (it is
  // write-only and never returned), so any non-empty value counts as unsaved.
  const chipDirty =
    brandId !== chipSaved.brand ||
    emailFallback !== chipSaved.email ||
    secretKey.trim() !== "" ||
    methodSetKey(whitelist) !== chipSaved.methods;
  const allMethodsSelected = METHODS.length > 0 && METHODS.every((m) => whitelist.includes(m.id));
  // Card t_9efc5ed7: ONE connection state, read from the server's own row, and
  // used by BOTH the card's pill and the sticky bar's label — the two cannot
  // disagree, and neither claims a "mode" any more. "Unverified" is a real third
  // state: credentials stored but the live CHIP check did not pass at save time,
  // so calling it "Connected" would be a lie.
  const chipConnection: "off" | "unverified" | "on" = !status?.configured
    ? "off"
    : verified
      ? "on"
      : "unverified";
  const chipStateLabel = !status
    ? ""
    : chipConnection === "on"
      ? t("set.connected")
      : chipConnection === "unverified"
        ? t("set.notVerified")
        : t("set.notConnected");
  // The masked tail alone, without the leading bullets: the API masks with
  // cryptoutil.MaskSecret ("••••4f2a"), and the card's line reads
  // "Saved encrypted · ends 4f2a" (card t_9efc5ed7). When the mask carries no
  // tail at all the line drops the "ends …" clause rather than printing one.
  const secretTail = secretMasked.replace(/^[•]+/, "").trim();

  // Stable identity: the toast's dismiss timer is keyed on this callback, and a
  // new function on every render would restart (or drop) the timer. It must live
  // in the component body — a hook inside the JSX below renders conditionally and
  // breaks the hook order.
  const dismissMsg = useCallback(() => setMsg(""), []);

  // Card t_69d2dd19: the fee editor's two halves (platform rows, legacy rows) and
  // the example box. Derived on every render from the rows + the merchant's
  // drafts, so the example always narrates the field in front of them.
  const feePlatformRows = feeRates.filter((r) => !r.legacy_grossup);
  const feeLegacyRows = feeRates.filter((r) => r.legacy_grossup);
  const feePreviewRow =
    feePlatformRows.find((r) => r.method === feePreviewMethod) ||
    feePlatformRows.find((r) => feeSenFromRM(feeAmountDrafts[r.method]) > 0) ||
    feePlatformRows[0];
  const feePreviewSen = feePreviewRow ? feeSenFromRM(feeAmountDrafts[feePreviewRow.method]) : 0;
  const feePreviewName = feePreviewRow ? FEE_METHOD_NAMES[feePreviewRow.method] || feePreviewRow.method : "—";

  // One literal t() per method: the i18n guard resolves these, so a missing
  // translation arm fails the build instead of rendering the raw key.
  const feeMethodDesc: Record<string, string> = {
    fpx: t("set.feesB2CDesc"),
    fpx_b2b1: t("set.feesB2B1Desc"),
    card: t("set.feesCardDesc"),
    ewallet: t("set.feesEWalletDesc"),
    duitnow_qr: t("set.feesQRDesc"),
  };

  // stageLegacyMove turns one legacy percentage row into a platform row in the
  // editor. It deliberately does NOT save: the merchant sees the new amount in
  // context and commits every change with the one Save button, so a percentage
  // workspace moves over in one save and a stray click cannot re-price a buyer.
  const stageLegacyMove = (row: FeeRateRow) => {
    const amount = feeSenFromRM(feeLegacyDrafts[row.method]);
    setFeeRates((rows) =>
      rows.map((r) =>
        r.method === row.method
          ? { ...r, legacy_grossup: false, platform_fee_sen: amount, fee_fixed_sen: 0, fee_percent_bp: 0 }
          : r,
      ),
    );
    setFeeAmountDrafts((d) => ({ ...d, [row.method]: amount > 0 ? (amount / 100).toFixed(2) : "" }));
    setFeePreviewMethod(row.method);
  };


  return (
    <Shell active="settings">
      <div className="w-full flex-1 px-6 py-8">
        <h1 className="max-w-xl text-2xl font-bold">
          {t("set.title")} <span className="text-[var(--jf-primary)]">JomForm</span>
        </h1>

        {/* Issue #163: read_only members get a read-only notice instead of the
            write tabs. */}
        {isReadOnly && (
          <p className="mt-4 rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 text-sm text-slate-500">
            {t("mem.readOnlyNote")}
          </p>
        )}

        {/* Tab bar (issue #68) — same styling as Profile.
            The strip WRAPPED on a phone. 11 tabs on
            `flex flex-wrap` measured 4 rows / 161px at 320-360px (20.1% of an
            800px viewport; Tamil 5 rows / 202px) of tab chrome above the card.
            It is now ONE row that scrolls sideways (`overflow-x-auto`), which
            is how the other wide dashboard rows hold at phone widths.

            WHY `overflow-x-auto` SITS ON THE SAME ELEMENT AS `border-b`
              A scroller only paints its overflow indicator at the edge it is
              scrolled TO, so giving the border its own wrapper does not keep
              the border visible while you are at the start edge — measured,
              the wrapper's border was simply not in view in RTL/Arabic. One
              element keeps the strip exactly one row tall and leaves the
              border under the full width of the viewport.
            RTL (ar)
              `overflow-x-auto` follows the writing mode: in Arabic the strip
              scrolls from the right edge and `scrollLeft` reads 0 at the start,
              which the reveal below already handles (measured, 25/25 cases).
            THE ACTIVE TAB MUST NOT ARRIVE SCROLLED OUT
              A plain `overflow-x-auto` hides the selection: at 320px the
              selected tab (the last one — Affiliates by default) measured
              867px inside a 272px box. So the strip reveals the selected tab
              on mount and whenever the selection changes, and `onFocus`
              reveals a tab the keyboard has moved to (see `revealTab` below —
              it never SELECTS on focus, which would switch tabs as the user
              Tab-cycles through the strip).
              `block: "nearest"` keeps the PAGE from scrolling vertically, so
              the reveal cannot move the reader's viewport.
            `-mb-px` stays on the tabs: it merges the active tab's 2px underline
            with the strip's 1px border, which is the intended tab-bar look. */}
        {visibleTabs.length > 0 && (
          /* The wrapper exists for the EDGE ARROWS (card t_f11d676f): they are
             absolute overlays on the strip's two ends, so they must live
             OUTSIDE the `role="tablist"` box (a button inside a tablist is a
             stray child of a widget whose children are tabs) while still being
             positioned against it. It adds no width, so the strip's own
             geometry — and the `e2e/settings-tabs.spec.ts` measurements of it —
             are unchanged. */
          <div className="relative mt-4">
            {(stripEdge === "start" || stripEdge === "both") && (
              <StripEdgeArrow
                side="start"
                label={t("set.tabsScrollBack")}
                testId="tabstrip-back"
                onClick={() => scrollStrip(-1)}
              />
            )}
            <div
              ref={tabStripRef}
              role="tablist"
              data-edge={stripEdge}
              className="jf-tabstrip flex gap-1 overflow-x-auto border-b border-slate-200"
              onKeyDown={tablistKeyDown}
            >
              {visibleTabs.map((id) => (
                <button
                  key={id}
                  id={tabDomId("set", id)}
                  aria-controls={panelDomId("set", id)}
                  tabIndex={tabStop(id, activeTab)}
                  onClick={() => setTab(id)}
                  onFocus={(e) => revealTab(e.currentTarget)}
                  aria-selected={activeTab === id}
                  role="tab"
                  className={`-mb-px whitespace-nowrap border-b-2 px-3 py-2 text-sm font-medium sm:px-4 ${
                    activeTab === id
                      ? "border-slate-900 text-slate-900"
                      : "border-transparent text-[var(--jf-muted)] hover:text-slate-800"
                  }`}
                >
                  <span className="sm:hidden">{t(`set.tab.${id}` as const)}</span>
                  <span className="hidden sm:inline">{t(`set.tab.${id}` as const)}</span>
                </button>
              ))}
            </div>
            {(stripEdge === "end" || stripEdge === "both") && (
              <StripEdgeArrow
                side="end"
                label={t("set.tabsScrollForward")}
                testId="tabstrip-forward"
                onClick={() => scrollStrip(1)}
              />
            )}
          </div>
        )}

        {/* Issue #68: transient Cloudflare OAuth callback banner + shared
            status/error banners shown above tab content regardless of the
            active tab (Profile-style).
            Each of the first two carries its own tone: `cfNotice` holds a
            connected/failed pair and `msg` a saved/failed pair, and a failure
            must not be painted in success chrome (card t_ad2af554). The emerald
            pair is a success; a failure goes red. The ink steps are the -700
            ones, which clear AA on their own -50 tint (red-600 is 4.36:1 on
            red-50 and only passes on white — see card t_fc1b67f1). */}
        {cfNotice && (
          <p
            className={`mt-4 rounded-lg px-3 py-2 text-sm ${
              cfNoticeOk ? "bg-emerald-50 text-emerald-700" : "bg-red-50 text-red-700"
            }`}
          >
            {cfNotice}
          </p>
        )}
        {msg && (
          <p
            className={`mt-4 rounded-lg px-3 py-2 text-sm ${
              msgOk ? "bg-emerald-50 text-emerald-700" : "bg-red-50 text-red-700"
            }`}
          >
            {msg}
          </p>
        )}
        {err && <p className="mt-4 rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700">{err}</p>}

        {/* Card t_69d2dd19 (requirement 6 of t_1a17d22d): a workspace whose rows
            still price by the legacy percentage must be TOLD, on whatever tab it
            opens. The amber section inside Payment fees is the same notice with
            the editor beside it; this one is the pointer to it. */}
        {feeLegacyRows.length > 0 && activeTab !== "fees" && (
          <p
            data-testid="fees-legacy-notice"
            className="mt-4 flex flex-wrap items-center gap-2 rounded-lg border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900"
          >
            <span>{t("set.feesLegacyNotice")}</span>
            <button type="button" onClick={() => setTab("fees")} className="font-semibold underline">
              {t("set.feesLegacyNoticeGo")}
            </button>
          </p>
        )}


        {/* ── CHIP tab ── */}
        {activeTab === "chip" && (
          <section
            className="jf-card mt-6 p-6"
            role="tabpanel"
            id={panelDomId("set", "chip")}
            aria-labelledby={tabDomId("set", "chip")}
          >
            {/* Card t_9efc5ed7: the card's header carries ONE status pill. The
                two "✓ API credentials verified" chips that used to sit on the
                Brand ID and Secret Key labels repeated the same fact twice and
                said the same thing in two places; the connection state belongs
                in the header, said once. It has three truthful states, not two:
                credentials stored but the live CHIP check unconfirmed is neither
                "Connected" nor "Not connected". */}
            <div className="flex items-center justify-between gap-3">
              <h2 className="text-lg font-semibold">{t("set.chipTitle")}</h2>
              {status && (
                <span
                  data-testid="chip-status-badge"
                  data-state={chipConnection}
                  className={`shrink-0 rounded-full border px-3 py-1 text-xs font-semibold ${
                    chipConnection === "on"
                      ? "border-[var(--jf-success)] bg-[var(--jf-success-bg)] text-[var(--jf-text-secondary)]"
                      : chipConnection === "unverified"
                        ? "border-[var(--jf-warning)] bg-[var(--jf-warning-bg)] text-[var(--jf-warning-text)]"
                        : "border-[var(--jf-border)] bg-[var(--jf-surface-2)] text-[var(--jf-muted-on-tint)]"
                  }`}
                >
                  {chipStateLabel}
                </span>
              )}
            </div>
            <p className="mt-2 max-w-xl text-sm text-[var(--jf-muted)]">
              {t("set.getYour")} <code className="rounded bg-[var(--jf-surface-2)] px-1 text-[var(--jf-text-secondary)]">Brand ID</code> {t("set.and")}{" "}
              <code className="rounded bg-[var(--jf-surface-2)] px-1 text-[var(--jf-text-secondary)]">{t("set.secretKey")}</code> {t("set.from")}{" "}
              <a
                href="https://portal.chip-in.asia/collect/developers/brands"
                className="text-[var(--jf-primary)] underline"
                target="_blank"
              >
                CHIP Merchant Portal → Developers
              </a>
              . {t("set.keysEncrypted")}
            </p>
            {/* `id` so the sticky Save bar at the bottom of the view can submit
                this form from outside it (`form="chip-creds-form"`) without a
                second copy of the write path (card t_f11d676f). */}
            <form id="chip-creds-form" onSubmit={save} className="mt-4 space-y-4">
              {/* Credentials FIRST (card t_9efc5ed7): Brand ID, then the Secret
                  Key. The placeholders describe the ACTION and the portal the
                  value comes from. The old placeholder was the literal string
                  "e.g. 5f2b…-uuid" — a "…" CHARACTER inside the string, not CSS
                  truncation — so the field looked clipped before it held
                  anything (Wan: "brand id tu ada truncate .... uuid"). No
                  placeholder in this card carries an ellipsis of either kind. */}
              <div>
                <label htmlFor="chip-brand-id" className="text-xs font-semibold text-[var(--jf-text-secondary)]">
                  Brand ID
                </label>
                <input
                  id="chip-brand-id"
                  data-testid="chip-brand-id"
                  value={brandId}
                  onChange={(e) => setBrandId(e.target.value)}
                  required
                  className="jf-input mt-1.5 max-w-xl"
                  placeholder={t("set.brandIdPh")}
                />
              </div>
              <div>
                {secretMasked && !replacingSecret ? (
                  <>
                    {/* A saved secret is a MASKED LINE plus a Replace ACTION — not
                        a second field that sits there empty and reads as "the key
                        is gone". The button is described by the masked line so a
                        screen reader hears WHICH key it replaces. */}
                    <span className="block text-xs font-semibold text-[var(--jf-text-secondary)]">
                      {t("set.secretKey")}
                    </span>
                    <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1">
                      <p id="chip-secret-masked" data-testid="chip-secret-masked" className="text-sm text-[var(--jf-text-secondary)]">
                        {secretTail
                          ? t("set.savedEncryptedEnds", { tail: secretTail })
                          : t("set.savedEncrypted")}
                      </p>
                      <button
                        type="button"
                        onClick={() => setReplacingSecret(true)}
                        aria-describedby="chip-secret-masked"
                        data-testid="chip-secret-replace"
                        className="text-xs font-semibold text-[var(--jf-primary-strong)] underline"
                      >
                        {t("set.replace")}
                      </button>
                    </div>
                  </>
                ) : (
                  <>
                    <label htmlFor="chip-secret-key" className="text-xs font-semibold text-[var(--jf-text-secondary)]">
                      {t("set.secretKey")}
                    </label>
                    <input
                      id="chip-secret-key"
                      data-testid="chip-secret-key"
                      value={secretKey}
                      onChange={(e) => setSecretKey(e.target.value)}
                      type="text"
                      autoComplete="off"
                      spellCheck={false}
                      className="jf-input mt-1.5 max-w-xl font-mono"
                      placeholder={t("set.secretKeyPh")}
                    />
                    {secretMasked && (
                      <button
                        type="button"
                        onClick={() => {
                          setReplacingSecret(false);
                          setSecretKey("");
                        }}
                        data-testid="chip-secret-keep"
                        className="mt-1 text-xs font-semibold text-[var(--jf-primary-strong)] underline"
                      >
                        {t("set.keepSaved")}
                      </button>
                    )}
                  </>
                )}
              </div>

              {/* Card t_9efc5ed7: the fallback email is a RARE one-time-form
                  setting, so it folds into an Advanced disclosure instead of
                  taking a primary row beside the credentials. It opens itself when
                  one is stored, so a value the merchant set is never hidden from
                  them (see the bundle applier). */}
              <details
                className="rounded-[var(--jf-radius-md)] border border-[var(--jf-border)] px-3 py-2"
                data-testid="chip-advanced"
                open={advancedOpen}
                onToggle={(e) => {
                  advancedTouched.current = true;
                  setAdvancedOpen(e.currentTarget.open);
                }}
              >
                <summary className="cursor-pointer text-xs font-semibold text-[var(--jf-text-secondary)]">
                  {t("set.advanced")}
                </summary>
                <div className="mt-2">
                  <label htmlFor="chip-email-fallback" className="text-xs font-semibold text-[var(--jf-text-secondary)]">
                    {t("set.emailFallback")}
                  </label>
                  <input
                    id="chip-email-fallback"
                    data-testid="chip-email-fallback"
                    value={emailFallback}
                    onChange={(e) => setEmailFallback(e.target.value)}
                    type="email"
                    className="jf-input mt-1.5 max-w-xl"
                    placeholder="store@yourshop.my"
                  />
                  <p className="mt-1 text-xs text-[var(--jf-muted)]">{t("set.emailFallbackHint")}</p>
                </div>
              </details>

              {/* Payment methods — CHECKBOXES, not filter pills (card
                  t_f11d676f).
                  The twelve rounded pills this used to render were the same
                  shape, colour and font as the filter chips on /sales and
                  /deliveries, and a pill with no tick, no box and no
                  selected-state mark cannot answer "is this one chosen?" — a
                  merchant read the row as a set of filters and could not tell
                  what had been picked. Each method is now a labelled native
                  checkbox: the box IS the control, the tick is the state (and
                  the accessible `checked` state a screen reader reads), and the
                  label's tint follows it.
                  "Select all" is here because twelve boxes is a long tap
                  sequence on a phone; it names the OPPOSITE of the current state
                  so it is always one action away from what the merchant wants.
                  The hint "(blank = all available)" stays: it is the server's
                  rule, and unticking everything is not the same as ticking
                  everything (one is "let CHIP decide", the other is the list). */}
              <div>
                <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
                  <span id="chip-pm-label" className="text-xs font-semibold text-[var(--jf-text-secondary)]">
                    {t("set.paymentMethods")} <span className="font-normal text-[var(--jf-muted)]">{t("set.allMethods")}</span>
                  </span>
                  <button
                    type="button"
                    onClick={() => setWhitelist(allMethodsSelected ? [] : METHODS.map((m) => m.id))}
                    data-testid="chip-pm-toggle-all"
                    className="min-h-8 rounded-[var(--jf-radius-md)] border border-[var(--jf-border)] px-2.5 py-1 text-xs font-semibold text-[var(--jf-primary)] hover:border-[var(--jf-muted)]"
                  >
                    {allMethodsSelected ? t("set.pmClearAll") : t("set.pmSelectAll")}
                  </button>
                </div>
                {/* Card t_9efc5ed7: a TWO-COLUMN grid at every width (the
                    mockup's 390px phone included), not one column that only
                    becomes two on a desktop. */}
                <div
                  role="group"
                  aria-labelledby="chip-pm-label"
                  className="mt-2 grid grid-cols-2 gap-1.5"
                >
                  {METHODS.map((m) => {
                    const checked = whitelist.includes(m.id);
                    return (
                      <label
                        key={m.id}
                        data-testid={`chip-pm-row-${m.id}`}
                        data-checked={checked ? "true" : "false"}
                        className={`flex min-h-9 cursor-pointer items-center gap-2 rounded-[var(--jf-radius-md)] border px-2.5 py-2 text-xs ${
                          checked
                            ? "border-[var(--jf-primary)] bg-[var(--jf-primary-tint)] font-medium text-[var(--jf-primary-on-tint)]"
                            : "border-[var(--jf-border)] bg-[var(--jf-surface)] text-[var(--jf-text-secondary)] hover:border-[var(--jf-muted)]"
                        }`}
                      >
                        {/* A native checkbox, visually replaced by the box below
                            so the tick is a real glyph at every zoom level and
                            in every browser. It stays in the DOM (sr-only) so
                            the row is one keyboard stop with its label, and the
                            focus ring is drawn on the visible box. `role` and
                            `aria-checked` are set explicitly (card t_9efc5ed7):
                            they are the same state the native control exposes,
                            and the card asks for the announced checkbox state by
                            name — the grid must read as checkboxes, not filters. */}
                        <input
                          type="checkbox"
                          role="checkbox"
                          aria-checked={checked}
                          className="peer sr-only"
                          checked={checked}
                          onChange={() => toggleMethod(m.id)}
                          data-testid={`chip-pm-${m.id}`}
                        />
                        {/* The box and the tick are two elements on purpose: a
                            white glyph must not be declared on the same element
                            as a white fill, which is the pairing the CTA
                            contrast sweep (`src/lib/ctaContrast.test.ts`)
                            refuses wherever it finds it. The fill is on the box,
                            the glyph is on the child.

                            The glyph exists ONLY while the row is checked. It
                            used to be rendered unconditionally, and an unticked
                            box is `bg-white`: a white ✓ on a white fill is a
                            measured 1:1, which `e2e/a11y-singlechar-text.spec.ts`
                            reds (12 rows, card t_5dc6bb86). `aria-hidden` does
                            not help — that scan measures PAINT, not the AX tree
                            — and neither does the box/glyph split above, because
                            the glyph's resolved surface IS the box's own white
                            fill. An unticked box therefore carries no text node
                            at all, and the ticked one paints white on
                            `--jf-primary` (4.70:1). */}
                        <span
                          aria-hidden="true"
                          className="flex h-4 w-4 shrink-0 items-center justify-center rounded border border-[var(--jf-border)] bg-[var(--jf-surface)] peer-checked:border-[var(--jf-primary)] peer-checked:bg-[var(--jf-primary)] peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-[var(--jf-primary)]"
                        >
                          {checked && (
                            <span className="text-jf-4xs font-bold leading-none text-white">✓</span>
                          )}
                        </span>
                        <span>{m.label}</span>
                      </label>
                    );
                  })}
                </div>
              </div>

              {/* Card t_9efc5ed7: the action bar sits at the BOTTOM of the card —
                  "Test connection" (secondary) then "Save" (primary), the two
                  things a merchant wants after pasting a key. Disconnect is a
                  destructive exception, so it is a quiet text action pushed to the
                  end rather than a third equal-weight button. */}
              <div className="flex flex-wrap items-center gap-2 border-t border-[var(--jf-border)] pt-4">
                <button
                  type="button"
                  onClick={testConnection}
                  disabled={testing || (!status?.configured && !secretKey.trim())}
                  data-testid="chip-test-connection"
                  className="jf-btn-ghost"
                >
                  {testing ? t("set.testing") : t("set.testConnection")}
                </button>
                <button className="jf-btn-primary" data-testid="chip-save">
                  {t("set.save")}
                </button>
                {status?.configured && (
                  <button
                    type="button"
                    onClick={clearCreds}
                    data-testid="chip-clear"
                    className="ml-auto text-xs font-semibold text-[var(--jf-muted)] underline hover:text-[var(--jf-text-secondary)]"
                  >
                    {t("set.clear")}
                  </button>
                )}
              </div>
            </form>
          </section>
        )}

        {/* ── Domains tab: custom domains + Cloudflare connect ── */}
        {activeTab === "domains" && (
          <div
            role="tabpanel"
            id={panelDomId("set", "domains")}
            aria-labelledby={tabDomId("set", "domains")}
          >
            <section className="jf-card mt-6 p-6">
              <h2 className="text-lg font-semibold">{t("set.domainsTitle")}</h2>
              <p className="mt-2 max-w-xl text-sm text-[var(--jf-muted)]">
                {t("set.domainsHelp")}
              </p>
              {domainMsg && <p className="mt-3 rounded-lg bg-indigo-50 px-3 py-2 text-sm text-indigo-700">{domainMsg}</p>}
              <form onSubmit={setDomainClaim} className="mt-4 flex flex-wrap gap-2">
                {/* `min-w-56` (224px) is a hard `min-width`, and this input is a
                    `flex-1` child (`flex: 1 1 0%`), whose `min-width` is a floor
                    the item cannot shrink below. The card's content box is only
                    222px at a 320px viewport, so the field pushed the row 2px past
                    its container (measured: row scrollWidth 224 > clientWidth 222,
                    input box 224 in a 222px row, every locale). Same shape and
                    same fix as MembersTab.tsx:158. `basis-56` keeps the 224px as a
                    PREFERRED size the item may shrink from, and `min-w-0` removes
                    the floor, so the row holds at phone widths while desktop is
                    unchanged. NOT a `min-w-[…]` utility: measured on the built
                    page, a `min-width` utility DOES win against the unlayered
                    `.jf-input`, so it would reproduce this same overflow. */}
                <input
                  value={domainInput}
                  onChange={(e) => setDomainInput(e.target.value)}
                  placeholder="shop.yourdomain.com"
                  className="jf-input min-w-0 max-w-xl flex-1 basis-56 sm:basis-auto"
                />
                <button className="jf-btn-primary" type="submit">{t("set.addDomain")}</button>
              </form>
              {/* Issue #322: when Cloudflare is linked, offer one-click auto-CNAME. */}
              {cfLinked && (
                <p className="mt-2 text-xs text-emerald-700">{t("set.autoCnameHint")}</p>
              )}
              <ul className="mt-4 space-y-2">
                {domains.map((d: { id: string; domain: string; verified: boolean; auto_cname?: string; tls_mode?: string; tls_status?: string; tls_error?: string; tls_email?: string }) => (
                  <li key={d.id} className="rounded-lg border border-[var(--jf-border)] px-4 py-3">
                    <div className="flex items-center justify-between gap-3">
                      <div className="min-w-0">
                        <p className="truncate text-sm font-medium">{d.domain}</p>
                        {d.verified ? (
                          <p className="text-jf-2xs text-emerald-700">{t("set.verified")}</p>
                        ) : (
                          <p className="flex items-center gap-1.5 text-jf-2xs text-amber-600">
                            {polling[d.id] && (
                              <span className="inline-block h-3 w-3 animate-spin rounded-full border-2 border-amber-500 border-t-transparent" />
                            )}
                            {polling[d.id] ? t("set.checkingDns") : t("set.pendingDns", { target: cnameTargetHost })}
                          </p>
                        )}
                        {/* Issue #322: per-domain auto-CNAME status. */}
                        {d.auto_cname === "created" && (
                          <p className="text-jf-2xs text-emerald-700">{t("set.autoCnameStatusCreated")}</p>
                        )}
                        {d.auto_cname === "failed" && (
                          <p className="text-jf-2xs text-red-600">{t("set.autoCnameStatusFailed")}</p>
                        )}
                        {/* Issue #397: per-domain TLS status. */}
                        {d.verified && d.tls_status === "active" && (
                          <p className="text-jf-2xs text-emerald-700">{t("set.tlsStatusActive")}</p>
                        )}
                        {d.verified && d.tls_status === "pending" && (
                          <p className="text-jf-2xs text-amber-600">{t("set.tlsStatusPending")}</p>
                        )}
                        {d.verified && d.tls_status === "error" && (
                          <p className="text-jf-2xs text-red-600">
                            {t("set.tlsStatusError")}
                            {d.tls_error ? ` — ${d.tls_error}` : ""}
                          </p>
                        )}
                        {d.verified && d.tls_status === "custom" && (
                          <p className="text-jf-2xs text-indigo-600">{t("set.tlsStatusCustom")}</p>
                        )}
                      </div>
                      <div className="flex shrink-0 items-center gap-2">
                        {!d.verified && (
                          <button
                            onClick={() => startPolling(d)}
                            disabled={!!polling[d.id]}
                            className="rounded-md bg-[var(--jf-primary)] px-3 py-1.5 text-xs font-semibold text-white transition-colors hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-60"
                          >
                            {polling[d.id] ? t("set.checkingDns") : t("set.verifyCta")}
                          </button>
                        )}
                        {/* Issue #322: one-click auto-CNAME for a pending domain. */}
                        {!d.verified && cfLinked && d.auto_cname !== "created" && (
                          <button
                            onClick={() => autoCreateCname(d)}
                            disabled={!!autoCnameBusy[d.id]}
                            className="rounded-md border border-[var(--jf-primary)] px-3 py-1.5 text-xs font-semibold text-[var(--jf-primary)] transition-colors hover:bg-indigo-50 hover:text-[var(--jf-primary-on-tint)] disabled:cursor-not-allowed disabled:opacity-60"
                          >
                            {autoCnameBusy[d.id] ? t("set.autoCnameBusy") : t("set.autoCnameCta")}
                          </button>
                        )}
                        <button
                          onClick={async () => {
                            await fetch(`/api/rest/domains/${d.id}${workspaceQuery()}`, { method: "DELETE" });
                            // Issue #321: load() reads bundle.domains from the closure's stale
                            // bundle, so the removed domain would linger until reloadSession()
                            // re-renders. refreshDomains() refetches the list directly so the
                            // domain disappears immediately (same as the verify flow).
                            setDomains((prev) => prev.filter((x) => x.id !== d.id));
                            await refreshDomains();
                          }}
                          className="text-xs text-slate-500 hover:text-red-700"
                        >{t("set.remove")}</button>
                      </div>
                    </div>
                    {!d.verified && (
                      <div className="mt-2 rounded-md bg-slate-50 px-3 py-2">
                        <p className="text-jf-2xs font-medium text-slate-500">{t("set.cnameRow")}</p>
                        <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 font-mono text-xs text-slate-700">
                          <span className="rounded bg-white px-1.5 py-0.5 ring-1 ring-slate-200">CNAME</span>
                          <span className="rounded bg-white px-1.5 py-0.5 ring-1 ring-slate-200">{d.domain}</span>
                          <span className="text-[var(--jf-muted-on-tint)]">→</span>
                          <span className="rounded bg-white px-1.5 py-0.5 ring-1 ring-slate-200">{cnameTargetHost}</span>
                          <button
                            type="button"
                            onClick={() => {
                              navigator.clipboard?.writeText(`CNAME ${d.domain} → ${cnameTargetHost}`);
                              setDomainMsg(t("set.copy"));
                            }}
                            className="ml-1 text-jf-2xs text-[var(--jf-primary)] hover:underline"
                          >
                            {t("set.copy")}
                          </button>
                        </div>
                        {polling[d.id] && (
                          <p className="mt-1.5 text-jf-2xs text-[var(--jf-muted-on-tint)]">{t("set.pollingHint")}</p>
                        )}
                      </div>
                    )}
                    {d.verified && (
                      <div className="mt-2">
                        <a
                          href={`https://${d.domain}`}
                          target="_blank"
                          rel="noreferrer"
                          className="text-xs font-semibold text-emerald-700 hover:underline"
                        >
                          {t("set.liveLink", { domain: d.domain })}
                        </a>
                      </div>
                    )}
                    {/* Issue #397: per-domain TLS mode selector + bring-your-own-cert. */}
                    {d.verified && (
                      <div className="mt-3 rounded-md bg-slate-50 px-3 py-2">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="text-jf-2xs font-medium text-slate-500">{t("set.tlsModeLabel")}</span>
                          <button
                            type="button"
                            onClick={() => setDomainTlsMode(d, "auto")}
                            disabled={!!tlsBusy[d.id] || d.tls_mode === "auto"}
                            className={`rounded-md px-2.5 py-1 text-jf-2xs font-semibold transition-colors disabled:cursor-not-allowed ${
                              d.tls_mode === "auto"
                                ? "bg-[var(--jf-primary)] text-white"
                                : "border border-[var(--jf-primary)] text-[var(--jf-primary)] hover:bg-indigo-50 hover:text-[var(--jf-primary-on-tint)]"
                            }`}
                          >
                            {t("set.tlsModeAutoLabel")}
                          </button>
                          <button
                            type="button"
                            onClick={() => setDomainTlsMode(d, "custom")}
                            disabled={!!tlsBusy[d.id] || d.tls_mode === "custom"}
                            className={`rounded-md px-2.5 py-1 text-jf-2xs font-semibold transition-colors disabled:cursor-not-allowed ${
                              d.tls_mode === "custom"
                                ? "bg-[var(--jf-primary)] text-white"
                                : "border border-[var(--jf-primary)] text-[var(--jf-primary)] hover:bg-indigo-50 hover:text-[var(--jf-primary-on-tint)]"
                            }`}
                          >
                            {t("set.tlsModeCustomLabel")}
                          </button>
                          {d.tls_mode === "custom" && (
                            <button
                              type="button"
                              onClick={() => setCertOpen((prev) => ({ ...prev, [d.id]: !prev[d.id] }))}
                              className="text-jf-2xs font-semibold text-[var(--jf-primary)] hover:underline"
                            >
                              {certOpen[d.id] ? t("set.tlsCertClose") : t("set.tlsCertUpload")}
                            </button>
                          )}
                        </div>
                        {/* Issue #398: per-domain ACME contact email (defaults to the account email). */}
                        <div className="mt-2 flex flex-wrap items-center gap-2">
                          <label className="text-jf-2xs font-medium text-slate-500">{t("set.tlsEmailLabel")}</label>
                          <input
                            value={tlsEmailInput[d.id] ?? d.tls_email ?? ""}
                            onChange={(e) => setTlsEmailInput((prev) => ({ ...prev, [d.id]: e.target.value }))}
                            placeholder={t("set.tlsEmailPlaceholder")}
                            className="jf-input w-64 font-mono text-jf-2xs"
                          />
                          <button
                            type="button"
                            onClick={() => setDomainTlsEmail(d)}
                            disabled={!!tlsBusy[d.id]}
                            className="rounded-md border border-[var(--jf-primary)] px-2.5 py-1 text-jf-2xs font-semibold text-[var(--jf-primary)] transition-colors hover:bg-indigo-50 hover:text-[var(--jf-primary-on-tint)] disabled:cursor-not-allowed disabled:opacity-60"
                          >
                            {tlsBusy[d.id] ? t("set.tlsEmailSaving") : t("set.tlsEmailSave")}
                          </button>
                        </div>
                        {d.tls_mode === "custom" && certOpen[d.id] && (
                          <div className="mt-2 space-y-2">
                            <textarea
                              value={certInput[d.id]?.cert ?? ""}
                              onChange={(e) =>
                                setCertInput((prev) => ({ ...prev, [d.id]: { ...(prev[d.id] ?? { cert: "", key: "" }), cert: e.target.value } }))
                              }
                              placeholder={t("set.tlsCertPlaceholder")}
                              rows={4}
                              className="jf-input w-full font-mono text-jf-2xs"
                            />
                            <textarea
                              value={certInput[d.id]?.key ?? ""}
                              onChange={(e) =>
                                setCertInput((prev) => ({ ...prev, [d.id]: { ...(prev[d.id] ?? { cert: "", key: "" }), key: e.target.value } }))
                              }
                              placeholder={t("set.tlsKeyPlaceholder")}
                              rows={4}
                              className="jf-input w-full font-mono text-jf-2xs"
                            />
                            <button
                              type="button"
                              onClick={() => uploadCustomCert(d)}
                              disabled={!!tlsBusy[d.id]}
                              className="rounded-md bg-[var(--jf-primary)] px-3 py-1.5 text-xs font-semibold text-white transition-colors hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-60"
                            >
                              {tlsBusy[d.id] ? t("set.tlsCertSaving") : t("set.tlsCertSave")}
                            </button>
                          </div>
                        )}
                      </div>
                    )}
                  </li>
                ))}
              </ul>
            </section>

            {/* Cloudflare connect (auto CNAME) — issue #22 + manual #82 */}
            <section className="jf-card mt-6 p-6">
              <div className="flex items-center justify-between">
                <h2 className="text-lg font-semibold">{t("set.cfTitle")}</h2>
                {cfLinked && (
                  <span className="rounded-full border border-emerald-200 bg-emerald-50 px-3 py-1 text-xs font-semibold text-emerald-700">
                    {t("set.cfConnected")}
                  </span>
                )}
              </div>
              <p className="mt-2 text-sm text-[var(--jf-muted)]">
                {t("set.cfHelp")}
              </p>
              {/* `flex-wrap` because a non-wrapping row of two button classes
                  cannot be narrower than their combined min-content: both classes
                  carry `white-space: nowrap; flex-shrink: 0` (globals.css, #655),
                  so at 320px the two buttons measured 200.25px each in a 222px
                  content box and the cluster — not the button row — set the
                  DOCUMENT's min-content to 446px, i.e. the whole tab scrolled
                  126px sideways at 320/360/390/430px. Same defect and same fix as
                  the /form-settings header cluster (PR 673); measured on the built
                  page before: documentElement scrollWidth 446 vs clientWidth 320. */}
              <div className="mt-3 flex flex-wrap gap-2">
                {/* Cloudflare's authorize URL, not a Next page: the API route
                    answers with a redirect to Cloudflare's consent screen, which
                    a `next/link` client-side transition cannot follow. `<a>` is
                    correct. The href carries the workspace scope (card
                    t_88485157) — it is held in state rather than built inline
                    because `workspaceQuery()` reads the browser scope, which the
                    server render does not have. */}
                {cfStatusLoaded && cfStartHref !== "" && !cfLinked && (
                  <a href={cfStartHref} className="jf-btn-primary inline-block">
                    {t("set.cfLogin")}
                  </a>
                )}
                {cfLinked && (
                  <button
                    onClick={async () => {
                      await fetch(`/api/cf/disconnect${workspaceQuery()}`, { method: "POST" });
                      setCfLinked(false);
                    }}
                    className="jf-btn-ghost text-slate-600"
                  >
                    {t("set.cfDisconnect")}
                  </button>
                )}
                {cfStatusLoaded && !cfLinked && (
                  <button
                    onClick={() => { setCfManualOpen(!cfManualOpen); setCfManualErr(""); }}
                    className="jf-btn-ghost text-slate-600"
                  >
                    {t("set.cfManualToggle")}
                  </button>
                )}
              </div>
              {!cfLinked && cfManualOpen && (
                <div className="mt-4 rounded-lg border border-slate-200 bg-slate-50 p-4">
                  <p className="text-xs text-slate-500">
                    {t("set.cfManualHelp")}{" "}
                    <a
                      href="https://dash.cloudflare.com/profile/api-tokens"
                      target="_blank"
                      rel="noreferrer"
                      className="text-sky-700 underline"
                    >
                      {t("set.cfApiTokensLink")}
                    </a>
                  </p>
                  <div className="mt-3 flex gap-2">
                    {(["apitoken", "globalkey"] as const).map((k) => (
                      <label key={k} className="flex items-center gap-1 text-sm text-slate-700">
                        <input
                          type="radio"
                          name="cfKind"
                          checked={cfKind === k}
                          onChange={() => { setCfKind(k); setCfManualErr(""); }}
                        />
                        {k === "apitoken" ? t("set.cfKindToken") : t("set.cfKindGlobal")}
                      </label>
                    ))}
                  </div>
                  <div className="mt-3 grid gap-2">
                    <input
                      value={cfAccName}
                      onChange={(e) => setCfAccName(e.target.value)}
                      placeholder={t("set.cfAccountName")}
                      className="jf-input text-sm"
                    />
                    <input
                      value={cfAccEmail}
                      onChange={(e) => setCfAccEmail(e.target.value)}
                      placeholder={t("set.cfAccountEmail")}
                      type="email"
                      className="jf-input text-sm"
                    />
                    <input
                      value={cfSecret}
                      onChange={(e) => setCfSecret(e.target.value)}
                      placeholder={cfKind === "apitoken" ? t("set.cfApiToken") : t("set.cfGlobalKey")}
                      type="password"
                      autoComplete="off"
                      className="jf-input text-sm"
                    />
                  </div>
                  {cfManualErr && (
                    <p className="mt-2 text-xs text-red-600">{t("set.cfManualErr")}{cfManualErr}</p>
                  )}
                  <button
                    disabled={cfManualBusy}
                    onClick={async () => {
                      setCfManualBusy(true);
                      setCfManualErr("");
                      try {
                        const body = cfKind === "apitoken"
                          ? { account_email: cfAccEmail, api_token: cfSecret, account_name: cfAccName }
                          : { account_email: cfAccEmail, api_key: cfSecret, account_name: cfAccName };
                        const r = await fetch(
                          `${cfKind === "apitoken" ? "/api/cf/api-token" : "/api/cf/global-key"}${workspaceQuery()}`,
                          { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }
                        );
                        const d = await r.json().catch(() => ({}));
                        if (!r.ok) {
                          setCfManualErr(apiErr(d, `HTTP ${r.status}`, t));
                          return;
                        }
                        setCfLinked(true);
                        setCfManualOpen(false);
                        setCfAccName(""); setCfAccEmail(""); setCfSecret("");
                      } finally {
                        setCfManualBusy(false);
                      }
                    }}
                    className="jf-btn-primary mt-3 inline-block"
                  >
                    {cfManualBusy ? t("set.cfConnecting") : t("set.cfConnect")}
                  </button>
                </div>
              )}
              {/* The NEGATIVE answer, so only once there IS an answer: this is
                  the note that painted for ~358ms on a configured deployment
                  before `/api/cf/status` resolved (card t_3c0cc8ce). */}
              {cfStatusLoaded && !cfConfigured && (
                <p className="mt-2 text-xs text-[var(--jf-muted)]">{t("set.cfNotConfigured")}</p>
              )}
            </section>
          </div>
        )}

        {/* ── Tracking tab: global tracking scripts (GTM/analytics) ── */}
        {activeTab === "tracking" && (
          <section
            className="jf-card mt-6 p-6"
            role="tabpanel"
            id={panelDomId("set", "tracking")}
            aria-labelledby={tabDomId("set", "tracking")}
          >
            <h2 className="text-lg font-semibold">{t("set.trackingTitle")}</h2>
            <p className="mt-1 max-w-xl text-sm text-[var(--jf-muted-on-tint)]">
              {t("set.trackingHelp")} <code className="rounded bg-slate-100 px-1">&lt;script src=&quot;https://…&quot;&gt;</code>, <code className="rounded bg-slate-100 px-1">&lt;meta&gt;</code>, comments. Event handlers & cookie writes stripped.
            </p>
            <textarea value={trackingHead} onChange={(e) => setTrackingHead(e.target.value)} rows={4}
              className="jf-input mt-3 max-w-xl font-mono text-xs"
              placeholder={'<script src="https://www.googletagmanager.com/gtag/js?id=G-XXXX"></-script>'} />
            <button onClick={async () => {
              const r = await fetch(`/api/rest/settings/tracking${workspaceQuery()}`, {
                method: "PUT", headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ head: trackingHead }),
              });
              {/* A FAILED PUT here is a FAILURE, not a confirmation: it writes
                  the failure tone (and the inline banner's red side) instead of
                  borrowing the success chrome, and the toast below refuses to
                  put it on a 6s timer (card t_ad2af554). */}
              (r.ok ? showMsgOk : showMsgFail)(t(r.ok ? "set.trackingSaved" : "prof.failed"));
            }} className="jf-btn-primary mt-3">{t("set.saveTracking")}</button>
          </section>
        )}

        {/* ── e-Invoice tab: e-Invoice (LHDN) TIN/MSIC ── */}
        {activeTab === "einvoice" && (
          <section
            className="jf-card mt-6 p-6"
            role="tabpanel"
            id={panelDomId("set", "einvoice")}
            aria-labelledby={tabDomId("set", "einvoice")}
          >
            <h2 className="text-lg font-semibold">{t("set.einvoiceTitle")}</h2>
            <p className="mt-1 max-w-xl text-sm text-[var(--jf-muted)]">
              {t("set.einvoiceHelp")}
            </p>
            <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div>
                <label className="text-xs text-slate-500">{t("set.companyTin")}</label>
                <input value={tin} onChange={(e) => setTin(e.target.value)} className="jf-input mt-1 max-w-xl" placeholder="e.g. C1234567890" />
              </div>
              <div>
                <label className="text-xs text-slate-500">{t("set.msicCode")}</label>
                <input value={msic} onChange={(e) => setMsic(e.target.value)} className="jf-input mt-1 max-w-xl" placeholder="e.g. 62010" />
              </div>
            </div>
            <button onClick={async () => {
              const r = await fetch(`/api/rest/settings/einvoice${workspaceQuery()}`, {
                method: "PUT", headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ tin, msic }),
              });
              {/* Same rule as the Tracking save above: the failed branch of
                  this PUT is a failure and must not read as a success. */}
              (r.ok ? showMsgOk : showMsgFail)(t(r.ok ? "set.einvoiceSaved" : "set.failedSave"));
            }} className="jf-btn-primary mt-3">{t("set.saveEinvoice")}</button>
          </section>
        )}

        {/* ── Payment fees tab: the platform fee the BUYER pays (cards
            t_135c3c72 and t_69d2dd19). The amount is JomForm's own charge, flat
            and per method. The method name is shown HERE and in the merchant's
            own sale row; it must never reach a buyer's fee line (Wan's rule). ── */}
        {activeTab === "fees" && (
          <section
            className="jf-card mt-6 p-6"
            role="tabpanel"
            id={panelDomId("set", "fees")}
            aria-labelledby={tabDomId("set", "fees")}
            data-testid="settings-fees"
          >
            <div className="flex flex-wrap items-center justify-between gap-2">
              <h2 className="text-lg font-semibold">{t("set.feesTitle")}</h2>
              <span
                data-testid="fee-model-pill"
                className="rounded-full border border-emerald-200 bg-emerald-50 px-2.5 py-0.5 text-xs font-semibold text-emerald-700"
              >
                {t("set.feesApplies")}
              </span>
            </div>
            <p className="mt-1 max-w-2xl text-sm text-[var(--jf-muted)]">{t("set.feesHelp")}</p>

            <div className="mt-4 space-y-2" data-testid="fee-platform-rows">
              {feePlatformRows.map((row) => {
                const name = FEE_METHOD_NAMES[row.method] || row.method;
                return (
                  <div
                    key={row.method}
                    data-testid={`fee-rate-${row.method}`}
                    className="flex flex-col gap-2 rounded-lg border border-slate-200 p-3 sm:flex-row sm:items-center sm:justify-between"
                  >
                    <label className="flex items-start gap-2">
                      <input
                        type="checkbox"
                        data-testid={`fee-enabled-${row.method}`}
                        checked={row.fee_enabled}
                        onChange={(e) => {
                          const next = [...feeRates];
                          const i = next.findIndex((x) => x.method === row.method);
                          if (i >= 0) {
                            next[i] = { ...next[i], fee_enabled: e.target.checked };
                            setFeeRates(next);
                          }
                        }}
                        className="mt-1"
                      />
                      <span>
                        <span className="block text-sm font-medium text-slate-800">{name}</span>
                        <span className="block text-xs text-[var(--jf-muted)]">{feeMethodDesc[row.method]}</span>
                      </span>
                    </label>
                    <span className="flex items-center gap-1.5 self-end sm:self-auto">
                      <label htmlFor={`fee-amount-${row.method}`} className="text-xs font-bold text-[var(--jf-muted)]">{currencyLabel(currency)}</label>
                      <input
                        id={`fee-amount-${row.method}`}
                        data-testid={`fee-amount-${row.method}`}
                        value={feeAmountDrafts[row.method] ?? ""}
                        inputMode="decimal"
                        placeholder="—"
                        onFocus={() => setFeePreviewMethod(row.method)}
                        onChange={(e) => setFeeAmountDrafts((d) => ({ ...d, [row.method]: e.target.value }))}
                        className="jf-input w-24 text-right"
                      />
                    </span>
                  </div>
                );
              })}
            </div>
            <p className="mt-2 max-w-2xl text-xs text-[var(--jf-muted)]" data-testid="fee-hint">
              {t("set.feesHint", { cur: currencyLabel(currency) })}
            </p>

            {/* The example box narrates the row last focused, and shows the
                SETTINGS consequence and the RECEIPT line separately: the receipt
                carries the amount and never the method (Wan's rule). */}
            <div
              className="mt-4 max-w-2xl rounded-lg border border-dashed border-slate-300 bg-[var(--jf-primary-tint)] px-3 py-2 text-xs text-slate-700"
              data-testid="fee-example"
            >
              <p>
                {t("set.feesPreview", {
                  amount: formatPrice(10000, currency),
                  method: feePreviewName,
                  total: formatPrice(10000 + feePreviewSen, currency),
                })}
                <br />
                {feePreviewSen > 0
                  ? t("set.feesPreviewLines", { item: formatPrice(10000, currency), fee: formatPrice(feePreviewSen, currency) })
                  : t("set.feesPreviewNoFee", { item: formatPrice(10000, currency) })}
              </p>
              <p className="mt-1.5 border-t border-dashed border-slate-300 pt-1.5">
                {t("set.feesReceiptLine", {
                  line:
                    feePreviewSen > 0
                      ? t("set.feesReceiptFeeLine", { amount: formatPrice(feePreviewSen, currency) })
                      : t("set.feesReceiptNone"),
                })}
                <span className="text-[var(--jf-muted)]">, {t("set.feesNeverNamed")}</span>
              </p>
            </div>

            {/* CHIP's own MDR wording is deliberately NOT touched: this is
                JomForm's charge, and CHIP's fee is the merchant's separate cost. */}
            <p className="mt-3 max-w-2xl text-xs text-[var(--jf-muted)]" data-testid="fee-chip-note">
              {t("set.feesChipNote")}
            </p>

            {/* Legacy percentage rate (migration 000087): kept for one release so
                a workspace that never touches this screen keeps charging exactly
                what it charges today. The move is staged here and committed by
                the one Save below — a stray click cannot re-price a buyer. */}
            {feeLegacyRows.length > 0 && (
              <div
                className="mt-5 max-w-2xl rounded-lg border border-amber-300 bg-amber-50 p-3"
                data-testid="fees-legacy"
              >
                <p className="text-jf-2xs font-bold uppercase tracking-wide text-amber-700">{t("set.feesLegacyTitle")}</p>
                {feeLegacyRows.map((row) => {
                  const name = FEE_METHOD_NAMES[row.method] || row.method;
                  return (
                    <div
                      key={row.method}
                      data-testid={`fee-legacy-${row.method}`}
                      className="mt-2 border-t border-amber-200 pt-2 text-xs text-amber-900"
                    >
                      <p>
                        {t("set.feesLegacyBody", {
                          method: name,
                          rate: feeLegacyRateText(row.fee_fixed_sen, row.fee_percent_bp, currency),
                        })}
                      </p>
                      <div className="mt-2 flex flex-wrap items-end gap-2">
                        <label className="text-jf-2xs text-amber-800">
                          {t("set.feesFixed")}
                          <input
                            type="number"
                            min={0}
                            step={1}
                            data-testid={`fee-legacy-fixed-${row.method}`}
                            value={row.fee_fixed_sen}
                            onChange={(e) => {
                              const next = [...feeRates];
                              const i = next.findIndex((x) => x.method === row.method);
                              if (i >= 0) {
                                next[i] = { ...next[i], fee_fixed_sen: Math.max(0, Math.trunc(Number(e.target.value) || 0)) };
                                setFeeRates(next);
                              }
                            }}
                            className="jf-input mt-0.5 block w-24"
                          />
                        </label>
                        <label className="text-jf-2xs text-amber-800">
                          {t("set.feesPercent")}
                          <input
                            type="number"
                            min={0}
                            max={9999}
                            step={1}
                            data-testid={`fee-legacy-bp-${row.method}`}
                            value={row.fee_percent_bp}
                            onChange={(e) => {
                              const next = [...feeRates];
                              const i = next.findIndex((x) => x.method === row.method);
                              if (i >= 0) {
                                // Clamp to the schema's bound (99.99%): at 100% the
                                // fee grows as fast as the amount and no total
                                // satisfies the gross-up, so the store refuses it.
                                next[i] = {
                                  ...next[i],
                                  fee_percent_bp: Math.min(9999, Math.max(0, Math.trunc(Number(e.target.value) || 0))),
                                };
                                setFeeRates(next);
                              }
                            }}
                            className="jf-input mt-0.5 block w-24"
                          />
                        </label>
                        <label className="text-jf-2xs text-amber-800">
                          {t("set.feesLegacyAmount", { cur: currencyLabel(currency) })}
                          <input
                            type="text"
                            inputMode="decimal"
                            data-testid={`fee-legacy-amount-${row.method}`}
                            value={feeLegacyDrafts[row.method] ?? ""}
                            placeholder="—"
                            onChange={(e) => setFeeLegacyDrafts((d) => ({ ...d, [row.method]: e.target.value }))}
                            className="jf-input mt-0.5 block w-24 text-right"
                          />
                        </label>
                        <button
                          type="button"
                          data-testid={`fee-legacy-move-${row.method}`}
                          onClick={() => stageLegacyMove(row)}
                          className="text-jf-2xs font-bold text-[var(--jf-primary-strong)] underline"
                        >
                          {t("set.feesLegacyMove", { method: name })} →
                        </button>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}

            <label className="mt-4 flex max-w-2xl items-start gap-2 text-sm text-slate-800">
              <input
                type="checkbox"
                data-testid="fee-ack"
                checked={feeAck}
                onChange={(e) => setFeeAck(e.target.checked)}
                className="mt-1"
              />
              <span>{t("set.feesAck")}</span>
            </label>
            {feeAckBy ? (
              <p className="mt-1 text-xs text-[var(--jf-muted)]" data-testid="fee-ack-by">
                {feeAckBy}
              </p>
            ) : null}

            <button
              data-testid="fee-save"
              onClick={async () => {
                // Enabling a fee requires the acknowledgement. The server
                // refuses it too (the API is not a UI); saying so here avoids a
                // round trip that only returns "tick the acknowledgement".
                const enabling = feeRates.some((r) =>
                  r.legacy_grossup
                    ? r.fee_enabled && (r.fee_fixed_sen > 0 || r.fee_percent_bp > 0)
                    : r.fee_enabled && feeSenFromRM(feeAmountDrafts[r.method]) > 0,
                );
                if (enabling && !feeAck) {
                  showMsgFail(t("set.feesNeedAck"));
                  return;
                }
                const r = await fetch(`/api/rest/settings/payment-fees${workspaceQuery()}`, {
                  method: "PUT",
                  headers: { "Content-Type": "application/json" },
                  body: JSON.stringify({
                    acknowledged: feeAck,
                    rates: feeRates.map((x) =>
                      x.legacy_grossup
                        ? {
                            // A legacy row keeps its percentage and its fixed
                            // part; platform_fee_sen is 0 because the store
                            // refuses a row that carries both models.
                            method: x.method,
                            fee_enabled: x.fee_enabled,
                            fee_fixed_sen: x.fee_fixed_sen,
                            fee_percent_bp: x.fee_percent_bp,
                            platform_fee_sen: 0,
                          }
                        : {
                            // The platform model: one flat amount. The legacy
                            // columns are written as 0 so a row can never carry
                            // both a platform amount and a percentage (the store
                            // refuses ErrFeeModelMixed).
                            method: x.method,
                            fee_enabled: x.fee_enabled,
                            fee_fixed_sen: 0,
                            fee_percent_bp: 0,
                            platform_fee_sen: feeSenFromRM(feeAmountDrafts[x.method]),
                          },
                    ),
                  }),
                });
                (r.ok ? showMsgOk : showMsgFail)(t(r.ok ? "set.feesSaved" : "set.failedSave"));
                if (r.ok) await refreshFees();
              }}
              className="jf-btn-primary mt-3"
            >
              {t("set.feesSave")}
            </button>
          </section>
        )}

        {/* ── Webhooks tab: outbound webhooks ── */}
        {activeTab === "webhooks" && (
          <section
            className="jf-card mt-6 p-6"
            role="tabpanel"
            id={panelDomId("set", "webhooks")}
            aria-labelledby={tabDomId("set", "webhooks")}
          >
              <h2 className="text-lg font-semibold">{t("set.webhooksTitle")}</h2>
              <p className="mt-1 max-w-xl text-sm text-[var(--jf-muted-on-tint)]">
                {t("set.eventsLabel")} <code className="rounded bg-slate-100 px-1">sale.paid</code>,{" "}
                <code className="rounded bg-slate-100 px-1">sale.failed</code>,{" "}
                <code className="rounded bg-slate-100 px-1">subscription.charged</code>. {t("set.hmacSigned")}
              </p>
              <form onSubmit={addWebhook} className="mt-3">
                <div className="flex flex-wrap gap-2">
                  {/* Same `min-w-56`-on-a-`flex-1`-child defect as the domains row
                      above (MembersTab.tsx:158): measured at 320px the row was
                      scrollWidth 224 > clientWidth 222, the URL field's box was
                      224px in a 222px row, and its placeholder is a 244.98px glyph
                      run in 198px of content, so it rendered cut at
                      "https://yourapp.com/jomform". `basis-56` + `min-w-0` keeps
                      the 224px desktop preference without the shrink floor. */}
                  <input value={whUrl} onChange={(e) => setWhUrl(e.target.value)} required type="url"
                    placeholder="https://yourapp.com/jomform-hook" className="jf-input min-w-0 max-w-xl flex-1 basis-56 sm:basis-auto" />
                  <input value={whSecret} onChange={(e) => setWhSecret(e.target.value)} required type="text"
                    placeholder={t("set.signingSecret")} className="jf-input w-48" />
                  <button className="jf-btn-primary">{t("set.add")}</button>
                </div>
                {/* Issue #128: event selection checkboxes (default all). */}
                <div className="mt-2">
                  <p className="text-xs text-slate-500">{t("webhook.eventsTitle")}</p>
                  <div className="mt-1 flex flex-wrap items-center gap-3">
                    {WEBHOOK_EVENTS.map((ev) => (
                      <label key={ev} className="flex items-center gap-1 text-sm text-slate-700">
                        <input
                          type="checkbox"
                          checked={whEvents.includes(ev)}
                          onChange={() => toggleWhEvent(whEvents, setWhEvents, ev)}
                        />
                        {ev}
                      </label>
                    ))}
                    <label className="flex items-center gap-1 text-sm text-[var(--jf-muted)]">
                      <input
                        type="checkbox"
                        checked={whEvents.includes("*")}
                        onChange={() => toggleWhEvent(whEvents, setWhEvents, "*")}
                      />
                      {t("webhook.event.all")}
                    </label>
                  </div>
                </div>
              </form>
              <ul className="mt-3 space-y-2">
                {webhooks.map((wh) => {
                  const deliv = deliveries[wh.id];
                  const status = deliveryStatus(deliv);
                  const open = openWh === wh.id;
                  const last = deliv && deliv.length ? deliv[0] : undefined;
                  return (
                    <li key={wh.id} className="rounded-lg border border-[var(--jf-border)]">
                      <div className="flex items-center justify-between gap-3 px-4 py-2.5">
                        <div className="min-w-0">
                          <p className="truncate font-mono text-xs">{wh.url}</p>
                          <p className="mt-0.5 text-jf-2xs text-[var(--jf-muted)]">{(wh.events || []).join(", ")}</p>
                        </div>
                        <div className="flex shrink-0 items-center gap-3">
                          {/* Issue #130: last-delivery status indicator. */}
                          {last && (
                            <span className="flex items-center gap-1.5 text-jf-2xs font-medium"
                              title={t("wh.lastAttempt") + ": " + formatDateTime(last.created_at || "", user?.timezone)}>
                              <span className={`inline-block h-2 w-2 rounded-full ${
                                status === "success" ? "bg-emerald-500"
                                : status === "pending" ? "bg-amber-400"
                                : "bg-red-500"
                              }`} />
                              {status === "success" && t("wh.success")}
                              {status === "pending" && t("wh.pending")}
                              {status === "failed" && t("wh.failed")}
                              <span className="text-[var(--jf-muted)]">· {t("wh.attemptCount", { n: last.attempt })}</span>
                            </span>
                          )}
                          <button onClick={() => openEdit(wh)}
                            className="shrink-0 text-xs text-slate-500 hover:text-slate-800">{t("set.webhookEdit")}</button>
                          <button onClick={() => {
                            const next = open ? null : wh.id;
                            setOpenWh(next);
                            if (next) fetchDeliveries(wh.id);
                          }} className="shrink-0 text-xs text-slate-500 hover:text-slate-800">
                            {open ? t("wh.hideHistory") : t("wh.showHistory")}
                          </button>
                          {/* Card t_818349ab: `load()` here re-applied the
                              pre-delete bundle, so the removed hook survived
                              until a page reload. */}
                          <button onClick={async () => { await fetch(`/api/rest/webhooks/${wh.id}${workspaceQuery()}`, { method: "DELETE" }); reload(); }}
                            className="shrink-0 text-xs text-slate-500 hover:text-red-700">✕</button>
                        </div>
                      </div>
                      {/* Issue #130: expandable delivery history for this webhook. */}
                      {open && (
                        <div className="border-t border-[var(--jf-border)] px-4 py-3">
                          <h4 className="mb-2 text-xs font-semibold text-slate-700">{t("wh.history")}</h4>
                          {(!deliv || deliv.length === 0) ? (
                            <p className="text-xs text-[var(--jf-muted)]">{t("wh.noDeliveries")}</p>
                          ) : (
                            <div className="overflow-x-auto">
                              <table className="w-full min-w-96 text-left text-xs">
                                <thead>
                                  <tr className="text-[var(--jf-muted)]">
                                    <th className="py-1 pr-3 font-medium">{t("wh.colTime")}</th>
                                    <th className="py-1 pr-3 font-medium">{t("wh.colEvent")}</th>
                                    <th className="py-1 pr-3 font-medium">{t("wh.colStatus")}</th>
                                    <th className="py-1 pr-3 font-medium">HTTP</th>
                                    <th className="py-1 pr-3 font-medium">{t("wh.colError")}</th>
                                    <th className="py-1 font-medium">{t("wh.colAction")}</th>
                                  </tr>
                                </thead>
                                <tbody>
                                  {deliv.map((d) => (
                                    <tr key={d.id} className={`border-t border-[var(--jf-border)] ${!d.success ? "bg-red-50/60" : ""}`}>
                                      <td className="py-1.5 pr-3 text-slate-500 whitespace-nowrap">
                                        {d.created_at ? formatDateTime(d.created_at, user?.timezone) : "—"}
                                      </td>
                                      <td className="py-1.5 pr-3 font-mono text-slate-700">{d.event}</td>
                                      <td className={`py-1.5 pr-3 font-medium ${d.success ? "text-emerald-700" : "text-red-700"}`}>
                                        {d.success ? t("wh.success") : t("wh.failed")}
                                      </td>
                                      <td className="py-1.5 pr-3 font-mono text-slate-500">{d.status_code ?? "—"}</td>
                                      <td className="py-1.5 pr-3 font-mono text-red-700">{d.error || "—"}</td>
                                      {/* Issue #197: manual retry of a failed delivery (or dead-lettered retry). */}
                                      <td className="py-1.5">
                                        {!d.success && (
                                          <button onClick={() => retryDelivery(wh.id, d)}
                                            className="shrink-0 text-xs font-medium text-indigo-600 hover:text-indigo-800">
                                            {t("wh.retry")}
                                          </button>
                                        )}
                                      </td>
                                    </tr>
                                  ))}
                                </tbody>
                              </table>
                            </div>
                          )}
                          <p className="mt-2 text-jf-2xs text-[var(--jf-muted)]">{t("wh.retryNote")}</p>
                        </div>
                      )}
                    </li>
                  );
                })}
              </ul>
              {/* Issue #128: inline edit drawer for an existing webhook. */}
              {editId !== null && (
                <form onSubmit={saveWebhook} className="mt-4 rounded-lg border border-slate-200 bg-slate-50 p-4">
                  <h3 className="text-sm font-semibold text-slate-700">{t("set.webhookEditTitle")}</h3>
                  <div className="mt-2 flex flex-wrap gap-2">
                    {/* The third instance of the `min-w-56`-on-a-`flex-1`-child
                        defect (MembersTab.tsx:158). It is worse here: the drawer
                        already spends 16px of padding on each side, so at 320px the
                        row was scrollWidth 224 > clientWidth 188 — 36px over — and
                        the URL placeholder rendered cut. `basis-56` + `min-w-0`
                        keeps the 224px desktop preference without the shrink floor. */}
                    <input value={editUrl} onChange={(e) => setEditUrl(e.target.value)} required type="url"
                      placeholder="https://yourapp.com/jomform-hook" className="jf-input min-w-0 max-w-xl flex-1 basis-56 sm:basis-auto" />
                    <input value={editSecret} onChange={(e) => setEditSecret(e.target.value)} type="text"
                      placeholder={t("set.secretKeep")} className="jf-input w-48" />
                  </div>
                  <div className="mt-2">
                    <p className="text-xs text-slate-500">{t("webhook.eventsTitle")}</p>
                    <div className="mt-1 flex flex-wrap items-center gap-3">
                      {WEBHOOK_EVENTS.map((ev) => (
                        <label key={ev} className="flex items-center gap-1 text-sm text-slate-700">
                          <input
                            type="checkbox"
                            checked={editEvents.includes(ev)}
                            onChange={() => toggleWhEvent(editEvents, setEditEvents, ev)}
                          />
                          {ev}
                        </label>
                      ))}
                      <label className="flex items-center gap-1 text-sm text-[var(--jf-muted-on-tint)]">
                        <input
                          type="checkbox"
                          checked={editEvents.includes("*")}
                          onChange={() => toggleWhEvent(editEvents, setEditEvents, "*")}
                        />
                        {t("webhook.event.all")}
                      </label>
                    </div>
                  </div>
                  <div className="mt-3 flex gap-2">
                    <button className="jf-btn-primary">{t("set.whSave")}</button>
                    <button type="button" onClick={closeEdit} className="jf-btn-ghost text-slate-600">{t("set.whCancel")}</button>
                  </div>
                </form>
              )}
            </section>
        )}

        {/* ── MCP tab removed (issue #307): connected agents are user-level
               and moved to Profile → Connected agents. OAuth agents bind to
               the user and can operate across all of the user's workspaces, not a
               single workspace. */}

        {/* ── Emel tab: per-workspace receipt/notification email (issue #107) ── */}
        {activeTab === "emel" && (
          <section
            className="jf-card mt-6 p-6"
            role="tabpanel"
            id={panelDomId("set", "emel")}
            aria-labelledby={tabDomId("set", "emel")}
          >
            <h2 className="text-lg font-semibold">{t("set.emelTitle")}</h2>
            <p className="mt-1 max-w-xl text-sm text-[var(--jf-muted)]">
              {t("set.emelHelp")}
            </p>
            <div className="mt-4 space-y-3">
              <div>
                <label className="text-xs text-slate-500">
                  {t("set.emelSubject")}{" "}
                  {/* The per-field rule is invisible until the merchant is told the
                      subject and body are judged separately (AGENTS.md
                      documentation-parity rule: an inline doc link next to the
                      field the rule applies to). */}
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
                  value={emelSubject}
                  onChange={(e) => setEmelSubject(e.target.value)}
                  className="jf-input mt-1 max-w-xl"
                  placeholder={t("set.emelSubjectPh")}
                />
              </div>
              <div>
                <label className="text-xs text-slate-500">
                  {t("set.emelBody")} <span className="text-[var(--jf-muted-on-tint)]">{t("set.emelPlaceholders")} <code className="rounded bg-slate-100 px-1">{"{{ref_code}}"}</code> <code className="rounded bg-slate-100 px-1">{"{{amount}}"}</code> <code className="rounded bg-slate-100 px-1">{"{{items}}"}</code> <code className="rounded bg-slate-100 px-1">{"{{form_url}}"}</code> <code className="rounded bg-slate-100 px-1">{"{{buyer_name}}"}</code> <code className="rounded bg-slate-100 px-1">{"{{date}}"}</code></span>{" "}
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
                  value={emelBody}
                  onChange={(e) => setEmelBody(e.target.value)}
                  rows={8}
                  className="jf-input mt-1 max-w-xl font-mono text-xs"
                />
              </div>
              <div className="flex flex-wrap items-center gap-4">
                <label className="flex items-center gap-2 text-sm text-slate-700">
                  <input type="checkbox" checked={emelReceiptEnabled} onChange={(e) => setEmelReceiptEnabled(e.target.checked)} />
                  {t("set.emelReceiptEnabled")}
                </label>
                <label className="flex items-center gap-2 text-sm text-slate-700">
                  <input type="checkbox" checked={emelNotifyMerchant} onChange={(e) => setEmelNotifyMerchant(e.target.checked)} />
                  {t("set.emelNotifyMerchant")}
                </label>
                <label className="flex items-center gap-2 text-sm text-slate-700">
                  <input type="checkbox" checked={emelHtml} onChange={(e) => setEmelHtml(e.target.checked)} />
                  {t("set.emelHtml")}
                </label>
              </div>
              {emelNotifyMerchant && (
                <div>
                  <label className="text-xs text-slate-500">{t("set.emelMerchantEmail")}</label>
                  <input
                    value={emelMerchantEmail}
                    onChange={(e) => setEmelMerchantEmail(e.target.value)}
                    type="email"
                    className="jf-input mt-1 max-w-xl"
                    placeholder="store@yourshop.my"
                  />
                </div>
              )}
              <div className="flex flex-wrap items-center gap-2 pt-1">
                <button onClick={saveEmailSettings} className="jf-btn-primary">{t("set.emelSave")}</button>
                <button type="button" onClick={() => setEmelPreview(!emelPreview)} className="jf-btn-ghost text-slate-600">
                  {t("set.emelPreview")}
                </button>
              </div>
              {emelPreview && (
                <div className="mt-3 rounded-lg border border-slate-200 bg-slate-50 p-4">
                  <p className="text-xs text-[var(--jf-muted-on-tint)]">{t("set.emelPreviewHint")}</p>
                  <p className="mt-2 text-sm font-medium">{renderPreview(emelSubject)}</p>
                  <pre className="mt-2 whitespace-pre-wrap rounded bg-white p-3 text-xs text-slate-700">{renderPreview(emelBody)}</pre>
                </div>
              )}
            </div>
          </section>
        )}
        {/* ── Storefront tab: per-workspace public-form footer (issue #183) ── */}
        {activeTab === "storefront" && (
          <div
            role="tabpanel"
            id={panelDomId("set", "storefront")}
            aria-labelledby={tabDomId("set", "storefront")}
          >
          <section className="jf-card mt-6 p-6">
            <h2 className="text-lg font-semibold">{t("set.sfTitle")}</h2>
            <p className="mt-1 max-w-xl text-sm text-[var(--jf-muted)]">
              {t("set.sfHelp")}
            </p>
            <div className="mt-4 space-y-3">
              <div>
                <label className="text-xs text-slate-500">{t("set.sfBusinessName")}</label>
                <input
                  value={sfBusinessName}
                  onChange={(e) => setSfBusinessName(e.target.value)}
                  className="jf-input mt-1 max-w-xl"
                  placeholder="Acme Sdn Bhd"
                />
              </div>
              <div>
                <label className="text-xs text-slate-500">{t("set.sfCompanyReg")}</label>
                <input
                  value={sfCompanyReg}
                  onChange={(e) => setSfCompanyReg(e.target.value)}
                  className="jf-input mt-1 max-w-xl"
                  placeholder="202001012345"
                />
              </div>
              <div>
                <label className="text-xs text-slate-500">{t("set.sfContactPhone")}</label>
                <input
                  value={sfContactPhone}
                  onChange={(e) => setSfContactPhone(e.target.value)}
                  className="jf-input mt-1 max-w-xl"
                  placeholder="+60 12 345 6789"
                />
              </div>
              <div>
                <label className="text-xs text-slate-500">{t("set.sfContactAddress")}</label>
                <textarea
                  value={sfContactAddress}
                  onChange={(e) => setSfContactAddress(e.target.value)}
                  rows={2}
                  className="jf-input mt-1 max-w-xl"
                  placeholder="1 Jalan Test, Kuala Lumpur"
                />
              </div>
              <div>
                <label className="text-xs text-slate-500" htmlFor="sf-privacy-url">{t("set.sfPrivacyUrl")}</label>
                <input
                  id="sf-privacy-url"
                  aria-describedby="sf-privacy-url-status"
                  value={sfPrivacyUrl}
                  onChange={(e) => setSfPrivacyUrl(e.target.value)}
                  type="url"
                  className="jf-input mt-1 max-w-xl"
                  placeholder="https://…/privacy"
                />
                {/* Card t_e8b4f7da: after Save, say why this link will not appear. */}
                <div id="sf-privacy-url-status">{legalUrlWarning("privacy_url")}</div>
              </div>
              <div>
                <label className="text-xs text-slate-500" htmlFor="sf-terms-url">{t("set.sfTermsUrl")}</label>
                <input
                  id="sf-terms-url"
                  aria-describedby="sf-terms-url-status"
                  value={sfTermsUrl}
                  onChange={(e) => setSfTermsUrl(e.target.value)}
                  type="url"
                  className="jf-input mt-1 max-w-xl"
                  placeholder="https://…/terms"
                />
                <div id="sf-terms-url-status">{legalUrlWarning("terms_url")}</div>
              </div>
              <div>
                <label className="text-xs text-slate-500" htmlFor="sf-refund-url">{t("set.sfRefundUrl")}</label>
                <input
                  id="sf-refund-url"
                  aria-describedby="sf-refund-url-status"
                  value={sfRefundUrl}
                  onChange={(e) => setSfRefundUrl(e.target.value)}
                  type="url"
                  className="jf-input mt-1 max-w-xl"
                  placeholder="https://…/refund"
                />
                <div id="sf-refund-url-status">{legalUrlWarning("refund_url")}</div>
              </div>
              {/* AGENTS.md documentation-parity step 4: an inline "?" beside the
                  fields that decide the footer's links, pointing at the page that
                  states the rule (card t_e8b4f7da). */}
              <p className="flex max-w-xl items-center gap-1.5 text-xs text-slate-500">
                {t("set.sfLegalLinkDocs")}
                <a
                  href="/docs/workspaces#footer-legal-links"
                  target="_blank"
                  rel="noopener noreferrer"
                  title={t("set.sfLegalLinkDocs")}
                  aria-label={t("set.sfLegalLinkDocs")}
                  className="inline-flex h-4 w-4 items-center justify-center rounded-full bg-slate-200 text-jf-3xs font-bold text-slate-600 hover:bg-[var(--jf-primary)] hover:text-white"
                >
                  ?
                </a>
              </p>
              <div>
                <label className="text-xs text-slate-500">{t("set.sfFooterText")}</label>
                <textarea
                  value={sfFooterText}
                  onChange={(e) => setSfFooterText(e.target.value)}
                  rows={2}
                  className="jf-input mt-1 max-w-xl"
                />
              </div>
              {/* Language placement: the WORKSPACE default language for public
                  forms — the fallback a buyer gets when a form carries no
                  per-form language. Sits next to the other Storefront fields
                  because it changes what the storefront/form shows. Explicitly
                  distinct from the per-profile dashboard language on /profile. */}
              <div>
                <label className="text-xs text-slate-500" htmlFor="sf-default-lang">
                  {t("set.sfDefaultLang")}
                </label>
                <select
                  id="sf-default-lang"
                  value={sfDefaultLang}
                  onChange={(e) => setSfDefaultLang(e.target.value)}
                  className="jf-input mt-1 max-w-xl"
                >
                  {LANGS_SHOWN.map((l) => (
                    // The endonym is written in its own language; 中文 inside an
                    // English (or Arabic) document is a language of parts, and an
                    // <option> holds text only — so the declaration goes on the
                    // option itself (WCAG 3.1.2). Same rule as LangSelect.
                    <option key={l} value={l} {...endonymLangProps(l)}>{LANG_LABEL[l]}</option>
                  ))}
                </select>
                <p className="mt-1 max-w-xl text-xs text-slate-500">{t("set.sfDefaultLangHelp")}</p>
              </div>
              {/* Issue #276: per-workspace SEO indexing toggle. */}
              <div className="flex items-start justify-between gap-4 rounded-lg border border-[var(--jf-border)] p-3">
                <div>
                  <p className="text-sm font-medium">{t("set.sfIndexingTitle")}</p>
                  <p className="mt-0.5 max-w-md text-xs text-slate-500">
                    {deployment && t(deployment.selfHosted ? "set.sfIndexingHelpSelf" : "set.sfIndexingHelp")}
                  </p>
                </div>
                {/* Shared switch (web/src/components/Switch.tsx, card t_b73eee21):
                    this markup used to be a hand-rolled pill here, in
                    form-settings and in /sales, and all three copies shipped the
                    default arrow cursor and a near-invisible OFF track. */}
                <Switch
                  checked={sfIndexing}
                  onChange={() => setSfIndexing((v) => !v)}
                  label={t("set.sfIndexingTitle")}
                />
              </div>
              {/* Issue #447: per-workspace realtime notification sound toggle. */}
              <div className="flex items-start justify-between gap-4 rounded-lg border border-[var(--jf-border)] p-3">
                <div>
                  <p className="text-sm font-medium">{t("set.sfSoundTitle")}</p>
                  <p className="mt-0.5 max-w-md text-xs text-slate-500">{t("set.sfSoundHelp")}</p>
                </div>
                <Switch
                  checked={sfSoundEnabled}
                  onChange={() => setSfSoundEnabled((v) => !v)}
                  label={t("set.sfSoundTitle")}
                />
              </div>
              {/* Per-workspace AI spam-judge toggle, with an inline
                  "?" doc link (AGENTS.md documentation-parity rule, step 4). */}
              <div className="flex items-start justify-between gap-4 rounded-lg border border-[var(--jf-border)] p-3">
                <div>
                  <p className="flex items-center gap-2 text-sm font-medium">
                    {t("set.sfSpamTitle")}
                    <a
                      href="/docs/spam-detection#per-workspace-toggle"
                      target="_blank"
                      rel="noopener noreferrer"
                      title={t("set.sfSpamDocs")}
                      className="inline-flex h-4 w-4 items-center justify-center rounded-full bg-slate-200 text-jf-3xs font-bold text-slate-600 hover:bg-[var(--jf-primary)] hover:text-white"
                    >
                      ?
                    </a>
                  </p>
                  <p className="mt-0.5 max-w-md text-xs text-slate-500">{t("set.sfSpamHelp")}</p>
                </div>
                <Switch
                  checked={sfSpamJudgeEnabled}
                  onChange={() => setSfSpamJudgeEnabled((v) => !v)}
                  label={t("set.sfSpamTitle")}
                />
              </div>
              <div className="flex flex-wrap items-center gap-2 pt-1">
                {/* card t_b73eee21: the button carries the PUT's in-flight state —
                    `disabled` while the request is open (so a double-click cannot
                    fire a second PUT with no feedback) and `aria-busy` so the
                    busy state is announced, not just drawn.
                    This card adds the THIRD state: a 2s "Saved ✓" on the button
                    itself, so the confirmation is where the press happened rather
                    than only in the page-top banner. No extra tick glyph — the
                    label already ends in its own "✓" and rendering both gave a
                    visible double tick. */}
                <SaveButton
                  onSave={saveStorefrontSettings}
                  label={t("set.sfSave")}
                  savedLabel={t("set.sfSavedShort")}
                />
              </div>
            </div>
          </section>

          {/* Issue #360: per-workspace customizable landing page (Storefront tab). */}
          <section className="jf-card mt-6 p-6">
            <h2 className="text-lg font-semibold">{t("set.lpTitle")}</h2>
            <p className="mt-1 max-w-xl text-sm text-[var(--jf-muted)]">
              {workspaceAddressLabel(activeWorkspaceSlug, deployment) &&
                t("set.lpHelp", { host: workspaceAddressLabel(activeWorkspaceSlug, deployment) })}
            </p>
            <div className="mt-4 space-y-3">
              {/* Landing page enable toggle. */}
              <div className="flex items-start justify-between gap-4 rounded-lg border border-[var(--jf-border)] p-3">
                <div>
                  <p className="text-sm font-medium">{t("set.lpEnabledTitle")}</p>
                  <p className="mt-0.5 max-w-md text-xs text-slate-500">{t("set.lpEnabledHelp")}</p>
                </div>
                <Switch
                  checked={lpEnabled}
                  onChange={() => setLpEnabled((v) => !v)}
                  label={t("set.lpEnabledTitle")}
                />
              </div>
              {lpEnabled && (
                <>
                  <div>
                    <label className="text-xs text-slate-500">{t("set.lpHero")}</label>
                    <input
                      value={lpHero}
                      onChange={(e) => setLpHero(e.target.value)}
                      className="jf-input mt-1 max-w-xl"
                      placeholder={t("set.lpHeroPh")}
                    />
                  </div>
                  <div>
                    <label className="text-xs text-slate-500">{t("set.lpSubtitle")}</label>
                    <input
                      value={lpSubtitle}
                      onChange={(e) => setLpSubtitle(e.target.value)}
                      className="jf-input mt-1 max-w-xl"
                      placeholder={t("set.lpSubtitlePh")}
                    />
                  </div>
                  <div>
                    <label className="text-xs text-slate-500">{t("set.lpBody")}</label>
                    <textarea
                      value={lpBody}
                      onChange={(e) => setLpBody(e.target.value)}
                      rows={3}
                      className="jf-input mt-1 max-w-xl"
                    />
                  </div>
                  <div>
                    <label className="text-xs text-slate-500">{t("set.lpImageUrl")}</label>
                    <input
                      value={lpImageUrl}
                      onChange={(e) => setLpImageUrl(e.target.value)}
                      type="url"
                      className="jf-input mt-1 max-w-xl"
                      placeholder="https://…/banner.jpg"
                    />
                  </div>
                  <div>
                    <label className="text-xs text-slate-500">{t("set.lpLogoUrl")}</label>
                    <input
                      value={lpLogoUrl}
                      onChange={(e) => setLpLogoUrl(e.target.value)}
                      type="url"
                      className="jf-input mt-1 max-w-xl"
                      placeholder="https://…/logo.png"
                    />
                  </div>
                  {/* Issue #501: a rejected (non-https) value shows here, at
                      the fields, instead of a silently broken storefront
                      image. */}
                  {lpUrlErr && (
                    <p className="max-w-xl rounded-md bg-red-50 px-2 py-1.5 text-xs font-medium text-red-700">
                      {lpUrlErr}
                    </p>
                  )}
                  <div className="flex max-w-xl gap-4">
                    <div className="flex-1">
                      <label className="text-xs text-slate-500">{t("set.lpPrimaryColor")}</label>
                      <div className="mt-1 flex items-center gap-2">
                        <input
                          type="color"
                          value={/^#[0-9a-fA-F]{6}$/.test(lpPrimaryColor) ? lpPrimaryColor : DEFAULT_LP_PRIMARY}
                          onChange={(e) => setLpPrimaryColor(e.target.value)}
                          className="h-9 w-12 rounded border border-[var(--jf-border)] bg-transparent"
                        />
                        <input
                          value={lpPrimaryColor}
                          onChange={(e) => setLpPrimaryColor(e.target.value)}
                          className="jf-input flex-1"
                        />
                      </div>
                    </div>
                    <div className="flex-1">
                      <label className="text-xs text-slate-500">{t("set.lpBackgroundColor")}</label>
                      <div className="mt-1 flex items-center gap-2">
                        <input
                          type="color"
                          value={/^#[0-9a-fA-F]{6}$/.test(lpBackgroundColor) ? lpBackgroundColor : DEFAULT_LP_BACKGROUND}
                          onChange={(e) => setLpBackgroundColor(e.target.value)}
                          className="h-9 w-12 rounded border border-[var(--jf-border)] bg-transparent"
                        />
                        <input
                          value={lpBackgroundColor}
                          onChange={(e) => setLpBackgroundColor(e.target.value)}
                          className="jf-input flex-1"
                        />
                      </div>
                    </div>
                  </div>
                  {/* CTA buttons linking to published forms (or external). */}
                  <div>
                    <label className="text-xs text-slate-500">{t("set.lpCtas")}</label>
                    <div className="mt-1 space-y-2">
                      {lpCtas.map((c, i) => (
                        <div key={i} className="flex max-w-xl items-center gap-2">
                          <input
                            value={c.label}
                            onChange={(e) =>
                              setLpCtas((arr) => arr.map((x, j) => (j === i ? { ...x, label: e.target.value } : x)))
                            }
                            className="jf-input flex-1"
                            placeholder={t("set.lpCtaLabel")}
                          />
                          <input
                            value={c.form_slug}
                            onChange={(e) =>
                              setLpCtas((arr) => arr.map((x, j) => (j === i ? { ...x, form_slug: e.target.value } : x)))
                            }
                            className="jf-input flex-1"
                            placeholder={t("set.lpCtaSlug")}
                          />
                          <button
                            type="button"
                            onClick={() => setLpCtas((arr) => arr.filter((_, j) => j !== i))}
                            className="jf-btn-ghost"
                          >
                            ✕
                          </button>
                        </div>
                      ))}
                      <button
                        type="button"
                        onClick={() => setLpCtas((arr) => [...arr, { label: "", form_slug: "" }])}
                        className="jf-btn-ghost"
                      >
                        + {t("set.lpCtaAdd")}
                      </button>
                    </div>
                  </div>
                  {/* Issue #362: published forms embedded inline on the landing page. */}
                  <div>
                    <label className="text-xs text-slate-500">{t("set.lpForms")}</label>
                    <div className="mt-1 space-y-2">
                      {lpForms.map((c, i) => (
                        <div key={i} className="flex max-w-xl items-center gap-2">
                          <input
                            value={c.label}
                            onChange={(e) =>
                              setLpForms((arr) => arr.map((x, j) => (j === i ? { ...x, label: e.target.value } : x)))
                            }
                            className="jf-input flex-1"
                            placeholder={t("set.lpFormLabel")}
                          />
                          <input
                            value={c.form_slug}
                            onChange={(e) =>
                              setLpForms((arr) => arr.map((x, j) => (j === i ? { ...x, form_slug: e.target.value } : x)))
                            }
                            className="jf-input flex-1"
                            placeholder={t("set.lpFormSlug")}
                          />
                          <button
                            type="button"
                            onClick={() => setLpForms((arr) => arr.filter((_, j) => j !== i))}
                            className="jf-btn-ghost"
                          >
                            ✕
                          </button>
                        </div>
                      ))}
                      <button
                        type="button"
                        onClick={() => setLpForms((arr) => [...arr, { label: "", form_slug: "" }])}
                        className="jf-btn-ghost"
                      >
                        + {t("set.lpFormAdd")}
                      </button>
                    </div>
                  </div>
                  <div className="flex flex-wrap items-center gap-2 pt-1">
                    <SaveButton
                      onSave={saveLandingPage}
                      label={t("set.lpSave")}
                      savedLabel={t("set.lpSavedShort")}
                    />
                    <a
                      href={`https://${activeWorkspaceSlug || "workspace"}.jomform.com`}
                      target="_blank"
                      rel="noreferrer"
                      className="jf-btn-ghost"
                    >
                      {t("set.lpPreview")}
                    </a>
                  </div>
                </>
              )}
            </div>
          </section>
          </div>
        )}

        {/* ── Members tab: workspace members management (issue #163) ── */}
        {activeTab === "members" && isAdmin && (
          <div
            role="tabpanel"
            id={panelDomId("set", "members")}
            aria-labelledby={tabDomId("set", "members")}
          >
            <MembersTab workspaceId={emelWorkspaceId || activeWorkspaceId} />
          </div>
        )}
        {/* ── Workspace tab: identity + Danger zone (delete workspace) ──
            Owner-only inside the component (an admin member is told to leave
            via Members instead). Gated to admin here AND filtered out of
            `visibleTabs` above, so the strip never offers this tab to a role
            whose panel it would not render — a `manage` member clicking it got
            an empty pane (card t_0ef9709c). */}
        {activeTab === "workspace" && isAdmin && (
          <div
            role="tabpanel"
            id={panelDomId("set", "workspace")}
            aria-labelledby={tabDomId("set", "workspace")}
          >
            <WorkspaceTab workspaceId={emelWorkspaceId || activeWorkspaceId} />
          </div>
        )}
        {/* ── Legal tab: per-workspace hosted legal pages (issue #184) ── */}
        {activeTab === "legal" && (
          <div
            role="tabpanel"
            id={panelDomId("set", "legal")}
            aria-labelledby={tabDomId("set", "legal")}
          >
            <LegalTab workspaceId={emelWorkspaceId || activeWorkspaceId} workspaceSlug={activeWorkspaceSlug} />
          </div>
        )}
        {/* ── Currency tab: per-workspace currency + BNM rates (issue #185) ── */}
        {activeTab === "currency" && (
          <section
            className="jf-card mt-6 p-6"
            role="tabpanel"
            id={panelDomId("set", "currency")}
            aria-labelledby={tabDomId("set", "currency")}
          >
            <h2 className="text-lg font-semibold">{t("set.currencyTitle")}</h2>
            <p className="mt-1 max-w-xl text-sm text-[var(--jf-muted)]">
              {t("set.currencyHelp")}
            </p>
            <div className="mt-4 max-w-xl space-y-3">
              <div>
                <label className="text-xs text-slate-500">{t("set.currencyLabel")}</label>
                <select
                  value={currency}
                  onChange={(e) => setCurrency(e.target.value)}
                  className="jf-input mt-1"
                >
                  {CURRENCIES.map((c) => (
                    <option key={c.code} value={c.code}>
                      {c.code} — {c.name}
                    </option>
                  ))}
                </select>
              </div>
              <div className="flex items-center gap-2 pt-1">
                <button onClick={saveCurrency} className="jf-btn-primary">{t("set.save")}</button>
              </div>
            </div>
            <div className="mt-6">
              {/* Card t_467614db: inline "?" doc link (AGENTS.md documentation-parity
                  rule 4). The hint used to claim the table was "used for
                  display/conversion of new amounts" while nothing in the product
                  converted an amount; the guide now says the table is reference
                  data and points at convert_currency for an on-demand figure. */}
              <div className="flex flex-wrap items-start justify-between gap-3">
                <h3 className="text-sm font-semibold text-slate-700">{t("set.currencyRates")}</h3>
                <a
                  href="/docs/workspaces#currency"
                  target="_blank"
                  rel="noopener noreferrer"
                  title={t("set.currencyDocs")}
                  aria-label={t("set.currencyDocs")}
                  className="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-slate-200 text-jf-2xs font-bold text-slate-600 hover:bg-[var(--jf-primary)] hover:text-white"
                >
                  ?
                </a>
              </div>
              <p className="mt-1 text-xs text-[var(--jf-muted)]">
                {t("set.currencyRatesHint")} <span className="text-slate-500">({ratesCached ? t("set.currencyCached") : t("set.currencyFresh")})</span>
              </p>
              {rates.length > 0 ? (
                <div className="mt-2 grid max-h-64 grid-cols-2 gap-x-6 overflow-y-auto sm:grid-cols-3">
                  {rates.map((r) => (
                    <div key={r.currency_code} className="flex items-center justify-between border-b border-slate-100 py-1 text-sm">
                      <span className="font-medium text-slate-700">{r.currency_code}</span>
                      <span className="text-slate-500">{r.rate.toFixed(4)}</span>
                    </div>
                  ))}
                </div>
              ) : (
                <p className="mt-2 text-sm text-[var(--jf-muted)]">—</p>
              )}
            </div>
          </section>
        )}
        {/* ── Affiliates tab: affiliate marketing (issue #444) ── */}
        {activeTab === "affiliates" && (
          <section
            className="jf-card mt-6 p-6"
            role="tabpanel"
            id={panelDomId("set", "affiliates")}
            aria-labelledby={tabDomId("set", "affiliates")}
          >
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div>
                <h2 className="text-lg font-semibold">{t("set.affiliatesTitle")}</h2>
                <p className="mt-1 max-w-xl text-sm text-[var(--jf-muted)]">{t("set.affiliatesHelp")}</p>
              </div>
              {/* Issue #513: inline "?" doc link (AGENTS.md documentation-parity
                  rule 4) — the affiliate surface had no docs at all. */}
              <a
                href="/docs/customize#affiliate-marketing"
                target="_blank"
                rel="noopener noreferrer"
                title={t("set.affiliatesDocs")}
                aria-label={t("set.affiliatesDocs")}
                className="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-slate-200 text-jf-2xs font-bold text-slate-600 hover:bg-[var(--jf-primary)] hover:text-white"
              >
                ?
              </a>
            </div>
            <div className="mt-4 max-w-xl space-y-3">
              {/* Two columns on a phone, the original one-line flex row from
                  `sm:` up.
                  WHY NOT A FLEX ROW AT EVERY WIDTH: a non-wrapping flex row of
                  two `flex-1` inputs, a fixed field and a content-sized button
                  cannot hold at 320-430px. Measured on the built page before this
                  change, the two `flex-1` boxes collapsed to a 30.73px / 26px
                  border box — 5px / 0px of CONTENT once `.jf-input`'s own
                  `padding: 8px 12px` (globals.css, unlayered) took its 24px — so
                  the "ALI" / "Ali" placeholder (a 21.51px / 17.36px glyph run at
                  14px) never rendered and the fields read as empty; that is why
                  they looked like blank boxes in the report. The button came out
                  at 2.9x the width of a field locally (3.2x on the screenshot).
                  A `w-*`/`min-w-*` UTILITY on those inputs cannot win either:
                  `.jf-input` is written outside any `@layer`, so it beats every
                  layered Tailwind utility (measured: `w-32` and `min-w-[200px]`
                  on a `.jf-input` both computed to the parent's full width, not
                  128px / 200px). A grid cell is a DEFINITE width, so the inputs
                  fill it and the button cannot claim space from them — no
                  dependence on which rule wins — and `grid-cols-2` is
                  `repeat(2, minmax(0, 1fr))`, so a long placeholder cannot push
                  the row wider than the card. The button spans both columns, so
                  it is now 2.05-2.07x a field (measured at 320/360/390/430px,
                  every locale) rather than 3.2x, and 0.75x at 1024px. */}
              <div className="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap sm:items-end">
                <div className="min-w-0 sm:flex-1">
                  <label className="text-xs text-slate-500">{t("set.affiliateCode")}</label>
                  <input
                    value={affCode}
                    onChange={(e) => setAffCode(e.target.value)}
                    placeholder="ALI"
                    className="jf-input mt-1"
                  />
                </div>
                <div className="min-w-0 sm:flex-1">
                  <label className="text-xs text-slate-500">{t("set.affiliateName")}</label>
                  <input
                    value={affName}
                    onChange={(e) => setAffName(e.target.value)}
                    placeholder="Ali"
                    className="jf-input mt-1"
                  />
                </div>
                <div className="min-w-0 sm:w-28">
                  <label className="text-xs text-slate-500">{t("set.affiliateCommission")}</label>
                  <input
                    type="number"
                    value={affCommission}
                    onChange={(e) => setAffCommission(e.target.value)}
                    placeholder="10"
                    className="jf-input mt-1"
                  />
                </div>
                <button
                  onClick={createAffiliate}
                  className="jf-btn-primary col-span-2 w-full sm:col-span-1 sm:w-auto"
                >
                  {t("set.affiliateAdd")}
                </button>
              </div>
              {affErr && <p className="text-sm text-red-600">{affErr}</p>}
              {affMsg && <p className="text-sm text-emerald-700">{affMsg}</p>}
            </div>
            <div className="mt-6">
              <h3 className="text-sm font-semibold text-slate-700">{t("set.affiliatesList")}</h3>
              {affiliates.length === 0 ? (
                <p className="mt-2 text-sm text-[var(--jf-muted)]">{t("set.affiliatesEmpty")}</p>
              ) : (
                <div className="mt-2 space-y-2">
                  {affiliates.map((a) => (
                    <div key={a.id} className="flex items-center justify-between rounded-lg border border-slate-100 px-3 py-2 text-sm">
                      <div>
                        <span className="font-medium text-slate-800">{a.code}</span>
                        {a.name && <span className="ml-2 text-slate-500">{a.name}</span>}
                        <span className="ml-2 text-xs text-[var(--jf-muted)]">{a.commission_pct}%</span>
                      </div>
                      <code className="max-w-[50%] truncate text-xs text-[var(--jf-primary)]">{a.referral_url}</code>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </section>
        )}
        {/* ── THE STICKY SAVE BAR (card t_f11d676f) ──────────────────────────
            Measured on the 390px screenshot of this tab: the first viewport
            ended inside the CHIP Collect card — Brand ID, Secret Key and the
            payment-method row all on screen, and NO Save/Connect control
            anywhere in it. The action existed, several screens down, so a
            merchant who had just pasted a key and looked for the next step saw
            nothing to press.

            So on a phone the CHIP panel now carries its Save at the bottom edge
            of the view, always: `sticky bottom-0` (its natural position is the
            end of the panel, so it is in the flow at the end of the page and
            pinned to the viewport while the card is taller than the screen —
            no content is covered because a sticky box keeps its space).

            It submits the SAME form (`form="chip-creds-form"`, HTML's own
            association) rather than owning a second write path, and it is
            DISABLED until the form differs from what the server holds
            (`chipDirty`): the one Save a merchant can see must not be a control
            that provably does nothing.

            `sm:hidden` because the desktop layout shows the card whole; the
            in-flow Save inside the form stays for desktop and for anyone who
            scrolled to the bottom of the card. */}
        {activeTab === "chip" && (
          <div
            data-testid="chip-sticky-save"
            className="sticky bottom-0 z-20 -mx-6 mt-6 flex items-center justify-between gap-3 border-t border-[var(--jf-border)] bg-[var(--jf-surface)] px-6 py-3 sm:hidden"
          >
            <span
              data-testid="chip-sticky-state"
              className="min-w-0 truncate text-xs text-[var(--jf-muted)]"
            >
              {chipStateLabel}
            </span>
            <button
              type="submit"
              form="chip-creds-form"
              disabled={!chipDirty}
              data-testid="chip-save-sticky"
              className="jf-btn-primary shrink-0"
            >
              {t("set.save")}
            </button>
          </div>
        )}

        {/* The save-confirmation toast, bound to the SAME `msg` state that paints
            the banner above: whatever sets the message shows the toast, so this
            is not a second success path. Dismissing clears `msg`, so the inline
            banner goes too — one source of truth, not two.
            It is handed the message's TONE as well as its text (`msgOk`), because
            `msg` carries failures too (the tracking and e-Invoice PUTs, the CHIP
            unverified save): a failure must be red-chromed and, above all, must
            NOT auto-dismiss — the 6s timer lives in the toast and is armed only
            for a success (card t_ad2af554, the rework of #997). The `err` banner
            stays a separate, always-persistent inline red. */}
        <Toast
          message={msg}
          ok={msgOk}
          onDismiss={dismissMsg}
          closeTitle={t("set.tlsCertClose")}
          raised={activeTab === "chip"}
        />
      </div>
    </Shell>
  );
}
