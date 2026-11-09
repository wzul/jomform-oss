import type { Metadata } from "next";
import Link from "next/link";

import { docsMetadata } from "../../../lib/docsMetadata";
import { GUIDES } from "../../../lib/guides.gen";

// The screenshot guides index (card t_7b9f6d18). The guides themselves already existed in the
// repo (docs/guides/<slug>) and were reachable from nowhere; this page is the way in, and the
// landing page's donation showcase links straight at its guide.
//
// The list comes from src/lib/guides.gen.ts, which web/scripts/guides.mjs generates from
// docs/guides — so a guide cannot be added to the repo and forgotten here, and a card in this
// list cannot point at a guide that does not exist (src/lib/guidesContent.test.ts pins both
// directions against the page files on disk).
export const metadata: Metadata = docsMetadata({
  title: "Screenshot guides — JomForm Docs",
  description:
    "Step-by-step setup guides for JomForm. Every step has its own screenshot from the running product, with the control you touch at that step boxed in red.",
  path: "/docs/guides",
});

export default function GuidesIndexPage() {
  return (
    <div className="max-w-3xl min-w-0">
      <h1 className="text-2xl font-bold text-slate-900 sm:text-3xl">Screenshot guides</h1>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        Each guide below walks one setup from the settings page to the live form. Every step has
        its own screenshot, captured from the running product, with the control you touch at that
        step marked in red — so you can follow it beside your own dashboard.
      </p>

      <ul className="mt-8 space-y-4">
        {GUIDES.map((g) => (
          <li key={g.slug} className="rounded-xl border border-slate-200 p-5">
            <h2 className="text-lg font-semibold text-slate-900">
              <Link
                href={`/docs/guides/${g.slug}`}
                className="underline decoration-dotted underline-offset-2 hover:text-[var(--jf-primary-strong)]"
              >
                {g.title}
              </Link>
            </h2>
            <p className="mt-1 text-sm leading-relaxed text-slate-600">{g.summary}</p>
            <p className="mt-2 text-xs text-slate-500">{g.shots.length} screenshots — one per step</p>
          </li>
        ))}
      </ul>

      <p className="mt-8 text-sm text-slate-600">
        <Link href="/docs" className="underline decoration-dotted underline-offset-2 hover:text-slate-900">
          ← Back to the docs index
        </Link>
      </p>
    </div>
  );
}
