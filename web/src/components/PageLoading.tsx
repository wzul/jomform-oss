"use client";

import type { ComponentProps } from "react";
import { Shell } from "./Shell";
import { useI18n } from "./i18n";

// Shared page-loading state for a dashboard page whose own data is still in
// flight.
//
// THE DEFECT THIS EXISTS TO STOP
//   Several pages early-returned a bare `<main className="flex min-h-screen
//   items-center justify-center text-slate-400">…</main>` while their list
//   fetch was outstanding. That tree contains no Shell around it, so React
//   unmounted the sidebar and the top bar for the duration of the fetch and
//   mounted them again afterwards. Nothing reloaded the browser — the route
//   change is a normal client-side <Link> transition — but the shell
//   disappearing and reappearing is exactly what reads as a full page reload,
//   and it is why /sales -> /forms felt slow while /forms -> /sales did not
//   (/sales has no such gate: it paints the Shell immediately and fills the
//   table in).
//
// THE RULE
//   A loading branch renders INSIDE the shell, never instead of it: the
//   skeleton replaces the page's content area only, so the navigation chrome is
//   continuously mounted from one dashboard page to the next. See
//   src/lib/pageShellLoading.test.ts, which fails the build if any page.tsx
//   loading branch drops the shell.
export function PageLoading({
  active,
}: {
  active: ComponentProps<typeof Shell>["active"];
}) {
  const { t } = useI18n();
  return (
    <Shell active={active}>
      {/* role="status" + aria-busy is the loading affordance a screen reader
          gets; the skeleton bars below are decorative and read as nothing. */}
      <div role="status" aria-busy="true" className="w-full flex-1 px-6 py-8">
        <span className="sr-only">{t("common.loading")}</span>
        <div aria-hidden="true" className="h-6 w-40 jf-skeleton rounded bg-slate-200" />
        <div aria-hidden="true" className="mt-2 h-4 w-64 max-w-full jf-skeleton rounded bg-slate-100" />
        <div aria-hidden="true" className="mt-6 h-56 w-full jf-skeleton rounded-lg bg-slate-100" />
      </div>
    </Shell>
  );
}
