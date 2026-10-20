"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Shell } from "../../components/Shell";
import { useRouter } from "next/navigation";
import { useI18n, LANGS_SHOWN, LANG_LABEL } from "../../components/i18n";
import { useSession } from "../../components/session";
import { formatDateTime } from "../../lib/formatDate";
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

// Issue #323: the CNAME target is consistent across all custom-domain copy.
const CNAME_TARGET = "custom.jomform.com";

type ChipStatus = { configured: boolean; verified?: boolean; mode?: string; secret_key_masked?: string };

// Issue #326: CHIP mode is NOT user-set — it is auto-derived from the secret
// key prefix (sk_live_ → live, sk_test_ → test, else test). One API endpoint;
// test-vs-live is decided by which secret the merchant pastes.
function detectMode(secret: string): "live" | "test" {
  const s = (secret || "").toLowerCase();
  if (s.startsWith("sk_live")) return "live";
  if (s.startsWith("sk_test")) return "test";
  return "test";
}
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
type SettingsTab = "chip" | "domains" | "tracking" | "einvoice" | "webhooks" | "emel" | "members" | "storefront" | "legal" | "currency" | "affiliates" | "workspace";

const TAB_IDS: SettingsTab[] = ["chip", "domains", "tracking", "einvoice", "webhooks", "emel", "members", "storefront", "legal", "currency", "affiliates", "workspace"];

function isSettingsTab(v: string | null): v is SettingsTab {
  return v === "chip" || v === "domains" || v === "tracking" || v === "einvoice" || v === "webhooks" || v === "emel" || v === "members" || v === "storefront" || v === "legal" || v === "currency" || v === "affiliates" || v === "workspace";
}

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
  { id: "fpx", label: "FPX" },
  { id: "fpx_b2b1", label: "FPX B2B1" },
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

export default function Settings() {
  const { t } = useI18n();
  const router = useRouter();
  // The tab the URL names wins over the stored one, and it is resolved in the
  // INITIALIZER (not only in the mount effect below) so the client's first paint
  // already agrees with the deep link — see `tabFromUrl`.
  const [tab, setTab] = useState<SettingsTab>(() => tabFromUrl() ?? loadStoredTab());
  const [status, setStatus] = useState<ChipStatus | null>(null);
  const [brandId, setBrandId] = useState("");
  const [secretKey, setSecretKey] = useState("");
  const [secretMasked, setSecretMasked] = useState("");
  const [emailFallback, setEmailFallback] = useState("");
  const [whitelist, setWhitelist] = useState<string[]>([]);
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
  const [lpPrimaryColor, setLpPrimaryColor] = useState("#7c3aed");
  const [lpBackgroundColor, setLpBackgroundColor] = useState("#ffffff");
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
  // products, profile) reuse it. Saves call reloadSession() to re-fetch.
  const { bundle, anon, reload: reloadSession, role, user } = useSession();
  // Phone tab strip: the settings tab strip is one scrolling row, not a wrapped
  // block (see the tab bar below). This ref points at it.
  const tabStripRef = useRef<HTMLDivElement | null>(null);
  const reload = useCallback(() => {
    // Saves mutate bundle-backed sections: drop the shared cache first so
    // the next load() re-fetches fresh data (and other pages inherit it).
    reloadSession();
  }, [reloadSession]);
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
        if (s.brand_id) setBrandId(s.brand_id);
        if (s.email_fallback) setEmailFallback(s.email_fallback);
        if (Array.isArray(s.whitelist) && s.whitelist.length) setWhitelist(s.whitelist);
        // Issue #327: the full secret is never returned — only a masked tail.
        // Never populate the editable field with a real secret; the merchant
        // can only enter a NEW key to replace the stored one (blank keeps it).
        setSecretKey("");
        setSecretMasked(s.secret_key_masked || "");
        // Issue #325: "verified" is TRUE only after a live CHIP API check
        // passed at save time — never merely because credentials are stored.
        setVerified(!!s.verified);
      }
      setKeys(Array.isArray(k) ? k : []);
    };
    if (bundle) { apply(bundle); return Promise.resolve(); }
    // Issue #48: when the bundle resolved as null (anonymous) there is
    // nothing to load here — the anon redirect below bounces to /landing.
    if (bundle === null) return Promise.resolve();
    return fetch(`/api/rest/dashboard-bundle${workspaceQuery()}`)
      .then((r) => (r.ok ? r.json() : null))
      .then(apply);
  }, [bundle]);

  // Issue #242: re-fetch the custom-domain list directly from the REST
  // endpoint. load() reads bundle.domains from the closure's stale bundle
  // value, so a freshly added domain wouldn't appear until reloadSession()
  // re-renders. Fetching the list here makes new domains show immediately.
  const refreshDomains = useCallback(() => {
    return fetch(`/api/rest/domains${workspaceQuery()}`)
      .then((r) => (r.ok ? r.json() : null))
      .then((list) => {
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
        if (d.currency) setCurrency(d.currency);
        // Issue #360: per-space customizable landing page.
        const lp = d.landing_page || {};
        setLpEnabled(!!lp.enabled);
        setLpHero(lp.hero || "");
        setLpSubtitle(lp.subtitle || "");
        setLpBody(lp.body || "");
        setLpImageUrl(lp.image_url || "");
        setLpPrimaryColor(lp.primary_color || "#7c3aed");
        setLpBackgroundColor(lp.background_color || "#ffffff");
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

  const loadCfStatus = useCallback(() => {
    return fetch("/api/cf/status")
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => {
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
      .finally(() => setCfStatusLoaded(true));
  }, []);

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
        // Issue #326: no mode field — the backend auto-derives live/test from
        // the secret key prefix (sk_live_ / sk_test_).
        email_fallback: emailFallback,
        payment_method_whitelist: whitelist,
      }),
    });
    if (r.ok) {
      // Issue #325: the backend only reports verified:true after a live CHIP
      // API check passed. Trust that value — never assume success.
      const d = await r.json().catch(() => ({}));
      setVerified(d.verified === true);
      // A save that reached the API but whose CHIP check could not be confirmed
      // is NOT a clean success. It is shown on the failure tone so the merchant
      // reads the caveat on the same chrome as its meaning (the sentence itself
      // already says the check was not confirmed).
      (d.verified === true ? showMsgOk : showMsgFail)(t(d.verified === true ? "set.savedCreds" : "set.savedCredsUnverified"));
      await load();
    } else {
      const d = await r.json().catch(() => ({}));
      setVerified(false);
      setErr(apiErr(d, t("set.failedSave"), t));
    }
  }

  async function clearCreds() {
    await fetch(`/api/rest/settings/chip${workspaceQuery()}`, { method: "DELETE" });
    showMsgOk(t("set.credsCleared"));
    await load();
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
      load();
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
      load();
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
        setDomainMsg(t("set.domainAdded", { domain: d.domain }));
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
  const ADMIN_ONLY_TABS: readonly SettingsTab[] = ["chip", "members", "workspace"];
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

  // Stable identity: the toast's dismiss timer is keyed on this callback, and a
  // new function on every render would restart (or drop) the timer. It must live
  // in the component body — a hook inside the JSX below renders conditionally and
  // breaks the hook order.
  const dismissMsg = useCallback(() => setMsg(""), []);

  return (
    <Shell active="settings">
      <main className="w-full flex-1 px-6 py-8">
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
        <div
          ref={tabStripRef}
          role="tablist"
          className="jf-tabstrip mt-4 flex gap-1 overflow-x-auto border-b border-slate-200"
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

        {/* ── CHIP tab ── */}
        {activeTab === "chip" && (
          <section
            className="jf-card mt-6 p-6"
            role="tabpanel"
            id={panelDomId("set", "chip")}
            aria-labelledby={tabDomId("set", "chip")}
          >
            <div className="flex items-center justify-between">
              <h2 className="text-lg font-semibold">{t("set.chipTitle")}</h2>
              {status && (
                <span
                  className={`rounded-full px-3 py-1 text-xs font-semibold ${
                    status.configured
                      ? "border border-emerald-200 bg-emerald-50 text-emerald-700"
                      : "border border-slate-200 bg-slate-50 text-slate-500"
                  }`}
                >
                  {status.configured ? t("set.activeMode", { mode: (status.mode || "test").toUpperCase() }) : t("set.notSet")}
                </span>
              )}
            </div>
            <p className="mt-2 max-w-xl text-sm text-[var(--jf-muted-on-tint)]">
              {t("set.getYour")} <code className="rounded bg-slate-100 px-1">Brand ID</code> {t("set.and")}{" "}
              <code className="rounded bg-slate-100 px-1">{t("set.secretKey")}</code> {t("set.from")}{" "}
              <a
                href="https://portal.chip-in.asia/collect/developers/brands"
                className="text-[var(--jf-primary)] underline"
                target="_blank"
              >
                CHIP Merchant Portal → Developers
              </a>
              . {t("set.keysEncrypted")}
            </p>
            <form onSubmit={save} className="mt-4 space-y-3">
              <div>
                <label className="flex items-center gap-2 text-xs text-slate-500">
                  Brand ID
                  {verified && (
                    <span className="rounded-full border border-emerald-200 bg-emerald-50 px-2 py-0.5 text-[10px] font-semibold text-emerald-700">
                      {t("set.apiVerified")}
                    </span>
                  )}
                </label>
                <input
                  value={brandId}
                  onChange={(e) => setBrandId(e.target.value)}
                  required
                  className="jf-input mt-1 max-w-xl"
                  placeholder="e.g. 5f2b…-uuid"
                />
              </div>
              <div>
                <label className="flex items-center gap-2 text-xs text-slate-500">
                  {t("set.secretKey")}
                  {verified && (
                    <span className="rounded-full border border-emerald-200 bg-emerald-50 px-2 py-0.5 text-[10px] font-semibold text-emerald-700">
                      {t("set.apiVerified")}
                    </span>
                  )}
                </label>
                <input
                  value={secretKey}
                  onChange={(e) => setSecretKey(e.target.value)}
                  type="text"
                  className="jf-input mt-1 max-w-xl font-mono"
                  placeholder={verified && secretMasked ? t("set.secretMaskedPlaceholder") : "sk_live_… / sk_test_…"}
                />
                {verified && secretMasked && (
                  <p className="mt-1 text-xs text-[var(--jf-muted)]">
                    {t("set.secretMasked", { masked: secretMasked })}
                  </p>
                )}
              </div>
              <div>
                <label className="text-xs text-slate-500">{t("set.mode")}</label>
                <p className="mt-1 text-sm">
                  {t("set.autoDetected", { mode: (secretKey ? detectMode(secretKey) : (status?.mode || "test")) === "live" ? t("set.live") : t("set.test") })}
                </p>
                <p className="mt-1 text-xs text-[var(--jf-muted)]">{t("set.modeHint")}</p>
              </div>

              <div>
                <label className="text-xs text-slate-500">
                  {t("set.emailFallback")} <span className="text-[var(--jf-muted)]">{t("set.emailFallbackHint")}</span>
                </label>
                <input
                  value={emailFallback}
                  onChange={(e) => setEmailFallback(e.target.value)}
                  type="email"
                  className="jf-input mt-1 max-w-xl"
                  placeholder="store@yourshop.my"
                />
              </div>

              <div>
                <label className="text-xs text-slate-500">
                  {t("set.paymentMethods")} <span className="text-[var(--jf-muted)]">{t("set.allMethods")}</span>
                </label>
                <div className="mt-1.5 flex flex-wrap gap-1.5">
                  {METHODS.map((m) => (
                    <button
                      key={m.id}
                      type="button"
                      onClick={() => toggleMethod(m.id)}
                      className={`rounded-full border px-3 py-1 text-xs font-medium transition-colors ${
                        whitelist.includes(m.id)
                          ? "border-[var(--jf-primary)] bg-indigo-50 text-[var(--jf-primary-on-tint)]"
                          : "border-slate-200 bg-white text-slate-500 hover:border-slate-300"
                      }`}
                    >
                      {m.label}
                    </button>
                  ))}
                </div>
              </div>

              <div className="flex gap-2 pt-1">
                <button className="jf-btn-primary">{t("set.save")}</button>
                {status?.configured && (
                  <button type="button" onClick={clearCreds} className="jf-btn-ghost text-slate-600">
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
                          <p className="text-[11px] text-emerald-700">{t("set.verified")}</p>
                        ) : (
                          <p className="flex items-center gap-1.5 text-[11px] text-amber-600">
                            {polling[d.id] && (
                              <span className="inline-block h-3 w-3 animate-spin rounded-full border-2 border-amber-500 border-t-transparent" />
                            )}
                            {polling[d.id] ? t("set.checkingDns") : t("set.pendingDns")}
                          </p>
                        )}
                        {/* Issue #322: per-domain auto-CNAME status. */}
                        {d.auto_cname === "created" && (
                          <p className="text-[11px] text-emerald-700">{t("set.autoCnameStatusCreated")}</p>
                        )}
                        {d.auto_cname === "failed" && (
                          <p className="text-[11px] text-red-600">{t("set.autoCnameStatusFailed")}</p>
                        )}
                        {/* Issue #397: per-domain TLS status. */}
                        {d.verified && d.tls_status === "active" && (
                          <p className="text-[11px] text-emerald-700">{t("set.tlsStatusActive")}</p>
                        )}
                        {d.verified && d.tls_status === "pending" && (
                          <p className="text-[11px] text-amber-600">{t("set.tlsStatusPending")}</p>
                        )}
                        {d.verified && d.tls_status === "error" && (
                          <p className="text-[11px] text-red-600">
                            {t("set.tlsStatusError")}
                            {d.tls_error ? ` — ${d.tls_error}` : ""}
                          </p>
                        )}
                        {d.verified && d.tls_status === "custom" && (
                          <p className="text-[11px] text-indigo-600">{t("set.tlsStatusCustom")}</p>
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
                        <p className="text-[11px] font-medium text-slate-500">{t("set.cnameRow")}</p>
                        <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 font-mono text-xs text-slate-700">
                          <span className="rounded bg-white px-1.5 py-0.5 ring-1 ring-slate-200">CNAME</span>
                          <span className="rounded bg-white px-1.5 py-0.5 ring-1 ring-slate-200">{d.domain}</span>
                          <span className="text-[var(--jf-muted-on-tint)]">→</span>
                          <span className="rounded bg-white px-1.5 py-0.5 ring-1 ring-slate-200">{CNAME_TARGET}</span>
                          <button
                            type="button"
                            onClick={() => {
                              navigator.clipboard?.writeText(`CNAME ${d.domain} → ${CNAME_TARGET}`);
                              setDomainMsg(t("set.copy"));
                            }}
                            className="ml-1 text-[11px] text-[#5e6ad2] hover:underline"
                          >
                            {t("set.copy")}
                          </button>
                        </div>
                        {polling[d.id] && (
                          <p className="mt-1.5 text-[11px] text-[var(--jf-muted-on-tint)]">{t("set.pollingHint")}</p>
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
                          <span className="text-[11px] font-medium text-slate-500">{t("set.tlsModeLabel")}</span>
                          <button
                            type="button"
                            onClick={() => setDomainTlsMode(d, "auto")}
                            disabled={!!tlsBusy[d.id] || d.tls_mode === "auto"}
                            className={`rounded-md px-2.5 py-1 text-[11px] font-semibold transition-colors disabled:cursor-not-allowed ${
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
                            className={`rounded-md px-2.5 py-1 text-[11px] font-semibold transition-colors disabled:cursor-not-allowed ${
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
                              className="text-[11px] font-semibold text-[#5e6ad2] hover:underline"
                            >
                              {certOpen[d.id] ? t("set.tlsCertClose") : t("set.tlsCertUpload")}
                            </button>
                          )}
                        </div>
                        {/* Issue #398: per-domain ACME contact email (defaults to the account email). */}
                        <div className="mt-2 flex flex-wrap items-center gap-2">
                          <label className="text-[11px] font-medium text-slate-500">{t("set.tlsEmailLabel")}</label>
                          <input
                            value={tlsEmailInput[d.id] ?? d.tls_email ?? ""}
                            onChange={(e) => setTlsEmailInput((prev) => ({ ...prev, [d.id]: e.target.value }))}
                            placeholder={t("set.tlsEmailPlaceholder")}
                            className="jf-input w-64 font-mono text-[11px]"
                          />
                          <button
                            type="button"
                            onClick={() => setDomainTlsEmail(d)}
                            disabled={!!tlsBusy[d.id]}
                            className="rounded-md border border-[var(--jf-primary)] px-2.5 py-1 text-[11px] font-semibold text-[var(--jf-primary)] transition-colors hover:bg-indigo-50 hover:text-[var(--jf-primary-on-tint)] disabled:cursor-not-allowed disabled:opacity-60"
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
                              className="jf-input w-full font-mono text-[11px]"
                            />
                            <textarea
                              value={certInput[d.id]?.key ?? ""}
                              onChange={(e) =>
                                setCertInput((prev) => ({ ...prev, [d.id]: { ...(prev[d.id] ?? { cert: "", key: "" }), key: e.target.value } }))
                              }
                              placeholder={t("set.tlsKeyPlaceholder")}
                              rows={4}
                              className="jf-input w-full font-mono text-[11px]"
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
                    correct; the rule is a false positive on the API routes. */}
                {cfStatusLoaded && !cfLinked && (
                  /* eslint-disable-next-line @next/next/no-html-link-for-pages */
                  <a href="/api/cf/start" className="jf-btn-primary inline-block">
                    {t("set.cfLogin")}
                  </a>
                )}
                {cfLinked && (
                  <button
                    onClick={async () => {
                      await fetch("/api/cf/disconnect", { method: "POST" });
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
                          cfKind === "apitoken" ? "/api/cf/api-token" : "/api/cf/global-key",
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
                          <p className="mt-0.5 text-[11px] text-[var(--jf-muted)]">{(wh.events || []).join(", ")}</p>
                        </div>
                        <div className="flex shrink-0 items-center gap-3">
                          {/* Issue #130: last-delivery status indicator. */}
                          {last && (
                            <span className="flex items-center gap-1.5 text-[11px] font-medium"
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
                          <button onClick={async () => { await fetch(`/api/rest/webhooks/${wh.id}${workspaceQuery()}`, { method: "DELETE" }); load(); }}
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
                          <p className="mt-2 text-[11px] text-[var(--jf-muted)]">{t("wh.retryNote")}</p>
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
                  placeholder="Resit pembayaran — {{ref_code}}"
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
                <label className="text-xs text-slate-500">{t("set.sfPrivacyUrl")}</label>
                <input
                  value={sfPrivacyUrl}
                  onChange={(e) => setSfPrivacyUrl(e.target.value)}
                  type="url"
                  className="jf-input mt-1 max-w-xl"
                  placeholder="https://…/privacy"
                />
              </div>
              <div>
                <label className="text-xs text-slate-500">{t("set.sfTermsUrl")}</label>
                <input
                  value={sfTermsUrl}
                  onChange={(e) => setSfTermsUrl(e.target.value)}
                  type="url"
                  className="jf-input mt-1 max-w-xl"
                  placeholder="https://…/terms"
                />
              </div>
              <div>
                <label className="text-xs text-slate-500">{t("set.sfRefundUrl")}</label>
                <input
                  value={sfRefundUrl}
                  onChange={(e) => setSfRefundUrl(e.target.value)}
                  type="url"
                  className="jf-input mt-1 max-w-xl"
                  placeholder="https://…/refund"
                />
              </div>
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
                    <option key={l} value={l}>{LANG_LABEL[l]}</option>
                  ))}
                </select>
                <p className="mt-1 max-w-xl text-xs text-slate-500">{t("set.sfDefaultLangHelp")}</p>
              </div>
              {/* Issue #276: per-workspace SEO indexing toggle. */}
              <div className="flex items-start justify-between gap-4 rounded-lg border border-[var(--jf-border)] p-3">
                <div>
                  <p className="text-sm font-medium">{t("set.sfIndexingTitle")}</p>
                  <p className="mt-0.5 max-w-md text-xs text-slate-500">{t("set.sfIndexingHelp")}</p>
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
                      className="inline-flex h-4 w-4 items-center justify-center rounded-full bg-slate-200 text-[10px] font-bold text-slate-600 hover:bg-[var(--jf-primary)] hover:text-white"
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
              {t("set.lpHelp")}
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
                      placeholder="Selamat datang ke kedai kami"
                    />
                  </div>
                  <div>
                    <label className="text-xs text-slate-500">{t("set.lpSubtitle")}</label>
                    <input
                      value={lpSubtitle}
                      onChange={(e) => setLpSubtitle(e.target.value)}
                      className="jf-input mt-1 max-w-xl"
                      placeholder="Barang-barang istimewa untuk anda"
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
                          value={/^#[0-9a-fA-F]{6}$/.test(lpPrimaryColor) ? lpPrimaryColor : "#7c3aed"}
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
                          value={/^#[0-9a-fA-F]{6}$/.test(lpBackgroundColor) ? lpBackgroundColor : "#ffffff"}
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
                            className="jf-btn-ghost shrink-0"
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
                            className="jf-btn-ghost shrink-0"
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
              <h3 className="text-sm font-semibold text-slate-700">{t("set.currencyRates")}</h3>
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
                className="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-slate-200 text-[11px] font-bold text-slate-600 hover:bg-[var(--jf-primary)] hover:text-white"
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
        />
      </main>
    </Shell>
  );
}
