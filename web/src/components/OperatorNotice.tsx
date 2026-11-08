"use client";

import { useI18n } from "./i18n";
import { OPERATOR_KEY, operatorDirProps, operatorParts } from "../lib/operator";

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
//   `dir` is emitted for an RTL LOCALE ONLY (card t_d2f2e21f), from the same
//   resolved locale: the language declaration says how the passage is
//   PRONOUNCED, the direction says how its runs are LAID OUT, and the two legal
//   pages are the case where the two disagree with the document. They keep
//   `dir="ltr"` (their prose is English, PR 1201), so without this the Arabic
//   sentence inherited an LTR paragraph base and its runs came out in logical
//   order left-to-right — an Arabic reader scanning right-to-left met it back to
//   front (measured: leftmost run "يُدار", the first word; rightmost the
//   registration clause, the last). Every LTR locale emits nothing: the document
//   already declares LTR and a repeat of it is noise.
//
// RTL (see ../lib/operator.ts for the measured detail)
//   The registration clause is wrapped in <bdi dir="ltr">. The provider sets
//   `dir="rtl"` on <html> for Arabic, and under it the raw sentence renders the
//   parenthesis pair INVERTED — visually `.(PC0012291-H` — with the
//   sentence-final period stranded at the start of the visual line. Measured in
//   Chromium against this app's own served pages, not assumed. That isolation is
//   needed in BOTH bases, so it stays whatever the paragraph direction is.
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
    // text that is already the document's own language). `operatorDirProps`
    // adds the base direction for an RTL locale, and nothing for the rest.
    <p className={className} lang={lang === "en" ? undefined : lang} {...operatorDirProps(lang)}>
      {lead.trim()}
      {/* THE CLAUSE IS ATOMIC AND SHRINKABLE (card t_68456ead)
          `whitespace-nowrap` alone made this an unbreakable run as wide as its
          glyphs: 347.6px at a 24px browser root in the landing footer's 248px
          paragraph (383.6px right edge in a 320px viewport — the 64px page pan),
          and 441.5px on /legal/privacy. `inline-block` + `max-w-full` keeps the
          two behaviours the two cards need at once:
            * the line breaker treats the box as ONE atomic unit, so the hyphen
              inside PC0012291-H is never a break opportunity the surrounding
              paragraph can use — the clause moves to the next line whole, which
              is what card t_bdcee56f measured (`H).` stranded at the 692px
              paragraph) and what a11y-docs-lang.spec.ts pins;
            * the box shrink-to-fits, so when the clause is WIDER than the
              paragraph (raised text) it takes the paragraph's width and wraps
              its own words inside it — instead of overflowing the page.
          `[line-height:normal]` keeps the inline-block's own box exactly the
          height of the text it holds (17px on the legal page, matching the
          plain inline box before this change), so the surrounding line box and
          the paragraph are pixel-identical at 100% text size — measured: same
          <p> height, same <bdi> top and left, all four a11y viewports (320,
          390, 740, 1280) still one line box.
          `[overflow-wrap:anywhere]` is the last resort for a single word wider
          than the paragraph at an extreme text size; the wrap it enables only
          opens when the clause cannot fit. */}
      {regClause ? <> <bdi dir="ltr" className="inline-block max-w-full whitespace-normal [line-height:normal] [overflow-wrap:anywhere]">{regClause}</bdi></> : null}
    </p>
  );
}
