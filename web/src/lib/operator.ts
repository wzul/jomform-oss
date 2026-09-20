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
