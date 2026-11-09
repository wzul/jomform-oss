"use client";

// One sidebar row that knows whether it is the page you are on (card t_45fd1bee).
//
// The layout is a server component, so it cannot read the pathname; a whole client sidebar
// would also mean shipping NAV_GROUPS across the boundary and would move the authored table
// out of the file the index guard parses. A per-row island keeps the table where it is and
// adds the one thing the reader needs: `aria-current="page"` (and a visible active state)
// on the page they are reading, including a section page under a listed parent.

import Link from "next/link";
import { usePathname } from "next/navigation";

export function DocsNavLink({ href, children }: { href: string; children: React.ReactNode }) {
  const pathname = usePathname() ?? "";
  const active = pathname === href || (href !== "/docs" && pathname.startsWith(`${href}/`));
  return (
    <Link
      href={href}
      aria-current={active ? "page" : undefined}
      className={[
        "mt-1 block rounded-md px-3 py-2 text-sm",
        active
          ? "bg-slate-100 font-medium text-slate-900"
          : "text-slate-600 hover:bg-slate-50 hover:text-slate-900",
      ].join(" ")}
    >
      {children}
    </Link>
  );
}
