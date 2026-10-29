// A language named in its OWN language, as the /docs prose names it.
//
// THE DEFECT THIS CLOSES (card t_23c6895c, WCAG 3.1.2 Language of Parts). Two
// guides name the supported languages inside an English sentence — /docs/chat
// ("It speaks the language you write in: English, Bahasa Melayu, 中文, தமிழ் or
// العربية") and /docs/customize ("The supported languages are …") — and both
// carried those names as LITERALS in JSX. Measured live on prod v1.21.333 with a
// fresh browser state, on a document that correctly declares `lang="en"`: 3
// elements per page held CJK / Tamil / Arabic in their own text and every one of
// them resolved, through the nearest `lang`, to that document's "en" — so 中文 was
// read with an English voice. The /docs CHROME labels had already been fixed
// (docsChrome.tsx); literals in prose cannot be reached by a component fix, which
// is why they were still there.
//
// WHY ONE COMPONENT AND NOT A SPAN AT EACH SITE. A hand-written
// `<span lang="zh">中文</span>` at a call site keeps the literal, and the literal
// is what drifted here. This element takes only the LOCALE, renders the name from
// the one shared map (`lib/endonymLang.ts`), and puts the declaration on the
// element that holds the text — so a site cannot name a language without
// declaring it, and the name cannot differ from the picker's label.
//
// NO DECLARATION FOR A LATIN NAME. `English` and `Bahasa Melayu` are written in
// the same script as the English page around them, so they render bare: a `lang`
// there would restate the document's own language, not announce a change. That is
// the rule `ENDONYM_LANG` already encodes for the switcher.
//
// Server component on purpose: both call sites are server-rendered guide pages,
// and the map it reads lives in a plain module for the same reason (a value from
// a `"use client"` module is a client reference on the server).
import { ENDONYM_LABEL, endonymLangProps } from "../lib/endonymLang";
import type { Lang } from "./i18nDict";

export function Endonym({ l }: { l: Lang }) {
  const props = endonymLangProps(l);
  const name = ENDONYM_LABEL[l];
  return props.lang ? <span {...props}>{name}</span> : <>{name}</>;
}
