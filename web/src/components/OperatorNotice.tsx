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
  const { t } = useI18n();
  const { lead, regClause } = operatorParts(t(OPERATOR_KEY));
  return (
    <p className={className}>
      {lead.trim()}
      {regClause ? <> <bdi dir="ltr">{regClause}</bdi></> : null}
    </p>
  );
}
