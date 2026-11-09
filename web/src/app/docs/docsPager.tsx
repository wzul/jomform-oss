"use client";

// Previous/next between /docs pages, in sidebar reading order (card t_45fd1bee).
//
// Rendered under every page by the docs layout, so 30 pages can be read end to end
// without going back to the sidebar. The order comes from the registry (the sidebar's own
// order, with the eight guides after their index), not from this component.

import Link from "next/link";
import { usePathname } from "next/navigation";

import { Card } from "../../components/ui";
import { docsPager } from "../../lib/docsNav";

export function DocsPager() {
  const pathname = usePathname() ?? "/docs";
  const { prev, next } = docsPager(pathname);
  if (!prev && !next) return null;

  return (
    <nav aria-label="Docs pagination" className="mt-10 border-t border-[var(--jf-border)] pt-6">
      <div className="grid gap-3 sm:grid-cols-2">
        {prev ? (
          <Link href={prev.path} className="group block">
            <Card interactive className="h-full px-4 py-3">
              <span className="block text-xs font-medium uppercase tracking-wide text-slate-600">
                Previous
              </span>
              <span className="mt-1 block text-sm font-semibold text-slate-900 group-hover:text-[var(--jf-primary-strong)]">
                ← {prev.label}
              </span>
            </Card>
          </Link>
        ) : (
          <span aria-hidden="true" />
        )}
        {next ? (
          <Link href={next.path} className="group block sm:text-right">
            <Card interactive className="h-full px-4 py-3">
              <span className="block text-xs font-medium uppercase tracking-wide text-slate-600">
                Next
              </span>
              <span className="mt-1 block text-sm font-semibold text-slate-900 group-hover:text-[var(--jf-primary-strong)]">
                {next.label} →
              </span>
            </Card>
          </Link>
        ) : null}
      </div>
    </nav>
  );
}
