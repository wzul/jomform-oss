// Which surfaces are ENGLISH-ONLY PROSE, and therefore which document
// declaration they must carry (card t_c9d23e2c, WCAG 3.1.1 Language of Page).
//
// THE DEFECT. `components/i18n.tsx` applied the visitor's DASHBOARD locale to
// `document.documentElement` on every route, without regard to what the page
// actually contains:
//
//   document.documentElement.dir  = lang === "ar" ? "rtl" : "ltr";
//   document.documentElement.lang = lang;
//
// The knowledge base is English-only by design — `app/docs/docsChrome.tsx`:
// "The docs PROSE stays English: every /docs guide page is static English, and
// half-translating the knowledge base would be worse than not translating it"
// (only the /docs index's chrome LABELS are translated). So an
// Arabic-preferring visitor reading a guide got an English document DECLARED as
// Arabic and mirrored right-to-left. Measured live on jomform.com (v1.18.0,
// fresh browser state) by card t_6c4ccef4: with `jf_lang=ar`, /docs/embed read
// `lang="ar"` / `dir="rtl"` with the h1 "Embed a form on your site", every
// paragraph, and every nav label in English — the same reading on
// /legal/privacy. And `jf_lang` is a real user setting, not a test artifact:
// /profile's "Bahasa dashboard" select persists it (i18n.tsx -> persistLang).
//
// THE RULE. A document's declaration must describe the text it actually
// contains. Three trees are the English-only prose surfaces: /docs (the
// knowledge base), /legal (privacy policy, terms) and /brand (the public
// brand-assets page). Every other surface keeps the visitor's locale, RTL
// included — an Arabic DASHBOARD genuinely renders Arabic, so its `dir="rtl"`
// must not be dropped to fix this.
//
// /brand joined the list in card t_4d3691b9. It is public (linked from the docs
// and the footer) and its prose is static English — h1 "Brand assets", the
// "Logo mark" / "Downloads" / "Colors" / "Banner" / "Usage" headings, the asset
// notes, the usage list and the contact line — but it was never in this list, so
// an Arabic-preferring visitor got `lang="ar"` / `dir="rtl"` over that English
// text. Measured live on a production build with `jf_lang=ar`: the document
// declared Arabic, mirrored right-to-left, while every heading stayed English.
// That is the same WCAG 3.1.1 defect this list was introduced for, in a third
// tree. The alternative — translating the page — was rejected because its prose
// is English today and a translation is a product decision, not a declaration
// fix. If the page is ever localised, any translated part (its image ALTs, for
// instance) must declare its own language inside this English document, so the
// text is not read with an English voice (WCAG 3.1.2); that is a separate
// change, not a reason to drop /brand from this list.
//
// ONE LIST, READ BY THE PROVIDER AND PINNED BY THE GUARD. `components/i18n.tsx`
// gates the documentElement write on `isEnglishOnlyProsePath`, and
// `src/lib/docsA11y.test.ts` walks the routes ON DISK under `src/app/docs`,
// `src/app/legal` and `src/app/brand` and fails when one of them is not matched
// by this rule — so a page added later is covered without editing this file, and
// a typo'd prefix cannot silently drop a whole tree from the rule.

export const ENGLISH_ONLY_PROSE_PREFIXES = ["/docs", "/legal", "/brand"] as const;

// True when `pathname` is one of the English-only prose surfaces.
//
// Matched on a SEGMENT boundary, not a bare string prefix: `/docs/embed` and
// `/docs` are prose, `/docsomething` and `/legalise` are not (a bare
// `startsWith("/docs")` would declare a future `/docsomething` page English and
// strip its RTL). A trailing slash (`/docs/embed/`) is the same page, and a
// missing pathname (a hook that has no route to report) is NOT a prose surface:
// the caller keeps the visitor's locale rather than silently forcing English.
export function isEnglishOnlyProsePath(pathname: string | null | undefined): boolean {
  if (!pathname) return false;
  const p = pathname.length > 1 && pathname.endsWith("/") ? pathname.slice(0, -1) : pathname;
  return ENGLISH_ONLY_PROSE_PREFIXES.some((prefix) => p === prefix || p.startsWith(`${prefix}/`));
}
