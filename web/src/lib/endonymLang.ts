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
