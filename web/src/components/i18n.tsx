// Minimal i18n: en (default) + ms. Context + hook, no heavy lib.
"use client";

import { createContext, useCallback, useContext, useEffect, useState } from "react";

export type Lang = "en" | "ms";

const DICT = {
  // nav / shell
  "nav.dashboard": { en: "Dashboard", ms: "Papan Pemuka" },
  "nav.forms": { en: "Forms", icon: undefined, ms: "Borang" },
  "nav.settings": { en: "Settings", ms: "Tetapan" },
  "nav.signout": { en: "Sign out", ms: "Log keluar" },
  // login
  "login.welcome": { en: "Welcome back", ms: "Selamat kembali" },
  "login.registerTitle": { en: "Create your JomForm account", ms: "Cipta akaun JomForm anda" },
  "login.tabLogin": { en: "Log in", ms: "Log Masuk" },
  "login.tabRegister": { en: "Sign up", ms: "Daftar" },
  "login.email": { en: "Email", ms: "Emel" },
  "login.password": { en: "Password", ms: "Kata laluan" },
  "login.pwRegister": { en: "Password (min. 10 chars)", ms: "Kata laluan (min. 10 aksara)" },
  "login.submitLogin": { en: "Log in", ms: "Log Masuk" },
  "login.submitRegister": { en: "Create free account", ms: "Cipta Akaun Percuma" },
  "login.freeNote": {
    en: "Free forever — your first form in 5 minutes.",
    ms: "Percuma selamanya — borang pertama siap dalam 5 minit.",
  },
  "login.loginNote": { en: "Log in to manage your forms and sales.", ms: "Log masuk untuk urus borang & jualan anda." },
  // dashboard
  "dash.title": { en: "Dashboard", ms: "Papan Pemuka" },
  "dash.subtitle": { en: "Overview of your forms and sales", ms: "Ringkasan borang & jualan anda" },
  "dash.newForm": { en: "New form", ms: "+ Borang Baru" },
  "dash.revenue": { en: "Confirmed revenue", ms: "Hasil disahkan" },
  "dash.pending": { en: "Pending", ms: "Menunggu" },
  "dash.confirmed": { en: "Confirmed", ms: "Disahkan" },
  "dash.activeProducts": { en: "Active products", ms: "Produk aktif" },
  "dash.recentSales": { en: "Recent sales", ms: "Jualan terkini" },
  "dash.records": { en: "records", ms: "rekod" },
  "dash.ref": { en: "Reference", ms: "Rujukan" },
  "dash.buyer": { en: "Buyer", ms: "Pembeli" },
  "dash.amount": { en: "Amount", ms: "Jumlah" },
  "dash.status": { en: "Status", ms: "Status" },
  "dash.emptySales": { en: "No sales yet.", ms: "Belum ada jualan lagi." },
  // products page
  "prod.title": { en: "Products", ms: "Produk" },
  "prod.subtitle": { en: "One-time items and subscriptions you sell", ms: "Produk one-time & langganan yang anda jual" },
  "prod.add": { en: "Add product", ms: "Tambah produk" },
  "prod.name": { en: "Product name", ms: "Nama produk" },
  "prod.price": { en: "Price (RM)", ms: "Harga (RM)" },
  "prod.type": { en: "Type", ms: "Jenis" },
  "prod.oneTime": { en: "One-time", ms: "One-time" },
  "prod.subscription": { en: "Subscription", ms: "Langganan" },
  "prod.monthly": { en: "Monthly", ms: "Bulanan" },
  "prod.yearly": { en: "Yearly", ms: "Tahunan" },
  "prod.empty": { en: "No products yet — add your first above.", ms: "Belum ada produk — tambah yang pertama di atas." },
  // forms page
  "forms.title": { en: "Forms", ms: "Borang" },
  "forms.subtitle": { en: "Your published and draft forms", ms: "Borang live & draft anda" },
  "forms.new": { en: "New form", ms: "Borang Baru" },
  "forms.empty": { en: "No forms yet — create your first one.", ms: "Belum ada borang — bina yang pertama." },
  "forms.live": { en: "LIVE", ms: "LIVE" },
  "forms.draft": { en: "Draft", ms: "Draft" },
  // builder common
  "builder.publish": { en: "Publish", ms: "Publish" },
  "builder.savePub": { en: "Save & publish", ms: "Simpan & Publish" },
  "builder.saving": { en: "Saving…", ms: "Menyimpan…" },
  "builder.unsaved": { en: "unsaved", ms: "belum disimpan" },
  // settings
  "set.title": { en: "Settings", ms: "Tetapan" },
  // landing
  "land.badge": { en: "100% free · open source · CHIP payments", ms: "100% percuma · open source · bayaran CHIP" },
  "land.hero1": { en: "Sell online with", ms: "Jual secara online dengan" },
  "land.hero2": { en: "beautiful order forms.", ms: "borang tempahan cantik." },
  "land.hero3": { en: "", ms: "" },
  "land.sub": { en: "Build a form, buyers pay online, you manage sales. For Malaysian social sellers — free forever.", ms: "Bina borang, buyer bayar secara online, anda urus jualan. Untuk penjual sosial Malaysia — percuma selamanya." },
  "land.cta1": { en: "Start free →", ms: "Mula percuma →" },
  "land.cta2": { en: "How it works", ms: "Macam mana ia berfungsi" },
  "land.step1t": { en: "1. Build your form", ms: "1. Bina borang" },
  "land.step1b": { en: "Drag products, name fields, WhatsApp — one-click publish.", ms: "Tarik produk, medan nama, WhatsApp — publish satu klik." },
  "land.step2t": { en: "2. Buyer pays", ms: "2. Buyer bayar" },
  "land.step2b": { en: "Secure checkout by CHIP — card / FPX / DuitNow QR / e-wallets.", ms: "Checkout selamat oleh CHIP — kad / FPX / DuitNow QR / e-wallet." },
  "land.step3t": { en: "3. Auto-updated", ms: "3. Anda auto-update" },
  "land.step3b": { en: "Sales status updates itself, receipts sent automatically.", ms: "Status jualan naik sendiri, resit hantar automatik." },
  "land.buildForm": { en: "Build a form", ms: "Bina borang" },
  "land.login": { en: "Log in", ms: "Log masuk" },
  "land.f1": { en: "Order form in 5 minutes — drag-and-drop products, fields, images, coupons. No code.", ms: "Borang siap 5 minit — tarik produk, medan, imej, kupon. Tanpa kod." },
  "land.f2": { en: "Buyers pay online the moment they submit. Card, FPX, DuitNow QR, e-wallets.", ms: "Buyer bayar terus selepas submit. Kad, FPX, DuitNow QR, e-wallet." },
  "land.f3": { en: "AI-agent native: your ChatGPT/Claude agent controls the whole store via MCP — products, prices, sales, refunds. The most powerful order-form API in Malaysia.", ms: "AI-agent native: agent ChatGPT/Claude kau kawal seluruh kedai melalui MCP — produk, harga, jualan, refund. API borang paling power di Malaysia." },
  "land.f4": { en: "Sales analytics, CSV export, PDF receipts — free forever, no monthly fee.", ms: "Analitik jualan, eksport CSV, resit PDF — percuma selamanya, tiada yuran bulanan." },
  "land.mcptitle": { en: "Control your store with plain language", ms: "Kawal kedai guna bahasa biasa" },
  "land.mcpbody": { en: 'JomForm speaks MCP (Model Context Protocol). Connect your AI agent once, then: \"add product RM25\", \"how many sales today?\", \"refund order JF-xxxx\" — all without opening a dashboard.', ms: 'JomForm cakap MCP (Model Context Protocol). Sambung agent AI sekali, lepas tu: \"tambah produk RM20\", \"berapa sales hari ni?\", \"refund order JF-xxxx\" — semua tanpa buka dashboard.' },
  "land.mcpsnippet": { en: "Connect at mcp.jomform.com → OAuth 2.1 (one-click) → your agent, your store.", ms: "Sambung di mcp.jomform.com → OAuth 2.1 (satu klik) → agent kau, kedai kau." },
  "land.footer": { en: "JomForm — open source (Apache-2.0). Made in Malaysia. Payments by", ms: "JomForm — open source (Apache-2.0). Buatan Malaysia. Bayaran oleh" },
} as const;

export type Key = keyof typeof DICT;

type Ctx = { lang: Lang; setLang: (l: Lang) => void; t: (k: Key) => string };

const I18nContext = createContext<Ctx>({ lang: "en", setLang: () => {}, t: (k) => k });

const STORAGE_KEY = "jf_lang";

export function I18nProvider({ children }: { children: React.ReactNode }) {
  const [lang, setLangState] = useState<Lang>("en");

  useEffect(() => {
    const saved = (localStorage.getItem(STORAGE_KEY) as Lang) || "en";
    setLangState(saved === "ms" ? "ms" : "en");
  }, []);

  const setLang = useCallback((l: Lang) => {
    setLangState(l);
    localStorage.setItem(STORAGE_KEY, l);
  }, []);

  const t = useCallback(
    (k: Key) => {
      const entry = DICT[k] as { en: string; ms: string } | undefined;
      if (!entry) return k;
      return entry[lang];
    },
    [lang],
  );

  return <I18nContext.Provider value={{ lang, setLang, t }}>{children}</I18nContext.Provider>;
}

export function useI18n() {
  return useContext(I18nContext);
}

// Small toggle for the sidebar/topbar.
export function LangToggle() {
  const { lang, setLang } = useI18n();
  return (
    <div className="flex overflow-hidden rounded-md border border-[var(--jf-border)] text-xs">
      {(["en", "ms"] as const).map((l) => (
        <button
          key={l}
          onClick={() => setLang(l)}
          className={`px-2 py-1 text-[11px] font-medium transition-colors ${
            lang === l ? "bg-[var(--jf-primary)] text-white" : "bg-white text-[var(--jf-muted)]"
          }`}
        >
          {l === "en" ? "EN" : "BM"}
        </button>
      ))}
    </div>
  );
}