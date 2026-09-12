// Minimal i18n: en (default), ms, zh, ta, ar. Context + hook, no heavy lib.
"use client";

import { createContext, useCallback, useContext, useEffect, useState } from "react";

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