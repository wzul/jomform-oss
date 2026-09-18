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
//
// Mobile audit (issue: /admin not mobile friendly): the five buttons measured
// 27-37px wide by 23px tall — far under the 44x44 tap-target floor — and simply
// growing them does not fit the phone topbar next to the menu button and the
// logo. So the phone topbar uses LangSelect (one native control, 44px tall)
// and the desktop topbar keeps this segmented control, raised to 44px.
const LANG_LABEL: Record<Lang, string> = {
  en: "English",
  ms: "Bahasa Melayu",
  zh: "中文",
  ta: "தமிழ்",
  ar: "العربية",
};

const LANG_SHORT: Record<Lang, string> = {
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
        <option key={l} value={l}>
          {LANG_SHORT[l]} · {LANG_LABEL[l]}
        </option>
      ))}
    </select>
  );
}