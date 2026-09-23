import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

// axe-core accessibility scan (issue #104).
//
// Scans the key public pages and FAILS on any critical or serious violation.
// Minor/moderate findings are reported but do not fail the build, so the gate
// stays strict on the WCAG AA blockers while surfacing the rest for triage.
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
  // t_f5a8730d: the knowledge base was outside this scan entirely, so a docs
  // PR could not be checked by the a11y gate at all (PR 676 edited
  // /docs/customize and even a full `npm run a11y` never looked at it). Every
  // page under web/src/app/docs is listed here; /docs and /docs/customize are
  // the minimum, the rest so a regression on any guide turns this red.
  // src/lib/docsA11y.test.ts fails the unit suite when a docs page is missing
  // from this list, so a page added later cannot quietly stay outside the gate.
  { name: "docs-index", path: "/docs" },
  { name: "docs-builder", path: "/docs/builder" },
  { name: "docs-customize", path: "/docs/customize" },
  { name: "docs-embed", path: "/docs/embed" },
  { name: "docs-subscriptions", path: "/docs/subscriptions" },
  { name: "docs-variations", path: "/docs/variations" },
  { name: "docs-spam-detection", path: "/docs/spam-detection" },
  { name: "docs-ses-reputation", path: "/docs/ses-reputation" },
  { name: "docs-placeholders", path: "/docs/placeholders" },
  { name: "docs-mcp", path: "/docs/mcp" },
  // t_bac29e18: the REST API reference is a public, sitemapped, footer-linked
  // page — exactly the shape this scan exists for. Its endpoint tables are the
  // largest block of dense small text on the docs site, so leaving it unscanned
  // would repeat the gap t_d768342e closed.
  { name: "docs-api", path: "/docs/api" },
  { name: "docs-feedback", path: "/docs/feedback" },
  { name: "docs-chat", path: "/docs/chat" },
];

for (const page of PAGES) {
  test(`axe: ${page.name} has no critical/serious violations`, async ({ page: p }) => {
    await p.goto(page.path, { waitUntil: "networkidle" });
    const results = await new AxeBuilder({ page: p }).analyze();

    const blockers = results.violations.filter((v) =>
      v.impact === "critical" || v.impact === "serious",
    );

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
