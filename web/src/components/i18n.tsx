// Minimal i18n: en (default) + ms. Context + hook, no heavy lib.
"use client";

import { createContext, useCallback, useContext, useEffect, useState } from "react";

export type Lang = "en" | "ms" | "zh" | "ta" | "ar";

const DICT = {
  // nav / shell
  "nav.dashboard": { en: "Dashboard", ms: "Papan Pemuka", zh: "仪表板", ta: "டாஷ்போர்டு", ar: "لوحة التحكم" },
  "nav.forms": { en: "Forms", icon: undefined, ms: "Borang", zh: "表单", ta: "படிவங்கள்", ar: "النماذج" },
  "nav.settings": { en: "Settings", ms: "Tetapan", zh: "设置", ta: "அமைப்புகள்", ar: "الإعدادات" },
  "nav.signout": { en: "Sign out", ms: "Log keluar", zh: "退出登录", ta: "வெளியேறு", ar: "تسجيل الخروج" },
  // login
  "login.welcome": { en: "Welcome back", ms: "Selamat kembali", zh: "欢迎回来", ta: "மீண்டும் வருக", ar: "أهلاً بعودتك" },
  "login.registerTitle": { en: "Create your JomForm account", ms: "Cipta akaun JomForm anda", zh: "创建您的 JomForm 账户", ta: "உங்கள் JomForm கணக்கை உருவாக்கவும்", ar: "أنشئ حساب JomForm الخاص بك" },
  "login.tabLogin": { en: "Log in", ms: "Log Masuk", zh: "登录", ta: "உள்நுழைவு", ar: "تسجيل الدخول" },
  "login.tabRegister": { en: "Sign up", ms: "Daftar", zh: "注册", ta: "பதிவு", ar: "إنشاء حساب" },
  "login.email": { en: "Email", ms: "Emel", zh: "电子邮件", ta: "மின்னஞ்சல்", ar: "البريد الإلكتروني" },
  "login.password": { en: "Password", ms: "Kata laluan", zh: "密码", ta: "கடவுச்சொல்", ar: "كلمة المرور" },
  "login.pwRegister": { en: "Password (min. 10 chars)", ms: "Kata laluan (min. 10 aksara)", zh: "密码（至少 10 个字符）", ta: "கடவுச்சொல் (குறைந்தது 10 எழுத்துகள்)", ar: "كلمة المرور (10 أحرف على الأقل)" },
  "login.submitLogin": { en: "Log in", ms: "Log Masuk", zh: "登录", ta: "உள்நுழை", ar: "تسجيل الدخول" },
  "login.submitRegister": { en: "Create free account", ms: "Cipta Akaun Percuma", zh: "免费创建账户", ta: "இலவச கணக்கை உருவாக்கு", ar: "أنشئ حساباً مجانياً" },
  "login.freeNote": {
    en: "Free forever — your first form in 5 minutes.",
    ms: "Percuma selamanya — borang pertama siap dalam 5 minit.",
    zh: "永久免费 — 5 分钟创建您的第一个表单。",
    ta: "என்றும் இலவசம் — 5 நிமிடங்களில் உங்கள் முதல் படிவம்.",
    ar: "مجاني للأبد — نموذجك الأول في 5 دقائق.",
  },
  "login.loginNote": { en: "Log in to manage your forms and sales.", ms: "Log masuk untuk urus borang & jualan anda.", zh: "登录以管理您的表单和销售。", ta: "உங்கள் படிவங்களையும் விற்பனையையும் நிர்வகிக்க உள்நுழையவும்.", ar: "سجّل الدخول لإدارة نماذجك ومبيعاتك." },
  // dashboard
  "dash.title": { en: "Dashboard", ms: "Papan Pemuka", zh: "仪表板", ta: "டாஷ்போர்டு", ar: "لوحة التحكم" },
  "dash.subtitle": { en: "Overview of your forms and sales", ms: "Ringkasan borang & jualan anda", zh: "表单与销售概览", ta: "படிவங்கள் மற்றும் விற்பனை கண்ணோட்டம்", ar: "نظرة عامة على النماذج والمبيعات" },
  "dash.newForm": { en: "New form", ms: "+ Borang Baru", zh: "+ 新建表单", ta: "+ புதிய படிவம்", ar: "+ نموذج جديد" },
  "dash.revenue": { en: "Confirmed revenue", ms: "Hasil disahkan", zh: "已确认收入", ta: "உறுதிப்படுத்தப்பட்ட வருவாய்", ar: "الإيرادات المؤكدة" },
  "dash.pending": { en: "Pending", ms: "Menunggu", zh: "待定", ta: "நிலுவையில்", ar: "قيد الانتظار" },
  "dash.confirmed": { en: "Confirmed", ms: "Disahkan", zh: "已确认", ta: "உறுதிப்படுத்தப்பட்டது", ar: "مؤكد" },
  "dash.activeProducts": { en: "Active products", ms: "Produk aktif", zh: "在售商品", ta: "செயலில் உள்ள தயாரிப்புகள்", ar: "المنتجات النشطة" },
  "dash.recentSales": { en: "Recent sales", ms: "Jualan terkini", zh: "最新销售", ta: "சமீபத்திய விற்பனை", ar: "أحدث المبيعات" },
  "dash.records": { en: "records", ms: "rekod", zh: "条记录", ta: "பதிவுகள்", ar: "سجلات" },
  "dash.ref": { en: "Reference", ms: "Rujukan", zh: "参考号", ta: "குறிப்பு", ar: "المرجع" },
  "dash.buyer": { en: "Buyer", ms: "Pembeli", zh: "买家", ta: "வாங்குபவர்", ar: "المشتري" },
  "dash.amount": { en: "Amount", ms: "Jumlah", zh: "金额", ta: "தொகை", ar: "المبلغ" },
  "dash.status": { en: "Status", ms: "Status", zh: "状态", ta: "நிலை", ar: "الحالة" },
  "dash.emptySales": { en: "No sales yet.", ms: "Belum ada jualan lagi.", zh: "暂无销售。", ta: "இன்னும் விற்பனை இல்லை.", ar: "لا توجد مبيعات بعد." },
  // products page
  "prod.title": { en: "Products", ms: "Produk", zh: "商品", ta: "தயாரிப்புகள்", ar: "المنتجات" },
  "prod.subtitle": { en: "One-time items and subscriptions you sell", ms: "Produk one-time & langganan yang anda jual", zh: "管理您销售的商品", ta: "விற்கும் தயாரிப்புகளை நிர்வகிக்கவும்", ar: "إدارة المنتجات التي تبيعها" },
  "prod.add": { en: "Add product", ms: "Tambah produk", zh: "+ 添加商品", ta: "+ சேர்க்க", ar: "+ إضافة منتج" },
  "prod.name": { en: "Product name", ms: "Nama produk", zh: "名称", ta: "பெயர்", ar: "الاسم" },
  "prod.price": { en: "Price (RM)", ms: "Harga (RM)", zh: "价格", ta: "விலை", ar: "السعر" },
  "prod.type": { en: "Type", ms: "Jenis", zh: "类型", ta: "வகை", ar: "النوع" },
  "prod.oneTime": { en: "One-time", ms: "One-time", zh: "一次性", ta: "ஒரு முறை", ar: "لمرة واحدة" },
  "prod.subscription": { en: "Subscription", ms: "Langganan", zh: "订阅", ta: "சந்தா", ar: "اشتراك" },
  "prod.monthly": { en: "Monthly", ms: "Bulanan", zh: "每月", ta: "மாதந்தோறும்", ar: "شهرياً" },
  "prod.yearly": { en: "Yearly", ms: "Tahunan", zh: "每年", ta: "ஆண்டுதோறும்", ar: "سنوياً" },
  "prod.empty": { en: "No products yet — add your first above.", ms: "Belum ada produk — tambah yang pertama di atas.", zh: "还没有商品。", ta: "இன்னும் தயாரிப்புகள் இல்லை.", ar: "لا توجد منتجات بعد." },
  // forms page
  "forms.title": { en: "Forms", ms: "Borang", zh: "表单", ta: "படிவங்கள்", ar: "النماذج" },
  "forms.subtitle": { en: "Your published and draft forms", ms: "Borang live & draft anda", zh: "您发布的订单表单", ta: "வெளியிடப்பட்ட ஆர்டர் படிவங்கள்", ar: "نماذج الطلبات المنشورة" },
  "forms.new": { en: "New form", ms: "Borang Baru", zh: "+ 新表单", ta: "+ புதிய படிவம்", ar: "+ نموذج جديد" },
  "forms.empty": { en: "No forms yet — create your first one.", ms: "Belum ada borang — bina yang pertama.", zh: "还没有表单。", ta: "இன்னும் படிவங்கள் இல்லை.", ar: "لا توجد نماذج بعد." },
  "forms.live": { en: "LIVE", ms: "LIVE", zh: "已上线", ta: "நேரலையில்", ar: "منشور" },
  "forms.draft": { en: "Draft", ms: "Draft", zh: "草稿", ta: "வரைவு", ar: "مسودة" },
  // builder common
  "builder.publish": { en: "Publish", ms: "Publish", zh: "发布", ta: "வெளியிடு", ar: "نشر" },
  "builder.savePub": { en: "Save & publish", ms: "Simpan & Publish", zh: "保存并发布", ta: "சேமித்து வெளியிடு", ar: "حفظ ونشر" },
  "builder.saving": { en: "Saving…", ms: "Menyimpan…", zh: "保存中…", ta: "சேமிக்கிறது…", ar: "جارٍ الحفظ…" },
  "builder.unsaved": { en: "unsaved", ms: "belum disimpan", zh: "未保存", ta: "சேமிக்கப்படவில்லை", ar: "غير محفوظ" },
  // settings
  "set.title": { en: "Settings", ms: "Tetapan", zh: "设置", ta: "அமைப்புகள்", ar: "الإعدادات" },
  // landing
  "land.badge": { en: "100% free · open source · CHIP payments", ms: "100% percuma · open source · bayaran CHIP", zh: "100% 免费 · 开源 · 代理原生支付", ta: "100% இலவசம் · திறந்த மூல · முகவர் சார்ந்த பணம்", ar: "مجاني 100% · مفتوح المصدر · مدفوعات ذكية" },
  "land.hero1": { en: "Sell online with", ms: "Jual secara online dengan", zh: "5 分钟建立订单表单", ta: "5 நிமிடங்களில் ஆர்டர் படிவம்", ar: "أنشئ نموذج طلب في 5 دقائق" },
  "land.hero2": { en: "beautiful order forms.", ms: "borang tempahan cantik.", zh: "通过 CHIP 收款", ta: "CHIP மூலம் பணம் வசூலிக்கவும்", ar: "اقبل المدفوعات عبر CHIP" },
  "land.hero3": { en: "", ms: "", zh: "让 AI 代理管理您的商店", ta: "AI முகவர் உங்கள் கடையை நிர்வகிக்கட்டும்", ar: "دع الوكيل الذكي يدير متجرك" },
  "land.sub": { en: "Build a form, buyers pay online, you manage sales. For Malaysian social sellers — free forever.", ms: "Bina borang, buyer bayar secara online, anda urus jualan. Untuk penjual sosial Malaysia — percuma selamanya.", zh: "为马来西亚社交卖家打造的表单、支付与销售管理。", ta: "மலேசிய சமூக விற்பனையாளர்களுக்கான படிவங்கள், பணம், விற்பனை.", ar: "نماذج ومدفوعات ومبيعات للبائعين على وسائل التواصل في ماليزيا." },
  "land.cta1": { en: "Start free →", ms: "Mula percuma →", zh: "免费开始", ta: "இலவசமாக தொடங்கு", ar: "ابدأ مجاناً" },
  "land.cta2": { en: "How it works", ms: "Macam mana ia berfungsi", zh: "查看演示", ta: "டெமோ பார்க்க", ar: "شاهد العرض" },
  "land.step1t": { en: "1. Build your form", ms: "1. Bina borang", zh: "构建", ta: "உருவாக்கு", ar: "ابنِ" },
  "land.step1b": { en: "Drag products, name fields, WhatsApp — one-click publish.", ms: "Tarik produk, medan nama, WhatsApp — publish satu klik.", zh: "用拖放式构建器创建表单。", ta: "இழுத்து விடும் கருவியால் படிவம் உருவாக்கவும்.", ar: "أنشئ نموذجك بمحرر السحب والإفلات." },
  "land.step2t": { en: "2. Buyer pays", ms: "2. Buyer bayar", zh: "收款", ta: "பணம் பெறு", ar: "اقبض" },
  "land.step2b": { en: "Secure checkout by CHIP — card / FPX / DuitNow QR / e-wallets.", ms: "Checkout selamat oleh CHIP — kad / FPX / DuitNow QR / e-wallet.", zh: "由 CHIP 提供的安全结账 — 卡 / FPX / DuitNow QR / 电子钱包。", ta: "CHIP பாதுகாப்பான செக்கௌட் — கார்டு / FPX / DuitNow QR / மின் பணப்பை.", ar: "دفع آمن عبر CHIP — بطاقة / FPX / DuitNow QR / محافظ." },
  "land.step3t": { en: "3. Auto-updated", ms: "3. Anda auto-update", zh: "管理", ta: "நிர்வகி", ar: "أدر" },
  "land.step3b": { en: "Sales status updates itself, receipts sent automatically.", ms: "Status jualan naik sendiri, resit hantar automatik.", zh: "跟踪销售、发送收据、分析销售数据。", ta: "விற்பனையைக் கண்காணி, ரசீது அனுப்பி, வளரவும்.", ar: "تتبع المبيعات، أرسل الإيصالات، وحلل." },
  "land.buildForm": { en: "Build a form", ms: "Bina borang", zh: "+ 创建表单", ta: "+ படிவம் உருவாக்கு", ar: "+ أنشئ نموذجاً" },
  "land.login": { en: "Log in", ms: "Log masuk", zh: "登录", ta: "உள்நுழை", ar: "تسجيل الدخول" },
  "land.f1": { en: "Order form in 5 minutes — drag-and-drop products, fields, images, coupons. No code.", ms: "Borang siap 5 minit — tarik produk, medan, imej, kupon. Tanpa kod.", zh: "拖放式表单构建器", ta: "இழுத்து விடும் படிவவாக்கி", ar: "منشئ نماذج بالسحب والإفلات" },
  "land.f2": { en: "Buyers pay online the moment they submit. Card, FPX, DuitNow QR, e-wallets.", ms: "Buyer bayar terus selepas submit. Kad, FPX, DuitNow QR, e-wallet.", zh: "CHIP 支付：卡 / FPX / DuitNow QR / 电子钱包", ta: "CHIP பணம்: கார்டு / FPX / DuitNow QR / மின்னணு பணப்பை", ar: "مدفوعات CHIP: بطاقة / FPX / DuitNow QR / محافظ" },
  "land.f3": { en: "AI-agent native: your ChatGPT/Claude agent controls the whole store via MCP — products, prices, sales, refunds. The most powerful order-form API in Malaysia.", ms: "AI-agent native: agent ChatGPT/Claude kau kawal seluruh kedai melalui MCP — produk, harga, jualan, refund. API borang paling power di Malaysia.", zh: "MCP 原生：AI 代理连接您的商店", ta: "MCP செயல்படும்: AI முகவர் கடையை இணைக்கும்", ar: "جاهز لـ MCP: وكلاء الذكاء يتصلون بمتجرك" },
  "land.f4": { en: "Sales analytics, CSV export, PDF receipts — free forever, no monthly fee.", ms: "Analitik jualan, eksport CSV, resit PDF — percuma selamanya, tiada yuran bulanan.", zh: "分析与 CSV 导出", ta: "பகுப்பாய்வு + CSV ஏற்றுமதி", ar: "تحليلات وتصدير CSV" },
  "land.mcptitle": { en: "Control your store with plain language", ms: "Kawal kedai guna bahasa biasa", zh: "您的代理，您的商店", ta: "உங்கள் முகவர், உங்கள் கடை", ar: "وكيلك، متجرك" },
  "land.mcpbody": { en: 'JomForm speaks MCP (Model Context Protocol). Connect your AI agent once, then: \"add product RM25\", \"how many sales today?\", \"refund order JF-xxxx\" — all without opening a dashboard.', ms: 'JomForm cakap MCP (Model Context Protocol). Sambung agent AI sekali, lepas tu: \"tambah produk RM20\", \"berapa sales hari ni?\", \"refund order JF-xxxx\" — semua tanpa buka dashboard.', zh: 'JomForm 支持 MCP。连接一次 AI 代理：添加商品、查销售、退款——无需仪表板。', ta: 'JomForm MCP பேசுகிறது — டாஷ்போர்டு இல்லாமல் கடை நிர்வகி.', ar: 'JomForm يتحدث MCP — أدر متجرك دون لوحة تحكم.' },
  "land.mcpsnippet": { en: "Connect at mcp.jomform.com → OAuth 2.1 (one-click) → your agent, your store.", ms: "Sambung di mcp.jomform.com → OAuth 2.1 (satu klik) → agent kau, kedai kau.", zh: "在 mcp.jomform.com 连接 → OAuth 2.1（一键）→ 您的代理，您的商店。", ta: "mcp.jomform.com இல் இணைக்கவும் → OAuth 2.1 (ஒரே கிளிக்) → உங்கள் முகவர், உங்கள் கடை.", ar: "اتصل عبر mcp.jomform.com → OAuth 2.1 (بنقرة واحدة) → وكيلك، متجرك." },
  "land.footer": { en: "JomForm — open source (Apache-2.0). Made in Malaysia. Payments by", ms: "JomForm — open source (Apache-2.0). Buatan Malaysia. Bayaran oleh", zh: "支付由", ta: "பணம் அளிப்பது", ar: "المدفوعات بواسطة" },
} as const;

export type Key = keyof typeof DICT;

type Ctx = { lang: Lang; setLang: (l: Lang) => void; t: (k: Key) => string };

const I18nContext = createContext<Ctx>({ lang: "en", setLang: () => {}, t: (k) => k });

const STORAGE_KEY = "jf_lang";

export function I18nProvider({ children }: { children: React.ReactNode }) {
  const [lang, setLangState] = useState<Lang>("en");

  useEffect(() => {
    const saved = (localStorage.getItem(STORAGE_KEY) as Lang) || "en";
    setLangState((["en", "ms", "zh", "ta", "ar"].includes(saved) ? saved : "en") as Lang);
  }, []);

  useEffect(() => {
    document.documentElement.dir = lang === "ar" ? "rtl" : "ltr";
  }, [lang]);

  const setLang = useCallback((l: Lang) => {
    setLangState(l);
    localStorage.setItem(STORAGE_KEY, l);
  }, []);

  const t = useCallback(
    (k: Key) => {
      const entry = DICT[k] as Record<Lang, string> | undefined;
      if (!entry) return k;
      return entry[lang] || entry.en;
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
      {(["en", "ms", "zh", "ta", "ar"] as const).map((l) => (
        <button
          key={l}
          onClick={() => setLang(l)}
          title={l === "en" ? "English" : l === "ms" ? "Bahasa Melayu" : l === "zh" ? "中文" : l === "ta" ? "தமிழ்" : "العربية"}
          className={`px-1.5 py-1 text-[11px] font-medium transition-colors ${
            lang === l ? "bg-[var(--jf-primary)] text-white" : "bg-white text-[var(--jf-muted)]"
          }`}
        >
          {l === "en" ? "EN" : l === "ms" ? "BM" : l === "zh" ? "中文" : l === "ta" ? "தமிழ்" : "عربي"}
        </button>
      ))}
    </div>
  );
}