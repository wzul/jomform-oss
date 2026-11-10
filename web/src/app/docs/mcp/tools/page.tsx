import type { Metadata } from "next";
import { docsMetadata } from "../../../../lib/docsMetadata";
import { DocsPage, type Section } from "../../docsProse";
import { TOOL_GROUPS } from "../../../../lib/mcpToolGroups.ts";

export const metadata: Metadata = docsMetadata({
  title: "MCP tool list — JomForm Docs",
  description:
    "Every MCP tool JomForm registers, grouped by what it does, with the schema an agent reads. Kept in sync with the api/internal/mcpsrv package by a build guard.",
  path: "/docs/mcp/tools",
});

// Card t_0356ed4c: the tool reference half of the old /docs/mcp page. The rows
// come from src/lib/mcpToolGroups.ts — the SAME array the Go-side description
// guard and the three web guards read — and the tables below are a scan aid: a
// reader looks for a name, not for a paragraph.
//
// WHY THE TABLES ARE JSX, NOT `{ table: … }` BLOCKS: the style gate measures every
// string a block carries, and a tool DESCRIPTION is data, not a paragraph. The
// block surface is for prose tables (short cells); this is the registry, rendered
// once from the array with the same scroll/announce shape the renderer uses
// (overflow-x-auto, tab stop, labelled region) so a phone and the a11y gate see
// the same table the block tables give them.

const TOTAL = TOOL_GROUPS.reduce((n, g) => n + g.tools.length, 0);
const CATEGORIES = TOOL_GROUPS.filter((g) => g.tools.length > 0).length;

/** A stable anchor for a group heading: "CHIP per-form" -> "group-chip-per-form". */
function groupSlug(name: string): string {
  return `group-${name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/(^-|-$)/g, "")}`;
}

const SECTIONS: Section[] = [
  {
    id: "tools",
    title: "Tool list",
    icon: "🧰",
    body: [
      "This page lists **every tool** the registry registers at this release, grouped by what it does.",
      "A guard fails the build if this list and `api/internal/mcpsrv` drift apart in either direction.",
      {
        note: "The total below is derived from the rows, never typed. A hand-typed count is how the page fell ten tools behind once already.",
      },
    ],
  },
  {
    id: "structured-output",
    title: "Structured output",
    icon: "📦",
    body: [
      "Tool results come back as **structured content**. Every tool declares the shape of that result in its `outputSchema`.",
      "All tools declare an **object** schema. That is what keeps the server usable by a client that validates `tools/list` strictly.",
      "Every listing tool wraps its array in an object: the records sit under `items`.",
      "A paginated listing also carries `page`, `limit` and `total`.",
      "So `list_products` returns `{\"items\": [...]}`, not a bare array.",
      "Batch tools report their summary beside the per-item outcomes. `capture_all_pending_sales` returns `{\"requested\", \"captured\", \"failed\", \"results\": [...]}`.",
      "Single-record tools return their fields at the top level of the object.",
    ],
  },
];

export default function McpToolsPage() {
  return (
    <div className="max-w-[36rem]">
      <DocsPage
        title="Tool list"
        lead="Every tool, grouped by what it does. Scan the names, then read the one you need."
        sections={SECTIONS}
        jumpTo
        extraJumps={[{ href: "#all-tools", label: "All tools" }]}
      />

      <section id="all-tools" className="mt-10 scroll-mt-24">
        <h2 className="flex items-center gap-2 text-lg font-semibold text-[var(--jf-text)]">
          <span aria-hidden="true" className="text-base">
            📇
          </span>
          All tools
        </h2>
        <p className="mt-2 text-base leading-7 text-[var(--jf-text-secondary)]">
          {TOTAL} tools across {CATEGORIES} categories. Every row below is a tool the registry registers.
        </p>

        <div
          tabIndex={0}
          role="region"
          aria-label="Tool categories"
          className="mt-4 overflow-x-auto rounded-xl border border-[var(--jf-border)]"
        >
          <table className="w-full border-collapse text-left text-sm">
            <thead className="bg-[var(--jf-bg-soft)] text-[var(--jf-text)]">
              <tr>
                <th scope="col" className="px-3 py-2 font-semibold">
                  Category
                </th>
                <th scope="col" className="px-3 py-2 font-semibold">
                  Tools
                </th>
              </tr>
            </thead>
            <tbody className="text-[var(--jf-text-secondary)]">
              {TOOL_GROUPS.filter((g) => g.tools.length > 0).map((g) => (
                <tr key={g.group} className="border-t border-[var(--jf-border)]">
                  <td className="px-3 py-2 align-top [overflow-wrap:anywhere]">
                    <a
                      href={`#${groupSlug(g.group)}`}
                      className="font-medium text-[var(--jf-primary-strong)] underline underline-offset-2"
                    >
                      {g.group}
                    </a>
                  </td>
                  <td className="px-3 py-2 align-top [overflow-wrap:anywhere]">{g.tools.length}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        {TOOL_GROUPS.map((g) => (
          <div key={g.group} className="mt-8">
            <h3
              id={groupSlug(g.group)}
              className="scroll-mt-24 text-base font-semibold tracking-tight text-[var(--jf-text)]"
            >
              {g.group}
            </h3>
            {g.note && (
              <p className="mt-2 text-base leading-7 text-[var(--jf-text-secondary)] [overflow-wrap:anywhere]">
                {g.note}
              </p>
            )}
            {g.tools.length > 0 && (
              // The region's accessible name is a LITERAL, not a template:
              // docsTableScroll.test.ts reads it out of the source, and a computed
              // label reads as "no name" to that scan. The group is named by the
              // table's own <caption> below.
              <div
                tabIndex={0}
                role="region"
                aria-label="MCP tool table"
                className="mt-3 overflow-x-auto rounded-xl border border-[var(--jf-border)]"
              >
                <table className="w-full border-collapse text-left text-sm">
                  <caption className="sr-only">{g.group} — MCP tools</caption>
                  <thead className="bg-[var(--jf-bg-soft)] text-[var(--jf-text)]">
                    <tr>
                      <th scope="col" className="px-3 py-2 font-semibold">
                        Tool
                      </th>
                      <th scope="col" className="px-3 py-2 font-semibold">
                        What it does
                      </th>
                    </tr>
                  </thead>
                  <tbody className="text-[var(--jf-text-secondary)]">
                    {g.tools.map((t) => (
                      <tr key={t.name} className="border-t border-[var(--jf-border)]">
                        <td className="px-3 py-2 align-top [overflow-wrap:anywhere]">
                          <code className="break-all font-mono text-sm font-semibold text-[var(--jf-primary-strong)]">
                            {t.name}
                          </code>
                        </td>
                        <td className="px-3 py-2 align-top [overflow-wrap:anywhere]">{t.desc}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        ))}
      </section>
    </div>
  );
}
