// One screenshot guide, rendered from docs/guides/<slug>/README.md.
//
// WHY THIS IS A SHARED COMPONENT AND NOT A DYNAMIC ROUTE (card t_7b9f6d18)
//   The guides live at /docs/guides/<slug>. A dynamic `[slug]` segment would make the three
//   docs-wide walks (docsRoutes.ts → sitemapCoverage, llmsFullCoverage, docsA11y) report a
//   literal "[slug]" route, and each of those guards is written to fail loudly rather than
//   guess at a dynamic segment. A static page per guide keeps every guard reading real routes:
//   /docs/guides, /docs/guides/payment-fees and /docs/guides/donations are ordinary pages, and
//   src/lib/guidesContent.test.ts fails the build when a guide in GUIDES has no page (or a page
//   has no guide), so adding a guide cannot silently produce a 404 — or an orphan page.
//
//   The prose is rendered by the repo's ONE markdown renderer in `mode="docs"` — headings,
//   tables and images for checked-in repo content. No second parser (AGENTS.md).
//
// WHAT IT CANNOT DO, stated honestly: the images are plain <img> tags at /guides/<slug>/<file>,
// copied from docs/guides by the generator. They are not run through next/image's optimiser,
// so the guide ships the exact PNG that was captured and reviewed — a step's screenshot is
// evidence, and re-encoding evidence is worse than paying the bytes.

import Link from "next/link";

import { GUIDES, type Guide } from "../lib/guides.gen";
import { Markdown } from "./Markdown";

export function GuideArticle({ guide }: { guide: Guide }) {
  // The only URLs this page can render: the files the generator measured and copied for THIS
  // guide. An image reference the manifest does not know falls back to its alt text.
  const shots = new Map(guide.shots.map((s) => [s.file, s]));
  const resolveImage = (file: string) => {
    const shot = shots.get(file);
    if (!shot) return null;
    return { src: `/guides/${guide.slug}/${shot.file}`, width: shot.width, height: shot.height };
  };

  const others = GUIDES.filter((g) => g.slug !== guide.slug);

  return (
    <article className="max-w-3xl min-w-0">
      <h1 className="mt-3 text-2xl font-bold text-slate-900 sm:text-3xl">{guide.title}</h1>
      <p className="mt-2 text-sm text-slate-600">{guide.summary}</p>
      <p className="mt-3 text-xs text-slate-600">
        {guide.shots.length} screenshots, one per step, captured from the running product. The red
        box marks the control you touch at that step.
      </p>

      <div className="mt-6">
        <Markdown text={guide.body} mode="docs" resolveImage={resolveImage} />
      </div>

      <footer className="mt-10 border-t border-slate-100 pt-6">
        <p className="text-sm font-semibold text-slate-900">More guides</p>
        <ul className="mt-2 space-y-1 text-sm text-slate-600">
          <li>
            <Link
              href="/docs/guides"
              className="underline decoration-dotted underline-offset-2 hover:text-slate-900"
            >
              All screenshot guides
            </Link>
          </li>
          {others.map((g) => (
            <li key={g.slug}>
              <Link
                href={`/docs/guides/${g.slug}`}
                className="underline decoration-dotted underline-offset-2 hover:text-slate-900"
              >
                {g.title}
              </Link>
            </li>
          ))}
          <li>
            <Link
              href="/docs"
              className="underline decoration-dotted underline-offset-2 hover:text-slate-900"
            >
              ← Back to the docs index
            </Link>
          </li>
        </ul>
      </footer>
    </article>
  );
}
