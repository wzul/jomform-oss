// Shared recursive walk over the app/docs tree (card t_3a5aa9b5).
//
// WHY THIS EXISTS
//   Three guards decide what "the pages on disk" means, and each carried its own
//   FLAT copy of the walk: llmsFullCoverage.test.ts, sitemapCoverage.test.ts and
//   docsProseConvention.test.ts. Each was a `readdirSync(DOCS_DIR, {withFileTypes})`
//   one level deep that skipped any directory without its own `page.tsx`:
//
//     for (const e of readdirSync(DOCS_DIR, { withFileTypes: true })) {
//       if (!e.isDirectory()) continue;
//       if (!existsSync(join(DOCS_DIR, e.name, "page.tsx"))) continue;   // <-- the hole
//       routes.push(`/docs/${e.name}`);
//     }
//
//   A nested Next route whose PARENT directory is not itself a route —
//   `app/docs/<seg>/<sub>/page.tsx` — is a real page, but the parent has no
//   `page.tsx`, so it is `continue`d and the walk is not recursive. Measured on
//   `origin/main` (2026-10-05, this card): with `app/docs/zz-probe/nested/page.tsx`
//   on disk and named nowhere, llmsFullCoverage stayed 3/3 GREEN and sitemapCoverage
//   5/5 GREEN; the sibling guards did NOT disagree about that shape — they shared
//   the hole, which is why the fix is one walk rather than three edits.
//
// WHAT IT PROVIDES
//   filesUnder(root):  every FILE under `root`, recursively, absolute, sorted.
//   docsRoutesOnDisk(docsDir): every real route under `docsDir` — `/docs` for the
//     directory's own page.tsx, otherwise `/docs/<posix-relative-parent-of-page.tsx>`.
//     A directory with no page.tsx is not a route; it is only ever a parent, and its
//     children are still reached.
//   routesOnDisk(dir, routePrefix): the same walk for ANY route tree under a known
//     prefix (card t_c9d23e2c, which reads the /legal tree as well as /docs), so
//     the two trees are read by one walk instead of a second hand-rolled copy.
//     docsRoutesOnDisk is this function with `/docs`.
//
// SCOPE, stated so it is not over-claimed: it walks the filesystem and reports what
// EXISTS. It does not know Next's special folders (a future `(group)`, `_private` or
// `[dynamic]` segment would be reported as a literal slug), because the tree carries
// none today and inventing a rule for one would be a guess — the day one appears, the
// guards fail loudly on a route nobody meant, which is the safe direction.

import { readdirSync } from "node:fs";

// Every file under `root`, recursively, as absolute paths, sorted.
export function filesUnder(root: string): string[] {
  const out: string[] = [];
  const walk = (dir: string) => {
    for (const e of readdirSync(dir, { withFileTypes: true })) {
      const full = `${dir}/${e.name}`;
      if (e.isDirectory()) walk(full);
      else out.push(full);
    }
  };
  walk(root);
  return out.sort();
}

// Every real route under `docsDir`, as `/docs` or `/docs/<posix-relative path>`,
// sorted. The relative path is the page.tsx's parent directory, so a nested route
// under a non-route parent (`docsDir/seg/sub/page.tsx` -> `/docs/seg/sub`) is counted
// rather than skipped.
export function docsRoutesOnDisk(docsDir: string): string[] {
  return routesOnDisk(docsDir, "/docs");
}

// Every real route under `dir`, as `routePrefix` or `routePrefix/<relative path>`,
// sorted — the same recursive walk and the same `page.tsx` key as
// docsRoutesOnDisk, generalized in card t_c9d23e2c so the /legal tree is read by
// ONE walk rather than a second copy of this loop. A directory that is only a
// parent is still not a route.
export function routesOnDisk(dir: string, routePrefix: string): string[] {
  const base = `${dir.replace(/\/+$/, "")}/`;
  const routes: string[] = [];
  for (const file of filesUnder(dir)) {
    if (!file.endsWith("/page.tsx")) continue;
    const rel = file.slice(base.length, -"/page.tsx".length); // "" for the index
    routes.push(rel === "" ? routePrefix : `${routePrefix}/${rel}`);
  }
  return routes.sort();
}
