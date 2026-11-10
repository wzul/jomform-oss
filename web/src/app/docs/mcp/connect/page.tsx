import type { Metadata } from "next";
import { docsMetadata } from "../../../../lib/docsMetadata";
import { DocsPage, type Section } from "../../docsProse";

export const metadata: Metadata = docsMetadata({
  title: "Connect an MCP client — JomForm Docs",
  description:
    "Connect Claude, Cursor or another MCP client to JomForm: the remote server URL, the OAuth sign-in, and the JSON entry Cursor uses.",
  path: "/docs/mcp/connect",
});

// Card t_0356ed4c split /docs/mcp (7,685 words on a phone) by TASK. This page is
// the one a reader lands on with "how do I connect this thing" — the prose that
// used to open the old page, plus the two client walk-throughs that were only in
// the MCP tool reference's OAuth section.
//
// The client steps are the vendors' own documented flows, checked against
// support.claude.com (custom connectors, remote MCP) and cursor.com/docs/mcp —
// both read 2026-11-10. A vendor that renames a menu item is a copy change here,
// not a protocol change.

const SECTIONS: Section[] = [
  {
    id: "connect",
    title: "Connect an agent",
    icon: "🔌",
    body: [
      "JomForm speaks the **Model Context Protocol**. Point any MCP client at `https://mcp.jomform.com/mcp` and it discovers the tools on its own.",
      {
        note: "The connector authenticates with **OAuth 2.1** using an authorization code and PKCE. There are no API keys to copy or rotate. Scopes are on [OAuth & tokens](/docs/mcp/oauth).",
      },
      { heading: "Claude" },
      {
        steps: [
          "Open **Settings → Connectors** and choose **Add custom connector**.",
          "Paste `https://mcp.jomform.com/mcp` as the remote MCP server URL.",
          "Claude reads the **OAuth metadata** itself, so the detected client settings are fine.",
          "Choose **Sign in now** and approve the JomForm consent screen. The tools then appear in a conversation.",
        ],
      },
      { heading: "Cursor" },
      {
        steps: [
          "Open **Settings → Tools & MCP → New MCP Server**.",
          "Add `https://mcp.jomform.com/mcp` as a remote URL, or edit `~/.cursor/mcp.json`.",
          "Sign in when Cursor opens the browser; the client stores the token for later calls.",
        ],
      },
      {
        code: '{\n  "mcpServers": {\n    "jomform": { "url": "https://mcp.jomform.com/mcp" }\n  }\n}',
        caption: "Cursor's mcp.json entry. The OAuth sign-in opens on the first call.",
      },
    ],
  },
];

export default function McpConnectPage() {
  return (
    <DocsPage
      title="Connect an MCP client"
      lead="One remote URL and a browser sign-in. No API key, no local server to run."
      sections={SECTIONS}
    />
  );
}
