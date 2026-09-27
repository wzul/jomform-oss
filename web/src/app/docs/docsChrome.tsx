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
import { useI18n, type Key } from "../../components/i18n";

export function T({ k }: { k: Key }) {
  const { t } = useI18n();
  return <>{t(k)}</>;
}
