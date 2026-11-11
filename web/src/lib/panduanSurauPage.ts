// The private surau guide's page BODY, as plain TypeScript.
//
// WHY THIS IS NOT INSIDE app/panduan/surau/page.tsx
//   page.tsx is a .tsx file, and the repo's unit runner is `node --test … src/lib/*.test.ts` with
//   type stripping only — Node refuses to import ".tsx" (ERR_UNKNOWN_FILE_EXTENSION). Every other
//   guard in src/lib renders a .ts component for that reason (see guidesContent.test.ts, which
//   renders Markdown.ts rather than a page). So the page's metadata and its article tree live
//   here, in a module a test can import, and page.tsx is a four-line wrapper that re-exports them.
//   That keeps ONE owner for both: the guard asserts the same object the route serves, instead of
//   asserting a copy of it.
//
// WHAT THIS PAGE IS
//   A private, Malay, nine-step setup guide for a surau committee taking donations with JomForm
//   where CHIP's processing fee lands on the donor. Its source is docs/guides/surau-donation/
//   (README + nine boxed screenshots), materialised by web/scripts/guides.mjs into
//   src/lib/guidesPrivate.gen.ts + public/panduan/surau/. It is NOT under app/docs, it is not in
//   GUIDES, and it is linked from nowhere: src/lib/panduanSurau.test.ts pins all of that.

import { createElement } from "react";
import type { Metadata } from "next";

import { Markdown } from "../components/Markdown.ts";
import { privateGuideBySlug } from "./guidesPrivate.gen.ts";

/** The route this page is served at — also the private guide's screenshot directory. */
export const PANDUAN_SURAU_ROUTE = "/panduan/surau";

const guide = privateGuideBySlug("surau-donation");

/**
 * The page metadata. `robots: { index: false, follow: false }` renders
 * `<meta name="robots" content="noindex, nofollow">`, which is the only thing that stops a search
 * engine that somehow learns the URL from keeping it. There is deliberately NO canonical: a
 * canonical is an advertisement of the page it names, and this page is not advertised.
 */
export const PANDUAN_SURAU_METADATA: Metadata = {
  title: "Panduan derma surau — JomForm",
  description:
    "Panduan peribadi untuk jawatankuasa surau: kutip derma dengan JomForm dan CHIP, dengan caj pemprosesan ditanggung pembayar.",
  robots: { index: false, follow: false },
};

/** The article. A plain server component: no hooks, no client boundary. */
export function PanduanSurauArticle() {
  // The only URLs this page can render: the files the generator measured and copied for THIS
  // guide. An image reference the manifest does not know falls back to its alt text.
  //
  // A shot renders its DETAIL crop — the capture is a 1900 px full-page screenshot, which at the
  // 358 px article column of a 390 px phone is a 0.19x thumbnail whose labels land at ~3 px
  // (card t_e1ea03a2: "gambar screenshot tu terlalu kecik … yg baca ni mungkin org berusia").
  // The full capture stays reachable as the figure's link (`href`), which is what a phone opens
  // so the reader can pinch-zoom; `src/lib/panduanSurau.test.ts` holds the scale floor.
  const shots = new Map(guide.shots.map((s) => [s.file, s]));
  const resolveImage = (file: string) => {
    const shot = shots.get(file);
    if (!shot) return null;
    const full = `${guide.assets}/${shot.file}`;
    if (!shot.detail) return { src: full, width: shot.width, height: shot.height };
    return {
      src: `${guide.assets}/${shot.detail.path}`,
      width: shot.detail.width,
      height: shot.detail.height,
      href: full,
    };
  };

  return createElement(
    "main",
    { className: "mx-auto max-w-3xl min-w-0 px-4 py-10 sm:px-6" },
    // The app shell's <html lang> is "en" (root layout); the content is the only thing on this
    // page, so marking the article `ms` is what tells a screen reader which voice to use.
    createElement(
      "article",
      { lang: "ms", className: "min-w-0" },
      createElement(
        "p",
        {
          className:
            "rounded-lg border border-amber-200 bg-amber-50 px-4 py-2 text-xs leading-relaxed text-amber-900",
        },
        "Panduan peribadi untuk jawatankuasa surau. Ia tidak disenaraikan dan tidak muncul dalam " +
          "carian — simpan pautan ini, dan jangan siarkan di laman awam.",
      ),
      createElement("h1", { className: "mt-5 text-2xl font-bold text-slate-900 sm:text-3xl" }, guide.title),
      createElement("p", { className: "mt-3 text-base leading-relaxed text-slate-600" }, guide.summary),
      createElement(
        "div",
        { className: "mt-8" },
        createElement(Markdown, { text: guide.body, mode: "docs", resolveImage }),
      ),
      createElement(
        "footer",
        { className: "mt-10 border-t border-slate-100 pt-6 text-xs leading-relaxed text-slate-500" },
        "Disediakan untuk jawatankuasa surau. Caj CHIP ialah kadar yang acquirer anda kenakan; " +
          "sahkan angka itu dengan kontrak CHIP anda sebelum mengaktifkannya.",
      ),
    ),
  );
}
