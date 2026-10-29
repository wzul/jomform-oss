// The language switcher names each locale in that locale's OWN language
// (中文, தமிழ், عربي), so the name is a language-of-paragraphs problem: the
// element that holds it must declare which language it is written in, or a
// screen reader pronounces it with the voice of the surrounding page
// (WCAG 3.1.2).
//
// THE DEFECT, measured live on jomform.com/mcp (prod v1.21.190, revision
// 645f31f7, fresh browser state): with `jf_lang=en` the five-button switcher
// rendered three `<button>`s whose own text is 中文 / தமிழ் / عربي, NONE of them
// carrying a `lang`, and `el.closest("[lang]").getAttribute("lang")` was "en"
// for all three — so 中文 was read with an English voice. With `jf_lang=ar` the
// same three buttons resolved to "ar" instead: the same fault, in the other
// direction. The strings live in one place (`LANG_SHORT`) and are copied into a
// second (`app/mcp/mcp-client.tsx` had its own ternary chain), so the
// declaration lives here, once, for every call site.
//
// WHICH ENDONYMS DECLARE A LANGUAGE. Those written in a script the surrounding
// page is not in: 中文 → zh, தமிழ் → ta, عربي → ar. `EN` and `BM` are Latin
// abbreviations of English and Malay — the same scripts the documents that
// render this switcher are written in — so they stay bare, exactly as the /docs
// island leaves its English label bare (app/docs/docsChrome.tsx). Declaring
// `lang="en"` under an Arabic document would be a second declaration of the
// default, not a language of parts.
//
// THE SAME RULE, ON THE /docs PROSE (card t_23c6895c). Two guide pages NAME the
// supported languages in an English sentence — "It speaks the language you write
// in: English, Bahasa Melayu, 中文, தமிழ் or العربية" (/docs/chat) and "The
// supported languages are …" (/docs/customize) — and those names are in another
// language, inside a document that declares `en`. Measured live on prod
// v1.21.333, fresh browser state: 3 elements per page held CJK / Tamil / Arabic
// in their OWN text and every one resolved, through the nearest `lang`, to the
// document's "en". The chrome labels were fixed on this surface already
// (docsChrome.tsx); the prose bodies were not, because they carried the names as
// LITERALS in JSX, which is how a fix at the component cannot reach them.
//
// So the names live here, once, and `components/Endonym.tsx` renders one:
//
//     <Endonym l="zh" />   →   <span lang="zh">中文</span>
//     <Endonym l="en" />   →   English            (no declaration: same script)
//
// `ENDONYM_LABEL` is the map the /profile and /settings pickers render too
// (`components/i18n.tsx` re-exports it as `LANG_LABEL`) — the guides and the
// pickers cannot drift apart. It lives in this plain module rather than in the
// client component because the guides are SERVER components: a value imported
// from a `"use client"` module is a client reference on the server, not the
// string. The guard for the prose sites is `docsProseEndonym.test.ts` and the
// served-DOM proof is `e2e/a11y-docs-lang.spec.ts`.
//
// The map is keyed on `Lang`, so adding a locale to the dictionary without
// deciding its endonym language is a type error, and `endonymLang.test.ts`
// pins the value of every entry.

import type { Lang } from "../components/i18nDict";

/** The BCP 47 tag an endonym is written in, or null when it needs none. */
export const ENDONYM_LANG: Readonly<Record<Lang, string | null>> = {
  en: null,
  ms: null,
  zh: "zh",
  ta: "ta",
  ar: "ar",
};

/**
 * The endonym as a language names ITSELF — the full name, in its own script.
 *
 * Two surfaces render these strings and only these: the pickers as a label
 * (`components/i18n.tsx` exports this same map as `LANG_LABEL`), and the /docs
 * prose, which names the supported languages inside English sentences and must
 * therefore declare each name's own language (see the header). The switcher's
 * short labels (EN / BM / 中文 / தமிழ் / عربي) are a different, deliberately
 * abbreviated list — `LANG_SHORT`, where it belongs.
 */
export const ENDONYM_LABEL: Readonly<Record<Lang, string>> = {
  en: "English",
  ms: "Bahasa Melayu",
  zh: "中文",
  ta: "தமிழ்",
  ar: "العربية",
};

/**
 * Attributes to spread onto the element that renders `l`'s endonym.
 *
 * Empty for a Latin endonym (nothing to declare); `{ lang }` otherwise. A spread
 * rather than a ternary at each call site so no site can render the label
 * without it: the guard in `endonymLang.test.ts` requires every render site to
 * spread THIS function.
 */
export function endonymLangProps(l: Lang): { lang?: string } {
  const tag = ENDONYM_LANG[l];
  return tag ? { lang: tag } : {};
}
