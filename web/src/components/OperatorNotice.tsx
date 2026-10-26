"use client";

import { useI18n } from "./i18n";
import { OPERATOR_KEY, operatorParts } from "../lib/operator";

// The operator block: "JomForm is operated by WANZUL HOSTING ENTERPRISE —
// Registration No. 201403298654 (formerly PC0012291-H)."
//
// RENDERED ON THREE SURFACES, FROM ONE SOURCE
//   * the landing / logged-out home footer (app/landing/page.tsx), and
//   * both legal pages (app/legal/privacy, app/legal/terms),
//   all of which render THIS component. The sentence exists once — the
//   `land.operator` entry in components/i18nDict.ts, five locales — and the
//   legal identifiers exist once, in ../lib/operator.ts. src/lib/operatorNotice.test.ts
//   fails if any of the three stops rendering from here.
//
// WHY IT READS THE LOCALE FROM useI18n() AND NOT FROM A PROP
//   The locale belongs to the visitor (`jf_lang` in localStorage), so only the
//   client can know it. A `lang` prop has to be supplied by the caller, and the
//   two legal pages are static server components with nothing to supply it —
//   they would print the English sentence for every visitor, permanently, and
//   the other four locales would never appear on those pages. Reading the
//   shared context makes all three surfaces follow the visitor's language like
//   every other translated string in this app: the server render is English and
//   the provider swaps it on mount.
//
// THE SENTENCE DECLARES ITS OWN LANGUAGE (WCAG 3.1.2, card t_b13ee0cc)
//   Because it follows the visitor, this block is a language CHANGE on two of
//   its three surfaces: /legal/privacy and /legal/terms declare `lang="en"`
//   (PR 1201 pins their prose to English, correctly — components/i18n.tsx), so
//   with `jf_lang=ar` an English-declared page carries a full Arabic sentence.
//   Measured on a production build with `jf_lang=ar`: the <p> below and the
//   <bdi> inside it were the only two elements on each page holding Arabic, and
//   neither declared a language — a screen reader pronounces "يُدار JomForm …"
//   with an English voice. So the passage declares the language it is written
//   in, taken from the resolved locale.
//
//   English declares nothing: it is the document's own language on all three
//   surfaces (the landing page's document declaration also follows the visitor,
//   so an English visitor gets `lang="en"` there too), and a declaration that
//   merely repeats the document's is noise the default/SSR/crawler state would
//   carry. The landing case is marked the same way as the legal pages
//   deliberately: one rule, one source, nothing to keep in step.
//
//   `dir` is NOT emitted here: this is a language declaration, not a direction
//   override — direction is handled where it actually breaks, in the RTL note
//   below.
//
// RTL (see ../lib/operator.ts for the measured detail)
//   The registration clause is wrapped in <bdi dir="ltr">. The provider sets
//   `dir="rtl"` on <html> for Arabic, and under it the raw sentence renders the
//   parenthesis pair INVERTED — visually `.(PC0012291-H` — with the
//   sentence-final period stranded at the start of the visual line. Measured in
//   Chromium against this app's own served pages, not assumed.
//
// WHY .trim() ON `lead`
//   `t()` falls back to the English string for a locale that is missing or
//   blank, so an incomplete dictionary entry degrades instead of throwing. The
//   trim only drops the separator whitespace the sentence was written with, so
//   a locale without a trailing space before the number does not render a
//   doubled gap under `dir="rtl"`.
export default function OperatorNotice({ className }: { className?: string }) {
  const { lang, t } = useI18n();
  const { lead, regClause } = operatorParts(t(OPERATOR_KEY));
  return (
    // `lang` on the element that HOLDS the passage, bound to the RESOLVED
    // locale — never a literal (which would freeze one language onto all five)
    // and never `lang={lang}` unconditionally (which would declare English over
    // text that is already the document's own language).
    <p className={className} lang={lang === "en" ? undefined : lang}>
      {lead.trim()}
      {regClause ? <> <bdi dir="ltr">{regClause}</bdi></> : null}
    </p>
  );
}
