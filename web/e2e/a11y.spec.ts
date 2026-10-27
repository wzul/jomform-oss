import { test, expect } from "@playwright/test";

import { blockersOf, gateScan } from "./axeGate";

// axe-core accessibility scan (issue #104).
//
// Scans the key public pages and FAILS on any critical or serious violation.
// Minor/moderate findings are reported but do not fail the build, so the gate
// stays strict on the WCAG AA blockers while surfacing the rest for triage.
//
// THIS LIST IS THE PUBLIC ROUTES ONLY. An authenticated dashboard page cannot be
// scanned from here: its session comes from the Go API, which this gate does not
// provision (no session cookie, no DB), so an entry for `/profile` would `goto`
// a page that bounced to /landing and scan a shell with no controls on it.
// The signed-in surfaces are scanned by the SIBLING `a11y-*.spec.ts` files, which
// stub the `dashboard-bundle` at the browser level and are members of this same
// `a11y` project by filename prefix (`playwright.config.ts`). Card t_b589a807 is
// why that half exists: this list was seven public routes while `/profile` and
// `/settings` carried 15 critical `aria-required-parent` nodes between them, and
// `npm run a11y` was green throughout. `src/lib/a11yAuthedCoverage.test.ts` holds
// those routes to a real page on disk AND to a spec that actually runs axe.
//
// The public order form (/f/...) and thank-you page (/t/...) are served by
// the Go API, not the Next.js app, so they are covered by the manual
// screen-reader + browser-matrix checklist (docs/accessibility.md) rather
// than this automated pass.
const PAGES = [
  { name: "login", path: "/login" },
  { name: "landing", path: "/landing" },
  { name: "reset-password", path: "/reset-password" },
  { name: "magic-link", path: "/magic-link" },
  { name: "brand", path: "/brand" },
  { name: "legal-privacy", path: "/legal/privacy" },
  { name: "legal-terms", path: "/legal/terms" },
  // The MCP setup page (/mcp) is public and linked from the landing page's MCP
  // section, the profile's MCP tab and the sitemap — and it was in NO scan.
  // It is not a docs route (`/docs/mcp` is a different page and is listed
  // below), so it matched no prefix this list polices, and a page outside every
  // gate is a page that ships unnoticed. It is the second place a code snippet
  // is painted on a dark `bg-slate-900` slab (`/landing` is the other), which is
  // a pairing no class-name guard can judge without compositing a background —
  // see `src/lib/emeraldText.test.ts`, which abstains on a dark slab precisely
  // because the browser gate is what reads it. src/lib/a11yDarkSlabCoverage.test.ts
  // now fails the unit suite when such a slab appears on a route this list omits.
  { name: "mcp", path: "/mcp" },
  // t_f5a8730d / t_d768342e: the knowledge base was outside this scan entirely,
  // so a docs PR could not be checked by the a11y gate at all (PR 676 edited
  // /docs/customize and even a full `npm run a11y` never looked at it). Every
  // page under web/src/app/docs is listed here; /docs and /docs/customize are
  // the minimum, the rest so a regression on any guide turns this red.
  // src/lib/docsA11y.test.ts fails the unit suite when a docs page is missing
  // from this list, so a page added later cannot quietly stay outside the gate.
  //
  // t_5425c958 (the rebase onto main): the two sides' lists were UNIONED rather
  // than one being chosen — main had added /docs/builder, /docs/api and the
  // guides this branch's own structure links; this branch had added the guide
  // pages a "?" link or a /docs entry sends a reader to. Either side alone
  // leaves a public, sitemapped page unscanned.
  { name: "docs-index", path: "/docs" },
  { name: "docs-shipping", path: "/docs/shipping" },
  { name: "docs-builder", path: "/docs/builder" },
  { name: "docs-workspaces", path: "/docs/workspaces" },
  { name: "docs-sales", path: "/docs/sales" },
  { name: "docs-email", path: "/docs/email" },
  { name: "docs-security", path: "/docs/security" },
  { name: "docs-customize", path: "/docs/customize" },
  { name: "docs-embed", path: "/docs/embed" },
  { name: "docs-subscriptions", path: "/docs/subscriptions" },
  { name: "docs-variations", path: "/docs/variations" },
  { name: "docs-spam-detection", path: "/docs/spam-detection" },
  { name: "docs-ses-reputation", path: "/docs/ses-reputation" },
  // Card t_eb09b25a: the activity log / audit trail.
  { name: "docs-activity", path: "/docs/activity" },
  { name: "docs-placeholders", path: "/docs/placeholders" },
  { name: "docs-mcp", path: "/docs/mcp" },
  // t_bac29e18: the REST API reference is a public, sitemapped, footer-linked
  // page — exactly the shape this scan exists for. Its endpoint tables are the
  // largest block of dense small text on the docs site, so leaving it unscanned
  // would repeat the gap t_d768342e closed.
  { name: "docs-api", path: "/docs/api" },
  { name: "docs-chat", path: "/docs/chat" },
  { name: "docs-feedback", path: "/docs/feedback" },
  // Issue #255's custom HTML forms guide. docsA11y.test.ts walks the pages on
  // disk and fails when one of them is missing here, so this entry ships with
  // the page.
  { name: "docs-custom-forms", path: "/docs/custom-forms" },
];

for (const page of PAGES) {
  test(`axe: ${page.name} has no critical/serious violations`, async ({ page: p }) => {
    const response = await p.goto(page.path, { waitUntil: "networkidle" });

    // THE PAGE MUST ACTUALLY BE THERE (t_81b2a928). `goto` follows a redirect and
    // resolves for a 404 too, so without this the scan below reads a Next.js "not
    // found" page: no landmarks, no headings, no interactive controls — and axe
    // finds nothing to complain about, so the case PASSES GREEN for a route that
    // does not exist. A PAGES entry with a typo, or one left behind by a page that
    // was renamed or removed, would therefore reduce coverage with no red anywhere.
    // The scan is only evidence for a 2xx document.
    const status = response?.status() ?? 0;
    if (status < 200 || status >= 300) {
      throw new Error(
        `a11y scan: ${page.name} (${page.path}) answered HTTP ${status || "no response"} — ` +
          `nothing was scanned. Fix the PAGES entry in web/e2e/a11y.spec.ts, or remove it ` +
          `if the page is gone; a 404 cannot be read as "no violations".`,
      );
    }

    // The gate's rule — one definition, in e2e/axeGate.ts, shared with the nine
    // scoped arms in the sibling a11y-*.spec.ts files (card t_5aa6c8ac). No
    // `withTags` anywhere: axe's DEFAULT set, which is what those arms used to
    // claim to mirror while running 6 of its 105 rules.
    const results = await gateScan(p);

    const blockers = blockersOf(results);

    // Report everything for triage, but only fail on critical/serious.
    if (blockers.length > 0) {
      console.log(
        `\n[${page.name}] ${blockers.length} critical/serious violation(s):\n` +
          blockers
            .map(
              (v) =>
                `  - ${v.id} (${v.impact}): ${v.help}\n` +
                v.nodes
                  .slice(0, 3)
                  .map((n) => `      ${n.target.join(" ")} — ${n.failureSummary?.split("\n")[0] ?? ""}`)
                  .join("\n"),
            )
            .join("\n"),
      );
    }

    expect(blockers, `${page.name} must have no critical/serious axe violations`).toEqual([]);
  });
}
