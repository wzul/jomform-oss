"use client";

import { useCallback, useEffect, useState } from "react";
import { Shell } from "../../components/Shell";
import { useRouter } from "next/navigation";
import { useI18n } from "../../components/i18n";
import { useSession } from "../../components/session";
import { formatDateTime } from "../../lib/formatDate";
import { workspaceQuery, workspaceScope } from "../../lib/workspace";
import { MembersTab } from "../../components/MembersTab";
import { LegalTab } from "../../components/LegalTab";

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
type DomainStatus = { domain: string; verified: boolean } | null;

// Issue #68: Settings page is organised into tabs (same pattern as Profile
// page tabs — #5) instead of one long scroll of 8 stacked sections.
//   chip      -> CHIP gateway
//   domains   -> Custom domains + Cloudflare connect (one workflow)
//   tracking  -> Tracking scripts (global GTM/analytics)
//   einvoice  -> e-Invoice TIN/MSIC
//   webhooks  -> Outbound webhooks
//   emel      -> Per-space receipt/notification email templates (issue #107)
//   members   -> Space members management (issue #163)
//   storefront-> Per-space public-form footer (issue #183)
//   legal     -> Per-space hosted legal pages (issue #184)
//   currency  -> Per-space currency + BNM rates (issue #185)
// (Connected accounts — social login connectors — live on the Profile page,
//  issue #301: they are user-level, not per-space. The MCP / connected-agents
//  tab moved to Profile too, issue #307: an OAuth agent is a user-level
//  credential bound to the user, not to a single space.)
type SettingsTab = "chip" | "domains" | "tracking" | "einvoice" | "webhooks" | "emel" | "members" | "storefront" | "legal" | "currency";

const TAB_IDS: SettingsTab[] = ["chip", "domains", "tracking", "einvoice", "webhooks", "emel", "members", "storefront", "legal", "currency"];

function isSettingsTab(v: string | null): v is SettingsTab {
  return v === "chip" || v === "domains" || v === "tracking" || v === "einvoice" || v === "webhooks" || v === "emel" || v === "members" || v === "storefront" || v === "legal" || v === "currency";
}

// Restore the last active tab. Falls back to "chip".
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
  const [tab, setTab] = useState<SettingsTab>(loadStoredTab);
  const [status, setStatus] = useState<ChipStatus | null>(null);
  const [brandId, setBrandId] = useState("");
  const [secretKey, setSecretKey] = useState("");
  const [secretMasked, setSecretMasked] = useState("");
  const [emailFallback, setEmailFallback] = useState("");
  const [whitelist, setWhitelist] = useState<string[]>([]);
  const [msg, setMsg] = useState("");
  const [err, setErr] = useState("");
  const [keys, setKeys] = useState<KeyRow[]>([]);
  const [verified, setVerified] = useState(false);

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
  const [cfNotice, setCfNotice] = useState("");
  // Issue #107: per-space email template + behaviour settings.
  const [emelSubject, setEmelSubject] = useState("");
  const [emelBody, setEmelBody] = useState("");
  const [emelReceiptEnabled, setEmelReceiptEnabled] = useState(true);
  const [emelNotifyMerchant, setEmelNotifyMerchant] = useState(false);
  const [emelMerchantEmail, setEmelMerchantEmail] = useState("");
  const [emelHtml, setEmelHtml] = useState(false);
  const [emelSpaceId, setEmelSpaceId] = useState("");
  // Issue #163: the active space's id, resolved from the session spaces list
  // + localStorage (same key the Shell uses). Used to scope the Members tab.
  const [activeSpaceId, setActiveSpaceId] = useState("");
  // Issue #184: the active space's slug, used to build the public legal-page
  // URLs ({space}.jomform.com/privacy, /terms, /refund).
  const [activeSpaceSlug, setActiveSpaceSlug] = useState("");
  const [emelPreview, setEmelPreview] = useState(false);
  // Issue #183: per-space public-form footer (Storefront tab).
  const [sfBusinessName, setSfBusinessName] = useState("");
  const [sfCompanyReg, setSfCompanyReg] = useState("");
  const [sfContactPhone, setSfContactPhone] = useState("");
  const [sfContactAddress, setSfContactAddress] = useState("");
  const [sfPrivacyUrl, setSfPrivacyUrl] = useState("");
  const [sfTermsUrl, setSfTermsUrl] = useState("");
  const [sfRefundUrl, setSfRefundUrl] = useState("");
  const [sfFooterText, setSfFooterText] = useState("");
  const [sfSpaceId, setSfSpaceId] = useState("");
  // Issue #276: per-space SEO indexing toggle (Storefront tab).
  const [sfIndexing, setSfIndexing] = useState(true);
  // Issue #360: per-space customizable landing page (Storefront tab).
  const [lpEnabled, setLpEnabled] = useState(false);
  const [lpHero, setLpHero] = useState("");
  const [lpSubtitle, setLpSubtitle] = useState("");
  const [lpBody, setLpBody] = useState("");
  const [lpImageUrl, setLpImageUrl] = useState("");
  const [lpPrimaryColor, setLpPrimaryColor] = useState("#7c3aed");
  const [lpBackgroundColor, setLpBackgroundColor] = useState("#ffffff");
  const [lpLogoUrl, setLpLogoUrl] = useState("");
  const [lpCtas, setLpCtas] = useState<{ label: string; form_slug: string }[]>([]);
  // Issue #362: published forms embedded inline on the landing page.
  const [lpForms, setLpForms] = useState<{ label: string; form_slug: string }[]>([]);
  const [lpSpaceId, setLpSpaceId] = useState("");
  // Issue #438: QR ordering flow — restaurant-menu landing template.
  const [menuEnabled, setMenuEnabled] = useState(false);
  const [menuCategories, setMenuCategories] = useState<{ name: string; product_ids: string[] }[]>([]);
  const [menuBestSeller, setMenuBestSeller] = useState("");
  // Issue #185: per-space currency + BNM rates.
  const [currency, setCurrency] = useState("MYR");
  const [currencySpaceId, setCurrencySpaceId] = useState("");
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
  useEffect(() => {
    if (typeof window === "undefined") return;
    const params = new URLSearchParams(window.location.search);
    const hash = window.location.hash.replace("#", "");
    const explicit = params.get("tab");
    const cf = params.get("cloudflare");
    if (cf === "linked") {
      setTab("domains");
      setCfNotice(t("set.cfConnected"));
    } else if (cf === "error") {
      setTab("domains");
      setCfNotice(t("set.cfLinkFailed"));
    } else if (isSettingsTab(explicit)) {
      setTab(explicit);
    } else if (isSettingsTab(hash)) {
      setTab(hash);
    }
  }, [t]);

  // Issue #13: single aggregated fetch replaces the 6-request waterfall
  // (chip, keys, domains, webhooks, einvoice) on mount.
  // Issue #29: routed through the shared session cache — the first page of
  // the session fetches the bundle once; later pages (dashboard, forms,
  // products, profile) reuse it. Saves call reloadSession() to re-fetch.
  const { bundle, anon, reload: reloadSession, role, user } = useSession();
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

  // Issue #107: load the per-space email settings for the active space.
  const { spaces } = useSession();
  const loadEmailSettings = useCallback(() => {
    const sp = Array.isArray(spaces) ? spaces : [];
    if (!sp.length) return;
    // Resolve the active space (same key the Shell uses).
    let slug = "";
    try { slug = window.localStorage.getItem("jf_workspace") || ""; } catch { /* ignore */ }
    const active = sp.find((x) => x.slug === slug) || sp[0];
    if (!active) return;
    setEmelSpaceId(active.id);
    setActiveSpaceId(active.id);
    setActiveSpaceSlug(active.slug);
    fetch(`/api/rest/spaces/${active.id}/email-settings`)
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
  }, [spaces]);

  useEffect(() => {
    loadEmailSettings();
  }, [loadEmailSettings]);

  // Issue #183: load the per-space storefront/footer settings for the active
  // space (same resolution as the email settings above).
  const loadStorefrontSettings = useCallback(() => {
    const sp = Array.isArray(spaces) ? spaces : [];
    if (!sp.length) return;
    let slug = "";
    try { slug = window.localStorage.getItem("jf_workspace") || ""; } catch { /* ignore */ }
    const active = sp.find((x) => x.slug === slug) || sp[0];
    if (!active) return;
    setSfSpaceId(active.id);
    setCurrencySpaceId(active.id);
    setLpSpaceId(active.id);
    fetch(`/api/rest/spaces/${active.id}/settings`)
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
        // Issue #438: QR ordering flow — restaurant-menu landing template.
        const menu = lp.menu || {};
        setMenuEnabled(!!menu.enabled);
        setMenuCategories(Array.isArray(menu.categories) ? menu.categories : []);
        setMenuBestSeller(menu.best_seller || "");
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
  }, [spaces]);

  useEffect(() => {
    loadStorefrontSettings();
  }, [loadStorefrontSettings]);

  // Issue #185: save the active space's currency.
  async function saveCurrency() {
    if (!currencySpaceId) return;
    setMsg("");
    setErr("");
    const r = await fetch(`/api/rest/spaces/${currencySpaceId}/settings`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ currency }),
    });
    if (r.ok) {
      setMsg(t("set.currencySaved"));
    } else {
      const d = await r.json().catch(() => ({}));
      setErr(d.error || t("set.failedSave"));
    }
  }

  // Issue #48: anonymous visit (401 / session expired) bounces to Landing
  // instead of hanging on a shell or flashing a logged-out settings form.
  useEffect(() => {
    if (anon) router.replace("/landing");
  }, [anon, router]);

  const loadCfStatus = useCallback(() => {
    return fetch("/api/cf/status")
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => {
        if (d) {
          setCfLinked(!!d.linked);
          setCfConfigured(d.configured !== false);
        }
      })
      .catch(() => {});
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
      setMsg(d.verified === true ? t("set.savedCreds") : t("set.savedCredsUnverified"));
      await load();
    } else {
      const d = await r.json().catch(() => ({}));
      setVerified(false);
      setErr(d.error || t("set.failedSave"));
    }
  }

  async function clearCreds() {
    await fetch(`/api/rest/settings/chip${workspaceQuery()}`, { method: "DELETE" });
    setMsg(t("set.credsCleared"));
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
      setErr(t("set.whAddFail", { error: d.error || t("set.failedSave") }));
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
      setMsg(t("set.whUpdated"));
      setEditId(null); setEditUrl(""); setEditSecret(""); setEditEvents([]);
      load();
    } else {
      const d = await r.json().catch(() => ({}));
      setErr(t("set.whUpdateFail", { error: d.error || t("set.failedSave") }));
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
      const r = await fetch(`/api/rest/webhooks/${webhookID}/deliveries?limit=10&workspace=${encodeURIComponent(workspaceScope())}`);
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
      const r = await fetch(`${path}?workspace=${encodeURIComponent(workspaceScope())}`, { method: "POST" });
      if (r.ok) {
        setMsg(t("wh.retryQueued"));
        await fetchDeliveries(webhookID);
      } else {
        const d = await r.json().catch(() => ({}));
        setErr(d.error || t("wh.retryFail"));
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
      setDomainMsg(d.error || t("set.failedAddDomain"));
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
        setDomainMsg(e.error || t("set.tlsModeFailed"));
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
        setDomainMsg(e.error || t("set.tlsEmailFailed"));
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
        setDomainMsg(e.error || t("set.tlsCertFailed"));
      }
      await refreshDomains();
    } finally {
      setTlsBusy((prev) => ({ ...prev, [d.id]: false }));
    }
  }

  // Issue #107: save the per-space email settings.
  async function saveEmailSettings() {
    if (!emelSpaceId) return;
    setMsg("");
    setErr("");
    const r = await fetch(`/api/rest/spaces/${emelSpaceId}/email-settings`, {
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
      setMsg(t("set.emelSaved"));
    } else {
      const d = await r.json().catch(() => ({}));
      setErr(d.error || t("set.failedSave"));
    }
  }

  // Issue #183: save the per-space public-form footer (Storefront tab).
  async function saveStorefrontSettings() {
    if (!sfSpaceId) return;
    setMsg("");
    setErr("");
    const r = await fetch(`/api/rest/spaces/${sfSpaceId}/settings`, {
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
      }),
    });
    if (r.ok) {
      setMsg(t("set.sfSaved"));
    } else {
      const d = await r.json().catch(() => ({}));
      setErr(d.error || t("set.failedSave"));
    }
  }

  // Issue #360: save the per-space customizable landing page (Storefront tab).
  async function saveLandingPage() {
    if (!lpSpaceId) return;
    setMsg("");
    setErr("");
    const r = await fetch(`/api/rest/spaces/${lpSpaceId}/settings`, {
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
          // Issue #438: QR ordering flow — restaurant-menu landing template.
          menu: {
            enabled: menuEnabled,
            categories: menuCategories,
            best_seller: menuBestSeller,
          },
        },
      }),
    });
    if (r.ok) {
      setMsg(t("set.lpSaved"));
    } else {
      const d = await r.json().catch(() => ({}));
      setErr(d.error || t("set.failedSave"));
    }
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

  // Issue #163: role-gate the settings tabs. CHIP credentials and member
  // management are admin-only; read_only members see no write tabs at all.
  const isAdmin = role === "admin" || role === "owner";
  const isReadOnly = role === "read_only";
  const visibleTabs: SettingsTab[] = isReadOnly
    ? []
    : TAB_IDS.filter((id) => (id === "chip" || id === "members") ? isAdmin : true);

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

        {/* Tab bar (issue #68) — same styling as Profile. */}
        {visibleTabs.length > 0 && (
        <div className="mt-4 flex flex-wrap gap-1 border-b border-slate-200">
          {visibleTabs.map((id) => (
            <button
              key={id}
              onClick={() => setTab(id)}
              aria-selected={tab === id}
              role="tab"
              className={`-mb-px whitespace-nowrap border-b-2 px-3 py-2 text-sm font-medium sm:px-4 ${
                tab === id
                  ? "border-slate-900 text-slate-900"
                  : "border-transparent text-slate-500 hover:text-slate-800"
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
            active tab (Profile-style). */}
        {cfNotice && <p className="mt-4 rounded-lg bg-emerald-50 px-3 py-2 text-sm text-emerald-700">{cfNotice}</p>}
        {msg && <p className="mt-4 rounded-lg bg-emerald-50 px-3 py-2 text-sm text-emerald-600">{msg}</p>}
        {err && <p className="mt-4 rounded-lg bg-red-50 px-3 py-2 text-sm text-red-600">{err}</p>}

        {/* ── CHIP tab ── */}
        {tab === "chip" && (
          <section className="jf-card mt-6 p-6">
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
            <p className="mt-2 max-w-xl text-sm text-slate-400">
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
                  <p className="mt-1 text-xs text-slate-400">
                    {t("set.secretMasked", { masked: secretMasked })}
                  </p>
                )}
              </div>
              <div>
                <label className="text-xs text-slate-500">{t("set.mode")}</label>
                <p className="mt-1 text-sm">
                  {t("set.autoDetected", { mode: (secretKey ? detectMode(secretKey) : (status?.mode || "test")) === "live" ? t("set.live") : t("set.test") })}
                </p>
                <p className="mt-1 text-xs text-slate-400">{t("set.modeHint")}</p>
              </div>

              <div>
                <label className="text-xs text-slate-500">
                  {t("set.emailFallback")} <span className="text-slate-400">{t("set.emailFallbackHint")}</span>
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
                  {t("set.paymentMethods")} <span className="text-slate-400">{t("set.allMethods")}</span>
                </label>
                <div className="mt-1.5 flex flex-wrap gap-1.5">
                  {METHODS.map((m) => (
                    <button
                      key={m.id}
                      type="button"
                      onClick={() => toggleMethod(m.id)}
                      className={`rounded-full border px-3 py-1 text-xs font-medium transition-colors ${
                        whitelist.includes(m.id)
                          ? "border-[var(--jf-primary)] bg-indigo-50 text-[var(--jf-primary)]"
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
        {tab === "domains" && (
          <>
            <section className="jf-card mt-6 p-6">
              <h2 className="text-lg font-semibold">{t("set.domainsTitle")}</h2>
              <p className="mt-2 max-w-xl text-sm text-slate-400">
                {t("set.domainsHelp")}
              </p>
              {domainMsg && <p className="mt-3 rounded-lg bg-indigo-50 px-3 py-2 text-sm text-indigo-700">{domainMsg}</p>}
              <form onSubmit={setDomainClaim} className="mt-4 flex flex-wrap gap-2">
                <input
                  value={domainInput}
                  onChange={(e) => setDomainInput(e.target.value)}
                  placeholder="shop.yourdomain.com"
                  className="jf-input min-w-56 max-w-xl flex-1"
                />
                <button className="jf-btn-primary" type="submit">{t("set.addDomain")}</button>
              </form>
              {/* Issue #322: when Cloudflare is linked, offer one-click auto-CNAME. */}
              {cfLinked && (
                <p className="mt-2 text-xs text-emerald-600">{t("set.autoCnameHint")}</p>
              )}
              <ul className="mt-4 space-y-2">
                {domains.map((d: { id: string; domain: string; verified: boolean; auto_cname?: string; tls_mode?: string; tls_status?: string; tls_error?: string; tls_email?: string }) => (
                  <li key={d.id} className="rounded-lg border border-[var(--jf-border)] px-4 py-3">
                    <div className="flex items-center justify-between gap-3">
                      <div className="min-w-0">
                        <p className="truncate text-sm font-medium">{d.domain}</p>
                        {d.verified ? (
                          <p className="text-[11px] text-emerald-600">{t("set.verified")}</p>
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
                          <p className="text-[11px] text-emerald-600">{t("set.autoCnameStatusCreated")}</p>
                        )}
                        {d.auto_cname === "failed" && (
                          <p className="text-[11px] text-red-600">{t("set.autoCnameStatusFailed")}</p>
                        )}
                        {/* Issue #397: per-domain TLS status. */}
                        {d.verified && d.tls_status === "active" && (
                          <p className="text-[11px] text-emerald-600">{t("set.tlsStatusActive")}</p>
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
                            className="rounded-md border border-[var(--jf-primary)] px-3 py-1.5 text-xs font-semibold text-[var(--jf-primary)] transition-colors hover:bg-indigo-50 disabled:cursor-not-allowed disabled:opacity-60"
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
                          className="text-xs text-slate-500 hover:text-red-500"
                        >{t("set.remove")}</button>
                      </div>
                    </div>
                    {!d.verified && (
                      <div className="mt-2 rounded-md bg-slate-50 px-3 py-2">
                        <p className="text-[11px] font-medium text-slate-500">{t("set.cnameRow")}</p>
                        <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 font-mono text-xs text-slate-700">
                          <span className="rounded bg-white px-1.5 py-0.5 ring-1 ring-slate-200">CNAME</span>
                          <span className="rounded bg-white px-1.5 py-0.5 ring-1 ring-slate-200">{d.domain}</span>
                          <span className="text-slate-400">→</span>
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
                          <p className="mt-1.5 text-[11px] text-slate-400">{t("set.pollingHint")}</p>
                        )}
                      </div>
                    )}
                    {d.verified && (
                      <div className="mt-2">
                        <a
                          href={`https://${d.domain}`}
                          target="_blank"
                          rel="noreferrer"
                          className="text-xs font-semibold text-emerald-600 hover:underline"
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
                                : "border border-[var(--jf-primary)] text-[var(--jf-primary)] hover:bg-indigo-50"
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
                                : "border border-[var(--jf-primary)] text-[var(--jf-primary)] hover:bg-indigo-50"
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
                            className="rounded-md border border-[var(--jf-primary)] px-2.5 py-1 text-[11px] font-semibold text-[var(--jf-primary)] transition-colors hover:bg-indigo-50 disabled:cursor-not-allowed disabled:opacity-60"
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
              <p className="mt-2 text-sm text-slate-400">
                {t("set.cfHelp")}
              </p>
              <div className="mt-3 flex gap-2">
                {!cfLinked && (
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
                {!cfLinked && (
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
                      className="text-sky-600 underline"
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
                          setCfManualErr(d.error || `HTTP ${r.status}`);
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
              {!cfConfigured && (
                <p className="mt-2 text-xs text-slate-400">{t("set.cfNotConfigured")}</p>
              )}
            </section>
          </>
        )}

        {/* ── Tracking tab: global tracking scripts (GTM/analytics) ── */}
        {tab === "tracking" && (
          <section className="jf-card mt-6 p-6">
            <h2 className="text-lg font-semibold">{t("set.trackingTitle")}</h2>
            <p className="mt-1 max-w-xl text-sm text-slate-400">
              {t("set.trackingHelp")} <code className="rounded bg-slate-100 px-1">&lt;script src="https://…"&gt;</code>, <code className="rounded bg-slate-100 px-1">&lt;meta&gt;</code>, comments. Event handlers & cookie writes stripped.
            </p>
            <textarea value={trackingHead} onChange={(e) => setTrackingHead(e.target.value)} rows={4}
              className="jf-input mt-3 max-w-xl font-mono text-xs"
              placeholder={'<script src="https://www.googletagmanager.com/gtag/js?id=G-XXXX"></-script>'} />
            <button onClick={async () => {
              const r = await fetch(`/api/rest/settings/tracking${workspaceQuery()}`, {
                method: "PUT", headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ head: trackingHead }),
              });
              setMsg(r.ok ? t("set.trackingSaved") : t("prof.failed"));
            }} className="jf-btn-primary mt-3">{t("set.saveTracking")}</button>
          </section>
        )}

        {/* ── e-Invoice tab: e-Invoice (LHDN) TIN/MSIC ── */}
        {tab === "einvoice" && (
          <section className="jf-card mt-6 p-6">
            <h2 className="text-lg font-semibold">{t("set.einvoiceTitle")}</h2>
            <p className="mt-1 max-w-xl text-sm text-slate-400">
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
              setMsg(r.ok ? t("set.einvoiceSaved") : t("set.failedSave"));
            }} className="jf-btn-primary mt-3">{t("set.saveEinvoice")}</button>
          </section>
        )}

        {/* ── Webhooks tab: outbound webhooks ── */}
        {tab === "webhooks" && (
          <section className="jf-card mt-6 p-6">
              <h2 className="text-lg font-semibold">{t("set.webhooksTitle")}</h2>
              <p className="mt-1 max-w-xl text-sm text-slate-400">
                {t("set.eventsLabel")} <code className="rounded bg-slate-100 px-1">sale.paid</code>,{" "}
                <code className="rounded bg-slate-100 px-1">sale.failed</code>,{" "}
                <code className="rounded bg-slate-100 px-1">subscription.charged</code>. {t("set.hmacSigned")}
              </p>
              <form onSubmit={addWebhook} className="mt-3">
                <div className="flex flex-wrap gap-2">
                  <input value={whUrl} onChange={(e) => setWhUrl(e.target.value)} required type="url"
                    placeholder="https://yourapp.com/jomform-hook" className="jf-input min-w-56 max-w-xl flex-1" />
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
                    <label className="flex items-center gap-1 text-sm text-slate-400">
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
                          <p className="mt-0.5 text-[11px] text-slate-400">{(wh.events || []).join(", ")}</p>
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
                              <span className="text-slate-400">· {t("wh.attemptCount", { n: last.attempt })}</span>
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
                            className="shrink-0 text-xs text-slate-500 hover:text-red-500">✕</button>
                        </div>
                      </div>
                      {/* Issue #130: expandable delivery history for this webhook. */}
                      {open && (
                        <div className="border-t border-[var(--jf-border)] px-4 py-3">
                          <h4 className="mb-2 text-xs font-semibold text-slate-700">{t("wh.history")}</h4>
                          {(!deliv || deliv.length === 0) ? (
                            <p className="text-xs text-slate-400">{t("wh.noDeliveries")}</p>
                          ) : (
                            <div className="overflow-x-auto">
                              <table className="w-full min-w-96 text-left text-xs">
                                <thead>
                                  <tr className="text-slate-400">
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
                                      <td className={`py-1.5 pr-3 font-medium ${d.success ? "text-emerald-600" : "text-red-600"}`}>
                                        {d.success ? t("wh.success") : t("wh.failed")}
                                      </td>
                                      <td className="py-1.5 pr-3 font-mono text-slate-500">{d.status_code ?? "—"}</td>
                                      <td className="py-1.5 pr-3 font-mono text-red-600">{d.error || "—"}</td>
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
                          <p className="mt-2 text-[11px] text-slate-400">{t("wh.retryNote")}</p>
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
                    <input value={editUrl} onChange={(e) => setEditUrl(e.target.value)} required type="url"
                      placeholder="https://yourapp.com/jomform-hook" className="jf-input min-w-56 max-w-xl flex-1" />
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
                      <label className="flex items-center gap-1 text-sm text-slate-400">
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
               the user and can operate across all of the user's spaces, not a
               single space. */}

        {/* ── Emel tab: per-space receipt/notification email (issue #107) ── */}
        {tab === "emel" && (
          <section className="jf-card mt-6 p-6">
            <h2 className="text-lg font-semibold">{t("set.emelTitle")}</h2>
            <p className="mt-1 max-w-xl text-sm text-slate-400">
              {t("set.emelHelp")}
            </p>
            <div className="mt-4 space-y-3">
              <div>
                <label className="text-xs text-slate-500">{t("set.emelSubject")}</label>
                <input
                  value={emelSubject}
                  onChange={(e) => setEmelSubject(e.target.value)}
                  className="jf-input mt-1 max-w-xl"
                  placeholder="Resit pembayaran — {{ref_code}}"
                />
              </div>
              <div>
                <label className="text-xs text-slate-500">
                  {t("set.emelBody")} <span className="text-slate-400">{t("set.emelPlaceholders")} <code className="rounded bg-slate-100 px-1">{"{{ref_code}}"}</code> <code className="rounded bg-slate-100 px-1">{"{{amount}}"}</code> <code className="rounded bg-slate-100 px-1">{"{{items}}"}</code> <code className="rounded bg-slate-100 px-1">{"{{form_url}}"}</code> <code className="rounded bg-slate-100 px-1">{"{{buyer_name}}"}</code> <code className="rounded bg-slate-100 px-1">{"{{date}}"}</code></span>
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
                  <p className="text-xs text-slate-400">{t("set.emelPreviewHint")}</p>
                  <p className="mt-2 text-sm font-medium">{renderPreview(emelSubject)}</p>
                  <pre className="mt-2 whitespace-pre-wrap rounded bg-white p-3 text-xs text-slate-700">{renderPreview(emelBody)}</pre>
                </div>
              )}
            </div>
          </section>
        )}
        {/* ── Storefront tab: per-space public-form footer (issue #183) ── */}
        {tab === "storefront" && (
          <>
          <section className="jf-card mt-6 p-6">
            <h2 className="text-lg font-semibold">{t("set.sfTitle")}</h2>
            <p className="mt-1 max-w-xl text-sm text-slate-400">
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
              {/* Issue #276: per-space SEO indexing toggle. */}
              <div className="flex items-start justify-between gap-4 rounded-lg border border-[var(--jf-border)] p-3">
                <div>
                  <p className="text-sm font-medium">{t("set.sfIndexingTitle")}</p>
                  <p className="mt-0.5 max-w-md text-xs text-slate-500">{t("set.sfIndexingHelp")}</p>
                </div>
                <button
                  type="button"
                  role="switch"
                  aria-checked={sfIndexing}
                  onClick={() => setSfIndexing((v) => !v)}
                  className={`relative h-6 w-11 shrink-0 rounded-full transition-colors ${
                    sfIndexing ? "bg-[var(--jf-primary)]" : "bg-slate-300"
                  }`}
                >
                  <span
                    className={`absolute top-0.5 h-5 w-5 rounded-full bg-white shadow transition-all ${
                      sfIndexing ? "left-[22px]" : "left-0.5"
                    }`}
                  />
                </button>
              </div>
              <div className="flex flex-wrap items-center gap-2 pt-1">
                <button onClick={saveStorefrontSettings} className="jf-btn-primary">{t("set.sfSave")}</button>
              </div>
            </div>
          </section>

          {/* Issue #360: per-space customizable landing page (Storefront tab). */}
          <section className="jf-card mt-6 p-6">
            <h2 className="text-lg font-semibold">{t("set.lpTitle")}</h2>
            <p className="mt-1 max-w-xl text-sm text-slate-400">
              {t("set.lpHelp")}
            </p>
            <div className="mt-4 space-y-3">
              {/* Landing page enable toggle. */}
              <div className="flex items-start justify-between gap-4 rounded-lg border border-[var(--jf-border)] p-3">
                <div>
                  <p className="text-sm font-medium">{t("set.lpEnabledTitle")}</p>
                  <p className="mt-0.5 max-w-md text-xs text-slate-500">{t("set.lpEnabledHelp")}</p>
                </div>
                <button
                  type="button"
                  role="switch"
                  aria-checked={lpEnabled}
                  onClick={() => setLpEnabled((v) => !v)}
                  className={`relative h-6 w-11 shrink-0 rounded-full transition-colors ${
                    lpEnabled ? "bg-[var(--jf-primary)]" : "bg-slate-300"
                  }`}
                >
                  <span
                    className={`absolute top-0.5 h-5 w-5 rounded-full bg-white shadow transition-all ${
                      lpEnabled ? "left-[22px]" : "left-0.5"
                    }`}
                  />
                </button>
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
                            className="jf-btn-outline shrink-0"
                          >
                            ✕
                          </button>
                        </div>
                      ))}
                      <button
                        type="button"
                        onClick={() => setLpCtas((arr) => [...arr, { label: "", form_slug: "" }])}
                        className="jf-btn-outline"
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
                            className="jf-btn-outline shrink-0"
                          >
                            ✕
                          </button>
                        </div>
                      ))}
                      <button
                        type="button"
                        onClick={() => setLpForms((arr) => [...arr, { label: "", form_slug: "" }])}
                        className="jf-btn-outline"
                      >
                        + {t("set.lpFormAdd")}
                      </button>
                    </div>
                  </div>
                  <div className="flex flex-wrap items-center gap-2 pt-1">
                    <button onClick={saveLandingPage} className="jf-btn-primary">{t("set.lpSave")}</button>
                    <a
                      href={`https://${activeSpaceSlug || "space"}.jomform.com`}
                      target="_blank"
                      rel="noreferrer"
                      className="jf-btn-outline"
                    >
                      {t("set.lpPreview")}
                    </a>
                  </div>
                </>
              )}
            </div>
          </section>

          {/* Issue #438: QR ordering flow — restaurant-menu landing template. */}
          <section className="jf-card mt-6 p-6">
            <h2 className="text-lg font-semibold">{t("set.menuTitle")}</h2>
            <p className="mt-1 max-w-xl text-sm text-slate-400">
              {t("set.menuHelp")}
            </p>
            <div className="mt-4 space-y-3">
              <div className="flex items-start justify-between gap-4 rounded-lg border border-[var(--jf-border)] p-3">
                <div>
                  <p className="text-sm font-medium">{t("set.menuEnabledTitle")}</p>
                  <p className="mt-0.5 max-w-md text-xs text-slate-500">{t("set.menuEnabledHelp")}</p>
                </div>
                <button
                  type="button"
                  role="switch"
                  aria-checked={menuEnabled}
                  onClick={() => setMenuEnabled((v) => !v)}
                  className={`relative h-6 w-11 shrink-0 rounded-full transition-colors ${
                    menuEnabled ? "bg-[var(--jf-primary)]" : "bg-slate-300"
                  }`}
                >
                  <span
                    className={`absolute top-0.5 h-5 w-5 rounded-full bg-white shadow transition-all ${
                      menuEnabled ? "left-[22px]" : "left-0.5"
                    }`}
                  />
                </button>
              </div>
              {menuEnabled && (
                <>
                  <div>
                    <label className="text-xs text-slate-500">{t("set.menuBestSeller")}</label>
                    <input
                      value={menuBestSeller}
                      onChange={(e) => setMenuBestSeller(e.target.value)}
                      className="jf-input mt-1 max-w-xl font-mono"
                      placeholder="product-id"
                    />
                  </div>
                  <div>
                    <label className="text-xs text-slate-500">{t("set.menuCategories")}</label>
                    <div className="mt-1 space-y-2">
                      {menuCategories.map((c, i) => (
                        <div key={i} className="flex flex-col gap-1.5 rounded-lg border border-[var(--jf-border)] p-3">
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
                              className="jf-btn-ghost px-2 py-1 text-[11px] text-red-600"
                            >
                              {t("set.menuCatRemove")}
                            </button>
                          </div>
                          <input
                            value={c.product_ids.join(", ")}
                            onChange={(e) =>
                              setMenuCategories((arr) =>
                                arr.map((x, j) =>
                                  j === i
                                    ? { ...x, product_ids: e.target.value.split(",").map((s) => s.trim()).filter(Boolean) }
                                    : x
                                )
                              )
                            }
                            className="jf-input font-mono"
                            placeholder="id1, id2, id3"
                          />
                        </div>
                      ))}
                      <button
                        type="button"
                        onClick={() => setMenuCategories((arr) => [...arr, { name: "", product_ids: [] }])}
                        className="jf-btn-outline"
                      >
                        + {t("set.menuCatAdd")}
                      </button>
                    </div>
                  </div>
                  <div className="flex flex-wrap items-center gap-2 pt-1">
                    <button onClick={saveLandingPage} className="jf-btn-primary">{t("set.lpSave")}</button>
                    <a
                      href={`https://${activeSpaceSlug || "space"}.jomform.com`}
                      target="_blank"
                      rel="noreferrer"
                      className="jf-btn-outline"
                    >
                      {t("set.menuPreview")}
                    </a>
                  </div>
                </>
              )}
            </div>
          </section>
          </>
        )}
        {/* ── Members tab: space members management (issue #163) ── */}
        {tab === "members" && isAdmin && (
          <MembersTab spaceId={emelSpaceId || activeSpaceId} />
        )}
        {/* ── Legal tab: per-space hosted legal pages (issue #184) ── */}
        {tab === "legal" && (
          <LegalTab spaceId={emelSpaceId || activeSpaceId} spaceSlug={activeSpaceSlug} />
        )}
        {/* ── Currency tab: per-space currency + BNM rates (issue #185) ── */}
        {tab === "currency" && (
          <section className="jf-card mt-6 p-6">
            <h2 className="text-lg font-semibold">{t("set.currencyTitle")}</h2>
            <p className="mt-1 max-w-xl text-sm text-slate-400">
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
              <p className="mt-1 text-xs text-slate-400">
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
                <p className="mt-2 text-sm text-slate-400">—</p>
              )}
            </div>
          </section>
        )}
      </main>
    </Shell>
  );
}
