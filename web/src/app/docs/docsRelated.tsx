"use client";

// The cross-links between a product page and the screenshot guides (card t_45fd1bee).
//
// The mapping is authored once, on the sidebar row in app/docs/layout.tsx (`guides: [...]`),
// and the reverse direction is derived from it — so a guide always links back to the
// product pages it belongs to and cannot become a dead end. This component renders the
// curated lists; the page prose is untouched (the docs prose guards are not asked to
// tolerate a new sentence here).

import Link from "next/link";
import { usePathname } from "next/navigation";

import { Card } from "../../components/ui";
import { docsEntry, relatedGuides, relatedProducts } from "../../lib/docsNav";

export function DocsRelated() {
  const pathname = usePathname() ?? "/docs";
  const entry = docsEntry(pathname);
  if (!entry) return null;

  const guides = relatedGuides(pathname);
  const products = relatedProducts(pathname);
  const isGuide = entry.kind === "guide";
  const list = isGuide ? products : guides;
  const heading = isGuide ? "Product documentation for this guide" : "Guides for this feature";

  return (
    <aside aria-label={heading} className="mt-10">
      {list.length > 0 ? (
        <>
          <p className="text-sm font-semibold text-slate-900">{heading}</p>
          <ul className="mt-3 grid gap-2 sm:grid-cols-2">
            {list.map((related) => (
              <li key={related.path}>
                <Link href={related.path} className="group block h-full">
                  <Card interactive className="h-full px-4 py-3">
                    <span className="block text-sm font-semibold text-slate-900 group-hover:text-[var(--jf-primary-strong)]">
                      {related.label}
                    </span>
                    <span className="mt-0.5 block text-xs text-slate-600">
                      {related.kind === "guide" ? "Screenshot guide" : related.section}
                    </span>
                  </Card>
                </Link>
              </li>
            ))}
          </ul>
        </>
      ) : null}
      {entry.updated ? (
        <p className="mt-6 text-xs text-slate-600">Last updated {entry.updated}</p>
      ) : null}
    </aside>
  );
}
