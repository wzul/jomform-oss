import type { Metadata } from "next";
import McpPage from "./mcp-client";

// Issue #276: per-page meta for the MCP setup/marketing page.
export const metadata: Metadata = {
  title: "Connect JomForm to your AI agent (MCP) — JomForm",
  description:
    "Connect JomForm to Claude, Cursor and other AI agents via the Model Context Protocol. Build forms, manage sales and payments from your agent.",
  alternates: { canonical: "https://jomform.com/mcp" },
  openGraph: {
    title: "Connect JomForm to your AI agent (MCP) — JomForm",
    description:
      "Connect JomForm to Claude, Cursor and other AI agents via the Model Context Protocol. Build forms, manage sales and payments from your agent.",
    url: "https://jomform.com/mcp",
    siteName: "JomForm",
    type: "website",
  },
};

export default function Page() {
  return <McpPage />;
}
