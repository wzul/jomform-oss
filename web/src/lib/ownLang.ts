// An element that HOLDS text in another language must declare that language
// (WCAG 3.1.2, Language of Parts) — and the declaration must be derived from the
// visitor's RESOLVED locale, never written as a literal.
//
// THE DEFECT, measured on a production build of PR #1604's tree (card
// t_4d3691b9 makes `/brand` an English-only prose surface, so the document
// declares `lang="en"` / `dir="ltr"` whatever the visitor's dashboard locale is)
// with `jf_lang=ar` and a fresh browser state:
//
//   document.documentElement.lang             → "en"
//   <img alt="شعار JomForm">   lang attribute → (none)  nearest [lang] → "en"
//   <img alt="لافتة JomForm">  lang attribute → (none)  nearest [lang] → "en"
//
// Both ALTs come from the dictionary (`brand.logoAlt` / `brand.bannerAlt`, PR
// #1602), so with an Arabic dashboard locale they are Arabic text inside a
// document declared English and a screen reader reads them with an English
// voice. Same class as the translated /docs chrome labels
// (`app/docs/docsChrome.tsx`'s `T` island).
//
// ENGLISH ADDS NOTHING. English is the document's own language on these
// surfaces, so `ownLangProps("en")` returns `{}` — a declaration that merely
// repeats the document's is noise, and for a locale the reader did not pick it
// would be a wrong one. The same rule `T` and `operatorDirProps` follow.
//
// WHY A SPREAD AT THE CALL SITE AND NOT A TERNARY: the value has to be the
// resolved locale from `useI18n()`. A literal (`lang="ar"`) is wrong on every
// other locale, and an element rendered without the spread is exactly the defect
// above. `ownLang.test.ts` executes this function and pins both /brand call
// sites; the served-DOM proof is `e2e/a11y-docs-lang.spec.ts` (the `a11y`
// gate), which judges an image's ALT attribute the same way it judges an
// element's own text.
import type { Lang } from "../components/i18nDict";

/** The `lang` an element holding text in `l` must declare — or nothing for English. */
export function ownLangProps(l: Lang): { lang?: Lang } {
  return l === "en" ? {} : { lang: l };
}
