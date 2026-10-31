// The platform's own operator identity — the single source of the legal
// identifiers, and the split the footer and the legal pages render from.
//
// WHY THIS IS ONE MODULE AND NOT A STRING IN EACH PAGE
//   Three surfaces must name the operator: the landing/home footer
//   (app/landing/page.tsx) and both legal pages (app/legal/privacy,
//   app/legal/terms). A privacy policy that does not say who the data
//   controller is has no way to stay correct if each page carries its own copy
//   of the registration number, so the number and the former number live here
//   ONCE and the localized sentence lives once in the dictionary
//   (`land.operator`, components/i18nDict.ts). src/lib/operatorNotice.test.ts
//   fails if a surface stops rendering from them, or if the identifiers are
//   copied anywhere else.
//
// THE REGISTRATION NUMBERS ARE NOT TRANSLATABLE
//   `WANZUL HOSTING ENTERPRISE`, `201403298654` and `PC0012291-H` are the legal
//   identifiers as registered. They are identical in every locale; only the
//   sentence around them is localized.
//
// WHY operatorParts() EXISTS (RTL)
//   The Arabic sentence is RTL and ends with a parenthesis-heavy clause
//   (`201403298654 (سابقًا PC0012291-H).`). Rendered raw under dir="rtl" the
//   bidi algorithm treats the trailing period and the parenthesis pair as
//   neutral characters, so the pair renders INVERTED — visually
//   `.(PC0012291-H` — and the sentence-final period lands at the START of the
//   visual line. Measured in Chromium on the live string, not assumed.
//   Wrapping the registration clause in `<bdi dir="ltr">` (what the shared
//   component does) pins the clause's own direction and renders it correctly.
//   So the sentence is split at the registration number here, in one pure
//   function, rather than by each surface.
//
//   The split is intentionally forgiving: a locale that somehow lost the
//   number returns the whole sentence as `lead` with an empty `regClause`
//   (i.e. it renders un-isolated, never blank, and never throws mid-render).
//   The guard test is what fails loudly on that drift — a render-time throw
//   would take the page down instead.
//
// WHY THE PASSAGE ALSO DECLARES ITS DIRECTION (card t_d2f2e21f)
//   `dir="rtl"` on <html> is the page's base direction, and it follows the
//   visitor — except on the two legal pages, whose PROSE is English and which
//   therefore keep `dir="ltr"` whatever the locale (PR 1201, see
//   proseSurfaces.ts). Their footer sentence is the one part of those pages that
//   follows the visitor, so with `jf_lang=ar` a full Arabic sentence sat inside
//   an LTR paragraph base. The bidi algorithm then places the RTL runs in
//   LOGICAL order, left to right, and an Arabic reader scanning right-to-left
//   meets the sentence BACK TO FRONT.
//   Measured on v1.21.332 (per-word client rects on /legal/privacy, 1440x900):
//   the LEFTMOST run was "يُدار" — the sentence's first word — and the
//   RIGHTMOST was the registration clause, i.e. the last thing written. Same on
//   /legal/terms; /landing was correct only because its document is already
//   `dir="rtl"`. Setting `dir="rtl"` on the passage inverted that to the correct
//   order (rightmost = "يُدار", clause at the left end, clause still reading
//   `201403298654 (سابقًا PC0012291-H).`).
//   So the passage carries its own base direction, from the SAME resolved
//   locale as its `lang` — not from the page. A pure RTL label does not need
//   this (a single Arabic run renders correctly in an LTR base; that is why the
//   translated /docs chrome is fine); a MIXED Arabic/Latin sentence does.


import type { Lang } from "../components/i18nDict";

/** The registered legal name of the operator of JomForm. Verbatim, never translated. */
export const OPERATOR_LEGAL_NAME = "WANZUL HOSTING ENTERPRISE";

/** Current business registration number (SSM). Verbatim, never translated. */
export const OPERATOR_REG_NO = "201403298654";

/** The former registration number, shown in parentheses. Verbatim, never translated. */
export const OPERATOR_FORMER_REG_NO = "PC0012291-H";

/** The dictionary key holding the localized operator sentence. */
export const OPERATOR_KEY = "land.operator" as const;

/** Locales the operator sentence must be written in (see i18nDict LANGS). */
export const OPERATOR_LOCALES: readonly Lang[] = ["en", "ms", "zh", "ta", "ar"];

/**
 * The base direction each locale is written in.
 *
 * Keyed on `Lang`, so a locale added to the dictionary without a decision here
 * is a TYPE ERROR — the same shape as `ENDONYM_LANG` in ./endonymLang.ts, and the
 * reason this is a record rather than a `lang === "ar"` test sprinkled at the
 * call site: Arabic is the RTL language TODAY, and the day a second one joins
 * the dictionary the compiler asks for it instead of the sentence silently
 * rendering back to front in the new locale.
 */
export const LOCALE_BASE_DIR: Readonly<Record<Lang, "ltr" | "rtl">> = {
  en: "ltr",
  ms: "ltr",
  zh: "ltr",
  ta: "ltr",
  ar: "rtl",
};


export type OperatorParts = {
  /** The localized sentence up to the registration number. */
  lead: string;
  /** The registration clause — `<REG_NO> (<former word> <FORMER_REG_NO>)…` — or "" if the number is absent. */
  regClause: string;
};

/**
 * Split a localized operator sentence at the current registration number, so the
 * caller can render the registration clause inside `<bdi dir="ltr">`.
 *
 * `operatorParts("… Registration No. 201403298654 (formerly PC0012291-H).")`
 *   → { lead: "… Registration No. ", regClause: "201403298654 (formerly PC0012291-H)." }
 */
export function operatorParts(text: string): OperatorParts {
  const at = text.indexOf(OPERATOR_REG_NO);
  if (at < 0) return { lead: text, regClause: "" };
  return { lead: text.slice(0, at), regClause: text.slice(at) };
}

/**
 * Direction attributes to spread onto the element that holds `l`'s sentence.
 *
 * `{ dir: "rtl" }` for an RTL locale, `{}` for every LTR one — so the English
 * (and Malay / Chinese / Tamil) sentence renders exactly as before, with no
 * `dir` attribute at all: the document it sits in already declares LTR, and a
 * declaration that merely repeats the document's is noise (the same rule the
 * `lang` declaration follows above).
 *
 * NOT `dir="auto"`: that resolves from the first strong character, which happens
 * to be Arabic here and would silently pick LTR for the first locale whose
 * sentence opens with the product name — the same silent, look-at-the-page-only
 * failure this exists to stop.
 *
 * A spread at the ONE call site rather than a ternary, so the passage cannot be
 * rendered without it (the guard in operatorNotice.test.ts requires the spread).
 */
export function operatorDirProps(l: Lang): { dir?: "rtl" } {
  return LOCALE_BASE_DIR[l] === "rtl" ? { dir: "rtl" } : {};
}

