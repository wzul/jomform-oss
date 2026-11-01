// Minimal i18n: en (default), ms, zh, ta, ar. Context + hook, no heavy lib.
"use client";

import { createContext, useCallback, useContext, useEffect, useRef, useState } from "react";
import { usePathname } from "next/navigation";

// Which surfaces are English-only prose, and therefore which document
// declaration they carry (card t_c9d23e2c). The rule and the measurement live
// in that module.
import { isEnglishOnlyProsePath } from "../lib/proseSurfaces";

// The endonyms below are written in their own language, so the element that
// holds one must declare it (WCAG 3.1.2). The decision and its measurement live
// in that module.
import { endonymLangProps, ENDONYM_LABEL } from "../lib/endonymLang";

// The dashboard language is written to the PROFILE in pick order (card
// t_04b728a9): two rapid picks must not reach the server out of order, or the
// profile keeps the OLDER one and the next load silently reverts the user.
import { orderedWriter } from "../lib/langWriteQueue";

// Issue #514: the dictionary data now lives in ./i18nDict.ts so the
// locale-completeness guard in src/lib/i18n.test.ts can import it with the
// repo's `npm run test:unit` runner — node --test executes TypeScript but
// cannot load a .tsx module, and this file must stay .tsx for the JSX below.
// The five-locale `Lang` type is defined there and re-exported here.
import { DICT, type Lang } from "./i18nDict";
export type { Lang };

export type Key = keyof typeof DICT;

type Ctx = { lang: Lang; setLang: (l: Lang) => void; t: (k: Key, params?: Record<string, string | number>) => string };

const I18nContext = createContext<Ctx>({ lang: "en", setLang: () => {}, t: (k) => k });

// Legacy per-BROWSER language key (issue #380). The dashboard language is now
// PER-PROFILE (users.lang): this key is kept only as the migration source and as
// an instant first-paint value, never as the source of truth.
const STORAGE_KEY = "jf_lang";

const isLang = (v: unknown): v is Lang => ["en", "ms", "zh", "ta", "ar"].includes(v as string);

// persistLang writes the choice to the user's PROFILE (users.lang) so it
// follows them into any browser. Best-effort: the local mirror above already
// keeps the UI responsive, and a signed-out/offline user simply keeps the
// browser value.
//
// ORDERED (card t_04b728a9): two picks made in quick succession used to produce
// two CONCURRENT POSTs, and a slow first one let the server commit the OLDER
// language last — the profile then held a choice the user had already replaced,
// which only showed up on the next load. The write is now queued through
// `orderedWriter`, so POST n+1 is not issued until POST n has settled and the
// LAST explicit choice is the one the profile keeps. The rule and its
// measurement live in `../lib/langWriteQueue.ts` (unit-pinned in
// `src/lib/langPlacement.test.ts`, executed in a browser by
// `web/e2e/profile-lang-write-order.spec.ts`).
const persistLang = orderedWriter<Lang>((l) =>
  fetch("/api/auth/profile/lang", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ lang: l }),
  }).catch(() => {
    // not signed in / offline — the local value still applies
  }),
);

export function I18nProvider({ children }: { children: React.ReactNode }) {
  const [lang, setLangState] = useState<Lang>("en");

  // Has the USER chosen a language in this mount? (card t_02338e21)
  //
  // The profile request below is issued at mount and answered from the row as
  // it was WHEN IT WAS SERVED. A pick made while it is in flight therefore
  // sends a POST that commits a NEWER value than the GET will return — and
  // applying the GET's answer then silently reverted the pick in the picker,
  // in `localStorage` and across the whole dashboard, while the server kept the
  // new language (so only a reload showed the truth). Measured on prod
  // v1.22.27: 4/4 picks inside the load window reverted.
  //
  // A ref, not state: this must neither re-render nor re-key the effect (a
  // dependency would re-run the request, replacing one stale read with another
  // issued later), and the effect has to see the CURRENT value inside its own
  // closure. Once set, the mount answer is stale by definition — the user's
  // choice is strictly newer than a read made before it — so it is discarded,
  // for the value AND for the migration path (the POST already writes the
  // profile, so a profile that still reads empty is a read that predates it).
  const chosenRef = useRef(false);

  // The route the visitor is ON, as the router sees it — not read once from
  // `window.location`. Both halves matter: the declaration below depends on it,
  // so a client-side navigation from /docs to a translated page must move the
  // declaration back (card t_c9d23e2c).
  const pathname = usePathname();

  // 1. Instant first paint from the legacy browser value, so an existing user
  //    never flashes English while the profile request is in flight.
  useEffect(() => {
    try {
      const saved = localStorage.getItem(STORAGE_KEY);
      if (isLang(saved)) setLangState(saved);
    } catch {
      // storage unavailable — stay on the default
    }
  }, []);

  // 2. The PROFILE wins. The dashboard language is per-profile, so the same
  //    user gets their language in every browser — localStorage cannot express
  //    that. When the profile has never been set ("" — e.g. a user whose
  //    preference only ever lived in this browser), migrate the local value up
  //    to the profile exactly once so nobody silently resets to English.
  useEffect(() => {
    let cancelled = false;
    fetch("/api/auth/profile")
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => {
        if (cancelled || !d) return;
        // The user picked while this request was in flight: this answer is a
        // read issued BEFORE their POST, so it is older than the choice. Applying
        // it is the t_02338e21 revert.
        if (chosenRef.current) return;
        if (isLang(d.lang)) {
          setLangState(d.lang);
          try { localStorage.setItem(STORAGE_KEY, d.lang); } catch { /* ignore */ }
          return;
        }
        // Profile unset: promote the browser value (migration path).
        let local: string | null = null;
        try { local = localStorage.getItem(STORAGE_KEY); } catch { /* ignore */ }
        if (isLang(local)) persistLang(local);
      })
      .catch(() => {
        // Not signed in / offline: the browser value stays in effect.
      });
    return () => { cancelled = true; };
  }, []);

  // The document declaration must match the CONTENT, not the visitor's
  // preference (card t_c9d23e2c, WCAG 3.1.1). The prose of /docs and /legal is
  // English-only by design, so those two trees keep an English declaration and
  // LTR whatever the dashboard locale is — measured live before this change:
  // with jf_lang=ar, /docs/embed read lang="ar"/dir="rtl" over English prose.
  // Everything else (the Arabic dashboard included) still follows `lang`, and
  // RTL is NOT dropped: it belongs to the surfaces whose text really is RTL.
  useEffect(() => {
    const prose = isEnglishOnlyProsePath(pathname);
    document.documentElement.dir = !prose && lang === "ar" ? "rtl" : "ltr";
    document.documentElement.lang = prose ? "en" : lang;
  }, [lang, pathname]);

  const setLang = useCallback((l: Lang) => {
    // Every explicit choice makes the mount-time profile read stale (see
    // `chosenRef` above), BEFORE the state write so a response landing between
    // these two lines cannot win the race.
    chosenRef.current = true;
    setLangState(l);
    // Mirror locally for the next first paint, then persist to the PROFILE —
    // the server is the source of truth so the choice follows the user.
    try { localStorage.setItem(STORAGE_KEY, l); } catch { /* ignore */ }
    persistLang(l);
  }, []);

  const t = useCallback(
    (k: Key, params?: Record<string, string | number>) => {
      const entry = DICT[k] as Record<Lang, string> | undefined;
      let s = (!entry ? k : entry[lang] || entry.en) as string;
      if (params) {
        for (const [k2, v] of Object.entries(params)) s = s.replaceAll(`{${k2}}`, String(v));
      }
      return s;
    },
    [lang],
  );

  return <I18nContext.Provider value={{ lang, setLang, t }}>{children}</I18nContext.Provider>;
}

export function useI18n() {
  return useContext(I18nContext);
}

// Small toggle for the sidebar/topbar.
//
// Mobile audit (issue: /admin not mobile friendly): the five buttons measured
// 27-37px wide by 23px tall — far under the 44x44 tap-target floor — and simply
// growing them does not fit the phone topbar next to the menu button and the
// logo. So the phone topbar uses LangSelect (one native control, 44px tall)
// and the desktop topbar keeps this segmented control, raised to 44px.
// The labels the pickers render. The STRINGS live in `lib/endonymLang.ts`
// next to the `lang` each one must declare, so the /docs guides — which name
// the same languages in prose, and are SERVER components — can render them too
// (a `"use client"` module's exports are client references on the server, not
// strings). One copy, every call site.
export const LANG_LABEL: Record<Lang, string> = ENDONYM_LABEL;

// The short labels the switchers render. Exported so the /mcp page and this
// component cannot drift: /mcp used to carry a SECOND copy of these literals in
// its own ternary chain, which is how it stayed undeclared after the shared
// components were fixed. One list, every call site.
export const LANG_SHORT: Record<Lang, string> = {
  en: "EN",
  ms: "BM",
  zh: "中文",
  ta: "தமிழ்",
  ar: "عربي",
};

export const LANGS_SHOWN: readonly Lang[] = ["en", "ms", "zh", "ta", "ar"];

export function LangToggle() {
  const { lang, setLang } = useI18n();
  return (
    <div className="flex overflow-hidden rounded-md border border-[var(--jf-border)] text-xs">
      {LANGS_SHOWN.map((l) => (
        <button
          key={l}
          onClick={() => setLang(l)}
          // The endonym is written in its own language; 中文 on an English page
          // is a language of parts, not English text (WCAG 3.1.2).
          {...endonymLangProps(l)}
          title={LANG_LABEL[l]}
          className={`flex min-h-11 min-w-11 items-center justify-center px-2 text-xs font-medium transition-colors ${
            lang === l ? "bg-[var(--jf-primary)] text-white" : "bg-white text-[var(--jf-muted)]"
          }`}
        >
          {LANG_SHORT[l]}
        </button>
      ))}
    </div>
  );
}

// Phone-width language control. A native <select> is a single 44px-tall target
// (the platform's own picker on touch), which the five-button row cannot be on a
// 360px topbar. The accessible name describes the control ("Language"); the
// current value is read from the selected option, so the label stays static.
export function LangSelect() {
  const { lang, setLang, t } = useI18n();
  return (
    <select
      aria-label={t("common.language")}
      value={lang}
      onChange={(e) => setLang(e.target.value as Lang)}
      className="min-h-11 max-w-[9.5rem] rounded-md border border-[var(--jf-border)] bg-white px-2 text-sm text-[var(--jf-text)]"
    >
      {LANGS_SHOWN.map((l) => (
        // An <option> holds text only, so the declaration belongs on the option
        // element itself — the same rule as the buttons above.
        <option key={l} value={l} {...endonymLangProps(l)}>
          {LANG_SHORT[l]} · {LANG_LABEL[l]}
        </option>
      ))}
    </select>
  );
}