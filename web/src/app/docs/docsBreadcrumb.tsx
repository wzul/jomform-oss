"use client";

// The breadcrumb every /docs page renders above its <h1> (card t_45fd1bee).
//
// It reads the current path from next/navigation and the trail from the ONE registry
// (src/lib/docsNav.ts), so a page cannot forget its own crumb and the trail cannot
// disagree with the sidebar. The last crumb is the page itself: plain text with
// `aria-current="page"`, not a link to where the reader already is.
//
// `aria-label="Docs breadcrumb"` is deliberately not the generic "Breadcrumb": the guide
// article used to render its own nav under that name, and two navigation landmarks with
// one name fail axe's landmark-unique. This layout is now the only breadcrumb on a docs
// page, and the distinct name keeps it distinguishable from the sidebar and the ToC.

import Link from "next/link";
import { usePathname } from "next/navigation";

import { docsBreadcrumb } from "../../lib/docsNav";

export function DocsBreadcrumb() {
  const pathname = usePathname() ?? "/docs";
  const trail = docsBreadcrumb(pathname);

  return (
    <nav aria-label="Docs breadcrumb" className="mb-5 text-sm">
      <ol className="flex flex-wrap items-center gap-x-2 gap-y-1 text-slate-600">
        {trail.map((crumb, index) => {
          const current = index === trail.length - 1;
          return (
            <li key={`${crumb.label}-${index}`} className="flex items-center gap-x-2">
              {index > 0 ? (
                <span aria-hidden="true" className="text-slate-600">
                  ›
                </span>
              ) : null}
              {/* `min-h-[44px]` (card t_ff973d0b): the /docs index's last
                  sub-24px tap target was this crumb. It is shared chrome, so the
                  row is 44px tall on every docs page — the crumb sits above the
                  h1, where the height costs nothing, and a phone reader gets a
                  real target. */}
              {crumb.href && !current ? (
                <Link
                  href={crumb.href}
                  className="inline-flex min-h-[44px] items-center underline decoration-dotted underline-offset-2 hover:text-slate-900"
                >
                  {crumb.label}
                </Link>
              ) : (
                <span aria-current={current ? "page" : undefined} className="text-slate-900">
                  {crumb.label}
                </span>
              )}
            </li>
          );
        })}
      </ol>
    </nav>
  );
}
