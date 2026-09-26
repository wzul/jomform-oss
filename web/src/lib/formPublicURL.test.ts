// Guard for the dashboard's "View" action on /forms — the reported defect was a
// click that RELOADED /forms instead of opening the form.
//
// THE BUG: `app/forms/page.tsx` rendered
//
//     <a href={formPublicURL(f)} target="_blank">{t("bld.view")}</a>
//
// and `formPublicURL` returned "" whenever the API payload carried no `url` and
// no workspace slug could be read from localStorage. The browser resolves
// `href=""` to the CURRENT document, so with target="_blank" the click opened
// /forms itself in a new tab. The API's `url` was empty for every
// signup-created account because the server resolved the URL through
// `accounts.workspace_id` (NULL for those rows) instead of the host the public
// request path actually serves.
//
// WHAT THIS GUARD PINS
//   1. The resolution decision cannot produce an empty href. `formViewAction`
//      returns a tagged result; an empty resolution is `unavailable`, never a
//      link. BITE-PROVED: the anchor built from a `link` action is rendered with
//      react-dom/server and its href is asserted non-empty, and the pre-fix
//      shape (`<a href={formPublicURL(...)}>`) is rendered alongside it and
//      asserted to produce the EXACT empty attribute — so the guard shows the
//      difference rather than asserting a property both shapes might share.
//   2. app/forms/page.tsx cannot go back to feeding the raw string into an
//      anchor, and renders a NON-NAVIGATING control for the unavailable case.
//      Reverting layer 3 fails here.
//   3. Layer 2 (the real fix) is still in place on the server: the Go URL
//      builders resolve through WorkspaceStore.PublicHostSlugByAccount, which is
//      the WORKSPACE-only lookup — the one host the public request path serves.
//      The account-slug fallback it used to carry is gone (a workspace is served
//      on its workspace subdomain and nothing else). Note what did NOT change:
//      a signup still creates an account and NO workspace (#718) — the workspace
//      comes from the user's own ask-first create. An account in that state has
//      no host yet, and these builders say so rather than naming a synthetic
//      address. Read cross-language on purpose (same idiom as
//      homeServerContent.test.ts): the invariant is one value living in two
//      languages, and a revert on the Go side is otherwise invisible from the
//      web tree.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * It cannot click. The end-to-end click is checked in a browser against a
//     real API payload (a production build + a stub/real API), reported in the
//     card handoff.
//   * It cannot execute React state, so it asserts the rendered attribute of the
//     two branches built here, not the page's own render.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { formPublicURL, formViewAction } from "./formPublicURL.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const formsPage = read("../app/forms/page.tsx");
const goWorkspace = read("../../../api/internal/dbstore/workspace.go");
const goRest = read("../../../api/internal/httpapi/rest_handlers.go");
const goMcp = read("../../../api/internal/mcpsrv/forms.go");
const goPayments = read("../../../api/internal/payments/formurl.go");

// The exact payload shape that produced the report: a form the API returned
// without a url (workspace slug unresolvable), visited with no workspace in
// localStorage — which is the normal case for a direct visit to /forms.
const UNRESOLVED = { slug: "new-form", url: "" };
const NO_HOST = "";

test("the guard is looking at real files (it cannot pass vacuously)", () => {
  assert.ok(
    formsPage.includes("export default function FormsPage"),
    "app/forms/page.tsx is not the forms page source — the guard's anchor moved",
  );
  assert.ok(
    goWorkspace.includes("func (s *WorkspaceStore) PublicHostSlugByAccount"),
    "could not find WorkspaceStore.PublicHostSlugByAccount in api/internal/dbstore/workspace.go",
  );
});

test("the pre-fix shape really produced href=\"\" — the bug is reproducible in this guard", () => {
  // This is the failure mode being fixed, rendered: a bare string straight into
  // the attribute. If this ever stops being empty, the guard's premise moved and
  // the assertions below would be guarding nothing.
  const prefixed = renderToStaticMarkup(
    createElement("a", { href: formPublicURL(UNRESOLVED, NO_HOST), target: "_blank" }, "View"),
  );
  assert.match(
    prefixed,
    /href=""/,
    "the pre-fix `<a href={formPublicURL(f)}>` shape no longer renders href=\"\" — re-anchor this guard " +
      "before trusting it, because the defect it documents is no longer reproduced here",
  );
  assert.ok(
    !formPublicURL(UNRESOLVED, NO_HOST),
    "formPublicURL must still report an EMPTY resolution for this input (it is the resolver, not the decision)",
  );
});

test("the decision turns an empty resolution into a non-navigating action, never an empty href", () => {
  const action = formViewAction(UNRESOLVED, NO_HOST);
  assert.equal(
    action.kind,
    "unavailable",
    "an unresolved form URL must not be a link action — the page would render `<a href=\"\">`, which the " +
      "browser resolves to the CURRENT page (the reported 'it loads the form page again')",
  );
  assert.ok(
    !("href" in action),
    "the unavailable action must carry no href at all, so no render path can emit href=\"\"",
  );
});

test("a resolved URL renders as a real anchor with a NON-EMPTY href", () => {
  // The API-provided absolute URL wins (the server resolves the owning
  // workspace/custom domain), so no client-side host is needed.
  const apiURL = "https://member00example-com.jomform.com/new-form";
  const action = formViewAction({ slug: "new-form", url: apiURL }, NO_HOST);
  assert.equal(action.kind, "link");
  const html = renderToStaticMarkup(createElement("a", { href: action.kind === "link" ? action.href : "" }, "View"));
  assert.match(html, /href="https:\/\/member00example-com\.jomform\.com\/new-form"/);

  // The fallback host path (older payload with no url) also yields a real link,
  // and the rendered attribute is never empty.
  const viaHost = formViewAction(UNRESOLVED, "https://member00example-com.jomform.com");
  assert.equal(viaHost.kind, "link");
  const html2 = renderToStaticMarkup(
    createElement("a", { href: viaHost.kind === "link" ? viaHost.href : "" }, "View"),
  );
  assert.doesNotMatch(html2, /href=""/);
  assert.match(html2, /href="https:\/\/member00example-com\.jomform\.com\/new-form"/);
});

test("the page renders the anchor only from a link action, and never passes the raw resolver into an href", () => {
  // A revert of layer 3 is exactly this shape: the raw string in the attribute.
  assert.doesNotMatch(
    formsPage,
    /<a\s[^>]*href=\{formPublicURL\(/,
    "app/forms/page.tsx feeds formPublicURL() straight into an href again — an empty resolution renders " +
      "`<a href=\"\">`, which reloads /forms instead of opening the form",
  );
  // The href must come from the guarded action.
  assert.match(
    formsPage,
    /href=\{view\.href\}/,
    "app/forms/page.tsx no longer renders the href from the formViewAction result",
  );
  // Both usages of the resolution must go through the decision.
  assert.match(formsPage, /const view = formViewAction\(f, workspaceHost\)/);
  assert.equal(
    (formsPage.match(/formViewAction\(/g) ?? []).length,
    1,
    "the row must resolve its link once, through one decision — a second call site is a second rule",
  );
});

test("the unavailable branch is a disabled control, not a navigating one", () => {
  // The unavailable case must not be an anchor or a Link: both navigate, and
  // the whole point is that there is nowhere to go.
  // Anchor on the ACTION ternary (`? (`), not the URL caption line above it,
  // which also mentions view.kind.
  const branchStart = formsPage.indexOf('view.kind === "link" ? (');
  assert.ok(branchStart > 0, "could not locate the view control's branch — re-anchor this guard");
  // The ternary ends where the published branch's React fragment closes.
  const branchEnd = formsPage.indexOf("</>", branchStart);
  assert.ok(branchEnd > branchStart, "could not locate the end of the view control's branch");
  const unavailable = formsPage.slice(formsPage.indexOf(") : (", branchStart) + 5, branchEnd);
  assert.ok(
    unavailable.includes("<button"),
    "the slice did not reach the unavailable branch — re-anchor this guard",
  );
  assert.match(
    unavailable,
    /<button[\s\S]*?disabled/,
    "the unavailable branch must render a DISABLED button — an anchor/Link with no destination is the bug",
  );
  assert.doesNotMatch(
    unavailable,
    /<a\s|<Link\s/,
    "the unavailable branch renders a navigating element — with no URL there is nothing to navigate to",
  );
  assert.match(
    unavailable,
    /title=\{t\("forms\.viewUnavailableHint"\)\}/,
    "the disabled control must explain itself (title), or it reads as a broken button",
  );
});

test("layer 2 is intact: the Go URL builders resolve the host the public request path serves", () => {
  // The store resolver is workspace-only — the same rule the serving path
  // enforces. There is deliberately NO account-slug fallback any more: making
  // the two agree by letting the serving path accept a second hostname left
  // every workspace reachable at a synthetic address the dashboard never shows.
  // The NULL-workspace case that fallback papered over is not "fixed" by minting
  // a workspace at signup — a signup creates an account and NO workspace (#718),
  // and the workspace comes from the user's ask-first create (see the
  // public_host_slug_test.go fixture precondition).
  assert.match(
    goWorkspace,
    /func \(s \*WorkspaceStore\) PublicHostSlugByAccount\(ctx context\.Context, accountID string\) \(string, error\) \{\n\treturn s\.WorkspaceSlugByAccount\(ctx, accountID\)\n\}/,
    "WorkspaceStore.PublicHostSlugByAccount no longer resolves through the workspace-only lookup — a " +
      "form URL could name a host the public server does not serve",
  );
  assert.doesNotMatch(
    goWorkspace,
    /LEFT JOIN workspaces sp ON sp\.id = a\.workspace_id/,
    "WorkspaceStore.go still carries the account-slug fallback query — a workspace must be reachable on " +
      "its workspace subdomain and nothing else",
  );
  // Every form-URL builder must call it. A revert to the workspace-only resolver
  // in any one of them silently empties that surface's link again.
  for (const [name, src, why] of [
    ["api/internal/httpapi/rest_handlers.go", goRest, "the dashboard /forms list (this report)"],
    ["api/internal/mcpsrv/forms.go", goMcp, "MCP list_forms / get_form url"],
    ["api/internal/payments/formurl.go", goPayments, "the buyer receipt email and receipt PDF"],
  ] as const) {
    assert.match(
      src,
      /PublicHostSlugByAccount\(/,
      `${name} does not resolve through PublicHostSlugByAccount — ${why} would not agree with the host ` +
        `the public server actually serves`,
    );
  }
});
