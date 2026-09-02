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
