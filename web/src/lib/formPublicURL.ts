// The one place the /forms row decides what its "View" control does.
//
// THE BUG: the row rendered `<a href={formPublicURL(f)} target="_blank">`, and
// `formPublicURL` returned "" whenever the API payload carried no `url` and no
// workspace slug could be read from localStorage. A browser resolves `href=""`
// to the CURRENT document, so with target="_blank" the click opened /forms
// itself in a new tab — reported as "bila user klik view form, dia load form
// page tu balik".
//
// Layer 3 of the fix (defence-in-depth, NOT the fix itself): the page must not be
// able to render an empty href at all. The real fix is on the server, where the
// URL is resolved through the same host the public request path serves
// (WorkspaceStore.PublicHostSlugByAccount → the workspace slug) — but `href=""`
// is ALWAYS a bug regardless of what the server answers, and an empty href is
// invisible until someone clicks it. So the decision is a tagged result rather
// than a bare string, and the page renders a non-navigating control for the
// `unavailable` case instead of an anchor.
//
// Kept as a plain .ts module (no JSX) so the repo's node test runner can execute
// it and assert the real rendered attribute through react-dom/server — see
// formPublicURL.test.ts. A .tsx file cannot be imported by that runner.

/** The subset of a form row this module needs. */
export type FormURLSource = { url?: string; slug: string };

/** The outcome of resolving a form's public link for the dashboard row. */
export type FormViewAction =
  // `href` is guaranteed non-empty: it is what the anchor is given.
  | { kind: "link"; href: string }
  // Nothing resolved — render a non-navigating control, never `<a href="">`.
  | { kind: "unavailable" };

/**
 * formPublicURL prefers the API-provided absolute URL (the API resolves the
 * owning workspace slug / custom domain server-side) and falls back to the
 * workspace-subdomain host only when the payload has none. Returns "" when
 * neither is available.
 *
 * Callers must go through formViewAction: this function can legitimately return
 * "" and an empty string is not usable as an href.
 */
export function formPublicURL(f: FormURLSource, workspaceHost: string): string {
  if (f.url) return f.url;
  return workspaceHost ? `${workspaceHost}/${f.slug}` : "";
}

/**
 * formViewAction turns the resolution into the decision the row renders.
 *
 * An empty (or whitespace-only) resolution is reported as `unavailable`, so the
 * page has no path that feeds "" into an href. A form slug with no host is not a
 * degraded link — it is no link.
 */
export function formViewAction(f: FormURLSource, workspaceHost: string): FormViewAction {
  const href = formPublicURL(f, workspaceHost).trim();
  if (href === "") return { kind: "unavailable" };
  return { kind: "link", href };
}
