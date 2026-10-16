"use client";

// The /docs index is a static server-rendered page: every feature summary, link
// and heading is in the HTML a crawler reads. Only the CHROME is translated, and
// this is the single component that does it — a client island rendering one
// dictionary label.
//
// Why not translate the whole page (like the dashboard pages do)? They are client
// components with no content in the HTML. This page is in the sitemap and is the
// entry point /llms.txt and every "?" link send people and agents to, so its text
// has to survive without JavaScript. Labels are the only part that needs the
// runtime language, so they are the only part that hydrates.
//
// The docs PROSE stays English: every /docs guide page is static English (see the
// note in customize/page.tsx), and half-translating the knowledge base would be
// worse than not translating it.
//
// WHICH IS WHY A TRANSLATED LABEL HAS TO DECLARE ITS OWN LANGUAGE (WCAG 3.1.2,
// Language of Parts — card t_0e2d71f4). The document this island sits in
// declares ENGLISH: `components/i18n.tsx` pins `documentElement.lang` to "en" on
// every English-only prose route whatever the visitor's dashboard locale is
// (card t_c9d23e2c, WCAG 3.1.1). A label in another language is therefore a
// language CHANGE inside that document, and if nothing declares it a screen
// reader pronounces "وثائق JomForm" with an English voice. Measured live on
// prod v1.21.119 with `jf_lang=ar`: `/docs` read `lang="en"` and every one of
// its Arabic chrome labels carried no `lang` of its own.
//
// `lang` (from the context) is one of the five dashboard locales, and `t()`
// falls back per KEY — the dictionary-completeness guard (`src/lib/i18n.test.ts`)
// is what makes every key carry all five, so a non-English `lang` always means
// the string really is in that language. English is the document's own
// language, so an English label adds nothing at all.
//
// `dir` is deliberately NOT emitted: the label is inline in an LTR line and the
// bidi algorithm already resolves an Arabic run right-to-left inside it. Adding
// `dir="rtl"` would re-order the MIXED labels — "وثائق JomForm" would render as
// "JomForm وثائق" — which is a visual change to chrome this card does not ask
// for and nothing about 3.1.2 requires.
import { useI18n, type Key } from "../../components/i18n";

export function T({ k }: { k: Key }) {
  const { lang, t } = useI18n();
  const label = t(k);
  if (lang === "en") return <>{label}</>;
  return <span lang={lang}>{label}</span>;
}
