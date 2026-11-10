import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";
import { DocsPage, type Section } from "../docsProse";
import { TOOL_GROUPS } from "../../../lib/mcpToolGroups.ts";

export const metadata: Metadata = docsMetadata({
  title: "MCP tool reference — JomForm Docs",
  description:
    "Connect JomForm to Claude, Cursor or another AI agent over the Model Context Protocol, then find the tool, the OAuth scope or the fix you need.",
  path: "/docs/mcp",
});

// Card t_0356ed4c: /docs/mcp was 7,685 words and 35,608px tall on a phone. It is
// now a HUB of four task pages under /docs/mcp/*, and every page is one hop away:
//
//   /docs/mcp/connect         set the client up (Claude, Cursor)
//   /docs/mcp/tools           the registry, as tables
//   /docs/mcp/oauth           discovery, scopes, grants
//   /docs/mcp/troubleshooting what a refusal means
//
// Nothing was deleted in the move: the old page's prose lives on those four, and
// the tool rows live in one module (src/lib/mcpToolGroups.ts) they all read.
// The count on this page is DERIVED from that array — mcpToolInventory.test.ts
// fails the build if it is typed by hand or if the rows and the Go registry drift.

const TOTAL = TOOL_GROUPS.reduce((n, g) => n + g.tools.length, 0);
const CATEGORIES = TOOL_GROUPS.filter((g) => g.tools.length > 0).length;

const SECTIONS: Section[] = [
  {
    id: "start",
    title: "Start here",
    icon: "🔌",
    body: [
      "JomForm speaks the **Model Context Protocol**. Point any MCP client at `https://mcp.jomform.com/mcp` and it discovers the tools on its own.",
      "The connector authenticates with **OAuth 2.1** using an authorization code and PKCE. There are no API keys to copy or rotate.",
      {
        list: [
          "**Connect** an agent → [Claude and Cursor setup](/docs/mcp/connect).",
          "**Look up a tool** → [the full tool list](/docs/mcp/tools), grouped by what it does.",
          "**Understand the grant** → [OAuth and tokens](/docs/mcp/oauth): discovery and scopes.",
          "**Something fails** → [troubleshooting](/docs/mcp/troubleshooting): what each refusal means.",
        ],
      },
    ],
  },
  {
    id: "what",
    title: "What an agent can do",
    icon: "🧭",
    body: [
      `This release registers **${TOTAL} tools across ${CATEGORIES} categories** — the same list the server advertises over \`tools/list\`.`,
      "Each tool names its **inputs and outputs**. An agent reads that schema and calls the tool directly, so the docs and the server cannot disagree.",
      {
        tip: "Tell the agent to read a record before changing it. `get_form` before `upsert_form` is the difference between an edit and a lost form.",
      },
    ],
  },
];

export default function McpReferencePage() {
  return (
    <DocsPage
      title="MCP tool reference"
      lead="One remote server for every AI agent: set it up, look up a tool, or find out why a call was refused."
      sections={SECTIONS}
      jumpTo
    />
  );
}
